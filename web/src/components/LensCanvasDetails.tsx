import React from 'react';
import { api } from '../lib/api';
import { humanizeCanonicalPath, preferDisplayLabel } from '../lib/uiUtils';

function pruneMapToMax<K, V>(m: Map<K, V>, maxSize: number) {
  if (maxSize <= 0) {
    m.clear();
    return;
  }
  while (m.size > maxSize) {
    const first = m.keys().next();
    if (first.done) break;
    m.delete(first.value);
  }
}

function boundedMapSet<K, V>(m: Map<K, V>, key: K, value: V, maxSize: number) {
  m.set(key, value);
  pruneMapToMax(m, maxSize);
}

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
  type SearchEntity = {
    id: string;
    canonicalPath: string;
    name: string;
    type: string;
    description?: string;
    location?: string;
    sector?: string;
  };

  const [newScopePath, setNewScopePath] = React.useState('');
  const [savingScope, setSavingScope] = React.useState(false);
  const [scopeError, setScopeError] = React.useState<string | null>(null);
  const scopeInputRef = React.useRef<HTMLInputElement | null>(null);
  const scopeSuggestRef = React.useRef<HTMLDivElement | null>(null);
  const [scopeSuggestOpen, setScopeSuggestOpen] = React.useState(false);
  const [scopeSuggestLoading, setScopeSuggestLoading] = React.useState(false);
  const [scopeSuggestError, setScopeSuggestError] = React.useState<string | null>(null);
  const [scopeSuggestResults, setScopeSuggestResults] = React.useState<SearchEntity[]>([]);
  const [scopeSuggestIndex, setScopeSuggestIndex] = React.useState(0);
  const scopeSuggestAbortRef = React.useRef<AbortController | null>(null);
  const scopeSuggestCacheRef = React.useRef<Map<string, SearchEntity[]>>(new Map());
  const [newsLoading, setNewsLoading] = React.useState(false);
  const [newsError, setNewsError] = React.useState<string | null>(null);

  const scope: string[] = Array.isArray(lens?.scope) ? lens.scope : [];
  const scopeFragments: Record<string, string> = (lens?.scopeFragments && typeof lens.scopeFragments === 'object') ? lens.scopeFragments : {};
  const resolvedQueries: Record<string, string[]> = (lens?.resolvedQueries && typeof lens.resolvedQueries === 'object') ? lens.resolvedQueries : {};
  const intents = Array.isArray(lens?.intents) ? lens.intents : [];
  const domains = Array.isArray(lens?.viewConfig?.domains) ? lens.viewConfig.domains : [];
  const domainModes = Array.isArray(lens?.domainModes) ? lens.domainModes : [];
  const domainModesOnly = domainModes.length > 0 && domains.length === 0;
  const totalObjects = scope.length + intents.length + domains.length + (domainModesOnly ? domainModes.length : 0);

  type ScopeResolved = { name?: string; type?: string; fragmentId?: string; status: 'resolved' | 'error' };
  const [scopeResolved, setScopeResolved] = React.useState<Record<string, ScopeResolved>>({});
  const scopeResolveInFlightRef = React.useRef<Set<string>>(new Set());

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

  const isStaticCanonicalPath = (v: string) => {
    const s = (v || '').trim();
    return s.startsWith('/') && !s.startsWith('/query/');
  };

  const closeScopeSuggest = React.useCallback(() => {
    setScopeSuggestOpen(false);
    setScopeSuggestError(null);
    setScopeSuggestLoading(false);
    setScopeSuggestResults([]);
    setScopeSuggestIndex(0);
    if (scopeSuggestAbortRef.current) {
      try { scopeSuggestAbortRef.current.abort(); } catch { /* ignore */ }
    }
    scopeSuggestAbortRef.current = null;
  }, []);

  // Memory safety: clear per-lens caches when switching lenses / unmounting.
  React.useEffect(() => {
    scopeSuggestCacheRef.current.clear();
    scopeResolveInFlightRef.current.clear();
    setScopeResolved({});
    closeScopeSuggest();
  }, [lens?.id, closeScopeSuggest]);
  React.useEffect(() => {
    return () => {
      try { scopeSuggestAbortRef.current?.abort(); } catch { /* ignore */ }
      scopeSuggestAbortRef.current = null;
      scopeSuggestCacheRef.current.clear();
      scopeResolveInFlightRef.current.clear();
    };
  }, []);

  const pickScopeSuggestion = React.useCallback((r: SearchEntity) => {
    setNewScopePath(r.canonicalPath);
    closeScopeSuggest();
    // Keep focus to allow quick Enter -> Add
    window.setTimeout(() => scopeInputRef.current?.focus(), 0);
  }, [closeScopeSuggest]);

  // Scope input: if user types a name (not starting with "/"), prefetch entities and allow picking a canonical path.
  React.useEffect(() => {
    const q = (newScopePath || '').trim();
    if (!q) {
      closeScopeSuggest();
      return;
    }
    if (q.startsWith('/')) {
      // Canonical paths / query addresses are entered directly.
      closeScopeSuggest();
      return;
    }
    if (q.length < 2) {
      setScopeSuggestOpen(true);
      setScopeSuggestError(null);
      setScopeSuggestLoading(false);
      setScopeSuggestResults([]);
      setScopeSuggestIndex(0);
      return;
    }

    setScopeSuggestOpen(true);
    setScopeSuggestError(null);

    const key = q.toLowerCase();
    const cached = scopeSuggestCacheRef.current.get(key);
    if (cached) {
      setScopeSuggestResults(cached);
      setScopeSuggestLoading(false);
      setScopeSuggestIndex(0);
      return;
    }

    const t = window.setTimeout(async () => {
      if (scopeSuggestAbortRef.current) {
        try { scopeSuggestAbortRef.current.abort(); } catch { /* ignore */ }
      }
      const ac = new AbortController();
      scopeSuggestAbortRef.current = ac;
      setScopeSuggestLoading(true);
      try {
        const res = await api.searchHorizonEntities(q, 8, ac.signal);
        const list = Array.isArray(res?.entities) ? (res.entities as SearchEntity[]) : [];
        boundedMapSet(scopeSuggestCacheRef.current, key, list, 80);
        setScopeSuggestResults(list);
        setScopeSuggestIndex(0);
        setScopeSuggestLoading(false);
      } catch (e: any) {
        if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return;
        setScopeSuggestLoading(false);
        setScopeSuggestError('Search failed');
      }
    }, 180);

    return () => window.clearTimeout(t);
  }, [newScopePath, closeScopeSuggest]);

  // Close suggestions on Esc (do NOT use global mousedown capture here:
  // on some devices it can steal focus while typing).
  React.useEffect(() => {
    if (!scopeSuggestOpen) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        closeScopeSuggest();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [scopeSuggestOpen, closeScopeSuggest]);

  // Progressive enhancement: resolve canonical scope paths -> fragment name/type (cached).
  React.useEffect(() => {
    const lensId = String(lens?.id || '').trim();
    if (!lensId) return;

    const paths = scope.map((s) => (s || '').trim()).filter((p) => isStaticCanonicalPath(p));
    if (paths.length === 0) return;

    let cancelled = false;
    const run = async () => {
      // Resolve in small batches to avoid request storms.
      const unresolved = paths.filter((p) => !scopeResolved[p] && !scopeResolveInFlightRef.current.has(p));
      if (unresolved.length === 0) return;

      const batchSize = 4;
      for (let i = 0; i < unresolved.length; i += batchSize) {
        const batch = unresolved.slice(i, i + batchSize);
        batch.forEach((p) => scopeResolveInFlightRef.current.add(p));
        try {
          const results = await Promise.all(
            batch.map(async (path) => {
              try {
                const cachedId = scopeFragments?.[path];
                const frag = cachedId ? await api.getHorizonFragment(cachedId) : await api.getHorizonFragmentByPath(path);
                const rawName = frag?.name || frag?.displayLabel || frag?.label || undefined;
                const preferred = preferDisplayLabel(rawName, frag?.canonicalPath, frag?.type);
                const name = preferred || humanizeCanonicalPath(frag?.canonicalPath) || rawName || undefined;
                const type = frag?.type || frag?.displayType || undefined;
                const fragmentId = frag?.id || cachedId || undefined;
                return { path, data: { name, type, fragmentId, status: 'resolved' as const } };
              } catch {
                return { path, data: { status: 'error' as const } };
              }
            })
          );
          if (cancelled) return;
          setScopeResolved((prev) => {
            const next = { ...prev };
            for (const r of results) {
              next[r.path] = r.data;
            }
            return next;
          });
        } finally {
          batch.forEach((p) => scopeResolveInFlightRef.current.delete(p));
        }
      }
    };

    void run();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lens?.id, scope.join('\n'), JSON.stringify(scopeFragments)]);

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

  const handleLoadNews = React.useCallback(async () => {
    const paths = scope.filter((p) => {
      const s = (p || '').trim();
      return s.startsWith('/') && !s.startsWith('/query/');
    });
    if (paths.length === 0) {
      setNewsError('Add at least one canonical path to scope (e.g. /org/ae/deloitte)');
      return;
    }
    setNewsLoading(true);
    setNewsError(null);
    try {
      await api.fetchNewsByFragments({
        fragments: paths.map((cp) => ({
          canonicalPath: cp,
          canonicalName: scopeFragments[cp] || undefined,
        })),
        daysBack: 7,
      });
    } catch (e: any) {
      setNewsError(e?.response?.data?.error || e?.message || 'Failed to load news');
    } finally {
      setNewsLoading(false);
    }
  }, [scope, scopeFragments]);

  const Card: React.FC<{ title: string; subtitle?: string; children: React.ReactNode; right?: React.ReactNode }> = ({ title, subtitle, children, right }) => (
    <div
      style={{
        borderRadius: 16,
        border: '1px solid var(--border-subtle)',
        background: 'var(--bg-elevated)',
        boxShadow: '0 10px 30px rgba(0,0,0,0.10)',
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          padding: '14px 16px',
          borderBottom: '1px solid var(--border-subtle)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 12,
        }}
      >
        <div style={{ minWidth: 0 }}>
          <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', letterSpacing: 0.2 }}>
            {title}
          </div>
          {subtitle && (
            <div style={{ marginTop: 2, fontSize: 11, color: 'var(--text-secondary-strong)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {subtitle}
            </div>
          )}
        </div>
        {right}
      </div>
      <div style={{ padding: '14px 16px' }}>{children}</div>
    </div>
  );

  const Pill: React.FC<{ text: string; tone?: 'neutral' | 'success' | 'danger' }> = ({ text, tone = 'neutral' }) => {
    const styles =
      tone === 'success'
        ? { bg: 'var(--success-20)', fg: 'var(--accent-success)' }
        : tone === 'danger'
          ? { bg: 'rgba(248, 113, 113, 0.18)', fg: '#f87171' }
          : { bg: 'var(--white-10)', fg: 'var(--text-secondary-strong)' };
    return (
      <span
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          padding: '3px 8px',
          borderRadius: 999,
          fontSize: 11,
          fontWeight: 750,
          background: styles.bg,
          color: styles.fg,
          whiteSpace: 'nowrap',
        }}
      >
        {text}
      </span>
    );
  };

  const mono: React.CSSProperties = {
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
  };

  return (
    <div style={{ padding: '16px 18px', width: '100%', height: '100%', overflow: 'auto' }}>
      <style>{`
        .horizon-details-grid {
          display: grid;
          gap: 14px;
          grid-template-columns: repeat(3, minmax(280px, 1fr));
          grid-template-areas:
            "scope scope scope"
            "news news news"
            "info assembly config"
            "jobs jobs jobs";
          align-items: start;
        }
        @media (max-width: 980px) {
          .horizon-details-grid { grid-template-columns: 1fr; }
          .horizon-details-grid { grid-template-areas: "scope" "news" "info" "assembly" "config" "jobs"; }
        }
      `}</style>

      <div className="horizon-details-grid" style={{ minHeight: 0 }}>
        <div style={{ gridArea: 'info' }}>
          <Card
            title="Information"
            subtitle={lens?.id ? `Lens ID: ${String(lens.id)}` : undefined}
            right={
              <Pill
                text={String(lens?.status || 'UNKNOWN')}
                tone={String(lens?.status || '').toUpperCase() === 'ACTIVE' ? 'success' : 'neutral'}
              />
            }
          >
            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Created</div>
                <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 650 }}>
                  {lens?.createdAt ? new Date(lens.createdAt).toLocaleDateString() : '—'}
                </div>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Updated</div>
                <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 650 }}>
                  {lens?.updatedAt ? new Date(lens.updatedAt).toLocaleDateString() : '—'}
                </div>
              </div>
              {lens?.lastResolved && (
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Last resolved</div>
                  <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 650 }}>
                    {new Date(lens.lastResolved).toLocaleString()}
                  </div>
                </div>
              )}
            </div>
          </Card>
        </div>

        <div style={{ gridArea: 'news' }}>
          <Card
            title="News & ontology facts"
            subtitle="Fetch recent news via search grounding, create events and 12-axis signals for scope entities."
            right={
              <button
                type="button"
                onClick={() => void handleLoadNews()}
                disabled={newsLoading}
                style={{
                  padding: '8px 10px',
                  borderRadius: 12,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-heading)',
                  fontSize: 12,
                  fontWeight: 800,
                  cursor: newsLoading ? 'not-allowed' : 'pointer',
                  opacity: newsLoading ? 0.55 : 1,
                }}
              >
                {newsLoading ? 'Loading…' : 'Load news for scope'}
              </button>
            }
          >
            {newsError && (
              <div style={{ fontSize: 12, color: '#f87171', marginBottom: 8 }}>{newsError}</div>
            )}
            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
              Uses Gemini search grounding. Creates ontology facts (events), extracts 12-axis signals, propagates to graph.
            </div>
          </Card>
        </div>

        <div style={{ gridArea: 'scope', minHeight: 0 }}>
          <Card
            title={`Objects (${totalObjects})`}
            subtitle='Scope anchors, intents, domain contracts. Add scope by path ("/…") or query ("/query/…").'
          >
            {/* Edit controls (not scrollable) */}
            <div
              ref={scopeSuggestRef}
              style={{ position: 'relative' }}
              onBlurCapture={(e) => {
                // Close only when focus truly leaves this scope editor (input + dropdown).
                const next = (e.relatedTarget as globalThis.Node | null);
                if (next && (e.currentTarget as any)?.contains?.(next)) return;
                closeScopeSuggest();
              }}
            >
              <div style={{ display: 'flex', gap: 10, alignItems: 'center', marginBottom: 10 }}>
                <input
                  ref={scopeInputRef}
                  value={newScopePath}
                  onChange={(e) => {
                    setNewScopePath(e.target.value);
                    setScopeSuggestIndex(0);
                  }}
                  placeholder='Add path (e.g., "/org/acme") or type a name…'
                  style={{
                    flex: 1,
                    minWidth: 0,
                    padding: '10px 12px',
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-base)',
                    color: 'var(--text-heading)',
                    ...mono,
                    opacity: savingScope ? 0.7 : 1,
                  }}
                  disabled={!onUpdateScope || savingScope}
                  onKeyDown={(e) => {
                    if (e.key === 'Escape') {
                      e.preventDefault();
                      closeScopeSuggest();
                      return;
                    }
                    if (scopeSuggestOpen && scopeSuggestResults.length > 0) {
                      if (e.key === 'ArrowDown') {
                        e.preventDefault();
                        setScopeSuggestIndex((i) => Math.min(i + 1, Math.max(0, scopeSuggestResults.length - 1)));
                        return;
                      }
                      if (e.key === 'ArrowUp') {
                        e.preventDefault();
                        setScopeSuggestIndex((i) => Math.max(0, i - 1));
                        return;
                      }
                      if (e.key === 'Enter' && !newScopePath.trim().startsWith('/')) {
                        // In "search by name" mode: Enter picks a suggestion first.
                        e.preventDefault();
                        const picked = scopeSuggestResults[scopeSuggestIndex];
                        if (picked) pickScopeSuggestion(picked);
                        return;
                      }
                    }
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      void handleAddScope();
                    }
                  }}
                  onFocus={() => {
                    const q = (newScopePath || '').trim();
                    if (q && !q.startsWith('/')) setScopeSuggestOpen(true);
                  }}
                />
                <button
                  type="button"
                  onClick={() => void handleAddScope()}
                  disabled={!onUpdateScope || savingScope || !newScopePath.trim()}
                  style={{
                    padding: '10px 12px',
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--white-10)',
                    color: 'var(--text-heading)',
                    fontSize: 12,
                    fontWeight: 800,
                    cursor: !onUpdateScope || savingScope || !newScopePath.trim() ? 'not-allowed' : 'pointer',
                    opacity: !onUpdateScope || savingScope || !newScopePath.trim() ? 0.5 : 1,
                    whiteSpace: 'nowrap',
                  }}
                >
                  {savingScope ? 'Saving…' : 'Add'}
                </button>
              </div>

              {/* Suggestions dropdown */}
              {scopeSuggestOpen && !newScopePath.trim().startsWith('/') && (
                <div
                  style={{
                    position: 'absolute',
                    top: 44,
                    left: 0,
                    right: 82,
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-elevated)',
                    boxShadow: '0 18px 60px rgba(0,0,0,0.35)',
                    overflow: 'hidden',
                    zIndex: 20,
                  }}
                >
                  <div style={{ maxHeight: 240, overflowY: 'auto' }}>
                    {scopeSuggestLoading && (
                      <div style={{ padding: '10px 12px', fontSize: 12, color: 'var(--text-secondary-strong)' }}>Searching…</div>
                    )}
                    {scopeSuggestError && (
                      <div style={{ padding: '10px 12px', fontSize: 12, color: '#f87171' }}>{scopeSuggestError}</div>
                    )}
                    {!scopeSuggestLoading && !scopeSuggestError && newScopePath.trim().length < 2 && (
                      <div style={{ padding: '10px 12px', fontSize: 12, color: 'var(--text-secondary-strong)' }}>Type 2+ characters to search</div>
                    )}
                    {!scopeSuggestLoading && !scopeSuggestError && newScopePath.trim().length >= 2 && scopeSuggestResults.length === 0 && (
                      <div style={{ padding: '10px 12px', fontSize: 12, color: 'var(--text-secondary-strong)' }}>No matches</div>
                    )}
                    {!scopeSuggestLoading && !scopeSuggestError && scopeSuggestResults.map((r, idx) => {
                      const active = idx === scopeSuggestIndex;
                      return (
                        <button
                          key={`${r.id}-${idx}`}
                          type="button"
                          onClick={() => pickScopeSuggestion(r)}
                          onMouseEnter={() => setScopeSuggestIndex(idx)}
                          style={{
                            width: '100%',
                            textAlign: 'left',
                            padding: '10px 12px',
                            border: 'none',
                            background: active ? 'var(--white-10)' : 'transparent',
                            cursor: 'pointer',
                            display: 'flex',
                            flexDirection: 'column',
                            gap: 3,
                          }}
                        >
                          <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 10 }}>
                            <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                              {r.name}
                            </div>
                            <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)', flexShrink: 0 }}>
                              {String(r.type || '').toUpperCase()}
                            </div>
                          </div>
                          <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', ...mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            {r.canonicalPath}
                          </div>
                        </button>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>

          {scopeError && (
            <div style={{ marginBottom: 10, fontSize: 12, color: '#f87171' }}>
              {scopeError}
            </div>
          )}

          {totalObjects > 0 ? (
            <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden', maxHeight: 320 }}>
              <div style={{ maxHeight: 320, overflowY: 'auto' }}>
                {intents.map((entry: { id?: string; question?: string; status?: string }, idx: number) => {
                  const q = (entry?.question || '').trim();
                  const status = (entry?.status || '').trim();
                  return (
                    <div key={`intent-${entry?.id || idx}`} style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-subtle)' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                        <Pill text="INTENT" tone={status === 'answered' ? 'success' : 'neutral'} />
                        <div style={{ flex: 1, minWidth: 0, fontSize: 13, fontWeight: 750, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                          {q || '—'}
                        </div>
                      </div>
                    </div>
                  );
                })}
                {domains.map((d: { canonicalPath?: string; contractType?: string; title?: string; description?: string }, idx: number) => {
                  const path = (d?.canonicalPath || '').trim();
                  const ct = (d?.contractType || '').trim();
                  const title = (d?.title || d?.description || '').trim();
                  return (
                    <div key={`domain-${path || idx}`} style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-subtle)' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                        <Pill text={ct || 'DOMAIN'} />
                        <div style={{ flex: 1, minWidth: 0, fontSize: 13, fontWeight: 750, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                          {title || humanizeCanonicalPath(path) || '—'}
                        </div>
                      </div>
                      {path ? (
                        <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-secondary-strong)', ...mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={path}>
                          {path}
                        </div>
                      ) : null}
                    </div>
                  );
                })}
                {domainModesOnly && domainModes.map((mode: string, idx: number) => (
                  <div key={`domainMode-${idx}`} style={{ padding: '10px 12px', borderBottom: '1px solid var(--border-subtle)' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                      <Pill text="DOMAIN_MODE" />
                      <div style={{ flex: 1, minWidth: 0, fontSize: 13, fontWeight: 750, color: 'var(--text-heading)' }}>
                        {mode}
                      </div>
                    </div>
                  </div>
                ))}
                {scope.map((entry: string, index: number) => {
                  const path = (entry || '').trim();
                  const isQuery = path.startsWith('/query/');
                  const resolved = scopeResolved[path];
                  const resolvedList = isQuery ? (resolvedQueries?.[path] || []) : [];
                  return (
                    <div key={`scope-${path}-${index}`} style={{ padding: '10px 12px', background: 'transparent', borderBottom: '1px solid var(--border-subtle)' }}>
                      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
                        <div style={{ minWidth: 0, display: 'flex', flexDirection: 'column', gap: 4 }}>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                            {isQuery ? <Pill text="Query" /> : resolved?.type ? <Pill text={String(resolved.type)} /> : null}
                            <div style={{ minWidth: 0, fontSize: 13, fontWeight: 750, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                              {!isQuery ? (resolved?.name ? resolved.name : humanizeCanonicalPath(path) || '—') : '—'}
                            </div>
                            {isQuery && resolvedList.length > 0 && (
                              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', flexShrink: 0 }}>{resolvedList.length} resolved</div>
                            )}
                          </div>
                          <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', ...mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={path}>
                            {path}
                          </div>
                        </div>
                        <button
                          type="button"
                          onClick={() => void handleRemoveScope(path)}
                          disabled={!onUpdateScope || savingScope}
                          style={{
                            padding: '6px 10px',
                            borderRadius: 10,
                            border: '1px solid var(--border-subtle)',
                            background: 'transparent',
                            color: 'var(--text-secondary-strong)',
                            fontSize: 12,
                            fontWeight: 700,
                            cursor: !onUpdateScope || savingScope ? 'not-allowed' : 'pointer',
                            opacity: !onUpdateScope || savingScope ? 0.5 : 1,
                          }}
                          title="Remove"
                        >
                          Remove
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          ) : (
            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>No objects defined</div>
          )}
        </Card>
        </div>

        {assemblyStatus && (
          <div style={{ gridArea: 'assembly' }}>
            <Card
              title="Assembly status"
              subtitle="Shows the current state of lens materialization."
              right={
                <Pill
                  text={String(assemblyStatus?.status || 'unknown')}
                  tone={String(assemblyStatus?.status || '') === 'ready' ? 'success' : 'neutral'}
                />
              }
            >
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Stage</div>
                  <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 650 }}>
                    {String((assemblyStatus as any)?.stage || assemblyStatus?.phase || '—')}
                  </div>
                </div>
                {assemblyStatus?.progress !== undefined && (
                  <div>
                    <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, marginBottom: 6 }}>
                      <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Progress</div>
                      <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 650 }}>
                        {Math.round(Number(assemblyStatus.progress) * 100)}%
                      </div>
                    </div>
                    <div style={{ height: 8, borderRadius: 999, overflow: 'hidden', background: 'var(--white-10)' }}>
                      <div style={{ width: `${Math.max(0, Math.min(1, Number(assemblyStatus.progress))) * 100}%`, height: '100%', background: 'var(--accent-primary, #3B82F6)' }} />
                    </div>
                  </div>
                )}
                {((assemblyStatus as any)?.stages?.scouting || assemblyStatus?.phases?.scouting) && (
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, padding: '8px 10px', borderRadius: 12, border: '1px solid var(--border-subtle)', background: 'var(--white-06)' }}>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Scouting</div>
                    <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 750 }}>
                      {((assemblyStatus as any)?.stages?.scouting?.completed ?? assemblyStatus.phases.scouting.completed)} / {((assemblyStatus as any)?.stages?.scouting?.total ?? assemblyStatus.phases.scouting.total)}
                    </div>
                  </div>
                )}
              </div>
            </Card>
          </div>
        )}

        {(lens?.viewConfig && Object.keys(lens.viewConfig).length > 0) || (lens?.alertConfig && Object.keys(lens.alertConfig).length > 0) ? (
          <div style={{ gridArea: 'config', minHeight: 0 }}>
            <Card title="Configuration" subtitle="Advanced settings (JSON).">
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10, maxHeight: 240, overflowY: 'auto' }}>
                {lens?.viewConfig && Object.keys(lens.viewConfig).length > 0 && (
                  <details style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden' }}>
                    <summary style={{ padding: '10px 12px', cursor: 'pointer', fontSize: 12, fontWeight: 750, color: 'var(--text-heading)' }}>
                      View configuration
                    </summary>
                    <div style={{ padding: '10px 12px', borderTop: '1px solid var(--border-subtle)', background: 'var(--white-06)' }}>
                      <pre style={{ margin: 0, fontSize: 11, color: 'var(--text-heading)', overflow: 'auto', ...mono }}>
                        {JSON.stringify(lens.viewConfig, null, 2)}
                      </pre>
                    </div>
                  </details>
                )}
                {lens?.alertConfig && Object.keys(lens.alertConfig).length > 0 && (
                  <details style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden' }}>
                    <summary style={{ padding: '10px 12px', cursor: 'pointer', fontSize: 12, fontWeight: 750, color: 'var(--text-heading)' }}>
                      Alert configuration
                    </summary>
                    <div style={{ padding: '10px 12px', borderTop: '1px solid var(--border-subtle)', background: 'var(--white-06)' }}>
                      <pre style={{ margin: 0, fontSize: 11, color: 'var(--text-heading)', overflow: 'auto', ...mono }}>
                        {JSON.stringify(lens.alertConfig, null, 2)}
                      </pre>
                    </div>
                  </details>
                )}
              </div>
            </Card>
          </div>
        ) : null}

        <div style={{ gridArea: 'jobs', minHeight: 0 }}>
          <Card
            title={`Scouting jobs (${scoutingJobs.length})`}
            subtitle="Recent scope scouting tasks for this lens."
            right={
              <button
                type="button"
                onClick={onRefreshJobs}
                disabled={loadingJobs}
                style={{
                  padding: '8px 10px',
                  borderRadius: 12,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-heading)',
                  fontSize: 12,
                  fontWeight: 800,
                  cursor: loadingJobs ? 'not-allowed' : 'pointer',
                  opacity: loadingJobs ? 0.55 : 1,
                }}
              >
                {loadingJobs ? 'Loading…' : 'Refresh'}
              </button>
            }
          >
            {scoutingJobs.length > 0 ? (
              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, overflow: 'hidden', maxHeight: 260 }}>
                <div style={{ maxHeight: 260, overflowY: 'auto' }}>
                  {scoutingJobs.map((job: any, idx: number) => {
                    const status = String(job?.status || '').toUpperCase();
                    const tone = status === 'VERIFIED' || status === 'COMPLETED' ? 'success' : (status === 'FAILED' || status === 'ERROR' ? 'danger' : 'neutral');
                    const showDivider = idx !== scoutingJobs.length - 1;
                    return (
                      <div key={job.id} style={{ padding: '10px 12px', borderBottom: showDivider ? '1px solid var(--border-subtle)' : 'none' }}>
                        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
                            <Pill text={status || 'UNKNOWN'} tone={tone as any} />
                            <div style={{ minWidth: 0 }}>
                              <div style={{ fontSize: 12, color: 'var(--text-heading)', fontWeight: 700, ...mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={job.targetCanonicalPath}>
                                {job.targetCanonicalPath}
                              </div>
                            </div>
                          </div>
                          <div style={{ fontSize: 11, color: 'var(--text-tertiary-weak)', flexShrink: 0 }}>
                            {job?.createdAt ? new Date(job.createdAt).toLocaleDateString() : '—'}
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            ) : (
              <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>No scouting jobs found</div>
            )}
          </Card>
        </div>
      </div>
    </div>
  );
};


