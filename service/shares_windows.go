//go:build windows

/*
 * @Description: Windows-native SMB file sharing management
 * @Website: https://www.casaos.io
 */
package service

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/IceWhaleTech/CasaOS-Common/utils/logger"
	model2 "github.com/IceWhaleTech/CasaOS/service/model"
	"go.uber.org/zap"
)

func (s *sharesStruct) UpdateConfigFile() {
	shares := []model2.SharesDBModel{}
	s.db.Select("anonymous,path").Find(&shares)

	for _, share := range shares {
		dirName := filepath.Base(share.Path)
		// On Windows, ensure the native SMB share exists
		psScript := fmt.Sprintf(`
			if (-not (Get-SmbShare -Name '%s' -ErrorAction SilentlyContinue)) {
				New-SmbShare -Name '%s' -Path '%s' -FullAccess 'Everyone' -Description 'CasaOS share %s'
			}
		`, dirName, dirName, share.Path, dirName)

		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
		if output, err := cmd.CombinedOutput(); err != nil {
			logger.Error("Failed to configure Windows SMB share",
				zap.String("name", dirName),
				zap.String("path", share.Path),
				zap.Error(err),
				zap.String("output", string(output)),
			)
		}
	}
}

func (s *sharesStruct) InitSambaConfig() {
	// On Windows, check that the Server service (LanmanServer) is running
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		`if ((Get-Service -Name LanmanServer).Status -ne 'Running') { Start-Service -Name LanmanServer }`)
	if output, err := cmd.CombinedOutput(); err != nil {
		logger.Info("Windows LanmanServer service check returned notice", zap.Error(err), zap.String("output", string(output)))
	}
}
