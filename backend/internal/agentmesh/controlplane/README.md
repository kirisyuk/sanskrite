# Control Plane

The control plane decides which agent should handle a task and whether the invocation is allowed.

Files:

- [`service.go`](./service.go): task preparation, routing, review assignment
- [`service_test.go`](./service_test.go): routing behavior tests

## Responsibilities

- validate task creation inputs
- route by `RequestedAgentID` or `Capability`
- fall back to `mesh.worker` when no exact capability match exists
- apply invocation policy before the task enters runtime
- mark whether approval is required

## Routing Rules

1. If `RequestedAgentID` is present, route there or fail.
2. Otherwise match agents by declared capability.
3. If no agent declares the capability, route to `mesh.worker`.

This makes the control plane resilient to gradual capability expansion while keeping the fallback execution path deterministic.

## Non-Responsibilities

The control plane does not:

- execute model calls
- persist artifacts
- claim leases
- manage Cloud Tasks

Those belong to [`../runtime`](../runtime/README.md) and [`../repo`](../repo/README.md).
