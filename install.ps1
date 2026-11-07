#requires -Version 5.1
[CmdletBinding()]
param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\previewctl'),
    [switch]$NoPathUpdate
)
$ErrorActionPreference = 'Stop'
$source = Join-Path $PSScriptRoot 'previewctl.exe'
if (!(Test-Path -LiteralPath $source -PathType Leaf)) { throw 'Extract the Windows release archive before installing.' }
$InstallDir = [System.IO.Path]::GetFullPath($InstallDir)
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
$destination = Join-Path $InstallDir 'previewctl.exe'
if ($source -ne $destination) { Copy-Item -LiteralPath $source -Destination $destination -Force }
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$parts = @($userPath -split ';' | Where-Object { $_ })
if (!$NoPathUpdate -and !($parts | Where-Object { $_.TrimEnd('\') -ieq $InstallDir.TrimEnd('\') })) {
    [Environment]::SetEnvironmentVariable('Path', (($parts + $InstallDir) -join ';'), 'User')
}
Write-Output "Installed: $destination"
if (!$NoPathUpdate) { Write-Output 'Open a new terminal, then run: previewctl version' }
