# Vertex Harvest Portfolio Agent

First Vertex-oriented harvest agent for Horizon entities.

What it does:

- loads a real entity from `horizon_fragments` in Firestore by canonical path
- uses a Vertex-backed ADK agent with `google_search` to discover high-value external materials
- drafts a consulting-grade portfolio and related-entity map
- fetches the discovered materials directly
- stores raw artifacts and extracted text into a dedicated GCS bucket
- writes a structured enrichment record into Firestore

Default storage:

- bucket: `gs://$PROJECT_ID-vertex-harvest-agent-artifacts`
- Firestore collections:
  - `vertex_harvest_runs`
  - `vertex_harvest_enrichments`

## Local run

```bash
cd agents/vertex/harvest_portfolio_agent
python3 -m venv .venv
source .venv/bin/activate
python3 -m pip install -r requirements.txt
PYTHONPATH=/Users/kirisyuk/cursor/sanskrite/agents/vertex \
python3 runner.py \
  --project prevalent-horizon-test \
  --canonical-path /fac/ae/jebel-ali-port-terminal
```

The script expects Application Default Credentials to already be configured.

## Notes

- `agent.py` is the ADK/Vertex search agent definition.
- `runner.py` is deterministic glue around entity lookup, fetch, storage, and final synthesis.
- The bucket is created automatically on first run if it does not exist.
