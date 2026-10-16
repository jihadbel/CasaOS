package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IceWhaleTech/CasaOS/pkg/localauth"
	"github.com/IceWhaleTech/CasaOS/route"
)

// Real HTTP on an ephemeral loopback listener; no production startup, child
// process, scheduled task, external interface or personal storage is involved.
func TestStandaloneHTTPFlow(t *testing.T) {
	root := setupTrashTest(t)
	authDir := filepath.Join(root, "auth")
	auth, err := localauth.New(authDir)
	if err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(root, "private.txt")
	writeTestFile(t, private, "private data")
	router := route.InitV1Router()
	server := httptest.NewServer(auth.Middleware(dashboardHandler(router)))
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	request := func(method, path string, body interface{}, want int) []byte {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		result, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: want %d got %d: %s", method, path, want, response.StatusCode, result)
		}
		return result
	}
	request("GET", "/v1/file?path="+url.QueryEscape(private), nil, 401)
	request("POST", "/api/zip", ZipRequest{Files: []string{private}}, 401)
	password, err := os.ReadFile(filepath.Join(authDir, "initial-password.txt"))
	if err != nil {
		t.Fatal(err)
	}
	request("POST", "/api/auth/session", map[string]string{"password": strings.TrimSpace(string(password))}, 200)
	result := request("GET", "/v1/file?path="+url.QueryEscape(private), nil, 200)
	if string(result) != "private data" {
		t.Fatal("download changed bytes")
	}
	request("PUT", "/v1/file", map[string]string{"path": private, "content": "edited"}, 200)
	folder := filepath.Join(root, "shared-folder")
	writeTestFile(t, filepath.Join(folder, "nested", "hello.txt"), "shared folder contents")
	download := request("GET", "/v1/file?path="+url.QueryEscape(folder), nil, 200)
	archive, err := zip.NewReader(bytes.NewReader(download), int64(len(download)))
	if err != nil {
		t.Fatal("shared folder is not a ZIP", err)
	}
	found := false
	for _, entry := range archive.File {
		if entry.Name == "shared-folder/nested/hello.txt" {
			reader, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			content, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || string(content) != "shared folder contents" {
				t.Fatal(string(content), err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("shared folder ZIP omitted its contents")
	}
	var moved struct {
		Item TrashItem `json:"item"`
	}
	json.Unmarshal(request("POST", "/api/trash/move", map[string]string{"path": private}, 200), &moved)
	request("POST", "/api/trash/restore", map[string]string{"id": moved.Item.ID}, 200)
	if data, _ := os.ReadFile(private); string(data) != "edited" {
		t.Fatal("restore changed bytes")
	}
	output := filepath.Join(root, "output.zip")
	request("POST", "/api/zip", ZipRequest{Files: []string{private}, Output: output}, 200)
	dest := filepath.Join(root, "extracted")
	request("POST", "/api/unzip", UnzipRequest{Path: output, Dest: dest}, 200)
	if data, _ := os.ReadFile(filepath.Join(dest, "private.txt")); string(data) != "edited" {
		t.Fatal("archive round trip changed bytes")
	}
	request("DELETE", "/api/auth/session", nil, 200)
	request("GET", "/api/trash", nil, 401)
}
