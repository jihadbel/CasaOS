package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
)

func TestArchiveAndTrashRejectInternalPathsBeforeMutation(t *testing.T) {
	root := t.TempDir()
	previous := config.AppInfo.UserDataPath
	previousBin := RecycleBinDir
	config.AppInfo.UserDataPath = filepath.Join(root, "conf")
	RecycleBinDir = filepath.Join(root, "bin")
	t.Cleanup(func() { config.AppInfo.UserDataPath = previous; RecycleBinDir = previousBin })
	secret := filepath.Join(config.AppInfo.UserDataPath, "password.hash")
	writeTestFile(t, secret, "private")
	public := filepath.Join(root, "public.txt")
	writeTestFile(t, public, "public")
	archive := filepath.Join(root, "source.zip")
	makeTestZIP(t, archive, "file.txt", "data")
	for _, response := range []*httptest.ResponseRecorder{
		apiRequest(t, handleZip, "POST", "/api/zip", ZipRequest{Files: []string{public}, Output: filepath.Join(config.AppInfo.UserDataPath, "bad.zip")}),
		apiRequest(t, handleUnzip, "POST", "/api/unzip", UnzipRequest{Path: archive, Dest: filepath.Join(config.AppInfo.UserDataPath, "extracted")}),
		apiRequest(t, handleTrash, "POST", "/api/trash/move", map[string]string{"path": secret}),
	} {
		if response.Code != 400 {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if data, err := os.ReadFile(secret); err != nil || string(data) != "private" {
		t.Fatal(string(data), err)
	}
}

func TestCancelledZipDoesNotPublishOutput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "file.txt")
	writeTestFile(t, source, "data")
	output := filepath.Join(root, "archive.zip")
	data, _ := json.Marshal(ZipRequest{Files: []string{source}, Output: output})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/api/zip", strings.NewReader(string(data))).WithContext(ctx)
	w := httptest.NewRecorder()
	handleZip(w, r)
	if w.Code == 200 {
		t.Fatal("cancelled archive published")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("cancelled output exists", err)
	}
}
