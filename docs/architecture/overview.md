## System Overview

Audience: AI agents. Source of truth is the repository; this document provides structure and entry points.

### Context Diagram
```mermaid
graph TD
  subgraph Client
    FE[Web Frontend (React/Vite)]
  end

  subgraph Backend
    GO[Go Server]
    HTTP[HTTP/REST + WS]
    RT[Realtime (Pub/Sub, WS)]
    AI[AI Services Abstraction]
    REPO[Repositories/Cache]
  end

  subgraph GCP/Firebase
    FS[(Firestore)]
    PS[(Pub/Sub)]
    SA[(Service Accounts)]
    Hosting[(Firebase Hosting)]
  end

  subgraph External AI
    OAI[OpenAI]
    CLAUDE[Anthropic]
    GROK[Grok]
    VTX[Vertex AI]
  end

  FE -->|REST/WS| HTTP
  HTTP --> GO
  GO --> REPO
  REPO --> FS
  GO --> RT
  RT <--> PS
  AI <--> OAI
  AI <--> CLAUDE
  AI <--> GROK
  AI <--> VTX
  GO --> AI
  FE -->|Static| Hosting
```

### Responsibilities
- Frontend: views, state, auth guard, realtime updates via WS.
- Backend: HTTP handlers, discovery/orchestration, AI calls, repo/cache, realtime.
- Data: Firestore as primary store; indexes and rules in repo.
- Realtime: Pub/Sub topics and WS bridge for client updates.
- Infra: GCP/Firebase deployment, service accounts, CI/CD.

### Key Links
- Backend packages: `backend/internal/*`
- Frontend app: `web/src/*`
- Infra/config: `docs/deployment/*`, `web/cloudbuild.deploy.yaml`, `scripts/*`, `backend/Dockerfile`

See also: `architecture/backend/overview.md`, `architecture/frontend/overview.md`, `architecture/infrastructure/overview.md`.


### Branding

- Internal project codename: `Sanskrite` (technical name used in code and repos).
- External branding: `HATL` — Human Above The Loop (`hatl.one`).
- UI uses HATL logo and copy; technical docs and directories may still reference `Sanskrite`.

