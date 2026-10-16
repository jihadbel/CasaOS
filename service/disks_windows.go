package service

import (
	"fmt"
	"math"
	"time"

	"github.com/IceWhaleTech/CasaOS/model"
	"golang.org/x/sys/windows"
)

func localDriveRoots(mask uint32, driveType func(string) uint32) []string {
	var roots []string
	for letter := 'A'; letter <= 'Z'; letter++ {
		if mask&(1<<uint(letter-'A')) == 0 {
			continue
		}
		root := fmt.Sprintf("%c:\\", letter)
		switch driveType(root) {
		case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE, windows.DRIVE_RAMDISK:
			roots = append(roots, root)
		}
	}
	return roots
}

func windowsDriveRoots() ([]string, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, err
	}
	return localDriveRoots(mask, func(root string) uint32 {
		name, _ := windows.UTF16PtrFromString(root)
		return windows.GetDriveType(name)
	}), nil
}

func readDisksInfo() ([]map[string]interface{}, error) {
	roots, err := windowsDriveRoots()
	if err != nil {
		return nil, err
	}
	var items []map[string]interface{}
	for _, root := range roots {
		name, _ := windows.UTF16PtrFromString(root)
		var available, total, free uint64
		if err := windows.GetDiskFreeSpaceEx(name, &available, &total, &free); err != nil || total == 0 {
			continue
		}
		used := total - free
		items = append(items, map[string]interface{}{
			"name": root[:2] + " Drive", "path": root, "drive": root[:1],
			"total": total, "used": used, "free": free,
			"usedPercent": math.Round(float64(used)/float64(total)*1000) / 10,
		})
	}
	return items, nil
}

func getWindowsDrives() []model.Path {
	roots, _ := windowsDriveRoots()
	var drives []model.Path
	for _, root := range roots {
		drives = append(drives, model.Path{Name: root[:2] + " Drive", Path: root, IsDir: true, Date: time.Now()})
	}
	return drives
}
