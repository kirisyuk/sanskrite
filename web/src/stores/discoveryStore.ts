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

interface DiscoveryState {
  sources: DataSource[];
  loading: boolean;
  error: string | null;
  correlationId: string | null;
  answer: string | null;
  interactions: Array<{ id: string; kind: string; content: string; createdAt: string }>;
  pendingSources: Array<{ id: string; kind: 'web' | 'telegram'; status: string }>;
  connectWs: (tenantId: string) => Promise<void>;
  
  // Actions
  executeDiscovery: (data: {
    projectId: string;
    queryText: string;
    language?: string;
    sources: { web: string[]; telegram: string[] };
  }) => Promise<{ correlationId: string }>;
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
      set({ error: error.message, loading: false });
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
      set({ error: error.message, loading: false });
    }
  },

  fetchProjectInteractions: async (projectId) => {
    try {
      const items = await api.getProjectInteractions(projectId);
      set({ interactions: items });
    } catch (error: any) {
      set({ error: error.message });
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
        const user = auth.currentUser;
        if (!user) {
          isConnecting = false;
          return;
        }
        const token = await user.getIdToken(true);

        const base = (import.meta as any).env?.VITE_API_BASE_URL || 'http://localhost:8080';
        const toWs = (u: string) => u.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:');
        const wsUrl = url || `${toWs(base)}/ws?token=${encodeURIComponent(token)}`;

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
        isManuallyClosed = false;

        wsRef.onopen = () => {
          isConnecting = false;
          clearReconnectTimer();
          reconnectAttempts = 0;
          import('./wsStore').then(({ useWsStore }) => {
            useWsStore.getState().setConnected(true);
          }).catch(() => {});
        };

        const scheduleReconnect = () => {
          isConnecting = false;
          if (isManuallyClosed) return;
          import('./wsStore').then(({ useWsStore }) => {
            useWsStore.getState().setConnected(false);
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
          console.log('[WS] Message received:', msg);
          
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
          }
          
          if (msg?.type === 'discovery.status' && msg.data?.sourceId) {
            set((state) => {
              const idx = state.sources.findIndex((s) => s.id === msg.data.sourceId);
              if (idx >= 0) {
                const updated = [...state.sources];
                updated[idx] = { ...updated[idx], status: msg.data.status } as any;
                return { sources: updated } as any;
              } else if (msg.data.status === 'fetching' && msg.data.projectId) {
                // Add placeholder source for visual feedback
                const placeholder = {
                  id: msg.data.sourceId,
                  displayName: msg.data.sourceId,
                  status: msg.data.status,
                  kind: 'unknown',
                } as any;
                return { sources: [...state.sources, placeholder] } as any;
              }
              return state as any;
            });
          }
          
          if (msg?.type === 'discovery.answer' && typeof msg.data?.answer === 'string') {
            set((state) => {
              const createdAt = new Date().toISOString();
              const items = [
                ...state.interactions,
                { id: createdAt, kind: 'discovery.answer', content: msg.data.answer, createdAt },
              ];
              return { answer: msg.data.answer, interactions: items } as any;
            });
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
      listenersAttached = true;
    }

    // Initial connect
    openSocket();
  },
}));

