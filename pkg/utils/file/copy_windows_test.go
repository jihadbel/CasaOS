package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestMoveKeepsBothTreesWhenNestedFileIsLocked(t *testing.T) {
	sourceRoot, targetRoot := copyFixture(t)
	source, target := filepath.Join(sourceRoot, "item"), filepath.Join(targetRoot, "item")
	putCopyFile(t, filepath.Join(source, "a.txt"), "source a")
	lockedPath := filepath.Join(source, "z.txt")
	putCopyFile(t, lockedPath, "source z")
	putCopyFile(t, filepath.Join(target, "old.txt"), "existing destination")
	name, err := windows.UTF16PtrFromString(lockedPath)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := MovePathContext(context.Background(), source, targetRoot, "overwrite"); err == nil {
		t.Fatal("locked nested source must fail the move")
	}
	requireCopyFile(t, filepath.Join(source, "a.txt"), "source a")
	requireCopyFile(t, filepath.Join(target, "old.txt"), "existing destination")
	entries, err := os.ReadDir(targetRoot)
	if err != nil || len(entries) != 1 {
		t.Fatalf("staging copy leaked: %v %v", entries, err)
	}
}
