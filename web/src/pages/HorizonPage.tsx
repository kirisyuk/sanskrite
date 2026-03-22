import React from 'react';
import { useTranslation } from 'react-i18next';
import { useLocation, useNavigate } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';
import { ThemeContext } from '../components/ThemeProvider';
import { SidebarPanel } from '../components/SidebarPanel';
import { useProjectsStore } from '../stores/projectsStore';
import { useUIStore } from '../stores/uiStore';
import { SettingsModal } from '../components/SettingsModal';
import { AccountModal } from '../components/AccountModal';
import { LensCanvas } from '../components/LensCanvas';
import { WorldMapPanel } from '../components/WorldMapPanel';
import AssembleLensRequestComponent, { AssembleLensRequest } from '../features/horizon/components/hook/AssembleLensRequest';
import { api } from '../lib/api';
import { useHorizonRealtimeStore } from '../stores/horizonRealtimeStore';
import { useWebSocketConnection } from '../hooks/useWebSocketConnection';
import { useIsAdmin } from '../hooks/useIsAdmin';
import { HorizonLoading } from '../components/HorizonLoading';
import { HorizonEmptyState } from '../components/HorizonEmptyState';
import { HorizonWatchSurface } from '../components/HorizonWatchSurface';
import { HorizonWatchLensSurface } from '../components/HorizonWatchLensSurface';

type HorizonFatalPipelineError = {
  title: string;
  message: string;
  code?: string;
  phase?: string;
  ts: number;
};

const HorizonPage: React.FC = () => {
  const { t } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const { currentUser } = useAuth();
  const { theme } = React.useContext(ThemeContext);
  const projects = useProjectsStore((s) => s.projects);
  const projectsLoading = useProjectsStore((s) => s.loading);
  const fetchProjects = useProjectsStore((s) => s.fetchProjects);
  const setCurrentProject = useProjectsStore((s) => s.setCurrentProject);
  const currentProject = useProjectsStore((s) => s.currentProject);
  const deleteProject = useProjectsStore((s) => s.deleteProject);
  const updateProject = useProjectsStore((s) => s.updateProject);
  
  const [sidebarOpen, setSidebarOpen] = React.useState(true);
  const [showSettings, setShowSettings] = React.useState(false);
  const [showAccount, setShowAccount] = React.useState(false);
  const [showWorldMap, setShowWorldMap] = React.useState(false);
  const isWatchRoute = location.pathname === '/horizon/watch';
  
  // Load saved Horizon state from localStorage
  const getSavedHorizonState = React.useCallback(() => {
    try {
      const savedProjectId = localStorage.getItem('horizon.selectedProjectId');
      const savedLensId = localStorage.getItem('horizon.selectedLensId');
      return {
        projectId: savedProjectId || null,
        lensId: savedLensId || null,
      };
    } catch {
      return { projectId: null, lensId: null };
    }
  }, []);

  // Save Horizon state to localStorage
  const saveHorizonState = React.useCallback((projectId: string | null, lensId: string | null) => {
    try {
      if (projectId) {
        localStorage.setItem('horizon.selectedProjectId', projectId);
      } else {
        localStorage.removeItem('horizon.selectedProjectId');
      }
      if (lensId) {
        localStorage.setItem('horizon.selectedLensId', lensId);
      } else {
        localStorage.removeItem('horizon.selectedLensId');
      }
    } catch (err) {
      console.warn('[HorizonPage] Failed to save state to localStorage:', err);
    }
  }, []);

  const [selectedLensId, setSelectedLensIdState] = React.useState<string | null>(() => {
    return getSavedHorizonState().lensId;
  });
  
  // Wrapper for setSelectedLensId that also saves to localStorage
  const setSelectedLensId = React.useCallback((lensId: string | null) => {
    setSelectedLensIdState(lensId);
    const currentProjectId = currentProject?.id || null;
    saveHorizonState(currentProjectId, lensId);
  }, [currentProject?.id, saveHorizonState]);

  const assembly = useHorizonRealtimeStore((s) => s.assembly);
  const events = useHorizonRealtimeStore((s) => s.events);
  const resetRealtime = useHorizonRealtimeStore((s) => s.reset);
  const resetEvents = useHorizonRealtimeStore((s) => s.resetEvents);
  const [lensStarted, setLensStarted] = React.useState(false);
  const [isCreatingLens, setIsCreatingLens] = React.useState(false);
  const [activeCreateCorrelationId, setActiveCreateCorrelationId] = React.useState<string | null>(null);
  const [activeCreateSessionId, setActiveCreateSessionId] = React.useState<string | null>(null);
  const [createFatalError, setCreateFatalError] = React.useState<HorizonFatalPipelineError | null>(null);
  const creatingLensRef = React.useRef(false);
  const feedRef = React.useRef<HTMLDivElement | null>(null);

  // Preflight (“How we understood your request”) before starting scouting.
  const [preflightOpen, setPreflightOpen] = React.useState(false);
  const [preflightResult, setPreflightResult] = React.useState<any | null>(null);
  const [preflightMode, setPreflightMode] = React.useState<'just_open' | 'research_first'>('research_first');
  const [preflightHotPathOnly, setPreflightHotPathOnly] = React.useState(false);
  const [preflightSelectedDomains, setPreflightSelectedDomains] = React.useState<Record<string, boolean>>({});
  const [preflightSelectedAnchors, setPreflightSelectedAnchors] = React.useState<Record<string, boolean>>({});
  const [preflightFocus, setPreflightFocus] = React.useState('');
  const [preflightDomainsDraft, setPreflightDomainsDraft] = React.useState<any[]>([]);
  const [preflightAnchorsDraft, setPreflightAnchorsDraft] = React.useState<any[]>([]);
  const pendingCreateRef = React.useRef<{
    request: AssembleLensRequest;
    correlationId: string;
    sessionId: string;
    query: string;
    prefetchScope: string[];
  } | null>(null);

  const isAdmin = useIsAdmin();
  const isMobile = React.useMemo(() => window.innerWidth < 768, []);
  
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
  
  // Editing state for projects
  const [editingProjectId, setEditingProjectId] = React.useState<string | null>(null);
  const [editingProjectName, setEditingProjectName] = React.useState('');
  const editingInputRef = React.useRef<HTMLInputElement | null>(null);
  
  // Load projects on mount
  React.useEffect(() => {
    if (currentUser) {
      fetchProjects();
    }
  }, [currentUser, fetchProjects]);

  // Restore saved Horizon state on mount and when projects are loaded
  const [hasRestoredProject, setHasRestoredProject] = React.useState(false);
  const [hasRestoredLens, setHasRestoredLens] = React.useState(false);
  
  // Step 1: Restore project selection
  React.useEffect(() => {
    // Wait until projects loading completes; otherwise an empty initial array can cause false restore state.
    if (!currentUser || hasRestoredProject || projectsLoading) return;

    const saved = getSavedHorizonState();

    if (saved.projectId && !currentProject) {
      const project = projects.find((p) => p.id === saved.projectId && p.projectType === 'horizon');
      if (project) {
        setCurrentProject(project);
      }
    }

    // Mark restoration finished even when projects are empty, so empty state can render.
    setHasRestoredProject(true);
  }, [currentUser, projects, currentProject, getSavedHorizonState, setCurrentProject, hasRestoredProject, projectsLoading]);

  // Step 2: Restore lens selection after project is restored
  React.useEffect(() => {
    if (!hasRestoredProject || hasRestoredLens || !currentProject) return;
    
    const saved = getSavedHorizonState();
    
    if (currentProject.id === saved.projectId && saved.lensId) {
      setSelectedLensId(saved.lensId);
    } else if (currentProject.id !== saved.projectId) {
      // Project changed, clear lens selection
      setSelectedLensId(null);
    }
    
    setHasRestoredLens(true);
  }, [hasRestoredProject, hasRestoredLens, currentProject, getSavedHorizonState, setSelectedLensId]);

  // Track previous project ID to detect project changes
  const prevProjectIdRef = React.useRef<string | null>(null);
  
  // Save project selection when currentProject changes (from SidebarPanel)
  React.useEffect(() => {
    if (!hasRestoredLens) {
      // Store current project ID for next render
      prevProjectIdRef.current = currentProject?.id || null;
      return; // Don't save during initial restore
    }
    
    const currentProjectId = currentProject?.id || null;
    const projectChanged = prevProjectIdRef.current !== currentProjectId;
    
    if (currentProject?.projectType === 'horizon') {
      // If project changed, clear lens selection (lens belongs to old project)
      if (projectChanged && prevProjectIdRef.current !== null) {
        setSelectedLensId(null);
        saveHorizonState(currentProjectId, null);
      } else {
        // Same project, save current lens selection
        const currentLensId = selectedLensId;
        saveHorizonState(currentProjectId, currentLensId);
      }
    } else if (!currentProject) {
      // Project was cleared, also clear lens
      setSelectedLensId(null);
      saveHorizonState(null, null);
    }
    
    prevProjectIdRef.current = currentProjectId;
  }, [currentProject, selectedLensId, saveHorizonState, hasRestoredLens, setSelectedLensId]);

  // Initialize WebSocket connection (same flow as Discovery).
  useWebSocketConnection();

  React.useEffect(() => {
    const openContext = async (evt: Event) => {
      const detail = (evt as CustomEvent<any>)?.detail || {};
      const projectId = String(detail?.projectId || '').trim();
      const lensId = String(detail?.lensId || '').trim();
      if (projectId) {
        const existing = projects.find((project) => project.id === projectId && project.projectType === 'horizon');
        if (existing) {
          setCurrentProject(existing);
        } else {
          try {
            const project = await api.getProject(projectId);
            setCurrentProject(project as any);
          } catch {
            // ignore hydration failures
          }
        }
      }
      if (lensId) {
        setShowWorldMap(false);
        setSelectedLensId(lensId);
      }
    };
    window.addEventListener('horizon:open-context', openContext as EventListener);
    return () => window.removeEventListener('horizon:open-context', openContext as EventListener);
  }, [projects, setCurrentProject, setSelectedLensId]);

  // Memory safety: drop accumulated realtime state when switching project/lens (store is global).
  React.useEffect(() => {
    resetRealtime();
    resetEvents();
  }, [currentProject?.id, selectedLensId, resetRealtime, resetEvents]);
  
  // Focus input when editing starts
  React.useEffect(() => {
    if (editingProjectId && editingInputRef.current) {
      editingInputRef.current.focus();
      editingInputRef.current.select();
    }
  }, [editingProjectId]);
  
  const handleDeleteChat = React.useCallback(async (projectId: string, projectName: string) => {
    if (!confirm(`Delete "${projectName}"?`)) return;
    try {
      // Check if the deleted project is currently open
      const isCurrentProject = currentProject?.id === projectId;
      
      await deleteProject(projectId);
      
      // If we deleted the current project, reset it
      if (isCurrentProject) {
        setCurrentProject(null);
        setSelectedLensId(null);
        saveHorizonState(null, null);
      }
    } catch (err) {
      console.error('Failed to delete project:', err);
    }
  }, [currentProject, deleteProject, setCurrentProject]);
  
  const handleRenameChat = React.useCallback(async (projectId: string, newName: string) => {
    if (!newName.trim()) {
      setEditingProjectId(null);
      setEditingProjectName('');
      return;
    }
    try {
      await updateProject(projectId, { displayName: newName.trim() });
      setEditingProjectId(null);
      setEditingProjectName('');
    } catch (err) {
      console.error('Failed to rename project:', err);
    }
  }, [updateProject]);
  
  const startEditingProject = React.useCallback((projectId: string, currentName: string) => {
    setEditingProjectId(projectId);
    setEditingProjectName(currentName);
  }, []);
  
  const handleResetChat = React.useCallback(() => {
    setCurrentProject(null);
    setSelectedLensId(null);
    setShowWorldMap(false);
    saveHorizonState(null, null);
    setCreateFatalError(null);
  }, [setCurrentProject, setSelectedLensId, saveHorizonState]);

  const handleCreateProject = React.useCallback(() => {
    window.dispatchEvent(new CustomEvent('horizon:create-project'));
  }, []);
  
  const handleCloseAllModals = React.useCallback(() => {
    setShowSettings(false);
    setShowAccount(false);
    useUIStore.getState().setShowAdminModal(false);
    setSelectedLensId(null);
    setCreateFatalError(null);
  }, []);

  const parseFatalPipelineError = React.useCallback((err: any, fallbackMessage: string, fallbackPhase = 'scouting'): HorizonFatalPipelineError => {
    const data = err?.response?.data;
    const nested = (data && typeof data === 'object' && data.fatalError && typeof data.fatalError === 'object')
      ? data.fatalError
      : null;

    const messageFromObject = (obj: any) =>
      String(obj?.message || obj?.error || '').trim();

    const message =
      (nested && messageFromObject(nested)) ||
      (data && typeof data === 'object' && messageFromObject(data)) ||
      (typeof data === 'string' ? data.trim() : '') ||
      String(err?.message || '').trim() ||
      fallbackMessage;

    const code =
      (nested && String(nested?.code || '').trim()) ||
      (data && typeof data === 'object' ? String(data?.code || '').trim() : '') ||
      undefined;

    const phase =
      (nested && String(nested?.phase || '').trim()) ||
      (data && typeof data === 'object' ? String(data?.phase || '').trim() : '') ||
      fallbackPhase;

    return {
      title: 'Fatal pipeline error',
      message: message || fallbackMessage,
      code: code || undefined,
      phase,
      ts: Date.now(),
    };
  }, []);

  const reportFatalPipelineError = React.useCallback((
    err: any,
    context: { projectId: string; correlationId?: string | null; sessionId?: string | null; fallbackMessage: string; fallbackPhase?: string },
  ) => {
    const fatal = parseFatalPipelineError(err, context.fallbackMessage, context.fallbackPhase || 'scouting');
    setCreateFatalError(fatal);
    try {
      useHorizonRealtimeStore.getState().handleWsEvent('horizon.assembly.failed', {
        projectId: context.projectId,
        correlationId: context.correlationId || undefined,
        sessionId: context.sessionId || undefined,
        phase: fatal.phase || 'scouting',
        stage: 'failed',
        fatal: true,
        code: fatal.code,
        error: fatal.message,
        humanReadable: {
          title: fatal.title,
          description: fatal.message,
        },
      });
    } catch {
      // ignore
    }
  }, [parseFatalPipelineError]);

  const handleAssembleLens = React.useCallback(async (request: AssembleLensRequest) => {
    // Guard against double-submit (e.g. double Enter / fast double click)
    if (creatingLensRef.current) return;
    creatingLensRef.current = true;
    setIsCreatingLens(true);
    setCreateFatalError(null);

    let attemptCorrelationId: string | null = null;
    let attemptSessionId: string | null = null;
    let openedPreflight = false;
    try {
      if (request.createEmptyLens) {
        const lens = await api.createHorizonLens(
          {
            projectId: request.projectId,
            name: String(request.name || '').trim() || 'untitled lens',
            scope: [],
            status: 'ACTIVE',
          },
          { skipScouting: true },
        );
        if (lens?.id) {
          setSelectedLensId(lens.id);
          window.dispatchEvent(new CustomEvent('horizon:lens-created', {
            detail: { lensId: lens.id, projectId: request.projectId },
          }));
        } else {
          setSelectedLensId(null);
        }
        setLensStarted(false);
        setActiveCreateCorrelationId(null);
        setActiveCreateSessionId(null);
        resetEvents();
        return;
      }

      // Two creation modes:
      // 1) Prefetch-only (chips only): create lens with explicit scope (no scouting).
      // 2) Query (+ optional prefetch scope): create lens from NL query (scouting), merging prefetchScope into final scope.
      const correlationId = (globalThis.crypto && 'randomUUID' in globalThis.crypto)
        ? (globalThis.crypto as any).randomUUID()
        : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
      const sessionId = (window as any).__horizonSessionId || correlationId;
      (window as any).__horizonSessionId = sessionId;
      attemptCorrelationId = correlationId;
      attemptSessionId = sessionId;

      // Mark current create attempt so Progress view can ignore unrelated background jobs.
      setActiveCreateCorrelationId(correlationId);
      setActiveCreateSessionId(sessionId);

      resetEvents();
      setLensStarted(true);

      // Show "Initializing..." as a first-class progress event (avoids duplicating a separate preflight header line).
      try {
        useHorizonRealtimeStore.getState().handleWsEvent('horizon.intent.initializing', {
          projectId: request.projectId,
          correlationId,
          sessionId,
          humanReadable: {
            title: 'Initializing',
            description: 'The system is determining how best to fulfill your request. This may take a few minutes depending on its complexity. Please keep this screen open until you receive confirmation.',
          },
        });
      } catch {
        // ignore
      }

      const q = String(request.query || '').trim();
      const prefetchScope = Array.from(
        new Set(
          (request.prefetchScope || [])
            .map((x) => String(x || '').trim())
            .filter(Boolean),
        ),
      );

      // Mode 1: no free-text query — create a lens directly from preselected scope.
      if (!q && prefetchScope.length > 0) {
        const defaultName = prefetchScope.length === 1
          ? prefetchScope[0].split('/').filter(Boolean).slice(-1)[0] || 'Lens'
          : `Lens (${prefetchScope.length})`;
        const lens = await api.createHorizonLens(
          {
            projectId: request.projectId,
            name: String(request.name || '').trim() || defaultName,
            scope: prefetchScope,
            status: 'ACTIVE',
          },
          { correlationId, sessionId, skipScouting: true },
        );

        if (lens?.id) {
          setSelectedLensId(lens.id);
          window.dispatchEvent(new CustomEvent('horizon:lens-created', {
            detail: { lensId: lens.id, projectId: request.projectId },
          }));
        } else {
          setSelectedLensId(null);
        }
        return;
      }

      // Mode 2: query-based lens creation — preflight first (“How we understood your request”).
      const preflight = await api.preflightHorizonLensFromQuery(
        {
          projectId: request.projectId,
          query: q,
          mode: request.filters?.mode,
          specialties: request.filters?.specialties,
        },
        { correlationId, sessionId },
      );

      pendingCreateRef.current = {
        request,
        correlationId,
        sessionId,
        query: q,
        prefetchScope,
      };
      setPreflightResult(preflight);
      setPreflightMode(
        String(preflight?.recommendation?.mode || 'research_first') === 'just_open'
          ? 'just_open'
          : 'research_first',
      );
      const domains = Array.isArray(preflight?.intent?.domains) ? preflight.intent.domains : [];
      const anchors = Array.isArray(preflight?.intent?.anchors) ? preflight.intent.anchors : [];
      const domSel: Record<string, boolean> = {};
      for (const d of domains as any[]) {
        const cp = String(d?.canonicalPath || '').trim();
        if (cp) domSel[cp] = true;
      }
      const ancSel: Record<string, boolean> = {};
      for (const a of anchors as any[]) {
        const cp = String(a?.canonicalPath || '').trim();
        if (cp) ancSel[cp] = true;
      }
      setPreflightSelectedDomains(domSel);
      setPreflightSelectedAnchors(ancSel);
      setPreflightFocus(String(preflight?.intent?.focus || '').trim());
      setPreflightDomainsDraft(Array.isArray(preflight?.intent?.domains) ? (preflight.intent.domains as any[]) : []);
      setPreflightAnchorsDraft(Array.isArray(preflight?.intent?.anchors) ? (preflight.intent.anchors as any[]) : []);
      setPreflightHotPathOnly(
        (domains as any[]).some((d: any) =>
          /^(supply-chain|value-chain)$/i.test(String(d?.contractType || '').trim())
          && String(d?.supplyChainExecutionPlan?.graphBuildMode || '').trim().toLowerCase() === 'hot_path_only'),
      );
      setPreflightOpen(true);
      openedPreflight = true;
    } catch (error) {
      console.error('Failed to assemble lens:', error);
      setLensStarted(false);
      setSelectedLensId(null);
      setActiveCreateCorrelationId(null);
      setActiveCreateSessionId(null);
      reportFatalPipelineError(error, {
        projectId: request.projectId,
        correlationId: attemptCorrelationId,
        sessionId: attemptSessionId,
        fallbackMessage: 'Failed to start Horizon pipeline.',
        fallbackPhase: 'decomposition',
      });
    } finally {
      creatingLensRef.current = false;
      setIsCreatingLens(false);
      // Keep correlation/session IDs while the preflight modal is open (so the progress feed stays correlated).
      if (!openedPreflight) {
        setActiveCreateCorrelationId(null);
        setActiveCreateSessionId(null);
      }
    }
  }, [reportFatalPipelineError, resetEvents, setSelectedLensId]);

  const closePreflight = React.useCallback(() => {
    setPreflightOpen(false);
    setPreflightResult(null);
    pendingCreateRef.current = null;
    setPreflightDomainsDraft([]);
    setPreflightAnchorsDraft([]);
    setPreflightHotPathOnly(false);
    setPreflightSelectedDomains({});
    setPreflightSelectedAnchors({});
    setPreflightFocus('');
    setActiveCreateCorrelationId(null);
    setActiveCreateSessionId(null);
    // Cancel preflight should reset the in-progress lens creation UI back to input.
    creatingLensRef.current = false;
    setIsCreatingLens(false);
    setLensStarted(false);
    resetEvents();
    resetRealtime();
    setSelectedLensId(null);
    setCreateFatalError(null);
  }, [resetEvents, resetRealtime, setSelectedLensId]);

  const confirmPreflightAndCreate = React.useCallback(async (modeOverride?: 'just_open' | 'research_first') => {
    const pending = pendingCreateRef.current;
    if (!pending) return;
    if (!preflightResult?.intent) return;
    if (creatingLensRef.current) return;

    creatingLensRef.current = true;
    setIsCreatingLens(true);
    setCreateFatalError(null);
    try {
      const filteredDomains = (preflightDomainsDraft || []).filter((d: any) => {
        const cp = String(d?.canonicalPath || '').trim();
        return cp && preflightSelectedDomains[cp] !== false;
      });
      const filteredAnchors = (preflightAnchorsDraft || []).filter((a: any) => {
        const cp = String(a?.canonicalPath || '').trim();
        return cp && preflightSelectedAnchors[cp] !== false;
      });
      const intentOverride = {
        ...preflightResult.intent,
        focus: String(preflightFocus || '').trim() || String(preflightResult?.intent?.focus || '').trim(),
        domains: filteredDomains.map((d: any) => {
          if (!/^(supply-chain|value-chain)$/i.test(String(d?.contractType || '').trim())) return d;
          const currentPlan = d?.supplyChainExecutionPlan && typeof d.supplyChainExecutionPlan === 'object'
            ? d.supplyChainExecutionPlan
            : {};
          return {
            ...d,
            supplyChainExecutionPlan: {
              ...currentPlan,
              graphBuildMode: preflightHotPathOnly ? 'hot_path_only' : 'hot_path_plus_graph',
            },
          };
        }),
        anchors: filteredAnchors,
      };

      const result = await api.createHorizonLensFromQuery(
        {
          projectId: pending.request.projectId,
          query: pending.query,
          mode: modeOverride || preflightMode,
          scoutingMode: pending.request.filters?.mode,
          intent: intentOverride,
          maxRecursionDepth: pending.request.filters?.maxRecursionDepth ?? 1,
          prefetchScope: pending.prefetchScope.length > 0 ? pending.prefetchScope : undefined,
          specialties: pending.request.filters?.specialties,
        },
        { correlationId: pending.correlationId, sessionId: pending.sessionId },
      );

      setPreflightOpen(false);
      setPreflightResult(null);
      pendingCreateRef.current = null;

      if (result?.lens?.id) {
        // Select the newly created lens so Cockpit/Details tabs become available,
        // but keep the user in Progress while the pipeline is running (lensStarted=true).
        setSelectedLensId(result.lens.id);

        // Do NOT auto-switch to the lens UI yet: when creating a lens from a query (new objects),
        // the cockpit is typically empty/unfinished. Keep user in Progress; they can open the lens
        // manually once they decide it's meaningful.
        try {
          useHorizonRealtimeStore.getState().handleWsEvent('horizon.intent.lens_created', {
            projectId: pending.request.projectId,
            lensId: result.lens.id,
            correlationId: pending.correlationId,
            sessionId: pending.sessionId,
            humanReadable: {
              title: 'Lens created',
              description: `Proceeding with scouting and enrichment… (${String(result?.lens?.name || '').trim() || 'new lens'})`,
            },
          });
        } catch {
          // ignore
        }
        window.dispatchEvent(
          new CustomEvent('horizon:lens-created', {
            detail: { lensId: result.lens.id, projectId: pending.request.projectId },
          }),
        );
      }
    } catch (err) {
      console.error('[HorizonPage] Failed to create lens after preflight', err);
      setLensStarted(false);
      setActiveCreateCorrelationId(null);
      setActiveCreateSessionId(null);
      reportFatalPipelineError(err, {
        projectId: pending.request.projectId,
        correlationId: pending.correlationId,
        sessionId: pending.sessionId,
        fallbackMessage: 'Failed to create and start Horizon lens.',
        fallbackPhase: 'scouting',
      });
    } finally {
      creatingLensRef.current = false;
      setIsCreatingLens(false);
      // Keep correlation/session IDs while we stay in Progress (until user opens the lens).
    }
  }, [preflightHotPathOnly, preflightMode, preflightResult, preflightSelectedAnchors, preflightSelectedDomains, preflightDomainsDraft, preflightAnchorsDraft, preflightFocus, reportFatalPipelineError, setSelectedLensId]);

  const isValidCanonicalPath = React.useCallback((cp: string) => {
    const s = String(cp || '').trim();
    if (!s) return false;
    if (!s.startsWith('/')) return false;
    if (/\s/.test(s)) return false;
    return true;
  }, []);

  // Auto-scroll realtime feed to bottom while started.
  React.useEffect(() => {
    if (!lensStarted) return;
    const el = feedRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [lensStarted, events.length]);
  
  return (
    <div
      className="flex h-full"
      style={{ padding: isMobile ? '8px 0' : '16px 0' }}
    >
      {/* Side panel */}
      <SidebarPanel
        isOpen={sidebarOpen}
        onOpen={() => setSidebarOpen(true)}
        onClose={() => setSidebarOpen(false)}
        isMobile={isMobile}
        isAdmin={isAdmin}
        onResetChat={handleResetChat}
        onShowSettings={() => {
          handleCloseAllModals();
          setShowSettings(true);
        }}
        onShowAccount={() => {
          handleCloseAllModals();
          setShowAccount(true);
        }}
        onShowAdmin={() => {
          handleCloseAllModals();
          useUIStore.getState().openAdminModal();
        }}
        onCloseAllModals={handleCloseAllModals}
        editingProjectId={editingProjectId}
        editingProjectName={editingProjectName}
        editingInputRef={editingInputRef}
        onEditingProjectIdChange={setEditingProjectId}
        onEditingProjectNameChange={setEditingProjectName}
        onDeleteChat={handleDeleteChat}
        onRenameChat={handleRenameChat}
        onStartEditingProject={startEditingProject}
        onProjectSelected={() => setShowWorldMap(false)}
        currentMode="horizon"
        projectId={currentProject?.id || null}
        onSelectLens={(lensId) => {
          setShowWorldMap(false);
          setSelectedLensId(lensId);
          // User explicitly chose to open a lens: stop treating this as an in-progress creation feed.
          setLensStarted(false);
          setActiveCreateCorrelationId(null);
          setActiveCreateSessionId(null);
          setCreateFatalError(null);
        }}
        selectedLensId={selectedLensId}
        onOpenWatch={() => {
          setShowWorldMap(false);
          navigate('/horizon/watch');
        }}
        isWatchActive={isWatchRoute}
        onOpenWorldMap={() => {
          navigate('/horizon');
          setShowWorldMap(true);
          setLensStarted(false);
          setActiveCreateCorrelationId(null);
          setActiveCreateSessionId(null);
          setCreateFatalError(null);
        }}
        isWorldMapActive={showWorldMap}
        onNewLens={() => {
          setShowWorldMap(false);
          // Ensure project is selected (should already be selected when hovering, but double-check)
          // Clear states and show input field for new lens
          setSelectedLensId(null);
          setLensStarted(false);
          resetEvents();
          setActiveCreateCorrelationId(null);
          setActiveCreateSessionId(null);
          setCreateFatalError(null);
          // Note: currentProject should already be set from SidebarPanel project selection
          // LensCanvas will use currentProject.id as projectId for creating the lens
        }}
      />

      {/* Main content area */}
      <div
        className="flex-1 min-h-0"
        style={{
          position: 'relative',
          display: 'flex',
          gap: 0,
        }}
      >
        {/* Content area - flexible width */}
        <div
          className="flex-1 min-h-0"
          data-horizon-graph-area="true"
          style={{
            display: 'flex',
            flexDirection: 'column',
            minWidth: 0,
            position: 'relative',
          }}
        >
          {isWatchRoute ? (
            currentProject && selectedLensId ? (
              <HorizonWatchLensSurface
                lensId={selectedLensId}
                projectName={currentProject?.displayName || null}
                onOpenWorldMap={() => {
                  navigate('/horizon');
                  setShowWorldMap(true);
                }}
              />
            ) : (
              <HorizonWatchSurface
                lensId={currentProject ? selectedLensId : null}
                projectName={currentProject?.displayName || null}
                onOpenWorldMap={() => {
                  navigate('/horizon');
                  setShowWorldMap(true);
                }}
              />
            )
          ) : showWorldMap ? (
            <WorldMapPanel />
          ) : currentProject ? (
            /* Lens Canvas overlay - show Progress view for both project and lens selection */
            <LensCanvas
              isOpen={true}
              lensId={selectedLensId}
              projectId={currentProject.id}
              isAdmin={isAdmin}
              initialViewMode={lensStarted ? 'progress' : (selectedLensId ? 'cockpit' : 'progress')}
              showInputInProgress={true}
              onAssemble={handleAssembleLens}
              // Keep Progress "active" after lens creation until user explicitly opens the lens.
              isCreatingLens={isCreatingLens || lensStarted}
              activeCreateCorrelationId={activeCreateCorrelationId}
              activeCreateSessionId={activeCreateSessionId}
              fatalError={createFatalError}
              preflight={
                preflightOpen && preflightResult
                  ? {
                      active: true,
                      reason: String(preflightResult?.recommendation?.reason || '').trim(),
                      mode: preflightMode,
                      setMode: setPreflightMode,
                      hotPathOnly: preflightHotPathOnly,
                      setHotPathOnly: setPreflightHotPathOnly,
                      focus: preflightFocus,
                      setFocus: setPreflightFocus,
                      domainsDraft: preflightDomainsDraft || [],
                      setDomainsDraft: setPreflightDomainsDraft,
                      anchorsDraft: preflightAnchorsDraft || [],
                      setAnchorsDraft: setPreflightAnchorsDraft,
                      selectedDomains: preflightSelectedDomains || {},
                      setSelectedDomains: setPreflightSelectedDomains,
                      selectedAnchors: preflightSelectedAnchors || {},
                      setSelectedAnchors: setPreflightSelectedAnchors,
                      isBusy: isCreatingLens,
                      isValidCanonicalPath,
                      onCancel: closePreflight,
                      onContinue: (mode?: 'just_open' | 'research_first') => confirmPreflightAndCreate(mode),
                    }
                  : null
              }
              onClose={() => {
                // Only close lens if one is selected, otherwise just reset state
                if (selectedLensId) {
                  setSelectedLensId(null);
                }
                setLensStarted(false);
                setActiveCreateCorrelationId(null);
                setActiveCreateSessionId(null);
                setCreateFatalError(null);
              }}
            />
          ) : projectsLoading || !hasRestoredProject ? (
            <HorizonLoading />
          ) : (
            <HorizonEmptyState onCreateProject={handleCreateProject} />
          )}
        </div>
      </div>
      
      {/* Preflight is rendered inline inside LensCanvasProgress. */}

      {showSettings && <SettingsModal isOpen={showSettings} onClose={() => setShowSettings(false)} />}
      {showAccount && <AccountModal isOpen={showAccount} onClose={() => setShowAccount(false)} />}
    </div>
  );
};

export default HorizonPage;
