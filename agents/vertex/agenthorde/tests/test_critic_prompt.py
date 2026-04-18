from __future__ import annotations

from agenthorde.critic.prompt import render_critic_prompt


class TestRenderCriticPrompt:
    def test_substitutes_compact_state_keys(self) -> None:
        state = {
            "ah_user_question": "Semiconductor supply chain scenario",
            "ah_pack_json": '{"cycle":3,"nodes":42}',
            "ah_analyzer_json": '{"relations":17}',
            "ah_governance_json": '{"people":5}',
        }
        prompt = render_critic_prompt(state)
        assert "Semiconductor supply chain scenario" in prompt
        assert '"nodes":42' in prompt
        assert '"relations":17' in prompt
        assert '"people":5' in prompt
        assert "tools-disabled mode" in prompt
        assert "CriticVerdict JSON" in prompt

    def test_falls_back_to_raw_state_when_compact_missing(self) -> None:
        state = {
            "ah_user_question": "Q",
            "ah_pack": {"k": "v"},
        }
        prompt = render_critic_prompt(state)
        assert '"k": "v"' in prompt
        assert "Q" in prompt

    def test_handles_empty_state(self) -> None:
        prompt = render_critic_prompt({})
        assert "(empty)" in prompt
        assert "{}" in prompt
