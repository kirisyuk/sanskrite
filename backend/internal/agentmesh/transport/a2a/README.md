# A2A Transport

This package exposes the AgentMesh A2A-facing HTTP surface.

Files:

- [`http.go`](./http.go): agent card endpoint, task submission, task fetch, result fetch, approval, rejection, cancellation

## Exposed Endpoints

- `GET /.well-known/agent-card.json`
- `GET /a2a/card`
- `POST /a2a/tasks`
- `GET /a2a/tasks/{id}`
- `GET /a2a/tasks/{id}/result`
- `POST /a2a/tasks/{id}:approve`
- `POST /a2a/tasks/{id}:reject`
- `POST /a2a/tasks/{id}:cancel`

## Security

- signed agent card required for submit and read operations
- incoming card is validated against the registry signer
- approval and cancellation are explicit actions, not implicit state changes

## Request Shape

Task submission includes:

- `tenantId`
- `capability`
- `requestedAgentId`
- `dedupKey`
- `approvalRequired`
- `input`

## Important Limitation

This is an A2A-oriented gateway and version-pinned to `1.0.0`, but it is not yet a full spec-complete implementation of every task, event, push-notification, or streaming feature in the broader A2A spec.
