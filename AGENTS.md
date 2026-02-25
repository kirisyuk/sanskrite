# Sanskrite Discovery Platform

## Cursor Cloud specific instructions

### Architecture overview

- **Go Backend** (`backend/`): REST API server on `:8080`. Uses Firebase Auth, Firestore, GCS, Pub/Sub.
- **React Frontend** (`web/`): Vite SPA on `:5173`. Uses Firebase Auth, Zustand, TanStack Query, Tailwind CSS.
- **Python Telegram Service** (`python/telegram_service/`): Optional Flask service on `:8081`.

### Running services locally

The backend requires Firestore and Firebase Auth. Use Firebase emulators to run locally without GCP credentials:

```bash
# Terminal 1: Start Firebase emulators (Auth on 9099, Firestore on 8181)
firebase emulators:start --only auth,firestore --project dev-local --config firebase-emulators.json

# Terminal 2: Start Go backend (with emulator env vars)
cd backend && \
  FIRESTORE_EMULATOR_HOST=localhost:8181 \
  FIREBASE_AUTH_EMULATOR_HOST=localhost:9099 \
  GCP_PROJECT_ID=dev-local \
  FRONTEND_ORIGIN=http://localhost:5173 \
  PORT=8080 \
  DISABLE_SUBSCRIPTION_BILLING=true \
  STORAGE_EMULATOR_HOST=localhost:9199 \
  go run ./cmd/server

# Terminal 3: Start frontend dev server
cd web && npm run dev
```

The Vite config (`web/vite.config.ts`) proxies `/api` and `/ws` to `localhost:8080`.

### Lint / Type-check / Build

| Check | Command | Notes |
|-------|---------|-------|
| Frontend lint | `cd web && npx eslint .` | Pre-existing warnings/errors in the codebase |
| Frontend type-check | `cd web && npx tsc -b` | Passes cleanly |
| Frontend build (CI) | `cd web && npm run build:ci` | Skips schema generation |
| Backend build | `cd backend && go build ./cmd/server` | |
| Backend vet | `cd backend && go vet ./...` | Passes cleanly |

### Key gotchas

- The `firebase-emulators.json` config in the repo root sets Firestore to port **8181** (not default 8080) to avoid conflict with the Go backend.
- The frontend `.env` file needs `VITE_FIREBASE_*` variables. Placeholder values allow the Vite dev server to start but real Firebase API keys are required for authentication to work.
- The Go backend startup initializes domain hierarchies and taxonomy from Firestore on boot. This takes ~5-10 seconds and writes initial data to the emulated Firestore.
- `npm run build` (production build) requires the Go schemagen tool (`go run ./internal/tools/schemagen`); use `npm run build:ci` to skip schema generation.
- The Go backend requires Go 1.25.1 (specified in `go.mod`).
