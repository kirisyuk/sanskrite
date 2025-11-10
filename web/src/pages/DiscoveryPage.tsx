import React, { useState, useEffect, useRef, startTransition } from 'react';
import ReactMarkdown, { Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
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
import { HATL_ONE_INSTRUCTIONS } from '../lib/hatlInstructions';
import DocumentViewer from '../components/DocumentViewer';
import FindingsPanel from '../components/FindingsPanel';
import ChatGraphMessage from '../components/ChatGraphMessage';
import { evaluateBias } from '../lib/biasEvaluation';
import {
  EntitiesList,
  FactsList,
  EventsList,
  MetricsList,
  DecisionsList,
} from '../components/ArtifactLists';
import { ThemeContext } from '../components/ThemeProvider';
import { EvaluationDetails } from '../components/EvaluationDetails';
import { ActionRow } from '../components/ActionRow';
import { UserPositionAnalysis } from '../components/UserPositionAnalysis';
import { LLMEvaluation } from '../components/LLMEvaluation';
import { DiscussionFlow } from '../components/DiscussionFlow';
import { TextTasksPanel, TextTask } from '../components/TextTasksPanel';
import { ExperimentalResponsePanel } from '../components/ExperimentalResponsePanel';
import {
  generateExperimentalResponse,
  ExperimentalResponse,
} from '../lib/experimentalResponse';
// TitleSuggestions removed - title generation now handled automatically in genaiChatStore.onDone
import {
  toggleInteractionPin,
  isInteractionPinned,
  findEvalBiasInteraction,
} from '../lib/interactionHelpers';
import {
  highlightNotes,
  clearHighlights,
  findFirstRange,
  applyGlobalHighlight,
  clearGlobalHighlight,
} from '../lib/textHighlight';
import {
  getPreferredLanguage,
  getLanguageName,
  getAnchorRect,
  getSxSystemInstructionShort,
  getSxSystemInstructionFull,
} from '../lib/uiUtils';
import { CollapsibleText } from '../components/CollapsibleText';
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
  formatPercent,
} from '../lib/metaInterpretations';
import { SidebarPanel } from '../components/SidebarPanel';
import { ArtifactsPanelToolbar } from '../components/ArtifactsPanelToolbar';
import { ContextPanel } from '../components/ContextPanel';
import { WorkspacePanel } from '../components/WorkspacePanel';
import { AnalysisPanel } from '../components/AnalysisPanel';
import { ExperimentalPanel } from '../components/ExperimentalPanel';
import { useSourcesManagement } from '../hooks/useSourcesManagement';
import { useProjectsManagement } from '../hooks/useProjectsManagement';
import { usePanelsManagement } from '../hooks/usePanelsManagement';
import { useAnalysisManagement } from '../hooks/useAnalysisManagement';
import { useChatManagement } from '../hooks/useChatManagement';
import { useArtifactsAnalysis } from '../hooks/useArtifactsAnalysis';
import { useTextEditing } from '../hooks/useTextEditing';
import { useModals } from '../hooks/useModals';
import { useChatTimers } from '../hooks/useChatTimers';
import { useAdvancedAutoScroll } from '../hooks/useAutoScroll';
import { useWebSocketConnection } from '../hooks/useWebSocketConnection';
import { RightPanelButtons } from '../components/RightPanelButtons';
import { MetricOverlay } from '../components/MetricOverlay';
import { MessageActions } from '../components/MessageActions';
import { useMergedMessages } from '../hooks/useMergedMessages';
import { SectionMarker } from '../components/SectionMarker';
import { ActionInteraction } from '../components/ActionInteraction';
import { BringInIndicator } from '../components/BringInIndicator';
import { parseGraphData } from '../lib/graphDataParser';
import { applyEditHighlight } from '../lib/editHighlight';
import { replaceTextInMarkdown } from '../lib/markdownNormalizer';
import { MarkdownRenderer, markdownComponents } from '../lib/markdownComponents';
import { OpponentAgentsPalette } from '../components/OpponentAgentsPalette';
import { MessageItem } from '../components/MessageItem';
import { useTextSelection } from '../hooks/useTextSelection';
import { handleEditTextDirect } from '../lib/textEditHandler';
import { FloatingActionButtons } from '../components/FloatingActionButtons';
import { EditStatusIndicator } from '../components/EditStatusIndicator';
import { ToolCallsDisplay } from '../components/ToolCallsDisplay';
import { AttributionsDisplay } from '../components/AttributionsDisplay';
import { RiskBanner } from '../components/RiskBanner';
import { ThinkingPlaceholder } from '../components/ThinkingPlaceholder';
import { SettingsModal } from '../components/SettingsModal';
import { AccountModal } from '../components/AccountModal';
import { AdminModal } from '../components/AdminModal';
import { TelegramAddModal } from '../components/TelegramAddModal';

const DiscoveryPage: React.FC = () => {
  const { t, i18n } = useTranslation();
  const { currentUser } = useAuth();
  const { theme } = React.useContext(ThemeContext);
  const projects = useProjectsStore((s) => s.projects);
  const fetchProjects = useProjectsStore((s) => s.fetchProjects);
  const setCurrentProject = useProjectsStore((s) => s.setCurrentProject);
  
  // Custom hooks for managing different aspects of the page
  const sourcesManagement = useSourcesManagement();
  const projectsManagement = useProjectsManagement();
  const panelsManagement = usePanelsManagement();
  const analysisManagement = useAnalysisManagement();
  const chatManagement = useChatManagement();
  const artifactsAnalysis = useArtifactsAnalysis();
  const textEditing = useTextEditing();
  const modals = useModals();
  const chatTimers = useChatTimers();
  
  // Initialize WebSocket connection
  useWebSocketConnection();
  
  // Destructure for easier access
  const {
    previewSource,
    setPreviewSource,
    showAddSourceMenu,
    setShowAddSourceMenu,
    addMenuRef,
    showAddTelegramModal,
    setShowAddTelegramModal,
    tgInput,
    setTgInput,
    tgSubmitting,
    tgError,
    addTelegramSource,
    parseSourcesFromQuery,
  } = sourcesManagement;
  
  const {
    editingProjectId,
    setEditingProjectId,
    editingProjectName,
    setEditingProjectName,
    editingInputRef,
    resetChatState,
    handleDeleteChat,
    handleRenameChat,
    startEditingProject,
  } = projectsManagement;
  
  const {
    rightPanelType,
    setRightPanelType,
    leftPanel,
    setLeftPanel,
    analysisTab,
    setAnalysisTab,
    drawerWidth,
    setDrawerWidth,
    closeAllRightPanels,
    getPanelVisibilityStyles,
  } = panelsManagement;
  
  const currentProject = useProjectsStore((s) => s.currentProject);
  const deleteProject = useProjectsStore((s) => s.deleteProject);
  const updateProject = useProjectsStore((s) => s.updateProject);
  const {
    sources,
    loading,
    executeDiscovery,
    fetchProjectSources,
    fetchProjectInteractions,
    clearSources,
    connectWs,
    answer,
    correlationId,
    interactions,
    sendChatMessage,
  } = useDiscoveryStore();
  const { viewMode } = useUIStore();
  
  // Get random empty state message (selected once per language)
  const emptyStateMessage = React.useMemo(() => {
    const emptyMessages = t('unfold.perspectives.chatEmptyState.descriptions', {
      returnObjects: true,
    }) as string[];
    if (Array.isArray(emptyMessages) && emptyMessages.length > 0) {
      return emptyMessages[Math.floor(Math.random() * emptyMessages.length)];
    }
    return emptyMessages?.[0] || 'Ready to start a conversation?';
  }, [i18n.language, t]);
  
  const [queryText, setQueryText] = useState('');
  const [sideOpen, setSideOpen] = useState(false);
  const [running, setRunning] = useState(false);
  const [thinkingSeconds, setThinkingSeconds] = useState(0);
  const [entitiesAnalyzing, setEntitiesAnalyzing] = useState(false);
  const [unfoldSubView, setUnfoldSubView] = useState<'chat' | 'perspectives'>(
    'chat',
  );
  const [currentPerspectivePreset, setCurrentPerspectivePreset] =
    useState<string>('consulting');
  const [isAnalysisClosed, setIsAnalysisClosed] = useState(false);
  const [unfoldMode, setUnfoldMode] = React.useState<'text' | 'graph'>('text');
  const unfoldRequestRef = React.useRef<HTMLDivElement | null>(null);
  const messagesRef = useRef<HTMLDivElement | null>(null);
  const discoveryTimeoutRef = useRef<number | null>(null);
  
  // Destructure modals hook
  const {
    showSettings,
    showAccount,
    showAdmin,
    staticPage,
    setStaticPage,
    isAdmin,
    openSettings,
    openAccount,
    openAdmin,
    closeAllModals,
  } = modals;
  
  // Destructure chat timers hook
  const { genaiThinkingSeconds, averageTimingSeconds } = chatTimers;
  const correlationRef = useRef<string | null>(null);
  const lastDocEvent = useDiscoveryStore((s: any) => (s as any).lastDocEvent);
  React.useEffect(() => {
    try {
      if (!lastDocEvent) return;
      const did = String((lastDocEvent as any).docId || '');
      const fname = String((lastDocEvent as any).filename || '');
      const isEntities =
        did === 'entities.json' || /entities\.json$/i.test(fname);

      console.log('[Entities] Doc event received:', {
        docId: did,
        filename: fname,
        isEntities,
        status: (lastDocEvent as any).status,
        fullEvent: lastDocEvent,
      });
      
      if (!isEntities) return;
      const st = String((lastDocEvent as any).status || '');
      
      console.log(
        '[Entities] Status update:',
        st,
        '-> entitiesAnalyzing:',
        st === 'parsing' || st === 'uploaded',
      );
      
      if (st === 'parsing' || st === 'uploaded') setEntitiesAnalyzing(true);
      if (st === 'parsed' || st === 'failed' || st === 'deleted')
        setEntitiesAnalyzing(false);
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
  // GenAI chat state (for Unfold view)
  // PERFORMANCE FIX: Subscribe only to specific values for critical paths, keep genaiChat for compatibility
  const genaiChat = useGenaiChatStore();
  const genaiChatProjectId = useGenaiChatStore((state) => state.projectId);
  const mergedMessages = useMergedMessages();
  
  const genaiChatInteractionsLength = useGenaiChatStore(
    (state) => state.interactions.length,
  );
  const genaiChatMessagesLength = useGenaiChatStore(
    (state) => state.messages.length,
  );
  const genaiChatHydrating = useGenaiChatStore((state) => state.hydrating);
  const genaiChatStreaming = useGenaiChatStore((state) => state.streaming);
  // Track only user interactions count to avoid triggering experimental response on every interaction update
  const userInteractionsCount = useGenaiChatStore(
    (state) =>
      state.interactions.filter((it: any) => it.kind === 'user').length,
  );
  const genaiSettings = useGenAISettings();
  const lastAnalysisStatus = useDiscoveryStore(
    (s: any) => (s as any).lastAnalysisStatus,
  );
  const notifiedAnalysesRef = React.useRef<Set<string>>(new Set());

  // Auto-switch to Chat tab when switching chats/New Chat
  React.useEffect(() => {
    try {
      setUnfoldSubView('chat');
    } catch {}
  }, [genaiChatProjectId]);

  React.useEffect(() => {
    if (genaiChatInteractionsLength === 0 && genaiChatMessagesLength === 0) {
      try {
        setUnfoldSubView('chat');
      } catch {}
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
          const ms =
            typeof st.elapsedMs === 'number' ? st.elapsedMs : undefined;
          const t =
            ms && ms >= 0
              ? ms < 1000
                ? `${Math.round(ms)}ms`
                : `${(ms / 1000).toFixed(1)}s`
              : '';
          const unitOne =
            kind === 'entities'
              ? 'entity'
              : kind === 'facts'
                ? 'fact'
                : kind === 'events'
                  ? 'event'
                  : kind === 'metrics'
                    ? 'metric'
                    : kind === 'decisions'
                      ? 'decision'
                      : kind;
          const unitMany =
            kind === 'entities'
              ? 'entities'
              : kind === 'facts'
                ? 'facts'
                : kind === 'events'
                  ? 'events'
                  : kind === 'metrics'
                    ? 'metrics'
                    : kind === 'decisions'
                      ? 'decisions'
                      : kind;
          const summary =
            n === 0
              ? `Found no ${unitMany}`
              : n === 1
                ? `Found 1 ${unitOne}`
                : `Found ${n} ${unitMany}`;
          const content = t ? `${summary} (${t})` : summary;
          const { api } = await import('../lib/api');
          await api.createProjectInteraction(pid, {
            kind: 'assistant',
            subkind: 'analysis.summary',
            content,
          });
          await genaiChat.hydrateInteractions();
        } catch {}
      })();
    } catch {}
  }, [lastAnalysisStatus, genaiChat]);
  // Collapsible sections state: sectionId -> collapsed
  const [collapsedSections, setCollapsedSections] = useState<
    Record<string, boolean>
  >({});
  // Destructure analysis management
  const {
    showUserAnalysis,
    setShowUserAnalysis,
    showLLMReview,
    setShowLLMReview,
    showDiscussionFlow,
    setShowDiscussionFlow,
    badgeUser,
    setBadgeUser,
    badgeLLM,
    setBadgeLLM,
    badgeFlow,
    setBadgeFlow,
    availUserAnalysis,
    availLLMReview,
    availDiscussionFlow,
    setAvailUserAnalysis,
    setAvailLLMReview,
    setAvailDiscussionFlow,
    riskBanner,
    setRiskBanner,
    isHighRisk,
    setIsHighRisk,
    expandedChip,
    setExpandedChip,
    hasAssistant,
    stanceFromInteractionsTop,
    onFirstResultDiscussionFlow,
  } = analysisManagement;
  
  // Destructure chat management
  const {
    experimentalResponse,
    setExperimentalResponse,
    experimentalLoading,
    setExperimentalLoading,
    textTasks,
    setTextTasks,
    textTasksApplying,
    handleAddTextTask,
    handleRemoveTextTask,
    handleApplyTextTasks,
  } = chatManagement;
  
  // Bottom slot min height to align its top with chat input top
  const [titleSlotMinHeight, setTitleSlotMinHeight] = useState<number>(48);
  const [titlesVisible, setTitlesVisible] = useState(true);
  const [titlesAvailable, setTitlesAvailable] = useState(false);
  const [titlesGlow, setTitlesGlow] = useState(false);
  
  // Detect mobile device
  const [isMobile, setIsMobile] = useState(false);
  useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth <= 768);
    };
    checkMobile();
    window.addEventListener('resize', checkMobile);
    return () => window.removeEventListener('resize', checkMobile);
  }, []);
  
  // Prevent body scroll on mobile when sidebar or drawer is open
  useEffect(() => {
    if (isMobile && (sideOpen || rightPanelType !== null)) {
      document.body.style.overflow = 'hidden';
    } else {
      document.body.style.overflow = '';
    }
    return () => {
      document.body.style.overflow = '';
    };
  }, [isMobile, sideOpen, rightPanelType]);
  
  // Sync drawer width when resized natively
  const drawerRef = useRef<HTMLDivElement>(null);
  const lastUserMessageIdRef = React.useRef<string | null>(null);
  useEffect(() => {
    const drawer = drawerRef.current;
    if (!drawer) return;
    
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const width = entry.contentRect.width;
        if (Math.abs(width - drawerWidth) > 1) {
          // Avoid infinite loops
          setDrawerWidth(width);
        }
      }
    });
    
    observer.observe(drawer);
    return () => observer.disconnect();
  }, [drawerWidth]);
  // Active document viewer state
  const [activeDoc, setActiveDoc] = useState<null | {
    id: string;
    filename: string;
    contentType?: string;
    sizeBytes?: number;
    parseStatus?: string;
  }>(null);
  
  const hasPinned = React.useMemo(() => {
    try {
      return useGenaiChatStore.getState().interactions.some((it: any) => {
        // Exclude perspective interactions (kind='note' with subkind starting with 'perspective.')
        if (
          it.kind === 'note' &&
          typeof it.subkind === 'string' &&
          it.subkind.startsWith('perspective.')
        ) {
          return false;
        }
        // Exclude perspective analysis interactions
        if (
          it.kind === 'system' &&
          typeof it.subkind === 'string' &&
          it.subkind.startsWith('perspective.') &&
          it.subkind.endsWith('.analysis')
        ) {
          return false;
        }
        return !!it?.metadata?.pinned;
      });
    } catch {
      return false;
    }
  }, [genaiChatInteractionsLength, rightPanelType]);
  
  // PERFORMANCE FIX: Use selector to get interactions only when needed, not subscribe to entire array
  const getInteractions = React.useCallback(
    () => useGenaiChatStore.getState().interactions,
    [],
  );
  
  const sectionStarts = React.useMemo(() => {
    try { 
      const interactions = getInteractions();
      return (interactions || []).filter(
        (it: any) => it?.kind === 'section_start',
      );
    } catch {
      return [];
    }
  }, [getInteractions, genaiChatInteractionsLength]);
  const hasSections = sectionStarts.length > 0;
  
  // Auto-switch to documents tab if context tab has no content (removed - context is now separate panel)
  // Ensure interactions are loaded on chat load to detect pinned items
  // Only hydrate once per projectId, not on every store update
  const projectId = useGenaiChatStore((s) => s.projectId);
  const hydrateInteractions = useGenaiChatStore((s) => s.hydrateInteractions);
  const interactionsLoadedRef = React.useRef<string | null>(null);
  
  React.useEffect(() => {
    try {
      if (
        projectId &&
        projectId !== 'local-unfold' &&
        interactionsLoadedRef.current !== projectId
      ) {
        interactionsLoadedRef.current = projectId;
        hydrateInteractions();
      } else if (!projectId || projectId === 'local-unfold') {
        interactionsLoadedRef.current = null;
      }
    } catch {}
  }, [projectId, hydrateInteractions]);


  // Track new user messages and generate experimental response when panel is open
  // FIXED: Only trigger on actual new user messages, not on every interaction update
  React.useEffect(() => {
    if (rightPanelType !== 'experimental') return;
    
    const interactions = getInteractions();
    const userInteractions = interactions.filter(
      (it: any) => it.kind === 'user',
    );
    if (userInteractions.length === 0) return;
    
    const lastUserInteraction = userInteractions[userInteractions.length - 1];
    const messageId = lastUserInteraction.id;
    
    // Skip if already processed or if no content
    if (
      !messageId ||
      messageId === lastUserMessageIdRef.current ||
      !lastUserInteraction.content?.trim()
    ) {
      return;
    }
    
    // Generate experimental response for new user message
    lastUserMessageIdRef.current = messageId;
    setExperimentalLoading(true);
    
    generateExperimentalResponse(lastUserInteraction.content, messageId)
      .then((response) => {
        if (response) {
          setExperimentalResponse(response);
        }
      })
      .catch((error) => {
        console.error('[ExperimentalResponse] Failed to generate', error);
      })
      .finally(() => {
        setExperimentalLoading(false);
      });
  }, [userInteractionsCount, rightPanelType, getInteractions]);

  const pinnedInteractions = React.useMemo(() => {
    try {
      const interactions = getInteractions();
      return (interactions || []).filter((it: any) => {
        // Exclude perspective interactions (kind='note' with subkind starting with 'perspective.')
        if (
          it.kind === 'note' &&
          typeof it.subkind === 'string' &&
          it.subkind.startsWith('perspective.')
        ) {
          return false;
        }
        // Exclude perspective analysis interactions
        if (
          it.kind === 'system' &&
          typeof it.subkind === 'string' &&
          it.subkind.startsWith('perspective.') &&
          it.subkind.endsWith('.analysis')
        ) {
          return false;
        }
        return !!it?.metadata?.pinned;
      });
    } catch {
      return [];
    }
  }, [getInteractions, genaiChatInteractionsLength]);
  const truncateCenterWords = (
    s: string,
    headWords: number = 12,
    tailWords: number = 12,
  ) => {
    try {
      const t = (s || '').toString().replace(/\s+/g, ' ').trim();
      if (!t) return t;
      const words = t.split(' ');
      if (words.length <= headWords + tailWords + 2) return t;
      const head = words.slice(0, headWords).join(' ');
      const tail = words.slice(-tailWords).join(' ');
      return `${head} … ${tail}`;
    } catch {
      return s as any;
    }
  };
  const scrollToInteraction = (id: string) => {
    try {
      const el =
        typeof document !== 'undefined'
          ? (document.querySelector(
              `[data-interaction-id="${id}"]`,
            ) as HTMLElement | null)
          : null;
      if (el && messagesRef.current) {
        const rect = el.getBoundingClientRect();
        const host = messagesRef.current;
        const hostRect = host.getBoundingClientRect();
        const offset =
          rect.top - hostRect.top + host.scrollTop - host.clientHeight / 2;
        host.scrollTo({ top: Math.max(0, offset), behavior: 'smooth' });
      } else if (el && typeof el.scrollIntoView === 'function') {
        // Fallback if messagesRef is not available
        el.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    } catch {}
  };

  // Handle removing notes from pinned interactions
  const handleRemoveNote = React.useCallback(
    async (interactionId: string, noteIndex: number) => {
      try {
        const projectId = genaiChat.projectId;
        if (!projectId || projectId === 'local-unfold') {
          console.warn('[RemoveNote] Invalid project ID', { projectId });
          return;
        }
        const interaction = genaiChat.interactions.find(
          (it) => it.id === interactionId,
        );
        if (!interaction) return;

        const map = (genaiChat as any).persistedByLocalId || {};
        const serverId = map[interactionId] || interactionId;
        const notes = (interaction.metadata?.notes || []).filter(
          (_, i) => i !== noteIndex,
        );
        const pinned =
          interaction.metadata?.pinned === true || notes.length > 0;
        await api.updateProjectInteraction(projectId, serverId, {
          metadata: {
            pinned,
            notes,
          },
        });
        await genaiChat.hydrateInteractions();
      } catch (err) {
        console.warn('[RemoveNote] failed', err);
      }
    },
    [genaiChat],
  );
  React.useEffect(() => {
    if (sideOpen) setTitlesGlow(false);
  }, [sideOpen]);

  // Detect server-provided user stance (eval.stance linked to a user message)
  // Note: hasAssistant, stanceFromInteractionsTop, riskBanner, and isHighRisk come from useAnalysisManagement hook

  // If server already sent stance, make the User button available
  React.useEffect(() => {
    setAvailUserAnalysis(!!stanceFromInteractionsTop);
  }, [stanceFromInteractionsTop]);

  // Discussion Flow is always available now (no need to check message count)
  React.useEffect(() => {
    setAvailDiscussionFlow(true);
  }, []);


  // Analysis handlers for artifacts panel - handled by useArtifactsAnalysis hook
  const {
    handleAnalyzeEntities,
    handleAnalyzeFacts,
    handleAnalyzeEvents,
    handleAnalyzeMetrics,
    handleAnalyzeDecisions,
  } = artifactsAnalysis;
  
  // Text editing state - handled by useTextEditing hook
  const {
    frozenEdits,
    editErrors,
    editLast,
    editSeconds,
    editHighlight,
    setEditHighlight,
    handleEditStart,
    handleEditFinish,
    handleEditError,
    handleEditUpdate,
    handleSaveRequest,
  } = textEditing;

  useEffect(() => {
    fetchProjects('discovery');
  }, [fetchProjects]);

  // Keep the top edge of TitleSuggestions aligned with chat input top
  React.useEffect(() => {
    const recalc = () => {
      try {
        const inputEl = document.querySelector(
          '#unfold-request-container',
        ) as HTMLElement | null;
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
      const hi = [...st.interactions]
        .reverse()
        .find(
          (it) => (it as any).highRisk && (it as any).highRisk.level === 'High',
        );
      if (hi && (hi as any).highRisk) {
        const rationale = (hi as any).highRisk.rationale || '';
        // persist tint regardless of banner visibility
        setIsHighRisk(true as any);
        // keep banner text updated if currently visible
        setRiskBanner((prev) =>
          prev
            ? { ...prev, rationale: rationale || prev.rationale || '' }
            : prev,
        );
      } else {
        setIsHighRisk(false as any);
      }
    } catch {}
  }, [genaiChat.interactions.length]);

  // User language and admin status are handled by useModals hook

  // WebSocket connection is handled by useWebSocketConnection hook

  // Centralized function to reset chat state (used for New Chat, deleting current chat, and switching chats)
  // Note: This function uses resetChatState from projectsManagement hook but adds additional cleanup
  const resetChatStateWithCleanup = React.useCallback(
    (resetToLocal: boolean = true) => {
      // Use the resetChatState from hook
      resetChatState();
      // Additional cleanup specific to this component
      clearSources();
      setQueryText('');
      setIsHighRisk(false);
      setRiskBanner(null);
      setShowUserAnalysis(false);
      setShowLLMReview(false);
      setShowDiscussionFlow(false);
      try {
        notifiedAnalysesRef.current = new Set();
      } catch {}
    },
    [resetChatState, clearSources],
  );

  useEffect(() => {
    const pid = currentProject?.id;
    if (pid) {
      try {
        const st = useGenaiChatStore.getState();
        const wasDifferentProject = st.projectId !== pid;
        if (wasDifferentProject) {
          // Clear local UI states when switching to a different project
          resetChatStateWithCleanup(false);
          // Set new project ID (this clears chat state in store)
          st.setProjectId(pid);
        }
      } catch {}
      fetchProjectSources(pid);
      fetchProjectInteractions(pid);
      try {
        const st = useGenaiChatStore.getState();
        st.hydrateInteractions();
      } catch {}
    } else {
      clearSources();
    }
  }, [currentProject?.id, resetChatStateWithCleanup, fetchProjectSources, fetchProjectInteractions, clearSources]);

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

  // Auto-scroll is handled by useAdvancedAutoScroll hook
  useAdvancedAutoScroll(messagesRef);
  
  // Track text selection within assistant markdown and position a floating action button
  useTextSelection({
    containerRef: messagesRef,
    onEditStart: handleEditStart,
    onEditFinish: handleEditFinish,
    onEditError: handleEditError,
    onEditUpdate: handleEditUpdate,
    onSaveRequest: handleSaveRequest,
  });

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
            const container = document.querySelector(
              `.markdown[data-interaction-id=\"${it.id}\"]`,
            );
            if (container) {
              pairs.push({ container: container as Element, notes });
            }
          }
        }
        if (pairs.length > 0) {
          const success = applyGlobalHighlight(pairs);
          if (!success) {
            pairs.forEach(({ container, notes }) =>
              highlightNotes(container, notes),
            );
          }
        }
        // Important: do NOT clear highlights on interim empty states to avoid flicker
      } catch (e) {
        console.warn('[Highlight] Failed to process highlights', e);
      }
    }, 50); // Increased delay to ensure DOM is stable after React re-render
    return () => {
      window.clearTimeout(t);
    };
  }, [genaiChat.interactions, rightPanelType]);

  // Track text selection within assistant markdown and position a floating action button
  useTextSelection({
    containerRef: messagesRef,
    onEditStart: handleEditStart,
    onEditFinish: handleEditFinish,
    onEditError: handleEditError,
    onEditUpdate: handleEditUpdate,
    onSaveRequest: handleSaveRequest,
  });

  // Handle Apply button from perspective analysis - sends key point to chat
  const handleApplyKeyPoint = React.useCallback(async (keyPoint: string) => {
    try {
      // Switch to chat view
      setUnfoldSubView('chat');

      // Send message to chat using genaiChatStore
      const genaiChat = useGenaiChatStore.getState();
      await genaiChat.startStream({
        prompt: keyPoint,
        suppressUserEcho: false,
      });
    } catch (error) {
      console.error('Failed to apply key point:', error);
    }
  }, []);

  // parseSourcesFromQuery comes from useSourcesManagement hook

  const handleExecuteDiscovery = async () => {
    if (running || !queryText.trim()) {
      console.log('[Discovery] Blocked:', {
        running,
        hasQuery: !!queryText.trim(),
      });
      return;
    }

    const parsed = parseSourcesFromQuery(queryText);
    console.log('[Discovery] Parsed sources:', parsed);
    
    if (parsed.web.length === 0 && parsed.telegram.length === 0) {
      alert(
        'Please include at least one source (URL or @telegram handle) in your query',
      );
      return;
    }

    // Pre-seed pending sources for immediate visual feedback
    const pending = [
      ...parsed.web.map((url) => ({
        id: url,
        kind: 'web' as const,
        status: 'fetching',
      })),
      ...parsed.telegram.map((handle) => ({
        id: handle,
        kind: 'telegram' as const,
        status: 'fetching',
      })),
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
          {
            id: createdAt,
            kind: 'discovery.query',
            content: queryText,
            createdAt,
          },
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

  React.useEffect(() => {
    const handler = (e: any) => {
      const kind = e?.detail?.kind as 'privacy' | 'terms';
      if (kind === 'privacy' || kind === 'terms') {
        closeAllModals();
        setStaticPage(kind);
      }
    };
    window.addEventListener('ui.openPage', handler as any);
    return () => window.removeEventListener('ui.openPage', handler as any);
  }, [closeAllModals]);
  return (
    <div
      className="flex h-full"
      style={{ padding: isMobile ? '8px 0' : '16px 0' }}
    >
      {/* Side panel */}
      <SidebarPanel
        isOpen={sideOpen}
        onOpen={() => setSideOpen(true)}
        onClose={() => setSideOpen(false)}
        isMobile={isMobile}
        isAdmin={isAdmin}
        onResetChat={() => {
          resetChatStateWithCleanup();
          setLeftPanel(null);
        }}
        onShowSettings={openSettings}
        onShowAccount={openAccount}
        onShowAdmin={openAdmin}
        editingProjectId={editingProjectId}
        editingProjectName={editingProjectName}
        editingInputRef={editingInputRef}
        onEditingProjectIdChange={setEditingProjectId}
        onEditingProjectNameChange={setEditingProjectName}
        onDeleteChat={handleDeleteChat}
        onRenameChat={handleRenameChat}
        onStartEditingProject={startEditingProject}
      />

      {/* Floating Account and Settings buttons (visible only when sidebar is hidden) */}
      <FloatingActionButtons
        sideOpen={sideOpen}
        showSettings={showSettings}
        showAccount={showAccount}
        onOpenAccount={openAccount}
        onOpenSettings={openSettings}
      />

      {/* Main content area: 5% margin + chat + drawer */}
      <div
        className="flex-1 min-h-0"
        style={{
          position: 'relative',
          display: 'flex',
          gap: '16px',
        }}
      >
        {/* 2% left margin for breathing room */}
        <div style={{ width: '2%', minWidth: '2%', flexShrink: 0 }} />

        {/* Chat area - flexible width */}
        <div
          className="flex-1 min-h-0"
          style={{
            display: 'flex',
            flexDirection: 'column',
            minWidth: 0,
          }}
        >
          {/* Headless runner to fetch user stance in background so the button appears */}
          <div style={{ display: 'none' }} aria-hidden>
            <UserPositionAnalysis
              onFirstResult={() => {
                try {
                  setBadgeUser(true);
                  setAvailUserAnalysis(true);
                } catch {}
              }}
              onRiskUpdate={(r) => {
                const high = !!(r && r.level === 'High');
                setIsHighRisk(high);
                if (high) {
                  setRiskBanner((prev) =>
                    prev && prev.visible === false
                      ? prev
                      : {
                          visible: true,
                          level: 'High',
                          rationale: r?.rationale || '',
                        },
                  );
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

          {/* Main center column: renders discussion or graph placeholder */}
        <div
          className="min-h-0 flex flex-col"
          style={{ 
            position: 'relative', 
              flex: 1,
          }}
        >
        {showAccount ? (
          <AccountModal
            isOpen={showAccount}
            onClose={closeAllModals}
          />
        ) : showSettings ? (
          <SettingsModal
            isOpen={showSettings}
            onClose={closeAllModals}
          />
        ) : showAdmin ? (
          <AdminModal
            isOpen={showAdmin}
            onClose={closeAllModals}
          />
        ) : staticPage ? (
          <div className="w-full h-full flex flex-col overflow-hidden">
            <div className="px-4 mt-2 mb-2 flex items-center flex-shrink-0">
              <button
                onClick={() => setStaticPage(null)}
                className="p-2 rounded-lg transition-colors"
                    style={{
                      backgroundColor: 'var(--white-10)',
                      color: 'var(--text-heading)',
                    }}
                    onMouseEnter={(e) => {
                      (
                        e.currentTarget as HTMLButtonElement
                      ).style.backgroundColor = 'var(--white-20)';
                    }}
                    onMouseLeave={(e) => {
                      (
                        e.currentTarget as HTMLButtonElement
                      ).style.backgroundColor = 'var(--white-10)';
                    }}
                aria-label={t('unfold.sections.back')}
              >
                    <svg
                      className="w-4 h-4"
                      fill="none"
                      stroke="currentColor"
                      viewBox="0 0 24 24"
                    >
                      <path
                        strokeLinecap="round"
                        strokeLinejoin="round"
                        strokeWidth={2}
                        d="M15 19l-7-7 7-7"
                      />
                </svg>
              </button>
            </div>
            <div className="flex-1 overflow-y-auto">
                  {staticPage === 'privacy' ? (
                    <PrivacyNotice />
                  ) : (
                    <TermsOfUsage />
                  )}
            </div>
          </div>
        ) : (
        <>
        {/* Sources moved to the left grid column */}

        {/* Graph placeholder when in graph mode */}
        {viewMode === 'graph' ? (
                  <div
                    className="w-full h-full flex items-center justify-center"
                    style={{
                      background: 'var(--bg-elevated)',
                      border: '1px dashed var(--overlay-line-90)',
                      borderRadius: '12px',
                    }}
                  >
                    <div
                      className="text-13"
                      style={{ color: 'var(--text-secondary-strong)' }}
                    >
                      {t('unfold.status.graphPlaceholder')}
                    </div>
          </div>
        ) : viewMode === 'unfold' ? (
                  <div
                    className="w-full px-4 flex-1 flex flex-col min-h-0"
                    style={{ position: 'relative' }}
                  >
            {/* Document viewer overlay - replaces chat area when open */}
                    {activeDoc &&
                      genaiChat.projectId &&
                      genaiChat.projectId !== 'local-unfold' && (
              <DocumentViewer
                projectId={genaiChat.projectId}
                doc={activeDoc}
                onClose={() => setActiveDoc(null)}
              />
            )}
            {/* Metric overlay - replaces chat area when open */}
            <MetricOverlay />
            {/* Unfold subview toggle */}
            <div className="w-fit self-start mb-3">
              <div 
                className="inline-flex items-center gap-1 rounded-xl px-2 py-1" 
                role="tablist" 
                aria-label={t('unfold.sections.unfoldView')}
                style={{
                          backgroundColor:
                            theme === 'light'
                              ? 'rgba(0, 0, 0, 0.02)'
                              : 'rgba(255, 255, 255, 0.04)',
                }}
              >
                <button
                  type="button"
                  role="tab"
                  aria-selected={unfoldSubView === 'chat'}
                  aria-controls="unfold-chat-panel"
                  onClick={() => setUnfoldSubView('chat')}
                  className="px-2 py-1 text-12 rounded"
                  style={{
                            background:
                              unfoldSubView === 'chat'
                                ? 'var(--white-10)'
                                : 'transparent',
                            color: 'var(--text-heading)',
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
                            background:
                              unfoldSubView === 'perspectives'
                                ? 'var(--white-10)'
                                : 'transparent',
                            color: 'var(--text-heading)',
                  }}
                >
                  {t('unfold.sections.perspectives')}
                </button>
              </div>
            </div>
            {/* Perspectives content (full column) */}
            {unfoldSubView === 'perspectives' && (
                      <PerspectivesPanel
                        onPresetChange={setCurrentPerspectivePreset}
                      />
            )}
            {/* GenAI chat history */}
                    <div
                      ref={messagesRef}
                      className="flex-1 overflow-y-auto px-1 min-h-[160px]"
                      style={{
                        contentVisibility: 'auto',
                        containIntrinsicSize: '800px 600px',
                        display:
                          unfoldSubView === 'perspectives' ? 'none' : undefined,
                      }}
                    >
              {(() => {
                // Check if chat is empty (not hydrating and no messages/interactions)
                        const isEmpty =
                          !genaiChat.hydrating &&
                          genaiChat.interactions.length === 0 &&
                          genaiChat.messages.length === 0;
                
                if (isEmpty) {
                  return (
                            <div
                              className="flex items-center justify-center"
                              style={{
                                height: '100%',
                                minHeight: isMobile
                                  ? '200px'
                                  : 'calc(100vh - 300px)',
                                padding: isMobile ? '24px 12px' : '48px 24px',
                              }}
                            >
                      <div className="max-w-md text-center">
                                <div
                                  className="text-16"
                                  style={{
                                    color: 'var(--text-heading)',
                                    lineHeight: '1.6',
                                    fontSize: 'clamp(18px, 2vw, 24px)',
                                    fontWeight: 500,
                                  }}
                                >
                          {emptyStateMessage}
                        </div>
                      </div>
                    </div>
                  );
                }
                
                return (
                  <div className="space-y-2">
                    {/* Skeleton loading during hydration */}
                            {genaiChat.hydrating &&
                              genaiChat.interactions.length === 0 && (
                      <div className="space-y-2 py-2">
                                  <div
                                    style={{
                                      height: 14,
                                      borderRadius: 4,
                                      background: 'var(--white-06)',
                                      position: 'relative',
                                      overflow: 'hidden',
                                    }}
                                  >
                                    <div
                                      style={{
                                        position: 'absolute',
                                        inset: 0,
                                        background:
                                          'linear-gradient(90deg, transparent, var(--white-20), transparent)',
                                        animation: 'etg-shimmer 2s infinite',
                                      }}
                                    />
                        </div>
                                  <div
                                    style={{
                                      height: 10,
                                      width: '90%',
                                      borderRadius: 4,
                                      background: 'var(--white-06)',
                                      position: 'relative',
                                      overflow: 'hidden',
                                    }}
                                  >
                                    <div
                                      style={{
                                        position: 'absolute',
                                        inset: 0,
                                        background:
                                          'linear-gradient(90deg, transparent, var(--white-20), transparent)',
                                        animation: 'etg-shimmer 2s infinite',
                                      }}
                                    />
                        </div>
                                  <div
                                    style={{
                                      height: 10,
                                      width: '80%',
                                      borderRadius: 4,
                                      background: 'var(--white-06)',
                                      position: 'relative',
                                      overflow: 'hidden',
                                    }}
                                  >
                                    <div
                                      style={{
                                        position: 'absolute',
                                        inset: 0,
                                        background:
                                          'linear-gradient(90deg, transparent, var(--white-20), transparent)',
                                        animation: 'etg-shimmer 2s infinite',
                                      }}
                                    />
                        </div>
                                  <div
                                    style={{
                                      height: 10,
                                      width: '70%',
                                      borderRadius: 4,
                                      background: 'var(--white-06)',
                                      position: 'relative',
                                      overflow: 'hidden',
                                    }}
                                  >
                                    <div
                                      style={{
                                        position: 'absolute',
                                        inset: 0,
                                        background:
                                          'linear-gradient(90deg, transparent, var(--white-20), transparent)',
                                        animation: 'etg-shimmer 2s infinite',
                                      }}
                                    />
                        </div>
                      </div>
                    )}
                    {(() => {
                  // Merge messages and interactions, sort by timestamp to ensure correct order
                  return mergedMessages.map((item) => {
                    if (item.type === 'message') {
                      const m = item.data;
                      return (
                        <MessageItem
                          key={m.id}
                          message={m}
                          genaiChat={{
                            streaming: genaiChat.streaming,
                            error: genaiChat.error,
                            messageMeta: genaiChat.messageMeta,
                            preStanceLatency: genaiChat.preStanceLatency,
                            evaluations: genaiChat.evaluations,
                          }}
                          genaiThinkingSeconds={genaiThinkingSeconds}
                          averageTimingSeconds={averageTimingSeconds}
                          genaiSettings={{
                            strength: genaiSettings.strength,
                            model: genaiSettings.model,
                            groundingEnabled: genaiSettings.groundingEnabled,
                          }}
                          expandedChip={expandedChip}
                          onExpandChip={setExpandedChip}
                        />
                      );
                    } else {
                                  // item.type === 'interaction'
                      const it = item.data;
                      // Render section markers
                      if (
                        it.kind === 'section_start' ||
                        it.kind === 'section_end'
                      ) {
                        return (
                          <SectionMarker
                            key={it.id}
                            interaction={it}
                            collapsedSections={collapsedSections}
                            onToggleCollapse={(sid) =>
                              setCollapsedSections((s) => ({
                                ...s,
                                [sid]: !s[sid],
                              }))
                            }
                          />
                        );
                      }
                      
                      // Special rendering for action interactions
                      if (it.kind === 'action') {
                        return (
                          <ActionInteraction
                            key={it.id}
                            interaction={it}
                            collapsedSections={collapsedSections}
                          />
                        );
                      }
                      
                      const isUser = it.kind === 'user';
                      // Skip rendering if inside a collapsed section
                      try {
                                    const sid = (it as any).sectionId as
                                      | string
                                      | undefined;
                                    if (sid && collapsedSections[sid])
                                      return null;
                      } catch {}
                      
                      // Special rendering for action.bring_in indicator messages
                      if (
                        isUser &&
                        it.subkind === 'action.bring_in'
                      ) {
                        return (
                          <BringInIndicator
                            key={it.id}
                            interaction={it}
                          />
                        );
                      }
                      
                      const meta = genaiChat.messageMeta[it.id];
                                  const header = !isUser
                                    ? meta?.headerText ||
                                      it.author?.displayName ||
                                      (it.agentProfile ? 'agent' : 'assistant')
                                    : undefined;
                                  const leftAccent = !isUser
                                    ? (meta?.leftAccent ?? !!it.agentProfile)
                                    : false;
                      
                      return (
                        <div key={it.id} className="group relative">
                          <MessageActions
                            interactionId={it.id}
                            isUser={isUser}
                            interaction={it}
                          />
                                      <ChatMessage
                                        role={isUser ? 'user' : 'assistant'}
                                        align={isUser ? 'right' : 'left'}
                                        isThinking={false}
                                        thinkingSeconds={0}
                                        isError={false}
                                        headerText={header}
                                        leftAccent={leftAccent}
                                        footer={undefined}
                                      >
                          {isUser ? (
                            <>
                              <CollapsibleText maxLines={7}>
                                <MarkdownRenderer
                                  content={it.content}
                                  className="text-13 markdown"
                                  style={{
                                    color: 'var(--text-heading)',
                                    whiteSpace: 'pre-wrap',
                                  }}
                                />
                              </CollapsibleText>
                              {/* Lightweight action row for USER: only evaluation, no buttons */}
                              {(() => {
                                try {
                                  const evalNode = findEvalBiasInteraction(
                                    it.id,
                                    genaiChat.interactions,
                                    (genaiChat as any).persistedByLocalId || {},
                                  );
                                  if (!evalNode) return null;
                                  return (
                                    <div className="mt-1">
                                      <EvaluationDetails messageId={it.id} />
                                    </div>
                                  );
                                } catch {
                                  return null;
                                }
                              })()}
                            </>
                          ) : (
                            <>
                              {/* Graph message rendering */}
                              {(() => {
                                const gd = parseGraphData(it);
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
                                const contentWithHighlight = applyEditHighlight(
                                  (it.contentSanitized || it.content) as string,
                                  editHighlight[it.id],
                                );
                                
                                return (
                                  <div
                                    className="text-13 markdown"
                                    data-interaction-id={it.id}
                                    style={{
                                      color: 'var(--text-heading)',
                                      position: 'relative',
                                      opacity: frozenEdits[it.id] ? 0.6 : 1,
                                      pointerEvents: frozenEdits[it.id]
                                        ? ('none' as any)
                                        : 'auto',
                                    }}
                                  >
                                    <MarkdownRenderer
                                      content={contentWithHighlight}
                                      useRaw={true}
                                      skipWrapper={true}
                                    />
                              {/* Top-right status chip */}
                                                  {(frozenEdits[it.id] ||
                                                    editErrors[it.id]) && (
                                                    <div
                                                      className="absolute"
                                                      style={{
                                                        top: 4,
                                                        right: 4,
                                                      }}
                                                    >
                                  {frozenEdits[it.id] && (
                                                        <span
                                                          className="px-2 py-0.5 rounded text-[10px]"
                                                          style={{
                                                            background:
                                                              'var(--white-10)',
                                                            border:
                                                              '1px solid var(--overlay-line-90)',
                                                            color:
                                                              'var(--text-secondary-strong)',
                                                          }}
                                                        >
                                                          <span
                                                            className="inline-block mr-1"
                                                            style={{
                                                              width: 8,
                                                              height: 8,
                                                              borderRadius: 9999,
                                                              background:
                                                                'var(--text-secondary-strong)',
                                                              opacity: 0.6,
                                                              animation:
                                                                'pulse 1.2s infinite',
                                                            }}
                                                          />
                                      Updating…
                                    </span>
                                  )}
                                                      {editErrors[it.id] &&
                                                        !frozenEdits[it.id] && (
                                                          <span
                                                            className="px-2 py-0.5 rounded text-[10px]"
                                                            style={{
                                                              background:
                                                                'var(--bg-elevated)',
                                                              border:
                                                                '1px solid var(--overlay-line-90)',
                                                              color:
                                                                'var(--accent-danger)',
                                                            }}
                                                          >
                                      Failed
                                                            <button
                                                              className="ml-2 underline"
                                                              style={{
                                                                color:
                                                                  'var(--text-secondary-strong)',
                                                              }}
                                                              onClick={() => {
                                                                const last =
                                                                  editLast[
                                                                    it.id
                                                                  ];
                                                                if (
                                                                  last &&
                                                                  last.request
                                                                ) {
                                          // re-run with saved request
                                          (async () => {
                                                                    const req =
                                                                      last.request;
                                                                    const selText =
                                                                      last.selected;
                                                                    const markdownRoot =
                                                                      document.querySelector(
                                                                        `[data-interaction-id="${it.id}"]`,
                                                                      ) as HTMLElement | null;
                                                                    if (
                                                                      !markdownRoot
                                                                    )
                                                                      return;
                                                                    const inter =
                                                                      useGenaiChatStore
                                                                        .getState()
                                                                        .interactions.find(
                                                                          (x) =>
                                                                            x.id ===
                                                                            it.id,
                                                                        );
                                                                    if (!inter) return;
                                                                    const full =
                                                                      (
                                                                        inter?.contentSanitized ||
                                                                        inter?.content ||
                                                                        ''
                                                                      ).toString();
                                                                    const displayed =
                                                                      (markdownRoot.innerText || '').toString();
                                                                    const meta =
                                                                      useGenaiChatStore.getState()
                                                                        .messageMeta[
                                                                        it.id
                                                                      ];
                                                                    const agentName =
                                                                      meta?.headerText ||
                                                                      'agent';
                                                                    const prof =
                                                                      meta?.agentProfile;
                                                                    await handleEditTextDirect({
                                                                      interactionId: it.id,
                                                                      selectedText: selText,
                                                                      fullText: full,
                                                                      displayedText: displayed,
                                                                      agentName,
                                                                      agentProfile: prof,
                                                                      request: req,
                                                                      onUpdate: (id, newContent) => {
                                                                        handleEditUpdate(id, newContent);
                                                                      },
                                                                      onError: (id, error) => {
                                                                        handleEditError(id, error);
                                                                      },
                                                                      onStart: (id) => {
                                                                        handleEditStart(id);
                                                                      },
                                                                      onFinish: (id) => {
                                                                        handleEditFinish(id);
                                                                      },
                                                                    });
                                          })();
                                        }
                                                              }}
                                                            >
                                                              Retry
                                                            </button>
                                                            <button
                                                              className="ml-1 underline"
                                                              style={{
                                                                color:
                                                                  'var(--text-secondary-strong)',
                                                              }}
                                                              onClick={() =>
                                                                handleEditError(it.id, null as any)
                                                              }
                                                            >
                                                              Cancel
                                                            </button>
                                    </span>
                                  )}
            </div>
                              )}
                              {frozenEdits[it.id] && (
                                                    <div
                                                      className="absolute inset-0"
                                                      style={{
                                                        pointerEvents: 'none',
                                                      }}
                                                    >
                                                      <div
                                                        className="absolute inset-0"
                                                        style={{
                                                          background:
                                                            'linear-gradient(90deg, transparent, var(--white-06), transparent)',
                                                          animation:
                                                            'etg-shimmer 2s infinite',
                                                        }}
                                                      />
                                </div>
                              )}
                              {editErrors[it.id] && (
                                                    <div
                                                      className="mt-1 px-2 py-1 rounded"
                                                      style={{
                                                        border:
                                                          '1px solid var(--overlay-line-90)',
                                                        background:
                                                          'var(--bg-elevated)',
                                                      }}
                                                    >
                                                      <div
                                                        className="text-[11px]"
                                                        style={{
                                                          color:
                                                            'var(--accent-danger)',
                                                        }}
                                                      >
                                                        Edit failed:{' '}
                                                        {editErrors[it.id]}
                                                      </div>
                                </div>
                              )}
                            </div>
                                              );
                                            })()}
                              {/* End of graph/markdown conditional rendering */}
                              {/* Tags row: per-interaction chips for assistant - BEFORE ActionRow/EvaluationDetails */}
                                            {!isUser &&
                                              (() => {
                                                try {
                                                  const perQ = Array.isArray(
                                                    (it as any).searchQueries,
                                                  )
                                                    ? (
                                                        it as any
                                                      ).searchQueries.filter(
                                                        Boolean,
                                                      )
                                                    : [];
                                                  const perS = Array.isArray(
                                                    (it as any).sources,
                                                  )
                                                    ? (
                                                        it as any
                                                      ).sources.filter(Boolean)
                                                    : [];
                                  const qs = perQ.slice(0, 4);
                                                  const attrs =
                                                    perS.length > 0
                                                      ? perS
                                                          .map(
                                                            (url: string) => ({
                                                              url,
                                                            }),
                                                          )
                                                          .slice(0, 4)
                                                      : [];
                                                  if (
                                                    qs.length === 0 &&
                                                    attrs.length === 0
                                                  )
                                                    return null;
                                  return (
                                    <div className="mt-1">
                                                      <div
                                                        className="text-[11px] mb-1"
                                                        style={{
                                                          color:
                                                            'var(--text-tertiary-weak)',
                                                        }}
                                                      >
                                                        Sources:
                                                      </div>
                                      <div className="flex flex-wrap gap-1">
                                                        {qs.map(
                                                          (
                                                            q: string,
                                                            i: number,
                                                          ) => {
                                        const chipId = `${it.id}-q-${i}`;
                                                            const isExpanded =
                                                              expandedChip ===
                                                              chipId;
                                                            const words =
                                                              String(q || '')
                                                                .trim()
                                                                .split(/\s+/);
                                                            const short =
                                                              words.length > 3
                                                                ? words
                                                                    .slice(0, 3)
                                                                    .join(' ') +
                                                                  '…'
                                                                : String(
                                                                    q || '',
                                                                  );
                                                            const needsExpand =
                                                              words.length > 3;
                                        return (
                                          <button
                                            key={`q-${i}`}
                                            type="button"
                                            className="px-2 py-0.5 rounded-full text-[10px] transition-colors"
                                                                style={{
                                                                  backgroundColor:
                                                                    isExpanded
                                                                      ? 'var(--white-20)'
                                                                      : 'var(--white-10)',
                                                                  color:
                                                                    'var(--text-secondary-strong)',
                                                                  border:
                                                                    'none',
                                                                  cursor:
                                                                    needsExpand
                                                                      ? 'pointer'
                                                                      : 'default',
                                                                }}
                                                                onClick={() =>
                                                                  needsExpand &&
                                                                  setExpandedChip(
                                                                    isExpanded
                                                                      ? null
                                                                      : chipId,
                                                                  )
                                                                }
                                                                onMouseEnter={(
                                                                  e,
                                                                ) => {
                                                                  if (
                                                                    needsExpand
                                                                  )
                                                                    (
                                                                      e.currentTarget as HTMLButtonElement
                                                                    ).style.backgroundColor =
                                                                      isExpanded
                                                                        ? 'var(--white-25)'
                                                                        : 'var(--white-15)';
                                                                }}
                                                                onMouseLeave={(
                                                                  e,
                                                                ) => {
                                                                  if (
                                                                    needsExpand
                                                                  )
                                                                    (
                                                                      e.currentTarget as HTMLButtonElement
                                                                    ).style.backgroundColor =
                                                                      isExpanded
                                                                        ? 'var(--white-20)'
                                                                        : 'var(--white-10)';
                                                                }}
                                                              >
                                                                {isExpanded
                                                                  ? String(
                                                                      q || '',
                                                                    )
                                                                  : short}
                                          </button>
                                        );
                                                          },
                                                        )}
                                                        {attrs.map(
                                                          (
                                                            a: any,
                                                            i: number,
                                                          ) => {
                                        const chipId = `${it.id}-src-${i}`;
                                                            const isExpanded =
                                                              expandedChip ===
                                                              chipId;
                                                            const raw = String(
                                                              (a as any).url ||
                                                                '',
                                                            ).replace(
                                                              /^https?:\/\//i,
                                                              '',
                                                            );
                                                            const parts =
                                                              raw.split('/');
                                                            const host =
                                                              parts[0] || '';
                                                            const tail =
                                                              parts[1]
                                                                ? `/${parts[1]}`
                                                                : '';
                                                            const label =
                                                              host + tail ||
                                                              (a as any)
                                                                .title ||
                                                              'link';
                                                            const needsExpand =
                                                              raw.length >
                                                              (host + tail)
                                                                .length;
                                        return (
                                          <a
                                            key={`src-${i}`}
                                                                href={
                                                                  (a as any).url
                                                                }
                                            target="_blank"
                                            rel="noreferrer"
                                            className="px-2 py-0.5 rounded-full text-[10px] underline transition-colors"
                                                                style={{
                                                                  backgroundColor:
                                                                    isExpanded
                                                                      ? 'var(--white-20)'
                                                                      : 'var(--white-10)',
                                                                  color:
                                                                    'var(--text-secondary-strong)',
                                                                }}
                                                                onClick={(
                                                                  e,
                                                                ) => {
                                                                  if (
                                                                    needsExpand
                                                                  ) {
                                                                    e.preventDefault();
                                                                    setExpandedChip(
                                                                      isExpanded
                                                                        ? null
                                                                        : chipId,
                                                                    );
                                                                  }
                                                                }}
                                                                onMouseEnter={(
                                                                  e,
                                                                ) => {
                                                                  (
                                                                    e.currentTarget as HTMLAnchorElement
                                                                  ).style.backgroundColor =
                                                                    isExpanded
                                                                      ? 'var(--white-25)'
                                                                      : 'var(--white-15)';
                                                                }}
                                                                onMouseLeave={(
                                                                  e,
                                                                ) => {
                                                                  (
                                                                    e.currentTarget as HTMLAnchorElement
                                                                  ).style.backgroundColor =
                                                                    isExpanded
                                                                      ? 'var(--white-20)'
                                                                      : 'var(--white-10)';
                                                                }}
                                                              >
                                                                {isExpanded
                                                                  ? raw
                                                                  : label}
                                          </a>
                                        );
                                                          },
                                                        )}
                                      </div>
                                    </div>
                                  );
                                                } catch {
                                                  return null;
                                                }
                              })()}
                              {!isUser && (
                                              <ActionRow
                                                messageId={it.id}
                                                isAssistant={!isUser}
                                                content={
                                                  it.contentSanitized ||
                                                  it.content
                                                }
                                              />
                              )}
                            </>
                          )}
                        </ChatMessage>
                        </div>
                      );
                    }
                  });
                })()}
                            {genaiChat.streaming &&
                              (() => {
                  try {
                                  const m =
                                    genaiChat.messages[
                                      genaiChat.messages.length - 1
                                    ];
                    if (!m || m.role !== 'assistant') return null;
                    // Don't show placeholder if message has content (streaming has started producing content)
                                  if (m.content && m.content.trim())
                                    return null;
                    
                    return (
                      <ThinkingPlaceholder
                        key={m.id}
                        messageId={m.id}
                        genaiThinkingSeconds={genaiThinkingSeconds}
                        averageTimingSeconds={averageTimingSeconds}
                      />
                    );
                                } catch {
                                  return null;
                                }
                })()}
                  </div>
                );
              })()}
            </div>
            {/* Edit status indicator */}
            {Object.values(frozenEdits).some(Boolean) && (
              <EditStatusIndicator editSeconds={editSeconds} />
            )}
            {/* Floating selection action button is mounted imperatively in effect to avoid rerendering content and losing selection */}
            {/* Tool calls / results */}
            <ToolCallsDisplay />
            {/* Thinking indicator moved inside assistant bubble during streaming */}
            {/* Usage and attributions after completion */}
            <AttributionsDisplay />
            {/* High-risk banner (if any) */}
            <RiskBanner
              riskBanner={riskBanner}
              onDismiss={() =>
                setRiskBanner((prev) =>
                  prev ? { ...prev, visible: false } : null,
                )
              }
            />
            {/* Analysis status banner unified in UnfoldRequest; duplicate removed here */}
            {/* Text Tasks Panel */}
            {textTasks.length > 0 && (
              <TextTasksPanel
                tasks={textTasks}
                onRemove={handleRemoveTextTask}
                onApply={handleApplyTextTasks}
                isApplying={textTasksApplying}
              />
            )}
            {/* Opponent button and agents list - overlay above input, aligned to right edge */}
            <OpponentAgentsPalette
              unfoldSubView={unfoldSubView}
              genaiChatInteractionsLength={genaiChatInteractionsLength}
              genaiChatMessagesLength={genaiChatMessagesLength}
            />
            {/* Input */}
            {unfoldSubView !== 'perspectives' && (
            <div ref={unfoldRequestRef}>
            <UnfoldRequest
              dangerTint={isHighRisk}
              showHighRiskTag={isHighRisk}
                          onHighRiskTagClick={() =>
                            setRiskBanner((prev) => ({
                              visible: true,
                              level: 'High',
                              rationale: prev?.rationale || '',
                            }))
                          }
              onModeChange={setUnfoldMode}
              onSubmit={async (text) => {
                if (genaiChat.streaming || !text.trim()) return;
                let prompt = text.trim();
                // Slash commands: /section <title> and /end
                const raw = prompt;
                const isSection = /^\s*\/section\b/i.test(raw);
                const isEnd = /^\s*\/end\b/i.test(raw);
                            const isAnalyzeEntities =
                              /^\s*\/analyze\s+entities\b/i.test(raw) ||
                              /^\s*\/analyze\s+entities\s*$/i.test(raw);
                if (isSection || isEnd) {
                  try {
                    const pid = await genaiChat.ensureProject();
                    if (isSection) {
                                  const title = raw
                                    .replace(/^\s*\/section\b\s*/i, '')
                                    .trim();
                                  await api.createProjectInteraction(pid, {
                                    kind: 'section_start',
                                    content: title || '',
                                  });
                    } else {
                                  await api.createProjectInteraction(pid, {
                                    kind: 'section_end',
                                    content: '',
                                  });
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
                                  if (
                                    (window as any).wsSubscribeProject &&
                                    pid
                                  ) {
                        (window as any).wsSubscribeProject(pid);
                                    const waitForWsSubscribed = async (
                                      projectId: string,
                                      timeoutMs = 1500,
                                    ) => {
                          const start = Date.now();
                          // eslint-disable-next-line no-constant-condition
                          while (true) {
                            try {
                                          const ws = (
                                            await import('../stores/wsStore')
                                          ).useWsStore.getState();
                                          if (
                                            ws.connected &&
                                            ws.subscribedProjectId === projectId
                                          )
                                            break;
                            } catch {}
                                        if (Date.now() - start > timeoutMs)
                                          break;
                                        await new Promise((r) =>
                                          setTimeout(r, 60),
                                        );
                          }
                        };
                        await waitForWsSubscribed(pid);
                      }
                    } catch {}
                                const prompt =
                                  raw.replace(
                                    /^\s*\/analyze\s+entities\b\s*/i,
                                    '',
                                  ) +
                                  "\n\nConstraints:\n- Only include concrete, named entities (no abstract categories).\n- Allowed types: person | group_of_persons | organization_or_platform | project_or_product | classifier_or_standard | classification_code | regulatory_document | government_agency | contract_document | location | other.\n- Reject generic words like 'technical regulation', 'open databases', 'contracts' without identifiers.";
                                const res = await api.analyzeEntities(
                                  pid,
                                  prompt,
                                );
                                try {
                                  console.log('[AnalyzeEntities] queued:', res);
                                } catch {}
                    // UnfoldRequest clears itself after submit; WS will flip analyzing=false on parsed/failed
                  } catch (e) {
                    console.error('[AnalyzeEntities] failed', e);
                    setEntitiesAnalyzing(false);
                  }
                  return;
                }
                            const tools = genaiSettings.enableTelegramTool
                              ? [TelegramFetchUpdatesTool]
                              : undefined;
                // Map UI model to backend aliases
                const model = genaiSettings.model;
                // Chat language preference (used in hidden system instructions)
                const preferredLang = getPreferredLanguage();
                            const langNameForPrompt =
                              getLanguageName(preferredLang);
                            const sxSystemInstruction =
                              getSxSystemInstructionFull();
                            const graphSystemInstruction =
                              unfoldMode === 'graph'
                                ? `=== GRAPH MODE ACTIVE ===\n` +
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
                  : '';
                // In graph mode, prioritize graph instruction by putting it first
                            const combinedSystemInstruction =
                              unfoldMode === 'graph'
                  ? [
                      graphSystemInstruction,
                  genaiSettings.systemInstruction || '',
                  HATL_ONE_INSTRUCTIONS,
                  sxSystemInstruction,
                                  ]
                                    .filter(Boolean)
                                    .join('\n\n')
                                    .trim() || undefined
                  : [
                      genaiSettings.systemInstruction || '',
                      HATL_ONE_INSTRUCTIONS,
                      sxSystemInstruction,
                                  ]
                                    .filter(Boolean)
                                    .join('\n\n')
                                    .trim() || undefined;

                const options: any = {
                  // profile is used only for gemini fallback; keep for compatibility
                              profile:
                                model === 'gemini_quality' ? 'quality' : 'fast',
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
                              const aiInter = stBefore.interactions.filter(
                                (it) => it.kind === 'assistant',
                              );
                if (aiInter.length > 0) {
                  const last = aiInter[aiInter.length - 1];
                                prevAssistantText =
                                  last.summary && last.summary.trim()
                                    ? last.summary
                                    : last.content || null;
                } else {
                                const priorMsgs = stBefore.messages.filter(
                                  (m) =>
                                    m.role === 'assistant' &&
                                    m.content &&
                                    m.content.trim(),
                                );
                                if (priorMsgs.length > 0)
                                  prevAssistantText =
                                    priorMsgs[priorMsgs.length - 1].content;
                }
              } catch {}
                // UnfoldRequest clears itself after submit
                            await genaiChat.startStream({
                              prompt,
                              tools,
                              options: { ...options, __lang: preferredLang },
                            });
              // Fire-and-forget: structured bias evaluation for the USER message we just sent
              try {
                const st = useGenaiChatStore.getState();
                              const userMsgs = st.messages.filter(
                                (mm) => mm.role === 'user',
                              );
                              const lastUser =
                                userMsgs.length > 0
                                  ? userMsgs[userMsgs.length - 1]
                                  : null;
                const textToEvaluate = (prompt || '').trim();
                if (lastUser && textToEvaluate) {
                  (async () => {
                    try {
                                    const settingsStateEval =
                                      useGenAISettings.getState();
                                    const evalModel =
                                      settingsStateEval.stanceModel &&
                                      settingsStateEval.stanceModel.trim()
                                        ? settingsStateEval.stanceModel
                                        : 'gemini_fast';
                                    const preferredLang =
                                      getPreferredLanguage();
                      
                      // Call unified evaluation function
                      await evaluateBias({
                        messageId: lastUser.id,
                        messageRole: 'user',
                        textToEvaluate,
                                      contextText:
                                        prevAssistantText || undefined,
                        evalModel,
                        preferredLang,
                      });
                    } catch (e) {
                                    console.warn(
                                      '[UserStanceEval] User bias eval failed',
                                      e,
                                    );
                    }
                  })().catch(console.error);
                }
              } catch {}
              }}
              disabled={genaiChat.streaming || !queryText.trim()}
            />
            </div>
            )}
            {/* Resizer handle at right edge of center column */}
            <div
              className="chat-resizer"
              style={{ 
                right: -4,
                        pointerEvents:
                          rightPanelType !== null ? 'none' : 'auto',
              }}
              onMouseDown={(e) => {
                const root = document.documentElement;
                const startX = e.clientX;
                        const startWidth =
                          messagesRef.current?.parentElement?.clientWidth || 0;
                        const initial =
                          getComputedStyle(root).getPropertyValue(
                            '--center-col-width',
                          );
                const onMove = (ev: MouseEvent) => {
                  const delta = ev.clientX - startX;
                  const newWidth = Math.max(480, startWidth + delta);
                          root.style.setProperty(
                            '--center-col-width',
                            `${newWidth}px`,
                          );
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
        <div className="w-full px-4 flex-1 flex flex-col min-h-0">
          {/* Interactions history (LLM answers etc.) */}
                    <div
                      ref={messagesRef}
                      className="flex-1 overflow-y-auto px-1 min-h-[160px]"
                      style={{
                        contentVisibility: 'auto',
                        containIntrinsicSize: '800px 600px',
                      }}
                    >
            <div className="space-y-2">
              {/* Skeleton loading during hydration */}
              {genaiChat.hydrating && interactions.length === 0 && (
                <div className="space-y-2 py-2">
                            <div
                              style={{
                                height: 14,
                                borderRadius: 4,
                                background: 'var(--white-06)',
                                position: 'relative',
                                overflow: 'hidden',
                              }}
                            >
                              <div
                                style={{
                                  position: 'absolute',
                                  inset: 0,
                                  background:
                                    'linear-gradient(90deg, transparent, var(--white-20), transparent)',
                                  animation: 'etg-shimmer 2s infinite',
                                }}
                              />
                  </div>
                            <div
                              style={{
                                height: 10,
                                width: '90%',
                                borderRadius: 4,
                                background: 'var(--white-06)',
                                position: 'relative',
                                overflow: 'hidden',
                              }}
                            >
                              <div
                                style={{
                                  position: 'absolute',
                                  inset: 0,
                                  background:
                                    'linear-gradient(90deg, transparent, var(--white-20), transparent)',
                                  animation: 'etg-shimmer 2s infinite',
                                }}
                              />
                  </div>
                            <div
                              style={{
                                height: 10,
                                width: '80%',
                                borderRadius: 4,
                                background: 'var(--white-06)',
                                position: 'relative',
                                overflow: 'hidden',
                              }}
                            >
                              <div
                                style={{
                                  position: 'absolute',
                                  inset: 0,
                                  background:
                                    'linear-gradient(90deg, transparent, var(--white-20), transparent)',
                                  animation: 'etg-shimmer 2s infinite',
                                }}
                              />
                  </div>
                            <div
                              style={{
                                height: 10,
                                width: '70%',
                                borderRadius: 4,
                                background: 'var(--white-06)',
                                position: 'relative',
                                overflow: 'hidden',
                              }}
                            >
                              <div
                                style={{
                                  position: 'absolute',
                                  inset: 0,
                                  background:
                                    'linear-gradient(90deg, transparent, var(--white-20), transparent)',
                                  animation: 'etg-shimmer 2s infinite',
                                }}
                              />
                  </div>
                </div>
              )}
              {interactions
                          .filter(
                            (it) =>
                              it.kind === 'discovery.query' ||
                              it.kind === 'discovery.answer',
                          )
                .map((it) => {
                  const isUser = it.kind === 'discovery.query';
                  const containerStyle: React.CSSProperties = {
                              backgroundColor: isUser
                                ? 'var(--brand-10)'
                                : 'transparent',
                              border: isUser
                                ? '1px solid var(--brand-25)'
                                : 'none',
                    maxWidth: '75%',
                  };
                  return (
                              <div
                                key={it.id}
                                className={`rounded-2xl px-3 py-2 ${isUser ? 'ml-auto' : ''}`}
                                style={containerStyle}
                              >
                                <div
                                  className="text-[10px] mb-1"
                                  style={{ color: 'var(--text-tertiary-weak)' }}
                                >
                                  {isUser ? 'you' : 'assistant'}
                                </div>
                                <div
                                  className="text-xs"
                                  style={{
                                    color: 'var(--text-heading)',
                                    whiteSpace: 'pre-wrap',
                                    wordBreak: 'break-word',
                                  }}
                                >
                        {it.content}
                      </div>
                    </div>
                  );
                          })}
            </div>
          </div>

          {/* Status indicator */}
          {(running || entitiesAnalyzing) && (
            <div className="px-1 pb-2">
                        <div
                          className="text-xs flex items-center gap-2"
                          style={{
                            textAlign: 'left',
                            color: 'var(--text-secondary-strong)',
                          }}
                        >
                          <span
                            className="animate-pulse"
                            style={{
                              width: '8px',
                              height: '8px',
                              borderRadius: '9999px',
                              backgroundColor: 'var(--text-secondary-strong)',
                            }}
                          />
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
                            await sendChatMessage(
                              currentProject.id,
                              queryText,
                              lang,
                            );
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
        <TelegramAddModal
          isOpen={showAddTelegramModal}
          onClose={() => setShowAddTelegramModal(false)}
          input={tgInput}
          onInputChange={setTgInput}
          onSubmit={addTelegramSource}
          isSubmitting={tgSubmitting}
          error={tgError}
        />
        </>
        )}
        </div>
        </div>

        {/* Right drawer: appears when any drawer is open */}
        {rightPanelType !== null &&
          viewMode !== 'graph' &&
          !showAccount &&
          !showSettings && (
          <>
            {/* Mobile overlay backdrop */}
            {isMobile && (
              <div
                  onClick={closeAllRightPanels}
                style={{
                  position: 'fixed',
                  top: 0,
                  left: 0,
                  right: 0,
                  bottom: 0,
                  backgroundColor: 'rgba(0, 0, 0, 0.5)',
                  zIndex: 40,
                }}
              />
            )}
            <div
              ref={drawerRef}
              className="min-h-0 flex flex-col drawer-resizable"
              style={{
                width: `${drawerWidth}px`,
                minWidth: '300px',
                maxWidth: '800px',
                flexShrink: 0,
                flexGrow: 0,
                position: isMobile ? 'fixed' : 'relative',
                top: isMobile ? 0 : 'auto',
                right: isMobile ? 0 : 'auto',
                bottom: isMobile ? 0 : 'auto',
                height: '100%',
                maxHeight: '100%',
                overflow: 'hidden',
                zIndex: isMobile ? 50 : 'auto',
                  backgroundColor: isMobile
                    ? 'var(--bg-sidebar)'
                    : 'transparent',
                  ...(isMobile
                    ? {
                  maxWidth: '85vw',
                  borderRadius: 0,
                      }
                    : {}),
              }}
            >
              {/* Resize handle on left edge */}
              {!isMobile && (
              <div
                onMouseDown={(e) => {
                  e.preventDefault();
                  const startX = e.clientX;
                      const startWidth =
                        drawerRef.current?.clientWidth || drawerWidth;
                  
                  const onMove = (ev: MouseEvent) => {
                    const delta = startX - ev.clientX; // Handle on left: moving right (ev.clientX > startX) decreases width
                        const newWidth = Math.max(
                          300,
                          Math.min(800, startWidth + delta),
                        );
                    if (drawerRef.current) {
                      drawerRef.current.style.width = `${newWidth}px`;
                      setDrawerWidth(newWidth);
                    }
                  };
                  
                  const onUp = () => {
                    window.removeEventListener('mousemove', onMove);
                    window.removeEventListener('mouseup', onUp);
                  };
                  
                  window.addEventListener('mousemove', onMove);
                  window.addEventListener('mouseup', onUp);
                }}
                style={{
                  position: 'absolute',
                  left: -4,
                  top: 0,
                  bottom: 0,
                  width: '8px',
                  cursor: 'col-resize',
                  zIndex: 10,
                }}
              />
              )}
              {/* Drawer content */}
                <div
                  className="min-h-0 flex flex-col"
                  style={{
                    background: 'transparent',
                    height: '100%',
                    overflow: 'hidden',
                  }}
                >
            {/* Right panel container - ALWAYS in DOM, never removed */}
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                      flex: rightPanelType !== null ? 1 : 0,
                      minWidth: rightPanelType !== null ? 'auto' : 0,
                      visibility:
                        rightPanelType !== null ? 'visible' : 'hidden',
                      pointerEvents: rightPanelType !== null ? 'auto' : 'none',
                height: '100%',
                minHeight: 0,
                overflow: 'hidden',
              }}
            >
              {/* Empty spacer matching Chat | Perspectives toggle height - placed before entire panel to shift it down */}
                    <div
                      className="flex items-center justify-center gap-1 mb-2 w-full"
                      role="tablist"
                      aria-label="Spacer"
                      style={{
                        visibility: 'hidden',
                        pointerEvents: 'none',
                        height: '28px',
                      }}
                    >
                <div className="px-2 py-1 text-12 rounded" />
                <div className="px-2 py-1 text-12 rounded" />
              </div>
              <div
                className="mt-3 p-3"
                style={{
                  display: 'flex',
                  flexDirection: 'column',
                  flex: 1,
                  minHeight: 0,
                  overflowY: 'auto',
                }}
              >
                      <div
                        className="flex items-center justify-between"
                        style={{
                          position: 'sticky',
                          top: 0,
                          paddingBottom: 8,
                          marginBottom: 8,
                          zIndex: 1,
                        }}
                      >
                        <h3
                          className="text-13 font-medium"
                          style={{ color: 'var(--text-heading)' }}
                        >
                          {rightPanelType === 'artifacts'
                            ? 'Workspace Artifacts'
                            : rightPanelType === 'experimental'
                              ? 'Experimental Response'
                              : rightPanelType === 'analysis'
                                ? t('unfold.sections.analysis')
                                : rightPanelType === 'context'
                                  ? t('unfold.sections.contextNavigation', {
                                      defaultValue: 'Context Navigation',
                                    })
                                  : 'Workspace'}
                    </h3>
                    <button
                      type="button"
                      aria-label="Close right panel"
                      title="Close"
                      className="p-2 rounded-lg transition-colors"
                          style={{
                            backgroundColor: 'transparent',
                            color: 'var(--text-secondary-strong)',
                          }}
                          onMouseEnter={(e) => {
                            (
                              e.currentTarget as HTMLButtonElement
                            ).style.backgroundColor = 'var(--white-10)';
                          }}
                          onMouseLeave={(e) => {
                            (
                              e.currentTarget as HTMLButtonElement
                            ).style.backgroundColor = 'transparent';
                          }}
                          onClick={closeAllRightPanels}
                        >
                          <svg
                            className="w-4 h-4"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                          >
                            <path
                              strokeLinecap="round"
                              strokeWidth={2}
                              d="M6 6l12 12M18 6L6 18"
                            />
                          </svg>
                    </button>
                  </div>
                  
                  {/* Workspace Artifacts toolbar (top) */}
                  {rightPanelType === 'artifacts' && (
                    <ArtifactsPanelToolbar
                      onAnalyzeEntities={handleAnalyzeEntities}
                      onAnalyzeFacts={handleAnalyzeFacts}
                      onAnalyzeEvents={handleAnalyzeEvents}
                      onAnalyzeMetrics={handleAnalyzeMetrics}
                      onAnalyzeDecisions={handleAnalyzeDecisions}
                    />
                  )}
                  
                  {/* Workspace Library toolbar (top) - now handled by WorkspacePanel */}

                      {/* Analysis tabs (User / Flow) - now handled by AnalysisPanel */}

                      {/* Panel content - always render all panels, hide inactive ones */}
                      <div
                        style={{
                          flex: 1,
                          minHeight: 0,
                          overflowY: 'auto',
                          paddingRight: 4,
                        }}
                      >
                        {/* Workspace Panel */}
                        {rightPanelType === 'workspace' && (
                          <div style={getPanelVisibilityStyles(true)}>
                            <WorkspacePanel
                              sources={sources}
                              currentProjectId={currentProject?.id || null}
                              addMenuRef={addMenuRef}
                              showAddSourceMenu={showAddSourceMenu}
                              onShowAddSourceMenuChange={setShowAddSourceMenu}
                              onShowAddTelegramModalChange={setShowAddTelegramModal}
                              onPreviewSource={setPreviewSource}
                              onOpenDocument={setActiveDoc}
                            />
                          </div>
                        )}

                        {/* Analysis Panel */}
                        {rightPanelType === 'analysis' && (
                          <div style={getPanelVisibilityStyles(true)}>
                            <AnalysisPanel
                              analysisTab={analysisTab}
                              onAnalysisTabChange={setAnalysisTab}
                              onFirstResultUser={() => {
                                setBadgeUser(true);
                                setAvailUserAnalysis(true);
                              }}
                              onRiskUpdate={(r) => {
                                const high = !!(r && r.level === 'High');
                                setIsHighRisk(high);
                                if (high) {
                                  setRiskBanner((prev) =>
                                    prev && prev.visible === false
                                      ? prev
                                      : {
                                          visible: true,
                                          level: 'High',
                                          rationale: r?.rationale || '',
                                        },
                                  );
                                } else if (!r) {
                                  setRiskBanner(null);
                                }
                              }}
                              onFirstResultDiscussionFlow={onFirstResultDiscussionFlow}
                              onAddTextTask={handleAddTextTask}
                              onRemoveTextTask={handleRemoveTextTask}
                              textTasks={textTasks}
                            />
                          </div>
                        )}

                        {/* Workspace Artifacts Panel */}
                        {rightPanelType === 'artifacts' && (
                          <div style={getPanelVisibilityStyles(true)}>
                            <FindingsPanel projectId={genaiChat.projectId} />
                          </div>
                        )}

                        {/* Experimental Response Panel */}
                        {rightPanelType === 'experimental' && (
                          <div style={getPanelVisibilityStyles(true)}>
                            <ExperimentalPanel
                              response={experimentalResponse}
                              loading={experimentalLoading}
                            />
                          </div>
                        )}

                        {/* Context Navigation Panel */}
                        {rightPanelType === 'context' && (
                          <div style={getPanelVisibilityStyles(true)}>
                            <ContextPanel
                              sectionStarts={sectionStarts}
                              pinnedInteractions={pinnedInteractions}
                              messagesRef={messagesRef}
                              scrollToInteraction={scrollToInteraction}
                              truncateCenterWords={truncateCenterWords}
                              onRemoveNote={handleRemoveNote}
                            />
                          </div>
                        )}
                      </div>
                    </div>
            </div>

            {/* Bottom slot: reserved area to match left column layout - removed to eliminate empty space */}
          </div>
            </div>
          </>
        )}

        {/* Right buttons column - always visible */}
        <RightPanelButtons
          rightPanelType={rightPanelType}
          onPanelTypeChange={setRightPanelType}
          hasPinned={hasPinned}
          hasSections={hasSections}
          availUserAnalysis={availUserAnalysis}
        />
      </div>
    </div>
  );
};

export default DiscoveryPage;
