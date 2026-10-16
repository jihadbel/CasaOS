package v1

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestEditorRejectsLargeFilesWithoutChangingThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxEditorFileBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/file/content?path="+url.QueryEscape(path), nil)
	if err := GetFilerContent(echo.New().NewContext(r, w)); err != nil {
		t.Fatal(err)
	}
	if w.Code != 413 {
		t.Fatal(w.Code, w.Body.String())
	}
	body, _ := json.Marshal(map[string]string{"path": path, "content": "replacement"})
	r = httptest.NewRequest("PUT", "/v1/file", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	if err := PutFileContent(echo.New().NewContext(r, w)); err != nil {
		t.Fatal(err)
	}
	if w.Code != 413 {
		t.Fatal(w.Code, w.Body.String())
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != maxEditorFileBytes+1 {
		t.Fatal("large file was changed", info, err)
	}
}

func TestEditorRejectsOversizedRequestBeforeReading(t *testing.T) {
	r := httptest.NewRequest("PUT", "/v1/file", strings.NewReader("{}"))
	r.ContentLength = maxEditorRequestBytes + 1
	w := httptest.NewRecorder()
	if err := PutFileContent(echo.New().NewContext(r, w)); err != nil {
		t.Fatal(err)
	}
	if w.Code != 413 {
		t.Fatal(w.Code, w.Body.String())
	}
}
