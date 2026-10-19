package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IceWhaleTech/CasaOS/internal/userctx"
	"github.com/IceWhaleTech/CasaOS/pkg/localauth"
)

func TestPrivateSettingsNormalizeAliasesBeforeSavingAndScanning(t *testing.T) {
	for _, alias := range []string{"/", "/drives", "/DATA", "/DATA/Photos"} {
		t.Run(url.PathEscape(alias), func(t *testing.T) {
			alice, bob, h := privateTestUsers(t)
			dir := alice.Home
			if alias == "/DATA/Photos" {
				dir = filepath.Join(dir, "Photos")
			}
			photo := filepath.Join(dir, "alice.jpg")
			writeTestFile(t, photo, "own image")
			writeTestFile(t, filepath.Join(bob.Home, "bob.jpg"), "foreign image")
			settings := settingsForHome(alice.Home)
			settings.PhotosPath = alias
			w := privateRequest(t, h, "PUT", "/api/settings", settings, "alice")
			var response struct{ Settings StorageSettings }
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 200 || response.Settings.PhotosPath != dir {
				t.Fatal("settings response must contain the authorized path", w.Code, w.Body.String(), err)
			}
			data, err := os.ReadFile(filepath.Join(alice.Home, ".casaos-settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			var saved StorageSettings
			if err := json.Unmarshal(data, &saved); err != nil || saved.PhotosPath != dir {
				t.Fatal("an unresolved alias was persisted", saved.PhotosPath, err)
			}
			w = privateRequest(t, h, "GET", "/api/media/timeline", nil, "alice")
			var gallery struct {
				Total    int
				Timeline []TimelineGroup
			}
			if err := json.Unmarshal(w.Body.Bytes(), &gallery); err != nil || w.Code != 200 || gallery.Total != 1 || len(gallery.Timeline) != 1 || len(gallery.Timeline[0].Items) != 1 || gallery.Timeline[0].Items[0].Path != filepath.ToSlash(photo) {
				t.Fatal("gallery escaped or lost the private photo", w.Code, w.Body.String(), err)
			}
		})
	}
}

func TestPrivateSettingsNormalizePreviouslySavedAliases(t *testing.T) {
	alice, bob, h := privateTestUsers(t)
	settings := settingsForHome(alice.Home)
	settings.PhotosPath, settings.VideosPath, settings.MobilePath = "/", "/drives", "/DATA/Mobile_Uploads"
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(alice.Home, ".casaos-settings.json"), string(data))
	writeTestFile(t, filepath.Join(alice.Home, "own.jpg"), "own image")
	writeTestFile(t, filepath.Join(bob.Home, "foreign.jpg"), "foreign image")
	loaded, err := readPrivateSettings(alice)
	if err != nil || loaded.PhotosPath != alice.Home || loaded.VideosPath != alice.Home || loaded.MobilePath != filepath.Join(alice.Home, "Mobile_Uploads") {
		t.Fatal("legacy aliases must be normalized on read", loaded, err)
	}
	w := privateRequest(t, h, "GET", "/api/media/timeline", nil, "alice")
	var gallery struct{ Total int }
	if err := json.Unmarshal(w.Body.Bytes(), &gallery); err != nil || w.Code != 200 || gallery.Total != 1 || strings.Contains(w.Body.String(), "foreign.jpg") {
		t.Fatal(w.Code, w.Body.String(), err)
	}
}

func TestPrivateTimelineScanRejectsForeignRoots(t *testing.T) {
	alice, bob, _ := privateTestUsers(t)
	writeTestFile(t, filepath.Join(alice.Home, "own.jpg"), "own image")
	writeTestFile(t, filepath.Join(bob.Home, "foreign.jpg"), "foreign image")
	ctx := userctx.With(context.Background(), alice)
	items, truncated := scanTimeline(ctx, []string{bob.Home, filepath.Dir(alice.Home), alice.Home}, "all", 10, 100)
	if !truncated || len(items) != 1 || items[0].Path != filepath.ToSlash(filepath.Join(alice.Home, "own.jpg")) {
		t.Fatal("scanner must enforce the request's home even when given foreign roots", items, truncated)
	}
}

func TestPrivateListingOmitsInternalFilesAndCountsVisibleEntries(t *testing.T) {
	alice, _, h := privateTestUsers(t)
	if w := privateRequest(t, h, "PUT", "/api/settings", settingsForHome(alice.Home), "alice"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, name := range []string{"visible.txt", ".notes.txt", ".casaos-write-staging", ".temp/part", ".CasaOS-RecycleBin/recycled.txt"} {
		writeTestFile(t, filepath.Join(alice.Home, filepath.FromSlash(name)), "test content")
	}
	w := privateRequest(t, h, "GET", "/v1/folder?path="+url.QueryEscape(alice.Home), nil, "alice")
	var result struct {
		Data struct {
			Content []struct{ Name string }
			Total   int
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	if result.Data.Total != 2 || len(result.Data.Content) != 2 {
		t.Fatal("internal files must not appear in listings or counts", w.Body.String())
	}
	for _, item := range result.Data.Content {
		if item.Name != "visible.txt" && item.Name != ".notes.txt" {
			t.Fatal("internal entry listed", item.Name)
		}
	}
}

func TestProductionRouterPingAndGatewaySessionDiscovery(t *testing.T) {
	server := httptest.NewServer(dashboardHandler(newAPIHandler()))
	defer server.Close()
	for _, check := range []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/ping", 200, "pong"},
		{"GET", "/ping-extra", 404, ""},
		{"POST", "/ping", 405, ""},
		{"GET", "/api/auth/session", 200, `"authenticationMode":"gateway"`},
		{"POST", "/api/auth/session", 501, "-standalone"},
		{"GET", "/api/settings", 401, ""},
		{"GET", "/v1/folder", 401, ""},
	} {
		t.Run(check.method+check.path, func(t *testing.T) {
			req, err := http.NewRequest(check.method, server.URL+check.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			res, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			data, err := io.ReadAll(res.Body)
			if err != nil || res.StatusCode != check.status || !strings.Contains(string(data), check.body) {
				t.Fatal(res.StatusCode, string(data), err)
			}
			if check.path == "/ping" && check.method == "GET" && string(data) != "pong" {
				t.Fatal("health response changed", string(data))
			}
			if check.path == "/api/auth/session" && res.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("session discovery must not be cached")
			}
		})
	}
}

func TestLocalAuthOwnsSessionDiscoveryBeforeGatewayHandler(t *testing.T) {
	dir := t.TempDir()
	auth, err := localauth.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	password, err := os.ReadFile(filepath.Join(dir, "initial-password.txt"))
	if err != nil {
		t.Fatal(err)
	}
	handler := auth.Middleware(dashboardHandler(newAPIHandler()))
	body, err := json.Marshal(map[string]string{"username": "admin", "password": strings.TrimSpace(string(password))})
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest("POST", "/api/auth/session", bytes.NewReader(body)))
	if login.Code != 200 || len(login.Result().Cookies()) != 1 {
		t.Fatal("local login no longer works", login.Code)
	}
	req := httptest.NewRequest("GET", "/api/auth/session", nil)
	req.AddCookie(login.Result().Cookies()[0])
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	var session struct{ Authenticated bool }
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil || w.Code != 200 || !session.Authenticated || strings.Contains(w.Body.String(), "gateway") {
		t.Fatal("local session was intercepted by gateway discovery", w.Code, err)
	}
}
