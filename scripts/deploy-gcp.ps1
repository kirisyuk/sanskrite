Param(
  [Parameter(Mandatory=$true)][string]$ProjectId,
  [Parameter(Mandatory=$true)][string]$Region,
  [switch]$BuildOnly
)

$ErrorActionPreference = 'Stop'

function Exec($cmd) {
  Write-Host $cmd -ForegroundColor Yellow
  iex $cmd
}

$repo = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)

# Build and push Go API
Exec "gcloud builds submit `"$repo/backend`" --project $ProjectId --tag gcr.io/$ProjectId/sanskrite-api:latest"
if (-not $BuildOnly) {
  Exec "gcloud run deploy sanskrite-api --image gcr.io/$ProjectId/sanskrite-api:latest --project $ProjectId --region $Region --allow-unauthenticated --port 8080"
}

# Build and push Telegram service
Exec "gcloud builds submit `"$repo/python/telegram_service`" --project $ProjectId --tag gcr.io/$ProjectId/telegram-service:latest"
if (-not $BuildOnly) {
  $secretEnv = @(
    'TELEGRAM_API_ID=TELEGRAM_API_ID:latest',
    'TELEGRAM_API_HASH=TELEGRAM_API_HASH:latest',
    'TELEGRAM_SESSION_STRING=TELEGRAM_SESSION_STRING:latest',
    'TELEGRAM_BOT_TOKEN=TELEGRAM_BOT_TOKEN:latest'
  ) -join ','
  Exec "gcloud run deploy telegram-service --image gcr.io/$ProjectId/telegram-service:latest --project $ProjectId --region $Region --no-allow-unauthenticated --port 8080 --set-secrets $secretEnv"
}

Write-Host "Done." -ForegroundColor Green


