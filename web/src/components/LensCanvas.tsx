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
import { LensCanvasList } from './LensCanvasList';

export interface LensCanvasProps {
  isOpen: boolean;
  lensId: string | null;
  onClose: () => void;
}

export const LensCanvas: React.FC<LensCanvasProps> = ({
  isOpen,
  lensId,
  onClose,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const [lens, setLens] = React.useState<any>(null);
  const [assemblyStatus, setAssemblyStatus] = React.useState<any>(null);
  const [scoutingJobs, setScoutingJobs] = React.useState<any[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [loadingJobs, setLoadingJobs] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [viewMode, setViewMode] = React.useState<'details' | 'cockpit' | 'list'>('details');

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

  if (!isOpen) return null;

  return (
    <ModalWithHeader
      title={lens ? lens.name : 'Lens'}
      onClose={onClose}
    >
      {/* View mode switcher */}
      <div className="mb-4">
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
          <button
            type="button"
            role="tab"
            aria-selected={viewMode === 'list'}
            aria-controls="lens-list-panel"
            onClick={() => setViewMode('list')}
            className="px-2 py-1 text-12 rounded"
            style={{
              background:
                viewMode === 'list'
                  ? 'var(--white-10)'
                  : 'transparent',
              color: 'var(--text-heading)',
            }}
          >
            List
          </button>
        </div>
      </div>

      <div 
        className={viewMode === 'cockpit' ? '' : 'space-y-6 py-2'} 
        style={{ 
          display: 'flex', 
          flexDirection: 'column', 
          height: viewMode === 'cockpit' ? '100%' : 'auto',
          minHeight: viewMode === 'cockpit' ? '600px' : 'auto',
          overflow: viewMode === 'cockpit' ? 'hidden' : 'visible',
        }}
      >
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <div className="animate-pulse text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
              Loading lens...
            </div>
          </div>
        ) : error ? (
          <div className="flex items-center justify-center py-8">
            <div className="text-xs text-center" style={{ color: '#f87171' }}>
              {error}
            </div>
          </div>
        ) : lens ? (
          <>
            {viewMode === 'details' && (
              <LensCanvasDetails
                lens={lens}
                assemblyStatus={assemblyStatus}
                scoutingJobs={scoutingJobs}
                loadingJobs={loadingJobs}
                onRefreshJobs={loadScoutingJobs}
              />
            )}
            {viewMode === 'cockpit' && (
              <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', width: '100%', height: '100%' }}>
                <LensCanvasCockpit lensId={lensId} projectId={lens.projectId} />
              </div>
            )}
            {viewMode === 'list' && (
              <LensCanvasList lensId={lensId} projectId={lens.projectId} />
            )}
          </>
        ) : null}
      </div>
    </ModalWithHeader>
  );
};

