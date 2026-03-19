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
- `AGENTMESH_AGENT_CARD_KMS_KEY_VERSION`

Operational env usually supplied by deploy:

- `AGENTMESH_PUBLIC_BASE_URL`
- `AGENTMESH_TASK_TARGET_URL`
- `AGENTMESH_TASKS_LOCATION`
- `AGENTMESH_TASKS_QUEUE`
- `AGENTMESH_TASKS_OIDC_SERVICE_ACCOUNT`
- `AGENTMESH_TASKS_OIDC_AUDIENCE`
- `AGENTMESH_RUNTIME_LEASE_OWNER`
- `AGENTMESH_ARTIFACT_BUCKET`
- `AGENTMESH_MODEL_FLASH`
- `AGENTMESH_MODEL_PRO`
- `VERTEX_SEARCH_DATA_STORE_ID`

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
- Cloud Run service
- readiness smoke check
