package service

import (
	"github.com/shirou/gopsutil/v3/net"
	"sort"
	"syscall"
)

type HealthService interface {
	Services() (map[bool]*[]string, error)
	Ports() ([]int, []int, error)
}

type service struct{}

func (s *service) Ports() ([]int, []int, error) {
	connections, err := net.Connections("all")
	if err != nil {
		return nil, nil, err
	}
	tcp, udp := map[int]bool{}, map[int]bool{}
	for _, connection := range connections {
		port := int(connection.Laddr.Port)
		if port == 0 {
			continue
		}
		if connection.Type == syscall.SOCK_STREAM {
			tcp[port] = true
		}
		if connection.Type == syscall.SOCK_DGRAM {
			udp[port] = true
		}
	}
	tcpPorts, udpPorts := make([]int, 0, len(tcp)), make([]int, 0, len(udp))
	for port := range tcp {
		tcpPorts = append(tcpPorts, port)
	}
	for port := range udp {
		udpPorts = append(udpPorts, port)
	}
	sort.Ints(tcpPorts)
	sort.Ints(udpPorts)
	return tcpPorts, udpPorts, nil
}

func NewHealthService() HealthService {
	return &service{}
}
