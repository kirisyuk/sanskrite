import { api } from './api';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings, getModelContextWindow } from '../stores/genaiSettingsStore';
import { getPreferredLanguage, getLanguageName } from './uiUtils';

// Track generation in progress to prevent duplicate requests
const generatingAnalyses = new Set<string>();

/**
 * Check if analysis generation is in progress for a given project
 */
export function isGeneratingDiscussionFlowAnalysis(projectId: string, messageCount: number): boolean {
  const analysisKey = `${projectId}:${messageCount}`;
  return generatingAnalyses.has(analysisKey);
}

export interface DiscussionFlowAnalysis {
  blindSpots: string[];
  recommendations: string[];
  frame: string;
  confidence: 'low' | 'medium' | 'high';
}

/**
 * Generates structured analysis for discussion flow (blind spots, recommendations, frame, confidence).
 * Creates an interaction with subkind 'discussion.flow.analysis'.
 * 
 * @param projectId - Project ID
 * @param chatMessages - Array of chat messages (user, assistant, agent only)
 * @param existingAnalysis - Previous analysis if exists (for incremental update)
 * @param lastPair - Last pair of messages (user message and assistant/agent response) for incremental update
 * @returns Promise that resolves when analysis generation is complete
 */
export async function generateDiscussionFlowAnalysis(
  projectId: string,
  chatMessages: Array<{ role: string; content: string }>,
  existingAnalysis?: DiscussionFlowAnalysis | null,
  lastPair?: Array<{ role: string; content: string }>
): Promise<void> {
  if (!projectId || projectId === 'local-unfold') {
    console.log('[DiscussionFlowAnalysis] Skipping for local project');
    return;
  }

  // Check if generation is already in progress
  const analysisKey = `${projectId}:${chatMessages.length}`;
  if (generatingAnalyses.has(analysisKey)) {
    console.log('[DiscussionFlowAnalysis] Generation already in progress, skipping', { analysisKey });
    return;
  }

  // Check if analysis interaction already exists
  const genaiChat = useGenaiChatStore.getState();
  const analysisSubkind = 'discussion.flow.analysis';
  const existingAnalysisInteraction = genaiChat.interactions.find((it) => 
    it.subkind === analysisSubkind && it.projectId === projectId
  );

  if (existingAnalysisInteraction && !existingAnalysis && !lastPair) {
    console.log('[DiscussionFlowAnalysis] Already exists, skipping', { projectId });
    return;
  }

  // Mark as generating
  generatingAnalyses.add(analysisKey);

  try {
    const settingsState = useGenAISettings.getState();
    
    // Use stance model/provider settings (same as Perspectives)
    const evalModel = (settingsState.stanceModel && settingsState.stanceModel.trim()) 
      ? settingsState.stanceModel 
      : 'gemini_fast';
    const preferredLang = getPreferredLanguage();
    const langName = getLanguageName(preferredLang);

    // Get context window and limit to 70%
    const contextWindow = getModelContextWindow(evalModel);
    const maxContextTokens = contextWindow ? Math.floor(contextWindow * 0.7) : Infinity;

    // Build transcript from messages
    let transcript = '';
    if (existingAnalysis && lastPair) {
      // Incremental update: use previous analysis + last pair
      transcript = `PREVIOUS ANALYSIS:\n${JSON.stringify(existingAnalysis, null, 2)}\n\nLAST MESSAGE PAIR:\n${lastPair.map(m => `${m.role}: ${m.content}`).join('\n')}`;
    } else {
      // Full analysis: use all messages (limit to 70% of context)
      const messagesForAnalysis = chatMessages.filter(m => 
        m.role === 'user' || m.role === 'assistant' || (m.role && (m as any).agentProfile)
      );
      
      // Rough token estimation: ~4 chars per token
      let totalChars = 0;
      const maxChars = Math.floor(maxContextTokens * 4);
      const selectedMessages: Array<{ role: string; content: string }> = [];
      
      // Take messages from the end until we hit the limit
      for (let i = messagesForAnalysis.length - 1; i >= 0; i--) {
        const msg = messagesForAnalysis[i];
        const msgChars = (msg.content || '').length;
        if (totalChars + msgChars > maxChars && selectedMessages.length > 0) {
          break;
        }
        selectedMessages.unshift(msg);
        totalChars += msgChars;
      }
      
      transcript = selectedMessages.map(m => `${m.role}: ${m.content}`).join('\n\n');
    }

    if (!transcript || transcript.trim().length === 0) {
      console.log('[DiscussionFlowAnalysis] No transcript available');
      return;
    }

    // Schema for structured response
    const schema = {
      type: 'object',
      additionalProperties: false,
      properties: {
        blindSpots: {
          type: 'array',
          minItems: 2,
          maxItems: 8,
          items: {
            type: 'string',
            minLength: 10,
            maxLength: 300,
            description: 'Blind spot or gap in the discussion that needs to be addressed'
          }
        },
        recommendations: {
          type: 'array',
          minItems: 2,
          maxItems: 8,
          items: {
            type: 'string',
            minLength: 10,
            maxLength: 300,
            description: 'Recommendation for deepening or expanding the discussion'
          }
        },
        frame: {
          type: 'string',
          minLength: 20,
          maxLength: 500,
          description: 'Identification of the discussion frame - what goals the user is likely trying to achieve through this discussion'
        },
        confidence: {
          type: 'string',
          enum: ['low', 'medium', 'high'],
          description: 'Confidence level in the analysis. Should be low on early stages of discussion, medium as more context accumulates, high when discussion is well-developed.'
        }
      },
      required: ['blindSpots', 'recommendations', 'frame', 'confidence']
    } as any;

    const schemaText = JSON.stringify(schema, null, 2);

    // Prompt
    const updateMode = existingAnalysis && lastPair;
    const prompt = updateMode
      ? `You are an expert analyst reviewing a discussion flow analysis.

Task:
1. Review the PREVIOUS ANALYSIS provided
2. Review the LAST MESSAGE PAIR (new user message and assistant/agent response)
3. Update the analysis based on the new information:
   - Update blind spots if new gaps are revealed or old ones are addressed
   - Update recommendations based on the new turn in the discussion
   - Refine the frame identification if the discussion direction has shifted
   - Update confidence level (should increase as discussion develops)

STYLE REQUIREMENTS:
- Blind Spots: Each should identify a specific gap, assumption, or missing consideration. 10-300 characters each.
- Recommendations: Each should be a concrete suggestion for deepening or expanding the discussion. 10-300 characters each.
- Frame: Should capture what goals the user is likely trying to achieve. 20-500 characters.
- Confidence: Should reflect the maturity of the discussion. Low for early stages (< 3 turns), medium for developing (3-8 turns), high for well-developed (> 8 turns).

User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.

The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${schemaText}

${transcript}`
      : `You are an expert analyst reviewing a discussion flow.

Task:
1. Analyze the discussion transcript
2. Identify blind spots (gaps, assumptions, missing considerations)
3. Provide recommendations for deepening or expanding the discussion
4. Identify the discussion frame (what goals the user is likely trying to achieve)
5. Assess confidence level based on discussion maturity

STYLE REQUIREMENTS:
- Blind Spots: Each should identify a specific gap, assumption, or missing consideration. 10-300 characters each. Include 2-8 items.
- Recommendations: Each should be a concrete suggestion for deepening or expanding the discussion. 10-300 characters each. Include 2-8 items.
- Frame: Should capture what goals the user is likely trying to achieve through this discussion. 20-500 characters.
- Confidence: Should reflect the maturity of the discussion. Low for early stages (< 3 turns), medium for developing (3-8 turns), high for well-developed (> 8 turns).

CRITICAL REQUIREMENTS:
- All content must be grounded in the discussion transcript provided
- Be specific and actionable
- Confidence should be low if discussion has fewer than 3 message pairs, medium for 3-8 pairs, high for more than 8 pairs

User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.

The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${schemaText}

DISCUSSION TRANSCRIPT:
${transcript}`;

    const langDirective = `You MUST produce all output strictly in ${langName} language. Use English only for technical terms or code if they are standard industry terms.`;

    console.log('[DiscussionFlowAnalysis] sending structured request', { model: evalModel, promptChars: prompt.length, updateMode });

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
      console.warn('[DiscussionFlowAnalysis] parse failed', pe, { contentPreview: (res?.content || '').slice(0, 200) });
    }

    if (!parsed || typeof parsed !== 'object' || !Array.isArray(parsed.blindSpots) || !Array.isArray(parsed.recommendations) || typeof parsed.frame !== 'string' || !['low', 'medium', 'high'].includes(parsed.confidence)) {
      console.log('[DiscussionFlowAnalysis] no valid analysis parsed, skipping');
      return;
    }

    // Extract and validate analysis
    const analysis: DiscussionFlowAnalysis = {
      blindSpots: parsed.blindSpots
        .filter((b: any) => b && typeof b === 'string' && b.trim().length > 0)
        .slice(0, 8)
        .map((b: any) => b.trim()),
      recommendations: parsed.recommendations
        .filter((r: any) => r && typeof r === 'string' && r.trim().length > 0)
        .slice(0, 8)
        .map((r: any) => r.trim()),
      frame: parsed.frame.trim(),
      confidence: parsed.confidence as 'low' | 'medium' | 'high',
    };

    if (analysis.blindSpots.length < 2 || analysis.recommendations.length < 2 || !analysis.frame) {
      console.log('[DiscussionFlowAnalysis] insufficient analysis data', { blindSpots: analysis.blindSpots.length, recommendations: analysis.recommendations.length, frame: !!analysis.frame });
      return;
    }

    console.log('[DiscussionFlowAnalysis] generated analysis', { blindSpots: analysis.blindSpots.length, recommendations: analysis.recommendations.length, frame: analysis.frame.substring(0, 50), confidence: analysis.confidence, model: evalModel, projectId });

    // Create or update discussion.flow.analysis interaction
    try {
      const analysisId = existingAnalysisInteraction?.id || (Date.now().toString() + '-discussion-flow-analysis');
      const analysisData = analysis;

      if (existingAnalysisInteraction) {
        // Update existing interaction
        useGenaiChatStore.setState((s) => {
          const items = [...s.interactions];
          const idx = items.findIndex((it) => it.id === analysisId);
          if (idx >= 0) {
            (items[idx] as any).content = JSON.stringify(analysisData);
            (items[idx] as any).contentSanitized = JSON.stringify(analysisData);
          }
          return { interactions: items } as any;
        });

        // Update on server
        (async () => {
          try {
            await api.updateProjectInteraction(projectId, analysisId, {
              content: JSON.stringify(analysisData),
              contentSanitized: JSON.stringify(analysisData),
            });
            console.log('[DiscussionFlowAnalysis] updated on server', { analysisId, projectId });
          } catch (e) {
            console.warn('[DiscussionFlowAnalysis] Failed to update on server', e);
          }
        })();
      } else {
        // Create new interaction
        useGenaiChatStore.setState((s) => ({
          interactions: [
            ...s.interactions,
            {
              id: analysisId,
              projectId: s.projectId,
              kind: 'note',
              subkind: analysisSubkind,
              content: JSON.stringify(analysisData),
              contentSanitized: JSON.stringify(analysisData),
              createdAt: new Date().toISOString(),
            } as any,
          ],
        } as any));

        // Persist to server (async, don't block UI)
        (async () => {
          try {
            const payload: any = {
              kind: 'note',
              subkind: analysisSubkind,
              content: JSON.stringify(analysisData),
            };

            const res = await api.createProjectInteraction(projectId, payload);

            // Update local interaction with server ID
            useGenaiChatStore.setState((st) => {
              const items = [...st.interactions];
              const idx = items.findIndex((it) => it.id === analysisId);
              if (idx >= 0) {
                (items[idx] as any).id = res.id;
              }

              const newMap = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
              newMap[analysisId] = res.id;

              return { interactions: items, persistedByLocalId: newMap } as any;
            });

            console.log('[DiscussionFlowAnalysis] persisted to server', { analysisId, serverId: res.id, projectId });
          } catch (e) {
            console.warn('[DiscussionFlowAnalysis] Failed to persist to server', e);
          }
        })();
      }
    } catch (e) {
      console.warn('[DiscussionFlowAnalysis] Failed to create/update analysis interaction', e);
    }
  } catch (e) {
    console.warn('[DiscussionFlowAnalysis] Failed to generate analysis', e);
  } finally {
    // Always remove from generating set
    generatingAnalyses.delete(analysisKey);
  }
}

