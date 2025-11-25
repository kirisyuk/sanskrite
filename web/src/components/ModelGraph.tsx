import React, { useCallback, useMemo, useRef } from 'react';
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
  ReactFlowInstance,
} from 'reactflow';
import 'reactflow/dist/style.css';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { ThemeContext } from './ThemeProvider';
import { nodeTypes } from './ModelGraphNodeTypes';

// Создаем стабильную ссылку на nodeTypes на уровне модуля
// Это гарантирует, что объект создается только один раз при загрузке модуля
// Object.freeze предотвращает любые изменения объекта
const STABLE_NODE_TYPES: NodeTypes = Object.freeze(nodeTypes);

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


// Hook для проверки наличия нод (используется в родительском компоненте для empty state)
export const useModelGraphHasNodes = (): boolean => {
  const interactions = useGenaiChatStore((s) => s.interactions);
  
  return useMemo(() => {
    const ontologyInteractions = interactions.filter((it) => {
      return it.kind === 'ontology';
    });
    
    console.log('[useModelGraphHasNodes] Checking for nodes:', {
      totalInteractions: interactions.length,
      ontologyInteractions: ontologyInteractions.length,
      interactions: ontologyInteractions.map(it => ({
        id: it.id,
        subkind: it.subkind,
        hasContent: !!it.content,
        contentPreview: it.content?.substring(0, 100),
      })),
    });
    
    // Проверяем, есть ли хотя бы одна interaction с валидными данными
    // Исключаем удаленные сущности (status='deleted')
    const hasNodes = ontologyInteractions.some((it) => {
      if (!it.content) {
        console.log('[useModelGraphHasNodes] Interaction without content:', it.id);
        return false;
      }
      try {
        const entityData = JSON.parse(it.content);
        const isValid = entityData.status !== 'deleted' && entityData.status !== 'archived';
        console.log('[useModelGraphHasNodes] Interaction check:', {
          id: it.id,
          status: entityData.status,
          isValid,
        });
        return isValid;
      } catch (err) {
        console.warn('[useModelGraphHasNodes] Failed to parse interaction:', it.id, err);
        return false;
      }
    });
    
    console.log('[useModelGraphHasNodes] Result:', hasNodes);
    return hasNodes;
  }, [interactions]);
};

export const ModelGraph: React.FC = () => {
  const { theme } = React.useContext(ThemeContext);
  const interactions = useGenaiChatStore((s) => s.interactions);
  const projectId = useGenaiChatStore((s) => s.projectId);
  const reactFlowInstanceRef = useRef<ReactFlowInstance | null>(null);

  // Фильтруем interactions для онтологических сущностей
  // Используем kind='ontology' с subkind для типа сущности
  const ontologyInteractions = useMemo(() => {
    const filtered = interactions.filter((it) => {
      return it.kind === 'ontology';
    });
    console.log('[ModelGraph] Ontology interactions:', {
      total: interactions.length,
      ontology: filtered.length,
      interactions: filtered.map(it => ({
        id: it.id,
        subkind: it.subkind,
        hasContent: !!it.content,
        contentLength: it.content?.length || 0,
      })),
    });
    return filtered;
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

        // Используем ID сущности из данных (финальное состояние)
        const entityId = entityData.id || interaction.ontologyEntityId || interaction.id;
        if (!entityId) {
          console.warn('[ModelGraph] No entity ID found', interaction.id);
          return;
        }

        // Нормализуем тип сущности из subkind или entityData.type
        const entityType = normalizeEntityType(interaction.subkind, entityData);
        
        // Relations не создаются как ноды - они только создают рёбра между существующими нодами
        // НО: мы не можем создать edge здесь, потому что ноды могут быть созданы позже
        // Поэтому мы сохраняем relation для обработки после создания всех нод
        if (entityType === 'relation') {
          // Пропускаем создание ноды для relation - они только создают рёбра
          // Edge будет создан позже, после того как все ноды будут созданы
          return;
        }
        
        // Проверяем статус - пропускаем удаленные сущности (только для нод, не для relations)
        if (entityData.status === 'deleted' || entityData.status === 'archived') {
          // Удаляем ноду, если она существует
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

        // Обрабатываем связи в зависимости от типа сущности (кроме relation - они уже обработаны выше)
        if (entityType === 'claim') {
          // Claim: subjectRef -> objectRef
          if (entityData.subjectRef?.id && entityData.objectRef) {
            const sourceId = entityData.subjectRef.id;
            const targetId = entityData.objectRef.id || (typeof entityData.objectRef.value === 'string' ? entityData.objectRef.value : null);
            if (
              sourceId &&
              targetId &&
              typeof sourceId === 'string' &&
              typeof targetId === 'string' &&
              sourceId !== 'undefined' &&
              targetId !== 'undefined' &&
              sourceId !== targetId
            ) {
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
            } else if (sourceId || targetId) {
              console.warn('[ModelGraph] Invalid claim edge data', {
                entityId,
                sourceId,
                targetId,
                sourceIdType: typeof sourceId,
                targetIdType: typeof targetId,
              });
            }
          }
        } else if (entityType === 'causalLink') {
          // CausalLink: causeRef -> effectRef
          if (entityData.causeRef?.id && entityData.effectRef?.id) {
            const sourceId = entityData.causeRef.id;
            const targetId = entityData.effectRef.id;
            if (
              sourceId &&
              targetId &&
              typeof sourceId === 'string' &&
              typeof targetId === 'string' &&
              sourceId !== 'undefined' &&
              targetId !== 'undefined' &&
              sourceId !== targetId
            ) {
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
            } else if (sourceId || targetId) {
              console.warn('[ModelGraph] Invalid causalLink edge data', {
                entityId,
                sourceId,
                targetId,
                sourceIdType: typeof sourceId,
                targetIdType: typeof targetId,
              });
            }
          }
        } else if (entityType === 'event') {
          // Event: participants (entityId -> event)
          if (entityData.participants && Array.isArray(entityData.participants)) {
            entityData.participants.forEach((participant: any, idx: number) => {
              const participantId = participant.entityId;
              if (
                participantId &&
                typeof participantId === 'string' &&
                participantId !== 'undefined' &&
                participantId !== entityId
              ) {
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
              } else if (participantId) {
                console.warn('[ModelGraph] Invalid event participant edge data', {
                  entityId,
                  participantId,
                  participantIdType: typeof participantId,
                });
              }
            });
          }
        } else if (entityType === 'metricState') {
          // MetricState: subjectRef -> metricState
          const subjectRefId = entityData.subjectRef?.id;
          if (
            subjectRefId &&
            typeof subjectRefId === 'string' &&
            subjectRefId !== 'undefined' &&
            subjectRefId !== entityId
          ) {
            edgesList.push({
              id: `${subjectRefId}-${entityId}-metric`,
              source: subjectRefId,
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
          } else if (subjectRefId) {
            console.warn('[ModelGraph] Invalid metricState edge data', {
              entityId,
              subjectRefId,
              subjectRefIdType: typeof subjectRefId,
            });
          }
        } else if (entityType === 'trend') {
          // Trend: subjectRef -> trend
          if (entityData.focus?.subjectScope?.ids && Array.isArray(entityData.focus.subjectScope.ids)) {
            entityData.focus.subjectScope.ids.forEach((subjectId: string, idx: number) => {
              if (
                subjectId &&
                typeof subjectId === 'string' &&
                subjectId !== 'undefined' &&
                subjectId !== entityId
              ) {
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
              } else if (subjectId) {
                console.warn('[ModelGraph] Invalid trend edge data', {
                  entityId,
                  subjectId,
                  subjectIdType: typeof subjectId,
                });
              }
            });
          }
        } else if (entityType === 'scenario') {
          // Scenario: impacts -> targetRef
          if (entityData.impacts && Array.isArray(entityData.impacts)) {
            entityData.impacts.forEach((impact: any, idx: number) => {
              const targetId = impact.targetRef?.id;
              if (
                targetId &&
                typeof targetId === 'string' &&
                targetId !== 'undefined' &&
                targetId !== entityId
              ) {
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
              } else if (targetId) {
                console.warn('[ModelGraph] Invalid scenario edge data', {
                  entityId,
                  targetId,
                  targetIdType: typeof targetId,
                });
              }
            });
          }
        } else if (entityType === 'actionableItem') {
          // ActionableItem: assignedTo, relatedEntityIds
          const assignedTo = entityData.assignedTo;
          if (
            assignedTo &&
            typeof assignedTo === 'string' &&
            assignedTo !== 'undefined' &&
            assignedTo !== entityId
          ) {
            edgesList.push({
              id: `${assignedTo}-${entityId}-assigned`,
              source: assignedTo,
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
          } else if (assignedTo) {
            console.warn('[ModelGraph] Invalid actionableItem assignedTo edge data', {
              entityId,
              assignedTo,
              assignedToType: typeof assignedTo,
            });
          }
          if (entityData.relatedEntityIds && Array.isArray(entityData.relatedEntityIds)) {
            entityData.relatedEntityIds.forEach((relatedId: string, idx: number) => {
              if (
                relatedId &&
                typeof relatedId === 'string' &&
                relatedId !== 'undefined' &&
                relatedId !== entityId
              ) {
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
              } else if (relatedId) {
                console.warn('[ModelGraph] Invalid actionableItem relatedEntityIds edge data', {
                  entityId,
                  relatedId,
                  relatedIdType: typeof relatedId,
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
    const finalNodes = Array.from(nodesMap.values()).map((item) => item.node);
    const nodeIds = new Set(finalNodes.map((n) => n.id));

    // Теперь обрабатываем relations - создаем edges для них после того, как все ноды созданы
    sortedInteractions.forEach((interaction) => {
      try {
        if (!interaction.content) return;
        
        let entityData: any = {};
        try {
          entityData = JSON.parse(interaction.content);
        } catch {
          return;
        }
        
        // Пропускаем удаленные
        if (entityData.status === 'deleted' || entityData.status === 'archived') {
          return;
        }
        
        const entityId = entityData.id || interaction.ontologyEntityId || interaction.id;
        if (!entityId) return;
        
        const entityType = normalizeEntityType(interaction.subkind, entityData);
        
        // Обрабатываем relations - создаем edges
        if (entityType === 'relation') {
          const subjectId = entityData.subjectId;
          const objectId = entityData.objectId;
          
          // Проверяем, что обе ноды существуют
          if (
            subjectId &&
            objectId &&
            typeof subjectId === 'string' &&
            typeof objectId === 'string' &&
            subjectId !== 'undefined' &&
            objectId !== 'undefined' &&
            subjectId !== objectId &&
            nodeIds.has(subjectId) &&
            nodeIds.has(objectId)
          ) {
            // Создаем edge с явным указанием handle ids
            const edge = {
              id: `${entityId}-relation-edge`,
              source: subjectId,
              target: objectId,
              sourceHandle: 'source', // ID handle на source ноде
              targetHandle: 'target', // ID handle на target ноде
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
            };
            
            edgesList.push(edge);
            
            console.log('[ModelGraph] Created relation edge', {
              edgeId: edge.id,
              source: subjectId,
              target: objectId,
              predicate: entityData.predicate,
              sourceExists: nodeIds.has(subjectId),
              targetExists: nodeIds.has(objectId),
            });
          } else {
            console.warn('[ModelGraph] Cannot create relation edge - missing nodes or invalid data', {
              entityId,
              subjectId,
              objectId,
              subjectExists: subjectId ? nodeIds.has(subjectId) : false,
              objectExists: objectId ? nodeIds.has(objectId) : false,
            });
          }
        }
      } catch (err) {
        console.warn('[ModelGraph] Failed to process relation for edge creation', interaction.id, err);
      }
    });

    // Фильтруем рёбра: оставляем только те, где и source, и target существуют в nodesMap
    const validEdges = edgesList.filter((edge) => {
      const sourceExists = nodeIds.has(edge.source);
      const targetExists = nodeIds.has(edge.target);
      
      if (!sourceExists || !targetExists) {
        console.warn(
          '[ModelGraph] Skipping edge with missing node(s)',
          {
            edgeId: edge.id,
            source: edge.source,
            sourceExists,
            target: edge.target,
            targetExists,
          }
        );
        return false;
      }
      
      return true;
    });

    console.log('[ModelGraph] Processed graph data:', {
      nodesCount: finalNodes.length,
      edgesCount: validEdges.length,
      edgesListCount: edgesList.length,
      nodeIds: finalNodes.map(n => n.id),
      edgeSources: validEdges.map(e => ({ 
        id: e.id, 
        source: e.source, 
        target: e.target,
        sourceExists: nodeIds.has(e.source),
        targetExists: nodeIds.has(e.target),
      })),
      filteredOutEdges: edgesList.length - validEdges.length,
    });

    return {
      nodes: finalNodes,
      edges: validEdges,
    };
  }, [ontologyInteractions, theme]);

  const [reactFlowNodes, setNodes, onNodesChange] = useNodesState(nodes);
  const [reactFlowEdges, setEdges, onEdgesChange] = useEdgesState(edges);
  
  // Логируем состояние ReactFlow
  React.useEffect(() => {
    console.log('[ModelGraph] ReactFlow state:', {
      reactFlowNodesCount: reactFlowNodes.length,
      reactFlowEdgesCount: reactFlowEdges.length,
      nodes: reactFlowNodes.map(n => ({ id: n.id, type: n.type, hasData: !!n.data })),
    });
  }, [reactFlowNodes, reactFlowEdges]);

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

  // Обработчик для кнопки Align - выравнивание нод
  const handleAlign = useCallback(() => {
    console.log('[ModelGraph] Align button clicked');
    
    // Получаем текущие ноды и edges
    const currentNodes = reactFlowNodes;
    const currentEdges = reactFlowEdges;
    
    if (currentNodes.length === 0) {
      console.warn('[ModelGraph] Align: No nodes to align');
      return;
    }
    
    // Создаем Set связанных нод (ноды, которые имеют хотя бы один edge)
    const connectedNodeIds = new Set<string>();
    currentEdges.forEach((edge) => {
      connectedNodeIds.add(edge.source);
      connectedNodeIds.add(edge.target);
    });
    
    // Разделяем ноды на связанные и несвязанные
    const connectedNodes: Node[] = [];
    const unconnectedNodes: Node[] = [];
    
    currentNodes.forEach((node) => {
      if (connectedNodeIds.has(node.id)) {
        connectedNodes.push(node);
      } else {
        unconnectedNodes.push(node);
      }
    });
    
    console.log('[ModelGraph] Align: nodes distribution', {
      total: currentNodes.length,
      connected: connectedNodes.length,
      unconnected: unconnectedNodes.length,
    });
    
    // Параметры размещения
    const nodeWidth = 150; // Примерная ширина ноды
    const nodeHeight = 100; // Примерная высота ноды
    const padding = 50; // Отступ от краев
    const nodeSpacing = 20; // Расстояние между нодами
    const horizontalGap = 200; // Расстояние между колонками (sources, intermediate, sinks)
    
    // Вычисляем размеры области графа (используем примерные значения, можно улучшить)
    const graphWidth = 1200;
    const graphHeight = 800;
    
    const updatedNodes = currentNodes.map((node) => {
      const isConnected = connectedNodeIds.has(node.id);
      
      if (isConnected) {
        // Для связанных нод определяем их роль в графе
        // Источники (sources) - ноды без входящих edges
        // Стоки (sinks) - ноды без исходящих edges
        // Промежуточные - остальные
        
        const hasIncoming = currentEdges.some((edge) => edge.target === node.id);
        const hasOutgoing = currentEdges.some((edge) => edge.source === node.id);
        
        let nodeCategory: 'source' | 'sink' | 'intermediate';
        if (!hasIncoming && hasOutgoing) {
          nodeCategory = 'source'; // Источник - только исходящие связи
        } else if (hasIncoming && !hasOutgoing) {
          nodeCategory = 'sink'; // Сток - только входящие связи
        } else {
          nodeCategory = 'intermediate'; // Промежуточная - и входящие, и исходящие
        }
        
        // Группируем ноды по категориям
        const sourceNodes = connectedNodes.filter((n) => {
          const hasIn = currentEdges.some((e) => e.target === n.id);
          const hasOut = currentEdges.some((e) => e.source === n.id);
          return !hasIn && hasOut;
        });
        
        const sinkNodes = connectedNodes.filter((n) => {
          const hasIn = currentEdges.some((e) => e.target === n.id);
          const hasOut = currentEdges.some((e) => e.source === n.id);
          return hasIn && !hasOut;
        });
        
        const intermediateNodes = connectedNodes.filter((n) => {
          const hasIn = currentEdges.some((e) => e.target === n.id);
          const hasOut = currentEdges.some((e) => e.source === n.id);
          return hasIn && hasOut;
        });
        
        // Определяем позицию ноды в зависимости от категории
        let x: number;
        let y: number;
        const startY = padding;
        
        if (nodeCategory === 'source') {
          // Источники - слева
          const index = sourceNodes.findIndex((n) => n.id === node.id);
          const cols = Math.max(1, Math.ceil(Math.sqrt(sourceNodes.length)));
          const rows = Math.ceil(sourceNodes.length / cols);
          const row = Math.floor(index / cols);
          const col = index % cols;
          
          const areaWidth = cols * (nodeWidth + nodeSpacing) - nodeSpacing;
          const startX = padding;
          
          x = startX + col * (nodeWidth + nodeSpacing);
          y = startY + row * (nodeHeight + nodeSpacing);
          
        } else if (nodeCategory === 'sink') {
          // Стоки - справа
          const index = sinkNodes.findIndex((n) => n.id === node.id);
          const cols = Math.max(1, Math.ceil(Math.sqrt(sinkNodes.length)));
          const rows = Math.ceil(sinkNodes.length / cols);
          const row = Math.floor(index / cols);
          const col = index % cols;
          
          const areaWidth = cols * (nodeWidth + nodeSpacing) - nodeSpacing;
          const startX = graphWidth - padding - areaWidth;
          
          x = startX + col * (nodeWidth + nodeSpacing);
          y = startY + row * (nodeHeight + nodeSpacing);
          
        } else {
          // Промежуточные - в центре
          const index = intermediateNodes.findIndex((n) => n.id === node.id);
          const cols = Math.max(1, Math.ceil(Math.sqrt(intermediateNodes.length)));
          const rows = Math.ceil(intermediateNodes.length / cols);
          const row = Math.floor(index / cols);
          const col = index % cols;
          
          const areaWidth = cols * (nodeWidth + nodeSpacing) - nodeSpacing;
          const startX = (graphWidth - areaWidth) / 2;
          
          x = startX + col * (nodeWidth + nodeSpacing);
          y = startY + row * (nodeHeight + nodeSpacing);
        }
        
        return {
          ...node,
          position: { x, y },
        };
      } else {
        // Размещаем несвязанные ноды снизу
        const index = unconnectedNodes.findIndex((n) => n.id === node.id);
        const cols = Math.ceil(Math.sqrt(unconnectedNodes.length));
        const rows = Math.ceil(unconnectedNodes.length / cols);
        const row = Math.floor(index / cols);
        const col = index % cols;
        
        const areaWidth = cols * (nodeWidth + nodeSpacing) - nodeSpacing;
        const startX = (graphWidth - areaWidth) / 2;
        // Размещаем снизу с отступом
        const startY = graphHeight - padding - rows * (nodeHeight + nodeSpacing);
        
        return {
          ...node,
          position: {
            x: startX + col * (nodeWidth + nodeSpacing),
            y: startY + row * (nodeHeight + nodeSpacing),
          },
        };
      }
    });
    
    // Обновляем позиции нод
    setNodes(updatedNodes);
    
    // После обновления позиций, вызываем fitView для центрирования
    // Используем setTimeout, чтобы дать React Flow обновиться
    setTimeout(() => {
      try {
        if (reactFlowInstanceRef.current) {
          reactFlowInstanceRef.current.fitView({ padding: 0.1, duration: 300 });
        }
      } catch (err) {
        console.warn('[ModelGraph] Failed to fit view after align', err);
      }
    }, 100);
    
    console.log('[ModelGraph] Align: nodes repositioned', {
      connectedCount: connectedNodes.length,
      unconnectedCount: unconnectedNodes.length,
    });
  }, [reactFlowNodes, reactFlowEdges, setNodes]);

  return (
    <div 
      style={{ width: '100%', height: '100%', background: bgColor, position: 'relative' }}
      className="react-flow-attribution-hidden"
    >
      <style>{`
        .react-flow-attribution-hidden .react-flow__attribution {
          display: none !important;
        }
      `}</style>
      <ReactFlow
        nodes={reactFlowNodes}
        edges={reactFlowEdges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        nodeTypes={STABLE_NODE_TYPES}
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
            const data = node.data as OntologyNodeData;
            return getEntityColor(data.type, theme);
          }}
          maskColor={theme === 'dark' ? 'rgba(0, 0, 0, 0.5)' : 'rgba(255, 255, 255, 0.5)'}
        />
      </ReactFlow>
      
      {/* Кастомная панель инструментов справа */}
      <div
        style={{
          position: 'absolute',
          right: '16px',
          top: '16px',
          display: 'flex',
          flexDirection: 'column',
          gap: '8px',
          zIndex: 5,
        }}
      >
        <div
          style={{
            background: theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
            border: `1px solid ${theme === 'dark' ? 'var(--border-subtle)' : 'var(--border-subtle)'}`,
            borderRadius: '8px',
            padding: '4px',
            display: 'flex',
            flexDirection: 'column',
            gap: '4px',
            boxShadow: theme === 'dark' 
              ? '0 2px 8px rgba(0, 0, 0, 0.3)'
              : '0 2px 8px rgba(0, 0, 0, 0.1)',
          }}
        >
          <button
            onClick={handleAlign}
            title="Align nodes"
            style={{
              width: '32px',
              height: '32px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              background: 'transparent',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
              color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-heading)',
              transition: 'background-color 150ms',
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.backgroundColor = theme === 'dark' 
                ? 'var(--white-10)' 
                : 'var(--white-10)';
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.backgroundColor = 'transparent';
            }}
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M3 3h18v18H3z" />
              <path d="M9 9h6v6H9z" />
              <path d="M3 9h6" />
              <path d="M15 9h6" />
              <path d="M9 3v6" />
              <path d="M9 15v6" />
            </svg>
          </button>
        </div>
      </div>
    </div>
  );
};

