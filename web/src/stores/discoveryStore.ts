import { create } from 'zustand';
import { api } from '../lib/api';
import type { DataSource } from '../types';

const IS_DEV = import.meta.env.DEV;

// --- WebSocket connection management (module scope to persist across store re-creations) ---
let wsRef: WebSocket | null = null;
let reconnectAttempts = 0;
let reconnectTimer: number | null = null;
let isManuallyClosed = false;
let isConnecting = false; // Prevent multiple simultaneous connection attempts
let listenersAttached = false; // Track if event listeners are already attached
let projectsStoreUnsubscribe: (() => void) | null = null; // Store unsubscribe function to prevent memory leaks
let windowEventListeners: { visibilitychange: () => void; online: () => void } | null = null; // Store listeners for cleanup
let lastSubscribedProjectId: string | undefined = undefined; // Track last subscribed projectId to restore on reconnect
// WebSocket broker URL: VITE_WS_BASE_URL or derived from VITE_API_BASE_URL

type SubscriptionWaiter = {
  timeoutId: number;
  resolve: () => void;
  reject: (err: Error) => void;
};

type SubscriptionOptions = {
  timeoutMs?: number;
  force?: boolean;
};

const subscriptionWaiters = new Map<string, SubscriptionWaiter[]>();
const pendingSubscriptions = new Map<string, Promise<void>>();

function addSubscriptionWaiter(projectId: string, waiter: SubscriptionWaiter) {
  const waiters = subscriptionWaiters.get(projectId) || [];
  waiters.push(waiter);
  subscriptionWaiters.set(projectId, waiters);
}

function removeSubscriptionWaiter(projectId: string, waiter: SubscriptionWaiter) {
  const waiters = subscriptionWaiters.get(projectId);
  if (!waiters) return;
  const next = waiters.filter((w) => w !== waiter);
  if (next.length === 0) {
    subscriptionWaiters.delete(projectId);
  } else {
    subscriptionWaiters.set(projectId, next);
  }
}

function resolveSubscriptionWaiters(projectId: string) {
  const waiters = subscriptionWaiters.get(projectId);
  if (!waiters) return;
  subscriptionWaiters.delete(projectId);
  waiters.forEach((waiter) => {
    window.clearTimeout(waiter.timeoutId);
    try {
      waiter.resolve();
    } catch (err) {
      console.error('[WS] Subscription waiter resolve error', err);
    }
  });
}

function rejectAllSubscriptionWaiters(message: string) {
  const reason = new Error(message);
  subscriptionWaiters.forEach((waiters, projectId) => {
    subscriptionWaiters.delete(projectId);
    waiters.forEach((waiter) => {
      window.clearTimeout(waiter.timeoutId);
      try {
        waiter.reject(reason);
      } catch (err) {
        console.error('[WS] Subscription waiter reject error', err);
      }
    });
  });
}

async function waitForWsOpen(timeoutMs = 5000): Promise<void> {
  const start = Date.now();
  while (true) {
    if (wsRef && wsRef.readyState === WebSocket.OPEN) {
      return;
    }
    if (Date.now() - start >= timeoutMs) {
      throw new Error('WebSocket connection timeout');
    }
    await new Promise((resolve) => window.setTimeout(resolve, 50));
  }
}

async function requestProjectSubscription(projectId: string, options?: SubscriptionOptions): Promise<void> {
  const pid = projectId?.trim();
  if (!pid) return;
  const timeoutMs = options?.timeoutMs ?? 6000;
  const force = !!options?.force;

  if (!force && lastSubscribedProjectId === pid) {
    return;
  }

  const existing = pendingSubscriptions.get(pid);
  if (!force && existing) {
    return existing;
  }

  const subscriptionPromise = (async () => {
    await waitForWsOpen(Math.max(timeoutMs, 4000));

    if (!force && lastSubscribedProjectId === pid) {
      return;
    }

    let attempt = 0;
    const maxAttempts = force ? 1 : 2;
    let lastError: Error | null = null;

    while (attempt < maxAttempts) {
      try {
        await new Promise<void>((resolve, reject) => {
          const waiter: SubscriptionWaiter = {
            timeoutId: 0,
            resolve: () => {},
            reject: () => {},
          };

          waiter.resolve = () => {
            window.clearTimeout(waiter.timeoutId);
            resolve();
          };
          waiter.reject = (err: Error) => {
            window.clearTimeout(waiter.timeoutId);
            reject(err);
          };
          waiter.timeoutId = window.setTimeout(() => {
            removeSubscriptionWaiter(pid, waiter);
            reject(new Error(`Timeout waiting for ws.subscribed (${pid})`));
          }, timeoutMs);

          addSubscriptionWaiter(pid, waiter);

          try {
            const subscribeMsg = { type: 'subscribe', projectId: pid, force: attempt > 0 || force };
            const msgStr = JSON.stringify(subscribeMsg);
            wsRef?.send(msgStr);
            console.log('[WS] Requested subscription', { projectId: pid, timeoutMs, force: attempt > 0 || force, message: msgStr });
          } catch (err) {
            removeSubscriptionWaiter(pid, waiter);
            reject(err instanceof Error ? err : new Error('Failed to send subscribe request'));
          }
        });
        return;
      } catch (err) {
        lastError = err instanceof Error ? err : new Error(String(err));
        attempt += 1;
        if (attempt >= maxAttempts) {
          throw lastError;
        }
        console.warn('[WS] Retrying subscription after timeout', { projectId: pid, attempt });
      }
    }
  })();

  pendingSubscriptions.set(pid, subscriptionPromise);
  try {
    await subscriptionPromise;
  } finally {
    const current = pendingSubscriptions.get(pid);
    if (current === subscriptionPromise) {
      pendingSubscriptions.delete(pid);
    }
  }
}

function clearReconnectTimer() {
  if (reconnectTimer !== null) {
    window.clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
}

function computeBackoffDelay(attempt: number): number {
  const base = 1000; // 1s base
  const max = 30000; // cap at 30s
  const exp = Math.min(max, base * Math.pow(2, Math.max(0, attempt - 1)));
  const jitter = exp * (0.2 * (Math.random() - 0.5)); // +/-10%
  return Math.max(1000, Math.floor(exp + jitter));
}

// Handle Research mode WebSocket events
function handleResearchEvent(
  msg: any,
  st: any,
  eventPid: string | undefined,
  eventStreamId: string | undefined,
  currentPid: string | undefined,
  isActiveStream: boolean
) {
  const researchStage = msg.type.replace('genai.stream.research.', '');
  console.log('[DiscoveryStore] Received research event', { 
    stage: researchStage,
    projectId: eventPid, 
    streamId: eventStreamId,
    data: msg.data
  });

  // Filter by project/stream if needed
  if (eventPid && currentPid && eventPid !== currentPid && !isActiveStream) {
    return; // Filter out mismatched projects
  }

  // Import genaiChatStore to update interactions
  import('./genaiChatStore').then(({ useGenaiChatStore }) => {
    // Get fresh state each time to avoid stale closures
    const getCurrentState = () => useGenaiChatStore.getState();
    const currentState = getCurrentState();
    const interactions = currentState.interactions || [];
    const progressInteractionId = eventStreamId ? `research-${eventStreamId}-progress` : null;
    
    // Find existing progress interaction
    let progressInteraction = progressInteractionId 
      ? interactions.find((it: any) => it.id === progressInteractionId)
      : null;

    switch (researchStage) {
      case 'start':
        // Create new research.progress interaction (only if doesn't exist to prevent duplicates)
        if (progressInteractionId && eventPid) {
          if (!progressInteraction) {
            const newProgress: any = {
              id: progressInteractionId,
              projectId: eventPid,
              kind: 'research',
              subkind: 'research.progress',
              researchQuery: msg.data?.query || '',
              researchProgress: {
                stage: 'planning',
                status: 'in_progress',
              },
              createdAt: new Date().toISOString(),
              replyTo: msg.data?.userInteractionId,
            };
            // Get fresh state to avoid race conditions
            const freshState = getCurrentState();
            const newInteractions = [...freshState.interactions, newProgress];
            useGenaiChatStore.setState({ 
              interactions: newInteractions,
              _lastUpdate: Date.now() // Force re-render
            } as any);
          } else {
            // Update existing progress interaction if start event received again
            const updated = {
              ...progressInteraction,
              researchQuery: msg.data?.query || progressInteraction.researchQuery,
              researchProgress: {
                ...progressInteraction.researchProgress,
                stage: 'planning',
                status: 'in_progress',
              },
            };
            const freshState = getCurrentState();
            useGenaiChatStore.setState({
              interactions: freshState.interactions.map((it: any) => 
                it.id === progressInteractionId ? updated : it
              )
            } as any);
          }
        }
        break;

      case 'planning':
        // Update progress interaction (create if doesn't exist)
        if (progressInteractionId && eventPid) {
          if (!progressInteraction) {
            // Create progress interaction if it doesn't exist (handles race condition)
            const newProgress: any = {
              id: progressInteractionId,
              projectId: eventPid,
              kind: 'research',
              subkind: 'research.progress',
              researchQuery: msg.data?.query || '',
              researchProgress: {
                stage: 'planning',
                status: msg.data?.status || 'in_progress',
                subQueries: msg.data?.subQueries || [],
              },
              createdAt: new Date().toISOString(),
              replyTo: msg.data?.userInteractionId,
            };
            const freshState = getCurrentState();
            useGenaiChatStore.setState({ interactions: [...freshState.interactions, newProgress] } as any);
          } else if (msg.data?.status) {
            const updated = {
              ...progressInteraction,
              researchProgress: {
                stage: 'planning',
                status: msg.data.status,
                subQueries: msg.data.subQueries || progressInteraction.researchProgress?.subQueries,
              },
            };
            const freshState = getCurrentState();
            const updatedInteractions = freshState.interactions.map((it: any) => 
              it.id === progressInteractionId ? updated : it
            );
            useGenaiChatStore.setState({
              interactions: updatedInteractions
            } as any);
            // Force re-render by updating a timestamp
            useGenaiChatStore.setState({ _lastUpdate: Date.now() } as any);
          }
        }
        break;

      case 'searching':
        // Update progress interaction (create if doesn't exist)
        if (progressInteractionId && eventPid) {
          if (!progressInteraction) {
            // Create progress interaction if it doesn't exist (handles race condition)
            const sourcesCount = Array.isArray(msg.data?.sources) 
              ? msg.data.sources.length 
              : (typeof msg.data?.sources === 'number' ? msg.data.sources : 0);
            const newProgress: any = {
              id: progressInteractionId,
              projectId: eventPid,
              kind: 'research',
              subkind: 'research.progress',
              researchQuery: msg.data?.query || '',
              researchProgress: {
                stage: 'searching',
                status: msg.data?.status || 'in_progress',
                sourcesCount,
              },
              createdAt: new Date().toISOString(),
              replyTo: msg.data?.userInteractionId,
            };
            const freshState = getCurrentState();
            useGenaiChatStore.setState({ interactions: [...freshState.interactions, newProgress] } as any);
          } else if (msg.data?.status) {
            const sourcesCount = Array.isArray(msg.data.sources) 
              ? msg.data.sources.length 
              : (typeof msg.data.sources === 'number' ? msg.data.sources : progressInteraction.researchProgress?.sourcesCount);
            const updated = {
              ...progressInteraction,
              researchProgress: {
                ...progressInteraction.researchProgress,
                stage: 'searching',
                status: msg.data.status,
                sourcesCount,
              },
            };
            const freshState = getCurrentState();
            const updatedInteractions = freshState.interactions.map((it: any) => 
              it.id === progressInteractionId ? updated : it
            );
            useGenaiChatStore.setState({
              interactions: updatedInteractions
            } as any);
            // Force re-render by updating a timestamp
            useGenaiChatStore.setState({ _lastUpdate: Date.now() } as any);
          }
        }
        break;

      case 'generating':
        // Update progress interaction (create if doesn't exist)
        if (progressInteractionId && eventPid) {
          if (!progressInteraction) {
            // Create progress interaction if it doesn't exist (handles race condition)
            const newProgress: any = {
              id: progressInteractionId,
              projectId: eventPid,
              kind: 'research',
              subkind: 'research.progress',
              researchQuery: msg.data?.query || '',
              researchProgress: {
                stage: 'generating',
                status: msg.data?.status || 'in_progress',
              },
              createdAt: new Date().toISOString(),
              replyTo: msg.data?.userInteractionId,
            };
            const freshState = getCurrentState();
            useGenaiChatStore.setState({ interactions: [...freshState.interactions, newProgress] } as any);
          } else if (msg.data?.status) {
            const updated = {
              ...progressInteraction,
              researchProgress: {
                ...progressInteraction.researchProgress,
                stage: 'generating',
                status: msg.data.status,
              },
            };
            const freshState = getCurrentState();
            const updatedInteractions = freshState.interactions.map((it: any) => 
              it.id === progressInteractionId ? updated : it
            );
            useGenaiChatStore.setState({
              interactions: updatedInteractions
            } as any);
            // Force re-render by updating a timestamp
            useGenaiChatStore.setState({ _lastUpdate: Date.now() } as any);
          }
        }
        break;

      case 'balancing':
        // Update progress interaction (create if doesn't exist)
        if (progressInteractionId && eventPid) {
          if (!progressInteraction) {
            // Create progress interaction if it doesn't exist (handles race condition)
            const newProgress: any = {
              id: progressInteractionId,
              projectId: eventPid,
              kind: 'research',
              subkind: 'research.progress',
              researchQuery: msg.data?.query || '',
              researchProgress: {
                stage: 'balancing',
                status: msg.data?.status || 'in_progress',
              },
              createdAt: new Date().toISOString(),
              replyTo: msg.data?.userInteractionId,
            };
            const freshState = getCurrentState();
            useGenaiChatStore.setState({ interactions: [...freshState.interactions, newProgress] } as any);
          } else if (msg.data?.status) {
            const updated = {
              ...progressInteraction,
              researchProgress: {
                ...progressInteraction.researchProgress,
                stage: 'balancing',
                status: msg.data.status,
              },
            };
            const freshState = getCurrentState();
            const updatedInteractions = freshState.interactions.map((it: any) => 
              it.id === progressInteractionId ? updated : it
            );
            useGenaiChatStore.setState({
              interactions: updatedInteractions
            } as any);
            // Force re-render by updating a timestamp
            useGenaiChatStore.setState({ _lastUpdate: Date.now() } as any);
          }
        }
        break;

      case 'synthesizing':
        // Update progress interaction (create if doesn't exist)
        if (progressInteractionId && eventPid) {
          if (!progressInteraction) {
            // Create progress interaction if it doesn't exist (handles race condition)
            const newProgress: any = {
              id: progressInteractionId,
              projectId: eventPid,
              kind: 'research',
              subkind: 'research.progress',
              researchQuery: msg.data?.query || '',
              researchProgress: {
                stage: 'synthesizing',
                status: msg.data?.status || 'in_progress',
              },
              createdAt: new Date().toISOString(),
              replyTo: msg.data?.userInteractionId,
            };
            const freshState = getCurrentState();
            useGenaiChatStore.setState({ interactions: [...freshState.interactions, newProgress] } as any);
          } else if (msg.data?.status) {
            const updated = {
              ...progressInteraction,
              researchProgress: {
                ...progressInteraction.researchProgress,
                stage: 'synthesizing',
                status: msg.data.status,
              },
            };
            const freshState = getCurrentState();
            const updatedInteractions = freshState.interactions.map((it: any) => 
              it.id === progressInteractionId ? updated : it
            );
            useGenaiChatStore.setState({
              interactions: updatedInteractions
            } as any);
            // Force re-render by updating a timestamp
            useGenaiChatStore.setState({ _lastUpdate: Date.now() } as any);
          }
        }
        break;

      case 'done':
        // Create research.result interaction and remove progress
        if (eventStreamId && eventPid) {
          const resultId = `research-${eventStreamId}-result`;
          const sources = Array.isArray(msg.data?.sources) 
            ? msg.data.sources 
            : [];
          const researchSources = sources.map((src: any, idx: number) => ({
            title: src?.title || `Source ${idx + 1}`,
            url: src?.url || '',
            score: src?.score,
          }));
          
          const resultInteraction: any = {
            id: resultId,
            projectId: eventPid,
            kind: 'research',
            subkind: 'research.result',
            content: msg.data?.synthesis || msg.data?.report || '', // Use synthesis as final content
            researchQuery: msg.data?.query || '',
            subQueries: msg.data?.subQueries || [],
            researchSources,
            model: msg.data?.model,
            usage: msg.data?.usage,
            // Store all three reports for potential display
            researchReport: msg.data?.report || '',
            researchCounterPosition: msg.data?.counterPosition || '',
            researchSynthesis: msg.data?.synthesis || '',
            createdAt: new Date().toISOString(),
            replyTo: msg.data?.userInteractionId,
          };
          
          // Remove progress interaction and add result
          // Get fresh state to avoid race conditions
          const freshState = getCurrentState();
          const newInteractions = freshState.interactions
            .filter((it: any) => it.id !== progressInteractionId)
            .concat([resultInteraction]);
          useGenaiChatStore.setState({
            interactions: newInteractions,
            _lastUpdate: Date.now() // Force re-render
          } as any);
          
          // CRITICAL: Persist research.result to server so it survives page reload
          import('../lib/api').then(({ api }) => {
            api.createProjectInteraction(eventPid, {
              kind: 'research',
              subkind: 'research.result',
              content: msg.data?.synthesis || msg.data?.report || '', // Use synthesis as final content
              researchQuery: msg.data?.query || '',
              subQueries: msg.data?.subQueries || [],
              researchSources,
              model: msg.data?.model,
              usage: msg.data?.usage,
              // Store all three reports for potential display
              researchReport: msg.data?.report || '',
              researchCounterPosition: msg.data?.counterPosition || '',
              researchSynthesis: msg.data?.synthesis || '',
              replyTo: msg.data?.userInteractionId,
            } as any).then((res) => {
              // Update local interaction with server ID
              const updatedState = getCurrentState();
              useGenaiChatStore.setState({
                interactions: updatedState.interactions.map((it: any) =>
                  it.id === resultId ? { ...it, id: res.id } : it
                )
              } as any);
              
              // Trigger bias evaluation for research result (same as for assistant messages)
              (async () => {
                try {
                  const { evaluateBias } = await import('../lib/biasEvaluation');
                  const { useGenAISettings } = await import('../stores/genaiSettingsStore');
                  
                  const settingsStateEval = useGenAISettings.getState();
                  const evalModel = (settingsStateEval.stanceModel && settingsStateEval.stanceModel.trim()) 
                    ? settingsStateEval.stanceModel 
                    : 'gemini_fast';
                  
                  // Evaluate synthesis if available, otherwise report
                  const textToEvaluate = (msg.data?.synthesis || msg.data?.report || '').trim();
                  if (!textToEvaluate) return;
                  
                  const preferredLang = (typeof window !== 'undefined' 
                    ? (window.localStorage.getItem('preferredLanguage') || 'EN') 
                    : 'EN').toLowerCase();
                  
                  // Skip evaluation for agent responses and scenario messages (they are system responses, not conversational content)
                  try {
                    const { useGenaiChatStore: genaiStore } = await import('./genaiChatStore');
                    const allInts = genaiStore.getState().interactions;
                    const interaction = allInts.find((it) => it.id === res.id);
                    const isAgentResponse = interaction?.subkind?.startsWith('agent.') || !!(interaction as any)?.agentProfile;
                    const isScenarioMessage = interaction?.subkind?.startsWith('scenarios.');
                    if (isAgentResponse || isScenarioMessage) {
                      return;
                    }
                  } catch {}
                  
                  // Find previous USER message as context (research query)
                  let prevUserText: string | null = null;
                  try {
                    const { useGenaiChatStore: genaiStore } = await import('./genaiChatStore');
                    const allInts = genaiStore.getState().interactions;
                    const idx = allInts.findIndex((it) => it.id === res.id);
                    if (idx > 0) {
                      for (let i = idx - 1; i >= 0; i--) {
                        if (allInts[i].kind === 'user' && allInts[i].content && (allInts[i].content as string).trim()) {
                          prevUserText = allInts[i].content as string;
                          break;
                        }
                      }
                    }
                    // Fallback to research query if no user message found
                    if (!prevUserText && msg.data?.query) {
                      prevUserText = msg.data.query;
                    }
                  } catch {}
                  
                  // Call unified evaluation function
                  await evaluateBias({
                    messageId: res.id,
                    messageRole: 'assistant',
                    textToEvaluate,
                    contextText: prevUserText || undefined,
                    evalModel,
                    preferredLang,
                  });
                } catch (e) {
                  console.warn('[ResearchEvent] Bias evaluation failed', e);
                }
              })().catch((err) => {
                console.warn('[ResearchEvent] Bias evaluation error', err);
              });
            }).catch((err) => {
              console.error('[ResearchEvent] Failed to persist research.result', err);
            });
          }).catch((err) => {
            console.error('[ResearchEvent] Failed to import api', err);
          });
          
          // CRITICAL: Stop streaming to remove "thinking" placeholder
          // Don't call onDone() for research - it would create empty assistant stub
          // Only clear streaming state
          st.onDoneMeta({ 
            usage: msg.data?.usage, 
            attributions: [], // Empty - research sources are in the interaction itself
            searchQueries: msg.data?.searchQueries,
            sources: [],
          });
          // Clear streaming state without creating assistant interaction
          const currentState = getCurrentState();
          if (currentState.streaming) {
            useGenaiChatStore.setState({ 
              streaming: false, 
              currentStreamId: undefined,
              pendingAssistantMeta: undefined,
              pendingAssistantOptions: undefined,
              pendingAssistantPrompt: undefined,
              pendingAssistantTools: undefined,
            } as any);
          }
        }
        break;

      case 'error':
        // Update progress interaction with error
        if (progressInteraction) {
          const updated = {
            ...progressInteraction,
            researchProgress: {
              ...progressInteraction.researchProgress,
              status: 'error' as any,
            },
            content: `Research error: ${msg.data?.error || 'Unknown error'}`,
          };
          // Get fresh state to avoid race conditions
          const freshState = getCurrentState();
          useGenaiChatStore.setState({
            interactions: freshState.interactions.map((it: any) => 
              it.id === progressInteractionId ? updated : it
            )
          } as any);
        }
        break;
    }
  }).catch((err) => {
    console.error('[DiscoveryStore] Failed to handle research event:', err);
  });
}

// Cleanup function to remove all event listeners and subscriptions (for testing/debugging)
export function cleanupDiscoveryStore() {
  // Clear reconnect timer
  clearReconnectTimer();
  
  // Unsubscribe from projects store
  if (projectsStoreUnsubscribe) {
    try {
      projectsStoreUnsubscribe();
    } catch {}
    projectsStoreUnsubscribe = null;
  }
  
  // Remove window event listeners
  if (windowEventListeners) {
    try {
      window.removeEventListener('visibilitychange', windowEventListeners.visibilitychange);
      window.removeEventListener('online', windowEventListeners.online);
    } catch {}
    windowEventListeners = null;
    listenersAttached = false;
  }
  
  // Close WebSocket if open
  if (wsRef) {
    try {
      wsRef.onclose = null;
      wsRef.onerror = null;
      wsRef.onmessage = null;
      wsRef.close();
    } catch {}
    wsRef = null;
  }
  
  // Reset subscription tracking on cleanup
  lastSubscribedProjectId = undefined;
  rejectAllSubscriptionWaiters('Discovery store cleanup');
  
  isManuallyClosed = true;
  isConnecting = false;
  reconnectAttempts = 0;
}

interface DiscoveryState {
  sources: DataSource[];
  loading: boolean;
  error: string | null;
  correlationId: string | null;
  answer: string | null;
  interactions: Array<{ id: string; kind: string; content: string; createdAt: string }>;
  pendingSources: Array<{ id: string; kind: 'web' | 'telegram' | 'rss'; status: string }>;
  lastDocEvent?: any;
  lastAnalysisStatus?: { projectId: string; kind: string; status: 'started'|'done'|'failed'; elapsedMs?: number; count?: number; error?: string; ts: number } | undefined;
  connectWs: (tenantId: string) => Promise<void>;
  
  // Actions
  executeDiscovery: (data: {
    projectId: string;
    queryText: string;
    language?: string;
    sources: { web: string[]; telegram: string[]; rss?: string[] };
  }) => Promise<{ correlationId: string }>;
  sendChatMessage: (projectId: string, message: string, language?: string) => Promise<string>;
  fetchProjectSources: (projectId: string) => Promise<void>;
  fetchProjectInteractions: (projectId: string) => Promise<void>;
  clearSources: () => void;
  setPendingSources: (sources: Array<{ id: string; kind: 'web' | 'telegram' | 'rss'; status: string }>) => void;
}

// Track processed stream.done events to prevent duplicates
const processedDoneEvents = new Set<string>();

export const useDiscoveryStore = create<DiscoveryState>((set, get) => ({
  sources: [],
  loading: false,
  error: null,
  correlationId: null,
  answer: null,
  interactions: [],
  pendingSources: [],
  lastAnalysisStatus: undefined,

  setPendingSources: (sources) => set({ pendingSources: sources }),

  executeDiscovery: async (data) => {
    set({ loading: true, error: null });
    try {
      const response = await api.executeDiscovery(data);
      console.log('[Store] API response:', response);
      set({
        correlationId: response.correlationId,
        loading: false,
      });
      return response;
    } catch (error: any) {
      console.error('[Store] API error:', error);
      // Network errors are handled by the global error interceptor
      if (error.response) {
        set({ error: error.message, loading: false });
      } else {
        set({ loading: false });
      }
      throw error;
    }
  },

  fetchProjectSources: async (projectId) => {
    const state = get();
    // Защита от множественных одновременных вызовов
    if (state.loading && (state as any).lastFetchedProjectId === projectId) {
      console.log('[fetchProjectSources] Already loading, skipping duplicate call');
      return;
    }
    
    set({ loading: true, error: null, lastFetchedProjectId: projectId } as any);
    try {
      const sources = await api.getProjectSources(projectId);
      // Merge pending sources with fetched sources (pending take precedence if status is 'fetching')
      set((state) => {
        const merged = [...sources];
        for (const pending of state.pendingSources) {
          const exists = merged.find((s) => s.id === pending.id || s.displayName === pending.id);
          if (!exists) {
            merged.push({
              id: pending.id,
              displayName: pending.id,
              kind: pending.kind,
              status: pending.status,
            } as any);
          }
        }
        return { sources: merged, loading: false };
      });
    } catch (error: any) {
      // Network errors are handled by the global error interceptor
      if (error.response) {
        set({ error: error.message, loading: false } as any);
      } else {
        set({ loading: false } as any);
      }
    }
  },

  fetchProjectInteractions: async (projectId) => {
    // Защита от множественных одновременных вызовов для одного projectId
    // Используем отдельный флаг для отслеживания загрузки interactions
    const state = get();
    if ((state as any).fetchingInteractions && (state as any).lastFetchedInteractionsProjectId === projectId) {
      console.log('[fetchProjectInteractions] Already fetching, skipping duplicate call');
      return;
    }
    
    set({ fetchingInteractions: true, lastFetchedInteractionsProjectId: projectId } as any);
    try {
      const items = await api.getProjectInteractions(projectId);
      set({ interactions: items, fetchingInteractions: false } as any);
    } catch (error: any) {
      // Network errors are handled by the global error interceptor
      if (error.response) {
        set({ error: error.message, fetchingInteractions: false } as any);
      } else {
        set({ fetchingInteractions: false } as any);
      }
      // Silently fail for network errors
    }
  },
  sendChatMessage: async (projectId: string, message: string, language?: string) => {
    // Append optimistic user message and a placeholder "Thinking..."
    const now = new Date().toISOString();
    set((state) => ({
      interactions: [
        ...state.interactions,
        { id: now, kind: 'discovery.query', content: message, createdAt: now },
        { id: now + '-thinking', kind: 'discovery.answer', content: 'Thinking...', createdAt: now },
      ],
    }));

    try {
      const { answer } = await api.sendChatMessage(projectId, message, language);
      // Replace the last placeholder with real answer
      set((state) => {
        const items = [...state.interactions];
        const idx = items.findIndex((it) => it.id === now + '-thinking');
        if (idx >= 0) {
          items[idx] = { ...items[idx], content: answer } as any;
        } else {
          items.push({ id: new Date().toISOString(), kind: 'discovery.answer', content: answer, createdAt: new Date().toISOString() });
        }
        return { interactions: items } as any;
      });
      return answer;
    } catch (e: any) {
      set((state) => {
        const items = [...state.interactions];
        const idx = items.findIndex((it) => it.id === now + '-thinking');
        if (idx >= 0) {
          items[idx] = { ...items[idx], content: 'Failed to get answer' } as any;
        }
        return { interactions: items } as any;
      });
      throw e;
    }
  },

  clearSources: () => {
    set({ sources: [], correlationId: null, answer: null, pendingSources: [], interactions: [] });
  },
  connectWs: async (tenantId) => {
    async function openSocket() {
      // Prevent multiple simultaneous connection attempts
      if (isConnecting) {
        console.log('[WS] Already connecting, skipping duplicate attempt');
        return;
      }
      
      // Skip if already connected
      if (wsRef && (wsRef.readyState === WebSocket.CONNECTING || wsRef.readyState === WebSocket.OPEN)) {
        console.log('[WS] Already connected or connecting, skipping');
        return;
      }
      
      isConnecting = true;
      
      try {
        // Refresh Firebase token each (re)connect
        const { auth } = await import('../lib/firebase');
        const user = auth?.currentUser;
        if (!user) {
          isConnecting = false;
          return;
        }
        const token = await user.getIdToken(true);

        const { getWsBrokerBaseUrl } = await import('../lib/ws');
        const toWs = (u: string) => u.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:');
        const wsUrl = `${toWs(getWsBrokerBaseUrl())}/ws?token=${encodeURIComponent(token)}`;

        // Close previous if any
        if (wsRef) {
          try {
            wsRef.onclose = null; // Remove handlers to prevent reconnect
            wsRef.onerror = null;
            wsRef.close();
          } catch {}
          wsRef = null;
        }

        wsRef = new WebSocket(wsUrl);
        // Expose wsRef to window for verified page subscription
        (window as any).__wsRef = wsRef;
        import('./wsStore').then(({ useWsStore }) => {
          useWsStore.getState().setConnecting(wsUrl, tenantId);
          useWsStore.getState().setReadyState(wsRef?.readyState ?? 0);
        }).catch(() => {});
        isManuallyClosed = false;

        wsRef.onopen = () => {
          isConnecting = false;
          clearReconnectTimer();
          reconnectAttempts = 0;
          import('./wsStore').then(({ useWsStore }) => {
            useWsStore.getState().setOpen(wsRef?.readyState);
          }).catch(() => {});
          // Always restore subscription to current project on reconnect
          // This ensures subscription is restored even if project didn't change
          // Critical: backend loses subscription state on WS disconnect, so we must resubscribe
          try {
            import('./projectsStore').then(async ({ useProjectsStore }) => {
              const current = useProjectsStore.getState().currentProject as any;
              const pid = current?.id;
              if (pid && wsRef && wsRef.readyState === WebSocket.OPEN) {
                try {
                  await requestProjectSubscription(pid, { timeoutMs: 6000, force: true });
                console.log('[WS] Restored subscription on reconnect', { 
                  projectId: pid, 
                  wasSubscribed: pid === lastSubscribedProjectId 
                });
                } catch (err) {
                  console.warn('[WS] Failed to restore subscription on reconnect', err);
                }
              }
            }).catch(() => {});
          } catch {}
        };

        const scheduleReconnect = () => {
          isConnecting = false;
          if (isManuallyClosed) return;
          lastSubscribedProjectId = undefined;
          rejectAllSubscriptionWaiters('WebSocket disconnected');
          import('./wsStore').then(({ useWsStore }) => {
            useWsStore.getState().setClosed(undefined, wsRef?.readyState);
          }).catch(() => {});
          reconnectAttempts += 1;
          const delay = computeBackoffDelay(reconnectAttempts);
          clearReconnectTimer();
          reconnectTimer = window.setTimeout(() => {
            openSocket();
          }, delay) as unknown as number;
        };

        wsRef.onclose = () => {
          // Reject all pending subscription waiters when WebSocket closes
          rejectAllSubscriptionWaiters('WebSocket closed');
          scheduleReconnect();
        };
        wsRef.onerror = () => {
          // Reject all pending subscription waiters when WebSocket errors
          rejectAllSubscriptionWaiters('WebSocket error');
          scheduleReconnect();
        };

        wsRef.onmessage = (evt) => {
          const raw = evt.data;
          if (typeof raw !== 'string') {
            console.warn('[WS] Ignoring non-text message', { dataType: typeof raw });
            return;
          }
          const text = raw.trim();
          if (!text) {
            console.warn('[WS] Ignoring empty WS message');
            return;
          }
          let msg: any;
          try {
            msg = JSON.parse(text);
          } catch (err) {
            console.error('[WS] Error parsing message:', { err, preview: text.slice(0, 200) });
            return;
          }
          try {
            import('./wsStore').then(({ useWsStore }) => {
              try { useWsStore.getState().noteMessage(msg?.type, msg?.data); } catch {}
            }).catch(() => {});

            // Horizon realtime events (Hook -> Assemble -> Sled).
            if (typeof msg?.type === 'string' && msg.type.startsWith('horizon.')) {
              import('./horizonRealtimeStore')
                .then(({ useHorizonRealtimeStore }) => {
                  try { useHorizonRealtimeStore.getState().handleWsEvent(msg.type, msg.data); } catch {}
                })
                .catch(() => {});
            }
            
            // Notify verified.stream listeners
            if (msg?.type?.startsWith('verified.stream.')) {
              const listeners = (window as any).__verifiedWsListeners || [];
              listeners.forEach((listener: (evt: MessageEvent) => void) => {
                try {
                  listener(evt);
                } catch (e) {
                  console.error('Verified WS listener error:', e);
                }
              });
            }

            // Handle subscription ack to reflect connId and ts in diagnostics
            if (msg?.type === 'ws.subscribed' && msg?.data?.projectId) {
              lastSubscribedProjectId = msg.data.projectId;
              resolveSubscriptionWaiters(msg.data.projectId);
              import('./wsStore').then(({ useWsStore }) => {
                useWsStore.getState().setSubscribed(msg.data.projectId, msg.data.connId, msg.data.ts);
              }).catch(() => {});
              console.log('[WS] Received subscription ACK', { 
                projectId: msg.data.projectId, 
                connId: msg.data.connId 
              });
            }

            if (msg?.type === 'discovery.profile' && msg.data?.profile) {
              set((state) => {
                const idx = state.sources.findIndex((s) => s.id === msg.data.sourceId);
                if (idx >= 0) {
                  // Update existing source
                  const updated = [...state.sources];
                  updated[idx] = { ...updated[idx], profile: msg.data.profile, status: 'completed' } as any;
                  return { sources: updated } as any;
                } else {
                  // Add new source if not exists (fetch from API to get full DataSource)
                  console.log('[WS] Source not found locally, fetching from API:', msg.data.sourceId);
                  if (msg.data.projectId) {
                    api.getProjectSources(msg.data.projectId).then((sources) => {
                      const source = sources.find((s) => s.id === msg.data.sourceId);
                      if (source) {
                        set((s) => ({
                          sources: [...s.sources.filter((src) => src.id !== source.id), { ...source, profile: msg.data.profile, status: 'completed' }]
                        }));
                      }
                    }).catch((e) => console.error('Failed to fetch source:', e));
                  }
                  return state as any;
                }
              });
              // Refresh interactions to include new context/profile snippets
              if (msg?.data?.projectId) {
                api.getProjectInteractions(msg.data.projectId)
                  .then((items) => set({ interactions: items }))
                  .catch((e) => console.error('Failed to refresh interactions:', e));
              }
            }
            
            if (msg?.type === 'discovery.status' && msg.data?.sourceId) {
              set((state) => {
                const idx = state.sources.findIndex((s) => s.id === msg.data.sourceId);
                if (idx >= 0) {
                  const updated = [...state.sources];
                  updated[idx] = { ...updated[idx], status: msg.data.status, __hint: msg.data?.hint, __firstProfile: !!msg.data?.firstProfile } as any;
                  return { sources: updated } as any;
                } else if (msg.data.status === 'fetching' && msg.data.projectId) {
                  // Add placeholder source for visual feedback; drop any skeleton by matching displayName
                  const displayName: string | undefined = msg.data.displayName;
                  const filtered = displayName
                    ? state.sources.filter((s) => s.displayName !== displayName && s.id !== displayName)
                    : state.sources;
                  const placeholder = {
                    id: msg.data.sourceId,
                    displayName: displayName || msg.data.sourceId,
                    status: msg.data.status,
                    kind: msg.data.kind || 'unknown',
                    __hint: msg.data?.hint,
                    __firstProfile: !!msg.data?.firstProfile,
                  } as any;
                  return { sources: [...filtered, placeholder] } as any;
                }
                return state as any;
              });
            }
            
            if (msg?.type === 'discovery.answer' && typeof msg.data?.answer === 'string') {
              set((state) => {
                const items = [...state.interactions];
                const model = typeof msg.data?.model === 'string' ? msg.data.model : undefined;
                const usage = msg.data?.usage;
                // Prefer replacing any existing Thinking... placeholder instead of appending (prevents duplicates for chat path)
                const idx = [...items].reverse().findIndex((it) => it.kind === 'discovery.answer' && it.content === 'Thinking...');
                if (idx >= 0) {
                  const realIdx = items.length - 1 - idx;
                  items[realIdx] = { ...items[realIdx], content: msg.data.answer, ...(model ? { model } : {}), ...(usage ? { usage } : {}) } as any;
                } else {
                  const createdAt = new Date().toISOString();
                  items.push({ id: createdAt, kind: 'discovery.answer', content: msg.data.answer, ...(model ? { model } : {}), ...(usage ? { usage } : {}), createdAt } as any);
                }
                return { answer: msg.data.answer, interactions: items } as any;
              });
            }
  
            // Route GenAI streaming events to chat store (filter by project/stream if present)
            if (typeof msg?.type === 'string' && msg.type.startsWith('genai.stream.')) {
              import('./genaiChatStore').then(({ useGenaiChatStore }) => {
                const st = useGenaiChatStore.getState();
                const eventStreamId = msg?.data?.streamId;
                const currentStreamId = st.currentStreamId;
                const currentPid = st.projectId;
                const eventPid = msg?.data?.projectId;
                
                // CRITICAL: Check if this event belongs to the active stream by streamId
                // If streamId matches currentStreamId, accept the event even if projectId doesn't match
                // This handles the case when user switches projects during an active stream
                const isActiveStream = eventStreamId && currentStreamId && eventStreamId === currentStreamId;
                
                // CRITICAL: For genai.stream.chunk, always accept if projectId matches (even without streamId match)
                // This ensures chunks are displayed even if streamId tracking has issues
                const isChunkEvent = msg.type === 'genai.stream.chunk';
                const projectMatches = eventPid && currentPid && eventPid === currentPid;
                
                try {
                  // If store has no projectId but event has one, set it (helps with newly created projects)
                  if (!currentPid || currentPid === 'local-unfold') {
                    if (eventPid && typeof eventPid === 'string') {
                      st.setProjectId(eventPid);
                      console.log('[DiscoveryStore] Auto-set projectId from stream event', { projectId: eventPid, eventType: msg.type });
                    }
                  } else if (eventPid && currentPid && eventPid !== currentPid) {
                    // Project mismatch - check if it's an active stream or a chunk event
                    if (isActiveStream) {
                      // Active stream but projectId mismatch - accept anyway but log warning
                      console.warn('[DiscoveryStore] Stream event projectId mismatch but accepting (active stream)', { 
                        storeProjectId: currentPid, 
                        eventProjectId: eventPid, 
                        eventType: msg.type,
                        streamId: eventStreamId,
                        currentStreamId: currentStreamId
                      });
                      // Continue processing - don't return
                    } else {
                      // Project mismatch and not an active stream - filter out
                      console.warn('[DiscoveryStore] Stream event projectId mismatch (not active stream)', { 
                        storeProjectId: currentPid, 
                        eventProjectId: eventPid, 
                        eventType: msg.type,
                        streamId: eventStreamId,
                        currentStreamId: currentStreamId
                      });
                      return; // Filter out mismatched projects that are not active streams
                    }
                  }
                  // If projectId matches, always accept (regardless of streamId)
                } catch (e) {
                  console.error('[DiscoveryStore] Error checking projectId filter', e);
                }
                switch (msg.type) {
                  case 'genai.stream.start':
                    // Save streamId from event if not already set (handles race condition)
                    if (eventStreamId && !currentStreamId) {
                      useGenaiChatStore.setState({ currentStreamId: eventStreamId } as any);
                    }
                    console.log('[DiscoveryStore] Received genai.stream.start', { 
                      projectId: msg.data?.projectId, 
                      streamId: msg.data?.streamId,
                      storeProjectId: st.projectId,
                      currentStreamId: currentStreamId || eventStreamId
                    });
                    st.onStart();
                    break;
                  case 'genai.stream.chunk':
                    // Ensure streaming is enabled before processing chunks
                    if (!st.streaming) {
                      if (IS_DEV) {
                        console.debug('[DiscoveryStore] genai.stream.chunk received before streaming flag, auto-starting', {
                          projectId: msg.data?.projectId,
                          streamId: msg.data?.streamId,
                          storeProjectId: st.projectId
                        });
                      }
                      st.onStart();
                    }
                    // CRITICAL: Ensure currentStreamId is set from chunk event if not already set
                    if (eventStreamId && !currentStreamId) {
                      useGenaiChatStore.setState({ currentStreamId: eventStreamId } as any);
                    }
                    if (msg.data?.text) {
                      if (IS_DEV) {
                        console.debug('[DiscoveryStore] Received genai.stream.chunk', { 
                          textLength: msg.data.text.length,
                          chunkIndex: msg.data?.chunkIndex,
                          projectId: msg.data?.projectId, 
                          streamId: msg.data?.streamId,
                          storeProjectId: st.projectId,
                          streaming: st.streaming
                        });
                      }
                      st.onChunk(msg.data.text);
                    } else {
                      if (IS_DEV) {
                        console.warn('[DiscoveryStore] genai.stream.chunk missing text', msg.data);
                      }
                    }
                    break;
                  case 'genai.stream.tool_call':
                    if (Array.isArray(msg.data?.toolCalls)) st.onToolCall(msg.data.toolCalls);
                    break;
                  case 'genai.stream.tool_result':
                    if (Array.isArray(msg.data?.toolCalls)) st.onToolResult(msg.data.toolCalls);
                    break;
                  case 'genai.stream.tool_retry':
                    // Notify about tool retry attempt
                    console.log('[DiscoveryStore] Tool retry', msg.data);
                    st.onToolRetry?.(msg.data);
                    break;
                  case 'genai.stream.tool_correction':
                    // Notify about tool correction
                    console.log('[DiscoveryStore] Tool correction', msg.data);
                    st.onToolCorrection?.(msg.data);
                    break;
                  case 'genai.stream.done':
                    // Prevent duplicate processing of done events (PubSub can deliver messages multiple times)
                    const streamId = msg.data?.streamId;
                    const doneEventKey = streamId ? `done:${streamId}` : null;
                    if (doneEventKey && processedDoneEvents.has(doneEventKey)) {
                      console.log('[DiscoveryStore] Skipping duplicate genai.stream.done event', { streamId });
                      break;
                    }
                    if (doneEventKey) {
                      processedDoneEvents.add(doneEventKey);
                      // Clean up after 5 minutes to prevent memory leak
                      setTimeout(() => processedDoneEvents.delete(doneEventKey), 5 * 60 * 1000);
                    }
                    console.log('[DiscoveryStore] Received genai.stream.done', { 
                      projectId: msg.data?.projectId, 
                      streamId: streamId,
                      storeProjectId: st.projectId,
                      usage: msg.data?.usage,
                      totalChunks: msg.data?.totalChunks
                    });
                    // CRITICAL: onDoneMeta must run BEFORE onDone so that usage/searchQueries/sources are in state when creating Interaction
                    st.onDoneMeta({ 
                      usage: msg.data?.usage, 
                      attributions: msg.data?.attributions,
                      searchQueries: msg.data?.searchQueries,
                      sources: msg.data?.sources
                    });
                    st.onDone();
                    break;
                  case 'genai.stream.error':
                    st.onError(msg.data?.error || 'Stream error');
                    break;
                  default:
                    // Handle Research mode events (genai.stream.research.*)
                    if (msg.type.startsWith('genai.stream.research.')) {
                      handleResearchEvent(msg, st, eventPid, eventStreamId, currentPid, isActiveStream);
                    }
                    break;
                }
              }).catch(() => {});
            }
  
            // Route artifact generation events (filter by project if present)
            if (typeof msg?.type === 'string' && msg.type.startsWith('artifact.')) {
              import('./artifactStore').then(({ useArtifactStore }) => {
                try {
                  import('./genaiChatStore').then(({ useGenaiChatStore }) => {
                    const pid = useGenaiChatStore.getState().projectId;
                    if (msg?.data?.projectId && pid && msg.data.projectId !== pid) return;
                    useArtifactStore.getState().handleEvent(msg);
                  }).catch(() => { useArtifactStore.getState().handleEvent(msg); });
                } catch { useArtifactStore.getState().handleEvent(msg); }
              }).catch(() => {});
            }
  
            // Document process streaming: uploaded/parsing/parsed/deleted
            if (msg?.type === 'document.process') {
              const ev = msg.data || {};
              console.log('[WS] document.process event received:', ev);
              // expose minimal bus: lastDocEvent for UI
              set((state) => ({ ...state, lastDocEvent: ev } as any));
            }
  
            // Unified analysis status: analysis.status
            if (msg?.type === 'analysis.status') {
              const ev = msg.data || {};
              console.log('[WS] analysis.status event received:', ev);
              if (ev && typeof ev.projectId === 'string' && typeof ev.kind === 'string' && typeof ev.status === 'string') {
                set({ lastAnalysisStatus: ev });
              }
            }

            // Agent updates: agent.update
            if (msg?.type === 'agent.update') {
              const update = msg.data as import('../types/agents').AgentUpdate;
              if (update && update.taskId && update.agentId) {
                console.log('[WS] agent.update event received:', update);
                import('./agentsStore').then(({ useAgentsStore }) => {
                  useAgentsStore.getState().handleUpdate(update);
                }).catch((e) => console.error('[WS] Failed to handle agent.update:', e));
              }
            }

            // Interaction created/updated events (e.g., when LLM creates graph via tool)
            if (msg?.type === 'interaction.created' || msg?.type === 'interaction.updated') {
              const projectId = msg?.data?.projectId;
              const interactionId = msg?.data?.interactionId;
              if (projectId && interactionId) {
                console.log('[WS] Interaction event received:', msg.type, { projectId, interactionId, subkind: msg.data?.subkind });
                // Add or update the specific interaction (more efficient than reloading all)
                import('./genaiChatStore').then(({ useGenaiChatStore }) => {
                  const st = useGenaiChatStore.getState();
                  // Only update if this is for the current project
                  if (st.projectId === projectId) {
                    st.addOrUpdateInteraction(interactionId).catch((e) => {
                      console.error('[WS] Failed to add/update interaction after', msg.type, e);
                      // Fallback: try hydrateInteractions if addOrUpdateInteraction fails
                      st.hydrateInteractions().catch((e2) => {
                        console.error('[WS] Failed to refresh interactions after', msg.type, e2);
                      });
                    });
                  }
                }).catch((e) => console.error('[WS] Failed to handle interaction event:', e));
              }
            }
            
            if (msg?.type === 'discovery.project' || msg?.type === 'discoveryProject') {
              console.log('[WS] New project event:', msg.data);
              // New project created - add directly to projects store
              import('./projectsStore').then(({ useProjectsStore }) => {
                const store = useProjectsStore.getState();
                const projectData = msg.data;
                
                // Check if project already exists
                const exists = store.projects.find((p) => p.id === projectData.projectId);
                if (!exists && projectData.projectId && projectData.title) {
                  // Add new project to the list
                  const newProject = {
                    id: projectData.projectId,
                    displayName: projectData.title,
                    view: 'discovery',
                    status: 'active',
                    createdAt: projectData.createdAt || new Date().toISOString(),
                    updatedAt: projectData.createdAt || new Date().toISOString(),
                  } as any;
                  
                  console.log('[WS] Adding new project to store:', newProject);
                  useProjectsStore.setState((state) => ({
                    projects: [newProject, ...state.projects],
                    currentProject: newProject, // Auto-select new project
                  }));
                }
              }).catch((e) => {
                console.error('Failed to load projectsStore:', e);
              });
            }
          } catch (err) {
            console.error('[WS] Error handling message:', err);
          }
        };
      } catch (e) {
        // Immediate retry plan if startup failed
        isConnecting = false;
        const delay = computeBackoffDelay(++reconnectAttempts);
        clearReconnectTimer();
        reconnectTimer = window.setTimeout(() => openSocket(), delay) as unknown as number;
      }
    }

    // Visibility/online triggers to speed up recovery (attach only once)
    if (!listenersAttached) {
      const onVisibility = () => {
        if (!document.hidden && (!wsRef || wsRef.readyState !== WebSocket.OPEN)) {
          reconnectAttempts = 0;
          clearReconnectTimer();
          openSocket();
        }
      };
      const onOnline = () => {
        reconnectAttempts = 0;
        clearReconnectTimer();
        openSocket();
      };
      window.addEventListener('visibilitychange', onVisibility);
      window.addEventListener('online', onOnline);
      windowEventListeners = { visibilitychange: onVisibility, online: onOnline };
      listenersAttached = true;
    }

    // Initial connect
    openSocket();

    // Subscribe on project changes (in case currentProject isn't ready at ws onopen)
    // Clean up previous subscription if exists to prevent memory leaks
    if (projectsStoreUnsubscribe) {
      try {
        projectsStoreUnsubscribe();
      } catch {}
      projectsStoreUnsubscribe = null;
    }
    try {
      import('./projectsStore').then(({ useProjectsStore }) => {
        const unsubscribe = useProjectsStore.subscribe((st) => {
          const pid = (st as any)?.currentProject?.id as string | undefined;
          if (!pid) return;
          if (pendingSubscriptions.has(pid) || lastSubscribedProjectId === pid) {
            return;
          }
          requestProjectSubscription(pid)
            .then(() => {
              console.log('[WS] Ensured subscription for project change', { projectId: pid });
            })
            .catch((err) => {
              console.warn('[WS] Failed to ensure subscription on project change', { projectId: pid, err });
            });
        });
        projectsStoreUnsubscribe = unsubscribe;
      }).catch(() => {});
    } catch {}

    // Expose a minimal helper for on-demand subscription (used by chat store before stream)
    try {
      (window as any).wsSubscribeProject = (pid: string, opts?: SubscriptionOptions) => {
        if (!pid) {
          return Promise.resolve();
          }
        return requestProjectSubscription(pid, opts);
      };
    } catch {}
  },
}));
