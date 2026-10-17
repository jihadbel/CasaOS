// Package storagepaths shares configured user storage between API handlers.
package storagepaths

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
)

type Settings struct {
	SmartAutoSort bool   `json:"smartAutoSort"`
	AppsPath      string `json:"appsPath"`
	ArchivesPath  string `json:"archivesPath"`
	PhotosPath    string `json:"photosPath"`
	VideosPath    string `json:"videosPath"`
	DocumentsPath string `json:"documentsPath"`
	MusicPath     string `json:"musicPath"`
	MobilePath    string `json:"mobilePath"`
	DefaultPath   string `json:"defaultPath"`
}

func Default() Settings {
	root := config.DefaultStorageRoot()
	return Settings{true, filepath.Join(root, "Applications"), filepath.Join(root, "Archives"), filepath.Join(root, "Photos"), filepath.Join(root, "Videos"), filepath.Join(root, "Documents"), filepath.Join(root, "Music"), filepath.Join(root, "Mobile_Uploads"), root}
}

func File() string { return filepath.Join(config.AppInfo.UserDataPath, "storage-paths.json") }

func (s Settings) Roots() []string {
	return []string{s.AppsPath, s.ArchivesPath, s.PhotosPath, s.VideosPath, s.DocumentsPath, s.MusicPath, s.MobilePath, s.DefaultPath}
}

func Read() (Settings, bool, error) {
	s := Default()
	data, err := os.ReadFile(File())
	if os.IsNotExist(err) {
		return s, false, Validate(&s)
	}
	if err != nil {
		return s, false, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, true, err
	}
	return s, true, Validate(&s)
}

func Validate(s *Settings) error {
	for _, path := range []*string{&s.AppsPath, &s.ArchivesPath, &s.PhotosPath, &s.VideosPath, &s.DocumentsPath, &s.MusicPath, &s.MobilePath, &s.DefaultPath} {
		if !filepath.IsAbs(*path) {
			return errors.New("storage paths must be absolute")
		}
		*path = filepath.Clean(*path)
		if filepath.Dir(*path) == *path && path != &s.DefaultPath {
			return errors.New("a drive or filesystem root cannot be user storage")
		}
		if err := safefs.NoLinks(*path); err != nil {
			return err
		}
	}
	return nil
}
