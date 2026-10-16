package service

import (
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// GetSambaStatus queries Windows directly; no Linux tools or config are used.
func GetSambaStatus() (running, needInit bool, err error) {
	manager, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false, false, err
	}
	defer windows.CloseServiceHandle(manager)
	name, _ := windows.UTF16PtrFromString("LanmanServer")
	handle, err := windows.OpenService(manager, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false, false, err
	}
	server := &mgr.Service{Name: "LanmanServer", Handle: handle}
	defer server.Close()
	status, err := server.Query()
	return status.State == svc.Running, false, err
}
