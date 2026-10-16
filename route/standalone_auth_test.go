package route

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/IceWhaleTech/CasaOS/pkg/localauth"
)

func TestWindowsRoutesFailClosedAndAcceptOnlyAuthenticatedContext(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows standalone authentication")
	}
	root := t.TempDir()
	private := filepath.Join(root, "private.txt")
	if err := os.WriteFile(private, []byte("private data"), 0600); err != nil {
		t.Fatal(err)
	}
	router := InitV1Router()
	for _, handler := range []http.Handler{router, InitFile()} {
		req := httptest.NewRequest("GET", "/v1/file?path="+url.QueryEscape(private), nil)
		req.RemoteAddr = "192.0.2.12:12345"
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code < 400 {
			t.Fatal("missing user service permitted a file read", w.Code)
		}
	}
	authDir := filepath.Join(root, "auth")
	auth, err := localauth.New(authDir)
	if err != nil {
		t.Fatal(err)
	}
	handler := auth.Middleware(router)
	password, err := os.ReadFile(filepath.Join(authDir, "initial-password.txt"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"password": strings.TrimSpace(string(password))})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/session", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	req := httptest.NewRequest("GET", "/v1/file?path="+url.QueryEscape(private), nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != "private data" {
		t.Fatal(w.Code, w.Body.String())
	}
	update, _ := json.Marshal(map[string]string{"path": private, "content": "edited data"})
	req = httptest.NewRequest("PUT", "/v1/file", bytes.NewReader(update))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	data, err := os.ReadFile(private)
	if err != nil || string(data) != "edited data" {
		t.Fatal(string(data), err)
	}
}
