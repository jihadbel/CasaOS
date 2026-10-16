package service_test

import (
	"net"
	"testing"

	"github.com/IceWhaleTech/CasaOS/service"
	"github.com/stretchr/testify/assert"
)

func TestPorts(t *testing.T) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	service := service.NewHealthService()

	tcpPorts, udpPorts, err := service.Ports()
	assert.NoError(t, err)

	assert.Contains(t, tcpPorts, tcp.Addr().(*net.TCPAddr).Port)
	assert.Contains(t, udpPorts, udp.LocalAddr().(*net.UDPAddr).Port)
}
