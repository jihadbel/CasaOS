package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
)

type MediaItem struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	DateStr  string    `json:"date_str"`
	MonthKey string    `json:"month_key"` // e.g., "2026-10"
	IsVideo  bool      `json:"is_video"`
}

type TimelineGroup struct {
	Period string      `json:"period"` // e.g., "أكتوبر 2026"
	Key    string      `json:"key"`
	Items  []MediaItem `json:"items"`
}

var arabicMonths = map[time.Month]string{
	time.January:   "يناير",
	time.February:  "فبراير",
	time.March:     "مارس",
	time.April:     "أبريل",
	time.May:       "مايو",
	time.June:      "يونيو",
	time.July:      "يوليو",
	time.August:    "أغسطس",
	time.September: "سبتمبر",
	time.October:   "أكتوبر",
	time.November:  "نوفمبر",
	time.December:  "ديسمبر",
}

func handleMediaTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, 405, errors.New("method not allowed"))
		return
	}

	// Scan folders (default to Photos, Videos, Mobile_Uploads)
	settings, _, err := readStorageSettings()
	if err != nil {
		apiError(w, 500, err)
		return
	}
	scanDirs := []string{settings.PhotosPath, settings.VideosPath, settings.MobilePath}

	customFolder := r.URL.Query().Get("folder")
	if customFolder != "" {
		scanDirs = []string{customFolder}
	}

	filterType := r.URL.Query().Get("type") // "all", "photos", "videos"

	var allMedia []MediaItem

	photoExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
		".gif": true, ".bmp": true, ".svg": true, ".heic": true, ".heif": true,
		".avif": true, ".raw": true, ".ico": true, ".tiff": true, ".psd": true, ".ai": true,
	}
	videoExts := map[string]bool{
		".mp4": true, ".mkv": true, ".webm": true, ".mov": true,
		".avi": true, ".flv": true, ".wmv": true, ".3gp": true,
		".m4v": true, ".mpeg": true,
	}

	seen := map[string]bool{}
	for _, dir := range scanDirs {
		// Also reject roots inside an internal directory when explicitly selected.
		if hiddenMediaPath(dir) {
			continue
		}
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				if hiddenMediaPath(p) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(info.Name(), ".") {
				return nil
			}
			if !info.Mode().IsRegular() || seen[filepath.Clean(p)] {
				return nil
			}
			seen[filepath.Clean(p)] = true

			ext := strings.ToLower(filepath.Ext(info.Name()))
			isPhoto := photoExts[ext]
			isVideo := videoExts[ext]

			if !isPhoto && !isVideo {
				return nil
			}

			if filterType == "photos" && !isPhoto {
				return nil
			}
			if filterType == "videos" && !isVideo {
				return nil
			}

			monthKey := fmt.Sprintf("%04d-%02d", info.ModTime().Year(), info.ModTime().Month())
			allMedia = append(allMedia, MediaItem{
				Name:     info.Name(),
				Path:     filepath.ToSlash(p),
				Size:     info.Size(),
				ModTime:  info.ModTime(),
				DateStr:  info.ModTime().Format("2006-01-02"),
				MonthKey: monthKey,
				IsVideo:  isVideo,
			})
			return nil
		})
	}

	// Sort newest first
	sort.Slice(allMedia, func(i, j int) bool {
		return allMedia[i].ModTime.After(allMedia[j].ModTime)
	})

	// Group by Year-Month
	groupsMap := make(map[string][]MediaItem)
	var groupKeys []string

	for _, item := range allMedia {
		if _, exists := groupsMap[item.MonthKey]; !exists {
			groupKeys = append(groupKeys, item.MonthKey)
		}
		groupsMap[item.MonthKey] = append(groupsMap[item.MonthKey], item)
	}

	var timeline []TimelineGroup
	for _, key := range groupKeys {
		items := groupsMap[key]
		if len(items) == 0 {
			continue
		}
		firstDate := items[0].ModTime
		periodName := fmt.Sprintf("%s %d", arabicMonths[firstDate.Month()], firstDate.Year())
		timeline = append(timeline, TimelineGroup{
			Period: periodName,
			Key:    key,
			Items:  items,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"total":    len(allMedia),
		"timeline": timeline,
	})
}

func hiddenMediaPath(path string) bool {
	clean, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	bin, err := filepath.Abs(RecycleBinDir)
	if err == nil && (sameFilePath(clean, bin) || safefs.Within(bin, clean)) {
		return true
	}
	for p := clean; ; p = filepath.Dir(p) {
		name := filepath.Base(p)
		if strings.HasPrefix(name, ".") || strings.EqualFold(name, "$RECYCLE.BIN") {
			return true
		}
		if filepath.Dir(p) == p {
			return false
		}
	}
}
