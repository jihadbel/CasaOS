package service

import (
	"strings"

	"github.com/IceWhaleTech/CasaOS-Common/utils/systemctl"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
)

func GetSambaStatus() (running, needInit bool, err error) {
	running, err = systemctl.IsServiceRunning("smbd")
	needInit = true
	if file.Exists("/etc/samba/smb.conf") {
		needInit = !strings.Contains(file.ReadLine(1, "/etc/samba/smb.conf"), "# Copyright (c) 2021-2022 CasaOS Inc. All rights reserved.")
	}
	return
}
