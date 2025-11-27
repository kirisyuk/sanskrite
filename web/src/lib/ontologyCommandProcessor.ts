// Обработчик команд от LLM для работы с онтологией
// Преобразует команды в финальные состояния и сохраняет в interactions

import { api } from './api';
import type { OntologyCommand } from '../types/ontologyCommands';
import { validateOntologyCommand } from '../types/ontologyCommands';
import type { LocalInteraction } from '../stores/genaiChatStore';
import { useGenaiChatStore } from '../stores/genaiChatStore';

// ID сущностей генерируется автоматически Firestore при создании interaction
// Используем Firestore document ID напрямую (уникальный, без префиксов)

/**
 * Обрабатывает команды от LLM и сохраняет финальные состояния в interactions
 * 
 * Логика обработки:
 * 1. Разделяем команды на объекты (entities, events, concepts и т.д.) и связи (relations, claims, causalLinks)
 * 2. Сначала создаем все объекты и строим маппинг временных ID (от LLM) -> реальных ID (Firestore)
 * 3. Затем обрабатываем связи, заменяя временные ID на реальные
 * 
 * @param commands - массив команд от LLM
 * @param projectId - ID проекта
 * @param onProgress - callback для обновления прогресса обработки
 * @returns Promise с массивом созданных/обновленных interaction IDs
 */
export async function processOntologyCommands(
  commands: OntologyCommand[],
  projectId: string,
  onProgress?: (progress: {
    stage: 'parsing' | 'validating' | 'creating' | 'updating' | 'deleting' | 'linking' | 'complete';
    processed: number;
    total: number;
    currentOperation?: string;
  }) => void
): Promise<{ success: boolean; createdIds: string[]; errors: Array<{ command: OntologyCommand; error: string }> }> {
  const createdInteractionIds: string[] = [];
  
  // Обновляем прогресс - начало валидации
  onProgress?.({
    stage: 'validating',
    processed: 0,
    total: commands.length,
    currentOperation: 'Validating commands...',
  });
  
  // Валидация команд перед обработкой
  const validationErrors: Array<{ command: OntologyCommand; error: string }> = [];
  const validCommands: OntologyCommand[] = [];
  
  for (let i = 0; i < commands.length; i++) {
    const cmd = commands[i];
    if (!validateOntologyCommand(cmd)) {
      validationErrors.push({
        command: cmd,
        error: 'Invalid command structure',
      });
      continue;
    }
    
    // Дополнительная валидация содержимого data
    if ((cmd.operation === 'create' || cmd.operation === 'update') && cmd.data) {
      // Проверяем обязательные поля в зависимости от типа
      if (cmd.type === 'entity' && !cmd.data.canonicalName) {
        validationErrors.push({
          command: cmd,
          error: 'Entity missing required field: canonicalName',
        });
        continue;
      }
      if (cmd.type === 'event' && (!cmd.data.type || !cmd.data.title)) {
        validationErrors.push({
          command: cmd,
          error: 'Event missing required fields: type or title',
        });
        continue;
      }
      if (cmd.type === 'relation' && (!cmd.data.subjectId || !cmd.data.objectId || !cmd.data.predicate)) {
        validationErrors.push({
          command: cmd,
          error: 'Relation missing required fields: subjectId, objectId, or predicate',
        });
        continue;
      }
      if (cmd.type === 'claim' && (!cmd.data.subjectRef || !cmd.data.predicate || !cmd.data.objectRef)) {
        validationErrors.push({
          command: cmd,
          error: 'Claim missing required fields: subjectRef, predicate, or objectRef',
        });
        continue;
      }
    }
    
    validCommands.push(cmd);
    
    // Обновляем прогресс валидации
    onProgress?.({
      stage: 'validating',
      processed: i + 1,
      total: commands.length,
      currentOperation: `Validating command ${i + 1}/${commands.length}...`,
    });
  }
  
  if (validationErrors.length > 0) {
    console.warn('[processOntologyCommands] Some commands failed validation', {
      totalCommands: commands.length,
      validationErrors: validationErrors.length,
      errors: validationErrors,
    });
  }
  
  if (validCommands.length === 0) {
    // Нет валидных команд для обработки
    const errorMessage = validationErrors.length > 0
      ? `All ${commands.length} commands failed validation. Check console for details.`
      : 'No valid commands to process.';
    
    useGenaiChatStore.setState((s) => {
      const existingError = s.error;
      const newError = existingError 
        ? `${existingError}. ${errorMessage}`
        : errorMessage;
      return { error: newError } as any;
    });
    
    return {
      success: false,
      createdIds: [],
      errors: validationErrors,
    };
  }
  
  // Получаем текущие interactions для поиска существующих сущностей
  const currentInteractions = useGenaiChatStore.getState().interactions.filter(
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
  
  // Маппинг временных ID (от LLM) -> реальных ID (Firestore)
  // Ключ: временный ID из команды LLM (например, "entity-id-for-ИП БАЙМУРАТОВ К.")
  // Значение: реальный Firestore document ID
  const tempIdToRealId = new Map<string, string>();
  
  // Типы, которые являются связями (требуют ссылок на другие объекты)
  const relationTypes = new Set(['relation', 'claim', 'causalLink']);
  
  // Разделяем команды на объекты и связи
  const objectCommands: OntologyCommand[] = [];
  const relationCommands: OntologyCommand[] = [];
  
  for (const cmd of validCommands) {
    if (cmd.operation === 'create' && relationTypes.has(cmd.type)) {
      relationCommands.push(cmd);
    } else {
      objectCommands.push(cmd);
    }
  }
  
  console.log('[processOntologyCommands] Separated commands', {
    total: commands.length,
    objects: objectCommands.length,
    relations: relationCommands.length,
  });
  
  const errors: Array<{ command: OntologyCommand; error: Error }> = [];
  
  // ШАГ 1: Обрабатываем все объекты (entities, events, concepts и т.д.)
  // Строим маппинг временных ID -> реальных ID
  let processedObjects = 0;
  for (const cmd of objectCommands) {
    try {
      if (cmd.operation === 'create') {
        // Обновляем прогресс - создание
        onProgress?.({
          stage: 'creating',
          processed: processedObjects,
          total: objectCommands.length,
          currentOperation: `Creating ${cmd.type}...`,
        });
        
        // CREATE: создаем interaction, Firestore генерирует уникальный ID
        if (!cmd.data) {
          console.warn('[processOntologyCommands] Create command missing data', cmd);
          errors.push({ command: cmd, error: new Error('Create command missing data') });
          processedObjects++;
          continue;
        }
        
        // Извлекаем временный ID из data (если LLM его предложил)
        // LLM может предложить ID вида "entity-id-for-ИП БАЙМУРАТОВ К." или просто использовать id из data
        const tempId = cmd.data?.id;
        
        // Создаем interaction БЕЗ id в content (ID будет добавлен после создания)
        // Firestore автоматически генерирует уникальный document ID
        let interaction;
        try {
          // Убираем временный ID из data перед созданием (если есть)
          const dataWithoutId = { ...cmd.data };
          delete dataWithoutId.id;
          
          interaction = await api.createProjectInteraction(projectId, {
            kind: 'ontology',
            subkind: cmd.type,
            content: JSON.stringify(dataWithoutId), // Без id, добавим после создания
          });
        } catch (err) {
          console.error('[processOntologyCommands] Failed to create interaction on server', cmd, err);
          errors.push({ command: cmd, error: err instanceof Error ? err : new Error(String(err)) });
          processedObjects++;
          continue; // Не обновляем локальное состояние, если создание на сервере не удалось
        }
        
        // Используем interaction.id как entity ID (уникальный Firestore document ID)
        const entityId = interaction.id;
        
        // Сохраняем маппинг временного ID -> реального ID
        if (tempId && tempId !== entityId) {
          tempIdToRealId.set(tempId, entityId);
          console.log('[processOntologyCommands] Mapped temp ID to real ID', {
            tempId,
            realId: entityId,
            type: cmd.type,
          });
        }
        
        // Обновляем content с правильным ID сущности (пока без разрешения ссылок)
        // Ссылки будут разрешены во втором проходе после создания всех объектов
        const entityData = {
          ...cmd.data,
          id: entityId, // Используем реальный Firestore ID
        };
        
        // Обновляем interaction с финальным состоянием (с ID, но пока без разрешенных ссылок)
        try {
          const updateResult = await api.updateProjectInteraction(projectId, interaction.id, {
            content: JSON.stringify(entityData),
            ontologyEntityId: entityId,
          });
          
          if (!updateResult.success) {
            throw new Error('Update returned success: false');
          }
        } catch (err) {
          console.error('[processOntologyCommands] Failed to update interaction on server after create', {
            interactionId: interaction.id,
            entityId,
            error: err,
          });
          // КРИТИЧНО: Если update не удался, у нас есть interaction без правильного ID в content
          // Попробуем удалить созданный interaction, чтобы не оставлять неполное состояние
          try {
            await api.deleteProjectInteraction(projectId, interaction.id);
            console.warn('[processOntologyCommands] Deleted incomplete interaction after failed update', interaction.id);
          } catch (deleteErr) {
            console.error('[processOntologyCommands] Failed to cleanup incomplete interaction', interaction.id, deleteErr);
          }
          errors.push({ command: cmd, error: err instanceof Error ? err : new Error(String(err)) });
          continue; // Не обновляем локальное состояние
        }
        
        createdInteractionIds.push(interaction.id);
        processedObjects++;
        
        // Обновляем прогресс после успешного создания
        onProgress?.({
          stage: 'creating',
          processed: processedObjects,
          total: objectCommands.length,
          currentOperation: `Created ${cmd.type}`,
        });
        
        // Обновляем локальное состояние только после успешного сохранения на сервере
        useGenaiChatStore.setState((s) => ({
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
        // Обновляем прогресс - обновление
        onProgress?.({
          stage: 'updating',
          processed: processedObjects,
          total: objectCommands.length,
          currentOperation: `Updating ${cmd.type}...`,
        });
        
        // UPDATE: находим существующий interaction и обновляем его content
        if (!cmd.id) {
          console.warn('[processOntologyCommands] Update command missing id', cmd);
          errors.push({ command: cmd, error: new Error('Update command missing id') });
          processedObjects++;
          continue;
        }
        
        if (!cmd.data) {
          console.warn('[processOntologyCommands] Update command missing data', cmd);
          errors.push({ command: cmd, error: new Error('Update command missing data') });
          processedObjects++;
          continue;
        }
        
        const existingInteraction = interactionsByEntityId.get(cmd.id);
        
        if (!existingInteraction) {
          console.warn('[processOntologyCommands] Update command: entity not found', cmd.id);
          errors.push({ command: cmd, error: new Error(`Entity not found: ${cmd.id}`) });
          processedObjects++;
          continue;
        }
        
        // Обновляем данные сущности (сохраняем ID)
        const entityData = {
          ...cmd.data,
          id: cmd.id, // Сохраняем существующий ID
        };
        
        // Обновляем interaction через API
        try {
          const updateResult = await api.updateProjectInteraction(projectId, existingInteraction.id, {
            content: JSON.stringify(entityData),
            subkind: cmd.type, // Может измениться тип
          });
          
          if (!updateResult.success) {
            throw new Error('Update returned success: false');
          }
        } catch (err) {
          console.error('[processOntologyCommands] Failed to update interaction on server', {
            interactionId: existingInteraction.id,
            entityId: cmd.id,
            error: err,
          });
          errors.push({ command: cmd, error: err instanceof Error ? err : new Error(String(err)) });
          processedObjects++;
          continue; // Не обновляем локальное состояние, если обновление на сервере не удалось
        }
        
        createdInteractionIds.push(existingInteraction.id);
        processedObjects++;
        
        // Обновляем прогресс после успешного обновления
        onProgress?.({
          stage: 'updating',
          processed: processedObjects,
          total: objectCommands.length,
          currentOperation: `Updated ${cmd.type}`,
        });
        
        // Обновляем локальное состояние только после успешного сохранения на сервере
        useGenaiChatStore.setState((s) => ({
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
        // Обновляем прогресс - удаление
        onProgress?.({
          stage: 'deleting',
          processed: processedObjects,
          total: objectCommands.length,
          currentOperation: `Deleting ${cmd.type}...`,
        });
        
        // DELETE: находим interaction и помечаем как удаленный или удаляем физически
        if (!cmd.id) {
          console.warn('[processOntologyCommands] Delete command missing id', cmd);
          errors.push({ command: cmd, error: new Error('Delete command missing id') });
          processedObjects++;
          continue;
        }
        
        const existingInteraction = interactionsByEntityId.get(cmd.id);
        
        if (!existingInteraction) {
          console.warn('[processOntologyCommands] Delete command: entity not found', cmd.id);
          errors.push({ command: cmd, error: new Error(`Entity not found: ${cmd.id}`) });
          processedObjects++;
          continue;
        }
        
        // Вариант 1: Помечаем как удаленный (обновляем content с status='deleted')
        let deleteSuccess = false;
        try {
          const entityData = JSON.parse(existingInteraction.content || '{}');
          entityData.status = 'deleted';
          
          const updateResult = await api.updateProjectInteraction(projectId, existingInteraction.id, {
            content: JSON.stringify(entityData),
          });
          
          if (updateResult.success) {
            deleteSuccess = true;
            // Обновляем локальное состояние только после успешного сохранения на сервере
            useGenaiChatStore.setState((s) => ({
              interactions: s.interactions.map((it) =>
                it.id === existingInteraction.id
                  ? {
                      ...it,
                      content: JSON.stringify(entityData),
                    }
                  : it
              ),
            }));
          }
        } catch (updateErr) {
          // Вариант 2: Если не удалось обновить, удаляем физически
          try {
            const deleteResult = await api.deleteProjectInteraction(projectId, existingInteraction.id);
            if (deleteResult.success) {
              deleteSuccess = true;
              // Удаляем из локального состояния только после успешного удаления на сервере
              useGenaiChatStore.setState((s) => ({
                interactions: s.interactions.filter((it) => it.id !== existingInteraction.id),
              }));
            }
          } catch (deleteErr) {
            console.error('[processOntologyCommands] Failed to delete interaction on server', {
              interactionId: existingInteraction.id,
              entityId: cmd.id,
              updateError: updateErr,
              deleteError: deleteErr,
            });
            errors.push({ 
              command: cmd, 
              error: deleteErr instanceof Error ? deleteErr : new Error(String(deleteErr)) 
            });
          }
        }
        
        if (!deleteSuccess) {
          // Если ни update, ни delete не удались, добавляем в ошибки
          if (errors.length === 0 || errors[errors.length - 1].command !== cmd) {
            errors.push({ command: cmd, error: new Error('Failed to delete entity') });
          }
        }
        
        processedObjects++;
        
        // Обновляем прогресс после удаления
        onProgress?.({
          stage: 'deleting',
          processed: processedObjects,
          total: objectCommands.length,
          currentOperation: `Deleted ${cmd.type}`,
        });
      }
    } catch (err) {
      console.error('[processOntologyCommands] Unexpected error processing command', cmd, err);
      errors.push({ command: cmd, error: err instanceof Error ? err : new Error(String(err)) });
    }
  }
  
  // ШАГ 1.5: Второй проход - разрешаем ссылки в уже созданных объектах
  // Обновляем объекты, которые ссылаются на другие объекты из этого батча
  const objectsToUpdate = new Map<string, { interactionId: string; data: any; type: string }>();
  
  for (const cmd of objectCommands) {
    if (cmd.operation !== 'create') continue;
    
    try {
      const tempId = cmd.data?.id;
      const realId = tempId ? tempIdToRealId.get(tempId) : null;
      if (!realId) continue; // Объект не был создан или не имел временного ID
      
      // Функция для замены временных ID на реальные
      const resolveId = (id: string | undefined): string | undefined => {
        if (!id) return undefined;
        if (tempIdToRealId.has(id)) {
          return tempIdToRealId.get(id);
        }
        if (interactionsByEntityId.has(id)) {
          return id;
        }
        return id; // Оставляем как есть, если не найден
      };
      
      // Проверяем, есть ли ссылки, которые нужно разрешить
      let needsUpdate = false;
      const resolvedData = JSON.parse(JSON.stringify(cmd.data)); // Глубокая копия
      
      // Для metricState: subjectRef.id
      if (cmd.type === 'metricState' && resolvedData.subjectRef?.id) {
        const resolved = resolveId(resolvedData.subjectRef.id);
        if (resolved && resolved !== resolvedData.subjectRef.id) {
          resolvedData.subjectRef.id = resolved;
          needsUpdate = true;
        }
      }
      
      // Для event: participants[].entityId
      if (cmd.type === 'event' && Array.isArray(resolvedData.participants)) {
        for (const p of resolvedData.participants) {
          if (p.entityId) {
            const resolved = resolveId(p.entityId);
            if (resolved && resolved !== p.entityId) {
              p.entityId = resolved;
              needsUpdate = true;
            }
          }
        }
      }
      
      // Для trend: focus.subjectScope.ids[]
      if (cmd.type === 'trend' && resolvedData.focus?.subjectScope?.ids && Array.isArray(resolvedData.focus.subjectScope.ids)) {
        for (let i = 0; i < resolvedData.focus.subjectScope.ids.length; i++) {
          const resolved = resolveId(resolvedData.focus.subjectScope.ids[i]);
          if (resolved && resolved !== resolvedData.focus.subjectScope.ids[i]) {
            resolvedData.focus.subjectScope.ids[i] = resolved;
            needsUpdate = true;
          }
        }
      }
      
      // Для scenario: impacts[].targetRef.id
      if (cmd.type === 'scenario' && Array.isArray(resolvedData.impacts)) {
        for (const impact of resolvedData.impacts) {
          if (impact.targetRef?.id) {
            const resolved = resolveId(impact.targetRef.id);
            if (resolved && resolved !== impact.targetRef.id) {
              impact.targetRef.id = resolved;
              needsUpdate = true;
            }
          }
        }
      }
      
      // Для actionableItem: assignedTo и relatedEntityIds[]
      if (cmd.type === 'actionableItem') {
        if (resolvedData.assignedTo) {
          const resolved = resolveId(resolvedData.assignedTo);
          if (resolved && resolved !== resolvedData.assignedTo) {
            resolvedData.assignedTo = resolved;
            needsUpdate = true;
          }
        }
        if (Array.isArray(resolvedData.relatedEntityIds)) {
          for (let i = 0; i < resolvedData.relatedEntityIds.length; i++) {
            const resolved = resolveId(resolvedData.relatedEntityIds[i]);
            if (resolved && resolved !== resolvedData.relatedEntityIds[i]) {
              resolvedData.relatedEntityIds[i] = resolved;
              needsUpdate = true;
            }
          }
        }
      }
      
      if (needsUpdate) {
        // Обновляем с реальным ID
        resolvedData.id = realId;
        objectsToUpdate.set(realId, {
          interactionId: realId, // interaction.id == entityId
          data: resolvedData,
          type: cmd.type,
        });
      }
    } catch (err) {
      console.warn('[processOntologyCommands] Failed to prepare object update for link resolution', cmd, err);
    }
  }
  
  // Обновляем объекты с разрешенными ссылками
  for (const [entityId, { interactionId, data, type }] of objectsToUpdate) {
    try {
      const updateResult = await api.updateProjectInteraction(projectId, interactionId, {
        content: JSON.stringify(data),
        subkind: type,
      });
      
      if (updateResult.success) {
        // Обновляем локальное состояние
        useGenaiChatStore.setState((s) => ({
          interactions: s.interactions.map((it) =>
            it.id === interactionId
              ? {
                  ...it,
                  content: JSON.stringify(data),
                }
              : it
          ),
        }));
      }
    } catch (err) {
      console.error('[processOntologyCommands] Failed to update object with resolved links', {
        interactionId,
        entityId,
        error: err,
      });
      errors.push({ 
        command: { operation: 'update', type, id: entityId, data } as OntologyCommand, 
        error: err instanceof Error ? err : new Error(String(err)) 
      });
    }
  }
  
  // ШАГ 2: Обрабатываем связи (relations, claims, causalLinks)
  // Заменяем временные ID на реальные используя маппинг
  let processedRelations = 0;
  const totalCommands = objectCommands.length + relationCommands.length;
  
  // Обновляем прогресс - начало обработки связей
  onProgress?.({
    stage: 'linking',
    processed: objectCommands.length,
    total: totalCommands,
    currentOperation: 'Linking entities...',
  });
  
  for (const cmd of relationCommands) {
    try {
      if (cmd.operation === 'create') {
        if (!cmd.data) {
          console.warn('[processOntologyCommands] Create relation command missing data', cmd);
          errors.push({ command: cmd, error: new Error('Create relation command missing data') });
          continue;
        }
        
        // Функция для замены временных ID на реальные в объекте
        const resolveId = (id: string | undefined): string | undefined => {
          if (!id) return undefined;
          // Сначала проверяем маппинг временных ID (для только что созданных сущностей)
          if (tempIdToRealId.has(id)) {
            return tempIdToRealId.get(id);
          }
          // Затем проверяем существующие interactions (для уже существующих сущностей)
          if (interactionsByEntityId.has(id)) {
            return id; // ID уже реальный и существует
          }
          // Если ID не найден ни в маппинге, ни в существующих interactions - это ошибка
          // LLM должен был использовать реальный ID из контекста или создать сущность сначала
          return undefined; // Возвращаем undefined, чтобы вызвать ошибку валидации
        };
        
        // Заменяем временные ID в data на реальные
        const resolvedData = { ...cmd.data };
        
        // Для relation: subjectId и objectId
        if (cmd.type === 'relation') {
          if (resolvedData.subjectId) {
            const resolved = resolveId(resolvedData.subjectId);
            if (resolved) {
              resolvedData.subjectId = resolved;
            } else {
              console.warn('[processOntologyCommands] Could not resolve subjectId', {
                tempId: resolvedData.subjectId,
                relation: cmd,
              });
              errors.push({ command: cmd, error: new Error(`Could not resolve subjectId: ${resolvedData.subjectId}`) });
              processedRelations++;
              continue;
            }
          }
          if (resolvedData.objectId) {
            const resolved = resolveId(resolvedData.objectId);
            if (resolved) {
              resolvedData.objectId = resolved;
            } else {
              console.warn('[processOntologyCommands] Could not resolve objectId', {
                tempId: resolvedData.objectId,
                relation: cmd,
              });
              errors.push({ command: cmd, error: new Error(`Could not resolve objectId: ${resolvedData.objectId}`) });
              processedRelations++;
              continue;
            }
          }
        }
        
        // Для claim: subjectRef.id и objectRef.id
        if (cmd.type === 'claim') {
          if (resolvedData.subjectRef?.id) {
            const resolved = resolveId(resolvedData.subjectRef.id);
            if (resolved) {
              resolvedData.subjectRef = { ...resolvedData.subjectRef, id: resolved };
            } else {
              console.warn('[processOntologyCommands] Could not resolve claim subjectRef.id', {
                tempId: resolvedData.subjectRef.id,
                relation: cmd,
              });
              errors.push({ command: cmd, error: new Error(`Could not resolve claim subjectRef.id: ${resolvedData.subjectRef.id}`) });
              processedRelations++;
              continue;
            }
          }
          if (resolvedData.objectRef?.id) {
            const resolved = resolveId(resolvedData.objectRef.id);
            if (resolved) {
              resolvedData.objectRef = { ...resolvedData.objectRef, id: resolved };
            } else {
              // objectRef может быть не ID, а значением
              if (typeof resolvedData.objectRef.value === 'string') {
                // Оставляем как есть
              } else {
                console.warn('[processOntologyCommands] Could not resolve claim objectRef.id', {
                  tempId: resolvedData.objectRef.id,
                  relation: cmd,
                });
                errors.push({ command: cmd, error: new Error(`Could not resolve claim objectRef.id: ${resolvedData.objectRef.id}`) });
                processedRelations++;
                continue;
              }
            }
          }
        }
        
        // Для causalLink: causeRef.id и effectRef.id
        if (cmd.type === 'causalLink') {
          if (resolvedData.causeRef?.id) {
            const resolved = resolveId(resolvedData.causeRef.id);
            if (resolved) {
              resolvedData.causeRef = { ...resolvedData.causeRef, id: resolved };
            } else {
              console.warn('[processOntologyCommands] Could not resolve causalLink causeRef.id', {
                tempId: resolvedData.causeRef.id,
                relation: cmd,
              });
              errors.push({ command: cmd, error: new Error(`Could not resolve causalLink causeRef.id: ${resolvedData.causeRef.id}`) });
              processedRelations++;
              continue;
            }
          }
          if (resolvedData.effectRef?.id) {
            const resolved = resolveId(resolvedData.effectRef.id);
            if (resolved) {
              resolvedData.effectRef = { ...resolvedData.effectRef, id: resolved };
            } else {
              console.warn('[processOntologyCommands] Could not resolve causalLink effectRef.id', {
                tempId: resolvedData.effectRef.id,
                relation: cmd,
              });
              errors.push({ command: cmd, error: new Error(`Could not resolve causalLink effectRef.id: ${resolvedData.effectRef.id}`) });
              processedRelations++;
              continue;
            }
          }
        }
        
        // Убираем временный ID из data перед созданием (если есть)
        const tempId = resolvedData.id;
        delete resolvedData.id;
        
        // Создаем interaction для связи
        let interaction;
        try {
          interaction = await api.createProjectInteraction(projectId, {
            kind: 'ontology',
            subkind: cmd.type,
            content: JSON.stringify(resolvedData),
          });
        } catch (err) {
          console.error('[processOntologyCommands] Failed to create relation interaction on server', cmd, err);
          errors.push({ command: cmd, error: err instanceof Error ? err : new Error(String(err)) });
          processedRelations++;
          continue;
        }
        
        const entityId = interaction.id;
        
        // Сохраняем маппинг временного ID связи -> реального ID (если был временный ID)
        if (tempId && tempId !== entityId) {
          tempIdToRealId.set(tempId, entityId);
        }
        
        // Обновляем content с правильным ID
        const entityData = {
          ...resolvedData,
          id: entityId,
        };
        
        // Обновляем interaction с финальным состоянием (с ID)
        try {
          const updateResult = await api.updateProjectInteraction(projectId, interaction.id, {
            content: JSON.stringify(entityData),
            ontologyEntityId: entityId,
          });
          
          if (!updateResult.success) {
            throw new Error('Update returned success: false');
          }
        } catch (err) {
          console.error('[processOntologyCommands] Failed to update relation interaction on server after create', {
            interactionId: interaction.id,
            entityId,
            error: err,
          });
          try {
            await api.deleteProjectInteraction(projectId, interaction.id);
            console.warn('[processOntologyCommands] Deleted incomplete relation interaction after failed update', interaction.id);
          } catch (deleteErr) {
            console.error('[processOntologyCommands] Failed to cleanup incomplete relation interaction', interaction.id, deleteErr);
          }
          errors.push({ command: cmd, error: err instanceof Error ? err : new Error(String(err)) });
          processedRelations++;
          continue;
        }
        
        createdInteractionIds.push(interaction.id);
        processedRelations++;
        
        // Обновляем прогресс после создания связи
        onProgress?.({
          stage: 'linking',
          processed: objectCommands.length + processedRelations,
          total: totalCommands,
          currentOperation: `Created ${cmd.type}`,
        });
        
        // Обновляем локальное состояние
        useGenaiChatStore.setState((s) => ({
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
        
      } else if (cmd.operation === 'update' || cmd.operation === 'delete') {
        // UPDATE и DELETE для связей обрабатываются так же, как для объектов
        // (логика уже есть выше в обработке objectCommands)
        console.warn('[processOntologyCommands] Update/Delete for relations not yet implemented in new flow', cmd);
        errors.push({ command: cmd, error: new Error('Update/Delete for relations not yet implemented') });
        processedRelations++;
      }
    } catch (err) {
      console.error('[processOntologyCommands] Unexpected error processing relation command', cmd, err);
      errors.push({ command: cmd, error: err instanceof Error ? err : new Error(String(err)) });
      processedRelations++;
    }
  }
  
  // Объединяем ошибки валидации и обработки
  const allErrors = [
    ...validationErrors.map(e => ({ command: e.command, error: e.error })),
    ...errors.map(e => ({ command: e.command, error: e.error.message })),
  ];
  
  // Логируем все ошибки, если они есть
  if (allErrors.length > 0) {
    console.error('[processOntologyCommands] Some commands failed', {
      totalCommands: commands.length,
      validCommands: validCommands.length,
      failedCommands: allErrors.length,
      errors: allErrors.map(e => ({
        operation: e.command.operation,
        type: e.command.type,
        id: e.command.id,
        error: e.error,
      })),
    });
    
    // Показываем предупреждение пользователю через store
    // Не перезаписываем существующий error, а добавляем к нему
    useGenaiChatStore.setState((s) => {
      const existingError = s.error;
      const newError = `Some ontology commands failed: ${allErrors.length} out of ${commands.length} commands failed. ${existingError ? existingError + ' ' : ''}Check console for details.`;
      return { error: newError } as any;
    });
  } else if (createdInteractionIds.length > 0) {
    // Успешно обработано - очищаем error только если он был о парсинге/truncation
    useGenaiChatStore.setState((s) => {
      const existingError = s.error;
      // Очищаем error только если он был о парсинге или truncation
      // Если error был установлен processOntologyCommands ранее, оставляем его
      if (existingError && (existingError.includes('parse') || existingError.includes('truncated'))) {
        return { error: null } as any;
      }
      return s;
    });
  }
  
  return {
    success: allErrors.length === 0,
    createdIds: createdInteractionIds,
    errors: allErrors,
  };
}

