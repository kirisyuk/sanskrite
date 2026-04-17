"""Regression: session.state in storage must receive ah_pack via event state_delta."""

from __future__ import annotations

import unittest

from google.adk.events import Event, EventActions
from google.adk.sessions.in_memory_session_service import InMemorySessionService

from agenthorde.config import APP_NAME, STATE_FINAL_ANSWER_KEY, STATE_PACK_KEY, USER_ID


class SessionPackPersistTests(unittest.IsolatedAsyncioTestCase):
    async def test_pack_merges_only_when_in_state_delta(self) -> None:
        svc = InMemorySessionService()
        session = await svc.create_session(app_name=APP_NAME, user_id=USER_ID, session_id="t1")

        pack_empty = {"reasoningGraph": {"nodes": [], "edges": []}}
        ev_init = Event(
            author="root",
            actions=EventActions(state_delta={STATE_PACK_KEY: pack_empty}),
        )
        await svc.append_event(session=session, event=ev_init)

        s1 = await svc.get_session(app_name=APP_NAME, user_id=USER_ID, session_id="t1")
        assert s1 is not None
        self.assertEqual(len(s1.state[STATE_PACK_KEY]["reasoningGraph"]["nodes"]), 0)

        # Mimic supervisor bug: mutate in-memory only — storage unchanged (no event).
        session.state[STATE_PACK_KEY] = {
            "reasoningGraph": {"nodes": [{"id": "n1", "kind": "claim", "text": "x"}], "edges": []}
        }
        s2 = await svc.get_session(app_name=APP_NAME, user_id=USER_ID, session_id="t1")
        assert s2 is not None
        self.assertEqual(len(s2.state[STATE_PACK_KEY]["reasoningGraph"]["nodes"]), 0)

        # Fix: persist full pack in final event delta (as supervisor does after merge).
        pack_merged = {
            "reasoningGraph": {"nodes": [{"id": "n1", "kind": "claim", "text": "x"}], "edges": []},
            "finalAnswer": "done",
        }
        ev_final = Event(
            author="root",
            actions=EventActions(
                state_delta={
                    STATE_FINAL_ANSWER_KEY: "done",
                    STATE_PACK_KEY: pack_merged,
                }
            ),
        )
        await svc.append_event(session=session, event=ev_final)

        s3 = await svc.get_session(app_name=APP_NAME, user_id=USER_ID, session_id="t1")
        assert s3 is not None
        self.assertEqual(len(s3.state[STATE_PACK_KEY]["reasoningGraph"]["nodes"]), 1)
        self.assertEqual(s3.state[STATE_FINAL_ANSWER_KEY], "done")


if __name__ == "__main__":
    unittest.main()
