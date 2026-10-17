package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPartialTrashEmptyPersistsCompletedRemovals(t *testing.T) {
	root := setupTrashTest(t)
	firstPath, secondPath := filepath.Join(root, "first.txt"), filepath.Join(root, "locked.txt")
	writeTestFile(t, firstPath, "first")
	writeTestFile(t, secondPath, "locked")
	first, second := moveTestItem(t, firstPath), moveTestItem(t, secondPath)
	name, err := windows.UTF16PtrFromString(second.TrashPath)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	w := apiRequest(t, handleTrash, "POST", "/api/trash/empty", nil)
	if w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	items, err := loadTrashManifest()
	if err != nil || len(items) != 1 || items[0].ID != second.ID {
		t.Fatal(items, err)
	}
	if _, err := os.Stat(first.TrashPath); !os.IsNotExist(err) {
		t.Fatal("completed removal retained", err)
	}
	if _, err := os.Stat(second.TrashPath); err != nil {
		t.Fatal("locked item lost", err)
	}
}
