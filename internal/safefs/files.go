// Package safefs contains the file commit operations used by the standalone server.
package safefs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Within checks path components, rather than a textual prefix.
func Within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

// NoLinks rejects symlinks and Windows junctions in an existing path prefix.
// The caller must also use NoReplace when publishing a new file.
func NoLinks(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for p := abs; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("links are not allowed: %s", p)
			}
			resolved, err := filepath.EvalSymlinks(p)
			if err != nil {
				return err
			}
			if !strings.EqualFold(filepath.Clean(resolved), filepath.Clean(p)) {
				return fmt.Errorf("redirected path is not allowed: %s", p)
			}
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}

// WriteAtomic preserves the last complete file if writing its replacement fails.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	return WriteAtomicReader(path, bytes.NewReader(data), mode)
}

// WriteAtomicReader stages and syncs all bytes before replacing the destination.
func WriteAtomicReader(path string, source io.Reader, mode os.FileMode) error {
	return WriteAtomicReaderContext(context.Background(), path, source, mode)
}

func WriteAtomicReaderContext(ctx context.Context, path string, source io.Reader, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".casaos-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	existing, err := prepareAtomicWrite(path, f.Name())
	if err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := io.Copy(f, source); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return commitAtomicWrite(f.Name(), path, existing)
}
