/**
 * Компонент Lens Canvas
 * Показывает информацию о линзе Horizon
 */

import React from 'react';
import { ModalWithHeader } from './ModalWithHeader';
import { ThemeContext } from './ThemeProvider';
import { api } from '../lib/api';
import { LensCanvasDetails } from './LensCanvasDetails';
import { LensCanvasCockpit } from './LensCanvasCockpit';
import { LensCanvasProgress } from './LensCanvasProgress';
import { HorizonRealtimeCallouts } from './HorizonRealtimeCallouts';
import { HorizonDrawers, HorizonPanelType } from './HorizonDrawers';
import { HorizonPanelButtons } from './HorizonPanelButtons';
import type { HorizonNodeData } from './HorizonGraphNodeTypes';
import type { AssembleLensRequest } from '../features/horizon/components/hook/AssembleLensRequest';

export interface LensCanvasProps {
  isOpen: boolean;
  lensId: string | null;
  projectId?: string | null;
  initialViewMode?: 'details' | 'cockpit' | 'progress';
  showInputInProgress?: boolean;
  onAssemble?: (request: AssembleLensRequest) => void;
  isCreatingLens?: boolean;
  onClose: () => void;
}

export const LensCanvas: React.FC<LensCanvasProps> = ({
  isOpen,
  lensId,
  projectId,
  initialViewMode = 'details',
  showInputInProgress = false,
  onAssemble,
  isCreatingLens = false,
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
  
  // Drawer state for cockpit mode
  const [panelType, setPanelType] = React.useState<HorizonPanelType>(null);
  const [drawerWidth, setDrawerWidth] = React.useState(400);
  const [selectedNodeId, setSelectedNodeId] = React.useState<string | null>(null);
  const [selectedNodeData, setSelectedNodeData] = React.useState<HorizonNodeData | null>(null);
  const [selectedEdgeId, setSelectedEdgeId] = React.useState<string | null>(null);
  const [selectedEdgeData, setSelectedEdgeData] = React.useState<any | null>(null);
  
  // Toolbar container for Cockpit
  const [toolbarContainer, setToolbarContainer] = React.useState<HTMLDivElement | null>(null);
  
  const isMobile = React.useMemo(() => window.innerWidth < 768, []);

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

    if (!lensId) {
      setViewMode('progress');
    } else {
      setViewMode('cockpit');
    }
  }, [lensId]);

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
      } catch (err: any) {
        console.error('[LensCanvas] Failed to load lens', err);
        setError(err.response?.data?.error || err.message || 'Failed to load lens');
        setLens(null);
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
      // Refresh related UI panels best-effort.
      try {
        const [assemblyData, jobsData] = await Promise.all([
          api.getHorizonLensAssemblyStatus(lensId).catch(() => null),
          api.listHorizonScoutingJobs(lensId, { limit: 50 }).catch(() => ({ jobs: [], total: 0 })),
        ]);
        setAssemblyStatus(assemblyData);
        setScoutingJobs(jobsData.jobs || []);
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
      } else {
        // Fallback: refetch lens best-effort
        try {
          const next = await api.getHorizonLens(lensId);
          setLens(next);
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
    >
      <div className="flex flex-col h-full" style={{ position: 'relative' }}>
        <HorizonRealtimeCallouts enabled={viewMode !== 'progress'} lensId={lensId} projectId={projectId} />
        {/* View mode switcher and Toolbar */}
        <div className="mb-4 flex-shrink-0 flex items-center justify-between gap-4">
          <div 
            className="inline-flex items-center gap-1 rounded-xl px-2 py-1" 
            role="tablist" 
            aria-label="Lens view mode"
            style={{
              backgroundColor:
                theme === 'light'
                  ? 'rgba(0, 0, 0, 0.02)'
                  : 'rgba(255, 255, 255, 0.04)',
            }}
          >
            {lens && (
              <>
                <button
                  type="button"
                  role="tab"
                  aria-selected={viewMode === 'details'}
                  aria-controls="lens-details-panel"
                  onClick={() => setViewMode('details')}
                  className="px-2 py-1 text-12 rounded"
                  style={{
                    background:
                      viewMode === 'details'
                        ? 'var(--white-10)'
                        : 'transparent',
                    color: 'var(--text-heading)',
                  }}
                >
                  Details
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={viewMode === 'cockpit'}
                  aria-controls="lens-cockpit-panel"
                  onClick={() => setViewMode('cockpit')}
                  className="px-2 py-1 text-12 rounded"
                  style={{
                    background:
                      viewMode === 'cockpit'
                        ? 'var(--white-10)'
                        : 'transparent',
                    color: 'var(--text-heading)',
                  }}
                >
                  Cockpit
                </button>
              </>
            )}
            <button
              type="button"
              role="tab"
              aria-selected={viewMode === 'progress'}
              aria-controls="lens-progress-panel"
              onClick={() => setViewMode('progress')}
              className="px-2 py-1 text-12 rounded"
              style={{
                background:
                  viewMode === 'progress'
                    ? 'var(--white-10)'
                    : 'transparent',
                color: 'var(--text-heading)',
              }}
            >
              Progress
            </button>
          </div>
          
          {/* Toolbar Slot (Portal Target) */}
          <div ref={setToolbarContainer} className="flex items-center gap-1" />
        </div>

        <div 
          className={(viewMode === 'cockpit' || viewMode === 'progress') ? '' : 'space-y-6 py-2'} 
          style={{ 
            display: 'flex', 
            flexDirection: 'column', 
            flex: (viewMode === 'cockpit' || viewMode === 'progress') ? 1 : 'none',
            minHeight: (viewMode === 'cockpit' || viewMode === 'progress') ? 0 : 'auto',
            overflow: (viewMode === 'cockpit' || viewMode === 'progress') ? 'hidden' : 'visible',
          }}
        >
          {loading ? (
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
          ) : error ? (
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
              <div style={{ 
                flex: 1, 
                minHeight: 0, 
                display: 'flex', 
                flexDirection: 'row',
                gap: '16px',
                width: '100%', 
                height: '100%' 
              }}>
                {/* Graph area - flexible width */}
                <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
                  <LensCanvasCockpit 
                    lensId={lensId} 
                    projectId={lens.projectId}
                    toolbarContainer={toolbarContainer}
                    onScopeScout={handleScopeScout}
                    onNodeSelect={(nodeId, nodeData) => {
                      if (import.meta.env.DEV) console.log('[LensCanvas] onNodeSelect', { nodeId, hasNodeData: !!nodeData });
                      setSelectedNodeId(nodeId);
                      setSelectedNodeData(nodeData);
                      // Clear edge selection when node is selected
                      setSelectedEdgeId(null);
                      setSelectedEdgeData(null);
                      // Auto-open inspector when selecting something (most users expect this)
                      if (nodeId) setPanelType('object-inspector');
                    }}
                    onEdgeSelect={(edgeId, edgeData) => {
                      if (import.meta.env.DEV) console.log('[LensCanvas] onEdgeSelect', { edgeId, hasEdgeData: !!edgeData, edgeData });
                      setSelectedEdgeId(edgeId);
                      setSelectedEdgeData(edgeData);
                      // Clear node selection when edge is selected
                      setSelectedNodeId(null);
                      setSelectedNodeData(null);
                      // Auto-open inspector when selecting something (most users expect this)
                      if (edgeId) setPanelType('object-inspector');
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

                {/* Right buttons column - always visible */}
                <HorizonPanelButtons
                  panelType={panelType}
                  onPanelTypeChange={setPanelType}
                  selectedNodeId={selectedNodeId}
                  selectedEdgeId={selectedEdgeId}
                />
              </div>
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

