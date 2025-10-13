## Infrastructure Overview

### Services
- Firestore: primary database
- Pub/Sub: event distribution
- Firebase Hosting: serve frontend
- Containerized backend: deployed to GCP (see deployment docs)

### Credentials
- Managed via service accounts and CI/CD secret store; avoid committing secrets.

### References
- Indexes: `backend/firestore.indexes.json`
- Rules: `backend/firestore.rules`
- Dockerfile: `backend/Dockerfile`
- Cloud Build deploy: `web/cloudbuild.deploy.yaml`
- Scripts: `scripts/deploy`, `scripts/deploy-gcp.ps1`


