//go:build !windows

package service

import (
	"math"

	"github.com/IceWhaleTech/CasaOS/model"
	"github.com/shirou/gopsutil/v3/disk"
)

func readDisksInfo() ([]map[string]interface{}, error) {
	usage, err := disk.Usage("/")
	if err != nil {
		return nil, err
	}
	if usage == nil {
		return nil, nil
	}
	return []map[string]interface{}{{
		"name": "Root Storage", "path": "/", "drive": "/", "total": usage.Total,
		"used": usage.Used, "free": usage.Free, "usedPercent": math.Round(usage.UsedPercent*10) / 10,
	}}, nil
}

func getWindowsDrives() []model.Path { return nil }
