# Agents Integration - Frontend Summary

## ✅ Completed

### 1. TypeScript Types (`web/src/types/agents.ts`)
- ✅ Unified agent types (AgentContext, TaskMetadata, AgentUpdate)
- ✅ Data Agent types (DataAgentTask, DataAgentResult)
- ✅ Entities Agent types (EntitiesAgentTask, EntitiesAgentResult, EntityInput)
- ✅ Common types (AgentInfo, AgentExecutionResponse, AgentsListResponse)
- ✅ All types exported from `web/src/types/index.ts`

### 2. API Client (`web/src/lib/api.ts`)
- ✅ `listAgents()`: Get list of available agents
- ✅ `executeDataAgent(projectId, task)`: Execute Data Agent
- ✅ `executeEntitiesAgent(projectId, task)`: Execute Entities Agent
- ✅ `executeAgent(projectId, agentId, task)`: Generic execution method
- ✅ All methods are fully typed

### 3. Agent Store (`web/src/stores/agentsStore.ts`)
- ✅ Zustand store for agent state management
- ✅ `loadAgents()`: Load available agents
- ✅ `executeDataAgent()`, `executeEntitiesAgent()`, `executeAgent()`: Execute agents
- ✅ `handleUpdate()`: Process WebSocket updates
- ✅ `getTaskState()`: Get task state by taskId
- ✅ `clearTask()`, `clearAllTasks()`: Task cleanup
- ✅ Active tasks tracking with progress, stage, and status

### 4. WebSocket Integration (`web/src/stores/discoveryStore.ts`)
- ✅ Added handler for `agent.update` events
- ✅ Routes updates to `agentsStore.handleUpdate()`
- ✅ Automatic integration with existing WebSocket connection

### 5. Documentation (`web/src/stores/AGENTS_README.md`)
- ✅ Complete usage guide for both agents
- ✅ Code examples for Data Agent and Entities Agent
- ✅ WebSocket event documentation
- ✅ Type safety guidelines

## 🎯 Key Features

### Unified Communication Pattern
Both agents use **identical** communication patterns:
- **Task**: JSON input (typed)
- **Result**: JSON output (typed)
- **Updates**: Real-time WebSocket events (`agent.update`)

### Type Safety
- All types are strictly typed
- TypeScript ensures type safety across the integration
- Types exported from centralized `types/index.ts`

### Real-time Progress Tracking
- WebSocket events automatically update agent state
- Progress tracking (0.0 - 1.0)
- Stage tracking (searching, analyzing, planning, executing, saving, completed, error)
- Error handling

## 📋 Available Agents

### 1. Data Agent (`data`)
- **Purpose**: Collects and aggregates data from various sources
- **Skills**: `data_collection`, `osint`, `data_aggregation`, `approximation`
- **Input**: Query, time range, granularity, region, category
- **Output**: Data points with metadata (sources, methods, assumptions)

### 2. Entities Agent (`entities`)
- **Purpose**: Searches for information about entities and discovers relations
- **Skills**: `web_search`, `entity_extraction`, `relation_discovery`
- **Input**: Array of entities, max depth, include details flag
- **Output**: Enriched entities, relations, sources

## 🔌 Integration Points

1. **API Client**: `web/src/lib/api.ts`
   - REST endpoints for agent operations
   - Automatic authentication via Firebase token

2. **Agent Store**: `web/src/stores/agentsStore.ts`
   - State management for agents
   - Task execution and tracking

3. **WebSocket**: `web/src/stores/discoveryStore.ts`
   - Real-time updates via `agent.update` events
   - Automatic routing to agent store

4. **Types**: `web/src/types/agents.ts`
   - Complete type definitions
   - Exported from `web/src/types/index.ts`

## 📝 Next Steps (UI Implementation)

The infrastructure is ready. To implement the UI:

1. **List Agents**: Use `useAgentsStore().agents` to display available agents
2. **Execute Agent**: Call `executeDataAgent()` or `executeEntitiesAgent()`
3. **Show Progress**: Use `getTaskState(taskId)` to display progress
4. **Handle Results**: Process `taskState.result` when status is 'completed'

## ✨ Example Usage

```typescript
import { useAgentsStore } from '@/stores/agentsStore';
import type { DataAgentTask } from '@/types';

const { executeDataAgent, getTaskState } = useAgentsStore();

// Execute agent
const taskId = await executeDataAgent(projectId, {
  context: { tenantId, projectId, userId },
  query: 'counterfeit level in beverages Kazakhstan 2025',
  granularity: 'monthly',
  timeRange: { from: '2025-01-01', to: '2025-12-31' },
});

// Monitor progress
const taskState = getTaskState(taskId);
console.log(`${taskState.progress * 100}% - ${taskState.message}`);
```

## 🔒 Security & Authentication

- ✅ Unified authentication across HATL (Firebase token)
- ✅ `projectId` is mandatory in all agent tasks
- ✅ Automatic context injection from authentication

## 📚 Documentation

See `web/src/stores/AGENTS_README.md` for complete documentation with examples.

