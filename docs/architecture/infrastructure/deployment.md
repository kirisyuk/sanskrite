## Deployment

SoT — исполняемые артефакты, а не md. Md-файлы устаревают быстрее, чем роботы успевают их прочитать.

- Primary entrypoint: `scripts/deploy` (bash, с флагами `--web/--api/--ws/--telegram/...`).
- Cloud Build: `backend/cloudbuild.*.yaml`, `web/cloudbuild.deploy.yaml`.
- Runtime config: `backend/Dockerfile`, `backend/cmd/*/main.go`.
- Firebase: `web/firebase.json`, `web/.firebaserc`.
- GCP project / resource naming conventions: `docs/deployment/FORK_MIGRATION_2026.md`.

Troubleshooting и чеклисты: `docs/deployment/deployment-checklist.md`, `docs/deployment/TROUBLESHOOTING.md`.
