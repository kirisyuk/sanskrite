# Repo

The repo package persists AgentMesh state in Firestore and GCS.

Files:

- [`store.go`](./store.go): Firestore task, trace, policy, artifact, and agent persistence
- [`lease.go`](./lease.go): lease claim helpers and semantics
- [`lease_test.go`](./lease_test.go): lease behavior tests

## Firestore Collections

- `agentmesh_agents`
- `agentmesh_capabilities`
- `agentmesh_tasks`
- `agentmesh_artifacts`
- `agentmesh_traces`
- `agentmesh_policies`
- `agentmesh_runtime_locks`

## Artifact Storage

Artifacts are stored in GCS and referenced from Firestore metadata.

This package intentionally stores:

- task metadata
- artifact metadata
- trace spans
- approval status
- usage counters

It intentionally does **not** store:

- full Vertex session transcript history
- custom long-term memory copies
- custom model execution payload archives outside typed artifacts

## Lease Model

Leases are transactional and enforce:

- single-owner claim
- heartbeat-based lease extension
- reclaim on expiry
- attempt counter increments
- dedup key retention across retries

## Important Methods

- `CreateTask`
- `FindTaskByDedupKey`
- `ClaimTask`
- `HeartbeatLease`
- `UpdateTaskRuntimeState`
- `UpdateTaskUsage`
- `AwaitReview`
- `ResolveReview`
- `RetryTask`
- `CompleteTask`
- `FailTask`
- `CancelTask`

## Operational Guarantees

- task status changes are written through Firestore transactions
- artifacts are immutable after creation
- dedup-aware creation prevents duplicate submissions for the same logical unit of work
