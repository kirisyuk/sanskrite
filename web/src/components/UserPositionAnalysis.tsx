import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useAuth } from '../contexts/AuthContext';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { api } from '../lib/api';

// Component to analyze user's position based on their interactions
export const UserPositionAnalysis: React.FC<{ onFirstResult?: () => void; onRiskUpdate?: (r: { level: string; score: number; rationale?: string } | null) => void; hideTitle?: boolean }> = ({ onFirstResult, onRiskUpdate, hideTitle = false }) => {
  const [position, setPosition] = React.useState<string | null>(null);
  const [risk, setRisk] = React.useState<{ level: string; score: number; rationale?: string } | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [durationMs, setDurationMs] = React.useState<number | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const [showRiskRationale, setShowRiskRationale] = React.useState(false);
  // PERFORMANCE FIX: Subscribe only to interactions.length, not entire array
  const interactionsLength = useGenaiChatStore((s) => s.interactions.length);
  const { currentUser } = useAuth();
  
  // Get interactions only when needed via getState()
  const getInteractions = React.useCallback(() => useGenaiChatStore.getState().interactions, []);
  
  const hasAssistant = React.useMemo(
    () => {
      const interactions = getInteractions();
      return interactions.some((it) => it.kind === 'assistant' && (it.contentSanitized || it.content));
    },
    [getInteractions, interactionsLength]
  );
  // Latest user interaction and whether it already has eval.stance
  const lastUserInter = React.useMemo(() => {
    const interactions = getInteractions();
    const ints = [...interactions].filter((it) => it.kind === 'user');
    return ints.length > 0 ? ints[ints.length - 1] : null;
  }, [getInteractions, interactionsLength]);
  const hasEvalForLatestUser = React.useMemo(() => {
    try {
      if (!lastUserInter) return false;
      const interactions = getInteractions();
      return !![...interactions].reverse()
        .find((it) => it.subkind === 'eval.stance' && it.replyTo === lastUserInter.id);
    } catch { return false; }
  }, [getInteractions, interactionsLength, lastUserInter?.id]);
  // Most recent previous stance text (not tied to the current last user)
  const previousStanceText = React.useMemo(() => {
    try {
      const interactions = getInteractions();
      const ints = [...interactions].reverse();
      for (const it of ints) {
        if (it.subkind === 'eval.stance' && (!lastUserInter || it.replyTo !== lastUserInter.id)) {
          return (it.contentSanitized || it.content) as string;
        }
      }
      return null as any;
    } catch { return null as any; }
  }, [getInteractions, interactionsLength, lastUserInter?.id]);
  const notifiedRef = React.useRef(false);
  const generatingStanceRef = React.useRef<Set<string>>(new Set());
  // Read the latest stored stance for the latest USER message (source of truth)
  const stanceForLatestUser = React.useMemo(() => {
    try {
      if (!lastUserInter?.id) return null as any;
      const interactions = getInteractions();
      const ints = [...interactions].reverse();
      const hit = ints.find((it) => it.subkind === 'eval.stance' && it.replyTo === lastUserInter.id);
      return hit || null;
    } catch { return null as any; }
  }, [getInteractions, interactionsLength, lastUserInter?.id]);

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
          ? `RECENT USER MESSAGES (analyze these for stance, roles, and industries):\n${userMessagesText}\n\nLAST ASSISTANT RESPONSE (for context only - do NOT extract facts from this):\n${lastAssistantMessage}`
          : `RECENT USER MESSAGES (analyze these for stance, roles, and industries):\n${userMessagesText}`;

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

        const prevBlock = previousStanceText && previousStanceText.trim().length > 0
          ? `\n\nPREVIOUS_STANCE (for reference, refine if intent shifted):\n${previousStanceText}\n`
          : '';
        const prompt = `In this request, you act as an external expert and must return your assessment STRICTLY following the provided JSON schema. Return ONLY JSON (no prose, no markdown, no code fences).

User requirement: you should create your answer in ${lang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.
All natural-language fields (stance, riskProfile.rationale, roles, industries) must be written strictly in the required language.

CRITICAL: Your analysis MUST be based PRIMARILY on USER MESSAGES only. The assistant's response is provided ONLY for context to understand what the user is responding to or referring to.

IMPORTANT: You are updating the stance based on RECENT conversation only. Use the PREVIOUS_STANCE as context, but update it based on the latest user messages. The assistant response is for context only - do not analyze its content. Do not re-analyze the entire conversation history.

Task: From the USER MESSAGES below, neutrally extract and update:
- stance: 1–2 concise sentences (<=100 words) written DIRECTLY to the user in SECOND PERSON ("you"). Do NOT describe the user in the third person. State what you seem to be pursuing, your perspective and deep intent, avoiding moral judgement and unsupported assumptions. Base this ONLY on what the user expresses, not on assistant content.
- riskProfile: EU AI Act risk assessment with score 0..1 and derived level. If you need to reference the user in rationale, also use SECOND PERSON ("you"). Assess risk based on USER requests and intent, not assistant responses.
- roles: Identify up to 3 professional roles that best describe the user's position in the professional world. A role is WHO the user likely is professionally (e.g., "Software Engineer", "Product Manager", "Marketing Director", "Data Scientist", "UX Designer", "Consultant", "Entrepreneur", "Researcher", "Teacher", "Doctor", "Lawyer", etc.). Each role must include:
  * role: The professional role name (1-100 characters, write in ${lang} language)
  * confidence: Confidence score 0..1 indicating how confident you are that this role matches the user's professional context based on the discussion
  * rationale: Brief explanation (1-300 characters) why this role fits the discussion, written in ${lang} language
  Consider what professional role would be most characteristic of such a discussion. Order by confidence (highest first). Include only roles you can identify with reasonable confidence (confidence >= 0.3).
- industries: Identify up to 3 industries that best describe whose interests the user represents, if they were part of a company, professional community, or other structured organization. An industry is WHAT professional domain/industry the user likely operates in (e.g., "Technology/Software", "Healthcare", "Finance", "Education", "Retail/E-commerce", "Manufacturing", "Consulting", "Media/Entertainment", "Real Estate", "Legal Services", "Non-profit", "Government", etc.). Each industry must include:
  * industry: The industry name (1-100 characters, write in ${lang} language)
  * confidence: Confidence score 0..1 indicating how confident you are that this industry matches the user's professional context based on the discussion
  * rationale: Brief explanation (1-300 characters) why this industry fits the discussion, written in ${lang} language
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
The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${schemaText}

CONVERSATION TRANSCRIPT:
${transcript}
${prevBlock}`;

        const settingsState = useGenAISettings.getState();
        const modelAlias = (settingsState.stanceModel && settingsState.stanceModel.trim()) ? settingsState.stanceModel : 'gemini_fast';
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
          
          // Extract roles and industries
          const rolesRaw = (parsed && typeof parsed === 'object' && Array.isArray((parsed as any).roles)) ? (parsed as any).roles : [];
          const industriesRaw = (parsed && typeof parsed === 'object' && Array.isArray((parsed as any).industries)) ? (parsed as any).industries : [];
          
          const roles = rolesRaw
            .filter((r: any) => r && typeof r === 'object' && typeof r.role === 'string' && r.role.trim() && typeof r.confidence === 'number' && typeof r.rationale === 'string' && r.rationale.trim())
            .slice(0, 3)
            .map((r: any) => ({
              role: r.role.trim(),
              confidence: clamp01(r.confidence),
              rationale: r.rationale.trim(),
            }));
          
          const industries = industriesRaw
            .filter((i: any) => i && typeof i === 'object' && typeof i.industry === 'string' && i.industry.trim() && typeof i.confidence === 'number' && typeof i.rationale === 'string' && i.rationale.trim())
            .slice(0, 3)
            .map((i: any) => ({
              industry: i.industry.trim(),
              confidence: clamp01(i.confidence),
              rationale: i.rationale.trim(),
            }));

          if (stanceText && !cancelled) {
            setPosition(stanceText);
            if (riskLevel && typeof riskScore === 'number') {
              setRisk({ level: riskLevel, score: riskScore, rationale: riskRationale || undefined });
              try { onRiskUpdate && onRiskUpdate({ level: riskLevel, score: riskScore, rationale: riskRationale || undefined }); } catch {}
            } else {
              setRisk(null);
              try { onRiskUpdate && onRiskUpdate(null); } catch {}
            }
            // Create eval.stance interaction linked to the latest USER interaction and persist to server
            if (!cancelled) {
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
                      roles: roles.length > 0 ? roles : undefined,
                      industries: industries.length > 0 ? industries : undefined,
                      language: lang,
                      createdAt: new Date().toISOString(),
                    } as any,
                  ],
                } as any));
                
                // Persist to server
                if (projectId && projectId !== 'local-unfold' && lastUserInter && !cancelled) {
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
                      if (roles.length > 0) payload.roles = roles;
                      if (industries.length > 0) payload.industries = industries;
                      
                      const res = await api.createProjectInteraction(projectId, payload);
                      
                      // Check cancelled before updating state
                      if (!cancelled) {
                        // Update local interaction with server ID
                        useGenaiChatStore.setState((st) => {
                          const items = [...st.interactions];
                          const idx = items.findIndex((it) => it.id === evalId);
                          if (idx >= 0) { (items[idx] as any).id = res.id; }
                          
                          const newMap = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
                          newMap[evalId] = res.id;
                          
                          return { interactions: items, persistedByLocalId: newMap } as any;
                        });
                      }
                    } catch (e) {
                      console.warn('[UserPositionAnalysis] Failed to persist eval.stance to server', e);
                    }
                  })();
                }
              } catch (e) {
                console.warn('[UserPositionAnalysis] Failed to create eval.stance', e);
              }
            }
            if (!cancelled) {
              setDurationMs(Math.max(1, Math.round(finished - started)));
              setError(null);
              if (!notifiedRef.current) {
                notifiedRef.current = true;
                onFirstResult && onFirstResult();
              }
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
  }, [interactionsLength, currentUser?.uid, hasEvalForLatestUser, lastUserInter?.id, getInteractions]);

  // Do not render until есть хотя бы одно сообщение пользователя
  if (!lastUserInter) return null;
  const sanitizeSx = (s: string) => {
    try { return (s || '').replace(/<!--\s*@sx:[\s\S]*?-->/g, '').trim(); } catch { return s; }
      };
  // Display content: prefer stanceForLatestUser, fallback to position, then previousStanceText
  const displayContent = stanceForLatestUser 
    ? (stanceForLatestUser.contentSanitized || stanceForLatestUser.content)
    : position 
      ? position 
      : previousStanceText;
  
  // Display risk: prefer from stanceForLatestUser, fallback to local risk state
  const displayRisk = stanceForLatestUser?.highRisk || risk;
  
  // Display roles and industries: prefer from stanceForLatestUser
  const displayRoles = stanceForLatestUser?.roles || [];
  const displayIndustries = stanceForLatestUser?.industries || [];

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
              </div>
              {showRiskRationale && displayRisk?.rationale && (
                <div className="text-[11px] mt-2" style={{ color: 'var(--text-secondary-strong)' }}>
                  {displayRisk.rationale}
                </div>
              )}
            </div>
          )}
          {(displayRoles.length > 0 || displayIndustries.length > 0) && (
            <div className="mt-3 pt-3" style={{ borderTop: '1px solid var(--overlay-line-90)' }}>
              {displayRoles.length > 0 && (
                <div className="mb-2">
                  <div className="text-[11px] mb-1.5" style={{ color: 'var(--text-tertiary-weak)', fontWeight: 500 }}>
                    Professional roles
                  </div>
                  <div className="space-y-1.5">
                    {displayRoles.map((r: any, idx: number) => {
                      const confidencePercent = Math.round((r.confidence || 0) * 100);
                      return (
                        <div key={idx} className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                          <span style={{ fontWeight: 500 }}>{r.role}</span>
                          <span className="ml-2" style={{ color: 'var(--text-tertiary-weak)' }}>
                            ({confidencePercent}%)
                          </span>
                          {r.rationale && (
                            <div className="mt-0.5 ml-0" style={{ color: 'var(--text-tertiary-weak)', fontSize: '10px' }}>
                              {r.rationale}
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}
              {displayIndustries.length > 0 && (
                <div>
                  <div className="text-[11px] mb-1.5" style={{ color: 'var(--text-tertiary-weak)', fontWeight: 500 }}>
                    Industries
                  </div>
                  <div className="space-y-1.5">
                    {displayIndustries.map((i: any, idx: number) => {
                      const confidencePercent = Math.round((i.confidence || 0) * 100);
                      return (
                        <div key={idx} className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                          <span style={{ fontWeight: 500 }}>{i.industry}</span>
                          <span className="ml-2" style={{ color: 'var(--text-tertiary-weak)' }}>
                            ({confidencePercent}%)
                          </span>
                          {i.rationale && (
                            <div className="mt-0.5 ml-0" style={{ color: 'var(--text-tertiary-weak)', fontSize: '10px' }}>
                              {i.rationale}
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>
          )}
          {typeof durationMs === 'number' && !stanceForLatestUser && (
            <div className="text-[11px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              Time: {durationMs} ms
            </div>
          )}
        </>
      ) : (
        <div className="text-12" style={{ color: 'var(--text-secondary)' }}>No clear position identified.</div>
      )}
    </div>
  );
};

