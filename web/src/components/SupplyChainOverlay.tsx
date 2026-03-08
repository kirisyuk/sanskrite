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
  flowRefs?: string[];
  zoneRefs?: string[];
  affectedZoneRefs?: string[];
  anchorSupplier?: string;
  anchorCustomer?: string;
  productAxis?: string;
  discoverySource?: string;
  discoveryReason?: string;
  matchedRefs?: string[];
  overlapScore?: number;
  isPinned?: boolean;
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
  const [scopedTotal, setScopedTotal] = React.useState(0);
  const [discoveredTotal, setDiscoveredTotal] = React.useState(0);
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
        setScopedTotal(Number(res.scopedTotal || 0));
        setDiscoveredTotal(Number(res.discoveredTotal || 0));
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
        overlapScore: typeof c.overlapScore === 'number' ? c.overlapScore : 0,
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
        {(scopedTotal > 0 || discoveredTotal > 0) && (
          <span style={{ marginLeft: 8, fontWeight: 500, opacity: 0.75 }}>
            {scopedTotal > 0 ? `${scopedTotal} pinned` : '0 pinned'}
            {discoveredTotal > 0 ? ` • ${discoveredTotal} discovered` : ''}
          </span>
        )}
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
          {chainStatus.map(({ artifact, onGraph, legCount, pathStressScore, bindingConfidence, flowCount, zoneCount, affectedZoneCount, spatialSemantics, overlapScore }) => (
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
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                <span style={{ fontWeight: 600 }}>
                  {artifact.displayName || artifact.id || 'Unnamed chain'}
                </span>
                <span
                  style={{
                    fontSize: 10,
                    padding: '2px 6px',
                    borderRadius: 999,
                    border: artifact.isPinned ? '1px solid rgba(59,130,246,0.25)' : '1px solid rgba(148,163,184,0.2)',
                    background: artifact.isPinned ? 'rgba(59,130,246,0.12)' : 'rgba(148,163,184,0.1)',
                    color: artifact.isPinned ? 'var(--text-accent)' : 'var(--text-secondary-strong)',
                    fontWeight: 700,
                    letterSpacing: '0.02em',
                    textTransform: 'uppercase',
                  }}
                >
                  {artifact.isPinned ? 'Pinned' : 'Discovered'}
                </span>
              </div>
              {artifact.discoveryReason && (
                <span style={{ color: 'var(--text-secondary-strong)', fontSize: 11 }}>
                  {artifact.discoveryReason}
                </span>
              )}
              {!artifact.isPinned && overlapScore > 0 && (
                <span style={{ color: 'var(--text-secondary-strong)', fontSize: 11 }}>
                  overlap {overlapScore.toFixed(1)}
                </span>
              )}
              {Array.isArray(artifact.matchedRefs) && artifact.matchedRefs.length > 0 && (
                <span style={{ color: 'var(--text-secondary-strong)', fontSize: 11 }}>
                  matched {artifact.matchedRefs.slice(0, 3).join(', ')}
                </span>
              )}
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
