from __future__ import annotations

from vertexai import agent_engines

from agentloom.agent import root_agent

app = agent_engines.AdkApp(agent=root_agent)

