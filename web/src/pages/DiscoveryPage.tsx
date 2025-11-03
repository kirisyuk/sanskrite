import React, { useState, useEffect, useRef } from 'react';
import ReactMarkdown, { Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import Settings from '../components/Settings';
import Account from '../components/Account';
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
import PerspectivesPanel from '../components/PerspectivesPanel';
import PerspectiveAnalysisPanel from '../components/PerspectiveAnalysisPanel';
import AgentPalette from '../components/AgentPalette';
import WorkspaceLibrary from '../components/WorkspaceLibrary';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { getAverageTiming } from '../lib/timingCache';
import { mapAliasToModel } from '../lib/modelMapping';
import { TelegramFetchUpdatesTool } from '../lib/genaiTools';
import { useTranslation } from 'react-i18next';
import { AGENT_PRESETS } from '../lib/agentPresets';
import { HATL_ONE_INSTRUCTIONS } from '../lib/hatlInstructions';
import DocumentViewer from '../components/DocumentViewer';
import FindingsPanel from '../components/FindingsPanel';
import ChatGraphMessage from '../components/ChatGraphMessage';
import { evaluateBias } from '../lib/biasEvaluation';
import { EntitiesList, FactsList, EventsList, MetricsList, DecisionsList } from '../components/ArtifactLists';
import { ThemeContext } from '../components/ThemeProvider';
import { EvaluationDetails } from '../components/EvaluationDetails';
import { ActionRow } from '../components/ActionRow';
import { UserPositionAnalysis } from '../components/UserPositionAnalysis';
import { LLMEvaluation } from '../components/LLMEvaluation';
import { DiscussionFlow } from '../components/DiscussionFlow';
import { TitleSuggestions } from '../components/TitleSuggestions';
import { toggleInteractionPin, isInteractionPinned } from '../lib/interactionHelpers';
import { highlightNotes, clearHighlights, findFirstRange, applyGlobalHighlight, clearGlobalHighlight } from '../lib/textHighlight';
import { getPreferredLanguage, getLanguageName, getAnchorRect, getSxSystemInstructionShort, getSxSystemInstructionFull } from '../lib/uiUtils';
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

const DiscoveryPage: React.FC = () => {
  const { t } = useTranslation();
  const { currentUser } = useAuth();
  const { theme } = React.useContext(ThemeContext);
  const { projects, currentProject, fetchProjects, deleteProject, setCurrentProject } = useProjectsStore();
  const { sources, loading, executeDiscovery, fetchProjectSources, fetchProjectInteractions, clearSources, connectWs, answer, correlationId, interactions, sendChatMessage } = useDiscoveryStore();
  const { viewMode } = useUIStore();
  
  const [queryText, setQueryText] = useState('');
  const [sideOpen, setSideOpen] = useState(false);
  const [running, setRunning] = useState(false);
  const [thinkingSeconds, setThinkingSeconds] = useState(0);
  const [genaiThinkingSeconds, setGenaiThinkingSeconds] = useState(0);
  const [averageTimingSeconds, setAverageTimingSeconds] = useState<number | null>(null);
  const [entitiesAnalyzing, setEntitiesAnalyzing] = useState(false);
  const [unfoldSubView, setUnfoldSubView] = useState<'chat' | 'perspectives'>('chat');
  const [currentPerspectivePreset, setCurrentPerspectivePreset] = useState<string>('consulting');
  const [isAnalysisClosed, setIsAnalysisClosed] = useState(false);
  const [unfoldMode, setUnfoldMode] = React.useState<'text' | 'graph'>('text');
  const messagesRef = useRef<HTMLDivElement | null>(null);
  const discoveryTimeoutRef = useRef<number | null>(null);
  const genaiThinkingIntervalRef = useRef<number | null>(null);
  const [showSettings, setShowSettings] = useState(false);
  const [showAccount, setShowAccount] = useState(false);
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
  // PERFORMANCE FIX: Subscribe only to specific values for critical paths, keep genaiChat for compatibility
  const genaiChat = useGenaiChatStore();
  const genaiChatProjectId = useGenaiChatStore((state) => state.projectId);
  const genaiChatInteractionsLength = useGenaiChatStore((state) => state.interactions.length);
  const genaiChatMessagesLength = useGenaiChatStore((state) => state.messages.length);
  const genaiChatHydrating = useGenaiChatStore((state) => state.hydrating);
  const genaiChatStreaming = useGenaiChatStore((state) => state.streaming);
  const genaiSettings = useGenAISettings();
  const lastAnalysisStatus = useDiscoveryStore((s: any) => (s as any).lastAnalysisStatus);
  const notifiedAnalysesRef = React.useRef<Set<string>>(new Set());

  // Auto-switch to Chat tab when switching chats/New Chat
  React.useEffect(() => {
    try {
      setUnfoldSubView('chat');
    } catch {}
  }, [genaiChatProjectId]);

  React.useEffect(() => {
    if (genaiChatInteractionsLength === 0 && genaiChatMessagesLength === 0) {
      try { setUnfoldSubView('chat'); } catch {}
    }
  }, [genaiChatInteractionsLength, genaiChatMessagesLength]);

  // Reset analysis closed state when preset changes or switching to perspectives
  React.useEffect(() => {
    setIsAnalysisClosed(false);
  }, [currentPerspectivePreset, unfoldSubView]);

  React.useEffect(() => {
    try {
      const st = lastAnalysisStatus;
      if (!st) return;
      const kind = String(st.kind || '').toLowerCase();
      const status = String(st.status || '').toLowerCase();
      if (!kind || status !== 'done') return;
      // Only for the currently open chat project
      const currentPid = String(genaiChat.projectId || '');
      const eventPid = String(st.projectId || '');
      if (!currentPid || !eventPid || eventPid !== currentPid) return;
      const key = `${eventPid}:${kind}:${st.ts || st.elapsedMs || st.count || Date.now()}`;
      if (notifiedAnalysesRef.current.has(key)) return;
      notifiedAnalysesRef.current.add(key);
      (async () => {
        try {
          const pid = eventPid;
          if (!pid) return;
          const n = typeof st.count === 'number' ? st.count : 0;
          const ms = typeof st.elapsedMs === 'number' ? st.elapsedMs : undefined;
          const t = (ms && ms >= 0) ? (ms < 1000 ? `${Math.round(ms)}ms` : `${(ms/1000).toFixed(1)}s`) : '';
          const unitOne = kind === 'entities' ? 'entity' : kind === 'facts' ? 'fact' : kind === 'events' ? 'event' : kind === 'metrics' ? 'metric' : kind === 'decisions' ? 'decision' : kind;
          const unitMany = kind === 'entities' ? 'entities' : kind === 'facts' ? 'facts' : kind === 'events' ? 'events' : kind === 'metrics' ? 'metrics' : kind === 'decisions' ? 'decisions' : kind;
          const summary = n === 0 ? `Found no ${unitMany}` : (n === 1 ? `Found 1 ${unitOne}` : `Found ${n} ${unitMany}`);
          const content = t ? `${summary} (${t})` : summary;
          const { api } = await import('../lib/api');
          await api.createProjectInteraction(pid, { kind: 'assistant', subkind: 'analysis.summary', content });
          await genaiChat.hydrateInteractions();
        } catch {}
      })();
    } catch {}
  }, [lastAnalysisStatus, genaiChat]);
  // Collapsible sections state: sectionId -> collapsed
  const [collapsedSections, setCollapsedSections] = useState<Record<string, boolean>>({});
  // Expanded chip state: only one chip can be expanded at a time (messageId-type-index or interactionId-type-index)
  const [expandedChip, setExpandedChip] = useState<string | null>(null);
  // Toggles for collapsible analysis sections
  const [showUserAnalysis, setShowUserAnalysis] = useState(false);
  const [showLLMReview, setShowLLMReview] = useState(false);
  const [showDiscussionFlow, setShowDiscussionFlow] = useState(false);
  const [leftPanel, setLeftPanel] = useState<'user' | 'flow' | null>(null);
  const [badgeUser, setBadgeUser] = useState(false);
  const [badgeLLM, setBadgeLLM] = useState(false);
  const [badgeFlow, setBadgeFlow] = useState(false);
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
      return useGenaiChatStore.getState().interactions.some((it: any) => {
        // Exclude perspective interactions (kind='note' with subkind starting with 'perspective.')
        if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
          return false;
        }
        // Exclude perspective analysis interactions
        if (it.kind === 'system' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.') && it.subkind.endsWith('.analysis')) {
          return false;
        }
        return !!it?.metadata?.pinned;
      });
    } catch { return false; }
  }, [genaiChatInteractionsLength, rightContextOpen, rightPanelOpen]);
  
  // PERFORMANCE FIX: Use selector to get interactions only when needed, not subscribe to entire array
  const getInteractions = React.useCallback(() => useGenaiChatStore.getState().interactions, []);
  
  const sectionStarts = React.useMemo(() => {
    try { 
      const interactions = getInteractions();
      return (interactions || []).filter((it: any) => it?.kind === 'section_start'); 
    } catch { return []; }
  }, [getInteractions, genaiChatInteractionsLength]);
  const hasSections = sectionStarts.length > 0;
  React.useEffect(() => { if (!hasPinned && !hasSections) setRightContextOpen(false); }, [hasPinned, hasSections]);
  // Ensure interactions are loaded on chat load to detect pinned items
  // Only hydrate once per projectId, not on every store update
  const projectId = useGenaiChatStore((s) => s.projectId);
  const hydrateInteractions = useGenaiChatStore((s) => s.hydrateInteractions);
  const interactionsLoadedRef = React.useRef<string | null>(null);
  
  React.useEffect(() => {
    try {
      if (projectId && projectId !== 'local-unfold' && interactionsLoadedRef.current !== projectId) {
        interactionsLoadedRef.current = projectId;
        hydrateInteractions();
      } else if (!projectId || projectId === 'local-unfold') {
        interactionsLoadedRef.current = null;
      }
    } catch {}
  }, [projectId, hydrateInteractions]);

  const pinnedInteractions = React.useMemo(() => {
    try {
      const interactions = getInteractions();
      return (interactions || []).filter((it: any) => {
        // Exclude perspective interactions (kind='note' with subkind starting with 'perspective.')
        if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
          return false;
        }
        // Exclude perspective analysis interactions
        if (it.kind === 'system' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.') && it.subkind.endsWith('.analysis')) {
          return false;
        }
        return !!it?.metadata?.pinned;
      });
    } catch { return []; }
  }, [getInteractions, genaiChatInteractionsLength]);
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
      if (el && messagesRef.current) {
        const rect = el.getBoundingClientRect();
        const host = messagesRef.current;
        const hostRect = host.getBoundingClientRect();
        const offset = rect.top - hostRect.top + host.scrollTop - host.clientHeight / 2;
        host.scrollTo({ top: Math.max(0, offset), behavior: 'smooth' });
      } else if (el && typeof el.scrollIntoView === 'function') {
        // Fallback if messagesRef is not available
        el.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    } catch {}
  };
  React.useEffect(() => { if (sideOpen) setTitlesGlow(false); }, [sideOpen]);
  const [availUserAnalysis, setAvailUserAnalysis] = useState(false);
  const [availLLMReview, setAvailLLMReview] = useState(false);
  const [availDiscussionFlow, setAvailDiscussionFlow] = useState(false);
  const [riskBanner, setRiskBanner] = useState<null | { visible: boolean; level: 'High'|'Medium'|'Low'; rationale: string }>(null);
  const [isHighRisk, setIsHighRisk] = useState(false);
  const hasAssistant = React.useMemo(
    () => {
      const interactions = getInteractions();
      return interactions.some((it) => it.kind === 'assistant' && (it.contentSanitized || it.content));
    },
    [getInteractions, genaiChatInteractionsLength]
  );

  // Detect server-provided user stance (eval.stance linked to a user message)
  const stanceFromInteractionsTop = React.useMemo(() => {
    try {
      const interactions = getInteractions();
      const ints = [...interactions].reverse();
      const userIds = new Set(ints.filter((it) => it.kind === 'user').map((it) => it.id));
      return ints.find((it) => it.subkind === 'eval.stance' && it.replyTo && userIds.has(it.replyTo));
    } catch { return null as any; }
  }, [getInteractions, genaiChatInteractionsLength]);

  // If server already sent stance, make the User button available
  React.useEffect(() => {
    setAvailUserAnalysis(!!stanceFromInteractionsTop);
  }, [stanceFromInteractionsTop]);

  // Discussion Flow is always available now (no need to check message count)
  React.useEffect(() => {
    setAvailDiscussionFlow(true);
  }, []);

  // Callback for DiscussionFlow onFirstResult - must be defined unconditionally (Rules of Hooks)
  const onFirstResultDiscussionFlow = React.useCallback(() => {
    setBadgeFlow(true);
    setAvailDiscussionFlow(true);
  }, []);

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

  // Centralized function to reset chat state (used for New Chat, deleting current chat, and switching chats)
  const resetChatState = React.useCallback((resetToLocal: boolean = true) => {
    // Clear right side: sources, status, input, current chat, interactions
    clearSources();
    // Start a fresh draft for the next message
    setQueryText('');
    // Reset high-risk state
    setIsHighRisk(false);
    setRiskBanner(null);
    // Reset Unfold chat state and analysis artifacts
    try {
      const st = useGenaiChatStore.getState();
      if (resetToLocal) {
        st.reset();
        st.setProjectId('local-unfold');
        try { console.log('[resetChatState] reset -> projectId set to local-unfold'); } catch {}
      }
    } catch {}
    setShowUserAnalysis(false);
    setShowLLMReview(false);
    setShowDiscussionFlow(false);
    // Clear analysis summary dedupe cache
    try { notifiedAnalysesRef.current = new Set(); } catch {}
  }, [clearSources]);

  useEffect(() => {
    const pid = currentProject?.id;
    if (pid) {
      try {
        const st = useGenaiChatStore.getState();
        const wasDifferentProject = st.projectId !== pid;
        if (wasDifferentProject) {
          // Clear local UI states when switching to a different project
          resetChatState(false);
          // Set new project ID (this clears chat state in store)
          st.setProjectId(pid);
        }
      } catch {}
      fetchProjectSources(pid);
      fetchProjectInteractions(pid);
      try {
        const st = useGenaiChatStore.getState();
        st.hydrateInteractions();
        st.ensureStanceAggregate();
      } catch {}
    } else {
      clearSources();
    }
  }, [currentProject?.id, resetChatState]);

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
  }, [interactions.length, running, genaiChatMessagesLength]);

  // Auto-scroll to bottom after hydration completes
  useEffect(() => {
    if (!genaiChatHydrating && genaiChatInteractionsLength > 0 && messagesRef.current) {
      // Small delay to ensure DOM has updated
      const timeoutId = setTimeout(() => {
        if (messagesRef.current) {
          messagesRef.current.scrollTop = messagesRef.current.scrollHeight;
        }
      }, 100);
      return () => clearTimeout(timeoutId);
    }
  }, [genaiChatHydrating, genaiChatInteractionsLength]);
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
              const preferredLang = getPreferredLanguage();
              // Open AgentPalette in edit mode anchored to input area
              const anchor = getAnchorRect();
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
                const sxSystemInstruction = getSxSystemInstructionShort();
                const options: any = {
                  profile: /_quality$/i.test(useModel || '') ? 'quality' : 'fast',
                  modelOverride: useModel,
                  groundingEnabled: settings.groundingEnabled,
                  temperature: settings.temperature,
                  maxOutputTokens: Math.max(512, settings.maxOutputTokens || 1024),
                  systemInstruction: [settings.systemInstruction || '', HATL_ONE_INSTRUCTIONS, sxSystemInstruction].filter(Boolean).join('\n\n').trim() || undefined,
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
            if (!sel) {
              console.warn('[AddToNotes] No selection found');
              return;
            }
            const range = document.getSelection()?.getRangeAt(0);
            const startEl = range ? (range.startContainer.nodeType === 3 ? (range.startContainer.parentElement) : (range.startContainer as Element)) as Element | null : null;
            const interId = startEl?.closest('.markdown')?.getAttribute('data-interaction-id') || '';
            if (!interId) {
              console.warn('[AddToNotes] No interaction ID found');
              return;
            }
            const store = useGenaiChatStore.getState();
            const it = store.interactions.find((x: any) => x.id === interId);
            if (!it) {
              console.warn('[AddToNotes] Interaction not found', { interId, availableIds: store.interactions.map((x: any) => x.id).slice(0, 5) });
              return;
            }
            const projectId = store.projectId;
            if (!projectId || projectId === 'local-unfold') {
              console.warn('[AddToNotes] Invalid project ID', { projectId });
              return;
            }
            // Resolve server ID if local ID is mapped
            const map = (store as any).persistedByLocalId || {};
            const serverId = map[it.id] || it.id;
            const meta = it.metadata || {};
            const notes = Array.isArray(meta.notes) ? [...meta.notes] : [];
            notes.push(sel);
            await api.updateProjectInteraction(projectId, serverId, { metadata: { pinned: true, notes } });
            await store.hydrateInteractions();
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
    const msgs = useGenaiChatStore.getState().messages;
    const last = msgs[msgs.length - 1];
    const awaitingFirstChunk = !!(genaiChatStreaming && last && last.role === 'assistant' && !last.content);
    if (awaitingFirstChunk) {
      if (!genaiThinkingIntervalRef.current) {
        setGenaiThinkingSeconds(0);
        genaiThinkingIntervalRef.current = window.setInterval(() => {
          setGenaiThinkingSeconds((s) => s + 1);
        }, 1000) as unknown as number;
        
        // Fetch average timing estimate
        const modelMapping = mapAliasToModel(genaiSettings.model);
        if (modelMapping) {
          // Estimate output tokens based on maxOutputTokens setting
          const estimatedTokens = genaiSettings.maxOutputTokens || 8192;
          getAverageTiming(modelMapping.provider, modelMapping.model, estimatedTokens)
            .then((timing) => {
              setAverageTimingSeconds(timing);
            })
            .catch((err) => {
              console.error('[DiscoveryPage] Failed to get average timing:', err);
              setAverageTimingSeconds(null);
            });
        } else {
          setAverageTimingSeconds(null);
        }
      }
    } else if (genaiThinkingIntervalRef.current) {
      window.clearInterval(genaiThinkingIntervalRef.current);
      genaiThinkingIntervalRef.current = null;
      setAverageTimingSeconds(null);
    }
  }, [genaiChat.streaming, genaiChat.messages.length, genaiSettings.model, genaiSettings.maxOutputTokens]);

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
      // Check if the deleted chat is currently open
      const isCurrentChat = currentProject?.id === projectId || genaiChat.projectId === projectId;
      
      await deleteProject(projectId);
      
      // If we deleted the current chat, reset it like "New Chat"
      if (isCurrentChat) {
        resetChatState();
      }
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
      const lang = getPreferredLanguage();
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
        setShowAccount(false);
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
              <h3 className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>{t('unfold.sections.navigation')}</h3>
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
                aria-label={t('unfold.sections.newChat')}
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors"
                style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => {
                  resetChatState();
                }}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  {/* Outlined plus in rounded rect (new chat) */}
                  <rect x="3.5" y="3.5" width="17" height="17" rx="3" strokeWidth={2} />
                  <path strokeLinecap="round" strokeWidth={2} d="M12 8v8M8 12h8" />
                </svg>
                <span className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>{t('unfold.sections.newChat')}</span>
              </button>
            </div>
            {/* Scrollable Chats section */}
            <div className="mb-4 flex-1 min-h-0 overflow-y-auto" style={{ scrollbarGutter: 'stable' }}>
              <div className="text-[11px] uppercase tracking-wide mb-2" style={{ color: 'var(--text-tertiary-weak)' }}>
                {t('unfold.sections.chats')}
              </div>

              {projects.length === 0 ? (
                <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>{t('unfold.sections.noChatsYet')}</div>
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
                        title={t('unfold.sections.deleteChat')}
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
            {(availUserAnalysis || availDiscussionFlow) && (
            <div className="mb-4 pt-3" style={{ borderTop: '1px solid var(--overlay-line-90)' }}>
              <div className="text-[11px] uppercase tracking-wide mb-2" style={{ color: 'var(--text-tertiary-weak)' }}>
                {t('unfold.sections.analysis')}
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
                <span className="text-12">{t('unfold.sections.howYouEngage')}</span>
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
                <span className="text-12">{t('unfold.sections.discussionFlow')}</span>
              </button>
              )}
            </div>
            )}

            {/* Bottom settings button */}
            <div className="mt-auto pt-4">
              {isAdmin && (
                  <button
                  type="button"
                  aria-label={t('unfold.sections.adminAccess')}
                  className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors mb-1"
                  style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                  onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                  onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                  onClick={() => { setShowAdmin(true); setShowSettings(false); setShowAccount(false); setStaticPage(null); }}
                >
                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    {/* Shield icon to reflect admin access */}
                    <path d="M12 3l7 3v6c0 5-3.5 8-7 9-3.5-1-7-4-7-9V6l7-3z" strokeWidth={2} />
                    <path d="M9.5 12.5l1.8 1.8 3.2-3.3" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                  <span className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>{t('unfold.sections.adminAccess')}</span>
                </button>
              )}
              <button
                type="button"
                aria-label="Account"
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors mb-1"
                style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => { setShowAccount(true); setShowSettings(false); setShowAdmin(false); setStaticPage(null); }}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
                  <circle cx="12" cy="7" r="4" />
                </svg>
                <span className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>Account</span>
              </button>
              <button
                type="button"
                aria-label={t('unfold.sections.settings')}
                className="w-full flex items-center gap-2 px-3 py-2 rounded-lg transition-colors"
                style={{ backgroundColor: 'transparent', color: 'var(--text-heading)' }}
                onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)'; }}
                onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                onClick={() => { setShowSettings(true); setShowAccount(false); setShowAdmin(false); setStaticPage(null); }}
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
                  <circle cx="12" cy="12" r="3" />
                </svg>
                <span className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>{t('unfold.sections.settings')}</span>
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
              resetChatState();
              setLeftPanel(null);
            }}
            className="w-8 h-8 rounded-lg flex items-center justify-center transition-all duration-300"
            style={{ backgroundColor: 'var(--newchat-bg)', color: 'var(--text-heading)', marginTop: '8px' }}
            aria-label={t('unfold.sections.newChat')}
            title={t('unfold.sections.newChat')}
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

          {/* Discussion Flow button (only visible if chat is not empty) */}
          {!(genaiChatInteractionsLength === 0 && genaiChatMessagesLength === 0) && (
          <button
            onClick={() => { setLeftPanel((p) => (p === 'flow' ? null : 'flow')); }}
            className="w-8 h-8 rounded-lg flex items-center justify-center transition-all duration-300"
            style={{ backgroundColor: leftPanel === 'flow' ? '#669999' : 'var(--newchat-bg)', color: leftPanel === 'flow' ? 'white' : 'var(--text-heading)', marginTop: '8px' }}
            aria-label={t('unfold.sections.discussionFlow')}
            title={t('unfold.sections.discussionFlow')}
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

        </div>
      )}

      {/* Mirror toggle button on the right edge (independent of sidebar) */}
      <button
        type="button"
        aria-label="Toggle Workspace Library"
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
          <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
          <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
          <path d="M8 7h8M8 11h8M8 15h4" strokeWidth={2} strokeLinecap="round" />
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

      {/* Workspace Artifacts button (styled like Context Navigation) shown on right edge */}
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
          try {
            const btn = document.getElementById('btn-artifacts');
            if (btn) btn.classList.remove('etg-glow-blue');
          } catch {}
        }}
        className="rounded-lg flex items-center justify-center transition-all duration-300"
        style={{
          position: 'fixed',
          right: 8,
          top: hasPinned ? 156 : 116,
          width: 32,
          height: 32,
          backgroundColor: 'var(--newchat-bg)',
          color: 'var(--text-heading)',
          zIndex: 40,
        }}
        onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--newchat-bg-hover)'; }}
        onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--newchat-bg)'; }}
      >
        {/* Icon: layers/stack for artifacts */}
        <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d="M12 2L2 7l10 5 10-5-10-5z" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
          <path d="M2 17l10 5 10-5" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
          <path d="M2 12l10 5 10-5" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      {/* Floating Account and Settings buttons (visible only when sidebar is hidden) */}
      {!sideOpen && !showSettings && !showAccount && (
        <>
          {/* Account button */}
          <button
            type="button"
            aria-label="Account"
            onClick={() => { setShowAccount(true); setShowSettings(false); setShowAdmin(false); setStaticPage(null); }}
            className="rounded-lg transition-colors flex items-center justify-center"
            style={{
              position: 'fixed',
              left: 8,
              bottom: 88,
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
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
              <circle cx="12" cy="7" r="4" />
            </svg>
          </button>
          {/* Settings button */}
          <button
            type="button"
            aria-label="Settings"
            onClick={() => { setShowSettings(true); setShowAccount(false); setShowAdmin(false); setStaticPage(null); }}
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
            <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
              <circle cx="12" cy="12" r="3" />
            </svg>
          </button>
        </>
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
        {/* Sources column (scrollable) - hidden when Account or Settings are shown */}
        <div className="min-h-0 h-full" style={{ background: 'transparent', display: (showAccount || showSettings) ? 'none' : 'flex', flexDirection: 'column' }}>
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
          {/* Show perspective analysis when in perspectives view, otherwise show user/flow panels */}
          {unfoldSubView === 'perspectives' && !isAnalysisClosed && (() => {
            const projectId = genaiChat.projectId;
            if (!projectId || projectId === 'local-unfold') return null;
            
            // Get perspective from cache (O(1) lookup)
            const perspective = genaiChat.perspectives[currentPerspectivePreset];
            
            if (!perspective) return null;
            
            return (
              <>
                {/* Spacer to align with separator line in center column */}
                <div style={{ height: '44px' }} />
                <div
                  className="mt-3 rounded-xl sidebar-panel"
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
                  <PerspectiveAnalysisPanel 
                    preset={currentPerspectivePreset}
                    perspectiveId={perspective.id}
                    perspectiveContent={perspective.content || null}
                    onClose={() => setIsAnalysisClosed(true)}
                  />
                </div>
              </>
            );
          })()}
          {leftPanel && (
            <>
              {/* Empty spacer matching Chat | Perspectives toggle height (only for flow panel) - placed before entire panel to shift it down */}
              {leftPanel === 'flow' && (
                <div className="flex items-center justify-center gap-1 mb-2 w-full" role="tablist" aria-label="Spacer" style={{ visibility: 'hidden', pointerEvents: 'none', height: '28px' }}>
                  <div className="px-2 py-1 text-12 rounded" />
                  <div className="px-2 py-1 text-12 rounded" />
                </div>
              )}
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
                  <h3 className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>{leftPanel === 'user' ? 'How you engage' : 'Discussion flow'}</h3>
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
                      <DiscussionFlow onFirstResult={onFirstResultDiscussionFlow} />
                    )}
                  </div>
                </div>
              )}
              {/* Duplicate old header/content removed to avoid double rendering */}
            </div>
            </>
          )}

            {/* Bottom slot: topic title suggestions now run headless (no UI) */}
          <div className="mt-3" style={{ marginTop: 'auto', minHeight: titleSlotMinHeight, position: 'relative' }}>
            <TitleSuggestions
              visible={false}
              onClose={() => {}}
              onAvailableChange={() => {}}
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

        {/* Main center column: renders discussion or graph placeholder (spans center+right when graph, or all columns when Account/Settings) */}
        <div
          className="min-h-0 flex flex-col"
          style={{ 
            position: 'relative', 
            gridColumn: (showAccount || showSettings) ? '1 / 4' : (viewMode === 'graph' ? '2 / 4' : '2 / 3')
          }}
        >
        {showAccount ? (
          <div className="w-full px-6 mt-2 mb-2" style={{ maxWidth: '100%' }}>
            <div className="mb-2 flex items-center">
              <button
                onClick={() => setShowAccount(false)}
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
              <div className="ml-2 text-13" style={{ color: 'var(--text-heading)' }}>Account</div>
            </div>
            <Account />
          </div>
        ) : showSettings ? (
          <div className="w-full px-6 mt-2 mb-2" style={{ maxWidth: '100%' }}>
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
            {/* Unfold subview toggle */}
            <div className="flex items-center justify-center gap-1 mb-2 w-full" role="tablist" aria-label="Unfold view">
              <button
                type="button"
                role="tab"
                aria-selected={unfoldSubView === 'chat'}
                aria-controls="unfold-chat-panel"
                onClick={() => setUnfoldSubView('chat')}
                className="px-2 py-1 text-12 rounded"
                style={{
                  background: unfoldSubView === 'chat' ? 'var(--white-10)' : 'transparent',
                  color: 'var(--text-heading)'
                }}
              >
                {t('unfold.sections.chat')}
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={unfoldSubView === 'perspectives'}
                aria-controls="unfold-perspectives-panel"
                onClick={() => setUnfoldSubView('perspectives')}
                className="px-2 py-1 text-12 rounded"
                style={{
                  background: unfoldSubView === 'perspectives' ? 'var(--white-10)' : 'transparent',
                  color: 'var(--text-heading)'
                }}
              >
                {t('unfold.sections.perspectives')}
              </button>
            </div>
            {/* Subtle separator line */}
            <div 
              className="mb-4 w-full" 
              style={{ 
                height: '1px', 
                backgroundColor: 'var(--overlay-line-90)',
                opacity: theme === 'light' ? 0.3 : 0.7
              }} 
            />
            {/* Perspectives content (full column) */}
            {unfoldSubView === 'perspectives' && (
              <PerspectivesPanel onPresetChange={setCurrentPerspectivePreset} />
            )}
            {/* GenAI chat history */}
            <div ref={messagesRef} className="flex-1 overflow-y-auto px-1 pb-2 min-h-[160px]" style={{ contentVisibility: 'auto', containIntrinsicSize: '800px 600px', display: unfoldSubView === 'perspectives' ? 'none' : undefined }}>
              {(() => {
                // Check if chat is empty (not hydrating and no messages/interactions)
                const isEmpty = !genaiChat.hydrating && genaiChat.interactions.length === 0 && genaiChat.messages.length === 0;
                
                if (isEmpty) {
                  return (
                    <div className="flex items-center justify-center" style={{ height: '100%', minHeight: 'calc(100vh - 300px)', padding: '48px 24px' }}>
                      <div className="max-w-md text-center">
                        <div className="text-16" style={{ color: 'var(--text-heading)', lineHeight: '1.6', fontSize: 'clamp(18px, 2vw, 24px)', fontWeight: 500 }}>
                          {t('unfold.perspectives.chatEmptyState.description')}
                        </div>
                      </div>
                    </div>
                  );
                }
                
                return (
                  <div className="space-y-2">
                    {/* Skeleton loading during hydration */}
                    {genaiChat.hydrating && genaiChat.interactions.length === 0 && (
                      <div className="space-y-2 py-2">
                        <div style={{ height: 14, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                          <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                        </div>
                        <div style={{ height: 10, width: '90%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                          <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                        </div>
                        <div style={{ height: 10, width: '80%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                          <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                        </div>
                        <div style={{ height: 10, width: '70%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                          <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                        </div>
                      </div>
                    )}
                    {(() => {
                  // Merge messages and interactions, sort by timestamp to ensure correct order
                  type MergedItem = { type: 'message' | 'interaction'; data: any; timestamp: number; };
                  const merged: MergedItem[] = [];

                  // Add interactions first (user, assistant, and section markers)
                  // This ensures interactions are processed before messages during streaming
                  // Exclude perspective interactions (kind='note' with subkind starting with 'perspective.')
                  genaiChat.interactions
                    .filter((it) => {
                      // Exclude perspective interactions
                      if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
                        return false;
                      }
                      // Exclude perspective analysis interactions
                      if (it.kind === 'system' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.') && it.subkind.endsWith('.analysis')) {
                        return false;
                      }
                      return it.kind === 'user' || it.kind === 'assistant' || it.kind === 'action' || it.kind === 'section_start' || it.kind === 'section_end';
                    })
                    .forEach((it) => {
                      let ts = 0;
                      // First, try to get timestamp from originalMessageId (for messages created during streaming)
                      // This ensures user and assistant messages from the same stream have consistent timestamps
                      try {
                        const originalMsgId = (it as any).originalMessageId;
                        if (originalMsgId && typeof originalMsgId === 'string') {
                          const parsedTs = parseInt(originalMsgId.split('-')[0]);
                          if (!Number.isNaN(parsedTs) && parsedTs > 0) {
                            ts = parsedTs;
                          }
                        }
                      } catch {}
                      // Fallback to createdAt if originalMessageId timestamp not available
                      if (ts === 0) {
                        try {
                          const ca: any = (it as any).createdAt;
                          if (ca && typeof ca.seconds === 'number') {
                            ts = ca.seconds * 1000;
                          } else if (typeof ca === 'string') {
                            const d = Date.parse(ca);
                            if (!Number.isNaN(d)) ts = d;
                          } else if (typeof ca === 'number') {
                            ts = ca;
                          }
                        } catch {}
                      }
                      merged.push({ type: 'interaction', data: it, timestamp: ts });
                    });

                  // Add filtered messages (only during streaming, and not already in interactions)
                  // These are typically assistant messages that are being streamed
                  if (genaiChat.streaming) {
                    genaiChat.messages.forEach((m) => {
                      const hasInteraction = genaiChat.interactions.some((it) => 
                        (it as any).originalMessageId === m.id || it.id === m.id
                      );
                      if (!hasInteraction) {
                        // Parse timestamp from id (format: "timestamp-role")
                        // Use Date.now() as fallback to ensure new messages appear at the end, not at the beginning
                        const parsedTs = parseInt(m.id.split('-')[0]);
                        const ts = (!Number.isNaN(parsedTs) && parsedTs > 0) ? parsedTs : Date.now();
                        merged.push({ type: 'message', data: m, timestamp: ts });
                      }
                    });
                  }

                  // Sort by timestamp; on ties, ensure user appears before assistant/other
                  const priority = (item: MergedItem) => {
                    try {
                      if (item.type === 'interaction') {
                        const k = String(item.data?.kind || '').toLowerCase();
                        if (k === 'user') return 1;
                        if (k === 'assistant') return 3;
                        return 2; // section markers, action
                      } else {
                        const r = String(item.data?.role || '').toLowerCase();
                        return r === 'user' ? 1 : 3;
                      }
                    } catch { return 2; }
                  };
                  merged.sort((a, b) => {
                    if (a.timestamp !== b.timestamp) return a.timestamp - b.timestamp;
                    return priority(a) - priority(b);
                  });

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
                      footer={isUser && genaiChat.preStanceLatency && genaiChat.preStanceLatency[m.id] ? (
                        <span className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                          stance: {(genaiChat.preStanceLatency[m.id] / 1000).toFixed(2)}s
                        </span>
                      ) : undefined}
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
                              ? 'The model is updating its analysis with up‑to‑date information from web search, which may take additional time.'
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
                          {/* Tags row: per-message chips (stable by messageId) with global fallback */}
                          {(() => {
                            try {
                              const store = useGenaiChatStore.getState() as any;
                              let inter = store.interactions.find((it: any) => it.kind === 'assistant' && (it as any).originalMessageId === m.id) as any;
                              if (!inter) {
                                const map: Record<string, string> = store.persistedByLocalId || {};
                                const serverId = map[m.id];
                                if (serverId) {
                                  inter = store.interactions.find((it: any) => it.id === serverId);
                                }
                              }
                              const perQ = Array.isArray(inter?.searchQueries) ? inter.searchQueries.filter(Boolean) : [];
                              const perS = Array.isArray(inter?.sources) ? inter.sources.filter(Boolean) : [];
                              const qs = perQ.slice(0, 4);
                              const attrs = perS.length > 0 ? perS.map((url: string) => ({ url })).slice(0, 4) : [];
                              if (qs.length === 0 && attrs.length === 0) return null;
                              return (
                                <div className="mt-1 flex flex-wrap gap-1">
                                  {qs.map((q: string, i: number) => {
                                    const chipId = `${m.id}-q-${i}`;
                                    const isExpanded = expandedChip === chipId;
                                    const words = String(q || '').trim().split(/\s+/);
                                    const short = words.length > 3 ? words.slice(0, 3).join(' ') + '…' : String(q || '');
                                    const needsExpand = words.length > 3;
                                    return (
                                      <button
                                        key={`q-${i}`}
                                        type="button"
                                        className="px-2 py-0.5 rounded-full text-[10px] transition-colors"
                                        style={{ backgroundColor: isExpanded ? 'var(--white-20)' : 'var(--white-10)', color: 'var(--text-secondary-strong)', border: 'none', cursor: needsExpand ? 'pointer' : 'default' }}
                                        onClick={() => needsExpand && setExpandedChip(isExpanded ? null : chipId)}
                                        onMouseEnter={(e) => { if (needsExpand) (e.currentTarget as HTMLButtonElement).style.backgroundColor = isExpanded ? 'var(--white-25)' : 'var(--white-15)'; }}
                                        onMouseLeave={(e) => { if (needsExpand) (e.currentTarget as HTMLButtonElement).style.backgroundColor = isExpanded ? 'var(--white-20)' : 'var(--white-10)'; }}
                                      >
                                        {isExpanded ? String(q || '') : short}
                                      </button>
                                    );
                                  })}
                                  {attrs.map((a: any, i: number) => {
                                    const chipId = `${m.id}-src-${i}`;
                                    const isExpanded = expandedChip === chipId;
                                    const raw = String((a as any).url || '').replace(/^https?:\/\//i, '');
                                    const parts = raw.split('/');
                                    const host = parts[0] || '';
                                    const tail = parts[1] ? `/${parts[1]}` : '';
                                    const label = (host + tail) || (a as any).title || 'link';
                                    const needsExpand = raw.length > (host + tail).length;
                                    return (
                                      <a
                                        key={`src-${i}`}
                                        href={(a as any).url}
                                        target="_blank"
                                        rel="noreferrer"
                                        className="px-2 py-0.5 rounded-full text-[10px] underline transition-colors"
                                        style={{ backgroundColor: isExpanded ? 'var(--white-20)' : 'var(--white-10)', color: 'var(--text-secondary-strong)' }}
                                        onClick={(e) => { if (needsExpand) { e.preventDefault(); setExpandedChip(isExpanded ? null : chipId); } }}
                                        onMouseEnter={(e) => { (e.currentTarget as HTMLAnchorElement).style.backgroundColor = isExpanded ? 'var(--white-25)' : 'var(--white-15)'; }}
                                        onMouseLeave={(e) => { (e.currentTarget as HTMLAnchorElement).style.backgroundColor = isExpanded ? 'var(--white-20)' : 'var(--white-10)'; }}
                                      >
                                        {isExpanded ? raw : label}
                                      </a>
                                    );
                                  })}
                                </div>
                              );
                            } catch { return null; }
                          })()}
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
                      
                      // Special rendering for action interactions
                      if (it.kind === 'action') {
                        // Respect collapsed sections
                        try {
                          const sid = (it as any).sectionId as string | undefined;
                          if (sid && collapsedSections[sid]) return null;
                        } catch {}
                        return (
                          <div key={it.id} className="my-2 p-3 rounded" style={{ 
                            background: 'var(--accent-research-10)', 
                            color: 'var(--accent-research)' 
                          }}>
                            <div className="text-12 font-medium">Executing command:</div>
                            <div className="text-12 font-mono mt-1">{it.content}</div>
                          </div>
                        );
                      }
                      
                      const isUser = it.kind === 'user';
                      // Skip rendering if inside a collapsed section
                      try {
                        const sid = (it as any).sectionId as string | undefined;
                        if (sid && collapsedSections[sid]) return null;
                      } catch {}
                      
                      // Special rendering for action.bring_in indicator messages
                      if (isUser && it.subkind === 'action.bring_in') {
                        return (
                          <div key={it.id} className="my-1.5 flex justify-end">
                            <div className="px-3 py-1.5 rounded-lg text-[12px] inline-flex items-center gap-2" style={{ 
                              background: 'var(--accent-research-10)', 
                              color: 'var(--accent-research)',
                              border: '1px solid var(--accent-research-20)',
                              maxWidth: '80%'
                            }}>
                              <svg className="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                                <path d="M12 7a3 3 0 1 1 0 6a3 3 0 0 1 0-6" strokeWidth="2" />
                                <path d="M5 20c0-3.314 3.134-6 7-6s7 2.686 7 6" strokeWidth="2" strokeLinecap="round" />
                                <path d="M18 7v4M16 9h4" strokeWidth="2" strokeLinecap="round" />
                              </svg>
                              <span>{it.content}</span>
                            </div>
                          </div>
                        );
                      }
                      
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
                            <>
                              {/* Graph message rendering */}
                              {(() => {
                                let gd = it.graphData as undefined | { chartTitle: string; chartType?: string; table: (string | number)[][]; sources: string[] };
                                if (!gd && it.subkind === 'graph') {
                                  try {
                                    let content = String(it.contentSanitized || it.content || '').trim();
                                    content = content.replace(/^```[a-zA-Z]*\n([\s\S]*?)\n```$/m, '$1').trim();
                                    let parsed: any = null;
                                    if (content.startsWith('{') || content.startsWith('[')) {
                                      parsed = JSON.parse(content);
                                    } else {
                                      const m = content.match(/\{[\s\S]*\}/);
                                      if (m && m[0]) parsed = JSON.parse(m[0]);
                                    }
                                    if (parsed && typeof parsed === 'object' && typeof parsed.chartTitle === 'string' && Array.isArray(parsed.table) && Array.isArray(parsed.sources)) {
                                      const allowedTypes = ['line', 'bar', 'area', 'stackedBar', 'stackedArea', 'composed'];
                                      const chartType = typeof parsed.chartType === 'string' && allowedTypes.includes(parsed.chartType) ? parsed.chartType : 'line';
                                      gd = { chartTitle: parsed.chartTitle, chartType: chartType, table: parsed.table, sources: parsed.sources };
                                    }
                                  } catch {}
                                }
                                if (gd) {
                                  return (
                                    <div data-interaction-id={it.id}>
                                      <ChatGraphMessage
                                        chartTitle={gd.chartTitle}
                                        chartType={gd.chartType || 'line'}
                                        table={gd.table}
                                        sources={gd.sources}
                                      />
                                    </div>
                                  );
                                }
                                return (
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
                                            const preferredLang = getPreferredLanguage();
                                            const settings = useGenAISettings.getState();
                                            const useModel = prof?.model || settings.model;
                                            const sxSystemInstruction = getSxSystemInstructionShort();
                                            const options: any = {
                                              profile: /_quality$/i.test(useModel || '') ? 'quality' : 'fast',
                                              modelOverride: useModel,
                                              groundingEnabled: settings.groundingEnabled,
                                              temperature: settings.temperature,
                                              maxOutputTokens: Math.max(512, settings.maxOutputTokens || 1024),
                                              systemInstruction: [settings.systemInstruction || '', HATL_ONE_INSTRUCTIONS, sxSystemInstruction].filter(Boolean).join('\n\n').trim() || undefined,
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
                              ); })()}
                              {/* End of graph/markdown conditional rendering */}
                              {/* Tags row: per-interaction chips for assistant - BEFORE ActionRow/EvaluationDetails */}
                              {!isUser && (() => {
                                try {
                                  const perQ = Array.isArray((it as any).searchQueries) ? (it as any).searchQueries.filter(Boolean) : [];
                                  const perS = Array.isArray((it as any).sources) ? (it as any).sources.filter(Boolean) : [];
                                  const qs = perQ.slice(0, 4);
                                  const attrs = perS.length > 0 ? perS.map((url: string) => ({ url })).slice(0, 4) : [];
                                  if (qs.length === 0 && attrs.length === 0) return null;
                                  return (
                                    <div className="mt-1">
                                      <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>Sources:</div>
                                      <div className="flex flex-wrap gap-1">
                                      {qs.map((q: string, i: number) => {
                                        const chipId = `${it.id}-q-${i}`;
                                        const isExpanded = expandedChip === chipId;
                                        const words = String(q || '').trim().split(/\s+/);
                                        const short = words.length > 3 ? words.slice(0, 3).join(' ') + '…' : String(q || '');
                                        const needsExpand = words.length > 3;
                                        return (
                                          <button
                                            key={`q-${i}`}
                                            type="button"
                                            className="px-2 py-0.5 rounded-full text-[10px] transition-colors"
                                            style={{ backgroundColor: isExpanded ? 'var(--white-20)' : 'var(--white-10)', color: 'var(--text-secondary-strong)', border: 'none', cursor: needsExpand ? 'pointer' : 'default' }}
                                            onClick={() => needsExpand && setExpandedChip(isExpanded ? null : chipId)}
                                            onMouseEnter={(e) => { if (needsExpand) (e.currentTarget as HTMLButtonElement).style.backgroundColor = isExpanded ? 'var(--white-25)' : 'var(--white-15)'; }}
                                            onMouseLeave={(e) => { if (needsExpand) (e.currentTarget as HTMLButtonElement).style.backgroundColor = isExpanded ? 'var(--white-20)' : 'var(--white-10)'; }}
                                          >
                                            {isExpanded ? String(q || '') : short}
                                          </button>
                                        );
                                      })}
                                      {attrs.map((a: any, i: number) => {
                                        const chipId = `${it.id}-src-${i}`;
                                        const isExpanded = expandedChip === chipId;
                                        const raw = String((a as any).url || '').replace(/^https?:\/\//i, '');
                                        const parts = raw.split('/');
                                        const host = parts[0] || '';
                                        const tail = parts[1] ? `/${parts[1]}` : '';
                                        const label = (host + tail) || (a as any).title || 'link';
                                        const needsExpand = raw.length > (host + tail).length;
                                        return (
                                          <a
                                            key={`src-${i}`}
                                            href={(a as any).url}
                                            target="_blank"
                                            rel="noreferrer"
                                            className="px-2 py-0.5 rounded-full text-[10px] underline transition-colors"
                                            style={{ backgroundColor: isExpanded ? 'var(--white-20)' : 'var(--white-10)', color: 'var(--text-secondary-strong)' }}
                                            onClick={(e) => { if (needsExpand) { e.preventDefault(); setExpandedChip(isExpanded ? null : chipId); } }}
                                            onMouseEnter={(e) => { (e.currentTarget as HTMLAnchorElement).style.backgroundColor = isExpanded ? 'var(--white-25)' : 'var(--white-15)'; }}
                                            onMouseLeave={(e) => { (e.currentTarget as HTMLAnchorElement).style.backgroundColor = isExpanded ? 'var(--white-20)' : 'var(--white-10)'; }}
                                          >
                                            {isExpanded ? raw : label}
                                          </a>
                                        );
                                      })}
                                      </div>
                                    </div>
                                  );
                                } catch { return null; }
                              })()}
                              {!isUser && (
                                <ActionRow messageId={it.id} isAssistant={!isUser} content={(it.contentSanitized || it.content)} />
                              )}
                            </>
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
                    if (!m || m.role !== 'assistant') return null;
                    // Don't show placeholder if message has content (streaming has started producing content)
                    if (m.content && m.content.trim()) return null;
                    // Don't show placeholder if this message is already in interactions (streaming completed)
                    // Check by originalMessageId which is set when interaction is created from message
                    const alreadyInInteractions = genaiChat.interactions.some((it) => 
                      it.kind === 'assistant' && (
                        it.id === m.id || 
                        (it as any).originalMessageId === m.id
                      )
                    );
                    if (alreadyInInteractions) {
                      console.log('[ThinkingPlaceholder] Hiding placeholder - already in interactions', { messageId: m.id });
                      return null;
                    }
                    const header = (genaiChat.messageMeta?.[m.id]?.headerText || 'assistant');
                    const leftAccent = !!genaiChat.messageMeta?.[m.id]?.leftAccent;
                    const isQuality = genaiSettings.strength === 'quality' || /_quality$/.test(genaiSettings.model || '');
                    const threshold = genaiSettings.groundingEnabled ? 35 : 20;
                    const showInfo = (genaiThinkingSeconds || 0) >= threshold && isQuality;
                    const infoMsg = genaiSettings.groundingEnabled
                      ? 'The model is updating its analysis with up‑to‑date information from web search, which may take additional time.'
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
                        averageTimingSeconds={averageTimingSeconds}
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
                );
              })()}
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
            {!genaiChat.streaming && ((genaiChat.usage || genaiChat.attributions.length > 0) || (() => {
              try {
                const map = (genaiChat as any).preStanceLatency || {} as Record<string, number>;
                const msgs = (genaiChat as any).messages || [];
                for (let i = msgs.length - 1; i >= 0; i--) {
                  const id = msgs[i]?.id;
                  const ms = map[id];
                  if (typeof ms === 'number' && ms > 0) return true;
                }
              } catch {}
              return false;
            })()) && (
              <div className="px-1 pb-2 space-y-1">
                {(() => {
                  try {
                    const map = (genaiChat as any).preStanceLatency || {} as Record<string, number>;
                    const msgs = (genaiChat as any).messages || [];
                    let stanceMs = 0;
                    for (let i = msgs.length - 1; i >= 0; i--) {
                      const id = msgs[i]?.id;
                      const ms = map[id];
                      if (typeof ms === 'number' && ms > 0) { stanceMs = ms; break; }
                    }
                    const u = (genaiChat as any).usage;
                    const hasUsage = !!u;
                    if (!hasUsage && stanceMs <= 0) return null;
                    const usageText = hasUsage ? `Tokens: prompt ${u?.promptTokens ?? 0}, output ${u?.outputTokens ?? 0}, total ${u?.totalTokens ?? 0}` : '';
                    const stanceText = stanceMs > 0 ? `${hasUsage ? ' · ' : ''}stance: ${(stanceMs / 1000).toFixed(2)}s` : '';
                    return (
                      <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                        {usageText}{stanceText}
                      </div>
                    );
                  } catch { return null; }
                })()}
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
            {/* Analysis status banner unified in UnfoldRequest; duplicate removed here */}
            {/* Input */}
            {unfoldSubView !== 'perspectives' && (
            <UnfoldRequest
              dangerTint={isHighRisk}
              showHighRiskTag={isHighRisk}
              onHighRiskTagClick={() => setRiskBanner((prev) => ({ visible: true, level: 'High', rationale: prev?.rationale || '' }))}
              onModeChange={setUnfoldMode}
              onSubmit={async (text) => {
                if (genaiChat.streaming || !text.trim()) return;
                let prompt = text.trim();
                // Slash commands: /section <title> and /end
                const raw = prompt;
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
                    try {
                      if ((window as any).wsSubscribeProject && pid) {
                        (window as any).wsSubscribeProject(pid);
                        const waitForWsSubscribed = async (projectId: string, timeoutMs = 1500) => {
                          const start = Date.now();
                          // eslint-disable-next-line no-constant-condition
                          while (true) {
                            try {
                              const ws = (await import('../stores/wsStore')).useWsStore.getState();
                              if (ws.connected && ws.subscribedProjectId === projectId) break;
                            } catch {}
                            if (Date.now() - start > timeoutMs) break;
                            await new Promise((r) => setTimeout(r, 60));
                          }
                        };
                        await waitForWsSubscribed(pid);
                      }
                    } catch {}
                    const prompt = raw.replace(/^\s*\/analyze\s+entities\b\s*/i, '') + "\n\nConstraints:\n- Only include concrete, named entities (no abstract categories).\n- Allowed types: person | group_of_persons | organization_or_platform | project_or_product | classifier_or_standard | classification_code | regulatory_document | government_agency | contract_document | location | other.\n- Reject generic words like 'technical regulation', 'open databases', 'contracts' without identifiers.";
                    const res = await api.analyzeEntities(pid, prompt);
                    try { console.log('[AnalyzeEntities] queued:', res); } catch {}
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
                // Chat language preference (used in hidden system instructions)
                const preferredLang = getPreferredLanguage();
                const langNameForPrompt = getLanguageName(preferredLang);
                const sxSystemInstruction = getSxSystemInstructionFull();
                const graphSystemInstruction = unfoldMode === 'graph'
                  ? (
                    `=== GRAPH MODE ACTIVE ===\n` +
                    `You MUST respond with ONLY a complete valid JSON object. Start with { and end with }. No text, no markdown, no explanations.\n` +
                    `\n` +
                    `Required JSON structure:\n` +
                    `{\n` +
                    `  "chartTitle": "string",\n` +
                    `  "chartType": "line" | "bar" | "area" | "stackedBar" | "stackedArea" | "composed",\n` +
                    `  "table": [\n` +
                    `    ["", "Year1", "Year2", "Year3"],\n` +
                    `    ["Label1", num1, num2, num3],\n` +
                    `    ["Label2", num4, num5, num6]\n` +
                    `  ],\n` +
                    `  "sources": ["url1", "url2"]\n` +
                    `}\n` +
                    `\n` +
                    `Chart type dictionary (chartType field):\n` +
                    `- "line": Line chart - best for showing trends over time, comparing multiple series over continuous intervals\n` +
                    `- "bar": Bar chart - best for comparing discrete categories, showing relative magnitudes\n` +
                    `- "area": Area chart - best for showing cumulative values over time, emphasizing volume\n` +
                    `- "stackedBar": Stacked bar chart - best for showing composition of totals across categories, part-to-whole relationships\n` +
                    `- "stackedArea": Stacked area chart - best for showing cumulative contributions of multiple series over time\n` +
                    `- "composed": Composed chart - best for combining different chart types (e.g., bars with lines) to show multiple perspectives\n` +
                    `\n` +
                    `Chart type selection:\n` +
                    `- YOU MUST analyze the user's query and recommend the MOST APPROPRIATE chart type from the dictionary above\n` +
                    `- Choose based on: what the user wants to show (trends vs comparisons vs compositions), data characteristics (time series vs categories), and visualization goals\n` +
                    `- Default to "line" if uncertain, but prefer the most suitable type for the specific use case\n` +
                    `- Set chartType to one of the dictionary values\n` +
                    `\n` +
                    `Table structure rules (depend on chartType):\n` +
                    `- Common: table is a 2D array; table[0] is header row; first cell is empty string ""; other cells in header are X-axis labels;\n` +
                    `- For chartType in ["line", "area", "stackedArea"]: X-axis MUST be a time axis (years/dates). Header cells after the first are time periods in ascending order.\n` +
                    `- For chartType in ["bar", "stackedBar", "composed"]: X-axis MAY be time (as above) OR categorical (product/country/etc.) depending on the user request.\n` +
                    `- Series rows (table[1..]) always: first cell is series label (country/entity/etc.), the rest are numeric values (int or double) for each X label.\n` +
                    `- No text in numeric cells; use numbers only.\n` +
                    `\n` +
                    `Sources requirements:\n` +
                    `- You MUST provide a list of sources (URLs) where the data comes from\n` +
                    `- Include actual URLs of websites, databases, reports, or official sources you used or would reference\n` +
                    `- If using web search, include the URLs from search results\n` +
                    `- If specific data sources are known, include them (e.g., "https://data.worldbank.org/indicator/...", "https://ec.europa.eu/eurostat/...")\n` +
                    `- sources must be a non-empty array of strings\n` +
                    `\n` +
                    `CRITICAL:\n` +
                    `- Response MUST be a complete JSON object starting with { and ending with }\n` +
                    `- MUST include all four fields: chartTitle, chartType, table, sources\n` +
                    `- chartTitle: title in ${langNameForPrompt}\n` +
                    `- chartType: MUST be one of the dictionary values: "line", "bar", "area", "stackedBar", "stackedArea", "composed"\n` +
                    `- table: 2D array as described above\n` +
                    `- sources: array of source URLs (REQUIRED - provide actual URLs)\n` +
                    `- NO markdown code fences (no \`\`\`json)\n` +
                    `- NO text before or after the JSON\n` +
                    `\n` +
                    `Example for R&D investments USA vs EU:\n` +
                    `{"chartTitle":"R&D Investment Trends (% of GDP)","chartType":"line","table":[["","2013","2018","2023"],["USA",2.8,2.82,3.4],["EU",2.08,2.18,2.26]],"sources":["https://ec.europa.eu/eurostat/statistics-explained/index.php?title=R%26D_expenditure","https://data.worldbank.org/indicator/GB.XPD.RSDV.GD.ZS"]}`
                  )
                  : '';
                // In graph mode, prioritize graph instruction by putting it first
                const combinedSystemInstruction = unfoldMode === 'graph'
                  ? [
                      graphSystemInstruction,
                  genaiSettings.systemInstruction || '',
                  HATL_ONE_INSTRUCTIONS,
                  sxSystemInstruction,
                    ].filter(Boolean).join('\n\n').trim() || undefined
                  : [
                      genaiSettings.systemInstruction || '',
                      HATL_ONE_INSTRUCTIONS,
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
                      const preferredLang = getPreferredLanguage();
                      
                      // Call unified evaluation function
                      await evaluateBias({
                        messageId: lastUser.id,
                        messageRole: 'user',
                        textToEvaluate,
                        contextText: prevAssistantText || undefined,
                        evalModel,
                        preferredLang,
                      });
                    } catch (e) {
                      console.warn('[UserStanceEval] User bias eval failed', e);
                    }
                  })().catch(console.error);
                }
              } catch {}
              }}
              disabled={genaiChat.streaming || !queryText.trim()}
            />
            )}
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
              {/* Skeleton loading during hydration */}
              {genaiChat.hydrating && interactions.length === 0 && (
                <div className="space-y-2 py-2">
                  <div style={{ height: 14, borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                    <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                  </div>
                  <div style={{ height: 10, width: '90%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                    <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                  </div>
                  <div style={{ height: 10, width: '80%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                    <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                  </div>
                  <div style={{ height: 10, width: '70%', borderRadius: 4, background: 'var(--white-06)', position: 'relative', overflow: 'hidden' }}>
                    <div style={{ position: 'absolute', inset: 0, background: 'linear-gradient(90deg, transparent, var(--white-20), transparent)', animation: 'etg-shimmer 2s infinite' }} />
                  </div>
                </div>
              )}
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
                const lang = getPreferredLanguage();
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
        {/* Right column: container for contextual panels (transparent, no border) - hidden when Account or Settings are shown */}
        {viewMode !== 'graph' && !showAccount && !showSettings && (
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
                    {rightArtifactsOpen ? 'Workspace Artifacts' : rightContextOpen ? 'Context Navigation' : 'Workspace Library'}
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
                    <FindingsPanel projectId={genaiChat.projectId} />
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
                                          const container = document.querySelector(`.markdown[data-interaction-id="${it.id}"]`) as HTMLElement | null;
                                          if (container) {
                                            // Normalize note text (trim whitespace for better matching)
                                            const noteText = String(note).trim();
                                            const r = noteText ? findFirstRange(container, noteText) : null;
                                            if (r) {
                                              const rect = r.getBoundingClientRect();
                                              const host = messagesRef.current as HTMLElement | null;
                                              if (host) {
                                                const hostRect = host.getBoundingClientRect();
                                                const offset = rect.top - hostRect.top + host.scrollTop - host.clientHeight / 2;
                                                host.scrollTo({ top: Math.max(0, offset), behavior: 'smooth' });
                                              } else {
                                                container.scrollIntoView({ behavior: 'smooth', block: 'center' });
                                              }
                                            } else {
                                              // Fallback: scroll to the message itself if note text not found
                                              if (messagesRef.current) {
                                                const rect = container.getBoundingClientRect();
                                                const host = messagesRef.current;
                                                const hostRect = host.getBoundingClientRect();
                                                const offset = rect.top - hostRect.top + host.scrollTop - host.clientHeight / 2;
                                                host.scrollTo({ top: Math.max(0, offset), behavior: 'smooth' });
                                              } else {
                                                container.scrollIntoView({ behavior: 'smooth', block: 'center' });
                                              }
                                            }
                                          } else {
                                            // If container not found, try using scrollToInteraction as fallback
                                            scrollToInteraction(it.id);
                                          }
                                        } catch (err) {
                                          console.warn('[ScrollToNote] Failed', err);
                                          // Fallback to scrolling to interaction
                                          try {
                                            scrollToInteraction(it.id);
                                          } catch {}
                                        }
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
                                          const projectId = genaiChat.projectId;
                                          if (!projectId || projectId === 'local-unfold') {
                                            console.warn('[RemoveNote] Invalid project ID', { projectId });
                                            return;
                                          }
                                          // Resolve server ID if local ID is mapped
                                          const map = (genaiChat as any).persistedByLocalId || {};
                                          const serverId = map[it.id] || it.id;
                                          const notes = (it.metadata?.notes || []).filter((_, i) => i !== idx);
                                          const pinned = it.metadata?.pinned === true || notes.length > 0; // keep pinned if still notes exist
                                          await api.updateProjectInteraction(projectId, serverId, { metadata: { pinned, notes } });
                                          await genaiChat.hydrateInteractions();
                                        } catch (err) {
                                          console.warn('[RemoveNote] failed', err);
                                        }
                                      }}
                                      onMouseEnter={(ev) => { (ev.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-12)'; }}
                                      onMouseLeave={(ev) => { (ev.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent'; }}
                                    >
                                      <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path strokeLinecap="round" strokeWidth={2} d="M6 6l12 12M18 6L6 18" /></svg>
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
                    <WorkspaceLibrary 
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
                    <div style={{ display: 'flex', justifyContent: 'flex-start', gap: 6 }}>
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
                          try {
                            if ((window as any).wsSubscribeProject && pid) {
                              (window as any).wsSubscribeProject(pid);
                              const waitForWsSubscribed = async (projectId: string, timeoutMs = 1500) => {
                                const start = Date.now();
                                // eslint-disable-next-line no-constant-condition
                                while (true) {
                                  try {
                                    const ws = (await import('../stores/wsStore')).useWsStore.getState();
                                    if (ws.connected && ws.subscribedProjectId === projectId) break;
                                  } catch {}
                                  if (Date.now() - start > timeoutMs) break;
                                  await new Promise((r) => setTimeout(r, 60));
                                }
                              };
                              await waitForWsSubscribed(pid);
                            }
                          } catch {}
                          const res = await (await import('../lib/api')).api.analyzeEntities(pid, '');
                          try { console.log('[AnalyzeEntities] queued:', res); } catch {}
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
                      onClick={async () => { try { const pid = await genaiChat.ensureProject(); try { if ((window as any).wsSubscribeProject && pid) { (window as any).wsSubscribeProject(pid); const ws = await import('../stores/wsStore'); const wait = async (projectId: string, timeoutMs = 1500) => { const start = Date.now(); while (true) { try { const st = ws.useWsStore.getState(); if (st.connected && st.subscribedProjectId === projectId) break; } catch {} if (Date.now() - start > timeoutMs) break; await new Promise((r)=>setTimeout(r,60)); } }; await wait(pid); } } catch {} const res = await (await import('../lib/api')).api.analyzeFacts(pid, ''); try { console.log('[AnalyzeFacts] queued:', res); } catch {} } catch {} }}
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
                      onClick={async () => { try { const pid = await genaiChat.ensureProject(); try { if ((window as any).wsSubscribeProject && pid) { (window as any).wsSubscribeProject(pid); const ws = await import('../stores/wsStore'); const wait = async (projectId: string, timeoutMs = 1500) => { const start = Date.now(); while (true) { try { const st = ws.useWsStore.getState(); if (st.connected && st.subscribedProjectId === projectId) break; } catch {} if (Date.now() - start > timeoutMs) break; await new Promise((r)=>setTimeout(r,60)); } }; await wait(pid); } } catch {} const res = await (await import('../lib/api')).api.analyzeEvents(pid, ''); try { console.log('[AnalyzeEvents] queued:', res); } catch {} } catch {} }}
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
                      onClick={async () => { try { const pid = await genaiChat.ensureProject(); try { if ((window as any).wsSubscribeProject && pid) { (window as any).wsSubscribeProject(pid); const ws = await import('../stores/wsStore'); const wait = async (projectId: string, timeoutMs = 1500) => { const start = Date.now(); while (true) { try { const st = ws.useWsStore.getState(); if (st.connected && st.subscribedProjectId === projectId) break; } catch {} if (Date.now() - start > timeoutMs) break; await new Promise((r)=>setTimeout(r,60)); } }; await wait(pid); } } catch {} const res = await (await import('../lib/api')).api.analyzeMetrics(pid, ''); try { console.log('[AnalyzeMetrics] queued:', res); } catch {} } catch {} }}
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
                      onClick={async () => { try { const pid = await genaiChat.ensureProject(); try { if ((window as any).wsSubscribeProject && pid) { (window as any).wsSubscribeProject(pid); const ws = await import('../stores/wsStore'); const wait = async (projectId: string, timeoutMs = 1500) => { const start = Date.now(); while (true) { try { const st = ws.useWsStore.getState(); if (st.connected && st.subscribedProjectId === projectId) break; } catch {} if (Date.now() - start > timeoutMs) break; await new Promise((r)=>setTimeout(r,60)); } }; await wait(pid); } } catch {} const res = await (await import('../lib/api')).api.analyzeDecisions(pid, ''); try { console.log('[AnalyzeDecisions] queued:', res); } catch {} } catch {} }}
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
                
                {/* Workspace Library toolbar (bottom) */}
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