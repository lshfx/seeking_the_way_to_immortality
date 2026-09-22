[CmdletBinding()]
param(
    [string]$Binary = (Join-Path (Split-Path -Parent $PSScriptRoot) 'dist\wendao.exe')
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$sourceBinary = (Resolve-Path -LiteralPath $Binary).Path
$stagingDir = Join-Path $repoRoot '.task-cache\release-check'
$stagedBinary = Join-Path $stagingDir 'wendao.exe'
$originalPath = $env:Path

New-Item -ItemType Directory -Force -Path $stagingDir | Out-Null
Copy-Item -LiteralPath $sourceBinary -Destination $stagedBinary -Force

try {
    # Keep only Windows system commands on PATH. The executable must not find
    # Go, Python, Node, package managers, or compilers at runtime.
    $env:Path = Join-Path $env:SystemRoot 'System32'

    Push-Location $stagingDir
    try {
        $versionOutput = & $stagedBinary --version
        if ($LASTEXITCODE -ne 0) { throw "--version failed with exit code $LASTEXITCODE" }
        $diagnoseOutput = & $stagedBinary --diagnose
        if ($LASTEXITCODE -ne 0) { throw "--diagnose failed with exit code $LASTEXITCODE" }
    }
    finally {
        Pop-Location
    }

    if ($diagnoseOutput -notcontains 'network=disabled') {
        throw 'diagnostic output did not confirm offline mode'
    }
    if ($diagnoseOutput -notcontains 'game_state=not_implemented') {
        throw 'TASK-03 artifact incorrectly claims game implementation'
    }

    Write-Output $versionOutput
    Write-Output $diagnoseOutput
    Write-Output "isolated_directory=$stagingDir"
    Write-Output 'runtime_path_test=passed'
}
finally {
    $env:Path = $originalPath
}
