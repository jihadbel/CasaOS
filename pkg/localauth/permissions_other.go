//go:build !windows

package localauth

import "os"

func protectDirectory(path string) error { return os.Chmod(path, 0700) }
