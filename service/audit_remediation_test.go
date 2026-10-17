package service

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/common_err"
)

func TestCreateFileProtectsInternalDataAndNeverTruncatesExistingContent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CASAOS_DATA_ROOT", root)
	previous := config.AppInfo.UserDataPath
	config.AppInfo.UserDataPath = filepath.Join(root, "conf")
	t.Cleanup(func() { config.AppInfo.UserDataPath = previous })
	s := NewSystemService()
	path := filepath.Join(root, "file.txt")
	if code, err := s.CreateFile(path); err != nil || code != common_err.SUCCESS {
		t.Fatal(code, err)
	}
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, err := s.CreateFile(path); err != nil || code == common_err.SUCCESS {
		t.Fatal(code, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
		t.Fatal(string(data), err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if code, err := s.CreateFile(outside); err != nil || code != common_err.SUCCESS {
		t.Fatal("browser-accessible location rejected", code, err)
	}
	for _, path := range []string{filepath.Join(root, ".temp", "part"), filepath.Join(config.AppInfo.UserDataPath, "new.conf")} {
		if code, err := s.CreateFile(path); code == common_err.SUCCESS || err == nil {
			t.Fatal("unsafe creation allowed", path, code, err)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("unsafe file created", path, err)
		}
	}
}

func TestRenamePreservesConflictAndAllowsCaseChange(t *testing.T) {
	root := t.TempDir()
	s := NewSystemService()
	from, to := filepath.Join(root, "report.txt"), filepath.Join(root, "occupied.txt")
	if err := os.WriteFile(from, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, err := s.RenameFile(from, to); code != common_err.DIR_ALREADY_EXISTS || err != nil {
		t.Fatal(code, err)
	}
	if data, err := os.ReadFile(to); err != nil || string(data) != "target" {
		t.Fatal(string(data), err)
	}
	to = filepath.Join(root, "Report.txt")
	if code, err := s.RenameFile(from, to); err != nil || code != common_err.SUCCESS {
		t.Fatal(code, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Name() == "Report.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("case change did not reach filesystem")
	}
}

func TestSMBShareNamesSupportRootsAndExplicitNames(t *testing.T) {
	for _, name := range []string{"Photos", "O'Brien", "Family Photos"} {
		if actual, err := ShareName(filepath.Join(t.TempDir(), "other"), name); err != nil || actual != name {
			t.Fatal(actual, err)
		}
	}
	for _, name := range []string{"..", "C:", "bad/name", `bad\name`, "bad\nname", "bad[name]", " Photos"} {
		if _, err := ShareName("unused", name); err == nil {
			t.Fatal("invalid share name allowed", name)
		}
	}
	if runtime.GOOS == "windows" {
		if name, err := ShareName(`C:\`, ""); err != nil || name != "C_Drive" {
			t.Fatal(name, err)
		}
	}
}
