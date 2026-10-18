//go:build !windows

package privatefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnixTrustRejectsWritableConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	if err := os.WriteFile(path, []byte("planted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if err := checkTrusted(path, false); err == nil {
		t.Fatal("untrusted writable credentials were accepted")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "planted" {
		t.Fatal("inspection changed the credentials", err)
	}
}

func TestUnixPrivateDirectoryCreationAndConfigTrust(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires the root service identity")
	}
	root := filepath.Join(t.TempDir(), "service")
	path := filepath.Join(root, "conf", "casaos.conf")
	if err := PrepareConfig(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("trusted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareConfig(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if err := PrepareConfig(path); err == nil {
		t.Fatal("configuration planted by another user was adopted")
	}
}
