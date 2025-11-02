import { create } from 'zustand';
import type {
  AgentInfo,
  AgentUpdate,
  AgentResult,
  AgentExecutionResponse,
  DataAgentTask,
  EntitiesAgentTask,
} from '../types/agents';
import { api } from '../lib/api';

/**
 * Agent Store
 * 
 * Manages agent state, execution, and real-time updates.
 * Both Data Agent and Entities Agent use the same communication patterns.
 */

interface AgentTaskState {
  taskId: string;
  agentId: string;
  agentName: string;
  status: 'idle' | 'running' | 'completed' | 'error';
  progress: number; // 0.0 - 1.0
  stage?: AgentUpdate['stage'];
  message?: string;
  error?: string;
  result?: AgentResult;
  startedAt?: number;
  completedAt?: number;
}

interface AgentsState {
  // Available agents list
  agents: AgentInfo[];
  agentsLoaded: boolean;

  // Active tasks by taskId
  activeTasks: Record<string, AgentTaskState>;

  // Actions
  loadAgents: () => Promise<void>;
  executeDataAgent: (projectId: string, task: DataAgentTask) => Promise<string>; // Returns taskId
  executeEntitiesAgent: (projectId: string, task: EntitiesAgentTask) => Promise<string>; // Returns taskId
  executeAgent: (projectId: string, agentId: string, task: any) => Promise<string>; // Generic execution
  handleUpdate: (update: AgentUpdate) => void;
  getTaskState: (taskId: string) => AgentTaskState | undefined;
  clearTask: (taskId: string) => void;
  clearAllTasks: () => void;
}

export const useAgentsStore = create<AgentsState>((set, get) => ({
  agents: [],
  agentsLoaded: false,
  activeTasks: {},

  loadAgents: async () => {
    try {
      const response = await api.listAgents();
      set({ agents: response.agents, agentsLoaded: true });
    } catch (error) {
      console.error('[AgentsStore] Failed to load agents:', error);
      set({ agentsLoaded: true }); // Mark as loaded even on error
    }
  },

  executeDataAgent: async (projectId: string, task: DataAgentTask): Promise<string> => {
    try {
      const response = await api.executeDataAgent(projectId, task);
      // Task ID is generated server-side, we'll get it from WebSocket updates
      // For now, create a placeholder task state
      const taskId = `task_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
      const taskState: AgentTaskState = {
        taskId,
        agentId: 'data',
        agentName: response.agentName,
        status: 'running',
        progress: 0,
        stage: 'searching',
        message: 'Starting data collection...',
        startedAt: Date.now(),
      };
      set((state) => ({
        activeTasks: { ...state.activeTasks, [taskId]: taskState },
      }));
      return taskId;
    } catch (error: any) {
      console.error('[AgentsStore] Failed to execute Data Agent:', error);
      throw error;
    }
  },

  executeEntitiesAgent: async (projectId: string, task: EntitiesAgentTask): Promise<string> => {
    try {
      const response = await api.executeEntitiesAgent(projectId, task);
      const taskId = `task_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
      const taskState: AgentTaskState = {
        taskId,
        agentId: 'entities',
        agentName: response.agentName,
        status: 'running',
        progress: 0,
        stage: 'searching',
        message: 'Starting entity research...',
        startedAt: Date.now(),
      };
      set((state) => ({
        activeTasks: { ...state.activeTasks, [taskId]: taskState },
      }));
      return taskId;
    } catch (error: any) {
      console.error('[AgentsStore] Failed to execute Entities Agent:', error);
      throw error;
    }
  },

  executeAgent: async (projectId: string, agentId: string, task: any): Promise<string> => {
    try {
      const response = await api.executeAgent(projectId, agentId, task);
      const taskId = `task_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
      const taskState: AgentTaskState = {
        taskId,
        agentId,
        agentName: response.agentName,
        status: 'running',
        progress: 0,
        stage: 'searching',
        message: `Starting ${response.agentName}...`,
        startedAt: Date.now(),
      };
      set((state) => ({
        activeTasks: { ...state.activeTasks, [taskId]: taskState },
      }));
      return taskId;
    } catch (error: any) {
      console.error('[AgentsStore] Failed to execute agent:', error);
      throw error;
    }
  },

  handleUpdate: (update: AgentUpdate) => {
    console.log('[AgentsStore] Agent update received:', update);
    
    set((state) => {
      const newTasks = { ...state.activeTasks };
      
      // Find task by taskId or agentId (fallback)
      let taskState = newTasks[update.taskId];
      if (!taskState) {
        // Try to find by agentId if taskId not found
        for (const [tid, ts] of Object.entries(newTasks)) {
          if (ts.agentId === update.agentId && ts.status === 'running') {
            taskState = ts;
            // Update taskId to match server-side ID
            delete newTasks[tid];
            break;
          }
        }
      }
      
      if (!taskState) {
        // Create new task state if not found
        taskState = {
          taskId: update.taskId,
          agentId: update.agentId,
          agentName: update.agentName,
          status: 'running',
          progress: 0,
          startedAt: Date.now(),
        };
      }
      
      // Update task state
      const updatedState: AgentTaskState = {
        ...taskState,
        taskId: update.taskId, // Ensure we have the correct taskId
        progress: update.progress,
        stage: update.stage,
        message: update.message,
        error: update.error,
      };
      
      // Update status based on stage
      if (update.stage === 'completed') {
        updatedState.status = 'completed';
        updatedState.completedAt = Date.now();
        // Note: result will be available via HTTP response, not WebSocket
      } else if (update.stage === 'error') {
        updatedState.status = 'error';
        updatedState.completedAt = Date.now();
        updatedState.error = update.error || update.message;
      } else {
        updatedState.status = 'running';
      }
      
      // Store any additional data
      if (update.data) {
        // You can extend this to store specific data fields if needed
      }
      
      newTasks[update.taskId] = updatedState;
      return { activeTasks: newTasks };
    });
  },

  getTaskState: (taskId: string): AgentTaskState | undefined => {
    return get().activeTasks[taskId];
  },

  clearTask: (taskId: string) => {
    set((state) => {
      const newTasks = { ...state.activeTasks };
      delete newTasks[taskId];
      return { activeTasks: newTasks };
    });
  },

  clearAllTasks: () => {
    set({ activeTasks: {} });
  },
}));

