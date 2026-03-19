# Policy

The policy package enforces mesh-level governance rules.

Files:

- [`service.go`](./service.go): caller, data-zone, tool, and model checks
- [`service_test.go`](./service_test.go): policy validation tests

## What Is Enforced

- which callers may invoke an agent
- which data zones an agent may operate in
- which model aliases an agent may use
- which MCP tools an agent may call

## Current Inputs

`RequestContext` contains:

- `CallerAgentID`
- `DataZone`
- `ModelAlias`
- `ToolName`

## Trust Model

Trust zones come from contracts:

- `internal`
- `gcp`
- `external`

These are not IAM roles by themselves. They are AgentMesh policy dimensions used for routing and invocation decisions.

## Relationship To Vertex Governance

Vertex handles model- and platform-level governance. AgentMesh policy adds mesh-specific controls:

- who may call whom
- which tool surfaces are exposed
- which model aliases are valid for an agent class
- when approval is required
