# Build the AgentRouter plugin as a CLIProxyAPI native plugin for the current
# platform. Requires Go 1.26+ and a C toolchain (MinGW-w64 on Windows).
#
# Usage:
#   ./build.ps1                # build for the host platform into dist/
#   ./build.ps1 -GoArch arm64  # build for another architecture (same OS)
param(
    [string]$GoArch = "",
    [string]$Output = ""
)

$ErrorActionPreference = "Stop"
Push-Location (Join-Path $PSScriptRoot "plugin")
try {
    if (-not $GoArch) { $GoArch = (& go env GOARCH).Trim() }
    $goos = (& go env GOOS).Trim()
    if (-not $Output) { $Output = Join-Path $PSScriptRoot "dist\agentrouter.dll" }
    New-Item -ItemType Directory -Force -Path (Split-Path $Output) | Out-Null

    $env:CGO_ENABLED = "1"
    & go test ./...
    & go build -buildmode=c-shared -trimpath -ldflags="-s -w" -o $Output .
    if ($LASTEXITCODE -ne 0) { throw "go build failed with exit code $LASTEXITCODE" }
    Write-Host "built $Output ($goos/$GoArch)"
}
finally {
    Pop-Location
}
