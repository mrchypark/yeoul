param(
    [string]$Version = $(if ($env:YEOUL_VERSION) { $env:YEOUL_VERSION } else { "latest" }),
    [string]$InstallRoot = $(if ($env:YEOUL_INSTALL_ROOT) { $env:YEOUL_INSTALL_ROOT } else { Join-Path $env:LOCALAPPDATA "Programs\yeoul" }),
    [string]$Repo = $(if ($env:YEOUL_REPO) { $env:YEOUL_REPO } else { "mrchypark/yeoul" }),
    [switch]$SkipPathUpdate,
    # Test seam: when set, the persisted user PATH is read from and written to
    # this file instead of the Windows user environment, so the PATH update can
    # be exercised on any platform without touching the real environment.
    [string]$UserPathStore = ""
)

$ErrorActionPreference = "Stop"

# The version-pinned Ladybug migration helper first shipped inside the v0.5.1
# archives. Older tags only carry the binaries, so requiring the helper for
# them would reject supported historical installs.
$MigrationHelperMinVersion = "v0.5.1"

# Resolve the install root once so PATH entries and staged directories stay
# absolute even when the caller passes a relative path.
$InstallRoot = [System.IO.Path]::GetFullPath($InstallRoot)

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

function Test-VersionAtLeast {
    param([string]$Version, [string]$Minimum)

    $have = @($Version.TrimStart('v') -split '\.')
    $want = @($Minimum.TrimStart('v') -split '\.')
    $count = [Math]::Max($have.Count, $want.Count)

    for ($i = 0; $i -lt $count; $i++) {
        $left = 0
        $right = 0
        if ($i -lt $have.Count) {
            [void][int]::TryParse(($have[$i] -replace '[^0-9].*$', ''), [ref]$left)
        }
        if ($i -lt $want.Count) {
            [void][int]::TryParse(($want[$i] -replace '[^0-9].*$', ''), [ref]$right)
        }
        if ($left -ne $right) {
            return ($left -gt $right)
        }
    }
    return $true
}

function Test-InstallerOwnedBinDir {
    # True only for <Root>\<tag>\bin, the bin directory this installer adds for
    # a single Yeoul release, so unrelated entries and retained installations
    # under other roots stay untouched.
    param([string]$Candidate, [string]$Root)

    # Normalize separators first so a PATH entry written with backslashes is
    # recognized even when the tests run on a platform where "\" is a literal
    # filename character.
    $separator = [System.IO.Path]::DirectorySeparatorChar
    $candidateFull = ([System.IO.Path]::GetFullPath(($Candidate -replace '\\', $separator))).TrimEnd('\', '/')
    $rootFull = ([System.IO.Path]::GetFullPath(($Root -replace '\\', $separator))).TrimEnd('\', '/')

    if ($candidateFull -ieq $rootFull) {
        return $false
    }
    if (([System.IO.Path]::GetFileName($candidateFull)) -ine 'bin') {
        return $false
    }

    $versionDir = ([System.IO.Path]::GetDirectoryName($candidateFull)).TrimEnd('\', '/')
    if (-not $versionDir) {
        return $false
    }
    if (([System.IO.Path]::GetDirectoryName($versionDir)) -ine $rootFull) {
        return $false
    }
    return (([System.IO.Path]::GetFileName($versionDir)) -like 'v*')
}

function Get-UpdatedUserPath {
    # Returns the PATH value that puts <Entry> first and drops the Yeoul bin
    # directories this installer owns, or $null when the current value already
    # matches. Unrelated entries keep their original order.
    param([string]$CurrentPath, [string]$Entry, [string]$Root)

    $existing = @()
    if ($CurrentPath) {
        $existing = $CurrentPath.Split(';', [System.StringSplitOptions]::RemoveEmptyEntries) | ForEach-Object { $_.Trim() }
    }
    $existing = @($existing | Where-Object { $_ })

    $kept = @()
    foreach ($candidate in $existing) {
        $unquoted = $candidate.Trim('"')
        if ($unquoted -ieq $Entry) {
            continue
        }
        if (Test-InstallerOwnedBinDir -Candidate $unquoted -Root $Root) {
            continue
        }
        $kept += $candidate
    }

    $updated = @($Entry) + $kept
    if ($existing.Count -eq $updated.Count) {
        $same = $true
        for ($i = 0; $i -lt $existing.Count; $i++) {
            if ($existing[$i] -ine $updated[$i]) {
                $same = $false
                break
            }
        }
        if ($same) {
            return $null
        }
    }

    return ($updated -join ';')
}

function Add-ToUserPath {
    param([string]$PathEntry, [string]$Root)

    $currentUserPath = Get-PersistedUserPath
    $updatedUserPath = Get-UpdatedUserPath -CurrentPath $currentUserPath -Entry $PathEntry -Root $Root
    if ($null -ne $updatedUserPath) {
        Set-PersistedUserPath -Value $updatedUserPath
    }

    $updatedProcessPath = Get-UpdatedUserPath -CurrentPath $env:Path -Entry $PathEntry -Root $Root
    if ($null -ne $updatedProcessPath) {
        $env:Path = $updatedProcessPath
    }
}

function Get-PersistedUserPath {
    if ($UserPathStore) {
        if (Test-Path -LiteralPath $UserPathStore) {
            return (Get-Content -LiteralPath $UserPathStore -Raw)
        }
        return ""
    }
    return [Environment]::GetEnvironmentVariable("Path", "User")
}

function Set-PersistedUserPath {
    param([string]$Value)

    if ($UserPathStore) {
        Set-Content -LiteralPath $UserPathStore -Value $Value -NoNewline
        return
    }
    [Environment]::SetEnvironmentVariable("Path", $Value, "User")
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
$stagingDir = $null

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
    $null = New-Item -ItemType Directory -Path $InstallRoot -Force

    $extractedRoot = Get-ChildItem -Path $extractDir -Directory | Select-Object -First 1
    if (-not $extractedRoot) {
        throw "Failed to locate extracted archive directory."
    }
    $stagingDir = Join-Path $InstallRoot (".$tag.staging-" + [System.Guid]::NewGuid().ToString("N"))
    $backupDir = Join-Path $InstallRoot (".$tag.previous-" + [System.Guid]::NewGuid().ToString("N"))
    Move-Item -Path $extractedRoot.FullName -Destination $stagingDir

    foreach ($exe in @("yeoul.exe", "yeould.exe")) {
        $exePath = Join-Path $stagingDir "bin\$exe"
        if (-not (Test-Path -LiteralPath $exePath)) {
            throw "Archive is missing executable bin/$exe."
        }
    }

    if (Test-VersionAtLeast -Version $tag -Minimum $MigrationHelperMinVersion) {
        $migrationHelper = Join-Path $stagingDir "libexec\ladybug-v0131\yeoul-migrate-v0131.exe"
        if (-not (Test-Path -LiteralPath $migrationHelper)) {
            throw "Archive is missing the version-pinned Ladybug migration helper."
        }
    }

    $hasBackup = $false
    $targetMoved = $false
    try {
        if (Test-Path -LiteralPath $targetDir) {
            Move-Item -LiteralPath $targetDir -Destination $backupDir
            $hasBackup = $true
            $targetMoved = $true
        }
        Move-Item -LiteralPath $stagingDir -Destination $targetDir
        $stagingDir = $null
        if ($hasBackup -and (Test-Path -LiteralPath $backupDir)) {
            Remove-Item -Recurse -Force -LiteralPath $backupDir
        }
    }
    catch {
        if ($targetMoved -and (Test-Path -LiteralPath $targetDir)) {
            Remove-Item -Recurse -Force -LiteralPath $targetDir
        }
        if ($hasBackup -and (Test-Path -LiteralPath $backupDir)) {
            Move-Item -LiteralPath $backupDir -Destination $targetDir
        }
        throw
    }

    $binDir = Join-Path $targetDir "bin"
    if (-not $SkipPathUpdate) {
        Add-ToUserPath -PathEntry $binDir -Root $InstallRoot
    }

    Write-Host "Installed Yeoul $tag to $targetDir"
    Write-Host "Binaries are available in $binDir"
    if (-not $SkipPathUpdate) {
        Write-Host "Added $binDir to the user PATH, ahead of any other installed Yeoul version. Open a new shell to pick it up everywhere."
    }
    Write-Host "If Windows reports a missing runtime, install the Microsoft Visual C++ 2015-2022 Redistributable (x64)."
}
finally {
    if ($stagingDir -and (Test-Path -LiteralPath $stagingDir)) {
        Remove-Item -Recurse -Force -LiteralPath $stagingDir
    }
    if (Test-Path $tempDir) {
        Remove-Item -Recurse -Force $tempDir
    }
}
