package file

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/storagepaths"
)

func TestStoragePathsRespectConfiguredRootsAndExcludeInternalData(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	t.Setenv("CASAOS_DATA_ROOT", data)
	previous := config.AppInfo.UserDataPath
	config.AppInfo.UserDataPath = filepath.Join(root, "conf")
	t.Cleanup(func() { config.AppInfo.UserDataPath = previous })
	s := storagepaths.Default()
	s.PhotosPath = filepath.Join(root, "separate-photos")
	if err := os.MkdirAll(config.AppInfo.UserDataPath, 0700); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storagepaths.File(), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{data, filepath.Join(data, "new.txt"), filepath.Join(s.PhotosPath, "2026", "a.jpg")} {
		if _, err := ValidateStoragePath(path); err != nil {
			t.Fatal(path, err)
		}
	}
	for _, path := range []string{"relative.txt", root, filepath.Dir(root), filepath.Join(root, "data-other", "a.txt"), storagepaths.File(), filepath.Join(data, ".temp", "part"), filepath.Join(data, "RecycleBin", "index")} {
		if _, err := ValidateStoragePath(path); err == nil {
			t.Fatal("unsafe path allowed", path)
		}
	}
	if err := os.MkdirAll(data, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(data, "link")
	if err := os.Symlink(root, link); err == nil {
		if _, err := ValidateStoragePath(filepath.Join(link, "escape.txt")); err == nil {
			t.Fatal("link escaped storage")
		}
	}
}
