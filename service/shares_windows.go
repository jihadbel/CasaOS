package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/IceWhaleTech/CasaOS-Common/utils/logger"
	model2 "github.com/IceWhaleTech/CasaOS/service/model"
	"go.uber.org/zap"
)

func (s *sharesStruct) UpdateConfigFile() error {
	shares := []model2.SharesDBModel{}
	if err := s.db.Select("name,path,read_only").Find(&shares).Error; err != nil {
		return err
	}
	type entry struct {
		Name     string
		Path     string
		ReadOnly bool
	}
	desired := []entry{}
	names := map[string]bool{}
	for _, share := range shares {
		if err := ValidateSharePath(share.Path); err != nil {
			return err
		}
		name, err := ShareName(share.Path, share.Name)
		if err != nil {
			return err
		}
		key := strings.ToLower(name)
		if names[key] {
			return fmt.Errorf("duplicate CasaOS share name: %s", name)
		}
		names[key] = true
		desired = append(desired, entry{name, share.Path, share.ReadOnly})
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
			if ($current -and ($current.Description -ne $marker -and $current.Description -ne ('CasaOS share ' + $share.Name))) {
				throw ('Share name is owned by another application: ' + $share.Name)
			}
		}
		foreach ($share in $existing) {
			$owned = $share.Description -eq $marker -or $share.Description -eq ('CasaOS share ' + $share.Name)
			if ($owned -and -not $names.ContainsKey($share.Name)) {
				Remove-SmbShare -Name $share.Name -Force -Confirm:$false
			}
		}
		$users = ([System.Security.Principal.SecurityIdentifier]'S-1-5-11').Translate([System.Security.Principal.NTAccount]).Value
        $everyone = ([System.Security.Principal.SecurityIdentifier]'S-1-1-0').Translate([System.Security.Principal.NTAccount]).Value
		foreach ($share in $desired) {
			$current = $existing | Where-Object Name -eq $share.Name
            if ($current -and $current.Path -ne $share.Path) {
                Remove-SmbShare -Name $share.Name -Force -Confirm:$false
                $current = $null
            }
            if ($current) {
                Revoke-SmbShareAccess -Name $share.Name -AccountName $everyone -Force -Confirm:$false -ErrorAction SilentlyContinue | Out-Null
                Revoke-SmbShareAccess -Name $share.Name -AccountName $users -Force -Confirm:$false -ErrorAction SilentlyContinue | Out-Null
                $right = 'Change'
                if ($share.ReadOnly) { $right = 'Read' }
                Grant-SmbShareAccess -Name $share.Name -AccountName $users -AccessRight $right -Force -Confirm:$false | Out-Null
            } else {
                $access = @{ ChangeAccess = $users }
                if ($share.ReadOnly) { $access = @{ ReadAccess = $users } }
                New-SmbShare -Name $share.Name -Path $share.Path @access -Description $marker | Out-Null
            }
		}
	`, map[string]string{"SHARES": string(data)})
}

func (s *sharesStruct) InitSambaConfig() {
	if err := runSMBScript(`if ((Get-Service -Name LanmanServer).Status -ne 'Running') { Start-Service -Name LanmanServer }`, nil); err != nil {
		logger.Error("Windows SMB service failed", zap.Error(err))
	}
}
