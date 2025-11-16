/**
 * Utilities for managing queued tasks stored in Interactions
 * 
 * Tasks are stored in a single Interaction per project:
 * - kind: 'note'
 * - subkind: 'queued.tasks'
 * - content: JSON string with QueuedTasksData
 */

import { api } from './api';
import type { QueuedTask, QueuedTasksData } from '../types/queuedTasks';
import type { Interaction } from '../types/interaction';

const QUEUED_TASKS_SUBKIND = 'queued.tasks';

/**
 * Find the queued tasks interaction for a project
 */
export async function findQueuedTasksInteraction(
  projectId: string
): Promise<Interaction | null> {
  try {
    const interactions = await api.getProjectInteractions(projectId);
    const found = interactions.find(
      (it) => it.kind === 'note' && it.subkind === QUEUED_TASKS_SUBKIND
    );
    if (!found) {
      return null;
    }
    // API returns partial Interaction without tenantId/projectId, but we know projectId
    return { ...found, tenantId: '', projectId } as Interaction;
  } catch (e) {
    console.warn('[queuedTasks] Failed to find interaction', e);
    return null;
  }
}

/**
 * Load queued tasks from interactions
 */
export async function loadQueuedTasks(
  projectId: string
): Promise<QueuedTask[]> {
  const interaction = await findQueuedTasksInteraction(projectId);
  if (!interaction) {
    return [];
  }

  try {
    const data: QueuedTasksData = JSON.parse(interaction.content);
    return data.tasks || [];
  } catch (e) {
    console.warn('[queuedTasks] Failed to parse tasks data', e);
    return [];
  }
}

/**
 * Save queued tasks to interaction (create or update)
 */
export async function saveQueuedTasks(
  projectId: string,
  tasks: QueuedTask[]
): Promise<void> {
  const data: QueuedTasksData = { tasks };
  const content = JSON.stringify(data);

  const existing = await findQueuedTasksInteraction(projectId);

  if (existing) {
    // Update existing interaction
    await api.updateProjectInteraction(projectId, existing.id, {
      content,
    });
  } else {
    // Create new interaction
    await api.createProjectInteraction(projectId, {
      kind: 'note',
      subkind: QUEUED_TASKS_SUBKIND,
      content,
    });
  }
}

/**
 * Add a new task
 */
export async function addQueuedTask(
  projectId: string,
  task: QueuedTask
): Promise<void> {
  const tasks = await loadQueuedTasks(projectId);
  const updated = [...tasks, task];
  await saveQueuedTasks(projectId, updated);
}

/**
 * Remove a task by ID
 */
export async function removeQueuedTask(
  projectId: string,
  taskId: string
): Promise<void> {
  const tasks = await loadQueuedTasks(projectId);
  const updated = tasks.filter((t) => t.id !== taskId);
  await saveQueuedTasks(projectId, updated);
}

/**
 * Mark a task as applied
 */
export async function markTaskApplied(
  projectId: string,
  taskId: string
): Promise<void> {
  const tasks = await loadQueuedTasks(projectId);
  const updated = tasks.map((t) =>
    t.id === taskId
      ? { ...t, applied: true, appliedAt: Date.now() }
      : t
  );
  await saveQueuedTasks(projectId, updated);
}

/**
 * Mark multiple tasks as applied
 */
export async function markTasksApplied(
  projectId: string,
  taskIds: string[]
): Promise<void> {
  const tasks = await loadQueuedTasks(projectId);
  const now = Date.now();
  const updated = tasks.map((t) =>
    taskIds.includes(t.id)
      ? { ...t, applied: true, appliedAt: now }
      : t
  );
  await saveQueuedTasks(projectId, updated);
}

