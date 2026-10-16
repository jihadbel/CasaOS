package v1

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/service"
	serviceModel "github.com/IceWhaleTech/CasaOS/service/model"
	"github.com/labstack/echo/v4"
)

type deleteConnections struct{ service.ConnectionsService }

func (deleteConnections) GetConnectionsList() []serviceModel.ConnectionsDBModel { return nil }

type deleteRepository struct{ service.Repository }

func (deleteRepository) Connections() service.ConnectionsService { return deleteConnections{} }

func TestBatchDeleteValidatesEveryPathBeforeRemovingAnything(t *testing.T) {
	root := t.TempDir()
	storage := filepath.Join(root, "storage")
	t.Setenv("CASAOS_DATA_ROOT", storage)
	old := config.AppInfo.UserDataPath
	config.AppInfo.UserDataPath = filepath.Join(root, "conf")
	t.Cleanup(func() { config.AppInfo.UserDataPath = old })
	valid := filepath.Join(root, "ordinary.txt")
	if err := os.WriteFile(valid, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{storage, root, filepath.Join(storage, "RecycleBin"), filepath.Join(storage, ".temp"), filepath.Join(root, "conf"), "", ".", "relative.txt"} {
		body, err := json.Marshal([]string{valid, forbidden})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("DELETE", "/v1/batch", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if err := DeleteFile(echo.New().NewContext(r, w)); err != nil {
			t.Fatal(err)
		}
		if w.Code != 400 {
			t.Fatal(forbidden, w.Code, w.Body.String())
		}
		if data, err := os.ReadFile(valid); err != nil || string(data) != "keep" {
			t.Fatal("earlier batch member deleted before validation", string(data), err)
		}
	}
}

func TestBatchDeleteRemovesOrdinaryFolderAndRejectsMalformedBody(t *testing.T) {
	previous := service.MyService
	service.MyService = deleteRepository{}
	t.Cleanup(func() { service.MyService = previous })
	root := t.TempDir()
	target := filepath.Join(root, "ordinary", "nested", "file.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal([]string{filepath.Join(root, "ordinary")})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		body []byte
		want int
	}{{[]byte(`{"invalid":true}`), 400}, {body, 200}} {
		req := httptest.NewRequest("DELETE", "/v1/batch", bytes.NewReader(entry.body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if err := DeleteFile(echo.New().NewContext(req, w)); err != nil {
			t.Fatal(err)
		}
		if w.Code != entry.want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "ordinary")); !os.IsNotExist(err) {
		t.Fatal("ordinary delete failed", err)
	}
}
