package safefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestAtomicWritePreservesWindowsSecurityAndStreams(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	setACL := func(path, sddl string) {
		t.Helper()
		sd, err := windows.SecurityDescriptorFromString(sddl)
		if err != nil {
			t.Fatal(err)
		}
		acl, _, err := sd.DACL()
		if err != nil {
			t.Fatal(err)
		}
		if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	setACL(dir, "D:P(A;OICI;FA;;;"+user.User.Sid.String()+")(A;OICI;GR;;;WD)")
	path := filepath.Join(dir, "private.txt")
	if err := os.WriteFile(path, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	setACL(path, "D:P(A;;FA;;;"+user.User.Sid.String()+")")
	if err := os.WriteFile(path+":note", []byte("metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := os.CreateTemp(dir, ".acl-probe-")
	if err != nil {
		t.Fatal(err)
	}
	staged.Close()
	defer os.Remove(staged.Name())
	if _, err := prepareAtomicWrite(path, staged.Name()); err != nil {
		t.Fatal(err)
	}
	stagedSD, err := windows.GetNamedSecurityInfo(staged.Name(), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || strings.Contains(stagedSD.String(), "WD)") {
		t.Fatal("staged content would be exposed", err)
	}
	if err := WriteAtomic(path, []byte("edited"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || before.String() != after.String() {
		t.Fatal("ACL changed", before.String(), after, err)
	}
	if data, err := os.ReadFile(path + ":note"); err != nil || string(data) != "metadata" {
		t.Fatal("named stream lost", string(data), err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "edited" {
		t.Fatal(string(data), err)
	}
}

func TestAtomicWriteWaitsForBriefWindowsReaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	timer := time.AfterFunc(80*time.Millisecond, func() { reader.Close() })
	defer timer.Stop()
	if err := WriteAtomic(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "after" {
		t.Fatal(string(data), err)
	}
}

func TestAtomicWriteKeepsOriginalWhenWindowsReaderStaysOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := WriteAtomic(path, []byte("after"), 0600); err == nil {
		t.Fatal("unexpected replacement of a locked file")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "before" {
		t.Fatal("original changed", string(data), err)
	}
}

func TestAtomicWriteCreatesAndReplacesLongWindowsPaths(t *testing.T) {
	dir := t.TempDir()
	for len(dir) < 290 {
		dir = filepath.Join(dir, "nested-path-segment")
	}
	path := filepath.Join(dir, "file.txt")
	for _, text := range []string{"first", "second"} {
		if err := WriteAtomic(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != text {
			t.Fatal(string(data), err)
		}
	}
}

func TestAtomicWritePreservesInheritedWindowsACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inherited.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || before.String() != after.String() {
		t.Fatal("inherited ACL changed", before.String(), after, err)
	}
}
