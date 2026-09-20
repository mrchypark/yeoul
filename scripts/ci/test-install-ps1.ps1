<#
.SYNOPSIS
Regression tests for the Windows installer PATH handling and release compatibility
policy in scripts/install.ps1.

The installer body is dot-sourced so the internal helpers can be exercised
directly. Add-ToUserPath persists through the $UserPathStore test seam, so the
real user environment is never touched and the tests run on any platform.
#>
[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptDir = $PSScriptRoot
$repoRoot = Split-Path -Parent (Split-Path -Parent $scriptDir)
$installerPath = Join-Path $repoRoot "scripts/install.ps1"

if (-not (Test-Path -LiteralPath $installerPath)) {
    throw "installer not found at $installerPath"
}

# The installer's param() block is not first inside the extracted text, so the
# function definitions are evaluated from the AST instead of dot-sourcing.
$installerSource = Get-Content -LiteralPath $installerPath -Raw
$parseTokens = $null
$parseErrors = $null
$installerAst = [System.Management.Automation.Language.Parser]::ParseInput($installerSource, [ref]$parseTokens, [ref]$parseErrors)
if ($parseErrors -and $parseErrors.Count -gt 0) {
    throw "scripts/install.ps1 does not parse: $($parseErrors[0].Message)"
}

$functionDefinitions = $installerAst.FindAll(
    { param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] },
    $true)
foreach ($definition in $functionDefinitions) {
    Invoke-Expression $definition.Extent.Text
}

foreach ($required in @("Test-VersionAtLeast", "Test-InstallerOwnedBinDir", "Get-UpdatedUserPath", "Add-ToUserPath")) {
    if (-not (Get-Command $required -CommandType Function -ErrorAction SilentlyContinue)) {
        throw "installer helper $required was not extracted from scripts/install.ps1"
    }
}

$workDir = Join-Path ([System.IO.Path]::GetTempPath()) ("yeoul-ps1-tests-" + [System.Guid]::NewGuid().ToString("N"))
$null = New-Item -ItemType Directory -Path $workDir -Force

$script:FailureCount = 0
$script:CheckCount = 0

function Write-Section {
    param([string]$Name)
    Write-Host ""
    Write-Host "== $Name =="
}

function Assert-Equal {
    param([string]$Label, $Expected, $Actual)

    $script:CheckCount++
    if ("$Expected" -ceq "$Actual") {
        Write-Host "  ok   $Label"
        return
    }
    $script:FailureCount++
    Write-Host "  FAIL $Label"
    Write-Host "       expected: $Expected"
    Write-Host "       actual:   $Actual"
}

function Assert-True {
    param([string]$Label, $Condition)

    Assert-Equal -Label $Label -Expected $true -Actual ([bool]$Condition)
}

function Assert-FileText {
    param([string]$Label, [string]$Path, [string]$Expected)

    $script:CheckCount++
    $actual = ""
    if (Test-Path -LiteralPath $Path) {
        $actual = (Get-Content -LiteralPath $Path -Raw)
    }
    if ($actual -ceq $Expected) {
        Write-Host "  ok   $Label"
        return
    }
    $script:FailureCount++
    Write-Host "  FAIL $Label"
    Write-Host "       expected: [$Expected]"
    Write-Host "       actual:   [$actual]"
}

function New-TempPathStore {
    param([string]$Name)
    return (Join-Path $workDir ("path-store-" + $Name + ".txt"))
}

try {
    Write-Section "Test-VersionAtLeast"
    Assert-True "v0.5.1 is at least v0.5.1" (Test-VersionAtLeast -Version "v0.5.1" -Minimum "v0.5.1")
    Assert-True "v0.5.5 is at least v0.5.1" (Test-VersionAtLeast -Version "v0.5.5" -Minimum "v0.5.1")
    Assert-True "v0.5.10 is newer than v0.5.9" (Test-VersionAtLeast -Version "v0.5.10" -Minimum "v0.5.9")
    Assert-True "v0.6.0 is at least v0.5.1" (Test-VersionAtLeast -Version "v0.6.0" -Minimum "v0.5.1")
    Assert-True "v1.0.0 is at least v0.5.1" (Test-VersionAtLeast -Version "v1.0.0" -Minimum "v0.5.1")
    Assert-True "0.5.5 without prefix is at least v0.5.1" (Test-VersionAtLeast -Version "0.5.5" -Minimum "v0.5.1")
    Assert-True "v0.5.0 is older than v0.5.1" (-not (Test-VersionAtLeast -Version "v0.5.0" -Minimum "v0.5.1"))
    Assert-True "v0.1.0 is older than v0.5.1" (-not (Test-VersionAtLeast -Version "v0.1.0" -Minimum "v0.5.1"))
    Assert-True "v0.4.99 is older than v0.5.1" (-not (Test-VersionAtLeast -Version "v0.4.99" -Minimum "v0.5.1"))

    Write-Section "Test-InstallerOwnedBinDir"
    $root = Join-Path $workDir "Programs/yeoul"
    Assert-True "own version bin dir is owned" (Test-InstallerOwnedBinDir -Candidate (Join-Path $root "v0.5.5/bin") -Root $root)
    Assert-True "backslash separator is owned" (Test-InstallerOwnedBinDir -Candidate "$root\v0.5.5\bin" -Root $root)
    Assert-True "case difference is owned" (Test-InstallerOwnedBinDir -Candidate (Join-Path $root "V0.5.5/BIN") -Root $root)
    Assert-True "trailing separator is owned" (Test-InstallerOwnedBinDir -Candidate ((Join-Path $root "v0.5.5/bin") + "/") -Root $root)
    Assert-True "retained install under another root is not owned" (-not (Test-InstallerOwnedBinDir -Candidate (Join-Path $workDir "other/yeoul/v0.5.5/bin") -Root $root))
    Assert-True "unversioned bin dir is not owned" (-not (Test-InstallerOwnedBinDir -Candidate (Join-Path $root "bin") -Root $root))
    Assert-True "non-bin directory is not owned" (-not (Test-InstallerOwnedBinDir -Candidate (Join-Path $root "v0.5.5/libexec") -Root $root))
    Assert-True "unrelated directory is not owned" (-not (Test-InstallerOwnedBinDir -Candidate (Join-Path $workDir "tools/bin") -Root $root))

    Write-Section "Add-ToUserPath persistence"
    $entry = Join-Path $root "v0.5.5/bin"
    $otherEntry = Join-Path $root "v0.5.4/bin"

    $freshStore = New-TempPathStore "fresh"
    $UserPathStore = $freshStore
    $env:Path = "C:\Windows"
    Add-ToUserPath -PathEntry $entry -Root $root
    Assert-FileText "fresh install writes the new bin dir" $freshStore $entry
    Assert-Equal "fresh install prepends to the process PATH" "$entry;C:\Windows" $env:Path

    $existingStore = New-TempPathStore "existing"
    Set-Content -LiteralPath $existingStore -Value "C:\Windows;C:\Tools;$otherEntry" -NoNewline
    $UserPathStore = $existingStore
    $env:Path = "C:\Windows;C:\Tools"
    Add-ToUserPath -PathEntry $entry -Root $root
    Assert-FileText "old Yeoul bin dir is removed and the new one is first" $existingStore "$entry;C:\Windows;C:\Tools"
    Assert-Equal "process PATH keeps unrelated order with the new entry first" "$entry;C:\Windows;C:\Tools" $env:Path

    $retainedStore = New-TempPathStore "retained"
    $retainedEntry = Join-Path $workDir "other/yeoul/v0.5.4/bin"
    Set-Content -LiteralPath $retainedStore -Value "$retainedEntry;C:\Tools" -NoNewline
    $UserPathStore = $retainedStore
    $env:Path = "$retainedEntry;C:\Tools"
    Add-ToUserPath -PathEntry $entry -Root $root
    Assert-FileText "retained install under another root is preserved" $retainedStore "$entry;$retainedEntry;C:\Tools"

    $duplicateStore = New-TempPathStore "duplicate"
    Set-Content -LiteralPath $duplicateStore -Value "$entry;C:\Tools;$entry" -NoNewline
    $UserPathStore = $duplicateStore
    $env:Path = "C:\Tools"
    Add-ToUserPath -PathEntry $entry -Root $root
    Assert-FileText "duplicate entries collapse to one leading entry" $duplicateStore "$entry;C:\Tools"

    $idempotentStore = New-TempPathStore "idempotent"
    Set-Content -LiteralPath $idempotentStore -Value "$entry;C:\Tools" -NoNewline
    $UserPathStore = $idempotentStore
    $env:Path = "$entry;C:\Tools"
    Add-ToUserPath -PathEntry $entry -Root $root
    Assert-FileText "reinstall leaves an already-correct PATH unchanged" $idempotentStore "$entry;C:\Tools"

    Write-Section "Rollback keeps the newer selection in a fresh environment"
    $freshEnvStore = New-TempPathStore "fresh-env"
    $newerEntry = Join-Path $root "v0.5.6/bin"
    Set-Content -LiteralPath $freshEnvStore -Value "$otherEntry;C:\Windows" -NoNewline
    $UserPathStore = $freshEnvStore
    $env:Path = "C:\Windows"
    Add-ToUserPath -PathEntry $newerEntry -Root $root
    $persisted = Get-Content -LiteralPath $freshEnvStore -Raw
    $firstEntry = ($persisted -split ';')[0]
    Assert-Equal "fresh shell resolves the newly installed version" $newerEntry $firstEntry
    Assert-True "fresh shell PATH no longer contains the older version" (-not ($persisted -split ';' -contains $otherEntry))
}
finally {
    if (Test-Path -LiteralPath $workDir) {
        Remove-Item -Recurse -Force -LiteralPath $workDir
    }
}

Write-Host ""
if ($script:FailureCount -ne 0) {
    Write-Host "$($script:FailureCount) of $($script:CheckCount) checks failed"
    exit 1
}
Write-Host "PowerShell installer tests passed ($($script:CheckCount) checks)"
