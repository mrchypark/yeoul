param(
    [string]$Version = $(if ($env:YEOUL_VERSION) { $env:YEOUL_VERSION } else { "latest" }),
    [string]$InstallRoot = $(if ($env:YEOUL_INSTALL_ROOT) { $env:YEOUL_INSTALL_ROOT } else { Join-Path $env:LOCALAPPDATA "Programs\yeoul" }),
    [string]$Repo = $(if ($env:YEOUL_REPO) { $env:YEOUL_REPO } else { "mrchypark/yeoul" }),
    [switch]$SkipPathUpdate
)

$ErrorActionPreference = "Stop"

function Resolve-Tag {
    param([string]$RequestedVersion, [string]$Repository)

    if ($RequestedVersion -and $RequestedVersion -ne "latest") {
        if ($RequestedVersion.StartsWith("v")) {
            return $RequestedVersion
        }
        return "v$RequestedVersion"
    }

    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repository/releases/latest"
    if (-not $release.tag_name) {
        throw "Failed to resolve latest release tag."
    }
    return $release.tag_name
}

function Add-ToUserPath {
    param([string]$PathEntry)

    $currentUserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $entries = @()
    if ($currentUserPath) {
        $entries = $currentUserPath.Split(';', [System.StringSplitOptions]::RemoveEmptyEntries)
    }

    if ($entries -contains $PathEntry) {
        return
    }

    $updatedPath = if ($currentUserPath) { "$currentUserPath;$PathEntry" } else { $PathEntry }
    [Environment]::SetEnvironmentVariable("Path", $updatedPath, "User")
    $env:Path = "$PathEntry;$env:Path"
}

$arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
if ($arch -ne "x64") {
    throw "Windows installation is currently supported only on x64."
}

$tag = Resolve-Tag -RequestedVersion $Version -Repository $Repo
$assetVersion = $tag.TrimStart('v')
$archiveName = "yeoul_${assetVersion}_windows_amd64.zip"
$checksumName = "checksums_windows-amd64.txt"
$baseUrl = "https://github.com/$Repo/releases/download/$tag"

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("yeoul-install-" + [System.Guid]::NewGuid().ToString("N"))
$null = New-Item -ItemType Directory -Path $tempDir -Force

try {
    $archivePath = Join-Path $tempDir $archiveName
    $checksumPath = Join-Path $tempDir $checksumName

    Invoke-WebRequest -Uri "$baseUrl/$archiveName" -OutFile $archivePath
    Invoke-WebRequest -Uri "$baseUrl/$checksumName" -OutFile $checksumPath

    $expectedLine = Select-String -Path $checksumPath -Pattern ([regex]::Escape($archiveName) + '$') | Select-Object -First 1
    if (-not $expectedLine) {
        throw "Missing checksum entry for $archiveName."
    }

    $expectedHash = ($expectedLine.Line -split '\s+')[0].ToLowerInvariant()
    $actualHash = (Get-FileHash -Algorithm SHA256 -Path $archivePath).Hash.ToLowerInvariant()
    if ($expectedHash -ne $actualHash) {
        throw "Checksum mismatch for $archiveName."
    }

    $extractDir = Join-Path $tempDir "extract"
    Expand-Archive -Path $archivePath -DestinationPath $extractDir -Force

    $targetDir = Join-Path $InstallRoot $tag
    if (Test-Path $targetDir) {
        Remove-Item -Recurse -Force $targetDir
    }
    $null = New-Item -ItemType Directory -Path $InstallRoot -Force

    $extractedRoot = Get-ChildItem -Path $extractDir -Directory | Select-Object -First 1
    if (-not $extractedRoot) {
        throw "Failed to locate extracted archive directory."
    }
    Move-Item -Path $extractedRoot.FullName -Destination $targetDir

    $binDir = Join-Path $targetDir "bin"
    if (-not $SkipPathUpdate) {
        Add-ToUserPath -PathEntry $binDir
    }

    Write-Host "Installed Yeoul $tag to $targetDir"
    Write-Host "Binaries are available in $binDir"
    if (-not $SkipPathUpdate) {
        Write-Host "Added $binDir to the user PATH. Open a new shell to pick it up everywhere."
    }
    Write-Host "If Windows reports a missing runtime, install the Microsoft Visual C++ 2015-2022 Redistributable (x64)."
}
finally {
    if (Test-Path $tempDir) {
        Remove-Item -Recurse -Force $tempDir
    }
}
