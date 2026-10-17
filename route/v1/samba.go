/*
 * @Author: LinkLeong link@icewhale.com
 * @Date: 2022-07-26 11:08:48
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-08-17 18:25:42
 * @FilePath: /CasaOS/route/v1/samba.go
 * @Description:
 * @Website: https://www.casaos.io
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package v1

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/IceWhaleTech/CasaOS-Common/utils/logger"
	"github.com/labstack/echo/v4"
	"go.uber.org/zap"

	"github.com/IceWhaleTech/CasaOS/model"
	"github.com/IceWhaleTech/CasaOS/pkg/samba"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/common_err"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
	"github.com/IceWhaleTech/CasaOS/service"
	model2 "github.com/IceWhaleTech/CasaOS/service/model"
)

// service

func GetSambaStatus(ctx echo.Context) error {
	status, needInit, err := service.GetSambaStatus()
	if err != nil || !status {
		return ctx.JSON(http.StatusInternalServerError, model.Result{
			Success: common_err.SERVICE_NOT_RUNNING,
			Message: common_err.GetMsg(common_err.SERVICE_NOT_RUNNING),
		})
	}

	data := make(map[string]string, 1)
	data["need_init"] = fmt.Sprintf("%v", needInit)
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

func GetSambaSharesList(ctx echo.Context) error {
	shares := service.MyService.Shares().GetSharesList()
	shareList := []model.Shares{}
	for _, v := range shares {
		shareList = append(shareList, model.Shares{
			Anonymous: v.Anonymous,
			ReadOnly:  v.ReadOnly,
			Path:      v.Path,
			ID:        v.ID,
			Name:      v.Name,
		})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: shareList})
}

func PostSambaSharesCreate(ctx echo.Context) error {
	shares := []model.Shares{}
	if err := ctx.Bind(&shares); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid share request")
	}
	names, paths := map[string]bool{}, map[string]bool{}
	for i := range shares {
		v := &shares[i]
		if runtime.GOOS == "windows" && v.Anonymous {
			return echo.NewHTTPError(http.StatusBadRequest, "Anonymous Windows SMB shares are not supported")
		}
		if v.Path == "" {
			return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INSUFFICIENT_PERMISSIONS, Message: common_err.GetMsg(common_err.INSUFFICIENT_PERMISSIONS)})
		}
		if !filepath.IsAbs(v.Path) {
			return echo.NewHTTPError(http.StatusBadRequest, "Share path must be absolute")
		}
		v.Path = filepath.Clean(v.Path)
		if err := service.ValidateSharePath(v.Path); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		info, statErr := os.Stat(v.Path)
		if statErr != nil || !info.IsDir() {
			return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.DIR_NOT_EXISTS, Message: common_err.GetMsg(common_err.DIR_NOT_EXISTS)})
		}
		name, err := service.ShareName(v.Path, v.Name)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		v.Name = name
		pathKey, nameKey := v.Path, strings.ToLower(v.Name)
		if runtime.GOOS == "windows" {
			pathKey = strings.ToLower(pathKey)
		}
		if paths[pathKey] || len(service.MyService.Shares().GetSharesByPath(v.Path)) > 0 {
			return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.SHARE_ALREADY_EXISTS, Message: common_err.GetMsg(common_err.SHARE_ALREADY_EXISTS)})
		}
		if names[nameKey] || len(service.MyService.Shares().GetSharesByName(v.Name)) > 0 {
			return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.SHARE_NAME_ALREADY_EXISTS, Message: common_err.GetMsg(common_err.SHARE_NAME_ALREADY_EXISTS)})
		}
		names[nameKey], paths[pathKey] = true, true
	}
	for _, v := range shares {
		shareDBModel := model2.SharesDBModel{}
		shareDBModel.Anonymous = v.Anonymous
		shareDBModel.Path = v.Path
		shareDBModel.Name = v.Name
		shareDBModel.ReadOnly = v.ReadOnly
		if err := service.MyService.Shares().CreateShare(shareDBModel); err != nil {
			return ctx.JSON(500, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		}
	}

	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: shares})
}

func DeleteSambaShares(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INSUFFICIENT_PERMISSIONS, Message: common_err.GetMsg(common_err.INSUFFICIENT_PERMISSIONS)})
	}
	if err := service.MyService.Shares().DeleteShare(id); err != nil {
		return ctx.JSON(500, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: id})
}

// client
func GetSambaConnectionsList(ctx echo.Context) error {
	connections := service.MyService.Connections().GetConnectionsList()
	connectionList := []model.Connections{}
	for _, v := range connections {
		connectionList = append(connectionList, model.Connections{
			ID:         v.ID,
			Username:   v.Username,
			Port:       v.Port,
			Host:       v.Host,
			MountPoint: v.MountPoint,
		})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: connectionList})
}

func PostSambaConnectionsCreate(ctx echo.Context) error {
	connection := model.Connections{}
	ctx.Bind(&connection)
	if connection.Port == "" {
		connection.Port = "445"
	}
	if connection.Username == "" || connection.Host == "" {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CHARACTER_LIMIT, Message: common_err.GetMsg(common_err.CHARACTER_LIMIT)})
	}

	// if ok, _ := regexp.MatchString(`^[\w@#*.]{4,30}$`, connection.Password); !ok {
	// 	return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CHARACTER_LIMIT, Message: common_err.GetMsg(common_err.CHARACTER_LIMIT)})
	// 	return
	// }
	// if ok, _ := regexp.MatchString(`^[\w@#*.]{4,30}$`, connection.Username); !ok {
	// 	return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	// 	return
	// }
	// if !ip_helper.IsIPv4(connection.Host) && !ip_helper.IsIPv6(connection.Host) {
	// 	return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	// 	return
	// }
	// if ok, _ := regexp.MatchString("^[0-9]{1,6}$", connection.Port); !ok {
	// 	return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	// 	return
	// }

	connection.Host = strings.Split(connection.Host, "/")[0]
	// check is exists
	connections := service.MyService.Connections().GetConnectionByHost(connection.Host)
	if len(connections) > 0 {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.Record_ALREADY_EXIST, Message: common_err.GetMsg(common_err.Record_ALREADY_EXIST), Data: common_err.GetMsg(common_err.Record_ALREADY_EXIST)})
	}
	// check connect is ok
	directories, err := samba.GetSambaSharesList(connection.Host, connection.Port, connection.Username, connection.Password)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}

	connectionDBModel := model2.ConnectionsDBModel{}
	connectionDBModel.Username = connection.Username
	connectionDBModel.Password = connection.Password
	connectionDBModel.Host = connection.Host
	connectionDBModel.Port = connection.Port
	connectionDBModel.Directories = strings.Join(directories, ",")
	baseHostPath := service.SMBHostPath(connection.Host)
	connectionDBModel.MountPoint = baseHostPath
	connection.MountPoint = baseHostPath
	if runtime.GOOS != "windows" {
		file.IsNotExistMkDir(baseHostPath)
	}
	mountedPaths := []string{}
	for _, v := range directories {
		mountPoint := service.SMBSharePath(connection.Host, v)
		if runtime.GOOS != "windows" {
			file.IsNotExistMkDir(mountPoint)
		}
		if err := service.MyService.Connections().MountSmaba(connectionDBModel.Username, connectionDBModel.Host, v, connectionDBModel.Port, mountPoint, connectionDBModel.Password); err != nil {
			for _, p := range mountedPaths {
				_ = service.MyService.Connections().UnmountSmaba(p)
			}
			return ctx.JSON(500, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		}
		mountedPaths = append(mountedPaths, mountPoint)
	}

	service.MyService.Connections().CreateConnection(&connectionDBModel)

	connection.ID = connectionDBModel.ID
	connection.Password = ""
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: connection})
}

func DeleteSambaConnections(ctx echo.Context) error {
	id := ctx.Param("id")
	connection := service.MyService.Connections().GetConnectionByID(id)
	if connection.Username == "" {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.Record_NOT_EXIST, Message: common_err.GetMsg(common_err.Record_NOT_EXIST)})
	}
	if runtime.GOOS == "windows" {
		for _, share := range strings.Split(connection.Directories, ",") {
			if share == "" {
				continue
			}
			if err := service.MyService.Connections().UnmountSmaba(service.SMBSharePath(connection.Host, share)); err != nil {
				return ctx.JSON(500, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
			}
		}
		service.MyService.Connections().DeleteConnection(id)
		return ctx.JSON(200, model.Result{Success: common_err.SUCCESS, Data: id})
	}
	mountPointList := strings.Split(connection.Directories, ",")
	baseHostPath := "/mnt/" + connection.Host
	for _, v := range mountPointList {
		if v == "" {
			continue
		}
		if service.IsMounted(baseHostPath + "/" + v) {
			err := service.MyService.Connections().UnmountSmaba(baseHostPath + "/" + v)
			if err != nil {
				logger.Error("unmount smaba error", zap.Error(err), zap.Any("path", baseHostPath+"/"+v))
				return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
			}
		}
	}
	dir, _ := ioutil.ReadDir(connection.MountPoint)
	if len(dir) == 0 {
		os.RemoveAll(connection.MountPoint)
	}
	service.MyService.Connections().DeleteConnection(id)
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: id})
}
