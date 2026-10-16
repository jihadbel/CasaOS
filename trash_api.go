package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/google/uuid"
)

type TrashItem struct {
	ID           string    `json:"id"`
	OriginalPath string    `json:"original_path"`
	TrashPath    string    `json:"trash_path"`
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	DeletedAt    time.Time `json:"deleted_at"`
	IsDir        bool      `json:"is_dir"`
}

var (
	RecycleBinDir = filepath.Join(config.DefaultStorageRoot(), "RecycleBin")
	trashMutex    sync.Mutex
)

func trashManifestPath() string { return filepath.Join(RecycleBinDir, "trash_manifest.json") }

// Keep recycled content on its original volume, while retaining one central index.
func trashDirectory(source string) string {
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.VolumeName(source), filepath.VolumeName(RecycleBinDir)) {
		return filepath.Join(filepath.VolumeName(source)+string(os.PathSeparator), ".CasaOS-RecycleBin")
	}
	return RecycleBinDir
}

func initTrashSystem() error {
	if err := safefs.NoLinks(RecycleBinDir); err != nil {
		return err
	}
	if err := os.MkdirAll(RecycleBinDir, 0700); err != nil {
		return err
	}
	return nil
}

func loadTrashManifest() ([]TrashItem, error) {
	if err := initTrashSystem(); err != nil {
		return nil, err
	}
	if err := safefs.NoLinks(trashManifestPath()); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(trashManifestPath())
	if os.IsNotExist(err) {
		return []TrashItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []TrashItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("recycle bin index is damaged; original index retained: %w", err)
	}
	for _, item := range items {
		if err := validateTrashItem(item); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func saveTrashManifest(items []TrashItem) error {
	if err := initTrashSystem(); err != nil {
		return err
	}
	if items == nil {
		items = []TrashItem{}
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return safefs.WriteAtomic(trashManifestPath(), data, 0600)
}

func validateTrashItem(item TrashItem) error {
	if item.ID == "" || filepath.Base(item.ID) != item.ID || strings.ContainsAny(item.ID, `/:`) {
		return errors.New("invalid recycle bin item ID")
	}
	if !filepath.IsAbs(item.OriginalPath) || !filepath.IsAbs(item.TrashPath) {
		return errors.New("recycle bin item paths must be absolute")
	}
	root, err := filepath.Abs(trashDirectory(item.OriginalPath))
	if err != nil {
		return err
	}
	target, err := filepath.Abs(item.TrashPath)
	if err != nil {
		return err
	}
	// Validate legacy eight-character IDs as well as new full UUIDs.
	if !safefs.Within(root, target) || !strings.HasPrefix(filepath.Base(target), item.ID+"_") {
		return errors.New("recycle bin item points outside managed storage")
	}
	return safefs.NoLinks(filepath.Dir(target))
}

func handleTrash(w http.ResponseWriter, r *http.Request) {
	trashMutex.Lock()
	defer trashMutex.Unlock()
	items, err := loadTrashManifest()
	if err != nil {
		apiError(w, 500, err)
		return
	}
	switch r.URL.Path {
	case "/api/trash", "/api/trash/":
		if r.Method != http.MethodGet {
			apiError(w, 405, errors.New("method not allowed"))
			return
		}
		valid := []TrashItem{}
		var total int64
		for _, item := range items {
			if _, err := os.Lstat(item.TrashPath); err == nil {
				valid = append(valid, item)
				total += item.Size
			} else if !os.IsNotExist(err) {
				apiError(w, 500, err)
				return
			}
		}
		sort.Slice(valid, func(i, j int) bool { return valid[i].DeletedAt.After(valid[j].DeletedAt) })
		apiJSON(w, 200, map[string]interface{}{"success": true, "items": valid, "count": len(valid), "total_size": total})
	case "/api/trash/move":
		if r.Method != http.MethodPost {
			apiError(w, 405, errors.New("method not allowed"))
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		if err := decodeAPI(w, r, &body); err != nil || !filepath.IsAbs(body.Path) {
			apiError(w, 400, errors.New("an absolute source path is required"))
			return
		}
		source, err := filepath.Abs(body.Path)
		if err != nil {
			apiError(w, 400, err)
			return
		}
		root, err := filepath.Abs(trashDirectory(source))
		if err != nil {
			apiError(w, 400, err)
			return
		}
		central, _ := filepath.Abs(RecycleBinDir)
		if sameFilePath(source, filepath.Dir(source)) || sameFilePath(source, root) || safefs.Within(source, root) || sameFilePath(source, central) || safefs.Within(source, central) || safefs.Within(root, source) || safefs.Within(central, source) {
			apiError(w, 400, errors.New("cannot recycle a storage root or recycle bin content"))
			return
		}
		if err := safefs.NoLinks(source); err != nil {
			apiError(w, 400, err)
			return
		}
		info, err := os.Lstat(source)
		if err != nil {
			apiError(w, 404, err)
			return
		}
		if err := safefs.NoLinks(root); err != nil {
			apiError(w, 400, err)
			return
		}
		if err := os.MkdirAll(root, 0700); err != nil {
			apiError(w, 500, err)
			return
		}
		id := uuid.NewString()
		item := TrashItem{ID: id, OriginalPath: filepath.ToSlash(source), TrashPath: filepath.ToSlash(filepath.Join(root, id+"_"+filepath.Base(source))), Name: filepath.Base(source), Size: info.Size(), DeletedAt: time.Now(), IsDir: info.IsDir()}
		// Persist the recovery path before moving the source. A crash cannot orphan a moved file.
		pending := append(append([]TrashItem{}, items...), item)
		if err := saveTrashManifest(pending); err != nil {
			apiError(w, 500, err)
			return
		}
		if err := safefs.NoReplace(source, item.TrashPath); err != nil {
			rollback := saveTrashManifest(items)
			apiError(w, 500, errors.Join(err, rollback))
			return
		}
		apiJSON(w, 200, map[string]interface{}{"success": true, "item": item})
	case "/api/trash/restore", "/api/trash/delete":
		if r.Method != http.MethodPost && !(r.URL.Path == "/api/trash/delete" && r.Method == http.MethodDelete) {
			apiError(w, 405, errors.New("method not allowed"))
			return
		}
		var body struct {
			ID string `json:"id"`
		}
		if err := decodeAPI(w, r, &body); err != nil || body.ID == "" {
			apiError(w, 400, errors.New("item ID is required"))
			return
		}
		index := -1
		for i := range items {
			if items[i].ID == body.ID {
				index = i
				break
			}
		}
		if index < 0 {
			apiError(w, 404, errors.New("item not found"))
			return
		}
		item := items[index]
		if err := validateTrashItem(item); err != nil {
			apiError(w, 400, err)
			return
		}
		remaining := append(append([]TrashItem{}, items[:index]...), items[index+1:]...)
		if r.URL.Path == "/api/trash/restore" {
			if err := safefs.NoLinks(item.OriginalPath); err != nil {
				apiError(w, 400, err)
				return
			}
			if err := os.MkdirAll(filepath.Dir(item.OriginalPath), 0755); err != nil {
				apiError(w, 500, err)
				return
			}
			if err := safefs.NoReplace(item.TrashPath, item.OriginalPath); err != nil {
				apiError(w, 409, err)
				return
			}
			if err := saveTrashManifest(remaining); err != nil {
				rollback := safefs.NoReplace(item.OriginalPath, item.TrashPath)
				apiError(w, 500, errors.Join(err, rollback))
				return
			}
		} else {
			// validateTrashItem checked the resolved absolute target against its managed bin.
			if err := os.RemoveAll(item.TrashPath); err != nil {
				apiError(w, 500, err)
				return
			}
			if err := saveTrashManifest(remaining); err != nil {
				apiError(w, 500, err)
				return
			}
		}
		apiJSON(w, 200, map[string]interface{}{"success": true, "path": item.OriginalPath})
	case "/api/trash/empty":
		if r.Method != http.MethodPost {
			apiError(w, 405, errors.New("method not allowed"))
			return
		}
		// Only remove indexed, validated items; never sweep unknown recovery content.
		for _, item := range items {
			if err := validateTrashItem(item); err != nil {
				apiError(w, 400, err)
				return
			}
			if err := os.RemoveAll(item.TrashPath); err != nil {
				apiError(w, 500, err)
				return
			}
		}
		if err := saveTrashManifest([]TrashItem{}); err != nil {
			apiError(w, 500, err)
			return
		}
		apiJSON(w, 200, map[string]bool{"success": true})
	default:
		apiError(w, 404, errors.New("trash endpoint not found"))
	}
}
