package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const MaxArchiveBytes int64 = 100 << 30

// WalkTree bounds directory buffers, tree depth and work between cancellations.
func WalkTree(ctx context.Context, root string, limit int, visit func(string, os.FileInfo) error) error {
	visited := 0
	var walk func(string, os.FileInfo, int) error
	walk = func(path string, info os.FileInfo, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if visited >= limit || depth > 128 {
			return fmt.Errorf("tree exceeds limits (maximum %d entries, depth 128)", limit)
		}
		visited++
		if err := visit(path, info); err != nil {
			if errors.Is(err, filepath.SkipDir) && info.IsDir() {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			return nil
		}
		folder, err := os.Open(path)
		if err != nil {
			return err
		}
		defer folder.Close()
		for {
			entries, readErr := folder.ReadDir(128)
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				if visited >= limit {
					return fmt.Errorf("tree exceeds entry limit %d", limit)
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if err := walk(filepath.Join(path, entry.Name()), info, depth+1); err != nil {
					return err
				}
			}
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	return walk(root, info, 0)
}

type ContextReader struct {
	Context context.Context
	Reader  io.Reader
}

func (r ContextReader) Read(p []byte) (int, error) {
	if err := r.Context.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// CopyArchiveFile detects a source growing after validation and bounds actual I/O.
func CopyArchiveFile(ctx context.Context, dst io.Writer, src io.Reader, size int64) error {
	n, err := io.Copy(dst, ContextReader{ctx, io.LimitReader(src, size+1)})
	if err != nil {
		return err
	}
	if n != size {
		return errors.New("archive source changed during reading")
	}
	return ctx.Err()
}
