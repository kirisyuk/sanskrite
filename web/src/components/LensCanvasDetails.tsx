import React from 'react';

export interface LensCanvasDetailsProps {
  lens: any;
  assemblyStatus: any;
  scoutingJobs: any[];
  loadingJobs: boolean;
  onRefreshJobs: () => void;
  onUpdateScope?: (nextScope: string[]) => Promise<void>;
}

export const LensCanvasDetails: React.FC<LensCanvasDetailsProps> = ({
  lens,
  assemblyStatus,
  scoutingJobs,
  loadingJobs,
  onRefreshJobs,
  onUpdateScope,
}) => {
  const [newScopePath, setNewScopePath] = React.useState('');
  const [savingScope, setSavingScope] = React.useState(false);
  const [scopeError, setScopeError] = React.useState<string | null>(null);

  const scope: string[] = Array.isArray(lens?.scope) ? lens.scope : [];

  const dedupe = (arr: string[]) => {
    const seen = new Set<string>();
    const out: string[] = [];
    for (const v of arr) {
      const s = (v || '').trim();
      if (!s) continue;
      if (seen.has(s)) continue;
      seen.add(s);
      out.push(s);
    }
    return out;
  };

  const isValidScopeEntry = (v: string) => {
    const s = (v || '').trim();
    if (!s) return false;
    // Allow canonical paths and query addresses.
    if (s.startsWith('/query/')) return true;
    return s.startsWith('/');
  };

  const handleAddScope = async () => {
    if (!onUpdateScope) return;
    setScopeError(null);
    const value = newScopePath.trim();
    if (!isValidScopeEntry(value)) {
      setScopeError('Enter a canonical path (starts with "/") or a query address (starts with "/query/").');
      return;
    }
    const next = dedupe([...scope, value]);
    if (next.length === scope.length) {
      setNewScopePath('');
      return;
    }
    setSavingScope(true);
    try {
      await onUpdateScope(next);
      setNewScopePath('');
    } catch (e: any) {
      setScopeError(e?.response?.data?.error || e?.message || 'Failed to update scope');
    } finally {
      setSavingScope(false);
    }
  };

  const handleRemoveScope = async (path: string) => {
    if (!onUpdateScope) return;
    setScopeError(null);
    const p = (path || '').trim();
    if (!p) return;
    if (!window.confirm(`Remove scope entry?\n\n${p}`)) return;
    const next = scope.filter((x) => (x || '').trim() !== p);
    if (next.length === 0) {
      setScopeError('Scope must contain at least one entry.');
      return;
    }
    setSavingScope(true);
    try {
      await onUpdateScope(next);
    } catch (e: any) {
      setScopeError(e?.response?.data?.error || e?.message || 'Failed to update scope');
    } finally {
      setSavingScope(false);
    }
  };

  return (
    <div className="space-y-4">
      {/* Lens Info Section */}
      <div>
        <h3 className="text-sm font-medium mb-3" style={{ color: 'var(--text-heading)' }}>
          Information
        </h3>
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <span className="text-xs" style={{ color: 'var(--text-secondary-strong)', minWidth: '100px' }}>
              Status:
            </span>
            <span
              className="text-xs px-2 py-1 rounded"
              style={{
                backgroundColor: lens.status === 'ACTIVE' ? 'var(--success-20)' : 'var(--white-10)',
                color: lens.status === 'ACTIVE' ? 'var(--accent-success)' : 'var(--text-secondary-strong)',
              }}
            >
              {lens.status}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs" style={{ color: 'var(--text-secondary-strong)', minWidth: '100px' }}>
              Created:
            </span>
            <span className="text-xs" style={{ color: 'var(--text-heading)' }}>
              {new Date(lens.createdAt).toLocaleDateString()}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs" style={{ color: 'var(--text-secondary-strong)', minWidth: '100px' }}>
              Updated:
            </span>
            <span className="text-xs" style={{ color: 'var(--text-heading)' }}>
              {new Date(lens.updatedAt).toLocaleDateString()}
            </span>
          </div>
        </div>
      </div>

      {/* Scope Section */}
      <div>
        <h3 className="text-sm font-medium mb-3" style={{ color: 'var(--text-heading)' }}>
          Scope ({scope.length || 0})
        </h3>

        {/* Edit controls */}
        <div className="flex items-center gap-2 mb-2">
          <input
            value={newScopePath}
            onChange={(e) => setNewScopePath(e.target.value)}
            placeholder='Add path (e.g., "/loc/ae/na/na" or "/query/sector:/Technology/**")'
            className="text-xs px-2 py-1.5 rounded flex-1"
            style={{
              backgroundColor: 'var(--white-05)',
              color: 'var(--text-heading)',
              border: '1px solid var(--white-10)',
              fontFamily: 'monospace',
              opacity: savingScope ? 0.7 : 1,
            }}
            disabled={!onUpdateScope || savingScope}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                handleAddScope();
              }
            }}
          />
          <button
            onClick={handleAddScope}
            disabled={!onUpdateScope || savingScope || !newScopePath.trim()}
            className="text-xs px-2 py-1.5 rounded transition-colors"
            style={{
              backgroundColor: 'var(--white-10)',
              color: 'var(--text-heading)',
              opacity: !onUpdateScope || savingScope || !newScopePath.trim() ? 0.5 : 1,
            }}
          >
            {savingScope ? 'Saving…' : 'Add'}
          </button>
        </div>
        {scopeError && (
          <div className="text-xs mb-2" style={{ color: '#f87171' }}>
            {scopeError}
          </div>
        )}

        {scope.length > 0 ? (
          <div className="space-y-1">
            {scope.map((path: string, index: number) => (
              <div
                key={`${path}-${index}`}
                className="px-2 py-1.5 rounded text-xs flex items-center justify-between gap-2"
                style={{
                  backgroundColor: 'var(--white-05)',
                }}
              >
                <span
                  className="truncate"
                  style={{
                    color: 'var(--text-heading)',
                    fontFamily: 'monospace',
                  }}
                  title={path}
                >
                  {path}
                </span>
                <button
                  onClick={() => handleRemoveScope(path)}
                  disabled={!onUpdateScope || savingScope}
                  className="text-[10px] px-2 py-1 rounded"
                  style={{
                    backgroundColor: 'var(--white-10)',
                    color: 'var(--text-secondary-strong)',
                    opacity: !onUpdateScope || savingScope ? 0.5 : 1,
                  }}
                  title="Remove"
                >
                  Remove
                </button>
              </div>
            ))}
          </div>
        ) : (
          <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
            No scope defined
          </div>
        )}
      </div>

      {/* View Config Section */}
      {lens.viewConfig && Object.keys(lens.viewConfig).length > 0 && (
        <div>
          <h3 className="text-sm font-medium mb-3" style={{ color: 'var(--text-heading)' }}>
            View Configuration
          </h3>
          <div className="px-3 py-2 rounded" style={{ backgroundColor: 'var(--white-05)' }}>
            <pre className="text-xs" style={{ color: 'var(--text-heading)', overflow: 'auto' }}>
              {JSON.stringify(lens.viewConfig, null, 2)}
            </pre>
          </div>
        </div>
      )}

      {/* Alert Config Section */}
      {lens.alertConfig && Object.keys(lens.alertConfig).length > 0 && (
        <div>
          <h3 className="text-sm font-medium mb-3" style={{ color: 'var(--text-heading)' }}>
            Alert Configuration
          </h3>
          <div className="px-3 py-2 rounded" style={{ backgroundColor: 'var(--white-05)' }}>
            <pre className="text-xs" style={{ color: 'var(--text-heading)', overflow: 'auto' }}>
              {JSON.stringify(lens.alertConfig, null, 2)}
            </pre>
          </div>
        </div>
      )}

      {/* Assembly Status Section */}
      {assemblyStatus && (
        <div>
          <h3 className="text-sm font-medium mb-3" style={{ color: 'var(--text-heading)' }}>
            Assembly Status
          </h3>
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <span className="text-xs" style={{ color: 'var(--text-secondary-strong)', minWidth: '100px' }}>
                Status:
              </span>
              <span
                className="text-xs px-2 py-1 rounded"
                style={{
                  backgroundColor: assemblyStatus.status === 'ready' ? 'var(--success-20)' : 'var(--white-10)',
                  color: assemblyStatus.status === 'ready' ? 'var(--accent-success)' : 'var(--text-secondary-strong)',
                }}
              >
                {assemblyStatus.status}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-xs" style={{ color: 'var(--text-secondary-strong)', minWidth: '100px' }}>
                Phase:
              </span>
              <span className="text-xs" style={{ color: 'var(--text-heading)' }}>
                {assemblyStatus.phase}
              </span>
            </div>
            {assemblyStatus.progress !== undefined && (
              <div className="flex items-center gap-2">
                <span className="text-xs" style={{ color: 'var(--text-secondary-strong)', minWidth: '100px' }}>
                  Progress:
                </span>
                <div className="flex-1 max-w-xs">
                  <div className="h-2 rounded-full overflow-hidden" style={{ backgroundColor: 'var(--white-10)' }}>
                    <div
                      className="h-full transition-all"
                      style={{
                        width: `${assemblyStatus.progress * 100}%`,
                        backgroundColor: 'var(--accent)',
                      }}
                    />
                  </div>
                  <span className="text-xs mt-1" style={{ color: 'var(--text-secondary-strong)' }}>
                    {Math.round(assemblyStatus.progress * 100)}%
                  </span>
                </div>
              </div>
            )}
            {assemblyStatus.phases?.scouting && (
              <div className="mt-2 px-2 py-1.5 rounded text-xs" style={{ backgroundColor: 'var(--white-05)' }}>
                <div className="flex items-center justify-between">
                  <span style={{ color: 'var(--text-secondary-strong)' }}>Scouting:</span>
                  <span style={{ color: 'var(--text-heading)' }}>
                    {assemblyStatus.phases.scouting.completed} / {assemblyStatus.phases.scouting.total}
                  </span>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Scouting Jobs Section */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-medium" style={{ color: 'var(--text-heading)' }}>
            Scouting Jobs ({scoutingJobs.length})
          </h3>
          <button
            onClick={onRefreshJobs}
            disabled={loadingJobs}
            className="text-xs px-2 py-1 rounded transition-colors"
            style={{
              backgroundColor: 'var(--white-10)',
              color: 'var(--text-heading)',
              opacity: loadingJobs ? 0.5 : 1,
            }}
            onMouseEnter={(e) => {
              if (!loadingJobs) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-20)';
              }
            }}
            onMouseLeave={(e) => {
              if (!loadingJobs) {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
              }
            }}
          >
            {loadingJobs ? 'Loading...' : 'Refresh'}
          </button>
        </div>
        {scoutingJobs.length > 0 ? (
          <div className="space-y-1">
            {scoutingJobs.map((job: any) => {
              const getStatusColor = (status: string) => {
                if (status === 'VERIFIED' || status === 'COMPLETED') {
                  return { bg: 'var(--success-20)', color: 'var(--accent-success)' };
                } else if (status === 'FAILED' || status === 'ERROR') {
                  return { bg: 'rgba(248, 113, 113, 0.2)', color: '#f87171' };
                } else if (status === 'PENDING' || status === 'RUNNING') {
                  return { bg: 'var(--white-10)', color: 'var(--text-secondary-strong)' };
                }
                return { bg: 'var(--white-05)', color: 'var(--text-secondary-strong)' };
              };
              const statusStyle = getStatusColor(job.status);
              return (
                <div
                  key={job.id}
                  className="px-2 py-1.5 rounded text-xs"
                  style={{
                    backgroundColor: 'var(--white-05)',
                  }}
                >
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2 flex-1 min-w-0">
                      <span
                        className="px-1.5 py-0.5 rounded text-[10px] font-medium flex-shrink-0"
                        style={{
                          backgroundColor: statusStyle.bg,
                          color: statusStyle.color,
                        }}
                      >
                        {job.status}
                      </span>
                      <span
                        className="truncate"
                        style={{
                          color: 'var(--text-heading)',
                          fontFamily: 'monospace',
                        }}
                        title={job.targetCanonicalPath}
                      >
                        {job.targetCanonicalPath}
                      </span>
                    </div>
                    <span className="text-[10px] flex-shrink-0" style={{ color: 'var(--text-tertiary-weak)' }}>
                      {new Date(job.createdAt).toLocaleDateString()}
                    </span>
                  </div>
                </div>
              );
            })}
          </div>
        ) : (
          <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
            No scouting jobs found
          </div>
        )}
      </div>
    </div>
  );
};


