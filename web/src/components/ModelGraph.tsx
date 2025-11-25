import React, { useCallback, useMemo } from 'react';
import ReactFlow, {
  Node,
  Edge,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  Connection,
  addEdge,
  NodeTypes,
  EdgeTypes,
  MarkerType,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { ThemeContext } from './ThemeProvider';

// Типы для онтологических сущностей (соответствуют subkind в interactions)
type OntologyEntityType = 'entity' | 'event' | 'claim' | 'relation' | 'concept' | 'metricState' | 'trend' | 'causalLink' | 'scenario' | 'actionableItem';

// Маппинг subkind к типу (нормализация)
function normalizeEntityType(subkind: string | undefined, entityData: any): OntologyEntityType {
  const typeFromData = entityData?.type?.toLowerCase();
  const subkindLower = subkind?.toLowerCase();
  
  // Проверяем известные типы
  const validTypes: OntologyEntityType[] = ['entity', 'event', 'claim', 'relation', 'concept', 'metricState', 'trend', 'causalLink', 'scenario', 'actionableItem'];
  
  if (typeFromData && validTypes.includes(typeFromData as OntologyEntityType)) {
    return typeFromData as OntologyEntityType;
  }
  if (subkindLower && validTypes.includes(subkindLower as OntologyEntityType)) {
    return subkindLower as OntologyEntityType;
  }
  
  // Fallback на entity
  return 'entity';
}

interface OntologyNodeData {
  id: string;
  type: OntologyEntityType;
  label: string;
  canonicalName?: string;
  description?: string;
  [key: string]: any;
}

interface OntologyEdgeData {
  id: string;
  type: string;
  source: string;
  target: string;
  label?: string;
}

// Цвета для разных типов сущностей (в соответствии с цветовой схемой)
const getEntityColor = (type: OntologyEntityType, theme: 'light' | 'dark'): string => {
  const colors: Record<OntologyEntityType, { light: string; dark: string }> = {
    entity: { light: '#3B82F6', dark: '#60A5FA' }, // blue
    event: { light: '#10B981', dark: '#34D399' }, // green
    claim: { light: '#F59E0B', dark: '#FBBF24' }, // yellow/orange
    relation: { light: '#8B5CF6', dark: '#A78BFA' }, // purple
    concept: { light: '#EC4899', dark: '#F472B6' }, // pink
    metricState: { light: '#06B6D4', dark: '#22D3EE' }, // cyan
    trend: { light: '#F97316', dark: '#FB923C' }, // orange
    causalLink: { light: '#EF4444', dark: '#F87171' }, // red
    scenario: { light: '#6366F1', dark: '#818CF8' }, // indigo
    actionableItem: { light: '#14B8A6', dark: '#2DD4BF' }, // teal
  };
  return colors[type]?.[theme] || (theme === 'dark' ? '#60A5FA' : '#3B82F6');
};

// Кастомный узел для онтологических сущностей
const OntologyNode: React.FC<{ data: OntologyNodeData }> = ({ data }) => {
  const { theme } = React.useContext(ThemeContext);
  const color = getEntityColor(data.type, theme);
  const bgColor = theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)';
  const textColor = theme === 'dark' ? 'var(--text-heading)' : 'var(--text-heading)';
  const borderColor = color;

  return (
    <div
      style={{
        background: bgColor,
        border: `2px solid ${borderColor}`,
        borderRadius: '8px',
        padding: '8px 12px',
        minWidth: '120px',
        boxShadow: theme === 'dark' 
          ? `0 2px 8px rgba(0, 0, 0, 0.3), 0 0 0 1px ${borderColor}40`
          : `0 2px 8px rgba(0, 0, 0, 0.1), 0 0 0 1px ${borderColor}40`,
      }}
    >
      <div
        style={{
          fontSize: '10px',
          fontWeight: 600,
          color: borderColor,
          textTransform: 'uppercase',
          letterSpacing: '0.5px',
          marginBottom: '4px',
        }}
      >
        {data.type.charAt(0).toUpperCase() + data.type.slice(1).replace(/([A-Z])/g, ' $1').trim()}
      </div>
      <div
        style={{
          fontSize: '13px',
          fontWeight: 500,
          color: textColor,
          lineHeight: '1.4',
          wordBreak: 'break-word',
        }}
      >
        {data.label || data.canonicalName || data.id}
      </div>
      {data.description && (
        <div
          style={{
            fontSize: '11px',
            color: 'var(--text-secondary)',
            marginTop: '4px',
            lineHeight: '1.3',
            maxWidth: '200px',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {data.description}
        </div>
      )}
    </div>
  );
};

const nodeTypes: NodeTypes = {
  ontology: OntologyNode,
};

// Hook для проверки наличия нод (используется в родительском компоненте для empty state)
export const useModelGraphHasNodes = (): boolean => {
  const interactions = useGenaiChatStore((s) => s.interactions);
  
  return useMemo(() => {
    const ontologyInteractions = interactions.filter((it) => {
      return it.kind === 'ontology';
    });
    
    // Проверяем, есть ли хотя бы одна interaction с валидными данными
    // Исключаем удаленные сущности (status='deleted')
    return ontologyInteractions.some((it) => {
      if (!it.content) return false;
      try {
        const entityData = JSON.parse(it.content);
        return entityData.status !== 'deleted' && entityData.status !== 'archived';
      } catch {
        return false;
      }
    });
  }, [interactions]);
};

export const ModelGraph: React.FC = () => {
  const { theme } = React.useContext(ThemeContext);
  const interactions = useGenaiChatStore((s) => s.interactions);
  const projectId = useGenaiChatStore((s) => s.projectId);

  // Фильтруем interactions для онтологических сущностей
  // Используем kind='ontology' с subkind для типа сущности
  const ontologyInteractions = useMemo(() => {
    return interactions.filter((it) => {
      return it.kind === 'ontology';
    });
  }, [interactions]);

  // Преобразуем interactions в ноды и связи для ReactFlow
  // Interactions хранят финальные состояния сущностей (не операции)
  // При загрузке истории показываем последнее состояние каждой сущности
  const { nodes, edges } = useMemo(() => {
    const nodesMap = new Map<string, { node: Node; createdAt: string }>();
    const edgesList: Edge[] = [];

    // Сортируем interactions по createdAt для обработки в хронологическом порядке
    // Последнее состояние сущности перезапишет предыдущее
    const sortedInteractions = [...ontologyInteractions].sort((a, b) => {
      const timeA = new Date(a.createdAt || 0).getTime();
      const timeB = new Date(b.createdAt || 0).getTime();
      return timeA - timeB;
    });

    sortedInteractions.forEach((interaction) => {
      try {
        // Парсим content как JSON - содержит финальное состояние сущности
        let entityData: any = {};
        if (interaction.content) {
          try {
            entityData = JSON.parse(interaction.content);
          } catch {
            // Если не JSON, пропускаем этот interaction
            console.warn('[ModelGraph] Failed to parse content as JSON', interaction.id);
            return;
          }
        } else {
          // Нет content - пропускаем
          return;
        }

        // Проверяем статус - пропускаем удаленные сущности
        if (entityData.status === 'deleted' || entityData.status === 'archived') {
          // Удаляем ноду, если она существует
          const entityId = entityData.id || interaction.ontologyEntityId || interaction.id;
          nodesMap.delete(entityId);
          // Удаляем связанные edges
          const edgesToRemove: number[] = [];
          edgesList.forEach((edge, idx) => {
            if (edge.source === entityId || edge.target === entityId) {
              edgesToRemove.push(idx);
            }
          });
          edgesToRemove.reverse().forEach((idx) => edgesList.splice(idx, 1));
          return;
        }

        // Используем ID сущности из данных (финальное состояние)
        const entityId = entityData.id || interaction.ontologyEntityId || interaction.id;
        if (!entityId) {
          console.warn('[ModelGraph] No entity ID found', interaction.id);
          return;
        }

        // Нормализуем тип сущности из subkind или entityData.type
        const entityType = normalizeEntityType(interaction.subkind, entityData);
        const label = entityData.canonicalName || entityData.name || entityData.title || entityData.label || entityId;

        // Создаем или обновляем ноду (последнее состояние перезапишет предыдущее)
        const existing = nodesMap.get(entityId);
        const nodePosition = existing?.node.position || {
          x: Math.random() * 800,
          y: Math.random() * 600,
        };

        nodesMap.set(entityId, {
          node: {
            id: entityId,
            type: 'ontology',
            position: nodePosition,
            data: {
              id: entityId,
              type: entityType,
              label,
              canonicalName: entityData.canonicalName,
              description: entityData.description,
              ...entityData,
            },
          },
          createdAt: interaction.createdAt || new Date().toISOString(),
        });

        // Обрабатываем связи в зависимости от типа сущности
        if (entityType === 'relation') {
          // Relation: subjectId -> objectId
          if (entityData.subjectId && entityData.objectId && entityData.subjectId !== entityData.objectId) {
            edgesList.push({
              id: `${entityId}-relation-edge`,
              source: entityData.subjectId,
              target: entityData.objectId,
              type: 'smoothstep',
              label: entityData.predicate || 'related',
              markerEnd: {
                type: MarkerType.ArrowClosed,
                width: 20,
                height: 20,
              },
              style: {
                stroke: theme === 'dark' ? 'var(--text-secondary)' : 'var(--text-secondary)',
                strokeWidth: 1.5,
              },
              labelStyle: {
                fill: theme === 'dark' ? 'var(--text-secondary-strong)' : 'var(--text-secondary-strong)',
                fontSize: '11px',
              },
            });
          }
        } else if (entityType === 'claim') {
          // Claim: subjectRef -> objectRef
          if (entityData.subjectRef?.id && entityData.objectRef) {
            const sourceId = entityData.subjectRef.id;
            const targetId = entityData.objectRef.id || (typeof entityData.objectRef.value === 'string' ? entityData.objectRef.value : null);
            if (sourceId && targetId && sourceId !== targetId) {
              edgesList.push({
                id: `${entityId}-claim-edge`,
                source: sourceId,
                target: targetId,
                type: 'smoothstep',
                label: entityData.predicate || 'claims',
                markerEnd: {
                  type: MarkerType.ArrowClosed,
                  width: 20,
                  height: 20,
                },
                style: {
                  stroke: theme === 'dark' ? 'var(--text-secondary)' : 'var(--text-secondary)',
                  strokeWidth: 1.5,
                },
                labelStyle: {
                  fill: theme === 'dark' ? 'var(--text-secondary-strong)' : 'var(--text-secondary-strong)',
                  fontSize: '11px',
                },
              });
            }
          }
        } else if (entityType === 'causalLink') {
          // CausalLink: causeRef -> effectRef
          if (entityData.causeRef?.id && entityData.effectRef?.id) {
            const sourceId = entityData.causeRef.id;
            const targetId = entityData.effectRef.id;
            if (sourceId !== targetId) {
              edgesList.push({
                id: `${entityId}-causal-edge`,
                source: sourceId,
                target: targetId,
                type: 'smoothstep',
                label: entityData.mechanism || 'causes',
                markerEnd: {
                  type: MarkerType.ArrowClosed,
                  width: 20,
                  height: 20,
                },
                style: {
                  stroke: theme === 'dark' ? 'var(--text-secondary)' : 'var(--text-secondary)',
                  strokeWidth: 1.5,
                },
                labelStyle: {
                  fill: theme === 'dark' ? 'var(--text-secondary-strong)' : 'var(--text-secondary-strong)',
                  fontSize: '11px',
                },
              });
            }
          }
        } else if (entityType === 'event') {
          // Event: participants (entityId -> event)
          if (entityData.participants && Array.isArray(entityData.participants)) {
            entityData.participants.forEach((participant: any, idx: number) => {
              const participantId = participant.entityId;
              if (participantId && participantId !== entityId) {
                edgesList.push({
                  id: `${participantId}-${entityId}-participant-${idx}`,
                  source: participantId,
                  target: entityId,
                  type: 'smoothstep',
                  label: participant.role || 'participates',
                  markerEnd: {
                    type: MarkerType.ArrowClosed,
                    width: 20,
                    height: 20,
                  },
                  style: {
                    stroke: theme === 'dark' ? 'var(--text-secondary)' : 'var(--text-secondary)',
                    strokeWidth: 1.5,
                  },
                  labelStyle: {
                    fill: theme === 'dark' ? 'var(--text-secondary-strong)' : 'var(--text-secondary-strong)',
                    fontSize: '11px',
                  },
                });
              }
            });
          }
        } else if (entityType === 'metricState') {
          // MetricState: subjectRef -> metricState
          if (entityData.subjectRef?.id && entityData.subjectRef.id !== entityId) {
            edgesList.push({
              id: `${entityData.subjectRef.id}-${entityId}-metric`,
              source: entityData.subjectRef.id,
              target: entityId,
              type: 'smoothstep',
              label: entityData.metricName || 'metric',
              markerEnd: {
                type: MarkerType.ArrowClosed,
                width: 20,
                height: 20,
              },
              style: {
                stroke: theme === 'dark' ? 'var(--text-secondary)' : 'var(--text-secondary)',
                strokeWidth: 1.5,
              },
              labelStyle: {
                fill: theme === 'dark' ? 'var(--text-secondary-strong)' : 'var(--text-secondary-strong)',
                fontSize: '11px',
              },
            });
          }
        } else if (entityType === 'trend') {
          // Trend: subjectRef -> trend
          if (entityData.focus?.subjectScope?.ids && Array.isArray(entityData.focus.subjectScope.ids)) {
            entityData.focus.subjectScope.ids.forEach((subjectId: string, idx: number) => {
              if (subjectId !== entityId) {
                edgesList.push({
                  id: `${subjectId}-${entityId}-trend-${idx}`,
                  source: subjectId,
                  target: entityId,
                  type: 'smoothstep',
                  label: entityData.focus.metricName || 'trend',
                  markerEnd: {
                    type: MarkerType.ArrowClosed,
                    width: 20,
                    height: 20,
                  },
                  style: {
                    stroke: theme === 'dark' ? 'var(--text-secondary)' : 'var(--text-secondary)',
                    strokeWidth: 1.5,
                  },
                  labelStyle: {
                    fill: theme === 'dark' ? 'var(--text-secondary-strong)' : 'var(--text-secondary-strong)',
                    fontSize: '11px',
                  },
                });
              }
            });
          }
        } else if (entityType === 'scenario') {
          // Scenario: impacts -> targetRef
          if (entityData.impacts && Array.isArray(entityData.impacts)) {
            entityData.impacts.forEach((impact: any, idx: number) => {
              const targetId = impact.targetRef?.id;
              if (targetId && targetId !== entityId) {
                edgesList.push({
                  id: `${entityId}-${targetId}-scenario-impact-${idx}`,
                  source: entityId,
                  target: targetId,
                  type: 'smoothstep',
                  label: impact.metricName || 'impacts',
                  markerEnd: {
                    type: MarkerType.ArrowClosed,
                    width: 20,
                    height: 20,
                  },
                  style: {
                    stroke: theme === 'dark' ? 'var(--text-secondary)' : 'var(--text-secondary)',
                    strokeWidth: 1.5,
                  },
                  labelStyle: {
                    fill: theme === 'dark' ? 'var(--text-secondary-strong)' : 'var(--text-secondary-strong)',
                    fontSize: '11px',
                  },
                });
              }
            });
          }
        } else if (entityType === 'actionableItem') {
          // ActionableItem: assignedTo, relatedEntityIds
          if (entityData.assignedTo && entityData.assignedTo !== entityId) {
            edgesList.push({
              id: `${entityData.assignedTo}-${entityId}-assigned`,
              source: entityData.assignedTo,
              target: entityId,
              type: 'smoothstep',
              label: 'assigned to',
              markerEnd: {
                type: MarkerType.ArrowClosed,
                width: 20,
                height: 20,
              },
              style: {
                stroke: theme === 'dark' ? 'var(--text-secondary)' : 'var(--text-secondary)',
                strokeWidth: 1.5,
              },
              labelStyle: {
                fill: theme === 'dark' ? 'var(--text-secondary-strong)' : 'var(--text-secondary-strong)',
                fontSize: '11px',
              },
            });
          }
          if (entityData.relatedEntityIds && Array.isArray(entityData.relatedEntityIds)) {
            entityData.relatedEntityIds.forEach((relatedId: string, idx: number) => {
              if (relatedId !== entityId) {
                edgesList.push({
                  id: `${entityId}-${relatedId}-related-${idx}`,
                  source: entityId,
                  target: relatedId,
                  type: 'smoothstep',
                  label: 'related',
                  markerEnd: {
                    type: MarkerType.ArrowClosed,
                    width: 20,
                    height: 20,
                  },
                  style: {
                    stroke: theme === 'dark' ? 'var(--text-secondary)' : 'var(--text-secondary)',
                    strokeWidth: 1.5,
                  },
                  labelStyle: {
                    fill: theme === 'dark' ? 'var(--text-secondary-strong)' : 'var(--text-secondary-strong)',
                    fontSize: '11px',
                  },
                });
              }
            });
          }
        }
      } catch (err) {
        console.warn('[ModelGraph] Failed to parse interaction', interaction.id, err);
      }
    });

    // Преобразуем Map в массив нод (берем только node, без createdAt)
    return {
      nodes: Array.from(nodesMap.values()).map((item) => item.node),
      edges: edgesList,
    };
  }, [ontologyInteractions, theme]);

  const [reactFlowNodes, setNodes, onNodesChange] = useNodesState(nodes);
  const [reactFlowEdges, setEdges, onEdgesChange] = useEdgesState(edges);

  // Обновляем ноды и связи при изменении данных
  React.useEffect(() => {
    setNodes(nodes);
    setEdges(edges);
  }, [nodes, edges, setNodes, setEdges]);

  const onConnect = useCallback(
    (params: Connection) => {
      setEdges((eds) => addEdge(params, eds));
    },
    [setEdges]
  );

  // Цвета для фона графа
  const bgColor = theme === 'dark' ? 'var(--bg-body)' : 'var(--bg-body)';
  const bgPatternColor = theme === 'dark' ? 'var(--overlay-line-90)' : 'var(--overlay-line-90)';

  // Empty state - показываем только если нет нод
  if (reactFlowNodes.length === 0) {
    return null; // Возвращаем null, чтобы родительский компонент мог показать empty state
  }

  return (
    <div style={{ width: '100%', height: '100%', background: bgColor, position: 'relative' }}>
      <ReactFlow
        nodes={reactFlowNodes}
        edges={reactFlowEdges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        nodeTypes={nodeTypes}
        fitView
        style={{ background: bgColor, width: '100%', height: '100%' }}
        minZoom={0.1}
        maxZoom={2}
        defaultViewport={{ x: 0, y: 0, zoom: 1 }}
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
            const data = node.data as OntologyNodeData;
            return getEntityColor(data.type, theme);
          }}
          maskColor={theme === 'dark' ? 'rgba(0, 0, 0, 0.5)' : 'rgba(255, 255, 255, 0.5)'}
        />
      </ReactFlow>
    </div>
  );
};

