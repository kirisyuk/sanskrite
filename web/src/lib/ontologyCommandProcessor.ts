// Обработчик команд от LLM для работы с онтологией
// Преобразует команды в финальные состояния и сохраняет в interactions

import { api } from './api';
import type { OntologyCommand } from '../types/ontologyCommands';
import type { LocalInteraction } from '../stores/genaiChatStore';
import { useGenaiChatStore } from '../stores/genaiChatStore';

// ID сущностей генерируется автоматически Firestore при создании interaction
// Используем Firestore document ID напрямую (уникальный, без префиксов)

/**
 * Обрабатывает команды от LLM и сохраняет финальные состояния в interactions
 * @param commands - массив команд от LLM
 * @param projectId - ID проекта
 * @returns Promise с массивом созданных/обновленных interaction IDs
 */
export async function processOntologyCommands(
  commands: OntologyCommand[],
  projectId: string
): Promise<string[]> {
  const createdInteractionIds: string[] = [];
  const genaiChatStore = useGenaiChatStore.getState();
  
  // Получаем текущие interactions для поиска существующих сущностей
  const currentInteractions = genaiChatStore.interactions.filter(
    (it) => it.kind === 'ontology'
  );
  
  // Создаем Map для быстрого поиска interactions по entity ID
  const interactionsByEntityId = new Map<string, LocalInteraction>();
  currentInteractions.forEach((it) => {
    try {
      if (it.content) {
        const entityData = JSON.parse(it.content);
        const entityId = entityData.id || it.ontologyEntityId;
        if (entityId) {
          interactionsByEntityId.set(entityId, it);
        }
      }
    } catch {
      // Игнорируем ошибки парсинга
    }
  });
  
  for (const cmd of commands) {
    try {
      if (cmd.operation === 'create') {
        // CREATE: создаем interaction, Firestore генерирует уникальный ID
        if (!cmd.data) {
          console.warn('[processOntologyCommands] Create command missing data', cmd);
          continue;
        }
        
        // Создаем interaction БЕЗ id в content (ID будет добавлен после создания)
        // Firestore автоматически генерирует уникальный document ID
        const interaction = await api.createProjectInteraction(projectId, {
          kind: 'ontology',
          subkind: cmd.type,
          content: JSON.stringify(cmd.data), // Без id, добавим после создания
        });
        
        // Используем interaction.id как entity ID (уникальный Firestore document ID)
        const entityId = interaction.id;
        
        // Обновляем content с правильным ID сущности
        const entityData = {
          ...cmd.data,
          id: entityId,
        };
        
        // Обновляем interaction с финальным состоянием (с ID)
        await api.updateProjectInteraction(projectId, interaction.id, {
          content: JSON.stringify(entityData),
          ontologyEntityId: entityId,
        });
        
        createdInteractionIds.push(interaction.id);
        
        // Обновляем локальное состояние
        genaiChatStore.setState((s) => ({
          interactions: [
            ...s.interactions,
            {
              id: interaction.id,
              projectId,
              kind: 'ontology',
              subkind: cmd.type,
              content: JSON.stringify(entityData),
              ontologyEntityId: entityId,
              createdAt: new Date().toISOString(),
            } as LocalInteraction,
          ],
        }));
        
      } else if (cmd.operation === 'update') {
        // UPDATE: находим существующий interaction и обновляем его content
        if (!cmd.id) {
          console.warn('[processOntologyCommands] Update command missing id', cmd);
          continue;
        }
        
        if (!cmd.data) {
          console.warn('[processOntologyCommands] Update command missing data', cmd);
          continue;
        }
        
        const existingInteraction = interactionsByEntityId.get(cmd.id);
        
        if (!existingInteraction) {
          console.warn('[processOntologyCommands] Update command: entity not found', cmd.id);
          continue;
        }
        
        // Обновляем данные сущности (сохраняем ID)
        const entityData = {
          ...cmd.data,
          id: cmd.id, // Сохраняем существующий ID
        };
        
        // Обновляем interaction через API
        await api.updateProjectInteraction(projectId, existingInteraction.id, {
          content: JSON.stringify(entityData),
          subkind: cmd.type, // Может измениться тип
        });
        
        createdInteractionIds.push(existingInteraction.id);
        
        // Обновляем локальное состояние
        genaiChatStore.setState((s) => ({
          interactions: s.interactions.map((it) =>
            it.id === existingInteraction.id
              ? {
                  ...it,
                  subkind: cmd.type,
                  content: JSON.stringify(entityData),
                  ontologyEntityId: cmd.id,
                }
              : it
          ),
        }));
        
      } else if (cmd.operation === 'delete') {
        // DELETE: находим interaction и помечаем как удаленный или удаляем физически
        if (!cmd.id) {
          console.warn('[processOntologyCommands] Delete command missing id', cmd);
          continue;
        }
        
        const existingInteraction = interactionsByEntityId.get(cmd.id);
        
        if (!existingInteraction) {
          console.warn('[processOntologyCommands] Delete command: entity not found', cmd.id);
          continue;
        }
        
        // Вариант 1: Помечаем как удаленный (обновляем content с status='deleted')
        try {
          const entityData = JSON.parse(existingInteraction.content || '{}');
          entityData.status = 'deleted';
          
          await api.updateProjectInteraction(projectId, existingInteraction.id, {
            content: JSON.stringify(entityData),
          });
          
          // Обновляем локальное состояние
          genaiChatStore.setState((s) => ({
            interactions: s.interactions.map((it) =>
              it.id === existingInteraction.id
                ? {
                    ...it,
                    content: JSON.stringify(entityData),
                  }
                : it
            ),
          }));
        } catch {
          // Вариант 2: Если не удалось обновить, удаляем физически
          await api.deleteProjectInteraction(projectId, existingInteraction.id);
          
          // Удаляем из локального состояния
          genaiChatStore.setState((s) => ({
            interactions: s.interactions.filter((it) => it.id !== existingInteraction.id),
          }));
        }
      }
    } catch (err) {
      console.error('[processOntologyCommands] Failed to process command', cmd, err);
    }
  }
  
  return createdInteractionIds;
}

