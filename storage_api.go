package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
)

type StorageSettings struct {
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

var settingsMutex sync.Mutex

func defaultStorageSettings() StorageSettings {
	root := config.DefaultStorageRoot()
	return StorageSettings{true, filepath.Join(root, "Applications"), filepath.Join(root, "Archives"), filepath.Join(root, "Photos"), filepath.Join(root, "Videos"), filepath.Join(root, "Documents"), filepath.Join(root, "Music"), filepath.Join(root, "Mobile_Uploads"), root}
}

func storageSettingsFile() string {
	return filepath.Join(config.AppInfo.UserDataPath, "storage-paths.json")
}

func readStorageSettings() (StorageSettings, bool, error) {
	settings := defaultStorageSettings()
	data, err := os.ReadFile(storageSettingsFile())
	if os.IsNotExist(err) {
		return settings, false, nil
	}
	if err != nil {
		return settings, false, err
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, true, err
	}
	return settings, true, validateStorageSettings(&settings)
}

func validateStorageSettings(s *StorageSettings) error {
	for _, path := range []*string{&s.AppsPath, &s.ArchivesPath, &s.PhotosPath, &s.VideosPath, &s.DocumentsPath, &s.MusicPath, &s.MobilePath, &s.DefaultPath} {
		if !filepath.IsAbs(*path) {
			return errors.New("storage paths must be absolute")
		}
		*path = filepath.Clean(*path)
		if err := safefs.NoLinks(*path); err != nil {
			return err
		}
	}
	return nil
}

func handleStorageSettings(w http.ResponseWriter, r *http.Request) {
	settingsMutex.Lock()
	defer settingsMutex.Unlock()
	if r.Method == http.MethodGet {
		settings, configured, err := readStorageSettings()
		if err != nil {
			apiError(w, 500, err)
			return
		}
		apiJSON(w, 200, map[string]interface{}{"success": true, "settings": settings, "configured": configured})
		return
	}
	if r.Method != http.MethodPut {
		apiError(w, 405, errors.New("method not allowed"))
		return
	}
	var settings StorageSettings
	if err := decodeAPI(w, r, &settings); err != nil {
		apiError(w, 400, err)
		return
	}
	if err := validateStorageSettings(&settings); err != nil {
		apiError(w, 400, err)
		return
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err == nil {
		err = safefs.WriteAtomic(storageSettingsFile(), data, 0600)
	}
	if err != nil {
		apiError(w, 500, err)
		return
	}
	apiJSON(w, 200, map[string]interface{}{"success": true, "settings": settings})
}

func handleServerInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, 405, errors.New("method not allowed"))
		return
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		apiError(w, 500, err)
		return
	}
	ips := []string{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addresses {
			ip, _, err := net.ParseCIDR(addr.String())
			if err == nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				ips = append(ips, ip.String())
			}
		}
	}
	sort.Strings(ips)
	apiJSON(w, 200, map[string]interface{}{"success": true, "addresses": ips})
}
