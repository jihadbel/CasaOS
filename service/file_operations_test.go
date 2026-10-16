package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/IceWhaleTech/CasaOS/model"
)

func TestCancellationSignalsRunningCopyAndRemovesOnlyRequestedJob(t *testing.T) {
	id, other := t.Name(), t.Name()+"-other"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fileOperationMu.Lock()
	FileQueue.Store(id, model.FileOperate{})
	FileQueue.Store(other, model.FileOperate{})
	OpStrArr = append(OpStrArr, id, other)
	operationCancels[id] = cancel
	fileOperationMu.Unlock()
	defer func() {
		CancelFileOperation(id)
		CancelFileOperation(other)
		fileOperationMu.Lock()
		delete(operationCancels, id)
		fileOperationMu.Unlock()
	}()
	CancelFileOperation(id)
	if ctx.Err() != context.Canceled {
		t.Fatal("running copy was not cancelled")
	}
	jobs := FileOperationsSnapshot()
	if len(jobs) != 1 || jobs[0].ID != other {
		t.Fatalf("wrong job removed: %+v", jobs)
	}
}

func TestConcurrentQueueAndSnapshotsCompleteWithoutSharedSlices(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	const count = 12
	var producers sync.WaitGroup
	for index := 0; index < count; index++ {
		index := index
		producers.Add(1)
		go func() {
			defer producers.Done()
			name := fmt.Sprintf("file-%d.txt", index)
			source := filepath.Join(root, name)
			if err := os.WriteFile(source, []byte(name), 0600); err != nil {
				t.Error(err)
				return
			}
			QueueFileOperation(fmt.Sprintf("%s-%d", t.Name(), index), model.FileOperate{Type: "copy", To: destination, Item: []model.FileItem{{From: source, Size: int64(len(name))}}})
			for _, snapshot := range FileOperationsSnapshot() {
				if len(snapshot.Operation.Item) > 0 {
					snapshot.Operation.Item[0].From = "corrupted snapshot"
				}
			}
		}()
	}
	producers.Wait()
	deadline := time.After(15 * time.Second)
	for {
		fileOperationMu.Lock()
		running := fileWorkerRunning
		done := fileWorkerDone
		fileOperationMu.Unlock()
		if !running {
			break
		}
		select {
		case <-done:
		case <-deadline:
			t.Fatal("file worker did not finish")
		}
	}
	for index := 0; index < count; index++ {
		name := fmt.Sprintf("file-%d.txt", index)
		bytes, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil || string(bytes) != name {
			t.Errorf("%s: %q %v", name, bytes, err)
		}
	}
}
