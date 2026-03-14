/**
 * Компонент Lens Canvas
 * Показывает информацию о линзе Horizon
 */

import React from 'react';
import { useSearchParams } from 'react-router-dom';
import { ModalWithHeader } from './ModalWithHeader';
import { ThemeContext } from './ThemeProvider';
import { api } from '../lib/api';
import { LensCanvasDetails } from './LensCanvasDetails';
import { LensCanvasCockpit } from './LensCanvasCockpit';
import { LensCanvasProgress } from './LensCanvasProgress';
import { WorldMapPanel } from './WorldMapPanel';
import { QAPanel } from './QAPanel';
import { HorizonRealtimeCallouts } from './HorizonRealtimeCallouts';
import { HorizonDrawers, HorizonPanelType } from './HorizonDrawers';
import type { HorizonNodeData } from './HorizonGraphNodeTypes';
import type { AssembleLensRequest } from '../features/horizon/components/hook/AssembleLensRequest';

/** Lens view modes: Graph | Map | Analysis (plan §1). Graph persists when switching. */
export type LensViewMode = 'graph' | 'map' | 'analysis';

export interface LensCanvasProps {
  isOpen: boolean;
  lensId: string | null;
  projectId?: string | null;
  /** @deprecated Use lensView for graph/map/analysis. Kept for backward compat with initialViewMode. */
  initialViewMode?: 'details' | 'cockpit' | 'progress';
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
  onClose: () => void;
}

type LensCanvasSnapshot = {
  lens: any;
  assemblyStatus: any;
  scoutingJobs: any[];
};

// Keep the last fetched lens payload in memory so incidental remounts or refreshes
// do not blank the entire canvas with a blocking loader.
const lensSnapshotCache = new Map<string, LensCanvasSnapshot>();

export const LensCanvas: React.FC<LensCanvasProps> = ({
  isOpen,
  lensId,
  projectId,
  initialViewMode = 'details',
  initialLensView,
  showInputInProgress = false,
  onAssemble,
  isCreatingLens = false,
  activeCreateCorrelationId = null,
  activeCreateSessionId = null,
  preflight = null,
  fatalError = null,
  onClose,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const [lens, setLens] = React.useState<any>(null);
  const [assemblyStatus, setAssemblyStatus] = React.useState<any>(null);
  const [scoutingJobs, setScoutingJobs] = React.useState<any[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [loadingJobs, setLoadingJobs] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [viewMode, setViewMode] = React.useState<'details' | 'cockpit' | 'progress'>(initialViewMode);
  const [searchParams, setSearchParams] = useSearchParams();

  // Lens view: Graph | Map | Analysis. Synced with URL ?view=graph|map|analysis
  const lensViewFromUrl = searchParams.get('view') as LensViewMode | null;
  const validLensView = (v: string | null): v is LensViewMode =>
    v === 'graph' || v === 'map' || v === 'analysis';
  const [lensView, setLensViewState] = React.useState<LensViewMode>(() => {
    if (validLensView(lensViewFromUrl)) return lensViewFromUrl;
    if (initialLensView && validLensView(initialLensView)) return initialLensView;
    return 'graph';
  });

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
  const [qaHighlightNodeIds, setQaHighlightNodeIds] = React.useState<string[]>([]);
  const [qaHighlightEdgeIds, setQaHighlightEdgeIds] = React.useState<string[]>([]);
  const [qaOpenTrigger, setQaOpenTrigger] = React.useState(0);

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

  // Default UX: when a lens is selected, show Cockpit by default.
  // When no lens is selected, fall back to Progress.
  const prevLensIdRef = React.useRef<string | null>(null);
  React.useEffect(() => {
    const prev = prevLensIdRef.current;
    if (prev === lensId) return;
    prevLensIdRef.current = lensId;

    // Reset selection state between lenses for cleaner UX
    setPanelType(null);
    setSelectedNodeId(null);
    setSelectedNodeData(null);
    setSelectedEdgeId(null);
    setSelectedEdgeData(null);
    setQaHighlightNodeIds([]);
    setQaHighlightEdgeIds([]);

    if (!lensId) {
      setViewMode('progress');
    } else {
      // When a lens is created from a query, HorizonPage keeps initialViewMode='progress'
      // so the user stays in the progress feed. Respect that choice and do not auto-switch.
      setViewMode(initialViewMode === 'progress' ? 'progress' : 'cockpit');
    }
  }, [lensId, initialViewMode]);

  // If no lens is selected and user tries to switch to details/cockpit, switch back to progress
  React.useEffect(() => {
    if (!lensId && !lens && (viewMode === 'details' || viewMode === 'cockpit')) {
      setViewMode('progress');
    }
  }, [lensId, lens, viewMode]);

  // Load lens data when lensId changes
  React.useEffect(() => {
    if (!isOpen || !lensId) {
      setLens(null);
      setAssemblyStatus(null);
      setScoutingJobs([]);
      setError(null);
      return;
    }

    const loadLens = async () => {
      const cached = lensSnapshotCache.get(lensId);
      if (cached) {
        setLens(cached.lens);
        setAssemblyStatus(cached.assemblyStatus);
        setScoutingJobs(cached.scoutingJobs || []);
      }
      setLoading(!cached);
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
      });
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

  const handleScopeScout = React.useCallback(
    async (input: { fragmentId: string; canonicalPath?: string; maxRecursionDepth?: number }) => {
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
        },
        { correlationId, sessionId }
      );

      // Keep local lens snapshot in sync so Details/Scope reflects the change immediately.
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
          {(lensId || lens) && (
            <>
              {/* Graph | Map | Analysis — plan §1. Graph persists when switching. */}
              <button
                type="button"
                role="tab"
                aria-selected={lensView === 'graph'}
                aria-controls="lens-graph-panel"
                onClick={() => { setLensView('graph'); setViewMode('cockpit'); }}
                title="Graph"
                style={{
                  border: 'none',
                  background: lensView === 'graph' ? 'var(--white-10)' : 'transparent',
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
                aria-selected={lensView === 'map'}
                aria-controls="lens-map-panel"
                onClick={() => { setLensView('map'); setViewMode('cockpit'); }}
                title="Map"
                style={{
                  border: 'none',
                  background: lensView === 'map' ? 'var(--white-10)' : 'transparent',
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
                aria-selected={lensView === 'analysis'}
                aria-controls="lens-analysis-panel"
                onClick={() => { setLensView('analysis'); setViewMode('cockpit'); }}
                title="Analysis"
                style={{
                  border: 'none',
                  background: lensView === 'analysis' ? 'var(--white-10)' : 'transparent',
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
              <div style={{ width: 1, height: 16, background: 'var(--border-subtle)', margin: '0 4px' }} />
              <button
                type="button"
                role="tab"
                aria-selected={viewMode === 'details'}
                aria-controls="lens-details-panel"
                onClick={() => setViewMode('details')}
                title="Details"
                style={{
                  border: 'none',
                  background: viewMode === 'details' ? 'var(--white-10)' : 'transparent',
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
                  <circle cx="12" cy="12" r="10" />
                  <path d="M12 16v-4" />
                  <path d="M12 8h.01" />
                </svg>
              </button>
            </>
          )}
          <button
            type="button"
            role="tab"
            aria-selected={viewMode === 'progress'}
            aria-controls="lens-progress-panel"
            onClick={() => setViewMode('progress')}
            title="Progress"
            style={{
              border: 'none',
              background: viewMode === 'progress' ? 'var(--white-10)' : 'transparent',
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
        </div>

        <div 
          className={(viewMode === 'cockpit' || viewMode === 'progress' || viewMode === 'details') ? '' : 'space-y-6 py-2'} 
          style={{ 
            display: 'flex', 
            flexDirection: 'column', 
            flex: (viewMode === 'cockpit' || viewMode === 'progress' || viewMode === 'details') ? 1 : 'none',
            minHeight: (viewMode === 'cockpit' || viewMode === 'progress' || viewMode === 'details') ? 0 : 'auto',
            overflow: (viewMode === 'cockpit' || viewMode === 'progress' || viewMode === 'details') ? 'hidden' : 'visible',
          }}
        >
          {loading && !lens ? (
            <div
              style={{
                flex: 1,
                minHeight: 0,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                padding: 24,
              }}
            >
              <div
                style={{
                  display: 'flex',
                  flexDirection: 'column',
                  alignItems: 'center',
                  gap: 10,
                  padding: '16px 20px',
                  borderRadius: 16,
                  border: '1px solid var(--border-subtle)',
                  background: 'var(--bg-elevated)',
                  boxShadow: '0 12px 40px rgba(0,0,0,0.18)',
                  minWidth: 220,
                }}
              >
                <style>{`
                  @keyframes horizon-dot-bounce {
                    0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
                    40% { transform: translateY(-6px); opacity: 1; }
                  }
                `}</style>
                <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  {[0, 1, 2].map((i) => (
                    <div
                      key={i}
                      style={{
                        width: 10,
                        height: 10,
                        borderRadius: 999,
                        background: 'var(--accent-primary, #3B82F6)',
                        animation: 'horizon-dot-bounce 1.1s infinite ease-in-out',
                        animationDelay: `${i * 0.14}s`,
                      }}
                    />
                  ))}
                </div>
                <div style={{ fontSize: 12, fontWeight: 650, color: 'var(--text-secondary-strong)' }}>
                  Loading lens…
                </div>
              </div>
            </div>
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
                showInput={showInputInProgress}
                onAssemble={onAssemble}
                isCreatingLens={isCreatingLens}
                activeCreateCorrelationId={activeCreateCorrelationId}
                activeCreateSessionId={activeCreateSessionId}
                preflight={preflight}
                fatalError={fatalError || assemblyFatalError}
              />
            )}
            {viewMode === 'details' && lens && (
              <LensCanvasDetails
                lens={lens}
                assemblyStatus={assemblyStatus}
                scoutingJobs={scoutingJobs}
                loadingJobs={loadingJobs}
                onRefreshJobs={loadScoutingJobs}
            onUpdateScope={updateLensScope}
              />
            )}
            {viewMode === 'cockpit' && lens && (
              <>
                {/* Map — render only when active to avoid loading map until needed */}
                {lensView === 'map' && (
                  <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
                    <WorldMapPanel scope={lensId ? { lensId } : undefined} />
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
                {/* Graph — persists when switching (plan §1). Hidden but mounted when Map/Analysis active. */}
                <div style={{ 
                  flex: 1, 
                  minHeight: 0, 
                  display: lensView === 'graph' ? 'flex' : 'none',
                  flexDirection: 'row',
                  gap: 0,
                  width: '100%', 
                  height: '100%' 
                }}>
                  <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', minWidth: 0, position: 'relative' }}>
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
                      setSelectedNodeId(nodeId);
                      setSelectedNodeData(nodeData);
                      // Clear edge selection when node is selected
                      setSelectedEdgeId(null);
                      setSelectedEdgeData(null);
                    }}
                    onEdgeSelect={(edgeId, edgeData) => {
                      if (import.meta.env.DEV) console.log('[LensCanvas] onEdgeSelect', { edgeId, hasEdgeData: !!edgeData, edgeData });
                      setSelectedEdgeId(edgeId);
                      setSelectedEdgeData(edgeData);
                      // Clear node selection when edge is selected
                      setSelectedNodeId(null);
                      setSelectedNodeData(null);
                    }}
                  />
                </div>

                {/* Right drawer */}
                <HorizonDrawers
                  panelType={panelType}
                  isMobile={isMobile}
                  drawerWidth={drawerWidth}
                  setDrawerWidth={setDrawerWidth}
                  closeAllPanels={() => setPanelType(null)}
                  getPanelVisibilityStyles={(visible: boolean) => ({
                    display: visible ? 'block' : 'none',
                    opacity: visible ? 1 : 0,
                    transition: 'opacity 0.2s ease-in-out',
                  })}
                  lensId={lensId}
                  selectedNodeId={selectedNodeId}
                  selectedNodeData={selectedNodeData}
                  selectedEdgeId={selectedEdgeId}
                  selectedEdgeData={selectedEdgeData}
                />
              </div>
            </>
            )}
            {viewMode !== 'progress' && !lens && (
              <div
                style={{
                  flex: 1,
                  minHeight: 0,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  padding: 24,
                }}
              >
                <div
                  style={{
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    gap: 10,
                    padding: '16px 20px',
                    borderRadius: 16,
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-elevated)',
                    boxShadow: '0 12px 40px rgba(0,0,0,0.18)',
                    minWidth: 220,
                  }}
                >
                  <style>{`
                    @keyframes horizon-dot-bounce {
                      0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
                      40% { transform: translateY(-6px); opacity: 1; }
                    }
                  `}</style>
                  <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                    {[0, 1, 2].map((i) => (
                      <div
                        key={i}
                        style={{
                          width: 10,
                          height: 10,
                          borderRadius: 999,
                          background: 'var(--accent-primary, #3B82F6)',
                          animation: 'horizon-dot-bounce 1.1s infinite ease-in-out',
                          animationDelay: `${i * 0.14}s`,
                        }}
                      />
                    ))}
                  </div>
                  <div style={{ fontSize: 12, fontWeight: 650, color: 'var(--text-secondary-strong)' }}>
                    Loading lens…
                  </div>
                </div>
              </div>
            )}
          </>
        )}
      </div>
      </div>
    </ModalWithHeader>
  );
};
