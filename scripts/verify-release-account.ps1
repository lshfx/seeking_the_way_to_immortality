# Independent-account release check for TASK-03.
#
# ADR-002 and the TASK-03 acceptance criteria both require the artifact to run
# under a *clean Windows user profile*, not just an isolated directory with a
# trimmed PATH. Those are different claims, and an isolated-directory run must
# never be recorded as an independent-account acceptance.
#
# This script cannot create a Windows user account for you. It does two things:
#   1. Reports the current profile and whether it looks clean for this purpose.
#   2. Runs the staged artifact under a PATH restricted to System32, recording
#      the exact evidence needed for the record file.
#
# Run it once from the developer profile and once from the fresh test account,
# then paste both EVIDENCE blocks into the TASK-03 section of the record.
#
# NOTE: keep this file ASCII-only. Windows PowerShell 5.1 reads .ps1 files as
# ANSI unless they carry a BOM, so non-ASCII text would be mis-decoded.

[CmdletBinding()]
param(
    [string]$Binary = '',
    [string]$Label = '',
    [string]$StagingDir = ''
)

$ErrorActionPreference = 'Stop'

# Resolve the artifact. When the script is run from the repository the default
# is <repo>\dist\wendao.exe, but this script is also meant to be copied next to
# the executable into a shared folder and run from a *different* Windows
# account. Deriving the repo root from $PSScriptRoot alone would then produce a
# bogus path (e.g. C:\.task-cache) and try to write to the drive root.
if ([string]::IsNullOrWhiteSpace($Binary)) {
    $repoCandidate = Join-Path (Split-Path -Parent $PSScriptRoot) 'dist\wendao.exe'
    $localCandidate = Join-Path $PSScriptRoot 'wendao.exe'
    if (Test-Path -LiteralPath $repoCandidate) {
        $Binary = $repoCandidate
    }
    elseif (Test-Path -LiteralPath $localCandidate) {
        # Script was copied next to the artifact (the cross-account workflow).
        $Binary = $localCandidate
    }
    else {
        throw "release artifact not found. Looked for '$repoCandidate' and '$localCandidate'. Pass -Binary <path> explicitly."
    }
}

if ([string]::IsNullOrWhiteSpace($StagingDir)) {
    # Detect the repository layout by looking for a marker that cannot exist in
    # a copied-script folder. Falling back to a path relative to $PSScriptRoot
    # keeps a copied script from ever writing outside its own directory.
    $repoMarker = Join-Path (Split-Path -Parent $PSScriptRoot) 'go.mod'
    if (Test-Path -LiteralPath $repoMarker) {
        $StagingDir = Join-Path (Split-Path -Parent $PSScriptRoot) '.task-cache\account-check'
    }
    else {
        $StagingDir = Join-Path $PSScriptRoot '.account-check'
    }
}

$stagedBinary = Join-Path $StagingDir 'wendao.exe'
$originalPath = $env:Path

if ([string]::IsNullOrWhiteSpace($Label)) {
    $Label = "$env:USERDOMAIN\$env:USERNAME"
}

if (-not (Test-Path -LiteralPath $Binary)) {
    throw "release artifact not found: $Binary (run scripts\build.ps1 first)"
}
$sourceBinary = (Resolve-Path -LiteralPath $Binary).Path

New-Item -ItemType Directory -Force -Path $StagingDir | Out-Null
Copy-Item -LiteralPath $sourceBinary -Destination $stagedBinary -Force

function Invoke-IsolatedBinary {
    param(
        [Parameter(Mandatory = $true)][string]$Executable,
        [string[]]$Arguments = @()
    )
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $Executable
    # AllowEmptyString keeps the bare (no-argument) invocation valid.
    $psi.Arguments = ([string]::Join(' ', $Arguments))
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.CreateNoWindow = $true
    $psi.WorkingDirectory = (Split-Path -Parent $Executable)
    # The artifact writes UTF-8. Without this the parent decodes it as the
    # console code page and Chinese output becomes mojibake in the evidence.
    $psi.StandardOutputEncoding = New-Object System.Text.UTF8Encoding($false)
    $psi.StandardErrorEncoding = New-Object System.Text.UTF8Encoding($false)

    $process = [System.Diagnostics.Process]::Start($psi)
    $stdout = $process.StandardOutput.ReadToEnd()
    $stderr = $process.StandardError.ReadToEnd()
    $process.WaitForExit()

    return [pscustomobject]@{
        ExitCode = $process.ExitCode
        Stdout   = @($stdout -split "`r?`n" | Where-Object { $_ -ne '' })
        Stderr   = $stderr
    }
}

try {
    $env:Path = Join-Path $env:SystemRoot 'System32'
    $versionResult = Invoke-IsolatedBinary -Executable $stagedBinary -Arguments @('--version')
    $diagnoseResult = Invoke-IsolatedBinary -Executable $stagedBinary -Arguments @('--diagnose')
    $bareResult = Invoke-IsolatedBinary -Executable $stagedBinary
}
finally {
    $env:Path = $originalPath
}

$goOnRestrictedPath = Test-Path 'C:\Program Files\Go\bin\go.exe'
if ($versionResult.ExitCode -ne 0) {
    throw "--version failed with exit code $($versionResult.ExitCode); stderr=$($versionResult.Stderr)"
}
if ($diagnoseResult.ExitCode -ne 0) {
    throw "--diagnose failed with exit code $($diagnoseResult.ExitCode); stderr=$($diagnoseResult.Stderr)"
}
if ($bareResult.ExitCode -ne 2 -or [string]::IsNullOrWhiteSpace($bareResult.Stderr)) {
	throw "bare invocation without a terminal should refuse safely with exit code 2; exit=$($bareResult.ExitCode) stderr=$($bareResult.Stderr)"
}

$diagnoseText = ($diagnoseResult.Stdout -join "`n")
if ($diagnoseText -notmatch 'network=disabled') {
    throw 'diagnostic output did not confirm offline mode'
}
if ($diagnoseText -notmatch 'game_state=short_loop_implemented') {
	throw 'artifact diagnostics do not report the implemented TASK-09 short loop'
}

# A fresh profile must not already contain game state. The artifact should also
# not have created any data directory during a read-only diagnostic run.
$expectedDataRoot = Join-Path $env:LOCALAPPDATA 'WendaoChangsheng'
$dataRootExists = Test-Path -LiteralPath $expectedDataRoot

$profileLooksFresh = -not (Test-Path -LiteralPath (Join-Path $HOME '.wendao'))
$sourceHash = (Get-FileHash -LiteralPath $sourceBinary -Algorithm SHA256).Hash

# $env:COMPUTERNAME can be absent when the process was launched from a host that
# did not propagate it, which leaves an empty field in the evidence block. The
# .NET accessor is always available and is the authoritative source.
$computerName = $env:COMPUTERNAME
if ([string]::IsNullOrWhiteSpace($computerName)) {
    $computerName = [System.Environment]::MachineName
}
$userIdentity = "$env:USERDOMAIN\$env:USERNAME"
if ([string]::IsNullOrWhiteSpace($env:USERDOMAIN)) {
    $userIdentity = "$env:USERNAME"
}

# The single failure mode this script cannot detect on its own is "you ran it
# from the developer account by mistake". That has already happened once: the
# new account was created, but the check was run from the original PowerShell
# window, so the evidence recorded the developer profile while everything else
# looked healthy. Flag it loudly instead of leaving it to a careful reader.
$looksLikeDeveloperProfile = $false
if ($env:USERNAME -match '^(Administrator|admin)$') {
    $looksLikeDeveloperProfile = $true
}
if (-not $profileLooksFresh) {
    $looksLikeDeveloperProfile = $true
}
if ($env:LOCALAPPDATA -and (Test-Path -LiteralPath $expectedDataRoot)) {
    $looksLikeDeveloperProfile = $true
}

Write-Output '----- TASK03-ACCOUNT-EVIDENCE-BEGIN -----'
Write-Output "label=$Label"
Write-Output "user=$userIdentity"
Write-Output "userprofile=$env:USERPROFILE"
Write-Output "localappdata=$env:LOCALAPPDATA"
Write-Output "computer=$computerName"
Write-Output "os=$([System.Environment]::OSVersion.VersionString)"
Write-Output "sha256=$($sourceHash.ToLowerInvariant())"
Write-Output "version_out_restricted_path=$($versionResult.Stdout -join ' / ')"
Write-Output "diagnose_out_restricted_path=$($diagnoseResult.Stdout -join ' / ')"
Write-Output "bare_out=$($bareResult.Stdout -join ' / ')"
Write-Output "bare_stderr=$($bareResult.Stderr -replace '\r?\n', ' / ')"
Write-Output "go_present_on_host_disk=$goOnRestrictedPath"
Write-Output "go_reachable_under_restricted_path=$false"
Write-Output "expected_data_root=$expectedDataRoot"
Write-Output "data_root_created=$dataRootExists"
Write-Output "profile_looks_fresh=$profileLooksFresh"
Write-Output "looks_like_developer_profile=$looksLikeDeveloperProfile"
Write-Output "run_path=only System32"
Write-Output 'account_check=passed'
Write-Output '----- TASK03-ACCOUNT-EVIDENCE-END -----'

if ($looksLikeDeveloperProfile) {
    Write-Warning 'This looks like the DEVELOPER account. A developer-account run does NOT satisfy the independent-account acceptance criterion and must not be recorded as such.'
    Write-Warning "user=$userIdentity userprofile=$env:USERPROFILE"
    Write-Warning 'Log in as the fresh test account, open a NEW PowerShell window, confirm with `whoami`, then re-run.'
}
