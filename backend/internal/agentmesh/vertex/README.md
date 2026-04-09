# Vertex

This package integrates AgentMesh with Vertex AI.

Files:

- [`service.go`](./service.go): Reasoning Engine validation, Session creation, Session event append, Memory generation, model execution

## Responsibilities

- validate the configured Reasoning Engine at startup
- create Vertex Sessions under the configured Reasoning Engine
- append task events into the Session timeline
- trigger Memory Bank generation from a Session
- execute task prompts via the existing Vertex AI model service

## Required Configuration

- `AGENTMESH_VERTEX_REASONING_ENGINE`
- `AGENTMESH_VERTEX_LOCATION`
- `AGENTMESH_MODEL_FLASH`
- `AGENTMESH_MODEL_FLASH_REASONING`
- `AGENTMESH_MODEL_PRO`

Optional:

- `VERTEX_SEARCH_DATA_STORE_ID`
- `AGENTMESH_VERTEX_SESSION_TTL`

Capability routing defaults:

- `scout.plan`, `scout.bridge`, `scout.critic` -> `AGENTMESH_MODEL_FLASH_REASONING` (default `gemini-3-flash-preview`)
- simple non-review tasks -> `AGENTMESH_MODEL_FLASH` (default `gemini-3.1-flash-lite-preview`)
- review tasks -> `AGENTMESH_MODEL_PRO` (default `gemini-3.1-pro-preview`)

## Why This Layer Exists

AgentMesh should not know raw Vertex REST details everywhere in the codebase. This package centralizes:

- resource naming
- long-running operation polling
- session event structure
- memory generation requests
- model alias mapping

## Interaction Pattern

- Sessions and memories use the Reasoning Engine REST surface
- model generation uses the existing internal Vertex AI service abstraction

This keeps AgentMesh aligned with Vertex as the substrate while minimizing duplicated model runtime logic.
