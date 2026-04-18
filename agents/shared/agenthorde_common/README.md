# agenthorde-common

Python package with the **canonicalizer** (post-run graph normalization) and **shared session / agent I/O schemas** used by both:

- **AgentHorde (Vertex)** — `agents/vertex/agenthorde/` — Google ADK + Vertex AI.
- **AgentHorde Azure** — `agents/azure/agenthorde_azure/` — Azure OpenAI–centric orchestration (no ADK).

Install editable from a venv rooted at either runtime:

```bash
pip install -e agents/shared/agenthorde_common
```

See [docs/agents/agenthorde-runtimes.md](../../../docs/agents/agenthorde-runtimes.md) for layout and how we test both paths.
