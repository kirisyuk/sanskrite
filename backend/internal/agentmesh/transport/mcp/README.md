# MCP Transport

This package exposes the AgentMesh MCP-facing HTTP gateway.

Files:

- [`http.go`](./http.go): JSON-RPC endpoint, tool registration, signed caller validation

## Supported Methods

- `initialize`
- `tools/list`
- `tools/call`

## Registered Tools

- `agentmesh.task.get`
- `vertex.generate`
- `agentmesh.scout.search_knowledge`
- `agentmesh.scout.resolve_product_canonical`
- `agentmesh.scout.lookup_product_transform`
- `agentmesh.scout.path_preview`
- `agentmesh.scout.materialize_path`
- `agentmesh.scout.critic`
- `agentmesh.scout.aggregate`
- `agentmesh.scout.review`

## Governance Model

`vertex.generate` and the task-backed Scout tools do **not** call models directly anymore. They create and execute AgentMesh tasks so that:

- policy still applies
- budgets still apply
- artifacts are still produced
- review still runs
- approval can still be required
- traces and usage still persist

The deterministic Scout tools stay inside the canonical control plane:

- search stays bounded and provenance-aware
- product resolution uses the canonical synonym dictionary
- product-pair lookup uses the canonical transform dictionary
- path preview/materialization stay on governed Horizon adapters

## Security

- signed agent card required
- tool access filtered by agent policy
- hosted Scout uses the dedicated `mesh.scout-orchestrator` card with a narrow allowlist

## Important Limitation

This is a narrow remote HTTP MCP server for internal production use. It does not yet implement the full set of MCP lifecycle or streaming features.
