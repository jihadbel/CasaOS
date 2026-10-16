package service

import (
	"path/filepath"
	"runtime"
	"strings"
)

func SMBHostPath(host string) string {
	if runtime.GOOS == "windows" {
		return `\\` + host
	}
	return "/mnt/" + host
}

func SMBSharePath(host, share string) string {
	if runtime.GOOS == "windows" {
		return SMBHostPath(host) + `\` + strings.Trim(share, `\/`)
	}
	return filepath.Join(SMBHostPath(host), share)
}
