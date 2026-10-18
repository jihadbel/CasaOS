// Package privatefs restricts stored files to the privileged server identity.
package privatefs

import (
	"errors"
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
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := safefs.NoLinks(path); err != nil {
			return err
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return errors.New("private storage cannot contain links or special files")
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
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	return protect(root, true)
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
