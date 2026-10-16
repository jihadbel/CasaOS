package service

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitDiskRefresh(t *testing.T, cache *diskInfoCache) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		cache.mu.Lock()
		done := !cache.refreshing
		cache.mu.Unlock()
		if done {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("disk refresh did not finish")
}

func TestDiskSnapshotDoesNotBlockOrDuplicateSlowProbes(t *testing.T) {
	var cache diskInfoCache
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	read := func() ([]map[string]interface{}, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return []map[string]interface{}{{"path": "test", "total": uint64(100)}}, nil
	}
	cache.snapshot(read)
	<-started
	var requests sync.WaitGroup
	for i := 0; i < 50; i++ {
		requests.Add(1)
		go func() {
			defer requests.Done()
			if len(cache.snapshot(read)) != 0 {
				t.Error("initial snapshot should be empty")
			}
		}()
	}
	done := make(chan struct{})
	go func() { requests.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("requests blocked behind disk probe")
	}
	if calls.Load() != 1 {
		close(release)
		t.Fatal("duplicate probes", calls.Load())
	}
	close(release)
	waitDiskRefresh(t, &cache)
	items := cache.snapshot(read)
	if len(items) != 1 || calls.Load() != 1 {
		t.Fatal(items, calls.Load())
	}
	items[0]["path"] = "changed by caller"
	if cache.snapshot(read)[0]["path"] != "test" {
		t.Fatal("cache snapshot exposed mutable map")
	}
}

func TestDiskSnapshotRetainsLastReadingOnRefreshFailure(t *testing.T) {
	cache := diskInfoCache{items: []map[string]interface{}{{"path": "previous"}}}
	items := cache.snapshot(func() ([]map[string]interface{}, error) { return nil, errors.New("device temporarily unavailable") })
	if len(items) != 1 || items[0]["path"] != "previous" {
		t.Fatal(items)
	}
	waitDiskRefresh(t, &cache)
	items = cache.snapshot(func() ([]map[string]interface{}, error) { t.Fatal("probe ran inside cache lifetime"); return nil, nil })
	if len(items) != 1 || items[0]["path"] != "previous" {
		t.Fatal(items)
	}
}
