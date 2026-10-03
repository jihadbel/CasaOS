package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TrashItem represents a deleted item stored in Recycle Bin
type TrashItem struct {
	ID           string    `json:"id"`
	OriginalPath string    `json:"original_path"`
	TrashPath    string    `json:"trash_path"`
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	DeletedAt    time.Time `json:"deleted_at"`
	IsDir        bool      `json:"is_dir"`
}

const (
	RecycleBinDir      = `C:\CasaOS\DATA\RecycleBin`
	TrashManifestFile  = `C:\CasaOS\DATA\RecycleBin\trash_manifest.json`
)

// initTrashSystem ensures RecycleBin directory and manifest exist
func initTrashSystem() {
	_ = os.MkdirAll(RecycleBinDir, os.ModePerm)
	if _, err := os.Stat(TrashManifestFile); os.IsNotExist(err) {
		_ = os.WriteFile(TrashManifestFile, []byte("[]"), 0644)
	}
}

func loadTrashManifest() ([]TrashItem, error) {
	initTrashSystem()
	data, err := os.ReadFile(TrashManifestFile)
	if err != nil {
		return []TrashItem{}, nil
	}
	var items []TrashItem
	if err := json.Unmarshal(data, &items); err != nil {
		return []TrashItem{}, nil
	}
	return items, nil
}

func saveTrashManifest(items []TrashItem) error {
	initTrashSystem()
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(TrashManifestFile, data, 0644)
}

// setCORS sets common CORS headers for API requests
func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
}

// ============================================================================
// 1. In-Cloud Zip & Unzip Engine
// ============================================================================

type ZipRequest struct {
	Files  []string `json:"files"`
	Output string   `json:"output"`
}

func handleZip(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req ZipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Files) == 0 {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	// Default output zip name if not specified
	outputZip := req.Output
	if outputZip == "" {
		firstFile := req.Files[0]
		dir := filepath.Dir(firstFile)
		base := strings.TrimSuffix(filepath.Base(firstFile), filepath.Ext(firstFile))
		outputZip = filepath.Join(dir, base+".zip")
	}

	// Create destination directory if needed
	_ = os.MkdirAll(filepath.Dir(outputZip), os.ModePerm)

	zipFile, err := os.Create(outputZip)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"Failed to create zip file: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer zipFile.Close()

	archive := zip.NewWriter(zipFile)
	defer archive.Close()

	for _, target := range req.Files {
		info, err := os.Stat(target)
		if err != nil {
			continue
		}

		baseDir := filepath.Dir(target)
		err = filepath.Walk(target, func(path string, fileInfo os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Compute relative path inside archive using forward slashes
			relPath, err := filepath.Rel(baseDir, path)
			if err != nil {
				return err
			}
			relPath = filepath.ToSlash(relPath)

			header, err := zip.FileInfoHeader(fileInfo)
			if err != nil {
				return err
			}
			header.Name = relPath

			if fileInfo.IsDir() {
				header.Name += "/"
			} else {
				header.Method = zip.Deflate
			}

			writer, err := archive.CreateHeader(header)
			if err != nil {
				return err
			}

			if fileInfo.IsDir() {
				return nil
			}

			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()

			_, err = io.Copy(writer, f)
			return err
		})

		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"Error during compression: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		_ = info
	}

	_ = archive.Close()
	_ = zipFile.Close()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "تم ضغط الملفات بنجاح",
		"output":  filepath.ToSlash(outputZip),
	})
}

type UnzipRequest struct {
	Path string `json:"path"`
	Dest string `json:"dest"`
}

func handleUnzip(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req UnzipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		http.Error(w, `{"error":"Invalid request payload"}`, http.StatusBadRequest)
		return
	}

	zipReader, err := zip.OpenReader(req.Path)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"Failed to open zip file: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer zipReader.Close()

	// Default extraction directory: subfolder with same name as zip
	destDir := req.Dest
	if destDir == "" {
		zipDir := filepath.Dir(req.Path)
		baseName := strings.TrimSuffix(filepath.Base(req.Path), filepath.Ext(req.Path))
		destDir = filepath.Join(zipDir, baseName)
	}

	cleanDest := filepath.Clean(destDir)
	_ = os.MkdirAll(cleanDest, os.ModePerm)

	for _, file := range zipReader.File {
		targetPath := filepath.Join(cleanDest, file.Name)

		// Guard against ZipSlip vulnerability
		if !strings.HasPrefix(filepath.Clean(targetPath), cleanDest) {
			continue
		}

		if file.FileInfo().IsDir() {
			_ = os.MkdirAll(targetPath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), os.ModePerm); err != nil {
			continue
		}

		destFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			continue
		}

		srcFile, err := file.Open()
		if err != nil {
			destFile.Close()
			continue
		}

		_, _ = io.Copy(destFile, srcFile)
		destFile.Close()
		srcFile.Close()
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "تم فك الضغط بنجاح",
		"dest":    filepath.ToSlash(cleanDest),
	})
}

// ============================================================================
// 2. Smart Recycle Bin (Trash with Restore)
// ============================================================================

func handleTrash(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	path := r.URL.Path
	switch {
	case path == "/api/trash" || path == "/api/trash/":
		if r.Method == http.MethodGet {
			items, _ := loadTrashManifest()
			// Filter out items that might have been removed manually
			validItems := []TrashItem{}
			var totalSize int64 = 0
			for _, item := range items {
				if _, err := os.Stat(item.TrashPath); err == nil {
					validItems = append(validItems, item)
					totalSize += item.Size
				}
			}
			// Sort newest deleted first
			sort.Slice(validItems, func(i, j int) bool {
				return validItems[i].DeletedAt.After(validItems[j].DeletedAt)
			})
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":    true,
				"items":      validItems,
				"count":      len(validItems),
				"total_size": totalSize,
			})
			return
		}

	case strings.HasPrefix(path, "/api/trash/move"):
		if r.Method == http.MethodPost {
			var body struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
				http.Error(w, `{"error":"Invalid path"}`, http.StatusBadRequest)
				return
			}

			info, err := os.Stat(body.Path)
			if err != nil {
				http.Error(w, `{"error":"File not found"}`, http.StatusNotFound)
				return
			}

			initTrashSystem()
			id := uuid.NewString()[:8]
			baseName := filepath.Base(body.Path)
			trashTarget := filepath.Join(RecycleBinDir, fmt.Sprintf("%s_%s", id, baseName))

			// Move file or folder to Recycle Bin
			if err := os.Rename(body.Path, trashTarget); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"Failed to move to trash: %s"}`, err.Error()), http.StatusInternalServerError)
				return
			}

			manifest, _ := loadTrashManifest()
			newItem := TrashItem{
				ID:           id,
				OriginalPath: filepath.ToSlash(body.Path),
				TrashPath:    filepath.ToSlash(trashTarget),
				Name:         baseName,
				Size:         info.Size(),
				DeletedAt:    time.Now(),
				IsDir:        info.IsDir(),
			}
			manifest = append(manifest, newItem)
			_ = saveTrashManifest(manifest)

			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "تم نقل العنصر إلى سلة المحذوفات",
				"item":    newItem,
			})
			return
		}

	case strings.HasPrefix(path, "/api/trash/restore"):
		if r.Method == http.MethodPost {
			var body struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
				http.Error(w, `{"error":"Invalid ID"}`, http.StatusBadRequest)
				return
			}

			manifest, _ := loadTrashManifest()
			var targetItem *TrashItem
			remaining := []TrashItem{}

			for _, item := range manifest {
				if item.ID == body.ID {
					targetItem = &item
				} else {
					remaining = append(remaining, item)
				}
			}

			if targetItem == nil {
				http.Error(w, `{"error":"Item not found in trash manifest"}`, http.StatusNotFound)
				return
			}

			// Ensure original parent directory exists
			_ = os.MkdirAll(filepath.Dir(targetItem.OriginalPath), os.ModePerm)

			// Move back to original location
			if err := os.Rename(targetItem.TrashPath, targetItem.OriginalPath); err != nil {
				http.Error(w, fmt.Sprintf(`{"error":"Failed to restore: %s"}`, err.Error()), http.StatusInternalServerError)
				return
			}

			_ = saveTrashManifest(remaining)

			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "تم استرجاع العنصر بنجاح إلى مكانه الأصلي",
				"path":    targetItem.OriginalPath,
			})
			return
		}

	case strings.HasPrefix(path, "/api/trash/empty"):
		if r.Method == http.MethodPost {
			initTrashSystem()
			entries, _ := os.ReadDir(RecycleBinDir)
			for _, entry := range entries {
				if entry.Name() == "trash_manifest.json" {
					continue
				}
				_ = os.RemoveAll(filepath.Join(RecycleBinDir, entry.Name()))
			}
			_ = saveTrashManifest([]TrashItem{})

			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "تم إفراغ سلة المحذوفات نهائياً",
			})
			return
		}

	case strings.HasPrefix(path, "/api/trash/delete"):
		if r.Method == http.MethodPost || r.Method == http.MethodDelete {
			var body struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
				http.Error(w, `{"error":"Invalid ID"}`, http.StatusBadRequest)
				return
			}

			manifest, _ := loadTrashManifest()
			remaining := []TrashItem{}
			for _, item := range manifest {
				if item.ID == body.ID {
					_ = os.RemoveAll(item.TrashPath)
				} else {
					remaining = append(remaining, item)
				}
			}
			_ = saveTrashManifest(remaining)

			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"message": "تم حذف العنصر نهائياً",
			})
			return
		}
	}

	http.Error(w, `{"error":"Trash endpoint not found"}`, http.StatusNotFound)
}

// ============================================================================
// 3. Media Timeline & Smart Album Scanner
// ============================================================================

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
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Scan folders (default to Photos, Videos, Mobile_Uploads)
	scanDirs := []string{
		`C:\CasaOS\DATA\Photos`,
		`C:\CasaOS\DATA\Videos`,
		`C:\CasaOS\DATA\Mobile_Uploads`,
	}

	customFolder := r.URL.Query().Get("folder")
	if customFolder != "" {
		scanDirs = []string{customFolder}
	}

	filterType := r.URL.Query().Get("type") // "all", "photos", "videos"

	var allMedia []MediaItem

	photoExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
		".gif": true, ".bmp": true, ".svg": true, ".heic": true, ".heif": true,
	}
	videoExts := map[string]bool{
		".mp4": true, ".mkv": true, ".webm": true, ".mov": true,
		".avi": true, ".flv": true, ".wmv": true, ".3gp": true,
	}

	for _, dir := range scanDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}

			// Skip hidden files
			if strings.HasPrefix(info.Name(), ".") {
				return nil
			}

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
