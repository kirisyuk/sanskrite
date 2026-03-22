## Deployment

This page summarizes and links to the authoritative deployment docs.

### Frontend
- Build/Deploy config: `web/cloudbuild.deploy.yaml`
- Firebase config: `web/firebase.json`, `web/.firebaserc`
- See `docs/deployment/deployment-checklist.md` and `docs/deployment/TROUBLESHOOTING.md`

### Backend
- Container image via `backend/Dockerfile`
- Entrypoint `backend/cmd/server/main.go`
- Refer to `docs/notes/DEPLOYMENT.md`, `docs/notes/DEPLOYMENT_STATUS.md`, and `docs/deployment/DEPLOYMENT_IMPROVEMENTS_SUMMARY.md`

### Scripts
- `scripts/deploy`, `scripts/deploy-gcp.ps1`, `scripts/dev.ps1`


