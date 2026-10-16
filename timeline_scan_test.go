package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IceWhaleTech/CasaOS/pkg/config"
)

func TestTimelinePaginationIsOrderedBoundedAndDisjoint(t *testing.T) {
	root := t.TempDir()
	old := config.AppInfo.UserDataPath
	config.AppInfo.UserDataPath = filepath.Join(root, "conf")
	t.Cleanup(func() { config.AppInfo.UserDataPath = old })
	for i := 0; i < 7; i++ {
		path := filepath.Join(root, fmt.Sprintf("%d.jpg", i))
		writeTestFile(t, path, "photo")
		stamp := time.Date(2026, time.October, 1, i, 0, 0, 0, time.UTC)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	seen := make(map[string]bool)
	for offset := 0; offset < 8; offset += 2 {
		w := apiRequest(t, handleMediaTimeline, "GET", "/api/media/timeline?folder="+url.QueryEscape(root)+fmt.Sprintf("&limit=2&offset=%d", offset), nil)
		var data struct {
			Total, Offset, Limit int
			HasMore              bool `json:"has_more"`
			Truncated            bool
			Timeline             []TimelineGroup
		}
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || w.Code != 200 {
			t.Fatal(w.Code, w.Body.String(), err)
		}
		if data.Total != 7 || data.Limit != 2 || data.Offset != offset || data.HasMore != (offset < 6) || data.Truncated {
			t.Fatal(data)
		}
		var page []MediaItem
		for _, group := range data.Timeline {
			page = append(page, group.Items...)
		}
		if len(page) > 2 {
			t.Fatal("unbounded page", len(page))
		}
		for i, item := range page {
			if seen[item.Path] || item.Name != fmt.Sprintf("%d.jpg", 6-offset-i) {
				t.Fatal("duplicate or unordered page", item)
			}
			seen[item.Path] = true
		}
	}
	if len(seen) != 7 {
		t.Fatal("pagination lost media", seen)
	}
	for _, query := range []string{"limit=0", "limit=-1", "limit=201", "limit=garbage", "offset=-1", "offset=2001", "offset=99999999999999999999", "type=invalid"} {
		if w := apiRequest(t, handleMediaTimeline, "GET", "/api/media/timeline?"+query, nil); w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
}

func TestTimelineTraversalCapsMediaEntriesAndCancellation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 6; i++ {
		writeTestFile(t, filepath.Join(root, fmt.Sprintf("%d.jpg", i)), "photo")
	}
	items, truncated := scanTimeline(context.Background(), []string{root}, "photos", 2, 100)
	if len(items) != 2 || !truncated {
		t.Fatal("media cap not enforced", len(items), truncated)
	}
	items, truncated = scanTimeline(context.Background(), []string{root, root}, "photos", 10, 100)
	if len(items) != 6 || truncated {
		t.Fatal("overlapping roots duplicated media", len(items), truncated)
	}
	items, truncated = scanTimeline(context.Background(), []string{root}, "videos", 10, 3)
	if len(items) != 0 || !truncated {
		t.Fatal("non-matching entries bypassed traversal budget", len(items), truncated)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items, truncated = scanTimeline(ctx, []string{root}, "all", 10, 100)
	if len(items) != 0 || !truncated {
		t.Fatal("cancelled scan continued", len(items), truncated)
	}
}

func TestTimelineAPIReportsPartialResultsAtMediaCap(t *testing.T) {
	root := t.TempDir()
	old := config.AppInfo.UserDataPath
	config.AppInfo.UserDataPath = filepath.Join(root, "conf")
	t.Cleanup(func() { config.AppInfo.UserDataPath = old })
	for i := 0; i <= maxTimelineItems; i++ {
		writeTestFile(t, filepath.Join(root, fmt.Sprintf("%04d.jpg", i)), "photo")
	}
	w := apiRequest(t, handleMediaTimeline, "GET", "/api/media/timeline?folder="+url.QueryEscape(root), nil)
	var data struct {
		Total     int
		Truncated bool
		HasMore   bool `json:"has_more"`
		Timeline  []TimelineGroup
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || w.Code != 200 {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	count := 0
	for _, group := range data.Timeline {
		count += len(group.Items)
	}
	if data.Total > maxTimelineItems || !data.Truncated || !data.HasMore || count != 100 {
		t.Fatal(data.Total, data.Truncated, data.HasMore, count)
	}
}
