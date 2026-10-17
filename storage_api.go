package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"sync"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/storagepaths"
)

type StorageSettings = storagepaths.Settings

var settingsMutex sync.Mutex

func defaultStorageSettings() StorageSettings { return storagepaths.Default() }

func storageSettingsFile() string { return storagepaths.File() }

func readStorageSettings() (StorageSettings, bool, error) { return storagepaths.Read() }

func validateStorageSettings(s *StorageSettings) error { return storagepaths.Validate(s) }

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
