# AgentMesh Scout Orchestrator

Vertex Agent Engine package for the AgentMesh Scout runtime.

The Go AgentMesh service remains the control plane. This agent is the hosted
reasoning runtime for `scout.orchestrate`; deterministic Horizon services are
accessed only through governed MCP tools with a signed Scout agent card.

## Deploy

```bash
cd /Users/kirisyuk/cursor/sanskrite/agents/vertex
python3 -m venv .venv
source .venv/bin/activate
python3 -m pip install -r agentmesh_scout_orchestrator/requirements.txt
PYTHONPATH=/Users/kirisyuk/cursor/sanskrite/agents/vertex \
python3 agentmesh_scout_orchestrator/deploy.py \
  --project "$GCP_PROJECT_ID" \
  --location "${AGENTMESH_VERTEX_LOCATION:-global}" \
  --staging-bucket "gs://${GCP_PROJECT_ID}-agentmesh-artifacts" \
  --service-account "agentmesh@${GCP_PROJECT_ID}.iam.gserviceaccount.com" \
  --mcp-url "$AGENTMESH_MCP_URL" \
  --agent-card "$AGENTMESH_SCOUT_AGENT_CARD"
```

Store the printed resource name as `AGENTMESH_SCOUT_AGENT_ENGINE`.

Required hosted-agent env:

- `AGENTMESH_MCP_URL`
- `AGENTMESH_SCOUT_AGENT_CARD`
- `AGENTMESH_SCOUT_COORDINATOR_MODEL` defaulting to `AGENTMESH_MODEL_FLASH_REASONING`
- `AGENTMESH_SCOUT_MAX_TURNS=12`

## Evaluation

Run domain-gate evals from Go when you need AgentMesh-shaped artifacts in GCS:

```bash
cd /Users/kirisyuk/cursor/sanskrite/backend
go run ./cmd/agentmesh_scout_eval \
  --project "$GCP_PROJECT_ID" \
  --location "${AGENTMESH_VERTEX_LOCATION:-global}" \
  --dataset /path/to/scout_cases.jsonl \
  --gcs-prefix "$AGENTMESH_EVAL_GCS_PREFIX"
```

Run Vertex rubric evals against the deployed Agent Engine:

```bash
cd /Users/kirisyuk/cursor/sanskrite/agents/vertex
PYTHONPATH=/Users/kirisyuk/cursor/sanskrite/agents/vertex \
python3 agentmesh_scout_orchestrator/evaluate.py \
  --project "$GCP_PROJECT_ID" \
  --location "${AGENTMESH_VERTEX_LOCATION:-global}" \
  --agent-engine "$AGENTMESH_SCOUT_AGENT_ENGINE" \
  --dataset /path/to/scout_cases.jsonl \
  --gcs-prefix "$AGENTMESH_EVAL_GCS_PREFIX"
```

The Vertex run uses `FINAL_RESPONSE_QUALITY`, `TOOL_USE_QUALITY`,
`HALLUCINATION`, and `SAFETY`. Domain gates remain canonical product
resolution, candidate emission, path continuity, materialization rejection
rate, cost, and latency.

## Runtime Contract

- Output must match `ScoutEnvelopeV2`.
- Memory context is hint-only and never evidence.
- Hosted Scout talks only to `/mcp`; it no longer depends on ad hoc internal
  HTTP tool routes.
- Hosted Scout remains read-side; graph materialization stays outside the
  hosted reasoning loop.
- Public-provider fallback is disabled; escalation is upward-only inside Vertex.
