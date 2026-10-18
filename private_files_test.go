package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IceWhaleTech/CasaOS/internal/textfile"
	"github.com/IceWhaleTech/CasaOS/internal/userctx"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/localauth"
	"github.com/IceWhaleTech/CasaOS/route"
	"github.com/IceWhaleTech/CasaOS/service"
	model2 "github.com/IceWhaleTech/CasaOS/service/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func privateTestUsers(t *testing.T) (localauth.Identity, localauth.Identity, http.Handler) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CASAOS_DATA_ROOT", filepath.Join(root, "legacy"))
	previousApp := *config.AppInfo
	config.AppInfo.UserDataPath = filepath.Join(root, "conf")
	config.AppInfo.DBPath = filepath.Join(root, "db")
	t.Cleanup(func() { *config.AppInfo = previousApp })
	previous := config.PrivateStorageRoot
	config.PrivateStorageRoot = root
	t.Cleanup(func() { config.PrivateStorageRoot = previous })
	alice := localauth.Identity{User: localauth.User{ID: "alice", Username: "alice"}, Home: filepath.Join(root, "alice")}
	bob := localauth.Identity{User: localauth.User{ID: "bob", Username: "bob"}, Home: filepath.Join(root, "bob")}
	db, err := gorm.Open(sqlite.Open(filepath.Join(root, "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model2.SharesDBModel{}, &model2.ConnectionsDBModel{}); err != nil {
		t.Fatal(err)
	}
	previousService := service.MyService
	service.MyService = service.NewService(db, filepath.Join(root, "runtime"))
	t.Cleanup(func() { service.MyService = previousService; sqlDB.Close() })
	for _, user := range []localauth.Identity{alice, bob} {
		if err := os.MkdirAll(user.Home, 0700); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/", route.InitV1Router())
	mux.Handle("/v3/file", route.InitFile())
	guard := privateFilesHandler(dashboardHandler(mux))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Test fixture injects the same typed context issued by the separately
		// tested cookie middleware; headers and query parameters cannot set it.
		user := alice
		if r.Header.Get("X-Test-User") == "bob" {
			user = bob
		}
		guard.ServeHTTP(w, r.WithContext(userctx.With(r.Context(), user)))
	})
	return alice, bob, handler
}

func privateRequest(t *testing.T, h http.Handler, method, path string, body interface{}, who string) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Test-User", who)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func privateUpload(t *testing.T, h http.Handler, user, dir, content string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "same.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"path": dir, "filename": "same.txt", "relativePath": "same.txt"} {
		if err := form.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/file/upload", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.Header.Set("X-Test-User", user)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestPrivateUsersUploadSameNameAndCannotReadEachOthersFiles(t *testing.T) {
	alice, bob, h := privateTestUsers(t)
	for _, entry := range []struct {
		user localauth.Identity
		data string
	}{{alice, "alice private file"}, {bob, "bob private file"}} {
		if w := privateUpload(t, h, entry.user.ID, entry.user.Home, entry.data); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		path := filepath.Join(entry.user.Home, "same.txt")
		w := privateRequest(t, h, "GET", "/v1/file?path="+url.QueryEscape(path), nil, entry.user.ID)
		if w.Code != 200 || w.Body.String() != entry.data {
			t.Fatal(w.Code, w.Body.String())
		}
		if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatal("private response may be cached")
		}
	}
	foreign := filepath.Join(bob.Home, "same.txt")
	for _, endpoint := range []string{"/v1/file?path=", "/v3/file?path=", "/v1/image?type=original&path=", "/v1/file/content?path=", "/v1/folder?path=", "/v1/batch?files=", "/api/media/timeline?folder="} {
		w := privateRequest(t, h, "GET", endpoint+url.QueryEscape(foreign), nil, "alice")
		if w.Code != 403 {
			t.Fatal(endpoint, w.Code, w.Body.String())
		}
	}
	if w := privateUpload(t, h, "alice", bob.Home, "intrusion"); w.Code != 403 {
		t.Fatal("cross-user upload accepted", w.Code)
	}
	for _, request := range []struct {
		method, path string
		body         interface{}
	}{
		{"PUT", "/v1/file", map[string]string{"PATH": foreign, "content": "overwrite"}},
		{"DELETE", "/v1/batch", []string{foreign}},
		{"POST", "/api/trash/move", map[string]string{"path": foreign}},
		{"PUT", "/v1/file/name", map[string]string{"old_path": foreign, "new_path": filepath.Join(alice.Home, "stolen")}},
		{"POST", "/v1/batch/task", map[string]interface{}{"type": "copy", "to": alice.Home, "item": []map[string]string{{"from": foreign}}}},
		{"POST", "/api/zip", ZipRequest{Files: []string{foreign}, Output: filepath.Join(alice.Home, "stolen.zip")}},
		{"POST", "/api/unzip", UnzipRequest{Path: foreign, Dest: alice.Home}},
	} {
		if w := privateRequest(t, h, request.method, request.path, request.body, "alice"); w.Code != 403 {
			t.Fatal(request.path, w.Code, w.Body.String())
		}
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "bob private file" {
		t.Fatal("foreign file changed", string(data), err)
	}
}

func TestPrivateTrashSettingsAndGalleryHaveIndependentState(t *testing.T) {
	alice, bob, h := privateTestUsers(t)
	for _, user := range []localauth.Identity{alice, bob} {
		if w := privateUpload(t, h, user.ID, user.Home, user.Username); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		w := privateRequest(t, h, "POST", "/api/trash/move", map[string]string{"path": filepath.Join(user.Home, "same.txt")}, user.ID)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := privateRequest(t, h, "GET", "/api/trash", nil, "bob")
	var bin struct {
		Items []TrashItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &bin); err != nil {
		t.Fatal(err)
	}
	if len(bin.Items) != 1 || !strings.Contains(bin.Items[0].OriginalPath, "bob") {
		t.Fatal(w.Body.String())
	}
	if w := privateRequest(t, h, "POST", "/api/trash/restore", map[string]string{"id": bin.Items[0].ID}, "alice"); w.Code != 404 {
		t.Fatal("foreign trash ID accepted", w.Code)
	}
	if w := privateRequest(t, h, "POST", "/api/trash/empty", nil, "alice"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.FromSlash(bin.Items[0].TrashPath)); err != nil {
		t.Fatal("emptying Alice's trash removed Bob's file", err)
	}
	settings := settingsForHome(alice.Home)
	settings.SmartAutoSort = false
	if w := privateRequest(t, h, "PUT", "/api/settings", settings, "alice"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	a, err := readPrivateSettings(alice)
	if err != nil || a.SmartAutoSort {
		t.Fatal(a, err)
	}
	b, err := readPrivateSettings(bob)
	if err != nil || !b.SmartAutoSort {
		t.Fatal(b, err)
	}
	settings.PhotosPath = bob.Home
	if w := privateRequest(t, h, "PUT", "/api/settings", settings, "alice"); w.Code != 403 {
		t.Fatal("settings escaped home", w.Code)
	}
	writeTestFile(t, filepath.Join(alice.Home, "Photos", "alice.jpg"), "photo")
	writeTestFile(t, filepath.Join(bob.Home, "Photos", "bob.jpg"), "photo")
	w = privateRequest(t, h, "GET", "/api/media/timeline", nil, "alice")
	if w.Code != 200 || strings.Contains(w.Body.String(), "bob.jpg") || !strings.Contains(w.Body.String(), "alice.jpg") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestPrivateGatewayDeniesHostFeaturesRootDeletionAndPathAliases(t *testing.T) {
	alice, bob, h := privateTestUsers(t)
	for _, path := range []string{"/v1/sys/debug", "/v1/file/ws", "/v1/file/peers", "/v1/samba/shares", "/v1/cloud", "/v1/zt/status", "/v2/casaos/health/ports", "/doc/v2/casaos"} {
		if w := privateRequest(t, h, "GET", path, nil, "alice"); w.Code != 403 {
			t.Fatal(path, w.Code)
		}
	}
	if w := privateRequest(t, h, "DELETE", "/v1/batch", []string{alice.Home}, "alice"); w.Code != 403 {
		t.Fatal("home deletion accepted", w.Code)
	}
	for _, raw := range []string{filepath.Join(alice.Home, "..", "bob", "file"), filepath.Join(alice.Home, ".casaos-settings.json"), filepath.Join(bob.Home, "file"), filepath.Dir(alice.Home)} {
		if _, err := scopedUserPath(alice, raw, true); err == nil {
			t.Fatal("unsafe path accepted", raw)
		}
	}
	if w := privateRequest(t, h, "GET", "/v1/folder?path="+url.QueryEscape(alice.Home)+"&path="+url.QueryEscape(bob.Home), nil, "alice"); w.Code != 403 {
		t.Fatal("duplicate path accepted", w.Code)
	}
	for _, raw := range []string{"/", "/DATA", "/drives"} {
		clean, err := scopedUserPath(alice, raw, true)
		if err != nil || clean != alice.Home {
			t.Fatal(clean, err)
		}
	}
}

func TestPrivateUserFolderCreationAndLargeUploadSpooling(t *testing.T) {
	alice, _, h := privateTestUsers(t)
	folder := filepath.Join(alice.Home, "notes")
	if w := privateRequest(t, h, "POST", "/v1/folder", map[string]string{"path": folder}, "alice"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	content := strings.Repeat("private upload ", 700000) // exceeds the gateway's 8 MiB in-memory multipart budget
	if w := privateUpload(t, h, "alice", folder, content); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if data, err := os.ReadFile(filepath.Join(folder, "same.txt")); err != nil || !bytes.Equal(data, []byte(content)) {
		t.Fatal("spooled upload incomplete", len(data), err)
	}
	w := privateRequest(t, h, "GET", "/v1/folder?path="+url.QueryEscape(folder), nil, "alice")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "same.txt") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestPrivateEditorSavesReadableFilesWithExpandedJSON(t *testing.T) {
	alice, _, h := privateTestUsers(t)
	for _, entry := range []struct{ name, content string }{
		{"quotes.txt", strings.Repeat("\"", 13<<20)},
		// This valid text expands past 64 MiB when encoded as JSON.
		{"escaped.txt", strings.Repeat("<\x01\"", textfile.MaxEditorFileBytes/3)},
	} {
		t.Run(entry.name, func(t *testing.T) {
			path := filepath.Join(alice.Home, entry.name)
			if err := os.WriteFile(path, []byte(entry.content), 0600); err != nil {
				t.Fatal(err)
			}
			get := privateRequest(t, h, "GET", "/v1/file/content?path="+url.QueryEscape(path), nil, "alice")
			if get.Code != 200 {
				t.Fatal("read", get.Code)
			}
			put := privateRequest(t, h, "PUT", "/v1/file", map[string]string{"path": path, "content": entry.content}, "alice")
			if put.Code != 200 {
				t.Fatal("save", put.Code, put.Body.String())
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != entry.content {
				t.Fatal("editor changed the text", err)
			}
		})
	}
}

func TestPrivateGatewayReturns413ForOversizedBodiesWithoutWriting(t *testing.T) {
	alice, _, h := privateTestUsers(t)
	path := filepath.Join(alice.Home, "unchanged.txt")
	writeTestFile(t, path, "original")
	r := httptest.NewRequest("PUT", "/v1/file", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	r.ContentLength = textfile.MaxEditorRequestBytes + 1
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatal(w.Code, w.Body.String())
	}
	// Unknown-length bodies must also retain the size error after the first JSON value.
	folder := filepath.Join(alice.Home, "never-created")
	body, err := json.Marshal(map[string]string{"path": folder})
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("POST", "/v1/folder", io.MultiReader(bytes.NewReader(body), strings.NewReader(strings.Repeat(" ", (24<<20)+1))))
	r.ContentLength = -1
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatal("oversized request created a folder", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
		t.Fatal("oversized editor request changed the file", err)
	}
}
