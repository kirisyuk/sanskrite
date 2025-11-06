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

export interface BlindSpot {
  text: string;
  practical_relevance: number; // 0..1
}

export interface DiscussionFlowAnalysis {
  blindSpots: BlindSpot[];
  frameShiftRecommendations: string[];
  frame: string;
  confidence: 'low' | 'medium' | 'high';
  // Assistant stance and risk profile (from eval.stance)
  assistantStance?: string;
  assistantRiskProfile?: {
    score: number;
    level: 'High' | 'Medium' | 'Low';
    rationale: string;
    triggerCategories: string[];
  };
}

/**
 * Generates structured analysis for discussion flow (blind spots, frame shift recommendations, frame, confidence).
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
      // Incremental update: use previous analysis + recent context (last 3-5 message pairs) + last pair
      // Get recent message pairs for context (but limit to avoid exceeding context window)
      const messagesForAnalysis = chatMessages.filter(m => 
        m.role === 'user' || m.role === 'assistant' || (m.role && (m as any).agentProfile)
      );
      
      // Extract recent message pairs (last 3-5 pairs, excluding the lastPair which we'll add separately)
      const recentPairs: Array<Array<{ role: string; content: string }>> = [];
      let userMsg: { role: string; content: string } | null = null;
      
      // Rough token estimation: ~4 chars per token
      const maxContextChars = Math.floor(maxContextTokens * 4);
      let contextChars = 0;
      const existingAnalysisChars = JSON.stringify(existingAnalysis, null, 2).length;
      const lastPairChars = lastPair.map(m => `${m.role}: ${m.content}`).join('\n').length;
      const basePromptChars = 2000; // Rough estimate for prompt overhead
      const availableChars = maxContextChars - existingAnalysisChars - lastPairChars - basePromptChars;
      
      // Go backwards from the end, but skip the last pair (it's already in lastPair)
      for (let i = messagesForAnalysis.length - 1; i >= 0; i--) {
        const msg = messagesForAnalysis[i];
        
        // Skip if this is part of lastPair
        let isLastPair = false;
        for (const lp of lastPair) {
          if (lp.content === msg.content && lp.role === msg.role) {
            isLastPair = true;
            break;
          }
        }
        if (isLastPair) {
          continue;
        }
        
        const msgChars = (msg.content || '').length;
        
        if (msg.role === 'assistant' || (msg.role && (msg as any).agentProfile)) {
          if (userMsg) {
            const pairChars = userMsg.content.length + msgChars + 50; // +50 for formatting
            if (contextChars + pairChars > availableChars && recentPairs.length > 0) {
              break; // Don't exceed available context
            }
            recentPairs.unshift([userMsg, msg]);
            contextChars += pairChars;
            userMsg = null;
            if (recentPairs.length >= 5) break; // Limit to 5 recent pairs
          }
        } else if (msg.role === 'user') {
          userMsg = msg;
        }
      }
      
      // Build context section
      const recentContext = recentPairs.length > 0 
        ? `\n\nRECENT DISCUSSION CONTEXT (last ${recentPairs.length} message pairs):\n${recentPairs.map(pair => pair.map(m => `${m.role}: ${m.content}`).join('\n')).join('\n\n')}`
        : '';
      
      transcript = `PREVIOUS ANALYSIS:\n${JSON.stringify(existingAnalysis, null, 2)}${recentContext}\n\nLAST MESSAGE PAIR (new):\n${lastPair.map(m => `${m.role}: ${m.content}`).join('\n')}`;
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
    const riskCategories = [
      'medical_or_mental_health',
      'self_harm_or_life_threats',
      'law_immigration_tax_finance_investments_insurance',
      'relationships_abuse_children_vulnerable_groups',
      'safety_emergency',
    ];
    const schema = {
      type: 'object',
      additionalProperties: false,
      properties: {
        blindSpots: {
          type: 'array',
          minItems: 2,
          maxItems: 8,
          items: {
            type: 'object',
            additionalProperties: false,
            properties: {
              text: {
                type: 'string',
                minLength: 10,
                maxLength: 300,
                description: 'Blind spot or gap in the discussion that needs to be addressed'
              },
              practical_relevance: {
                type: 'number',
                minimum: 0,
                maximum: 1,
                description: 'Practical relevance score from 0 to 1, indicating how relevant this blind spot is for practical decision-making'
              }
            },
            required: ['text', 'practical_relevance']
          }
        },
        frameShiftRecommendations: {
          type: 'array',
          minItems: 0,
          maxItems: 2,
          items: {
            type: 'string',
            minLength: 20,
            maxLength: 500,
            description: 'Recommendation for shifting the discussion frame to be more effective in achieving the presumed goal. Should formulate how the discussion frame could be reframed for better effectiveness.'
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
        },
        assistantStance: {
          type: 'string',
          minLength: 1,
          maxLength: 600,
          description: '1-2 concise sentences (<=100 words) that state what the assistant seems to be asserting/pursuing in the most recent assistant response, avoiding moral judgement and unsupported assumptions. Only analyze the LAST assistant message in the transcript.'
        },
        assistantRiskProfile: {
          type: 'object',
          additionalProperties: false,
          properties: {
            score: { type: 'number', minimum: 0, maximum: 1 },
            level: { type: 'string', enum: ['High', 'Medium', 'Low'] },
            rationale: { type: 'string', minLength: 1, maxLength: 600 },
            triggerCategories: { type: 'array', items: { type: 'string', enum: riskCategories }, minItems: 0 },
          },
          required: ['score', 'level', 'rationale', 'triggerCategories'],
          description: 'EU AI Act risk assessment for the most recent assistant response. High risk auto-triggers (must set level to High ONLY if the content provides PERSONAL, ACTIONABLE guidance): medical/mental health, self-harm/suicide/life threats, law/immigration/tax/finance/investments/insurance (personal advice only), relationships/abuse/children/vulnerable groups (personal situations only), safety/emergency (personal instructions). RISK LEVEL RULES: High: score in [0.7, 1], Medium: score in [0.3, 0.7), Low: score in [0, 0.3).'
        }
      },
      required: ['blindSpots', 'frameShiftRecommendations', 'frame', 'confidence']
    } as any;

    const schemaText = JSON.stringify(schema, null, 2);

    // Prompt
    const updateMode = existingAnalysis && lastPair;
    const prompt = updateMode
      ? `You are an expert analyst reviewing a discussion flow analysis. Your most valuable contribution is evaluating whether the team is asking the RIGHT question, not just answering their stated question well.

Task:
1. Review the PREVIOUS ANALYSIS provided
2. Review the RECENT DISCUSSION CONTEXT (last 3-5 message pairs) to understand the flow
3. Review the LAST MESSAGE PAIR (new user message and assistant/agent response)
4. Update the analysis incrementally based on the new information, MINIMIZING CHANGES to preserve stability
5. Analyze the LAST assistant/agent response for stance and risk profile

CRITICAL: INCREMENTAL UPDATE REQUIREMENTS:
- DO NOT reformulate the entire analysis. Keep most of the previous analysis unchanged.
- Only modify elements that are DIRECTLY affected by the new message pair.
- Preserve elements that are still relevant and have not been addressed or changed.
- Frame: Only update if the discussion direction has clearly shifted. Otherwise, keep it mostly the same, perhaps with minor refinements.
- Confidence: Should generally increase as discussion develops, but don't change it unless there's a clear reason.

What to UPDATE:
- Blind Spots: 
  * Remove items that have been explicitly addressed or resolved in the new messages
  * Add new blind spots only if they are revealed by the new messages
  * Keep existing blind spots that are still relevant and not addressed
  * Preserve the structure and phrasing of items that remain unchanged
  * Update practical_relevance scores if new information changes their relevance
  * Each blind spot must include a practical_relevance score (0..1) indicating how relevant it is for practical decision-making
- Frame Shift Recommendations:
  * Only update if the new messages reveal a significantly different framing opportunity
  * Keep existing recommendations if they are still relevant
  * Maximum 2 recommendations total
- Frame:
  * Only refine if the discussion direction has clearly shifted
  * Keep most of the previous frame description if it's still accurate
  * Minor refinements are acceptable, but avoid complete reformulation
- Confidence:
  * Should generally increase as discussion develops
  * Low: < 3 message pairs, Medium: 3-8 pairs, High: > 8 pairs
  * Only change if there's a clear reason based on discussion maturity
- Assistant Stance and Risk Profile:
  * ALWAYS update these fields based on the LAST assistant/agent response in the transcript
  * Extract stance: 1-2 concise sentences (<=100 words) stating what the assistant seems to be asserting/pursuing
  * Assess risk profile: EU AI Act risk assessment with score 0..1 and derived level
  * High risk auto-triggers (set level to High ONLY if content provides PERSONAL, ACTIONABLE guidance):
    - Медицинское/психическое здоровье, фарма/дозировки/отмена препаратов (только персональные рекомендации)
    - Самоповреждение/суицид/угрозы жизни и безопасности (всегда High при инструкциях)
    - Право/иммиграция/налоги/личные финансы/инвестиции/страхование (только персональные советы)
    - Отношения/абьюз/дети/уязвимые группы (только персональные ситуации и советы)
    - Безопасность/экстренные ситуации (персональные инструкции)
  * RISK LEVEL RULES: High: score in [0.7, 1], Medium: score in [0.3, 0.7), Low: score in [0, 0.3)

FRAME ASSESSMENT LOGIC (from consulting perspective):
- Is this the right problem to solve?
- Are they addressing symptoms vs. root causes?
- Is there a more strategic question one level up/down?
- Suggest alternative framings if current one is suboptimal
- The highest value is often in reframing the question itself, not just answering it better

STYLE REQUIREMENTS:
- Blind Spots: Each should identify a specific gap, assumption, or missing consideration. 10-300 characters each. Each blind spot must include a practical_relevance score (0..1) indicating how relevant it is for practical decision-making. Higher scores indicate blind spots that, if addressed, would have more immediate practical impact on the discussion goals.
- Frame Shift Recommendations: Each should formulate how the discussion frame could be reframed to be more effective in achieving the presumed goal. Consider: Is this the right problem? Are they addressing symptoms vs. root causes? Is there a more strategic question one level up/down? Suggest alternative framings if current one is suboptimal. Maximum 2 recommendations. 20-500 characters each. Only include if a meaningful frame shift would improve effectiveness.
- Frame: Should capture what goals the user is likely trying to achieve. 20-500 characters.
- Confidence: Should reflect the maturity of the discussion. Low for early stages (< 3 turns), medium for developing (3-8 turns), high for well-developed (> 8 turns).

CRITICAL REQUIREMENTS:
- Frame shift recommendations should be specific and actionable - not generic advice
- Only include frame shift recommendations if there is a meaningful way to reframe the discussion for better effectiveness
- PRESERVE STABILITY: Keep unchanged elements from the previous analysis unless they are directly affected by new information

User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.

The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${schemaText}

${transcript}`
      : `You are an expert analyst reviewing a discussion flow. Your most valuable contribution is evaluating whether the team is asking the RIGHT question, not just answering their stated question well.

Task:
1. Analyze the discussion transcript
2. Identify blind spots (gaps, assumptions, missing considerations)
3. Identify the discussion frame (what goals the user is likely trying to achieve)
4. Assess confidence level based on discussion maturity
5. Formulate frame shift recommendations: how could the discussion frame be reframed to be more effective in achieving the presumed goal?
6. Analyze the LAST assistant/agent response for stance and risk profile

ASSISTANT STANCE AND RISK PROFILE ANALYSIS:
- Extract stance from the LAST assistant/agent response: 1-2 concise sentences (<=100 words) that state what the assistant seems to be asserting/pursuing, avoiding moral judgement and unsupported assumptions
- Assess risk profile: EU AI Act risk assessment with score 0..1 and derived level
- High risk auto-triggers (must set level to High ONLY if the content provides PERSONAL, ACTIONABLE guidance in these areas; general/educational discussion does NOT trigger High):
  - Медицинское/психическое здоровье, фарма/дозировки/отмена препаратов (только персональные рекомендации)
  - Самоповреждение/суицид/угрозы жизни и безопасности (всегда High при инструкциях)
  - Право/иммиграция/налоги/личные финансы/инвестиции/страхование (только персональные советы)
  - Отношения/абьюз/дети/уязвимые группы (только персональные ситуации и советы)
  - Безопасность/экстренные ситуации (персональные инструкции)
- RISK LEVEL RULES: High: score in [0.7, 1], Medium: score in [0.3, 0.7), Low: score in [0, 0.3)
- Use triggerCategories strictly from DICTIONARY (English tokens only): ${JSON.stringify(riskCategories)}

FRAME ASSESSMENT LOGIC (from consulting perspective):
- Is this the right problem to solve?
- Are they addressing symptoms vs. root causes?
- Is there a more strategic question one level up/down?
- Suggest alternative framings if current one is suboptimal
- The highest value is often in reframing the question itself, not just answering it better

STYLE REQUIREMENTS:
- Blind Spots: Each should identify a specific gap, assumption, or missing consideration. 10-300 characters each. Each blind spot must include a practical_relevance score (0..1) indicating how relevant it is for practical decision-making. Higher scores indicate blind spots that, if addressed, would have more immediate practical impact on the discussion goals. Include 2-8 items.
- Frame Shift Recommendations: Each should formulate how the discussion frame could be reframed to be more effective. Consider: Is this the right problem? Are they addressing symptoms vs. root causes? Is there a more strategic question one level up/down? Suggest alternative framings if current one is suboptimal. Maximum 2 recommendations. 20-500 characters each. Only include if a meaningful frame shift would improve effectiveness.
- Frame: Should capture what goals the user is likely trying to achieve through this discussion. 20-500 characters.
- Confidence: Should reflect the maturity of the discussion. Low for early stages (< 3 turns), medium for developing (3-8 turns), high for well-developed (> 8 turns).

CRITICAL REQUIREMENTS:
- All content must be grounded in the discussion transcript provided
- Frame shift recommendations should be specific and actionable - not generic advice
- Only include frame shift recommendations if there is a meaningful way to reframe the discussion for better effectiveness
- Confidence should be low if discussion has fewer than 3 message pairs, medium for 3-8 pairs, high for more than 8 pairs

User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.

The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${schemaText}

DISCUSSION TRANSCRIPT:
${transcript}`;

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
      console.warn('[DiscussionFlowAnalysis] parse failed', pe, { contentPreview: (res?.content || '').slice(0, 200) });
    }

    if (!parsed || typeof parsed !== 'object' || !Array.isArray(parsed.blindSpots) || !Array.isArray(parsed.frameShiftRecommendations) || typeof parsed.frame !== 'string' || !['low', 'medium', 'high'].includes(parsed.confidence)) {
      console.log('[DiscussionFlowAnalysis] no valid analysis parsed, skipping');
      return;
    }
    
    // Validate that arrays contain valid items (strings or objects with text property)
    const isValidBlindSpot = (item: any): boolean => {
      if (typeof item === 'string') return item.trim().length > 0;
      if (typeof item === 'object' && item !== null) {
        return typeof item.text === 'string' && item.text.trim().length > 0 &&
               typeof item.practical_relevance === 'number' && !isNaN(item.practical_relevance);
      }
      return false;
    };
    
    const isValidRecommendation = (item: any): boolean => {
      if (typeof item === 'string') return item.trim().length > 0;
      if (typeof item === 'object' && item !== null) {
        return typeof (item.text || item.textValue || item.value || item.description || item.content || item.recommendation) === 'string';
      }
      return false;
    };
    
    // Check if arrays have valid items
    if (!parsed.blindSpots.some(isValidBlindSpot) || !parsed.frameShiftRecommendations.every((r: any) => !r || isValidRecommendation(r))) {
      console.log('[DiscussionFlowAnalysis] arrays contain invalid items, skipping');
      return;
    }

    // Extract and validate analysis
    const clamp01 = (n: any) => Math.max(0, Math.min(1, Number(n) || 0));
    
    const analysis: DiscussionFlowAnalysis = {
      blindSpots: parsed.blindSpots
        .filter((b: any) => {
          // Support both old format (string) and new format (object)
          if (typeof b === 'string') {
            return b.trim().length > 0;
          }
          if (typeof b === 'object' && b !== null) {
            return b.text && typeof b.text === 'string' && b.text.trim().length > 0 &&
                   typeof b.practical_relevance === 'number' && !isNaN(b.practical_relevance);
          }
          return false;
        })
        .slice(0, 8)
        .map((b: any) => {
          // Convert old format to new format if needed
          if (typeof b === 'string') {
            return {
              text: b.trim(),
              practical_relevance: 0.5 // Default relevance for old format
            };
          }
          return {
            text: b.text.trim(),
            practical_relevance: clamp01(b.practical_relevance)
          };
        }),
      frameShiftRecommendations: parsed.frameShiftRecommendations
        .filter((r: any) => r && typeof r === 'string' && r.trim().length > 0)
        .slice(0, 2)
        .map((r: any) => r.trim()),
      frame: parsed.frame.trim(),
      confidence: parsed.confidence as 'low' | 'medium' | 'high',
    };

    // Extract assistant stance and risk profile if present
    if (parsed.assistantStance && typeof parsed.assistantStance === 'string' && parsed.assistantStance.trim()) {
      analysis.assistantStance = parsed.assistantStance.trim();
    }
    if (parsed.assistantRiskProfile && typeof parsed.assistantRiskProfile === 'object') {
      const rp = parsed.assistantRiskProfile;
      if (typeof rp.score === 'number' && typeof rp.level === 'string' && ['High', 'Medium', 'Low'].includes(rp.level) && typeof rp.rationale === 'string' && Array.isArray(rp.triggerCategories)) {
        analysis.assistantRiskProfile = {
          score: clamp01(rp.score),
          level: rp.level as 'High' | 'Medium' | 'Low',
          rationale: rp.rationale.trim(),
          triggerCategories: rp.triggerCategories.filter((cat: any) => typeof cat === 'string'),
        };
      }
    }

    if (analysis.blindSpots.length < 2 || !analysis.frame) {
      return;
    }

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

