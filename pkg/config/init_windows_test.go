package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsSampleDoesNotOverrideNativeDefaults(t *testing.T) {
	savedApp, savedServer, savedSystem, savedCommon := *AppInfo, *ServerInfo, *SystemConfigInfo, *CommonInfo
	savedCfg, savedPath := Cfg, ConfigFilePath
	t.Cleanup(func() {
		*AppInfo = savedApp
		*ServerInfo = savedServer
		*SystemConfigInfo = savedSystem
		*CommonInfo = savedCommon
		Cfg = savedCfg
		ConfigFilePath = savedPath
	})
	sample, err := os.ReadFile(filepath.Join("..", "..", "build", "sysroot", "etc", "casaos", "casaos.conf.sample"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "casaos.conf")
	InitSetup(path, string(sample))
	if AppInfo.DBPath != savedApp.DBPath || AppInfo.LogPath != savedApp.LogPath || AppInfo.ShellPath != savedApp.ShellPath || AppInfo.UserDataPath != savedApp.UserDataPath {
		t.Fatal("Linux sample replaced Windows defaults", *AppInfo)
	}
	if SystemConfigInfo.ConfigPath != path {
		t.Fatal("configuration writes use the wrong destination")
	}
}

func TestWindowsExplicitStorageConfigurationIsPreserved(t *testing.T) {
	savedApp, savedServer, savedSystem, savedCommon := *AppInfo, *ServerInfo, *SystemConfigInfo, *CommonInfo
	savedCfg, savedPath := Cfg, ConfigFilePath
	t.Cleanup(func() {
		*AppInfo = savedApp
		*ServerInfo = savedServer
		*SystemConfigInfo = savedSystem
		*CommonInfo = savedCommon
		Cfg = savedCfg
		ConfigFilePath = savedPath
	})
	root := t.TempDir()
	path := filepath.Join(root, "casaos.conf")
	custom := filepath.Join(root, "custom-db")
	sample := "[app]\nDBPath = " + custom + "\n"
	if err := os.WriteFile(path, []byte(sample), 0600); err != nil {
		t.Fatal(err)
	}
	InitSetup(path, sample)
	if AppInfo.DBPath != custom {
		t.Fatal("custom database path changed", AppInfo.DBPath)
	}
}

func TestWindowsUnchangedReadonlyConfigurationStartsWithoutRewrite(t *testing.T) {
	savedApp, savedServer, savedSystem, savedCommon := *AppInfo, *ServerInfo, *SystemConfigInfo, *CommonInfo
	savedCfg, savedPath := Cfg, ConfigFilePath
	t.Cleanup(func() {
		*AppInfo = savedApp
		*ServerInfo = savedServer
		*SystemConfigInfo = savedSystem
		*CommonInfo = savedCommon
		Cfg = savedCfg
		ConfigFilePath = savedPath
	})
	path := filepath.Join(t.TempDir(), "readonly.conf")
	content := "[app]\nDBPath = " + filepath.Dir(path) + "\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	InitSetup(path, "")
	if data, err := os.ReadFile(path); err != nil || string(data) != content {
		t.Fatal("unchanged config was rewritten", string(data), err)
	}
}
