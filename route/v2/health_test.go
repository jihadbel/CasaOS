package v2

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/labstack/echo/v4"
)

func TestHealthLogsUsesConfiguredDirectory(t *testing.T) {
	previous := config.AppInfo.LogPath
	config.AppInfo.LogPath = t.TempDir()
	defer func() { config.AppInfo.LogPath = previous }()
	if err := os.WriteFile(filepath.Join(config.AppInfo.LogPath, "server.log"), []byte("configured log"), 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health/logs", nil)
	if err := NewCasaOS().GetHealthlogs(echo.New().NewContext(r, w)); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 1 {
		t.Fatal("incorrect log archive", z.File)
	}
	f, err := z.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	value, err := io.ReadAll(f)
	if err != nil || string(value) != "configured log" {
		t.Fatalf("%q %v", value, err)
	}
}
