package file

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/mholt/archiver/v3"
)

type ArchiveEntry struct {
	Path, Name string
	Info       os.FileInfo
}

// CollectArchiveEntries validates the complete selection before sending headers
// or creating output, sharing one entry and byte budget across all roots.
func CollectArchiveEntries(ctx context.Context, roots []string, limit int) ([]ArchiveEntry, error) {
	var entries []ArchiveEntry
	seen := map[string]bool{}
	var bytes int64
	visited := 0
	for _, raw := range roots {
		root, err := ValidateUserPath(raw)
		if err != nil {
			return nil, err
		}
		base := filepath.Dir(root)
		err = WalkTree(ctx, root, limit-visited, func(path string, info os.FileInfo) error {
			visited++
			if InternalStoragePath(path) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if err := safefs.NoLinks(path); err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported archive entry: %s", path)
			}
			if !info.IsDir() {
				if info.Size() < 0 || info.Size() > MaxArchiveBytes-bytes {
					return fmt.Errorf("archive exceeds %d bytes", MaxArchiveBytes)
				}
				bytes += info.Size()
			}
			name, err := filepath.Rel(base, path)
			if err != nil {
				return err
			}
			name = filepath.ToSlash(name)
			key := name
			if runtime.GOOS == "windows" {
				key = strings.ToLower(key)
			}
			if seen[key] {
				return fmt.Errorf("duplicate archive entry: %s", name)
			}
			seen[key] = true
			entries = append(entries, ArchiveEntry{path, name, info})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return entries, nil
}

type archiveReadCloser struct {
	io.Reader
	io.Closer
}

func WriteArchiveEntries(ctx context.Context, archive archiver.Writer, entries []ArchiveEntry) error {
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := safefs.NoLinks(entry.Path); err != nil {
			return err
		}
		input, err := os.Open(entry.Path)
		if err != nil {
			return err
		}
		info, err := input.Stat()
		if err != nil {
			input.Close()
			return err
		}
		if !os.SameFile(info, entry.Info) || info.Size() != entry.Info.Size() || info.Mode() != entry.Info.Mode() {
			input.Close()
			return fmt.Errorf("archive source changed: %s", entry.Path)
		}
		reader := &io.LimitedReader{R: ContextReader{ctx, input}, N: info.Size()}
		err = archive.Write(archiver.File{FileInfo: archiver.FileInfo{FileInfo: info, CustomName: entry.Name}, ReadCloser: archiveReadCloser{reader, input}})
		closeErr := input.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if !info.IsDir() && reader.N != 0 {
			return fmt.Errorf("archive source changed: %s", entry.Path)
		}
	}
	return ctx.Err()
}
