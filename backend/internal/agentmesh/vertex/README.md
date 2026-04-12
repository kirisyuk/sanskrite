# Vertex

This package integrates AgentMesh with Vertex AI.

Files:

- [`service.go`](./service.go): Reasoning Engine validation, Session creation, Session event append, model execution
- [`orchestrator.go`](./orchestrator.go): hosted Scout Agent Engine invocation
- [`memory.go`](./memory.go): Memory Bank readiness, retrieval, generation, and poisoning guards
- [`schemas.go`](./schemas.go): structured output schemas by Scout capability

## Responsibilities

- validate the configured Reasoning Engine at startup
- validate the configured Scout Agent Engine for hosted orchestration
- create Vertex Sessions under the configured Reasoning Engine
- append task events into the Session timeline
- fetch sanitized Memory Bank context and trigger Memory Bank generation from a Session
- call the hosted Scout orchestrator and preserve the AgentMesh output envelope
- validate that the hosted Scout returns a strict `scout_envelope` contract
- retry once with upward repair metadata when the hosted envelope is invalid
- execute task prompts via the existing Vertex AI model service
- attach Vertex structured-output config for Scout JSON capabilities

## Required Configuration

- `AGENTMESH_VERTEX_REASONING_ENGINE`
- `AGENTMESH_SCOUT_AGENT_ENGINE` unless `AGENTMESH_SCOUT_ORCHESTRATOR_MODE=local`
- `AGENTMESH_VERTEX_LOCATION`
- `AGENTMESH_MODEL_FLASH`
- `AGENTMESH_MODEL_FLASH_REASONING`
- `AGENTMESH_MODEL_PRO`

Optional:

- `VERTEX_SEARCH_DATA_STORE_ID`
- `AGENTMESH_VERTEX_SESSION_TTL`

Capability routing defaults:

- `scout.plan`, `scout.research`, `scout.bridge`, `scout.critic` -> `AGENTMESH_MODEL_FLASH_REASONING` (default `vertex-gemini-3-flash-preview`)
- simple non-review tasks -> `AGENTMESH_MODEL_FLASH` (default `vertex-gemini-3.1-flash-lite-preview`)
- review tasks -> `AGENTMESH_MODEL_PRO` (default `vertex-gemini-3.1-pro-preview`)

Fallback is provider-locked and upward only:

- default tasks: `flash-lite -> flash-reasoning -> pro`
- Scout reasoning/research/bridge/critic: `flash-reasoning -> pro`
- review: `pro`

All `AGENTMESH_MODEL_*` aliases must be `vertex-` prefixed and registered as
Vertex AI models.

## Why This Layer Exists

AgentMesh should not know raw Vertex REST details everywhere in the codebase. This package centralizes:

- resource naming
- long-running operation polling
- session event structure
- memory readiness, retrieval, and generation requests
- model alias mapping
- hosted Scout orchestration

## Interaction Pattern

- Sessions and memories use the Reasoning Engine REST surface
- root Scout orchestration uses the configured Scout Agent Engine resource by default
- model generation uses the existing internal Vertex AI service abstraction
- hosted Scout output is accepted only when `scout_envelope` satisfies the strict typed contract; legacy brace-salvage is no longer a success path

This keeps AgentMesh aligned with Vertex as the substrate while minimizing duplicated model runtime logic.
