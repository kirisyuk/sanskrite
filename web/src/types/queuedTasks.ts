/**
 * Queued Tasks types
 * 
 * Tasks are stored in a single Interaction with kind='note' and subkind='queued.tasks'
 * The content field contains JSON with array of tasks.
 */

export type QueuedTaskType = 'text' | string; // 'text' is current, future types can be added

export interface QueuedTask {
  id: string;
  type: QueuedTaskType;
  title: string;
  prompt: string;
  createdAt: number; // timestamp
  applied?: boolean;
  appliedAt?: number; // timestamp when applied
}

export interface QueuedTasksData {
  tasks: QueuedTask[];
}

// Legacy TextTask interface for backward compatibility
export interface TextTask {
  id: string;
  title: string;
  prompt: string;
}

