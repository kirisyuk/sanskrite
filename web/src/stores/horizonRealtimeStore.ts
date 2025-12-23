import { create } from 'zustand';

export type HorizonAssemblyPhase = 'decomposition' | 'cache_hit' | 'scouting' | 'propagation' | 'ready' | 'failed' | string;

export interface HorizonAssemblyState {
  phase?: HorizonAssemblyPhase;
  progress?: number;
  counters?: Record<string, any>;
  lastEventAt?: number;
  correlationId?: string;
  sessionId?: string;
  lensId?: string;
  projectId?: string;
}

export interface HorizonScoutJobState {
  jobId: string;
  targetCanonicalPath?: string;
  state?: string;
  progress?: number;
  hint?: string;
  logs: Array<{ ts?: string; level?: string; line: string }>;
  updatedAt?: number;
}

export interface HorizonNodeState {
  fragmentId?: string;
  canonicalPath?: string;
  status?: string;
  epistemicStatus?: string;
  version?: number;
  updatedAt?: number;
}

export interface HorizonEdgeState {
  edgeId: string;
  sourceFragmentId?: string;
  targetFragmentId?: string;
  type?: string;
  properties?: any;
  qualifiers?: any;
  updatedAt?: number;
}

export interface HorizonRealtimeEvent {
  id: string;
  ts: number;
  type: string;
  projectId?: string;
  lensId?: string;
  text: string;
  data?: any;
}

interface HorizonRealtimeStore {
  assembly: HorizonAssemblyState;
  jobs: Record<string, HorizonScoutJobState>;
  nodes: Record<string, HorizonNodeState>; // key: fragmentId || canonicalPath
  edges: Record<string, HorizonEdgeState>; // key: edgeId
  events: HorizonRealtimeEvent[];

  handleWsEvent: (type: string, data: any) => void;
  reset: () => void;
  resetEvents: () => void;
}

function keyForNode(data: any): string {
  const fid = typeof data?.fragmentId === 'string' ? data.fragmentId : '';
  const cp = typeof data?.canonicalPath === 'string' ? data.canonicalPath : '';
  return fid || cp || 'unknown';
}

export const useHorizonRealtimeStore = create<HorizonRealtimeStore>((set, get) => ({
  assembly: {},
  jobs: {},
  nodes: {},
  edges: {},
  events: [],

  reset: () => set({ assembly: {}, jobs: {}, nodes: {}, edges: {} }),
  resetEvents: () => set({ events: [] }),

  handleWsEvent: (type, data) => {
    const now = Date.now();

    // Append a compact event message for UI (bounded).
    try {
      const projectId = typeof data?.projectId === 'string' ? data.projectId : undefined;
      const lensId = typeof data?.lensId === 'string' ? data.lensId : undefined;
      const mk = (text: string): HorizonRealtimeEvent => ({
        id: `${now}-${Math.random().toString(16).slice(2)}`,
        ts: now,
        type,
        projectId,
        lensId,
        text,
        data,
      });

      const short = (s: any) => {
        const t = typeof s === 'string' ? s : '';
        return t.length > 220 ? t.slice(0, 217) + '...' : t;
      };

      let text = type;
      if (type.startsWith('horizon.assembly.')) {
        const phase = typeof data?.phase === 'string' ? data.phase : '';
        const progress = typeof data?.progress === 'number' ? `${Math.round(data.progress * 100)}%` : '';
        text = `assembly${phase ? `.${phase}` : ''}${progress ? ` (${progress})` : ''}`;
      } else if (type === 'horizon.scout.job.log') {
        const jobId = typeof data?.jobId === 'string' ? data.jobId : '';
        const line = short(data?.line);
        text = `${jobId ? `job ${jobId}: ` : ''}${line || 'log'}`;
      } else if (type === 'horizon.scout.job.state' || type === 'horizon.scout.job.completed' || type === 'horizon.scout.job.created') {
        const jobId = typeof data?.jobId === 'string' ? data.jobId : '';
        const st = typeof data?.state === 'string' ? data.state : '';
        const target = typeof data?.targetCanonicalPath === 'string' ? data.targetCanonicalPath : '';
        text = `${jobId ? `job ${jobId}` : 'job'}${st ? ` ${st}` : ''}${target ? ` • ${target}` : ''}`;
      } else if (type === 'horizon.graph.node.upserted') {
        const cp = typeof data?.canonicalPath === 'string' ? data.canonicalPath : '';
        const st = typeof data?.status === 'string' ? data.status : '';
        text = `node${st ? ` ${st}` : ''}${cp ? ` • ${cp}` : ''}`;
      } else if (type === 'horizon.graph.edge.upserted') {
        const et = typeof data?.type === 'string' ? data.type : '';
        const src = typeof data?.sourceFragmentId === 'string' ? data.sourceFragmentId : '';
        const dst = typeof data?.targetFragmentId === 'string' ? data.targetFragmentId : '';
        text = `edge${et ? ` ${et}` : ''}${src && dst ? ` • ${src} → ${dst}` : ''}`;
      } else if (type.startsWith('horizon.propagation.')) {
        text = `propagation${typeof data?.nodesUpdated === 'number' ? ` (${data.nodesUpdated} nodes)` : ''}`;
      }

      set((s) => ({
        events: [...(s.events || []), mk(text)].slice(-400),
      }));
    } catch {
      // ignore
    }

    // Assembly orchestration
    if (type === 'horizon.assembly.started' || type === 'horizon.assembly.phase' || type === 'horizon.assembly.progress' || type === 'horizon.assembly.ready' || type === 'horizon.assembly.failed') {
      set((s) => ({
        assembly: {
          ...s.assembly,
          phase: data?.phase ?? s.assembly.phase,
          progress: typeof data?.progress === 'number' ? data.progress : s.assembly.progress,
          counters: data?.counters ?? s.assembly.counters,
          correlationId: data?.correlationId ?? s.assembly.correlationId,
          sessionId: data?.sessionId ?? s.assembly.sessionId,
          lensId: data?.lensId ?? s.assembly.lensId,
          projectId: data?.projectId ?? s.assembly.projectId,
          lastEventAt: now,
        },
      }));
      return;
    }

    // Scout jobs
    if (type === 'horizon.scout.job.created' || type === 'horizon.scout.job.state' || type === 'horizon.scout.job.completed' || type === 'horizon.scout.job.log') {
      const jobId = typeof data?.jobId === 'string' ? data.jobId : '';
      if (!jobId) return;

      set((s) => {
        const prev = s.jobs[jobId] || { jobId, logs: [] };
        const next: HorizonScoutJobState = {
          ...prev,
          jobId,
          targetCanonicalPath: data?.targetCanonicalPath ?? prev.targetCanonicalPath,
          state: data?.state ?? prev.state,
          progress: typeof data?.progress === 'number' ? data.progress : prev.progress,
          hint: data?.hint ?? prev.hint,
          updatedAt: now,
          logs: prev.logs,
        };

        if (type === 'horizon.scout.job.log') {
          const line = typeof data?.line === 'string' ? data.line : '';
          if (line) {
            next.logs = [...(prev.logs || []), { ts: data?.ts, level: data?.level, line }].slice(-200);
          }
        }

        return { jobs: { ...s.jobs, [jobId]: next } };
      });
      return;
    }

    // Graph nodes
    if (type === 'horizon.graph.node.upserted') {
      const k = keyForNode(data);
      if (!k) return;
      set((s) => ({
        nodes: {
          ...s.nodes,
          [k]: {
            ...s.nodes[k],
            fragmentId: data?.fragmentId ?? s.nodes[k]?.fragmentId,
            canonicalPath: data?.canonicalPath ?? s.nodes[k]?.canonicalPath,
            status: data?.status ?? s.nodes[k]?.status,
            epistemicStatus: data?.epistemicStatus ?? s.nodes[k]?.epistemicStatus,
            version: typeof data?.version === 'number' ? data.version : s.nodes[k]?.version,
            updatedAt: now,
          },
        },
      }));
      return;
    }

    // Graph edges
    if (type === 'horizon.graph.edge.upserted') {
      const edgeId = typeof data?.edgeId === 'string' ? data.edgeId : '';
      if (!edgeId) return;
      set((s) => ({
        edges: {
          ...s.edges,
          [edgeId]: {
            ...(s.edges[edgeId] || { edgeId }),
            edgeId,
            sourceFragmentId: data?.sourceFragmentId ?? s.edges[edgeId]?.sourceFragmentId,
            targetFragmentId: data?.targetFragmentId ?? s.edges[edgeId]?.targetFragmentId,
            type: data?.type ?? s.edges[edgeId]?.type,
            properties: data?.properties ?? s.edges[edgeId]?.properties,
            qualifiers: data?.qualifiers ?? s.edges[edgeId]?.qualifiers,
            updatedAt: now,
          },
        },
      }));
      return;
    }

    // Propagation + lens updates can be treated as assembly progress hints for now
    if (type === 'horizon.propagation.started') {
      set((s) => ({ assembly: { ...s.assembly, phase: 'propagation', lastEventAt: now } }));
      return;
    }
    if (type === 'horizon.propagation.node_updated') {
      // Best-effort: bump activity timestamp; UI can infer progress from node upserts + job states
      set((s) => ({ assembly: { ...s.assembly, phase: 'propagation', lastEventAt: now } }));
      return;
    }
    if (type === 'horizon.propagation.completed') {
      // Best-effort: mark phase back to ready-ish; the actual ready event still comes from assembly.ready
      set((s) => ({
        assembly: {
          ...s.assembly,
          phase: s.assembly.phase === 'propagation' ? 'scouting' : s.assembly.phase,
          lastEventAt: now,
          counters: {
            ...(s.assembly.counters || {}),
            propagation: {
              nodesUpdated: data?.nodesUpdated,
              lensesAffected: data?.lensesAffected,
              sourceFragmentId: data?.sourceFragmentId,
              ts: data?.ts,
            },
          },
        },
      }));
      return;
    }
    if (type === 'horizon.lens.updated') {
      // no-op for now; LensCanvas can refetch via HTTP
      return;
    }
  },
}));


