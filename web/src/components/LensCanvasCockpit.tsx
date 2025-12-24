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
import type { LensItem } from '../types/lensItem';
import type { HorizonNodeData } from './HorizonGraphNodeTypes';

export interface LensCanvasCockpitProps {
  lensId: string | null;
  projectId?: string | null;
}

export const LensCanvasCockpit: React.FC<LensCanvasCockpitProps> = ({ lensId, projectId }) => {
  const { theme } = React.useContext(ThemeContext);
  const reactFlowInstanceRef = useRef<ReactFlowInstance | null>(null);
  
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [lensItems, setLensItems] = React.useState<LensItem[]>([]);

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
        const items = await api.getLensItems(lensId);
        setLensItems(items);
        
        // Преобразуем LensItems в ноды и рёбра
        const { nodes: loadedNodes, edges: loadedEdges } = convertLensItemsToGraph(items);
        setNodes(loadedNodes);
        setEdges(loadedEdges);
      } catch (err: any) {
        console.error('[Cockpit] Failed to load lens items', err);
        setError(err.message || 'Failed to load graph');
      }
    };

    loadLensItems();
  }, [lensId, setNodes, setEdges]);

  // Преобразование LensItems в ReactFlow ноды и рёбра
  const convertLensItemsToGraph = useCallback((items: LensItem[]) => {
    const graphNodes: Node<HorizonNodeData>[] = [];
    const graphEdges: Edge[] = [];

    // Создаём маппинг fragmentId -> nodeId для рёбер
    const fragmentToNodeId = new Map<string, string>();

    // Обрабатываем ноды
    items
      .filter(item => item.kind === 'node')
      .forEach(item => {
        const nodeId = item.fragmentId || item.id;
        if (item.fragmentId) {
          fragmentToNodeId.set(item.fragmentId, nodeId);
        }

        const position = item.position || { x: Math.random() * 400, y: Math.random() * 400 };

        graphNodes.push({
          id: nodeId,
          type: 'horizonFragment',
          position,
          data: {
            id: nodeId,
            lensItemId: item.id,
            fragmentId: item.fragmentId,
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
    items
      .filter(item => item.kind === 'edge')
      .forEach(item => {
        const sourceId = item.sourceFragmentId ? fragmentToNodeId.get(item.sourceFragmentId) : null;
        const targetId = item.targetFragmentId ? fragmentToNodeId.get(item.targetFragmentId) : null;

        if (sourceId && targetId) {
          graphEdges.push({
            id: item.id,
            source: sourceId,
            target: targetId,
            type: 'default',
            label: item.edgeType || item.subkind,
            markerEnd: {
              type: MarkerType.ArrowClosed,
            },
            style: {
              stroke: item.criticality && item.criticality > 0.7 ? '#EF4444' : '#8B5CF6',
              strokeWidth: item.criticality ? Math.max(1, item.criticality * 3) : 2,
            },
            data: {
              lensItemId: item.id,
              edgeType: item.edgeType,
              criticality: item.criticality,
              amplification: item.amplification,
              latencyHours: item.latencyHours,
              direction: item.direction,
            },
          });
        }
      });

    return { nodes: graphNodes, edges: graphEdges };
  }, []);

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

      // 3. Создаём LensItems для нод
      const nodeItems: Partial<LensItem>[] = fragments.map((fragment: any, index: number) => {
        // Извлекаем метрики из state (может быть MetricValue объект или число)
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
          fragmentId: fragment.id,
          canonicalPath: fragment.canonicalPath,
          label: fragment.labels?.en || fragment.canonicalPath?.split('/').pop() || `Fragment ${index}`,
          type: fragment.type,
          state: Object.keys(stateMetrics).length > 0 ? stateMetrics : undefined,
          status: fragment.status,
          position: {
            x: Math.random() * 400 + 100,
            y: Math.random() * 400 + 100,
          },
          critical: false,
          expanded: false,
          collapsed: false,
        };
      });

      // 4. Создаём LensItems для рёбер
      const edgeItems: Partial<LensItem>[] = edges.map((edge: any) => {
        // Находим source и target по fragmentId или по индексу в массиве nodes
        const sourceFragment = fragments.find((f: any) => f.id === edge.sourceFragmentId || f.id === edge.source);
        const targetFragment = fragments.find((f: any) => f.id === edge.targetFragmentId || f.id === edge.target);
        
        return {
          kind: 'edge' as const,
          subkind: edge.type || 'DEPENDS_ON',
          sourceFragmentId: sourceFragment?.id || edge.sourceFragmentId || edge.source,
          targetFragmentId: targetFragment?.id || edge.targetFragmentId || edge.target,
          edgeType: edge.type,
          criticality: edge.properties?.criticality,
          amplification: edge.properties?.amplification,
          latencyHours: edge.properties?.latencyHours,
          direction: 'forward' as const,
        };
      });

      // 5. Сохраняем LensItems асинхронно
      console.log('[Cockpit] Creating lens items...', { nodes: nodeItems.length, edges: edgeItems.length });
      
      const createPromises = [
        ...nodeItems.map(item => api.createLensItem(lensId, item)),
        ...edgeItems.map(item => api.createLensItem(lensId, item)),
      ];

      // Не ждём завершения всех созданий, но отслеживаем прогресс
      Promise.all(createPromises)
        .then((ids) => {
          console.log('[Cockpit] Created lens items:', ids.length);
          // Перезагружаем LensItems для отображения
          return api.getLensItems(lensId);
        })
        .then((items) => {
          setLensItems(items);
          const { nodes: newNodes, edges: newEdges } = convertLensItemsToGraph(items);
          setNodes(newNodes);
          setEdges(newEdges);
          setLoading(false);
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

  const bgColor = theme === 'dark' ? 'var(--bg-primary)' : 'var(--bg-primary)';
  const bgPatternColor = theme === 'dark' ? 'rgba(255, 255, 255, 0.05)' : 'rgba(0, 0, 0, 0.05)';

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
        {error && (
          <div style={{ fontSize: '11px', color: '#EF4444', marginLeft: '8px' }}>
            {error}
          </div>
        )}
        {lensItems.length > 0 && (
          <div style={{ fontSize: '11px', color: 'var(--text-secondary-strong)', marginLeft: 'auto' }}>
            {lensItems.filter(i => i.kind === 'node').length} nodes, {lensItems.filter(i => i.kind === 'edge').length} edges
          </div>
        )}
      </div>

      {/* Graph */}
      <div style={{ flex: 1, minHeight: 0, position: 'relative', width: '100%' }}>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
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
