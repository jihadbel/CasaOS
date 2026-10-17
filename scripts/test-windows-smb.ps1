# Exercise the exact embedded scripts with mocks. No native SMB operation runs.
$ErrorActionPreference = 'Stop'
$repositoryRoot = Split-Path -Parent $PSScriptRoot
$tick = [char]96
$scriptPattern = '(?s)runSMBScript\(\s*' + $tick + '(.*?)' + $tick
$sharesSource = Get-Content -LiteralPath (Join-Path $repositoryRoot 'service\shares_windows.go') -Raw
$connectionSource = Get-Content -LiteralPath (Join-Path $repositoryRoot 'service\connections_windows.go') -Raw
$sharesScript = [regex]::Matches($sharesSource, $scriptPattern)[0].Groups[1].Value
$connectionScripts = [regex]::Matches($connectionSource, $scriptPattern)
if (-not $sharesScript -or $connectionScripts.Count -ne 2) { throw 'Embedded scripts not found.' }

$script:removedShares = @()
$script:createdShares = @()
$script:lastPassword = ''
$script:lastRemote = ''
$script:access = @()
function Get-SmbShare {
    [pscustomobject]@{ Name = 'old'; Path = 'C:\old'; Description = 'CasaOS share old' }
    [pscustomobject]@{ Name = 'keep'; Path = 'C:\keep'; Description = 'CasaOS managed share' }
    [pscustomobject]@{ Name = 'unrelated'; Path = 'C:\other'; Description = 'manual' }
}
function Remove-SmbShare {
    [CmdletBinding(SupportsShouldProcess = $true)]
    param([string]$Name, [switch]$Force)
    $script:removedShares += $Name
}
function New-SmbShare {
    param([string]$Name, [string]$Path, [string]$ReadAccess, [string]$ChangeAccess, [string]$FullAccess, [string]$Description)
    if ($FullAccess) { throw 'CasaOS must not grant FullAccess.' }
    $script:createdShares += $Name
    $script:access += [pscustomobject]@{Name=$Name; Path=$Path; Read=$ReadAccess; Change=$ChangeAccess}
}
function Revoke-SmbShareAccess {
    [CmdletBinding(SupportsShouldProcess=$true)]
    param([string]$Name, [string]$AccountName, [switch]$Force)
}
function Grant-SmbShareAccess {
    [CmdletBinding(SupportsShouldProcess=$true)]
    param([string]$Name, [string]$AccountName, [string]$AccessRight, [switch]$Force)
    $script:access += [pscustomobject]@{Name=$Name; Account=$AccountName; Right=$AccessRight}
}
function New-SmbMapping {
    param([string]$RemotePath, [string]$UserName, [string]$Password, [bool]$Persistent)
    $script:lastRemote = $RemotePath
    $script:lastPassword = $Password
}
function Get-SmbMapping { param([string]$RemotePath) }
function Remove-SmbMapping {
    [CmdletBinding(SupportsShouldProcess = $true)]
    param([switch]$Force)
}
foreach ($name in @('Get-SmbShare', 'Remove-SmbShare', 'New-SmbShare', 'Revoke-SmbShareAccess', 'Grant-SmbShareAccess', 'New-SmbMapping', 'Get-SmbMapping', 'Remove-SmbMapping')) {
    if ((Get-Command $name).ModuleName) { throw "Mock missing for $name; refusing to run." }
}

foreach ($code in @($sharesScript, $connectionScripts[0].Groups[1].Value, $connectionScripts[1].Groups[1].Value)) {
    $tokens = $null
    $parseErrors = $null
    [System.Management.Automation.Language.Parser]::ParseInput($code, [ref]$tokens, [ref]$parseErrors) | Out-Null
    if ($parseErrors.Count) { throw ($parseErrors | Out-String) }
}
$env:CASAOS_SMB_SHARES = '[{"Name":"keep","Path":"C:\\keep"}]'
& ([ScriptBlock]::Create($sharesScript))
if ($script:removedShares.Count -ne 1 -or $script:removedShares[0] -ne 'old') { throw 'Deleted share was not removed, or unrelated shares were touched.' }
if ($script:createdShares.Count -ne 0) { throw 'Unchanged share was recreated.' }

$script:removedShares = @()
$script:access = @()
$env:CASAOS_SMB_SHARES = '[{"Name":"keep","Path":"D:\\new","ReadOnly":true}]'
& ([ScriptBlock]::Create($sharesScript))
if ($script:removedShares -notcontains 'keep') { throw 'Changed CasaOS path was not reconciled.' }
$created = $script:access | Where-Object Name -eq 'keep' | Select-Object -Last 1
if ($created.Path -ne 'D:\new' -or -not $created.Read -or $created.Change) { throw 'Read-only access or changed path was lost.' }

$env:CASAOS_SMB_SHARES = '[{"Name":"unrelated","Path":"C:\\other"}]'
$rejected = $false
try { & ([ScriptBlock]::Create($sharesScript)) } catch { $rejected = $true }
if (-not $rejected) { throw 'Foreign share was not protected.' }

$script:removedShares = @()
$env:CASAOS_SMB_SHARES = '[]'
& ([ScriptBlock]::Create($sharesScript))
if ($script:removedShares.Count -ne 2 -or $script:removedShares -contains 'unrelated') { throw 'Empty desired state did not revoke only CasaOS shares.' }

$hostileName = "new'; throw 'must-never-execute"
$env:CASAOS_SMB_SHARES = ConvertTo-Json -InputObject @(@{ Name = $hostileName; Path = "C:\O'Brien" }) -Compress
& ([ScriptBlock]::Create($sharesScript))
if ($script:createdShares -notcontains $hostileName) { throw 'Share name was not passed as literal data.' }

$password = "pass'; throw 'must-never-execute"
$env:CASAOS_SMB_REMOTE = '\\server\share'
$env:CASAOS_SMB_USER = "O'Brien"
$env:CASAOS_SMB_PASSWORD = $password
& ([ScriptBlock]::Create($connectionScripts[0].Groups[1].Value))
if ($script:lastPassword -ne $password -or $script:lastRemote -ne '\\server\share') { throw 'Connection data was changed or interpreted as code.' }
Write-Output 'PASS: SMB share reconciliation and literal credential/path handling (mocked).'

$tokens = $null
$parseErrors = $null
[System.Management.Automation.Language.Parser]::ParseFile((Join-Path $repositoryRoot 'install-service.ps1'), [ref]$tokens, [ref]$parseErrors) | Out-Null
if ($parseErrors.Count) { throw ($parseErrors | Out-String) }
Write-Output 'PASS: scheduled-task installer syntax.'
