//go:build !windows

package privatefs

import (
	"errors"
	"os"
)

func requireServiceIdentity() error {
	if os.Geteuid() != 0 {
		return errors.New("protected local storage requires the root-owned CasaOS service")
	}
	return nil
}
func protect(path string, directory bool) error {
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	if err := os.Chown(path, 0, 0); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}
