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
import { api } from '../lib/api';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useHorizonRealtimeStore } from '../stores/horizonRealtimeStore';
import { humanizeCanonicalPath } from '../lib/uiUtils';
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
  createEmptyGrid,
  setGridCell,
  getGridCell,
  findCellForExpandedNode,
  gridToCanvasPosition,
  type GridState,
  type GridCell,
} from '../lib/elkLayout';
import type { LensItem } from '../types/lensItem';
import type { HorizonNodeData } from './HorizonGraphNodeTypes';
import { ExpandEmptyToast } from './ExpandEmptyToast';
import { MarketProfileModal } from './MarketProfileModal';
import { OrgProfileModal } from './OrgProfileModal';
import { GovProfileModal } from './GovProfileModal';
import { DynastyProfileModal } from './DynastyProfileModal';
import { TribeProfileModal } from './TribeProfileModal';
import { ProgramProfileModal } from './ProgramProfileModal';

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
  const labelX = midX;
  const labelY = midY - 8; // Slightly above the arc apex

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
  const [edgePath, labelX, labelY] = getBezierPath({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition });
  const d: any = data || {};
  const showLabel = !!d.showLabel;
  const caption = String(d.edgeCaption || d.edgeLabel || '').trim();
  const onExplain = d.onExplain as undefined | ((args: EdgeExplainOpenArgs) => void);
  const onLabelHoverEnter = d.onLabelHoverEnter as undefined | ((edgeId: string) => void);
  const onLabelHoverLeave = d.onLabelHoverLeave as undefined | ((edgeId: string) => void);
  const raiseToFront = !!d.raiseToFront;

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
  const labelX = midX;
  const labelY = midY - 8;
  const d: any = data || {};
  const showLabel = !!d.showLabel;
  const caption = String(d.edgeCaption || d.edgeLabel || '').trim();
  const onExplain = d.onExplain as undefined | ((args: EdgeExplainOpenArgs) => void);
  const onLabelHoverEnter = d.onLabelHoverEnter as undefined | ((edgeId: string) => void);
  const onLabelHoverLeave = d.onLabelHoverLeave as undefined | ((edgeId: string) => void);
  const raiseToFront = !!d.raiseToFront;

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
  onScopeScout?: (input: { fragmentId: string; canonicalPath?: string; maxRecursionDepth?: number }) => Promise<void>;
  inspectorActive?: boolean;
  onToggleInspector?: () => void;
}

export const LensCanvasCockpit: React.FC<LensCanvasCockpitProps> = ({
  lensId,
  projectId,
  onNodeSelect,
  onEdgeSelect,
  onScopeScout,
  inspectorActive,
  onToggleInspector,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const DEV = import.meta.env.DEV;
  const dbg = React.useCallback((...args: any[]) => {
    if (DEV) console.log(...args);
  }, [DEV]);

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
      DOMINATES: 'Dominates',
      HAS_SUBMARKET: 'Submarket',
      HAS_SUBGROUP: 'Subgroup',
      ADMINISTERS: 'Administers',
      // Community relationships
      OPERATES_VIA: 'Via',
      MEMBER_OF: 'Member',
      AFFILIATED_WITH: 'Affiliated',
      // Person relationships
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
  const [scopeScoutInFlightId, setScopeScoutInFlightId] = React.useState<string | null>(null);
  const scopeScoutCancelBtnRef = React.useRef<HTMLButtonElement | null>(null);
  const scopeScoutPrevActiveElRef = React.useRef<HTMLElement | null>(null);
  const [hoveredGroupId, setHoveredGroupId] = React.useState<string | null>(null);
  const [hoveredNodeId, setHoveredNodeId] = React.useState<string | null>(null);
  const [hoveredEdgeId, setHoveredEdgeId] = React.useState<string | null>(null);
  // Risk Shield (8-segment overlay around a node) — single active node at a time.
  const [riskShieldActiveNodeId, setRiskShieldActiveNodeId] = React.useState<string | null>(null);
  const toggleRiskShield = React.useCallback((fragmentId: string) => {
    const id = String(fragmentId || '').trim();
    if (!id) return;
    setRiskShieldActiveNodeId((prev) => (String(prev || '').trim() === id ? null : id));
  }, []);
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

  // Avoid unbounded growth across long sessions / lens switches.
  // Also reset collapsed subcategories to defaults when lens changes.
  React.useEffect(() => {
    edgeExplainSessionCacheRef.current.clear();
    setEdgeExplainPopover(null);
    nodeExplainSessionCacheRef.current.clear();
    setNodeExplainPopover(null);
    setCollapsedSubcategories(new Set());
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

  type HoverHighlightMode = 'off' | 'control' | 'expansion' | 'production' | 'supplies' | 'competition';
  const [hoverHighlightMode, setHoverHighlightMode] = React.useState<HoverHighlightMode>('control');

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
  const [layoutMode, setLayoutMode] = React.useState<'freeform' | 'linear' | 'levels'>('levels');
  const [showToolbarMenu, setShowToolbarMenu] = React.useState(false);
  const toolbarMenuRef = React.useRef<HTMLDivElement | null>(null);
  const [showLayoutMenu, setShowLayoutMenu] = React.useState(false);
  const layoutMenuRef = React.useRef<HTMLDivElement | null>(null);
  const [showHoverModeMenu, setShowHoverModeMenu] = React.useState(false);
  const hoverModeMenuRef = React.useRef<HTMLDivElement | null>(null);

  // Stack preview overlay (Levels): show up to 5 hidden stack members as chips on controller click.
  const stackPreviewControllerIdRef = React.useRef<string | null>(null);
  const pendingFocusNodeIdRef = React.useRef<string | null>(null);

  // Levels (Hierarchical) expansion filter (controls which edge types are fetched on expand).
  type LevelsExpandMode = 'all' | 'ownership' | 'expansion' | 'dependencies' | 'context' | 'market' | 'influence' | 'balanced';
  const [levelsExpandMode, setLevelsExpandMode] = React.useState<LevelsExpandMode>('all');
  const [showLevelsExpandMenu, setShowLevelsExpandMenu] = React.useState(false);
  const levelsExpandMenuRef = React.useRef<HTMLDivElement | null>(null);

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
    const CONTEXT = ['LOCATED_IN', 'OPERATES_IN', 'BASED_IN', 'HEADQUARTERED_IN', 'REGULATES', 'AUDITED_BY', 'EXTRACTS_FROM', 'ADMINISTERS'];
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
      // Market skeleton
      'HAS_SUBMARKET',
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
      case 'all':
      default:
        return [];
    }
  }, [levelsExpandMode]);

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
  const addNodeInputRef = React.useRef<HTMLInputElement | null>(null);
  const addNodeCacheRef = React.useRef<Map<string, HorizonSearchEntity[]>>(new Map());
  const addNodeAbortRef = React.useRef<AbortController | null>(null);

  const graphSearchResults = React.useMemo(() => {
    if (!addNodeOpen) return [] as Array<{ node: Node<HorizonNodeData>; label: string; type?: string; canonicalPath?: string; score: number }>;
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
  }, [addNodeOpen, addNodeQuery, nodes]);

  const filteredAddNodeResults = React.useMemo(() => {
    if (!addNodeOpen) return [] as HorizonSearchEntity[];
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
  }, [addNodeOpen, addNodeResults, nodes]);

  const paletteSelectableItems = React.useMemo(() => {
    const q = String(addNodeQuery || '').trim();
    const scoutItem = q.length >= 2 ? [{ kind: 'scout' as const, query: q }] : [];
    return [
      ...scoutItem,
      ...graphSearchResults.map((m) => ({ kind: 'graph' as const, node: m.node, label: m.label, canonicalPath: m.canonicalPath, type: m.type })),
      ...filteredAddNodeResults.map((r) => ({ kind: 'index' as const, entity: r })),
    ];
  }, [addNodeQuery, graphSearchResults, filteredAddNodeResults]);

  const addNodeQueryTrim = String(addNodeQuery || '').trim();

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

  const [askGraphOpen, setAskGraphOpen] = React.useState(false);
  const [askGraphQuery, setAskGraphQuery] = React.useState('');
  const [askGraphLoading, setAskGraphLoading] = React.useState(false);
  const [askGraphError, setAskGraphError] = React.useState<string | null>(null);
  const askGraphInputRef = React.useRef<HTMLInputElement | null>(null);
  const [graphQueryChips, setGraphQueryChips] = React.useState<GraphQueryChip[]>([]);
  const [graphQueryActiveChipId, setGraphQueryActiveChipId] = React.useState<string | null>(null);
  const [graphQueryHighlightIds, setGraphQueryHighlightIds] = React.useState<Set<string> | null>(null);

  const [domainOverlayOpen, setDomainOverlayOpen] = React.useState(false);
  const [domainOverlayQuery, setDomainOverlayQuery] = React.useState('');
  const [domainOverlayPreview, setDomainOverlayPreview] = React.useState<any | null>(null);
  const [domainOverlayLoading, setDomainOverlayLoading] = React.useState(false);
  const [domainOverlayExecuting, setDomainOverlayExecuting] = React.useState(false);
  const [domainOverlayError, setDomainOverlayError] = React.useState<string | null>(null);
  const domainOverlayInputRef = React.useRef<HTMLInputElement | null>(null);

  // Lens scope preview (empty state)
  const [lensName, setLensName] = React.useState<string | null>(null);
  const [lensScopePaths, setLensScopePaths] = React.useState<string[]>([]);
  const [scopePreview, setScopePreview] = React.useState<Array<{ canonicalPath: string; name?: string; type?: string }>>([]);
  const [scopePreviewLoading, setScopePreviewLoading] = React.useState(false);
  const [lensMetaLoading, setLensMetaLoading] = React.useState(false);
  const lensMetaRef = React.useRef<any | null>(null);

  // Layout mode helpers - определяют, какую логику раскладки использовать
  const isFreeformLayout = layoutMode === 'freeform';
  const isLinearLayout = layoutMode === 'linear';
  const isLevelsLayout = layoutMode === 'levels';

  // Prevent mode-switch relayout loops
  const lastAppliedLayoutModeRef = React.useRef<'freeform' | 'linear' | 'levels' | null>(null);
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
    if (!domainOverlayOpen) return;
    const t = window.setTimeout(() => domainOverlayInputRef.current?.focus(), 0);
    return () => window.clearTimeout(t);
  }, [domainOverlayOpen]);

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
        const cleaned = scope
          .map((s: any) => String(s || '').trim())
          .filter(Boolean);
        setLensScopePaths(cleaned);
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

  // Add-node: search/prefetch entities by partial name (existing but previously unused API).
  React.useEffect(() => {
    if (!addNodeOpen) return;
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
  }, [addNodeOpen, addNodeQuery]);

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
    if (layoutMode === 'levels') {
      // Use ref directly (bypassing debounce) to ensure latest closure is called immediately.
      // onArrangeRef is updated each render to point to current handleArrange.
      const fn = onArrangeRef.current;
      if (typeof fn === 'function') fn();
    }
  }, [collapsedSubcategories, layoutMode]);

  // Trigger relayout when right dock state changes (in Levels mode).
  React.useEffect(() => {
    if (!rightDockChangedRef.current) return;
    rightDockChangedRef.current = false;
    if (layoutMode === 'levels') {
      const fn = onArrangeRef.current;
      if (typeof fn === 'function') fn();
    }
  }, [rightDockStateByNodeId, layoutMode]);

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
    if (layoutMode === 'levels') {
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

  // Helper function for edge styles based on type
  // Цветовая схема общепринятая для графов знаний
  const getEdgeStyle = (edgeType: string | undefined, criticality: number | undefined) => {
    const type = (edgeType || '').toUpperCase();
    const baseWidth = criticality ? Math.max(1, criticality * 2) : 1.5;

    // 🟡 Dominance / market-defining — Amber (high salience)
    if (type === 'DOMINATES') {
      return {
        stroke: '#F59E0B', // amber-500
        strokeWidth: Math.max(baseWidth + 1.0, 2.6),
      };
    }

    // 🟦 Market hierarchy (skeleton) — Teal dashed
    if (type === 'HAS_SUBMARKET') {
      return {
        stroke: '#14B8A6', // teal-500
        strokeWidth: Math.max(1.2, baseWidth * 0.95),
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
    if (layoutMode !== 'levels') {
      setLayoutMode('levels');
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
      const CONTEXT = ['LOCATED_IN', 'OPERATES_IN', 'BASED_IN', 'HEADQUARTERED_IN', 'REGULATES', 'AUDITED_BY', 'EXTRACTS_FROM', 'ADMINISTERS'];
      const MARKET = ['COMPETES_WITH', 'CONFLICTS_WITH', 'OPPOSES', 'PARTNERS_WITH', 'COOPERATES_WITH', 'RELATED_TO', 'RELATIVE_OF', 'SIMILAR_TO', 'LINKED_TO', 'HAS_SUBMARKET', 'HAS_SUBGROUP'];
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

            const baseStyle = getEdgeStyle(et, (e as any)?.criticality);
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
    let label: string | undefined;
    try {
      const currentNodes: Node<HorizonNodeData>[] = nodesStateRef.current || [];
      const n = currentNodes.find((x) => String((x.data as any)?.fragmentId || x.id || '').trim() === fid);
      const d: any = n?.data;
      label = (d?.label as string) || (d?.canonicalPath as string) || undefined;
    } catch {
      // ignore
    }
    setScopeScoutModal({ fragmentId: fid, canonicalPath, label });
  }, []);

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
    'HAS_SUBMARKET': 3,
    'HAS_SUBGROUP': 3,
    'ADMINISTERS': 3,
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
      const edges = Array.isArray(edgesIn) ? edgesIn : [];
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

    const eps = 8; // px threshold to treat as "same column"

    const isInGroup = (n: Node<HorizonNodeData>) => {
      const pid = (n as any).parentId || (n as any).parentNode;
      return Boolean(pid);
    };

    const normalizedEdgeTypeOf = (e: any): string => {
      const raw = (e as any)?.data?.edgeType || (e as any)?.data?.subkind || (e as any)?.label || (e as any)?.type || '';
      return String(raw || '').toUpperCase().trim() || 'OTHER';
    };

    const normalizeTypeKey = (t: any) => String(t || '').toUpperCase().trim().replace(/\s+/g, '_') || 'UNKNOWN';
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
        const baseStyle = (ed as any)?.baseStyle || getEdgeStyle(edgeType, (ed as any)?.criticality);
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
        const gx = typeof (gNode as any)?.position?.x === 'number' ? (gNode as any).position.x : 0;
        const ox = typeof (oNode as any)?.position?.x === 'number' ? (oNode as any).position.x : 0;
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

    // Emit stack group proxy edges (right-side expanded stacks)
    for (const b of stackGroupProxyBuckets.values()) {
      const { groupId, outsideId, edgeType, groupAsSource, outsideAsSource } = b;
      const port = stackGroupPortsByGroupId.get(groupId)?.get(edgeType) ||
                   stackGroupPortsByGroupId.get(groupId)?.get('OTHER') ||
                   { sourceHandleId: 'source-left', targetHandleId: 'target-left' };
      const baseStyle = getEdgeStyle(edgeType, undefined);
      const stroke = getEdgeStroke(baseStyle);
      const id = `levels:stackgproxy:${groupId}:node:${outsideId}:edge:${edgeType}`;
      const aggKey = `stackgroup:${groupId}::node:${outsideId}::edge:${edgeType}`;
      const groupToOutside = groupAsSource >= outsideAsSource;
      const gNode = groupMap.get(groupId);
      const oNode = nodeMap.get(outsideId);
      const gx = typeof (gNode as any)?.position?.x === 'number' ? (gNode as any).position.x : 0;
      const ox = typeof (oNode as any)?.position?.x === 'number' ? (oNode as any).position.x : 0;
      // Stack groups are on the right; edges come from the left (typically)
      const outsideIsLeftOfGroup = ox < gx - eps;
      const outsideIsRightOfGroup = ox > gx + eps;
      const outsideTargetHandle = outsideIsLeftOfGroup ? 'target-right' : (outsideIsRightOfGroup ? 'target-left' : 'target-right');
      const outsideSourceHandle = outsideIsLeftOfGroup ? 'source-right' : (outsideIsRightOfGroup ? 'source-left' : 'source-right');
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
          levelsStackGroupProxy: true,
          levelsStackGroupProxyOutsideId: outsideId,
        } as any,
      } as any);
    }

    const mapped = edgeList.map((e) => {
      const sourceNode = nodeMap.get(e.source);
      const targetNode = nodeMap.get(e.target);
      if (!sourceNode || !targetNode) return e;

      const sx = sourceNode.position?.x ?? 0;
      const sy = sourceNode.position?.y ?? 0;
      const tx = targetNode.position?.x ?? 0;
      const ty = targetNode.position?.y ?? 0;

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

        const eid = String((e as any)?.id || '').trim();
        const aggKey = eid ? perChildEdgeIdsToAggKey.get(eid) : undefined;
        const d0: any = (e as any)?.data || {};
        const noHover = eid && noHoverRevealEdgeIds.has(eid);

        // If this is an intra-group edge (same group), hide by aggregation even if there's no aggKey.
        // (No agg edge exists; we just keep the canvas clean and reveal only on hover.)
        if (sameLeftGroup && !aggKey) {
          const d = { ...d0, levelsHiddenByAggregation: true, ...(noHover ? { levelsNoHoverReveal: true } : {}) };
          return { ...e, data: d };
        }

        const d = aggKey
          ? { ...d0, levelsHiddenByAggregation: true, levelsAggKey: aggKey, ...(noHover ? { levelsNoHoverReveal: true } : {}) }
          : stripAggFlags(d0);
        return { ...e, data: d };
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
          ...(tInStackGroup
            ? { sourceHandle: 'source-right', targetHandle: 'target-left' }
            : { sourceHandle: 'source-left', targetHandle: 'target-right' }),
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
          ...(sInStackGroup
            ? { sourceHandle: 'source-left', targetHandle: 'target-right' }
            : { sourceHandle: 'source-right', targetHandle: 'target-left' }),
        };
      }

      // Group nodes: always right side
      if (sGrouped) {
        sourceHandle = 'source-right';
      } else {
        if (tx > sx + eps) sourceHandle = 'source-right';
        else if (tx < sx - eps) sourceHandle = 'source-left';
        else sourceHandle = (isStackProxy || isPeerEdge) ? 'source-right' : ((ty > sy) ? 'source-bottom' : 'source-top');
      }

      if (tGrouped) {
        targetHandle = 'target-right';
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

      const sx = sourceNode.position?.x ?? 0;
      const sy = sourceNode.position?.y ?? 0;
      const tx = targetNode.position?.x ?? 0;
      const ty = targetNode.position?.y ?? 0;

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
        if (sameLeftGroup) {
          const eid = String((e as any)?.id || '').trim();
          const aggKey = eid ? perChildEdgeIdsToAggKey.get(eid) : undefined;
          const d0: any = (e as any)?.data || {};
          const noHover = eid && noHoverRevealEdgeIds.has(eid);
          const d = aggKey
            ? { ...d0, levelsHiddenByAggregation: true, levelsAggKey: aggKey, ...(noHover ? { levelsNoHoverReveal: true } : {}) }
            : { ...stripAggFlags(d0), levelsHiddenByAggregation: true, ...(noHover ? { levelsNoHoverReveal: true } : {}) };
          // Handles can remain as computed defaults; only visibility is affected.
          return { ...e, data: d };
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
          ...(tInStackGroup
            ? { sourceHandle: 'source-right', targetHandle: 'target-left' }
            : { sourceHandle: 'source-left', targetHandle: 'target-right' }),
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
          ...(sInStackGroup
            ? { sourceHandle: 'source-left', targetHandle: 'target-right' }
            : { sourceHandle: 'source-right', targetHandle: 'target-left' }),
        };
      }

      // Edge no longer points into a group-child: clear any stale aggregation-hiding flags
      // (this is the key fix for "node left group => show its original connection again").
      const d0: any = (e as any)?.data || {};
      const hadAggHide = Boolean(d0.levelsHiddenByAggregation) || Boolean(d0.levelsAggKey);
      const eOut: any = hadAggHide ? { ...e, data: stripAggFlags(d0) } : e;

      if (sGrouped) sourceHandle = 'source-right';
      else {
        if (tx > sx + eps) sourceHandle = 'source-right';
        else if (tx < sx - eps) sourceHandle = 'source-left';
        else sourceHandle = (isStackProxy || isPeerEdge) ? 'source-right' : ((ty > sy) ? 'source-bottom' : 'source-top');
      }

      if (tGrouped) targetHandle = 'target-right';
      else {
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

  // ELK layout options
  const ELK_NODE_WIDTH = 180;
  // Layout height includes the "+" button protruding half outside the visual card:
  // visual card is 148px, plus button is 26px => add radius 13px => 161px.
  const ELK_NODE_HEIGHT = 161;
  const ELK_NODE_SPACING = 60;
  const ELK_LAYER_SPACING = 150; // Увеличено для incoming/outgoing
  const ELK_MAX_CHILD_ROWS = 3; // Максимум 3 строки детей

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
      setRiskShieldActiveNodeId(null);
      return;
    }

    const loadLensItems = async () => {
      try {
        // New lens / reload: reset risk shield state.
        setRiskShieldActiveNodeId(null);
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
        if (!nodeId) {
          console.warn('[Cockpit] Node item missing both fragmentId and id:', item);
          return;
        }
        
        if (fragmentId) {
          fragmentToNodeId.set(fragmentId, nodeId);
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
        const sourceId = sFrag ? fragmentToNodeId.get(sFrag) : null;
        const targetId = tFrag ? fragmentToNodeId.get(tFrag) : null;

        if (sourceId && targetId) {
          const baseStyle = getEdgeStyle(item.edgeType, item.criticality);
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
    setRiskShieldActiveNodeId(null);

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

      // Создаём узлы из fragments (с временными позициями, ELK пересчитает)
      fragments.forEach((fragment: any, index: number) => {
        const nodeId = norm(fragment.id);
        const fragmentId = nodeId;
        fragmentToNodeId.set(fragmentId, nodeId);

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

          const baseStyle = getEdgeStyle(edge.type || edge.edgeType, edge.properties?.criticality);
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

      // 4. Применяем ELK layout и отображаем граф
      dbg('[Cockpit] Applying ELK layout to nodes:', graphNodes.length);
      const layoutedNodes = await layoutFullGraph(graphNodes, filteredEdges, {
        direction: 'DOWN',
        nodeWidth: ELK_NODE_WIDTH,
        nodeHeight: ELK_NODE_HEIGHT,
        nodeSpacing: ELK_NODE_SPACING,
        layerSpacing: ELK_LAYER_SPACING,
        maxChildRows: ELK_MAX_CHILD_ROWS,
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
          reactFlowInstanceRef.current.fitView({ padding: 0.1, duration: 300 });
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

    const record = expansionsRef.current.get(fragmentId);
    
    // Fallback: if no expansion record exists but the node is marked as expanded,
    // still clear the expanded flag so the UI updates correctly.
    // This can happen if the record was lost due to race conditions or errors.
    if (!record) {
      console.warn('[Cockpit] No expansion record found for', fragmentId, '- clearing expanded flag only');
      setNodes((prev) =>
        (prev || []).map((n) => {
          if (n.id === fragmentId || (n.data as any)?.fragmentId === fragmentId) {
            return { ...n, data: { ...n.data, expanded: false, isLoadingRelated: false } as any };
          }
          return n;
        }),
      );
      expandedNodesRef.current.delete(fragmentId);
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
      let withGroups: Node<HorizonNodeData>[] = fragmentNodes as any;
      let outEdges: Edge[] = nextEdges;

      if (isLinearLayout) {
        const arranged = await layoutLinearFullGraph(fragmentNodes as any, nextEdges as any, {
          direction: 'DOWN',
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
        });
        const withCallbacks = arranged.map((node) => ({
          ...node,
          data: { ...(node.data as any), onLoadRelated: invokeLoadRelated, onCollapse: invokeCollapse, onScopeScout: invokeScopeScout },
        }));
        const updatedEdges = recalculateLinearHandles(nextEdges, withCallbacks);
        setNodes(withCallbacks as any);
        setEdges(updatedEdges as any);
        setTimeout(() => focusOnFragment(fragmentId), 80);
        return;
      }
      if (isLevelsLayout) {
        // Levels (Hierarchical): contract-driven Two-Pane layout.
        // IMPORTANT: pass through existing group nodes so pagination state can be preserved.
        const nextNodesNoRightStacks = (nextNodes as any[]).filter((n) => {
          const id = String((n as any)?.id || '').trim();
          const d: any = (n as any)?.data;
          if (id.startsWith('stack:group:')) return false;
          if (Boolean(d?.isStackGroup)) return false;
          return true;
        });
        const arranged = await layoutLevelsTwoPane(nextNodesNoRightStacks as any, nextEdges as any, {
          direction: 'DOWN',
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
          collapsedSubcategories,
          onToggleSubcategoryCollapse: toggleSubcategoryCollapse,
          compactLeftPane: true,
          rightDockMode: true,
          rightDockStateByNodeId,
          onToggleRightDock: toggleRightDock,
        });
        const withCallbacks = arranged.map((node) => {
          if ((node.data as any)?.isGroup) {
            return { ...node, data: { ...node.data, onGroupPageChange: handleGroupPageChange } };
          }
          return {
            ...node,
            data: { ...(node.data as any), onLoadRelated: invokeLoadRelated, onCollapse: invokeCollapse, onScopeScout: invokeScopeScout },
          };
        });
        const updatedEdges = recalculateLevelsTwoPaneHandles(nextEdges, withCallbacks as any);
        setNodes(withCallbacks as any);
        setEdges(updatedEdges as any);
        setTimeout(() => focusOnFragment(fragmentId), 80);
        return;
      }

      // Freeform (and fallback): rebuild group containers for current fragment nodes
      withGroups = rebuildGroupContainers(
        await layoutFullGraph(fragmentNodes as any, nextEdges as any, {
          direction: 'DOWN',
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
        }),
        { nodeWidth: ELK_NODE_WIDTH, nodeHeight: ELK_NODE_HEIGHT },
      ) as any;

      const withCallbacks = (withGroups as any[]).map((node) => {
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
      // Freeform handle recalculation is not strictly required here; keep existing edge handles.
      setNodes(withCallbacks as any);
      setEdges(outEdges as any);
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
    isFreeformLayout,
    setGridState,
    recalculateLinearHandles,
    recalculateLevelsTwoPaneHandles,
    handleGroupPageChange,
    invokeLoadRelated,
    invokeCollapse,
    invokeScopeScout,
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
      const dependencies = await api.getFragmentDependencies(
        fragmentId,
        isLevelsLayout
          ? {
              direction: 'both',
              // In Levels mode we can request the backend to pre-filter by edge types (focus filter)
              ...(levelsExpandEdgeTypes.length > 0 ? { edgeTypes: levelsExpandEdgeTypes } : {}),
              includeFragments: true,
              // Default limits for Hierarchical expansion (4 quadrants × 18)
              maxNeighbors: 72,
              maxPerType: 18,
              limit: 500,
            }
          : { direction: 'both' }
      );
      dbg('[Cockpit] Loaded dependencies for fragment:', fragmentId, dependencies);

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

      for (const n of currentNodes) {
        const fid = n.data?.fragmentId ? norm(n.data.fragmentId) : '';
        const nid = norm(n.id);
        if (nid) nodeById.set(nid, n);
        if (fid) nodeByFragmentId.set(fid, n);
        if (fid) fragmentToNodeIdMap.set(fid, nid);
      }

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

      // Also check existing edges on canvas to catch relations not returned by dependencies API.
      // In Levels mode we intentionally keep expansions strict to the backend filter (edgeTypes),
      // so we skip this "supplemental" path to avoid pulling unrelated nodes into the hierarchy.
      if (!isLevelsLayout) {
        // const currentEdges = (reactFlowInstanceRef.current as any)?.getEdges?.() || []; // Variable already declared above
        for (const e of currentEdges) {
          if (norm(e.source) === fragmentId && !expandedNodesRef.current.has(norm(e.target))) {
            // Outgoing edge from current node to existing target
            const tId = norm(e.target);
            const targetNode = nodeById.get(tId) || nodeByFragmentId.get(tId);
            if (targetNode && !targetNode.data?.parentFragmentId) {
              existingRelatedIds.add(tId);
            }
          }
          if (norm(e.target) === fragmentId && !expandedNodesRef.current.has(norm(e.source))) {
            // Incoming edge from existing source to current node
            const sId = norm(e.source);
            const sourceNode = nodeById.get(sId) || nodeByFragmentId.get(sId);
            if (sourceNode && !sourceNode.data?.parentFragmentId) {
              existingRelatedIds.add(sId);
            }
          }
        }
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
      
      // debug logs removed
      
      const updateEdgeInfo = (neighborId: string, edgeType: string, direction: 'incoming' | 'outgoing') => {
        // In Levels mode we interpret some relations with inverted "up/down" meaning for placement:
        // - DEPENDS_ON: if neighbor → central (incoming), neighbor is a dependent (downstream) => treat as 'outgoing' for grouping/placement
        // - DEPENDS_ON: if central → neighbor (outgoing), neighbor is a prerequisite (upstream) => treat as 'incoming'
        let effectiveDirection: 'incoming' | 'outgoing' = direction;
        const etUpper = String(edgeType || '').toUpperCase().trim();
        const DEP_TYPES = new Set(['DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM']);
        if (layoutMode === 'levels' && DEP_TYPES.has(etUpper)) {
          effectiveDirection = direction === 'incoming' ? 'outgoing' : 'incoming';
        }

        const existingType = fragmentEdgeType.get(neighborId);
        const existingDir = fragmentDirection.get(neighborId);
        
        // Helper to check if edge type is an "incoming dependency" type
        // These types, when incoming to central node, mean "they own/control/supply us" → should go to TOP row
        // (Previously incorrectly stated BOTTOM row in comments - incoming deps are UPSTREAM)
        const isIncomingDependencyType = (t: string) => {
          const upper = (t || '').toUpperCase().trim();
          return (
            // Ownership / Control (Strongest)
            upper === 'OWNS' || upper === 'PARENT_OF' || upper === 'SUBSIDIARY_OF' || upper === 'MANAGES' || upper === 'CONTROLS' ||
            // Supply / Dependency
            upper === 'DEPENDS_ON' || upper === 'REQUIRES' || upper === 'NEEDS' || upper === 'USES' || upper === 'CONSUMES' || upper === 'EXTRACTS_FROM'
          );
        };
        
        if (!existingType) {
          // debug logs removed
          fragmentEdgeType.set(neighborId, edgeType);
          fragmentDirection.set(neighborId, effectiveDirection);
        } else {
          // === LOCATED_IN vs DEPENDS_ON OVERRIDE ===
          // If both LOCATED_IN and DEPENDS-like relations exist between an ORG/GOV and a GEO node,
          // we want the GEO placement to be driven by LOCATED_IN (when directed correctly: org -> geo).
          // Otherwise GEO nodes can be misplaced into dependency groups.
          const typeOf = (fid: string): string => {
            const f = byId.get(String(fid || '').trim());
            const t0 = String(f?.type || '').trim();
            if (t0) return t0.toUpperCase().replace(/\s+/g, '_');
            const n = nodeByFragmentId.get(String(fid || '').trim());
            const t1 = String((n as any)?.data?.type || '').trim();
            return t1 ? t1.toUpperCase().replace(/\s+/g, '_') : '';
          };
          const isOrgLikeType = (t: string, fid: string): boolean => {
            const tt = String(t || '').toUpperCase().trim();
            if (tt === 'ORGANIZATION') return true;
            const f = byId.get(String(fid || '').trim());
            const cp = String(f?.canonicalPath || '').trim();
            return Boolean(cp && (cp.startsWith('/org/') || cp.startsWith('/gov/')));
          };
          const isGeoType = (t: string, fid: string): boolean => {
            const tt = String(t || '').toUpperCase().trim();
            if (tt === 'GEO_REGION' || tt === 'LOCATION') return true;
            if (tt.includes('GEO')) return true;
            const f = byId.get(String(fid || '').trim());
            const cp = String(f?.canonicalPath || '').trim();
            return Boolean(cp && cp.startsWith('/loc/'));
          };
          const centralType = typeOf(fragmentId);
          const neighborType = typeOf(neighborId);
          const isCentralOrgLike = isOrgLikeType(centralType, fragmentId); // includes /gov/*
          const isNeighborOrgLike = isOrgLikeType(neighborType, neighborId);
          const isNeighborGeo = isGeoType(neighborType, neighborId);

          // === LEVELS PRODUCT OVERRIDE ===
          // In Levels mode, if the expanded node has an OUTGOING PRODUCES edge to a neighbor,
          // it must dominate placement/grouping even if other edge types exist (including dependencies).
          // Otherwise products can "escape" into the right backbone due to dependency heuristics.
          const existingUpper = String(existingType || '').toUpperCase().trim();
          const newUpper = String(edgeType || '').toUpperCase().trim();

          if (layoutMode === 'levels' && isCentralOrgLike && isNeighborGeo) {
            const isDepLike = (u: string) => DEP_TYPES.has(String(u || '').toUpperCase().trim());
            const existingIsLocatedOut = existingUpper === 'LOCATED_IN' && existingDir === 'outgoing';
            const newIsLocatedOut = newUpper === 'LOCATED_IN' && effectiveDirection === 'outgoing';

            // If LOCATED_IN (org -> geo) is already chosen, do NOT let DEPENDS-like edges override it.
            if (existingIsLocatedOut && isDepLike(newUpper)) {
              return;
            }
            // If we currently have a DEPENDS-like edge, but we see LOCATED_IN (org -> geo), promote LOCATED_IN.
            if (isDepLike(existingUpper) && newIsLocatedOut) {
              fragmentEdgeType.set(neighborId, edgeType);
              fragmentDirection.set(neighborId, effectiveDirection);
              return;
            }
          }

          // === INFLUENCE vs DEPENDS OVERRIDE ===
          // If an ORG/GOV influences another ORG, this semantic is stronger than dependency for placement/grouping,
          // even if dependency edges exist (and even if they point in the opposite direction).
          // Otherwise "Depends" can hide the actual semantic and misplace the influencer/target in the wrong group.
          if (isCentralOrgLike && isNeighborOrgLike) {
            const INFL = new Set(['INFLUENCES', 'AFFECTS', 'IMPACTS', 'REGULATES']);
            const PARTNER = new Set(['PARTNERS_WITH', 'COOPERATES_WITH']);
            const existingIsInfluence = INFL.has(existingUpper);
            const newIsInfluence = INFL.has(newUpper);
            const existingIsDepLike = DEP_TYPES.has(existingUpper);
            const newIsDepLike = DEP_TYPES.has(newUpper);
            const existingIsPartner = PARTNER.has(existingUpper);
            const newIsPartner = PARTNER.has(newUpper);

            // Partnership dominates both influence and dependency regardless of direction/priority.
            if (existingIsPartner && (newIsInfluence || newIsDepLike)) return;
            if (!existingIsPartner && newIsPartner && (existingIsInfluence || existingIsDepLike)) {
              fragmentEdgeType.set(neighborId, edgeType);
              fragmentDirection.set(neighborId, effectiveDirection);
              return;
            }

            // Keep influence if already chosen.
            if (existingIsInfluence && newIsDepLike) return;
            // Promote influence over dependency regardless of direction/priority.
            if (existingIsDepLike && newIsInfluence) {
              fragmentEdgeType.set(neighborId, edgeType);
              fragmentDirection.set(neighborId, effectiveDirection);
              return;
            }
          }

          const existingIsProducesOut = layoutMode === 'levels' && existingUpper === 'PRODUCES' && existingDir === 'outgoing';
          const newIsProducesOut = layoutMode === 'levels' && newUpper === 'PRODUCES' && effectiveDirection === 'outgoing';
          if (existingIsProducesOut) return;
          if (newIsProducesOut) {
            fragmentEdgeType.set(neighborId, edgeType);
            fragmentDirection.set(neighborId, effectiveDirection);
            return;
          }

          // === LEVELS CONTROL > DEPENDENCY OVERRIDE ===
          // If a node has both a control/management signal and a dependency signal to the expanded node,
          // the control signal must dominate. Otherwise the node can be misclassified (e.g., not bucketed into Stakeholders).
          const LEVELS_CONTROL_TYPES = new Set(['OWNS', 'PARENT_OF', 'MANAGES', 'CONTROLS', 'INVESTS_IN', 'SUBSIDIARY_OF']);
          const LEVELS_DEP_TYPES = new Set(['DEPENDS_ON', 'REQUIRES', 'NEEDS', 'USES', 'CONSUMES', 'EXTRACTS_FROM']);
          const existingIsControl = layoutMode === 'levels' && LEVELS_CONTROL_TYPES.has(existingUpper);
          const newIsControl = layoutMode === 'levels' && LEVELS_CONTROL_TYPES.has(newUpper);
          const existingIsDep = layoutMode === 'levels' && LEVELS_DEP_TYPES.has(existingUpper);
          const newIsDep = layoutMode === 'levels' && LEVELS_DEP_TYPES.has(newUpper);
          if (existingIsControl && newIsDep) return;
          if (newIsControl && existingIsDep) {
            fragmentEdgeType.set(neighborId, edgeType);
            fragmentDirection.set(neighborId, effectiveDirection);
            return;
          }

          // === LEVELS CONTROL OVERRIDE ===
          // If there is a control/ownership edge from the expanded node to the neighbor (OUTGOING),
          // it must dominate over influence edges. Otherwise a node can be incorrectly promoted to the right backbone
          // due to an incoming INFLUENCES edge, and "miss" the left group.
          const LEVELS_INFLUENCE_TYPES = new Set(['INFLUENCES', 'AFFECTS', 'IMPACTS', 'REGULATES']);
          const existingIsControlOut =
            layoutMode === 'levels' && LEVELS_CONTROL_TYPES.has(existingUpper) && existingDir === 'outgoing';
          const newIsControlOut =
            layoutMode === 'levels' && LEVELS_CONTROL_TYPES.has(newUpper) && effectiveDirection === 'outgoing';
          const existingIsInfluence = layoutMode === 'levels' && LEVELS_INFLUENCE_TYPES.has(existingUpper);
          const newIsInfluence = layoutMode === 'levels' && LEVELS_INFLUENCE_TYPES.has(newUpper);
          if (existingIsControlOut && newIsInfluence) return;
          if (newIsControlOut && existingIsInfluence) {
            fragmentEdgeType.set(neighborId, edgeType);
            fragmentDirection.set(neighborId, effectiveDirection);
            return;
          }

          // === LINEAR LAYOUT SPECIAL LOGIC ===
          // In Linear mode, incoming dependency edges (neighbor DEPENDS_ON central) MUST determine placement.
          // These nodes should ALWAYS go to BOTTOM row, regardless of other edges they might have.
          if (layoutMode === 'linear') {
            // 1. If EXISTING is an incoming dependency, NEVER override it
            if (existingDir === 'incoming' && isIncomingDependencyType(existingType)) {
              // debug logs removed
              return;
            }
            
            // 2. If NEW is an incoming dependency, it ALWAYS wins
            if (effectiveDirection === 'incoming' && isIncomingDependencyType(edgeType)) {
              // debug logs removed
              fragmentEdgeType.set(neighborId, edgeType);
              fragmentDirection.set(neighborId, effectiveDirection);
              return;
            }
          }
          
          // === DEFAULT PRIORITY LOGIC ===
          // Если уже есть связь, проверяем приоритет. 
          // Меньшее число = более высокий приоритет (важнее).
          const existingPriority = getEdgePriority(existingType);
          const newPriority = getEdgePriority(edgeType);
          if (newPriority < existingPriority) {
            // debug logs removed
            fragmentEdgeType.set(neighborId, edgeType);
            fragmentDirection.set(neighborId, effectiveDirection);
          } else {
            // debug logs removed
          }
        }
      };
      
      for (const e of filteredDepEdges) {
        const s = String(e?.sourceFragmentId || '').trim();
        const t = String(e?.targetFragmentId || '').trim();
        const edgeType = e?.type || e?.edgeType || '';
        
        // Если центральная нода — source, то ребро: central → neighbor (для neighbor = outgoing)
        if (s === fragmentId && t) {
          // debug logs removed
          updateEdgeInfo(t, edgeType, 'outgoing');
        }
        // Если центральная нода — target, то ребро: neighbor → central (для neighbor = incoming) 
        if (t === fragmentId && s) {
          // debug logs removed
          updateEdgeInfo(s, edgeType, 'incoming');
        }
      }

      // Levels: keep expansions strict to backend edgeTypes, BUT allow an important exception:
      // If an expanded node already has a PRODUCES edge on canvas to a neighbor, it must dominate placement
      // over DEPENDS-like edges (otherwise products can be misplaced).
      if (isLevelsLayout) {
        for (const e of currentEdges) {
          const s = norm(e.source);
          const t = norm(e.target);
          const edgeType0 = (e as any)?.data?.edgeType || (e as any)?.type || '';
          const u = String(edgeType0 || '').toUpperCase().trim();
          if (u !== 'PRODUCES') continue;

          // Normalize node ids -> fragment ids (ReactFlow ids can be lensItemId in some flows)
          const sNode = nodeById.get(s) || nodeByFragmentId.get(s);
          const tNode = nodeById.get(t) || nodeByFragmentId.get(t);
          const sFrag = norm((sNode?.data as any)?.fragmentId || s);
          const tFrag = norm((tNode?.data as any)?.fragmentId || t);

          // Only OUTGOING from expanded -> neighbor matters for the strong override.
          if (sFrag === fragmentId && tFrag) {
            updateEdgeInfo(tFrag, 'PRODUCES', 'outgoing');
          }
        }
      }

      // Also populate edge info from current canvas edges if missing from API deps.
      // In Levels mode, keep behavior strict to the backend response.
      if (!isLevelsLayout) for (const e of currentEdges) {
        const s = norm(e.source);
        const t = norm(e.target);
        const edgeType = e.data?.edgeType || e.type || '';

        // Normalize node ids -> fragment ids (ReactFlow ids can be lensItemId in some flows)
        const sNode = nodeById.get(s) || nodeByFragmentId.get(s);
        const tNode = nodeById.get(t) || nodeByFragmentId.get(t);
        const sFrag = norm((sNode?.data as any)?.fragmentId || s);
        const tFrag = norm((tNode?.data as any)?.fragmentId || t);

        if (sFrag === fragmentId && tFrag) {
          updateEdgeInfo(tFrag, edgeType, 'outgoing');
        }
        if (tFrag === fragmentId && sFrag) {
          updateEdgeInfo(sFrag, edgeType, 'incoming');
        }
      }

      // Log final edge info for all neighbors
      if (layoutMode === 'linear' || layoutMode === 'levels') {
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
            depth: childDepth,
            canonicalPath: fragment.canonicalPath,
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
        } else if (isLinearLayout || isLevelsLayout) {
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

        const baseStyle = getEdgeStyle(e?.type || e?.edgeType, e?.properties?.criticality);
        const stroke = getEdgeStroke(baseStyle);
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
          style: baseStyle,
          data: {
            lensItemId: '',
            edgeType: e?.type || e?.edgeType,  // Used for label on hover
            edgeCaption: getEdgeCaptionShort(e?.type || e?.edgeType),
            criticality: e?.properties?.criticality,
            amplification: e?.properties?.amplification,
            latencyHours: e?.properties?.latencyHours,
            direction: 'forward',
            baseStyle,
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
      if (isLevelsLayout && isNodeInGroup) {
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

        // Skip if not an existing related node, or if it already has a parent, or if it's the expanded node
        if (!existingRelatedIds.has(nodeFragmentId)) return n;
        if (n.data?.parentFragmentId) return n; // Already has a parent, don't override
        if (nodeFragmentId === fragmentId) return n;
        // Double-check: never group already expanded nodes
        // Note: we allow grouping the current rootNodeId if it gets absorbed by another expanding node
        if (expandedNodesRef.current.has(nodeFragmentId)) return n;
        
        // Group logic:
        // 1. If node is already part of another subgraph (depth > 0 and different root), don't steal it.
        // 2. If node is a root (depth == 0) and NOT expanded, we CAN absorb it into this graph.
        const nodeDepth = typeof (n.data as any)?.depth === 'number' ? (n.data as any).depth : undefined;
        const nodeRoot = norm((n.data as any)?.rootFragmentId || '');
        
        if (nodeDepth !== 0 && nodeRoot && nodeRoot !== sourceRootId) return n;

        const edgeTypeFromParent = fragmentEdgeType.get(nodeFragmentId) || '';
        const directionFromParent = fragmentDirection.get(nodeFragmentId) || 'outgoing';

        dbg('[Cockpit] Updating existing node for grouping (freeform):', nodeFragmentId, { edgeTypeFromParent, directionFromParent });

        const updated = {
          ...n,
          data: {
            ...n.data,
            // Freeform uses parentFragmentId for container parenting; Linear uses global section containers,
            // but still needs edgeType/direction + depth for correct quadrant placement.
            ...(isFreeformLayout || isLevelsLayout ? { parentFragmentId: fragmentId } : {}),
            edgeTypeFromParent,
            directionFromParent,
            // Make absorbed nodes non-central so they move into section groups in Linear.
            depth: childDepth,
            // Keep consistent root marker (Linear treats expanded node as the local root).
            rootFragmentId: sourceRootId,
          },
        };
        return updated;
      });

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
      } else if (isLinearLayout) {
        // Linear layout is GLOBAL:
        // after any expansion we re-layout the whole visible graph so that:
        // - expanded node becomes depth=0 and moves to central row
        // - no per-node sub-groups are created; children go into existing global sections
        // - central row is centered relative to full graph width
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

        const fragmentNodes = [...updatedCurrentNodes, ...newNodes].filter((n) => !(n?.data as any)?.isGroup);
        const filteredMergedEdges = suppressDominatedEdges(mergedEdges as any, fragmentNodes as any);
        layoutedNodes = await layoutLinearFullGraph(fragmentNodes as any, filteredMergedEdges as any, {
          direction: 'DOWN',
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
        });

        // Apply Linear handle rules using the freshly updated node data (edgeTypeFromParent/directionFromParent)
        linearEdgesAfterLayout = recalculateLinearHandles(filteredMergedEdges as any, layoutedNodes);
      } else if (isLevelsLayout) {
        // Levels layout (contract-driven): Two-Pane model
        // - right column: expanded nodes (plus depth=0 roots)
        // - left blocks: paged 1-row upstream/downstream groups per right node
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

        // Keep any existing group nodes in the input so pagination state can be preserved.
        const allNodes = [...updatedCurrentNodes, ...newNodes];
        const filteredMergedEdges = suppressDominatedEdges(mergedEdges as any, allNodes as any);
        const allNodesNoRightStacks = (allNodes as any[]).filter((n) => {
          const id = String((n as any)?.id || '').trim();
          const d: any = (n as any)?.data;
          if (id.startsWith('stack:group:')) return false;
          if (Boolean(d?.isStackGroup)) return false;
          return true;
        });
        layoutedNodes = await layoutLevelsTwoPane(allNodesNoRightStacks as any, filteredMergedEdges as any, {
          direction: 'DOWN',
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
          collapsedSubcategories,
          onToggleSubcategoryCollapse: toggleSubcategoryCollapse,
          compactLeftPane: true,
          rightDockMode: true,
          rightDockStateByNodeId,
          onToggleRightDock: toggleRightDock,
        });
        linearEdgesAfterLayout = recalculateLevelsTwoPaneHandles(filteredMergedEdges as any, layoutedNodes as any);
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
        // Remove loading flag from expanded node
        if (n.id === fragmentId || n.data.fragmentId === fragmentId) {
          updates.isLoadingRelated = false;
          updates.expanded = true;
          updates.collapsed = false;
          updates.loadRelatedAttempted = true;
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
      if ((isLinearLayout || isLevelsLayout) && linearEdgesAfterLayout) {
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
      if (isFreeformLayout || isLevelsLayout) {
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

      // Подгоняем вид к родителю и новым узлам
      const newNodeIds = newNodes.map((n) => n.id);
      setTimeout(() => {
        if (reactFlowInstanceRef.current) {
          const inst: any = reactFlowInstanceRef.current;
          const toFit: any[] = [];
          const parent = inst.getNode?.(fragmentId);
          if (parent) toFit.push(parent);
          for (const id of newNodeIds) {
            const n = inst.getNode?.(id);
            if (n) toFit.push(n);
          }
          if (toFit.length > 0) {
            inst.fitView?.({ nodes: toFit, padding: 0.25, duration: 400 });
          }
          // Always keep the expanded/collapsed node in focus (centered).
          focusOnFragment(fragmentId);
        }
      }, 100);

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
    isFreeformLayout,
    levelsExpandEdgeTypes,
    gridState,
    setEdges,
    setNodes,
    normalizeEdgeType,
    getEdgeHandles,
    handleGroupPageChange,
    recalculateLinearHandles,
    recalculateFreeformHandles,
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
    if (perfMode) return null as null | { nodeIds: Set<string>; edgeIds: Set<string> };
    if (hoverHighlightMode === 'off') return null;
    const hovered = String(hoveredNodeId || '').trim();
    if (!hovered) return null;
    const isControlLike = hoverHighlightMode === 'control' || hoverHighlightMode === 'expansion';

    // Single-pass node iteration: collect visible nodes, groups, positions, and types
    // (Optimization: previously this was 2-3 separate iterations over nodes)
    const visibleNodeIds = new Set<string>();
    const groupIds = new Set<string>();
    const xById = new Map<string, number>();
    const typeById = new Map<string, string>();
    let hoveredNode: typeof nodes[0] | undefined;
    
    for (let i = 0; i < nodes.length; i++) {
      const n = nodes[i]!;
      const id = String(n.id || '').trim();
      if (!id) continue;
      
      // Check if this is the hovered node
      if (id === hovered) {
        hoveredNode = n;
      }
      
      if ((n as any).hidden) continue;
      
      const isGroup = Boolean((n.data as any)?.isGroup);
      visibleNodeIds.add(id);
      
      if (isGroup) {
        groupIds.add(id);
      } else {
        // For Levels Two-Pane we can use x-position as a reliable hierarchy hint
        const x = typeof (n as any)?.position?.x === 'number' ? (n as any).position.x : undefined;
        if (typeof x === 'number' && Number.isFinite(x)) xById.set(id, x);
        // Node types (for control semantics)
        const t = String((n.data as any)?.type || '').toUpperCase().trim();
        if (t) typeById.set(id, t);
      }
    }
    
    // Early exit if hovered node not found or is a group
    if (!hoveredNode) return null;
    if ((hoveredNode.data as any)?.isGroup) return null;
    if (!visibleNodeIds.has(hovered)) return null;

    const isPerson = (id: string) => (typeById.get(id) || '') === 'PERSON';

    const normalizeType = (t: string) => String(t || '').toUpperCase().trim();
    // Control highlight uses two semantic buckets:
    // - "chain" (transitive control propagation): used for traversal up/down
    //   - equity: OWNS/PARENT_OF/SUBSIDIARY_OF/INVESTS_IN
    //   - non-equity control: CONTROLS/MANAGES
    //   - governance-as-control signal: BOARD_MEMBER_OF / EXECUTIVE_OF (used for traversal; treated as bidirectional to be robust to edge direction)
    // - "governance overlay": shown inside the cluster (not used to expand it)
    //
    // IMPORTANT: ultimate beneficiary detection (PERSON highlight) should NOT be blocked by governance-as-control edges
    // (BOARD/EXECUTIVE), otherwise a single board edge can "disqualify" a real UBO from being considered ultimate.
    const CONTROL_CHAIN_TYPES = new Set<string>([
      'OWNS', 'PARENT_OF', 'SUBSIDIARY_OF', 'INVESTS_IN',
      'CONTROLS', 'MANAGES',
      'BOARD_MEMBER_OF', 'EXECUTIVE_OF',
    ]);
    const CONTROL_GOVERNANCE_TYPES = new Set<string>(['EMPLOYED_BY', 'FOUNDED']);
    const EXPANSION_EXTRA_TYPES = new Set<string>(['PARTNERS_WITH']);
    const CONTROL_ULTIMATE_BLOCKING_TYPES = new Set<string>([
      // "Real" control signals that should prevent a PERSON from being considered ultimate if there's a higher controller in-cluster.
      // Exclude BOARD_MEMBER_OF / EXECUTIVE_OF on purpose (see comment above).
      'OWNS', 'PARENT_OF', 'SUBSIDIARY_OF', 'INVESTS_IN', 'CONTROLS', 'MANAGES',
    ]);
    const isEdgeTypeOfMode = (t: string) => {
      const u = normalizeType(t);
      switch (hoverHighlightMode) {
        case 'control':
          return CONTROL_CHAIN_TYPES.has(u) || CONTROL_GOVERNANCE_TYPES.has(u);
        case 'expansion':
          return CONTROL_CHAIN_TYPES.has(u) || CONTROL_GOVERNANCE_TYPES.has(u) || EXPANSION_EXTRA_TYPES.has(u);
        case 'production':
          return u === 'PRODUCES';
        case 'supplies':
          return u === 'SUPPLIES_TO';
        case 'competition':
          return u === 'COMPETES_WITH' || u === 'CONFLICTS_WITH' || u === 'OPPOSES';
        default:
          return false;
      }
    };

    type Adj = { to: string; edgeId: string; edgeType: string };
    type AdjIn = { from: string; edgeId: string; edgeType: string };
    // For most modes we treat the relation as undirected (we only need "neighbors").
    const adj = new Map<string, Adj[]>();
    // For control we want directional semantics:
    // - down: controller -> controlled (fan-out)
    // - up: controlled -> controller (chains only)
    // IMPORTANT: traversal uses ONLY chain types; governance edges are overlaid (not used to expand the cluster).
    const adjOut = new Map<string, Adj[]>();
    const adjIn = new Map<string, AdjIn[]>();
    const governanceEdges: Array<{ id: string; a: string; b: string }> = [];
    const partnerEdges: Array<{ id: string; a: string; b: string }> = [];

    // Two-pass approach for control edges:
    // Pass 1: collect raw directed edges by semantic type
    // Pass 2: detect conflicts (A->B and B->A) and resolve via X-position
    type RawEdge = { from: string; to: string; edgeId: string; edgeType: string };
    const rawControlEdges: RawEdge[] = [];
    // Track node pairs that have edges in each direction (for conflict detection)
    const pairKey = (a: string, b: string) => a < b ? `${a}|${b}` : `${b}|${a}`;
    const pairDirections = new Map<string, { aToB: RawEdge[]; bToA: RawEdge[] }>();

    const addDirected = (from: string, to: string, edgeId: string, edgeType: string) => {
      if (!from || !to) return;
      if (!adjOut.has(from)) adjOut.set(from, []);
      adjOut.get(from)!.push({ to, edgeId, edgeType });
      if (!adjIn.has(to)) adjIn.set(to, []);
      adjIn.get(to)!.push({ from, edgeId, edgeType });
    };

    for (const e of edges) {
      const eid = String((e as any)?.id || '').trim();
      const src = String((e as any)?.source || '').trim();
      const dst = String((e as any)?.target || '').trim();
      if (!eid || !src || !dst) continue;
      if (!visibleNodeIds.has(src) || !visibleNodeIds.has(dst)) continue;
      if (groupIds.has(src) || groupIds.has(dst)) continue;

      const edgeType = String((e as any)?.data?.edgeType || (e as any)?.label || (e as any)?.type || '');
      if (!isEdgeTypeOfMode(edgeType)) continue;
      const u = normalizeType(edgeType);

      // Control-like modes: use directional semantics for control chains.
      if (isControlLike) {
        // Expansion adds PARTNERS_WITH as a 1-hop overlay (not used to expand the control cluster).
        if (hoverHighlightMode === 'expansion' && EXPANSION_EXTRA_TYPES.has(u)) {
          partnerEdges.push({ id: eid, a: src, b: dst });
          continue;
        }
        if (CONTROL_GOVERNANCE_TYPES.has(u)) {
          governanceEdges.push({ id: eid, a: src, b: dst });
          continue;
        }
        if (!CONTROL_CHAIN_TYPES.has(u)) {
          continue;
        }

        // Determine "natural" direction based on edge type semantics:
        // - OWNS, INVESTS_IN, CONTROLS, MANAGES, PARENT_OF: source -> target (source controls target)
        // - SUBSIDIARY_OF: target -> source (target controls source, i.e. parent is target)
        let from = src;
        let to = dst;
        if (u === 'SUBSIDIARY_OF') {
          from = dst;
          to = src;
        }

        // BOARD_MEMBER_OF / EXECUTIVE_OF:
        // - Strong governance/control signals.
        // - If one side is PERSON, enforce direction PERSON -> OTHER (person is controller).
        if (u === 'BOARD_MEMBER_OF' || u === 'EXECUTIVE_OF') {
          const srcIsPerson = isPerson(src);
          const dstIsPerson = isPerson(dst);
          if (srcIsPerson && !dstIsPerson) {
            from = src;
            to = dst;
          } else if (dstIsPerson && !srcIsPerson) {
            from = dst;
            to = src;
          }
          // else: keep semantic direction
        }

        // Collect for conflict detection
        const pk = pairKey(from, to);
        if (!pairDirections.has(pk)) {
          pairDirections.set(pk, { aToB: [], bToA: [] });
        }
        const pd = pairDirections.get(pk)!;
        // "a" is the lexicographically smaller node in the pair key
        const aNode = from < to ? from : to;
        if (from === aNode) {
          pd.aToB.push({ from, to, edgeId: eid, edgeType: u });
        } else {
          pd.bToA.push({ from, to, edgeId: eid, edgeType: u });
        }
        rawControlEdges.push({ from, to, edgeId: eid, edgeType: u });
        continue;
      }

      // Other modes: undirected neighbor graph.
      if (!adj.has(src)) adj.set(src, []);
      if (!adj.has(dst)) adj.set(dst, []);
      adj.get(src)!.push({ to: dst, edgeId: eid, edgeType: u });
      adj.get(dst)!.push({ to: src, edgeId: eid, edgeType: u });
    }

    // Pass 2 (control-like modes): resolve conflicts using X-position, then populate adjOut/adjIn.
    if (isControlLike) {
      const EPS_X = 12;
      // For each pair with edges in both directions, decide which direction to keep.
      const conflictResolved = new Map<string, 'aToB' | 'bToA'>();
      for (const [pk, pd] of pairDirections.entries()) {
        if (pd.aToB.length > 0 && pd.bToA.length > 0) {
          // Conflict: edges in both directions. Use X-position to resolve.
          const [aNode, bNode] = pk.split('|');
          const ax = xById.get(aNode!);
          const bx = xById.get(bNode!);
          if (typeof ax === 'number' && typeof bx === 'number' && Math.abs(ax - bx) > EPS_X) {
            // Node with higher X is the controller
            if (ax > bx) {
              conflictResolved.set(pk, 'aToB'); // a controls b
            } else {
              conflictResolved.set(pk, 'bToA'); // b controls a
            }
          } else {
            // X too close: prefer edges with stronger semantics (OWNS > INVESTS_IN > others)
            const scoreType = (t: string) => {
              if (t === 'OWNS' || t === 'PARENT_OF') return 3;
              if (t === 'INVESTS_IN' || t === 'SUBSIDIARY_OF') return 2;
              return 1;
            };
            const aToBScore = Math.max(...pd.aToB.map((e) => scoreType(e.edgeType)));
            const bToAScore = Math.max(...pd.bToA.map((e) => scoreType(e.edgeType)));
            conflictResolved.set(pk, aToBScore >= bToAScore ? 'aToB' : 'bToA');
          }
        }
      }

      // Now add edges, skipping the "wrong" direction for conflicting pairs.
      for (const re of rawControlEdges) {
        const pk = pairKey(re.from, re.to);
        const resolved = conflictResolved.get(pk);
        if (resolved) {
          const aNode = re.from < re.to ? re.from : re.to;
          const isAToB = re.from === aNode;
          if ((resolved === 'aToB' && !isAToB) || (resolved === 'bToA' && isAToB)) {
            // This edge is in the "wrong" direction for this conflicting pair; skip it.
            continue;
          }
        }
        addDirected(re.from, re.to, re.edgeId, re.edgeType);
      }
    }

    if (isControlLike) {
      if (adjOut.size === 0 && adjIn.size === 0) return null;
    } else {
      if (adj.size === 0) return null;
    }

    const usedEdgeIds = new Set<string>();
    const maxSteps = isControlLike ? 6 : 1;

    // Default: undirected BFS (direct neighbors for most relations).
    if (!isControlLike) {
      const visited = new Set<string>();
      const q: Array<{ id: string; depth: number }> = [{ id: hovered, depth: 0 }];
      visited.add(hovered);

      while (q.length > 0) {
        const cur = q.shift()!;
        if (cur.depth >= maxSteps) continue;
        const next = adj.get(cur.id) || [];
        for (const a of next) {
          usedEdgeIds.add(a.edgeId);
          if (!visited.has(a.to)) {
            visited.add(a.to);
            q.push({ id: a.to, depth: cur.depth + 1 });
          }
        }
      }

      if (visited.size <= 1 || usedEdgeIds.size === 0) return null;
      return { nodeIds: visited, edgeIds: usedEdgeIds };
    }

    // Control mode: asymmetrical traversal over "chain" edges.
    // - Down: expand broadly (fan-out) to controlled entities (up to maxSteps).
    // - Up: expand only along ownership/control chains to ultimate controllers (may branch).
    const downVisited = new Set<string>([hovered]);
    const upVisited = new Set<string>([hovered]);

    // Up traversal first — determines the set of "controllers" so we can prevent down traversal
    // from leaking into the controllers' other holdings.
    const qUp: Array<{ id: string; depth: number }> = [{ id: hovered, depth: 0 }];
    while (qUp.length > 0) {
      const cur = qUp.shift()!;
      if (cur.depth >= maxSteps) continue;
      const next = adjIn.get(cur.id) || [];
      for (const a of next) {
        usedEdgeIds.add(a.edgeId);
        if (!upVisited.has(a.from)) {
          upVisited.add(a.from);
          qUp.push({ id: a.from, depth: cur.depth + 1 });
        }
      }
    }

    // Down traversal — expand only the hovered node's controlled entities, but:
    // - never expand from PERSON nodes unless hover is on a PERSON
    // - never expand from nodes that are in the up-cluster (controllers) to avoid showing their other holdings
    // - in Levels layout, only allow "leftward" steps (more subordinate) to avoid backtracking via bad edges
    const EPS_X = 12;
    const hoveredX = xById.get(hovered);
    const qDown: Array<{ id: string; depth: number }> = [{ id: hovered, depth: 0 }];
    while (qDown.length > 0) {
      const cur = qDown.shift()!;
      if (cur.depth >= maxSteps) continue;
      // When hovering a non-person entity, do not expand "down" from PERSON nodes.
      if (!isPerson(hovered) && isPerson(cur.id)) continue;
      // Do not fan out from controllers (nodes discovered by the up traversal).
      if (cur.id !== hovered && upVisited.has(cur.id)) continue;

      const next = adjOut.get(cur.id) || [];
      for (const a of next) {
        usedEdgeIds.add(a.edgeId);

        // Never traverse "down" into controllers.
        if (a.to !== hovered && upVisited.has(a.to)) {
          continue;
        }
        // In Levels layout, "down" must move LEFT (more subordinate).
        if (isLevelsLayout) {
          const cx = xById.get(cur.id);
          const tx = xById.get(a.to);
          if (
            typeof cx === 'number' &&
            typeof tx === 'number' &&
            Number.isFinite(cx) &&
            Number.isFinite(tx) &&
            // require a meaningful move to the left; also never go to the right of the hovered node
            (tx > cx - EPS_X || (typeof hoveredX === 'number' && Number.isFinite(hoveredX) && tx > hoveredX - EPS_X))
          ) {
            continue;
          }
        }

        if (!downVisited.has(a.to)) {
          downVisited.add(a.to);
          qDown.push({ id: a.to, depth: cur.depth + 1 });
        }
      }
    }

    const visited = new Set<string>();
    for (const id of downVisited) visited.add(id);
    for (const id of upVisited) visited.add(id);

    // Overlay governance edges, but do not use them to expand the cluster.
    // Rule:
    // - If we found a chain cluster, include governance edges fully inside it.
    // - If there's no chain at all, still allow 1-hop governance highlight from the hovered node (so "management-only" graphs still work).
    if (governanceEdges.length > 0) {
      if (visited.size > 1) {
        for (const ge of governanceEdges) {
          if (visited.has(ge.a) && visited.has(ge.b)) {
            usedEdgeIds.add(ge.id);
          }
        }
      } else {
        for (const ge of governanceEdges) {
          if (ge.a === hovered) {
            visited.add(ge.b);
            usedEdgeIds.add(ge.id);
          } else if (ge.b === hovered) {
            visited.add(ge.a);
            usedEdgeIds.add(ge.id);
          }
        }
      }
    }

    // Expansion mode: overlay PARTNERS_WITH as a 1-hop neighborhood from the hovered node,
    // plus include any partner edges fully inside the existing control cluster.
    if (partnerEdges.length > 0) {
      for (const pe of partnerEdges) {
        if (visited.has(pe.a) && visited.has(pe.b)) {
          usedEdgeIds.add(pe.id);
        }
      }
      for (const pe of partnerEdges) {
        if (pe.a === hovered) {
          visited.add(pe.b);
          usedEdgeIds.add(pe.id);
        } else if (pe.b === hovered) {
          visited.add(pe.a);
          usedEdgeIds.add(pe.id);
        }
      }
    }

    if (visited.size <= 1 || usedEdgeIds.size === 0) return null;

    // Ultimate beneficiaries (persons) for "up" traversal:
    // highlight only PERSON nodes that have no further controllers within the up-cluster.
    const ultimatePersonIds = new Set<string>();
    for (const id of upVisited) {
      if (!isPerson(id)) continue;
      const nextUp = adjIn.get(id) || [];
      const hasHigherControllerInCluster = nextUp.some((a) =>
        upVisited.has(a.from) && CONTROL_ULTIMATE_BLOCKING_TYPES.has(normalizeType(a.edgeType))
      );
      if (!hasHigherControllerInCluster) ultimatePersonIds.add(id);
    }

    return { nodeIds: visited, edgeIds: usedEdgeIds, ultimatePersonIds };
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

    let base = hiddenNodeIds.size === 0 ? edges : edges.filter((e) => !hiddenNodeIds.has(e.source) && !hiddenNodeIds.has(e.target));

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

    // Levels (right graph): if multiple control/governance edges exist between the same pair,
    // keep only the strongest one (avoid drawing multiple parallel control edges).
    if (isLevelsLayout) {
      const CONTROLISH = new Set([
        'OWNS',
        'PARENT_OF',
        'SUBSIDIARY_OF',
        'INVESTS_IN',
        'CONTROLS',
        'MANAGES',
        'BOARD_MEMBER_OF',
        'EXECUTIVE_OF',
        'EMPLOYED_BY',
        'FOUNDED',
      ]);
      const strength = (t: string): number => {
        const u = String(t || '').toUpperCase().trim();
        // Lower is stronger
        if (u === 'OWNS' || u === 'PARENT_OF') return 10;
        if (u === 'SUBSIDIARY_OF') return 12;
        if (u === 'INVESTS_IN') return 20;
        if (u === 'CONTROLS' || u === 'MANAGES') return 30;
        if (u === 'BOARD_MEMBER_OF' || u === 'EXECUTIVE_OF') return 40;
        if (u === 'EMPLOYED_BY') return 50;
        if (u === 'FOUNDED') return 60;
        return 100;
      };
      const isRight = (id: string) => (paneByKey.get(id) || '') === 'right';

      const bestByPair = new Map<string, { id: string; rank: number }>();
      for (const e of base) {
        const s = String((e as any)?.source || '').trim();
        const t = String((e as any)?.target || '').trim();
        if (!s || !t || s === t) continue;
        if (!isRight(s) || !isRight(t)) continue;
        const et = edgeTypeOf(e);
        if (!CONTROLISH.has(et)) continue;
        const k = pairKey(s, t);
        const r = strength(et);
        const prev = bestByPair.get(k);
        if (!prev || r < prev.rank || (r === prev.rank && String(e.id || '').localeCompare(prev.id) < 0)) {
          bestByPair.set(k, { id: String(e.id || ''), rank: r });
        }
      }
      if (bestByPair.size > 0) {
        base = base.filter((e) => {
          const s = String((e as any)?.source || '').trim();
          const t = String((e as any)?.target || '').trim();
          if (!s || !t || s === t) return true;
          if (!isRight(s) || !isRight(t)) return true;
          const et = edgeTypeOf(e);
          if (!CONTROLISH.has(et)) return true;
          const k = pairKey(s, t);
          const best = bestByPair.get(k);
          if (!best) return true;
          return String(e.id || '') === best.id;
        });
      }
    }

    return base;
  }, [edges, hiddenNodeIds, nodes, isLevelsLayout]);

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
    if (!isLevelsLayout) return new Map<string, Set<string>>();
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

  // Derive edges styles for hover highlighting (do not mutate base edges)
  // Labels are hidden by default to reduce visual noise, shown only on hover
  const displayEdges = React.useMemo(() => {
    const DIM_OPACITY = 0.22;
    const FADE_OPACITY = 0.12;
    const riskId = String(riskShieldActiveNodeId || '').trim();
    const riskActive = !!riskId;

    const applyRiskShieldDimming = (arr: Edge[]) => {
      if (!riskActive) return arr;
      return (arr || []).map((e: any) => {
        const baseStyle: any = (e as any)?.style || {};
        const curOpacity = typeof baseStyle.opacity === 'number' ? baseStyle.opacity : DIM_OPACITY;
        // In shield mode, edges are not the primary semantic layer — keep ALL edges muted uniformly.
        const nextOpacity = Math.min(curOpacity, 0.06);
        const d0: any = (e as any)?.data || {};
        return {
          ...(e as any),
          // Keep labels suppressed; shield mode is meant to be quiet.
          label: undefined,
          data: { ...d0, showLabel: false },
          style: { ...baseStyle, opacity: nextOpacity },
        } as any;
      });
    };

    // Perf mode: do not derive per-edge styles/labels (avoids O(E) object cloning on hover),
    // just return filtered edges.
    if (perfMode && !riskActive) return baseVisibleEdges;
    // Risk Shield prevails over all hover/selection edge logic.
    if (riskActive) return applyRiskShieldDimming(baseVisibleEdges);

    // Levels Two-Pane aggregation:
    // - hide per-child edges by default (they exist for hover reveal)
    // - show per-child edges incident to hovered node
    // - hide the aggregated edge for the hovered node's bucket to reduce clutter
    let edgesForDisplay: Edge[] = baseVisibleEdges;
    if (isLevelsLayout) {
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
      const baseStyle = (e as any)?.data?.baseStyle || e.style || {};
      const edgeTypeRaw = (e as any)?.data?.edgeType || (e as any)?.data?.subkind || e.label;
      const edgeLabel =
        (e as any)?.data?.edgeCaption ||
        getEdgeCaptionShort(edgeTypeRaw) ||
        (edgeTypeRaw ? String(edgeTypeRaw) : '');
      return { baseStyle, edgeLabel };
    };

    // Helper to create edge with optional label
    const withLabel = (e: Edge, showLabel: boolean, style: any, raiseToFront: boolean) => {
      const d: any = (e as any)?.data || {};
      if (!showLabel) {
        return {
          ...e,
          label: undefined,
          data: { ...d, showLabel: false, raiseToFront: false },
          style,
        };
      }
      const { edgeLabel } = baseStyled(e);
      return {
        ...e,
        label: undefined,
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
    if (isLevelsLayout) {
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
            return applyRiskShieldDimming(applySelectedEdge(orderByRaised(out)));
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
          return applyRiskShieldDimming(applySelectedEdge(orderByRaised(out)));
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
        return applyRiskShieldDimming(applySelectedEdge(edgesForDisplay.map((e) => {
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
        return applyRiskShieldDimming(applySelectedEdge(edgesForDisplay.map((e) => {
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
      return applyRiskShieldDimming(applySelectedEdge(orderByRaised(out)));
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
      return applyRiskShieldDimming(applySelectedEdge(orderByRaised(out)));
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
      return applyRiskShieldDimming(applySelectedEdge(orderByRaised(out)));
    }

    // Selected node (when not hovering): highlight incident edges, show labels
    if (selectedNodeId) {
      const nodeId = selectedNodeId;
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
        return withLabel(e, false, { ...baseStyle, opacity: DIM_OPACITY }, false);
      });
      return applyRiskShieldDimming(applySelectedEdge(orderByRaised(out)));
    }

    // Default: dim all edges, hide labels
    return applyRiskShieldDimming(applySelectedEdge(edgesForDisplay));
  }, [baseVisibleEdges, hoveredEdgeId, hoveredGroupId, hoveredNodeId, nodes, normalizeEdgeType, selectedNodeId, perfMode, hoverHighlight, theme, getEdgeCaptionShort, isLevelsLayout, levelsAggKeysByNodeId, selectedEdgeId, selectedLevelsAggGroupId, selectedLevelsAggEdgeType, riskShieldActiveNodeId]);

  // Right-side temporary stacking toggle (Levels layout):
  // The layout will mark the controlling node with stackGroupKey/stackCount/stackExpanded.
  // Toggling updates that flag and triggers a relayout so the stack can expand/collapse.
  const toggleStackGroup = React.useCallback((stackKey: string, nextExpanded: boolean) => {
    const key = String(stackKey || '').trim();
    if (!key) return;
    const parseKey = (k: string) => {
      const parts = String(k || '').split(':');
      if (parts.length < 5) return null;
      if (parts[0] !== 'levels') return null;
      const mode = parts[1] === 'rightstack' ? 'rightstack' : (parts[1] === 'rightstackp' ? 'rightstackp' : null);
      if (!mode) return null;
      const pivotId = String(parts[2] || '').trim();
      return { mode, pivotId };
    };
    const parsed = parseKey(key);
    setNodes((prev) =>
      prev.map((n) => {
        const d: any = (n as any)?.data;
        if (!d || d.isGroup) return n;
        const k = String(d.stackGroupKey || '').trim();
        if (!k) return n;
        const cur = Boolean(d.stackExpanded);
        if (k === key) {
          if (cur === nextExpanded) return n;
          return { ...(n as any), data: { ...d, stackExpanded: nextExpanded } } as any;
        }
        // Only one expanded stack group per right node (same pivot).
        if (nextExpanded && parsed) {
          const other = parseKey(k);
          if (other && other.pivotId === parsed.pivotId && cur) {
            return { ...(n as any), data: { ...d, stackExpanded: false } } as any;
          }
        }
        return n;
      }),
    );
    if (layoutMode === 'levels') {
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

    if (layoutMode === 'levels') {
      invokeArrange();
    }
  }, [nodes, edges, setNodes, layoutMode, invokeArrange, normalizeEdgeType]);

  // Local relation filters (Levels layout): per-node toggles to show/hide neighbors by edge type.
  // When a node is clicked, we show icons for each edge type in levelsExpandEdgeTypes.
  // Clicking an icon toggles visibility of neighbors connected by that edge type for that specific node.
  const getDependentEdgeTypesForNode = React.useCallback((centerNodeId: string): string[] => {
    const centerId = String(centerNodeId || '').trim();
    if (!centerId) return [];
    const snapNodes: Node<HorizonNodeData>[] = (nodesStateRef.current || nodes) as any;
    const snapEdges: Edge[] = (edgesStateRef.current || edges) as any;
    const isGroup = (n: any) => Boolean((n as any)?.data?.isGroup);
    const allExpand = new Set((levelsExpandEdgeTypes || []).map((x) => String(x || '').toUpperCase().trim()).filter(Boolean));
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
  }, [nodes, edges, levelsExpandEdgeTypes, normalizeEdgeType, getEdgePriority]);

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

    if (layoutMode === 'levels') {
      invokeArrange();
    }
  }, [nodes, edges, setNodes, layoutMode, invokeArrange, normalizeEdgeType]);

  const displayNodes = React.useMemo(() => {
    // Perf mode: keep visuals light, but still allow graph-query highlight (low-cost style).
    const qset = graphQueryHighlightIds;
    const qActive = !!qset && qset.size > 0;
    if (perfMode && !qActive && !String(riskShieldActiveNodeId || '').trim()) return nodes;

    const owned = hoverHighlight?.nodeIds;
    const ultimatePersons: Set<string> | undefined = (hoverHighlight as any)?.ultimatePersonIds;
    const hovered = String(hoveredNodeId || '').trim();
    const hoverActive = !!hoverHighlight && !!hovered;
    const riskId = String(riskShieldActiveNodeId || '').trim();
    const riskActive = !!riskId;

    // Pre-compute query highlight styles (avoid creating objects in loop)
    const queryOutline = theme === 'dark' ? '2px solid rgba(248,113,113,0.75)' : '2px solid rgba(220,38,38,0.60)';
    const queryBoxShadow = theme === 'dark'
      ? '0 0 0 4px rgba(248,113,113,0.18), 0 10px 26px rgba(0,0,0,0.35)'
      : '0 0 0 4px rgba(220,38,38,0.14), 0 10px 26px rgba(0,0,0,0.18)';

    return nodes.map((n) => {
      const id = String(n.id || '').trim();
      const isGroup = Boolean((n.data as any)?.isGroup);

      const isOwnedNode = !!owned && id && owned.has(id);
      const isHovered = id === hovered;
      const isQuery = qActive && !!qset && id && qset.has(id);
      const isRiskNode = riskActive && !isGroup && id && id === riskId;

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

      const wantQuery = Boolean(qActive && isQuery && !isGroup);
      if (qActive && !wantQuery) {
        // De-emphasize others (original logic: min(currentOpacity, 0.55), groups even more muted)
        opacity = isGroup ? 0.12 : Math.min(opacity, 0.55);
      }
      if (wantQuery) {
        opacity = 1;
        zIndex = Math.max(zIndex, 40);
      }

      // Risk Shield mode: dim everything except the active node.
      if (riskActive) {
        if (isRiskNode) {
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

      // Query highlight style props: add/remove deterministically
      if (wantQuery) {
        const needOutline = prevStyle.outline !== queryOutline;
        const needOffset = prevStyle.outlineOffset !== 2;
        const needShadow = prevStyle.boxShadow !== queryBoxShadow;
        if (needOutline || needOffset || needShadow) {
          if (!styleChanged) {
            style = { ...prevStyle };
            styleChanged = true;
          }
          style.outline = queryOutline;
          style.outlineOffset = 2;
          style.boxShadow = queryBoxShadow;
        }
      } else {
        // If this node had query styles from a previous run, clear them.
        if (prevStyle.outline || typeof prevStyle.outlineOffset !== 'undefined' || prevStyle.boxShadow) {
          if (!styleChanged) {
            style = { ...prevStyle };
            styleChanged = true;
          }
          delete style.outline;
          delete style.outlineOffset;
          delete style.boxShadow;
        }
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
      const isLevelsRight = !isGroup && layoutMode === 'levels' && String(d0?.levelsPane || '') === 'right';

      // Stack group (expanded stack container)
      const isStackGroup = isGroup && Boolean(d0?.isStackGroup);
      const stackGroupKey = isStackGroup ? String(d0?.stackGroupKey || '').trim() : '';

      // Only clone data when needed (perf).
      let nextData: any = d0;
      let changed = false;
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

      // Risk Shield: attach toggle callback + open flag only for real fragment nodes (not groups, not stacks).
      const canRisk = !isGroup && !stackKey;
      const wantRiskOpen = Boolean(canRisk && isRiskNode);
      if (canRisk) {
        if (nextData === d0) {
          nextData = { ...d0 };
          changed = true;
        }
        if (nextData.onToggleRiskShield !== toggleRiskShield) {
          nextData.onToggleRiskShield = toggleRiskShield;
          changed = true;
        }
        if (Boolean(nextData.riskShieldOpen) !== wantRiskOpen) {
          nextData.riskShieldOpen = wantRiskOpen;
          changed = true;
        }
      } else if (typeof nextData?.onToggleRiskShield === 'function' || typeof nextData?.riskShieldOpen !== 'undefined') {
        nextData = { ...d0 };
        delete nextData.onToggleRiskShield;
        delete nextData.riskShieldOpen;
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

      // Local relation filters (all non-group nodes in Levels mode)
      const isLevelsMode = layoutMode === 'levels';
      if (!isGroup && isLevelsMode && levelsExpandEdgeTypes.length > 0) {
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
      
      if (changed || styleChanged || zIndexChanged) {
        return { ...n, data: changed ? nextData : d0, zIndex, style } as any;
      }
      // Return original node if nothing changed (preserves reference for React.memo)
      return n;
    });
  }, [perfMode, nodes, hoverHighlight, hoveredNodeId, graphQueryHighlightIds, theme, hubNodeIds, toggleStackGroup, layoutMode, openRightFilters, toggleRightFilter, openTraceModalForNode, undoLastTrace, canUndoTrace, openLocalFilters, toggleLocalFilter, levelsExpandEdgeTypes, riskShieldActiveNodeId, toggleRiskShield]);

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

      // Применяем layout в зависимости от режима раскладки
      const fragmentNodes = nodes.filter((n) => !(n.data as any)?.isGroup);
      
      let arrangedFragments: Node<HorizonNodeData>[];
      let withGroups: Node<HorizonNodeData>[];
      
      if (isFreeformLayout) {
        // Freeform layout: используем текущую логику с ELK и группами
        // CRITICAL: Restore expanded flag from expandedNodesRef before relayout.
        const fragmentNodesWithExpanded = fragmentNodes.map((n) => {
          const fid = String((n.data as any)?.fragmentId || n.id || '').trim();
          if (!fid) return n;
          if (expandedNodesRef.current.has(fid) && !(n.data as any)?.expanded) {
            return { ...n, data: { ...(n.data || {}), expanded: true } as any };
          }
          return n;
        });
        arrangedFragments = await layoutFullGraph(fragmentNodesWithExpanded, edges, {
        direction: 'DOWN',
        nodeWidth: ELK_NODE_WIDTH,
        nodeHeight: ELK_NODE_HEIGHT,
        nodeSpacing: ELK_NODE_SPACING,
        layerSpacing: ELK_LAYER_SPACING,
        maxChildRows: ELK_MAX_CHILD_ROWS,
      });

      // Сохраняем onLoadRelated callback для каждой ноды
        const finalFragments = arrangedFragments.map((node) => {
          const fid = String((node.data as any)?.fragmentId || node.id || '').trim();
          const shouldBeExpanded = fid && expandedNodesRef.current.has(fid);
          return {
            ...node,
            data: {
              ...node.data,
              expanded: shouldBeExpanded ? true : (node.data as any)?.expanded || false,
              onLoadRelated: invokeLoadRelated,
              onCollapse: invokeCollapse,
              onScopeScout: invokeScopeScout,
            },
          };
        });

        // Пересобираем группы (freeform использует группировку)
        withGroups = rebuildGroupContainers(finalFragments, {
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
        });
        
        // Recalculate edge handles for Freeform layout
        const updatedEdges = recalculateFreeformHandles(edges, withGroups);
        setEdges(updatedEdges);
      } else if (isLinearLayout) {
        // Linear layout: 3-row layout с 4 секциями
        // CRITICAL: Restore expanded flag from expandedNodesRef before relayout.
        const fragmentNodesWithExpanded = fragmentNodes.map((n) => {
          const fid = String((n.data as any)?.fragmentId || n.id || '').trim();
          if (!fid) return n;
          if (expandedNodesRef.current.has(fid) && !(n.data as any)?.expanded) {
            return { ...n, data: { ...(n.data || {}), expanded: true } as any };
          }
          return n;
        });
        arrangedFragments = await layoutLinearFullGraph(fragmentNodesWithExpanded, edges, {
          direction: 'DOWN',
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
        });

        const finalFragments = arrangedFragments.map((node) => {
          const fid = String((node.data as any)?.fragmentId || node.id || '').trim();
          const shouldBeExpanded = fid && expandedNodesRef.current.has(fid);
          return {
            ...node,
            data: {
              ...node.data,
              expanded: shouldBeExpanded ? true : (node.data as any)?.expanded || false,
              onLoadRelated: invokeLoadRelated,
              onCollapse: invokeCollapse,
              onScopeScout: invokeScopeScout,
            },
          };
        });

        // Linear layout уже создает группы внутри layoutLinearFullGraph
        withGroups = finalFragments;
        
        // Recalculate edge handles for Linear layout
        const updatedEdges = recalculateLinearHandles(edges, finalFragments);
        setEdges(updatedEdges);
      } else if (isLevelsLayout) {
        // Levels (Hierarchical): contract-driven Two-Pane layout.
        // IMPORTANT: pass the current nodes (including existing groups) so pagination can be preserved.
        const nodesNoRightStacks = (nodes as any[]).filter((n) => {
          const id = String((n as any)?.id || '').trim();
          const d: any = (n as any)?.data;
          if (id.startsWith('stack:group:')) return false;
          if (Boolean(d?.isStackGroup)) return false;
          return true;
        });
        // CRITICAL: Restore expanded flag from expandedNodesRef before relayout.
        // This ensures nodes that were expanded before filter toggles remain expanded after relayout.
        const nodesWithExpandedRestored = nodesNoRightStacks.map((n) => {
          if ((n.data as any)?.isGroup) return n;
          const fid = String((n.data as any)?.fragmentId || n.id || '').trim();
          if (!fid) return n;
          if (expandedNodesRef.current.has(fid) && !(n.data as any)?.expanded) {
            return { ...n, data: { ...(n.data || {}), expanded: true } as any };
          }
          return n;
        });
        arrangedFragments = await layoutLevelsTwoPane(nodesWithExpandedRestored as any, edges as any, {
          direction: 'DOWN',
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
          collapsedSubcategories,
          onToggleSubcategoryCollapse: toggleSubcategoryCollapse,
          compactLeftPane: true,
          rightDockMode: true,
          rightDockStateByNodeId,
          onToggleRightDock: toggleRightDock,
        });

        const finalFragments = arrangedFragments.map((node) => {
          if ((node.data as any)?.isGroup) return node as any;
          const fid = String((node.data as any)?.fragmentId || node.id || '').trim();
          // Ensure expanded flag is preserved from expandedNodesRef (critical for collapse button to work).
          const shouldBeExpanded = fid && expandedNodesRef.current.has(fid);
          return {
            ...node,
            data: {
              ...node.data,
              expanded: shouldBeExpanded ? true : (node.data as any)?.expanded || false,
              onLoadRelated: invokeLoadRelated,
              onCollapse: invokeCollapse,
              onScopeScout: invokeScopeScout,
            },
          };
        });

        withGroups = finalFragments;

        // Levels Two-Pane: attach edges to left/right handles based on columns + group membership.
        const updatedEdges = recalculateLevelsTwoPaneHandles(edges, finalFragments as any);
        setEdges(updatedEdges);
      } else {
        // Fallback: используем freeform
        // CRITICAL: Restore expanded flag from expandedNodesRef before relayout.
        const fragmentNodesWithExpanded = fragmentNodes.map((n) => {
          const fid = String((n.data as any)?.fragmentId || n.id || '').trim();
          if (!fid) return n;
          if (expandedNodesRef.current.has(fid) && !(n.data as any)?.expanded) {
            return { ...n, data: { ...(n.data || {}), expanded: true } as any };
          }
          return n;
        });
        arrangedFragments = await layoutFullGraph(fragmentNodesWithExpanded, edges, {
          direction: 'DOWN',
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
        });

        const finalFragments = arrangedFragments.map((node) => {
          const fid = String((node.data as any)?.fragmentId || node.id || '').trim();
          const shouldBeExpanded = fid && expandedNodesRef.current.has(fid);
          return {
            ...node,
            data: {
              ...node.data,
              expanded: shouldBeExpanded ? true : (node.data as any)?.expanded || false,
              onLoadRelated: invokeLoadRelated,
              onCollapse: invokeCollapse,
              onScopeScout: invokeScopeScout,
            },
          };
        });

        withGroups = rebuildGroupContainers(finalFragments, {
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
        });
      }
      // Add onGroupPageChange callback to group nodes
      const withCallbacks = withGroups.map((node) => {
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
      // - If a stack-preview chip (or expand/collapse flow) requested focus, center on that node.
      // - Otherwise DO NOT auto-fit the entire graph. Expanding a node should keep the user's zoom/context.
      const focusId = String(pendingFocusNodeIdRef.current || '').trim();
      pendingFocusNodeIdRef.current = null;
      if (focusId) {
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
  }, [nodes, edges, arranging, handleLoadRelatedFragments, handleGroupPageChange, setNodes, setEdges, recalculateLinearHandles, recalculateFreeformHandles, isLinearLayout, isFreeformLayout, isLevelsLayout, collapsedSubcategories, toggleSubcategoryCollapse, rightDockStateByNodeId, toggleRightDock]);

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

        // Always compute layout on fragment nodes (never on group nodes).
        const current = (nodesStateRef.current || nodes) as any[];
        const fragmentNodes: Node<HorizonNodeData>[] = current.filter((n) => !(n?.data as any)?.isGroup);

        // Edge render style by mode (linear => curved)
        const edgeRenderType = 'default';
        let nextEdges: Edge[] = (edges || []).map((e: any) => ({ ...e, type: edgeRenderType }));

        let nextNodes: Node<HorizonNodeData>[];
        if (isLinearLayout) {
          // Linear layout builds its own section containers.
          const laidOut = await layoutLinearFullGraph(fragmentNodes, nextEdges, {
            direction: 'DOWN',
            nodeWidth: ELK_NODE_WIDTH,
            nodeHeight: ELK_NODE_HEIGHT,
            nodeSpacing: ELK_NODE_SPACING,
            layerSpacing: ELK_LAYER_SPACING,
            maxChildRows: ELK_MAX_CHILD_ROWS,
          });
          nextNodes = laidOut as any;
          
          // Recalculate edge handles for Linear layout based on node positions
          nextEdges = recalculateLinearHandles(nextEdges, nextNodes);
        } else if (isLevelsLayout) {
          const fragmentNodesNoRightStacks = (fragmentNodes as any[]).filter((n) => {
            const id = String((n as any)?.id || '').trim();
            const d: any = (n as any)?.data;
            if (id.startsWith('stack:group:')) return false;
            if (Boolean(d?.isStackGroup)) return false;
            return true;
          });
          const laidOut = await layoutLevelsTwoPane(fragmentNodesNoRightStacks as any, nextEdges as any, {
            direction: 'DOWN',
            nodeWidth: ELK_NODE_WIDTH,
            nodeHeight: ELK_NODE_HEIGHT,
            nodeSpacing: ELK_NODE_SPACING,
            layerSpacing: ELK_LAYER_SPACING,
            maxChildRows: ELK_MAX_CHILD_ROWS,
            collapsedSubcategories,
            onToggleSubcategoryCollapse: toggleSubcategoryCollapse,
            compactLeftPane: true,
            rightDockMode: true,
            rightDockStateByNodeId,
            onToggleRightDock: toggleRightDock,
          });
          nextNodes = laidOut as any;

          // Levels Two-Pane: attach edges to left/right handles based on columns + group membership.
          nextEdges = recalculateLevelsTwoPaneHandles(nextEdges, nextNodes);
        } else {
          // Freeform layout: run ELK on fragments then rebuild semantic-group containers.
          const arrangedFragments = await layoutFullGraph(fragmentNodes, nextEdges, {
            direction: 'DOWN',
            nodeWidth: ELK_NODE_WIDTH,
            nodeHeight: ELK_NODE_HEIGHT,
            nodeSpacing: ELK_NODE_SPACING,
            layerSpacing: ELK_LAYER_SPACING,
            maxChildRows: ELK_MAX_CHILD_ROWS,
          });

          const withCallbacks = arrangedFragments.map((node) => ({
            ...node,
            data: { ...(node.data as any), onLoadRelated: invokeLoadRelated, onCollapse: invokeCollapse, onScopeScout: invokeScopeScout },
          }));

          const withGroups = rebuildGroupContainers(withCallbacks, {
            nodeWidth: ELK_NODE_WIDTH,
            nodeHeight: ELK_NODE_HEIGHT,
          });
          nextNodes = withGroups as any;
          
          // Recalculate edge handles for Freeform layout based on semantic groups
          nextEdges = recalculateFreeformHandles(nextEdges, nextNodes);

          // Freeform-only: reset grid bookkeeping based on current root nodes.
          try {
            const roots = withCallbacks
              .filter((n) => !(n.data as any)?.isGroup)
              .filter((n) => typeof (n.data as any)?.depth === 'number' && (n.data as any).depth === 0)
              .sort((a, b) => (a.position?.x || 0) - (b.position?.x || 0));

            const rootId = roots[0]?.id || null;
            rootNodeIdRef.current = rootId;
            expandedNodesRef.current = new Set(); // keep conservative; will be rebuilt by future expansions

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
        const finalNodes = (nextNodes as any[]).map((n) => {
          if ((n.data as any)?.isGroup) {
            return { ...n, data: { ...n.data, onGroupPageChange: handleGroupPageChange } };
          }
          // Ensure onLoadRelated is present on fragment nodes
          return { ...n, data: { ...n.data, onLoadRelated: invokeLoadRelated, onCollapse: invokeCollapse, onScopeScout: invokeScopeScout } };
        });

        setEdges(nextEdges as any);
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

  const openCommandPalette = React.useCallback(() => {
    if (!lensId) return;
    // Single palette at a time.
    setAskGraphOpen(false);
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

  // --- Scout from query (no manual type selection) ---
  // Uses existing WS pipeline + new /scope-scout-query endpoint.
  const rtGraphNodes = useHorizonRealtimeStore((s) => s.nodes);
  const pendingScoutRef = React.useRef<
    Map<string, { placeholderId: string; query: string; canonicalPath?: string; createdAt: number }>
  >(new Map());

  const genCorrelationId = React.useCallback((): string => {
    try {
      const c: any = (globalThis as any)?.crypto;
      if (c?.randomUUID) return String(c.randomUUID());
    } catch {}
    // Fallback: good-enough unique token for client-side correlation
    return `${Date.now()}-${Math.random().toString(16).slice(2)}`;
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
        canonicalPath.split('/').filter(Boolean).slice(-1)[0] ||
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

  // WS-driven placeholder reconciliation: once a scouted canonicalPath is materialized, swap in the real node.
  React.useEffect(() => {
    if (!lensId) return;
    const pending = pendingScoutRef.current;
    if (!pending || pending.size === 0) return;

    for (const [cid, st] of pending.entries()) {
      const cp = String(st.canonicalPath || '').trim();
      if (!cp) continue;
      const fid = findFragmentIdByCanonicalPath(cp);
      if (!fid) continue;
      pending.delete(cid);
      void materializeScoutedNode({ placeholderId: st.placeholderId, fragmentId: fid, canonicalPath: cp });
    }
  }, [lensId, rtGraphNodes, findFragmentIdByCanonicalPath, materializeScoutedNode]);

  const scoutForQuery = React.useCallback(
    async (query: string) => {
      const q = String(query || '').trim();
      if (!lensId || q.length < 2) return;

      const correlationId = genCorrelationId();
      const placeholderId = `scout:${correlationId}`;

      // Add placeholder node immediately.
      const placement = computeNewRootPosition(placeholderId);
      const position = placement.position;
      const ph: Node<HorizonNodeData> = {
        id: placeholderId,
        type: 'horizonFragment',
        position,
        data: {
          id: placeholderId,
          fragmentId: placeholderId,
          canonicalPath: '',
          label: `Scouting: ${q}`,
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

      pendingScoutRef.current.set(correlationId, { placeholderId, query: q, createdAt: Date.now() });

      // Close palette quickly (user stays on graph).
      closeAddNode();

      try {
        // Special-case DOMAIN queries: "Scout it" should run strategic projection (decision layer), not generic scope scout.
        // We detect a domain intent best-effort via the overlay preview endpoint (no writes).
        try {
          const preview = await api.previewDomainOverlay(lensId, { query: q });
          const domainCp = String((preview as any)?.intent?.domains?.[0]?.canonicalPath || '').trim();
          if (domainCp && domainCp.startsWith('/domain/')) {
            // Optimistic scope preview update (pin state and UI consistency).
            setLensScopePaths((prev) => (prev.includes(domainCp) ? prev : [...prev, domainCp]));

            // Update placeholder node with canonicalPath to enable WS reconciliation.
            setNodes((prev) =>
              (prev || []).map((n) => {
                if (String((n as any)?.id || '').trim() !== placeholderId) return n;
                const d0: any = (n as any)?.data || {};
                const d = { ...d0, canonicalPath: domainCp, label: d0?.label || `Strategic projection: ${q}` };
                return { ...(n as any), data: d } as any;
              }),
            );
            const st = pendingScoutRef.current.get(correlationId);
            if (st) pendingScoutRef.current.set(correlationId, { ...st, canonicalPath: domainCp });

            // Trigger strategic projection job(s) for this domain.
            const sessionId = (window as any).__horizonSessionId || correlationId;
            (window as any).__horizonSessionId = sessionId;
            await api.triggerHorizonPhase2StrategicOverlay(lensId, { domainPaths: [domainCp] }, { correlationId, sessionId });
            return;
          }
        } catch {
          // ignore: fall back to normal scope-scout-query
        }

        const res = await api.scopeScoutHorizonLensQuery(lensId, { query: q }, { correlationId });
        const cp = String(res?.targetCanonicalPath || '').trim();
        if (cp) {
          // Optimistic scope preview update (pin state and UI consistency).
          setLensScopePaths((prev) => (prev.includes(cp) ? prev : [...prev, cp]));

          // Update placeholder node with canonicalPath to enable WS reconciliation.
          setNodes((prev) =>
            (prev || []).map((n) => {
              if (String((n as any)?.id || '').trim() !== placeholderId) return n;
              const d0: any = (n as any)?.data || {};
              const d = { ...d0, canonicalPath: cp, label: d0?.label || `Scouting: ${q}` };
              return { ...(n as any), data: d } as any;
            }),
          );
          const st = pendingScoutRef.current.get(correlationId);
          if (st) pendingScoutRef.current.set(correlationId, { ...st, canonicalPath: cp });
        }
      } catch (e) {
        console.warn('[Cockpit] scope-scout-query failed', e);
        // Mark placeholder as failed (keep visible so user understands something happened).
        setNodes((prev) =>
          (prev || []).map((n) => {
            if (String((n as any)?.id || '').trim() !== placeholderId) return n;
            const d0: any = (n as any)?.data || {};
            return { ...(n as any), data: { ...d0, isScopeScouting: false, status: 'failed', label: `Scout failed: ${q}` } } as any;
          }),
        );
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
        canonicalPath.split('/').pop() ||
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
    const vt = getHorizonNodeVisualType(data.type, (data as any)?.canonicalPath, (data as any)?.label);
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
    if (import.meta.env.DEV) console.log('[Cockpit] Node clicked:', node.id, node.data);
    ignorePaneClicksUntilRef.current = Date.now() + 250;
    event.stopPropagation();
    event.preventDefault();

    // Risk Shield: clicking any other node counts as "outside" and closes it.
    const rid = String(riskShieldActiveNodeId || '').trim();
    const nid = String(node?.id || '').trim();
    if (rid && nid && rid !== nid) {
      setRiskShieldActiveNodeId(null);
    }

    // Selecting a node should clear any selected edge highlight.
    setSelectedEdgeId(null);
    onEdgeSelect?.(null, null);

    // Levels Two-Pane: clicking an aggregated left group toggles "real edges" preview.
    // Clicking any other node clears the preview (returns to default aggregated view).
    if (layoutMode === 'levels' && Boolean((node.data as any)?.isGroup) && String(node.id || '').trim().startsWith('levels:left:')) {
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

    if (layoutMode === 'levels') {
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
  }, [onNodeSelect, onEdgeSelect, edges, nodes, layoutMode, riskShieldActiveNodeId]);

  // Handle edge click
  const handleEdgeClick = useCallback((event: React.MouseEvent, edge: Edge) => {
    if (import.meta.env.DEV) console.log('[Cockpit] Edge clicked:', edge.id, edge.data);
    ignorePaneClicksUntilRef.current = Date.now() + 250;
    event.stopPropagation();
    event.preventDefault();

    // Risk Shield: edge click is "outside" => close.
    if (riskShieldActiveNodeId) setRiskShieldActiveNodeId(null);

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
    if (layoutMode === 'levels' && isLevelsAgg && (isParentChildAggEdge || isGroupPairAggEdge) && isLevelsLeftGroup) {
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
    if (!(layoutMode === 'levels' && isLevelsAgg && (isParentChildAggEdge || isGroupPairAggEdge) && isLevelsLeftGroup)) {
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
  }, [onEdgeSelect, nodes, layoutMode, normalizeEdgeType, selectedLevelsAggGroupId, selectedLevelsAggEdgeType, riskShieldActiveNodeId]);

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
    if (riskShieldActiveNodeId) setRiskShieldActiveNodeId(null);
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
  }, [onNodeSelect, onEdgeSelect, graphQueryHighlightIds, riskShieldActiveNodeId]);

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
      // Пересборка групп после перетаскивания (freeform layout)
      let relayouted: Node<HorizonNodeData>[];
      
      if (isFreeformLayout) {
        // Freeform layout: используем текущую логику с группами
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
        // Fallback: используем freeform
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
      }
      // Add onGroupPageChange callback to groups
      const withCallbacks = relayouted.map((n) => {
        if ((n.data as any)?.isGroup) {
          return { ...n, data: { ...n.data, onGroupPageChange: handleGroupPageChange } };
        }
        return n;
      });
      setNodes(withCallbacks);
    } catch (err) {
      console.warn('[Cockpit] Failed to relayout groups after drag stop', err);
    }
  }, [nodes, setNodes, handleGroupPageChange]);

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
        const correlationId = genCorrelationId();
        const sessionId = (window as any).__horizonSessionId || correlationId;
        (window as any).__horizonSessionId = sessionId;
        await api.triggerHorizonPhase2StrategicOverlay(lensId, { domainPaths: [cp] }, { correlationId, sessionId });
        ok = true;
      } else {
        if (!onScopeScout) return;
        await onScopeScout({ fragmentId, canonicalPath: scopeScoutModal.canonicalPath, maxRecursionDepth: scopeScoutDepth });
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
  }, [lensId, onScopeScout, scopeScoutModal, scopeScoutDepth, setNodes, genCorrelationId]);

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
      if (askGraphOpen || addNodeOpen || domainOverlayOpen) return;
      if (graphQueryHighlightIds && graphQueryHighlightIds.size > 0) {
        e.preventDefault();
        clearGraphQueryHighlight();
      }
    };
    window.addEventListener('keydown', onKeyDown, true);
    return () => window.removeEventListener('keydown', onKeyDown, true);
  }, [askGraphOpen, addNodeOpen, domainOverlayOpen, graphQueryHighlightIds, clearGraphQueryHighlight]);

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

  const closeDomainOverlay = React.useCallback(() => {
    setDomainOverlayOpen(false);
    setDomainOverlayQuery('');
    setDomainOverlayPreview(null);
    setDomainOverlayLoading(false);
    setDomainOverlayExecuting(false);
    setDomainOverlayError(null);
  }, []);

  const submitDomainOverlayPreview = React.useCallback(async () => {
    const q = String(domainOverlayQuery || '').trim();
    if (!lensId || !q || domainOverlayLoading || domainOverlayExecuting) return;
    setDomainOverlayLoading(true);
    setDomainOverlayError(null);
    try {
      const res = await api.previewDomainOverlay(lensId, { query: q });
      const intent = (res as any)?.intent || null;
      setDomainOverlayPreview({ intent, existingDomains: (res as any)?.existingDomains || [] });
      if (!intent?.domains || intent.domains.length === 0) {
        setDomainOverlayError('No domain detected for this query.');
      }
    } catch (err: any) {
      console.error('[LensCanvas] previewDomainOverlay error:', err);
      setDomainOverlayError('Failed to interpret the query.');
    } finally {
      setDomainOverlayLoading(false);
    }
  }, [domainOverlayQuery, lensId, domainOverlayLoading, domainOverlayExecuting]);

  const executeDomainOverlay = React.useCallback(async () => {
    const q = String(domainOverlayQuery || '').trim();
    if (!lensId || !q || domainOverlayExecuting) return;
    const domainPath = String(domainOverlayPreview?.intent?.domains?.[0]?.canonicalPath || '').trim();
    if (!domainPath) {
      setDomainOverlayError('Check the domain first.');
      return;
    }
    const correlationId = genCorrelationId();
    setDomainOverlayExecuting(true);
    setDomainOverlayError(null);
    try {
      // Strategic projection triggers via dedicated endpoint (jobs are created per domain).
      await api.triggerHorizonPhase2StrategicOverlay(lensId, { domainPaths: [domainPath] }, { correlationId });
      closeDomainOverlay();
    } catch (err: any) {
      console.error('[LensCanvas] executeDomainOverlay error:', err);
      setDomainOverlayError('Failed to start overlay job.');
    } finally {
      setDomainOverlayExecuting(false);
    }
  }, [domainOverlayQuery, lensId, domainOverlayExecuting, domainOverlayPreview, genCorrelationId, closeDomainOverlay]);

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
  const IconButton = ({ onClick, active, disabled, title, icon }: any) => (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={title}
        style={{
        background: active ? (theme === 'dark' ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.06)') : 'transparent',
        border: 'none',
        borderRadius: '6px',
        padding: '6px',
        cursor: disabled ? 'not-allowed' : 'pointer',
        color: active ? 'var(--text-primary)' : 'var(--text-secondary)',
        opacity: disabled ? 0.4 : 1,
          display: 'flex',
          alignItems: 'center',
        justifyContent: 'center',
        transition: 'all 0.2s ease',
      }}
    >
      {icon}
    </button>
  );

  const Icons = {
    Refresh: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" /><path d="M3 3v5h5" /><path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16" /><path d="M16 21h5v-5" /></svg>,
    Load: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round"><path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z" /><path d="M12 11v6" /><path d="M9 14l3 3 3-3" /></svg>,
    Search: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" /></svg>,
    Sliders: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><line x1="4" y1="21" x2="4" y2="14" /><line x1="4" y1="10" x2="4" y2="3" /><line x1="12" y1="21" x2="12" y2="12" /><line x1="12" y1="8" x2="12" y2="3" /><line x1="20" y1="21" x2="20" y2="16" /><line x1="20" y1="12" x2="20" y2="3" /><line x1="1" y1="14" x2="7" y2="14" /><line x1="9" y1="8" x2="15" y2="8" /><line x1="17" y1="16" x2="23" y2="16" /></svg>,
    Dots: <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><circle cx="5" cy="12" r="1.8" /><circle cx="12" cy="12" r="1.8" /><circle cx="19" cy="12" r="1.8" /></svg>,
    Plus: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 5v14" /><path d="M5 12h14" /></svg>,
    X: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M18 6 6 18" /><path d="M6 6l12 12" /></svg>,
    Magic: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z"/><path d="M5 3v4"/><path d="M9 3v4"/><path d="M3 7h4"/><path d="M3 3h4"/></svg>,
    Download: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>,
    Zap: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>,
    Map: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="1 6 1 22 8 18 16 22 23 18 23 2 16 6 8 2 1 6"/><line x1="8" y1="2" x2="8" y2="18"/><line x1="16" y1="6" x2="16" y2="22"/></svg>,
    Grid: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>,
    Layout: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><line x1="9" y1="3" x2="9" y2="21"/><line x1="15" y1="3" x2="15" y2="21"/></svg>,
    LayoutFreeform: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><circle cx="18" cy="5" r="2.4"/><circle cx="6" cy="12" r="2.4"/><circle cx="18" cy="19" r="2.4"/><path d="M8.1 11.2 15.8 6.2"/><path d="M8.1 12.8 15.8 17.8"/></svg>,
    LayoutLinear: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="4" width="6" height="16" rx="2"/><rect x="15" y="4" width="6" height="16" rx="2"/><path d="M10.5 12h3"/></svg>,
    LayoutLevels: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M4 7h16"/><path d="M4 12h16"/><path d="M4 17h16"/><circle cx="8" cy="7" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="16" cy="17" r="1.6"/></svg>,
    ChevronDown: <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="6 9 12 15 18 9"/></svg>,
    Info: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><path d="M12 16v-4" /><path d="M12 8h.01" /></svg>,
  };

  const layoutModeLabel = React.useCallback((m: 'freeform' | 'linear' | 'levels'): string => {
    switch (m) {
      case 'freeform':
        return 'Freeform';
      case 'linear':
        return 'Linear';
      case 'levels':
        return 'Hierarchical';
      default:
        return 'Layout';
    }
  }, []);

  const layoutModeIcon = React.useCallback((m: 'freeform' | 'linear' | 'levels') => {
    switch (m) {
      case 'freeform':
        return Icons.LayoutFreeform;
      case 'linear':
        return Icons.LayoutLinear;
      case 'levels':
        return Icons.LayoutLevels;
      default:
        return Icons.Layout;
    }
  }, []);

  const hoverModeLabel = React.useCallback((m: HoverHighlightMode): string => {
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
      default:
        return 'Highlight';
    }
  }, []);

  const setHoverMode = React.useCallback((m: HoverHighlightMode) => {
    setHoverHighlightMode(m);
    setVisualPreset('custom');
  }, []);

  const toolbarContent = (
        <div style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
      <IconButton onClick={handleLoadGraph} disabled={loading || !lensId} title="Load Graph" icon={Icons.Load} />
      <IconButton
        onClick={() => {
          toggleCommandPalette();
        }}
        disabled={!lensId}
        active={addNodeOpen}
        title={addNodeOpen ? 'Close search (Ctrl+K)' : 'Search / add (Ctrl+K)'}
        icon={Icons.Search}
      />
      <IconButton onClick={handleArrange} disabled={arranging || nodes.length === 0} title="Arrange Graph" icon={Icons.Magic} />
      <IconButton
        onClick={() => onToggleInspector?.()}
        disabled={!onToggleInspector}
        active={Boolean(inspectorActive)}
        title={inspectorActive ? 'Close inspector' : 'Open inspector'}
        icon={Icons.Info}
      />
      <IconButton onClick={() => setVisualSettingsOpen(true)} title="Visual settings" icon={Icons.Sliders} />

      {/* Layout mode (quick control) */}
      <div ref={layoutMenuRef} style={{ position: 'relative', display: 'inline-block' }}>
        <button
          type="button"
          onClick={() => {
            setShowLayoutMenu((v) => !v);
            setShowHoverModeMenu(false);
            setShowToolbarMenu(false);
          }}
          title={`Layout: ${layoutModeLabel(layoutMode)}`}
          aria-label="Layout mode"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '6px 8px',
            borderRadius: 10,
            border: '1px solid var(--border-subtle)',
            background: theme === 'dark' ? 'rgba(0,0,0,0.18)' : 'rgba(255,255,255,0.40)',
            color: 'var(--text-heading)',
            cursor: 'pointer',
            fontSize: 12,
            fontWeight: 750,
            lineHeight: 1,
          }}
        >
          <span style={{ color: 'var(--text-secondary-strong)', display: 'inline-flex' }}>
            {layoutModeIcon(layoutMode)}
          </span>
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
                { key: 'levels', label: 'Hierarchical' },
              ] as Array<{ key: 'freeform' | 'linear' | 'levels'; label: string }>
            ).map((opt) => {
              const active = layoutMode === opt.key;
              return (
                <button
                  key={opt.key}
                  type="button"
                  onClick={() => {
                    setLayoutMode(opt.key);
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
                      {layoutModeIcon(opt.key)}
                    </span>
                    <span style={{ fontSize: 12, fontWeight: 800 }}>{opt.label}</span>
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>{active ? '✓' : ''}</div>
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Highlight mode (quick control) */}
      <div ref={hoverModeMenuRef} style={{ position: 'relative', display: 'inline-block' }}>
        <button
          type="button"
          onClick={() => {
            setShowHoverModeMenu((v) => !v);
            setShowLayoutMenu(false);
            setShowToolbarMenu(false);
          }}
          title="Highlight mode"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
              padding: '6px 8px',
            borderRadius: 10,
            border: '1px solid var(--border-subtle)',
            background: theme === 'dark' ? 'rgba(0,0,0,0.18)' : 'rgba(255,255,255,0.40)',
            color: 'var(--text-heading)',
              cursor: 'pointer',
            fontSize: 12,
            fontWeight: 750,
            lineHeight: 1,
          }}
        >
          <span style={{ color: 'var(--text-secondary-strong)', fontWeight: 800 }}>Highlight:</span>
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
            {(
              [
                { key: 'off', label: 'Off', hint: 'No emphasis' },
                { key: 'control', label: 'Control', hint: 'Equity ownership + control chains (dashed) + governance overlay' },
                { key: 'expansion', label: 'Expansion', hint: 'Control chains + partners (1-hop)' },
                { key: 'production', label: 'Produces', hint: 'Direct producers/outputs' },
                { key: 'supplies', label: 'Supplies', hint: 'Direct suppliers/inputs' },
                { key: 'competition', label: 'Competes', hint: 'Direct competition/conflict' },
              ] as Array<{ key: HoverHighlightMode; label: string; hint: string }>
            ).map((opt) => {
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

      {/* Levels: expansion filter (what edge types are fetched on expand) */}
      {isLevelsLayout && (
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
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              padding: '6px 8px',
              borderRadius: 10,
              border: expandModeGlow ? '1px solid rgba(245, 158, 11, 0.6)' : '1px solid var(--border-subtle)',
              background: theme === 'dark' ? 'rgba(0,0,0,0.18)' : 'rgba(255,255,255,0.40)',
              color: 'var(--text-heading)',
              cursor: 'pointer',
              fontSize: 12,
              fontWeight: 750,
              lineHeight: 1,
              boxShadow: expandModeGlow
                ? '0 0 12px rgba(245, 158, 11, 0.4), 0 0 24px rgba(245, 158, 11, 0.2)'
                : 'none',
              transition: 'box-shadow 0.4s ease, border-color 0.4s ease',
            }}
          >
            <span style={{ color: 'var(--text-secondary-strong)', fontWeight: 800 }}>Expand:</span>
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

      <div ref={toolbarMenuRef} style={{ position: 'relative', display: 'inline-block' }}>
        <IconButton
          onClick={() => setShowToolbarMenu((v: boolean) => !v)}
          active={showToolbarMenu}
          title="More"
          icon={Icons.Dots}
        />
        {showToolbarMenu && (
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
              Canvas menu
            </div>

            <div style={{ borderTop: '1px solid var(--border-subtle)' }}>
              <button
                type="button"
                onClick={() => {
                  setShowToolbarMenu(false);
                  handleDownload();
                }}
                disabled={nodes.length === 0 && edges.length === 0}
          style={{
                    width: '100%',
                  padding: '10px 12px',
                    border: 'none',
                  background: 'transparent',
                  cursor: nodes.length === 0 && edges.length === 0 ? 'not-allowed' : 'pointer',
                  color: 'var(--text-heading)',
                  display: 'flex',
                  alignItems: 'center',
                  gap: 10,
                  opacity: nodes.length === 0 && edges.length === 0 ? 0.45 : 1,
                }}
              >
                <span style={{ color: 'var(--text-secondary-strong)' }}>{Icons.Download}</span>
                <span style={{ fontSize: 12, fontWeight: 700 }}>Download graph</span>
              </button>
            </div>

            <div style={{ padding: '10px 12px', fontSize: 11, color: 'var(--text-secondary-strong)', borderTop: '1px solid var(--border-subtle)', display: 'flex', justifyContent: 'space-between' }}>
              <span>{nodes.length} nodes</span>
              <span>{edges.length} edges</span>
            </div>
            {error && (
              <div style={{ padding: '10px 12px', fontSize: 11, color: '#EF4444', borderTop: '1px solid var(--border-subtle)' }}>
                {error}
              </div>
            )}
          </div>
        )}
      </div>
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

        {/* Canvas toolbar overlay (actions + overflow menu) */}
    <div
      style={{
        position: 'absolute',
        top: 12,
        right: 12,
            zIndex: 80,
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: '6px 8px',
        borderRadius: 12,
        border: '1px solid var(--border-subtle)',
        background: theme === 'dark' ? 'rgba(0,0,0,0.35)' : 'rgba(255,255,255,0.65)',
        backdropFilter: 'blur(8px)',
        boxShadow: theme === 'dark' ? '0 8px 20px rgba(0,0,0,0.35)' : '0 8px 20px rgba(0,0,0,0.12)',
        pointerEvents: 'auto',
      }}
    >
          {toolbarContent}
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

        <ReactFlow
            nodes={displayNodes}
            edges={displayEdges}
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
          {graphQueryChips.length > 0 && (
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

          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              e.preventDefault();
              if (!lensId) return;
              setDomainOverlayError(null);
              setDomainOverlayOpen(true);
            }}
            disabled={!lensId || domainOverlayLoading || domainOverlayExecuting}
            title="Strategic overlay"
            style={{
              pointerEvents: 'auto',
              width: 44,
              height: 44,
              borderRadius: 999,
              border: '1px solid var(--border-subtle)',
              background: theme === 'dark' ? 'rgba(0,0,0,0.55)' : 'rgba(255,255,255,0.80)',
              color: 'var(--text-heading)',
              cursor: (!lensId || domainOverlayLoading || domainOverlayExecuting) ? 'not-allowed' : 'pointer',
              opacity: (!lensId || domainOverlayLoading || domainOverlayExecuting) ? 0.55 : 1,
              boxShadow: 'none',
              filter: theme === 'dark' ? 'drop-shadow(0 16px 40px rgba(0,0,0,0.55))' : 'drop-shadow(0 16px 40px rgba(0,0,0,0.18))',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontSize: 16,
              fontWeight: 900,
              lineHeight: 1,
              backdropFilter: 'blur(8px)',
            }}
          >
            S
          </button>

          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              e.preventDefault();
              if (!lensId) return;
              setAskGraphError(null);
              setAskGraphOpen(true);
            }}
            disabled={!lensId || nodes.length === 0 || askGraphLoading}
            title="Ask this graph"
            style={{
              pointerEvents: 'auto',
              width: 44,
              height: 44,
              borderRadius: 999,
              border: '1px solid var(--border-subtle)',
              background: theme === 'dark' ? 'rgba(0,0,0,0.55)' : 'rgba(255,255,255,0.80)',
              color: 'var(--text-heading)',
              cursor: (!lensId || nodes.length === 0 || askGraphLoading) ? 'not-allowed' : 'pointer',
              opacity: (!lensId || nodes.length === 0 || askGraphLoading) ? 0.55 : 1,
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
        </div>

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

        {/* Overlay empty-state (граф всё равно виден как canvas) */}
        {nodes.length === 0 && !loading && (
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
                        {scopePreview.slice(0, 5).map((it) => (
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
                        ))}
                        {scopePreview.length > 5 && (
                          <div style={{ padding: '8px 16px', fontSize: 12, color: 'var(--text-secondary)', fontStyle: 'italic' }}>
                            ...and {scopePreview.length - 5} more
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
                  <span aria-hidden="true" style={{ display: 'inline-flex' }}>{Icons.Load}</span>
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
            position: 'fixed',
            inset: 0,
            zIndex: 9999,
            display: 'flex',
            alignItems: 'flex-start',
            justifyContent: 'center',
            paddingTop: '15vh',
            background: theme === 'dark' ? 'rgba(0,0,0,0.5)' : 'rgba(255,255,255,0.4)',
            backdropFilter: 'blur(4px)',
          }}
          onClick={() => closeAddNode()}
        >
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
                    if (picked.kind === 'scout') {
                      void scoutForQuery(picked.query);
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
                placeholder="Search nodes (on graph) or add fragments…"
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
                  Type to search nodes on this graph, or add from the global index
                </div>
              )}

              {/* Scout from query */}
              {addNodeQueryTrim.length >= 2 && (
                <div>
                  <div style={{ padding: '10px 16px 6px', fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
                    Scout
                  </div>
                  {(() => {
                    const absIdx = 0;
                    const isActive = absIdx === addNodeActiveIndex;
                    return (
                      <button
                        key={`scout-${addNodeQueryTrim}`}
                        type="button"
                        onClick={() => void scoutForQuery(addNodeQueryTrim)}
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
                          Adds to scope and starts scouting; node will appear when ready.
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

              {/* On-graph matches */}
              {graphSearchResults.length > 0 && (
                <div style={{ padding: '10px 16px 6px', fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
                  On graph
                </div>
              )}
              {graphSearchResults.map((m, idx) => {
                const absIdx = (addNodeQueryTrim.length >= 2 ? 1 : 0) + idx;
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

              {/* Index matches (global) */}
              {addNodeQueryTrim.length >= 2 && (
                <div style={{ padding: `${graphSearchResults.length > 0 ? '12px' : '10px'} 16px 6px`, fontSize: 11, fontWeight: 800, color: 'var(--text-secondary-strong)', letterSpacing: 0.4, textTransform: 'uppercase' }}>
                  Add to graph
                </div>
              )}

              {addNodeQueryTrim.length >= 2 && !addNodeLoading && !addNodeError && filteredAddNodeResults.length === 0 && (
                <div style={{ padding: '16px 24px', textAlign: 'center', color: 'var(--text-secondary-strong)', fontSize: 13 }}>
                  No results for "{addNodeQueryTrim}"
                </div>
              )}

              {filteredAddNodeResults.map((r, idx) => {
                const absIdx = (addNodeQueryTrim.length >= 2 ? 1 : 0) + graphSearchResults.length + idx;
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
                justifyContent: 'flex-end',
                gap: 12,
                fontSize: 11,
                color: 'var(--text-secondary-strong)',
              }}
            >
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
      )}

      {/* Domain Overlay Command Palette Overlay */}
      {domainOverlayOpen && (
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
          onClick={() => closeDomainOverlay()}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              width: 640,
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
                S
              </div>
              <input
                ref={domainOverlayInputRef}
                value={domainOverlayQuery}
                onChange={(e) => {
                  setDomainOverlayQuery(e.target.value);
                  setDomainOverlayPreview(null);
                  setDomainOverlayError(null);
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') {
                    e.preventDefault();
                    closeDomainOverlay();
                    return;
                  }
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    if (domainOverlayPreview?.intent?.domains?.length) {
                      void executeDomainOverlay();
                    } else {
                      void submitDomainOverlayPreview();
                    }
                    return;
                  }
                }}
                placeholder="Describe how control or influence works here… (Enter to check)"
                autoComplete="off"
                spellCheck={false}
                disabled={domainOverlayLoading || domainOverlayExecuting}
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
              {(domainOverlayLoading || domainOverlayExecuting) && (
                <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                  {domainOverlayExecuting ? 'Starting…' : 'Analyzing…'}
                </div>
              )}
            </div>

            <div style={{ padding: 14 }}>
              {domainOverlayError && (
                <div style={{ padding: '10px 12px', borderRadius: 12, background: theme === 'dark' ? 'rgba(239,68,68,0.10)' : 'rgba(239,68,68,0.08)', border: '1px solid rgba(239,68,68,0.22)', color: '#EF4444', fontSize: 13 }}>
                  {domainOverlayError}
                </div>
              )}

              {!domainOverlayError && domainOverlayPreview?.intent?.domains?.length > 0 && (
                <div style={{ padding: 12, borderRadius: 12, border: '1px solid var(--border-subtle)', background: theme === 'dark' ? 'rgba(255,255,255,0.04)' : 'rgba(0,0,0,0.03)' }}>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', marginBottom: 6 }}>We’ll analyze as</div>
                  <div style={{ fontSize: 14, fontWeight: 700, color: 'var(--text-heading)' }}>
                    {String(domainOverlayPreview.intent.domains[0]?.title || domainOverlayPreview.intent.domains[0]?.canonicalPath || 'Domain')}
                  </div>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', marginTop: 6, lineHeight: 1.4 }}>
                    <div>{String(domainOverlayPreview.intent.domains[0]?.canonicalPath || '').trim()}</div>
                    {domainOverlayPreview.intent.domains[0]?.contractType && (
                      <div>Type: {String(domainOverlayPreview.intent.domains[0]?.contractType || '').trim()}</div>
                    )}
                  </div>
                </div>
              )}

              {!domainOverlayError && (!domainOverlayPreview?.intent?.domains?.length) && (
                <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.5 }}>
                  This graph shows the baseline relationships. Your question defines a domain; we’ll identify it and suggest the actions to build that domain automatically.
                </div>
              )}

              <div style={{ marginTop: 12, display: 'flex', justifyContent: 'flex-end', gap: 10 }}>
                <button
                  type="button"
                  onClick={() => closeDomainOverlay()}
                  disabled={domainOverlayLoading || domainOverlayExecuting}
                  style={{
                    height: 36,
                    padding: '0 14px',
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: 'transparent',
                    color: 'var(--text-secondary-strong)',
                    cursor: (domainOverlayLoading || domainOverlayExecuting) ? 'not-allowed' : 'pointer',
                  }}
                >
                  Close
                </button>
                <button
                  type="button"
                  onClick={() => void submitDomainOverlayPreview()}
                  disabled={domainOverlayLoading || domainOverlayExecuting || !domainOverlayQuery.trim() || !lensId}
                  style={{
                    height: 36,
                    padding: '0 16px',
                    borderRadius: 12,
                    border: '1px solid var(--border-subtle)',
                    background: 'transparent',
                    color: 'var(--text-heading)',
                    fontWeight: 700,
                    cursor: (domainOverlayLoading || domainOverlayExecuting || !domainOverlayQuery.trim() || !lensId) ? 'not-allowed' : 'pointer',
                    opacity: (domainOverlayLoading || domainOverlayExecuting || !domainOverlayQuery.trim() || !lensId) ? 0.6 : 1,
                  }}
                >
                  Check
                </button>
                <button
                  type="button"
                  onClick={() => void executeDomainOverlay()}
                  disabled={domainOverlayExecuting || domainOverlayLoading || !domainOverlayPreview?.intent?.domains?.length}
                  style={{
                    height: 36,
                    padding: '0 16px',
                    borderRadius: 12,
                    border: 'none',
                    background: 'var(--accent-primary, #3B82F6)',
                    color: '#fff',
                    fontWeight: 800,
                    cursor: (domainOverlayExecuting || domainOverlayLoading || !domainOverlayPreview?.intent?.domains?.length) ? 'not-allowed' : 'pointer',
                    opacity: (domainOverlayExecuting || domainOverlayLoading || !domainOverlayPreview?.intent?.domains?.length) ? 0.6 : 1,
                    boxShadow: '0 8px 20px rgba(59,130,246,0.25)',
                  }}
                >
                  Analyze
                </button>
              </div>
            </div>
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
    </div>
  );
};
