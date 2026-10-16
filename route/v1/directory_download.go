package v1

import (
	"archive/zip"
	"fmt"
	"io"
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
	type entry struct {
		path, name string
		info       os.FileInfo
	}
	var entries []entry
	base := file.ArchiveBase(root)
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
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
		_, copyErr := io.Copy(writer, input)
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
