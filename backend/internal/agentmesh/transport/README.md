# Transport

The transport package contains external protocol surfaces exposed by AgentMesh.

Directories:

- [`a2a`](./a2a/README.md): agent-to-agent HTTP gateway
- [`mcp`](./mcp/README.md): remote HTTP MCP gateway

## Design Goals

- keep protocol handling separate from control-plane logic
- enforce signed agent cards at the edge
- let transport adapt to future protocol evolution without rewriting runtime logic

## Current Position

The v1 implementation is an internal production gateway, not a full public protocol server. It provides the surfaces AgentMesh needs now while keeping the protocol boundary clean for future expansion.
