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
import { DynamicContentPanel } from '../components/DynamicContentPanel';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useGenAISettings } from '../stores/genaiSettingsStore';
import { processScenarioQuery } from '../lib/scenarioDataHelpers';
import { useScenarioDataStore } from '../stores/scenarioDataStore';
import { ScenariosContent } from '../components/ScenariosContent';
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
import { Graph, useGraphHasNodes } from '../components/Graph';
import { GraphProcessingIndicator } from '../components/GraphProcessingIndicator';
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
import { getGraphSystemPrompt } from '../lib/graphSystemPrompt';
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
import { useSourcesManagement } from '../hooks/useSourcesManagement';
import { useProjectsManagement } from '../hooks/useProjectsManagement';
import { usePanelsManagement } from '../hooks/usePanelsManagement';
import type { ProjectScenario } from '../types/projectScenario';
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
import { RiskBanner } from '../components/RiskBanner';
import { ThinkingPlaceholder } from '../components/ThinkingPlaceholder';
import { SettingsModal } from '../components/SettingsModal';
import { AccountModal } from '../components/AccountModal';
import { WorkspaceCanvas } from '../components/WorkspaceCanvas';
import { TelegramAddModal } from '../components/TelegramAddModal';
import { RSSAddModal } from '../components/RSSAddModal';
import { ChatMessagesList } from '../components/ChatMessagesList';
import { RightDrawer } from '../components/RightDrawer';
import { AgentJobDetails } from '../components/AgentJobDetails';

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
    showAddRSSModal,
    setShowAddRSSModal,
    rssInput,
    setRssInput,
    rssSubmitting,
    rssError,
    addRSSSource,
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
  
  const [queryText, setQueryText] = useState('');
  const [sideOpen, setSideOpen] = useState(false);
  const [running, setRunning] = useState(false);
  const [thinkingSeconds, setThinkingSeconds] = useState(0);
  const [entitiesAnalyzing, setEntitiesAnalyzing] = useState(false);
  const [unfoldSubView, setUnfoldSubView] = useState<'chat' | 'perspectives' | 'models' | 'pages' | 'scenarios'>(
    'chat',
  );
  const [selectedScenarioId, setSelectedScenarioId] = useState<string | null>(null);
  // Check if Graph has nodes (for empty state)
  const graphHasNodes = useGraphHasNodes();
  const processingCommands = useGenaiChatStore((s) => s.processingCommands);
  const commandsProgress = useGenaiChatStore((s) => s.commandsProgress);
  
  // Get random empty state message (selected once per language)
  // Use graphEmptyState for Graph tab, chatEmptyState for Discussion tab
  const emptyStateMessage = React.useMemo(() => {
    const key = unfoldSubView === 'models' 
      ? 'unfold.perspectives.graphEmptyState.descriptions'
      : 'unfold.perspectives.chatEmptyState.descriptions';
    const emptyMessages = t(key, {
      returnObjects: true,
    }) as string[];
    if (Array.isArray(emptyMessages) && emptyMessages.length > 0) {
      return emptyMessages[Math.floor(Math.random() * emptyMessages.length)];
    }
    return emptyMessages?.[0] || (unfoldSubView === 'models' ? 'Ready to map some connections?' : 'Ready to start a conversation?');
  }, [i18n.language, t, unfoldSubView]);
  const [currentPerspectivePreset, setCurrentPerspectivePreset] =
    useState<string>('consulting');
  const [isAnalysisClosed, setIsAnalysisClosed] = useState(false);
  const [unfoldMode, setUnfoldMode] = React.useState<'text' | 'graph' | 'analytics' | 'agent' | 'analyze' | 'scenarios' | 'sources'>('text');
  const unfoldRequestRef = React.useRef<HTMLDivElement | null>(null);
  const unfoldSubViewRef = React.useRef(unfoldSubView);
  React.useEffect(() => {
    unfoldSubViewRef.current = unfoldSubView;
  }, [unfoldSubView]);
  const messagesRef = useRef<HTMLDivElement | null>(null);

  // Scroll functions for chat navigation
  const handleScrollToTop = React.useCallback(() => {
    if (messagesRef.current) {
      messagesRef.current.scrollTo({ top: 0, behavior: 'smooth' });
    }
  }, []);

  const handleScrollToBottom = React.useCallback(() => {
    if (messagesRef.current) {
      messagesRef.current.scrollTo({ top: messagesRef.current.scrollHeight, behavior: 'smooth' });
    }
  }, []);
  const discoveryTimeoutRef = useRef<number | null>(null);
  const [selectedJobTaskId, setSelectedJobTaskId] = React.useState<string | null>(null);
  
  // Destructure modals hook
  const {
    showSettings,
    showAccount,
    showWorkspace,
    workspaceProjectId,
    settingsParams,
    staticPage,
    setStaticPage,
    isAdmin,
    openSettings,
    openAccount,
    openAdmin,
    openWorkspace,
    closeAllModals,
  } = modals;

  const handleOpenSettings = React.useCallback((params?: { initialTab?: 'personal' | 'corporate' | 'guardians'; highlightCheckbox?: 'disableBiasEvaluation' | 'disableFrameExpansion' | 'disableComplianceEvaluation' }) => {
    openSettings(params);
  }, [openSettings]);
  
  // Destructure chat timers hook
  const { genaiThinkingSeconds, averageTimingSeconds } = chatTimers;
  const correlationRef = useRef<string | null>(null);
  const lastDocEvent = useDiscoveryStore((s: any) => (s as any).lastDocEvent);
  React.useEffect(() => {
    try {
      if (!lastDocEvent) return;
      
      // Skip RAG document events (from sources) - they have documentId, sourceId, externalId
      // We only care about CoreDoc events (project documents) which have docId, filename
      if ((lastDocEvent as any).documentId || (lastDocEvent as any).sourceId) {
        // This is a RAG document event, not a CoreDoc event - skip it
        return;
      }
      
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
  // Исключаем сообщения из Graph вкладки, когда мы в Discussion вкладке
  const mergedMessages = useMergedMessages(unfoldSubView === 'chat');
  
  const genaiChatInteractionsLength = useGenaiChatStore(
    (state) => state.interactions.length,
  );
  const genaiChatMessagesLength = useGenaiChatStore(
    (state) => state.messages.length,
  );
  const genaiChatHydrating = useGenaiChatStore((state) => state.hydrating);
  const genaiChatStreaming = useGenaiChatStore((state) => state.streaming);
  // Track only user interactions count
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

  // Auto-switch from Dynamic Content, Perspectives, or Scenarios tabs if project becomes empty
  React.useEffect(() => {
    const isEmpty = !genaiChatProjectId || genaiChatProjectId === 'local-unfold' || (genaiChatInteractionsLength === 0 && genaiChatMessagesLength === 0);
    if (isEmpty && (unfoldSubView === 'pages' || unfoldSubView === 'perspectives' || unfoldSubView === 'scenarios')) {
      try {
        setUnfoldSubView('chat');
      } catch {}
    }
  }, [genaiChatProjectId, genaiChatInteractionsLength, genaiChatMessagesLength, unfoldSubView]);

  // Reset analysis closed state when preset changes or switching to perspectives
  React.useEffect(() => {
    setIsAnalysisClosed(false);
  }, [currentPerspectivePreset, unfoldSubView]);

  // Reload interactions when switching from scenarios to chat to show scenario modification messages
  const prevUnfoldSubViewRef = React.useRef<'chat' | 'perspectives' | 'models' | 'pages' | 'scenarios' | undefined>(undefined);
  React.useEffect(() => {
    const prevUnfoldSubView = prevUnfoldSubViewRef.current;
    prevUnfoldSubViewRef.current = unfoldSubView;
    
    // Only reload if switching from scenarios to chat
    if (prevUnfoldSubView === 'scenarios' && unfoldSubView === 'chat' && genaiChatProjectId && genaiChatProjectId !== 'local-unfold') {
      const store = useGenaiChatStore.getState();
      // Only reload if not already hydrating to avoid duplicate calls
      if (!store.hydrating) {
        console.log('[DiscoveryPage] Reloading interactions after switching to chat from scenarios');
        store.hydrateInteractions();
      }
    }
    
    // Reset selected scenario when switching away from scenarios mode
    if (prevUnfoldSubView === 'scenarios' && unfoldSubView !== 'scenarios') {
      setSelectedScenarioId(null);
    }
  }, [unfoldSubView, genaiChatProjectId]);
  
  // Reset selected scenario when project changes
  React.useEffect(() => {
    setSelectedScenarioId(null);
  }, [genaiChatProjectId]);

  // Load scenario data when a scenario is selected
  React.useEffect(() => {
    if (!selectedScenarioId || unfoldSubView !== 'scenarios' || !genaiChatProjectId) {
      return;
    }

    // Skip loading for 'scenarios-main' as it's used for new queries
    if (selectedScenarioId === 'scenarios-main') {
      return;
    }

    const loadScenarioData = async () => {
      try {
        console.log('[DiscoveryPage] Loading scenario data:', { projectId: genaiChatProjectId, scenarioId: selectedScenarioId });
        const data = await api.getScenarioData(genaiChatProjectId, selectedScenarioId);
        console.log('[DiscoveryPage] Loaded scenario data:', data);
        
        // Convert backend format to frontend format and add to store
        // Store will handle duplicates by ID automatically
        const scenarioDataStore = useScenarioDataStore.getState();
        data.forEach((item: any) => {
          // Convert backend format to frontend ScenarioData format
          const frontendData: import('../types/scenarioData').ScenarioData = {
            id: item.id,
            scenarioId: item.scenarioId,
            projectId: item.projectId,
            userQuery: item.userQuery,
            databaseSchema: item.databaseSchema,
            executionStages: item.executionStages,
            interpretation: {
              queries: item.interpretation.queries || [],
              executedAt: typeof item.interpretation.executedAt === 'string' 
                ? item.interpretation.executedAt 
                : (item.interpretation.executedAt ? new Date(item.interpretation.executedAt).toISOString() : new Date().toISOString()),
            },
            results: item.results || [],
            isComposite: item.isComposite || false,
            createdAt: typeof item.createdAt === 'string' 
              ? item.createdAt 
              : (item.createdAt ? new Date(item.createdAt).toISOString() : new Date().toISOString()),
            updatedAt: typeof item.updatedAt === 'string' 
              ? item.updatedAt 
              : (item.updatedAt ? new Date(item.updatedAt).toISOString() : new Date().toISOString()),
          };
          scenarioDataStore.addScenarioData(frontendData);
        });
      } catch (error) {
        console.error('[DiscoveryPage] Error loading scenario data:', error);
      }
    };

    loadScenarioData();
  }, [selectedScenarioId, unfoldSubView, genaiChatProjectId]);

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
    queuedTasks,
    textTasks,
    textTasksApplying,
    handleAddTextTask,
    handleRemoveTextTask,
    handleApplyTextTasks,
    handleApplyTasks,
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

  // Detect smartphone (smaller than Galaxy Fold, typically < 600px width)
  // Rules:
  // 1. Desktop (> 768px): desktop interface
  // 2. Tablet/Galaxy Fold (600-768px): fold mode (medium) - reduced but no major simplifications
  // 3. Small smartphone (< 600px): simplified smartphone interface (both portrait and landscape)
  //    Simplified interface is especially useful in portrait orientation
  const [isSmartphone, setIsSmartphone] = React.useState(false);
  React.useEffect(() => {
    const checkSmartphone = () => {
      // Simplified smartphone interface for screens <= 600px (both orientations)
      setIsSmartphone(window.innerWidth <= 600);
    };
    checkSmartphone();
    window.addEventListener('resize', checkSmartphone);
    // Also listen to orientation changes to update when device rotates
    if (window.screen?.orientation) {
      window.screen.orientation.addEventListener('change', checkSmartphone);
      return () => {
        window.removeEventListener('resize', checkSmartphone);
        window.screen.orientation.removeEventListener('change', checkSmartphone);
      };
    }
    return () => window.removeEventListener('resize', checkSmartphone);
  }, []);

  // State for mode dropdown on smartphones
  const [showModeDropdown, setShowModeDropdown] = React.useState(false);
  const modeDropdownRef = React.useRef<HTMLDivElement>(null);

  // Close mode dropdown on outside click
  React.useEffect(() => {
    if (!showModeDropdown) return;
    function onDocMouseDown(e: MouseEvent) {
      const dropdown = modeDropdownRef.current;
      const target = e.target as Node | null;
      if (dropdown && dropdown.contains(target as Node)) return;
      setShowModeDropdown(false);
    }
    document.addEventListener('mousedown', onDocMouseDown, { capture: true });
    return () => document.removeEventListener('mousedown', onDocMouseDown, { capture: true } as any);
  }, [showModeDropdown]);
  
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
  
  // Sync drawer width when resized natively - now handled by RightDrawer
  // const drawerRef = useRef<HTMLDivElement>(null);
  const lastUserMessageIdRef = React.useRef<string | null>(null);
  // Active document viewer state
  const [activeDoc, setActiveDoc] = useState<null | {
    id: string;
    filename: string;
    contentType?: string;
    sizeBytes?: number;
    parseStatus?: string;
    docType?: string;
    artifactConfig?: any;
    extractedText?: string;
    projectId?: string;
  }>(null);
  
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
  const interactionsLoadedRef = React.useRef<string | null>(null);
  
  React.useEffect(() => {
    try {
      if (
        projectId &&
        projectId !== 'local-unfold' &&
        interactionsLoadedRef.current !== projectId
      ) {
        console.log('[DiscoveryPage] Calling hydrateInteractions', { 
          projectId, 
          currentRef: interactionsLoadedRef.current 
        });
        interactionsLoadedRef.current = projectId;
        // Use getState() to get stable reference to function
        const store = useGenaiChatStore.getState();
        // Don't set hydrating here - let hydrateInteractions set it
        // This prevents the "Already hydrating" check from skipping the call
        console.log('[DiscoveryPage] Calling hydrateInteractions', { 
          projectId,
          currentHydrating: store.hydrating,
          hydratingProjectId: (store as any).hydratingProjectId
        });
        store.hydrateInteractions();
      } else if (!projectId || projectId === 'local-unfold') {
        interactionsLoadedRef.current = null;
      }
    } catch (e) {
      console.error('[DiscoveryPage] Error in hydrateInteractions effect', e);
    }
  }, [projectId]); // Removed hydrateInteractions from dependencies - use getState() instead


  // Track pinned status changes by subscribing to a hash of pinned interaction IDs
  // This ensures pinnedInteractions updates even when interactions array length doesn't change
  const pinnedIdsHash = useGenaiChatStore((s) => {
    try {
      const interactions = s.interactions || [];
      const pinnedIds = interactions
        .filter((it: any) => {
          // Exclude perspective interactions
          if (
            it.kind === 'note' &&
            typeof it.subkind === 'string' &&
            it.subkind.startsWith('perspective.')
          ) {
            return false;
          }
          if (
            it.kind === 'system' &&
            typeof it.subkind === 'string' &&
            it.subkind.startsWith('perspective.') &&
            it.subkind.endsWith('.analysis')
          ) {
            return false;
          }
          return !!it?.metadata?.pinned;
        })
        .map((it: any) => it.id)
        .sort()
        .join(',');
      return pinnedIds;
    } catch {
      return '';
    }
  });

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
        // Exclude queued tasks interactions (they are metadata, not chat content)
        if (
          it.kind === 'note' &&
          typeof it.subkind === 'string' &&
          it.subkind === 'queued.tasks'
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
  }, [getInteractions, genaiChatInteractionsLength, pinnedIdsHash]);
  
  // Use pinnedInteractions.length instead of separate computation
  // This ensures hasPinned updates correctly when interactions are pinned/unpinned
  const hasPinned = React.useMemo(() => {
    return pinnedInteractions.length > 0;
  }, [pinnedInteractions.length]);
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

  // Track last loaded projectId to prevent duplicate calls
  const lastLoadedProjectRef = React.useRef<string | null>(null);
  
  useEffect(() => {
    const rawPid = currentProject?.id;
    if (!rawPid) {
      // Clear ref when no project selected
      lastLoadedProjectRef.current = null;
      return;
    }
    
    // Ensure projectId is always a string (handle cases where id might be an object)
    const pid = typeof rawPid === 'string' ? rawPid : String(rawPid);
    
    // Skip if this is the same project we already loaded (prevent unnecessary re-processing)
    if (lastLoadedProjectRef.current === pid && currentProject?.id === pid) {
      return;
    }
    
    try {
      const st = useGenaiChatStore.getState();
      const wasDifferentProject = st.projectId !== pid;
      
      console.log('[DiscoveryPage] currentProject changed', { 
        currentProjectId: pid,
        genaiChatProjectId: st.projectId,
        wasDifferent: wasDifferentProject,
        lastLoaded: lastLoadedProjectRef.current
      });
      
      // Always update projectId when currentProject changes to a different project
      // This ensures switching between projects works correctly
      // CRITICAL: Skip if projectId already matches (prevents race condition when project is just created)
      // This can happen when ensureProject sets projectId and then sets currentProject
      if (wasDifferentProject) {
        // Double-check projectId hasn't changed since we read it (race condition protection)
        const currentPid = useGenaiChatStore.getState().projectId;
        if (currentPid === pid) {
          console.log('[DiscoveryPage] ProjectId already matches, skipping update (race condition protection)', { 
            projectId: pid 
          });
          // Still update lastLoadedProjectRef to prevent duplicate fetches
          if (lastLoadedProjectRef.current !== pid) {
            lastLoadedProjectRef.current = pid;
          }
          return;
        }
        
        console.log('[DiscoveryPage] Switching project', { 
          from: st.projectId, 
          to: pid,
          currentPid: currentPid
        });
        
        // When switching to a different project, always clear messages and interactions
        // to prevent showing old messages from previous project
        const hasActiveContent = st.messages.length > 0 || st.interactions.length > 0 || st.streaming;
        
        if (hasActiveContent) {
          // If there's active content, clear it when switching projects
          // This prevents showing old messages from previous project
          console.log('[DiscoveryPage] Clearing active content when switching projects', { 
            messagesCount: st.messages.length, 
            interactionsCount: st.interactions.length,
            streaming: st.streaming,
            fromProject: st.projectId,
            toProject: pid
          });
          // Update projectId, clear interactions and messages (they're from old project), and set hydrating to show skeleton
          // CRITICAL: Clear currentStreamId when switching projects to avoid filtering events from wrong project
          // For local-unfold, don't set hydrating=true since hydrateInteractions won't be called
          const isLocalUnfold = pid === 'local-unfold';
          useGenaiChatStore.setState({ 
            projectId: pid, 
            interactions: [], // Clear interactions from old project
            messages: [], // Clear messages from old project
            hydrating: isLocalUnfold ? false : true, 
            hydratingProjectId: isLocalUnfold ? undefined : pid,
            currentStreamId: undefined, // Clear stream tracking when switching projects
            streaming: false, // Stop streaming when switching projects
          } as any);
        } else {
          // Clear local UI states when switching to a different project with no active content
          // Set new project ID (this clears chat state in store and sets hydrating: false)
          // hydrateInteractions will set hydrating: true when it starts loading
          st.setProjectId(pid);
          // Don't call resetChatStateWithCleanup here as it calls setCurrentProject(null)
          // Just clear local state manually
          const discoveryStore = useDiscoveryStore.getState();
          discoveryStore.clearSources();
        }
        
        // Reset last loaded ref when switching projects to allow reload
        lastLoadedProjectRef.current = null;
      }
      
      // Only fetch if we haven't already loaded this project
      if (lastLoadedProjectRef.current !== pid) {
        console.log('[DiscoveryPage] Fetching project data', { pid });
        
        // Mark as loading to prevent duplicate calls
        lastLoadedProjectRef.current = pid;
        
        // Use getState() to get stable references to functions
        const discoveryStore = useDiscoveryStore.getState();
        discoveryStore.fetchProjectSources(pid);
        discoveryStore.fetchProjectInteractions(pid);
        // hydrateInteractions is already called in the other useEffect above, no need to duplicate
      }
    } catch (e) {
      console.error('[DiscoveryPage] Error switching project', e);
    }
  }, [currentProject?.id, resetChatStateWithCleanup]); // Removed function dependencies - use getState() instead

  // Handle case when currentProject is cleared
  useEffect(() => {
    if (!currentProject?.id) {
      lastLoadedProjectRef.current = null;
      const discoveryStore = useDiscoveryStore.getState();
      discoveryStore.clearSources();
    }
  }, [currentProject?.id, resetChatStateWithCleanup]); // Removed function dependencies - use getState() instead

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
  
  // Scroll to assistant thinking placeholder when streaming starts (especially for Second Opinion from compliance eval)
  const lastStreamingMessageIdRef = React.useRef<string | null>(null);
  const lastUpdateTimestamp = useGenaiChatStore((state) => (state as any)._lastUpdate || 0);
  useEffect(() => {
    // Only in chat view
    if (unfoldSubView !== 'chat') return;
    
    // Check if streaming just started and there's a new assistant message without content
    if (genaiChat.streaming && messagesRef.current) {
      const messages = genaiChat.messages || [];
      const lastAssistantMessage = messages.length > 0 
        ? messages[messages.length - 1]
        : null;
      
      // If last message is assistant and has no content (thinking...), scroll to it
      if (lastAssistantMessage && 
          lastAssistantMessage.role === 'assistant' && 
          (!lastAssistantMessage.content || !lastAssistantMessage.content.trim()) &&
          lastAssistantMessage.id !== lastStreamingMessageIdRef.current) {
        // Mark this message as processed
        lastStreamingMessageIdRef.current = lastAssistantMessage.id;
        
        // Use timeout to ensure DOM is fully updated
        const timeoutId = window.setTimeout(() => {
          if (messagesRef.current) {
            // Find all ChatMessage elements (they have rounded-2xl class)
            const chatMessages = messagesRef.current.querySelectorAll('.rounded-2xl');
            if (chatMessages.length > 0) {
              // Get the last message (should be the thinking placeholder)
              const lastMessage = chatMessages[chatMessages.length - 1] as HTMLElement;
              if (lastMessage) {
                lastMessage.scrollIntoView({ 
                  behavior: 'smooth', 
                  block: 'start',
                  inline: 'nearest'
                });
              }
            } else {
              // Fallback: scroll to bottom
              messagesRef.current.scrollTop = messagesRef.current.scrollHeight;
            }
          }
        }, 150);
        
        return () => clearTimeout(timeoutId);
      }
    } else if (!genaiChat.streaming) {
      // Reset when streaming stops
      lastStreamingMessageIdRef.current = null;
    }
  }, [genaiChat.streaming, genaiChat.messages.length, lastUpdateTimestamp, unfoldSubView]);
  
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
      const rawProjectId = currentProject?.id;
      // Ensure projectId is always a string (handle cases where id might be an object)
      const projectId = rawProjectId ? (typeof rawProjectId === 'string' ? rawProjectId : String(rawProjectId)) : '';
      
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
      const mode = e?.detail?.mode as 'chat' | 'perspectives' | 'models' | 'pages';
      if (mode && ['chat', 'perspectives', 'models', 'pages'].includes(mode)) {
        setUnfoldSubView(mode);
      }
    };
    window.addEventListener('ui.switchMode', handler as any);
    return () => window.removeEventListener('ui.switchMode', handler as any);
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
        onShowWorkspace={openWorkspace}
        onShowSettings={openSettings}
        onShowAccount={openAccount}
        onShowAdmin={() => {
          closeAllModals();
          useUIStore.getState().openAdminModal();
        }}
        onCloseAllModals={closeAllModals}
        editingProjectId={editingProjectId}
        editingProjectName={editingProjectName}
        editingInputRef={editingInputRef}
        onEditingProjectIdChange={setEditingProjectId}
        onEditingProjectNameChange={setEditingProjectName}
        onDeleteChat={handleDeleteChat}
        onRenameChat={handleRenameChat}
        onStartEditingProject={startEditingProject}
        currentMode={unfoldSubView}
        projectId={genaiChat.projectId}
        onOpenDocument={setActiveDoc}
        onSelectJob={setSelectedJobTaskId}
        selectedJobTaskId={selectedJobTaskId}
        selectedScenarioId={selectedScenarioId}
        onSelectScenario={setSelectedScenarioId}
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
        {/* 2% left margin for breathing room (hidden on smartphones) */}
        {!isSmartphone && <div style={{ width: '2%', minWidth: '2%', flexShrink: 0 }} />}

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
            height: '100%',
            overflow: 'hidden',
          }}
        >
        {/* Document viewer overlay - renders above everything including WorkspaceCanvas */}
        {activeDoc && (
          <DocumentViewer
            projectId={activeDoc.projectId || genaiChat.projectId || ''}
            doc={activeDoc}
            onClose={() => setActiveDoc(null)}
          />
        )}
        {showAccount ? (
          <AccountModal
            isOpen={showAccount}
            onClose={closeAllModals}
          />
        ) : showSettings ? (
          <SettingsModal
            isOpen={showSettings}
            onClose={closeAllModals}
            initialTab={settingsParams?.initialTab}
            highlightCheckbox={settingsParams?.highlightCheckbox}
          />
        ) : showWorkspace ? (
          <WorkspaceCanvas
            isOpen={showWorkspace}
            projectId={workspaceProjectId}
            onClose={closeAllModals}
            onOpenDocument={setActiveDoc}
          />
        ) : selectedJobTaskId ? (
          <AgentJobDetails
            isOpen={selectedJobTaskId !== null}
            taskId={selectedJobTaskId}
            onClose={() => setSelectedJobTaskId(null)}
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
                    className={`w-full flex-1 flex flex-col min-h-0 ${isSmartphone ? '' : 'px-4'}`}
                    style={{ position: 'relative' }}
                  >
            {/* Metric overlay - replaces chat area when open */}
            <MetricOverlay />
            {/* Unfold subview toggle */}
            <div className="w-fit self-start mb-3">
              {isSmartphone ? (
                /* Dropdown for smartphones */
                <div ref={modeDropdownRef} style={{ position: 'relative' }}>
                  <button
                    type="button"
                    aria-label="Select view mode"
                    aria-expanded={showModeDropdown}
                    onClick={() => setShowModeDropdown(!showModeDropdown)}
                    className="text-xs rounded flex items-center gap-1 px-3 py-1.5"
                    style={{
                      border: '1px solid var(--overlay-line-90)',
                      background: 'var(--bg-input)',
                      color: 'var(--text-heading)',
                    }}
                  >
                    <span>
                      {unfoldSubView === 'chat' ? t('unfold.sections.discuss', { defaultValue: 'Discussion' }) :
                       unfoldSubView === 'models' ? t('unfold.sections.model', { defaultValue: 'Graph' }) :
                       unfoldSubView === 'perspectives' ? t('unfold.sections.perspectives') :
                       unfoldSubView === 'pages' ? t('unfold.sections.dynamicContent', { defaultValue: 'Dynamic Content' }) :
                       unfoldSubView === 'scenarios' ? t('unfold.sections.scenarios', { defaultValue: 'Scenarios' }) : t('unfold.sections.discuss', { defaultValue: 'Discussion' })}
                    </span>
                    <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                      <path d="M6 9l6 6 6-6" />
                    </svg>
                  </button>
                  {showModeDropdown && (
                    <div
                      className="absolute top-full left-0 mt-1 rounded-lg border shadow-md overflow-hidden z-20"
                      style={{
                        background: 'var(--bg-elevated)',
                        borderColor: 'var(--overlay-line-90)',
                        minWidth: '120px',
                      }}
                    >
                      <button
                        type="button"
                        onClick={() => {
                          setUnfoldSubView('chat');
                          setShowModeDropdown(false);
                        }}
                        className="w-full text-left px-3 py-2 flex items-center gap-2 text-xs"
                        style={{
                          background: unfoldSubView === 'chat' ? 'var(--white-10)' : 'transparent',
                          color: 'var(--text-heading)',
                        }}
                      >
                        {t('unfold.sections.discuss', { defaultValue: 'Discussion' })}
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          setUnfoldSubView('models');
                          setShowModeDropdown(false);
                        }}
                        className="w-full text-left px-3 py-2 flex items-center gap-2 text-xs"
                        style={{
                          background: unfoldSubView === 'models' ? 'var(--white-10)' : 'transparent',
                          color: 'var(--text-heading)',
                        }}
                      >
                        {t('unfold.sections.model', { defaultValue: 'Graph' })}
                      </button>
                      {genaiChatProjectId && genaiChatProjectId !== 'local-unfold' && (genaiChatInteractionsLength > 0 || genaiChatMessagesLength > 0) && (
                        <>
                          {/* Divider */}
                          <div
                            className="h-px w-full"
                            style={{
                              backgroundColor: theme === 'dark' 
                                ? 'rgba(255, 255, 255, 0.2)' 
                                : 'rgba(0, 0, 0, 0.15)',
                              margin: '4px 0',
                              flexShrink: 0,
                            }}
                          />
                          <button
                            type="button"
                            onClick={() => {
                              setUnfoldSubView('perspectives');
                              setShowModeDropdown(false);
                            }}
                            className="w-full text-left px-3 py-2 flex items-center gap-2 text-xs"
                            style={{
                              background: unfoldSubView === 'perspectives' ? 'var(--white-10)' : 'transparent',
                              color: 'var(--text-heading)',
                            }}
                          >
                            {t('unfold.sections.perspectives')}
                          </button>
                        </>
                      )}
                      {genaiChatProjectId && genaiChatProjectId !== 'local-unfold' && (genaiChatInteractionsLength > 0 || genaiChatMessagesLength > 0) && (
                        <button
                          type="button"
                          onClick={() => {
                            setUnfoldSubView('pages');
                            setShowModeDropdown(false);
                          }}
                          className="w-full text-left px-3 py-2 flex items-center gap-2 text-xs"
                          style={{
                            background: unfoldSubView === 'pages' ? 'var(--white-10)' : 'transparent',
                            color: 'var(--text-heading)',
                          }}
                        >
                          {t('unfold.sections.dynamicContent', { defaultValue: 'Dynamic Content' })}
                        </button>
                      )}
                      {genaiChatProjectId && genaiChatProjectId !== 'local-unfold' && (genaiChatInteractionsLength > 0 || genaiChatMessagesLength > 0) && (
                        <button
                          type="button"
                          onClick={() => {
                            setUnfoldSubView('scenarios');
                            setShowModeDropdown(false);
                          }}
                          className="w-full text-left px-3 py-2 flex items-center gap-2 text-xs"
                          style={{
                            background: unfoldSubView === 'scenarios' ? 'var(--white-10)' : 'transparent',
                            color: 'var(--text-heading)',
                          }}
                        >
                          {t('unfold.sections.scenarios', { defaultValue: 'Scenarios' })}
                        </button>
                      )}
                    </div>
                  )}
                </div>
              ) : (
                /* Buttons for larger screens */
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
                    {t('unfold.sections.discuss', { defaultValue: 'Discussion' })}
                  </button>
                  <button
                    type="button"
                    role="tab"
                    aria-selected={unfoldSubView === 'models'}
                    aria-controls="unfold-models-panel"
                    onClick={() => setUnfoldSubView('models')}
                    className="px-2 py-1 text-12 rounded"
                    style={{
                              background:
                                unfoldSubView === 'models'
                                  ? 'var(--white-10)'
                                  : 'transparent',
                              color: 'var(--text-heading)',
                    }}
                  >
                    {t('unfold.sections.model', { defaultValue: 'Graph' })}
                  </button>
                  {genaiChatProjectId && genaiChatProjectId !== 'local-unfold' && (genaiChatInteractionsLength > 0 || genaiChatMessagesLength > 0) && (
                    <>
                      {/* Divider */}
                      <div
                        className="h-4 w-px"
                        style={{
                          backgroundColor: theme === 'dark' 
                            ? 'rgba(255, 255, 255, 0.2)' 
                            : 'rgba(0, 0, 0, 0.15)',
                          margin: '0 8px',
                          flexShrink: 0,
                        }}
                      />
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
                    </>
                  )}
                  {genaiChatProjectId && genaiChatProjectId !== 'local-unfold' && (genaiChatInteractionsLength > 0 || genaiChatMessagesLength > 0) && (
                    <button
                      type="button"
                      role="tab"
                      aria-selected={unfoldSubView === 'pages'}
                      aria-controls="unfold-pages-panel"
                      onClick={() => setUnfoldSubView('pages')}
                      className="px-2 py-1 text-12 rounded"
                      style={{
                                background:
                                  unfoldSubView === 'pages'
                                    ? 'var(--white-10)'
                                    : 'transparent',
                                color: 'var(--text-heading)',
                      }}
                    >
                      {t('unfold.sections.dynamicContent', { defaultValue: 'Dynamic Content' })}
                    </button>
                  )}
                  {genaiChatProjectId && genaiChatProjectId !== 'local-unfold' && (genaiChatInteractionsLength > 0 || genaiChatMessagesLength > 0) && (
                    <>
                      {/* Divider */}
                      <div
                        className="h-4 w-px"
                        style={{
                          backgroundColor: theme === 'dark' 
                            ? 'rgba(255, 255, 255, 0.2)' 
                            : 'rgba(0, 0, 0, 0.15)',
                          margin: '0 8px',
                          flexShrink: 0,
                        }}
                      />
                      <button
                        type="button"
                        role="tab"
                        aria-selected={unfoldSubView === 'scenarios'}
                        aria-controls="unfold-scenarios-panel"
                        onClick={() => setUnfoldSubView('scenarios')}
                        className="px-2 py-1 text-12 rounded"
                        style={{
                                  background:
                                    unfoldSubView === 'scenarios'
                                      ? 'var(--white-10)'
                                      : 'transparent',
                                  color: 'var(--text-heading)',
                        }}
                      >
                        {t('unfold.sections.scenarios', { defaultValue: 'Scenarios' })}
                      </button>
                    </>
                  )}
                </div>
              )}
            </div>
            {/* Perspectives content (full column) */}
            {unfoldSubView === 'perspectives' && (
                      <PerspectivesPanel
                        onPresetChange={setCurrentPerspectivePreset}
                        onOpenDocument={setActiveDoc}
                      />
            )}
            {/* Graph content - React Graph */}
            {unfoldSubView === 'models' && (
              <div 
                className="flex flex-1" 
                style={{ 
                  minHeight: 0, 
                  display: 'flex',
                  height: '100%',
                }}
              >
                {/* Graph - занимает всю ширину */}
                {!graphHasNodes ? (
                  // Empty state для графа
                  <div
                    className="flex-1 flex items-center justify-center"
                    style={{
                      minHeight: 0,
                      height: '100%',
                      padding: isMobile ? '24px 12px' : '48px 24px',
                    }}
                  >
                    {processingCommands || commandsProgress ? (
                      <GraphProcessingIndicator variant="empty-state" />
                    ) : (
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
                    )}
                  </div>
                ) : (
                  <div 
                    className="flex-1" 
                    style={{ 
                      minHeight: 0, 
                      position: 'relative',
                      height: '100%',
                    }}
                  >
                    <Graph />
                    {(processingCommands || commandsProgress) && (
                      <>
                        <GraphProcessingIndicator variant="overlay" />
                        <GraphProcessingIndicator variant="status-bar" />
                      </>
                    )}
                  </div>
                )}
              </div>
            )}
            {/* Dynamic Content content */}
            {unfoldSubView === 'pages' && (
              <DynamicContentPanel
                projectId={genaiChat.projectId}
                onOpenDocument={setActiveDoc}
                onCreateFromTemplate={(templateId) => {
                  // TODO: Implement template-based artifact creation
                  console.log('[DynamicContentPanel] Create from template', templateId);
                }}
                      />
            )}
            {/* Scenarios mode content - show ScenarioData results */}
            {unfoldSubView === 'scenarios' && (
              <div className="flex-1 flex flex-col min-h-0">
                <div
                  className="flex-1 overflow-y-auto overflow-x-hidden px-1 min-h-[160px]"
                  style={{
                    contentVisibility: 'auto',
                    containIntrinsicSize: '0px 600px',
                  }}
                >
                  <ScenariosContent scenarioId={selectedScenarioId || 'scenarios-main'} />
                </div>
                {/* Input for Scenarios mode */}
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
                    scenariosMode={true}
                    onSubmit={async (text) => {
                      // CRITICAL: Check fresh state from store, not stale component state
                      // genaiChat.streaming may be stale due to React batching/closure
                      const currentStreaming = useGenaiChatStore.getState().streaming;
                      if (currentStreaming || !text.trim()) {
                        return;
                      }
                      
                      console.log('[DiscoveryPage] Scenarios mode: processing BigQuery query:', text);
                      const pid = await genaiChat.ensureProject();
                      
                      try {
                        // Show loading state
                        useGenaiChatStore.setState({ streaming: true, error: null });
                        
                        // Use selected scenario ID or fallback to default
                        const targetScenarioId = selectedScenarioId || 'scenarios-main';
                        console.log('[DiscoveryPage] Using scenario ID:', targetScenarioId);
                        
                        // Get model from settings
                        const settings = useGenAISettings.getState();
                        const model = settings.model;
                        
                        // Process query: interpret + execute
                        // Progress callback is optional - stages are included in final ScenarioData
                        const scenarioData = await processScenarioQuery(
                          text.trim(),
                          targetScenarioId,
                          pid,
                          model
                        );
                        
                        // Save to store
                        useScenarioDataStore.getState().addScenarioData(scenarioData);
                        
                        // Save to backend (only for real scenarios, not 'scenarios-main')
                        if (targetScenarioId !== 'scenarios-main') {
                          try {
                            await api.createScenarioData(pid, targetScenarioId, scenarioData);
                            console.log('[DiscoveryPage] Scenario data saved to backend');
                          } catch (error) {
                            console.error('[DiscoveryPage] Failed to save scenario data to backend:', error);
                            // Don't fail the whole operation if backend save fails
                          }
                        }
                        
                        // Clear streaming state
                        useGenaiChatStore.setState({ streaming: false });
                      } catch (error) {
                        console.error('[DiscoveryPage] Error processing scenario query:', error);
                        useGenaiChatStore.setState({
                          streaming: false,
                          error: error instanceof Error ? error.message : 'Unknown error',
                        });
                      }
                    }}
                  />
                </div>
              </div>
            )}
            {/* GenAI chat history */}
                    <div
                      ref={messagesRef}
                      className="flex-1 overflow-y-auto px-1 min-h-[160px]"
                      style={{
                        contentVisibility: 'auto',
                        containIntrinsicSize: '0px 600px',
                        display:
                          unfoldSubView === 'perspectives' || unfoldSubView === 'pages' || unfoldSubView === 'models' || unfoldSubView === 'scenarios' ? 'none' : undefined,
                      }}
                    >
              {(() => {
                // Check if chat is empty (not hydrating and no messages/interactions)
                // Don't show empty state if streaming (user just sent a message)
                const isEmpty =
                          !genaiChat.hydrating &&
                          !genaiChat.streaming &&
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
                    <ChatMessagesList
                      mergedMessages={mergedMessages}
                      genaiChat={{
                        streaming: genaiChat.streaming,
                        error: genaiChat.error,
                        messageMeta: genaiChat.messageMeta,
                        preStanceLatency: genaiChat.preStanceLatency,
                        evaluations: genaiChat.evaluations,
                        interactions: genaiChat.interactions,
                        messages: genaiChat.messages,
                        persistedByLocalId: (genaiChat as any).persistedByLocalId || {},
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
                      collapsedSections={collapsedSections}
                      onToggleCollapse={(sid) =>
                        setCollapsedSections((s) => ({
                          ...s,
                          [sid]: !s[sid],
                        }))
                      }
                      frozenEdits={frozenEdits}
                      editErrors={editErrors}
                      editLast={editLast}
                      editHighlight={editHighlight}
                      onEditStart={handleEditStart}
                      onEditFinish={handleEditFinish}
                      onEditError={handleEditError}
                      onEditUpdate={handleEditUpdate}
                      onOpenSettings={handleOpenSettings}
                    />
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
            {/* Text Tasks Panel - hide when Pending Actions drawer is open */}
            {queuedTasks && 
             queuedTasks.filter((t) => t.type === 'text' && !t.applied).length > 0 &&
             rightPanelType !== 'pending-actions' && (
              <TextTasksPanel
                tasks={queuedTasks}
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
            {unfoldSubView !== 'perspectives' && unfoldSubView !== 'pages' && unfoldSubView !== 'scenarios' && (
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
              scenariosMode={false}
              onSubmit={async (text) => {
                // CRITICAL: Check fresh state from store, not stale component state
                // genaiChat.streaming may be stale due to React batching/closure
                const currentStreaming = useGenaiChatStore.getState().streaming;
                if (currentStreaming || !text.trim()) {
                  return;
                }
                
                // Handle Analytics mode separately
                if (unfoldMode === 'analytics') {
                  console.log('[DiscoveryPage] Analytics mode: processing BigQuery query:', text);
                  const pid = await genaiChat.ensureProject();
                  
                  try {
                    const now = Date.now().toString();
                    const createdAt = new Date().toISOString();
                    
                    // Add user interaction locally
                    const userInteractionLocalId = now + '-iu';
                    const userInteraction = {
                      id: userInteractionLocalId,
                      projectId: pid,
                      kind: 'user' as const,
                      subkind: 'analytics.query', // Для фильтрации по режиму
                      content: text.trim(),
                      createdAt,
                    };
                    // Add interaction using setState
                    useGenaiChatStore.setState((st) => ({
                      interactions: [...st.interactions, userInteraction as any]
                    }));

                    // Persist user interaction to server and get server ID
                    let userInteractionServerId = userInteractionLocalId;
                    try {
                      const serverUserInteraction = await api.createProjectInteraction(pid, {
                        kind: 'user',
                        subkind: 'analytics.query',
                        content: text.trim(),
                      });
                      userInteractionServerId = serverUserInteraction.id;
                      // Update local interaction with server ID
                      const st = useGenaiChatStore.getState();
                      const items = [...st.interactions];
                      const idx = items.findIndex((it) => it.id === userInteractionLocalId);
                      if (idx >= 0) {
                        items[idx] = { ...items[idx], id: userInteractionServerId } as any;
                        useGenaiChatStore.setState({ interactions: items } as any);
                      }
                    } catch (e) {
                      console.warn('[DiscoveryPage] Failed to persist user interaction:', e);
                    }

                    // Show loading state
                    useGenaiChatStore.setState({ streaming: true, error: null });

                    // Step 1: Generate SQL
                    let sqlResponse: { sql: string; explanation?: string };
                    try {
                      console.log('[DiscoveryPage] Step 1: Generating SQL from natural language query...');
                      sqlResponse = await api.generateSQL(pid, text.trim());
                      console.log('[DiscoveryPage] SQL generated successfully, length:', sqlResponse.sql.length);
                      console.log('[DiscoveryPage] SQL explanation:', sqlResponse.explanation);
                    } catch (error: any) {
                      console.error('[DiscoveryPage] SQL generation failed:', error);
                      const errorMsg = error.response?.data?.message || error.message || 'Не удалось сгенерировать SQL';
                      const assistantContent = `Ошибка генерации SQL:\n\n**Ошибка:** ${errorMsg}\n\n*Проверьте логи сервера для деталей*`;
                      
                      const assistantInteraction: any = {
                        id: `temp-assistant-${Date.now()}`,
                        projectId: pid,
                        kind: 'assistant',
                        subkind: 'analytics.error',
                        content: assistantContent,
                        replyTo: userInteractionServerId, // Use server ID for replyTo
                        createdAt: new Date(),
                      };
                      
                      useGenaiChatStore.setState((st) => ({
                        interactions: [...st.interactions, assistantInteraction as any],
                        streaming: false,
                        error: errorMsg,
                      }));
                      
                      // Persist assistant interaction
                      try {
                        const persistedAssistant = await api.createProjectInteraction(pid, assistantInteraction);
                        useGenaiChatStore.setState((st) => ({
                          interactions: st.interactions.map((i: any) =>
                            i.id === assistantInteraction.id ? persistedAssistant : i
                          ),
                        }));
                      } catch (persistError) {
                        console.error('[DiscoveryPage] Failed to persist assistant interaction:', persistError);
                      }
                      
                      return;
                    }

                    // Step 2: Execute SQL
                    let executeResponse: { results: Array<Record<string, any>>; rowCount: number; error?: string };
                    try {
                      console.log('[DiscoveryPage] Step 2: Executing SQL query...');
                      executeResponse = await api.executeSQL(pid, sqlResponse.sql, 100);
                      console.log('[DiscoveryPage] SQL executed, rowCount:', executeResponse.rowCount);
                      console.log('[DiscoveryPage] Results count:', executeResponse.results?.length || 0);
                      console.log('[DiscoveryPage] Has error?', !!executeResponse.error);
                    } catch (error: any) {
                      console.error('[DiscoveryPage] SQL execution failed:', error);
                      executeResponse = {
                        results: [],
                        rowCount: 0,
                        error: error.response?.data?.error || error.response?.data?.message || error.message || 'Неизвестная ошибка',
                      };
                    }

                    let assistantContent = '';
                    if (executeResponse.error) {
                      const errorMsg = executeResponse.error;
                      const sqlQuery = sqlResponse.sql;
                      assistantContent = `Ошибка выполнения SQL запроса:\n\n**Ошибка:** ${errorMsg}\n\n**SQL запрос:**\n\`\`\`sql\n${sqlQuery}\n\`\`\`\n\n*Проверьте логи сервера для деталей*`;
                      useGenaiChatStore.setState({ error: errorMsg });
                    } else {
                      // Format results
                      if (sqlResponse.explanation) {
                        assistantContent += `**Объяснение:** ${sqlResponse.explanation}\n\n`;
                      }
                      
                      // Show SQL for debugging
                      assistantContent += `**SQL запрос:**\n\`\`\`sql\n${sqlResponse.sql}\n\`\`\`\n\n`;
                      assistantContent += `**Результаты:** (найдено строк: ${executeResponse.rowCount})\n\n`;

                      if (executeResponse.results && executeResponse.results.length > 0) {
                        // Format as table
                        const headers = Object.keys(executeResponse.results[0]);
                        assistantContent += '| ' + headers.join(' | ') + ' |\n';
                        assistantContent += '|' + headers.map(() => '---').join('|') + '|\n';
                        executeResponse.results.slice(0, 50).forEach((row: any) => {
                          assistantContent += '| ' + headers.map(h => {
                            const val = row[h];
                            if (val === null || val === undefined) return '';
                            if (typeof val === 'object') return JSON.stringify(val);
                            return String(val);
                          }).join(' | ') + ' |\n';
                        });
                        if (executeResponse.results.length > 50) {
                          assistantContent += `\n*Показано 50 из ${executeResponse.results.length} строк*\n`;
                        }
                      } else {
                        assistantContent += 'Нет данных для отображения.\n';
                      }
                    }

                    // Add assistant interaction locally
                    const assistantInteractionLocalId = now + '-ia';
                    const assistantInteraction = {
                      id: assistantInteractionLocalId,
                      projectId: pid,
                      kind: 'assistant' as const,
                      subkind: 'analytics.response', // Для фильтрации по режиму
                      content: assistantContent,
                      createdAt: new Date().toISOString(),
                      replyTo: userInteractionServerId, // Use server ID for replyTo
                    };
                    // Add interaction using setState
                    useGenaiChatStore.setState((st) => ({
                      interactions: [...st.interactions, assistantInteraction as any]
                    }));

                    // Persist assistant interaction to server
                    try {
                      const serverAssistantInteraction = await api.createProjectInteraction(pid, {
                        kind: 'assistant',
                        subkind: 'analytics.response',
                        content: assistantContent,
                        replyTo: userInteractionServerId, // Use server ID for replyTo
                      });
                      // Update local interaction with server ID
                      const st = useGenaiChatStore.getState();
                      const items = [...st.interactions];
                      const idx = items.findIndex((it) => it.id === assistantInteractionLocalId);
                      if (idx >= 0) {
                        items[idx] = { ...items[idx], id: serverAssistantInteraction.id } as any;
                        useGenaiChatStore.setState({ interactions: items } as any);
                      }
                    } catch (e) {
                      console.warn('[DiscoveryPage] Failed to persist assistant interaction:', e);
                    }

                  } catch (error: any) {
                    console.error('[DiscoveryPage] BigQuery query error:', error);
                    const errorContent = `Ошибка при выполнении запроса: ${error.message || String(error)}`;
                    const errorInteraction = {
                      id: Date.now().toString() + '-ia-error',
                      projectId: pid,
                      kind: 'assistant' as const,
                      subkind: 'analytics.error',
                      content: errorContent,
                      createdAt: new Date().toISOString(),
                    };
                    // Add error interaction using setState
                    useGenaiChatStore.setState((st) => ({
                      interactions: [...st.interactions, errorInteraction as any],
                      error: error.message || String(error)
                    }));
                    
                    // Try to persist error interaction
                    try {
                      await api.createProjectInteraction(pid, {
                        kind: 'assistant',
                        subkind: 'analytics.error',
                        content: errorContent,
                      });
                    } catch (e) {
                      console.warn('[DiscoveryPage] Failed to persist error interaction:', e);
                    }
                  } finally {
                    useGenaiChatStore.setState({ streaming: false });
                  }
                      return;
                }

                // Handle Scenarios mode separately
                if (unfoldMode === 'scenarios') {
                  console.log('[DiscoveryPage] Scenarios mode: processing scenario modification request:', text);
                  const pid = await genaiChat.ensureProject();
                  
                  if (!pid || pid === 'local-unfold') {
                    console.warn('[DiscoveryPage] Cannot process scenarios for local project');
                    return;
                  }

                  try {
                    // Show loading state
                    useGenaiChatStore.setState({ streaming: true, error: null });

                    // Step 1: Load all scenarios for the project
                    console.log('[DiscoveryPage] Step 1: Loading scenarios...');
                    const allScenarios = await api.scenariosList(pid, null);
                    const scenariosArray = Array.isArray(allScenarios) ? allScenarios : [];
                    
                    // Build hierarchical tree structure
                    const buildScenarioTree = (scenariosList: ProjectScenario[]): Array<ProjectScenario & { children?: ProjectScenario[] }> => {
                      const scenarioMap = new Map<string, ProjectScenario & { children?: ProjectScenario[] }>();
                      const roots: Array<ProjectScenario & { children?: ProjectScenario[] }> = [];

                      // First pass: create map of all scenarios
                      scenariosList.forEach(scenario => {
                        scenarioMap.set(scenario.id, { ...scenario, children: [] });
                      });

                      // Second pass: build tree
                      scenariosList.forEach(scenario => {
                        const node = scenarioMap.get(scenario.id)!;
                        if (!scenario.parentId) {
                          roots.push(node);
                        } else {
                          const parent = scenarioMap.get(scenario.parentId);
                          if (parent) {
                            if (!parent.children) parent.children = [];
                            parent.children.push(node);
                          }
                        }
                      });

                      // Sort by order, then by createdAt
                      const sortScenarios = (nodes: Array<ProjectScenario & { children?: ProjectScenario[] }>) => {
                        nodes.sort((a, b) => {
                          if (a.order !== b.order) return a.order - b.order;
                          return new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime();
                        });
                        nodes.forEach(node => {
                          if (node.children) sortScenarios(node.children);
                        });
                      };

                      sortScenarios(roots);
                      return roots;
                    };

                    const scenarioTree = buildScenarioTree(scenariosArray);
                    const scenariosJSON = JSON.stringify(scenarioTree, null, 2);

                    // Step 2: Prepare JSON schema for delta changes
                    const preferredLang = getPreferredLanguage();
                    const langName = getLanguageName(preferredLang);
                    const { useGenAISettings } = await import('../stores/genaiSettingsStore');
                    const settings = useGenAISettings.getState();
                    const evalModel = (settings.stanceModel && settings.stanceModel.trim()) ? settings.stanceModel : 'gemini_fast';

                    const schema = {
                      type: 'object',
                      additionalProperties: false,
                      properties: {
                        changes: {
                          type: 'array',
                          items: {
                            oneOf: [
                              {
                                // CREATE
                                type: 'object',
                                additionalProperties: false,
                                properties: {
                                  action: { type: 'string', enum: ['create'] },
                                  id: { type: 'string', pattern: '^temp_\\d+$', description: 'Temporary ID for new scenario (format: temp_1, temp_2, etc.). Required for hierarchical scenarios.' },
                                  title: { type: 'string', minLength: 1, maxLength: 200 },
                                  parentId: { type: ['string', 'null'], description: 'Parent scenario ID. Use temporary ID (temp_X) if parent is also being created, or existing scenario ID, or null for root.' },
                                  description: { type: ['string', 'null'] },
                                  order: { type: 'number', minimum: 0 }
                                },
                                required: ['action', 'id', 'title']
                              },
                              {
                                // EDIT
                                type: 'object',
                                additionalProperties: false,
                                properties: {
                                  action: { type: 'string', enum: ['edit'] },
                                  id: { type: 'string' },
                                  title: { type: ['string', 'null'] },
                                  description: { type: ['string', 'null'] },
                                  parentId: { type: ['string', 'null'] },
                                  order: { type: ['number', 'null'] }
                                },
                                required: ['action', 'id']
                              },
                              {
                                // DELETE
                                type: 'object',
                                additionalProperties: false,
                                properties: {
                                  action: { type: 'string', enum: ['delete'] },
                                  id: { type: 'string' }
                                },
                                required: ['action', 'id']
                              }
                            ]
                          }
                        }
                      },
                      required: ['changes']
                    } as any;

                    const schemaText = JSON.stringify(schema, null, 2);

                    // Step 3: Build prompt
                    const prompt = `User requirement: you should create your answer in ${langName} language. Use English or other languages inline only if you use well-known and accepted in target language terms.

You are an expert scenario planner. Your task is to analyze the user's request and modify the existing scenario structure accordingly.

CURRENT SCENARIOS (hierarchical structure):
${scenariosJSON}

USER REQUEST:
${text.trim()}

TASK:
Analyze the user's request and determine what changes need to be made to the scenario structure. Return ONLY a delta of changes - what needs to be created, edited, or deleted.

RULES:
1. For CREATE: 
   - MUST provide a temporary ID in format "temp_1", "temp_2", "temp_3", etc. (required)
   - Provide title (required), parentId (null for root, or temporary ID if parent is also being created, or existing scenario ID), description (optional), order (optional)
   - When creating hierarchical scenarios, use temporary IDs for parent references. For example: if creating "Parent" (temp_1) and "Child" (temp_2), set Child's parentId to "temp_1"
   - Temporary IDs are only used during creation - the system will replace them with real IDs automatically
2. For EDIT: Provide id (required, use existing scenario ID) and fields to update (title, description, parentId, order - only include fields that should change)
3. For DELETE: Provide id (required, use existing scenario ID) - this will delete the scenario and all its children
4. Apply changes in order: DELETE first, then EDIT, then CREATE
5. For CREATE parentId validation:
   - If parentId is null → creates root scenario
   - If parentId is "temp_X" → parent is also being created in this batch (use temporary ID)
   - If parentId is an existing scenario ID → parent already exists
6. IMPORTANT: When creating multiple levels of hierarchy, use temporary IDs consistently. Example:
   - Create "Level 1" with id="temp_1", parentId=null
   - Create "Level 2" with id="temp_2", parentId="temp_1"
   - Create "Level 3" with id="temp_3", parentId="temp_2"

The JSON MUST conform EXACTLY to this schema (no additional keys, types must match):
${schemaText}

Return ONLY JSON, no markdown, no code fences, no explanations.`;

                    const langDirective = `You MUST produce all output strictly in ${langName} language. Use English only for technical terms or code if they are standard industry terms.`;

                    // Step 4: Call LLM with strict JSON
                    console.log('[DiscoveryPage] Step 2: Calling LLM for scenario changes...');
                    const res = await api.genaiGenerate({
                      model: evalModel,
                      prompt,
                      strictJSONEnabled: true,
                      jsonSchema: schema,
                      maxOutputTokens: 2048,
                      webSearchEnabled: false,
                      systemInstruction: langDirective,
                    });

                    // Step 5: Parse response
                    let parsed: any = null;
                    try {
                      let contentToParse = res?.content || '';
                      
                      // Try rawJSON first if available
                      const rawJSON = (res as any)?.rawJSON;
                      if (typeof rawJSON === 'string' && rawJSON.trim()) {
                        contentToParse = rawJSON;
                      }
                      
                      // Remove markdown code fences if present
                      contentToParse = contentToParse
                        .replace(/^```json\s*/i, '')
                        .replace(/^```\s*/i, '')
                        .replace(/```\s*$/i, '')
                        .trim();
                      
                      // Fix common JSON escaping issues
                      contentToParse = contentToParse.replace(/\\'/g, "'");
                      
                      // Try to extract JSON from content
                      if (contentToParse.startsWith('{') || contentToParse.startsWith('[')) {
                        parsed = JSON.parse(contentToParse);
                      } else {
                        // Try to find JSON object in content
                        const jsonMatch = contentToParse.match(/\{[\s\S]*\}/);
                        if (jsonMatch && jsonMatch[0]) {
                          parsed = JSON.parse(jsonMatch[0]);
                        }
                      }
                    } catch (parseError) {
                      console.error('[DiscoveryPage] Failed to parse LLM response:', parseError);
                      throw new Error('Failed to parse LLM response as JSON');
                    }

                    if (!parsed || !parsed.changes || !Array.isArray(parsed.changes)) {
                      throw new Error('Invalid response format: missing changes array');
                    }

                    // Step 6: Apply changes (DELETE first, then EDIT, then CREATE)
                    console.log('[DiscoveryPage] Step 3: Applying changes...', parsed.changes);
                    const changes = parsed.changes;
                    const appliedChanges: string[] = [];
                    const errors: string[] = [];

                    // Sort changes: DELETE first, then EDIT, then CREATE
                    const deleteChanges = changes.filter((c: any) => c.action === 'delete');
                    const editChanges = changes.filter((c: any) => c.action === 'edit');
                    const createChanges = changes.filter((c: any) => c.action === 'create');

                    // Map for temporary ID -> real ID mapping
                    const tempIdToRealId = new Map<string, string>();

                    // Apply DELETE changes
                    for (const change of deleteChanges) {
                      try {
                        await api.scenarioDelete(pid, change.id);
                        appliedChanges.push(`Deleted scenario: ${change.id}`);
                        console.log('[DiscoveryPage] Deleted scenario:', change.id);
                      } catch (error: any) {
                        const errorMsg = `Failed to delete scenario ${change.id}: ${error.message}`;
                        errors.push(errorMsg);
                        console.error('[DiscoveryPage]', errorMsg, error);
                      }
                    }

                    // Apply EDIT changes
                    for (const change of editChanges) {
                      try {
                        // Validate scenario exists
                        const existingScenario = scenariosArray.find(s => s.id === change.id);
                        if (!existingScenario) {
                          errors.push(`Scenario ${change.id} not found for edit`);
                          continue;
                        }

                        // Build update object (only include fields that are provided)
                        const updateData: any = {};
                        if (change.title !== undefined && change.title !== null) updateData.title = change.title;
                        if (change.description !== undefined) updateData.description = change.description;
                        
                        // Handle parentId: replace temp ID with real ID if needed
                        if (change.parentId !== undefined) {
                          if (change.parentId === null) {
                            updateData.parentId = null;
                          } else if (change.parentId.startsWith('temp_')) {
                            // Parent is being created in this batch - use real ID from map
                            const realParentId = tempIdToRealId.get(change.parentId);
                            if (realParentId) {
                              updateData.parentId = realParentId;
                            } else {
                              errors.push(`Temporary parent ID ${change.parentId} not found in created scenarios`);
                              continue;
                            }
                          } else {
                            // Existing parent ID
                            const parentExists = scenariosArray.find(s => s.id === change.parentId);
                            if (!parentExists) {
                              errors.push(`Parent scenario ${change.parentId} not found for edit`);
                              continue;
                            }
                            updateData.parentId = change.parentId;
                          }
                        }
                        
                        if (change.order !== undefined && change.order !== null) updateData.order = change.order;

                        if (Object.keys(updateData).length > 0) {
                          await api.scenarioUpdate(pid, change.id, updateData);
                          appliedChanges.push(`Updated scenario: ${change.id}`);
                          console.log('[DiscoveryPage] Updated scenario:', change.id, updateData);
                        }
                      } catch (error: any) {
                        const errorMsg = `Failed to edit scenario ${change.id}: ${error.message}`;
                        errors.push(errorMsg);
                        console.error('[DiscoveryPage]', errorMsg, error);
                      }
                    }

                    // Apply CREATE changes - need to handle hierarchical creation
                    // First, sort CREATE changes by dependency level (root scenarios first, then children)
                    const createChangesSorted: any[] = [];
                    const createChangesMap = new Map<string, any>();
                    const processedTempIds = new Set<string>();

                    // Build map of temp IDs
                    createChanges.forEach((c: any) => {
                      if (c.id && c.id.startsWith('temp_')) {
                        createChangesMap.set(c.id, c);
                      }
                    });

                    // Helper function to check if parent is ready (exists or already created)
                    const isParentReady = (parentId: string | null | undefined): boolean => {
                      if (!parentId || parentId === null) return true; // Root scenario
                      if (parentId.startsWith('temp_')) {
                        return processedTempIds.has(parentId); // Parent temp ID already processed
                      }
                      return scenariosArray.some(s => s.id === parentId); // Existing parent
                    };

                    // Process CREATE changes in levels (breadth-first)
                    let remainingChanges = [...createChanges];
                    let maxIterations = 100; // Safety limit
                    let iteration = 0;

                    while (remainingChanges.length > 0 && iteration < maxIterations) {
                      iteration++;
                      const readyChanges = remainingChanges.filter((c: any) => {
                        const parentId = c.parentId;
                        return isParentReady(parentId);
                      });

                      if (readyChanges.length === 0) {
                        // Circular dependency or missing parent
                        const stuck = remainingChanges.map((c: any) => `temp_${c.id || 'unknown'}`).join(', ');
                        errors.push(`Cannot resolve parent dependencies for scenarios: ${stuck}`);
                        break;
                      }

                      // Process ready changes
                      for (const change of readyChanges) {
                        try {
                          // Determine real parentId
                          let realParentId: string | null = null;
                          if (change.parentId !== null && change.parentId !== undefined) {
                            if (change.parentId.startsWith('temp_')) {
                              // Parent was created in this batch - get real ID
                              realParentId = tempIdToRealId.get(change.parentId) || null;
                              if (!realParentId) {
                                errors.push(`Temporary parent ID ${change.parentId} not found for scenario ${change.id}`);
                                continue;
                              }
                            } else {
                              // Existing parent ID - validate it exists
                              const parentExists = scenariosArray.find(s => s.id === change.parentId);
                              if (!parentExists) {
                                errors.push(`Parent scenario ${change.parentId} not found for create`);
                                continue;
                              }
                              realParentId = change.parentId;
                            }
                          }

                          const createData: any = {
                            title: change.title,
                            parentId: realParentId,
                            status: 'active',
                          };
                          if (change.description !== undefined && change.description !== null) {
                            createData.description = change.description;
                          }
                          if (change.order !== undefined && change.order !== null) {
                            createData.order = change.order;
                          }

                          const newScenario = await api.scenarioCreate(pid, createData);
                          
                          // Map temporary ID to real ID
                          if (change.id && change.id.startsWith('temp_')) {
                            tempIdToRealId.set(change.id, newScenario.id);
                            processedTempIds.add(change.id);
                          }
                          
                          appliedChanges.push(`Created scenario: ${newScenario.title} (${newScenario.id})`);
                          console.log('[DiscoveryPage] Created scenario:', newScenario.id, newScenario.title, 'temp_id:', change.id);
                        } catch (error: any) {
                          const errorMsg = `Failed to create scenario ${change.id || change.title}: ${error.message}`;
                          errors.push(errorMsg);
                          console.error('[DiscoveryPage]', errorMsg, error);
                        }
                      }

                      // Remove processed changes
                      remainingChanges = remainingChanges.filter(c => !readyChanges.includes(c));
                    }

                    if (remainingChanges.length > 0) {
                      errors.push(`Failed to create ${remainingChanges.length} scenario(s) due to unresolved dependencies`);
                    }

                    // Step 7: Build response message
                    let assistantContent = '**Изменения сценариев применены:**\n\n';
                    if (appliedChanges.length > 0) {
                      assistantContent += appliedChanges.map(c => `✓ ${c}`).join('\n');
                    }
                    if (errors.length > 0) {
                      assistantContent += '\n\n**Ошибки:**\n';
                      assistantContent += errors.map(e => `✗ ${e}`).join('\n');
                    }
                    if (appliedChanges.length === 0 && errors.length === 0) {
                      assistantContent += 'Нет изменений для применения.';
                    }

                    // Add user interaction
                    const now = Date.now().toString();
                    const createdAt = new Date().toISOString();
                    const userInteractionLocalId = now + '-iu';
                    const userInteraction = {
                      id: userInteractionLocalId,
                      projectId: pid,
                      kind: 'user' as const,
                      subkind: 'scenarios.modify',
                      content: text.trim(),
                      createdAt,
                    };
                    useGenaiChatStore.setState((st) => ({
                      interactions: [...st.interactions, userInteraction as any]
                    }));

                    // Persist user interaction
                    let userInteractionServerId = userInteractionLocalId;
                    try {
                      const serverUserInteraction = await api.createProjectInteraction(pid, {
                        kind: 'user',
                        subkind: 'scenarios.modify',
                        content: text.trim(),
                      });
                      userInteractionServerId = serverUserInteraction.id;
                      const st = useGenaiChatStore.getState();
                      const items = [...st.interactions];
                      const idx = items.findIndex((it) => it.id === userInteractionLocalId);
                      if (idx >= 0) {
                        items[idx] = { ...items[idx], id: userInteractionServerId } as any;
                        useGenaiChatStore.setState({ interactions: items } as any);
                      }
                    } catch (e) {
                      console.warn('[DiscoveryPage] Failed to persist user interaction:', e);
                    }

                    // Add assistant interaction
                    const assistantInteraction: any = {
                      id: `temp-assistant-${Date.now()}`,
                      projectId: pid,
                      kind: 'assistant',
                      subkind: 'scenarios.modified',
                      content: assistantContent,
                      replyTo: userInteractionServerId,
                      createdAt: new Date(),
                    };
                    
                    useGenaiChatStore.setState((st) => ({
                      interactions: [...st.interactions, assistantInteraction as any],
                      streaming: false,
                    }));

                    // Persist assistant interaction
                    try {
                      const persistedAssistant = await api.createProjectInteraction(pid, {
                        kind: 'assistant',
                        subkind: 'scenarios.modified',
                        content: assistantContent,
                        replyTo: userInteractionServerId,
                      });
                      const st = useGenaiChatStore.getState();
                      const items = [...st.interactions];
                      const idx = items.findIndex((it) => it.id === assistantInteraction.id);
                      if (idx >= 0) {
                        items[idx] = { ...items[idx], id: persistedAssistant.id } as any;
                        useGenaiChatStore.setState({ interactions: items } as any);
                      }
                    } catch (e) {
                      console.warn('[DiscoveryPage] Failed to persist assistant interaction:', e);
                    }

                    // Step 8: Update sidebar
                    console.log('[DiscoveryPage] Dispatching scenarios.updated event', { projectId: pid });
                    window.dispatchEvent(new CustomEvent('scenarios.updated', { detail: { projectId: pid } }));

                  } catch (error: any) {
                    console.error('[DiscoveryPage] Failed to process scenario changes:', error);
                    const errorMsg = error.response?.data?.message || error.message || 'Не удалось обработать изменения сценариев';
                    
                    const assistantContent = `Ошибка обработки изменений сценариев:\n\n**Ошибка:** ${errorMsg}\n\n*Проверьте логи для деталей*`;
                    
                    const assistantInteraction: any = {
                      id: `temp-assistant-error-${Date.now()}`,
                      projectId: pid,
                      kind: 'assistant',
                      subkind: 'scenarios.error',
                      content: assistantContent,
                      createdAt: new Date(),
                    };
                    
                    useGenaiChatStore.setState((st) => ({
                      interactions: [...st.interactions, assistantInteraction as any],
                      streaming: false,
                      error: errorMsg,
                    }));
                  }
                  return;
                }

                // Handle Agent mode separately
                if (unfoldMode === 'agent') {
                  console.log('[DiscoveryPage] Agent mode: processing agent execution request:', text);
                  const pid = await genaiChat.ensureProject();
                  
                  try {
                    const now = Date.now().toString();
                    const createdAt = new Date().toISOString();
                    
                    // Add user interaction locally
                    const userInteractionLocalId = now + '-iu';
                    const userInteraction = {
                      id: userInteractionLocalId,
                      projectId: pid,
                      kind: 'user' as const,
                      subkind: 'agent.query',
                      content: text.trim(),
                      createdAt,
                    };
                    // Add interaction using setState
                    useGenaiChatStore.setState((st) => ({
                      interactions: [...st.interactions, userInteraction as any]
                    }));

                    // Persist user interaction to server and get server ID
                    let userInteractionServerId = userInteractionLocalId;
                    try {
                      const serverUserInteraction = await api.createProjectInteraction(pid, {
                        kind: 'user',
                        subkind: 'agent.query',
                        content: text.trim(),
                      });
                      userInteractionServerId = serverUserInteraction.id;
                      // Update local interaction with server ID
                      const st = useGenaiChatStore.getState();
                      const items = [...st.interactions];
                      const idx = items.findIndex((it) => it.id === userInteractionLocalId);
                      if (idx >= 0) {
                        items[idx] = { ...items[idx], id: userInteractionServerId } as any;
                        useGenaiChatStore.setState({ interactions: items } as any);
                      }
                    } catch (e) {
                      console.warn('[DiscoveryPage] Failed to persist user interaction:', e);
                    }

                    // Show loading state
                    useGenaiChatStore.setState({ streaming: true, error: null });

                    // Create and execute agent job
                    try {
                      console.log('[DiscoveryPage] Creating agent job for document preparation...');
                      
                      // Generate title for the job (async, non-blocking)
                      const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
                      const { generateAgentJobTitle } = await import('../lib/agentJobTitleGeneration');
                      
                      // Create job first (title will be updated later)
                      const jobResponse = await api.createAgentJob(pid, {
                        agentId: 'document_preparation_agent',
                        task: {
                          userQuery: text.trim(),
                          planMode: 'auto',
                        },
                        autoMode: true,
                      });
                      
                      console.log('[DiscoveryPage] Agent job created:', jobResponse.job.id);
                      
                      // Generate and update title asynchronously
                      generateAgentJobTitle({
                        jobId: jobResponse.job.id,
                        userQuery: text.trim(),
                        preferredLang,
                      }).then((title) => {
                        if (title) {
                          api.updateAgentJob(pid, jobResponse.job.id, { title }).catch((e) => {
                            console.warn('[DiscoveryPage] Failed to update job title:', e);
                          });
                        }
                      }).catch((e) => {
                        console.warn('[DiscoveryPage] Failed to generate job title:', e);
                      });
                      
                      // Start execution (async, updates via WebSocket)
                      await api.executeAgentJob(pid, jobResponse.job.id);
                      console.log('[DiscoveryPage] Agent job execution started');
                      
                      // Show initial message
                      const assistantContent = `**Выполнение агента запущено**\n\nАгент анализирует задачу и планирует необходимые действия. Прогресс будет отображаться в реальном времени через WebSocket обновления.`;
                      
                      const assistantInteraction: any = {
                        id: `temp-assistant-${Date.now()}`,
                        projectId: pid,
                        kind: 'assistant',
                        subkind: 'agent.started',
                        content: assistantContent,
                        replyTo: userInteractionServerId,
                        createdAt: new Date(),
                      };
                      
                      useGenaiChatStore.setState((st) => ({
                        interactions: [...st.interactions, assistantInteraction as any],
                        streaming: false,
                      }));
                      
                      // Persist assistant interaction
                      try {
                        const persistedAssistant = await api.createProjectInteraction(pid, {
                          kind: 'assistant',
                          subkind: 'agent.started',
                          content: assistantContent,
                          replyTo: userInteractionServerId,
                        });
                        const st = useGenaiChatStore.getState();
                        const items = [...st.interactions];
                        const idx = items.findIndex((it) => it.id === assistantInteraction.id);
                        if (idx >= 0) {
                          items[idx] = { ...items[idx], id: persistedAssistant.id } as any;
                          useGenaiChatStore.setState({ interactions: items } as any);
                        }
                      } catch (e) {
                        console.warn('[DiscoveryPage] Failed to persist assistant interaction:', e);
                      }
                      
                    } catch (error: any) {
                      console.error('[DiscoveryPage] Failed to create/execute agent job:', error);
                      const errorMsg = error.response?.data?.message || error.message || 'Не удалось запустить агента';
                      const assistantContent = `Ошибка запуска агента:\n\n**Ошибка:** ${errorMsg}\n\n*Проверьте логи сервера для деталей*`;
                      
                      const assistantInteraction: any = {
                        id: `temp-assistant-error-${Date.now()}`,
                        projectId: pid,
                        kind: 'assistant',
                        subkind: 'agent.error',
                        content: assistantContent,
                        replyTo: userInteractionServerId,
                        createdAt: new Date(),
                      };
                      
                      useGenaiChatStore.setState((st) => ({
                        interactions: [...st.interactions, assistantInteraction as any],
                        streaming: false,
                        error: errorMsg,
                      }));
                      
                      // Persist assistant interaction
                      try {
                        await api.createProjectInteraction(pid, {
                          kind: 'assistant',
                          subkind: 'agent.error',
                          content: assistantContent,
                          replyTo: userInteractionServerId,
                        });
                      } catch (persistError) {
                        console.error('[DiscoveryPage] Failed to persist error interaction:', persistError);
                      }
                    }

                  } catch (error: any) {
                    console.error('[DiscoveryPage] Prepare mode error:', error);
                    const errorContent = `Ошибка при подготовке документов: ${error.message || String(error)}`;
                    const errorInteraction = {
                      id: Date.now().toString() + '-ia-error',
                      projectId: pid,
                      kind: 'assistant' as const,
                      subkind: 'agent.error',
                      content: errorContent,
                      createdAt: new Date().toISOString(),
                    };
                    // Add error interaction using setState
                    useGenaiChatStore.setState((st) => ({
                      interactions: [...st.interactions, errorInteraction as any],
                      streaming: false,
                      error: error.message || String(error)
                    }));
                    
                    // Try to persist error interaction
                    try {
                      await api.createProjectInteraction(pid, {
                        kind: 'assistant',
                        subkind: 'agent.error',
                        content: errorContent,
                      });
                    } catch (e) {
                      console.warn('[DiscoveryPage] Failed to persist error interaction:', e);
                    }
                  }
                  return;
                }

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
                      if ((window as any).wsSubscribeProject && pid) {
                        await (window as any).wsSubscribeProject(pid, { timeoutMs: 2500 });
                      }
                    } catch (err) {
                      console.warn('[SlashCommand] wsSubscribeProject failed', err);
                          }
                                const prompt =
                                  raw.replace(
                                    /^\s*\/analyze\s+entities\b\s*/i,
                                    '',
                                  ) +
                                  "\n\nConstraints:\n- Only include concrete, named entities (no abstract categories).\n- Allowed types: Person | Organization | Location | Product | Document | Facility | Other.\n- Reject generic words like 'technical regulation', 'open databases', 'contracts' without identifiers.";
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
                            
                            // Graph mode system prompt (for ontological entity management)
                            const graphSystemInstruction =
                              unfoldSubView === 'models'
                                ? getGraphSystemPrompt(langNameForPrompt)
                                : '';
                            
                            const graphModeSystemInstruction =
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
                // In Graph mode, use Graph system instruction (no other instructions)
                // In graph mode, prioritize graph instruction by putting it first
                // Otherwise, use standard instructions
                            const combinedSystemInstruction =
                              unfoldSubView === 'models'
                  ? graphSystemInstruction // Graph mode: ONLY ontology instructions, no other prompts
                  : unfoldMode === 'graph'
                  ? [
                      graphModeSystemInstruction,
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
                  researchEnabled: genaiSettings.researchEnabled,
                  temperature: genaiSettings.temperature,
                  // Graph mode needs more tokens for large ontology command sets
                  // Increase to 32768 (32k) for Graph mode, use default for other modes
                  maxOutputTokens: unfoldSubView === 'models' 
                    ? Math.max(genaiSettings.maxOutputTokens || 8192, 32768)
                    : genaiSettings.maxOutputTokens,
                  systemInstruction: combinedSystemInstruction,
                  // Graph mode: disable streaming, use non-streaming API
                  disableStreaming: unfoldSubView === 'models',
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
                              options: { 
                                ...options, 
                                __lang: preferredLang,
                                // Добавляем метаданные о вкладке, из которой было отправлено сообщение
                                __sourceView: unfoldSubView,
                              },
                              agentMeta: unfoldSubView === 'models' 
                                ? { 
                                    displayName: 'Model',
                                    subkind: 'graph.chat',
                                  }
                                : undefined,
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
                  const headerText = isUser ? 'you' : 'assistant';
                  
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
                                  {headerText}
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
              const rawProjectId = currentProject?.id;
              if (rawProjectId) {
                // Ensure projectId is always a string (handle cases where id might be an object)
                const projectId = typeof rawProjectId === 'string' ? rawProjectId : String(rawProjectId);
                const lang = getPreferredLanguage();
                setRunning(true);
                try {
                            await sendChatMessage(
                              projectId,
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

        {/* Add RSS modal */}
        <RSSAddModal
          isOpen={showAddRSSModal}
          onClose={() => setShowAddRSSModal(false)}
          input={rssInput}
          onInputChange={setRssInput}
          onSubmit={addRSSSource}
          isSubmitting={rssSubmitting}
          error={rssError}
        />
        </>
        )}
        </div>
        </div>

        {/* Right drawer: appears when any drawer is open */}
        <RightDrawer
          rightPanelType={rightPanelType}
          viewMode={viewMode}
          showAccount={showAccount}
          showSettings={showSettings}
          isMobile={isMobile}
          drawerWidth={drawerWidth}
          setDrawerWidth={setDrawerWidth}
          closeAllRightPanels={closeAllRightPanels}
          getPanelVisibilityStyles={getPanelVisibilityStyles}
          currentProjectId={currentProject?.id || null}
          addMenuRef={addMenuRef}
          showAddSourceMenu={showAddSourceMenu}
          onShowAddSourceMenuChange={setShowAddSourceMenu}
          onShowAddTelegramModalChange={setShowAddTelegramModal}
          onShowAddRSSModalChange={setShowAddRSSModal}
          onPreviewSource={setPreviewSource}
          onOpenDocument={setActiveDoc}
          onExpandWorkspace={openWorkspace}
          currentMode={unfoldSubView}
          handleAnalyzeEntities={handleAnalyzeEntities}
          handleAnalyzeFacts={handleAnalyzeFacts}
          handleAnalyzeEvents={handleAnalyzeEvents}
          handleAnalyzeMetrics={handleAnalyzeMetrics}
          handleAnalyzeDecisions={handleAnalyzeDecisions}
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
          queuedTasks={queuedTasks}
          onApplyTasks={handleApplyTasks}
          textTasksApplying={textTasksApplying}
          sectionStarts={sectionStarts}
          pinnedInteractions={pinnedInteractions}
          messagesRef={messagesRef}
          scrollToInteraction={scrollToInteraction}
          truncateCenterWords={truncateCenterWords}
          onRemoveNote={handleRemoveNote}
        />

        {/* Right buttons column - always visible */}
        <RightPanelButtons
          rightPanelType={rightPanelType}
          onPanelTypeChange={setRightPanelType}
          onScrollToTop={handleScrollToTop}
          onScrollToBottom={handleScrollToBottom}
          hasPinned={hasPinned}
          hasSections={hasSections}
          availUserAnalysis={availUserAnalysis}
          hasQueuedTasks={queuedTasks && queuedTasks.some((t) => !t.applied)}
          availDiscussionFlow={availDiscussionFlow}
          unfoldSubView={unfoldSubView}
        />
      </div>
    </div>
  );
};

export default DiscoveryPage;
