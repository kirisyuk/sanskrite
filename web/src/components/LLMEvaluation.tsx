import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useAuth } from '../contexts/AuthContext';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { useTranslation } from 'react-i18next';
import { api } from '../lib/api';
import { evaluateBias } from '../lib/biasEvaluation';

// Component to evaluate quality/style/biases of assistant answers and suggest system prompt improvements
export const LLMEvaluation: React.FC<{ onFirstResult?: () => void }> = ({ onFirstResult }) => {
  const [review, setReview] = React.useState<string | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [durationMs, setDurationMs] = React.useState<number | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  // PERFORMANCE FIX: Subscribe only to lengths, not entire arrays
  const interactionsLength = useGenaiChatStore((s) => s.interactions.length);
  const messagesLength = useGenaiChatStore((s) => s.messages.length);
  const streaming = useGenaiChatStore((s) => s.streaming);
  const persistedByLocalId = useGenaiChatStore((s) => (s as any).persistedByLocalId);
  const { currentUser } = useAuth();
  const { i18n: i18nCore } = useTranslation();
  
  // Get interactions/messages only when needed via getState()
  const getInteractions = React.useCallback(() => useGenaiChatStore.getState().interactions, []);
  const getMessages = React.useCallback(() => useGenaiChatStore.getState().messages, []);
  
  const hasAssistant = React.useMemo(() => {
    const messages = getMessages();
    return messages.some((m) => m.role === 'assistant' && m.content && m.content.trim().length > 0);
  }, [getMessages, messagesLength]);
  const lastAssistant = React.useMemo(() => {
    const interactions = getInteractions();
    const ints = interactions.filter((it) => it.kind === 'assistant' && (it.contentSanitized || it.content));
    return ints.length > 0 ? ints[ints.length - 1] : null;
  }, [getInteractions, interactionsLength]);
  const lastReviewedId = React.useRef<string | null>(null);
  const evaluatingRef = React.useRef<Set<string>>(new Set());

  React.useEffect(() => {
    let cancelled = false;
    const run = async () => {
      if (!currentUser?.uid) return;
      // Trigger only AFTER streaming completes and a new assistant message is available
      if (streaming) return;
      if (!lastAssistant) return;
      // Skip evaluations for assistant summaries generated without LLM
      try {
        const sk = String((lastAssistant as any).subkind || '').toLowerCase();
        if (sk === 'prestance.summary' || sk === 'analysis.summary') {
          lastReviewedId.current = lastAssistant.id;
          return;
        }
      } catch {}
      if (lastReviewedId.current === lastAssistant.id) return;
      
      // Prevent duplicate evaluation runs (race condition guard)
      if (evaluatingRef.current.has(lastAssistant.id)) {
        console.log('[LLMEvaluation] Already evaluating message', lastAssistant.id, '- skipping duplicate');
        return;
      }
      
      // Check if eval.bias already exists for this assistant message
      try {
        const map = persistedByLocalId || {};
        const serverId = map[lastAssistant.id];
        const reverseMap: Record<string, string[]> = {};
        Object.entries(map).forEach(([local, server]) => {
          if (!reverseMap[server as string]) reverseMap[server as string] = [];
          reverseMap[server as string].push(local);
        });
        const localIds = reverseMap[lastAssistant.id] || [];
        const interactions = getInteractions();
        const existingEvalBias = [...interactions].reverse().find((it: any) => 
          it.subkind === 'eval.bias' && 
          (it.replyTo === lastAssistant.id || (serverId && it.replyTo === serverId) || localIds.includes(it.replyTo || ''))
        );
        if (existingEvalBias) {
          lastReviewedId.current = lastAssistant.id;
          return; // Skip evaluation if it already exists
        }
      } catch (e) {
        console.warn('[LLMEvaluation] Failed to check existing eval.bias', e);
      }
      
      // Mark as evaluating
      evaluatingRef.current.add(lastAssistant.id);
      
      setLoading(true);
      setError(null);
      try {
        const started = performance.now();
        // Prefer local interactions (assistant), fallback to chat messages
        const interactions = getInteractions();
        const assistantInteractions = interactions.filter((it: any) => it.kind === 'assistant');
        let texts: string[] = [];
        if (assistantInteractions.length > 0) {
          const last = assistantInteractions.slice(Math.max(0, assistantInteractions.length - 5));
          texts = last.map((it: any) => (it.summary && it.summary.trim()) ? it.summary : it.content);
        } else {
          const messages = getMessages();
          const all = messages.filter((m) => m.role === 'assistant' && m.content && m.content.trim());
          const last = all.slice(Math.max(0, all.length - 5));
          texts = last.map((m) => m.content);
        }
        if (texts.length === 0) {
          setError('No assistant replies to evaluate yet.');
          return;
        }
        const transcript = texts.join('\n\n---\n\n');
        const lang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'RU') : 'RU').toLowerCase();
        const nowISO = new Date().toISOString();
        const prompt = `User requirement: you should create your answer in ${lang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nYou are an expert LLM output reviewer.
Current date/time (trustworthy, real-time): ${nowISO}.
The ASSISTANT model that produced these replies had access to up-to-date information via web search.
Evaluate the following ASSISTANT responses (most recent last). Provide:
1) Brief quality assessment (strengths/weaknesses)
2) Style characteristics (tone, structure, clarity)
3) Potential biases/safety concerns
4) 3-5 concrete improvements to the SYSTEM INSTRUCTION to improve future answers
5) A short note referencing the five-dimension thoughtfulness rubric used for the structured score (do not include numbers here)
Important: Do not critique formatting or output length; formatting/verbosity are enforced elsewhere.
Assistant responses:
${transcript}
Respond concisely in the specified language (${lang}). Use short bullet points where appropriate. Return plain text only (no JSON/markdown). If unsure, write 'No issues found'.`;

        // Kick off async structured eval of the latest assistant message (strict JSON)
        {
          const settingsStateEval = useGenAISettings.getState();
          const evalModel = (settingsStateEval.stanceModel && settingsStateEval.stanceModel.trim()) ? settingsStateEval.stanceModel : 'gemini_fast';
        const textToEvaluate = ((lastAssistant as any)?.contentSanitized || (lastAssistant as any)?.content || '').trim();
          if (textToEvaluate) {
            (async () => {
              try {
                const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
                
                // Skip evaluation for agent responses and scenario messages (they are system responses, not conversational content)
                const isAgentResponse = (lastAssistant as any)?.subkind?.startsWith('agent.') || !!(lastAssistant as any)?.agentProfile;
                const isScenarioMessage = (lastAssistant as any)?.subkind?.startsWith('scenarios.');
                if (isAgentResponse || isScenarioMessage) {
                  return;
                }
                
                // Find previous USER message as context
                let prevUserText: string | null = null;
                try {
                  const allInts = useGenaiChatStore.getState().interactions;
                  const idx = allInts.findIndex((it) => it.id === (lastAssistant as any)?.id);
                  if (idx > 0) {
                    for (let i = idx - 1; i >= 0; i--) {
                      if (allInts[i].kind === 'user' && allInts[i].content && (allInts[i].content as string).trim()) {
                        prevUserText = allInts[i].content as string;
                        break;
                      }
                    }
                  }
                } catch {}
                
                // Call unified evaluation function
                await evaluateBias({
                  messageId: lastAssistant.id,
                  messageRole: 'assistant',
                  textToEvaluate,
                  contextText: prevUserText || undefined,
                  evalModel,
                  preferredLang,
                });
              } catch (e) {
                console.warn('[LLMEvaluation] Assistant bias eval failed', e);
              }
            })().catch(console.error);
          }
        }

        // Check if eval.review already exists for this assistant message
        try {
          const map = persistedByLocalId || {};
          const serverId = map[lastAssistant.id];
          const reverseMap: Record<string, string[]> = {};
          Object.entries(map).forEach(([local, server]) => {
            if (!reverseMap[server as string]) reverseMap[server as string] = [];
            reverseMap[server as string].push(local);
          });
          const localIds = reverseMap[lastAssistant.id] || [];
          const interactions = getInteractions();
          const existingReview = [...interactions].reverse().find((it: any) => 
            it.subkind === 'eval.review' && 
            (it.replyTo === lastAssistant.id || (serverId && it.replyTo === serverId) || localIds.includes(it.replyTo || ''))
          );
          if (existingReview) {
            console.log('[LLMEvaluation] eval.review already exists for message', lastAssistant.id, '- skipping');
            lastReviewedId.current = lastAssistant.id;
            if (!cancelled) {
              setReview((existingReview as any).content || (existingReview as any).contentSanitized || '');
              setLoading(false);
            }
            return;
          }
        } catch (e) {
          console.warn('[LLMEvaluation] Failed to check existing eval.review', e);
        }
        const settingsState2 = useGenAISettings.getState();
        const modelAlias = (settingsState2.stanceModel && settingsState2.stanceModel.trim()) ? settingsState2.stanceModel : 'gemini_fast';
        const response = await api.genaiGenerate({
          model: modelAlias,
          prompt,
          temperature: 0.2,
          maxOutputTokens: 4096,
          webSearchEnabled: true,
          toolCallingEnabled: false,
          strictJSONEnabled: false,
          systemInstruction: 'You are an expert LLM output reviewer. Always return a non-empty plain text answer in the specified language with short bullet points where appropriate.'
        });
        const finished = performance.now();
        if (!cancelled) {
          const content = (response?.content || '').trim();
          if (content) {
            setReview(content);
            setDurationMs(Math.max(1, Math.round(finished - started)));
            setError(null);
            if (lastReviewedId.current !== lastAssistant?.id) {
              onFirstResult && onFirstResult();
            }
            lastReviewedId.current = lastAssistant?.id || null;
            // Create eval.review interaction linked to the last assistant message
            if (!cancelled) {
              try {
                const evalId = Date.now().toString() + '-eval-review-a';
                const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
                const projectId = useGenaiChatStore.getState().projectId;
                
                // Add to local state
                useGenaiChatStore.setState((s) => ({
                  interactions: [
                    ...s.interactions,
                    {
                      id: evalId,
                      projectId: s.projectId,
                      kind: 'system',
                      subkind: 'eval.review',
                      content: content,
                      contentSanitized: content,
                      replyTo: lastAssistant?.id,
                      relations: lastAssistant?.id ? [{ type: 'evaluates', targetId: lastAssistant.id }] : undefined,
                      language: preferredLang,
                      createdAt: new Date().toISOString(),
                    } as any,
                  ],
                } as any));
                
                // Persist to server
                if (projectId && projectId !== 'local-unfold' && lastAssistant?.id && !cancelled) {
                  (async () => {
                    try {
                      const map = (useGenaiChatStore.getState() as any).persistedByLocalId || {};
                      const serverReplyTo = map[lastAssistant.id] || lastAssistant.id;
                      
                      const payload: any = {
                        kind: 'system',
                        subkind: 'eval.review',
                        content: content,
                        replyTo: serverReplyTo,
                        relations: [{ type: 'evaluates', targetId: serverReplyTo }],
                        language: preferredLang,
                      };
                      
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
                      console.warn('[LLMEvaluation] Failed to persist eval.review to server', e);
                    }
                  })();
                }
              } catch (e) {
                console.warn('[LLMEvaluation] Failed to create eval.review', e);
              }
            }
          } else {
            setError('The model returned an empty review.');
          }
        }
      } catch (err: any) {
        console.error('[LLMEvaluation] Error:', err);
        if (!cancelled) setError(err?.message || 'Failed to evaluate answers.');
      } finally {
        if (!cancelled) {
          setLoading(false);
          // Clear evaluation flag
          if (lastAssistant?.id) {
            evaluatingRef.current.delete(lastAssistant.id);
          }
        }
      }
    };
    run();
    return () => { 
      cancelled = true;
      // Clear evaluation flag on unmount
      if (lastAssistant?.id) {
        evaluatingRef.current.delete(lastAssistant.id);
      }
    };
  }, [streaming, lastAssistant?.id, currentUser?.uid]);

  if (!hasAssistant) return null;
  const sanitizeSx = (s: string) => {
    try { return (s || '').replace(/<!--\s*@sx:[\s\S]*?-->/g, '').trim(); } catch { return s; }
  };

  return (
    <div className="mt-3 p-3">
      <div className="text-13 mb-2" style={{ color: '#669999' }}>Assistant's stance:</div>
      {error ? (
        <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{error}</div>
      ) : review ? (
        <>
          <div className="text-12 fade-in" style={{ color: 'var(--text-secondary-strong)', lineHeight: '1.5' }}>
            {sanitizeSx(review)}
          </div>
          {typeof durationMs === 'number' && (
            <div className="text-[11px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              Time: {durationMs} ms
            </div>
          )}
        </>
      ) : (
        <div className="text-12" style={{ color: 'var(--text-secondary)' }}>No review yet.</div>
      )}
    </div>
  );
};

