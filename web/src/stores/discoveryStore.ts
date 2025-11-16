import { create } from 'zustand';
import { api } from '../lib/api';
import type { DataSource } from '../types';

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
  pendingSources: Array<{ id: string; kind: 'web' | 'telegram'; status: string }>;
  lastDocEvent?: any;
  lastAnalysisStatus?: { projectId: string; kind: string; status: 'started'|'done'|'failed'; elapsedMs?: number; count?: number; error?: string; ts: number } | undefined;
  connectWs: (tenantId: string) => Promise<void>;
  
  // Actions
  executeDiscovery: (data: {
    projectId: string;
    queryText: string;
    language?: string;
    sources: { web: string[]; telegram: string[] };
  }) => Promise<{ correlationId: string }>;
  sendChatMessage: (projectId: string, message: string, language?: string) => Promise<string>;
  fetchProjectSources: (projectId: string) => Promise<void>;
  fetchProjectInteractions: (projectId: string) => Promise<void>;
  clearSources: () => void;
  setPendingSources: (sources: Array<{ id: string; kind: 'web' | 'telegram'; status: string }>) => void;
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
    const url = (window as any).buildWsUrl ? (window as any).buildWsUrl(tenantId) : undefined;
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

        const base = (import.meta as any).env?.VITE_API_BASE_URL || 'http://localhost:8080';
        const broker = (import.meta as any).env?.VITE_WS_BROKER_URL as string | undefined;
        const toWs = (u: string) => u.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:');
        const wsUrl = (() => {
          if (broker && broker.trim()) return `${toWs(broker.replace(/\/$/, ''))}/ws?token=${encodeURIComponent(token)}`;
          if (url && typeof url === 'string') return url;
          return `${toWs(base)}/ws?token=${encodeURIComponent(token)}`;
        })();

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

        wsRef.onclose = scheduleReconnect;
        wsRef.onerror = scheduleReconnect;

        wsRef.onmessage = (evt) => {
          try {
            const msg = JSON.parse(evt.data);
            console.log('[WS] Received message', { type: msg?.type, hasData: !!msg?.data, rawLength: evt.data?.length });
            import('./wsStore').then(({ useWsStore }) => {
              try { useWsStore.getState().noteMessage(msg?.type, msg?.data); } catch {}
            }).catch(() => {});
            
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
                // Prefer replacing any existing Thinking... placeholder instead of appending (prevents duplicates for chat path)
                const idx = [...items].reverse().findIndex((it) => it.kind === 'discovery.answer' && it.content === 'Thinking...');
                if (idx >= 0) {
                  const realIdx = items.length - 1 - idx;
                  items[realIdx] = { ...items[realIdx], content: msg.data.answer } as any;
                } else {
                  const createdAt = new Date().toISOString();
                  items.push({ id: createdAt, kind: 'discovery.answer', content: msg.data.answer, createdAt } as any);
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
                
                try {
                  // If store has no projectId but event has one, set it (helps with newly created projects)
                  if (!currentPid || currentPid === 'local-unfold') {
                    if (eventPid && typeof eventPid === 'string') {
                      st.setProjectId(eventPid);
                      console.log('[DiscoveryStore] Auto-set projectId from stream event', { projectId: eventPid, eventType: msg.type });
                    }
                  } else if (eventPid && currentPid && eventPid !== currentPid && !isActiveStream) {
                    // Project mismatch and not an active stream - filter out
                    // But if it's an active stream (by streamId), accept it anyway
                    console.warn('[DiscoveryStore] Stream event projectId mismatch (not active stream)', { 
                      storeProjectId: currentPid, 
                      eventProjectId: eventPid, 
                      eventType: msg.type,
                      streamId: eventStreamId,
                      currentStreamId: currentStreamId
                    });
                    return; // Filter out mismatched projects that are not active streams
                  } else if (eventPid && currentPid && eventPid !== currentPid && isActiveStream) {
                    // Active stream but projectId mismatch - accept anyway but log warning
                    console.warn('[DiscoveryStore] Stream event projectId mismatch but accepting (active stream)', { 
                      storeProjectId: currentPid, 
                      eventProjectId: eventPid, 
                      eventType: msg.type,
                      streamId: eventStreamId,
                      currentStreamId: currentStreamId
                    });
                    // Continue processing - don't return
                  }
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
                    if (msg.data?.text) {
                      console.log('[DiscoveryStore] Received genai.stream.chunk', { 
                        textLength: msg.data.text.length,
                        chunkIndex: msg.data?.chunkIndex,
                        projectId: msg.data?.projectId, 
                        streamId: msg.data?.streamId,
                        storeProjectId: st.projectId 
                      });
                      st.onChunk(msg.data.text);
                    } else {
                      console.warn('[DiscoveryStore] genai.stream.chunk missing text', msg.data);
                    }
                    break;
                  case 'genai.stream.tool_call':
                    if (Array.isArray(msg.data?.toolCalls)) st.onToolCall(msg.data.toolCalls);
                    break;
                  case 'genai.stream.tool_result':
                    if (Array.isArray(msg.data?.toolCalls)) st.onToolResult(msg.data.toolCalls);
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
            console.error('[WS] Error parsing message:', err);
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