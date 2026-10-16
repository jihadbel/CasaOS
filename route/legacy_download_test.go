package route

import (
	"bytes"
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/localauth"
)

func TestLegacyDownloadSharesValidationAndSecurityHeaders(t *testing.T) {
	root := t.TempDir()
	old := config.AppInfo.UserDataPath
	config.AppInfo.UserDataPath = filepath.Join(root, "conf")
	t.Cleanup(func() { config.AppInfo.UserDataPath = old })
	auth, err := localauth.New(filepath.Join(config.AppInfo.UserDataPath, "auth"))
	if err != nil {
		t.Fatal(err)
	}
	handler := auth.Middleware(InitFile())
	password, err := os.ReadFile(filepath.Join(config.AppInfo.UserDataPath, "auth", "initial-password.txt"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"password": strings.TrimSpace(string(password))})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/session", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]

	for _, entry := range []struct {
		name   string
		status int
	}{
		{"صورة العائلة.jpg", 206}, {"attack.html", 206}, {"attack.svg", 206},
		{".temp/partial", 400}, {".CasaOS-RecycleBin/deleted", 400}, {".casaos-upload-part", 400},
		{".TEMP/partial", 400},
		{"conf/storage-paths.json", 400}, {"conf/auth/password.hash", 400},
	} {
		t.Run(entry.name, func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(entry.name))
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("private contents"), 0600); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("GET", "/v3/file?path="+url.QueryEscape(path), nil)
			req.AddCookie(cookie)
			req.Header.Set("Range", "bytes=0-3")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != entry.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if entry.status == 400 {
				if strings.Contains(w.Body.String(), "private contents") {
					t.Fatal("internal file leaked")
				}
				return
			}
			disposition, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
			if err != nil || disposition != "attachment" || params["filename"] != filepath.Base(path) {
				t.Fatal(w.Header(), err)
			}
			if w.Body.String() != "priv" || w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
				t.Fatal(w.Body.String(), w.Header())
			}
		})
	}
	unauth := httptest.NewRecorder()
	handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/v3/file?path="+url.QueryEscape(filepath.Join(root, "attack.html")), nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatal("legacy download bypassed authentication", unauth.Code)
	}
}
