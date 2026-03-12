import type { CSSProperties } from 'react';
import type { SharedGraphEdgeMeta } from './types';

export function getGraphViewEdgeStyle(
  edgeType: string | undefined,
  criticality: number | undefined,
  edgeData: Record<string, any> | undefined,
  useSemanticGroupColors: boolean,
): CSSProperties | null {
  const baseWidth = criticality ? Math.max(1, criticality * 2) : 1.5;
  const graphView = String(edgeData?.graphView || '').trim();
  if (!graphView) return null;

  if (graphView === 'supply-flow') {
    const meta = (edgeData as SharedGraphEdgeMeta | undefined)?.supplyFlowMeta;
    const strokeByClass: Record<string, string> = {
      'material-input': '#2563EB',
      'product-transformation': '#0891B2',
      'logistics-leg': '#0F766E',
      'jurisdiction': '#8B5CF6',
      'corporate-context': '#64748B',
      'route-support': '#94A3B8',
    };
    const stroke = strokeByClass[meta?.flowClass || 'route-support'] || '#64748B';
    const dash =
      meta?.variant === 'blocked' ? '3 3' :
      meta?.variant === 'fallback' ? '8 4' :
      meta?.variant === 'uncertain' ? '5 4' :
      undefined;
    return {
      stroke,
      strokeWidth: meta?.flowClass === 'product-transformation' ? Math.max(2, baseWidth + 0.4) : baseWidth,
      ...(dash ? { strokeDasharray: dash } : {}),
    };
  }

  if (graphView === 'influence-network') {
    const meta = (edgeData as SharedGraphEdgeMeta | undefined)?.influenceMeta;
    const strokeByClass: Record<string, string> = {
      'ownership-control': '#2563EB',
      'board-executive': '#4F46E5',
      'political-access': '#DC2626',
      'regulatory-leverage': '#7C3AED',
      'commercial-dependence': '#059669',
      'family-community': '#D97706',
      'influence-general': '#EA580C',
      other: '#64748B',
    };
    const influenceClass = meta?.influenceClass || 'other';
    return {
      stroke: strokeByClass[influenceClass] || '#64748B',
      strokeWidth: influenceClass === 'ownership-control' ? Math.max(2, baseWidth + 0.3) : baseWidth,
      ...(influenceClass === 'board-executive' ? { strokeDasharray: '2 3' } : {}),
      ...(influenceClass === 'family-community' ? { strokeDasharray: '6 4' } : {}),
      ...(influenceClass === 'influence-general' ? { strokeDasharray: '5 3' } : {}),
    };
  }

  if (graphView === 'value-chain') {
    const meta = (edgeData as SharedGraphEdgeMeta | undefined)?.valueChainMeta;
    const strokeByClass: Record<string, string> = {
      'transforms-into': '#0284C7',
      'supplies-input': '#2563EB',
      enables: '#475569',
      'sells-into': '#059669',
      'governance-context': '#7C3AED',
    };
    const valueClass = meta?.valueClass || 'enables';
    return {
      stroke: strokeByClass[valueClass] || '#64748B',
      strokeWidth: valueClass === 'transforms-into' ? Math.max(2, baseWidth + 0.5) : baseWidth,
      ...(valueClass === 'enables' ? { strokeDasharray: '4 3' } : {}),
      ...(valueClass === 'governance-context' ? { strokeDasharray: '6 4' } : {}),
    };
  }

  return useSemanticGroupColors ? null : null;
}
