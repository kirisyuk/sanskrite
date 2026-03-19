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

## Governance Model

`vertex.generate` does **not** call Vertex directly anymore. It creates and executes an AgentMesh task so that:

- policy still applies
- budgets still apply
- artifacts are still produced
- review still runs
- approval can still be required
- traces and usage still persist

## Security

- signed agent card required
- tool access filtered by agent policy

## Important Limitation

This is a narrow remote HTTP MCP server for internal production use. It does not yet implement the full set of MCP lifecycle or streaming features.
