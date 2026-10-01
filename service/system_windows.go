//go:build windows

/*
 * @Description: Windows-native system service implementation
 * @Website: https://www.casaos.io
 */
package service

import (
	"fmt"
	net2 "net"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/host"
)

var (
	cachedNetStates     = make(map[string]string)
	lastNetStateRefresh time.Time
	netStateMutex       sync.RWMutex
)

func (c *systemService) GetDeviceTree() string {
	h, err := host.Info()
	if err != nil {
		return "Windows System"
	}
	return fmt.Sprintf("%s (%s %s)", h.Platform, h.PlatformVersion, h.KernelArch)
}

func (c *systemService) GetNetState(name string) string {
	netStateMutex.RLock()
	if time.Since(lastNetStateRefresh) < 3*time.Second {
		if state, ok := cachedNetStates[name]; ok {
			netStateMutex.RUnlock()
			return state
		}
	}
	netStateMutex.RUnlock()

	netStateMutex.Lock()
	defer netStateMutex.Unlock()
	if time.Since(lastNetStateRefresh) >= 3*time.Second {
		interfaces, err := net2.Interfaces()
		if err == nil {
			cachedNetStates = make(map[string]string)
			for _, iface := range interfaces {
				if iface.Flags&net2.FlagUp != 0 {
					cachedNetStates[iface.Name] = "up"
				} else {
					cachedNetStates[iface.Name] = "down"
				}
			}
			lastNetStateRefresh = time.Now()
		}
	}
	if state, ok := cachedNetStates[name]; ok {
		return state
	}
	return "up"
}

func (c *systemService) GetNet(physics bool) []string {
	interfaces, err := net2.Interfaces()
	if err != nil {
		return []string{}
	}
	var names []string
	for _, iface := range interfaces {
		if iface.Flags&net2.FlagLoopback != 0 {
			continue
		}
		if physics {
			if len(iface.HardwareAddr) == 0 {
				continue
			}
		}
		names = append(names, iface.Name)
	}
	return names
}

func (s *systemService) UpdateSystemVersion(version string) {
	keyName := "casa_version"
	Cache.Delete(keyName)
}

func (s *systemService) UpdateAssist() {
	// No-op on Windows
}

func (s *systemService) GetTimeZone() string {
	zone, _ := time.Now().Zone()
	return zone
}

func (s *systemService) GetSystemConfigDebug() []string {
	h, _ := host.Info()
	return []string{
		fmt.Sprintf("OS: %s %s", h.Platform, h.PlatformVersion),
		fmt.Sprintf("Arch: %s", runtime.GOARCH),
		fmt.Sprintf("Hostname: %s", h.Hostname),
	}
}

func GetCPUThermalZone() string {
	return "windows_thermal_zone"
}

func (s *systemService) GetCPUTemperature() int {
	return 0
}

func (s *systemService) GetCPUPower() map[string]string {
	data := make(map[string]string, 2)
	data["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
	data["value"] = "0"
	return data
}

func (s *systemService) SystemReboot() error {
	cmd := exec.Command("shutdown", "/r", "/t", "0")
	return cmd.Run()
}

func (s *systemService) SystemShutdown() error {
	cmd := exec.Command("shutdown", "/s", "/t", "0")
	return cmd.Run()
}
