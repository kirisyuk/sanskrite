/**
 * Supply Chain Overlay - displays supply chains for the lens and allows loading them onto the graph.
 */

import React from 'react';
import { api } from '../lib/api';
import type { Node } from 'reactflow';

export interface MaterializedArtifact {
  id: string;
  lensId?: string;
  kind: string;
  displayName?: string;
  nodeRefs?: string[];
  payload?: {
    v?: number;
    nodes?: Array<{ type: string; ref: string }>;
    edges?: Array<{
      type: string;
      from: number;
      to: number;
      relation?: string;
      viaProductIndex?: number;
    }>;
  };
}

interface SupplyChainOverlayProps {
  lensId: string | null;
  nodes: Node[];
  isVisible: boolean;
  onLoadChain: (paths: string[], artifact: MaterializedArtifact) => Promise<void>;
  onRetry?: () => void;
  refreshKey?: number;
}

export function SupplyChainOverlay({
  lensId,
  nodes,
  isVisible,
  onLoadChain,
  onRetry,
  refreshKey = 0,
}: SupplyChainOverlayProps) {
  const [chains, setChains] = React.useState<MaterializedArtifact[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);

  React.useEffect(() => {
    if (!lensId || !isVisible) {
      setChains([]);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .getSupplyChainsByLens(lensId)
      .then((res) => {
        if (cancelled) return;
        setChains(res.chains || []);
      })
      .catch((err: any) => {
        if (cancelled) return;
        setError(err?.message || 'Failed to load supply chains');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [lensId, isVisible, refreshKey]);

  const canonicalPathsOnGraph = React.useMemo(() => {
    const set = new Set<string>();
    for (const n of nodes) {
      const cp = String((n.data as any)?.canonicalPath || '').trim();
      if (cp) set.add(cp);
    }
    return set;
  }, [nodes]);

  const chainStatus = React.useMemo(() => {
    return chains.map((c) => {
      const refs = c.nodeRefs || (c.payload?.nodes?.map((x) => x.ref) ?? []);
      const onGraph = refs.some((r) => canonicalPathsOnGraph.has(r));
      return { artifact: c, onGraph };
    });
  }, [chains, canonicalPathsOnGraph]);

  const handleSelect = React.useCallback(
    async (artifact: MaterializedArtifact) => {
      const paths = artifact.nodeRefs || (artifact.payload?.nodes?.map((n) => n.ref) ?? []).filter(Boolean);
      if (paths.length === 0) return;
      try {
        await onLoadChain(paths, artifact);
      } catch (err: any) {
        console.error('[SupplyChainOverlay] Load chain failed:', err);
      }
    },
    [onLoadChain]
  );

  if (!isVisible) return null;

  return (
    <div
      style={{
        minWidth: 260,
        maxWidth: 320,
        maxHeight: 'calc(100vh - 120px)',
        overflowY: 'auto',
        borderRadius: 12,
        border: '1px solid var(--border-subtle)',
        background: 'var(--bg-elevated)',
        boxShadow: '0 8px 24px rgba(0,0,0,0.15)',
        zIndex: 20,
      }}
    >
      <div
        style={{
          padding: '10px 12px',
          fontSize: 11,
          color: 'var(--text-secondary-strong)',
          borderBottom: '1px solid var(--border-subtle)',
          fontWeight: 600,
        }}
      >
        Supply Chains
      </div>
      {loading && (
        <div style={{ padding: 16, color: 'var(--text-secondary-strong)', fontSize: 12 }}>Loading…</div>
      )}
      {error && (
        <div style={{ padding: 12 }}>
          <div style={{ color: 'var(--text-error)', fontSize: 12, marginBottom: 8 }}>{error}</div>
          {onRetry && (
            <button
              type="button"
              onClick={onRetry}
              style={{
                padding: '6px 12px',
                fontSize: 12,
                borderRadius: 8,
                border: '1px solid var(--border-subtle)',
                background: 'var(--bg-elevated)',
                cursor: 'pointer',
              }}
            >
              Retry
            </button>
          )}
        </div>
      )}
      {!loading && !error && chainStatus.length === 0 && (
        <div style={{ padding: 16, color: 'var(--text-secondary-strong)', fontSize: 12 }}>
          No supply chains for this lens
        </div>
      )}
      {!loading && !error && chainStatus.length > 0 && (
        <div style={{ padding: '8px 0' }}>
          {chainStatus.map(({ artifact, onGraph }) => (
            <button
              key={artifact.id}
              type="button"
              onClick={() => handleSelect(artifact)}
              style={{
                width: '100%',
                padding: '10px 12px',
                border: 'none',
                background: 'transparent',
                color: 'var(--text-heading)',
                cursor: 'pointer',
                textAlign: 'left',
                fontSize: 12,
                display: 'flex',
                flexDirection: 'column',
                gap: 4,
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.background = 'var(--white-10)';
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.background = 'transparent';
              }}
            >
              <span style={{ fontWeight: 600 }}>
                {artifact.displayName || artifact.id || 'Unnamed chain'}
              </span>
              {onGraph && (
                <span style={{ fontSize: 11, color: 'var(--text-success)' }}>On graph</span>
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
