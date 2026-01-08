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
        const rawLine = typeof data?.line === 'string' ? data.line : '';
        const level = typeof data?.level === 'string' ? data.level.toLowerCase() : '';
        icon = level === 'warn' || level === 'error' ? 'error' : 'scout';

        const edgeVerb = (et: string): string => {
          const u = (et || '').toUpperCase().trim();
          switch (u) {
            case 'SUPPLIES_TO':
              return 'supplies to';
            case 'PRODUCES':
              return 'produces';
            case 'TRANSFORMS_INTO':
              return 'transforms into';
            case 'DEPENDS_ON':
              return 'depends on';
            case 'REQUIRES':
              return 'requires';
            case 'USES':
              return 'uses';
            case 'NEEDS':
              return 'needs';
            case 'OWNS':
              return 'owns';
            case 'MANAGES':
              return 'manages';
            case 'CONTROLS':
              return 'controls';
            case 'INVESTS_IN':
              return 'invests in';
            default:
              return u ? u.toLowerCase().replace(/_/g, ' ') : 'linked to';
          }
        };

        const typeName = (t: string): string => {
          const u = (t || '').toUpperCase().trim();
          if (!u) return 'Entity';
          return u.toLowerCase().replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
        };

        const prettyCP = (cp: string): string => {
          const c = String(cp || '').trim();
          if (!c) return '';
          const name = getEntityName(c);
          return name ? name : c;
        };

        const fmt = (() => {
          const l = String(rawLine || '').trim();
          if (!l) return null as null | { title: string; description?: string; text: string };

          // Supply chain parse summary
          {
            const m = l.match(/^\[SupplyChainParse\]\s+chains=(\d+)\s+nodes=(\d+)\s+edges=(\d+)\s+fragments=(\d+)/i);
            if (m) {
              const chains = parseInt(m[1] || '0', 10) || 0;
              const nodesN = parseInt(m[2] || '0', 10) || 0;
              const edgesN = parseInt(m[3] || '0', 10) || 0;
              const fragsN = parseInt(m[4] || '0', 10) || 0;
              const title = 'Supply chain extracted';
              const description = `${chains} chain${chains === 1 ? '' : 's'} • ${nodesN} node${nodesN === 1 ? '' : 's'} • ${edgesN} link${edgesN === 1 ? '' : 's'} • ${fragsN} entity${fragsN === 1 ? '' : 'ies'}`;
              return { title, description: short(description), text: title };
            }
          }

          // Supply chain parse error
          {
            const m = l.match(/^\[SupplyChainParse\]\s+failed.*?:\s*(.+)$/i);
            if (m) {
              const title = 'Supply chain parsing failed';
              return { title, description: short(m[1] || ''), text: title };
            }
          }

          // Fragment create/update
          {
            const m = l.match(/^\[SupplyChainFragmentCreate\]\s+(OK|FAILED)\s+canonicalPath=([^\s]+)(?:\s+id=([^\s]+))?(?:\s+type=([A-Z_]+))?/i);
            if (m) {
              const ok = String(m[1] || '').toUpperCase() === 'OK';
              const cp = String(m[2] || '').trim();
              const t = String(m[4] || '').trim();
              const title = ok ? 'Entity added' : 'Entity creation failed';
              const description = ok
                ? `${typeName(t)} • ${prettyCP(cp)}`
                : `${prettyCP(cp)} • ${short(l)}`;
              return { title, description: short(description), text: title };
            }
          }
          {
            const m = l.match(/^\[SupplyChainFragmentUpdate\]\s+(OK|FAILED)\s+canonicalPath=([^\s]+)(?:\s+id=([^\s]+))?/i);
            if (m) {
              const ok = String(m[1] || '').toUpperCase() === 'OK';
              const cp = String(m[2] || '').trim();
              const title = ok ? 'Entity updated' : 'Entity update failed';
              const description = ok ? prettyCP(cp) : `${prettyCP(cp)} • ${short(l)}`;
              return { title, description: short(description), text: title };
            }
          }

          // Edge create
          {
            const m = l.match(/^\[SupplyChainEdgeCreate\]\s+(OK|FAILED)\s+([^\s]+)\s+-\[(\w+)\]->\s+([^\s]+)/i);
            if (m) {
              const ok = String(m[1] || '').toUpperCase() === 'OK';
              const src = String(m[2] || '').trim();
              const et = String(m[3] || '').trim();
              const dst = String(m[4] || '').trim();
              const title = ok ? 'Relationship added' : 'Relationship creation failed';
              const description = `${prettyCP(src)} ${edgeVerb(et)} ${prettyCP(dst)}`;
              return { title, description: short(description), text: title };
            }
          }

          // Summary
          {
            const m = l.match(/^\[SupplyChainSummary\]\s+fragments\(created=(\d+)\s+updated=(\d+)\s+err=(\d+)\)\s+edges\(created=(\d+)\s+dupes=(\d+)\s+err=(\d+)\s+reversed=(\d+)\s+penalized=(\d+)\)\s+gaps=(\d+)/i);
            if (m) {
              const createdFrags = parseInt(m[1] || '0', 10) || 0;
              const updatedFrags = parseInt(m[2] || '0', 10) || 0;
              const createdEdges = parseInt(m[4] || '0', 10) || 0;
              const gaps = parseInt(m[9] || '0', 10) || 0;
              const title = 'Supply chain saved';
              const parts = [
                createdFrags ? `+${createdFrags} new` : null,
                updatedFrags ? `${updatedFrags} updated` : null,
                createdEdges ? `+${createdEdges} links` : null,
                gaps ? `${gaps} gap${gaps === 1 ? '' : 's'} identified` : null,
              ].filter(Boolean);
              const description = parts.length > 0 ? parts.join(' • ') : 'No changes';
              return { title, description: short(description), text: title };
            }
          }

          // Default fallback: strip technical prefix if present
          const cleaned = l.replace(/^\[[^\]]+\]\s*/, '');
          const title = 'Scout Activity';
          const description = short(cleaned || l);
          return { title, description, text: description };
        })();

        if (fmt) {
          text = fmt.text;
          humanReadable = { title: fmt.title, description: fmt.description };
        } else {
          const line = short(rawLine);
          text = line || 'Scout activity';
          humanReadable = { title: 'Scout Activity', description: line };
        }
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


