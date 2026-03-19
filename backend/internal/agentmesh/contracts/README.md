# Contracts

This package defines the stable type system for AgentMesh.

Files:

- [`agent.go`](./agent.go): agent identity, capabilities, invocation policy, trust zones, agent card signature envelope
- [`task.go`](./task.go): task state machine, budgets, leases, subtasks, usage counters, approval metadata
- [`artifact.go`](./artifact.go): persisted output references, evidence bundles, checkpoint references
- [`trace.go`](./trace.go): spans, lineage edges, cost attribution

## Design Rules

- contracts are storage-safe: they are used both over the wire and in Firestore
- contracts are workflow-safe: task status and lease fields are sufficient for durable execution
- contracts are mesh-safe: they model agents, callers, tools, model policies, and approval gates

## Key Types

- `AgentSpec`: canonical description of a mesh agent
- `CapabilitySpec`: routable capability declaration
- `InvocationPolicy`: allowed callers, data zones, tools, models, and signature requirements
- `MeshTask`: end-to-end unit of work
- `Lease`: transactional claim state for distributed execution
- `BudgetPolicy`: per-task execution limits
- `UsageCounters`: model/tool usage and estimated spend
- `ArtifactRef`: immutable artifact metadata
- `TraceSpan`: mesh lineage event

## Status Model

`MeshTask.Status` supports:

- `pending`
- `running`
- `awaiting_review`
- `succeeded`
- `failed`
- `cancelled`
- `degraded`

## Storage Notes

- contract tags are aligned for JSON and Firestore
- `Lease.DedupKey` is used for idempotent task creation
- `ReviewAccepted`, `ApprovalActor`, and `ApprovalRationale` capture human review resolution

## Consumers

- [`../controlplane`](../controlplane/README.md)
- [`../runtime`](../runtime/README.md)
- [`../repo`](../repo/README.md)
- [`../transport`](../transport/README.md)
