import { create } from 'zustand';
import { api } from '../lib/api';
import type { DataSource } from '../types';

interface DiscoveryState {
  sources: DataSource[];
  loading: boolean;
  error: string | null;
  correlationId: string | null;
  answer: string | null;
  connectWs: (tenantId: string) => void;
  
  // Actions
  executeDiscovery: (data: {
    projectId: string;
    queryText: string;
    language?: string;
    sources: { web: string[]; telegram: string[] };
  }) => Promise<void>;
  fetchProjectSources: (projectId: string) => Promise<void>;
  clearSources: () => void;
}

export const useDiscoveryStore = create<DiscoveryState>((set) => ({
  sources: [],
  loading: false,
  error: null,
  correlationId: null,
  answer: null,

  executeDiscovery: async (data) => {
    set({ loading: true, error: null });
    try {
      const response = await api.executeDiscovery(data);
      set({
        correlationId: response.correlationId,
        loading: false,
      });
    } catch (error: any) {
      set({ error: error.message, loading: false });
      throw error;
    }
  },

  fetchProjectSources: async (projectId) => {
    set({ loading: true, error: null });
    try {
      const sources = await api.getProjectSources(projectId);
      set({ sources, loading: false });
    } catch (error: any) {
      set({ error: error.message, loading: false });
    }
  },

  clearSources: () => {
    set({ sources: [], correlationId: null, answer: null });
  },
  connectWs: (tenantId) => {
    const url = (window as any).buildWsUrl ? (window as any).buildWsUrl(tenantId) : undefined;
    let ws: WebSocket | null = null;
    try {
      const base = (import.meta as any).env?.VITE_API_BASE_URL || 'http://localhost:8080';
      const toWs = (u: string) => u.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:');
      const wsUrl = url || `${toWs(base)}/ws?tenantId=${encodeURIComponent(tenantId)}`;
      ws = new WebSocket(wsUrl);
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

