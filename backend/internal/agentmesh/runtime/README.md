# Runtime

The runtime package is the execution core of AgentMesh.

Files:

- [`service.go`](./service.go): task creation, claim, execution, review, retry, approval, readiness

## Responsibilities

- create new tasks
- enforce dedup at submission time
- claim tasks with transactional leases
- run periodic lease heartbeats during execution
- ensure Vertex Session and Memory Bank binding
- execute worker and reviewer passes
- persist artifacts and usage
- transition tasks into `awaiting_review`, `succeeded`, `failed`, or `degraded`
- cancel tasks and resolve approval outcomes

## Execution Flow

1. `CreateTask`
2. `ClaimTask`
3. `EnsureSession`
4. `EnsureMemoryBank`
5. append input event to Vertex Session
6. execute worker model call
7. enforce budget
8. persist artifact
9. execute reviewer model call
10. enforce budget again
11. generate memories from session
12. either:
   - complete task
   - fail task
   - move to `awaiting_review`

## Review Behavior

- model review always runs
- if the routed task requires human approval, success is not final
- task moves to `awaiting_review`
- caller must explicitly approve or reject

## Failure Domains

- transient model or platform errors lead to retry up to the configured attempt limit
- permanent policy or validation failures are terminal
- reviewer rejection can produce `degraded` or `failed` depending on path

## Readiness

Runtime readiness is bound to:

- store present
- control plane present
- vertex service present
- live reachability of the configured Vertex Reasoning Engine
