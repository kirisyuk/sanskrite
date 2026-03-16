import { create } from 'zustand';
import { api } from '../lib/api';

export type StrategicRecommendationRecord = {
  id: string;
  tenantId: string;
  projectId?: string;
  lensId?: string;
  subjectType: string;
  subjectCanonicalPath: string;
  recommendationType: string;
  status: string;
  priority: string;
  confidence: number;
  headline: string;
  reason: string;
  evidence?: string[];
  expectedMissingTargets?: string[];
  currentCoverageStats?: Record<string, any>;
  proposedAction: string;
  domainContractHint?: string;
  execution?: Record<string, any>;
  createdAt?: string;
  updatedAt?: string;
};

type StrategicRecommendationsState = {
  items: StrategicRecommendationRecord[];
  pendingCount: number;
  tenantPendingCount: number;
  globalPendingCount: number;
  activeScope: 'tenant' | 'global' | 'all';
  loading: boolean;
  countLoading: boolean;
  error: string | null;
  fetchList: (params?: { status?: string; projectId?: string; lensId?: string; limit?: number; scope?: 'tenant' | 'global' | 'all' }) => Promise<void>;
  fetchCount: (params?: { scope?: 'tenant' | 'global' | 'all' }) => Promise<void>;
  approve: (id: string) => Promise<void>;
  dismiss: (id: string) => Promise<void>;
  applyRealtimeEvent: (type: string, data: any) => void;
};

function upsert(items: StrategicRecommendationRecord[], nextItem: StrategicRecommendationRecord): StrategicRecommendationRecord[] {
  const map = new Map(items.map((item) => [item.id, item] as const));
  map.set(nextItem.id, { ...(map.get(nextItem.id) || {}), ...nextItem });
  return Array.from(map.values()).sort((a, b) => {
    const ta = Date.parse(String(a.updatedAt || a.createdAt || 0));
    const tb = Date.parse(String(b.updatedAt || b.createdAt || 0));
    return tb - ta;
  });
}

function isPendingStatus(status: string): boolean {
  const s = String(status || '').trim().toLowerCase();
  return s === 'pending' || s === 'approved' || s === 'executing';
}

export const useStrategicRecommendationsStore = create<StrategicRecommendationsState>((set, get) => ({
  items: [],
  pendingCount: 0,
  tenantPendingCount: 0,
  globalPendingCount: 0,
  activeScope: 'tenant',
  loading: false,
  countLoading: false,
  error: null,

  fetchList: async (params) => {
    set({ loading: true, error: null });
    try {
      const response = await api.listStrategicRecommendations(params);
      set({
        items: Array.isArray(response?.items) ? response.items : [],
        activeScope: (response?.scope || params?.scope || 'tenant') as 'tenant' | 'global' | 'all',
        loading: false,
      });
    } catch (error: any) {
      set({ loading: false, error: error?.message || 'Failed to load recommendations' });
    }
  },

  fetchCount: async (params) => {
    set({ countLoading: true });
    try {
      const response = await api.getStrategicRecommendationsCount(params);
      set({
        pendingCount: Number(response?.count || 0),
        tenantPendingCount: Number(response?.tenantCount || 0),
        globalPendingCount: Number(response?.globalCount || 0),
        activeScope: (response?.scope || params?.scope || get().activeScope || 'tenant') as 'tenant' | 'global' | 'all',
        countLoading: false,
      });
    } catch {
      set({ countLoading: false });
    }
  },

  approve: async (id) => {
    const item = await api.approveStrategicRecommendation(id);
    set((state) => {
      const items = upsert(state.items, item);
      return {
        items,
        pendingCount: items.filter((entry) => isPendingStatus(entry.status)).length,
      };
    });
  },

  dismiss: async (id) => {
    const item = await api.dismissStrategicRecommendation(id);
    set((state) => {
      const items = upsert(state.items, item);
      return {
        items,
        pendingCount: items.filter((entry) => isPendingStatus(entry.status)).length,
      };
    });
  },

  applyRealtimeEvent: (type, data) => {
    if (!type.startsWith('horizon.recommendation.')) return;
    if (!data || typeof data !== 'object' || !data.id) return;
    set((state) => {
      const items = upsert(state.items, data as StrategicRecommendationRecord);
      return {
        items,
        pendingCount: items.filter((entry) => isPendingStatus(entry.status)).length,
      };
    });
  },
}));
