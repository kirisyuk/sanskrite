from __future__ import annotations

import argparse
import os
import sys

import vertexai
from google.genai import types as genai_types
from vertexai import Client
from vertexai import agent_engines

PACKAGE_DIR = os.path.dirname(os.path.abspath(__file__))
PACKAGE_PARENT = os.path.dirname(PACKAGE_DIR)
if PACKAGE_PARENT not in sys.path:
    sys.path.insert(0, PACKAGE_PARENT)

from agentmesh_scout_orchestrator.agent import root_agent


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Deploy AgentMesh Scout Orchestrator to Vertex Agent Engine.")
    parser.add_argument("--project", required=True)
    parser.add_argument("--location", default=os.getenv("GOOGLE_CLOUD_LOCATION", "global"))
    parser.add_argument("--staging-bucket", required=True)
    parser.add_argument("--display-name", default="AgentMesh Scout Orchestrator")
    parser.add_argument("--service-account", default=os.getenv("AGENTMESH_AGENT_SERVICE_ACCOUNT", ""))
    parser.add_argument("--mcp-url", default=os.getenv("AGENTMESH_MCP_URL"), required=not os.getenv("AGENTMESH_MCP_URL"))
    parser.add_argument("--agent-card", default=os.getenv("AGENTMESH_SCOUT_AGENT_CARD"), required=not os.getenv("AGENTMESH_SCOUT_AGENT_CARD"))
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    vertexai.init(project=args.project, location=args.location, staging_bucket=args.staging_bucket)
    client = Client(
        project=args.project,
        location=args.location,
        http_options=genai_types.HttpOptions(api_version="v1beta1"),
    )
    app = agent_engines.AdkApp(agent=root_agent)
    requirements = [
        "cloudpickle",
        "google-adk",
        "google-cloud-aiplatform[adk,agent_engines]",
        "google-genai",
        "pydantic",
        "requests",
    ]
    config = {
        "staging_bucket": args.staging_bucket,
        "requirements": requirements,
        "extra_packages": ["agentmesh_scout_orchestrator"],
        "display_name": args.display_name,
        "env_vars": {
            "GOOGLE_CLOUD_AGENT_ENGINE_ENABLE_TELEMETRY": "true",
            "AGENTMESH_MCP_URL": args.mcp_url,
            "AGENTMESH_MCP_AUDIENCE": os.getenv("AGENTMESH_MCP_AUDIENCE", args.mcp_url.rsplit("/mcp", 1)[0]),
            "AGENTMESH_SCOUT_AGENT_CARD": args.agent_card,
            "AGENTMESH_SCOUT_COORDINATOR_MODEL": os.getenv("AGENTMESH_SCOUT_COORDINATOR_MODEL", os.getenv("AGENTMESH_MODEL_FLASH_REASONING", "")),
            "AGENTMESH_SCOUT_MAX_TURNS": os.getenv("AGENTMESH_SCOUT_MAX_TURNS", "12"),
        },
    }
    if args.service_account:
        config["service_account"] = args.service_account
    previous_cwd = os.getcwd()
    try:
        os.chdir(PACKAGE_PARENT)
        remote_app = client.agent_engines.create(agent=app, config=config)
    finally:
        os.chdir(previous_cwd)
    print(remote_app.api_resource.name)


if __name__ == "__main__":
    main()
