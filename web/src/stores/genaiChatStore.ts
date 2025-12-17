import { create } from 'zustand';
import { api } from '../lib/api';
import type { SxInstruction, SxEntityMark, SxMonitorMetric } from '../types/sx';
import { parseSxInstructionsFromContent } from '../lib/sxUtils';
import { persistArtifacts } from '../lib/artifactPersistence';
import type { AnalyzeInteractionRef, AnalyzeRequest } from '../types/stance';
import type { GenaiToolCall } from './genaiStore';
import { useGenAISettings, mapProviderStrengthToModel, getModelContextWindow } from './genaiSettingsStore';
import { useProjectsStore } from './projectsStore';
import { parseOntologyCommands } from '../types/ontologyCommands';

// Track active agent completion timers to prevent memory leaks
const activeAgentTimers = new Map<string, number>();

// Cleanup function to clear all active agent timers
function clearAllAgentTimers() {
  activeAgentTimers.forEach((timeoutId) => {
    window.clearTimeout(timeoutId);
  });
  activeAgentTimers.clear();
}

// Cleanup timers for a specific project
function clearAgentTimersForProject(projectId: string) {
  // Note: We don't track projectId in timers, so we clear all
  // This is safe because timers check projectId before executing
  clearAllAgentTimers();
}
import { processOntologyCommands } from '../lib/ontologyCommandProcessor';
import { mapAliasToModel } from '../lib/modelMapping';

export type ChatRole = 'user' | 'assistant' | 'system';

export interface ChatMessage {
  id: string;
  role: ChatRole;
  content: string;
  timestamp?: number;
}

export interface StartStreamArgs {
  prompt: string;
  tools?: any[];
  options?: any;
  agentMeta?: { displayName?: string; replyTo?: string; subkind?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } };
  suppressUserEcho?: boolean;
}

type InteractionKind = 'user' | 'assistant' | 'system' | 'note' | 'section_start' | 'section_end' | 'action' | 'research' | 'ontology' | 'agent.execution';

// Ontology entity subkinds
export type OntologySubkind = 'entity' | 'event' | 'claim' | 'relation' | 'concept' | 'metricState' | 'trend' | 'causalLink' | 'scenario' | 'actionableItem';

// Ontology operations (commands from LLM)
export type OntologyOperation = 'create' | 'update' | 'delete';

export interface LocalInteraction {
  id: string;
  projectId: string; // temporary local id binding
  kind: InteractionKind;
  subkind?: string;
  // For ontology interactions: subkind contains entity type, content contains full entity JSON
  // Status is tracked via metadata.status (active/deleted) - interactions store final states only
  content: string;
  contentSanitized?: string;
  sxInstructions?: SxInstruction[];
  summary?: string;
  originalMessageId?: string;
  model?: string;
  usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number };
  // Extended fields (optional; defaults keep UI working)
  author?: { participantId: string; kind: 'human'|'ai'|'system'|'tool'; displayName?: string };
  recipients?: string[];
  replyTo?: string;
  relations?: Array<{ type: 'answers'|'refines'|'evaluates'; targetId: string }>;
  agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> };
  systemInstructionApplied?: string;
  language?: string;
  generationParams?: { temperature?: number; maxOutputTokens?: number; groundingEnabled?: boolean };
  highRisk?: { level: 'High'|'Medium'|'Low'; score: number; rationale?: string; triggers?: string[] };
  roles?: Array<{ role: string; confidence: number; rationale: string }>;
  industries?: Array<{ industry: string; confidence: number; rationale: string }>;
  evaluations?: Record<string, any>;
  coreDocIds?: string[];
  metadata?: { pinned?: boolean; notes?: string[] };
  graphData?: { chartTitle: string; chartType?: string; table: any[][]; sources: string[] };
  // Anchored stream meta (per assistant message)
  searchQueries?: string[];
  sources?: string[];
  createdAt: string;
  // Research-specific fields
  researchQuery?: string;
  researchProgress?: {
    stage: 'planning' | 'searching' | 'generating';
    status: 'in_progress' | 'completed';
    subQueries?: string[];
    sourcesCount?: number;
  };
  subQueries?: string[];
  researchSources?: Array<{
    title: string;
    url: string;
    score?: number;
  }>;
  // Ontology-specific: entity ID from the entity data (stored in content as JSON)
  // Used for deduplication and updates - same entity ID means same entity
  ontologyEntityId?: string; // Extracted from content.id when kind='ontology'
}

interface GenaiChatState {
  messages: ChatMessage[];
  streaming: boolean;
  error: string | null;
  toolCalls: GenaiToolCall[];
  toolResults: GenaiToolCall[];
  toolRetries: Array<{ toolName: string; attempt: number; errorType: string; error: string; timestamp: number }>;
  toolCorrections: Array<{ toolName: string; attempt: number; correctedArgs: string; timestamp: number }>;
  attributions: Array<{ title?: string; url?: string }>;
  searchQueries: string[];
  sources: string[];
  // Per-message structured evaluations (unified schema)
  evaluations: Record<string, {
    biasLevel: number;
    thoughtfulness: number; // kept for backward compatibility (same as thoughtfulnessLevel)
    thoughtfulnessLevel?: number;
    thoughtfulnessDimensions?: {
      analyticalDepth: number;
      epistemicHumility: number;
      alternativeConsideration: number;
      evidenceReasoningQuality: number;
      practicalGrounding: number;
    };
    categories: Array<{ code: string; explanation: string }>;
  }>;
  // Pre-stance preliminary call results (per user message)
  preStanceLatency: Record<string, number>;
  preStanceExtract: Record<string, { user_request: string }>;
  // Per-message UI meta (e.g., custom header for agent messages)
  messageMeta: Record<string, { headerText?: string; title?: string; leftAccent?: boolean; bgOverride?: string; refineEnabled?: boolean; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } }>;
  usage?: { promptTokens: number; outputTokens: number; totalTokens: number };

  // local-only interactions cache for Unfold
  projectId: string; // temporary project id (frontend-owned)
  interactions: LocalInteraction[];
  // Extracted perspectives cache (keyed by preset: 'consulting', 'themes', etc.)
  perspectives: Record<string, { id: string; content: string; preset: string; createdAt: string }>;
  // Centralized artifacts extracted from @sx instructions (deduplicated)
  artifacts: { entities: SxEntityMark[]; metrics: SxMonitorMetric[] };
  // Metric overlay (full-screen over chat) state and cache
  metricOverlayOpenKey?: string | null;
  metricOverlayLoading?: boolean;
  metricOverlayCache: Record<string, { key: string; metric: SxMonitorMetric; content: string; fetchedAt: string }>;
  // Mapping of local ids/message ids -> server interaction ids
  persistedByLocalId: Record<string, string>;
  pendingAssistantMeta?: { displayName?: string; subkind?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } };
  pendingAssistantOptions?: any;
  pendingAssistantPrompt?: string;
  pendingAssistantTools?: any[];
  pendingAttachedDocIds: string[];

  startStream: (args: StartStreamArgs) => Promise<void>;
  setPendingAttachedDocIds: (ids: string[]) => void;
  reset: () => void;
  cancelStream: () => void;
  clearToolRetries: () => void;
  openMetricOverlay: (metric: SxMonitorMetric) => Promise<void>;
  closeMetricOverlay: () => void;

  onStart: () => void;
  onChunk: (text: string) => void;
  onDone: () => void;
  onError: (err: string) => void;
  onToolCall: (calls: GenaiToolCall[]) => void;
  onToolResult: (calls: GenaiToolCall[]) => void;
  onToolRetry: (data: { toolName: string; attempt: number; errorType: string; error: string }) => void;
  onToolCorrection: (data: { toolName: string; attempt: number; correctedArgs: string }) => void;
  onDoneMeta: (meta: { usage?: { promptTokens: number; outputTokens: number; totalTokens: number }; attributions?: Array<{ title?: string; url?: string }>; searchQueries?: string[]; sources?: string[] }) => void;
  setEvaluation: (messageId: string, ev: {
    biasLevel: number;
    thoughtfulness: number;
    thoughtfulnessLevel?: number;
    thoughtfulnessDimensions?: {
      analyticalDepth: number;
      epistemicHumility: number;
      alternativeConsideration: number;
      evidenceReasoningQuality: number;
      practicalGrounding: number;
    };
    categories: Array<{ code: string; explanation: string }>;
  }) => void;
  setPreStanceResult: (messageId: string, latencyMs: number, extract?: { user_request: string }) => void;

  // helpers to manage local interactions
  setProjectId: (id: string) => void;
  ensureProject: () => Promise<string>;
  hydrateInteractions: () => Promise<void>;
  addOrUpdateInteraction: (interactionId: string) => Promise<void>;
  deleteInteraction: (interactionId: string) => Promise<void>;
  createIndicatorMessage: (content: string, subkind: string, replyTo?: string, metadata?: Record<string, any>) => Promise<void>;

  // Your stance (meta aggregate) cache and actions
  stanceAggregate?: any;
  stanceLoading: boolean;
  loadStanceAggregate: () => Promise<void>;
  ensureStanceAggregate: () => Promise<void>;

  // Hydration loading state
  hydrating: boolean;
  // Active stream tracking (for filtering events by streamId)
  currentStreamId?: string;
  // Graph mode: ontology commands processing state
  processingCommands: boolean;
  commandsProgress: {
    stage: 'parsing' | 'validating' | 'creating' | 'updating' | 'deleting' | 'linking' | 'complete';
    processed: number;
    total: number;
    currentOperation?: string;
  } | null;
  // Request cancellation support
  abortController?: AbortController;
}

export const useGenaiChatStore = create<GenaiChatState>((set, get) => ({
  messages: [],
  streaming: false,
  error: null,
  toolCalls: [],
  toolResults: [],
  toolRetries: [],
  toolCorrections: [],
  attributions: [],
  searchQueries: [],
  sources: [],
  evaluations: {},
  preStanceLatency: {},
  preStanceExtract: {},
  messageMeta: {},
  projectId: 'local-unfold',
  interactions: [],
  perspectives: {},
  artifacts: { entities: [], metrics: [] },
  metricOverlayOpenKey: null,
  metricOverlayLoading: false,
  metricOverlayCache: {},
  persistedByLocalId: {},
  stanceAggregate: undefined,
  stanceLoading: false,
  pendingAttachedDocIds: [],
  hydrating: false,
  currentStreamId: undefined,
  processingCommands: false,
  commandsProgress: null,
  abortController: undefined,

  reset: () => {
    // Cancel any ongoing request before resetting
    const state = get();
    if (state.abortController) {
      state.abortController.abort();
    }
    // Clear all active agent timers to prevent memory leaks
    clearAllAgentTimers();
    set({ projectId: 'local-unfold', messages: [], streaming: false, error: null, toolCalls: [], toolResults: [], toolRetries: [], toolCorrections: [], attributions: [], searchQueries: [], sources: [], evaluations: {}, messageMeta: {}, usage: undefined, interactions: [], perspectives: {}, artifacts: { entities: [], metrics: [] }, metricOverlayOpenKey: null, metricOverlayLoading: false, metricOverlayCache: {}, persistedByLocalId: {}, stanceAggregate: undefined, stanceLoading: false, pendingAttachedDocIds: [], hydrating: false, currentStreamId: undefined, processingCommands: false, commandsProgress: null, abortController: undefined });
  },

  cancelStream: () => {
    const state = get();
    if (state.abortController) {
      console.log('[Chat] Cancelling stream request');
      state.abortController.abort();
      set({ 
        streaming: false, 
        currentStreamId: undefined, 
        abortController: undefined,
        error: 'Запрос был прерван пользователем'
      });
    }
  },
  
  clearToolRetries: () => set({ toolRetries: [], toolCorrections: [] }),
  
  setPendingAttachedDocIds: (ids) => set({ pendingAttachedDocIds: ids }),

  setProjectId: (id) => {
    // Ensure projectId is always a string
    const newProjectId = id ? (typeof id === 'string' ? id : String(id)) : 'local-unfold';
    
    // Clear agent timers when switching projects to prevent memory leaks
    const currentState = get();
    if (currentState.projectId !== newProjectId) {
      clearAgentTimersForProject(currentState.projectId);
    }
    
    set({
      projectId: newProjectId,
      // clear chat-local state to avoid mixing between projects
      messages: [],
      interactions: [],
      perspectives: {},
      artifacts: { entities: [], metrics: [] },
      persistedByLocalId: {},
      evaluations: {},
      messageMeta: {},
      metricOverlayCache: {},
      metricOverlayOpenKey: null,
      metricOverlayLoading: false,
      error: null,
      streaming: false,
      currentStreamId: undefined, // Clear stream tracking when switching projects
      stanceAggregate: undefined,
      stanceLoading: false,
      pendingAttachedDocIds: [],
      hydrating: false,
      hydratingProjectId: undefined,
    } as any);
  },

  ensureProject: async () => {
    // Check if project already exists
    let pid = get().projectId;
    if (pid && pid !== 'local-unfold') {
      console.log('[Chat] ensureProject: using existing project', pid);
      return pid;
    }
    
    // Check if another call is already creating a project (race condition protection)
    const state = get();
    const ensuringProject = (state as any).ensuringProject as Promise<string> | undefined;
    if (ensuringProject) {
      console.log('[Chat] ensureProject: waiting for concurrent project creation');
      return await ensuringProject;
    }
    
    // Create a promise to track this creation and prevent duplicates
    let creationPromise: Promise<string>;
    let lockCleared = false;
    
    creationPromise = (async () => {
      try {
        console.log('[Chat] ensureProject: creating project');
        // Double-check after acquiring lock (another call might have created it)
        const currentPid = get().projectId;
        if (currentPid && currentPid !== 'local-unfold') {
          console.log('[Chat] ensureProject: project was created by concurrent call', currentPid);
          if (!lockCleared) {
            set({ ensuringProject: undefined } as any);
            lockCleared = true;
          }
          return currentPid;
        }
        
        // Use 'discovery' view to match what DiscoveryPage uses for sidebar
        // This ensures the new project appears in the sidebar list
        const res = await api.createProject({ displayName: 'Draft', view: 'discovery' });
        // Ensure projectId is a string (handle cases where id might be an object)
        pid = typeof res.id === 'string' ? res.id : String(res.id);
        // CRITICAL: Set projectId immediately so stream events are not filtered out
        set({ projectId: pid } as any);
        console.log('[Chat] ensureProject: set projectId in store', { projectId: pid });
        // Reflect immediately in projects store as current chat
        try {
          // Ensure res.id is a string for consistency
          const projectWithStringId = {
            ...res,
            id: typeof res.id === 'string' ? res.id : String(res.id)
          };
          
          useProjectsStore.setState((prev) => ({
            projects: [projectWithStringId as any, ...prev.projects.filter((p) => {
              const pId = typeof p.id === 'string' ? p.id : String(p.id);
              const resId = typeof res.id === 'string' ? res.id : String(res.id);
              return pId !== resId;
            })],
            currentProject: projectWithStringId as any,
          }));
          
          // Refresh projects list to ensure sidebar shows the new project
          // After fetchProjects, restore currentProject to the newly created project
          useProjectsStore.getState().fetchProjects('discovery').then(() => {
            // Restore currentProject after fetchProjects (which may have cleared it)
            // Find the project in the fetched list or use the one we just created
            const state = useProjectsStore.getState();
            const foundProject = state.projects.find((p) => {
              const pId = typeof p.id === 'string' ? p.id : String(p.id);
              return pId === pid;
            });
            useProjectsStore.setState({ 
              currentProject: foundProject || projectWithStringId as any 
            });
          }).catch((err) => {
            console.warn('[ensureProject] Failed to refresh projects list', err);
          });
        } catch (_) {}
        console.log('[Chat] ensureProject: project created', pid);
        return pid;
      } catch (e) {
        console.error('[ensureProject] failed to create project', e);
        throw e;
      } finally {
        // Clear the lock after completion (success or error)
        if (!lockCleared) {
          set({ ensuringProject: undefined } as any);
          lockCleared = true;
        }
      }
    })();
    
    // Store the promise to prevent concurrent creations (must be after promise creation)
    set({ ensuringProject: creationPromise } as any);
    
    try {
      return await creationPromise;
    } catch (e) {
      // If creation failed, check if projectId was set by another call
      const finalPid = get().projectId;
      if (finalPid && finalPid !== 'local-unfold') {
        console.log('[Chat] ensureProject: using project set during failed creation', finalPid);
        return finalPid;
      }
      throw e;
    }
  },

  hydrateInteractions: async () => {
    const projectId = get().projectId;
    if (!projectId || projectId === 'local-unfold') return;
    
    // Защита от множественных одновременных вызовов для того же projectId
    // Но только если interactions уже загружены (не пустой массив) И hydratingProjectId совпадает
    const state = get();
    const currentHydratingProjectId = (state as any).hydratingProjectId;
    
    // Skip only if we're already hydrating the SAME project AND interactions are loaded
    if (state.hydrating && currentHydratingProjectId === projectId && state.interactions.length > 0) {
      console.log('[hydrateInteractions] Already hydrating same project with interactions loaded, skipping duplicate call', {
        projectId,
        hydratingProjectId: currentHydratingProjectId,
        interactionsCount: state.interactions.length
      });
      return;
    }
    
    // If hydrating but for different project, or interactions are empty, continue to load
    if (state.hydrating && currentHydratingProjectId !== projectId) {
      console.log('[hydrateInteractions] Hydrating different project, clearing and reloading', {
        currentHydratingProjectId,
        newProjectId: projectId
      });
      // Clear interactions and messages from old project
      set({ interactions: [], messages: [] } as any);
    } else if (state.hydrating && currentHydratingProjectId === projectId && state.interactions.length === 0) {
      console.log('[hydrateInteractions] Already hydrating but interactions empty, continuing to load');
    }
    
    set({ hydrating: true, hydratingProjectId: projectId } as any);
    try {
      const serverItems = await api.getProjectInteractions(projectId);
      // Check if projectId is still current before applying results (race condition guard)
      set((s) => {
        // If project changed while request was in flight, ignore this response
        if (s.projectId !== projectId) {
          return s;
        }
        const byServerId = new Set<string>((serverItems as any[]).map((it: any) => it.id));
        const map = (s as any).persistedByLocalId || {} as Record<string,string>;
        const keepLocals: any[] = [];
        for (const l of s.interactions) {
          // Always keep error interactions (subkind === 'error') - they are local-only and should persist
          if (l.subkind === 'error') {
            keepLocals.push(l);
            continue;
          }
          // Drop local if server already has this id, or local maps to a server id present
          const mapped = map[l.id as any];
          if (byServerId.has(l.id as any)) continue;
          if (mapped && byServerId.has(mapped)) continue;
          keepLocals.push(l);
        }
        // Обеспечиваем наличие contentSanitized для всех загруженных взаимодействий
        // Если его нет, но есть content с @sx тегами - парсим и создаем
        // Также обрабатываем graphData - добавляем chartType если отсутствует
        const processedServerItems = (serverItems as any[]).map((it: any) => {
          let updated = it;
          
          // Обработка contentSanitized
          if (it.kind === 'assistant' && it.content && !it.contentSanitized) {
            const { sanitized } = parseSxInstructionsFromContent(it.content);
            updated = { ...updated, contentSanitized: sanitized };
          }
          
          // Обработка graphData - добавляем chartType если отсутствует или невалиден
          if (it.kind === 'assistant' && (it.subkind === 'graph' || it.graphData)) {
            let graphData = it.graphData;
            
            // Если graphData отсутствует, но есть subkind === 'graph', пытаемся распарсить из content
            if (!graphData && it.subkind === 'graph' && (it.contentSanitized || it.content)) {
              try {
                let content = String(it.contentSanitized || it.content || '').trim();
                content = content.replace(/^```[a-zA-Z]*\n([\s\S]*?)\n```$/m, '$1').trim();
                let parsed: any = null;
                if (content.startsWith('{') || content.startsWith('[')) {
                  parsed = JSON.parse(content);
                } else {
                  const m = content.match(/\{[\s\S]*\}/);
                  if (m && m[0]) parsed = JSON.parse(m[0]);
                }
                if (parsed && typeof parsed === 'object' && typeof parsed.chartTitle === 'string' && Array.isArray(parsed.table) && Array.isArray(parsed.sources)) {
                  const allowedTypes = ['line', 'bar', 'area', 'stackedBar', 'stackedArea', 'composed'];
                  const chartType = typeof parsed.chartType === 'string' && allowedTypes.includes(parsed.chartType) ? parsed.chartType : 'line';
                  graphData = { chartTitle: parsed.chartTitle, chartType: chartType, table: parsed.table, sources: parsed.sources };
                }
              } catch {}
            }
            
            // Если graphData есть, но нет chartType или он невалиден - добавляем/исправляем
            if (graphData && typeof graphData === 'object' && typeof graphData.chartTitle === 'string') {
              const allowedTypes = ['line', 'bar', 'area', 'stackedBar', 'stackedArea', 'composed'];
              const chartType = typeof graphData.chartType === 'string' && allowedTypes.includes(graphData.chartType)
                ? graphData.chartType
                : 'line';
              updated = { ...updated, graphData: { ...graphData, chartType } };
            }
          }
          
          // Нормализация researchSources для research interactions
          if (it.kind === 'research' && it.researchSources) {
            const normalizedSources = Array.isArray(it.researchSources) 
              ? it.researchSources.map((src: any) => {
                  // Если это уже объект с title/url, нормализуем его
                  if (src && typeof src === 'object' && ('title' in src || 'url' in src)) {
                    return {
                      title: String(src.title || src.url || 'Unknown source'),
                      url: String(src.url || ''),
                      score: typeof src.score === 'number' ? src.score : undefined,
                    };
                  }
                  // Если это строка (URL), конвертируем в объект
                  if (typeof src === 'string') {
                    return {
                      title: src,
                      url: src,
                      score: undefined,
                    };
                  }
                  // Fallback
                  return {
                    title: 'Unknown source',
                    url: '',
                    score: undefined,
                  };
                })
              : [];
            updated = { ...updated, researchSources: normalizedSources };
          }
          
          return updated;
        });
        // Дедупликация по id: если есть два interaction с одинаковым id, оставляем только один (приоритет серверному)
        const seenIds = new Set<string>();
        const deduplicatedServerItems: any[] = [];
        for (const it of processedServerItems) {
          if (!seenIds.has(it.id)) {
            seenIds.add(it.id);
            deduplicatedServerItems.push(it);
          }
        }
        const deduplicatedKeepLocals: any[] = [];
        for (const it of keepLocals) {
          const localId = it.id as any;
          // Проверяем, не был ли уже добавлен interaction с таким id (через persistedByLocalId маппинг)
          const mappedId = map[localId];
          // Пропускаем локальный interaction, если:
          // 1. Его id уже есть в серверных interactions
          // 2. Его mapped id уже есть в серверных interactions (значит серверный interaction уже загружен)
          if (seenIds.has(localId) || (mappedId && seenIds.has(mappedId))) {
            continue;
          }
          seenIds.add(localId);
          if (mappedId) seenIds.add(mappedId);
          deduplicatedKeepLocals.push(it);
        }
        let merged = [...deduplicatedServerItems, ...deduplicatedKeepLocals];
        
        // Clean up dangling relations (relations pointing to non-existent entities) at chat load time
        // Build a set of all existing entity/concept IDs
        const existingEntityIds = new Set<string>();
        merged.forEach((it: any) => {
          if (it.kind === 'ontology') {
            // For entities/concepts, use ontologyEntityId or id
            const entityId = it.ontologyEntityId || it.id;
            if (entityId) {
              existingEntityIds.add(entityId);
            }
            // Also check content.id for backward compatibility
            if (it.content) {
              try {
                const entityData = JSON.parse(it.content);
                if (entityData.id) {
                  existingEntityIds.add(entityData.id);
                }
              } catch {
                // Ignore parse errors
              }
            }
          }
        });
        
        // Find and remove dangling relations
        const danglingRelationIds: string[] = [];
        const cleanedMerged: any[] = [];
        
        merged.forEach((it: any) => {
          if (it.kind === 'ontology' && it.subkind === 'relation' && it.content) {
            try {
              const relationData = JSON.parse(it.content);
              const subjectId = relationData.subjectId;
              const objectId = relationData.objectId;
              
              // Check if both subjectId and objectId exist
              const subjectExists = subjectId && existingEntityIds.has(subjectId);
              const objectExists = objectId && existingEntityIds.has(objectId);
              
              if (!subjectExists || !objectExists) {
                console.warn('[hydrateInteractions] Found dangling relation, will delete', {
                  relationId: it.id,
                  subjectId,
                  objectId,
                  subjectExists,
                  objectExists,
                });
                danglingRelationIds.push(it.id);
                // Don't add to cleanedMerged - effectively removing it
                return;
              }
            } catch (e) {
              console.warn('[hydrateInteractions] Failed to parse relation content, keeping relation', {
                relationId: it.id,
                error: e,
              });
            }
          }
          cleanedMerged.push(it);
        });
        
        // Delete dangling relations from server asynchronously (don't block hydration)
        if (danglingRelationIds.length > 0) {
          console.log('[hydrateInteractions] Deleting dangling relations', {
            count: danglingRelationIds.length,
            ids: danglingRelationIds,
          });
          (async () => {
            for (const relationId of danglingRelationIds) {
              try {
                await api.deleteProjectInteraction(projectId, relationId);
                console.log('[hydrateInteractions] Deleted dangling relation', { relationId });
              } catch (e) {
                console.warn('[hydrateInteractions] Failed to delete dangling relation', {
                  relationId,
                  error: e,
                });
              }
            }
          })();
        }
        
        merged = cleanedMerged;
        
        // Remove duplicate user messages by content (fire & forget async deletion)
        // This handles race conditions where the same user message is created multiple times
        const duplicateUserMessageIds: string[] = [];
        const contentToKeepId = new Map<string, string>(); // content -> id to keep
        
        // Find duplicate user messages by content
        merged.forEach((it: any) => {
          if (it.kind === 'user' && it.content && typeof it.content === 'string' && it.subkind !== 'action.bring_in') {
            const contentKey = it.content.trim();
            if (!contentKey) return;
            
            const existingKeepId = contentToKeepId.get(contentKey);
            if (existingKeepId) {
              // Found duplicate - determine which one to keep (keep the oldest by createdAt)
              const existingIt = merged.find((item: any) => item.id === existingKeepId);
              if (existingIt) {
                const existingCreatedAt = existingIt.createdAt;
                const currentCreatedAt = it.createdAt;
                
                // Parse timestamps for comparison
                let existingTs = 0;
                let currentTs = 0;
                try {
                  if (existingCreatedAt) {
                    if (typeof existingCreatedAt === 'string') {
                      existingTs = Date.parse(existingCreatedAt);
                    } else if (existingCreatedAt.seconds) {
                      existingTs = existingCreatedAt.seconds * 1000;
                    }
                  }
                  if (currentCreatedAt) {
                    if (typeof currentCreatedAt === 'string') {
                      currentTs = Date.parse(currentCreatedAt);
                    } else if (currentCreatedAt.seconds) {
                      currentTs = currentCreatedAt.seconds * 1000;
                    }
                  }
                } catch {}
                
                // Keep the oldest one (smallest timestamp), or if timestamps are equal, keep the one with smaller ID
                if (currentTs > 0 && existingTs > 0) {
                  if (currentTs < existingTs) {
                    // Current is older, keep it and mark existing as duplicate
                    duplicateUserMessageIds.push(existingKeepId);
                    contentToKeepId.set(contentKey, it.id);
                  } else if (currentTs === existingTs) {
                    // Same timestamp, keep the one with smaller ID (lexicographically)
                    if (it.id < existingKeepId) {
                      duplicateUserMessageIds.push(existingKeepId);
                      contentToKeepId.set(contentKey, it.id);
                    } else {
                      duplicateUserMessageIds.push(it.id);
                    }
                  } else {
                    // Existing is older, keep it and mark current as duplicate
                    duplicateUserMessageIds.push(it.id);
                  }
                } else {
                  // If timestamps are invalid, keep the one with smaller ID
                  if (it.id < existingKeepId) {
                    duplicateUserMessageIds.push(existingKeepId);
                    contentToKeepId.set(contentKey, it.id);
                  } else {
                    duplicateUserMessageIds.push(it.id);
                  }
                }
              } else {
                // Existing not found, keep current
                contentToKeepId.set(contentKey, it.id);
              }
            } else {
              // First occurrence, keep it
              contentToKeepId.set(contentKey, it.id);
            }
          }
        });
        
        // Remove duplicates from merged array before setting state (so they don't appear in UI)
        const duplicateSet = new Set(duplicateUserMessageIds);
        merged = merged.filter((it: any) => !duplicateSet.has(it.id));
        
        // Delete duplicate user messages and their dependents asynchronously (fire & forget)
        if (duplicateUserMessageIds.length > 0) {
          console.log('[hydrateInteractions] Found duplicate user messages, will delete', {
            count: duplicateUserMessageIds.length,
            ids: duplicateUserMessageIds,
          });
          (async () => {
            const map = (s as any).persistedByLocalId || {};
            
            for (const duplicateId of duplicateUserMessageIds) {
              try {
                // Build cascade set: include duplicate id and any evaluations/system notes tied via replyTo or relations
                const idSet = new Set<string>();
                idSet.add(duplicateId);
                
                // Expand with local/server mapping variants
                if (map[duplicateId]) idSet.add(map[duplicateId]);
                const reverse: Record<string, string[]> = {};
                Object.entries(map).forEach(([local, server]) => {
                  const serverId = String(server);
                  const localId = String(local);
                  if (!reverse[serverId]) reverse[serverId] = [];
                  reverse[serverId].push(localId);
                });
                const locals = reverse[duplicateId] || [];
                for (const lid of locals) idSet.add(lid);
                
                // Find dependent interactions (evaluations or system/note referencing target)
                for (const it of merged) {
                  const repliesToTarget = it.replyTo && idSet.has(it.replyTo);
                  const relatesToTarget = Array.isArray(it.relations) && it.relations.some((r: { type: string; targetId: string }) => r && idSet.has(r.targetId));
                  const isEval = typeof it.subkind === 'string' && it.subkind.startsWith('eval.');
                  const isSystemOrNote = it.kind === 'system' || it.kind === 'note';
                  if ((repliesToTarget || relatesToTarget) && (isEval || isSystemOrNote)) {
                    idSet.add(it.id);
                  }
                }
                
                const idsToDelete = Array.from(idSet);
                
                // Delete on server (best-effort for each; ignore 404s)
                for (const id of idsToDelete) {
                  try {
                    await api.deleteProjectInteraction(projectId, id);
                  } catch (err: any) {
                    const status = err?.response?.status;
                    if (status !== 404) {
                      console.warn('[hydrateInteractions] Failed to delete duplicate message on server', {
                        id,
                        status,
                        error: err,
                      });
                    }
                  }
                }
                
                console.log('[hydrateInteractions] Deleted duplicate user message and dependents', {
                  duplicateId,
                  deletedIds: idsToDelete,
                });
              } catch (e) {
                console.warn('[hydrateInteractions] Failed to delete duplicate user message', {
                  duplicateId,
                  error: e,
                });
              }
            }
          })();
        }
        
        // Clear global queries/sources on hydration - chips are now per-interaction only
        // Global state is only temporary during streaming and should not persist
        
        const restoredMeta = { ...s.messageMeta };
        
        // Generate frameExpansions for assistant interactions that don't have them yet
        // Include both LLM responses and agent responses (bring-in agents)
        // Only generate for the last assistant interaction if it doesn't have an expansion interaction
        // Exclude ontology interactions and graph.chat responses (they are structured data, not conversational content)
        const assistantInteractions = merged.filter((it: any) => 
          it.kind === 'assistant' && 
          it.content && 
          it.content.trim() &&
          it.subkind !== 'graph.chat'
        );
        if (assistantInteractions.length >= 1) {
          // Generate expansions for the last assistant interaction if it doesn't have them
          const lastAssistant = assistantInteractions[assistantInteractions.length - 1];
          
          // Check if expansion interaction already exists
          const map = (s as any).persistedByLocalId || {};
          const serverId = map[lastAssistant.id] || lastAssistant.id;
          const reverseMap: Record<string, string[]> = {};
          Object.entries(map).forEach(([local, server]) => {
            if (!reverseMap[server as string]) reverseMap[server as string] = [];
            reverseMap[server as string].push(local);
          });
          const localIds = reverseMap[lastAssistant.id] || [];
          
          const existingExpansion = merged.find((it: any) => 
            it.subkind === 'eval.frame_expansions' && 
            (it.replyTo === lastAssistant.id || it.replyTo === serverId || localIds.includes(it.replyTo || '') ||
             (it.relations && it.relations.some((r: { type: string; targetId: string }) => r.targetId === lastAssistant.id || r.targetId === serverId)))
          );
          
          if (!existingExpansion && lastAssistant.content) {
            // Find the last 2 user messages that prompted this response
            const userMessages: string[] = [];
            const replyTo = (lastAssistant as any).replyTo;
            if (replyTo) {
              // Start from replyTo and collect up to 2 user messages
              const replyToIdx = merged.findIndex((it: any) => 
                it.id === replyTo || (it as any).originalMessageId === replyTo
              );
              if (replyToIdx >= 0) {
                // Collect user messages before and including replyTo (up to 2 most recent)
                  for (let i = replyToIdx; i >= 0 && userMessages.length < 2; i--) {
                    const item = merged[i];
                    // Exclude graph.chat user messages (they are ontology commands, not conversational content)
                    if (item.kind === 'user' && 
                        item.subkind !== 'graph.chat' &&
                        (item.content || (item as any).contentSanitized)) {
                    const content = (item.content || (item as any).contentSanitized || '') as string;
                    if (content.trim()) {
                      userMessages.unshift(content); // Add to beginning to maintain chronological order
                    }
                  }
                }
              }
            } else {
              // If no explicit replyTo, find the last 2 user messages before this assistant message
              const assistantIdx = merged.findIndex((it: any) => it.id === lastAssistant.id && it.kind === 'assistant');
              if (assistantIdx > 0) {
                  for (let i = assistantIdx - 1; i >= 0 && userMessages.length < 2; i--) {
                    const item = merged[i];
                    // Exclude graph.chat user messages (they are ontology commands, not conversational content)
                    if (item.kind === 'user' && 
                        item.subkind !== 'graph.chat' &&
                        (item.content || (item as any).contentSanitized)) {
                    const content = (item.content || (item as any).contentSanitized || '') as string;
                    if (content.trim()) {
                      userMessages.unshift(content); // Add to beginning to maintain chronological order
                    }
                  }
                }
              }
            }
            
            // Generate asynchronously without blocking hydration
            // Skip evaluations for agent responses and scenario messages (they are system responses, not conversational content)
            const isAgentResponse = lastAssistant.subkind?.startsWith('agent.') || !!(lastAssistant as any).agentProfile;
            const isScenarioMessage = lastAssistant.subkind?.startsWith('scenarios.');
            (async () => {
              // Skip frame expansions and compliance check for agent responses and scenario messages
              if (!isAgentResponse && !isScenarioMessage) {
                try {
                  const { generateFrameExpansions } = await import('../lib/frameExpansions');
                  await generateFrameExpansions(lastAssistant.id, lastAssistant.content, userMessages.length > 0 ? userMessages : undefined);
                  
                  try {
                    const { generateFrameComplianceCheck } = await import('../lib/frameComplianceCheck');
                    await generateFrameComplianceCheck(lastAssistant.id, lastAssistant.content, userMessages.length > 0 ? userMessages : undefined);
                  } catch (e) {
                    console.warn('[hydrateInteractions] Failed to generate frameComplianceCheck', e);
                  }
                } catch (e) {
                  console.warn('[hydrateInteractions] Failed to generate frameExpansions', e);
                }
              }
            })();
          }
        }
        
        // Extract perspectives from interactions and build perspectives cache
        // Preserve existing perspectives and only update/add from merged interactions
        const perspectivesMap: Record<string, { id: string; content: string; preset: string; createdAt: string }> = { ...s.perspectives };
        merged.forEach((it: any) => {
          if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
            const preset = it.subkind.replace(/^perspective\./, '');
            if (preset && it.content) {
              // Keep the latest perspective for each preset
              const existing = perspectivesMap[preset];
              if (!existing || (it.createdAt && existing.createdAt && it.createdAt > existing.createdAt)) {
                perspectivesMap[preset] = {
                  id: it.id,
                  content: it.content,
                  preset,
                  createdAt: it.createdAt || new Date().toISOString(),
                };
              }
            }
          }
        });
        
        // Remove perspectives that are no longer in merged interactions
        // Only remove if the perspective ID doesn't exist in merged at all
        Object.keys(perspectivesMap).forEach((preset) => {
          const perspectiveId = perspectivesMap[preset].id;
          const stillExists = merged.some((it: any) => it.id === perspectiveId);
          if (!stillExists) {
            delete perspectivesMap[preset];
          }
        });
        
        return { 
          interactions: merged,
          perspectives: perspectivesMap,
          searchQueries: [],
          sources: [],
          messageMeta: restoredMeta,
          hydrating: false,
          hydratingProjectId: undefined,
        } as any;
      });
      
      // Check if discussion flow analysis exists, generate if missing
      (async () => {
        try {
          if (!projectId || projectId === 'local-unfold') return;
          
          // Wait a bit for state to settle, then check
          await new Promise(resolve => setTimeout(resolve, 100));
          
          const state = get();
          
          // Check if analysis already exists
          const existingAnalysisInteraction = state.interactions.find((it) => 
            it.subkind === 'discussion.flow.analysis' && it.projectId === projectId
          );
          
          if (existingAnalysisInteraction) {
            return;
          }
          
          // Get all chat messages (user, assistant) from current state
          const chatMessages = state.interactions
            .filter((it) => it.kind === 'user' || it.kind === 'assistant')
            .map((it) => ({
              role: it.kind === 'user' ? 'user' : 'assistant',
              content: it.content || '',
            }));
          
          // Only generate if we have at least 2 messages (user + assistant)
          if (chatMessages.length < 2) {
            return;
          }
          
          const { generateDiscussionFlowAnalysis } = await import('../lib/discussionFlowAnalysis');
          await generateDiscussionFlowAnalysis(projectId, chatMessages, null, undefined);
        } catch (e) {
          console.warn('[hydrateInteractions] Failed to generate discussion flow analysis', e);
        }
      })();
      
      // Check if user stance analysis exists, generate if missing
      (async () => {
        try {
          if (!projectId || projectId === 'local-unfold') return;
          
          // Wait a bit for state to settle, then check
          await new Promise(resolve => setTimeout(resolve, 100));
          
          const state = get();
          
          // Check if analysis already exists
          const existingAnalysisInteraction = state.interactions.find((it) => 
            it.subkind === 'eval.stance' && it.projectId === projectId && !it.replyTo
          );
          
          if (existingAnalysisInteraction) {
            return;
          }
          
          // Get all chat messages (user, assistant) from current state
          const chatMessages = state.interactions
            .filter((it) => it.kind === 'user' || it.kind === 'assistant')
            .map((it) => ({
              role: it.kind === 'user' ? 'user' : 'assistant',
              content: it.content || '',
            }));
          
          // Only generate if we have at least 2 messages (user + assistant)
          if (chatMessages.length < 2) {
            return;
          }
          
          const { generateUserStanceAnalysis } = await import('../lib/userStanceAnalysis');
          await generateUserStanceAnalysis(projectId, chatMessages, null, undefined);
        } catch (e) {
          console.warn('[hydrateInteractions] Failed to generate user stance analysis', e);
        }
      })();
    } catch (e: any) {
      // Log with status code for debugging
      const status = e?.response?.status;
      if (status === 500) {
        console.error('[hydrateInteractions] Server error (500) - project may be initializing', { projectId });
      } else {
        console.error('[hydrateInteractions] Failed to load interactions', { projectId, status, error: e });
      }
      set({ hydrating: false, hydratingProjectId: undefined } as any);
    }
  },

  // Add or update a specific interaction by fetching it from server
  // Used when interaction.created/updated event is received via WebSocket
  addOrUpdateInteraction: async (interactionId: string) => {
    const projectId = get().projectId;
    if (!projectId || projectId === 'local-unfold') return;
    
    try {
      console.log('[addOrUpdateInteraction] Fetching interaction', { projectId, interactionId });
      // Fetch all interactions and find the one we need
      const serverItems = await api.getProjectInteractions(projectId);
      const interaction = (serverItems as any[]).find((it: any) => it.id === interactionId);
      
      if (!interaction) {
        console.warn('[addOrUpdateInteraction] Interaction not found', { interactionId });
        return;
      }
      
      // Check if projectId is still current before applying results
      set((s) => {
        if (s.projectId !== projectId) {
          return s;
        }
        
        // Process the interaction (same logic as in hydrateInteractions)
        const processedServerItems = [interaction].map((it: any) => {
          let updated = it;
          
          // Обработка contentSanitized
          if (it.kind === 'assistant' && it.content && !it.contentSanitized) {
            updated = { ...updated, contentSanitized: it.content };
          }
          
          // Обработка graphData - добавляем chartType если отсутствует или невалиден
          if (it.kind === 'assistant' && (it.subkind === 'graph' || it.graphData)) {
            let graphData = it.graphData;
            
            // Если graphData отсутствует, но есть subkind === 'graph', пытаемся распарсить из content
            if (!graphData && it.subkind === 'graph' && (it.contentSanitized || it.content)) {
              try {
                let content = String(it.contentSanitized || it.content || '').trim();
                content = content.replace(/^```[a-zA-Z]*\n([\s\S]*?)\n```$/m, '$1').trim();
                let parsed: any = null;
                if (content.startsWith('{') || content.startsWith('[')) {
                  parsed = JSON.parse(content);
                } else {
                  const m = content.match(/\{[\s\S]*\}/);
                  if (m && m[0]) parsed = JSON.parse(m[0]);
                }
                if (parsed && typeof parsed === 'object' && typeof parsed.chartTitle === 'string' && Array.isArray(parsed.table) && Array.isArray(parsed.sources)) {
                  const allowedTypes = ['line', 'bar', 'area', 'stackedBar', 'stackedArea', 'composed'];
                  const chartType = typeof parsed.chartType === 'string' && allowedTypes.includes(parsed.chartType) ? parsed.chartType : 'line';
                  graphData = { chartTitle: parsed.chartTitle, chartType: chartType, table: parsed.table, sources: parsed.sources };
                }
              } catch {}
            }
            
            // Если graphData есть, но нет chartType или он невалиден - добавляем/исправляем
            if (graphData && typeof graphData === 'object' && typeof graphData.chartTitle === 'string') {
              const allowedTypes = ['line', 'bar', 'area', 'stackedBar', 'stackedArea', 'composed'];
              const chartType = typeof graphData.chartType === 'string' && allowedTypes.includes(graphData.chartType)
                ? graphData.chartType
                : 'line';
              updated = { ...updated, graphData: { ...graphData, chartType } };
            }
          }
          
          return updated;
        });
        
        const processedInteraction = processedServerItems[0];
        
        // Check if interaction already exists (by ID, originalMessageId, or by graph data for graphs to avoid duplicates)
        const existingIndex = s.interactions.findIndex((it) => {
          if (it.id === interactionId) return true;
          // Also check by originalMessageId to catch duplicates from WebSocket events
          const newOriginalMsgId = (processedInteraction as any).originalMessageId;
          if (newOriginalMsgId && (it as any).originalMessageId === newOriginalMsgId) {
            return true;
          }
          // For user interactions, also check by content and createdAt to catch duplicates
          // This handles the case where WebSocket event arrives before local ID is updated
          // Also handles race conditions where parallel calls create interactions with different IDs
          if (processedInteraction.kind === 'user' && it.kind === 'user') {
            const newContent = processedInteraction.content;
            const newCreatedAt = processedInteraction.createdAt;
            if (newContent && it.content === newContent) {
              // Check if this is likely the same interaction (within 2 seconds)
              // Increased window to match startStream check and handle server timestamp differences
              if (newCreatedAt && it.createdAt) {
                const newTime = new Date(newCreatedAt).getTime();
                const existingTime = new Date(it.createdAt).getTime();
                if (Math.abs(newTime - existingTime) < 2000) {
                  console.log('[addOrUpdateInteraction] Duplicate user message detected by content+time', {
                    existingId: it.id,
                    newId: interactionId,
                    content: newContent.substring(0, 50),
                    timeDiff: Math.abs(newTime - existingTime)
                  });
                  return true;
                }
              } else if (newContent === it.content) {
                // If createdAt is missing but content matches exactly, treat as duplicate
                // This handles edge cases where timestamps might not be set
                console.log('[addOrUpdateInteraction] Duplicate user message detected by content only', {
                  existingId: it.id,
                  newId: interactionId,
                  content: newContent.substring(0, 50)
                });
                return true;
              }
            }
          }
          // For graphs, also check by graphData to avoid duplicates when tool result creates local interaction
          if (it.subkind === 'graph' && processedInteraction.subkind === 'graph') {
            const existingGraph = it.graphData;
            const newGraph = processedInteraction.graphData;
            if (existingGraph && newGraph && 
                existingGraph.chartTitle === newGraph.chartTitle &&
                JSON.stringify(existingGraph.table) === JSON.stringify(newGraph.table)) {
              return true;
            }
          }
          return false;
        });
        
        if (existingIndex >= 0) {
          // Update existing interaction (merge data, prefer server data but keep local graphData if server doesn't have it)
          const updated = [...s.interactions];
          const existing = updated[existingIndex];
          // Merge: keep local graphData if server doesn't have it, but prefer server data
          if (existing.subkind === 'graph' && existing.graphData && !processedInteraction.graphData) {
            updated[existingIndex] = { ...processedInteraction, graphData: existing.graphData } as LocalInteraction;
          } else {
            updated[existingIndex] = processedInteraction as LocalInteraction;
          }
          console.log('[addOrUpdateInteraction] Updated existing interaction', { interactionId, subkind: processedInteraction.subkind });
          return { interactions: updated } as any;
        } else {
          // Add new interaction (insert at appropriate position based on createdAt)
          const updated = [...s.interactions];
          const insertIndex = updated.findIndex((it) => {
            const itTime = new Date(it.createdAt).getTime();
            const newTime = new Date(processedInteraction.createdAt).getTime();
            return itTime > newTime;
          });
          
          if (insertIndex >= 0) {
            updated.splice(insertIndex, 0, processedInteraction as LocalInteraction);
          } else {
            updated.push(processedInteraction as LocalInteraction);
          }
          
          console.log('[addOrUpdateInteraction] Added new interaction', { interactionId, insertIndex, subkind: processedInteraction.subkind });
          return { interactions: updated } as any;
        }
      });
    } catch (e) {
      console.error('[addOrUpdateInteraction] Failed to add/update interaction', { interactionId, error: e });
    }
  },

  loadStanceAggregate: async () => {
    const projectId = get().projectId;
    if (!projectId || projectId === 'local-unfold') return;
    
    // Защита от множественных одновременных вызовов
    const state = get();
    if (state.stanceLoading) {
      console.log('[loadStanceAggregate] Already loading, skipping duplicate call');
      return;
    }
    
    set({ stanceLoading: true });
    try {
      const agg = await api.metaGetAggregate(projectId);
      // Check if projectId is still current before applying results (race condition guard)
      set((s) => {
        if (s.projectId !== projectId) {
          return { stanceLoading: false };
        }
        return { stanceAggregate: agg, stanceLoading: false };
      });
    } catch (e: any) {
      // 404 is normal for projects without aggregate yet - silently ignore
      if (e?.response?.status === 404) {
        // Don't log - 404 is expected for new projects without analysis
      } else {
        console.warn('[loadStanceAggregate] Failed to load aggregate', e);
      }
      set({ stanceLoading: false });
    }
  },

  ensureStanceAggregate: async () => {
    const projectId = get().projectId;
    if (!projectId || projectId === 'local-unfold') return;
    
    // META ANALYSIS GENERATION IS DISABLED (see onDone around line 2019)
    // Therefore, loading aggregate only makes sense for old projects that already have it
    // Skip for new projects since no new aggregates will be generated
    
    // Don't load aggregate if project is empty or has very few interactions
    const interactions = get().interactions;
    const hasEnoughInteractions = interactions.length >= 3;
    if (!hasEnoughInteractions) {
      return;
    }
    
    // Only load existing aggregate from old projects; skip silently if not found
    // Since meta analysis generation is disabled, new projects won't have aggregates
    try {
      set({ stanceLoading: true });
      await get().loadStanceAggregate();
    } catch (e: any) {
      // 404 is expected - meta analysis generation is disabled, so new projects won't have aggregates
      if (e?.response?.status !== 404) {
        console.warn('[ensureStanceAggregate] Error loading aggregate', e);
      }
    } finally {
      set({ stanceLoading: false });
    }
  },

  deleteInteraction: async (interactionId: string) => {
    const projectId = get().projectId;
    if (!projectId || projectId === 'local-unfold') return;

    const interactions = get().interactions;
    const targetIdx = interactions.findIndex((it) => it.id === interactionId);
    if (targetIdx === -1) return;

    // Build cascade set: include selected id and any evaluations/system notes tied via replyTo or relations
    const idSet = new Set<string>();
    idSet.add(interactionId);

    // Expand with local/server mapping variants
    try {
      const map = (get() as any).persistedByLocalId || ({} as Record<string, string>);
      // If selected id maps to server id, include it
      if (map[interactionId]) idSet.add(map[interactionId]);
      // Reverse map: collect all locals that point to selected id
      const reverse: Record<string, string[]> = {};
      Object.entries(map).forEach(([local, server]) => {
        const serverId = String(server);
        const localId = String(local);
        if (!reverse[serverId]) reverse[serverId] = [];
        reverse[serverId].push(localId);
      });
      const locals = reverse[interactionId] || [];
      for (const lid of locals) idSet.add(lid);
    } catch {}

    // Find dependent interactions (evaluations or system/note referencing target)
    for (const it of interactions) {
      const repliesToTarget = it.replyTo && idSet.has(it.replyTo);
      const relatesToTarget = Array.isArray(it.relations) && it.relations.some((r: { type: string; targetId: string }) => r && idSet.has(r.targetId));
      const isEval = typeof it.subkind === 'string' && it.subkind.startsWith('eval.');
      const isSystemOrNote = it.kind === 'system' || it.kind === 'note';
      if ((repliesToTarget || relatesToTarget) && (isEval || isSystemOrNote)) {
        idSet.add(it.id);
      }
    }

    const idsToDelete = Array.from(idSet);

    // Delete on server (best-effort for each; ignore 404s)
    try {
      await Promise.all(idsToDelete.map(async (id) => {
        try {
          await api.deleteProjectInteraction(projectId, id);
        } catch (err: any) {
          const status = err?.response?.status;
          if (status !== 404) {
            // Log but don't block local cleanup for dependents
            console.warn('[deleteInteraction] Server delete failed', { id, status, err });
            // Only throw if it's the main selected id and error isn't 404
            if (id === interactionId) throw err;
          }
        }
      }));
    } catch (e) {
      console.error('[deleteInteraction] Failed to delete selected interaction on server', e);
      throw e;
    }

    // Local cleanup: remove interactions and any mapped ids from persisted map
    set((s) => {
      const toDelete = new Set(idsToDelete);
      const nextInteractions = s.interactions.filter((it) => !toDelete.has(it.id));
      const nextMessages = s.messages.filter((m) => !toDelete.has(m.id));
      const nextMap: Record<string,string> = { ...(s.persistedByLocalId || {}) } as any;
      for (const [k, v] of Object.entries(nextMap)) {
        if (toDelete.has(k) || toDelete.has(v)) {
          delete nextMap[k];
        }
      }
      // Update perspectives cache: remove deleted perspectives
      const nextPerspectives = { ...s.perspectives };
      for (const it of s.interactions) {
        if (toDelete.has(it.id) && it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
          const preset = it.subkind.replace(/^perspective\./, '');
          if (preset && nextPerspectives[preset]?.id === it.id) {
            delete nextPerspectives[preset];
          }
        }
      }
      return { interactions: nextInteractions, messages: nextMessages, persistedByLocalId: nextMap, perspectives: nextPerspectives } as any;
    });

    console.log('[deleteInteraction] Deleted interactions (cascade)', { idsToDelete });
  },

  createIndicatorMessage: async (content: string, subkind: string, replyTo?: string, metadata?: Record<string, any>) => {
    const projectId = get().projectId;
    if (!projectId || projectId === 'local-unfold') return;
    
    const now = Date.now().toString();
    const createdAt = new Date().toISOString();
    const localId = now + '-indicator';
    
    try {
      // Create local interaction first
      const localInteraction: LocalInteraction = {
        id: localId,
        projectId,
        kind: 'user',
        subkind,
        content,
        createdAt,
        replyTo,
        ...(metadata || {}),
      };
      
      set((st) => ({
        interactions: [...st.interactions, localInteraction],
      }));
      
      // Resolve replyTo to server ID if needed
      let serverReplyTo: string | undefined = undefined;
      if (replyTo) {
        const map = (get() as any).persistedByLocalId || {};
        serverReplyTo = map[replyTo] || map[String(replyTo)] || replyTo;
      }
      
      // Persist on server
      const payload: any = {
        kind: 'user',
        subkind,
        content,
        originalMessageId: now + '-u',
      };
      
      if (serverReplyTo) payload.replyTo = serverReplyTo;
      if (metadata) {
        Object.assign(payload, metadata);
      }
      
      const res = await api.createProjectInteraction(projectId, payload);
      
      // Update local interaction with server ID
      set((st) => {
        const items = [...st.interactions];
        const idx = items.findIndex((it) => it.id === localId);
        if (idx >= 0) {
          items[idx] = { ...items[idx], id: res.id } as LocalInteraction;
        }
        const map = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
        map[localId] = res.id;
        map[now + '-u'] = res.id;
        return { interactions: items, persistedByLocalId: map } as any;
      });
    } catch (e) {
      console.warn('[createIndicatorMessage] Failed to create indicator message', e);
    }
  },

  openMetricOverlay: async (metric: SxMonitorMetric) => {
    const key = String((metric as any).id || (metric as any).metricName || (metric as any).name || '').toLowerCase();
    if (!key) return;
    const st = get();
    // If already cached, just open
    if (st.metricOverlayCache[key]?.content) {
      set({ metricOverlayOpenKey: key });
      return;
    }
    set({ metricOverlayLoading: true, metricOverlayOpenKey: key });
    try {
      const settings = useGenAISettings.getState();
      const model = settings.model;
      const preferred = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN');
      // First user request in this Unfold session, fallback to earliest message
      let firstRequest = '';
      try {
        const inters = get().interactions;
        const firstUser = inters.find((it) => it.kind === 'user');
        if (firstUser) firstRequest = firstUser.content || '';
        if (!firstRequest) {
          const msgs = get().messages;
          const firstMsg = msgs.find((m) => m.role === 'user');
          if (firstMsg) firstRequest = firstMsg.content || '';
        }
      } catch {}
      const today = new Date();
      const dateStr = today.toISOString().slice(0, 10);
      const metricJson = JSON.stringify(metric, null, 2);
      const prompt = `User requirement: you should create your answer in ${preferred} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nCurrentDate: ${dateStr}\n\nWe need a detailed, up-to-date insight for the following metric. Return ONLY Markdown.\n\nRequirements:\n- Start with a compact table summarizing current understanding of the metric (name, definition, unit, frequency, thresholds if any, typical ranges, data sources).\n- Follow with a concise article: definition, why it matters, how to track, caveats, and suggested next steps.\n- Use headings (###), bullet lists, and short paragraphs.\n- If data is uncertain, clearly say so and propose how to validate.\n\nContext:\n- METRIC:\n${metricJson}\n\n- ORIGINAL_USER_REQUEST:\n${firstRequest}\n\nUse neutral, factual tone.`;
      const maxTok = Math.max(2048, settings.maxOutputTokens || 4096);
      const res = await api.genaiGenerate({ model, prompt, strictJSONEnabled: false, maxOutputTokens: maxTok, webSearchEnabled: true, systemInstruction: settings.systemInstruction || undefined });
      const content = (res?.content || '').toString().trim();
      set((s) => ({ metricOverlayCache: { ...s.metricOverlayCache, [key]: { key, metric, content, fetchedAt: new Date().toISOString() } }, metricOverlayLoading: false, metricOverlayOpenKey: key } as any));
    } catch (e) {
      console.error('[metricOverlay] fetch failed', e);
      set({ metricOverlayLoading: false });
    }
  },
  closeMetricOverlay: () => {
    set({ metricOverlayOpenKey: null });
  },

  startStream: async ({ prompt, tools, options, agentMeta, suppressUserEcho }) => {
    const currentState = get();
    if (currentState.streaming) {
      console.warn('[Chat] startStream: Already streaming, blocking new request', {
        projectId: currentState.projectId,
        messagesCount: currentState.messages.length
      });
      return;
    }
    
    // Set streaming flag immediately to prevent parallel calls (race condition protection)
    set({ streaming: true } as any);
    
    // CRITICAL: Жесткая синхронизация - получаем projectId ДО отправки сообщения
    // Это гарантирует отсутствие гонок при создании нового проекта
    let projectId: string;
    try { 
      // Шаг 1: Вызываем ensureProject и ждем завершения
      projectId = await get().ensureProject();
      
      // Шаг 2: Проверяем, что получили валидный projectId
      if (!projectId || projectId === 'local-unfold') {
        console.error('[Chat] startStream: Failed to get valid projectId after ensureProject', { projectId });
        set({ streaming: false, error: 'Failed to create or get project' });
        return;
      }
      
      // Шаг 3: Убеждаемся, что projectId установлен в store (может быть гонка)
      let storePid = get().projectId;
      if (!storePid || storePid === 'local-unfold') {
        console.warn('[Chat] startStream: projectId not in store after ensureProject, setting it', { 
          projectId, 
          storePid 
        });
        set({ projectId: projectId } as any);
        storePid = projectId;
      }
      
      // Шаг 4: Проверяем, что projectId в store совпадает с полученным
      // Если не совпадает, используем тот, что в store (может быть более свежий)
      if (storePid !== projectId) {
        console.warn('[Chat] startStream: projectId mismatch, using store value', { 
          ensureProjectResult: projectId, 
          storeValue: storePid 
        });
        projectId = storePid;
      }
      
      // Шаг 5: Финальная проверка перед продолжением
      if (!projectId || projectId === 'local-unfold') {
        console.error('[Chat] startStream: Invalid projectId after all checks', { projectId });
        set({ streaming: false, error: 'Failed to get valid projectId' });
        return;
      }
      
      console.log('[Chat] startStream: projectId confirmed before stream', { projectId });
    } catch (e) {
      console.error('[Chat] startStream: ensureProject failed', e);
      set({ streaming: false, error: 'Failed to create project' });
      return;
    }
    
    // Теперь projectId гарантированно установлен - можно продолжать
    // Ensure WS subscription is active before starting stream to avoid missing early chunks
    // Используем уже проверенный projectId из переменной выше
      if ((window as any).wsSubscribeProject && projectId) {
      try {
        await (window as any).wsSubscribeProject(projectId, { timeoutMs: 6000, force: true });
    } catch (e) {
        console.error('[Chat] startStream: WS subscription confirmation failed', e);
        set({ streaming: false, error: 'Не удалось подключиться к realtime каналу. Попробуйте снова.' });
        return;
      }
    } else {
      console.warn('[Chat] startStream: wsSubscribeProject helper missing', { hasHelper: !!(window as any).wsSubscribeProject, projectId });
    }
    
    // Используем уже проверенный projectId - не читаем из store снова
    const now = Date.now().toString();
    const createdAt = new Date().toISOString();
    // add user message and a placeholder assistant message
    set((s) => {
      // prune trailing empty assistant placeholders to avoid stale skeletons
      const pruned = [...s.messages];
      while (pruned.length > 0) {
        const last = pruned[pruned.length - 1];
        if (last.role === 'assistant' && (!last.content || last.content.trim().length === 0)) {
          pruned.pop();
          continue;
        }
        break;
      }
      // Check if interaction with same originalMessageId already exists to prevent duplicates
      // Also check by content + createdAt to catch duplicates from parallel calls (race condition)
      const existingByOriginalMsgId = suppressUserEcho ? null : s.interactions.find(
        (it) => {
          if (it.kind !== 'user') return false;
          // Check by originalMessageId
          if ((it as any).originalMessageId === now + '-u') return true;
          // Check by content + createdAt (within 2 seconds) to catch race conditions
          // This handles the case where two parallel calls create interactions with different originalMessageIds
          // but same content and nearly same time
          if (it.content === prompt && it.createdAt) {
            const itTime = new Date(it.createdAt).getTime();
            const newTime = new Date(createdAt).getTime();
            if (Math.abs(itTime - newTime) < 2000) {
              return true;
            }
          }
          return false;
        }
      );
      if (existingByOriginalMsgId && !suppressUserEcho) {
        console.warn('[Chat] startStream: Duplicate user message detected, skipping interaction creation', {
          existingId: existingByOriginalMsgId.id,
          existingOriginalMsgId: (existingByOriginalMsgId as any).originalMessageId,
          newOriginalMsgId: now + '-u',
          content: prompt.substring(0, 50)
        });
      }
      return ({
      messages: [
        ...pruned,
        ...(suppressUserEcho ? [] : [{ id: now + '-u', role: 'user', content: prompt } as ChatMessage]),
        { id: now + '-a', role: 'assistant', content: '' },
      ],
      streaming: true,
      error: null,
      pendingAssistantMeta: agentMeta,
      pendingAssistantOptions: options,
      pendingAssistantPrompt: prompt,
      pendingAssistantTools: tools,
      // also record local interaction for user unless suppressed
      interactions: [
        ...s.interactions,
        ...(suppressUserEcho || existingByOriginalMsgId ? [] : [{ 
          id: now + '-iu', 
          projectId, 
          kind: 'user' as InteractionKind, 
          subkind: agentMeta?.subkind, // Сохраняем subkind из agentMeta для фильтрации по вкладкам
          content: prompt, 
          createdAt, 
          originalMessageId: now + '-u' 
        } as LocalInteraction]),
      ],
    }) as any;});
    
    // Generate project title immediately after first user message (don't wait for assistant response)
    // Capture projectId in closure to ensure stability
    if (!suppressUserEcho && projectId && projectId !== 'local-unfold') {
      const capturedProjectId = projectId; // Capture projectId to prevent race conditions
      (async () => {
        try {
          // Verify projectId is still current before proceeding
          const currentState = get();
          if (currentState.projectId !== capturedProjectId) {
            console.log('[ProjectTitle] Project changed, skipping title generation', { 
              capturedProjectId, 
              currentProjectId: currentState.projectId 
            });
            return;
          }
          
          // Check if project already has a title (not Draft)
          const currentProject = useProjectsStore.getState().currentProject;
          // Ensure both IDs are strings for comparison
          const currentProjectId = currentProject?.id ? (typeof currentProject.id === 'string' ? currentProject.id : String(currentProject.id)) : null;
          // Verify we're still working with the same project
          if (currentProjectId !== capturedProjectId) {
            console.log('[ProjectTitle] Project mismatch in store, skipping', { 
              capturedProjectId, 
              storeProjectId: currentProjectId 
            });
            return;
          }
          
          const projectName = (currentProject?.displayName || '').trim();
          const hasTitle = projectName && projectName.toLowerCase() !== 'draft' && projectName.length > 0;
          
          // Check if title was already finalized (localStorage check)
          const titleFinalized = typeof window !== 'undefined' && 
            window.localStorage.getItem(`chatTitleFinal:${capturedProjectId}`) === '1';
          
          // Get current user messages count
          const state = get();
          // Double-check projectId hasn't changed
          if (state.projectId !== capturedProjectId) {
            console.log('[ProjectTitle] Project changed during check, skipping', { 
              capturedProjectId, 
              currentProjectId: state.projectId 
            });
            return;
          }
          
          const userMessagesFromInteractions = state.interactions.filter((it) => it.kind === 'user');
          const userMessagesFromMessages = state.messages.filter((m) => m.role === 'user');
          const totalUserMessages = Math.max(userMessagesFromInteractions.length, userMessagesFromMessages.length);
          
          console.log('[ProjectTitle] Checking conditions in startStream', { 
            projectId: capturedProjectId, 
            totalUserMessages,
            hasTitle,
            titleFinalized
          });
          
          // Generate title if:
          // - This is the first user message (totalUserMessages === 1 after adding this message)
          // - Project doesn't have a title yet (or is Draft)
          // - Title hasn't been finalized yet
          if (totalUserMessages === 1 && !hasTitle && !titleFinalized) {
            const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
            const { generateProjectTitle } = await import('../lib/projectTitleGeneration');
            console.log('[ProjectTitle] Calling generateProjectTitle from startStream', { projectId: capturedProjectId, preferredLang });
            await generateProjectTitle({ projectId: capturedProjectId, preferredLang });
          }
        } catch (e) {
          console.warn('[ProjectTitle] Title generation failed in startStream', e);
        }
      })();
    }
    
    // Direct command detection from prompt (before LLM call to avoid unnecessary latency)
    // Check if prompt starts with a known command
    let detectedCommand: string | undefined = undefined;
    let detectedParams: string | undefined = undefined;
    
    if (prompt && typeof prompt === 'string') {
      const trimmed = prompt.trim();
      
      // Check for exact command matches first
      if (trimmed === '/end') {
        detectedCommand = '/end';
        detectedParams = '';
      } else if (trimmed.startsWith('/section ')) {
        detectedCommand = '/section';
        detectedParams = trimmed.replace(/^\s*\/section\s+/i, '').trim();
      } else if (trimmed.startsWith('/analyze entities')) {
        detectedCommand = '/analyze entities';
        detectedParams = trimmed.replace(/^\s*\/analyze\s+entities\s*/i, '').trim();
      } else if (trimmed.startsWith('/analyze facts')) {
        detectedCommand = '/analyze facts';
        detectedParams = trimmed.replace(/^\s*\/analyze\s+facts\s*/i, '').trim();
      } else if (trimmed.startsWith('/analyze events')) {
        detectedCommand = '/analyze events';
        detectedParams = trimmed.replace(/^\s*\/analyze\s+events\s*/i, '').trim();
      } else if (trimmed.startsWith('/analyze metrics')) {
        detectedCommand = '/analyze metrics';
        detectedParams = trimmed.replace(/^\s*\/analyze\s+metrics\s*/i, '').trim();
      } else if (trimmed.startsWith('/analyze decisions')) {
        detectedCommand = '/analyze decisions';
        detectedParams = trimmed.replace(/^\s*\/analyze\s+decisions\s*/i, '').trim();
      } else if (trimmed.startsWith('/agent data')) {
        detectedCommand = '/agent data';
        detectedParams = trimmed.replace(/^\s*\/agent\s+data\s*/i, '').trim();
      } else if (trimmed.startsWith('/agent entity')) {
        detectedCommand = '/agent entity';
        detectedParams = trimmed.replace(/^\s*\/agent\s+entity\s*/i, '').trim();
      }
      
      // Debug logging
      if (trimmed.startsWith('/')) {
        console.log('[Command] Detection:', { trimmed, detectedCommand, detectedParams });
      }
    }
    
    // If command detected, execute it instead of running GenAI
    if (detectedCommand) {
      try {
        // Remove assistant placeholder
        set((s) => {
          const messages = [...s.messages];
          if (messages.length > 0 && messages[messages.length - 1].role === 'assistant' && !messages[messages.length - 1].content) {
            messages.pop(); // Remove placeholder
          }
          return { messages, streaming: false } as any;
        });

        // Ensure USER interaction is persisted before creating ACTION
        if (!suppressUserEcho) {
          try {
            const userLocalId = now + '-iu';
            const meta = agentMeta as any;
            const payload: any = { kind: 'user', content: prompt, originalMessageId: now + '-u' };
            if (meta?.subkind) payload.subkind = meta.subkind;
            if (meta?.replyTo) {
              const map = (get() as any).persistedByLocalId || {};
              const serverReply = map[meta.replyTo] || map[String(meta.replyTo)];
              if (serverReply) payload.replyTo = serverReply;
            }
            if (meta?.agentProfile) payload.agentProfile = meta.agentProfile;
            // Attach document IDs if any
            const attachedDocIds = get().pendingAttachedDocIds;
            if (attachedDocIds && attachedDocIds.length > 0) {
              payload.coreDocIds = attachedDocIds;
              try { console.log('[Chat] Attaching documents to interaction', { docIds: attachedDocIds }); } catch {}
            }
            const resUser = await api.createProjectInteraction(projectId, payload);
            
            // Update local interaction with server ID to prevent duplicates from WebSocket events
            set((st) => {
              const items = [...st.interactions];
              const localId = now + '-iu';
              const idx = items.findIndex((it) => it.id === localId);
              if (idx >= 0) {
                // Check if already updated (prevent duplicate updates)
                if (items[idx].id === resUser.id) {
                  console.log('[Chat] User interaction already has server ID, skipping update', { interactionId: resUser.id });
                  return {} as any;
                }
                console.log('[Chat] Updating user interaction ID', { localId, serverId: resUser.id, idx });
                items[idx] = { ...items[idx], id: resUser.id } as LocalInteraction;
                const map = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
                map[localId] = resUser.id;
                map[now + '-u'] = resUser.id;
                return { interactions: items, persistedByLocalId: map } as any;
              } else {
                // Interaction not found - check if it was already added with server ID
                const existingByServerId = items.find((it) => it.id === resUser.id);
                if (existingByServerId) {
                  console.log('[Chat] User interaction already exists with server ID, skipping', { interactionId: resUser.id });
                  return {} as any;
                }
                console.warn('[Chat] User interaction not found for update', { localId, serverId: resUser.id, interactionsCount: items.length });
              }
              return {} as any;
            });
            
            // Create non-displayable interactions for attached documents
            if (attachedDocIds && attachedDocIds.length > 0) {
              try {
                // Load documents to get their content
                const docs = await api.docsList(projectId);
                const docMap = new Map(docs.map((d: any) => [d.id, d]));
                
                // Create document interactions
                const docInteractions: LocalInteraction[] = [];
                for (const docId of attachedDocIds) {
                  const doc = docMap.get(docId);
                  if (!doc) {
                    console.warn('[Chat] Document not found in list', { docId });
                    continue;
                  }
                  
                  // Always download the full document content
                  // For text files (CSV, TXT, etc.), docsDownload returns the original file which is the text
                  // For other files, it returns the original, but that's better than using a preview
                  let docContent = '';
                  const docSizeBytes = doc.sizeBytes || 0;
                  let contentSource = 'unknown';
                  
                  try {
                    console.log('[Chat] Downloading full document content', { docId, filename: doc.filename, contentType: doc.contentType, sizeBytes: docSizeBytes });
                    const blob = await api.docsDownload(projectId, docId);
                    docContent = await new Response(blob).text();
                    contentSource = 'download';
                    console.log('[Chat] Downloaded document content', { 
                      docId, 
                      filename: doc.filename,
                      contentLength: docContent.length,
                      contentLengthKB: (docContent.length / 1024).toFixed(2),
                      sizeBytes: docSizeBytes,
                      sizeKB: (docSizeBytes / 1024).toFixed(2)
                    });
                  } catch (downloadErr) {
                    // Fallback to extractedText if download fails
                    if (doc.extractedText && doc.extractedText.length > 0) {
                      console.warn('[Chat] Failed to download document, using extractedText fallback', { docId, error: downloadErr });
                      docContent = doc.extractedText;
                      contentSource = 'extractedText';
                    } else {
                      console.warn('[Chat] Failed to download document and no extractedText available', { docId, error: downloadErr });
                      continue; // Skip this document if we can't get its content
                    }
                  }
                  
                  if (docContent.length === 0) {
                    console.warn('[Chat] Document content is empty', { docId });
                    continue;
                  }
                  
                  // Calculate tokens added to context
                  const contentChars = docContent.length;
                  const contentTokens = Math.ceil(contentChars / 4);
                  
                  console.log('[Chat] Document added to context', {
                    docId,
                    filename: doc.filename,
                    sizeBytes: docSizeBytes,
                    sizeKB: (docSizeBytes / 1024).toFixed(2),
                    contentChars,
                    contentKB: (contentChars / 1024).toFixed(2),
                    contentTokens,
                    contentSource,
                    ratio: docSizeBytes > 0 ? ((contentChars / docSizeBytes) * 100).toFixed(1) + '%' : 'N/A'
                  });
                  
                  const docInteraction: LocalInteraction = {
                    id: `${now}-doc-${docId}`,
                    projectId,
                    kind: 'system',
                    subkind: 'document.attach',
                    content: docContent,
                    createdAt: new Date().toISOString(),
                    replyTo: resUser.id, // Link to user interaction
                  };
                  docInteractions.push(docInteraction);
                  
                  // Persist on server
                  try {
                    await api.createProjectInteraction(projectId, {
                      kind: 'system',
                      subkind: 'document.attach',
                      content: docContent,
                      replyTo: resUser.id,
                    });
                  } catch (e) {
                    console.warn('[Chat] Failed to persist document interaction', { docId, error: e });
                  }
                }
                
                // Add to local interactions
                if (docInteractions.length > 0) {
                  set((st) => ({
                    interactions: [...st.interactions, ...docInteractions],
                  } as any));
                }
              } catch (e) {
                console.warn('[Chat] Failed to create document interactions', { error: e });
              }
            }
            
            // Clear attached docs after creating interaction
            set({ pendingAttachedDocIds: [] });
            set((st) => {
              const items = [...st.interactions];
              const idx = items.findIndex((it) => it.id === userLocalId);
              if (idx >= 0) { (items[idx] as any).id = resUser.id; }
              const map2 = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
              map2[userLocalId] = resUser.id;
              map2[now + '-u'] = resUser.id;
              return { interactions: items, persistedByLocalId: map2 } as any;
            });
          } catch (e) {
            console.warn('[persist user-before-action] failed', e);
          }
        }

        // Create action interaction (to show intent in chat)
        try {
          const actionId = Date.now().toString() + '-ac';
          const createdAt = new Date().toISOString();
          const actionText = detectedCommand + (detectedParams ? (' ' + detectedParams) : '');
          const actionInter: LocalInteraction = {
            id: actionId,
            projectId,
            kind: 'action',
            content: actionText,
            createdAt,
            originalMessageId: now + '-u',
          };
          set((s) => ({ interactions: [...s.interactions, actionInter] }));
          // Persist action (best-effort)
          (async () => {
            try {
              const res = await api.createProjectInteraction(projectId, { kind: 'action', content: actionText, originalMessageId: now + '-u' });
              set((s) => {
                const map = { ...(s.persistedByLocalId || {}) } as Record<string, string>;
                map[actionId] = res.id;
                const items = [...s.interactions];
                const idx = items.findIndex((it) => it.id === actionId);
                if (idx >= 0) { (items[idx] as any).id = res.id; }
                return { interactions: items, persistedByLocalId: map } as any;
              });
            } catch (e) {
              console.warn('[Action] persist failed', e);
            }
          })();
        } catch {}

        // Execute command
        const cmd = detectedCommand.toLowerCase();
        const params = (detectedParams || '').trim();
        try {
          if (cmd === '/end') {
            await api.createProjectInteraction(projectId, { kind: 'section_end', content: '' });
            await get().hydrateInteractions();
            return;
          }
          if (cmd.startsWith('/section')) {
            // Use params as title
            await api.createProjectInteraction(projectId, { kind: 'section_start', content: params });
            await get().hydrateInteractions();
            return;
          }
          if (cmd === '/analyze entities') { await api.analyzeEntities(projectId, params); return; }
          if (cmd === '/analyze facts') { await api.analyzeFacts(projectId, params); return; }
          if (cmd === '/analyze events') { await api.analyzeEvents(projectId, params); return; }
          if (cmd === '/analyze metrics') { await api.analyzeMetrics(projectId, params); return; }
          if (cmd === '/analyze decisions') { await api.analyzeDecisions(projectId, params); return; }
          
          // Agent commands: /agent data <query> and /agent entity <query>
          if (cmd === '/agent data' || cmd === '/agent entity') {
            const agentType = cmd === '/agent data' ? 'data' : 'entities';
            const query = params || prompt.replace(/^\s*\/agent\s+(data|entity)\s+/i, '').trim();
            
            console.log('[Agent] Executing agent command', { cmd, agentType, query, params, projectId });
            
            if (!query) {
              console.warn('[Agent] Empty query provided');
              return;
            }
            
            try {
              // Import agents store
              const { useAgentsStore } = await import('./agentsStore');
              const agentsStore = useAgentsStore.getState();
              
              // Get current user context
              const { auth } = await import('../lib/firebase');
              const user = auth?.currentUser;
              if (!user) {
                console.error('[Agent] User not authenticated');
                return;
              }
              
              console.log('[Agent] User authenticated', { userId: user.uid, email: user.email });
              
              // Get tenant ID from user (assuming it's stored somewhere or we can get it from project)
              // For now, we'll use projectId as tenantId fallback
              const tenantId = user.uid; // This should be actual tenantId, but using uid as fallback
              
              if (agentType === 'data') {
                // For Data Agent, parse query as data collection request
                const task: import('../types/agents').DataAgentTask = {
                  context: {
                    tenantId,
                    projectId,
                    userId: user.uid,
                  },
                  query: query,
                  granularity: 'monthly',
                  timeRange: {
                    from: new Date(Date.now() - 365 * 24 * 60 * 60 * 1000).toISOString().split('T')[0], // 1 year ago
                    to: new Date().toISOString().split('T')[0], // today
                  },
                  metadata: {
                    interactionId: now + '-iu',
                    messageId: now + '-u',
                  },
                };
                
                // Execute agent via API and get result
                console.log('[Agent] Calling executeDataAgent', { projectId, task });
                let response: import('../types/agents').AgentExecutionResponse;
                try {
                  if (agentType === 'data') {
                    response = await api.executeDataAgent(projectId, task);
                  } else {
                    response = await api.executeEntitiesAgent(projectId, task as any);
                  }
                  console.log('[Agent] Received response', { agentId: response.agentId, agentName: response.agentName, resultKeys: Object.keys(response.result || {}) });
                } catch (e: any) {
                  console.error('[Agent] API call failed', e);
                  if (e.code === 'ECONNABORTED' || e.message?.includes('timeout')) {
                    console.error('[Agent] Request timeout - agent may still be running. Check WebSocket updates.');
                    // Agent may still be running, but HTTP request timed out
                    // WebSocket updates should still work
                    await api.createProjectInteraction(projectId, {
                      kind: 'assistant',
                      subkind: 'agent.timeout',
                      content: 'Agent execution is taking longer than expected. Progress updates will be sent via WebSocket.',
                    });
                    await get().hydrateInteractions();
                  }
                  throw e;
                }
                
                // Save result immediately
                await saveAgentResult(response.result, agentType, projectId, query, response.agentId, user.uid);
                
                // Track task via store for progress updates
                const taskId = await agentsStore.executeAgent(projectId, agentType, task);
                
                // Wait for completion status updates via WebSocket
                await waitForAgentCompletion(taskId, agentType, projectId, query, user.uid);
              } else if (agentType === 'entities') {
                // For Entities Agent, parse entities from query
                // Split by commas or newlines to support multiple entities
                const parseEntities = (text: string): Array<{ name: string; type: string }> => {
                  if (!text.trim()) return [];
                  
                  // Split by commas or newlines, then trim each part
                  const parts = text
                    .split(/[,\n]/)
                    .map(p => p.trim())
                    .filter(p => p.length > 0);
                  
                  // If only one part or splitting didn't help, return as single entity
                  if (parts.length === 0) return [];
                  if (parts.length === 1) {
                    return [{
                      name: parts[0],
                      type: 'other', // Agent will determine proper type
                    }];
                  }
                  
                  // Multiple entities found
                  return parts.map(name => ({
                    name,
                    type: 'other', // Agent will determine proper type
                  }));
                };
                
                const entities = parseEntities(query);
                
                const task: import('../types/agents').EntitiesAgentTask = {
                  context: {
                    tenantId,
                    projectId,
                    userId: user.uid,
                  },
                  entities: entities.length > 0 ? entities : [{
                    name: query,
                    type: 'other', // Fallback: pass entire query as single entity
                  }],
                  maxDepth: 2,
                  includeDetails: true,
                  metadata: {
                    interactionId: now + '-iu',
                    messageId: now + '-u',
                    custom: {
                      query: query, // Store original query in metadata for agent reference
                    },
                  },
                };
                
                // Execute agent via API and get result
                console.log('[Agent] Calling executeEntitiesAgent', { projectId, task });
                let response: import('../types/agents').AgentExecutionResponse;
                try {
                  response = await api.executeEntitiesAgent(projectId, task);
                  console.log('[Agent] Received response', { agentId: response.agentId, agentName: response.agentName, resultKeys: Object.keys(response.result || {}) });
                } catch (e: any) {
                  console.error('[Agent] API call failed', e);
                  if (e.code === 'ECONNABORTED' || e.message?.includes('timeout')) {
                    console.error('[Agent] Request timeout - agent may still be running. Check WebSocket updates.');
                    // Agent may still be running, but HTTP request timed out
                    // WebSocket updates should still work
                    await api.createProjectInteraction(projectId, {
                      kind: 'assistant',
                      subkind: 'agent.timeout',
                      content: 'Agent execution is taking longer than expected. Progress updates will be sent via WebSocket.',
                    });
                    await get().hydrateInteractions();
                  }
                  throw e;
                }
                
                // Save result immediately
                await saveAgentResult(response.result, agentType, projectId, query, response.agentId, user.uid);
                
                // Track task via store for progress updates
                const taskId = await agentsStore.executeAgent(projectId, agentType, task);
                
                // Wait for completion status updates via WebSocket
                await waitForAgentCompletion(taskId, agentType, projectId, query, user.uid);
              }
            } catch (e: any) {
              console.error('[Agent] Execution failed', e);
              // Show error status
              try {
                await api.createProjectInteraction(projectId, {
                  kind: 'assistant',
                  subkind: 'agent.error',
                  content: `Agent execution failed: ${e.message || 'Unknown error'}`,
                });
                await get().hydrateInteractions();
              } catch {}
            }
            return;
          }
        } catch (e) {
          console.error('[Command] Execution failed', e);
        }
        return;
      } catch (e) {
        console.error('[Command] Handler failed', e);
        // Fall through to normal LLM flow
      }
    }
    
    // Preliminary stance structured call (extract user_request) - after UI update, before server sync
    // TEMPORARILY DISABLED: Prestance call adds ~2 seconds latency and is not critical
    // TODO: Re-enable when needed or optimize to reduce latency
    /*
    if (!suppressUserEcho) {
      try {
        const stanceModel = useGenAISettings.getState().stanceModel;
        const evalModel = (stanceModel && stanceModel.trim()) ? stanceModel : 'gemini_fast';
        const schemaUr = {
          type: 'object',
          additionalProperties: false,
          properties: { 
            run: { type: 'string' },
            command: { type: 'string' },
            params: { type: 'string' }
          },
          required: ['run', 'command']
        } as any;
        const availableCommands = 'Available commands: /analyze entities, /analyze facts, /analyze events, /analyze metrics, /analyze decisions, /agent data, /agent entity, /section <title>, /end';
        const promptUr = `You are performing a preliminary assessment of the user's request before execution. In the normal case, this is a regular LLM chat and command execution is not required, but in rare cases the user may request execution of system commands in natural language.\n\n${availableCommands}\n\nDetermine if the user's request is a demand to execute a system command from the list above. If it is - select the single most appropriate command. If command execution is not required - return empty string in the 'run' field.\n\nReturn STRICTLY JSON with this exact shape: { "run": string, "command": string, "params": string } (no prose, no markdown, no code fences).\n- "run": the extracted user request text if command execution is required, OR empty string "" if it's a regular chat request\n- "command": the matched command (e.g., "/analyze entities", "/section") if found, OR empty string "" if no command is required\n- "params": the remaining text/arguments for the command (e.g., section title or analyze context); empty string "" if not applicable\n\nUSER_MESSAGE:\n${prompt}`;
        const t0 = (typeof performance !== 'undefined' && performance.now) ? performance.now() : Date.now();
        const urRes = await api.genaiGenerate({
          model: evalModel,
          prompt: promptUr,
          strictJSONEnabled: true,
          jsonSchema: schemaUr,
          maxOutputTokens: 2048,
          webSearchEnabled: false,
        });
        const t1 = (typeof performance !== 'undefined' && performance.now) ? performance.now() : Date.now();
        const preLatency = Math.max(0, Math.floor(t1 - t0));
        let preExtract: { user_request: string } | undefined = undefined;
        let detectedCommand: string | undefined = undefined;
        let detectedParams: string | undefined = undefined;
        try {
          let parsedUr: any = null;
          if (typeof (urRes as any)?.rawJSON === 'string' && (urRes as any).rawJSON.trim()) {
            try { parsedUr = JSON.parse((urRes as any).rawJSON); } catch {}
          }
          if (!parsedUr && typeof urRes?.content === 'string') {
            const s = urRes.content.trim();
            if ((s.startsWith('{') || s.startsWith('['))) parsedUr = JSON.parse(s);
          }
          if (parsedUr && typeof parsedUr.run === 'string') {
            // If run is not empty, it contains the user request (command scenario)
            // If run is empty, use original prompt (normal chat scenario)
            const extracted = parsedUr.run.trim();
            preExtract = { user_request: extracted || prompt.trim() };
          } else {
            // Fallback: use the original user prompt
            if (prompt && typeof prompt === 'string' && prompt.trim()) {
              preExtract = { user_request: prompt.trim() };
            }
          }
          
          // Check if a command was detected
          if (parsedUr && typeof parsedUr.command === 'string') {
            const command = parsedUr.command.trim();
            if (command) {
              detectedCommand = command;
            }
          }
          // Capture params if provided
          if (parsedUr && typeof parsedUr.params === 'string') {
            const p = parsedUr.params.trim();
            if (p) detectedParams = p;
          }
        } catch {}
        
        // Store preStance result for this user message
        if (preLatency > 0) {
          try {
            if (detectedCommand) {
              console.log('[PreStance] detected command', { command: detectedCommand, params: detectedParams, latencyMs: preLatency });
            }
            console.log('[PreStance] measured', { latencyMs: preLatency, user_request: preExtract?.user_request, command: detectedCommand, params: detectedParams });
            get().setPreStanceResult(now + '-u', preLatency, preExtract);
          } catch {}
        }

        // If command detected, execute it instead of running GenAI
        if (detectedCommand) {
          try {
            // Remove assistant placeholder
            set((s) => {
              const messages = [...s.messages];
              if (messages.length > 0 && messages[messages.length - 1].role === 'assistant' && !messages[messages.length - 1].content) {
                messages.pop(); // Remove placeholder
              }
              return { messages, streaming: false } as any;
            });

            // Ensure USER interaction is persisted before creating ACTION
            if (!suppressUserEcho) {
              try {
                const userLocalId = now + '-iu';
                const meta = agentMeta as any;
                const payload: any = { kind: 'user', content: prompt, originalMessageId: now + '-u' };
                if (meta?.subkind) payload.subkind = meta.subkind;
                if (meta?.replyTo) {
                  const map = (get() as any).persistedByLocalId || {};
                  const serverReply = map[meta.replyTo] || map[String(meta.replyTo)];
                  if (serverReply) payload.replyTo = serverReply;
                }
                if (meta?.agentProfile) payload.agentProfile = meta.agentProfile;
                // Attach document IDs if any
                const attachedDocIds = get().pendingAttachedDocIds;
                if (attachedDocIds && attachedDocIds.length > 0) {
                  payload.coreDocIds = attachedDocIds;
                  try { console.log('[Chat] Attaching documents to interaction', { docIds: attachedDocIds }); } catch {}
                }
                const resUser = await api.createProjectInteraction(projectId, payload);
                
                // Update local interaction with server ID to prevent duplicates from WebSocket events
                set((st) => {
                  const items = [...st.interactions];
                  const localId = now + '-iu';
                  const idx = items.findIndex((it) => it.id === localId);
                  if (idx >= 0) {
                    // Check if already updated (prevent duplicate updates)
                    if (items[idx].id === resUser.id) {
                      console.log('[Chat] User interaction already has server ID, skipping update', { interactionId: resUser.id });
                      return {} as any;
                    }
                    console.log('[Chat] Updating user interaction ID', { localId, serverId: resUser.id, idx });
                    items[idx] = { ...items[idx], id: resUser.id } as LocalInteraction;
                    const map = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
                    map[localId] = resUser.id;
                    map[now + '-u'] = resUser.id;
                    return { interactions: items, persistedByLocalId: map } as any;
                  } else {
                    // Interaction not found - check if it was already added with server ID
                    const existingByServerId = items.find((it) => it.id === resUser.id);
                    if (existingByServerId) {
                      console.log('[Chat] User interaction already exists with server ID, skipping', { interactionId: resUser.id });
                      return {} as any;
                    }
                    console.warn('[Chat] User interaction not found for update', { localId, serverId: resUser.id, interactionsCount: items.length });
                  }
                  return {} as any;
                });
                
                // Create non-displayable interactions for attached documents
                if (attachedDocIds && attachedDocIds.length > 0) {
                  try {
                    // Load documents to get their content
                    const docs = await api.docsList(projectId);
                    const docMap = new Map(docs.map((d: any) => [d.id, d]));
                    
                    // Create document interactions
                    const docInteractions: LocalInteraction[] = [];
                    for (const docId of attachedDocIds) {
                      const doc = docMap.get(docId);
                      if (!doc) {
                        console.warn('[Chat] Document not found in list', { docId });
                        continue;
                      }
                      
                      // Always download the full document content
                      // For text files (CSV, TXT, etc.), docsDownload returns the original file which is the text
                      // For other files, it returns the original, but that's better than using a preview
                      let docContent = '';
                      const docSizeBytes = doc.sizeBytes || 0;
                      let contentSource = 'unknown';
                      
                      try {
                        console.log('[Chat] Downloading full document content', { docId, filename: doc.filename, contentType: doc.contentType, sizeBytes: docSizeBytes });
                        const blob = await api.docsDownload(projectId, docId);
                        docContent = await new Response(blob).text();
                        contentSource = 'download';
                        console.log('[Chat] Downloaded document content', { 
                          docId, 
                          filename: doc.filename,
                          contentLength: docContent.length,
                          contentLengthKB: (docContent.length / 1024).toFixed(2),
                          sizeBytes: docSizeBytes,
                          sizeKB: (docSizeBytes / 1024).toFixed(2)
                        });
                      } catch (downloadErr) {
                        // Fallback to extractedText if download fails
                        if (doc.extractedText && doc.extractedText.length > 0) {
                          console.warn('[Chat] Failed to download document, using extractedText fallback', { docId, error: downloadErr });
                          docContent = doc.extractedText;
                          contentSource = 'extractedText';
                        } else {
                          console.warn('[Chat] Failed to download document and no extractedText available', { docId, error: downloadErr });
                          continue; // Skip this document if we can't get its content
                        }
                      }
                      
                      if (docContent.length === 0) {
                        console.warn('[Chat] Document content is empty', { docId });
                        continue;
                      }
                      
                      // Calculate tokens added to context
                      const contentChars = docContent.length;
                      const contentTokens = Math.ceil(contentChars / 4);
                      
                      console.log('[Chat] Document added to context', {
                        docId,
                        filename: doc.filename,
                        sizeBytes: docSizeBytes,
                        sizeKB: (docSizeBytes / 1024).toFixed(2),
                        contentChars,
                        contentKB: (contentChars / 1024).toFixed(2),
                        contentTokens,
                        contentSource,
                        ratio: docSizeBytes > 0 ? ((contentChars / docSizeBytes) * 100).toFixed(1) + '%' : 'N/A'
                      });
                      
                      const docInteraction: LocalInteraction = {
                        id: `${now}-doc-${docId}`,
                        projectId,
                        kind: 'system',
                        subkind: 'document.attach',
                        content: docContent,
                        createdAt: new Date().toISOString(),
                        replyTo: resUser.id, // Link to user interaction
                      };
                      docInteractions.push(docInteraction);
                      
                      // Persist on server
                      try {
                        await api.createProjectInteraction(projectId, {
                          kind: 'system',
                          subkind: 'document.attach',
                          content: docContent,
                          replyTo: resUser.id,
                        });
                      } catch (e) {
                        console.warn('[Chat] Failed to persist document interaction', { docId, error: e });
                      }
                    }
                    
                    // Add to local interactions
                    if (docInteractions.length > 0) {
                      set((st) => ({
                        interactions: [...st.interactions, ...docInteractions],
                      } as any));
                    }
                  } catch (e) {
                    console.warn('[Chat] Failed to create document interactions', { error: e });
                  }
                }
                
                // Clear attached docs after creating interaction
                set({ pendingAttachedDocIds: [] });
                set((st) => {
                  const items = [...st.interactions];
                  const idx = items.findIndex((it) => it.id === userLocalId);
                  if (idx >= 0) { (items[idx] as any).id = resUser.id; }
                  const map2 = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
                  map2[userLocalId] = resUser.id;
                  map2[now + '-u'] = resUser.id;
                  return { interactions: items, persistedByLocalId: map2 } as any;
                });
              } catch (e) {
                console.warn('[persist user-before-action] failed', e);
              }
            }

            // Create action interaction (to show intent in chat)
            try {
              const actionId = Date.now().toString() + '-ac';
              const createdAt = new Date().toISOString();
              const actionText = detectedCommand + (detectedParams ? (' ' + detectedParams) : '');
              const actionInter: LocalInteraction = {
                id: actionId,
                projectId,
                kind: 'action',
                content: actionText,
                createdAt,
                originalMessageId: now + '-u',
              };
              set((s) => ({ interactions: [...s.interactions, actionInter] }));
              // Persist action (best-effort)
              (async () => {
                try {
                  const res = await api.createProjectInteraction(projectId, { kind: 'action', content: actionText, originalMessageId: now + '-u' });
                  set((s) => {
                    const map = { ...(s.persistedByLocalId || {}) } as Record<string, string>;
                    map[actionId] = res.id;
                    const items = [...s.interactions];
                    const idx = items.findIndex((it) => it.id === actionId);
                    if (idx >= 0) { (items[idx] as any).id = res.id; }
                    return { interactions: items, persistedByLocalId: map } as any;
                  });
                } catch (e) {
                  console.warn('[Action] persist failed', e);
                }
              })();
            } catch {}

            // Execute command
            const cmd = detectedCommand.toLowerCase();
            const params = (detectedParams || '').trim();
            try {
              if (cmd === '/end') {
                await api.createProjectInteraction(projectId, { kind: 'section_end', content: '' });
                await get().hydrateInteractions();
                return;
              }
              if (cmd.startsWith('/section')) {
                // Use params as title
                await api.createProjectInteraction(projectId, { kind: 'section_start', content: params });
                await get().hydrateInteractions();
                return;
              }
              if (cmd === '/analyze entities') { await api.analyzeEntities(projectId, params); return; }
              if (cmd === '/analyze facts') { await api.analyzeFacts(projectId, params); return; }
              if (cmd === '/analyze events') { await api.analyzeEvents(projectId, params); return; }
              if (cmd === '/analyze metrics') { await api.analyzeMetrics(projectId, params); return; }
              if (cmd === '/analyze decisions') { await api.analyzeDecisions(projectId, params); return; }
              
              // Agent commands: /agent data <query> and /agent entity <query>
              if (cmd === '/agent data' || cmd === '/agent entity') {
                const agentType = cmd === '/agent data' ? 'data' : 'entities';
                const query = params || prompt.replace(/^\s*\/agent\s+(data|entity)\s+/i, '').trim();
                
                if (!query) {
                  console.warn('[Agent] Empty query provided');
                  return;
                }
                
                try {
                  // Import agents store
                  const { useAgentsStore } = await import('./agentsStore');
                  const agentsStore = useAgentsStore.getState();
                  
                  // Get current user context
                  const { auth } = await import('../lib/firebase');
                  const user = auth?.currentUser;
                  if (!user) {
                    console.error('[Agent] User not authenticated');
                    return;
                  }
                  
                  // Get tenant ID from user (assuming it's stored somewhere or we can get it from project)
                  // For now, we'll use projectId as tenantId fallback
                  const tenantId = user.uid; // This should be actual tenantId, but using uid as fallback
                  
                  // Create task based on agent type
                  if (agentType === 'data') {
                    // For Data Agent, we'll let the agent parse the query itself
                    // The agent can understand natural language queries
                    const task: import('../types/agents').DataAgentTask = {
                      context: {
                        tenantId,
                        projectId,
                        userId: user.uid,
                      },
                      query: query,
                      granularity: 'monthly', // Default, agent can adjust if needed
                      timeRange: {
                        from: new Date(new Date().getFullYear(), 0, 1).toISOString().split('T')[0], // Current year start
                        to: new Date().toISOString().split('T')[0], // Today
                      },
                      metadata: {
                        interactionId: userLocalId,
                        messageId: now + '-u',
                      },
                    };
                    
                    // Execute agent via API and get result
                    let response: import('../types/agents').AgentExecutionResponse;
                    if (agentType === 'data') {
                      response = await api.executeDataAgent(projectId, task);
                    } else {
                      response = await api.executeEntitiesAgent(projectId, task);
                    }
                    
                    // Save result immediately
                    await saveAgentResult(response.result, agentType, projectId, query, response.agentId, user.uid);
                    
                    // Track task via store for progress updates
                    const taskId = await agentsStore.executeAgent(projectId, agentType, task);
                    
                    // Wait for completion status updates via WebSocket
                    waitForAgentCompletion(taskId, agentType, projectId, query, user.uid);
                  } else if (agentType === 'entities') {
                    // For Entities Agent, parse entities from query
                    // If entities array is empty, agent will parse from query string
                    // We'll pass query as a special entity for agent to parse
                    const task: import('../types/agents').EntitiesAgentTask = {
                      context: {
                        tenantId,
                        projectId,
                        userId: user.uid,
                      },
                      entities: query.trim() ? [{
                        name: query,
                        type: 'other', // Agent will determine proper type
                      }] : [], // Empty array - agent will parse from query if needed
                      maxDepth: 2,
                      includeDetails: true,
                      metadata: {
                        interactionId: userLocalId,
                        messageId: now + '-u',
                        custom: {
                          query: query, // Store query in metadata for agent reference
                        },
                      },
                    };
                    
                    // Execute agent via API and get result
                    let response: import('../types/agents').AgentExecutionResponse;
                    response = await api.executeEntitiesAgent(projectId, task);
                    
                    // Save result immediately
                    await saveAgentResult(response.result, agentType, projectId, query, response.agentId, user.uid);
                    
                    // Track task via store for progress updates
                    const taskId = await agentsStore.executeAgent(projectId, agentType, task);
                    
                    // Wait for completion status updates via WebSocket
                    waitForAgentCompletion(taskId, agentType, projectId, query, user.uid);
                  }
                } catch (e: any) {
                  console.error('[Agent] Execution failed', e);
                  // Show error status
                  try {
                    await api.createProjectInteraction(projectId, {
                      kind: 'assistant',
                      subkind: 'agent.error',
                      content: `Agent execution failed: ${e.message || 'Unknown error'}`,
                    });
                    await get().hydrateInteractions();
                  } catch {}
                }
                return;
              }
            } catch (e) {
              console.error('[CommandExecute] failed', e);
              return;
            }

            // Exit early, don't run GenAI
            return;
          } catch (e) {
            console.error('[Action] failed', e);
          }
        }
      } catch {
        // PreStance is non-blocking, continue even if it fails
      }
    }
    */
    
    // Helper function to save agent result to CoreDoc
    async function saveAgentResult(
      result: any,
      agentType: 'data' | 'entities',
      projectId: string,
      query: string,
      taskId: string,
      userId: string
    ) {
      try {
        const docType = agentType === 'data' ? 'agent_data' : 'agent_entity';
        const filename = `${agentType}_agent_${Date.now()}.json`;
        const jsonContent = JSON.stringify({
          agentId: agentType,
          agentName: agentType === 'data' ? 'Data Collection' : 'Entity Research',
          query: query,
          taskId: taskId,
          result: result,
          metadata: {
            confidence: result?.confidence,
            sources: result?.sources || result?.metadata?.sources,
            methods: result?.metadata?.methods,
            assumptions: result?.metadata?.assumptions,
            explanation: result?.explanation,
          },
          completedAt: new Date().toISOString(),
        }, null, 2);
        
        // Save to CoreDoc
        await api.docsCreateFromText(projectId, {
          filename,
          contentType: 'application/json',
          text: jsonContent,
          description: `${agentType === 'data' ? 'Data Agent' : 'Entities Agent'} result: ${query.substring(0, 100)}`,
        });
        
        // Create interaction to show completion
        await api.createProjectInteraction(projectId, {
          kind: 'assistant',
          subkind: `agent.${agentType}.completed`,
          content: `${agentType === 'data' ? 'Data Agent' : 'Entities Agent'} completed. Result saved to Library.`,
        });
        
        await get().hydrateInteractions();
      } catch (e: any) {
        console.error('[Agent] Failed to save result', e);
        throw e;
      }
    }
    
    // Helper function to wait for agent completion and save result to CoreDoc
    async function waitForAgentCompletion(
      taskId: string,
      agentType: 'data' | 'entities',
      projectId: string,
      query: string,
      userId: string
    ) {
      const { useAgentsStore } = await import('./agentsStore');
      const agentsStore = useAgentsStore.getState();
      
      // Poll for completion (max 5 minutes)
      const maxAttempts = 300; // 5 minutes at 1 second intervals
      let attempts = 0;
      let currentTimeoutId: number | null = null;
      
      const checkCompletion = async () => {
        // Check if project changed or store was reset
        const currentState = get();
        if (currentState.projectId !== projectId) {
          console.log('[Agent] Project changed, stopping completion check', { taskId, originalProjectId: projectId, currentProjectId: currentState.projectId });
          if (currentTimeoutId !== null) {
            activeAgentTimers.delete(taskId);
          }
          return;
        }
        
        attempts++;
        const taskState = agentsStore.getTaskState(taskId);
        
        if (!taskState) {
          // Task not found, might have been cleared or not started properly
          if (attempts < 10) {
            // Retry a few times in case task is still initializing
            currentTimeoutId = window.setTimeout(checkCompletion, 1000);
            activeAgentTimers.set(taskId, currentTimeoutId);
            return;
          }
          console.warn('[Agent] Task not found after multiple attempts');
          activeAgentTimers.delete(taskId);
          return;
        }
        
        if (taskState.status === 'completed') {
          // Agent completed successfully - result already saved from HTTP response
          activeAgentTimers.delete(taskId);
          try {
            await api.createProjectInteraction(projectId, {
              kind: 'assistant',
              subkind: `agent.${agentType}.completed`,
              content: `${agentType === 'data' ? 'Data Agent' : 'Entities Agent'} completed successfully.`,
            });
            await get().hydrateInteractions();
            agentsStore.clearTask(taskId);
          } catch {}
        } else if (taskState.status === 'error') {
          // Agent failed
          activeAgentTimers.delete(taskId);
          try {
            await api.createProjectInteraction(projectId, {
              kind: 'assistant',
              subkind: 'agent.error',
              content: `${agentType === 'data' ? 'Data Agent' : 'Entities Agent'} failed: ${taskState.error || 'Unknown error'}`,
            });
            await get().hydrateInteractions();
            agentsStore.clearTask(taskId);
          } catch {}
        } else if (taskState.status === 'running') {
          // Still running, check again
          if (attempts < maxAttempts) {
            currentTimeoutId = window.setTimeout(checkCompletion, 1000);
            activeAgentTimers.set(taskId, currentTimeoutId);
          } else {
            // Timeout
            activeAgentTimers.delete(taskId);
            console.warn('[Agent] Task timeout after 5 minutes');
            await api.createProjectInteraction(projectId, {
              kind: 'assistant',
              subkind: 'agent.error',
              content: `${agentType === 'data' ? 'Data Agent' : 'Entities Agent'} timeout after 5 minutes`,
            });
            await get().hydrateInteractions();
            agentsStore.clearTask(taskId);
          }
        }
      };
      
      // Start checking after a short delay
      currentTimeoutId = window.setTimeout(checkCompletion, 1000);
      activeAgentTimers.set(taskId, currentTimeoutId);
    }
    
    // Persist user interaction on server (only if not suppressed)
    if (!suppressUserEcho) {
      try {
        const userLocalId = now + '-iu';
        const meta = agentMeta as any;
        const payload: any = { kind: 'user', content: prompt, originalMessageId: now + '-u' };
        if (meta?.subkind) payload.subkind = meta.subkind;
        if (meta?.replyTo) {
          const map = (get() as any).persistedByLocalId || {};
          const replyToValue = meta.replyTo;
          // Try to find server ID in mapping (if replyTo is a local ID)
          let serverReply = map[replyToValue] || map[String(replyToValue)];
          // If not found in mapping, check if replyTo is already a server ID (exists in interactions)
          if (!serverReply) {
            const existsAsServerId = get().interactions.some((it) => it.id === replyToValue);
            if (existsAsServerId) {
              serverReply = replyToValue;
            }
          }
          if (serverReply) payload.replyTo = serverReply;
        }
        if (meta?.agentProfile) payload.agentProfile = meta.agentProfile;
        // Attach document IDs if any
        const attachedDocIds = get().pendingAttachedDocIds;
        if (attachedDocIds && attachedDocIds.length > 0) {
          payload.coreDocIds = attachedDocIds;
          console.log('[Chat] Attaching documents to interaction', { docIds: attachedDocIds });
        }
        const res = await api.createProjectInteraction(projectId, payload);
        
        // Update local interaction with server ID to prevent duplicates from WebSocket events
        set((st) => {
          const items = [...st.interactions];
          const localId = now + '-iu';
          const idx = items.findIndex((it) => it.id === localId);
          if (idx >= 0) {
            // Check if already updated (prevent duplicate updates)
            if (items[idx].id === res.id) {
              console.log('[Chat] User interaction already has server ID, skipping update', { interactionId: res.id });
              return {} as any;
            }
            console.log('[Chat] Updating user interaction ID', { localId, serverId: res.id, idx });
            items[idx] = { ...items[idx], id: res.id } as LocalInteraction;
            const map = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
            map[localId] = res.id;
            map[now + '-u'] = res.id;
            return { interactions: items, persistedByLocalId: map } as any;
          } else {
            // Interaction not found - check if it was already added with server ID
            const existingByServerId = items.find((it) => it.id === res.id);
            if (existingByServerId) {
              console.log('[Chat] User interaction already exists with server ID, skipping', { interactionId: res.id });
              return {} as any;
            }
            console.warn('[Chat] User interaction not found for update', { localId, serverId: res.id, interactionsCount: items.length });
          }
          return {} as any;
        });
        
        // Create non-displayable interactions for attached documents
        if (attachedDocIds && attachedDocIds.length > 0) {
          try {
            // Load documents to get their content
            const docs = await api.docsList(projectId);
            const docMap = new Map(docs.map((d: any) => [d.id, d]));
            
            // Create document interactions
            const docInteractions: LocalInteraction[] = [];
            for (const docId of attachedDocIds) {
              const doc = docMap.get(docId);
              if (!doc) {
                console.warn('[Chat] Document not found in list', { docId });
                continue;
              }
              
              // Always download the full document content
              // For text files (CSV, TXT, etc.), docsDownload returns the original file which is the text
              // For other files, it returns the original, but that's better than using a preview
              let docContent = '';
              const docSizeBytes = doc.sizeBytes || 0;
              let contentSource = 'unknown';
              
              try {
                console.log('[Chat] Downloading full document content', { docId, filename: doc.filename, contentType: doc.contentType, sizeBytes: docSizeBytes });
                const blob = await api.docsDownload(projectId, docId);
                docContent = await new Response(blob).text();
                contentSource = 'download';
                console.log('[Chat] Downloaded document content', { 
                  docId, 
                  filename: doc.filename,
                  contentLength: docContent.length,
                  contentLengthKB: (docContent.length / 1024).toFixed(2),
                  sizeBytes: docSizeBytes,
                  sizeKB: (docSizeBytes / 1024).toFixed(2)
                });
              } catch (downloadErr) {
                // Fallback to extractedText if download fails
                if (doc.extractedText && doc.extractedText.length > 0) {
                  console.warn('[Chat] Failed to download document, using extractedText fallback', { docId, error: downloadErr });
                  docContent = doc.extractedText;
                  contentSource = 'extractedText';
                } else {
                  console.warn('[Chat] Failed to download document and no extractedText available', { docId, error: downloadErr });
                  continue; // Skip this document if we can't get its content
                }
              }
              
              if (docContent.length === 0) {
                console.warn('[Chat] Document content is empty', { docId });
                continue;
              }
              
              // Calculate tokens added to context
              const contentChars = docContent.length;
              const contentTokens = Math.ceil(contentChars / 4);
              
              console.log('[Chat] Document added to context', {
                docId,
                filename: doc.filename,
                sizeBytes: docSizeBytes,
                sizeKB: (docSizeBytes / 1024).toFixed(2),
                contentChars,
                contentKB: (contentChars / 1024).toFixed(2),
                contentTokens,
                contentSource,
                ratio: docSizeBytes > 0 ? ((contentChars / docSizeBytes) * 100).toFixed(1) + '%' : 'N/A'
              });
              
              const docInteraction: LocalInteraction = {
                id: `${now}-doc-${docId}`,
                projectId,
                kind: 'system',
                subkind: 'document.attach',
                content: docContent,
                createdAt: new Date().toISOString(),
                replyTo: res.id, // Link to user interaction
              };
              docInteractions.push(docInteraction);
              
              // Persist on server
              try {
                await api.createProjectInteraction(projectId, {
                  kind: 'system',
                  subkind: 'document.attach',
                  content: docContent,
                  replyTo: res.id,
                });
              } catch (e) {
                console.warn('[Chat] Failed to persist document interaction', { docId, error: e });
              }
            }
            
            // Add to local interactions
            if (docInteractions.length > 0) {
              set((st) => ({
                interactions: [...st.interactions, ...docInteractions],
              } as any));
            }
          } catch (e) {
            console.warn('[Chat] Failed to create document interactions', { error: e });
          }
        }
        
        // Clear attached docs after creating interaction
        set({ pendingAttachedDocIds: [] });
        set((st) => {
          const items = [...st.interactions];
          const idx = items.findIndex((it) => it.id === userLocalId);
          if (idx >= 0) { (items[idx] as any).id = res.id; }
          const map = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
          map[userLocalId] = res.id;
          map[now + '-u'] = res.id;
          return { interactions: items, persistedByLocalId: map } as any;
        });
      } catch (e) {
        console.warn('[persist user] failed', e);
      }
    }
    try {
      // Build messages from local interactions (summary preferred, else content)
      const interactions = get().interactions;
      
      // Debug: log interactions count before filtering
      console.log('[Chat] Building messages from interactions', {
        totalInteractions: interactions.length,
        interactionKinds: interactions.map(it => it.kind).slice(-5), // Last 5 kinds
        projectId: get().projectId,
      });
      
      // Filter out evaluation messages, actions, section markers, and perspectives - they shouldn't be in chat context
      const chatInteractions = interactions.filter((it) => {
        // Exclude Graph chat interactions (they belong to Graph tab, not Discussion chat)
        if (it.subkind === 'graph.chat') {
          return false;
        }
        // Exclude layout interactions (they are UI state, not chat content)
        if (it.kind === 'ontology' && it.subkind === 'layout') {
          return false;
        }
        // Exclude ontology interactions (they are shown in Graph tab, not in chat)
        if (it.kind === 'ontology') {
          return false;
        }
        // Exclude perspective interactions (kind='note' with subkind starting with 'perspective.')
        if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
          return false;
        }
        // Exclude queued tasks interactions (they are metadata, not chat content)
        if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind === 'queued.tasks') {
          return false;
        }
        // Exclude perspective analysis interactions (kind='system' with subkind starting with 'perspective.' and ending with '.analysis')
        if (it.kind === 'system' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.') && it.subkind.endsWith('.analysis')) {
          return false;
        }
        // Exclude eval.* system messages (bias, stance, user_request, review evaluations)
        if (it.kind === 'system' && typeof it.subkind === 'string' && it.subkind.startsWith('eval.')) {
          return false;
        }
        // Exclude document attachments (they are context, not chat content)
        if (it.subkind === 'document.attach') {
          return false;
        }
        // Exclude action, section markers (these are UI markers, not chat content)
        if (it.kind === 'action' || it.kind === 'section_start' || it.kind === 'section_end') {
          return false;
        }
        // Exclude agent execution interactions (they are execution status, not chat content)
        if (it.kind === 'agent.execution') {
          return false;
        }
        return true;
      });
      
      // LLM context includes document attachments (for token counting and LLM), but excludes other non-displayable types
      // Exclude ontology interactions - they are structured data (entities, relations, etc.), not conversational content
      // They should not be sent to LLM as chat context (they are shown in Graph tab, not in chat)
      const llmContextInteractions = interactions.filter((it) => {
        // Exclude ontology interactions (structured data, not chat content)
        // This includes layout interactions (UI state, not chat content)
        if (it.kind === 'ontology') {
          return false;
        }
        // Exclude perspective interactions (kind='note' with subkind starting with 'perspective.')
        if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.')) {
          return false;
        }
        // Exclude queued tasks interactions (they are metadata, not chat content)
        if (it.kind === 'note' && typeof it.subkind === 'string' && it.subkind === 'queued.tasks') {
          return false;
        }
        // Exclude perspective analysis interactions (kind='system' with subkind starting with 'perspective.' and ending with '.analysis')
        if (it.kind === 'system' && typeof it.subkind === 'string' && it.subkind.startsWith('perspective.') && it.subkind.endsWith('.analysis')) {
          return false;
        }
        // Exclude eval.* system messages (bias, stance, user_request, review evaluations)
        if (it.kind === 'system' && typeof it.subkind === 'string' && it.subkind.startsWith('eval.')) {
          return false;
        }
        // Exclude action, section markers (these are UI markers, not chat content)
        if (it.kind === 'action' || it.kind === 'section_start' || it.kind === 'section_end') {
          return false;
        }
        // Exclude agent execution interactions (they are execution status, not chat content)
        if (it.kind === 'agent.execution') {
          return false;
        }
        // Include document attachments in LLM context (they are context for LLM)
        return true;
      });
      
      // TODO: Implement automatic message compaction (autocompact) to intelligently summarize/truncate old messages
      // when context window usage exceeds thresholds, preserving recent context and key information from older messages.
      
      // Limit context to 70% of model's maximum context window
      const currentSettings = useGenAISettings.getState();
      const modelAlias = mapProviderStrengthToModel(currentSettings.provider, currentSettings.strength);
      const contextWindow = getModelContextWindow(modelAlias);
      const maxContextTokens = contextWindow ? Math.floor(contextWindow * 0.7) : Infinity;
      const maxPinnedTokens = contextWindow ? Math.floor(contextWindow * 0.9) : Infinity; // Pinned messages can use up to 90% of context
      
      // Separate pinned and regular interactions
      const now = Date.now();
      const twoDaysAgo = now - (2 * 24 * 60 * 60 * 1000); // 2 days in milliseconds
      
      const pinnedInteractions = llmContextInteractions.filter((it) => {
        if (!it.metadata?.pinned) return false;
        // Filter out pinned messages older than 2 days
        const createdAt = it.createdAt ? new Date(it.createdAt).getTime() : 0;
        return createdAt >= twoDaysAgo;
      });
      
      const regularInteractions = llmContextInteractions.filter((it) => !it.metadata?.pinned);
      
      // Calculate tokens for each interaction and build messages array
      const messagesWithTokens: Array<{ role: 'user'|'assistant'|'system'|'note'; text: string; tokens: number }> = [];
      let totalTokens = 0;
      
      // Process pinned interactions first (oldest to newest, but truncate oldest if needed)
      // Sort pinned by creation date (oldest first) to truncate oldest ones if needed
      const sortedPinned = [...pinnedInteractions].sort((a, b) => {
        const aTime = a.createdAt ? new Date(a.createdAt).getTime() : 0;
        const bTime = b.createdAt ? new Date(b.createdAt).getTime() : 0;
        return aTime - bTime; // Oldest first
      });
      
      let pinnedTokens = 0;
      const pinnedMessages: Array<{ role: 'user'|'assistant'|'system'|'note'; text: string; tokens: number }> = [];
      
      for (const it of sortedPinned) {
        // For ontology interactions in Graph mode, format as structured entity data
        let text: string;
        if (it.kind === 'ontology' && it.content) {
          try {
            const entityData = JSON.parse(it.content);
            // Format ontology entity for LLM context: show type, ID, and key properties
            const entityId = entityData.id || it.ontologyEntityId || it.id;
            const entityType = it.subkind || 'entity';
            const name = entityData.canonicalName || entityData.name || entityData.title || entityId;
            text = `[ONTOLOGY_ENTITY:${entityType}]\nID: ${entityId}\nName: ${name}\nData: ${JSON.stringify(entityData, null, 2)}`;
          } catch {
            // Fallback to raw content if JSON parsing fails
            text = it.content;
          }
        } else {
          text = (it.summary && it.summary.trim()) ? it.summary : it.content;
        }
        const tokens = Math.ceil(text.length / 4); // Approximate: 1 token = 4 chars
        
        // If adding this pinned message would exceed 90% limit, truncate oldest pinned messages
        if (pinnedTokens + tokens > maxPinnedTokens && pinnedMessages.length > 0) {
          const remainingTokens = maxPinnedTokens - pinnedTokens;
          if (remainingTokens > 100) { // Only add if we have meaningful space left
            const maxChars = remainingTokens * 4;
            const truncated = text.length > maxChars 
              ? text.slice(0, maxChars - 50) + '\n... [truncated]'
              : text;
            pinnedMessages.push({
              role: (it.kind === 'assistant' || it.kind === 'system' || it.kind === 'research' || it.kind === 'ontology') ? 'assistant' : (it.kind === 'note' ? 'note' : 'user'),
              text: it.subkind === 'document.attach' 
                ? `[ATTACHED DOCUMENT]\n${truncated}` 
                : truncated,
              tokens: Math.ceil(truncated.length / 4),
            });
            pinnedTokens += Math.ceil(truncated.length / 4);
          }
          // Stop processing older pinned messages once we hit the limit
          break;
        }
        
        pinnedMessages.push({
          role: (it.kind === 'assistant' || it.kind === 'system' || it.kind === 'research' || it.kind === 'ontology') ? 'assistant' : (it.kind === 'note' ? 'note' : 'user'),
          text: it.subkind === 'document.attach' 
            ? `[ATTACHED DOCUMENT]\n${text}` 
            : text,
          tokens: tokens,
        });
        pinnedTokens += tokens;
      }
      
      // Add pinned messages to the beginning of context
      messagesWithTokens.push(...pinnedMessages);
      totalTokens += pinnedTokens;
      
      // Process regular interactions from newest to oldest, keeping recent ones fully
      // Collect messages in reverse order (newest first), then reverse to get chronological order
      const regularMessages: Array<{ role: 'user'|'assistant'|'system'|'note'; text: string; tokens: number }> = [];
      let regularTokens = 0;
      
      for (let i = regularInteractions.length - 1; i >= 0; i--) {
        const it = regularInteractions[i];
        // For ontology interactions in Graph mode, format as structured entity data
        let text: string;
        if (it.kind === 'ontology' && it.content) {
          try {
            const entityData = JSON.parse(it.content);
            // Format ontology entity for LLM context: show type, ID, and key properties
            // CRITICAL: Always use the REAL ID (ontologyEntityId first) so LLM can reference it in relations
            // Use ontologyEntityId first, as it's the canonical Firestore ID, not a temporary one from entityData.id
            const entityId = it.ontologyEntityId || entityData.id || it.id;
            const entityType = it.subkind || 'entity';
            const name = entityData.canonicalName || entityData.name || entityData.title || entityId;
            const aliases = entityData.aliases || [];
            const aliasesText = aliases.length > 0 ? `\nAliases: ${aliases.join(', ')}` : '';
            
            // CRITICAL: Remove any temporary ID from entityData before sending to LLM
            // Create a clean copy without temporary IDs to ensure LLM only sees real IDs
            const cleanEntityData = { ...entityData };
            // Replace id field with the real ID (entityId) to ensure consistency
            cleanEntityData.id = entityId;
            
            // Emphasize the ID prominently - LLM must use this exact ID when creating relations
            // Include aliases to help LLM find entities by different names
            text = `[ONTOLOGY_ENTITY:${entityType}]\nID: ${entityId} (USE THIS EXACT ID IN RELATIONS)\nName: ${name}${aliasesText}\nData: ${JSON.stringify(cleanEntityData, null, 2)}`;
          } catch {
            // Fallback to raw content if JSON parsing fails
            text = it.content;
          }
        } else {
          text = (it.summary && it.summary.trim()) ? it.summary : it.content;
        }
        const tokens = Math.ceil(text.length / 4); // Approximate: 1 token = 4 chars
        
        // If adding this message would exceed remaining limit, truncate older messages
        if (totalTokens + regularTokens + tokens > maxContextTokens && regularMessages.length > 0) {
          // Truncate oldest messages to fit within limit
          const remainingTokens = maxContextTokens - totalTokens - regularTokens;
          if (remainingTokens > 100) { // Only add if we have meaningful space left
            // Truncate this message to fit remaining space
            const maxChars = remainingTokens * 4;
            const truncated = text.length > maxChars 
              ? text.slice(0, maxChars - 50) + '\n... [truncated]'
              : text;
            regularMessages.push({
              role: (it.kind === 'assistant' || it.kind === 'system' || it.kind === 'research' || it.kind === 'ontology') ? 'assistant' : (it.kind === 'note' ? 'note' : 'user'),
              text: it.subkind === 'document.attach' 
                ? `[ATTACHED DOCUMENT]\n${truncated}` 
                : truncated,
              tokens: Math.ceil(truncated.length / 4),
            });
            regularTokens += Math.ceil(truncated.length / 4);
          }
          // Stop processing older messages once we hit the limit
          break;
        }
        
        regularMessages.push({
          role: (it.kind === 'assistant' || it.kind === 'system' || it.kind === 'research' || it.kind === 'ontology') ? 'assistant' : (it.kind === 'note' ? 'note' : 'user'),
          text: it.subkind === 'document.attach' 
            ? `[ATTACHED DOCUMENT]\n${text}` 
            : text,
          tokens: tokens,
        });
        regularTokens += tokens;
      }
      
      // Reverse regular messages to get chronological order (oldest to newest)
      // This ensures LLM sees messages in the correct temporal sequence
      regularMessages.reverse();
      messagesWithTokens.push(...regularMessages);
      totalTokens += regularTokens;
      
      // Convert to final messages format (drop tokens field)
      const messages: Array<{ role: 'user'|'assistant'|'system'|'note'; text: string }> = messagesWithTokens.map(({ tokens, ...msg }) => msg);
      
      // Append current user prompt as the latest message
      const promptTokens = Math.ceil(prompt.length / 4);
      messages.push({ role: 'user', text: prompt });
      totalTokens += promptTokens;
      // Финальная проверка projectId перед отправкой API запроса
      // Используем локальную переменную projectId, которая была проверена выше
      if (!projectId || projectId === 'local-unfold') {
        console.error('[Chat] startStream: projectId became invalid before API call', { projectId });
        set({ streaming: false, currentStreamId: undefined, error: 'Project ID lost before stream start' });
        return;
      }
      
      // Generate per-stream UUID for FE-side filtering
      const streamId = (crypto && 'randomUUID' in crypto) ? (crypto as any).randomUUID() : (Date.now().toString(36) + Math.random().toString(36).slice(2));
      
      // Create AbortController for request cancellation
      const abortController = new AbortController();
      
      // CRITICAL: Save streamId and abortController immediately so events can be filtered correctly
      // This must be set before API call to avoid race condition
      set({ currentStreamId: streamId, abortController } as any);
      
      // If toolUsageEnabled is true, pass empty tools array [] to signal backend should register its tools
      // If toolUsageEnabled is false, pass undefined/null to signal backend should NOT register tools
      // Backend distinguishes: [] = enable tool registration, undefined/null = disable tool registration
      const finalTools = currentSettings.toolUsageEnabled ? (tools || []) : (tools !== undefined ? tools : undefined);
      
      // CRITICAL: Используем проверенный projectId из локальной переменной
      // Не читаем из store, чтобы избежать гонок
      
      // Check if streaming is disabled (Graph mode) or model is gemini_preview - use non-streaming API
      // Get model from options.modelOverride or from currentSettings.model
      const modelOverride = options?.modelOverride || currentSettings.model || '';
      const isGeminiPreview = modelOverride === 'gemini_preview' || modelOverride === 'gemini-3-pro-preview';
      const disableStreaming = options?.disableStreaming === true;
      
      if (disableStreaming || isGeminiPreview) {
        // Use non-streaming API for Gemini 3 Pro Preview
        console.log('[Chat] Using non-streaming API for gemini_preview', { projectId, streamId });
        
        // Graph mode: set processing state immediately to show UI indicator
        if (disableStreaming) {
          set((s) => ({
            processingCommands: true,
            commandsProgress: {
              stage: 'parsing',
              processed: 0,
              total: 0,
              currentOperation: 'Waiting for response...',
            },
          } as any));
        }
        
        // Call onStart to set up UI state (streaming placeholder)
        get().onStart();
        
        // Prepare genaiGenerate request - use same approach as streaming
        // Pass messages array (excluding current prompt), backend will add to systemInstruction
        // The last message in 'messages' is the current prompt, so we exclude it for history
        const historyMessages = messages.length > 1 
          ? messages.slice(0, -1).map((msg) => ({
              role: msg.role,
              text: msg.text,
            }))
          : [];
        
        const generateRequest: any = {
          model: disableStreaming ? (modelOverride || currentSettings.model || 'gemini_fast') : 'gemini_preview',
          prompt: prompt,
          messages: historyMessages.length > 0 ? historyMessages : undefined, // Backend will add to systemInstruction
          systemInstruction: options?.systemInstruction,
          temperature: options?.temperature,
          maxOutputTokens: options?.maxOutputTokens,
          webSearchEnabled: options?.groundingEnabled || false,
          toolCallingEnabled: currentSettings.toolUsageEnabled && finalTools && finalTools.length > 0,
          tools: finalTools,
          projectId: projectId,
          streamId: streamId,
          // Graph mode: require strict JSON output
          strictJSONEnabled: disableStreaming ? true : undefined,
        };
        
        try {
          // Call non-streaming API with abort signal
          const response = await api.genaiGenerate(generateRequest, abortController.signal);
          
          // Process response as if it came from streaming
          const content = response?.content || '';
          const model = response?.model || 'gemini_preview';
          const usage = response?.usage;
          const sources = response?.sources || [];
          const attributions = response?.attributions || [];
          
          // Graph mode: parse and process ontology commands
          if (disableStreaming) {
            try {
              console.log('[Chat] Graph mode: parsing ontology commands', { 
                contentLength: content.length, 
                contentPreview: content.substring(0, 200),
                contentEnd: content.substring(Math.max(0, content.length - 200)),
                usage: usage ? { 
                  outputTokens: usage.outputTokens, 
                  totalTokens: usage.totalTokens,
                  maxOutputTokens: options?.maxOutputTokens 
                } : null
              });
              
              // Update processing state - already set before API call, now update to parsing stage
              set((s) => ({
                processingCommands: true,
                commandsProgress: {
                  stage: 'parsing',
                  processed: 0,
                  total: 0,
                  currentOperation: 'Parsing commands...',
                },
              } as any));
              
              // Упрощенная проверка: парсер сам определит truncation на основе баланса скобок
              // Не делаем предварительную проверку - она была слишком строгой
              const parseResult = parseOntologyCommands(content, false);
              
              // Обрабатываем результат парсинга
              if (parseResult.parseError) {
                // Ошибка парсинга - показываем пользователю
                console.error('[Chat] Graph mode: Failed to parse ontology commands', {
                  error: parseResult.parseError,
                  wasTruncated: parseResult.wasTruncated,
                  contentPreview: content.substring(0, 500),
                });
                
                let errorMessage = `Failed to parse ontology commands: ${parseResult.parseError}`;
                if (parseResult.wasTruncated) {
                  errorMessage += ` Response was truncated. Consider increasing maxOutputTokens (current: ${options?.maxOutputTokens || 'default'}).`;
                }
                
                set((s) => ({
                  error: errorMessage,
                  streaming: false,
                  currentStreamId: undefined,
                  processingCommands: false,
                  commandsProgress: null,
                } as any));
                
                // Fall through to normal message handling - показываем ответ как текст
                // Но не создаем assistant interaction, так как это была ошибка парсинга
                return;
              }
              
              if (parseResult.wasTruncated) {
                // Ответ обрезан, но мы не пытались парсить
                console.warn('[Chat] Graph mode: Response was truncated, skipping parsing', {
                  contentLength: content.length,
                  maxOutputTokens: options?.maxOutputTokens,
                });
                
                set((s) => ({
                  error: `Response was truncated. Cannot parse incomplete JSON. Consider increasing maxOutputTokens (current: ${options?.maxOutputTokens || 'default'}).`,
                  streaming: false,
                  currentStreamId: undefined,
                  processingCommands: false,
                  commandsProgress: null,
                } as any));
                
                return;
              }
              
              console.log('[Chat] Graph mode: parsed commands', { 
                commandsCount: parseResult.commands.length,
                isValidJson: parseResult.isValidJson,
                commands: parseResult.commands.map(c => ({ operation: c.operation, type: c.type, id: c.id }))
              });
              
              if (parseResult.commands.length > 0) {
                // Есть команды для обработки
                try {
                  // Устанавливаем состояние обработки команд
                  set((s) => ({
                    processingCommands: true,
                    commandsProgress: {
                      stage: 'validating',
                      processed: 0,
                      total: parseResult.commands.length,
                    },
                  } as any));
                  
                  // Process commands and create/update/delete ontology interactions
                  const processResult = await processOntologyCommands(
                    parseResult.commands, 
                    projectId,
                    (progress) => {
                      // Callback для обновления прогресса
                      set((s) => ({
                        commandsProgress: progress,
                      } as any));
                    }
                  );
                  
                  // Логируем результат обработки
                  console.log('[Chat] Graph mode: Commands processed', {
                    totalCommands: parseResult.commands.length,
                    success: processResult.success,
                    createdIds: processResult.createdIds.length,
                    errors: processResult.errors.length,
                  });
                  
                  // Обновляем состояние - завершаем обработку команд
                  set((s) => {
                    const msgs = [...s.messages];
                    // Remove last assistant placeholder if exists
                    if (msgs.length > 0 && msgs[msgs.length - 1].role === 'assistant' && !msgs[msgs.length - 1].content) {
                      msgs.pop();
                    }
                    
                    // processOntologyCommands уже обновил error через setState внутри себя
                    // Не перезаписываем error здесь - он уже установлен правильно
                    
                    return {
                      messages: msgs,
                      streaming: false, 
                      currentStreamId: undefined,
                      usage,
                      processingCommands: false,
                      commandsProgress: {
                        stage: 'complete',
                        processed: processResult.createdIds.length,
                        total: parseResult.commands.length,
                      },
                      // Не трогаем error - processOntologyCommands уже установил его правильно
                    } as any;
                  });
                  
                  // Очищаем прогресс через небольшую задержку, чтобы пользователь увидел завершение
                  setTimeout(() => {
                    set((s) => ({
                      commandsProgress: null,
                    } as any));
                  }, 1000);
                  
                  return; // Exit early - commands processed, no message needed
                } catch (processErr) {
                  // Ошибка при обработке команд
                  console.error('[Chat] Graph mode: Failed to process ontology commands', processErr);
                  set((s) => {
                    const existingError = s.error;
                    const newError = `Failed to process ontology commands: ${processErr instanceof Error ? processErr.message : String(processErr)}. ${existingError ? existingError : ''}`;
                    return {
                      error: newError,
                      streaming: false,
                      currentStreamId: undefined,
                      processingCommands: false,
                      commandsProgress: null,
                    } as any;
                  });
                  return;
                }
              } else {
                // Нет команд - это может быть нормально (пустой ответ) или ошибка
                // Если JSON валидный, но команд нет - это нормально (LLM вернул пустой массив)
                // Если JSON невалидный - это ошибка (но мы уже обработали это выше)
                if (parseResult.isValidJson) {
                  // Валидный JSON, но команд нет - это нормально
                  console.log('[Chat] Graph mode: Valid JSON but no commands found - empty response');
                  
                  set((s) => {
                    const msgs = [...s.messages];
                    // Remove last assistant placeholder if exists
                    if (msgs.length > 0 && msgs[msgs.length - 1].role === 'assistant' && !msgs[msgs.length - 1].content) {
                      msgs.pop();
                    }
                    return {
                      messages: msgs,
                      streaming: false,
                      currentStreamId: undefined,
                      usage,
                      error: null,
                    } as any;
                  });
                  
                  return; // Exit early - no commands to process
                } else {
                  // Невалидный JSON - ошибка (но мы уже обработали это выше через parseError)
                  // Этот код не должен выполняться, но на всякий случай
                  console.error('[Chat] Graph mode: Invalid JSON and no commands - unexpected state');
                  set((s) => ({
                    error: 'Invalid JSON response from LLM. Expected ontology commands.',
                    streaming: false,
                    currentStreamId: undefined,
                  } as any));
                  return;
                }
              }
            } catch (err) {
              // Неожиданная ошибка при обработке
              console.error('[Chat] Graph mode: Unexpected error processing ontology commands', err);
              set((s) => ({
                error: `Unexpected error: ${err instanceof Error ? err.message : String(err)}`,
                streaming: false,
                currentStreamId: undefined,
              } as any));
              // Fall through to normal message handling if parsing fails
            }
          }
          
          // Update messages with full content
          set((s) => {
            const msgs = [...s.messages];
            let lastAssistantIndex = -1;
            for (let i = msgs.length - 1; i >= 0; i--) {
              if (msgs[i].role === 'assistant') {
                lastAssistantIndex = i;
                break;
              }
            }
            
            if (lastAssistantIndex >= 0) {
              msgs[lastAssistantIndex] = { ...msgs[lastAssistantIndex], content };
            } else {
              const id = Date.now().toString() + '-a';
              msgs.push({ id, role: 'assistant', content });
            }
            
            return { 
              messages: msgs, 
              streaming: false, 
              currentStreamId: undefined,
              usage,
              error: null,
            } as any;
          });
          
          // Create interaction with full response
          const now = Date.now();
          const assistantLocalId = now + '-ia';
          const originalMessageId = now + '-a';
          const assistantInteraction: LocalInteraction = {
            id: assistantLocalId,
            projectId,
            kind: 'assistant',
            content,
            createdAt: new Date().toISOString(),
            originalMessageId,
          };
          
          // Add sources and attributions if available
          if (sources.length > 0 || attributions.length > 0) {
            (assistantInteraction as any).sources = sources;
            (assistantInteraction as any).attributions = attributions;
          }
          
          // Add to interactions (check for duplicates first)
          set((s) => {
            // Check if interaction with this originalMessageId already exists (from WebSocket)
            const existingByOriginalId = s.interactions.find((it: any) => 
              (it as any).originalMessageId === originalMessageId
            );
            // Check if interaction with this local ID already exists
            const existingByLocalId = s.interactions.find((it: any) => it.id === assistantLocalId);
            
            if (existingByOriginalId || existingByLocalId) {
              console.log('[Chat] Non-streaming: Interaction already exists, skipping duplicate', {
                originalMessageId,
                assistantLocalId,
                existingByOriginalId: existingByOriginalId?.id,
                existingByLocalId: existingByLocalId?.id
              });
              return s;
            }
            
            return {
              interactions: [...s.interactions, assistantInteraction],
            } as any;
          });
          
          // Persist on server
          try {
            const payload: any = {
              kind: 'assistant',
              content,
              originalMessageId,
            };
            if (sources.length > 0) {
              payload.sources = sources;
            }
            if (attributions.length > 0) {
              payload.attributions = attributions;
            }
            if (usage) {
              payload.usage = usage;
            }
            
            const res = await api.createProjectInteraction(projectId, payload);
            
            // Update interaction ID (check for duplicates from WebSocket)
            set((s) => {
              const map = { ...(s.persistedByLocalId || {}) } as Record<string, string>;
              map[assistantLocalId] = res.id;
              const items = [...s.interactions];
              
              // Check if WebSocket already added interaction with server ID
              const existingByServerId = items.find((it: any) => it.id === res.id);
              if (existingByServerId) {
                // WebSocket already added it, remove local duplicate
                const idx = items.findIndex((it) => it.id === assistantLocalId);
                if (idx >= 0) {
                  items.splice(idx, 1);
                }
                console.log('[Chat] Non-streaming: WebSocket already added interaction, removed local duplicate', {
                  serverId: res.id,
                  assistantLocalId
                });
              } else {
                // Update local interaction ID
                const idx = items.findIndex((it) => it.id === assistantLocalId);
                if (idx >= 0) {
                  (items[idx] as any).id = res.id;
                }
              }
              
              return { interactions: items, persistedByLocalId: map } as any;
            });

            // Generate evaluations (frame expansions / compliance / bias) for non-streaming mode too.
            // This is required for gemini_preview ("experimental") where streaming is disabled/unreliable.
            (async () => {
              try {
                if (!projectId || projectId === 'local-unfold') return;

                // Skip evaluations for agent/scenario replies (same behavior as streaming onDone)
                const st = get();
                const meta = st.pendingAssistantMeta as any;
                const isAgentResponse =
                  meta?.subkind?.startsWith('agent.') ||
                  (typeof meta?.displayName === 'string' && meta.displayName.trim().length > 0);
                const isScenarioMessage = meta?.subkind?.startsWith('scenarios.');
                if (isAgentResponse || isScenarioMessage) return;

                // Collect up to 2 most recent user prompts from the already-built messages array
                // (it includes the current prompt we just sent).
                const userMessages = (messages || [])
                  .filter((m) => m && m.role === 'user' && typeof (m as any).text === 'string')
                  .map((m: any) => String(m.text))
                  .filter((t) => t.trim().length > 0)
                  .slice(-2);

                const { generateFrameExpansions } = await import('../lib/frameExpansions');
                await generateFrameExpansions(res.id, content, userMessages.length > 0 ? userMessages : undefined);

                try {
                  const { generateFrameComplianceCheck } = await import('../lib/frameComplianceCheck');
                  await generateFrameComplianceCheck(res.id, content, userMessages.length > 0 ? userMessages : undefined);
                } catch (e) {
                  console.warn('[FrameComplianceCheck] generation failed for non-streaming mode', e);
                }

                try {
                  const { evaluateBias } = await import('../lib/biasEvaluation');
                  const settingsStateEval = useGenAISettings.getState();
                  const evalModel = (settingsStateEval.stanceModel && settingsStateEval.stanceModel.trim())
                    ? settingsStateEval.stanceModel
                    : 'gemini_fast';
                  const preferredLang = (typeof window !== 'undefined'
                    ? (window.localStorage.getItem('preferredLanguage') || 'EN')
                    : 'EN').toLowerCase();
                  const contextText = userMessages.length > 0 ? userMessages[userMessages.length - 1] : undefined;
                  await evaluateBias({
                    messageId: res.id,
                    messageRole: 'assistant',
                    textToEvaluate: content,
                    contextText,
                    evalModel,
                    preferredLang,
                  });
                } catch (e) {
                  console.warn('[BiasEval] assistant eval failed for non-streaming mode', e);
                }
              } catch (e) {
                console.warn('[FrameExpansions] generation failed for non-streaming mode', e);
              }
            })();
          } catch (e) {
            console.warn('[Chat] Failed to persist non-streaming response', e);
          }
          
          // Generate discussion flow analysis (same as in onDone for streaming mode)
          // This ensures frame shift recommendations are generated for gemini_preview
          (async () => {
            try {
              if (!projectId || projectId === 'local-unfold') return;
              const { generateDiscussionFlowAnalysis } = await import('../lib/discussionFlowAnalysis');
              const interactions = get().interactions;
              const messages = get().messages;
              
              // Get all chat messages (user, assistant, agent)
              let chatMessages: Array<{ role: string; content: string }> = interactions
                .filter((it) => 
                  it.kind === 'user' || it.kind === 'assistant'
                )
                .map((it) => ({
                  role: it.kind === 'user' ? 'user' : 'assistant',
                  content: it.content || '',
                  ...((it as any).agentProfile ? { agentProfile: (it as any).agentProfile } : {}),
                }));
              
              // If no messages from interactions, fallback to messages array
              if (chatMessages.length === 0 && messages.length > 0) {
                chatMessages = messages.map((m) => ({
                  role: m.role,
                  content: m.content || '',
                }));
              }
              
              if (chatMessages.length === 0) return;
              
              // Check if analysis already exists
              const existingAnalysisInteraction = interactions.find((it) => 
                it.subkind === 'discussion.flow.analysis' && it.projectId === projectId
              );
              
              let existingAnalysis: any = null;
              if (existingAnalysisInteraction && existingAnalysisInteraction.content) {
                try {
                  existingAnalysis = JSON.parse(existingAnalysisInteraction.content || existingAnalysisInteraction.contentSanitized || '{}');
                } catch {}
              }
              
              // Get last pair for incremental update
              let lastPair: Array<{ role: string; content: string }> | undefined = undefined;
              if (existingAnalysis && content && content.trim()) {
                // Find last user message before this assistant response
                const lastUserInteraction = interactions
                  .filter((it) => it.kind === 'user')
                  .reverse()[0];
                if (lastUserInteraction && lastUserInteraction.content) {
                  lastPair = [
                    { role: 'user', content: lastUserInteraction.content },
                    { role: 'assistant', content: content }
                  ];
                }
              }
              
              await generateDiscussionFlowAnalysis(
                projectId,
                chatMessages,
                existingAnalysis,
                lastPair
              );
            } catch (e) {
              console.warn('[DiscussionFlowAnalysis] generation failed for non-streaming mode', e);
            }
          })();
          
        } catch (e: any) {
          // Check if request was aborted
          if (e?.name === 'AbortError' || abortController.signal.aborted) {
            console.log('[Chat] Non-streaming request was cancelled');
            set({ streaming: false, currentStreamId: undefined, abortController: undefined, processingCommands: false, commandsProgress: null });
            return;
          }
          console.error('[Chat] Non-streaming API error', e);
          set({ streaming: false, currentStreamId: undefined, abortController: undefined, processingCommands: false, commandsProgress: null, error: e?.message || 'Non-streaming request failed' });
        }
      } else {
        // Use streaming API for other models
        console.log('[Chat] startStream: Sending API request', { projectId, streamId });
        try {
          await api.genaiStream({ prompt, tools: finalTools, options, messages, projectId, streamId }, abortController.signal);
        } catch (e: any) {
          // Check if request was aborted
          if (e?.name === 'AbortError' || abortController.signal.aborted) {
            console.log('[Chat] Streaming request was cancelled');
            set({ streaming: false, currentStreamId: undefined, abortController: undefined });
            return;
          }
          throw e; // Re-throw if not aborted
        }
      }
    } catch (e: any) {
      // Check if request was aborted
      const state = get();
      if (e?.name === 'AbortError' || state.abortController?.signal.aborted) {
        console.log('[Chat] Request was cancelled');
        set({ streaming: false, currentStreamId: undefined, abortController: undefined, processingCommands: false, commandsProgress: null });
        return;
      }
      set({ streaming: false, currentStreamId: undefined, abortController: undefined, processingCommands: false, commandsProgress: null, error: e?.message || 'Stream start failed' });
    }
  },

  onStart: () => set((s) => {
    // Ensure we have an assistant message placeholder for streaming
    const msgs = [...s.messages];
    // Find the last assistant message (if any)
    let lastAssistantIndex = -1;
    for (let i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i].role === 'assistant') {
        lastAssistantIndex = i;
        break;
      }
    }
    
    // If no assistant message exists, create one
    if (lastAssistantIndex === -1) {
      const id = Date.now().toString() + '-a';
      msgs.push({ id, role: 'assistant', content: '' });
    } else {
      // Ensure the last assistant message has empty content (for thinking placeholder)
      const lastAssistant = msgs[lastAssistantIndex];
      if (lastAssistant.content && lastAssistant.content.trim()) {
        // If last assistant has content, don't modify it - streaming has started
        // But still ensure streaming flag is set
      } else {
        // Last assistant is empty - ensure it stays empty for thinking placeholder
        msgs[lastAssistantIndex] = { ...lastAssistant, content: '' };
      }
    }
    
    return { 
      streaming: true, 
      error: null, 
      toolCalls: [], 
      toolResults: [], 
      attributions: [], 
      usage: undefined,
      messages: msgs,
      // Keep currentStreamId if already set (from startStream)
      // If not set, it will be set when stream.start event arrives
    } as any;
  }),
  onChunk: (text: string) => {
    set((s) => {
      const msgs = [...s.messages];
      
      // Find the last assistant message that was created after the last user message
      // This ensures we update the correct message during streaming, not an old one
      let lastUserIndex = -1;
      for (let i = msgs.length - 1; i >= 0; i--) {
        if (msgs[i].role === 'user') {
          lastUserIndex = i;
          break;
        }
      }
      
      // Find assistant message after the last user message (or the last assistant if no user found)
      let found = false;
      const searchStart = lastUserIndex >= 0 ? lastUserIndex + 1 : 0;
      for (let i = msgs.length - 1; i >= searchStart; i--) {
        if (msgs[i].role === 'assistant') {
          msgs[i] = { ...msgs[i], content: msgs[i].content + text };
          found = true;
          break;
        }
      }
      
      // If no assistant message found after last user (e.g., after hot-reload), create one
      if (!found) {
        const id = Date.now().toString() + '-a';
        msgs.push({ id, role: 'assistant', content: text });
      }
      
      // CRITICAL: Update _lastUpdate to force UI re-render when messages content changes
      // This ensures useMergedMessages recalculates even if messages.length doesn't change
      return { messages: msgs, _lastUpdate: Date.now() } as any;
    });
  },
  onDone: () =>
    set((s) => {
      // capture final assistant message into local interactions
      const projectId = s.projectId;
      const lastAssistant = [...s.messages].reverse().find((m) => m.role === 'assistant');
      if (lastAssistant && lastAssistant.content.trim()) {
        // Check if interaction for this message already exists (prevent duplicates)
        const existingInteraction = s.interactions.find((it) => 
          it.kind === 'assistant' && 
          ((it as any).originalMessageId === lastAssistant.id || it.id === lastAssistant.id)
        );
        if (existingInteraction) {
          console.log('[onDone] Interaction already exists for this assistant message, skipping duplicate creation', { 
            messageId: lastAssistant.id, 
            existingInteractionId: existingInteraction.id 
          });
          return {} as any; // Return empty update to prevent duplicate
        }
        
        // Check if a graph interaction was already created from tool result for this stream
        // If graph exists, we should still create the text interaction but ensure graph is visible
        const hasGraphFromToolResult = s.interactions.some((it) => 
          it.kind === 'assistant' && 
          it.subkind === 'graph' && 
          (it as any).author?.kind === 'tool' &&
          // Check if graph was created recently (within last 10 seconds)
          new Date(it.createdAt).getTime() > Date.now() - 10000
        );
        
        // If graph was created via tool and assistant message is empty or very short, skip text interaction
        // This prevents empty text messages from hiding the graph
        if (hasGraphFromToolResult && (!lastAssistant.content.trim() || lastAssistant.content.trim().length < 30)) {
          console.log('[onDone] Graph was created via tool result, skipping empty/confirmatory text interaction', { 
            contentLength: lastAssistant.content.trim().length,
            hasGraph: true 
          });
          return {} as any;
        }
        
        const id = Date.now().toString() + '-ia';
        const createdAt = new Date().toISOString();
        const meta = s.pendingAssistantMeta;
        const opts = s.pendingAssistantOptions || {};
        const nextMeta = { ...s.messageMeta } as Record<string, { headerText?: string; title?: string; leftAccent?: boolean; bgOverride?: string; refineEnabled?: boolean; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } }>;
        if (meta?.displayName) {
          nextMeta[lastAssistant.id] = { headerText: meta.displayName, leftAccent: true, refineEnabled: !!meta?.agentProfile?.capabilities?.refinePrevious, agentProfile: meta?.agentProfile };
        }
        // Parse hidden @sx with minimal validation and sanitize content centrally
        const { sanitized, instructions: sx } = parseSxInstructionsFromContent(lastAssistant.content);

        // Try to parse graph-format response (chartTitle, chartType, table, sources)
        let parsedGraph: any = null;
        let detectedGraphData: { chartTitle: string; chartType?: string; table: any[][]; sources: string[] } | undefined = undefined;
        const wasGraphMode = (opts?.systemInstruction || '').includes('GRAPH MODE');
        try {
          let content = (lastAssistant.content || '').toString().trim();
          // Strip code fences (both markdown and plain)
          content = content.replace(/^```[a-zA-Z]*\n([\s\S]*?)\n```$/m, '$1').trim();
          content = content.replace(/^```\n([\s\S]*?)\n```$/m, '$1').trim();
          
          // In graph mode, prioritize finding JSON even if there's text before/after
          if (wasGraphMode) {
            // Try to find the first valid JSON object (look for { ... } structure)
            const jsonMatch = content.match(/\{[\s\S]*\}/);
            if (jsonMatch && jsonMatch[0]) {
              try {
                parsedGraph = JSON.parse(jsonMatch[0]);
              } catch {}
            }
          }
          
          // If no JSON found yet, try direct parse
          if (!parsedGraph) {
            if ((content.startsWith('{') || content.startsWith('['))) {
              parsedGraph = JSON.parse(content);
            } else {
              // Try to find JSON within content (non-greedy, find first complete object)
              const match = content.match(/\{[\s\S]*?\}/);
              if (match && match[0]) {
                try {
                  parsedGraph = JSON.parse(match[0]);
                } catch {}
              }
            }
          }
          
          // Validate graph structure
          if (
            parsedGraph &&
            typeof parsedGraph === 'object' &&
            typeof parsedGraph.chartTitle === 'string' &&
            Array.isArray(parsedGraph.table) &&
            Array.isArray(parsedGraph.sources)
          ) {
            // Validate chartType is from allowed dictionary, fallback to 'line' if invalid/missing
            const allowedTypes = ['line', 'bar', 'area', 'stackedBar', 'stackedArea', 'composed'];
            const chartType = typeof parsedGraph.chartType === 'string' && allowedTypes.includes(parsedGraph.chartType)
              ? parsedGraph.chartType
              : 'line';
            detectedGraphData = {
              chartTitle: parsedGraph.chartTitle,
              chartType: chartType,
              table: parsedGraph.table,
              sources: parsedGraph.sources,
            };
            console.log('[Graph] Detected valid graph response', { chartTitle: detectedGraphData.chartTitle, chartType: detectedGraphData.chartType, tableRows: detectedGraphData.table.length, sources: detectedGraphData.sources.length });
          } else if (wasGraphMode && !detectedGraphData) {
            // Graph mode was requested but response is not valid JSON graph format
            console.warn('[Graph] Graph mode was requested but model returned non-JSON response. Content preview:', content.substring(0, 300));
            console.warn('[Graph] Full content length:', content.length, 'Starts with:', content.substring(0, 50));
          }
        } catch (e) {
          // Not a graph response, continue with normal flow
          if (wasGraphMode) {
            console.warn('[Graph] Graph mode was requested but JSON parsing failed:', e);
            console.warn('[Graph] Content that failed to parse:', (lastAssistant.content || '').toString().substring(0, 300));
          }
        }

        // Ensure we persist which model produced this assistant answer (even for non-agent replies)
        const settingsModelAlias = (() => {
          try {
            return (useGenAISettings.getState() as any)?.model as string | undefined;
          } catch {
            return undefined;
          }
        })();
        const modelAlias =
          (meta as any)?.agentProfile?.model ||
          (opts as any)?.modelOverride ||
          settingsModelAlias;
        const resolvedAgentProfile = (() => {
          if (meta?.agentProfile) return meta.agentProfile;
          if (typeof modelAlias !== 'string' || !modelAlias) return undefined;
          const mapped = mapAliasToModel(modelAlias);
          if (mapped) return { provider: mapped.provider, model: mapped.model } as any;
          const lower = modelAlias.toLowerCase();
          const inferredProvider =
            lower.startsWith('gpt-') ? 'openai' :
            lower.startsWith('gemini-') ? 'gemini' :
            lower.startsWith('grok-') ? 'grok' :
            lower.startsWith('claude-') ? 'anthropic' :
            lower.startsWith('deepseek-') ? 'deepseek' :
            undefined;
          return inferredProvider ? ({ provider: inferredProvider, model: modelAlias } as any) : undefined;
        })();
        const resolvedModel = resolvedAgentProfile?.model || (typeof modelAlias === 'string' ? modelAlias : undefined);

        const base = { id, projectId, kind: 'assistant' as InteractionKind, subkind: detectedGraphData ? 'graph' : (s.pendingAssistantMeta as any)?.subkind, content: lastAssistant.content, contentSanitized: sanitized, sxInstructions: sx, graphData: detectedGraphData, createdAt, author: meta ? { participantId: `agent:${meta.displayName || 'model'}`, kind: 'ai', displayName: meta.displayName } : undefined, agentProfile: resolvedAgentProfile, model: resolvedModel, systemInstructionApplied: typeof opts?.systemInstruction === 'string' ? opts.systemInstruction : undefined, language: opts?.__lang, generationParams: { temperature: opts?.temperature, maxOutputTokens: opts?.maxOutputTokens, groundingEnabled: opts?.groundingEnabled }, originalMessageId: lastAssistant.id, searchQueries: (s.searchQueries || []).filter(Boolean), sources: (s.sources || []).filter(Boolean), usage: s.usage } as LocalInteraction;
        const replyTo = (s.pendingAssistantMeta as any)?.replyTo as string | undefined;
        if (replyTo) {
          (base as any).replyTo = replyTo;
          (base as any).relations = [{ type: 'answers', targetId: replyTo }];
        }
        // duplicate meta under interaction id as well for interaction-based rendering
        if (meta?.displayName) {
          (nextMeta as any)[id] = { headerText: meta.displayName, leftAccent: true, refineEnabled: !!meta?.agentProfile?.capabilities?.refinePrevious, agentProfile: meta?.agentProfile };
        }
        // Merge artifacts into centralized state (deduplicated)
        const currentArtifacts = s.artifacts || { entities: [], metrics: [] };
        const nextEntities: SxEntityMark[] = [...currentArtifacts.entities];
        const nextMetrics: SxMonitorMetric[] = [...currentArtifacts.metrics];
        const seenEntity = new Set(nextEntities.map((e) => `entity:${(e as any).kind || 'other'}:${String((e as any).name || (e as any).id || '').toLowerCase()}`));
        const seenMetric = new Set(nextMetrics.map((m) => `metric:${String((m as any).id || (m as any).metricName || (m as any).name || '').toLowerCase()}`));
        const newEntities: SxEntityMark[] = [];
        const newMetrics: SxMonitorMetric[] = [];
        for (const instr of sx) {
          if (instr.type === 'entity_mark') {
            const e = instr as SxEntityMark;
            const key = `entity:${(e as any).kind || 'other'}:${String((e as any).name || (e as any).id || '').toLowerCase()}`;
            if (!seenEntity.has(key)) { 
              seenEntity.add(key); 
              nextEntities.push(e);
              newEntities.push(e);
            }
          } else if (instr.type === 'monitor_metric') {
            const m = instr as SxMonitorMetric;
            const key = `metric:${String((m as any).id || (m as any).metricName || (m as any).name || '').toLowerCase()}`;
            if (!seenMetric.has(key)) { 
              seenMetric.add(key); 
              nextMetrics.push(m);
              newMetrics.push(m);
            }
          }
        }
        // Сохраняем новые артефакты на сервер
        if ((newEntities.length > 0 || newMetrics.length > 0) && projectId && projectId !== 'local-unfold') {
          persistArtifacts(projectId, newEntities, newMetrics).catch((err) => {
            console.error('[GenAIChat] Failed to persist artifacts', err);
          });
        }
        const nextInteractions = [...s.interactions, base];
        (async () => {
          try {
            const payload: any = {
              kind: 'assistant',
              content: lastAssistant.content,
              subkind: detectedGraphData ? 'graph' : (meta as any)?.subkind,
              contentSanitized: sanitized,
              author: meta ? { participantId: `agent:${meta.displayName || 'model'}`, kind: 'ai', displayName: meta.displayName } : undefined,
              agentProfile: resolvedAgentProfile,
              model: resolvedModel,
              systemInstructionApplied: typeof opts?.systemInstruction === 'string' ? opts.systemInstruction : undefined,
              language: opts?.__lang,
              generationParams: { temperature: opts?.temperature, maxOutputTokens: opts?.maxOutputTokens, groundingEnabled: opts?.groundingEnabled },
              originalMessageId: lastAssistant.id,
              usage: s.usage,
              searchQueries: s.searchQueries,
              sources: s.sources,
              graphData: detectedGraphData,
            };
            const map = (get() as any).persistedByLocalId || {};
            const replyToMsg = (s.pendingAssistantMeta as any)?.replyTo as string | undefined;
            if (replyToMsg && map[replyToMsg]) {
              payload.replyTo = map[replyToMsg];
              payload.relations = [{ type: 'answers', targetId: map[replyToMsg] }];
            } else if ((base as any).replyTo) {
              payload.replyTo = (base as any).replyTo;
              if ((base as any).relations) payload.relations = (base as any).relations;
            }
            const res = await api.createProjectInteraction(projectId, payload);
            set((st) => {
              // FIXED: Only update if id actually changed to prevent unnecessary re-renders
              const idx = st.interactions.findIndex((it) => it.id === id);
              if (idx < 0) return {} as any; // Interaction not found, skip update
              
              const currentItem = st.interactions[idx];
              // Skip update if id is already the same (already persisted)
              if (currentItem.id === res.id) return {} as any;
              
              // Update in place without creating new array if possible
              const items = [...st.interactions];
              (items[idx] as any).id = res.id;
              const m = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
              m[id] = res.id;
              m[lastAssistant.id] = res.id;
              
              return { interactions: items, persistedByLocalId: m } as any;
            });
          } catch (e) {
            console.warn('[persist assistant] failed', e);
          }
        })();

        // Title generation is now handled in startStream immediately after first user message
        // No need to generate here in onDone - it would be redundant

        // Trigger meta-analysis only for standard USER→ASSISTANT exchanges on server-backed projects
        // DISABLED: temporarily disabled to reduce LLM API calls
        /*
        (async () => {
          try {
            if (!projectId || projectId === 'local-unfold') return;
            // Skip if this is an agent reply (has displayName)
            if (meta?.displayName) return;
            const inters = get().interactions || [];
            if (inters.length === 0) return;
            // Freshness guard: do not analyze if aggregate is up-to-date
            const agg = get().stanceAggregate;
            const last = inters[inters.length - 1];
            const aggUpdatedAt = agg && (agg.updatedAt || agg.UpdatedAt);
            if (aggUpdatedAt && last?.createdAt) {
              try {
                const a = new Date(aggUpdatedAt);
                const b = new Date(last.createdAt);
                if (!Number.isNaN(a.getTime()) && !Number.isNaN(b.getTime()) && a >= b) {
                  console.log('[Meta] Skip analyze: aggregate is fresh');
                  return;
                }
              } catch {}
            }
            // Decide mode: periodic full recompute every ~12 exchanges
            const messagesSinceLastFull = (agg?.lastAnalyzedMessageCount as number) || 0;
            const doFull = (inters.length - messagesSinceLastFull) >= 12 || !agg;
            let req: AnalyzeRequest;
            if (doFull) {
              // Full recompute: last up to 20 refs
              const refs: AnalyzeInteractionRef[] = [];
              const take = Math.min(20, inters.length);
              for (let i = Math.max(0, inters.length - take); i < inters.length; i++) {
                const it = inters[i] as any;
                if (!it || !it.content) continue;
                const isServerBacked = it.id && typeof it.id === 'string' && !it.id.endsWith('-iu') && !it.id.endsWith('-ia') && it.id.indexOf('-eval-') === -1 && it.id.indexOf('eval-bias-auto') === -1;
                if (isServerBacked) refs.push({ refId: it.id });
                else {
                  const role = (it.kind === 'assistant') ? 'assistant' : (it.kind === 'system' ? 'system' : 'user');
                  refs.push({ inline: { role, text: String(it.content), timestamp: it.createdAt } });
                }
              }
              req = { analyzers: ['thinking','biases','decisions','comm','learning'], interactions: refs } as any;
            } else {
              // Incremental: previous aggregate + last pair (user, assistant)
              const prev = (agg?.lastResults || agg?.result || agg) as any;
              const refs: AnalyzeInteractionRef[] = [];
              const take = Math.min(2, inters.length);
              for (let i = Math.max(0, inters.length - take); i < inters.length; i++) {
                const it = inters[i] as any;
                if (!it || !it.content) continue;
                const role = (it.kind === 'assistant') ? 'assistant' : (it.kind === 'system' ? 'system' : 'user');
                refs.push({ inline: { role, text: String(it.content), timestamp: it.createdAt } });
              }
              req = { analyzers: ['thinking','biases','decisions','comm','learning'], interactions: refs, previousAggregate: prev } as any;
              console.log('[Meta] Incremental update', { pair: refs.length });
            }
            await api.metaAnalyze(projectId, req as any);
            await get().loadStanceAggregate();
          } catch (e) {
            console.warn('[Meta] analysis failed', e);
          }
        })();
        */

        // Generate discussion flow analysis (blind spots, frame shift recommendations, frame, confidence)
        (async () => {
          try {
            if (!projectId || projectId === 'local-unfold') return;
            const { generateDiscussionFlowAnalysis } = await import('../lib/discussionFlowAnalysis');
            const interactions = get().interactions;
            const messages = get().messages;
            
            // Get all chat messages (user, assistant, agent)
            let chatMessages: Array<{ role: string; content: string }> = interactions
              .filter((it) => 
                it.kind === 'user' || it.kind === 'assistant'
              )
              .map((it) => ({
                role: it.kind === 'user' ? 'user' : 'assistant',
                content: it.content || '',
                ...((it as any).agentProfile ? { agentProfile: (it as any).agentProfile } : {}),
              }));
            
            // If no messages from interactions, fallback to messages array
            if (chatMessages.length === 0 && messages.length > 0) {
              chatMessages = messages.map((m) => ({
                role: m.role,
                content: m.content || '',
              }));
            }
            
            if (chatMessages.length === 0) return;
            
            // Check if analysis already exists
            const existingAnalysisInteraction = interactions.find((it) => 
              it.subkind === 'discussion.flow.analysis' && it.projectId === projectId
            );
            
            let existingAnalysis: any = null;
            if (existingAnalysisInteraction && existingAnalysisInteraction.content) {
              try {
                existingAnalysis = JSON.parse(existingAnalysisInteraction.content || existingAnalysisInteraction.contentSanitized || '{}');
              } catch {}
            }
            
            // Get last pair for incremental update
            let lastPair: Array<{ role: string; content: string }> | undefined = undefined;
            if (existingAnalysis && lastAssistant && lastAssistant.content.trim()) {
              // Find last user message before this assistant response
              const lastUserInteraction = interactions
                .filter((it) => it.kind === 'user')
                .reverse()[0];
              if (lastUserInteraction && lastUserInteraction.content) {
                lastPair = [
                  { role: 'user', content: lastUserInteraction.content },
                  { role: 'assistant', content: lastAssistant.content }
                ];
              }
            }
            
            await generateDiscussionFlowAnalysis(
              projectId,
              chatMessages,
              existingAnalysis,
              lastPair
            );
          } catch (e) {
            console.warn('[DiscussionFlowAnalysis] generation failed', e);
          }
        })();

        // Generate user stance analysis (How you engage)
        (async () => {
          try {
            if (!projectId || projectId === 'local-unfold') return;
            const { generateUserStanceAnalysis } = await import('../lib/userStanceAnalysis');
            const interactions = get().interactions;
            const messages = get().messages;
            
            // Get all chat messages (user, assistant, agent)
            let chatMessages: Array<{ role: string; content: string }> = interactions
              .filter((it) => 
                it.kind === 'user' || it.kind === 'assistant'
              )
              .map((it) => ({
                role: it.kind === 'user' ? 'user' : 'assistant',
                content: it.content || '',
                ...((it as any).agentProfile ? { agentProfile: (it as any).agentProfile } : {}),
              }));
            
            // If no messages from interactions, fallback to messages array
            if (chatMessages.length === 0 && messages.length > 0) {
              chatMessages = messages.map((m) => ({
                role: m.role,
                content: m.content || '',
              }));
            }
            
            if (chatMessages.length === 0) return;
            
            // Check if analysis already exists
            const existingAnalysisInteraction = interactions.find((it) => 
              it.subkind === 'eval.stance' && it.projectId === projectId && !it.replyTo
            );
            
            let existingAnalysis: any = null;
            if (existingAnalysisInteraction && existingAnalysisInteraction.content) {
              try {
                existingAnalysis = JSON.parse(existingAnalysisInteraction.content || '{}');
              } catch {}
            }
            
            // Get last pair for incremental update
            let lastPair: Array<{ role: string; content: string }> | undefined = undefined;
            if (existingAnalysis && lastAssistant && lastAssistant.content.trim()) {
              // Find last user message before this assistant response
              const lastUserInteraction = interactions
                .filter((it) => it.kind === 'user')
                .reverse()[0];
              if (lastUserInteraction && lastUserInteraction.content) {
                lastPair = [
                  { role: 'user', content: lastUserInteraction.content },
                  { role: 'assistant', content: lastAssistant.content }
                ];
              }
            }
            
            await generateUserStanceAnalysis(
              projectId,
              chatMessages,
              existingAnalysis,
              lastPair
            );
          } catch (e) {
            console.warn('[UserStanceAnalysis] generation failed', e);
          }
        })();

        // Generate frame expansions for assistant responses (excludes agent responses)
        (async () => {
          try {
            if (!projectId || projectId === 'local-unfold') return;
            // Check if this is an agent response or scenario message - these don't need evaluations since they are system responses, not conversational content
            const state = get();
            const meta = state.pendingAssistantMeta;
            const isAgentResponse = meta?.subkind?.startsWith('agent.') || !!meta?.agentProfile;
            const isScenarioMessage = meta?.subkind?.startsWith('scenarios.');
            
            // Count all assistant interactions (including agents) to ensure we have at least 1 response
            // Count includes this one we're about to add, so we check if we'll have >= 1 after adding
            // Exclude ontology interactions and graph.chat responses (they are structured data, not conversational content)
            const currentAssistants = state.interactions.filter((it) => 
              it.kind === 'assistant' &&
              it.subkind !== 'graph.chat'
            );
            // After adding this interaction, we'll have currentAssistants.length + 1
            if (currentAssistants.length + 1 < 1) {
              return;
            }
            // Generate expansions for the last assistant message
            // Skip ontology interactions, graph.chat responses, and agent responses (they are structured data or responses to previous messages, not direct user queries)
            if (lastAssistant && 
                lastAssistant.content && 
                lastAssistant.content.trim() &&
                (lastAssistant as any).kind !== 'ontology' &&
                (lastAssistant as any).subkind !== 'graph.chat' &&
                !isAgentResponse) {
              
              // Check if expansion interaction already exists
              const state = get();
              const map = (state as any).persistedByLocalId || {};
              const serverId = map[lastAssistant.id] || lastAssistant.id;
              const reverseMap: Record<string, string[]> = {};
              Object.entries(map).forEach(([local, server]) => {
                if (!reverseMap[server as string]) reverseMap[server as string] = [];
                reverseMap[server as string].push(local);
              });
              const localIds = reverseMap[lastAssistant.id] || [];
              
              const existingExpansion = state.interactions.find((it) => 
                it.subkind === 'eval.frame_expansions' && 
                (it.replyTo === lastAssistant.id || it.replyTo === serverId || localIds.includes(it.replyTo || '') ||
                 (it.relations && it.relations.some((r: { type: string; targetId: string }) => r.targetId === lastAssistant.id || r.targetId === serverId)))
              );
              
              if (existingExpansion) {
                return;
              }
              
              // Find the last 2 user messages that prompted this response
              const userMessages: string[] = [];
              const replyTo = (meta as any)?.replyTo as string | undefined;
              const allItems = [...get().messages, ...get().interactions];
              
              if (replyTo) {
                // Start from replyTo and collect up to 2 user messages (most recent first)
                const replyToIdx = allItems.findIndex((item: any) => 
                  item.id === replyTo || (item as any).originalMessageId === replyTo
                );
                if (replyToIdx >= 0) {
                  // Collect user messages backwards from replyTo (up to 2 most recent)
                  for (let i = replyToIdx; i >= 0 && userMessages.length < 2; i--) {
                    const item = allItems[i];
                    const isUser = ('kind' in item && item.kind === 'user') || ('role' in item && item.role === 'user');
                    // Exclude graph.chat user messages (they are ontology commands, not conversational content)
                    if (isUser && 
                        (item as any).subkind !== 'graph.chat' &&
                        (item.content || (item as any).contentSanitized)) {
                      const content = (item.content || (item as any).contentSanitized || '') as string;
                      if (content.trim()) {
                        userMessages.unshift(content); // Add to beginning to maintain chronological order
                      }
                    }
                  }
                }
              } else {
                // If no explicit replyTo, find the last 2 user messages before this assistant message
                const assistantIdx = allItems.findIndex((item: any) => {
                  const isAssistant = ('kind' in item && item.kind === 'assistant') || ('role' in item && item.role === 'assistant');
                  return (item.id === lastAssistant.id || (item as any).originalMessageId === lastAssistant.id) && isAssistant;
                });
                if (assistantIdx > 0) {
                  // Collect backwards from assistant message (up to 2 most recent user messages)
                  for (let i = assistantIdx - 1; i >= 0 && userMessages.length < 2; i--) {
                    const item = allItems[i];
                    const isUser = ('kind' in item && item.kind === 'user') || ('role' in item && item.role === 'user');
                    // Exclude graph.chat user messages (they are ontology commands, not conversational content)
                    if (isUser && 
                        (item as any).subkind !== 'graph.chat' &&
                        (item.content || (item as any).contentSanitized)) {
                      const content = (item.content || (item as any).contentSanitized || '') as string;
                      if (content.trim()) {
                        userMessages.unshift(content); // Add to beginning to maintain chronological order
                      }
                    }
                  }
                }
              }
              
              // Run frame expansions, compliance check, and bias evaluation in parallel to reduce latency
              // Note: all evaluations are skipped for agent responses and scenario messages (they are system responses, not conversational content)
              const expansionsPromise = (isAgentResponse || isScenarioMessage) ? Promise.resolve() : (async () => {
                try {
                  const { generateFrameExpansions } = await import('../lib/frameExpansions');
                  await generateFrameExpansions(lastAssistant.id, lastAssistant.content, userMessages.length > 0 ? userMessages : undefined);
                } catch (e) {
                  console.warn('[FrameExpansions] generation failed', e);
                }
              })();

              const compliancePromise = (isAgentResponse || isScenarioMessage) ? Promise.resolve() : (async () => {
                try {
                  const { generateFrameComplianceCheck } = await import('../lib/frameComplianceCheck');
                  await generateFrameComplianceCheck(lastAssistant.id, lastAssistant.content, userMessages.length > 0 ? userMessages : undefined);
                } catch (e) {
                  console.warn('[FrameComplianceCheck] generation failed', e);
                }
              })();

              const biasPromise = (isAgentResponse || isScenarioMessage) ? Promise.resolve() : (async () => {
                try {
                  const { evaluateBias } = await import('../lib/biasEvaluation');
                  const settingsStateEval = useGenAISettings.getState();
                  const evalModel = (settingsStateEval.stanceModel && settingsStateEval.stanceModel.trim())
                    ? settingsStateEval.stanceModel
                    : 'gemini_fast';
                  const preferredLang = (typeof window !== 'undefined'
                    ? (window.localStorage.getItem('preferredLanguage') || 'EN')
                    : 'EN').toLowerCase();
                  const contextText = userMessages.length > 0 ? userMessages[userMessages.length - 1] : undefined;
                  await evaluateBias({
                    messageId: lastAssistant.id,
                    messageRole: 'assistant',
                    textToEvaluate: lastAssistant.content,
                    contextText,
                    evalModel,
                    preferredLang,
                  });
                } catch (e) {
                  console.warn('[BiasEval] assistant eval failed', e);
                }
              })();

              await Promise.all([expansionsPromise, compliancePromise, biasPromise]);
            }
          } catch (e) {
            console.warn('[FrameExpansions] generation failed', e);
          }
        })();

        return { streaming: false, currentStreamId: undefined, abortController: undefined, pendingAssistantMeta: undefined, pendingAssistantOptions: undefined, pendingAssistantPrompt: undefined, pendingAssistantTools: undefined, messageMeta: nextMeta, interactions: nextInteractions, artifacts: { entities: nextEntities, metrics: nextMetrics } } as any;
      }
      // If we are here, assistant produced no chunks and content is empty.
      // Remove the trailing empty placeholder and create a stub interaction so the user sees the outcome.
      const msgs = [...s.messages];
      if (msgs.length > 0 && msgs[msgs.length - 1].role === 'assistant' && !msgs[msgs.length - 1].content) {
        msgs.pop();
      }
      const id = Date.now().toString() + '-ia';
      const createdAt = new Date().toISOString();
      const meta = s.pendingAssistantMeta;
      const opts = s.pendingAssistantOptions || {};
      const nextMeta = { ...s.messageMeta } as Record<string, { headerText?: string; title?: string; leftAccent?: boolean; bgOverride?: string; refineEnabled?: boolean; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } }>;
      const stub = 'Empty reply (no text was returned by the model).';

      // Preserve model info even when the model returns no text
      const settingsModelAlias = (() => {
        try {
          return (useGenAISettings.getState() as any)?.model as string | undefined;
        } catch {
          return undefined;
        }
      })();
      const modelAlias =
        (meta as any)?.agentProfile?.model ||
        (opts as any)?.modelOverride ||
        settingsModelAlias;
      const resolvedAgentProfile = (() => {
        if (meta?.agentProfile) return meta.agentProfile;
        if (typeof modelAlias !== 'string' || !modelAlias) return undefined;
        const mapped = mapAliasToModel(modelAlias);
        if (mapped) return { provider: mapped.provider, model: mapped.model } as any;
        const lower = modelAlias.toLowerCase();
        const inferredProvider =
          lower.startsWith('gpt-') ? 'openai' :
          lower.startsWith('gemini-') ? 'gemini' :
          lower.startsWith('grok-') ? 'grok' :
          lower.startsWith('claude-') ? 'anthropic' :
          lower.startsWith('deepseek-') ? 'deepseek' :
          undefined;
        return inferredProvider ? ({ provider: inferredProvider, model: modelAlias } as any) : undefined;
      })();
      const resolvedModel = resolvedAgentProfile?.model || (typeof modelAlias === 'string' ? modelAlias : undefined);
      const base: LocalInteraction = {
        id,
        projectId,
        kind: 'assistant',
        subkind: (s.pendingAssistantMeta as any)?.subkind,
        content: stub,
        contentSanitized: stub,
        createdAt,
        author: meta ? { participantId: `agent:${meta.displayName || 'model'}`, kind: 'ai', displayName: meta.displayName } : undefined,
        agentProfile: resolvedAgentProfile,
        model: resolvedModel,
        systemInstructionApplied: typeof opts?.systemInstruction === 'string' ? opts.systemInstruction : undefined,
        language: opts?.__lang,
        generationParams: { temperature: opts?.temperature, maxOutputTokens: opts?.maxOutputTokens, groundingEnabled: opts?.groundingEnabled },
        originalMessageId: id,
      } as any;
      const replyTo = (s.pendingAssistantMeta as any)?.replyTo as string | undefined;
      if (replyTo) {
        (base as any).replyTo = replyTo;
        (base as any).relations = [{ type: 'answers', targetId: replyTo }];
      }
      if (meta?.displayName) {
        nextMeta[id] = { headerText: meta.displayName, leftAccent: true, refineEnabled: !!meta?.agentProfile?.capabilities?.refinePrevious, agentProfile: meta?.agentProfile };
      }
      console.warn('[GenAI Stream] done without chunks: created empty reply stub');
      return { streaming: false, currentStreamId: undefined, abortController: undefined, messages: msgs, pendingAssistantMeta: undefined, pendingAssistantOptions: undefined, pendingAssistantPrompt: undefined, pendingAssistantTools: undefined, messageMeta: nextMeta, interactions: [...s.interactions, base] } as any;
    }),
  onError: (err) => {
    try {
      const st = get();
      const model = (st.pendingAssistantMeta as any)?.agentProfile?.model || st.pendingAssistantOptions?.modelOverride;
      console.error('[GenAI Stream] error', {
        error: err,
        kind: (st.pendingAssistantMeta as any)?.subkind || 'chat',
        agentName: st.pendingAssistantMeta?.displayName,
        provider: (st.pendingAssistantMeta as any)?.agentProfile?.provider,
        model,
        promptChars: (st.pendingAssistantPrompt || '').length,
        lang: st.pendingAssistantOptions?.__lang,
        hasTools: Array.isArray(st.pendingAssistantTools) && st.pendingAssistantTools.length > 0,
        groundingEnabled: !!st.pendingAssistantOptions?.groundingEnabled,
      });
      
      // Parse error message to provide user-friendly explanation
      let userFriendlyError = err || 'Stream error';
      try {
        const errorStr = typeof err === 'string' ? err : JSON.stringify(err);
        
        // Try to parse JSON error if it's embedded in the string
        let parsedError: any = null;
        try {
          // Check if error contains JSON array/object
          const jsonMatch = errorStr.match(/\[({.*})\]|({.*})/s);
          if (jsonMatch) {
            parsedError = JSON.parse(jsonMatch[1] || jsonMatch[2]);
          }
        } catch {}
        
        // Extract error details from parsed JSON or string
        const errorCode = parsedError?.error?.code || (errorStr.match(/404|401|403|429|500/) || [])[0];
        const errorMessage = parsedError?.error?.message || errorStr;
        const errorStatus = parsedError?.error?.status || (errorStr.includes('NOT_FOUND') ? 'NOT_FOUND' : null);
        
        // Check for model not found errors (404)
        if (errorCode === 404 || errorStatus === 'NOT_FOUND' || errorStr.includes('not found') || errorMessage.includes('not found')) {
          // Check if it's a model-specific error
          if (errorMessage.includes('gemini-3-pro') || errorStr.includes('gemini-3-pro') || errorStr.includes('gemini_preview') || model === 'gemini_preview' || model === 'gemini-3-pro-preview') {
            userFriendlyError = 'Модель Gemini 3 Pro Preview временно недоступна. Пожалуйста, выберите другую модель (например, Gemini Quality или Gemini Balanced).';
          } else if (errorMessage.includes('models/') || errorStr.includes('models/')) {
            const modelMatch = errorMessage.match(/models\/([^\s"]+)/) || errorStr.match(/models\/([^\s"]+)/);
            if (modelMatch) {
              const modelName = modelMatch[1];
              userFriendlyError = `Модель "${modelName}" не найдена или недоступна. Пожалуйста, выберите другую модель.`;
            } else {
              userFriendlyError = 'Выбранная модель недоступна. Пожалуйста, выберите другую модель.';
            }
          } else {
            userFriendlyError = 'Модель не найдена или недоступна. Пожалуйста, выберите другую модель.';
          }
        } else if (errorCode === 401 || errorStr.includes('UNAUTHENTICATED')) {
          userFriendlyError = 'Ошибка аутентификации. Пожалуйста, обновите страницу и попробуйте снова.';
        } else if (errorCode === 403 || errorStr.includes('PERMISSION_DENIED')) {
          userFriendlyError = 'Недостаточно прав доступа. Пожалуйста, проверьте настройки доступа.';
        } else if (errorCode === 429 || errorStr.includes('RESOURCE_EXHAUSTED')) {
          userFriendlyError = 'Превышен лимит запросов. Пожалуйста, подождите немного и попробуйте снова.';
        } else if (errorCode === 500 || errorStr.includes('INTERNAL_ERROR')) {
          userFriendlyError = 'Внутренняя ошибка сервера. Мы уже работаем над исправлением. Пожалуйста, попробуйте позже.';
        } else if (errorStr.includes('timeout') || errorStr.includes('TIMEOUT')) {
          userFriendlyError = 'Превышено время ожидания ответа. Пожалуйста, попробуйте снова.';
        }
      } catch (parseErr) {
        // If parsing fails, use original error
        console.warn('[GenAI Stream] Failed to parse error message', parseErr);
      }
      
      // Also prune trailing empty assistant placeholder so skeleton doesn't stick
      const pruned = [...st.messages];
      if (pruned.length > 0 && pruned[pruned.length - 1].role === 'assistant' && (!pruned[pruned.length - 1].content || pruned[pruned.length - 1].content.trim().length === 0)) {
        pruned.pop();
      }
      
      // Create error message as assistant message if there's no assistant message with content
      const lastMessage = pruned.length > 0 ? pruned[pruned.length - 1] : null;
      const needsErrorMessage = !lastMessage || lastMessage.role !== 'assistant' || !lastMessage.content || lastMessage.content.trim().length === 0;
      
      const errorMessageId = needsErrorMessage ? (Date.now().toString() + '-error') : null;
      
      if (needsErrorMessage && errorMessageId) {
        pruned.push({
          id: errorMessageId,
          role: 'assistant',
          content: '', // Empty content, error will be shown via isError flag
        });
      }
      
      // Create error interaction to persist it across hydration
      // This ensures the error message survives hydrateInteractions
      const errorInteraction: LocalInteraction | null = errorMessageId ? {
        id: errorMessageId,
        projectId: st.projectId || 'local-unfold',
        kind: 'assistant',
        subkind: 'error',
        content: userFriendlyError, // Store error message in content
        createdAt: new Date().toISOString(),
        originalMessageId: errorMessageId,
      } : null;
      
      // Add error interaction to interactions list if it doesn't exist
      const currentInteractions = [...st.interactions];
      if (errorInteraction && !currentInteractions.find((it) => it.id === errorInteraction.id)) {
        currentInteractions.push(errorInteraction);
      }
      
      set({ 
        streaming: false, 
        currentStreamId: undefined, 
        abortController: undefined,
        messages: pruned, 
        error: userFriendlyError, 
        interactions: currentInteractions, // Preserve error interaction
        pendingAssistantMeta: undefined, 
        pendingAssistantOptions: get().pendingAssistantOptions, 
        pendingAssistantPrompt: get().pendingAssistantPrompt, 
        pendingAssistantTools: get().pendingAssistantTools 
      });
    } catch (e) {
      console.error('[GenAI Stream] Error in onError handler', e);
      const st = get();
      const pruned = [...st.messages];
      if (pruned.length > 0 && pruned[pruned.length - 1].role === 'assistant' && (!pruned[pruned.length - 1].content || pruned[pruned.length - 1].content.trim().length === 0)) {
        pruned.pop();
      }
      set({ streaming: false, currentStreamId: undefined, abortController: undefined, messages: pruned, error: err || 'Stream error', pendingAssistantMeta: undefined, pendingAssistantOptions: get().pendingAssistantOptions, pendingAssistantPrompt: get().pendingAssistantPrompt, pendingAssistantTools: get().pendingAssistantTools });
    }
  },
  onToolCall: (calls) => set({ toolCalls: calls || [] }),
  onToolRetry: (data) => {
    console.log('[GenAIChatStore] Tool retry', data);
    set((s) => ({
      toolRetries: [...s.toolRetries, { ...data, timestamp: Date.now() }],
    } as Partial<GenaiChatState>));
    // Add notification message to chat
    const notificationMsg: ChatMessage = {
      id: `tool-retry-${Date.now()}`,
      role: 'system',
      content: `⚠️ Обнаружена ошибка при выполнении инструмента "${data.toolName}". Попытка исправления ${data.attempt}/3...`,
      timestamp: Date.now(),
    };
    set((s) => ({
      messages: [...s.messages, notificationMsg],
    } as Partial<GenaiChatState>));
  },
  onToolCorrection: (data) => {
    console.log('[GenAIChatStore] Tool correction', data);
    set((s) => ({
      toolCorrections: [...s.toolCorrections, { ...data, timestamp: Date.now() }],
    } as Partial<GenaiChatState>));
    // Update notification message
    set((s) => {
      const msgs = [...s.messages];
      const lastRetryMsg = msgs.findLast((m) => m.id?.startsWith('tool-retry-'));
      if (lastRetryMsg) {
        const idx = msgs.indexOf(lastRetryMsg);
        msgs[idx] = {
          ...lastRetryMsg,
          content: `✅ Ошибка исправлена. Повторная попытка выполнения инструмента "${data.toolName}"...`,
        };
      }
      return { messages: msgs } as Partial<GenaiChatState>;
    });
  },
  onToolResult: (calls) => {
    const toolCalls = calls || [];
    set((s) => {
      const projectId = s.projectId;
      if (!projectId || projectId === 'local-unfold') {
        return { toolResults: [...s.toolResults, ...toolCalls] } as any;
      }
      
      // Process tool results and create interactions for graph creation
      const newInteractions: LocalInteraction[] = [];
      const graphSummaries: string[] = [];
      const now = Date.now().toString();
      
      for (const call of toolCalls) {
        if (!call || typeof call !== 'object') continue;
        
        const toolName = call.name || '';
        const toolArgs = call.arguments || '';
        
        // Handle create_graph tool result
        if (toolName === 'create_graph') {
          try {
            let parsedResult: any = null;
            if (typeof toolArgs === 'string') {
              try {
                parsedResult = JSON.parse(toolArgs);
              } catch (parseErr) {
                console.warn('[onToolResult] Failed to parse create_graph result JSON', { toolArgs: toolArgs.substring(0, 200), error: parseErr });
                continue;
              }
            } else if (typeof toolArgs === 'object' && toolArgs !== null) {
              parsedResult = toolArgs;
            }
            
            // Check if result contains graph data (interactionId indicates graph was created)
            // Tool result format: { interactionId, chartTitle, chartType, table, sources, status }
            if (parsedResult && typeof parsedResult === 'object') {
              const interactionId = parsedResult.interactionId;
              const chartTitle = parsedResult.chartTitle;
              const chartType = parsedResult.chartType;
              const table = parsedResult.table;
              const sources = parsedResult.sources;
              
              if (interactionId && chartTitle && Array.isArray(table) && Array.isArray(sources)) {
                // Graph was created - create local interaction immediately
                const allowedTypes = ['line', 'bar', 'area', 'stackedBar', 'stackedArea', 'composed'];
                const validChartType = typeof chartType === 'string' && allowedTypes.includes(chartType) ? chartType : 'line';
                
                const graphInteraction: LocalInteraction = {
                  id: interactionId, // Use server ID from tool result
                  projectId,
                  kind: 'assistant',
                  subkind: 'graph',
                  content: JSON.stringify({ chartTitle, chartType: validChartType, table, sources }),
                  contentSanitized: JSON.stringify({ chartTitle, chartType: validChartType, table, sources }),
                  graphData: {
                    chartTitle,
                    chartType: validChartType,
                    table,
                    sources,
                  },
                  createdAt: new Date().toISOString(),
                  author: { participantId: 'tool:create_graph', kind: 'tool', displayName: 'Graph Tool' },
                };
                
                // Check if interaction already exists (by ID or by checking if we already have this graph)
                const exists = s.interactions.find((it) => 
                  it.id === interactionId || 
                  (it.subkind === 'graph' && it.graphData?.chartTitle === chartTitle && 
                   JSON.stringify(it.graphData?.table) === JSON.stringify(table))
                );
                
                if (!exists) {
                  newInteractions.push(graphInteraction);
                  const preferredLang = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN').toLowerCase();
                  const summary =
                    preferredLang === 'ru'
                      ? `Готово: построил график «${chartTitle}». Посмотри визуализацию ниже.`
                      : `Done: built the chart “${chartTitle}”. See the visualization below.`;
                  graphSummaries.push(summary);
                  console.log('[onToolResult] Created local graph interaction from tool result', { 
                    interactionId, 
                    chartTitle, 
                    tableRows: table.length,
                    sourcesCount: sources.length,
                    graphData: graphInteraction.graphData,
                    subkind: graphInteraction.subkind,
                    kind: graphInteraction.kind
                  });
                } else {
                  console.log('[onToolResult] Graph interaction already exists, skipping', { interactionId });
                }
              } else {
                console.warn('[onToolResult] create_graph result missing required fields', { 
                  hasInteractionId: !!interactionId,
                  hasChartTitle: !!chartTitle,
                  hasTable: Array.isArray(table),
                  hasSources: Array.isArray(sources),
                  parsedResult 
                });
              }
            }
          } catch (e) {
            console.warn('[onToolResult] Failed to process create_graph result', { error: e, toolArgs: typeof toolArgs === 'string' ? toolArgs.substring(0, 200) : toolArgs });
          }
        }
      }
      
      let updatedInteractions: LocalInteraction[] | undefined;
      if (newInteractions.length > 0) {
        updatedInteractions = [...s.interactions];
        for (const newIt of newInteractions) {
          if (newIt.subkind === 'graph' && (newIt as any).author?.kind === 'tool') {
            const exists = updatedInteractions.find((it) => 
              it.id === newIt.id || 
              (it.subkind === 'graph' && it.graphData?.chartTitle === newIt.graphData?.chartTitle)
            );
            if (!exists) {
              updatedInteractions.push(newIt);
              console.log('[onToolResult] Added graph interaction to end of list', { interactionId: newIt.id, chartTitle: newIt.graphData?.chartTitle });
            }
          } else {
            const insertIndex = updatedInteractions.findIndex((it) => {
              const itTime = new Date(it.createdAt).getTime();
              const newTime = new Date(newIt.createdAt).getTime();
              return itTime > newTime;
            });
            
            if (insertIndex >= 0) {
              updatedInteractions.splice(insertIndex, 0, newIt);
            } else {
              updatedInteractions.push(newIt);
            }
          }
        }
      }

      let updatedMessages: ChatMessage[] | undefined;
      if (graphSummaries.length > 0) {
        const summary = graphSummaries[graphSummaries.length - 1];
        const messages = [...s.messages];
        let lastUserIndex = -1;
        for (let i = messages.length - 1; i >= 0; i--) {
          if (messages[i].role === 'user') {
            lastUserIndex = i;
            break;
          }
        }
        let assistantIndex = -1;
        for (let i = messages.length - 1; i > lastUserIndex; i--) {
          if (messages[i].role === 'assistant') {
            assistantIndex = i;
            break;
          }
        }
        if (assistantIndex >= 0) {
          const existing = messages[assistantIndex].content?.trim();
          if (!existing) {
            messages[assistantIndex] = { ...messages[assistantIndex], content: summary };
          } else {
            const id = Date.now().toString() + '-graph-summary';
            messages.splice(assistantIndex + 1, 0, { id, role: 'assistant', content: summary });
          }
        } else {
          const id = Date.now().toString() + '-graph-summary';
          messages.push({ id, role: 'assistant', content: summary });
        }
        updatedMessages = messages;
      }
      
      return { 
        toolResults: [...s.toolResults, ...toolCalls],
        interactions: updatedInteractions || s.interactions,
        messages: updatedMessages || s.messages,
      } as any;
    });
  },
  onDoneMeta: (meta) => set((s) => {
    // Replace (not merge) queries/sources for current stream to avoid accumulation across messages
    const incomingQueries = Array.isArray(meta.searchQueries) ? meta.searchQueries.filter(Boolean) : [];
    const incomingSources = Array.isArray(meta.sources) ? meta.sources.filter(Boolean) : [];
    
    // Merge attributions: keep existing + incoming attributions + sources as URLs
    const allAttributions = [...(s.attributions || []), ...((meta.attributions || []) as any[])];
    const seen = new Set(allAttributions.map((a) => a.url).filter(Boolean));
    for (const url of incomingSources) {
      if (url && !seen.has(url)) {
        allAttributions.push({ url });
        seen.add(url);
      }
    }
    
    // Limit attributions size to prevent memory leaks (keep last 500 entries)
    const MAX_ATTRIBUTIONS = 500;
    const limitedAttributions = allAttributions.length > MAX_ATTRIBUTIONS 
      ? allAttributions.slice(-MAX_ATTRIBUTIONS)
      : allAttributions;
    
    return { 
      usage: meta.usage, 
      attributions: limitedAttributions, 
      searchQueries: incomingQueries, 
      sources: incomingSources 
    } as any;
  }),
  setEvaluation: (messageId, ev) => set((s) => {
    const next = { ...s.evaluations, [messageId]: ev };
    // Only update evaluation map; creation/persistence of eval.bias interactions
    // is handled by the caller (e.g., unified evaluateBias in DiscoveryPage).
    return { evaluations: next } as any;
  }),
  setPreStanceResult: (messageId, latencyMs, extract) => set((s) => {
    const nextLatency = { ...(s.preStanceLatency || {}) } as Record<string, number>;
    const nextExtract = { ...(s.preStanceExtract || {}) } as Record<string, { user_request: string }>;
    nextLatency[messageId] = Math.max(0, Math.floor(Number(latencyMs) || 0));
    if (extract && typeof extract.user_request === 'string') {
      nextExtract[messageId] = { user_request: extract.user_request };
    }
    try {
      console.log('[PreStance] state', { messageId, latencyMs: nextLatency[messageId], user_request: nextExtract[messageId]?.user_request });
    } catch {}
    // Also persist as a system interaction for traceability (best-effort)
    try {
      const projectId = s.projectId;
      const map = (s as any).persistedByLocalId || {};
      const serverReplyTo = map[messageId];
      const payload: any = { kind: 'system', subkind: 'eval.user_request', content: JSON.stringify({ latencyMs: nextLatency[messageId], ...(nextExtract[messageId] || {}) }) };
      if (serverReplyTo) {
        payload.replyTo = serverReplyTo;
        payload.relations = [{ type: 'evaluates', targetId: serverReplyTo }];
      }
      api.createProjectInteraction(projectId, payload).catch(() => {});
    } catch {}
    return { preStanceLatency: nextLatency, preStanceExtract: nextExtract } as any;
  }),
}));


