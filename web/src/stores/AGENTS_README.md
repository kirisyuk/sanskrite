# Agents API - Frontend Integration

## Overview

The Agents system provides a unified interface for autonomous, stateless agents that can perform research tasks. Both **Data Agent** and **Entities Agent** use the same communication patterns:

- **Task**: JSON input sent to agent
- **Result**: JSON output returned by agent  
- **Updates**: Real-time progress updates via WebSocket (`agent.update` event)

## Available Agents

### 1. Data Agent (`data`)
**Purpose**: Collects and aggregates data from various sources with metadata

**Skills**: `data_collection`, `osint`, `data_aggregation`, `approximation`

**Use Case**: Request data metrics (e.g., "level of counterfeit in low-alcohol beverages Kazakhstan (all), 2025")

**Task Schema**:
```typescript
{
  context: AgentContext;      // Required: tenantId, projectId, userId
  query: string;               // Required: Data query description
  granularity?: 'monthly' | 'quarterly' | 'yearly' | 'annual';
  timeRange: {                // Required
    from: string;             // ISO date string
    to: string;               // ISO date string
  };
  region?: string;
  category?: string;
  metadata?: TaskMetadata;
}
```

**Result Schema**:
```typescript
{
  dataPoints: Array<Record<string, any>>;  // Monthly/quarterly/yearly data
  metadata: {
    sources: Array<Record<string, any>>;    // Data sources used
    methods: Array<Record<string, any>>;    // Methods applied
    usedData: Array<Record<string, any>>;   // Indirect data used
    assumptions: Array<Record<string, any>>; // Assumptions made
  };
  confidence: number;          // 0.0 - 1.0
  explanation: string;         // How data was collected
}
```

### 2. Entities Agent (`entities`)
**Purpose**: Searches for information about entities and discovers relations

**Skills**: `web_search`, `entity_extraction`, `relation_discovery`

**Use Case**: Research entities, find relationships, enrich entity details

**Task Schema**:
```typescript
{
  context: AgentContext;      // Required: tenantId, projectId, userId
  entities: EntityInput[];   // Required: Array of entities to research
  maxDepth?: number;        // 1-5, default: 2 (relation depth)
  includeDetails?: boolean;  // default: true
  metadata?: TaskMetadata;
}
```

**EntityInput**:
```typescript
{
  id?: string;
  name: string;              // Required
  type: string;              // Required
  knownAliases?: string[];
}
```

**Result Schema**:
```typescript
{
  entities: Array<Record<string, any>>;      // Enriched entities
  relations: Array<Record<string, any>>;      // Discovered relationships
  confidence: number;         // 0.0 - 1.0
  explanation: string;        // How research was conducted
  sources: Array<Record<string, any>>;       // Sources used
}
```

## Usage

### 1. Load Available Agents

```typescript
import { useAgentsStore } from '@/stores/agentsStore';

const { loadAgents, agents, agentsLoaded } = useAgentsStore();

// Load agents on component mount
useEffect(() => {
  if (!agentsLoaded) {
    loadAgents();
  }
}, [agentsLoaded]);
```

### 2. Execute Data Agent

```typescript
import { useAgentsStore } from '@/stores/agentsStore';
import type { DataAgentTask } from '@/types/agents';

const { executeDataAgent } = useAgentsStore();

const task: DataAgentTask = {
  context: {
    tenantId: 'tenant123',
    projectId: 'project456',  // REQUIRED
    userId: 'user789',
  },
  query: 'level of counterfeit in low-alcohol beverages Kazakhstan (all), 2025',
  granularity: 'monthly',
  timeRange: {
    from: '2025-01-01',
    to: '2025-12-31',
  },
  region: 'Kazakhstan',
  category: 'low-alcohol beverages',
};

try {
  const taskId = await executeDataAgent('project456', task);
  console.log('Task started:', taskId);
} catch (error) {
  console.error('Failed to execute Data Agent:', error);
}
```

### 3. Execute Entities Agent

```typescript
import { useAgentsStore } from '@/stores/agentsStore';
import type { EntitiesAgentTask, EntityInput } from '@/types/agents';

const { executeEntitiesAgent } = useAgentsStore();

const entities: EntityInput[] = [
  {
    name: 'Kazakhstan Beverage Association',
    type: 'organization',
    knownAliases: ['KBA', 'Казахстанская Ассоциация Напитков'],
  },
  {
    name: 'Counterfeit Products',
    type: 'concept',
  },
];

const task: EntitiesAgentTask = {
  context: {
    tenantId: 'tenant123',
    projectId: 'project456',  // REQUIRED
    userId: 'user789',
  },
  entities,
  maxDepth: 2,
  includeDetails: true,
};

try {
  const taskId = await executeEntitiesAgent('project456', task);
  console.log('Task started:', taskId);
} catch (error) {
  console.error('Failed to execute Entities Agent:', error);
}
```

### 4. Monitor Progress via WebSocket

WebSocket events are automatically handled by the store. The `agent.update` event is processed in `discoveryStore.ts` and routed to `agentsStore.ts`.

```typescript
import { useAgentsStore } from '@/stores/agentsStore';

const { getTaskState, activeTasks } = useAgentsStore();

// Get task state
const taskState = getTaskState(taskId);
if (taskState) {
  console.log('Status:', taskState.status);      // 'running' | 'completed' | 'error'
  console.log('Progress:', taskState.progress);  // 0.0 - 1.0
  console.log('Stage:', taskState.stage);         // 'searching' | 'analyzing' | 'planning' | 'executing' | 'saving' | 'completed' | 'error'
  console.log('Message:', taskState.message);
  console.log('Error:', taskState.error);
}

// Iterate all active tasks
for (const [taskId, state] of activeTasks.entries()) {
  console.log(`Task ${taskId}: ${state.agentName} - ${state.status}`);
}
```

### 5. Generic Agent Execution

```typescript
import { useAgentsStore } from '@/stores/agentsStore';

const { executeAgent } = useAgentsStore();

// Execute any agent by ID
const taskId = await executeAgent(
  'project456',
  'data',  // or 'entities'
  task     // Task must match agent's TaskSchema
);
```

## WebSocket Events

The frontend automatically subscribes to `agent.update` events via the existing WebSocket connection in `discoveryStore.ts`.

**Event Structure**:
```typescript
{
  type: 'agent.update',
  data: {
    taskId: string;
    agentId: string;
    agentName: string;
    stage: 'searching' | 'analyzing' | 'planning' | 'executing' | 'saving' | 'completed' | 'error';
    progress: number;        // 0.0 - 1.0
    message: string;
    data?: Record<string, any>;
    timestamp: string;
    error?: string;
  }
}
```

## Type Safety

All types are defined in `web/src/types/agents.ts` and exported from `web/src/types/index.ts`:

```typescript
import type {
  AgentContext,
  TaskMetadata,
  AgentUpdate,
  AgentInfo,
  DataAgentTask,
  DataAgentResult,
  EntityInput,
  EntitiesAgentTask,
  EntitiesAgentResult,
  AgentTask,
  AgentResult,
  AgentExecutionResponse,
  AgentsListResponse,
} from '@/types';
```

## API Methods

The API client (`web/src/lib/api.ts`) provides typed methods:

```typescript
import { api } from '@/lib/api';

// List all agents
const { agents } = await api.listAgents();

// Execute Data Agent
const response = await api.executeDataAgent(projectId, task);

// Execute Entities Agent
const response = await api.executeEntitiesAgent(projectId, task);

// Generic execution
const response = await api.executeAgent(projectId, agentId, task);
```

## Store API

The `useAgentsStore` provides:

- `loadAgents()`: Load list of available agents
- `executeDataAgent(projectId, task)`: Execute Data Agent
- `executeEntitiesAgent(projectId, task)`: Execute Entities Agent
- `executeAgent(projectId, agentId, task)`: Generic execution
- `handleUpdate(update)`: Handle WebSocket update (called automatically)
- `getTaskState(taskId)`: Get task state
- `clearTask(taskId)`: Remove task from store
- `clearAllTasks()`: Clear all tasks

## Notes

1. **projectId is REQUIRED**: All agent tasks must include a valid `projectId` in the context
2. **Unified Interface**: Both agents use the same communication patterns (Task → Result → Updates)
3. **Real-time Updates**: Progress is tracked via WebSocket events, not polling
4. **Stateless**: Agents don't store anything; storage and usage are the caller's responsibility
5. **Authentication**: Agents use unified authentication across Etigram (via Firebase token)
