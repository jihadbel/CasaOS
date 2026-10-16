package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
)

const maxTimelineItems = 2000
const maxTimelineEntries = 50000

var errTimelineLimit = errors.New("timeline scan limit reached")

// scanTimeline bounds both retained media and traversal, even for non-media trees.
// ReadDir batches avoid allocating every entry in a large directory at once.
func scanTimeline(ctx context.Context, roots []string, filter string, maxItems, maxEntries int) ([]MediaItem, bool) {
	items := make([]MediaItem, 0)
	seen := make(map[string]bool)
	visited, truncated := 0, false
	var walk func(string, int) error
	walk = func(dir string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if hiddenMediaPath(dir) {
			return nil
		}
		if depth > 128 {
			truncated = true
			return nil
		}
		if err := safefs.NoLinks(dir); err != nil {
			truncated = true
			return nil
		}
		folder, err := os.Open(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				truncated = true
			}
			return nil
		}
		defer folder.Close()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			entries, readErr := folder.ReadDir(128)
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				if visited >= maxEntries {
					return errTimelineLimit
				}
				visited++
				p := filepath.Join(dir, entry.Name())
				if strings.HasPrefix(entry.Name(), ".") || hiddenMediaPath(p) {
					continue
				}
				if entry.IsDir() {
					if err := walk(p, depth+1); err != nil {
						return err
					}
					continue
				}
				key := filepath.Clean(p)
				if runtime.GOOS == "windows" {
					key = strings.ToLower(key)
				}
				if !entry.Type().IsRegular() || seen[key] {
					continue
				}
				ext := strings.ToLower(filepath.Ext(entry.Name()))
				photo := strings.Contains("|.jpg|.jpeg|.png|.webp|.gif|.bmp|.svg|.heic|.heif|.avif|.raw|.ico|.tiff|.psd|.ai|", "|"+ext+"|")
				video := strings.Contains("|.mp4|.mkv|.webm|.mov|.avi|.flv|.wmv|.3gp|.m4v|.mpeg|", "|"+ext+"|")
				if !photo && !video || filter == "photos" && !photo || filter == "videos" && !video {
					continue
				}
				info, err := entry.Info()
				if err != nil {
					truncated = true
					continue
				}
				if !info.Mode().IsRegular() {
					continue
				}
				if len(items) >= maxItems {
					return errTimelineLimit
				}
				seen[key] = true
				items = append(items, MediaItem{
					Name: info.Name(), Path: filepath.ToSlash(p), Size: info.Size(), ModTime: info.ModTime(),
					DateStr:  info.ModTime().Format("2006-01-02"),
					MonthKey: fmt.Sprintf("%04d-%02d", info.ModTime().Year(), info.ModTime().Month()), IsVideo: video,
				})
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					truncated = true
				}
				return nil
			}
		}
	}
	for _, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if err := walk(abs, 0); err != nil {
			truncated = true
			break
		}
	}
	return items, truncated
}
