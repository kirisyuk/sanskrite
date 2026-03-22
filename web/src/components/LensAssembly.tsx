import React from 'react';
import { ThemeContext } from './ThemeProvider';
import AssembleLensRequestComponent, { type AssembleLensRequest } from '../features/horizon/components/hook/AssembleLensRequest';
import {
  api,
  type HorizonProductPrefetchSuggestion,
  type HorizonProductStreamEdge,
  type HorizonProductStreamNode,
  type HorizonProductStreamResponse,
} from '../lib/api';

type AssemblyMode = 'products' | 'freeform' | 'orgfac';

interface LensAssemblyProps {
  lens: any;
  projectId?: string | null;
  onApplyScope: (nextScope: string[]) => Promise<void> | void;
  onOpenGraph: () => void;
  onOpenMap: () => void;
  onAssemble?: (request: AssembleLensRequest) => void;
}

type SelectedProduct = {
  canonicalPath: string;
  label: string;
  sourceScope?: string;
};

type ProductStreamPair = {
  upstream: HorizonProductStreamResponse | null;
  downstream: HorizonProductStreamResponse | null;
};

const MAX_HOPS = 3;

const byHop = (nodes: HorizonProductStreamNode[] | undefined, maxHops: number) => {
  const out = new Map<number, HorizonProductStreamNode[]>();
  for (let hop = 1; hop <= maxHops; hop += 1) {
    out.set(hop, []);
  }
  for (const node of nodes || []) {
    if (!node || node.hop <= 0) continue;
    const bucket = out.get(node.hop);
    if (!bucket) continue;
    bucket.push(node);
  }
  for (const [hop, bucket] of out.entries()) {
    bucket.sort((a, b) => (a.label || a.canonicalPath).localeCompare(b.label || b.canonicalPath));
    out.set(hop, bucket);
  }
  return out;
};

const edgeKindsForPath = (
  path: string,
  direction: 'upstream' | 'downstream',
  edges: HorizonProductStreamEdge[] | undefined,
) => {
  const out = new Set<string>();
  for (const edge of edges || []) {
    if (direction === 'downstream' && edge.toPath === path) out.add(edge.relationKind || 'transformation');
    if (direction === 'upstream' && edge.fromPath === path) out.add(edge.relationKind || 'transformation');
  }
  return Array.from(out);
};

export const LensAssembly: React.FC<LensAssemblyProps> = ({
  lens,
  projectId,
  onApplyScope,
  onOpenGraph,
  onOpenMap,
  onAssemble,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const [mode, setMode] = React.useState<AssemblyMode>('products');
  const [query, setQuery] = React.useState('');
  const deferredQuery = React.useDeferredValue(query);
  const [suggestions, setSuggestions] = React.useState<HorizonProductPrefetchSuggestion[]>([]);
  const [loadingSuggestions, setLoadingSuggestions] = React.useState(false);
  const [showSuggestions, setShowSuggestions] = React.useState(false);
  const [selectedProducts, setSelectedProducts] = React.useState<SelectedProduct[]>([]);
  const [activePath, setActivePath] = React.useState<string | null>(null);
  const [streamCache, setStreamCache] = React.useState<Record<string, ProductStreamPair>>({});
  const [loadingStream, setLoadingStream] = React.useState(false);
  const [streamError, setStreamError] = React.useState<string | null>(null);
  const [applyState, setApplyState] = React.useState<'idle' | 'saving' | 'saved' | 'error'>('idle');
  const [applyError, setApplyError] = React.useState<string | null>(null);
  const inputRef = React.useRef<HTMLInputElement | null>(null);
  const dropdownAbortRef = React.useRef<AbortController | null>(null);
  const productRootRef = React.useRef<HTMLDivElement | null>(null);

  const scopePaths = React.useMemo(
    () => new Set(((lens?.scope as string[] | undefined) || []).map((item) => String(item || '').trim()).filter(Boolean)),
    [lens?.scope],
  );

  const activeStream = activePath ? streamCache[activePath] || null : null;
  const upstreamByHop = React.useMemo(() => byHop(activeStream?.upstream?.nodes, MAX_HOPS), [activeStream?.upstream]);
  const downstreamByHop = React.useMemo(() => byHop(activeStream?.downstream?.nodes, MAX_HOPS), [activeStream?.downstream]);
  const activeLabel = React.useMemo(() => {
    const chip = selectedProducts.find((item) => item.canonicalPath === activePath);
    if (chip?.label) return chip.label;
    return activeStream?.downstream?.resolvedLabel || activeStream?.upstream?.resolvedLabel || '';
  }, [activePath, activeStream, selectedProducts]);

  React.useEffect(() => {
    if (mode !== 'products') return;
    const q = deferredQuery.trim();
    if (q.length < 2) {
      setSuggestions([]);
      setShowSuggestions(false);
      setLoadingSuggestions(false);
      try { dropdownAbortRef.current?.abort(); } catch {}
      return;
    }
    const handle = window.setTimeout(async () => {
      try {
        try { dropdownAbortRef.current?.abort(); } catch {}
        const abort = new AbortController();
        dropdownAbortRef.current = abort;
        setLoadingSuggestions(true);
        const response = await api.prefetchHorizonProducts({ query: q, limit: 8 }, abort.signal);
        const selected = new Set(selectedProducts.map((item) => item.canonicalPath));
        setSuggestions((response.suggestions || []).filter((item) => !selected.has(item.canonicalPath)));
        setShowSuggestions(true);
      } catch (error: any) {
        if (error?.name === 'AbortError' || error?.code === 'ERR_CANCELED') return;
        setSuggestions([]);
        setShowSuggestions(false);
      } finally {
        setLoadingSuggestions(false);
      }
    }, 160);
    return () => window.clearTimeout(handle);
  }, [deferredQuery, mode, selectedProducts]);

  React.useEffect(() => {
    const onDown = (event: MouseEvent) => {
      const root = productRootRef.current;
      if (!root || root.contains(event.target as Node)) return;
      setShowSuggestions(false);
    };
    window.addEventListener('mousedown', onDown);
    return () => window.removeEventListener('mousedown', onDown);
  }, []);

  React.useEffect(() => {
    if (selectedProducts.length === 0) {
      if (activePath !== null) setActivePath(null);
      return;
    }
    if (!activePath || !selectedProducts.some((item) => item.canonicalPath === activePath)) {
      setActivePath(selectedProducts[selectedProducts.length - 1].canonicalPath);
    }
  }, [activePath, selectedProducts]);

  React.useEffect(() => {
    if (!activePath || mode !== 'products') return;
    if (streamCache[activePath]?.upstream && streamCache[activePath]?.downstream) return;
    let cancelled = false;
    const load = async () => {
      setLoadingStream(true);
      setStreamError(null);
      try {
        const [upstream, downstream] = await Promise.all([
          api.getHorizonProductStream({ productRef: activePath, direction: 'upstream', hops: MAX_HOPS, limitPerHop: 14 }),
          api.getHorizonProductStream({ productRef: activePath, direction: 'downstream', hops: MAX_HOPS, limitPerHop: 14 }),
        ]);
        if (cancelled) return;
        setStreamCache((prev) => ({
          ...prev,
          [activePath]: { upstream, downstream },
        }));
      } catch (error: any) {
        if (cancelled) return;
        setStreamError(error?.response?.data?.error || error?.message || 'Failed to load product stream');
      } finally {
        if (!cancelled) setLoadingStream(false);
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [activePath, mode, streamCache]);

  const addProduct = React.useCallback((suggestion: HorizonProductPrefetchSuggestion) => {
    const canonicalPath = String(suggestion.canonicalPath || '').trim();
    if (!canonicalPath) return;
    setSelectedProducts((prev) => {
      if (prev.some((item) => item.canonicalPath === canonicalPath)) return prev;
      return [
        ...prev,
        {
          canonicalPath,
          label: suggestion.label || suggestion.matchedLabel || canonicalPath.split('/').filter(Boolean).slice(-1)[0] || canonicalPath,
          sourceScope: suggestion.sourceScope,
        },
      ];
    });
    setActivePath(canonicalPath);
    setQuery('');
    setShowSuggestions(false);
    setApplyState('idle');
    setApplyError(null);
    window.setTimeout(() => inputRef.current?.focus(), 0);
  }, []);

  const removeProduct = React.useCallback((canonicalPath: string) => {
    setSelectedProducts((prev) => prev.filter((item) => item.canonicalPath !== canonicalPath));
    setApplyState('idle');
    setApplyError(null);
  }, []);

  const handleApply = React.useCallback(async () => {
    const selectedPaths = selectedProducts.map((item) => item.canonicalPath).filter(Boolean);
    if (selectedPaths.length === 0) return;
    const nextScope = Array.from(new Set([...(lens?.scope || []), ...selectedPaths]));
    setApplyState('saving');
    setApplyError(null);
    try {
      await onApplyScope(nextScope);
      setApplyState('saved');
    } catch (error: any) {
      setApplyState('error');
      setApplyError(error?.response?.data?.error || error?.message || 'Failed to apply scope');
    }
  }, [lens?.scope, onApplyScope, selectedProducts]);

  const renderProductColumn = (
    title: string,
    direction: 'upstream' | 'downstream',
    nodes: HorizonProductStreamNode[],
    edges: HorizonProductStreamEdge[] | undefined,
  ) => (
    <div
      style={{
        minWidth: 0,
        display: 'flex',
        flexDirection: 'column',
        gap: 10,
        padding: 12,
        borderRadius: 14,
        border: '1px solid var(--border-subtle)',
        background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(15,23,42,0.03)',
      }}
    >
      <div style={{ fontSize: 11, fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--text-tertiary)' }}>
        {title}
      </div>
      {nodes.length === 0 ? (
        <div style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>No linked products at this hop</div>
      ) : (
        nodes.map((node) => {
          const kinds = edgeKindsForPath(node.canonicalPath, direction, edges);
          return (
            <button
              key={node.canonicalPath}
              type="button"
              onClick={() => setActivePath(node.canonicalPath)}
              style={{
                textAlign: 'left',
                border: node.canonicalPath === activePath ? '1px solid var(--accent-primary)' : '1px solid var(--border-subtle)',
                background: node.canonicalPath === activePath
                  ? (theme === 'dark' ? 'rgba(59,130,246,0.12)' : 'rgba(59,130,246,0.08)')
                  : 'var(--bg-elevated)',
                borderRadius: 12,
                padding: '10px 12px',
                cursor: 'pointer',
                display: 'flex',
                flexDirection: 'column',
                gap: 8,
              }}
            >
              <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>
                {node.label || node.canonicalPath.split('/').filter(Boolean).slice(-1)[0] || node.canonicalPath}
              </div>
              <div style={{ fontSize: 11, color: 'var(--text-tertiary)', wordBreak: 'break-all' }}>{node.canonicalPath}</div>
              {kinds.length > 0 && (
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  {kinds.map((kind) => (
                    <span
                      key={`${node.canonicalPath}:${kind}`}
                      style={{
                        fontSize: 10,
                        fontWeight: 700,
                        textTransform: 'uppercase',
                        letterSpacing: '0.04em',
                        padding: '4px 6px',
                        borderRadius: 999,
                        background: kind === 'assembly'
                          ? 'rgba(245,158,11,0.18)'
                          : 'rgba(16,185,129,0.18)',
                        color: kind === 'assembly' ? '#d97706' : '#059669',
                      }}
                    >
                      {kind}
                    </span>
                  ))}
                </div>
              )}
            </button>
          );
        })
      )}
    </div>
  );

  return (
    <div
      style={{
        flex: 1,
        minHeight: 0,
        display: 'flex',
        flexDirection: 'column',
        padding: '72px 20px 20px',
        gap: 16,
        overflow: 'auto',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
        <div>
          <div style={{ fontSize: 20, fontWeight: 800, color: 'var(--text-heading)' }}>Lens Assembly</div>
          <div style={{ marginTop: 4, fontSize: 13, color: 'var(--text-secondary-strong)' }}>
            Build scope from canonical products first, then move to Graph or Map.
          </div>
        </div>
        <div
          role="tablist"
          aria-label="Assembly mode"
          style={{
            display: 'inline-flex',
            gap: 6,
            padding: 6,
            borderRadius: 14,
            border: '1px solid var(--border-subtle)',
            background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(15,23,42,0.03)',
          }}
        >
          {[
            { key: 'products', label: 'Products', disabled: false },
            { key: 'freeform', label: 'Freeform', disabled: false },
            { key: 'orgfac', label: 'Org/Fac', disabled: true },
          ].map((item) => (
            <button
              key={item.key}
              type="button"
              disabled={item.disabled}
              onClick={() => !item.disabled && setMode(item.key as AssemblyMode)}
              style={{
                border: 'none',
                borderRadius: 10,
                padding: '10px 12px',
                background: mode === item.key ? 'var(--white-10)' : 'transparent',
                color: item.disabled ? 'var(--text-disabled)' : 'var(--text-heading)',
                fontSize: 12,
                fontWeight: 700,
                cursor: item.disabled ? 'not-allowed' : 'pointer',
              }}
              title={item.disabled ? 'Coming next' : item.label}
            >
              {item.label}
              {item.disabled ? ' · next' : ''}
            </button>
          ))}
        </div>
      </div>

      {mode === 'products' && (
        <>
          <div ref={productRootRef} style={{ position: 'relative' }}>
            <div
              style={{
                borderRadius: 18,
                border: '1px solid var(--border-subtle)',
                background: 'var(--bg-elevated)',
                padding: 14,
                display: 'flex',
                flexDirection: 'column',
                gap: 12,
              }}
            >
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                {selectedProducts.map((item) => {
                  const inScope = scopePaths.has(item.canonicalPath);
                  const active = activePath === item.canonicalPath;
                  return (
                    <button
                      key={item.canonicalPath}
                      type="button"
                      onClick={() => setActivePath(item.canonicalPath)}
                      style={{
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: 8,
                        borderRadius: 999,
                        border: active ? '1px solid var(--accent-primary)' : '1px solid var(--border-subtle)',
                        background: active
                          ? (theme === 'dark' ? 'rgba(59,130,246,0.12)' : 'rgba(59,130,246,0.08)')
                          : 'transparent',
                        color: 'var(--text-heading)',
                        padding: '8px 12px',
                        cursor: 'pointer',
                      }}
                    >
                      <span style={{ fontSize: 12, fontWeight: 700 }}>{item.label}</span>
                      {inScope && <span style={{ fontSize: 10, color: 'var(--text-tertiary)' }}>in scope</span>}
                      <span
                        onClick={(event) => {
                          event.stopPropagation();
                          removeProduct(item.canonicalPath);
                        }}
                        style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}
                      >
                        ×
                      </span>
                    </button>
                  );
                })}
              </div>

              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 12,
                  borderRadius: 14,
                  border: '1px solid var(--border-subtle)',
                  padding: '12px 14px',
                  background: theme === 'dark' ? 'rgba(255,255,255,0.02)' : 'rgba(15,23,42,0.02)',
                }}
              >
                <input
                  ref={inputRef}
                  type="text"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  onFocus={() => {
                    if (suggestions.length > 0) setShowSuggestions(true);
                  }}
                  placeholder="Search products by canonical name or synonym"
                  style={{
                    flex: 1,
                    minWidth: 180,
                    border: 'none',
                    outline: 'none',
                    background: 'transparent',
                    fontSize: 15,
                    color: 'var(--text-heading)',
                  }}
                />
                {loadingSuggestions && <span style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>Searching…</span>}
              </div>
            </div>

            {showSuggestions && suggestions.length > 0 && (
              <div
                style={{
                  position: 'absolute',
                  top: '100%',
                  left: 0,
                  right: 0,
                  marginTop: 10,
                  borderRadius: 16,
                  border: '1px solid var(--border-subtle)',
                  background: 'var(--bg-elevated)',
                  boxShadow: theme === 'dark' ? '0 22px 60px rgba(0,0,0,0.45)' : '0 22px 60px rgba(0,0,0,0.14)',
                  overflow: 'hidden',
                  zIndex: 30,
                }}
              >
                {suggestions.map((item) => (
                  <button
                    key={item.canonicalPath}
                    type="button"
                    onClick={() => addProduct(item)}
                    style={{
                      width: '100%',
                      textAlign: 'left',
                      border: 'none',
                      background: 'transparent',
                      cursor: 'pointer',
                      padding: '12px 14px',
                      display: 'flex',
                      justifyContent: 'space-between',
                      gap: 14,
                    }}
                    onMouseEnter={(event) => { event.currentTarget.style.background = 'var(--white-06)'; }}
                    onMouseLeave={(event) => { event.currentTarget.style.background = 'transparent'; }}
                  >
                    <div style={{ minWidth: 0 }}>
                      <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>{item.label}</div>
                      <div style={{ marginTop: 2, fontSize: 11, color: 'var(--text-tertiary)', wordBreak: 'break-all' }}>{item.canonicalPath}</div>
                      {(item.matchedLabel || item.canonicalizedFrom) && (
                        <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                          matched: {item.matchedLabel || item.canonicalizedFrom}
                        </div>
                      )}
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--text-tertiary)', whiteSpace: 'nowrap' }}>
                      {item.sourceScope || 'global'}
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>

          <div
            style={{
              flex: 1,
              minHeight: 320,
              display: 'flex',
              flexDirection: 'column',
              gap: 14,
              borderRadius: 20,
              border: '1px solid var(--border-subtle)',
              background: theme === 'dark'
                ? 'linear-gradient(180deg, rgba(255,255,255,0.02) 0%, rgba(255,255,255,0.01) 100%)'
                : 'linear-gradient(180deg, rgba(248,250,252,0.9) 0%, rgba(255,255,255,0.95) 100%)',
              padding: 18,
            }}
          >
            {streamError && <div style={{ fontSize: 12, color: '#ef4444' }}>{streamError}</div>}
            {!activePath && (
              <div style={{ margin: 'auto', textAlign: 'center', maxWidth: 520 }}>
                <div style={{ fontSize: 18, fontWeight: 800, color: 'var(--text-heading)' }}>Start with a product</div>
                <div style={{ marginTop: 8, fontSize: 13, color: 'var(--text-secondary-strong)' }}>
                  Pick a canonical product through prefetch. Assembly will show upstream products on the right and downstream products on the left.
                </div>
              </div>
            )}

            {activePath && (
              <>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
                  <div>
                    <div style={{ fontSize: 12, color: 'var(--text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.08em' }}>
                      Active product
                    </div>
                    <div style={{ marginTop: 4, fontSize: 20, fontWeight: 800, color: 'var(--text-heading)' }}>
                      {activeLabel || activePath.split('/').filter(Boolean).slice(-1)[0] || activePath}
                    </div>
                    <div style={{ marginTop: 6, fontSize: 11, color: 'var(--text-tertiary)', wordBreak: 'break-all' }}>{activePath}</div>
                  </div>
                  {loadingStream && <div style={{ fontSize: 12, color: 'var(--text-tertiary)' }}>Loading product tree…</div>}
                </div>

                <div
                  style={{
                    display: 'grid',
                    gridTemplateColumns: 'repeat(7, minmax(0, 1fr))',
                    gap: 12,
                    alignItems: 'start',
                  }}
                >
                  {[3, 2, 1].map((hop) => (
                    <div key={`downstream-${hop}`}>
                      {renderProductColumn(
                        `Downstream +${hop}`,
                        'downstream',
                        downstreamByHop.get(hop) || [],
                        activeStream?.downstream?.edges,
                      )}
                    </div>
                  ))}

                  <div
                    style={{
                      minWidth: 0,
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 10,
                      justifyContent: 'center',
                      alignItems: 'stretch',
                    }}
                  >
                    <div
                      style={{
                        borderRadius: 16,
                        padding: 16,
                        border: '1px solid var(--accent-primary)',
                        background: theme === 'dark' ? 'rgba(59,130,246,0.12)' : 'rgba(59,130,246,0.08)',
                      }}
                    >
                      <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-tertiary)', textTransform: 'uppercase', letterSpacing: '0.08em' }}>
                        Center
                      </div>
                      <div style={{ marginTop: 8, fontSize: 15, fontWeight: 800, color: 'var(--text-heading)' }}>
                        {activeLabel || activePath}
                      </div>
                    </div>
                  </div>

                  {[1, 2, 3].map((hop) => (
                    <div key={`upstream-${hop}`}>
                      {renderProductColumn(
                        `Upstream -${hop}`,
                        'upstream',
                        upstreamByHop.get(hop) || [],
                        activeStream?.upstream?.edges,
                      )}
                    </div>
                  ))}
                </div>
              </>
            )}
          </div>
        </>
      )}

      {mode === 'freeform' && (
        <div
          style={{
            borderRadius: 18,
            border: '1px solid var(--border-subtle)',
            background: theme === 'dark' ? 'rgba(255,255,255,0.02)' : 'rgba(255,255,255,0.95)',
            padding: 18,
          }}
        >
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 18, fontWeight: 800, color: 'var(--text-heading)' }}>Freeform lens flow</div>
            <div style={{ marginTop: 6, fontSize: 13, color: 'var(--text-secondary-strong)' }}>
              This mode keeps the existing query-based assembly and preflight behavior.
            </div>
          </div>
          {onAssemble && projectId ? (
            <AssembleLensRequestComponent projectId={projectId} onAssemble={onAssemble} />
          ) : (
            <div style={{ fontSize: 13, color: 'var(--text-secondary-strong)' }}>
              Freeform assembly is unavailable without a project context.
            </div>
          )}
        </div>
      )}

      {mode === 'orgfac' && (
        <div
          style={{
            borderRadius: 18,
            border: '1px dashed var(--border-subtle)',
            background: theme === 'dark' ? 'rgba(255,255,255,0.02)' : 'rgba(15,23,42,0.02)',
            padding: 22,
          }}
        >
          <div style={{ fontSize: 18, fontWeight: 800, color: 'var(--text-heading)' }}>Org/Fac</div>
          <div style={{ marginTop: 8, fontSize: 13, color: 'var(--text-secondary-strong)' }}>
            Coming next. This mode will compose scope from organization and facility one-hop supply-chain expansion.
          </div>
        </div>
      )}

      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 12,
          flexWrap: 'wrap',
          padding: 14,
          borderRadius: 16,
          border: '1px solid var(--border-subtle)',
          background: 'var(--bg-elevated)',
        }}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          <div style={{ fontSize: 13, fontWeight: 700, color: 'var(--text-heading)' }}>
            {selectedProducts.length} selected product{selectedProducts.length === 1 ? '' : 's'}
          </div>
          <div style={{ fontSize: 12, color: applyState === 'error' ? '#ef4444' : 'var(--text-tertiary)' }}>
            {applyState === 'saved'
              ? 'Scope updated. Stay here or move to Graph/Map.'
              : applyError || 'Apply adds only the selected canonical chips to lens scope.'}
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
          <button
            type="button"
            onClick={() => {
              setSelectedProducts([]);
              setActivePath(null);
              setApplyState('idle');
              setApplyError(null);
            }}
            style={{
              borderRadius: 12,
              border: '1px solid var(--border-subtle)',
              background: 'transparent',
              color: 'var(--text-heading)',
              padding: '10px 12px',
              cursor: 'pointer',
              fontSize: 12,
              fontWeight: 700,
            }}
          >
            Clear
          </button>
          <button
            type="button"
            onClick={handleApply}
            disabled={selectedProducts.length === 0 || applyState === 'saving'}
            style={{
              borderRadius: 12,
              border: 'none',
              background: selectedProducts.length === 0 ? 'var(--surface-muted)' : 'var(--accent-primary)',
              color: selectedProducts.length === 0 ? 'var(--text-disabled)' : '#fff',
              padding: '10px 14px',
              cursor: selectedProducts.length === 0 ? 'not-allowed' : 'pointer',
              fontSize: 12,
              fontWeight: 800,
            }}
          >
            {applyState === 'saving' ? 'Applying…' : 'Apply'}
          </button>
          <button
            type="button"
            onClick={onOpenGraph}
            style={{
              borderRadius: 12,
              border: '1px solid var(--border-subtle)',
              background: 'transparent',
              color: 'var(--text-heading)',
              padding: '10px 12px',
              cursor: 'pointer',
              fontSize: 12,
              fontWeight: 700,
            }}
          >
            Graph
          </button>
          <button
            type="button"
            onClick={onOpenMap}
            style={{
              borderRadius: 12,
              border: '1px solid var(--border-subtle)',
              background: 'transparent',
              color: 'var(--text-heading)',
              padding: '10px 12px',
              cursor: 'pointer',
              fontSize: 12,
              fontWeight: 700,
            }}
          >
            Map
          </button>
        </div>
      </div>
    </div>
  );
};

export default LensAssembly;
