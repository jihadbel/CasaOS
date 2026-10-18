// Package privatefs restricts stored files to the privileged server identity.
package privatefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
)

func ProtectTree(root string) error {
	if !filepath.IsAbs(root) || filepath.Dir(filepath.Clean(root)) == filepath.Clean(root) {
		return errors.New("private storage must be an absolute dedicated directory")
	}
	if err := requireServiceIdentity(); err != nil {
		return err
	}
	if err := safefs.NoLinks(root); err != nil {
		return err
	}
	if err := ensureTrustedDirectory(root); err != nil {
		return err
	}
	// Inspect every existing owner/ACL before changing any of them. Tightening
	// permissions cannot make previously planted credentials trustworthy.
	if err := filepath.WalkDir(root, validateTreeEntry); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := validateTreeEntry(path, entry, nil); err != nil {
			return err
		}
		return protect(path, entry.IsDir())
	})
}

// ProtectDirectory avoids rescanning homes already protected by the startup scan.
func ProtectDirectory(root string) error {
	if !filepath.IsAbs(root) || filepath.Dir(filepath.Clean(root)) == filepath.Clean(root) {
		return errors.New("private storage must be an absolute dedicated directory")
	}
	if err := requireServiceIdentity(); err != nil {
		return err
	}
	if err := safefs.NoLinks(root); err != nil {
		return err
	}
	if err := ensureTrustedDirectory(root); err != nil {
		return err
	}
	return protect(root, true)
}

// PrepareConfig runs before parsing configuration or opening the database.
// It never repairs an untrusted existing file, even when running as SYSTEM.
func PrepareConfig(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("service configuration path must be absolute")
	}
	if err := requireServiceIdentity(); err != nil {
		return err
	}
	if err := safefs.NoLinks(path); err != nil {
		return err
	}
	// A custom -c file may live outside the service's data tree. Validate that
	// directory without changing permissions on unrelated existing files.
	if err := ensureTrustedDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("service configuration must be a regular file")
	}
	if err := safefs.NoLinks(path); err != nil {
		return err
	}
	if err := checkTrusted(path, false); err != nil {
		return err
	}
	return protect(path, false)
}

func validateTreeEntry(path string, entry os.DirEntry, err error) error {
	if err != nil {
		return err
	}
	if !entry.IsDir() && !entry.Type().IsRegular() {
		return errors.New("private storage cannot contain links or special files")
	}
	if err := safefs.NoLinks(path); err != nil {
		return err
	}
	return checkTrusted(path, false)
}

// Missing directories are created with private permissions atomically. Existing
// ancestors may permit creation of siblings (ProgramData, /tmp), but must not
// let an untrusted user rename or replace the protected directory itself.
func ensureTrustedDirectory(root string) error {
	var missing []string
	ancestor := root
	for {
		info, err := os.Lstat(ancestor)
		if os.IsNotExist(err) && filepath.Dir(ancestor) != ancestor {
			missing = append(missing, ancestor)
			ancestor = filepath.Dir(ancestor)
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("service directory is not a directory: %s", ancestor)
		}
		break
	}
	for parent := ancestor; ; parent = filepath.Dir(parent) {
		if err := checkTrusted(parent, true); err != nil {
			return err
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := createPrivateDirectory(missing[i]); err != nil && !os.IsExist(err) {
			return err
		}
		// Includes directories created by a racing process, which must not be adopted.
		if err := safefs.NoLinks(missing[i]); err != nil {
			return err
		}
		if err := checkTrusted(missing[i], false); err != nil {
			return err
		}
	}
	return checkTrusted(root, false)
}

// Multipart parsing must never spool private uploads to a shared OS temp folder.
func ConfigureTemporaryDirectory(root string) error {
	if err := ProtectTree(root); err != nil {
		return err
	}
	// GetTempPath2 uses SystemTemp for SYSTEM; TMP/TEMP cover other Windows
	// identities and TMPDIR covers Unix. Verify the actual runtime selection.
	// https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-gettemppath2a
	for _, name := range []string{"TMP", "TEMP", "TMPDIR", "SystemTemp"} {
		if err := os.Setenv(name, root); err != nil {
			return err
		}
	}
	expected, err := os.Stat(root)
	if err != nil {
		return err
	}
	actual, err := os.Stat(os.TempDir())
	if err != nil {
		return err
	}
	if !os.SameFile(expected, actual) {
		return errors.New("runtime temporary directory is outside protected storage")
	}
	return nil
}
