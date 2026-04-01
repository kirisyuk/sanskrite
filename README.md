# Sanskrite Discovery Platform

A modern Discovery platform for source analysis and profiling using AI.

## Architecture

### Backend (Go)
- **Firebase Auth** - User authentication with corporate/personal tenant support
- **Firestore** - Data persistence (users, projects, datasources)
- **Vertex AI** - Source profiling with structured JSON output
- **Web Crawler** - HTML content fetching and text extraction
- **Telegram Integration** - Python service for Telegram channel polling

### Frontend (React + TypeScript)
- **Firebase Auth** - Google Sign-In
- **Zustand** - State management
- **Tailwind CSS** - Styling
- **Vite** - Build tool

### Python Services
- **Telegram Service** - Fetches messages from Telegram channels via Telethon

## Project Structure

```
.
├── backend/
│   ├── cmd/server/              # Main API server
│   ├── cmd/agentmesh/           # AgentMesh CLI / jobs
│   ├── cmd/tools/               # One-off smoke tools (incl. Horizon)
│   ├── internal/
│   │   ├── ai/                  # AI task executor (Vertex, OpenAI, Claude, …)
│   │   ├── http/                # HTTP handlers (core + horizon/, criticalchain/)
│   │   ├── horizon/             # Horizon: fragments, lens, scout, propagation, …
│   │   ├── criticalchain/       # Critical chain scheduling / pipeline
│   │   ├── agentmesh/           # Agent mesh runtime, policy, transport
│   │   ├── realtime/            # Pub/Sub and realtime bridge
│   │   ├── repo/                # Firestore repositories
│   │   ├── models/              # Data models
│   │   ├── discovery/           # Source discovery & orchestration
│   │   ├── crawler/             # Web fetch / extract
│   │   ├── telegram/            # Telegram client
│   │   └── …                    # genai, ontology, taxonomy, cache, …
│   └── go.mod
├── web/
│   ├── src/
│   │   ├── components/          # UI components (incl. Lens / Horizon canvas)
│   │   ├── contexts/            # Auth and shared context
│   │   ├── lib/                 # API client, Firebase
│   │   ├── pages/               # Routes (Discovery, Horizon, …)
│   │   ├── stores/              # Zustand stores
│   │   └── types/               # TypeScript types
│   └── package.json
├── docs/                        # Architecture, Horizon, deployment, notes
├── python/
│   └── telegram_service/        # Telegram polling service
└── scripts/
    ├── deploy                   # Main deployment script
    └── dev.ps1                  # Local development (Windows)
```

Документация: [docs/README.md](docs/README.md). Подсистема Horizon: [docs/horizon/README.md](docs/horizon/README.md).

## Getting Started

### Prerequisites

- **Go 1.25+** (см. `backend/go.mod`)
- **Node.js 18+**
- **Python 3.11+**
- **GCP Project** with Firestore, Vertex AI, Cloud Run enabled
- **Firebase Project** for authentication

### Configuration

#### 1. Backend Configuration

Create `.env` or set environment variables:

```bash
# GCP
GCP_PROJECT_ID=your-gcp-project-id
GOOGLE_CLOUD_PROJECT=your-gcp-project-id
FIREBASE_AUTH_PROJECT_ID=your-gcp-project-id
# Prefer ADC (gcloud auth application-default login)
GOOGLE_APPLICATION_CREDENTIALS=$HOME/.config/gcloud/application_default_credentials.json

# Server
PORT=8080
FRONTEND_ORIGIN=http://localhost:5173

# Telegram Service (for Python service)
TELEGRAM_SERVICE_URL=http://localhost:8081
```

#### 2. Frontend Configuration

Copy `web/.env.example` to `web/.env` and fill in:

```bash
VITE_FIREBASE_API_KEY=your_api_key
VITE_FIREBASE_AUTH_DOMAIN=your_project.firebaseapp.com
VITE_FIREBASE_PROJECT_ID=your_project_id
VITE_FIREBASE_STORAGE_BUCKET=your_project.appspot.com
VITE_FIREBASE_MESSAGING_SENDER_ID=your_sender_id
VITE_FIREBASE_APP_ID=your_app_id

VITE_API_BASE_URL=http://localhost:8080
```

#### 3. Telegram Service Configuration

```bash
TELEGRAM_API_ID=your_telegram_api_id
TELEGRAM_API_HASH=your_telegram_api_hash
TELEGRAM_SESSION_STRING=your_session_string
PORT=8081
```

### Local Development

#### 1. Start Backend

```bash
cd backend
go run ./cmd/server
```

Backend will run on `http://localhost:8080`

#### 2. Start Frontend

```bash
cd web
npm install
npm run dev
```

Frontend will run on `http://localhost:5173`

#### 3. Start Telegram Service (optional)

```bash
cd python/telegram_service
pip install -r requirements.txt
python app.py
```

Service will run on `http://localhost:8081`

## API Endpoints

**Полный каталог** маршрутов платформы (`/api/v1`), health, WebSocket, внутренних задач, Critical Chain и ссылка на Horizon: [docs/architecture/apis.md](docs/architecture/apis.md). Ниже — только самые частые точки входа.

### Authentication

Большинство пользовательских маршрутов требуют `Authorization: Bearer <firebase-id-token>`. Исключения (публичные вебхуки, демо Critical Chain, часть `/api/v1/waitlist`, прокси фидов Horizon и т.д.) перечислены в каталоге API.

### Health

- `GET /healthz` или `GET /api/v1/healthz`

### Users
- `POST /api/v1/users` - Create user
- `GET /api/v1/users/{id}` - Get user
- `PUT /api/v1/users/{id}` - Update user

### Projects
- `POST /api/v1/projects` - Create project
- `GET /api/v1/projects` - List projects (supports `?view=discovery`)
- `GET /api/v1/projects/{id}` - Get project
- `PUT /api/v1/projects/{id}` - Update project
- `DELETE /api/v1/projects/{id}` - Delete project

У проекта большое число вложенных путей (документы, поиск, vertex-search, analytics, agent-jobs, сценарии и т.д.) — см. [apis.md](docs/architecture/apis.md).

### Discovery
- `POST /api/v1/sources-discovery/execute` - Execute discovery analysis
- `GET /api/v1/projects/{id}/datasources` - Get sources for project

### Horizon

Базовый префикс: `/api/horizon`. Реестр маршрутов и подпутей линзы/фрагмента: [docs/horizon/api/endpoints.md](docs/horizon/api/endpoints.md).

### Critical Chain

Демо-данные и админ pipeline: префикс `/api/v1/criticalchain` — см. [apis.md](docs/architecture/apis.md).

## Deployment

**📖 Full documentation**: [docs/deployment/deployment-checklist.md](docs/deployment/deployment-checklist.md)

**🚀 Quick Start**: See [DEPLOY_QUICK_START.md](DEPLOY_QUICK_START.md)

### Automated Deployment Script

The easiest way to deploy all services:

```bash
# First time: setup infrastructure + deploy all
wsl bash scripts/deploy

# Regular deploys
bash scripts/deploy              # Deploy all services
bash scripts/deploy --api        # Deploy only backend
bash scripts/deploy --web        # Deploy only frontend
bash scripts/deploy --py         # Deploy only Telegram service
```

The script automatically:
- ✅ Creates service accounts and grants IAM roles
- ✅ Creates GCP secrets (with placeholders if needed)
- ✅ Creates Pub/Sub topics
- ✅ Validates Go build before deployment
- ✅ Routes 100% traffic to latest revision
- ✅ Creates Firestore composite indexes
- ✅ Provides deployment summary and checklist

### Post-Deployment

After first deploy, update secrets with real API keys:

```bash
echo "YOUR_KEY" | gcloud secrets versions add vertex_api_key --data-file=-
echo "YOUR_KEY" | gcloud secrets versions add openai_api_key --data-file=-
echo "YOUR_KEY" | gcloud secrets versions add anthropic_api_key --data-file=-
echo "YOUR_KEY" | gcloud secrets versions add grok_api_key --data-file=-
echo "YOUR_AISSTREAM_KEY" | gcloud secrets versions add aisstream_api_key --data-file=-   # optional: vessel map layer
```

### Verification

```bash
# Check health
API_URL=$(gcloud run services describe nv-horizon-api --region us-central1 --format='value(status.url)')
curl "$API_URL/healthz"

# View logs
gcloud run services logs read nv-horizon-api --region us-central1 --limit 20
```

## Features

### Implemented ✅

1. **Authentication & Authorization**
   - Firebase Google Sign-In
   - Corporate/Personal tenant support
   - User CRUD operations

2. **Project Management**
   - Create/Update/Delete projects
   - Discovery view filtering
   - Last interaction tracking

3. **Source Discovery**
   - Web URL crawling with HTML parsing
   - Telegram channel integration
   - Source canonicalization & deduplication
   - Primary/Related project relationships

4. **AI-Powered Source Profiling**
   - Editorial stance analysis
   - Trust score calculation
   - Behavioral patterns detection
   - Content pattern analysis
   - Red flags identification
   - Strict JSON schema validation

5. **Frontend UI**
   - Discovery page with chat-like interface
   - Source input (URLs + @telegram handles)
   - Real-time updates (WebSocket / store integration where wired)
   - Profile visualization

6. **Horizon**
   - Graph of `HorizonFragment`, lenses, Scout pipeline, dependency edges, Horizon UI (`HorizonPage`, lens canvas)
   - Детали и статус фич: [docs/horizon/README.md](docs/horizon/README.md), [roadmap](docs/horizon/implementation/roadmap.md)

### Pending / в развитии 🚧

1. **Наблюдаемость и SRE**
   - Расширенные SLO, алерты, единые дашборды

2. **CI/CD**
   - Полностью автоматизированный pipeline и политика окружений

3. **UI**
   - Дальнейшая полировка ошибок, загрузки, адаптива и тёмной темы

4. **Horizon**
   - Отдельные крупные ветки из roadmap (signaling pipeline, часть governance и метрик) — см. таблицу статуса в roadmap

## Data Models

### User
- Multi-tenant support (corporate/personal)
- Firebase UID as primary key
- TenantID computed from email

### Project
- Discovery "chats"
- LastInteractionAt for sorting
- View filter for organization

### DataSource
- Web and Telegram sources
- Canonical URL deduplication
- Primary + Related project references
- AI-generated profile data

### SourceProfile
- Editorial stance & confidence
- Trust score & rationale
- Behavioral & content patterns
- Topics, methods, goals
- Metrics & red flags

## Security

- Firebase Auth for authentication
- TenantID isolation in all queries
- CORS configuration
- No public data exposure

## Performance

- Firestore indexes for fast queries
- AI task executor with fallback models
- Caching for processed content
- Efficient HTML parsing

## Monitoring & Logging

- Structured logging throughout
- Error tracking
- API request/response logging

## License

Proprietary

## Support

For issues or questions, contact the development team.
