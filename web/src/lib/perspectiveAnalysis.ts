import { api } from './api';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { getPreferredLanguage, getLanguageName } from './uiUtils';

// Track perspective IDs for which generation is in progress to prevent duplicate requests
const generatingAnalyses = new Set<string>();

export interface PerspectiveAnalysis {
  title: string;
  keyPoints: string[];
  blindSpots: string[];
}

/**
 * Generates structured analysis for a perspective (title, key points, blind spots).
 * Creates an interaction with subkind 'perspective.{preset}.analysis' linked to the perspective.
 * 
 * @param perspectiveId - ID of the perspective interaction (can be local or server ID)
 * @param perspectiveContent - Content of the perspective (markdown text)
 * @param preset - Preset type (e.g., 'consulting', 'themes')
 * @returns Promise that resolves when analysis generation is complete
 */
export async function generatePerspectiveAnalysis(
  perspectiveId: string,
  perspectiveContent: string,
  preset: string
): Promise<void> {
  // Check if generation is already in progress for this perspective
  if (generatingAnalyses.has(perspectiveId)) {
    console.log('[PerspectiveAnalysis] Generation already in progress, skipping', { perspectiveId });
    return;
  }

  const genaiChat = useGenaiChatStore.getState();
  const projectId = genaiChat.projectId;
  
  if (!projectId || projectId === 'local-unfold') {
    console.log('[PerspectiveAnalysis] Skipping for local project');
    return;
  }

  // Resolve server ID from local ID mapping
  const map = (genaiChat as any).persistedByLocalId || {};
  const serverId = map[perspectiveId] || perspectiveId;
  
  // Build reverse map for bidirectional lookup
  const reverseMap: Record<string, string[]> = {};
  Object.entries(map).forEach(([local, server]) => {
    if (!reverseMap[server as string]) reverseMap[server as string] = [];
    reverseMap[server as string].push(local);
  });
  const localIds = reverseMap[perspectiveId] || [];
  
  // Check if analysis interaction already exists
  const analysisSubkind = `perspective.${preset}.analysis`;
  const existingAnalysis = genaiChat.interactions.find((it) => {
    if (it.subkind !== analysisSubkind) return false;
    // Check replyTo
    if (it.replyTo === perspectiveId || it.replyTo === serverId || localIds.includes(it.replyTo || '')) {
      return true;
    }
    // Check relations
    if (it.relations && Array.isArray(it.relations)) {
      return it.relations.some((r: any) => 
        r.targetId === perspectiveId || r.targetId === serverId || localIds.includes(r.targetId || '')
      );
    }
    return false;
  });
  
  if (existingAnalysis) {
    console.log('[PerspectiveAnalysis] Already exists, skipping', { perspectiveId, analysisId: existingAnalysis.id });
    return;
  }

  // Mark as generating
  generatingAnalyses.add(perspectiveId);

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
        title: {
          type: 'string',
          minLength: 5,
          maxLength: 100,
          description: 'Concise title summarizing this perspective (max 100 characters)'
        },
        keyPoints: {
          type: 'array',
          minItems: 3,
          maxItems: 8,
          items: {
            type: 'string',
            minLength: 10,
            maxLength: 200,
            description: 'Concrete, specific action, recommendation, question, or next step explicitly stated anywhere in the perspective text. First check for explicit sections (e.g., "Key Questions", "Next Steps", "Where to Dig Deeper"). If such sections exist, extract from them. If not, scan the entire text for imperative statements, explicit questions, specific next steps, or concrete recommendations. Must be actionable and specific (e.g., "Ask: What is the maximum budget?", "Stop generating new ideas and focus on choosing from three concepts", "Segment criteria separately for 8-year-old and 12-year-old children"). Extract verbatim or closely paraphrased - do not summarize or generalize.'
          }
        },
        blindSpots: {
          type: 'array',
          minItems: 2,
          maxItems: 6,
          items: {
            type: 'string',
            minLength: 10,
            maxLength: 200,
            description: 'Blind spot or gap in the discussion that needs to be addressed'
          }
        }
      },
      required: ['title', 'keyPoints', 'blindSpots']
    } as any;

    const schemaText = JSON.stringify(schema, null, 2);

    // Prompt in preferred language
    const prompt = `You are an expert analyst reviewing a perspective analysis of a discussion.

Task:
1. Review the following perspective analysis
2. Extract a concise title that captures the essence of this perspective
3. Identify 3-8 key points that are direct actions, recommendations, questions, or next steps explicitly stated in the perspective text
4. Identify 2-6 blind spots or gaps in the discussion that this perspective reveals but doesn't fully address

STYLE REQUIREMENTS:
- Title: Concise, specific, max 100 characters. Should capture the unique angle of this perspective.
- Key Points: Extract concrete, specific actions, recommendations, questions, or next steps that are explicitly stated anywhere in the text. First, check for explicit sections like "КЛЮЧЕВЫЕ ВОПРОСЫ" (Key Questions), "Самый важный следующий шаг" (Most Important Next Step), "ГДЕ СТОИТ КОПАТЬ ГЛУБЖЕ" (Where to Dig Deeper), or similar labeled sections. If such sections exist, extract from them. If not, scan the entire text for:
  * Direct imperative statements (e.g., "Провести квалификацию...", "Прекратить генерацию...", "Сфокусироваться на...")
  * Explicit questions formulated as recommendations (e.g., "Задать вопрос: 'Какой бюджет?'")
  * Specific next steps or actions stated directly (e.g., "Следующий шаг: ...", "Необходимо...", "Следует...")
  * Concrete recommendations that can be acted upon (e.g., "Сегментировать критерии для 8-летних и 12-летних детей отдельно")
Each point should be a verbatim or closely paraphrased action/recommendation/question from the text. Do not summarize or generalize - extract the exact actionable items. 10-200 characters each.
- Blind Spots: Each should identify a specific gap, assumption, or missing consideration. 10-200 characters each. These are areas that need resolution or deeper exploration.

CRITICAL REQUIREMENTS:
- All content must be grounded in the perspective text provided
- Key Points must be directly extracted verbatim or closely paraphrased from the text
- Look for explicit recommendations, questions, actions, or next steps throughout the entire text, not just in labeled sections
- Identify actionable items by their structure: imperative verbs, direct questions, explicit recommendations, specific next steps
- Extract the exact wording or close paraphrase - do not summarize, generalize, or create meta-commentary
- Each Key Point should be a concrete, specific item that can be acted upon immediately
- If explicit sections exist, prioritize them; if not, extract from anywhere in the text where concrete actions/recommendations/questions are stated
- Avoid generic advice - only extract what is explicitly stated as a recommendation, question, or action in the text
- Be specific and grounded in the actual text

User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.

The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${schemaText}

PERSPECTIVE ANALYSIS TO REVIEW:
${perspectiveContent}`;

    const langDirective = `You MUST produce all output strictly in ${langName} language. Use English only for technical terms or code if they are standard industry terms.`;

    console.log('[PerspectiveAnalysis] sending structured request', { model: evalModel, promptChars: prompt.length });

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
      console.warn('[PerspectiveAnalysis] parse failed', pe, { contentPreview: (res?.content || '').slice(0, 200) });
    }

    if (!parsed || typeof parsed !== 'object' || typeof parsed.title !== 'string' || !Array.isArray(parsed.keyPoints) || !Array.isArray(parsed.blindSpots)) {
      console.log('[PerspectiveAnalysis] no valid analysis parsed, skipping');
      return;
    }

    // Extract and validate analysis
    const analysis: PerspectiveAnalysis = {
      title: parsed.title.trim(),
      keyPoints: parsed.keyPoints
        .filter((p: any) => p && typeof p === 'string' && p.trim().length > 0)
        .slice(0, 8)
        .map((p: any) => p.trim()),
      blindSpots: parsed.blindSpots
        .filter((b: any) => b && typeof b === 'string' && b.trim().length > 0)
        .slice(0, 6)
        .map((b: any) => b.trim()),
    };

    if (analysis.keyPoints.length < 3 || analysis.blindSpots.length < 2) {
      console.log('[PerspectiveAnalysis] insufficient analysis data', { keyPoints: analysis.keyPoints.length, blindSpots: analysis.blindSpots.length });
      return;
    }

    console.log('[PerspectiveAnalysis] generated analysis', { title: analysis.title, keyPoints: analysis.keyPoints.length, blindSpots: analysis.blindSpots.length, model: evalModel, perspectiveId });

    // Create perspective.{preset}.analysis interaction
    try {
      const analysisId = Date.now().toString() + '-perspective-analysis';
      const analysisData = analysis;

      // Add to local state first
      useGenaiChatStore.setState((s) => ({
        interactions: [
          ...s.interactions,
          {
            id: analysisId,
            projectId: s.projectId,
            kind: 'system',
            subkind: analysisSubkind,
            content: JSON.stringify(analysisData),
            contentSanitized: JSON.stringify(analysisData),
            replyTo: perspectiveId,
            relations: [{ type: 'analyzes', targetId: perspectiveId }],
            createdAt: new Date().toISOString(),
          } as any,
        ],
      } as any));

      // Persist to server (async, don't block UI)
      (async () => {
        try {
          const map = (useGenaiChatStore.getState() as any).persistedByLocalId || {};
          const serverReplyTo = map[perspectiveId] || perspectiveId;

          const payload: any = {
            kind: 'system',
            subkind: analysisSubkind,
            content: JSON.stringify(analysisData),
            replyTo: serverReplyTo,
            relations: [{ type: 'analyzes', targetId: serverReplyTo }],
          };

          const res = await api.createProjectInteraction(projectId, payload);

          // Update local interaction with server ID and update replyTo/relations if needed
          useGenaiChatStore.setState((st) => {
            const items = [...st.interactions];
            const idx = items.findIndex((it) => it.id === analysisId);
            if (idx >= 0) {
              (items[idx] as any).id = res.id;
              // Update replyTo and relations.targetId to use server ID if they reference perspectiveId
              if (items[idx].replyTo === perspectiveId && serverReplyTo !== perspectiveId) {
                (items[idx] as any).replyTo = serverReplyTo;
              }
              if (items[idx].relations && Array.isArray(items[idx].relations)) {
                items[idx].relations = items[idx].relations!.map((r: any) => {
                  if (r.targetId === perspectiveId && serverReplyTo !== perspectiveId) {
                    return { ...r, targetId: serverReplyTo };
                  }
                  return r;
                });
              }
            }

            const newMap = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
            newMap[analysisId] = res.id;

            return { interactions: items, persistedByLocalId: newMap } as any;
          });

          console.log('[PerspectiveAnalysis] persisted to server', { analysisId, serverId: res.id, perspectiveId });
        } catch (e) {
          console.warn('[PerspectiveAnalysis] Failed to persist to server', e);
        }
      })();
    } catch (e) {
      console.warn('[PerspectiveAnalysis] Failed to create analysis interaction', e);
    }
  } catch (e) {
    console.warn('[PerspectiveAnalysis] Failed to generate analysis', e);
  } finally {
    // Always remove from generating set
    generatingAnalyses.delete(perspectiveId);
  }
}

