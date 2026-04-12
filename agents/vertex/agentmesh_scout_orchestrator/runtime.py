from __future__ import annotations

from vertexai import agent_engines

from agentmesh_scout_orchestrator.agent import root_agent

app = agent_engines.AdkApp(agent=root_agent)
