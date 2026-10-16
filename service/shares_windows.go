package service

import (
	"encoding/json"
	"path/filepath"

	"github.com/IceWhaleTech/CasaOS-Common/utils/logger"
	model2 "github.com/IceWhaleTech/CasaOS/service/model"
	"go.uber.org/zap"
)

func (s *sharesStruct) UpdateConfigFile() error {
	shares := []model2.SharesDBModel{}
	if err := s.db.Select("name,path").Find(&shares).Error; err != nil {
		return err
	}
	type entry struct {
		Name string
		Path string
	}
	desired := []entry{}
	for _, share := range shares {
		desired = append(desired, entry{filepath.Base(share.Path), share.Path})
	}
	data, err := json.Marshal(desired)
	if err != nil {
		return err
	}
	return runSMBScript(`
		$marker = 'CasaOS managed share'
		$desired = ConvertFrom-Json -InputObject $env:CASAOS_SMB_SHARES
		$existing = @(Get-SmbShare)
		$names = @{}
		foreach ($share in $desired) {
			if ($names.ContainsKey($share.Name)) { throw 'Duplicate CasaOS share name' }
			$names[$share.Name] = $share.Path
			$current = $existing | Where-Object Name -eq $share.Name
			if ($current -and ($current.Path -ne $share.Path -or ($current.Description -ne $marker -and $current.Description -ne ('CasaOS share ' + $share.Name)))) {
				throw ('Share name is owned by another application: ' + $share.Name)
			}
		}
		foreach ($share in $existing) {
			$owned = $share.Description -eq $marker -or $share.Description -eq ('CasaOS share ' + $share.Name)
			if ($owned -and -not $names.ContainsKey($share.Name)) {
				Remove-SmbShare -Name $share.Name -Force -Confirm:$false
			}
		}
		$everyone = ([System.Security.Principal.SecurityIdentifier]'S-1-1-0').Translate([System.Security.Principal.NTAccount]).Value
		foreach ($share in $desired) {
			if (-not ($existing | Where-Object Name -eq $share.Name)) {
				New-SmbShare -Name $share.Name -Path $share.Path -FullAccess $everyone -Description $marker | Out-Null
			}
		}
	`, map[string]string{"SHARES": string(data)})
}

func (s *sharesStruct) InitSambaConfig() {
	if err := runSMBScript(`if ((Get-Service -Name LanmanServer).Status -ne 'Running') { Start-Service -Name LanmanServer }`, nil); err != nil {
		logger.Error("Windows SMB service failed", zap.Error(err))
	}
}
