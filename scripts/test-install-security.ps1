# Test ACL construction/validation with mocks. No ACL or scheduled task is changed.
$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot 'install-security.ps1')

$script:lastAcl = $null
function Set-Acl { param([string]$LiteralPath, $AclObject) $script:lastAcl = $AclObject }
function Get-Acl { param([string]$LiteralPath) return $script:lastAcl }
if ((Get-Command Set-Acl).CommandType -ne 'Function' -or (Get-Command Get-Acl).CommandType -ne 'Function') {
    throw 'ACL mocks missing; refusing to run.'
}
foreach ($directory in @($true, $false)) {
    Set-CasaOSInstallAcl -Path 'unused-test-path' -Directory:$directory
    if ($script:lastAcl.GetOwner([Security.Principal.SecurityIdentifier]).Value -ne 'S-1-5-32-544') { throw 'Installation owner is writable by the installing user.' }
    $users = @($script:lastAcl.GetAccessRules($true,$true,[Security.Principal.SecurityIdentifier]) | Where-Object { $_.IdentityReference.Value -eq 'S-1-5-32-545' })
    if ($users.Count -ne 1 -or -not ($users[0].FileSystemRights -band [Security.AccessControl.FileSystemRights]::ExecuteFile)) { throw 'Users cannot run the installed executable.' }
}
$hostileRule = [Security.AccessControl.FileSystemAccessRule]::new(
    [Security.Principal.SecurityIdentifier]::new('S-1-1-0'), [Security.AccessControl.FileSystemRights]::Write,
    [Security.AccessControl.AccessControlType]::Allow)
$script:lastAcl.AddAccessRule($hostileRule)
$rejected = $false
try { Assert-CasaOSInstallAcl -Path 'unused-test-path' } catch { $rejected = $true }
if (-not $rejected) { throw 'Writable installation was accepted.' }
Set-CasaOSInstallAcl -Path 'unused-test-path' -Directory
$script:lastAcl.SetOwner([Security.Principal.SecurityIdentifier]::new('S-1-5-32-545'))
$rejected = $false
try { Assert-CasaOSInstallAcl -Path 'unused-test-path' -AllowInherited } catch { $rejected = $true }
if (-not $rejected) { throw 'User-owned existing directory was accepted.' }
Write-Output 'PASS: protected Administrators-owned ACL; SYSTEM/Administrators write; users read/execute only (mocked).'

foreach ($file in @('install-service.ps1', 'scripts\install-security.ps1')) {
    $tokens = $null; $errors = $null
    [System.Management.Automation.Language.Parser]::ParseFile((Join-Path $repositoryRoot $file),[ref]$tokens,[ref]$errors) | Out-Null
    if ($errors.Count) { throw ($errors | Out-String) }
}
$source = Get-Content -LiteralPath (Join-Path $repositoryRoot 'install-service.ps1') -Raw
if ($source -notmatch 'SpecialFolder\]::ProgramFiles' -or $source -notmatch '\-WorkingDirectory \$installDirectory' -or $source -notmatch '\-Execute \$serverExecutable') { throw 'Task does not use the protected installation.' }
if ($source -notmatch '& \$serverExecutable -prepare-storage' -or
    $source.IndexOf('& $serverExecutable -prepare-storage') -gt $source.IndexOf('Register-ScheduledTask')) {
    throw 'Protected storage must be validated before registering the SYSTEM task.'
}
Write-Output 'PASS: installer syntax and protected task target; no task registered.'
