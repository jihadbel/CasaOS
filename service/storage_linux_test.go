//go:build linux

package service

import (
	"os"
	"testing"
)

func TestCloudConfigReturnsUnavailableBackendError(t *testing.T) {
	if _, err := os.Stat("/var/run/rclone/rclone.sock"); !os.IsNotExist(err) {
		t.Skip("do not send configuration writes to an existing rclone service")
	}
	if err := NewStorageService().CreateConfig(map[string]string{}, "test-unavailable-backend", "drive"); err == nil {
		t.Fatal("storage service swallowed the rclone failure")
	}
}
