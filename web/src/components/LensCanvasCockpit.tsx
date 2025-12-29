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
} from 'reactflow';
import 'reactflow/dist/style.css';
import { ThemeContext } from './ThemeProvider';
import { HORIZON_NODE_TYPES } from './HorizonGraphNodeTypes';
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

export interface LensCanvasCockpitProps {
  lensId: string | null;
  projectId?: string | null;
  toolbarContainer?: HTMLDivElement | null;
  onNodeSelect?: (nodeId: string | null, nodeData: HorizonNodeData | null) => void;
  onEdgeSelect?: (edgeId: string | null, edgeData: any | null) => void;
}

export const LensCanvasCockpit: React.FC<LensCanvasCockpitProps> = ({ lensId, projectId, toolbarContainer, onNodeSelect, onEdgeSelect }) => {
  const { theme } = React.useContext(ThemeContext);
  // Guard: ReactFlow can sometimes fire onPaneClick after node/edge click (sometimes twice).
  // We ignore pane clicks for a short window after selecting to avoid clearing selection immediately.
  const ignorePaneClicksUntilRef = React.useRef<number>(0);
  const reactFlowInstanceRef = useRef<ReactFlowInstance | null>(null);
  
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  
  const [loading, setLoading] = React.useState(false);
  const [arranging, setArranging] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [lensItems, setLensItems] = React.useState<LensItem[]>([]);
  const [loadingFragmentId, setLoadingFragmentId] = React.useState<string | null>(null);
  const [hoveredGroupId, setHoveredGroupId] = React.useState<string | null>(null);
  const [hoveredNodeId, setHoveredNodeId] = React.useState<string | null>(null);
  const [hoveredEdgeId, setHoveredEdgeId] = React.useState<string | null>(null);
  const [gridState, setGridState] = React.useState<GridState>(createEmptyGrid());

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
    setHoveredGroupId(null);
    setHoveredNodeId(null);
  }, [perfMode]);

  // Refs for synchronous tracking of expanded nodes (avoids stale state issues)
  const expandedNodesRef = React.useRef<Set<string>>(new Set());
  const rootNodeIdRef = React.useRef<string | null>(null);

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
    if (['OWNS', 'MANAGES', 'CONTROLS', 'PARENT_OF', 'SUBSIDIARY_OF'].includes(type)) {
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
      
      if (sourceIsCentral && !targetIsCentral) {
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
        console.log('[Cockpit] Initial load converted:', { nodes: loadedNodes.length, edges: loadedEdges.length });
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
  }, [lensId, setNodes, setEdges]);

  // Преобразование LensItems в ReactFlow ноды и рёбра
  const convertLensItemsToGraph = useCallback((items: LensItem[] | null | undefined) => {
    const safeItems: LensItem[] = Array.isArray(items) ? items : [];
    if (!Array.isArray(items)) {
      console.warn('[Cockpit] convertLensItemsToGraph received non-array, defaulting to []:', items);
    }
    console.log('[Cockpit] convertLensItemsToGraph called with items:', safeItems.length);
    console.log('[Cockpit] Sample items:', safeItems.slice(0, 3).map(item => ({ 
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
        console.log('[Cockpit] Item is not a node:', { id: item.id, kind, hasFragmentId });
      }
      
      return isExplicitNode || isLikelyNode;
    });
    console.log('[Cockpit] Filtered node items:', nodeItems.length, 'out of', safeItems.length);
    console.log('[Cockpit] Node items details:', nodeItems.map(item => ({
      id: item.id,
      fragmentId: item.fragmentId,
      kind: item.kind,
      hasPosition: !!item.position
    })));
    
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
        console.log(
          `[Cockpit] ${skippedEdges.length} edges skipped (endpoints not loaded). Use + to load related fragments.`,
        );
      }
      console.log('[Cockpit] Converted to graph:', { nodes: graphNodes.length, edges: graphEdges.length });
    return { nodes: graphNodes, edges: graphEdges };
  }, [norm]);

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
        console.log('[Cockpit] Clearing existing lens items...', lensItems.length);
        // Удаляем в фоне, не ждём завершения
        Promise.all(
          lensItems.map(item => api.deleteLensItem(lensId, item.id).catch(err => {
            console.warn('[Cockpit] Failed to delete lens item', item.id, err);
          }))
        ).then(() => {
          console.log('[Cockpit] Cleared existing lens items');
        });
      }

      // 2. Загружаем граф из Cockpit API
      console.log('[Cockpit] Loading graph from cockpit API...');
      const cockpitData = await api.getHorizonLensCockpit(lensId);
      
      const fragments = cockpitData.graph.nodes || [];
      const edges = cockpitData.graph.edges || [];

      console.log('[Cockpit] Loaded graph:', { fragments: fragments.length, edges: edges.length });

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
            onLoadRelated: handleLoadRelatedFragments, // Передаём функцию для загрузки связанных элементов
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
      console.log('[Cockpit] Applying ELK layout to nodes:', graphNodes.length);
      const layoutedNodes = await layoutFullGraph(graphNodes, graphEdges, {
        direction: 'DOWN',
        nodeWidth: ELK_NODE_WIDTH,
        nodeHeight: ELK_NODE_HEIGHT,
        nodeSpacing: ELK_NODE_SPACING,
        layerSpacing: ELK_LAYER_SPACING,
        maxChildRows: ELK_MAX_CHILD_ROWS,
      });

      console.log('[Cockpit] Setting nodes and edges:', { nodes: layoutedNodes.length, edges: graphEdges.length });
      setNodes(layoutedNodes);
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

            if (import.meta.env.DEV) {
              console.log('[Cockpit] Grid initialized from root nodes:', {
                roots: roots.map((r) => r.id),
                rootNodeId: g.rootNodeId,
                cellCount: g.cells.size,
              });
            }
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
      console.log('[Cockpit] Saving lens items in background...', { nodes: fragments.length, edges: edges.length });
      
      // Создаём LensItems для сохранения
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
          position: layoutedNodes[index]?.position,
          critical: false,
          expanded: false,
          collapsed: false,
        };
      });

      const edgeItems: Partial<LensItem>[] = edges.map((edge: any) => {
        const sourceFragment = fragments.find((f: any) => f.id === edge.sourceFragmentId || f.id === edge.source);
        const targetFragment = fragments.find((f: any) => f.id === edge.targetFragmentId || f.id === edge.target);
        
        return {
          kind: 'edge' as const,
          subkind: edge.type || 'DEPENDS_ON',
          sourceFragmentId: norm(sourceFragment?.id || edge.sourceFragmentId || edge.source),
          targetFragmentId: norm(targetFragment?.id || edge.targetFragmentId || edge.target),
          edgeType: edge.type,
          criticality: edge.properties?.criticality,
          amplification: edge.properties?.amplification,
          latencyHours: edge.properties?.latencyHours,
          direction: 'forward' as const,
        };
      });

      // Сохраняем асинхронно и обновляем ID
      Promise.all([
        ...nodeItems.map(item => api.createLensItem(lensId, item)),
        ...edgeItems.map(item => api.createLensItem(lensId, item)),
      ])
        .then((responses) => {
          console.log('[Cockpit] Saved lens items:', responses.length);
          
          // Обновляем lensItems с полученными ID
          const createdItems: LensItem[] = [];
          
          responses.slice(0, nodeItems.length).forEach((response, index) => {
            const itemData = nodeItems[index];
            const node = layoutedNodes[index];
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
              position: node?.position,
              createdAt: new Date().toISOString(),
              updatedAt: new Date().toISOString(),
            });
            
            // Обновляем lensItemId в узле
            if (node) {
              node.data.lensItemId = response.id;
            }
          });
          
          responses.slice(nodeItems.length).forEach((response, index) => {
            const itemData = edgeItems[index];
            const edge = graphEdges[index];
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
            
            // Обновляем lensItemId в ребре
            if (edge && edge.data) {
              edge.data.lensItemId = response.id;
            }
          });
          
          // Обновляем состояние с полученными ID
          setLensItems(createdItems);
          setNodes([...layoutedNodes]); // Триггерим обновление
          setEdges([...graphEdges]); // Триггерим обновление
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
        if ((n as any).parentId === groupId) {
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

  // Функция для загрузки связанных элементов фрагмента
  const handleLoadRelatedFragments = useCallback(async (rawFragmentId: string) => {
    const fragmentId = (rawFragmentId || '').trim();
    if (!fragmentId || loadingFragmentId === fragmentId) return;

    // Снапшотим исходную ноду СРАЗУ (до await), чтобы второй порядок не ломался.
    const rfNodes: Node<HorizonNodeData>[] = (reactFlowInstanceRef.current as any)?.getNodes?.() || [];
    const sourceSnapshot = rfNodes.find((n) => n?.id === fragmentId || n?.data?.fragmentId === fragmentId) || null;

    if (!sourceSnapshot) {
      console.warn('[Cockpit] Source node not found at click time for fragment:', fragmentId);
      return;
    }

    const isNodeInGroup = !!(sourceSnapshot as any).parentId && !(sourceSnapshot.data as any)?.isGroup;
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

    console.log('[Cockpit] Expansion context:', {
      fragmentId,
      isFirstExpansion,
      isNodeInGroup,
      nodeSemanticGroup,
      expandedNodes: Array.from(expandedNodesRef.current),
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
      console.log('[Cockpit] Loaded dependencies for fragment:', fragmentId, dependencies);

      const depEdges: any[] = Array.isArray(dependencies?.edges) ? dependencies.edges : [];
      if (depEdges.length === 0) {
        setNodes((prev) =>
          prev.map((n) =>
            n.id === fragmentId || n.data.fragmentId === fragmentId
              ? { ...n, data: { ...n.data, isLoadingRelated: false } }
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
      console.log('[Cockpit] Loaded related fragments:', relatedFragments.length);

      // Получаем актуальное состояние узлов
      const currentNodes: Node<HorizonNodeData>[] = (reactFlowInstanceRef.current as any)?.getNodes?.() || [];
      const currentEdges: Edge[] = (reactFlowInstanceRef.current as any)?.getEdges?.() || [];
      const existingNodeIds = new Set(currentNodes.map((n) => n.id));
      const fragmentToNodeIdMap = new Map<string, string>();

      for (const n of currentNodes) {
        const fid = n.data?.fragmentId ? norm(n.data.fragmentId) : '';
        const nid = norm(n.id);
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
            console.log('[Cockpit] Skipping root node from grouping:', fid);
            continue;
          }
          // Don't group nodes that have been expanded (they are centers of their own subgraphs)
          if (expandedNodesRef.current.has(fid)) {
            console.log('[Cockpit] Skipping already expanded node from grouping:', fid);
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
             const targetNode = currentNodes.find(n => norm(n.id) === tId || norm(n.data?.fragmentId) === tId);
             if (targetNode && !targetNode.data?.parentFragmentId) {
                 existingRelatedIds.add(tId);
                 // We will need edge type later, adding to depEdges-like structure would be complex here,
                 // but we can add to fragmentEdgeType map directly if we move this logic after map init
             }
         }
         if (norm(e.target) === fragmentId && !expandedNodesRef.current.has(norm(e.source))) {
             // Incoming edge from existing source to current node
             const sId = norm(e.source);
             const sourceNode = currentNodes.find(n => norm(n.id) === sId || norm(n.data?.fragmentId) === sId);
             if (sourceNode && !sourceNode.data?.parentFragmentId) {
                 existingRelatedIds.add(sId);
             }
         }
      }

      if (newFragmentsUnsorted.length === 0 && existingRelatedIds.size === 0) {
        console.log('[Cockpit] No new or existing related fragments to process');
        setNodes((prev) =>
          prev.map((n) =>
            n.id === fragmentId || n.data.fragmentId === fragmentId
              ? { ...n, data: { ...n.data, isLoadingRelated: false } }
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

      console.log('[Cockpit] Sorted fragments by edge priority:', 
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
          },
        };
      });

      // Добавляем новые фрагменты в маппинг
      for (const n of newNodes) {
        fragmentToNodeIdMap.set(n.id, n.id);
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
        const sourceNode = currentNodes.find(n => (n.data as any)?.fragmentId === s) || newNodes.find(n => n.id === s);
        const targetNode = currentNodes.find(n => (n.data as any)?.fragmentId === t) || newNodes.find(n => n.id === t);
        
        const sourceParentId = sourceNode ? (sourceNode.data as any)?.parentFragmentId : null;
        const targetParentId = targetNode ? (targetNode.data as any)?.parentFragmentId : null;
        const sourceRootId = sourceNode ? (sourceNode.data as any)?.rootFragmentId : null;
        const targetRootId = targetNode ? (targetNode.data as any)?.rootFragmentId : null;
        
        // Helper to get group position for a node
        const getNodePosition = (nodeId: string): 'top' | 'bottom' | 'left' | 'right' | 'center' | undefined => {
          if (nodeId === fragmentId) return 'center';
          
          const node = currentNodes.find(n => (n.data as any)?.fragmentId === nodeId) || 
                       newNodes.find(n => n.id === nodeId);
          
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
      if (isNodeInGroup && nodeSemanticGroup && isFreeformLayout) {
        console.log('[Cockpit] Extracting node from group:', fragmentId, 'semantic group:', nodeSemanticGroup);

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

            console.log('[Cockpit] Repositioning extracted node to grid cell:', cellPos, 'canvas:', canvasPos);

            return {
              ...n,
              parentId: undefined, // Remove from group container
              extent: undefined,
              position: canvasPos,
              data: newData,
            };
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
          return {
            ...n,
            parentId: undefined,
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

        console.log('[Cockpit] Updating existing node for grouping (freeform):', nodeFragmentId, { edgeTypeFromParent, directionFromParent });

        return {
          ...n,
          data: {
            ...n.data,
            parentFragmentId: fragmentId,
            edgeTypeFromParent,
            directionFromParent,
            depth: childDepth,
          },
        };
      });

      // Применяем layout для позиционирования новых узлов относительно родителя
      // Логика зависит от режима раскладки (freeform или linear)
      console.log('[Cockpit] Applying layout for new nodes:', newNodes.length, 'existing related:', existingRelatedIds.size, 'layout mode:', layoutMode);
      
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

      // Update refs SYNCHRONOUSLY before state (refs are immediate, state is async)
      // This ensures subsequent expansions see the correct values
      if (isFreeformLayout) {
        expandedNodesRef.current.add(fragmentId);
        if (rootNodeIdRef.current === null) {
          rootNodeIdRef.current = fragmentId;
          console.log('[Cockpit] Refs: set root node:', fragmentId);
        }
        console.log('[Cockpit] Refs: marked as expanded:', fragmentId, 'expandedNodes:', Array.from(expandedNodesRef.current));

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
            console.log('[Cockpit] Grid: set root node at (0, 0):', fragmentId);
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
            console.log('[Cockpit] Grid: added extracted node at cell:', cellPos, fragmentId);
          }

          // Mark this node as expanded
          const newExpandedNodes = new Set(prevGrid.expandedNodes);
          newExpandedNodes.add(fragmentId);
          newGrid.expandedNodes = newExpandedNodes;

          console.log('[Cockpit] Grid state updated:', {
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
  }, [loadingFragmentId, setEdges, setNodes, normalizeEdgeType, getEdgeHandles, handleGroupPageChange]);

  // Derive edges styles for hover highlighting (do not mutate base edges)
  // Labels are hidden by default to reduce visual noise, shown only on hover
  const displayEdges = React.useMemo(() => {
    const DIM_OPACITY = 0.22;
    const FADE_OPACITY = 0.12;

    // Collect hidden node IDs (for pagination filtering)
    const hiddenNodeIds = new Set(
      nodes.filter((n) => (n as any).hidden).map((n) => n.id)
    );

    // Filter out edges connected to hidden nodes
    const visibleEdges = edges.filter((e) =>
      !hiddenNodeIds.has(e.source) && !hiddenNodeIds.has(e.target)
    );

    // Perf mode: do not derive per-edge styles/labels (avoids O(E) object cloning on hover),
    // just return filtered edges.
    if (perfMode) return visibleEdges;

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
      return visibleEdges.map((e) => {
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
        return visibleEdges.map((e) => {
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
        nodes.filter((n) => (n as any).parentId === hoveredGroupId && !(n as any).hidden).map((n) => n.id)
      );
      if (!groupParentId || childIds.size === 0) {
        return visibleEdges.map((e) => {
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

      return visibleEdges.map((e) => {
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

    // Node hover: highlight incident edges and show their labels
    if (hoveredNodeId) {
      const nodeId = hoveredNodeId;
      return visibleEdges.map((e) => {
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
      return visibleEdges.map((e) => {
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
    return visibleEdges.map((e) => {
      const { baseStyle } = baseStyled(e);
      return withLabel(e, false, { ...baseStyle, opacity: DIM_OPACITY });
    });
  }, [edges, hoveredEdgeId, hoveredGroupId, hoveredNodeId, nodes, normalizeEdgeType, selectedNodeId, perfMode]);

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
      console.log('[Cockpit] Arranging', nodes.length, 'nodes and', edges.length, 'edges', 'layout mode:', layoutMode);

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
          onLoadRelated: handleLoadRelatedFragments,
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
            onLoadRelated: handleLoadRelatedFragments,
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
            onLoadRelated: handleLoadRelatedFragments,
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

      console.log('[Cockpit] Arrange completed');
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
        const current = (reactFlowInstanceRef.current as any)?.getNodes?.() || nodes;
        const fragmentNodes: Node<HorizonNodeData>[] = (current as any[]).filter((n) => !(n?.data as any)?.isGroup);

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
            data: { ...(node.data as any), onLoadRelated: handleLoadRelatedFragments },
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
          return { ...n, data: { ...n.data, onLoadRelated: handleLoadRelatedFragments } };
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

      console.log('[Cockpit] Downloaded', lensItems.length, 'lens items');
    } catch (err: any) {
      console.error('[Cockpit] Download failed:', err);
      setError(err.message || 'Failed to download graph data');
    }
  }, [nodes, edges, lensId, projectId]);

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

    const inst: any = reactFlowInstanceRef.current;
    const currentNodes: Node<HorizonNodeData>[] = inst?.getNodes?.() || nodes;
    const hasChildren = currentNodes.some((n) => !((n.data as any)?.isGroup) && (n.data as any)?.parentFragmentId === node.id);
    if (!hasChildren) return;

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
      } else if (isLinearLayout) {
        // Linear layout is global: re-layout the whole visible graph
        const currentEdgesForDrag: Edge[] = inst?.getEdges?.() || edges;
        const fragmentNodes = currentNodes.filter((n) => !(n.data as any)?.isGroup);
        relayouted = await layoutLinearFullGraph(fragmentNodes, currentEdgesForDrag, {
          direction: 'DOWN',
          nodeWidth: ELK_NODE_WIDTH,
          nodeHeight: ELK_NODE_HEIGHT,
          nodeSpacing: ELK_NODE_SPACING,
          layerSpacing: ELK_LAYER_SPACING,
          maxChildRows: ELK_MAX_CHILD_ROWS,
        });
        // Keep edges consistent with linear handle rules
        setEdges(recalculateLinearHandles(currentEdgesForDrag, relayouted) as any);
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
      {LayoutDropdown}
      <IconButton onClick={handleArrange} disabled={arranging || nodes.length === 0} title="Arrange Graph" icon={Icons.Magic} />
      <IconButton onClick={handleDownload} disabled={nodes.length === 0 && edges.length === 0} title="Download Graph" icon={Icons.Download} />
      
      <div style={{ width: '1px', height: '16px', background: 'var(--border-subtle)', margin: '0 4px' }} />
      
      <IconButton onClick={() => setForcePerfMode((v) => (v === null ? !isLargeGraph : !v))} active={perfMode} title={`Performance Mode: ${perfMode ? 'On' : 'Off'}`} icon={Icons.Zap} />
      <IconButton onClick={() => setShowMiniMap((v) => !v)} active={showMiniMap} title={`MiniMap: ${showMiniMap ? 'On' : 'Off'}`} icon={Icons.Map} />
      <IconButton onClick={() => setShowBackgroundGrid((v) => !v)} active={showBackgroundGrid} title={`Background Grid: ${showBackgroundGrid ? 'On' : 'Off'}`} icon={Icons.Grid} />
      
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

      {/* Graph */}
      <div style={{ flex: 1, minHeight: 0, position: 'relative', width: '100%' }}>
        <ReactFlow
            nodes={nodes}
            edges={displayEdges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onNodeClick={handleNodeClick}
            onEdgeClick={handleEdgeClick}
            onPaneClick={handlePaneClick}
            onNodeDragStop={handleNodeDragStop}
            onNodeMouseEnter={(_, node) => {
              if (perfMode) return;
              const d: any = node?.data;
              if (d?.isGroup) {
                setHoveredGroupId(node.id);
                setHoveredNodeId(null);
              } else {
                setHoveredNodeId(node.id);
                setHoveredGroupId(null);
              }
            }}
            onNodeMouseLeave={(_, node) => {
              if (perfMode) return;
              const d: any = node?.data;
              if (d?.isGroup) setHoveredGroupId(null);
              else setHoveredNodeId(null);
            }}
            onEdgeMouseEnter={(_, edge) => {
              if (perfMode) return;
              setHoveredEdgeId(edge.id);
            }}
            onEdgeMouseLeave={() => {
              if (perfMode) return;
              setHoveredEdgeId(null);
            }}
            nodeTypes={HORIZON_NODE_TYPES}
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
              pointerEvents: 'none',
            }}
          >
            <div className="text-center">
              <div className="text-sm mb-2" style={{ color: 'var(--text-heading)' }}>
                No graph data
              </div>
              <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                Click \"Load Graph\" to load graph from lens
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
