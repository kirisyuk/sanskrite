## Configuration

Authoritative configs live in code and environment. This document centralizes references.

### Backend
- Service account (local/dev): `backend/backend/gcp-service-account.json` (do not use in prod)
- Docker image: `backend/Dockerfile`
- Entrypoint: `backend/cmd/server/main.go`
- Environment variables: injected via deploy scripts/CI; document in deployment docs

### Frontend
- Firebase project config: `web/firebase.json`, `.firebaserc`
- Build and deploy: `web/cloudbuild.deploy.yaml`, `web/scripts/README.md`

### Scripts
- Deployment scripts: `scripts/deploy`, `scripts/deploy-gcp.ps1`, `scripts/dev.ps1`

### Environments
- Dev/Local: uses local credentials and emulators if configured
- Staging/Prod: credentials via CI/CD secret store (not committed)


