package main

import (
	"archive/zip"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
)

func TestAuditCompressionExcludesInternalStorage(t *testing.T) {
	root := t.TempDir()
	previousBin := RecycleBinDir
	t.Setenv("CASAOS_DATA_ROOT", root)
	RecycleBinDir = filepath.Join(config.DefaultStorageRoot(), "RecycleBin")
	defer func() { RecycleBinDir = previousBin }()
	for _, rel := range []string{"visible.txt", "RecycleBin/deleted-secret.txt", ".temp/incomplete-upload.txt"} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(rel), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(t.TempDir(), "export.zip")
	body, err := json.Marshal(ZipRequest{Files: []string{root}, Output: output})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handleZip(w, httptest.NewRequest("POST", "/api/zip", strings.NewReader(string(body))))
	if w.Code != 200 {
		t.Fatalf("zip failed: %d %s", w.Code, w.Body.String())
	}
	z, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	for _, f := range z.File {
		if strings.Contains(f.Name, "deleted-secret") || strings.Contains(f.Name, "incomplete-upload") {
			t.Errorf("folder export includes internal content: %s", f.Name)
		}
	}
}
