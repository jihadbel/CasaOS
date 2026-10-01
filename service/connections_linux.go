//go:build !windows

/*
 * @Author: LinkLeong link@icewhale.org
 * @Date: 2022-07-26 18:13:22
 * @LastEditors: LinkLeong
 * @LastEditTime: 2022-08-04 20:10:31
 * @FilePath: /CasaOS/service/connections_linux.go
 * @Description: Linux-specific CIFS mounting using Unix syscalls
 * @Website: https://www.casaos.io
 * Copyright (c) 2022 by icewhale, All Rights Reserved.
 */
package service

import (
	"fmt"

	"github.com/moby/sys/mount"
	"golang.org/x/sys/unix"
)

func (s *connectionsStruct) MountSmaba(username, host, directory, port, mountPoint, password string) error {
	err := unix.Mount(
		fmt.Sprintf("//%s/%s", host, directory),
		mountPoint,
		"cifs",
		unix.MS_NOATIME|unix.MS_NODEV|unix.MS_NOSUID,
		fmt.Sprintf("username=%s,password=%s", username, password),
	)
	return err
}

func (s *connectionsStruct) UnmountSmaba(mountPoint string) error {
	return mount.Unmount(mountPoint)
}
