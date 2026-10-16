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

func TestEditorPreservesOriginalEncodingAndLineEndings(t *testing.T) {
	for name, raw := range map[string][]byte{
		"utf8": []byte("a\r\nمرحبا"), "utf8-bom": append([]byte{0xef, 0xbb, 0xbf}, []byte("a\r\nمرحبا")...),
		"utf16le": {0xff, 0xfe, 'a', 0, 13, 0, 10, 0, 0x27, 0x06}, "utf16be": {0xfe, 0xff, 0, 'a', 0, 13, 0, 10, 0x06, 0x27},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "text.txt")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			read := httptest.NewRecorder()
			if err := GetFilerContent(echo.New().NewContext(httptest.NewRequest("GET", "/v1/file/content?path="+url.QueryEscape(path), nil), read)); err != nil {
				t.Fatal(err)
			}
			var result struct {
				Success int    `json:"success"`
				Data    string `json:"data"`
			}
			if err := json.Unmarshal(read.Body.Bytes(), &result); err != nil || read.Code != 200 || result.Success != 200 || strings.ContainsRune(result.Data, '\ufffd') {
				t.Fatal(read.Code, read.Body.String(), err)
			}
			body, _ := json.Marshal(map[string]string{"path": path, "content": strings.ReplaceAll(result.Data, "\r\n", "\n")})
			req := httptest.NewRequest("PUT", "/v1/file", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			write := httptest.NewRecorder()
			if err := PutFileContent(echo.New().NewContext(req, write)); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil || write.Code != 200 || !bytes.Equal(raw, after) {
				t.Fatalf("save changed encoding: %x -> %x, %d, %v", raw, after, write.Code, err)
			}
		})
	}
}

func TestEditorRejectsUnsupportedEncodingWithoutOverwriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.txt")
	before := []byte{0xff, 0xe9, 'a'}
	os.WriteFile(path, before, 0600)
	read := httptest.NewRecorder()
	GetFilerContent(echo.New().NewContext(httptest.NewRequest("GET", "/v1/file/content?path="+url.QueryEscape(path), nil), read))
	body, _ := json.Marshal(map[string]string{"path": path, "content": "replacement"})
	req := httptest.NewRequest("PUT", "/v1/file", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	write := httptest.NewRecorder()
	PutFileContent(echo.New().NewContext(req, write))
	after, _ := os.ReadFile(path)
	if read.Code != 415 || write.Code != 415 || !bytes.Equal(before, after) {
		t.Fatal(read.Code, write.Code, before, after)
	}
}
