[CmdletBinding()]
param(
    [switch]$SkipProbeTests
)

# NOTE: keep this file ASCII-only. Windows PowerShell 5.1 reads .ps1 files as
# ANSI unless they carry a BOM, so non-ASCII text would be mis-decoded.

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$goCache = Join-Path $repoRoot '.task-cache\go-build'
$goTemp = Join-Path $repoRoot '.task-cache\go-tmp'
New-Item -ItemType Directory -Force -Path $goCache, $goTemp | Out-Null

$env:GOCACHE = $goCache
$env:GOTMPDIR = $goTemp
$env:CGO_ENABLED = '0'

Push-Location $repoRoot
try {
    go test ./...
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    $engineImports = @(go list -f '{{range .Imports}}{{println .}}{{end}}' ./internal/engine)
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    $forbiddenEngineImports = @(
        'net', 'net/http', 'os', 'os/exec', 'syscall', 'time',
        'github.com/lshfx/seeking_the_way_to_immortality/internal/cli',
        'github.com/lshfx/seeking_the_way_to_immortality/internal/storage',
        'github.com/lshfx/seeking_the_way_to_immortality/internal/tui'
    )
    $violations = @($engineImports | Where-Object { $forbiddenEngineImports -contains $_ })
    if ($violations.Count -gt 0) {
        throw "engine imported forbidden boundary package(s): $($violations -join ', ')"
    }

    $modules = @(go list -m all)
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    if ($modules.Count -ne 1) {
        throw "TASK-03 expects zero third-party modules; found: $($modules -join ', ')"
    }
}
finally {
    Pop-Location
}

# TASK-02 probe suite. It is pure standard-library Python and needs no packages.
# Skip with -SkipProbeTests when only the Go side matters.
if (-not $SkipProbeTests) {
    $probeDir = Join-Path $repoRoot 'tools\task02-terminal-probe'
    $python = Get-Command python -ErrorAction SilentlyContinue
    if ($null -eq $python) {
        Write-Warning "python not found; skipping TASK-02 probe tests"
    }
    else {
        Push-Location $probeDir
        try {
            & $python.Source -m unittest discover -p 'test_*.py' -v
            if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        }
        finally {
            Pop-Location
        }
    }
}

Write-Output 'test suite passed'
