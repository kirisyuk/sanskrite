import React from 'react';
import { useDiscoveryStore } from '../stores/discoveryStore';

type Kind = 'entities' | 'facts' | 'events' | 'metrics' | 'decisions';

const kinds: Kind[] = ['entities', 'facts', 'events', 'metrics', 'decisions'];

function useCountAndStatus(kind: Kind) {
  const last = useDiscoveryStore((s: any) => s.lastAnalysisStatus);
  const [expanded, setExpanded] = React.useState<boolean>(false);
  const status = (last && String(last.kind).toLowerCase() === kind) ? String(last.status || '') : '';
  const countStatus: number | undefined = (last && String(last.kind).toLowerCase() === kind && typeof last.count === 'number') ? last.count : undefined;
  const elapsedMs: number | undefined = (last && String(last.kind).toLowerCase() === kind && typeof last.elapsedMs === 'number') ? last.elapsedMs : undefined;
  return { status, countStatus, elapsedMs, expanded, setExpanded } as const;
}

function useKindItems(projectId: string, kind: Kind, expanded: boolean) {
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [items, setItems] = React.useState<any[]>([]);

  const load = React.useCallback(async () => {
    if (!expanded) return;
    if (!projectId || projectId === 'local-unfold') { setItems([]); return; }
    setLoading(true); setError(null);
    try {
      const { api } = await import('../lib/api');
      const docs = await api.docsList(projectId);
      const filename = `${kind}.json`;
      const doc = docs.find((d: any) => String(d.filename || '').toLowerCase() === filename);
      if (!doc?.id) { setItems([]); return; }
      const blob = await api.docsDownload(projectId, doc.id);
      const text = await new Response(blob).text();
      const json = JSON.parse(text || '{}');
      const arr = Array.isArray((json as any)[kind]) ? (json as any)[kind] : [];
      setItems(arr);
    } catch (e: any) {
      setError(`Failed to load ${kind}`);
      setItems([]);
    } finally { setLoading(false); }
  }, [projectId, kind, expanded]);

  React.useEffect(() => { load(); }, [load]);

  return { loading, error, items } as const;
}

const SectionHeader: React.FC<{ title: string; badge?: number; badgeLoading?: boolean; status?: string; onRefresh?: () => void; expanded: boolean; onToggle: () => void }>
  = ({ title, badge, badgeLoading, status, onRefresh, expanded, onToggle }) => {
  return (
    <div className="flex items-center gap-2 px-2 py-1 rounded" style={{ background: 'var(--bg-sidebar)', border: '1px solid var(--overlay-line-90)' }}>
      <button type="button" onClick={onToggle} className="rounded flex items-center" aria-label={expanded ? 'Collapse' : 'Expand'}
        style={{ background: 'transparent', color: 'var(--text-heading)' }}>
        <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor">
          <path d={expanded ? 'M6 15l6-6 6 6' : 'M6 9l6 6 6-6'} strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      <div className="text-13" style={{ color: 'var(--text-heading)', fontWeight: 600 }}>{title}</div>
      {badgeLoading ? (
        <span className="text-[10px] px-1.5 py-0.5 rounded" style={{ background: 'var(--brand-10)', color: 'var(--blue-400)' }}>⏳</span>
      ) : (
        typeof badge === 'number' && (
          <span className="text-[10px] px-1.5 py-0.5 rounded" style={{ background: 'var(--brand-10)', color: 'var(--blue-400)' }}>{badge}</span>
        )
      )}
      <div style={{ flex: 1 }} />
      {status && (
        <span className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>{status}</span>
      )}
      {onRefresh && (
        <button type="button" onClick={onRefresh} className="rounded px-2 py-1 text-[11px]"
          style={{ border: '1px solid var(--overlay-line-90)', background: 'transparent', color: 'var(--text-heading)' }}>
          Refresh
        </button>
      )}
    </div>
  );
};

const FindingsPanel: React.FC<{ projectId: string }> = ({ projectId }) => {
  const apiRef = React.useRef<typeof import('../lib/api') | null>(null);
  React.useEffect(() => { import('../lib/api').then((m) => { apiRef.current = m; }); }, []);
  const last = useDiscoveryStore((s: any) => s.lastAnalysisStatus);

  const [countMap, setCountMap] = React.useState<Partial<Record<Kind, number>>>({});
  const [loadingCounts, setLoadingCounts] = React.useState<Partial<Record<Kind, boolean>>>({});
  const isLoadingAny = React.useMemo(() => Object.values(loadingCounts).some(Boolean), [loadingCounts]);

  const loadCount = React.useCallback(async (kind: Kind) => {
    if (!projectId || projectId === 'local-unfold') { setCountMap((m) => ({ ...m, [kind]: 0 })); return; }
    try {
      setLoadingCounts((m) => ({ ...m, [kind]: true }));
      const api = (apiRef.current as any)?.api || (await import('../lib/api')).api;
      const docs = await api.docsList(projectId);
      const filename = `${kind}.json`;
      const doc = docs.find((d: any) => String(d.filename || '').toLowerCase() === filename);
      if (!doc?.id) { setCountMap((m) => ({ ...m, [kind]: 0 })); return; }
      const blob = await api.docsDownload(projectId, doc.id);
      const text = await new Response(blob).text();
      const json = JSON.parse(text || '{}');
      const arr = Array.isArray((json as any)[kind]) ? (json as any)[kind] : [];
      setCountMap((m) => ({ ...m, [kind]: arr.length }));
    } catch {
      setCountMap((m) => ({ ...m, [kind]: 0 }));
    } finally {
      setLoadingCounts((m) => ({ ...m, [kind]: false }));
    }
  }, [projectId]);

  // When panel opens (mount) or project changes – preload counts for all kinds
  React.useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        await Promise.all(kinds.map((k) => cancelled ? Promise.resolve() : loadCount(k)));
      } catch {}
    })();
    return () => { cancelled = true; };
  }, [projectId, loadCount]);

  // When a kind finishes analysis, refresh its count
  React.useEffect(() => {
    if (!last || !projectId) return;
    try {
      const k = String(last.kind || '').toLowerCase() as Kind;
      const st = String(last.status || '').toLowerCase();
      if ((kinds as string[]).includes(k) && (st === 'done' || st === 'failed')) {
        loadCount(k as Kind);
      }
    } catch {}
  }, [last, projectId, loadCount]);

  const refresh = React.useCallback(async (kind: Kind) => {
    try {
      const api = (apiRef.current as any)?.api;
      if (!api || !projectId) return;
      switch (kind) {
        case 'entities': await api.analyzeEntities(projectId, ''); break;
        case 'facts': await api.analyzeFacts(projectId, ''); break;
        case 'events': await api.analyzeEvents(projectId, ''); break;
        case 'metrics': await api.analyzeMetrics(projectId, ''); break;
        case 'decisions': await api.analyzeDecisions(projectId, ''); break;
      }
    } catch {}
  }, [projectId]);

  const refreshAll = React.useCallback(async () => {
    for (const k of kinds) { await refresh(k); }
  }, [refresh]);

  const fmtStatus = (kind: Kind, status: string, count?: number, elapsedMs?: number) => {
    const t = (ms?: number) => (typeof ms === 'number' ? (ms < 1000 ? `${Math.round(ms)}ms` : `${(ms/1000).toFixed(1)}s`) : '');
    const plural = (n: number, one: string, many: string) => (n === 1 ? one : many);
    const s = status.toLowerCase();
    if (s === 'started') return `Analyzing ${kind}…`;
    if (s === 'done') {
      const n = typeof count === 'number' ? count : 0;
      const tt = t(elapsedMs);
      const labelFor = (baseKind: Kind) => {
        const unitOne = baseKind === 'entities' ? 'entity'
          : baseKind === 'facts' ? 'fact'
          : baseKind === 'events' ? 'event'
          : baseKind === 'metrics' ? 'metric'
          : 'decision';
        const unitMany = baseKind === 'entities' ? 'entities'
          : baseKind === 'facts' ? 'facts'
          : baseKind === 'events' ? 'events'
          : baseKind === 'metrics' ? 'metrics'
          : 'decisions';
        if (n === 0) return `found no ${unitMany}`;
        if (n === 1) return `found 1 ${unitOne}`;
        return `found ${n} ${unitMany}`;
      };
      return `${labelFor(kind)}${tt ? ` · ${tt}` : ''}`;
    }
    if (s === 'failed' || s === 'error') return `${kind} failed`;
    return s;
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between px-2">
        <div className="flex items-center gap-2">
          <div className="text-13" style={{ color: 'var(--text-heading)', fontWeight: 600 }}>Findings</div>
          {isLoadingAny && (
            <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>Refreshing…</div>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button type="button" className="rounded px-2 py-1 text-[11px]" onClick={refreshAll}
            style={{ border: '1px solid var(--overlay-line-90)', background: 'transparent', color: 'var(--text-heading)' }}>Refresh all</button>
        </div>
      </div>
      {kinds.map((k) => {
        const { status, countStatus, elapsedMs, expanded, setExpanded } = useCountAndStatus(k);
        const { loading, error, items } = useKindItems(projectId, k, expanded);
        const preloaded = countMap[k];
        const count = typeof preloaded === 'number' ? preloaded : (typeof countStatus === 'number' ? countStatus : (Array.isArray(items) ? items.length : undefined));
        return (
          <div key={k} className="rounded">
            <SectionHeader
              title={k.charAt(0).toUpperCase() + k.slice(1)}
              badge={typeof count === 'number' ? count : undefined}
              badgeLoading={!!loadingCounts[k] && typeof count !== 'number'}
              status={status ? fmtStatus(k, status, count, elapsedMs) : ''}
              onRefresh={() => refresh(k)}
              expanded={expanded}
              onToggle={() => setExpanded((v) => !v)}
            />
            {expanded && (
              <div className="px-2 py-2 text-[12px]" style={{ color: 'var(--text-secondary-strong)' }}>
                {loading && <div>Loading…</div>}
                {error && <div style={{ color: 'var(--accent-danger)' }}>{error}</div>}
                {!loading && !error && (
                  <div className="space-y-1">
                    {Array.isArray(items) && items.length === 0 && (
                      <div className="text-[12px]">No {k} yet. Run "analyze {k}".</div>
                    )}
                    {Array.isArray(items) && items.length > 0 && (
                      <div className="grid" style={{ gridTemplateColumns: '1fr', gap: 6 }}>
                        {items.map((it, i) => {
                          switch (k) {
                            case 'entities': {
                              return (
                                <div key={i} className="p-2 rounded" style={{ background: 'var(--bg-sidebar)', border: '1px solid var(--overlay-line-90)' }}>
                                  <div className="flex items-center justify-between gap-2">
                                    <div className="text-12 font-medium" style={{ color: 'var(--text-heading)' }}>{it.name}</div>
                                    <span className="text-[10px] px-2 py-0.5 rounded" style={{ background: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}>{it.type}</span>
                                  </div>
                                  {it.description && <div className="mt-1 text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>{it.description}</div>}
                                </div>
                              );
                            }
                            case 'facts': {
                              return (
                                <div key={i} className="p-2 rounded" style={{ background: 'var(--bg-sidebar)', border: '1px solid var(--overlay-line-90)' }}>
                                  <div className="flex items-center justify-between gap-2">
                                    <div className="text-12" style={{ color: 'var(--text-heading)' }}>{it.statement}</div>
                                    {typeof it.confidence === 'number' && (
                                      <span className="text-[10px] px-2 py-0.5 rounded" style={{ background: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}>{Math.round(Math.max(0, Math.min(1, it.confidence))*100)}%</span>
                                    )}
                                  </div>
                                  {Array.isArray(it.evidence) && it.evidence.length > 0 && (
                                    <div className="mt-1 text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>Evidence: {it.evidence.join('; ')}</div>
                                  )}
                                </div>
                              );
                            }
                            case 'events': {
                              return (
                                <div key={i} className="p-2 rounded" style={{ background: 'var(--bg-sidebar)', border: '1px solid var(--overlay-line-90)' }}>
                                  <div className="text-12 font-medium" style={{ color: 'var(--text-heading)' }}>{it.title}</div>
                                  <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>{it.ts}</div>
                                  {it.summary && <div className="mt-1 text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>{it.summary}</div>}
                                </div>
                              );
                            }
                            case 'metrics': {
                              return (
                                <div key={i} className="p-2 rounded" style={{ background: 'var(--bg-sidebar)', border: '1px solid var(--overlay-line-90)' }}>
                                  <div className="flex items-center justify-between gap-2">
                                    <div className="text-12 font-medium" style={{ color: 'var(--text-heading)' }}>{it.label || it.key}</div>
                                    <div className="text-12" style={{ color: 'var(--text-secondary-strong)' }}>{String(it.value)}{it.unit ? ` ${it.unit}` : ''}</div>
                                  </div>
                                </div>
                              );
                            }
                            case 'decisions': {
                              return (
                                <div key={i} className="p-2 rounded" style={{ background: 'var(--bg-sidebar)', border: '1px solid var(--overlay-line-90)' }}>
                                  <div className="flex items-center justify-between gap-2">
                                    <div className="text-12 font-medium" style={{ color: 'var(--text-heading)' }}>{it.title}</div>
                                    {it.selected && (
                                      <span className="text-[10px] px-2 py-0.5 rounded" style={{ background: 'var(--white-10)', color: 'var(--text-secondary-strong)' }}>{it.selected}</span>
                                    )}
                                  </div>
                                  {it.rationale && <div className="mt-1 text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>{it.rationale}</div>}
                                </div>
                              );
                            }
                          }
                          return null;
                        })}
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
};

export default FindingsPanel;


