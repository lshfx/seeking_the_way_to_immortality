[CmdletBinding()]
param(
    [string]$Version = '0.0.0-dev'
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$outputDir = Join-Path $repoRoot 'dist'
$goCache = Join-Path $repoRoot '.task-cache\go-build'
$goTemp = Join-Path $repoRoot '.task-cache\go-tmp'
$binary = Join-Path $outputDir 'wendao.exe'

New-Item -ItemType Directory -Force -Path $outputDir, $goCache, $goTemp | Out-Null
$env:GOCACHE = $goCache
$env:GOTMPDIR = $goTemp
$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'

Push-Location $repoRoot
try {
    go build -mod=readonly -trimpath -buildvcs=false `
        -ldflags "-s -w -X main.version=$Version" `
        -o $binary ./cmd/wendao
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}
finally {
    Pop-Location
}

$item = Get-Item -LiteralPath $binary
$hash = Get-FileHash -LiteralPath $binary -Algorithm SHA256
Write-Output "built=$($item.FullName)"
Write-Output "bytes=$($item.Length)"
Write-Output "sha256=$($hash.Hash.ToLowerInvariant())"
