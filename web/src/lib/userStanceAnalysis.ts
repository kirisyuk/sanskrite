import { api } from './api';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings, getModelContextWindow } from '../stores/genaiSettingsStore';
import { getPreferredLanguage, getLanguageName } from './uiUtils';

// Track generation in progress to prevent duplicate requests
const generatingAnalyses = new Set<string>();

/**
 * Check if analysis generation is in progress for a given project
 */
export function isGeneratingUserStanceAnalysis(projectId: string, messageCount: number): boolean {
  const analysisKey = `${projectId}:${messageCount}`;
  return generatingAnalyses.has(analysisKey);
}

export interface UserStanceAnalysis {
  stance: string;
  riskProfile: {
    score: number;
    level: 'High' | 'Medium' | 'Low';
    rationale: string;
    triggerCategories: string[];
  };
  roles?: Array<{
    role: string;
    confidence: number;
    rationale: string;
  }>;
  industries?: Array<{
    industry: string;
    confidence: number;
    rationale: string;
  }>;
}

/**
 * Generates structured analysis for user stance (position, roles, industries, risk profile).
 * Creates an interaction with subkind 'eval.stance'.
 * 
 * @param projectId - Project ID
 * @param chatMessages - Array of chat messages (user, assistant, agent only)
 * @param existingAnalysis - Previous analysis if exists (for incremental update)
 * @param lastPair - Last pair of messages (user message and assistant/agent response) for incremental update
 * @returns Promise that resolves when analysis generation is complete
 */
export async function generateUserStanceAnalysis(
  projectId: string,
  chatMessages: Array<{ role: string; content: string }>,
  existingAnalysis?: UserStanceAnalysis | null,
  lastPair?: Array<{ role: string; content: string }>
): Promise<void> {
  if (!projectId || projectId === 'local-unfold') {
    console.log('[UserStanceAnalysis] Skipping for local project');
    return;
  }

  // Check if generation is already in progress
  const analysisKey = `${projectId}:${chatMessages.length}`;
  if (generatingAnalyses.has(analysisKey)) {
    return;
  }

  // Check if analysis interaction already exists
  const genaiChat = useGenaiChatStore.getState();
  const analysisSubkind = 'eval.stance';
  const existingAnalysisInteraction = genaiChat.interactions.find((it) => 
    it.subkind === analysisSubkind && it.projectId === projectId && !it.replyTo
  );

  // Only skip if we have existing interaction AND not doing incremental update AND not forcing refresh
  const isRefresh = existingAnalysis === null; // Explicit null means refresh
  const isIncremental = existingAnalysis && lastPair; // Both present means incremental
  
  if (existingAnalysisInteraction && !isIncremental && !isRefresh && existingAnalysis === undefined) {
    console.log('[UserStanceAnalysis] Already exists, skipping', { projectId });
    return;
  }

  // Mark as generating
  generatingAnalyses.add(analysisKey);

  try {
    const settingsState = useGenAISettings.getState();
    
    // Use stance model/provider settings
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
        ? `\n\nRECENT DISCUSSION CONTEXT (last ${recentPairs.length} message pairs):\n${recentPairs.map(pair => `user: ${pair[0].content}\nassistant: ${pair[1].content}`).join('\n\n')}`
        : '';
      
      transcript = `PREVIOUS ANALYSIS:\n${JSON.stringify(existingAnalysis, null, 2)}${recentContext}\n\nLAST MESSAGE PAIR (new):\nuser: ${lastPair[0].content}\nassistant: ${lastPair[1].content}`;
    } else {
      // Full analysis: use all messages (limit to 70% of context)
      const messagesForAnalysis = chatMessages.filter(m => 
        m.role === 'user' || m.role === 'assistant' || (m.role && (m as any).agentProfile)
      );
      
      // Build pairs: user + assistant
      const pairs: Array<Array<{ role: string; content: string }>> = [];
      let userMsg: { role: string; content: string } | null = null;
      
      // Rough token estimation: ~4 chars per token
      let totalChars = 0;
      const maxChars = Math.floor(maxContextTokens * 4);
      
      // Go backwards from the end
      for (let i = messagesForAnalysis.length - 1; i >= 0; i--) {
        const msg = messagesForAnalysis[i];
        const msgChars = (msg.content || '').length;
        
        if (msg.role === 'assistant' || (msg.role && (msg as any).agentProfile)) {
          if (userMsg) {
            const pairChars = userMsg.content.length + msgChars + 50; // +50 for formatting
            if (totalChars + pairChars > maxChars && pairs.length > 0) {
              break;
            }
            pairs.unshift([userMsg, msg]);
            totalChars += pairChars;
            userMsg = null;
          }
        } else if (msg.role === 'user') {
          userMsg = msg;
        }
      }
      
      transcript = pairs.map(pair => `user: ${pair[0].content}\nassistant: ${pair[1].content}`).join('\n\n');
    }

    if (!transcript || transcript.trim().length === 0) {
      console.log('[UserStanceAnalysis] No transcript available');
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
        stance: { type: 'string', minLength: 1, maxLength: 600 },
        riskProfile: {
          type: 'object',
          additionalProperties: false,
          properties: {
            score: { type: 'number', minimum: 0, maximum: 1 },
            level: { type: 'string', enum: ['High', 'Medium', 'Low'] },
            rationale: { type: 'string', minLength: 1, maxLength: 600 },
            triggerCategories: { type: 'array', items: { type: 'string', enum: riskCategories }, minItems: 0 },
          },
          required: ['score', 'level', 'rationale', 'triggerCategories'],
        },
        roles: {
          type: 'array',
          minItems: 0,
          maxItems: 3,
          items: {
            type: 'object',
            additionalProperties: false,
            properties: {
              role: { type: 'string', minLength: 1, maxLength: 100 },
              confidence: { type: 'number', minimum: 0, maximum: 1 },
              rationale: { type: 'string', minLength: 1, maxLength: 300 },
            },
            required: ['role', 'confidence', 'rationale'],
          },
        },
        industries: {
          type: 'array',
          minItems: 0,
          maxItems: 3,
          items: {
            type: 'object',
            additionalProperties: false,
            properties: {
              industry: { type: 'string', minLength: 1, maxLength: 100 },
              confidence: { type: 'number', minimum: 0, maximum: 1 },
              rationale: { type: 'string', minLength: 1, maxLength: 300 },
            },
            required: ['industry', 'confidence', 'rationale'],
          },
        },
      },
      required: ['stance', 'riskProfile'],
    } as any;

    const schemaText = JSON.stringify(schema, null, 2);
    const categoriesText = JSON.stringify(riskCategories);

    // Prompt
    const updateMode = existingAnalysis && lastPair;
    const prompt = updateMode
      ? `You are an expert analyst reviewing a user stance analysis. Your task is to analyze USER MESSAGES to understand the user's position, roles, industries, and risk profile.

Task:
1. Review the PREVIOUS ANALYSIS provided
2. Review the RECENT DISCUSSION CONTEXT (last 3-5 message pairs) to understand how the user responds to assistant messages
3. Review the LAST MESSAGE PAIR (new user message and assistant/agent response)
4. Update the analysis incrementally based on the new information, MINIMIZING CHANGES to preserve stability

CRITICAL: INCREMENTAL UPDATE REQUIREMENTS:
- DO NOT reformulate the entire analysis. Keep most of the previous analysis unchanged.
- Only modify elements that are DIRECTLY affected by the new message pair.
- Preserve elements that are still relevant and have not been addressed or changed.

CRITICAL: ANALYSIS FOCUS:
- Your analysis MUST be based PRIMARILY on USER MESSAGES only.
- Assistant messages are provided ONLY for context to understand:
  * What the user is responding to or referring to
  * How the user reacts to different types of responses
  * The context that shapes the user's questions and statements
- DO NOT extract facts or information from assistant messages - they are for context only.
- Focus on analyzing the USER's intent, position, and behavior patterns.

What to UPDATE:
- Stance: Only update if the user's intent or position has clearly shifted based on new user messages. Otherwise, keep it mostly the same, perhaps with minor refinements.
- Risk Profile: Update based on new USER requests and intent revealed in the new user message. Assess risk based on what the USER seeks, not assistant responses.
- Roles: Only update if new user messages reveal different professional roles. Keep existing roles that are still relevant.
- Industries: Only update if new user messages reveal different industries. Keep existing industries that are still relevant.

User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.
All natural-language fields (stance, riskProfile.rationale, roles, industries) must be written strictly in the required language.

The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${schemaText}

${transcript}`
      : `You are an expert analyst reviewing user interactions. Your task is to analyze USER MESSAGES to understand the user's position, roles, industries, and risk profile.

CRITICAL: ANALYSIS FOCUS:
- Your analysis MUST be based PRIMARILY on USER MESSAGES only.
- Assistant messages are provided ONLY for context to understand:
  * What the user is responding to or referring to
  * How the user reacts to different types of responses
  * The context that shapes the user's questions and statements
- DO NOT extract facts or information from assistant messages - they are for context only.
- Focus on analyzing the USER's intent, position, and behavior patterns.

Task: From the USER MESSAGES below, neutrally extract:
- stance: 1–2 concise sentences (<=100 words) written DIRECTLY to the user in SECOND PERSON ("you"). Do NOT describe the user in the third person. State what you seem to be pursuing, your perspective and deep intent, avoiding moral judgement and unsupported assumptions. Base this ONLY on what the user expresses, not on assistant content.
- riskProfile: EU AI Act risk assessment with score 0..1 and derived level. If you need to reference the user in rationale, also use SECOND PERSON ("you"). Assess risk based on USER requests and intent, not assistant responses.
- roles: Identify up to 3 professional roles that best describe the user's position in the professional world. A role is WHO the user likely is professionally (e.g., "Software Engineer", "Product Manager", "Marketing Director", "Data Scientist", "UX Designer", "Consultant", "Entrepreneur", "Researcher", "Teacher", "Doctor", "Lawyer", etc.). Each role must include:
  * role: The professional role name (1-100 characters, write in ${langName} language)
  * confidence: Confidence score 0..1 indicating how confident you are that this role matches the user's professional context based on the discussion
  * rationale: Brief explanation (1-300 characters) why this role fits the discussion, written in ${langName} language
  Consider what professional role would be most characteristic of such a discussion. Order by confidence (highest first). Include only roles you can identify with reasonable confidence (confidence >= 0.3).
- industries: Identify up to 3 industries that best describe whose interests the user represents, if they were part of a company, professional community, or other structured organization. An industry is WHAT professional domain/industry the user likely operates in (e.g., "Technology/Software", "Healthcare", "Finance", "Education", "Retail/E-commerce", "Manufacturing", "Consulting", "Media/Entertainment", "Real Estate", "Legal Services", "Non-profit", "Government", etc.). Each industry must include:
  * industry: The industry name (1-100 characters, write in ${langName} language)
  * confidence: Confidence score 0..1 indicating how confident you are that this industry matches the user's professional context based on the discussion
  * rationale: Brief explanation (1-300 characters) why this industry fits the discussion, written in ${langName} language
  Consider what industry would be most characteristic of such a discussion. Order by confidence (highest first). Include only industries you can identify with reasonable confidence (confidence >= 0.3).

High risk auto-triggers (must set level to High ONLY if the user seeks PERSONAL, ACTIONABLE advice in these areas; general or educational discussion does NOT trigger High):
- Медицинское/психическое здоровье, фарма/дозировки/отмена препаратов — только персональные рекомендации/советы по здоровью, диагнозам, схемам/дозировкам/отмене; НЕ включает общие сведения, новости фарма/здравполитики, обсуждение индустрии или науки без личных указаний.
- Самоповреждение/суицид/угрозы жизни и безопасности — всегда High при признаках просьбы о помощи/инструкциях.
- Право/иммиграция/налоги/личные финансы/инвестиции/страхование — только персональные советы, инвестиционные рекомендации, налоговое/юридическое планирование; НЕ включает макроэкономику, монетарную/бюджетную политику, институциональные финансы, общие обзоры рынков/экономики.
- Отношения/абьюз/дети/уязвимые группы — только персональные ситуации и советы; НЕ включает абстрактные рассуждения, социологию/культуру без личного контекста.
- Безопасность/экстренные ситуации — персональные инструкции/советы по действиям в ЧС.
General note: classify as High only when guidance could materially impact an individual's health, safety, legal or personal financial outcomes; otherwise lower level.
RISK LEVEL RULES:
- High: score in [0.7, 1]
- Medium: score in [0.3, 0.7)
- Low: score in [0, 0.3)
Use triggerCategories strictly from DICTIONARY (English tokens only):
${categoriesText}

User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.
All natural-language fields (stance, riskProfile.rationale, roles, industries) must be written strictly in the required language.

The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${schemaText}

CONVERSATION TRANSCRIPT (user messages are what you analyze, assistant messages are for context only):
${transcript}`;

    const langDirective = `You MUST produce all output strictly in ${langName} language. Use English only for technical terms or code if they are standard industry terms.`;

    const res = await api.genaiGenerate({
      model: evalModel,
      prompt,
      strictJSONEnabled: true,
      jsonSchema: schema,
      maxOutputTokens: Math.max(2048, 4096),
      webSearchEnabled: true,
      systemInstruction: langDirective,
    });

    // Save raw API response for debugging
    const rawResponse = {
      content: res?.content,
      rawJSON: (res as any)?.rawJSON,
      toolCalls: (res as any)?.toolCalls,
      model: res?.model,
      usage: res?.usage,
    };

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
        if (m && m[0]) parsed = JSON.parse(m[0]);
      }
    } catch (pe) {
      console.warn('[UserStanceAnalysis] parse failed', pe, { contentPreview: (res?.content || '').slice(0, 200) });
    }

    if (!parsed || typeof parsed !== 'object' || typeof parsed.stance !== 'string' || !parsed.riskProfile) {
      console.log('[UserStanceAnalysis] no valid analysis parsed, skipping');
      
      // Save raw response even if parsing failed
      try {
        const analysisId = existingAnalysisInteraction?.id || (Date.now().toString() + '-user-stance-analysis-failed');
        useGenaiChatStore.setState((s) => {
          const items = [...s.interactions];
          const idx = items.findIndex((it) => it.id === analysisId);
          if (idx >= 0) {
            (items[idx] as any).rawResponse = rawResponse;
            (items[idx] as any).parseError = true;
          } else {
            items.push({
              id: analysisId,
              projectId: s.projectId,
              kind: 'system',
              subkind: analysisSubkind,
              content: '',
              contentSanitized: '',
              rawResponse: rawResponse,
              parseError: true,
              createdAt: new Date().toISOString(),
            } as any);
          }
          return { interactions: items } as any;
        });
      } catch (e) {
        console.warn('[UserStanceAnalysis] Failed to save raw response', e);
      }
      
      return;
    }
    
    // Extract and validate analysis
    const clamp01 = (n: any) => Math.max(0, Math.min(1, Number(n) || 0));
    
    const analysis: UserStanceAnalysis = {
      stance: parsed.stance.trim(),
      riskProfile: {
        score: clamp01(parsed.riskProfile.score),
        level: parsed.riskProfile.level as 'High' | 'Medium' | 'Low',
        rationale: parsed.riskProfile.rationale.trim(),
        triggerCategories: (parsed.riskProfile.triggerCategories || []).filter((cat: any) => typeof cat === 'string'),
      },
    };

    // Extract roles if present
    if (parsed.roles && Array.isArray(parsed.roles)) {
      analysis.roles = parsed.roles
        .filter((r: any) => r && typeof r === 'object' && typeof r.role === 'string' && r.role.trim() && typeof r.confidence === 'number' && typeof r.rationale === 'string' && r.rationale.trim())
        .slice(0, 3)
        .map((r: any) => ({
          role: r.role.trim(),
          confidence: clamp01(r.confidence),
          rationale: r.rationale.trim(),
        }));
    }

    // Extract industries if present
    if (parsed.industries && Array.isArray(parsed.industries)) {
      analysis.industries = parsed.industries
        .filter((i: any) => i && typeof i === 'object' && typeof i.industry === 'string' && i.industry.trim() && typeof i.confidence === 'number' && typeof i.rationale === 'string' && i.rationale.trim())
        .slice(0, 3)
        .map((i: any) => ({
          industry: i.industry.trim(),
          confidence: clamp01(i.confidence),
          rationale: i.rationale.trim(),
        }));
    }

    if (!analysis.stance || analysis.stance.trim().length === 0) {
      return;
    }

    // Create or update eval.stance interaction
    try {
      const analysisId = existingAnalysisInteraction?.id || (Date.now().toString() + '-user-stance-analysis');
      const analysisData = analysis;
      
      // Save raw API response for debugging
      const rawResponse = {
        content: res?.content,
        rawJSON: (res as any)?.rawJSON,
        toolCalls: (res as any)?.toolCalls,
        model: res?.model,
        usage: res?.usage,
      };

      if (existingAnalysisInteraction) {
        // Update existing interaction
        useGenaiChatStore.setState((s) => {
          const items = [...s.interactions];
          const idx = items.findIndex((it) => it.id === analysisId);
          if (idx >= 0) {
            (items[idx] as any).content = JSON.stringify(analysisData);
            (items[idx] as any).contentSanitized = analysisData.stance;
            (items[idx] as any).rawResponse = rawResponse;
            (items[idx] as any).highRisk = analysisData.riskProfile;
            (items[idx] as any).roles = analysisData.roles;
            (items[idx] as any).industries = analysisData.industries;
          }
          return { interactions: items } as any;
        });

        // Update on server
        (async () => {
          try {
            await api.updateProjectInteraction(projectId, analysisId, {
              content: JSON.stringify(analysisData),
              contentSanitized: analysisData.stance,
              highRisk: analysisData.riskProfile,
              roles: analysisData.roles,
              industries: analysisData.industries,
            });
          } catch (e) {
            console.warn('[UserStanceAnalysis] Failed to update on server', e);
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
              kind: 'system',
              subkind: analysisSubkind,
              content: JSON.stringify(analysisData),
              contentSanitized: analysisData.stance,
              rawResponse: rawResponse,
              highRisk: analysisData.riskProfile,
              roles: analysisData.roles,
              industries: analysisData.industries,
              language: preferredLang,
              createdAt: new Date().toISOString(),
            } as any,
          ],
        } as any));

        // Persist to server (async, don't block UI)
        (async () => {
          try {
            const payload: any = {
              kind: 'system',
              subkind: analysisSubkind,
              content: JSON.stringify(analysisData),
              contentSanitized: analysisData.stance,
              language: preferredLang,
            };
            if (analysisData.riskProfile) payload.highRisk = analysisData.riskProfile;
            if (analysisData.roles && analysisData.roles.length > 0) payload.roles = analysisData.roles;
            if (analysisData.industries && analysisData.industries.length > 0) payload.industries = analysisData.industries;

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
            console.warn('[UserStanceAnalysis] Failed to persist to server', e);
          }
        })();
      }
    } catch (e) {
      console.warn('[UserStanceAnalysis] Failed to create/update analysis interaction', e);
    }
  } catch (e) {
    console.warn('[UserStanceAnalysis] Failed to generate analysis', e);
  } finally {
    // Always remove from generating set
    generatingAnalyses.delete(analysisKey);
  }
}

