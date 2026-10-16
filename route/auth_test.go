package route

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestForwardedLoopbackCannotBypassAuthentication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(path, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"127.0.0.1:12345", "192.0.2.12:12345"} {
		for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
			for _, entry := range []struct {
				handler http.Handler
				path    string
			}{
				{InitV1Router(), "/v1/file?path=" + url.QueryEscape(path)},
				{InitV2Router(), V2APIPath + "/health/ports"},
			} {
				r := httptest.NewRequest("GET", entry.path, nil)
				r.RemoteAddr = address
				r.Header.Set(header, "127.0.0.1")
				w := httptest.NewRecorder()
				entry.handler.ServeHTTP(w, r)
				if w.Code != http.StatusUnauthorized {
					t.Errorf("%s %s with %s: got %d %s", address, header, entry.path, w.Code, w.Body.String())
				}
			}
		}
	}
}

func TestExtensionsRequireIdentityOnEveryPlatform(t *testing.T) {
	called := false
	handler := AuthenticateExtensions(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
	for _, path := range []string{"/api/zip", "/api/unzip", "/api/trash/move", "/api/settings", "/api/media/timeline"} {
		r := httptest.NewRequest("POST", path, nil)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 || called {
			t.Fatalf("unprotected extension: %s %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/ping", nil))
	if w.Code != 204 || !called {
		t.Fatal("non-extension routing changed")
	}
}
