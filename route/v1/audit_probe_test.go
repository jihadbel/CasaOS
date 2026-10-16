package v1

import (
	"archive/zip"
	"bytes"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestAuditBatchSingleDownloadDoesNotAppendArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.txt")
	want := []byte("original file bytes")
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/v1/batch?format=zip&files="+url.QueryEscape(path), nil)
	w := httptest.NewRecorder()
	if err := GetDownloadFile(echo.New().NewContext(r, w)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.Body.Bytes(), want) {
		t.Errorf("download contains %d bytes instead of %d: archive appended to original file", w.Body.Len(), len(want))
	}
}

func TestAuditOperationRejectsWindowsBackslashPathWithoutPanic(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/batch/task", strings.NewReader(`{"type":"copy","to":"C:\\target","item":[{"from":"C:\\source.txt"}]}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("native Windows path panics before validation: %v", p)
		}
	}()
	if err := PostOperateFileOrDir(echo.New().NewContext(r, w)); err != nil {
		t.Fatal(err)
	}
}

func TestBatchFolderDownloadExcludesInternalFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CASAOS_DATA_ROOT", root)
	for _, relative := range []string{"visible.txt", "RecycleBin/deleted.txt", ".temp/part.txt"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(relative), 0600); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/batch?format=zip&files="+url.QueryEscape(root), nil)
	if err := GetDownloadFile(echo.New().NewContext(r, w)); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 1 || z.File[0].Name != "visible.txt" {
		t.Fatalf("unexpected export: %+v", z.File)
	}
}
