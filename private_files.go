package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/internal/textfile"
	"github.com/IceWhaleTech/CasaOS/model"
	"github.com/IceWhaleTech/CasaOS/pkg/localauth"
	"github.com/IceWhaleTech/CasaOS/pkg/storagepaths"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
)

// The standalone server exposes only file operations authorized for this home.
// Admin accounts manage accounts, but use the same file boundary as everyone.
func privateFilesHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Add("Vary", "Cookie")
		if r.URL.Path == "/" {
			next.ServeHTTP(w, r)
			return
		}
		u, ok := localauth.CurrentUser(r)
		if !ok || u.Home == "" {
			apiError(w, 403, errors.New("a private user session is required"))
			return
		}
		if r.URL.Path == "/api/settings" {
			handlePrivateSettings(w, r, u)
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()
		if err := authorizeFileRequest(w, r, u); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				apiError(w, http.StatusRequestEntityTooLarge, errors.New("request is too large"))
				return
			}
			apiError(w, 403, errors.New("operation is outside your private storage or is unavailable"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func scopedUserPath(u localauth.Identity, raw string, allowHome bool) (string, error) {
	if raw == "/" || raw == "/drives" || raw == "/DATA" {
		raw = u.Home
	} else if strings.HasPrefix(raw, "/DATA/") {
		raw = filepath.Join(u.Home, filepath.FromSlash(strings.TrimPrefix(raw, "/DATA/")))
	}
	if !filepath.IsAbs(raw) {
		return "", errors.New("absolute user path required")
	}
	clean := filepath.Clean(raw)
	if !safefs.Within(u.Home, clean) && !(allowHome && sameFilePath(u.Home, clean)) {
		return "", errors.New("file belongs to another storage area")
	}
	if _, err := file.ValidateUserPath(clean); err != nil {
		return "", err
	}
	return clean, nil
}

func authorizeFileRequest(w http.ResponseWriter, r *http.Request, u localauth.Identity) error {
	denied := errors.New("unavailable file operation")
	p := r.URL.Path
	method := r.Method
	get := method == http.MethodGet || method == http.MethodHead
	q := r.URL.Query()
	queryPath := func(name string, allowHome bool) error {
		values := q[name]
		if len(values) != 1 {
			return denied
		}
		clean, err := scopedUserPath(u, values[0], allowHome)
		if err != nil {
			return err
		}
		q.Set(name, clean)
		return nil
	}
	commitQuery := func() { r.URL.RawQuery = q.Encode(); r.RequestURI = r.URL.RequestURI() }
	if get {
		switch p {
		case "/api/server-info", "/v1/sys/utilization":
			return nil
		case "/api/trash", "/api/trash/":
			return nil
		case "/api/media/timeline":
			if _, ok := q["folder"]; ok {
				if err := queryPath("folder", true); err != nil {
					return err
				}
			}
			commitQuery()
			return nil
		case "/v1/folder", "/v1/file", "/v1/file/content", "/v1/image", "/v3/file":
			if err := queryPath("path", p != "/v1/file/content"); err != nil {
				return err
			}
			commitQuery()
			return nil
		case "/v1/file/upload":
			if err := queryPath("path", true); err != nil {
				return err
			}
			commitQuery()
			return nil
		case "/v1/batch":
			if len(q["files"]) != 1 {
				return denied
			}
			paths := strings.Split(q.Get("files"), ",")
			for i, raw := range paths {
				clean, err := scopedUserPath(u, raw, true)
				if err != nil {
					return err
				}
				paths[i] = clean
			}
			q.Set("files", strings.Join(paths, ","))
			commitQuery()
			return nil
		}
		return denied
	}
	// JSON is decoded and re-encoded as the exact downstream type, preventing
	// duplicate fields, mixed-case keys and Echo query binding from bypassing us.
	if r.URL.RawQuery != "" {
		return denied
	}
	if p == "/v1/file/upload" && method == http.MethodPost {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			return err
		}
		if r.MultipartForm == nil {
			return denied
		}
		// The outer gateway owns cleanup, after the upload handler has read any
		// disk-spooled file. Authorization must not remove it prematurely.
		if len(r.MultipartForm.Value["path"]) != 1 {
			return denied
		}
		clean, err := scopedUserPath(u, r.MultipartForm.Value["path"][0], true)
		if err != nil {
			return err
		}
		r.MultipartForm.Value["path"] = []string{clean}
		r.Form.Set("path", clean)
		r.PostForm.Set("path", clean)
		return nil
	}
	decode := func(value interface{}) error {
		limit := int64(24 << 20)
		if p == "/v1/file" && method == http.MethodPut {
			limit = textfile.MaxEditorRequestBytes
		}
		if r.ContentLength > limit {
			return &http.MaxBytesError{Limit: limit}
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
		if err := decoder.Decode(value); err != nil {
			return err
		}
		var extra interface{}
		if err := decoder.Decode(&extra); err != io.EOF {
			if err != nil {
				return err
			}
			return denied
		}
		return nil
	}
	replace := func(value interface{}) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
		r.ContentLength = int64(len(data))
		r.Header.Set("Content-Type", "application/json")
		return nil
	}
	if p == "/v1/file" && method == http.MethodPut {
		var body model.FileUpdate
		if err := decode(&body); err != nil {
			return err
		}
		clean, err := scopedUserPath(u, body.FilePath, false)
		if err != nil {
			return err
		}
		body.FilePath = clean
		return replace(body)
	}
	if (p == "/v1/file" || p == "/v1/folder" || p == "/api/trash/move") && method == http.MethodPost {
		var body map[string]string
		if err := decode(&body); err != nil {
			return err
		}
		clean, err := scopedUserPath(u, body["path"], false)
		if err != nil {
			return err
		}
		body["path"] = clean
		return replace(body)
	}
	if (p == "/v1/file/name" || p == "/v1/folder/name") && method == http.MethodPut {
		var body map[string]string
		if err := decode(&body); err != nil {
			return err
		}
		for _, key := range []string{"old_path", "new_path"} {
			clean, err := scopedUserPath(u, body[key], false)
			if err != nil {
				return err
			}
			body[key] = clean
		}
		return replace(body)
	}
	if p == "/v1/batch" && method == http.MethodDelete {
		var paths []string
		if err := decode(&paths); err != nil {
			return err
		}
		if len(paths) == 0 {
			return denied
		}
		for i, raw := range paths {
			clean, err := scopedUserPath(u, raw, false)
			if err != nil {
				return err
			}
			paths[i] = clean
		}
		return replace(paths)
	}
	if p == "/v1/batch/task" && method == http.MethodPost {
		var body model.FileOperate
		if err := decode(&body); err != nil {
			return err
		}
		clean, err := scopedUserPath(u, body.To, true)
		if err != nil {
			return err
		}
		body.To = clean
		for i, item := range body.Item {
			source, err := scopedUserPath(u, item.From, false)
			if err != nil {
				return err
			}
			body.Item[i].From = source
		}
		return replace(body)
	}
	if p == "/api/zip" && method == http.MethodPost {
		var body ZipRequest
		if err := decode(&body); err != nil {
			return err
		}
		for i, raw := range body.Files {
			clean, err := scopedUserPath(u, raw, true)
			if err != nil {
				return err
			}
			body.Files[i] = clean
		}
		if body.Output == "" && len(body.Files) > 0 && sameFilePath(body.Files[0], u.Home) {
			body.Output = filepath.Join(u.Home, "my-files.zip")
		}
		if body.Output != "" {
			clean, err := scopedUserPath(u, body.Output, false)
			if err != nil {
				return err
			}
			body.Output = clean
		}
		return replace(body)
	}
	if p == "/api/unzip" && method == http.MethodPost {
		var body UnzipRequest
		if err := decode(&body); err != nil {
			return err
		}
		clean, err := scopedUserPath(u, body.Path, false)
		if err != nil {
			return err
		}
		body.Path = clean
		if body.Dest != "" {
			clean, err := scopedUserPath(u, body.Dest, false)
			if err != nil {
				return err
			}
			body.Dest = clean
		}
		return replace(body)
	}
	if (p == "/api/trash/empty" || p == "/api/trash/restore" || p == "/api/trash/delete") && method == http.MethodPost {
		return nil
	}
	return denied
}

func settingsForHome(home string) StorageSettings {
	return StorageSettings{SmartAutoSort: true, AppsPath: filepath.Join(home, "Applications"), ArchivesPath: filepath.Join(home, "Archives"), PhotosPath: filepath.Join(home, "Photos"), VideosPath: filepath.Join(home, "Videos"), DocumentsPath: filepath.Join(home, "Documents"), MusicPath: filepath.Join(home, "Music"), MobilePath: filepath.Join(home, "Mobile_Uploads"), DefaultPath: home}
}

func handlePrivateSettings(w http.ResponseWriter, r *http.Request, u localauth.Identity) {
	settings, err := readPrivateSettings(u)
	if err != nil {
		apiError(w, 500, err)
		return
	}
	if r.Method == http.MethodPut {
		var requested storagepaths.Settings
		if err := decodeAPI(w, r, &requested); err != nil {
			apiError(w, 400, err)
			return
		}
		for _, path := range requested.Roots() {
			if _, err := scopedUserPath(u, path, true); err != nil {
				apiError(w, 403, errors.New("storage settings must stay inside your own home"))
				return
			}
		}
		if !sameFilePath(requested.DefaultPath, u.Home) {
			apiError(w, 400, errors.New("your private storage root cannot be changed"))
			return
		}
		settings = requested
		settings.DefaultPath = u.Home
		data, err := json.Marshal(settings)
		if err == nil {
			err = safefs.WriteAtomic(filepath.Join(u.Home, ".casaos-settings.json"), data, 0600)
		}
		if err != nil {
			apiError(w, 500, err)
			return
		}
	} else if r.Method != http.MethodGet {
		apiError(w, 405, errors.New("method not allowed"))
		return
	}
	apiJSON(w, 200, map[string]interface{}{"success": true, "configured": true, "privateStorage": true, "settings": settings})
}

func readPrivateSettings(u localauth.Identity) (StorageSettings, error) {
	s := settingsForHome(u.Home)
	path := filepath.Join(u.Home, ".casaos-settings.json")
	if err := safefs.NoLinks(path); err != nil {
		return s, err
	}
	input, err := os.Open(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	defer input.Close()
	if err := json.NewDecoder(io.LimitReader(input, 1<<20)).Decode(&s); err != nil {
		return s, err
	}
	if !sameFilePath(s.DefaultPath, u.Home) {
		return s, errors.New("invalid private storage settings")
	}
	for _, path := range s.Roots() {
		if _, err := scopedUserPath(u, path, true); err != nil {
			return s, err
		}
	}
	return s, nil
}
