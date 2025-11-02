import React from 'react';
import { useTranslation } from 'react-i18next';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { EvaluationDetails } from './EvaluationDetails';
import AgentPalette from './AgentPalette';
import { AGENT_PRESETS } from '../lib/agentPresets';
import { TelegramFetchUpdatesTool } from '../lib/genaiTools';
import { HATL_ONE_INSTRUCTIONS } from '../lib/hatlInstructions';
import { getPreferredLanguage, getLanguageName, getAnchorRect, getSxSystemInstructionFull } from '../lib/uiUtils';

// Action row under assistant messages: shows bias details when available,
// otherwise shows action buttons (Bring In, Copy) immediately after message
export const ActionRow: React.FC<{ messageId: string; isAssistant: boolean; content?: string }> = ({ messageId, isAssistant, content }) => {
  const genaiChat = useGenaiChatStore();
  const { t } = useTranslation();
  const hasEvalBias = React.useMemo(() => {
    try {
      const map = (genaiChat as any).persistedByLocalId || {};
      const serverId = map[messageId];
      // Build reverse map for bidirectional lookup
      const reverseMap: Record<string, string[]> = {};
      Object.entries(map).forEach(([local, server]) => {
        if (!reverseMap[server as string]) reverseMap[server as string] = [];
        reverseMap[server as string].push(local);
      });
      const localIds = reverseMap[messageId] || [];
      return !![...genaiChat.interactions].reverse().find((it: any) => 
        it.subkind === 'eval.bias' && 
        (it.replyTo === messageId || (serverId && it.replyTo === serverId) || localIds.includes(it.replyTo || ''))
      );
    } catch { return false; }
  }, [genaiChat.interactions, (genaiChat as any).persistedByLocalId, messageId]);
  const settings = useGenAISettings();
  const [showBringIn, setShowBringIn] = React.useState(false);
  const [showAsk, setShowAsk] = React.useState(false);
  const [askText, setAskText] = React.useState('');
  const bringInRef = React.useRef<HTMLDivElement | null>(null);
  const askRef = React.useRef<HTMLDivElement | null>(null);
  const [paletteAskOpen, setPaletteAskOpen] = React.useState(false);
  const [paletteBringOpen, setPaletteBringOpen] = React.useState(false);
  const [exportMenuOpen, setExportMenuOpen] = React.useState(false);
  const exportRef = React.useRef<HTMLDivElement | null>(null);
  const interForMessage = React.useMemo(() => {
    try {
      // First try direct match
      let found = genaiChat.interactions.find((x) => x.id === messageId);
      if (found) return found;
      
      // If not found, check mapping and originalMessageId
      const map = (genaiChat as any).persistedByLocalId || {};
      const serverId = map[messageId] || messageId;
      
      // Try server ID
      if (serverId !== messageId) {
        found = genaiChat.interactions.find((x) => x.id === serverId);
        if (found) return found;
      }
      
      // Try by originalMessageId
      found = genaiChat.interactions.find((x) => (x as any).originalMessageId === messageId);
      if (found) return found;
      
      // Try reverse mapping: if messageId is a server ID, find interactions with local IDs that map to it
      const reverseMap: Record<string, string[]> = {};
      Object.entries(map).forEach(([local, server]) => {
        if (!reverseMap[server as string]) reverseMap[server as string] = [];
        reverseMap[server as string].push(local);
      });
      const localIds = reverseMap[messageId] || [];
      for (const localId of localIds) {
        found = genaiChat.interactions.find((x) => x.id === localId);
        if (found) return found;
      }
      
      return undefined as any;
    } catch { return undefined as any; }
  }, [genaiChat.interactions, messageId, (genaiChat as any).persistedByLocalId]);
  const derivedHeaderText = React.useMemo(() => {
    try {
      const meta = genaiChat.messageMeta[messageId];
      return meta?.headerText || interForMessage?.author?.displayName || (interForMessage?.agentProfile ? 'agent' : undefined);
    } catch { return undefined; }
  }, [genaiChat.messageMeta, interForMessage, messageId]);
  const derivedAgentProfile = React.useMemo(() => {
    try {
      const meta = genaiChat.messageMeta[messageId];
      return meta?.agentProfile || interForMessage?.agentProfile;
    } catch { return undefined; }
  }, [genaiChat.messageMeta, interForMessage, messageId]);
  const derivedRefineEnabled = React.useMemo(() => {
    try {
      const meta = genaiChat.messageMeta[messageId];
      if (typeof meta?.refineEnabled === 'boolean') return meta.refineEnabled;
      return !!interForMessage?.agentProfile?.capabilities?.refinePrevious;
    } catch { return false; }
  }, [genaiChat.messageMeta, interForMessage, messageId]);
  React.useEffect(() => {
    const onDocClick = (e: MouseEvent) => {
      if (!showBringIn) return;
      const el = bringInRef.current;
      if (el && !el.contains(e.target as Node)) setShowBringIn(false);
    };
    document.addEventListener('mousedown', onDocClick, { capture: true });
    return () => document.removeEventListener('mousedown', onDocClick, { capture: true } as any);
  }, [showBringIn]);
  React.useEffect(() => {
    const onDocClick = (e: MouseEvent) => {
      if (!showAsk) return;
      const el = askRef.current;
      if (el && !el.contains(e.target as Node)) setShowAsk(false);
    };
    document.addEventListener('mousedown', onDocClick, { capture: true });
    return () => document.removeEventListener('mousedown', onDocClick, { capture: true } as any);
  }, [showAsk]);
  React.useEffect(() => {
    const onDocClick = (e: MouseEvent) => {
      if (!exportMenuOpen) return;
      const el = exportRef.current;
      if (el && !el.contains(e.target as Node)) setExportMenuOpen(false);
    };
    document.addEventListener('mousedown', onDocClick, { capture: true });
    return () => document.removeEventListener('mousedown', onDocClick, { capture: true } as any);
  }, [exportMenuOpen]);

  const exportMarkdown = React.useCallback(async () => {
    try {
      const projectId = (genaiChat as any)?.projectId as string | undefined;
      if (!projectId || projectId === 'local-unfold') return;
      const text = (content || '').toString();
      const baseUnsafe = (derivedHeaderText ? `${derivedHeaderText}-contribution` : 'assistant-answer');
      const base = baseUnsafe.toLowerCase().replace(/[^a-z0-9-_]+/gi, '-').replace(/-+/g, '-').replace(/^-|-$/g, '') || 'assistant-answer';
      const ts = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
      const filename = `${base}-${ts}.md`;
      const { api } = await import('../lib/api');
      await api.docsCreateFromText(projectId, { filename, contentType: 'text/markdown', text });
    } catch {}
  }, [content, genaiChat, derivedHeaderText]);

  const exportToDocx = React.useCallback(async () => {
    try {
      const projectId = (genaiChat as any)?.projectId as string | undefined;
      if (!projectId || projectId === 'local-unfold') return;
      
      // First, save as markdown document
      const text = (content || '').toString();
      const baseUnsafe = (derivedHeaderText ? `${derivedHeaderText}-contribution` : 'assistant-answer');
      const base = baseUnsafe.toLowerCase().replace(/[^a-z0-9-_]+/gi, '-').replace(/-+/g, '-').replace(/^-|-$/g, '') || 'assistant-answer';
      const ts = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
      const filename = `${base}-${ts}.md`;
      
      const { api } = await import('../lib/api');
      const doc = await api.docsCreateFromText(projectId, { filename, contentType: 'text/markdown', text });
      
      // Then export to DOCX
      const blob = await api.docsExportToDocx(projectId, doc.id);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${base}-${ts}.docx`;
      document.body.appendChild(a);
      a.click();
      setTimeout(() => {
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
      }, 0);
    } catch (error) {
      console.error('Failed to export to DOCX:', error);
    }
  }, [content, genaiChat, derivedHeaderText]);
  if (!isAssistant || !content || !content.trim()) return null;
  
  // Get frame expansions from interactions with subkind 'eval.frame_expansions'
  const frameExpansions = React.useMemo(() => {
    try {
      const map = (genaiChat as any).persistedByLocalId || {};
      const serverId = map[messageId] || messageId;
      
      // Build reverse map for bidirectional lookup
      const reverseMap: Record<string, string[]> = {};
      Object.entries(map).forEach(([local, server]) => {
        if (!reverseMap[server as string]) reverseMap[server as string] = [];
        reverseMap[server as string].push(local);
      });
      const localIds = reverseMap[messageId] || [];
      
      // Find expansion interaction linked to this message
      const expansionInteraction = genaiChat.interactions.find((it) => 
        it.subkind === 'eval.frame_expansions' && 
        (it.replyTo === messageId || it.replyTo === serverId || localIds.includes(it.replyTo || '') ||
         (it.relations && it.relations.some((r) => r.targetId === messageId || r.targetId === serverId)))
      );
      
      if (expansionInteraction) {
        try {
          const data = JSON.parse(expansionInteraction.content || expansionInteraction.contentSanitized || '{}');
          if (data.expansions && Array.isArray(data.expansions)) {
            return data.expansions;
          }
        } catch (e) {
          console.warn('[ActionRow] Failed to parse frame expansions', e);
        }
      }
      
      return undefined;
    } catch (e) { 
      // Silently fail - no need to log every time
      return undefined; 
    }
  }, [genaiChat.interactions, messageId, (genaiChat as any).persistedByLocalId]);
  
  return (
    <div className="mt-1" style={{ width: '100%', position: 'relative' }}>
      <div className="flex items-center gap-2">
        {hasEvalBias && (
          <div style={{ flex: 1, minWidth: 0 }}>
            <EvaluationDetails messageId={messageId} />
          </div>
        )}
        <div className="ml-auto flex items-center gap-1" style={{ position: 'relative' }}>
          {/* Error block for failed agent reply (Ask/Bring In) with retry/switch */}
          {(() => {
            try {
              const st = useGenaiChatStore.getState();
              const errText = st.error;
              const pMeta = st.pendingAssistantMeta;
              const pOpts = st.pendingAssistantOptions;
              const pPrompt = st.pendingAssistantPrompt;
              if (!errText || !pOpts || !pPrompt) return null;
              const label = pMeta?.displayName ? `агентом ${pMeta.displayName}` : 'агентом';
              return (
                <div className="flex items-center gap-2 px-2 py-1 rounded-lg" style={{ border: '1px solid var(--overlay-line-90)', background: 'var(--bg-elevated)' }}>
                  <span className="text-[11px]" style={{ color: 'var(--accent-danger)' }}>Ошибка связи с {label}. Запрос не доставлен.</span>
                  <button
                    className="text-[11px] rounded px-2 py-0.5"
                    style={{ border: '1px solid var(--overlay-line-90)', color: 'var(--text-secondary-strong)' }}
                    onClick={async () => {
                      try {
                        await useGenaiChatStore.getState().startStream({ prompt: pPrompt, tools: st.pendingAssistantTools, options: pOpts, agentMeta: pMeta, suppressUserEcho: true });
                      } catch {}
                    }}
                  >{t('unfold.actions.retry')}</button>
                  <button
                    className="text-[11px] rounded px-2 py-0.5"
                    style={{ border: '1px solid var(--overlay-line-90)', color: 'var(--text-secondary-strong)' }}
                    onClick={() => setShowBringIn((v) => !v)}
                  >{t('unfold.actions.switchModel')}</button>
                  <button
                    className="text-[11px] rounded px-2 py-0.5"
                    style={{ border: '1px solid var(--overlay-line-90)', color: 'var(--text-secondary-strong)' }}
                    onClick={() => { try { navigator.clipboard.writeText(pPrompt); } catch {} }}
                  >{t('unfold.actions.copyRequest')}</button>
                </div>
              );
            } catch { return null; }
          })()}
          {(() => {
            const displayName = derivedHeaderText;
            if (!displayName) return null;
            return (
              <>
                <button
                  type="button"
                  aria-label="Ask this agent"
                  title="Ask this agent"
                  className="rounded-lg"
                  style={{ height: 24, backgroundColor: 'transparent', color: 'var(--text-secondary-strong)', display: 'flex', alignItems: 'center', gap: 6, padding: '0 8px', border: '1px solid var(--overlay-line-90)' }}
                  onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                  onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                  onClick={() => setPaletteAskOpen(true)}
                >
                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M12 20l-3.5-3H6a4 4 0 01-4-4V7a4 4 0 014-4h12a4 4 0 014 4v6a4 4 0 01-4 4h-2.5L12 20z" strokeWidth="2"/></svg>
                  <span className="text-[11px]">Ask this agent</span>
                </button>
                <AgentPalette
                  mode="ask"
                  visible={paletteAskOpen}
                  title={`Ask: ${displayName}`}
                  subtitle={`${derivedAgentProfile?.provider ?? ''} · ${derivedAgentProfile?.model ?? ''}`}
                  initialAskText={''}
                  anchorRect={getAnchorRect()}
                  onClose={() => setPaletteAskOpen(false)}
                  onSubmitAsk={async (question) => {
                    if (!question.trim()) { setPaletteAskOpen(false); return; }
                    try {
                      const ints = useGenaiChatStore.getState().interactions;
                      const idx = ints.findIndex((it) => it.id === messageId);
                      let origUser: string | null = null;
                      for (let i = idx - 1; i >= 0; i--) {
                        if (ints[i].kind === 'user' && ints[i].content && (ints[i].content as string).trim()) { origUser = ints[i].content as string; break; }
                      }
                      const assistantInter = ints.find((it) => it.id === messageId);
                      const agentAnswer = (assistantInter?.contentSanitized || assistantInter?.content || '').toString();
                      const preferredLang = getPreferredLanguage();
                      const langName = getLanguageName(preferredLang);
                      const agentName = displayName || 'agent';
                      const prof = derivedAgentProfile;
                      const prompt = `User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nYou are ${agentName}. The user has a follow-up question to your previous answer.\n\nOriginal user question (context):\n${origUser || '(not found)'}\n\nYour previous answer:\n${agentAnswer}\n\nUser follow-up (address this directly):\n${question}`;
                      const tools = settings.enableTelegramTool ? [TelegramFetchUpdatesTool] : undefined;
                      const useModel = prof?.model || settings.model;
                      const sxSystemInstruction = getSxSystemInstructionFull();
                      const combinedSystemInstruction = [settings.systemInstruction || '', HATL_ONE_INSTRUCTIONS, sxSystemInstruction].filter(Boolean).join('\n\n').trim() || undefined;
                      const options: any = {
                        profile: /_quality$/i.test(useModel || '') ? 'quality' : 'fast',
                        modelOverride: useModel,
                        groundingEnabled: settings.groundingEnabled,
                        temperature: settings.temperature,
                        maxOutputTokens: settings.maxOutputTokens,
                        systemInstruction: combinedSystemInstruction,
                      };
                      const userEcho = `${question}`;
                      setPaletteAskOpen(false);
                      await useGenaiChatStore.getState().startStream({ prompt: userEcho, tools, options: { ...options, __lang: preferredLang }, agentMeta: { displayName: agentName, agentProfile: prof || undefined, subkind: 'agent.ask', replyTo: messageId } });
                    } catch {}
                  }}
                />
              </>
            );
          })()}
          {/* Ask button is shown in EvaluationDetails near Refine; ActionRow keeps Bring In / Copy only */}
          {/* Refine button should be left of Bring In */}
          {(() => {
            if (!derivedRefineEnabled) return null;
            return (
              <button
                type="button"
                aria-label="Refine the previous answer"
                title="Refine the previous primary answer based on this contribution."
                className="rounded-lg"
                style={{ height: 24, backgroundColor: 'transparent', color: 'var(--text-secondary-strong)', display: 'flex', alignItems: 'center', gap: 6, padding: '0 8px', border: '1px solid var(--overlay-line-90)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={async () => {
                  try {
                    const msgs = useGenaiChatStore.getState().messages;
                    const idx = msgs.findIndex((m) => m.id === messageId);
                    let prevPrimary: string | null = null;
                    let prevPrimaryId: string | null = null;
                    for (let i = idx - 1; i >= 0; i--) {
                      const mm = msgs[i];
                      if (mm.role === 'assistant') {
                        const mmeta = useGenaiChatStore.getState().messageMeta[mm.id];
                        if (!mmeta?.headerText) { prevPrimary = mm.content; prevPrimaryId = mm.id; break; }
                      }
                    }
                    if (!prevPrimary) return;
                    const preferredLang = getPreferredLanguage();
                    const langName = getLanguageName(preferredLang);
                    const contribution = msgs.find((m) => m.id === messageId)?.content || '';
                    const prompt = `User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nAs an expert, you are participating in a multi-agent discussion. Your task is to refine your own previous answer by integrating the contribution from another agent.\n\nRequirements:\n- Rewrite the Original Answer by integrating the Agent Contribution into a single, improved answer.\n- Do not comment on the changes, the agent, or the process; no meta-remarks; do not ask questions.\n- Use the same language and tone as the Original Answer; keep or improve structure (headings, bullets).\n- Be concise; remove redundancies; keep key facts only.\n- Weave added counterpoints into their relevant sections rather than appending a separate critique.\n- Output only the refined answer with no preface, labels, or extra text.\n\nOriginal Answer:\n${prevPrimary}\n\nAgent Contribution:\n${contribution}\n\nRefined Answer:`;
                    const gs = useGenAISettings.getState();
                    const res = await (await import('../lib/api')).api.genaiGenerate({ model: gs.model, prompt, strictJSONEnabled: false, maxOutputTokens: Math.max(1024, gs.maxOutputTokens || 2048), webSearchEnabled: false, systemInstruction: gs.systemInstruction || undefined });
                    const stripFences = (s: string) => { let t = (s || '').toString().trim(); t = t.replace(/^```[a-zA-Z]*\n([\s\S]*?)\n```$/m, '$1').trim(); t = t.replace(/^```[\s\S]*?```$/m, (m) => m.replace(/^```[a-zA-Z]*\n?/, '').replace(/\n?```$/, '')).trim(); if ((t.startsWith('"') && t.endsWith('"')) || (t.startsWith("'") && t.endsWith("'"))) t = t.slice(1, -1); return t.trim(); };
                    const refined = stripFences(res?.content || '');
                    if (!refined || !prevPrimaryId) return;
                    useGenaiChatStore.setState((s) => ({
                      messages: s.messages.map((m) => m.id === prevPrimaryId ? { ...m, content: refined } : m),
                      interactions: s.interactions.map((it: any) => it.id === prevPrimaryId ? ({ ...it, contentSanitized: refined, content: refined }) : it),
                    } as any));
                  } catch {}
                }}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <path d="M3 17v4h4" strokeWidth="2" strokeLinecap="round" />
                  <path d="M7 17l5-5" strokeWidth="2" strokeLinecap="round" />
                  <rect x="10" y="3" width="11" height="11" rx="2" strokeWidth="2" />
                </svg>
                <span className="text-[11px]">Refine the previous answer</span>
              </button>
            );
          })()}

          <button
            type="button"
            aria-label="Bring in another model"
            title="Bring in another AI model to the discussion."
            className="rounded-lg"
            style={{ width: 24, height: 24, backgroundColor: 'transparent', color: 'var(--text-secondary-strong)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
            onClick={() => setPaletteBringOpen(true)}
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
              <path d="M12 7a3 3 0 1 1 0 6a3 3 0 0 1 0-6" strokeWidth="2" />
              <path d="M5 20c0-3.314 3.134-6 7-6s7 2.686 7 6" strokeWidth="2" strokeLinecap="round" />
              <path d="M18 7v4M16 9h4" strokeWidth="2" strokeLinecap="round" />
            </svg>
          </button>
          <AgentPalette
            mode="bring_in"
            visible={paletteBringOpen}
            title={'Bring in an agent'}
            presets={AGENT_PRESETS}
            anchorRect={(() => {
              try {
                const el = (document.querySelector('#unfold-request-container') || document.querySelector('#app') || document.body) as HTMLElement;
                const rect = el.getBoundingClientRect();
                return { top: rect.top - 8, left: rect.left, width: rect.width, height: rect.height };
              } catch { return null as any; }
            })()}
            onClose={() => setPaletteBringOpen(false)}
            onSelectPreset={async (p: any) => {
              setPaletteBringOpen(false);
              try {
                const tools = settings.enableTelegramTool ? [TelegramFetchUpdatesTool] : undefined;
                const model = p.modelAlias;
                const sxSystemInstruction = (
                  `Hidden-instructions protocol (do not show to users):\n` +
                  `- When you need to output service instructions (entities, metrics to monitor, watchlist, relations), emit ONLY HTML comments of the form <!-- @sx:{JSON} -->.\n` +
                  `- JSON must be valid and match v: \"1\" with one of types: entity_mark, monitor_metric, watchlist_add, link_reference.\n` +
                  `- Do not duplicate @sx content in visible markdown. Keep normal answer readable.\n` +
                  `- Examples: <!-- @sx:{\\\"v\\\":\\\"1\\\",\\\"type\\\":\\\"entity_mark\\\",\\\"id\\\":\\\"ent-001\\\",\\\"kind\\\":\\\"person\\\",\\\"name\\\":\\\"…\\\",\\\"description\\\":\\\"…\\\"} -->`
                );
                const combinedSystemInstruction = [settings.systemInstruction || '', p.params?.systemInstruction || '', HATL_ONE_INSTRUCTIONS, sxSystemInstruction].filter(Boolean).join('\n\n').trim() || undefined;
                const options: any = {
                  profile: /_quality$/i.test(model) ? 'quality' : 'fast',
                  modelOverride: model,
                  groundingEnabled: p.capabilities?.webSearch ?? settings.groundingEnabled,
                  temperature: typeof p.params?.temperature === 'number' ? p.params?.temperature : settings.temperature,
                  maxOutputTokens: settings.maxOutputTokens,
                  systemInstruction: combinedSystemInstruction,
                };
                const preferredLang = getPreferredLanguage();
                const langName = getLanguageName(preferredLang);
                const st = useGenaiChatStore.getState();
                // Find the specific message that triggered Bring In and the previous user message
                // Use both messages and interactions, accounting for ID mapping
                const map = (st as any).persistedByLocalId || {};
                const serverMessageId = map[messageId] || messageId;
                
                // Build reverse map for bidirectional lookup
                const reverseMap: Record<string, string[]> = {};
                Object.entries(map).forEach(([local, server]) => {
                  if (!reverseMap[server as string]) reverseMap[server as string] = [];
                  reverseMap[server as string].push(local);
                });
                const localIds = reverseMap[messageId] || [];
                
                // Combine messages and interactions, prioritizing interactions (server-side data)
                const allItems = [...st.interactions, ...st.messages];
                
                // Find target message - try multiple ID variations
                const targetMsg = allItems.find((m: any) => 
                  m.id === messageId || 
                  m.id === serverMessageId || 
                  (m as any).originalMessageId === messageId ||
                  (m as any).originalMessageId === serverMessageId ||
                  localIds.includes(m.id || '')
                ) || null;
                
                // Find target index in filtered list (for finding previous messages)
                const msgs = allItems.filter((m: any) => {
                  const content = m.content || (m as any).contentSanitized || '';
                  return content && content.trim();
                });
                
                const targetIdx = msgs.findIndex((m: any) => 
                  m.id === messageId || 
                  m.id === serverMessageId || 
                  (m as any).originalMessageId === messageId ||
                  (m as any).originalMessageId === serverMessageId ||
                  localIds.includes(m.id || '')
                );
                
                // Collect context: find user messages before target (up to 3 for better context)
                const contextMessages: Array<{ role: string; content: string }> = [];
                if (targetIdx >= 0) {
                  // Collect backwards from target message
                  for (let i = targetIdx - 1; i >= 0 && contextMessages.length < 3; i--) {
                    const msg = msgs[i];
                    const role = (msg as any).role || (msg as any).kind || '';
                    const isUser = role === 'user' || role === 'discovery.query';
                    const isAssistant = role === 'assistant' || role === 'discovery.answer' || role === 'note';
                    const content = (msg as any).content || (msg as any).contentSanitized || '';
                    if (content && content.trim() && (isUser || isAssistant)) {
                      contextMessages.unshift({ role: isUser ? 'user' : 'assistant', content });
                    }
                  }
                  // Add target message itself
                  if (targetMsg) {
                    const targetContent = targetMsg.content || (targetMsg as any).contentSanitized || '';
                    if (targetContent && targetContent.trim()) {
                      contextMessages.push({ role: 'assistant', content: targetContent });
                    }
                  }
                }
                
                // Build transcript from collected context
                const transcript = contextMessages.map(m => 
                  `${m.role.toUpperCase()}: ${m.content}`
                ).join('\n');
                
                // Log context size for debugging
                const transcriptChars = transcript.length;
                const transcriptTokens = Math.ceil(transcriptChars / 4);
                console.log('[Bring In] Context size:', {
                  messageId,
                  serverMessageId,
                  contextMessagesCount: contextMessages.length,
                  transcriptChars,
                  transcriptTokens,
                  targetFound: !!targetMsg,
                });
                
                // Include bias evaluation of this answer to help the agent calibrate its stance
                const biasBlock = (() => {
                  try {
                    const serverId = map[messageId];
                    const localIds = reverseMap[messageId] || [];
                    const evalNode = [...genaiChat.interactions].reverse().find((it: any) => 
                      it.subkind === 'eval.bias' && 
                      (it.replyTo === messageId || (serverId && it.replyTo === serverId) || localIds.includes(it.replyTo || ''))
                    );
                    if (!evalNode) return '';
                    const ev = JSON.parse((evalNode as any).content || '{}');
                    const thLevel = (typeof ev.thoughtfulnessLevel === 'number' ? ev.thoughtfulnessLevel : ev.thoughtfulness);
                    const obj = {
                      biasLevel: ev.biasLevel,
                      thoughtfulness: thLevel,
                      thoughtfulnessLevel: thLevel,
                      thoughtfulnessDimensions: ev.thoughtfulnessDimensions,
                      categories: ev.categories || [],
                    };
                    return `\n\nAutomatic bias analysis (for your awareness; do not echo):\n${JSON.stringify(obj, null, 2)}`;
                  } catch { return ''; }
                })();
                // Build prompt depending on agent
                const prompt = (() => {
                  if ((p.id && /evidence-builder/i.test(p.id)) || /evidence builder/i.test(p.displayName || '')) {
                    return (
                      `User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if they are standard terms in target language.\n\n` +
                      `You are ${p.displayName}, an invited AI participant. Your role is to DEEPEN and VERIFY the ASSISTANT's answer by building a comprehensive evidence base.\n\n` +
                      `CONTEXT (do not echo verbatim):\n${transcript}${biasBlock}\n\n` +
                      `TASK:\n` +
                      `1) Verify all factual claims and data points in the ASSISTANT's answer using authoritative, verifiable sources.\n` +
                      `2) Expand on verified points with additional context and nuance.\n` +
                      `3) Where logical approximations/assumptions are needed, state them explicitly, justify them, and explain how they are applied.\n` +
                      `4) Explicitly flag any claims that cannot be verified or lack sufficient evidence.\n` +
                      `5) Use web search to find authoritative sources when necessary.\n` +
                      `6) All section titles/headings MUST be in ${langName} (preferred language).\n` +
                      `7) You MAY think out of the box: propose deeper, unconventional yet logically sound chains of evidence and assumptions beyond typical domain practice; keep reasoning explicit, traceable, and transparent.\n\n` +
                      `OUTPUT (do not include prefaces):\n` +
                      `- Provide the deepened, verified response (same language and tone as the ASSISTANT).\n` +
                      `- Integrate inline citations [1], [2] as needed.\n` +
                      `\n---\nEVIDENCE SUMMARY\n\n` +
                      `Sources Used (numbered list, one per line; include URLs; no continuous paragraph):\n1) URL or reference — brief description\n2) URL or reference — brief description\n[...]\n\n` +
                      `Assumptions & Approximations:\n• [Assumption]: How it was interpreted and applied\n[...]\n\n` +
                      `Could Not Verify:\n• [Claim lacking sufficient evidence]\n[...]\n`
                    );
                  }
                  // Default counterpoint-style prompt for other agents
                  return (
                    `User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\n` +
                    `You are ${p.displayName}, an invited AI participant analyzing an AI assistant's response to a user query.\n\n` +
                    `YOUR TASK:\n` +
                    `Evaluate the ASSISTANT's answer and provide an alternative perspective or counterpoint. If the ASSISTANT's response has limitations, biases, or missing viewpoints, articulate them constructively. Present a well-reasoned contribution that adds value through critical analysis or an alternative angle.\n\n` +
                    `CRITICAL REQUIREMENTS FOR TONE AND STYLE:\n` +
                    `1) Write for educated non-specialists, not for policy analysts or academics. Avoid bureaucratic jargon and acronyms without explanation.\n` +
                    `2) Ground your analysis in real human context: who is affected? What are the practical consequences for people, communities, or organizations?\n` +
                    `3) Use narrative and concrete examples alongside data. Tell a story, not a spreadsheet. Weave ideas into coherent prose rather than bullet-only lists.\n` +
                    `4) Express uncertainty both qualitatively ("likely", "possibly", "unclear") AND quantitatively when logically justified. If you provide metrics (probabilities, confidence ranges, pro/con weights), wrap them in human-readable explanations:\n` +
                    `   - Good: "This scenario is more likely to fail than succeed (roughly 60% risk vs 40% benefit), primarily because..."\n` +
                    `   - Good: "The evidence base is fairly strong (about 78% of key claims have direct sources), though some projections rely on analogies..."\n` +
                    `   - Bad: "Баланс веса: pro 40%, con 60%" (naked numbers without context)\n` +
                    `   Always explain the reasoning behind your numbers: what base rates, analogies, or models inform them.\n` +
                    `5) Acknowledge trade-offs and moral/values dimensions where relevant. Recognize that decisions affect real people's lives.\n` +
                    `6) Structure your response for readability: use section headings (in ${langName}), short paragraphs, and strategic use of lists—but avoid purely mechanical formatting.\n` +
                    `7) When citing sources or data, integrate them naturally into your narrative with brief inline explanations. Avoid dense citation blocks that interrupt flow.\n` +
                    `8) FORMALIZATION OPTION: If the topic requires rigorous policy analysis, quantitative risk assessment, or regulatory evaluation, you may use structured analytical frameworks (pro/con weights, evidence quality metrics, probability ranges, scenario trees). However, you MUST:\n` +
                    `   - Introduce each framework in plain language first (e.g., "To weigh the arguments systematically, I'll score the strength of evidence...")\n` +
                    `   - Explain your methodology and assumptions explicitly\n` +
                    `   - Connect metrics back to real-world consequences for people/organizations\n` +
                    `   - Close with a human-readable synthesis, not just raw numbers\n\n` +
                    `Do not ask clarifying questions or request additional input; rely only on the provided context.\n` +
                    `Do not restate the prompt or context; respond directly with your perspective.\n\n` +
                    `Context:\n${transcript}${biasBlock}\n\n` +
                    `Your perspective:`
                  );
                })();
                // Create indicator message before calling agent
                // map and serverMessageId already defined above
                const indicatorContent = `${t('unfold.actions.agentInvoked')} ${p.displayName}`;
                await useGenaiChatStore.getState().createIndicatorMessage(indicatorContent, 'action.bring_in', serverMessageId, { agentProfile: { provider: p.provider, model: model, capabilities: { tools: !!p.capabilities?.tools, webSearch: !!p.capabilities?.webSearch, reasoning: !!p.capabilities?.reasoning, refinePrevious: !!p.capabilities?.refinePrevious }, params: p.params } });
                await useGenaiChatStore.getState().startStream({ prompt, tools, options: { ...options, __lang: preferredLang }, agentMeta: { displayName: p.displayName, agentProfile: { provider: p.provider, model: model, capabilities: { tools: !!p.capabilities?.tools, webSearch: !!p.capabilities?.webSearch, reasoning: !!p.capabilities?.reasoning, refinePrevious: !!p.capabilities?.refinePrevious }, params: p.params }, subkind: 'agent.bring_in', replyTo: serverMessageId }, suppressUserEcho: true });
              } catch {}
            }}
          />
          <button
            type="button"
            aria-label="Copy answer"
            title="Copy answer"
            className="rounded-lg"
            style={{ width: 24, height: 24, backgroundColor: 'transparent', color: 'var(--text-secondary-strong)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
            onClick={() => { try { navigator.clipboard.writeText(content); } catch {} }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
              <rect x="9" y="9" width="11" height="11" rx="2" strokeWidth="2" />
              <path d="M7 15H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h7a2 2 0 0 1 2 2v1" strokeWidth="2" strokeLinecap="round" />
            </svg>
          </button>
          <div ref={exportRef} style={{ position: 'relative' }}>
            <button
              type="button"
              aria-label="Export answer"
              title="Export answer"
              className="rounded-lg"
              style={{ width: 24, height: 24, backgroundColor: 'transparent', color: 'var(--text-secondary-strong)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
              onClick={() => setExportMenuOpen((v) => !v)}
              onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
              onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <path d="M10 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4" strokeWidth="2" strokeLinecap="round" />
                <polyline points="16 3 21 3 21 8" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                <line x1="21" y1="3" x2="13" y2="11" strokeWidth="2" strokeLinecap="round" />
              </svg>
            </button>
            {exportMenuOpen && (
              <div className="rounded-lg" style={{ position: 'absolute', right: 0, top: 28, zIndex: 60, background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)', minWidth: 220, boxShadow: '0 4px 14px rgba(0,0,0,0.2)' }}>
                <div className="p-1">
                  <button
                    type="button"
                    className="w-full text-left rounded px-2 py-1 text-[12px]"
                    style={{ color: 'var(--text-secondary-strong)' }}
                    onClick={async () => { setExportMenuOpen(false); await exportMarkdown(); }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                  >
                    Static · Markdown (.md)
                  </button>
                  <button
                    type="button"
                    className="w-full text-left rounded px-2 py-1 text-[12px]"
                    style={{ color: 'var(--text-secondary-strong)' }}
                    onClick={async () => { setExportMenuOpen(false); await exportToDocx(); }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                  >
                    Microsoft Word (.docx)
                  </button>
                </div>
              </div>
            )}
          </div>
          {/* Refine button moved earlier (before Bring In) */}
        </div>
      </div>
      {frameExpansions && frameExpansions.length > 0 && (
        <div className="mt-2" style={{ width: '100%' }}>
          <div className="p-2" style={{ backgroundColor: 'var(--white-05)', borderLeft: '2px solid var(--overlay-line-90)' }}>
            <div className="text-[11px] mb-1.5 font-medium" style={{ color: 'var(--text-heading)' }}>
              {t('unfold.actions.youMightConsider')}
            </div>
            <div className="space-y-1.5">
              {frameExpansions.map((exp: { question: string }, idx: number) => (
                <div
                  key={idx}
                  className="text-[11px] cursor-pointer hover:opacity-80 transition-opacity"
                  style={{ color: 'var(--text-secondary-strong)', lineHeight: '1.4' }}
                  onClick={() => {
                    try {
                      // Find the textarea in UnfoldRequest component
                      const container = document.getElementById('unfold-request-container');
                      if (container) {
                        const textarea = container.querySelector('textarea') as HTMLTextAreaElement;
                        if (textarea) {
                          // Set value using React-friendly method
                          const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value')?.set;
                          if (nativeInputValueSetter) {
                            nativeInputValueSetter.call(textarea, exp.question);
                          } else {
                            textarea.value = exp.question;
                          }
                          // Trigger React's onChange handler by dispatching input event
                          const inputEvent = new Event('input', { bubbles: true });
                          textarea.dispatchEvent(inputEvent);
                          // Focus the textarea and move cursor to end
                          textarea.focus();
                          setTimeout(() => {
                            textarea.setSelectionRange(textarea.value.length, textarea.value.length);
                          }, 0);
                        }
                      }
                    } catch (e) {
                      console.warn('[ActionRow] Failed to set question in input', e);
                    }
                  }}
                >
                  {exp.question}
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

