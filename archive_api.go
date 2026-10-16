package main

import (
	"archive/zip"
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
	if err := decodeAPI(w, r, &req); err != nil || len(req.Files) == 0 {
		apiError(w, 400, errors.New("files are required"))
		return
	}
	if len(req.Files) > 10000 {
		apiError(w, 400, errors.New("too many source paths"))
		return
	}
	output := req.Output
	if output == "" {
		first := req.Files[0]
		name := fileutil.ArchiveBase(first)
		output = filepath.Join(filepath.Dir(first), strings.TrimSuffix(name, filepath.Ext(name))+".zip")
	}
	output, err := filepath.Abs(output)
	if err != nil {
		apiError(w, 400, err)
		return
	}
	if runtime.GOOS == "windows" && strings.Contains(filepath.Base(output), ":") {
		apiError(w, 400, errors.New("archive output cannot use an NTFS alternate data stream"))
		return
	}
	// Never truncate a source, an existing archive, or a destination reached through a link.
	if err := safefs.NoLinks(output); err != nil {
		apiError(w, 400, err)
		return
	}
	for i, source := range req.Files {
		source, err = filepath.Abs(source)
		if err != nil {
			apiError(w, 400, err)
			return
		}
		if sameFilePath(source, output) {
			apiError(w, 409, errors.New("archive output must differ from every source"))
			return
		}
		if _, err := os.Lstat(source); err != nil {
			apiError(w, 400, err)
			return
		}
		req.Files[i] = source
	}
	if _, err := os.Lstat(output); err == nil {
		apiError(w, 409, errors.New("output already exists"))
		return
	} else if !os.IsNotExist(err) {
		apiError(w, 500, err)
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
	seen := map[string]bool{}
	for _, source := range req.Files {
		base := filepath.Dir(source)
		err = filepath.Walk(source, func(p string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if sameFilePath(p, staged.Name()) || sameFilePath(p, output) {
				return nil
			}
			if fileutil.InternalStoragePath(p) {
				if sameFilePath(p, source) {
					return errors.New("internal storage cannot be archived")
				}
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if err := safefs.NoLinks(p); err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported source: %s", p)
			}
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(rel)
			if fileutil.IsFilesystemRoot(source) {
				name = filepath.ToSlash(filepath.Join(fileutil.ArchiveBase(source), rel))
			}
			key := name
			if runtime.GOOS == "windows" {
				key = strings.ToLower(key)
			}
			if seen[key] {
				return fmt.Errorf("duplicate archive entry: %s", name)
			}
			seen[key] = true
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			header.Name = name
			if info.IsDir() {
				header.Name += "/"
			} else {
				header.Method = zip.Deflate
			}
			writer, err := archive.CreateHeader(header)
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(writer, f)
			return errors.Join(copyErr, f.Close())
		})
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
	dest, err = filepath.Abs(dest)
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
		if err := extractZipFile(f, target); err != nil {
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
	src, err := entry.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(dst, io.LimitReader(src, int64(entry.UncompressedSize64)+1))
	if copyErr == nil && uint64(n) != entry.UncompressedSize64 {
		copyErr = errors.New("archive entry size mismatch")
	}
	syncErr := dst.Sync()
	return errors.Join(copyErr, syncErr, dst.Close())
}
