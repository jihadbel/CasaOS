package service

import (
	"fmt"
	"strings"
)

func (s *connectionsStruct) MountSmaba(username, host, directory, port, mountPoint, password string) error {
	if port != "" && port != "445" {
		return fmt.Errorf("native Windows SMB requires port 445")
	}
	remote := SMBSharePath(host, directory)
	if !strings.HasPrefix(remote, `\\`) {
		return fmt.Errorf("invalid SMB remote path")
	}
	return runSMBScript(`
		New-SmbMapping -RemotePath $env:CASAOS_SMB_REMOTE -UserName $env:CASAOS_SMB_USER -Password $env:CASAOS_SMB_PASSWORD -Persistent $false | Out-Null
	`, map[string]string{"REMOTE": remote, "USER": username, "PASSWORD": password})
}

func (s *connectionsStruct) UnmountSmaba(mountPoint string) error {
	return runSMBScript(`
		$mapping = Get-SmbMapping -RemotePath $env:CASAOS_SMB_REMOTE -ErrorAction SilentlyContinue
		if ($mapping) { $mapping | Remove-SmbMapping -Force -Confirm:$false }
	`, map[string]string{"REMOTE": mountPoint})
}
