//go:build !windows

package service

import "github.com/IceWhaleTech/CasaOS-Common/utils/systemctl"

func (s *service) Services() (map[bool]*[]string, error) {
	services, err := systemctl.ListServices("casaos*")
	if err != nil {
		return nil, err
	}
	running, stopped := []string{}, []string{}
	for _, entry := range services {
		if entry.Running {
			running = append(running, entry.Name)
		} else {
			stopped = append(stopped, entry.Name)
		}
	}
	return map[bool]*[]string{true: &running, false: &stopped}, nil
}
