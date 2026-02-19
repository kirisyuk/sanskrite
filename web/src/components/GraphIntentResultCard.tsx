import React from 'react';
import type { GraphIntentPlanResponse, GraphIntentNodeSpec, GraphIntentEdgeSpec } from '../types/graphIntent';
import {
  HorizonNodeIcon,
  EdgeTypeIcon,
  getNodeColors,
  getHorizonNodeVisualType,
  getEdgeColor,
} from './HorizonGraphNodeTypes';

export interface GraphIntentResultCardProps {
  plan: GraphIntentPlanResponse;
  applyLoading: boolean;
  onApply: () => void;
  onDismiss: () => void;
  theme?: 'light' | 'dark';
}

function collectNodes(plan: GraphIntentPlanResponse): GraphIntentNodeSpec[] {
  const seen = new Set<string>();
  const out: GraphIntentNodeSpec[] = [];
  const add = (n: GraphIntentNodeSpec) => {
    const key = (n.tempId || n.canonicalPathHint || `${n.label}:${n.type}`).trim() || `_${out.length}`;
    if (seen.has(key)) return;
    seen.add(key);
    out.push(n);
  };
  for (const n of plan.create?.nodes ?? []) add(n);
  for (const grp of plan.create?.groupings ?? []) {
    for (const n of grp.entities ?? []) add(n);
  }
  return out;
}

function collectEdges(plan: GraphIntentPlanResponse): GraphIntentEdgeSpec[] {
  const seen = new Set<string>();
  const out: GraphIntentEdgeSpec[] = [];
  const add = (e: GraphIntentEdgeSpec) => {
    const from = (e.fromTempId || e.fromNodeId || '').trim();
    const to = (e.toTempId || e.toNodeId || '').trim();
    const rel = (e.relationType || '').trim();
    const key = `${from}|${to}|${rel}`;
    if (seen.has(key)) return;
    seen.add(key);
    out.push(e);
  };
  for (const e of plan.create?.edges ?? []) add(e);
  for (const grp of plan.create?.groupings ?? []) {
    for (const e of grp.relations ?? []) add(e);
  }
  return out;
}

function buildTempIdToLabel(nodes: GraphIntentNodeSpec[]): Map<string, string> {
  const m = new Map<string, string>();
  for (const n of nodes) {
    const id = n.tempId?.trim();
    const label = (n.label || '').trim() || '?';
    if (id) m.set(id, label);
  }
  return m;
}

export function GraphIntentResultCard({
  plan,
  applyLoading,
  onApply,
  onDismiss,
  theme = 'dark',
}: GraphIntentResultCardProps) {
  const nodes = collectNodes(plan);
  const edges = collectEdges(plan);
  const tempIdToLabel = buildTempIdToLabel(nodes);

  return (
    <div
      style={{
        width: 520,
        maxWidth: '90vw',
        borderRadius: 16,
        border: '1px solid var(--border-subtle)',
        background: theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
        boxShadow: theme === 'dark' ? '0 20px 60px rgba(0,0,0,0.6)' : '0 20px 60px rgba(0,0,0,0.2)',
        overflow: 'hidden',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      {/* Top gradient: transparent → opaque */}
      <div
        style={{
          height: 48,
          background: `linear-gradient(to bottom, transparent, ${theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)'})`,
          flexShrink: 0,
        }}
      />
      {/* Content */}
      <div style={{ padding: '0 20px 16px', flexShrink: 0, maxHeight: 320, overflowY: 'auto' }}>
        {/* Query + summary */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 12,
            marginBottom: 12,
            flexWrap: 'wrap',
          }}
        >
          <span
            style={{
              fontSize: 13,
              fontWeight: 600,
              color: 'var(--text-heading)',
              flex: '1 1 200px',
              minWidth: 0,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {plan.normalizedEnglishQuery}
          </span>
          <span style={{ fontSize: 11, color: 'var(--text-secondary)', flexShrink: 0 }}>
            {[
              nodes.length > 0 && `+${nodes.length} nodes`,
              edges.length > 0 && `+${edges.length} edges`,
              plan.merge?.primaryNodeId && (plan.merge?.duplicateNodeIds?.length ?? 0) > 0 && `merge ${plan.merge!.duplicateNodeIds!.length}→1`,
              (plan.delete?.nodeIds?.length ?? 0) > 0 && `−${plan.delete!.nodeIds!.length} nodes`,
              (plan.delete?.edgeIds?.length ?? 0) > 0 && `−${plan.delete!.edgeIds!.length} edges`,
            ]
              .filter(Boolean)
              .join(', ') || 'No changes'}
          </span>
        </div>

        {/* Chips: nodes by label */}
        {nodes.length > 0 && (
          <div style={{ marginBottom: 10 }}>
            <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-secondary-strong)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: 0.4 }}>
              Nodes
            </div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {nodes.map((n, idx) => {
                const visualType = getHorizonNodeVisualType(n.type, n.canonicalPathHint, n.label);
                const colors = getNodeColors(visualType, theme);
                const label = (n.label || '').trim() || '?';
                return (
                  <div
                    key={`node-${n.tempId ?? idx}`}
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 6,
                      padding: '4px 10px',
                      borderRadius: 999,
                      border: `1px solid ${colors.border}`,
                      background: colors.bg,
                      color: colors.text,
                      fontSize: 12,
                      fontWeight: 600,
                      maxWidth: '100%',
                    }}
                    title={n.canonicalPathHint || label}
                  >
                    <HorizonNodeIcon type={visualType} color={colors.accent} size={12} />
                    <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{label}</span>
                  </div>
                );
              })}
            </div>
          </div>
        )}

        {/* Chips: edges with source → target */}
        {edges.length > 0 && (
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-secondary-strong)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: 0.4 }}>
              Relations
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              {edges.map((e, idx) => {
                const fromId = (e.fromTempId || e.fromNodeId || '').trim();
                const toId = (e.toTempId || e.toNodeId || '').trim();
                const fromLabel = tempIdToLabel.get(fromId) || fromId || '?';
                const toLabel = tempIdToLabel.get(toId) || toId || '?';
                const relType = (e.relationType || 'unknown').trim();
                const color = getEdgeColor(relType);
                const relLabel = relType.replace(/_/g, ' ').toLowerCase().replace(/\b\w/g, (c) => c.toUpperCase());
                return (
                  <div
                    key={`edge-${idx}-${fromId}-${toId}-${relType}`}
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 8,
                      padding: '6px 10px',
                      borderRadius: 8,
                      border: `1px solid ${color}`,
                      background: `${color}12`,
                      fontSize: 12,
                      fontWeight: 500,
                      flexWrap: 'wrap',
                    }}
                  >
                    <span style={{ color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 140 }}>{fromLabel}</span>
                    <span style={{ display: 'flex', alignItems: 'center', gap: 4, color, flexShrink: 0 }}>
                      <EdgeTypeIcon edgeType={relType} color={color} size={12} />
                      <span>{relLabel}</span>
                    </span>
                    <span style={{ color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 140 }}>{toLabel}</span>
                  </div>
                );
              })}
            </div>
          </div>
        )}

        {/* Merge: primary + duplicates */}
        {(plan.merge?.primaryNodeId && (plan.merge?.duplicateNodeIds?.length ?? 0) > 0) && (
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-secondary-strong)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: 0.4 }}>
              Merge
            </div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, alignItems: 'center' }}>
              <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>Primary:</span>
              <div
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  padding: '4px 10px',
                  borderRadius: 999,
                  border: '1px solid rgba(34,197,94,0.5)',
                  background: 'rgba(34,197,94,0.12)',
                  color: 'var(--text-heading)',
                  fontSize: 12,
                  fontWeight: 600,
                }}
              >
                {plan.merge!.primaryNodeId}
              </div>
              <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>←</span>
              {(plan.merge?.duplicateNodeIds ?? []).map((id) => (
                <div
                  key={`dup-${id}`}
                  style={{
                    display: 'inline-flex',
                    padding: '4px 10px',
                    borderRadius: 999,
                    border: '1px solid rgba(234,179,8,0.5)',
                    background: 'rgba(234,179,8,0.12)',
                    color: 'var(--text-heading)',
                    fontSize: 12,
                    fontWeight: 500,
                  }}
                  title={`Duplicate → merge into primary`}
                >
                  {id}
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Delete: nodes and edges to remove */}
        {((plan.delete?.nodeIds?.length ?? 0) > 0 || (plan.delete?.edgeIds?.length ?? 0) > 0) && (
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 10, fontWeight: 700, color: 'var(--text-secondary-strong)', marginBottom: 6, textTransform: 'uppercase', letterSpacing: 0.4 }}>
              Remove
            </div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {(plan.delete?.nodeIds ?? []).map((id) => (
                <div
                  key={`del-node-${id}`}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 6,
                    padding: '4px 10px',
                    borderRadius: 999,
                    border: '1px solid rgba(239,68,68,0.5)',
                    background: 'rgba(239,68,68,0.12)',
                    color: 'var(--text-heading)',
                    fontSize: 12,
                    fontWeight: 500,
                  }}
                  title={`Delete node ${id}`}
                >
                  <span style={{ opacity: 0.8 }}>−</span>
                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 120 }}>{id}</span>
                </div>
              ))}
              {(plan.delete?.edgeIds ?? []).map((id) => (
                <div
                  key={`del-edge-${id}`}
                  style={{
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 6,
                    padding: '4px 10px',
                    borderRadius: 999,
                    border: '1px solid rgba(239,68,68,0.5)',
                    background: 'rgba(239,68,68,0.12)',
                    color: 'var(--text-heading)',
                    fontSize: 12,
                    fontWeight: 500,
                  }}
                  title={`Delete edge ${id}`}
                >
                  <span style={{ opacity: 0.8 }}>−</span>
                  <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 120 }}>{id}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Actions */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
          <button
            type="button"
            onClick={onApply}
            disabled={applyLoading}
            style={{
              padding: '6px 14px',
              borderRadius: 8,
              border: 'none',
              background: 'var(--accent-primary, #3B82F6)',
              color: '#fff',
              fontSize: 13,
              fontWeight: 600,
              cursor: applyLoading ? 'not-allowed' : 'pointer',
              opacity: applyLoading ? 0.7 : 1,
            }}
          >
            {applyLoading ? 'Applying…' : 'Apply'}
          </button>
          <button
            type="button"
            onClick={onDismiss}
            disabled={applyLoading}
            style={{
              padding: '6px 14px',
              borderRadius: 8,
              border: '1px solid var(--border-subtle)',
              background: 'transparent',
              color: 'var(--text-secondary)',
              fontSize: 13,
              fontWeight: 600,
              cursor: applyLoading ? 'not-allowed' : 'pointer',
            }}
          >
            Dismiss
          </button>
        </div>
      </div>
    </div>
  );
}
