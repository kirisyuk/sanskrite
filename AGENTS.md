# AGENTS.md

## Cursor Cloud specific instructions

### Architecture overview

This is a monorepo with three services:

| Service | Location | Language | Dev command | Port |
|---------|----------|----------|-------------|------|
| **Backend API** | `backend/` | Go 1.25.1 | `go run ./cmd/server` | 8080 |
| **Frontend SPA** | `web/` | TypeScript/React 19 + Vite 7 | `npm run dev` | 5173 |
| **Telegram Service** | `python/telegram_service/` | Python 3.11+ | `python app.py` | 8081 (optional) |

Go workspace file is at the repo root (`go.work`). The `backend/` directory is the only Go module.

### Running the backend locally

The backend requires a Firestore database. Without GCP credentials, use the **Firestore emulator**:

```bash
# Terminal 1 — Firestore emulator (requires Java, pre-installed)
java -jar ~/.cache/firebase/emulators/cloud-firestore-emulator-v1.20.2.jar \
  --host 127.0.0.1 --port 9099 &

# Terminal 2 — Go backend
cd backend
FIRESTORE_EMULATOR_HOST=127.0.0.1:9099 \
STORAGE_EMULATOR_HOST=http://127.0.0.1:9199 \
FIREBASE_AUTH_EMULATOR_HOST=127.0.0.1:9098 \
GCP_PROJECT_ID=demo-project \
FRONTEND_ORIGIN=http://localhost:5173 \
PORT=8080 \
go run ./cmd/server
```

The server starts with many `WARNING:` lines about missing optional services (PubSub, BigQuery, AI providers, Redis, etc.). These are expected in local dev without full GCP credentials.

Health check: `curl http://localhost:8080/healthz` should return `ok`.

### Running the frontend locally

```bash
cd web
npm install
npm run dev
```

The Vite dev server proxies `/api/*` and `/ws/*` requests to `http://localhost:8080` automatically (see `vite.config.ts`). The `web/.env` file must exist (copy from `.env.example` and set `VITE_API_BASE_URL=http://localhost:8080`).

### Lint / Type check / Build

- **Frontend lint**: `cd web && npm run lint` — ESLint (pre-existing warnings/errors in codebase)
- **Frontend types**: `cd web && npx tsc --noEmit` — clean
- **Frontend build**: `cd web && npm run build:ci` — uses `tsc -b && vite build`
- **Backend lint**: `cd backend && go vet ./...` — clean
- **Backend build**: `cd backend && go build ./cmd/server` — clean
- **No Go test files** exist in the codebase currently.

### Gotchas

- The Firestore emulator JAR is downloaded by `firebase-tools` to `~/.cache/firebase/emulators/`. If not present, run `npm install -g firebase-tools` and then start any emulator once so it downloads.
- The backend `main.go` (`backend/cmd/server/main.go`) is very large (~4000+ lines, single-file monolith).
- The backend uses `log.Fatalf` for Firestore and GCS init failures — the `STORAGE_EMULATOR_HOST` env var is needed to bypass GCS credential checks when running locally without GCP credentials.
- `FIREBASE_AUTH_EMULATOR_HOST` is needed for the Firebase Admin SDK to skip real auth verification.
- The `web/package.json` uses npm (lockfile is `package-lock.json`).
- Frontend `npm run build` requires the Go backend for schema generation (`gen:schemas`). Use `npm run build:ci` to skip that step.
