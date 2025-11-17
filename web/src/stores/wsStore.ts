import { create } from 'zustand';

type WsHealth = 'ok' | 'degraded' | 'down';

interface WsDiagCounters {
  byType: Record<string, number>;
  total: number;
}

interface LastStreamDiag {
  projectId?: string;
  streamId?: string;
  lastStartAt?: number;
  lastChunkAt?: number;
  lastDoneAt?: number;
  errors?: number;
  chunks?: number;
}

interface WsState {
  // derived UI flags
  connected: boolean;
  health: WsHealth;

  // connection info
  wsUrl?: string;
  tenantId?: string;
  subscribedProjectId?: string;
  connId?: string;
  readyState?: number; // WebSocket readyState snapshot

  // timing
  lastOpenAt?: number;
  lastCloseAt?: number;
  lastSubscribedAt?: number;
  lastError?: string;
  lastMessageAt?: number;

  // counters
  counters: WsDiagCounters;
  lastGenai: LastStreamDiag;
  lastArtifact: LastStreamDiag;

  // modal
  modalOpen: boolean;

  // actions
  setConnecting: (wsUrl: string, tenantId?: string) => void;
  setOpen: (readyState?: number) => void;
  setClosed: (err?: string, readyState?: number) => void;
  setConnected: (connected: boolean) => void;
  setSubscribed: (projectId: string, connId?: string, ts?: number) => void;
  noteMessage: (type?: string, data?: any) => void;
  setHealth: (health: WsHealth) => void;
  setReadyState: (rs: number) => void;
  setModalOpen: (open: boolean) => void;
  getDiagnostics: () => string;
}

export const useWsStore = create<WsState>((set, get) => ({
  connected: false,
  health: 'down',
  counters: { byType: {}, total: 0 },
  lastGenai: {},
  lastArtifact: {},
  modalOpen: false,

  setConnecting: (wsUrl, tenantId) => set({ wsUrl, tenantId, health: 'degraded' }),
  setOpen: (readyState) => set({ connected: true, health: 'degraded', lastOpenAt: Date.now(), readyState }),
  setClosed: (err, readyState) => set({ connected: false, health: 'down', lastCloseAt: Date.now(), lastError: err, readyState }),
  setConnected: (connected) => set({ connected, health: connected ? 'ok' : 'down' }),
  setSubscribed: (projectId, connId, ts) => set({ subscribedProjectId: projectId, connId, lastSubscribedAt: ts || Date.now() }),
  setHealth: (health) => set({ health }),
  setReadyState: (rs) => set({ readyState: rs }),
  setModalOpen: (open) => set({ modalOpen: open }),

  noteMessage: (type, data) => {
    const now = Date.now();
    const prev = get().counters;
    const byType = { ...(prev.byType || {}) } as Record<string, number>;
    const key = typeof type === 'string' ? type : 'unknown';
    byType[key] = (byType[key] || 0) + 1;

    const next: Partial<WsState> = {
      counters: { byType, total: (prev.total || 0) + 1 },
      lastMessageAt: now,
      // first successful message → health ok
      health: 'ok',
    };

    // Track last genai/artifact stream timeline
    if (key.startsWith('genai.stream.')) {
      const lg = { ...(get().lastGenai || {}) } as LastStreamDiag;
      if (data?.projectId && typeof data.projectId === 'string') lg.projectId = data.projectId;
      if (data?.streamId && typeof data.streamId === 'string') lg.streamId = data.streamId;
      if (key === 'genai.stream.start' || key === 'genai.stream.research.start') lg.lastStartAt = now;
      if (key === 'genai.stream.chunk') { lg.lastChunkAt = now; lg.chunks = (lg.chunks || 0) + 1; }
      if (key === 'genai.stream.done' || key === 'genai.stream.research.done') lg.lastDoneAt = now;
      if (key === 'genai.stream.error' || key === 'genai.stream.research.error') lg.errors = (lg.errors || 0) + 1;
      next.lastGenai = lg;
    }
    if (key.startsWith('artifact.')) {
      const la = { ...(get().lastArtifact || {}) } as LastStreamDiag;
      if (data?.projectId && typeof data.projectId === 'string') la.projectId = data.projectId;
      if (data?.streamId && typeof data.streamId === 'string') la.streamId = data.streamId;
      if (key === 'artifact.generation.started') la.lastStartAt = now;
      if (key === 'artifact.section.chunk' || key === 'artifact.assembly.chunk') { la.lastChunkAt = now; la.chunks = (la.chunks || 0) + 1; }
      if (key === 'artifact.generation.completed' || key === 'artifact.assembly.completed') la.lastDoneAt = now;
      if (key === 'artifact.generation.error') la.errors = (la.errors || 0) + 1;
      next.lastArtifact = la;
    }
    set(next as any);
  },

  getDiagnostics: () => {
    const s = get();
    const diag = {
      wsUrl: s.wsUrl,
      tenantId: s.tenantId,
      subscribedProjectId: s.subscribedProjectId,
      readyState: s.readyState,
      connected: s.connected,
      health: s.health,
      lastOpenAt: s.lastOpenAt,
      lastCloseAt: s.lastCloseAt,
      lastError: s.lastError,
      lastMessageAt: s.lastMessageAt,
      counters: s.counters,
      lastGenai: s.lastGenai,
      lastArtifact: s.lastArtifact,
      ts: Date.now(),
    };
    try { return JSON.stringify(diag, null, 2); } catch { return JSON.stringify(diag); }
  },
}));

