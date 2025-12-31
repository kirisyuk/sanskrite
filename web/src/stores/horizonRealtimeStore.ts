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
  icon?: 'assembly' | 'scout' | 'node' | 'edge' | 'propagation' | 'success' | 'error' | 'info';
  humanReadable?: {
    title: string;
    description?: string;
    entityName?: string;
  };
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

      // Helper to extract human-readable name from canonical path
      const getEntityName = (canonicalPath: string): string => {
        if (!canonicalPath) return '';
        // Extract last meaningful part: /org/tesla -> tesla, /fac/tsmc/fab-21 -> fab-21
        const parts = canonicalPath.split('/').filter(p => p);
        if (parts.length === 0) return canonicalPath;
        const last = parts[parts.length - 1];
        // Capitalize first letter and replace dashes/underscores with spaces
        return last
          .replace(/[-_]/g, ' ')
          .replace(/\b\w/g, (l) => l.toUpperCase());
      };

      // Helper to get phase display name
      const getPhaseName = (phase: string): string => {
        const phaseMap: Record<string, string> = {
          'decomposition': 'Decomposing',
          'cache_hit': 'Loading from cache',
          'scouting': 'Scouting',
          'propagation': 'Propagating',
          'ready': 'Ready',
          'failed': 'Failed',
        };
        return phaseMap[phase] || phase.charAt(0).toUpperCase() + phase.slice(1);
      };

      // Helper to get state display name
      const getStateName = (state: string): string => {
        const stateMap: Record<string, string> = {
          'PENDING': 'Pending',
          'PLANNING': 'Planning',
          'EXECUTION': 'Executing',
          'EXTRACTION': 'Extracting',
          'VALIDATION': 'Validating',
          'RESOLUTION': 'Resolving',
          'VERIFIED': 'Verified',
          'FAILED': 'Failed',
          'COMPLETED': 'Completed',
        };
        return stateMap[state] || state;
      };

      let text = type;
      let icon: HorizonRealtimeEvent['icon'] = 'info';
      let humanReadable: HorizonRealtimeEvent['humanReadable'] | undefined;

      if (type.startsWith('horizon.assembly.')) {
        icon = type.includes('failed') ? 'error' : type.includes('ready') ? 'success' : 'assembly';
        const phase = typeof data?.phase === 'string' ? data.phase : '';
        const progress = typeof data?.progress === 'number' ? `${Math.round(data.progress * 100)}%` : '';
        const phaseName = phase ? getPhaseName(phase) : 'Assembly';
        text = `${phaseName}${progress ? ` (${progress})` : ''}`;
        humanReadable = {
          title: phaseName,
          description: progress ? `Progress: ${progress}` : undefined,
        };
      } else if (type === 'horizon.scout.job.log') {
        icon = 'scout';
        const line = short(data?.line);
        text = line || 'Scout activity';
        humanReadable = {
          title: 'Scout Activity',
          description: line,
        };
      } else if (type === 'horizon.scout.job.created') {
        icon = 'scout';
        const target = typeof data?.targetCanonicalPath === 'string' ? data.targetCanonicalPath : '';
        const entityName = target ? getEntityName(target) : 'Unknown entity';
        text = `Scout job created for ${entityName}`;
        humanReadable = {
          title: 'Scout Job Created',
          description: target ? `Analyzing ${entityName}` : undefined,
          entityName: entityName,
        };
      } else if (type === 'horizon.scout.job.state') {
        icon = 'scout';
        const st = typeof data?.state === 'string' ? data.state : '';
        const target = typeof data?.targetCanonicalPath === 'string' ? data.targetCanonicalPath : '';
        const stateName = st ? getStateName(st) : 'Unknown state';
        const entityName = target ? getEntityName(target) : '';
        text = entityName ? `${stateName}: ${entityName}` : stateName;
        humanReadable = {
          title: stateName,
          description: entityName ? `Processing ${entityName}` : undefined,
          entityName: entityName,
        };
      } else if (type === 'horizon.scout.job.completed') {
        icon = 'success';
        const target = typeof data?.targetCanonicalPath === 'string' ? data.targetCanonicalPath : '';
        const entityName = target ? getEntityName(target) : 'Unknown entity';
        text = `Scout job completed for ${entityName}`;
        humanReadable = {
          title: 'Scout Job Completed',
          description: `Finished analyzing ${entityName}`,
          entityName: entityName,
        };
      } else if (type === 'horizon.graph.node.upserted') {
        icon = 'node';
        const cp = typeof data?.canonicalPath === 'string' ? data.canonicalPath : '';
        const st = typeof data?.status === 'string' ? data.status : '';
        const entityName = cp ? getEntityName(cp) : 'Unknown node';
        const statusText = st === 'cached' ? 'loaded from cache' : st === 'verified' ? 'verified' : st || 'updated';
        text = `${entityName} ${statusText}`;
        humanReadable = {
          title: `${entityName} ${statusText}`,
          description: cp ? `Entity: ${cp}` : undefined,
          entityName: entityName,
        };
      } else if (type === 'horizon.graph.edge.upserted') {
        icon = 'edge';
        const src = typeof data?.sourceFragmentId === 'string' ? data.sourceFragmentId : '';
        const dst = typeof data?.targetFragmentId === 'string' ? data.targetFragmentId : '';
        const edgeType = typeof data?.type === 'string' ? data.type : 'Connection';
        // Format edge type: "SUPPLIES_TO" -> "Supplies To"
        const formattedType = edgeType.replace(/_/g, ' ').toLowerCase().replace(/\b\w/g, (c: string) => c.toUpperCase());
        text = formattedType;
        humanReadable = {
          title: formattedType, // Just the type, UI will render "Link <chip> created"
          description: src && dst ? `${src} → ${dst}` : 'Connection details', // Full canonical paths
        };
      } else if (type.startsWith('horizon.propagation.')) {
        icon = 'propagation';
        const nodesUpdated = typeof data?.nodesUpdated === 'number' ? data.nodesUpdated : 0;
        text = `Propagation: ${nodesUpdated} node${nodesUpdated !== 1 ? 's' : ''} updated`;
        humanReadable = {
          title: 'Propagation',
          description: nodesUpdated > 0 ? `Updated ${nodesUpdated} node${nodesUpdated !== 1 ? 's' : ''}` : undefined,
        };
      }

      const event: HorizonRealtimeEvent = mk(text);
      if (icon) event.icon = icon;
      if (humanReadable) event.humanReadable = humanReadable;

      set((s) => ({
        events: [...(s.events || []), event].slice(-400),
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


