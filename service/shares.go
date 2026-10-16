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
	s.db.Select("anonymous,path,id").Where("name = ?", name).Find(&shares)
	return
}

func (s *sharesStruct) GetSharesByPath(path string) (shares []model2.SharesDBModel) {
	s.db.Select("anonymous,path,id").Where("path = ?", path).Find(&shares)
	return
}

func (s *sharesStruct) GetSharesList() (shares []model2.SharesDBModel) {
	s.db.Select("anonymous,path,id").Find(&shares)
	return
}

func (s *sharesStruct) CreateShare(share model2.SharesDBModel) error {
	s.InitSambaConfig()
	return s.changeShares(func(db *gorm.DB) error { return db.Create(&share).Error })
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
