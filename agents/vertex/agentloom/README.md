# AgentLoom

Standalone Vertex ADK package for experimenting with multi-agent investigation logic
outside `AgentMesh`.

Current scope:

- separate from `AgentMesh` and `scout.*`
- no canonicalization
- no graph writes
- no materialization
- strict JSON contracts between agents
- deterministic final composer without LLM

Current agent lineup:

1. `Supervisor`
2. `Investigator Agent`
3. `Gap/Bridge Agent`
4. `Critic Agent`
5. deterministic `Compose`

This package now uses shared `session.state` as the main investigation memory.

- `Supervisor` selects one or more roles first, reads shared memory, explicit `agent_contracts`, and explicit `tool_contracts`, then delegates one concrete role-scoped mission at a time.
- Roles are fixed to `supply_chain`, `governance_and_control`, and `policy_impact`.
- Role contracts are loaded directly from the core `scout/taxonomy` Go sources at runtime: expected entity types, allowed relations, path-carried edges, overlays, and role-specific missing-info prompts are not duplicated in Python.
- `Investigator Agent` has its own internal reasoning cycle: it inspects the mission, checks what the real tools can do, gathers corpus evidence when present, uses Vertex grounded retrieval when configured, or switches to honest `mixed` model-knowledge reasoning when the evidence substrate is absent.
- `Gap/Bridge Agent` reads the current graph plus unresolved gaps and proposes only role-valid bridging hypotheses or next questions.
- `Critic Agent` evaluates sufficiency and honesty of the current graph against the same role contracts and decides whether more investigation is needed or composition is justified.
- `AgentResult` carries a `graphDelta`, `gaps`, `handoffRecommendations`, and explicit `failure`.
- The supervisor merges each result back into shared memory and decides the next delegation.

There is no fake branch execution and no fake grounded retrieval.

The real tools currently exposed inside the investigation runtime are:

- `read_shared_memory`
- `read_corpus_index`
- `read_corpus_document`
- `search_corpus`
- `vertex_grounded_search`
- `append_graph_observations`
- `record_open_gaps`
- `record_failure`

All substantive findings use a graph-native contract: `subject -> relation -> object`.

## Request Contract

`AgentLoom` expects the user message to be a JSON object matching `AgentLoomRequest`.
The user-facing contract stays small: question, optional corpus, and optional requested roles.
If corpus is omitted, AgentLoom still runs in `mixed` mode. The investigator reasons from model knowledge, marks observations as `model_knowledge`, and keeps verification gaps explicit.

Minimal shape:

```json
{
  "question": "Who controls the Chile -> Germany lithium chain and where are the main regulatory choke points?",
  "requestedRoles": ["supply_chain", "governance_and_control", "policy_impact"],
  "corpusDocuments": [
    {
      "documentId": "doc-1",
      "title": "Example note",
      "text": "..."
    }
  ]
}
```

See [example_request.json](/Users/kirisyuk/cursor/sanskrite/agents/vertex/agentloom/example_request.json).
Question-only mode example: [example_question_only.json](/Users/kirisyuk/cursor/sanskrite/agents/vertex/agentloom/example_question_only.json).

## Local Setup

```bash
cd /Users/kirisyuk/cursor/sanskrite/agents/vertex
python3 -m venv .venv-agentloom
source .venv-agentloom/bin/activate
python3 -m pip install -r agentloom/requirements.txt
```

Validate the request envelope without calling models:

```bash
PYTHONPATH=/Users/kirisyuk/cursor/sanskrite/agents/vertex \
python3 agentloom/runner.py \
  --input-file /Users/kirisyuk/cursor/sanskrite/agents/vertex/agentloom/example_request.json \
  --validate-only
```

Run the full local ADK workflow:

```bash
export GOOGLE_GENAI_USE_VERTEXAI=True
export GOOGLE_CLOUD_PROJECT="$GCP_PROJECT_ID"
export GOOGLE_CLOUD_LOCATION="${AGENTLOOM_VERTEX_LOCATION:-global}"

PYTHONPATH=/Users/kirisyuk/cursor/sanskrite/agents/vertex \
python3 agentloom/runner.py \
  --input-file /Users/kirisyuk/cursor/sanskrite/agents/vertex/agentloom/example_request.json
```

## Deploy To Vertex Agent Engine

```bash
cd /Users/kirisyuk/cursor/sanskrite/agents/vertex
source .venv-agentloom/bin/activate

PYTHONPATH=/Users/kirisyuk/cursor/sanskrite/agents/vertex \
python3 agentloom/deploy.py \
  --project "$GCP_PROJECT_ID" \
  --location "${AGENTLOOM_VERTEX_LOCATION:-global}" \
  --staging-bucket "gs://${GCP_PROJECT_ID}-agentloom-artifacts" \
  --service-account "agentloom@${GCP_PROJECT_ID}.iam.gserviceaccount.com"
```

The command prints the deployed Agent Engine resource name.

## Runtime Notes

- The corpus comes in through the request envelope, so you can vary data without redeploying.
- The request stays minimal; the supervisor creates missions from shared memory, role contracts, and real tool availability, not from user-supplied orchestration hints.
- If `requestedRoles` is empty, AgentLoom chooses the smallest sufficient role set from the three built-in roles.
- `SupervisorDecision` decides which peer agent should act next and with what mission.
- Shared memory contains selected roles, role contracts, the working graph, open gaps, failures, answered questions, agent results, and `logicTrace`.
- If `VERTEX_SEARCH_*` env vars are configured, the investigator can call grounded Vertex AI Search retrieval through `vertex_grounded_search`.
- If no corpus is supplied and grounded search is unavailable, the system stays honest: it reasons from model knowledge, labels that reasoning, and preserves verification needs instead of pretending grounding exists.
- The final response is deterministic JSON assembled from the shared graph and includes the full reasoning trace, including the final `compose` decision.
- This package is intentionally read-side only.
