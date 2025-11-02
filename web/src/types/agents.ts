/**
 * Agent types - unified interface for all agents
 * 
 * Both Data Agent and Entities Agent use the same communication patterns:
 * - Task: input JSON sent to agent
 * - Result: output JSON returned by agent
 * - Update: real-time progress updates via WebSocket
 */

// ============================================================================
// Common Types
// ============================================================================

/** Base context required for all agent tasks */
export interface AgentContext {
  tenantId: string;
  projectId: string; // REQUIRED
  userId: string;
}

/** Optional metadata for agent tasks */
export interface TaskMetadata {
  interactionId?: string;
  messageId?: string;
  custom?: Record<string, any>;
}

/** Real-time progress update from agent */
export interface AgentUpdate {
  taskId: string;
  agentId: string;
  agentName: string;
  stage: 'searching' | 'analyzing' | 'planning' | 'executing' | 'saving' | 'completed' | 'error';
  progress: number; // 0.0 - 1.0
  message: string;
  data?: Record<string, any>;
  timestamp: string;
  error?: string;
}

/** Agent information for listing */
export interface AgentInfo {
  id: string;
  name: string;
  displayName: string;
  description: string;
  skills: string[];
  type: 'internal' | 'external';
  isExternal: boolean;
}

// ============================================================================
// Data Agent Types
// ============================================================================

/** Data Agent Task - request for data collection */
export interface DataAgentTask {
  context: AgentContext;
  query: string;
  granularity?: 'monthly' | 'quarterly' | 'yearly' | 'annual';
  timeRange: {
    from: string; // ISO date string
    to: string; // ISO date string
  };
  region?: string;
  category?: string;
  metadata?: TaskMetadata;
}

/** Data Agent Result */
export interface DataAgentResult {
  dataPoints: Array<Record<string, any>>;
  metadata: {
    sources: Array<Record<string, any>>;
    methods: Array<Record<string, any>>;
    usedData: Array<Record<string, any>>;
    assumptions: Array<Record<string, any>>;
  };
  confidence: number; // 0.0 - 1.0
  explanation: string;
}

// ============================================================================
// Entities Agent Types
// ============================================================================

/** Entity input for Entities Agent */
export interface EntityInput {
  id?: string;
  name: string;
  type: string;
  knownAliases?: string[];
}

/** Entities Agent Task - request for entity research */
export interface EntitiesAgentTask {
  context: AgentContext;
  entities: EntityInput[];
  maxDepth?: number; // 1-5, default: 2
  includeDetails?: boolean; // default: true
  metadata?: TaskMetadata;
}

/** Entities Agent Result */
export interface EntitiesAgentResult {
  entities: Array<Record<string, any>>;
  relations: Array<Record<string, any>>;
  confidence: number; // 0.0 - 1.0
  explanation: string;
  sources: Array<Record<string, any>>;
}

// ============================================================================
// Unified Agent Types (for generic usage)
// ============================================================================

/** Unified agent task - can be either Data or Entities */
export type AgentTask = DataAgentTask | EntitiesAgentTask;

/** Unified agent result - can be either Data or Entities */
export type AgentResult = DataAgentResult | EntitiesAgentResult;

/** Agent execution response */
export interface AgentExecutionResponse {
  result: AgentResult;
  agentId: string;
  agentName: string;
}

/** List of available agents */
export interface AgentsListResponse {
  agents: AgentInfo[];
}

