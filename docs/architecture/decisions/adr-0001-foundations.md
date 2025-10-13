# ADR-0001: Architectural Foundations

Status: Accepted
Date: 2025-10-12

## Context
- Backend in Go for performance, concurrency, and static typing
- Frontend in React/Vite for rapid UI iteration and TS typing
- Data on Firestore (managed, scalable) with Pub/Sub for async events
- AI via pluggable services (OpenAI, Anthropic, Grok, Vertex) behind a common interface

## Decision
- Keep business logic in Go services with clear package boundaries under `backend/internal/*`
- Use WebSocket for realtime client updates; Pub/Sub for internal distribution
- Deploy frontend via Firebase Hosting; backend as containerized service on GCP

## Consequences
- Clear separation of concerns enables parallel evolution of modules
- Cloud-managed services reduce ops overhead but couple to GCP/Firebase
- AI providers can be swapped with minimal surface changes


