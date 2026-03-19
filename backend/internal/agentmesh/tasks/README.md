# Tasks

The tasks package integrates AgentMesh with Google Cloud Tasks.

Files:

- [`cloudtasks.go`](./cloudtasks.go): queue dispatcher and task payload model

## Responsibilities

- create Cloud Tasks HTTP invocations for AgentMesh execution
- attach OIDC credentials for the runtime service account
- keep queue interaction isolated from runtime logic

## Payload

Current task payload:

- `taskId`

The runtime resolves the rest from Firestore.

## Why Cloud Tasks

- retries with backoff
- authenticated invocation of Cloud Run
- decoupled submission and execution
- controlled concurrency and dispatch rate
