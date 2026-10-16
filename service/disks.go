package service

import (
	"sync"
	"time"
)

// One background refresh per service keeps slow media probes off HTTP handlers
// and bounds the number of probes even if a device stops responding.
type diskInfoCache struct {
	mu         sync.Mutex
	items      []map[string]interface{}
	next       time.Time
	refreshing bool
}

func cloneDisks(items []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, len(items))
	for index, item := range items {
		result[index] = make(map[string]interface{}, len(item))
		for key, value := range item {
			result[index][key] = value
		}
	}
	return result
}

func (c *diskInfoCache) snapshot(read func() ([]map[string]interface{}, error)) []map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.refreshing && !time.Now().Before(c.next) {
		c.refreshing = true
		go func() {
			items, err := read()
			c.mu.Lock()
			defer c.mu.Unlock()
			if err == nil {
				c.items = cloneDisks(items)
			}
			c.next = time.Now().Add(10 * time.Second)
			c.refreshing = false
		}()
	}
	return cloneDisks(c.items)
}
