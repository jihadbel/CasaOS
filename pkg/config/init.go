/*
 * @Author: LinkLeong link@icewhale.com
 * @Date: 2022-05-13 18:15:46
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-09-05 11:58:02
 * @FilePath: /CasaOS/pkg/config/init.go
 * @Description:
 * @Website: https://www.casaos.io
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package config

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/IceWhaleTech/CasaOS-Common/utils/constants"
	"github.com/IceWhaleTech/CasaOS/common"
	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/model"
	"github.com/go-ini/ini"
)

var (
	SysInfo = &model.SysInfoModel{}
	AppInfo = &model.APPModel{
		DBPath:       constants.DefaultDataPath,
		LogPath:      constants.DefaultLogPath,
		LogSaveName:  common.SERVICENAME,
		LogFileExt:   "log",
		ShellPath:    "/usr/share/casaos/shell",
		UserDataPath: filepath.Join(constants.DefaultDataPath, "conf"),
	}
	CommonInfo = &model.CommonModel{
		RuntimePath: constants.DefaultRuntimePath,
	}
	ServerInfo       = &model.ServerModel{}
	SystemConfigInfo = &model.SystemConfig{}
	FileSettingInfo  = &model.FileSetting{}

	Cfg            *ini.File
	ConfigFilePath string
)

func init() {
	if runtime.GOOS == "windows" {
		programData := os.Getenv("ProgramData")
		if len(programData) == 0 {
			programData = `C:\ProgramData`
		}
		casaData := filepath.Join(programData, "CasaOS")
		AppInfo.DBPath = filepath.Join(casaData, "db")
		AppInfo.LogPath = filepath.Join(casaData, "logs")
		AppInfo.ShellPath = filepath.Join(casaData, "shell")
		AppInfo.UserDataPath = filepath.Join(casaData, "conf")
		CommonInfo.RuntimePath = filepath.Join(casaData, "run")
		CasaOSConfigFilePath = filepath.Join(casaData, "conf", "casaos.conf")
	}
}

func DefaultStorageRoot() string {
	if root := os.Getenv("CASAOS_DATA_ROOT"); root != "" {
		if !filepath.IsAbs(root) {
			panic("CASAOS_DATA_ROOT must be an absolute path")
		}
		return filepath.Clean(root)
	}
	if runtime.GOOS == "windows" {
		return `C:\CasaOS\DATA`
	}
	return "/DATA"
}

// 初始化设置，获取系统的部分信息。
func InitSetup(config string, sample string) {
	ConfigFilePath = CasaOSConfigFilePath
	if len(config) > 0 {
		ConfigFilePath = config
	}

	createdConfig := false
	// create default config file if not exist
	if _, err := os.Stat(ConfigFilePath); os.IsNotExist(err) {
		createdConfig = true
		fmt.Println("config file not exist, create it")
		if err := safefs.WriteAtomic(ConfigFilePath, []byte(sample), 0600); err != nil {
			panic(err)
		}
	}

	var err error

	// 读取文件
	Cfg, err = ini.Load(ConfigFilePath)
	if err != nil {
		panic(err)
	}

	// Old Windows installations also received the Linux sample. Migrate only
	// those exact defaults, preserving every explicitly chosen Windows path.
	if runtime.GOOS == "windows" {
		migrated := false
		defaults := map[string]string{"DBPath": "/var/lib/casaos", "LogPath": "/var/log/casaos/", "ShellPath": "/usr/share/casaos/shell", "UserDataPath": "/var/lib/casaos/conf"}
		targets := map[string]string{"DBPath": AppInfo.DBPath, "LogPath": AppInfo.LogPath, "ShellPath": AppInfo.ShellPath, "UserDataPath": AppInfo.UserDataPath}
		for key, old := range defaults {
			k := Cfg.Section("app").Key(key)
			if filepath.ToSlash(filepath.Clean(k.String())) == filepath.ToSlash(filepath.Clean(old)) {
				if !createdConfig {
					// Preserve existing databases and user configuration. Resolve
					// the old drive-relative path once instead of silently starting
					// with an empty database in the new default location.
					resolved, err := filepath.Abs(k.String())
					if err == nil {
						if info, err := os.Stat(resolved); err == nil && info.IsDir() {
							k.SetValue(resolved)
							migrated = true
							continue
						}
					}
				}
				k.SetValue(targets[key])
				migrated = true
			}
		}
		if migrated {
			var contents bytes.Buffer
			if _, err := Cfg.WriteTo(&contents); err != nil {
				panic(err)
			}
			info, err := os.Stat(ConfigFilePath)
			if err != nil {
				panic(err)
			}
			if err := safefs.WriteAtomic(ConfigFilePath, contents.Bytes(), info.Mode().Perm()); err != nil {
				panic(err)
			}
		}
	}

	mapTo("app", AppInfo)
	mapTo("server", ServerInfo)
	mapTo("system", SystemConfigInfo)
	mapTo("file", FileSettingInfo)
	mapTo("common", CommonInfo)
	SystemConfigInfo.ConfigPath = ConfigFilePath
}

// 映射
func mapTo(section string, v interface{}) {
	err := Cfg.Section(section).MapTo(v)
	if err != nil {
		log.Fatalf("Cfg.MapTo %s err: %v", section, err)
	}
}
