from __future__ import annotations

import json
import unittest

from agenthorde.config import STATE_PACK_KEY, STATE_TOOL_LEDGER_KEY
from agenthorde_common.schemas.state import SessionPack
from agenthorde_common.tools.tool_context import MutableStateToolContext
from agenthorde.tools.builtins import ah_apply, ah_knowledge_search, ah_retrieve


class ToolTests(unittest.TestCase):
    def setUp(self) -> None:
        self.ctx = MutableStateToolContext(state={})
        pack = SessionPack(user_question="hello")
        self.ctx.state[STATE_PACK_KEY] = pack.model_dump(mode="json", by_alias=True)
        self.ctx.state[STATE_TOOL_LEDGER_KEY] = []

    def test_knowledge_search_stub(self) -> None:
        out = ah_knowledge_search("What is gravity?", tool_context=self.ctx)
        body = json.loads(json.dumps(out))
        self.assertTrue(body.get("ok"))

    def test_retrieve_empty_query_validation(self) -> None:
        out = ah_retrieve("", tool_context=self.ctx)
        body = json.loads(json.dumps(out))
        self.assertFalse(body.get("ok"))

    def test_apply_merge_nodes(self) -> None:
        patch = json.dumps({"nodes": [{"label": "n1"}]}, ensure_ascii=False)
        out = ah_apply(patch, hypothesis_id="H0", tool_context=self.ctx)
        body = json.loads(json.dumps(out))
        self.assertTrue(body.get("ok"))
        raw = self.ctx.state[STATE_PACK_KEY]
        pack = SessionPack.model_validate(raw)
        self.assertGreaterEqual(len(pack.working_graph.get("nodes") or []), 1)


if __name__ == "__main__":
    unittest.main()
