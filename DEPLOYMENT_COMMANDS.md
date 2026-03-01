# Deployment Commands

## ✅ Build Status
- **Backend**: Compiled successfully → `backend/bin/server.exe`
- **Frontend**: Built successfully → `web/dist/`

## Deploy to GCP

### 1. Deploy Backend (Cloud Run)

```bash
# From project root
cd backend

# Deploy to Cloud Run (using existing Dockerfile)
gcloud run deploy etigram-api \
  --source . \
  --region us-central1 \
  --platform managed \
  --allow-unauthenticated \
  --set-env-vars="PROJECT_ID=sanskrite"

# Note: This will rebuild the container image and deploy
```

### 2. Deploy Frontend (Firebase Hosting)

```bash
# From project root
cd web

# Deploy to Firebase Hosting
firebase deploy --only hosting

# Or if you need to deploy with specific project
firebase use sanskrite
firebase deploy --only hosting
```

## Quick Deploy Script (PowerShell)

Save as `deploy-all.ps1`:

```powershell
# Build and deploy everything

Write-Host "=== Building Backend ===" -ForegroundColor Green
Set-Location backend
go build -o bin/server.exe ./cmd/server
if ($LASTEXITCODE -ne 0) {
    Write-Host "Backend build failed!" -ForegroundColor Red
    exit 1
}

Write-Host "=== Deploying Backend to Cloud Run ===" -ForegroundColor Green
gcloud run deploy etigram-api `
  --source . `
  --region us-central1 `
  --platform managed `
  --allow-unauthenticated `
  --set-env-vars="PROJECT_ID=sanskrite"

if ($LASTEXITCODE -ne 0) {
    Write-Host "Backend deployment failed!" -ForegroundColor Red
    exit 1
}

Set-Location ..

Write-Host "=== Building Frontend ===" -ForegroundColor Green
Set-Location web
npm run build
if ($LASTEXITCODE -ne 0) {
    Write-Host "Frontend build failed!" -ForegroundColor Red
    exit 1
}

Write-Host "=== Deploying Frontend to Firebase ===" -ForegroundColor Green
firebase deploy --only hosting
if ($LASTEXITCODE -ne 0) {
    Write-Host "Frontend deployment failed!" -ForegroundColor Red
    exit 1
}

Set-Location ..

Write-Host "=== Deployment Complete! ===" -ForegroundColor Green
```

## Testing After Deployment

### 1. Check Backend Logs
```bash
gcloud logs read --project=etigram --service=etigram-api --limit=100
```

### 2. Test Discovery Flow
1. Open app: https://sanskrite.web.app (or your Firebase domain)
2. Sign in
3. Start new discovery with: `@riverpark_life`
4. Watch for:
   - Project appears in sidebar immediately
   - Source shows "fetching" status
   - Profile data loads
   - No "loaded 0 sources" error in logs

### 3. Monitor Backend Logs for:
```
[FindByCanonicalSource] Found match by ChannelUsername: canonical=riverpark_life
orchestrator: found existing telegram source @riverpark_life
orchestrator: loaded N sources for project XXX
```

### 4. Monitor Frontend Console for:
```
[WS] Message received: {type: 'discovery.project', ...}
[WS] Adding new project to store
[WS] Message received: {type: 'discovery.profile', ...}
```

## Rollback (if needed)

### Backend
```bash
# List revisions
gcloud run revisions list --service=etigram-api --region=us-central1

# Rollback to previous revision
gcloud run services update-traffic etigram-api \
  --region=us-central1 \
  --to-revisions=etigram-api-00039-xxx=100
```

### Frontend
```bash
# Firebase Hosting versions are kept automatically
# Rollback via Firebase Console: Hosting → Manage → Rollback
```

## Environment Variables

Make sure these are set in Cloud Run:
```
PROJECT_ID=sanskrite
FIRESTORE_EMULATOR_HOST= (not set for production)
```

Check with:
```bash
gcloud run services describe etigram-api --region=us-central1 --format='value(spec.template.spec.containers[0].env)'
```

## Monitoring

### Check Service Health
```bash
# Backend
curl https://etigram-api-<hash>-uc.a.run.app/health

# Check WebSocket
wscat -c wss://etigram-api-<hash>-uc.a.run.app/ws?token=YOUR_TOKEN
```

### View Real-time Logs
```bash
gcloud logs tail --project=etigram --resource-type=cloud_run_revision
```


