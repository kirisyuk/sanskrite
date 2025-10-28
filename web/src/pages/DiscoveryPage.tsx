import React, { useState, useEffect, useRef } from 'react';
import ReactMarkdown, { Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import Settings from '../components/Settings';
import AdminAccess from '../components/AdminAccess';
import TermsOfUsage from './TermsOfUsage';
import PrivacyNotice from './PrivacyNotice';
import { useProjectsStore } from '../stores/projectsStore';
import { useDiscoveryStore } from '../stores/discoveryStore';
import { useAuth } from '../contexts/AuthContext';
import { api } from '../lib/api';
import SourcePreview from '../components/SourcePreview';
import { useUIStore } from '../stores/uiStore';
import DiscoveryRequest from '../components/DiscoveryRequest';
import UnfoldRequest from '../components/UnfoldRequest';
import ChatMessage from '../components/ChatMessage';
import AgentPalette from '../components/AgentPalette';
import DocumentsAndSources from '../components/DocumentsAndSources';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { TelegramFetchUpdatesTool } from '../lib/genaiTools';
import { ResponsiveContainer, LineChart, Line, XAxis, YAxis, CartesianGrid, Legend, Tooltip } from 'recharts';
import { useTranslation } from 'react-i18next';
import { AGENT_PRESETS } from '../lib/agentPresets';
import DocumentViewer from '../components/DocumentViewer';

const ArtifactsAnalysisStatus: React.FC = () => {
  const status = useDiscoveryStore((s: any) => (s as any).lastAnalysisStatus);
  if (!status) return null;
  const color = status.status === 'started' ? 'var(--text-secondary-strong)' : (status.status === 'done' ? 'var(--green-50)' : 'var(--error-50)');
  const label = `${status.kind}: ${status.status}` + (status.count != null ? ` (${status.count})` : '') + (status.elapsedMs != null && status.status !== 'started' ? ` · ${status.elapsedMs}ms` : '');
  return (
    <div className="text-xs" style={{ color }}>
      {label}
    </div>
  );
};

// Lightweight Entities list component
const EntitiesList: React.FC<{ projectId: string }> = ({ projectId }) => {
  const [items, setItems] = React.useState<Array<{ id?: string; name: string; type: string; description?: string; excluded?: boolean }>>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [open, setOpen] = React.useState<boolean>(false);
  const [expanded, setExpanded] = React.useState<Record<string, boolean>>({});
  const reload = React.useCallback(async () => {
    if (!projectId || projectId === 'local-unfold') return;
    setLoading(true); setError(null);
    try {
      const docs = await api.docsList(projectId);
      const ents = docs.find((d: any) => String(d.filename || '').toLowerCase() === 'entities.json');
      if (!ents?.id) { setItems([]); setLoading(false); return; }
      const blob = await api.docsDownload(projectId, ents.id);
      const text = await new Response(blob).text();
      const json = JSON.parse(text || '{}');
      const arr = Array.isArray(json?.entities) ? json.entities : [];
      setItems(arr.filter((e: any) => !e?.excluded));
    } catch (e: any) {
      setError('Failed to load entities');
      setItems([]);
    } finally { setLoading(false); }
  }, [projectId]);
  React.useEffect(() => { reload(); }, [reload]);

  const count = items.length;

  const typeIcon = (t: string) => {
    const stroke = 'currentColor';
    switch (t) {
      case 'person':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><circle cx="12" cy="8" r="3"/><path d="M4 20c0-3.314 3.134-6 7-6s7 2.686 7 6"/></svg>);
      case 'organization_or_platform':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M21 16V8a2 2 0 00-1-1.73l-7-4a2 2 0 00-2 0l-7 4A2 2 0 003 8v8a2 2 0 001 1.73l7 4a2 2 0 002 0l7-4A2 2 0 0021 16z"/><circle cx="12" cy="7" r="1"/><path d="M7.5 10l2.5 1.45v2.9L7.5 16.5"/><path d="M16.5 10L14 11.45v2.9l2.5 1.45"/><path d="M16.5 16.5l-2.5-1.45v-2.9l2.5-1.45"/></svg>);
      case 'project_or_product':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>);
      case 'classifier_or_standard':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 10h3M7 14h3M7 7h6"/><circle cx="15.5" cy="12.5" r="2.5"/></svg>);
      case 'classification_code':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><rect x="5" y="4" width="14" height="16" rx="2"/><path d="M8 9h8M8 12h8M8 15h4"/></svg>);
      case 'regulatory_document':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M7 3h10l5 5v13H7z"/><path d="M7 3v7h7"/><path d="M9 13h6M9 17h6"/></svg>);
      case 'government_agency':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><rect x="4" y="3" width="16" height="16" rx="1"/><path d="M4 7h16M8 11h8M8 15h6"/><path d="M8 3v4M16 3v4"/></svg>);
      case 'contract_document':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M6 2h12v20H6z"/><path d="M9 7h6M9 11h6M9 15h4"/><path d="M6 2l3 6-3 6M18 2l-3 6 3 6"/></svg>);
      case 'location':
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>);
      default:
        return (<svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke={stroke}><circle cx="12" cy="12" r="9"/></svg>);
    }
  };

  const header = (
    <button type="button" onClick={() => setOpen(!open)} className="w-full flex items-center justify-between px-2 py-1" style={{ border: 'none', background: 'transparent', cursor: 'pointer' }}>
      <div className="flex items-center gap-2" style={{ color: 'var(--text-heading)' }}>
        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="4" y="5" width="16" height="4" rx="1"/><rect x="4" y="11" width="16" height="4" rx="1"/><rect x="4" y="17" width="16" height="2" rx="1"/></svg>
        <span className="text-[11px] font-medium">Entities ({count})</span>
      </div>
      <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ transform: open ? 'rotate(90deg)' : 'rotate(0deg)', transition: 'transform 0.15s ease' }}><path d="M9 6l6 6-6 6"/></svg>
    </button>
  );

  if (loading) return <div className="text-11" style={{ color: 'var(--text-secondary-strong)' }}>Loading…</div>;
  if (error) return <div className="text-11" style={{ color: 'var(--accent-danger)' }}>{error}</div>;
  if (items.length === 0) return (
    <div className="space-y-1">
      {header}
      {open && (<div className="text-11 px-2" style={{ color: 'var(--text-secondary-strong)' }}>No entities yet. Run "analyze entities".</div>)}
    </div>
  );

  return (
    <div className="space-y-1">
      {header}
      {open && (
        <div className="grid" style={{ gridTemplateColumns: '1fr', gap: 6 }}>
          {items.map((e, i) => {
            const key = `${e.name}-${e.type}-${i}`;
            const isExp = !!expanded[key];
            return (
              <button
                key={key}
                type="button"
                onClick={() => setExpanded((s) => ({ ...s, [key]: !isExp }))}
                className="p-2 rounded text-left w-full"
                style={{ background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)' }}
              >
                <div className="flex items-center gap-2 justify-between">
                  <div className="flex items-center gap-2">
                    <span style={{ color: 'var(--text-secondary-strong)' }}>{typeIcon(e.type)}</span>
                    <span className="text-12 font-medium" style={{ color: 'var(--text-heading)' }}>{e.name}</span>
                  </div>
                  <span className="text-[10px] px-2 py-0.5 rounded" style={{ background: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}>{e.type}</span>
                </div>
                {isExp && e.description && (
                  <div className="mt-1 text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>{e.description}</div>
                )}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
};
import { ThemeContext } from '../components/ThemeProvider';
import { toggleInteractionPin, isInteractionPinned } from '../lib/interactionHelpers';
import { highlightNotes, clearHighlights, findFirstRange, applyGlobalHighlight, clearGlobalHighlight } from '../lib/textHighlight';
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

// Inline evaluation details renderer (reads eval.bias interactions)
const EvaluationDetails: React.FC<{ messageId: string }> = ({ messageId }) => {
  const genaiChat = useGenaiChatStore();
  // Hydrate interactions when a concrete project is selected
  React.useEffect(() => {
    try {
      if (genaiChat.projectId && genaiChat.projectId !== 'local-unfold') {
        genaiChat.hydrateInteractions();
        genaiChat.ensureStanceAggregate();
      }
    } catch {}
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [genaiChat.projectId]);
  const { t } = useTranslation();
  const ev = React.useMemo(() => {
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
      const found = [...genaiChat.interactions].reverse().find((it: any) => 
        it.subkind === 'eval.bias' && 
        (it.replyTo === messageId || (serverId && it.replyTo === serverId) || localIds.includes(it.replyTo || ''))
      );
      if (!found) return null;
      try { return JSON.parse((found as any).content || '{}'); } catch { try { return JSON.parse((found as any).contentSanitized || '{}'); } catch { return null; } }
    } catch { return null; }
  }, [genaiChat.interactions, (genaiChat as any).persistedByLocalId, messageId]);
  const [open, setOpen] = React.useState(false);
  const msg = React.useMemo(() => genaiChat.messages.find((m) => m.id === messageId), [genaiChat.messages, messageId]);
  const { theme } = React.useContext(ThemeContext);
  if (!ev) return null;
  const level = Math.max(0, Math.min(1, Number(ev.biasLevel || 0)));
  const pct = Math.round(level * 100);
  const hue = Math.round(120 * (1 - level));
  const fill = `hsl(${hue}, 60%, 70%)`;
  const labelLevel = Math.round(level * 10) / 10;
  const bucketLabel = (level <= 0.1) ? 'Neutral' : (level <= 0.4) ? 'Mild tilt' : (level <= 0.7) ? 'Biased' : 'Strong bias';

  const tLevel = Math.max(0, Math.min(1, Number(ev.thoughtfulness || 0)));
  const tPct = Math.round(tLevel * 100);
  const tHue = Math.round(120 * (tLevel));
  const tFill = `hsl(${tHue}, 60%, 70%)`;
  const tLabel = tLevel <= 0.2 ? 'Shallow' : tLevel <= 0.5 ? 'Basic' : tLevel <= 0.8 ? 'Nuanced' : 'Deep';

  return (
    <div className="mt-1" style={{ width: '100%', position: 'relative' }}>
      <button
        type="button"
        className="flex items-center gap-2 w-full text-left"
        onClick={() => setOpen(!open)}
        style={{ background: 'transparent', border: 'none', padding: 0, cursor: 'pointer' }}
      >
        <div style={{ width: 120, height: 6, backgroundColor: (theme === 'dark' && msg?.role === 'user') ? 'var(--white-20)' : 'var(--overlay-line-90)', borderRadius: 4, overflow: 'hidden' }}>
          <div style={{ width: pct + '%', height: '100%', backgroundColor: fill }} />
        </div>
        <div className="text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>{labelLevel.toFixed(1)} {bucketLabel}</div>
      </button>
      {open && (
        <div className="mt-2 space-y-2">
          {Array.isArray(ev.biases) && ev.biases.length > 0 && (
            <div className="text-[12px]" style={{ color: 'var(--text-secondary-strong)' }}>
              {ev.biases.map((b: string, i: number) => (
                <div key={i} className="mb-1">• {b}</div>
              ))}
            </div>
          )}
          {Array.isArray(ev.biasTypes) && ev.biasTypes.length > 0 && (
            <div className="flex flex-wrap gap-1">
              {ev.biasTypes.map((t: string, i: number) => (
                <span key={i} className="px-2 py-0.5 rounded-full text-[10px]" style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}>{t}</span>
              ))}
            </div>
          )}
          <div className="flex items-center gap-2">
            <div style={{ width: 120, height: 6, backgroundColor: 'var(--overlay-line-90)', borderRadius: 4, overflow: 'hidden' }}>
              <div style={{ width: tPct + '%', height: '100%', backgroundColor: tFill }} />
            </div>
            <div className="text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>{(Math.round(tLevel*10)/10).toFixed(1)} {tLabel}</div>
          </div>
        </div>
      )}
    </div>
  );
};

// Action row under assistant messages: shows bias details when available,
// otherwise shows action buttons (Bring In, Copy) immediately after message
const ActionRow: React.FC<{ messageId: string; isAssistant: boolean; content?: string }> = ({ messageId, isAssistant, content }) => {
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
    try { return genaiChat.interactions.find((x) => x.id === messageId); } catch { return undefined as any; }
  }, [genaiChat.interactions, messageId]);
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
  if (!isAssistant || !content || !content.trim()) return null;
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
                  anchorRect={(() => {
                    try {
                      const el = (document.querySelector('#unfold-request-container') || document.querySelector('#app') || document.body) as HTMLElement;
                      const rect = el.getBoundingClientRect();
                      return { top: rect.top - 8, left: rect.left, width: rect.width, height: rect.height };
                    } catch { return null as any; }
                  })()}
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
                      const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
                      const agentName = displayName || 'agent';
                      const prof = derivedAgentProfile;
                      const prompt = `User requirement: you should create your answer in ${preferredLang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nYou are ${agentName}. The user has a follow-up question to your previous answer.\n\nOriginal user question (context):\n${origUser || '(not found)'}\n\nYour previous answer:\n${agentAnswer}\n\nUser follow-up (address this directly):\n${question}`;
                      const tools = settings.enableTelegramTool ? [TelegramFetchUpdatesTool] : undefined;
                      const useModel = prof?.model || settings.model;
                      const sxSystemInstruction = (
                        `Hidden-instructions protocol (do not show to users):\n` +
                        `- When you need to output service instructions (entities, metrics to monitor, watchlist, relations), emit ONLY HTML comments of the form <!-- @sx:{JSON} -->.\n` +
                        `- JSON must be valid and match v: \"1\" with one of types: entity_mark, monitor_metric, watchlist_add, link_reference.\n` +
                        `- Do not duplicate @sx content in visible markdown. Keep normal answer readable.\n` +
                        `- Examples: <!-- @sx:{\\\"v\\\":\\\"1\\\",\\\"type\\\":\\\"entity_mark\\\",\\\"id\\\":\\\"ent-001\\\",\\\"kind\\\":\\\"person\\\",\\\"name\\\":\\\"…\\\",\\\"description\\\":\\\"…\\\"} -->`
                      );
                      const combinedSystemInstruction = [settings.systemInstruction || '', sxSystemInstruction].filter(Boolean).join('\n\n').trim() || undefined;
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
                    const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
                    const contribution = msgs.find((m) => m.id === messageId)?.content || '';
                    const prompt = `User requirement: you should create your answer in ${preferredLang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nAs an expert, you are participating in a multi-agent discussion. Your task is to refine your own previous answer by integrating the contribution from another agent.\n\nRequirements:\n- Rewrite the Original Answer by integrating the Agent Contribution into a single, improved answer.\n- Do not comment on the changes, the agent, or the process; no meta-remarks; do not ask questions.\n- Use the same language and tone as the Original Answer; keep or improve structure (headings, bullets).\n- Be concise; remove redundancies; keep key facts only.\n- Weave added counterpoints into their relevant sections rather than appending a separate critique.\n- Output only the refined answer with no preface, labels, or extra text.\n\nOriginal Answer:\n${prevPrimary}\n\nAgent Contribution:\n${contribution}\n\nRefined Answer:`;
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
                const combinedSystemInstruction = [settings.systemInstruction || '', p.params?.systemInstruction || '', sxSystemInstruction].filter(Boolean).join('\n\n').trim() || undefined;
                      const options: any = {
                        profile: /_quality$/i.test(model) ? 'quality' : 'fast',
                        modelOverride: model,
                        groundingEnabled: p.capabilities?.webSearch ?? settings.groundingEnabled,
                        temperature: typeof p.params?.temperature === 'number' ? p.params?.temperature : settings.temperature,
                        maxOutputTokens: settings.maxOutputTokens,
                  systemInstruction: combinedSystemInstruction,
                      };
                      const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
                      const st = useGenaiChatStore.getState();
                      // Find the specific message that triggered Bring In and the previous user message
                      const msgs = st.messages.filter((m) => m.content && m.content.trim());
                      const targetIdx = msgs.findIndex((m) => m.id === messageId);
                      const targetMsg = targetIdx >= 0 ? msgs[targetIdx] : null;
                      let prevUserMsg: typeof msgs[0] | null = null;
                      if (targetIdx >= 0) {
                        for (let i = targetIdx - 1; i >= 0; i--) {
                          if (msgs[i].role === 'user') {
                            prevUserMsg = msgs[i];
                            break;
                          }
                        }
                      }
                      const transcript = [
                        prevUserMsg ? `USER: ${prevUserMsg.content}` : null,
                        targetMsg ? `ASSISTANT: ${targetMsg.content}` : null,
                      ].filter(Boolean).join('\n');
                      // Include bias evaluation of this answer to help the agent calibrate its stance
                      const biasBlock = (() => {
                        try {
                          const map = (genaiChat as any).persistedByLocalId || {};
                          const serverId = map[messageId];
                          const reverseMap: Record<string, string[]> = {};
                          Object.entries(map).forEach(([local, server]) => {
                            if (!reverseMap[server as string]) reverseMap[server as string] = [];
                            reverseMap[server as string].push(local);
                          });
                          const localIds = reverseMap[messageId] || [];
                          const evalNode = [...genaiChat.interactions].reverse().find((it: any) => 
                            it.subkind === 'eval.bias' && 
                            (it.replyTo === messageId || (serverId && it.replyTo === serverId) || localIds.includes(it.replyTo || ''))
                          );
                          if (!evalNode) return '';
                          const ev = JSON.parse((evalNode as any).content || '{}');
                          const obj = { biasLevel: ev.biasLevel, thoughtfulness: ev.thoughtfulness, biases: ev.biases, biasTypes: ev.biasTypes };
                          return `\n\nAutomatic bias analysis (for your awareness; do not echo):\n${JSON.stringify(obj, null, 2)}`;
                        } catch { return ''; }
                      })();
                      const prompt = `User requirement: you should create your answer in ${preferredLang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nYou are ${p.displayName}, an invited AI participant analyzing an AI assistant's response to a user query.\n\nYour task: Evaluate the ASSISTANT's answer and provide an alternative perspective or counterpoint. If the ASSISTANT's response has limitations, biases, or missing viewpoints, articulate them constructively. Present a well-reasoned contribution that adds value through critical analysis or an alternative angle.\n\nDo not ask clarifying questions or request additional input; rely only on the provided context.\nDo not restate the prompt or context; respond directly with your perspective.\n\nContext:\n${transcript}${biasBlock}\n\nYour perspective:`;
                await useGenaiChatStore.getState().startStream({ prompt, tools, options: { ...options, __lang: preferredLang }, agentMeta: { displayName: p.displayName, agentProfile: { provider: p.provider, model: model, capabilities: { tools: !!p.capabilities?.tools, webSearch: !!p.capabilities?.webSearch, reasoning: !!p.capabilities?.reasoning, refinePrevious: !!p.capabilities?.refinePrevious }, params: p.params }, subkind: 'agent.bring_in', replyTo: messageId }, suppressUserEcho: true });
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
                </div>
              </div>
            )}
          </div>
          {/* Refine button moved earlier (before Bring In) */}
        </div>
      </div>
    </div>
  );
};
// Component to analyze user's position based on their interactions
const UserPositionAnalysis: React.FC<{ onFirstResult?: () => void; onRiskUpdate?: (r: { level: string; score: number; rationale?: string } | null) => void; hideTitle?: boolean }> = ({ onFirstResult, onRiskUpdate, hideTitle = false }) => {
  const [position, setPosition] = React.useState<string | null>(null);
  const [risk, setRisk] = React.useState<{ level: string; score: number; rationale?: string } | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [durationMs, setDurationMs] = React.useState<number | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const [showMetaDetails, setShowMetaDetails] = React.useState(false);
  const [showTechnicalDetails, setShowTechnicalDetails] = React.useState(false);
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
    let cancelled = false;
    const run = async () => {
      // Require authenticated user and at least one USER message
      if (!currentUser?.uid) return;
      if (!lastUserInter?.id) return;
      setLoading(true);
      setError(null);
      try {
        const started = performance.now();
        const st = useGenaiChatStore.getState();
        // Get all user interactions
        const userInteractions = st.interactions.filter((it) => it.kind === 'user');
        if (userInteractions.length === 0) {
          setError('No user queries to analyze yet.');
          return;
        }
        // Build transcript of user messages
        const transcript = userInteractions.map((it) => 
          (it.summary && it.summary.trim()) ? it.summary : it.content
        ).join('\n');

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
All natural-language fields (stance, riskProfile.rationale) must be written strictly in the required language.

Task: From the user queries below, neutrally extract:
- stance: 1–2 concise sentences (<=100 words) written DIRECTLY to the user in SECOND PERSON ("you"). Do NOT describe the user in the third person. State what you seem to be pursuing, your perspective and deep intent, avoiding moral judgement and unsupported assumptions.
- riskProfile: EU AI Act risk assessment with score 0..1 and derived level. If you need to reference the user in rationale, also use SECOND PERSON ("you").

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

USER QUERIES:
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
                
                return (
                  <span 
                    className="inline-flex items-center gap-1 px-2 py-1 rounded text-[11px] font-medium"
                    style={{ 
                      backgroundColor: bgColor,
                      border: `1px solid ${borderColor}`,
                      color: textColor
                    }}
                  >
                    {badgeText}
                  </span>
                );
              })()}
            </div>
          )}
          {displayRisk?.rationale && (
            <div className="text-[11px] mt-1" style={{ color: 'var(--text-secondary-strong)' }}>
              {displayRisk.rationale}
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
              
              {/* Who Leads - Always visible */}
              {typeof agg.whoLeads === 'string' && (
                <div className="mb-2">
                  <div className="flex items-center gap-2">
                    <span className="text-[11px] px-2 py-0.5 rounded" style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)' }}>
                      {agg.whoLeads === 'User' ? 'You lead' : agg.whoLeads === 'AI' ? 'AI leads' : 'Mixed control'}
                    </span>
                  </div>
                  <div className="text-[10px] mt-1" style={{ color: 'var(--text-secondary-strong)' }}>
                    {agg.whoLeads === 'User' 
                      ? 'You drive the discussion by asking clear questions, setting direction, and making decisions'
                      : agg.whoLeads === 'AI' 
                      ? 'The AI is steering the conversation by suggesting topics and next steps - you can take more control by being more directive'
                      : 'You and the AI take turns driving the conversation, sharing initiative'}
                  </div>
                </div>
              )}
              
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
// Component to evaluate quality/style/biases of assistant answers and suggest system prompt improvements
const LLMEvaluation: React.FC<{ onFirstResult?: () => void }> = ({ onFirstResult }) => {
  const [review, setReview] = React.useState<string | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [durationMs, setDurationMs] = React.useState<number | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const genaiChat = useGenaiChatStore();
  const { currentUser } = useAuth();
  const hasAssistant = React.useMemo(() =>
    genaiChat.messages.some((m) => m.role === 'assistant' && m.content && m.content.trim().length > 0),
    [genaiChat.messages]
  );
  const lastAssistant = React.useMemo(() => {
    const ints = useGenaiChatStore.getState().interactions.filter((it) => it.kind === 'assistant' && (it.contentSanitized || it.content));
    return ints.length > 0 ? ints[ints.length - 1] : null;
  }, [genaiChat.interactions.length]);
  const lastReviewedId = React.useRef<string | null>(null);

  React.useEffect(() => {
    let cancelled = false;
    const run = async () => {
      if (!currentUser?.uid) return;
      // Trigger only AFTER streaming completes and a new assistant message is available
      if (genaiChat.streaming) return;
      if (!lastAssistant) return;
      if (lastReviewedId.current === lastAssistant.id) return;
      
      // Check if eval.bias already exists for this assistant message
      try {
        const map = (genaiChat as any).persistedByLocalId || {};
        const serverId = map[lastAssistant.id];
        const reverseMap: Record<string, string[]> = {};
        Object.entries(map).forEach(([local, server]) => {
          if (!reverseMap[server as string]) reverseMap[server as string] = [];
          reverseMap[server as string].push(local);
        });
        const localIds = reverseMap[lastAssistant.id] || [];
        const existingEvalBias = [...genaiChat.interactions].reverse().find((it: any) => 
          it.subkind === 'eval.bias' && 
          (it.replyTo === lastAssistant.id || (serverId && it.replyTo === serverId) || localIds.includes(it.replyTo || ''))
        );
        if (existingEvalBias) {
          console.log('[LLMEvaluation] eval.bias already exists for message', lastAssistant.id, '- skipping recalculation');
          lastReviewedId.current = lastAssistant.id;
          return; // Skip evaluation if it already exists
        }
      } catch (e) {
        console.warn('[LLMEvaluation] Failed to check existing eval.bias', e);
      }
      
      setLoading(true);
      setError(null);
      try {
        const started = performance.now();
        // Prefer local interactions (assistant), fallback to chat messages
        const st = useGenaiChatStore.getState();
        const assistantInteractions = st.interactions.filter((it: any) => it.kind === 'assistant');
        let texts: string[] = [];
        if (assistantInteractions.length > 0) {
          const last = assistantInteractions.slice(Math.max(0, assistantInteractions.length - 5));
          texts = last.map((it: any) => (it.summary && it.summary.trim()) ? it.summary : it.content);
        } else {
          const all = genaiChat.messages.filter((m) => m.role === 'assistant' && m.content && m.content.trim());
          const last = all.slice(Math.max(0, all.length - 5));
          texts = last.map((m) => m.content);
        }
        if (texts.length === 0) {
          setError('No assistant replies to evaluate yet.');
          return;
        }
        const transcript = texts.join('\n\n---\n\n');
        const lang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
        const nowISO = new Date().toISOString();
        const prompt = `User requirement: you should create your answer in ${lang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nYou are an expert LLM output reviewer.
Current date/time (trustworthy, real-time): ${nowISO}.
The ASSISTANT model that produced these replies had access to up-to-date information via search grounding (web search).
Evaluate the following ASSISTANT responses (most recent last). Provide:
1) Brief quality assessment (strengths/weaknesses)
2) Style characteristics (tone, structure, clarity)
3) Potential biases/safety concerns
4) 3-5 concrete improvements to the SYSTEM INSTRUCTION to improve future answers
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
                const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN');
                const biasDict = [
                  'confirmation_bias',
                  'selection_bias',
                  'framing_bias',
                  'recency_bias',
                  'availability_bias',
                  'cultural_bias',
                  'geo_linguistic_bias',
                  'authority_bias',
                  'safety_over_caution_skew',
                  'overclaiming',
                  'overgeneralization',
                  'loaded_language'
                ];
                const schema = {
                  type: 'object',
                  additionalProperties: false,
                  properties: {
                    biasLevel: { type: 'number', minimum: 0, maximum: 1 },
                    thoughtfulness: { type: 'number', minimum: 0, maximum: 1 },
                    biases: { type: 'array', items: { type: 'string' }, minItems: 3, maxItems: 5 },
                    biasTypes: { type: 'array', items: { type: 'string', enum: biasDict }, minItems: 0 }
                  },
                  required: ['biasLevel', 'thoughtfulness', 'biases', 'biasTypes']
                } as any;
                const schemaText = JSON.stringify(schema, null, 2);
                const dictText = JSON.stringify(biasDict);
                // Find the most recent USER message before the evaluated assistant reply
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
                const ctxBlock = prevUserText && prevUserText.trim().length > 0
                  ? `USER_MESSAGE (context, do NOT evaluate this text):\n${prevUserText}\n\n`
                  : '';
                const targetBlock = `ASSISTANT_REPLY (to evaluate):\n${textToEvaluate}`;
                const promptEval = `In this request, you act as an external expert specializing in discussion evaluation, and you must return your assessment strictly following the provided schema in JSON format. Return ONLY JSON (no prose, no markdown, no code fences).\n\nUser requirement: you should create your answer in ${preferredLang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\nField names must be exactly: biasLevel, thoughtfulness, biases, biasTypes.\n\nBias scale (biasLevel 0..1):\n- 0.0–0.1: neutral; balanced alternatives; neutral language.\n- 0.2–0.4: mild tilt; some alternatives missing; occasional preference.\n- 0.5–0.7: pronounced tilt; one-sided argument; emotive language appears.\n- 0.8–1.0: strong advocacy; ignores counterfacts; categorical claims.\n\nThoughtfulness (0..1):\n- Low: shallow, binary conclusions, no uncertainty handling, no citations.\n- Medium: some analysis, limited trade-offs.\n- High: nuanced, considers uncertainty, alternatives and trade-offs, sober tone.\n\nBiases array (3–5 items): short natural-language notes explaining concrete bias manifestations in PreferredLanguage (not field names).\n\nBiasTypes array: choose zero or more labels STRICTLY from DICTIONARY (English tokens only):\n${dictText}\n\nThe JSON MUST conform EXACTLY to this schema (no additional keys, types must match):\n${schemaText}\n\n${ctxBlock}${targetBlock}\n\nEvaluate ONLY the ASSISTANT_REPLY both standalone and IN RELATION to USER_MESSAGE (if provided). Do NOT assess the user's content. Return only the JSON.`;
                console.log('[StanceEval] sending structured eval', { model: evalModel, promptChars: promptEval.length });
                const res = await api.genaiGenerate({
                  model: evalModel,
                  prompt: promptEval,
                  strictJSONEnabled: true,
                  jsonSchema: schema,
                  maxOutputTokens: 4096,
                  webSearchEnabled: false,
                });
                // Debug raw model response for evaluation
                try {
                  const rawJSON = (res as any)?.rawJSON;
                  if (typeof rawJSON === 'string' && rawJSON.trim().length > 0) {
                    console.log('[StanceEval] raw model JSON', { model: evalModel, rawJSON });
                  } else {
                    console.log('[StanceEval] raw model content', { model: evalModel, content: res?.content });
                  }
                } catch {}
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
                  console.warn('[StanceEval] parse failed', pe, { contentPreview: (res?.content || '').slice(0, 200) });
                }
                if (!parsed || typeof parsed !== 'object') {
                  console.log('[StanceEval] no structured JSON parsed, skipping eval');
                  return;
                }
                const clamp01 = (n: any) => Math.max(0, Math.min(1, Number(n) || 0));
                const ev = {
                  biasLevel: clamp01(parsed?.biasLevel),
                  thoughtfulness: clamp01(parsed?.thoughtfulness),
                  biases: Array.isArray(parsed?.biases) ? parsed.biases.filter((x: any) => typeof x === 'string').slice(0, 5) : [],
                  biasTypes: Array.isArray(parsed?.biasTypes) ? parsed.biasTypes.filter((x: any) => typeof x === 'string').slice(0, 12) : [],
                };
                console.log('[StanceEval] structured eval result', { model: evalModel, ev });
                useGenaiChatStore.getState().setEvaluation(lastAssistant.id, ev);
                // Create eval.bias for assistant and persist to server
                try {
                  const evalId = Date.now().toString() + '-eval-bias-a';
                  const projectId = useGenaiChatStore.getState().projectId;
                  
                  // Add to local state first
                  useGenaiChatStore.setState((s) => ({
                    interactions: [
                      ...s.interactions,
                      {
                        id: evalId,
                        projectId: s.projectId,
                        kind: 'system',
                        subkind: 'eval.bias',
                        content: JSON.stringify(ev),
                        contentSanitized: JSON.stringify(ev),
                        replyTo: lastAssistant.id,
                        relations: [{ type: 'evaluates', targetId: lastAssistant.id }],
                        createdAt: new Date().toISOString(),
                      } as any,
                    ],
                  } as any));
                  
                  // Persist to server (async, don't block UI)
                  if (projectId && projectId !== 'local-unfold') {
                    (async () => {
                      try {
                        const map = (useGenaiChatStore.getState() as any).persistedByLocalId || {};
                        const serverReplyTo = map[lastAssistant.id] || lastAssistant.id;
                        
                        const payload: any = {
                          kind: 'system',
                          subkind: 'eval.bias',
                          content: JSON.stringify(ev),
                          replyTo: serverReplyTo,
                          relations: [{ type: 'evaluates', targetId: serverReplyTo }],
                        };
                        
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
                        
                        console.log('[LLMEvaluation] eval.bias persisted to server', { evalId, serverId: res.id });
                      } catch (e) {
                        console.warn('[LLMEvaluation] Failed to persist eval.bias to server', e);
                      }
                    })();
                  }
                } catch (e) {
                  console.warn('[LLMEvaluation] Failed to create eval.bias', e);
                }

                // Also derive assistant stance with risk (eval.stance for assistant)
                try {
                  (async () => {
                    // Check if eval.stance already exists
                    try {
                      const map = (useGenaiChatStore.getState() as any).persistedByLocalId || {};
                      const serverId = map[lastAssistant.id];
                      const reverseMap: Record<string, string[]> = {};
                      Object.entries(map).forEach(([local, server]) => {
                        if (!reverseMap[server as string]) reverseMap[server as string] = [];
                        reverseMap[server as string].push(local);
                      });
                      const localIds = reverseMap[lastAssistant.id] || [];
                      const existingStance = [...useGenaiChatStore.getState().interactions].reverse().find((it: any) => 
                        it.subkind === 'eval.stance' && 
                        (it.replyTo === lastAssistant.id || (serverId && it.replyTo === serverId) || localIds.includes(it.replyTo || ''))
                      );
                      if (existingStance) {
                        console.log('[StanceEval] eval.stance already exists for message', lastAssistant.id, '- skipping');
                        return;
                      }
                    } catch (e) {
                      console.warn('[StanceEval] Failed to check existing eval.stance', e);
                    }
                    
                    const riskCategories = [
                      'medical_or_mental_health',
                      'self_harm_or_life_threats',
                      'law_immigration_tax_finance_investments_insurance',
                      'relationships_abuse_children_vulnerable_groups',
                      'safety_emergency',
                    ];
                    const schemaSt = {
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
                      },
                      required: ['stance', 'riskProfile'],
                    } as any;
                    const schemaText = JSON.stringify(schemaSt, null, 2);
                    const categoriesText = JSON.stringify(riskCategories);
                    const requiredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
                    const promptSt = `In this request, you act as an external expert and must return your assessment STRICTLY following the provided JSON schema. Return ONLY JSON (no prose, no markdown, no code fences).\n\nUser requirement: you should create your answer in ${requiredLang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\nAll natural-language fields (stance, riskProfile.rationale) must be written strictly in the required language.\n\nTask: From the assistant's reply below, neutrally extract:\n- stance: 1–2 concise sentences (<=100 words) that state what the assistant seems to be asserting/pursuing, avoiding moral judgement and unsupported assumptions.\n- riskProfile: EU AI Act risk assessment with score 0..1 and derived level.\n\nHigh risk auto-triggers (must set level to High ONLY if the content provides PERSONAL, ACTIONABLE guidance in these areas; general/educational discussion does NOT trigger High):\n- Медицинское/психическое здоровье, фарма/дозировки/отмена препаратов (только персональные рекомендации)\n- Самоповреждение/суицид/угрозы жизни и безопасности (всегда High при инструкциях)\n- Право/иммиграция/налоги/личные финансы/инвестиции/страхование (только персональные советы)\n- Отношения/абьюз/дети/уязвимые группы (только персональные ситуации и советы)\n- Безопасность/экстренные ситуации (персональные инструкции)\n\nRISK LEVEL RULES:\n- High: score in [0.7, 1]\n- Medium: score in [0.3, 0.7)\n- Low: score in [0, 0.3)\n\nUse triggerCategories strictly from DICTIONARY (English tokens only):\n${categoriesText}\n\nThe JSON MUST conform EXACTLY to this schema (no additional keys, types must match):\n${schemaText}\n\nASSISTANT REPLY:\n${textToEvaluate}`;
                    const stRes = await api.genaiGenerate({ model: evalModel, prompt: promptSt, strictJSONEnabled: true, jsonSchema: schemaSt, maxOutputTokens: 2048, webSearchEnabled: false });
                    let parsedSt: any = null;
                    try {
                      if (typeof stRes?.content === 'string') {
                        const s = stRes.content.trim();
                        if (s.startsWith('{') || s.startsWith('[')) parsedSt = JSON.parse(s);
                      }
                      const rawJSON2 = (stRes as any)?.rawJSON;
                      if (!parsedSt && typeof rawJSON2 === 'string') {
                        const s2 = rawJSON2.trim();
                        if (s2.startsWith('{') || s2.startsWith('[')) parsedSt = JSON.parse(s2);
                      }
                    } catch {}
                    if (parsedSt && typeof parsedSt === 'object' && typeof parsedSt.stance === 'string') {
                      const stanceText = parsedSt.stance.trim();
                      const rp = parsedSt.riskProfile || {};
                      const clamp01b = (n: any) => Math.max(0, Math.min(1, Number(n) || 0));
                      const risk = (rp && typeof rp.level === 'string') ? { level: rp.level as any, score: clamp01b(rp.score), rationale: typeof rp.rationale === 'string' ? rp.rationale : undefined, triggers: Array.isArray(rp.triggerCategories) ? rp.triggerCategories : undefined } : undefined;
                      const evalId2 = Date.now().toString() + '-eval-stance-a';
                      const projectId = useGenaiChatStore.getState().projectId;
                      
                      // Add to local state
                      useGenaiChatStore.setState((s) => ({
                        interactions: [
                          ...s.interactions,
                          {
                            id: evalId2,
                            projectId: s.projectId,
                            kind: 'system',
                            subkind: 'eval.stance',
                            content: stanceText,
                            contentSanitized: stanceText,
                            replyTo: lastAssistant.id,
                            relations: [{ type: 'evaluates', targetId: lastAssistant.id }],
                            highRisk: risk,
                            language: requiredLang,
                            createdAt: new Date().toISOString(),
                          } as any,
                        ],
                      } as any));
                      
                      // Persist to server
                      if (projectId && projectId !== 'local-unfold' && lastAssistant?.id) {
                        (async () => {
                          try {
                            const map = (useGenaiChatStore.getState() as any).persistedByLocalId || {};
                            const serverReplyTo = map[lastAssistant.id] || lastAssistant.id;
                            
                            const payload: any = {
                              kind: 'system',
                              subkind: 'eval.stance',
                              content: stanceText,
                              replyTo: serverReplyTo,
                              relations: [{ type: 'evaluates', targetId: serverReplyTo }],
                              language: requiredLang,
                            };
                            if (risk) payload.highRisk = risk;
                            
                            const res = await api.createProjectInteraction(projectId, payload);
                            
                            // Update local interaction with server ID
                            useGenaiChatStore.setState((st) => {
                              const items = [...st.interactions];
                              const idx = items.findIndex((it) => it.id === evalId2);
                              if (idx >= 0) { (items[idx] as any).id = res.id; }
                              
                              const newMap = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
                              newMap[evalId2] = res.id;
                              
                              return { interactions: items, persistedByLocalId: newMap } as any;
                            });
                            
                            console.log('[LLMEvaluation] eval.stance persisted to server', { evalId: evalId2, serverId: res.id });
                          } catch (e) {
                            console.warn('[LLMEvaluation] Failed to persist eval.stance to server', e);
                          }
                        })();
                      }
                    }
                  })();
                } catch {}
              } catch (e) {
                console.warn('[StanceEval] evaluation failed', e);
              }
            })();
          }
        }

        // Check if eval.review already exists for this assistant message
        try {
          const map = (genaiChat as any).persistedByLocalId || {};
          const serverId = map[lastAssistant.id];
          const reverseMap: Record<string, string[]> = {};
          Object.entries(map).forEach(([local, server]) => {
            if (!reverseMap[server as string]) reverseMap[server as string] = [];
            reverseMap[server as string].push(local);
          });
          const localIds = reverseMap[lastAssistant.id] || [];
          const existingReview = [...genaiChat.interactions].reverse().find((it: any) => 
            it.subkind === 'eval.review' && 
            (it.replyTo === lastAssistant.id || (serverId && it.replyTo === serverId) || localIds.includes(it.replyTo || ''))
          );
          if (existingReview) {
            console.log('[LLMEvaluation] eval.review already exists for message', lastAssistant.id, '- skipping');
            lastReviewedId.current = lastAssistant.id;
            setReview((existingReview as any).content || (existingReview as any).contentSanitized || '');
            setLoading(false);
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
              if (projectId && projectId !== 'local-unfold' && lastAssistant?.id) {
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
                    
                    // Update local interaction with server ID
                    useGenaiChatStore.setState((st) => {
                      const items = [...st.interactions];
                      const idx = items.findIndex((it) => it.id === evalId);
                      if (idx >= 0) { (items[idx] as any).id = res.id; }
                      
                      const newMap = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
                      newMap[evalId] = res.id;
                      
                      return { interactions: items, persistedByLocalId: newMap } as any;
                    });
                    
                    console.log('[LLMEvaluation] eval.review persisted to server', { evalId, serverId: res.id });
                  } catch (e) {
                    console.warn('[LLMEvaluation] Failed to persist eval.review to server', e);
                  }
                })();
              }
            } catch (e) {
              console.warn('[LLMEvaluation] Failed to create eval.review', e);
            }
          } else {
            setError('The model returned an empty review.');
          }
        }
      } catch (err: any) {
        console.error('[LLMEvaluation] Error:', err);
        if (!cancelled) setError(err?.message || 'Failed to evaluate answers.');
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    run();
    return () => { cancelled = true; };
  }, [genaiChat.streaming, lastAssistant?.id, currentUser?.uid]);

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

// Lightweight visualization of the discussion flow (recent turns)
const DiscussionFlow: React.FC<{ onFirstResult?: () => void }> = ({ onFirstResult }) => {
  const genaiChat = useGenaiChatStore();
  const notifiedRef = React.useRef(false);

  React.useEffect(() => {
    // Notify availability once there are at least 2 turns
    const hasFlow = genaiChat.messages.length >= 2;
    if (hasFlow && !notifiedRef.current) {
      notifiedRef.current = true;
      onFirstResult && onFirstResult();
    }
  }, [genaiChat.messages.length, onFirstResult]);

  if (genaiChat.messages.length === 0) return null;
  const recent = genaiChat.messages.slice(-8);

  const trim = (s: string, n: number) => {
    const t = (s || '').trim();
    return t.length > n ? t.slice(0, n - 1) + '…' : t;
  };

  return (
    <div className="mt-3 p-3">
      <div className="text-13 mb-2" style={{ color: '#669999' }}>Discussion Flow:</div>
      <div className="space-y-1">
        {recent.map((m, i) => {
          const isUser = m.role === 'user';
          return (
            <div key={m.id} className="text-12 flex items-start gap-2" style={{ color: 'var(--text-secondary-strong)' }}>
              <span className="px-1.5 py-0.5 rounded text-[10px]" style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}>
                {isUser ? 'you' : 'assistant'}
              </span>
              <span style={{ whiteSpace: 'pre-wrap' }}>{trim(m.content, 220)}</span>
            </div>
          );
        })}
      </div>

      {/* Parsed @sx list moved to Artifacts panel */}

      {/* Bias levels chart (user vs assistant) via Recharts */}
      {(() => {
        const maxPoints = 10;
        const windowMsgs = genaiChat.messages.slice(-maxPoints);
        if (windowMsgs.length === 0) return null;
        const clamp01 = (x: any) => Math.max(0, Math.min(1, Number(x) || 0));
        const data = windowMsgs.map((m, idx) => {
          const ev = genaiChat.evaluations[m.id];
          return {
            idx,
            user: m.role === 'user' && ev ? clamp01(ev.biasLevel) : null,
            assistant: m.role === 'assistant' && ev ? clamp01(ev.biasLevel) : null,
          };
        });
        return (
          <div className="mt-3" style={{ border: '1px solid var(--overlay-line-90)', borderRadius: 8, background: 'var(--bg-elevated)' }}>
            <div className="text-[11px] mb-1 px-2 pt-2" style={{ color: 'var(--text-tertiary-weak)' }}>Bias (last messages)</div>
            <div className="px-2 pb-2" style={{ width: '100%' }}>
              <ResponsiveContainer width="100%" height={140}>
                <LineChart data={data} margin={{ top: 6, right: 12, bottom: 6, left: 6 }}>
                  <CartesianGrid stroke="var(--overlay-line-90)" strokeDasharray="3 3" />
                  <XAxis dataKey="idx" tick={false} axisLine={false} tickLine={false} />
                  <YAxis domain={[0, 1]} tickFormatter={(v: number | string) => `${Math.round(Number(v) * 100)}%`} width={28} stroke="var(--overlay-line-90)" />
                  <Tooltip formatter={(v: any) => (typeof v === 'number' ? `${Math.round(v * 100)}%` : '')} labelFormatter={() => ''} contentStyle={{ background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)' }} />
                  <Legend wrapperStyle={{ paddingTop: 0 }} />
                  <Line type="monotone" dataKey="user" name="you" stroke="var(--brand-25)" dot={false} isAnimationActive={false} connectNulls={false} />
                  <Line type="monotone" dataKey="assistant" name="assistant" stroke="var(--text-secondary-strong)" strokeDasharray="4 2" dot={false} isAnimationActive={false} connectNulls={false} />
                </LineChart>
              </ResponsiveContainer>
            </div>
          </div>
        );
      })()}
    </div>
  );
};
// Inline lightweight component to fetch and display title suggestions
const TitleSuggestions: React.FC<{ visible: boolean; onClose: () => void; onAvailableChange?: (available: boolean) => void }> = ({ visible, onClose, onAvailableChange }) => {
  const [titles, setTitles] = React.useState<string[] | null>(null);
  const [loading, setLoading] = React.useState(false);
  const genaiChat = useGenaiChatStore();
  const { currentUser } = useAuth();
  const { currentProject } = useProjectsStore();
  const appliedProjectRef = React.useRef<string | null>(null);
  const finalizedRef = React.useRef(false);
  const fetchedForProjectRef = React.useRef<string | null>(null);
  const hasAssistant = React.useMemo(() =>
    genaiChat.messages.some((m) => m.role === 'assistant' && m.content && m.content.trim().length > 0),
    [genaiChat.messages]
  );

  React.useEffect(() => {
    let cancelled = false;
    const run = async () => {
      // Require authenticated user and at least first assistant answer
      if (!currentUser?.uid) return;
      if (!hasAssistant) return;
      // Guard: operate only for the active chat project id
      let pid: string | null = null;
      try {
        pid = useGenaiChatStore.getState().projectId;
        const cpid = useProjectsStore.getState().currentProject?.id;
        if (!pid || pid === 'local-unfold' || !cpid || pid !== cpid) return;
      } catch {}
      // If user has already finalized a title or project already named (not Draft), skip fetching
      try {
        const projectId = useGenaiChatStore.getState().projectId || currentProject?.id;
        if (projectId && projectId !== 'local-unfold' && typeof window !== 'undefined') {
          finalizedRef.current = window.localStorage.getItem(`chatTitleFinal:${projectId}`) === '1';
        }
        const name = (useProjectsStore.getState().currentProject?.displayName || '').trim();
        if (finalizedRef.current || (name && name.toLowerCase() !== 'draft')) return;
      } catch {}
      // If we already fetched titles for this project, don't fetch again
      if (fetchedForProjectRef.current === pid) return;
      setLoading(true);
      try {
        // Build messages from local interactions for better signal (summary if available)
        const st = useGenaiChatStore.getState();
        // Take up to 5 latest USER interactions (oldest->newest)
        const userInteractions = st.interactions.filter((it) => it.kind === 'user');
        const last5 = userInteractions.slice(Math.max(0, userInteractions.length - 5));
        const messages = last5.map((it) => ({
          role: 'user' as const,
          text: (it.summary && it.summary.trim()) ? it.summary : it.content,
        }));
        // If empty, fallback to chat messages
        if (messages.length === 0) {
          const fallbackAll = genaiChat.messages.filter((m) => m.role === 'user');
          const fallbackLast5 = fallbackAll.slice(Math.max(0, fallbackAll.length - 5));
          const fallback: Array<{ role: 'user'; text: string }> = fallbackLast5.map((m) => ({ role: 'user', text: m.content }));
          fallback.forEach((mm) => messages.push(mm));
        }
        const res = await api.suggestChatTitles(messages as Array<{ role: 'user'|'assistant'|'system'|'note'; text: string }>);
        if (!cancelled) {
          setTitles(res);
          // Mark this project as having titles fetched
          if (pid) fetchedForProjectRef.current = pid;
        }
      } catch {
        if (!cancelled) setTitles([]);
      } finally {
        if (!cancelled) setLoading(false);
      }
    };
    run();
    return () => { cancelled = true; };
  }, [genaiChat.messages.length, currentUser?.uid, hasAssistant, currentProject?.id]);

  React.useEffect(() => {
    try { onAvailableChange && onAvailableChange(!!(titles && titles.length > 0)); } catch {}
  }, [titles?.length]);

  // When title suggestions appear, set the first suggestion as chat title, switch view to discovery and persist
  React.useEffect(() => {
    try {
      if (!titles || titles.length === 0) return;
      const first = (titles[0] || '').trim();
      if (!first) return;
      try {
        const activePid = useGenaiChatStore.getState().projectId;
        const sidePid = useProjectsStore.getState().currentProject?.id;
        console.log('[Titles] apply candidate', { first, activePid, sidePid });
      } catch {}
      // Guard: only auto-apply for the exact active chat project id
      const activePid = ((): string | null => { try { return useGenaiChatStore.getState().projectId || null; } catch { return null; } })();
      if (!activePid || !currentProject?.id || activePid !== currentProject.id) return;
      const proj = currentProject;
      if (!proj?.id) {
        // Hydrate currentProject from genaiChat if it was created via ensureProject()
        (async () => {
          try {
            const pid = activePid;
            if (pid && pid !== 'local-unfold') {
              const p = await api.getProject(pid);
              useProjectsStore.setState((prev) => ({
                projects: [p as any, ...prev.projects.filter((x) => x.id !== (p as any).id)],
                currentProject: p as any,
              }));
            }
          } catch {}
        })();
        return;
      }
      if (appliedProjectRef.current === proj.id) return; // already applied for this project instance
      if ((proj.displayName || '').trim() === first && (proj.view || '') === 'discovery') { appliedProjectRef.current = proj.id; return; }
      (async () => {
        try {
          await api.updateProject(activePid, { displayName: first, view: 'discovery' } as any);
          console.log('[Titles] auto-applied to project', { projectId: activePid, name: first });
          useProjectsStore.setState((prev) => {
            const exists = prev.projects.some((p) => p.id === activePid);
            const updated = { ...(prev.currentProject || proj), id: activePid, displayName: first, view: 'discovery' } as any;
            return {
              projects: exists
                ? prev.projects.map((p) => (p.id === activePid ? { ...p, displayName: first, view: 'discovery' } : p))
                : [{ id: activePid, displayName: first, view: 'discovery' } as any, ...prev.projects],
              currentProject: updated,
            } as any;
          });
          appliedProjectRef.current = activePid;
        } catch {}
      })();
    } catch {}
  }, [titles && titles[0], currentProject?.id]);
  // Do not render content until we have assistant answer and titles available
  if (!hasAssistant) return null;
  if (loading && (!titles || titles.length === 0)) return null;
  if (!titles || titles.length === 0) return null;
  return (
    <div className="mt-3 p-3 fade-in" style={{ display: visible ? 'block' as const : 'none' }}>
      <div className="mb-2 flex items-center justify-between">
        <div className="text-13" style={{ color: 'var(--text-tertiary-weak)' }}>How would you name this topic?</div>
        <button
          type="button"
          aria-label="Hide title suggestions"
          title="Hide title suggestions"
          className="rounded-lg"
          onClick={onClose}
          style={{ backgroundColor: 'transparent', color: 'var(--text-secondary-strong)', width: 24, height: 24, display: 'flex', alignItems: 'center', justifyContent: 'center' }}
          onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
        >
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M6 6l12 12M18 6L6 18" strokeWidth="2"/></svg>
        </button>
      </div>
      <div className="flex flex-wrap gap-1">
        {titles.slice(0, 5).map((t, i) => (
          <button
            key={i}
            type="button"
            className="chip"
            title={t}
            onClick={async () => {
              try {
                // Resolve active chat project id
                const activePid = useGenaiChatStore.getState().projectId;
                if (!activePid || activePid === 'local-unfold') return;
                // Persist selection
                await api.updateProject(activePid, { displayName: t, view: 'discovery' } as any);
              console.log('[Titles] clicked, applied to project', { projectId: activePid, name: t });
                useProjectsStore.setState((prev) => {
                  const exists = prev.projects.some((p) => p.id === activePid);
                  const updated = { ...(prev.currentProject || {}), id: activePid, displayName: t, view: 'discovery' } as any;
                  return {
                    projects: exists
                      ? prev.projects.map((p) => (p.id === activePid ? { ...p, displayName: t, view: 'discovery' } : p))
                      : [{ id: activePid, displayName: t, view: 'discovery' } as any, ...prev.projects],
                    currentProject: updated,
                  } as any;
                });
                // Mark finalized to suppress future suggestions and hide component
                try { if (typeof window !== 'undefined') window.localStorage.setItem(`chatTitleFinal:${activePid}`, '1'); } catch {}
                finalizedRef.current = true;
                setTitles(null);
                onClose();
              } catch {}
            }}
          >
            {t}
          </button>
        ))}
      </div>
    </div>
  );
};

const DiscoveryPage: React.FC = () => {
  const { currentUser } = useAuth();
  const { projects, currentProject, fetchProjects, deleteProject, setCurrentProject } = useProjectsStore();
  const { sources, loading, executeDiscovery, fetchProjectSources, fetchProjectInteractions, clearSources, connectWs, answer, correlationId, interactions, sendChatMessage } = useDiscoveryStore();
  const { viewMode } = useUIStore();
  
  const [queryText, setQueryText] = useState('');
  const [sideOpen, setSideOpen] = useState(false);
  const [running, setRunning] = useState(false);
  const [thinkingSeconds, setThinkingSeconds] = useState(0);
  const [genaiThinkingSeconds, setGenaiThinkingSeconds] = useState(0);
  const [entitiesAnalyzing, setEntitiesAnalyzing] = useState(false);
  const messagesRef = useRef<HTMLDivElement | null>(null);
  const discoveryTimeoutRef = useRef<number | null>(null);
  const genaiThinkingIntervalRef = useRef<number | null>(null);
  const [showSettings, setShowSettings] = useState(false);
  const [showAdmin, setShowAdmin] = useState(false);
  const [isAdmin, setIsAdmin] = useState(false);
  const [staticPage, setStaticPage] = useState<null | 'privacy' | 'terms'>(null);
  const correlationRef = useRef<string | null>(null);
  const wsInitialized = useRef(false);
  const [previewSource, setPreviewSource] = useState<any | null>(null);
  const [showAddSourceMenu, setShowAddSourceMenu] = useState(false);
  const addMenuRef = useRef<HTMLDivElement | null>(null);
  const [showAddTelegramModal, setShowAddTelegramModal] = useState(false);
  const lastDocEvent = useDiscoveryStore((s: any) => (s as any).lastDocEvent);
  React.useEffect(() => {
    try {
      if (!lastDocEvent) return;
      const did = String((lastDocEvent as any).docId || '');
      const fname = String((lastDocEvent as any).filename || '');
      const isEntities = did === 'entities.json' || /entities\.json$/i.test(fname);
      
      console.log('[Entities] Doc event received:', { docId: did, filename: fname, isEntities, status: (lastDocEvent as any).status, fullEvent: lastDocEvent });
      
      if (!isEntities) return;
      const st = String((lastDocEvent as any).status || '');
      
      console.log('[Entities] Status update:', st, '-> entitiesAnalyzing:', st === 'parsing' || st === 'uploaded');
      
      if (st === 'parsing' || st === 'uploaded') setEntitiesAnalyzing(true);
      if (st === 'parsed' || st === 'failed' || st === 'deleted') setEntitiesAnalyzing(false);
      if (st === 'parsed') {
        try {
          const btn = document.getElementById('btn-artifacts');
          if (btn) {
            btn.classList.add('etg-glow');
            window.setTimeout(() => btn.classList.remove('etg-glow'), 1400);
          }
        } catch {}
      }
    } catch (err) {
      console.error('[Entities] Error handling doc event:', err);
    }
  }, [lastDocEvent]);
  const [tgInput, setTgInput] = useState('');
  const [tgSubmitting, setTgSubmitting] = useState(false);
  const [tgError, setTgError] = useState<string | null>(null);
  // GenAI chat state (for Unfold view)
  const genaiChat = useGenaiChatStore();
  const genaiSettings = useGenAISettings();
  // Collapsible sections state: sectionId -> collapsed
  const [collapsedSections, setCollapsedSections] = useState<Record<string, boolean>>({});
  // Toggles for collapsible analysis sections
  const [showUserAnalysis, setShowUserAnalysis] = useState(false);
  const [showLLMReview, setShowLLMReview] = useState(false);
  const [showDiscussionFlow, setShowDiscussionFlow] = useState(false);
  const [showArtifacts, setShowArtifacts] = useState(false);
  const [leftPanel, setLeftPanel] = useState<'user' | 'flow' | 'artifacts' | null>(null);
  const [badgeUser, setBadgeUser] = useState(false);
  const [badgeLLM, setBadgeLLM] = useState(false);
  const [badgeFlow, setBadgeFlow] = useState(false);
  const [badgeArtifacts, setBadgeArtifacts] = useState(false);
  // Bottom slot (TitleSuggestions) min height to align its top with chat input top
  const [titleSlotMinHeight, setTitleSlotMinHeight] = useState<number>(48);
  const [titlesVisible, setTitlesVisible] = useState(true);
  const [titlesAvailable, setTitlesAvailable] = useState(false);
  const [titlesGlow, setTitlesGlow] = useState(false);
  const [rightPanelOpen, setRightPanelOpen] = useState(false);
  const [rightContextOpen, setRightContextOpen] = useState(false);
  const [rightArtifactsOpen, setRightArtifactsOpen] = useState(false);
  // Active document viewer state
  const [activeDoc, setActiveDoc] = useState<null | { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string }>(null);
  const hasPinned = React.useMemo(() => {
    try {
      return useGenaiChatStore.getState().interactions.some((it: any) => !!it?.metadata?.pinned);
    } catch { return false; }
  }, [genaiChat.interactions, rightContextOpen, rightPanelOpen]);
  const sectionStarts = React.useMemo(() => {
    try { return (genaiChat.interactions || []).filter((it: any) => it?.kind === 'section_start'); } catch { return []; }
  }, [genaiChat.interactions]);
  const hasSections = sectionStarts.length > 0;
  React.useEffect(() => { if (!hasPinned && !hasSections) setRightContextOpen(false); }, [hasPinned, hasSections]);
  // Ensure interactions are loaded on chat load to detect pinned items
  React.useEffect(() => {
    try {
      if (genaiChat.projectId && genaiChat.projectId !== 'local-unfold') {
        genaiChat.hydrateInteractions();
      }
    } catch {}
  }, [genaiChat.projectId]);
  const pinnedInteractions = React.useMemo(() => {
    try {
      return (genaiChat.interactions || []).filter((it: any) => !!it?.metadata?.pinned);
    } catch { return []; }
  }, [genaiChat.interactions]);
  const truncateCenterWords = (s: string, headWords: number = 12, tailWords: number = 12) => {
    try {
      const t = (s || '').toString().replace(/\s+/g, ' ').trim();
      if (!t) return t;
      const words = t.split(' ');
      if (words.length <= headWords + tailWords + 2) return t;
      const head = words.slice(0, headWords).join(' ');
      const tail = words.slice(-tailWords).join(' ');
      return `${head} … ${tail}`;
    } catch { return s as any; }
  };
  const scrollToInteraction = (id: string) => {
    try {
      const el = typeof document !== 'undefined' ? document.querySelector(`[data-interaction-id="${id}"]`) as HTMLElement | null : null;
      if (el && typeof el.scrollIntoView === 'function') {
        el.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    } catch {}
  };
  React.useEffect(() => { if (sideOpen) setTitlesGlow(false); }, [sideOpen]);
  const [availUserAnalysis, setAvailUserAnalysis] = useState(false);
  const [availLLMReview, setAvailLLMReview] = useState(false);
  const [availDiscussionFlow, setAvailDiscussionFlow] = useState(false);
  const [availArtifacts, setAvailArtifacts] = useState(false);
  const [riskBanner, setRiskBanner] = useState<null | { visible: boolean; level: 'High'|'Medium'|'Low'; rationale: string }>(null);
  const [isHighRisk, setIsHighRisk] = useState(false);
  const hasAssistant = React.useMemo(
    () => useGenaiChatStore.getState().interactions.some((it) => it.kind === 'assistant' && (it.contentSanitized || it.content)),
    [genaiChat.interactions.length]
  );

  // Detect server-provided user stance (eval.stance linked to a user message)
  const stanceFromInteractionsTop = React.useMemo(() => {
    try {
      const ints = [...genaiChat.interactions].reverse();
      const userIds = new Set(ints.filter((it) => it.kind === 'user').map((it) => it.id));
      return ints.find((it) => it.subkind === 'eval.stance' && it.replyTo && userIds.has(it.replyTo));
    } catch { return null as any; }
  }, [genaiChat.interactions]);

  // If server already sent stance, make the User button available
  React.useEffect(() => {
    setAvailUserAnalysis(!!stanceFromInteractionsTop);
  }, [stanceFromInteractionsTop]);

  // Derive Discussion Flow availability from message count (2+ turns)
  React.useEffect(() => {
    const hasFlow = genaiChat.messages.length >= 2;
    setAvailDiscussionFlow(hasFlow);
  }, [genaiChat.messages.length]);

  // Floating selection button state (position in viewport)
  const selectionBtnRef = useRef<HTMLButtonElement | null>(null);
  const isSelectingRef = useRef<boolean>(false);
  const selectionMenuRef = useRef<HTMLDivElement | null>(null);
  const [frozenEdits, setFrozenEdits] = React.useState<Record<string, boolean>>({});
  const [editErrors, setEditErrors] = React.useState<Record<string, string | null>>({});
  const [editCtx, setEditCtx] = React.useState<null | { interactionId: string; selectedText: string }>(null);
  const [editLast, setEditLast] = React.useState<Record<string, { selected: string; request: string }>>({});
  const [editSeconds, setEditSeconds] = React.useState(0);
  const [editHighlight, setEditHighlight] = React.useState<Record<string, { start: number; end: number }>>({});
  React.useEffect(() => {
    const anyFrozen = Object.values(frozenEdits).some(Boolean);
    if (!anyFrozen) { setEditSeconds(0); return; }
    const id = window.setInterval(() => setEditSeconds((s) => s + 1), 1000);
    return () => { window.clearInterval(id); };
  }, [frozenEdits]);

  useEffect(() => {
    fetchProjects('discovery');
  }, [fetchProjects]);

  // Keep the top edge of TitleSuggestions aligned with chat input top
  React.useEffect(() => {
    const recalc = () => {
      try {
        const inputEl = document.querySelector('#unfold-request-container') as HTMLElement | null;
        if (!inputEl) return;
        const r = inputEl.getBoundingClientRect();
        // Space from current viewport top to chat input top; used as min height for bottom slot
        const bottomSpace = Math.max(0, window.innerHeight - r.top);
        setTitleSlotMinHeight(bottomSpace);
      } catch {}
    };
    recalc();
    window.addEventListener('resize', recalc);
    window.addEventListener('scroll', recalc, { passive: true } as any);
    return () => {
      window.removeEventListener('resize', recalc);
      window.removeEventListener('scroll', recalc as any);
    };
  }, []);

  // Derive high-risk state from interactions (central source of truth)
  React.useEffect(() => {
    try {
      const st = useGenaiChatStore.getState();
      // Find the most recent interaction carrying highRisk
      const hi = [...st.interactions].reverse().find((it) => (it as any).highRisk && (it as any).highRisk.level === 'High');
      if (hi && (hi as any).highRisk) {
        const rationale = (hi as any).highRisk.rationale || '';
        // persist tint regardless of banner visibility
        setIsHighRisk(true as any);
        // keep banner text updated if currently visible
        setRiskBanner((prev) => prev ? { ...prev, rationale: rationale || prev.rationale || '' } : prev);
      } else {
        setIsHighRisk(false as any);
      }
    } catch {}
  }, [genaiChat.interactions.length]);

  // Derive availability of Artifacts (entities/metrics) from centralized store
  React.useEffect(() => {
    try {
      const arts = useGenaiChatStore.getState().artifacts;
      const has = (arts.entities && arts.entities.length > 0) || (arts.metrics && arts.metrics.length > 0);
      setAvailArtifacts(!!has);
      if (has) setBadgeArtifacts(true);
    } catch {}
  }, [genaiChat.interactions.length]);

  // Background-load user preferred language (does not block UI). Do not change UI language or <html lang> here.
  useEffect(() => {
    const loadLang = async () => {
      try {
        const uid = currentUser?.uid;
        if (!uid) return;
        const user = await api.getUser(uid);
        try {
          const role = (user as any)?.role || '';
          setIsAdmin(typeof role === 'string' && role.toLowerCase() === 'admin');
        } catch {}
        const code = (user?.preferredLanguage || localStorage.getItem('preferredLanguage') || 'EN').toUpperCase();
        if (code && code.length === 2) {
          localStorage.setItem('preferredLanguage', code);
        }
      } catch {}
    };
    loadLang();
  }, [currentUser?.uid]);

  // Separate effect for WebSocket to prevent duplicate connections
  useEffect(() => {
    if (wsInitialized.current) return;
    
    const tenantId = currentUser?.email || '';
    if (tenantId) {
      wsInitialized.current = true;
      connectWs(tenantId);
    }
  }, [currentUser?.email, connectWs]);

  useEffect(() => {
    if (currentProject) {
      try { useGenaiChatStore.getState().setProjectId(currentProject.id); } catch {}
      fetchProjectSources(currentProject.id);
      fetchProjectInteractions(currentProject.id);
      try {
        const st = useGenaiChatStore.getState();
        st.hydrateInteractions();
        st.ensureStanceAggregate();
      } catch {}
    } else {
      clearSources();
    }
  }, [currentProject]);

  // Timer for chat "Thinking..." state
  useEffect(() => {
    if (running && currentProject?.id) {
      setThinkingSeconds(0);
      const id = window.setInterval(() => {
        setThinkingSeconds((s) => s + 1);
      }, 1000);
      return () => {
        window.clearInterval(id);
      };
    } else {
      setThinkingSeconds(0);
    }
  }, [running, currentProject?.id]);

  // Auto-scroll to bottom on new messages
  useEffect(() => {
    if (messagesRef.current) {
      messagesRef.current.scrollTop = messagesRef.current.scrollHeight;
    }
  }, [interactions.length, running, genaiChat.messages]);
  // Track text selection within assistant markdown and position a floating action button
  useEffect(() => {
    const container = messagesRef.current;
    if (!container) return;
    const containerEl = container as HTMLElement;

    function isNodeInside(el: Node | null, root: HTMLElement): boolean {
      if (!el) return false;
      const element = (el.nodeType === 3 ? (el.parentElement as HTMLElement | null) : (el as HTMLElement));
      return !!element && (element === root || root.contains(element));
    }

    // Ensure floating button element exists once
    if (!selectionBtnRef.current) {
      const btn = document.createElement('button');
      btn.setAttribute('type', 'button');
      btn.setAttribute('aria-label', 'Selection action');
      btn.setAttribute('title', 'Selection action');
      btn.tabIndex = -1;
      Object.assign(btn.style, {
        position: 'fixed',
        width: '28px',
        height: '28px',
        borderRadius: '9999px',
        backgroundColor: 'var(--bg-elevated)',
        border: '1px solid var(--overlay-line-90)',
        color: 'var(--text-heading)',
        display: 'none',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: '60',
        boxShadow: '0 2px 8px rgba(0,0,0,0.25)',
        userSelect: 'none',
      } as CSSStyleDeclaration);
      btn.innerHTML = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor"><path d="M12 5v14M5 12h14" stroke-width="2" stroke-linecap="round"/></svg>';
      btn.addEventListener('mousedown', (e) => { e.preventDefault(); });
      btn.addEventListener('mouseenter', () => { if (selectionBtnRef.current) selectionBtnRef.current.style.backgroundColor = 'var(--white-10)'; });
      btn.addEventListener('mouseleave', () => { if (selectionBtnRef.current) selectionBtnRef.current.style.backgroundColor = 'var(--bg-elevated)'; });
      document.body.appendChild(btn);
      selectionBtnRef.current = btn as HTMLButtonElement;
    }

    // Ensure floating menu exists once
    if (!selectionMenuRef.current) {
      const menu = document.createElement('div');
      Object.assign(menu.style, {
        position: 'fixed',
        display: 'none',
        gap: '6px',
        padding: '6px',
        borderRadius: '10px',
        backgroundColor: 'var(--bg-elevated)',
        border: '1px solid var(--overlay-line-90)',
        boxShadow: '0 4px 14px rgba(0,0,0,0.25)',
        zIndex: '61',
        alignItems: 'center',
      } as CSSStyleDeclaration);

      const mkBtn = (label: string, icon: 'edit' | 'note') => {
        const b = document.createElement('button');
        b.setAttribute('type', 'button');
        Object.assign(b.style, {
          fontSize: '12px',
          lineHeight: '18px',
          padding: '4px 8px',
          borderRadius: '8px',
          background: 'transparent',
          color: 'var(--text-heading)',
          border: '1px solid var(--overlay-line-90)',
          display: 'inline-flex',
          alignItems: 'center',
          gap: '6px'
        } as CSSStyleDeclaration);
        // Build icon using SVG namespace (reliable rendering)
        const ns = 'http://www.w3.org/2000/svg';
        const svg = document.createElementNS(ns, 'svg');
        svg.setAttribute('viewBox', '0 0 24 24');
        svg.setAttribute('width', '14');
        svg.setAttribute('height', '14');
        svg.setAttribute('fill', 'none');
        svg.setAttribute('stroke', 'currentColor');
        svg.setAttribute('stroke-width', '2');
        svg.setAttribute('stroke-linecap', 'round');
        svg.setAttribute('stroke-linejoin', 'round');
        if (icon === 'edit') {
          const p1 = document.createElementNS(ns, 'path');
          p1.setAttribute('d', 'M3 21l3-1 11-11-2-2L4 18l-1 3z');
          const p2 = document.createElementNS(ns, 'path');
          p2.setAttribute('d', 'M14 4l2 2');
          svg.appendChild(p1); svg.appendChild(p2);
        } else {
          const rect = document.createElementNS(ns, 'rect');
          rect.setAttribute('x', '4'); rect.setAttribute('y', '3');
          rect.setAttribute('width', '16'); rect.setAttribute('height', '18');
          rect.setAttribute('rx', '2');
          const p1 = document.createElementNS(ns, 'path');
          p1.setAttribute('d', 'M12 8v8');
          const p2 = document.createElementNS(ns, 'path');
          p2.setAttribute('d', 'M8 12h8');
          svg.appendChild(rect); svg.appendChild(p1); svg.appendChild(p2);
        }
        const labelSpan = document.createElement('span');
        labelSpan.textContent = label;
        b.appendChild(svg);
        b.appendChild(labelSpan);
        // Keep selection while pressing menu buttons
        b.addEventListener('mousedown', (e) => { e.preventDefault(); });
        // Handle actions
        if (icon === 'edit') {
          b.addEventListener('click', async () => {
            try {
              const sel = window.getSelection();
              if (!sel || sel.isCollapsed || sel.rangeCount === 0) return;
              const range = sel.getRangeAt(0);
              const startEl = (range.startContainer.nodeType === 3 ? (range.startContainer.parentElement) : (range.startContainer as Element)) as Element | null;
              if (!startEl) return;
              const markdownRoot = startEl.closest('.markdown') as HTMLElement | null;
              if (!markdownRoot) return;
              const interId = markdownRoot.getAttribute('data-interaction-id') || '';
              const selectedText = sel.toString();
              const inter = useGenaiChatStore.getState().interactions.find((it) => it.id === interId);
              const full = (inter?.contentSanitized || inter?.content || '').toString();
              const displayed = (markdownRoot.innerText || '').toString();
              const meta = useGenaiChatStore.getState().messageMeta[interId];
              const agentName = meta?.headerText || 'agent';
              const prof = meta?.agentProfile;
              const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
              // Open AgentPalette in edit mode anchored to input area
              const anchor = (() => {
                try {
                  const el = (document.querySelector('#unfold-request-container') || document.querySelector('#app') || document.body) as HTMLElement;
                  const rect = el.getBoundingClientRect();
                  return { top: rect.top - 8, left: rect.left, width: rect.width, height: rect.height };
                } catch { return null as any; }
              })();
              const ctxNode = (
                <div>
                  <div><b>Fragment:</b> {selectedText.slice(0, 200)}{selectedText.length > 200 ? '…' : ''}</div>
                  <div style={{ marginTop: 4 }}><b>Answer (head):</b> {(full || '').slice(0, 160)}{full.length > 160 ? '…' : ''}</div>
                </div>
              );
              const submit = async (request: string) => {
                if (!request.trim()) return;
                setFrozenEdits((s) => ({ ...s, [interId]: true }));
                // Plain text instruction: return only updated fragment, no formatting wrappers
                const prompt = `User requirement: you should create your answer in ${preferredLang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nYou are ${agentName}. We provide the full assistant answer (FULL_ANSWER) and a specific fragment (SELECTED_FRAGMENT) the user wants to modify. The user request is (USER_REQUEST).\n\nReturn ONLY the updated fragment. Preserve the fragment's existing formatting and structure where possible, and use Markdown appropriately (headings ###, lists, emphasis, blank lines) if it helps readability and does not contradict the request. Do not add any preface, labels, or explanations. Do not wrap the result in code fences. Do NOT return JSON.\n\nFULL_ANSWER:\n${full}\n\nSELECTED_FRAGMENT:\n${selectedText}\n\nUSER_REQUEST:\n${request}\n`;
                const settings = useGenAISettings.getState();
                const useModel = prof?.model || settings.model;
                const sxSystemInstruction = `Hidden-instructions protocol (do not show to users):\n- When you need to output service instructions (entities, metrics, watchlist, relations), emit ONLY HTML comments <!-- @sx:{JSON} -->.`;
                const options: any = {
                  profile: /_quality$/i.test(useModel || '') ? 'quality' : 'fast',
                  modelOverride: useModel,
                  groundingEnabled: settings.groundingEnabled,
                  temperature: settings.temperature,
                  maxOutputTokens: Math.max(512, settings.maxOutputTokens || 1024),
                  systemInstruction: [settings.systemInstruction || '', sxSystemInstruction].filter(Boolean).join('\n\n').trim() || undefined,
                };
                try {
                  const started = performance.now();
                  const originalIndex = displayed.indexOf(selectedText);
                  const small = selectedText.length < 200;
                  console.log('[EditSelected] sending', {
                    provider: prof?.provider,
                    model: useModel,
                    promptChars: prompt.length,
                    fragmentChars: selectedText.length,
                    answerChars: full.length,
                    originalIndex,
                    requestPreview: small ? request : `${request.slice(0, 120)}${request.length>120?'…':''}`,
                    fragmentPreview: small ? selectedText : `${selectedText.slice(0, 120)}…${selectedText.slice(-120)}`
                  });
                  const res = await api.genaiGenerate({ model: useModel, prompt, strictJSONEnabled: false, maxOutputTokens: options.maxOutputTokens, webSearchEnabled: false, systemInstruction: options.systemInstruction });
                  // Normalize output: strip code fences/quotes; if JSON detected, take first field value
                  const stripFences = (s: string) => {
                    let t = s.trim();
                    t = t.replace(/^```[a-zA-Z]*\n([\s\S]*?)\n```$/m, '$1').trim();
                    t = t.replace(/^```[\s\S]*?```$/m, (m) => m.replace(/^```[a-zA-Z]*\n?/,'').replace(/\n?```$/,'')).trim();
                    if ((t.startsWith('"') && t.endsWith('"')) || (t.startsWith('\'') && t.endsWith('\''))) {
                      t = t.slice(1, -1);
                    }
                    return t.trim();
                  };
                  const rawOut = (res?.content || '').toString();
                  let cleaned = stripFences(rawOut);
                  let updated = cleaned;
                  let outFormat: 'json' | 'plain' = 'plain';
                  try {
                    const obj = JSON.parse(cleaned);
                    if (obj && typeof obj === 'object' && !Array.isArray(obj)) {
                      const k = Object.keys(obj)[0];
                      if (k) {
                        updated = String((obj as any)[k] ?? '').trim();
                        outFormat = 'json';
                      }
                    }
                  } catch {}
                  updated = stripFences(updated);
                  if (!updated) throw new Error('Empty model response');
                  // Equality / head-tail previews and char delta
                  const eq = updated === selectedText;
                  const headFrom = selectedText.slice(0, 120);
                  const tailFrom = selectedText.length > 240 ? selectedText.slice(-120) : selectedText.slice(Math.max(0, selectedText.length - 120));
                  const headTo = updated.slice(0, 120);
                  const tailTo = updated.length > 240 ? updated.slice(-120) : updated.slice(Math.max(0, updated.length - 120));
                  const delta = Math.abs(updated.length - selectedText.length);
                  let newContentSanitized = '';
                  try {
                    // Prefer replacing inside original markdown to preserve formatting
                    const md = (inter?.contentSanitized || inter?.content || '').toString();
                    // Build normalized plain string from markdown with index mapping
                    const normalized: string[] = [];
                    const mapIdx: number[] = [];
                    const len = md.length;
                    let i = 0;
                    let atLineStart = true;
                    const pushChar = (ch: string, origIdx: number) => { normalized.push(ch); mapIdx.push(origIdx); atLineStart = (ch === '\n'); };
                    while (i < len) {
                      const ch = md[i];
                      const nxt = md[i+1];
                      // Skip heading markers at line start (###, ##, #, >, - )
                      if (atLineStart) {
                        if (ch === '#') { while (i < len && md[i] === '#') i++; while (i < len && md[i] === ' ') i++; continue; }
                        if (ch === '>') { i++; if (md[i] === ' ') i++; continue; }
                        if (ch === '-' && (md[i+1] === ' ')) { i += 2; continue; }
                      }
                      // Bold/italic markers
                      if ((ch === '*' && nxt === '*') || (ch === '_' && nxt === '_')) { i += 2; continue; }
                      if (ch === '*' || ch === '_' || ch === '~') { i++; continue; }
                      // Inline code fences
                      if (ch === '`') { i++; while (i < len && md[i] !== '`') { pushChar(md[i], i); i++; } if (i < len && md[i] === '`') i++; continue; }
                      // Links [text](url)
                      if (ch === '[') {
                        // collect link text
                        i++; while (i < len && md[i] !== ']') { pushChar(md[i], i); i++; }
                        if (i < len && md[i] === ']') i++;
                        if (i < len && md[i] === '(') { while (i < len && md[i] !== ')') i++; if (i < len && md[i] === ')') i++; }
                        continue;
                      }
                      // Images ![alt](url): skip entirely
                      if (ch === '!' && md[i+1] === '[') { i += 2; while (i < len && md[i] !== ']') i++; if (i < len && md[i] === ']') i++; if (i < len && md[i] === '(') { while (i < len && md[i] !== ')') i++; if (i < len && md[i] === ')') i++; } continue; }
                      // Newlines pass through
                      if (ch === '\n' || ch === '\r') { pushChar('\n', i); i++; continue; }
                      // Otherwise keep char
                      pushChar(ch, i);
                      i++;
                    }
                    // Collapse whitespace in normalized for searching
                    const normStr = normalized.join('').replace(/[\u00A0\u202F]/g, ' ').replace(/\r/g,'').replace(/\s+/g, ' ');
                    const selNorm = selectedText.replace(/[\u00A0\u202F]/g, ' ').replace(/\r/g,'').replace(/\s+/g, ' ').trim();
                    let pos = normStr.indexOf(selNorm);
                    if (pos >= 0) {
                      const startOrig = mapIdx[pos];
                      // find endOrig by advancing selNorm length in normStr and mapping
                      const endMapIdx = mapIdx[pos + selNorm.length - 1];
                      const endOrig = (typeof endMapIdx === 'number') ? (endMapIdx + 1) : startOrig + selectedText.length;
                      const before = md.slice(0, startOrig);
                      const after = md.slice(endOrig);
                      newContentSanitized = before + updated + after;
                    } else {
                      // Fallback: replace in displayed text only (may lose formatting)
                      console.warn('[EditSelected] mapping fallback: using displayed.replace');
                      newContentSanitized = displayed.replace(selectedText, updated);
                    }
                  } catch {
                    newContentSanitized = displayed.replace(selectedText, updated);
                  }
                  useGenaiChatStore.setState((s) => ({ interactions: s.interactions.map((x) => x.id === interId ? ({ ...x, contentSanitized: newContentSanitized }) : x) } as any));
                  const finished = performance.now();
                  console.log('[EditSelected] result', {
                    ms: Math.max(1, Math.round(finished - started)),
                    updatedChars: updated.length,
                    eq,
                    delta,
                    headFrom,
                    tailFrom,
                    headTo,
                    tailTo,
                    replacedOnce: true,
                    fullIndex: originalIndex,
                    outFormat,
                    insertedPreview: updated.length < 200 ? updated : `${updated.slice(0,120)}…${updated.slice(-80)}`
                  });
                  if (eq) {
                    setEditErrors((s) => ({ ...s, [interId]: 'No change produced. Try clarifying the edit request.' }));
                  } else {
                    setEditErrors((s) => ({ ...s, [interId]: null }));
                  }
                } catch (e: any) {
                  console.error('[EditSelected] error', { error: e?.message || String(e) });
                  setEditErrors((s) => ({ ...s, [interId]: e?.message || 'Edit failed' }));
                } finally {
                  setFrozenEdits((s) => ({ ...s, [interId]: false }));
                }
              };
              // Mount AgentPalette (edit)
              const mount = document.createElement('div');
              document.body.appendChild(mount);
              const rootMod = await import('react-dom/client');
              const root = rootMod.createRoot(mount);
              const onClose = () => { try { root.unmount(); mount.remove(); } catch {} };
              root.render(
                React.createElement(AgentPalette as any, {
                  mode: 'edit', visible: true, title: 'Edit selected fragment', subtitle: agentName, anchorRect: anchor,
                  contextNode: ctxNode, submitLabel: 'Apply',
                  onClose, onSubmitAsk: (t: string) => { onClose(); submit(t); }
                })
              );
            } catch (e: any) {
              console.warn('Edit selection failed', e);
              try {
                const sel = window.getSelection();
                const range = sel?.getRangeAt(0);
                const startEl = range ? (range.startContainer.nodeType === 3 ? (range.startContainer.parentElement) : (range.startContainer as Element)) as Element | null : null;
                const interId = startEl?.closest('.markdown')?.getAttribute('data-interaction-id') || '';
                if (interId) setEditErrors((s) => ({ ...s, [interId]: e?.message || 'Edit failed' }));
                if (interId) setFrozenEdits((s) => ({ ...s, [interId]: false }));
              } catch {}
            }
          });
        } else {
        b.addEventListener('click', (e) => { e.preventDefault(); });
        }
        return b;
      };
      const editBtn = mkBtn('Ask to edit selected', 'edit');
      const notesBtn = mkBtn('Add to Notes', 'note');
      // Wire Add to Notes: pin if needed + append selected text to metadata.notes
      if (notesBtn) {
        notesBtn.addEventListener('click', async (e) => {
          e.preventDefault();
          try {
            const sel = window.getSelection?.()?.toString()?.trim() || '';
            if (!sel) return;
            const range = document.getSelection()?.getRangeAt(0);
            const startEl = range ? (range.startContainer.nodeType === 3 ? (range.startContainer.parentElement) : (range.startContainer as Element)) as Element | null : null;
            const interId = startEl?.closest('.markdown')?.getAttribute('data-interaction-id') || '';
            const it = interId ? useGenaiChatStore.getState().interactions.find((x: any) => x.id === interId) : undefined as any;
            if (!it) return;
            const meta = it.metadata || {};
            const notes = Array.isArray(meta.notes) ? [...meta.notes] : [];
            notes.push(sel);
            await api.updateProjectInteraction(useGenaiChatStore.getState().projectId, it.id, { metadata: { pinned: true, notes } });
            await useGenaiChatStore.getState().hydrateInteractions();
            // Hide selection UI and clear selection after successful add
            if (selectionMenuRef.current) selectionMenuRef.current.style.display = 'none';
            if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
            setTimeout(() => {
              const s = window.getSelection && window.getSelection();
              if (s && s.removeAllRanges) s.removeAllRanges();
            }, 100);
          } catch (err) {
            console.warn('[AddToNotes] failed', err);
          }
        });
      }
      menu.appendChild(editBtn);
      menu.appendChild(notesBtn);
      // Prevent selection clearing when starting interaction with the menu
      menu.addEventListener('mousedown', (e) => { e.preventDefault(); });
      document.body.appendChild(menu);
      selectionMenuRef.current = menu;
    }

    // Throttle selection handling to avoid work during fast typing/caret moves
    let selectionThrottle = 0 as any;
    function handleSelectionChange() {
      // Skip if typing in inputs/textareas/contenteditable to reduce background work
      const ae = (document.activeElement as HTMLElement | null);
      if (ae && (ae.tagName === 'TEXTAREA' || ae.tagName === 'INPUT' || ae.isContentEditable)) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        if (selectionMenuRef.current) selectionMenuRef.current.style.display = 'none';
        return;
      }
      // Basic throttle (~60fps max)
      const now = performance.now();
      if (selectionThrottle && now - selectionThrottle < 16) return;
      selectionThrottle = now;
      if (isSelectingRef.current) {
        // Do not show the button while user is dragging to select text
        return;
      }
      const sel = document.getSelection();
      if (!sel || sel.isCollapsed || sel.rangeCount === 0) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        if (selectionMenuRef.current) selectionMenuRef.current.style.display = 'none';
        return;
      }
      const range = sel.getRangeAt(0);
      if (!range) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        return;
      }
      // Ensure selection is within assistant markdown content in this view
      const startEl = (range.startContainer.nodeType === 3 ? (range.startContainer.parentElement) : (range.startContainer as Element)) as Element | null;
      const endEl = (range.endContainer.nodeType === 3 ? (range.endContainer.parentElement) : (range.endContainer as Element)) as Element | null;
      if (!startEl || !endEl) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        return;
      }
      if (!isNodeInside(startEl, containerEl) || !isNodeInside(endEl, containerEl)) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        return;
      }
      // Require that at least one side sits inside a .markdown (assistant bubble)
      const inMarkdown = !!(startEl.closest('.markdown') || endEl.closest('.markdown'));
      if (!inMarkdown) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        return;
      }

      const rects = range.getClientRects();
      const rect = rects.length > 0 ? rects : [range.getBoundingClientRect()];
      if (!rect || rect.length === 0) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        return;
      }
      let minTop = Number.POSITIVE_INFINITY;
      let maxBottom = Number.NEGATIVE_INFINITY;
      for (let i = 0; i < rect.length; i++) {
        const r = rect[i];
        if (!r) continue;
        minTop = Math.min(minTop, r.top);
        maxBottom = Math.max(maxBottom, r.bottom);
      }
      if (!isFinite(minTop) || !isFinite(maxBottom)) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        return;
      }
      const containerRect = containerEl.getBoundingClientRect();
      const buttonSize = 28;
      const gap = 8;
      const top = Math.min(Math.max((minTop + maxBottom) / 2 - buttonSize / 2, 4), window.innerHeight - buttonSize - 4);
      const leftCandidate = (containerRect?.left || 0) - gap - buttonSize;
      const left = Math.max(4, leftCandidate);
      if (selectionBtnRef.current) {
        selectionBtnRef.current.style.top = `${top}px`;
        selectionBtnRef.current.style.left = `${left}px`;
        selectionBtnRef.current.style.display = 'flex';
        selectionBtnRef.current.style.pointerEvents = 'none';
        window.setTimeout(() => {
          if (selectionBtnRef.current) selectionBtnRef.current.style.pointerEvents = 'auto';
        }, 180);
      }
      // Position menu to the right of the main button
      if (selectionMenuRef.current) {
        selectionMenuRef.current.style.top = `${Math.round(top - 2)}px`;
        selectionMenuRef.current.style.left = `${Math.round(left + buttonSize + 8)}px`;
      }
    }

    const onScrollOrResize = () => {
      // Recompute while selection exists; otherwise hide
      const sel = document.getSelection();
      if (!sel || sel.isCollapsed) {
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
      } else {
        handleSelectionChange();
      }
    };

    const onPointerDown = (e: MouseEvent) => {
      const btn = selectionBtnRef.current;
      const menu = selectionMenuRef.current;
      if ((btn && (e.target === btn || (btn.contains(e.target as Node))))) {
        // Keep selection while interacting with the button
        e.preventDefault();
        // Toggle menu visibility
        if (menu) {
          const visible = menu.style.display !== 'none';
          if (!visible) {
            // Ensure menu aligns to current button position
            const rect = btn.getBoundingClientRect();
            const top = rect.top;
            const left = rect.right + 8;
            menu.style.top = `${Math.round(top - 2)}px`;
            menu.style.left = `${Math.round(left)}px`;
            menu.style.display = 'flex';
          } else {
            menu.style.display = 'none';
          }
        }
        return;
      }
      if (menu && (e.target === menu || (menu.contains(e.target as Node)))) {
        // Interactions inside the menu should not close it or clear selection
        e.preventDefault();
        return;
      }
      // Let selection clear naturally; we'll hide on selectionchange
      // Also close menu on any outside click
      if (menu) menu.style.display = 'none';
      setTimeout(() => handleSelectionChange(), 0);
    };

    const onContainerPointerDown = () => {
      isSelectingRef.current = true;
      if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
      if (selectionMenuRef.current) selectionMenuRef.current.style.display = 'none';
    };
    const onDocumentPointerUp = () => {
      if (isSelectingRef.current) {
        isSelectingRef.current = false;
        // Show the button once selection is finalized
        setTimeout(() => handleSelectionChange(), 0);
      }
    };

    document.addEventListener('selectionchange', handleSelectionChange);
    window.addEventListener('scroll', onScrollOrResize, true);
    window.addEventListener('resize', onScrollOrResize);
    document.addEventListener('pointerdown', onPointerDown, true);
    containerEl.addEventListener('pointerdown', onContainerPointerDown, true);
    document.addEventListener('pointerup', onDocumentPointerUp, true);
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (selectionMenuRef.current) selectionMenuRef.current.style.display = 'none';
        if (selectionBtnRef.current) selectionBtnRef.current.style.display = 'none';
        const sel = document.getSelection();
        if (sel) sel.removeAllRanges();
      }
    };
    document.addEventListener('keydown', onKeyDown, true);
    return () => {
      document.removeEventListener('selectionchange', handleSelectionChange);
      window.removeEventListener('scroll', onScrollOrResize, true);
      window.removeEventListener('resize', onScrollOrResize);
      document.removeEventListener('pointerdown', onPointerDown, true);
      containerEl.removeEventListener('pointerdown', onContainerPointerDown, true);
      document.removeEventListener('pointerup', onDocumentPointerUp, true);
      document.removeEventListener('keydown', onKeyDown, true);
      // Cleanup button element
      if (selectionBtnRef.current && selectionBtnRef.current.parentElement) {
        try { selectionBtnRef.current.parentElement.removeChild(selectionBtnRef.current); } catch {}
        selectionBtnRef.current = null;
      }
      if (selectionMenuRef.current && selectionMenuRef.current.parentElement) {
        try { selectionMenuRef.current.parentElement.removeChild(selectionMenuRef.current); } catch {}
        selectionMenuRef.current = null;
      }
    };
  }, [messagesRef.current]);

  // Highlight notes in message content
  useEffect(() => {
    // Defer until DOM is rendered to avoid flicker
    const t = window.setTimeout(() => {
      try {
        const interactions = genaiChat.interactions || [];
        const pairs: Array<{ container: Element; notes: string[] }> = [];
        for (const it of interactions as any[]) {
          const notes = (it?.metadata?.notes || []) as string[];
          if (Array.isArray(notes) && notes.length > 0) {
            const container = document.querySelector(`.markdown[data-interaction-id=\"${it.id}\"]`);
            if (container) {
              pairs.push({ container: container as Element, notes });
            }
          }
        }
        if (pairs.length > 0) {
          const success = applyGlobalHighlight(pairs);
          if (!success) {
            pairs.forEach(({ container, notes }) => highlightNotes(container, notes));
          }
        }
        // Important: do NOT clear highlights on interim empty states to avoid flicker
      } catch (e) {
        console.warn('[Highlight] Failed to process highlights', e);
      }
    }, 50); // Increased delay to ensure DOM is stable after React re-render
    return () => { window.clearTimeout(t); };
  }, [genaiChat.interactions, rightContextOpen, rightPanelOpen]);

  // GenAI thinking seconds timer: runs until first chunk arrives
  useEffect(() => {
    const msgs = genaiChat.messages;
    const last = msgs[msgs.length - 1];
    const awaitingFirstChunk = !!(genaiChat.streaming && last && last.role === 'assistant' && !last.content);
    if (awaitingFirstChunk) {
      if (!genaiThinkingIntervalRef.current) {
        setGenaiThinkingSeconds(0);
        genaiThinkingIntervalRef.current = window.setInterval(() => {
          setGenaiThinkingSeconds((s) => s + 1);
        }, 1000) as unknown as number;
      }
    } else if (genaiThinkingIntervalRef.current) {
      window.clearInterval(genaiThinkingIntervalRef.current);
      genaiThinkingIntervalRef.current = null;
    }
  }, [genaiChat.streaming, genaiChat.messages.length]);

  // Cleanup genai thinking timer on unmount
  useEffect(() => {
    return () => {
      if (genaiThinkingIntervalRef.current) {
        window.clearInterval(genaiThinkingIntervalRef.current);
        genaiThinkingIntervalRef.current = null;
      }
    };
  }, []);

  // Close add source menu on outside click or Escape
  useEffect(() => {
    function onDocMouseDown(e: MouseEvent) {
      if (!showAddSourceMenu) return;
      if (addMenuRef.current && !addMenuRef.current.contains(e.target as Node)) {
        setShowAddSourceMenu(false);
      }
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setShowAddSourceMenu(false);
    }
    document.addEventListener('mousedown', onDocMouseDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDocMouseDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [showAddSourceMenu]);

  const getPreferredLanguage = () => {
    const preferred = (localStorage.getItem('preferredLanguage') || 'EN').toLowerCase();
    return preferred && preferred.length === 2 ? preferred : 'en';
  };

  const normalizeTelegramInput = (value: string): string | null => {
    const v = (value || '').trim();
    if (!v) return null;
    const linkMatch = v.match(/(?:https?:\/\/)?(?:t\.me|telegram\.me)\/([A-Za-z0-9_]{3,})/i);
    if (linkMatch && linkMatch[1]) return '@' + linkMatch[1];
    const atMatch = v.match(/^@([A-Za-z0-9_]{3,})$/);
    if (atMatch && atMatch[1]) return '@' + atMatch[1];
    const bare = v.match(/^([A-Za-z0-9_]{3,})$/);
    if (bare && bare[1]) return '@' + bare[1];
    return null;
  };
  const addTelegramSource = async () => {
    setTgError(null);
    const handle = normalizeTelegramInput(tgInput);
    if (!handle) {
      setTgError('Enter @username or t.me/username');
      return;
    }
    // Pre-seed skeleton (pending source)
    const pending = { id: handle, kind: 'telegram' as const, status: 'fetching' };
    const store = useDiscoveryStore.getState();
    store.setPendingSources([...(store as any).pendingSources, pending]);
    if (currentProject?.id) {
      // Merge pending skeleton into visible list right away
      fetchProjectSources(currentProject.id);
    }

    setTgSubmitting(true);
    try {
      const lang = getPreferredLanguage();
      await executeDiscovery({
        projectId: currentProject?.id || '',
        queryText: handle, // minimal context; backend ignores for existing project
        language: lang,
        sources: { web: [], telegram: [handle] },
      });
      setShowAddTelegramModal(false);
      setTgInput('');
    } catch (e: any) {
      setTgError(e?.message || 'Failed to add channel');
    } finally {
      setTgSubmitting(false);
    }
  };
  const handleDeleteChat = async (projectId: string, projectName: string) => {
    if (!confirm(`Delete "${projectName}"?`)) return;
    
    try {
      await deleteProject(projectId);
    } catch (error) {
      console.error('Failed to delete chat:', error);
    }
  };

  const parseSourcesFromQuery = (text: string) => {
    // Normalize accidental '@' before URLs (e.g., @https://...)
    const normalized = text.trim().replace(/@(?=https?:\/\/)/g, '');
    
    // Extract URLs
    const rawUrls = [
      ...(normalized.match(/https?:\/\/[\w.-]+(?:\/[\w\-._~:\/?#[\]@!$&'()*+,;=%]+)?/gi) || []),
      ...(normalized.match(/\bwww\.[\w.-]+(?:\/[\w\-._~:\/?#[\]@!$&'()*+,;=%]+)?/gi) || [])
    ];
    
    const normalizeUrl = (u: string) => {
      let v = u.trim();
      if (/^www\./i.test(v)) v = `https://${v}`;
      v = v.replace(/[)\],.;]+$/g, ''); // Strip trailing punctuation
      return v;
    };
    
    const webSources = Array.from(new Set(rawUrls.map(normalizeUrl)));

    // Extract Telegram handles
    const tgHandles = new Set<string>();
    
    // t.me / telegram.me links
    const tgLinks = normalized.match(/(?:https?:\/\/)?(?:t\.me|telegram\.me)\/([A-Za-z0-9_]{3,})/gi) || [];
    tgLinks.forEach((m) => {
      const grp = m.match(/(?:t\.me|telegram\.me)\/([A-Za-z0-9_]{3,})/i);
      if (grp && grp[1]) tgHandles.add(`@${grp[1]}`);
    });
    
    // @username tokens
    const ats = normalized.match(/(^|\s)@([A-Za-z0-9_]{3,})\b/g) || [];
    ats.forEach((m) => {
      const grp = m.match(/@([A-Za-z0-9_]{3,})/);
      if (grp && grp[1]) tgHandles.add(`@${grp[1]}`);
    });

    return {
      web: webSources,
      telegram: Array.from(tgHandles),
    };
  };

  const handleExecuteDiscovery = async () => {
    if (running || !queryText.trim()) {
      console.log('[Discovery] Blocked:', { running, hasQuery: !!queryText.trim() });
      return;
    }

    const parsed = parseSourcesFromQuery(queryText);
    console.log('[Discovery] Parsed sources:', parsed);
    
    if (parsed.web.length === 0 && parsed.telegram.length === 0) {
      alert('Please include at least one source (URL or @telegram handle) in your query');
      return;
    }

    // Pre-seed pending sources for immediate visual feedback
    const pending = [
      ...parsed.web.map((url) => ({ id: url, kind: 'web' as const, status: 'fetching' })),
      ...parsed.telegram.map((handle) => ({ id: handle, kind: 'telegram' as const, status: 'fetching' })),
    ];
    useDiscoveryStore.getState().setPendingSources(pending);

    setRunning(true);
    
    try {
      // If no currentProject, backend will create one automatically
      const projectId = currentProject?.id || '';
      
      console.log('[Discovery] Executing with projectId:', projectId);
      const preferred = (localStorage.getItem('preferredLanguage') || 'EN').toLowerCase();
      const lang = preferred && preferred.length === 2 ? preferred : 'en';
      const result = await executeDiscovery({
        projectId,
        queryText,
        language: lang,
        sources: parsed,
      });
      console.log('[Discovery] Result:', result);
      
      correlationRef.current = correlationId;
      // Optimistically append user query interaction (typed as discovery.query)
      const createdAt = new Date().toISOString();
      useDiscoveryStore.setState((state) => ({
        interactions: [
          ...state.interactions,
          { id: createdAt, kind: 'discovery.query', content: queryText, createdAt },
        ],
      }));
      
      // Reload projects after discovery starts (to get newly created project)
      if (!currentProject) {
        setTimeout(() => fetchProjects('discovery'), 1000);
      }

      // Keep running=true until first answer arrives via WS or timeout fires
      if (discoveryTimeoutRef.current) {
        window.clearTimeout(discoveryTimeoutRef.current);
      }
      discoveryTimeoutRef.current = window.setTimeout(() => {
        console.warn('[Discovery] Timeout waiting for first answer');
        setRunning(false);
        discoveryTimeoutRef.current = null;
      }, 90000) as unknown as number;
    } catch (error) {
      console.error('[Discovery] Failed to execute:', error);
      setRunning(false);
    }
  };

  // Stop running when the first answer arrives (from WS store)
  useEffect(() => {
    if (answer && running) {
      if (discoveryTimeoutRef.current) {
        window.clearTimeout(discoveryTimeoutRef.current);
        discoveryTimeoutRef.current = null;
      }
      setRunning(false);
    }
  }, [answer, running]);

  // Cleanup timeout on unmount
  useEffect(() => {
    return () => {
      if (discoveryTimeoutRef.current) {
        window.clearTimeout(discoveryTimeoutRef.current);
        discoveryTimeoutRef.current = null;
      }
    };
  }, []);

  const getSourceIcon = (kind?: string) => {
    if (kind === 'web') {
      return (
        <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 5c7.18 0 13 5.82 13 13" />
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 11a7 7 0 017 7" />
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 18a1 1 0 11-2 0 1 1 0 012 0z" />
        </svg>
      );
    } else if (kind === 'telegram') {
      return (
        <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 11l18-7-7 18-4-8-7-3z" />
        </svg>
      );
    }
    return (
      <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
        <circle cx="12" cy="12" r="9" strokeWidth={2} />
      </svg>
    );
  };

  const renderSourceCard = (source: any) => {
    const isFetched = source.status === 'completed' || !!source.profile;
    const inferredKind = source.kind || (typeof source.id === 'string' && source.id.startsWith('@') ? 'telegram' : undefined);

    return (
      <div
        key={source.id}
        className="text-xs rounded-2xl cursor-pointer"
        style={{
          backgroundColor: 'var(--bg-sidebar)',
          border: '1px solid var(--overlay-line-90)',
        }}
        role="button"
        onClick={() => setPreviewSource(source)}
      >
        <div className="flex items-center gap-2 px-3 py-2">
          {isFetched ? (
            <svg className="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" style={{ color: 'var(--accent-success)' }}>
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4" />
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 22C6.477 22 2 17.523 2 12S6.477 2 12 2s10 4.477 10 10-4.477 10-10 10z" />
            </svg>
          ) : (
            getSourceIcon(inferredKind)
          )}
          <span>
            <strong style={{ fontWeight: 500 }}>{inferredKind || 'source'}</strong> {source.displayName || source.id}
          </span>
          <span className="ml-auto">
            {source.status === 'processing' && (
              <span className="animate-pulse" style={{ color: 'var(--text-secondary-strong)' }}>...</span>
            )}
            {source.status === 'failed' && (
              <span style={{ color: 'var(--accent-danger)' }}>failed</span>
            )}
          </span>
        </div>

        <div className="px-3 pb-2" style={{ color: 'var(--text-secondary-strong)' }}>
          {source.profile?.motivation ? (
            <span>{source.profile.motivation}</span>
          ) : source.profile?.agenda?.description ? (
            <span>{source.profile.agenda.description}</span>
          ) : source.status === 'processing' ? (
            <span className="animate-pulse" style={{ color: 'var(--text-secondary-strong)' }}>...</span>
          ) : !isFetched && (source as any).__firstProfile && (source as any).__hint === 'profilingNewSource' ? (
            <span style={{ color: 'var(--text-secondary-strong)' }}>
              Our system is establishing the channel's analytical profile based on its content.
            </span>
          ) : source.description ? (
            <span>{source.description}</span>
          ) : null}
        </div>

        {source.profile?.agenda?.primaryGoal && (
          <div className="ml-5 mt-1 rounded mb-1" style={{ color: 'var(--text-heading)' }}>
            <div>
              <span style={{ color: 'var(--text-secondary-strong)' }}>primaryGoal:</span> {source.profile.agenda.primaryGoal}
            </div>
          </div>
        )}

        {source.profile && (
          <div className="px-3 pb-3 space-y-2 mt-2">
            {source.profile.topics && source.profile.topics.length > 0 && (
              <div className="flex flex-wrap gap-1">
                {source.profile.topics.slice(0, 6).map((t: any, i: number) => (
                  <span
                    key={i}
                    className="px-2 py-0.5 rounded-full text-[10px]"
                    style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}
                  >
                    {t.name}
                    {typeof t.share === 'number' ? ` ${Math.round(t.share * 100)}%` : ''}
                  </span>
                ))}
              </div>
            )}

            {(typeof source.profile.trustScore === 'number' || typeof source.profile.goalAlignmentScore === 'number') && (
              <div className="flex items-center gap-3 text-[10px]" style={{ color: 'var(--text-secondary-strong)' }}>
                {typeof source.profile.goalAlignmentScore === 'number' && (
                  <div className="flex items-center gap-1">
                    <span>alignment</span>
                    <div className="w-24 h-1.5 rounded" style={{ backgroundColor: 'var(--overlay-line-90)' }}>
                      <div
                        className="h-1.5 rounded"
                        style={{
                          width: `${Math.max(0, Math.min(1, source.profile.goalAlignmentScore)) * 100}%`,
                          backgroundColor: 'var(--accent-success)',
                        }}
                      />
                    </div>
                    <span>{Math.round(Math.max(0, Math.min(1, source.profile.goalAlignmentScore)) * 100)}%</span>
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    );
  };


  React.useEffect(() => {
    const handler = (e: any) => {
      const kind = e?.detail?.kind as 'privacy' | 'terms';
      if (kind === 'privacy' || kind === 'terms') {
        setShowSettings(false);
        setStaticPage(kind);
      }
    };
    window.addEventListener('ui.openPage', handler as any);
    return () => window.removeEventListener('ui.openPage', handler as any);
  }, []);
  return (
    <div className="flex h-full" style={{ padding: '16px 0' }}>
      {/* Side panel (hidden by default) - LEFT */}
      {sideOpen && (
        <div
          className="flex-shrink-0 rounded-xl mr-4 overflow-hidden relative sidebar-panel"
          style={{ backgroundColor: 'var(--bg-sidebar)', width: '320px', minWidth: '260px', maxWidth: '420px' }}
        >
          <div className="p-6 h-full flex flex-col">
            <div className="flex items-center justify-between mb-6">
              <h3 className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>Navigation</h3>
              <button
                onClick={() => setSideOpen(false)}
                className="p-2 rounded-lg transition-colors"
                style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-20)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
                </svg>
              </button>
            </div>

            {/* New Chat action */}
            <div className="mb-3">
              <button
                type="button"
                aria-label="New Chat"
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors"
                style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => {
                  // Clear right side: sources, status, input, current chat, interactions
                  clearSources();
                  setCurrentProject(null);
                  setQueryText('');
                  // Reset high-risk state
                  setIsHighRisk(false);
                  setRiskBanner(null);
                  // Reset Unfold chat state and analysis artifacts
                  try {
                    useGenaiChatStore.getState().reset();
                    // Force new Draft creation on next message
                    useGenaiChatStore.getState().setProjectId('local-unfold');
                    try { console.log('[NewChat] reset -> projectId set to local-unfold'); } catch {}
                  } catch {}
                  setShowUserAnalysis(false);
                  setShowLLMReview(false);
                setShowDiscussionFlow(false);
                setShowArtifacts(false);
                }}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  {/* Outlined plus in rounded rect (new chat) */}
                  <rect x="3.5" y="3.5" width="17" height="17" rx="3" strokeWidth={2} />
                  <path strokeLinecap="round" strokeWidth={2} d="M12 8v8M8 12h8" />
                </svg>
                <span className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>New Chat</span>
              </button>
            </div>
            {/* Chats section */}
            <div className="mb-4">
              <div className="text-[11px] uppercase tracking-wide mb-2" style={{ color: 'var(--text-tertiary-weak)' }}>
                Chats
              </div>

              {projects.length === 0 ? (
                <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>No chats yet</div>
              ) : (
                <div className="space-y-1">
                  {projects.map((project) => (
                    <div
                      key={project.id}
                      className="px-2 py-1 rounded text-xs flex items-center gap-2 group cursor-pointer"
                      title={project.displayName}
                      onClick={() => setCurrentProject(project)}
                      style={{
                        backgroundColor: currentProject?.id === project.id ? 'var(--white-10)' : 'transparent',
                      }}
                      onMouseEnter={(e) => { if (currentProject?.id !== project.id) (e.currentTarget as HTMLDivElement).style.backgroundColor = 'var(--white-10)'; }}
                      onMouseLeave={(e) => { if (currentProject?.id !== project.id) (e.currentTarget as HTMLDivElement).style.backgroundColor = 'transparent'; }}
                    >
                      <span className="flex-1 truncate" style={{ color: 'var(--text-heading)' }}>
                        {project.displayName}
                      </span>
                      <button
                        className="opacity-0 group-hover:opacity-100 transition-opacity p-1 rounded"
                        title="Delete chat"
                        style={{ backgroundColor: 'transparent' }}
                        onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                        onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                        onClick={(e) => {
                          e.stopPropagation();
                          handleDeleteChat(project.id, project.displayName);
                        }}
                      >
                        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 7h12M10 11v6M14 11v6M9 7l1-2h4l1 2M6 7l1 12a2 2 0 002 2h6a2 2 0 002-2l1-12" />
                        </svg>
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Analysis shortcuts (duplicate of floating buttons when sidebar hidden) */}
            {(availUserAnalysis || availDiscussionFlow || availArtifacts) && (
            <div className="mb-4 pt-3" style={{ borderTop: '1px solid var(--overlay-line-90)' }}>
              <div className="text-[11px] uppercase tracking-wide mb-2" style={{ color: 'var(--text-tertiary-weak)' }}>
                Analysis
              </div>
              {availUserAnalysis && (
              <button
                type="button"
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors mb-1"
                style={{ backgroundColor: leftPanel === 'user' ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { if (leftPanel !== 'user') (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { if (leftPanel !== 'user') (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => setLeftPanel((p) => (p === 'user' ? null : 'user'))}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <circle cx="12" cy="8" r="3.5" strokeWidth={2} />
                  <path d="M5 20c0-3.314 3.134-6 7-6s7 2.686 7 6" strokeWidth={2} strokeLinecap="round" />
                </svg>
                <span className="text-12">How you engage</span>
              </button>
              )}
              {availDiscussionFlow && (
              <button
                type="button"
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors mb-1"
                style={{ backgroundColor: leftPanel === 'flow' ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { if (leftPanel !== 'flow') (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { if (leftPanel !== 'flow') (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => setLeftPanel((p) => (p === 'flow' ? null : 'flow'))}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <path d="M5 5h6v6H5z" strokeWidth={2} />
                  <path d="M13 7h6" strokeWidth={2} strokeLinecap="round" />
                  <path d="M13 17h6" strokeWidth={2} strokeLinecap="round" />
                  <path d="M8 11v2c0 2.209 1.791 4 4 4h1" strokeWidth={2} strokeLinecap="round" />
                </svg>
                <span className="text-12">Discussion Flow</span>
              </button>
              )}
              {availArtifacts && (
              <button
                type="button"
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors"
                style={{ backgroundColor: leftPanel === 'artifacts' ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { if (leftPanel !== 'artifacts') (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { if (leftPanel !== 'artifacts') (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => setLeftPanel((p) => (p === 'artifacts' ? null : 'artifacts'))}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <rect x="5" y="5" width="14" height="6" rx="1.5" strokeWidth={2} />
                  <rect x="5" y="13" width="14" height="6" rx="1.5" strokeWidth={2} />
                </svg>
                <span className="text-12">Artifacts</span>
              </button>
              )}
            </div>
            )}

            {/* Bottom settings button */}
            <div className="mt-auto pt-4">
              {isAdmin && (
                <button
                  type="button"
                  aria-label="Admin access"
                  className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors mb-1"
                  style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                  onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                  onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                  onClick={() => { setShowAdmin(true); setShowSettings(false); setStaticPage(null); }}
                >
                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    {/* Shield icon to reflect admin access */}
                    <path d="M12 3l7 3v6c0 5-3.5 8-7 9-3.5-1-7-4-7-9V6l7-3z" strokeWidth={2} />
                    <path d="M9.5 12.5l1.8 1.8 3.2-3.3" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                  <span className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>Admin access</span>
                </button>
              )}
              <button
                type="button"
                aria-label="Settings"
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors"
                style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => setShowSettings(true)}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  {/* Outlined gear (non-filled), simple and monotone */}
                  <circle cx="12" cy="12" r="4.5" strokeWidth={2} />
                  <path strokeLinecap="round" strokeWidth={2} d="M12 3v2M12 19v2M3 12h2M19 12h2M5.636 5.636l1.414 1.414M16.95 7.05l1.414-1.414M18.364 18.364l-1.414-1.414M7.05 16.95l-1.414 1.414" />
                </svg>
                <span className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>Settings</span>
              </button>
            </div>
          </div>

          
        </div>
      )}

      {/* Toggle + New Chat buttons stacked (visible only when sidebar is hidden) */}
      {!sideOpen && (
        <div id="left-toggle-anchor" className="flex-shrink-0 ml-2 mr-2 flex flex-col" style={{ alignSelf: 'flex-start', marginTop: '20px' }}>
          <button
            onClick={() => setSideOpen(true)}
            className="w-8 h-8 rounded-lg flex items-center justify-center transition-all duration-300"
            style={{ backgroundColor: '#669999', color: 'white' }}
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M9 5l7 7-7 7" />
            </svg>
          </button>

          <button
            onClick={() => {
              // Perform the same actions as the "New Chat" button in the sidebar
              clearSources();
              setCurrentProject(null);
              setQueryText('');
                // Reset high-risk state
                setIsHighRisk(false);
                setRiskBanner(null);
              try {
                useGenaiChatStore.getState().reset();
                useGenaiChatStore.getState().setProjectId('local-unfold');
                try { console.log('[NewChat:compact] reset -> projectId set to local-unfold'); } catch {}
              } catch {}
              setShowUserAnalysis(false);
              setShowLLMReview(false);
                setShowDiscussionFlow(false);
                setShowArtifacts(false);
              setLeftPanel(null);
            }}
            className="w-8 h-8 rounded-lg flex items-center justify-center transition-all duration-300"
            style={{ backgroundColor: 'var(--newchat-bg)', color: 'var(--text-heading)', marginTop: '8px' }}
            aria-label="New Chat"
            title="New Chat"
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--newchat-bg-hover)'; }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--newchat-bg)'; }}
          >
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
              <path strokeLinecap="round" strokeWidth={2.5} d="M12 6v12M6 12h12" />
            </svg>
          </button>

          {/* Person button, visually analogous to New Chat, placed ~2 buttons below */}
          {availUserAnalysis && (
            <button
              onClick={() => { setLeftPanel((p) => (p === 'user' ? null : 'user')); }}
              className="w-8 h-8 rounded-lg flex items-center justify-center transition-all duration-300"
              style={{ backgroundColor: leftPanel === 'user' ? '#669999' : 'var(--newchat-bg)', color: leftPanel === 'user' ? 'white' : 'var(--text-heading)', marginTop: '72px' }}
              aria-label="User"
              title="User"
              onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = leftPanel === 'user' ? '#669999' : 'var(--newchat-bg-hover)'; }}
              onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = leftPanel === 'user' ? '#669999' : 'var(--newchat-bg)'; }}
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <circle cx="12" cy="8" r="3.5" strokeWidth={2} />
                <path d="M5 20c0-3.314 3.134-6 7-6s7 2.686 7 6" strokeWidth={2} strokeLinecap="round" />
              </svg>
            </button>
          )}

          {/* Discussion Flow button (visible only when discussion flow is available) */}
          {availDiscussionFlow && (
            <button
              onClick={() => { setLeftPanel((p) => (p === 'flow' ? null : 'flow')); }}
              className="w-8 h-8 rounded-lg flex items-center justify-center transition-all duration-300"
              style={{ backgroundColor: leftPanel === 'flow' ? '#669999' : 'var(--newchat-bg)', color: leftPanel === 'flow' ? 'white' : 'var(--text-heading)', marginTop: '8px' }}
              aria-label="Discussion Flow"
              title="Discussion Flow"
              onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = leftPanel === 'flow' ? '#669999' : 'var(--newchat-bg-hover)'; }}
              onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = leftPanel === 'flow' ? '#669999' : 'var(--newchat-bg)'; }}
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <path d="M5 5h6v6H5z" strokeWidth={2} />
                <path d="M13 7h6" strokeWidth={2} strokeLinecap="round" />
                <path d="M13 17h6" strokeWidth={2} strokeLinecap="round" />
                <path d="M8 11v2c0 2.209 1.791 4 4 4h1" strokeWidth={2} strokeLinecap="round" />
              </svg>
            </button>
          )}

          {/* Artifacts button (visible only when artifacts are available) */}
          {availArtifacts && (
            <button
              onClick={() => { setLeftPanel((p) => (p === 'artifacts' ? null : 'artifacts')); }}
              className="w-8 h-8 rounded-lg flex items-center justify-center transition-all duration-300"
              style={{ backgroundColor: leftPanel === 'artifacts' ? '#669999' : 'var(--newchat-bg)', color: leftPanel === 'artifacts' ? 'white' : 'var(--text-heading)', marginTop: '8px' }}
              aria-label="Artifacts"
              title="Artifacts"
              onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = leftPanel === 'artifacts' ? '#669999' : 'var(--newchat-bg-hover)'; }}
              onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = leftPanel === 'artifacts' ? '#669999' : 'var(--newchat-bg)'; }}
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <rect x="5" y="5" width="14" height="6" rx="1.5" strokeWidth={2} />
                <rect x="5" y="13" width="14" height="6" rx="1.5" strokeWidth={2} />
              </svg>
            </button>
          )}
        </div>
      )}

      {/* Mirror toggle button on the right edge (independent of sidebar) */}
      <button
        type="button"
        aria-label="Toggle Right Panel"
        className="mirror-doc-btn rounded-lg flex items-center justify-center transition-all duration-300"
        style={{ position: 'fixed', right: 8, top: 76, width: 32, height: 32, color: 'white', zIndex: 40 }}
        onClick={() => {
          console.log('[RightPanel] toggle', !rightPanelOpen);
          // Ensure mutual exclusivity
          setRightContextOpen(false);
          setRightArtifactsOpen(false);
          setRightPanelOpen((v) => !v);
        }}
      >
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" aria-hidden="true">
          <path d="M6 3h7l5 5v11a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
          <path d="M13 3v5h5" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>

      {/* Context Navigation button (gray, like New Chat) shown on right edge */}
      {hasPinned && (
        <button
          type="button"
          aria-label="Context Navigation"
          title="Context Navigation"
          onClick={() => {
            // Ensure mutual exclusivity
            setRightPanelOpen(false);
            setRightArtifactsOpen(false);
            setRightContextOpen((v) => !v);
          }}
          className="rounded-lg flex items-center justify-center transition-all duration-300"
          style={{
            position: 'fixed',
            right: 8,
            top: 116,
            width: 32,
            height: 32,
            backgroundColor: 'var(--newchat-bg)',
            color: 'var(--text-heading)',
            zIndex: 40,
          }}
          onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--newchat-bg-hover)'; }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--newchat-bg)'; }}
        >
          {/* Icon: compass/graph for context navigation */}
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <circle cx="12" cy="12" r="9" strokeWidth={2} />
            <path d="M10 14l2-6 6-2-2 6-6 2z" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
      )}

      {/* Workspace Artifacts button (pastel green) shown on right edge */}
      <button
        id="btn-artifacts"
        type="button"
        aria-label="Workspace Artifacts"
        title="Workspace Artifacts"
        onClick={() => {
          // Ensure mutual exclusivity
          setRightPanelOpen(false);
          setRightContextOpen(false);
          setRightArtifactsOpen((v) => !v);
        }}
        className="rounded-lg flex items-center justify-center transition-all duration-300"
        style={{
          position: 'fixed',
          right: 8,
          top: hasPinned ? 156 : 116,
          width: 32,
          height: 32,
          backgroundColor: rightArtifactsOpen ? 'hsl(140, 45%, 65%)' : 'hsl(140, 40%, 70%)',
          color: 'hsl(140, 50%, 20%)',
          zIndex: 40,
        }}
        onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'hsl(140, 45%, 65%)'; }}
        onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = rightArtifactsOpen ? 'hsl(140, 45%, 65%)' : 'hsl(140, 40%, 70%)'; }}
      >
        {/* Icon: layers/stack for artifacts */}
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M12 2L2 7l10 5 10-5-10-5z" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
          <path d="M2 17l10 5 10-5" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
          <path d="M2 12l10 5 10-5" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      {/* Floating Settings button (visible only when sidebar is hidden) */}
      {!sideOpen && !showSettings && (
        <button
          type="button"
          aria-label="Settings"
          onClick={() => setShowSettings(true)}
          className="rounded-lg transition-colors flex items-center justify-center"
          style={{
            position: 'fixed',
            left: 8,
            bottom: 48,
            width: 32,
            height: 32,
            backgroundColor: 'transparent',
            color: 'var(--text-heading)',
            border: 'none',
            zIndex: 40,
          }}
          onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
          onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
        >
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
            <circle cx="12" cy="12" r="4.5" strokeWidth={2} />
            <path strokeLinecap="round" strokeWidth={2} d="M12 3v2M12 19v2M3 12h2M19 12h2M5.636 5.636l1.414 1.414M16.95 7.05l1.414-1.414M18.364 18.364l-1.414-1.414M7.05 16.95l-1.414 1.414" />
          </svg>
        </button>
      )}

      {/* Grid wrapper to the right of the sidebar: Sources | Center | Right */}
      <div
        className="flex-1 min-h-0"
        style={{
          position: 'relative',
          display: 'grid',
          gridTemplateColumns: `minmax(320px, 25%) var(--center-col-width, minmax(640px, 1fr)) minmax(320px, 25%)`,
          gap: '16px',
        }}
      >
        {/* Sources column (scrollable) */}
        <div className="min-h-0 h-full" style={{ background: 'transparent', display: 'flex', flexDirection: 'column' }}>
          {sources.length > 0 && (
            <div className="space-y-2">
              {sources.map((source) => renderSourceCard(source))}
            </div>
          )}

          {/* Analysis toggles removed */}

          {/* Headless runner to fetch user stance in background so the button appears */}
          <div style={{ display: 'none' }} aria-hidden>
            <UserPositionAnalysis
              onFirstResult={() => { try { setBadgeUser(true); setAvailUserAnalysis(true); } catch {} }}
              onRiskUpdate={(r) => {
                const high = !!(r && r.level === 'High');
                setIsHighRisk(high);
                if (high) {
                  setRiskBanner((prev) => (prev && prev.visible === false ? prev : { visible: true, level: 'High', rationale: r?.rationale || '' }));
                } else if (!r) {
                  setRiskBanner(null);
                }
              }}
            />
          </div>

          {/* Headless runner for bias evaluation (assistant + user messages) */}
          <div style={{ display: 'none' }} aria-hidden>
            <LLMEvaluation />
          </div>

          {/* Left panel holder: matches sidebar style, single active section */}
          {leftPanel && (
            <div
              className="mt-3 p-3 rounded-xl sidebar-panel"
              style={{
                backgroundColor: 'var(--bg-sidebar)',
                border: '1px solid var(--overlay-line-90)',
                display: 'flex',
                flexDirection: 'column',
                flex: 1,
                minHeight: 0,
                overflowY: 'auto',
              }}
            >
              {/* Header: chosen section title + close */}
              {leftPanel && (
                <div className="flex items-center justify-between" style={{ position: 'sticky', top: 0, backgroundColor: 'var(--bg-sidebar)', paddingBottom: 8, marginBottom: 8, zIndex: 1 }}>
                  <h3 className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>{leftPanel === 'user' ? 'How you engage' : (leftPanel === 'flow' ? 'Discussion flow' : 'Artifacts')}</h3>
                  <button type="button" aria-label="Close" title="Close" className="p-2 rounded-lg transition-colors" style={{ backgroundColor: 'transparent', color: 'var(--text-secondary-strong)' }} onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }} onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }} onClick={() => setLeftPanel(null)}>
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path strokeLinecap="round" strokeWidth={2} d="M6 6l12 12M18 6L6 18" /></svg>
                  </button>
                </div>
              )}

              {/* Content area: selected section */}
              {leftPanel && (
                <div style={{ display: 'flex', flexDirection: 'column', minHeight: 0, flex: 1 }}>
                  <div style={{ flex: 1, minHeight: 0 }}>
                    {leftPanel === 'user' && (
                      <UserPositionAnalysis hideTitle onFirstResult={() => { setBadgeUser(true); setAvailUserAnalysis(true); }} onRiskUpdate={(r) => {
                        const high = !!(r && r.level === 'High');
                        setIsHighRisk(high);
                        if (high) {
                          setRiskBanner((prev) => (prev && prev.visible === false ? prev : { visible: true, level: 'High', rationale: r?.rationale || '' }));
                        } else if (!r) {
                          setRiskBanner(null);
                        }
                      }} />
                    )}
                    {leftPanel === 'flow' && (
                      <DiscussionFlow onFirstResult={() => { setBadgeFlow(true); setAvailDiscussionFlow(true); }} />
                    )}
                    {leftPanel === 'artifacts' && (
                      <div className="p-1">
                        {(() => {
                          const arts = genaiChat.artifacts;
                          const any = (arts.entities && arts.entities.length > 0) || (arts.metrics && arts.metrics.length > 0);
                          if (!any) return <div className="text-12" style={{ color: 'var(--text-secondary)' }}>No artifacts yet.</div>;
                          return (
                            <div className="space-y-2">
                              {arts.entities && arts.entities.length > 0 && (
                                <div>
                                  <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>Entities</div>
                                  <ul className="text-12 space-y-1" style={{ color: 'var(--text-secondary-strong)' }}>
                                    {arts.entities.map((e, idx) => (
                                      <li key={`ent-${idx}`} className="flex items-start gap-2">
                                        <span aria-hidden className="w-4 h-4 flex items-center justify-center" style={{ color: 'var(--text-secondary-strong)' }}>
                                          {e.kind === 'person' ? (
                                            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor"><circle cx="12" cy="8" r="3.5" strokeWidth={2}/><path d="M5 20c0-3.314 3.134-6 7-6s7 2.686 7 6" strokeWidth={2} strokeLinecap="round"/></svg>
                                          ) : e.kind === 'org' ? (
                                            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor"><rect x="4" y="8" width="16" height="10" rx="2" strokeWidth={2}/><path d="M8 8V6a4 4 0 014-4h0a4 4 0 014 4v2" strokeWidth={2}/></svg>
                                          ) : e.kind === 'product' ? (
                                            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor"><path d="M3 7l9-4 9 4-9 4-9-4z" strokeWidth={2}/><path d="M3 7v10l9 4 9-4V7" strokeWidth={2}/></svg>
                                          ) : e.kind === 'location' ? (
                                            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor"><path d="M12 22s7-6.582 7-12a7 7 0 10-14 0c0 5.418 7 12 7 12z" strokeWidth={2}/><circle cx="12" cy="10" r="2.5" strokeWidth={2}/></svg>
                                          ) : e.kind === 'event' ? (
                                            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor"><rect x="3" y="5" width="18" height="16" rx="2" strokeWidth={2}/><path d="M8 3v4M16 3v4M3 11h18" strokeWidth={2}/></svg>
                                          ) : (
                                            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor"><circle cx="12" cy="12" r="4" strokeWidth={2}/></svg>
                                          )}
                                        </span>
                                        <div className="flex-1">
                                          <div className="leading-[1.3]" style={{ color: 'var(--text-secondary-strong)' }}>{e.name}</div>
                                          {e.description && <div className="text-[11px]" style={{ color: 'var(--text-secondary)' }}>{e.description}</div>}
                                        </div>
                                      </li>
                                    ))}
                                  </ul>
                                </div>
                              )}
                              {arts.metrics && arts.metrics.length > 0 && (
                                <div>
                                  <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>Metrics</div>
                                  <ul className="text-12 space-y-1" style={{ color: 'var(--text-secondary-strong)' }}>
                                    {arts.metrics.map((m, idx) => (
                                      <li key={`met-${idx}`} className="flex items-start gap-2">
                                        <span aria-hidden className="w-4 h-4 flex items-center justify-center" style={{ color: 'var(--text-secondary-strong)' }}>
                                          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor"><path d="M4 18h16" strokeWidth={2} strokeLinecap="round"/><path d="M6 18V8M10 18v-5M14 18v-9M18 18v-3" strokeWidth={2} strokeLinecap="round"/></svg>
                                        </span>
                                        <button type="button" className="flex-1 text-left rounded-md" style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }} title="Open metric analysis" onClick={() => { try { genaiChat.openMetricOverlay(m as any); } catch {} }} onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }} onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}>
                                          <div className="leading-[1.3]" style={{ color: 'var(--text-secondary-strong)' }}>{m.metricName || (m as any).name || m.id}</div>
                                          {m.unit && <div className="text-[11px]" style={{ color: 'var(--text-secondary)' }}>Unit: {m.unit}</div>}
                                          {m.frequency && <div className="text-[11px]" style={{ color: 'var(--text-secondary)' }}>Frequency: {m.frequency}</div>}
                                        </button>
                                      </li>
                                    ))}
                                  </ul>
                                </div>
                              )}
                            </div>
                          );
                        })()}
                      </div>
                    )}
                  </div>
                </div>
              )}
              {/* Duplicate old header/content removed to avoid double rendering */}
            </div>
          )}

          {/* Bottom slot: topic title suggestions (reserved area, fixed at column bottom) */}
          <div className="mt-3" style={{ marginTop: 'auto', minHeight: titleSlotMinHeight, position: 'relative' }}>
            {/* Floating reopen button (left rail), appears only when suggestions exist and panel is hidden */}
            {!titlesVisible && titlesAvailable && !sideOpen && (
              <button
                type="button"
                aria-label="Show title suggestions"
                title="Show title suggestions"
                className={`w-8 h-8 rounded-lg flex items-center justify-center transition-all duration-300 ${titlesGlow ? 'animate-pulse' : ''}`}
                style={{ position: 'fixed', left: 8, bottom: Math.max(56, titleSlotMinHeight) + 12, backgroundColor: 'var(--newchat-bg)', color: 'var(--text-heading)', boxShadow: titlesGlow ? '0 0 0 3px rgba(0, 200, 200, 0.28), 0 0 16px rgba(0, 200, 200, 0.60), 0 0 28px rgba(0, 180, 180, 0.45)' : 'none' }}
                onClick={() => { setTitlesVisible(true); setTitlesGlow(false); }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--newchat-bg-hover)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--newchat-bg)'; }}
              >
                {/* Label/tag icon */}
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ filter: titlesGlow ? 'drop-shadow(0 0 6px rgba(0, 200, 200, 0.65))' : 'none' }}>
                  <path d="M3 12l7-7h4l7 7-7 7h-4z" strokeWidth="2" />
                  <circle cx="10" cy="10" r="1.5" fill="currentColor" />
                </svg>
              </button>
            )}
            <TitleSuggestions
              visible={titlesVisible}
              onClose={() => {
                setTitlesVisible(false);
                setTitlesGlow(true);
                window.setTimeout(() => setTitlesGlow(false), 1600);
              }}
              onAvailableChange={setTitlesAvailable}
            />
          </div>

          {/* Add source button (only when chat exists and has sources) */}
          {currentProject?.id && sources.length > 0 && (
            <div className="mt-2" ref={addMenuRef}>
              <button
                type="button"
                title="Add source"
                aria-haspopup="true"
                aria-expanded={showAddSourceMenu}
                onClick={() => setShowAddSourceMenu((v) => !v)}
                className="rounded-lg transition-colors flex items-center justify-center"
                style={{
                  width: '32px',
                  height: '32px',
                  backgroundColor: 'transparent',
                  color: 'var(--text-heading)',
                  border: '1px solid var(--overlay-line-90)'
                }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <path strokeLinecap="round" strokeWidth={2} d="M12 6v12M6 12h12" />
                </svg>
              </button>

              {showAddSourceMenu && (
                <div
                  role="menu"
                  aria-label="Add source options"
                  className="mt-2 rounded-xl shadow-lg"
                  style={{
                    backgroundColor: 'var(--bg-elevated)',
                    border: '1px solid var(--overlay-line-90)'
                  }}
                >
                  {/* Telegram Channel */}
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => { setShowAddSourceMenu(false); setShowAddTelegramModal(true); }}
                    className="w-full flex items-center gap-2 px-3 py-2 rounded-t-xl text-left transition-colors"
                    style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 11l18-7-7 18-4-8-7-3z" />
                    </svg>
                    <span className="text-13">Telegram Channel</span>
                  </button>

                  {/* Web Page */}
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => setShowAddSourceMenu(false)}
                    className="w-full flex items-center gap-2 px-3 py-2 text-left transition-colors"
                    style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <circle cx="12" cy="12" r="9" strokeWidth={2} />
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 12h18M12 3a15 15 0 010 18M12 3a15 15 0 000 18" />
                    </svg>
                    <span className="text-13">Web Page</span>
                  </button>

                  {/* RSS */}
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => setShowAddSourceMenu(false)}
                    className="w-full flex items-center gap-2 px-3 py-2 text-left transition-colors"
                    style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 5c7.18 0 13 5.82 13 13" />
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 11a7 7 0 017 7" />
                      <circle cx="6" cy="18" r="1.5" fill="currentColor" />
                    </svg>
                    <span className="text-13">RSS</span>
                  </button>

                  {/* Document upload */}
                  <label
                    className="w-full flex items-center gap-2 px-3 py-2 rounded-b-xl text-left cursor-pointer"
                    style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLLabelElement).style.backgroundColor = 'var(--white-10)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLLabelElement).style.backgroundColor = 'transparent'; }}
                  >
                    <input
                      type="file"
                      accept=".txt,.md,.csv,.tsv,application/pdf,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document,text/plain,text/markdown,text/csv,text/tab-separated-values"
                      style={{ display: 'none' }}
                      onChange={async (e) => {
                        const file = e.target.files?.[0];
                        if (!file) return;
                        try {
                          setShowAddSourceMenu(false);
                          const pid = useGenaiChatStore.getState().projectId || (await useGenaiChatStore.getState().ensureProject());
                          const doc = await api.docsUpload(pid as string, file);
                          try { await api.docsParse(pid as string, doc.id); } catch {}
                        } catch (err) {
                          console.error('[Discovery] Document upload failed', err);
                        } finally {
                          (e.currentTarget as HTMLInputElement).value = '';
                        }
                      }}
                    />
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 3h7l4 4v14a2 2 0 01-2 2H7a2 2 0 01-2-2V5a2 2 0 012-2z" />
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14 3v4h4" />
                    </svg>
                    <span className="text-13">Document</span>
                  </label>
                </div>
              )}
            </div>
          )}
        </div>

        {/* Main center column: renders discussion or graph placeholder (spans center+right when graph) */}
        <div
          className="min-h-0 flex flex-col"
          style={{ position: 'relative', gridColumn: viewMode === 'graph' ? '2 / 4' : '2 / 3' }}
        >
        {showSettings ? (
          <div className="w-full px-4 mt-2 mb-2">
            <div className="mb-2 flex items-center">
              <button
                onClick={() => setShowSettings(false)}
                className="p-2 rounded-lg transition-colors"
                style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-20)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                aria-label="Back"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
                </svg>
              </button>
              <div className="ml-2 text-13" style={{ color: 'var(--text-heading)' }}>Settings</div>
            </div>
            <Settings />
          </div>
        ) : showAdmin ? (
          <div className="w-full px-4 mt-2 mb-2">
            <div className="mb-2 flex items-center">
              <button
                onClick={() => setShowAdmin(false)}
                className="p-2 rounded-lg transition-colors"
                style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-20)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                aria-label="Back"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
                </svg>
              </button>
              <div className="ml-2 text-13" style={{ color: 'var(--text-heading)' }}>Admin access</div>
            </div>
            <AdminAccess />
          </div>
        ) : staticPage ? (
          <div className="w-full h-full flex flex-col overflow-hidden">
            <div className="px-4 mt-2 mb-2 flex items-center flex-shrink-0">
              <button
                onClick={() => setStaticPage(null)}
                className="p-2 rounded-lg transition-colors"
                style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-20)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                aria-label="Back"
              >
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
                </svg>
              </button>
            </div>
            <div className="flex-1 overflow-y-auto">
              {staticPage === 'privacy' ? <PrivacyNotice /> : <TermsOfUsage />}
            </div>
          </div>
        ) : (
        <>
        {/* Sources moved to the left grid column */}

        {/* Graph placeholder when in graph mode */}
        {viewMode === 'graph' ? (
          <div className="w-full h-full flex items-center justify-center" style={{ background: 'var(--bg-elevated)', border: '1px dashed var(--overlay-line-90)', borderRadius: '12px' }}>
            <div className="text-13" style={{ color: 'var(--text-secondary-strong)' }}>Graph will appear here</div>
          </div>
        ) : viewMode === 'unfold' ? (
          <div className="w-full px-4 flex-1 flex flex-col min-h-0 pb-2" style={{ position: 'relative' }}>
            {/* Document viewer overlay - replaces chat area when open */}
            {activeDoc && genaiChat.projectId && genaiChat.projectId !== 'local-unfold' && (
              <DocumentViewer
                projectId={genaiChat.projectId}
                doc={activeDoc}
                onClose={() => setActiveDoc(null)}
              />
            )}
            {/* Metric overlay - replaces chat area when open */}
            {(() => {
              const openKey = genaiChat.metricOverlayOpenKey || null;
              if (!openKey) return null;
              const cache = genaiChat.metricOverlayCache[openKey];
              const loading = !!genaiChat.metricOverlayLoading && !cache?.content;
              const content = cache?.content || '';
              return (
                <div className="absolute inset-0 z-30 rounded-xl" style={{ background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)', overflow: 'hidden' }}>
                  <div className="flex items-center justify-between px-3 py-2" style={{ borderBottom: '1px solid var(--overlay-line-90)' }}>
                    <div className="text-13" style={{ color: 'var(--text-heading)' }}>{cache?.metric?.metricName || (cache?.metric as any)?.name || cache?.metric?.id || 'Metric'}</div>
                    <button
                      type="button"
                      className="rounded-lg transition-colors"
                      aria-label="Close"
                      title="Close"
                      onClick={() => {
                        try {
                          genaiChat.closeMetricOverlay();
                        } catch (e) {
                          console.error('[MetricOverlay] Error closing overlay:', e);
                        }
                      }}
                      style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                      onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                      onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M6 6l12 12M18 6L6 18" strokeWidth={2} /></svg>
                    </button>
                  </div>
                  <div className="absolute inset-0 pt-[38px]">
                    <div className="w-full h-full overflow-y-auto px-4 py-3">
                      {loading ? (
                        <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>Loading metric analysis…</div>
                      ) : content ? (
                        <div className="markdown text-13" style={{ color: 'var(--text-heading)' }}>
                          <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]}>{content}</ReactMarkdown>
                        </div>
                      ) : (
                        <div className="text-12" style={{ color: 'var(--text-secondary)' }}>No content.</div>
                      )}
                      {/* TODO: создавать документ на базе метрики; создавать графики на базе анализа метрики */}
                    </div>
                  </div>
                </div>
              );
            })()}
            {/* GenAI chat history */}
            <div ref={messagesRef} className="flex-1 overflow-y-auto px-1 pb-2 min-h-[160px]" style={{ contentVisibility: 'auto', containIntrinsicSize: '800px 600px' }}>
              <div className="space-y-2">
                {(() => {
                  // Merge messages and interactions, sort by timestamp to ensure correct order
                  type MergedItem = { type: 'message' | 'interaction'; data: any; timestamp: number; };
                  const merged: MergedItem[] = [];

                  // Add filtered messages (only during streaming, and not already in interactions)
                  if (genaiChat.streaming) {
                    genaiChat.messages.forEach((m) => {
                      const hasInteraction = genaiChat.interactions.some((it) => 
                        (it as any).originalMessageId === m.id || it.id === m.id
                      );
                      if (!hasInteraction) {
                        // Parse timestamp from id (format: "timestamp-role")
                        const ts = parseInt(m.id.split('-')[0]) || 0;
                        merged.push({ type: 'message', data: m, timestamp: ts });
                      }
                    });
                  }

                  // Add interactions (user, assistant, and section markers)
                  genaiChat.interactions
                    .filter((it) => it.kind === 'user' || it.kind === 'assistant' || it.kind === 'section_start' || it.kind === 'section_end')
                    .forEach((it) => {
                      const ts = (it as any).createdAt?.seconds * 1000 || 0;
                      merged.push({ type: 'interaction', data: it, timestamp: ts });
                    });

                  // Sort by timestamp
                  merged.sort((a, b) => a.timestamp - b.timestamp);

                  return merged.map((item) => {
                    if (item.type === 'message') {
                      const m = item.data;
                      return (
                        (function() {
                  const isUser = m.role === 'user';
                  const isAssistantPlaceholder = !isUser && genaiChat.streaming && !m.content;
                  const isAssistantError = !isUser && !genaiChat.streaming && !m.content && !!genaiChat.error;
                  if (isAssistantPlaceholder) return null;
                  return (
                    <ChatMessage
                      key={m.id}
                      role={isUser ? 'user' : 'assistant'}
                      align={isUser ? 'right' : 'left'}
                      isThinking={isAssistantPlaceholder}
                      thinkingSeconds={genaiThinkingSeconds}
                      isError={isAssistantError}
                        headerText={!isUser ? (genaiChat.messageMeta?.[m.id]?.headerText || 'assistant') : undefined}
                        leftAccent={!isUser ? !!genaiChat.messageMeta?.[m.id]?.leftAccent : false}
                      footer={undefined}
                    >
                      {isAssistantPlaceholder ? (
                        <div className="space-y-1">
                          <div style={{ width: 160, height: 10, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                            <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                          </div>
                          <div style={{ width: 220, height: 10, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                            <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                          </div>
                          {(() => {
                            const isQuality = genaiSettings.strength === 'quality' || /_quality$/.test(genaiSettings.model || '');
                            if (!isQuality) return null;
                            const threshold = genaiSettings.groundingEnabled ? 35 : 20;
                            if ((genaiThinkingSeconds || 0) < threshold) return null;
                            const msg = genaiSettings.groundingEnabled
                              ? 'The complex‑reasoning model is updating its analysis with up‑to‑date information, which may take additional time.'
                              : 'This model is performing complex reasoning and may respond more slowly than usual.';
                            return (
                              <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                                {msg}
                              </div>
                            );
                          })()}
                        </div>
                      ) : (
                        <>
                          <div className="text-13 markdown" style={{ color: 'var(--text-heading)' }}>
                            <ReactMarkdown
                              remarkPlugins={[remarkGfm]}
                              components={{
                                p: (props: React.DetailedHTMLProps<React.HTMLAttributes<HTMLParagraphElement>, HTMLParagraphElement>) => (
                                  <div {...props} className="md-p" />
                                ),
                                a: (props: React.DetailedHTMLProps<React.AnchorHTMLAttributes<HTMLAnchorElement>, HTMLAnchorElement>) => (
                                  <a {...props} target="_blank" rel="noreferrer" className="underline" />
                                ),
                                code: (
                                  props: {
                                    inline?: boolean;
                                    className?: string;
                                    children?: React.ReactNode;
                                  } & React.DetailedHTMLProps<React.HTMLAttributes<HTMLElement>, HTMLElement>
                                ) => {
                                  const { inline, className, children, ...rest } = props;
                                  // Treat short single-line code blocks as inline chips
                                  const text = typeof children === 'string' ? children : Array.isArray(children) ? children.join('') : String(children ?? '');
                                  const isSingleLineBlock = !inline && !/\n/.test(text) && text.trim().length > 0 && text.trim().length <= 80;
                                  if (inline || isSingleLineBlock) {
                                    return (
                                      <code {...rest} style={{ backgroundColor: 'var(--white-10)', padding: '0 3px', borderRadius: 4 }}>
                                        {text}
                                      </code>
                                    );
                                  }
                                  return (
                                    <pre style={{ backgroundColor: 'var(--bg-elevated)', padding: 8, borderRadius: 8, overflowX: 'auto', border: '1px solid var(--overlay-line-90)' }}>
                                      <code {...rest} className={className}>{children}</code>
                                    </pre>
                                  );
                                },
                                ul: (props: React.DetailedHTMLProps<React.HTMLAttributes<HTMLUListElement>, HTMLUListElement>) => (
                                  <ul {...props} style={{ paddingLeft: '1.25rem' }} />
                                ),
                                ol: (props: React.DetailedHTMLProps<React.OlHTMLAttributes<HTMLOListElement>, HTMLOListElement>) => (
                                  <ol {...props} style={{ paddingLeft: '1.25rem' }} />
                                ),
                              } as Components}
                            >
                              {(() => {
                                try {
                                  // Prefer matching by originalMessageId to keep markdown and sanitizer consistent
                                  const inter = useGenaiChatStore.getState().interactions.find((it) => it.kind === 'assistant' && (it as any).originalMessageId === m.id);
                                  if (inter && typeof inter.contentSanitized === 'string' && inter.contentSanitized.length > 0) return inter.contentSanitized;
                                  return m.content;
                                } catch { return m.content; }
                              })()}
                            </ReactMarkdown>
                          </div>
                          {/* Action row: shows bias if available, else only buttons (assistant only) */}
                          {!isUser && (
                            <ActionRow messageId={m.id} isAssistant={!isUser} content={m.content} />
                          )}
                          {/* Show bias for user messages if available */}
                          {isUser && genaiChat.evaluations[m.id] && (
                            <EvaluationDetails messageId={m.id} />
                          )}
                          {/* Tags row: show searchQueries and sources as chips */}
                          {genaiChat.searchQueries.length > 0 || genaiChat.attributions.length > 0 ? (
                            <div className="mt-1 flex flex-wrap gap-1">
                              {genaiChat.searchQueries.slice(0, 4).map((q, i) => {
                                const words = q.trim().split(/\s+/);
                                const short = words.length > 3 ? words.slice(0, 3).join(' ') + '…' : q;
                                return (
                                  <span key={`q-${i}`} className="px-2 py-0.5 rounded-full text-[10px]" style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}>
                                    {short}
                                  </span>
                                );
                              })}
                              {genaiChat.attributions.slice(0, 4).map((a, i) => {
                                const url = (a.url || '').replace(/^https?:\/\//i, '');
                                const parts = url.split('/');
                                const host = parts[0] || '';
                                const tail = parts[1] ? `/${parts[1]}` : '';
                                const label = (host + tail) || (a.title || 'link');
                                return (
                                  <a key={`src-${i}`} href={a.url} target="_blank" rel="noreferrer" className="px-2 py-0.5 rounded-full text-[10px] underline" style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}>
                                    {label}
                                  </a>
                                );
                              })}
                            </div>
                          ) : null}
                        </>
                      )}
                    </ChatMessage>
                  );
                        })()
                      );
                    } else { // item.type === 'interaction'
                      const it = item.data;
                      // Render section markers
                      if (it.kind === 'section_start' || it.kind === 'section_end') {
                        const sid = (it.sectionId || it.id) as string;
                        if (it.kind === 'section_start') {
                          const collapsed = !!collapsedSections[sid];
                          return (
                            <div
                              key={it.id}
                              data-interaction-id={it.id}
                              className="my-2 sticky top-0 z-10"
                              style={{
                                background: 'var(--surface-700)',
                                border: '1px dashed var(--accent-research)',
                                borderRadius: 10,
                                padding: '6px 8px',
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'space-between'
                              }}
                            >
                              <div className="text-12 font-medium" style={{ color: 'var(--text-heading)' }}>
                                {it.content || 'Section'}
                              </div>
                              <button
                                type="button"
                                className="px-2 py-1 rounded text-11"
                                style={{ background: 'transparent', color: 'var(--text-secondary-strong)' }}
                                onClick={() => setCollapsedSections((s) => ({ ...s, [sid]: !collapsed }))}
                                aria-label={collapsed ? 'Expand section' : 'Collapse section'}
                                title={collapsed ? 'Expand section' : 'Collapse section'}
                              >
                                {collapsed ? 'Expand' : 'Collapse'}
                              </button>
                            </div>
                          );
                        }
                        // section_end divider
                        return (
                          <div key={it.id} className="my-2" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                            <div style={{ height: 1, background: 'var(--overlay-line-90)', flex: 1 }} />
                            <div className="text-[10px]" style={{ color: 'var(--accent-research)' }}>end of section</div>
                            <div style={{ height: 1, background: 'var(--overlay-line-90)', flex: 1 }} />
                          </div>
                        );
                      }
                      const isUser = it.kind === 'user';
                      // Skip rendering if inside a collapsed section
                      try {
                        const sid = (it as any).sectionId as string | undefined;
                        if (sid && collapsedSections[sid]) return null;
                      } catch {}
                      const meta = genaiChat.messageMeta[it.id];
                      const header = !isUser ? (meta?.headerText || it.author?.displayName || (it.agentProfile ? 'agent' : 'assistant')) : undefined;
                      const leftAccent = !isUser ? (meta?.leftAccent ?? !!it.agentProfile) : false;
                      
                      const handleDelete = async () => {
                const targetName = isUser ? 'user message' : 'assistant response';
                if (window.confirm(`Delete this ${targetName}?`)) {
                          try {
                            await genaiChat.deleteInteraction(it.id);
                          } catch (e) {
                            alert('Failed to delete message. Please try again.');
                            console.error('[Delete] Error:', e);
                          }
                        }
                      };
                      
                      const isPinned = !isUser && isInteractionPinned(it);
                      const handlePinToggle = async () => {
                        if (isUser) return; // Only for assistant messages
                        try {
                          await toggleInteractionPin(genaiChat.projectId, it.id, isPinned);
                          // Refresh interactions to update pin state
                          await genaiChat.hydrateInteractions();
                        } catch (e) {
                          alert('Failed to toggle pin. Please try again.');
                          console.error('[Pin] Error:', e);
                        }
                      };
                      
                      return (
                        <div key={it.id} className="group relative">
                          {/* Pin button - appears on hover for assistant messages only */}
                          {!isUser && (
                            <button
                              type="button"
                              aria-label={isPinned ? "Unpin message" : "Pin message"}
                              className="absolute top-2 right-10 p-1.5 rounded opacity-0 group-hover:opacity-100 transition-opacity z-10"
                              style={{ backgroundColor: 'transparent' }}
                              onClick={handlePinToggle}
                              onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                              onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                            >
                              {isPinned ? (
                                <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor" style={{ color: 'var(--accent-research)' }}>
                                  <path d="M5 5a2 2 0 012-2h10a2 2 0 012 2v16l-7-3.5L5 21V5z" />
                                </svg>
                              ) : (
                                <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-secondary-strong)' }}>
                                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 5a2 2 0 012-2h10a2 2 0 012 2v16l-7-3.5L5 21V5z" />
                                </svg>
                              )}
                            </button>
                          )}
                          {/* Delete button - appears on hover */}
                          <button
                            type="button"
                            aria-label="Delete message"
                            className="absolute top-2 right-2 p-1.5 rounded opacity-0 group-hover:opacity-100 transition-opacity z-10"
                            style={{ backgroundColor: 'transparent' }}
                            onClick={handleDelete}
                            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                          >
                            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-secondary-strong)' }}>
                              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                            </svg>
                          </button>
                        <ChatMessage role={isUser ? 'user' : 'assistant'} align={isUser ? 'right' : 'left'} isThinking={false} thinkingSeconds={0} isError={false} headerText={header} leftAccent={leftAccent} footer={undefined}>
                          {isUser ? (
                            <>
                              <div className="text-13" style={{ color: 'var(--text-heading)', whiteSpace: 'pre-wrap' }}>{it.content}</div>
                              {/* Lightweight action row for USER: only evaluation, no buttons */}
                              {(() => {
                                try {
                                  // Find eval.bias with bidirectional mapping support
                                  const map = (genaiChat as any).persistedByLocalId || {};
                                  const reverseMap: Record<string, string[]> = {};
                                  Object.entries(map).forEach(([local, server]) => {
                                    if (!reverseMap[server as string]) reverseMap[server as string] = [];
                                    reverseMap[server as string].push(local);
                                  });
                                  const localIds = reverseMap[it.id] || [];
                                  const evalNode = [...genaiChat.interactions].reverse().find((n) => 
                                    n.subkind === 'eval.bias' && 
                                    (n.replyTo === it.id || localIds.includes(n.replyTo || ''))
                                  );
                                  if (!evalNode) return null;
                                  return (
                                    <div className="mt-1">
                                      <EvaluationDetails messageId={it.id} />
              </div>
                                  );
                                } catch { return null; }
                              })()}
                            </>
                          ) : (
                            <div className="text-13 markdown" data-interaction-id={it.id} style={{ color: 'var(--text-heading)', position: 'relative', opacity: frozenEdits[it.id] ? 0.6 : 1, pointerEvents: frozenEdits[it.id] ? 'none' as any : 'auto' }}>
                              <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]} components={{
                                p: (props: React.DetailedHTMLProps<React.HTMLAttributes<HTMLParagraphElement>, HTMLParagraphElement>) => (<div {...props} className="md-p" />),
                                a: (props: React.DetailedHTMLProps<React.AnchorHTMLAttributes<HTMLAnchorElement>, HTMLAnchorElement>) => (<a {...props} target="_blank" rel="noreferrer" className="underline" />),
                                code: (props: { inline?: boolean; className?: string; children?: React.ReactNode } & React.DetailedHTMLProps<React.HTMLAttributes<HTMLElement>, HTMLElement>) => {
                                  const { inline, className, children, ...rest } = props;
                                  const text = typeof children === 'string' ? children : Array.isArray(children) ? children.join('') : String(children ?? '');
                                  const isSingleLineBlock = !inline && !/\n/.test(text) && text.trim().length > 0 && text.trim().length <= 80;
                                  if (inline || isSingleLineBlock) {
                                    return (<code {...rest} style={{ backgroundColor: 'var(--white-10)', padding: '0 3px', borderRadius: 4 }}>{text}</code>);
                                  }
                                  return (
                                    <pre style={{ backgroundColor: 'var(--bg-elevated)', padding: 8, borderRadius: 8, overflowX: 'auto', border: '1px solid var(--overlay-line-90)' }}>
                                      <code {...rest} className={className}>{children}</code>
                                    </pre>
                                  );
                                },
                                ul: (props: React.DetailedHTMLProps<React.HTMLAttributes<HTMLUListElement>, HTMLUListElement>) => (<ul {...props} style={{ paddingLeft: '1.25rem' }} />),
                                ol: (props: React.DetailedHTMLProps<React.OlHTMLAttributes<HTMLOListElement>, HTMLOListElement>) => (<ol {...props} style={{ paddingLeft: '1.25rem' }} />),
                              } as Components}>
                                {(() => {
                                  const base = (it.contentSanitized || it.content) as string;
                                  const hl = editHighlight[it.id];
                                  if (hl && typeof hl.start === 'number' && typeof hl.end === 'number' && hl.start >= 0 && hl.end > hl.start && hl.end <= base.length) {
                                    const pre = base.slice(0, hl.start);
                                    const mid = base.slice(hl.start, hl.end);
                                    const post = base.slice(hl.end);
                                    const span = `<span class="etg-edit-hl" style="background: rgba(255, 230, 150, 0.6); padding: 0 2px; border-radius: 3px;">${mid.replace(/</g,'&lt;').replace(/>/g,'&gt;')}</span>`;
                                    return pre + span + post;
                                  }
                                  return base;
                                })()}
                              </ReactMarkdown>
                              {/* Top-right status chip */}
                              {(frozenEdits[it.id] || editErrors[it.id]) && (
                                <div className="absolute" style={{ top: 4, right: 4 }}>
                                  {frozenEdits[it.id] && (
                                    <span className="px-2 py-0.5 rounded text-[10px]" style={{ background: 'var(--white-10)', border: '1px solid var(--overlay-line-90)', color: 'var(--text-secondary-strong)' }}>
                                      <span className="inline-block mr-1" style={{ width: 8, height: 8, borderRadius: 9999, background: 'var(--text-secondary-strong)', opacity: 0.6, animation: 'pulse 1.2s infinite' }} />
                                      Updating…
                                    </span>
                                  )}
                                  {editErrors[it.id] && !frozenEdits[it.id] && (
                                    <span className="px-2 py-0.5 rounded text-[10px]" style={{ background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)', color: 'var(--accent-danger)' }}>
                                      Failed
                                      <button className="ml-2 underline" style={{ color: 'var(--text-secondary-strong)' }} onClick={() => {
                                        const last = editLast[it.id];
                                        if (last && last.request) {
                                          // re-run with saved request
                                          (async () => {
                                            const req = last.request;
                                            const selText = last.selected;
                                            // reuse local submit logic by triggering palette-less path
                                            const markdownRoot = document.querySelector(`[data-interaction-id="${it.id}"]`) as HTMLElement | null;
                                            if (!markdownRoot) return;
                                            const inter = useGenaiChatStore.getState().interactions.find((x) => x.id === it.id);
                                            const full = (inter?.contentSanitized || inter?.content || '').toString();
                                            const meta = useGenaiChatStore.getState().messageMeta[it.id];
                                            const agentName = meta?.headerText || 'agent';
                                            const prof = meta?.agentProfile;
                                            const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
                                            const settings = useGenAISettings.getState();
                                            const useModel = prof?.model || settings.model;
                                            const sxSystemInstruction = `Hidden-instructions protocol (do not show to users):\n- When you need to output service instructions (entities, metrics, watchlist, relations), emit ONLY HTML comments <!-- @sx:{JSON} -->.`;
                                            const options: any = {
                                              profile: /_quality$/i.test(useModel || '') ? 'quality' : 'fast',
                                              modelOverride: useModel,
                                              groundingEnabled: settings.groundingEnabled,
                                              temperature: settings.temperature,
                                              maxOutputTokens: Math.max(512, settings.maxOutputTokens || 1024),
                                              systemInstruction: [settings.systemInstruction || '', sxSystemInstruction].filter(Boolean).join('\n\n').trim() || undefined,
                                            };
                                            const prompt = `User requirement: you should create your answer in ${preferredLang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nYou are ${agentName}. We provide the full assistant answer (FULL_ANSWER) and a specific fragment (SELECTED_FRAGMENT) the user wants to modify. The user request is (USER_REQUEST).\n\nReturn ONLY the updated fragment. Preserve the fragment's existing formatting and structure where possible, and use Markdown appropriately (headings ###, lists, emphasis, blank lines) if it helps readability and does not contradict the request. Do not add any preface, labels, or explanations. Do not wrap the result in code fences. Do NOT return JSON.\n\nFULL_ANSWER:\n${full}\n\nSELECTED_FRAGMENT:\n${selText}\n\nUSER_REQUEST:\n${req}\n`;
                                            setFrozenEdits((s) => ({ ...s, [it.id]: true }));
                                            try {
                                              const res = await api.genaiGenerate({ model: useModel, prompt, strictJSONEnabled: false, maxOutputTokens: options.maxOutputTokens, webSearchEnabled: false, systemInstruction: options.systemInstruction });
                                              const rawOut = (res?.content || '').toString();
                                              const stripFences = (s: string) => { let t = s.trim(); t = t.replace(/^```[a-zA-Z]*\n([\s\S]*?)\n```$/m, '$1').trim(); t = t.replace(/^```[\s\S]*?```$/m, (m) => m.replace(/^```[a-zA-Z]*\n?/,'').replace(/\n?```$/,'')).trim(); if ((t.startsWith('"') && t.endsWith('"')) || (t.startsWith('\'') && t.endsWith('\''))) { t = t.slice(1, -1); } return t.trim(); };
                                              let cleaned = stripFences(rawOut);
                                              try { const obj = JSON.parse(cleaned); if (obj && typeof obj === 'object' && !Array.isArray(obj)) { const k = Object.keys(obj)[0]; if (k) cleaned = String((obj as any)[k] ?? '').trim(); } } catch {}
                                              const updated = stripFences(cleaned);
                                              if (!updated) throw new Error('Empty model response');
                                              // Use markdown-preserving path
                                              const md = (inter?.contentSanitized || inter?.content || '').toString();
                                              const normalized: string[] = []; const mapIdx: number[] = []; const len = md.length; let i = 0; let atLineStart = true; const pushChar = (ch: string, origIdx: number) => { normalized.push(ch); mapIdx.push(origIdx); atLineStart = (ch === '\n'); };
                                              while (i < len) { const ch = md[i]; const nxt = md[i+1]; if (atLineStart) { if (ch === '#') { while (i < len && md[i] === '#') i++; while (i < len && md[i] === ' ') i++; continue; } if (ch === '>') { i++; if (md[i] === ' ') i++; continue; } if (ch === '-' && (md[i+1] === ' ')) { i += 2; continue; } }
                                                if ((ch === '*' && nxt === '*') || (ch === '_' && nxt === '_')) { i += 2; continue; }
                                                if (ch === '*' || ch === '_' || ch === '~') { i++; continue; }
                                                if (ch === '`') { i++; while (i < len && md[i] !== '`') { pushChar(md[i], i); i++; } if (i < len && md[i] === '`') i++; continue; }
                                                if (ch === '[') { i++; while (i < len && md[i] !== ']') { pushChar(md[i], i); i++; } if (i < len && md[i] === ']') i++; if (i < len && md[i] === '(') { while (i < len && md[i] !== ')') i++; if (i < len && md[i] === ')') i++; } continue; }
                                                if (ch === '!' && md[i+1] === '[') { i += 2; while (i < len && md[i] !== ']') i++; if (i < len && md[i] === ']') i++; if (i < len && md[i] === '(') { while (i < len && md[i] !== ')') i++; if (i < len && md[i] === ')') i++; } continue; }
                                                if (ch === '\n' || ch === '\r') { pushChar('\n', i); i++; continue; }
                                                pushChar(ch, i); i++; }
                                              const normStr = normalized.join('').replace(/[\u00A0\u202F]/g, ' ').replace(/\r/g,'').replace(/\s+/g, ' ');
                                              const selNorm = selText.replace(/[\u00A0\u202F]/g, ' ').replace(/\r/g,'').replace(/\s+/g, ' ').trim();
                                              const pos = normStr.indexOf(selNorm);
                                              let newContentSanitized = '';
                                              if (pos >= 0) { const startOrig = mapIdx[pos]; const endMapIdx = mapIdx[pos + selNorm.length - 1]; const endOrig = (typeof endMapIdx === 'number') ? (endMapIdx + 1) : startOrig + selText.length; newContentSanitized = md.slice(0, startOrig) + updated + md.slice(endOrig); }
                                              else { newContentSanitized = (inter?.contentSanitized || inter?.content || '').toString().replace(selText, updated); }
                                              useGenaiChatStore.setState((s) => ({ interactions: s.interactions.map((x) => x.id === it.id ? ({ ...x, contentSanitized: newContentSanitized }) : x) } as any));
                                              setEditErrors((s) => ({ ...s, [it.id]: null }));
                                            } catch (err: any) {
                                              setEditErrors((s) => ({ ...s, [it.id]: err?.message || 'Edit failed' }));
                                            } finally {
                                              setFrozenEdits((s) => ({ ...s, [it.id]: false }));
                                            }
                                          })();
                                        }
                                      }}>Retry</button>
                                      <button className="ml-1 underline" style={{ color: 'var(--text-secondary-strong)' }} onClick={() => setEditErrors((s) => ({ ...s, [it.id]: null }))}>Cancel</button>
                                    </span>
                                  )}
            </div>
                              )}
                              {frozenEdits[it.id] && (
                                <div className="absolute inset-0" style={{ pointerEvents: 'none' }}>
                                  <div className="absolute inset-0" style={{ background: 'linear-gradient(90deg, transparent, var(--white-06), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                                </div>
                              )}
                              {editErrors[it.id] && (
                                <div className="mt-1 px-2 py-1 rounded" style={{ border: '1px solid var(--overlay-line-90)', background: 'var(--bg-elevated)' }}>
                                  <div className="text-[11px]" style={{ color: 'var(--accent-danger)' }}>Edit failed: {editErrors[it.id]}</div>
                                </div>
                              )}
                            </div>
                          )}
                          {!isUser && (
                            <ActionRow messageId={it.id} isAssistant={!isUser} content={(it.contentSanitized || it.content)} />
                          )}
                        </ChatMessage>
                        </div>
                      );
                    }
                  });
                })()}
                {genaiChat.streaming && (() => {
                  try {
                    const m = genaiChat.messages[genaiChat.messages.length - 1];
                    if (!m || m.role !== 'assistant' || m.content) return null;
                    const header = (genaiChat.messageMeta?.[m.id]?.headerText || 'assistant');
                    const leftAccent = !!genaiChat.messageMeta?.[m.id]?.leftAccent;
                    const isQuality = genaiSettings.strength === 'quality' || /_quality$/.test(genaiSettings.model || '');
                    const threshold = genaiSettings.groundingEnabled ? 35 : 20;
                    const showInfo = (genaiThinkingSeconds || 0) >= threshold && isQuality;
                    const infoMsg = genaiSettings.groundingEnabled
                      ? 'The complex‑reasoning model is updating its analysis with up‑to‑date information, which may take additional time.'
                      : 'This model is performing complex reasoning and may respond more slowly than usual.';
                    return (
                      <ChatMessage
                        key={m.id}
                        role="assistant"
                        align="left"
                        isThinking={true}
                        thinkingSeconds={genaiThinkingSeconds}
                        isError={false}
                        headerText={header}
                        leftAccent={leftAccent}
                        footer={undefined}
                      >
                        <div className="space-y-1">
                          <div style={{ width: 160, height: 10, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                            <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                          </div>
                          <div style={{ width: 220, height: 10, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                            <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                          </div>
                          {showInfo && (
                            <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                              {infoMsg}
                            </div>
                          )}
                        </div>
                      </ChatMessage>
                    );
                  } catch { return null; }
                })()}
              </div>
            </div>
            {/* Edit status indicator */}
            {Object.values(frozenEdits).some(Boolean) && (
              <div className="px-1 pb-2">
                <div className="text-xs flex items-center gap-2" style={{ textAlign: 'left', color: 'var(--text-secondary-strong)' }}>
                  <span className="animate-pulse" style={{ width: '8px', height: '8px', borderRadius: '9999px', backgroundColor: 'var(--text-secondary-strong)' }} />
                  <span>Applying edit… {editSeconds}s</span>
                </div>
              </div>
            )}
            {/* Floating selection action button is mounted imperatively in effect to avoid rerendering content and losing selection */}
            {/* Tool calls / results */}
            {(genaiChat.toolCalls.length > 0 || genaiChat.toolResults.length > 0) && (
              <div className="px-1 pb-2">
                {genaiChat.toolCalls.length > 0 && (
                  <div className="text-[11px] mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                    Tool calls:
                    <ul className="list-disc ml-4">
                      {genaiChat.toolCalls.map((c, i) => (
                        <li key={`tc-${i}`}>{c.name}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {genaiChat.toolResults.length > 0 && (
                  <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                    Tool results:
                    <ul className="list-disc ml-4">
                      {genaiChat.toolResults.map((c, i) => (
                        <li key={`tr-${i}`}><span className="font-mono">{c.name}</span></li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            )}
            {/* Thinking indicator moved inside assistant bubble during streaming */}
            {/* Usage and attributions after completion */}
            {!genaiChat.streaming && (genaiChat.usage || genaiChat.attributions.length > 0) && (
              <div className="px-1 pb-2 space-y-1">
                {genaiChat.usage && (
                  <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                    Tokens: prompt {genaiChat.usage.promptTokens}, output {genaiChat.usage.outputTokens}, total {genaiChat.usage.totalTokens}
                  </div>
                )}
                {genaiChat.attributions.length > 0 && (
                  <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
                    Sources:
                    <ul className="list-disc ml-4">
                      {genaiChat.attributions.map((a, i) => (
                        <li key={`att-${i}`}><a href={a.url} target="_blank" rel="noreferrer" className="underline">{a.title || a.url}</a></li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            )}
            {/* High-risk banner (if any) */}
            {(() => {
              const isHigh = (riskBanner?.visible && riskBanner?.level === 'High');
              if (!isHigh) return null;
              return (
                <div className="rounded-lg border p-2 mb-2" style={{ borderColor: 'rgba(220,20,60,0.5)', background: 'rgba(255,0,0,0.06)', color: 'var(--text-heading)', display: 'flex', alignItems: 'flex-start', gap: 8 }}>
                  <div style={{ flex: 1 }}>
                    <div className="text-12" style={{ color: '#cc3344', fontWeight: 600 }}>High-risk conversation</div>
                    {riskBanner?.rationale && (
                      <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>{riskBanner.rationale}</div>
                    )}
                  </div>
                  <button
                    aria-label="Dismiss high-risk banner"
                    title="Dismiss"
                    className="rounded"
                    onClick={() => setRiskBanner((prev) => (prev ? { ...prev, visible: false } : null))}
                    style={{ background: 'transparent', color: 'var(--text-secondary-strong)' }}
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24"><path d="M6 6l12 12M18 6L6 18" stroke="currentColor" strokeWidth="2"/></svg>
                  </button>
                </div>
              );
            })()}
            {/* Analysis status banner above input */}
            {(() => {
              const s: any = (useDiscoveryStore as any).getState?.().lastAnalysisStatus;
              if (!s) return null;
              const color = s.status === 'started' ? 'var(--text-secondary-strong)' : (s.status === 'done' ? 'var(--green-50)' : 'var(--error-50)');
              const text = `${s.kind}: ${s.status}` + (s.count != null ? ` (${s.count})` : '') + (s.elapsedMs != null && s.status !== 'started' ? ` · ${s.elapsedMs}ms` : '');
              return (
                <div className="mb-2 px-2 py-1 rounded" style={{ border: '1px solid var(--overlay-line-90)', color, background: 'var(--bg-elevated)' }}>
                  <span className="text-xs">{text}</span>
                </div>
              );
            })()}
            {/* Input */}
            <UnfoldRequest
              dangerTint={isHighRisk}
              showHighRiskTag={isHighRisk}
              onHighRiskTagClick={() => setRiskBanner((prev) => ({ visible: true, level: 'High', rationale: prev?.rationale || '' }))}
              onSubmit={async (text) => {
                // Text comes directly from UnfoldRequest, already trimmed and validated
                if (genaiChat.streaming || !text.trim()) return;
                // Slash commands: /section <title> and /end
                const raw = text.trim();
                const isSection = /^\s*\/section\b/i.test(raw);
                const isEnd = /^\s*\/end\b/i.test(raw);
                const isAnalyzeEntities = /^\s*\/analyze\s+entities\b/i.test(raw) || /^\s*\/analyze\s+entities\s*$/i.test(raw);
                if (isSection || isEnd) {
                  try {
                    const pid = await genaiChat.ensureProject();
                    if (isSection) {
                      const title = raw.replace(/^\s*\/section\b\s*/i, '').trim();
                      await api.createProjectInteraction(pid, { kind: 'section_start', content: title || '' });
                    } else {
                      await api.createProjectInteraction(pid, { kind: 'section_end', content: '' });
                    }
                    // UnfoldRequest clears itself after submit
                    await genaiChat.hydrateInteractions();
                  } catch (e) {
                    console.error('[SlashCommand] failed', e);
                  }
                  return;
                }
                if (isAnalyzeEntities) {
                  try {
                    setEntitiesAnalyzing(true); // show status immediately
                    const pid = await genaiChat.ensureProject();
                  const prompt = raw.replace(/^\s*\/analyze\s+entities\b\s*/i, '') + "\n\nConstraints:\n- Only include concrete, named entities (no abstract categories).\n- Allowed types: person | group_of_persons | organization_or_platform | project_or_product | classifier_or_standard | classification_code | regulatory_document | government_agency | contract_document | location | other.\n- Reject generic words like 'technical regulation', 'open databases', 'contracts' without identifiers.";
                    await api.analyzeEntities(pid, prompt);
                    // UnfoldRequest clears itself after submit; WS will flip analyzing=false on parsed/failed
                  } catch (e) {
                    console.error('[AnalyzeEntities] failed', e);
                    setEntitiesAnalyzing(false);
                  }
                  return;
                }
                const tools = genaiSettings.enableTelegramTool ? [TelegramFetchUpdatesTool] : undefined;
                // Map UI model to backend aliases
                const model = genaiSettings.model;
                const sxSystemInstruction = (
                  `Hidden-instructions protocol (do not show to users):\n` +
                  `- When you need to output service instructions (entities, metrics to monitor, watchlist, relations), emit ONLY HTML comments of the form <!-- @sx:{JSON} -->.\n` +
                  `- JSON must be valid and match v: "1" with one of types: entity_mark, monitor_metric, watchlist_add, link_reference.\n` +
                  `- Do not duplicate @sx content in visible markdown. Keep normal answer readable.\n` +
                  `- Examples: <!-- @sx:{"v":"1","type":"entity_mark","id":"ent-001","kind":"person","name":"…","description":"…"} -->`);
                const combinedSystemInstruction = [
                  genaiSettings.systemInstruction || '',
                  sxSystemInstruction,
                ].filter(Boolean).join('\n\n').trim() || undefined;

                const options: any = {
                  // profile is used only for gemini fallback; keep for compatibility
                  profile: model === 'gemini_quality' ? 'quality' : 'fast',
                  modelOverride: model,
                  groundingEnabled: genaiSettings.groundingEnabled,
                  temperature: genaiSettings.temperature,
                  maxOutputTokens: genaiSettings.maxOutputTokens,
                  systemInstruction: combinedSystemInstruction,
                };
              const prompt = text;
              // Capture previous assistant reply (if any) BEFORE starting stream
              const stBefore = useGenaiChatStore.getState();
              let prevAssistantText: string | null = null;
              try {
                const aiInter = stBefore.interactions.filter((it) => it.kind === 'assistant');
                if (aiInter.length > 0) {
                  const last = aiInter[aiInter.length - 1];
                  prevAssistantText = (last.summary && last.summary.trim()) ? last.summary : (last.content || null);
                } else {
                  const priorMsgs = stBefore.messages.filter((m) => m.role === 'assistant' && m.content && m.content.trim());
                  if (priorMsgs.length > 0) prevAssistantText = priorMsgs[priorMsgs.length - 1].content;
                }
              } catch {}
                // UnfoldRequest clears itself after submit
                const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
                await genaiChat.startStream({ prompt, tools, options: { ...options, __lang: preferredLang } });
              // Fire-and-forget: structured bias evaluation for the USER message we just sent
              try {
                const st = useGenaiChatStore.getState();
                const userMsgs = st.messages.filter((mm) => mm.role === 'user');
                const lastUser = userMsgs.length > 0 ? userMsgs[userMsgs.length - 1] : null;
                const textToEvaluate = (prompt || '').trim();
                if (lastUser && textToEvaluate) {
                  (async () => {
                    try {
                      const settingsStateEval = useGenAISettings.getState();
                      const evalModel = (settingsStateEval.stanceModel && settingsStateEval.stanceModel.trim()) ? settingsStateEval.stanceModel : 'gemini_fast';
                      const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN');
                      const biasDict = [
                        'confirmation_bias',
                        'selection_bias',
                        'framing_bias',
                        'recency_bias',
                        'availability_bias',
                        'cultural_bias',
                        'geo_linguistic_bias',
                        'authority_bias',
                        'safety_over_caution_skew',
                        'overclaiming',
                        'overgeneralization',
                        'loaded_language'
                      ];
                      const schema = {
                        type: 'object',
                        additionalProperties: false,
                        properties: {
                          biasLevel: { type: 'number', minimum: 0, maximum: 1 },
                          thoughtfulness: { type: 'number', minimum: 0, maximum: 1 },
                          biases: { type: 'array', items: { type: 'string' }, minItems: 3, maxItems: 5 },
                          biasTypes: { type: 'array', items: { type: 'string', enum: biasDict }, minItems: 0 }
                        },
                        required: ['biasLevel', 'thoughtfulness', 'biases', 'biasTypes']
                      } as any;
                      const schemaText = JSON.stringify(schema, null, 2);
                      const dictText = JSON.stringify(biasDict);
                      const ctxBlock = prevAssistantText && prevAssistantText.trim().length > 0
                        ? `ASSISTANT_REPLY (context, do NOT evaluate this text):\n${prevAssistantText}\n\n`
                        : '';
                      const promptEval = `In this request, you act as an external expert specializing in discussion evaluation, and you must return your assessment strictly following the provided schema in JSON format. Return ONLY JSON (no prose, no markdown, no code fences).\n\nUser requirement: you should create your answer in ${preferredLang} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\nField names must be exactly: biasLevel, thoughtfulness, biases, biasTypes.\n\nBias scale (biasLevel 0..1):\n- 0.0–0.1: neutral; balanced alternatives; neutral language.\n- 0.2–0.4: mild tilt; some alternatives missing; occasional preference.\n- 0.5–0.7: pronounced tilt; one-sided argument; emotive language appears.\n- 0.8–1.0: strong advocacy; ignores counterfacts; categorical claims.\n\nThoughtfulness (0..1):\n- Low: shallow, binary conclusions, no uncertainty handling, no citations.\n- Medium: some analysis, limited trade-offs.\n- High: nuanced, considers uncertainty, alternatives and trade-offs, sober tone.\n\nBiases array (3–5 items): short natural-language notes explaining concrete bias manifestations in PreferredLanguage (not field names).\n\nBiasTypes array: choose zero or more labels STRICTLY from DICTIONARY (English tokens only):\n${dictText}\n\nThe JSON MUST conform EXACTLY to this schema (no additional keys, types must match):\n${schemaText}\n\n${ctxBlock}USER_MESSAGE (to evaluate):\n${textToEvaluate}\n\nEvaluate ONLY the USER_MESSAGE both standalone and in relation to ASSISTANT_REPLY (if provided). Do NOT assess the assistant's content. Return only the JSON.`;
                      const res = await api.genaiGenerate({
                        model: evalModel,
                        prompt: promptEval,
                        strictJSONEnabled: true,
                        jsonSchema: schema,
                        maxOutputTokens: 4096,
                        webSearchEnabled: false,
                      });
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
                      } catch {}
                      if (!parsed || typeof parsed !== 'object') return;
                      const clamp01 = (n: any) => Math.max(0, Math.min(1, Number(n) || 0));
                      const ev = {
                        biasLevel: clamp01(parsed?.biasLevel),
                        thoughtfulness: clamp01(parsed?.thoughtfulness),
                        biases: Array.isArray(parsed?.biases) ? parsed.biases.filter((x: any) => typeof x === 'string').slice(0, 5) : [],
                        biasTypes: Array.isArray(parsed?.biasTypes) ? parsed.biasTypes.filter((x: any) => typeof x === 'string').slice(0, 12) : [],
                      };
                      useGenaiChatStore.getState().setEvaluation(lastUser.id, ev);
                      // Create eval.bias for user
                      try {
                        const evalId = Date.now().toString() + '-eval-bias-u';
                        useGenaiChatStore.setState((s) => ({
                          interactions: [
                            ...s.interactions,
                            {
                              id: evalId,
                              projectId: s.projectId,
                              kind: 'system',
                              subkind: 'eval.bias',
                              content: JSON.stringify(ev),
                              contentSanitized: JSON.stringify(ev),
                              replyTo: lastUser.id,
                              relations: [{ type: 'evaluates', targetId: lastUser.id }],
                              createdAt: new Date().toISOString(),
                            } as any,
                          ],
                        } as any));
                      } catch {}
                    } catch (e) {
                      console.warn('[UserStanceEval] evaluation failed', e);
                    }
                  })();
                }
              } catch {}
              }}
              disabled={genaiChat.streaming || !queryText.trim()}
            />
            {/* Resizer handle at right edge of center column */}
            <div
              className="chat-resizer"
              style={{ right: -4 }}
              onMouseDown={(e) => {
                const root = document.documentElement;
                const startX = e.clientX;
                const startWidth = (messagesRef.current?.parentElement?.clientWidth || 0);
                const initial = getComputedStyle(root).getPropertyValue('--center-col-width');
                const onMove = (ev: MouseEvent) => {
                  const delta = ev.clientX - startX;
                  const newWidth = Math.max(480, startWidth + delta);
                  root.style.setProperty('--center-col-width', `${newWidth}px`);
                };
                const onUp = () => {
                  window.removeEventListener('mousemove', onMove);
                  window.removeEventListener('mouseup', onUp);
                };
                window.addEventListener('mousemove', onMove);
                window.addEventListener('mouseup', onUp);
              }}
            />
          </div>
        ) : (
        /* Interactions + input container with internal scroll */
        <div className="w-full px-4 flex-1 flex flex-col min-h-0 pb-2">
          {/* Interactions history (LLM answers etc.) */}
          <div ref={messagesRef} className="flex-1 overflow-y-auto px-1 pb-2 min-h-[160px]" style={{ contentVisibility: 'auto', containIntrinsicSize: '800px 600px' }}>
            <div className="space-y-2">
              {interactions
                .filter((it) => it.kind === 'discovery.query' || it.kind === 'discovery.answer')
                .map((it) => {
                  const isUser = it.kind === 'discovery.query';
                  const containerStyle: React.CSSProperties = {
                    backgroundColor: isUser ? 'var(--brand-10)' : 'transparent',
                    border: isUser ? '1px solid var(--brand-25)' : 'none',
                    maxWidth: '75%',
                  };
                  return (
                    <div key={it.id} className={`rounded-2xl px-3 py-2 ${isUser ? 'ml-auto' : ''}`} style={containerStyle}>
                      <div className="text-[10px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>{isUser ? 'you' : 'assistant'}</div>
                      <div className="text-xs" style={{ color: 'var(--text-heading)', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
                        {it.content}
                      </div>
                    </div>
                  );
              })
            }
            </div>
          </div>

          {/* Status indicator */}
          {(running || entitiesAnalyzing) && (
            <div className="px-1 pb-2">
              <div className="text-xs flex items-center gap-2" style={{ textAlign: 'left', color: 'var(--text-secondary-strong)' }}>
                <span className="animate-pulse" style={{ width: '8px', height: '8px', borderRadius: '9999px', backgroundColor: 'var(--text-secondary-strong)' }} />
                {running ? (
                  <span>Thinking... {thinkingSeconds}s</span>
                ) : (
                  <span>Analyzing entities…</span>
                )}
              </div>
            </div>
          )}
          {/* TODO(metrics): aggregate stance/bias metrics per participant and per thread once backend summary API is available */}

          {/* Input box */}
          <DiscoveryRequest
            value={queryText}
            onChange={setQueryText}
            onSubmit={async () => {
              if (running || !queryText.trim()) return;
              if (currentProject?.id) {
                const preferred = (localStorage.getItem('preferredLanguage') || 'EN').toLowerCase();
                const lang = preferred && preferred.length === 2 ? preferred : 'en';
                setRunning(true);
                try {
                  await sendChatMessage(currentProject.id, queryText, lang);
                  setQueryText('');
                } finally {
                  setRunning(false);
                }
              } else {
                handleExecuteDiscovery();
              }
            }}
            disabled={running || !queryText.trim()}
          />
        </div>
        )}
        {previewSource && (
          <SourcePreview
            source={previewSource}
            onClose={() => setPreviewSource(null)}
            style={{ top: 0, left: '12px', width: 760, maxWidth: 760 }}
          />
        )}

        {/* Add Telegram modal */}
        {showAddTelegramModal && (
          <div
            role="dialog"
            aria-modal="true"
            aria-label="Add Telegram Channel"
            className="fixed inset-0 flex items-center justify-center"
            style={{ zIndex: 50 }}
          >
            <div
              className="absolute inset-0"
              style={{ backgroundColor: 'rgba(0,0,0,0.5)' }}
              onClick={() => !tgSubmitting && setShowAddTelegramModal(false)}
            />
            <div
              className="relative rounded-xl shadow-xl w-full max-w-sm"
              style={{ backgroundColor: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)' }}
            >
              <div className="px-4 py-3" style={{ borderBottom: '1px solid var(--overlay-line-90)' }}>
                <div className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>Add Telegram Channel</div>
              </div>
              <div className="p-4 space-y-3">
                <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                  Enter @username or a link like t.me/username
                </div>
                <div className="flex items-center gap-2">
                  <div
                    className="flex items-center justify-center rounded-lg"
                    style={{ width: 32, height: 32, background: 'linear-gradient(135deg, #2AABEE 0%, #229ED9 100%)', color: 'white' }}
                  >
                    <svg viewBox="0 0 24 24" width={16} height={16} fill="none" stroke="currentColor" strokeWidth={2}>
                      <path d="M3 11l18-7-7 18-4-8-7-3z" stroke="currentColor" />
                    </svg>
                  </div>
                  <input
                    type="text"
                    value={tgInput}
                    onChange={(e) => setTgInput(e.target.value)}
                    placeholder="@channel or t.me/channel"
                    className="flex-1 bg-transparent outline-none text-sm"
                    style={{ color: 'var(--text-heading)' }}
                    onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addTelegramSource(); } }}
                    autoFocus
                  />
                </div>
                {tgError && (
                  <div className="text-xs" style={{ color: 'var(--accent-danger)' }}>{tgError}</div>
                )}
              </div>
              <div className="px-4 py-3 flex items-center justify-end gap-2" style={{ borderTop: '1px solid var(--overlay-line-90)' }}>
                <button
                  type="button"
                  className="px-3 py-1.5 rounded-lg text-xs transition-colors"
                  onClick={() => setShowAddTelegramModal(false)}
                  disabled={tgSubmitting}
                  style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                  onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                  onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="px-3 py-1.5 rounded-lg text-xs"
                  onClick={addTelegramSource}
                  disabled={tgSubmitting}
                  style={{ backgroundColor: '#2AABEE', color: 'white', opacity: tgSubmitting ? 0.7 : 1 }}
                >
                  {tgSubmitting ? 'Adding…' : 'Add' }
                </button>
              </div>
            </div>
          </div>
        )}
        </>
        )}
        </div>
        {/* Right column: container for contextual panels (transparent, no border) */}
        {viewMode !== 'graph' && (
          <div className="min-h-0 h-full" style={{ background: 'transparent', display: 'flex', flexDirection: 'column', marginRight: 48 }}>
            {(rightPanelOpen || rightContextOpen || rightArtifactsOpen) && (
              <div
                className="mt-3 p-3 rounded-xl sidebar-panel"
                style={{
                  backgroundColor: 'var(--bg-sidebar)',
                  border: '1px solid var(--overlay-line-90)',
                  display: 'flex',
                  flexDirection: 'column',
                  flex: 1,
                  minHeight: 0,
                  overflowY: 'auto',
                }}
              >
                <div className="flex items-center justify-between" style={{ position: 'sticky', top: 0, backgroundColor: 'var(--bg-sidebar)', paddingBottom: 8, marginBottom: 8, zIndex: 1 }}>
                  <h3 className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>
                    {rightArtifactsOpen ? 'Workspace Artifacts' : rightContextOpen ? 'Context Navigation' : 'Documents & Sources'}
                  </h3>
                  <button
                    type="button"
                    aria-label="Close right panel"
                    title="Close"
                    className="p-2 rounded-lg transition-colors"
                    style={{ backgroundColor: 'transparent', color: 'var(--text-secondary-strong)' }}
                    onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                    onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                    onClick={() => { setRightPanelOpen(false); setRightContextOpen(false); setRightArtifactsOpen(false); }}
                  >
                    <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path strokeLinecap="round" strokeWidth={2} d="M6 6l12 12M18 6L6 18" /></svg>
                  </button>
                </div>
                {/* Panel content */}
                <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', paddingRight: 4 }}>
                  {rightArtifactsOpen ? (
                    <div className="space-y-3">
                      {/* Entities artifact */}
                      <div className="p-2 rounded" style={{ background: 'var(--white-06)', border: '1px solid var(--overlay-line-90)' }}>
                        <div className="flex items-center justify-between mb-2">
                          <div className="text-12" style={{ color: 'var(--text-heading)' }}>Entities</div>
                          <button
                            type="button"
                            className="px-2 py-1 rounded text-11"
                            style={{ background: 'transparent', color: 'var(--text-secondary-strong)' }}
                            onClick={async () => {
                              try {
                                const pid = await useGenaiChatStore.getState().ensureProject();
                                await api.analyzeEntities(pid, '');
                              } catch {}
                            }}
                          >
                            Refresh
                          </button>
                        </div>
                        <EntitiesList projectId={genaiChat.projectId} />
                      </div>
                    </div>
                  ) : rightContextOpen ? (
                    <div className="space-y-2">
                      {/* Sections list */}
                      {sectionStarts.length > 0 && (
                        <div className="mb-2">
                          <div className="text-[11px] mb-1 flex items-center gap-1" style={{ color: 'var(--text-secondary-strong)' }}>
                            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                              <rect x="4" y="5" width="16" height="4" rx="1" strokeWidth="2" />
                              <rect x="4" y="11" width="16" height="4" rx="1" strokeWidth="2" />
                              <rect x="4" y="17" width="16" height="2" rx="1" strokeWidth="2" />
                            </svg>
                            <span>Sections</span>
                          </div>
                          <div className="space-y-1">
                            {sectionStarts.map((s) => (
                              <button
                                key={s.id}
                                type="button"
                                className="w-full text-left p-2 rounded transition-colors"
                                style={{ background: 'var(--white-06)', border: '1px solid var(--overlay-line-90)', color: 'var(--text-heading)' }}
                                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-12)'; }}
                                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-06)'; }}
                                onClick={() => scrollToInteraction(s.id)}
                              >
                                <div className="text-12 truncate">{(s as any).content || 'Section'}</div>
                              </button>
                            ))}
                          </div>
                        </div>
                      )}

                      {/* Pinned messages */}
                      <div className="text-[11px] mb-1 mt-3 flex items-center gap-1" style={{ color: 'var(--text-tertiary-weak)' }}>
                        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor" style={{ color: 'var(--accent-research)' }}>
                          <path d="M5 5a2 2 0 012-2h10a2 2 0 012 2v16l-7-3.5L5 21V5z" />
                        </svg>
                        <span>Pinned messages</span>
                      </div>
                      {pinnedInteractions.length === 0 ? (
                        <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>No pinned messages yet.</div>
                      ) : (
                        pinnedInteractions.map((it) => (
                          <div key={it.id} className="p-2 rounded text-left w-full transition-colors" style={{ background: 'var(--white-06)', border: '1px solid var(--overlay-line-90)' }}>
                            <div className="flex items-start justify-between gap-2">
                              <button type="button" onClick={() => scrollToInteraction(it.id)} className="text-left" style={{ flex: 1 }} onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).parentElement!.parentElement!.style.backgroundColor = 'var(--white-12)'; }} onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).parentElement!.parentElement!.style.backgroundColor = 'var(--white-06)'; }}>
                            <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>
                              {it.kind === 'assistant' ? 'Assistant' : it.kind === 'user' ? 'User' : (it.agentProfile ? 'Agent' : (it.kind || ''))}
                            </div>
                            <div className="text-12" style={{ color: 'var(--text-heading)', whiteSpace: 'pre-wrap' }}>
                              {truncateCenterWords(it.contentSanitized || it.content || '')}
                            </div>
                              </button>
                            </div>
                            {Array.isArray(it?.metadata?.notes) && it.metadata!.notes!.length > 0 && (
                              <div className="mt-2 space-y-1">
                                {it.metadata!.notes!.map((note, idx) => (
                                  <div key={idx} className="flex items-start justify-between gap-2 p-1 rounded" style={{ background: 'var(--white-10)' }}>
                                    <button
                                      type="button"
                                      className="text-left flex-1"
                                      style={{ color: 'var(--text-secondary-strong)', whiteSpace: 'pre-wrap' }}
                                      title="Scroll to note in message"
                                      onClick={(e) => {
                                        e.stopPropagation();
                                        // Try to scroll to the first occurrence of this note within the message
                                        try {
                                          const container = document.querySelector(`.markdown[data-interaction-id=\\"${it.id}\\"]`) as HTMLElement | null;
                                          if (container) {
                                            const r = findFirstRange(container, String(note));
                                            if (r) {
                                              const rect = r.getBoundingClientRect();
                                              const host = messagesRef.current as HTMLElement | null;
                                              if (host) {
                                                const hostRect = host.getBoundingClientRect();
                                                const offset = rect.top - hostRect.top + host.scrollTop - host.clientHeight / 2;
                                                host.scrollTo({ top: Math.max(0, offset), behavior: 'smooth' });
                                              } else {
                                                (container as HTMLElement).scrollIntoView({ behavior: 'smooth', block: 'center' });
                                              }
                                            } else {
                                              (container as HTMLElement).scrollIntoView({ behavior: 'smooth', block: 'center' });
                                            }
                                          }
                                        } catch {}
                                      }}
                                    >
                                      <span className="text-[12px]">{note}</span>
                                    </button>
                                    <button
                                      type="button"
                                      aria-label="Remove note"
                                      className="p-1 rounded"
                                      style={{ background: 'transparent', color: 'var(--text-secondary-strong)' }}
                                      onClick={async (ev) => {
                                        ev.stopPropagation();
                                        try {
                                          const notes = (it.metadata?.notes || []).filter((_, i) => i !== idx);
                                          const pinned = it.metadata?.pinned === true || notes.length > 0; // keep pinned if still notes exist
                                          await api.updateProjectInteraction(genaiChat.projectId, it.id, { metadata: { pinned, notes } });
                                          await genaiChat.hydrateInteractions();
                                        } catch {}
                                      }}
                                      onMouseEnter={(ev) => { (ev.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-12)'; }}
                                      onMouseLeave={(ev) => { (ev.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                                    >
                                      <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path strokeLinecap="round" strokeWidth={2} d="M6 6l12 12M18 6L6 18"/></svg>
                                    </button>
                                  </div>
                                ))}
                              </div>
                            )}
                          </div>
                        ))
                      )}
                    </div>
                  ) : (
                    <DocumentsAndSources 
                      projectId={genaiChat.projectId} 
                      onDocumentDelete={(docId: string) => {
                        console.log('[DiscoveryPage] Document deleted', { docId });
                        // Optionally refresh interactions or handle cleanup
                      }}
                      onOpenDocument={(doc: any) => setActiveDoc(doc)}
                    />
                  )}
                </div>
                
                {/* Workspace Artifacts toolbar (bottom) */}
                {rightArtifactsOpen && (
                  <div className="mt-2 px-3" style={{ borderTop: '1px solid var(--overlay-line-90)', paddingTop: 8 }}>
                    {/* Inline analysis status indicator */}
                    <ArtifactsAnalysisStatus />
                    <div style={{ display: 'flex', justifyContent: 'flex-start', gap: 6, marginTop: 6 }}>
                    <button
                      type="button"
                      aria-label="Analyze entities"
                      title="Analyze entities"
                      className="p-2 rounded"
                      style={{ background: 'transparent', color: 'var(--text-secondary-strong)', cursor: 'pointer' }}
                      onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                      onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                      onClick={async () => {
                        try {
                          const pid = await genaiChat.ensureProject();
                          await (await import('../lib/api')).api.analyzeEntities(pid, '');
                        } catch {}
                      }}
                    >
                      {/* New entities icon: network nodes */}
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                        <circle cx="5" cy="12" r="2" strokeWidth="2" />
                        <circle cx="19" cy="7" r="2" strokeWidth="2" />
                        <circle cx="19" cy="17" r="2" strokeWidth="2" />
                        <path d="M7 12h8M17 8l-4 4 4 4" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                      </svg>
                    </button>
                    {/* Facts */}
                    <button
                      type="button"
                      aria-label="Analyze facts"
                      title="Analyze facts"
                      className="p-2 rounded"
                      style={{ background: 'transparent', color: 'var(--text-secondary-strong)', cursor: 'pointer' }}
                      onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                      onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                      onClick={async () => { try { const pid = await genaiChat.ensureProject(); await (await import('../lib/api')).api.analyzeFacts(pid, ''); } catch {} }}
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                        <path d="M3 4h18v4H3zM3 10h18v10H3z" strokeWidth="2" />
                        <path d="M6 13h6M6 17h10" strokeWidth="2" strokeLinecap="round" />
                      </svg>
                    </button>
                    {/* Events */}
                    <button
                      type="button"
                      aria-label="Analyze events"
                      title="Analyze events"
                      className="p-2 rounded"
                      style={{ background: 'transparent', color: 'var(--text-secondary-strong)', cursor: 'pointer' }}
                      onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                      onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                      onClick={async () => { try { const pid = await genaiChat.ensureProject(); await (await import('../lib/api')).api.analyzeEvents(pid, ''); } catch {} }}
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                        <rect x="3" y="4" width="18" height="16" rx="2" strokeWidth="2" />
                        <path d="M3 8h18M7 2v4M17 2v4M8 12h4" strokeWidth="2" strokeLinecap="round" />
                      </svg>
                    </button>
                    {/* Metrics */}
                    <button
                      type="button"
                      aria-label="Analyze metrics"
                      title="Analyze metrics"
                      className="p-2 rounded"
                      style={{ background: 'transparent', color: 'var(--text-secondary-strong)', cursor: 'pointer' }}
                      onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                      onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                      onClick={async () => { try { const pid = await genaiChat.ensureProject(); await (await import('../lib/api')).api.analyzeMetrics(pid, ''); } catch {} }}
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                        <path d="M4 20V10M10 20V4M16 20v-8M22 20H2" strokeWidth="2" strokeLinecap="round" />
                      </svg>
                    </button>
                    {/* Decisions */}
                    <button
                      type="button"
                      aria-label="Analyze decisions"
                      title="Analyze decisions"
                      className="p-2 rounded"
                      style={{ background: 'transparent', color: 'var(--text-secondary-strong)', cursor: 'pointer' }}
                      onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                      onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                      onClick={async () => { try { const pid = await genaiChat.ensureProject(); await (await import('../lib/api')).api.analyzeDecisions(pid, ''); } catch {} }}
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                        <path d="M4 12h8M12 12l4-4M12 12l4 4" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                        <circle cx="4" cy="12" r="2" strokeWidth="2" />
                      </svg>
                    </button>
                    <button
                      type="button"
                      aria-label="Create artifact"
                      title="Create artifact"
                      className="p-2 rounded"
                      style={{ background: 'transparent', color: 'var(--text-secondary-strong)', cursor: 'pointer' }}
                      onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                      onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                      onClick={() => { /* no-op for now */ }}
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                        <path d="M6 2h7l5 5v13a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2z" strokeWidth="2" />
                        <path d="M13 2v6h6" strokeWidth="2" />
                        <path d="M12 10v6M9 13h6" strokeWidth="2" strokeLinecap="round" />
                      </svg>
                    </button>
                    </div>
                  </div>
                )}
                
                {/* Documents & Sources toolbar (bottom) */}
                {rightPanelOpen && !rightArtifactsOpen && !rightContextOpen && (
                  <div className="mt-2 px-3" style={{ borderTop: '1px solid var(--overlay-line-90)', paddingTop: 8, display: 'flex', justifyContent: 'flex-start', gap: 6 }}>
                    <button
                      type="button"
                      aria-label="Add document"
                      title="Add document"
                      className="p-2 rounded"
                      style={{ background: 'transparent', color: 'var(--text-secondary-strong)', cursor: 'pointer' }}
                      onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                      onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                      onClick={() => { document.getElementById('doc-upload-input')?.click(); }}
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                        <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" strokeWidth="2" />
                        <path d="M14 2v6h6M12 11v6M9 14h6" strokeWidth="2" strokeLinecap="round" />
                      </svg>
                    </button>
                    <button
                      type="button"
                      aria-label="Add RSS source"
                      title="Add RSS source (coming soon)"
                      className="p-2 rounded"
                      style={{ background: 'transparent', color: 'var(--text-tertiary-weak)', cursor: 'not-allowed', opacity: 0.5 }}
                      disabled
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                        <path d="M4 11a9 9 0 0 1 9 9M4 4a16 16 0 0 1 16 16" strokeWidth="2" strokeLinecap="round" />
                        <circle cx="5" cy="19" r="1" fill="currentColor" />
                      </svg>
                    </button>
                    <button
                      type="button"
                      aria-label="Add Telegram source"
                      title="Add Telegram source (coming soon)"
                      className="p-2 rounded"
                      style={{ background: 'transparent', color: 'var(--text-tertiary-weak)', cursor: 'not-allowed', opacity: 0.5 }}
                      disabled
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                        <path d="M21 5L2 12.5l7 1M21 5l-2.5 15L9 13M21 5L9 13m0 0l1 6l2-3" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                      </svg>
                    </button>
                  </div>
                )}
              </div>
            )}

            {/* Bottom slot: reserved area to match left column layout */}
            <div className="mt-3" style={{ marginTop: 'auto', minHeight: titleSlotMinHeight, position: 'relative' }} />
          </div>
        )}
      </div>
    </div>
  );
};

export default DiscoveryPage;