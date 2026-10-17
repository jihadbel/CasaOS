package v1

import (
	"archive/zip"
	"context"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
	"github.com/labstack/echo/v4"
)

func downloadDirectory(ctx echo.Context, root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return echo.NewHTTPError(400, "Invalid folder path")
	}
	if filepath.Dir(root) == root {
		return echo.NewHTTPError(400, "Filesystem roots cannot be downloaded")
	}
	type entry struct {
		path, name string
		info       os.FileInfo
	}
	var entries []entry
	var totalBytes int64
	base := file.ArchiveBase(root)
	err = walkDownloadTree(ctx.Request().Context(), root, 10000, func(path string, info os.FileInfo) error {
		if err := ctx.Request().Context().Err(); err != nil {
			return err
		}
		internal := file.InternalStoragePath(path)
		if internal {
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
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if info.Size() < 0 || info.Size() > file.MaxArchiveBytes-totalBytes {
				return fmt.Errorf("folder exceeds archive byte limit")
			}
			totalBytes += info.Size()
		}
		entries = append(entries, entry{path, filepath.ToSlash(filepath.Join(base, rel)), info})
		return nil
	})
	if err != nil {
		return echo.NewHTTPError(400, "Folder cannot be archived: "+err.Error())
	}
	if len(entries) == 0 {
		return echo.NewHTTPError(400, "Internal storage cannot be downloaded")
	}
	headers := ctx.Response().Header()
	headers.Set("Content-Type", "application/zip")
	headers.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": base + ".zip"}))
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	headers.Set("Cache-Control", "no-store")
	if ctx.Request().Method == http.MethodHead {
		return ctx.NoContent(http.StatusOK)
	}
	archive := zip.NewWriter(ctx.Response())
	for _, entry := range entries {
		if err := ctx.Request().Context().Err(); err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(entry.info)
		if err != nil {
			return err
		}
		header.Name = entry.name
		if entry.info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		if entry.info.IsDir() {
			continue
		}
		if err := safefs.NoLinks(entry.path); err != nil {
			return err
		}
		input, err := os.Open(entry.path)
		if err != nil {
			return err
		}
		copyErr := file.CopyArchiveFile(ctx.Request().Context(), writer, input, entry.info.Size())
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return archive.Close()
}

// ReadDir in batches also bounds memory for a single enormous directory.
func walkDownloadTree(ctx context.Context, root string, limit int, visit func(string, os.FileInfo) error) error {
	return file.WalkTree(ctx, root, limit, visit)
}
