# Deployment Guide

## Prerequisites

1. **GCP Project Setup**
   ```bash
   # Set your project
   gcloud config set project YOUR_PROJECT_ID
   
   # Enable required APIs
   gcloud services enable run.googleapis.com
   gcloud services enable firestore.googleapis.com
   gcloud services enable aiplatform.googleapis.com
   ```

2. **Firebase Project Setup**
   - Create Firebase project at https://console.firebase.google.com
   - Enable Google Authentication
   - Get Firebase config for web app

3. **Firestore Setup**
   ```bash
   # Create Firestore database (Native mode)
   gcloud firestore databases create --region=us-central1
   
   # Deploy indexes
   gcloud firestore indexes create --collection=core_projects \
     --field-config field-path=tenantId,order=ascending \
     --field-config field-path=view,order=ascending \
     --field-config field-path=lastInteractionAt,order=descending
   ```

## Step-by-Step Deployment

### 1. Deploy Backend to Cloud Run

```bash
cd backend

# Build and deploy
gcloud run deploy sanskrite-backend \
  --source . \
  --region us-central1 \
  --platform managed \
  --allow-unauthenticated \
  --memory 512Mi \
  --timeout 300 \
  --max-instances 10 \
  --set-env-vars "GCP_PROJECT_ID=YOUR_PROJECT_ID" \
  --set-env-vars "FRONTEND_ORIGIN=https://your-app.web.app"

# Get the service URL
BACKEND_URL=$(gcloud run services describe sanskrite-backend \
  --region us-central1 --format='value(status.url)')

echo "Backend URL: $BACKEND_URL"
```

### 2. Deploy Telegram Service to Cloud Run

```bash
cd python/telegram_service

# Create secret for Telegram credentials
echo -n "YOUR_API_ID" | gcloud secrets create telegram-api-id --data-file=-
echo -n "YOUR_API_HASH" | gcloud secrets create telegram-api-hash --data-file=-
echo -n "YOUR_SESSION_STRING" | gcloud secrets create telegram-session --data-file=-

# Deploy with secrets
gcloud run deploy nv-horizon-telegram \
  --source . \
  --region us-central1 \
  --platform managed \
  --allow-unauthenticated \
  --memory 256Mi \
  --timeout 120 \
  --max-instances 3 \
  --set-secrets "TELEGRAM_API_ID=telegram-api-id:latest" \
  --set-secrets "TELEGRAM_API_HASH=telegram-api-hash:latest" \
  --set-secrets "TELEGRAM_SESSION_STRING=telegram-session:latest"

# Get the service URL
TELEGRAM_URL=$(gcloud run services describe nv-horizon-telegram \
  --region us-central1 --format='value(status.url)')

echo "Telegram Service URL: $TELEGRAM_URL"
```

### 3. Update Backend with Telegram Service URL

```bash
gcloud run services update sanskrite-backend \
  --region us-central1 \
  --set-env-vars "TELEGRAM_SERVICE_URL=$TELEGRAM_URL"
```

### 4. Deploy Frontend to Firebase Hosting

```bash
cd web

# Update .env with production values
cat > .env << EOF
VITE_FIREBASE_API_KEY=your_api_key
VITE_FIREBASE_AUTH_DOMAIN=your_project.firebaseapp.com
VITE_FIREBASE_PROJECT_ID=your_project_id
VITE_FIREBASE_STORAGE_BUCKET=your_project.appspot.com
VITE_FIREBASE_MESSAGING_SENDER_ID=your_sender_id
VITE_FIREBASE_APP_ID=your_app_id
VITE_API_BASE_URL=$BACKEND_URL
EOF

# Build
npm install
npm run build

# Deploy
firebase deploy --only hosting

# Get hosting URL
firebase hosting:channel:list
```

## Environment Variables

### Backend (Cloud Run)
- `GCP_PROJECT_ID` - Your GCP project ID
- `FRONTEND_ORIGIN` - Frontend URL for CORS
- `TELEGRAM_SERVICE_URL` - Telegram service URL
- `GEONAMES_USERNAME` - GeoNames username (used for deterministic geo hierarchy normalization)
- `PORT` - Auto-set by Cloud Run (8080)

### Telegram Service (Cloud Run)
- `TELEGRAM_API_ID` - From https://my.telegram.org
- `TELEGRAM_API_HASH` - From https://my.telegram.org
- `TELEGRAM_SESSION_STRING` - Generated session string
- `PORT` - Auto-set by Cloud Run (8080)

### Frontend (Environment Variables)
- `VITE_FIREBASE_*` - Firebase configuration
- `VITE_API_BASE_URL` - Backend API URL

## Post-Deployment

### 1. Configure Firebase Authentication

```bash
# Add authorized domain for production
# Go to Firebase Console > Authentication > Settings > Authorized domains
# Add: your-app.web.app
```

### 2. Test the Deployment

```bash
# Test backend health
curl $BACKEND_URL/healthz

# Test telegram service health
curl $TELEGRAM_URL/healthz

# Open frontend
echo "Visit: https://your-app.web.app"
```

### 3. Monitor Logs

```bash
# Backend logs
gcloud run services logs read sanskrite-backend --region us-central1

# Telegram service logs
gcloud run services logs read nv-horizon-telegram --region us-central1
```

## Troubleshooting

### Backend Issues

```bash
# Check service status
gcloud run services describe sanskrite-backend --region us-central1

# View recent logs
gcloud run services logs read sanskrite-backend \
  --region us-central1 --limit 50

# Update environment variable
gcloud run services update sanskrite-backend \
  --region us-central1 \
  --set-env-vars "NEW_VAR=value"
```

### Frontend Issues

```bash
# Rebuild and redeploy
cd web
npm run build
firebase deploy --only hosting

# Check deployment status
firebase hosting:channel:list
```

### Firestore Issues

```bash
# Check indexes
gcloud firestore indexes list

# Create missing index
gcloud firestore indexes create --collection=COLLECTION \
  --field-config field-path=FIELD,order=DIRECTION
```

## Rollback

### Backend Rollback
```bash
# List revisions
gcloud run revisions list --service sanskrite-backend --region us-central1

# Rollback to previous revision
gcloud run services update-traffic sanskrite-backend \
  --region us-central1 \
  --to-revisions REVISION_NAME=100
```

### Frontend Rollback
```bash
# List releases
firebase hosting:channel:list

# Rollback
firebase hosting:rollback
```

## Scaling & Performance

### Backend Auto-scaling
```bash
gcloud run services update sanskrite-backend \
  --region us-central1 \
  --min-instances 1 \
  --max-instances 100 \
  --concurrency 80
```

### Cost Optimization
- Set `--min-instances 0` for dev/staging
- Use `--memory 256Mi` for lighter services
- Set appropriate `--timeout` values

## Security

### 1. Restrict Cloud Run Access
```bash
# Remove public access (if using IAP or internal only)
gcloud run services remove-iam-policy-binding sanskrite-backend \
  --region us-central1 \
  --member="allUsers" \
  --role="roles/run.invoker"
```

### 2. Configure CORS Properly
- Update `FRONTEND_ORIGIN` to match your domain
- Use specific origins, not wildcards

### 3. Enable Cloud Armor (optional)
```bash
# For DDoS protection
gcloud compute security-policies create sanskrite-policy \
  --description "Sanskrite security policy"
```

## Monitoring

### Set up Alerts
```bash
# CPU usage alert
gcloud alpha monitoring policies create \
  --notification-channels=CHANNEL_ID \
  --display-name="High CPU Usage" \
  --condition-display-name="CPU > 80%" \
  --condition-threshold-value=0.8
```

### View Metrics
- Cloud Run: https://console.cloud.google.com/run
- Firebase: https://console.firebase.google.com
- Firestore: https://console.cloud.google.com/firestore

## Maintenance

### Regular Updates
```bash
# Update backend
cd backend
git pull
gcloud run deploy sanskrite-backend --source .

# Update frontend
cd web
git pull
npm install
npm run build
firebase deploy --only hosting
```

### Database Maintenance
```bash
# Backup Firestore
gcloud firestore export gs://YOUR_BACKUP_BUCKET/$(date +%Y%m%d)

# Clean old data (implement in code)
# - Delete old sources
# - Archive old projects
```

## CI/CD Integration

Example GitHub Actions workflow:

```yaml
name: Deploy to Production

on:
  push:
    branches: [main]

jobs:
  deploy-backend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: google-github-actions/setup-gcloud@v0
      - run: |
          cd backend
          gcloud run deploy sanskrite-backend \
            --source . \
            --region us-central1

  deploy-frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-node@v3
      - run: |
          cd web
          npm ci
          npm run build
      - uses: FirebaseExtended/action-hosting-deploy@v0
```

## Support

For issues, check:
1. Cloud Run logs
2. Firebase console
3. Browser DevTools console
4. Network tab for API errors




