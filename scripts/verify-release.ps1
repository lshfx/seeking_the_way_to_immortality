[CmdletBinding()]
param(
    [string]$Binary = (Join-Path (Split-Path -Parent $PSScriptRoot) 'dist\wendao.exe')
)

# NOTE: keep this file ASCII-only. Windows PowerShell 5.1 reads .ps1 files as
# ANSI unless they carry a BOM, so non-ASCII text would be mis-decoded.

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$stagingDir = Join-Path $repoRoot '.task-cache\release-check'
$stagedBinary = Join-Path $stagingDir 'wendao.exe'
$originalPath = $env:Path

if (-not (Test-Path -LiteralPath $Binary)) {
    throw "release artifact not found: $Binary (run scripts\build.ps1 first)"
}
$sourceBinary = (Resolve-Path -LiteralPath $Binary).Path

New-Item -ItemType Directory -Force -Path $stagingDir | Out-Null
Copy-Item -LiteralPath $sourceBinary -Destination $stagedBinary -Force

# The staged copy must exist before we try to run it. Earlier revisions could
# report a passing run even when staging produced nothing, because they only
# checked the source path.
if (-not (Test-Path -LiteralPath $stagedBinary)) {
    throw "staging directory did not receive the artifact: $stagedBinary"
}

function Invoke-IsolatedBinary {
    param(
        [Parameter(Mandatory = $true)][string]$Executable,
        [Parameter(Mandatory = $true)][string[]]$Arguments
    )

    # Run through .NET Process instead of the call operator. The call operator
    # does not reliably populate $LASTEXITCODE for console apps in every
    # PowerShell host, which previously made this script throw
    # "--version failed with exit code " with an empty code and abort before any
    # real assertion ran.
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $Executable
    $psi.Arguments = ($Arguments -join ' ')
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.CreateNoWindow = $true
    $psi.WorkingDirectory = (Split-Path -Parent $Executable)

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
    # Keep only Windows system commands on PATH. The executable must not find
    # Go, Python, Node, package managers, or compilers at runtime.
    $env:Path = Join-Path $env:SystemRoot 'System32'

    $versionResult = Invoke-IsolatedBinary -Executable $stagedBinary -Arguments @('--version')
    $diagnoseResult = Invoke-IsolatedBinary -Executable $stagedBinary -Arguments @('--diagnose')
}
finally {
    $env:Path = $originalPath
}

if ($versionResult.ExitCode -ne 0) {
    throw "--version failed with exit code $($versionResult.ExitCode); stderr=$($versionResult.Stderr)"
}
if ($diagnoseResult.ExitCode -ne 0) {
    throw "--diagnose failed with exit code $($diagnoseResult.ExitCode); stderr=$($diagnoseResult.Stderr)"
}

$versionText = ($versionResult.Stdout -join "`n")
$diagnoseText = ($diagnoseResult.Stdout -join "`n")

if ($versionText -notmatch 'wendao') {
    throw "--version output did not contain the product name: $versionText"
}
if ($diagnoseText -notmatch 'network=disabled') {
    throw 'diagnostic output did not confirm offline mode'
}
if ($diagnoseText -notmatch 'game_state=short_loop_implemented') {
	throw 'artifact diagnostics do not report the implemented TASK-09 short loop'
}

# The staged artifact must be byte-identical to the source it came from.
$sourceHash = (Get-FileHash -LiteralPath $sourceBinary -Algorithm SHA256).Hash
$stagedHash = (Get-FileHash -LiteralPath $stagedBinary -Algorithm SHA256).Hash
if ($sourceHash -ne $stagedHash) {
    throw "staged artifact hash differs from source: $sourceHash vs $stagedHash"
}

Write-Output "version=$versionText"
Write-Output "diagnose=$diagnoseText"
Write-Output "sha256=$($stagedHash.ToLowerInvariant())"
Write-Output "isolated_directory=$stagingDir"
Write-Output 'runtime_path_test=passed'
Write-Output 'staged_hash_matches_source=true'
