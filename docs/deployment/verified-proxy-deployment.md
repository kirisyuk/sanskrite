# Verified Proxy Deployment

## Overview

The Verified Proxy is a Cloud Run service that acts as a proxy between the frontend and the main backend API. It adds the `x-functions-key` header to requests, allowing the frontend to call the verified endpoint without exposing the system key.

## Architecture

```
Frontend → Verified Proxy (Cloud Run) → Backend API (Cloud Run)
           [adds x-functions-key]
```

## Automatic Deployment

**The verified-proxy is automatically deployed as part of the API deployment.**

When you run `scripts/deploy --api` or `scripts/deploy` (which includes API), the verified-proxy is deployed automatically after the main API is deployed.

### Deployment Flow

1. Main API (`api`) is deployed
2. Verified proxy (`verified-proxy`) is automatically deployed using the API URL
3. Proxy URL is displayed in the deployment summary

## Manual Setup (First Time)

### 1. Update Functions Key Secret

After first deployment, update the placeholder secret with a real value:

```bash
echo -n "YOUR_REAL_FUNCTIONS_KEY" | gcloud secrets versions add functions-key --data-file=-
```

The secret is automatically created with a placeholder during deployment if it doesn't exist.

### 2. Update Frontend Configuration

After deployment, the script will display the proxy URL. Update your frontend environment:

```bash
# In web/.env or web/.env.production
VITE_VERIFIED_PROXY_URL=https://verified-proxy-<PROJECT_ID>-uc.a.run.app
```

The proxy URL will be shown in the deployment summary after running `scripts/deploy --api`.

## Manual Deployment (Optional)

If you need to deploy the proxy separately:

```bash
cd backend

# Build and deploy using Cloud Build
gcloud builds submit \
  --config=cmd/verified-proxy/cloudbuild.yaml \
  --substitutions=_BACKEND_URL=$BACKEND_URL,_FUNCTIONS_KEY=functions-key:latest,_ALLOWED_ORIGIN=https://horizon.nordicvision.io \
  .
```

## Testing

### Test the proxy endpoint:

```bash
# Get proxy URL
PROXY_URL=$(gcloud run services describe verified-proxy --region us-central1 --format='value(status.url)')

# Test health check
curl $PROXY_URL/health

# Test verified endpoint
curl -X POST $PROXY_URL/verified \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com/article"}'
```

## Security Considerations

1. **Functions Key**: Stored in GCP Secret Manager (`functions-key` secret)
2. **CORS**: Configured via `ALLOWED_ORIGIN` environment variable
3. **Service Account**: Uses the same `backend@PROJECT_ID.iam.gserviceaccount.com` as main API
4. **Monitoring**: Check Cloud Run logs for errors

## Environment Variables

Set automatically during deployment:
- `BACKEND_URL`: Full URL of the backend API (from `api` service)
- `ALLOWED_ORIGIN`: CORS allowed origin (default: `https://horizon.nordicvision.io`)

From Secret Manager:
- `FUNCTIONS_KEY`: System authentication key (from `functions-key` secret)

## Troubleshooting

### Check logs:
```bash
gcloud run services logs read verified-proxy --region us-central1 --limit 50
```

### Common issues:

1. **502 Bad Gateway**: Backend API URL is incorrect or service is down
2. **403 Forbidden**: `functions-key` secret contains wrong value or placeholder
3. **CORS errors**: `ALLOWED_ORIGIN` doesn't match frontend URL

## Integration with Deployment Script

The verified-proxy is automatically:
- ✅ Built and validated before deployment
- ✅ Deployed after main API deployment
- ✅ Configured with correct backend URL
- ✅ Using the same service account as main API
- ✅ Using secrets from Secret Manager

No manual steps required when using `scripts/deploy --api`!

