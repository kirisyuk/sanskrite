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
import { ThemeContext } from './ThemeProvider';

// Action row under assistant messages: shows bias details when available,
// otherwise shows action buttons (Bring In, Copy) immediately after message
export const ActionRow: React.FC<{ messageId: string; isAssistant: boolean; content?: string }> = ({ messageId, isAssistant, content }) => {
  // PERFORMANCE FIX: Subscribe only to specific values, not entire arrays
  const interactionsLength = useGenaiChatStore((s) => s.interactions.length);
  const persistedByLocalId = useGenaiChatStore((s) => (s as any).persistedByLocalId);
  const messageMeta = useGenaiChatStore((s) => s.messageMeta);
  const projectId = useGenaiChatStore((s) => s.projectId);
  const evaluations = useGenaiChatStore((s) => (s as any).evaluations);
  
  // Get interactions only when needed via getState()
  const getInteractions = React.useCallback(() => useGenaiChatStore.getState().interactions, []);
  
  const { t } = useTranslation();
  const { theme } = React.useContext(ThemeContext);
  const hasEvalBias = React.useMemo(() => {
    // Check genaiChat.evaluations first (for user messages)
    if (evaluations && evaluations[messageId]) {
      return true;
    }
    // Then check interactions with subkind 'eval.bias'
    try {
      const map = persistedByLocalId || {};
      // Determine reliable server ID
      const effectiveServerId = map[messageId] || messageId;
      
      // Collect all known IDs for this message (local, server, mapped)
      const targetIds = new Set<string>();
      targetIds.add(messageId);
      if (effectiveServerId) targetIds.add(effectiveServerId);
      
      // Add all local IDs that map to the server ID
      Object.entries(map).forEach(([local, server]) => {
        if (server === effectiveServerId) targetIds.add(local);
      });
      
      const interactions = getInteractions();
      return !![...interactions].reverse().find((it: any) => 
        it.subkind === 'eval.bias' && 
        targetIds.has(it.replyTo || '')
      );
    } catch { return false; }
  }, [evaluations, getInteractions, interactionsLength, persistedByLocalId, messageId]);
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
  const [showReframe, setShowReframe] = React.useState(false);
  const [showSources, setShowSources] = React.useState(false);
  const [showBiasDetails, setShowBiasDetails] = React.useState(false);
  const [showAlternativeAgents, setShowAlternativeAgents] = React.useState(false);
  const alternativeAgentsRef = React.useRef<HTMLDivElement | null>(null);
  const reframeRef = React.useRef<HTMLDivElement | null>(null);
  const interForMessage = React.useMemo(() => {
    try {
      const interactions = getInteractions();
      // First try direct match
      let found = interactions.find((x) => x.id === messageId);
      if (found) return found;
      
      // If not found, check mapping and originalMessageId
      const map = persistedByLocalId || {};
      const serverId = map[messageId] || messageId;
      
      // Try server ID
      if (serverId !== messageId) {
        found = interactions.find((x) => x.id === serverId);
        if (found) return found;
      }
      
      // Try by originalMessageId
      found = interactions.find((x) => (x as any).originalMessageId === messageId);
      if (found) return found;
      
      // Try reverse mapping: if messageId is a server ID, find interactions with local IDs that map to it
      const reverseMap: Record<string, string[]> = {};
      Object.entries(map).forEach(([local, server]) => {
        if (!reverseMap[server as string]) reverseMap[server as string] = [];
        reverseMap[server as string].push(local);
      });
      const localIds = reverseMap[messageId] || [];
      for (const localId of localIds) {
        found = interactions.find((x) => x.id === localId);
        if (found) return found;
      }
      
      return undefined as any;
    } catch { return undefined as any; }
  }, [getInteractions, interactionsLength, messageId, persistedByLocalId]);
  const derivedHeaderText = React.useMemo(() => {
    try {
      const meta = messageMeta[messageId];
      return meta?.headerText || interForMessage?.author?.displayName || (interForMessage?.agentProfile ? 'agent' : undefined);
    } catch { return undefined; }
  }, [messageMeta, interForMessage, messageId]);
  const derivedAgentProfile = React.useMemo(() => {
    try {
      const meta = messageMeta[messageId];
      return meta?.agentProfile || interForMessage?.agentProfile;
    } catch { return undefined; }
  }, [messageMeta, interForMessage, messageId]);
  const derivedRefineEnabled = React.useMemo(() => {
    try {
      const meta = messageMeta[messageId];
      if (typeof meta?.refineEnabled === 'boolean') return meta.refineEnabled;
      return !!interForMessage?.agentProfile?.capabilities?.refinePrevious;
    } catch { return false; }
  }, [messageMeta, interForMessage, messageId]);
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
  React.useEffect(() => {
    const onDocClick = (e: MouseEvent) => {
      if (!showAlternativeAgents) return;
      const el = alternativeAgentsRef.current;
      if (el && !el.contains(e.target as Node)) setShowAlternativeAgents(false);
    };
    document.addEventListener('mousedown', onDocClick, { capture: true });
    return () => document.removeEventListener('mousedown', onDocClick, { capture: true } as any);
  }, [showAlternativeAgents]);
  // Auto-scroll to reframe content when it expands
  React.useEffect(() => {
    if (!showReframe || !reframeRef.current) return;
    // Use requestAnimationFrame to ensure DOM is updated
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        setTimeout(() => {
          if (reframeRef.current) {
            // Find the scrollable container (messages container)
            const scrollableContainer = reframeRef.current.closest('[class*="overflow-y-auto"], [style*="overflow-y"]') as HTMLElement | null;
            if (scrollableContainer) {
              const reframeRect = reframeRef.current.getBoundingClientRect();
              const containerRect = scrollableContainer.getBoundingClientRect();
              // Check if reframe content is below visible area or partially visible
              const isBelowViewport = reframeRect.bottom > containerRect.bottom;
              const isAboveViewport = reframeRect.top < containerRect.top;
              if (isBelowViewport || isAboveViewport) {
                // Scroll to show the entire reframe content
                reframeRef.current.scrollIntoView({
                  behavior: 'smooth',
                  block: 'nearest',
                  inline: 'nearest',
                });
              }
            } else {
              // Fallback: use scrollIntoView on the element itself
              reframeRef.current.scrollIntoView({
                behavior: 'smooth',
                block: 'nearest',
                inline: 'nearest',
              });
            }
          }
        }, 50); // Small delay to ensure content is fully rendered
      });
    });
  }, [showReframe]);
  // Auto-scroll to alternative agents list when it expands
  React.useEffect(() => {
    if (!showAlternativeAgents || !alternativeAgentsRef.current) return;
    // Use requestAnimationFrame to ensure DOM is updated
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        setTimeout(() => {
          if (alternativeAgentsRef.current) {
            // Find the scrollable container (messages container)
            const scrollableContainer = alternativeAgentsRef.current.closest('[class*="overflow-y-auto"], [style*="overflow-y"]') as HTMLElement | null;
            if (scrollableContainer) {
              const agentsRect = alternativeAgentsRef.current.getBoundingClientRect();
              const containerRect = scrollableContainer.getBoundingClientRect();
              // Check if agents list is below visible area or partially visible
              const isBelowViewport = agentsRect.bottom > containerRect.bottom;
              const isAboveViewport = agentsRect.top < containerRect.top;
              if (isBelowViewport || isAboveViewport) {
                // Scroll to show the entire agents list
                alternativeAgentsRef.current.scrollIntoView({
                  behavior: 'smooth',
                  block: 'nearest',
                  inline: 'nearest',
                });
              }
            } else {
              // Fallback: use scrollIntoView on the element itself
              alternativeAgentsRef.current.scrollIntoView({
                behavior: 'smooth',
                block: 'nearest',
                inline: 'nearest',
              });
            }
          }
        }, 50); // Small delay to ensure content is fully rendered
      });
    });
  }, [showAlternativeAgents]);

  const exportMarkdown = React.useCallback(async () => {
    try {
      const currentProjectId = projectId as string | undefined;
      if (!currentProjectId || currentProjectId === 'local-unfold') return;
      const text = (content || '').toString();
      const baseUnsafe = (derivedHeaderText ? `${derivedHeaderText}-contribution` : 'assistant-answer');
      const base = baseUnsafe.toLowerCase().replace(/[^a-z0-9-_]+/gi, '-').replace(/-+/g, '-').replace(/^-|-$/g, '') || 'assistant-answer';
      const ts = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
      const filename = `${base}-${ts}.md`;
      const { api } = await import('../lib/api');
      await api.docsCreateFromText(currentProjectId, { filename, contentType: 'text/markdown', text });
    } catch {}
  }, [content, projectId, derivedHeaderText]);

  const exportToDocx = React.useCallback(async () => {
    try {
      const currentProjectId = projectId as string | undefined;
      if (!currentProjectId || currentProjectId === 'local-unfold') return;
      
      // First, save as markdown document
      const text = (content || '').toString();
      const baseUnsafe = (derivedHeaderText ? `${derivedHeaderText}-contribution` : 'assistant-answer');
      const base = baseUnsafe.toLowerCase().replace(/[^a-z0-9-_]+/gi, '-').replace(/-+/g, '-').replace(/^-|-$/g, '') || 'assistant-answer';
      const ts = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
      const filename = `${base}-${ts}.md`;
      
      const { api } = await import('../lib/api');
      const doc = await api.docsCreateFromText(currentProjectId, { filename, contentType: 'text/markdown', text });
      
      // Then export to DOCX
      const blob = await api.docsExportToDocx(currentProjectId, doc.id);
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
  }, [content, projectId, derivedHeaderText]);
  // For user messages, only show ActionRow if there's bias evaluation
  // For assistant messages, show full ActionRow
  if (!content || !content.trim()) return null;
  
  // Get frame expansions from interactions with subkind 'eval.frame_expansions'
  const frameExpansions = React.useMemo(() => {
    try {
      const map = persistedByLocalId || {};
      const serverId = map[messageId] || messageId;
      
      // Build reverse map for bidirectional lookup
      const reverseMap: Record<string, string[]> = {};
      Object.entries(map).forEach(([local, server]) => {
        if (!reverseMap[server as string]) reverseMap[server as string] = [];
        reverseMap[server as string].push(local);
      });
      // Fix: use serverId to lookup in reverseMap, as reverseMap keys are server IDs
      const localIds = reverseMap[serverId] || [];
      
      // Find expansion interaction linked to this message
      const interactions = getInteractions();
      const expansionInteraction = interactions.find((it) => 
        it.subkind === 'eval.frame_expansions' && 
        (it.replyTo === messageId || it.replyTo === serverId || localIds.includes(it.replyTo || '') ||
         (it.relations && it.relations.some((r) => r.targetId === messageId || r.targetId === serverId || localIds.includes(r.targetId || ''))))
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
  }, [getInteractions, interactionsLength, messageId, persistedByLocalId]);
  
  // Get frame compliance check from interactions with subkind 'eval.frame_compliance'
  const frameComplianceCheck = React.useMemo(() => {
    try {
      const map = persistedByLocalId || {};
      const serverId = map[messageId] || messageId;
      
      // Build reverse map for bidirectional lookup
      const reverseMap: Record<string, string[]> = {};
      Object.entries(map).forEach(([local, server]) => {
        if (!reverseMap[server as string]) reverseMap[server as string] = [];
        reverseMap[server as string].push(local);
      });
      const localIds = reverseMap[serverId] || [];
      
      // Find compliance check interaction linked to this message
      const interactions = getInteractions();
      const complianceInteraction = interactions.find((it) => 
        it.subkind === 'eval.frame_compliance' && 
        (it.replyTo === messageId || it.replyTo === serverId || localIds.includes(it.replyTo || '') ||
         (it.relations && it.relations.some((r) => r.targetId === messageId || r.targetId === serverId || localIds.includes(r.targetId || ''))))
      );
      
      if (complianceInteraction) {
        try {
          const data = JSON.parse(complianceInteraction.content || complianceInteraction.contentSanitized || '{}');
          if (data.confidence !== undefined && data.explanation && typeof data.explanation === 'string') {
            // Only return if confidence >= 0.6
            if (data.confidence >= 0.6) {
              return { confidence: data.confidence, explanation: data.explanation };
            }
          }
        } catch (e) {
          console.warn('[ActionRow] Failed to parse frame compliance check', e);
        }
      }
      
      return undefined;
    } catch (e) { 
      // Silently fail - no need to log every time
      return undefined; 
    }
  }, [getInteractions, interactionsLength, messageId, persistedByLocalId]);
  
  // Get sources and search queries from interactions
  const messageSources = React.useMemo(() => {
    try {
      const map = persistedByLocalId || {};
      const serverId = map[messageId] || messageId;
      
      // Build reverse map for bidirectional lookup
      const reverseMap: Record<string, string[]> = {};
      Object.entries(map).forEach(([local, server]) => {
        if (!reverseMap[server as string]) reverseMap[server as string] = [];
        reverseMap[server as string].push(local);
      });
      const localIds = reverseMap[serverId] || [];
      
      // Find interaction linked to this message
      const interactions = getInteractions();
      const interaction = interactions.find((it) => 
        (it.id === messageId || it.id === serverId || localIds.includes(it.id || '') ||
         (it as any).originalMessageId === messageId || (it as any).originalMessageId === serverId)
      ) as any;
      
      if (!interaction) return { queries: [], sources: [] };
      
      const perQ = Array.isArray(interaction.searchQueries) 
        ? interaction.searchQueries.filter(Boolean)
        : [];
      const perS = Array.isArray(interaction.sources)
        ? interaction.sources.filter(Boolean)
        : [];
      
      return {
        queries: perQ,
        sources: perS.map((url: string) => ({ url })),
      };
    } catch {
      return { queries: [], sources: [] };
    }
  }, [getInteractions, interactionsLength, messageId, persistedByLocalId]);
  
  // Get bias evaluation data for button color
  const biasData = React.useMemo(() => {
    if (!hasEvalBias) return null;
    try {
      // First check genaiChat.evaluations (for user messages)
      if (evaluations && evaluations[messageId]) {
        const ev = evaluations[messageId];
        const level = Math.max(0, Math.min(1, Number(ev.biasLevel || 0)));
        const hue = Math.round(120 * (1 - level));
        const fill = theme === 'light' 
          ? `hsl(${hue}, 70%, 50%)`
          : `hsl(${hue}, 60%, 70%)`;
        const bucketLabel = level <= 0.2 
          ? 'neutral'
          : level <= 0.4
          ? 'balanced'
          : level <= 0.6
          ? 'subjective'
          : level <= 0.8
          ? 'oneSided'
          : 'biased';
        return { level, fill, bucketLabel };
      }
      
      // Then check interactions with subkind 'eval.bias'
      const map = persistedByLocalId || {};
      // Determine reliable server ID
      const effectiveServerId = map[messageId] || messageId;
      
      // Collect all known IDs for this message
      const targetIds = new Set<string>();
      targetIds.add(messageId);
      if (effectiveServerId) targetIds.add(effectiveServerId);
      
      // Add all local IDs that map to the server ID
      Object.entries(map).forEach(([local, server]) => {
        if (server === effectiveServerId) targetIds.add(local);
      });
      
      const interactions = getInteractions();
      const found = [...interactions].reverse().find((it: any) => 
        it.subkind === 'eval.bias' && 
        targetIds.has(it.replyTo || '')
      );
      if (!found) return null;
      try {
        const ev = JSON.parse((found as any).content || (found as any).contentSanitized || '{}');
        const level = Math.max(0, Math.min(1, Number(ev.biasLevel || 0)));
        const hue = Math.round(120 * (1 - level));
        // Use more saturated and darker color for light theme, lighter for dark theme
        const fill = theme === 'light' 
          ? `hsl(${hue}, 70%, 50%)`  // More saturated, darker for light theme
          : `hsl(${hue}, 60%, 70%)`; // Lighter for dark theme
        const bucketLabel = level <= 0.2 
          ? 'neutral'
          : level <= 0.4
          ? 'balanced'
          : level <= 0.6
          ? 'subjective'
          : level <= 0.8
          ? 'oneSided'
          : 'biased';
        return { level, fill, bucketLabel };
      } catch {
        return null;
      }
    } catch {
      return null;
    }
  }, [hasEvalBias, evaluations, getInteractions, interactionsLength, messageId, persistedByLocalId, theme]);
  
  const hasReframeContent = frameExpansions && frameExpansions.length > 0;
  const hasSources = (messageSources.queries.length > 0 || messageSources.sources.length > 0);
  
  return (
    <div className="mt-3" style={{ width: '100%', position: 'relative' }}>
      <div className="flex items-start gap-2">
        {hasEvalBias && biasData && (
          <button
            type="button"
            onClick={() => setShowBiasDetails(!showBiasDetails)}
            className="rounded-lg"
            style={{ 
              height: 24, 
              backgroundColor: showBiasDetails ? biasData.fill : 'transparent', 
              color: showBiasDetails ? (theme === 'light' ? '#000' : '#000') : 'var(--text-secondary-strong)', 
              display: 'flex', 
              alignItems: 'center', 
              gap: 6, 
              padding: '0 8px', 
              border: `1px solid ${biasData.fill}` 
            }}
            onMouseEnter={(e) => { 
              if (!showBiasDetails) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = biasData.fill;
                (e.currentTarget as HTMLButtonElement).style.color = theme === 'light' ? '#000' : '#000';
              }
            }}
            onMouseLeave={(e) => { 
              if (!showBiasDetails) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                (e.currentTarget as HTMLButtonElement).style.color = 'var(--text-secondary-strong)';
              }
            }}
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M12 2L2 7l10 5 10-5-10-5z" />
              <path d="M2 17l10 5 10-5" />
              <path d="M2 12l10 5 10-5" />
            </svg>
            <span className="text-[11px]">{t(`unfold.bias.${biasData.bucketLabel}`, { defaultValue: biasData.bucketLabel })}</span>
          </button>
        )}
        {hasReframeContent && (
          <button
            type="button"
            onClick={() => setShowReframe(!showReframe)}
            className="rounded-lg"
            style={{ 
              height: 24, 
              backgroundColor: showReframe ? 'var(--white-10)' : 'transparent', 
              color: 'var(--text-secondary-strong)', 
              display: 'flex', 
              alignItems: 'center', 
              gap: 6, 
              padding: '0 8px', 
              border: `1px solid ${showReframe ? 'var(--overlay-line-80)' : 'var(--overlay-line-90)'}` 
            }}
            onMouseEnter={(e) => { 
              if (!showReframe) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
              }
            }}
            onMouseLeave={(e) => { 
              if (!showReframe) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
              }
            }}
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" />
              <path d="M21 3v5h-5" />
              <path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" />
              <path d="M3 21v-5h5" />
            </svg>
            <span className="text-[11px]">{t('unfold.actions.reframeIt', { defaultValue: 'Reframe it?' })}</span>
          </button>
        )}
        {hasSources && (
          <button
            type="button"
            onClick={() => setShowSources(!showSources)}
            className="rounded-lg"
            style={{ 
              height: 24, 
              backgroundColor: showSources ? 'var(--white-10)' : 'transparent', 
              color: 'var(--text-secondary-strong)', 
              display: 'flex', 
              alignItems: 'center', 
              gap: 6, 
              padding: '0 8px', 
              border: `1px solid ${showSources ? 'var(--overlay-line-80)' : 'var(--overlay-line-90)'}` 
            }}
            onMouseEnter={(e) => { 
              if (!showSources) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
              }
            }}
            onMouseLeave={(e) => { 
              if (!showSources) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
              }
            }}
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M4 19.5A2.5 2.5 0 0 0 6.5 17H20" />
              <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" />
            </svg>
            <span className="text-[11px]">Sources</span>
          </button>
        )}
        {isAssistant && (
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
                // Get current date and time for temporal context
                const now = new Date();
                const currentDate = now.toLocaleDateString('en-US', { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric', timeZone: 'UTC' });
                const currentTime = now.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short', timeZone: 'UTC' });
                const currentDateTime = `${currentDate} at ${currentTime} UTC`;
                const temporalContextInstruction = `CURRENT DATE AND TIME: ${currentDateTime}\nYou must be aware of the current temporal context. Use this information to evaluate the recency and relevance of information, events, and data.`;
                const sxSystemInstruction = (
                  `Hidden-instructions protocol (do not show to users):\n` +
                  `- When you need to output service instructions (entities, metrics to monitor, watchlist, relations), emit ONLY HTML comments of the form <!-- @sx:{JSON} -->.\n` +
                  `- JSON must be valid and match v: \"1\" with one of types: entity_mark, monitor_metric, watchlist_add, link_reference.\n` +
                  `- Do not duplicate @sx content in visible markdown. Keep normal answer readable.\n` +
                  `- Examples: <!-- @sx:{\\\"v\\\":\\\"1\\\",\\\"type\\\":\\\"entity_mark\\\",\\\"id\\\":\\\"ent-001\\\",\\\"kind\\\":\\\"Person\\\",\\\"name\\\":\\\"…\\\",\\\"description\\\":\\\"…\\\"} -->`
                );
                const combinedSystemInstruction = [temporalContextInstruction, settings.systemInstruction || '', p.params?.systemInstruction || '', HATL_ONE_INSTRUCTIONS, sxSystemInstruction].filter(Boolean).join('\n\n').trim() || undefined;
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
                    const interactions = getInteractions();
                    const evalNode = [...interactions].reverse().find((it: any) => 
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
                      `${t('unfold.evidenceBuilder.sourcesUsed')}\n` +
                      `${t('unfold.evidenceBuilder.sourcesFormat')}\n` +
                      `• Author, A. (Year). Title. Publisher. — Description\n` +
                      `• Author, B. (Year). Title. URL — Description\n` +
                      `${t('unfold.evidenceBuilder.sourcesCritical')}\n\n` +
                      `${t('unfold.evidenceBuilder.assumptions')}\n• [Assumption]: How it was interpreted and applied\n[...]\n\n` +
                      `${t('unfold.evidenceBuilder.couldNotVerify')}\n• [Claim lacking sufficient evidence]\n[...]\n`
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
        )}
      </div>
      {showBiasDetails && hasEvalBias && (
        <div className="mt-2" style={{ width: '100%' }}>
          <EvaluationDetails messageId={messageId} />
        </div>
      )}
      {showReframe && frameExpansions && frameExpansions.length > 0 && (
        <div ref={reframeRef} className="mt-2" style={{ width: '100%' }}>
          <div className="p-2" style={{ backgroundColor: 'var(--white-05)', borderLeft: '2px solid var(--overlay-line-90)' }}>
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
            {/* Alternative opinion button */}
            <div className="mt-3" style={{ position: 'relative' }} ref={alternativeAgentsRef}>
              <button
                type="button"
                onClick={() => setShowAlternativeAgents(!showAlternativeAgents)}
                className="rounded-lg"
                style={{ 
                  height: 24, 
                  backgroundColor: showAlternativeAgents ? 'var(--white-10)' : 'transparent', 
                  color: 'var(--text-secondary-strong)', 
                  display: 'flex', 
                  alignItems: 'center', 
                  gap: 6, 
                  padding: '0 8px', 
                  border: `1px solid ${showAlternativeAgents ? 'var(--overlay-line-80)' : 'var(--overlay-line-90)'}` 
                }}
                onMouseEnter={(e) => { 
                  if (!showAlternativeAgents) {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                  }
                }}
                onMouseLeave={(e) => { 
                  if (!showAlternativeAgents) {
                    (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                  }
                }}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                  <path d="M12 7a3 3 0 1 1 0 6a3 3 0 0 1 0-6" />
                  <path d="M5 20c0-3.314 3.134-6 7-6s7 2.686 7 6" />
                  <path d="M18 7v4M16 9h4" />
                </svg>
                <span className="text-[11px]">{t('unfold.actions.alternativeOpinion')}</span>
              </button>
              {/* Agents list */}
              {showAlternativeAgents && (
                <div
                  className="mt-2 p-2 rounded-lg"
                  style={{
                    backgroundColor: 'var(--bg-elevated)',
                    border: '1px solid var(--overlay-line-90)',
                    display: 'flex',
                    flexWrap: 'wrap',
                    gap: '8px',
                    animation: 'fadeInScale 0.2s ease-out',
                  }}
                >
                  {AGENT_PRESETS.map((preset) => (
                    <button
                      key={preset.id}
                      type="button"
                      aria-label={preset.displayName}
                      title={preset.description}
                      onClick={async () => {
                        setShowAlternativeAgents(false);
                        try {
                          const tools = settings.enableTelegramTool ? [TelegramFetchUpdatesTool] : undefined;
                          const model = preset.modelAlias;
                          // Get current date and time for temporal context
                          const now = new Date();
                          const currentDate = now.toLocaleDateString('en-US', { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric', timeZone: 'UTC' });
                          const currentTime = now.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short', timeZone: 'UTC' });
                          const currentDateTime = `${currentDate} at ${currentTime} UTC`;
                          const temporalContextInstruction = `CURRENT DATE AND TIME: ${currentDateTime}\nYou must be aware of the current temporal context. Use this information to evaluate the recency and relevance of information, events, and data.`;
                          const sxSystemInstruction = (
                            `Hidden-instructions protocol (do not show to users):\n` +
                            `- When you need to output service instructions (entities, metrics to monitor, watchlist, relations), emit ONLY HTML comments of the form <!-- @sx:{JSON} -->.\n` +
                            `- JSON must be valid and match v: \"1\" with one of types: entity_mark, monitor_metric, watchlist_add, link_reference.\n` +
                            `- Do not duplicate @sx content in visible markdown. Keep normal answer readable.\n` +
                            `- Examples: <!-- @sx:{\\\"v\\\":\\\"1\\\",\\\"type\\\":\\\"entity_mark\\\",\\\"id\\\":\\\"ent-001\\\",\\\"kind\\\":\\\"Person\\\",\\\"name\\\":\\\"…\\\",\\\"description\\\":\\\"…\\\"} -->`
                          );
                          const combinedSystemInstruction = [temporalContextInstruction, settings.systemInstruction || '', preset.params?.systemInstruction || '', HATL_ONE_INSTRUCTIONS, sxSystemInstruction].filter(Boolean).join('\n\n').trim() || undefined;
                          const options: any = {
                            profile: /_quality$/i.test(model) ? 'quality' : 'fast',
                            modelOverride: model,
                            groundingEnabled: preset.capabilities?.webSearch ?? settings.groundingEnabled,
                            temperature: typeof preset.params?.temperature === 'number' ? preset.params?.temperature : settings.temperature,
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
                          
                          // Include bias evaluation of this answer to help the agent calibrate its stance
                          const biasBlock = (() => {
                            try {
                              const serverId = map[messageId];
                              const localIds = reverseMap[messageId] || [];
                              const interactions = getInteractions();
                              const evalNode = [...interactions].reverse().find((it: any) => 
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
                            if ((preset.id && /evidence-builder/i.test(preset.id)) || /evidence builder/i.test(preset.displayName || '')) {
                              return (
                                `User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if they are standard terms in target language.\n\n` +
                                `You are ${preset.displayName}, an invited AI participant. Your role is to DEEPEN and VERIFY the ASSISTANT's answer by building a comprehensive evidence base.\n\n` +
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
                                `${t('unfold.evidenceBuilder.sourcesUsed')}\n` +
                                `${t('unfold.evidenceBuilder.sourcesFormat')}\n` +
                                `• Author, A. (Year). Title. Publisher. — Description\n` +
                                `• Author, B. (Year). Title. URL — Description\n` +
                                `${t('unfold.evidenceBuilder.sourcesCritical')}\n\n` +
                                `${t('unfold.evidenceBuilder.assumptions')}\n• [Assumption]: How it was interpreted and applied\n[...]\n\n` +
                                `${t('unfold.evidenceBuilder.couldNotVerify')}\n• [Claim lacking sufficient evidence]\n[...]\n`
                              );
                            }
                            // Default counterpoint-style prompt for other agents
                            return (
                              `User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\n` +
                              `You are ${preset.displayName}, an invited AI participant analyzing an AI assistant's response to a user query.\n\n` +
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
                          const indicatorContent = `${t('unfold.actions.agentInvoked')} ${preset.displayName}`;
                          await useGenaiChatStore.getState().createIndicatorMessage(indicatorContent, 'action.bring_in', serverMessageId, { agentProfile: { provider: preset.provider, model: model, capabilities: { tools: !!preset.capabilities?.tools, webSearch: !!preset.capabilities?.webSearch, reasoning: !!preset.capabilities?.reasoning, refinePrevious: !!preset.capabilities?.refinePrevious }, params: preset.params } });
                          await useGenaiChatStore.getState().startStream({ prompt, tools, options: { ...options, __lang: preferredLang }, agentMeta: { displayName: preset.displayName, agentProfile: { provider: preset.provider, model: model, capabilities: { tools: !!preset.capabilities?.tools, webSearch: !!preset.capabilities?.webSearch, reasoning: !!preset.capabilities?.reasoning, refinePrevious: !!preset.capabilities?.refinePrevious }, params: preset.params }, subkind: 'agent.bring_in', replyTo: serverMessageId }, suppressUserEcho: true });
                        } catch (err) {
                          console.error('[ActionRow] Failed to invoke alternative agent', err);
                        }
                      }}
                      className="flex items-center gap-2 rounded-lg px-2 py-1 text-[11px] transition-colors"
                      style={{
                        backgroundColor: 'var(--white-10)',
                        color: 'var(--text-secondary-strong)',
                        border: '1px solid var(--overlay-line-90)',
                      }}
                      onMouseEnter={(e) => {
                        (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-15)';
                      }}
                      onMouseLeave={(e) => {
                        (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                      }}
                    >
                      {preset.icon || (
                        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                          <circle cx="12" cy="12" r="10" />
                          <path d="M12 16v-4M12 8h.01" />
                        </svg>
                      )}
                      <span>{preset.displayName}</span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      )}
      {isAssistant && frameComplianceCheck && frameComplianceCheck.confidence >= 0.6 && (
        <div className="mt-2" style={{ width: '100%' }}>
          <div className="p-2" style={{ backgroundColor: 'var(--warning-10)', borderLeft: '2px solid var(--accent-warning)', borderRadius: '4px' }}>
            <div className="text-[11px]" style={{ color: 'var(--text-heading)', lineHeight: '1.4' }}>
              {frameComplianceCheck.explanation}
            </div>
            <button
              type="button"
              onClick={async () => {
                try {
                  const secondOpinionPreset = AGENT_PRESETS.find(p => p.id === 'second-opinion');
                  if (!secondOpinionPreset) {
                    console.error('[ActionRow] Second Opinion agent not found');
                    return;
                  }
                  
                  const tools = settings.enableTelegramTool ? [TelegramFetchUpdatesTool] : undefined;
                  const model = secondOpinionPreset.modelAlias;
                  
                  // Get current date and time for temporal context
                  const now = new Date();
                  const currentDate = now.toLocaleDateString('en-US', { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric', timeZone: 'UTC' });
                  const currentTime = now.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short', timeZone: 'UTC' });
                  const currentDateTime = `${currentDate} at ${currentTime} UTC`;
                  const temporalContextInstruction = `CURRENT DATE AND TIME: ${currentDateTime}\nYou must be aware of the current temporal context. Use this information to evaluate the recency and relevance of information, events, and data.`;
                  const sxSystemInstruction = getSxSystemInstructionFull();
                  const combinedSystemInstruction = [temporalContextInstruction, settings.systemInstruction || '', secondOpinionPreset.params?.systemInstruction || '', HATL_ONE_INSTRUCTIONS, sxSystemInstruction].filter(Boolean).join('\n\n').trim() || undefined;
                  
                  const options: any = {
                    profile: /_quality$/i.test(model) ? 'quality' : 'fast',
                    modelOverride: model,
                    groundingEnabled: secondOpinionPreset.capabilities?.webSearch ?? settings.groundingEnabled,
                    temperature: typeof secondOpinionPreset.params?.temperature === 'number' ? secondOpinionPreset.params?.temperature : settings.temperature,
                    maxOutputTokens: settings.maxOutputTokens,
                    systemInstruction: combinedSystemInstruction,
                  };
                  
                  const preferredLang = getPreferredLanguage();
                  const langName = getLanguageName(preferredLang);
                  const st = useGenaiChatStore.getState();
                  
                  // Find the specific assistant message and related user message
                  const map = (st as any).persistedByLocalId || {};
                  const serverMessageId = map[messageId] || messageId;
                  
                  // Build reverse map for bidirectional lookup
                  const reverseMap: Record<string, string[]> = {};
                  Object.entries(map).forEach(([local, server]) => {
                    if (!reverseMap[server as string]) reverseMap[server as string] = [];
                    reverseMap[server as string].push(local);
                  });
                  const localIds = reverseMap[serverMessageId] || [];
                  
                  // Combine messages and interactions
                  const allItems = [...st.interactions, ...st.messages];
                  
                  // Find target assistant message
                  const targetAssistantMsg = allItems.find((m: any) => 
                    m.id === messageId || 
                    m.id === serverMessageId || 
                    (m as any).originalMessageId === messageId ||
                    (m as any).originalMessageId === serverMessageId ||
                    localIds.includes(m.id || '')
                  ) || null;
                  
                  if (!targetAssistantMsg) {
                    console.error('[ActionRow] Target assistant message not found');
                    return;
                  }
                  
                  // Find the last user message before this assistant message
                  const assistantIdx = allItems.findIndex((m: any) => 
                    m.id === messageId || 
                    m.id === serverMessageId || 
                    (m as any).originalMessageId === messageId ||
                    (m as any).originalMessageId === serverMessageId ||
                    localIds.includes(m.id || '')
                  );
                  
                  let lastUserMsg: any = null;
                  if (assistantIdx > 0) {
                    // Search backwards for user message
                    for (let i = assistantIdx - 1; i >= 0; i--) {
                      const item = allItems[i];
                      const role = (item as any).role || (item as any).kind || '';
                      const isUser = role === 'user' || role === 'discovery.query';
                      const content = (item as any).content || (item as any).contentSanitized || '';
                      if (isUser && content && content.trim()) {
                        lastUserMsg = item;
                        break;
                      }
                    }
                  }
                  
                  // Build context with exactly 3 messages: user, assistant, compliance warning
                  const contextMessages: Array<{ role: string; content: string }> = [];
                  
                  if (lastUserMsg) {
                    const userContent = lastUserMsg.content || (lastUserMsg as any).contentSanitized || '';
                    if (userContent.trim()) {
                      contextMessages.push({ role: 'user', content: userContent });
                    }
                  }
                  
                  const assistantContent = targetAssistantMsg.content || (targetAssistantMsg as any).contentSanitized || '';
                  if (assistantContent.trim()) {
                    contextMessages.push({ role: 'assistant', content: assistantContent });
                  }
                  
                  // Add compliance warning as a system message
                  contextMessages.push({ 
                    role: 'system', 
                    content: `COMPLIANCE WARNING: ${frameComplianceCheck.explanation}` 
                  });
                  
                  // Build transcript
                  const transcript = contextMessages.map(m => 
                    `${m.role.toUpperCase()}: ${m.content}`
                  ).join('\n\n');
                  
                  // Build prompt for Second Opinion agent
                  const prompt = (
                    `User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\n` +
                    `You are ${secondOpinionPreset.displayName}, an invited AI participant. Your role is to provide an alternative well-founded perspective considering the compliance warning.\n\n` +
                    `CONTEXT:\n${transcript}\n\n` +
                    `TASK:\n` +
                    `1) Review the user's question, the assistant's response, and the compliance warning.\n` +
                    `2) Provide an alternative well-founded perspective that:\n` +
                    `   - Addresses the same question from a different angle\n` +
                    `   - Challenges the assumptions identified in the compliance warning\n` +
                    `   - Offers evidence-based alternative viewpoints\n` +
                    `   - Remains constructive and respectful\n` +
                    `3) Be less strict than Socratic Counterpoint - focus on offering alternatives rather than rigorous critique.\n` +
                    `4) Write clearly and accessibly, using natural structure.\n` +
                    `5) Prioritize practical insights.\n\n` +
                    `OUTPUT:\n` +
                    `Provide your alternative well-founded perspective directly, without prefaces or meta-commentary.`
                  );
                  
                  // Create indicator message before calling agent
                  const indicatorContent = `${t('unfold.actions.agentInvoked')} ${secondOpinionPreset.displayName}`;
                  await useGenaiChatStore.getState().createIndicatorMessage(indicatorContent, 'action.bring_in', serverMessageId, { agentProfile: { provider: secondOpinionPreset.provider, model: model, capabilities: { tools: !!secondOpinionPreset.capabilities?.tools, webSearch: !!secondOpinionPreset.capabilities?.webSearch, reasoning: !!secondOpinionPreset.capabilities?.reasoning, refinePrevious: !!secondOpinionPreset.capabilities?.refinePrevious }, params: secondOpinionPreset.params } });
                  await useGenaiChatStore.getState().startStream({ prompt, tools, options: { ...options, __lang: preferredLang }, agentMeta: { displayName: secondOpinionPreset.displayName, agentProfile: { provider: secondOpinionPreset.provider, model: model, capabilities: { tools: !!secondOpinionPreset.capabilities?.tools, webSearch: !!secondOpinionPreset.capabilities?.webSearch, reasoning: !!secondOpinionPreset.capabilities?.reasoning, refinePrevious: !!secondOpinionPreset.capabilities?.refinePrevious }, params: secondOpinionPreset.params }, subkind: 'agent.bring_in', replyTo: serverMessageId }, suppressUserEcho: true });
                } catch (err) {
                  console.error('[ActionRow] Failed to invoke Second Opinion agent', err);
                }
              }}
              className="mt-2 rounded-lg px-2 py-1 text-[11px] transition-colors"
              style={{
                backgroundColor: 'var(--white-10)',
                color: 'var(--text-secondary-strong)',
                border: '1px solid var(--overlay-line-90)',
                display: 'flex',
                alignItems: 'center',
                gap: 6,
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-15)';
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
              }}
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                <path d="M8 9c0 1.5-1.5 3-3 3s-3-1.5-3-3 1.5-3 3-3 3 1.5 3 3" />
                <path d="M8 9H5c-1.5 0-3 1.5-3 3v2" />
                <path d="M16 9c0 1.5 1.5 3 3 3s3-1.5 3-3-1.5-3-3-3-3 1.5-3 3" />
                <path d="M16 9h3c1.5 0 3 1.5 3 3v2" />
                <circle cx="12" cy="9" r="1.5" />
              </svg>
              <span>{t('unfold.actions.secondOpinion')}</span>
            </button>
          </div>
        </div>
      )}
      {showSources && hasSources && (
        <div className="mt-2" style={{ width: '100%' }}>
          <div className="p-2" style={{ backgroundColor: 'var(--white-05)', borderLeft: '2px solid var(--overlay-line-90)' }}>
            <div className="text-[11px] mb-1.5 font-medium" style={{ color: 'var(--text-heading)' }}>
              Sources:
            </div>
            <div className="flex flex-wrap gap-1">
              {messageSources.queries.map((q: string, i: number) => {
                const words = String(q || '').trim().split(/\s+/);
                const short = words.length > 3
                  ? words.slice(0, 3).join(' ') + '…'
                  : String(q || '');
                return (
                  <button
                    key={`q-${i}`}
                    type="button"
                    className="px-2 py-0.5 rounded-full text-[10px] transition-colors"
                    style={{
                      backgroundColor: 'var(--white-10)',
                      color: 'var(--text-secondary-strong)',
                      border: 'none',
                      cursor: 'default',
                    }}
                    onMouseEnter={(e) => {
                      (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-15)';
                    }}
                    onMouseLeave={(e) => {
                      (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                    }}
                  >
                    {short}
                  </button>
                );
              })}
              {messageSources.sources.map((a: any, i: number) => {
                const raw = String((a as any).url || '').replace(/^https?:\/\//i, '');
                const parts = raw.split('/');
                const host = parts[0] || '';
                const tail = parts[1] ? `/${parts[1]}` : '';
                const label = host + tail || (a as any).title || 'link';
                return (
                  <a
                    key={`src-${i}`}
                    href={(a as any).url}
                    target="_blank"
                    rel="noreferrer"
                    className="px-2 py-0.5 rounded-full text-[10px] underline transition-colors"
                    style={{
                      backgroundColor: 'var(--white-10)',
                      color: 'var(--text-secondary-strong)',
                    }}
                    onMouseEnter={(e) => {
                      (e.currentTarget as HTMLAnchorElement).style.backgroundColor = 'var(--white-15)';
                    }}
                    onMouseLeave={(e) => {
                      (e.currentTarget as HTMLAnchorElement).style.backgroundColor = 'var(--white-10)';
                    }}
                  >
                    {label}
                  </a>
                );
              })}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

