//go:build !windows

/*
 * @Author: LinkLeong link@icewhale.org
 * @Date: 2022-07-26 11:21:14
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-08-18 11:16:25
 * @FilePath: /CasaOS/service/shares_linux.go
 * @Description: Linux-specific Samba configuration management
 * @Website: https://www.casaos.io
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package service

import (
	"strings"

	"github.com/IceWhaleTech/CasaOS-Common/utils/command"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
	model2 "github.com/IceWhaleTech/CasaOS/service/model"
)

func (s *sharesStruct) UpdateConfigFile() error {
	shares := []model2.SharesDBModel{}
	if err := s.db.Select("anonymous,read_only,name,path").Find(&shares).Error; err != nil {
		return err
	}
	// generated config file
	configStr := ""
	for _, share := range shares {
		if err := ValidateSharePath(share.Path); err != nil {
			return err
		}
		dirName, err := ShareName(share.Path, share.Name)
		if err != nil {
			return err
		}
		readOnly := "No"
		if share.ReadOnly {
			readOnly = "Yes"
		}
		guest := "No"
		if share.Anonymous {
			guest = "Yes"
		}
		configStr += `
[` + dirName + `]
comment = CasaOS share ` + dirName + `
public = ` + guest + `
path = ` + share.Path + `
browseable = Yes
read only = ` + readOnly + `
guest ok = ` + guest + `
create mask = 0660
directory mask = 0770

`
	}
	// write config file
	if err := file.WriteToPath([]byte(configStr), "/etc/samba", "smb.casa.conf"); err != nil {
		return err
	}
	// restart samba
	_, err := command.OnlyExec("source " + config.AppInfo.ShellPath + "/helper.sh ;RestartSMBD")
	return err
}

func (s *sharesStruct) InitSambaConfig() {
	if file.Exists("/etc/samba/smb.conf") {
		str := file.ReadLine(1, "/etc/samba/smb.conf")
		if strings.Contains(str, "# Copyright (c) 2021-2022 CasaOS Inc. All rights reserved.") {
			return
		}
		file.MoveFile("/etc/samba/smb.conf", "/etc/samba/smb.conf.bak")
		smbConf := ""
		smbConf += `# Copyright (c) 2021-2022 CasaOS Inc. All rights reserved.
#
#
#                          ______     _______
#                        (  __  \   (  ___  )
#                        | (  \  )  | (   ) |
#                        | |   ) |  | |   | |
#                        | |   | |  | |   | |
#                        | |   ) |  | |   | |
#                        | (__/  )  | (___) |
#                        (______/   (_______)
#
#                   _          _______   _________
#                  ( (    /|  (  ___  )  \__   __/
#                  |  \  ( |  | (   ) |     ) (
#                  |   \ | |  | |   | |     | |
#                  | (\ \) |  | |   | |     | |
#                  | | \   |  | |   | |     | |
#                  | )  \  |  | (___) |     | |
#                  |/    )_)  (_______)     )_(
#
#   _______    _______    ______    _________   _______
#  (       )  (  ___  )  (  __  \   \__   __/  (  ____ \  |\     /|
#  | () () |  | (   ) |  | (  \  )     ) (     | (    \/  ( \   / )
#  | || || |  | |   | |  | |   ) |     | |     | (__       \ (_) /
#  | |(_)| |  | |   | |  | |   | |     | |     |  __)       \   /
#  | |   | |  | |   | |  | |   ) |     | |     | (           ) (
#  | )   ( |  | (___) |  | (__/  )  ___) (___  | )           | |
#  |/     \|  (_______)  (______/   \_______/  |/            \_/
#
#
# IMPORTANT: CasaOS will not provide technical support for any issues
#            caused by unauthorized modification to the configuration.

[global]
## fruit settings
   min protocol = SMB2
   ea support = yes
## vfs objects = fruit streams_xattr
   fruit:metadata = stream
   fruit:model = Macmini
   fruit:veto_appledouble = no
   fruit:posix_rename = yes
   fruit:zero_file_id = yes
   fruit:wipe_intentionally_left_blank_rfork = yes
   fruit:delete_empty_adfiles = yes
   map to guest = bad user
   include=/etc/samba/smb.casa.conf`
		file.WriteToPath([]byte(smbConf), "/etc/samba", "smb.conf")
	}
}
