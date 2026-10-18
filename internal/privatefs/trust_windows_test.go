package privatefs

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestServiceTrustRejectsSeededOwnersAndWritableDescriptors(t *testing.T) {
	for _, entry := range []struct {
		name, sddl        string
		ancestor, allowed bool
	}{
		{"private", directoryACL, false, true},
		{"ordinary-owner", "O:BUG:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)", false, false},
		{"world-writable", "O:BAG:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FW;;;WD)", false, false},
		{"inherited-write", "O:BAG:BAD:P(A;OICIIO;GW;;;BU)(A;;FA;;;BA)", false, false},
		{"null-dacl", "O:BAG:BAD:NO_ACCESS_CONTROL", false, false},
		{"readonly-users", "O:BAG:BAD:P(A;;FA;;;BA)(A;;FR;;;BU)", false, true},
		{"parent-allows-siblings", "O:SYG:SYD:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;GW;;;BU)", true, true},
		{"parent-deletes-children", "O:SYG:SYD:P(A;;FA;;;SY)(A;;0x40;;;BU)", true, false},
		{"parent-changes-acl", "O:SYG:SYD:P(A;;FA;;;SY)(A;;WD;;;BU)", true, false},
		{"trusted-installer-parent", "O:" + trustedInstallerSID + "G:SYD:P(A;;FA;;;SY)", true, true},
	} {
		t.Run(entry.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(entry.sddl)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkDescriptor(sd, entry.ancestor); (err == nil) != entry.allowed {
				t.Fatal("unexpected trust decision", err)
			}
		})
	}
}

func TestTrustInspectionLeavesPlantedCredentialsUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	const planted = `[{"username":"admin","passwordHash":"planted"}]`
	if err := os.WriteFile(path, []byte(planted), 0600); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;WD)")
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
	before, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTrusted(path, false); err == nil {
		t.Fatal("planted writable account database was trusted")
	}
	after, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil || after.String() != before.String() {
		t.Fatal("inspection repaired the untrusted ACL", err)
	}
	if contents, err := os.ReadFile(path); err != nil || string(contents) != planted {
		t.Fatal("inspection changed the planted credentials", err)
	}
}

func TestWindowsProgramDataIsSafeForPrivateDirectoryCreation(t *testing.T) {
	path, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0)
	if err != nil {
		t.Fatal(err)
	}
	for parent := path; ; parent = filepath.Dir(parent) {
		if err := checkTrusted(parent, true); err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
}
