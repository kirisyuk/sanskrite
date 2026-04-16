"""LogicTraceEntry telemetry fields (plan phase 0)."""

from __future__ import annotations

import unittest

from agentloom.contracts import InvestigationMission, LogicTraceEntry, SupervisorDecision


class TestLogicTraceTelemetry(unittest.TestCase):
    def test_logic_trace_entry_optional_telemetry_roundtrip(self) -> None:
        mission = InvestigationMission(
            missionId="m1",
            objective="test",
            assigned_roles=["supply_chain"],
            groundingMode="mixed",
        )
        dec = SupervisorDecision(
            cycle=1,
            assessment="ok",
            rationale=["x"],
            next_agent="supply_chain_investigator_agent",
            mission=mission,
        )
        entry = LogicTraceEntry(
            cycle=1,
            stateSeen={"k": 1},
            decision=dec,
            mission=mission,
            agentResult=None,
            resultingState={"k": 2},
            nextStepLogic="delegate",
            policyCorrection=None,
            supervisor_next_agent_raw="critic_agent",
            delegate_policy_outcome={
                "supervisorNextAgentRaw": "critic_agent",
                "delegatePolicyDetails": ["stagnation_redirect_to_supply_chain"],
                "nextAgent": "supply_chain_investigator_agent",
            },
            hypothesis_restructure={"applied": True, "decisionNote": "Runtime redirected", "branchId": "b1"},
        )
        dumped = entry.model_dump(mode="json", by_alias=True)
        restored = LogicTraceEntry.model_validate(dumped)
        self.assertEqual(restored.supervisor_next_agent_raw, "critic_agent")
        self.assertEqual(restored.delegate_policy_outcome.get("nextAgent"), "supply_chain_investigator_agent")
        self.assertTrue(restored.hypothesis_restructure.get("applied"))


if __name__ == "__main__":
    unittest.main()
