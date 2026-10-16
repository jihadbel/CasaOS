# Shared by the installer and its tests; importing this file makes no changes.
function Assert-CasaOSInstallPath {
    param([Parameter(Mandatory = $true)][string]$Path)
    $current = [IO.Path]::GetFullPath($Path)
    while ($current) {
        if (Test-Path -LiteralPath $current) {
            $item = Get-Item -LiteralPath $current -Force
            if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
                throw "Installation paths must not contain links or junctions: $current"
            }
        }
        $parent = [IO.Path]::GetDirectoryName($current)
        if ($parent -eq $current) { break }
        $current = $parent
    }
}

function Assert-CasaOSInstallAcl {
    param([Parameter(Mandatory = $true)][string]$Path, [switch]$AllowInherited)
    $acl = Get-Acl -LiteralPath $Path
    $trusted = @('S-1-5-18', 'S-1-5-32-544') # SYSTEM, Administrators
    if ($acl.GetOwner([Security.Principal.SecurityIdentifier]).Value -notin $trusted -or
        (-not $AllowInherited -and -not $acl.AreAccessRulesProtected)) {
        throw "Installation owner or ACL is not protected: $Path"
    }
    $writeMask = [Security.AccessControl.FileSystemRights]::Write -bor
        [Security.AccessControl.FileSystemRights]::Delete -bor
        [Security.AccessControl.FileSystemRights]::DeleteSubdirectoriesAndFiles -bor
        [Security.AccessControl.FileSystemRights]::ChangePermissions -bor
        [Security.AccessControl.FileSystemRights]::TakeOwnership
    foreach ($rule in $acl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier])) {
        if ($rule.AccessControlType -eq [Security.AccessControl.AccessControlType]::Allow -and
            $rule.IdentityReference.Value -notin $trusted -and ($rule.FileSystemRights -band $writeMask)) {
            throw "An untrusted account can modify the installation: $Path"
        }
    }
}

function Set-CasaOSInstallAcl {
    param([Parameter(Mandatory = $true)][string]$Path, [switch]$Directory)
    if ($Directory) {
        $acl = [Security.AccessControl.DirectorySecurity]::new()
        $inherit = [Security.AccessControl.InheritanceFlags]'ContainerInherit,ObjectInherit'
    } else {
        $acl = [Security.AccessControl.FileSecurity]::new()
        $inherit = [Security.AccessControl.InheritanceFlags]::None
    }
    $acl.SetAccessRuleProtection($true, $false)
    $acl.SetOwner([Security.Principal.SecurityIdentifier]::new('S-1-5-32-544'))
    foreach ($sid in @('S-1-5-18', 'S-1-5-32-544', 'S-1-5-32-545')) {
        $rights = [Security.AccessControl.FileSystemRights]::ReadAndExecute
        if ($sid -ne 'S-1-5-32-545') { $rights = [Security.AccessControl.FileSystemRights]::FullControl }
        $rule = [Security.AccessControl.FileSystemAccessRule]::new(
            [Security.Principal.SecurityIdentifier]::new($sid), $rights, $inherit,
            [Security.AccessControl.PropagationFlags]::None, [Security.AccessControl.AccessControlType]::Allow)
        $acl.AddAccessRule($rule)
    }
    Set-Acl -LiteralPath $Path -AclObject $acl
    Assert-CasaOSInstallAcl -Path $Path
}
