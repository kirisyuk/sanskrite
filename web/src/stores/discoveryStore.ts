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

export const useDiscoveryStore = create<DiscoveryState>((set) => ({
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
    set({ loading: true, error: null });
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
        set({ error: error.message, loading: false });
      } else {
        set({ loading: false });
      }
    }
  },

  fetchProjectInteractions: async (projectId) => {
    try {
      const items = await api.getProjectInteractions(projectId);
      set({ interactions: items });
    } catch (error: any) {
      // Network errors are handled by the global error interceptor
      if (error.response) {
        set({ error: error.message });
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
          // Subscribe to current project for server-side routing (best-effort)
          try {
            import('./projectsStore').then(({ useProjectsStore }) => {
              const current = useProjectsStore.getState().currentProject as any;
              const pid = current?.id;
              if (pid && wsRef && wsRef.readyState === WebSocket.OPEN) {
                wsRef.send(JSON.stringify({ type: 'subscribe', projectId: pid }));
                import('./wsStore').then(({ useWsStore }) => { useWsStore.getState().setSubscribed(pid); }).catch(() => {});
              }
            }).catch(() => {});
          } catch {}
        };

        const scheduleReconnect = () => {
          isConnecting = false;
          if (isManuallyClosed) return;
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
              import('./wsStore').then(({ useWsStore }) => {
                useWsStore.getState().setSubscribed(msg.data.projectId, msg.data.connId, msg.data.ts);
              }).catch(() => {});
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
                // Filter: must match current project if provided
                try {
                  const currentPid = st.projectId;
                  if (msg?.data?.projectId && currentPid && msg.data.projectId !== currentPid) return;
                } catch {}
                switch (msg.type) {
                  case 'genai.stream.start':
                    st.onStart();
                    break;
                  case 'genai.stream.chunk':
                    if (msg.data?.text) st.onChunk(msg.data.text);
                    break;
                  case 'genai.stream.tool_call':
                    if (Array.isArray(msg.data?.toolCalls)) st.onToolCall(msg.data.toolCalls);
                    break;
                  case 'genai.stream.tool_result':
                    if (Array.isArray(msg.data?.toolCalls)) st.onToolResult(msg.data.toolCalls);
                    break;
                  case 'genai.stream.done':
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
        let lastPid: string | undefined;
        const unsubscribe = useProjectsStore.subscribe((st) => {
          const pid = (st as any)?.currentProject?.id as string | undefined;
          if (!pid || pid === lastPid) return;
          lastPid = pid;
          if (wsRef && wsRef.readyState === WebSocket.OPEN) {
            try { wsRef.send(JSON.stringify({ type: 'subscribe', projectId: pid })); } catch {}
            import('./wsStore').then(({ useWsStore }) => { useWsStore.getState().setSubscribed(pid); }).catch(() => {});
          }
        });
        projectsStoreUnsubscribe = unsubscribe;
      }).catch(() => {});
    } catch {}

    // Expose a minimal helper for on-demand subscription (used by chat store before stream)
    try {
      (window as any).wsSubscribeProject = (pid: string) => {
        try {
          if (wsRef && wsRef.readyState === WebSocket.OPEN && pid) {
            wsRef.send(JSON.stringify({ type: 'subscribe', projectId: pid }));
            import('./wsStore').then(({ useWsStore }) => { useWsStore.getState().setSubscribed(pid); }).catch(() => {});
          }
        } catch {}
      };
    } catch {}
  },
}));