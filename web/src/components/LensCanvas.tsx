/**
 * Компонент Lens Canvas
 * Показывает информацию о линзе Horizon
 */

import React from 'react';
import { useSearchParams } from 'react-router-dom';
import { ModalWithHeader } from './ModalWithHeader';
import { ThemeContext } from './ThemeProvider';
import { api, type DirectedSupplyChainExplorationRequest } from '../lib/api';
import { LensCanvasHome } from './LensCanvasHome';
import { LensCanvasCockpit } from './LensCanvasCockpit';
import { LensCanvasProgress } from './LensCanvasProgress';
import { LensAssembly } from './LensAssembly';
import { HorizonWatchLensSurface } from './HorizonWatchLensSurface';
import { HorizonLoading } from './HorizonLoading';
import { WorldMapPanel } from './WorldMapPanel';
import { QAPanel } from './QAPanel';
import { LensChatDockPlaceholder, type LensChatMapFocus } from './LensChatDockPlaceholder';
import { HorizonRealtimeCallouts } from './HorizonRealtimeCallouts';
import { HorizonDrawers, HorizonPanelType, type MapDrawerPanelType } from './HorizonDrawers';
import { ScoutIntentModal, type ScoutIntentResolution } from './ScoutIntentModal';
import type { HorizonNodeData } from './HorizonGraphNodeTypes';
import type { AssembleLensRequest } from '../features/horizon/components/hook/AssembleLensRequest';
import type { MapObjectInspectorPayload } from './MapObjectDetail';
import {
  defaultScoutingModeForIntent,
  specialtiesForScoutingMode,
  type ScoutingMode,
} from '../features/horizon/lib/scoutingMode';
import { useHorizonRealtimeStore } from '../stores/horizonRealtimeStore';
import { buildHotPathProgressView, filterHorizonProgressEvents } from './horizonProgressUtils';

/** Lens view modes: Graph | Map | Analysis (plan §1). Graph persists when switching. */
export type LensViewMode = 'graph' | 'map' | 'analysis' | 'brief';

export interface LensCanvasProps {
  isOpen: boolean;
  lensId: string | null;
  projectId?: string | null;
  sidebarOpen?: boolean;
  /** @deprecated Use lensView for graph/map/analysis. Kept for backward compat with initialViewMode. */
  initialViewMode?: 'assembly' | 'home' | 'cockpit' | 'progress';
  /** Initial lens view (Graph | Map | Analysis). Synced with URL ?view= */
  initialLensView?: LensViewMode;
  showInputInProgress?: boolean;
  onAssemble?: (request: AssembleLensRequest) => void;
  isCreatingLens?: boolean;
  /** When creating a new lens (lensId=null), filter progress events to only those matching this creation attempt. */
  activeCreateCorrelationId?: string | null;
  activeCreateSessionId?: string | null;
  /** Optional preflight UI (inlined into progress view). */
  preflight?: {
    active: boolean;
    reason?: string;
    mode: 'just_open' | 'research_first';
    setMode: (mode: 'just_open' | 'research_first') => void;
    hotPathOnly?: boolean;
    setHotPathOnly?: (next: boolean) => void;
    focus: string;
    setFocus: (focus: string) => void;
    domainsDraft: any[];
    setDomainsDraft: (next: any[]) => void;
    anchorsDraft: any[];
    setAnchorsDraft: (next: any[]) => void;
    selectedDomains: Record<string, boolean>;
    setSelectedDomains: (next: Record<string, boolean> | ((prev: Record<string, boolean>) => Record<string, boolean>)) => void;
    selectedAnchors: Record<string, boolean>;
    setSelectedAnchors: (next: Record<string, boolean> | ((prev: Record<string, boolean>) => Record<string, boolean>)) => void;
    isBusy: boolean;
    isValidCanonicalPath: (cp: string) => boolean;
    onCancel: () => void;
    onContinue: (mode?: 'just_open' | 'research_first') => void;
  } | null;
  fatalError?: {
    title?: string;
    message: string;
    code?: string;
    phase?: string;
  } | null;
  isAdmin?: boolean;
  onClose: () => void;
}

type LensCanvasSnapshot = {
  lens: any;
  assemblyStatus: any;
  scoutingJobs: any[];
};

// Keep the last fetched lens payload in memory for same-lens refreshes.
// When switching lenses we intentionally clear the visible snapshot first,
// so the user sees the standard loading state instead of stale Home content.
const lensSnapshotCache = new Map<string, LensCanvasSnapshot>();

function resolveDirectedSupplyChainDirection(intent: ScoutIntentResolution | null, rawQuery: string): 'upstream' | 'downstream' | 'both' {
  const domains = Array.isArray(intent?.domains) ? intent.domains : [];
  for (const domain of domains as any[]) {
    const contractType = String(domain?.contractType || '').trim().toLowerCase();
    if (contractType !== 'supply-chain' && contractType !== 'value-chain') continue;
    const queries = Array.isArray(domain?.supplyChainExecutionPlan?.queries) ? domain.supplyChainExecutionPlan.queries : [];
    for (const query of queries) {
      const direction = String(query?.direction || '').trim().toLowerCase();
      if (direction === 'upstream' || direction === 'downstream' || direction === 'both') return direction;
    }
  }

  const q = String(rawQuery || '').trim().toLowerCase();
  const bothHints = ['supply chain', 'value chain', 'цепоч', 'цепочка поставок', 'value-chain'];
  if (bothHints.some((token) => q.includes(token))) return 'both';
  const upstreamHints = ['upstream', 'supplier', 'suppliers', 'sourcing', 'source', 'sources', 'input', 'inputs', 'raw material', 'raw materials', 'кто поставляет', 'поставщик', 'поставщики', 'сырье', 'сырьё', 'источник', 'источники', 'снабж'];
  if (upstreamHints.some((token) => q.includes(token))) return 'upstream';
  return 'downstream';
}

export const LensCanvas: React.FC<LensCanvasProps> = ({
  isOpen,
  lensId,
  projectId,
  sidebarOpen = true,
  initialViewMode = 'home',
  initialLensView,
  showInputInProgress = false,
  onAssemble,
  isCreatingLens = false,
  activeCreateCorrelationId = null,
  activeCreateSessionId = null,
  preflight = null,
  fatalError = null,
  isAdmin = false,
  onClose,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const [lens, setLens] = React.useState<any>(null);
  const [assemblyStatus, setAssemblyStatus] = React.useState<any>(null);
  const [scoutingJobs, setScoutingJobs] = React.useState<any[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [loadingJobs, setLoadingJobs] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [viewMode, setViewMode] = React.useState<'assembly' | 'home' | 'cockpit' | 'progress'>(initialViewMode);
  const [followUpModalOpen, setFollowUpModalOpen] = React.useState(false);
  const [followUpQuery, setFollowUpQuery] = React.useState('');
  const [followUpIntent, setFollowUpIntent] = React.useState<ScoutIntentResolution | null>(null);
  const [followUpIntentLoading, setFollowUpIntentLoading] = React.useState(false);
  const [followUpIntentError, setFollowUpIntentError] = React.useState<string | null>(null);
  const [followUpRunLoading, setFollowUpRunLoading] = React.useState(false);
  const [followUpMode, setFollowUpMode] = React.useState<ScoutingMode>('general');
  const [followUpModeTouched, setFollowUpModeTouched] = React.useState(false);
  const [followUpHotPathOnly, setFollowUpHotPathOnly] = React.useState(false);
  const [followUpDirection, setFollowUpDirection] = React.useState<'upstream' | 'downstream' | 'both'>('downstream');
  const [followUpProgressActive, setFollowUpProgressActive] = React.useState(false);
  const [searchParams, setSearchParams] = useSearchParams();
  const realtimeEvents = useHorizonRealtimeStore((s) => s.events);
  const followUpPrefetchScopeRef = React.useRef<string[]>([]);

  // Lens view: Graph | Map | Analysis. Synced with URL ?view=graph|map|analysis
  const lensViewFromUrl = searchParams.get('view') as LensViewMode | null;
  const validLensView = (v: string | null): v is LensViewMode =>
    v === 'graph' || v === 'map' || v === 'analysis' || v === 'brief';
  const [lensView, setLensViewState] = React.useState<LensViewMode>(() => {
    if (validLensView(lensViewFromUrl)) return lensViewFromUrl;
    if (initialLensView && validLensView(initialLensView)) return initialLensView;
    return 'graph';
  });

  const graphModeActive = viewMode === 'cockpit' && lensView === 'graph';
  const mapModeActive = viewMode === 'cockpit' && lensView === 'map';
  const analysisModeActive = viewMode === 'cockpit' && lensView === 'analysis';
  const briefModeActive = viewMode === 'cockpit' && lensView === 'brief';
  const activeLensId = lensId || lens?.id || null;
  const activeProjectId = projectId || lens?.projectId || null;
  const tabActiveBackground = 'var(--white-10)';
  const tabHoverBackground = theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)';

  const handleTabMouseEnter = React.useCallback((event: React.MouseEvent<HTMLButtonElement>, active: boolean) => {
    if (active) return;
    event.currentTarget.style.background = tabHoverBackground;
  }, [tabHoverBackground]);

  const handleTabMouseLeave = React.useCallback((event: React.MouseEvent<HTMLButtonElement>, active: boolean) => {
    event.currentTarget.style.background = active ? tabActiveBackground : 'transparent';
  }, []);

  const setLensView = React.useCallback(
    (next: LensViewMode) => {
      setLensViewState(next);
      setSearchParams(
        (prev) => {
          const p = new URLSearchParams(prev);
          p.set('view', next);
          return p;
        },
        { replace: true }
      );
    },
    [setSearchParams]
  );

  // Sync lensView from URL (e.g. deep link)
  React.useEffect(() => {
    if (validLensView(lensViewFromUrl) && lensViewFromUrl !== lensView) {
      setLensViewState(lensViewFromUrl);
    }
  }, [lensViewFromUrl]);

  // Drawer state for cockpit mode
  const [panelType, setPanelType] = React.useState<HorizonPanelType>(null);
  const [drawerWidth, setDrawerWidth] = React.useState(400);
  const [selectedNodeId, setSelectedNodeId] = React.useState<string | null>(null);
  const [selectedNodeData, setSelectedNodeData] = React.useState<HorizonNodeData | null>(null);
  const [selectedEdgeId, setSelectedEdgeId] = React.useState<string | null>(null);
  const [selectedEdgeData, setSelectedEdgeData] = React.useState<any | null>(null);
  const [mapInspectorState, setMapInspectorState] = React.useState<MapObjectInspectorPayload | null>(null);
  const [activeMapDrawerPanel, setActiveMapDrawerPanel] = React.useState<MapDrawerPanelType | null>(null);
  const [mapDrawerHost, setMapDrawerHost] = React.useState<HTMLDivElement | null>(null);
  const [qaHighlightNodeIds, setQaHighlightNodeIds] = React.useState<string[]>([]);
  const [qaHighlightEdgeIds, setQaHighlightEdgeIds] = React.useState<string[]>([]);
  const [qaOpenTrigger, setQaOpenTrigger] = React.useState(0);
  const [chatDockOpen, setChatDockOpen] = React.useState(false);
  const [chatMapFocus, setChatMapFocus] = React.useState<LensChatMapFocus | null>(null);
  const visibleLensIdRef = React.useRef<string | null>(null);
  const mapDrawerTitle = activeMapDrawerPanel === 'view'
    ? 'View'
    : activeMapDrawerPanel === 'simulation'
      ? 'Simulation'
      : activeMapDrawerPanel === 'export-tools'
        ? 'Export / Tools'
        : null;
  const handleMapInspectorStateChange = React.useCallback((nextState: MapObjectInspectorPayload | null) => {
    setMapInspectorState(nextState);
    if (nextState) {
      setActiveMapDrawerPanel(null);
      setSelectedNodeId(null);
      setSelectedNodeData(null);
      setSelectedEdgeId(null);
      setSelectedEdgeData(null);
      setPanelType('object-inspector');
    } else if (panelType === 'object-inspector' && !selectedNodeId && !selectedEdgeId) {
      setPanelType(null);
    }
  }, [panelType, selectedEdgeId, selectedNodeId]);
  const handleMapDrawerPanelChange = React.useCallback((nextPanel: MapDrawerPanelType | null) => {
    setActiveMapDrawerPanel(nextPanel);
    if (nextPanel) {
      setMapInspectorState(null);
      setSelectedNodeId(null);
      setSelectedNodeData(null);
      setSelectedEdgeId(null);
      setSelectedEdgeData(null);
      setPanelType('map-drawer');
    } else if (panelType === 'map-drawer') {
      setPanelType(null);
    }
  }, [panelType]);

  React.useEffect(() => {
    setChatDockOpen(false);
    setChatMapFocus(null);
  }, [activeLensId]);

  // Toolbar container for Cockpit
  // NOTE: Cockpit toolbar is rendered as an overlay inside the graph canvas.
  
  const isMobile = React.useMemo(() => window.innerWidth < 768, []);

  // Ctrl+K / Cmd+K opens QA palette when in analysis view
  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k' && lensView === 'analysis') {
        e.preventDefault();
        setQaOpenTrigger((prev) => prev + 1);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [lensView]);

  // Update view mode when initialViewMode changes
  React.useEffect(() => {
    setViewMode(initialViewMode);
  }, [initialViewMode]);

  const handleOpenDocuments = React.useCallback(() => {
    if (!activeLensId) return;
    setViewMode('cockpit');
    setPanelType('documents');
    setActiveMapDrawerPanel(null);
  }, [activeLensId]);

  const assemblyFatalError = React.useMemo(() => {
    const fatal = assemblyStatus?.fatalError;
    if (!fatal || typeof fatal !== 'object') return null;
    const message = String(fatal?.message || '').trim();
    if (!message) return null;
    return {
      title: 'Fatal pipeline error',
      message,
      code: String(fatal?.code || '').trim() || undefined,
      phase: String(fatal?.phase || '').trim() || undefined,
    };
  }, [assemblyStatus]);

  const progressEvents = React.useMemo(() => filterHorizonProgressEvents(realtimeEvents || [], {
    lensId,
    projectId,
    isCreatingLens,
    preflightActive: !!preflight?.active,
    activeCreateCorrelationId,
    activeCreateSessionId,
  }), [realtimeEvents, lensId, projectId, isCreatingLens, preflight?.active, activeCreateCorrelationId, activeCreateSessionId]);

  const hotPathProgressView = React.useMemo(() => buildHotPathProgressView(progressEvents), [progressEvents]);
  const assemblyStageValue = String((assemblyStatus as any)?.stage || assemblyStatus?.phase || '').trim().toLowerCase();
  const assemblyStatusValue = String(assemblyStatus?.status || '').trim().toLowerCase();
  const hasActiveAssembly = Boolean(
    (assemblyStatusValue && assemblyStatusValue !== 'ready')
    || (assemblyStageValue && assemblyStageValue !== 'ready' && assemblyStageValue !== 'failed'),
  );
  const hasActiveScoutingJob = React.useMemo(
    () => scoutingJobs.some((job: any) => {
      const status = String(job?.status || '').trim().toLowerCase();
      if (status === 'pending' || status === 'running') return true;
      const currentState = String(job?.currentState || '').trim().toLowerCase();
      return (
        currentState === 'pending'
        || currentState === 'planning'
        || currentState === 'execution'
        || currentState === 'extraction'
        || currentState === 'validation'
        || currentState === 'resolution'
      );
    }),
    [scoutingJobs],
  );
  const showHomeTab = Boolean(projectId || lensId || lens);
  const showProgressTab = Boolean(
    preflight?.active
    || isCreatingLens
    || followUpProgressActive
    || fatalError
    || assemblyFatalError
    || hasActiveAssembly
    || progressEvents.length > 0
    || hotPathProgressView,
  );

  // Default UX:
  // - new/empty lens starts from Home
  // - existing lenses keep prior behavior unless an explicit mode was requested
  const prevLensIdRef = React.useRef<string | null>(null);
  React.useEffect(() => {
    const prev = prevLensIdRef.current;
    if (prev === lensId) return;
    prevLensIdRef.current = lensId;

    // Reset selection state between lenses for cleaner UX
    setPanelType(null);
    setActiveMapDrawerPanel(null);
    setSelectedNodeId(null);
    setSelectedNodeData(null);
    setSelectedEdgeId(null);
    setSelectedEdgeData(null);
    setMapInspectorState(null);
    setQaHighlightNodeIds([]);
    setQaHighlightEdgeIds([]);
    setFollowUpModalOpen(false);
    setFollowUpQuery('');
    setFollowUpIntent(null);
    setFollowUpIntentLoading(false);
    setFollowUpIntentError(null);
    setFollowUpRunLoading(false);
    setFollowUpMode('general');
    setFollowUpModeTouched(false);
    setFollowUpHotPathOnly(false);
    setFollowUpDirection('downstream');
    setFollowUpProgressActive(false);
    followUpPrefetchScopeRef.current = [];

    if (!lensId) {
      setViewMode('home');
    } else {
      if (initialViewMode === 'progress' && showProgressTab) {
        setViewMode('progress');
      } else if (initialViewMode === 'assembly') {
        setViewMode('assembly');
      } else if (initialViewMode === 'home') {
        setViewMode('home');
      } else {
        setViewMode('cockpit');
      }
    }
  }, [lensId, initialViewMode, showProgressTab]);

  // If no lens is selected and user tries to switch to lens-bound modes, switch back to Home.
  React.useEffect(() => {
    if (!lensId && !lens && (viewMode === 'assembly' || viewMode === 'home' || viewMode === 'cockpit')) {
      setViewMode('home');
    }
  }, [lensId, lens, viewMode]);

  React.useEffect(() => {
    if (viewMode === 'progress' && !showProgressTab) {
      setViewMode(showHomeTab ? 'home' : 'progress');
    }
  }, [viewMode, showProgressTab, showHomeTab]);

  React.useEffect(() => {
    if (!followUpProgressActive) return;
    if (
      preflight?.active
      || isCreatingLens
      || fatalError
      || assemblyFatalError
      || hasActiveAssembly
      || hasActiveScoutingJob
      || progressEvents.length > 0
      || hotPathProgressView
    ) {
      return;
    }
    setFollowUpProgressActive(false);
  }, [
    assemblyFatalError,
    fatalError,
    followUpProgressActive,
    hasActiveAssembly,
    hasActiveScoutingJob,
    hotPathProgressView,
    isCreatingLens,
    preflight?.active,
    progressEvents.length,
  ]);

  // Load lens data when lensId changes
  React.useEffect(() => {
    if (!isOpen || !lensId) {
      visibleLensIdRef.current = null;
      setLens(null);
      setAssemblyStatus(null);
      setScoutingJobs([]);
      setError(null);
      return;
    }

    const loadLens = async () => {
      const isLensSwitch = visibleLensIdRef.current !== lensId;
      if (isLensSwitch) {
        setLens(null);
        setAssemblyStatus(null);
        setScoutingJobs([]);
      } else {
        const cached = lensSnapshotCache.get(lensId);
        if (cached) {
          setLens(cached.lens);
          setAssemblyStatus(cached.assemblyStatus);
          setScoutingJobs(cached.scoutingJobs || []);
        }
      }
      setLoading(true);
      setError(null);
      try {
        const [lensData, assemblyData, jobsData] = await Promise.all([
          api.getHorizonLens(lensId),
          api.getHorizonLensAssemblyStatus(lensId).catch(() => null),
          api.listHorizonScoutingJobs(lensId, { limit: 50 }).catch(() => ({ jobs: [], total: 0 })),
        ]);
        setLens(lensData);
        setAssemblyStatus(assemblyData);
        setScoutingJobs(jobsData.jobs || []);
        visibleLensIdRef.current = lensId;
        if (initialViewMode !== 'progress') {
          const scopeSize = Array.isArray(lensData?.scope) ? lensData.scope.length : 0;
          if (scopeSize === 0) {
            setViewMode('home');
          }
        }
        lensSnapshotCache.set(lensId, {
          lens: lensData,
          assemblyStatus: assemblyData,
          scoutingJobs: jobsData.jobs || [],
        });
      } catch (err: any) {
        console.error('[LensCanvas] Failed to load lens', err);
        setError(err.response?.data?.error || err.message || 'Failed to load lens');
      } finally {
        setLoading(false);
      }
    };

    loadLens();
  }, [isOpen, lensId]);

  // Load scouting jobs separately (can be refreshed)
  const loadScoutingJobs = React.useCallback(async () => {
    if (!lensId) return;
    setLoadingJobs(true);
    try {
      const jobsData = await api.listHorizonScoutingJobs(lensId, { limit: 50 });
      setScoutingJobs(jobsData.jobs || []);
    } catch (err: any) {
      console.error('[LensCanvas] Failed to load scouting jobs', err);
    } finally {
      setLoadingJobs(false);
    }
  }, [lensId]);

  const genCorrelationId = React.useCallback(() => {
    if (globalThis.crypto && 'randomUUID' in globalThis.crypto) {
      return (globalThis.crypto as any).randomUUID();
    }
    return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  }, []);

  const refreshLensSnapshot = React.useCallback(async () => {
    if (!lensId) return null;
    const [lensData, assemblyData, jobsData] = await Promise.all([
      api.getHorizonLens(lensId),
      api.getHorizonLensAssemblyStatus(lensId).catch(() => null),
      api.listHorizonScoutingJobs(lensId, { limit: 50 }).catch(() => ({ jobs: [], total: 0 })),
    ]);
    setLens(lensData);
    setAssemblyStatus(assemblyData);
    setScoutingJobs(jobsData.jobs || []);
    lensSnapshotCache.set(lensId, {
      lens: lensData,
      assemblyStatus: assemblyData,
      scoutingJobs: jobsData.jobs || [],
    });
    return lensData;
  }, [lensId]);

  const updateLensScope = React.useCallback(
    async (nextScope: string[]) => {
      if (!lensId || !lens) return;
      const updated = await api.updateHorizonLens(lensId, {
        tenantId: lens.tenantId,
        projectId: lens.projectId,
        name: lens.name,
        scope: nextScope,
        status: lens.status,
        viewConfig: lens.viewConfig,
        alertConfig: lens.alertConfig,
        scopeFragments: lens.scopeFragments,
        resolvedQueries: lens.resolvedQueries,
        lastResolved: lens.lastResolved,
      }, { skipScouting: true });
      setLens(updated);
      lensSnapshotCache.set(lensId, {
        lens: updated,
        assemblyStatus,
        scoutingJobs,
      });
      // Refresh related UI panels best-effort.
      try {
        const [assemblyData, jobsData] = await Promise.all([
          api.getHorizonLensAssemblyStatus(lensId).catch(() => null),
          api.listHorizonScoutingJobs(lensId, { limit: 50 }).catch(() => ({ jobs: [], total: 0 })),
        ]);
        setAssemblyStatus(assemblyData);
        setScoutingJobs(jobsData.jobs || []);
        lensSnapshotCache.set(lensId, {
          lens: updated,
          assemblyStatus: assemblyData,
          scoutingJobs: jobsData.jobs || [],
        });
      } catch {
        // ignore
      }
    },
    [lensId, lens]
  );

  const closeFollowUpModal = React.useCallback(() => {
    setFollowUpModalOpen(false);
    setFollowUpQuery('');
    setFollowUpIntent(null);
    setFollowUpIntentLoading(false);
    setFollowUpIntentError(null);
    setFollowUpRunLoading(false);
    setFollowUpMode('general');
    setFollowUpModeTouched(false);
    setFollowUpHotPathOnly(false);
    setFollowUpDirection('downstream');
    followUpPrefetchScopeRef.current = [];
  }, []);

  React.useEffect(() => {
    if (!followUpModalOpen || !lensId || !followUpQuery.trim()) return;
    let alive = true;
    const correlationId = genCorrelationId();
    const sessionId = (window as any).__horizonSessionId || correlationId;
    (window as any).__horizonSessionId = sessionId;

    setFollowUpIntentLoading(true);
    setFollowUpIntentError(null);
    setFollowUpIntent(null);

    (async () => {
      try {
        const res = await api.preflightHorizonLensFollowUp(
          lensId,
          { query: followUpQuery.trim() },
          { correlationId, sessionId },
        );
        if (!alive) return;
        const nextIntent = (res?.intent || null) as ScoutIntentResolution | null;
        setFollowUpIntent(nextIntent);
        setFollowUpDirection(resolveDirectedSupplyChainDirection(nextIntent, followUpQuery));
        if (!followUpModeTouched && nextIntent) {
          setFollowUpMode(defaultScoutingModeForIntent(nextIntent));
        }
        setFollowUpIntentError(null);
      } catch (e: any) {
        if (!alive) return;
        const msg = e?.response?.data?.error || e?.message || 'Failed to resolve intent';
        setFollowUpIntentError(String(msg));
        setFollowUpIntent(null);
      } finally {
        if (alive) setFollowUpIntentLoading(false);
      }
    })();

    return () => {
      alive = false;
    };
  }, [followUpModalOpen, followUpModeTouched, followUpQuery, genCorrelationId, lensId]);

  const followUpHasSupplyChainContract = React.useMemo(() => {
    const domains = Array.isArray(followUpIntent?.domains) ? followUpIntent.domains : [];
    return domains.some((domain: any) => {
      const contractType = String(domain?.contractType || '').trim().toLowerCase();
      return contractType === 'supply-chain' || contractType === 'value-chain';
    });
  }, [followUpIntent]);

  const openFollowUpModal = React.useCallback((query: string, prefetchScope?: string[]) => {
    const q = String(query || '').trim();
    if (!q) return;
    followUpPrefetchScopeRef.current = Array.from(new Set((prefetchScope || []).map((value) => String(value || '').trim()).filter(Boolean)));
    setFollowUpQuery(q);
    setFollowUpIntent(null);
    setFollowUpIntentLoading(false);
    setFollowUpIntentError(null);
    setFollowUpRunLoading(false);
    setFollowUpMode('general');
    setFollowUpModeTouched(false);
    setFollowUpHotPathOnly(false);
    setFollowUpDirection('downstream');
    setFollowUpModalOpen(true);
  }, []);

  const runFollowUpFromModal = React.useCallback(async () => {
    if (!lensId || !lens) return;
    const q = String(followUpQuery || '').trim();
    if (!q) return;

    setFollowUpRunLoading(true);
    setFollowUpIntentError(null);
    try {
      const prefetchScope = followUpPrefetchScopeRef.current;
      if (prefetchScope.length > 0) {
        const currentScope = Array.isArray(lens?.scope) ? lens.scope : [];
        const nextScope = Array.from(new Set([...currentScope, ...prefetchScope].map((value) => String(value || '').trim()).filter(Boolean)));
        if (nextScope.length !== currentScope.length) {
          await updateLensScope(nextScope);
        }
      }

      const correlationId = genCorrelationId();
      const sessionId = (window as any).__horizonSessionId || correlationId;
      (window as any).__horizonSessionId = sessionId;

      if (followUpMode === 'supply_chain' && followUpHotPathOnly && followUpHasSupplyChainContract && followUpDirection !== 'both') {
        await api.scopeScoutHorizonLensQuery(
          lensId,
          {
            query: q,
            mode: 'supply_chain',
            specialties: specialtiesForScoutingMode('supply_chain'),
            exploration: {
              domainMode: 'supply_chain',
              direction: followUpDirection === 'upstream' ? 'upstream' : 'downstream',
              hotPathOnly: true,
              renderTarget: 'graph',
              ignoreFreshness: true,
            },
          },
          { correlationId, sessionId },
        );
      } else {
        await api.scopeScoutHorizonLensFull(
          lensId,
          {
            query: q,
            mode: followUpMode,
            specialties: specialtiesForScoutingMode(followUpMode),
          },
          { correlationId, sessionId },
        );
      }

      closeFollowUpModal();
      setFollowUpProgressActive(true);
      setViewMode('progress');
      await refreshLensSnapshot();
    } catch (e: any) {
      const msg = e?.response?.data?.error || e?.message || 'Failed to start follow-up research';
      setFollowUpIntentError(String(msg));
    } finally {
      setFollowUpRunLoading(false);
    }
  }, [
    closeFollowUpModal,
    followUpDirection,
    followUpHasSupplyChainContract,
    followUpHotPathOnly,
    followUpMode,
    followUpQuery,
    genCorrelationId,
    lens,
    lensId,
    refreshLensSnapshot,
    updateLensScope,
  ]);

  const handleHomeAssemble = React.useCallback(async (request: AssembleLensRequest) => {
    if (!lensId || !lens) {
      await onAssemble?.(request);
      return;
    }

    const q = String(request.query || '').trim();
    const prefetchScope = Array.from(
      new Set(
        (request.prefetchScope || [])
          .map((value) => String(value || '').trim())
          .filter(Boolean),
      ),
    );
    if (!q && prefetchScope.length === 0) return;

    if (!q) {
      const currentScope = Array.isArray(lens?.scope) ? lens.scope : [];
      const nextScope = Array.from(new Set([...currentScope, ...prefetchScope].map((value) => String(value || '').trim()).filter(Boolean)));
      if (nextScope.length !== currentScope.length) {
        await updateLensScope(nextScope);
        await refreshLensSnapshot();
      }
      return;
    }

    openFollowUpModal(q, prefetchScope);
  }, [lens, lensId, onAssemble, openFollowUpModal, refreshLensSnapshot, updateLensScope]);

  const handleScopeScout = React.useCallback(
    async (input: {
      fragmentId: string;
      canonicalPath?: string;
      maxRecursionDepth?: number;
      mode?: ScoutingMode;
      specialties?: string[];
      exploration?: DirectedSupplyChainExplorationRequest;
    }) => {
      if (!lensId) return;

      const correlationId = (globalThis.crypto && 'randomUUID' in globalThis.crypto)
        ? (globalThis.crypto as any).randomUUID()
        : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
      const sessionId = (window as any).__horizonSessionId || correlationId;
      (window as any).__horizonSessionId = sessionId;

      const res = await api.scopeScoutHorizonLensNode(
        lensId,
        {
          fragmentId: input.fragmentId,
          canonicalPath: input.canonicalPath,
          maxRecursionDepth: typeof input.maxRecursionDepth === 'number' ? input.maxRecursionDepth : 1,
          mode: input.mode,
          specialties: input.specialties,
          exploration: input.exploration,
        },
        { correlationId, sessionId }
      );

      // Keep local lens snapshot in sync so Home/Scope reflects the change immediately.
      if (res?.lens) {
        setLens(res.lens);
        lensSnapshotCache.set(lensId, {
          lens: res.lens,
          assemblyStatus,
          scoutingJobs,
        });
      } else {
        // Fallback: refetch lens best-effort
        try {
          const next = await api.getHorizonLens(lensId);
          setLens(next);
          lensSnapshotCache.set(lensId, {
            lens: next,
            assemblyStatus,
            scoutingJobs,
          });
        } catch {
          // ignore
        }
      }

      // Refresh related UI panels best-effort.
      try {
        const [assemblyData, jobsData] = await Promise.all([
          api.getHorizonLensAssemblyStatus(lensId).catch(() => null),
          api.listHorizonScoutingJobs(lensId, { limit: 50 }).catch(() => ({ jobs: [], total: 0 })),
        ]);
        setAssemblyStatus(assemblyData);
        setScoutingJobs(jobsData.jobs || []);
        const latestLens = res?.lens || lensSnapshotCache.get(lensId)?.lens;
        if (latestLens) {
          lensSnapshotCache.set(lensId, {
            lens: latestLens,
            assemblyStatus: assemblyData,
            scoutingJobs: jobsData.jobs || [],
          });
        }
      } catch {
        // ignore
      }
    },
    [lensId]
  );

  if (!isOpen) return null;

  // Determine title: use lens name if available, otherwise use project name or default
  const getTitle = () => {
    if (lens) return lens.name;
    if (projectId) return 'New Lens';
    return 'Lens';
  };

  return (
    <ModalWithHeader
      title={getTitle()}
      onClose={onClose}
      hideHeader={true}
      contentPadding="none"
    >
      <div className="flex flex-col h-full" style={{ position: 'relative' }}>
        <HorizonRealtimeCallouts enabled={viewMode !== 'progress'} lensId={lensId} projectId={projectId} />
        {/* View mode switcher: render as a consistent overlay (same place & style in all modes). */}
        <div
          role="tablist"
          aria-label="Lens view mode"
          style={{
            position: 'absolute',
            top: 12,
            left: 12,
            zIndex: 80,
            display: 'inline-flex',
            alignItems: 'center',
            gap: 4,
            padding: '6px 8px',
            borderRadius: 12,
            border: '1px solid var(--border-subtle)',
            background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.65)',
            backdropFilter: 'blur(8px)',
            boxShadow: theme === 'dark' ? '0 8px 20px rgba(0,0,0,0.35)' : '0 8px 20px rgba(0,0,0,0.12)',
            pointerEvents: 'auto',
          }}
        >
          {showHomeTab && (
            <>
              <button
                type="button"
                role="tab"
                aria-selected={viewMode === 'home'}
                aria-controls="lens-home-panel"
                onClick={() => setViewMode('home')}
                onMouseEnter={(event) => handleTabMouseEnter(event, viewMode === 'home')}
                onMouseLeave={(event) => handleTabMouseLeave(event, viewMode === 'home')}
                title="Home"
                style={{
                  border: 'none',
                  background: viewMode === 'home' ? tabActiveBackground : 'transparent',
                  color: 'var(--text-heading)',
                  borderRadius: 10,
                  padding: '6px 8px',
                  cursor: 'pointer',
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <svg aria-hidden="true" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" style={{ display: 'block' }}>
                  <path d="M3 10.5 12 3l9 7.5" />
                  <path d="M5.5 9.5V21h13V9.5" />
                  <path d="M9.5 21v-6h5v6" />
                </svg>
              </button>
            </>
          )}
          {(lensId || lens) ? (
            <>
              <div style={{ width: 1, height: 16, background: 'var(--border-subtle)', margin: '0 4px' }} />
              {/* Graph | Map | Analysis — plan §1. Graph persists when switching. */}
              <button
                type="button"
                role="tab"
                aria-selected={graphModeActive}
                aria-controls="lens-graph-panel"
                onClick={() => { setLensView('graph'); setViewMode('cockpit'); }}
                onMouseEnter={(event) => handleTabMouseEnter(event, graphModeActive)}
                onMouseLeave={(event) => handleTabMouseLeave(event, graphModeActive)}
                title="Graph"
                style={{
                  border: 'none',
                  background: graphModeActive ? tabActiveBackground : 'transparent',
                  color: 'var(--text-heading)',
                  borderRadius: 10,
                  padding: '6px 10px',
                  cursor: 'pointer',
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontSize: 12,
                  fontWeight: 600,
                }}
              >
                Graph
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={mapModeActive}
                aria-controls="lens-map-panel"
                onClick={() => { setLensView('map'); setViewMode('cockpit'); }}
                onMouseEnter={(event) => handleTabMouseEnter(event, mapModeActive)}
                onMouseLeave={(event) => handleTabMouseLeave(event, mapModeActive)}
                title="Map"
                style={{
                  border: 'none',
                  background: mapModeActive ? tabActiveBackground : 'transparent',
                  color: 'var(--text-heading)',
                  borderRadius: 10,
                  padding: '6px 10px',
                  cursor: 'pointer',
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontSize: 12,
                  fontWeight: 600,
                }}
              >
                Map
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={analysisModeActive}
                aria-controls="lens-analysis-panel"
                onClick={() => { setLensView('analysis'); setViewMode('cockpit'); }}
                onMouseEnter={(event) => handleTabMouseEnter(event, analysisModeActive)}
                onMouseLeave={(event) => handleTabMouseLeave(event, analysisModeActive)}
                title="Analysis"
                style={{
                  border: 'none',
                  background: analysisModeActive ? tabActiveBackground : 'transparent',
                  color: 'var(--text-heading)',
                  borderRadius: 10,
                  padding: '6px 10px',
                  cursor: 'pointer',
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontSize: 12,
                  fontWeight: 600,
                }}
              >
                Analysis
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={briefModeActive}
                aria-controls="lens-brief-panel"
                onClick={() => { setLensView('brief'); setViewMode('cockpit'); }}
                onMouseEnter={(event) => handleTabMouseEnter(event, briefModeActive)}
                onMouseLeave={(event) => handleTabMouseLeave(event, briefModeActive)}
                title="Lens Brief"
                style={{
                  border: 'none',
                  background: briefModeActive ? tabActiveBackground : 'transparent',
                  color: 'var(--text-heading)',
                  borderRadius: 10,
                  padding: '6px 10px',
                  cursor: 'pointer',
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontSize: 12,
                  fontWeight: 600,
                }}
              >
                Lens Brief
              </button>
              <div style={{ width: 1, height: 16, background: 'var(--border-subtle)', margin: '0 4px' }} />
              <button
                type="button"
                onClick={handleOpenDocuments}
                disabled={!activeLensId}
                title="Open lens documents"
                style={{
                  border: '1px solid var(--border-subtle)',
                  background: panelType === 'documents' ? tabActiveBackground : 'rgba(236, 179, 56, 0.16)',
                  color: 'var(--text-heading)',
                  borderRadius: 10,
                  padding: '6px 10px',
                  cursor: !activeLensId ? 'not-allowed' : 'pointer',
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontSize: 12,
                  fontWeight: 700,
                  opacity: !activeLensId ? 0.55 : 1,
                }}
              >
                Documents
              </button>
            </>
          ) : null}
          {showProgressTab ? (
            <button
              type="button"
              role="tab"
              aria-selected={viewMode === 'progress'}
              aria-controls="lens-progress-panel"
              onClick={() => setViewMode('progress')}
              onMouseEnter={(event) => handleTabMouseEnter(event, viewMode === 'progress')}
              onMouseLeave={(event) => handleTabMouseLeave(event, viewMode === 'progress')}
              title="Progress"
              style={{
                border: 'none',
                background: viewMode === 'progress' ? tabActiveBackground : 'transparent',
                color: 'var(--text-heading)',
                borderRadius: 10,
                padding: '6px 8px',
                cursor: 'pointer',
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
              }}
            >
              <svg aria-hidden="true" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" style={{ display: 'block' }}>
                <path d="M9 6h12" />
                <path d="M9 12h12" />
                <path d="M9 18h12" />
                <path d="M3.5 6l1.5 1.5L7.5 5" />
                <path d="M3.5 12l1.5 1.5L7.5 11" />
              </svg>
            </button>
          ) : null}
        </div>

        <div 
          className={(viewMode === 'assembly' || viewMode === 'cockpit' || viewMode === 'progress' || viewMode === 'home') ? '' : 'space-y-6 py-2'} 
          style={{ 
            display: 'flex', 
            flexDirection: 'column', 
            flex: (viewMode === 'assembly' || viewMode === 'cockpit' || viewMode === 'progress' || viewMode === 'home') ? 1 : 'none',
            minHeight: (viewMode === 'assembly' || viewMode === 'cockpit' || viewMode === 'progress' || viewMode === 'home') ? 0 : 'auto',
            overflow: (viewMode === 'assembly' || viewMode === 'cockpit' || viewMode === 'progress' || viewMode === 'home') ? 'hidden' : 'visible',
          }}
        >
          {loading && !lens && !!lensId ? (
            <HorizonLoading label="Loading lens..." />
          ) : error && !lens ? (
          <div className="flex items-center justify-center py-8">
            <div className="text-xs text-center" style={{ color: '#f87171' }}>
              {error}
            </div>
          </div>
        ) : (
          <>
            {viewMode === 'progress' && (
              <LensCanvasProgress 
                lensId={lensId} 
                projectId={projectId || lens?.projectId} 
                showInput={false}
                onAssemble={onAssemble}
                isCreatingLens={isCreatingLens}
                isStartingFollowUp={followUpProgressActive}
                activeCreateCorrelationId={activeCreateCorrelationId}
                activeCreateSessionId={activeCreateSessionId}
                preflight={preflight}
                fatalError={fatalError || assemblyFatalError}
              />
            )}
            {viewMode === 'assembly' && lens && (
              <div id="lens-assembly-panel" style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
                <LensAssembly
                  lens={lens}
                  projectId={projectId || lens?.projectId}
                  onApplyScope={updateLensScope}
                  onOpenGraph={() => {
                    setLensView('graph');
                    setViewMode('cockpit');
                  }}
                  onOpenMap={() => {
                    setLensView('map');
                    setViewMode('cockpit');
                  }}
                  onAssemble={onAssemble}
                />
              </div>
            )}
            {viewMode === 'home' && (lens || projectId) && (
              <LensCanvasHome
                lens={lens}
                projectId={projectId || lens?.projectId}
                showInput={showInputInProgress}
                onAssemble={lens ? handleHomeAssemble : onAssemble}
                onOpenGraph={() => {
                  setLensView('graph');
                  setViewMode('cockpit');
                }}
                onOpenMap={() => {
                  setLensView('map');
                  setViewMode('cockpit');
                }}
                onOpenDocuments={activeLensId ? handleOpenDocuments : undefined}
                isCreatingLens={isCreatingLens}
                assemblyStatus={assemblyStatus}
                scoutingJobs={scoutingJobs}
                loadingJobs={loadingJobs}
                onRefreshJobs={loadScoutingJobs}
                onUpdateScope={updateLensScope}
              />
            )}
            {viewMode === 'cockpit' && lens && (
              <>
                <div
                  style={{
                    flex: 1,
                    minHeight: 0,
                    display: 'flex',
                    flexDirection: 'row',
                    gap: 0,
                    width: '100%',
                    height: '100%',
                  }}
                >
                  <div
                    style={{
                      flex: 1,
                      minHeight: 0,
                      display: 'flex',
                      flexDirection: 'column',
                      minWidth: 0,
                      position: 'relative',
                    }}
                  >
                    {/* Map — render only when active to avoid loading map until needed */}
                    {lensView === 'map' && (
                      <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
                        <WorldMapPanel
                          scope={lensId ? { lensId } : undefined}
                          onOpenDocuments={activeLensId ? handleOpenDocuments : null}
                          chatMapFocus={chatMapFocus}
                          onClearChatMapFocus={() => setChatMapFocus(null)}
                          onMapInspectorStateChange={handleMapInspectorStateChange}
                          activeMapDrawerPanel={activeMapDrawerPanel}
                          onMapDrawerPanelChange={handleMapDrawerPanelChange}
                          mapDrawerHost={mapDrawerHost}
                        />
                      </div>
                    )}
                    {/* Analysis — Q&A (plan §10) */}
                    {lensView === 'analysis' && (
                      <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
                        <QAPanel
                          lensId={lensId!}
                          projectId={lens?.projectId ?? projectId ?? undefined}
                          onHighlightNodes={setQaHighlightNodeIds}
                          onHighlightEdges={setQaHighlightEdgeIds}
                          onHighlightTracePaths={(_paths) => {
                            /* Trace paths: could pan/zoom to path; keep for future */
                          }}
                          externalOpenTrigger={qaOpenTrigger}
                        />
                      </div>
                    )}
                    {lensView === 'brief' && lensId && (
                      <div
                        id="lens-brief-panel"
                        style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}
                      >
                        <HorizonWatchLensSurface
                          lensId={lensId}
                          projectName={lens?.name || lens?.projectName || projectId || null}
                          onOpenWorldMap={() => setLensView('map')}
                          isAdmin={isAdmin}
                        />
                      </div>
                    )}
                    {/* Graph — persists when switching (plan §1). Hidden but mounted when Map/Analysis active. */}
                    <div
                      style={{
                        flex: 1,
                        minHeight: 0,
                        display: lensView === 'graph' ? 'flex' : 'none',
                        flexDirection: 'column',
                        width: '100%',
                        height: '100%',
                      }}
                    >
                      <LensCanvasCockpit
                        lensId={lensId}
                        projectId={lens.projectId ?? projectId}
                        qaHighlightNodeIds={qaHighlightNodeIds.length > 0 ? qaHighlightNodeIds : undefined}
                        qaHighlightEdgeIds={qaHighlightEdgeIds.length > 0 ? qaHighlightEdgeIds : undefined}
                        onScopeScout={handleScopeScout}
                        inspectorActive={panelType === 'object-inspector'}
                        onToggleInspector={() => setPanelType(panelType === 'object-inspector' ? null : 'object-inspector')}
                        onNodeSelect={(nodeId, nodeData) => {
                          if (import.meta.env.DEV) console.log('[LensCanvas] onNodeSelect', { nodeId, hasNodeData: !!nodeData });
                          setMapInspectorState(null);
                          setActiveMapDrawerPanel(null);
                          setSelectedNodeId(nodeId);
                          setSelectedNodeData(nodeData);
                          // Clear edge selection when node is selected
                          setSelectedEdgeId(null);
                          setSelectedEdgeData(null);
                        }}
                        onEdgeSelect={(edgeId, edgeData) => {
                          if (import.meta.env.DEV) console.log('[LensCanvas] onEdgeSelect', { edgeId, hasEdgeData: !!edgeData, edgeData });
                          setMapInspectorState(null);
                          setActiveMapDrawerPanel(null);
                          setSelectedEdgeId(edgeId);
                          setSelectedEdgeData(edgeData);
                          // Clear node selection when edge is selected
                          setSelectedNodeId(null);
                          setSelectedNodeData(null);
                        }}
                      />
                    </div>
                  </div>

                  {/* Right drawer */}
                  <HorizonDrawers
                    panelType={panelType}
                    isMobile={isMobile}
                    drawerWidth={drawerWidth}
                    setDrawerWidth={setDrawerWidth}
                    closeAllPanels={() => {
                      mapInspectorState?.onClose();
                      setMapInspectorState(null);
                      setActiveMapDrawerPanel(null);
                      setPanelType(null);
                    }}
                    getPanelVisibilityStyles={(visible: boolean) => ({
                      display: visible ? 'block' : 'none',
                      opacity: visible ? 1 : 0,
                      transition: 'opacity 0.2s ease-in-out',
                    })}
                    projectId={activeProjectId}
                    lensId={lensId}
                    lensName={lens?.name || null}
                    selectedNodeId={selectedNodeId}
                    selectedNodeData={selectedNodeData}
                    selectedEdgeId={selectedEdgeId}
                    selectedEdgeData={selectedEdgeData}
                    mapInspectorState={mapInspectorState}
                    mapDrawerTitle={mapDrawerTitle}
                    setMapDrawerHost={setMapDrawerHost}
                  />
                </div>
              </>
            )}
            {viewMode !== 'progress' && !lens && !!lensId && (
              <HorizonLoading label="Loading lens..." />
            )}
          </>
        )}
        {activeLensId && viewMode !== 'progress' ? (
          <LensChatDockPlaceholder
            isOpen={chatDockOpen}
            onOpen={() => setChatDockOpen(true)}
            onClose={() => setChatDockOpen(false)}
            isMobile={isMobile}
            sidebarOpen={sidebarOpen}
            lensId={activeLensId}
            projectId={activeProjectId}
            lensName={lens?.name || null}
            activeMapFocusSourceId={chatMapFocus?.sourceMessageId || null}
            onShowOnMap={(focus) => {
              setChatMapFocus(focus);
              setLensView('map');
              setViewMode('cockpit');
            }}
            onClearMapFocus={() => setChatMapFocus(null)}
          />
        ) : null}
      </div>
      <ScoutIntentModal
        visible={followUpModalOpen}
        query={followUpQuery}
        intent={followUpIntent}
        intentLoading={followUpIntentLoading}
        intentError={followUpIntentError}
        runLoading={followUpRunLoading}
        progressLog={[]}
        mode={followUpMode}
        showHotPathOption={followUpHasSupplyChainContract}
        hotPathOnly={followUpHotPathOnly}
        onHotPathOnlyChange={setFollowUpHotPathOnly}
        supplyChainDirection={followUpDirection}
        onSupplyChainDirectionChange={setFollowUpDirection}
        onModeChange={(mode) => {
          setFollowUpMode(mode);
          setFollowUpModeTouched(true);
        }}
        onClose={closeFollowUpModal}
        onRunScout={() => void runFollowUpFromModal()}
        theme={theme as 'light' | 'dark'}
      />
      </div>
    </ModalWithHeader>
  );
};
