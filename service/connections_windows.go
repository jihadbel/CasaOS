//go:build windows

/*
 * @Description: Windows-native SMB connection management
 * @Website: https://www.casaos.io
 */
package service

import (
	"fmt"
	"os/exec"
	"strings"
)

func (s *connectionsStruct) MountSmaba(username, host, directory, port, mountPoint, password string) error {
	remotePath := fmt.Sprintf(`\\%s\%s`, host, directory)
	var cmd *exec.Cmd

	if strings.Contains(mountPoint, ":") {
		// Drive letter mapping (e.g. Z:)
		cmd = exec.Command("net", "use", mountPoint, remotePath, password, fmt.Sprintf("/user:%s", username), "/persistent:no")
	} else {
		// Windows SMB mapping
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
			fmt.Sprintf(`New-SmbMapping -RemotePath '%s' -UserName '%s' -Password '%s' -LocalPath '%s'`, remotePath, username, password, mountPoint))
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("windows mount smb failed: %w, output: %s", err, string(output))
	}
	return nil
}

func (s *connectionsStruct) UnmountSmaba(mountPoint string) error {
	var cmd *exec.Cmd

	if strings.Contains(mountPoint, ":") {
		cmd = exec.Command("net", "use", mountPoint, "/delete", "/y")
	} else {
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
			fmt.Sprintf(`Remove-SmbMapping -LocalPath '%s' -Force`, mountPoint))
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("windows unmount smb failed: %w, output: %s", err, string(output))
	}
	return nil
}
