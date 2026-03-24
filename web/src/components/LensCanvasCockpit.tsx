import React, { useCallback, useRef } from 'react';
import ReactDOM from 'react-dom';
import ReactFlow, {
  Node,
  Edge,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  ReactFlowInstance,
  MarkerType,
  Position,
  EdgeProps,
  EdgeTypes,
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { ThemeContext } from './ThemeProvider';
import {
  HORIZON_NODE_TYPES,
  setHorizonNodeVisualQuality,
  type HorizonNodeVisualQuality,
  getNodeColors,
  getHorizonNodeVisualType,
} from './HorizonGraphNodeTypes';
import { api, type DirectedSupplyChainExplorationRequest } from '../lib/api';
import { useIsAdmin } from '../hooks/useIsAdmin';
import { useUIStore } from '../stores/uiStore';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {
  useHorizonRealtimeStore,
  type HorizonRealtimeEvent,
} from '../stores/horizonRealtimeStore';
import { getDomainRegionLabel, getScopeGeoLabel, humanizeCanonicalPath, normalizeCanonicalPath } from '../lib/uiUtils';
import { isSphereCanonicalPath, isSphereEntity, isSphereType } from '../lib/horizonSphere';
import {
  layoutExpansionWithGroups,
  layoutFullGraph,
  layoutLinearFullGraph,
  layoutLevelsTwoPane,
  rebuildGroupContainers,
  getSemanticGroup,
  getLinearSection,
  SEMANTIC_GROUP_POSITION,
  SEMANTIC_GROUP_LABELS,
  getEdgePriority,
  createEmptyGrid,
  setGridCell,
  getGridCell,
  findCellForExpandedNode,
  gridToCanvasPosition,
  type GridState,
  type GridCell,
  type SemanticGroup,
} from '../lib/elkLayout';
import {
  GRAPH_VIEW_LABELS,
  defaultGraphViewState,
  graphViewToSemantic,
  type GraphViewHoverMode,
  type GraphViewId,
  type PersistedGraphViewState,
  type SemanticGraphView,
  isHierarchicalGraphView,
  isInfluenceNetworkGraphView,
  isSupplyFlowGraphView,
  isValueChainGraphView,
  normalizeGraphView,
  normalizeGraphViewState,
} from '../lib/graphViews/types';
import {
  arrangeGraphView,
  filterEdgesForGraphView,
  getGraphViewExpansionPolicy,
  getGraphViewFilterPolicy,
  getGraphViewOverlayPolicy,
  getGraphViewToolbarPolicy,
} from '../lib/graphViews/runtime';
import { computeHoverHighlight } from '../lib/graphViews/hover';
import { getGraphViewEdgeStyle } from '../lib/graphViews/rendering';
import type { LensItem } from '../types/lensItem';
import type { HorizonViewDirective } from '../types/horizonView';
import type { HorizonNodeData } from './HorizonGraphNodeTypes';
import {
  GRAPH_INTENT_VERSION_V1,
  type GraphIntentPlanRequest,
  type GraphIntentPlanResponse,
  type GraphIntentApplyRequest,
} from '../types/graphIntent';
import { ExpandEmptyToast } from './ExpandEmptyToast';
import { MarketProfileModal } from './MarketProfileModal';
import { OrgProfileModal } from './OrgProfileModal';
import { GovProfileModal } from './GovProfileModal';
import { DynastyProfileModal } from './DynastyProfileModal';
import { TribeProfileModal } from './TribeProfileModal';
import { ProgramProfileModal } from './ProgramProfileModal';
import { GraphIntentResultCard } from './GraphIntentResultCard';
import { ScoutIntentModal, type ScoutIntentResolution } from './ScoutIntentModal';
import { SupplyChainOverlay, type MaterializedArtifact } from './SupplyChainOverlay';
import { SimulationOverlayPanel } from './SimulationOverlayPanel';
import {
  defaultScoutingModeForCanonicalPath,
  defaultScoutingModeForIntent,
  specialtiesForScoutingMode,
  type ScoutingMode,
} from '../features/horizon/lib/scoutingMode';

const clamp = (v: number, min: number, max: number) => Math.min(max, Math.max(min, v));
const readFacilityKindFromAny = (obj: any): string | undefined => {
  const value = String(
    obj?.facilityKind ||
      obj?.identifiers?.facilityKind ||
      obj?.identifiers?.facility_kind ||
      '',
  ).trim();
  return value || undefined;
};

// Deterministic hash for stable edge routing/label offsets across renders.
const stableHash32 = (input: string): number => {
  let h = 2166136261;
  for (let i = 0; i < input.length; i++) {
    h ^= input.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
};

const edgeLabelOffset = (
  seed: string,
  sourceX: number,
  sourceY: number,
  targetX: number,
  targetY: number,
): { dx: number; dy: number } => {
  const h = stableHash32(seed);
  // Spread labels across a few deterministic lanes so captions don't stack on one point.
  const lane = (h % 5) - 2; // -2..2
  if (lane === 0) return { dx: 0, dy: -4 };
  const mag = 10 + ((h >> 3) % 3) * 4; // 10/14/18
  const vx = targetX - sourceX;
  const vy = targetY - sourceY;
  const len = Math.hypot(vx, vy) || 1;
  const nx = -vy / len;
  const ny = vx / len;
  return {
    dx: nx * lane * mag,
    dy: ny * lane * mag - 4,
  };
};

const CORRIDOR_SYN_NODE_PREFIX = 'corridor:syn:';
const CORRIDOR_SYN_EDGE_PREFIX = 'corridor:synedge:';
const CORRIDOR_LEGACY_NODE_PREFIXES = ['corridor:group:', 'corridor:lane:', 'corridor:node:', 'corridor:more:'] as const;
const CORRIDOR_LEGACY_EDGE_PREFIXES = ['corridor:edge:'] as const;

const hasAnyPrefix = (value: string, prefixes: readonly string[]): boolean => {
  const v = String(value || '').trim();
  if (!v) return false;
  for (const p of prefixes) {
    if (v.startsWith(p)) return true;
  }
  return false;
};

const isLegacyCorridorNodeId = (id: string): boolean => hasAnyPrefix(id, CORRIDOR_LEGACY_NODE_PREFIXES);
const isLegacyCorridorEdgeId = (id: string): boolean => hasAnyPrefix(id, CORRIDOR_LEGACY_EDGE_PREFIXES);
const isCorridorSyntheticNodeId = (id: string): boolean => String(id || '').trim().startsWith(CORRIDOR_SYN_NODE_PREFIX);
const isCorridorSyntheticEdgeId = (id: string): boolean => String(id || '').trim().startsWith(CORRIDOR_SYN_EDGE_PREFIX);
const isCorridorArtifactNodeId = (id: string): boolean => isCorridorSyntheticNodeId(id) || isLegacyCorridorNodeId(id);
const isCorridorArtifactEdgeId = (id: string): boolean => isCorridorSyntheticEdgeId(id) || isLegacyCorridorEdgeId(id);

function resolveDirectedSupplyChainDirection(intent: ScoutIntentResolution | null, rawQuery: string): 'upstream' | 'downstream' | 'both' {
  const domains = Array.isArray(intent?.domains) ? intent.domains : [];
  for (const domain of domains as any[]) {
    const contractType = String(domain?.contractType || '').trim().toLowerCase();
    if (contractType !== 'supply-chain' && contractType !== 'value-chain') continue;
    const queries = Array.isArray(domain?.supplyChainExecutionPlan?.queries) ? domain.supplyChainExecutionPlan.queries : [];
    for (const query of queries) {
      const direction = String(query?.direction || '').trim().toLowerCase();
      if (direction === 'upstream' || direction === 'downstream' || direction === 'both') return direction;
    }
  }

  const q = String(rawQuery || '').trim().toLowerCase();
  const bothHints = ['supply chain', 'value chain', 'цепоч', 'цепочк', 'цепочка поставок', 'value-chain'];
  if (bothHints.some((token) => q.includes(token))) return 'both';
  const upstreamHints = ['upstream', 'supplier', 'suppliers', 'sourcing', 'source', 'sources', 'input', 'inputs', 'raw material', 'raw materials', 'кто поставляет', 'поставщик', 'поставщики', 'сырье', 'сырьё', 'источник', 'источники', 'снабж'];
  if (upstreamHints.some((token) => q.includes(token))) return 'upstream';
  return 'downstream';
}

/**
 * Custom edge that draws an arc (inverted parabola) going UP and over.
 * Used for central-to-central connections in Linear mode.
 */
const ArcTopEdge: React.FC<EdgeProps> = ({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  style = {},
  markerEnd,
  label,
  labelStyle,
  labelShowBg = true,
  labelBgStyle,
  labelBgPadding,
  labelBgBorderRadius,
}) => {
  // Calculate the arc path - inverted parabola going up
  const midX = (sourceX + targetX) / 2;
  const dx = Math.abs(targetX - sourceX);
  // Arc height proportional to distance, but capped
  const arcHeight = Math.min(80, Math.max(40, dx * 0.25));
  const midY = Math.min(sourceY, targetY) - arcHeight;

  // Create a quadratic bezier curve (parabola)
  const path = `M ${sourceX} ${sourceY} Q ${midX} ${midY} ${targetX} ${targetY}`;

  // Label position: at the apex of the arc
  const arcOffset = edgeLabelOffset(String(id || ''), sourceX, sourceY, targetX, targetY);
  const labelX = midX + arcOffset.dx;
  const labelY = midY - 8 + arcOffset.dy; // Slightly above the arc apex

  return (
    <>
      <path
        id={id}
        className="react-flow__edge-path"
        d={path}
        style={style}
        markerEnd={markerEnd}
      />
      {/* Invisible wider path for easier selection */}
      <path
        d={path}
        fill="none"
        strokeWidth={20}
        stroke="transparent"
        className="react-flow__edge-interaction"
      />
      {/* Label */}
      {label && (
        <g transform={`translate(${labelX}, ${labelY})`}>
          {labelShowBg && (
            <rect
              x={-((String(label).length * 4) + (labelBgPadding?.[0] ?? 2))}
              y={-10 - (labelBgPadding?.[1] ?? 2)}
              width={(String(label).length * 8) + (labelBgPadding?.[0] ?? 2) * 2}
              height={16 + (labelBgPadding?.[1] ?? 2) * 2}
              rx={labelBgBorderRadius ?? 2}
              ry={labelBgBorderRadius ?? 2}
              className="react-flow__edge-textbg"
              style={labelBgStyle}
            />
          )}
          <text
            className="react-flow__edge-text"
            y={0}
            dy="0.3em"
            textAnchor="middle"
            style={labelStyle}
          >
            {label}
          </text>
        </g>
      )}
    </>
  );
};

type EdgeExplainAnchor = { left: number; top: number; right: number; bottom: number; width: number; height: number };

type EdgeExplainOpenArgs = {
  edgeId: string;
  edgeType?: string;
  sourceNodeId: string;
  targetNodeId: string;
  anchor: EdgeExplainAnchor;
};

const ExplainEdgeLabel: React.FC<{
  labelX: number;
  labelY: number;
  caption: string;
  theme: 'light' | 'dark';
  onExplain?: (args: EdgeExplainOpenArgs) => void;
  onHoverEnter?: (edgeId: string) => void;
  onHoverLeave?: (edgeId: string) => void;
  edgeId: string;
  edgeType?: string;
  sourceNodeId: string;
  targetNodeId: string;
}> = ({ labelX, labelY, caption, theme, onExplain, onHoverEnter, onHoverLeave, edgeId, edgeType, sourceNodeId, targetNodeId }) => {
  const labelText = theme === 'dark' ? 'rgba(255,255,255,0.92)' : 'rgba(15,23,42,0.88)';
  const labelBg = theme === 'dark' ? 'rgba(0,0,0,0.55)' : 'rgba(255,255,255,0.86)';
  const labelBorder = theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)';

  return (
    <EdgeLabelRenderer>
      <div
        style={{
          position: 'absolute',
          transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)`,
          pointerEvents: 'auto',
          zIndex: 20,
        }}
        onMouseEnter={() => onHoverEnter?.(edgeId)}
        onMouseLeave={() => onHoverLeave?.(edgeId)}
      >
        <div
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 8,
            padding: '4px 8px',
            borderRadius: 10,
            background: labelBg,
            border: `1px solid ${labelBorder}`,
            color: labelText,
            fontSize: 11,
            fontWeight: 700,
            letterSpacing: '0.2px',
            boxShadow: theme === 'dark' ? '0 8px 20px rgba(0,0,0,0.35)' : '0 8px 20px rgba(0,0,0,0.12)',
          }}
        >
          <span style={{ pointerEvents: 'none' }}>{caption}</span>
          {typeof onExplain === 'function' && (
            <button
              type="button"
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                const el = e.currentTarget as HTMLElement;
                const r = el.getBoundingClientRect();
                onExplain({
                  edgeId,
                  edgeType,
                  sourceNodeId,
                  targetNodeId,
                  anchor: {
                    left: r.left,
                    top: r.top,
                    right: r.right,
                    bottom: r.bottom,
                    width: r.width,
                    height: r.height,
                  },
                });
              }}
              title="Explain relationship"
              aria-label="Explain relationship"
              style={{
                pointerEvents: 'auto',
                width: 18,
                height: 18,
                borderRadius: 999,
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.22)' : 'rgba(15,23,42,0.18)'}`,
                background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.75)',
                color: theme === 'dark' ? 'rgba(255,255,255,0.9)' : 'rgba(15,23,42,0.85)',
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                fontSize: 11,
                fontWeight: 900,
                cursor: 'pointer',
              }}
            >
              i
            </button>
          )}
        </div>
      </div>
    </EdgeLabelRenderer>
  );
};

// Custom edge with HTML label (caption + explain button) for modern 2026 UX.
const HznEdge: React.FC<EdgeProps> = ({
  id,
  source,
  target,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  style = {},
  markerEnd,
  data,
}) => {
  const { theme } = React.useContext(ThemeContext);
  // IMPORTANT: pass source/target positions, otherwise getBezierPath defaults to Bottom/Top and curves can look
  // like they connect from the wrong side (even if the anchor point is correct).
  const routeSeed = `${String(id || '')}|${String(source || '')}|${String(target || '')}|${String((data as any)?.edgeType || '')}`;
  const routeHash = stableHash32(routeSeed);
  const curvature = clamp(0.25 + (((routeHash % 5) - 2) * 0.06), 0.12, 0.48);
  const [edgePath, baseLabelX, baseLabelY] = getBezierPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    curvature,
  });
  const d: any = data || {};
  const showLabel = !!d.showLabel;
  const caption = String(d.edgeCaption || d.edgeLabel || '').trim();
  const onExplain = d.onExplain as undefined | ((args: EdgeExplainOpenArgs) => void);
  const onLabelHoverEnter = d.onLabelHoverEnter as undefined | ((edgeId: string) => void);
  const onLabelHoverLeave = d.onLabelHoverLeave as undefined | ((edgeId: string) => void);
  const raiseToFront = !!d.raiseToFront;
  const lblOffset = edgeLabelOffset(routeSeed, sourceX, sourceY, targetX, targetY);
  const labelX = baseLabelX + lblOffset.dx;
  const labelY = baseLabelY + lblOffset.dy;

  return (
    <>
      <BaseEdge path={edgePath} style={style} markerEnd={markerEnd} />
      {/* Wider invisible path for easier selection */}
      <path d={edgePath} fill="none" strokeWidth={20} stroke="transparent" className="react-flow__edge-interaction" />
      {/* Front-layer highlight: draw the focused relationship above nodes to avoid caption ambiguity */}
      {raiseToFront && (
        <EdgeLabelRenderer>
          <svg
            style={{
              position: 'absolute',
              left: 0,
              top: 0,
              width: 1,
              height: 1,
              overflow: 'visible',
              pointerEvents: 'none',
              zIndex: 15,
            }}
          >
            <path
              d={edgePath}
              fill="none"
              stroke={String((style as any)?.stroke || (theme === 'dark' ? 'rgba(255,255,255,0.65)' : 'rgba(15,23,42,0.65)'))}
              strokeWidth={Math.max(3, Number((style as any)?.strokeWidth || 1.5) + 1.5)}
              strokeOpacity={0.92}
            />
          </svg>
        </EdgeLabelRenderer>
      )}
      {showLabel && caption && (
        <ExplainEdgeLabel
          labelX={labelX}
          labelY={labelY}
          caption={caption}
          theme={theme === 'dark' ? 'dark' : 'light'}
          onExplain={onExplain}
          onHoverEnter={onLabelHoverEnter}
          onHoverLeave={onLabelHoverLeave}
          edgeId={id}
          edgeType={String(d.edgeType || '').trim() || undefined}
          sourceNodeId={String(source)}
          targetNodeId={String(target)}
        />
      )}
    </>
  );
};

// Same as ArcTopEdge, but with HTML label (so we can host the explain button).
const HznArcTopEdge: React.FC<EdgeProps> = ({
  id,
  source,
  target,
  sourceX,
  sourceY,
  targetX,
  targetY,
  style = {},
  markerEnd,
  data,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const midX = (sourceX + targetX) / 2;
  const dx = Math.abs(targetX - sourceX);
  const arcHeight = Math.min(80, Math.max(40, dx * 0.25));
  const midY = Math.min(sourceY, targetY) - arcHeight;
  const path = `M ${sourceX} ${sourceY} Q ${midX} ${midY} ${targetX} ${targetY}`;
  const d: any = data || {};
  const showLabel = !!d.showLabel;
  const caption = String(d.edgeCaption || d.edgeLabel || '').trim();
  const onExplain = d.onExplain as undefined | ((args: EdgeExplainOpenArgs) => void);
  const onLabelHoverEnter = d.onLabelHoverEnter as undefined | ((edgeId: string) => void);
  const onLabelHoverLeave = d.onLabelHoverLeave as undefined | ((edgeId: string) => void);
  const raiseToFront = !!d.raiseToFront;
  const routeSeed = `${String(id || '')}|${String(source || '')}|${String(target || '')}|${String(d.edgeType || '')}|arc`;
  const lblOffset = edgeLabelOffset(routeSeed, sourceX, sourceY, targetX, targetY);
  const labelX = midX + lblOffset.dx;
  const labelY = midY - 8 + lblOffset.dy;

  return (
    <>
      <BaseEdge path={path} style={style} markerEnd={markerEnd} />
      <path d={path} fill="none" strokeWidth={20} stroke="transparent" className="react-flow__edge-interaction" />
      {raiseToFront && (
        <EdgeLabelRenderer>
          <svg
            style={{
              position: 'absolute',
              left: 0,
              top: 0,
              width: 1,
              height: 1,
              overflow: 'visible',
              pointerEvents: 'none',
              zIndex: 15,
            }}
          >
            <path
              d={path}
              fill="none"
              stroke={String((style as any)?.stroke || (theme === 'dark' ? 'rgba(255,255,255,0.65)' : 'rgba(15,23,42,0.65)'))}
              strokeWidth={Math.max(3, Number((style as any)?.strokeWidth || 1.5) + 1.5)}
              strokeOpacity={0.92}
            />
          </svg>
        </EdgeLabelRenderer>
      )}
      {showLabel && caption && (
        <ExplainEdgeLabel
          labelX={labelX}
          labelY={labelY}
          caption={caption}
          theme={theme === 'dark' ? 'dark' : 'light'}
          onExplain={onExplain}
          onHoverEnter={onLabelHoverEnter}
          onHoverLeave={onLabelHoverLeave}
          edgeId={id}
          edgeType={String(d.edgeType || '').trim() || undefined}
          sourceNodeId={String(source)}
          targetNodeId={String(target)}
        />
      )}
    </>
  );
};

// Custom edge types for the graph
const CUSTOM_EDGE_TYPES: EdgeTypes = {
  arcTop: ArcTopEdge,
  hzn: HznEdge,
  hznArcTop: HznArcTopEdge,
};

const PHYSICAL_AXES_12: string[] = [
  'legal/access',
  'legal/compliance',
  'financial/settlement',
  'financial/credit',
  'physical_flow/supply',
  'physical_flow/demand',
  'logistics/capacity',
  'logistics/latency',
  'operational/continuity',
  'info/uncertainty',
  'info/reputation',
  'geo/conflict',
];

type ModelerScenarioPreset = {
  id: string;
  label: string;
  description: string;
  scenarioId: string;
  contractType: string;
  domainModes: string[];
  engineId: string;
  parameters: Record<string, any>;
};

const MODELER_SCENARIO_PRESETS: ModelerScenarioPreset[] = [
  {
    id: 'vat_kz_baseline_12_to_16',
    label: 'Scenario 1: VAT 12 -> 16',
    description: 'Baseline macro response with moderate elasticity/compliance assumptions.',
    scenarioId: 'vat_kz_baseline_12_to_16',
    contractType: 'policy-impact',
    domainModes: ['policy_impact'],
    engineId: 'macro_economical_v1',
    parameters: {
      location_path: '/loc/kz/na/na',
      vat_rate_from: 12.0,
      vat_rate_to: 16.0,
      baseline_gdp_bln_kzt: 135000.0,
      baseline_vat_revenue_bln_kzt: 4200.0,
      pass_through_rate: 0.75,
      demand_elasticity: -0.35,
      compliance_elasticity: 0.14,
      shadow_sensitivity: 0.18,
      price_basket_share: 0.45,
      wage_indexation_rate: 0.30,
      investment_sensitivity: 0.45,
    },
  },
  {
    id: 'vat_kz_stress_12_to_16',
    label: 'Scenario 2: VAT stress',
    description: 'Stronger pass-through and shadow-economy reaction under weaker compliance.',
    scenarioId: 'vat_kz_stress_12_to_16',
    contractType: 'policy-impact',
    domainModes: ['policy_impact'],
    engineId: 'macro_economical_v1',
    parameters: {
      location_path: '/loc/kz/na/na',
      vat_rate_from: 12.0,
      vat_rate_to: 16.0,
      baseline_gdp_bln_kzt: 135000.0,
      baseline_vat_revenue_bln_kzt: 4200.0,
      pass_through_rate: 0.88,
      demand_elasticity: -0.55,
      compliance_elasticity: 0.24,
      shadow_sensitivity: 0.34,
      price_basket_share: 0.50,
      wage_indexation_rate: 0.15,
      investment_sensitivity: 0.65,
    },
  },
];

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

export interface LensCanvasCockpitProps {
  lensId: string | null;
  projectId?: string | null;
  onNodeSelect?: (nodeId: string | null, nodeData: HorizonNodeData | null) => void;
  onEdgeSelect?: (edgeId: string | null, edgeData: any | null) => void;
  onScopeScout?: (input: {
    fragmentId: string;
    canonicalPath?: string;
    maxRecursionDepth?: number;
    mode?: ScoutingMode;
    specialties?: string[];
    exploration?: DirectedSupplyChainExplorationRequest;
  }) => Promise<void>;
  inspectorActive?: boolean;
  onToggleInspector?: () => void;
  /** QA answer highlight — nodes in impact chain (plan §10.6 X-Ray). */
  qaHighlightNodeIds?: string[];
  /** QA answer highlight — edges in trace paths (plan §10.6 X-Ray). */
  qaHighlightEdgeIds?: string[];
}

export const LensCanvasCockpit: React.FC<LensCanvasCockpitProps> = ({
  lensId,
  projectId,
  onNodeSelect,
  onEdgeSelect,
  onScopeScout,
  inspectorActive,
  onToggleInspector,
  qaHighlightNodeIds,
  qaHighlightEdgeIds,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const isAdmin = useIsAdmin();
  const DEV = import.meta.env.DEV;
  const dbg = React.useCallback((...args: any[]) => {
    if (DEV) console.log(...args);
  }, [DEV]);
  const edgePolicyWarnedPairsRef = React.useRef<Set<string>>(new Set());

  const getEdgeStroke = React.useCallback((style: any): string => {
    const s = style?.stroke;
    if (typeof s === 'string' && s.trim()) return s.trim();
    return theme === 'dark' ? 'rgba(226,232,240,0.55)' : 'rgba(71,85,105,0.55)';
  }, [theme]);

  const getEdgeCaptionShort = React.useCallback((raw: any): string | undefined => {
    const t = String(raw || '').trim();
    if (!t) return undefined;
    const u = t.toUpperCase();
    const MAP: Record<string, string> = {
      // Strategic overlay
      SURFACES: 'Surfaces',
      CONTROLS_ACCESS: 'Controls',
      IMPOSES_CONDITION: 'Condition',
      GATEKEEPS_DECISION: 'Gatekeeps',
      USES_MECHANISM: 'Mechanism',
      APPLIES_TO: 'Applies',
      CORRIDOR_AUTHORITY: 'Authority',
      CORRIDOR_DECISION: 'Decision',
      CORRIDOR_MECHANISM: 'Mechanism',
      CORRIDOR_IMPACT: 'Impact',
      CORRIDOR_MEDIATOR: 'Mediator',
      // Dependency / usage
      DEPENDS_ON: 'Depends',
      REQUIRES: 'Requires',
      NEEDS: 'Needs',
      USES: 'Uses',
      CONSUMES: 'Consumes',
      EXTRACTS_FROM: 'Extracts',
      // Ownership / control
      OWNS: 'Owns',
      MANAGES: 'Manages',
      CONTROLS: 'Controls',
      INVESTS_IN: 'Invests',
      PARENT_OF: 'Parent',
      SUBSIDIARY_OF: 'Subsidiary',
      EMPLOYED_BY: 'Employed',
      BOARD_MEMBER_OF: 'Board',
      FOUNDED: 'Founded',
      // Cooperation / relationships
      PARTNERS_WITH: 'Partners',
      COOPERATES_WITH: 'Cooperates',
      SUPPORTS: 'Supports',
      SUPPLIES_TO: 'Supplies',
      PRODUCES: 'Produces',
      // Location / context
      LOCATED_IN: 'Located',
      OPERATES_IN: 'Operates',
      BASED_IN: 'Based',
      HEADQUARTERED_IN: 'HQ',
      // Competition / conflict
      COMPETES_WITH: 'Competes',
      CONFLICTS_WITH: 'Conflicts',
      OPPOSES: 'Opposes',
      INFLUENCES: 'Influences',
      AFFECTS: 'Affects',
      REGULATES: 'Regulates',
      IMPACTS: 'Impacts',
      // Market / hierarchy
      PARTICIPATES_IN: 'Participates',
      TRADED_IN: 'Traded In',
      DOMINATES: 'Dominates',
      HAS_MARKET_SEGMENT: 'Segment',
      HAS_SUBMARKET: 'Segment', // deprecated, backward compat for existing edges
      HAS_SUBGROUP: 'Subgroup',
      ADMINISTERS: 'Administers',
      // Community relationships
      OPERATES_VIA: 'Via',
      MEMBER_OF: 'Member',
      AFFILIATED_WITH: 'Affiliated',
      // Person relationships
      ASSURES: 'Assures',
      AUDITED_BY: 'Audited',
      RELATED_TO: 'Related',
      RELATIVE_OF: 'Relatives',
    };
    if (MAP[u]) return MAP[u];
    const human = u
      .replace(/_WITH$/g, '')
      .replace(/_TO$/g, '')
      .replace(/_OF$/g, '')
      .replace(/_/g, ' ')
      .toLowerCase()
      .replace(/\b\w/g, (c) => c.toUpperCase());
    return human.length > 18 ? `${human.slice(0, 18)}…` : human;
  }, []);
  // Guard: ReactFlow can sometimes fire onPaneClick after node/edge click (sometimes twice).
  // We ignore pane clicks for a short window after selecting to avoid clearing selection immediately.
  const ignorePaneClicksUntilRef = React.useRef<number>(0);
  const reactFlowInstanceRef = useRef<ReactFlowInstance | null>(null);
  
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);

  // Keep latest canonical nodes/edges in refs for async handlers.
  // IMPORTANT: do NOT use ReactFlow instance `getNodes/getEdges` as a source of truth for persistence,
  // because it reflects rendered nodes/edges (e.g. hover-derived opacity) and can "leak" transient styles into state.
  const nodesStateRef = React.useRef<Node<HorizonNodeData>[]>([]);
  const edgesStateRef = React.useRef<Edge[]>([]);
  const expandedStackKeysRef = React.useRef<Set<string>>(new Set());
  React.useEffect(() => {
    nodesStateRef.current = nodes as any;
  }, [nodes]);
  React.useEffect(() => {
    edgesStateRef.current = edges as any;
  }, [edges]);
  
  const [loading, setLoading] = React.useState(false);
  const [arranging, setArranging] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [lensItems, setLensItems] = React.useState<LensItem[]>([]);
  const [loadingFragmentId, setLoadingFragmentId] = React.useState<string | null>(null);
  const [selectedEdgeId, setSelectedEdgeId] = React.useState<string | null>(null);
  const [expandEmptyToast, setExpandEmptyToast] = React.useState(false);
  const [expandEmptyContext, setExpandEmptyContext] = React.useState<{
    fragmentId: string;
    canonicalPath?: string;
    isFullNetwork: boolean;
  } | null>(null);
  const [expandModeGlow, setExpandModeGlow] = React.useState(false);
  const [scopeScoutModal, setScopeScoutModal] = React.useState<{
    fragmentId: string;
    canonicalPath?: string;
    label?: string;
  } | null>(null);
  const [scopeScoutDepth, setScopeScoutDepth] = React.useState<1 | 2>(1);
  const [scopeScoutMode, setScopeScoutMode] = React.useState<ScoutingMode>('general');
  const [scopeScoutDirection, setScopeScoutDirection] = React.useState<'upstream' | 'downstream'>('downstream');
  const [scopeScoutHotPathOnly, setScopeScoutHotPathOnly] = React.useState(false);
  const [scopeScoutIncludeCapabilities, setScopeScoutIncludeCapabilities] = React.useState(false);
  const [scopeScoutIncludeEdgeMetrics, setScopeScoutIncludeEdgeMetrics] = React.useState(false);
  const [scopeScoutCustomQuery, setScopeScoutCustomQuery] = React.useState('');
  const [scopeScoutInFlightId, setScopeScoutInFlightId] = React.useState<string | null>(null);
  const scopeScoutCancelBtnRef = React.useRef<HTMLButtonElement | null>(null);
  const scopeScoutPrevActiveElRef = React.useRef<HTMLElement | null>(null);
  const [hoveredGroupId, setHoveredGroupId] = React.useState<string | null>(null);
  const [hoveredNodeId, setHoveredNodeId] = React.useState<string | null>(null);
  const [hoveredEdgeId, setHoveredEdgeId] = React.useState<string | null>(null);
  // Fragment Profile (Latest events + Signals panels under node) — single active node at a time.
  const [fragmentProfileActiveNodeId, setFragmentProfileActiveNodeId] = React.useState<string | null>(null);
  const [fragmentProfileEvents, setFragmentProfileEvents] = React.useState<Array<{ id: string; title: string; summary?: string; occurredAt?: string; type?: string }>>([]);
  const [fragmentProfileSignals, setFragmentProfileSignals] = React.useState<Array<{ id: string; title: string; summary?: string; type?: string; observedAt?: string; tags?: string[] }>>([]);
  const [fragmentProfileLoading, setFragmentProfileLoading] = React.useState(false);
  const [fragmentProfileFetchTriggered, setFragmentProfileFetchTriggered] = React.useState(false);
  const fragmentProfilePathRef = React.useRef<string | null>(null);
  const toggleFragmentProfile = React.useCallback((fragmentId: string) => {
    const id = String(fragmentId || '').trim();
    if (!id) return;
    setFragmentProfileActiveNodeId((prev) => (String(prev || '').trim() === id ? null : id));
    setFragmentProfileEvents([]);
    setFragmentProfileSignals([]);
    setFragmentProfileFetchTriggered(false);
  }, []);

  // Load ontology facts (events + signals) when fragment profile opens; if no events, trigger news fetch.
  React.useEffect(() => {
    const fid = String(fragmentProfileActiveNodeId || '').trim();
    if (!fid) return;
    const nodeList = nodesStateRef.current || [];
    const node = nodeList.find((n) => {
      const nid = String((n as any)?.id || '').trim();
      const nfid = String((n.data as any)?.fragmentId || '').trim();
      return nid === fid || nfid === fid;
    }) as any;
    const cp = normalizeCanonicalPath((node?.data as any)?.canonicalPath);
    if (!cp || !cp.startsWith('/')) return;
    fragmentProfilePathRef.current = cp;
    let cancelled = false;
    const run = async () => {
      setFragmentProfileLoading(true);
      try {
        const [eventsRes, signalsRes] = await Promise.all([
          api.getOntologyFactsByCanonicalPath(cp, { kind: 'event', limit: 50 }),
          api.getOntologyFactsByCanonicalPath(cp, { kind: 'signal', limit: 50 }),
        ]);
        if (cancelled) return;
        const eventFacts = eventsRes?.facts || [];
        const signalFacts = signalsRes?.facts || [];
        setFragmentProfileSignals(signalFacts.map((f: any) => ({
          id: f.id,
          title: f.title || '',
          summary: f.summary,
          type: f.type,
          observedAt: f.observedAt,
          tags: f.tags,
        })));
        if (eventFacts.length > 0) {
          setFragmentProfileEvents(eventFacts.map((f: any) => ({
            id: f.id,
            title: f.title || '',
            summary: f.summary,
            occurredAt: f.occurredAt,
            type: f.type,
          })));
          setFragmentProfileFetchTriggered(false);
          setFragmentProfileLoading(false);
        } else {
          setFragmentProfileEvents([]);
          setFragmentProfileFetchTriggered(true);
          try {
            const newsRes = await api.fetchNewsByFragments({
              fragments: [{ canonicalPath: cp }],
              daysBack: 7,
            });
            if (cancelled) return;
            const cpNorm = normalizeCanonicalPath(cp);
            const items = newsRes?.newsByPath?.[cpNorm] || [];
            if (items.length > 0) {
              setFragmentProfileEvents(items.map((it: any) => ({
                id: it.factId || '',
                title: it.title || '',
                summary: it.summary,
                occurredAt: it.occurredAt,
                type: it.eventType,
              })));
            }
            setFragmentProfileFetchTriggered(false);
            setFragmentProfileLoading(false);
          } catch {
            // HTTP failed — fall through; WS listener is a fallback
          }
        }
      } catch {
        if (!cancelled) { setFragmentProfileEvents([]); setFragmentProfileSignals([]); setFragmentProfileLoading(false); }
      }
    };
    void run();
    return () => { cancelled = true; fragmentProfilePathRef.current = null; };
  }, [fragmentProfileActiveNodeId]);

  // Fallback: refetch ontology facts when WS newsByFragments.completed arrives for our path.
  // Primary path uses the HTTP response directly (above). This handles edge cases where
  // the HTTP response was lost but WS arrived, or when facts were created by another tab.
  const lastNewsWsRef = React.useRef<number>(0);
  const latestNewsWsEvent = useHorizonRealtimeStore((s) => {
    const all = s.events || [];
    for (let i = all.length - 1; i >= 0; i--) {
      if (all[i].type === 'horizon.canvas.newsByFragments.completed') return all[i];
    }
    return null;
  });
  React.useEffect(() => {
    if (!latestNewsWsEvent) return;
    const fid = String(fragmentProfileActiveNodeId || '').trim();
    const cp = fragmentProfilePathRef.current;
    if (!fid || !cp || !fragmentProfileFetchTriggered) return;
    const newsByPath = latestNewsWsEvent?.data?.newsByPath;
    const cpNorm = normalizeCanonicalPath(cp);
    if (!newsByPath || !(cpNorm in newsByPath)) return;
    const ts = latestNewsWsEvent.ts || 0;
    if (ts <= lastNewsWsRef.current) return;
    let active = true;
    Promise.all([
      api.getOntologyFactsByCanonicalPath(cp, { kind: 'event', limit: 50 }),
      api.getOntologyFactsByCanonicalPath(cp, { kind: 'signal', limit: 50 }),
    ]).then(([eventsRes, signalsRes]) => {
      if (!active) return;
      lastNewsWsRef.current = ts;
      const eventFacts = eventsRes?.facts || [];
      const signalFacts = signalsRes?.facts || [];
      setFragmentProfileEvents(eventFacts.map((f: any) => ({
        id: f.id,
        title: f.title || '',
        summary: f.summary,
        occurredAt: f.occurredAt,
        type: f.type,
      })));
      setFragmentProfileSignals(signalFacts.map((f: any) => ({
        id: f.id,
        title: f.title || '',
        summary: f.summary,
        type: f.type,
        observedAt: f.observedAt,
        tags: f.tags,
      })));
      setFragmentProfileFetchTriggered(false);
      setFragmentProfileLoading(false);
    }).catch(() => { if (active) setFragmentProfileLoading(false); });
    return () => { active = false; };
  }, [fragmentProfileActiveNodeId, fragmentProfileFetchTriggered, latestNewsWsEvent]);

  // Levels Two-Pane: click on a left group ("aggregated node") should reveal real parent<->child edges.
  const [selectedLevelsAggGroupId, setSelectedLevelsAggGroupId] = React.useState<string | null>(null);
  // When set, we only reveal edges of this type within the selected group (clicked agg edge).
  const [selectedLevelsAggEdgeType, setSelectedLevelsAggEdgeType] = React.useState<string | null>(null);
  // When set (e.g. group↔group proxy edges), we reveal original edges that belong to this aggregation key.
  const [selectedLevelsAggKey, setSelectedLevelsAggKey] = React.useState<string | null>(null);
  const edgeExplainSessionCacheRef = React.useRef<Map<string, { markdown: string; cachedAt: string }>>(new Map());
  const [edgeExplainPopover, setEdgeExplainPopover] = React.useState<{
    open: boolean;
    edgeId: string;
    edgeType?: string;
    sourceNodeId: string;
    targetNodeId: string;
    anchor: EdgeExplainAnchor;
    markdown: string | null;
    cachedAt: string | null;
    loading: boolean;
    error: string | null;
  } | null>(null);
  const edgeExplainPopoverRef = React.useRef<HTMLDivElement | null>(null);
  const nodeExplainSessionCacheRef = React.useRef<Map<string, { markdown: string; cachedAt: string }>>(new Map());
  const [nodeExplainPopover, setNodeExplainPopover] = React.useState<{
    open: boolean;
    nodeId: string;
    fragmentId?: string;
    label?: string;
    canonicalPath?: string;
    nodeType?: string;
    markdown: string | null;
    cachedAt: string | null;
    loading: boolean;
    error: string | null;
  } | null>(null);
  const nodeExplainPopoverRef = React.useRef<HTMLDivElement | null>(null);
  
  // Market profile modal state
  const [marketProfileModal, setMarketProfileModal] = React.useState<{
    open: boolean;
    fragmentId: string;
    canonicalPath?: string;
    label?: string;
  } | null>(null);
  const [orgProfileModal, setOrgProfileModal] = React.useState<{
    open: boolean;
    fragmentId: string;
    canonicalPath?: string;
    label?: string;
  } | null>(null);
  const [govProfileModal, setGovProfileModal] = React.useState<{
    open: boolean;
    fragmentId: string;
    canonicalPath?: string;
    label?: string;
  } | null>(null);
  const [dynastyProfileModal, setDynastyProfileModal] = React.useState<{
    open: boolean;
    fragmentId: string;
    canonicalPath?: string;
    label?: string;
  } | null>(null);
  const [tribeProfileModal, setTribeProfileModal] = React.useState<{
    open: boolean;
    fragmentId: string;
    canonicalPath?: string;
    label?: string;
  } | null>(null);
  const [programProfileModal, setProgramProfileModal] = React.useState<{
    open: boolean;
    fragmentId: string;
    canonicalPath?: string;
    label?: string;
  } | null>(null);
  const [gridState, setGridState] = React.useState<GridState>(createEmptyGrid());

  // Collapsed subcategories state (Levels layout: left-pane groups can collapse relation-type subsections).
  // Key format: `${groupId}:${edgeType}` for toggled state. Defaults are handled in layout logic.
  const [collapsedSubcategories, setCollapsedSubcategories] = React.useState<Set<string>>(new Set());
  // Track whether collapsed subcategories changed (to trigger relayout).
  const collapsedSubcategoriesChangedRef = React.useRef(false);
  // Expanded role groups (Levels layout: left-pane compact categories can expand beyond cap).
  // Key format: `levels:lefttype:${groupId}:${role}`
  const [expandedRoleGroups, setExpandedRoleGroups] = React.useState<Set<string>>(new Set());
  const expandedRoleGroupsChangedRef = React.useRef(false);

  // Right Dock state (Levels layout: right pane filters control which upstream categories are promoted to the right).
  // Per-node: enabled filters among {governance, supply, dependencies, structure}.
  const [rightDockStateByNodeId, setRightDockStateByNodeId] = React.useState<
    Record<string, Array<'governance' | 'supply' | 'dependencies' | 'structure'>>
  >({});
  const rightDockChangedRef = React.useRef(false);
  const pendingDockAutoCollapseNodeIdRef = React.useRef<string | null>(null);

  const toggleRightDock = React.useCallback(
    (nodeId: string, key: 'governance' | 'supply' | 'dependencies' | 'structure', nextEnabled: boolean) => {
      const nid = String(nodeId || '').trim();
      if (!nid) return;
      // Determine whether this node is currently expanded (only then we trigger a relayout).
      const expandedNow = Boolean(
        (nodesStateRef.current || []).some((n) => String((n as any)?.id || '').trim() === nid && Boolean((n as any)?.data?.expanded)),
      );

      // Compute nextOrdered using functional setState to avoid stale closure issues.
      // IMPORTANT: `rightDockStateByNodeId` stores overrides only. When absent, elkLayout falls back to type-based defaults.
      // So when the user toggles OFF a currently-active default filter, we must start from the *effective* set
      // (the one currently rendered on the node), not from the override map (which may be empty).
      let computedNextOrdered: Array<'governance' | 'supply' | 'dependencies' | 'structure'> = [];
      let willBeEmpty = false;
      
      setRightDockStateByNodeId((prev) => {
        const rendered = (nodesStateRef.current || []).find((n) => String((n as any)?.id || '').trim() === nid) as any;
        const renderedEnabled = Array.isArray(rendered?.data?.rightDockEnabled) ? (rendered.data.rightDockEnabled as any[]) : null;
        const cur = renderedEnabled ?? (Array.isArray((prev as any)[nid]) ? ([...(prev as any)[nid]] as any[]) : []);
        const curSet = new Set(cur.map((x) => String(x || '').trim().toLowerCase()).filter(Boolean));
        const k = String(key || '').trim().toLowerCase();
        if (nextEnabled) curSet.add(k);
        else curSet.delete(k);
        // Keep stable ordering
        const ordered = (['governance', 'supply', 'dependencies', 'structure'] as const).filter((x) => curSet.has(x));
        computedNextOrdered = ordered as any;
        willBeEmpty = ordered.length === 0;

        // If user toggled everything off on an EXPANDED node: this is equivalent to collapsing it back.
        // We clear the override so next expansion returns to defaults.
        if (willBeEmpty) {
          const out: any = { ...(prev || {}) };
          delete out[nid];
          return out;
        }
        return { ...(prev || {}), [nid]: ordered as any };
      });
      
      // Set pending collapse AFTER state update is scheduled (will trigger useEffect).
      if (expandedNow && willBeEmpty) {
        pendingDockAutoCollapseNodeIdRef.current = nid;
      }
      // Update UI immediately (without waiting for relayout) so icons "depress" right away.
      // CRITICAL: Preserve expanded flag from expandedNodesRef so collapse button shows correct state.
      setNodes((prevNodes) => {
        return (prevNodes || []).map((n: any) => {
          const id = String(n?.id || '').trim();
          if (id !== nid) return n;
          if (Boolean(n?.data?.isGroup)) return n;
          const fid = String((n.data as any)?.fragmentId || id || '').trim();
          const shouldBeExpanded = fid && expandedNodesRef.current.has(fid);
          return { 
            ...n, 
            data: { 
              ...(n.data || {}), 
              rightDockEnabled: computedNextOrdered,
              expanded: shouldBeExpanded ? true : (n.data as any)?.expanded || false,
            } 
          };
        });
      });

      // Only trigger relayout if this node is actually expanded (otherwise it's just a preset).
      if (expandedNow) rightDockChangedRef.current = true;
    },
    [],
  );

  // Callback to toggle subcategory collapse state (passed to layout and group nodes).
  // Uses an `:expanded` marker to override default-collapsed edge types.
  const toggleSubcategoryCollapse = React.useCallback((groupId: string, edgeType: string, nextCollapsed: boolean, focusNodeId?: string) => {
    const key = `${groupId}:${edgeType}`;
    const expandedKey = `${key}:expanded`;
    const fid = String(focusNodeId || '').trim();
    if (fid) pendingFocusNodeIdRef.current = fid;
    setCollapsedSubcategories((prev) => {
      const next = new Set(prev);
      if (nextCollapsed) {
        // Collapsing: add explicit collapse marker, remove expand marker
        next.add(key);
        next.delete(expandedKey);
      } else {
        // Expanding: remove collapse marker, add explicit expand marker (overrides defaults)
        next.delete(key);
        next.delete(edgeType);
        next.add(expandedKey);
      }
      return next;
    });
    collapsedSubcategoriesChangedRef.current = true;
  }, []);

  const toggleRoleOverflow = React.useCallback((groupKey: string, nextExpanded: boolean) => {
    if (!groupKey) return;
    setExpandedRoleGroups((prev) => {
      const next = new Set(prev);
      if (nextExpanded) next.add(groupKey);
      else next.delete(groupKey);
      return next;
    });
    expandedRoleGroupsChangedRef.current = true;
  }, []);

  // Avoid unbounded growth across long sessions / lens switches.
  // Also reset collapsed subcategories to defaults when lens changes.
  React.useEffect(() => {
    edgeExplainSessionCacheRef.current.clear();
    setEdgeExplainPopover(null);
    nodeExplainSessionCacheRef.current.clear();
    setNodeExplainPopover(null);
    setCollapsedSubcategories(new Set());
    setExpandedRoleGroups(new Set());
    setRightDockStateByNodeId({});
  }, [lensId]);

  // Hover updates can fire very frequently; coalesce to 1 update per frame to avoid re-render storms.
  const hoverRafRef = React.useRef<number | null>(null);
  const pendingHoverRef = React.useRef<{
    hoveredGroupId: string | null;
    hoveredNodeId: string | null;
    hoveredEdgeId: string | null;
  }>({ hoveredGroupId: null, hoveredNodeId: null, hoveredEdgeId: null });

  const scheduleHoverCommit = React.useCallback(() => {
    // Always read the latest value at execution time, not at scheduling time.
    // If RAF is already scheduled, cancel it and schedule a new one to ensure we get the latest state.
    if (hoverRafRef.current !== null) {
      window.cancelAnimationFrame(hoverRafRef.current);
    }
    hoverRafRef.current = window.requestAnimationFrame(() => {
      hoverRafRef.current = null;
      // Read the latest value at execution time to avoid stale state
      const next = { ...pendingHoverRef.current };
      const batched = (ReactDOM as any)?.unstable_batchedUpdates;
      if (typeof batched === 'function') {
        batched(() => {
          setHoveredGroupId(next.hoveredGroupId);
          setHoveredNodeId(next.hoveredNodeId);
          setHoveredEdgeId(next.hoveredEdgeId);
        });
      } else {
        setHoveredGroupId(next.hoveredGroupId);
        setHoveredNodeId(next.hoveredNodeId);
        setHoveredEdgeId(next.hoveredEdgeId);
      }
    });
  }, []);

  const closeEdgeExplainPopover = React.useCallback(() => {
    setEdgeExplainPopover(null);
  }, []);

  const closeNodeExplainPopover = React.useCallback(() => {
    setNodeExplainPopover(null);
  }, []);

  React.useEffect(() => {
    if (!edgeExplainPopover?.open) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeEdgeExplainPopover();
    };
    const onMouseDown = (e: MouseEvent) => {
      const el = edgeExplainPopoverRef.current;
      if (!el) return;
      if (e.target && el.contains(e.target as any)) return;
      closeEdgeExplainPopover();
    };
    window.addEventListener('keydown', onKeyDown, true);
    window.addEventListener('mousedown', onMouseDown, true);
    return () => {
      window.removeEventListener('keydown', onKeyDown, true);
      window.removeEventListener('mousedown', onMouseDown, true);
    };
  }, [edgeExplainPopover?.open, closeEdgeExplainPopover]);

  React.useEffect(() => {
    if (!nodeExplainPopover?.open) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeNodeExplainPopover();
    };
    const onMouseDown = (e: MouseEvent) => {
      const el = nodeExplainPopoverRef.current;
      if (!el) return;
      if (e.target && el.contains(e.target as any)) return;
      closeNodeExplainPopover();
    };
    window.addEventListener('keydown', onKeyDown, true);
    window.addEventListener('mousedown', onMouseDown, true);
    return () => {
      window.removeEventListener('keydown', onKeyDown, true);
      window.removeEventListener('mousedown', onMouseDown, true);
    };
  }, [nodeExplainPopover?.open, closeNodeExplainPopover]);

  const preferredLang = React.useMemo(() => {
    const preferredLanguage =
      (typeof navigator !== 'undefined' && (navigator as any)?.language) ? (navigator as any).language : 'en';
    const short = (String(preferredLanguage || 'en').split('-')[0] || 'en').toLowerCase();
    return short || 'en';
  }, []);

  const generateAndPersistEdgeExplanation = React.useCallback(async (args: {
    edgeId: string;
    edgeType?: string;
    sourceNodeId: string;
    targetNodeId: string;
    lang: string;
    force?: boolean;
  }): Promise<{ markdown: string; cachedAt: string | null }> => {
    const lang = args.lang || 'en';
    const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || [];
    const nodeById = new Map<string, Node<HorizonNodeData>>();
    for (const n of currentNodes) nodeById.set(n.id, n);

    const sourceNode = nodeById.get(args.sourceNodeId);
    const targetNode = nodeById.get(args.targetNodeId);
    const sd: any = sourceNode?.data;
    const td: any = targetNode?.data;
    const sourceFragmentId = String(sd?.fragmentId || '').trim();
    const targetFragmentId = String(td?.fragmentId || '').trim();

    const payload = {
      preferredLanguage: lang,
      edge: {
        id: args.edgeId,
        edgeType: String(args.edgeType || '').trim() || undefined,
      },
      source: {
        nodeId: args.sourceNodeId,
        fragmentId: sourceFragmentId || null,
        label: sd?.label || null,
        canonicalPath: sd?.canonicalPath || null,
        type: sd?.type || null,
      },
      target: {
        nodeId: args.targetNodeId,
        fragmentId: targetFragmentId || null,
        label: td?.label || null,
        canonicalPath: td?.canonicalPath || null,
        type: td?.type || null,
      },
    };

    const prompt =
      `Explain why this graph relationship exists.\n` +
      `- Output language: ${lang}\n` +
      `- Use Google Search grounding.\n` +
      `- Output: Markdown.\n` +
      `- Keep it concise (3-6 sentences). You may use bullet points.\n` +
      `- If you are unsure, say so explicitly.\n\n` +
      `Graph edge context (JSON):\n` +
      `${JSON.stringify(payload, null, 2)}\n`;

    const resp: any = await api.genaiComplete({
      prompt,
      options: {
        profile: 'fast',
        modelOverride: 'gemini-3-flash-preview',
        groundingEnabled: true,
        temperature: 0.2,
        maxOutputTokens: 2048,
        systemInstruction:
          'You are a careful analyst. Explain the relationship in plain language. Do not invent facts. Prefer grounded statements. Output Markdown.',
      },
    });

    const md = String(resp?.content || '').trim();
    if (!md) throw new Error('Empty response from model');

    // Persist best-effort
    try {
      await api.upsertHorizonAIExplanation(
        'edge',
        args.edgeId,
        {
          markdown: md,
          language: lang,
          lensId: lensId || undefined,
          edgeType: String(args.edgeType || '').trim() || undefined,
          sourceFragmentId: sourceFragmentId || undefined,
          targetFragmentId: targetFragmentId || undefined,
        },
        { lang },
      );
    } catch {
      // ignore
    }

    return { markdown: md, cachedAt: new Date().toISOString() };
  }, [lensId, nodesStateRef, preferredLang]);

  const loadEdgeExplanation = React.useCallback(async (args: {
    edgeId: string;
    edgeType?: string;
    sourceNodeId: string;
    targetNodeId: string;
    force?: boolean;
  }) => {
    const lang = preferredLang;
    const cacheKey = `edge:${args.edgeId}:${lang}`;

    // Session cache (prevents repeated LLM calls if backend is temporarily failing).
    if (!args.force) {
      const cached = edgeExplainSessionCacheRef.current.get(cacheKey);
      if (cached && String(cached.markdown || '').trim()) {
        setEdgeExplainPopover((p) => {
          if (!p || p.edgeId !== args.edgeId) return p;
          return { ...p, markdown: cached.markdown, cachedAt: cached.cachedAt, loading: false, error: null };
        });
        return;
      }
    }

    // Mark loading for this popover (if still open for the same edge).
    setEdgeExplainPopover((p) => {
      if (!p || p.edgeId !== args.edgeId) return p;
      return { ...p, loading: true, error: null };
    });
    // 1) Try cache (unless force)
    if (!args.force) {
      try {
        const cached = await api.getHorizonAIExplanation('edge', args.edgeId, { lang });
        const md = String(cached?.markdown || '').trim();
        if (md) {
          boundedMapSet(
            edgeExplainSessionCacheRef.current,
            cacheKey,
            { markdown: md, cachedAt: String(cached.updatedAt || '') || new Date().toISOString() },
            200,
          );
          setEdgeExplainPopover((p) => {
            if (!p || p.edgeId !== args.edgeId) return p;
            return { ...p, markdown: md, cachedAt: String(cached.updatedAt || ''), loading: false, error: null };
          });
          return;
        }
      } catch (err: any) {
        if (err?.response?.status !== 404) {
          setEdgeExplainPopover((p) => {
            if (!p || p.edgeId !== args.edgeId) return p;
            return { ...p, loading: false, error: err.response?.data?.error || err.message || 'Failed to load explanation' };
          });
          return;
        }
      }
    }

    // 2) Generate
    try {
      const out = await generateAndPersistEdgeExplanation({
        edgeId: args.edgeId,
        edgeType: args.edgeType,
        sourceNodeId: args.sourceNodeId,
        targetNodeId: args.targetNodeId,
        lang,
        force: args.force,
      });
      boundedMapSet(
        edgeExplainSessionCacheRef.current,
        cacheKey,
        { markdown: out.markdown, cachedAt: out.cachedAt || new Date().toISOString() },
        200,
      );
      setEdgeExplainPopover((p) => {
        if (!p || p.edgeId !== args.edgeId) return p;
        return { ...p, markdown: out.markdown, cachedAt: out.cachedAt, loading: false, error: null };
      });
    } catch (err: any) {
      setEdgeExplainPopover((p) => {
        if (!p || p.edgeId !== args.edgeId) return p;
        return { ...p, loading: false, error: err.message || 'Failed to generate explanation' };
      });
    }
  }, [preferredLang, generateAndPersistEdgeExplanation]);

  const openEdgeExplainPopover = React.useCallback((args: EdgeExplainOpenArgs) => {
    setEdgeExplainPopover({
      open: true,
      edgeId: args.edgeId,
      edgeType: args.edgeType,
      sourceNodeId: args.sourceNodeId,
      targetNodeId: args.targetNodeId,
      anchor: args.anchor,
      markdown: null,
      cachedAt: null,
      loading: true,
      error: null,
    });
  }, []);
  // Ref for stable callback in useMemo (avoids re-computing baseVisibleEdges on callback identity change)
  const openEdgeExplainPopoverRef = React.useRef(openEdgeExplainPopover);
  openEdgeExplainPopoverRef.current = openEdgeExplainPopover;

  const generateAndPersistNodeExplanation = React.useCallback(async (args: {
    nodeId: string;
    fragmentId?: string;
    label?: string;
    canonicalPath?: string;
    nodeType?: string;
    lang: string;
    force?: boolean;
  }): Promise<{ markdown: string; cachedAt: string | null }> => {
    const lang = args.lang || 'en';
    const nodeId = String(args.nodeId || '').trim();
    const fragmentId = String(args.fragmentId || '').trim();
    const targetId = fragmentId || nodeId;
    if (!targetId) throw new Error('Missing node ID');

    const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || [];
    const currentEdges: Edge[] = edgesStateRef.current || [];
    const node = currentNodes.find((n) => String(n.id || '').trim() === nodeId)
      || (fragmentId ? currentNodes.find((n) => String((n.data as any)?.fragmentId || '').trim() === fragmentId) : undefined);
    const nd: any = node?.data || {};

    const label = String(args.label ?? nd.label ?? '').trim() || null;
    const canonicalPath = String(args.canonicalPath ?? nd.canonicalPath ?? '').trim() || null;
    const nodeType = String(args.nodeType ?? nd.type ?? '').trim() || null;

    const incidentEdges = currentEdges
      .filter((e: any) => e.source === nodeId || e.target === nodeId)
      .map((e: any) => {
        const neighborId = e.source === nodeId ? e.target : e.source;
        const neighborNode = currentNodes.find((n) => n.id === neighborId);
        return {
          edgeId: e.id,
          edgeType: String((e as any)?.data?.edgeType || e.label || 'UNKNOWN'),
          direction: e.source === nodeId ? 'outgoing' : 'incoming',
          neighborNodeId: neighborId,
          neighborLabel: (neighborNode?.data as any)?.label || null,
          neighborType: (neighborNode?.data as any)?.type || null,
        };
      });

    const neighborNodes = Array.from(
      new Map(
        incidentEdges.map((e) => [String(e.neighborNodeId || '').trim(), {
          nodeId: e.neighborNodeId,
          label: e.neighborLabel || null,
          type: e.neighborType || null,
        }]),
      ).values(),
    );

    const payload = {
      preferredLanguage: lang,
      node: {
        id: nodeId,
        fragmentId: targetId,
        label: label || null,
        canonicalPath: canonicalPath || null,
        type: nodeType || null,
      },
      context: {
        incidentEdges: incidentEdges.slice(0, 12),
        neighborNodes: neighborNodes.slice(0, 12),
      },
    };

    const prompt =
      `Provide a concise corporate profile of this entity for quick user understanding.\n` +
      `- Output language: ${lang}\n` +
      `- Use Google Search grounding.\n` +
      `- Output: Markdown.\n` +
      `- Be concise and factual (5-8 bullet points max).\n` +
      `- If you are unsure, say so explicitly.\n\n` +
      `Entity context (JSON):\n` +
      `${JSON.stringify(payload, null, 2)}\n`;

    const resp: any = await api.genaiComplete({
      prompt,
      options: {
        profile: 'fast',
        modelOverride: 'gemini-3-flash-preview',
        groundingEnabled: true,
        temperature: 0.2,
        maxOutputTokens: 2048,
        systemInstruction:
          'You are a careful analyst. Provide a concise corporate summary of the entity in Markdown. Do not invent facts. Prefer grounded statements.',
      },
    });

    const md = String(resp?.content || '').trim();
    if (!md) throw new Error('Empty response from model');

    // Persist best-effort
    try {
      await api.upsertHorizonAIExplanation(
        'node',
        targetId,
        {
          markdown: md,
          language: lang,
          lensId: lensId || undefined,
          attrs: {
            canonicalPath: canonicalPath || undefined,
            type: nodeType || undefined,
            label: label || undefined,
          },
        },
        { lang },
      );
    } catch {
      // ignore
    }

    return { markdown: md, cachedAt: new Date().toISOString() };
  }, [lensId]);

  const loadNodeExplanation = React.useCallback(async (args: {
    nodeId: string;
    fragmentId?: string;
    label?: string;
    canonicalPath?: string;
    nodeType?: string;
    force?: boolean;
  }) => {
    const lang = preferredLang;
    const targetId = String(args.fragmentId || args.nodeId || '').trim();
    if (!targetId) return;
    const cacheKey = `node:${targetId}:${lang}`;

    if (!args.force) {
      const cached = nodeExplainSessionCacheRef.current.get(cacheKey);
      if (cached && String(cached.markdown || '').trim()) {
        setNodeExplainPopover((p) => {
          if (!p || p.nodeId !== args.nodeId) return p;
          return { ...p, markdown: cached.markdown, cachedAt: cached.cachedAt, loading: false, error: null };
        });
        return;
      }
    }

    setNodeExplainPopover((p) => {
      if (!p || p.nodeId !== args.nodeId) return p;
      return { ...p, loading: true, error: null };
    });

    if (!args.force) {
      try {
        const cached = await api.getHorizonAIExplanation('node', targetId, { lang });
        const md = String(cached?.markdown || '').trim();
        if (md) {
          boundedMapSet(
            nodeExplainSessionCacheRef.current,
            cacheKey,
            { markdown: md, cachedAt: String(cached.updatedAt || '') || new Date().toISOString() },
            200,
          );
          setNodeExplainPopover((p) => {
            if (!p || p.nodeId !== args.nodeId) return p;
            return { ...p, markdown: md, cachedAt: String(cached.updatedAt || ''), loading: false, error: null };
          });
          return;
        }
      } catch (err: any) {
        if (err?.response?.status !== 404) {
          setNodeExplainPopover((p) => {
            if (!p || p.nodeId !== args.nodeId) return p;
            return { ...p, loading: false, error: err.response?.data?.error || err.message || 'Failed to load explanation' };
          });
          return;
        }
      }
    }

    try {
      const out = await generateAndPersistNodeExplanation({
        nodeId: args.nodeId,
        fragmentId: args.fragmentId,
        canonicalPath: args.canonicalPath,
        label: args.label,
        nodeType: args.nodeType,
        lang,
        force: args.force,
      });
      boundedMapSet(
        nodeExplainSessionCacheRef.current,
        cacheKey,
        { markdown: out.markdown, cachedAt: out.cachedAt || new Date().toISOString() },
        200,
      );
      setNodeExplainPopover((p) => {
        if (!p || p.nodeId !== args.nodeId) return p;
        return { ...p, markdown: out.markdown, cachedAt: out.cachedAt, loading: false, error: null };
      });
    } catch (err: any) {
      setNodeExplainPopover((p) => {
        if (!p || p.nodeId !== args.nodeId) return p;
        return { ...p, loading: false, error: err.message || 'Failed to generate explanation' };
      });
    }
  }, [preferredLang, generateAndPersistNodeExplanation]);

  const openNodeExplainPopover = React.useCallback((args: {
    nodeId: string;
    fragmentId?: string;
    label?: string;
    canonicalPath?: string;
    nodeType?: string;
  }) => {
    // For MARKET fragments, open profile modal instead of explanation popover
    const nodeType = String(args.nodeType || '').trim().toUpperCase();
    const canonicalPath = String(args.canonicalPath || '').trim();
    const canonicalPathLower = canonicalPath.toLowerCase();
    const isMarket = nodeType === 'MARKET' || canonicalPath.startsWith('/market/');
    const isOrg = nodeType === 'ORGANIZATION' || canonicalPath.startsWith('/org/');
    const isGov = canonicalPath.startsWith('/gov/');
    
    // Detect COMMUNITY subtypes: dynasty and tribe
    // Path patterns: /community/{geo}/dynasty/{slug} or /community/{geo}/tribe/{slug}
    const isCommunity = nodeType === 'COMMUNITY' || canonicalPathLower.startsWith('/community/');
    let isDynasty = false;
    let isTribe = false;
    if (isCommunity && canonicalPathLower.startsWith('/community/')) {
      const pathParts = canonicalPathLower.replace(/^\//, '').split('/');
      // pathParts: ["community", "{geo}", "{type}", "{slug}"]
      if (pathParts.length >= 3 && pathParts[0] === 'community') {
        const communityType = pathParts[2];
        isDynasty = communityType === 'dynasty';
        isTribe = communityType === 'tribe';
      }
    }
    
    if (isMarket && args.fragmentId) {
      setMarketProfileModal({
        open: true,
        fragmentId: args.fragmentId,
        canonicalPath: args.canonicalPath,
        label: args.label,
      });
      return;
    }
    if (isDynasty && args.fragmentId) {
      setDynastyProfileModal({
        open: true,
        fragmentId: args.fragmentId,
        canonicalPath: args.canonicalPath,
        label: args.label,
      });
      return;
    }
    if (isTribe && args.fragmentId) {
      setTribeProfileModal({
        open: true,
        fragmentId: args.fragmentId,
        canonicalPath: args.canonicalPath,
        label: args.label,
      });
      return;
    }
    // Detect PROGRAM type
    const isProgram = nodeType === 'PROGRAM' || canonicalPathLower.startsWith('/program/');
    if (isProgram && args.fragmentId) {
      setProgramProfileModal({
        open: true,
        fragmentId: args.fragmentId,
        canonicalPath: args.canonicalPath,
        label: args.label,
      });
      return;
    }
    if (isGov && args.fragmentId) {
      setGovProfileModal({
        open: true,
        fragmentId: args.fragmentId,
        canonicalPath: args.canonicalPath,
        label: args.label,
      });
      return;
    }
    if (isOrg && !isGov && args.fragmentId) {
      setOrgProfileModal({
        open: true,
        fragmentId: args.fragmentId,
        canonicalPath: args.canonicalPath,
        label: args.label,
      });
      return;
    }
    
    // For other types, use explanation popover
    setNodeExplainPopover({
      open: true,
      nodeId: String(args.nodeId || '').trim(),
      fragmentId: args.fragmentId,
      label: args.label,
      canonicalPath: args.canonicalPath,
      nodeType: args.nodeType,
      markdown: null,
      cachedAt: null,
      loading: true,
      error: null,
    });
  }, []);
  const openNodeExplainPopoverRef = React.useRef(openNodeExplainPopover);
  openNodeExplainPopoverRef.current = openNodeExplainPopover;

  React.useEffect(() => {
    if (!edgeExplainPopover?.open) return;
    // Load after open (cache-first)
    loadEdgeExplanation({
      edgeId: edgeExplainPopover.edgeId,
      edgeType: edgeExplainPopover.edgeType,
      sourceNodeId: edgeExplainPopover.sourceNodeId,
      targetNodeId: edgeExplainPopover.targetNodeId,
      force: false,
    });
  }, [edgeExplainPopover?.open, edgeExplainPopover?.edgeId, loadEdgeExplanation, edgeExplainPopover?.edgeType, edgeExplainPopover?.sourceNodeId, edgeExplainPopover?.targetNodeId]);

  React.useEffect(() => {
    if (!nodeExplainPopover?.open) return;
    loadNodeExplanation({
      nodeId: nodeExplainPopover.nodeId,
      fragmentId: nodeExplainPopover.fragmentId,
      label: nodeExplainPopover.label,
      canonicalPath: nodeExplainPopover.canonicalPath,
      nodeType: nodeExplainPopover.nodeType,
      force: false,
    });
  }, [nodeExplainPopover?.open, nodeExplainPopover?.nodeId, nodeExplainPopover?.fragmentId, nodeExplainPopover?.label, nodeExplainPopover?.canonicalPath, nodeExplainPopover?.nodeType, loadNodeExplanation]);

  React.useEffect(() => {
    return () => {
      if (hoverRafRef.current !== null) {
        window.cancelAnimationFrame(hoverRafRef.current);
        hoverRafRef.current = null;
      }
    };
  }, []);

  type SignalPropagationOverlay = {
    sourceNodeId: string;
    sourceFragmentId: string;
    nodeIds: string[];
    edgeIds: string[];
    signalRuns: number;
    traceSteps: number;
  };
  const [hoverHighlightMode, setHoverHighlightMode] = React.useState<GraphViewHoverMode>('control');

  // Safety: if hover highlight is disabled, ensure no stale opacity stays on nodes (can happen if a rendered snapshot
  // was persisted earlier by mistake).
  React.useEffect(() => {
    if (hoverHighlightMode !== 'off') return;
    setNodes((prev) => {
      let changed = false;
      const next = (prev as any[]).map((n) => {
        const s: any = (n as any)?.style;
        if (!s || typeof s !== 'object' || !('opacity' in s)) return n;
        const { opacity: _opacity, ...rest } = s;
        changed = true;
        const out: any = { ...(n as any) };
        if (Object.keys(rest).length > 0) out.style = rest;
        else delete out.style;
        return out;
      });
      return changed ? (next as any) : prev;
    });
  }, [hoverHighlightMode, setNodes]);

  // Performance controls (large graphs)
  // Defaults: enable features on small graphs, auto-disable on large graphs (but allow manual override).
  const isLargeGraph = React.useMemo(() => {
    const n = nodes.length;
    const e = edges.length;
    return n >= 160 || e >= 260 || (n + e) >= 420;
  }, [nodes.length, edges.length]);

  const [forcePerfMode, setForcePerfMode] = React.useState<boolean | null>(null);
  const perfMode = forcePerfMode ?? isLargeGraph;

  const [showMiniMap, setShowMiniMap] = React.useState(false);
  const [showBackgroundGrid, setShowBackgroundGrid] = React.useState(false);
  const [layoutMode, setLayoutMode] = React.useState<GraphViewId>('hierarchical');
  const [graphViewState, setGraphViewState] = React.useState<PersistedGraphViewState>(() => normalizeGraphViewState({}));
  // Layout mode helpers - shared across callbacks/effects below.
  const isFreeformLayout = layoutMode === 'freeform';
  const isLinearLayout = layoutMode === 'linear';
  const isLevelsLayout = isHierarchicalGraphView(layoutMode);
  const isSupplyChainLayout = isSupplyFlowGraphView(layoutMode);
  const isInfluenceNetworkLayout = isInfluenceNetworkGraphView(layoutMode);
  const isValueChainLayout = isValueChainGraphView(layoutMode);
  const graphViewFilterPolicy = React.useMemo(() => getGraphViewFilterPolicy(layoutMode), [layoutMode]);
  const graphViewOverlayPolicy = React.useMemo(() => getGraphViewOverlayPolicy(layoutMode), [layoutMode]);
  const graphViewToolbarPolicy = React.useMemo(() => getGraphViewToolbarPolicy(layoutMode), [layoutMode]);
  const currentSemanticGraphView = React.useMemo<SemanticGraphView | null>(() => graphViewToSemantic(layoutMode), [layoutMode]);
  const [showToolbarMenu, setShowToolbarMenu] = React.useState(false);
  const toolbarMenuRef = React.useRef<HTMLDivElement | null>(null);
  // Right-side pushable overlays: actions, view, info
  const [actionsOverlayOpen, setActionsOverlayOpen] = React.useState(false);
  const [simulationOverlayOpen, setSimulationOverlayOpen] = React.useState(false);
  const [viewOverlayOpen, setViewOverlayOpen] = React.useState(false);
  const [infoOverlayOpen, setInfoOverlayOpen] = React.useState(false);
  const [showLayoutMenu, setShowLayoutMenu] = React.useState(false);
  const layoutMenuRef = React.useRef<HTMLDivElement | null>(null);
  const [showHoverModeMenu, setShowHoverModeMenu] = React.useState(false);
  const hoverModeMenuRef = React.useRef<HTMLDivElement | null>(null);
  const [showEdgeViewMenu, setShowEdgeViewMenu] = React.useState(false);
  const edgeViewMenuRef = React.useRef<HTMLDivElement | null>(null);

  // Graph clarity: focus mode (dim non-incident edges when node selected)
  const [focusMode, setFocusMode] = React.useState(false);
  // Graph clarity: semantic layer filter (show only selected relation groups)
  const [edgeViewFilter, setEdgeViewFilter] = React.useState<SemanticGroup[] | 'all'>('all');
  // Graph clarity: hide low-priority (context/geography) edges by default
  const [showSecondaryEdges, setShowSecondaryEdges] = React.useState(false);
  // Graph clarity: 4 colors by semantic group instead of per-edge-type palette
  const [useSemanticGroupColors, setUseSemanticGroupColors] = React.useState(true);
  const graphViewStateLoadedRef = React.useRef(false);

  // Stack preview overlay (Levels): show up to 5 hidden stack members as chips on controller click.
  const stackPreviewControllerIdRef = React.useRef<string | null>(null);
  const pendingFocusNodeIdRef = React.useRef<string | null>(null);
  const pendingFocusNodeIdsRef = React.useRef<Set<string> | null>(null);

  // Levels (Hierarchical) expansion filter (controls which edge types are fetched on expand).
  type LevelsExpandMode = 'all' | 'ownership' | 'expansion' | 'dependencies' | 'context' | 'market' | 'influence' | 'balanced' | 'decision-chain';
  const [levelsExpandMode, setLevelsExpandMode] = React.useState<LevelsExpandMode>('all');
  const [showLevelsExpandMenu, setShowLevelsExpandMenu] = React.useState(false);
  const levelsExpandMenuRef = React.useRef<HTMLDivElement | null>(null);
  const [supplyChainRefreshKey, setSupplyChainRefreshKey] = React.useState(0);

  const levelsExpandLabel = React.useCallback((m: LevelsExpandMode): string => {
    switch (m) {
      case 'ownership':
        return 'Control';
      case 'expansion':
        return 'Expansion';
      case 'dependencies':
        return 'Supply chain';
      case 'context':
        return 'Operating footprint';
      case 'market':
        return 'Competitive landscape';
      case 'influence':
        return 'Key stakeholders';
      case 'balanced':
        return 'Strategic overview';
      case 'decision-chain':
        return 'Decision Chain';
      case 'all':
      default:
        return 'Full network';
    }
  }, []);

  const levelsExpandEdgeTypes = React.useMemo((): string[] => {
    const OWNERSHIP = ['OWNS', 'SUBSIDIARY_OF', 'PARENT_OF', 'MANAGES', 'CONTROLS', 'INVESTS_IN', 'BOARD_MEMBER_OF', 'EXECUTIVE_OF'];
    const EXPANSION = Array.from(new Set([...OWNERSHIP, 'PARTNERS_WITH']));
    const DEPENDENCIES = [
      'SUPPLIES_TO',
      'PRODUCES',
      'TRANSFORMS_INTO',
      'DEPENDS_ON',
      'REQUIRES',
      'NEEDS',
      'USES',
      'CONSUMES',
      'EXTRACTS_FROM',
    ];
    const CONTEXT = ['LOCATED_IN', 'OPERATES_IN', 'BASED_IN', 'HEADQUARTERED_IN', 'REGULATES', 'ASSURES', 'AUDITED_BY', 'EXTRACTS_FROM', 'ADMINISTERS'];
    const MARKET = [
      'COMPETES_WITH',
      'CONFLICTS_WITH',
      'OPPOSES',
      'PARTNERS_WITH',
      'COOPERATES_WITH',
      'RELATED_TO',
      'RELATIVE_OF',
      'SIMILAR_TO',
      'LINKED_TO',
      // Org participates in market (key for market expansion)
      'PARTICIPATES_IN',
      // Product traded / price-discovered in market
      'TRADED_IN',
      // Org dominates market
      'DOMINATES',
      // Market skeleton
      'HAS_MARKET_SEGMENT',
      // Community hierarchy
      'HAS_SUBGROUP',
    ];
    const INFLUENCE = ['INFLUENCES', 'AFFECTS', 'IMPACTS'];

    switch (levelsExpandMode) {
      case 'ownership':
        return OWNERSHIP;
      case 'expansion':
        return EXPANSION;
      case 'dependencies':
        return DEPENDENCIES;
      case 'context':
        return CONTEXT;
      case 'market':
        return MARKET;
      case 'influence':
        return INFLUENCE;
      case 'balanced':
        return [...OWNERSHIP, ...CONTEXT, ...MARKET];
      case 'decision-chain':
        return [];
      case 'all':
      default:
        return [];
    }
  }, [levelsExpandMode]);

  /** Supply Chain layout: edge types for expansion (supply-chain links only). */
  const supplyChainExpandEdgeTypes = React.useMemo((): string[] => [
    'EXTRACTS_FROM',
    'PRODUCES',
    'TRANSFORMS_INTO',
    'SUPPLIES_TO',
    'CONSUMES',
    'DEPENDS_ON',
    'REQUIRES',
    'NEEDS',
    'USES',
    'LOCATED_IN',
    'OPERATES_IN',
    'BASED_IN',
    'HEADQUARTERED_IN',
    'REGULATES',
    'ADMINISTERS',
    'PARTICIPATES_IN',
    'TRADED_IN',
    'DOMINATES',
    'INFLUENCES',
    'AFFECTS',
    'IMPACTS',
  ], []);

  /** Edge types used for expansion in current layout. */
  const expandEdgeTypesForLayout = React.useMemo(
    () => getGraphViewExpansionPolicy(layoutMode, {
      hierarchicalEdgeTypes: levelsExpandEdgeTypes,
      supplyFlowEdgeTypes: supplyChainExpandEdgeTypes,
    }).edgeTypes,
    [layoutMode, levelsExpandEdgeTypes, supplyChainExpandEdgeTypes],
  );

  // Trace (multi-hop exploration that adds nodes/edges to the current Lens).
  type TraceDirection = 'up' | 'down' | 'both';
  type TraceScopeKey = 'ownership' | 'dependencies' | 'context' | 'market' | 'influence' | 'all';
  type TraceDraft = {
    rootId: string;
    rootLabel?: string;
    direction: TraceDirection;
    scopes: Record<TraceScopeKey, boolean>;
    /** Space/comma-separated keywords, matched against canonicalPath/label (AND semantics). */
    keywords: string;
    /** Max hop distance. */
    depth: number;
    /** Safety budgets (soft caps). */
    maxNodes: number;
    maxEdges: number;
  };
  const [traceDraft, setTraceDraft] = React.useState<TraceDraft | null>(null);
  type TraceUndoEntry = {
    runId: string;
    rootId: string;
    nodeIds: string[];
    edgeIds: string[];
    /** Existing nodes that were temporarily modified (e.g. traceBackbone). */
    touched?: Array<{ id: string; prevTraceBackbone?: boolean }>;
    finishedAt: number;
  };
  const [traceUndoStack, setTraceUndoStack] = React.useState<TraceUndoEntry[]>([]);

  type TraceJob = {
    runId: string;
    rootId: string;
    status: 'running' | 'stopped' | 'done' | 'error';
    message?: string;
    depth: number;
    maxDepth: number;
    processed: number;
    queued: number;
    addedNodes: number;
    addedEdges: number;
    startedAt: number;
  };
  const [traceJob, setTraceJob] = React.useState<TraceJob | null>(null);
  // Disallow undo while trace is running (avoids surprise mutations mid-run).
  const canUndoTrace = traceUndoStack.length > 0 && (!traceJob || traceJob.status !== 'running');
  const traceAbortRef = React.useRef<AbortController | null>(null);
  const traceActiveRunIdRef = React.useRef<string | null>(null);
  const traceLastProgressAtRef = React.useRef<number>(0);

  // Visual settings (toolbar modal)
  type VisualPreset = 'max' | 'balanced' | 'minimal' | 'custom';
  const [visualSettingsOpen, setVisualSettingsOpen] = React.useState(false);
  const [visualPreset, setVisualPreset] = React.useState<VisualPreset>('balanced');
  const [showNodeMetrics, setShowNodeMetrics] = React.useState<boolean>(() => {
    try {
      return localStorage.getItem('horizon.showNodeMetrics') === '1';
    } catch {
      return false;
    }
  });
  const [nodeVisualQuality, setNodeVisualQuality] = React.useState<HorizonNodeVisualQuality>(() => {
    try {
      const raw = localStorage.getItem('horizon.nodeVisualQuality');
      return raw === 'max' || raw === 'minimal' ? raw : 'balanced';
    } catch {
      return 'balanced';
    }
  });
  React.useEffect(() => {
    setHorizonNodeVisualQuality(nodeVisualQuality);
    try {
      localStorage.setItem('horizon.nodeVisualQuality', nodeVisualQuality);
    } catch {
      // ignore
    }
  }, [nodeVisualQuality]);
  React.useEffect(() => {
    try {
      localStorage.setItem('horizon.showNodeMetrics', showNodeMetrics ? '1' : '0');
    } catch {
      // ignore
    }
  }, [showNodeMetrics]);

  // Apply node-metrics toggle to all existing fragment nodes (cheap O(N), invoked only on toggle).
  React.useEffect(() => {
    setNodes((prev) => {
      if (!prev || prev.length === 0) return prev;
      let changed = false;
      const next = (prev as any[]).map((n) => {
        const d: any = (n as any)?.data;
        if (!d || d.isGroup) return n;
        if (d.showMetrics === showNodeMetrics) return n;
        changed = true;
        return { ...(n as any), data: { ...d, showMetrics: showNodeMetrics } };
      });
      return changed ? (next as any) : prev;
    });
  }, [showNodeMetrics, setNodes]);

  // Close visual settings on Escape
  React.useEffect(() => {
    if (!visualSettingsOpen) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        setVisualSettingsOpen(false);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [visualSettingsOpen]);

  // Toolbar menu (overflow) interactions
  React.useEffect(() => {
    if (!showToolbarMenu) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        setShowToolbarMenu(false);
      }
    };
    const onMouseDown = (e: MouseEvent) => {
      const el = toolbarMenuRef.current;
      const target = e.target as globalThis.Node | null;
      if (!el || !target) return;
      if (el.contains(target)) return;
      setShowToolbarMenu(false);
    };
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('mousedown', onMouseDown, true);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('mousedown', onMouseDown, true);
    };
  }, [showToolbarMenu]);

  // Layout dropdown interactions
  React.useEffect(() => {
    if (!showLayoutMenu) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        setShowLayoutMenu(false);
      }
    };
    const onMouseDown = (e: MouseEvent) => {
      const el = layoutMenuRef.current;
      const target = e.target as globalThis.Node | null;
      if (!el || !target) return;
      if (el.contains(target)) return;
      setShowLayoutMenu(false);
    };
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('mousedown', onMouseDown, true);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('mousedown', onMouseDown, true);
    };
  }, [showLayoutMenu]);

  // Edge view dropdown interactions
  React.useEffect(() => {
    if (!showEdgeViewMenu) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        setShowEdgeViewMenu(false);
      }
    };
    const onMouseDown = (e: MouseEvent) => {
      const el = edgeViewMenuRef.current;
      const target = e.target as globalThis.Node | null;
      if (!el || !target) return;
      if (el.contains(target)) return;
      setShowEdgeViewMenu(false);
    };
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('mousedown', onMouseDown, true);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('mousedown', onMouseDown, true);
    };
  }, [showEdgeViewMenu]);

  // Hover mode dropdown interactions
  React.useEffect(() => {
    if (!showHoverModeMenu) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        setShowHoverModeMenu(false);
      }
    };
    const onMouseDown = (e: MouseEvent) => {
      const el = hoverModeMenuRef.current;
      const target = e.target as globalThis.Node | null;
      if (!el || !target) return;
      if (el.contains(target)) return;
      setShowHoverModeMenu(false);
    };
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('mousedown', onMouseDown, true);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('mousedown', onMouseDown, true);
    };
  }, [showHoverModeMenu]);

  // Levels expand filter dropdown interactions
  React.useEffect(() => {
    if (!showLevelsExpandMenu) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        setShowLevelsExpandMenu(false);
      }
    };
    const onMouseDown = (e: MouseEvent) => {
      const el = levelsExpandMenuRef.current;
      const target = e.target as globalThis.Node | null;
      if (!el || !target) return;
      if (el.contains(target)) return;
      setShowLevelsExpandMenu(false);
    };
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('mousedown', onMouseDown, true);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('mousedown', onMouseDown, true);
    };
  }, [showLevelsExpandMenu]);

  // Add-node UI (toolbar)
  type HorizonSearchEntity = {
    id: string;
    canonicalPath: string;
    name: string;
    type: string;
    description?: string;
    location?: string;
    sector?: string;
  };
  const [addNodeOpen, setAddNodeOpen] = React.useState(false);
  const [addNodeQuery, setAddNodeQuery] = React.useState('');
  const [addNodeResults, setAddNodeResults] = React.useState<HorizonSearchEntity[]>([]);
  const [addNodeLoading, setAddNodeLoading] = React.useState(false);
  const [addNodeError, setAddNodeError] = React.useState<string | null>(null);
  const [addNodeActiveIndex, setAddNodeActiveIndex] = React.useState(0);
  const [addNodeSearchEnabled, setAddNodeSearchEnabled] = React.useState(true); // Search mode: prefetch + add-node options from graph
  const addNodeInputRef = React.useRef<HTMLInputElement | null>(null);
  const addNodeCacheRef = React.useRef<Map<string, HorizonSearchEntity[]>>(new Map());
  const addNodeAbortRef = React.useRef<AbortController | null>(null);

  const [graphIntentPlan, setGraphIntentPlan] = React.useState<GraphIntentPlanResponse | null>(null);
  const [graphIntentLoading, setGraphIntentLoading] = React.useState(false);
  const [graphIntentError, setGraphIntentError] = React.useState<string | null>(null);
  const [graphIntentApplyLoading, setGraphIntentApplyLoading] = React.useState(false);

  const [scoutIntentModalOpen, setScoutIntentModalOpen] = React.useState(false);
  const [scoutIntentQuery, setScoutIntentQuery] = React.useState('');
  const [scoutIntentIntent, setScoutIntentIntent] = React.useState<ScoutIntentResolution | null>(null);
  const [scoutIntentLoading, setScoutIntentLoading] = React.useState(false);
  const [scoutIntentError, setScoutIntentError] = React.useState<string | null>(null);
  const [scoutProgressLog, setScoutProgressLog] = React.useState<string[]>([]);
  const [scoutRunLoading, setScoutRunLoading] = React.useState(false);
  const [scoutIntentMode, setScoutIntentMode] = React.useState<ScoutingMode>('general');
  const [scoutIntentModeTouched, setScoutIntentModeTouched] = React.useState(false);
  const [scoutIntentHotPathOnly, setScoutIntentHotPathOnly] = React.useState(false);
  const [scoutIntentDirection, setScoutIntentDirection] = React.useState<'upstream' | 'downstream' | 'both'>('downstream');

  const graphIntentNodeIdToLabel = React.useMemo(() => {
    const m: Record<string, string> = {};
    for (const n of nodes) {
      const d = (n?.data as any) || {};
      if ((d as any)?.isGroup) continue;
      const label = String(d?.label || '').trim() || '?';
      const nid = String(n?.id || '').trim();
      const fid = String(d?.fragmentId || '').trim();
      const cp = String(d?.canonicalPath || '').trim();
      if (nid) m[nid] = label;
      if (fid) m[fid] = label;
      if (cp) m[cp] = label;
    }
    return m;
  }, [nodes]);

  const graphSearchResults = React.useMemo(() => {
    if (!addNodeOpen || !addNodeSearchEnabled) return [] as Array<{ node: Node<HorizonNodeData>; label: string; type?: string; canonicalPath?: string; score: number }>;
    const raw = String(addNodeQuery || '').trim().toLowerCase();
    if (!raw) return [];
    const tokens = raw
      .split(/[\s,;]+/g)
      .map((t) => t.trim())
      .filter(Boolean)
      .slice(0, 8);
    if (tokens.length === 0) return [];

    const snapNodes: Node<HorizonNodeData>[] = (nodesStateRef.current || nodes) as any;
    const candidates = snapNodes.filter((n: any) => !((n?.data as any)?.isGroup));

    const scoreOf = (hay: string): number => {
      let s = 0;
      for (const t of tokens) {
        const idx = hay.indexOf(t);
        if (idx < 0) return -1;
        if (idx === 0) s += 4;
        else if (idx <= 3) s += 3;
        else s += 1;
      }
      return s;
    };

    const out: Array<{ node: Node<HorizonNodeData>; label: string; type?: string; canonicalPath?: string; score: number }> = [];
    for (const n of candidates) {
      const d: any = (n as any)?.data || {};
      const label = String(d?.label || '').trim() || String(d?.canonicalPath || '').trim() || String(d?.fragmentId || n.id || '').trim();
      const canonicalPath = String(d?.canonicalPath || '').trim() || undefined;
      const type = String(d?.type || '').trim() || undefined;
      const fragId = String(d?.fragmentId || '').trim();
      const hay = `${label} ${canonicalPath || ''} ${type || ''} ${fragId} ${(n as any)?.id || ''}`.toLowerCase();
      const score = scoreOf(hay);
      if (score < 0) continue;
      out.push({ node: n as any, label, canonicalPath, type, score });
    }
    out.sort((a, b) => b.score - a.score || a.label.localeCompare(b.label));
    return out.slice(0, 10);
  }, [addNodeOpen, addNodeSearchEnabled, addNodeQuery, nodes]);

  const filteredAddNodeResults = React.useMemo(() => {
    if (!addNodeOpen || !addNodeSearchEnabled) return [] as HorizonSearchEntity[];
    const snapNodes: Node<HorizonNodeData>[] = (nodesStateRef.current || nodes) as any;
    const existingPaths = new Set(
      snapNodes
        .filter((n: any) => !((n?.data as any)?.isGroup))
        .map((n: any) => String(n?.data?.canonicalPath || '').trim())
        .filter(Boolean),
    );
    return (addNodeResults || []).filter((r) => {
      const cp = String(r?.canonicalPath || '').trim();
      return !cp || !existingPaths.has(cp);
    });
  }, [addNodeOpen, addNodeSearchEnabled, addNodeResults, nodes]);

  const paletteSelectableItems = React.useMemo(() => {
    const q = String(addNodeQuery || '').trim();
    const intentItem = q.length >= 3 ? [{ kind: 'intent' as const, query: q }] : [];
    if (graphIntentLoading && q.length >= 3) {
      return intentItem;
    }
    const scoutItem = q.length >= 2 ? [{ kind: 'scout' as const, query: q }] : [];
    const graphItems = addNodeSearchEnabled ? graphSearchResults.map((m) => ({ kind: 'graph' as const, node: m.node, label: m.label, canonicalPath: m.canonicalPath, type: m.type })) : [];
    const indexItems = addNodeSearchEnabled ? filteredAddNodeResults.map((r) => ({ kind: 'index' as const, entity: r })) : [];
    return [
      ...intentItem,
      ...scoutItem,
      ...graphItems,
      ...indexItems,
    ];
  }, [addNodeQuery, addNodeSearchEnabled, graphSearchResults, filteredAddNodeResults, graphIntentLoading]);

  const addNodeQueryTrim = String(addNodeQuery || '').trim();

  React.useEffect(() => {
    setAddNodeActiveIndex((i) => Math.min(i, Math.max(0, paletteSelectableItems.length - 1)));
  }, [paletteSelectableItems]);

  // When Search is disabled, abort prefetch and clear add-node results.
  React.useEffect(() => {
    if (addNodeSearchEnabled) return;
    addNodeAbortRef.current?.abort();
    addNodeAbortRef.current = null;
    setAddNodeResults([]);
    setAddNodeLoading(false);
    setAddNodeError(null);
  }, [addNodeSearchEnabled]);

  // If the add-node UI is closed/unmounted, abort in-flight requests and release retained results.
  React.useEffect(() => {
    if (addNodeOpen) return;
    addNodeAbortRef.current?.abort();
    addNodeAbortRef.current = null;
    setAddNodeResults([]);
    setAddNodeLoading(false);
    setAddNodeError(null);
    setAddNodeActiveIndex(0);
  }, [addNodeOpen]);
  React.useEffect(() => {
    return () => {
      addNodeAbortRef.current?.abort();
      addNodeAbortRef.current = null;
    };
  }, []);

  // Ask-Graph UI (FAB + command palette + chips)
  type GraphQueryFinding = { nodeId: string; title: string; eventDate: string; source: string; url: string };
  type GraphQueryChip = {
    id: string; // interactionId (stable for this session)
    question: string;
    answer?: string;
    relevantNodeIds: string[];
    findings: GraphQueryFinding[];
    sources: string[];
    searchQueries: string[];
    createdAt: number;
  };

  type ControlCorridorNode = {
    id: string;
    label: string;
    type?: string;
    canonicalPath?: string;
    lane: string;
    depth: number;
    seed?: boolean;
  };
  type ControlCorridorEdge = {
    id: string;
    from: string;
    to: string;
    type: string;
    stage: string;
    role?: string;
    criticality?: number;
  };
  type ControlCorridorScoredNode = {
    id: string;
    label: string;
    type?: string;
    canonicalPath?: string;
    score?: number;
    coverage?: number;
  };
  type ControlCorridorPathBundle = {
    id: string;
    controllerId: string;
    targetId: string;
    mediatorId?: string;
    nodeIds: string[];
    edgeIds: string[];
    edgeKeys?: string[];
    coverageScore?: number;
    pathType?: string;
    description?: string;
  };
  type ControlCorridorGraph = {
    lensId: string;
    seedFragmentIds: string[];
    nodes: ControlCorridorNode[];
    edges: ControlCorridorEdge[];
    bundles?: {
      controllers?: ControlCorridorScoredNode[];
      decisionPoints?: ControlCorridorScoredNode[];
      mechanisms?: ControlCorridorScoredNode[];
      mediators?: ControlCorridorScoredNode[];
      impactedTargets?: ControlCorridorScoredNode[];
      paths?: ControlCorridorPathBundle[];
      bottlenecks?: {
        nodes?: Array<{ id: string; usageCount?: number; score?: number }>;
        edges?: Array<{ id: string; from: string; to: string; usageCount?: number }>;
      };
    };
    summary?: {
      seedCount?: number;
      authorityCount?: number;
      decisionCount?: number;
      mechanismCount?: number;
      impactCount?: number;
      controllerCount?: number;
      mediatorsCount?: number;
      pathBundleCount?: number;
      totalNodes?: number;
      totalEdges?: number;
      truncated?: boolean;
    };
    warnings?: string[];
  };

  const [askGraphOpen, setAskGraphOpen] = React.useState(false);
  const [askGraphQuery, setAskGraphQuery] = React.useState('');
  const [askGraphLoading, setAskGraphLoading] = React.useState(false);
  const [askGraphError, setAskGraphError] = React.useState<string | null>(null);
  const askGraphInputRef = React.useRef<HTMLInputElement | null>(null);
  const [modelerOpen, setModelerOpen] = React.useState(false);
  const [modelerLoading, setModelerLoading] = React.useState(false);
  const [modelerError, setModelerError] = React.useState<string | null>(null);
  const [modelerPresetId, setModelerPresetId] = React.useState<string>(MODELER_SCENARIO_PRESETS[0]?.id || '');
  const [modelerScenarioId, setModelerScenarioId] = React.useState<string>(MODELER_SCENARIO_PRESETS[0]?.scenarioId || 'policy-impact-scenario');
  const [modelerContractType, setModelerContractType] = React.useState<string>(MODELER_SCENARIO_PRESETS[0]?.contractType || 'policy-impact');
  const [modelerDomainModesInput, setModelerDomainModesInput] = React.useState<string>((MODELER_SCENARIO_PRESETS[0]?.domainModes || ['policy_impact']).join(', '));
  const [modelerEngineId, setModelerEngineId] = React.useState<string>(MODELER_SCENARIO_PRESETS[0]?.engineId || 'macro_economical_v1');
  const [modelerMode, setModelerMode] = React.useState<'preview' | 'apply'>('preview');
  const [modelerTargetPathsInput, setModelerTargetPathsInput] = React.useState<string>('');
  const [modelerAdvancedOpen, setModelerAdvancedOpen] = React.useState(false);
  const [modelerParametersInput, setModelerParametersInput] = React.useState<string>(
    JSON.stringify(MODELER_SCENARIO_PRESETS[0]?.parameters || {}, null, 2),
  );
  const [modelerResult, setModelerResult] = React.useState<Awaited<ReturnType<typeof api.runHorizonScenario>> | null>(null);
  const [graphQueryChips, setGraphQueryChips] = React.useState<GraphQueryChip[]>([]);
  const [graphQueryActiveChipId, setGraphQueryActiveChipId] = React.useState<string | null>(null);
  const [graphQueryHighlightIds, setGraphQueryHighlightIds] = React.useState<Set<string> | null>(null);
  const [signalPropagationOverlay, setSignalPropagationOverlay] = React.useState<SignalPropagationOverlay | null>(null);
  const [signalPropagationLoading, setSignalPropagationLoading] = React.useState(false);
  const [signalPropagationError, setSignalPropagationError] = React.useState<string | null>(null);
  const signalPropagationRequestSeqRef = React.useRef(0);
  const [viewDirective, setViewDirective] = React.useState<HorizonViewDirective | null>(null);
  const [controlCorridorMode, setControlCorridorMode] = React.useState(false);
  const [controlCorridorLoading, setControlCorridorLoading] = React.useState(false);
  const [, setControlCorridorError] = React.useState<string | null>(null);
  const [controlCorridorGraph, setControlCorridorGraph] = React.useState<ControlCorridorGraph | null>(null);
  const [controlCorridorSeedId, setControlCorridorSeedId] = React.useState<string>('');
  const runControlCorridorRef = React.useRef<((seedIdOverride?: string) => Promise<{ graph: ControlCorridorGraph; seedId: string } | null>) | null>(null);
  const controlCorridorRenderLogKeyRef = React.useRef<string>('');
  const controlCorridorRevealKeyRef = React.useRef<string>('');
  // domainOverlayPreview removed — no longer used
  const realtimeEvents = useHorizonRealtimeStore((s) => s.events);

  React.useEffect(() => {
    setControlCorridorMode(false);
    setControlCorridorLoading(false);
    setControlCorridorError(null);
    setControlCorridorGraph(null);
    setControlCorridorSeedId('');
    setGraphIntentPlan(null);
    setGraphIntentError(null);
    setModelerOpen(false);
    setModelerLoading(false);
    setModelerError(null);
    setModelerResult(null);
    setSignalPropagationOverlay(null);
    setSignalPropagationLoading(false);
    setSignalPropagationError(null);
    signalPropagationRequestSeqRef.current += 1;
  }, [lensId]);

  const domainAnswerItems = React.useMemo<any[]>(() => [], []);
  const activeDomainAnswerKey = '';
  const activeDomainAnswer = null as any;
  const domainOverlayPanelMode = 'mini' as 'mini' | 'list' | 'expanded';
  const domainOverlayHighlightEnabled = false;
  const domainOverlayHoverNodeIds: string[] = [];
  const domainOverlayHoverEdgeIds: string[] = [];
  const setDomainOverlayPanelMode = () => {};
  const setActiveDomainAnswerKey = () => {};
  const setDomainOverlayHighlightEnabled = () => {};
  const setDomainOverlayHoverNodeIds = () => {};
  const setDomainOverlayHoverEdgeIds = () => {};
  const clearDomainOverlayHover = React.useCallback(() => {}, []);
  const domainContractEventTone = (ev: any) => 'info' as const;
  const domainContractEventLabel = (ev: any) => 'Domain contract';
  const domainReadinessMeta = () => ({ label: '', tone: 'info' as const });
  const activeDomainOverlayPath = '';
  const domainContractTimeline: any[] = [];
  const domainContractMiniBadge = null;
  const setDomainOverlayHoverFromTarget = () => {};
  const onDomainDependencyClick = () => {};
  const onDomainDependencyHover = () => {};
  const onDomainExplanatoryUnitHover = () => {};
  const handleCreateDomain = async () => {};
  const handleRunDomain = async () => {};
  const domainOverlayGraphIndex = React.useMemo(() => {
    const nodeIdByFragmentId = new Map<string, string>();
    const nodeIdByCanonicalPath = new Map<string, string>();
    const nodeIds = new Set<string>();
    for (const n of nodes) {
      const d: any = (n as any)?.data;
      if (d?.isGroup) continue;
      const nodeId = String((n as any)?.id || '').trim();
      if (!nodeId) continue;
      nodeIds.add(nodeId);
      const fragmentId = String(d?.fragmentId || '').trim();
      if (fragmentId && !nodeIdByFragmentId.has(fragmentId)) nodeIdByFragmentId.set(fragmentId, nodeId);
      const canonicalPath = String(d?.canonicalPath || '').trim();
      if (canonicalPath && !nodeIdByCanonicalPath.has(canonicalPath)) nodeIdByCanonicalPath.set(canonicalPath, nodeId);
    }

    const edgeIds = new Set<string>();
    const edgeIdsByPair = new Map<string, string[]>();
    const edgeIdsByPairAndRelation = new Map<string, string[]>();
    for (const e of edges) {
      const edgeId = String((e as any)?.id || '').trim();
      const sourceId = String((e as any)?.source || '').trim();
      const targetId = String((e as any)?.target || '').trim();
      if (edgeId) edgeIds.add(edgeId);
      if (!edgeId || !sourceId || !targetId) continue;
      const pairKey = `${sourceId}=>${targetId}`;
      const baseBucket = edgeIdsByPair.get(pairKey) || [];
      baseBucket.push(edgeId);
      edgeIdsByPair.set(pairKey, baseBucket);
      const relationType = String((e as any)?.data?.edgeType || (e as any)?.data?.subkind || (e as any)?.label || '')
        .trim()
        .replace(/\s+/g, '_')
        .toUpperCase();
      if (!relationType) continue;
      const relKey = `${pairKey}|${relationType}`;
      const relBucket = edgeIdsByPairAndRelation.get(relKey) || [];
      relBucket.push(edgeId);
      edgeIdsByPairAndRelation.set(relKey, relBucket);
    }

    return {
      nodeIds,
      nodeIdByFragmentId,
      nodeIdByCanonicalPath,
      edgeIds,
      edgeIdsByPair,
      edgeIdsByPairAndRelation,
    };
  }, [nodes, edges]);

  const resolveDomainOverlayNodeId = React.useCallback((fragmentId: string, canonicalPath: string): string => {
    const fid = String(fragmentId || '').trim();
    const cp = String(canonicalPath || '').trim();
    if (fid) {
      const byFragment = domainOverlayGraphIndex.nodeIdByFragmentId.get(fid);
      if (byFragment) return byFragment;
      if (domainOverlayGraphIndex.nodeIds.has(fid)) return fid;
    }
    if (cp) {
      const byPath = domainOverlayGraphIndex.nodeIdByCanonicalPath.get(cp);
      if (byPath) return byPath;
      if (domainOverlayGraphIndex.nodeIds.has(cp)) return cp;
    }
    return '';
  }, [domainOverlayGraphIndex]);

  const resolveDomainOverlayHoverTarget = React.useCallback((target: any): { nodeIds: string[]; edgeIds: string[] } => {
    if (!target || typeof target !== 'object') return { nodeIds: [], edgeIds: [] };

    const normalizeRelation = (v: any): string => String(v || '').trim().replace(/\s+/g, '_').toUpperCase();

    const nodeSet = new Set<string>();
    const edgeSet = new Set<string>();

    const sourceNodeId = resolveDomainOverlayNodeId(
      String(target?.sourceFragmentId || '').trim(),
      String(target?.sourceCanonicalPath || '').trim(),
    );
    const targetNodeId = resolveDomainOverlayNodeId(
      String(target?.targetFragmentId || '').trim(),
      String(target?.targetCanonicalPath || '').trim(),
    );
    if (sourceNodeId) nodeSet.add(sourceNodeId);
    if (targetNodeId) nodeSet.add(targetNodeId);

    const anchorNodeIDs = Array.isArray(target?.anchorNodeIds) ? target.anchorNodeIds : [];
    for (const rawNodeId of anchorNodeIDs) {
      const candidate = String(rawNodeId || '').trim();
      if (!candidate) continue;
      const resolved = resolveDomainOverlayNodeId(candidate, '');
      if (resolved) nodeSet.add(resolved);
      else if (domainOverlayGraphIndex.nodeIds.has(candidate)) nodeSet.add(candidate);
    }

    const candidateEdgeIDs: string[] = [];
    if (target?.edgeId) candidateEdgeIDs.push(String(target.edgeId));
    if (target?.dependencyEdgeId) candidateEdgeIDs.push(String(target.dependencyEdgeId));
    if (Array.isArray(target?.anchorEdgeIds)) {
      for (const id of target.anchorEdgeIds) candidateEdgeIDs.push(String(id || ''));
    }
    for (const rawEdgeId of candidateEdgeIDs) {
      const edgeId = String(rawEdgeId || '').trim();
      if (!edgeId) continue;
      if (domainOverlayGraphIndex.edgeIds.has(edgeId)) edgeSet.add(edgeId);
    }

    const relation = normalizeRelation(target?.relationType);
    const addByPair = (source: string, destination: string) => {
      if (!source || !destination) return;
      const pairKey = `${source}=>${destination}`;
      const byPair = domainOverlayGraphIndex.edgeIdsByPair.get(pairKey) || [];
      for (const edgeId of byPair) edgeSet.add(edgeId);
      if (!relation) return;
      const byRelation = domainOverlayGraphIndex.edgeIdsByPairAndRelation.get(`${pairKey}|${relation}`) || [];
      for (const edgeId of byRelation) edgeSet.add(edgeId);
    };

    if (sourceNodeId && targetNodeId) {
      addByPair(sourceNodeId, targetNodeId);
    }

    const resolvedAnchors = Array.from(nodeSet);
    if (resolvedAnchors.length >= 2) {
      for (let i = 0; i < resolvedAnchors.length; i += 1) {
        for (let j = 0; j < resolvedAnchors.length; j += 1) {
          if (i === j) continue;
          addByPair(resolvedAnchors[i] || '', resolvedAnchors[j] || '');
        }
      }
    }

    return {
      nodeIds: Array.from(nodeSet.values()),
      edgeIds: Array.from(edgeSet.values()),
    };
  }, [domainOverlayGraphIndex, resolveDomainOverlayNodeId]);

  const qaHighlightNodeIdSet = React.useMemo<Set<string> | null>(() => {
    if (!qaHighlightNodeIds || qaHighlightNodeIds.length === 0) return null;
    const out = new Set<string>();
    for (const id of qaHighlightNodeIds) {
      const s = String(id || '').trim();
      if (s) out.add(s);
    }
    return out.size > 0 ? out : null;
  }, [qaHighlightNodeIds]);

  const qaHighlightEdgeIdSet = React.useMemo<Set<string> | null>(() => {
    if (!qaHighlightEdgeIds || qaHighlightEdgeIds.length === 0) return null;
    const out = new Set<string>();
    for (const id of qaHighlightEdgeIds) {
      const s = String(id || '').trim();
      if (s) out.add(s);
    }
    return out.size > 0 ? out : null;
  }, [qaHighlightEdgeIds]);

  const domainOverlayHighlightNodeIds = React.useMemo<Set<string> | null>(() => {
    if (!domainOverlayHighlightEnabled || !activeDomainAnswer) return null;
    const out = new Set<string>();
    for (const id of activeDomainAnswer.highlightedNodeIds || []) {
      const s = String(id || '').trim();
      if (s) out.add(s);
    }
    if (out.size === 0) {
      for (const n of Array.isArray(activeDomainAnswer.raw?.criticalNodes) ? activeDomainAnswer.raw.criticalNodes : []) {
        const id = String(n?.fragmentId || '').trim();
        if (id) out.add(id);
      }
      for (const dep of activeDomainAnswer.criticalDependencies || []) {
        const s = String(dep?.sourceFragmentId || '').trim();
        const t = String(dep?.targetFragmentId || '').trim();
        if (s) out.add(s);
        if (t) out.add(t);
      }
    }
    return out.size > 0 ? out : null;
  }, [activeDomainAnswer, domainOverlayHighlightEnabled]);

  const domainOverlayHighlightEdgeIds = React.useMemo<Set<string> | null>(() => {
    if (!domainOverlayHighlightEnabled || !activeDomainAnswer) return null;
    const out = new Set<string>();
    for (const id of activeDomainAnswer.highlightedEdgeIds || []) {
      const s = String(id || '').trim();
      if (s) out.add(s);
    }
    if (out.size === 0) {
      for (const dep of activeDomainAnswer.criticalDependencies || []) {
        const id = String(dep?.edgeId || '').trim();
        if (id) out.add(id);
      }
      for (const path of activeDomainAnswer.impactPaths || []) {
        const edgeIDs = Array.isArray(path?.edgeIds) ? path.edgeIds : [];
        for (const edgeID of edgeIDs) {
          const id = String(edgeID || '').trim();
          if (id) out.add(id);
        }
      }
    }
    return out.size > 0 ? out : null;
  }, [activeDomainAnswer, domainOverlayHighlightEnabled]);

  const domainOverlayHoverNodeIdSet = React.useMemo<Set<string> | null>(() => {
    if (!domainOverlayHighlightEnabled || !activeDomainAnswer || domainOverlayHoverNodeIds.length === 0) return null;
    const out = new Set<string>();
    for (const id of domainOverlayHoverNodeIds) {
      const nodeId = String(id || '').trim();
      if (nodeId) out.add(nodeId);
    }
    return out.size > 0 ? out : null;
  }, [activeDomainAnswer, domainOverlayHighlightEnabled, domainOverlayHoverNodeIds]);

  const domainOverlayHoverEdgeIdSet = React.useMemo<Set<string> | null>(() => {
    if (!domainOverlayHighlightEnabled || !activeDomainAnswer || domainOverlayHoverEdgeIds.length === 0) return null;
    const out = new Set<string>();
    for (const id of domainOverlayHoverEdgeIds) {
      const edgeId = String(id || '').trim();
      if (edgeId) out.add(edgeId);
    }
    return out.size > 0 ? out : null;
  }, [activeDomainAnswer, domainOverlayHighlightEnabled, domainOverlayHoverEdgeIds]);

  const domainTopDependencies = React.useMemo<any[]>(() => {
    if (!activeDomainAnswer) return [];
    const list = Array.isArray(activeDomainAnswer.criticalDependencies) ? activeDomainAnswer.criticalDependencies : [];
    return list.slice(0, 10);
  }, [activeDomainAnswer]);

  const domainTopDependencySpecs = React.useMemo<
    Array<{
      edgeId: string;
      relationType: string;
      sourceFragmentId: string;
      targetFragmentId: string;
      sourceCanonicalPath: string;
      targetCanonicalPath: string;
      topDependencyScore: number;
    }>
  >(() => {
    if (domainTopDependencies.length === 0) return [];
    return domainTopDependencies
      .map((dep: any) => ({
        edgeId: String(dep?.edgeId || '').trim(),
        relationType: String(dep?.relationType || '').trim(),
        sourceFragmentId: String(dep?.sourceFragmentId || '').trim(),
        targetFragmentId: String(dep?.targetFragmentId || '').trim(),
        sourceCanonicalPath: String(dep?.sourceCanonicalPath || '').trim(),
        targetCanonicalPath: String(dep?.targetCanonicalPath || '').trim(),
        topDependencyScore: Number(dep?.topDependencyScore || 0) || 0,
      }))
      .filter((dep) =>
        Boolean(
          dep.sourceFragmentId ||
            dep.targetFragmentId ||
            dep.sourceCanonicalPath ||
            dep.targetCanonicalPath ||
            dep.edgeId,
        ),
      );
  }, [domainTopDependencies]);

  const domainTopDependencyNodeIds = React.useMemo<Set<string> | null>(() => {
    if (domainTopDependencySpecs.length === 0) return null;
    const nodeIdByFragmentId = new Map<string, string>();
    const nodeIdByCanonicalPath = new Map<string, string>();
    const nodeIds = new Set<string>();
    for (const n of nodes) {
      const d: any = (n as any)?.data;
      if (d?.isGroup) continue;
      const nodeId = String((n as any)?.id || '').trim();
      if (!nodeId) continue;
      nodeIds.add(nodeId);
      const fid = String(d?.fragmentId || '').trim();
      if (fid && !nodeIdByFragmentId.has(fid)) nodeIdByFragmentId.set(fid, nodeId);
      const cp = String(d?.canonicalPath || '').trim();
      if (cp && !nodeIdByCanonicalPath.has(cp)) nodeIdByCanonicalPath.set(cp, nodeId);
    }
    const resolveNodeId = (fragmentId: string, canonicalPath: string): string => {
      const fid = String(fragmentId || '').trim();
      const cp = String(canonicalPath || '').trim();
      if (fid) {
        const byFid = nodeIdByFragmentId.get(fid);
        if (byFid) return byFid;
        if (nodeIds.has(fid)) return fid;
      }
      if (cp) {
        const byPath = nodeIdByCanonicalPath.get(cp);
        if (byPath) return byPath;
      }
      return '';
    };

    const out = new Set<string>();
    for (const dep of domainTopDependencySpecs) {
      const s = resolveNodeId(dep.sourceFragmentId, dep.sourceCanonicalPath);
      const t = resolveNodeId(dep.targetFragmentId, dep.targetCanonicalPath);
      if (s) out.add(s);
      if (t) out.add(t);
    }
    return out.size > 0 ? out : null;
  }, [domainTopDependencySpecs, nodes]);

  const domainTopDependencyMissingNodeRefs = React.useMemo<
    Array<{
      fragmentId: string;
      canonicalPath: string;
      label: string;
      type: string;
    }>
  >(() => {
    if (domainTopDependencySpecs.length === 0) return [];
    const nodeIdByFragmentId = new Map<string, string>();
    const nodeIdByCanonicalPath = new Map<string, string>();
    const nodeIds = new Set<string>();
    for (const n of nodes) {
      const d: any = (n as any)?.data;
      if (d?.isGroup) continue;
      const nodeId = String((n as any)?.id || '').trim();
      if (!nodeId) continue;
      nodeIds.add(nodeId);
      const fid = String(d?.fragmentId || '').trim();
      if (fid && !nodeIdByFragmentId.has(fid)) nodeIdByFragmentId.set(fid, nodeId);
      const cp = String(d?.canonicalPath || '').trim();
      if (cp && !nodeIdByCanonicalPath.has(cp)) nodeIdByCanonicalPath.set(cp, nodeId);
    }
    const resolveNodeId = (fragmentId: string, canonicalPath: string): string => {
      const fid = String(fragmentId || '').trim();
      const cp = String(canonicalPath || '').trim();
      if (fid) {
        const byFid = nodeIdByFragmentId.get(fid);
        if (byFid) return byFid;
        if (nodeIds.has(fid)) return fid;
      }
      if (cp) {
        const byPath = nodeIdByCanonicalPath.get(cp);
        if (byPath) return byPath;
      }
      return '';
    };

    const missing = new Map<
      string,
      {
        fragmentId: string;
        canonicalPath: string;
        label: string;
        type: string;
      }
    >();
    const addMissing = (fragmentId: string, canonicalPath: string) => {
      const fid = String(fragmentId || '').trim();
      const cp = String(canonicalPath || '').trim();
      if (!fid && !cp) return;
      if (resolveNodeId(fid, cp)) return;
      const key = fid || cp;
      if (!key || missing.has(key)) return;
      missing.set(key, {
        fragmentId: fid,
        canonicalPath: cp,
        label: cp ? humanizeCanonicalPath(cp) : (fid ? `Fragment ${fid.slice(0, 8)}` : 'Fragment'),
        type: '',
      });
    };

    for (const dep of domainTopDependencySpecs) {
      addMissing(dep.sourceFragmentId, dep.sourceCanonicalPath);
      addMissing(dep.targetFragmentId, dep.targetCanonicalPath);
    }
    return Array.from(missing.values());
  }, [domainTopDependencySpecs, nodes]);

  const domainTopDependencyEdgeIds = React.useMemo<Set<string> | null>(() => {
    if (domainTopDependencySpecs.length === 0) return null;
    const nodeIdByFragmentId = new Map<string, string>();
    const nodeIdByCanonicalPath = new Map<string, string>();
    const nodeIds = new Set<string>();
    for (const n of nodes) {
      const d: any = (n as any)?.data;
      if (d?.isGroup) continue;
      const nodeId = String((n as any)?.id || '').trim();
      if (!nodeId) continue;
      nodeIds.add(nodeId);
      const fid = String(d?.fragmentId || '').trim();
      if (fid && !nodeIdByFragmentId.has(fid)) nodeIdByFragmentId.set(fid, nodeId);
      const cp = String(d?.canonicalPath || '').trim();
      if (cp && !nodeIdByCanonicalPath.has(cp)) nodeIdByCanonicalPath.set(cp, nodeId);
    }
    const resolveNodeId = (fragmentId: string, canonicalPath: string): string => {
      const fid = String(fragmentId || '').trim();
      const cp = String(canonicalPath || '').trim();
      if (fid) {
        const byFid = nodeIdByFragmentId.get(fid);
        if (byFid) return byFid;
        if (nodeIds.has(fid)) return fid;
      }
      if (cp) {
        const byPath = nodeIdByCanonicalPath.get(cp);
        if (byPath) return byPath;
      }
      return '';
    };

    const out = new Set<string>();
    for (const dep of domainTopDependencySpecs) {
      const id = String(dep.edgeId || '').trim();
      if (id) out.add(id);
    }
    for (const dep of domainTopDependencySpecs) {
      const sourceId = resolveNodeId(dep.sourceFragmentId, dep.sourceCanonicalPath);
      const targetId = resolveNodeId(dep.targetFragmentId, dep.targetCanonicalPath);
      if (!sourceId || !targetId) continue;
      const rel = String(dep.relationType || '').trim().replace(/\s+/g, '_').toUpperCase();
      for (const e of edges) {
        const eid = String((e as any)?.id || '').trim();
        const es = String((e as any)?.source || '').trim();
        const et = String((e as any)?.target || '').trim();
        if (!eid || es !== sourceId || et !== targetId) continue;
        if (rel) {
          const edgeRel = String((e as any)?.data?.edgeType || (e as any)?.data?.subkind || (e as any)?.label || '')
            .trim()
            .replace(/\s+/g, '_')
            .toUpperCase();
          if (edgeRel && edgeRel !== rel) continue;
        }
        out.add(eid);
      }
    }
    return out.size > 0 ? out : null;
  }, [domainTopDependencySpecs, nodes, edges]);

  const domainTopDependencyFocusActive =
    domainOverlayPanelMode === 'expanded' &&
    !!activeDomainAnswer &&
    domainTopDependencies.length > 0;

  // Lens scope preview (empty state)
  const [lensName, setLensName] = React.useState<string | null>(null);
  const [lensScopePaths, setLensScopePaths] = React.useState<string[]>([]);
  const [scopePreview, setScopePreview] = React.useState<Array<{
    canonicalPath: string;
    name?: string;
    type?: string;
    subtitle?: string;
    meta?: string;
  }>>([]);
  const SCOPE_PREVIEW_SECTION_LIMIT = 5;
  const [scopePreviewLoading, setScopePreviewLoading] = React.useState(false);
  const [lensMetaLoading, setLensMetaLoading] = React.useState(false);
  const lensMetaRef = React.useRef<any | null>(null);

  React.useEffect(() => {
    const enabled = isLevelsLayout && levelsExpandMode === 'decision-chain';
    setControlCorridorMode(enabled);
    if (!enabled) {
      setControlCorridorLoading(false);
      setControlCorridorError(null);
      setControlCorridorGraph(null);
      setControlCorridorSeedId('');
    }
  }, [isLevelsLayout, levelsExpandMode]);

  // Prevent mode-switch relayout loops
  const lastAppliedLayoutModeRef = React.useRef<GraphViewId | null>(null);
  const autoArrangeOnceRef = React.useRef<boolean>(false);
  // Proxy ref to avoid TDZ when effects above reference convertLensItemsToGraph (defined later).
  // We set this ref to the real callback after it's initialized.
  const convertLensItemsToGraphRef = React.useRef<
    (items: LensItem[] | null | undefined) => { nodes: Node<HorizonNodeData>[]; edges: Edge[] }
  >(() => ({ nodes: [], edges: [] }));

  // When graph grows large and user didn't override perf mode, auto-disable heavy visuals once.
  React.useEffect(() => {
    if (!isLargeGraph) return;
    if (forcePerfMode !== null) return;
    // defaults are already false, but explicit ensure:
    setShowMiniMap(false);
    setShowBackgroundGrid(false);
  }, [isLargeGraph, forcePerfMode]);

  // If perf mode is enabled, clear hover states that are used only for expensive styling.
  React.useEffect(() => {
    if (!perfMode) return;
    pendingHoverRef.current = { hoveredGroupId: null, hoveredNodeId: null, hoveredEdgeId: null };
    setHoveredGroupId(null);
    setHoveredNodeId(null);
    setHoveredEdgeId(null);
  }, [perfMode]);

  // Sync hover state with actual nodes/edges to prevent "sticky" hover when nodes are removed/changed
  React.useEffect(() => {
    if (perfMode) return;
    const currentHoveredNodeId = hoveredNodeId;
    const currentHoveredGroupId = hoveredGroupId;
    const currentHoveredEdgeId = hoveredEdgeId;
    
    let needsUpdate = false;
    const nextHover: typeof pendingHoverRef.current = { ...pendingHoverRef.current };
    
    // Check if hovered node still exists
    if (currentHoveredNodeId) {
      const nodeExists = nodes.some((n) => String(n.id || '').trim() === currentHoveredNodeId);
      if (!nodeExists) {
        nextHover.hoveredNodeId = null;
        needsUpdate = true;
      }
    }
    
    // Check if hovered group still exists
    if (currentHoveredGroupId) {
      const groupExists = nodes.some((n) => String(n.id || '').trim() === currentHoveredGroupId);
      if (!groupExists) {
        nextHover.hoveredGroupId = null;
        needsUpdate = true;
      }
    }
    
    // Check if hovered edge still exists
    if (currentHoveredEdgeId) {
      const edgeExists = edges.some((e) => String(e.id || '').trim() === currentHoveredEdgeId);
      if (!edgeExists) {
        nextHover.hoveredEdgeId = null;
        needsUpdate = true;
      }
    }
    
    if (needsUpdate) {
      pendingHoverRef.current = nextHover;
      setHoveredGroupId(nextHover.hoveredGroupId);
      setHoveredNodeId(nextHover.hoveredNodeId);
      setHoveredEdgeId(nextHover.hoveredEdgeId);
    }
  }, [nodes, edges, hoveredNodeId, hoveredGroupId, hoveredEdgeId, perfMode]);

  // Set CSS perf-mode attribute for disabling transitions (better paint performance on large graphs)
  React.useEffect(() => {
    if (typeof document === 'undefined') return;
    document.documentElement.dataset.hznPerfMode = perfMode ? 'true' : 'false';
    return () => {
      document.documentElement.dataset.hznPerfMode = 'false';
    };
  }, [perfMode]);

  // Add-node: focus input when opened
  React.useEffect(() => {
    if (!addNodeOpen) return;
    const t = window.setTimeout(() => addNodeInputRef.current?.focus(), 0);
    return () => window.clearTimeout(t);
  }, [addNodeOpen]);

  // Keep active index valid as results change (graph + index combined).
  React.useEffect(() => {
    if (!addNodeOpen) return;
    const max = Math.max(0, paletteSelectableItems.length - 1);
    setAddNodeActiveIndex((i) => Math.min(Math.max(0, i), max));
  }, [addNodeOpen, paletteSelectableItems.length]);

  // Ask-graph: focus input when opened
  React.useEffect(() => {
    if (!askGraphOpen) return;
    const t = window.setTimeout(() => askGraphInputRef.current?.focus(), 0);
    return () => window.clearTimeout(t);
  }, [askGraphOpen]);

  React.useEffect(() => {
    if (!modelerOpen) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      e.preventDefault();
      setModelerOpen(false);
      setModelerLoading(false);
      setModelerError(null);
    };
    window.addEventListener('keydown', onKeyDown, true);
    return () => window.removeEventListener('keydown', onKeyDown, true);
  }, [modelerOpen]);

  // Scout intent modal: resolve intent when opened with query
  React.useEffect(() => {
    if (!scoutIntentModalOpen || !scoutIntentQuery.trim()) return;
    let alive = true;
    setScoutIntentLoading(true);
    setScoutIntentError(null);
    setScoutIntentIntent(null);
    (async () => {
      try {
        const res = await api.resolveHorizonIntent({ query: scoutIntentQuery.trim() });
        if (!alive) return;
        const nextIntent = res?.intent as ScoutIntentResolution;
        setScoutIntentIntent(nextIntent);
        setScoutIntentDirection(resolveDirectedSupplyChainDirection(nextIntent, scoutIntentQuery));
        if (!scoutIntentModeTouched) {
          setScoutIntentMode(defaultScoutingModeForIntent(nextIntent));
        }
        setScoutIntentError(null);
      } catch (e: any) {
        if (!alive) return;
        const msg = e?.response?.data?.error || e?.message || 'Failed to resolve intent';
        setScoutIntentError(String(msg));
        setScoutIntentIntent(null);
      } finally {
        if (alive) setScoutIntentLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [scoutIntentModalOpen, scoutIntentModeTouched, scoutIntentQuery]);

  // (Old domain overlay focus effect removed — replaced by DomainOverlayPanel)

  // Lens scope preview: load lens metadata (name + scope canonical paths)
  React.useEffect(() => {
    if (!lensId) {
      setLensName(null);
      setLensScopePaths([]);
      setScopePreview([]);
      setScopePreviewLoading(false);
      setLensMetaLoading(false);
      return;
    }

    let alive = true;
    (async () => {
      try {
        setLensMetaLoading(true);
        const lens = await api.getHorizonLens(lensId);
        if (!alive) return;
        lensMetaRef.current = lens || null;
        setLensName(typeof lens?.name === 'string' ? lens.name : null);
        const scope = Array.isArray(lens?.scope) ? lens.scope : [];
        const domainPaths = (Array.isArray(lens?.viewConfig?.domains) ? lens.viewConfig.domains : [])
          .map((d: any) => (d?.canonicalPath || '').trim())
          .filter(Boolean);
        const combined: string[] = [];
        scope.forEach((s: any) => {
          const p = String(s || '').trim();
          if (p && !combined.includes(p)) combined.push(p);
        });
        domainPaths.forEach((p: string) => {
          if (p && !combined.includes(p)) combined.push(p);
        });
        setLensScopePaths(combined);
      } catch (e) {
        if (!alive) return;
        // Best-effort: keep empty scope preview if we can't load lens metadata
        lensMetaRef.current = null;
        setLensName(null);
        setLensScopePaths([]);
      } finally {
        if (alive) setLensMetaLoading(false);
      }
    })();

    return () => {
      alive = false;
    };
  }, [lensId]);

  const persistGraphViewTimeoutRef = React.useRef<number | null>(null);
  React.useEffect(() => {
    if (!lensId || !graphViewStateLoadedRef.current) return;
    if (persistGraphViewTimeoutRef.current !== null) {
      window.clearTimeout(persistGraphViewTimeoutRef.current);
      persistGraphViewTimeoutRef.current = null;
    }
    persistGraphViewTimeoutRef.current = window.setTimeout(async () => {
      const lens = lensMetaRef.current;
      if (!lens) return;
      const nextViewConfig = {
        ...(lens.viewConfig || {}),
        graphView: layoutMode,
        layoutMode,
        graphViewState,
      };
      try {
        const updated = await api.updateHorizonLens(lensId, {
          tenantId: lens.tenantId,
          projectId: lens.projectId,
          name: lens.name,
          scope: Array.isArray(lens.scope) ? lens.scope : [],
          viewConfig: nextViewConfig,
          alertConfig: lens.alertConfig,
          status: lens.status,
          scopeFragments: lens.scopeFragments,
          resolvedQueries: lens.resolvedQueries,
          lastResolved: lens.lastResolved,
        }, { skipScouting: true });
        lensMetaRef.current = updated;
      } catch (e) {
        console.warn('[Cockpit] Failed to persist graph view state', e);
      }
    }, 500);
    return () => {
      if (persistGraphViewTimeoutRef.current !== null) {
        window.clearTimeout(persistGraphViewTimeoutRef.current);
        persistGraphViewTimeoutRef.current = null;
      }
    };
  }, [lensId, layoutMode, graphViewState]);

  const toggleScopePin = React.useCallback(
    async (args: { fragmentId?: string; canonicalPath?: string; nextPinned: boolean }) => {
      const cp = String(args?.canonicalPath || '').trim();
      if (!lensId || !cp) return;

      // Optimistic UI update
      setLensScopePaths((prev) => {
        const has = prev.includes(cp);
        if (args.nextPinned) {
          return has ? prev : [...prev, cp];
        }
        return prev.filter((x) => x !== cp);
      });

      try {
        const lens = lensMetaRef.current || (await api.getHorizonLens(lensId));
        if (!lens) return;
        lensMetaRef.current = lens;
        const currentScope: string[] = Array.isArray(lens?.scope) ? lens.scope.map((s: any) => String(s || '').trim()).filter(Boolean) : [];
        const has = currentScope.includes(cp);
        const nextScope = args.nextPinned ? (has ? currentScope : [...currentScope, cp]) : currentScope.filter((x) => x !== cp);

        const updated = await api.updateHorizonLens(lensId, {
          tenantId: lens.tenantId,
          projectId: lens.projectId,
          name: lens.name,
          scope: nextScope,
          viewConfig: lens.viewConfig,
          alertConfig: lens.alertConfig,
          status: lens.status,
          scopeFragments: lens.scopeFragments,
          resolvedQueries: lens.resolvedQueries,
          lastResolved: lens.lastResolved,
        }, { skipScouting: true });
        lensMetaRef.current = updated;
        const cleaned = (Array.isArray(updated?.scope) ? updated.scope : [])
          .map((s: any) => String(s || '').trim())
          .filter(Boolean);
        setLensScopePaths(cleaned);
      } catch (e) {
        // Revert optimistic update on failure (best-effort)
        try {
          const lens = await api.getHorizonLens(lensId);
          lensMetaRef.current = lens || null;
          const scope = Array.isArray(lens?.scope) ? lens.scope : [];
          const cleaned = scope.map((s: any) => String(s || '').trim()).filter(Boolean);
          setLensScopePaths(cleaned);
        } catch {
          // ignore
        }
      }
    },
    [lensId],
  );

  // Sync nodes with current lens scope (pinned state + callback).
  // Extracted into a stable callback so it can be called after initial load.
  const syncPinnedStateRef = React.useRef<() => void>(() => {});
  syncPinnedStateRef.current = React.useCallback(() => {
    if (!lensId) return;
    const pinned = new Set(lensScopePaths.map((p) => String(p || '').trim()).filter(Boolean));
    setNodes((prev) => {
      if (!prev || prev.length === 0) return prev;
      let changed = false;
      const onExplainNode = openNodeExplainPopoverRef.current;
      const next = (prev as any[]).map((n) => {
        const d: any = (n as any)?.data;
        if (!d || d.isGroup) return n;
        const cp = String(d.canonicalPath || '').trim();
        const isPinned = cp ? pinned.has(cp) : false;
        if (d.pinnedToScope === isPinned && d.onTogglePin === toggleScopePin && d.onExplainNode === onExplainNode) return n;
        changed = true;
        return { ...(n as any), data: { ...d, pinnedToScope: isPinned, onTogglePin: toggleScopePin, onExplainNode } };
      });
      return changed ? (next as any) : prev;
    });
  }, [lensId, lensScopePaths, setNodes, toggleScopePin]);

  // React to scope changes (pin/unpin from elsewhere)
  React.useEffect(() => {
    syncPinnedStateRef.current();
    // Also re-run when nodes are loaded/added/removed so newly created nodes receive onTogglePin.
    // This is intentionally limited to nodes.length (not full nodes array) to avoid re-running on style-only updates.
  }, [lensId, lensScopePaths, toggleScopePin, nodes.length]);

  // Lens scope preview: resolve canonical paths to (name/type) best-effort
  React.useEffect(() => {
    const paths = lensScopePaths;
    if (!lensId || paths.length === 0) {
      setScopePreview([]);
      setScopePreviewLoading(false);
      return;
    }

    // Initial placeholders (fast)
    setScopePreview(
      paths.map((p) => ({
        canonicalPath: p,
        name: p.split('/').filter(Boolean).slice(-1)[0] || p,
      })),
    );

    let alive = true;
    setScopePreviewLoading(true);

    (async () => {
      const limited = paths.slice(0, 24);
      const results = await Promise.all(
        limited.map(async (p) => {
          // Domain paths don't have fragments — resolve from path structure.
          if (p.startsWith('/domain/')) {
            const segments = p.split('/').filter(Boolean);
            const contractType = segments.length >= 3 ? segments[2] : 'domain';
            const name = segments.slice(3).join(' / ') || contractType;
            return { canonicalPath: p, name, type: 'DOMAIN' };
          }
          // Artifact paths don't have fragments — use scopeArtifacts metadata.
          if (p.startsWith('/artifact/')) {
            const artKey = p.replace(/^\/artifact\//, '');
            const artMeta = lensMetaRef.current?.scopeArtifacts?.[artKey];
            const name =
              String(artMeta?.displayName || '').trim() ||
              artKey.split('/').filter(Boolean).slice(-1)[0] ||
              p;
            const subtitle = [artMeta?.anchorSupplier, artMeta?.anchorCustomer].filter(Boolean).join(' → ');
            const meta = [
              artMeta?.productAxis ? String(artMeta.productAxis).trim() : '',
              typeof artMeta?.legCount === 'number' && artMeta.legCount > 0 ? `${artMeta.legCount} legs` : '',
            ].filter(Boolean).join(' • ');
            return {
              canonicalPath: p,
              name,
              type: String(artMeta?.kind || '').trim() || 'ARTIFACT',
              subtitle: subtitle || undefined,
              meta: meta || undefined,
            };
          }
          try {
            const f = await api.getHorizonFragmentByPath(p);
            const name =
              String(f?.labels?.en || f?.label || '').trim() ||
              String(f?.canonicalPath || '').trim().split('/').filter(Boolean).slice(-1)[0] ||
              p.split('/').filter(Boolean).slice(-1)[0] ||
              p;
            const type = typeof f?.type === 'string' ? f.type : undefined;
            return { canonicalPath: p, name, type };
          } catch {
            return { canonicalPath: p };
          }
        }),
      );

      if (!alive) return;
      const byPath = new Map(results.map((r) => [r.canonicalPath, r]));
      setScopePreview((prev) =>
        prev.map((it) => {
          const upd = byPath.get(it.canonicalPath);
          return upd ? { ...it, ...upd } : it;
        }),
      );
      setScopePreviewLoading(false);
    })();

    return () => {
      alive = false;
    };
  }, [lensId, lensScopePaths]);

  const isDomainScopePreviewEntry = React.useCallback((entry: { canonicalPath: string; type?: string }) => {
    const cp = String(entry?.canonicalPath || '').trim().toLowerCase();
    const t = String(entry?.type || '').trim().toUpperCase();
    return t === 'DOMAIN' || t === 'ARTIFACT' || cp.startsWith('/domain/') || cp.startsWith('/artifact/');
  }, []);

  const scopePreviewDomains = React.useMemo(() => {
    return scopePreview.filter((entry) => isDomainScopePreviewEntry(entry));
  }, [scopePreview, isDomainScopePreviewEntry]);

  const scopePreviewNonDomains = React.useMemo(() => {
    return scopePreview.filter((entry) => !isDomainScopePreviewEntry(entry));
  }, [scopePreview, isDomainScopePreviewEntry]);

  // Add-node: search/prefetch entities by partial name (existing but previously unused API).
  React.useEffect(() => {
    if (!addNodeOpen || !addNodeSearchEnabled) return;
    const q = addNodeQuery.trim();
    if (q.length < 2) {
      addNodeAbortRef.current?.abort();
      addNodeAbortRef.current = null;
      setAddNodeResults([]);
      setAddNodeLoading(false);
      setAddNodeError(null);
      setAddNodeActiveIndex(0);
      return;
    }

    const cached = addNodeCacheRef.current.get(q);
    if (cached) {
      setAddNodeResults(cached);
      setAddNodeLoading(false);
      setAddNodeError(null);
      setAddNodeActiveIndex(0);
      return;
    }

    const handle = window.setTimeout(async () => {
      try {
        addNodeAbortRef.current?.abort();
        const ac = new AbortController();
        addNodeAbortRef.current = ac;
        setAddNodeLoading(true);
        setAddNodeError(null);

        const res = await api.searchHorizonEntities(q, 10, ac.signal);
        const entities = Array.isArray(res?.entities) ? (res.entities as any[]) : [];
        const next: HorizonSearchEntity[] = entities
          .filter(Boolean)
          .map((e: any) => ({
            id: String(e.id || '').trim(),
            canonicalPath: String(e.canonicalPath || '').trim(),
            name: String(e.name || '').trim(),
            type: String(e.type || '').trim(),
            description: typeof e.description === 'string' ? e.description : undefined,
            location: typeof e.location === 'string' ? e.location : undefined,
            sector: typeof e.sector === 'string' ? e.sector : undefined,
          }))
          .filter((e) => e.canonicalPath && e.name);

        boundedMapSet(addNodeCacheRef.current, q, next, 80);
        setAddNodeResults(next);
        setAddNodeActiveIndex(0);
      } catch (err: any) {
        // Ignore aborts
        if (err?.name === 'AbortError' || err?.code === 'ERR_CANCELED') return;
        console.warn('[Cockpit] add-node search failed', err);
        setAddNodeError(err?.message || 'Search failed');
      } finally {
        setAddNodeLoading(false);
      }
    }, 180);

    return () => window.clearTimeout(handle);
  }, [addNodeOpen, addNodeSearchEnabled, addNodeQuery]);

  // Refs for synchronous tracking of expanded nodes (avoids stale state issues)
  const expandedNodesRef = React.useRef<Set<string>>(new Set());
  const rootNodeIdRef = React.useRef<string | null>(null);
  const onLoadRelatedRef = React.useRef<((fragmentId: string) => void) | null>(null);
  const onCollapseRef = React.useRef<((fragmentId: string) => void) | null>(null);
  const onScopeScoutRef = React.useRef<((fragmentId: string, canonicalPath?: string) => void) | null>(null);
  const onArrangeRef = React.useRef<(() => void) | null>(null);

  // Stable proxies to avoid TDZ/circular hook deps between expand and collapse handlers.
  const invokeLoadRelated = React.useCallback((id: string) => {
    onLoadRelatedRef.current?.(id);
  }, []);
  const invokeCollapse = React.useCallback((id: string) => {
    onCollapseRef.current?.(id);
  }, []);

  // Auto-collapse when user disables the last active dock filter on an expanded node.
  React.useEffect(() => {
    const nid = String(pendingDockAutoCollapseNodeIdRef.current || '').trim();
    if (!nid) return;
    // Double-check: node should still be expanded and have no active filters.
    const node = (nodesStateRef.current || []).find((n) => String((n as any)?.id || '').trim() === nid) as any;
    const isStillExpanded = Boolean(node && (node.data as any)?.expanded);
    const currentFilters = Array.isArray((node?.data as any)?.rightDockEnabled) ? ((node.data as any).rightDockEnabled as any[]) : [];
    const hasNoFilters = currentFilters.length === 0;
    
    if (isStillExpanded && hasNoFilters) {
      pendingDockAutoCollapseNodeIdRef.current = null;
      // Collapse via the same pipeline as manual collapse (removes related fragments / edges).
      onCollapseRef.current?.(nid);
    } else {
      // Reset if conditions changed (e.g., user re-enabled a filter before collapse happened).
      pendingDockAutoCollapseNodeIdRef.current = null;
    }
  }, [rightDockStateByNodeId]);
  const invokeScopeScout = React.useCallback((fragmentId: string, canonicalPath?: string) => {
    onScopeScoutRef.current?.(fragmentId, canonicalPath);
  }, []);
  // Arrange scheduler: coalesce bursts of events (expand/add edges/page changes) into a single relayout.
  // This prevents cascading full-graph layouts in the same tick.
  const arrangeRafRef = React.useRef<number | null>(null);
  const arrangePendingRef = React.useRef<boolean>(false);
  const invokeArrange = React.useCallback(() => {
    if (arrangeRafRef.current != null) {
      arrangePendingRef.current = true;
      return;
    }
    arrangeRafRef.current = window.requestAnimationFrame(() => {
      arrangeRafRef.current = null;
    const fn: any = onArrangeRef.current;
    if (typeof fn === 'function') void fn();
      // If more requests came in while we were queued, run one more frame.
      if (arrangePendingRef.current) {
        arrangePendingRef.current = false;
        invokeArrange();
      }
    });
  }, []);

  // Trigger relayout when collapsed subcategories change (in Levels mode).
  React.useEffect(() => {
    if (!collapsedSubcategoriesChangedRef.current) return;
    collapsedSubcategoriesChangedRef.current = false;
    if (isLevelsLayout || isSupplyChainLayout || isInfluenceNetworkLayout || isValueChainLayout) {
      // Use ref directly (bypassing debounce) to ensure latest closure is called immediately.
      // onArrangeRef is updated each render to point to current handleArrange.
      const fn = onArrangeRef.current;
      if (typeof fn === 'function') fn();
    }
  }, [collapsedSubcategories, layoutMode]);

  // Trigger relayout when role overflow expansion changes (in Levels mode).
  React.useEffect(() => {
    if (!expandedRoleGroupsChangedRef.current) return;
    expandedRoleGroupsChangedRef.current = false;
    if (isLevelsLayout || isSupplyChainLayout || isInfluenceNetworkLayout || isValueChainLayout) {
      const fn = onArrangeRef.current;
      if (typeof fn === 'function') fn();
    }
  }, [expandedRoleGroups, layoutMode]);

  // Trigger relayout when right dock state changes (in Levels or Supply Chain mode).
  React.useEffect(() => {
    if (!rightDockChangedRef.current) return;
    rightDockChangedRef.current = false;
    if (isLevelsLayout || isSupplyChainLayout || isInfluenceNetworkLayout || isValueChainLayout) {
      const fn = onArrangeRef.current;
      if (typeof fn === 'function') fn();
    }
  }, [rightDockStateByNodeId, layoutMode]);

  // Corridor synthetic nodes/edges are only valid in Levels + Decision Chain mode.
  // Leaving the mode should purge them from the canonical graph state.
  React.useEffect(() => {
    const corridorAllowed = isLevelsLayout && levelsExpandMode === 'decision-chain';
    if (corridorAllowed) return;
    const nodesNow = nodesStateRef.current || [];
    const edgesNow = edgesStateRef.current || [];
    const hasCorridorNodes = nodesNow.some((n: any) => isCorridorArtifactNodeId(String((n as any)?.id || '').trim()));
    const hasCorridorEdges = edgesNow.some((e: any) => isCorridorArtifactEdgeId(String((e as any)?.id || '').trim()));
    if (!hasCorridorNodes && !hasCorridorEdges) return;
    setNodes((prev) => (prev || []).filter((n: any) => !isCorridorArtifactNodeId(String((n as any)?.id || '').trim())));
    setEdges((prev) => (prev || []).filter((e: any) => !isCorridorArtifactEdgeId(String((e as any)?.id || '').trim())));
    if (isLevelsLayout) invokeArrange();
  }, [layoutMode, levelsExpandMode, setNodes, setEdges, invokeArrange]);

  const lastDomainTopDepsRevealKeyRef = React.useRef<string>('');
  const lastDomainTopDepsMaterializeKeyRef = React.useRef<string>('');
  React.useEffect(() => {
    if (domainTopDependencyFocusActive) return;
    lastDomainTopDepsRevealKeyRef.current = '';
    lastDomainTopDepsMaterializeKeyRef.current = '';
  }, [domainTopDependencyFocusActive]);

  React.useEffect(() => {
    if (!domainTopDependencyFocusActive) return;
    if (domainTopDependencyMissingNodeRefs.length === 0) return;
    const missingKeys = domainTopDependencyMissingNodeRefs
      .map((x) => String(x.fragmentId || x.canonicalPath || '').trim())
      .filter(Boolean)
      .sort();
    if (missingKeys.length === 0) return;
    const key = `${String(activeDomainAnswerKey || '').trim()}|${missingKeys.join(',')}`;
    if (key && key === lastDomainTopDepsMaterializeKeyRef.current) return;
    lastDomainTopDepsMaterializeKeyRef.current = key;

    let cancelled = false;

    void (async () => {
      const fetched: Array<{ ref: { fragmentId: string; canonicalPath: string; label: string; type: string }; raw: any }> = [];
      await Promise.all(
        domainTopDependencyMissingNodeRefs.map(async (ref) => {
          const fragmentId = String(ref.fragmentId || '').trim();
          const canonicalPath = String(ref.canonicalPath || '').trim();
          if (!fragmentId && !canonicalPath) return;
          try {
            let raw: any = null;
            if (fragmentId) {
              raw = await api.getHorizonFragment(fragmentId);
            }
            if (!raw && canonicalPath) {
              raw = await api.getHorizonFragmentByPath(canonicalPath);
            }
            if (raw) fetched.push({ ref, raw });
          } catch (err) {
            console.warn('[Cockpit] Failed to materialize top dependency node', {
              fragmentId,
              canonicalPath,
              err,
            });
          }
        }),
      );
      if (cancelled || fetched.length === 0) return;

      setNodes((prev) => {
        const byId = new Set<string>();
        const byFragmentId = new Set<string>();
        const byCanonicalPath = new Set<string>();
        let centerX = 0;
        let centerY = 0;
        let centerN = 0;
        for (const n of prev as any[]) {
          const nodeId = String((n as any)?.id || '').trim();
          if (nodeId) byId.add(nodeId);
          const d: any = (n as any)?.data || {};
          const fid = String(d?.fragmentId || '').trim();
          if (fid) byFragmentId.add(fid);
          const cp = String(d?.canonicalPath || '').trim();
          if (cp) byCanonicalPath.add(cp);
          const p: any = (n as any)?.position;
          if (p && Number.isFinite(p.x) && Number.isFinite(p.y)) {
            centerX += Number(p.x);
            centerY += Number(p.y);
            centerN += 1;
          }
        }
        const baseX = centerN > 0 ? centerX / centerN : 0;
        const baseY = centerN > 0 ? centerY / centerN : 0;

        let changed = false;
        let addedCount = 0;
        const next = [...(prev as any[])];

        for (const row of fetched) {
          const raw = row.raw || {};
          const labels = raw?.labels && typeof raw.labels === 'object' ? raw.labels : {};
          const fragmentId = String(raw?.id || row.ref.fragmentId || '').trim();
          const canonicalPath = String(raw?.canonicalPath || row.ref.canonicalPath || '').trim();
          const nodeId = fragmentId || canonicalPath;
          if (!nodeId) continue;
          if (byId.has(nodeId)) continue;
          if (fragmentId && byFragmentId.has(fragmentId)) continue;
          if (canonicalPath && byCanonicalPath.has(canonicalPath)) continue;

          const stateMetrics: Record<string, number> = {};
          const rawState = raw?.state && typeof raw.state === 'object' ? raw.state : {};
          Object.entries(rawState).forEach(([k, v]: [string, any]) => {
            if (typeof v === 'number' && Number.isFinite(v)) {
              stateMetrics[k] = v;
              return;
            }
            if (v && typeof v === 'object' && typeof (v as any).value === 'number' && Number.isFinite((v as any).value)) {
              stateMetrics[k] = Number((v as any).value);
            }
          });

          const offsetCol = addedCount % 4;
          const offsetRow = Math.floor(addedCount / 4);
          addedCount += 1;

          next.push({
            id: nodeId,
            type: 'horizonFragment',
            position: { x: baseX + (offsetCol - 1.5) * 170, y: baseY + (offsetRow + 1) * 150 },
            hidden: false,
            data: {
              id: nodeId,
              lensItemId: '',
              fragmentId: fragmentId || undefined,
              canonicalPath: canonicalPath || undefined,
              facilityKind: readFacilityKindFromAny(raw),
              label:
                String(labels?.en || raw?.label || '').trim() ||
                (canonicalPath ? humanizeCanonicalPath(canonicalPath) : '') ||
                row.ref.label,
              type: String(raw?.type || row.ref.type || '').trim() || undefined,
              state: Object.keys(stateMetrics).length > 0 ? stateMetrics : undefined,
              showMetrics: showNodeMetrics,
              status: raw?.status,
              expanded: false,
              collapsed: false,
              critical: false,
              onLoadRelated: invokeLoadRelated,
              onCollapse: invokeCollapse,
              onScopeScout: invokeScopeScout,
            },
          } as any);
          changed = true;
          byId.add(nodeId);
          if (fragmentId) byFragmentId.add(fragmentId);
          if (canonicalPath) byCanonicalPath.add(canonicalPath);
        }

        return changed ? (next as any) : prev;
      });
      invokeArrange();
    })();

    return () => {
      cancelled = true;
    };
  }, [
    activeDomainAnswerKey,
    domainTopDependencyFocusActive,
    domainTopDependencyMissingNodeRefs,
    invokeArrange,
    invokeCollapse,
    invokeLoadRelated,
    invokeScopeScout,
    setNodes,
    showNodeMetrics,
  ]);

  React.useEffect(() => {
    if (!domainTopDependencyFocusActive) return;
    if (domainTopDependencySpecs.length === 0) return;
    setEdges((prev) => {
      const currentNodes = (nodesStateRef.current || nodes) as any[];
      const nodeIdByFragmentId = new Map<string, string>();
      const nodeIdByCanonicalPath = new Map<string, string>();
      const nodeIds = new Set<string>();
      for (const n of currentNodes) {
        const d: any = (n as any)?.data;
        if (d?.isGroup) continue;
        const nodeId = String((n as any)?.id || '').trim();
        if (!nodeId) continue;
        nodeIds.add(nodeId);
        const fid = String(d?.fragmentId || '').trim();
        if (fid && !nodeIdByFragmentId.has(fid)) nodeIdByFragmentId.set(fid, nodeId);
        const cp = String(d?.canonicalPath || '').trim();
        if (cp && !nodeIdByCanonicalPath.has(cp)) nodeIdByCanonicalPath.set(cp, nodeId);
      }
      const resolveNodeId = (fragmentId: string, canonicalPath: string): string => {
        const fid = String(fragmentId || '').trim();
        const cp = String(canonicalPath || '').trim();
        if (fid) {
          const byFid = nodeIdByFragmentId.get(fid);
          if (byFid) return byFid;
          if (nodeIds.has(fid)) return fid;
        }
        if (cp) {
          const byPath = nodeIdByCanonicalPath.get(cp);
          if (byPath) return byPath;
        }
        return '';
      };

      const next = [...(prev as any[])];
      const existingIds = new Set<string>();
      for (const e of next) {
        const id = String((e as any)?.id || '').trim();
        if (id) existingIds.add(id);
      }

      const relationNorm = (v: string) => String(v || '').trim().replace(/\s+/g, '_').toUpperCase();
      const hasMatchingEdge = (sourceId: string, targetId: string, relationType: string): boolean => {
        const relNorm = relationNorm(relationType);
        for (const e of next) {
          const es = String((e as any)?.source || '').trim();
          const et = String((e as any)?.target || '').trim();
          if (es !== sourceId || et !== targetId) continue;
          if (!relNorm) return true;
          const edgeRelNorm = relationNorm(
            String((e as any)?.data?.edgeType || (e as any)?.data?.subkind || (e as any)?.label || ''),
          );
          if (!edgeRelNorm || edgeRelNorm === relNorm) return true;
        }
        return false;
      };

      let changed = false;
      for (const dep of domainTopDependencySpecs) {
        const sourceId = resolveNodeId(dep.sourceFragmentId, dep.sourceCanonicalPath);
        const targetId = resolveNodeId(dep.targetFragmentId, dep.targetCanonicalPath);
        if (!sourceId || !targetId || sourceId === targetId) continue;
        if (hasMatchingEdge(sourceId, targetId, dep.relationType)) continue;

        const relationRaw = String(dep.relationType || '').trim() || 'DEPENDS_ON';
        const relNorm = relationNorm(relationRaw) || 'DEPENDS_ON';
        let edgeId = String(dep.edgeId || '').trim();
        if (!edgeId) edgeId = `domain-topdep:${sourceId}:${targetId}:${relNorm}`;
        if (existingIds.has(edgeId)) {
          let k = 2;
          let candidate = `${edgeId}:${k}`;
          while (existingIds.has(candidate)) {
            k += 1;
            candidate = `${edgeId}:${k}`;
          }
          edgeId = candidate;
        }
        existingIds.add(edgeId);

        const stroke =
          theme === 'dark'
            ? 'rgba(56,189,248,0.74)'
            : 'rgba(2,132,199,0.62)';
        next.push({
          id: edgeId,
          source: sourceId,
          target: targetId,
          type: 'default',
          interactionWidth: 18,
          markerEnd: {
            type: MarkerType.ArrowClosed,
            color: stroke,
          },
          style: {
            stroke,
            strokeWidth: 1.8,
            opacity: 0.95,
          },
          data: {
            edgeType: relationRaw,
            edgeCaption: getEdgeCaptionShort(relationRaw || relNorm),
            topDependencyScore: dep.topDependencyScore,
            isTopDependencySynthetic: true,
          },
        } as any);
        changed = true;
      }
      return changed ? (next as any) : prev;
    });
  }, [
    domainTopDependencyFocusActive,
    domainTopDependencySpecs,
    getEdgeCaptionShort,
    nodes,
    setEdges,
    theme,
  ]);

  React.useEffect(() => {
    if (!domainTopDependencyFocusActive || !domainTopDependencyNodeIds) return;
    const ids = Array.from(domainTopDependencyNodeIds).filter(Boolean).sort();
    if (ids.length === 0) return;
    const edgeIds = Array.from(domainTopDependencyEdgeIds || []).filter(Boolean).sort();
    const key = `${String(activeDomainAnswerKey || '').trim()}|${ids.join(',')}|${edgeIds.join(',')}`;
    if (key && key === lastDomainTopDepsRevealKeyRef.current) return;
    lastDomainTopDepsRevealKeyRef.current = key;

    const idSet = new Set<string>(ids);
    setNodes((prev) => {
      let changed = false;
      const next = (prev as any[]).map((n) => {
        const nid = String((n as any)?.id || '').trim();
        const d: any = (n as any)?.data || {};
        const fid = String(d?.fragmentId || '').trim();
        const cp = String(d?.canonicalPath || '').trim();
        const shouldReveal = Boolean(nid && idSet.has(nid)) || Boolean(fid && idSet.has(fid)) || Boolean(cp && idSet.has(cp));
        if (!shouldReveal || !Boolean((n as any)?.hidden)) return n;
        changed = true;
        return { ...(n as any), hidden: false } as any;
      });
      return changed ? (next as any) : prev;
    });

    pendingFocusNodeIdsRef.current = idSet;
    invokeArrange();
  }, [
    activeDomainAnswerKey,
    domainTopDependencyFocusActive,
    domainTopDependencyNodeIds,
    domainTopDependencyEdgeIds,
    setNodes,
    invokeArrange,
  ]);

  const stopTrace = React.useCallback(() => {
    traceAbortRef.current?.abort();
    traceAbortRef.current = null;
    traceActiveRunIdRef.current = null;
    setTraceJob((prev) => {
      if (!prev) return prev;
      if (prev.status !== 'running') return prev;
      return { ...prev, status: 'stopped', message: 'Stopped' };
    });
  }, []);

  const undoLastTrace = React.useCallback(() => {
    const last = traceUndoStack[traceUndoStack.length - 1];
    if (!last) return;
    const nodeSet = new Set(last.nodeIds.map((x) => String(x || '').trim()).filter(Boolean));
    const edgeSet = new Set(last.edgeIds.map((x) => String(x || '').trim()).filter(Boolean));
    setNodes((prev) => (prev || []).filter((n) => !nodeSet.has(String((n as any)?.id || '').trim())));
    setEdges((prev) => (prev || []).filter((e) => !edgeSet.has(String((e as any)?.id || '').trim())));
    if (Array.isArray(last.touched) && last.touched.length > 0) {
      const byId = new Map(last.touched.map((t) => [String(t.id || '').trim(), t]));
      setNodes((prev) => {
        return (prev || []).map((n) => {
          const id = String((n as any)?.id || '').trim();
          const hit = byId.get(id);
          if (!hit) return n;
          const d0: any = (n as any)?.data;
          if (!d0 || (d0 as any)?.isGroup) return n;
          const prevTB = hit.prevTraceBackbone;
          const curTB = (d0 as any)?.traceBackbone;
          const wantTB = typeof prevTB === 'boolean' ? prevTB : undefined;
          if (curTB === wantTB) return n;
          const d = { ...d0 };
          if (typeof wantTB === 'boolean') d.traceBackbone = wantTB;
          else delete d.traceBackbone;
          return { ...(n as any), data: d } as any;
        });
      });
    }
    setTraceUndoStack((prev) => prev.slice(0, -1));
    if (isLevelsLayout) {
      invokeArrange();
    }
  }, [traceUndoStack, setNodes, setEdges, layoutMode, invokeArrange]);

  const openTraceModalForNode = React.useCallback((nodeId: string) => {
    const rawId = String(nodeId || '').trim();
    if (!rawId) return;
    let rootLabel: string | undefined;
    let rootId: string = rawId;
    try {
      const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || [];
      const n = currentNodes.find((x) => String((x.data as any)?.fragmentId || x.id || '').trim() === rawId);
      const d: any = n?.data;
      // Prefer fragmentId for API calls.
      const fid = String(d?.fragmentId || '').trim();
      if (fid) rootId = fid;
      rootLabel = String(d?.label || '').trim() || String(d?.canonicalPath || '').trim() || undefined;
    } catch {
      // ignore
    }

    const scopes: Record<TraceScopeKey, boolean> = {
      ownership: false,
      dependencies: false,
      context: false,
      market: false,
      influence: false,
      all: false,
    };
    // Seed scopes from current Hierarchical expansion mode (keeps UX consistent).
    if (levelsExpandMode === 'ownership') scopes.ownership = true;
    else if (levelsExpandMode === 'dependencies') scopes.dependencies = true;
    else if (levelsExpandMode === 'context') scopes.context = true;
    else if (levelsExpandMode === 'market') scopes.market = true;
    else if (levelsExpandMode === 'influence') scopes.influence = true;
    else if (levelsExpandMode === 'balanced') {
      scopes.ownership = true;
      scopes.context = true;
      scopes.market = true;
    } else if (levelsExpandMode === 'expansion') {
      scopes.ownership = true;
      scopes.market = true;
    } else {
      // 'all'
      scopes.all = true;
    }

    setTraceDraft({
      rootId,
      rootLabel,
      direction: 'up',
      scopes,
      keywords: '',
      depth: 3,
      maxNodes: 160,
      maxEdges: 260,
    });
  }, [levelsExpandMode]);

  // Semantic group stroke colors (4-color palette for graph clarity)
  const SEMANTIC_GROUP_STROKE: Record<SemanticGroup, string> = {
    'supply-chain': '#3B82F6',  // blue
    'customers': '#22C55E',     // green
    'market': '#EF4444',       // red
    'context': '#A855F7',      // purple
  };
  const DOMAIN_SCAFFOLD_EDGE_TYPES = new Set([
    'FOCUSES_ON',
    'INVOLVES_ACTOR',
    'IN_GEO_SCOPE',
    'ABOUT',
    'RELATED_DOMAIN',
    'BROADENS_TO',
    'NARROWS_TO',
    'DOMAIN_RELATION',
  ]);

  // Helper function for edge styles based on type
  // When useSemanticGroupColors: 4 colors by semantic group. Else: full per-type palette.
  const getEdgeStyle = (edgeType: string | undefined, criticality: number | undefined, edgeData?: Record<string, any>) => {
    const type = (edgeType || '').toUpperCase();
    const baseWidth = criticality ? Math.max(1, criticality * 2) : 1.5;
    const specialized = getGraphViewEdgeStyle(edgeType, criticality, edgeData, useSemanticGroupColors);
    if (specialized) return specialized;

    if (useSemanticGroupColors) {
      if (DOMAIN_SCAFFOLD_EDGE_TYPES.has(type)) {
        return {
          stroke: '#9CA3AF',
          strokeWidth: Math.max(1.1, baseWidth * 0.9),
          strokeDasharray: '5 4',
        };
      }
      // Market overlays: neutral participation/trading, not conflict — override market red
      if (type === 'PARTICIPATES_IN') {
        return { stroke: '#0D9488', strokeWidth: baseWidth }; // teal-600
      }
      if (type === 'TRADED_IN') {
        return {
          stroke: '#14B8A6', // teal-500
          strokeWidth: Math.max(1.4, baseWidth),
          strokeDasharray: '5 3',
        };
      }
      // HAS_MARKET_SEGMENT: hierarchy/skeleton — dashed gray (distinct from red competes)
      if (type === 'HAS_MARKET_SEGMENT' || type === 'HAS_SUBMARKET') {
        return {
          stroke: '#9CA3AF', // gray-400
          strokeWidth: Math.max(1.2, baseWidth * 0.95),
          strokeDasharray: '5 4',
        };
      }
      const sg = getSemanticGroup(type, true);
      const stroke = SEMANTIC_GROUP_STROKE[sg] ?? '#64748B';
      return { stroke, strokeWidth: baseWidth };
    }

    // 🟢 Participation (org in market) — Teal (neutral, distinct from red competes)
    if (type === 'PARTICIPATES_IN') {
      return {
        stroke: '#0D9488', // teal-600
        strokeWidth: baseWidth,
      };
    }

    // 🟢 Product trading / price discovery in market — Teal dashed
    if (type === 'TRADED_IN') {
      return {
        stroke: '#14B8A6', // teal-500
        strokeWidth: Math.max(1.4, baseWidth),
        strokeDasharray: '5 3',
      };
    }

    // 🟡 Dominance / market-defining — Amber (high salience)
    if (type === 'DOMINATES') {
      return {
        stroke: '#F59E0B', // amber-500
        strokeWidth: Math.max(baseWidth + 1.0, 2.6),
      };
    }

    // Market hierarchy (skeleton) — Gray dashed (distinct from red competes)
    if (type === 'HAS_MARKET_SEGMENT' || type === 'HAS_SUBMARKET') {
      return {
        stroke: '#9CA3AF', // gray-400
        strokeWidth: Math.max(1.2, baseWidth * 0.95),
        strokeDasharray: '5 4',
      };
    }

    // Domain scaffold/context edges — neutral gray dashed to avoid false conflict emphasis
    if (DOMAIN_SCAFFOLD_EDGE_TYPES.has(type)) {
      return {
        stroke: '#9CA3AF', // gray-400
        strokeWidth: Math.max(1.1, baseWidth * 0.9),
        strokeDasharray: '5 4',
      };
    }

    // 🟣 Community hierarchy (dynasty/tribe skeleton) — Purple dashed
    if (type === 'HAS_SUBGROUP') {
      return {
        stroke: '#A855F7', // purple-500 (matches COMMUNITY node color)
        strokeWidth: Math.max(1.2, baseWidth * 0.95),
        strokeDasharray: '5 4',
      };
    }

    // 🟦 Government admin hierarchy (skeleton) — Sky dashed
    if (type === 'ADMINISTERS') {
      return {
        stroke: '#0EA5E9', // sky-500
        strokeWidth: Math.max(1.2, baseWidth * 0.95),
        strokeDasharray: '4 4',
      };
    }

    // 🔴 Конкуренция/Конфликт — Красный
    if (['COMPETES_WITH', 'CONFLICTS_WITH', 'OPPOSES'].includes(type)) {
      return {
        stroke: '#EF4444', // red-500
        strokeWidth: baseWidth + 0.5,
      };
    }

    // 🟢 Партнёрство/Кооперация — Зелёный
    if (['PARTNERS_WITH', 'COOPERATES_WITH', 'SUPPLIES_TO', 'SUPPORTS'].includes(type)) {
      return {
        stroke: '#22C55E', // green-500
        strokeWidth: baseWidth,
      };
    }

    // 🟠 Влияние — Оранжевый
    if (['INFLUENCES', 'AFFECTS', 'REGULATES', 'IMPACTS'].includes(type)) {
      return {
        stroke: '#F97316', // orange-500
        strokeWidth: baseWidth,
      };
    }

    // 🔵 Equity ownership / corporate structure — Blue (solid)
    // Treat INVESTS_IN here intentionally (often part of ownership structure in our UX).
    if (['OWNS', 'INVESTS_IN', 'PARENT_OF', 'SUBSIDIARY_OF'].includes(type)) {
      return {
        stroke: '#3B82F6', // blue-500
        strokeWidth: baseWidth,
      };
    }

    // 🟦 Control / management (non-equity) — Indigo (dashed)
    if (['CONTROLS', 'MANAGES'].includes(type)) {
      return {
        stroke: '#6366F1', // indigo-500
        strokeWidth: Math.max(1.3, baseWidth),
        strokeDasharray: '6 4',
      };
    }

    // 🟦 Governance ties — Indigo (light dotted)
    if (['BOARD_MEMBER_OF', 'EMPLOYED_BY', 'FOUNDED'].includes(type)) {
      return {
        stroke: '#6366F1', // indigo-500
        strokeWidth: Math.max(1.1, baseWidth * 0.9),
        strokeDasharray: '2 3',
      };
    }

    // ⚪ Зависимость — Серый пунктир
    if (['DEPENDS_ON', 'REQUIRES', 'USES', 'NEEDS'].includes(type)) {
      return {
        stroke: '#9CA3AF', // gray-400
        strokeWidth: 1,
        strokeDasharray: '4 2',
      };
    }

    // 🟣 Локация — Фиолетовый
    if (['LOCATED_IN', 'OPERATES_IN', 'BASED_IN', 'HEADQUARTERED_IN'].includes(type)) {
      return {
        stroke: '#A855F7', // purple-500
        strokeWidth: baseWidth,
      };
    }

    // По умолчанию — нейтральный серо-голубой
    return {
      stroke: '#64748B', // slate-500
      strokeWidth: baseWidth,
    };
  };

  // When useSemanticGroupColors changes, refresh edge baseStyle (no layout needed)
  React.useEffect(() => {
    setEdges((prev) =>
      (prev || []).map((e: any) => {
        const ed = e?.data || {};
        const et = ed.edgeType ?? ed.subkind ?? e?.label ?? e?.type;
        const baseStyle = getEdgeStyle(et, ed.criticality, ed);
        return { ...e, data: { ...ed, baseStyle } };
      }),
    );
  }, [useSemanticGroupColors, setEdges]);

  const runTrace = React.useCallback(async (draft: TraceDraft) => {
    const rootFragId = String(draft?.rootId || '').trim();
    if (!rootFragId) return;

    // Single active trace at a time.
    stopTrace();

    const runId = `trace:${Date.now().toString(36)}:${Math.random().toString(36).slice(2, 8)}`;
    traceActiveRunIdRef.current = runId;
    const ac = new AbortController();
    traceAbortRef.current = ac;
    traceLastProgressAtRef.current = 0;

    const startedAt = Date.now();
    setTraceJob({
      runId,
      rootId: rootFragId,
      status: 'running',
      message: 'Starting…',
      depth: 0,
      maxDepth: Math.max(1, Math.min(12, Math.floor(draft.depth || 1))),
      processed: 0,
      queued: 1,
      addedNodes: 0,
      addedEdges: 0,
      startedAt,
    });

    // Ensure Hierarchical mode (Levels).
    if (!isLevelsLayout) {
      setLayoutMode('hierarchical');
      // Give React a beat to flush state before we start mutating the graph.
      await new Promise((r) => window.setTimeout(r, 0));
    }

    const normEdgeTypeLocal = (t: any) => String(t || '').toUpperCase().trim();
    const splitKeywords = (raw: string): string[] => {
      return String(raw || '')
        .split(/[\s,;]+/g)
        .map((x) => x.trim().toLowerCase())
        .filter(Boolean)
        .slice(0, 10);
    };
    const tokens = splitKeywords(draft.keywords);
    const matchesKeywords = (frag: any): boolean => {
      if (!tokens || tokens.length === 0) return true;
      const label = String(frag?.labels?.en || frag?.label || frag?.name || '').trim();
      const cp = String(frag?.canonicalPath || '').trim();
      const hay = `${label} ${cp}`.toLowerCase();
      return tokens.every((t) => hay.includes(t));
    };

    const wantsAll = Boolean(draft.scopes?.all) || !draft.scopes || Object.values(draft.scopes).every((v) => !v);
    const unionEdgeTypes = (() => {
      if (wantsAll) return [] as string[];
      const OWNERSHIP = ['OWNS', 'SUBSIDIARY_OF', 'PARENT_OF', 'MANAGES', 'CONTROLS', 'INVESTS_IN', 'BOARD_MEMBER_OF', 'EXECUTIVE_OF'];
      const DEPENDENCIES = ['SUPPLIES_TO', 'PRODUCES', 'TRANSFORMS_INTO', 'DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM'];
      const CONTEXT = ['LOCATED_IN', 'OPERATES_IN', 'BASED_IN', 'HEADQUARTERED_IN', 'REGULATES', 'ASSURES', 'AUDITED_BY', 'EXTRACTS_FROM', 'ADMINISTERS'];
      const MARKET = ['COMPETES_WITH', 'CONFLICTS_WITH', 'OPPOSES', 'PARTNERS_WITH', 'COOPERATES_WITH', 'RELATED_TO', 'RELATIVE_OF', 'SIMILAR_TO', 'LINKED_TO', 'PARTICIPATES_IN', 'TRADED_IN', 'DOMINATES', 'HAS_MARKET_SEGMENT', 'HAS_SUBGROUP'];
      const INFLUENCE = ['INFLUENCES', 'AFFECTS', 'IMPACTS'];
      const out = new Set<string>();
      if (draft.scopes.ownership) for (const t of OWNERSHIP) out.add(t);
      if (draft.scopes.dependencies) for (const t of DEPENDENCIES) out.add(t);
      if (draft.scopes.context) for (const t of CONTEXT) out.add(t);
      if (draft.scopes.market) for (const t of MARKET) out.add(t);
      if (draft.scopes.influence) for (const t of INFLUENCE) out.add(t);
      return Array.from(out);
    })();

    // Relative direction inference for traversal (best-effort, front-end guard; backend can pre-filter too).
    const CONTROL_FWD = new Set(['OWNS', 'PARENT_OF', 'MANAGES', 'CONTROLS', 'INVESTS_IN', 'BOARD_MEMBER_OF', 'EXECUTIVE_OF']);
    const CONTROL_INV = new Set(['SUBSIDIARY_OF']);
    const SUPPLY_FWD = new Set(['SUPPLIES_TO', 'PRODUCES', 'TRANSFORMS_INTO']);
    const SUPPLY_INV = new Set(['DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM']);
    const PEER = new Set(['COMPETES_WITH', 'CONFLICTS_WITH', 'OPPOSES', 'PARTNERS_WITH', 'COOPERATES_WITH', 'RELATED_TO', 'RELATIVE_OF', 'SIMILAR_TO', 'LINKED_TO']);
    // Market-defining relations: org DOMINATES market → org appears to the RIGHT of market (not up/down hierarchy)
    const MARKET_DEFINING = new Set(['DOMINATES']);
    const CONTEXTISH = new Set(['LOCATED_IN', 'OPERATES_IN', 'BASED_IN', 'HEADQUARTERED_IN', 'REGULATES']);
    const INFLUENCEISH = new Set(['INFLUENCES', 'AFFECTS', 'IMPACTS']);

    type Rel = 'up' | 'down' | 'peer' | 'unknown';
    const relForEdge = (fromId: string, s: string, t: string, et: string): Rel => {
      if (!fromId || !s || !t) return 'unknown';
      if (PEER.has(et)) return 'peer';
      if (MARKET_DEFINING.has(et)) return 'peer'; // DOMINATES is horizontal (org to the right of market)
      if (CONTEXTISH.has(et) || INFLUENCEISH.has(et)) return 'peer';

      // Control hierarchy
      if (CONTROL_FWD.has(et)) {
        if (fromId === t) return 'up';    // from = child, other = parent
        if (fromId === s) return 'down';  // from = parent, other = child
      }
      if (CONTROL_INV.has(et)) {
        // SUBSIDIARY_OF: child(source) -> parent(target)
        if (fromId === s) return 'up';
        if (fromId === t) return 'down';
      }

      // Supply-chain-ish
      if (SUPPLY_FWD.has(et)) {
        // supplier(source) -> consumer(target)
        if (fromId === t) return 'up';
        if (fromId === s) return 'down';
      }
      if (SUPPLY_INV.has(et)) {
        // depends_on: dependent(source) -> dependency(target)
        if (fromId === s) return 'up';
        if (fromId === t) return 'down';
      }

      return 'unknown';
    };

    const want = (rel: Rel): boolean => {
      const d = draft.direction || 'both';
      if (d === 'both') return rel !== 'unknown';
      return rel === d;
    };

    const semanticParam =
      draft.direction === 'up'
        ? 'upstream'
        : draft.direction === 'down'
          ? 'downstream'
          : 'both';

    const addedNodeIds: string[] = [];
    const addedEdgeIds: string[] = [];
    // Nodes we've already traversed along the up/down hierarchy (loop guard) — by fragmentId.
    const traversed = new Set<string>([rootFragId]);
    // Existing nodes we modified (so Undo can restore).
    const touchedPrevById = new Map<string, boolean | undefined>();
    let frontier: string[] = [rootFragId]; // fragmentIds

    const bump = (patch: Partial<TraceJob>) => {
      const now = Date.now();
      if (now - traceLastProgressAtRef.current < 180) return;
      traceLastProgressAtRef.current = now;
      setTraceJob((prev) => {
        if (!prev || prev.runId !== runId) return prev;
        return { ...prev, ...patch };
      });
    };

    let processed = 0;
    let depth = 0;
    let aborted = false;
    let capped: string | null = null;

    try {
      const maxDepthLimit = Math.max(1, Math.min(12, Math.floor(draft.depth || 1)));
      console.log('[Trace] Starting BFS. maxDepth:', maxDepthLimit, 'direction:', draft.direction, 'scopes:', draft.scopes);

      while (frontier.length > 0 && depth < maxDepthLimit) {
        if (traceActiveRunIdRef.current !== runId) break;
        depth += 1;
        console.log('[Trace] === Layer', depth, '===  frontier.length:', frontier.length, 'frontier:', frontier.slice(0, 5));
        bump({ depth, queued: frontier.length, message: `Depth ${depth}…` });

        const layerNewNodes: Node<HorizonNodeData>[] = [];
        const layerNewEdges: Edge[] = [];
        const nextFrontierSet = new Set<string>();
        const touchedTraceBackbone = new Map<string, boolean | undefined>();

        for (const fromFragIdRaw of frontier) {
          const fromFragId = String(fromFragIdRaw || '').trim();
          if (!fromFragId) continue;
          if (traceActiveRunIdRef.current !== runId) break;
          processed += 1;

          // Build a robust mapping fragmentId -> current nodeId (some nodes may be keyed by lensItemId).
          const snapNodes: Node<HorizonNodeData>[] = (nodesStateRef.current || nodes) as any;
          const snapEdges: Edge[] = (edgesStateRef.current || edges) as any;
          const fragToNodeId = new Map<string, string>();
          const nodeById = new Map<string, Node<HorizonNodeData>>();
          for (const n of snapNodes) {
            const nid = String((n as any)?.id || '').trim();
            if (nid) nodeById.set(nid, n as any);
            const fid = String((n as any)?.data?.fragmentId || '').trim();
            if (fid && !fragToNodeId.has(fid)) fragToNodeId.set(fid, nid || fid);
          }
          const resolveNodeId = (fragId: string) => fragToNodeId.get(fragId) || fragId;
          const rootNodeId = resolveNodeId(rootFragId);
          const fromNodeId = resolveNodeId(fromFragId);

          let deps: any;
          try {
            deps = await api.getFragmentDependencies(fromFragId, {
              direction: 'both',
              ...(unionEdgeTypes.length > 0 ? { edgeTypes: unionEdgeTypes } : {}),
              semantic: semanticParam as any,
              includeFragments: true,
              maxNeighbors: 84,
              maxPerType: 22,
              limit: 650,
              ...(lensId ? { lensId } : {}),
              signal: ac.signal,
            });
          } catch (err: any) {
            if (err?.name === 'AbortError' || err?.code === 'ERR_CANCELED') {
              aborted = true;
              break;
            }
            console.warn('[Trace] getFragmentDependencies failed', err);
            throw err;
          }

          const depEdges: any[] = Array.isArray(deps?.edges) ? deps.edges : [];
          const frags: any[] = Array.isArray((deps as any)?.fragments) ? (deps as any).fragments : [];
          console.log('[Trace] Node', fromFragId, '→ API returned', depEdges.length, 'edges,', frags.length, 'fragments');
          const byId = new Map<string, any>();
          for (const f of frags) {
            const fid = String(f?.id || '').trim();
            if (!fid) continue;
            if (!byId.has(fid)) byId.set(fid, f);
          }

          const currentNodeIds = new Set<string>(snapNodes.map((n) => String((n as any)?.id || '').trim()).filter(Boolean));
          const currentEdgeIds = new Set<string>(snapEdges.map((e) => String((e as any)?.id || '').trim()).filter(Boolean));

          for (const e of depEdges) {
            const s = String(e?.sourceFragmentId || e?.source || '').trim();
            const t = String(e?.targetFragmentId || e?.target || '').trim();
            if (!s || !t) continue;
            if (s !== fromFragId && t !== fromFragId) continue;
            const otherFragId = s === fromFragId ? t : s;
            if (!otherFragId) continue;

            const et = normEdgeTypeLocal(e?.type || e?.edgeType || e?.subkind || e?.label);
            const rel = relForEdge(fromFragId, s, t, et);
            console.log('[Trace]   edge:', et, 's:', s.slice(0, 8), 't:', t.slice(0, 8), 'from:', fromFragId.slice(0, 8), '→ rel:', rel, 'want:', want(rel));
            if (!want(rel)) continue;

            let otherFrag = byId.get(otherFragId) || null;
            if (tokens.length > 0 && otherFrag && !matchesKeywords(otherFrag)) {
              continue;
            }
            if (tokens.length > 0 && !otherFrag) {
              try {
                otherFrag = await api.getHorizonFragment(otherFragId, ac.signal);
              } catch {
                otherFrag = null;
              }
              // If keyword filtering is active and we can't fetch the fragment, skip it.
              if (!otherFrag) continue;
              if (otherFrag && !matchesKeywords(otherFrag)) continue;
            }

            // Soft caps (avoid blowing up the canvas).
            const otherNodeId = resolveNodeId(otherFragId);
            const willCreateNewNode = !currentNodeIds.has(otherNodeId) && !layerNewNodes.some((n) => String(n.id || '').trim() === otherNodeId);
            if (willCreateNewNode) {
              if (addedNodeIds.length + layerNewNodes.length >= Math.max(20, Math.floor(draft.maxNodes || 160))) {
                capped = 'Node limit reached';
                break;
              }

              // For newly created nodes, try a best-effort fetch to populate label/canonicalPath
              // (keeps UX readable even when backend didn't include fragments).
              if (!otherFrag) {
                try {
                  otherFrag = await api.getHorizonFragment(otherFragId, ac.signal);
                } catch {
                  otherFrag = null;
                }
              }
              const fragObj = otherFrag || { id: otherFragId, canonicalPath: '', labels: {} };
              if (isSphereEntity(fragObj?.type, fragObj?.canonicalPath)) {
                continue;
              }
              const stateMetrics: Record<string, number> = {};
              if (fragObj?.state) {
                Object.entries(fragObj.state).forEach(([k, v]: [string, any]) => {
                  if (typeof v === 'object' && v !== null && 'value' in v) {
                    stateMetrics[k] = typeof (v as any).value === 'number' ? (v as any).value : 0;
                  } else if (typeof v === 'number') {
                    stateMetrics[k] = v;
                  }
                });
              }

              // Direction from the perspective of the central node (fromNodeId):
              // - 'incoming' if the edge points TO the central node (other → central)
              // - 'outgoing' if the edge points FROM the central node (central → other)
              // For hierarchical relations (up/down), use rel. For peer relations, use actual edge direction.
              let directionFromParent: 'incoming' | 'outgoing';
              if (rel === 'up') {
                directionFromParent = 'incoming';
              } else if (rel === 'down') {
                directionFromParent = 'outgoing';
              } else {
                // Peer/market-defining: determine by actual edge direction
                // If central node (fromNodeId) is the target, it's incoming; otherwise outgoing
                directionFromParent = (t === fromNodeId) ? 'incoming' : 'outgoing';
              }
              const isBackbone = rel === 'up' || rel === 'down';

              layerNewNodes.push({
                id: otherNodeId,
                type: 'horizonFragment' as const,
                position: { x: 0, y: 0 },
                data: {
                  id: otherNodeId,
                  lensItemId: '',
                  fragmentId: otherFragId,
                  parentFragmentId: fromNodeId,
                  rootFragmentId: rootNodeId,
                  edgeTypeFromParent: et,
                  directionFromParent,
                  depth,
                  canonicalPath: fragObj?.canonicalPath,
                  facilityKind: readFacilityKindFromAny(fragObj),
                  indexPaths: Array.isArray(fragObj?.indexPaths) ? fragObj.indexPaths : undefined,
                  marketSegments: Array.isArray(fragObj?.marketSegments) ? fragObj.marketSegments : undefined,
                  marketSegmentNorms: Array.isArray(fragObj?.marketSegmentNorms) ? fragObj.marketSegmentNorms : undefined,
                  marketSegmentNormDomains: Array.isArray(fragObj?.marketSegmentNormDomains) ? fragObj.marketSegmentNormDomains : undefined,
                  marketSegmentNormVerticals: Array.isArray(fragObj?.marketSegmentNormVerticals) ? fragObj.marketSegmentNormVerticals : undefined,
                  marketSegmentNormPairs: Array.isArray(fragObj?.marketSegmentNormPairs) ? fragObj.marketSegmentNormPairs : undefined,
                  label: fragObj?.labels?.en || humanizeCanonicalPath(fragObj?.canonicalPath) || 'Fragment',
                  type: fragObj?.type,
                  state: Object.keys(stateMetrics).length > 0 ? stateMetrics : undefined,
                  showMetrics: showNodeMetrics,
                  status: fragObj?.status,
                  expanded: false,
                  collapsed: false,
                  critical: false,
                  onLoadRelated: invokeLoadRelated,
                  onCollapse: invokeCollapse,
                  onScopeScout: invokeScopeScout,
                  traceRunId: runId,
                  traceBackbone: isBackbone ? true : undefined,
                } as any,
              });
            }

            // BFS frontier management (no loops): traverse only up/down; peer matches are included but not expanded.
            if ((rel === 'up' || rel === 'down') && !traversed.has(otherFragId)) {
              traversed.add(otherFragId);
              nextFrontierSet.add(otherFragId);
              console.log('[Trace]   → added to frontier:', otherFragId.slice(0, 12), 'rel:', rel);
            }

            // If the node already exists and is part of the backbone traversal, ensure it is treated as backbone.
            if ((rel === 'up' || rel === 'down') && currentNodeIds.has(otherNodeId)) {
              const existing = nodeById.get(otherNodeId);
              const d0: any = (existing as any)?.data;
              const already = Boolean(d0?.traceBackbone);
              if (!already) {
                // Record previous value only once (per-layer); we'll merge into touchedPrevById below.
                if (!touchedTraceBackbone.has(otherNodeId)) {
                  touchedTraceBackbone.set(otherNodeId, typeof d0?.traceBackbone === 'boolean' ? d0.traceBackbone : undefined);
                }
              }
            }

            // Add edge if both endpoints exist or are created in this layer.
            const sourceNodeId = resolveNodeId(s);
            const targetNodeId = resolveNodeId(t);
            const haveSource = currentNodeIds.has(sourceNodeId) || layerNewNodes.some((n) => String(n.id || '').trim() === sourceNodeId);
            const haveTarget = currentNodeIds.has(targetNodeId) || layerNewNodes.some((n) => String(n.id || '').trim() === targetNodeId);
            const haveOther = haveSource && haveTarget;
            if (!haveOther) continue;
            const edgeId = String(e?.id || '').trim() || `trace:${runId}:${s}->${t}:${et}`;
            if (currentEdgeIds.has(edgeId) || layerNewEdges.some((x) => x.id === edgeId)) continue;
            if (addedEdgeIds.length + layerNewEdges.length >= Math.max(40, Math.floor(draft.maxEdges || 260))) {
              capped = 'Edge limit reached';
              break;
            }

            const baseStyle = getEdgeStyle(et, (e as any)?.criticality, (e as any)?.data);
            const stroke = getEdgeStroke(baseStyle);
            layerNewEdges.push({
              id: edgeId,
              source: sourceNodeId,
              target: targetNodeId,
              type: 'default',
              label: undefined,
              interactionWidth: 18,
              markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
              style: baseStyle,
              data: {
                ...(e?.properties || {}),
                edgeType: et,
                edgeCaption: getEdgeCaptionShort(et),
                traceRunId: runId,
              } as any,
            });
          }

          bump({
            processed,
            queued: frontier.length,
            addedNodes: addedNodeIds.length + layerNewNodes.length,
            addedEdges: addedEdgeIds.length + layerNewEdges.length,
          });

          if (aborted || capped) break;
        }

        // Apply any traceBackbone toggles to existing nodes in one pass (stable + undoable).
        if (touchedTraceBackbone.size > 0) {
          const touchedIds = new Set<string>(Array.from(touchedTraceBackbone.keys()));
          // Merge into run-level touched map (for undo).
          for (const [id, prev] of touchedTraceBackbone.entries()) {
            if (!touchedPrevById.has(id)) touchedPrevById.set(id, prev);
          }
          setNodes((prev) => {
            return (prev || []).map((n) => {
              const id = String((n as any)?.id || '').trim();
              if (!touchedIds.has(id)) return n;
              const d0: any = (n as any)?.data;
              if (!d0 || d0.isGroup) return n;
              if (d0.traceBackbone) return n;
              return { ...(n as any), data: { ...d0, traceBackbone: true } } as any;
            });
          });
        }

        // Next frontier for BFS.
        console.log('[Trace] Layer', depth, 'done. nextFrontierSet.size:', nextFrontierSet.size, 'layerNewNodes:', layerNewNodes.length, 'layerNewEdges:', layerNewEdges.length);
        frontier = Array.from(nextFrontierSet);

        // Apply batched updates for this layer.
        if (layerNewNodes.length > 0) {
          addedNodeIds.push(...layerNewNodes.map((n) => n.id));
          setNodes((prev) => {
            const existing = new Set((prev || []).map((n) => String(n.id || '').trim()).filter(Boolean));
            const add = layerNewNodes.filter((n) => !existing.has(String(n.id || '').trim()));
            return add.length > 0 ? ([...(prev || []), ...add] as any) : prev;
          });
        }
        if (layerNewEdges.length > 0) {
          addedEdgeIds.push(...layerNewEdges.map((e) => e.id));
          setEdges((prev) => {
            const existing = new Set((prev || []).map((e) => String(e.id || '').trim()).filter(Boolean));
            const add = layerNewEdges.filter((e) => !existing.has(String(e.id || '').trim()));
            return add.length > 0 ? ([...(prev || []), ...add] as any) : prev;
          });
        }
        if (layerNewNodes.length > 0 || layerNewEdges.length > 0) {
          invokeArrange();
        }

        if (aborted || capped) break;
      }
    } finally {
      // Clear active run ref unless a new run started.
      if (traceActiveRunIdRef.current === runId) {
        traceActiveRunIdRef.current = null;
        traceAbortRef.current = null;
      }
    }

    const finishedAt = Date.now();
    if (!aborted && !capped && addedNodeIds.length === 0 && addedEdgeIds.length === 0) {
      setTraceJob((prev) => (prev && prev.runId === runId ? { ...prev, status: 'done', message: 'No matches' } : prev));
      return;
    }

    if (addedNodeIds.length > 0 || addedEdgeIds.length > 0) {
      setTraceUndoStack((prev) => {
        const touchedList =
          touchedPrevById.size > 0
            ? Array.from(touchedPrevById.entries()).map(([id, prevTraceBackbone]) => ({ id, prevTraceBackbone }))
            : undefined;
        const next = [...prev, { runId, rootId: rootFragId, nodeIds: addedNodeIds, edgeIds: addedEdgeIds, touched: touchedList, finishedAt }];
        return next.slice(-12);
      });
    }

    setTraceJob((prev) => {
      if (!prev || prev.runId !== runId) return prev;
      if (aborted) return { ...prev, status: 'stopped', message: 'Stopped' };
      if (capped) return { ...prev, status: 'done', message: capped };
      return { ...prev, status: 'done', message: 'Done' };
    });
  }, [
    stopTrace,
    layoutMode,
    setLayoutMode,
    lensId,
    setNodes,
    setEdges,
    invokeArrange,
    getEdgeStyle,
    getEdgeStroke,
    getEdgeCaptionShort,
    showNodeMetrics,
    invokeLoadRelated,
    invokeCollapse,
    invokeScopeScout,
  ]);

  const startTraceFromModal = React.useCallback(async () => {
    const draft = traceDraft;
    if (!draft) return;
    setTraceDraft(null);
    await runTrace(draft);
  }, [traceDraft, runTrace]);

  // Close trace modal on Escape.
  React.useEffect(() => {
    if (!traceDraft) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        setTraceDraft(null);
      }
    };
    window.addEventListener('keydown', onKeyDown, true);
    return () => window.removeEventListener('keydown', onKeyDown, true);
  }, [traceDraft]);

  const openScopeScoutModal = React.useCallback((fragmentId: string, canonicalPath?: string) => {
    const fid = String(fragmentId || '').trim();
    const nextCanonicalPath = String(canonicalPath || '').trim();
    let label: string | undefined;
    try {
      const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || [];
      const n = currentNodes.find((x) => String((x.data as any)?.fragmentId || x.id || '').trim() === fid);
      const d: any = n?.data;
      label = (d?.label as string) || (d?.canonicalPath as string) || undefined;
    } catch {
      // ignore
    }
    const defaultMode = defaultScoutingModeForCanonicalPath(nextCanonicalPath);
    setScopeScoutMode(defaultMode);
    setScopeScoutDepth(defaultMode === 'supply_chain' ? 2 : 1);
    setScopeScoutDirection('downstream');
    setScopeScoutHotPathOnly(false);
    setScopeScoutIncludeCapabilities(false);
    setScopeScoutIncludeEdgeMetrics(false);
    setScopeScoutCustomQuery('');
    setScopeScoutModal({ fragmentId: fid, canonicalPath: nextCanonicalPath || canonicalPath, label });
  }, []);

  const handleLoadSupplyChain = React.useCallback(
    async (paths: string[], artifact: MaterializedArtifact) => {
      const currentNodes = nodesStateRef.current || [];
      const refToNodeId = new Map<string, string>();
      for (const n of currentNodes) {
        if ((n.data as any)?.isGroup) continue;
        const cp = String((n.data as any)?.canonicalPath || '').trim();
        if (cp) refToNodeId.set(cp, String(n.id || '').trim());
      }

      const newNodes: Node<HorizonNodeData>[] = [];
      const newEdges: Edge[] = [];
      const pathsToFetch = paths.filter((p) => !refToNodeId.has(String(p || '').trim()));
      const fragments = await Promise.all(
        pathsToFetch.map((p) => api.getHorizonFragmentByPath(p).catch(() => null))
      );

      for (let i = 0; i < pathsToFetch.length; i++) {
        const frag = fragments[i];
        const path = String(pathsToFetch[i] || '').trim();
        if (!path || refToNodeId.has(path)) continue;
        const nodeId = frag?.id || path;
        const label = frag?.labels?.en || frag?.displayLabel || frag?.name || path.split('/').pop() || path;
        newNodes.push({
          id: nodeId,
          type: 'horizonFragment',
          position: { x: 100 + (i % 4) * 220, y: 100 + Math.floor(i / 4) * 180 },
          data: {
            id: nodeId,
            fragmentId: frag?.id || nodeId,
            canonicalPath: path,
            facilityKind: readFacilityKindFromAny(frag),
            label,
            type: frag?.type || 'ORGANIZATION',
            state: frag?.state,
            showMetrics: showNodeMetrics,
            expanded: false,
            onLoadRelated: invokeLoadRelated,
            onCollapse: invokeCollapse,
            onScopeScout: invokeScopeScout,
          } as any,
        });
        refToNodeId.set(path, nodeId);
      }

      const payloadNodes = artifact.payload?.nodes || [];
      const payloadEdges = artifact.payload?.edges || [];
      const payloadLegs = artifact.payload?.legs || [];
      const pathStress = artifact.payload?.stress || null;
      const legByIndex = new Map<number, (typeof payloadLegs)[number]>();
      for (const leg of payloadLegs) {
        if (typeof leg?.index === 'number' && Number.isFinite(leg.index)) {
          legByIndex.set(leg.index, leg);
        }
      }
      const existingEdgeIds = new Set((edges || []).map((e) => String((e as any)?.id || '').trim()));
      const resolvePathEdgeType = (edge: any): string => {
        if (edge.type === 'ONTOLOGY' && edge.relation) return edge.relation;
        if (String(edge.role || '').trim() === 'logistics_leg' || edge.type === 'LOGISTICS') return 'OPERATES_VIA';
        if (edge.type === 'SUPPLY_LINK') return 'SUPPLIES_TO';
        return 'DEPENDS_ON';
      };
      const addEdge = (
        id: string,
        sourceId: string,
        targetId: string,
        edgeType: string,
        extras?: Record<string, any>,
      ) => {
        if (existingEdgeIds.has(id)) return;
        const baseStyle = getEdgeStyle(edgeType, undefined);
        const stroke = getEdgeStroke(baseStyle);
        const edgeCaption = typeof extras?.edgeCaption === 'string' && extras.edgeCaption.trim()
          ? extras.edgeCaption.trim()
          : getEdgeCaptionShort(edgeType);
        newEdges.push({
          id,
          source: sourceId,
          target: targetId,
          type: 'default',
          markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
          style: baseStyle,
          data: {
            edgeType,
            edgeCaption,
            ...(extras || {}),
          } as any,
        });
        existingEdgeIds.add(id);
      };
      for (const e of payloadEdges) {
        const fromRef = payloadNodes[e.from]?.ref;
        const toRef = payloadNodes[e.to]?.ref;
        if (!fromRef || !toRef) continue;
        const srcId = refToNodeId.get(fromRef);
        const trgId = refToNodeId.get(toRef);
        if (!srcId || !trgId) continue;
        const edgeType = resolvePathEdgeType(e);
        const segmentIndex = typeof e.segmentIndex === 'number' ? e.segmentIndex : undefined;
        const leg = typeof segmentIndex === 'number' ? legByIndex.get(segmentIndex) : undefined;
        const pathSufficiency: any = artifact.payload?.sufficiency || {};
        const pathFamily: any = artifact.payload?.family || {};
        const joinAssessments = Array.isArray(pathSufficiency?.joinAssessments) ? pathSufficiency.joinAssessments : [];
        const pathEdgeMeta = {
          pathArtifactId: artifact.id,
          pathRole: String(e.role || '').trim() || undefined,
          pathProductRef: String(e.productRef || '').trim() || undefined,
          pathCarriesProduct: Boolean(e.carriesProduct),
          pathSegmentIndex: segmentIndex,
          pathSupplyLinkId: String(e.supplyLinkId || '').trim() || undefined,
          pathQualifiers: e.qualifiers,
          pathStressScore: typeof leg?.stress?.stressScore === 'number' ? leg.stress.stressScore : pathStress?.pathScore,
          pathFragility: typeof leg?.stress?.fragility === 'number' ? leg.stress.fragility : pathStress?.fragility,
          pathRerouteability:
            typeof leg?.stress?.rerouteability === 'number' ? leg.stress.rerouteability : pathStress?.rerouteability,
          pathZonePressure:
            typeof leg?.stress?.zonePressure === 'number' ? leg.stress.zonePressure : pathStress?.zonePressure,
          pathAffectedZonePressure:
            typeof leg?.stress?.affectedZonePressure === 'number'
              ? leg.stress.affectedZonePressure
              : pathStress?.affectedZonePressure,
          pathRecoveryLatencyHours:
            typeof leg?.stress?.recoveryLatencyHours === 'number'
              ? leg.stress.recoveryLatencyHours
              : pathStress?.recoveryLatencyHours,
          pathBindingConfidence: typeof leg?.bindingConfidence === 'number' ? leg.bindingConfidence : undefined,
          pathFlowRefs: leg?.flowRefs || [],
          pathZoneRefs: leg?.zoneRefs || [],
          pathAffectedZoneRefs: leg?.affectedZoneRefs || [],
          pathSpatialSemantics: leg?.spatialSemantics || [],
          pathDominantFactors: leg?.stress?.dominantFactors || pathStress?.dominantFactors || [],
          pathVesselType: String(leg?.vesselType || e?.qualifiers?.vesselType || '').trim() || undefined,
          pathSufficiencyStatus: String(artifact.sufficiencyStatus || pathSufficiency?.status || '').trim() || undefined,
          pathSufficiencyScore:
            typeof artifact.sufficiencyScore === 'number'
              ? artifact.sufficiencyScore
              : (typeof pathSufficiency?.score === 'number' ? pathSufficiency.score : undefined),
          pathGapCount:
            typeof artifact.gapCount === 'number'
              ? artifact.gapCount
              : ((pathSufficiency?.openGaps?.length || 0) + (pathSufficiency?.hardFailures?.length || 0)),
          pathSufficiencySummary: String(pathSufficiency?.summary || '').trim() || undefined,
          pathHardFailures: pathSufficiency?.hardFailures || [],
          pathOpenGaps: pathSufficiency?.openGaps || [],
          pathWarnings: pathSufficiency?.warnings || [],
          pathJoinAssessments: joinAssessments,
          pathFamilyId: String(artifact.familyId || pathFamily?.id || '').trim() || undefined,
          pathFamilyPathCount:
            typeof artifact.familyPathCount === 'number'
              ? artifact.familyPathCount
              : (typeof pathFamily?.pathCount === 'number' ? pathFamily.pathCount : undefined),
          pathFamilyValidatedCount:
            typeof artifact.familyValidatedCount === 'number'
              ? artifact.familyValidatedCount
              : (typeof pathFamily?.validatedPathCount === 'number' ? pathFamily.validatedPathCount : undefined),
          pathFamilyGapCount:
            typeof artifact.familyGapCount === 'number'
              ? artifact.familyGapCount
              : (Array.isArray(pathFamily?.openGaps) ? pathFamily.openGaps.length : (typeof pathFamily?.gapPathCount === 'number' ? pathFamily.gapPathCount : undefined)),
          pathValidatedJoinCount:
            typeof artifact.validatedJoinCount === 'number'
              ? artifact.validatedJoinCount
              : joinAssessments.filter((item: any) => String(item?.status || '').trim() === 'validated').length,
          pathUnresolvedJoinCount:
            typeof artifact.unresolvedJoinCount === 'number'
              ? artifact.unresolvedJoinCount
              : joinAssessments.filter((item: any) => {
                  const status = String(item?.status || '').trim();
                  const product = String(item?.productContinuity || '').trim();
                  const temporal = String(item?.temporalContinuity || '').trim();
                  const reuse = String(item?.entityReuse || '').trim();
                  return status !== 'validated' && (product === 'unknown' || temporal === 'unknown' || reuse === 'unresolved');
                }).length,
          pathBottleneck:
            typeof pathStress?.bottleneckLegIndex === 'number' && typeof segmentIndex === 'number'
              ? pathStress.bottleneckLegIndex === segmentIndex
              : false,
          criticalSummary:
            typeof leg?.stress?.stressScore === 'number' || typeof pathStress?.pathScore === 'number'
              ? `${
                  (typeof pathStress?.bottleneckLegIndex === 'number' && typeof segmentIndex === 'number' && pathStress.bottleneckLegIndex === segmentIndex)
                    ? 'Bottleneck leg'
                    : 'Route leg'
                } stress ${(typeof leg?.stress?.stressScore === 'number' ? leg.stress.stressScore : (pathStress?.pathScore ?? 0)).toFixed(2)}`
              : undefined,
          primaryRiskMechanism:
            (typeof pathStress?.bottleneckLegIndex === 'number' && typeof segmentIndex === 'number' && pathStress.bottleneckLegIndex === segmentIndex)
              ? 'route_fragility'
              : (typeof leg?.stress?.stressScore === 'number' || typeof pathStress?.pathScore === 'number' ? 'path_stress' : undefined),
          primaryCriticalMetricKey:
            typeof leg?.stress?.stressScore === 'number' || typeof pathStress?.pathScore === 'number' ? 'pathStressScore' : undefined,
          primaryCriticalMetricLabel:
            typeof leg?.stress?.stressScore === 'number' || typeof pathStress?.pathScore === 'number' ? 'Path stress' : undefined,
          primaryCriticalMetricValue:
            typeof leg?.stress?.stressScore === 'number' ? leg.stress.stressScore : pathStress?.pathScore,
          riskFlags:
            (typeof pathStress?.bottleneckLegIndex === 'number' && typeof segmentIndex === 'number' && pathStress.bottleneckLegIndex === segmentIndex)
              ? ['route_fragility']
              : [],
          riskCoverageStatus: String(artifact.sufficiencyStatus || pathSufficiency?.status || '').trim() || undefined,
        } as Record<string, any>;

        const productIdx = e.productNodeIndex ?? e.viaProductIndex ?? (e as any).productNodeIndex ?? (e as any).viaProductIndex;
        if (
          edgeType === 'SUPPLIES_TO' &&
          typeof productIdx === 'number' &&
          productIdx >= 0 &&
          productIdx < payloadNodes.length
        ) {
          const productRef = payloadNodes[productIdx]?.ref;
          const productId = productRef ? refToNodeId.get(productRef) : undefined;
          if (productId && productId !== srcId && productId !== trgId) {
            addEdge(
              `supply:${artifact.id}:${e.from}-product${productIdx}`,
              srcId,
              productId,
              'PRODUCES',
              {
                ...pathEdgeMeta,
                edgeCaption: getEdgeCaptionShort('PRODUCES'),
              },
            );
            addEdge(
              `supply:${artifact.id}:product${productIdx}-${e.to}`,
              productId,
              trgId,
              'SUPPLIES_TO',
              {
                ...pathEdgeMeta,
                edgeCaption: getEdgeCaptionShort('SUPPLIES_TO'),
              },
            );
            continue;
          }
        }

        addEdge(
          `supply:${artifact.id}:${e.from}-${e.to}:${String(e.role || e.type || '').trim() || edgeType}`,
          srcId,
          trgId,
          edgeType,
          {
            ...pathEdgeMeta,
            edgeCaption:
              String(e.role || '').trim() === 'logistics_leg'
                ? 'Via'
                : getEdgeCaptionShort(edgeType),
          },
        );
      }

      setNodes((prev) => {
        const byId = new Map((prev || []).map((n) => [n.id, n]));
        for (const n of newNodes) {
          if (!byId.has(n.id)) byId.set(n.id, n);
        }
        return Array.from(byId.values());
      });
      setEdges((prev) => {
        const byId = new Map((prev || []).map((e) => [e.id, e]));
        for (const e of newEdges) {
          if (!byId.has(e.id)) byId.set(e.id, e);
        }
        return Array.from(byId.values());
      });
      invokeArrange();
    },
    [setNodes, setEdges, invokeArrange, getEdgeStyle, getEdgeStroke, getEdgeCaptionShort, showNodeMetrics, invokeLoadRelated, invokeCollapse, invokeScopeScout, edges]
  );

  // Track per-node expansions so we can "collapse" (undo expand) safely.
  type ExpansionRecord = {
    /** Node ids (fragments) that this expansion considers its neighborhood (new + existing related). */
    claimedNodeIds: Set<string>;
    /** Edge ids created/introduced by this expansion. */
    claimedEdgeIds: Set<string>;
    /** Snapshot of the expanded node before it was promoted/extracted (to restore on collapse). */
    prevCentral?: {
      nodeId: string;
      prevData: any;
      prevParentId?: string;
      prevExtent?: any;
    };
    /** Existing nodes whose grouping fields we changed (freeform absorb). */
    touchedNodes?: Map<
      string,
      {
        prevData: any;
        prevParentId?: string;
        prevExtent?: any;
      }
    >;
  };

  const baseNodeIdsRef = React.useRef<Set<string>>(new Set());
  const baseEdgeIdsRef = React.useRef<Set<string>>(new Set());
  const nodeClaimCountRef = React.useRef<Map<string, number>>(new Map());
  const edgeClaimCountRef = React.useRef<Map<string, number>>(new Map());
  const expansionsRef = React.useRef<Map<string, ExpansionRecord>>(new Map());

  const selectedNodeId = React.useMemo(() => {
    const selected = nodes.find((n) => (n as any)?.selected && !((n.data as any)?.isGroup));
    return selected?.id || null;
  }, [nodes]);

  const controlCorridorSeedNodeId = React.useMemo(() => {
    const selected = String(selectedNodeId || '').trim();
    if (selected && !isCorridorArtifactNodeId(selected)) return selected;
    const expanded = nodes.find((n) => {
      if (!n || (n.data as any)?.isGroup) return false;
      const id = String((n as any)?.id || '').trim();
      if (!id || isCorridorArtifactNodeId(id)) return false;
      return Boolean((n.data as any)?.expanded);
    });
    return String((expanded as any)?.id || '').trim() || '';
  }, [nodes, selectedNodeId]);

  // Normalize IDs coming from backend / firestore / UI (avoid whitespace or non-string surprises)
  const norm = (v: unknown) => String(v ?? '').trim();

  // Приоритет типов связей для сортировки (меньше = важнее = левее)
  const EDGE_TYPE_PRIORITY: Record<string, number> = {
    // 0. "Market-defining" relation — should dominate other semantics in the same pair
    'DOMINATES': 0,
    // 1. Конкуренция/Конфликт — самые важные
    'COMPETES_WITH': 1,
    'CONFLICTS_WITH': 1,
    'OPPOSES': 1,
    // 2. Влияние — очень важные
    'INFLUENCES': 2,
    'AFFECTS': 2,
    'REGULATES': 2,
    'IMPACTS': 2,
    // 3. Владение/Контроль — важные
    'OWNS': 3,
    'MANAGES': 3,
    'CONTROLS': 3,
    'INVESTS_IN': 3,
    'EMPLOYED_BY': 3,
    'BOARD_MEMBER_OF': 3,
    'FOUNDED': 3,
    'PARENT_OF': 3,
    'SUBSIDIARY_OF': 3,
    // 3.5. Hierarchies / skeleton links (market + gov + community)
    'HAS_MARKET_SEGMENT': 3,
    'HAS_SUBGROUP': 3,
    'ADMINISTERS': 3,
    'TRADED_IN': 3,
    // 4. Партнёрство/Поставки — средний приоритет
    'PARTNERS_WITH': 4,
    'COOPERATES_WITH': 4,
    'SUPPLIES_TO': 4,
    'SUPPORTS': 4,
    // 5. Локация — ниже среднего
    'LOCATED_IN': 5,
    'OPERATES_IN': 5,
    'BASED_IN': 5,
    'HEADQUARTERED_IN': 5,
    // 6. Зависимости — низкий приоритет
    'DEPENDS_ON': 6,
    'REQUIRES': 6,
    'USES': 6,
    'NEEDS': 6,
  };
  const DEFAULT_EDGE_PRIORITY = 7; // Для неизвестных типов

  const getEdgePriority = (edgeType: string | undefined): number => {
    const type = (edgeType || '').toUpperCase();
    return EDGE_TYPE_PRIORITY[type] ?? DEFAULT_EDGE_PRIORITY;
  };

  const normalizeEdgeType = useCallback((t: string | undefined) => (t || '').toUpperCase().trim(), []);

  // If two nodes are connected by both influence and dependency edges, we should present the influence edge
  // as the primary semantic (per product rules). This applies regardless of direction.
  // Likewise, LOCATED_IN should dominate DEPENDS-like relations for ORG/GOV -> GEO placements when directed correctly.
  const suppressDominatedEdges = useCallback(
    (edgesIn: Edge[] | null | undefined, nodesIn?: Node<HorizonNodeData>[] | null): Edge[] => {
      let edges = Array.isArray(edgesIn) ? edgesIn : [];
      if (edges.length === 0) return [];

      // Strong edge types that dominate DEPENDS_ON when in the opposite direction
      // (e.g. ORG→PROGRAM PRODUCES + PROGRAM→ORG Depends → suppress Depends)
      const STRONG_VS_DEPENDS = new Set([
        'PRODUCES', 'SUPPLIES_TO', 'TRANSFORMS_INTO', 'PARTICIPATES_IN',
        'TRADED_IN',
        'PARTNERS_WITH', 'COOPERATES_WITH', 'SUPPORTS', 'INFLUENCES', 'AFFECTS', 'IMPACTS', 'REGULATES',
        'OWNS', 'MANAGES', 'CONTROLS', 'INVESTS_IN', 'FOUNDED', 'EMPLOYED_BY', 'BOARD_MEMBER_OF',
      ]);
      const dirKey = (a: string, b: string) => `${norm(a)}→${norm(b)}`;
      const strongReverse = new Set<string>();
      for (const e of edges as any[]) {
        const et = String((e?.data?.edgeType || e?.label || '').trim()).toUpperCase();
        if (STRONG_VS_DEPENDS.has(et)) {
          strongReverse.add(dirKey(e.target, e.source));
        }
      }
      edges = edges.filter((e) => {
        const et = String((e as any)?.data?.edgeType || (e as any)?.label || '').trim().toUpperCase();
        if (et !== 'DEPENDS_ON') return true;
        // Suppress DEPENDS_ON only when there is a stronger edge in the opposite direction
        return !strongReverse.has(dirKey((e as any).source, (e as any).target));
      });
      if (edges.length === 0) return [];

      const nodeById = new Map<string, any>();
      if (Array.isArray(nodesIn)) {
        for (const n of nodesIn) {
          const id = norm((n as any)?.id);
          if (!id) continue;
          nodeById.set(id, n);
        }
      }

      const typeOfNode = (id: string): string => {
        const n = nodeById.get(norm(id));
        const t = String((n as any)?.data?.type || '').trim();
        return t ? t.toUpperCase().replace(/\s+/g, '_') : '';
      };
      const cpOfNode = (id: string): string => {
        const n = nodeById.get(norm(id));
        return String((n as any)?.data?.canonicalPath || '').trim();
      };
      const isOrgLike = (id: string): boolean => {
        const t = typeOfNode(id);
        if (t === 'ORGANIZATION') return true;
        const cp = cpOfNode(id);
        return Boolean(cp && (cp.startsWith('/org/') || cp.startsWith('/gov/')));
      };
      const isGeoLike = (id: string): boolean => {
        const t = typeOfNode(id);
        if (t === 'GEO_REGION' || t === 'LOCATION') return true;
        if (t && t.includes('GEO')) return true;
        const cp = cpOfNode(id);
        return Boolean(cp && cp.startsWith('/loc/'));
      };
      const isMarketLike = (id: string): boolean => {
        const t = typeOfNode(id);
        if (t === 'MARKET') return true;
        const cp = cpOfNode(id);
        return Boolean(cp && cp.startsWith('/market/'));
      };
      const isTribeLike = (id: string): boolean => {
        const t = typeOfNode(id);
        if (t === 'TRIBE' || t === 'COMMUNITY_TRIBE') return true;
        const cp = cpOfNode(id);
        if (!cp) return false;
        // Support both /tribe/... and /community/.../tribe/... formats
        // Check for /community/{geo}/tribe/{slug} pattern
        if (cp.startsWith('/community/')) {
          const parts = cp.split('/').filter(Boolean);
          if (parts.length >= 3 && parts[2] === 'tribe') return true;
        }
        return cp.startsWith('/tribe/') || cp.includes('/tribe/');
      };
      const isDynastyLike = (id: string): boolean => {
        const t = typeOfNode(id);
        if (t === 'DYNASTY' || t === 'COMMUNITY_DYNASTY') return true;
        const cp = cpOfNode(id);
        if (!cp) return false;
        // Support both /dynasty/... and /community/.../dynasty/... formats
        // Check for /community/{geo}/dynasty/{slug} pattern
        if (cp.startsWith('/community/')) {
          const parts = cp.split('/').filter(Boolean);
          if (parts.length >= 3 && parts[2] === 'dynasty') return true;
        }
        return cp.startsWith('/dynasty/') || cp.includes('/dynasty/');
      };

      const INFL = new Set(['INFLUENCES', 'AFFECTS', 'IMPACTS', 'REGULATES']);
      const DEP = new Set(['DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM']);
      const PARTNER = new Set(['PARTNERS_WITH', 'COOPERATES_WITH']);
      const DOM = new Set(['DOMINATES']);
      const isInfluenceType = (et: string): boolean => {
        if (!et) return false;
        if (INFL.has(et)) return true;
        return et.includes('INFLUENC');
      };
      const bestPartner = (types: Set<string>): string => {
        // Prefer PARTNERS_WITH over COOPERATES_WITH for UI stability.
        if (types.has('PARTNERS_WITH')) return 'PARTNERS_WITH';
        if (types.has('COOPERATES_WITH')) return 'COOPERATES_WITH';
        return 'PARTNERS_WITH';
      };
      const bestInfluence = (types: Set<string>): string => {
        // Prefer the most specific label for UI stability.
        if (types.has('INFLUENCES')) return 'INFLUENCES';
        if (types.has('AFFECTS')) return 'AFFECTS';
        if (types.has('IMPACTS')) return 'IMPACTS';
        if (types.has('REGULATES')) return 'REGULATES';
        return 'INFLUENCES';
      };
      const pairKey = (a: string, b: string) => {
        const x = norm(a);
        const y = norm(b);
        return x < y ? `${x}||${y}` : `${y}||${x}`;
      };
      const edgeTypeOf = (e: any): string => {
        return normalizeEdgeType((e as any)?.data?.edgeType || (e as any)?.data?.subkind || (e as any)?.label || (e as any)?.type);
      };

      // Build type sets per unordered pair.
      const typesByPair = new Map<string, Set<string>>();
      const hasLocatedOutByPair = new Map<string, boolean>(); // ORG/GOV -> GEO LOCATED_IN exists
      const hasDominatesOrgMarketByPair = new Map<string, boolean>(); // org<->market has DOMINATES
      const influenceDirByPair = new Map<string, { tribeToDynasty: boolean; dynastyToTribe: boolean }>();
      for (const e of edges as any[]) {
        const s = norm(e?.source);
        const t = norm(e?.target);
        if (!s || !t || s === t) continue;
        const et = edgeTypeOf(e);
        if (!et) continue;
        const k = pairKey(s, t);
        const set = typesByPair.get(k) || new Set<string>();
        set.add(et);
        typesByPair.set(k, set);
        if (et === 'LOCATED_IN') {
          if (isOrgLike(s) && isGeoLike(t)) {
            hasLocatedOutByPair.set(k, true);
          }
        }
        if (et === 'DOMINATES') {
          if ((isOrgLike(s) && isMarketLike(t)) || (isMarketLike(s) && isOrgLike(t))) {
            hasDominatesOrgMarketByPair.set(k, true);
          }
        }
        if (isInfluenceType(et)) {
          const isTribeToDynasty = isTribeLike(s) && isDynastyLike(t);
          const isDynastyToTribe = isDynastyLike(s) && isTribeLike(t);
          if (isTribeToDynasty || isDynastyToTribe) {
            const cur = influenceDirByPair.get(k) || { tribeToDynasty: false, dynastyToTribe: false };
            if (isTribeToDynasty) cur.tribeToDynasty = true;
            if (isDynastyToTribe) cur.dynastyToTribe = true;
            influenceDirByPair.set(k, cur);
          }
        }
      }

      const mutualInfluenceCommunityPairs = new Set<string>();
      for (const [k, dir] of influenceDirByPair.entries()) {
        if (dir.tribeToDynasty && dir.dynastyToTribe) {
          mutualInfluenceCommunityPairs.add(k);
        }
      }

      const winnerByPair = new Map<string, string>(); // if set, suppress others (dep-like / non-winner influence)
      for (const [k, set] of typesByPair.entries()) {
        const hasDominates = Array.from(set).some((t) => DOM.has(t));
        if (hasDominates && Boolean(hasDominatesOrgMarketByPair.get(k)) && set.size > 1) {
          // Product rule: for org<->market links, DOMINATES is primary and suppresses all other semantics.
          winnerByPair.set(k, 'DOMINATES');
          continue;
        }
        const hasPartner = Array.from(set).some((t) => PARTNER.has(t));
        const hasInfluence = Array.from(set).some((t) => INFL.has(t));
        const hasDep = Array.from(set).some((t) => DEP.has(t));
        // Partnership dominates both influence and dependency (product rule).
        if (hasPartner && (hasDep || hasInfluence)) {
          winnerByPair.set(k, bestPartner(set));
          continue;
        }
        if (hasInfluence && hasDep) {
          winnerByPair.set(k, bestInfluence(set));
          continue;
        }
        const hasLocatedOut = Boolean(hasLocatedOutByPair.get(k));
        if (hasLocatedOut && hasDep) {
          winnerByPair.set(k, 'LOCATED_IN');
          continue;
        }
      }

      // Filter edges by winner and mutual influence suppression.
      const out: Edge[] = [];
      for (const e of edges as any[]) {
        const s = norm(e?.source);
        const t = norm(e?.target);
        if (!s || !t || s === t) {
          out.push(e);
          continue;
        }
        const k = pairKey(s, t);
        const et = edgeTypeOf(e);
        // Suppress dynasty -> tribe influence when mutual influence exists (keep only tribe -> dynasty).
        if (mutualInfluenceCommunityPairs.has(k) && isInfluenceType(et)) {
          if (!(isTribeLike(s) && isDynastyLike(t))) {
            continue; // Skip dynasty -> tribe, keep only tribe -> dynasty
          }
        }
        const win = winnerByPair.get(k);
        if (!win) {
          out.push(e);
          continue;
        }
        if (et === win) {
          out.push(e);
          continue;
        }
        // If DOMINATES is the winner, suppress all other edges for the pair.
        if (win === 'DOMINATES') continue;
        // Suppress dependency edges when influence or located_in dominates.
        if (DEP.has(et)) continue;
        // Suppress alternate influence labels when a winner is chosen (keeps one clean semantic edge).
        if (INFL.has(et) && INFL.has(win)) continue;
        // Suppress influence edges when partnership dominates.
        if (INFL.has(et) && PARTNER.has(win)) continue;
        out.push(e);
      }

      return out;
    },
    [norm, normalizeEdgeType],
  );

  /**
   * Determine source and target handles based on semantic group.
   * Handles are assigned to edge.source and edge.target nodes respectively.
   * 
   * For vertical groups (top/bottom): always bottom → top
   * For horizontal groups (left/right): depends on who is source in the edge data
   * 
   * Правила привязки edges к сторонам центральной ноды:
   * - Левая группа (market): из левой грани центральной ноды в правую грань ноды в группе
   * - Правая группа (context): из правой грани центральной ноды в левую грань ноды в группе
   * - Нижняя группа (customers): из нижней грани центральной ноды в верхнюю грань ноды в группе
   * - Верхняя группа (supply-chain): из верхней грани центральной ноды в нижнюю грань ноды в группе
   * 
   * Направление связи определяет, какой handle использовать (source или target).
   * Эти правила применяются только для связей внутри одного grid.
   */
  const getEdgeHandles = useCallback((edgeType: string | undefined, isOutgoing: boolean): { sourceHandle: string; targetHandle: string } => {
    const sg = getSemanticGroup(edgeType, isOutgoing);
    const position = SEMANTIC_GROUP_POSITION[sg];
    
    switch (position) {
      case 'top':
        // Верхняя группа (supply-chain): из верхней грани центральной ноды в нижнюю грань ноды в группе
        if (isOutgoing) {
          // edge: parent(source) → child(target)
          // source node = parent → use top handle
          // target node = child → use bottom handle
          return { sourceHandle: 'source-top', targetHandle: 'target-bottom' };
        } else {
          // edge: child(source) → parent(target)
          // source node = child → use bottom handle (towards parent on top)
          // target node = parent → use top handle (from child on bottom)
          return { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
        }
      case 'bottom':
        // Нижняя группа (customers): из нижней грани центральной ноды в верхнюю грань ноды в группе
        if (isOutgoing) {
          // edge: parent(source) → child(target)
          // source node = parent → use bottom handle
          // target node = child → use top handle
          return { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
        } else {
          // edge: child(source) → parent(target)
          // source node = child → use top handle (towards parent on bottom)
          // target node = parent → use bottom handle (from child on top)
          return { sourceHandle: 'source-top', targetHandle: 'target-bottom' };
        }
      case 'left':
        // Левая группа (market): из левой грани центральной ноды в правую грань ноды в группе
        if (isOutgoing) {
          // edge: parent(source) → child(target)
          // source node = parent → use left handle
          // target node = child → use right handle
          return { sourceHandle: 'source-left', targetHandle: 'target-right' };
        } else {
          // edge: child(source) → parent(target)
          // source node = child → use right handle (towards parent on left)
          // target node = parent → use left handle (from child on right)
          return { sourceHandle: 'source-right', targetHandle: 'target-left' };
        }
      case 'right':
        // Правая группа (context): из правой грани центральной ноды в левую грань ноды в группе
        if (isOutgoing) {
          // edge: parent(source) → child(target)
          // source node = parent → use right handle
          // target node = child → use left handle
          return { sourceHandle: 'source-right', targetHandle: 'target-left' };
        } else {
          // edge: child(source) → parent(target)
          // source node = child → use left handle (towards parent on right)
          // target node = parent → use right handle (from child on left)
          return { sourceHandle: 'source-left', targetHandle: 'target-right' };
        }
      default:
        return { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
    }
  }, []);

  /**
   * Recalculate edge handles for Linear layout.
   * - Bottom groups (bottom-left, bottom-right): group nodes use top handles, central uses bottom
   * - Top groups (top-left, top-right): group nodes use bottom handles, central uses top
   */
  const recalculateLinearHandles = useCallback((
    edgeList: Edge[],
    nodeList: Node<HorizonNodeData>[]
  ): Edge[] => {
    // Build lookup maps
    const nodeMap = new Map<string, Node<HorizonNodeData>>();
    
    for (const n of nodeList) {
      const d = n.data as any;
      if (d?.isGroup) continue;
      nodeMap.set(n.id, n);
      // Also map by fragmentId if available
      if (d?.fragmentId) {
        nodeMap.set(d.fragmentId, n);
      }
    }
    
    return edgeList.map((e) => {
      const sourceNode = nodeMap.get(e.source);
      const targetNode = nodeMap.get(e.target);
      
      if (!sourceNode || !targetNode) {
        return e;
      }
      
      const sourceData = sourceNode.data as any;
      const targetData = targetNode.data as any;
      const sourceIsCentral = sourceData?.depth === 0;
      const targetIsCentral = targetData?.depth === 0;
      
      // Default handles
      let handles = { sourceHandle: e.sourceHandle || 'source-bottom', targetHandle: e.targetHandle || 'target-top' };
      
      if (sourceIsCentral && targetIsCentral) {
        // central → central: route edges through TOP handles with arc (inverted parabola)
        // Use custom arcTop edge type for the curved appearance
        return {
          ...e,
          type: 'arcTop',
          sourceHandle: 'source-top',
          targetHandle: 'target-top',
        };
      } else if (sourceIsCentral && !targetIsCentral) {
        // central → group node
        const targetEdgeType = targetData?.edgeTypeFromParent || (e.data as any)?.edgeType || '';
        const targetDirection = targetData?.directionFromParent;
        const targetIsOutgoing = targetDirection !== 'incoming';
        const section = getLinearSection(targetEdgeType, targetIsOutgoing);
        
        if (section === 'bottom-left' || section === 'bottom-right') {
          // Bottom group: central uses bottom handle, group node uses top
          handles = { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
        } else {
          // Top group: central uses top handle, group node uses bottom
          handles = { sourceHandle: 'source-top', targetHandle: 'target-bottom' };
        }
        // no-op: avoid noisy logs in production
      } else if (targetIsCentral && !sourceIsCentral) {
        // group node → central
        const sourceEdgeType = sourceData?.edgeTypeFromParent || (e.data as any)?.edgeType || '';
        const sourceDirection = sourceData?.directionFromParent;
        const sourceIsOutgoing = sourceDirection !== 'incoming';
        const section = getLinearSection(sourceEdgeType, sourceIsOutgoing);
        
        if (section === 'bottom-left' || section === 'bottom-right') {
          // Bottom group: group node uses top handle, central uses bottom
          handles = { sourceHandle: 'source-top', targetHandle: 'target-bottom' };
        } else {
          // Top group: group node uses bottom handle, central uses top
          handles = { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
        }
        // no-op: avoid noisy logs in production
      }
      
      return { ...e, ...handles };
    });
  }, []);

  /**
   * Recalculate edge handles for Levels (Hierarchical) Two-Pane layout.
   *
   * Contract:
   * - Nodes inside group containers (left hierarchies) always use RIGHT-side handles.
   * - Right-side nodes choose LEFT/RIGHT handle based on relative horizontal position (level) and edge direction.
   *   - If the other node is to the right => connect via right side (source-right / target-left).
   *   - If the other node is to the left  => connect via left side (source-left / target-right).
   *   - If X is approximately equal, fall back to vertical (top/bottom) based on Y.
   */
  const recalculateLevelsTwoPaneHandles = useCallback((
    edgeList: Edge[],
    nodeList: Node<HorizonNodeData>[]
  ): Edge[] => {
    // Always rebuild stack-proxy edges; remove prior ones to avoid stale edges.
    const baseEdgeList = (edgeList || []).filter((e: any) => {
      const id = String(e?.id || '').trim();
      const d: any = e?.data || {};
      if (d.levelsStackProxy) return false;
      if (id.startsWith('levels:stackproxy:')) return false;
      // Also rebuild group-proxy edges (group↔outside via group handlers).
      if (d.levelsGroupProxy) return false;
      if (id.startsWith('levels:gproxy:')) return false;
      return true;
    });

    const nodeMap = new Map<string, Node<HorizonNodeData>>();
    const groupMap = new Map<string, Node<HorizonNodeData>>();
    for (const n of nodeList) {
      const d: any = n.data;
      if (d?.isGroup) {
        groupMap.set(String(n.id || '').trim(), n);
        continue;
      }
      nodeMap.set(n.id, n);
      if (d?.fragmentId) nodeMap.set(String(d.fragmentId), n);
    }
    const absolutePosCache = new Map<string, { x: number; y: number }>();
    const getAbsolutePos = (node: any, depth = 0): { x: number; y: number } => {
      const id = String(node?.id || '').trim();
      if (id) {
        const cached = absolutePosCache.get(id);
        if (cached) return cached;
      }
      // IMPORTANT:
      // In relayout flows, `positionAbsolute` can be stale (ReactFlow-internal snapshot from a previous pass).
      // For handle routing we must prioritize the fresh layout coordinates (`position` + parent chain).
      // Otherwise X-order can flip logically and produce backward hooks.
      let x = Number((node as any)?.position?.x);
      let y = Number((node as any)?.position?.y);
      const hasLocalPos = Number.isFinite(x) && Number.isFinite(y);
      if (!Number.isFinite(x)) x = 0;
      if (!Number.isFinite(y)) y = 0;
      if (!hasLocalPos) {
        const ax = Number((node as any)?.positionAbsolute?.x);
        const ay = Number((node as any)?.positionAbsolute?.y);
        if (Number.isFinite(ax) && Number.isFinite(ay)) {
          const out = { x: ax, y: ay };
          if (id) absolutePosCache.set(id, out);
          return out;
        }
      }
      if (depth < 12) {
        const pid = String((node as any)?.parentId || (node as any)?.parentNode || '').trim();
        if (pid) {
          const parent = nodeById.get(pid) || nodeMap.get(pid) || groupMap.get(pid);
          if (parent) {
            const pp = getAbsolutePos(parent, depth + 1);
            x += pp.x;
            y += pp.y;
          }
        }
      }
      const out = { x, y };
      if (id) absolutePosCache.set(id, out);
      return out;
    };

    const eps = 8; // px threshold to treat as "same column"
    // Wider ambiguity band for side handles to avoid unnatural "hook" turns near vertical alignment.
    const nearColumnBand = 56;
    const horizontalHandlesByX = (
      sx: number,
      tx: number,
      prefer: 'toRight' | 'toLeft',
    ): { sourceHandle: string; targetHandle: string } => {
      if (tx > sx + nearColumnBand) return { sourceHandle: 'source-right', targetHandle: 'target-left' };
      if (tx < sx - nearColumnBand) return { sourceHandle: 'source-left', targetHandle: 'target-right' };
      return prefer === 'toRight'
        ? { sourceHandle: 'source-right', targetHandle: 'target-left' }
        : { sourceHandle: 'source-left', targetHandle: 'target-right' };
    };

    const isInGroup = (n: Node<HorizonNodeData>) => {
      const pid = (n as any).parentId || (n as any).parentNode;
      return Boolean(pid);
    };

    const normalizedEdgeTypeOf = (e: any): string => {
      const raw = (e as any)?.data?.edgeType || (e as any)?.data?.subkind || (e as any)?.label || (e as any)?.type || '';
      return String(raw || '').toUpperCase().trim() || 'OTHER';
    };

    const normalizeTypeKey = (t: any) => String(t || '').toUpperCase().trim().replace(/\s+/g, '_') || 'UNKNOWN';
    const corridorLaneRankForNode = (n: Node<HorizonNodeData> | undefined): number => {
      if (!n) return -1;
      const d: any = n.data || {};
      if (d?.isGroup) return -1;
      // Fallback to lane-based rank (authority rightmost = highest rank, propagation leftmost = lowest).
      const lane = String(d?.corridorLane || '').trim().toLowerCase();
      if (lane === 'propagation' || lane === 'impact') return 0;
      if (lane === 'execution' || lane === 'decision' || lane === 'mechanism' || lane === 'mediator') return 1;
      if (lane === 'authority') return 2;

      const parentEdgeType = normalizeTypeKey(d?.edgeTypeFromParent || '');
      if (parentEdgeType === 'CORRIDOR_IMPACT') return 0;
      if (
        parentEdgeType === 'CORRIDOR_DECISION' ||
        parentEdgeType === 'CORRIDOR_MECHANISM' ||
        parentEdgeType === 'CORRIDOR_MEDIATOR'
      ) return 1;
      if (parentEdgeType === 'CORRIDOR_AUTHORITY') return 2;

      const tk = normalizeTypeKey(d?.type || '');
      if (tk === 'DECISION_POINT' || tk === 'MECHANISM') return 1;
      if (tk === 'ORGANIZATION' || tk === 'PERSON' || tk === 'COMMUNITY') return 2;
      return -1;
    };
    const isDecisionCorridorNode = (n: Node<HorizonNodeData> | undefined): boolean =>
      corridorLaneRankForNode(n) >= 0;
    const corridorDepthForNode = (n: Node<HorizonNodeData> | undefined): number => {
      if (!n) return Number.POSITIVE_INFINITY;
      const d: any = n.data || {};
      const raw = Number(d?.corridorDepth);
      return Number.isFinite(raw) ? raw : Number.POSITIVE_INFINITY;
    };
    const corridorDirectionHint = (
      sourceNode: Node<HorizonNodeData>,
      targetNode: Node<HorizonNodeData>,
      sx: number,
      tx: number,
    ): 'toLeft' | 'toRight' => {
      // First honor actual on-canvas geometry; this prevents long wraparounds
      // when semantic lane/depth hints disagree with final packed positions.
      const corridorBand = 24;
      if (tx < sx - corridorBand) return 'toLeft';
      if (tx > sx + corridorBand) return 'toRight';

      const sLane = corridorLaneRankForNode(sourceNode);
      const tLane = corridorLaneRankForNode(targetNode);
      // Lane order is authoritative for Decision Chain:
      // authority (right) -> execution -> propagation (left).
      if (sLane >= 0 && tLane >= 0 && sLane !== tLane) return sLane > tLane ? 'toLeft' : 'toRight';

      const sDepth = corridorDepthForNode(sourceNode);
      const tDepth = corridorDepthForNode(targetNode);
      // Within one lane, keep nearer-to-seed nodes on the right.
      if (Number.isFinite(sDepth) && Number.isFinite(tDepth) && sDepth !== tDepth) {
        return sDepth < tDepth ? 'toLeft' : 'toRight';
      }

      if (tx < sx - eps) return 'toLeft';
      if (tx > sx + eps) return 'toRight';
      // Deterministic fallback for near-vertical pairs (avoid top/bottom hooks in corridor mode).
      return 'toLeft';
    };
    const corridorHandlesForPair = (
      sourceNode: Node<HorizonNodeData>,
      targetNode: Node<HorizonNodeData>,
      sx: number,
      sy: number,
      tx: number,
      ty: number,
    ): { sourceHandle: string; targetHandle: string } => {
      void sy;
      void ty;
      return corridorDirectionHint(sourceNode, targetNode, sx, tx) === 'toLeft'
        ? { sourceHandle: 'source-left', targetHandle: 'target-right' }
        : { sourceHandle: 'source-right', targetHandle: 'target-left' };
    };
    const PEER_EDGE_TYPES = new Set<string>([
      'COMPETES_WITH',
      'CONFLICTS_WITH',
      'OPPOSES',
      'PARTNERS_WITH',
      'COOPERATES_WITH',
      'RELATED_TO',
      'RELATIVE_OF',
      'SIMILAR_TO',
      'LINKED_TO',
    ]);

    // === Levels right-stack edge proxying ===
    // Stacked nodes are hidden (and `visibleEdges` filters out edges incident to hidden nodes).
    // To preserve connectivity, we generate proxy edges that connect every external neighbor
    // to the stack controller, preserving edge type + direction.
    const stackControllerByHiddenId = new Map<string, string>();
    for (const n of nodeList) {
      const d: any = n.data;
      if (!d || d.isGroup) continue;
      if (!d.hiddenByStack) continue;
      const ctrl = String((d as any)?.stackControllerId || '').trim();
      if (!ctrl) continue;
      stackControllerByHiddenId.set(String(n.id || '').trim(), ctrl);
    }
    const stackProxyEdges: Edge[] = [];
    if (stackControllerByHiddenId.size > 0) {
      const seenProxy = new Set<string>();
      for (const e of baseEdgeList) {
        const eidRaw = String((e as any)?.id || '').trim();
        if (eidRaw.startsWith('levels:agg:')) continue;
        const ed: any = (e as any)?.data || {};
        if (ed.levelsAgg) continue;
        if (ed.levelsStackProxy) continue;
        const s = String((e as any)?.source || '').trim();
        const t = String((e as any)?.target || '').trim();
        if (!s || !t) continue;

        const sCtrl = stackControllerByHiddenId.get(s);
        const tCtrl = stackControllerByHiddenId.get(t);
        if (!sCtrl && !tCtrl) continue;

        const edgeType = normalizedEdgeTypeOf(e);
        const baseStyle = (ed as any)?.baseStyle || getEdgeStyle(edgeType, (ed as any)?.criticality, ed);
        const stroke = getEdgeStroke(baseStyle);

        // Source hidden -> proxy from controller to target
        if (sCtrl) {
          const proxySource = sCtrl;
          const proxyTarget = tCtrl ? tCtrl : t;
          if (proxySource && proxyTarget && proxySource !== proxyTarget) {
            const pid = `levels:stackproxy:${proxySource}->${proxyTarget}:edge:${edgeType}`;
            if (!seenProxy.has(pid)) {
              seenProxy.add(pid);
              stackProxyEdges.push({
                id: pid,
                source: proxySource,
                target: proxyTarget,
                type: 'hzn',
                interactionWidth: 18,
                markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
                style: baseStyle,
                data: {
                  ...ed,
                  edgeType,
                  edgeCaption: getEdgeCaptionShort(edgeType),
                  baseStyle,
                  levelsStackProxy: true,
                  levelsStackProxyFromHidden: s,
                  levelsStackControllerId: sCtrl,
                } as any,
              } as any);
            }
          }
        }

        // Target hidden -> proxy from source to controller
        if (tCtrl) {
          const proxySource = sCtrl ? sCtrl : s;
          const proxyTarget = tCtrl;
          if (proxySource && proxyTarget && proxySource !== proxyTarget) {
            const pid = `levels:stackproxy:${proxySource}->${proxyTarget}:edge:${edgeType}`;
            if (!seenProxy.has(pid)) {
              seenProxy.add(pid);
              stackProxyEdges.push({
                id: pid,
                source: proxySource,
                target: proxyTarget,
                type: 'hzn',
                interactionWidth: 18,
                markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
                style: baseStyle,
                data: {
                  ...ed,
                  edgeType,
                  edgeCaption: getEdgeCaptionShort(edgeType),
                  baseStyle,
                  levelsStackProxy: true,
                  levelsStackProxyToHidden: t,
                  levelsStackControllerId: tCtrl,
                } as any,
              } as any);
            }
          }
        }
      }
    }

    const edgeListWithStackProxy = stackProxyEdges.length > 0 ? [...baseEdgeList, ...stackProxyEdges] : baseEdgeList;

    const stripAggFlags = (d0: any) => {
      const d = { ...(d0 || {}) };
      delete d.levelsHiddenByAggregation;
      delete d.levelsAggKey;
      return d;
    };

    // === Levels Two-Pane aggregation:
    // If we have left group containers (levels:left:*), collapse per-child edges (parent<->child)
    // into one edge per EDGE TYPE handle (show all relation types).
    const aggEdges: Edge[] = [];
    const perChildEdgeIdsToAggKey = new Map<string, string>(); // edgeId -> aggKey (used for hover reveal)
    const noHoverRevealEdgeIds = new Set<string>(); // edges that should stay hidden even on hover (proxied to group)

    // Build child lookup by *root* Levels-left groupId (levels:left:*), even when nodes are nested
    // inside additional container nodes (e.g. type columns).
    const childrenByGroupId = new Map<string, Node<HorizonNodeData>[]>();
    const groupIdByChildId = new Map<string, string>(); // child node id -> root groupId
    const nodeById = new Map<string, any>();
    for (const n of nodeList) nodeById.set(String(n.id || '').trim(), n);
    const rootLeftCache = new Map<string, string | null>(); // parentId -> resolved levels:left:* or null
    const resolveLevelsLeftGroupId = (startPid: string): string | null => {
      let cur = String(startPid || '').trim();
      const cached = rootLeftCache.get(cur);
      if (cached !== undefined) return cached;
      // Bound the walk to avoid accidental cycles.
      for (let i = 0; i < 12; i++) {
        if (!cur) return null;
        if (cur.startsWith('levels:left:')) return cur;
        const pn: any = nodeById.get(cur);
        cur = String(pn?.parentId || pn?.parentNode || '').trim();
      }
      rootLeftCache.set(String(startPid || '').trim(), null);
      return null;
    };
    for (const n of nodeList) {
      const d: any = n.data;
      if (d?.isGroup) continue;
      const pid0 = String((n as any).parentId || (n as any).parentNode || '').trim();
      if (!pid0) continue;
      const rootLeft = resolveLevelsLeftGroupId(pid0);
      const gid = rootLeft || pid0;
      const arr = childrenByGroupId.get(gid) || [];
      arr.push(n);
      childrenByGroupId.set(gid, arr);
      groupIdByChildId.set(String(n.id || '').trim(), gid);
    }

    // Helper maps for Levels left groups.
    const groupParentByGroupId = new Map<string, string>(); // groupId -> parentId (right backbone node)
    const portHandlesByGroupId = new Map<string, Map<string, { sourceHandleId: string; targetHandleId: string }>>();
    for (const [gid, gnode] of groupMap.entries()) {
      if (!gid.startsWith('levels:left:')) continue;
      const gd: any = gnode.data || {};
      const parentId = String(gd?.groupParentId || '').trim();
      if (parentId) groupParentByGroupId.set(gid, parentId);
      const ports = Array.isArray(gd?.groupEdgePorts) ? (gd.groupEdgePorts as any[]) : [];
      const edgeTypesFallback = Array.isArray(gd?.groupEdgeTypes) ? (gd.groupEdgeTypes as any[]) : [];
      const m = new Map<string, { sourceHandleId: string; targetHandleId: string }>();
      if (ports.length > 0) {
        for (const p of ports) {
          const et = normalizedEdgeTypeOf({ data: { edgeType: p?.edgeType } } as any);
          const targetHandleId = String(p?.targetHandleId || `edge:${et}:target`).trim();
          const sourceHandleId = String(p?.sourceHandleId || `edge:${et}:source`).trim();
          if (et) m.set(et, { sourceHandleId, targetHandleId });
        }
      } else {
        for (const etRaw of edgeTypesFallback) {
          const et = normalizedEdgeTypeOf({ data: { edgeType: etRaw } } as any);
          if (!et) continue;
          m.set(et, { sourceHandleId: `edge:${et}:source`, targetHandleId: `edge:${et}:target` });
        }
      }
      portHandlesByGroupId.set(gid, m);
    }

    // Helper maps for right-side stack groups (expanded stacks).
    const stackGroupMemberIds = new Map<string, Set<string>>(); // stackGroupId -> Set of member node ids
    const stackGroupByMemberId = new Map<string, string>(); // memberId -> stackGroupId
    const stackGroupPortsByGroupId = new Map<string, Map<string, { sourceHandleId: string; targetHandleId: string }>>();
    for (const [gid, gnode] of groupMap.entries()) {
      if (!gid.startsWith('stack:group:')) continue;
      const gd: any = gnode.data || {};
      const memberIds = Array.isArray(gd?.groupAllChildIds) ? (gd.groupAllChildIds as string[]) : [];
      const memberSet = new Set<string>();
      for (const mid of memberIds) {
        const id = String(mid || '').trim();
        if (id) {
          memberSet.add(id);
          stackGroupByMemberId.set(id, gid);
        }
      }
      stackGroupMemberIds.set(gid, memberSet);

      // Build port handles for the stack group
      const ports = Array.isArray(gd?.groupEdgePorts) ? (gd.groupEdgePorts as any[]) : [];
      const edgeTypesFallback = Array.isArray(gd?.groupEdgeTypes) ? (gd.groupEdgeTypes as any[]) : [];
      const m = new Map<string, { sourceHandleId: string; targetHandleId: string }>();
      if (ports.length > 0) {
        for (const p of ports) {
          const et = normalizedEdgeTypeOf({ data: { edgeType: p?.edgeType } } as any);
          const targetHandleId = String(p?.targetHandleId || `edge:${et}:target`).trim();
          const sourceHandleId = String(p?.sourceHandleId || `edge:${et}:source`).trim();
          if (et) m.set(et, { sourceHandleId, targetHandleId });
        }
      } else {
        for (const etRaw of edgeTypesFallback) {
          const et = normalizedEdgeTypeOf({ data: { edgeType: etRaw } } as any);
          if (!et) continue;
          m.set(et, { sourceHandleId: `edge:${et}:source`, targetHandleId: `edge:${et}:target` });
        }
      }
      // Add a default handle for any edge type
      if (!m.has('OTHER')) {
        m.set('OTHER', { sourceHandleId: 'source-left', targetHandleId: 'target-left' });
      }
      stackGroupPortsByGroupId.set(gid, m);
    }

    for (const [gid, gnode] of groupMap.entries()) {
      // Only target the Levels Two-Pane left blocks (created by layoutLevelsTwoPane).
      if (!gid.startsWith('levels:left:')) continue;
      const gd: any = gnode.data;
      const parentId = String(gd?.groupParentId || '').trim();
      if (!parentId) continue;
      const ports = (gd?.groupEdgePorts as any[]) || [];
      const edgeTypesFallback = Array.isArray(gd?.groupEdgeTypes) ? (gd.groupEdgeTypes as any[]) : [];
      const portList =
        Array.isArray(ports) && ports.length > 0
          ? ports
          : edgeTypesFallback.map((et: any) => ({
              edgeType: normalizeEdgeType(et),
              targetHandleId: `edge:${normalizeEdgeType(et)}:target`,
              sourceHandleId: `edge:${normalizeEdgeType(et)}:source`,
            }));
      if (!Array.isArray(portList) || portList.length === 0) continue;

      const kids = childrenByGroupId.get(gid) || [];
      if (kids.length === 0) continue;
      const childIds = new Set(kids.map((n) => String(n.id || '').trim()).filter(Boolean));
      for (const p of portList) {
        const edgeType = normalizedEdgeTypeOf({ data: { edgeType: p?.edgeType } } as any);
        const targetHandleId = String(p?.targetHandleId || `edge:${edgeType}:target`).trim();
        const sourceHandleId = String(p?.sourceHandleId || `edge:${edgeType}:source`).trim();

        const candidates: Edge[] = [];
        let parentAsSource = 0;
        let parentAsTarget = 0;
        for (const e of edgeListWithStackProxy) {
          const eidRaw = String((e as any)?.id || '').trim();
          if (eidRaw.startsWith('levels:agg:')) continue;
          if (Boolean((e as any)?.data?.levelsAgg)) continue;
          if (Boolean((e as any)?.data?.levelsStackProxy)) continue;
          const s = String((e as any)?.source || '').trim();
          const t = String((e as any)?.target || '').trim();
          if (!s || !t) continue;
          const isPC =
            (s === parentId && childIds.has(t)) ||
            (t === parentId && childIds.has(s));
          if (!isPC) continue;
          const et = normalizedEdgeTypeOf(e);
          if (et !== edgeType) continue;
          candidates.push(e);
          if (s === parentId) parentAsSource += 1;
          else if (t === parentId) parentAsTarget += 1;
        }
        if (candidates.length === 0) continue;

        const aggKey = `${gid}::edge:${edgeType}`;
        for (const ce of candidates) {
          const eid = String((ce as any)?.id || '').trim();
          if (eid) perChildEdgeIdsToAggKey.set(eid, aggKey);
        }

        const parentToGroup = parentAsSource >= parentAsTarget;
        const id = `levels:agg:${gid}:edge:${edgeType}`;
        const baseStyle = getEdgeStyle(edgeType, undefined);
        aggEdges.push({
          id,
          source: parentToGroup ? parentId : gid,
          target: parentToGroup ? gid : parentId,
          ...(parentToGroup
            ? { sourceHandle: 'source-left', targetHandle: targetHandleId }
            : { sourceHandle: sourceHandleId, targetHandle: 'target-left' }),
          type: 'hzn',
          style: baseStyle,
          data: {
            edgeType,
            edgeCaption: getEdgeCaptionShort(edgeType),
            baseStyle,
            levelsAgg: true,
            levelsAggKey: aggKey,
            levelsAggEdgeType: edgeType,
            levelsAggGroupId: gid,
            levelsAggDirection: parentToGroup ? 'parent_to_group' : 'group_to_parent',
          } as any,
        } as any);
      }
    }

    // === Levels group-proxy edges ===
    // Default UX: If a grouped child has edges to nodes OUTSIDE its group, those edges terminate on the GROUP handlers
    // (aggregated on the right edge of the group), not on the child nodes.
    //
    // Hover UX: On hover over a specific child (including collapsed subcategory controllers),
    // we reveal the original edge(s) incident to that child and temporarily suppress the corresponding aggregated edge.
    // This keeps the default view clean, but allows precise inspection on demand.
    const groupProxyBuckets = new Map<
      string,
      | { kind: 'node'; groupId: string; outsideId: string; edgeType: string; groupAsSource: number; outsideAsSource: number }
      | { kind: 'group'; groupA: string; groupB: string; edgeType: string; aToB: number; bToA: number }
    >();

    // Stack group proxy buckets (for right-side expanded stacks)
    const stackGroupProxyBuckets = new Map<
      string,
      { groupId: string; outsideId: string; edgeType: string; groupAsSource: number; outsideAsSource: number }
    >();

    for (const e of edgeListWithStackProxy) {
      const eidRaw = String((e as any)?.id || '').trim();
      const ed: any = (e as any)?.data || {};
      if (!eidRaw) continue;
      if (eidRaw.startsWith('levels:agg:')) continue;
      if (eidRaw.startsWith('levels:gproxy:')) continue;
      if (eidRaw.startsWith('levels:stackgproxy:')) continue;
      if (Boolean(ed.levelsAgg)) continue;
      if (Boolean(ed.levelsGroupProxy)) continue;
      if (Boolean(ed.levelsStackGroupProxy)) continue;

      const s = String((e as any)?.source || '').trim();
      const t = String((e as any)?.target || '').trim();
      if (!s || !t) continue;
      const sg = groupIdByChildId.get(s);
      const tg = groupIdByChildId.get(t);

      // Check for stack group membership
      const sStackGroup = stackGroupByMemberId.get(s);
      const tStackGroup = stackGroupByMemberId.get(t);
      const sInStackGroup = !!sStackGroup && stackGroupPortsByGroupId.has(sStackGroup);
      const tInStackGroup = !!tStackGroup && stackGroupPortsByGroupId.has(tStackGroup);

      // Handle stack group proxy edges (edges to/from members of expanded stack groups)
      if (sInStackGroup || tInStackGroup) {
        const edgeType = normalizedEdgeTypeOf(e);
        if (!edgeType) continue;

        // If both endpoints are in the same stack group, skip proxying
        if (sInStackGroup && tInStackGroup && sStackGroup === tStackGroup) continue;

        // If both endpoints are in different stack groups, proxy group-to-group
        if (sInStackGroup && tInStackGroup && sStackGroup !== tStackGroup) {
          // Skip for now - complex case, handle as separate edges
          continue;
        }

        // Exactly one endpoint is in a stack group -> proxy to stack group
        const stackGroupId = sInStackGroup ? sStackGroup! : tStackGroup!;
        const outsideId = sInStackGroup ? t : s;
        const childId = sInStackGroup ? s : t;
        const groupIsSource = s === childId;

        const k = `stackgroup:${stackGroupId}::node:${outsideId}::edge:${edgeType}`;
        const bucket = stackGroupProxyBuckets.get(k);
        if (!bucket) {
          stackGroupProxyBuckets.set(k, {
            groupId: stackGroupId,
            outsideId,
            edgeType,
            groupAsSource: groupIsSource ? 1 : 0,
            outsideAsSource: groupIsSource ? 0 : 1,
          });
        } else {
          if (groupIsSource) bucket.groupAsSource += 1;
          else bucket.outsideAsSource += 1;
        }
        if (eidRaw) {
          perChildEdgeIdsToAggKey.set(eidRaw, k);
          // NOTE: Do NOT add to noHoverRevealEdgeIds - we want to reveal real edges on hover
        }
        continue;
      }

      if (!sg && !tg) continue;
      const edgeType = normalizedEdgeTypeOf(e);
      if (!edgeType) continue;

      // Only proxy for Levels left groups.
      const sgIsLeftGroup = !!sg && sg.startsWith('levels:left:') && portHandlesByGroupId.has(sg);
      const tgIsLeftGroup = !!tg && tg.startsWith('levels:left:') && portHandlesByGroupId.has(tg);

      // Child-to-child across two different left groups -> proxy group-to-group.
      if (sgIsLeftGroup && tgIsLeftGroup && sg !== tg) {
        const a = sg < tg ? sg : tg;
        const b = sg < tg ? tg : sg;
        const k = `gpair:${a}<->${b}::edge:${edgeType}`;
        const bucket = groupProxyBuckets.get(k);
        const fromA = (groupIdByChildId.get(s) === a) && (groupIdByChildId.get(t) === b);
        const fromB = (groupIdByChildId.get(s) === b) && (groupIdByChildId.get(t) === a);
        if (!bucket || bucket.kind !== 'group') {
          groupProxyBuckets.set(k, { kind: 'group', groupA: a, groupB: b, edgeType, aToB: fromA ? 1 : 0, bToA: fromB ? 1 : 0 });
        } else {
          if (fromA) bucket.aToB += 1;
          if (fromB) bucket.bToA += 1;
        }
        if (eidRaw) {
          perChildEdgeIdsToAggKey.set(eidRaw, k);
          // NOTE: do NOT mark as no-hover-reveal; we want to reveal on hover and suppress the aggregated edge.
        }
        continue;
      }

      // Same left group child-to-child edge: keep it as a real intra-group edge.
      // Do not proxy to group handlers, otherwise decision groups produce wrapped/chaotic internals.
      if (sgIsLeftGroup && tgIsLeftGroup && sg === tg) {
        continue;
      }

      // Exactly one endpoint is a grouped child in a left group -> proxy group-to-node.
      const groupId = sgIsLeftGroup ? sg! : (tgIsLeftGroup ? tg! : '');
      if (!groupId) continue;
      const outsideId = sgIsLeftGroup ? t : s;

      // Edges between group child and its own group parent are handled by levels:agg edges.
      // But for STACK PROXY edges, we still need to register them in perChildEdgeIdsToAggKey so they are hidden/revealed.
      const parentId = groupParentByGroupId.get(groupId);
      const isStackProxyEdge = Boolean(ed.levelsStackProxy);
      if (parentId && outsideId === parentId) {
        if (isStackProxyEdge && eidRaw) {
          // Stack proxy edge to parent: add to perChildEdgeIdsToAggKey so it gets hidden with the appropriate aggKey.
          const edgeTypeForAgg = edgeType;
          const aggKeyForParent = `${groupId}::edge:${edgeTypeForAgg}`;
          perChildEdgeIdsToAggKey.set(eidRaw, aggKeyForParent);
        }
        continue;
      }

      const k = `${groupId}::node:${outsideId}::edge:${edgeType}`;
      const bucket = groupProxyBuckets.get(k);
      // If the original edge source is the grouped CHILD, count as groupAsSource; else outsideAsSource.
      const childId = sgIsLeftGroup ? s : t;
      const groupIsSource = String((e as any)?.source || '').trim() === childId;
      if (!bucket || bucket.kind !== 'node') {
        groupProxyBuckets.set(k, {
          kind: 'node',
          groupId,
          outsideId,
          edgeType,
          groupAsSource: groupIsSource ? 1 : 0,
          outsideAsSource: groupIsSource ? 0 : 1,
        });
      } else {
        if (groupIsSource) bucket.groupAsSource += 1;
        else bucket.outsideAsSource += 1;
      }
      if (eidRaw) {
        perChildEdgeIdsToAggKey.set(eidRaw, k);
        // NOTE: do NOT mark as no-hover-reveal; we want to reveal on hover and suppress the aggregated edge.
      }
    }

    const resolveGroupPort = (gid: string, edgeType: string) => {
      const portMap = portHandlesByGroupId.get(gid);
      if (!portMap || portMap.size === 0) return null;
      if (portMap.has(edgeType)) return portMap.get(edgeType) || null;
      if (portMap.has('OTHER')) return portMap.get('OTHER') || null;
      const first = portMap.values().next();
      return first && !first.done ? first.value : null;
    };

    // Emit group-proxy edges as levelsAgg edges (but they won't be suppressed on hover; originals never reveal).
    for (const b of groupProxyBuckets.values()) {
      if (b.kind === 'node') {
        const groupId = b.groupId;
        const outsideId = b.outsideId;
        const edgeType = b.edgeType;
        const port = resolveGroupPort(groupId, edgeType) || { sourceHandleId: `edge:${edgeType}:source`, targetHandleId: `edge:${edgeType}:target` };
        const baseStyle = getEdgeStyle(edgeType, undefined);
        const stroke = getEdgeStroke(baseStyle);
        const id = `levels:gproxy:${groupId}:node:${outsideId}:edge:${edgeType}`;
        const aggKey = `${groupId}::node:${outsideId}::edge:${edgeType}`;
        const groupToOutside = b.groupAsSource >= b.outsideAsSource;
        const gNode = groupMap.get(groupId);
        const oNode = nodeMap.get(outsideId);
        const gp = gNode ? getAbsolutePos(gNode) : { x: 0, y: 0 };
        const op = oNode ? getAbsolutePos(oNode) : { x: 0, y: 0 };
        const gx = gp.x;
        const ox = op.x;
        const outsideIsRightOfGroup = ox > gx + eps;
        const outsideIsLeftOfGroup = ox < gx - eps;
        const outsideTargetHandle = outsideIsRightOfGroup ? 'target-left' : (outsideIsLeftOfGroup ? 'target-right' : 'target-left');
        const outsideSourceHandle = outsideIsRightOfGroup ? 'source-left' : (outsideIsLeftOfGroup ? 'source-right' : 'source-left');
        aggEdges.push({
          id,
          source: groupToOutside ? groupId : outsideId,
          target: groupToOutside ? outsideId : groupId,
          ...(groupToOutside
            ? { sourceHandle: port.sourceHandleId, targetHandle: outsideTargetHandle }
            : { sourceHandle: outsideSourceHandle, targetHandle: port.targetHandleId }),
          type: 'hzn',
          interactionWidth: 18,
          markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
          style: baseStyle,
          data: {
            edgeType,
            edgeCaption: getEdgeCaptionShort(edgeType),
            baseStyle,
            levelsAgg: true,
            levelsAggKey: aggKey,
            levelsAggEdgeType: edgeType,
            levelsAggGroupId: groupId,
            levelsGroupProxy: true,
            levelsGroupProxyOutsideId: outsideId,
          } as any,
        } as any);
      } else {
        const a = b.groupA;
        const c = b.groupB;
        const edgeType = b.edgeType;
        const portA = resolveGroupPort(a, edgeType) || { sourceHandleId: `edge:${edgeType}:source`, targetHandleId: `edge:${edgeType}:target` };
        const portB = resolveGroupPort(c, edgeType) || { sourceHandleId: `edge:${edgeType}:source`, targetHandleId: `edge:${edgeType}:target` };
        const baseStyle = getEdgeStyle(edgeType, undefined);
        const stroke = getEdgeStroke(baseStyle);
        const id = `levels:gproxy:${a}<->${c}:edge:${edgeType}`;
        const aggKey = `gpair:${a}<->${c}::edge:${edgeType}`;
        const aToB = b.aToB >= b.bToA;
        aggEdges.push({
          id,
          source: aToB ? a : c,
          target: aToB ? c : a,
          ...(aToB
            ? { sourceHandle: portA.sourceHandleId, targetHandle: portB.targetHandleId }
            : { sourceHandle: portB.sourceHandleId, targetHandle: portA.targetHandleId }),
          type: 'hzn',
          interactionWidth: 18,
          markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
          style: baseStyle,
          data: {
            edgeType,
            edgeCaption: getEdgeCaptionShort(edgeType),
            baseStyle,
            levelsAgg: true,
            levelsAggKey: aggKey,
            levelsAggEdgeType: edgeType,
            levelsAggGroupId: aToB ? a : c,
            levelsGroupProxy: true,
            levelsGroupProxyOtherGroupId: aToB ? c : a,
          } as any,
        } as any);
      }
    }

    // Emit stack group proxy edges (expanded stacks)
    for (const b of stackGroupProxyBuckets.values()) {
      const { groupId, outsideId, edgeType, groupAsSource, outsideAsSource } = b;
      const baseStyle = getEdgeStyle(edgeType, undefined);
      const stroke = getEdgeStroke(baseStyle);
      const id = `levels:stackgproxy:${groupId}:node:${outsideId}:edge:${edgeType}`;
      const aggKey = `stackgroup:${groupId}::node:${outsideId}::edge:${edgeType}`;
      const groupToOutside = groupAsSource >= outsideAsSource;
      const gNode = groupMap.get(groupId);
      const oNode = nodeMap.get(outsideId);
      const gp = gNode ? getAbsolutePos(gNode) : { x: 0, y: 0 };
      const op = oNode ? getAbsolutePos(oNode) : { x: 0, y: 0 };
      const gx = gp.x;
      const ox = op.x;
      // Choose side by real geometry to avoid wraparounds when a stack group appears left of an outside node.
      const outsideIsLeftOfGroup = ox < gx - eps;
      const outsideIsRightOfGroup = ox > gx + eps;
      const outsideTargetHandle = outsideIsLeftOfGroup ? 'target-right' : (outsideIsRightOfGroup ? 'target-left' : 'target-right');
      const outsideSourceHandle = outsideIsLeftOfGroup ? 'source-right' : (outsideIsRightOfGroup ? 'source-left' : 'source-right');
      const groupTargetHandle = outsideIsLeftOfGroup ? 'target-left' : (outsideIsRightOfGroup ? 'target-right' : 'target-left');
      const groupSourceHandle = outsideIsLeftOfGroup ? 'source-left' : (outsideIsRightOfGroup ? 'source-right' : 'source-right');
      aggEdges.push({
        id,
        source: groupToOutside ? groupId : outsideId,
        target: groupToOutside ? outsideId : groupId,
        ...(groupToOutside
          ? { sourceHandle: groupSourceHandle, targetHandle: outsideTargetHandle }
          : { sourceHandle: outsideSourceHandle, targetHandle: groupTargetHandle }),
        type: 'hzn',
        interactionWidth: 18,
        markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
        style: baseStyle,
        data: {
          edgeType,
          edgeCaption: getEdgeCaptionShort(edgeType),
          baseStyle,
          levelsAgg: true,
          levelsAggKey: aggKey,
          levelsAggEdgeType: edgeType,
          levelsAggGroupId: groupId,
          levelsStackGroupProxy: true,
          levelsStackGroupProxyOutsideId: outsideId,
        } as any,
      } as any);
    }

    const mapped = edgeList.map((e) => {
      const sourceNode = nodeMap.get(e.source);
      const targetNode = nodeMap.get(e.target);
      if (!sourceNode || !targetNode) return e;

      const sp = getAbsolutePos(sourceNode);
      const tp = getAbsolutePos(targetNode);
      const sx = sp.x;
      const sy = sp.y;
      const tx = tp.x;
      const ty = tp.y;

      const sGrouped = isInGroup(sourceNode);
      const tGrouped = isInGroup(targetNode);
      const isStackProxy = Boolean(((e as any)?.data as any)?.levelsStackProxy);
      const isPeerEdge = PEER_EDGE_TYPES.has(normalizedEdgeTypeOf(e));

      if (sGrouped && tGrouped) {
        // IMPORTANT: even inside a group, do not show direct edges by default.
        // We reveal them only on hover over the specific node (Levels UX).
        const sId = String((e as any)?.source || '').trim();
        const tId = String((e as any)?.target || '').trim();
        const sg = sId ? groupIdByChildId.get(sId) : undefined;
        const tg = tId ? groupIdByChildId.get(tId) : undefined;
        const sameLeftGroup = Boolean(sg && tg && sg === tg && sg.startsWith('levels:left:'));
        const decisionCorridorPair =
          isDecisionCorridorNode(sourceNode) &&
          isDecisionCorridorNode(targetNode);

        const eid = String((e as any)?.id || '').trim();
        const aggKey = eid ? perChildEdgeIdsToAggKey.get(eid) : undefined;
        const d0: any = (e as any)?.data || {};
        const noHover = eid && noHoverRevealEdgeIds.has(eid);

        if (decisionCorridorPair) {
          const sLane = corridorLaneRankForNode(sourceNode);
          const tLane = corridorLaneRankForNode(targetNode);
          if (sLane >= 0 && tLane >= 0 && sLane === tLane) {
            // Same-lane corridor edges are secondary; keep them hover-reveal to reduce clutter.
            const d = { ...stripAggFlags(d0), levelsHiddenByAggregation: true, levelsDecisionIntraLane: true };
            return {
              ...e,
              data: d,
              ...corridorHandlesForPair(sourceNode, targetNode, sx, sy, tx, ty),
            };
          }
          return {
            ...e,
            data: stripAggFlags(d0),
            ...corridorHandlesForPair(sourceNode, targetNode, sx, sy, tx, ty),
          };
        }

        // If this is an intra-group edge (same group), hide by aggregation even if there's no aggKey.
        // (No agg edge exists; we just keep the canvas clean and reveal only on hover.)
        if (sameLeftGroup && !aggKey) {
          const d = { ...d0, levelsHiddenByAggregation: true, ...(noHover ? { levelsNoHoverReveal: true } : {}) };
          return {
            ...e,
            data: d,
            ...horizontalHandlesByX(sx, tx, tx >= sx ? 'toRight' : 'toLeft'),
          };
        }

        const d = aggKey
          ? { ...d0, levelsHiddenByAggregation: true, levelsAggKey: aggKey, ...(noHover ? { levelsNoHoverReveal: true } : {}) }
          : stripAggFlags(d0);
        return {
          ...e,
          data: d,
          ...horizontalHandlesByX(sx, tx, tx >= sx ? 'toRight' : 'toLeft'),
        };
      }

      // Defaults
      let sourceHandle = e.sourceHandle || 'source-right';
      let targetHandle = e.targetHandle || 'target-left';

      const sInStackGroup = Boolean((sourceNode.data as any)?.insideStackGroup) ||
        String(((sourceNode as any)?.parentId || (sourceNode as any)?.parentNode || '')).startsWith('stack:group:');
      const tInStackGroup = Boolean((targetNode.data as any)?.insideStackGroup) ||
        String(((targetNode as any)?.parentId || (targetNode as any)?.parentNode || '')).startsWith('stack:group:');

      // Hard rule for Two-Pane:
      // Left groups: edges attach left on right-side nodes, right on group-children.
      // Right stack groups: edges attach right on outside nodes, left on stack-children.
      if (!sGrouped && tGrouped) {
        const eid = String((e as any)?.id || '').trim();
        const aggKey = eid ? perChildEdgeIdsToAggKey.get(eid) : undefined;
        const d0: any = (e as any)?.data || {};
        const noHover = eid && noHoverRevealEdgeIds.has(eid);
        const d = aggKey
          ? { ...d0, levelsHiddenByAggregation: true, levelsAggKey: aggKey, ...(noHover ? { levelsNoHoverReveal: true } : {}) }
          : d0;
        return {
          ...e,
          data: d,
          ...horizontalHandlesByX(sx, tx, tInStackGroup ? 'toRight' : 'toLeft'),
        };
      }
      if (sGrouped && !tGrouped) {
        const eid = String((e as any)?.id || '').trim();
        const aggKey = eid ? perChildEdgeIdsToAggKey.get(eid) : undefined;
        const d0: any = (e as any)?.data || {};
        const noHover = eid && noHoverRevealEdgeIds.has(eid);
        const d = aggKey
          ? { ...d0, levelsHiddenByAggregation: true, levelsAggKey: aggKey, ...(noHover ? { levelsNoHoverReveal: true } : {}) }
          : d0;
        return {
          ...e,
          data: d,
          ...horizontalHandlesByX(sx, tx, sInStackGroup ? 'toLeft' : 'toRight'),
        };
      }

      // Grouped children: choose side by real geometry (avoids right-edge wraparounds).
      if (sGrouped) {
        if (tx > sx + nearColumnBand) sourceHandle = 'source-right';
        else if (tx < sx - nearColumnBand) sourceHandle = 'source-left';
        else sourceHandle = sInStackGroup ? 'source-left' : 'source-right';
      } else {
        if (tx > sx + eps) sourceHandle = 'source-right';
        else if (tx < sx - eps) sourceHandle = 'source-left';
        else sourceHandle = (isStackProxy || isPeerEdge) ? 'source-right' : ((ty > sy) ? 'source-bottom' : 'source-top');
      }

      if (tGrouped) {
        if (sx < tx - nearColumnBand) targetHandle = 'target-left';
        else if (sx > tx + nearColumnBand) targetHandle = 'target-right';
        else targetHandle = tInStackGroup ? 'target-left' : 'target-right';
      } else {
        if (sx < tx - eps) targetHandle = 'target-left';
        else if (sx > tx + eps) targetHandle = 'target-right';
        else targetHandle = (isStackProxy || isPeerEdge) ? 'target-left' : ((sy > ty) ? 'target-bottom' : 'target-top');
      }

      return { ...e, sourceHandle, targetHandle };
    });

    // Append aggregated edges (stable) after mapping.
    // Deduplicate by id just in case upstream edges already contain a matching id.
    const seen = new Set<string>(mapped.map((e) => String((e as any)?.id || '').trim()).filter(Boolean));
    const appended = aggEdges.filter((e) => {
      const id = String((e as any)?.id || '').trim();
      if (!id) return false;
      if (seen.has(id)) return false;
      seen.add(id);
      return true;
    });
    // Ensure stack-proxy edges are included too (they are already in `mapped` if we used the extended list).
    // Re-run mapping using the extended list so proxy edges get correct handles.
    const mappedWithProxy = edgeListWithStackProxy.map((e) => {
      const sourceNode = nodeMap.get(e.source);
      const targetNode = nodeMap.get(e.target);
      if (!sourceNode || !targetNode) return e;

      const sp = getAbsolutePos(sourceNode);
      const tp = getAbsolutePos(targetNode);
      const sx = sp.x;
      const sy = sp.y;
      const tx = tp.x;
      const ty = tp.y;

      const sGrouped = isInGroup(sourceNode);
      const tGrouped = isInGroup(targetNode);
      const isStackProxy = Boolean(((e as any)?.data as any)?.levelsStackProxy);
      const isPeerEdge = PEER_EDGE_TYPES.has(normalizedEdgeTypeOf(e));

      // Hide intra-left-group edges by default (including stackproxy edges).
      // They will be revealed only when the specific node is hovered (see displayEdges).
      if (sGrouped && tGrouped) {
        const sId = String((e as any)?.source || '').trim();
        const tId = String((e as any)?.target || '').trim();
        const sg = sId ? groupIdByChildId.get(sId) : undefined;
        const tg = tId ? groupIdByChildId.get(tId) : undefined;
        const sameLeftGroup = Boolean(sg && tg && sg === tg && sg.startsWith('levels:left:'));
        const decisionCorridorPair =
          isDecisionCorridorNode(sourceNode) &&
          isDecisionCorridorNode(targetNode);
        if (decisionCorridorPair) {
          const d0: any = (e as any)?.data || {};
          const sLane = corridorLaneRankForNode(sourceNode);
          const tLane = corridorLaneRankForNode(targetNode);
          if (sLane >= 0 && tLane >= 0 && sLane === tLane) {
            const d = { ...stripAggFlags(d0), levelsHiddenByAggregation: true, levelsDecisionIntraLane: true };
            return {
              ...e,
              data: d,
              ...corridorHandlesForPair(sourceNode, targetNode, sx, sy, tx, ty),
            };
          }
          return {
            ...e,
            data: stripAggFlags(d0),
            ...corridorHandlesForPair(sourceNode, targetNode, sx, sy, tx, ty),
          };
        }
        if (sameLeftGroup) {
          const eid = String((e as any)?.id || '').trim();
          const aggKey = eid ? perChildEdgeIdsToAggKey.get(eid) : undefined;
          const d0: any = (e as any)?.data || {};
          const noHover = eid && noHoverRevealEdgeIds.has(eid);
          const d = aggKey
            ? { ...d0, levelsHiddenByAggregation: true, levelsAggKey: aggKey, ...(noHover ? { levelsNoHoverReveal: true } : {}) }
            : { ...stripAggFlags(d0), levelsHiddenByAggregation: true, ...(noHover ? { levelsNoHoverReveal: true } : {}) };
          return {
            ...e,
            data: d,
            ...horizontalHandlesByX(sx, tx, tx >= sx ? 'toRight' : 'toLeft'),
          };
        }
      }

      // Defaults
      let sourceHandle = e.sourceHandle || 'source-right';
      let targetHandle = e.targetHandle || 'target-left';

      const sInStackGroup = Boolean((sourceNode.data as any)?.insideStackGroup) ||
        String(((sourceNode as any)?.parentId || (sourceNode as any)?.parentNode || '')).startsWith('stack:group:');
      const tInStackGroup = Boolean((targetNode.data as any)?.insideStackGroup) ||
        String(((targetNode as any)?.parentId || (targetNode as any)?.parentNode || '')).startsWith('stack:group:');

      if (!sGrouped && tGrouped) {
        const eid = String((e as any)?.id || '').trim();
        const aggKey = eid ? perChildEdgeIdsToAggKey.get(eid) : undefined;
        const d0: any = (e as any)?.data || {};
        const noHover = eid && noHoverRevealEdgeIds.has(eid);
        const d = aggKey
          ? { ...d0, levelsHiddenByAggregation: true, levelsAggKey: aggKey, ...(noHover ? { levelsNoHoverReveal: true } : {}) }
          : stripAggFlags(d0);
        return {
          ...e,
          data: d,
          ...horizontalHandlesByX(sx, tx, tInStackGroup ? 'toRight' : 'toLeft'),
        };
      }
      if (sGrouped && !tGrouped) {
        const eid = String((e as any)?.id || '').trim();
        const aggKey = eid ? perChildEdgeIdsToAggKey.get(eid) : undefined;
        const d0: any = (e as any)?.data || {};
        const noHover = eid && noHoverRevealEdgeIds.has(eid);
        const d = aggKey
          ? { ...d0, levelsHiddenByAggregation: true, levelsAggKey: aggKey, ...(noHover ? { levelsNoHoverReveal: true } : {}) }
          : stripAggFlags(d0);
        return {
          ...e,
          data: d,
          ...horizontalHandlesByX(sx, tx, sInStackGroup ? 'toLeft' : 'toRight'),
        };
      }

      // Edge no longer points into a group-child: clear any stale aggregation-hiding flags
      // (this is the key fix for "node left group => show its original connection again").
      const d0: any = (e as any)?.data || {};
      const hadAggHide = Boolean(d0.levelsHiddenByAggregation) || Boolean(d0.levelsAggKey);
      const eOut: any = hadAggHide ? { ...e, data: stripAggFlags(d0) } : e;

      if (sGrouped) {
        if (tx > sx + nearColumnBand) sourceHandle = 'source-right';
        else if (tx < sx - nearColumnBand) sourceHandle = 'source-left';
        else sourceHandle = sInStackGroup ? 'source-left' : 'source-right';
      } else {
        if (tx > sx + eps) sourceHandle = 'source-right';
        else if (tx < sx - eps) sourceHandle = 'source-left';
        else sourceHandle = (isStackProxy || isPeerEdge) ? 'source-right' : ((ty > sy) ? 'source-bottom' : 'source-top');
      }

      if (tGrouped) {
        if (sx < tx - nearColumnBand) targetHandle = 'target-left';
        else if (sx > tx + nearColumnBand) targetHandle = 'target-right';
        else targetHandle = tInStackGroup ? 'target-left' : 'target-right';
      } else {
        if (sx < tx - eps) targetHandle = 'target-left';
        else if (sx > tx + eps) targetHandle = 'target-right';
        else targetHandle = (isStackProxy || isPeerEdge) ? 'target-left' : ((sy > ty) ? 'target-bottom' : 'target-top');
      }

      return { ...(eOut as any), sourceHandle, targetHandle };
    });

    // Drop stale agg edges that no longer have any supporting candidates after regrouping/extraction.
    const validAggIds = new Set<string>(aggEdges.map((e: any) => String(e?.id || '').trim()).filter(Boolean));
    const filteredMapped = mappedWithProxy.filter((e: any) => {
      const d: any = e?.data || {};
      if (!d.levelsAgg) return true;
      const id = String(e?.id || '').trim();
      if ((id.startsWith('levels:agg:') || id.startsWith('levels:gproxy:')) && !validAggIds.has(id)) return false;
      return true;
    });

    const seen2 = new Set<string>(filteredMapped.map((e: any) => String(e?.id || '').trim()).filter(Boolean));
    const appended2 = aggEdges.filter((e) => {
      const id = String((e as any)?.id || '').trim();
      if (!id) return false;
      if (seen2.has(id)) return false;
      seen2.add(id);
      return true;
    });
    return [...filteredMapped, ...appended2];
  }, []);

  /**
   * Recalculate edge handles for Freeform layout.
   * Based on semantic group position (top/bottom/left/right) relative to central node.
   */
  const recalculateFreeformHandles = useCallback((
    edgeList: Edge[],
    nodeList: Node<HorizonNodeData>[]
  ): Edge[] => {
    // Build lookup maps
    const nodeMap = new Map<string, Node<HorizonNodeData>>();
    
    for (const n of nodeList) {
      const d = n.data as any;
      if (d?.isGroup) continue;
      nodeMap.set(n.id, n);
      if (d?.fragmentId) {
        nodeMap.set(d.fragmentId, n);
      }
    }
    
    return edgeList.map((e) => {
      const sourceNode = nodeMap.get(e.source);
      const targetNode = nodeMap.get(e.target);
      
      if (!sourceNode || !targetNode) {
        return e;
      }
      
      const sourceData = sourceNode.data as any;
      const targetData = targetNode.data as any;
      const sourceIsCentral = sourceData?.depth === 0;
      const targetIsCentral = targetData?.depth === 0;
      
      // Default handles
      let handles = { sourceHandle: e.sourceHandle || 'source-bottom', targetHandle: e.targetHandle || 'target-top' };
      
      // Determine group position based on semantic group
      const getGroupPosition = (data: any): 'top' | 'bottom' | 'left' | 'right' | null => {
        if (!data?.edgeTypeFromParent) return null;
        const isOutgoing = data?.directionFromParent !== 'incoming';
        const sg = getSemanticGroup(data.edgeTypeFromParent, isOutgoing);
        return SEMANTIC_GROUP_POSITION[sg];
      };
      
      if (sourceIsCentral && !targetIsCentral) {
        // central → group node
        const position = getGroupPosition(targetData);
        if (position) {
          switch (position) {
            case 'top':
              handles = { sourceHandle: 'source-top', targetHandle: 'target-bottom' };
              break;
            case 'bottom':
              handles = { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
              break;
            case 'left':
              handles = { sourceHandle: 'source-left', targetHandle: 'target-right' };
              break;
            case 'right':
              handles = { sourceHandle: 'source-right', targetHandle: 'target-left' };
              break;
          }
        }
      } else if (targetIsCentral && !sourceIsCentral) {
        // group node → central
        const position = getGroupPosition(sourceData);
        if (position) {
          switch (position) {
            case 'top':
              handles = { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
              break;
            case 'bottom':
              handles = { sourceHandle: 'source-top', targetHandle: 'target-bottom' };
              break;
            case 'left':
              handles = { sourceHandle: 'source-right', targetHandle: 'target-left' };
              break;
            case 'right':
              handles = { sourceHandle: 'source-left', targetHandle: 'target-right' };
              break;
          }
        }
      }
      
      return { ...e, ...handles };
    });
  }, []);

  /**
   * Recalculate edge handles for Supply Chain (horizontal hierarchy).
   * Uses only left/right handles: source on the right → source-right, target-left; source on the left → source-left, target-right.
   */
  const recalculateSupplyChainHandles = useCallback((
    edgeList: Edge[],
    nodeList: Node<HorizonNodeData>[]
  ): Edge[] => {
    const nodeMap = new Map<string, Node<HorizonNodeData>>();
    for (const n of nodeList) {
      const d = n.data as any;
      if (d?.isGroup) continue;
      nodeMap.set(n.id, n);
      if (d?.fragmentId) nodeMap.set(String(d.fragmentId), n);
    }
    const nearColumnBand = 40;
    return edgeList.map((e) => {
      const sourceNode = nodeMap.get(e.source) || nodeMap.get(String((e as any).source));
      const targetNode = nodeMap.get(e.target) || nodeMap.get(String((e as any).target));
      if (!sourceNode || !targetNode) {
        return { ...e, sourceHandle: e.sourceHandle || 'source-right', targetHandle: e.targetHandle || 'target-left' };
      }
      const sx = Number((sourceNode as any)?.position?.x) ?? 0;
      const tx = Number((targetNode as any)?.position?.x) ?? 0;
      let sourceHandle: string;
      let targetHandle: string;
      if (tx > sx + nearColumnBand) {
        sourceHandle = 'source-right';
        targetHandle = 'target-left';
      } else if (tx < sx - nearColumnBand) {
        sourceHandle = 'source-left';
        targetHandle = 'target-right';
      } else {
        sourceHandle = 'source-right';
        targetHandle = 'target-left';
      }
      return { ...e, sourceHandle, targetHandle };
    });
  }, []);

  // ELK layout options
  const ELK_NODE_WIDTH = 180;
  // Layout height includes the "+" button protruding half outside the visual card:
  // visual card is 148px, plus button is 26px => add radius 13px => 161px.
  const ELK_NODE_HEIGHT = 161;
  const ELK_NODE_SPACING = 60;
  const ELK_LAYER_SPACING = 150; // Увеличено для incoming/outgoing
  const ELK_MAX_CHILD_ROWS = 3; // Максимум 3 строки детей

  const arrangeWithGraphView = useCallback(async (
    view: GraphViewId,
    nodeList: Node<HorizonNodeData>[],
    edgeList: Edge[],
  ) => {
    return arrangeGraphView(view, nodeList, edgeList, {
      nodeWidth: ELK_NODE_WIDTH,
      nodeHeight: ELK_NODE_HEIGHT,
      nodeSpacing: ELK_NODE_SPACING,
      layerSpacing: ELK_LAYER_SPACING,
      maxChildRows: ELK_MAX_CHILD_ROWS,
      expandedNodeIds: expandedNodesRef.current,
      collapsedSubcategories,
      expandedRoleGroups,
      rightDockStateByNodeId,
      expandedStackKeysOverride: expandedStackKeysRef.current,
      hierarchicalLeftPaneMode: levelsExpandMode === 'decision-chain' ? 'decision-chain' : 'default',
      graphViewState,
      onToggleSubcategoryCollapse: toggleSubcategoryCollapse,
      onToggleRoleOverflow: toggleRoleOverflow,
      onToggleRightDock: toggleRightDock,
      layoutFullGraph,
      layoutLinearFullGraph,
      layoutLevelsTwoPane,
      rebuildGroupContainers,
      recalculateFreeformHandles,
      recalculateLinearHandles,
      recalculateLevelsTwoPaneHandles,
    });
  }, [
    graphViewState,
    collapsedSubcategories,
    expandedRoleGroups,
    rightDockStateByNodeId,
    levelsExpandMode,
    toggleSubcategoryCollapse,
    toggleRoleOverflow,
    toggleRightDock,
    recalculateFreeformHandles,
    recalculateLinearHandles,
    recalculateLevelsTwoPaneHandles,
  ]);

  // Focus/center viewport on a specific fragment node (used after expand/collapse to keep context).
  const focusOnFragment = React.useCallback((rawId: string) => {
    const fragmentId = String(rawId || '').trim();
    const inst: any = reactFlowInstanceRef.current;
    if (!fragmentId || !inst) return;
    const node: any = inst.getNode?.(fragmentId);
    if (!node) return;
    const x0 = (node.positionAbsolute?.x ?? node.position?.x ?? 0);
    const y0 = (node.positionAbsolute?.y ?? node.position?.y ?? 0);
    const w = typeof node.width === 'number' ? node.width : ELK_NODE_WIDTH;
    const h = typeof node.height === 'number' ? node.height : ELK_NODE_HEIGHT;
    const cx = x0 + w / 2;
    const cy = y0 + h / 2;
    const z = inst.getZoom?.();
    // Prefer setCenter (keeps zoom stable). Fall back to fitView if unavailable.
    if (typeof inst.setCenter === 'function') {
      inst.setCenter(cx, cy, { zoom: typeof z === 'number' ? z : undefined, duration: 360 });
      return;
    }
    if (typeof inst.fitView === 'function') {
      inst.fitView({ nodes: [node], padding: 0.25, duration: 360 });
    }
  }, [ELK_NODE_HEIGHT, ELK_NODE_WIDTH]);

  // Загружаем существующие LensItems при монтировании
  React.useEffect(() => {
    if (!lensId) {
      setLensItems([]);
      setNodes([]);
      setEdges([]);
      setFragmentProfileActiveNodeId(null);
      return;
    }

    const loadLensItems = async () => {
      try {
        // New lens / reload: reset risk shield state.
        setFragmentProfileActiveNodeId(null);
        // New lens / reload: reset context-card state (collapsed blocks + overflow).
        setCollapsedSubcategories(new Set());
        setExpandedRoleGroups(new Set());
        collapsedSubcategoriesChangedRef.current = false;
        expandedRoleGroupsChangedRef.current = false;
        // New lens / reload: allow a single auto-arrange on initial load (whatever the current layout mode is).
        autoArrangeOnceRef.current = false;
        const itemsRaw = await api.getLensItems(lensId);
        const items = Array.isArray(itemsRaw) ? (itemsRaw as LensItem[]) : [];
        if (!Array.isArray(itemsRaw)) {
          console.warn('[Cockpit] getLensItems returned non-array, defaulting to []:', itemsRaw);
        }
        setLensItems(items);
        
        // Преобразуем LensItems в ноды и рёбра
        const { nodes: loadedNodes, edges: loadedEdges } = convertLensItemsToGraphRef.current(items);
        dbg('[Cockpit] Initial load converted:', { nodes: loadedNodes.length, edges: loadedEdges.length });
        // IMPORTANT: do not re-load lens items when scope changes (pin/unpin).
        // Pin state is injected via a separate effect that reacts to scope + nodes.
        setNodes(loadedNodes);
        setEdges(loadedEdges);
        
        // Pin state + callbacks are injected by the dedicated effect (triggered by nodes.length changes).
        
        // Fit view after initial load and trigger ONE auto-arrange using the CURRENT layout mode.
        setTimeout(() => {
          if (reactFlowInstanceRef.current && loadedNodes.length > 0) {
            reactFlowInstanceRef.current.fitView({ padding: 0.1, duration: 300 });
          }
          // Trigger auto-arrange once when nodes actually land in state.
          const tryAutoArrange = () => {
            if (autoArrangeOnceRef.current) return;
            const current = (nodesStateRef.current || []) as any[];
            if (!current.length) {
              window.requestAnimationFrame(tryAutoArrange);
              return;
            }
            const hasFragments = current.some((n) => !((n?.data as any)?.isGroup));
            if (!hasFragments) return;
            autoArrangeOnceRef.current = true;
            // Note: invokeArrange -> handleArrange uses current `layoutMode` to choose the algorithm.
            // We keep layoutMode in a ref so we don't re-fetch LensItems on layout mode changes.
            invokeArrange();
          };
          tryAutoArrange();
        }, 100);
      } catch (err: any) {
        console.error('[Cockpit] Failed to load lens items', err);
        setError(err.message || 'Failed to load graph');
      }
    };

    expandedStackKeysRef.current.clear();
    loadLensItems();
  }, [lensId, setNodes, setEdges, dbg, invokeArrange]);

  // Преобразование LensItems в ReactFlow ноды и рёбра
  const convertLensItemsToGraph = useCallback((items: LensItem[] | null | undefined) => {
    const safeItems: LensItem[] = Array.isArray(items) ? items : [];
    if (!Array.isArray(items)) {
      console.warn('[Cockpit] convertLensItemsToGraph received non-array, defaulting to []:', items);
    }
    dbg('[Cockpit] convertLensItemsToGraph called with items:', safeItems.length);
    dbg('[Cockpit] Sample items:', safeItems.slice(0, 3).map(item => ({ 
      id: item.id, 
      kind: item.kind, 
      fragmentId: item.fragmentId,
      hasKind: 'kind' in item,
      keys: Object.keys(item)
    })));
    
    const graphNodes: Node<HorizonNodeData>[] = [];
    const graphEdges: Edge[] = [];

    // Создаём маппинг fragmentId -> nodeId для рёбер
    const fragmentToNodeId = new Map<string, string>();
    const canonicalToNodeId = new Map<string, string>();
    const skippedEdges: Array<{ s: string; t: string }> = [];

    // Обрабатываем ноды
    // Determine node items: explicit kind='node' OR has fragmentId (fallback for items without kind)
    const nodeItems = safeItems.filter(item => {
      const kind = item.kind;
      const isExplicitNode = kind === 'node';
      const hasFragmentId = !!item.fragmentId;
      const isLikelyNode = !kind && hasFragmentId; // Fallback: if no kind but has fragmentId, treat as node

      if (isSphereType(item.type) || isSphereCanonicalPath(item.canonicalPath)) {
        return false;
      }
      
      if (!isExplicitNode && !isLikelyNode && kind) {
        // Only log if kind exists but isn't 'node'
        dbg('[Cockpit] Item is not a node:', { id: item.id, kind, hasFragmentId });
      }
      
      return isExplicitNode || isLikelyNode;
    });
    dbg('[Cockpit] Filtered node items:', nodeItems.length, 'out of', safeItems.length);
    dbg(
      '[Cockpit] Node items sample:',
      nodeItems.slice(0, 25).map(item => ({
      id: item.id,
      fragmentId: item.fragmentId,
      kind: item.kind,
      hasPosition: !!item.position
      }))
    );
    
    nodeItems.forEach(item => {
        const fragmentId = item.fragmentId ? norm(item.fragmentId) : '';
        const nodeId = fragmentId || norm(item.id);
        const canonicalPath = norm(item.canonicalPath || '');
        if (!nodeId) {
          console.warn('[Cockpit] Node item missing both fragmentId and id:', item);
          return;
        }

        // Dedup by fragmentId first.
        const existingByFragment = fragmentId ? fragmentToNodeId.get(fragmentId) : undefined;
        if (existingByFragment) {
          if (canonicalPath && !canonicalToNodeId.has(canonicalPath)) canonicalToNodeId.set(canonicalPath, existingByFragment);
          return;
        }

        // Defensive dedup by canonicalPath: if backend emitted another fragment id for the same entity,
        // alias that fragment id to the already materialized node id.
        const existingByCanonical = canonicalPath ? canonicalToNodeId.get(canonicalPath) : undefined;
        if (existingByCanonical) {
          if (fragmentId) fragmentToNodeId.set(fragmentId, existingByCanonical);
          return;
        }

        // Ensure position is valid
        let position = item.position;
        if (!position || typeof position.x !== 'number' || typeof position.y !== 'number' || 
            isNaN(position.x) || isNaN(position.y) || !isFinite(position.x) || !isFinite(position.y)) {
          position = { x: Math.random() * 400 + 100, y: Math.random() * 400 + 100 };
        }

        graphNodes.push({
          id: nodeId,
          type: 'horizonFragment',
          position,
          data: {
            id: nodeId,
            lensItemId: item.id,
            fragmentId: fragmentId || undefined,
            canonicalPath: item.canonicalPath,
            facilityKind: readFacilityKindFromAny(item as any),
            label: item.label,
            type: item.type,
            state: item.state,
            showMetrics: showNodeMetrics,
            status: item.status,
            expanded: item.expanded,
            collapsed: item.collapsed,
            critical: item.critical,
          },
        });

        if (fragmentId) fragmentToNodeId.set(fragmentId, nodeId);
        if (canonicalPath) canonicalToNodeId.set(canonicalPath, nodeId);
      });

    // Обрабатываем рёбра
    // Determine edge items: explicit kind='edge' OR has sourceFragmentId and targetFragmentId (fallback)
    const edgeItems = safeItems.filter(item => {
      const kind = item.kind;
      const isExplicitEdge = kind === 'edge';
      const hasSourceAndTarget = !!(item.sourceFragmentId && item.targetFragmentId);
      const isLikelyEdge = !kind && hasSourceAndTarget; // Fallback: if no kind but has source/target, treat as edge
      return isExplicitEdge || isLikelyEdge;
    });
    
    edgeItems.forEach(item => {
        const sFrag = item.sourceFragmentId ? norm(item.sourceFragmentId) : '';
        const tFrag = item.targetFragmentId ? norm(item.targetFragmentId) : '';
        const srcType = (item as any)?.sourceType;
        const dstType = (item as any)?.targetType;
        const srcPath = (item as any)?.sourceCanonicalPath;
        const dstPath = (item as any)?.targetCanonicalPath;
        if (isSphereType(srcType) || isSphereType(dstType)) return;
        if (isSphereCanonicalPath(srcPath) || isSphereCanonicalPath(dstPath)) return;
        const sourceId =
          (sFrag ? fragmentToNodeId.get(sFrag) : null) ||
          (srcPath ? (canonicalToNodeId.get(norm(srcPath)) || null) : null);
        const targetId =
          (tFrag ? fragmentToNodeId.get(tFrag) : null) ||
          (dstPath ? (canonicalToNodeId.get(norm(dstPath)) || null) : null);

        if (sourceId && targetId) {
          const baseStyle = getEdgeStyle(item.edgeType, item.criticality, item as any);
          const stroke = getEdgeStroke(baseStyle);
          // В linear режиме используем кривые edges (ReactFlow built-in "default" bezier), в freeform - default
          const edgeRenderType = 'default';
          graphEdges.push({
            id: item.id,
            source: sourceId,
            target: targetId,
            type: edgeRenderType,
            // Labels are hidden by default (see displayEdges). Keep unset to avoid noisy/system labels in perfMode.
            label: undefined,
            // Increase hit-area for clicking thin edges (compatible with ReactFlow v11 types)
            interactionWidth: 18,
            markerEnd: {
              type: MarkerType.ArrowClosed,
              color: stroke,
            },
            style: baseStyle,
            data: {
              lensItemId: item.id,
              edgeType: item.edgeType,
              edgeCaption: getEdgeCaptionShort(item.edgeType || item.subkind),
              criticality: item.criticality,
              amplification: item.amplification,
              latencyHours: item.latencyHours,
              direction: item.direction,
              // Endpoint context (used for UI heuristics, e.g. dimming DOMAIN edges by default).
              sourceType: srcType,
              targetType: dstType,
              sourceCanonicalPath: srcPath,
              targetCanonicalPath: dstPath,
              baseStyle,
            },
          });
        } else if (sFrag || tFrag) {
          skippedEdges.push({ s: sFrag, t: tFrag });
        }
      });

      const filteredEdges = suppressDominatedEdges(graphEdges, graphNodes);

      if (skippedEdges.length > 0) {
        dbg(
          `[Cockpit] ${skippedEdges.length} edges skipped (endpoints not loaded). Use + to load related fragments.`,
        );
      }
      dbg('[Cockpit] Converted to graph:', { nodes: graphNodes.length, edges: filteredEdges.length });
    return { nodes: graphNodes, edges: filteredEdges };
  }, [norm, dbg, getEdgeStroke, getEdgeCaptionShort, suppressDominatedEdges, isSphereCanonicalPath, isSphereType]);
  // Keep proxy ref updated (used by initial-load effect declared above).
  convertLensItemsToGraphRef.current = convertLensItemsToGraph;

  // Загрузка графа из Cockpit API и сохранение в LensItems
  const handleLoadGraph = useCallback(async () => {
    if (!lensId) {
      setError('Lens ID is required');
      return;
    }

    setLoading(true);
    setError(null);
    // Fresh graph load should always reset shield mode.
    setFragmentProfileActiveNodeId(null);

    try {
      // Reset expansion/grid bookkeeping for a fresh load.
      expandedNodesRef.current = new Set();
      rootNodeIdRef.current = null;
      // Grid is a Freeform-only concept
      if (isFreeformLayout) {
        setGridState(createEmptyGrid());
      }

      // 1. Удаляем существующие LensItems (идемпотентно)
      if (lensItems.length > 0) {
        dbg('[Cockpit] Clearing existing lens items...', lensItems.length);
        // Удаляем в фоне, не ждём завершения
        Promise.all(
          lensItems.map(item => api.deleteLensItem(lensId, item.id).catch(err => {
            console.warn('[Cockpit] Failed to delete lens item', item.id, err);
          }))
        ).then(() => {
          dbg('[Cockpit] Cleared existing lens items');
        });
      }

      // 2. Загружаем граф из Cockpit API
      dbg('[Cockpit] Loading graph from cockpit API...');
      const cockpitData = await api.getHorizonLensCockpit(lensId);
      const viewDirectiveRaw = (cockpitData?.lens?.viewConfig?.viewDirective || null) as HorizonViewDirective | null;
      setViewDirective(viewDirectiveRaw);
      setGraphViewState(normalizeGraphViewState(cockpitData?.lens?.viewConfig?.graphViewState || {}));
      graphViewStateLoadedRef.current = true;
      const configuredGraphView = normalizeGraphView(
        cockpitData?.lens?.viewConfig?.graphView ||
        viewDirectiveRaw?.graphView ||
        cockpitData?.lens?.viewConfig?.layoutMode ||
        layoutMode,
        layoutMode,
      );
      if (configuredGraphView !== layoutMode) {
        setLayoutMode(configuredGraphView);
      }
      const rawFragments = cockpitData.graph.nodes || [];
      const rawEdges = cockpitData.graph.edges || [];
      const scopePaths = new Set(
        (Array.isArray(cockpitData?.lens?.scope) ? cockpitData.lens.scope : [])
          .map((s: any) => String(s || '').trim())
          .filter(Boolean),
      );

      let fragments = rawFragments.filter((f: any) => {
        if (isSphereType(f?.type) || isSphereCanonicalPath(f?.canonicalPath)) return false;
        const cp = String(f?.canonicalPath || '').trim();
        return cp && scopePaths.has(cp);
      });
      let allowedIds = new Set(fragments.map((f: any) => String(f?.id || '').trim()).filter(Boolean));
      let edges = rawEdges.filter((e: any) => {
        if (isSphereType(e?.sourceType) || isSphereType(e?.targetType)) return false;
        if (isSphereCanonicalPath(e?.sourceCanonicalPath) || isSphereCanonicalPath(e?.targetCanonicalPath)) return false;
        const src = String(e?.sourceFragmentId || e?.source || '').trim();
        const dst = String(e?.targetFragmentId || e?.target || '').trim();
        if (!src || !dst) return false;
        return allowedIds.has(src) && allowedIds.has(dst);
      });

      // If scope is empty, fall back to previous behavior (avoid showing an empty graph).
      if (scopePaths.size === 0) {
        const fallbackFragments = rawFragments.filter((f: any) => !(isSphereType(f?.type) || isSphereCanonicalPath(f?.canonicalPath)));
        const fallbackIds = new Set(fallbackFragments.map((f: any) => String(f?.id || '').trim()).filter(Boolean));
        const fallbackEdges = rawEdges.filter((e: any) => {
          if (isSphereType(e?.sourceType) || isSphereType(e?.targetType)) return false;
          if (isSphereCanonicalPath(e?.sourceCanonicalPath) || isSphereCanonicalPath(e?.targetCanonicalPath)) return false;
          const src = String(e?.sourceFragmentId || e?.source || '').trim();
          const dst = String(e?.targetFragmentId || e?.target || '').trim();
          if (!src || !dst) return false;
          return fallbackIds.has(src) && fallbackIds.has(dst);
        });
        dbg('[Cockpit] Scope empty; using fallback graph.');
        fragments = fallbackFragments;
        allowedIds = fallbackIds;
        edges = fallbackEdges;
      }
      

      dbg('[Cockpit] Loaded graph:', { fragments: fragments.length, edges: edges.length });

      // 3. Сразу преобразуем в ReactFlow узлы и рёбра для отображения
      const graphNodes: Node<HorizonNodeData>[] = [];
      const graphEdges: Edge[] = [];
      const fragmentToNodeId = new Map<string, string>();
      const canonicalToNodeId = new Map<string, string>();

      // Создаём узлы из fragments (с временными позициями, ELK пересчитает)
      fragments.forEach((fragment: any, index: number) => {
        const nodeId = norm(fragment.id);
        const fragmentId = nodeId;
        fragmentToNodeId.set(fragmentId, nodeId);
        const canonicalPath = String(fragment?.canonicalPath || '').trim();
        if (canonicalPath) {
          canonicalToNodeId.set(canonicalPath, nodeId);
        }

        // Извлекаем метрики из state
        const stateMetrics: Record<string, number> = {};
        if (fragment.state) {
          Object.entries(fragment.state).forEach(([key, value]: [string, any]) => {
            if (typeof value === 'object' && value !== null && 'value' in value) {
              stateMetrics[key] = typeof value.value === 'number' ? value.value : 0;
            } else if (typeof value === 'number') {
              stateMetrics[key] = value;
            }
          });
        }

        // Временная позиция (ELK пересчитает)
        const position = { x: 0, y: 0 };

        graphNodes.push({
          id: nodeId,
          type: 'horizonFragment',
          position,
          data: {
            id: nodeId,
            lensItemId: '', // Будет обновлено после сохранения
            fragmentId,
            parentFragmentId: undefined,
            rootFragmentId: fragmentId,
            depth: 0,
            canonicalPath: fragment.canonicalPath,
            facilityKind: readFacilityKindFromAny(fragment),
            indexPaths: Array.isArray(fragment.indexPaths) ? fragment.indexPaths : undefined,
            marketSegments: Array.isArray(fragment.marketSegments) ? fragment.marketSegments : undefined,
            marketSegmentNorms: Array.isArray(fragment.marketSegmentNorms) ? fragment.marketSegmentNorms : undefined,
            marketSegmentNormDomains: Array.isArray(fragment.marketSegmentNormDomains) ? fragment.marketSegmentNormDomains : undefined,
            marketSegmentNormVerticals: Array.isArray(fragment.marketSegmentNormVerticals) ? fragment.marketSegmentNormVerticals : undefined,
            marketSegmentNormPairs: Array.isArray(fragment.marketSegmentNormPairs) ? fragment.marketSegmentNormPairs : undefined,
            label: fragment.labels?.en || humanizeCanonicalPath(fragment.canonicalPath) || `Fragment ${index}`,
            type: fragment.type,
            state: Object.keys(stateMetrics).length > 0 ? stateMetrics : undefined,
            showMetrics: showNodeMetrics,
            status: fragment.status,
            expanded: false,
            collapsed: false,
            critical: false,
            onLoadRelated: invokeLoadRelated, // proxy (handler is set via ref)
            onCollapse: invokeCollapse,
            onScopeScout: invokeScopeScout,
          },
        });
      });

      // Создаём рёбра из edges (только для существующих узлов)
      const missingEdges: string[] = [];
      const seenEdgeKeys = new Set<string>();
      edges.forEach((edge: any) => {
        const sourceFragmentId = norm(edge.sourceFragmentId || edge.source);
        const targetFragmentId = norm(edge.targetFragmentId || edge.target);
        const sourceId = sourceFragmentId ? fragmentToNodeId.get(sourceFragmentId) : undefined;
        const targetId = targetFragmentId ? fragmentToNodeId.get(targetFragmentId) : undefined;

        if (sourceId && targetId) {
          const edgeType = edge.type || edge.edgeType || 'UNKNOWN';
          // Dedup to avoid ReactFlow duplicate key warnings.
          // We treat identical (source,target,type) as duplicates for visualization.
          const key = `${sourceId}::${targetId}::${edgeType}`;
          if (seenEdgeKeys.has(key)) {
            return;
          }
          seenEdgeKeys.add(key);

          const baseStyle = getEdgeStyle(edge.type || edge.edgeType, edge.properties?.criticality, edge.properties as any);
          const stroke = getEdgeStroke(baseStyle);
          // В linear режиме используем кривые edges (ReactFlow built-in "default" bezier), в freeform - default
          const edgeTypeForDisplay = 'default';
          
          graphEdges.push({
            // Make ids stable and unique even if backend returns duplicates or lacks id.
            // Use backend edge.id if present; otherwise key.
            id: edge.id || `edge-${key}`,
            source: sourceId,
            target: targetId,
            sourceHandle: 'source-bottom', // Исходящие связи снизу (от кнопки +)
            targetHandle: 'target-top', // Входящие связи сверху
            type: edgeTypeForDisplay,
            // label hidden by default, shown on hover via displayEdges
            // Increase hit-area for clicking thin edges (compatible with ReactFlow v11 types)
            interactionWidth: 18,
            markerEnd: {
              type: MarkerType.ArrowClosed,
              color: stroke,
            },
            style: baseStyle,
            data: {
              lensItemId: '', // Будет обновлено после сохранения
              edgeType,  // Used for label on hover
              edgeCaption: getEdgeCaptionShort(edgeType),
              criticality: edge.properties?.criticality,
              amplification: edge.properties?.amplification,
              latencyHours: edge.properties?.latencyHours,
              direction: 'forward',
              baseStyle,
            },
          });
        }
      });

      // Suppress dominated edges (partners > influence > depends, located_in > depends when directed correctly)
      const filteredEdges = suppressDominatedEdges(graphEdges, graphNodes);

      const viewHighlightIds = new Set<string>();
      const viewFocusIds = new Set<string>();
      if (viewDirectiveRaw?.highlightPaths) {
        for (const p of viewDirectiveRaw.highlightPaths) {
          const nodePaths = Array.isArray(p?.nodePaths) ? p.nodePaths : [];
          for (const cp of nodePaths) {
            const id = canonicalToNodeId.get(String(cp || '').trim());
            if (id) viewHighlightIds.add(id);
          }
        }
      }
      if (Array.isArray(viewDirectiveRaw?.focusNodePaths)) {
        for (const cp of viewDirectiveRaw?.focusNodePaths || []) {
          const id = canonicalToNodeId.get(String(cp || '').trim());
          if (id) viewFocusIds.add(id);
        }
      }

      // 4. Применяем ELK layout и отображаем граф
      dbg('[Cockpit] Applying ELK layout to nodes:', graphNodes.length);
      const layoutedNodes = await layoutFullGraph(graphNodes, filteredEdges, {
        direction: 'DOWN',
        nodeWidth: ELK_NODE_WIDTH,
        nodeHeight: ELK_NODE_HEIGHT,
        nodeSpacing: ELK_NODE_SPACING,
        layerSpacing: ELK_LAYER_SPACING,
        maxChildRows: ELK_MAX_CHILD_ROWS,
        hierarchyAxis: String(viewDirectiveRaw?.hierarchyAxis || '').trim() || undefined,
      });

      dbg('[Cockpit] Setting nodes and edges:', { nodes: layoutedNodes.length, edges: filteredEdges.length });
      // Reset collapse bookkeeping for a fresh graph
      baseNodeIdsRef.current = new Set(layoutedNodes.filter((n) => !(n.data as any)?.isGroup).map((n) => n.id));
      baseEdgeIdsRef.current = new Set(filteredEdges.map((e) => e.id));
      nodeClaimCountRef.current = new Map();
      edgeClaimCountRef.current = new Map();
      expansionsRef.current = new Map();

      // Attach callbacks to nodes
      const withCallbacks = layoutedNodes.map((node) => ({
        ...node,
        data: { ...(node.data as any), onLoadRelated: invokeLoadRelated, onCollapse: invokeCollapse, onScopeScout: invokeScopeScout },
      }));
      setNodes(withCallbacks as any);
      setEdges(filteredEdges);
      setLoading(false);
      if (viewHighlightIds.size > 0) {
        setGraphQueryHighlightIds(new Set(viewHighlightIds));
        setGraphQueryActiveChipId('view-directive');
      } else {
        setGraphQueryHighlightIds(null);
        setGraphQueryActiveChipId(null);
      }

        // Initialize grid cells for all central nodes (depth=0) so second-order expansions
        // can place new centers relative to the correct parent, not (0,0).
        // Это используется только в freeform режиме
        if (isFreeformLayout) {
          try {
            const roots = layoutedNodes
              .filter((n) => !(n.data as any)?.isGroup)
              .filter((n) => typeof (n.data as any)?.depth === 'number' && (n.data as any).depth === 0)
              .sort((a, b) => (a.position?.x || 0) - (b.position?.x || 0));

            const rootId = roots[0]?.id || null;
            rootNodeIdRef.current = rootId;

        let g = createEmptyGrid();
        if (rootId) g.rootNodeId = rootId;
        for (let i = 0; i < roots.length; i++) {
          const rid = String(roots[i].id || '').trim();
          if (!rid) continue;
          g = setGridCell(g, { row: 0, col: i, type: 'node', nodeId: rid });
        }
        setGridState(g);

            dbg('[Cockpit] Grid initialized from root nodes:', {
                roots: roots.map((r) => r.id),
                rootNodeId: g.rootNodeId,
                cellCount: g.cells.size,
              });
          } catch (e) {
            console.warn('[Cockpit] Failed to init grid from roots', e);
          }
        }
        // В linear режиме grid не используется (TODO)

      // Fit view after a short delay
      setTimeout(() => {
        if (reactFlowInstanceRef.current && layoutedNodes.length > 0) {
          const toFit =
            viewFocusIds.size > 0
              ? layoutedNodes.filter((n) => viewFocusIds.has(String(n.id || '').trim()))
              : layoutedNodes;
          reactFlowInstanceRef.current.fitView({ nodes: toFit, padding: 0.1, duration: 300 });
        }
      }, 100);

      // 5. Сохраняем LensItems асинхронно в фоне и обновляем ID
      dbg('[Cockpit] Saving lens items in background...', { nodes: fragments.length, edges: edges.length });
      
      // Создаём LensItems для сохранения
      const posByFragmentId = new Map<string, { x: number; y: number }>();
      for (const n of layoutedNodes) {
        const id = norm(n.id);
        if (id && n.position) posByFragmentId.set(id, n.position);
      }
      const nodeItems: Partial<LensItem>[] = fragments.map((fragment: any, index: number) => {
        const stateMetrics: Record<string, number> = {};
        if (fragment.state) {
          Object.entries(fragment.state).forEach(([key, value]: [string, any]) => {
            if (typeof value === 'object' && value !== null && 'value' in value) {
              stateMetrics[key] = typeof value.value === 'number' ? value.value : 0;
            } else if (typeof value === 'number') {
              stateMetrics[key] = value;
            }
          });
        }

        return {
          kind: 'node' as const,
          subkind: 'fragment',
          fragmentId: norm(fragment.id),
          canonicalPath: fragment.canonicalPath,
          label: fragment.labels?.en || humanizeCanonicalPath(fragment.canonicalPath) || `Fragment ${index}`,
          type: fragment.type,
          state: Object.keys(stateMetrics).length > 0 ? stateMetrics : undefined,
          status: fragment.status,
          position: posByFragmentId.get(norm(fragment.id)),
          critical: false,
          expanded: false,
          collapsed: false,
        };
      });

      // Persist edges in the same order as we render them (graphEdges) to keep response↔edge mapping stable.
      const edgeItems: Partial<LensItem>[] = graphEdges.map((e: any) => {
        const edgeType = String(e?.data?.edgeType || e?.label || e?.data?.subkind || '').trim() || 'DEPENDS_ON';
        return {
          kind: 'edge' as const,
          subkind: edgeType,
          sourceFragmentId: norm(e.source),
          targetFragmentId: norm(e.target),
          edgeType: edgeType as any,
          criticality: e?.data?.criticality,
          amplification: e?.data?.amplification,
          latencyHours: e?.data?.latencyHours,
          direction: 'forward' as const,
        };
      });

      // Сохраняем асинхронно и обновляем ID
      Promise.all([
        ...nodeItems.map(item => api.createLensItem(lensId, item)),
        ...edgeItems.map(item => api.createLensItem(lensId, item)),
      ])
        .then((responses) => {
          dbg('[Cockpit] Saved lens items:', responses.length);
          
          // Обновляем lensItems с полученными ID
          const createdItems: LensItem[] = [];
          const nodeLensItemIdByFragmentId = new Map<string, string>();
          const edgeLensItemIdByLocalEdgeId = new Map<string, string>();
          
          responses.slice(0, nodeItems.length).forEach((response, index) => {
            const itemData = nodeItems[index];
            const fragmentId = String(itemData.fragmentId || '').trim();
            if (fragmentId) nodeLensItemIdByFragmentId.set(fragmentId, response.id);
            createdItems.push({
              id: response.id,
              tenantId: '',
              projectId: projectId || '',
              lensId: lensId,
              kind: 'node',
              subkind: itemData.subkind,
              fragmentId: itemData.fragmentId,
              canonicalPath: itemData.canonicalPath,
              label: itemData.label,
              type: itemData.type,
              state: itemData.state,
              status: itemData.status,
              expanded: itemData.expanded,
              collapsed: itemData.collapsed,
              critical: itemData.critical,
              position: fragmentId ? posByFragmentId.get(fragmentId) : undefined,
              createdAt: new Date().toISOString(),
              updatedAt: new Date().toISOString(),
            });
          });
          
          responses.slice(nodeItems.length, nodeItems.length + edgeItems.length).forEach((response, index) => {
            const itemData = edgeItems[index];
            const localEdgeId = String((graphEdges[index] as any)?.id || '').trim();
            if (localEdgeId) edgeLensItemIdByLocalEdgeId.set(localEdgeId, response.id);
            createdItems.push({
              id: response.id,
              tenantId: '',
              projectId: projectId || '',
              lensId: lensId,
              kind: 'edge',
              subkind: itemData.subkind,
              sourceFragmentId: itemData.sourceFragmentId,
              targetFragmentId: itemData.targetFragmentId,
              edgeType: itemData.edgeType,
              criticality: itemData.criticality,
              amplification: itemData.amplification,
              latencyHours: itemData.latencyHours,
              direction: itemData.direction,
              createdAt: new Date().toISOString(),
              updatedAt: new Date().toISOString(),
            });
          });
          
          // Обновляем состояние с полученными ID
          setLensItems(createdItems);
          // Avoid a full graph refresh: only patch lensItemId fields (keeps positions and avoids jank).
          setNodes((prev) =>
            prev.map((n) => {
              const d: any = n?.data;
              if (d?.isGroup) return n;
              const fid = norm(d?.fragmentId || n.id);
              const nextId = fid ? nodeLensItemIdByFragmentId.get(fid) : undefined;
              if (!nextId || d?.lensItemId === nextId) return n;
              return { ...n, data: { ...d, lensItemId: nextId } } as any;
            }),
          );
          setEdges((prev) =>
            prev.map((e) => {
              const eid = String((e as any)?.id || '').trim();
              const nextId = eid ? edgeLensItemIdByLocalEdgeId.get(eid) : undefined;
              if (!nextId) return e;
              const d: any = (e as any)?.data || {};
              if (d.lensItemId === nextId) return e;
              return { ...(e as any), data: { ...d, lensItemId: nextId } } as any;
            }),
          );
        })
        .catch((err) => {
          console.error('[Cockpit] Failed to create lens items', err);
          setError(err.message || 'Failed to save graph');
          setLoading(false);
        });

    } catch (err: any) {
      console.error('[Cockpit] Failed to load graph', err);
      setError(err.message || 'Failed to load graph');
      setLoading(false);
    }
  }, [lensId, lensItems, convertLensItemsToGraph, setNodes, setEdges]);

  // Callback for group pagination - changes the visible page of children in a group
  const handleGroupPageChange = useCallback((groupId: string, newPage: number) => {
    setNodes((prevNodes) => {
      // Find the group node to get its data
      const groupNode = prevNodes.find((n) => n.id === groupId);
      if (!groupNode || !(groupNode.data as any)?.isGroup) return prevNodes;

      const gd = groupNode.data as any;
      const allChildIds: string[] = gd.groupAllChildIds || [];
      const pageSize = gd.groupPageSize || 18;
      const totalPages = Math.ceil(allChildIds.length / pageSize);

      // Validate new page
      if (newPage < 0 || newPage >= totalPages) return prevNodes;

      const startIdx = newPage * pageSize;
      const endIdx = Math.min(startIdx + pageSize, allChildIds.length);
      const visibleIds = new Set(allChildIds.slice(startIdx, endIdx));

      // Calculate child positions for new page
      const nodeWidth = 180;
      const nodeHeight = 161;
      const childGapX = 60;
      const childGapY = Math.max(48, Math.floor(60 * 1.05));
      const padTop = 34; // GROUP_HEADER_HEIGHT
      const padX = 16;   // GROUP_PADDING_X
      const nodesPerRow = Number.isFinite(gd.groupMaxCols) ? gd.groupMaxCols : 6; // GROUP_MAX_COLS (6)

      // Fast lookup: childId -> index
      const indexById = new Map<string, number>();
      for (let i = 0; i < allChildIds.length; i++) indexById.set(allChildIds[i]!, i);

      return prevNodes.map((n) => {
        // Update group's current page
        if (n.id === groupId) {
          return {
            ...n,
            data: { ...n.data, groupCurrentPage: newPage },
          };
        }

        // Update child visibility and positions
        if ((n as any).parentId === groupId || (n as any).parentNode === groupId) {
          const childIdx = indexById.get(n.id);
          if (childIdx === -1) return n;

          const isVisible = visibleIds.has(n.id);
          // Calculate position relative to current page
          const pageIdx = (childIdx as number) - startIdx;
          const rowIdx = Math.floor(pageIdx / nodesPerRow);
          const colIdx = pageIdx % nodesPerRow;
          // Horizontal fill: left-to-right rows
          const cx = padX + colIdx * (nodeWidth + childGapX);
          const cy = padTop + rowIdx * (nodeHeight + childGapY);

          return {
            ...n,
            hidden: !isVisible,
            position: isVisible ? { x: cx, y: cy } : n.position,
          };
        }

        return n;
      });
    });
  }, [setNodes]);

  const handleCollapseCentral = useCallback(async (rawFragmentId: string) => {
    const fragmentId = (rawFragmentId || '').trim();
    if (!fragmentId) return;
    if (controlCorridorMode && String(controlCorridorSeedId || '').trim() === fragmentId) {
      setControlCorridorGraph(null);
      setControlCorridorSeedId('');
      setControlCorridorError(null);
    }

    const record = expansionsRef.current.get(fragmentId);
    
    // Fallback: if no expansion record exists but the node is marked as expanded,
    // still clear the expanded flag so the UI updates correctly.
    // This can happen if the record was lost due to race conditions or errors.
    if (!record) {
      console.warn('[Cockpit] No expansion record found for', fragmentId, '- clearing expanded flag only');
      const synNodePrefix = `${CORRIDOR_SYN_NODE_PREFIX}${fragmentId}:`;
      const synEdgePrefix = `${CORRIDOR_SYN_EDGE_PREFIX}${fragmentId}:`;
      setEdges((prev) =>
        (prev || []).filter((e: any) => {
          const eid = String((e as any)?.id || '').trim();
          if (eid.startsWith(synEdgePrefix)) return false;
          if (isLegacyCorridorEdgeId(eid)) return false;
          return true;
        }),
      );
      setNodes((prev) =>
        (prev || [])
          .filter((n: any) => {
            const nid = String((n as any)?.id || '').trim();
            if (nid.startsWith(synNodePrefix)) return false;
            if (isLegacyCorridorNodeId(nid)) return false;
            return true;
          })
          .map((n) => {
          if (n.id === fragmentId || (n.data as any)?.fragmentId === fragmentId) {
            return { ...n, data: { ...n.data, expanded: false, isLoadingRelated: false } as any };
          }
          return n;
        }),
      );
      expandedNodesRef.current.delete(fragmentId);
      const fn = onArrangeRef.current;
      if (typeof fn === 'function') fn();
      return;
    }

    // Use latest canonical state snapshots to avoid leaking transient (hover-derived) styles from rendered nodes.
    const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || [];
    const currentEdges: Edge[] = edgesStateRef.current || [];

    const decCount = (m: Map<string, number>, key: string) => {
      const prev = m.get(key) || 0;
      const next = prev - 1;
      if (next <= 0) {
        m.delete(key);
        return 0;
      }
      m.set(key, next);
      return next;
    };

    // 1) Restore touched nodes (freeform grouping absorb) BEFORE removals.
    if (record.touchedNodes && record.touchedNodes.size > 0) {
      for (const [nid, snap] of record.touchedNodes.entries()) {
        // Restore only if the node is still grouped under this collapsing parent (avoid clobbering newer changes).
        const n = currentNodes.find((x) => x.id === nid);
        const curParent = (n?.data as any)?.parentFragmentId;
        if (!n || curParent !== fragmentId) continue;

        // Restore previous data fields and RF parent linkage.
        (n as any).__restore = snap;
      }
    }

    // 2) Restore the expanded (central) node itself if it was promoted/extracted.
    if (record.prevCentral) {
      const n = currentNodes.find((x) => x.id === record.prevCentral!.nodeId);
      if (n) {
        (n as any).__restoreCentral = record.prevCentral;
      }
    }

    // 3) Apply node data restorations
    let nextNodes: Node<HorizonNodeData>[] = currentNodes.map((n) => {
      const restore = (n as any).__restore as undefined | { prevData: any; prevParentId?: string; prevExtent?: any };
      const restoreCentral = (n as any).__restoreCentral as undefined | {
        nodeId: string;
        prevData: any;
        prevParentId?: string;
        prevExtent?: any;
      };

      if (restoreCentral) {
        const prevData = { ...restoreCentral.prevData, expanded: false, isLoadingRelated: false };
        return {
          ...n,
          // Do NOT restore old group container id; containers are rebuilt on relayout.
          parentId: undefined,
          parentNode: undefined as any,
          extent: undefined,
          data: { ...n.data, ...prevData },
        };
      }

      if (restore) {
        return {
          ...n,
          // Same rationale: do not restore old group container ids.
          parentId: undefined,
          parentNode: undefined as any,
          extent: undefined,
          data: { ...n.data, ...restore.prevData },
        };
      }

      // Always clear expanded flag on the collapsing node itself.
      if (n.id === fragmentId || (n.data as any)?.fragmentId === fragmentId) {
        return { ...n, data: { ...n.data, expanded: false, isLoadingRelated: false } as any };
      }

      return n;
    });

    // 4) Remove/keep edges by claims.
    const edgesToRemove = new Set<string>();
    for (const eid of record.claimedEdgeIds) {
      if (baseEdgeIdsRef.current.has(eid)) continue;
      const cnt = decCount(edgeClaimCountRef.current, eid);
      if (cnt === 0) edgesToRemove.add(eid);
    }
    let nextEdges: Edge[] = currentEdges.filter((e) => !edgesToRemove.has(e.id));

    // 5) Remove/keep nodes by claims.
    const nodesToRemove = new Set<string>();
    for (const nid of record.claimedNodeIds) {
      if (nid === fragmentId) continue;
      if (baseNodeIdsRef.current.has(nid)) continue;
      const cnt = decCount(nodeClaimCountRef.current, nid);
      if (cnt !== 0) continue;
      const n = nextNodes.find((x) => x.id === nid);
      const depth = typeof (n?.data as any)?.depth === 'number' ? (n!.data as any).depth : undefined;
      if (depth === 0) continue; // never auto-remove central nodes
      nodesToRemove.add(nid);
    }

    // CASCADE COLLAPSE: 
    // If a removed node was itself expanded, we must recursively clear its expansion record.
    // Otherwise, its children become orphans (claims remain > 0, but parent is gone).
    // We do this by finding all removed nodes that have an entry in expansionsRef,
    // and effectively "collapsing" them too (clearing their claims).
    // Since this is recursive, we use a loop to process cascading removals.
    
    const processedRemovals = new Set<string>();
    const pendingRemovals = Array.from(nodesToRemove);
    
    while (pendingRemovals.length > 0) {
      const removingId = pendingRemovals.pop()!;
      if (processedRemovals.has(removingId)) continue;
      processedRemovals.add(removingId);
      
      // If this removed node was expanded, clear its record and dec counts for ITS children
      if (expansionsRef.current.has(removingId)) {
        const childRecord = expansionsRef.current.get(removingId)!;
        
        // Remove edges claimed by child
        for (const eid of childRecord.claimedEdgeIds) {
          if (baseEdgeIdsRef.current.has(eid)) continue;
          const cnt = decCount(edgeClaimCountRef.current, eid);
          if (cnt === 0) edgesToRemove.add(eid);
        }
        
        // Remove nodes claimed by child
        for (const nid of childRecord.claimedNodeIds) {
          if (nid === removingId) continue; // already removing
          if (baseNodeIdsRef.current.has(nid)) continue;
          const cnt = decCount(nodeClaimCountRef.current, nid);
          if (cnt === 0) {
            if (!nodesToRemove.has(nid)) {
              nodesToRemove.add(nid);
              pendingRemovals.push(nid);
            }
          }
        }
        
        // Cleanup refs
        expansionsRef.current.delete(removingId);
        expandedNodesRef.current.delete(removingId);
      }
    }

    if (nodesToRemove.size > 0) {
      nextNodes = nextNodes.filter((n) => !nodesToRemove.has(n.id));
      // Also drop any edges that now dangle.
      const alive = new Set(nextNodes.map((n) => n.id));
      nextEdges = nextEdges.filter((e) => alive.has(e.source) && alive.has(e.target));
    }

    // 6) Forget record
    expansionsRef.current.delete(fragmentId);

    // Freeform bookkeeping: collapsed node is no longer considered expanded.
    expandedNodesRef.current.delete(fragmentId);
    if (rootNodeIdRef.current === fragmentId) {
      const remaining = Array.from(expandedNodesRef.current.values());
      rootNodeIdRef.current = remaining.length > 0 ? remaining[0] : null;
    }
    if (isFreeformLayout) {
      setGridState((prev) => {
        const next = { ...prev, expandedNodes: new Set(prev.expandedNodes) };
        next.expandedNodes.delete(fragmentId);
        if (next.rootNodeId === fragmentId) {
          next.rootNodeId = rootNodeIdRef.current;
        }
        return next;
      });
    }

    // 7) Re-layout after collapse
    try {
      const fragmentNodes = nextNodes.filter((n) => !(n.data as any)?.isGroup);
      const nodesForArrange =
        isFreeformLayout || isLinearLayout
          ? fragmentNodes
          : (nextNodes as Node<HorizonNodeData>[]);

      const arrangedResult = await arrangeWithGraphView(layoutMode, nodesForArrange as any, nextEdges as any);
      const withCallbacks = (arrangedResult.nodes as any[]).map((node) => {
        if ((node.data as any)?.isGroup) {
          return {
            ...node,
            data: { ...node.data, onGroupPageChange: handleGroupPageChange },
          };
        }
        return {
          ...node,
          data: { ...(node.data as any), onLoadRelated: invokeLoadRelated, onCollapse: invokeCollapse, onScopeScout: invokeScopeScout },
        };
      });
      setNodes(withCallbacks as any);
      setEdges(arrangedResult.edges as any);
      setTimeout(() => focusOnFragment(fragmentId), 80);
    } catch (e) {
      console.error('[Cockpit] Collapse relayout failed', e);
      // Fallback: still update state without relayout
      setNodes(nextNodes as any);
      setEdges(nextEdges as any);
      setTimeout(() => focusOnFragment(fragmentId), 80);
    }
  }, [
    setNodes,
    setEdges,
    isLinearLayout,
    isLevelsLayout,
    isSupplyChainLayout,
    isFreeformLayout,
    setGridState,
    recalculateLinearHandles,
    recalculateLevelsTwoPaneHandles,
    handleGroupPageChange,
    invokeLoadRelated,
    invokeCollapse,
    invokeScopeScout,
    controlCorridorMode,
    controlCorridorSeedId,
    levelsExpandMode,
  ]);

  // Keep ref updated for proxies (avoid circular deps)
  onCollapseRef.current = handleCollapseCentral;

  // Функция для загрузки связанных элементов фрагмента
  const handleLoadRelatedFragments = useCallback(async (rawFragmentId: string) => {
    const fragmentId = (rawFragmentId || '').trim();
    if (!fragmentId || loadingFragmentId === fragmentId) return;

    // Снапшотим исходную ноду СРАЗУ (до await), чтобы второй порядок не ломался.
    // IMPORTANT: exclude group nodes from search - they may have fragmentId from exemplarData
    const rfNodes: Node<HorizonNodeData>[] = nodesStateRef.current || [];
    const sourceSnapshot = rfNodes.find(
      (n) => (n?.id === fragmentId || n?.data?.fragmentId === fragmentId) && !(n?.data as any)?.isGroup
    ) || null;

    if (!sourceSnapshot) {
      console.warn('[Cockpit] Source node not found at click time for fragment:', fragmentId);
      return;
    }

    const isNodeInGroup =
      Boolean((sourceSnapshot as any).parentId || (sourceSnapshot as any).parentNode) &&
      !(sourceSnapshot.data as any)?.isGroup;
    // If expanding a node extracted from a group, it becomes a new local root.
    const sourceRootIdRaw = norm(sourceSnapshot?.data?.rootFragmentId || sourceSnapshot?.data?.fragmentId || sourceSnapshot?.id);
    // In Linear mode, any expanded node becomes a new central node (depth=0) in the middle row.
    const sourceRootId = isLinearLayout ? fragmentId : (isNodeInGroup ? fragmentId : sourceRootIdRaw);
    const sourceDepthRaw = typeof sourceSnapshot?.data?.depth === 'number' ? sourceSnapshot.data.depth : 0;
    const sourceDepth = isLinearLayout ? 0 : (isNodeInGroup ? 0 : sourceDepthRaw);

    // Grid management: determine if this is root expansion or second-order expansion
    // Use refs for synchronous checks (gridState may be stale)
    const isFirstExpansion = rootNodeIdRef.current === null;
    const nodeSemanticGroup = isNodeInGroup
      ? getSemanticGroup(
          (sourceSnapshot.data as any)?.edgeTypeFromParent,
          (sourceSnapshot.data as any)?.directionFromParent !== 'incoming'
        )
      : null;

    dbg('[Cockpit] Expansion context:', {
      fragmentId,
      isFirstExpansion,
      isNodeInGroup,
      nodeSemanticGroup,
      expandedNodes: Array.from(expandedNodesRef.current),
      // Debug: why isNodeInGroup might be wrong
      _debug_parentId: (sourceSnapshot as any).parentId,
      _debug_parentNode: (sourceSnapshot as any).parentNode,
      _debug_isGroup: (sourceSnapshot.data as any)?.isGroup,
      _debug_parentFragmentId: (sourceSnapshot.data as any)?.parentFragmentId,
      _debug_depth: (sourceSnapshot.data as any)?.depth,
    });

    setLoadingFragmentId(fragmentId);
    setNodes((prev) =>
      prev.map((n) =>
        n.id === fragmentId || n.data.fragmentId === fragmentId
          ? { ...n, data: { ...n.data, isLoadingRelated: true } }
          : n,
      ),
    );

    try {
      const laneToEdgeMeta = (laneRaw: string): { edgeType: string; direction: 'incoming' | 'outgoing' } => {
        const lane = String(laneRaw || '').trim().toLowerCase();
        if (lane === 'authority') return { edgeType: 'CORRIDOR_AUTHORITY', direction: 'outgoing' };
        if (lane === 'decision') return { edgeType: 'CORRIDOR_DECISION', direction: 'outgoing' };
        if (lane === 'mechanism') return { edgeType: 'CORRIDOR_MECHANISM', direction: 'outgoing' };
        if (lane === 'impact') return { edgeType: 'CORRIDOR_IMPACT', direction: 'outgoing' };
        return { edgeType: 'CORRIDOR_MEDIATOR', direction: 'outgoing' };
      };
      const corridorLaneById = new Map<string, string>();

      let dependencies: any;
      const expandedType = String((sourceSnapshot?.data as any)?.type || '').trim().toUpperCase();
      const expandedCp = String((sourceSnapshot?.data as any)?.canonicalPath || '').trim();
      const isMarket = expandedType === 'MARKET' || expandedCp.toLowerCase().startsWith('/market/');

      if (isLevelsLayout && levelsExpandMode === 'decision-chain') {
        const result = await runControlCorridorRef.current?.(fragmentId);
        const corridorGraphLocal = result?.graph || null;
        if (!corridorGraphLocal) {
          setNodes((prev) =>
            prev.map((n) =>
              n.id === fragmentId || n.data.fragmentId === fragmentId
                ? { ...n, data: { ...n.data, isLoadingRelated: false, loadRelatedAttempted: true } }
                : n,
            ),
          );
          setLoadingFragmentId(null);
          return;
        }

        const corridorSeedId =
          String((Array.isArray(corridorGraphLocal.seedFragmentIds) ? corridorGraphLocal.seedFragmentIds[0] : '') || '').trim() ||
          fragmentId;
        const corridorNodesRaw = Array.isArray(corridorGraphLocal.nodes) ? corridorGraphLocal.nodes : [];
        const corridorEdgesRaw = Array.isArray(corridorGraphLocal.edges) ? corridorGraphLocal.edges : [];
        const CORRIDOR_GEO_EDGE_TYPES = new Set(['LOCATED_IN', 'OPERATES_IN', 'BASED_IN', 'HEADQUARTERED_IN', 'IN_GEO_SCOPE']);
        const CORRIDOR_GEO_NODE_TYPES = new Set(['GEO_REGION', 'LOCATION', 'COUNTRY', 'STATE', 'PROVINCE', 'CITY']);
        const isGeoCorridorNode = (nodeRaw: any): boolean => {
          const t = String(nodeRaw?.type || '').trim().toUpperCase();
          if (CORRIDOR_GEO_NODE_TYPES.has(t) || t.includes('GEO')) return true;
          const cp = String(nodeRaw?.canonicalPath || '').trim().toLowerCase();
          return cp.startsWith('/loc/');
        };
        const corridorNodesNoGeo = corridorNodesRaw.filter((n: any) => !isGeoCorridorNode(n));
        const corridorEdgesNoGeo = corridorEdgesRaw.filter((e: any) => {
          const et = String(e?.type || '').trim().toUpperCase();
          return et && !CORRIDOR_GEO_EDGE_TYPES.has(et);
        });
        const corridorPathsRaw = Array.isArray((corridorGraphLocal as any)?.bundles?.paths)
          ? ((corridorGraphLocal as any).bundles.paths as ControlCorridorPathBundle[])
          : [];

        // Extract bottleneck node IDs for gatekeeper highlighting.
        const bottleneckNodeIds = new Set<string>(
          (Array.isArray((corridorGraphLocal as any)?.bundles?.bottlenecks?.nodes)
            ? (corridorGraphLocal as any).bundles.bottlenecks.nodes
            : []
          )
            .map((n: any) => String(n?.id || '').trim())
            .filter(Boolean),
        );

        const edgeKeyOf = (fromRaw: any, toRaw: any, typeRaw: any): string => {
          const from = String(fromRaw || '').trim();
          const to = String(toRaw || '').trim();
          const type = String(typeRaw || '').trim().toUpperCase();
          if (!from || !to || !type) return '';
          return `${from}|${to}|${type}`;
        };

        const seededPaths = corridorPathsRaw.filter((p) => {
          const controller = String(p?.controllerId || '').trim();
          const target = String(p?.targetId || '').trim();
          const mediator = String(p?.mediatorId || '').trim();
          const nodeIds = Array.isArray(p?.nodeIds) ? p.nodeIds : [];
          return (
            controller === corridorSeedId ||
            target === corridorSeedId ||
            mediator === corridorSeedId ||
            nodeIds.some((x) => String(x || '').trim() === corridorSeedId)
          );
        });
        const corridorPaths = seededPaths.length > 0 ? seededPaths : corridorPathsRaw;

        const pathNodeIds = new Set<string>();
        const pathEdgeIds = new Set<string>();
        const pathEdgeKeys = new Set<string>();
        const edgeKeyUsageCount = new Map<string, number>();
        const nodeUsageCount = new Map<string, number>();
        for (const p of corridorPaths) {
          const controller = String(p?.controllerId || '').trim();
          const target = String(p?.targetId || '').trim();
          const mediator = String(p?.mediatorId || '').trim();
          if (controller) { pathNodeIds.add(controller); nodeUsageCount.set(controller, (nodeUsageCount.get(controller) || 0) + 1); }
          if (target) { pathNodeIds.add(target); nodeUsageCount.set(target, (nodeUsageCount.get(target) || 0) + 1); }
          if (mediator) { pathNodeIds.add(mediator); nodeUsageCount.set(mediator, (nodeUsageCount.get(mediator) || 0) + 1); }
          const nodeIds = Array.isArray(p?.nodeIds) ? p.nodeIds : [];
          for (const nidRaw of nodeIds) {
            const nid = String(nidRaw || '').trim();
            if (nid) { pathNodeIds.add(nid); nodeUsageCount.set(nid, (nodeUsageCount.get(nid) || 0) + 1); }
          }
          const edgeIds = Array.isArray(p?.edgeIds) ? p.edgeIds : [];
          for (const eidRaw of edgeIds) {
            const eid = String(eidRaw || '').trim();
            if (eid) pathEdgeIds.add(eid);
          }
          const edgeKeys = Array.isArray((p as any)?.edgeKeys) ? (p as any).edgeKeys : [];
          for (const ekRaw of edgeKeys) {
            const ek = String(ekRaw || '').trim();
            if (ek) { pathEdgeKeys.add(ek); edgeKeyUsageCount.set(ek, (edgeKeyUsageCount.get(ek) || 0) + 1); }
          }
        }

        const corridorEdgesScoped = corridorEdgesNoGeo.filter((e: any) => {
          if (pathEdgeIds.size === 0 && pathEdgeKeys.size === 0) return true;
          const eid = String(e?.id || '').trim();
          const key = edgeKeyOf(e?.from, e?.to, e?.type);
          return (eid && pathEdgeIds.has(eid)) || (key && pathEdgeKeys.has(key));
        });
        const corridorEdgesByPathNodes = corridorEdgesNoGeo.filter((e: any) => {
          const from = String(e?.from || '').trim();
          const to = String(e?.to || '').trim();
          return from && to && pathNodeIds.has(from) && pathNodeIds.has(to);
        });
        const corridorEdges =
          corridorEdgesScoped.length > 0
            ? corridorEdgesScoped
            : (corridorEdgesByPathNodes.length > 0 ? corridorEdgesByPathNodes : corridorEdgesNoGeo);

        const corridorEdgeNodeIds = new Set<string>();
        for (const e of corridorEdges) {
          const from = String((e as any)?.from || '').trim();
          const to = String((e as any)?.to || '').trim();
          if (from) corridorEdgeNodeIds.add(from);
          if (to) corridorEdgeNodeIds.add(to);
        }
        const corridorNodesByPaths = corridorNodesNoGeo.filter((n: any) => {
          const id = String(n?.id || '').trim();
          return id && pathNodeIds.has(id);
        });
        const corridorNodesByEdges = corridorNodesNoGeo.filter((n: any) => {
          const id = String(n?.id || '').trim();
          return id && corridorEdgeNodeIds.has(id);
        });
        const corridorNodesInitial =
          corridorNodesByPaths.length > 0
            ? corridorNodesByPaths
            : (corridorNodesByEdges.length > 0 ? corridorNodesByEdges : corridorNodesNoGeo);
        const laneRank = (laneRaw: any): number => {
          const lane = String(laneRaw || '').trim().toLowerCase();
          if (lane === 'impact' || lane === 'propagation') return 1;
          if (lane === 'decision' || lane === 'mechanism' || lane === 'execution' || lane === 'intermediary' || lane === 'mediator') return 2;
          if (lane === 'authority') return 3;
          return -1;
        };
        const corridorLaneByRawId = new Map<string, string>();
        for (const n of corridorNodesInitial) {
          const nid = String(n?.id || '').trim();
          if (!nid) continue;
          corridorLaneByRawId.set(nid, String(n?.lane || '').trim().toLowerCase());
        }
        const monotonicCorridorEdges = corridorEdges.filter((e: any) => {
          const from = String(e?.from || '').trim();
          const to = String(e?.to || '').trim();
          if (!from || !to) return false;
          const sRank = laneRank(corridorLaneByRawId.get(from));
          const tRank = laneRank(corridorLaneByRawId.get(to));
          if (sRank >= 0 && tRank >= 0 && sRank < tRank) {
            return false;
          }
          return true;
        });

        let corridorEdgesForRender = monotonicCorridorEdges.length > 0 ? monotonicCorridorEdges : corridorEdges;

        // Keep only the component that is actually connected to the expanded seed
        // in the rendered corridor edge graph. This prevents "hanging" nodes with no readable
        // link to the focal entity in Decision Chain.
        const renderEdgeNodeIds = new Set<string>();
        const neighbors = new Map<string, Set<string>>();
        const addNeighbor = (a: string, b: string) => {
          const m = neighbors.get(a) || new Set<string>();
          m.add(b);
          neighbors.set(a, m);
        };
        for (const e of corridorEdgesForRender) {
          const from = String((e as any)?.from || '').trim();
          const to = String((e as any)?.to || '').trim();
          if (!from || !to) continue;
          renderEdgeNodeIds.add(from);
          renderEdgeNodeIds.add(to);
          addNeighbor(from, to);
          addNeighbor(to, from);
        }
        const connectedNodeIds = new Set<string>();
        const seedForConnectivity = String(corridorSeedId || '').trim();
        if (seedForConnectivity && renderEdgeNodeIds.has(seedForConnectivity)) {
          const q: string[] = [seedForConnectivity];
          connectedNodeIds.add(seedForConnectivity);
          for (let i = 0; i < q.length; i++) {
            const cur = q[i]!;
            for (const nb of neighbors.get(cur) || []) {
              if (connectedNodeIds.has(nb)) continue;
              connectedNodeIds.add(nb);
              q.push(nb);
            }
          }
        }
        const corridorNodeIdsForRender =
          connectedNodeIds.size > 0 ? connectedNodeIds : renderEdgeNodeIds;
        if (corridorNodeIdsForRender.size > 0) {
          corridorEdgesForRender = corridorEdgesForRender.filter((e: any) => {
            const from = String(e?.from || '').trim();
            const to = String(e?.to || '').trim();
            return from && to && corridorNodeIdsForRender.has(from) && corridorNodeIdsForRender.has(to);
          });
        }
        const corridorNodes = corridorNodesInitial.filter((n: any) => {
          const id = String(n?.id || '').trim();
          if (!id) return false;
          if (id === seedForConnectivity) return true;
          if (corridorNodeIdsForRender.size === 0) return false;
          return corridorNodeIdsForRender.has(id);
        });

        const depEdgesFromCorridor = corridorEdgesForRender
          .map((e: any) => {
            const key = edgeKeyOf(e?.from, e?.to, e?.type);
            const usage = edgeKeyUsageCount.get(key) || 0;
            return {
              id: String(e?.id || '').trim() || key,
              sourceFragmentId: String(e?.from || '').trim(),
              targetFragmentId: String(e?.to || '').trim(),
              type: String(e?.type || '').trim(),
              edgeType: String(e?.type || '').trim(),
              properties: {
                criticality: Number.isFinite(Number(e?.criticality)) ? Number(e.criticality) : 0,
              },
              _corridorPathCount: usage,
            };
          })
          .filter((e: any) => e.sourceFragmentId && e.targetFragmentId && e.type);

        const fragmentsFromCorridor = corridorNodes
          .filter((n: any) => {
            const id = String(n?.id || '').trim();
            return id && id !== corridorSeedId;
          })
          .map((n: any) => {
            const id = String(n?.id || '').trim();
            const label = String(n?.label || '').trim();
            const lane = String(n?.lane || '').trim().toLowerCase() || 'intermediary';
            corridorLaneById.set(id, lane);
            const labels = label ? { en: label } : undefined;
            const corridorDepth = Number.isFinite(Number(n?.depth)) ? Number(n.depth) : 999;
            return {
              id,
              canonicalPath: String(n?.canonicalPath || '').trim() || undefined,
              type: String(n?.type || '').trim() || 'UNKNOWN',
              labels,
              state: {},
              status: 'active',
              _corridorDepth: corridorDepth,
            };
          });

        const maxEdgeUsage = Math.max(1, ...Array.from(edgeKeyUsageCount.values()));
        dependencies = {
          edges: depEdgesFromCorridor,
          fragments: fragmentsFromCorridor,
          _corridor: true,
          _corridorSeedId: corridorSeedId,
          _bottleneckNodeIds: bottleneckNodeIds,
          _corridorEdgeUsage: edgeKeyUsageCount,
          _corridorMaxEdgeUsage: maxEdgeUsage,
          _corridorNodeUsage: nodeUsageCount,
          _corridorTotalPaths: corridorPaths.length,
        };
      } else {
        const graphViewExpansionPolicy = getGraphViewExpansionPolicy(layoutMode, {
          isMarket,
          hierarchicalEdgeTypes: levelsExpandEdgeTypes,
          supplyFlowEdgeTypes: supplyChainExpandEdgeTypes,
        });
        const requestOptions = graphViewExpansionPolicy.usesFilteredExpansion
          ? {
              direction: graphViewExpansionPolicy.direction,
              ...(graphViewExpansionPolicy.edgeTypes.length > 0 ? { edgeTypes: graphViewExpansionPolicy.edgeTypes } : {}),
              includeFragments: graphViewExpansionPolicy.includeFragments,
              ...(graphViewExpansionPolicy.includeUnindexed ? { includeUnindexed: true } : {}),
              maxNeighbors: graphViewExpansionPolicy.maxNeighbors,
              maxPerType: graphViewExpansionPolicy.maxPerType,
              limit: graphViewExpansionPolicy.limit,
            }
          : { direction: graphViewExpansionPolicy.direction };

        const mergeDependencies = (primary: any, secondary: any) => {
          const primaryEdges = Array.isArray(primary?.edges) ? primary.edges : [];
          const secondaryEdges = Array.isArray(secondary?.edges) ? secondary.edges : [];
          const edgeByKey = new Map<string, any>();
          const edgeKeyOf = (e: any): string => {
            const eid = String(e?.id || '').trim();
            if (eid) return `id:${eid}`;
            const s = String(e?.sourceFragmentId || '').trim();
            const t = String(e?.targetFragmentId || '').trim();
            const et = String(e?.type || e?.edgeType || '').trim().toUpperCase();
            return `stt:${s}|${et}|${t}`;
          };
          for (const e of primaryEdges) {
            if (!e) continue;
            edgeByKey.set(edgeKeyOf(e), e);
          }
          for (const e of secondaryEdges) {
            if (!e) continue;
            const k = edgeKeyOf(e);
            if (!edgeByKey.has(k)) edgeByKey.set(k, e);
          }

          const primaryFragments = Array.isArray(primary?.fragments) ? primary.fragments : [];
          const secondaryFragments = Array.isArray(secondary?.fragments) ? secondary.fragments : [];
          const fragmentById = new Map<string, any>();
          const putFragment = (f: any) => {
            const fid = String(f?.id || '').trim();
            if (!fid) return;
            if (!fragmentById.has(fid)) fragmentById.set(fid, f);
          };
          for (const f of primaryFragments) putFragment(f);
          for (const f of secondaryFragments) putFragment(f);

          const mergedEdges = Array.from(edgeByKey.values());
          const mergedFragments = Array.from(fragmentById.values());

          return {
            ...primary,
            edges: mergedEdges,
            total: mergedEdges.length,
            ...(mergedFragments.length > 0 ? { fragments: mergedFragments } : {}),
          };
        };

        const baseDeps = await api.getFragmentDependencies(fragmentId, requestOptions);
        if (isLevelsLayout && isMarket) {
          // Direction-safe market expansion:
          // fetch incoming participants explicitly and merge with the generic "both" payload.
          const incomingMarketDeps = await api.getFragmentDependencies(fragmentId, {
            direction: 'incoming',
            edgeTypes: ['PARTICIPATES_IN', 'TRADED_IN', 'DOMINATES'],
            includeFragments: true,
            includeUnindexed: true,
            maxNeighbors: 72,
            maxPerType: 18,
            limit: 500,
          });
          dependencies = mergeDependencies(baseDeps, incomingMarketDeps);
        } else {
          dependencies = baseDeps;
        }
      }
      dbg('[Cockpit] Loaded dependencies for fragment:', fragmentId, dependencies);

      // === MARKET EXPANSION DEBUG ===
      if (isMarket) {
        const dEdges = Array.isArray(dependencies?.edges) ? dependencies.edges : [];
        const participatesEdges = dEdges.filter((e: any) => {
          const et = String(e?.type || e?.edgeType || '').toUpperCase().trim();
          return et === 'PARTICIPATES_IN' || et === 'TRADED_IN' || et === 'DOMINATES';
        });
        const canvasEdgesToMarket = (edgesStateRef.current || []).filter((e: any) => {
          return norm(e.target) === fragmentId || norm(e.source) === fragmentId;
        });
        console.warn('[MARKET-DEBUG] API returned edges:', dEdges.length, 'PARTICIPATES_IN/TRADED_IN/DOMINATES:', participatesEdges.length);
        console.warn('[MARKET-DEBUG] market participation/trading edges:', participatesEdges.map((e: any) => ({
          source: e?.sourceFragmentId, target: e?.targetFragmentId, type: e?.type || e?.edgeType,
        })));
        console.warn('[MARKET-DEBUG] Canvas edges touching market:', canvasEdgesToMarket.length, canvasEdgesToMarket.map((e: any) => ({
          source: e.source, target: e.target, type: e.data?.edgeType || e.type,
        })));
        console.warn('[MARKET-DEBUG] Fragments from deps:', Array.isArray((dependencies as any)?.fragments) ? (dependencies as any).fragments.length : 0);
      }

      // Bottleneck node IDs from corridor analysis (for gatekeeper highlighting).
      const corridorBottleneckNodeIds: Set<string> =
        (dependencies as any)?._bottleneckNodeIds instanceof Set
          ? (dependencies as any)._bottleneckNodeIds
          : new Set<string>();

      // Per-node path usage count from corridor analysis (how many paths flow through this node).
      const corridorNodeUsage: Map<string, number> =
        (dependencies as any)?._corridorNodeUsage instanceof Map
          ? (dependencies as any)._corridorNodeUsage
          : new Map<string, number>();
      const corridorTotalPaths: number = Number((dependencies as any)?._corridorTotalPaths || 0);

      const depEdges: any[] = Array.isArray(dependencies?.edges) ? dependencies.edges : [];
      if (depEdges.length === 0) {
        setNodes((prev) =>
          prev.map((n) =>
            n.id === fragmentId || n.data.fragmentId === fragmentId
              ? { ...n, data: { ...n.data, isLoadingRelated: false, loadRelatedAttempted: true } }
              : n,
          ),
        );
        setLoadingFragmentId(null);
        return;
      }

      // Уникальные связанные fragmentId
      const relatedFragmentIds = new Set<string>();
      for (const e of depEdges) {
        if (e?.sourceFragmentId && e.sourceFragmentId !== fragmentId) relatedFragmentIds.add(String(e.sourceFragmentId).trim());
        if (e?.targetFragmentId && e.targetFragmentId !== fragmentId) relatedFragmentIds.add(String(e.targetFragmentId).trim());
      }

      const fragmentsFromDeps = Array.isArray((dependencies as any)?.fragments) ? ((dependencies as any).fragments as any[]) : null;
      const byId = new Map<string, any>();
      if (fragmentsFromDeps) {
        for (const f of fragmentsFromDeps) {
          const fid = norm(f?.id || '');
          if (!fid) continue;
          if (!byId.has(fid)) byId.set(fid, f);
        }
      }

      const missingIds: string[] = [];
      for (const id of relatedFragmentIds) {
        if (!byId.has(id)) missingIds.push(id);
      }

      if (missingIds.length > 0) {
        const fragmentPromises = missingIds.map((id) =>
          api.getHorizonFragment(id).catch((err) => {
            console.warn('[Cockpit] Failed to load fragment', id, err);
            return null;
          }),
        );
        const loaded = (await Promise.all(fragmentPromises)).filter((f) => f !== null) as any[];
        for (const f of loaded) {
          const fid = norm(f?.id || '');
          if (!fid) continue;
          if (!byId.has(fid)) byId.set(fid, f);
        }
      }

      let relatedFragments = Array.from(byId.values());
      let filteredDepEdges = depEdges;
      const sphereIds = new Set<string>();
      for (const f of relatedFragments) {
        if (!isSphereEntity(f?.type, f?.canonicalPath)) continue;
        const fid = norm(f?.id || '');
        if (fid) sphereIds.add(fid);
      }
      if (sphereIds.size > 0) {
        relatedFragments = relatedFragments.filter((f) => !sphereIds.has(norm(f?.id || '')));
        filteredDepEdges = depEdges.filter((e) => {
          const s = norm(e?.sourceFragmentId || '');
          const t = norm(e?.targetFragmentId || '');
          return !(sphereIds.has(s) || sphereIds.has(t));
        });
      }
      dbg('[Cockpit] Loaded related fragments:', relatedFragments.length);

      if (filteredDepEdges.length === 0 && relatedFragments.length === 0) {
        setNodes((prev) =>
          prev.map((n) =>
            n.id === fragmentId || n.data.fragmentId === fragmentId
              ? { ...n, data: { ...n.data, isLoadingRelated: false, loadRelatedAttempted: true } }
              : n,
          ),
        );
        setLoadingFragmentId(null);
        setExpandEmptyToast(true);
        setExpandEmptyContext({
          fragmentId,
          canonicalPath: (sourceSnapshot?.data as any)?.canonicalPath,
          isFullNetwork: isLevelsLayout && levelsExpandMode === 'all',
        });
        setExpandModeGlow(true);
        setTimeout(() => setExpandModeGlow(false), 2500);
        return;
      }

      // Получаем актуальное состояние узлов
      const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || [];
      const currentEdges: Edge[] = edgesStateRef.current || [];
      const existingNodeIds = new Set(currentNodes.map((n) => norm(n.id)));
      const fragmentToNodeIdMap = new Map<string, string>();
      const nodeById = new Map<string, Node<HorizonNodeData>>();
      const nodeByFragmentId = new Map<string, Node<HorizonNodeData>>();
      const nodeByCanonicalPath = new Map<string, Node<HorizonNodeData>>();

      for (const n of currentNodes) {
        if ((n.data as any)?.isGroup) continue;
        const fid = n.data?.fragmentId ? norm(n.data.fragmentId) : '';
        const nid = norm(n.id);
        const cp = norm((n.data as any)?.canonicalPath || '');
        if (nid) nodeById.set(nid, n);
        if (fid) nodeByFragmentId.set(fid, n);
        if (fid) fragmentToNodeIdMap.set(fid, nid);
        if (cp && !nodeByCanonicalPath.has(cp)) nodeByCanonicalPath.set(cp, n);
      }

      // Canonical-path aliases:
      // if API returns another fragment id for an already-rendered entity (same canonicalPath),
      // keep a single node and route all edges to it.
      const canonicalOwnerByPath = new Map<string, string>(); // canonicalPath -> owner fragmentId
      for (const [cp, n] of nodeByCanonicalPath.entries()) {
        const owner = norm((n.data as any)?.fragmentId || n.id);
        if (cp && owner) canonicalOwnerByPath.set(cp, owner);
      }
      const canonicalAliasByFragmentId = new Map<string, string>(); // alias fragmentId -> owner fragmentId
      const dedupedRelatedFragments: any[] = [];
      for (const f of relatedFragments) {
        const fid = norm(f?.id || '');
        const cp = norm(f?.canonicalPath || '');
        if (!fid) continue;

        const ownerByCp = cp ? canonicalOwnerByPath.get(cp) : undefined;
        if (ownerByCp && ownerByCp !== fid) {
          canonicalAliasByFragmentId.set(fid, ownerByCp);
          const ownerNode = nodeByFragmentId.get(ownerByCp) || nodeById.get(ownerByCp);
          const ownerNodeId = norm(ownerNode?.id || ownerByCp);
          if (ownerNodeId) {
            fragmentToNodeIdMap.set(fid, ownerNodeId);
            if (ownerNode) nodeByFragmentId.set(fid, ownerNode);
          }
          continue;
        }

        dedupedRelatedFragments.push(f);
        if (cp && !canonicalOwnerByPath.has(cp)) canonicalOwnerByPath.set(cp, fid);
      }
      relatedFragments = dedupedRelatedFragments;

      const resolveNeighborFragmentId = (idRaw: string): string => {
        const id = norm(idRaw);
        if (!id) return '';
        let cur = id;
        const seen = new Set<string>();
        while (canonicalAliasByFragmentId.has(cur) && !seen.has(cur)) {
          seen.add(cur);
          cur = norm(canonicalAliasByFragmentId.get(cur));
        }
        return cur || id;
      };

      // Separate new fragments from existing ones
      const newFragmentsUnsorted = relatedFragments.filter((f) => {
        const fid = norm(f?.id || '');
        return fid && !existingNodeIds.has(fid);
      });

      // Find existing nodes that are related to the expanded node
      // These need to be updated and included in groups
      // EXCLUDE nodes that have already been expanded (they should not be grouped)
      // EXCLUDE root node - it should never be grouped
      const existingRelatedIds = new Set<string>();
      for (const f of relatedFragments) {
        const fid = norm(f?.id || '');
        if (fid && existingNodeIds.has(fid) && fid !== fragmentId) {
          // Don't group the root node - it's the center of the entire graph
          if (fid === rootNodeIdRef.current) {
            dbg('[Cockpit] Skipping root node from grouping:', fid);
            continue;
          }
          // Don't group nodes that have been expanded (they are centers of their own subgraphs)
          if (expandedNodesRef.current.has(fid)) {
            dbg('[Cockpit] Skipping already expanded node from grouping:', fid);
            continue;
          }
          
          // We allow grouping depth=0 and cross-root nodes if they are related and not expanded
          // This enables multi-root graphs to merge when one root expands and captures another
          
          existingRelatedIds.add(fid);
        }
      }
      for (const [aliasFid, ownerFidRaw] of canonicalAliasByFragmentId.entries()) {
        void aliasFid;
        const ownerFid = resolveNeighborFragmentId(ownerFidRaw);
        if (!ownerFid || ownerFid === fragmentId) continue;
        if (!existingNodeIds.has(ownerFid)) continue;
        if (ownerFid === rootNodeIdRef.current) continue;
        if (expandedNodesRef.current.has(ownerFid)) continue;
        existingRelatedIds.add(ownerFid);
      }

      // === MARKET EXPANSION DEBUG (phase 2) ===
      if (isMarket) {
        console.warn('[MARKET-DEBUG] newFragmentsUnsorted:', newFragmentsUnsorted.length,
          newFragmentsUnsorted.slice(0, 5).map((f: any) => ({ id: f?.id, type: f?.type, cp: f?.canonicalPath })));
        console.warn('[MARKET-DEBUG] existingRelatedIds (from API fragments):', Array.from(existingRelatedIds));
      }

      // Also check existing edges on canvas to catch relations not returned by dependencies API.
      // In Levels mode we intentionally keep expansions strict to the backend filter (edgeTypes),
      // except when expanding a MARKET: we must supplement from canvas so orgs with PARTICIPATES_IN
      // appear in the left group even if the API filtered them (e.g. indexPaths, mode).
      const runSupplemental = getGraphViewExpansionPolicy(layoutMode, {
        isMarket,
        hierarchicalEdgeTypes: levelsExpandEdgeTypes,
        supplyFlowEdgeTypes: supplyChainExpandEdgeTypes,
      }).runSupplemental;
      if (runSupplemental) {
        for (const e of currentEdges) {
          if (norm(e.source) === fragmentId && !expandedNodesRef.current.has(norm(e.target))) {
            const tId = norm(e.target);
            const targetNode = nodeById.get(tId) || nodeByFragmentId.get(tId);
            const targetFragId = norm((targetNode?.data as any)?.fragmentId || tId);
            if (
              targetFragId &&
              targetFragId !== fragmentId &&
              targetFragId !== rootNodeIdRef.current &&
              !expandedNodesRef.current.has(targetFragId)
            ) {
              existingRelatedIds.add(targetFragId);
            }
          }
          if (norm(e.target) === fragmentId && !expandedNodesRef.current.has(norm(e.source))) {
            const sId = norm(e.source);
            const sourceNode = nodeById.get(sId) || nodeByFragmentId.get(sId);
            const sourceFragId = norm((sourceNode?.data as any)?.fragmentId || sId);
            if (
              sourceFragId &&
              sourceFragId !== fragmentId &&
              sourceFragId !== rootNodeIdRef.current &&
              !expandedNodesRef.current.has(sourceFragId)
            ) {
              existingRelatedIds.add(sourceFragId);
            }
          }
        }

        // Supplemental from persisted lens edge items:
        // currentEdges in Levels can be proxy/gproxy edges, so we also recover direct fragment relations
        // from lensItems to avoid dropping already-loaded PARTICIPATES_IN neighbors.
        for (const li of (lensItems || [])) {
          const kind = String((li as any)?.kind || '').trim().toLowerCase();
          if (kind !== 'edge') continue;
          const sFrag = norm((li as any)?.sourceFragmentId || '');
          const tFrag = norm((li as any)?.targetFragmentId || '');
          if (!sFrag || !tFrag) continue;
          const edgeTypeLi = String((li as any)?.edgeType || (li as any)?.subkind || '').trim();
          if (!edgeTypeLi) continue;

          if (sFrag === fragmentId) {
            const neighborId = tFrag;
            if (
              neighborId &&
              neighborId !== fragmentId &&
              neighborId !== rootNodeIdRef.current &&
              !expandedNodesRef.current.has(neighborId) &&
              nodeByFragmentId.has(neighborId)
            ) {
              existingRelatedIds.add(neighborId);
            }
          }
          if (tFrag === fragmentId) {
            const neighborId = sFrag;
            if (
              neighborId &&
              neighborId !== fragmentId &&
              neighborId !== rootNodeIdRef.current &&
              !expandedNodesRef.current.has(neighborId) &&
              nodeByFragmentId.has(neighborId)
            ) {
              existingRelatedIds.add(neighborId);
            }
          }
        }
      }

      // === MARKET EXPANSION DEBUG (phase 3) ===
      if (isMarket) {
        console.warn('[MARKET-DEBUG] After supplemental: existingRelatedIds:', Array.from(existingRelatedIds));
        console.warn('[MARKET-DEBUG] newFragmentsUnsorted:', newFragmentsUnsorted.length);
      }

      if (newFragmentsUnsorted.length === 0 && existingRelatedIds.size === 0) {
        dbg('[Cockpit] No new or existing related fragments to process');
        setNodes((prev) =>
          prev.map((n) =>
            n.id === fragmentId || n.data.fragmentId === fragmentId
              ? { ...n, data: { ...n.data, isLoadingRelated: false, loadRelatedAttempted: true } }
              : n,
          ),
        );
        setLoadingFragmentId(null);
        // Show toast notification for empty expand result + glow on expand mode button
        setExpandEmptyToast(true);
        setExpandEmptyContext({
          fragmentId,
          canonicalPath: (sourceSnapshot?.data as any)?.canonicalPath,
          isFullNetwork: isLevelsLayout && levelsExpandMode === 'all',
        });
        setExpandModeGlow(true);
        setTimeout(() => setExpandModeGlow(false), 2500); // Glow fades after toast
        return;
      }

      // Создаём маппинг fragmentId → тип связи и направление относительно родителя
      const fragmentEdgeType = new Map<string, string>();
      const fragmentDirection = new Map<string, 'incoming' | 'outgoing'>();
      type EdgeCandidate = {
        edgeType: string;
        direction: 'incoming' | 'outgoing';
        sourcePriority: number;
        selectionPriority: number;
      };
      const DEP_TYPES = new Set(['DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM']);
      const normalizeEdgeTypeKeyLocal = (t: any): string =>
        String(t || '').toUpperCase().trim().replace(/\s+/g, '_');
      const EDGE_SELECTION_PRIORITY: Record<string, number> = {
        OWNS: 10,
        PARENT_OF: 10,
        SUBSIDIARY_OF: 10,
        CONTROLS: 12,
        MANAGES: 12,
        INVESTS_IN: 14,
        FOUNDED: 16,
        BOARD_MEMBER_OF: 20,
        EXECUTIVE_OF: 20,
        EMPLOYED_BY: 22,
        ADMINISTERS: 24,
        DOMINATES: 30,
        PARTICIPATES_IN: 32,
        TRADED_IN: 33,
        HAS_MARKET_SEGMENT: 34,
        HAS_SUBMARKET: 34,
        HAS_SUBGROUP: 34,
        PRODUCES: 40,
        SUPPLIES_TO: 42,
        TRANSFORMS_INTO: 44,
        LOCATED_IN: 50,
        OPERATES_IN: 52,
        BASED_IN: 52,
        HEADQUARTERED_IN: 52,
        PARTNERS_WITH: 60,
        COOPERATES_WITH: 60,
        AFFILIATED_WITH: 62,
        MEMBER_OF: 62,
        OPERATES_VIA: 62,
        REGULATES: 70,
        INFLUENCES: 72,
        AFFECTS: 72,
        IMPACTS: 72,
        DEPENDS_ON: 80,
        REQUIRES: 80,
        NEEDS: 80,
        USES: 80,
        CONSUMES: 80,
        EXTRACTS_FROM: 80,
        ASSURES: 90,
        AUDITED_BY: 90,
      };
      const directionPreferenceForType = (edgeType: string): 'incoming' | 'outgoing' | 'any' => {
        const et = normalizeEdgeTypeKeyLocal(edgeType);
        if (et === 'LOCATED_IN' || et === 'PRODUCES' || et === 'PARTICIPATES_IN' || et === 'TRADED_IN' || et === 'DOMINATES') return 'outgoing';
        if (layoutMode === 'linear' && DEP_TYPES.has(et)) return 'incoming';
        return 'any';
      };
      const normalizeDirectionForLayout = (
        edgeType: string,
        direction: 'incoming' | 'outgoing',
      ): 'incoming' | 'outgoing' => {
        const et = normalizeEdgeTypeKeyLocal(edgeType);
        if (isLevelsLayout && DEP_TYPES.has(et)) {
          return direction === 'incoming' ? 'outgoing' : 'incoming';
        }
        return direction;
      };
      const selectionPriorityFor = (edgeType: string, direction: 'incoming' | 'outgoing'): number => {
        const et = normalizeEdgeTypeKeyLocal(edgeType);
        if (layoutMode === 'linear' && DEP_TYPES.has(et) && direction === 'incoming') return 5;
        return EDGE_SELECTION_PRIORITY[et] ?? (100 + getEdgePriority(et));
      };
      const candidatesByNeighbor = new Map<string, EdgeCandidate[]>();
      const addEdgeCandidate = (
        neighborIdRaw: string,
        edgeTypeRaw: string,
        directionRaw: 'incoming' | 'outgoing',
        sourcePriority: number,
      ) => {
        const neighborId = resolveNeighborFragmentId(neighborIdRaw);
        if (!neighborId || neighborId === fragmentId) return;
        const edgeType = normalizeEdgeTypeKeyLocal(edgeTypeRaw || '');
        if (!edgeType) return;
        const direction = normalizeDirectionForLayout(edgeType, directionRaw);
        const list = candidatesByNeighbor.get(neighborId) || [];
        list.push({
          edgeType,
          direction,
          sourcePriority,
          selectionPriority: selectionPriorityFor(edgeType, direction),
        });
        candidatesByNeighbor.set(neighborId, list);
      };
      const bestCandidateForNeighbor = (neighborId: string): EdgeCandidate | null => {
        const list = candidatesByNeighbor.get(neighborId) || [];
        if (list.length === 0) return null;
        const sorted = [...list].sort((a, b) => {
          if (a.selectionPriority !== b.selectionPriority) return a.selectionPriority - b.selectionPriority;
          const prefA = directionPreferenceForType(a.edgeType);
          const prefB = directionPreferenceForType(b.edgeType);
          const penA = prefA === 'any' ? 0 : (a.direction === prefA ? 0 : 1);
          const penB = prefB === 'any' ? 0 : (b.direction === prefB ? 0 : 1);
          if (penA !== penB) return penA - penB;
          if (a.sourcePriority !== b.sourcePriority) return a.sourcePriority - b.sourcePriority;
          const pA = getEdgePriority(a.edgeType);
          const pB = getEdgePriority(b.edgeType);
          if (pA !== pB) return pA - pB;
          if (a.direction !== b.direction) return a.direction === 'outgoing' ? -1 : 1;
          return a.edgeType.localeCompare(b.edgeType);
        });
        return sorted[0] || null;
      };

      for (const e of filteredDepEdges) {
        const s = String(e?.sourceFragmentId || '').trim();
        const t = String(e?.targetFragmentId || '').trim();
        const edgeType = String(e?.type || e?.edgeType || '').trim();
        if (!s || !t || !edgeType) continue;
        if (s === fragmentId) addEdgeCandidate(t, edgeType, 'outgoing', 0);
        if (t === fragmentId) addEdgeCandidate(s, edgeType, 'incoming', 0);
      }

      // Supplemental candidates from the current canvas are allowed in:
      // - freeform / linear always
      // - levels only for market expansion (to include visible participants not returned by API filter)
      if (!isLevelsLayout || (isLevelsLayout && isMarket)) {
        for (const e of currentEdges) {
          const s = norm(e.source);
          const t = norm(e.target);
          const edgeType = String((e.data as any)?.edgeType || e.type || '').trim();
          if (!s || !t || !edgeType) continue;
          const sNode = nodeById.get(s) || nodeByFragmentId.get(s);
          const tNode = nodeById.get(t) || nodeByFragmentId.get(t);
          const sFrag = norm((sNode?.data as any)?.fragmentId || s);
          const tFrag = norm((tNode?.data as any)?.fragmentId || t);
          if (sFrag === fragmentId && tFrag) addEdgeCandidate(tFrag, edgeType, 'outgoing', 1);
          if (tFrag === fragmentId && sFrag) addEdgeCandidate(sFrag, edgeType, 'incoming', 1);
        }

        // Supplemental candidates from persisted lens edge items.
        // Priority 2 => weaker than direct dependency response (0) and current-edge supplemental (1).
        for (const li of (lensItems || [])) {
          const kind = String((li as any)?.kind || '').trim().toLowerCase();
          if (kind !== 'edge') continue;
          const sFrag = norm((li as any)?.sourceFragmentId || '');
          const tFrag = norm((li as any)?.targetFragmentId || '');
          const edgeType = String((li as any)?.edgeType || (li as any)?.subkind || '').trim();
          if (!sFrag || !tFrag || !edgeType) continue;
          if (sFrag === fragmentId && nodeByFragmentId.has(tFrag)) addEdgeCandidate(tFrag, edgeType, 'outgoing', 2);
          if (tFrag === fragmentId && nodeByFragmentId.has(sFrag)) addEdgeCandidate(sFrag, edgeType, 'incoming', 2);
        }
      }

      const candidateNeighborIds = new Set<string>([
        ...Array.from(relatedFragmentIds).map((id) => resolveNeighborFragmentId(id)).filter(Boolean),
        ...Array.from(existingRelatedIds).map((id) => resolveNeighborFragmentId(id)).filter(Boolean),
        ...Array.from(candidatesByNeighbor.keys()).map((id) => resolveNeighborFragmentId(id)).filter(Boolean),
      ]);
      for (const neighborId of candidateNeighborIds) {
        const best = bestCandidateForNeighbor(neighborId);
        if (!best) continue;
        fragmentEdgeType.set(neighborId, best.edgeType);
        fragmentDirection.set(neighborId, best.direction);
      }

      if (isLevelsLayout && levelsExpandMode === 'decision-chain' && corridorLaneById.size > 0) {
        for (const rid of relatedFragmentIds) {
          const fid = resolveNeighborFragmentId(rid);
          if (!fid) continue;
          const lane = String(corridorLaneById.get(fid) || '').trim().toLowerCase();
          const fallback = laneToEdgeMeta(lane);
          // Corridor nodes must stay in the corridor group (left pane), not promoted by inherited incoming edges.
          fragmentEdgeType.set(fid, fallback.edgeType);
          fragmentDirection.set(fid, fallback.direction);
        }
      }

      // === MARKET EXPANSION DEBUG (phase 4) ===
      if (isMarket) {
        const allEdgeTypes = new Map<string, number>();
        for (const [nid, et] of fragmentEdgeType.entries()) {
          allEdgeTypes.set(et, (allEdgeTypes.get(et) || 0) + 1);
        }
        console.warn('[MARKET-DEBUG] fragmentEdgeType summary:', Object.fromEntries(allEdgeTypes));
        const participatesNodes = Array.from(fragmentEdgeType.entries()).filter(([, et]) => et === 'PARTICIPATES_IN' || et === 'TRADED_IN' || et === 'DOMINATES');
        console.warn('[MARKET-DEBUG] PARTICIPATES_IN/TRADED_IN/DOMINATES nodes:', participatesNodes.length, participatesNodes);
        console.warn('[MARKET-DEBUG] fragmentDirection for those:', participatesNodes.map(([nid]) => [nid, fragmentDirection.get(nid)]));
      }

      // Log final edge info for all neighbors
      if (layoutMode === 'linear' || isLevelsLayout) {
        // debug logs removed
      }

      // Сортируем фрагменты по приоритету типа связи (важные — левее)
      const newFragments = [...newFragmentsUnsorted].sort((a, b) => {
        const aId = norm(a?.id || '');
        const bId = norm(b?.id || '');
        const aType = fragmentEdgeType.get(aId) || '';
        const bType = fragmentEdgeType.get(bId) || '';
        const aPriority = getEdgePriority(aType);
        const bPriority = getEdgePriority(bType);
        return aPriority - bPriority;
      });

      dbg('[Cockpit] Sorted fragments by edge priority:', 
        newFragments.slice(0, 5).map(f => ({
          id: norm(f?.id || ''),
          edgeType: fragmentEdgeType.get(norm(f?.id || '')),
          priority: getEdgePriority(fragmentEdgeType.get(norm(f?.id || ''))),
        }))
      );

      // Создаём новые ноды (с временными позициями, ELK пересчитает)
      const childDepth = sourceDepth + 1;
      const newNodes: Node<HorizonNodeData>[] = newFragments.map((fragment: any) => {
        const stateMetrics: Record<string, number> = {};
        if (fragment?.state) {
          Object.entries(fragment.state).forEach(([key, value]: [string, any]) => {
            if (typeof value === 'object' && value !== null && 'value' in value) {
              stateMetrics[key] = typeof value.value === 'number' ? value.value : 0;
            } else if (typeof value === 'number') {
              stateMetrics[key] = value;
            }
          });
        }
        const fid = String(fragment.id).trim();
        const edgeTypeFromParent = fragmentEdgeType.get(fid) || '';
        const directionFromParent = fragmentDirection.get(fid) || 'outgoing';
        const corridorLaneForNode =
          (isLevelsLayout && levelsExpandMode === 'decision-chain')
            ? String(corridorLaneById.get(fid) || '').trim().toLowerCase()
            : '';
        // В freeform режиме используется группировка (parentFragmentId, edgeTypeFromParent, directionFromParent)
        // В linear режиме это не используется (TODO)
        return {
          id: fid,
          type: 'horizonFragment' as const,
          position: { x: 0, y: 0 }, // Будет рассчитано
          data: {
            id: fid,
            lensItemId: '',
            fragmentId: fid,
            parentFragmentId: fragmentId, // Используется в freeform для группировки
            rootFragmentId: sourceRootId,
            edgeTypeFromParent, // Тип связи с родителем для сортировки (freeform)
            directionFromParent, // 'incoming' (сверху) или 'outgoing' (снизу) (freeform)
            ...(corridorLaneForNode ? { corridorLane: corridorLaneForNode } : {}),
            ...(typeof (fragment as any)?._corridorDepth === 'number' ? { corridorDepth: (fragment as any)._corridorDepth } : {}),
            ...(corridorBottleneckNodeIds.has(fid) ? { bottleneckHighlight: true } : {}),
            ...(corridorNodeUsage.has(fid) ? { corridorPathCount: corridorNodeUsage.get(fid), corridorTotalPaths } : {}),
            depth: childDepth,
            canonicalPath: fragment.canonicalPath,
            facilityKind: readFacilityKindFromAny(fragment),
            indexPaths: Array.isArray(fragment.indexPaths) ? fragment.indexPaths : undefined,
            marketSegments: Array.isArray(fragment.marketSegments) ? fragment.marketSegments : undefined,
            marketSegmentNorms: Array.isArray(fragment.marketSegmentNorms) ? fragment.marketSegmentNorms : undefined,
            marketSegmentNormDomains: Array.isArray(fragment.marketSegmentNormDomains) ? fragment.marketSegmentNormDomains : undefined,
            marketSegmentNormVerticals: Array.isArray(fragment.marketSegmentNormVerticals) ? fragment.marketSegmentNormVerticals : undefined,
            marketSegmentNormPairs: Array.isArray(fragment.marketSegmentNormPairs) ? fragment.marketSegmentNormPairs : undefined,
            label: fragment.labels?.en || humanizeCanonicalPath(fragment.canonicalPath) || 'Fragment',
            type: fragment.type,
            state: Object.keys(stateMetrics).length > 0 ? stateMetrics : undefined,
            showMetrics: showNodeMetrics,
            status: fragment.status,
            expanded: false,
            collapsed: false,
            critical: false,
            onLoadRelated: handleLoadRelatedFragments,
            onCollapse: invokeCollapse,
            onScopeScout: invokeScopeScout,
          },
        };
      });

      // Добавляем новые фрагменты в маппинг
      for (const n of newNodes) {
        const nid = norm(n.id);
        const fid = norm((n.data as any)?.fragmentId || n.id);
        if (nid) nodeById.set(nid, n);
        if (fid) nodeByFragmentId.set(fid, n);
        if (fid) fragmentToNodeIdMap.set(fid, nid || fid);
      }
      for (const [aliasFid, ownerFidRaw] of canonicalAliasByFragmentId.entries()) {
        const ownerFid = resolveNeighborFragmentId(ownerFidRaw);
        const ownerNodeId = ownerFid ? fragmentToNodeIdMap.get(ownerFid) : '';
        if (ownerNodeId) fragmentToNodeIdMap.set(aliasFid, ownerNodeId);
      }

      // Создаём новые рёбра
      // Симметричные типы связей — показываем только исходящие от раскрытой ноды, чтобы избежать петель
      const SYMMETRIC_EDGE_TYPES = new Set([
        'COMPETES_WITH',
        'PARTNERS_WITH', 
        'COOPERATES_WITH',
        'RELATED_TO',
        'RELATIVE_OF',
        'SIMILAR_TO',
        'LINKED_TO',
      ]);
      
      const existingEdgeIds = new Set(currentEdges.map((e) => e.id));
      const addedEdgePairs = new Set<string>(); // Для отслеживания уже добавленных пар (избежание дубликатов)
      const newEdges: Edge[] = [];

      // If a neighbor has ANY outgoing edge INTO the expanded node (neighbor → expanded),
      // then we do NOT render dependency edges outgoing FROM the expanded node (expanded -(DEPENDS_*)-> neighbor).
      // This avoids confusing bidirectional "Depends" visuals when the relationship is already expressed in reverse.
      const DEP_EDGE_TYPES = new Set(['DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM']);
      const centralFragId = String(fragmentId || '').trim();
      const neighborsWithOutgoingIntoCentral = new Set<string>();
      for (const e of filteredDepEdges) {
        const s0 = String(e?.sourceFragmentId || '').trim();
        const t0 = String(e?.targetFragmentId || '').trim();
        if (!s0 || !t0) continue;
        if (t0 === centralFragId) neighborsWithOutgoingIntoCentral.add(s0);
      }

      for (const e of filteredDepEdges) {
        const s = String(e?.sourceFragmentId || '').trim();
        const t = String(e?.targetFragmentId || '').trim();
        if (!s || !t) continue;
        
        const edgeType = String(e?.type || e?.edgeType || '').toUpperCase();
        if (s === centralFragId && DEP_EDGE_TYPES.has(edgeType) && neighborsWithOutgoingIntoCentral.has(t)) {
          continue;
        }
        const isSymmetric = SYMMETRIC_EDGE_TYPES.has(edgeType);
        
        // Для симметричных связей: фильтруем дубликаты пар (A-B и B-A считаем одним ребром)
        if (isSymmetric) {
          // Проверяем, не добавили ли мы уже эту пару (в любом направлении)
          const pairKey1 = `${s}-${t}`;
          const pairKey2 = `${t}-${s}`;
          if (addedEdgePairs.has(pairKey1) || addedEdgePairs.has(pairKey2)) {
            continue;
          }
          addedEdgePairs.add(pairKey1);
        }
        
        const edgeId = String(e?.id || `${s}-${t}`);
        if (existingEdgeIds.has(edgeId)) continue;

        const sourceId = fragmentToNodeIdMap.get(s);
        const targetId = fragmentToNodeIdMap.get(t);
        if (!sourceId || !targetId) continue;

        // Проверяем, находятся ли обе ноды в одном grid
        // Ноды в одном grid, если:
        // 1. Одна из нод является раскрываемой нодой (fragmentId)
        // 2. Обе ноды имеют одинаковый parentFragmentId или rootFragmentId
        const sourceNode = nodeByFragmentId.get(s) || nodeById.get(s);
        const targetNode = nodeByFragmentId.get(t) || nodeById.get(t);
        
        const sourceParentId = sourceNode ? (sourceNode.data as any)?.parentFragmentId : null;
        const targetParentId = targetNode ? (targetNode.data as any)?.parentFragmentId : null;
        const sourceRootId = sourceNode ? (sourceNode.data as any)?.rootFragmentId : null;
        const targetRootId = targetNode ? (targetNode.data as any)?.rootFragmentId : null;
        
        // Helper to get group position for a node
        const getNodePosition = (nodeId: string): 'top' | 'bottom' | 'left' | 'right' | 'center' | undefined => {
          if (nodeId === fragmentId) return 'center';
          
          const node = nodeByFragmentId.get(nodeId) || nodeById.get(nodeId);
          
          if (!node) return undefined;
          
          const d = node.data as any;
          // Check if node belongs to the current expansion parent
          if (d?.parentFragmentId !== fragmentId && d?.rootFragmentId !== fragmentId && nodeId !== fragmentId) {
             return undefined; 
          }

          // Calculate position based on how node is attached to parent
          const sg = getSemanticGroup(d.edgeTypeFromParent, d.directionFromParent !== 'incoming');
          return SEMANTIC_GROUP_POSITION[sg];
        };

        const sourcePos = getNodePosition(s);
        const targetPos = getNodePosition(t);
        
        // Determine layout position for routing
        let layoutPos: 'top' | 'bottom' | 'left' | 'right' | 'default' = 'default';
        
        // Helper to convert 'center' to 'default'
        const normalizePos = (pos: 'center' | 'left' | 'right' | 'top' | 'bottom' | undefined): 'top' | 'bottom' | 'left' | 'right' | 'default' => {
            if (pos === 'center' || !pos) return 'default';
            return pos;
        };
        
        if (sourcePos === 'center' && targetPos) {
            layoutPos = normalizePos(targetPos);
        } else if (targetPos === 'center' && sourcePos) {
            layoutPos = normalizePos(sourcePos);
        } else if (sourcePos && targetPos && sourcePos === targetPos) {
            layoutPos = normalizePos(sourcePos);
        } else if (sourcePos && !targetPos) {
            layoutPos = normalizePos(sourcePos); 
        } else if (targetPos && !sourcePos) {
            layoutPos = normalizePos(targetPos);
        }

        // Выбор handles для edges зависит от режима раскладки
        // В freeform режиме используем логику с группами и позициями
        // В linear режиме будет другая логика (TODO)
        let handles;
        
        if (isFreeformLayout) {
          // Freeform layout: используем текущую логику с группами и позициями
          if (layoutPos === 'default') {
               // Fallback to standard check for other same-grid cases or default behavior
               const isSameGrid = 
                 s === fragmentId || t === fragmentId || 
                 (sourceParentId && sourceParentId === targetParentId) || 
                 (sourceRootId && sourceRootId === targetRootId) || 
                 (sourceParentId === t || targetParentId === s);
               
               if (isSameGrid) {
                   const isOutgoing = s === fragmentId;
                   handles = getEdgeHandles(edgeType, isOutgoing);
               } else {
                   handles = { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
               }
          } else {
               // Use layoutPos to determine handles (freeform группировка)
               // Is outgoing? 
               // If source is center -> outgoing
               // If target is center -> incoming
               // If both in group -> assume flow direction s->t (treat as outgoing)
               const isFlowOut = sourcePos === 'center' || (sourcePos && targetPos && sourcePos === targetPos);
               
               switch (layoutPos) {
                  case 'top':
                      handles = isFlowOut 
                          ? { sourceHandle: 'source-top', targetHandle: 'target-bottom' }
                          : { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
                      break;
                  case 'bottom':
                      handles = isFlowOut 
                          ? { sourceHandle: 'source-bottom', targetHandle: 'target-top' }
                          : { sourceHandle: 'source-top', targetHandle: 'target-bottom' };
                      break;
                  case 'left':
                      handles = isFlowOut 
                          ? { sourceHandle: 'source-left', targetHandle: 'target-right' }
                          : { sourceHandle: 'source-right', targetHandle: 'target-left' };
                      break;
                  case 'right':
                      handles = isFlowOut 
                          ? { sourceHandle: 'source-right', targetHandle: 'target-left' }
                          : { sourceHandle: 'source-left', targetHandle: 'target-right' };
                      break;
                  default:
                      handles = { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
               }
          }
        } else if (isLinearLayout || isLevelsLayout || isSupplyChainLayout || isInfluenceNetworkLayout || isValueChainLayout) {
          // Linear layout: специальная логика для нижних групп
          // Проверяем, находятся ли ноды в одном grid
          const isSameGrid = 
            s === fragmentId || t === fragmentId || 
            (sourceParentId && sourceParentId === targetParentId) || 
            (sourceRootId && sourceRootId === targetRootId) || 
            (sourceParentId === t || targetParentId === s);
          
          if (isSameGrid) {
            // Определяем, является ли одна из нод центральной (depth=0)
            const sourceDepth = sourceNode ? (sourceNode.data as any)?.depth : undefined;
            const targetDepth = targetNode ? (targetNode.data as any)?.depth : undefined;
            const sourceIsCentral = sourceDepth === 0 || s === fragmentId;
            const targetIsCentral = targetDepth === 0 || t === fragmentId;
            
            // Определяем Linear section для нецентральной ноды
            let bottomNodeSection: 'bottom-left' | 'bottom-right' | null = null;
            let bottomNodeId: string | null = null;
            let isOutgoingFromCentral: boolean | null = null;
            
            if (sourceIsCentral && !targetIsCentral && targetNode) {
              // source = центральная, target = в группе
              const targetData = targetNode.data as any;
              const targetEdgeType = targetData?.edgeTypeFromParent || edgeType || '';
              const targetDirection = targetData?.directionFromParent;
              const targetIsOutgoing = targetDirection !== 'incoming';
              const section = getLinearSection(targetEdgeType, targetIsOutgoing);
              if (section === 'bottom-left' || section === 'bottom-right') {
                bottomNodeSection = section;
                bottomNodeId = t;
                isOutgoingFromCentral = true; // central → bottom node
              }
            } else if (targetIsCentral && !sourceIsCentral && sourceNode) {
              // target = центральная, source = в группе
              const sourceData = sourceNode.data as any;
              const sourceEdgeType = sourceData?.edgeTypeFromParent || edgeType || '';
              const sourceDirection = sourceData?.directionFromParent;
              const sourceIsOutgoing = sourceDirection !== 'incoming';
              const section = getLinearSection(sourceEdgeType, sourceIsOutgoing);
              if (section === 'bottom-left' || section === 'bottom-right') {
                bottomNodeSection = section;
                bottomNodeId = s;
                isOutgoingFromCentral = false; // bottom node → central
              }
            }
            
            // Если одна нода в нижней группе, а другая центральная - применяем правило
            if (bottomNodeSection && (sourceIsCentral || targetIsCentral)) {
              // Нижняя нода использует верхние handles, центральная - нижние
              if (isOutgoingFromCentral) {
                // central(source) → bottom(target)
                handles = { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
              } else {
                // bottom(source) → central(target)
                handles = { sourceHandle: 'source-top', targetHandle: 'target-bottom' };
              }
            } else {
              // Fallback: используем стандартную логику для других случаев внутри grid
              const isOutgoing = s === fragmentId;
              handles = getEdgeHandles(edgeType, isOutgoing);
            }
          } else {
            // Ноды в разных grid - используем стандартные handles
            handles = { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
          }
        } else {
          // Fallback: используем freeform
          handles = { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
        }

        const baseStyle = getEdgeStyle(e?.type || e?.edgeType, e?.properties?.criticality, e?.properties as any);
        const stroke = getEdgeStroke(baseStyle);

        // Corridor flow weight: thicker edges = more path bundles use this edge.
        const corridorUsage = Number((e as any)?._corridorPathCount || 0);
        const isCorridorEdge = isLevelsLayout && levelsExpandMode === 'decision-chain';
        const corridorMaxUsage = isCorridorEdge ? Number((dependencies as any)?._corridorMaxEdgeUsage || 1) : 1;
        const corridorStrokeWidth = isCorridorEdge && corridorUsage > 0
          ? 1.5 + 2.5 * (corridorUsage / Math.max(1, corridorMaxUsage))
          : undefined;
        const finalStyle = corridorStrokeWidth
          ? { ...baseStyle, strokeWidth: corridorStrokeWidth }
          : baseStyle;

        // В linear режиме используем кривые edges (ReactFlow built-in "default" bezier), в freeform - default
        const edgeRenderType = 'default';

        newEdges.push({
          id: edgeId,
          source: sourceId,
          target: targetId,
          sourceHandle: handles.sourceHandle,
          targetHandle: handles.targetHandle,
          type: edgeRenderType,
          // label hidden by default, shown on hover via displayEdges
          interactionWidth: 18,
          markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
          style: finalStyle,
          data: {
            lensItemId: '',
            edgeType: e?.type || e?.edgeType,  // Used for label on hover
            edgeCaption: getEdgeCaptionShort(e?.type || e?.edgeType),
            criticality: e?.properties?.criticality,
            amplification: e?.properties?.amplification,
            latencyHours: e?.properties?.latencyHours,
            direction: 'forward',
            baseStyle: finalStyle,
            corridorEdge: isCorridorEdge,
            corridorPathCount: corridorUsage,
          },
        });
      }

      // If expanding any node, it becomes a local root.
      // - Freeform: extract-from-group + grid reposition
      // - Linear: always promote to depth=0 (global linear centers)
      let preprocessedNodes = currentNodes;
      // Snapshot the clicked node state so collapse can restore it.
      const prevCentralSnapshot = (() => {
        const n = currentNodes.find(
          (x) => (norm(x.data?.fragmentId || x.id) === fragmentId) && !(x.data as any)?.isGroup
        ) || null;
        if (!n) return undefined;
        return {
          nodeId: n.id,
          prevData: { ...(n.data as any) },
          prevParentId: (n as any).parentId,
          prevExtent: (n as any).extent,
        };
      })();
      dbg('[Cockpit] Freeform extraction check:', {
        isNodeInGroup,
        nodeSemanticGroup,
        isFreeformLayout,
        willExtract: isNodeInGroup && nodeSemanticGroup && isFreeformLayout,
      });

      if (isNodeInGroup && nodeSemanticGroup && isFreeformLayout) {
        dbg('[Cockpit] Extracting node from group:', fragmentId, 'semantic group:', nodeSemanticGroup);

        // Find the parent node (the one that expanded and created the group containing this node)
        const expandingNodeParentId = (sourceSnapshot.data as any)?.parentFragmentId;

        preprocessedNodes = currentNodes.map((n) => {
          const nodeFragmentId = norm(n.data?.fragmentId || n.id);

          // Remove the expanding node from its group
          if (nodeFragmentId === fragmentId) {
            // Clear parent relationship so it's no longer grouped
            const newData = { ...n.data };
            delete (newData as any).parentFragmentId;
            delete (newData as any).edgeTypeFromParent;
            delete (newData as any).directionFromParent;
            // Promote to a new subgraph center
            (newData as any).rootFragmentId = fragmentId;
            (newData as any).depth = 0;

            // Calculate new position based on grid (freeform layout only)
            // В freeform режиме используем grid для позиционирования
            let canvasPos = n.position;
            let cellPos: { row: number; col: number } | null = null;
            if (isFreeformLayout) {
              cellPos = findCellForExpandedNode(gridState, expandingNodeParentId || '', nodeSemanticGroup);
              canvasPos = gridToCanvasPosition(cellPos.row, cellPos.col);
            }
            // В linear режиме позиционирование будет другим (TODO)

            dbg('[Cockpit] Repositioning extracted node to grid cell:', cellPos, 'canvas:', canvasPos);

            const extracted = {
              ...n,
              parentId: undefined, // Remove from group container
              parentNode: undefined as any,
              extent: undefined,
              position: canvasPos,
              data: newData,
            };
            dbg('[Cockpit] Extracted node result:', {
              id: extracted.id,
              parentId: (extracted as any).parentId,
              parentNode: (extracted as any).parentNode,
              dataParentFragmentId: (extracted.data as any)?.parentFragmentId,
              dataDepth: (extracted.data as any)?.depth,
            });
            return extracted;
          }
          return n;
        });
      }

      // Linear mode: promote clicked node to central row (depth=0) even if it wasn't in a group container.
      // This is required so that when expanding any node, it exits its group and becomes a central node.
      if (isLinearLayout) {
        preprocessedNodes = preprocessedNodes.map((n) => {
          const nodeFragmentId = norm(n.data?.fragmentId || n.id);
          if (nodeFragmentId !== fragmentId) return n;
          const newData = { ...n.data } as any;
          delete newData.parentFragmentId;
          delete newData.edgeTypeFromParent;
          delete newData.directionFromParent;
          newData.rootFragmentId = fragmentId;
          newData.depth = 0;
          newData.expanded = true;
          newData.collapsed = false;
          return {
            ...n,
            parentId: undefined,
            parentNode: undefined as any,
            extent: undefined,
            data: newData,
          };
        });
      }

      // Levels mode (Two-Pane contract): if user expands a node that was inside a group,
      // it must become a "right" node immediately (before layout), otherwise its newly fetched
      // neighbors won't be placed under its own left block.
      if ((isLevelsLayout || isSupplyChainLayout || isInfluenceNetworkLayout || isValueChainLayout) && isNodeInGroup) {
        preprocessedNodes = preprocessedNodes.map((n) => {
          const nodeFragmentId = norm(n.data?.fragmentId || n.id);
          if (nodeFragmentId !== fragmentId) return n;
          const newData = { ...n.data } as any;
          newData.expanded = true;
          newData.collapsed = false;
          return {
            ...n,
            parentId: undefined,
            parentNode: undefined as any,
            extent: undefined,
            data: newData,
          };
        });
      }

      // Update existing related nodes to include parentFragmentId so they get grouped (freeform layout)
      // This handles cases like: Tesla and BYD both on graph, expanding Tesla should group BYD
          // В freeform режиме используется группировка, в linear - нет (TODO)
      const updatedCurrentNodes = preprocessedNodes.map((n) => {
        const nodeFragmentId = norm(n.data?.fragmentId || n.id);

        // Skip if not an existing related node, or if it's the expanded node.
        // Existing parent linkage is intentionally re-evaluated for this expansion:
        // the current expanded node becomes the deterministic owner for all directly related, non-expanded neighbors.
        if (!existingRelatedIds.has(nodeFragmentId)) return n;
        if (nodeFragmentId === fragmentId) return n;
        // Double-check: never group already expanded nodes
        // Note: we allow grouping the current rootNodeId if it gets absorbed by another expanding node
        if (expandedNodesRef.current.has(nodeFragmentId)) return n;
        
        // Group logic:
        // 1. If node is already part of another subgraph (depth > 0 and different root), don't steal it.
        // 2. If node is a root (depth == 0) and NOT expanded, we CAN absorb it into this graph.
        const nodeDepth = typeof (n.data as any)?.depth === 'number' ? (n.data as any).depth : undefined;
        const nodeRoot = norm((n.data as any)?.rootFragmentId || '');
        
        // In Levels, direct neighbors of the expanded node are always re-parented to this node.
        // This avoids direction-dependent "orphaning" when an incoming relation exists but node
        // currently belongs to another local subgraph.
        if (!isLevelsLayout && nodeDepth !== 0 && nodeRoot && nodeRoot !== sourceRootId) return n;

        const edgeTypeFromParent = fragmentEdgeType.get(nodeFragmentId) || '';
        const directionFromParent = fragmentDirection.get(nodeFragmentId) || 'outgoing';
        const corridorLaneForNode =
          (isLevelsLayout && levelsExpandMode === 'decision-chain')
            ? String(corridorLaneById.get(nodeFragmentId) || '').trim().toLowerCase()
            : '';

        dbg('[Cockpit] Updating existing node for grouping (freeform):', nodeFragmentId, { edgeTypeFromParent, directionFromParent });

        const updated = {
          ...n,
          data: {
            ...n.data,
            // Freeform uses parentFragmentId for container parenting; Linear uses global section containers,
            // but still needs edgeType/direction + depth for correct quadrant placement.
            ...(isFreeformLayout || isLevelsLayout || isSupplyChainLayout || isInfluenceNetworkLayout || isValueChainLayout ? { parentFragmentId: fragmentId } : {}),
            edgeTypeFromParent,
            directionFromParent,
            ...(corridorLaneForNode ? { corridorLane: corridorLaneForNode } : {}),
            // Make absorbed nodes non-central so they move into section groups in Linear.
            depth: childDepth,
            // Keep consistent root marker (Linear treats expanded node as the local root).
            rootFragmentId: sourceRootId,
          },
        };
        return updated;
      });

      // Reset expanded stack state when loading new graph (expand node).
      if (isLevelsLayout || isSupplyChainLayout || isInfluenceNetworkLayout || isValueChainLayout) expandedStackKeysRef.current.clear();

      // Применяем layout для позиционирования новых узлов относительно родителя
      // Логика зависит от режима раскладки (freeform или linear)
      dbg('[Cockpit] Applying layout for new nodes:', newNodes.length, 'existing related:', existingRelatedIds.size, 'layout mode:', layoutMode);
      
      let layoutedNodes: Node<HorizonNodeData>[];
      let linearEdgesAfterLayout: Edge[] | null = null;
      
      if (isFreeformLayout) {
        // Freeform layout: используем текущую логику с группами и grid
        layoutedNodes = await layoutExpansionWithGroups(
          updatedCurrentNodes,
        newNodes,
        fragmentId,
        {
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
        },
      );
      } else if (isLinearLayout || isLevelsLayout || isSupplyChainLayout || isInfluenceNetworkLayout || isValueChainLayout) {
        const mergedEdges: Edge[] = (() => {
          const out: Edge[] = [];
          const seen = new Set<string>();
          for (const e of [...currentEdges, ...newEdges]) {
            if (!e) continue;
            const id = String((e as any).id || '');
            if (id) {
              if (seen.has(id)) continue;
              seen.add(id);
              out.push(e);
              continue;
            }
            const t = String((e as any).data?.edgeType || (e as any).label || (e as any).type || 'UNKNOWN');
            const k = `${(e as any).source}::${(e as any).target}::${t}`;
            if (seen.has(k)) continue;
            seen.add(k);
            out.push({ ...(e as any), id: `edge-${k}` });
          }
          return out;
        })();

        const allNodes = [...updatedCurrentNodes, ...newNodes];
        const nodesForArrange =
          isLinearLayout
            ? allNodes.filter((n) => !(n?.data as any)?.isGroup)
            : allNodes;
        const filteredMergedEdges = suppressDominatedEdges(mergedEdges as any, nodesForArrange as any);
        const arrangedResult = await arrangeWithGraphView(layoutMode, nodesForArrange as any, filteredMergedEdges as any);
        layoutedNodes = arrangedResult.nodes as any;
        linearEdgesAfterLayout = arrangedResult.edges as any;
      } else {
        // Fallback: используем freeform
        layoutedNodes = await layoutExpansionWithGroups(
          updatedCurrentNodes,
          newNodes,
          fragmentId,
          {
            nodeWidth: ELK_NODE_WIDTH,
            nodeHeight: ELK_NODE_HEIGHT,
            nodeSpacing: ELK_NODE_SPACING,
            layerSpacing: ELK_LAYER_SPACING,
            maxChildRows: ELK_MAX_CHILD_ROWS,
          },
        );
      }

      // Debug: check if the expanded node still has parent relationship after layout
      const expandedNodeAfterLayout = layoutedNodes.find(
        (n) => (norm(n.data?.fragmentId || n.id) === fragmentId) && !(n.data as any)?.isGroup
      );
      if (expandedNodeAfterLayout) {
        dbg('[Cockpit] Expanded node AFTER layout:', {
          id: expandedNodeAfterLayout.id,
          parentId: (expandedNodeAfterLayout as any).parentId,
          parentNode: (expandedNodeAfterLayout as any).parentNode,
          dataParentFragmentId: (expandedNodeAfterLayout.data as any)?.parentFragmentId,
          dataDepth: (expandedNodeAfterLayout.data as any)?.depth,
          position: expandedNodeAfterLayout.position,
        });
      }

      // Снимаем флаг загрузки с раскрытого узла и добавляем callbacks
      const finalNodes = layoutedNodes.map((n) => {
        const updates: any = {};
        // Add onGroupPageChange to groups
        if ((n.data as any)?.isGroup) {
          updates.onGroupPageChange = handleGroupPageChange;
        }
        // Remove loading flag from expanded node; add temporary glow
        if (n.id === fragmentId || n.data.fragmentId === fragmentId) {
          updates.isLoadingRelated = false;
          updates.expanded = true;
          updates.collapsed = false;
          updates.loadRelatedAttempted = true;
          updates.expandGlow = true;
        }
        // Ensure callbacks are present on fragment nodes (not groups)
        if (!(n.data as any)?.isGroup) {
          updates.onLoadRelated = handleLoadRelatedFragments;
          updates.onCollapse = invokeCollapse;
          updates.onScopeScout = invokeScopeScout;
        }
        if (Object.keys(updates).length > 0) {
          return { ...n, data: { ...n.data, ...updates } };
        }
        return n;
      });

      // Обновляем состояние
      setNodes(finalNodes);
      if ((isLinearLayout || isLevelsLayout || isSupplyChainLayout || isInfluenceNetworkLayout || isValueChainLayout) && linearEdgesAfterLayout) {
        setEdges(suppressDominatedEdges(linearEdgesAfterLayout as any, finalNodes as any) as any);
      } else {
        setEdges((prev) => {
          const out: any[] = [];
          const seen = new Set<string>();
          for (const e of [...prev, ...newEdges]) {
            if (!e) continue;
            const id = String((e as any).id || '');
            if (id) {
              if (seen.has(id)) continue;
              seen.add(id);
              out.push(e);
              continue;
            }
            // Fallback: source/target/type-based key
            const t = String((e as any).data?.edgeType || (e as any).label || (e as any).type || 'UNKNOWN');
            const k = `${(e as any).source}::${(e as any).target}::${t}`;
            if (seen.has(k)) continue;
            seen.add(k);
            out.push({ ...(e as any), id: `edge-${k}` });
          }
          return suppressDominatedEdges(out as any, finalNodes as any) as any;
        });
      }

      // === Record expansion (for future collapse) ===
      try {
        const incCount = (m: Map<string, number>, key: string) => {
          const prev = m.get(key) || 0;
          m.set(key, prev + 1);
        };

        const preFragmentIds = new Set(
          currentNodes.filter((n) => !(n.data as any)?.isGroup).map((n) => n.id),
        );
        const postFragmentIds = new Set(
          finalNodes.filter((n) => !(n.data as any)?.isGroup).map((n) => n.id),
        );

        // "Neighbors" from deps + existing captures from canvas
        const neighborIds = new Set<string>([
          ...Array.from(relatedFragmentIds),
          ...Array.from(existingRelatedIds),
        ]);

        const claimedNodeIds = new Set<string>();
        for (const nid of neighborIds) {
          const id = norm(nid);
          if (!id || id === fragmentId) continue;
          claimedNodeIds.add(id);
        }
        // Also claim newly added fragment nodes (post - pre)
        for (const nid of postFragmentIds) {
          if (!preFragmentIds.has(nid) && nid !== fragmentId) {
            claimedNodeIds.add(nid);
          }
        }

        const claimedEdgeIds = new Set<string>();
        // Prefer using edges we just introduced (newEdges), regardless of layout mode.
        for (const e of newEdges) {
          if (e?.id) claimedEdgeIds.add(String(e.id));
        }

        // Track touched nodes (freeform absorb): only those we actually modified.
        // We detect modifications by comparing currentNodes vs updatedCurrentNodes for the ids in existingRelatedIds.
        let touchedNodes: ExpansionRecord['touchedNodes'] | undefined;
        if (isFreeformLayout && existingRelatedIds.size > 0) {
          const before = new Map(currentNodes.map((n) => [n.id, n]));
          const after = new Map(updatedCurrentNodes.map((n) => [n.id, n]));
          for (const nid of existingRelatedIds) {
            const id = norm(nid);
            const b = before.get(id);
            const a = after.get(id);
            if (!b || !a) continue;
            const bParent = (b.data as any)?.parentFragmentId;
            const aParent = (a.data as any)?.parentFragmentId;
            if (bParent === aParent) continue;
            touchedNodes = touchedNodes || new Map();
            touchedNodes.set(id, {
              prevData: { ...(b.data as any) },
              prevParentId: (b as any).parentId,
              prevExtent: (b as any).extent,
            });
          }
        }

        // Install record (overwrites if re-expanded)
        expansionsRef.current.set(fragmentId, {
          claimedNodeIds,
          claimedEdgeIds,
          prevCentral: prevCentralSnapshot,
          touchedNodes,
        });

        for (const nid of claimedNodeIds) {
          if (baseNodeIdsRef.current.has(nid)) continue;
          incCount(nodeClaimCountRef.current, nid);
        }
        for (const eid of claimedEdgeIds) {
          if (baseEdgeIdsRef.current.has(eid)) continue;
          incCount(edgeClaimCountRef.current, eid);
        }
      } catch (e) {
        console.warn('[Cockpit] Failed to record expansion', e);
      }

      // Update refs SYNCHRONOUSLY before state (refs are immediate, state is async)
      // This ensures subsequent expansions see the correct values
      // NOTE: Levels mode also needs "expanded" tracking to avoid regrouping already-expanded nodes
      // when using the contract-driven Two-Pane layout.
      if (isFreeformLayout || isLevelsLayout || isSupplyChainLayout || isInfluenceNetworkLayout || isValueChainLayout) {
        expandedNodesRef.current.add(fragmentId);
        if (rootNodeIdRef.current === null) {
          rootNodeIdRef.current = fragmentId;
          dbg('[Cockpit] Refs: set root node:', fragmentId);
        }
        dbg('[Cockpit] Refs: marked as expanded:', fragmentId, 'expandedNodes:', Array.from(expandedNodesRef.current));

        // Update grid state: add expanded node to the grid (freeform only)
        if (isFreeformLayout) setGridState((prevGrid) => {
          let newGrid = { ...prevGrid };

          // If this is the first expansion, set root node at (0, 0)
          if (prevGrid.rootNodeId === null) {
            newGrid.rootNodeId = fragmentId;
            newGrid = setGridCell(newGrid, {
              row: 0,
              col: 0,
              type: 'node',
              nodeId: fragmentId,
            });
            dbg('[Cockpit] Grid: set root node at (0, 0):', fragmentId);
          } else if (isNodeInGroup && nodeSemanticGroup) {
            // Node was extracted from a group, add it to its new cell
            const expandingNodeParentId = (sourceSnapshot.data as any)?.parentFragmentId;
            const cellPos = findCellForExpandedNode(prevGrid, expandingNodeParentId || '', nodeSemanticGroup);
            newGrid = setGridCell(newGrid, {
              row: cellPos.row,
              col: cellPos.col,
              type: 'node',
              nodeId: fragmentId,
            });
            dbg('[Cockpit] Grid: added extracted node at cell:', cellPos, fragmentId);
          }

          // Mark this node as expanded
          const newExpandedNodes = new Set(prevGrid.expandedNodes);
          newExpandedNodes.add(fragmentId);
          newGrid.expandedNodes = newExpandedNodes;

          dbg('[Cockpit] Grid state updated:', {
            rootNodeId: newGrid.rootNodeId,
            expandedNodes: Array.from(newGrid.expandedNodes),
            cellCount: newGrid.cells.size,
          });

          return newGrid;
        });
      }

      // Focus on the expanded node (not the whole graph) and center it
      setTimeout(() => {
        if (reactFlowInstanceRef.current) {
          const inst: any = reactFlowInstanceRef.current;
          const expandedNode = inst.getNode?.(fragmentId);
          if (expandedNode) {
            // Fit view on the expanded node only so it stays prominent
            inst.fitView?.({ nodes: [expandedNode], padding: 0.35, duration: 400 });
            focusOnFragment(fragmentId);
          }
        }
      }, 100);

      // Clear expand glow after ~2.5s
      const glowTimeout = setTimeout(() => {
        setNodes((prev) =>
          prev.map((n) =>
            n.id === fragmentId || n.data.fragmentId === fragmentId
              ? { ...n, data: { ...n.data, expandGlow: false } }
              : n,
          ),
        );
      }, 2500);

      setLoadingFragmentId(null);
    } catch (err: any) {
      console.error('[Cockpit] Failed to load related fragments', err);
      setNodes((prev) =>
        prev.map((n) =>
          n.id === fragmentId || n.data.fragmentId === fragmentId
            ? { ...n, data: { ...n.data, isLoadingRelated: false } }
            : n,
        ),
      );
      setLoadingFragmentId(null);
    }
  }, [
    loadingFragmentId,
    layoutMode,
    isLinearLayout,
    isLevelsLayout,
    isSupplyChainLayout,
    isFreeformLayout,
    levelsExpandEdgeTypes,
    supplyChainExpandEdgeTypes,
    gridState,
    setEdges,
    setNodes,
    normalizeEdgeType,
    getEdgeHandles,
    handleGroupPageChange,
    invokeArrange,
    recalculateLinearHandles,
    recalculateFreeformHandles,
    recalculateSupplyChainHandles,
    levelsExpandMode,
    expandedRoleGroups,
    toggleRoleOverflow,
  ]);

  // Keep ref updated for proxies (avoid circular deps)
  onLoadRelatedRef.current = handleLoadRelatedFragments;
  onScopeScoutRef.current = openScopeScoutModal;

  // Hover highlight: compute a relation-specific cluster for the hovered node.
  // - control: transitive over corporate control relations (OWNS/SUBSIDIARY_OF/CONTROLS/MANAGES/INVESTS_IN/...)
  // - supplies: direct neighbors via SUPPLIES_TO
  // - production: direct neighbors via PRODUCES
  // - competition: direct neighbors via COMPETES_WITH/CONFLICTS_WITH/OPPOSES
  // Best-effort: disabled in perf mode to avoid extra work.
  const hoverHighlight = React.useMemo(() => {
    return computeHoverHighlight({
      edges,
      hoveredNodeId,
      hoverMode: hoverHighlightMode,
      isHierarchicalView: isLevelsLayout,
      nodes,
      perfMode,
    });
  }, [perfMode, hoverHighlightMode, hoveredNodeId, nodes, edges, isLevelsLayout]);

  // Off-screen node previews: show previews for nodes that are outside viewport when hovering.
  // IMPORTANT: Must NOT disturb ReactFlow's internal layout or edge rendering.
  const offScreenPreviews = React.useMemo(() => {
    // Temporarily disabled (UX needs redesign).
    const OFFSCREEN_PREVIEW_ENABLED = false;
    if (!OFFSCREEN_PREVIEW_ENABLED) return [];
    if (perfMode) return [];
    if (!hoverHighlight || !hoveredNodeId) return [];
    
    const hovered = String(hoveredNodeId || '').trim();
    if (!hovered) return [];
    
    const inst: any = reactFlowInstanceRef.current;
    if (!inst) return [];
    
    // Get viewport bounds (in screen pixels relative to the ReactFlow container).
    // In reactflow@11:
    // - node.position is in flow coordinates
    // - viewport { x, y, zoom } maps flow->screen: sx = fx*zoom + x, sy = fy*zoom + y
    const vp = inst.getViewport?.();
    const zoom = typeof vp?.zoom === 'number' ? vp.zoom : 1;
    const vx = typeof vp?.x === 'number' ? vp.x : 0;
    const vy = typeof vp?.y === 'number' ? vp.y : 0;

    // We render previews in an absolute overlay over the ReactFlow wrapper.
    const rf = document.querySelector('.react-flow') as HTMLElement | null;
    if (!rf) return [];
    const rfRect = rf.getBoundingClientRect();
    const viewportWidth = rfRect.width;
    const viewportHeight = rfRect.height;

    const nodeBoxScreen = (n: any) => {
      const px = typeof n?.position?.x === 'number' ? n.position.x : 0;
      const py = typeof n?.position?.y === 'number' ? n.position.y : 0;
      const w = typeof n?.width === 'number' ? n.width : ELK_NODE_WIDTH;
      const h = typeof n?.height === 'number' ? n.height : ELK_NODE_HEIGHT;
      const left = px * zoom + vx;
      const top = py * zoom + vy;
      return { left, top, right: left + w * zoom, bottom: top + h * zoom, w: w * zoom, h: h * zoom };
    };

    const nodeCenterScreen = (n: any) => {
      const b = nodeBoxScreen(n);
      return { x: b.left + b.w / 2, y: b.top + b.h / 2 };
    };

    // Place the preview card INSIDE the viewport with padding so it never overflows the screen.
    // (We don't know the exact rendered width, so we reserve a conservative inset.)
    const PREVIEW_HALF_W = 115;
    const PREVIEW_HALF_H = 34;
    const PREVIEW_PAD = 10;
    const xmin = PREVIEW_HALF_W + PREVIEW_PAD;
    const xmax = Math.max(xmin, viewportWidth - PREVIEW_HALF_W - PREVIEW_PAD);
    const ymin = PREVIEW_HALF_H + PREVIEW_PAD;
    const ymax = Math.max(ymin, viewportHeight - PREVIEW_HALF_H - PREVIEW_PAD);

    const rayToRect = (ox: number, oy: number, dx: number, dy: number) => {
      const EPS = 1e-6;
      let tmin = -Infinity;
      let tmax = Infinity;

      // X slab
      if (Math.abs(dx) < EPS) {
        if (ox < xmin || ox > xmax) return null;
      } else {
        const tx1 = (xmin - ox) / dx;
        const tx2 = (xmax - ox) / dx;
        tmin = Math.max(tmin, Math.min(tx1, tx2));
        tmax = Math.min(tmax, Math.max(tx1, tx2));
      }

      // Y slab
      if (Math.abs(dy) < EPS) {
        if (oy < ymin || oy > ymax) return null;
      } else {
        const ty1 = (ymin - oy) / dy;
        const ty2 = (ymax - oy) / dy;
        tmin = Math.max(tmin, Math.min(ty1, ty2));
        tmax = Math.min(tmax, Math.max(ty1, ty2));
      }

      if (tmax < 0 || tmin > tmax) return null;
      // If origin is inside the rect, entry is behind us; use exit.
      const t = tmin >= 0 ? tmin : tmax;
      if (!Number.isFinite(t) || t < 0) return null;

      const px = ox + dx * t;
      const py = oy + dy * t;
      return {
        x: Math.min(xmax, Math.max(xmin, px)),
        y: Math.min(ymax, Math.max(ymin, py)),
      };
    };
    
    // Find hovered node position
    const hoveredNode = nodes.find((n) => n.id === hovered);
    if (!hoveredNode || !hoveredNode.position) return [];
    
    const hoveredScreenPos = nodeCenterScreen(hoveredNode);
    
    // Find direct edges from hovered node (max 2)
    const directEdges = edges.filter((e) => {
      const source = String(e.source || '').trim();
      const target = String(e.target || '').trim();
      return (source === hovered || target === hovered) && hoverHighlight.edgeIds.has(e.id);
    }).slice(0, 2);
    
    const previews: Array<{
      nodeId: string;
      edgeId: string;
      screenX: number;
      screenY: number;
      angle: number;
      node: Node;
      edge: Edge;
      hoveredScreenX: number;
      hoveredScreenY: number;
    }> = [];
    
    for (const edge of directEdges) {
      const targetNodeId = edge.source === hovered ? edge.target : edge.source;
      const targetNode = nodes.find((n) => n.id === targetNodeId);
      if (!targetNode || !targetNode.position) continue;
      
      const targetScreenPos = nodeCenterScreen(targetNode);
      
      // If the target node is fully visible (with margin), don't show preview.
      const margin = 18;
      const tb = nodeBoxScreen(targetNode);
      const isFullyVisible =
        tb.left >= margin &&
        tb.top >= margin &&
        tb.right <= viewportWidth - margin &&
        tb.bottom <= viewportHeight - margin;
      if (isFullyVisible) continue;
      
      // Calculate angle from hovered node to target node
      const dx = targetScreenPos.x - hoveredScreenPos.x;
      const dy = targetScreenPos.y - hoveredScreenPos.y;
      const angle = Math.atan2(dy, dx);
      const normalizedAngle = angle < 0 ? angle + 2 * Math.PI : angle;

      // Ray-rectangle intersection against the *padded* rect (keeps preview on-screen).
      const hit = rayToRect(hoveredScreenPos.x, hoveredScreenPos.y, dx, dy);
      if (!hit) continue;
      const previewX = hit.x;
      const previewY = hit.y;
      
      previews.push({
        nodeId: targetNodeId,
        edgeId: edge.id,
        screenX: previewX,
        screenY: previewY,
        angle: normalizedAngle,
        node: targetNode,
        edge: edge,
        hoveredScreenX: hoveredScreenPos.x,
        hoveredScreenY: hoveredScreenPos.y,
      });
    }
    
    return previews;
  }, [hoverHighlight, hoveredNodeId, nodes, edges, perfMode]);

  const offScreenPreviewEdgeIds = React.useMemo(() => {
    const s = new Set<string>();
    for (const p of offScreenPreviews as any[]) {
      const id = String((p as any)?.edgeId || '').trim();
      if (id) s.add(id);
    }
    return s;
  }, [offScreenPreviews]);

  // Pagination: hide children on non-active pages; never render edges connected to hidden nodes.
  const hiddenNodeIds = React.useMemo(() => {
    const out = new Set<string>();
    for (const n of nodes) {
      if ((n as any).hidden) out.add(n.id);
    }
    return out;
  }, [nodes]);

  const visibleEdges = React.useMemo(() => {
    const isGeoType = (t: string) => {
      const tt = String(t || '').toUpperCase().trim().replace(/\s+/g, '_');
      if (!tt) return false;
      if (tt.includes('GEO')) return true;
      return tt === 'LOCATION' || tt === 'COUNTRY' || tt === 'STATE' || tt === 'PROVINCE' || tt === 'CITY' || tt === 'REGION';
    };
    const isPersonType = (t: string) => {
      const tt = String(t || '').toUpperCase().trim().replace(/\s+/g, '_');
      return tt === 'PERSON';
    };
    const INFLUENCEISH = new Set(['INFLUENCES', 'AFFECTS', 'IMPACTS', 'REGULATES']);
    const LOCATEDISH = new Set(['LOCATED_IN', 'OPERATES_IN', 'BASED_IN', 'HEADQUARTERED_IN', 'IN_GEO_SCOPE']);
    const PARTNERSHIPISH = new Set(['PARTNERS_WITH']);
    const edgeTypeOf = (e: any): string => {
      const raw = (e as any)?.data?.edgeType || (e as any)?.data?.subkind || (e as any)?.label || (e as any)?.type || '';
      return String(raw || '').toUpperCase().trim();
    };

    // Map nodeId + fragmentId -> visual type (stack controllers can override type via stackVisualType).
    const typeByKey = new Map<string, string>();
    const paneByKey = new Map<string, string>(); // Levels: right/left/dock (used to scope some edge filtering)
    for (const n of nodes) {
      const d: any = (n as any)?.data;
      if (d?.isGroup) continue;
      const id = String((n as any)?.id || '').trim();
      const fid = String(d?.fragmentId || '').trim();
      const vt = String(d?.stackVisualType || d?.type || '').toUpperCase().trim().replace(/\s+/g, '_');
      if (id) typeByKey.set(id, vt);
      if (fid) typeByKey.set(fid, vt);
      const pane = String(d?.levelsPane || '').trim();
      if (id && pane) paneByKey.set(id, pane);
      if (fid && pane) paneByKey.set(fid, pane);
    }

    // Suppress Influence-ish edges between:
    // - ORGANIZATION ↔ GEO
    // - ORGANIZATION ↔ PERSON
    // when ANY other relation exists between the same pair. Rule is direction-agnostic.
    const pairHasNonInfluence = new Set<string>();
    const pairHasAnyInfluence = new Set<string>();
    const pairKey = (a: string, b: string) => (a < b ? `${a}||${b}` : `${b}||${a}`);

    for (const e of edges) {
      const s = String((e as any)?.source || '').trim();
      const t = String((e as any)?.target || '').trim();
      if (!s || !t || s === t) continue;
      const st = typeByKey.get(s) || '';
      const tt = typeByKey.get(t) || '';
      if (!st || !tt) continue;
      const isOrgGeo = (st === 'ORGANIZATION' && isGeoType(tt)) || (tt === 'ORGANIZATION' && isGeoType(st));
      const isOrgPerson = (st === 'ORGANIZATION' && isPersonType(tt)) || (tt === 'ORGANIZATION' && isPersonType(st));
      if (!isOrgGeo && !isOrgPerson) continue;
      const et = edgeTypeOf(e);
      const k = pairKey(s, t);
      if (INFLUENCEISH.has(et)) pairHasAnyInfluence.add(k);
      else pairHasNonInfluence.add(k);
    }

    const topDepNodeSet = domainTopDependencyFocusActive ? domainTopDependencyNodeIds : null;
    const topDepEdgeSet = domainTopDependencyFocusActive ? domainTopDependencyEdgeIds : null;
    const keepByDomainTopDep = (e: any): boolean => {
      const s = String((e as any)?.source || '').trim();
      const t = String((e as any)?.target || '').trim();
      if (!s || !t) return false;
      const eid = String((e as any)?.id || '').trim();
      if (topDepEdgeSet && eid && topDepEdgeSet.has(eid)) return true;
      return !!topDepNodeSet && topDepNodeSet.has(s) && topDepNodeSet.has(t);
    };

    let base = hiddenNodeIds.size === 0
      ? edges
      : edges.filter((e) => (!hiddenNodeIds.has(e.source) && !hiddenNodeIds.has(e.target)) || keepByDomainTopDep(e));

    // Suppress clearly inconsistent geo-partnership visuals:
    // if a pair involving GEO has both location semantics and PARTNERS_WITH,
    // keep location edge(s) and suppress PARTNERS_WITH on the frontend.
    // NOTE: this is visual suppression only; data remains intact.
    const pairKeyGeo = (a: string, b: string) => (a < b ? `${a}||${b}` : `${b}||${a}`);
    const geoPairsWithLocated = new Set<string>();
    const geoPairsWithPartnership = new Set<string>();
    if (base.length > 0) {
      for (const e of base) {
        const s = String((e as any)?.source || '').trim();
        const t = String((e as any)?.target || '').trim();
        if (!s || !t || s === t) continue;
        const st = typeByKey.get(s) || '';
        const tt = typeByKey.get(t) || '';
        if (!isGeoType(st) && !isGeoType(tt)) continue;
        const et = edgeTypeOf(e);
        const k = pairKeyGeo(s, t);
        if (LOCATEDISH.has(et)) geoPairsWithLocated.add(k);
        if (PARTNERSHIPISH.has(et)) geoPairsWithPartnership.add(k);
      }
      if (geoPairsWithLocated.size > 0 && geoPairsWithPartnership.size > 0) {
        const suppressedPairs = new Set<string>();
        base = base.filter((e) => {
          const s = String((e as any)?.source || '').trim();
          const t = String((e as any)?.target || '').trim();
          if (!s || !t || s === t) return true;
          const st = typeByKey.get(s) || '';
          const tt = typeByKey.get(t) || '';
          if (!isGeoType(st) && !isGeoType(tt)) return true;
          const et = edgeTypeOf(e);
          if (!PARTNERSHIPISH.has(et)) return true;
          const k = pairKeyGeo(s, t);
          if (!geoPairsWithLocated.has(k)) return true;
          suppressedPairs.add(k);
          return false;
        });
        if (suppressedPairs.size > 0 && DEV) {
          for (const k of suppressedPairs) {
            if (edgePolicyWarnedPairsRef.current.has(k)) continue;
            edgePolicyWarnedPairsRef.current.add(k);
            console.warn('[HZN:EDGE_POLICY] Suppressed PARTNERS_WITH for GEO pair due to LOCATED/OPERATES relation', { pairKey: k });
          }
        }
      }
    }

    // Apply influence suppression (ORG↔GEO / ORG↔PERSON) when a stronger non-influence relation exists.
    if (pairHasNonInfluence.size > 0 && pairHasAnyInfluence.size > 0) {
      base = base.filter((e) => {
        const s = String((e as any)?.source || '').trim();
        const t = String((e as any)?.target || '').trim();
        if (!s || !t || s === t) return true;
        const st = typeByKey.get(s) || '';
        const tt = typeByKey.get(t) || '';
        if (!st || !tt) return true;
        const isOrgGeo = (st === 'ORGANIZATION' && isGeoType(tt)) || (tt === 'ORGANIZATION' && isGeoType(st));
        const isOrgPerson = (st === 'ORGANIZATION' && isPersonType(tt)) || (tt === 'ORGANIZATION' && isPersonType(st));
        if (!isOrgGeo && !isOrgPerson) return true;
        const k = pairKey(s, t);
        if (!pairHasNonInfluence.has(k)) return true;
        const et = edgeTypeOf(e);
        if (INFLUENCEISH.has(et)) return false;
        return true;
      });
    }

    return filterEdgesForGraphView(layoutMode, base, {
      viewDirective,
      edgeViewFilter,
      showSecondaryEdges,
      topDepEdgeSet,
      paneByKey,
      edgeTypeOf,
      getSemanticGroup,
      getEdgePriority,
    });
  }, [
    edges,
    hiddenNodeIds,
    nodes,
    layoutMode,
    viewDirective,
    DEV,
    domainTopDependencyFocusActive,
    domainTopDependencyNodeIds,
    domainTopDependencyEdgeIds,
    edgeViewFilter,
    showSecondaryEdges,
  ]);

  // Stable node structure info (only changes when nodes are added/removed/hidden, NOT on style changes).
  // This prevents hubNodeIds from re-computing on every hover/style update.
  const stableNodeStructure = React.useMemo(() => {
    const visibleFragIds = new Set<string>();
    const groupIds = new Set<string>();
    for (const n of nodes) {
      const id = String(n.id || '').trim();
      if (!id) continue;
      if ((n as any).hidden) continue;
      if ((n.data as any)?.isGroup) groupIds.add(id);
      else visibleFragIds.add(id);
    }
    // Create a stable key that only changes when structure changes
    const key = `${visibleFragIds.size}:${groupIds.size}:${Array.from(visibleFragIds).sort().join(',')}`;
    return { visibleFragIds, groupIds, key };
  }, [nodes]);

  // Hub nodes: inDegree >= 5 and/or outDegree >= 5 (visible graph only, ignore group containers).
  // Now depends on stableNodeStructure.key instead of full nodes array.
  const hubNodeIds = React.useMemo(() => {
    const { visibleFragIds, groupIds } = stableNodeStructure;
    
    const inDeg = new Map<string, number>();
    const outDeg = new Map<string, number>();
    const inc = (m: Map<string, number>, k: string) => m.set(k, (m.get(k) || 0) + 1);

    for (const e of visibleEdges) {
      const s = String((e as any)?.source || '').trim();
      const t = String((e as any)?.target || '').trim();
      if (!s || !t) continue;
      if (!visibleFragIds.has(s) || !visibleFragIds.has(t)) continue;
      if (groupIds.has(s) || groupIds.has(t)) continue;
      inc(outDeg, s);
      inc(inDeg, t);
    }

    const out = new Set<string>();
    for (const id of visibleFragIds) {
      const i = inDeg.get(id) || 0;
      const o = outDeg.get(id) || 0;
      if (i >= 5 || o >= 5) out.add(id);
    }
    return out;
  }, [stableNodeStructure, visibleEdges]);

  // Keep edge hover when interacting with the HTML caption.
  const handleEdgeLabelMouseEnter = React.useCallback(
    (edgeId: string) => {
      if (perfMode) return;
      pendingHoverRef.current.hoveredEdgeId = edgeId;
      scheduleHoverCommit();
    },
    [perfMode, scheduleHoverCommit],
  );

  const handleEdgeLabelMouseLeave = React.useCallback(
    (edgeId: string) => {
      if (perfMode) return;
      if (pendingHoverRef.current.hoveredEdgeId === edgeId) {
        pendingHoverRef.current.hoveredEdgeId = null;
      }
      scheduleHoverCommit();
    },
    [perfMode, scheduleHoverCommit],
  );

  // Base edge presentation (labels hidden, dimmed) - computed only when the underlying edge set changes.
  // This avoids O(E) object cloning on every hover state update when nothing is hovered/selected.
  // Note: use openEdgeExplainPopoverRef to avoid dependency on callback identity.
  const baseVisibleEdges = React.useMemo(() => {
    const DIM_OPACITY = 0.22;
    const DOMAIN_DIM_OPACITY = 0.08;
    if (perfMode) return visibleEdges;
    // Capture ref value once for this computation (stable across renders)
    const onExplain = openEdgeExplainPopoverRef.current;
    return visibleEdges.map((e) => {
      const baseStyle = (e as any)?.data?.baseStyle || e.style || {};
      const isArcTop = e.type === 'arcTop';
      const d: any = (e as any)?.data || {};
      const srcType = String(d?.sourceType || '').toUpperCase().trim();
      const dstType = String(d?.targetType || '').toUpperCase().trim();
      const srcPath = String(d?.sourceCanonicalPath || '').trim();
      const dstPath = String(d?.targetCanonicalPath || '').trim();
      const isDomainIncident =
        srcType === 'DOMAIN' ||
        dstType === 'DOMAIN' ||
        srcPath.startsWith('/domain/') ||
        dstPath.startsWith('/domain/');
      const opacity = isDomainIncident ? DOMAIN_DIM_OPACITY : DIM_OPACITY;
      return {
        ...e,
        type: isArcTop ? 'hznArcTop' : 'hzn',
        data: { ...d, onExplain, showLabel: false, onLabelHoverEnter: handleEdgeLabelMouseEnter, onLabelHoverLeave: handleEdgeLabelMouseLeave },
        label: undefined,
        // Default edge rendering should be quiet; DOMAIN edges are even quieter unless user hovers/selects.
        style: isDomainIncident
          ? { ...baseStyle, opacity, strokeOpacity: opacity }
          : { ...baseStyle, opacity },
      };
    });
  }, [visibleEdges, perfMode, handleEdgeLabelMouseEnter, handleEdgeLabelMouseLeave]);

  // Levels: derive aggKeys per node once per edge-set, so hover can suppress aggregated edges without an extra full scan.
  const levelsAggKeysByNodeId = React.useMemo(() => {
    if (!isLevelsLayout && !isSupplyChainLayout) return new Map<string, Set<string>>();
    const map = new Map<string, Set<string>>();
    for (const e of baseVisibleEdges) {
      const d: any = (e as any)?.data || {};
      if (!d.levelsHiddenByAggregation) continue;
      // For certain edges (e.g., cross-group edges proxied to group handlers), we never reveal the original,
      // so we also must NOT suppress the aggregated edge on hover.
      if (d.levelsNoHoverReveal) continue;
      const k = String(d.levelsAggKey || '').trim();
      if (!k) continue;
      const s = String((e as any)?.source || '').trim();
      const t = String((e as any)?.target || '').trim();
      if (s) {
        const set = map.get(s) || new Set<string>();
        set.add(k);
        map.set(s, set);
      }
      if (t) {
        const set = map.get(t) || new Set<string>();
        set.add(k);
        map.set(t, set);
      }
    }
    return map;
  }, [isLevelsLayout, baseVisibleEdges]);

  const signalPropagationNodeIdSet = React.useMemo<Set<string> | null>(() => {
    if (hoverHighlightMode !== 'signal-propagation' || !signalPropagationOverlay) return null;
    const out = new Set<string>();
    for (const id of signalPropagationOverlay.nodeIds || []) {
      const nodeId = String(id || '').trim();
      if (nodeId) out.add(nodeId);
    }
    return out.size > 0 ? out : null;
  }, [hoverHighlightMode, signalPropagationOverlay]);

  const signalPropagationEdgeIdSet = React.useMemo<Set<string> | null>(() => {
    if (hoverHighlightMode !== 'signal-propagation' || !signalPropagationOverlay) return null;
    const out = new Set<string>();
    for (const id of signalPropagationOverlay.edgeIds || []) {
      const edgeId = String(id || '').trim();
      if (edgeId) out.add(edgeId);
    }
    return out.size > 0 ? out : null;
  }, [hoverHighlightMode, signalPropagationOverlay]);

  // Derive edges styles for hover highlighting (do not mutate base edges)
  // Labels are hidden by default to reduce visual noise, shown only on hover
  const displayEdges = React.useMemo(() => {
    const DIM_OPACITY = 0.22;
    const FADE_OPACITY = 0.12;
    const profileActiveId = String(fragmentProfileActiveNodeId || '').trim();
    const profileActive = !!profileActiveId;
    const signalModeActive = hoverHighlightMode === 'signal-propagation';

    const applyFragmentProfileDimming = (arr: Edge[]) => {
      if (!profileActive) return arr;
      return (arr || []).map((e: any) => {
        const baseStyle: any = (e as any)?.style || {};
        const curOpacity = typeof baseStyle.opacity === 'number' ? baseStyle.opacity : DIM_OPACITY;
        // In Fragment Profile mode, edges are muted uniformly.
        const nextOpacity = Math.min(curOpacity, 0.06);
        const d0: any = (e as any)?.data || {};
        return {
          ...(e as any),
          label: undefined,
          data: { ...d0, showLabel: false },
          style: { ...baseStyle, opacity: nextOpacity },
        } as any;
      });
    };

    // Perf mode: do not derive per-edge styles/labels (avoids O(E) object cloning on hover),
    // just return filtered edges.
    if (perfMode && !profileActive && !signalModeActive) return baseVisibleEdges;
    if (profileActive) return applyFragmentProfileDimming(baseVisibleEdges);

    // Levels Two-Pane aggregation:
    // - hide per-child edges by default (they exist for hover reveal)
    // - show per-child edges incident to hovered node
    // - hide the aggregated edge for the hovered node's bucket to reduce clutter
    let edgesForDisplay: Edge[] = baseVisibleEdges;
    if (isLevelsLayout || isSupplyChainLayout) {
      const hovered = String(hoveredNodeId || '').trim();
      const aggKeysToSuppress = hovered ? (levelsAggKeysByNodeId.get(hovered) || null) : null;

      const selectedGroupId = String(selectedLevelsAggGroupId || '').trim();
      const selectedEdgeType = normalizeEdgeType(selectedLevelsAggEdgeType ?? undefined);
      const selectedAggKey = String(selectedLevelsAggKey || '').trim();
      const selectedAggKeyIsGpair = !!selectedAggKey && selectedAggKey.startsWith('gpair:');
      const selectedGroupNode = selectedGroupId ? nodes.find((n) => String(n.id || '').trim() === selectedGroupId) : undefined;
      const selectedGroupData: any = selectedGroupNode?.data;
      const selectedGroupParentId = String(selectedGroupData?.groupParentId || '').trim();
      const selectedGroupIsLevelsLeft =
        !!selectedGroupId &&
        !!selectedGroupData?.isGroup &&
        selectedGroupId.startsWith('levels:left:') &&
        !!selectedGroupParentId;
      const selectedGroupChildIds = selectedGroupIsLevelsLeft
        ? new Set(
            nodes
              .filter(
                (n) =>
                  (((n as any).parentId === selectedGroupId || (n as any).parentNode === selectedGroupId) &&
                    !(n as any).hidden &&
                    !((n as any)?.data as any)?.isGroup),
              )
              .map((n) => String(n.id || '').trim())
              .filter(Boolean),
          )
        : null;
      const isSelectedGroupParentChildEdge = (e: any) => {
        if (!selectedGroupIsLevelsLeft || !selectedGroupChildIds || selectedGroupChildIds.size === 0) return false;
        const s = String(e?.source || '').trim();
        const t = String(e?.target || '').trim();
        if (!s || !t) return false;
        if (selectedEdgeType) {
          const et = normalizeEdgeType(e?.data?.edgeType || e?.data?.subkind || e?.label || e?.type);
          if (et !== selectedEdgeType) return false;
        }
        return (
          (s === selectedGroupParentId && selectedGroupChildIds.has(t)) ||
          (t === selectedGroupParentId && selectedGroupChildIds.has(s))
        );
      };
      // Suppress only the aggregated edges corresponding to the selected group's parent<->child buckets.
      // (Do NOT suppress group-proxy agg edges like `${groupId}::node:...`.)
      const selectedAggKeysToSuppress =
        selectedGroupIsLevelsLeft && selectedGroupChildIds && selectedGroupChildIds.size > 0
          ? (() => {
              const set = new Set<string>();
              if (selectedEdgeType) {
                set.add(`${selectedGroupId}::edge:${selectedEdgeType}`);
                return set;
              }
              // Group node click (no explicit edgeType): suppress all parent<->child agg edges for this group.
              for (const e of baseVisibleEdges) {
                const d: any = (e as any)?.data || {};
                if (!d.levelsAgg) continue;
                const k = String(d.levelsAggKey || '').trim();
                if (!k) continue;
                if (k.startsWith(`${selectedGroupId}::edge:`)) set.add(k);
              }
              return set.size > 0 ? set : null;
            })()
          : null;
      const combinedAggKeysToSuppress =
        aggKeysToSuppress && selectedAggKeysToSuppress
          ? (() => {
              const next = new Set<string>(aggKeysToSuppress);
              for (const k of selectedAggKeysToSuppress) next.add(k);
              return next;
            })()
          : (aggKeysToSuppress || selectedAggKeysToSuppress);

      // If preview is keyed off an explicit aggKey (e.g. group↔group proxy), suppress that agg edge too.
      const combinedAggKeysToSuppress2 =
        selectedAggKey
          ? (() => {
              const next = new Set<string>(combinedAggKeysToSuppress || []);
              next.add(selectedAggKey);
              return next;
            })()
          : combinedAggKeysToSuppress;

      // For group↔group previews, pick a single strongest underlying edge to avoid spaghetti.
      // We define "strength" as max(criticality, amplification) with deterministic tie-break.
      const strongestEdgeIdForSelectedAggKey =
        selectedAggKeyIsGpair && selectedEdgeType
          ? (() => {
              let bestId: string | null = null;
              let bestScore = -Infinity;
              for (const e of baseVisibleEdges as any[]) {
                const d: any = e?.data || {};
                if (!d.levelsHiddenByAggregation) continue;
                if (d.levelsNoHoverReveal) continue;
                const k = String(d.levelsAggKey || '').trim();
                if (k !== selectedAggKey) continue;
                const et = normalizeEdgeType(d.edgeType || d.subkind || e?.label || e?.type);
                if (et !== selectedEdgeType) continue;
                const cid = String(e?.id || '').trim();
                if (!cid) continue;
                const crit = Number.parseFloat(String(d.criticality ?? '0'));
                const amp = Number.parseFloat(String(d.amplification ?? '0'));
                const critScore = Number.isFinite(crit) ? crit : 0;
                const ampScore = Number.isFinite(amp) ? amp : 0;
                // Prefer criticality, then amplification, then stable id tie-break.
                const score = critScore * 1000 + ampScore * 10;
                if (score > bestScore) {
                  bestScore = score;
                  bestId = cid;
                } else if (score === bestScore && bestId && cid < bestId) {
                  bestId = cid;
                }
              }
              return bestId;
            })()
          : null;
      edgesForDisplay = baseVisibleEdges.map((e) => {
        const d: any = (e as any)?.data || {};
        if (d.levelsHiddenByAggregation) {
          if (d.levelsNoHoverReveal) return { ...(e as any), hidden: true } as any;
          const isIncident = hovered && (e.source === hovered || e.target === hovered);
          const isSelectedGroupPC = selectedGroupIsLevelsLeft && isSelectedGroupParentChildEdge(e);
          const edgeAggKey = String(d.levelsAggKey || '').trim();
          const isSelectedAggKeyMatch =
            selectedAggKey && edgeAggKey === selectedAggKey &&
            (!selectedEdgeType || normalizeEdgeType(d.edgeType || d.subkind || (e as any)?.label || (e as any)?.type) === selectedEdgeType);
          if (selectedAggKeyIsGpair && isSelectedAggKeyMatch && strongestEdgeIdForSelectedAggKey) {
            const eid = String((e as any)?.id || '').trim();
            if (eid !== strongestEdgeIdForSelectedAggKey) return { ...(e as any), hidden: true } as any;
          }
          if (!isIncident && !isSelectedGroupPC && !isSelectedAggKeyMatch) return { ...(e as any), hidden: true } as any;
          return { ...(e as any), hidden: false } as any;
        }
        if (combinedAggKeysToSuppress2 && d.levelsAgg) {
          const k = String(d.levelsAggKey || '').trim();
          if (k && combinedAggKeysToSuppress2.has(k)) return { ...(e as any), hidden: true } as any;
        }
        return e;
      });
    }

    const baseStyled = (e: Edge) => {
      const d: any = (e as any)?.data || {};
      const rawBaseStyle = d.baseStyle || e.style || {};
      const stressScore = typeof d.pathStressScore === 'number' ? d.pathStressScore : undefined;
      const bindingConfidence = typeof d.pathBindingConfidence === 'number' ? d.pathBindingConfidence : undefined;
      const pathBottleneck = Boolean(d.pathBottleneck);
      let baseStyle: any = { ...rawBaseStyle };
      if (typeof stressScore === 'number' && Number.isFinite(stressScore) && stressScore > 0) {
        const stroke =
          stressScore >= 0.78
            ? '#ef4444'
            : stressScore >= 0.58
              ? '#f59e0b'
              : stressScore >= 0.36
                ? '#38bdf8'
                : rawBaseStyle.stroke;
        const currentWidth = typeof rawBaseStyle.strokeWidth === 'number' ? rawBaseStyle.strokeWidth : 1.5;
        baseStyle = {
          ...baseStyle,
          stroke: stroke || rawBaseStyle.stroke,
          strokeWidth: currentWidth + (pathBottleneck ? 1.8 : 0.5 + stressScore * 1.5),
          opacity: Math.max(typeof rawBaseStyle.opacity === 'number' ? rawBaseStyle.opacity : 0.42, 0.48 + stressScore * 0.34),
          strokeOpacity:
            Math.max(typeof rawBaseStyle.strokeOpacity === 'number' ? rawBaseStyle.strokeOpacity : 0.42, 0.56 + stressScore * 0.32),
          ...(bindingConfidence !== undefined && bindingConfidence < 0.42
            ? { strokeDasharray: '4 3' }
            : {}),
          ...(pathBottleneck ? { filter: 'drop-shadow(0 0 5px rgba(245,158,11,0.38))' } : {}),
        };
      }
      const edgeTypeRaw = (e as any)?.data?.edgeType || (e as any)?.data?.subkind || e.label;
      const edgeLabel =
        d.edgeCaption ||
        getEdgeCaptionShort(edgeTypeRaw) ||
        (edgeTypeRaw ? String(edgeTypeRaw) : '');
      return { baseStyle, edgeLabel };
    };

    // Helper to create edge with optional label
    const withLabel = (e: Edge, showLabel: boolean, style: any, raiseToFront: boolean) => {
      const d: any = (e as any)?.data || {};
      const markerColor = typeof style?.stroke === 'string' && style.stroke.trim()
        ? style.stroke.trim()
        : undefined;
      const markerEnd =
        markerColor && (e as any)?.markerEnd
          ? { ...((e as any).markerEnd || {}), color: markerColor }
          : (e as any)?.markerEnd;
      if (!showLabel) {
        return {
          ...e,
          label: undefined,
          markerEnd,
          data: { ...d, showLabel: false, raiseToFront: false },
          style,
        };
      }
      const { edgeLabel } = baseStyled(e);
      return {
        ...e,
        label: undefined,
        markerEnd,
        data: { ...d, showLabel, edgeLabel, raiseToFront },
        style,
      };
    };

    // Single-pass partition: split edges into [rest, raised] and concatenate.
    // Avoids 2× filter() calls which iterate the array twice.
    const orderByRaised = (edges: Edge[]): Edge[] => {
      const rest: Edge[] = [];
      const raised: Edge[] = [];
      for (let i = 0; i < edges.length; i++) {
        const e = edges[i]!;
        if ((e as any)?.data?.raiseToFront) {
          raised.push(e);
        } else {
          rest.push(e);
        }
      }
      // Only create new array if there are raised edges
      return raised.length > 0 ? [...rest, ...raised] : edges;
    };

    if (signalModeActive && signalPropagationOverlay) {
      const signalEdgeSet = signalPropagationEdgeIdSet;
      const signalEdgeActive = !!signalEdgeSet && signalEdgeSet.size > 0;
      const signalStroke = theme === 'dark' ? 'rgba(248,113,113,0.96)' : 'rgba(220,38,38,0.94)';
      const signalGlow = theme === 'dark' ? 'drop-shadow(0 0 8px rgba(248,113,113,0.72))' : 'drop-shadow(0 0 6px rgba(239,68,68,0.56))';

      const out = edgesForDisplay.map((e) => {
        if ((e as any)?.hidden) return e;
        const { baseStyle } = baseStyled(e);
        const edgeId = String((e as any)?.id || '').trim();
        const isSignalEdge = signalEdgeActive && !!edgeId && !!signalEdgeSet && signalEdgeSet.has(edgeId);
        if (isSignalEdge) {
          const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
          const highlighted: any = withLabel(
            e,
            true,
            {
              ...baseStyle,
              opacity: 1,
              stroke: signalStroke,
              strokeOpacity: 1,
              strokeWidth: Math.max(2.8, strokeWidth + 1.2),
              filter: signalGlow,
            },
            true,
          );
          highlighted.markerEnd = { type: MarkerType.ArrowClosed, color: signalStroke };
          return highlighted;
        }
        return withLabel(
          e,
          false,
          {
            ...baseStyle,
            opacity: signalEdgeActive ? 0.06 : 0.12,
            strokeOpacity: signalEdgeActive ? 0.06 : (baseStyle as any)?.strokeOpacity,
          },
          false,
        );
      });
      return applyFragmentProfileDimming(orderByRaised(out));
    }

    // Persisted selection highlight (keeps an edge emphasized until the user clicks elsewhere).
    // Hover still works normally; we only skip this overlay in "edge hover" mode (hoveredEdgeId),
    // because that path intentionally fades everything except the hovered edge.
    const applySelectedEdge = (list: Edge[]) => {
      const sel = String(selectedEdgeId || '').trim();
      if (!sel) return list;
      const out = list.map((e) => {
        if (String(e.id || '').trim() !== sel) return e;
        const { baseStyle } = baseStyled(e);
        const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
        return withLabel(
          e,
          false,
          {
            ...baseStyle,
            opacity: 1,
            strokeWidth: strokeWidth + 1.8,
          },
          true,
        );
      });
      return orderByRaised(out);
    };

    // Levels Two-Pane: persisted "real edges preview" from clicking an aggregated edge / group.
    // This should take precedence over hover modes (edge hover, node hover, group hover) so it doesn't look "in the background".
    if (isLevelsLayout || isSupplyChainLayout) {
      const selectedGroupId = String(selectedLevelsAggGroupId || '').trim();
      const selectedEdgeType = normalizeEdgeType(selectedLevelsAggEdgeType ?? undefined);
      const selectedAggKey = String(selectedLevelsAggKey || '').trim();
      if (selectedGroupId) {
        const groupNode = nodes.find((n) => String(n.id || '').trim() === selectedGroupId);
        const gd: any = groupNode?.data;
        const parentId = String(gd?.groupParentId || '').trim();
        const isLevelsLeftGroup = !!gd?.isGroup && selectedGroupId.startsWith('levels:left:') && !!parentId;
        if (isLevelsLeftGroup) {
          const childIds = new Set(
            nodes
              .filter(
                (n) =>
                  (((n as any).parentId === selectedGroupId || (n as any).parentNode === selectedGroupId) &&
                    !(n as any).hidden &&
                    !((n as any)?.data as any)?.isGroup),
              )
              .map((n) => String(n.id || '').trim())
              .filter(Boolean),
          );
          if (childIds.size > 0) {
            const isPC = (e: any) => {
              const s = String(e?.source || '').trim();
              const t = String(e?.target || '').trim();
              if (!s || !t) return false;
              const okPair =
                (s === parentId && childIds.has(t)) ||
                (t === parentId && childIds.has(s));
              if (!okPair) return false;
              if (selectedEdgeType) {
                const et = normalizeEdgeType(e?.data?.edgeType || e?.data?.subkind || e?.label || e?.type);
                if (et !== selectedEdgeType) return false;
              }
              return true;
            };
            const out = edgesForDisplay.map((e) => {
              const { baseStyle } = baseStyled(e);
              if (isPC(e)) {
                const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
                return withLabel(
                  e,
                  true,
                  {
                    ...baseStyle,
                    opacity: 1,
                    strokeWidth: strokeWidth + 1.9,
                  },
                  true,
                );
              }
              return withLabel(e, false, { ...baseStyle, opacity: FADE_OPACITY }, false);
            });
            return applyFragmentProfileDimming(applySelectedEdge(orderByRaised(out)));
          }
        }
      }

      // Group↔Group preview: reveal only one strongest underlying edge for this aggKey (avoid clutter).
      if (!selectedGroupId && selectedAggKey && selectedAggKey.startsWith('gpair:') && selectedEdgeType) {
        // Find the strongest visible hidden-by-aggregation edge for this aggKey/type.
        let bestId: string | null = null;
        let bestScore = -Infinity;
        const scoreOf = (e: any): number => {
          const d: any = e?.data || {};
          const crit = Number.parseFloat(String(d.criticality ?? '0'));
          const amp = Number.parseFloat(String(d.amplification ?? '0'));
          const critScore = Number.isFinite(crit) ? crit : 0;
          const ampScore = Number.isFinite(amp) ? amp : 0;
          return critScore * 1000 + ampScore * 10;
        };
        for (const e of edgesForDisplay as any[]) {
          const d: any = e?.data || {};
          if (!d.levelsHiddenByAggregation) continue;
          if (d.levelsNoHoverReveal) continue;
          const k = String(d.levelsAggKey || '').trim();
          if (k !== selectedAggKey) continue;
          const et = normalizeEdgeType(d.edgeType || d.subkind || e?.label || e?.type);
          if (et !== selectedEdgeType) continue;
          const id = String(e?.id || '').trim();
          if (!id) continue;
          const s = scoreOf(e);
          if (s > bestScore) {
            bestScore = s;
            bestId = id;
          } else if (s === bestScore && bestId && id < bestId) {
            bestId = id;
          }
        }
        if (bestId) {
          const out = edgesForDisplay.map((e) => {
            const { baseStyle } = baseStyled(e);
            const isBest = String((e as any)?.id || '').trim() === bestId;
            if (isBest) {
              const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
              return withLabel(
                e,
                true,
                { ...baseStyle, opacity: 1, strokeWidth: strokeWidth + 2.0 },
                true,
              );
            }
            return withLabel(e, false, { ...baseStyle, opacity: FADE_OPACITY }, false);
          });
          return applyFragmentProfileDimming(applySelectedEdge(orderByRaised(out)));
        }
      }
    }

    // Edge hover: show only this edge's label
    if (hoveredEdgeId) {
      const out = edgesForDisplay.map((e) => {
        const { baseStyle } = baseStyled(e);
        const isHovered = e.id === hoveredEdgeId;
        if (isHovered) {
          const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
          return withLabel(e, true, {
            ...baseStyle,
            opacity: 1,
            strokeWidth: strokeWidth + 1.5,
          }, true);
        }
        return withLabel(e, false, { ...baseStyle, opacity: FADE_OPACITY }, false);
      });
      // Keep hovered edge on top within edge layer.
      return orderByRaised(out);
    }

    // Group hover has priority over node hover
    if (hoveredGroupId) {
      const groupNode = nodes.find((n) => n.id === hoveredGroupId);
      const gd: any = groupNode?.data;
      if (!groupNode || !gd?.isGroup) {
        return applyFragmentProfileDimming(applySelectedEdge(edgesForDisplay.map((e) => {
          const { baseStyle } = baseStyled(e);
          return withLabel(e, false, { ...baseStyle, opacity: DIM_OPACITY }, false);
        })));
      }

      const groupEdgeType = normalizeEdgeType(gd.groupEdgeType);
      const groupParentId = String(gd.groupParentId || '').trim();
      const groupDirection: 'incoming' | 'outgoing' | 'both' =
        gd.groupDirection === 'incoming' ? 'incoming' : gd.groupDirection === 'both' ? 'both' : 'outgoing';

      // Only consider visible (non-hidden) children for edge highlighting
      const childIds = new Set(
        nodes
          .filter(
            (n) =>
              ((n as any).parentId === hoveredGroupId || (n as any).parentNode === hoveredGroupId) &&
              !(n as any).hidden,
          )
          .map((n) => n.id),
      );
      if (!groupParentId || childIds.size === 0) {
        return applyFragmentProfileDimming(applySelectedEdge(edgesForDisplay.map((e) => {
          const { baseStyle } = baseStyled(e);
          return withLabel(e, false, { ...baseStyle, opacity: DIM_OPACITY }, false);
        })));
      }

      const isGroupEdge = (e: any) => {
        const et = normalizeEdgeType(e?.data?.edgeType || e?.label);
        // For LOCATED_IN group, also match OPERATES_IN edges
        if (groupEdgeType === 'LOCATED_IN') {
          if (et !== 'LOCATED_IN' && et !== 'OPERATES_IN') return false;
        } else if (groupEdgeType && et !== groupEdgeType) {
          return false;
        }
        if (groupDirection === 'both') {
          return (
            (e.source === groupParentId && childIds.has(e.target)) ||
            (e.target === groupParentId && childIds.has(e.source))
          );
        }
        if (groupDirection === 'outgoing') {
          return e.source === groupParentId && childIds.has(e.target);
        }
        return e.target === groupParentId && childIds.has(e.source);
      };

      const out = edgesForDisplay.map((e) => {
        const { baseStyle } = baseStyled(e);
        if (isGroupEdge(e)) {
          const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
          return withLabel(e, true, {
            ...baseStyle,
            opacity: 1,
            strokeWidth: strokeWidth + 1.5,
          }, true);
        }
        return withLabel(e, false, { ...baseStyle, opacity: FADE_OPACITY }, false);
      });
      return applyFragmentProfileDimming(applySelectedEdge(orderByRaised(out)));
    }

    // Hover highlight mode: highlight selected relation cluster and fade everything else.
    if (hoverHighlight && hoveredNodeId) {
      const hiEdgeIds = hoverHighlight.edgeIds;
      const CONTROL_EQUITY_TYPES = new Set<string>(['OWNS', 'PARENT_OF', 'SUBSIDIARY_OF', 'INVESTS_IN']);
      const CONTROL_NON_EQUITY_TYPES = new Set<string>(['CONTROLS', 'MANAGES']);
      const CONTROL_GOVERNANCE_TYPES = new Set<string>(['BOARD_MEMBER_OF', 'EXECUTIVE_OF', 'EMPLOYED_BY', 'FOUNDED']);
      const isControlLike = hoverHighlightMode === 'control' || hoverHighlightMode === 'expansion';
      const out = edgesForDisplay.map((e) => {
        const { baseStyle } = baseStyled(e);
        const eid = String((e as any)?.id || '').trim();
        // If we are rendering an off-screen preview for this edge, hide the real edge segment
        // (otherwise it still points to the true off-screen node and visually contradicts the preview).
        if (eid && offScreenPreviewEdgeIds.has(eid)) {
          return withLabel(e, false, { ...baseStyle, opacity: 0 }, false);
        }
        const isHiEdge = eid && hiEdgeIds.has(eid);
        // Modern UX: show captions and lift to front only for edges incident to the hovered node.
        const isIncident = e.source === hoveredNodeId || e.target === hoveredNodeId;
        const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
        if (isHiEdge) {
          const edgeTypeRaw = (e as any)?.data?.edgeType || (e as any)?.data?.subkind || e.label || (e as any)?.type || '';
          const u = String(edgeTypeRaw || '').toUpperCase().trim();
          const isGovernance = isControlLike && CONTROL_GOVERNANCE_TYPES.has(u);
          const isNonEquityChain = isControlLike && CONTROL_NON_EQUITY_TYPES.has(u);
          const isEquityChain = isControlLike && CONTROL_EQUITY_TYPES.has(u);
          const dash = isGovernance ? '2 3' : (isNonEquityChain ? '6 4' : undefined);
          return withLabel(e, isIncident, {
            ...baseStyle,
            opacity: 1,
            strokeWidth: isGovernance
              ? Math.max(2.4, strokeWidth + 0.5)
              : isNonEquityChain
                ? Math.max(2.8, strokeWidth + 0.7)
                : isEquityChain
                  ? Math.max(3.2, strokeWidth + 1.1)
                  : Math.max(3, strokeWidth + 1.0),
            ...(dash ? { strokeDasharray: dash } : {}),
          }, isIncident);
        }
        return withLabel(e, false, { ...baseStyle, opacity: FADE_OPACITY }, false);
      });
      return applyFragmentProfileDimming(applySelectedEdge(orderByRaised(out)));
    }

    // Node hover: highlight incident edges and show their labels
    if (hoveredNodeId) {
      const nodeId = hoveredNodeId;
      const out = edgesForDisplay.map((e) => {
        const { baseStyle } = baseStyled(e);
        const isIncident = e.source === nodeId || e.target === nodeId;
        if (isIncident) {
          const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
          return withLabel(e, true, {
            ...baseStyle,
            opacity: 1,
            strokeWidth: strokeWidth + 1.0,
          }, true);
        }
        return withLabel(e, false, { ...baseStyle, opacity: FADE_OPACITY }, false);
      });
      return applyFragmentProfileDimming(applySelectedEdge(orderByRaised(out)));
    }

    // Selected node (when not hovering): highlight incident edges, show labels
    // Focus mode: when enabled, dim non-incident edges much more (0.06) to reduce spaghetti
    if (selectedNodeId) {
      const nodeId = selectedNodeId;
      const nonIncidentOpacity = focusMode ? 0.06 : DIM_OPACITY;
      const out = edgesForDisplay.map((e) => {
        const { baseStyle } = baseStyled(e);
        const isIncident = e.source === nodeId || e.target === nodeId;
        if (isIncident) {
          const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
          return withLabel(e, true, {
            ...baseStyle,
            opacity: 1,
            strokeWidth: strokeWidth + 0.75,
          }, true);
        }
        return withLabel(e, false, { ...baseStyle, opacity: nonIncidentOpacity }, false);
      });
      return applyFragmentProfileDimming(applySelectedEdge(orderByRaised(out)));
    }

    // QA edge highlight: accent stroke + increased width for trace-path edges
    const qaEdgeSet = qaHighlightEdgeIdSet;
    const qaEdgeActive = !!qaEdgeSet && qaEdgeSet.size > 0;
    if (qaEdgeActive) {
      const accentStroke = '#3B82F6';
      const out = edgesForDisplay.map((e) => {
        const { baseStyle } = baseStyled(e);
        const eid = String((e as any)?.id || '').trim();
        const isQaEdge = !!eid && !!qaEdgeSet && qaEdgeSet.has(eid);
        if (isQaEdge) {
          const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
          return withLabel(e, true, {
            ...baseStyle,
            opacity: 1,
            stroke: accentStroke,
            strokeWidth: Math.max(3, strokeWidth + 1.5),
            filter: 'drop-shadow(0 0 4px rgba(59,130,246,0.5))',
          }, true);
        }
        return withLabel(e, false, { ...baseStyle, opacity: DIM_OPACITY }, false);
      });
      return applyFragmentProfileDimming(applySelectedEdge(orderByRaised(out)));
    }

    const domainHoverEdgeSet = domainOverlayHoverEdgeIdSet;
    const domainHoverNodeSet = domainOverlayHoverNodeIdSet;
    const domainHoverEdgeActive = !!domainHoverEdgeSet && domainHoverEdgeSet.size > 0;
    const domainHoverNodeActive = !!domainHoverNodeSet && domainHoverNodeSet.size > 0;
    if (domainHoverEdgeActive || domainHoverNodeActive) {
      const out = edgesForDisplay.map((e) => {
        const { baseStyle } = baseStyled(e);
        const eid = String((e as any)?.id || '').trim();
        const src = String((e as any)?.source || '').trim();
        const dst = String((e as any)?.target || '').trim();
        const byEdgeId = !!domainHoverEdgeSet && !!eid && domainHoverEdgeSet.has(eid);
        const byNodePair = !!domainHoverNodeSet && !!src && !!dst && domainHoverNodeSet.has(src) && domainHoverNodeSet.has(dst);
        const isHi = byEdgeId || byNodePair;
        if (isHi) {
          const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
          return withLabel(e, true, {
            ...baseStyle,
            opacity: 1,
            strokeWidth: strokeWidth + 1.4,
          }, true);
        }
        return withLabel(e, false, { ...baseStyle, opacity: FADE_OPACITY }, false);
      });
      return applyFragmentProfileDimming(applySelectedEdge(orderByRaised(out)));
    }

    const domainEdgeSet = domainOverlayHighlightEdgeIds;
    const domainNodeSet = domainOverlayHighlightNodeIds;
    const domainEdgeActive = !!domainEdgeSet && domainEdgeSet.size > 0;
    const domainNodeActive = !!domainNodeSet && domainNodeSet.size > 0;
    if (domainEdgeActive || domainNodeActive) {
      const out = edgesForDisplay.map((e) => {
        const { baseStyle } = baseStyled(e);
        const eid = String((e as any)?.id || '').trim();
        const src = String((e as any)?.source || '').trim();
        const dst = String((e as any)?.target || '').trim();
        const byEdgeId = !!domainEdgeSet && !!eid && domainEdgeSet.has(eid);
        const byNodePair = !!domainNodeSet && !!src && !!dst && domainNodeSet.has(src) && domainNodeSet.has(dst);
        const isHi = byEdgeId || byNodePair;
        if (isHi) {
          const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
          return withLabel(e, true, {
            ...baseStyle,
            opacity: 1,
            strokeWidth: strokeWidth + 1.0,
          }, true);
        }
        return withLabel(e, false, { ...baseStyle, opacity: DIM_OPACITY }, false);
      });
      return applyFragmentProfileDimming(applySelectedEdge(orderByRaised(out)));
    }

    // Default: dim all edges, hide labels
    return applyFragmentProfileDimming(applySelectedEdge(edgesForDisplay));
  }, [baseVisibleEdges, hoveredEdgeId, hoveredGroupId, hoveredNodeId, nodes, normalizeEdgeType, selectedNodeId, perfMode, hoverHighlight, hoverHighlightMode, signalPropagationOverlay, signalPropagationEdgeIdSet, theme, getEdgeCaptionShort, isLevelsLayout, levelsAggKeysByNodeId, selectedEdgeId, selectedLevelsAggGroupId, selectedLevelsAggEdgeType, fragmentProfileActiveNodeId, domainOverlayHoverEdgeIdSet, domainOverlayHoverNodeIdSet, domainOverlayHighlightEdgeIds, domainOverlayHighlightNodeIds, controlCorridorMode, focusMode, qaHighlightEdgeIdSet]);

  // Right-side temporary stacking toggle (Levels layout):
  // The layout will mark the controlling node with stackGroupKey/stackCount/stackExpanded.
  // Toggling updates that flag and triggers a relayout so the stack can expand/collapse.
  const toggleStackGroup = React.useCallback((stackKey: string, nextExpanded: boolean) => {
    const key = String(stackKey || '').trim();
    if (!key) return;
    setNodes((prev) => {
      let updated = 0;
      const next = prev.map((n) => {
        const d: any = (n as any)?.data;
        if (!d || d.isGroup) return n;
        const k = String(d.stackGroupKey || d.stackKey || '').trim();
        if (!k) return n;
        const cur = Boolean(d.stackExpanded);
        if (k === key) {
          if (cur === nextExpanded) return n;
          updated++;
          return { ...(n as any), data: { ...d, stackExpanded: nextExpanded } } as any;
        }
        return n;
      });
      if (nextExpanded) expandedStackKeysRef.current.add(key);
      else expandedStackKeysRef.current.delete(key);
      return next;
    });
    if (isLevelsLayout) {
      invokeArrange();
    }
  }, [setNodes, layoutMode, invokeArrange]);

  // Right-side node filters (Levels layout).
  // These mutate `node.hidden` for selected neighbors and trigger relayout.
  const openRightFilters = React.useCallback((nodeId: string, nextOpen: boolean) => {
    const id = String(nodeId || '').trim();
    if (!id) return;
    setNodes((prev) =>
      prev.map((n) => {
        const d: any = (n as any)?.data;
        if (!d || d.isGroup) return n;
        const isRight = String(d.levelsPane || '') === 'right';
        const isOpen = Boolean(d.rightFiltersOpen);
        if (!isRight) {
          if (!isOpen) return n;
          const nd = { ...d };
          delete nd.rightFiltersOpen;
          return { ...(n as any), data: nd } as any;
        }
        if (String(n.id || '').trim() === id) {
          return { ...(n as any), data: { ...d, rightFiltersOpen: nextOpen } } as any;
        }
        if (nextOpen && isOpen) {
          const nd = { ...d };
          delete nd.rightFiltersOpen;
          return { ...(n as any), data: nd } as any;
        }
        return n;
      }),
    );
  }, [setNodes]);

  const toggleRightFilter = React.useCallback((
    nodeId: string,
    key: 'loneParents' | 'loneChildren' | 'loneGovernancePeople',
    nextOn: boolean,
  ) => {
    const centerId = String(nodeId || '').trim();
    if (!centerId) return;

    const snapNodes: Node<HorizonNodeData>[] = (nodesStateRef.current || nodes) as any;
    const snapEdges: Edge[] = (edgesStateRef.current || edges) as any;

    const isGroup = (n: any) => Boolean((n as any)?.data?.isGroup);
    const nodeById = new Map<string, Node<HorizonNodeData>>();
    const typeById = new Map<string, string>();
    const xById = new Map<string, number>();
    for (const n of snapNodes) {
      const id = String((n as any)?.id || '').trim();
      if (!id) continue;
      nodeById.set(id, n as any);
      if (!isGroup(n)) {
        const t = String((n as any)?.data?.type || '').toUpperCase().trim();
        if (t) typeById.set(id, t);
        const x = typeof (n as any)?.position?.x === 'number' ? (n as any).position.x : undefined;
        if (typeof x === 'number' && Number.isFinite(x)) xById.set(id, x);
      }
    }
    const center = nodeById.get(centerId);
    if (!center || isGroup(center)) return;
    const centerX = xById.get(centerId);
    const EPS_X = 12;

    const isRight = (id: string) => String((nodeById.get(id) as any)?.data?.levelsPane || '') === 'right';
    const isPerson = (id: string) => (typeById.get(id) || '') === 'PERSON';

    const allFragIds = new Set<string>(Array.from(nodeById.keys()).filter((id) => !isGroup(nodeById.get(id))));
    const neighbors = new Map<string, Set<string>>();
    const pairKey = (a: string, b: string) => (a < b ? `${a}||${b}` : `${b}||${a}`);
    const pairTypes = new Map<string, Set<string>>();
    const addNeighbor = (a: string, b: string) => {
      if (!neighbors.has(a)) neighbors.set(a, new Set());
      neighbors.get(a)!.add(b);
    };

    const GOVERNANCE_TYPES = new Set(['BOARD_MEMBER_OF', 'EXECUTIVE_OF', 'EMPLOYED_BY']);
    const HIER = new Set(['OWNS', 'PARENT_OF', 'MANAGES', 'CONTROLS', 'INVESTS_IN', 'BOARD_MEMBER_OF', 'EXECUTIVE_OF']);
    const INV = new Set(['SUBSIDIARY_OF', 'DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM']);

    const edgeTypeOf = (e: any) => normalizeEdgeType(e?.data?.edgeType || e?.data?.subkind || e?.label || e?.type);
    const parentChildFromEdge = (e: any): { parent: string; child: string; et: string } | null => {
      const s = String(e?.source || '').trim();
      const t = String(e?.target || '').trim();
      if (!s || !t) return null;
      if (!allFragIds.has(s) || !allFragIds.has(t)) return null;
      const et = edgeTypeOf(e);
      if (!et) return null;
      // Governance: if one side is PERSON, treat PERSON as controller.
      if (et === 'BOARD_MEMBER_OF' || et === 'EXECUTIVE_OF') {
        const sP = isPerson(s);
        const tP = isPerson(t);
        if (sP && !tP) return { parent: s, child: t, et };
        if (tP && !sP) return { parent: t, child: s, et };
        // fallback to direction
        return { parent: s, child: t, et };
      }
      if (HIER.has(et)) return { parent: s, child: t, et };
      if (INV.has(et)) return { parent: t, child: s, et };
      return null;
    };

    for (const e of snapEdges as any[]) {
      const s = String(e?.source || '').trim();
      const t = String(e?.target || '').trim();
      if (!s || !t) continue;
      if (!allFragIds.has(s) || !allFragIds.has(t)) continue;
      addNeighbor(s, t);
      addNeighbor(t, s);
      const pk = pairKey(s, t);
      const et = edgeTypeOf(e);
      if (et) {
        if (!pairTypes.has(pk)) pairTypes.set(pk, new Set());
        pairTypes.get(pk)!.add(et);
      }
    }

    const lone = (id: string) => {
      const ns = neighbors.get(id);
      if (!ns || ns.size === 0) return true;
      return ns.size === 1 && ns.has(centerId);
    };

    const loneParentsToHide = new Set<string>();
    const loneChildrenToHide = new Set<string>();
    const loneGovPeopleToHide = new Set<string>();

    const centerEdges: Array<{ other: string; et: string; pc: { parent: string; child: string; et: string } | null }> = [];
    for (const e of snapEdges as any[]) {
      const s = String(e?.source || '').trim();
      const t = String(e?.target || '').trim();
      if (!s || !t) continue;
      if (s !== centerId && t !== centerId) continue;
      const other = s === centerId ? t : s;
      const pc = parentChildFromEdge(e);
      const et = edgeTypeOf(e);
      centerEdges.push({ other, et, pc });
    }

    for (const { other, pc } of centerEdges) {
      if (!other || !allFragIds.has(other)) continue;
      if (!isRight(other)) continue;
      const ox = xById.get(other);
      if (pc && pc.child === centerId) {
        // parent candidate
        if (typeof centerX === 'number' && typeof ox === 'number' && ox < centerX + EPS_X) continue;
        if (lone(other)) loneParentsToHide.add(other);
      }
      if (pc && pc.parent === centerId) {
        // child candidate
        if (typeof centerX === 'number' && typeof ox === 'number' && ox > centerX - EPS_X) continue;
        if (lone(other)) loneChildrenToHide.add(other);
      }

      // governance-only person
      if (isPerson(other) && lone(other)) {
        const pk = pairKey(centerId, other);
        const types = pairTypes.get(pk) || new Set<string>();
        const onlyGov = Array.from(types).every((t) => GOVERNANCE_TYPES.has(t));
        if (onlyGov) loneGovPeopleToHide.add(other);
      }
    }

    const shouldHideParents = key === 'loneParents' ? nextOn : Boolean((center.data as any)?.rightFilterHideLoneParents);
    const shouldHideChildren = key === 'loneChildren' ? nextOn : Boolean((center.data as any)?.rightFilterHideLoneChildren);
    const shouldHideGov = key === 'loneGovernancePeople' ? nextOn : Boolean((center.data as any)?.rightFilterHideLoneGovernancePeople);

    setNodes((prev) => {
      return prev.map((n) => {
        const id = String((n as any)?.id || '').trim();
        const d0: any = (n as any)?.data;
        if (!id || !d0 || d0.isGroup) return n;

        let d = d0;
        let changed = false;

        const ensureArr = (k: string) => {
          const v = d[k];
          if (Array.isArray(v)) return v as string[];
          return [];
        };
        const setArr = (k: string, arr: string[]) => {
          if (d === d0) d = { ...d0 };
          d[k] = arr;
          changed = true;
        };
        const toggleReason = (k: string, shouldHide: boolean, inSet: boolean) => {
          const arr0 = ensureArr(k);
          const has = arr0.includes(centerId);
          const want = shouldHide && inSet;
          if (want && !has) {
            setArr(k, [...arr0, centerId]);
          } else if (!want && has) {
            setArr(k, arr0.filter((x) => x !== centerId));
          } else if (shouldHide && !inSet && has) {
            // still on, but no longer eligible -> remove
            setArr(k, arr0.filter((x) => x !== centerId));
          }
        };

        if (id === centerId) {
          if (d === d0) d = { ...d0 };
          if (key === 'loneParents') d.rightFilterHideLoneParents = nextOn;
          if (key === 'loneChildren') d.rightFilterHideLoneChildren = nextOn;
          if (key === 'loneGovernancePeople') d.rightFilterHideLoneGovernancePeople = nextOn;
          changed = true;
        } else {
          toggleReason('hiddenByLoneParentsOf', shouldHideParents, loneParentsToHide.has(id));
          toggleReason('hiddenByLoneChildrenOf', shouldHideChildren, loneChildrenToHide.has(id));
          toggleReason('hiddenByLoneGovernanceOf', shouldHideGov, loneGovPeopleToHide.has(id));
        }

        if (!changed) return n;

        const reasonsAfter =
          (Array.isArray(d.hiddenByLoneParentsOf) && d.hiddenByLoneParentsOf.length > 0) ||
          (Array.isArray(d.hiddenByLoneChildrenOf) && d.hiddenByLoneChildrenOf.length > 0) ||
          (Array.isArray(d.hiddenByLoneGovernanceOf) && d.hiddenByLoneGovernanceOf.length > 0) ||
          (Array.isArray(d.hiddenByLocalFilter) && d.hiddenByLocalFilter.length > 0);
        const reasonsBefore =
          (Array.isArray(d0.hiddenByLoneParentsOf) && d0.hiddenByLoneParentsOf.length > 0) ||
          (Array.isArray(d0.hiddenByLoneChildrenOf) && d0.hiddenByLoneChildrenOf.length > 0) ||
          (Array.isArray(d0.hiddenByLoneGovernanceOf) && d0.hiddenByLoneGovernanceOf.length > 0) ||
          (Array.isArray(d0.hiddenByLocalFilter) && d0.hiddenByLocalFilter.length > 0);
        // Preserve truly manual hidden state, but do NOT "stick" hidden after filter toggles.
        const legacyHidden = Boolean((n as any).hidden) && !d.hiddenByStack && !reasonsAfter && !reasonsBefore;
        const hidden = legacyHidden || Boolean(d.hiddenByStack) || Boolean(reasonsAfter);
        return { ...(n as any), hidden, data: d } as any;
      });
    });

    if (isLevelsLayout) {
      invokeArrange();
    }
  }, [nodes, edges, setNodes, layoutMode, invokeArrange, normalizeEdgeType]);

  // Local relation filters (Levels/Supply Chain layout): per-node toggles to show/hide neighbors by edge type.
  // When a node is clicked, we show icons for each edge type in expandEdgeTypesForLayout.
  // Clicking an icon toggles visibility of neighbors connected by that edge type for that specific node.
  const getDependentEdgeTypesForNode = React.useCallback((centerNodeId: string): string[] => {
    const centerId = String(centerNodeId || '').trim();
    if (!centerId) return [];
    const snapNodes: Node<HorizonNodeData>[] = (nodesStateRef.current || nodes) as any;
    const snapEdges: Edge[] = (edgesStateRef.current || edges) as any;
    const isGroup = (n: any) => Boolean((n as any)?.data?.isGroup);
    const allExpand = new Set((expandEdgeTypesForLayout || []).map((x) => String(x || '').toUpperCase().trim()).filter(Boolean));
    if (allExpand.size === 0) return [];

    // Dependents = nodes created/attached from this node expansion (parentFragmentId is node.id).
    const dependentIds = new Set<string>();
    for (const n of snapNodes as any[]) {
      const nid = String(n?.id || '').trim();
      if (!nid) continue;
      if (nid === centerId) continue;
      const d: any = n?.data;
      if (!d || d.isGroup) continue;
      if (String(d.parentFragmentId || '').trim() === centerId) dependentIds.add(nid);
    }
    if (dependentIds.size === 0) return [];

    const seen = new Set<string>();
    const edgeTypeOf = (e: any) => normalizeEdgeType(e?.data?.edgeType || e?.data?.subkind || e?.label || e?.type);
    for (const e of snapEdges as any[]) {
      const s = String(e?.source || '').trim();
      const t = String(e?.target || '').trim();
      if (!s || !t) continue;
      const other = s === centerId ? t : t === centerId ? s : '';
      if (!other || !dependentIds.has(other)) continue;
      const et = edgeTypeOf(e);
      if (!et || !allExpand.has(et)) continue;
      seen.add(et);
    }

    const out = Array.from(seen);
    out.sort((a, b) => {
      const pa = getEdgePriority(a);
      const pb = getEdgePriority(b);
      if (pa !== pb) return pa - pb;
      return a.localeCompare(b);
    });
    return out;
  }, [nodes, edges, expandEdgeTypesForLayout, normalizeEdgeType, getEdgePriority]);

  const openLocalFilters = React.useCallback((nodeId: string, nextOpen: boolean) => {
    const id = String(nodeId || '').trim();
    if (!id) return;
    setNodes((prev) =>
      prev.map((n) => {
        const d: any = (n as any)?.data;
        if (!d || d.isGroup) return n;
        const nid = String(n.id || '').trim();
        if (nid === id) {
          const nd: any = { ...d, localFiltersOpen: nextOpen };
          if (nextOpen) {
            const ets = getDependentEdgeTypesForNode(id);
            nd.localFilterEdgeTypes = ets;
          } else {
            delete nd.localFilterEdgeTypes;
          }
          return { ...(n as any), data: nd } as any;
        }
        // Close any other open local filters
        if (nextOpen && Boolean(d.localFiltersOpen)) {
          const nd = { ...d };
          delete nd.localFiltersOpen;
          delete nd.localFilterEdgeTypes;
          return { ...(n as any), data: nd } as any;
        }
        return n;
      }),
    );
  }, [setNodes, getDependentEdgeTypesForNode]);

  const toggleLocalFilter = React.useCallback((
    nodeId: string,
    edgeType: string,
    nextVisible: boolean,
  ) => {
    const centerId = String(nodeId || '').trim();
    const et = String(edgeType || '').toUpperCase().trim();
    if (!centerId || !et) return;

    const snapNodes: Node<HorizonNodeData>[] = (nodesStateRef.current || nodes) as any;
    const snapEdges: Edge[] = (edgesStateRef.current || edges) as any;

    const isGroup = (n: any) => Boolean((n as any)?.data?.isGroup);
    const nodeById = new Map<string, Node<HorizonNodeData>>();
    for (const n of snapNodes) {
      const id = String((n as any)?.id || '').trim();
      if (id) nodeById.set(id, n as any);
    }
    const center = nodeById.get(centerId);
    if (!center || isGroup(center)) return;

    // Find dependent nodes connected by this edge type (ONLY dependents of this node)
    const edgeTypeOf = (e: any) => normalizeEdgeType(e?.data?.edgeType || e?.data?.subkind || e?.label || e?.type);
    const dependentIds = new Set<string>();
    for (const n of snapNodes as any[]) {
      const nid = String(n?.id || '').trim();
      if (!nid || nid === centerId) continue;
      const d: any = (n as any)?.data;
      if (!d || d.isGroup) continue;
      if (String(d.parentFragmentId || '').trim() === centerId) dependentIds.add(nid);
    }
    const neighborsToToggle = new Set<string>();
    for (const e of snapEdges as any[]) {
      const s = String(e?.source || '').trim();
      const t = String(e?.target || '').trim();
      if (!s || !t) continue;
      const eType = edgeTypeOf(e);
      if (eType !== et) continue;
      if (s === centerId && dependentIds.has(t) && nodeById.has(t) && !isGroup(nodeById.get(t))) {
        neighborsToToggle.add(t);
      } else if (t === centerId && dependentIds.has(s) && nodeById.has(s) && !isGroup(nodeById.get(s))) {
        neighborsToToggle.add(s);
      }
    }

    setNodes((prev) => {
      return prev.map((n) => {
        const id = String((n as any)?.id || '').trim();
        const d0: any = (n as any)?.data;
        if (!id || !d0 || d0.isGroup) return n;

        let d = d0;
        let changed = false;

        // Update the local filter state for the center node
        if (id === centerId) {
          const prevState = (d.localFilterState || {}) as Record<string, boolean>;
          const newState = { ...prevState, [et]: nextVisible };
          d = { ...d0, localFilterState: newState };
          changed = true;
        } else if (neighborsToToggle.has(id)) {
          // Track which center+edgeType combos hide this node
          const reasonKey = 'hiddenByLocalFilter';
          const filterKey = `${centerId}:${et}`;
          const arr0 = Array.isArray(d[reasonKey]) ? (d[reasonKey] as string[]) : [];
          const has = arr0.includes(filterKey);
          if (!nextVisible && !has) {
            d = { ...d0, [reasonKey]: [...arr0, filterKey] };
            changed = true;
          } else if (nextVisible && has) {
            d = { ...d0, [reasonKey]: arr0.filter((x: string) => x !== filterKey) };
            changed = true;
          }
        }

        if (!changed) return n;

        // Compute hidden state from all reasons
        const reasonsAfter =
          (Array.isArray(d.hiddenByLoneParentsOf) && d.hiddenByLoneParentsOf.length > 0) ||
          (Array.isArray(d.hiddenByLoneChildrenOf) && d.hiddenByLoneChildrenOf.length > 0) ||
          (Array.isArray(d.hiddenByLoneGovernanceOf) && d.hiddenByLoneGovernanceOf.length > 0) ||
          (Array.isArray(d.hiddenByLocalFilter) && d.hiddenByLocalFilter.length > 0);
        const reasonsBefore =
          (Array.isArray(d0.hiddenByLoneParentsOf) && d0.hiddenByLoneParentsOf.length > 0) ||
          (Array.isArray(d0.hiddenByLoneChildrenOf) && d0.hiddenByLoneChildrenOf.length > 0) ||
          (Array.isArray(d0.hiddenByLoneGovernanceOf) && d0.hiddenByLoneGovernanceOf.length > 0) ||
          (Array.isArray(d0.hiddenByLocalFilter) && d0.hiddenByLocalFilter.length > 0);
        const legacyHidden = Boolean((n as any).hidden) && !d.hiddenByStack && !reasonsAfter && !reasonsBefore;
        const hidden = legacyHidden || Boolean(d.hiddenByStack) || Boolean(reasonsAfter);
        return { ...(n as any), hidden, data: d } as any;
      });
    });

    if (isLevelsLayout) {
      invokeArrange();
    }
  }, [nodes, edges, setNodes, layoutMode, invokeArrange, normalizeEdgeType]);

  const displayNodes = React.useMemo(() => {
    // Perf mode: keep visuals light, but still allow graph-query highlight (low-cost style).
    const signalModeActive = hoverHighlightMode === 'signal-propagation';
    const signalSet = signalModeActive ? signalPropagationNodeIdSet : null;
    const signalActive = !!signalSet && signalSet.size > 0;
    const qset = signalModeActive ? null : graphQueryHighlightIds;
    const qActive = !!qset && qset.size > 0;
    const qaSet = signalModeActive || qActive ? null : qaHighlightNodeIdSet;
    const qaActive = !!qaSet && qaSet.size > 0;
    const hoverSet = qActive || qaActive ? null : domainOverlayHoverNodeIdSet;
    const domainHoverActive = !!hoverSet && hoverSet.size > 0;
    const dset = qActive || qaActive || domainHoverActive ? null : domainOverlayHighlightNodeIds;
    const dActive = !!dset && dset.size > 0;
    const topDepSet = signalModeActive ? null : (domainTopDependencyFocusActive && !domainHoverActive ? domainTopDependencyNodeIds : null);
    const topDepActive = !!topDepSet && topDepSet.size > 0;
    if (perfMode && !signalActive && !qActive && !qaActive && !domainHoverActive && !dActive && !topDepActive && !String(fragmentProfileActiveNodeId || '').trim()) return nodes;

    const owned = hoverHighlight?.nodeIds;
    const ultimatePersons: Set<string> | undefined = (hoverHighlight as any)?.ultimatePersonIds;
    const hovered = String(hoveredNodeId || '').trim();
    const hoverActive = !!hoverHighlight && !!hovered;
    const profileActiveId = String(fragmentProfileActiveNodeId || '').trim();
    const profileActive = !!profileActiveId;
    const nodeCriticalSurface = new Map<string, {
      score: number;
      productRef?: string;
      productLabel?: string;
      summary?: string;
      metricKey?: string;
      metricLabel?: string;
      metricValue?: number;
      metricUnit?: string;
    }>();
    const assignNodeCriticalSurface = (nodeId: string, meta: {
      score: number;
      productRef?: string;
      productLabel?: string;
      summary?: string;
      metricKey?: string;
      metricLabel?: string;
      metricValue?: number;
      metricUnit?: string;
    }) => {
      nodeId = String(nodeId || '').trim();
      if (!nodeId) return;
      const prev = nodeCriticalSurface.get(nodeId);
      if (!prev || meta.score > prev.score) {
        nodeCriticalSurface.set(nodeId, meta);
      }
    };
    for (const edge of edges) {
      const ed: any = (edge as any)?.data || {};
      const criticality =
        typeof ed?.criticality === 'number'
          ? ed.criticality
          : (typeof (edge as any)?.criticality === 'number' ? (edge as any).criticality : undefined);
      const primaryMetricValue =
        typeof ed?.primaryCriticalMetricValue === 'number'
          ? ed.primaryCriticalMetricValue
          : (typeof ed?.pathStressScore === 'number' ? ed.pathStressScore : undefined);
      const score = typeof criticality === 'number'
        ? criticality
        : (typeof primaryMetricValue === 'number' ? Math.max(0, Math.min(1, primaryMetricValue)) : 0);
      if (!(score > 0)) continue;
      const productRef = String(ed?.pathProductRef || ed?.productRef || '').trim();
      const productLabel = productRef ? (humanizeCanonicalPath(productRef) || productRef) : '';
      const summary = String(ed?.criticalSummary || '').trim() || undefined;
      const metricKey = String(ed?.primaryCriticalMetricKey || '').trim() || undefined;
      const metricLabel = String(ed?.primaryCriticalMetricLabel || '').trim() || undefined;
      const metricUnit = String(ed?.primaryCriticalMetricUnit || '').trim() || undefined;
      const metricValue = typeof ed?.primaryCriticalMetricValue === 'number' ? ed.primaryCriticalMetricValue : undefined;
      const meta = {
        score,
        productRef: productRef || undefined,
        productLabel: productLabel || undefined,
        summary,
        metricKey,
        metricLabel,
        metricValue,
        metricUnit,
      };
      assignNodeCriticalSurface(edge.source, meta);
      assignNodeCriticalSurface(edge.target, meta);
    }

    return nodes.map((n) => {
      const id = String(n.id || '').trim();
      const isGroup = Boolean((n.data as any)?.isGroup);

      const isOwnedNode = !!owned && id && owned.has(id);
      const isHovered = id === hovered;
      const isQuery = qActive && !!qset && id && qset.has(id);
      const isQaHighlight = qaActive && !!qaSet && id && qaSet.has(id);
      const isDomainHover = domainHoverActive && !!hoverSet && id && hoverSet.has(id);
      const isDomain = dActive && !!dset && id && dset.has(id);
      const isTopDep = topDepActive && !!topDepSet && id && topDepSet.has(id);
      const isSignal = signalActive && !!signalSet && id && signalSet.has(id);
      const isProfileNode = profileActive && !isGroup && id && id === profileActiveId;

      // Base hover highlight behavior (existing UX).
      let opacity = 1;
      let zIndex = 5;
      if (!perfMode && hoverActive) {
        opacity = isGroup ? 0.18 : (isOwnedNode || isHovered ? 1 : 0.22);
        zIndex = isOwnedNode || isHovered ? 20 : (isGroup ? 1 : 5);
      } else if (isGroup) {
        zIndex = 1;
      }

      // Graph-query highlight: make affected nodes pop and lightly de-emphasize others.
      // This is reversible (cleared on pane click / Esc / toggling chip).
      // IMPORTANT: preserve behavior exactly, including clearing query-only styles when inactive.
      const prevStyle: any = (n as any).style || {};
      let style: any = prevStyle;
      let styleChanged = false;

      const highlightMode: '' | 'signal' | 'query' | 'qa' | 'domain-hover' | 'domain' =
        signalActive ? 'signal' : qActive ? 'query' : qaActive ? 'qa' : domainHoverActive ? 'domain-hover' : dActive ? 'domain' : '';
      const wantPrimaryHighlight =
        highlightMode === 'signal'
          ? Boolean(isSignal && !isGroup)
          : highlightMode === 'query'
          ? Boolean(isQuery && !isGroup)
          : highlightMode === 'qa'
          ? Boolean(isQaHighlight && !isGroup)
          : highlightMode === 'domain-hover'
            ? Boolean(isDomainHover && !isGroup)
          : highlightMode === 'domain'
            ? Boolean(isDomain && !isGroup)
            : false;
      if (highlightMode && !wantPrimaryHighlight) {
        // Query keeps slightly stronger dimming than domain overlay.
        opacity =
          highlightMode === 'signal'
            ? (isGroup ? 0.06 : Math.min(opacity, 0.18))
            : highlightMode === 'query'
            ? (isGroup ? 0.12 : Math.min(opacity, 0.55))
            : highlightMode === 'qa'
            ? (isGroup ? 0.12 : Math.min(opacity, 0.55))
            : highlightMode === 'domain-hover'
              ? (isGroup ? 0.10 : Math.min(opacity, 0.38))
            : (isGroup ? 0.12 : Math.min(opacity, 0.52));
      }
      if (wantPrimaryHighlight) {
        opacity = 1;
        zIndex = Math.max(zIndex, 40);
      }
      if (isTopDep && !isGroup) {
        opacity = 1;
        zIndex = Math.max(zIndex, 46);
      }

      // Fragment Profile mode: dim everything except the active node.
      if (profileActive) {
        if (isProfileNode) {
          opacity = 1;
          zIndex = Math.max(zIndex, 70);
        } else {
          opacity = isGroup ? Math.min(opacity, 0.08) : Math.min(opacity, 0.16);
          zIndex = isGroup ? 1 : Math.min(zIndex, 10);
        }
      }

      // Opacity: only materialize if it actually changes from current style value.
      // Missing opacity is equivalent to 1 in CSS, so we don't need to force-set it for opacity=1.
      if (typeof prevStyle.opacity === 'number') {
        if (prevStyle.opacity !== opacity) {
          style = { ...style, opacity };
          styleChanged = true;
        }
      } else if (opacity !== 1) {
        style = { ...style, opacity };
        styleChanged = true;
      }

      // Legacy cleanup: remove old wrapper-level outline highlights if present.
      // New highlight is rendered inside the node card (rounded halo) via data flags.
      if (prevStyle.outline || typeof prevStyle.outlineOffset !== 'undefined' || prevStyle.boxShadow) {
        if (!styleChanged) {
          style = { ...prevStyle };
          styleChanged = true;
        }
        delete style.outline;
        delete style.outlineOffset;
        delete style.boxShadow;
      }

      // Decoration flags (read by HorizonFragmentNode via data):
      // - hub: >=5 incoming or >=5 outgoing visible edges
      // - ultimate beneficiary (person): top of "up" traversal in control hover
      // - stack toggle: temporary right-side stacked columns (levels layout)
      const isHub = !isGroup && id && hubNodeIds.has(id);
      const isUltimateBeneficiary = !isGroup && hoverActive && ultimatePersons && ultimatePersons.has(id);

      const d0: any = (n as any).data || {};
      const stackKey = String(d0?.stackGroupKey || '').trim();
      const stackCount = typeof d0?.stackCount === 'number' ? d0.stackCount : undefined;
      const hasStack = !isGroup && !!stackKey && typeof stackCount === 'number' && stackCount > 1;
      const isLevelsRight = !isGroup && isLevelsLayout && String(d0?.levelsPane || '') === 'right';

      // Stack group (expanded stack container)
      const isStackGroup = isGroup && Boolean(d0?.isStackGroup);
      const stackGroupKey = isStackGroup ? String(d0?.stackGroupKey || '').trim() : '';

      // Only clone data when needed (perf).
      let nextData: any = d0;
      let changed = false;
      const wantQueryHighlight = Boolean(highlightMode === 'query' && isQuery && !isGroup);
      const wantDomainHighlight = Boolean(
        (highlightMode === 'domain' && isDomain && !isGroup) ||
        (highlightMode === 'domain-hover' && isDomainHover && !isGroup),
      );
      const wantTopDependencyHighlight = Boolean(isTopDep && !isGroup);
      const wantSignalPropagationHighlight = Boolean(highlightMode === 'signal' && isSignal && !isGroup);

      if (!isGroup) {
        const prevQuery = Boolean((nextData as any)?.queryHighlight);
        const prevDomain = Boolean((nextData as any)?.domainHighlight);
        const prevTopDep = Boolean((nextData as any)?.topDependencyHighlight);
        const prevSignal = Boolean((nextData as any)?.signalPropagationHighlight);
        if (prevQuery !== wantQueryHighlight || prevDomain !== wantDomainHighlight || prevTopDep !== wantTopDependencyHighlight || prevSignal !== wantSignalPropagationHighlight) {
          if (nextData === d0) {
            nextData = { ...d0 };
            changed = true;
          }
          if (wantQueryHighlight) (nextData as any).queryHighlight = true;
          else delete (nextData as any).queryHighlight;
          if (wantDomainHighlight) (nextData as any).domainHighlight = true;
          else delete (nextData as any).domainHighlight;
          if (wantTopDependencyHighlight) (nextData as any).topDependencyHighlight = true;
          else delete (nextData as any).topDependencyHighlight;
          if (wantSignalPropagationHighlight) (nextData as any).signalPropagationHighlight = true;
          else delete (nextData as any).signalPropagationHighlight;
          changed = true;
        }
      } else if (
        typeof (nextData as any)?.queryHighlight !== 'undefined' ||
        typeof (nextData as any)?.domainHighlight !== 'undefined' ||
        typeof (nextData as any)?.topDependencyHighlight !== 'undefined' ||
        typeof (nextData as any)?.signalPropagationHighlight !== 'undefined'
      ) {
        if (nextData === d0) {
          nextData = { ...d0 };
          changed = true;
        }
        delete (nextData as any).queryHighlight;
        delete (nextData as any).domainHighlight;
        delete (nextData as any).topDependencyHighlight;
        delete (nextData as any).signalPropagationHighlight;
      }
      // For regular (right-pane) stacks, set onToggleStack to toggleStackGroup.
      // BUT: subcategory stacks (left-pane, collapsed by default) already have their own
      // onToggleStack set by elkLayout that calls onToggleSubcategoryCollapse. Don't overwrite it!
      const isSubcategoryStack = Boolean(d0?.subcategoryStack);
      if (hasStack && !isSubcategoryStack) {
        if (nextData === d0) {
          nextData = { ...d0 };
          changed = true;
        }
        if (nextData.onToggleStack !== toggleStackGroup) {
          nextData.onToggleStack = toggleStackGroup;
          changed = true;
        }
      } else if (isStackGroup && stackGroupKey) {
        // Stack group containers also need the toggle callback
        if (nextData === d0) {
          nextData = { ...d0 };
          changed = true;
        }
        if (nextData.onToggleStack !== toggleStackGroup) {
          nextData.onToggleStack = toggleStackGroup;
          changed = true;
        }
      } else if (!isSubcategoryStack && !isStackGroup && typeof nextData?.onToggleStack === 'function') {
        nextData = { ...d0 };
        delete nextData.onToggleStack;
        changed = true;
      }
      // Right-pane collapsed stacks rendered as context cards: inject expand callback.
      const isRightStackContextCard = Boolean((nextData as any)?.rightStackContextCard);
      const rightStackKey = isRightStackContextCard ? String((nextData as any)?.stackKey || '').trim() : '';
      if (isRightStackContextCard && rightStackKey) {
        if (nextData === d0) {
          nextData = { ...d0 };
          changed = true;
        }
        (nextData as any).onToggleContextDetails = () => toggleStackGroup(rightStackKey, true);
        changed = true;
      }

      // Fragment Profile: attach toggle callback + open flag only for real fragment nodes (not groups, not stacks).
      const canProfile = !isGroup && !stackKey;
      const wantProfileOpen = Boolean(canProfile && isProfileNode);
      if (canProfile) {
        if (nextData === d0) {
          nextData = { ...d0 };
          changed = true;
        }
        if (nextData.onToggleFragmentProfile !== toggleFragmentProfile) {
          nextData.onToggleFragmentProfile = toggleFragmentProfile;
          changed = true;
        }
        if (Boolean(nextData.fragmentProfileOpen) !== wantProfileOpen) {
          nextData.fragmentProfileOpen = wantProfileOpen;
          changed = true;
        }
        if (wantProfileOpen) {
          nextData.fragmentProfileEvents = fragmentProfileEvents;
          nextData.fragmentProfileSignals = fragmentProfileSignals;
          nextData.fragmentProfileLoading = fragmentProfileLoading;
          nextData.fragmentProfileFetchTriggered = fragmentProfileFetchTriggered;
        }
      } else if (typeof nextData?.onToggleFragmentProfile === 'function' || typeof nextData?.fragmentProfileOpen !== 'undefined') {
        nextData = { ...d0 };
        delete nextData.onToggleFragmentProfile;
        delete nextData.fragmentProfileOpen;
        delete nextData.fragmentProfileEvents;
        delete nextData.fragmentProfileSignals;
        delete nextData.fragmentProfileLoading;
        delete nextData.fragmentProfileFetchTriggered;
        changed = true;
      }

      if (isHub || isUltimateBeneficiary) {
        if (nextData === d0) {
          nextData = { ...d0 };
          changed = true;
        }
        nextData.hubHighlight = isHub;
        nextData.ultimateBeneficiaryHighlight = isUltimateBeneficiary;
        changed = true;
      }

      if (!isGroup) {
        const criticalMeta = nodeCriticalSurface.get(id);
        const nextProductRef = criticalMeta?.productRef;
        const nextProductLabel = criticalMeta?.productLabel;
        const nextSummary = criticalMeta?.summary;
        const nextMetricKey = criticalMeta?.metricKey;
        const nextMetricLabel = criticalMeta?.metricLabel;
        const nextMetricValue = criticalMeta?.metricValue;
        const nextMetricUnit = criticalMeta?.metricUnit;
        const prevChanged =
          String((nextData as any)?.criticalProductRef || '').trim() !== String(nextProductRef || '').trim() ||
          String((nextData as any)?.criticalProductLabel || '').trim() !== String(nextProductLabel || '').trim() ||
          String((nextData as any)?.criticalSummary || '').trim() !== String(nextSummary || '').trim() ||
          String((nextData as any)?.primaryCriticalMetricKey || '').trim() !== String(nextMetricKey || '').trim() ||
          String((nextData as any)?.primaryCriticalMetricLabel || '').trim() !== String(nextMetricLabel || '').trim() ||
          String((nextData as any)?.primaryCriticalMetricUnit || '').trim() !== String(nextMetricUnit || '').trim() ||
          (typeof (nextData as any)?.primaryCriticalMetricValue === 'number' ? (nextData as any).primaryCriticalMetricValue : undefined) !== nextMetricValue;
        if (prevChanged) {
          if (nextData === d0) {
            nextData = { ...d0 };
          }
          changed = true;
          if (nextProductRef) (nextData as any).criticalProductRef = nextProductRef;
          else delete (nextData as any).criticalProductRef;
          if (nextProductLabel) (nextData as any).criticalProductLabel = nextProductLabel;
          else delete (nextData as any).criticalProductLabel;
          if (nextSummary) (nextData as any).criticalSummary = nextSummary;
          else delete (nextData as any).criticalSummary;
          if (nextMetricKey) (nextData as any).primaryCriticalMetricKey = nextMetricKey;
          else delete (nextData as any).primaryCriticalMetricKey;
          if (nextMetricLabel) (nextData as any).primaryCriticalMetricLabel = nextMetricLabel;
          else delete (nextData as any).primaryCriticalMetricLabel;
          if (typeof nextMetricValue === 'number') (nextData as any).primaryCriticalMetricValue = nextMetricValue;
          else delete (nextData as any).primaryCriticalMetricValue;
          if (nextMetricUnit) (nextData as any).primaryCriticalMetricUnit = nextMetricUnit;
          else delete (nextData as any).primaryCriticalMetricUnit;
        }
      } else if (
        typeof (nextData as any)?.criticalProductRef !== 'undefined' ||
        typeof (nextData as any)?.criticalProductLabel !== 'undefined' ||
        typeof (nextData as any)?.criticalSummary !== 'undefined' ||
        typeof (nextData as any)?.primaryCriticalMetricKey !== 'undefined' ||
        typeof (nextData as any)?.primaryCriticalMetricLabel !== 'undefined' ||
        typeof (nextData as any)?.primaryCriticalMetricValue !== 'undefined' ||
        typeof (nextData as any)?.primaryCriticalMetricUnit !== 'undefined'
      ) {
        if (nextData === d0) nextData = { ...d0 };
        delete (nextData as any).criticalProductRef;
        delete (nextData as any).criticalProductLabel;
        delete (nextData as any).criticalSummary;
        delete (nextData as any).primaryCriticalMetricKey;
        delete (nextData as any).primaryCriticalMetricLabel;
        delete (nextData as any).primaryCriticalMetricValue;
        delete (nextData as any).primaryCriticalMetricUnit;
        changed = true;
      }

      if (isLevelsRight) {
        if (nextData === d0) {
          nextData = { ...d0 };
          changed = true;
        }
        if (nextData.onOpenRightFilters !== openRightFilters) {
          nextData.onOpenRightFilters = openRightFilters;
          changed = true;
        }
        if (nextData.onToggleRightFilter !== toggleRightFilter) {
          nextData.onToggleRightFilter = toggleRightFilter;
          changed = true;
        }
        if (nextData.onTrace !== openTraceModalForNode) {
          nextData.onTrace = openTraceModalForNode;
          changed = true;
        }
        if (nextData.onUndoTrace !== undoLastTrace) {
          nextData.onUndoTrace = undoLastTrace;
          changed = true;
        }
        if (Boolean(nextData.canUndoTrace) !== Boolean(canUndoTrace)) {
          nextData.canUndoTrace = canUndoTrace;
          changed = true;
        }
      } else if (
        typeof (nextData as any)?.onOpenRightFilters === 'function' ||
        typeof (nextData as any)?.onToggleRightFilter === 'function' ||
        typeof (nextData as any)?.onTrace === 'function' ||
        typeof (nextData as any)?.onUndoTrace === 'function' ||
        typeof (nextData as any)?.canUndoTrace !== 'undefined'
      ) {
        if (nextData === d0) nextData = { ...d0 };
        delete nextData.onOpenRightFilters;
        delete nextData.onToggleRightFilter;
        delete nextData.rightFiltersOpen;
        delete nextData.onTrace;
        delete nextData.onUndoTrace;
        delete nextData.canUndoTrace;
        changed = true;
      }

      // Local relation filters (non-group nodes in Levels or Supply Chain mode)
      const hasLocalFilters =
        graphViewOverlayPolicy.allowLocalRelationFilters &&
        expandEdgeTypesForLayout.length > 0;
      if (!isGroup && hasLocalFilters) {
        if (nextData === d0) {
          nextData = { ...d0 };
          changed = true;
        }
        if (nextData.onOpenLocalFilters !== openLocalFilters) {
          nextData.onOpenLocalFilters = openLocalFilters;
          changed = true;
        }
        if (nextData.onToggleLocalFilter !== toggleLocalFilter) {
          nextData.onToggleLocalFilter = toggleLocalFilter;
          changed = true;
        }
      } else if (
        typeof (nextData as any)?.onOpenLocalFilters === 'function' ||
        typeof (nextData as any)?.onToggleLocalFilter === 'function' ||
        Array.isArray((nextData as any)?.localFilterEdgeTypes)
      ) {
        if (nextData === d0) nextData = { ...d0 };
        delete nextData.onOpenLocalFilters;
        delete nextData.onToggleLocalFilter;
        delete nextData.localFilterEdgeTypes;
        delete nextData.localFiltersOpen;
        delete nextData.localFilterState;
        changed = true;
      }

      // Only create new node object if something actually changed
      const prevZIndex = (n as any).zIndex;
      const zIndexChanged = prevZIndex !== zIndex;

      const prevHidden = Boolean((n as any).hidden);
      const nextHidden = isTopDep && !isGroup ? false : prevHidden;
      const hiddenChanged = prevHidden !== nextHidden;

      if (changed || styleChanged || zIndexChanged || hiddenChanged) {
        const out: any = { ...n, data: changed ? nextData : d0, zIndex, style };
        if (hiddenChanged) out.hidden = nextHidden;
        return out as any;
      }
      // Return original node if nothing changed (preserves reference for React.memo)
      return n;
    });
  }, [
    perfMode,
    nodes,
    edges,
    hoverHighlight,
    hoverHighlightMode,
    hoveredNodeId,
    graphQueryHighlightIds,
    qaHighlightNodeIdSet,
    signalPropagationNodeIdSet,
    domainOverlayHoverNodeIdSet,
    domainOverlayHighlightNodeIds,
    theme,
    hubNodeIds,
    toggleStackGroup,
    layoutMode,
    openRightFilters,
    toggleRightFilter,
    openTraceModalForNode,
    undoLastTrace,
    canUndoTrace,
    openLocalFilters,
    toggleLocalFilter,
    expandEdgeTypesForLayout,
    graphViewOverlayPolicy.allowLocalRelationFilters,
    fragmentProfileActiveNodeId,
    fragmentProfileEvents,
    fragmentProfileSignals,
    fragmentProfileLoading,
    fragmentProfileFetchTriggered,
    toggleFragmentProfile,
    domainTopDependencyFocusActive,
    domainTopDependencyNodeIds,
    controlCorridorMode,
  ]);

  // Lightweight info for perf mode (no re-styling of edges)
  const perfHoveredEdgeInfo = React.useMemo(() => {
    if (!perfMode || !hoveredEdgeId) return null;
    const e = edges.find((x) => x.id === hoveredEdgeId) as any;
    if (!e) return null;
    const edgeTypeRaw = String(e?.data?.edgeType || e?.label || e?.data?.subkind || '').trim();
    const edgeType = (e?.data?.edgeCaption as string | undefined) || getEdgeCaptionShort(edgeTypeRaw) || edgeTypeRaw;
    return edgeType ? { id: hoveredEdgeId, edgeType } : { id: hoveredEdgeId, edgeType: 'Edge' };
  }, [perfMode, hoveredEdgeId, edges, getEdgeCaptionShort]);

  // Функция для перераспределения всех видимых нод
  const handleArrange = useCallback(async () => {
    if (nodes.length === 0 || arranging) return;

    setArranging(true);
    setError(null);

    try {
      dbg('[Cockpit] Arranging', nodes.length, 'nodes and', edges.length, 'edges', 'layout mode:', layoutMode);

      const fragmentNodes = nodes.filter((n) => !(n.data as any)?.isGroup);
      const nodesForArrange =
        isFreeformLayout || isLinearLayout
          ? fragmentNodes
          : ((nodesStateRef.current?.length ? nodesStateRef.current : nodes) as Node<HorizonNodeData>[]);
      const arrangedResult = await arrangeWithGraphView(layoutMode, nodesForArrange as any, edges as any);
      let withGroups: Node<HorizonNodeData>[] = arrangedResult.nodes as any;
      setEdges(arrangedResult.edges as any);
      // Add onGroupPageChange callback to group nodes
      let withCallbacks = withGroups.map((node) => {
        if ((node.data as any)?.isGroup) {
          return {
            ...node,
            data: { ...node.data, onGroupPageChange: handleGroupPageChange },
          };
        }
        return node;
      });
      setNodes(withCallbacks);

      // Focus behavior after arrange:
      // - If domain top-deps focus is requested, fit viewport to that subset.
      // - Else if a stack-preview chip (or expand/collapse flow) requested focus, center on that node.
      // - Otherwise DO NOT auto-fit the entire graph. Expanding a node should keep the user's zoom/context.
      const focusNodeIds = pendingFocusNodeIdsRef.current;
      pendingFocusNodeIdsRef.current = null;
      const focusId = String(pendingFocusNodeIdRef.current || '').trim();
      pendingFocusNodeIdRef.current = null;
      if (focusNodeIds && focusNodeIds.size > 0) {
        setTimeout(() => {
          try {
            const inst: any = reactFlowInstanceRef.current;
            if (!inst || typeof inst.fitView !== 'function') return;
            const toFit = (withCallbacks as any[]).filter((n) => {
              if (!n || (n.data as any)?.isGroup || Boolean((n as any).hidden)) return false;
              const id = String((n as any)?.id || '').trim();
              return !!id && focusNodeIds.has(id);
            });
            if (toFit.length > 0) {
              inst.fitView({ nodes: toFit, padding: 0.24, duration: 420 });
              return;
            }
            const first = Array.from(focusNodeIds)[0];
            if (first) focusOnFragment(first);
          } catch {}
        }, 80);
      } else if (focusId) {
        setTimeout(() => {
          try {
            // Keep zoom stable; just center on the node.
            focusOnFragment(focusId);
          } catch {}
        }, 60);
      }

      dbg('[Cockpit] Arrange completed');
    } catch (err: any) {
      console.error('[Cockpit] Arrange failed:', err);
      setError(err.message || 'Failed to arrange graph');
    } finally {
      setArranging(false);
    }
  }, [nodes, edges, arranging, handleLoadRelatedFragments, handleGroupPageChange, setNodes, setEdges, recalculateLinearHandles, recalculateFreeformHandles, recalculateSupplyChainHandles, recalculateLevelsTwoPaneHandles, isLinearLayout, isFreeformLayout, isLevelsLayout, isSupplyChainLayout, levelsExpandMode, collapsedSubcategories, toggleSubcategoryCollapse, rightDockStateByNodeId, toggleRightDock, expandedRoleGroups, toggleRoleOverflow]);

  // Keep ref updated for stack toggle re-layout (avoid TDZ / circular deps).
  onArrangeRef.current = () => { void handleArrange(); };

  // When switching layout mode, re-layout the currently visible graph.
  React.useEffect(() => {
    if (!nodes || nodes.length === 0) return;
    if (arranging) return;
    if (lastAppliedLayoutModeRef.current === layoutMode) return;

    const doRelayout = async () => {
      try {
        lastAppliedLayoutModeRef.current = layoutMode;

        const current = (nodesStateRef.current || nodes) as any[];
        const fragmentNodes: Node<HorizonNodeData>[] = current.filter((n) => !(n?.data as any)?.isGroup);
        const edgeRenderType = 'default';
        const nextEdges: Edge[] = (edges || []).map((e: any) => ({ ...e, type: edgeRenderType }));
        const nodesForArrange =
          isFreeformLayout || isLinearLayout
            ? fragmentNodes
            : (current as Node<HorizonNodeData>[]);
        const arrangedResult = await arrangeWithGraphView(layoutMode, nodesForArrange as any, nextEdges as any);
        let nextNodes: Node<HorizonNodeData>[] = arrangedResult.nodes as any;
        let finalEdges: Edge[] = arrangedResult.edges as any;

        if (isFreeformLayout) {
          try {
            const roots = nextNodes
              .filter((n) => !(n.data as any)?.isGroup)
              .filter((n) => typeof (n.data as any)?.depth === 'number' && (n.data as any).depth === 0)
              .sort((a, b) => (a.position?.x || 0) - (b.position?.x || 0));

            const rootId = roots[0]?.id || null;
            rootNodeIdRef.current = rootId;
            expandedNodesRef.current = new Set();

            let g = createEmptyGrid();
            if (rootId) g.rootNodeId = rootId;
            for (let i = 0; i < roots.length; i++) {
              const rid = String(roots[i].id || '').trim();
              if (!rid) continue;
              g = setGridCell(g, { row: 0, col: i, type: 'node', nodeId: rid });
            }
            setGridState(g);
          } catch (e) {
            console.warn('[Cockpit] Failed to re-init freeform grid after layout mode switch', e);
          }
        }

        // Add onGroupPageChange callback to groups (both modes)
        let finalNodes = (nextNodes as any[]).map((n) => {
          if ((n.data as any)?.isGroup) {
            return { ...n, data: { ...n.data, onGroupPageChange: handleGroupPageChange } };
          }
          return { ...n, data: { ...n.data, onLoadRelated: invokeLoadRelated, onCollapse: invokeCollapse, onScopeScout: invokeScopeScout } };
        });
        setEdges(finalEdges as any);
        setNodes(finalNodes as any);
      } catch (e: any) {
        console.warn('[Cockpit] Failed to relayout after layout mode switch', e);
      }
    };

    // Fire and forget (no blocking render)
    void doRelayout();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layoutMode]);

  // Функция для скачивания всех видимых Lens Items в JSON
  const handleDownload = useCallback(() => {
    if (nodes.length === 0 && edges.length === 0) {
      setError('No graph data to download');
      return;
    }

    try {
      const now = new Date().toISOString();
      const lensItems: LensItem[] = [];

      // Преобразуем nodes в LensItems
      nodes.forEach((node) => {
        const nodeData = node.data;
        // Skip UI-only container nodes
        if ((nodeData as any)?.isGroup) return;
        const lensItem: LensItem = {
          id: nodeData.lensItemId || `node-${node.id}`,
          tenantId: '',
          projectId: projectId || '',
          lensId: lensId || '',
          kind: 'node',
          subkind: 'fragment',
          fragmentId: nodeData.fragmentId,
          canonicalPath: nodeData.canonicalPath,
          label: nodeData.label,
          type: nodeData.type,
          state: nodeData.state,
          status: nodeData.status,
          expanded: nodeData.expanded,
          collapsed: nodeData.collapsed,
          critical: nodeData.critical,
          position: node.position ? { x: node.position.x, y: node.position.y } : undefined,
          createdAt: now,
          updatedAt: now,
        };
        lensItems.push(lensItem);
      });

      // Преобразуем edges в LensItems
      edges.forEach((edge) => {
        const edgeData = edge.data;
        const sourceNode = nodes.find((n) => n.id === edge.source);
        const targetNode = nodes.find((n) => n.id === edge.target);
        
        if (!sourceNode || !targetNode) {
          console.warn('[Cockpit] Skipping edge with missing nodes:', edge.id);
          return;
        }

        const lensItem: LensItem = {
          id: edgeData.lensItemId || `edge-${edge.id}`,
          tenantId: '',
          projectId: projectId || '',
          lensId: lensId || '',
          kind: 'edge',
          subkind: edgeData.edgeType || edge.label || 'DEPENDS_ON',
          sourceFragmentId: sourceNode.data.fragmentId || sourceNode.id,
          targetFragmentId: targetNode.data.fragmentId || targetNode.id,
          edgeType: edgeData.edgeType || edge.label as any,
          criticality: edgeData.criticality,
          amplification: edgeData.amplification,
          latencyHours: edgeData.latencyHours,
          direction: edgeData.direction || 'forward',
          createdAt: now,
          updatedAt: now,
        };
        lensItems.push(lensItem);
      });

      // Создаем JSON файл
      const jsonContent = JSON.stringify(lensItems, null, 2);
      const blob = new Blob([jsonContent], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `lens-items-${lensId || 'export'}-${new Date().toISOString().split('T')[0]}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      dbg('[Cockpit] Downloaded', lensItems.length, 'lens items');
    } catch (err: any) {
      console.error('[Cockpit] Download failed:', err);
      setError(err.message || 'Failed to download graph data');
    }
  }, [nodes, edges, lensId, projectId]);

  const handleFitView = useCallback(() => {
    const inst: any = reactFlowInstanceRef.current;
    if (!inst || typeof inst.fitView !== 'function') return;
    try {
      inst.fitView({ padding: 0.12, duration: 320 });
    } catch {
      // best effort only
    }
  }, []);

  const closeAddNode = React.useCallback(() => {
    addNodeAbortRef.current?.abort();
    addNodeAbortRef.current = null;
    setAddNodeOpen(false);
    setAddNodeQuery('');
    setAddNodeResults([]);
    setAddNodeLoading(false);
    setAddNodeError(null);
    setAddNodeActiveIndex(0);
  }, []);

  const openScoutIntentModal = React.useCallback((query: string) => {
    const q = String(query || '').trim();
    if (!q) return;
    closeAddNode();
    setScoutIntentQuery(q);
    setScoutIntentIntent(null);
    setScoutIntentError(null);
    setScoutProgressLog([]);
    setScoutRunLoading(false);
    setScoutIntentMode('general');
    setScoutIntentModeTouched(false);
    setScoutIntentHotPathOnly(false);
    setScoutIntentDirection('downstream');
    setScoutIntentModalOpen(true);
  }, [closeAddNode]);

  const closeScoutIntentModal = React.useCallback(() => {
    setScoutIntentModalOpen(false);
    setScoutIntentQuery('');
    setScoutIntentIntent(null);
    setScoutIntentError(null);
    setScoutProgressLog([]);
    setScoutRunLoading(false);
    setScoutIntentMode('general');
    setScoutIntentModeTouched(false);
    setScoutIntentHotPathOnly(false);
    setScoutIntentDirection('downstream');
  }, []);

  const openCommandPalette = React.useCallback(() => {
    if (!lensId) return;
    // Single palette at a time.
    setAskGraphOpen(false);
    setModelerOpen(false);
    setAddNodeOpen(true);
    setAddNodeQuery('');
    setAddNodeResults([]);
    setAddNodeLoading(false);
    setAddNodeError(null);
    setAddNodeActiveIndex(0);
  }, [lensId]);

  const toggleCommandPalette = React.useCallback(() => {
    if (addNodeOpen) {
      closeAddNode();
      return;
    }
    openCommandPalette();
  }, [addNodeOpen, closeAddNode, openCommandPalette]);

  const closeAskGraph = React.useCallback(() => {
    setAskGraphOpen(false);
    setAskGraphQuery('');
    setAskGraphLoading(false);
    setAskGraphError(null);
  }, []);

  const normalizeListInput = React.useCallback((raw: string): string[] => {
    const seen = new Set<string>();
    const out: string[] = [];
    String(raw || '')
      .split(/[\n,]+/g)
      .map((v) => String(v || '').trim())
      .filter(Boolean)
      .forEach((v) => {
        if (seen.has(v)) return;
        seen.add(v);
        out.push(v);
      });
    return out;
  }, []);

  const parseModelerParameters = React.useCallback((raw: string): { value: Record<string, any>; error: string | null } => {
    const trimmed = String(raw || '').trim();
    if (!trimmed) return { value: {}, error: null };
    try {
      const parsed = JSON.parse(trimmed);
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        return { value: {}, error: 'Parameters must be a JSON object.' };
      }
      return { value: parsed as Record<string, any>, error: null };
    } catch (e: any) {
      return { value: {}, error: String(e?.message || 'JSON parse error') };
    }
  }, []);

  const modelerParsedParams = React.useMemo(() => parseModelerParameters(modelerParametersInput), [modelerParametersInput, parseModelerParameters]);
  const modelerParamObj = modelerParsedParams.value;
  const modelerParamError = modelerParsedParams.error;

  const modelerParamNum = React.useCallback((key: string, fallback: number): number => {
    const raw = modelerParamObj?.[key];
    if (typeof raw === 'number' && Number.isFinite(raw)) return raw;
    if (typeof raw === 'string') {
      const n = Number(raw);
      if (Number.isFinite(n)) return n;
    }
    return fallback;
  }, [modelerParamObj]);

  const modelerParamStr = React.useCallback((key: string, fallback: string): string => {
    const raw = modelerParamObj?.[key];
    if (typeof raw === 'string' && raw.trim()) return raw.trim();
    return fallback;
  }, [modelerParamObj]);

  const updateModelerParameter = React.useCallback((key: string, value: any) => {
    setModelerParametersInput((prev) => {
      const parsed = parseModelerParameters(prev);
      const next = { ...(parsed.value || {}) };
      next[key] = value;
      return JSON.stringify(next, null, 2);
    });
  }, [parseModelerParameters]);

  const selectedNodeCanonicalPath = React.useMemo(() => {
    const sid = String(selectedNodeId || '').trim();
    if (!sid) return '';
    const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || nodes;
    const node = currentNodes.find((n) => String((n as any)?.id || '').trim() === sid);
    const cp = String((node?.data as any)?.canonicalPath || '').trim();
    return cp;
  }, [nodes, selectedNodeId]);

  const selectedNodeFragmentId = React.useMemo(() => {
    const sid = String(selectedNodeId || '').trim();
    if (!sid) return '';
    const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || nodes;
    const node = currentNodes.find((n) => String((n as any)?.id || '').trim() === sid);
    const fid = String((node?.data as any)?.fragmentId || '').trim();
    return fid;
  }, [nodes, selectedNodeId]);

  const visibleCanonicalPaths = React.useMemo(
    () =>
      nodes
        .map((node) => String((node.data as any)?.canonicalPath || '').trim())
        .filter(Boolean),
    [nodes],
  );

  const suggestedModelerTargetPaths = React.useMemo(() => {
    const seen = new Set<string>();
    const out: string[] = [];
    const push = (v: string) => {
      const p = String(v || '').trim();
      if (!p || seen.has(p)) return;
      seen.add(p);
      out.push(p);
    };
    push(selectedNodeCanonicalPath);
    for (const p of lensScopePaths) push(String(p || '').trim());
    const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || nodes;
    for (const n of currentNodes) {
      if ((n.data as any)?.isGroup) continue;
      push(String((n.data as any)?.canonicalPath || '').trim());
      if (out.length >= 24) break;
    }
    return out;
  }, [lensScopePaths, nodes, selectedNodeCanonicalPath]);

  const modelerTargetPaths = React.useMemo(() => normalizeListInput(modelerTargetPathsInput), [modelerTargetPathsInput, normalizeListInput]);

  const setModelerTargetPaths = React.useCallback((paths: string[]) => {
    const unique = normalizeListInput(paths.join('\n'));
    setModelerTargetPathsInput(unique.join('\n'));
  }, [normalizeListInput]);

  const toggleModelerTargetPath = React.useCallback((path: string) => {
    const p = String(path || '').trim();
    if (!p) return;
    const cur = normalizeListInput(modelerTargetPathsInput);
    const has = cur.includes(p);
    const next = has ? cur.filter((x) => x !== p) : [...cur, p];
    setModelerTargetPaths(next);
  }, [modelerTargetPathsInput, normalizeListInput, setModelerTargetPaths]);

  const modelerLocationPath = modelerParamStr('location_path', '/loc/kz/na/na');
  const modelerVatFrom = modelerParamNum('vat_rate_from', 12);
  const modelerVatTo = modelerParamNum('vat_rate_to', 16);
  const modelerBaselineGDP = modelerParamNum('baseline_gdp_bln_kzt', 135000);
  const modelerBaselineVatRevenue = modelerParamNum('baseline_vat_revenue_bln_kzt', 4200);
  const modelerPassThrough = modelerParamNum('pass_through_rate', 0.75);
  const modelerDemandElasticity = modelerParamNum('demand_elasticity', -0.35);
  const modelerComplianceElasticity = modelerParamNum('compliance_elasticity', 0.14);
  const modelerShadowSensitivity = modelerParamNum('shadow_sensitivity', 0.18);

  const setModelerPreset = React.useCallback((presetId: string) => {
    const preset = MODELER_SCENARIO_PRESETS.find((p) => p.id === presetId) || MODELER_SCENARIO_PRESETS[0];
    if (!preset) return;
    setModelerPresetId(preset.id);
    setModelerScenarioId(preset.scenarioId);
    setModelerContractType(preset.contractType);
    setModelerDomainModesInput((preset.domainModes || []).join(', '));
    setModelerEngineId(preset.engineId);
    setModelerParametersInput(JSON.stringify(preset.parameters || {}, null, 2));
    setModelerMode('preview');
    setModelerAdvancedOpen(false);
    setModelerError(null);
  }, []);

  const closeModeler = React.useCallback(() => {
    setModelerOpen(false);
    setModelerLoading(false);
    setModelerError(null);
    setModelerAdvancedOpen(false);
  }, []);

  const openModeler = React.useCallback(() => {
    if (!lensId) return;
    closeAddNode();
    closeAskGraph();
    setModelerOpen(true);
    setModelerError(null);
    setModelerAdvancedOpen(false);
    if (!String(modelerTargetPathsInput || '').trim()) {
      setModelerTargetPaths(suggestedModelerTargetPaths.slice(0, 12));
    }
  }, [lensId, closeAddNode, closeAskGraph, modelerTargetPathsInput, suggestedModelerTargetPaths, setModelerTargetPaths]);

  const runModelerScenario = React.useCallback(
    async (modeOverride?: 'preview' | 'apply') => {
      if (!lensId || modelerLoading) return;
      setModelerError(null);

      if (modelerParamError) {
        setModelerError(`Parameters JSON is invalid: ${modelerParamError}`);
        return;
      }
      const parsedParameters: Record<string, any> = modelerParamObj || {};

      const reqMode = modeOverride || modelerMode;
      const contractType = String(modelerContractType || '').trim() || 'policy-impact';
      const domainModes = normalizeListInput(modelerDomainModesInput);
      const targetPaths = modelerTargetPaths;
      const seedFragmentIds = selectedNodeFragmentId ? [selectedNodeFragmentId] : [];

      setModelerLoading(true);
      try {
        const result = await api.runHorizonScenario({
          scenarioId: String(modelerScenarioId || '').trim() || undefined,
          contractType,
          domainModes: domainModes.length > 0 ? domainModes : undefined,
          engineId: String(modelerEngineId || '').trim() || undefined,
          mode: reqMode,
          parameters: parsedParameters,
          seedFragmentIds: seedFragmentIds.length > 0 ? seedFragmentIds : undefined,
          targetPaths: targetPaths.length > 0 ? targetPaths : undefined,
          maxTraceSteps: 3000,
        });
        setModelerResult(result);

        if (reqMode === 'apply') {
          // Apply mode persists decoded business metrics to node state; reload to reflect updated node metrics.
          await handleLoadGraph();
        }
      } catch (e: any) {
        const msg = String(e?.response?.data?.message || e?.response?.data?.error || e?.message || 'Scenario run failed').trim();
        setModelerError(msg || 'Scenario run failed');
      } finally {
        setModelerLoading(false);
      }
    },
    [
      lensId,
      modelerLoading,
      modelerParametersInput,
      modelerMode,
      modelerContractType,
      modelerDomainModesInput,
      modelerTargetPaths,
      selectedNodeFragmentId,
      modelerScenarioId,
      modelerEngineId,
      modelerParamError,
      modelerParamObj,
      normalizeListInput,
      handleLoadGraph,
    ],
  );

  React.useEffect(() => {
    const requestSeq = signalPropagationRequestSeqRef.current + 1;
    signalPropagationRequestSeqRef.current = requestSeq;

    if (hoverHighlightMode !== 'signal-propagation') {
      setSignalPropagationLoading(false);
      setSignalPropagationError(null);
      setSignalPropagationOverlay(null);
      return;
    }

    const sourceNodeId = String(selectedNodeId || '').trim();
    const sourceFragmentId = String(selectedNodeFragmentId || '').trim();
    if (!lensId || !sourceNodeId || !sourceFragmentId) {
      setSignalPropagationLoading(false);
      setSignalPropagationOverlay(null);
      setSignalPropagationError('Select a visible node to preview signal propagation.');
      return;
    }
    if (modelerParamError) {
      setSignalPropagationLoading(false);
      setSignalPropagationOverlay(null);
      setSignalPropagationError(`Invalid modeler parameters: ${modelerParamError}`);
      return;
    }

    const visibleNodeIds = new Set<string>();
    const nodeIdsByFragmentId = new Map<string, string[]>();
    const pushFragmentNodeId = (fragmentId: string, nodeId: string) => {
      const fid = String(fragmentId || '').trim();
      const nid = String(nodeId || '').trim();
      if (!fid || !nid) return;
      const bucket = nodeIdsByFragmentId.get(fid) || [];
      if (!bucket.includes(nid)) bucket.push(nid);
      nodeIdsByFragmentId.set(fid, bucket);
    };
    for (const n of nodes) {
      const nodeId = String((n as any)?.id || '').trim();
      if (!nodeId || (n as any)?.hidden) continue;
      const d: any = (n as any)?.data || {};
      if (d?.isGroup) continue;
      visibleNodeIds.add(nodeId);
      const fragmentId = String(d?.fragmentId || nodeId).trim();
      if (fragmentId) pushFragmentNodeId(fragmentId, nodeId);
      pushFragmentNodeId(nodeId, nodeId);
    }
    if (!visibleNodeIds.has(sourceNodeId)) {
      setSignalPropagationLoading(false);
      setSignalPropagationOverlay(null);
      setSignalPropagationError('Selected node is not visible in current graph view.');
      return;
    }

    const visibleEdgeIds = new Set<string>();
    const edgeIdsByPair = new Map<string, string[]>();
    const edgeIdsByPairAndType = new Map<string, string[]>();
    const addEdgeLookup = (m: Map<string, string[]>, key: string, edgeId: string) => {
      if (!key || !edgeId) return;
      const bucket = m.get(key) || [];
      if (!bucket.includes(edgeId)) bucket.push(edgeId);
      m.set(key, bucket);
    };
    for (const e of visibleEdges) {
      if ((e as any)?.hidden) continue;
      const edgeId = String((e as any)?.id || '').trim();
      const sourceId = String((e as any)?.source || '').trim();
      const targetId = String((e as any)?.target || '').trim();
      if (!edgeId || !sourceId || !targetId) continue;
      visibleEdgeIds.add(edgeId);
      const pairKey = `${sourceId}=>${targetId}`;
      addEdgeLookup(edgeIdsByPair, pairKey, edgeId);
      const edgeTypeNorm = normalizeEdgeType(
        String((e as any)?.data?.edgeType || (e as any)?.data?.subkind || (e as any)?.label || ''),
      );
      if (edgeTypeNorm) {
        addEdgeLookup(edgeIdsByPairAndType, `${pairKey}|${edgeTypeNorm}`, edgeId);
      }
    }

    const resolveVisibleNodeIdsForFragment = (fragmentIdRaw: string): string[] => {
      const fragmentId = String(fragmentIdRaw || '').trim();
      if (!fragmentId) return [];
      const out = new Set<string>();
      const direct = nodeIdsByFragmentId.get(fragmentId) || [];
      for (const nid of direct) {
        if (visibleNodeIds.has(nid)) out.add(nid);
      }
      if (visibleNodeIds.has(fragmentId)) out.add(fragmentId);
      return Array.from(out);
    };

    setSignalPropagationLoading(true);
    setSignalPropagationError(null);

    (async () => {
      try {
        const domainModes = normalizeListInput(modelerDomainModesInput);
        const result = await api.runHorizonScenario({
          scenarioId: String(modelerScenarioId || '').trim() || undefined,
          contractType: String(modelerContractType || '').trim() || undefined,
          domainModes: domainModes.length > 0 ? domainModes : undefined,
          engineId: String(modelerEngineId || '').trim() || undefined,
          mode: 'preview',
          parameters: modelerParamObj || {},
          seedFragmentIds: [sourceFragmentId],
          targetPaths: modelerTargetPaths.length > 0 ? modelerTargetPaths : undefined,
          maxTraceSteps: 3000,
        });
        if (signalPropagationRequestSeqRef.current !== requestSeq) return;

        const nextNodeIds = new Set<string>([sourceNodeId]);
        const nextEdgeIds = new Set<string>();
        const addNodesByFragment = (fragmentIdRaw: string) => {
          for (const nid of resolveVisibleNodeIdsForFragment(fragmentIdRaw)) {
            nextNodeIds.add(nid);
          }
        };
        const linkByTrace = (fromFragmentRaw: string, toFragmentRaw: string, edgeTypeRaw: string) => {
          const fromNodeIds = resolveVisibleNodeIdsForFragment(fromFragmentRaw);
          const toNodeIds = resolveVisibleNodeIdsForFragment(toFragmentRaw);
          if (fromNodeIds.length === 0 || toNodeIds.length === 0) return;
          const edgeTypeNorm = normalizeEdgeType(edgeTypeRaw);
          const tryLookup = (m: Map<string, string[]>, key: string): boolean => {
            const ids = m.get(key) || [];
            if (ids.length === 0) return false;
            for (const id of ids) nextEdgeIds.add(id);
            return true;
          };

          let matched = false;
          for (const fromNodeId of fromNodeIds) {
            for (const toNodeId of toNodeIds) {
              if (!fromNodeId || !toNodeId || fromNodeId === toNodeId) continue;
              if (edgeTypeNorm) {
                matched =
                  tryLookup(edgeIdsByPairAndType, `${fromNodeId}=>${toNodeId}|${edgeTypeNorm}`) ||
                  tryLookup(edgeIdsByPairAndType, `${toNodeId}=>${fromNodeId}|${edgeTypeNorm}`) ||
                  matched;
              }
            }
          }
          if (matched) return;
          for (const fromNodeId of fromNodeIds) {
            for (const toNodeId of toNodeIds) {
              if (!fromNodeId || !toNodeId || fromNodeId === toNodeId) continue;
              matched =
                tryLookup(edgeIdsByPair, `${fromNodeId}=>${toNodeId}`) ||
                tryLookup(edgeIdsByPair, `${toNodeId}=>${fromNodeId}`) ||
                matched;
            }
          }
        };

        const signalRuns = Array.isArray(result?.signalRuns) ? result.signalRuns : [];
        let traceSteps = 0;
        for (const run of signalRuns) {
          const affected = Array.isArray((run as any)?.affectedFragments) ? (run as any).affectedFragments : [];
          for (const fragmentId of affected) addNodesByFragment(String(fragmentId || ''));

          const trace = Array.isArray((run as any)?.trace) ? (run as any).trace : [];
          traceSteps += trace.length;
          for (const ev of trace) {
            const fromFragmentId = String((ev as any)?.fromFragmentId || (ev as any)?.fromFragment || '').trim();
            const toFragmentId = String((ev as any)?.toFragmentId || (ev as any)?.toFragment || '').trim();
            addNodesByFragment(fromFragmentId);
            addNodesByFragment(toFragmentId);

            const outcome = String((ev as any)?.outcome || '').trim().toLowerCase();
            if (outcome !== 'propagated') continue;

            const edgeId = String((ev as any)?.edgeId || '').trim();
            if (edgeId && visibleEdgeIds.has(edgeId)) {
              nextEdgeIds.add(edgeId);
              continue;
            }
            linkByTrace(fromFragmentId, toFragmentId, String((ev as any)?.edgeType || ''));
          }
        }

        const affectedOverall = Array.isArray((result as any)?.affectedFragments) ? (result as any).affectedFragments : [];
        for (const fragmentId of affectedOverall) addNodesByFragment(String(fragmentId || ''));

        setSignalPropagationOverlay({
          sourceNodeId,
          sourceFragmentId,
          nodeIds: Array.from(nextNodeIds).filter((id) => visibleNodeIds.has(id)).sort(),
          edgeIds: Array.from(nextEdgeIds).filter((id) => visibleEdgeIds.has(id)).sort(),
          signalRuns: signalRuns.length,
          traceSteps,
        });
        setSignalPropagationError(null);
      } catch (e: any) {
        if (signalPropagationRequestSeqRef.current !== requestSeq) return;
        const msg = String(
          e?.response?.data?.message || e?.response?.data?.error || e?.message || 'Signal propagation preview failed',
        ).trim();
        setSignalPropagationOverlay(null);
        setSignalPropagationError(msg || 'Signal propagation preview failed');
      } finally {
        if (signalPropagationRequestSeqRef.current === requestSeq) {
          setSignalPropagationLoading(false);
        }
      }
    })();
  }, [
    hoverHighlightMode,
    selectedNodeId,
    selectedNodeFragmentId,
    lensId,
    nodes,
    visibleEdges,
    normalizeEdgeType,
    modelerScenarioId,
    modelerContractType,
    modelerDomainModesInput,
    modelerEngineId,
    modelerParamObj,
    modelerParamError,
    modelerTargetPaths,
    normalizeListInput,
  ]);

  const extractStateMetrics = React.useCallback((raw: any): Record<string, number> | undefined => {
    if (!raw || typeof raw !== 'object') return undefined;
    const out: Record<string, number> = {};
    for (const [k, v] of Object.entries(raw)) {
      if (typeof v === 'number') {
        out[k] = v;
        continue;
      }
      if (v && typeof v === 'object' && 'value' in (v as any)) {
        const vv: any = (v as any).value;
        if (typeof vv === 'number') out[k] = vv;
      }
    }
    return Object.keys(out).length > 0 ? out : undefined;
  }, []);

  const computeNewRootPosition = React.useCallback(
    (fragmentId: string) => {
      // Prefer grid placement in freeform mode (keeps multi-root row aligned)
      if (isFreeformLayout && gridState.cells.size > 0) {
        let maxCol = -1;
        for (const [, cell] of gridState.cells) {
          if (cell.type !== 'node') continue;
          if (cell.row !== 0) continue;
          maxCol = Math.max(maxCol, cell.col);
        }
        const nextCol = maxCol + 1;
        return { position: gridToCanvasPosition(0, nextCol), row: 0, col: nextCol, nodeId: fragmentId };
      }

      // Fallback: place to the right of the right-most depth=0 fragment node.
      const roots = nodes.filter((n) => !(n.data as any)?.isGroup).filter((n) => (n.data as any)?.depth === 0);
      const maxX = roots.reduce((acc, n) => Math.max(acc, Number(n.position?.x ?? 0)), 0);
      const pos = { x: maxX + ELK_NODE_WIDTH + 220, y: 0 };
      return { position: pos, row: null as any, col: null as any, nodeId: fragmentId };
    },
    [isFreeformLayout, gridState.cells, nodes, ELK_NODE_WIDTH],
  );

  // --- Scout from query (full pipeline: scope-scout-full) ---
  const rtGraphNodes = useHorizonRealtimeStore((s) => s.nodes);
  const rtGraphEdges = useHorizonRealtimeStore((s) => s.edges);
  const pendingScoutRef = React.useRef<
    Map<
      string,
      {
        placeholders: { placeholderId: string; canonicalPath: string }[];
        restPaths: string[];
      }
    >
  >(new Map());
  const realtimeEdgeLensItemIdRef = React.useRef<Map<string, string>>(new Map());
  const realtimeEdgePersistingRef = React.useRef<Set<string>>(new Set());

  const genCorrelationId = React.useCallback((): string => {
    try {
      const c: any = (globalThis as any)?.crypto;
      if (c?.randomUUID) return String(c.randomUUID());
    } catch {}
    // Fallback: good-enough unique token for client-side correlation
    return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  }, []);

  // --- Graph Intent ---
  const executeGraphIntentForQuery = React.useCallback(
    async (query: string) => {
      const q = String(query || '').trim();
      if (!lensId || !q || graphIntentLoading) return;

      const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || [];
      const currentEdges: Edge[] = edgesStateRef.current || [];

      const nodesSnap = currentNodes
        .filter((n) => !((n.data as any)?.isGroup))
        .map((n: any) => ({
          nodeId: String(n.id || '').trim(),
          fragmentId: String((n.data as any)?.fragmentId || '').trim() || undefined,
          canonicalPath: String((n.data as any)?.canonicalPath || '').trim() || undefined,
          label: String((n.data as any)?.label || '').trim() || undefined,
          type: String((n.data as any)?.type || '').trim() || undefined,
          position: n.position ? { x: Number(n.position.x) || 0, y: Number(n.position.y) || 0 } : undefined,
        }))
        .filter((n) => n.nodeId);

      const nodeIdSet = new Set(nodesSnap.map((n) => n.nodeId));
      const edgesSnap = currentEdges
        .filter((e: any) => nodeIdSet.has(String(e.source || '').trim()) && nodeIdSet.has(String(e.target || '').trim()))
        .map((e: any) => ({
          edgeId: String(e.id || '').trim() || undefined,
          sourceNodeId: String(e.source || '').trim(),
          targetNodeId: String(e.target || '').trim(),
          sourceFragmentId: String((e.data as any)?.sourceFragmentId || (e.data as any)?.source || '').trim() || undefined,
          targetFragmentId: String((e.data as any)?.targetFragmentId || (e.data as any)?.target || '').trim() || undefined,
          relationType: String((e.data as any)?.edgeType || e.type || '').trim() || undefined,
          direction: 'forward' as const,
        }));

      const planReq: GraphIntentPlanRequest = {
        version: GRAPH_INTENT_VERSION_V1,
        rawQuery: q,
        queryLanguage: 'auto',
        executionLanguage: 'en',
        snapshot: {
          nodes: nodesSnap,
          edges: edgesSnap,
          visibleNodeIds: nodesSnap.map((n) => n.nodeId),
          viewState: { zoom: 1, filters: {} },
        },
        options: {
          allowSearchGrounding: true,
          allowInternalGrounding: true,
          maxNewNodes: 60,
          maxNewEdges: 120,
        },
      };

      if (import.meta.env.DEV) {
        console.log('[GraphIntent] plan request:', {
          rawQuery: planReq.rawQuery,
          snapshotNodesN: planReq.snapshot.nodes?.length ?? 0,
          snapshotEdgesN: planReq.snapshot.edges?.length ?? 0,
          snapshot: planReq.snapshot,
        });
      }

      setGraphIntentLoading(true);
      setGraphIntentError(null);
      setGraphIntentPlan(null);

      try {
        const plan = await api.planHorizonGraphIntent(lensId, planReq);
        if (import.meta.env.DEV) {
          console.log('[GraphIntent] plan response:', JSON.stringify(plan, null, 2));
        }
        setGraphIntentPlan(plan);
      } catch (e: any) {
        const d = e?.response?.data;
        const msg = (typeof d === 'object' && d?.detail)
          ? String(d.detail).trim()
          : (typeof d === 'object' && d?.error)
            ? String(d.error).trim()
            : (typeof d === 'string' ? d : e?.message || 'Failed to plan').trim();
        if (import.meta.env.DEV) {
          console.warn('[GraphIntent] plan error:', e);
        }
        setGraphIntentError(msg || 'Failed to generate graph intent plan');
        setGraphIntentPlan(null);
      } finally {
        setGraphIntentLoading(false);
      }
    },
    [lensId, graphIntentLoading],
  );

  const applyGraphIntentPlan = React.useCallback(async () => {
    const plan = graphIntentPlan;
    if (!lensId || !plan || graphIntentApplyLoading) return;

    const applyReq: GraphIntentApplyRequest = {
      version: GRAPH_INTENT_VERSION_V1,
      plan,
      applyMode: 'patch',
      idempotencyKey: genCorrelationId(),
    };

    if (import.meta.env.DEV) {
      console.log('[GraphIntent] apply request:', JSON.stringify({ applyReq: { ...applyReq, plan: { intentClass: plan.intentClass, create: { groupingsN: plan.create?.groupings?.length, nodesN: plan.create?.nodes?.length, edgesN: plan.create?.edges?.length } } } }, null, 2));
    }

    setGraphIntentApplyLoading(true);
    try {
      const res = await api.applyHorizonGraphIntent(lensId, applyReq, { confirm: plan.requiresConfirmation });
      if (import.meta.env.DEV) {
        console.log('[GraphIntent] apply response:', JSON.stringify(res, null, 2));
      }
      setGraphIntentPlan(null);
      setGraphIntentError(null);
      await handleLoadGraph();
    } catch (e: any) {
      const d = e?.response?.data;
      const msg =
        (typeof d === 'object' && d?.detail) ? String(d.detail).trim()
        : (typeof d === 'object' && d?.error) ? String(d.error).trim()
        : (typeof d === 'string' ? d : e?.message || 'Failed to apply').trim();
      if (import.meta.env.DEV) {
        console.warn('[GraphIntent] apply error:', e);
      }
      setGraphIntentError(msg || 'Failed to apply');
    } finally {
      setGraphIntentApplyLoading(false);
    }
  }, [lensId, graphIntentPlan, graphIntentApplyLoading, genCorrelationId, handleLoadGraph]);

  const dismissGraphIntentPlan = React.useCallback(() => {
    setGraphIntentPlan(null);
    setGraphIntentError(null);
  }, []);

  const findFragmentIdByCanonicalPath = React.useCallback(
    (canonicalPath: string): string => {
      const cp = String(canonicalPath || '').trim();
      if (!cp) return '';
      const nodes = rtGraphNodes || {};
      for (const v of Object.values(nodes as any)) {
        const vcp = String((v as any)?.canonicalPath || '').trim();
        const fid = String((v as any)?.fragmentId || '').trim();
        if (vcp && fid && vcp === cp) return fid;
      }
      return '';
    },
    [rtGraphNodes],
  );

  const materializeScoutedNode = React.useCallback(
    async (args: { placeholderId: string; fragmentId: string; canonicalPath: string }) => {
      const fragmentId = norm(args.fragmentId);
      const canonicalPath = String(args.canonicalPath || '').trim();
      if (!lensId || !fragmentId || !canonicalPath) return;

      // If already on canvas, just remove placeholder and focus existing.
      const existing = (nodesStateRef.current || nodes).find((n) => norm((n.data as any)?.fragmentId || n.id) === fragmentId);
      if (existing) {
        setNodes((prev) => (prev || []).filter((n) => String(n.id || '').trim() !== String(args.placeholderId || '').trim()));
        try {
          focusOnFragment(existing.id);
        } catch {}
        return;
      }

      // Resolve fragment for label/type/state and persist as a LensItem node.
      let fragment: any;
      try {
        fragment = await api.getHorizonFragmentByPath(canonicalPath);
      } catch (e) {
        console.warn('[Cockpit] Failed to resolve scouted fragment by path', canonicalPath, e);
        return;
      }

      const label =
        String(fragment?.labels?.en || fragment?.label || '').trim() ||
        humanizeCanonicalPath(canonicalPath) ||
        fragmentId;
      const type = String(fragment?.type || '').trim() || undefined;
      const state = extractStateMetrics(fragment?.state);
      const placement = computeNewRootPosition(fragmentId);
      const position = placement.position;

      let createdId = '';
      try {
        const res = await api.createLensItem(lensId, {
          kind: 'node',
          subkind: 'fragment',
          fragmentId,
          canonicalPath,
          label,
          type,
          state,
          status: fragment?.status,
          expanded: false,
          collapsed: false,
          critical: false,
          position,
        });
        createdId = String(res?.id || '').trim();
      } catch (e) {
        console.warn('[Cockpit] Failed to create lens item for scouted node', e);
        return;
      }

      // Replace placeholder node with real node.
      const newNode: Node<HorizonNodeData> = {
        id: fragmentId,
        type: 'horizonFragment',
        position,
        data: {
          id: fragmentId,
          lensItemId: createdId,
          fragmentId,
          canonicalPath,
          facilityKind: readFacilityKindFromAny(fragment),
          label,
          type,
          state,
          showMetrics: showNodeMetrics,
          status: fragment?.status,
          expanded: false,
          collapsed: false,
          critical: false,
          parentFragmentId: undefined,
          rootFragmentId: fragmentId,
          depth: 0,
          onLoadRelated: invokeLoadRelated,
          onCollapse: invokeCollapse,
          onScopeScout: invokeScopeScout,
        },
      } as any;

      setNodes((prev) => {
        const pid = String(args.placeholderId || '').trim();
        const filtered = (prev || []).filter((n) => String((n as any)?.id || '').trim() !== pid);
        return [...filtered, newNode as any];
      });

      // Focus new node (keep current zoom).
      try {
        focusOnFragment(fragmentId);
      } catch {}
    },
    [
      lensId,
      nodes,
      setNodes,
      computeNewRootPosition,
      extractStateMetrics,
      showNodeMetrics,
      invokeLoadRelated,
      invokeCollapse,
      invokeScopeScout,
      focusOnFragment,
    ],
  );

  const materializeScoutedNodeWithoutPlaceholder = React.useCallback(
    async (args: { fragmentId: string; canonicalPath: string }) => {
      const fragmentId = norm(args.fragmentId);
      const canonicalPath = String(args.canonicalPath || '').trim();
      if (!lensId || !fragmentId || !canonicalPath) return;

      const existing = (nodesStateRef.current || nodes).find((n) => norm((n.data as any)?.fragmentId || n.id) === fragmentId);
      if (existing) return;

      let fragment: any;
      try {
        fragment = await api.getHorizonFragmentByPath(canonicalPath);
      } catch (e) {
        console.warn('[Cockpit] Failed to resolve scouted fragment by path', canonicalPath, e);
        return;
      }

      const label =
        String(fragment?.labels?.en || fragment?.label || '').trim() ||
        humanizeCanonicalPath(canonicalPath) ||
        fragmentId;
      const type = String(fragment?.type || '').trim() || undefined;
      const state = extractStateMetrics(fragment?.state);
      const placement = computeNewRootPosition(fragmentId);
      const position = placement.position;

      let createdId = '';
      try {
        const res = await api.createLensItem(lensId, {
          kind: 'node',
          subkind: 'fragment',
          fragmentId,
          canonicalPath,
          label,
          type,
          state,
          status: fragment?.status,
          expanded: false,
          collapsed: false,
          critical: false,
          position,
        });
        createdId = String(res?.id || '').trim();
      } catch (e) {
        console.warn('[Cockpit] Failed to create lens item for scouted node', e);
        return;
      }

      const newNode: Node<HorizonNodeData> = {
        id: fragmentId,
        type: 'horizonFragment',
        position,
        data: {
          id: fragmentId,
          lensItemId: createdId,
          fragmentId,
          canonicalPath,
          facilityKind: readFacilityKindFromAny(fragment),
          label,
          type,
          state,
          showMetrics: showNodeMetrics,
          status: fragment?.status,
          expanded: false,
          collapsed: false,
          critical: false,
          parentFragmentId: undefined,
          rootFragmentId: fragmentId,
          depth: 0,
          onLoadRelated: invokeLoadRelated,
          onCollapse: invokeCollapse,
          onScopeScout: invokeScopeScout,
        },
      } as any;

      setNodes((prev) => [...(prev || []), newNode as any]);
    },
    [
      lensId,
      nodes,
      setNodes,
      computeNewRootPosition,
      extractStateMetrics,
      showNodeMetrics,
      invokeLoadRelated,
      invokeCollapse,
      invokeScopeScout,
    ],
  );

  const materializeRealtimeEdge = React.useCallback(
    async (raw: {
      edgeId?: string;
      sourceFragmentId?: string;
      targetFragmentId?: string;
      sourceCanonicalPath?: string;
      targetCanonicalPath?: string;
      type?: string;
      qualifiers?: any;
    }) => {
      const edgeId = String(raw?.edgeId || '').trim();
      if (!lensId || !edgeId) return;

      const currentNodes = (nodesStateRef.current || nodes) as Node<HorizonNodeData>[];
      const nodeIdByFragmentId = new Map<string, string>();
      const nodeIdByCanonicalPath = new Map<string, string>();
      const nodeById = new Map<string, Node<HorizonNodeData>>();
      for (const node of currentNodes) {
        const data: any = node?.data;
        if (data?.isGroup) continue;
        const nodeId = String(node?.id || '').trim();
        if (!nodeId) continue;
        nodeById.set(nodeId, node);
        const fragmentId = String(data?.fragmentId || '').trim();
        if (fragmentId && !nodeIdByFragmentId.has(fragmentId)) nodeIdByFragmentId.set(fragmentId, nodeId);
        const canonicalPath = String(data?.canonicalPath || '').trim();
        if (canonicalPath && !nodeIdByCanonicalPath.has(canonicalPath)) nodeIdByCanonicalPath.set(canonicalPath, nodeId);
      }

      const resolveNodeId = (fragmentId?: string, canonicalPath?: string): string => {
        const fid = String(fragmentId || '').trim();
        const cp = String(canonicalPath || '').trim();
        if (fid) {
          const byFragment = nodeIdByFragmentId.get(fid);
          if (byFragment) return byFragment;
        }
        if (cp) {
          const byPath = nodeIdByCanonicalPath.get(cp);
          if (byPath) return byPath;
        }
        return '';
      };

      const sourceNodeId = resolveNodeId(raw?.sourceFragmentId, raw?.sourceCanonicalPath);
      const targetNodeId = resolveNodeId(raw?.targetFragmentId, raw?.targetCanonicalPath);
      if (!sourceNodeId || !targetNodeId || sourceNodeId === targetNodeId) return;

      const sourceNode = nodeById.get(sourceNodeId);
      const targetNode = nodeById.get(targetNodeId);
      const sourceFragmentId = String(raw?.sourceFragmentId || (sourceNode?.data as any)?.fragmentId || sourceNodeId).trim();
      const targetFragmentId = String(raw?.targetFragmentId || (targetNode?.data as any)?.fragmentId || targetNodeId).trim();
      const edgeType = String(raw?.type || '').trim() || 'SUPPLIES_TO';
      const baseStyle = getEdgeStyle(edgeType, undefined, raw?.qualifiers);
      const stroke = getEdgeStroke(baseStyle);
      let existingLensItemId = '';
      for (const edge of edgesStateRef.current || []) {
        if (String((edge as any)?.id || '').trim() !== edgeId) continue;
        existingLensItemId = String(((edge as any)?.data || {}).lensItemId || '').trim();
        if (existingLensItemId) break;
      }
      const cachedLensItemId = realtimeEdgeLensItemIdRef.current.get(edgeId) || existingLensItemId;
      if (cachedLensItemId && !realtimeEdgeLensItemIdRef.current.has(edgeId)) {
        realtimeEdgeLensItemIdRef.current.set(edgeId, cachedLensItemId);
      }

      setEdges((prev) => {
        const current = Array.isArray(prev) ? prev : [];
        const nextEdge: Edge = {
          id: edgeId,
          source: sourceNodeId,
          target: targetNodeId,
          type: 'default',
          interactionWidth: 18,
          markerEnd: {
            type: MarkerType.ArrowClosed,
            color: stroke,
          },
          style: baseStyle,
          data: {
            edgeType,
            edgeCaption: getEdgeCaptionShort(edgeType),
            sourceFragmentId,
            targetFragmentId,
            sourceCanonicalPath: String(raw?.sourceCanonicalPath || '').trim() || undefined,
            targetCanonicalPath: String(raw?.targetCanonicalPath || '').trim() || undefined,
            qualifiers: raw?.qualifiers,
            baseStyle,
            lensItemId: cachedLensItemId,
            isDirectedExplorationSynthetic: true,
          },
        } as any;

        const existingIndex = current.findIndex((edge: any) => String(edge?.id || '').trim() === edgeId);
        if (existingIndex >= 0) {
          const existing = current[existingIndex] as any;
          const merged = {
            ...existing,
            ...nextEdge,
            data: {
              ...(existing?.data || {}),
              ...(nextEdge.data || {}),
            },
          } as Edge;
          const next = current.slice();
          next[existingIndex] = merged;
          return next as any;
        }

        const duplicate = current.some((edge: any) => {
          const data = edge?.data || {};
          return (
            String(edge?.source || '').trim() === sourceNodeId &&
            String(edge?.target || '').trim() === targetNodeId &&
            String(data?.edgeType || edge?.label || '').trim().toUpperCase() === edgeType.toUpperCase() &&
            String(data?.sourceCanonicalPath || '').trim() === String(raw?.sourceCanonicalPath || '').trim() &&
            String(data?.targetCanonicalPath || '').trim() === String(raw?.targetCanonicalPath || '').trim()
          );
        });
        if (duplicate) return prev;
        return [...current, nextEdge as any];
      });

      if (!sourceFragmentId || !targetFragmentId) return;
      if (cachedLensItemId || realtimeEdgePersistingRef.current.has(edgeId)) return;

      realtimeEdgePersistingRef.current.add(edgeId);
      try {
        const created = await api.createLensItem(lensId, {
          kind: 'edge',
          subkind: edgeType,
          sourceFragmentId,
          targetFragmentId,
          edgeType: edgeType as any,
          direction: 'forward',
        } as any);
        const lensItemId = String(created?.id || '').trim();
        if (lensItemId) {
          realtimeEdgeLensItemIdRef.current.set(edgeId, lensItemId);
          setEdges((prev) =>
            (prev || []).map((edge: any) =>
              String(edge?.id || '').trim() === edgeId
                ? {
                    ...edge,
                    data: {
                      ...(edge?.data || {}),
                      lensItemId,
                    },
                  }
                : edge,
            ),
          );
        }
      } catch (error) {
        console.warn('[Cockpit] Failed to persist realtime directed edge', edgeId, error);
      } finally {
        realtimeEdgePersistingRef.current.delete(edgeId);
      }
    },
    [lensId, nodes, setEdges, getEdgeStyle, getEdgeStroke, getEdgeCaptionShort],
  );

  // WS-driven reconciliation: placeholders -> materializeScoutedNode; restPaths -> materializeScoutedNodeWithoutPlaceholder.
  React.useEffect(() => {
    if (!lensId) return;
    const pending = pendingScoutRef.current;
    if (!pending || pending.size === 0) return;

    for (const [cid, st] of pending.entries()) {
      let changed = false;
      const nextPlaceholders = st.placeholders.filter((p) => {
        const cp = String(p.canonicalPath || '').trim();
        if (!cp) return true;
        const fid = findFragmentIdByCanonicalPath(cp);
        if (!fid) return true;
        void materializeScoutedNode({ placeholderId: p.placeholderId, fragmentId: fid, canonicalPath: cp });
        changed = true;
        return false;
      });
      const nextRestPaths = st.restPaths.filter((cp) => {
        const c = String(cp || '').trim();
        if (!c) return false;
        const fid = findFragmentIdByCanonicalPath(c);
        if (!fid) return true;
        void materializeScoutedNodeWithoutPlaceholder({ fragmentId: fid, canonicalPath: c });
        changed = true;
        return false;
      });
      if (changed) {
        if (nextPlaceholders.length === 0 && nextRestPaths.length === 0) {
          pending.delete(cid);
        } else {
          pending.set(cid, { placeholders: nextPlaceholders, restPaths: nextRestPaths });
        }
      }
    }
  }, [lensId, rtGraphNodes, findFragmentIdByCanonicalPath, materializeScoutedNode, materializeScoutedNodeWithoutPlaceholder]);

  React.useEffect(() => {
    if (!lensId) return;
    const items = Object.values(rtGraphEdges || {});
    if (items.length === 0) return;
    for (const raw of items as any[]) {
      void materializeRealtimeEdge(raw);
    }
  }, [lensId, rtGraphEdges, nodes, materializeRealtimeEdge]);

  const scoutIntentHasSupplyChainContract = React.useMemo(() => {
    const domains = Array.isArray(scoutIntentIntent?.domains) ? scoutIntentIntent?.domains : [];
    return domains.some((domain: any) => {
      const contractType = String(domain?.contractType || '').trim().toLowerCase();
      return contractType === 'supply-chain' || contractType === 'value-chain';
    });
  }, [scoutIntentIntent]);

  const scoutFullForQuery = React.useCallback(
    async (query: string, options?: { onProgress?: (msg: string) => void; mode?: ScoutingMode }) => {
      const q = String(query || '').trim();
      if (!lensId || q.length < 2) return;

      const onProgress = options?.onProgress;
      const scoutingMode = options?.mode || 'general';
      if (!onProgress) {
        closeAddNode();
      }

      const correlationId = genCorrelationId();
      const firstFiveForGraph: string[] = [];
      const placeholders: { placeholderId: string; canonicalPath: string }[] = [];
      const restPaths: string[] = [];

      try {
        onProgress?.(`Calling scope-scout-full (${scoutingMode})…`);
        const res = await api.scopeScoutHorizonLensFull(
          lensId,
          {
            query: q,
            mode: scoutingMode,
            specialties: specialtiesForScoutingMode(scoutingMode),
          },
          { correlationId },
        );
        const firstFive = res?.firstFiveForGraph || [];
        const allPaths = res?.canonicalPaths || [];
        const jobIds = res?.jobIds || [];

        onProgress?.(`Intent resolved. Jobs created: ${jobIds.length}, paths: ${allPaths.length}`);
        onProgress?.(`First 5 for graph: ${firstFive.join(', ') || '(none)'}`);

        for (let i = 0; i < firstFive.length; i++) {
          const cp = String(firstFive[i] || '').trim();
          if (!cp) continue;
          firstFiveForGraph.push(cp);
          const placeholderId = `scout:${correlationId}:${i}`;
          placeholders.push({ placeholderId, canonicalPath: cp });

          const placement = computeNewRootPosition(placeholderId);
          const ph: Node<HorizonNodeData> = {
            id: placeholderId,
            type: 'horizonFragment',
            position: placement.position,
            data: {
              id: placeholderId,
              fragmentId: placeholderId,
              canonicalPath: cp,
              label: `Scouting: ${cp.split('/').pop() || q}`,
              type: 'UNKNOWN',
              showMetrics: showNodeMetrics,
              status: 'scouting',
              expanded: false,
              collapsed: false,
              critical: false,
              parentFragmentId: undefined,
              rootFragmentId: placeholderId,
              depth: 0,
              isScopeScouting: true,
              onLoadRelated: invokeLoadRelated,
              onCollapse: invokeCollapse,
              onScopeScout: invokeScopeScout,
            } as any,
          } as any;
          setNodes((prev) => [...(prev || []), ph as any]);
        }

        for (let i = firstFive.length; i < allPaths.length; i++) {
          const cp = String(allPaths[i] || '').trim();
          if (cp && !firstFiveForGraph.includes(cp)) restPaths.push(cp);
        }

        setLensScopePaths((prev) => {
          const next = [...prev];
          for (const cp of firstFiveForGraph) {
            if (!next.includes(cp)) next.push(cp);
          }
          return next;
        });

        pendingScoutRef.current.set(correlationId, { placeholders, restPaths });
        onProgress?.(`Placeholders created: ${firstFiveForGraph.length}. ${restPaths.length} more in queue.`);
      } catch (e) {
        console.warn('[Cockpit] scope-scout-full failed', e);
        onProgress?.(`Error: ${(e as Error)?.message || 'scope-scout-full failed'}`);
        const placeholderId = `scout:${correlationId}:0`;
        const placement = computeNewRootPosition(placeholderId);
        const ph: Node<HorizonNodeData> = {
          id: placeholderId,
          type: 'horizonFragment',
          position: placement.position,
          data: {
            id: placeholderId,
            fragmentId: placeholderId,
            canonicalPath: '',
            label: `Scout failed: ${q}`,
            type: 'UNKNOWN',
            showMetrics: showNodeMetrics,
            status: 'failed',
            expanded: false,
            collapsed: false,
            critical: false,
            parentFragmentId: undefined,
            rootFragmentId: placeholderId,
            depth: 0,
            isScopeScouting: false,
            onLoadRelated: invokeLoadRelated,
            onCollapse: invokeCollapse,
            onScopeScout: invokeScopeScout,
          } as any,
        } as any;
        setNodes((prev) => [...(prev || []), ph as any]);
      }
    },
    [
      lensId,
      genCorrelationId,
      computeNewRootPosition,
      showNodeMetrics,
      invokeLoadRelated,
      invokeCollapse,
      invokeScopeScout,
      setNodes,
      closeAddNode,
      setLensScopePaths,
    ],
  );

  const scoutHotPathForQuery = React.useCallback(
    async (query: string, intent: ScoutIntentResolution | null, options?: { onProgress?: (msg: string) => void }) => {
      const q = String(query || '').trim();
      if (!lensId || q.length < 2) return;
      const onProgress = options?.onProgress;
      const correlationId = genCorrelationId();
      const resolvedDirection = scoutIntentDirection === 'both' ? resolveDirectedSupplyChainDirection(intent, q) : scoutIntentDirection;
      const direction: 'upstream' | 'downstream' = resolvedDirection === 'upstream' ? 'upstream' : 'downstream';

      try {
        onProgress?.(`Calling scope-scout-query (supply_chain hot path, ${direction})…`);
        const res = await api.scopeScoutHorizonLensQuery(
          lensId,
          {
            query: q,
            mode: 'supply_chain',
            specialties: specialtiesForScoutingMode('supply_chain'),
            exploration: {
              domainMode: 'supply_chain',
              direction,
              hotPathOnly: true,
              renderTarget: 'graph',
              ignoreFreshness: true,
            },
          },
          { correlationId },
        );

        const targetCanonicalPath = String(res?.targetCanonicalPath || '').trim();
        onProgress?.(`Intent resolved. Directed hot path job created.`);
        if (targetCanonicalPath) {
          onProgress?.(`Target: ${targetCanonicalPath}`);
          setLensScopePaths((prev) => (prev.includes(targetCanonicalPath) ? prev : [...prev, targetCanonicalPath]));
        }
        onProgress?.(`Execution mode: hot path only (${direction}). Live updates will stream into the graph.`);
      } catch (e) {
        console.warn('[Cockpit] scope-scout-query hot path failed', e);
        onProgress?.(`Error: ${(e as Error)?.message || 'scope-scout-query failed'}`);
      }
    },
    [lensId, genCorrelationId, scoutIntentDirection, setLensScopePaths],
  );

  const runScoutFromModal = React.useCallback(async () => {
    const q = scoutIntentQuery.trim();
    if (!q || !lensId) return;
    setScoutRunLoading(true);
    setScoutProgressLog((prev) => [...prev, `Starting ${scoutIntentMode} scout…`]);
    try {
      if (scoutIntentMode === 'supply_chain' && scoutIntentHotPathOnly && scoutIntentHasSupplyChainContract && scoutIntentDirection !== 'both') {
        await scoutHotPathForQuery(q, scoutIntentIntent, {
          onProgress: (msg) => setScoutProgressLog((prev) => [...prev, msg]),
        });
      } else {
        if (scoutIntentMode === 'supply_chain' && scoutIntentHotPathOnly && scoutIntentDirection === 'both') {
          setScoutProgressLog((prev) => [...prev, 'Direction "Both" keeps the broader full supply-chain expansion.']);
        }
        await scoutFullForQuery(q, {
          mode: scoutIntentMode,
          onProgress: (msg) => setScoutProgressLog((prev) => [...prev, msg]),
        });
      }
    } finally {
      setScoutRunLoading(false);
    }
  }, [
    lensId,
    scoutIntentMode,
    scoutIntentQuery,
    scoutIntentHotPathOnly,
    scoutIntentDirection,
    scoutIntentHasSupplyChainContract,
    scoutIntentIntent,
    scoutFullForQuery,
    scoutHotPathForQuery,
  ]);

  const addNodeToLens = React.useCallback(
    async (entity: HorizonSearchEntity) => {
      if (!lensId) return;
      const canonicalPath = String(entity?.canonicalPath || '').trim();
      if (!canonicalPath) return;

      // Resolve to fragment (we need fragmentId/type/labels/state)
      let fragment: any;
      try {
        fragment = await api.getHorizonFragmentByPath(canonicalPath);
      } catch (e) {
        console.warn('[Cockpit] Failed to resolve fragment by path', canonicalPath, e);
        setAddNodeError('Failed to load fragment');
        return;
      }

      const fragmentId = norm(fragment?.id || '');
      if (!fragmentId) {
        setAddNodeError('Fragment not found');
        return;
      }

      // If already on canvas, just focus it.
      const existing = nodes.find((n) => norm((n.data as any)?.fragmentId || n.id) === fragmentId);
      if (existing) {
        try {
          const inst: any = reactFlowInstanceRef.current;
          inst?.fitView?.({ nodes: [existing], padding: 0.25, duration: 350 });
        } catch {}
        closeAddNode();
        return;
      }

      const label =
        String(fragment?.labels?.en || fragment?.label || entity?.name || '').trim() ||
        humanizeCanonicalPath(canonicalPath) ||
        fragmentId;
      const type = String(fragment?.type || entity?.type || '').trim() || undefined;
      const state = extractStateMetrics(fragment?.state);

      const placement = computeNewRootPosition(fragmentId);
      const position = placement.position;

      // Persist lens item first (so node has stable lensItemId)
      let createdId = '';
      try {
        const res = await api.createLensItem(lensId, {
          kind: 'node',
          subkind: 'fragment',
          fragmentId,
          canonicalPath,
          label,
          type,
          state,
          status: fragment?.status,
          expanded: false,
          collapsed: false,
          critical: false,
          position,
        });
        createdId = String(res?.id || '').trim();
      } catch (e) {
        console.warn('[Cockpit] Failed to create lens item for node', e);
        setAddNodeError('Failed to add node to lens');
        return;
      }

      // Update local lensItems list (best-effort, used for download/export)
      setLensItems((prev) => [
        ...prev,
        {
          id: createdId || `node-${fragmentId}`,
          tenantId: '',
          projectId: projectId || '',
          lensId,
          kind: 'node',
          subkind: 'fragment',
          fragmentId,
          canonicalPath,
          label,
          type,
          state,
          status: fragment?.status,
          expanded: false,
          collapsed: false,
          critical: false,
          position,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        } as any,
      ]);

      // Add node to canvas (depth=0 root)
      const newNode: Node<HorizonNodeData> = {
        id: fragmentId,
        type: 'horizonFragment',
        position,
        data: {
          id: fragmentId,
          lensItemId: createdId,
          fragmentId,
          canonicalPath,
          facilityKind: readFacilityKindFromAny(fragment),
          label,
          type,
          state,
          showMetrics: showNodeMetrics,
          status: fragment?.status,
          expanded: false,
          collapsed: false,
          critical: false,
          parentFragmentId: undefined,
          rootFragmentId: fragmentId,
          depth: 0,
          onLoadRelated: invokeLoadRelated,
          onCollapse: invokeCollapse,
          onScopeScout: invokeScopeScout,
        },
      } as any;
      setNodes((prev) => [
        ...prev,
        newNode as any,
      ]);

      // Update freeform grid bookkeeping (keeps multi-root placement stable)
      if (isFreeformLayout) {
        // If the grid wasn't initialized (e.g. graph loaded from LensItems without ELK),
        // rebuild a minimal row0 grid from current depth=0 nodes for predictable future expansions.
        if (gridState.cells.size === 0) {
          const rootCandidates = [...nodes, newNode]
            .filter((n) => !(n.data as any)?.isGroup)
            .filter((n) => (n.data as any)?.depth === 0)
            .sort((a, b) => (a.position?.x ?? 0) - (b.position?.x ?? 0));
          let g = createEmptyGrid();
          const rootId = rootCandidates[0]?.id || null;
          g.rootNodeId = rootId;
          for (let i = 0; i < rootCandidates.length; i++) {
            const rid = String(rootCandidates[i]?.id || '').trim();
            if (!rid) continue;
            g = setGridCell(g, { row: 0, col: i, type: 'node', nodeId: rid });
          }
          setGridState(g);
        } else {
          setGridState((prevGrid) => {
            let maxCol = -1;
            for (const [, cell] of prevGrid.cells) {
              if (cell.type === 'node' && cell.row === 0) maxCol = Math.max(maxCol, cell.col);
            }
            const col = maxCol + 1;
            let g = prevGrid;
            if (!g.rootNodeId) g = { ...g, rootNodeId: fragmentId };
            g = setGridCell(g, { row: 0, col, type: 'node', nodeId: fragmentId });
            return g;
          });
        }
      }

      // Fit view to the new node
      try {
        const inst: any = reactFlowInstanceRef.current;
        // ReactFlow state updates are async; the node might not be immediately available via getNode().
        // Use a short retry + fallback to setCenter so users always "see" the newly added node.
        const focus = () => {
          const n = inst?.getNode?.(fragmentId);
          if (n) {
            inst?.fitView?.({ nodes: [n], padding: 0.25, duration: 350 });
            return true;
          }
          return false;
        };
        if (!focus()) {
          window.setTimeout(() => {
            try {
              if (focus()) return;
              // Fallback: center viewport on the intended position.
              const cx = position.x + ELK_NODE_WIDTH / 2;
              const cy = position.y + ELK_NODE_HEIGHT / 2;
              const zoom = typeof inst?.getZoom === 'function' ? inst.getZoom() : undefined;
              inst?.setCenter?.(cx, cy, { zoom: typeof zoom === 'number' ? zoom : 1, duration: 350 });
            } catch {}
          }, 50);
        }
      } catch {}

      closeAddNode();
    },
    [
      lensId,
      nodes,
      projectId,
      setLensItems,
      setNodes,
      setEdges,
      isFreeformLayout,
      setGridState,
      closeAddNode,
      extractStateMetrics,
      computeNewRootPosition,
      invokeLoadRelated,
      invokeCollapse,
      invokeScopeScout,
    ],
  );

  const focusGraphNodeFromPalette = React.useCallback(
    (nodeId: string) => {
      const id = String(nodeId || '').trim();
      if (!id) return;

      const snapNodes: Node<HorizonNodeData>[] = (nodesStateRef.current || nodes) as any;
      const snapEdges: Edge[] = (edgesStateRef.current || edges) as any;
      const node = snapNodes.find((n: any) => String(n?.id || '').trim() === id);
      if (!node) return;
      if (((node as any)?.data as any)?.isGroup) return;

      // If the node is currently hidden under a collapsed subcategory stack, auto-expand that subcategory,
      // then focus the found node after relayout (search UX).
      const nd: any = (node as any)?.data || {};
      const isHiddenByStack = Boolean(nd?.hiddenByStack) || Boolean((node as any)?.hidden);
      const parentGroupId = String((node as any)?.parentId || (node as any)?.parentNode || '').trim();
      const stackGroupKey = String(nd?.stackGroupKey || '').trim();
      const inferredEdgeType = (() => {
        const direct = String(nd?.subcategoryEdgeType || '').trim();
        if (direct) return direct;
        if (!stackGroupKey) return '';
        const parts = stackGroupKey.split(':');
        return String(parts[parts.length - 1] || '').trim();
      })();
      if (isHiddenByStack && parentGroupId && inferredEdgeType) {
        // Set selection now (consistent), but focus will happen after arrange.
        ignorePaneClicksUntilRef.current = Date.now() + 250;
        setNodes((prev) =>
          (prev || []).map((n: any) => {
            if ((n?.data as any)?.isGroup) return { ...n, selected: false };
            return { ...n, selected: String(n?.id || '').trim() === id };
          }),
        );
        setEdges((prev) => (prev || []).map((e: any) => ({ ...e, selected: false })));

        // Expand the subcategory and focus the actual node.
        toggleSubcategoryCollapse(parentGroupId, inferredEdgeType, false, id);
        closeAddNode();
        return;
      }

      // Keep selection consistent with click behavior (one selected node, no selected edges).
      ignorePaneClicksUntilRef.current = Date.now() + 250;
      setNodes((prev) =>
        (prev || []).map((n: any) => {
          if ((n?.data as any)?.isGroup) return { ...n, selected: false };
          return { ...n, selected: String(n?.id || '').trim() === id };
        }),
      );
      setEdges((prev) => (prev || []).map((e: any) => ({ ...e, selected: false })));

      // Notify inspector with the same "visible context" payload we build on click.
      if (onNodeSelect) {
        const selectedId = id;
        const selectedFragmentId = ((node as any)?.data as any)?.fragmentId || selectedId;

        const incidentEdges = snapEdges
          .filter((e: any) => e.source === selectedId || e.target === selectedId)
          .map((e: any) => {
            const src = snapNodes.find((n: any) => n.id === e.source);
            const tgt = snapNodes.find((n: any) => n.id === e.target);
            return {
              id: e.id,
              edgeType: (e as any)?.data?.edgeType || e.label || 'UNKNOWN',
              sourceNodeId: e.source,
              targetNodeId: e.target,
              sourceFragmentId: (src?.data as any)?.fragmentId || e.source,
              targetFragmentId: (tgt?.data as any)?.fragmentId || e.target,
              criticality: (e as any)?.data?.criticality,
              amplification: (e as any)?.data?.amplification,
              latencyHours: (e as any)?.data?.latencyHours,
            };
          });

        const neighborNodeIds = new Set<string>();
        for (const e of incidentEdges as any[]) {
          if (e.sourceNodeId && e.sourceNodeId !== selectedId) neighborNodeIds.add(e.sourceNodeId);
          if (e.targetNodeId && e.targetNodeId !== selectedId) neighborNodeIds.add(e.targetNodeId);
        }

        const neighborNodes = Array.from(neighborNodeIds)
          .map((nid) => snapNodes.find((n: any) => n.id === nid))
          .filter(Boolean)
          .map((n: any) => ({
            nodeId: n.id,
            fragmentId: n.data?.fragmentId || n.id,
            label: n.data?.label || null,
            canonicalPath: n.data?.canonicalPath || null,
            type: n.data?.type || null,
          }));

        const nodeDataWithContext: any = {
          ...((node as any)?.data || {}),
          fragmentId: selectedFragmentId,
          visibleIncidentEdges: incidentEdges,
          visibleNeighborNodes: neighborNodes,
        };

        onNodeSelect(selectedId, nodeDataWithContext);
      }

      // Focus viewport.
      try {
        const inst: any = reactFlowInstanceRef.current;
        inst?.fitView?.({ nodes: [node as any], padding: 0.25, duration: 350 });
      } catch {}

      closeAddNode();
    },
    [nodes, edges, setNodes, setEdges, onNodeSelect, closeAddNode, toggleSubcategoryCollapse],
  );

  const bgColor = theme === 'dark' ? 'var(--bg-primary)' : 'var(--bg-primary)';
  const bgPatternColor = theme === 'dark' ? 'rgba(255, 255, 255, 0.05)' : 'rgba(0, 0, 0, 0.05)';

  // Memoized styles for Controls and MiniMap (avoid recreating objects on every render during pan/zoom)
  const controlsStyle = React.useMemo(() => ({
    background: 'var(--bg-elevated)',
    border: '1px solid var(--border-subtle)',
    borderRadius: '8px',
  }), []);

  const miniMapStyle = React.useMemo(() => ({
    background: 'var(--bg-elevated)',
    border: '1px solid var(--border-subtle)',
    borderRadius: '8px',
  }), []);

  // Memoized nodeColor callback for MiniMap (avoid recreating function on every render)
  const miniMapNodeColor = React.useCallback((node: Node<HorizonNodeData>) => {
    const data = node.data as HorizonNodeData;
    if (data.critical) return '#EF4444';
    const vt = getHorizonNodeVisualType(data.type, (data as any)?.canonicalPath, (data as any)?.label, (data as any)?.facilityKind);
    return getNodeColors(vt || data.type, theme === 'dark' ? 'dark' : 'light').accent;
  }, [theme]);

  const miniMapMaskColor = theme === 'dark' ? 'rgba(0, 0, 0, 0.5)' : 'rgba(255, 255, 255, 0.5)';

  // Handle node click
  const handleNodeClick = useCallback((event: React.MouseEvent, node: Node<HorizonNodeData>) => {
    // If this click was already handled by edge click, ignore to prevent overriding edge selection.
    if ((event as any)?.defaultPrevented) return;
    // Extra guard: if the click originated from an edge element, ignore.
    const targetEl = event?.target as any;
    if (targetEl?.closest?.('.react-flow__edge') || targetEl?.classList?.contains?.('react-flow__edge-path')) {
      return;
    }
    if (isLegacyCorridorNodeId(String((node as any)?.id || '').trim())) {
      return;
    }
    if (import.meta.env.DEV) console.log('[Cockpit] Node clicked:', node.id, node.data);
    ignorePaneClicksUntilRef.current = Date.now() + 250;
    event.stopPropagation();
    event.preventDefault();

    // Risk Shield: clicking any other node counts as "outside" and closes it.
    const rid = String(fragmentProfileActiveNodeId || '').trim();
    const nid = String(node?.id || '').trim();
    if (rid && nid && rid !== nid) {
      setFragmentProfileActiveNodeId(null);
    }

    // Selecting a node should clear any selected edge highlight.
    setSelectedEdgeId(null);
    onEdgeSelect?.(null, null);

    // Levels Two-Pane: clicking a type-column group with a collapse key triggers collapse-back.
    if (isLevelsLayout && Boolean((node.data as any)?.isGroup) && String(node.id || '').trim().startsWith('levels:lefttype:')) {
      const blockCollapseKey = String((node.data as any)?.groupBlockCollapseKey || '').trim();
      const blockCollapseGroupId = String((node.data as any)?.groupBlockCollapseGroupId || '').trim();
      if (blockCollapseKey && blockCollapseGroupId) {
        toggleSubcategoryCollapse(blockCollapseGroupId, blockCollapseKey, true);
        return;
      }
    }

    // Levels Two-Pane: clicking an aggregated left group toggles "real edges" preview.
    // Clicking any other node clears the preview (returns to default aggregated view).
    if (isLevelsLayout && Boolean((node.data as any)?.isGroup) && String(node.id || '').trim().startsWith('levels:left:')) {
      const gid = String(node.id || '').trim();
      setSelectedLevelsAggGroupId((prev) => (String(prev || '').trim() === gid ? null : gid));
      // Group node click doesn't imply a specific edge type; show all types for that group.
      setSelectedLevelsAggEdgeType(null);
      setSelectedLevelsAggKey(null);
    } else {
      setSelectedLevelsAggGroupId(null);
      setSelectedLevelsAggEdgeType(null);
      setSelectedLevelsAggKey(null);
    }

    // Levels stack preview: clicking a collapsed stack controller shows chip overlays.
    // Clicking elsewhere (another node) closes the preview.
    const clearStackPreview = () => {
      const prevId = stackPreviewControllerIdRef.current;
      if (!prevId) return;
      stackPreviewControllerIdRef.current = null;
      setNodes((prev) =>
        (prev || []).map((n) => {
          if (String((n as any)?.id || '').trim() !== prevId) return n;
          const d0: any = (n as any)?.data || {};
          if (!d0.stackPreviewOpen && !d0.stackPreviewItems && !d0.stackPreviewMore) return n;
          const d = { ...d0 };
          delete d.stackPreviewOpen;
          delete d.stackPreviewItems;
          delete d.stackPreviewMore;
          delete d.onStackPreviewSelect;
          delete d.onStackPreviewMore;
          return { ...(n as any), data: d } as any;
        }),
      );
    };

    const openStackPreview = (controllerId: string, stackKey: string) => {
      const key = String(stackKey || '').trim();
      const cid = String(controllerId || '').trim();
      if (!key || !cid) return;

      // Collect members for this stack.
      // Historically we excluded the controller because its label was shown on the node itself.
      // Now we show it in the preview as the first chip (so the list is complete).
      const allMembers = (nodesStateRef.current || nodes)
        .filter((n) => !(n as any)?.data?.isGroup)
        .filter((n) => String(((n as any)?.data as any)?.stackGroupKey || '').trim() === key);

      const controllerNode = allMembers.find((n: any) => String(n?.id || '').trim() === cid) as any;
      const controllerItem = controllerNode
        ? {
            nodeId: cid,
            label:
              String(controllerNode?.data?.label || '').trim() ||
              humanizeCanonicalPath(String(controllerNode?.data?.canonicalPath || '').trim()) ||
              'Fragment',
            type: String(controllerNode?.data?.type || '').trim() || 'UNKNOWN',
          }
        : null;

      const hiddenMembers = allMembers
        .filter((n: any) => String(n?.id || '').trim() !== cid)
        .filter((n: any) => Boolean(((n as any)?.data as any)?.hiddenByStack) || Boolean((n as any)?.hidden));

      hiddenMembers.sort((a: any, b: any) => {
        const al = String(a?.data?.label || a?.data?.canonicalPath || a?.id || '');
        const bl = String(b?.data?.label || b?.data?.canonicalPath || b?.id || '');
        return al.localeCompare(bl);
      });

      // Keep the old cap for hidden members, but add the controller as the first chip.
      const HIDDEN_PREVIEW_LIMIT = 5;
      const hiddenItems = hiddenMembers.slice(0, HIDDEN_PREVIEW_LIMIT).map((n: any) => ({
        nodeId: String(n.id || '').trim(),
        label: String(n.data?.label || '').trim() || humanizeCanonicalPath(String(n.data?.canonicalPath || '').trim()) || 'Fragment',
        type: String(n.data?.type || '').trim() || 'UNKNOWN',
      }));
      const items = (controllerItem ? [controllerItem, ...hiddenItems] : hiddenItems);
      const more = Math.max(0, allMembers.length - items.length);

      stackPreviewControllerIdRef.current = cid;
      setNodes((prev) =>
        (prev || []).map((n) => {
          const id = String((n as any)?.id || '').trim();
          if (id !== cid) return n;
          const d0: any = (n as any)?.data || {};
          const d = {
            ...d0,
            stackPreviewOpen: true,
            stackPreviewItems: items,
            stackPreviewMore: more,
            onStackPreviewSelect: (targetNodeId: string) => {
              const tid = String(targetNodeId || '').trim();
              if (!tid) return;
              if (tid === cid) {
                // Clicking the controller chip is a no-op: just close the preview.
                clearStackPreview();
                return;
              }
              clearStackPreview();
              pendingFocusNodeIdRef.current = tid;
              if (typeof (d0 as any)?.onToggleStack === 'function') {
                try { (d0 as any).onToggleStack(key, true); } catch {}
              }
            },
            onStackPreviewMore: () => {
              clearStackPreview();
              if (typeof (d0 as any)?.onToggleStack === 'function') {
                try { (d0 as any).onToggleStack(key, true); } catch {}
              }
            },
          };
          return { ...(n as any), data: d } as any;
        }),
      );
    };

    if (isLevelsLayout) {
      const d: any = node.data as any;
      const key = String(d?.stackGroupKey || '').trim();
      const count = typeof d?.stackCount === 'number' ? d.stackCount : 0;
      const expanded = Boolean(d?.stackExpanded);
      const ctrl = String((d as any)?.stackControllerId || '').trim();
      const isController = key && ctrl && ctrl === String(node.id || '').trim();
      const isGroup = Boolean(d?.isGroup);
      if (isController && count > 1 && !expanded) {
        const currentlyOpen = stackPreviewControllerIdRef.current === String(node.id || '').trim();
        if (currentlyOpen) {
          clearStackPreview();
        } else {
          clearStackPreview();
          openStackPreview(String(node.id || '').trim(), key);
        }
        // Close local filters when opening stack preview
        openLocalFilters(String(node.id || '').trim(), false);
      } else {
        clearStackPreview();
        // Toggle local relation filters for non-group, non-stack nodes
        if (!isGroup) {
          const nodeId = String(node.id || '').trim();
          const isOpen = Boolean(d?.localFiltersOpen);
          openLocalFilters(nodeId, !isOpen);
        }
      }
    }

    if (onNodeSelect) {
      const selectedId = node.id;
      const selectedFragmentId = (node.data as any)?.fragmentId || selectedId;

      // Visible edges/nodes context (only what's currently in ReactFlow state)
      const incidentEdges = edges
        .filter((e) => e.source === selectedId || e.target === selectedId)
        .map((e) => {
          const src = nodes.find((n) => n.id === e.source);
          const tgt = nodes.find((n) => n.id === e.target);
          return {
            id: e.id,
            edgeType: (e as any)?.data?.edgeType || e.label || 'UNKNOWN',
            sourceNodeId: e.source,
            targetNodeId: e.target,
            sourceFragmentId: (src?.data as any)?.fragmentId || e.source,
            targetFragmentId: (tgt?.data as any)?.fragmentId || e.target,
            criticality: (e as any)?.data?.criticality,
            amplification: (e as any)?.data?.amplification,
            latencyHours: (e as any)?.data?.latencyHours,
          };
        });

      const neighborNodeIds = new Set<string>();
      for (const e of incidentEdges) {
        if (e.sourceNodeId && e.sourceNodeId !== selectedId) neighborNodeIds.add(e.sourceNodeId);
        if (e.targetNodeId && e.targetNodeId !== selectedId) neighborNodeIds.add(e.targetNodeId);
      }

      const neighborNodes = Array.from(neighborNodeIds)
        .map((id) => nodes.find((n) => n.id === id))
        .filter(Boolean)
        .map((n: any) => ({
          nodeId: n.id,
          fragmentId: n.data?.fragmentId || n.id,
          label: n.data?.label || null,
          canonicalPath: n.data?.canonicalPath || null,
          type: n.data?.type || null,
        }));

      const nodeDataWithContext: any = {
        ...node.data,
        fragmentId: selectedFragmentId,
        visibleIncidentEdges: incidentEdges,
        visibleNeighborNodes: neighborNodes,
      };

      onNodeSelect(selectedId, nodeDataWithContext);
    }
  }, [onNodeSelect, onEdgeSelect, edges, nodes, layoutMode, fragmentProfileActiveNodeId, toggleSubcategoryCollapse]);

  // Handle edge click
  const handleEdgeClick = useCallback((event: React.MouseEvent, edge: Edge) => {
    if (import.meta.env.DEV) console.log('[Cockpit] Edge clicked:', edge.id, edge.data);
    if (isLegacyCorridorEdgeId(String((edge as any)?.id || '').trim())) {
      return;
    }
    ignorePaneClicksUntilRef.current = Date.now() + 250;
    event.stopPropagation();
    event.preventDefault();

    // Risk Shield: edge click is "outside" => close.
    if (fragmentProfileActiveNodeId) setFragmentProfileActiveNodeId(null);

    // Levels Two-Pane: clicking an aggregated edge should toggle the same "real edges" preview
    // as clicking the left group node.
    const ed: any = (edge as any)?.data || {};
    const isLevelsAgg = Boolean(ed?.levelsAgg);
    const aggGroupId = String(ed?.levelsAggGroupId || '').trim();
    const aggKey = String(ed?.levelsAggKey || '').trim();
    const aggEdgeType = normalizeEdgeType(ed?.levelsAggEdgeType || ed?.edgeType || edge?.label);
    const isParentChildAggEdge =
      String((edge as any)?.id || '').trim().startsWith('levels:agg:') ||
      (aggGroupId && aggKey && aggKey.startsWith(`${aggGroupId}::edge:`));
    const isGroupPairAggEdge = aggKey.startsWith('gpair:');
    const isLevelsLeftGroup = aggGroupId && aggGroupId.startsWith('levels:left:');
    if (isLevelsLayout && isLevelsAgg && (isParentChildAggEdge || isGroupPairAggEdge) && isLevelsLeftGroup) {
      // Toggle preview for this exact group+type.
      const curGroup = String(selectedLevelsAggGroupId || '').trim();
      const curType = normalizeEdgeType(selectedLevelsAggEdgeType ?? undefined);
      const curKey = String(selectedLevelsAggKey || '').trim();
      const nextKey = isGroupPairAggEdge ? aggKey : `${aggGroupId}::edge:${aggEdgeType}`;
      const same = curType === aggEdgeType && curKey === nextKey && (!isGroupPairAggEdge ? (curGroup === aggGroupId) : true);
      if (same) {
        setSelectedLevelsAggGroupId(null);
        setSelectedLevelsAggEdgeType(null);
        setSelectedLevelsAggKey(null);
      } else {
        // For group-pair previews, we key purely off aggKey and don't need a single group id.
        setSelectedLevelsAggGroupId(isGroupPairAggEdge ? null : aggGroupId);
        setSelectedLevelsAggEdgeType(aggEdgeType || null);
        setSelectedLevelsAggKey(nextKey || null);
      }
      // The agg edge itself will be suppressed; don't keep it as selected edge highlight.
      setSelectedEdgeId(null);
    } else {
      // Any other edge click closes the group real-edges preview.
      setSelectedLevelsAggGroupId(null);
      setSelectedLevelsAggEdgeType(null);
      setSelectedLevelsAggKey(null);
    }

    // Persist edge highlight until user explicitly clicks elsewhere / another object.
    // (Skip for levels agg edges, since we hide that edge while previewing real edges.)
    if (!(isLevelsLayout && isLevelsAgg && (isParentChildAggEdge || isGroupPairAggEdge) && isLevelsLeftGroup)) {
      setSelectedEdgeId(String(edge.id || '').trim() || null);
    }

    // Click elsewhere should close stack preview and local filters too.
    if (stackPreviewControllerIdRef.current) {
      const prevId = stackPreviewControllerIdRef.current;
      stackPreviewControllerIdRef.current = null;
      setNodes((prev) =>
        (prev || []).map((n) => {
          if (String((n as any)?.id || '').trim() !== prevId) return n;
          const d0: any = (n as any)?.data || {};
          if (!d0.stackPreviewOpen && !d0.stackPreviewItems && !d0.stackPreviewMore) return n;
          const d = { ...d0 };
          delete d.stackPreviewOpen;
          delete d.stackPreviewItems;
          delete d.stackPreviewMore;
          delete d.onStackPreviewSelect;
          delete d.onStackPreviewMore;
          return { ...(n as any), data: d } as any;
        }),
      );
    }
    // Close any open local relation filters
    setNodes((prev) =>
      prev.map((n) => {
        const d: any = (n as any)?.data;
        if (!d || d.isGroup || !d.localFiltersOpen) return n;
        const nd = { ...d };
        delete nd.localFiltersOpen;
        return { ...(n as any), data: nd } as any;
      }),
    );

    if (onEdgeSelect) {
      // Get source and target nodes to extract fragmentIds
      const sourceNode = nodes.find((n) => n.id === edge.source);
      const targetNode = nodes.find((n) => n.id === edge.target);
      
      // Build edge data with source and target fragment IDs
      const edgeDataWithFragments = {
        ...(edge.data || {}),
        sourceFragmentId: sourceNode?.data?.fragmentId || edge.source,
        targetFragmentId: targetNode?.data?.fragmentId || edge.target,
        edgeType: edge.data?.edgeType || edge.label,
      };
      
      onEdgeSelect(edge.id, edgeDataWithFragments);
    }
  }, [onEdgeSelect, nodes, layoutMode, normalizeEdgeType, selectedLevelsAggGroupId, selectedLevelsAggEdgeType, fragmentProfileActiveNodeId]);

  // Handle pane click (click on empty space) - clear selection
  const handlePaneClick = useCallback(() => {
    if (Date.now() < ignorePaneClicksUntilRef.current) {
      return;
    }
    if (import.meta.env.DEV) console.log('[Cockpit] Pane clicked - clearing selection');
    // Clear hover states to prevent "sticky" hover behavior
    pendingHoverRef.current = { hoveredGroupId: null, hoveredNodeId: null, hoveredEdgeId: null };
    setHoveredGroupId(null);
    setHoveredNodeId(null);
    setHoveredEdgeId(null);
    // Risk Shield: pane click closes it.
    if (fragmentProfileActiveNodeId) setFragmentProfileActiveNodeId(null);
    if (onNodeSelect) {
      onNodeSelect(null, null);
    }
    if (onEdgeSelect) {
      onEdgeSelect(null, null);
    }
    setSelectedEdgeId(null);
    setSelectedLevelsAggGroupId(null);
    setSelectedLevelsAggEdgeType(null);
    setSelectedLevelsAggKey(null);
    // Also clear graph-query highlight (UX: click elsewhere returns to normal view)
    // Clear stack preview overlay (Levels).
    if (stackPreviewControllerIdRef.current) {
      const prevId = stackPreviewControllerIdRef.current;
      stackPreviewControllerIdRef.current = null;
      setNodes((prev) =>
        (prev || []).map((n) => {
          if (String((n as any)?.id || '').trim() !== prevId) return n;
          const d0: any = (n as any)?.data || {};
          if (!d0.stackPreviewOpen && !d0.stackPreviewItems && !d0.stackPreviewMore) return n;
          const d = { ...d0 };
          delete d.stackPreviewOpen;
          delete d.stackPreviewItems;
          delete d.stackPreviewMore;
          delete d.onStackPreviewSelect;
          delete d.onStackPreviewMore;
          return { ...(n as any), data: d } as any;
        }),
      );
    }
    if (graphQueryHighlightIds && graphQueryHighlightIds.size > 0) {
      setGraphQueryHighlightIds(null);
      setGraphQueryActiveChipId(null);
    }
    // Close any open right-side filter menus and local relation filters (Levels UI)
    setNodes((prev) =>
      prev.map((n) => {
        const d: any = (n as any)?.data;
        if (!d || d.isGroup) return n;
        if (!d.rightFiltersOpen && !d.localFiltersOpen) return n;
        const nd = { ...d };
        delete nd.rightFiltersOpen;
        delete nd.localFiltersOpen;
        return { ...(n as any), data: nd } as any;
      }),
    );
  }, [onNodeSelect, onEdgeSelect, graphQueryHighlightIds, fragmentProfileActiveNodeId]);

  const handleNodeDragStop = useCallback(async (_event: React.MouseEvent, node: Node<HorizonNodeData>) => {
    const d: any = node?.data;
    // Only re-layout when a fragment (not a group container) is moved and it has expanded children
    if (!node?.id || d?.isGroup) return;

    const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || nodes;
    const hasChildren = currentNodes.some((n) => !((n.data as any)?.isGroup) && (n.data as any)?.parentFragmentId === node.id);
    if (!hasChildren) return;

    // Linear layout is global and expensive. Dragging in Linear mode should stay snappy:
    // we do NOT auto-relayout on drag-stop; user can press Arrange when they want a reflow.
    if (isLinearLayout) return;

    try {
      const edgesNow = edgesStateRef.current || edges;

      let relayouted: Node<HorizonNodeData>[];
      let relayoutedEdges: Edge[] = edgesNow as any;

      if (isFreeformLayout) {
        relayouted = await layoutExpansionWithGroups(
          currentNodes,
          [],
          node.id,
          {
            nodeWidth: ELK_NODE_WIDTH,
            nodeHeight: ELK_NODE_HEIGHT,
            nodeSpacing: ELK_NODE_SPACING,
            layerSpacing: ELK_LAYER_SPACING,
            maxChildRows: ELK_MAX_CHILD_ROWS,
          },
        );
      } else {
        const nodesForArrange =
          isLinearLayout
            ? currentNodes.filter((n) => !(n.data as any)?.isGroup)
            : currentNodes;
        const arrangedResult = await arrangeWithGraphView(layoutMode, nodesForArrange as any, edgesNow as any);
        relayouted = arrangedResult.nodes as any;
        relayoutedEdges = arrangedResult.edges as any;
      }
      // Add onGroupPageChange callback to groups (no-op in supply chain, no groups)
      const withCallbacks = relayouted.map((n) => {
        if ((n.data as any)?.isGroup) {
          return { ...n, data: { ...n.data, onGroupPageChange: handleGroupPageChange } };
        }
        return n;
      });
      setNodes(withCallbacks);
      setEdges(relayoutedEdges as any);
    } catch (err) {
      console.warn('[Cockpit] Failed to relayout groups after drag stop', err);
    }
  }, [nodes, edges, setNodes, setEdges, handleGroupPageChange, isFreeformLayout, isLinearLayout, layoutMode, arrangeWithGraphView]);

  const confirmScopeScout = React.useCallback(async () => {
    if (!lensId) return;
    if (!scopeScoutModal?.fragmentId) return;
    const fragmentId = String(scopeScoutModal.fragmentId || '').trim();
    if (!fragmentId) return;

    setScopeScoutInFlightId(fragmentId);
    // Mark node as busy (UI)
    setNodes((prev) =>
      prev.map((n) =>
        (n.id === fragmentId || (n.data as any)?.fragmentId === fragmentId) && !((n.data as any)?.isGroup)
          ? { ...n, data: { ...(n.data as any), isScopeScouting: true } }
          : n
      )
    );

    let ok = false;
    try {
      const cp = String(scopeScoutModal.canonicalPath || '').trim();
      const isDomain = cp.startsWith('/domain/');

      if (isDomain) {
        ok = false;
      } else {
        if (!onScopeScout) return;
        const exploration =
          scopeScoutMode === 'supply_chain'
            ? ({
                domainMode: 'supply_chain',
                direction: scopeScoutDirection,
                hotPathOnly: scopeScoutHotPathOnly,
                hotPathNodeIds: [fragmentId],
                includeCapabilities: scopeScoutIncludeCapabilities,
                includeEdgeMetrics: scopeScoutIncludeEdgeMetrics,
                customQuery: String(scopeScoutCustomQuery || '').trim() || undefined,
                renderTarget: 'graph',
                ignoreFreshness: true,
              } satisfies DirectedSupplyChainExplorationRequest)
            : undefined;
        await onScopeScout({
          fragmentId,
          canonicalPath: scopeScoutModal.canonicalPath,
          maxRecursionDepth: scopeScoutDepth,
          mode: scopeScoutMode,
          specialties: specialtiesForScoutingMode(scopeScoutMode),
          exploration,
        });
        ok = true;
      }
    } catch (e) {
      console.warn('[Cockpit] scope-scout failed', e);
    } finally {
      setNodes((prev) =>
        prev.map((n) =>
          (n.id === fragmentId || (n.data as any)?.fragmentId === fragmentId) && !((n.data as any)?.isGroup)
            ? { ...n, data: { ...(n.data as any), isScopeScouting: false, scopeScoutDone: ok ? true : (n.data as any)?.scopeScoutDone } }
            : n
        )
      );
      setScopeScoutInFlightId(null);
      setScopeScoutModal(null);
    }
  }, [
    lensId,
    onScopeScout,
    scopeScoutModal,
    scopeScoutDepth,
    scopeScoutMode,
    scopeScoutDirection,
    scopeScoutHotPathOnly,
    scopeScoutIncludeCapabilities,
    scopeScoutIncludeEdgeMetrics,
    scopeScoutCustomQuery,
    setNodes,
    genCorrelationId,
  ]);

  // Scope-scout modal: Esc to close + basic focus management.
  React.useEffect(() => {
    if (!scopeScoutModal) return;

    scopeScoutPrevActiveElRef.current = document.activeElement as HTMLElement | null;
    const t = window.setTimeout(() => scopeScoutCancelBtnRef.current?.focus(), 0);

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (scopeScoutInFlightId) return;
      e.preventDefault();
      e.stopPropagation();
      setScopeScoutModal(null);
    };
    window.addEventListener('keydown', onKeyDown, true);

    return () => {
      window.clearTimeout(t);
      window.removeEventListener('keydown', onKeyDown, true);
      const prev = scopeScoutPrevActiveElRef.current;
      scopeScoutPrevActiveElRef.current = null;
      prev?.focus?.();
    };
  }, [scopeScoutModal, scopeScoutInFlightId]);

  // Ask-graph helpers
  const clearGraphQueryHighlight = React.useCallback(() => {
    setGraphQueryHighlightIds(null);
    setGraphQueryActiveChipId(null);
  }, []);

  // Ctrl+K / ⌘K opens the command palette (Search / Add).
  React.useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      const key = String((e as any)?.key || '').toLowerCase();
      const isK = key === 'k';
      if (!isK) return;
      if (!e.ctrlKey && !e.metaKey) return;
      if (e.altKey) return;
      // Shift+K is still commonly used for command palettes too; allow it.
      if (!lensId) return;

      e.preventDefault();
      e.stopPropagation();
      toggleCommandPalette();
    };
    window.addEventListener('keydown', onKeyDown, true);
    return () => window.removeEventListener('keydown', onKeyDown, true);
  }, [lensId, toggleCommandPalette]);

  // Esc clears highlight (when no overlays are open)
  React.useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (askGraphOpen || addNodeOpen || modelerOpen) return;
      if (graphQueryHighlightIds && graphQueryHighlightIds.size > 0) {
        e.preventDefault();
        clearGraphQueryHighlight();
      }
    };
    window.addEventListener('keydown', onKeyDown, true);
    return () => window.removeEventListener('keydown', onKeyDown, true);
  }, [askGraphOpen, addNodeOpen, modelerOpen, graphQueryHighlightIds, clearGraphQueryHighlight]);

  const submitAskGraph = React.useCallback(async () => {
    const q = String(askGraphQuery || '').trim();
    if (!lensId || !q || askGraphLoading) return;

    // Snapshot current visible graph (nodes/edges in ReactFlow state). Use canonical state refs.
    const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || [];
    const currentEdges: Edge[] = edgesStateRef.current || [];

    const nodesSnap = currentNodes
      .filter((n) => !((n.data as any)?.isGroup))
      .map((n: any) => ({
        id: String(n.id || '').trim(),
        label: String(n?.data?.label || '').trim() || undefined,
        type: String(n?.data?.type || '').trim() || undefined,
        canonicalPath: String(n?.data?.canonicalPath || '').trim() || undefined,
      }))
      .filter((n) => n.id);

    const nodeIdSet = new Set(nodesSnap.map((n) => n.id));
    const edgesSnap = currentEdges
      .filter((e: any) => nodeIdSet.has(String(e.source || '').trim()) && nodeIdSet.has(String(e.target || '').trim()))
      .map((e: any) => ({
        from: String(e.source || '').trim(),
        to: String(e.target || '').trim(),
        type: String(e?.data?.edgeType || e?.type || '').trim() || undefined,
        label: String(e?.data?.edgeCaption || e?.label || '').trim() || undefined,
      }));

    if (nodesSnap.length === 0) {
      setAskGraphError('Graph is empty. Load the graph first.');
      return;
    }

    setAskGraphLoading(true);
    setAskGraphError(null);
    try {
      const res = await api.queryHorizonGraph(lensId, {
        question: q,
        graph: { nodes: nodesSnap, edges: edgesSnap },
      });

      const chip: GraphQueryChip = {
        id: String(res?.interactionId || '').trim() || `local-${Date.now()}`,
        question: String(res?.question || q).trim(),
        answer: typeof res?.answer === 'string' ? res.answer.trim() : undefined,
        relevantNodeIds: Array.isArray(res?.relevantNodeIds) ? res.relevantNodeIds.map((x) => String(x || '').trim()).filter(Boolean) : [],
        findings: Array.isArray(res?.findings)
          ? (res.findings as any[]).filter(Boolean).map((f) => ({
              nodeId: String(f?.nodeId || '').trim(),
              title: String(f?.title || '').trim(),
              eventDate: String(f?.eventDate || '').trim(),
              source: String(f?.source || '').trim(),
              url: String(f?.url || '').trim(),
            }))
          : [],
        sources: Array.isArray(res?.sources) ? res.sources.map((s) => String(s || '').trim()).filter(Boolean) : [],
        searchQueries: Array.isArray(res?.searchQueries) ? res.searchQueries.map((s) => String(s || '').trim()).filter(Boolean) : [],
        createdAt: Date.now(),
      };

      setGraphQueryChips((prev) => {
        const next = [chip, ...prev.filter((c) => c.id !== chip.id)];
        return next.slice(0, 5);
      });

      // Apply highlight immediately.
      const ids = chip.relevantNodeIds;
      if (ids.length > 0) {
        setGraphQueryHighlightIds(new Set(ids));
        setGraphQueryActiveChipId(chip.id);
      } else {
        clearGraphQueryHighlight();
      }

      setAskGraphOpen(false);
      setAskGraphQuery('');
    } catch (e: any) {
      const msg =
        String(e?.response?.data?.message || e?.response?.data?.error || e?.message || 'Failed to query graph').trim();
      setAskGraphError(msg);
    } finally {
      setAskGraphLoading(false);
    }
  }, [askGraphQuery, askGraphLoading, lensId, clearGraphQueryHighlight]);

  const runControlCorridor = React.useCallback(async (seedIdOverride?: string): Promise<{ graph: ControlCorridorGraph; seedId: string } | null> => {
    if (!lensId || controlCorridorLoading) return null;
    const seedId =
      String(seedIdOverride || '').trim() ||
      String(controlCorridorSeedId || '').trim() ||
      String(controlCorridorSeedNodeId || '').trim();
    if (!seedId) {
      setControlCorridorError('Expand a node to build a focused corridor.');
      setControlCorridorGraph(null);
      setControlCorridorSeedId('');
      return null;
    }
    setControlCorridorLoading(true);
    setControlCorridorError(null);
    try {
      const response = await api.queryHorizonControlCorridor(lensId, {
        seedFragmentIds: [seedId],
        maxNodes: 96,
        maxEdges: 140,
      });
      const nodesSafe = Array.isArray(response?.nodes) ? response.nodes : [];
      const edgesSafe = Array.isArray(response?.edges) ? response.edges : [];
      const normalizedNodes = nodesSafe.map((n: any) => ({
        id: String(n?.id || '').trim(),
        label: String(n?.label || '').trim(),
        type: typeof n?.type === 'string' ? n.type : undefined,
        canonicalPath: typeof n?.canonicalPath === 'string' ? n.canonicalPath : undefined,
        lane: String(n?.lane || '').trim() || 'intermediary',
        depth: Number.isFinite(Number(n?.depth)) ? Number(n.depth) : 0,
        seed: Boolean(n?.seed),
      })).filter((n) => n.id);
      const normalizedEdges = edgesSafe.map((e: any) => ({
        id: String(e?.id || '').trim(),
        from: String(e?.from || '').trim(),
        to: String(e?.to || '').trim(),
        type: String(e?.type || '').trim(),
        stage: String(e?.stage || '').trim() || 'mechanism',
        role: String(e?.role || '').trim() || undefined,
        criticality: Number.isFinite(Number(e?.criticality)) ? Number(e.criticality) : undefined,
      })).filter((e) => e.from && e.to);
      const bundlesRaw = response?.bundles && typeof response.bundles === 'object' ? response.bundles : undefined;
      const normalizeScoredNodes = (arr: any): ControlCorridorScoredNode[] => (
        Array.isArray(arr)
          ? arr
              .map((n: any) => ({
                id: String(n?.id || '').trim(),
                label: String(n?.label || '').trim(),
                type: typeof n?.type === 'string' ? n.type : undefined,
                canonicalPath: typeof n?.canonicalPath === 'string' ? n.canonicalPath : undefined,
                score: Number.isFinite(Number(n?.score)) ? Number(n.score) : undefined,
                coverage: Number.isFinite(Number(n?.coverage)) ? Number(n.coverage) : undefined,
              }))
              .filter((n) => n.id)
          : []
      );
      const normalizedBundles = bundlesRaw
        ? {
            controllers: normalizeScoredNodes((bundlesRaw as any).controllers),
            decisionPoints: normalizeScoredNodes((bundlesRaw as any).decisionPoints),
            mechanisms: normalizeScoredNodes((bundlesRaw as any).mechanisms),
            mediators: normalizeScoredNodes((bundlesRaw as any).mediators),
            impactedTargets: normalizeScoredNodes((bundlesRaw as any).impactedTargets),
            paths: Array.isArray((bundlesRaw as any).paths)
              ? (bundlesRaw as any).paths
                  .map((p: any) => ({
                    id: String(p?.id || '').trim(),
                    controllerId: String(p?.controllerId || '').trim(),
                    targetId: String(p?.targetId || '').trim(),
                    mediatorId: String(p?.mediatorId || '').trim() || undefined,
                    nodeIds: Array.isArray(p?.nodeIds) ? p.nodeIds.map((x: any) => String(x || '').trim()).filter(Boolean) : [],
                    edgeIds: Array.isArray(p?.edgeIds) ? p.edgeIds.map((x: any) => String(x || '').trim()).filter(Boolean) : [],
                    edgeKeys: Array.isArray(p?.edgeKeys) ? p.edgeKeys.map((x: any) => String(x || '').trim()).filter(Boolean) : [],
                    coverageScore: Number.isFinite(Number(p?.coverageScore)) ? Number(p.coverageScore) : undefined,
                    pathType: typeof p?.pathType === 'string' ? p.pathType : undefined,
                    description: typeof p?.description === 'string' ? p.description : undefined,
                  }))
                  .filter((p: ControlCorridorPathBundle) => p.id && p.controllerId && p.targetId)
              : [],
            bottlenecks: (bundlesRaw as any).bottlenecks && typeof (bundlesRaw as any).bottlenecks === 'object'
              ? {
                  nodes: Array.isArray((bundlesRaw as any).bottlenecks?.nodes)
                    ? (bundlesRaw as any).bottlenecks.nodes
                        .map((n: any) => ({
                          id: String(n?.id || '').trim(),
                          usageCount: Number.isFinite(Number(n?.usageCount)) ? Number(n.usageCount) : undefined,
                          score: Number.isFinite(Number(n?.score)) ? Number(n.score) : undefined,
                        }))
                        .filter((n: any) => n.id)
                    : [],
                  edges: Array.isArray((bundlesRaw as any).bottlenecks?.edges)
                    ? (bundlesRaw as any).bottlenecks.edges
                        .map((e: any) => ({
                          id: String(e?.id || '').trim(),
                          from: String(e?.from || '').trim(),
                          to: String(e?.to || '').trim(),
                          usageCount: Number.isFinite(Number(e?.usageCount)) ? Number(e.usageCount) : undefined,
                        }))
                        .filter((e: any) => e.id && e.from && e.to)
                    : [],
                }
              : undefined,
          }
        : undefined;
      const responseSeedId = String((Array.isArray(response?.seedFragmentIds) ? response.seedFragmentIds[0] : '') || '').trim() || seedId;
      if (import.meta.env.DEV) {
        const laneCounts: Record<string, number> = {};
        for (const n of normalizedNodes) {
          const lane = String(n.lane || '').trim().toLowerCase() || 'intermediary';
          laneCounts[lane] = (laneCounts[lane] || 0) + 1;
        }
        const stageCounts: Record<string, number> = {};
        for (const e of normalizedEdges) {
          const stage = String(e.stage || '').trim().toLowerCase() || 'mechanism';
          stageCounts[stage] = (stageCounts[stage] || 0) + 1;
        }
        const uniqueNodeIds = new Set(normalizedNodes.map((n) => n.id));
        let edgesWithMissingNode = 0;
        for (const e of normalizedEdges) {
          if (!uniqueNodeIds.has(e.from) || !uniqueNodeIds.has(e.to)) edgesWithMissingNode++;
        }
        console.groupCollapsed('[Corridor][API] aggregated');
        console.log({
          lensId,
          requestSeedId: seedId,
          responseSeedId,
          summary: response?.summary || null,
          warnings: Array.isArray(response?.warnings) ? response.warnings : [],
          nodeCount: normalizedNodes.length,
          edgeCount: normalizedEdges.length,
          laneCounts,
          stageCounts,
          edgesWithMissingNode,
          bundleCounts: {
            controllers: normalizedBundles?.controllers?.length || 0,
            decisionPoints: normalizedBundles?.decisionPoints?.length || 0,
            mechanisms: normalizedBundles?.mechanisms?.length || 0,
            mediators: normalizedBundles?.mediators?.length || 0,
            impactedTargets: normalizedBundles?.impactedTargets?.length || 0,
            paths: normalizedBundles?.paths?.length || 0,
          },
        });
        console.groupEnd();
      }
      const graphPayload: ControlCorridorGraph = {
        lensId: String(response?.lensId || lensId).trim(),
        seedFragmentIds: Array.isArray(response?.seedFragmentIds) ? response.seedFragmentIds : [],
        nodes: normalizedNodes,
        edges: normalizedEdges,
        bundles: normalizedBundles,
        summary: response?.summary ? { ...response.summary } : undefined,
        warnings: Array.isArray(response?.warnings) ? response.warnings.map((w: any) => String(w || '').trim()).filter(Boolean) : [],
      };
      setControlCorridorGraph(graphPayload);
      setControlCorridorSeedId(responseSeedId);
      return { graph: graphPayload, seedId: responseSeedId };
    } catch (e: any) {
      const msg = String(e?.response?.data?.message || e?.response?.data?.error || e?.message || 'Failed to build control corridor').trim();
      setControlCorridorError(msg || 'Failed to build control corridor');
      setControlCorridorGraph(null);
      setControlCorridorSeedId('');
      return null;
    } finally {
      setControlCorridorLoading(false);
    }
  }, [lensId, controlCorridorLoading, controlCorridorSeedId, controlCorridorSeedNodeId]);
  runControlCorridorRef.current = runControlCorridor;

  const truncateChip = React.useCallback((s: string, max: number) => {
    const t = String(s || '').trim();
    if (!t) return '';
    return t.length > max ? `${t.slice(0, max)}…` : t;
  }, []);

  const toggleGraphQueryChip = React.useCallback(
    (chip: GraphQueryChip) => {
      const id = String(chip?.id || '').trim();
      if (!id) return;
      if (graphQueryActiveChipId === id) {
        clearGraphQueryHighlight();
        return;
      }
      const ids = Array.isArray(chip.relevantNodeIds) ? chip.relevantNodeIds : [];
      if (ids.length === 0) {
        clearGraphQueryHighlight();
        setGraphQueryActiveChipId(id);
        return;
      }
      setGraphQueryHighlightIds(new Set(ids));
      setGraphQueryActiveChipId(id);
    },
    [graphQueryActiveChipId, clearGraphQueryHighlight],
  );

  // Toolbar components
  const IconButton = ({ onClick, active, disabled, title, icon, label }: { onClick: () => void; active?: boolean; disabled?: boolean; title: string; icon: React.ReactNode; label?: string }) => (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={title}
      aria-label={title}
      style={{
        background: active ? (theme === 'dark' ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.06)') : 'transparent',
        border: 'none',
        borderRadius: 8,
        padding: label ? '6px 10px' : '6px',
        cursor: disabled ? 'not-allowed' : 'pointer',
        color: active ? 'var(--text-primary)' : 'var(--text-secondary)',
        opacity: disabled ? 0.4 : 1,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 6,
        transition: 'all 0.2s ease',
        fontSize: 12,
        fontWeight: 600,
      }}
    >
      {icon}
      {label && <span>{label}</span>}
    </button>
  );

  const Icons = {
    Refresh: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" /><path d="M3 3v5h5" /><path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16" /><path d="M16 21h5v-5" /></svg>,
    Search: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" /></svg>,
    Arrange: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>,
    Focus: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M15 3h6v6"/><path d="M9 21H3v-6"/><path d="M21 3l-7 7"/><path d="M3 21l7-7"/></svg>,
    Message: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round"><path d="M21 12a8.5 8.5 0 0 1-8.5 8.5h-5L3 22l1.5-4.5v-5A8.5 8.5 0 1 1 21 12Z" /></svg>,
    Sliders: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><line x1="4" y1="21" x2="4" y2="14" /><line x1="4" y1="10" x2="4" y2="3" /><line x1="12" y1="21" x2="12" y2="12" /><line x1="12" y1="8" x2="12" y2="3" /><line x1="20" y1="21" x2="20" y2="16" /><line x1="20" y1="12" x2="20" y2="3" /><line x1="1" y1="14" x2="7" y2="14" /><line x1="9" y1="8" x2="15" y2="8" /><line x1="17" y1="16" x2="23" y2="16" /></svg>,
    Dots: <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><circle cx="5" cy="12" r="1.8" /><circle cx="12" cy="12" r="1.8" /><circle cx="19" cy="12" r="1.8" /></svg>,
    Plus: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 5v14" /><path d="M5 12h14" /></svg>,
    X: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M18 6 6 18" /><path d="M6 6l12 12" /></svg>,
    Download: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>,
    Zap: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>,
    Map: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="1 6 1 22 8 18 16 22 23 18 23 2 16 6 8 2 1 6"/><line x1="8" y1="2" x2="8" y2="18"/><line x1="16" y1="6" x2="16" y2="22"/></svg>,
    Grid: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>,
    Layout: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><line x1="9" y1="3" x2="9" y2="21"/><line x1="15" y1="3" x2="15" y2="21"/></svg>,
    LayoutFreeform: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><circle cx="18" cy="5" r="2.4"/><circle cx="6" cy="12" r="2.4"/><circle cx="18" cy="19" r="2.4"/><path d="M8.1 11.2 15.8 6.2"/><path d="M8.1 12.8 15.8 17.8"/></svg>,
    LayoutLinear: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="4" width="6" height="16" rx="2"/><rect x="15" y="4" width="6" height="16" rx="2"/><path d="M10.5 12h3"/></svg>,
    LayoutLevels: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M4 7h16"/><path d="M4 12h16"/><path d="M4 17h16"/><circle cx="8" cy="7" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="16" cy="17" r="1.6"/></svg>,
    LayoutSupplyChain: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><circle cx="5" cy="12" r="2.4"/><circle cx="12" cy="12" r="2.4"/><circle cx="19" cy="12" r="2.4"/><path d="M7.4 12h2.2"/><path d="M14.4 12h2.2"/></svg>,
    LayoutInfluence: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="2.3"/><circle cx="5" cy="7" r="1.8"/><circle cx="19" cy="7" r="1.8"/><circle cx="6" cy="18" r="1.8"/><circle cx="18" cy="18" r="1.8"/><path d="M10.2 10.8 6.6 8.1"/><path d="M13.8 10.8 17.4 8.1"/><path d="M10.3 13.3 7.2 16.4"/><path d="M13.7 13.3 16.8 16.4"/></svg>,
    LayoutValueChain: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round"><rect x="2.5" y="5" width="4" height="14" rx="1.5"/><rect x="9.5" y="8" width="4" height="11" rx="1.5"/><rect x="16.5" y="11" width="4" height="8" rx="1.5"/><path d="M6.5 12h3"/><path d="M13.5 14h3"/></svg>,
    ChevronDown: <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="6 9 12 15 18 9"/></svg>,
    Info: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><path d="M12 16v-4" /><path d="M12 8h.01" /></svg>,
    Target: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="6"/><circle cx="12" cy="12" r="2"/></svg>,
    Layers: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 17 12 22 22 17"/></svg>,
    Palette: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="13.5" cy="6.5" r="1.5"/><circle cx="17.5" cy="10.5" r="1.5"/><circle cx="8.5" cy="7.5" r="1.5"/><circle cx="6.5" cy="12.5" r="1.5"/><path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.648 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668c1.105 0 2.059-.215 2.98-.643 2.02-1.008 3.48-3.157 3.48-5.75 0-4.5-4.5-8.5-10-8.5z"/></svg>,
    Filter: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3"/></svg>,
  };

  const layoutModeLabel = React.useCallback((m: GraphViewId): string => {
    return GRAPH_VIEW_LABELS[m] || 'Layout';
  }, []);

  const layoutModeIcon = React.useCallback((m: GraphViewId) => {
    switch (m) {
      case 'freeform':
        return Icons.LayoutFreeform;
      case 'linear':
        return Icons.LayoutLinear;
      case 'levels':
      case 'hierarchical':
        return Icons.LayoutLevels;
      case 'supply-chain':
      case 'supply-flow':
        return Icons.LayoutSupplyChain;
      case 'influence-network':
        return Icons.LayoutInfluence;
      case 'value-chain':
        return Icons.LayoutValueChain;
      default:
        return Icons.Layout;
    }
  }, []);

  const hoverModeLabel = React.useCallback((m: GraphViewHoverMode): string => {
    switch (m) {
      case 'off':
        return 'Off';
      case 'control':
        return 'Control';
      case 'expansion':
        return 'Expansion';
      case 'production':
        return 'Produces';
      case 'supplies':
        return 'Supplies';
      case 'competition':
        return 'Competes';
      case 'decision-chain':
        return 'Decision Chain';
      case 'signal-propagation':
        return 'Signal propagation';
      default:
        return 'Highlight';
    }
  }, []);

  const setHoverMode = React.useCallback((m: GraphViewHoverMode) => {
    setHoverHighlightMode(m);
    setVisualPreset('custom');
  }, []);

  React.useEffect(() => {
    const allowed = new Set(graphViewToolbarPolicy.highlightModes.map((mode) => mode.key));
    if (allowed.has(hoverHighlightMode)) return;
    const fallback = graphViewToolbarPolicy.highlightModes[0]?.key || 'off';
    setHoverHighlightMode(fallback);
  }, [graphViewToolbarPolicy, hoverHighlightMode]);

  React.useEffect(() => {
    if (!graphViewStateLoadedRef.current || !currentSemanticGraphView) return;
    const saved = (graphViewState[currentSemanticGraphView] || defaultGraphViewState(currentSemanticGraphView)) as Record<string, any>;
    if (typeof saved.hoverMode === 'string') setHoverHighlightMode(saved.hoverMode as GraphViewHoverMode);
    if (typeof saved.focusMode === 'boolean') setFocusMode(saved.focusMode);
    if (typeof saved.showSecondaryEdges === 'boolean') setShowSecondaryEdges(saved.showSecondaryEdges);
    if (saved.edgeViewFilter === 'all' || (Array.isArray(saved.edgeViewFilter) && saved.edgeViewFilter.length > 0)) {
      setEdgeViewFilter(saved.edgeViewFilter as any);
    } else if (!graphViewFilterPolicy.semanticGroupFilter) {
      setEdgeViewFilter('all');
    }
  }, [currentSemanticGraphView, graphViewState, graphViewFilterPolicy.semanticGroupFilter]);

  React.useEffect(() => {
    if (!graphViewStateLoadedRef.current || !currentSemanticGraphView) return;
    setGraphViewState((prev) => {
      const base = normalizeGraphViewState(prev);
      const current = {
        ...(base[currentSemanticGraphView] || defaultGraphViewState(currentSemanticGraphView)),
        hoverMode: hoverHighlightMode,
        focusMode,
        showSecondaryEdges,
        edgeViewFilter,
      };
      return {
        ...base,
        [currentSemanticGraphView]: current,
      };
    });
  }, [currentSemanticGraphView, hoverHighlightMode, focusMode, showSecondaryEdges, edgeViewFilter]);

  const graphEmpty = nodes.length === 0;

  // Clear hover when overlays open to prevent "sticky" hover and ensure graph receives events after overlay closes
  const clearHoverWhenOverlayOpens = React.useCallback(() => {
    pendingHoverRef.current = { hoveredGroupId: null, hoveredNodeId: null, hoveredEdgeId: null };
    setHoveredGroupId(null);
    setHoveredNodeId(null);
    setHoveredEdgeId(null);
  }, []);

  const overlayPanelStyle: React.CSSProperties = {
    padding: '8px 10px',
    borderRadius: 12,
    border: '1px solid var(--border-subtle)',
    background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.65)',
    backdropFilter: 'blur(8px)',
    boxShadow: theme === 'dark' ? '0 8px 20px rgba(0,0,0,0.35)' : '0 8px 20px rgba(0,0,0,0.12)',
    overflow: 'visible',
    minWidth: 120,
    maxWidth: 'min(90vw, 720px)',
    userSelect: 'none',
  };

  const pushableButtonStyle = (active: boolean): React.CSSProperties => ({
    width: 40,
    height: 40,
    borderRadius: 10,
    border: active ? '1px solid rgba(245, 158, 11, 0.5)' : '1px solid var(--border-subtle)',
    background: active
      ? (theme === 'dark' ? 'rgba(245, 158, 11, 0.18)' : 'rgba(245, 158, 11, 0.14)')
      : (theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.65)'),
    color: 'var(--text-heading)',
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
    transition: 'background 0.15s ease, border-color 0.15s ease',
  });

  const actionsOverlayContent = (
    <div
      onClick={(e) => { if (e.target === e.currentTarget) setActionsOverlayOpen(false); }}
      style={{ ...overlayPanelStyle, display: 'flex', flexDirection: 'row', alignItems: 'center', flexWrap: 'nowrap', gap: 6, overflow: 'visible', whiteSpace: 'nowrap', maxWidth: 'none' }}
    >
        <IconButton onClick={handleLoadGraph} disabled={loading || !lensId} title="Load graph" icon={Icons.Refresh} />
        <IconButton
          onClick={() => { setActionsOverlayOpen(false); toggleCommandPalette(); }}
          disabled={!lensId}
          active={addNodeOpen}
          title={addNodeOpen ? 'Close search (Ctrl+K)' : 'Search / add (Ctrl+K)'}
          icon={Icons.Search}
        />
        {!graphEmpty && (
          <>
            <IconButton onClick={handleArrange} disabled={arranging} title="Arrange graph" icon={Icons.Arrange} />
            <IconButton onClick={handleFitView} title="Fit view" icon={Icons.Focus} />
            <IconButton
              onClick={() => { setActionsOverlayOpen(false); handleDownload(); }}
              disabled={edges.length === 0}
              title="Download graph"
              icon={Icons.Download}
            />
          </>
        )}
    </div>
  );

  const viewOverlayContent = !graphEmpty ? (
    <div
      onClick={(e) => { if (e.target === e.currentTarget) setViewOverlayOpen(false); }}
      style={{
        ...overlayPanelStyle,
        display: 'flex',
        flexDirection: 'row',
        alignItems: 'center',
        flexWrap: 'nowrap',
        gap: 6,
        overflow: 'visible',
        whiteSpace: 'nowrap',
        maxWidth: 'none',
      }}
    >
      {/* Modes */}
      <div ref={layoutMenuRef} style={{ position: 'relative', display: 'inline-block' }}>
        <button
          type="button"
          onClick={() => {
            setShowLayoutMenu((v) => !v);
            setShowHoverModeMenu(false);
            setShowLevelsExpandMenu(false);
            setShowToolbarMenu(false);
          }}
          title={`Layout: ${layoutModeLabel(layoutMode)}`}
          aria-label="Layout mode"
          aria-expanded={showLayoutMenu}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '6px 10px',
            borderRadius: 8,
            border: '1px solid var(--border-subtle)',
            background: theme === 'dark' ? 'rgba(0,0,0,0.18)' : 'rgba(255,255,255,0.40)',
            color: 'var(--text-heading)',
            cursor: 'pointer',
            fontSize: 12,
            fontWeight: 600,
            lineHeight: 1,
          }}
        >
          <span style={{ display: 'inline-flex' }}>{layoutModeIcon(layoutMode)}</span>
          <span>{layoutModeLabel(layoutMode)}</span>
          <span style={{ color: 'var(--text-secondary-strong)' }}>{Icons.ChevronDown}</span>
        </button>

        {showLayoutMenu && (
          <div
            style={{
              position: 'absolute',
              top: 'calc(100% + 6px)',
              right: 0,
              minWidth: 220,
              borderRadius: 12,
              border: '1px solid var(--border-subtle)',
              background: 'var(--bg-elevated)',
              boxShadow: theme === 'dark' ? '0 14px 40px rgba(0,0,0,0.55)' : '0 14px 40px rgba(0,0,0,0.18)',
              overflow: 'hidden',
              zIndex: 1000,
            }}
          >
            <div style={{ padding: '10px 12px', fontSize: 11, color: 'var(--text-secondary-strong)', borderBottom: '1px solid var(--border-subtle)' }}>
              Layout
            </div>
            {(
              [
                { key: 'freeform', label: 'Freeform' },
                { key: 'linear', label: 'Linear' },
                { key: 'hierarchical', label: 'Hierarchical' },
                { type: 'separator' as const },
                { key: 'supply-flow', label: 'Supply Flow' },
                { key: 'influence-network', label: 'Influence Network' },
                { key: 'value-chain', label: 'Value Chain' },
              ] as Array<{ key?: GraphViewId; label?: string; type?: 'separator' }>
            ).map((opt, idx) => {
              if ((opt as any).type === 'separator') {
                return <div key={`sep-${idx}`} style={{ height: 1, background: 'var(--border-subtle)', margin: '6px 0' }} />;
              }
              const active = layoutMode === (opt as any).key;
              const key = (opt as any).key;
              return (
                <button
                  key={key}
                  type="button"
                  onClick={() => {
                    setLayoutMode(key);
                    setShowLayoutMenu(false);
                  }}
                  style={{
                    width: '100%',
                    padding: '10px 12px',
                    border: 'none',
                    background: active ? 'var(--white-10)' : 'transparent',
                    color: 'var(--text-heading)',
                    cursor: 'pointer',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    gap: 10,
                    textAlign: 'left',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
                    <span style={{ color: 'var(--text-secondary-strong)', display: 'inline-flex' }}>
                      {layoutModeIcon(key)}
                    </span>
                    <span style={{ fontSize: 12, fontWeight: 800 }}>{(opt as any).label}</span>
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>{active ? '✓' : ''}</div>
                </button>
              );
            })}
          </div>
        )}
      </div>

      <div ref={hoverModeMenuRef} style={{ position: 'relative', display: 'inline-block' }}>
        <button
          type="button"
          onClick={() => {
            setShowHoverModeMenu((v) => !v);
            setShowLayoutMenu(false);
            setShowToolbarMenu(false);
            setShowLevelsExpandMenu(false);
          }}
          title="Highlight mode"
          aria-expanded={showHoverModeMenu}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '6px 10px',
            borderRadius: 8,
            border: '1px solid var(--border-subtle)',
            background: theme === 'dark' ? 'rgba(0,0,0,0.18)' : 'rgba(255,255,255,0.40)',
            color: 'var(--text-heading)',
            cursor: 'pointer',
            fontSize: 12,
            fontWeight: 600,
            lineHeight: 1,
          }}
        >
          <span style={{ color: 'var(--text-secondary-strong)' }}>Highlight</span>
          <span>{hoverModeLabel(hoverHighlightMode)}</span>
          <span style={{ color: 'var(--text-secondary-strong)' }}>{Icons.ChevronDown}</span>
        </button>
        
        {showHoverModeMenu && (
            <div
          style={{
              position: 'absolute',
              top: 'calc(100% + 6px)',
                right: 0,
              minWidth: 220,
              borderRadius: 12,
              border: '1px solid var(--border-subtle)',
              background: 'var(--bg-elevated)',
              boxShadow: theme === 'dark' ? '0 14px 40px rgba(0,0,0,0.55)' : '0 14px 40px rgba(0,0,0,0.18)',
              overflow: 'hidden',
              zIndex: 1000,
            }}
          >
            <div style={{ padding: '10px 12px', fontSize: 11, color: 'var(--text-secondary-strong)', borderBottom: '1px solid var(--border-subtle)' }}>
              Highlight
            </div>
            {graphViewToolbarPolicy.highlightModes.map((opt) => {
              const active = hoverHighlightMode === opt.key;
              return (
                <button
                  key={opt.key}
                  type="button"
                  onClick={() => {
                    setHoverMode(opt.key);
                    setShowHoverModeMenu(false);
                  }}
                  style={{
                    width: '100%',
                    padding: '10px 12px',
                    border: 'none',
                    background: active ? 'var(--white-10)' : 'transparent',
                    color: 'var(--text-heading)',
                    cursor: 'pointer',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    gap: 10,
                    textAlign: 'left',
                  }}
                >
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                    <div style={{ fontSize: 12, fontWeight: 800 }}>
                      {opt.label}
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary)' }}>
                      {opt.hint}
                    </div>
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                    {active ? '✓' : ''}
                  </div>
                </button>
              );
            })}
          </div>
        )}
      </div>

      {hoverHighlightMode === 'signal-propagation' && (
        <div
          style={{
            padding: '6px 10px',
            borderRadius: 8,
            border: '1px solid rgba(239,68,68,0.30)',
            background: theme === 'dark' ? 'rgba(127,29,29,0.28)' : 'rgba(254,226,226,0.85)',
            color: theme === 'dark' ? 'rgba(254,202,202,0.94)' : 'rgba(127,29,29,0.92)',
            fontSize: 11,
            fontWeight: 700,
            lineHeight: 1.1,
            maxWidth: 280,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
          }}
          title={
            signalPropagationLoading
              ? 'Running backend propagation preview from selected node'
              : signalPropagationError
                ? signalPropagationError
                : signalPropagationOverlay
                  ? `${signalPropagationOverlay.nodeIds.length} nodes, ${signalPropagationOverlay.edgeIds.length} edges, ${signalPropagationOverlay.traceSteps} trace steps`
                  : 'No visible propagation path'
          }
        >
          {signalPropagationLoading
            ? 'Signal propagation: running…'
            : signalPropagationError
              ? `Signal propagation: ${signalPropagationError}`
              : signalPropagationOverlay
                ? `Signal propagation: ${signalPropagationOverlay.nodeIds.length} nodes · ${signalPropagationOverlay.edgeIds.length} edges`
                : 'Signal propagation: no visible path'}
        </div>
      )}

      {graphViewOverlayPolicy.showExpansionModeMenu && (
        <div ref={levelsExpandMenuRef} style={{ position: 'relative', display: 'inline-block' }}>
          <button
            type="button"
            onClick={() => {
              setShowLevelsExpandMenu((v) => !v);
              setShowLayoutMenu(false);
              setShowHoverModeMenu(false);
              setShowToolbarMenu(false);
            }}
            title="Expansion filter (edge types fetched on expand)"
            aria-expanded={showLevelsExpandMenu}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              padding: '6px 10px',
              borderRadius: 8,
              border: expandModeGlow ? '1px solid rgba(245, 158, 11, 0.6)' : '1px solid var(--border-subtle)',
              background: theme === 'dark' ? 'rgba(0,0,0,0.18)' : 'rgba(255,255,255,0.40)',
              color: 'var(--text-heading)',
              cursor: 'pointer',
              fontSize: 12,
              fontWeight: 600,
              lineHeight: 1,
              boxShadow: expandModeGlow
                ? '0 0 12px rgba(245, 158, 11, 0.4), 0 0 24px rgba(245, 158, 11, 0.2)'
                : 'none',
              transition: 'box-shadow 0.4s ease, border-color 0.4s ease',
            }}
          >
            <span style={{ color: 'var(--text-secondary-strong)' }}>Expand</span>
            <span>{levelsExpandLabel(levelsExpandMode)}</span>
            <span style={{ color: 'var(--text-secondary-strong)' }}>{Icons.ChevronDown}</span>
          </button>

          {showLevelsExpandMenu && (
            <div
              style={{
                position: 'absolute',
                top: 'calc(100% + 6px)',
                right: 0,
                minWidth: 240,
                maxHeight: 'calc(100vh - 200px)',
                overflowY: 'auto',
                borderRadius: 12,
                border: '1px solid var(--border-subtle)',
                background: 'var(--bg-elevated)',
                boxShadow: theme === 'dark' ? '0 14px 40px rgba(0,0,0,0.55)' : '0 14px 40px rgba(0,0,0,0.18)',
                zIndex: 1000,
              }}
            >
              <div
                style={{
                  padding: '10px 12px',
                  fontSize: 11,
                  color: 'var(--text-secondary-strong)',
                  borderBottom: '1px solid var(--border-subtle)',
                }}
              >
                Expansion filter
              </div>
              {(
                [
                  { key: 'all', label: 'Full network', hint: 'Complete relationship map' },
                  { key: 'ownership', label: 'Control', hint: 'Holdings, subsidiaries, and investment portfolio' },
                  { key: 'expansion', label: 'Expansion', hint: 'Control chains + partners' },
                  { key: 'dependencies', label: 'Supply chain', hint: 'Upstream suppliers and critical inputs' },
                  { key: 'context', label: 'Operating footprint', hint: 'Markets, jurisdictions, and regulatory environment' },
                  { key: 'market', label: 'Competitive landscape', hint: 'Competitors, partners, and market positioning' },
                  { key: 'influence', label: 'Key stakeholders', hint: 'Decision-makers and influencing entities' },
                  { key: 'balanced', label: 'Strategic overview', hint: 'Structure, footprint, and competitive position' },
                  { key: 'decision-chain', label: 'Decision Chain', hint: 'On expand: build focused authority → decision → mechanism → impact group' },
                ] as Array<{ key: LevelsExpandMode; label: string; hint: string }>
              ).map((opt) => {
                const active = levelsExpandMode === opt.key;
                return (
                  <button
                    key={opt.key}
                    type="button"
                    onClick={() => {
                      setLevelsExpandMode(opt.key);
                      setShowLevelsExpandMenu(false);
                    }}
                    style={{
                      width: '100%',
                      padding: '10px 12px',
                      border: 'none',
                      background: active ? 'var(--white-10)' : 'transparent',
                      color: 'var(--text-heading)',
                      cursor: 'pointer',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      gap: 10,
                      textAlign: 'left',
                    }}
                  >
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                      <div style={{ fontSize: 12, fontWeight: 800 }}>{opt.label}</div>
                      <div style={{ fontSize: 11, color: 'var(--text-secondary)' }}>{opt.hint}</div>
                    </div>
                    <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>{active ? '✓' : ''}</div>
                  </button>
                );
              })}
            </div>
          )}
        </div>
      )}

      <div style={{ width: 1, minHeight: 24, background: 'var(--border-subtle)', margin: '0 4px' }} />

      {/* Graph clarity */}
      {graphViewToolbarPolicy.showFocusMode && (
        <IconButton
          onClick={() => setFocusMode((v) => !v)}
          active={focusMode}
          title={focusMode ? 'Focus mode: dim non-selected edges when a node is selected' : 'Focus mode: click to enable'}
          icon={Icons.Target}
        />
      )}
      {graphViewFilterPolicy.semanticGroupFilter && (
      <div ref={edgeViewMenuRef} style={{ position: 'relative', display: 'inline-block' }}>
        <button
          type="button"
          onClick={() => {
            setShowEdgeViewMenu((v) => !v);
            setShowLayoutMenu(false);
            setShowHoverModeMenu(false);
            setShowLevelsExpandMenu(false);
          }}
          title="Filter edges by relation type"
          aria-expanded={showEdgeViewMenu}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '6px 10px',
            borderRadius: 8,
            border: '1px solid var(--border-subtle)',
            background: theme === 'dark' ? 'rgba(0,0,0,0.18)' : 'rgba(255,255,255,0.40)',
            color: 'var(--text-heading)',
            cursor: 'pointer',
            fontSize: 12,
            fontWeight: 600,
            lineHeight: 1,
          }}
        >
          <span style={{ display: 'inline-flex' }}>{Icons.Layers}</span>
          <span>
            {edgeViewFilter === 'all'
              ? 'All relations'
              : edgeViewFilter.length === 1
                ? SEMANTIC_GROUP_LABELS[edgeViewFilter[0]!]
                : `${edgeViewFilter.length} groups`}
          </span>
          <span style={{ color: 'var(--text-secondary-strong)' }}>{Icons.ChevronDown}</span>
        </button>
        {showEdgeViewMenu && (
          <div
            style={{
              position: 'absolute',
              top: 'calc(100% + 6px)',
              right: 0,
              minWidth: 200,
              borderRadius: 12,
              border: '1px solid var(--border-subtle)',
              background: 'var(--bg-elevated)',
              boxShadow: theme === 'dark' ? '0 14px 40px rgba(0,0,0,0.55)' : '0 14px 40px rgba(0,0,0,0.18)',
              overflow: 'hidden',
              zIndex: 1000,
            }}
          >
            <div style={{ padding: '10px 12px', fontSize: 11, color: 'var(--text-secondary-strong)', borderBottom: '1px solid var(--border-subtle)' }}>
              Show relations
            </div>
            {[
              { key: 'all' as const, label: 'All relations' },
              ...graphViewToolbarPolicy.relationFilterOptions.map((sg) => ({
                key: [sg] as SemanticGroup[],
                label: SEMANTIC_GROUP_LABELS[sg],
              })),
            ].map((opt) => {
              const active =
                opt.key === 'all'
                  ? edgeViewFilter === 'all'
                  : Array.isArray(opt.key) && edgeViewFilter !== 'all' && edgeViewFilter.length === 1 && edgeViewFilter[0] === opt.key[0];
              return (
                <button
                  key={Array.isArray(opt.key) ? opt.key[0] : opt.key}
                  type="button"
                  onClick={() => {
                    setEdgeViewFilter(opt.key === 'all' ? 'all' : opt.key);
                    setShowEdgeViewMenu(false);
                  }}
                  style={{
                    width: '100%',
                    padding: '10px 12px',
                    border: 'none',
                    background: active ? 'var(--white-10)' : 'transparent',
                    color: 'var(--text-heading)',
                    cursor: 'pointer',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    gap: 10,
                    textAlign: 'left',
                  }}
                >
                  <span style={{ fontSize: 12, fontWeight: 800 }}>{opt.label}</span>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>{active ? '✓' : ''}</div>
                </button>
              );
            })}
            <div style={{ padding: '8px 12px', fontSize: 10, color: 'var(--text-secondary)', borderTop: '1px solid var(--border-subtle)' }}>
              Filter edges by semantic group
            </div>
          </div>
        )}
      </div>
      )}
      {graphViewToolbarPolicy.showSecondaryEdgeToggle && (
        <IconButton
          onClick={() => setShowSecondaryEdges((v) => !v)}
          active={showSecondaryEdges}
          title={showSecondaryEdges ? 'Showing all edges (incl. context/geography)' : 'Primary edges only (hide context)'}
          icon={Icons.Filter}
          label={showSecondaryEdges ? 'All' : 'Primary'}
        />
      )}
      {graphViewFilterPolicy.semanticGroupColors && (
        <IconButton
          onClick={() => setUseSemanticGroupColors((v) => !v)}
          active={useSemanticGroupColors}
          title={useSemanticGroupColors ? '4 colors by semantic group' : 'Full per-type color palette'}
          icon={Icons.Palette}
        />
      )}
    </div>
  ) : null;

  const infoOverlayContent = (
    <div
      onClick={(e) => { if (e.target === e.currentTarget) setInfoOverlayOpen(false); }}
      style={{ ...overlayPanelStyle, display: 'flex', flexDirection: 'row', alignItems: 'center', flexWrap: 'nowrap', gap: 6, overflow: 'visible', whiteSpace: 'nowrap', maxWidth: 'none' }}
    >
      <IconButton
        onClick={() => onToggleInspector?.()}
        disabled={!onToggleInspector}
        active={Boolean(inspectorActive)}
        title={inspectorActive ? 'Close inspector' : 'Open inspector'}
        icon={Icons.Info}
      />
      <IconButton onClick={() => { setInfoOverlayOpen(false); setVisualSettingsOpen(true); }} title="Visual settings" icon={Icons.Sliders} />
      <div ref={toolbarMenuRef} style={{ padding: '0 8px', fontSize: 11, color: 'var(--text-secondary-strong)', display: 'flex', gap: 12, alignItems: 'center', borderLeft: '1px solid var(--border-subtle)' }}>
        <span>{nodes.length} nodes</span>
        <span>{edges.length} edges</span>
      </div>
      {error && (
        <div style={{ fontSize: 11, color: '#EF4444' }}>
          {error}
        </div>
      )}
    </div>
  );

  const applyVisualPreset = React.useCallback((preset: VisualPreset) => {
    setVisualPreset(preset);
    if (preset === 'max') {
      setForcePerfMode(false);
      setShowMiniMap(true);
      setShowBackgroundGrid(true);
      setHoverHighlightMode('control');
      setNodeVisualQuality('max');
      setShowNodeMetrics(false);
      return;
    }
    if (preset === 'minimal') {
      setForcePerfMode(true);
      setShowMiniMap(false);
      setShowBackgroundGrid(false);
      setHoverHighlightMode('off');
      setNodeVisualQuality('minimal');
      setShowNodeMetrics(false);
      return;
    }
    // balanced
    setForcePerfMode(null);
    setShowMiniMap(false);
    setShowBackgroundGrid(false);
    setHoverHighlightMode('control');
    setNodeVisualQuality('balanced');
    setShowNodeMetrics(false);
  }, []);

  const markCustom = React.useCallback(() => setVisualPreset('custom'), []);

  const handleNodeMouseEnter = React.useCallback(
    (_: any, node: Node<HorizonNodeData>) => {
      if (perfMode) return;
      const d: any = node?.data;
      // Entering a node/group should exit "edge hover" mode (edge leave can be unreliable).
      pendingHoverRef.current.hoveredEdgeId = null;
      if (d?.isGroup) {
        pendingHoverRef.current.hoveredGroupId = node.id;
        pendingHoverRef.current.hoveredNodeId = null;
      } else {
        pendingHoverRef.current.hoveredNodeId = node.id;
        pendingHoverRef.current.hoveredGroupId = null;
      }
      scheduleHoverCommit();
    },
    [perfMode, scheduleHoverCommit],
  );

  const handleNodeMouseLeave = React.useCallback(
    (_: any, node: Node<HorizonNodeData>) => {
      if (perfMode) return;
      const d: any = node?.data;
      const nodeId = String(node?.id || '').trim();
      if (!nodeId) return;
      
      if (d?.isGroup) {
        // Only clear if this is still the hovered group (avoid clearing if mouse moved to another group)
        if (pendingHoverRef.current.hoveredGroupId === nodeId) {
          pendingHoverRef.current.hoveredGroupId = null;
        }
      } else {
        // Only clear if this is still the hovered node (avoid clearing if mouse moved to another node)
        if (pendingHoverRef.current.hoveredNodeId === nodeId) {
          pendingHoverRef.current.hoveredNodeId = null;
        }
      }
      scheduleHoverCommit();
    },
    [perfMode, scheduleHoverCommit],
  );

  const handleEdgeMouseEnter = React.useCallback(
    (_: any, edge: Edge) => {
      if (perfMode) return;
      // Edge-hover mode intentionally fades everything else; clear node/group hover.
      pendingHoverRef.current.hoveredNodeId = null;
      pendingHoverRef.current.hoveredGroupId = null;
      pendingHoverRef.current.hoveredEdgeId = edge.id;
      scheduleHoverCommit();
    },
    [perfMode, scheduleHoverCommit],
  );

  const handleEdgeMouseLeave = React.useCallback(() => {
    if (perfMode) return;
    pendingHoverRef.current.hoveredEdgeId = null;
    scheduleHoverCommit();
  }, [perfMode, scheduleHoverCommit]);

  // 2026 UX: double-click node toggles expand/collapse (shortcut).
  // Important: stop propagation so ReactFlow's zoom-on-double-click (pane) doesn't hijack it.
  const handleNodeDoubleClick = React.useCallback((event: any, node: Node<HorizonNodeData>) => {
    try {
      event?.preventDefault?.();
      event?.stopPropagation?.();
    } catch {
      // ignore
    }
    if (!node?.id) return;
    const d: any = node?.data;
    if (d?.isGroup) return;
    if (d?.isLoadingRelated || d?.isScopeScouting) return;

    const fid = String(d?.fragmentId || node.id || '').trim();
    if (!fid) return;

    // Toggle collapse if already expanded; otherwise expand.
    if (d?.expanded && d?.onCollapse) {
      d.onCollapse(fid);
      return;
    }
    if (d?.onLoadRelated) {
      d.onLoadRelated(fid);
    }
  }, []);

  const renderNodes = displayNodes;
  const renderEdges = displayEdges;

  React.useEffect(() => {
    const corridorActive =
      isLevelsLayout &&
      levelsExpandMode === 'decision-chain' &&
      controlCorridorMode;
    if (!corridorActive) {
      controlCorridorRevealKeyRef.current = '';
      return;
    }
    const seedId =
      String(controlCorridorSeedId || '').trim() ||
      String((Array.isArray(controlCorridorGraph?.seedFragmentIds) ? controlCorridorGraph?.seedFragmentIds[0] : '') || '').trim();
    if (!seedId) return;
    const synNodePrefix = `${CORRIDOR_SYN_NODE_PREFIX}${seedId}:`;
    const synNodeCount = (renderNodes as any[]).filter((n) => String((n as any)?.id || '').trim().startsWith(synNodePrefix)).length;
    if (synNodeCount === 0) return;
    const key = `${seedId}|${synNodeCount}`;
    if (controlCorridorRevealKeyRef.current === key) return;
    controlCorridorRevealKeyRef.current = key;
    window.setTimeout(() => {
      try {
        const inst: any = reactFlowInstanceRef.current;
        if (!inst || typeof inst.fitView !== 'function') return;
        const toFit = (renderNodes as any[]).filter((n) => {
          const id = String((n as any)?.id || '').trim();
          if (!id) return false;
          if (id.startsWith(synNodePrefix)) return true;
          if (id === seedId) return true;
          const fid = String(((n as any)?.data as any)?.fragmentId || '').trim();
          return fid === seedId || fid.startsWith(synNodePrefix);
        });
        if (toFit.length > 0) {
          inst.fitView({ nodes: toFit as any, padding: 0.18, duration: 380 });
        }
      } catch {
        // best effort only
      }
    }, 90);
  }, [
    isLevelsLayout,
    isSupplyChainLayout,
    levelsExpandMode,
    controlCorridorMode,
    controlCorridorSeedId,
    controlCorridorGraph?.seedFragmentIds,
    renderNodes,
  ]);

  React.useEffect(() => {
    if (!import.meta.env.DEV) return;
    const corridorActive =
      isLevelsLayout &&
      levelsExpandMode === 'decision-chain' &&
      controlCorridorMode;
    if (!corridorActive) return;
    const seedId =
      String(controlCorridorSeedId || '').trim() ||
      String((Array.isArray(controlCorridorGraph?.seedFragmentIds) ? controlCorridorGraph?.seedFragmentIds[0] : '') || '').trim();
    const synNodePrefix = seedId ? `${CORRIDOR_SYN_NODE_PREFIX}${seedId}:` : CORRIDOR_SYN_NODE_PREFIX;
    const synEdgePrefix = seedId ? `${CORRIDOR_SYN_EDGE_PREFIX}${seedId}:` : CORRIDOR_SYN_EDGE_PREFIX;
    const syntheticNodeCount = (renderNodes as any[]).filter((n) => String((n as any)?.id || '').trim().startsWith(synNodePrefix)).length;
    const syntheticEdgeCount = (renderEdges as any[]).filter((e) => String((e as any)?.id || '').trim().startsWith(synEdgePrefix)).length;
    const key = [
      String(lensId || '').trim(),
      seedId,
      syntheticNodeCount,
      syntheticEdgeCount,
      renderNodes.length,
      renderEdges.length,
    ].join('|');
    if (controlCorridorRenderLogKeyRef.current === key) return;
    controlCorridorRenderLogKeyRef.current = key;
    console.groupCollapsed('[Corridor][Render] aggregated');
    console.log({
      lensId,
      corridorActive,
      seedId,
      syntheticNodeCount,
      syntheticEdgeCount,
      finalRenderNodeCount: renderNodes.length,
      finalRenderEdgeCount: renderEdges.length,
    });
    console.groupEnd();
  }, [
    lensId,
    isLevelsLayout,
    isSupplyChainLayout,
    levelsExpandMode,
    controlCorridorMode,
    controlCorridorSeedId,
    controlCorridorGraph?.seedFragmentIds,
    renderNodes.length,
    renderEdges.length,
  ]);

  return (
    <div style={{ width: '100%', height: '100%', position: 'relative', display: 'flex', flexDirection: 'column', minHeight: 0 }}>

      {/* Trace modal */}
      {traceDraft && ReactDOM.createPortal(
        <div
          role="dialog"
          aria-modal="true"
          onClick={() => setTraceDraft(null)}
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 9600,
            background: 'rgba(0,0,0,0.35)',
            backdropFilter: 'blur(6px)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            padding: 16,
          }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              width: 620,
              maxWidth: 'min(620px, calc(100vw - 32px))',
              borderRadius: 18,
              border: '1px solid var(--border-subtle)',
              background: 'var(--bg-elevated)',
              boxShadow: '0 18px 60px rgba(0,0,0,0.35)',
              padding: 14,
              color: 'var(--text-heading)',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginBottom: 10 }}>
              <div style={{ minWidth: 0 }}>
                <div style={{ fontSize: 13, fontWeight: 900 }}>
                  Trace
                </div>
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 3 }}>
                  Starting from <span style={{ fontWeight: 800, color: 'var(--text-heading)' }}>{traceDraft.rootLabel || traceDraft.rootId}</span>
                </div>
              </div>
              <button
                type="button"
                onClick={() => setTraceDraft(null)}
                className="sidebar-icon-btn"
                style={{
                  width: 32,
                  height: 32,
                  borderRadius: 10,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-secondary-strong)',
                  cursor: 'pointer',
                }}
                title="Close"
              >
                {Icons.X}
              </button>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 14, padding: 12, background: 'var(--bg-base)' }}>
                <div style={{ fontSize: 12, fontWeight: 800, marginBottom: 8 }}>Direction</div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                  {([
                    { key: 'up' as const, label: 'Up (hierarchy)' },
                    { key: 'down' as const, label: 'Down (hierarchy)' },
                    { key: 'both' as const, label: 'Both' },
                  ] as const).map((opt) => {
                    const active = traceDraft.direction === opt.key;
                    return (
                      <button
                        key={opt.key}
                        type="button"
                        onClick={() => setTraceDraft((prev) => prev ? ({ ...prev, direction: opt.key }) : prev)}
                        style={{
                          padding: '7px 10px',
                          borderRadius: 999,
                          border: '1px solid var(--border-subtle)',
                          background: active ? 'var(--white-10)' : 'transparent',
                          color: 'var(--text-heading)',
                          cursor: 'pointer',
                          fontSize: 12,
                          fontWeight: 850,
                        }}
                      >
                        {opt.label}
                      </button>
                    );
                  })}
                </div>
                <div style={{ marginTop: 10, fontSize: 11, color: 'var(--text-secondary-strong)', lineHeight: 1.35 }}>
                  If you’re not in Hierarchical view, Trace will switch to it automatically.
                </div>
              </div>

              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 14, padding: 12, background: 'var(--bg-base)' }}>
                <div style={{ fontSize: 12, fontWeight: 800, marginBottom: 8 }}>Scope</div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                  {([
                    { key: 'ownership' as const, label: 'Governance & Equity' },
                    { key: 'dependencies' as const, label: 'Supply chain' },
                    { key: 'context' as const, label: 'Operating footprint' },
                    { key: 'market' as const, label: 'Market' },
                    { key: 'influence' as const, label: 'Influence' },
                    { key: 'all' as const, label: 'Full network' },
                  ] as const).map((opt) => {
                    const active = Boolean(traceDraft.scopes?.[opt.key]);
                    return (
                      <button
                        key={opt.key}
                        type="button"
                        onClick={() => {
                          setTraceDraft((prev) => {
                            if (!prev) return prev;
                            const nextScopes = { ...(prev.scopes || ({} as any)) };
                            if (opt.key === 'all') {
                              const nextAll = !Boolean(nextScopes.all);
                              for (const k of Object.keys(nextScopes) as TraceScopeKey[]) nextScopes[k] = false;
                              nextScopes.all = nextAll;
                            } else {
                              nextScopes.all = false;
                              nextScopes[opt.key] = !Boolean(nextScopes[opt.key]);
                            }
                            return { ...prev, scopes: nextScopes };
                          });
                        }}
                        style={{
                          padding: '7px 10px',
                          borderRadius: 999,
                          border: '1px solid var(--border-subtle)',
                          background: active ? 'var(--white-10)' : 'transparent',
                          color: 'var(--text-heading)',
                          cursor: 'pointer',
                          fontSize: 12,
                          fontWeight: 850,
                        }}
                        title={opt.label}
                      >
                        {opt.label}
                      </button>
                    );
                  })}
                </div>
                <div style={{ marginTop: 10, fontSize: 11, color: 'var(--text-secondary-strong)', lineHeight: 1.35 }}>
                  Tip: avoid “Full network” with depth &gt; 3 on large graphs.
                </div>
              </div>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginTop: 12 }}>
              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 14, padding: 12, background: 'var(--bg-base)' }}>
                <div style={{ fontSize: 12, fontWeight: 800, marginBottom: 8 }}>Filters</div>
                <label style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Keywords (AND)</div>
                  <input
                    value={traceDraft.keywords}
                    onChange={(e) => setTraceDraft((prev) => prev ? ({ ...prev, keywords: e.target.value }) : prev)}
                    placeholder="e.g. oil, refinery, uae"
                    style={{
                      border: '1px solid var(--border-subtle)',
                      background: 'var(--bg-elevated)',
                      color: 'var(--text-heading)',
                      borderRadius: 12,
                      padding: '8px 10px',
                      fontSize: 12,
                    }}
                  />
                </label>
              </div>

              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 14, padding: 12, background: 'var(--bg-base)' }}>
                <div style={{ fontSize: 12, fontWeight: 800, marginBottom: 8 }}>Limits</div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 8 }}>
                  <label style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Depth</div>
                    <input
                      type="number"
                      min={1}
                      max={12}
                      value={traceDraft.depth}
                      onChange={(e) => setTraceDraft((prev) => prev ? ({ ...prev, depth: Math.max(1, Math.min(12, Number(e.target.value) || 1)) }) : prev)}
                      style={{
                        border: '1px solid var(--border-subtle)',
                        background: 'var(--bg-elevated)',
                        color: 'var(--text-heading)',
                        borderRadius: 12,
                        padding: '8px 10px',
                        fontSize: 12,
                      }}
                    />
                  </label>
                  <label style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Max nodes</div>
                    <input
                      type="number"
                      min={20}
                      max={1200}
                      value={traceDraft.maxNodes}
                      onChange={(e) => setTraceDraft((prev) => prev ? ({ ...prev, maxNodes: Math.max(20, Math.min(1200, Number(e.target.value) || 160)) }) : prev)}
                      style={{
                        border: '1px solid var(--border-subtle)',
                        background: 'var(--bg-elevated)',
                        color: 'var(--text-heading)',
                        borderRadius: 12,
                        padding: '8px 10px',
                        fontSize: 12,
                      }}
                    />
                  </label>
                  <label style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Max edges</div>
                    <input
                      type="number"
                      min={40}
                      max={2400}
                      value={traceDraft.maxEdges}
                      onChange={(e) => setTraceDraft((prev) => prev ? ({ ...prev, maxEdges: Math.max(40, Math.min(2400, Number(e.target.value) || 260)) }) : prev)}
                      style={{
                        border: '1px solid var(--border-subtle)',
                        background: 'var(--bg-elevated)',
                        color: 'var(--text-heading)',
                        borderRadius: 12,
                        padding: '8px 10px',
                        fontSize: 12,
                      }}
                    />
                  </label>
                </div>
              </div>
            </div>

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, marginTop: 12 }}>
              <button
                type="button"
                onClick={() => setTraceDraft(null)}
                style={{
                  padding: '9px 12px',
                  borderRadius: 12,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-heading)',
                  cursor: 'pointer',
                  fontSize: 12,
                  fontWeight: 850,
                }}
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void startTraceFromModal()}
                style={{
                  padding: '9px 12px',
                  borderRadius: 12,
                  border: '1px solid var(--border-subtle)',
                  background: 'var(--white-10)',
                  color: 'var(--text-heading)',
                  cursor: 'pointer',
                  fontSize: 12,
                  fontWeight: 900,
                }}
              >
                Start trace
              </button>
            </div>
          </div>
        </div>,
        document.body
      )}

      {/* Visual settings modal */}
      {visualSettingsOpen && ReactDOM.createPortal(
        <div
          role="dialog"
          aria-modal="true"
          onClick={() => setVisualSettingsOpen(false)}
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 9500,
            background: 'rgba(0,0,0,0.35)',
            backdropFilter: 'blur(6px)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            padding: 16,
          }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              width: 520,
              maxWidth: 'min(520px, calc(100vw - 32px))',
              borderRadius: 16,
              border: '1px solid var(--border-subtle)',
              background: 'var(--bg-elevated)',
              boxShadow: '0 18px 60px rgba(0,0,0,0.35)',
              padding: 14,
              color: 'var(--text-heading)',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, marginBottom: 10 }}>
              <div style={{ fontSize: 13, fontWeight: 800 }}>Visual settings</div>
              <button
                type="button"
                onClick={() => setVisualSettingsOpen(false)}
                className="sidebar-icon-btn"
                style={{
                  width: 32,
                  height: 32,
                  borderRadius: 10,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-secondary-strong)',
                  cursor: 'pointer',
                }}
                title="Close"
              >
                {Icons.X}
        </button>
            </div>

            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.4, marginBottom: 10 }}>
              Presets control the biggest performance levers. You can fine‑tune below.
      </div>

            <div style={{ display: 'flex', gap: 8, marginBottom: 12 }}>
              {([
                { key: 'max', label: 'Max', hint: 'Heaviest' },
                { key: 'balanced', label: 'Balanced', hint: 'Recommended' },
                { key: 'minimal', label: 'Minimal', hint: 'Fastest' },
              ] as const).map((p) => {
                const active = visualPreset === p.key;
                return (
                  <button
                    key={p.key}
                    type="button"
                    onClick={() => applyVisualPreset(p.key)}
                    style={{
                      flex: 1,
                      padding: '10px 10px',
                      borderRadius: 14,
                      border: '1px solid var(--border-subtle)',
                      background: active ? 'var(--white-10)' : 'transparent',
                      color: 'var(--text-heading)',
                      cursor: 'pointer',
                      textAlign: 'left',
                    }}
                  >
                    <div style={{ fontSize: 12, fontWeight: 800 }}>{p.label}</div>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{p.hint}</div>
                  </button>
                );
              })}
            </div>

            <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
              {/* Perf mode */}
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12, fontWeight: 700 }}>Performance mode</div>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                    Biggest impact: reduces per-hover edge work and disables expensive highlights on large graphs.
                  </div>
                </div>
                <select
                  value={forcePerfMode === null ? 'auto' : forcePerfMode ? 'on' : 'off'}
                  onChange={(e) => {
                    markCustom();
                    const v = e.target.value;
                    setForcePerfMode(v === 'auto' ? null : v === 'on');
                  }}
                  style={{
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-base)',
                    color: 'var(--text-heading)',
                    borderRadius: 10,
                    padding: '6px 10px',
                    fontSize: 12,
                    cursor: 'pointer',
                  }}
                >
                  <option value="auto">Auto</option>
                  <option value="on">On</option>
                  <option value="off">Off</option>
                </select>
              </div>

              {/* Node visuals */}
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12, fontWeight: 700 }}>Node visuals</div>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                    Medium impact: shadows/overlays and extra node chrome.
                  </div>
                </div>
                <select
                  value={nodeVisualQuality}
                  onChange={(e) => {
                    markCustom();
                    const v = e.target.value as HorizonNodeVisualQuality;
                    setNodeVisualQuality(v === 'max' || v === 'minimal' ? v : 'balanced');
                  }}
                  style={{
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-base)',
                    color: 'var(--text-heading)',
                    borderRadius: 10,
                    padding: '6px 10px',
                    fontSize: 12,
                    cursor: 'pointer',
                  }}
                >
                  <option value="max">Max</option>
                  <option value="balanced">Balanced</option>
                  <option value="minimal">Minimal</option>
                </select>
              </div>

              {/* Node metrics */}
              <label style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12, fontWeight: 700 }}>Node metrics</div>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                    High impact. Off by default — shows 2–3 metrics inside nodes.
                  </div>
                </div>
                <input
                  type="checkbox"
                  checked={showNodeMetrics}
                  onChange={(e) => {
                    markCustom();
                    setShowNodeMetrics(e.target.checked);
                  }}
                />
              </label>

              {/* MiniMap */}
              <label style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12, fontWeight: 700 }}>MiniMap</div>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Low–medium impact depending on graph size.</div>
                </div>
                <input
                  type="checkbox"
                  checked={showMiniMap}
                  onChange={(e) => {
                    markCustom();
                    setShowMiniMap(e.target.checked);
                  }}
                />
              </label>

              {/* Background grid */}
              <label style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12, fontWeight: 700 }}>Background grid</div>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>Low impact.</div>
          </div>
                <input
                  type="checkbox"
                  checked={showBackgroundGrid}
                  onChange={(e) => {
                    markCustom();
                    setShowBackgroundGrid(e.target.checked);
                  }}
                />
              </label>

              {/* Highlight on hover */}
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12, fontWeight: 700 }}>Highlight</div>
                  <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                    High impact on dense graphs (computes relation clusters + restyles many edges).
          </div>
      </div>
                <select
                  value={hoverHighlightMode}
                  onChange={(e) => {
                    markCustom();
                    setHoverHighlightMode(e.target.value as any);
                  }}
                  disabled={perfMode}
                  style={{
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-base)',
                    color: 'var(--text-heading)',
                    borderRadius: 10,
                    padding: '6px 10px',
                    fontSize: 12,
                    cursor: perfMode ? 'not-allowed' : 'pointer',
                    opacity: perfMode ? 0.5 : 1,
                  }}
                >
                  <option value="off">Off</option>
                  <option value="control">Control</option>
                  <option value="expansion">Expansion</option>
                  <option value="production">Production</option>
                  <option value="supplies">Supplies</option>
                  <option value="competition">Competition</option>
                  <option value="decision-chain">Decision Chain</option>
                  <option value="signal-propagation">Signal propagation</option>
                </select>
              </div>
            </div>
          </div>
        </div>,
        document.body
      )}

      {/* Graph */}
      <div style={{ flex: 1, minHeight: 0, position: 'relative', width: '100%' }}>
        {/* Trace status callout (bottom-left) */}
        {traceJob && (
          <div
            style={{
              position: 'absolute',
              left: 16,
              bottom: 16,
              zIndex: 85,
              width: 340,
              maxWidth: 'min(340px, calc(100vw - 32px))',
              borderRadius: 14,
              border: '1px solid var(--border-subtle)',
              background: theme === 'dark' ? 'rgba(0,0,0,0.40)' : 'rgba(255,255,255,0.70)',
              backdropFilter: 'blur(10px)',
              boxShadow: theme === 'dark' ? '0 14px 40px rgba(0,0,0,0.45)' : '0 14px 40px rgba(0,0,0,0.18)',
              padding: '10px 12px',
              pointerEvents: 'auto',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
              <div style={{ minWidth: 0 }}>
                <div style={{ fontSize: 12, fontWeight: 900, color: 'var(--text-heading)' }}>
                  Trace {traceJob.status === 'running' ? 'running…' : traceJob.status === 'stopped' ? 'stopped' : traceJob.status === 'error' ? 'error' : 'done'}
                </div>
                <div style={{ marginTop: 2, fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                  {traceJob.message || `Depth ${traceJob.depth}/${traceJob.maxDepth}`}
                </div>
              </div>
              <button
                type="button"
                onClick={() => {
                  if (traceJob.status === 'running') return;
                  setTraceJob(null);
                }}
                disabled={traceJob.status === 'running'}
                style={{
                  width: 28,
                  height: 28,
                  borderRadius: 10,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-secondary-strong)',
                  cursor: traceJob.status === 'running' ? 'not-allowed' : 'pointer',
                  opacity: traceJob.status === 'running' ? 0.45 : 1,
                }}
                title="Dismiss"
                aria-label="Dismiss"
              >
                {Icons.X}
              </button>
            </div>

            <div style={{ marginTop: 8, display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 8 }}>
              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                <div style={{ fontWeight: 900, color: 'var(--text-heading)' }}>{traceJob.addedNodes}</div>
                <div>nodes</div>
              </div>
              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                <div style={{ fontWeight: 900, color: 'var(--text-heading)' }}>{traceJob.addedEdges}</div>
                <div>edges</div>
              </div>
              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                <div style={{ fontWeight: 900, color: 'var(--text-heading)' }}>{traceJob.processed}</div>
                <div>visited</div>
              </div>
            </div>

            <div style={{ display: 'flex', gap: 8, marginTop: 10 }}>
              {traceJob.status === 'running' ? (
                <button
                  type="button"
                  onClick={() => stopTrace()}
                  style={{
                    flex: 1,
                    padding: '8px 10px',
                    borderRadius: 12,
                    border: '1px solid rgba(248,113,113,0.55)',
                    background: theme === 'dark' ? 'rgba(248,113,113,0.12)' : 'rgba(248,113,113,0.10)',
                    color: 'var(--text-heading)',
                    cursor: 'pointer',
                    fontSize: 12,
                    fontWeight: 900,
                  }}
                >
                  Stop
                </button>
              ) : (
                <button
                  type="button"
                  onClick={() => setTraceJob(null)}
                  style={{
                    flex: 1,
                    padding: '8px 10px',
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: 'transparent',
                    color: 'var(--text-heading)',
                    cursor: 'pointer',
                    fontSize: 12,
                    fontWeight: 850,
                  }}
                >
                  Close
                </button>
              )}

              <button
                type="button"
                onClick={() => undoLastTrace()}
                disabled={!canUndoTrace}
                style={{
                  padding: '8px 10px',
                  borderRadius: 12,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-heading)',
                  cursor: canUndoTrace ? 'pointer' : 'not-allowed',
                  fontSize: 12,
                  fontWeight: 900,
                  opacity: canUndoTrace ? 1 : 0.45,
                  whiteSpace: 'nowrap',
                }}
                title={canUndoTrace ? 'Undo last trace' : 'No trace to undo'}
              >
                Undo
              </button>
            </div>
          </div>
        )}

        {/* Right-side vertical buttons; overlays expand left as horizontal toolbar rows.
            pointerEvents: none on the wrapper so the graph underneath remains fully interactive.
            Each button / overlay row sets pointerEvents: auto on itself. */}
        <div
          style={{
            position: 'absolute',
            top: 12,
            right: 12,
            zIndex: 80,
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'flex-end',
            gap: 8,
            pointerEvents: 'none',
            userSelect: 'none',
          }}
        >
          {/* 1. Actions */}
          <div style={{ position: 'relative', pointerEvents: 'auto' }}>
            <button
              type="button"
              onClick={() => {
                setSimulationOverlayOpen(false);
                setViewOverlayOpen(false);
                setInfoOverlayOpen(false);
                setActionsOverlayOpen((v) => {
                  if (!v) clearHoverWhenOverlayOpens();
                  return !v;
                });
              }}
              title="Actions"
              aria-label="Actions"
              style={pushableButtonStyle(actionsOverlayOpen)}
            >
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" /><path d="M3 3v5h5" /><path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16" /><path d="M16 21h5v-5" />
              </svg>
            </button>
            {actionsOverlayOpen && (
              <div style={{ position: 'absolute', top: 0, right: 'calc(100% + 8px)' }}>
                {actionsOverlayContent}
              </div>
            )}
          </div>

          {/* Supply Flow overlay */}
          {graphViewOverlayPolicy.showSupplyChainOverlay && (
            <div style={{ pointerEvents: 'auto' }}>
              <SupplyChainOverlay
                lensId={lensId}
                nodes={nodes}
                isVisible={true}
                onLoadChain={handleLoadSupplyChain}
                onRetry={() => { setSupplyChainRefreshKey((k) => k + 1); }}
                refreshKey={supplyChainRefreshKey}
              />
            </div>
          )}

          <div style={{ position: 'relative', pointerEvents: 'auto' }}>
            <button
              type="button"
              onClick={() => {
                setActionsOverlayOpen(false);
                setViewOverlayOpen(false);
                setInfoOverlayOpen(false);
                setSimulationOverlayOpen((v) => {
                  if (!v) clearHoverWhenOverlayOpens();
                  return !v;
                });
              }}
              disabled={!lensId}
              title={lensId ? 'Simulation overlays' : 'Open a lens first'}
              aria-label="Simulation overlays"
              style={{
                ...pushableButtonStyle(simulationOverlayOpen),
                opacity: lensId ? 1 : 0.5,
                cursor: lensId ? 'pointer' : 'not-allowed',
              }}
            >
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M4 19h16" />
                <path d="M7 16V8" />
                <path d="M12 16V5" />
                <path d="M17 16v-3" />
              </svg>
            </button>
            {simulationOverlayOpen && lensId && (
              <div style={{ position: 'absolute', top: 0, right: 'calc(100% + 8px)' }}>
                <SimulationOverlayPanel
                  lensId={lensId}
                  isVisible={simulationOverlayOpen}
                  visibleTargetRefs={visibleCanonicalPaths}
                  selectedTargetRef={selectedNodeCanonicalPath}
                />
              </div>
            )}
          </div>

          {/* 2. View & highlight */}
          <div style={{ position: 'relative', pointerEvents: 'auto' }}>
            <button
              type="button"
              onClick={() => {
                if (graphEmpty) return;
                setActionsOverlayOpen(false);
                setSimulationOverlayOpen(false);
                setInfoOverlayOpen(false);
                setViewOverlayOpen((v) => {
                  if (!v) clearHoverWhenOverlayOpens();
                  return !v;
                });
              }}
              disabled={graphEmpty}
              title={graphEmpty ? 'Load graph first' : 'View & highlight'}
              aria-label="View & highlight"
              style={{ ...pushableButtonStyle(viewOverlayOpen), opacity: graphEmpty ? 0.5 : 1, cursor: graphEmpty ? 'not-allowed' : 'pointer' }}
            >
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <circle cx="13.5" cy="6.5" r="1.5"/><circle cx="17.5" cy="10.5" r="1.5"/><circle cx="8.5" cy="7.5" r="1.5"/><circle cx="6.5" cy="12.5" r="1.5"/><path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.648 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668c1.105 0 2.059-.215 2.98-.643 2.02-1.008 3.48-3.157 3.48-5.75 0-4.5-4.5-8.5-10-8.5z"/>
              </svg>
            </button>
            {viewOverlayOpen && (
              <div style={{ position: 'absolute', top: 0, right: 'calc(100% + 8px)' }}>
                {viewOverlayContent}
              </div>
            )}
          </div>

          {/* 3. Info & settings */}
          <div style={{ position: 'relative', pointerEvents: 'auto' }}>
            <button
              type="button"
              onClick={() => {
                setActionsOverlayOpen(false);
                setSimulationOverlayOpen(false);
                setViewOverlayOpen(false);
                setInfoOverlayOpen((v) => {
                  if (!v) clearHoverWhenOverlayOpens();
                  return !v;
                });
              }}
              title="Info & settings"
              aria-label="Info & settings"
              style={pushableButtonStyle(infoOverlayOpen)}
            >
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                <circle cx="12" cy="12" r="10" /><path d="M12 16v-4" /><path d="M12 8h.01" />
              </svg>
            </button>
            {infoOverlayOpen && (
              <div style={{ position: 'absolute', top: 0, right: 'calc(100% + 8px)' }}>
                {infoOverlayContent}
              </div>
            )}
          </div>
        </div>

        {/* Edge explanation popover (opened via small button next to edge caption) */}
        {edgeExplainPopover?.open && (
          <>
            {/* Backdrop: dim + blur (keeps the graph context visible but quiet) */}
            <div
              style={{
                position: 'fixed',
                inset: 0,
                zIndex: 1950,
                background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(15,23,42,0.12)',
                backdropFilter: 'blur(10px)',
              }}
            />
            <div
              ref={edgeExplainPopoverRef}
              style={{
                position: 'fixed',
                left: '50%',
                top: '50%',
                transform: 'translate(-50%, -50%)',
                width: 560,
                maxWidth: 'calc(100vw - 24px)',
                maxHeight: 'calc(100vh - 24px)',
                zIndex: 2000,
                borderRadius: 16,
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.12)' : 'rgba(15,23,42,0.10)'}`,
                background: theme === 'dark' ? 'rgba(10,10,12,0.70)' : 'rgba(255,255,255,0.72)',
                backdropFilter: 'blur(12px)',
                boxShadow: theme === 'dark' ? '0 22px 70px rgba(0,0,0,0.60)' : '0 22px 70px rgba(0,0,0,0.18)',
                overflow: 'hidden',
              }}
              onMouseDown={(e) => {
                // Prevent canvas pan/selection side-effects.
                e.stopPropagation();
              }}
            >
              {/* Minimal actions (no header section): unobtrusive corner controls */}
              <div
                style={{
                  position: 'absolute',
                  top: 10,
                  right: 10,
                  zIndex: 2,
                  display: 'flex',
                  gap: 8,
                }}
              >
        <button
          type="button"
                  disabled={edgeExplainPopover.loading}
                  onClick={() => {
                    loadEdgeExplanation({
                      edgeId: edgeExplainPopover.edgeId,
                      edgeType: edgeExplainPopover.edgeType,
                      sourceNodeId: edgeExplainPopover.sourceNodeId,
                      targetNodeId: edgeExplainPopover.targetNodeId,
                      force: true,
                    });
                  }}
                  title="Reload"
                  aria-label="Reload"
          style={{
                    width: 28,
                    height: 28,
                    borderRadius: 10,
                    border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)'}`,
                    background: 'transparent',
            color: 'var(--text-heading)',
                    cursor: edgeExplainPopover.loading ? 'not-allowed' : 'pointer',
                    opacity: edgeExplainPopover.loading ? 0.35 : 0.7,
                    fontSize: 14,
                    fontWeight: 900,
                  }}
                >
                  ↻
        </button>
      <button
        type="button"
                  onClick={closeEdgeExplainPopover}
                  title="Close"
                  aria-label="Close"
        style={{
                    width: 28,
                    height: 28,
                    borderRadius: 10,
                    border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)'}`,
                    background: 'transparent',
                    color: 'var(--text-heading)',
                    cursor: 'pointer',
                    opacity: 0.7,
                    fontSize: 16,
                    fontWeight: 900,
                    lineHeight: 1,
                  }}
                >
                  ×
      </button>
    </div>

              {/* Reserve space for top controls + equal breathing room at the bottom */}
        <div
          style={{
                  paddingTop: 54,
                  paddingBottom: 54,
                  paddingLeft: 18,
                  paddingRight: 18,
                  maxHeight: 'calc(100vh - 24px)',
                  overflow: 'auto',
                }}
              >
                {edgeExplainPopover.error && (
                  <div style={{ fontSize: 12, color: '#f87171' }}>{edgeExplainPopover.error}</div>
                )}
                {!edgeExplainPopover.error && edgeExplainPopover.loading && !edgeExplainPopover.markdown && (
                  <div
                    style={{
                      minHeight: 220,
            display: 'flex',
            alignItems: 'center',
                      justifyContent: 'center',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                      <style>{`
                        @keyframes horizon-pulse-dot {
                          0%, 100% { transform: scale(0.6); opacity: 0.4; }
                          50% { transform: scale(1); opacity: 1; }
                        }
                      `}</style>
                      {[0, 1, 2].map((i) => (
                        <div
                          key={i}
                          style={{
                            width: 10,
                            height: 10,
                            borderRadius: '50%',
                            background: 'var(--accent-primary, #3B82F6)',
                            animation: `horizon-pulse-dot 1.2s infinite ease-in-out`,
                            animationDelay: `${i * 0.2}s`,
                          }}
                        />
                      ))}
                    </div>
        </div>
                )}
                {!!edgeExplainPopover.markdown && (
                  <div style={{ fontSize: 12, color: 'var(--text-heading)', lineHeight: 1.6 }}>
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>{edgeExplainPopover.markdown}</ReactMarkdown>
                  </div>
                )}
              </div>
            </div>
          </>
        )}

        {/* Node explanation popover (opened via "i" button on a node) */}
        {nodeExplainPopover?.open && (
          <>
            {/* Backdrop */}
            <div
              style={{
                position: 'fixed',
                inset: 0,
                zIndex: 1950,
                background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(15,23,42,0.12)',
                backdropFilter: 'blur(10px)',
              }}
            />
            <div
              ref={nodeExplainPopoverRef}
              style={{
                position: 'fixed',
                left: '50%',
                top: '50%',
                transform: 'translate(-50%, -50%)',
                width: 560,
                maxWidth: 'calc(100vw - 24px)',
                maxHeight: 'calc(100vh - 24px)',
                zIndex: 2000,
                borderRadius: 16,
                border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.12)' : 'rgba(15,23,42,0.10)'}`,
                background: theme === 'dark' ? 'rgba(10,10,12,0.70)' : 'rgba(255,255,255,0.72)',
                backdropFilter: 'blur(12px)',
                boxShadow: theme === 'dark' ? '0 22px 70px rgba(0,0,0,0.60)' : '0 22px 70px rgba(0,0,0,0.18)',
                overflow: 'hidden',
              }}
              onMouseDown={(e) => {
                e.stopPropagation();
              }}
            >
              <div
                style={{
                  position: 'absolute',
                  top: 10,
                  right: 10,
                  zIndex: 2,
                  display: 'flex',
                  gap: 8,
                }}
              >
                <button
                  type="button"
                  disabled={nodeExplainPopover.loading}
                  onClick={() => {
                    loadNodeExplanation({
                      nodeId: nodeExplainPopover.nodeId,
                      fragmentId: nodeExplainPopover.fragmentId,
                      label: nodeExplainPopover.label,
                      canonicalPath: nodeExplainPopover.canonicalPath,
                      nodeType: nodeExplainPopover.nodeType,
                      force: true,
                    });
                  }}
                  title="Reload"
                  aria-label="Reload"
                  style={{
                    width: 28,
                    height: 28,
                    borderRadius: 10,
                    border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)'}`,
                    background: 'transparent',
                    color: 'var(--text-heading)',
                    cursor: nodeExplainPopover.loading ? 'not-allowed' : 'pointer',
                    opacity: nodeExplainPopover.loading ? 0.35 : 0.7,
                    fontSize: 14,
                    fontWeight: 900,
                  }}
                >
                  ↻
                </button>
                <button
                  type="button"
                  onClick={closeNodeExplainPopover}
                  title="Close"
                  aria-label="Close"
                  style={{
                    width: 28,
                    height: 28,
                    borderRadius: 10,
                    border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.12)'}`,
                    background: 'transparent',
                    color: 'var(--text-heading)',
                    cursor: 'pointer',
                    opacity: 0.7,
                    fontSize: 16,
                    fontWeight: 900,
                    lineHeight: 1,
                  }}
                >
                  ×
                </button>
              </div>

              <div
                style={{
                  paddingTop: 54,
                  paddingBottom: 54,
                  paddingLeft: 18,
                  paddingRight: 18,
                  maxHeight: 'calc(100vh - 24px)',
                  overflow: 'auto',
                }}
              >
                {nodeExplainPopover.error && (
                  <div style={{ fontSize: 12, color: '#f87171' }}>{nodeExplainPopover.error}</div>
                )}
                {!nodeExplainPopover.error && nodeExplainPopover.loading && !nodeExplainPopover.markdown && (
                  <div
                    style={{
                      minHeight: 220,
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                      <style>{`
                        @keyframes horizon-pulse-dot {
                          0%, 100% { transform: scale(0.6); opacity: 0.4; }
                          50% { transform: scale(1); opacity: 1; }
                        }
                      `}</style>
                      {[0, 1, 2].map((i) => (
                        <div
                          key={i}
                          style={{
                            width: 10,
                            height: 10,
                            borderRadius: '50%',
                            background: 'var(--accent-primary, #3B82F6)',
                            animation: `horizon-pulse-dot 1.2s infinite ease-in-out`,
                            animationDelay: `${i * 0.2}s`,
                          }}
                        />
                      ))}
                    </div>
                  </div>
                )}
                {!!nodeExplainPopover.markdown && (
                  <div style={{ fontSize: 12, color: 'var(--text-heading)', lineHeight: 1.6 }}>
                    <ReactMarkdown remarkPlugins={[remarkGfm]}>{nodeExplainPopover.markdown}</ReactMarkdown>
                  </div>
                )}
              </div>
            </div>
          </>
        )}

        {viewDirective?.summary && (
          <div
            style={{
              position: 'absolute',
              top: 12,
              left: 12,
              zIndex: 180,
              maxWidth: 420,
              padding: '8px 12px',
              borderRadius: 10,
              background: theme === 'dark' ? 'rgba(15, 23, 42, 0.92)' : 'rgba(255, 255, 255, 0.94)',
              border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.18)' : 'rgba(15,23,42,0.12)'}`,
              color: 'var(--text-heading)',
              fontSize: 12,
              lineHeight: 1.4,
            }}
          >
            {String(viewDirective.summary || '').trim()}
          </div>
        )}

        <ReactFlow
            nodes={renderNodes}
            edges={renderEdges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onNodeClick={handleNodeClick}
            onNodeDoubleClick={handleNodeDoubleClick}
            onEdgeClick={handleEdgeClick}
            onPaneClick={handlePaneClick}
            onNodeDragStop={handleNodeDragStop}
            onNodeMouseEnter={handleNodeMouseEnter}
            onNodeMouseLeave={handleNodeMouseLeave}
            onEdgeMouseEnter={handleEdgeMouseEnter}
            onEdgeMouseLeave={handleEdgeMouseLeave}
            nodeTypes={HORIZON_NODE_TYPES}
            edgeTypes={CUSTOM_EDGE_TYPES}
            onInit={(instance) => {
              reactFlowInstanceRef.current = instance;
              // Initial fitView is called manually in the loadLensItems effect, not here,
              // to avoid viewport jumps during dock filter toggles and other relayouts.
            }}
            style={{ background: bgColor, width: '100%', height: '100%' }}
            minZoom={0.1}
            maxZoom={2}
            defaultViewport={{ x: 0, y: 0, zoom: 1 }}
            proOptions={{ hideAttribution: true }}
            onlyRenderVisibleElements={true}
            nodesConnectable={false}
            nodesDraggable={true}
            elementsSelectable={true}
          >
          {showBackgroundGrid && <Background color={bgPatternColor} gap={16} />}
          <Controls style={controlsStyle} />
          {showMiniMap && (
            <MiniMap
              style={miniMapStyle}
              nodeColor={miniMapNodeColor}
              maskColor={miniMapMaskColor}
            />
          )}
        </ReactFlow>

        {/* Off-screen node previews (render OUTSIDE ReactFlow to avoid interfering with layout). */}
        {offScreenPreviews.length > 0 && (
          <div
            style={{
              position: 'absolute',
              inset: 0,
              pointerEvents: 'none',
              zIndex: 210,
            }}
          >
            {/* Shared SVG layer for all preview lines */}
            <svg
              style={{
                position: 'absolute',
                inset: 0,
                width: '100%',
                height: '100%',
                pointerEvents: 'none',
              }}
            >
              {offScreenPreviews.map((p) => (
                <line
                  key={`preview-line-${p.edgeId}-${p.nodeId}`}
                  x1={p.hoveredScreenX}
                  y1={p.hoveredScreenY}
                  x2={p.screenX}
                  y2={p.screenY}
                  stroke={theme === 'dark' ? 'rgba(255, 255, 255, 0.40)' : 'rgba(15, 23, 42, 0.40)'}
                  strokeWidth={2}
                  strokeDasharray="4 4"
                />
              ))}
            </svg>

            {/* Preview cards */}
            {offScreenPreviews.map((p) => {
              const nodeData: any = p.node.data || {};
              const nodeLabel = String(nodeData.label || p.node.id || '').trim();
              const edgeData: any = p.edge.data || {};
              const edgeType = String(edgeData.edgeType || p.edge.label || '').trim();

              return (
                <div
                  key={`preview-card-${p.nodeId}`}
                  style={{
                    position: 'absolute',
                    left: p.screenX,
                    top: p.screenY,
                    transform: 'translate(-50%, -50%)',
                    minWidth: 120,
                    maxWidth: 200,
                    padding: '8px 12px',
                    borderRadius: 10,
                    background: theme === 'dark' ? 'rgba(15, 23, 42, 0.95)' : 'rgba(255, 255, 255, 0.96)',
                    border: `1px solid ${theme === 'dark' ? 'rgba(255, 255, 255, 0.22)' : 'rgba(15, 23, 42, 0.16)'}`,
                    boxShadow: theme === 'dark'
                      ? '0 10px 26px rgba(0, 0, 0, 0.55)'
                      : '0 10px 26px rgba(0, 0, 0, 0.20)',
                    color: theme === 'dark' ? 'rgba(255, 255, 255, 0.92)' : 'rgba(15, 23, 42, 0.90)',
                    fontSize: 12,
                    fontWeight: 650,
                    textAlign: 'center',
                    whiteSpace: 'nowrap',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                  }}
                >
                  {nodeLabel || '…'}
                  {edgeType && (
                    <div style={{ fontSize: 10, opacity: 0.72, marginTop: 4, fontWeight: 550 }}>
                      {edgeType}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}

        {/* Ask-Graph FAB + recent questions chips (max 5) */}
        <div
          style={{
            position: 'absolute',
            right: 14,
            bottom: 14,
            zIndex: 220,
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'flex-end',
            gap: 8,
            pointerEvents: 'none', // only inner elements handle pointer events
          }}
        >
          {!graphEmpty && graphQueryChips.length > 0 && (
            <div
              style={{
                display: 'flex',
                flexWrap: 'wrap',
                gap: 6,
                justifyContent: 'flex-end',
                maxWidth: 340,
                pointerEvents: 'auto',
              }}
            >
              {graphQueryChips.slice(0, 5).map((c) => {
                const active = graphQueryActiveChipId === c.id && !!graphQueryHighlightIds && graphQueryHighlightIds.size > 0;
                const fullQ = String(c.question || '').trim();
                const nodesCount = Array.isArray(c.relevantNodeIds) ? c.relevantNodeIds.length : 0;
                const sourcesCount = Array.isArray(c.sources) ? c.sources.length : 0;
                const topFindings = Array.isArray(c.findings) ? c.findings.slice(0, 2).map((f) => String(f?.title || '').trim()).filter(Boolean) : [];
                const tooltip =
                  [
                    fullQ || 'Question',
                    '',
                    `Nodes highlighted: ${nodesCount}`,
                    `Sources: ${sourcesCount}`,
                    topFindings.length > 0 ? '' : null,
                    topFindings.length > 0 ? 'Top findings:' : null,
                    ...topFindings.map((t) => `- ${t}`),
                    c.answer ? '' : null,
                    c.answer ? `Answer: ${String(c.answer || '').trim()}` : null,
                  ]
                    .filter((x) => typeof x === 'string' && String(x).length > 0)
                    .join('\n');

                return (
                  <button
                    key={c.id}
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      e.preventDefault();
                      toggleGraphQueryChip(c);
                    }}
                    title={tooltip}
                    style={{
                      maxWidth: 340,
                      padding: '6px 10px',
                      borderRadius: 999,
                      border: active ? '1px solid rgba(248,113,113,0.55)' : '1px solid var(--border-subtle)',
                      background: active
                        ? (theme === 'dark' ? 'rgba(248,113,113,0.14)' : 'rgba(220,38,38,0.10)')
                        : (theme === 'dark' ? 'rgba(0,0,0,0.30)' : 'rgba(255,255,255,0.72)'),
                      color: 'var(--text-heading)',
                      cursor: 'pointer',
                      fontSize: 12,
                      fontWeight: 700,
                      backdropFilter: 'blur(8px)',
                      boxShadow: active
                        ? (theme === 'dark' ? '0 10px 28px rgba(0,0,0,0.40)' : '0 10px 28px rgba(0,0,0,0.18)')
                        : undefined,
                      display: 'inline-flex',
                      alignItems: 'center',
                      gap: 8,
                    }}
                  >
                    <span
                      style={{
                        minWidth: 0,
                        maxWidth: 280,
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {truncateChip(fullQ || 'Ask graph…', 64)}
                    </span>
                    {sourcesCount > 0 && (
                      <span
                        style={{
                          flexShrink: 0,
                          fontSize: 11,
                          fontWeight: 800,
                          padding: '2px 8px',
                          borderRadius: 999,
                          border: '1px solid var(--border-subtle)',
                          background: theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)',
                          color: 'var(--text-secondary-strong)',
                        }}
                        title={`${sourcesCount} sources`}
                      >
                        {sourcesCount}
                      </span>
                    )}
                  </button>
                );
              })}
            </div>
          )}

          {/* Graph Intent error toast (only when palette closed) */}
          {graphIntentError && !graphIntentPlan && !addNodeOpen && (
            <div
              style={{
                position: 'absolute',
                left: '50%',
                bottom: 72,
                transform: 'translateX(-50%)',
                zIndex: 210,
                padding: '10px 20px',
                borderRadius: 12,
                border: '1px solid rgba(239,68,68,0.5)',
                background: theme === 'dark' ? 'rgba(239,68,68,0.15)' : 'rgba(239,68,68,0.08)',
                color: 'var(--text-heading)',
                fontSize: 13,
                maxWidth: 400,
              }}
            >
              {graphIntentError}
              <button
                type="button"
                onClick={() => setGraphIntentError(null)}
                style={{ marginLeft: 12, textDecoration: 'underline', cursor: 'pointer', background: 'none', border: 'none', color: 'inherit' }}
              >
                Dismiss
              </button>
            </div>
          )}

          {!graphEmpty && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              e.preventDefault();
              if (!lensId) return;
              openModeler();
            }}
            disabled={!lensId || modelerLoading}
            title="Policy impact modeler"
            style={{
              pointerEvents: 'auto',
              width: 44,
              height: 44,
              borderRadius: 999,
              border: modelerOpen
                ? (theme === 'dark' ? '1px solid rgba(96,165,250,0.55)' : '1px solid rgba(37,99,235,0.50)')
                : '1px solid var(--border-subtle)',
              background: modelerOpen
                ? (theme === 'dark' ? 'rgba(59,130,246,0.22)' : 'rgba(59,130,246,0.14)')
                : (theme === 'dark' ? 'rgba(0,0,0,0.55)' : 'rgba(255,255,255,0.80)'),
              color: 'var(--text-heading)',
              cursor: (!lensId || modelerLoading) ? 'not-allowed' : 'pointer',
              opacity: (!lensId || modelerLoading) ? 0.55 : 1,
              filter: theme === 'dark' ? 'drop-shadow(0 16px 40px rgba(0,0,0,0.55))' : 'drop-shadow(0 16px 40px rgba(0,0,0,0.18))',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 14,
              fontWeight: 900,
              lineHeight: 1,
              backdropFilter: 'blur(8px)',
              transition: 'background-color 0.18s ease, border-color 0.18s ease',
            }}
          >
            FX
          </button>
          )}

          {!graphEmpty && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              e.preventDefault();
              if (!lensId) return;
              setModelerOpen(false);
              setAskGraphError(null);
              setAskGraphOpen(true);
            }}
            disabled={!lensId || askGraphLoading}
            title="Ask this graph"
            style={{
              pointerEvents: 'auto',
              width: 44,
              height: 44,
              borderRadius: 999,
              border: '1px solid var(--border-subtle)',
              background: theme === 'dark' ? 'rgba(0,0,0,0.55)' : 'rgba(255,255,255,0.80)',
              color: 'var(--text-heading)',
              cursor: (!lensId || askGraphLoading) ? 'not-allowed' : 'pointer',
              opacity: (!lensId || askGraphLoading) ? 0.55 : 1,
              // Use drop-shadow so the shadow follows the circular shape (box-shadow can look "square" visually).
              boxShadow: 'none',
              filter: theme === 'dark' ? 'drop-shadow(0 16px 40px rgba(0,0,0,0.55))' : 'drop-shadow(0 16px 40px rgba(0,0,0,0.18))',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 18,
              fontWeight: 900,
              lineHeight: 1,
              backdropFilter: 'blur(8px)',
            }}
          >
            ?
          </button>
          )}
        </div>

        {/* Bottom-center quick trigger for Search/Add command palette — hidden when graph empty */}
        {!graphEmpty && (
        <div
          style={{
            position: 'absolute',
            left: '50%',
            bottom: 14,
            transform: 'translateX(-50%)',
            zIndex: 220,
            pointerEvents: 'none',
          }}
        >
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              e.preventDefault();
              toggleCommandPalette();
            }}
            disabled={!lensId}
            title={addNodeOpen ? 'Close search / add (Ctrl+K)' : 'Find or add node (Ctrl+K)'}
            aria-label={addNodeOpen ? 'Close search and add palette' : 'Open search and add palette'}
            style={{
              pointerEvents: 'auto',
              width: 44,
              height: 44,
              borderRadius: 999,
              border: addNodeOpen
                ? (theme === 'dark' ? '1px solid rgba(96,165,250,0.55)' : '1px solid rgba(37,99,235,0.50)')
                : '1px solid var(--border-subtle)',
              background: addNodeOpen
                ? (theme === 'dark' ? 'rgba(59,130,246,0.22)' : 'rgba(59,130,246,0.14)')
                : (theme === 'dark' ? 'rgba(0,0,0,0.55)' : 'rgba(255,255,255,0.80)'),
              color: 'var(--text-heading)',
              cursor: !lensId ? 'not-allowed' : 'pointer',
              opacity: !lensId ? 0.5 : 1,
              filter: theme === 'dark' ? 'drop-shadow(0 16px 40px rgba(0,0,0,0.55))' : 'drop-shadow(0 16px 40px rgba(0,0,0,0.18))',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              backdropFilter: 'blur(8px)',
              transition: 'background-color 0.18s ease, border-color 0.18s ease',
            }}
          >
            {Icons.Plus}
          </button>
        </div>
        )}

        {/* Perf-mode edge info (avoids heavy edge relabel/restyling) */}
        {perfMode && perfHoveredEdgeInfo && (
          <div
            style={{
              position: 'absolute',
              left: 12,
              bottom: 12,
              padding: '6px 10px',
              borderRadius: 10,
              background: theme === 'dark' ? 'rgba(0,0,0,0.55)' : 'rgba(255,255,255,0.75)',
              border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.12)' : 'rgba(0,0,0,0.12)'}`,
              color: 'var(--text-heading)',
              fontSize: 12,
              pointerEvents: 'none',
              backdropFilter: 'blur(6px)',
            }}
          >
            {perfHoveredEdgeInfo.edgeType}
          </div>
        )}

        {/* Overlay empty-state:
            show only when lens has a non-empty scope; for empty-lens flow keep canvas immediately interactive. */}
        {nodes.length === 0 && !loading && !lensMetaLoading && lensScopePaths.length > 0 && (
          <div
            style={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              pointerEvents: 'auto',
              padding: 16,
            }}
          >
            <div
              style={{
                width: 420,
                maxWidth: 'calc(100vw - 32px)',
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'center',
                textAlign: 'center',
              }}
            >
              {!lensId || !lensMetaLoading ? (
                <>
                  <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--text-heading)', marginBottom: 4 }}>
                    {lensName || 'Graph Scope'}
              </div>
                  <div style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 24 }}>
                     {scopePreview.length} object{scopePreview.length !== 1 ? 's' : ''} of focus
              </div>

                  <div
                    style={{
                      width: '100%',
                      textAlign: 'left',
                      background: theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'rgba(0,0,0,0.03)',
                      borderRadius: 12,
                      padding: '8px 0',
                      marginBottom: 24,
                    }}
                  >
                    {scopePreviewLoading ? (
                      <div style={{ padding: '12px', fontSize: 12, color: 'var(--text-secondary)', textAlign: 'center' }}>
                        Resolving scope...
            </div>
                    ) : scopePreview.length === 0 ? (
                      <div style={{ padding: '12px', fontSize: 12, color: 'var(--text-secondary)', textAlign: 'center' }}>
                        No objects configured
                      </div>
                    ) : (
                      <>
                        {scopePreviewDomains.length > 0 && (
                          <div
                            style={{
                              paddingTop: 2,
                              paddingBottom: scopePreviewNonDomains.length > 0 ? 6 : 2,
                              borderBottom:
                                scopePreviewNonDomains.length > 0
                                  ? (theme === 'dark' ? '1px solid rgba(255,255,255,0.08)' : '1px solid rgba(0,0,0,0.08)')
                                  : 'none',
                            }}
                          >
                            <div
                              style={{
                                padding: '4px 16px 6px',
                                fontSize: 10,
                                fontWeight: 800,
                                letterSpacing: '0.04em',
                                textTransform: 'uppercase',
                                color: theme === 'dark' ? 'rgba(251,191,36,0.92)' : 'rgba(180,83,9,0.92)',
                              }}
                            >
                              Domains & Artifacts ({scopePreviewDomains.length})
                            </div>
                            {scopePreviewDomains.slice(0, SCOPE_PREVIEW_SECTION_LIMIT).map((it) => {
                              const regionLabel = getDomainRegionLabel(it.canonicalPath);
                              const isArtifact = String(it.canonicalPath || '').startsWith('/artifact/');
                              return (
                                <div
                                  key={it.canonicalPath}
                                  style={{
                                    padding: '8px 16px',
                                    display: 'flex',
                                    alignItems: 'flex-start',
                                    gap: 10,
                                    borderBottom: '1px solid transparent',
                                  }}
                                >
                                  <div
                                    style={{
                                      width: 6,
                                      height: 6,
                                      borderRadius: '50%',
                                      background: theme === 'dark' ? 'rgba(251,191,36,0.95)' : '#D97706',
                                      flexShrink: 0,
                                      marginTop: 6,
                                    }}
                                  />
                                  {regionLabel ? (
                                    <span
                                      style={{
                                        fontSize: 10,
                                        fontWeight: 700,
                                        padding: '2px 6px',
                                        borderRadius: 4,
                                        background: theme === 'dark' ? 'rgba(251,191,36,0.2)' : 'rgba(180,83,9,0.2)',
                                        color: theme === 'dark' ? 'rgba(251,191,36,0.95)' : '#B45309',
                                        flexShrink: 0,
                                      }}
                                    >
                                      {regionLabel}
                                    </span>
                                  ) : null}
                                  <div style={{ flex: 1, minWidth: 0 }}>
                                    <div
                                      style={{
                                        overflow: 'hidden',
                                        textOverflow: 'ellipsis',
                                        whiteSpace: 'nowrap',
                                        fontSize: 13,
                                        fontWeight: 700,
                                        color: 'var(--text-heading)',
                                      }}
                                      title={it.name || it.canonicalPath}
                                    >
                                      {it.name || it.canonicalPath}
                                    </div>
                                    {(it.subtitle || it.meta) && (
                                      <div
                                        style={{
                                          marginTop: 2,
                                          fontSize: 11,
                                          color: 'var(--text-secondary-strong)',
                                          overflow: 'hidden',
                                          textOverflow: 'ellipsis',
                                          whiteSpace: 'nowrap',
                                        }}
                                        title={[it.subtitle, it.meta].filter(Boolean).join(' • ')}
                                      >
                                        {[it.subtitle, it.meta].filter(Boolean).join(' • ')}
                                      </div>
                                    )}
                                  </div>
                                  <div
                                    style={{
                                      fontSize: 10,
                                      color: isArtifact ? 'var(--text-secondary-strong)' : (theme === 'dark' ? 'rgba(251,191,36,0.9)' : 'rgba(180,83,9,0.9)'),
                                      textTransform: 'uppercase',
                                      fontWeight: 700,
                                      flexShrink: 0,
                                      opacity: 0.9,
                                    }}
                                  >
                                    {isArtifact
                                      ? (String(it.type || 'ARTIFACT').replace(/_/g, ' '))
                                      : (it.type || 'DOMAIN')}
                                  </div>
                                </div>
                              );
                            })}
                            {scopePreviewDomains.length > SCOPE_PREVIEW_SECTION_LIMIT && (
                              <div style={{ padding: '8px 16px', fontSize: 12, color: 'var(--text-secondary)', fontStyle: 'italic' }}>
                                ...and {scopePreviewDomains.length - SCOPE_PREVIEW_SECTION_LIMIT} more entries
                              </div>
                            )}
                          </div>
                        )}

                        {scopePreviewNonDomains.length > 0 && (
                          <div style={{ paddingTop: scopePreviewDomains.length > 0 ? 6 : 2, paddingBottom: 2 }}>
                            <div
                              style={{
                                padding: '4px 16px 6px',
                                fontSize: 10,
                                fontWeight: 800,
                                letterSpacing: '0.04em',
                                textTransform: 'uppercase',
                                color: 'var(--text-secondary-strong)',
                              }}
                            >
                              Scope ({scopePreviewNonDomains.length})
                            </div>
                            {scopePreviewNonDomains.slice(0, SCOPE_PREVIEW_SECTION_LIMIT).map((it) => {
                              const geoLabel = getScopeGeoLabel(it.canonicalPath);
                              return (
                                <div
                                  key={it.canonicalPath}
                                  style={{
                                    padding: '8px 16px',
                                    display: 'flex',
                                    alignItems: 'baseline',
                                    gap: 10,
                                    borderBottom: '1px solid transparent',
                                  }}
                                >
                                  <div
                                    style={{
                                      width: 6,
                                      height: 6,
                                      borderRadius: '50%',
                                      background: 'var(--accent-primary, #3B82F6)',
                                      flexShrink: 0,
                                      marginTop: 6,
                                    }}
                                  />
                                  {geoLabel ? (
                                    <span
                                      style={{
                                        fontSize: 10,
                                        fontWeight: 700,
                                        padding: '2px 6px',
                                        borderRadius: 4,
                                        background: theme === 'dark' ? 'rgba(251,191,36,0.2)' : 'rgba(180,83,9,0.2)',
                                        color: theme === 'dark' ? 'rgba(251,191,36,0.95)' : '#B45309',
                                        flexShrink: 0,
                                      }}
                                    >
                                      {geoLabel}
                                    </span>
                                  ) : null}
                                  <div
                                    style={{
                                      flex: 1,
                                      overflow: 'hidden',
                                      textOverflow: 'ellipsis',
                                      whiteSpace: 'nowrap',
                                      fontSize: 13,
                                      fontWeight: 600,
                                      color: 'var(--text-heading)',
                                    }}
                                    title={it.name || it.canonicalPath}
                                  >
                                    {it.name || it.canonicalPath}
                                  </div>
                                  {it.type && (
                                    <div
                                      style={{
                                        fontSize: 10,
                                        color: 'var(--text-secondary-strong)',
                                        textTransform: 'uppercase',
                                        fontWeight: 600,
                                        flexShrink: 0,
                                        opacity: 0.7,
                                      }}
                                    >
                                      {it.type}
                                    </div>
                                  )}
                                </div>
                              );
                            })}
                            {scopePreviewNonDomains.length > SCOPE_PREVIEW_SECTION_LIMIT && (
                              <div style={{ padding: '8px 16px', fontSize: 12, color: 'var(--text-secondary)', fontStyle: 'italic' }}>
                                ...and {scopePreviewNonDomains.length - SCOPE_PREVIEW_SECTION_LIMIT} more
                              </div>
                            )}
                          </div>
                        )}
                      </>
                    )}
                  </div>
                </>
              ) : (
                <div style={{ width: '100%', marginBottom: 24 }}>
                  <style>{`
                    @keyframes horizon-skeleton-shimmer {
                      0% { background-position: -400px 0; }
                      100% { background-position: 400px 0; }
                    }
                    .horizon-skeleton {
                      background: linear-gradient(
                        90deg,
                        rgba(255,255,255,0.06) 0%,
                        rgba(255,255,255,0.10) 40%,
                        rgba(255,255,255,0.06) 80%
                      );
                      background-size: 800px 100%;
                      animation: horizon-skeleton-shimmer 1.2s infinite linear;
                    }
                    .horizon-skeleton.light {
                      background: linear-gradient(
                        90deg,
                        rgba(0,0,0,0.04) 0%,
                        rgba(0,0,0,0.08) 40%,
                        rgba(0,0,0,0.04) 80%
                      );
                      background-size: 800px 100%;
                      animation: horizon-skeleton-shimmer 1.2s infinite linear;
                    }
                  `}</style>

                  <div
                    className={`horizon-skeleton ${theme === 'dark' ? '' : 'light'}`}
                    style={{
                      height: 18,
                      width: '60%',
                      borderRadius: 10,
                      margin: '0 auto 10px auto',
                    }}
                  />
                  <div
                    className={`horizon-skeleton ${theme === 'dark' ? '' : 'light'}`}
                    style={{
                      height: 12,
                      width: '45%',
                      borderRadius: 10,
                      margin: '0 auto 18px auto',
                      opacity: 0.9,
                    }}
                  />

                  <div
                    style={{
                      width: '100%',
                      textAlign: 'left',
                      background: theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'rgba(0,0,0,0.03)',
                      borderRadius: 12,
                      padding: '10px 0',
                    }}
                  >
                    {[0, 1, 2, 3, 4].map((i) => (
                      <div
                        key={i}
                        style={{
                          padding: '8px 16px',
                          display: 'flex',
                          alignItems: 'center',
                          gap: 10,
                        }}
                      >
                        <div
                          className={`horizon-skeleton ${theme === 'dark' ? '' : 'light'}`}
                          style={{ width: 8, height: 8, borderRadius: 999, flexShrink: 0, opacity: 0.9 }}
                        />
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <div
                            className={`horizon-skeleton ${theme === 'dark' ? '' : 'light'}`}
                            style={{ height: 12, borderRadius: 10, width: `${70 - i * 6}%` }}
                          />
                        </div>
                        <div
                          className={`horizon-skeleton ${theme === 'dark' ? '' : 'light'}`}
                          style={{ height: 10, borderRadius: 999, width: 60, opacity: 0.7 }}
                        />
                      </div>
                    ))}
                  </div>

                  <div style={{ marginTop: 10, fontSize: 12, color: 'var(--text-secondary)', textAlign: 'center' }}>
                    Loading lens scope…
                  </div>
                </div>
              )}

              <button
                type="button"
                onClick={() => void handleLoadGraph()}
                disabled={!lensId || loading}
                style={{
                  height: 40,
                  padding: '0 24px',
                  borderRadius: 20,
                  background: 'var(--accent-primary, #3B82F6)',
                  color: '#fff',
                  border: 'none',
                  fontSize: 14,
                  fontWeight: 600,
                  cursor: (!lensId || loading) ? 'not-allowed' : 'pointer',
                  opacity: (!lensId || loading) ? 0.7 : 1,
                  boxShadow: '0 4px 12px rgba(59, 130, 246, 0.3)',
                  transition: 'all 0.2s',
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                }}
              >
                  <span aria-hidden="true" style={{ display: 'inline-flex' }}>{Icons.Refresh}</span>
                  Load Graph
              </button>
            </div>
          </div>
        )}

        {/* Loading Overlay */}
        {loading && (
          <div
            style={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              background: theme === 'dark' ? 'rgba(0,0,0,0.2)' : 'rgba(255,255,255,0.4)',
              backdropFilter: 'blur(3px)',
              zIndex: 50,
              pointerEvents: 'all', // Block interactions while loading
            }}
          >
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 12,
                padding: '16px 24px',
                borderRadius: 16,
                background: theme === 'dark' ? 'var(--bg-elevated)' : 'white',
                boxShadow: '0 10px 40px rgba(0,0,0,0.2)',
                border: '1px solid var(--border-subtle)',
              }}
            >
              <style>{`
                @keyframes horizon-pulse-dot {
                  0%, 100% { transform: scale(0.6); opacity: 0.4; }
                  50% { transform: scale(1); opacity: 1; }
                }
              `}</style>
              {[0, 1, 2].map((i) => (
                <div
                  key={i}
                  style={{
                    width: 10,
                    height: 10,
                    borderRadius: '50%',
                    background: 'var(--accent-primary, #3B82F6)',
                    animation: `horizon-pulse-dot 1.2s infinite ease-in-out`,
                    animationDelay: `${i * 0.2}s`,
                  }}
                />
              ))}
              <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-heading)' }}>
                Building graph...
              </div>
            </div>
          </div>
        )}

      </div>

      {/* Search / Add Command Palette Overlay */}
      {addNodeOpen && (
        <div
          style={{
            position: 'absolute',
            inset: 0,
            zIndex: 260,
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'flex-end',
            paddingBottom: 72,
            background: theme === 'dark' ? 'rgba(0,0,0,0.5)' : 'rgba(255,255,255,0.4)',
            backdropFilter: 'blur(4px)',
            animation: 'horizon-command-backdrop-in 0.12s ease-out',
          }}
          onClick={() => closeAddNode()}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              gap: 8,
              marginBottom: 8,
            }}
          >
            {graphIntentLoading && !graphIntentPlan && (
              <>
                <style>{`
                  @keyframes horizon-command-dot-bounce {
                    0%, 80%, 100% { transform: translateY(0); opacity: 0.4; }
                    40% { transform: translateY(-6px); opacity: 1; }
                  }
                `}</style>
                <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  {[0, 1, 2].map((i) => (
                    <div
                      key={i}
                      style={{
                        width: 10,
                        height: 10,
                        borderRadius: 999,
                        background: 'var(--accent-primary, #3B82F6)',
                        animation: 'horizon-command-dot-bounce 1.1s infinite ease-in-out',
                        animationDelay: `${i * 0.14}s`,
                      }}
                    />
                  ))}
                </div>
              </>
            )}
            {graphIntentPlan && (
              <GraphIntentResultCard
                plan={graphIntentPlan}
                applyLoading={graphIntentApplyLoading}
                onApply={() => void applyGraphIntentPlan()}
                onDismiss={dismissGraphIntentPlan}
                theme={theme as 'light' | 'dark'}
                nodeIdToLabel={graphIntentNodeIdToLabel}
              />
            )}
            {graphIntentError && !graphIntentPlan && (
              <div
                style={{
                  width: 520,
                  maxWidth: '90vw',
                  borderRadius: 16,
                  border: '1px solid rgba(239,68,68,0.5)',
                  background: theme === 'dark' ? 'rgba(239,68,68,0.15)' : 'rgba(239,68,68,0.08)',
                  color: 'var(--text-heading)',
                  padding: '16px 20px',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 12,
                }}
              >
                <span style={{ fontSize: 13, flex: 1 }}>{graphIntentError}</span>
                <button
                  type="button"
                  onClick={() => setGraphIntentError(null)}
                  style={{
                    padding: '6px 14px',
                    borderRadius: 8,
                    border: '1px solid var(--border-subtle)',
                    background: 'transparent',
                    color: 'var(--text-secondary)',
                    fontSize: 13,
                    fontWeight: 600,
                    cursor: 'pointer',
                  }}
                >
                  Dismiss
                </button>
              </div>
            )}
          </div>
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              width: 520,
              maxWidth: '90vw',
              display: 'flex',
              flexDirection: 'column',
              background: theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
              border: '1px solid var(--border-subtle)',
              borderRadius: 16,
              boxShadow: theme === 'dark' ? '0 20px 60px rgba(0,0,0,0.6)' : '0 20px 60px rgba(0,0,0,0.2)',
              overflow: 'hidden',
              animation: 'horizon-command-rise 0.2s cubic-bezier(0.2, 0.9, 0.2, 1)',
              transformOrigin: '50% 100%',
            }}
          >
            <style>{`
              @keyframes horizon-command-backdrop-in {
                from { opacity: 0; }
                to { opacity: 1; }
              }
              @keyframes horizon-command-rise {
                from { opacity: 0; transform: translateY(14px) scale(0.98); }
                to { opacity: 1; transform: scale(1) translateY(0); }
              }
            `}</style>

            <div style={{ display: 'flex', alignItems: 'center', padding: '14px 16px', borderBottom: '1px solid var(--border-subtle)' }}>
              <div style={{ marginRight: 12, color: 'var(--text-secondary-strong)', display: 'flex' }}>
                {Icons.Search}
              </div>
              <input
                ref={addNodeInputRef}
                value={addNodeQuery}
                onChange={(e) => {
                  setAddNodeQuery(e.target.value);
                  setAddNodeActiveIndex(0);
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') {
                    e.preventDefault();
                    closeAddNode();
                    return;
                  }
                  if (e.key === 'ArrowDown') {
                    e.preventDefault();
                    setAddNodeActiveIndex((i) => Math.min(i + 1, Math.max(0, paletteSelectableItems.length - 1)));
                    return;
                  }
                  if (e.key === 'ArrowUp') {
                    e.preventDefault();
                    setAddNodeActiveIndex((i) => Math.max(0, i - 1));
                    return;
                  }
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    const picked = paletteSelectableItems[addNodeActiveIndex];
                    if (!picked) return;
                    if (picked.kind === 'intent') {
                      void executeGraphIntentForQuery(picked.query);
                      return;
                    }
                    if (picked.kind === 'scout') {
                      openScoutIntentModal(picked.query);
                      return;
                    }
                    if (picked.kind === 'graph') {
                      focusGraphNodeFromPalette(picked.node.id);
                      return;
                    }
                    if (picked.kind === 'index') {
                      void addNodeToLens(picked.entity);
                      return;
                    }
                    return;
                  }
                }}
                placeholder="What should I do?"
                autoComplete="off"
                spellCheck={false}
                style={{
                  flex: 1,
                  border: 'none',
                  outline: 'none',
                  background: 'transparent',
                  color: 'var(--text-heading)',
                  fontSize: 16,
                  fontWeight: 500,
                }}
              />
              {addNodeLoading && (
                <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>Loading…</div>
              )}
            </div>

            <div style={{ maxHeight: 400, overflowY: 'auto', padding: '6px 0' }}>
              {!addNodeLoading && !addNodeError && !addNodeQueryTrim && (
                <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-secondary-strong)', fontSize: 13 }}>
                  {addNodeSearchEnabled
                    ? 'Type to search nodes on this graph, or add from the global index'
                    : 'Type for Graph Intent or Scout (Search off — no graph/index results)'}
                </div>
              )}

              {/* Execute intent (Graph Intent) */}
              {addNodeQueryTrim.length >= 3 && (
                <div>
                  <div style={{ padding: '10px 16px 6px', fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
                    Graph Intent
                  </div>
                  {(() => {
                    const absIdx = 0;
                    const isActive = absIdx === addNodeActiveIndex;
                    return (
                      <button
                        key={`intent-${addNodeQueryTrim}`}
                        type="button"
                        onClick={() => void executeGraphIntentForQuery(addNodeQueryTrim)}
                        onMouseEnter={() => setAddNodeActiveIndex(absIdx)}
                        disabled={graphIntentLoading}
                        style={{
                          width: '100%',
                          textAlign: 'left',
                          padding: '10px 16px',
                          border: 'none',
                          borderLeft: `3px solid ${isActive ? 'var(--accent-primary, #3B82F6)' : 'transparent'}`,
                          background: isActive ? (theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)') : 'transparent',
                          cursor: graphIntentLoading ? 'not-allowed' : 'pointer',
                          display: 'flex',
                          flexDirection: 'column',
                          gap: 2,
                          transition: 'background 0.1s',
                        }}
                      >
                        <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12 }}>
                          <div style={{ fontSize: 14, fontWeight: 650, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            Execute intent: &quot;{addNodeQueryTrim}&quot;
                          </div>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
                            <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', border: '1px solid var(--border-subtle)', borderRadius: 999, padding: '2px 8px', background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)' }}>
                              {graphIntentLoading ? '…' : 'Plan'}
                            </div>
                          </div>
                        </div>
                        <div style={{ fontSize: 12, color: 'var(--text-secondary)', opacity: 0.9 }}>
                          {graphIntentLoading ? 'Request is being processed...' : 'Build structure from NL query; preview then apply to graph.'}
                        </div>
                      </button>
                    );
                  })()}
                </div>
              )}

              {/* Scout from query - hidden when loading */}
              {addNodeQueryTrim.length >= 2 && !graphIntentLoading && (
                <div>
                  <div style={{ padding: '10px 16px 6px', fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
                    Scout
                  </div>
                  {(() => {
                    const absIdx = addNodeQueryTrim.length >= 3 ? 1 : 0;
                    const isActive = absIdx === addNodeActiveIndex;
                    return (
                      <button
                        key={`scout-${addNodeQueryTrim}`}
                        type="button"
                        onClick={() => openScoutIntentModal(addNodeQueryTrim)}
                        onMouseEnter={() => setAddNodeActiveIndex(absIdx)}
                        style={{
                          width: '100%',
                          textAlign: 'left',
                          padding: '10px 16px',
                          border: 'none',
                          borderLeft: `3px solid ${isActive ? 'var(--accent-primary, #3B82F6)' : 'transparent'}`,
                          background: isActive ? (theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)') : 'transparent',
                          cursor: 'pointer',
                          display: 'flex',
                          flexDirection: 'column',
                          gap: 2,
                          transition: 'background 0.1s',
                        }}
                      >
                        <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12 }}>
                          <div style={{ fontSize: 14, fontWeight: 650, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            Scout for “{addNodeQueryTrim}”
                          </div>
                          <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
                            <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', border: '1px solid var(--border-subtle)', borderRadius: 999, padding: '2px 8px', background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)' }}>
                              Create
                            </div>
                          </div>
                        </div>
                        <div style={{ fontSize: 12, color: 'var(--text-secondary)', opacity: 0.9 }}>
                          Full expansion: anchors, domains, first 5 to graph.
                        </div>
                      </button>
                    );
                  })()}
                </div>
              )}

              {addNodeError && (
                <div style={{ padding: '16px', textAlign: 'center', color: '#EF4444', fontSize: 13 }}>
                  {addNodeError}
                </div>
              )}

              {/* On-graph matches - hidden when loading or when Search is off */}
              {addNodeSearchEnabled && graphSearchResults.length > 0 && !graphIntentLoading && (
                <div style={{ padding: '10px 16px 6px', fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
                  On graph
                </div>
              )}
              {addNodeSearchEnabled && graphSearchResults.map((m, idx) => {
                const intentOffset = addNodeQueryTrim.length >= 3 ? 1 : 0;
                const scoutOffset = addNodeQueryTrim.length >= 2 ? 1 : 0;
                const absIdx = intentOffset + scoutOffset + idx;
                const isActive = absIdx === addNodeActiveIndex;
                return (
                  <button
                    key={`graph-${m.node.id}-${idx}`}
                    type="button"
                    onClick={() => focusGraphNodeFromPalette(m.node.id)}
                    onMouseEnter={() => setAddNodeActiveIndex(absIdx)}
                    style={{
                      width: '100%',
                      textAlign: 'left',
                      padding: '10px 16px',
                      border: 'none',
                      borderLeft: `3px solid ${isActive ? 'var(--accent-primary, #3B82F6)' : 'transparent'}`,
                      background: isActive ? (theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)') : 'transparent',
                      cursor: 'pointer',
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 2,
                      transition: 'background 0.1s',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12 }}>
                      <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {m.label}
                      </div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
                        {m.type && (
                          <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-secondary-strong)', textTransform: 'uppercase', opacity: 0.8 }}>
                            {m.type}
                          </div>
                        )}
                        <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', border: '1px solid var(--border-subtle)', borderRadius: 999, padding: '2px 8px', background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)' }}>
                          Focus
                        </div>
                      </div>
                    </div>
                    {m.canonicalPath && (
                      <div style={{ fontSize: 12, color: 'var(--text-secondary)', fontFamily: 'monospace', opacity: 0.8, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {m.canonicalPath}
                      </div>
                    )}
                  </button>
                );
              })}

              {/* Index matches (global) - hidden when loading or when Search is off */}
              {addNodeSearchEnabled && addNodeQueryTrim.length >= 2 && !graphIntentLoading && (
                <div style={{ padding: `${graphSearchResults.length > 0 ? '12px' : '10px'} 16px 6px`, fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
                  Add to graph
                </div>
              )}

              {addNodeSearchEnabled && addNodeQueryTrim.length >= 2 && !graphIntentLoading && !addNodeLoading && !addNodeError && filteredAddNodeResults.length === 0 && (
                <div style={{ padding: '16px 24px', textAlign: 'center', color: 'var(--text-secondary-strong)', fontSize: 13 }}>
                  No results for "{addNodeQueryTrim}"
                </div>
              )}

              {!graphIntentLoading && filteredAddNodeResults.map((r, idx) => {
                const intentOffset = addNodeQueryTrim.length >= 3 ? 1 : 0;
                const scoutOffset = addNodeQueryTrim.length >= 2 ? 1 : 0;
                const absIdx = intentOffset + scoutOffset + graphSearchResults.length + idx;
                const isActive = absIdx === addNodeActiveIndex;
                return (
                  <button
                    key={`${r.canonicalPath}-${idx}`}
                    type="button"
                    onClick={() => void addNodeToLens(r)}
                    onMouseEnter={() => setAddNodeActiveIndex(absIdx)}
                    style={{
                      width: '100%',
                      textAlign: 'left',
                      padding: '10px 16px',
                      border: 'none',
                      borderLeft: `3px solid ${isActive ? 'var(--accent-primary, #3B82F6)' : 'transparent'}`,
                      background: isActive ? (theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)') : 'transparent',
                      cursor: 'pointer',
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 2,
                      transition: 'background 0.1s',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12 }}>
                      <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {r.name}
                      </div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
                        <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-secondary-strong)', textTransform: 'uppercase', opacity: 0.8 }}>
                          {r.type}
                        </div>
                        <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-secondary-strong)', border: '1px solid var(--border-subtle)', borderRadius: 999, padding: '2px 8px', background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)' }}>
                          Add
                        </div>
                      </div>
                    </div>
                    <div style={{ fontSize: 12, color: 'var(--text-secondary)', fontFamily: 'monospace', opacity: 0.8, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {r.canonicalPath}
                    </div>
                    {r.description && (
                      <div style={{ marginTop: 2, fontSize: 12, color: 'var(--text-secondary-strong)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {r.description}
                      </div>
                    )}
                  </button>
                );
              })}
            </div>
            
            <div
              style={{
                padding: '8px 12px',
                borderTop: '1px solid var(--border-subtle)',
                background: theme === 'dark' ? 'rgba(255,255,255,0.02)' : 'rgba(0,0,0,0.01)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 12,
                fontSize: 11,
                color: 'var(--text-secondary-strong)',
              }}
            >
              <button
                type="button"
                onClick={() => setAddNodeSearchEnabled((v) => !v)}
                title={addNodeSearchEnabled ? 'Search on: show nodes from graph and global index (click to disable)' : 'Search off: only Graph Intent and Scout (click to enable)'}
                aria-pressed={addNodeSearchEnabled}
                aria-label={addNodeSearchEnabled ? 'Search enabled' : 'Search disabled'}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  gap: 4,
                  padding: '1px 4px',
                  borderRadius: 4,
                  border: 'none',
                  background: addNodeSearchEnabled ? (theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(0,0,0,0.04)') : 'transparent',
                  color: 'var(--text-secondary-strong)',
                  cursor: 'pointer',
                  fontSize: 11,
                  lineHeight: 1,
                  transition: 'background 0.12s',
                }}
              >
                <span style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', opacity: addNodeSearchEnabled ? 1 : 0.5, flexShrink: 0, lineHeight: 0 }}>{Icons.Search}</span>
                <span>Search</span>
              </button>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                  <kbd style={{ background: 'var(--bg-base)', border: '1px solid var(--border-subtle)', borderRadius: 4, padding: '1px 4px', fontSize: 10 }}>↵</kbd>
                  <span>to focus / add</span>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                  <kbd style={{ background: 'var(--bg-base)', border: '1px solid var(--border-subtle)', borderRadius: 4, padding: '1px 4px', fontSize: 10 }}>esc</kbd>
                  <span>to close</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}


      {/* Policy Impact Modeler Overlay */}
      {modelerOpen && (
        <div
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 9997,
            display: 'flex',
            alignItems: 'flex-start',
            justifyContent: 'center',
            paddingTop: '8vh',
            background: theme === 'dark' ? 'rgba(0,0,0,0.52)' : 'rgba(255,255,255,0.42)',
            backdropFilter: 'blur(4px)',
          }}
          onClick={() => closeModeler()}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              width: 980,
              maxWidth: '96vw',
              maxHeight: '88vh',
              overflowY: 'auto',
              display: 'flex',
              flexDirection: 'column',
              gap: 12,
              background: theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
              border: '1px solid var(--border-subtle)',
              borderRadius: 16,
              boxShadow: theme === 'dark' ? '0 22px 64px rgba(0,0,0,0.62)' : '0 22px 64px rgba(0,0,0,0.22)',
              padding: 14,
              animation: 'horizon-command-pop 0.15s ease-out',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
              <div style={{ minWidth: 0 }}>
                <div style={{ fontSize: 14, fontWeight: 900, color: 'var(--text-heading)' }}>
                  Policy Impact Modeler
                </div>
                <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', marginTop: 2 }}>
                  Encode -&gt; Transfer (12-axis propagation) -&gt; Decode
                </div>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                {isAdmin && (
                  <button
                    type="button"
                    onClick={() => useUIStore.getState().openAdminModal()}
                    title="Admin panel"
                    style={{
                      padding: '6px 10px',
                      borderRadius: 10,
                      border: '1px solid var(--border-subtle)',
                      background: 'transparent',
                      color: 'var(--text-secondary-strong)',
                      cursor: 'pointer',
                      fontSize: 12,
                      fontWeight: 700,
                    }}
                  >
                    Admin
                  </button>
                )}
                <button
                  type="button"
                  onClick={() => closeModeler()}
                style={{
                  width: 30,
                  height: 30,
                  borderRadius: 10,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-secondary-strong)',
                  cursor: 'pointer',
                  fontSize: 16,
                  fontWeight: 900,
                }}
              >
                {Icons.X}
              </button>
              </div>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(240px, 1fr))', gap: 10 }}>
              {MODELER_SCENARIO_PRESETS.map((p) => {
                const active = modelerPresetId === p.id;
                return (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => setModelerPreset(p.id)}
                    style={{
                      textAlign: 'left',
                      borderRadius: 12,
                      border: active
                        ? (theme === 'dark' ? '1px solid rgba(96,165,250,0.55)' : '1px solid rgba(37,99,235,0.50)')
                        : '1px solid var(--border-subtle)',
                      background: active
                        ? (theme === 'dark' ? 'rgba(59,130,246,0.16)' : 'rgba(59,130,246,0.10)')
                        : (theme === 'dark' ? 'rgba(255,255,255,0.02)' : 'rgba(0,0,0,0.015)'),
                      padding: '10px 12px',
                      cursor: 'pointer',
                      color: 'var(--text-heading)',
                    }}
                  >
                    <div style={{ fontSize: 13, fontWeight: 900 }}>{p.label}</div>
                    <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 3 }}>{p.description}</div>
                  </button>
                );
              })}
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(160px, 1fr))', gap: 10 }}>
              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: '8px 10px', background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.015)' }}>
                <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)', textTransform: 'uppercase', letterSpacing: 0.35 }}>VAT change</div>
                <div style={{ fontSize: 18, fontWeight: 900, color: 'var(--text-heading)', marginTop: 3 }}>
                  {(modelerVatTo - modelerVatFrom).toFixed(2)} pp
                </div>
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{modelerVatFrom.toFixed(2)}% → {modelerVatTo.toFixed(2)}%</div>
              </div>
              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: '8px 10px', background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.015)' }}>
                <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)', textTransform: 'uppercase', letterSpacing: 0.35 }}>Baseline VAT</div>
                <div style={{ fontSize: 18, fontWeight: 900, color: 'var(--text-heading)', marginTop: 3 }}>
                  {modelerBaselineVatRevenue.toFixed(0)}
                </div>
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>bln KZT</div>
              </div>
              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: '8px 10px', background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.015)' }}>
                <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)', textTransform: 'uppercase', letterSpacing: 0.35 }}>Target nodes</div>
                <div style={{ fontSize: 18, fontWeight: 900, color: 'var(--text-heading)', marginTop: 3 }}>
                  {modelerTargetPaths.length}
                </div>
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>seed scope</div>
              </div>
              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: '8px 10px', background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.015)' }}>
                <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)', textTransform: 'uppercase', letterSpacing: 0.35 }}>Mode</div>
                <div style={{ fontSize: 18, fontWeight: 900, color: 'var(--text-heading)', marginTop: 3 }}>
                  {modelerMode === 'apply' ? 'Apply' : 'Preview'}
                </div>
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>{modelerMode === 'apply' ? 'Persists decode' : 'No persistence'}</div>
              </div>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(260px, 1fr))', gap: 12 }}>
              <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)', fontWeight: 700 }}>Jurisdiction path</span>
                <input
                  value={modelerLocationPath}
                  onChange={(e) => updateModelerParameter('location_path', String(e.target.value || '').trim())}
                  spellCheck={false}
                  style={{
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-base)',
                    color: 'var(--text-heading)',
                    borderRadius: 10,
                    padding: '8px 10px',
                    fontSize: 12,
                    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
                  }}
                />
              </label>
              <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)', fontWeight: 700 }}>Scenario ID</span>
                <input
                  value={modelerScenarioId}
                  onChange={(e) => setModelerScenarioId(e.target.value)}
                  placeholder="scenario id"
                  spellCheck={false}
                  style={{
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-base)',
                    color: 'var(--text-heading)',
                    borderRadius: 10,
                    padding: '8px 10px',
                    fontSize: 12,
                  }}
                />
              </label>
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(260px, 1fr))', gap: 12 }}>
              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: 10 }}>
                <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', marginBottom: 8 }}>VAT rates</div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
                  <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>from (%)</span>
                    <input
                      type="number"
                      step="0.1"
                      min={0}
                      max={100}
                      value={Number.isFinite(modelerVatFrom) ? modelerVatFrom : 0}
                      onChange={(e) => updateModelerParameter('vat_rate_from', Number(e.target.value))}
                      style={{ border: '1px solid var(--border-subtle)', background: 'var(--bg-base)', color: 'var(--text-heading)', borderRadius: 9, padding: '7px 9px', fontSize: 12 }}
                    />
                  </label>
                  <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>to (%)</span>
                    <input
                      type="number"
                      step="0.1"
                      min={0}
                      max={100}
                      value={Number.isFinite(modelerVatTo) ? modelerVatTo : 0}
                      onChange={(e) => updateModelerParameter('vat_rate_to', Number(e.target.value))}
                      style={{ border: '1px solid var(--border-subtle)', background: 'var(--bg-base)', color: 'var(--text-heading)', borderRadius: 9, padding: '7px 9px', fontSize: 12 }}
                    />
                  </label>
                </div>
              </div>

              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: 10 }}>
                <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', marginBottom: 8 }}>Baselines</div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
                  <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>GDP (bln KZT)</span>
                    <input
                      type="number"
                      step="100"
                      value={Number.isFinite(modelerBaselineGDP) ? modelerBaselineGDP : 0}
                      onChange={(e) => updateModelerParameter('baseline_gdp_bln_kzt', Number(e.target.value))}
                      style={{ border: '1px solid var(--border-subtle)', background: 'var(--bg-base)', color: 'var(--text-heading)', borderRadius: 9, padding: '7px 9px', fontSize: 12 }}
                    />
                  </label>
                  <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>VAT revenue (bln KZT)</span>
                    <input
                      type="number"
                      step="10"
                      value={Number.isFinite(modelerBaselineVatRevenue) ? modelerBaselineVatRevenue : 0}
                      onChange={(e) => updateModelerParameter('baseline_vat_revenue_bln_kzt', Number(e.target.value))}
                      style={{ border: '1px solid var(--border-subtle)', background: 'var(--bg-base)', color: 'var(--text-heading)', borderRadius: 9, padding: '7px 9px', fontSize: 12 }}
                    />
                  </label>
                </div>
              </div>
            </div>

            <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: 10, display: 'grid', gap: 10 }}>
              <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>Behavior assumptions</div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(220px, 1fr))', gap: 10 }}>
                {[
                  { key: 'pass_through_rate', label: 'Pass-through', value: modelerPassThrough, min: 0.30, max: 1.00, step: 0.01 },
                  { key: 'demand_elasticity', label: 'Demand elasticity', value: modelerDemandElasticity, min: -1.20, max: 0.00, step: 0.01 },
                  { key: 'compliance_elasticity', label: 'Compliance elasticity', value: modelerComplianceElasticity, min: 0.02, max: 0.45, step: 0.01 },
                  { key: 'shadow_sensitivity', label: 'Shadow sensitivity', value: modelerShadowSensitivity, min: 0.02, max: 0.65, step: 0.01 },
                ].map((row) => (
                  <label key={row.key} style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
                      <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)', fontWeight: 700 }}>{row.label}</span>
                      <span style={{ fontSize: 11, color: 'var(--text-heading)', fontWeight: 800 }}>{Number(row.value).toFixed(2)}</span>
                    </div>
                    <input
                      type="range"
                      min={row.min}
                      max={row.max}
                      step={row.step}
                      value={Number.isFinite(row.value) ? row.value : row.min}
                      onChange={(e) => updateModelerParameter(row.key, Number(e.target.value))}
                    />
                  </label>
                ))}
              </div>
            </div>

            <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: 10, display: 'grid', gap: 8 }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
                <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>Target paths (seed scope)</div>
                <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                  <button
                    type="button"
                    onClick={() => setModelerTargetPaths(selectedNodeCanonicalPath ? [selectedNodeCanonicalPath] : [])}
                    disabled={!selectedNodeCanonicalPath}
                    style={{ padding: '4px 8px', borderRadius: 999, border: '1px solid var(--border-subtle)', background: 'transparent', color: 'var(--text-secondary-strong)', fontSize: 11, cursor: selectedNodeCanonicalPath ? 'pointer' : 'not-allowed', opacity: selectedNodeCanonicalPath ? 1 : 0.45 }}
                  >
                    Use selected
                  </button>
                  <button
                    type="button"
                    onClick={() => setModelerTargetPaths(suggestedModelerTargetPaths.slice(0, 12))}
                    disabled={suggestedModelerTargetPaths.length === 0}
                    style={{ padding: '4px 8px', borderRadius: 999, border: '1px solid var(--border-subtle)', background: 'transparent', color: 'var(--text-secondary-strong)', fontSize: 11, cursor: suggestedModelerTargetPaths.length > 0 ? 'pointer' : 'not-allowed', opacity: suggestedModelerTargetPaths.length > 0 ? 1 : 0.45 }}
                  >
                    Use scope
                  </button>
                  <button
                    type="button"
                    onClick={() => setModelerTargetPaths([])}
                    style={{ padding: '4px 8px', borderRadius: 999, border: '1px solid var(--border-subtle)', background: 'transparent', color: 'var(--text-secondary-strong)', fontSize: 11, cursor: 'pointer' }}
                  >
                    Clear
                  </button>
                </div>
              </div>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', maxHeight: 108, overflowY: 'auto', paddingRight: 2 }}>
                {suggestedModelerTargetPaths.slice(0, 20).map((p) => {
                  const active = modelerTargetPaths.includes(p);
                  return (
                    <button
                      key={p}
                      type="button"
                      onClick={() => toggleModelerTargetPath(p)}
                      style={{
                        maxWidth: '100%',
                        borderRadius: 999,
                        border: active
                          ? (theme === 'dark' ? '1px solid rgba(34,197,94,0.45)' : '1px solid rgba(16,185,129,0.50)')
                          : '1px solid var(--border-subtle)',
                        background: active
                          ? (theme === 'dark' ? 'rgba(16,185,129,0.16)' : 'rgba(16,185,129,0.10)')
                          : 'transparent',
                        color: active ? 'var(--text-heading)' : 'var(--text-secondary-strong)',
                        padding: '4px 9px',
                        fontSize: 11,
                        cursor: 'pointer',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
                      }}
                    >
                      {p}
                    </button>
                  );
                })}
              </div>
              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                If no targets are selected, seeds are inferred from <code>location_path</code> via <code>OPERATES_IN/LOCATED_IN</code>.
              </div>
              <div style={{ fontSize: 11, color: selectedNodeFragmentId ? 'var(--text-heading)' : 'var(--text-secondary-strong)' }}>
                Seed source: {selectedNodeFragmentId ? <code>{selectedNodeFragmentId}</code> : 'no selected visible node (fallback to inferred seeds)'}
              </div>
            </div>

            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10, flexWrap: 'wrap' }}>
              <button
                type="button"
                onClick={() => setModelerAdvancedOpen((v) => !v)}
                style={{
                  height: 34,
                  padding: '0 12px',
                  borderRadius: 10,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-secondary-strong)',
                  fontSize: 12,
                  fontWeight: 800,
                  cursor: 'pointer',
                }}
              >
                {modelerAdvancedOpen ? 'Hide advanced' : 'Advanced settings'}
              </button>

              <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                <button
                  type="button"
                  onClick={() => {
                    setModelerMode('preview');
                    void runModelerScenario('preview');
                  }}
                  disabled={modelerLoading}
                  style={{
                    height: 36,
                    padding: '0 14px',
                    borderRadius: 10,
                    border: '1px solid var(--border-subtle)',
                    background: modelerMode === 'preview' ? (theme === 'dark' ? 'rgba(59,130,246,0.22)' : 'rgba(59,130,246,0.14)') : 'transparent',
                    color: 'var(--text-heading)',
                    fontWeight: 900,
                    fontSize: 12,
                    cursor: modelerLoading ? 'not-allowed' : 'pointer',
                    opacity: modelerLoading ? 0.6 : 1,
                  }}
                >
                  Preview impact
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setModelerMode('apply');
                    void runModelerScenario('apply');
                  }}
                  disabled={modelerLoading}
                  style={{
                    height: 36,
                    padding: '0 14px',
                    borderRadius: 10,
                    border: 'none',
                    background: 'var(--accent-primary, #3B82F6)',
                    color: '#fff',
                    fontWeight: 900,
                    fontSize: 12,
                    cursor: modelerLoading ? 'not-allowed' : 'pointer',
                    opacity: modelerLoading ? 0.6 : 1,
                    boxShadow: '0 8px 20px rgba(59,130,246,0.25)',
                  }}
                >
                  Apply to graph
                </button>
              </div>
            </div>

            {modelerAdvancedOpen && (
              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: 10, display: 'grid', gap: 8 }}>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(160px, 1fr))', gap: 8 }}>
                  <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)', fontWeight: 700 }}>Contract type</span>
                    <input value={modelerContractType} onChange={(e) => setModelerContractType(e.target.value)} spellCheck={false} style={{ border: '1px solid var(--border-subtle)', background: 'var(--bg-base)', color: 'var(--text-heading)', borderRadius: 9, padding: '7px 9px', fontSize: 12 }} />
                  </label>
                  <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)', fontWeight: 700 }}>Domain modes</span>
                    <input value={modelerDomainModesInput} onChange={(e) => setModelerDomainModesInput(e.target.value)} spellCheck={false} style={{ border: '1px solid var(--border-subtle)', background: 'var(--bg-base)', color: 'var(--text-heading)', borderRadius: 9, padding: '7px 9px', fontSize: 12 }} />
                  </label>
                  <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <span style={{ fontSize: 11, color: 'var(--text-secondary-strong)', fontWeight: 700 }}>Engine</span>
                    <select value={modelerEngineId} onChange={(e) => setModelerEngineId(String(e.target.value || '').trim())} style={{ border: '1px solid var(--border-subtle)', background: 'var(--bg-base)', color: 'var(--text-heading)', borderRadius: 9, padding: '7px 9px', fontSize: 12 }}>
                      <option value="macro_economical_v1">macro_economical_v1</option>
                    </select>
                  </label>
                </div>
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>Raw target paths</div>
                    <textarea
                      value={modelerTargetPathsInput}
                      onChange={(e) => setModelerTargetPathsInput(e.target.value)}
                      spellCheck={false}
                      style={{
                        width: '100%',
                        minHeight: 120,
                        resize: 'vertical',
                        border: '1px solid var(--border-subtle)',
                        background: 'var(--bg-base)',
                        color: 'var(--text-heading)',
                        borderRadius: 10,
                        padding: '8px 10px',
                        fontSize: 12,
                        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
                      }}
                    />
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)' }}>Raw parameters JSON</div>
                    <textarea
                      value={modelerParametersInput}
                      onChange={(e) => setModelerParametersInput(e.target.value)}
                      spellCheck={false}
                      style={{
                        width: '100%',
                        minHeight: 120,
                        resize: 'vertical',
                        border: '1px solid var(--border-subtle)',
                        background: 'var(--bg-base)',
                        color: 'var(--text-heading)',
                        borderRadius: 10,
                        padding: '8px 10px',
                        fontSize: 12,
                        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
                      }}
                    />
                  </div>
                </div>
              </div>
            )}

            {modelerLoading && (
              <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>Running scenario...</div>
            )}

            {modelerParamError && !modelerError && (
              <div
                style={{
                  padding: '8px 10px',
                  borderRadius: 10,
                  border: '1px solid rgba(245,158,11,0.35)',
                  background: theme === 'dark' ? 'rgba(245,158,11,0.12)' : 'rgba(245,158,11,0.10)',
                  color: '#B45309',
                  fontSize: 12,
                }}
              >
                Parameters JSON has a parse error. Quick controls still work, but run is blocked until JSON is valid.
              </div>
            )}

            {modelerError && (
              <div
                style={{
                  padding: '10px 12px',
                  borderRadius: 12,
                  border: '1px solid rgba(239,68,68,0.35)',
                  background: theme === 'dark' ? 'rgba(239,68,68,0.12)' : 'rgba(239,68,68,0.08)',
                  color: '#EF4444',
                  fontSize: 12,
                }}
              >
                {modelerError}
              </div>
            )}

            {modelerResult && (
              <div
                style={{
                  display: 'grid',
                  gap: 10,
                  borderTop: '1px solid var(--border-subtle)',
                  paddingTop: 10,
                }}
              >
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                  <span style={{ padding: '4px 8px', borderRadius: 999, border: '1px solid var(--border-subtle)', fontSize: 11 }}>
                    Signals: <b>{Array.isArray(modelerResult.encodedSignals) ? modelerResult.encodedSignals.length : 0}</b>
                  </span>
                  <span style={{ padding: '4px 8px', borderRadius: 999, border: '1px solid var(--border-subtle)', fontSize: 11 }}>
                    Seeds: <b>{Array.isArray(modelerResult.seedFragmentIds) ? modelerResult.seedFragmentIds.length : 0}</b>
                  </span>
                  <span style={{ padding: '4px 8px', borderRadius: 999, border: '1px solid var(--border-subtle)', fontSize: 11 }}>
                    Affected nodes: <b>{Array.isArray(modelerResult.affectedFragments) ? modelerResult.affectedFragments.length : 0}</b>
                  </span>
                  <span style={{ padding: '4px 8px', borderRadius: 999, border: '1px solid var(--border-subtle)', fontSize: 11 }}>
                    Decoded nodes: <b>{Array.isArray(modelerResult.domainMetrics) ? modelerResult.domainMetrics.length : 0}</b>
                  </span>
                  <span style={{ padding: '4px 8px', borderRadius: 999, border: '1px solid var(--border-subtle)', fontSize: 11 }}>
                    Trace steps: <b>{Number((modelerResult.propagationStats || {})['trace_steps'] || 0).toFixed(0)}</b>
                  </span>
                </div>

                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
                  <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: 10 }}>
                    <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', marginBottom: 6 }}>Input resolution</div>
                    {Array.isArray(modelerResult?.inputResolution?.missing) && modelerResult.inputResolution!.missing!.length > 0 && (
                      <div style={{ fontSize: 11, color: '#EF4444', marginBottom: 6 }}>
                        Missing: {modelerResult.inputResolution!.missing!.join(', ')}
                      </div>
                    )}
                    <div style={{ display: 'grid', gap: 4 }}>
                      {Object.entries(modelerResult?.inputResolution?.resolved || {}).slice(0, 12).map(([k, v]) => {
                        const from = (modelerResult?.inputResolution?.resolvedFrom || ({} as Record<string, string>))[k] || '';
                        return (
                          <div key={k} style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 8, fontSize: 11 }}>
                            <span style={{ color: 'var(--text-secondary-strong)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{k}</span>
                            <span style={{ color: 'var(--text-heading)', fontWeight: 700 }}>{Number(v).toFixed(4)}</span>
                            <span style={{ color: 'var(--text-secondary-strong)', maxWidth: '45%', overflow: 'hidden', textOverflow: 'ellipsis' }}>{from}</span>
                          </div>
                        );
                      })}
                    </div>
                  </div>

                  <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: 10 }}>
                    <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', marginBottom: 6 }}>Encoded + 12-axis basis</div>
                    <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginBottom: 8 }}>
                      {PHYSICAL_AXES_12.map((axis) => {
                        const hit = Array.isArray(modelerResult.encodedSignals) && modelerResult.encodedSignals.some((s) => String(s?.axis || '').trim() === axis);
                        return (
                          <span
                            key={axis}
                            style={{
                              padding: '3px 7px',
                              borderRadius: 999,
                              border: '1px solid var(--border-subtle)',
                              background: hit ? (theme === 'dark' ? 'rgba(34,197,94,0.20)' : 'rgba(16,185,129,0.14)') : 'transparent',
                              color: hit ? 'var(--text-heading)' : 'var(--text-secondary-strong)',
                              fontSize: 10,
                              fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, \"Liberation Mono\", monospace',
                            }}
                          >
                            {axis}
                          </span>
                        );
                      })}
                    </div>
                    <div style={{ display: 'grid', gap: 4 }}>
                      {(modelerResult.encodedSignals || []).slice(0, 8).map((s, idx) => (
                        <div key={`${s.id || 'sig'}-${idx}`} style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                          <b style={{ color: 'var(--text-heading)' }}>{s.axis || 'axis'}</b> / amp {(Number(s.amplitude || 0)).toFixed(3)} / {s.polarity || 'n/a'}
                        </div>
                      ))}
                    </div>
                  </div>
                </div>

                <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: 10 }}>
                  <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', marginBottom: 6 }}>
                    Signal transfer trace
                  </div>
                  <div style={{ display: 'grid', gap: 8, maxHeight: 220, overflowY: 'auto' }}>
                    {(Array.isArray(modelerResult.signalRuns) ? modelerResult.signalRuns : []).slice(0, 8).map((run, idx) => (
                      <div
                        key={`${String(run.signalId || run.axis || 'run')}-${idx}`}
                        style={{
                          border: '1px solid var(--border-subtle)',
                          borderRadius: 10,
                          padding: 8,
                          background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
                        }}
                      >
                        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, marginBottom: 5 }}>
                          <div style={{ fontSize: 11, fontWeight: 800, color: 'var(--text-heading)' }}>
                            {run.signalId || run.axis || 'signal'}
                          </div>
                          <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>
                            seeds {Array.isArray(run.seedFragmentIds) ? run.seedFragmentIds.length : 0} • affected {Array.isArray(run.affectedFragments) ? run.affectedFragments.length : 0} • trace {Array.isArray(run.trace) ? run.trace.length : 0}
                          </div>
                        </div>
                        <div style={{ display: 'grid', gap: 3 }}>
                          {(Array.isArray(run.trace) ? run.trace : []).slice(0, 6).map((ev, evIdx) => (
                            <div key={`${String(run.signalId || 'signal')}-${idx}-${evIdx}`} style={{ display: 'grid', gap: 2, fontSize: 10 }}>
                              <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 8 }}>
                                <span style={{ color: 'var(--text-secondary-strong)', fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, \"Liberation Mono\", monospace', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                  {ev.outcome || 'event'} {ev.fromFragmentId ? `${ev.fromFragmentId} -> ` : ''}{ev.toFragmentId || ''}
                                </span>
                                <span style={{ color: 'var(--text-heading)', whiteSpace: 'nowrap' }}>
                                  {Number(ev.amplitudeOut || 0).toFixed(3)} {ev.axisOut || ''}
                                </span>
                              </div>
                              <div style={{ color: 'var(--text-muted)', fontSize: 9, display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                                {ev.channel ? <span>channel: {ev.channel}</span> : null}
                                {ev.scope ? <span>scope: {ev.scope}</span> : null}
                                {ev.spillover ? <span>spillover: {ev.spillover}</span> : null}
                                {ev.factors && typeof ev.factors === 'object' && typeof (ev.factors as any)['after_adapter'] === 'number'
                                  ? <span>f(after): {Number((ev.factors as any)['after_adapter']).toFixed(3)}</span>
                                  : null}
                              </div>
                            </div>
                          ))}
                        </div>
                      </div>
                    ))}
                    {(!Array.isArray(modelerResult.signalRuns) || modelerResult.signalRuns.length === 0) && (
                      <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>No per-signal trace available for this run.</div>
                    )}
                  </div>
                </div>

                <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 12, padding: 10 }}>
                  <div style={{ fontSize: 12, fontWeight: 800, color: 'var(--text-heading)', marginBottom: 6 }}>
                    Decode output by node
                  </div>
                  <div style={{ display: 'grid', gap: 8, maxHeight: 260, overflowY: 'auto' }}>
                    {(Array.isArray(modelerResult.domainMetrics) ? [...modelerResult.domainMetrics] : [])
                      .sort((a, b) => {
                        const ax = Array.isArray(a.metrics) ? Math.max(0, ...a.metrics.map((m) => Math.abs(Number(m?.value || 0)))) : 0;
                        const bx = Array.isArray(b.metrics) ? Math.max(0, ...b.metrics.map((m) => Math.abs(Number(m?.value || 0)))) : 0;
                        return bx - ax;
                      })
                      .slice(0, 20)
                      .map((node, idx) => (
                        <div
                          key={`${String(node.fragmentId || node.canonicalPath || 'node')}-${idx}`}
                          style={{
                            border: '1px solid var(--border-subtle)',
                            borderRadius: 10,
                            padding: 8,
                            background: theme === 'dark' ? 'rgba(255,255,255,0.03)' : 'rgba(0,0,0,0.02)',
                          }}
                        >
                          <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 8, marginBottom: 6 }}>
                            <div style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: 11, color: 'var(--text-heading)', fontWeight: 800 }}>
                              {String(node.canonicalPath || node.fragmentId || 'node')}
                            </div>
                            <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>
                              {node.persisted ? 'persisted' : 'preview'}
                            </span>
                          </div>
                          <div style={{ display: 'grid', gap: 3 }}>
                            {(node.metrics || []).slice(0, 10).map((m) => (
                              <div
                                key={`${String(node.fragmentId || '')}-${String(m.metricPath || '')}`}
                                style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 8, fontSize: 11 }}
                              >
                                <span style={{ color: 'var(--text-secondary-strong)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                  {m.metricPath}
                                </span>
                                <span style={{ color: 'var(--text-heading)', fontWeight: 700, whiteSpace: 'nowrap' }}>
                                  {Number(m.value || 0).toFixed(4)} {m.unit || ''}
                                </span>
                              </div>
                            ))}
                          </div>
                        </div>
                      ))}
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* Ask Graph Command Palette Overlay */}
      {askGraphOpen && (
        <div
          style={{
            position: 'fixed',
            inset: 0,
            zIndex: 9998,
            display: 'flex',
            alignItems: 'flex-start',
            justifyContent: 'center',
            paddingTop: '15vh',
            background: theme === 'dark' ? 'rgba(0,0,0,0.5)' : 'rgba(255,255,255,0.4)',
            backdropFilter: 'blur(4px)',
          }}
          onClick={() => closeAskGraph()}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              width: 560,
              maxWidth: '92vw',
              display: 'flex',
              flexDirection: 'column',
              background: theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
              border: '1px solid var(--border-subtle)',
              borderRadius: 16,
              boxShadow: theme === 'dark' ? '0 20px 60px rgba(0,0,0,0.6)' : '0 20px 60px rgba(0,0,0,0.2)',
              overflow: 'hidden',
              animation: 'horizon-command-pop 0.15s ease-out',
            }}
          >
            <style>{`
              @keyframes horizon-command-pop {
                from { opacity: 0; transform: scale(0.96) translateY(10px); }
                to { opacity: 1; transform: scale(1) translateY(0); }
              }
            `}</style>
            <div style={{ display: 'flex', alignItems: 'center', padding: '14px 16px', borderBottom: '1px solid var(--border-subtle)' }}>
              <div style={{ marginRight: 12, color: 'var(--text-secondary-strong)', display: 'flex', fontWeight: 900 }}>
                ?
              </div>
              <input
                ref={askGraphInputRef}
                value={askGraphQuery}
                onChange={(e) => {
                  setAskGraphQuery(e.target.value);
                  setAskGraphError(null);
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') {
                    e.preventDefault();
                    closeAskGraph();
                    return;
                  }
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    void submitAskGraph();
                    return;
                  }
                }}
                placeholder="Ask this graph… (press Enter)"
                autoComplete="off"
                spellCheck={false}
                disabled={askGraphLoading}
                style={{
                  flex: 1,
                  border: 'none',
                  outline: 'none',
                  background: 'transparent',
                  color: 'var(--text-heading)',
                  fontSize: 16,
                  fontWeight: 550,
                }}
              />
              {askGraphLoading && (
                <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>Asking…</div>
              )}
            </div>

            <div style={{ padding: 14 }}>
              {askGraphError && (
                <div style={{ padding: '10px 12px', borderRadius: 12, background: theme === 'dark' ? 'rgba(239,68,68,0.10)' : 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.22)', color: '#EF4444', fontSize: 13 }}>
                  {askGraphError}
                </div>
              )}

              {!askGraphError && (
                <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.5 }}>
                  Ask a question about the <b>currently visible</b> graph. We’ll highlight the affected nodes immediately.
                  <div style={{ marginTop: 6, display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                    <span style={{ padding: '4px 8px', borderRadius: 999, border: '1px solid var(--border-subtle)', background: theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'rgba(0,0,0,0.03)' }}>
                      Nodes: {nodes.filter((n: any) => !((n.data as any)?.isGroup)).length}
                    </span>
                    <span style={{ padding: '4px 8px', borderRadius: 999, border: '1px solid var(--border-subtle)', background: theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'rgba(0,0,0,0.03)' }}>
                      Edges: {edges.length}
                    </span>
                    <span style={{ padding: '4px 8px', borderRadius: 999, border: '1px solid var(--border-subtle)', background: theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'rgba(0,0,0,0.03)' }}>
                      Esc clears highlight
                    </span>
                  </div>
                </div>
              )}

              <div style={{ marginTop: 12, display: 'flex', justifyContent: 'flex-end', gap: 10 }}>
                <button
                  type="button"
                  onClick={() => closeAskGraph()}
                  disabled={askGraphLoading}
                  style={{
                    height: 36,
                    padding: '0 14px',
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: 'transparent',
                    color: 'var(--text-secondary-strong)',
                    cursor: askGraphLoading ? 'not-allowed' : 'pointer',
                  }}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  onClick={() => void submitAskGraph()}
                  disabled={askGraphLoading || !askGraphQuery.trim() || !lensId || nodes.length === 0}
                  style={{
                    height: 36,
                    padding: '0 16px',
                    borderRadius: 12,
                    border: 'none',
                    background: 'var(--accent-primary, #3B82F6)',
                    color: '#fff',
                    fontWeight: 800,
                    cursor: (askGraphLoading || !askGraphQuery.trim() || !lensId || nodes.length === 0) ? 'not-allowed' : 'pointer',
                    opacity: (askGraphLoading || !askGraphQuery.trim() || !lensId || nodes.length === 0) ? 0.6 : 1,
                    boxShadow: '0 8px 20px rgba(59,130,246,0.25)',
                  }}
                >
                  Ask
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Scope-scout confirm modal (portal) */}
      {scopeScoutModal && ReactDOM.createPortal(
        <div
          onClick={() => {
            if (scopeScoutInFlightId) return;
            setScopeScoutModal(null);
          }}
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(0,0,0,0.45)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            padding: 16,
            zIndex: 1000,
          }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            role="dialog"
            aria-modal="true"
            aria-labelledby="scope-scout-title"
            aria-describedby="scope-scout-desc"
            tabIndex={-1}
            style={{
              width: 520,
              maxWidth: 'min(520px, calc(100vw - 32px))',
              borderRadius: 14,
              border: '1px solid var(--border-subtle)',
              background: 'var(--bg-elevated)',
              boxShadow: '0 18px 60px rgba(0,0,0,0.35)',
              padding: 16,
              color: 'var(--text-heading)',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12, marginBottom: 8 }}>
              <div id="scope-scout-title" style={{ fontSize: 14, fontWeight: 800, lineHeight: 1.2 }}>
                Run supplemental research?
              </div>
              <button
                type="button"
                onClick={() => {
                  if (scopeScoutInFlightId) return;
                  setScopeScoutModal(null);
                }}
                aria-label="Close"
                disabled={Boolean(scopeScoutInFlightId)}
                style={{
                  width: 30,
                  height: 30,
                  borderRadius: 10,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-secondary-strong)',
                  cursor: scopeScoutInFlightId ? 'not-allowed' : 'pointer',
                  opacity: scopeScoutInFlightId ? 0.5 : 0.85,
                  fontSize: 18,
                  fontWeight: 900,
                  lineHeight: 1,
                }}
                title="Close"
              >
                ×
              </button>
            </div>
            <div id="scope-scout-desc" style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.4 }}>
              This action adds the selected node to the lens <strong>Scope</strong> and initiates an incremental discovery
              pass to expand the relationship map around it.
            </div>

            <div style={{ marginTop: 10, fontSize: 12 }}>
              <div style={{ color: 'var(--text-secondary-strong)', marginBottom: 4 }}>Target</div>
              <div style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--text-heading)' }}>
                {scopeScoutModal.label || scopeScoutModal.canonicalPath || scopeScoutModal.fragmentId}
              </div>
            </div>

            <div style={{ marginTop: 12 }}>
              <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', marginBottom: 6 }}>
                Scouting mode
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: 8 }}>
                {([
                  { value: 'general' as const, label: 'General', desc: 'Broad relations and context' },
                  { value: 'supply_chain' as const, label: 'Supply chain', desc: 'Operational and product flows' },
                  { value: 'governance' as const, label: 'Governance', desc: 'Ownership, control, leadership' },
                ]).map((opt) => {
                  const active = scopeScoutMode === opt.value;
                  return (
                    <button
                      key={opt.value}
                      type="button"
                      onClick={() => {
                        setScopeScoutMode(opt.value);
                        setScopeScoutDepth(opt.value === 'supply_chain' ? 2 : 1);
                      }}
                      disabled={Boolean(scopeScoutInFlightId)}
                      aria-pressed={active}
                      style={{
                        textAlign: 'left',
                        padding: '10px 10px 9px',
                        borderRadius: 10,
                        border: '1px solid var(--border-subtle)',
                        background: active ? 'var(--white-10)' : 'transparent',
                        color: 'var(--text-heading)',
                        cursor: scopeScoutInFlightId ? 'not-allowed' : 'pointer',
                        opacity: scopeScoutInFlightId ? 0.5 : 1,
                      }}
                    >
                      <div style={{ fontSize: 12, fontWeight: 700, lineHeight: 1.2 }}>{opt.label}</div>
                      <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-secondary-strong)', lineHeight: 1.35 }}>
                        {opt.desc}
                      </div>
                    </button>
                  );
                })}
              </div>
            </div>

            <div style={{ marginTop: 12, display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 10 }}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 0 }}>
                <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                  Depth
                </div>
                <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.35 }}>
                  {scopeScoutDepth === 1
                    ? 'Direct connections only — immediate relationships to the selected node.'
                    : 'Direct + indirect connections — relationships of the newly discovered connections.'}
                </div>
              </div>
              <div style={{ display: 'flex', gap: 8 }}>
                <button
                  type="button"
                  onClick={() => setScopeScoutDepth(1)}
                  disabled={Boolean(scopeScoutInFlightId)}
                  aria-pressed={scopeScoutDepth === 1}
                  aria-label="Depth 1"
                  style={{
                    padding: '6px 10px',
                    borderRadius: 10,
                    border: '1px solid var(--border-subtle)',
                    background: scopeScoutDepth === 1 ? 'var(--white-10)' : 'transparent',
                    color: 'var(--text-heading)',
                    cursor: scopeScoutInFlightId ? 'not-allowed' : 'pointer',
                    opacity: scopeScoutInFlightId ? 0.5 : 1,
                  }}
                  title="Direct relationships (one hop)"
                >
                  1
                </button>
                <button
                  type="button"
                  onClick={() => setScopeScoutDepth(2)}
                  disabled={Boolean(scopeScoutInFlightId)}
                  aria-pressed={scopeScoutDepth === 2}
                  aria-label="Depth 2"
                  style={{
                    padding: '6px 10px',
                    borderRadius: 10,
                    border: '1px solid var(--border-subtle)',
                    background: scopeScoutDepth === 2 ? 'var(--white-10)' : 'transparent',
                    color: 'var(--text-heading)',
                    cursor: scopeScoutInFlightId ? 'not-allowed' : 'pointer',
                    opacity: scopeScoutInFlightId ? 0.5 : 1,
                  }}
                  title="Direct + indirect relationships"
                >
                  2
                </button>
              </div>
            </div>

            {scopeScoutMode === 'supply_chain' && (
              <div
                style={{
                  marginTop: 12,
                  padding: 12,
                  borderRadius: 12,
                  border: '1px solid var(--border-subtle)',
                  background: 'var(--white-04)',
                  display: 'grid',
                  gap: 12,
                }}
              >
                <div>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', marginBottom: 6 }}>
                    Direction
                  </div>
                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 8 }}>
                    {([
                      { value: 'downstream' as const, label: 'Downstream', desc: 'Follow dependencies, customers, and exposed flows.' },
                      { value: 'upstream' as const, label: 'Upstream', desc: 'Trace suppliers, source dependencies, and origin points.' },
                    ]).map((opt) => {
                      const active = scopeScoutDirection === opt.value;
                      return (
                        <button
                          key={opt.value}
                          type="button"
                          onClick={() => setScopeScoutDirection(opt.value)}
                          disabled={Boolean(scopeScoutInFlightId)}
                          aria-pressed={active}
                          style={{
                            textAlign: 'left',
                            padding: '10px 10px 9px',
                            borderRadius: 10,
                            border: '1px solid var(--border-subtle)',
                            background: active ? 'var(--white-10)' : 'transparent',
                            color: 'var(--text-heading)',
                            cursor: scopeScoutInFlightId ? 'not-allowed' : 'pointer',
                            opacity: scopeScoutInFlightId ? 0.5 : 1,
                          }}
                        >
                          <div style={{ fontSize: 12, fontWeight: 700, lineHeight: 1.2 }}>{opt.label}</div>
                          <div style={{ marginTop: 4, fontSize: 11, color: 'var(--text-secondary-strong)', lineHeight: 1.35 }}>
                            {opt.desc}
                          </div>
                        </button>
                      );
                    })}
                  </div>
                </div>

                <div style={{ display: 'grid', gap: 8 }}>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                    Directed enrichments
                  </div>
                  {([
                    {
                      key: 'hot-path',
                      checked: scopeScoutHotPathOnly,
                      onChange: (next: boolean) => setScopeScoutHotPathOnly(next),
                      label: 'Only hot path',
                      desc: 'Return the first operational chain quickly and stop after the fast path.',
                    },
                    {
                      key: 'capabilities',
                      checked: scopeScoutIncludeCapabilities,
                      onChange: (next: boolean) => setScopeScoutIncludeCapabilities(next),
                      label: 'Capabilities',
                      desc: 'Request capability-level enrichment for newly materialized entities.',
                    },
                    {
                      key: 'edge-metrics',
                      checked: scopeScoutIncludeEdgeMetrics,
                      onChange: (next: boolean) => setScopeScoutIncludeEdgeMetrics(next),
                      label: 'Edge metrics',
                      desc: 'Add transport and dependency metrics when the supply-chain pass can resolve them.',
                    },
                  ]).map((opt) => (
                    <label
                      key={opt.key}
                      style={{
                        display: 'grid',
                        gridTemplateColumns: '16px minmax(0, 1fr)',
                        gap: 10,
                        alignItems: 'flex-start',
                        cursor: scopeScoutInFlightId ? 'not-allowed' : 'pointer',
                        opacity: scopeScoutInFlightId ? 0.5 : 1,
                      }}
                    >
                      <input
                        type="checkbox"
                        checked={opt.checked}
                        onChange={(e) => opt.onChange(e.target.checked)}
                        disabled={Boolean(scopeScoutInFlightId)}
                        style={{ marginTop: 2 }}
                      />
                      <span style={{ display: 'grid', gap: 2 }}>
                        <span style={{ fontSize: 12, fontWeight: 700, color: 'var(--text-heading)' }}>{opt.label}</span>
                        <span style={{ fontSize: 11, lineHeight: 1.35, color: 'var(--text-secondary-strong)' }}>{opt.desc}</span>
                      </span>
                    </label>
                  ))}
                </div>

                <div style={{ display: 'grid', gap: 6 }}>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                    Supplemental request
                  </div>
                  <textarea
                    value={scopeScoutCustomQuery}
                    onChange={(e) => setScopeScoutCustomQuery(e.target.value)}
                    disabled={Boolean(scopeScoutInFlightId)}
                    placeholder="Optional: focus on export controls, choke points, sanctions exposure, logistics paths…"
                    rows={3}
                    style={{
                      width: '100%',
                      resize: 'vertical',
                      minHeight: 74,
                      borderRadius: 10,
                      border: '1px solid var(--border-subtle)',
                      background: 'rgba(255,255,255,0.02)',
                      color: 'var(--text-heading)',
                      padding: '10px 12px',
                      fontSize: 12,
                      lineHeight: 1.45,
                    }}
                  />
                  <div style={{ fontSize: 11, lineHeight: 1.35, color: 'var(--text-secondary-strong)' }}>
                    This supplements the supply-chain pass. It does not replace the selected direction or hot-path logic.
                  </div>
                </div>
              </div>
            )}

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 14 }}>
              <button
                type="button"
                onClick={() => setScopeScoutModal(null)}
                disabled={Boolean(scopeScoutInFlightId)}
                ref={scopeScoutCancelBtnRef}
                style={{
                  padding: '8px 10px',
                  borderRadius: 10,
                  border: '1px solid var(--border-subtle)',
                  background: 'transparent',
                  color: 'var(--text-secondary-strong)',
                  cursor: scopeScoutInFlightId ? 'not-allowed' : 'pointer',
                  opacity: scopeScoutInFlightId ? 0.5 : 1,
                }}
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={confirmScopeScout}
                disabled={!onScopeScout || Boolean(scopeScoutInFlightId)}
                style={{
                  padding: '8px 12px',
                  borderRadius: 10,
                  border: 'none',
                  background: 'var(--accent-primary, #3B82F6)',
                  color: '#fff',
                  fontWeight: 850,
                  cursor: (!onScopeScout || scopeScoutInFlightId) ? 'not-allowed' : 'pointer',
                  opacity: (!onScopeScout || scopeScoutInFlightId) ? 0.5 : 1,
                  boxShadow: '0 10px 22px rgba(59,130,246,0.24)',
                }}
              >
                {scopeScoutInFlightId ? 'Starting…' : 'Run research'}
              </button>
            </div>
          </div>
        </div>,
        document.body
      )}

      {/* Toast notification for empty expand results */}
      <ExpandEmptyToast
        visible={expandEmptyToast}
        onHide={() => {
          setExpandEmptyToast(false);
          setExpandEmptyContext(null);
        }}
        duration={expandEmptyContext?.isFullNetwork ? 5000 : 2000}
        message={expandEmptyContext?.isFullNetwork ? 'All available on-the-fly data is loaded.' : 'No related objects found'}
        subMessage={
          expandEmptyContext?.isFullNetwork
            ? 'For additional details, the system can run Scout.'
            : 'Try a different expansion mode'
        }
        actionLabel={expandEmptyContext?.isFullNetwork ? 'Scout' : undefined}
        onAction={
          expandEmptyContext?.isFullNetwork && expandEmptyContext.fragmentId
            ? () => {
                openScopeScoutModal(expandEmptyContext.fragmentId, expandEmptyContext.canonicalPath);
                setExpandEmptyToast(false);
                setExpandEmptyContext(null);
              }
            : undefined
        }
        actionDisabled={!onScopeScout || Boolean(scopeScoutInFlightId)}
      />

      {/* Market Profile Modal */}
      {marketProfileModal && (
        <MarketProfileModal
          open={marketProfileModal.open}
          onClose={() => setMarketProfileModal(null)}
          fragmentId={marketProfileModal.fragmentId}
          canonicalPath={marketProfileModal.canonicalPath}
          label={marketProfileModal.label}
        />
      )}

      {orgProfileModal && (
        <OrgProfileModal
          open={orgProfileModal.open}
          onClose={() => setOrgProfileModal(null)}
          fragmentId={orgProfileModal.fragmentId}
          canonicalPath={orgProfileModal.canonicalPath}
          label={orgProfileModal.label}
        />
      )}

      {govProfileModal && (
        <GovProfileModal
          open={govProfileModal.open}
          onClose={() => setGovProfileModal(null)}
          fragmentId={govProfileModal.fragmentId}
          canonicalPath={govProfileModal.canonicalPath}
          label={govProfileModal.label}
        />
      )}

      {dynastyProfileModal && (
        <DynastyProfileModal
          open={dynastyProfileModal.open}
          onClose={() => setDynastyProfileModal(null)}
          fragmentId={dynastyProfileModal.fragmentId}
          canonicalPath={dynastyProfileModal.canonicalPath}
          label={dynastyProfileModal.label}
        />
      )}

      {tribeProfileModal && (
        <TribeProfileModal
          open={tribeProfileModal.open}
          onClose={() => setTribeProfileModal(null)}
          fragmentId={tribeProfileModal.fragmentId}
          canonicalPath={tribeProfileModal.canonicalPath}
          label={tribeProfileModal.label}
        />
      )}

      {programProfileModal && (
        <ProgramProfileModal
          open={programProfileModal.open}
          onClose={() => setProgramProfileModal(null)}
          fragmentId={programProfileModal.fragmentId}
          canonicalPath={programProfileModal.canonicalPath}
          label={programProfileModal.label}
        />
      )}

      <ScoutIntentModal
        visible={scoutIntentModalOpen}
        query={scoutIntentQuery}
        intent={scoutIntentIntent}
        intentLoading={scoutIntentLoading}
        intentError={scoutIntentError}
        runLoading={scoutRunLoading}
        progressLog={scoutProgressLog}
        mode={scoutIntentMode}
        showHotPathOption={scoutIntentHasSupplyChainContract}
        hotPathOnly={scoutIntentHotPathOnly}
        onHotPathOnlyChange={setScoutIntentHotPathOnly}
        supplyChainDirection={scoutIntentDirection}
        onSupplyChainDirectionChange={setScoutIntentDirection}
        onModeChange={(mode) => {
          setScoutIntentMode(mode);
          setScoutIntentModeTouched(true);
        }}
        onClose={closeScoutIntentModal}
        onRunScout={() => void runScoutFromModal()}
        theme={theme as 'light' | 'dark'}
      />
    </div>
  );
};
