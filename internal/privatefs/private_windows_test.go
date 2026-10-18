package privatefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivateDescriptorsExcludeOrdinaryWindowsAccounts(t *testing.T) {
	for _, sddl := range []string{directoryACL, fileACL} {
		sd, err := windows.SecurityDescriptorFromString(sddl)
		if err != nil {
			t.Fatal(err)
		}
		owner, _, err := sd.Owner()
		if err != nil || owner.String() != "S-1-5-32-544" {
			t.Fatal(owner, err)
		}
		text := sd.String()
		if !strings.Contains(text, "D:P") || !strings.Contains(text, ";;;SY)") || !strings.Contains(text, ";;;BA)") || strings.Count(text, "(A;") != 2 {
			t.Fatal("unexpected ACL", text)
		}
	}
}

func TestPrivateStorageRefusesUnprivilegedProcess(t *testing.T) {
	if windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("unprivileged process test")
	}
	root := filepath.Join(t.TempDir(), "private")
	if err := ProtectTree(root); err == nil {
		t.Fatal("unprivileged server silently accepted insecure storage")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("storage created before privilege check", err)
	}
}

func TestPrivateWindowsACLAppliedToExistingDescendants(t *testing.T) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("applying the owner/ACL requires an elevated service identity")
	}
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("private"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ProtectTree(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TMP", "TEMP", "TMPDIR", "SystemTemp"} {
		t.Setenv(name, os.Getenv(name))
	}
	if err := ConfigureTemporaryDirectory(filepath.Join(root, "uploads")); err != nil {
		t.Fatal(err)
	}
	temp, err := os.CreateTemp("", "private-upload-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := temp.Close(); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{root, path, temp.Name()} {
		sd, err := windows.GetNamedSecurityInfo(entry, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		owner, _, err := sd.Owner()
		if err != nil || (entry != temp.Name() && owner.String() != "S-1-5-32-544") {
			t.Fatal(owner, err)
		}
		if strings.Contains(sd.String(), ";;;BU)") || strings.Contains(sd.String(), ";;;AU)") {
			t.Fatal(sd.String())
		}
	}
}

func TestPrivateConfigDirectoriesAreProtectedAtCreation(t *testing.T) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("creating Administrators-owned storage requires elevation")
	}
	root := filepath.Join(t.TempDir(), "service")
	configDir := filepath.Join(root, "conf")
	path := filepath.Join(configDir, "casaos.conf")
	if err := PrepareConfig(path); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{root, configDir} {
		sd, err := windows.GetNamedSecurityInfo(directory, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		owner, _, err := sd.Owner()
		if err != nil || owner.String() != "S-1-5-32-544" {
			t.Fatal("new directory was not born Administrators-owned", owner, err)
		}
		if err := checkDescriptor(sd, false); err != nil {
			t.Fatal("new directory was not born private", err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("preparation created configuration contents", err)
	}
	// A trusted custom config directory must not have its own ACL rewritten.
	before, err := windows.GetNamedSecurityInfo(configDir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("trusted config"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareConfig(path); err != nil {
		t.Fatal(err)
	}
	after, err := windows.GetNamedSecurityInfo(configDir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || before.String() != after.String() {
		t.Fatal("preparing a custom config changed its parent ACL", err)
	}
}
