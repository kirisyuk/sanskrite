import { create } from 'zustand';
import { api } from '../lib/api';
import type { DataSource } from '../types';

interface DiscoveryState {
  sources: DataSource[];
  loading: boolean;
  error: string | null;
  correlationId: string | null;
  answer: string | null;
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
  clearSources: () => void;
  setPendingSources: (sources: Array<{ id: string; kind: 'web' | 'telegram'; status: string }>) => void;
}

export const useDiscoveryStore = create<DiscoveryState>((set) => ({
  sources: [],
  loading: false,
  error: null,
  correlationId: null,
  answer: null,
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

  clearSources: () => {
    set({ sources: [], correlationId: null, answer: null, pendingSources: [] });
  },
  connectWs: async (tenantId) => {
    const url = (window as any).buildWsUrl ? (window as any).buildWsUrl(tenantId) : undefined;
    let ws: WebSocket | null = null;
    try {
      // Get Firebase ID token
      const { auth } = await import('../lib/firebase');
      const user = auth.currentUser;
      if (!user) {
        return;
      }
      const token = await user.getIdToken();
      
      const base = (import.meta as any).env?.VITE_API_BASE_URL || 'http://localhost:8080';
      const toWs = (u: string) => u.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:');
      const wsUrl = url || `${toWs(base)}/ws?token=${encodeURIComponent(token)}`;
      ws = new WebSocket(wsUrl);
      
      ws.onopen = () => {
        import('./wsStore').then(({ useWsStore }) => {
          useWsStore.getState().setConnected(true);
        }).catch(() => {});
      };
      
      ws.onclose = () => {
        import('./wsStore').then(({ useWsStore }) => {
          useWsStore.getState().setConnected(false);
        }).catch(() => {});
      };
      
      ws.onerror = () => {
        import('./wsStore').then(({ useWsStore }) => {
          useWsStore.getState().setConnected(false);
        }).catch(() => {});
      };
      
      ws.onmessage = (evt) => {
        try {
          const msg = JSON.parse(evt.data);
          if (msg?.type === 'discovery.profile' && msg.data?.profile) {
            set((state) => {
              const idx = state.sources.findIndex((s) => s.id === msg.data.sourceId);
              if (idx >= 0) {
                const updated = [...state.sources];
                updated[idx] = { ...updated[idx], profile: msg.data.profile, status: 'completed' } as any;
                return { sources: updated } as any;
              }
              return state as any;
            });
          }
          if (msg?.type === 'discovery.status' && msg.data?.sourceId) {
            set((state) => {
              const idx = state.sources.findIndex((s) => s.id === msg.data.sourceId);
              if (idx >= 0) {
                const updated = [...state.sources];
                updated[idx] = { ...updated[idx], status: msg.data.status } as any;
                return { sources: updated } as any;
              }
              return state as any;
            });
          }
          if (msg?.type === 'discovery.answer' && typeof msg.data?.answer === 'string') {
            set({ answer: msg.data.answer });
          }
          if (msg?.type === 'discovery.project' && msg.data?.projectId) {
            // New project created - update projects store
            import('./projectsStore').then(({ useProjectsStore }) => {
              const { fetchProjects } = useProjectsStore.getState();
              fetchProjects('discovery').catch((e) => {
                console.error('Failed to refresh projects:', e);
              });
            }).catch((e) => {
              console.error('Failed to load projectsStore:', e);
            });
          }
        } catch {}
      };
    } catch {}
  },
}));

