Param(
  [switch]$WebOnly,
  [switch]$ApiOnly,
  [switch]$Schemas
)

$ErrorActionPreference = 'Stop'

function Start-Proc($name, $cmd, $path) {
  Write-Host "Starting $name..." -ForegroundColor Green
  Start-Process -FilePath powershell -ArgumentList "-NoProfile -Command cd '$path'; $cmd" -WindowStyle Minimized
}

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$repo = Split-Path -Parent $root

if (-not $ApiOnly) {
  Start-Proc 'web' 'npm run dev' (Join-Path $repo 'web')
}

if (-not $WebOnly) {
  if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Warning 'Go is not installed or not in PATH; skipping backend.'
  } else {
    Start-Proc 'api' 'go run ./cmd/server' (Join-Path $repo 'backend')
  }
}

Write-Host 'Dev processes launched.' -ForegroundColor Cyan

if ($Schemas) {
  Write-Host 'Generating JSON Schemas...' -ForegroundColor Yellow
  Push-Location (Join-Path $repo 'backend/internal/tools/schemagen')
  go run .
  Pop-Location
}
