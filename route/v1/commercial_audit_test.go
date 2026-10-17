package v1

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/service"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

func TestEditorUploadAndMkdirRejectInternalStorage(t *testing.T) {
	root := t.TempDir()
	previous := config.AppInfo.UserDataPath
	config.AppInfo.UserDataPath = filepath.Join(root, "conf")
	t.Cleanup(func() { config.AppInfo.UserDataPath = previous })
	if err := os.MkdirAll(config.AppInfo.UserDataPath, 0700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(config.AppInfo.UserDataPath, "password.hash")
	if err := os.WriteFile(secret, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, path string
		handler      echo.HandlerFunc
		body         map[string]string
	}{
		{"GET", "/editor?path=" + url.QueryEscape(secret), GetFilerContent, nil},
		{"PUT", "/editor", PutFileContent, map[string]string{"path": secret, "content": "changed"}},
		{"POST", "/mkdir", MkdirAll, map[string]string{"path": filepath.Join(config.AppInfo.UserDataPath, "new")}},
	} {
		e := echo.New()
		e.Add(test.method, strings.Split(test.path, "?")[0], test.handler)
		body, _ := json.Marshal(test.body)
		r := httptest.NewRequest(test.method, test.path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(test.path, w.Code, w.Body.String())
		}
	}
	if response := testUpload(t, config.AppInfo.UserDataPath, "new.txt", "new.txt", "invalid", nil); response.Code != 400 {
		t.Fatal(response.Code, response.Body.String())
	}
	if data, err := os.ReadFile(secret); err != nil || string(data) != "private" {
		t.Fatal(string(data), err)
	}
}

func TestBatchDownloadPreservesZipTarAndGzipContents(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.txt")
	folder := filepath.Join(root, "album")
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "second.txt"), []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"zip", "tar", "targz"} {
		t.Run(format, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/v1/batch?format="+format+"&files="+url.QueryEscape(first+","+folder), nil)
			w := httptest.NewRecorder()
			if err := GetDownloadFile(echo.New().NewContext(r, w)); err != nil {
				t.Fatal(err)
			}
			contents := map[string]string{}
			if format == "zip" {
				archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range archive.File {
					if entry.FileInfo().IsDir() {
						continue
					}
					input, err := entry.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(input)
					input.Close()
					if err != nil {
						t.Fatal(err)
					}
					contents[entry.Name] = string(data)
				}
			} else {
				var input io.Reader = bytes.NewReader(w.Body.Bytes())
				if format == "targz" {
					compressed, err := gzip.NewReader(input)
					if err != nil {
						t.Fatal(err)
					}
					defer compressed.Close()
					input = compressed
				}
				archive := tar.NewReader(input)
				for {
					entry, err := archive.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					if entry.FileInfo().IsDir() {
						continue
					}
					data, err := io.ReadAll(archive)
					if err != nil {
						t.Fatal(err)
					}
					contents[entry.Name] = string(data)
				}
			}
			if len(contents) != 2 || contents["first.txt"] != "first" || contents["album/second.txt"] != "second" {
				t.Fatal(format, contents)
			}
		})
	}
}

func TestRetiredEndpointsNeverMakeNetworkOrPowerCalls(t *testing.T) {
	for _, handler := range []echo.HandlerFunc{GetSystemProxy, GetSearchResult, WsSsh, PostSshLogin, PostKillCasaOS, PutSystemState} {
		e := echo.New()
		e.Any("/retired", handler)
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest("GET", "/retired?url=http://127.0.0.1:1&password=secret", nil))
		if w.Code != http.StatusGone {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestUploadLocksAreIndependentAndReleaseState(t *testing.T) {
	unlock := lockUpload("first")
	finished := make(chan struct{})
	go func() { release := lockUpload("second"); release(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		unlock()
		t.Fatal("unrelated upload blocked")
	}
	unlock()
	uploadLocks.Lock()
	defer uploadLocks.Unlock()
	if uploadLocks.entries["first"] != nil || uploadLocks.entries["second"] != nil {
		t.Fatal("finished upload lock retained")
	}
}

func TestUploadCleanupPreservesActiveRecentAndUnknownDirectories(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-25 * time.Hour)
	paths := []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64), "unknown"}
	for _, name := range paths {
		path := filepath.Join(root, ".temp", name)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		if name != paths[2] {
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	active := filepath.Join(root, ".temp", paths[1])
	unlock := lockUpload(active)
	cleanupExpiredUploads(root, time.Now())
	unlock()
	if _, err := os.Stat(filepath.Join(root, ".temp", paths[0])); !os.IsNotExist(err) {
		t.Fatal("stale chunks retained", err)
	}
	for _, name := range paths[1:] {
		if _, err := os.Stat(filepath.Join(root, ".temp", name)); err != nil {
			t.Fatal("protected chunks removed", name, err)
		}
	}
}

func TestWebSocketRejectsMessagesOver64KiB(t *testing.T) {
	center := &CenterHandler{broadcast: make(chan []byte, 2), unregister: make(chan *Client, 2)}
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgraderFile.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &Client{ID: `peer", "injected":true`, conn: conn, handler: center, done: make(chan struct{})}
		client.readPump()
		close(done)
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte("a"), (64<<10)+1))
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("oversized message did not close connection")
	}
	message := <-center.broadcast
	var payload map[string]interface{}
	if err := json.Unmarshal(message, &payload); err != nil || len(payload) != 2 {
		t.Fatal(string(message), err)
	}
}

func TestPeerCookieRequiresUUID(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "peerid", Value: "invalid-peer"})
	if id := service.GetPeerId(r, "fallback"); id != "fallback" {
		t.Fatal(id)
	}
}
