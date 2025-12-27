import React, { useCallback, useRef } from 'react';
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
import { layoutExpansionWithGroups, layoutFullGraph, rebuildGroupContainers, getSemanticGroup, SEMANTIC_GROUP_POSITION } from '../lib/elkLayout';
import type { LensItem } from '../types/lensItem';
import type { HorizonNodeData } from './HorizonGraphNodeTypes';

export interface LensCanvasCockpitProps {
  lensId: string | null;
  projectId?: string | null;
  onNodeSelect?: (nodeId: string | null, nodeData: HorizonNodeData | null) => void;
  onEdgeSelect?: (edgeId: string | null, edgeData: any | null) => void;
}

export const LensCanvasCockpit: React.FC<LensCanvasCockpitProps> = ({ lensId, projectId, onNodeSelect, onEdgeSelect }) => {
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
   * Visual layout:
   * - Supply Chain (top): child above parent, edge bottom → top
   * - Customers (bottom): child below parent, edge bottom → top  
   * - Market (left): child left of parent, edge left ↔ right
   * - Context (right): child right of parent, edge right ↔ left
   */
  const getEdgeHandles = useCallback((edgeType: string | undefined, isOutgoing: boolean): { sourceHandle: string; targetHandle: string } => {
    const sg = getSemanticGroup(edgeType, isOutgoing);
    const position = SEMANTIC_GROUP_POSITION[sg];
    
    switch (position) {
      case 'top':
        // Supply chain: vertical, always bottom → top
        return { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
      case 'bottom':
        // Customers: vertical, always bottom → top
        return { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
      case 'left':
        // Market: child is to the left of parent
        // Visual: parent.left → child.right
        if (isOutgoing) {
          // edge: parent(source) → child(target)
          // source node = parent → use left handle
          // target node = child → use right handle
          return { sourceHandle: 'source-left', targetHandle: 'target-right' };
        } else {
          // edge: child(source) → parent(target)
          // source node = child → use right handle (towards parent on right)
          // target node = parent → use left handle (from child on left)
          return { sourceHandle: 'source-right', targetHandle: 'target-left' };
        }
      case 'right':
        // Context: child is to the right of parent
        // Visual: parent.right → child.left
        if (isOutgoing) {
          // edge: parent(source) → child(target)
          // source node = parent → use right handle
          // target node = child → use left handle
          return { sourceHandle: 'source-right', targetHandle: 'target-left' };
        } else {
          // edge: child(source) → parent(target)
          // source node = child → use left handle (towards parent on left)
          // target node = parent → use right handle (from child on right)
          return { sourceHandle: 'source-left', targetHandle: 'target-right' };
        }
      default:
        return { sourceHandle: 'source-bottom', targetHandle: 'target-top' };
    }
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
          graphEdges.push({
            id: item.id,
            source: sourceId,
            target: targetId,
            type: 'default',
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
          graphEdges.push({
            // Make ids stable and unique even if backend returns duplicates or lacks id.
            // Use backend edge.id if present; otherwise key.
            id: edge.id || `edge-${key}`,
            source: sourceId,
            target: targetId,
            sourceHandle: 'source-bottom', // Исходящие связи снизу (от кнопки +)
            targetHandle: 'target-top', // Входящие связи сверху
            type: 'default',
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

    const sourceDepth = typeof sourceSnapshot?.data?.depth === 'number' ? sourceSnapshot.data.depth : 0;
    const sourceRootId = norm(sourceSnapshot?.data?.rootFragmentId || sourceSnapshot?.data?.fragmentId || sourceSnapshot?.id);

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

      // Фильтруем только новые фрагменты
      const newFragmentsUnsorted = relatedFragments.filter((f) => {
        const fid = norm(f?.id || '');
        return fid && !existingNodeIds.has(fid);
      });

      if (newFragmentsUnsorted.length === 0) {
        console.log('[Cockpit] No new fragments to add');
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
      
      for (const e of depEdges) {
        const s = String(e?.sourceFragmentId || '').trim();
        const t = String(e?.targetFragmentId || '').trim();
        const edgeType = e?.type || e?.edgeType || '';
        
        // Если родитель — source, то ребёнок — target (outgoing: родитель → ребёнок)
        if (s === fragmentId && t) {
          fragmentEdgeType.set(t, edgeType);
          fragmentDirection.set(t, 'outgoing'); // Ребёнок получает связь ОТ родителя
        }
        // Если родитель — target, то ребёнок — source (incoming: ребёнок → родитель)
        if (t === fragmentId && s) {
          if (!fragmentEdgeType.has(s)) {
            fragmentEdgeType.set(s, edgeType);
          }
          fragmentDirection.set(s, 'incoming'); // Ребёнок направлен К родителю
        }
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
        return {
          id: fid,
          type: 'horizonFragment' as const,
          position: { x: 0, y: 0 }, // Будет рассчитано
          data: {
            id: fid,
            lensItemId: '',
            fragmentId: fid,
            parentFragmentId: fragmentId,
            rootFragmentId: sourceRootId,
            edgeTypeFromParent, // Тип связи с родителем для сортировки
            directionFromParent, // 'incoming' (сверху) или 'outgoing' (снизу)
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
        
        // Для симметричных связей: показываем только исходящие от раскрытой ноды
        if (isSymmetric) {
          // Пропускаем если это входящее ребро к раскрытой ноде (обратное направление)
          if (t === fragmentId) {
            console.log('[Cockpit] Skipping reverse symmetric edge:', edgeType, s, '→', t);
            continue;
          }
          
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

        // Determine handles based on semantic group
        // isOutgoing = true if the expanded node (fragmentId) is the source
        const isOutgoing = s === fragmentId;
        const handles = getEdgeHandles(edgeType, isOutgoing);

        const baseStyle = getEdgeStyle(e?.type || e?.edgeType, e?.properties?.criticality);
        newEdges.push({
          id: edgeId,
          source: sourceId,
          target: targetId,
          sourceHandle: handles.sourceHandle,
          targetHandle: handles.targetHandle,
          type: 'default',
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

      // Применяем ELK layout для позиционирования новых узлов относительно родителя
      console.log('[Cockpit] Applying ELK layout for new nodes:', newNodes.length);
      const layoutedNodes = await layoutExpansionWithGroups(
        currentNodes,
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

      // Снимаем флаг загрузки с раскрытого узла
      const finalNodes = layoutedNodes.map((n) =>
        n.id === fragmentId || n.data.fragmentId === fragmentId
          ? { ...n, data: { ...n.data, isLoadingRelated: false } }
          : n,
      );

      // Обновляем состояние
      setNodes(finalNodes);
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
  }, [loadingFragmentId, setEdges, setNodes, normalizeEdgeType]);

  // Derive edges styles for hover highlighting (do not mutate base edges)
  // Labels are hidden by default to reduce visual noise, shown only on hover
  const displayEdges = React.useMemo(() => {
    const DIM_OPACITY = 0.22;
    const FADE_OPACITY = 0.12;

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
      return edges.map((e) => {
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
        return edges.map((e) => {
          const { baseStyle } = baseStyled(e);
          return withLabel(e, false, { ...baseStyle, opacity: DIM_OPACITY });
        });
      }

      const groupEdgeType = normalizeEdgeType(gd.groupEdgeType);
      const groupParentId = String(gd.groupParentId || '').trim();
      const groupDirection: 'incoming' | 'outgoing' | 'both' =
        gd.groupDirection === 'incoming' ? 'incoming' : gd.groupDirection === 'both' ? 'both' : 'outgoing';

      const childIds = new Set(nodes.filter((n) => (n as any).parentId === hoveredGroupId).map((n) => n.id));
      if (!groupParentId || childIds.size === 0) {
        return edges.map((e) => {
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

      return edges.map((e) => {
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
      return edges.map((e) => {
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
      return edges.map((e) => {
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
    return edges.map((e) => {
      const { baseStyle } = baseStyled(e);
      return withLabel(e, false, { ...baseStyle, opacity: DIM_OPACITY });
    });
  }, [edges, hoveredEdgeId, hoveredGroupId, hoveredNodeId, nodes, normalizeEdgeType, selectedNodeId]);

  // Функция для перераспределения всех видимых нод
  const handleArrange = useCallback(async () => {
    if (nodes.length === 0 || arranging) return;

    setArranging(true);
    setError(null);

    try {
      console.log('[Cockpit] Arranging', nodes.length, 'nodes and', edges.length, 'edges');

      // Применяем полный layout только к фрагментам (UI-группы пересоберём отдельно)
      const fragmentNodes = nodes.filter((n) => !(n.data as any)?.isGroup);
      const arrangedFragments = await layoutFullGraph(fragmentNodes, edges, {
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

      const withGroups = rebuildGroupContainers(finalFragments, {
        nodeWidth: ELK_NODE_WIDTH,
        nodeHeight: ELK_NODE_HEIGHT,
      });
      setNodes(withGroups);

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
  }, [nodes, edges, arranging, handleLoadRelatedFragments, setNodes]);

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
      const relayouted = await layoutExpansionWithGroups(
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
      setNodes(relayouted);
    } catch (err) {
      console.warn('[Cockpit] Failed to relayout groups after drag stop', err);
    }
  }, [nodes, setNodes]);

  return (
    <div style={{ width: '100%', height: '100%', position: 'relative', display: 'flex', flexDirection: 'column', minHeight: '600px' }}>
      {/* Toolbar */}
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
        <button
          type="button"
          onClick={handleLoadGraph}
          disabled={loading || !lensId}
          style={{
            padding: '6px 12px',
            fontSize: '12px',
            fontWeight: 500,
            color: 'var(--text-heading)',
            background: loading ? 'var(--bg-secondary)' : 'var(--accent-primary)',
            border: 'none',
            borderRadius: '6px',
            cursor: loading || !lensId ? 'not-allowed' : 'pointer',
            opacity: loading || !lensId ? 0.6 : 1,
          }}
        >
          {loading ? 'Loading...' : 'Load Graph'}
        </button>
        <button
          type="button"
          onClick={handleArrange}
          disabled={arranging || nodes.length === 0}
          style={{
            padding: '6px 12px',
            fontSize: '12px',
            fontWeight: 500,
            color: 'var(--text-heading)',
            background: arranging ? 'var(--bg-secondary)' : 'var(--bg-tertiary)',
            border: `1px solid ${theme === 'dark' ? 'var(--border-subtle)' : 'var(--border-subtle)'}`,
            borderRadius: '6px',
            cursor: arranging || nodes.length === 0 ? 'not-allowed' : 'pointer',
            opacity: arranging || nodes.length === 0 ? 0.6 : 1,
          }}
        >
          {arranging ? 'Arranging...' : 'Arrange'}
        </button>
        <button
          type="button"
          onClick={handleDownload}
          disabled={nodes.length === 0 && edges.length === 0}
          style={{
            padding: '6px 12px',
            fontSize: '12px',
            fontWeight: 500,
            color: 'var(--text-heading)',
            background: 'var(--bg-tertiary)',
            border: `1px solid ${theme === 'dark' ? 'var(--border-subtle)' : 'var(--border-subtle)'}`,
            borderRadius: '6px',
            cursor: nodes.length === 0 && edges.length === 0 ? 'not-allowed' : 'pointer',
            opacity: nodes.length === 0 && edges.length === 0 ? 0.6 : 1,
          }}
        >
          Download
        </button>
        {error && (
          <div style={{ fontSize: '11px', color: '#EF4444', marginLeft: '8px' }}>
            {error}
          </div>
        )}
        {(nodes.length > 0 || edges.length > 0) && (
          <div style={{ fontSize: '11px', color: 'var(--text-secondary-strong)', marginLeft: 'auto' }}>
            {nodes.length} nodes, {edges.length} edges
          </div>
        )}
      </div>

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
              const d: any = node?.data;
              if (d?.isGroup) setHoveredGroupId(null);
              else setHoveredNodeId(null);
            }}
            onEdgeMouseEnter={(_, edge) => {
              setHoveredEdgeId(edge.id);
            }}
            onEdgeMouseLeave={() => {
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
          >
          <Background color={bgPatternColor} gap={16} />
          <Controls
            style={{
              background: theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
              border: `1px solid ${theme === 'dark' ? 'var(--border-subtle)' : 'var(--border-subtle)'}`,
              borderRadius: '8px',
            }}
          />
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
        </ReactFlow>

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
