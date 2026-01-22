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
import { nodeTypes } from './GraphNodeTypes';
import { api } from '../lib/api';

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
export const useGraphHasNodes = (): boolean => {
  const interactions = useGenaiChatStore((s) => s.interactions);
  
  return useMemo(() => {
    const ontologyInteractions = interactions.filter((it) => {
      return it.kind === 'ontology';
    });
    
    // Проверяем, есть ли хотя бы одна interaction с валидными данными
    // Исключаем удаленные сущности (status='deleted')
    const hasNodes = ontologyInteractions.some((it) => {
      if (!it.content) {
        return false;
      }
      try {
        const entityData = JSON.parse(it.content);
        const isValid = entityData.status !== 'deleted' && entityData.status !== 'archived';
        return isValid;
      } catch (err) {
        // Failed to parse - skip this interaction
        return false;
      }
    });
    
    return hasNodes;
  }, [interactions]);
};

export const Graph: React.FC = () => {
  const { theme } = React.useContext(ThemeContext);
  const interactions = useGenaiChatStore((s) => s.interactions);
  const projectId = useGenaiChatStore((s) => s.projectId);
  const reactFlowInstanceRef = useRef<ReactFlowInstance | null>(null);
  
  // Храним предыдущие ноды для сохранения позиций
  const previousNodesRef = useRef<Map<string, Node>>(new Map());

  // Фильтруем interactions для онтологических сущностей
  // Используем kind='ontology' с subkind для типа сущности
  const ontologyInteractions = useMemo(() => {
    const filtered = interactions.filter((it) => {
      return it.kind === 'ontology';
    });
    
    console.log('[Graph] Ontology interactions:', {
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

  // Загружаем layout из отдельного interaction (должен быть только один)
  // Если их несколько (из-за ошибок), берем самый новый
  const layoutInteraction = useMemo(() => {
    const layouts = ontologyInteractions.filter(
      (it) => it.kind === 'ontology' && it.subkind === 'layout'
    );
    
    if (layouts.length === 0) {
      return undefined;
    }
    
    // Если несколько layout interactions, берем самый новый
    if (layouts.length > 1) {
      console.warn('[Graph] Found multiple layout interactions, using the newest one', {
        count: layouts.length,
        ids: layouts.map(l => l.id),
      });
      // Сортируем по времени создания (новый первый) и берем первый
      layouts.sort((a, b) => {
        const timeA = new Date(a.createdAt || 0).getTime();
        const timeB = new Date(b.createdAt || 0).getTime();
        return timeB - timeA; // Новый первый
      });
    }
    
    return layouts[0];
  }, [ontologyInteractions]);

  const savedNodePositions = useMemo(() => {
    if (!layoutInteraction?.content) {
      return new Map<string, { x: number; y: number }>();
    }
    
    try {
      const layoutData = JSON.parse(layoutInteraction.content);
      const positions = new Map<string, { x: number; y: number }>();
      
      if (layoutData.positions && typeof layoutData.positions === 'object') {
        Object.entries(layoutData.positions).forEach(([nodeId, pos]: [string, any]) => {
          if (pos && typeof pos.x === 'number' && typeof pos.y === 'number') {
            positions.set(nodeId, { x: pos.x, y: pos.y });
          }
        });
      }
      
      return positions;
    } catch (err) {
      console.warn('[Graph] Failed to parse layout interaction', err);
      return new Map<string, { x: number; y: number }>();
    }
  }, [layoutInteraction]);

  // Преобразуем interactions в ноды и связи для ReactFlow
  // Interactions хранят финальные состояния сущностей (не операции)
  // Фильтруем только финальные состояния - берем последний interaction для каждой сущности
  const { nodes, edges } = useMemo(() => {
    const nodesMap = new Map<string, { node: Node; createdAt: string }>();
    const edgesList: Edge[] = [];

    // Фильтруем layout interactions - они не являются сущностями
    const entityInteractions = ontologyInteractions.filter(
      (it) => !(it.kind === 'ontology' && it.subkind === 'layout')
    );

    // Группируем interactions по entityId и берем только последний (финальное состояние)
    const interactionsByEntityId = new Map<string, typeof entityInteractions[0]>();
    
    entityInteractions.forEach((interaction) => {
      try {
        let entityData: any = {};
        if (interaction.content) {
          try {
            entityData = JSON.parse(interaction.content);
          } catch {
            return; // Пропускаем некорректные
          }
        }
        
        const entityId = entityData.id || interaction.ontologyEntityId || interaction.id;
        if (!entityId) return;
        
        // Берем последний interaction для каждой сущности (финальное состояние)
        const existing = interactionsByEntityId.get(entityId);
        if (!existing) {
          interactionsByEntityId.set(entityId, interaction);
        } else {
          // Сравниваем по времени - берем более новый
          const existingTime = new Date(existing.createdAt || 0).getTime();
          const currentTime = new Date(interaction.createdAt || 0).getTime();
          if (currentTime > existingTime) {
            interactionsByEntityId.set(entityId, interaction);
          }
        }
      } catch (err) {
        // Пропускаем некорректные interactions
      }
    });

    // Сортируем финальные interactions по createdAt для обработки
    const finalInteractions = Array.from(interactionsByEntityId.values()).sort((a, b) => {
      const timeA = new Date(a.createdAt || 0).getTime();
      const timeB = new Date(b.createdAt || 0).getTime();
      return timeA - timeB;
    });

    finalInteractions.forEach((interaction) => {
      try {
        // Парсим content как JSON - содержит финальное состояние сущности
        let entityData: any = {};
        if (interaction.content) {
          try {
            entityData = JSON.parse(interaction.content);
          } catch {
            // Если не JSON, пропускаем этот interaction
            console.warn('[Graph] Failed to parse content as JSON', interaction.id);
            return;
          }
        } else {
          // Нет content - пропускаем
          return;
        }

        // Используем ID сущности из данных (финальное состояние)
        const entityId = entityData.id || interaction.ontologyEntityId || interaction.id;
        if (!entityId) {
          console.warn('[Graph] No entity ID found', interaction.id);
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

        // Создаем или обновляем ноду
        // Используем сохраненную позицию из layout interaction, если есть
        const existing = nodesMap.get(entityId);
        const savedPosition = savedNodePositions.get(entityId);
        const previousNode = previousNodesRef.current.get(entityId);
        const nodePosition = savedPosition || previousNode?.position || existing?.node.position || {
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
              console.warn('[Graph] Invalid claim edge data', {
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
              console.warn('[Graph] Invalid causalLink edge data', {
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
                console.warn('[Graph] Invalid event participant edge data', {
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
            console.warn('[Graph] Invalid metricState edge data', {
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
                console.warn('[Graph] Invalid trend edge data', {
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
                console.warn('[Graph] Invalid scenario edge data', {
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
            console.warn('[Graph] Invalid actionableItem assignedTo edge data', {
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
                console.warn('[Graph] Invalid actionableItem relatedEntityIds edge data', {
                  entityId,
                  relatedId,
                  relatedIdType: typeof relatedId,
                });
              }
            });
          }
        }
      } catch (err) {
        console.warn('[Graph] Failed to parse interaction', interaction.id, err);
      }
    });

    // Преобразуем Map в массив нод (берем только node, без createdAt)
    const finalNodes = Array.from(nodesMap.values()).map((item) => item.node);
    const nodeIds = new Set(finalNodes.map((n) => n.id));

    // Теперь обрабатываем relations - создаем edges для них после того, как все ноды созданы
    finalInteractions.forEach((interaction) => {
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
            
            console.log('[Graph] Created relation edge', {
              edgeId: edge.id,
              source: subjectId,
              target: objectId,
              predicate: entityData.predicate,
              sourceExists: nodeIds.has(subjectId),
              targetExists: nodeIds.has(objectId),
            });
          } else {
            console.warn('[Graph] Cannot create relation edge - missing nodes or invalid data', {
              entityId,
              subjectId,
              objectId,
              subjectExists: subjectId ? nodeIds.has(subjectId) : false,
              objectExists: objectId ? nodeIds.has(objectId) : false,
            });
          }
        }
      } catch (err) {
        console.warn('[Graph] Failed to process relation for edge creation', interaction.id, err);
      }
    });

    // Фильтруем рёбра: оставляем только те, где и source, и target существуют в nodesMap
    const validEdges = edgesList.filter((edge) => {
      const sourceExists = nodeIds.has(edge.source);
      const targetExists = nodeIds.has(edge.target);
      
      if (!sourceExists || !targetExists) {
        console.warn(
          '[Graph] Skipping edge with missing node(s)',
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

    console.log('[Graph] Processed graph data:', {
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
  }, [ontologyInteractions, theme, savedNodePositions]);

  const [reactFlowNodes, setNodes, onNodesChange] = useNodesState(nodes);
  const [reactFlowEdges, setEdges, onEdgesChange] = useEdgesState(edges);
  
  // Сохраняем текущие ноды в ref для сохранения позиций при следующем обновлении
  React.useEffect(() => {
    const nodesMap = new Map<string, Node>();
    reactFlowNodes.forEach(node => {
      nodesMap.set(node.id, node);
    });
    previousNodesRef.current = nodesMap;
  }, [reactFlowNodes]);

  // Сохраняем позиции нод в layout interaction (debounced, неблокирующее)
  const saveLayoutTimeoutRef = useRef<NodeJS.Timeout | null>(null);
  const isSavingRef = useRef<boolean>(false);

  // Prevent pending debounced save from firing after unmount (can retain large node arrays longer than needed).
  React.useEffect(() => {
    return () => {
      if (saveLayoutTimeoutRef.current) {
        clearTimeout(saveLayoutTimeoutRef.current as any);
        saveLayoutTimeoutRef.current = null;
      }
    };
  }, []);
  
  const saveNodePositions = useCallback((nodes: Node[]) => {
    if (!projectId || projectId === 'local-unfold') {
      return;
    }

    // Отменяем предыдущий таймер
    if (saveLayoutTimeoutRef.current) {
      clearTimeout(saveLayoutTimeoutRef.current);
    }

    // Debounce сохранение на 1 секунду - полностью неблокирующее
    saveLayoutTimeoutRef.current = setTimeout(() => {
      // Выполняем сохранение асинхронно, не блокируя UI
      (async () => {
        // Предотвращаем параллельные сохранения
        if (isSavingRef.current) {
          return;
        }
        
        isSavingRef.current = true;
        
        try {
          const positions: Record<string, { x: number; y: number }> = {};
          nodes.forEach(node => {
            if (node.position) {
              positions[node.id] = { x: node.position.x, y: node.position.y };
            }
          });

          const layoutData = { positions };
          const content = JSON.stringify(layoutData);

          // Проверяем, есть ли уже layout interactions (может быть несколько из-за ошибок)
          const allLayoutInteractions = useGenaiChatStore.getState().interactions.filter(
            (it) => it.kind === 'ontology' && it.subkind === 'layout'
          );
          
          if (allLayoutInteractions.length > 0) {
            // Есть существующие layout interactions - обновляем самый новый
            const latestLayout = allLayoutInteractions.sort((a, b) => {
              const timeA = new Date(a.createdAt || 0).getTime();
              const timeB = new Date(b.createdAt || 0).getTime();
              return timeB - timeA; // Новый первый
            })[0];
            
            // Обновляем самый новый
            await api.updateProjectInteraction(projectId, latestLayout.id, {
              content,
            });
            
            // Если есть старые дубликаты, удаляем их (в фоне, не блокируя)
            if (allLayoutInteractions.length > 1) {
              const oldLayouts = allLayoutInteractions.slice(1);
              // Удаляем старые асинхронно, не ждем завершения
              Promise.all(
                oldLayouts.map(old => 
                  api.deleteProjectInteraction(projectId, old.id).catch(err => {
                    console.warn('[Graph] Failed to delete old layout interaction', old.id, err);
                  })
                )
              ).then(() => {
                // Обновляем локальное состояние после удаления
                useGenaiChatStore.setState((s) => ({
                  interactions: s.interactions.filter(
                    (it) => !(it.kind === 'ontology' && it.subkind === 'layout' && it.id !== latestLayout.id)
                  ),
                }));
              });
            }
            
            // Тихий лог (не показываем пользователю)
            console.log('[Graph] Layout saved', {
              projectId,
              positionsCount: Object.keys(positions).length,
            });
          } else {
            // Создаем новый layout interaction (первый раз)
            // Дополнительная проверка перед созданием (защита от race condition)
            const checkAgain = useGenaiChatStore.getState().interactions.filter(
              (it) => it.kind === 'ontology' && it.subkind === 'layout'
            );
            
            if (checkAgain.length > 0) {
              // Кто-то уже создал layout interaction, обновляем его
              const latestLayout = checkAgain.sort((a, b) => {
                const timeA = new Date(a.createdAt || 0).getTime();
                const timeB = new Date(b.createdAt || 0).getTime();
                return timeB - timeA;
              })[0];
              
              await api.updateProjectInteraction(projectId, latestLayout.id, {
                content,
              });
              console.log('[Graph] Layout updated (found existing during create)', {
                projectId,
                positionsCount: Object.keys(positions).length,
              });
            } else {
              // Создаем новый layout interaction
              const result = await api.createProjectInteraction(projectId, {
                kind: 'ontology',
                subkind: 'layout',
                content,
              });
              // Обновляем локальное состояние после создания (оптимистично)
              useGenaiChatStore.setState({
                interactions: [
                  ...useGenaiChatStore.getState().interactions,
                  {
                    id: result.id,
                    projectId,
                    kind: 'ontology' as const,
                    subkind: 'layout',
                    content,
                    createdAt: new Date().toISOString(),
                  },
                ],
              } as any);
              console.log('[Graph] Layout created', {
                projectId,
                positionsCount: Object.keys(positions).length,
              });
            }
          }
        } catch (err) {
          // Тихая обработка ошибок - не показываем пользователю
          // Позиции останутся в памяти и будут сохранены при следующем изменении
          console.warn('[Graph] Failed to save layout (will retry on next change)', err);
        } finally {
          isSavingRef.current = false;
        }
      })();
    }, 1000);
  }, [projectId, layoutInteraction]);

  // Сохраняем позиции при изменении нод (debounced)
  React.useEffect(() => {
    if (reactFlowNodes.length > 0) {
      saveNodePositions(reactFlowNodes);
    }
    
    return () => {
      if (saveLayoutTimeoutRef.current) {
        clearTimeout(saveLayoutTimeoutRef.current);
      }
    };
  }, [reactFlowNodes, saveNodePositions]);
  
  // Логируем состояние ReactFlow (только при реальных изменениях)
  const previousNodesCountRef = useRef<number>(0);
  const previousEdgesCountRef = useRef<number>(0);
  
  React.useEffect(() => {
    const nodesChanged = reactFlowNodes.length !== previousNodesCountRef.current;
    const edgesChanged = reactFlowEdges.length !== previousEdgesCountRef.current;
    
    if (nodesChanged || edgesChanged) {
      console.log('[Graph] ReactFlow state changed:', {
        reactFlowNodesCount: reactFlowNodes.length,
        reactFlowEdgesCount: reactFlowEdges.length,
        nodesChanged,
        edgesChanged,
      });
      previousNodesCountRef.current = reactFlowNodes.length;
      previousEdgesCountRef.current = reactFlowEdges.length;
    }
  }, [reactFlowNodes, reactFlowEdges]);

  // Обновляем ноды и связи при изменении данных, но только если данные реально изменились
  const previousNodesDataRef = useRef<string>('');
  const previousEdgesDataRef = useRef<string>('');
  
  React.useEffect(() => {
    // Создаем хеш для сравнения нод (без позиций, так как они могут меняться пользователем)
    const nodesHash = nodes
      .map(n => `${n.id}:${JSON.stringify(n.data)}`)
      .sort()
      .join('|');
    
    // Создаем хеш для сравнения рёбер
    const edgesHash = edges
      .map(e => `${e.id}:${e.source}:${e.target}:${e.label || ''}`)
      .sort()
      .join('|');
    
    // Обновляем только если данные реально изменились
    const nodesChanged = nodesHash !== previousNodesDataRef.current;
    const edgesChanged = edgesHash !== previousEdgesDataRef.current;
    
    if (nodesChanged || edgesChanged) {
      console.log('[Graph] Graph data changed, updating ReactFlow:', {
        nodesChanged,
        edgesChanged,
        nodesCount: nodes.length,
        edgesCount: edges.length,
      });
      
      // При обновлении нод сохраняем позиции из текущего состояния ReactFlow
      if (nodesChanged) {
        const nodesWithPreservedPositions = nodes.map(newNode => {
          const existingNode = reactFlowNodes.find(n => n.id === newNode.id);
          if (existingNode) {
            // Сохраняем позицию из текущего состояния, если нода уже существует
            return {
              ...newNode,
              position: existingNode.position,
            };
          }
          return newNode;
        });
        setNodes(nodesWithPreservedPositions);
        previousNodesDataRef.current = nodesHash;
      }
      
      if (edgesChanged) {
        setEdges(edges);
        previousEdgesDataRef.current = edgesHash;
      }
    } else {
      console.log('[Graph] Graph data unchanged, skipping ReactFlow update');
    }
  }, [nodes, edges, setNodes, setEdges, reactFlowNodes]);

  const onConnect = useCallback(
    (params: Connection) => {
      setEdges((eds) => addEdge(params, eds));
    },
    [setEdges]
  );
  
  // Обработчик для кнопки Align - улучшенная иерархическая раскладка графа
  const handleAlign = useCallback(() => {
    console.log('[Graph] Arrange button clicked');
    
    const currentNodes = reactFlowNodes;
    const currentEdges = reactFlowEdges;
    
    if (currentNodes.length === 0) {
      console.warn('[Graph] Arrange: No nodes to arrange');
      return;
    }
    
    // Параметры раскладки
    const nodeWidth = 180;
    const nodeHeight = 120;
    const padding = 120;
    const layerSpacing = 450; // Расстояние между слоями (увеличено для подписей рёбер)
    const nodeSpacing = 100; // Расстояние между нодами в слое (увеличено для подписей рёбер)
    const componentSpacing = 200; // Расстояние между компонентами
    
    // Создаем граф связей
    const nodeMap = new Map<string, Node>();
    const incomingEdges = new Map<string, string[]>(); // target -> [sources]
    const outgoingEdges = new Map<string, string[]>(); // source -> [targets]
    const nodeIds = new Set<string>();
    
    currentNodes.forEach((node) => {
      nodeMap.set(node.id, node);
      nodeIds.add(node.id);
      incomingEdges.set(node.id, []);
      outgoingEdges.set(node.id, []);
    });
    
    currentEdges.forEach((edge) => {
      if (nodeIds.has(edge.source) && nodeIds.has(edge.target)) {
        const sources = incomingEdges.get(edge.target) || [];
        sources.push(edge.source);
        incomingEdges.set(edge.target, sources);
        
        const targets = outgoingEdges.get(edge.source) || [];
        targets.push(edge.target);
        outgoingEdges.set(edge.source, targets);
      }
    });
    
    // Разделяем на связанные компоненты
    const visited = new Set<string>();
    const components: string[][] = [];
    
    const dfs = (nodeId: string, component: string[]) => {
      if (visited.has(nodeId)) return;
      visited.add(nodeId);
      component.push(nodeId);
      
      const neighbors = [
        ...(outgoingEdges.get(nodeId) || []),
        ...(incomingEdges.get(nodeId) || []),
      ];
      
      neighbors.forEach((neighbor) => {
        if (!visited.has(neighbor)) {
          dfs(neighbor, component);
        }
      });
    };
    
    nodeIds.forEach((nodeId) => {
      if (!visited.has(nodeId)) {
        const component: string[] = [];
        dfs(nodeId, component);
        components.push(component);
      }
    });
    
    console.log('[Graph] Arrange: found components', {
      totalNodes: currentNodes.length,
      componentsCount: components.length,
      componentSizes: components.map(c => c.length),
    });
    
    // Функция для топологической сортировки и определения слоев
    const assignLayers = (componentNodeIds: string[]): Map<string, number> => {
      const layerMap = new Map<string, number>();
      const inDegree = new Map<string, number>();
      
      // Инициализация
      componentNodeIds.forEach((id) => {
        inDegree.set(id, incomingEdges.get(id)?.filter(s => componentNodeIds.includes(s)).length || 0);
      });
      
      // Топологическая сортировка с определением слоев
      const queue: string[] = [];
      componentNodeIds.forEach((id) => {
        if (inDegree.get(id) === 0) {
          queue.push(id);
          layerMap.set(id, 0);
        }
      });
      
      while (queue.length > 0) {
        const current = queue.shift()!;
        const currentLayer = layerMap.get(current) || 0;
        
        const targets = outgoingEdges.get(current) || [];
        targets.forEach((target) => {
          if (!componentNodeIds.includes(target)) return;
          
          const targetInDegree = (inDegree.get(target) || 0) - 1;
          inDegree.set(target, targetInDegree);
          
          if (targetInDegree === 0) {
            // Находим максимальный слой среди всех предшественников
            const predecessors = incomingEdges.get(target) || [];
            let maxPredLayer = -1;
            predecessors.forEach((pred) => {
              if (componentNodeIds.includes(pred)) {
                const predLayer = layerMap.get(pred) || 0;
                maxPredLayer = Math.max(maxPredLayer, predLayer);
              }
            });
            
            layerMap.set(target, maxPredLayer + 1);
            queue.push(target);
          }
        });
      }
      
      // Для узлов с циклами (если остались необработанные)
      componentNodeIds.forEach((id) => {
        if (!layerMap.has(id)) {
          // Находим минимальный слой среди предшественников или ставим 0
          const predecessors = incomingEdges.get(id) || [];
          let minPredLayer = Infinity;
          predecessors.forEach((pred) => {
            if (componentNodeIds.includes(pred) && layerMap.has(pred)) {
              minPredLayer = Math.min(minPredLayer, layerMap.get(pred) || 0);
            }
          });
          layerMap.set(id, minPredLayer === Infinity ? 0 : minPredLayer + 1);
        }
      });
      
      return layerMap;
    };
    
    // Функция для размещения нод в слое
    const positionNodesInLayer = (
      layerNodes: string[],
      layerIndex: number,
      componentStartX: number,
      componentStartY: number
    ): Map<string, { x: number; y: number }> => {
      const positions = new Map<string, { x: number; y: number }>();
      const layerHeight = layerNodes.length * (nodeHeight + nodeSpacing) - nodeSpacing;
      
      layerNodes.forEach((nodeId, index) => {
        const x = componentStartX + layerIndex * layerSpacing;
        const y = componentStartY + (layerHeight / 2) - (layerNodes.length - 1 - index) * (nodeHeight + nodeSpacing) + (nodeHeight / 2);
        positions.set(nodeId, { x, y });
      });
      
      return positions;
    };
    
    // Размещаем каждый компонент
    const nodePositions = new Map<string, { x: number; y: number }>();
    let currentY = padding;
    
    components.forEach((component, compIndex) => {
      if (component.length === 0) return;
      
      // Для одиночных нод используем простую раскладку
      if (component.length === 1) {
        const nodeId = component[0];
        const cols = Math.ceil(Math.sqrt(components.length));
        const col = compIndex % cols;
        const row = Math.floor(compIndex / cols);
        const areaWidth = cols * (nodeWidth + nodeSpacing * 2) - nodeSpacing * 2;
        const graphWidth = Math.max(1600, 1200); // Увеличена минимальная ширина
        const startX = padding + (graphWidth - padding * 2 - areaWidth) / 2;
        
        nodePositions.set(nodeId, {
          x: startX + col * (nodeWidth + nodeSpacing * 2),
          y: currentY + row * (nodeHeight + nodeSpacing * 2),
        });
        return;
      }
      
      // Для связанных компонентов используем иерархическую раскладку
      const layerMap = assignLayers(component);
      
      // Группируем ноды по слоям
      const layers: string[][] = [];
      let maxLayer = -1;
      layerMap.forEach((layer, nodeId) => {
        maxLayer = Math.max(maxLayer, layer);
        while (layers.length <= layer) {
          layers.push([]);
        }
        layers[layer].push(nodeId);
      });
      
      // Сортируем ноды внутри каждого слоя для лучшей визуализации
      layers.forEach((layer) => {
        layer.sort((a, b) => {
          // Сортируем по количеству связей (более связанные - выше)
          const aConnections = (outgoingEdges.get(a)?.length || 0) + (incomingEdges.get(a)?.length || 0);
          const bConnections = (outgoingEdges.get(b)?.length || 0) + (incomingEdges.get(b)?.length || 0);
          return bConnections - aConnections;
        });
      });
      
      // Вычисляем размеры компонента
      const maxLayerHeight = Math.max(...layers.map(l => l.length * (nodeHeight + nodeSpacing) - nodeSpacing));
      const componentWidth = (maxLayer + 1) * layerSpacing;
      const componentHeight = maxLayerHeight;
      
      // Центрируем компонент (используем динамическую ширину)
      const graphWidth = Math.max(1600, componentWidth + padding * 2);
      const componentStartX = padding + (graphWidth - padding * 2 - componentWidth) / 2;
      const componentStartY = currentY;
      
      // Размещаем ноды
      layers.forEach((layerNodes, layerIndex) => {
        const layerPositions = positionNodesInLayer(
          layerNodes,
          layerIndex,
          componentStartX,
          componentStartY
        );
        layerPositions.forEach((pos, nodeId) => {
          nodePositions.set(nodeId, pos);
        });
      });
      
      // Обновляем текущую Y позицию для следующего компонента
      currentY += componentHeight + componentSpacing;
    });
    
    // Применяем позиции к нодам
    const updatedNodes = currentNodes.map((node) => {
      const position = nodePositions.get(node.id);
      if (position) {
        return {
          ...node,
          position,
        };
      }
      // Fallback для нод без позиции (не должно происходить)
      return {
        ...node,
        position: {
          x: Math.random() * 800,
          y: Math.random() * 600,
        },
      };
    });
    
    setNodes(updatedNodes);
    
    // Центрируем вид после раскладки
    setTimeout(() => {
      try {
        if (reactFlowInstanceRef.current) {
          reactFlowInstanceRef.current.fitView({ padding: 0.2, duration: 500 });
        }
      } catch (err) {
        console.warn('[Graph] Failed to fit view after arrange', err);
      }
    }, 150);
    
    console.log('[Graph] Arrange: nodes repositioned', {
      totalNodes: updatedNodes.length,
      components: components.length,
      layers: components.map(c => {
        const layerMap = assignLayers(c);
        return Math.max(...Array.from(layerMap.values()), -1) + 1;
      }),
    });
  }, [reactFlowNodes, reactFlowEdges, setNodes]);
  
  // Цвета для фона графа
  const bgColor = theme === 'dark' ? 'var(--bg-body)' : 'var(--bg-body)';
  const bgPatternColor = theme === 'dark' ? 'var(--overlay-line-90)' : 'var(--overlay-line-90)';

  // Empty state - показываем только если нет нод
  if (reactFlowNodes.length === 0) {
    return null; // Возвращаем null, чтобы родительский компонент мог показать empty state
  }

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

