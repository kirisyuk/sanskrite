# Registry

The registry package owns agent definitions and agent-card signing and verification.

Files:

- [`service.go`](./service.go): registry service, bootstrap, listing, validation, encoded card parsing
- [`kms_signer.go`](./kms_signer.go): production signer backed by Cloud KMS asymmetric signing
- [`hmac_signer.go`](./hmac_signer.go): test and local signer
- [`service_test.go`](./service_test.go): registry signing tests

## Responsibilities

- bootstrap built-in mesh agents
- sign agent cards
- verify signed agent cards on inbound requests
- expose canonical `AgentSpec` records to the control plane

## Built-In Agents

- `mesh.supervisor`
- `mesh.worker`
- `mesh.reviewer`
- `mesh.scout-orchestrator`

Each built-in agent defines:

- stable ID
- trust zone
- capabilities
- invocation policy
- A2A version and endpoint
- signed invocation policy, including tool allowlists for MCP callers

## Signing Model

Production:

- KMS asymmetric signing
- key version injected from deploy
- signature algorithm recorded in `AgentCardSignature.Alg`

Tests and local fallback:

- HMAC signer

## Why KMS

- removes shared-secret signing from runtime configuration
- allows key rotation using KMS primary version changes
- keeps verification bound to the deployed public key material

## Consumers

- [`../controlplane`](../controlplane/README.md)
- [`../transport/a2a`](../transport/a2a/README.md)
- [`../transport/mcp`](../transport/mcp/README.md)
- [`../../cmd/agentmesh`](../../cmd/agentmesh/README.md)
