import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useAuth } from '../contexts/AuthContext';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { api } from '../lib/api';
import {
  generateKeyInsights,
  interpretThinkingDepth,
  interpretTimeHorizon,
  interpretUncertainty,
  interpretDecisionProcess,
  interpretRiskAttitude,
  interpretCriteriaStyle,
  interpretClarity,
  interpretAbstraction,
  interpretContextCompleteness,
  interpretLearningDynamics,
  interpretIntegration,
  interpretMetaAwareness,
  getMetricColor,
  formatPercent
} from '../lib/metaInterpretations';

// Component to analyze user's position based on their interactions
export const UserPositionAnalysis: React.FC<{ onFirstResult?: () => void; onRiskUpdate?: (r: { level: string; score: number; rationale?: string } | null) => void; hideTitle?: boolean }> = ({ onFirstResult, onRiskUpdate, hideTitle = false }) => {
  const [position, setPosition] = React.useState<string | null>(null);
  const [risk, setRisk] = React.useState<{ level: string; score: number; rationale?: string } | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [durationMs, setDurationMs] = React.useState<number | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const [showMetaDetails, setShowMetaDetails] = React.useState(false);
  const [showTechnicalDetails, setShowTechnicalDetails] = React.useState(false);
  const [showRiskRationale, setShowRiskRationale] = React.useState(false);
  const [showWhoLeadsExplanation, setShowWhoLeadsExplanation] = React.useState(false);
  const genaiChat = useGenaiChatStore();
  const { currentUser } = useAuth();
  const hasAssistant = React.useMemo(
    () => genaiChat.interactions.some((it) => it.kind === 'assistant' && (it.contentSanitized || it.content)),
    [genaiChat.interactions]
  );
  // Latest user interaction and whether it already has eval.stance
  const lastUserInter = React.useMemo(() => {
    const ints = [...useGenaiChatStore.getState().interactions].filter((it) => it.kind === 'user');
    return ints.length > 0 ? ints[ints.length - 1] : null;
  }, [genaiChat.interactions.length]);
  const hasEvalForLatestUser = React.useMemo(() => {
    try {
      if (!lastUserInter) return false;
      return !![...useGenaiChatStore.getState().interactions].reverse()
        .find((it) => it.subkind === 'eval.stance' && it.replyTo === lastUserInter.id);
    } catch { return false; }
  }, [genaiChat.interactions.length, lastUserInter?.id]);
  // Most recent previous stance text (not tied to the current last user)
  const previousStanceText = React.useMemo(() => {
    try {
      const ints = [...useGenaiChatStore.getState().interactions].reverse();
      for (const it of ints) {
        if (it.subkind === 'eval.stance' && (!lastUserInter || it.replyTo !== lastUserInter.id)) {
          return (it.contentSanitized || it.content) as string;
        }
      }
      return null as any;
    } catch { return null as any; }
  }, [genaiChat.interactions.length, lastUserInter?.id]);
  const notifiedRef = React.useRef(false);
  const generatingStanceRef = React.useRef<Set<string>>(new Set());
  // Read the latest stored stance for the latest USER message (source of truth)
  const stanceForLatestUser = React.useMemo(() => {
    try {
      if (!lastUserInter?.id) return null as any;
      const ints = [...useGenaiChatStore.getState().interactions].reverse();
      const hit = ints.find((it) => it.subkind === 'eval.stance' && it.replyTo === lastUserInter.id);
      return hit || null;
    } catch { return null as any; }
  }, [genaiChat.interactions.length, lastUserInter?.id]);

  React.useEffect(() => {
    // Skip if stance for the latest USER message already exists
    if (hasEvalForLatestUser) return;
    // Skip if generation is already in progress for this user message
    if (lastUserInter?.id && generatingStanceRef.current.has(lastUserInter.id)) {
      return;
    }
    let cancelled = false;
    const run = async () => {
      // Require authenticated user and at least one USER message
      if (!currentUser?.uid) return;
      if (!lastUserInter?.id) return;
      
      // Mark as generating
      generatingStanceRef.current.add(lastUserInter.id);
      setLoading(true);
      setError(null);
      try {
        const started = performance.now();
        const st = useGenaiChatStore.getState();
        
        // Get recent user interactions (last 2-3 messages)
        const allUserInteractions = st.interactions.filter((it) => it.kind === 'user');
        if (allUserInteractions.length === 0) {
          setError('No user queries to analyze yet.');
          return;
        }
        
        // Get last 2-3 user messages (most recent first, then reverse for chronological order)
        const recentUserMessages = allUserInteractions.slice(-3).map((it) => 
          (it.summary && it.summary.trim()) ? it.summary : it.content
        );
        
        // Get last assistant/LLM message
        const assistantInteractions = st.interactions.filter((it) => it.kind === 'assistant');
        const lastAssistantMessage = assistantInteractions.length > 0 
          ? (assistantInteractions[assistantInteractions.length - 1].contentSanitized || assistantInteractions[assistantInteractions.length - 1].content)
          : null;
        
        // Build compact transcript: recent user messages + last LLM response (for context only)
        const userMessagesText = recentUserMessages.join('\n');
        const transcript = lastAssistantMessage 
          ? `RECENT USER MESSAGES (analyze these for stance and discussionFacts):\n${userMessagesText}\n\nLAST ASSISTANT RESPONSE (for context only - do NOT extract facts from this):\n${lastAssistantMessage}`
          : `RECENT USER MESSAGES (analyze these for stance and discussionFacts):\n${userMessagesText}`;

        // Determine preferred language (fallback to EN)
        const lang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();

        // Strict JSON schema for stance with risk profile
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
            discussionFacts: {
              type: 'object',
              additionalProperties: false,
              properties: {
                dependencies: { type: 'string', maxLength: 500 },
                prerequisites: { type: 'string', maxLength: 500 },
                participants: { type: 'string', maxLength: 500 },
                participantRoles: { type: 'string', maxLength: 500 },
                availableResources: { type: 'string', maxLength: 500 },
                previousAttempts: { type: 'string', maxLength: 500 },
                resultFormat: { type: 'string', maxLength: 500 },
              },
            },
          },
          required: ['stance', 'riskProfile'],
        } as any;
        const schemaText = JSON.stringify(schema, null, 2);
        const categoriesText = JSON.stringify(riskCategories);

        const prevBlock = previousStanceText && previousStanceText.trim().length > 0
          ? `\n\nPREVIOUS_STANCE (for reference, refine if intent shifted):\n${previousStanceText}\n`
          : '';
        const prompt = `In this request, you act as an external expert and must return your assessment STRICTLY following the provided JSON schema. Return ONLY JSON (no prose, no markdown, no code fences).

User requirement: you should create your answer in ${lang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.
All natural-language fields (stance, riskProfile.rationale, discussionFacts) must be written strictly in the required language.

CRITICAL: Your analysis MUST be based PRIMARILY on USER MESSAGES only. The assistant's response is provided ONLY for context to understand what the user is responding to or referring to. Do NOT extract facts from the assistant's content into discussionFacts fields - these fields should reflect what the USER mentions, needs, or describes, not what the assistant discusses.

IMPORTANT: You are updating the stance based on RECENT conversation only. Use the PREVIOUS_STANCE as context, but update it based on the latest user messages. The assistant response is for context only - do not analyze its content. Do not re-analyze the entire conversation history.

Task: From the USER MESSAGES below, neutrally extract and update:
- stance: 1–2 concise sentences (<=100 words) written DIRECTLY to the user in SECOND PERSON ("you"). Do NOT describe the user in the third person. State what you seem to be pursuing, your perspective and deep intent, avoiding moral judgement and unsupported assumptions. Base this ONLY on what the user expresses, not on assistant content.
- riskProfile: EU AI Act risk assessment with score 0..1 and derived level. If you need to reference the user in rationale, also use SECOND PERSON ("you"). Assess risk based on USER requests and intent, not assistant responses.
- discussionFacts: Extract key information that the USER mentions, needs, or describes in their messages. All fields are optional strings. Use empty string or omit if information is not available or cannot be inferred from USER messages. Write in ${lang} language (same as stance). IMPORTANT: Extract only what the USER explicitly or implicitly mentions - do NOT include facts from assistant responses:
  * dependencies: What the USER mentions their decision/action depends on (extract from user messages only, e.g., "approval from management", "weather conditions", "budget allocation")
  * prerequisites: What the USER indicates must happen first (extract from user messages only, e.g., "obtain visa", "complete training", "gather documents")
  * participants: Who the USER mentions is involved (extract from user messages only, e.g., "user and family of 4", "user and colleagues", "user, client, and legal advisor")
  * participantRoles: Roles and relationships the USER describes (extract from user messages only, e.g., "user is parent, children are 8 and 12 years old", "user is project manager, team members are developers")
  * availableResources: Resources the USER mentions having or needing (extract from user messages only, e.g., "budget of $5000", "2 weeks available", "access to library database")
  * previousAttempts: Previous attempts or experience the USER mentions (extract from user messages only, e.g., "user tried similar approach last year", "first time considering this option")
  * resultFormat: Format or type of result the USER requests or expects (extract from user messages only, e.g., "detailed travel itinerary", "comparison table", "step-by-step guide", "formal document")
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
The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${schemaText}

CONVERSATION TRANSCRIPT:
${transcript}
${prevBlock}`;

        const settingsState = useGenAISettings.getState();
        const modelAlias = (settingsState.stanceModel && settingsState.stanceModel.trim()) ? settingsState.stanceModel : 'gemini_fast';
        console.log('[UserPositionAnalysis] sending structured stance', { model: modelAlias, promptChars: prompt.length });
        const response = await api.genaiGenerate({
          model: modelAlias,
          prompt,
          strictJSONEnabled: true,
          jsonSchema: schema,
          maxOutputTokens: Math.max(2048, 4096),
          webSearchEnabled: true,
          toolCallingEnabled: false,
          temperature: 0.2,
        });
        const finished = performance.now();

        if (!cancelled) {
          try {
            const rawJSON = (response as any)?.rawJSON;
            if (typeof rawJSON === 'string' && rawJSON.trim().length > 0) {
              console.log('[UserPositionAnalysis] raw model JSON', { model: modelAlias, rawJSON });
            } else {
              console.log('[UserPositionAnalysis] raw model content', { model: modelAlias, content: response?.content });
            }
          } catch {}

          let parsed: any = null;
          try {
            if (typeof response?.content === 'string') {
              const s = response.content.trim();
              if (s.startsWith('{') || s.startsWith('[')) parsed = JSON.parse(s);
            }
            const rawJSON = (response as any)?.rawJSON;
            if (!parsed && typeof rawJSON === 'string') {
              const s2 = rawJSON.trim();
              if (s2.startsWith('{') || s2.startsWith('[')) parsed = JSON.parse(s2);
            }
            if (!parsed && typeof response?.content === 'string') {
              const m = response.content.match(/\{[\s\S]*\}/);
              if (m && m[0]) parsed = JSON.parse(m[0]);
            }
          } catch {}

          const stanceText = (parsed && typeof parsed === 'object' && typeof parsed.stance === 'string') ? parsed.stance.trim() : '';
          // Extract temporary risk info for display
          const clamp01 = (n: any) => Math.max(0, Math.min(1, Number(n) || 0));
          const rp = (parsed && typeof parsed === 'object') ? (parsed as any).riskProfile : null;
          const riskLevel = rp && typeof rp.level === 'string' ? rp.level : null;
          const riskScore = rp && typeof rp.score !== 'undefined' ? clamp01(rp.score) : null;
          const riskRationale = rp && typeof rp.rationale === 'string' ? rp.rationale : null;
          
          // Extract discussion facts
          const df = (parsed && typeof parsed === 'object') ? (parsed as any).discussionFacts : null;
          const discussionFacts = df && typeof df === 'object' ? {
            dependencies: (typeof df.dependencies === 'string' && df.dependencies.trim()) ? df.dependencies.trim() : undefined,
            prerequisites: (typeof df.prerequisites === 'string' && df.prerequisites.trim()) ? df.prerequisites.trim() : undefined,
            participants: (typeof df.participants === 'string' && df.participants.trim()) ? df.participants.trim() : undefined,
            participantRoles: (typeof df.participantRoles === 'string' && df.participantRoles.trim()) ? df.participantRoles.trim() : undefined,
            availableResources: (typeof df.availableResources === 'string' && df.availableResources.trim()) ? df.availableResources.trim() : undefined,
            previousAttempts: (typeof df.previousAttempts === 'string' && df.previousAttempts.trim()) ? df.previousAttempts.trim() : undefined,
            resultFormat: (typeof df.resultFormat === 'string' && df.resultFormat.trim()) ? df.resultFormat.trim() : undefined,
          } : undefined;

          if (stanceText) {
            setPosition(stanceText);
            if (riskLevel && typeof riskScore === 'number') {
              setRisk({ level: riskLevel, score: riskScore, rationale: riskRationale || undefined });
              try { onRiskUpdate && onRiskUpdate({ level: riskLevel, score: riskScore, rationale: riskRationale || undefined }); } catch {}
            } else {
              setRisk(null);
              try { onRiskUpdate && onRiskUpdate(null); } catch {}
            }
            // Create eval.stance interaction linked to the latest USER interaction and persist to server
            try {
              const stAll = useGenaiChatStore.getState();
              const evalId = Date.now().toString() + '-eval-stance-u';
              const projectId = stAll.projectId;
              const risk = (riskLevel && typeof riskScore === 'number') ? { level: riskLevel as any, score: riskScore, rationale: riskRationale || undefined } : undefined;
              
              // Add to local state
              useGenaiChatStore.setState((s) => ({
                interactions: [
                  ...s.interactions,
                  {
                    id: evalId,
                    projectId: s.projectId,
                    kind: 'system',
                    subkind: 'eval.stance',
                    content: stanceText,
                    contentSanitized: stanceText,
                    replyTo: lastUserInter ? lastUserInter.id : undefined,
                    relations: lastUserInter ? [{ type: 'evaluates', targetId: lastUserInter.id }] : undefined,
                    highRisk: risk,
                    discussionFacts: discussionFacts,
                    language: lang,
                    createdAt: new Date().toISOString(),
                  } as any,
                ],
              } as any));
              
              // Persist to server
              if (projectId && projectId !== 'local-unfold' && lastUserInter) {
                (async () => {
                  try {
                    const map = (useGenaiChatStore.getState() as any).persistedByLocalId || {};
                    const serverReplyTo = map[lastUserInter.id] || lastUserInter.id;
                    
                    const payload: any = {
                      kind: 'system',
                      subkind: 'eval.stance',
                      content: stanceText,
                      replyTo: serverReplyTo,
                      relations: [{ type: 'evaluates', targetId: serverReplyTo }],
                      language: lang,
                    };
                    if (risk) payload.highRisk = risk;
                    if (discussionFacts) payload.discussionFacts = discussionFacts;
                    
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
                    
                    console.log('[UserPositionAnalysis] eval.stance persisted to server', { evalId, serverId: res.id });
                  } catch (e) {
                    console.warn('[UserPositionAnalysis] Failed to persist eval.stance to server', e);
                  }
                })();
              }
            } catch (e) {
              console.warn('[UserPositionAnalysis] Failed to create eval.stance', e);
            }
            setDurationMs(Math.max(1, Math.round(finished - started)));
            setError(null);
            if (!notifiedRef.current) {
              notifiedRef.current = true;
              onFirstResult && onFirstResult();
            }
          } else {
            setError('The model returned an empty result.');
            setRisk(null);
            try { onRiskUpdate && onRiskUpdate(null); } catch {}
          }
        }
      } catch (err: any) {
        console.error('[UserPositionAnalysis] Error:', err);
        if (!cancelled) setError(err?.message || 'Failed to analyze position.');
      } finally {
        if (!cancelled) setLoading(false);
        // Always remove from generating set
        if (lastUserInter?.id) {
          generatingStanceRef.current.delete(lastUserInter.id);
        }
      }
    };
    run();
    return () => { cancelled = true; };
  }, [genaiChat.interactions, currentUser?.uid, hasEvalForLatestUser, lastUserInter?.id]);

  // Do not render until есть хотя бы одно сообщение пользователя
  if (!lastUserInter) return null;
  const sanitizeSx = (s: string) => {
    try { return (s || '').replace(/<!--\s*@sx:[\s\S]*?-->/g, '').trim(); } catch { return s; }
  };
  const agg = genaiChat.stanceAggregate;

  // Display content: prefer stanceForLatestUser, fallback to position, then previousStanceText
  const displayContent = stanceForLatestUser 
    ? (stanceForLatestUser.contentSanitized || stanceForLatestUser.content)
    : position 
      ? position 
      : previousStanceText;
  
  // Display risk: prefer from stanceForLatestUser, fallback to local risk state
  const displayRisk = stanceForLatestUser?.highRisk || risk;
  
  // Display discussion facts: prefer from stanceForLatestUser
  const displayDiscussionFacts = stanceForLatestUser?.discussionFacts;

  return (
    <div className="mt-3 p-3">
      {!hideTitle && (<div className="text-13 mb-2" style={{ color: '#669999' }}>How you engage</div>)}
      {error ? (
        <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{error}</div>
      ) : displayContent ? (
        <>
          <div className="text-12 fade-in" style={{ color: 'var(--text-secondary-strong)', lineHeight: '1.5', opacity: loading ? 0.6 : 1, transition: 'opacity 0.2s' }}>
            {sanitizeSx(displayContent as string)}
          </div>
          {displayRisk && (
            <div className="mt-2 mb-2">
              <div className="flex items-center gap-2">
                {(() => {
                  const hr = displayRisk;
                  if (!hr) return null as any;
                  
                  const level = hr.level || 'Low';
                  
                  // Determine badge text and colors based on risk level
                  let badgeText = 'This discussion is safe';
                  let bgColor = 'rgba(34, 197, 94, 0.1)'; // green
                  let borderColor = 'rgba(34, 197, 94, 0.3)';
                  let textColor = 'rgb(34, 197, 94)';
                  
                  if (level === 'High') {
                    badgeText = 'High risk detected';
                    bgColor = 'rgba(239, 68, 68, 0.1)'; // red
                    borderColor = 'rgba(239, 68, 68, 0.3)';
                    textColor = 'rgb(239, 68, 68)';
                  } else if (level === 'Medium') {
                    badgeText = 'Moderate risk detected';
                    bgColor = 'rgba(251, 191, 36, 0.1)'; // yellow/orange
                    borderColor = 'rgba(251, 191, 36, 0.3)';
                    textColor = 'rgb(251, 191, 36)';
                  }
                  
                  const hasRationale = hr.rationale && hr.rationale.trim().length > 0;
                  
                  return (
                    <button
                      type="button"
                      onClick={() => hasRationale && setShowRiskRationale(!showRiskRationale)}
                      className={`inline-flex items-center gap-1 px-2 py-1 rounded text-[11px] font-medium ${hasRationale ? 'cursor-pointer hover:opacity-80 transition-opacity' : ''}`}
                      style={{ 
                        backgroundColor: bgColor,
                        border: `1px solid ${borderColor}`,
                        color: textColor
                      }}
                      disabled={!hasRationale}
                    >
                      {badgeText}
                    </button>
                  );
                })()}
                {agg && typeof agg === 'object' && typeof agg.whoLeads === 'string' && (
                  <button
                    type="button"
                    onClick={() => setShowWhoLeadsExplanation(!showWhoLeadsExplanation)}
                    className="inline-flex items-center gap-1 px-2 py-1 rounded text-[11px] font-medium cursor-pointer hover:opacity-80 transition-opacity"
                    style={{ 
                      backgroundColor: 'rgba(59, 130, 246, 0.1)', // blue
                      border: '1px solid rgba(59, 130, 246, 0.3)',
                      color: 'rgb(59, 130, 246)'
                    }}
                  >
                    {agg.whoLeads === 'User' ? 'You lead' : agg.whoLeads === 'AI' ? 'AI leads' : 'Mixed control'}
                  </button>
                )}
              </div>
              {showRiskRationale && displayRisk?.rationale && (
                <div className="text-[11px] mt-2" style={{ color: 'var(--text-secondary-strong)' }}>
                  {displayRisk.rationale}
                </div>
              )}
              {showWhoLeadsExplanation && agg && typeof agg === 'object' && typeof agg.whoLeads === 'string' && (
                <div className="text-[11px] mt-2" style={{ color: 'var(--text-secondary-strong)' }}>
                  {agg.whoLeads === 'User' 
                    ? 'You, not the AI, are taking the leading role in this discussion.'
                    : agg.whoLeads === 'AI'
                    ? 'The AI is taking the leading role in this discussion, guiding the conversation direction.'
                    : 'You and the AI are sharing control of the conversation, taking turns to lead.'}
                </div>
              )}
            </div>
          )}
          {displayDiscussionFacts && (
            <div className="mt-3 pt-3" style={{ borderTop: '1px solid var(--overlay-line-90)' }}>
              <div className="text-[11px] mb-2" style={{ color: 'var(--text-tertiary-weak)', fontWeight: 500 }}>
                Key facts
              </div>
              <div className="space-y-1.5">
                {displayDiscussionFacts.dependencies && (
                  <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                    <span style={{ color: 'var(--text-tertiary-weak)' }}>Dependencies: </span>
                    {displayDiscussionFacts.dependencies}
                  </div>
                )}
                {displayDiscussionFacts.prerequisites && (
                  <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                    <span style={{ color: 'var(--text-tertiary-weak)' }}>Prerequisites: </span>
                    {displayDiscussionFacts.prerequisites}
                  </div>
                )}
                {displayDiscussionFacts.participants && (
                  <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                    <span style={{ color: 'var(--text-tertiary-weak)' }}>Participants: </span>
                    {displayDiscussionFacts.participants}
                  </div>
                )}
                {displayDiscussionFacts.participantRoles && (
                  <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                    <span style={{ color: 'var(--text-tertiary-weak)' }}>Roles: </span>
                    {displayDiscussionFacts.participantRoles}
                  </div>
                )}
                {displayDiscussionFacts.availableResources && (
                  <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                    <span style={{ color: 'var(--text-tertiary-weak)' }}>Available resources: </span>
                    {displayDiscussionFacts.availableResources}
                  </div>
                )}
                {displayDiscussionFacts.previousAttempts && (
                  <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                    <span style={{ color: 'var(--text-tertiary-weak)' }}>Previous attempts: </span>
                    {displayDiscussionFacts.previousAttempts}
                  </div>
                )}
                {displayDiscussionFacts.resultFormat && (
                  <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                    <span style={{ color: 'var(--text-tertiary-weak)' }}>Result format: </span>
                    {displayDiscussionFacts.resultFormat}
                  </div>
                )}
              </div>
            </div>
          )}
          {typeof durationMs === 'number' && !stanceForLatestUser && (
            <div className="text-[11px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              Time: {durationMs} ms
            </div>
          )}
          {agg && typeof agg === 'object' && (
            <div className="mt-3 pt-3" style={{ borderTop: '1px solid var(--overlay-line-90)' }}>
              <div className="flex items-center justify-between mb-2">
                <div className="text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                  {(() => {
                    const messagesAnalyzed = (agg as any)?.lastAnalyzedMessageCount;
                    const samples = (agg as any)?.samples;
                    const label = typeof messagesAnalyzed === 'number' && messagesAnalyzed > 0
                      ? `Conversation insights (${messagesAnalyzed} messages)`
                      : (typeof samples === 'number' && samples > 0 ? `Conversation insights (${samples} update${samples > 1 ? 's' : ''})` : 'Conversation insights');
                    return label;
                  })()}
                </div>
                <button
                  type="button"
                  className="text-[10px] underline"
                  style={{ color: 'var(--text-secondary-strong)' }}
                  onClick={() => setShowMetaDetails(!showMetaDetails)}
                >
                  {showMetaDetails ? 'Hide' : 'Show'}
                </button>
              </div>
              
              {showMetaDetails && (
                <>
                  {/* Key Insights */}
                  {(() => {
                    try {
                      const insights = generateKeyInsights(agg);
                      if (insights.length === 0) return null;
                      return (
                        <div className="space-y-2 mb-3">
                          {insights.map((insight, idx) => {
                            const iconColor = insight.sentiment === 'positive' ? 'hsl(120, 50%, 50%)' : insight.sentiment === 'negative' ? 'hsl(0, 50%, 50%)' : 'hsl(45, 70%, 55%)';
                            return (
                              <div key={idx} className="flex items-start gap-2">
                                <div className="w-1 h-1 rounded-full mt-1.5" style={{ backgroundColor: iconColor, flexShrink: 0 }} />
                                <div className="flex-1">
                                  <div className="text-[11px]" style={{ color: 'var(--text-heading)' }}>{insight.title}</div>
                                  <div className="text-[10px]" style={{ color: 'var(--text-secondary-strong)' }}>{insight.description}</div>
                                </div>
                              </div>
                            );
                          })}
                        </div>
                      );
                    } catch (e) {
                      console.error('[Meta] Failed to generate insights:', e);
                      return null;
                    }
                  })()}

                  {/* Technical Details Toggle */}
                  <div className="flex items-center justify-between pt-2" style={{ borderTop: '1px solid var(--overlay-line-90)' }}>
                    <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>Technical details</span>
                    <button
                      type="button"
                      className="text-[9px] underline"
                      style={{ color: 'var(--text-secondary-strong)' }}
                      onClick={() => setShowTechnicalDetails(!showTechnicalDetails)}
                    >
                      {showTechnicalDetails ? 'Hide' : 'Show'}
                    </button>
                  </div>

                  {showTechnicalDetails && (
                    <div className="space-y-2 mt-2">
                      {typeof agg.whoLeads === 'string' && (
                        <div className="flex items-center justify-between">
                          <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>Dialog control:</span>
                          <span className="text-[10px] px-2 py-0.5 rounded" style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)' }}>{agg.whoLeads}</span>
                        </div>
                      )}
                      {typeof agg.effectiveness === 'number' && (
                        <div>
                          <div className="flex items-center justify-between mb-1">
                            <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>Effectiveness:</span>
                            <span className="text-[10px]" style={{ color: 'var(--text-secondary-strong)' }}>{formatPercent(agg.effectiveness)}</span>
                          </div>
                          <div style={{ width: '100%', height: 3, backgroundColor: 'var(--overlay-line-90)', borderRadius: 2, overflow: 'hidden' }}>
                            <div style={{ width: formatPercent(agg.effectiveness), height: '100%', backgroundColor: getMetricColor(agg.effectiveness), transition: 'width 0.3s ease' }} />
                          </div>
                        </div>
                      )}
                      {typeof agg.leadershipIndex === 'number' && (
                        <div>
                          <div className="flex items-center justify-between mb-1">
                            <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>Leadership:</span>
                            <span className="text-[10px]" style={{ color: 'var(--text-secondary-strong)' }}>{formatPercent(agg.leadershipIndex)}</span>
                          </div>
                          <div style={{ width: '100%', height: 3, backgroundColor: 'var(--overlay-line-90)', borderRadius: 2, overflow: 'hidden' }}>
                            <div style={{ width: formatPercent(agg.leadershipIndex), height: '100%', backgroundColor: getMetricColor(agg.leadershipIndex) }} />
                          </div>
                        </div>
                      )}
                      {typeof agg.focusRetention === 'number' && (
                        <div>
                          <div className="flex items-center justify-between mb-1">
                            <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>Focus retention:</span>
                            <span className="text-[10px]" style={{ color: 'var(--text-secondary-strong)' }}>{formatPercent(agg.focusRetention)}</span>
                          </div>
                          <div style={{ width: '100%', height: 3, backgroundColor: 'var(--overlay-line-90)', borderRadius: 2, overflow: 'hidden' }}>
                            <div style={{ width: formatPercent(agg.focusRetention), height: '100%', backgroundColor: getMetricColor(agg.focusRetention) }} />
                          </div>
                        </div>
                      )}
                      {typeof agg.structureScore === 'number' && (
                        <div>
                          <div className="flex items-center justify-between mb-1">
                            <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>Structure:</span>
                            <span className="text-[10px]" style={{ color: 'var(--text-secondary-strong)' }}>{formatPercent(agg.structureScore)}</span>
                          </div>
                          <div style={{ width: '100%', height: 3, backgroundColor: 'var(--overlay-line-90)', borderRadius: 2, overflow: 'hidden' }}>
                            <div style={{ width: formatPercent(agg.structureScore), height: '100%', backgroundColor: getMetricColor(agg.structureScore) }} />
                          </div>
                        </div>
                      )}
                      {agg.lastResults && typeof agg.lastResults === 'object' && (() => {
                        try {
                          const res = agg.lastResults as any;
                          const results = Array.isArray(res.results) ? res.results : [];
                          if (results.length === 0) return null;
                          
                          const analyzerDetails = results.map((r: any) => {
                            const name = r.analyzer || r.Analyzer || 'unknown';
                            const resultData = r.result || r.Result || {};
                            return { name, data: resultData };
                          });

                          return (
                            <div className="mt-2 pt-2" style={{ borderTop: '1px solid var(--overlay-line-90)' }}>
                              <div className="text-[9px] mb-2" style={{ color: 'var(--text-tertiary-weak)' }}>
                                Detailed patterns ({analyzerDetails.length} analyzers):
                              </div>
                              <div className="space-y-3">
                                {analyzerDetails.map((a: any, i: number) => {
                                  const data: any = a.data || {};
                                  const interpretations: string[] = [];
                                  
                                  try {
                                    if (a.name === 'thinking') {
                                      if (data.depth?.label) interpretations.push(interpretThinkingDepth(data.depth.label));
                                      if (Array.isArray(data.timeHorizon) && data.timeHorizon.length > 0) {
                                        data.timeHorizon.slice(0, 2).forEach((h: any) => {
                                          if (h?.label) interpretations.push(interpretTimeHorizon(h.label));
                                        });
                                      }
                                      if (data.uncertainty?.label) interpretations.push(interpretUncertainty(data.uncertainty.label));
                                    } else if (a.name === 'decisions') {
                                      if (data.process?.label) interpretations.push(interpretDecisionProcess(data.process.label));
                                      if (data.riskAttitude?.label) interpretations.push(interpretRiskAttitude(data.riskAttitude.label));
                                      if (data.criteriaStyle?.label) interpretations.push(interpretCriteriaStyle(data.criteriaStyle.label));
                                    } else if (a.name === 'comm' || a.name === 'communication' || a.name === 'communicationPatterns') {
                                      if (data.clarity?.label) interpretations.push(interpretClarity(data.clarity.label));
                                      if (data.abstraction?.label) interpretations.push(interpretAbstraction(data.abstraction.label));
                                      if (data.contextCompleteness?.label) interpretations.push(interpretContextCompleteness(data.contextCompleteness.label));
                                    } else if (a.name === 'learning') {
                                      if (data.metaAwareness?.label) interpretations.push(interpretMetaAwareness(data.metaAwareness.label));
                                      if (data.dynamics?.label) interpretations.push(interpretLearningDynamics(data.dynamics.label));
                                      if (data.integration?.label) interpretations.push(interpretIntegration(data.integration.label));
                                    } else if (a.name === 'biases') {
                                      if (Array.isArray(data.biases) && data.biases.length > 0) {
                                        data.biases.slice(0, 3).forEach((b: any) => {
                                          if (b?.label) interpretations.push(`${b.label}${b.intensity ? ` (${formatPercent(b.intensity)})` : ''}`);
                                        });
                                      }
                                    }
                                  } catch {}
                                  
                                  if (interpretations.length === 0) return null;
                                  
                                  const analyzerLabel = a.name === 'thinking' ? 'Thinking' : a.name === 'decisions' ? 'Decision-making' : a.name === 'comm' || a.name === 'communication' ? 'Communication' : a.name === 'learning' ? 'Learning' : a.name === 'biases' ? 'Cognitive biases' : a.name;
                                  
                                  return (
                                    <div key={i}>
                                      <div className="text-[9px] mb-1" style={{ color: 'var(--text-heading)' }}>
                                        {analyzerLabel}
                                      </div>
                                      <div className="space-y-0.5">
                                        {interpretations.map((interp, idx) => (
                                          <div key={idx} className="text-[9px] pl-2" style={{ color: 'var(--text-secondary-strong)' }}>
                                            • {interp}
                                          </div>
                                        ))}
                                      </div>
                                    </div>
                                  );
                                })}
                              </div>
                            </div>
                          );
                        } catch (e) { 
                          console.error('[Meta] Failed to parse lastResults:', e, agg.lastResults);
                          return null; 
                        }
                      })()}
                    </div>
                  )}
                </>
              )}
            </div>
          )}
        </>
      ) : (
        <div className="text-12" style={{ color: 'var(--text-secondary)' }}>No clear position identified.</div>
      )}
    </div>
  );
};

