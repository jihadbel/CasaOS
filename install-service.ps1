$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'scripts\install-security.ps1')

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Run this installer as administrator.'
}

$sourceExecutable = Join-Path $PSScriptRoot 'casaos-server.exe'
if (-not (Test-Path -LiteralPath $sourceExecutable -PathType Leaf)) {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw 'Build casaos-server.exe first, or install Go and run the installer again.'
    }
    Push-Location -LiteralPath $PSScriptRoot
    try {
        & go build -o $sourceExecutable .
        if ($LASTEXITCODE -ne 0) { throw 'CasaOS compilation failed.' }
    } finally {
        Pop-Location
    }
}

# Resolve the trusted Windows folder through the OS, not an inherited env var.
$programFiles = [Environment]::GetFolderPath([Environment+SpecialFolder]::ProgramFiles)
if (-not [IO.Path]::IsPathRooted($programFiles)) { throw 'Program Files is unavailable.' }
$installDirectory = Join-Path $programFiles 'CasaOS'
$serverExecutable = Join-Path $installDirectory 'casaos-server.exe'
Assert-CasaOSInstallPath -Path $serverExecutable
if (Test-Path -LiteralPath $installDirectory) {
    # Never try to repair a directory that an untrusted account can replace
    # while the elevated installer is using it.
    Assert-CasaOSInstallAcl -Path $installDirectory -AllowInherited
}
[IO.Directory]::CreateDirectory($installDirectory) | Out-Null
Set-CasaOSInstallAcl -Path $installDirectory -Directory

# Publish a fresh protected file; do not retain a previous file's writable ACL.
$stagedExecutable = Join-Path $installDirectory ('casaos-' + [guid]::NewGuid().ToString('N') + '.tmp')
try {
    Copy-Item -LiteralPath $sourceExecutable -Destination $stagedExecutable
    Set-CasaOSInstallAcl -Path $stagedExecutable
    if (Test-Path -LiteralPath $serverExecutable) {
        Assert-CasaOSInstallPath -Path $serverExecutable
        Remove-Item -LiteralPath $serverExecutable -Force
    }
    Move-Item -LiteralPath $stagedExecutable -Destination $serverExecutable
    Assert-CasaOSInstallAcl -Path $serverExecutable
} finally {
    if (Test-Path -LiteralPath $stagedExecutable) { Remove-Item -LiteralPath $stagedExecutable -Force }
}

$taskAction = New-ScheduledTaskAction -Execute $serverExecutable -WorkingDirectory $installDirectory
$taskTrigger = New-ScheduledTaskTrigger -AtStartup
$taskPrincipal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
$taskSettings = New-ScheduledTaskSettingsSet `
    -ExecutionTimeLimit ([TimeSpan]::Zero) `
    -StartWhenAvailable `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -RestartCount 3 `
    -RestartInterval (New-TimeSpan -Minutes 1) `
    -MultipleInstances IgnoreNew

Register-ScheduledTask -TaskName 'CasaOS_FileServer' -Action $taskAction -Trigger $taskTrigger `
    -Principal $taskPrincipal -Settings $taskSettings -Force | Out-Null
Write-Output "[OK] CasaOS_FileServer will run from the protected installation: $serverExecutable"
