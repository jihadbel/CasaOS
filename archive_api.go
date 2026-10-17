package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	fileutil "github.com/IceWhaleTech/CasaOS/pkg/utils/file"
)

func apiJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func apiError(w http.ResponseWriter, status int, err error) {
	apiJSON(w, status, map[string]interface{}{"success": false, "error": err.Error()})
}

func decodeAPI(w http.ResponseWriter, r *http.Request, value interface{}) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(value)
}

type ZipRequest struct {
	Files  []string `json:"files"`
	Output string   `json:"output"`
}

func handleZip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, 405, errors.New("method not allowed"))
		return
	}
	var req ZipRequest
	if err := decodeAPI(w, r, &req); err != nil || len(req.Files) == 0 || len(req.Files) > 10000 {
		apiError(w, 400, errors.New("between 1 and 10000 source paths are required"))
		return
	}
	output := req.Output
	if output == "" {
		first := req.Files[0]
		name := fileutil.ArchiveBase(first)
		output = filepath.Join(filepath.Dir(first), strings.TrimSuffix(name, filepath.Ext(name))+".zip")
	}
	output, err := fileutil.ValidateUserPath(output)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	for _, source := range req.Files {
		if sameFilePath(source, output) {
			apiError(w, 409, errors.New("archive output must differ from every source"))
			return
		}
	}
	if _, err := os.Lstat(output); err == nil {
		apiError(w, 409, errors.New("output already exists"))
		return
	} else if !os.IsNotExist(err) {
		apiError(w, 500, err)
		return
	}
	entries, err := fileutil.CollectArchiveEntries(r.Context(), req.Files, 100000)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		apiError(w, 500, err)
		return
	}
	staged, err := os.CreateTemp(filepath.Dir(output), ".casaos-archive-*.zip")
	if err != nil {
		apiError(w, 500, err)
		return
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	archive := zip.NewWriter(staged)
	for _, entry := range entries {
		if err := r.Context().Err(); err != nil {
			_ = archive.Close()
			apiError(w, 400, err)
			return
		}
		header, err := zip.FileInfoHeader(entry.Info)
		if err != nil {
			_ = archive.Close()
			apiError(w, 500, err)
			return
		}
		header.Name = entry.Name
		if entry.Info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			_ = archive.Close()
			apiError(w, 500, err)
			return
		}
		if entry.Info.IsDir() {
			continue
		}
		if err := safefs.NoLinks(entry.Path); err != nil {
			_ = archive.Close()
			apiError(w, 400, err)
			return
		}
		input, err := os.Open(entry.Path)
		if err != nil {
			_ = archive.Close()
			apiError(w, 500, err)
			return
		}
		copyErr := fileutil.CopyArchiveFile(r.Context(), writer, input, entry.Info.Size())
		err = errors.Join(copyErr, input.Close())
		if err != nil {
			_ = archive.Close()
			apiError(w, 500, err)
			return
		}
	}
	if err := archive.Close(); err != nil {
		apiError(w, 500, err)
		return
	}
	if err := staged.Sync(); err != nil {
		apiError(w, 500, err)
		return
	}
	if err := staged.Close(); err != nil {
		apiError(w, 500, err)
		return
	}
	if err := r.Context().Err(); err != nil {
		apiError(w, 400, err)
		return
	}
	if err := safefs.NoReplace(staged.Name(), output); err != nil {
		apiError(w, 409, err)
		return
	}
	apiJSON(w, 200, map[string]interface{}{"success": true, "output": filepath.ToSlash(output)})
}

func sameFilePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

type UnzipRequest struct {
	Path string `json:"path"`
	Dest string `json:"dest"`
}

// zipRelative rejects drive letters, ADS, device names, absolute paths and traversal.
func zipRelative(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimSuffix(name, "/")
	rel := filepath.FromSlash(name)
	if !filepath.IsLocal(rel) || rel == "." || strings.Contains(name, ":") {
		return "", fmt.Errorf("unsafe archive entry: %s", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "" || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return "", fmt.Errorf("unsafe archive entry: %s", name)
		}
	}
	return rel, nil
}

func handleUnzip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apiError(w, 405, errors.New("method not allowed"))
		return
	}
	var req UnzipRequest
	if err := decodeAPI(w, r, &req); err != nil || req.Path == "" {
		apiError(w, 400, errors.New("archive path is required"))
		return
	}
	if _, err := fileutil.ValidateUserPath(req.Path); err != nil {
		apiError(w, 400, err)
		return
	}
	reader, err := zip.OpenReader(req.Path)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	defer reader.Close()
	dest := req.Dest
	if dest == "" {
		dest = filepath.Join(filepath.Dir(req.Path), strings.TrimSuffix(filepath.Base(req.Path), filepath.Ext(req.Path)))
	}
	dest, err = fileutil.ValidateUserPath(dest)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	if err := safefs.NoLinks(dest); err != nil {
		apiError(w, 400, err)
		return
	}
	if _, err := os.Lstat(dest); err == nil {
		apiError(w, 409, errors.New("extraction destination already exists"))
		return
	} else if !os.IsNotExist(err) {
		apiError(w, 500, err)
		return
	}
	const maxExpanded uint64 = 100 << 30
	var expanded uint64
	if len(reader.File) > 100000 {
		apiError(w, 400, errors.New("archive has too many entries"))
		return
	}
	names := make([]string, len(reader.File))
	seen := map[string]bool{}
	// Validate every entry before extracting anything.
	for i, f := range reader.File {
		if err := r.Context().Err(); err != nil {
			apiError(w, 400, err)
			return
		}
		rel, err := zipRelative(f.Name)
		if err != nil {
			apiError(w, 400, err)
			return
		}
		if !f.FileInfo().IsDir() && !f.Mode().IsRegular() {
			apiError(w, 400, errors.New("links and special files are not supported"))
			return
		}
		key := rel
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] {
			apiError(w, 400, errors.New("duplicate archive entry"))
			return
		}
		seen[key] = true
		if f.UncompressedSize64 > maxExpanded-expanded {
			apiError(w, 400, errors.New("archive exceeds 100 GiB extraction limit"))
			return
		}
		expanded += f.UncompressedSize64
		if _, err := fileutil.ValidateUserPath(filepath.Join(dest, rel)); err != nil {
			apiError(w, 400, err)
			return
		}
		names[i] = rel
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		apiError(w, 500, err)
		return
	}
	stage, err := os.MkdirTemp(filepath.Dir(dest), ".casaos-extract-")
	if err != nil {
		apiError(w, 500, err)
		return
	}
	// stage is a newly allocated absolute directory; no caller controls its path.
	defer os.RemoveAll(stage)
	for i, f := range reader.File {
		target := filepath.Join(stage, names[i])
		if !safefs.Within(stage, target) {
			apiError(w, 400, errors.New("unsafe extraction path"))
			return
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				apiError(w, 500, err)
				return
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			apiError(w, 500, err)
			return
		}
		if err := extractZipFileContext(r.Context(), f, target); err != nil {
			apiError(w, 500, err)
			return
		}
	}
	if err := safefs.NoReplace(stage, dest); err != nil {
		apiError(w, 409, err)
		return
	}
	apiJSON(w, 200, map[string]interface{}{"success": true, "dest": filepath.ToSlash(dest)})
}

func extractZipFile(entry *zip.File, target string) error {
	return extractZipFileContext(context.Background(), entry, target)
}
func extractZipFileContext(ctx context.Context, entry *zip.File, target string) error {
	src, err := entry.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(dst, fileutil.ContextReader{Context: ctx, Reader: io.LimitReader(src, int64(entry.UncompressedSize64)+1)})
	if copyErr == nil && uint64(n) != entry.UncompressedSize64 {
		copyErr = errors.New("archive entry size mismatch")
	}
	syncErr := dst.Sync()
	return errors.Join(copyErr, syncErr, dst.Close())
}
