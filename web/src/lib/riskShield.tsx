import React from 'react';

export type RiskSegmentId =
  | 'geopolitical'
  | 'supply'
  | 'financial'
  | 'cyber'
  | 'esg'
  | 'operational'
  | 'keyperson'
  | 'regulatory';

export type RiskLevel = 'none' | 'low' | 'medium' | 'high' | 'critical';

export interface RiskSegmentSpec {
  id: RiskSegmentId;
  label: string;
  color: string;
  description: string;
}

// Order: from top (12 o'clock) clockwise — matches the polygon.
export const RISK_SEGMENTS: readonly RiskSegmentSpec[] = [
  {
    id: 'geopolitical',
    label: 'Geopolitical',
    color: '#F59E0B',
    description: 'Sanctions, export controls, trade restrictions, jurisdictional exposure.',
  },
  {
    id: 'supply',
    label: 'Supply chain',
    color: '#14B8A6',
    description: 'Dependencies, critical suppliers, single points of failure, concentration.',
  },
  {
    id: 'financial',
    label: 'Financial',
    color: '#10B981',
    description: 'Liquidity/credit pressure proxies, market sentiment and financial fragility.',
  },
  {
    id: 'cyber',
    label: 'Cyber/Tech',
    color: '#3B82F6',
    description: 'Cyber incidents, IT outages, tech dependencies, IP / data exposure.',
  },
  {
    id: 'esg',
    label: 'ESG',
    color: '#84CC16',
    description: 'Environmental/social/governance signals; sustainability & labor risk proxies.',
  },
  {
    id: 'operational',
    label: 'Operational',
    color: '#64748B',
    description: 'Operational health, capacity, reliability, continuity and facility risk.',
  },
  {
    id: 'keyperson',
    label: 'Key person',
    color: '#8B5CF6',
    description: 'Key people exposure: reputation/PEP/adverse media and governance signals.',
  },
  {
    id: 'regulatory',
    label: 'Regulatory',
    color: '#F43F5E',
    description: 'Compliance & enforcement pressure proxies; licensing/regulatory friction.',
  },
] as const;

export const clamp01 = (x: number) => {
  if (!Number.isFinite(x)) return 0;
  if (x < 0) return 0;
  if (x > 1) return 1;
  return x;
};

export const directionalToRisk01 = (x: number) => {
  // Directional metrics are typically [-1..1]. Negative means "worse".
  // Map: -1 -> 1 (max risk), +1 -> 0 (min risk).
  if (!Number.isFinite(x)) return 0;
  if (x < -1) x = -1;
  if (x > 1) x = 1;
  return clamp01((-x + 1) / 2);
};

export const scoreToLevel = (v01: number): RiskLevel => {
  const v = clamp01(v01);
  if (v <= 0.001) return 'none';
  if (v < 0.25) return 'low';
  if (v < 0.5) return 'medium';
  if (v < 0.75) return 'high';
  return 'critical';
};

export const formatRiskLevel = (lvl: RiskLevel) => {
  switch (lvl) {
    case 'none':
      return 'None';
    case 'low':
      return 'Low';
    case 'medium':
      return 'Medium';
    case 'high':
      return 'High';
    case 'critical':
      return 'Critical';
    default:
      return 'Unknown';
  }
};

export const metricValue = (state: Record<string, number> | undefined, baseOrKey: string): number | undefined => {
  const s = state;
  if (!s) return undefined;
  const k = String(baseOrKey || '').trim();
  if (!k) return undefined;
  if (typeof s[k] === 'number') return s[k];
  const riskPath = k.startsWith('/') ? k : `/metric/risk/${k}`;
  if (typeof s[riskPath] === 'number') return s[riskPath];
  const base = k.split('/').filter(Boolean).slice(-1)[0] || k;
  if (typeof s[base] === 'number') return s[base];
  const base2 = riskPath.split('/').filter(Boolean).slice(-1)[0];
  if (base2 && typeof s[base2] === 'number') return s[base2];
  return undefined;
};

const maxDefined = (...vals: Array<number | undefined>) => {
  let m: number | undefined = undefined;
  for (const v of vals) {
    if (typeof v !== 'number' || !Number.isFinite(v)) continue;
    m = typeof m === 'number' ? Math.max(m, v) : v;
  }
  return m;
};

export const computeRiskSegmentScore01 = (state: Record<string, number> | undefined, seg: RiskSegmentId): number => {
  // Prefer derived segment metric if present.
  const segKey = `/metric/risk/risk_segment_${seg}`;
  const segKeyAlt = `risk_segment_${seg}`;
  const fromSeg = metricValue(state, segKey);
  const fromSegAlt = metricValue(state, segKeyAlt);
  if (typeof fromSeg === 'number') return clamp01(fromSeg);
  if (typeof fromSegAlt === 'number') return clamp01(fromSegAlt);

  // Fallback to existing metrics: ONLY direct metrics (no derived transforms, no inversions, no rollups).
  // We choose the closest available single metric per segment; if absent => 0.
  const pickFirst = (...keys: string[]) => {
    for (const k of keys) {
      const v = metricValue(state, k);
      if (typeof v === 'number' && Number.isFinite(v)) return v;
    }
    return undefined;
  };

  switch (seg) {
    case 'geopolitical': {
      // Explicit, unambiguous indicator exists: sanctions exposure.
      const v = pickFirst('sanction_exposure');
      return clamp01(typeof v === 'number' ? v : 0);
    }
    case 'supply': {
      const v = pickFirst('supply_chain_criticality');
      return clamp01(typeof v === 'number' ? v : 0);
    }
    case 'financial': {
      // Closest available proxy today (may be directional in practice; we still use raw value).
      const v = pickFirst('market_sentiment');
      return clamp01(typeof v === 'number' ? v : 0);
    }
    case 'cyber': {
      // No stable direct metric in v1 set yet.
      return 0;
    }
    case 'esg': {
      // Closest direct proxies (take the first available, do not combine).
      const v = pickFirst('water_intensity', 'energy_intensity', 'labor_intensity');
      return clamp01(typeof v === 'number' ? v : 0);
    }
    case 'operational': {
      // Closest direct operational indicators (do not invert health).
      const v = pickFirst('operational_health', 'capacity_utilization');
      return clamp01(typeof v === 'number' ? v : 0);
    }
    case 'keyperson': {
      // Closest available proxy today (may be directional in practice; we still use raw value).
      const v = pickFirst('public_perception');
      return clamp01(typeof v === 'number' ? v : 0);
    }
    case 'regulatory': {
      // Closest direct indicator today: corruption vulnerability (regulatory friction proxy).
      const v = pickFirst('corruption_vulnerability');
      return clamp01(typeof v === 'number' ? v : 0);
    }
    default:
      return 0;
  }
};

export const computeRiskSegments = (state: Record<string, number> | undefined) => {
  return RISK_SEGMENTS.map((s) => {
    const score = computeRiskSegmentScore01(state, s.id);
    const level = scoreToLevel(score);
    return { ...s, score, level };
  });
};

export type RiskSegmentSource =
  | { kind: 'segment_metric'; key: string; value: number }
  | { kind: 'fallback_metric'; key: string; value: number }
  | { kind: 'none'; key: null; value: null };

export const computeRiskSegmentSource = (state: Record<string, number> | undefined, seg: RiskSegmentId): RiskSegmentSource => {
  const segKey = `/metric/risk/risk_segment_${seg}`;
  const segKeyAlt = `risk_segment_${seg}`;
  const fromSeg = metricValue(state, segKey);
  if (typeof fromSeg === 'number' && Number.isFinite(fromSeg)) return { kind: 'segment_metric', key: segKey, value: fromSeg };
  const fromSegAlt = metricValue(state, segKeyAlt);
  if (typeof fromSegAlt === 'number' && Number.isFinite(fromSegAlt)) return { kind: 'segment_metric', key: segKeyAlt, value: fromSegAlt };

  const pickFirst = (...keys: string[]) => {
    for (const k of keys) {
      const v = metricValue(state, k);
      if (typeof v === 'number' && Number.isFinite(v)) return { key: k, value: v };
    }
    return null;
  };

  let chosen: { key: string; value: number } | null = null;
  switch (seg) {
    case 'geopolitical':
      chosen = pickFirst('sanction_exposure');
      break;
    case 'supply':
      chosen = pickFirst('supply_chain_criticality');
      break;
    case 'financial':
      chosen = pickFirst('market_sentiment');
      break;
    case 'cyber':
      chosen = null;
      break;
    case 'esg':
      chosen = pickFirst('water_intensity', 'energy_intensity', 'labor_intensity');
      break;
    case 'operational':
      chosen = pickFirst('operational_health', 'capacity_utilization');
      break;
    case 'keyperson':
      chosen = pickFirst('public_perception');
      break;
    case 'regulatory':
      chosen = pickFirst('corruption_vulnerability');
      break;
    default:
      chosen = null;
  }

  if (!chosen) return { kind: 'none', key: null, value: null };
  // Preserve original key formatting for UI display (but also attempt canonical form for readability).
  const canonical = chosen.key.startsWith('/') ? chosen.key : `/metric/risk/${chosen.key}`;
  const isCanonicalPresent = typeof metricValue(state, canonical) === 'number';
  const key = isCanonicalPresent ? canonical : chosen.key;
  return { kind: 'fallback_metric', key, value: chosen.value };
};

export const segmentForMetricBaseKey = (metricBaseKey: string): RiskSegmentId | null => {
  const k = String(metricBaseKey || '').trim().toLowerCase();
  if (!k) return null;
  // Map the commonly displayed signals to segments.
  if (k === 'sanction_exposure' || k === 'geopolitical_alignment') return 'geopolitical';
  if (k === 'supply_chain_criticality') return 'supply';
  if (k === 'market_sentiment') return 'financial';
  if (k === 'public_perception') return 'keyperson';
  if (k === 'operational_health' || k === 'capacity_utilization') return 'operational';
  if (k === 'corruption_vulnerability') return 'regulatory';
  return null;
};

export const RiskSegmentIcon: React.FC<{ id: RiskSegmentId; size?: number; color?: string; opacity?: number; title?: string }> = ({
  id,
  size = 18,
  color = 'currentColor',
  opacity = 1,
  title,
}) => {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke={color}
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      style={{ opacity, display: 'block' }}
    >
      {title ? <title>{title}</title> : null}
      {(() => {
        switch (id) {
          case 'geopolitical':
            return (
              <>
                <circle cx="12" cy="12" r="9" />
                <path d="M3 12h18" />
                <path d="M12 3c3 3 3 15 0 18" />
                <path d="M12 3c-3 3-3 15 0 18" />
              </>
            );
          case 'supply':
            return (
              <>
                <path d="M10.5 13.5l3-3" />
                <path d="M8.2 15.8a4 4 0 0 1 0-5.6l2-2a4 4 0 0 1 5.6 0" />
                <path d="M15.8 8.2a4 4 0 0 1 0 5.6l-2 2a4 4 0 0 1-5.6 0" />
              </>
            );
          case 'financial':
            return (
              <>
                <ellipse cx="12" cy="7" rx="6.5" ry="3" />
                <path d="M5.5 7v4c0 1.7 2.9 3 6.5 3s6.5-1.3 6.5-3V7" />
                <path d="M5.5 11v4c0 1.7 2.9 3 6.5 3s6.5-1.3 6.5-3v-4" />
              </>
            );
          case 'cyber':
            return <path d="M13 2L4 14h7l-1 8 9-12h-7l1-8z" />;
          case 'esg':
            return (
              <>
                <path d="M5 21c10-1 14-8 14-16C11 5 6 10 5 21z" />
                <path d="M9 19c2-4 6-7 10-9" />
              </>
            );
          case 'operational':
            return (
              <>
                <path d="M4 20V10l6 3V10l6 3V6l4 2v12H4z" />
                <path d="M8 20v-3" />
                <path d="M12 20v-5" />
                <path d="M16 20v-2" />
              </>
            );
          case 'keyperson':
            return (
              <>
                <circle cx="12" cy="8.5" r="3.2" />
                <path d="M5.5 20a6.5 6.5 0 0 1 13 0" />
              </>
            );
          case 'regulatory':
            return (
              <>
                <path d="M12 4v16" />
                <path d="M6 7h12" />
                <path d="M7 7l-3 6h6l-3-6z" />
                <path d="M17 7l-3 6h6l-3-6z" />
                <path d="M8 20h8" />
              </>
            );
          default:
            return <circle cx="12" cy="12" r="8" />;
        }
      })()}
    </svg>
  );
};


