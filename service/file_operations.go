package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/IceWhaleTech/CasaOS/model"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
)

var fileOperationMu sync.Mutex
var operationCancels = make(map[string]context.CancelFunc)
var fileWorkerRunning bool
var fileWorkerDone chan struct{}

type QueuedFileOperation struct {
	ID        string
	Operation model.FileOperate
}

func cloneFileOperation(operation model.FileOperate) model.FileOperate {
	operation.Item = append([]model.FileItem(nil), operation.Item...)
	return operation
}

// FileOperationsSnapshot owns its slices; readers cannot race with the worker.
func FileOperationsSnapshot() []QueuedFileOperation {
	fileOperationMu.Lock()
	defer fileOperationMu.Unlock()
	result := make([]QueuedFileOperation, 0, len(OpStrArr))
	for _, id := range OpStrArr {
		value, ok := FileQueue.Load(id)
		if !ok {
			continue
		}
		operation, ok := value.(model.FileOperate)
		if !ok {
			continue
		}
		result = append(result, QueuedFileOperation{id, cloneFileOperation(operation)})
	}
	return result
}

func QueueFileOperation(id string, operation model.FileOperate) {
	fileOperationMu.Lock()
	FileQueue.Store(id, cloneFileOperation(operation))
	OpStrArr = append(OpStrArr, id)
	start := !fileWorkerRunning
	if start {
		fileWorkerRunning = true
		fileWorkerDone = make(chan struct{})
	}
	done := fileWorkerDone
	fileOperationMu.Unlock()
	if start {
		go runFileOperations()
		go monitorFileOperations(done)
	}
}

func removeFileOperationLocked(id string) {
	FileQueue.Delete(id)
	for i, queued := range OpStrArr {
		if queued == id {
			OpStrArr = append(OpStrArr[:i], OpStrArr[i+1:]...)
			break
		}
	}
}

func CancelFileOperation(id string) {
	fileOperationMu.Lock()
	defer fileOperationMu.Unlock()
	if id == "0" {
		for _, cancel := range operationCancels {
			cancel()
		}
		FileQueue.Range(func(key, _ interface{}) bool { FileQueue.Delete(key); return true })
		OpStrArr = nil
		return
	}
	if cancel := operationCancels[id]; cancel != nil {
		cancel()
	}
	removeFileOperationLocked(id)
}

func FileOperate(id string) {
	fileOperationMu.Lock()
	value, ok := FileQueue.Load(id)
	if !ok || operationCancels[id] != nil {
		fileOperationMu.Unlock()
		return
	}
	operation, ok := value.(model.FileOperate)
	if !ok || operation.Finished {
		fileOperationMu.Unlock()
		return
	}
	operation = cloneFileOperation(operation)
	ctx, cancel := context.WithCancel(context.Background())
	operationCancels[id] = cancel
	operation.Running = true
	FileQueue.Store(id, cloneFileOperation(operation))
	fileOperationMu.Unlock()
	defer func() {
		cancel()
		fileOperationMu.Lock()
		delete(operationCancels, id)
		fileOperationMu.Unlock()
	}()
	for index, item := range operation.Item {
		if err := ctx.Err(); err != nil {
			return
		}
		var err error
		switch operation.Type {
		case "move":
			err = file.MovePathContext(ctx, item.From, operation.To, operation.Style)
		case "copy":
			err = file.CopyDirContext(ctx, item.From, operation.To, operation.Style)
		default:
			err = fmt.Errorf("unsupported file operation: %s", operation.Type)
		}
		if err != nil {
			operation.Error = err.Error()
			break
		}
		operation.Item[index].Finished = true
		operation.Item[index].ProcessedSize = item.Size
		operation.ProcessedSize += item.Size
		fileOperationMu.Lock()
		if _, exists := FileQueue.Load(id); exists {
			FileQueue.Store(id, cloneFileOperation(operation))
		}
		fileOperationMu.Unlock()
	}
	operation.Finished, operation.Running = true, false
	fileOperationMu.Lock()
	if _, exists := FileQueue.Load(id); exists {
		FileQueue.Store(id, cloneFileOperation(operation))
	}
	fileOperationMu.Unlock()
}

func runFileOperations() {
	for {
		fileOperationMu.Lock()
		if len(OpStrArr) == 0 {
			fileWorkerRunning = false
			if fileWorkerDone != nil {
				close(fileWorkerDone)
				fileWorkerDone = nil
			}
			fileOperationMu.Unlock()
			return
		}
		id := OpStrArr[0]
		fileOperationMu.Unlock()
		FileOperate(id)
		if MyService != nil {
			MyService.Notify().SendFileOperateNotify(true)
		}
		fileOperationMu.Lock()
		removeFileOperationLocked(id)
		fileOperationMu.Unlock()
	}
}

// ExecOpFile is retained for callers that explicitly start an existing queue.
func ExecOpFile() {
	fileOperationMu.Lock()
	if fileWorkerRunning {
		fileOperationMu.Unlock()
		return
	}
	fileWorkerRunning = true
	fileWorkerDone = make(chan struct{})
	fileOperationMu.Unlock()
	runFileOperations()
}

func CheckFileStatus() {
	fileOperationMu.Lock()
	done := fileWorkerDone
	fileOperationMu.Unlock()
	if done != nil {
		monitorFileOperations(done)
	}
}

func monitorFileOperations(done <-chan struct{}) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if MyService != nil {
				MyService.Notify().SendFileOperateNotify(true)
			}
		}
	}
}
