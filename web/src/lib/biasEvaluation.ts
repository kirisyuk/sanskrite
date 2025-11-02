import { api } from './api';
import { useGenaiChatStore } from '../stores/genaiChatStore';

/**
 * Evaluates bias and thoughtfulness for a message (user or assistant).
 * @param messageId - ID of the message to evaluate
 * @param messageRole - 'user' or 'assistant'
 * @param textToEvaluate - Content to evaluate
 * @param contextText - Optional context (user message for assistant eval, assistant reply for user eval)
 * @param evalModel - Model to use for evaluation (e.g., 'grok_fast')
 * @param preferredLang - Target language for evaluation ('ru', 'en', 'kk', etc.)
 * @returns Promise that resolves when evaluation is complete
 */
// Track message IDs for which evaluation is in progress to prevent duplicate requests
const evaluatingBias = new Set<string>();

export async function evaluateBias(params: {
  messageId: string;
  messageRole: 'user' | 'assistant';
  textToEvaluate: string;
  contextText?: string;
  evalModel: string;
  preferredLang: string;
}): Promise<void> {
  const { messageId, messageRole, textToEvaluate, contextText, evalModel, preferredLang } = params;

  // Check if evaluation is already in progress for this message
  if (evaluatingBias.has(messageId)) {
    console.log('[BiasEval] Evaluation already in progress, skipping', { messageId, messageRole });
    return;
  }

  // Check if eval.bias already exists for this message
  const genaiChat = useGenaiChatStore.getState();
  const map = (genaiChat as any).persistedByLocalId || {};
  const serverId = map[messageId] || messageId;
  const reverseMap: Record<string, string[]> = {};
  Object.entries(map).forEach(([local, server]) => {
    if (!reverseMap[server as string]) reverseMap[server as string] = [];
    reverseMap[server as string].push(local);
  });
  const localIds = reverseMap[messageId] || [];
  const existingEvalBias = [...genaiChat.interactions].reverse().find((it: any) => 
    it.subkind === 'eval.bias' && 
    (it.replyTo === messageId || (serverId && it.replyTo === serverId) || localIds.includes(it.replyTo || ''))
  );
  if (existingEvalBias) {
    console.log('[BiasEval] eval.bias already exists, skipping', { messageId, messageRole });
    return;
  }

  // Mark as evaluating
  evaluatingBias.add(messageId);

  const langName = (() => {
    switch (preferredLang) {
      case 'ru': return 'Russian';
      case 'en': return 'English';
      case 'kk':
      case 'kz': return 'Kazakh';
      default: return 'Russian';
    }
  })();

  const biasDict = [
    'confirmation_bias',
    'selection_bias',
    'framing_bias',
    'recency_bias',
    'availability_bias',
    'cultural_bias',
    'geo_linguistic_bias',
    'authority_bias',
    'safety_over_caution_skew',
    'overclaiming',
    'overgeneralization',
    'loaded_language'
  ];

  const schema = {
    type: 'object',
    additionalProperties: false,
    properties: {
      biasLevel: { type: 'number', minimum: 0, maximum: 1 },
      categories: {
        type: 'array',
        minItems: 1,
        maxItems: 2,
        items: {
          type: 'object',
          additionalProperties: false,
          properties: {
            code: { type: 'string', enum: biasDict },
            explanation: { type: 'string', minLength: 10, maxLength: 600 }
          },
          required: ['code', 'explanation']
        }
      },
      thoughtfulnessLevel: { type: 'number', minimum: 0, maximum: 1 },
      thoughtfulnessDimensions: {
        type: 'object',
        additionalProperties: false,
        properties: {
          analyticalDepth: { type: 'number', minimum: 0, maximum: 1 },
          epistemicHumility: { type: 'number', minimum: 0, maximum: 1 },
          alternativeConsideration: { type: 'number', minimum: 0, maximum: 1 },
          evidenceReasoningQuality: { type: 'number', minimum: 0, maximum: 1 },
          practicalGrounding: { type: 'number', minimum: 0, maximum: 1 },
        },
        required: [
          'analyticalDepth',
          'epistemicHumility',
          'alternativeConsideration',
          'evidenceReasoningQuality',
          'practicalGrounding'
        ]
      }
    },
    required: ['biasLevel', 'categories', 'thoughtfulnessLevel', 'thoughtfulnessDimensions']
  } as any;

  const schemaText = JSON.stringify(schema, null, 2);
  const dictText = JSON.stringify(biasDict);

  // Build context and target blocks based on role
  const ctxBlock = contextText && contextText.trim().length > 0
    ? (messageRole === 'assistant'
        ? `USER_MESSAGE (context, do NOT evaluate this text):\n${contextText}\n\n`
        : `ASSISTANT_REPLY (context, do NOT evaluate this text):\n${contextText}\n\n`)
    : '';

  const targetBlock = messageRole === 'assistant'
    ? `ASSISTANT_REPLY (to evaluate):\n${textToEvaluate}`
    : `USER_MESSAGE (to evaluate):\n${textToEvaluate}`;

  const evalInstruction = messageRole === 'assistant'
    ? `Evaluate ONLY the ASSISTANT_REPLY both standalone and IN RELATION to USER_MESSAGE (if provided). Do NOT assess the user's content.`
    : `Evaluate ONLY the USER_MESSAGE. Do NOT assess the assistant's content.`;

  const promptEval = `In this request, you act as an external expert specializing in discussion evaluation, and you must return your assessment strictly following the provided schema in JSON format. Return ONLY JSON (no prose, no markdown, no code fences).

User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.

Field names (exact):
- Required: biasLevel, categories, thoughtfulnessLevel, thoughtfulnessDimensions
- categories: array of 1–2 objects, each with 'code' (from DICTIONARY) and 'explanation' (how this bias manifests in the evaluated text; 1–3 sentences).

Bias scale (biasLevel 0..1):
- 0.0–0.1: neutral; balanced alternatives; neutral language.
- 0.2–0.4: mild tilt; some alternatives missing; occasional preference.
- 0.5–0.7: pronounced tilt; one-sided argument; emotive language appears.
- 0.8–1.0: strong advocacy; ignores counterfacts; categorical claims.

THOUGHTFULNESS SCALE (0.0–1.0): Evaluate across FIVE dimensions. Each dimension must be scored in 0.1 increments. The overall thoughtfulnessLevel MUST be the MEAN of the five dimensions, rounded to one decimal (0.1).

DIMENSION 1: Analytical Depth
0.0–0.3: Surface-level; restates question; binary conclusions; no causal reasoning
0.4–0.6: Identifies 1–2 factors; basic causality; limited mechanism explanation
0.7–0.8: Multi-factor analysis; explores mechanisms; shows interactions between factors
0.9–1.0: Systems-level thinking; second-order effects; identifies non-obvious dependencies

DIMENSION 2: Epistemic Humility
0.0–0.3: Presents all claims as certainties; no acknowledgment of limits
0.4–0.6: Single hedge ("this might" / "generally") without explaining uncertainty sources
0.7–0.8: Multiple explicit uncertainty markers; names what is unknown; discusses confidence levels
0.9–1.0: Distinguishes knowns/unknowns/unknowables; explains why confidence varies; identifies what would change assessment

DIMENSION 3: Alternative Consideration
0.0–0.3: Single path; no alternatives mentioned
0.4–0.6: Mentions alternatives but doesn't explore trade-offs
0.7–0.8: Explores 2+ alternatives with explicit trade-offs for each
0.9–1.0: Compares alternatives on multiple dimensions; discusses context-dependency of choice; identifies decision criteria

DIMENSION 4: Evidence & Reasoning Quality
0.0–0.3: Assertions without support; anecdotal reasoning; logical fallacies present
0.4–0.6: Some supporting reasoning but gaps/leaps present; informal logic
0.7–0.8: Structured argument; reasoning explicit; minimal logical gaps
0.9–1.0: Rigorous reasoning chain; anticipates objections; cites evidence types (even if not specific sources); distinguishes correlation/causation

DIMENSION 5: Practical Grounding
0.0–0.3: Abstract theory only; no real-world constraints; ignores implementation
0.4–0.6: Mentions constraints but doesn't integrate them; generic recommendations
0.7–0.8: Discusses real constraints (resources, time, politics); context-specific guidance
0.9–1.0: Anticipates implementation challenges; discusses failure modes; provides decision heuristics for ambiguous cases

SCORING INSTRUCTIONS:
1) Score each dimension independently (0.0–1.0 in 0.1 increments)
2) thoughtfulnessLevel = mean(dimensions), rounded to 0.1
3) A response can score high on depth but low on humility → the average must reflect all
4) Generic LLM responses typically score 0.4–0.6 (not 0.9)
5) Reserve 0.8+ for genuinely exceptional reasoning

EXAMPLES:
Low (0.2): "Just do X. It's the best approach."
→ Depth: 0.1, Humility: 0.0, Alternatives: 0.0, Evidence: 0.2, Grounding: 0.3 = 0.12

Medium (0.5): "X is generally better because [reason]. However, Y might work in some cases."
→ Depth: 0.4, Humility: 0.5, Alternatives: 0.5, Evidence: 0.5, Grounding: 0.6 = 0.5

High (0.8): "X offers [trade-off 1] while Y provides [trade-off 2]. Choice depends on [factors]. Uncertainty exists around [specific aspect] because [reason]. In [context A], prefer X; in [context B], Y may be better. Implementation risks include [failure modes]."
→ Depth: 0.8, Humility: 0.8, Alternatives: 0.9, Evidence: 0.8, Grounding: 0.8 = 0.82

DICTIONARY for categories.code (English tokens only):
${dictText}

The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${schemaText}

${ctxBlock}${targetBlock}

${evalInstruction} Return only the JSON.`;

  const langDirective = `You MUST produce all output strictly in ${langName} language. Use English only for technical terms or code if they are standard industry terms.`;

  console.log('[BiasEval] sending structured eval', { messageRole, model: evalModel, promptChars: promptEval.length });

  const res = await api.genaiGenerate({
    model: evalModel,
    prompt: promptEval,
    strictJSONEnabled: true,
    jsonSchema: schema,
    maxOutputTokens: 4096,
    webSearchEnabled: false,
    systemInstruction: langDirective,
  });

  // Debug raw model response
  try {
    const rawJSON = (res as any)?.rawJSON;
    if (typeof rawJSON === 'string' && rawJSON.trim().length > 0) {
      console.log('[BiasEval] raw model JSON', { model: evalModel, messageRole, rawJSON });
    } else {
      console.log('[BiasEval] raw model content', { model: evalModel, messageRole, content: res?.content });
    }
  } catch {}

  // Parse response
  let parsed: any = null;
  try {
    if (typeof res?.content === 'string') {
      const s = res.content.trim();
      if (s.startsWith('{') || s.startsWith('[')) parsed = JSON.parse(s);
    }
    const rawJSON = (res as any)?.rawJSON;
    if (!parsed && typeof rawJSON === 'string') {
      const s2 = rawJSON.trim();
      if (s2.startsWith('{') || s2.startsWith('[')) parsed = JSON.parse(s2);
    }
    if (!parsed && typeof res?.content === 'string') {
      const m = res.content.match(/\{[\s\S]*\}/);
      if (m && m[0]) {
        parsed = JSON.parse(m[0]);
      }
    }
  } catch (pe) {
    console.warn('[BiasEval] parse failed', pe, { contentPreview: (res?.content || '').slice(0, 200) });
  }

  if (!parsed || typeof parsed !== 'object') {
    console.log('[BiasEval] no structured JSON parsed, skipping eval');
    return;
  }

  // Build evaluation object
  const clamp01 = (n: any) => Math.max(0, Math.min(1, Number(n) || 0));
  const dimsObj = (() => {
    try {
      const d = parsed?.thoughtfulnessDimensions || {};
      if (!d || typeof d !== 'object') return undefined;
      return {
        analyticalDepth: clamp01(d.analyticalDepth),
        epistemicHumility: clamp01(d.epistemicHumility),
        alternativeConsideration: clamp01(d.alternativeConsideration),
        evidenceReasoningQuality: clamp01(d.evidenceReasoningQuality),
        practicalGrounding: clamp01(d.practicalGrounding),
      };
    } catch { return undefined; }
  })();

  const thLevel = (() => {
    if (typeof parsed?.thoughtfulnessLevel === 'number') return clamp01(parsed.thoughtfulnessLevel);
    if (dimsObj && Object.values(dimsObj).every((v) => typeof v === 'number')) {
      const vals = Object.values(dimsObj) as number[];
      return Math.round((vals.reduce((a, b) => a + b, 0) / vals.length) * 10) / 10;
    }
    return clamp01(parsed?.thoughtfulness);
  })();

  const ev = {
    biasLevel: clamp01(parsed?.biasLevel),
    thoughtfulnessLevel: thLevel,
    thoughtfulness: thLevel,
    thoughtfulnessDimensions: dimsObj,
    categories: Array.isArray(parsed?.categories)
      ? parsed.categories
          .filter((c: any) => c && typeof c.code === 'string')
          .slice(0, 2)
          .map((c: any) => ({ code: String(c.code), explanation: typeof c.explanation === 'string' ? c.explanation : '' }))
      : []
  } as any;

  console.log('[BiasEval] structured eval result', { model: evalModel, messageRole, ev });

  // Update local state
  useGenaiChatStore.getState().setEvaluation(messageId, ev);

  // Create eval.bias interaction
  try {
    const evalId = Date.now().toString() + '-eval-bias-' + (messageRole === 'assistant' ? 'a' : 'u');
    const projectId = useGenaiChatStore.getState().projectId;

    // Add to local state first
    useGenaiChatStore.setState((s) => ({
      interactions: [
        ...s.interactions,
        {
          id: evalId,
          projectId: s.projectId,
          kind: 'system',
          subkind: 'eval.bias',
          content: JSON.stringify(ev),
          contentSanitized: JSON.stringify(ev),
          replyTo: messageId,
          relations: [{ type: 'evaluates', targetId: messageId }],
          createdAt: new Date().toISOString(),
        } as any,
      ],
    } as any));

    // Persist to server (async, don't block UI)
    if (projectId && projectId !== 'local-unfold') {
      (async () => {
        try {
          const map = (useGenaiChatStore.getState() as any).persistedByLocalId || {};
          const serverReplyTo = map[messageId] || messageId;

          const payload: any = {
            kind: 'system',
            subkind: 'eval.bias',
            content: JSON.stringify(ev),
            replyTo: serverReplyTo,
            relations: [{ type: 'evaluates', targetId: serverReplyTo }],
          };

          const res = await api.createProjectInteraction(projectId, payload);

          // Update local interaction with server ID
          useGenaiChatStore.setState((st) => {
            const items = [...st.interactions];
            const idx = items.findIndex((it) => it.id === evalId);
            if (idx >= 0) { (items[idx] as any).id = res.id; }

            const newMap = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
            newMap[evalId] = res.id;

            return { interactions: items, persistedByLocalId: newMap } as any;
          });

          console.log('[BiasEval] persisted to server', { evalId, serverId: res.id, messageRole });
        } catch (e) {
          console.warn('[BiasEval] Failed to persist to server', e);
        }
      })();
    }
  } catch (e) {
    console.warn('[BiasEval] Failed to create eval.bias', e);
  } finally {
    // Always remove from evaluating set
    evaluatingBias.delete(messageId);
  }
}

