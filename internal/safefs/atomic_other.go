//go:build !windows

package safefs

import "os"

func prepareAtomicWrite(path, temporary string) (bool, error) { return false, nil }

func commitAtomicWrite(from, to string, existing bool) error { return os.Rename(from, to) }
