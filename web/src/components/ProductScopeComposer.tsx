import React from 'react';
import { ThemeContext } from './ThemeProvider';
import {
  api,
  type HorizonProductProducer,
  type HorizonProductProducersResponse,
  type HorizonProductStreamNode,
  type HorizonProductStreamResponse,
} from '../lib/api';

export type ProductScopeSelection = {
  canonicalPath: string;
  label: string;
  sourceScope?: string;
};

type ProductStreamPair = {
  upstream: HorizonProductStreamResponse | null;
  downstream: HorizonProductStreamResponse | null;
};

export interface ProductScopeComposerProps {
  selectedProducts: ProductScopeSelection[];
  activePath: string | null;
  onSelectProduct: (path: string) => void;
  maxHops?: number;
  maxPerHop?: number;
  minHeight?: number;
}

const uniqByPath = (nodes: HorizonProductStreamNode[] | undefined, hop: number) => {
  const out: HorizonProductStreamNode[] = [];
  const seen = new Set<string>();
  for (const node of nodes || []) {
    if (!node || node.hop !== hop || !node.canonicalPath || seen.has(node.canonicalPath)) continue;
    seen.add(node.canonicalPath);
    out.push(node);
  }
  return out;
};

const uniqProducerLabels = (producers: HorizonProductProducer[] | undefined) => {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const producer of producers || []) {
    const label = String(producer.label || '').trim();
    if (!label) continue;
    const key = label.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(label);
  }
  return out;
};

const summarizeLabels = (labels: string[], limit = 3) => {
  const trimmed = labels.filter(Boolean);
  if (trimmed.length === 0) return 'None yet';
  const top = trimmed.slice(0, limit);
  const rest = trimmed.length - top.length;
  return rest > 0 ? `${top.join(', ')} +${rest} more` : top.join(', ');
};

const SummaryLine: React.FC<{
  label: string;
  value: string;
}> = ({ label, value }) => (
  <div
    style={{
      display: 'grid',
      gridTemplateColumns: '72px minmax(0, 1fr)',
      gap: 10,
      alignItems: 'start',
    }}
  >
    <div
      style={{
        fontSize: 10,
        fontWeight: 800,
        letterSpacing: '0.08em',
        textTransform: 'uppercase',
        color: 'var(--text-tertiary)',
      }}
    >
      {label}
    </div>
    <div
      style={{
        fontSize: 13,
        lineHeight: 1.5,
        color: 'var(--text-secondary-strong)',
      }}
    >
      {value}
    </div>
  </div>
);

export const ProductScopeComposer: React.FC<ProductScopeComposerProps> = ({
  selectedProducts,
  activePath,
  onSelectProduct,
  maxHops = 3,
  maxPerHop = 7,
  minHeight = 260,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const [streamCache, setStreamCache] = React.useState<Record<string, ProductStreamPair>>({});
  const [producerCache, setProducerCache] = React.useState<Record<string, HorizonProductProducersResponse>>({});
  const [loadingPaths, setLoadingPaths] = React.useState<Record<string, boolean>>({});
  const [errorByPath, setErrorByPath] = React.useState<Record<string, string>>({});
  const scrollRef = React.useRef<HTMLDivElement | null>(null);
  const [showBottomFade, setShowBottomFade] = React.useState(false);

  const syncFade = React.useCallback(() => {
    const node = scrollRef.current;
    if (!node) {
      setShowBottomFade(false);
      return;
    }
    const hasOverflow = node.scrollHeight - node.clientHeight > 8;
    const nearBottom = node.scrollTop + node.clientHeight >= node.scrollHeight - 8;
    setShowBottomFade(hasOverflow && !nearBottom);
  }, []);

  React.useEffect(() => {
    const paths = selectedProducts
      .map((item) => item.canonicalPath)
      .filter(Boolean)
      .filter((path) => !streamCache[path] || !producerCache[path]);
    if (paths.length === 0) return;

    let cancelled = false;

    const load = async () => {
      setLoadingPaths((prev) => {
        const next = { ...prev };
        for (const path of paths) next[path] = true;
        return next;
      });

      await Promise.all(paths.map(async (path) => {
        try {
          const [upstream, downstream, producers] = await Promise.all([
            streamCache[path]?.upstream
              ? Promise.resolve(streamCache[path].upstream)
              : api.getHorizonProductStream({
                  productRef: path,
                  direction: 'upstream',
                  hops: maxHops,
                  limitPerHop: maxPerHop,
                }),
            streamCache[path]?.downstream
              ? Promise.resolve(streamCache[path].downstream)
              : api.getHorizonProductStream({
                  productRef: path,
                  direction: 'downstream',
                  hops: maxHops,
                  limitPerHop: maxPerHop,
                }),
            producerCache[path]?.resolvedPath
              ? Promise.resolve(producerCache[path])
              : api.getHorizonProductProducers({
                  productRef: path,
                  limit: maxPerHop,
                }),
          ]);

          if (cancelled) return;
          setStreamCache((prev) => ({ ...prev, [path]: { upstream, downstream } }));
          setProducerCache((prev) => ({ ...prev, [path]: producers }));
          setErrorByPath((prev) => {
            if (!prev[path]) return prev;
            const next = { ...prev };
            delete next[path];
            return next;
          });
        } catch (error: any) {
          if (cancelled) return;
          setErrorByPath((prev) => ({
            ...prev,
            [path]: error?.response?.data?.error || error?.message || 'Failed to load product context',
          }));
        } finally {
          if (cancelled) return;
          setLoadingPaths((prev) => ({ ...prev, [path]: false }));
        }
      }));
    };

    void load();
    return () => {
      cancelled = true;
    };
  }, [maxHops, maxPerHop, producerCache, selectedProducts, streamCache]);

  React.useEffect(() => {
    syncFade();
    const node = scrollRef.current;
    if (!node) return;
    node.addEventListener('scroll', syncFade, { passive: true });
    window.addEventListener('resize', syncFade);
    return () => {
      node.removeEventListener('scroll', syncFade);
      window.removeEventListener('resize', syncFade);
    };
  }, [selectedProducts, streamCache, producerCache, errorByPath, loadingPaths, syncFade]);

  if (selectedProducts.length === 0) return null;

  return (
    <div
      style={{
        width: '100%',
        minHeight,
        position: 'relative',
      }}
    >
      <div
        ref={scrollRef}
        style={{
          maxHeight: `max(${minHeight}px, min(52vh, 560px))`,
          overflowY: 'auto',
          overflowX: 'hidden',
          display: 'flex',
          flexDirection: 'column',
          gap: 12,
          paddingTop: 6,
          paddingRight: 4,
          scrollbarGutter: 'stable',
        }}
      >
        {selectedProducts.map((selection) => {
          const path = selection.canonicalPath;
          const active = activePath === path;
          const stream = streamCache[path];
          const producers = producerCache[path];
          const upstream = uniqByPath(stream?.upstream?.nodes, 1).map((node) => node.label || node.canonicalPath);
          const downstream = uniqByPath(stream?.downstream?.nodes, 1).map((node) => node.label || node.canonicalPath);
          const producerLabels = uniqProducerLabels(producers?.producers);
          const loading = loadingPaths[path];
          const error = errorByPath[path];

          return (
            <button
              key={path}
              type="button"
              onClick={() => onSelectProduct(path)}
              style={{
                width: '100%',
                textAlign: 'left',
                borderRadius: 18,
                border: active ? '1px solid rgba(245,158,11,0.7)' : '1px solid var(--border-subtle)',
                background: theme === 'dark' ? 'rgba(17,17,19,0.72)' : 'rgba(255,255,255,0.8)',
                backdropFilter: 'blur(10px)',
                boxShadow: active ? '0 18px 42px rgba(0,0,0,0.22)' : '0 10px 28px rgba(0,0,0,0.14)',
                padding: '16px 18px',
                cursor: 'pointer',
              }}
            >
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 12,
                  marginBottom: 12,
                }}
              >
                <div style={{ minWidth: 0 }}>
                  <div
                    style={{
                      fontSize: 15,
                      fontWeight: 800,
                      color: 'var(--text-heading)',
                      lineHeight: 1.3,
                    }}
                  >
                    {selection.label}
                  </div>
                  <div
                    style={{
                      marginTop: 4,
                      fontSize: 11,
                      color: 'var(--text-tertiary)',
                      whiteSpace: 'nowrap',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                    }}
                  >
                    {path}
                  </div>
                </div>
                {active && (
                  <div
                    style={{
                      fontSize: 10,
                      fontWeight: 800,
                      letterSpacing: '0.08em',
                      textTransform: 'uppercase',
                      color: 'var(--accent-primary)',
                      flexShrink: 0,
                    }}
                  >
                    Selected
                  </div>
                )}
              </div>

              {error ? (
                <div style={{ fontSize: 13, color: '#ef4444' }}>{error}</div>
              ) : (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                  <SummaryLine label="From" value={loading && !stream ? 'Loading…' : summarizeLabels(upstream)} />
                  <SummaryLine label="Into" value={loading && !stream ? 'Loading…' : summarizeLabels(downstream)} />
                  <SummaryLine label="Made by" value={loading && !producers ? 'Loading…' : summarizeLabels(producerLabels)} />
                </div>
              )}
            </button>
          );
        })}
      </div>

      {showBottomFade && (
        <div
          style={{
            position: 'absolute',
            left: 0,
            right: 0,
            bottom: 0,
            height: 64,
            pointerEvents: 'none',
            background: theme === 'dark'
              ? 'linear-gradient(to bottom, rgba(15,15,18,0), rgba(15,15,18,0.92) 78%, rgba(15,15,18,1))'
              : 'linear-gradient(to bottom, rgba(248,250,252,0), rgba(248,250,252,0.92) 78%, rgba(248,250,252,1))',
          }}
        />
      )}
    </div>
  );
};

export default ProductScopeComposer;
