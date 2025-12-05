import { api } from './api';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { getPreferredLanguage, getLanguageName } from './uiUtils';

// Track message IDs for which generation is in progress to prevent duplicate requests
const generatingExpansions = new Set<string>();

/**
 * Generates frame expansion suggestions for an LLM response.
 * Creates an interaction with subkind 'eval.frame_expansions' linked to the assistant message.
 * 
 * @param assistantMessageId - ID of the assistant message to analyze (can be local or server ID)
 * @param assistantContent - Content of the assistant's response
 * @param userMessages - Optional array of user messages (up to 2 most recent) that prompted this response
 * @returns Promise that resolves when expansion generation is complete
 */
export async function generateFrameExpansions(
  assistantMessageId: string,
  assistantContent: string,
  userMessages?: string[]
): Promise<void> {
  const genaiChat = useGenaiChatStore.getState();
  const projectId = genaiChat.projectId;
  
  // Resolve server ID from local ID mapping
  const map = (genaiChat as any).persistedByLocalId || {};
  const serverId = map[assistantMessageId] || assistantMessageId;
  
  // Build reverse map for bidirectional lookup
  const reverseMap: Record<string, string[]> = {};
  Object.entries(map).forEach(([local, server]) => {
    if (!reverseMap[server as string]) reverseMap[server as string] = [];
    reverseMap[server as string].push(local);
  });
  // Fix: use serverId to lookup in reverseMap, as reverseMap keys are server IDs
  const localIds = reverseMap[serverId] || [];

  // Check if generation is already in progress for this message (check all known IDs)
  const allIds = [assistantMessageId, serverId, ...localIds];
  if (allIds.some(id => generatingExpansions.has(id))) {
    return;
  }
  
  // Check if expansion interaction already exists
  const existingExpansion = genaiChat.interactions.find((it) => 
    it.subkind === 'eval.frame_expansions' && 
    (it.replyTo === assistantMessageId || it.replyTo === serverId || localIds.includes(it.replyTo || '') ||
     (it.relations && it.relations.some((r) => r.targetId === assistantMessageId || r.targetId === serverId || localIds.includes(r.targetId || ''))))
  );
  
  if (existingExpansion) {
    return;
  }

  // Mark all IDs as generating
  allIds.forEach(id => generatingExpansions.add(id));

  try {
    const settingsState = useGenAISettings.getState();
    
    // Use stance model/provider settings
    const evalModel = (settingsState.stanceModel && settingsState.stanceModel.trim()) 
      ? settingsState.stanceModel 
      : 'gemini_fast';
    const preferredLang = getPreferredLanguage();
    const langName = getLanguageName(preferredLang);

    // Schema for structured response
    const schema = {
      type: 'object',
      additionalProperties: false,
      properties: {
        expansions: {
          type: 'array',
          minItems: 3,
          maxItems: 5,
          items: {
            type: 'object',
            additionalProperties: false,
            properties: {
              question: {
                type: 'string',
                minLength: 10,
                maxLength: 200,
                description: 'Question in 2nd person form suggesting how to expand the frame or view from another angle'
              }
            },
            required: ['question']
          }
        }
      },
      required: ['expansions']
    } as any;

    const schemaText = JSON.stringify(schema, null, 2);

    // Limit context size to prevent 502 errors
    // Truncate assistant content to ~4000 chars (roughly 1000 tokens)
    const MAX_ASSISTANT_CONTENT_LENGTH = 4000;
    const truncatedAssistantContent = assistantContent.length > MAX_ASSISTANT_CONTENT_LENGTH
      ? assistantContent.substring(0, MAX_ASSISTANT_CONTENT_LENGTH) + '\n\n[... content truncated for analysis ...]'
      : assistantContent;

    // Build context block with user messages if available (up to 2 most recent, max 500 chars each)
    const MAX_USER_MESSAGE_LENGTH = 500;
    const userMessagesFiltered = (userMessages?.filter((msg) => msg && msg.trim().length > 0) || [])
      .slice(0, 2) // Limit to 2 most recent
      .map((msg) => msg.length > MAX_USER_MESSAGE_LENGTH 
        ? msg.substring(0, MAX_USER_MESSAGE_LENGTH) + '[...]'
        : msg);
    
    const userContextBlock = userMessagesFiltered.length > 0
      ? `ORIGINAL USER QUESTIONS (context for understanding what prompted this response, chronological order - oldest first):\n${userMessagesFiltered.map((msg, idx) => `${idx + 1}. ${msg}`).join('\n\n')}\n\n`
      : '';

    // Prompt in English (as required)
    const prompt = `You are an expert at analyzing responses and identifying ways to expand their frame or view them from alternative perspectives.

Task:
1. Look at the following LLM response (just received)
2. Assess how much the frame could be expanded or viewed from another angle to complement it
3. Generate 3-5 concise, simple questions suggesting alternative angles or expansions

STYLE REQUIREMENTS:
- Maximum 15-20 words per question
- Use natural, conversational language (like asking a friend)
- One clear idea per question
- Avoid complex nested clauses, long lists, or multiple conditions in one sentence
- Don't use meta-phrases like "expanding the frame" or "alternative perspectives" - just suggest the alternative directly

QUESTION FORMAT:
- Use simple question structures: "What about...?", "Have you considered...?", "Would [specific suggestion] work?"
- Keep it brief and direct - let the suggestion itself convey the alternative perspective
- If mentioning examples, limit to 1-2 items maximum
- Write as if speaking naturally, not formally

AVOID:
- Long sentences with multiple clauses (e.g., "Хотели бы вы рассмотреть расширение рамок ответа, добавив варианты с акцентом на...")
- Explaining reasons or conditions within the question
- Formal language or meta-commentary about the expansion process
- Questions longer than 20 words
- Complex nested structures with multiple "such as", "where", "which", "that" clauses

CRITICAL REQUIREMENTS:
- Avoid superficial suggestions that might be driven solely by the volume of training data or generic patterns
- Focus on meaningful, context-specific expansions that genuinely add depth or alternative perspectives
- Each suggestion should be substantive and relevant to the specific content and user's original question
- Do not propose expansions that are obvious, trivial, or could apply to any response regardless of content

EXAMPLES OF GOOD QUESTIONS (concise, direct):
- "What about eco-friendly destinations like Malaysia or Vietnam?"
- "Have you considered combining Thailand with Cambodia for cultural tours?"
- "Would educational activities for children in warm countries work?"

BAD EXAMPLE (too complex - avoid this style):
- "Хотели бы вы рассмотреть расширение рамок ответа, добавив варианты с акцентом на экологически чистые направления, такие как Малайзия или Вьетнам, где сочетается тропическая теплота, семейные активности и относительно низкие затраты на транспорт из Казахстана?"

User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.

The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${schemaText}

${userContextBlock}LLM RESPONSE TO ANALYZE:
${truncatedAssistantContent}`;

    const langDirective = `You MUST produce all output strictly in ${langName} language. Use English only for technical terms or code if they are standard industry terms.`;

    const res = await api.genaiGenerate({
      model: evalModel,
      prompt,
      strictJSONEnabled: true,
      jsonSchema: schema,
      maxOutputTokens: 2048,
      webSearchEnabled: false,
      systemInstruction: langDirective,
    });

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
      console.warn('[FrameExpansions] parse failed', pe, { contentPreview: (res?.content || '').slice(0, 200) });
    }

    if (!parsed || typeof parsed !== 'object' || !Array.isArray(parsed.expansions)) {
      console.log('[FrameExpansions] no valid expansions parsed, skipping');
      return;
    }

    // Extract and validate expansions
    const expansions = parsed.expansions
      .filter((e: any) => e && typeof e.question === 'string' && e.question.trim().length > 0)
      .slice(0, 5)
      .map((e: any) => ({ question: e.question.trim() }));

    if (expansions.length < 3) {
      return;
    }

    // Create eval.frame_expansions interaction
    try {
      const evalId = Date.now().toString() + '-eval-frame-expansions';
      const expansionData = { expansions };

      // Add to local state first
      // Fix: use serverId for replyTo/relations if available to ensure consistent linking
      useGenaiChatStore.setState((s) => ({
        interactions: [
          ...s.interactions,
          {
            id: evalId,
            projectId: s.projectId,
            kind: 'system',
            subkind: 'eval.frame_expansions',
            content: JSON.stringify(expansionData),
            contentSanitized: JSON.stringify(expansionData),
            replyTo: serverId, // Use resolved serverId
            relations: [{ type: 'expands', targetId: serverId }], // Use resolved serverId
            createdAt: new Date().toISOString(),
          } as any,
        ],
      } as any));

      // Persist to server (async, don't block UI)
      if (projectId && projectId !== 'local-unfold') {
        (async () => {
          try {
            const map = (useGenaiChatStore.getState() as any).persistedByLocalId || {};
            const serverReplyTo = map[assistantMessageId] || assistantMessageId;

            const payload: any = {
              kind: 'system',
              subkind: 'eval.frame_expansions',
              content: JSON.stringify(expansionData),
              replyTo: serverReplyTo,
              relations: [{ type: 'expands', targetId: serverReplyTo }],
            };

            const res = await api.createProjectInteraction(projectId, payload);

            // Update local interaction with server ID and update replyTo/relations if needed
            useGenaiChatStore.setState((st) => {
              const items = [...st.interactions];
              const idx = items.findIndex((it) => it.id === evalId);
              if (idx >= 0) {
                (items[idx] as any).id = res.id;
                // Update replyTo and relations.targetId to use server ID if they reference assistantMessageId
                if (items[idx].replyTo === assistantMessageId && serverReplyTo !== assistantMessageId) {
                  (items[idx] as any).replyTo = serverReplyTo;
                }
                if (items[idx].relations && Array.isArray(items[idx].relations)) {
                  items[idx].relations = items[idx].relations!.map((r: any) => {
                    if (r.targetId === assistantMessageId && serverReplyTo !== assistantMessageId) {
                      return { ...r, targetId: serverReplyTo };
                    }
                    return r;
                  });
                }
              }

              const newMap = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
              newMap[evalId] = res.id;

              return { interactions: items, persistedByLocalId: newMap } as any;
            });
          } catch (e) {
            console.warn('[FrameExpansions] Failed to persist to server', e);
          }
        })();
      }
    } catch (e) {
      console.warn('[FrameExpansions] Failed to create eval.frame_expansions interaction', e);
    }
  } catch (e) {
    console.warn('[FrameExpansions] Failed to generate expansions', e);
  } finally {
    // Always remove from generating set
    allIds.forEach(id => generatingExpansions.delete(id));
  }
}

