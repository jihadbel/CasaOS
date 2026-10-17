package v1

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/storagepaths"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
)

type uploadLock struct {
	mu   sync.Mutex
	refs int
}

var uploadLocks = struct {
	sync.Mutex
	entries map[string]*uploadLock
}{entries: map[string]*uploadLock{}}

func lockUpload(key string) func() {
	key = uploadLockKey(key)
	uploadLocks.Lock()
	entry := uploadLocks.entries[key]
	if entry == nil {
		entry = &uploadLock{}
		uploadLocks.entries[key] = entry
	}
	entry.refs++
	uploadLocks.Unlock()
	entry.mu.Lock()
	return uploadUnlock(key, entry)
}

func uploadLockKey(key string) string {
	key = filepath.Clean(key)
	if runtime.GOOS == "windows" {
		return strings.ToLower(key)
	}
	return key
}

func uploadUnlock(key string, entry *uploadLock) func() {
	return func() {
		entry.mu.Unlock()
		uploadLocks.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(uploadLocks.entries, key)
		}
		uploadLocks.Unlock()
	}
}

var uploadDirectories = struct {
	sync.Mutex
	enabled bool
	roots   map[string]bool
}{roots: map[string]bool{}}

func registerUploadDirectory(dir string) {
	uploadDirectories.Lock()
	defer uploadDirectories.Unlock()
	if !uploadDirectories.enabled || uploadDirectories.roots[dir] {
		return
	}
	uploadDirectories.roots[dir] = true
	roots := make([]string, 0, len(uploadDirectories.roots))
	for root := range uploadDirectories.roots {
		roots = append(roots, root)
	}
	data, err := json.Marshal(roots)
	if err == nil {
		_ = safefs.WriteAtomic(filepath.Join(config.AppInfo.UserDataPath, "upload-directories.json"), data, 0600)
	}
}

// StartUploadCleanup tracks arbitrary upload locations across restarts and
// discovers legacy .temp directories inside configured storage once at startup.
func StartUploadCleanup(ctx context.Context) {
	uploadDirectories.Lock()
	if uploadDirectories.enabled {
		uploadDirectories.Unlock()
		return
	}
	uploadDirectories.enabled = true
	uploadDirectories.Unlock()
	var saved []string
	if input, err := os.Open(filepath.Join(config.AppInfo.UserDataPath, "upload-directories.json")); err == nil {
		_ = json.NewDecoder(io.LimitReader(input, 1<<20)).Decode(&saved)
		input.Close()
	}
	for _, root := range saved {
		if _, err := file.ValidateUserPath(filepath.Join(root, "upload-check")); err == nil {
			uploadDirectories.Lock()
			uploadDirectories.roots[root] = true
			uploadDirectories.Unlock()
		}
	}
	go func() {
		settings, _, err := storagepaths.Read()
		if err == nil {
			for _, root := range settings.Roots() {
				_ = file.WalkTree(ctx, root, 100000, func(path string, info os.FileInfo) error {
					if err := safefs.NoLinks(path); err != nil {
						if info.IsDir() {
							return filepath.SkipDir
						}
						return nil
					}
					if info.IsDir() && filepath.Base(path) == ".temp" {
						registerUploadDirectory(filepath.Dir(path))
						return filepath.SkipDir
					}
					if file.InternalStoragePath(path) && info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				})
			}
		}
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			uploadDirectories.Lock()
			roots := make([]string, 0, len(uploadDirectories.roots))
			for root := range uploadDirectories.roots {
				roots = append(roots, root)
			}
			uploadDirectories.Unlock()
			for _, root := range roots {
				if ctx.Err() != nil {
					return
				}
				cleanupExpiredUploads(root, time.Now())
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func cleanupExpiredUploads(root string, now time.Time) {
	temp := filepath.Join(root, ".temp")
	if !filepath.IsAbs(temp) || safefs.NoLinks(temp) != nil {
		return
	}
	folder, err := os.Open(temp)
	if err != nil {
		return
	}
	defer folder.Close()
	for {
		entries, readErr := folder.ReadDir(128)
		for _, entry := range entries {
			if !entry.IsDir() || len(entry.Name()) != 64 {
				continue
			}
			if _, err := hex.DecodeString(entry.Name()); err != nil {
				continue
			}
			path := filepath.Join(temp, entry.Name())
			key := uploadLockKey(path)
			// An active request owns a reference before touching its chunk tree.
			uploadLocks.Lock()
			if uploadLocks.entries[key] != nil {
				uploadLocks.Unlock()
				continue
			}
			lease := &uploadLock{refs: 1}
			lease.mu.Lock()
			uploadLocks.entries[key] = lease
			uploadLocks.Unlock()
			info, err := os.Lstat(path)
			if err == nil && now.Sub(info.ModTime()) > 24*time.Hour {
				cleanupUploadChunks(path)
			}
			uploadUnlock(key, lease)()
		}
		if readErr != nil {
			return
		}
	}
}
