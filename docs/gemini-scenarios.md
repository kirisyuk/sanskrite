## Gemini: Scenario Behavior and Timings

Date: 2025-10-19

Models (aliases → provider IDs):
- gemini_fast → gemini-2.5-flash-lite
- gemini_balanced → gemini-2.5-flash
- gemini_quality → gemini-2.5-pro

Scenarios (per model):
1. loose: Q/A only
2. loose: Q/A + tool
3. loose: Q/A + search
4. loose: Q/A + tool + search
5. strict: Q/A only (schema)
6. strict: Q/A + tool (schema)
7. strict: Q/A + search (schema)
8. strict: Q/A + tool + search (schema)

Execution settings:
- MaxOutputTokens: 8192
- Per-scenario timeouts: fast ~45s, balanced ~60s, quality ~90s
- Search: Google grounding via public v1beta (no tools together)
- Strict: function-calling with a single `return_structured` tool carrying the JSON schema (no `response_mime_type` when tools are present)
- Tool+Search: implemented as a 2-step pipeline (see below)

Implementation notes (public v1beta):
- Function calling (tools) and grounding (search) cannot be enabled in the same request. When both are requested we perform:
  - Step A: tool-only request (no search) → strictly structured result (validated against schema on our side)
  - Step B: grounded request (search enabled, no tools) → `Sources`/`SearchQueries`
  - Merge: combine `Usage`/`Sources`/`SearchQueries` in unified response
- Strict JSON path: uses function tool (schema) only; do NOT set `response_mime_type` when tools are in use.

### Results Overview
- All 8 scenarios are supported with the above two-step handling for “tool + search”.
- Strict scenarios succeed after removing `response_mime_type` from tool-based requests and enforcing strictness via the tool schema + server-side validation.
- Sources and search queries are returned for search scenarios; URLs are post-processed (redirect filtering, balanced parentheses) similarly to other providers.

### Quality observations
- The models produce compact and schema-conformant `return_structured` arguments in strict scenarios.
- Grounded answers cite Wikipedia and related resources; attribution presence and richness vary by model, with `gemini_quality` generally providing the best grounded rationale.
- When run as two-step (tool+search), content quality is on par with single-step loose runs; the merge preserves tool structure and adds citations.

### Indicative timings (one recent run)
- gemini_balanced: full suite finished ~2m45s (mixed scenarios). Individual scenario durations vary by prompt/search.
- gemini_fast / gemini_quality: similar order-of-magnitude per-scenario timings as configured timeouts; exact values depend on prompt length and search latency.

### Recommendations
- Always avoid `response_mime_type` when using tools; rely on function tool + schema for strictness.
- For any Gemini request that needs both tools and search, prefer the two-step pipeline to maintain provider compliance and result quality.
- Keep server-side schema validation and URL post-processing enabled for consistent outputs across providers.

### Known caveats: MALFORMED_FUNCTION_CALL
- Meaning: the model attempted a function call but its `functionCall.args` failed validation on Gemini’s side (non‑parsable JSON, missing required fields, wrong types, or mismatched function name).
- Common triggers:
  - Overly strict/complex JSON schemas (e.g., enums, oneOf/anyOf, numeric constraints, or `additionalProperties: false`).
  - Function name mismatch or any extra text output in addition to the function call.
  - High temperature in strict scenarios leading to format drift.
- Mitigations we use:
  - Single, simple tool `return_structured` with a minimal object schema (basic `type` per property; avoid complex constraints).
  - Do not set `response_mime_type` when tools are active; strictness is enforced via the tool schema and server-side validation.
  - System instruction requires: “Return ONLY by calling `return_structured`.”
  - Lower temperature for strict runs.


