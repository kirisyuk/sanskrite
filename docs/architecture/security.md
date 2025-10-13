## Security

### Authentication and Authorization
- Backend auth entry: `backend/internal/http/auth.go`.
- Frontend auth gate: `web/src/components/AuthGuard.tsx`, `web/src/contexts/AuthContext.tsx`.
- Token verification and user context are handled on each request; prefer least-privilege.

### Secrets and Service Accounts
- GCP service account: `backend/backend/gcp-service-account.json` (local dev). Use secret managers in production.
- Do not commit live secrets; ensure CI/CD injects credentials via environment.

### Data Protection
- Firestore rules: `backend/firestore.rules`.
- Restrict read/write by user role/ownership where applicable.

### Transport
- Enforce HTTPS in production. WebSocket over WSS.


