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
    legs?: Array<{
      index: number;
      supplierRef?: string;
      customerRef?: string;
      productRef?: string;
      transportViaRefs?: string[];
      flowRefs?: string[];
      alternateFlowRefs?: string[];
      zoneRefs?: string[];
      affectedZoneRefs?: string[];
      spatialSemantics?: string[];
      bindingConfidence?: number;
      stress?: {
        fragility?: number;
        rerouteability?: number;
        zonePressure?: number;
        affectedZonePressure?: number;
        recoveryLatencyHours?: number;
        stressScore?: number;
        dominantFactors?: string[];
      };
    }>;
    stress?: {
      pathScore?: number;
      fragility?: number;
      rerouteability?: number;
      zonePressure?: number;
      affectedZonePressure?: number;
      recoveryLatencyHours?: number;
      bottleneckLegIndex?: number;
      dominantFactors?: string[];
    };
    edges?: Array<{
      type: string;
      from: number;
      to: number;
      relation?: string;
      viaProductIndex?: number;
      productNodeIndex?: number;
      productRef?: string;
      supplyLinkId?: string;
      segmentIndex?: number;
      carriesProduct?: boolean;
      role?: string;
      qualifiers?: Record<string, any>;
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
      const legs = c.payload?.legs || [];
      const stress = c.payload?.stress;
      const flowRefs = new Set<string>();
      const zoneRefs = new Set<string>();
      const affectedZoneRefs = new Set<string>();
      const semantics = new Set<string>();
      let bindingConfidenceTotal = 0;
      let bindingConfidenceCount = 0;
      for (const leg of legs) {
        for (const ref of leg.flowRefs || []) flowRefs.add(String(ref || '').trim());
        for (const ref of leg.zoneRefs || []) zoneRefs.add(String(ref || '').trim());
        for (const ref of leg.affectedZoneRefs || []) affectedZoneRefs.add(String(ref || '').trim());
        for (const tag of leg.spatialSemantics || []) semantics.add(String(tag || '').trim());
        if (typeof leg.bindingConfidence === 'number' && Number.isFinite(leg.bindingConfidence)) {
          bindingConfidenceTotal += leg.bindingConfidence;
          bindingConfidenceCount += 1;
        }
      }
      const avgBindingConfidence =
        bindingConfidenceCount > 0 ? bindingConfidenceTotal / bindingConfidenceCount : 0;
      return {
        artifact: c,
        onGraph,
        legCount: legs.length,
        pathStressScore:
          typeof stress?.pathScore === 'number' && Number.isFinite(stress.pathScore) ? stress.pathScore : 0,
        bindingConfidence: avgBindingConfidence,
        flowCount: Array.from(flowRefs).filter(Boolean).length,
        zoneCount: Array.from(zoneRefs).filter(Boolean).length,
        affectedZoneCount: Array.from(affectedZoneRefs).filter(Boolean).length,
        spatialSemantics: Array.from(semantics).filter(Boolean),
      };
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
          {chainStatus.map(({ artifact, onGraph, legCount, pathStressScore, bindingConfidence, flowCount, zoneCount, affectedZoneCount, spatialSemantics }) => (
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
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                {legCount > 0 && (
                  <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                    {legCount} legs
                  </span>
                )}
                {bindingConfidence > 0 && (
                  <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                    bind {Math.round(bindingConfidence * 100)}%
                  </span>
                )}
                {pathStressScore > 0 && (
                  <span style={{ fontSize: 11, color: pathStressScore >= 0.55 ? 'var(--text-warning)' : 'var(--text-secondary-strong)' }}>
                    stress {pathStressScore.toFixed(2)}
                  </span>
                )}
              </div>
              {(flowCount > 0 || zoneCount > 0 || affectedZoneCount > 0) && (
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  {flowCount > 0 && (
                    <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                      {flowCount} flows
                    </span>
                  )}
                  {zoneCount > 0 && (
                    <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                      {zoneCount} zones
                    </span>
                  )}
                  {affectedZoneCount > 0 && (
                    <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                      {affectedZoneCount} affected
                    </span>
                  )}
                </div>
              )}
              {spatialSemantics.length > 0 && (
                <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                  {spatialSemantics.slice(0, 3).join(' • ')}
                </span>
              )}
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
