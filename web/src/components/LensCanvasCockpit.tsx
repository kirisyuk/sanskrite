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
} from 'reactflow';
import 'reactflow/dist/style.css';
import { ThemeContext } from './ThemeProvider';
import { HORIZON_NODE_TYPES, setHorizonNodeVisualQuality, type HorizonNodeVisualQuality } from './HorizonGraphNodeTypes';
import { api } from '../lib/api';
import {
  layoutExpansionWithGroups,
  layoutFullGraph,
  layoutLinearFullGraph,
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

// Custom edge types for the graph
const CUSTOM_EDGE_TYPES: EdgeTypes = {
  arcTop: ArcTopEdge,
};

export interface LensCanvasCockpitProps {
  lensId: string | null;
  projectId?: string | null;
  toolbarContainer?: HTMLDivElement | null;
  onNodeSelect?: (nodeId: string | null, nodeData: HorizonNodeData | null) => void;
  onEdgeSelect?: (edgeId: string | null, edgeData: any | null) => void;
  onScopeScout?: (input: { fragmentId: string; canonicalPath?: string; maxRecursionDepth?: number }) => Promise<void>;
}

export const LensCanvasCockpit: React.FC<LensCanvasCockpitProps> = ({ lensId, projectId, toolbarContainer, onNodeSelect, onEdgeSelect, onScopeScout }) => {
  const { theme } = React.useContext(ThemeContext);
  const DEV = import.meta.env.DEV;
  const dbg = React.useCallback((...args: any[]) => {
    if (DEV) console.log(...args);
  }, [DEV]);
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
  const [scopeScoutModal, setScopeScoutModal] = React.useState<{
    fragmentId: string;
    canonicalPath?: string;
    label?: string;
  } | null>(null);
  const [scopeScoutDepth, setScopeScoutDepth] = React.useState<1 | 2>(1);
  const [scopeScoutInFlightId, setScopeScoutInFlightId] = React.useState<string | null>(null);
  const [hoveredGroupId, setHoveredGroupId] = React.useState<string | null>(null);
  const [hoveredNodeId, setHoveredNodeId] = React.useState<string | null>(null);
  const [hoveredEdgeId, setHoveredEdgeId] = React.useState<string | null>(null);
  const [gridState, setGridState] = React.useState<GridState>(createEmptyGrid());

  // Hover updates can fire very frequently; coalesce to 1 update per frame to avoid re-render storms.
  const hoverRafRef = React.useRef<number | null>(null);
  const pendingHoverRef = React.useRef<{
    hoveredGroupId: string | null;
    hoveredNodeId: string | null;
    hoveredEdgeId: string | null;
  }>({ hoveredGroupId: null, hoveredNodeId: null, hoveredEdgeId: null });

  const scheduleHoverCommit = React.useCallback(() => {
    if (hoverRafRef.current !== null) return;
    hoverRafRef.current = window.requestAnimationFrame(() => {
      hoverRafRef.current = null;
      const next = pendingHoverRef.current;
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

  React.useEffect(() => {
    return () => {
      if (hoverRafRef.current !== null) {
        window.cancelAnimationFrame(hoverRafRef.current);
        hoverRafRef.current = null;
      }
    };
  }, []);

  type HoverHighlightMode = 'off' | 'ownership' | 'production' | 'supplies' | 'competition';
  const [hoverHighlightMode, setHoverHighlightMode] = React.useState<HoverHighlightMode>('ownership');

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
  const [layoutMode, setLayoutMode] = React.useState<'freeform' | 'linear'>('freeform');
  const [showLayoutDropdown, setShowLayoutDropdown] = React.useState(false);

  // Visual settings (toolbar modal)
  type VisualPreset = 'max' | 'balanced' | 'minimal' | 'custom';
  const [visualSettingsOpen, setVisualSettingsOpen] = React.useState(false);
  const [visualPreset, setVisualPreset] = React.useState<VisualPreset>('balanced');
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

  // Lens scope preview (empty state)
  const [lensName, setLensName] = React.useState<string | null>(null);
  const [lensScopePaths, setLensScopePaths] = React.useState<string[]>([]);
  const [scopePreview, setScopePreview] = React.useState<Array<{ canonicalPath: string; name?: string; type?: string }>>([]);
  const [scopePreviewLoading, setScopePreviewLoading] = React.useState(false);
  const [lensMetaLoading, setLensMetaLoading] = React.useState(false);

  // Layout mode helpers - определяют, какую логику раскладки использовать
  const isFreeformLayout = layoutMode === 'freeform';
  const isLinearLayout = layoutMode === 'linear';

  // Prevent mode-switch relayout loops
  const lastAppliedLayoutModeRef = React.useRef<'freeform' | 'linear' | null>(null);

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

  // Add-node: focus input when opened
  React.useEffect(() => {
    if (!addNodeOpen) return;
    const t = window.setTimeout(() => addNodeInputRef.current?.focus(), 0);
    return () => window.clearTimeout(t);
  }, [addNodeOpen]);

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
        setLensName(typeof lens?.name === 'string' ? lens.name : null);
        const scope = Array.isArray(lens?.scope) ? lens.scope : [];
        const cleaned = scope
          .map((s: any) => String(s || '').trim())
          .filter(Boolean);
        setLensScopePaths(cleaned);
      } catch (e) {
        if (!alive) return;
        // Best-effort: keep empty scope preview if we can't load lens metadata
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

        addNodeCacheRef.current.set(q, next);
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

  // Stable proxies to avoid TDZ/circular hook deps between expand and collapse handlers.
  const invokeLoadRelated = React.useCallback((id: string) => {
    onLoadRelatedRef.current?.(id);
  }, []);
  const invokeCollapse = React.useCallback((id: string) => {
    onCollapseRef.current?.(id);
  }, []);
  const invokeScopeScout = React.useCallback((fragmentId: string, canonicalPath?: string) => {
    onScopeScoutRef.current?.(fragmentId, canonicalPath);
  }, []);

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

  // Helper function for edge styles based on type
  // Цветовая схема общепринятая для графов знаний
  const getEdgeStyle = (edgeType: string | undefined, criticality: number | undefined) => {
    const type = (edgeType || '').toUpperCase();
    const baseWidth = criticality ? Math.max(1, criticality * 2) : 1.5;
    
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
    
    // 🔵 Владение/Иерархия — Синий
    if (['OWNS', 'MANAGES', 'CONTROLS', 'INVESTS_IN', 'EMPLOYED_BY', 'BOARD_MEMBER_OF', 'FOUNDED', 'PARENT_OF', 'SUBSIDIARY_OF'].includes(type)) {
      return {
        stroke: '#3B82F6', // blue-500
        strokeWidth: baseWidth,
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

  const normalizeEdgeType = useCallback((t: string | undefined) => (t || '').toUpperCase().trim(), []);

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

  // Загружаем существующие LensItems при монтировании
  React.useEffect(() => {
    if (!lensId) {
      setLensItems([]);
      setNodes([]);
      setEdges([]);
      return;
    }

    const loadLensItems = async () => {
      try {
        const itemsRaw = await api.getLensItems(lensId);
        const items = Array.isArray(itemsRaw) ? (itemsRaw as LensItem[]) : [];
        if (!Array.isArray(itemsRaw)) {
          console.warn('[Cockpit] getLensItems returned non-array, defaulting to []:', itemsRaw);
        }
        setLensItems(items);
        
        // Преобразуем LensItems в ноды и рёбра
        const { nodes: loadedNodes, edges: loadedEdges } = convertLensItemsToGraph(items);
        dbg('[Cockpit] Initial load converted:', { nodes: loadedNodes.length, edges: loadedEdges.length });
        setNodes(loadedNodes);
        setEdges(loadedEdges);
        
        // Fit view after initial load
        setTimeout(() => {
          if (reactFlowInstanceRef.current && loadedNodes.length > 0) {
            reactFlowInstanceRef.current.fitView({ padding: 0.1, duration: 300 });
          }
        }, 100);
      } catch (err: any) {
        console.error('[Cockpit] Failed to load lens items', err);
        setError(err.message || 'Failed to load graph');
      }
    };

    loadLensItems();
  }, [lensId, setNodes, setEdges, dbg]);

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
        const sourceId = sFrag ? fragmentToNodeId.get(sFrag) : null;
        const targetId = tFrag ? fragmentToNodeId.get(tFrag) : null;

        if (sourceId && targetId) {
          const baseStyle = getEdgeStyle(item.edgeType, item.criticality);
          // В linear режиме используем кривые edges (ReactFlow built-in "default" bezier), в freeform - default
          const edgeRenderType = 'default';
          graphEdges.push({
            id: item.id,
            source: sourceId,
            target: targetId,
            type: edgeRenderType,
            label: item.edgeType || item.subkind,
            // Increase hit-area for clicking thin edges (compatible with ReactFlow v11 types)
            interactionWidth: 18,
            markerEnd: {
              type: MarkerType.ArrowClosed,
            },
            style: baseStyle,
            data: {
              lensItemId: item.id,
              edgeType: item.edgeType,
              criticality: item.criticality,
              amplification: item.amplification,
              latencyHours: item.latencyHours,
              direction: item.direction,
              baseStyle,
            },
          });
        } else if (sFrag || tFrag) {
          skippedEdges.push({ s: sFrag, t: tFrag });
        }
      });

      if (skippedEdges.length > 0) {
        dbg(
          `[Cockpit] ${skippedEdges.length} edges skipped (endpoints not loaded). Use + to load related fragments.`,
        );
      }
      dbg('[Cockpit] Converted to graph:', { nodes: graphNodes.length, edges: graphEdges.length });
    return { nodes: graphNodes, edges: graphEdges };
  }, [norm, dbg]);

  // Загрузка графа из Cockpit API и сохранение в LensItems
  const handleLoadGraph = useCallback(async () => {
    if (!lensId) {
      setError('Lens ID is required');
      return;
    }

    setLoading(true);
    setError(null);

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
      
      const fragments = cockpitData.graph.nodes || [];
      const edges = cockpitData.graph.edges || [];

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
            label: fragment.labels?.en || fragment.canonicalPath?.split('/').pop() || `Fragment ${index}`,
            type: fragment.type,
            state: Object.keys(stateMetrics).length > 0 ? stateMetrics : undefined,
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
            },
            style: baseStyle,
            data: {
              lensItemId: '', // Будет обновлено после сохранения
              edgeType,  // Used for label on hover
              criticality: edge.properties?.criticality,
              amplification: edge.properties?.amplification,
              latencyHours: edge.properties?.latencyHours,
              direction: 'forward',
              baseStyle,
            },
          });
        }
      });

      // 4. Применяем ELK layout и отображаем граф
      dbg('[Cockpit] Applying ELK layout to nodes:', graphNodes.length);
      const layoutedNodes = await layoutFullGraph(graphNodes, graphEdges, {
        direction: 'DOWN',
        nodeWidth: ELK_NODE_WIDTH,
        nodeHeight: ELK_NODE_HEIGHT,
        nodeSpacing: ELK_NODE_SPACING,
        layerSpacing: ELK_LAYER_SPACING,
        maxChildRows: ELK_MAX_CHILD_ROWS,
      });

      dbg('[Cockpit] Setting nodes and edges:', { nodes: layoutedNodes.length, edges: graphEdges.length });
      // Reset collapse bookkeeping for a fresh graph
      baseNodeIdsRef.current = new Set(layoutedNodes.filter((n) => !(n.data as any)?.isGroup).map((n) => n.id));
      baseEdgeIdsRef.current = new Set(graphEdges.map((e) => e.id));
      nodeClaimCountRef.current = new Map();
      edgeClaimCountRef.current = new Map();
      expansionsRef.current = new Map();

      // Attach callbacks to nodes
      const withCallbacks = layoutedNodes.map((node) => ({
        ...node,
        data: { ...(node.data as any), onLoadRelated: invokeLoadRelated, onCollapse: invokeCollapse, onScopeScout: invokeScopeScout },
      }));
      setNodes(withCallbacks as any);
      setEdges(graphEdges);
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
          label: fragment.labels?.en || fragment.canonicalPath?.split('/').pop() || `Fragment ${index}`,
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
      const pageSize = gd.groupPageSize || 12;
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
      const nodesPerRow = 4; // GROUP_MAX_COLS

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
          const childIdx = allChildIds.indexOf(n.id);
          if (childIdx === -1) return n;

          const isVisible = visibleIds.has(n.id);
          // Calculate position relative to current page
          const pageIdx = childIdx - startIdx;
          const rowIdx = Math.floor(pageIdx / nodesPerRow);
          const colIdx = pageIdx % nodesPerRow;
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
    if (!record) return;

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
    } catch (e) {
      console.error('[Cockpit] Collapse relayout failed', e);
      // Fallback: still update state without relayout
      setNodes(nextNodes as any);
      setEdges(nextEdges as any);
    }
  }, [
    setNodes,
    setEdges,
    isLinearLayout,
    isFreeformLayout,
    setGridState,
    recalculateLinearHandles,
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
      const dependencies = await api.getFragmentDependencies(fragmentId, { direction: 'both' });
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

      const fragmentPromises = Array.from(relatedFragmentIds).map((id) =>
        api.getHorizonFragment(id).catch((err) => {
          console.warn('[Cockpit] Failed to load fragment', id, err);
          return null;
        }),
      );
      const relatedFragments = (await Promise.all(fragmentPromises)).filter((f) => f !== null) as any[];
      dbg('[Cockpit] Loaded related fragments:', relatedFragments.length);

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

      // Also check existing edges on canvas to catch relations not returned by dependencies API
      // This ensures visual consistency: if there's an edge, we should group if possible
      // const currentEdges = (reactFlowInstanceRef.current as any)?.getEdges?.() || []; // Variable already declared above
      for (const e of currentEdges) {
         if (norm(e.source) === fragmentId && !expandedNodesRef.current.has(norm(e.target))) {
             // Outgoing edge from current node to existing target
             const tId = norm(e.target);
             const targetNode = nodeById.get(tId) || nodeByFragmentId.get(tId);
             if (targetNode && !targetNode.data?.parentFragmentId) {
                 existingRelatedIds.add(tId);
                 // We will need edge type later, adding to depEdges-like structure would be complex here,
                 // but we can add to fragmentEdgeType map directly if we move this logic after map init
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
        return;
      }

      // Создаём маппинг fragmentId → тип связи и направление относительно родителя
      const fragmentEdgeType = new Map<string, string>();
      const fragmentDirection = new Map<string, 'incoming' | 'outgoing'>();
      
      // debug logs removed
      
      const updateEdgeInfo = (neighborId: string, edgeType: string, direction: 'incoming' | 'outgoing') => {
        const existingType = fragmentEdgeType.get(neighborId);
        const existingDir = fragmentDirection.get(neighborId);
        
        // Helper to check if edge type is an "incoming dependency" type
        // These types, when incoming to central node, mean "they depend on us" → should go to BOTTOM row
        const isIncomingDependencyType = (t: string) => {
          const upper = (t || '').toUpperCase().trim();
          return upper === 'DEPENDS_ON' || upper === 'REQUIRES' || upper === 'NEEDS' || upper === 'USES' || upper === 'CONSUMES';
        };
        
        if (!existingType) {
          // debug logs removed
          fragmentEdgeType.set(neighborId, edgeType);
          fragmentDirection.set(neighborId, direction);
        } else {
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
            if (direction === 'incoming' && isIncomingDependencyType(edgeType)) {
              // debug logs removed
              fragmentEdgeType.set(neighborId, edgeType);
              fragmentDirection.set(neighborId, direction);
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
            fragmentDirection.set(neighborId, direction);
          } else {
            // debug logs removed
          }
        }
      };
      
      for (const e of depEdges) {
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

      // Also populate edge info from current canvas edges if missing from API deps
      for (const e of currentEdges) {
        const s = norm(e.source);
        const t = norm(e.target);
        const edgeType = e.data?.edgeType || e.type || '';
        
        if (s === fragmentId && t) {
          updateEdgeInfo(t, edgeType, 'outgoing');
        }
        if (t === fragmentId && s) {
          updateEdgeInfo(s, edgeType, 'incoming');
        }
      }

      // Log final edge info for all neighbors
      if (layoutMode === 'linear') {
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
            label: fragment.labels?.en || fragment.canonicalPath?.split('/').pop() || 'Fragment',
            type: fragment.type,
            state: Object.keys(stateMetrics).length > 0 ? stateMetrics : undefined,
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
        'SIMILAR_TO',
        'LINKED_TO',
      ]);
      
      const existingEdgeIds = new Set(currentEdges.map((e) => e.id));
      const addedEdgePairs = new Set<string>(); // Для отслеживания уже добавленных пар (избежание дубликатов)
      const newEdges: Edge[] = [];

      for (const e of depEdges) {
        const s = String(e?.sourceFragmentId || '').trim();
        const t = String(e?.targetFragmentId || '').trim();
        if (!s || !t) continue;
        
        const edgeType = String(e?.type || e?.edgeType || '').toUpperCase();
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
        } else if (isLinearLayout) {
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
          markerEnd: { type: MarkerType.ArrowClosed },
          style: baseStyle,
          data: {
            lensItemId: '',
            edgeType: e?.type || e?.edgeType,  // Used for label on hover
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

        // В freeform режиме добавляем информацию для группировки
        // В linear режиме группировка не используется (TODO)
        if (!isFreeformLayout) {
          return n; // В linear режиме не группируем
        }
        
        const edgeTypeFromParent = fragmentEdgeType.get(nodeFragmentId) || '';
        const directionFromParent = fragmentDirection.get(nodeFragmentId) || 'outgoing';

        dbg('[Cockpit] Updating existing node for grouping (freeform):', nodeFragmentId, { edgeTypeFromParent, directionFromParent });

        const updated = {
          ...n,
          data: {
            ...n.data,
            parentFragmentId: fragmentId,
            edgeTypeFromParent,
            directionFromParent,
            depth: childDepth,
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
        layoutedNodes = await layoutLinearFullGraph(fragmentNodes as any, mergedEdges as any, {
          direction: 'DOWN',
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
        });

        // Apply Linear handle rules using the freshly updated node data (edgeTypeFromParent/directionFromParent)
        linearEdgesAfterLayout = recalculateLinearHandles(mergedEdges, layoutedNodes);
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
      if (isLinearLayout && linearEdgesAfterLayout) {
        setEdges(linearEdgesAfterLayout as any);
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
          return out;
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
      if (isFreeformLayout) {
        expandedNodesRef.current.add(fragmentId);
        if (rootNodeIdRef.current === null) {
          rootNodeIdRef.current = fragmentId;
          dbg('[Cockpit] Refs: set root node:', fragmentId);
        }
        dbg('[Cockpit] Refs: marked as expanded:', fragmentId, 'expandedNodes:', Array.from(expandedNodesRef.current));

        // Update grid state: add expanded node to the grid (freeform only)
        setGridState((prevGrid) => {
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
    isFreeformLayout,
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
  // - ownership: transitive over OWNS/SUBSIDIARY_OF
  // - supplies: direct neighbors via SUPPLIES_TO
  // - production: direct neighbors via PRODUCES
  // - competition: direct neighbors via COMPETES_WITH/CONFLICTS_WITH/OPPOSES
  // Best-effort: disabled in perf mode to avoid extra work.
  const hoverHighlight = React.useMemo(() => {
    if (perfMode) return null as null | { nodeIds: Set<string>; edgeIds: Set<string> };
    if (hoverHighlightMode === 'off') return null;
    const hovered = String(hoveredNodeId || '').trim();
    if (!hovered) return null;

    const hoveredNode = nodes.find((n) => String(n.id || '').trim() === hovered);
    if (!hoveredNode) return null;
    if ((hoveredNode.data as any)?.isGroup) return null;

    // Visible nodes (exclude hidden) + group lookup
    const visibleNodeIds = new Set<string>();
    const groupIds = new Set<string>();
    for (const n of nodes) {
      const id = String(n.id || '').trim();
      if (!id) continue;
      if ((n as any).hidden) continue;
      visibleNodeIds.add(id);
      if ((n.data as any)?.isGroup) groupIds.add(id);
    }
    if (!visibleNodeIds.has(hovered)) return null;

    const normalizeType = (t: string) => String(t || '').toUpperCase().trim();
    const isEdgeTypeOfMode = (t: string) => {
      const u = normalizeType(t);
      switch (hoverHighlightMode) {
        case 'ownership':
          return u === 'OWNS' || u === 'SUBSIDIARY_OF';
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

    type Adj = { to: string; edgeId: string };
    const adj = new Map<string, Adj[]>();
    const edgeIds = new Set<string>();

    for (const e of edges) {
      const eid = String((e as any)?.id || '').trim();
      const src = String((e as any)?.source || '').trim();
      const dst = String((e as any)?.target || '').trim();
      if (!eid || !src || !dst) continue;
      if (!visibleNodeIds.has(src) || !visibleNodeIds.has(dst)) continue;
      if (groupIds.has(src) || groupIds.has(dst)) continue;

      const edgeType = String((e as any)?.data?.edgeType || (e as any)?.label || (e as any)?.type || '');
      if (!isEdgeTypeOfMode(edgeType)) continue;

      edgeIds.add(eid);
      if (!adj.has(src)) adj.set(src, []);
      if (!adj.has(dst)) adj.set(dst, []);
      adj.get(src)!.push({ to: dst, edgeId: eid });
      adj.get(dst)!.push({ to: src, edgeId: eid });
    }

    if (adj.size === 0) return null;

    const visited = new Set<string>();
    const usedEdgeIds = new Set<string>();
    const maxSteps = hoverHighlightMode === 'ownership' ? 6 : 1;
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
  }, [perfMode, hoverHighlightMode, hoveredNodeId, nodes, edges]);

  // Pagination: hide children on non-active pages; never render edges connected to hidden nodes.
  const hiddenNodeIds = React.useMemo(() => {
    const out = new Set<string>();
    for (const n of nodes) {
      if ((n as any).hidden) out.add(n.id);
    }
    return out;
  }, [nodes]);

  const visibleEdges = React.useMemo(() => {
    if (hiddenNodeIds.size === 0) return edges;
    return edges.filter((e) => !hiddenNodeIds.has(e.source) && !hiddenNodeIds.has(e.target));
  }, [edges, hiddenNodeIds]);

  // Base edge presentation (labels hidden, dimmed) - computed only when the underlying edge set changes.
  // This avoids O(E) object cloning on every hover state update when nothing is hovered/selected.
  const baseVisibleEdges = React.useMemo(() => {
    const DIM_OPACITY = 0.22;
    if (perfMode) return visibleEdges;
    return visibleEdges.map((e) => {
      const baseStyle = (e as any)?.data?.baseStyle || e.style || {};
      return {
        ...e,
        label: undefined,
        style: { ...baseStyle, opacity: DIM_OPACITY },
      };
    });
  }, [visibleEdges, perfMode]);

  // Derive edges styles for hover highlighting (do not mutate base edges)
  // Labels are hidden by default to reduce visual noise, shown only on hover
  const displayEdges = React.useMemo(() => {
    const DIM_OPACITY = 0.22;
    const FADE_OPACITY = 0.12;

    // Perf mode: do not derive per-edge styles/labels (avoids O(E) object cloning on hover),
    // just return filtered edges.
    if (perfMode) return baseVisibleEdges;

    const baseStyled = (e: Edge) => {
      const baseStyle = (e as any)?.data?.baseStyle || e.style || {};
      const edgeLabel = (e as any)?.data?.edgeType || e.label;
      return { baseStyle, edgeLabel };
    };

    // Helper to create edge with optional label
    const withLabel = (e: Edge, showLabel: boolean, style: any) => {
      const { edgeLabel } = baseStyled(e);
      return {
        ...e,
        label: showLabel ? edgeLabel : undefined,
        style,
      };
    };

    // Edge hover: show only this edge's label
    if (hoveredEdgeId) {
      return baseVisibleEdges.map((e) => {
        const { baseStyle } = baseStyled(e);
        const isHovered = e.id === hoveredEdgeId;
        if (isHovered) {
          const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
          return withLabel(e, true, {
            ...baseStyle,
            opacity: 1,
            strokeWidth: strokeWidth + 1.5,
          });
        }
        return withLabel(e, false, { ...baseStyle, opacity: FADE_OPACITY });
      });
    }

    // Group hover has priority over node hover
    if (hoveredGroupId) {
      const groupNode = nodes.find((n) => n.id === hoveredGroupId);
      const gd: any = groupNode?.data;
      if (!groupNode || !gd?.isGroup) {
        return baseVisibleEdges.map((e) => {
          const { baseStyle } = baseStyled(e);
          return withLabel(e, false, { ...baseStyle, opacity: DIM_OPACITY });
        });
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
        return baseVisibleEdges.map((e) => {
          const { baseStyle } = baseStyled(e);
          return withLabel(e, false, { ...baseStyle, opacity: DIM_OPACITY });
        });
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

      return baseVisibleEdges.map((e) => {
        const { baseStyle } = baseStyled(e);
        if (isGroupEdge(e)) {
          const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
          return withLabel(e, true, {
            ...baseStyle,
            opacity: 1,
            strokeWidth: strokeWidth + 1.5,
          });
        }
        return withLabel(e, false, { ...baseStyle, opacity: FADE_OPACITY });
      });
    }

    // Hover highlight mode: highlight selected relation cluster and fade everything else.
    if (hoverHighlight && hoveredNodeId) {
      const hiEdgeIds = hoverHighlight.edgeIds;
      return baseVisibleEdges.map((e) => {
        const { baseStyle } = baseStyled(e);
        const eid = String((e as any)?.id || '').trim();
        const isHiEdge = eid && hiEdgeIds.has(eid);
        const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
        if (isHiEdge) {
          return withLabel(e, true, {
            ...baseStyle,
            opacity: 1,
            strokeWidth: Math.max(3, strokeWidth + 1.0),
          });
        }
        return withLabel(e, false, { ...baseStyle, opacity: FADE_OPACITY });
      });
    }

    // Node hover: highlight incident edges and show their labels
    if (hoveredNodeId) {
      const nodeId = hoveredNodeId;
      return baseVisibleEdges.map((e) => {
        const { baseStyle } = baseStyled(e);
        const isIncident = e.source === nodeId || e.target === nodeId;
        if (isIncident) {
          const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
          return withLabel(e, true, {
            ...baseStyle,
            opacity: 1,
            strokeWidth: strokeWidth + 1.0,
          });
        }
        return withLabel(e, false, { ...baseStyle, opacity: FADE_OPACITY });
      });
    }

    // Selected node (when not hovering): highlight incident edges, show labels
    if (selectedNodeId) {
      const nodeId = selectedNodeId;
      return baseVisibleEdges.map((e) => {
        const { baseStyle } = baseStyled(e);
        const isIncident = e.source === nodeId || e.target === nodeId;
        if (isIncident) {
          const strokeWidth = typeof (baseStyle as any).strokeWidth === 'number' ? (baseStyle as any).strokeWidth : 1.5;
          return withLabel(e, true, {
            ...baseStyle,
            opacity: 1,
            strokeWidth: strokeWidth + 0.75,
          });
        }
        return withLabel(e, false, { ...baseStyle, opacity: DIM_OPACITY });
      });
    }

    // Default: dim all edges, hide labels
    return baseVisibleEdges;
  }, [baseVisibleEdges, hoveredEdgeId, hoveredGroupId, hoveredNodeId, nodes, normalizeEdgeType, selectedNodeId, perfMode, hoverHighlight]);

  const displayNodes = React.useMemo(() => {
    if (perfMode) return nodes;
    if (!hoverHighlight || !hoveredNodeId) return nodes;

    const owned = hoverHighlight.nodeIds;
    const hovered = String(hoveredNodeId || '').trim();

    return nodes.map((n) => {
      const id = String(n.id || '').trim();
      const isGroup = Boolean((n.data as any)?.isGroup);
      const isOwnedNode = id && owned.has(id);
      const isHovered = id === hovered;

      // Keep group nodes visible but muted to reduce noise.
      const opacity = isGroup ? 0.18 : (isOwnedNode || isHovered ? 1 : 0.22);

      // Elevate ownership chain nodes above muted ones.
      const zIndex = isOwnedNode || isHovered ? 20 : (isGroup ? 1 : 5);

      return {
        ...n,
        zIndex,
        style: {
          ...(n as any).style,
          opacity,
        },
      } as any;
    });
  }, [perfMode, nodes, hoverHighlight, hoveredNodeId]);

  // Lightweight info for perf mode (no re-styling of edges)
  const perfHoveredEdgeInfo = React.useMemo(() => {
    if (!perfMode || !hoveredEdgeId) return null;
    const e = edges.find((x) => x.id === hoveredEdgeId) as any;
    if (!e) return null;
    const edgeType = String(e?.data?.edgeType || e?.label || e?.data?.subkind || '').trim();
    return edgeType ? { id: hoveredEdgeId, edgeType } : { id: hoveredEdgeId, edgeType: 'EDGE' };
  }, [perfMode, hoveredEdgeId, edges]);

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
        arrangedFragments = await layoutFullGraph(fragmentNodes, edges, {
        direction: 'DOWN',
        nodeWidth: ELK_NODE_WIDTH,
        nodeHeight: ELK_NODE_HEIGHT,
        nodeSpacing: ELK_NODE_SPACING,
        layerSpacing: ELK_LAYER_SPACING,
        maxChildRows: ELK_MAX_CHILD_ROWS,
      });

      // Сохраняем onLoadRelated callback для каждой ноды
        const finalFragments = arrangedFragments.map((node) => ({
        ...node,
        data: {
          ...node.data,
          onLoadRelated: invokeLoadRelated,
          onCollapse: invokeCollapse,
          onScopeScout: invokeScopeScout,
        },
      }));

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
        arrangedFragments = await layoutLinearFullGraph(fragmentNodes, edges, {
          direction: 'DOWN',
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
        });

        const finalFragments = arrangedFragments.map((node) => ({
          ...node,
          data: {
            ...node.data,
            onLoadRelated: invokeLoadRelated,
            onCollapse: invokeCollapse,
            onScopeScout: invokeScopeScout,
          },
        }));

        // Linear layout уже создает группы внутри layoutLinearFullGraph
        withGroups = finalFragments;
        
        // Recalculate edge handles for Linear layout
        const updatedEdges = recalculateLinearHandles(edges, finalFragments);
        setEdges(updatedEdges);
      } else {
        // Fallback: используем freeform
        arrangedFragments = await layoutFullGraph(fragmentNodes, edges, {
          direction: 'DOWN',
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
        });

        const finalFragments = arrangedFragments.map((node) => ({
          ...node,
          data: {
            ...node.data,
            onLoadRelated: invokeLoadRelated,
            onCollapse: invokeCollapse,
            onScopeScout: invokeScopeScout,
          },
        }));

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

      // Fit view после arrange
      setTimeout(() => {
        if (reactFlowInstanceRef.current && withGroups.length > 0) {
          reactFlowInstanceRef.current.fitView({ padding: 0.1, duration: 400 });
        }
      }, 100);

      dbg('[Cockpit] Arrange completed');
    } catch (err: any) {
      console.error('[Cockpit] Arrange failed:', err);
      setError(err.message || 'Failed to arrange graph');
    } finally {
      setArranging(false);
    }
  }, [nodes, edges, arranging, handleLoadRelatedFragments, handleGroupPageChange, setNodes, setEdges, recalculateLinearHandles, recalculateFreeformHandles, isLinearLayout, isFreeformLayout]);

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
        const n = inst?.getNode?.(fragmentId);
        if (n) inst?.fitView?.({ nodes: [n], padding: 0.25, duration: 350 });
      } catch {}

      closeAddNode();
    },
    [
      lensId,
      nodes,
      projectId,
      setLensItems,
      setNodes,
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

  const bgColor = theme === 'dark' ? 'var(--bg-primary)' : 'var(--bg-primary)';
  const bgPatternColor = theme === 'dark' ? 'rgba(255, 255, 255, 0.05)' : 'rgba(0, 0, 0, 0.05)';

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
  }, [onNodeSelect, onEdgeSelect, edges, nodes]);

  // Handle edge click
  const handleEdgeClick = useCallback((event: React.MouseEvent, edge: Edge) => {
    if (import.meta.env.DEV) console.log('[Cockpit] Edge clicked:', edge.id, edge.data);
    ignorePaneClicksUntilRef.current = Date.now() + 250;
    event.stopPropagation();
    event.preventDefault();
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
  }, [onEdgeSelect, nodes]);

  // Handle pane click (click on empty space) - clear selection
  const handlePaneClick = useCallback(() => {
    if (Date.now() < ignorePaneClicksUntilRef.current) {
      return;
    }
    if (import.meta.env.DEV) console.log('[Cockpit] Pane clicked - clearing selection');
    if (onNodeSelect) {
      onNodeSelect(null, null);
    }
    if (onEdgeSelect) {
      onEdgeSelect(null, null);
    }
  }, [onNodeSelect, onEdgeSelect]);

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
    if (!onScopeScout) return;
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
      await onScopeScout({ fragmentId, canonicalPath: scopeScoutModal.canonicalPath, maxRecursionDepth: scopeScoutDepth });
      ok = true;
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
  }, [lensId, onScopeScout, scopeScoutModal, scopeScoutDepth, setNodes]);

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
    Search: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" /></svg>,
    Sliders: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><line x1="4" y1="21" x2="4" y2="14" /><line x1="4" y1="10" x2="4" y2="3" /><line x1="12" y1="21" x2="12" y2="12" /><line x1="12" y1="8" x2="12" y2="3" /><line x1="20" y1="21" x2="20" y2="16" /><line x1="20" y1="12" x2="20" y2="3" /><line x1="1" y1="14" x2="7" y2="14" /><line x1="9" y1="8" x2="15" y2="8" /><line x1="17" y1="16" x2="23" y2="16" /></svg>,
    Plus: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 5v14" /><path d="M5 12h14" /></svg>,
    X: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M18 6 6 18" /><path d="M6 6l12 12" /></svg>,
    Magic: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z"/><path d="M5 3v4"/><path d="M9 3v4"/><path d="M3 7h4"/><path d="M3 3h4"/></svg>,
    Download: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>,
    Zap: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>,
    Map: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="1 6 1 22 8 18 16 22 23 18 23 2 16 6 8 2 1 6"/><line x1="8" y1="2" x2="8" y2="18"/><line x1="16" y1="6" x2="16" y2="22"/></svg>,
    Grid: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>,
    Layout: <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><line x1="9" y1="3" x2="9" y2="21"/><line x1="15" y1="3" x2="15" y2="21"/></svg>,
    ChevronDown: <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="6 9 12 15 18 9"/></svg>,
  };

  // Layout dropdown component
  const LayoutDropdown = React.useMemo(() => {
    const layoutOptions: Array<{ value: 'freeform' | 'linear'; label: string }> = [
      { value: 'freeform', label: 'Freeform' },
      { value: 'linear', label: 'Linear' },
    ];

    return (
      <div style={{ position: 'relative', display: 'inline-block' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
          <div style={{ width: '1px', height: '16px', background: 'var(--border-subtle)', margin: '0 4px' }} />
        <button
          type="button"
            onClick={() => setShowLayoutDropdown((v) => !v)}
          style={{
              background: showLayoutDropdown ? (theme === 'dark' ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.06)') : 'transparent',
            border: 'none',
            borderRadius: '6px',
              padding: '6px 8px',
              cursor: 'pointer',
              color: 'var(--text-secondary)',
              display: 'flex',
              alignItems: 'center',
              gap: '6px',
              transition: 'all 0.2s ease',
            }}
            title="Layout Mode"
          >
            {Icons.Layout}
            <span style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
              {layoutOptions.find(opt => opt.value === layoutMode)?.label || 'Freeform'}
            </span>
            {Icons.ChevronDown}
        </button>
          <div style={{ width: '1px', height: '16px', background: 'var(--border-subtle)', margin: '0 4px' }} />
        </div>
        
        {showLayoutDropdown && (
          <>
            <div
          style={{
                position: 'fixed',
                top: 0,
                left: 0,
                right: 0,
                bottom: 0,
                zIndex: 999,
              }}
              onClick={() => setShowLayoutDropdown(false)}
            />
            <div
              style={{
                position: 'absolute',
                top: '100%',
                left: 0,
                marginTop: '4px',
                background: theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
                border: `1px solid var(--border-subtle)`,
                borderRadius: '8px',
                boxShadow: theme === 'dark' ? '0 4px 12px rgba(0,0,0,0.3)' : '0 2px 8px rgba(0,0,0,0.1)',
                zIndex: 1000,
                minWidth: '140px',
                overflow: 'hidden',
              }}
            >
              {layoutOptions.map((option) => (
        <button
                  key={option.value}
          type="button"
                  onClick={() => {
                    setLayoutMode(option.value);
                    setShowLayoutDropdown(false);
                  }}
          style={{
                    width: '100%',
                    padding: '8px 12px',
                    background: layoutMode === option.value ? (theme === 'dark' ? 'rgba(255,255,255,0.1)' : 'rgba(0,0,0,0.06)') : 'transparent',
                    border: 'none',
                    textAlign: 'left',
                    cursor: 'pointer',
                    color: layoutMode === option.value ? 'var(--text-primary)' : 'var(--text-secondary)',
            fontSize: '12px',
                    transition: 'all 0.15s ease',
                  }}
                  onMouseEnter={(e) => {
                    if (layoutMode !== option.value) {
                      e.currentTarget.style.background = theme === 'dark' ? 'rgba(255,255,255,0.05)' : 'rgba(0,0,0,0.03)';
                    }
                  }}
                  onMouseLeave={(e) => {
                    if (layoutMode !== option.value) {
                      e.currentTarget.style.background = 'transparent';
                    }
                  }}
                >
                  {option.label}
        </button>
              ))}
            </div>
          </>
        )}
      </div>
    );
  }, [showLayoutDropdown, layoutMode, theme]);

  const toolbarContent = (
    <div style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
      <IconButton onClick={handleLoadGraph} disabled={loading || !lensId} title="Load Graph" icon={Icons.Refresh} />
      <IconButton
        onClick={() => {
          if (!lensId) return;
          setAddNodeOpen((v) => {
            const next = !v;
            if (next) {
              setAddNodeQuery('');
              setAddNodeResults([]);
              setAddNodeError(null);
              setAddNodeActiveIndex(0);
            }
            return next;
          });
        }}
        disabled={!lensId}
        active={addNodeOpen}
        title={addNodeOpen ? 'Close add-node' : 'Add node'}
        icon={Icons.Plus}
      />

      {/* Add-node input (inline in toolbar) */}

      {LayoutDropdown}
      <IconButton onClick={handleArrange} disabled={arranging || nodes.length === 0} title="Arrange Graph" icon={Icons.Magic} />
      <IconButton onClick={handleDownload} disabled={nodes.length === 0 && edges.length === 0} title="Download Graph" icon={Icons.Download} />
      
      <div style={{ width: '1px', height: '16px', background: 'var(--border-subtle)', margin: '0 4px' }} />
      
      <IconButton onClick={() => setVisualSettingsOpen(true)} title="Visual settings" icon={Icons.Sliders} />
      
        {error && (
        <div style={{ fontSize: '11px', color: '#EF4444', marginLeft: '8px', maxWidth: '200px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }} title={error}>
            {error}
          </div>
        )}
        {(nodes.length > 0 || edges.length > 0) && (
        <div style={{ fontSize: '10px', color: 'var(--text-secondary-strong)', marginLeft: '8px', fontFamily: 'monospace' }}>
          {nodes.length}n/{edges.length}e
          </div>
        )}
      </div>
  );

  const HoverHighlightControl = (
    <div
      style={{
        position: 'absolute',
        top: 12,
        right: 12,
        zIndex: 55,
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
      title="Hover highlight mode"
    >
      <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', whiteSpace: 'nowrap' }}>
        Hover
      </div>
      {(
        [
          { key: 'ownership', label: 'Ownership' },
          { key: 'production', label: 'Production' },
          { key: 'supplies', label: 'Supplies' },
          { key: 'competition', label: 'Competition' },
        ] as const
      ).map((opt) => (
        <button
          key={opt.key}
          type="button"
          disabled={perfMode}
          onClick={() => setHoverHighlightMode(opt.key)}
          style={{
            padding: '4px 8px',
            borderRadius: 999,
            border: '1px solid var(--border-subtle)',
            background: hoverHighlightMode === opt.key ? 'var(--white-10)' : 'transparent',
            color: 'var(--text-heading)',
            fontSize: 11,
            cursor: perfMode ? 'not-allowed' : 'pointer',
            opacity: perfMode ? 0.4 : 1,
          }}
        >
          {opt.label}
        </button>
      ))}
      <button
        type="button"
        disabled={perfMode}
        onClick={() => setHoverHighlightMode('off')}
        style={{
          padding: '4px 8px',
          borderRadius: 999,
          border: '1px solid var(--border-subtle)',
          background: hoverHighlightMode === 'off' ? 'var(--white-10)' : 'transparent',
          color: 'var(--text-secondary-strong)',
          fontSize: 11,
          cursor: perfMode ? 'not-allowed' : 'pointer',
          opacity: perfMode ? 0.4 : 1,
        }}
        title="Disable hover highlight mode"
      >
        Off
      </button>
    </div>
  );

  const applyVisualPreset = React.useCallback((preset: VisualPreset) => {
    setVisualPreset(preset);
    if (preset === 'max') {
      setForcePerfMode(false);
      setShowMiniMap(true);
      setShowBackgroundGrid(true);
      setHoverHighlightMode('ownership');
      setNodeVisualQuality('max');
      return;
    }
    if (preset === 'minimal') {
      setForcePerfMode(true);
      setShowMiniMap(false);
      setShowBackgroundGrid(false);
      setHoverHighlightMode('off');
      setNodeVisualQuality('minimal');
      return;
    }
    // balanced
    setForcePerfMode(null);
    setShowMiniMap(false);
    setShowBackgroundGrid(false);
    setHoverHighlightMode('ownership');
    setNodeVisualQuality('balanced');
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
      if (d?.isGroup) {
        if (pendingHoverRef.current.hoveredGroupId === node.id) {
          pendingHoverRef.current.hoveredGroupId = null;
        }
      } else {
        if (pendingHoverRef.current.hoveredNodeId === node.id) {
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
    <div style={{ width: '100%', height: '100%', position: 'relative', display: 'flex', flexDirection: 'column', minHeight: '600px' }}>
      {/* Toolbar via Portal if container provided, otherwise inline fallback */}
      {toolbarContainer ? ReactDOM.createPortal(toolbarContent, toolbarContainer) : (
        <div
          style={{
            padding: '8px 12px',
            borderBottom: `1px solid ${theme === 'dark' ? 'var(--border-subtle)' : 'var(--border-subtle)'}`,
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            background: theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
            flexShrink: 0,
          }}
        >
          {toolbarContent}
        </div>
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

              {/* Hover highlight */}
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12, fontWeight: 700 }}>Hover highlight</div>
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
                  <option value="ownership">Ownership</option>
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
        {HoverHighlightControl}
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
            }}
            fitView
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
          <Controls
            style={{
              background: theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
              border: `1px solid ${theme === 'dark' ? 'var(--border-subtle)' : 'var(--border-subtle)'}`,
              borderRadius: '8px',
            }}
          />
          {showMiniMap && (
          <MiniMap
            style={{
              background: theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
              border: `1px solid ${theme === 'dark' ? 'var(--border-subtle)' : 'var(--border-subtle)'}`,
              borderRadius: '8px',
            }}
            nodeColor={(node) => {
              const data = node.data as HorizonNodeData;
              return data.critical ? '#EF4444' : '#3B82F6';
            }}
            maskColor={theme === 'dark' ? 'rgba(0, 0, 0, 0.5)' : 'rgba(255, 255, 255, 0.5)'}
          />
          )}
        </ReactFlow>

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

      {/* Add Node Command Palette Overlay */}
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
                    setAddNodeActiveIndex((i) => Math.min(i + 1, Math.max(0, addNodeResults.length - 1)));
                    return;
                  }
                  if (e.key === 'ArrowUp') {
                    e.preventDefault();
                    setAddNodeActiveIndex((i) => Math.max(0, i - 1));
                    return;
                  }
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    const picked = addNodeResults[addNodeActiveIndex];
                    if (picked) void addNodeToLens(picked);
                    return;
                  }
                }}
                placeholder="Type to search fragments…"
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
              {!addNodeLoading && !addNodeError && addNodeQuery.trim().length < 2 && (
                <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-secondary-strong)', fontSize: 13 }}>
                  Type a name or path to find fragments
                </div>
              )}

              {!addNodeLoading && !addNodeError && addNodeQuery.trim().length >= 2 && addNodeResults.length === 0 && (
                <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-secondary-strong)', fontSize: 13 }}>
                  No fragments found for "{addNodeQuery}"
                </div>
              )}

              {addNodeError && (
                <div style={{ padding: '16px', textAlign: 'center', color: '#EF4444', fontSize: 13 }}>
                  {addNodeError}
                </div>
              )}

              {addNodeResults.map((r, idx) => {
                const isActive = idx === addNodeActiveIndex;
                return (
                  <button
                    key={`${r.canonicalPath}-${idx}`}
                    type="button"
                    onClick={() => void addNodeToLens(r)}
                    onMouseEnter={() => setAddNodeActiveIndex(idx)}
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
                      <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-secondary-strong)', textTransform: 'uppercase', opacity: 0.8, flexShrink: 0 }}>
                        {r.type}
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
                <span>to select</span>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                <kbd style={{ background: 'var(--bg-base)', border: '1px solid var(--border-subtle)', borderRadius: 4, padding: '1px 4px', fontSize: 10 }}>esc</kbd>
                <span>to close</span>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Scope-scout confirm modal (portal) */}
      {scopeScoutModal && ReactDOM.createPortal(
        <div
          role="dialog"
          aria-modal="true"
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
            <div style={{ fontSize: 14, fontWeight: 800, marginBottom: 8 }}>
              Run additional research?
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)', lineHeight: 1.4 }}>
              This will add the node to the current lens <strong>Scope</strong> and start a new scouting run with <strong>depth = 1</strong>.
            </div>

            <div style={{ marginTop: 10, fontSize: 12 }}>
              <div style={{ color: 'var(--text-secondary-strong)', marginBottom: 4 }}>Target</div>
              <div style={{ fontFamily: 'monospace', fontSize: 11, color: 'var(--text-heading)' }}>
                {scopeScoutModal.label || scopeScoutModal.canonicalPath || scopeScoutModal.fragmentId}
              </div>
            </div>

            <div style={{ marginTop: 12, display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
              <div style={{ fontSize: 12, color: 'var(--text-secondary-strong)' }}>
                Depth
              </div>
              <div style={{ display: 'flex', gap: 8 }}>
                <button
                  type="button"
                  onClick={() => setScopeScoutDepth(1)}
                  disabled={Boolean(scopeScoutInFlightId)}
                  style={{
                    padding: '6px 10px',
                    borderRadius: 10,
                    border: '1px solid var(--border-subtle)',
                    background: scopeScoutDepth === 1 ? 'var(--white-10)' : 'transparent',
                    color: 'var(--text-heading)',
                    cursor: scopeScoutInFlightId ? 'not-allowed' : 'pointer',
                    opacity: scopeScoutInFlightId ? 0.5 : 1,
                  }}
                  title="Scout only this node (and record direct relations)"
                >
                  1
                </button>
                <button
                  type="button"
                  onClick={() => setScopeScoutDepth(2)}
                  disabled={Boolean(scopeScoutInFlightId)}
                  style={{
                    padding: '6px 10px',
                    borderRadius: 10,
                    border: '1px solid var(--border-subtle)',
                    background: scopeScoutDepth === 2 ? 'var(--white-10)' : 'transparent',
                    color: 'var(--text-heading)',
                    cursor: scopeScoutInFlightId ? 'not-allowed' : 'pointer',
                    opacity: scopeScoutInFlightId ? 0.5 : 1,
                  }}
                  title="Also scout newly discovered neighbors (one additional hop)"
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
                  padding: '8px 10px',
                  borderRadius: 10,
                  border: '1px solid rgba(59,130,246,0.55)',
                  background: 'rgba(59,130,246,0.16)',
                  color: 'var(--text-heading)',
                  cursor: (!onScopeScout || scopeScoutInFlightId) ? 'not-allowed' : 'pointer',
                  opacity: (!onScopeScout || scopeScoutInFlightId) ? 0.5 : 1,
                }}
              >
                {scopeScoutInFlightId ? 'Starting…' : `Run research (depth ${scopeScoutDepth})`}
              </button>
            </div>
          </div>
        </div>,
        document.body
      )}
    </div>
  );
};
