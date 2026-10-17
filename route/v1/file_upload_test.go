package v1

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
)

func testUpload(t *testing.T, dir, filename, relative, content string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"path": dir, "filename": filename, "relativePath": relative} {
		form.WriteField(key, value)
	}
	for key, value := range fields {
		form.WriteField(key, value)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/file/upload", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	if err := PostFileUpload(echo.New().NewContext(req, w)); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestUploadPublishesCompleteFileAndPreservesExistingFile(t *testing.T) {
	root := t.TempDir()
	response := testUpload(t, root, "file.txt", "file.txt", "original", nil)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	response = testUpload(t, root, "file.txt", "file.txt", "replacement", nil)
	data, err := os.ReadFile(filepath.Join(root, "file.txt"))
	if response.Code != 409 || err != nil || string(data) != "original" {
		t.Fatal(response.Code, string(data), err)
	}
}

func TestUploadRejectsRelativePathTraversal(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"../file.txt", `..\file.txt`, "nested/../../file.txt"} {
		response := testUpload(t, root, "file.txt", relative, "invalid", nil)
		if response.Code != 400 {
			t.Fatal(relative, response.Code, response.Body.String())
		}
	}
}

func TestChunkUploadPublishesAllChunksInOrder(t *testing.T) {
	root := t.TempDir()
	first := testUpload(t, root, "file.txt", "file.txt", "first", map[string]string{"totalChunks": "2", "chunkNumber": "1", "uploadId": "upload-1"})
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "file.txt")); !os.IsNotExist(err) {
		t.Fatal("incomplete upload was published")
	}
	second := testUpload(t, root, "file.txt", "file.txt", "second", map[string]string{"totalChunks": "2", "chunkNumber": "2", "uploadId": "upload-1"})
	data, err := os.ReadFile(filepath.Join(root, "file.txt"))
	if second.Code != 200 || err != nil || string(data) != "firstsecond" {
		t.Fatal(second.Code, string(data), err, second.Body.String())
	}
}

func TestChunkUploadSeparatesInterleavedTransfers(t *testing.T) {
	root := t.TempDir()
	for _, part := range []struct {
		id, number, content string
		status              int
	}{
		{"A", "1", "A-first-", 200}, {"B", "1", "B-first-", 200},
		{"A", "2", "A-second", 200}, {"B", "2", "B-second", 409},
	} {
		response := testUpload(t, root, "file.txt", "file.txt", part.content, map[string]string{"totalChunks": "2", "chunkNumber": part.number, "uploadId": part.id})
		if response.Code != part.status {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "file.txt")); err != nil || string(data) != "A-first-A-second" {
		t.Fatal("transfers mixed", string(data), err)
	}
}

func TestChunkUploadRequiresIdentityAndPreservesCompleteParts(t *testing.T) {
	root := t.TempDir()
	for _, fields := range []map[string]string{
		{"totalChunks": "2", "chunkNumber": "1"},
		{"totalChunks": "invalid", "chunkNumber": "1"},
	} {
		if w := testUpload(t, root, "file.txt", "file.txt", "bad", fields); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, part := range []struct {
		number, content string
		status          int
	}{
		{"01", "original-", 200}, {"1", "original-", 200}, {"1", "wrong-", 409}, {"2", "end", 200},
	} {
		w := testUpload(t, root, "file.txt", "nested/file.txt", part.content, map[string]string{"identifier": "transfer", "totalChunks": "2", "chunkNumber": part.number})
		if w.Code != part.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "nested", "file.txt")); err != nil || string(data) != "original-end" {
		t.Fatal(string(data), err)
	}
}

func TestChunkUploadProbeUsesSameTransferIdentityAndRelativePath(t *testing.T) {
	root := t.TempDir()
	w := testUpload(t, root, "file.txt", "nested/file.txt", "first", map[string]string{"uploadId": "A", "totalChunks": "2", "chunkNumber": "1"})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for id, want := range map[string]int{"A": 200, "B": 204, "": 400} {
		query := url.Values{"path": {root}, "filename": {"file.txt"}, "relativePath": {"nested/file.txt"}, "totalChunks": {"2"}, "chunkNumber": {"01"}, "uploadId": {id}}
		response := httptest.NewRecorder()
		if err := GetFileUpload(echo.New().NewContext(httptest.NewRequest("GET", "/v1/file/upload?"+query.Encode(), nil), response)); err != nil {
			t.Fatal(err)
		}
		if response.Code != want {
			t.Fatal(id, response.Code, response.Body.String())
		}
	}
}

func TestUploadSingleChunkProbeDoesNotRequireTransferID(t *testing.T) {
	root := t.TempDir()
	query := url.Values{"path": {root}, "filename": {"file.txt"}, "totalChunks": {"1"}, "chunkNumber": {"1"}}
	probe := func(want int) {
		t.Helper()
		w := httptest.NewRecorder()
		if err := GetFileUpload(echo.New().NewContext(httptest.NewRequest("GET", "/v1/file/upload?"+query.Encode(), nil), w)); err != nil {
			t.Fatal(err)
		}
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	probe(204)
	if w := testUpload(t, root, "file.txt", "file.txt", "single", map[string]string{"totalChunks": "1", "chunkNumber": "1"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	probe(409)
}

func TestChunkUploadIgnoresAbandonedTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	dir, err := uploadChunkDir(root, "file.txt", "interrupted", 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".chunk-abandoned"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, part := range []struct{ number, content string }{{"1", "first"}, {"1", "first"}, {"2", "second"}} {
		w := testUpload(t, root, "file.txt", "file.txt", part.content, map[string]string{"totalChunks": "2", "chunkNumber": part.number, "uploadId": "interrupted"})
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if part.number == "1" {
			if _, err := os.Stat(filepath.Join(root, "file.txt")); !os.IsNotExist(err) {
				t.Fatal("incomplete upload published", err)
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(root, "file.txt")); err != nil || string(data) != "firstsecond" {
		t.Fatal(string(data), err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("completed upload chunks were not cleaned up", err)
	}
}

func TestChunkReadinessRejectsNonRegularNumberedEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "1"), 0700); err != nil {
		t.Fatal(err)
	}
	if ready, err := uploadChunksReady(root, 2); ready || err == nil {
		t.Fatal("directory counted as a completed part", ready, err)
	}
}

func TestConflictingChunkTransferCleansOnlyItsOwnStagingDirectory(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"A", "B"} {
		w := testUpload(t, root, "file.txt", "file.txt", id, map[string]string{"uploadId": id, "totalChunks": "2", "chunkNumber": "1"})
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	w := testUpload(t, root, "file.txt", "file.txt", "end", map[string]string{"uploadId": "A", "totalChunks": "2", "chunkNumber": "2"})
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	a, _ := uploadChunkDir(root, "file.txt", "A", 2, 1)
	b, _ := uploadChunkDir(root, "file.txt", "B", 2, 1)
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatal("conflicted chunks retained", err)
	}
	if _, err := os.Stat(filepath.Join(b, "1")); err != nil {
		t.Fatal("other transfer removed", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "file.txt")); err != nil || string(data) != "keep" {
		t.Fatal(string(data), err)
	}
}
