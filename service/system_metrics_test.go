package service

import (
	"errors"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

func TestSystemMetricsSurviveEmptyAndFailedProbes(t *testing.T) {
	for _, probeError := range []error{nil, errors.New("probe unavailable")} {
		s := &systemService{
			sampleCPU:    func(time.Duration, bool) ([]float64, error) { return nil, probeError },
			sampleMemory: func() (*mem.VirtualMemoryStat, error) { return nil, probeError },
			sampleHost:   func() (*host.InfoStat, error) { return nil, probeError },
		}
		if s.GetCpuPercent() != 0 || s.GetSysInfo().Hostname != "" {
			t.Fatal("invalid fallback")
		}
		data := s.GetMemInfo()
		if len(data) != 5 || data["total"] != uint64(0) || data["usedPercent"] != float64(0) {
			t.Fatal(data)
		}
	}
}

func TestSystemMetricsPreserveHealthyReadings(t *testing.T) {
	s := &systemService{
		sampleCPU:  func(time.Duration, bool) ([]float64, error) { return []float64{12.34}, nil },
		sampleHost: func() (*host.InfoStat, error) { return &host.InfoStat{Hostname: "test-host"}, nil },
		sampleMemory: func() (*mem.VirtualMemoryStat, error) {
			return &mem.VirtualMemoryStat{Total: 100, Used: 25, Available: 70, Free: 60, UsedPercent: 25.34}, nil
		},
	}
	if s.GetCpuPercent() != 12.3 || s.GetSysInfo().Hostname != "test-host" || s.GetMemInfo()["usedPercent"] != 25.3 {
		t.Fatal("healthy metric changed")
	}
}
