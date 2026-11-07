#requires -Version 5.1
$ErrorActionPreference = 'Stop'
if ($env:OS -ne 'Windows_NT') { throw 'Use setup.sh on macOS or Linux.' }

$version = (Get-Content -LiteralPath (Join-Path $PSScriptRoot 'VERSION') -Raw).Trim()
$nativeArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
switch ($nativeArch) {
    'x64' { $arch = 'amd64' }
    'arm64' { $arch = 'arm64' }
    default { throw 'Unsupported CPU. previewctl requires x64 or ARM64.' }
}

function Assert-Checksum([string]$Path, [string]$Name) {
    $entries = @(Get-Content -LiteralPath (Join-Path $PSScriptRoot 'checksums.txt') | ForEach-Object {
        $fields = $_ -split '\s+', 2
        if ($fields.Count -eq 2 -and $fields[1] -ceq $Name) { $fields[0] }
    })
    $actual = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($entries.Count -ne 1 -or $actual -cne $entries[0]) { throw "SHA-256 mismatch: $Path" }
}

$cacheRoot = $env:PREVIEWCTL_CACHE_DIR
if (!$cacheRoot) { $cacheRoot = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'previewctl\cache' }
$platform = "windows_$arch"
$cacheDir = [IO.Path]::GetFullPath((Join-Path $cacheRoot "$version\$platform"))
New-Item -ItemType Directory -Path $cacheDir -Force | Out-Null
$binary = Join-Path $cacheDir 'previewctl.exe'
if (Test-Path -LiteralPath $binary -PathType Leaf) {
    Assert-Checksum $binary "$platform/previewctl.exe"
    Write-Output $binary
    return
}

$tempDir = Join-Path $cacheDir ('.download.' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tempDir | Out-Null
$archive = "previewctl_${version}_${platform}.zip"
$archivePath = Join-Path $tempDir $archive
$candidate = Join-Path $tempDir 'previewctl.exe'
$previousProgress = $ProgressPreference
$previousTls = [Net.ServicePointManager]::SecurityProtocol
try {
    $ProgressPreference = 'SilentlyContinue'
    [Net.ServicePointManager]::SecurityProtocol = $previousTls -bor [Net.SecurityProtocolType]::Tls12
    [Console]::Error.WriteLine("Downloading previewctl v$version for $platform...")
    Invoke-WebRequest -UseBasicParsing -TimeoutSec 120 -Uri "https://github.com/kamedesuyo/previewctl/releases/download/v$version/$archive" -OutFile $archivePath
    Assert-Checksum $archivePath $archive
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead($archivePath)
    try {
        $entry = $zip.GetEntry('previewctl.exe')
        if (!$entry) { throw 'Release archive does not contain previewctl.exe.' }
        [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $candidate)
    } finally { $zip.Dispose() }
    Assert-Checksum $candidate "$platform/previewctl.exe"
    try { [IO.File]::Move($candidate, $binary) } catch {
        if (!(Test-Path -LiteralPath $binary -PathType Leaf)) { throw }
        Assert-Checksum $binary "$platform/previewctl.exe"
    }
} finally {
    $ProgressPreference = $previousProgress
    [Net.ServicePointManager]::SecurityProtocol = $previousTls
    foreach ($temporaryFile in @($archivePath, $candidate)) {
        if (Test-Path -LiteralPath $temporaryFile -PathType Leaf) { Remove-Item -LiteralPath $temporaryFile -Force }
    }
    Remove-Item -LiteralPath $tempDir -Force
}
Write-Output $binary
