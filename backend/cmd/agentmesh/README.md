# AgentMesh Binary

This directory contains the deployable `AgentMesh` service entrypoint.

Files:

- [`main.go`](./main.go): Cloud Run service bootstrap
- [`Dockerfile`](./Dockerfile): service image build
- [`cloudbuild.yaml`](./cloudbuild.yaml): Cloud Build pipeline for the service image

## What `main.go` Wires Together

- Firestore client
- GCS client
- registry with KMS-backed agent-card signer
- policy service
- control plane
- Vertex substrate service
- trace service
- Cloud Tasks dispatcher
- runtime service
- A2A and MCP transports

## Exposed HTTP Surface

- `/healthz`
- `/readyz`
- `/.well-known/agent-card.json`
- `/a2a/*`
- `/mcp`
- `/api/v1/internal/tasks/agentmesh/run`

## Runtime Requirements

Required env:

- `GCP_PROJECT_ID`
- `AGENTMESH_VERTEX_REASONING_ENGINE`
- `AGENTMESH_SCOUT_AGENT_ENGINE` unless `AGENTMESH_SCOUT_ORCHESTRATOR_MODE=local`
- `AGENTMESH_AGENT_CARD_KMS_KEY_VERSION`

Operational env usually supplied by deploy:

- `AGENTMESH_PUBLIC_BASE_URL`
- `AGENTMESH_TASK_TARGET_URL`
- `AGENTMESH_TASKS_LOCATION`
- `AGENTMESH_TASKS_QUEUE`
- `AGENTMESH_TASKS_OIDC_SERVICE_ACCOUNT`
- `AGENTMESH_TASKS_OIDC_AUDIENCE`
- `AGENTMESH_TASKS_DISABLE_OIDC=0`
- `AGENTMESH_RUNTIME_LEASE_OWNER`
- `AGENTMESH_ARTIFACT_BUCKET`
- `AGENTMESH_SCOUT_ORCHESTRATOR_MODE=vertex`
- `AGENTMESH_ALLOW_UNAUTH_INTERNAL_TASKS=0`
- `AGENTMESH_MODEL_FLASH=vertex-gemini-3.1-flash-lite-preview`
- `AGENTMESH_MODEL_FLASH_REASONING=vertex-gemini-3-flash-preview`
- `AGENTMESH_MODEL_PRO=vertex-gemini-3.1-pro-preview`
- `AGENTMESH_OTEL_ENABLED=1`
- `AGENTMESH_EVAL_GCS_PREFIX`
- `VERTEX_SEARCH_DATA_STORE_ID`

`AGENTMESH_TASKS_DISABLE_OIDC=1` and
`AGENTMESH_ALLOW_UNAUTH_INTERNAL_TASKS=1` are accepted only with the matching
`AGENTMESH_DEV_*` override flags for explicit local/dev runs.

## Deployment

Deploy is owned by [`/Users/kirisyuk/cursor/sanskrite/scripts/deploy`](/Users/kirisyuk/cursor/sanskrite/scripts/deploy) via:

```bash
scripts/deploy --agentmesh
```

The deploy path ensures:

- service account
- IAM bindings
- KMS signer key
- Firestore indexes
- Cloud Tasks queue
- artifact bucket
- Vertex Reasoning Engine
- Vertex Scout Agent Engine resource configuration
- Cloud Run service
- authenticated internal task endpoint
- readiness smoke check
