## Grok: Scenario Behavior and Quality Evaluation

Date: 2025-10-19

Models:
- grok_fast → grok-4-fast-reasoning
- grok_quality → grok-4-0709

Scenarios (run per model):
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
- Per-scenario timeout: fast ~45s, quality ~75s
- Search enabled via request-level `search.enable: true` and `sources: [{type: "web"}]`
- Tool-calling via function tools when applicable

### grok_fast (grok-4-fast-reasoning)
- Status: All 8 scenarios completed successfully.
- Tool-calling: Returned function calls when expected (tool scenarios).
- Search: Sources extracted; URLs parsed and stored in `Sources`.
- Quality (subjective):
  - Q/A: coherent, concise; maintains topic.
  - Tool outputs: arguments well-structured; adheres to requested shape in strict mode.
  - Search grounding: includes Wikipedia link and relevant context; limited but acceptable breadth.
  - Token usage: Reasoning tokens reported; totals consistent with output size.

### grok_quality (grok-4-0709)
- Status (success): 1, 3, 5, 6, 7
- Status (timeout): 2, 4, 8
- Notes:
  - Some successful runs included refusal-style content for search/Q&A-only; still returned 200 with content.
  - Tool scenarios that succeeded produced structured tool arguments; others timed out under current per-scenario limits.
- Quality (subjective):
  - When successful, answers are detailed and grounded; more conservative behavior (occasional refusals).
  - Tool outputs align with schema in strict mode when returned.
  - Search: sources primarily include target Wikipedia; occasionally adds general context URLs.
  - Token usage: higher reasoning token counts; larger totals than fast model.

### Extracted Parameters
- Usage: prompt/output/total/reasoning tokens parsed and exposed uniformly.
- Sources: URLs extracted from message content and tool arguments; post-processed to trim common trailing punctuation; preserved parentheses for Wikipedia URLs.
- Search queries: when embedded in content/tool arguments, captured and normalized.

### Recommendations
- Increase per-scenario timeout for grok_quality tool+search scenarios (2, 4, 8) if non-timeout completion is required.
- Optionally expand URL cleanup to strip trailing `}` in rare outputs.
- Continue using strict tool-calling for hard JSON adherence; combine with search in two-step workflows if needed.


