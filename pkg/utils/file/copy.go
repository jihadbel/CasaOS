package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
)

type copyReader struct {
	ctx    context.Context
	source io.Reader
}

func (r copyReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}

// ValidateCopyTarget rejects aliases and overlapping trees before any mutation.
func ValidateCopyTarget(source, parent string) (string, string, error) {
	if !filepath.IsAbs(source) || !filepath.IsAbs(parent) {
		return "", "", errors.New("source and destination are required")
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return "", "", err
	}
	parent, err = filepath.Abs(parent)
	if err != nil {
		return "", "", err
	}
	if filepath.Dir(source) == source {
		return "", "", errors.New("cannot operate on a filesystem root")
	}
	target := filepath.Join(parent, filepath.Base(source))
	if _, err := ValidateUserPath(source); err != nil {
		return "", "", err
	}
	if _, err := ValidateUserPath(target); err != nil {
		return "", "", err
	}
	if err := safefs.NoLinks(source); err != nil {
		return "", "", err
	}
	if err := safefs.NoLinks(target); err != nil {
		return "", "", err
	}
	same := source == target
	if runtime.GOOS == "windows" {
		same = strings.EqualFold(source, target)
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", "", err
	}
	if existing, err := os.Stat(target); err == nil && os.SameFile(info, existing) {
		same = true
	}
	if same || safefs.Within(target, source) || info.IsDir() && safefs.Within(source, target) {
		return "", "", errors.New("source and destination must be different, non-overlapping paths")
	}
	return source, target, nil
}

func copySingle(ctx context.Context, source, target, style string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := safefs.NoLinks(source); err != nil {
		return err
	}
	if err := safefs.NoLinks(target); err != nil {
		return err
	}
	if info, err := os.Lstat(source); err != nil {
		return err
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", source)
	}
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", source)
	}
	if dst, err := os.Stat(target); err == nil {
		if os.SameFile(info, dst) {
			return errors.New("source and destination are the same file")
		}
		if style == "skip" {
			return nil
		}
	}
	if parent, err := os.Stat(filepath.Dir(target)); err != nil {
		return err
	} else if !parent.IsDir() {
		return errors.New("destination parent is not a directory")
	}
	if style != "skip" {
		return safefs.WriteAtomicReaderContext(ctx, target, copyReader{ctx, src}, info.Mode().Perm())
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".casaos-copy-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := io.Copy(f, copyReader{ctx, src}); err != nil {
		return err
	}
	if err := f.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return safefs.NoReplace(f.Name(), target)
}

func CopyDirContext(ctx context.Context, source, parent, style string) error {
	source, target, err := ValidateCopyTarget(source, parent)
	if err != nil {
		return err
	}
	return copyTree(ctx, source, target, style, false)
}

func copyTree(ctx context.Context, source, target, style string, staged bool) error {
	if InternalStoragePath(source) {
		return errors.New("internal storage cannot be copied")
	}
	if !staged {
		if _, err := ValidateUserPath(target); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := safefs.NoLinks(source); err != nil {
		return err
	}
	if err := safefs.NoLinks(target); err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copySingle(ctx, source, target, style)
	}
	if _, err := os.Lstat(target); err == nil && style == "skip" {
		return nil
	}
	if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyTree(ctx, filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name()), style, staged); err != nil {
			return err
		}
	}
	return nil
}

// MovePathContext renames directly when the destination is absent. Replacements
// and cross-volume moves stage a full copy before removing any existing data.
func MovePathContext(ctx context.Context, source, parent, style string) error {
	if _, err := ValidateDeletePath(source); err != nil {
		return err
	}
	source, target, err := ValidateCopyTarget(source, parent)
	if err != nil {
		return err
	}
	if _, err := ValidateDeletePath(target); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Lstat(target); err == nil {
		if style == "skip" {
			return nil
		}
	} else if os.IsNotExist(err) {
		// Keep the copy path's restrictions on nested links and special files.
		if err := validateMoveTree(ctx, source); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := safefs.NoReplace(source, target); err == nil {
			return nil
		}
		// Rename cannot cross volumes. Fall back without removing the source.
	} else {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".casaos-move-*")
	if err != nil {
		return err
	}
	// Allocated by MkdirTemp within the validated destination parent.
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stage)
		}
	}()
	staged := filepath.Join(stage, "new")
	if err := copyTree(ctx, source, staged, "overwrite", true); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	backup := filepath.Join(stage, "previous")
	hadTarget := false
	if _, err := os.Lstat(target); err == nil {
		if style == "skip" {
			return nil
		}
		if err := safefs.NoLinks(target); err != nil {
			return err
		}
		if err := safefs.NoReplace(target, backup); err != nil {
			return err
		}
		hadTarget = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := safefs.NoReplace(staged, target); err != nil {
		if hadTarget {
			if rollback := safefs.NoReplace(backup, target); rollback != nil {
				// Retain the previous destination for recovery if it cannot be restored.
				cleanup = false
				return errors.Join(err, fmt.Errorf("previous destination retained at %s: %w", backup, rollback))
			}
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.RemoveAll(source)
}

func validateMoveTree(ctx context.Context, source string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if InternalStoragePath(path) {
			return errors.New("internal storage cannot be moved")
		}
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := safefs.NoLinks(path); err != nil {
			return err
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return fmt.Errorf("not a regular file: %s", path)
		}
		return nil
	})
}
