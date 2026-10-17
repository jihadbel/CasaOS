/*
 * @Author: LinkLeong link@icewhale.org
 * @Date: 2022-07-26 11:21:14
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-08-18 11:16:25
 * @FilePath: /CasaOS/service/shares.go
 * @Description:
 * @Website: https://www.casaos.io
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package service

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
	"github.com/IceWhaleTech/CasaOS/service/model"
	model2 "github.com/IceWhaleTech/CasaOS/service/model"
	"gorm.io/gorm"
)

type SharesService interface {
	GetSharesList() (shares []model2.SharesDBModel)
	GetSharesByPath(path string) (shares []model2.SharesDBModel)
	GetSharesByName(name string) (shares []model2.SharesDBModel)
	CreateShare(share model2.SharesDBModel) error
	DeleteShare(id string) error
	UpdateConfigFile() error
	InitSambaConfig()
	DeleteShareByPath(path string) error
}

type sharesStruct struct {
	db *gorm.DB
}

func (s *sharesStruct) DeleteShareByPath(path string) error {
	return s.changeShares(func(db *gorm.DB) error {
		return db.Where("path LIKE ?", path+"%").Delete(&model.SharesDBModel{}).Error
	})
}

func (s *sharesStruct) GetSharesByName(name string) (shares []model2.SharesDBModel) {
	s.db.Select("anonymous,read_only,name,path,id").Where("LOWER(name) = LOWER(?)", name).Find(&shares)
	return
}

func (s *sharesStruct) GetSharesByPath(path string) (shares []model2.SharesDBModel) {
	s.db.Select("anonymous,read_only,name,path,id").Where("path = ?", path).Find(&shares)
	return
}

func (s *sharesStruct) GetSharesList() (shares []model2.SharesDBModel) {
	s.db.Select("anonymous,read_only,name,path,id").Find(&shares)
	return
}

func (s *sharesStruct) CreateShare(share model2.SharesDBModel) error {
	if err := ValidateSharePath(share.Path); err != nil {
		return err
	}
	name, err := ShareName(share.Path, share.Name)
	if err != nil {
		return err
	}
	share.Name = name
	s.InitSambaConfig()
	return s.changeShares(func(db *gorm.DB) error { return db.Create(&share).Error })
}

// A share exports a whole tree; reject internal data and its ancestors.
func ValidateSharePath(path string) error {
	if strings.ContainsFunc(path, unicode.IsControl) {
		return fmt.Errorf("share paths cannot contain control characters")
	}
	if _, err := file.ValidateUserPath(path); err != nil {
		return err
	}
	for _, root := range []string{config.AppInfo.UserDataPath, config.AppInfo.DBPath, config.CommonInfo.RuntimePath, config.ConfigFilePath} {
		if root != "" && safefs.Within(path, root) {
			return fmt.Errorf("a share cannot contain internal storage")
		}
	}
	if safefs.Within(path, filepath.Join(config.DefaultStorageRoot(), "RecycleBin")) {
		return fmt.Errorf("a share cannot contain the recycle bin")
	}
	return nil
}

// ShareName keeps persisted names stable and gives drive roots a valid default.
func ShareName(path, requested string) (string, error) {
	name := requested
	if name == "" {
		clean := filepath.Clean(path)
		name = filepath.Base(clean)
		if filepath.Dir(clean) == clean {
			volume := filepath.VolumeName(clean)
			if len(volume) == 2 && volume[1] == ':' {
				name = volume[:1] + "_Drive"
			} else if volume == "" {
				name = "Root"
			}
		}
	}
	if name == "" || name == "." || name == ".." || len(name) > 80 || strings.TrimSpace(name) != name || strings.HasSuffix(name, ".") || strings.ContainsAny(name, `"/\[]:;|=,+*?<>`) || strings.ContainsFunc(name, unicode.IsControl) {
		return "", fmt.Errorf("invalid share name: %q", name)
	}
	return name, nil
}

func (s *sharesStruct) DeleteShare(id string) error {
	return s.changeShares(func(db *gorm.DB) error { return db.Where("id= ?", id).Delete(&model.SharesDBModel{}).Error })
}

func (s *sharesStruct) changeShares(change func(*gorm.DB) error) error {
	return s.db.Transaction(func(db *gorm.DB) error {
		if err := change(db); err != nil {
			return err
		}
		return (&sharesStruct{db: db}).UpdateConfigFile()
	})
}

func NewSharesService(db *gorm.DB) SharesService {
	return &sharesStruct{db: db}
}
