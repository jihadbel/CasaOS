package service

import (
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"strings"

	"golang.org/x/sys/windows"
)

func (s *service) Services() (map[bool]*[]string, error) {
	handle, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT|windows.SC_MANAGER_ENUMERATE_SERVICE)
	if err != nil {
		return nil, err
	}
	manager := &mgr.Mgr{Handle: handle}
	defer manager.Disconnect()
	names, err := manager.ListServices()
	if err != nil {
		return nil, err
	}
	running, stopped := []string{}, []string{}
	for _, name := range names {
		if !strings.HasPrefix(strings.ToLower(name), "casaos") {
			continue
		}
		serviceName, err := windows.UTF16PtrFromString(name)
		if err != nil {
			return nil, err
		}
		serviceHandle, err := windows.OpenService(handle, serviceName, windows.SERVICE_QUERY_STATUS)
		if err != nil {
			return nil, err
		}
		entry := &mgr.Service{Name: name, Handle: serviceHandle}
		status, err := entry.Query()
		_ = entry.Close()
		if err != nil {
			return nil, err
		}
		if status.State == svc.Running {
			running = append(running, name)
		} else {
			stopped = append(stopped, name)
		}
	}
	return map[bool]*[]string{true: &running, false: &stopped}, nil
}
