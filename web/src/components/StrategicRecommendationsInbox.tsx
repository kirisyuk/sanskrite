import React from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../lib/api';
import { useIsAdmin } from '../hooks/useIsAdmin';
import { useProjectsStore } from '../stores/projectsStore';
import { useStrategicRecommendationsStore } from '../stores/strategicRecommendationsStore';
import { useUIStore } from '../stores/uiStore';

const statusOptions = ['pending', 'executing', 'completed', 'dismissed'];
const scopeOptions = ['all', 'tenant', 'global'] as const;

export const StrategicRecommendationsInbox: React.FC = () => {
  const navigate = useNavigate();
  const isAdmin = useIsAdmin();
  const open = useUIStore((s) => s.showStrategicInbox);
  const setOpen = useUIStore((s) => s.setShowStrategicInbox);
  const items = useStrategicRecommendationsStore((s) => s.items);
  const loading = useStrategicRecommendationsStore((s) => s.loading);
  const activeScope = useStrategicRecommendationsStore((s) => s.activeScope);
  const tenantPendingCount = useStrategicRecommendationsStore((s) => s.tenantPendingCount);
  const globalPendingCount = useStrategicRecommendationsStore((s) => s.globalPendingCount);
  const fetchList = useStrategicRecommendationsStore((s) => s.fetchList);
  const fetchCount = useStrategicRecommendationsStore((s) => s.fetchCount);
  const approve = useStrategicRecommendationsStore((s) => s.approve);
  const dismiss = useStrategicRecommendationsStore((s) => s.dismiss);
  const projects = useProjectsStore((s) => s.projects);
  const setCurrentProject = useProjectsStore((s) => s.setCurrentProject);
  const [status, setStatus] = React.useState('pending');
  const [scope, setScope] = React.useState<'tenant' | 'global' | 'all'>('tenant');
  const [busyId, setBusyId] = React.useState<string | null>(null);

  React.useEffect(() => {
    setScope(isAdmin ? 'all' : 'tenant');
  }, [isAdmin]);

  React.useEffect(() => {
    if (!open) return;
    const requestScope = isAdmin ? scope : 'tenant';
    void fetchList({ status, limit: 50, scope: requestScope });
    void fetchCount({ scope: requestScope });
  }, [open, status, scope, isAdmin, fetchList, fetchCount]);

  const openSubject = React.useCallback(async (item: any) => {
    const projectId = String(item?.projectId || '').trim();
    const lensId = String(item?.lensId || '').trim();
    if (projectId) {
      const found = projects.find((project) => project.id === projectId);
      if (found) {
        setCurrentProject(found);
      } else {
        try {
          const project = await api.getProject(projectId);
          setCurrentProject(project as any);
        } catch {
          // keep navigation even if project hydration fails
        }
      }
    }
    try {
      if (projectId) localStorage.setItem('horizon.selectedProjectId', projectId);
      if (lensId) localStorage.setItem('horizon.selectedLensId', lensId);
    } catch {}
    window.dispatchEvent(new CustomEvent('horizon:open-context', { detail: { projectId, lensId, subjectCanonicalPath: item?.subjectCanonicalPath } }));
    setOpen(false);
    navigate('/horizon');
  }, [navigate, projects, setCurrentProject, setOpen]);

  const onApprove = React.useCallback(async (id: string) => {
    setBusyId(id);
    try {
      await approve(id);
      await fetchCount({ scope: isAdmin ? scope : 'tenant' });
    } finally {
      setBusyId(null);
    }
  }, [approve, fetchCount, isAdmin, scope]);

  const onDismiss = React.useCallback(async (id: string) => {
    setBusyId(id);
    try {
      await dismiss(id);
      await fetchCount({ scope: isAdmin ? scope : 'tenant' });
    } finally {
      setBusyId(null);
    }
  }, [dismiss, fetchCount, isAdmin, scope]);

  if (!open) return null;

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.35)',
        zIndex: 100,
        display: 'flex',
        justifyContent: 'flex-end',
      }}
      onClick={() => setOpen(false)}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 'min(520px, 96vw)',
          height: '100%',
          background: 'var(--bg-elevated)',
          borderLeft: '1px solid var(--overlay-line-90)',
          padding: 20,
          overflowY: 'auto',
          color: 'var(--text-heading)',
        }}
      >
        <div className="flex items-center justify-between gap-3 mb-4">
          <div>
            <div className="text-[18px] font-semibold">Strategic Inbox</div>
            <div className="text-[12px]" style={{ color: 'var(--text-tertiary-weak)' }}>
              Strategic downstream gaps that need approval-driven enrichment.
            </div>
            {isAdmin && (
              <div className="mt-1 text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                Tenant {tenantPendingCount} • Global {globalPendingCount} • View {activeScope}
              </div>
            )}
          </div>
          <button
            type="button"
            onClick={() => setOpen(false)}
            className="rounded px-2 py-1 text-12"
            style={{ border: '1px solid var(--overlay-line-90)', color: 'var(--text-secondary-strong)' }}
          >
            Close
          </button>
        </div>

        <div className="flex items-center gap-2 mb-4">
          {statusOptions.map((option) => (
            <button
              key={option}
              type="button"
              onClick={() => setStatus(option)}
              className="rounded px-2 py-1 text-12"
              style={{
                border: '1px solid var(--overlay-line-90)',
                background: status === option ? 'var(--white-10)' : 'transparent',
                color: 'var(--text-secondary-strong)',
              }}
            >
              {option}
            </button>
          ))}
        </div>

        {isAdmin && (
          <div className="flex items-center gap-2 mb-4">
            {scopeOptions.map((option) => (
              <button
                key={option}
                type="button"
                onClick={() => setScope(option)}
                className="rounded px-2 py-1 text-12"
                style={{
                  border: '1px solid var(--overlay-line-90)',
                  background: scope === option ? 'var(--white-10)' : 'transparent',
                  color: 'var(--text-secondary-strong)',
                }}
              >
                {option}
              </button>
            ))}
          </div>
        )}

        {loading && <div className="text-12">Loading recommendations…</div>}
        {!loading && items.length === 0 && (
          <div className="text-12" style={{ color: 'var(--text-tertiary-weak)' }}>
            No recommendations in this view.
          </div>
        )}

        <div className="flex flex-col gap-3">
          {items.map((item) => {
            const currentCoverage = item.currentCoverageStats || {};
            const pending = busyId === item.id;
            return (
              <div
                key={item.id}
                className="rounded-xl p-3"
                style={{ border: '1px solid var(--overlay-line-90)', background: 'var(--panel-bg)' }}
              >
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="text-[11px] uppercase tracking-[0.14em]" style={{ color: 'var(--text-tertiary-weak)' }}>
                      {item.priority} • {item.status} • {item.tenantId === 'global' ? 'global' : 'tenant'}
                    </div>
                    <div className="text-[15px] font-medium mt-1">{item.headline}</div>
                    <div className="text-[12px] mt-2" style={{ color: 'var(--text-secondary-strong)' }}>
                      {item.reason}
                    </div>
                  </div>
                  <button
                    type="button"
                    onClick={() => void openSubject(item)}
                    className="rounded px-2 py-1 text-12"
                    style={{ border: '1px solid var(--overlay-line-90)', color: 'var(--text-secondary-strong)' }}
                  >
                    Open
                  </button>
                </div>

                <div className="mt-3 text-[12px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                  Subject: {item.subjectCanonicalPath}
                </div>
                {!!item.expectedMissingTargets?.length && (
                  <div className="mt-2 text-[12px]" style={{ color: 'var(--text-secondary-strong)' }}>
                    Missing: {item.expectedMissingTargets.join(', ')}
                  </div>
                )}
                <div className="mt-2 text-[12px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                  Coverage: {currentCoverage.matchedFamilies || 0}/{currentCoverage.expectedFamilies || 0} families • remote {currentCoverage.remoteMatches || 0}
                </div>
                {!!item.domainContractHint && (
                  <div className="mt-1 text-[12px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                    Domain hint: {item.domainContractHint}
                  </div>
                )}

                {!!item.evidence?.length && (
                  <div className="mt-3">
                    <div className="text-[11px] uppercase tracking-[0.14em]" style={{ color: 'var(--text-tertiary-weak)' }}>
                      Evidence
                    </div>
                    <div className="mt-1 flex flex-col gap-1">
                      {item.evidence.slice(0, 4).map((line) => (
                        <div key={line} className="text-[12px]" style={{ color: 'var(--text-secondary-strong)' }}>
                          {line}
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                <div className="mt-4 flex items-center gap-2">
                  {item.status !== 'dismissed' && item.status !== 'completed' && (
                    <button
                      type="button"
                      disabled={pending}
                      onClick={() => void onApprove(item.id)}
                      className="rounded px-3 py-1.5 text-12"
                      style={{ border: '1px solid var(--overlay-line-90)', color: 'var(--text-heading)', background: 'var(--white-10)' }}
                    >
                      {pending ? 'Running…' : 'Approve'}
                    </button>
                  )}
                  {item.status !== 'dismissed' && (
                    <button
                      type="button"
                      disabled={pending}
                      onClick={() => void onDismiss(item.id)}
                      className="rounded px-3 py-1.5 text-12"
                      style={{ border: '1px solid var(--overlay-line-90)', color: 'var(--text-secondary-strong)' }}
                    >
                      Dismiss
                    </button>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
};
