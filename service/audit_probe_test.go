package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/IceWhaleTech/CasaOS-Common/utils/logger"
	"github.com/IceWhaleTech/CasaOS/model"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/common_err"
)

// Regression tests for file safety and notification failures found by the audit.
// All file operations are confined to a fresh testing.T.TempDir.
func TestAuditMoveRetainsSourceOnCopyFailure(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	if err := os.WriteFile(source, []byte("irreplaceable"), 0600); err != nil {
		t.Fatal(err)
	}
	key := t.Name()
	FileQueue.Store(key, model.FileOperate{Type: "move", Style: "overwrite", To: filepath.ToSlash(filepath.Join(root, "missing", "target")), Item: []model.FileItem{{From: filepath.ToSlash(source)}}})
	defer FileQueue.Delete(key)
	FileOperate(key)
	if _, err := os.Stat(source); err != nil {
		t.Errorf("source was deleted after destination copy failed: %v", err)
	}
}

func TestAuditMoveRejectsEquivalentParent(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	if err := os.WriteFile(source, []byte("irreplaceable"), 0600); err != nil {
		t.Fatal(err)
	}
	key := t.Name()
	FileQueue.Store(key, model.FileOperate{Type: "move", Style: "overwrite", To: filepath.ToSlash(root) + "/.", Item: []model.FileItem{{From: filepath.ToSlash(source)}}})
	defer FileQueue.Delete(key)
	FileOperate(key)
	if _, err := os.Stat(source); err != nil {
		t.Errorf("equivalent destination parent deleted the source: %v", err)
	}
}

func TestAuditCreateFileReportsWriteFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-parent", "new.txt")
	code, err := NewSystemService().CreateFile(path)
	if code == common_err.SUCCESS && err == nil {
		t.Error("CreateFile reported success although its parent directory does not exist")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unexpected target state: %v", err)
	}
}

func TestAuditMkdirReportsWriteFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(parent, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	code, err := NewSystemService().MkdirAll(filepath.Join(parent, "child"))
	if code == common_err.SUCCESS && err == nil {
		t.Error("MkdirAll reported success for a child of a regular file")
	}
}

func TestAuditMissingMessageBusDoesNotPanic(t *testing.T) {
	logger.LogInitConsoleOnly()
	previousService, previousRuntime := MyService, config.CommonInfo.RuntimePath
	previousOrder := OpStrArr
	defer func() {
		MyService = previousService
		config.CommonInfo.RuntimePath = previousRuntime
		OpStrArr = previousOrder
	}()
	config.CommonInfo.RuntimePath = t.TempDir()
	MyService = NewService(nil, config.CommonInfo.RuntimePath)
	FileQueue.Range(func(k, _ interface{}) bool { FileQueue.Delete(k); return true })
	OpStrArr = nil
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("missing message bus caused panic: %v", p)
		}
	}()
	MyService.Notify().SendFileOperateNotify(true)
}

func TestAuditStaleQueueEntryDoesNotPanic(t *testing.T) {
	previousOrder := OpStrArr
	defer func() { FileQueue.Delete("other"); OpStrArr = previousOrder }()
	FileQueue.Range(func(k, _ interface{}) bool { FileQueue.Delete(k); return true })
	OpStrArr = []string{"cancelled"}
	// Retain a different queue entry so the notification takes its traversal path.
	FileQueue.Store("other", model.FileOperate{})
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("queue entry removed by cancellation caused panic: %v", p)
		}
	}()
	NewNotifyService(nil).SendFileOperateNotify(true)
}
