package service

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
	model2 "github.com/IceWhaleTech/CasaOS/service/model"
	"github.com/glebarez/sqlite"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func TestLegacyUploaderRejectsInvalidMetadataWithoutCreatingFiles(t *testing.T) {
	root := t.TempDir()
	ctx := echo.New().NewContext(httptest.NewRequest("POST", "/", nil), httptest.NewRecorder())
	s := NewFileUploadService()
	for _, chunks := range []int64{-1, 0, 10000000000} {
		if err := s.UploadFile(ctx, root, 0, 0, 0, chunks, 0, "id", "../escaped", "escaped", nil); err == nil {
			t.Fatal("retired uploader accepted metadata")
		}
	}
	if err := s.TestChunk(ctx, "id", -1); err == nil {
		t.Fatal("retired probe accepted chunk")
	}
	if files, err := os.ReadDir(root); err != nil || len(files) != 0 {
		t.Fatal(files, err)
	}
}

func TestLogTailUsesRequestedLinesAndBoundsBytes(t *testing.T) {
	previous := *config.AppInfo
	t.Cleanup(func() { *config.AppInfo = previous })
	config.AppInfo.LogPath = t.TempDir()
	config.AppInfo.LogSaveName = "server"
	config.AppInfo.LogFileExt = "log"
	path := filepath.Join(config.AppInfo.LogPath, "server.log")
	if err := os.WriteFile(path, []byte("first\nsecond\nthird\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewSystemService()
	if text := s.GetCasaOSLogs(2); text != "second\nthird\n" {
		t.Fatal(text)
	}
	if text := s.GetCasaOSLogs(0); text != "" {
		t.Fatal(text)
	}
	if err := os.WriteFile(path, make([]byte, 2<<20), 0600); err != nil {
		t.Fatal(err)
	}
	if text := s.GetCasaOSLogs(10); len(text) > 1<<20 {
		t.Fatal("unbounded tail", len(text))
	}
}

func TestSMBCredentialsAreNotPersistedAndLegacyRowsAreCleared(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&model2.ConnectionsDBModel{}); err != nil {
		t.Fatal(err)
	}
	legacy := model2.ConnectionsDBModel{Host: "legacy", Password: "legacy-secret"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	s := NewConnectionsService(db)
	newConnection := model2.ConnectionsDBModel{Host: "new", Password: "new-secret"}
	s.CreateConnection(&newConnection)
	var rows []model2.ConnectionsDBModel
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		if row.Password != "" {
			t.Fatal("plaintext SMB credential remains")
		}
	}
}

func TestShareValidationRejectsInternalPathsAndAncestors(t *testing.T) {
	root := t.TempDir()
	previous := config.AppInfo.UserDataPath
	config.AppInfo.UserDataPath = filepath.Join(root, "private", "conf")
	t.Cleanup(func() { config.AppInfo.UserDataPath = previous })
	for _, path := range []string{config.AppInfo.UserDataPath, filepath.Join(config.AppInfo.UserDataPath, "auth"), root} {
		if err := ValidateSharePath(path); err == nil {
			t.Fatal("internal tree may be exported", path)
		}
	}
	if err := ValidateSharePath(filepath.Join(root, "public")); err != nil {
		t.Fatal(err)
	}
}
