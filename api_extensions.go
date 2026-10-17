package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
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
	limit, offset := 100, 0
	for name, target := range map[string]*int{"limit": &limit, "offset": &offset} {
		if raw := r.URL.Query().Get(name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 0 || name == "limit" && (value == 0 || value > 200) || name == "offset" && value > maxTimelineItems {
				apiError(w, 400, fmt.Errorf("invalid %s", name))
				return
			}
			*target = value
		}
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
		clean, err := file.ValidateStoragePath(customFolder)
		if err != nil {
			apiError(w, http.StatusBadRequest, err)
			return
		}
		scanDirs = []string{clean}
	}
	for _, dir := range scanDirs {
		if _, err := file.ValidateStoragePath(dir); err != nil {
			apiError(w, http.StatusBadRequest, err)
			return
		}
	}

	filterType := r.URL.Query().Get("type") // "all", "photos", "videos"
	if filterType != "" && filterType != "all" && filterType != "photos" && filterType != "videos" {
		apiError(w, 400, errors.New("invalid media type"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	allMedia, truncated := scanTimeline(ctx, scanDirs, filterType, maxTimelineItems, maxTimelineEntries)
	if r.Context().Err() != nil {
		return
	}

	// Sort newest first
	sort.Slice(allMedia, func(i, j int) bool {
		if allMedia[i].ModTime.Equal(allMedia[j].ModTime) {
			return allMedia[i].Path < allMedia[j].Path
		}
		return allMedia[i].ModTime.After(allMedia[j].ModTime)
	})
	total := len(allMedia)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	// Group by Year-Month
	groupsMap := make(map[string][]MediaItem)
	var groupKeys []string

	for _, item := range allMedia[start:end] {
		if _, exists := groupsMap[item.MonthKey]; !exists {
			groupKeys = append(groupKeys, item.MonthKey)
		}
		groupsMap[item.MonthKey] = append(groupsMap[item.MonthKey], item)
	}

	timeline := []TimelineGroup{}
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

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"total":     total,
		"offset":    offset,
		"limit":     limit,
		"has_more":  end < total,
		"truncated": truncated,
		"timeline":  timeline,
	})
}

func hiddenMediaPath(path string) bool {
	if file.InternalStoragePath(path) {
		return true
	}
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
