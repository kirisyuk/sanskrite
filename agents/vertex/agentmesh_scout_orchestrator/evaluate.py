"""Run Vertex Agent Engine rubric evaluation for AgentMesh Scout."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
from typing import Any

import pandas as pd
import vertexai
from google.genai import types as genai_types
from vertexai import Client
from vertexai import types as vertex_types

try:
    from agentmesh_scout_orchestrator.agent import root_agent
except ImportError:  # pragma: no cover - supports python -m package execution.
    from .agent import root_agent


RUBRIC_METRICS = [
    vertex_types.RubricMetric.FINAL_RESPONSE_QUALITY,
    vertex_types.RubricMetric.TOOL_USE_QUALITY,
    vertex_types.RubricMetric.HALLUCINATION,
    vertex_types.RubricMetric.SAFETY,
]

DOMAIN_GATE_NAMES = [
    "canonical_product_resolution",
    "candidate_emission",
    "path_continuity",
    "materialization_rejection_rate",
    "cost",
    "latency",
]


def main() -> None:
    parser = argparse.ArgumentParser(description="Evaluate the deployed AgentMesh Scout Agent Engine.")
    parser.add_argument("--project", default=os.getenv("GCP_PROJECT_ID"), required=not os.getenv("GCP_PROJECT_ID"))
    parser.add_argument("--location", default=os.getenv("AGENTMESH_VERTEX_LOCATION", "global"))
    parser.add_argument("--agent-engine", default=os.getenv("AGENTMESH_SCOUT_AGENT_ENGINE"), required=not os.getenv("AGENTMESH_SCOUT_AGENT_ENGINE"))
    parser.add_argument("--dataset", required=True, help="JSONL file with representative Scout cases")
    parser.add_argument("--gcs-prefix", default=os.getenv("AGENTMESH_EVAL_GCS_PREFIX"), required=not os.getenv("AGENTMESH_EVAL_GCS_PREFIX"))
    args = parser.parse_args()

    vertexai.init(project=args.project, location=args.location)
    client = Client(
        project=args.project,
        location=args.location,
        http_options=genai_types.HttpOptions(api_version="v1beta1"),
    )

    cases = read_jsonl(args.dataset)
    dataset = build_dataset(cases)
    dataset_with_inference = client.evals.run_inference(
        agent=args.agent_engine,
        src=dataset,
    )
    agent_info = vertex_types.evals.AgentInfo.load_from_agent(
        root_agent,
        args.agent_engine,
    )
    evaluation_run = client.evals.create_evaluation_run(
        dataset=dataset_with_inference,
        agent_info=agent_info,
        metrics=RUBRIC_METRICS,
        dest=args.gcs_prefix,
    )
    print(json.dumps({
        "evaluationRun": evaluation_run.name,
        "gcsPrefix": args.gcs_prefix,
        "rubricMetrics": [metric.name for metric in RUBRIC_METRICS],
        "domainGatesTrackedByGoRunner": DOMAIN_GATE_NAMES,
        "caseCount": len(cases),
    }, indent=2))


def read_jsonl(path: str) -> list[dict[str, Any]]:
    cases: list[dict[str, Any]] = []
    for line in Path(path).read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        item = json.loads(line)
        if not isinstance(item, dict):
            raise ValueError(f"dataset row must be an object: {line[:80]}")
        cases.append(item)
    if not cases:
        raise ValueError("dataset is empty")
    return cases


def build_dataset(cases: list[dict[str, Any]]) -> pd.DataFrame:
    rows: list[dict[str, Any]] = []
    for index, item in enumerate(cases):
        tenant_id = str(item.get("tenantId") or item.get("tenant_id") or "global")
        rows.append({
            "case_id": str(item.get("caseId") or item.get("id") or f"case-{index + 1:03d}"),
            "prompt": prompt_for_case(item),
            "reference": item.get("reference") or item.get("expected"),
            "session_inputs": vertex_types.evals.SessionInput(
                user_id=tenant_id,
                state={
                    "tenantId": tenant_id,
                    "capability": "scout.orchestrate",
                    "evalCase": item,
                },
            ),
        })
    return pd.DataFrame(rows)


def prompt_for_case(item: dict[str, Any]) -> str:
    prompt = item.get("prompt") or item.get("query") or item.get("goal")
    if isinstance(prompt, str) and prompt.strip():
        return prompt.strip()
    return "Run AgentMesh Scout orchestration for this case:\n" + json.dumps(item, ensure_ascii=False, sort_keys=True)


if __name__ == "__main__":
    main()
