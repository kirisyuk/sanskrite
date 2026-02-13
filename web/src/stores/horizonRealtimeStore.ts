import { create } from 'zustand';
import { isSphereCanonicalPath, isSphereEntity } from '../lib/horizonSphere';
import { humanizeCanonicalPath } from '../lib/uiUtils';

export type HorizonAssemblyStage =
  | 'decomposition'
  | 'cache_hit'
  | 'scouting'
  | 'propagation'
  | 'ready'
  | 'failed'
  | string;

/** @deprecated Use `HorizonAssemblyStage` instead. */
export type HorizonAssemblyPhase = HorizonAssemblyStage;

export interface HorizonAssemblyState {
  /** Preferred field name (new). */
  stage?: HorizonAssemblyStage;
  /** @deprecated Backward compat for older payloads/UI. */
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
  /** Optional label/name provided by backend for better UX (esp. /domain/... nodes). */
  label?: string;
  /** Optional fragment type (e.g. "DOMAIN"). */
  type?: string;
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
  icon?:
    | 'assembly'
    | 'scout'
    | 'node'
    | 'edge'
    | 'propagation'
    | 'success'
    | 'error'
    | 'info';
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

export const useHorizonRealtimeStore = create<HorizonRealtimeStore>(
  (set, get) => ({
    assembly: {},
    jobs: {},
    nodes: {},
    edges: {},
    events: [],

    reset: () => set({ assembly: {}, jobs: {}, nodes: {}, edges: {} }),
    resetEvents: () => set({ events: [] }),

    handleWsEvent: (type, dataIn) => {
      const now = Date.now();
      let data = dataIn;
      const resolveNodePath = (id: string): string => {
        const nodes = get().nodes || {};
        const node = nodes[id];
        return typeof node?.canonicalPath === 'string'
          ? node.canonicalPath
          : id;
      };

      if (type === 'horizon.graph.node.upserted') {
        if (isSphereEntity(data?.type, data?.canonicalPath)) {
          const k = keyForNode(data);
          if (!k) return;
          set((s) => {
            if (!s.nodes[k]) return s;
            const next = { ...s.nodes };
            delete next[k];
            return { nodes: next };
          });
          return;
        }
      }

      if (type === 'horizon.graph.edge.upserted') {
        const resolveNodePath = (id: string): string => {
          const nodes = get().nodes || {};
          const node = nodes[id];
          return typeof node?.canonicalPath === 'string'
            ? node.canonicalPath
            : id;
        };
        const src =
          typeof data?.sourceCanonicalPath === 'string' &&
          data.sourceCanonicalPath
            ? data.sourceCanonicalPath
            : typeof data?.sourceFragmentId === 'string'
              ? resolveNodePath(data.sourceFragmentId)
              : '';
        const dst =
          typeof data?.targetCanonicalPath === 'string' &&
          data.targetCanonicalPath
            ? data.targetCanonicalPath
            : typeof data?.targetFragmentId === 'string'
              ? resolveNodePath(data.targetFragmentId)
              : '';
        if (isSphereCanonicalPath(src) || isSphereCanonicalPath(dst)) {
          const edgeId = typeof data?.edgeId === 'string' ? data.edgeId : '';
          if (edgeId) {
            set((s) => {
              if (!s.edges[edgeId]) return s;
              const next = { ...s.edges };
              delete next[edgeId];
              return { edges: next };
            });
          }
          return;
        }
      }

      // Append a compact event message for UI (bounded).
      try {
        const projectId =
          typeof data?.projectId === 'string' ? data.projectId : undefined;
        const lensId =
          typeof data?.lensId === 'string' ? data.lensId : undefined;
        const mk = (text: string, dataOverride?: any): HorizonRealtimeEvent => ({
          id: `${now}-${Math.random().toString(16).slice(2)}`,
          ts: now,
          type,
          projectId,
          lensId,
          text,
          data: dataOverride ?? data,
        });

        const short = (s: any) => {
          const t = typeof s === 'string' ? s : '';
          return t.length > 220 ? t.slice(0, 217) + '...' : t;
        };

        // Helper to extract human-readable name from canonical path
        const getEntityName = (canonicalPath: string): string => {
          const s = String(canonicalPath || '').trim();
          if (!s) return '';
          // Centralize canonical-path-to-title fallback (DOMAIN-aware).
          return humanizeCanonicalPath(s) || s;
        };

        // Helper to get stage display name
        const getStageName = (stage: string): string => {
          const stageMap: Record<string, string> = {
            decomposition: 'Decomposing',
            cache_hit: 'Loading from cache',
            scouting: 'Scouting',
            propagation: 'Propagating',
            ready: 'Ready',
            failed: 'Failed',
          };
          return (
            stageMap[stage] || stage.charAt(0).toUpperCase() + stage.slice(1)
          );
        };

        // Helper to get state display name
        const getStateName = (state: string): string => {
          const stateMap: Record<string, string> = {
            PENDING: 'Pending',
            PLANNING: 'Planning',
            EXECUTION: 'Executing',
            EXTRACTION: 'Extracting',
            VALIDATION: 'Validating',
            RESOLUTION: 'Resolving',
            VERIFIED: 'Verified',
            FAILED: 'Failed',
            COMPLETED: 'Completed',
          };
          return stateMap[state] || state;
        };

        const formatTypeLabel = (raw: string): string => {
          const t = String(raw || '').trim();
          if (!t) return '';
          return t
            .replace(/_/g, ' ')
            .toLowerCase()
            .replace(/\b\w/g, (c) => c.toUpperCase());
        };

        const relationVerb = (raw: string): string => {
          const u = String(raw || '')
            .toUpperCase()
            .trim();
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
            case 'INFLUENCES':
              return 'influences';
            case 'AFFECTS':
              return 'affects';
            case 'FOCUSES_ON':
              return 'focuses on';
            case 'INVOLVES_ACTOR':
              return 'involves';
            case 'IN_GEO_SCOPE':
              return 'in geo scope';
            case 'ABOUT':
              return 'about';
            case 'RELATED_DOMAIN':
              return 'related to';
            case 'BROADENS_TO':
              return 'broadens to';
            case 'NARROWS_TO':
              return 'narrows to';
            case 'IMPACTS':
              return 'impacts';
            case 'REGULATES':
              return 'regulates';
            case 'COMPETES_WITH':
              return 'competes with';
            case 'PARTNERS_WITH':
              return 'partners with';
            case 'COOPERATES_WITH':
              return 'cooperates with';
            default:
              return u ? u.toLowerCase().replace(/_/g, ' ') : 'linked to';
          }
        };

        let text = type;
        let icon: HorizonRealtimeEvent['icon'] = 'info';
        let humanReadable: HorizonRealtimeEvent['humanReadable'] | undefined;

        if (type.startsWith('horizon.assembly.')) {
          icon = type.includes('failed')
            ? 'error'
            : type.includes('ready')
              ? 'success'
              : 'assembly';
          const stage =
            typeof data?.stage === 'string'
              ? data.stage
              : typeof data?.phase === 'string'
                ? data.phase
                : '';
          const progress =
            typeof data?.progress === 'number'
              ? `${Math.round(data.progress * 100)}%`
              : '';
          const stageName = stage ? getStageName(stage) : 'Assembly';
          text = `${stageName}${progress ? ` (${progress})` : ''}`;
          humanReadable = {
            title: stageName,
            description: progress ? `Progress: ${progress}` : undefined,
          };
        } else if (type.startsWith('horizon.intent.')) {
          icon = 'info';
          if (type === 'horizon.intent.preflight') {
            const intent = data?.intent || {};
            const domainsN = Array.isArray(intent?.domains) ? intent.domains.length : 0;
            const anchorsN = Array.isArray(intent?.anchors) ? intent.anchors.length : 0;
            const mode =
              typeof data?.recommendation?.mode === 'string'
                ? data.recommendation.mode
                : '';
            const modeLabel =
              mode === 'just_open'
                ? 'Just open'
                : mode === 'research_first'
                  ? 'Research'
                  : '';

            const parts: string[] = [];
            parts.push(`${domainsN} domain${domainsN === 1 ? '' : 's'}`);
            parts.push(`${anchorsN} anchor${anchorsN === 1 ? '' : 's'}`);
            if (modeLabel) parts.push(`recommended: ${modeLabel}`);

            text = `Intent resolved — ${parts.join(' • ')}`;
            humanReadable = {
              title: 'Intent resolved',
              description: parts.join(' • '),
            };
          } else {
            const hr = data?.humanReadable;
            if (hr && typeof hr?.title === 'string' && hr.title.trim()) {
              const title = hr.title.trim();
              const description =
                typeof hr?.description === 'string' ? hr.description.trim() : '';
              text = description ? `${title} — ${short(description)}` : title;
              humanReadable = {
                title,
                description: description ? short(description) : undefined,
                entityName:
                  typeof hr?.entityName === 'string' ? hr.entityName : undefined,
              };
            } else {
              text = 'Intent';
              humanReadable = { title: 'Intent' };
            }
          }
        } else if (type.startsWith('horizon.domain.research.')) {
          icon = 'info';
          const hr = data?.humanReadable;
          if (hr && typeof hr?.title === 'string' && hr.title.trim()) {
            const title = hr.title.trim();
            const description =
              typeof hr?.description === 'string' ? hr.description.trim() : '';
            text = description ? `${title} — ${short(description)}` : title;
            humanReadable = {
              title,
              description: description ? short(description) : undefined,
              entityName:
                typeof hr?.entityName === 'string' ? hr.entityName : undefined,
            };
          } else {
            text = 'Domain research';
            humanReadable = { title: 'Domain research' };
          }
        } else if (type.startsWith('horizon.domain.base_enrichment.')) {
          icon = type.includes('completed') ? 'success' : 'info';
          const hr = data?.humanReadable;
          if (hr && typeof hr?.title === 'string' && hr.title.trim()) {
            const title = hr.title.trim();
            const description =
              typeof hr?.description === 'string' ? hr.description.trim() : '';
            text = description ? `${title} — ${short(description)}` : title;
            humanReadable = {
              title,
              description: description ? short(description) : undefined,
              entityName:
                typeof hr?.entityName === 'string' ? hr.entityName : undefined,
            };
          } else {
            text = 'Base enrichment';
            humanReadable = { title: 'Base enrichment' };
          }
        } else if (type.startsWith('horizon.domain.strategic_projection.')) {
          icon = type.includes('completed') ? 'success' : 'info';
          const hr = data?.humanReadable;
          if (hr && typeof hr?.title === 'string' && hr.title.trim()) {
            const title = hr.title.trim();
            const description =
              typeof hr?.description === 'string' ? hr.description.trim() : '';
            text = description ? `${title} — ${short(description)}` : title;
            humanReadable = {
              title,
              description: description ? short(description) : undefined,
              entityName:
                typeof hr?.entityName === 'string' ? hr.entityName : undefined,
            };
          } else {
            text = 'Strategic projection';
            humanReadable = { title: 'Strategic projection' };
          }
        } else if (type.startsWith('horizon.domain.overlay.')) {
          icon = type.includes('completed') ? 'success' : 'info';
          const hr = data?.humanReadable;
          if (hr && typeof hr?.title === 'string' && hr.title.trim()) {
            const title = hr.title.trim();
            const description =
              typeof hr?.description === 'string' ? hr.description.trim() : '';
            text = description ? `${title} — ${short(description)}` : title;
            humanReadable = {
              title,
              description: description ? short(description) : undefined,
              entityName:
                typeof hr?.entityName === 'string' ? hr.entityName : undefined,
            };
          } else {
            text = 'Domain overlay';
            humanReadable = { title: 'Domain overlay' };
          }
        } else if (type.startsWith('horizon.domain.contract.')) {
          if (type.includes('.failed') || type.includes('.fallback')) {
            icon = 'error';
          } else if (type.includes('.completed') || type.includes('.passed')) {
            icon = 'success';
          } else {
            icon = 'info';
          }
          const hr = data?.humanReadable;
          if (hr && typeof hr?.title === 'string' && hr.title.trim()) {
            const title = hr.title.trim();
            const description =
              typeof hr?.description === 'string' ? hr.description.trim() : '';
            text = description ? `${title} — ${short(description)}` : title;
            humanReadable = {
              title,
              description: description ? short(description) : undefined,
              entityName:
                typeof hr?.entityName === 'string' ? hr.entityName : undefined,
            };
          } else {
            const raw = String(type || '').replace('horizon.domain.contract.', '');
            const title = raw
              ? raw
                  .replace(/\./g, ' ')
                  .replace(/\b\w/g, (c) => c.toUpperCase())
              : 'Domain contract';
            text = title;
            humanReadable = { title };
          }
        } else if (type === 'horizon.scout.job.log') {
          const rawLine = typeof data?.line === 'string' ? data.line : '';
          const level =
            typeof data?.level === 'string' ? data.level.toLowerCase() : '';
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
              case 'FOCUSES_ON':
                return 'focuses on';
              case 'INVOLVES_ACTOR':
                return 'involves';
              case 'IN_GEO_SCOPE':
                return 'in geo scope';
              case 'ABOUT':
                return 'about';
              case 'RELATED_DOMAIN':
                return 'related to';
              case 'BROADENS_TO':
                return 'broadens to';
              case 'NARROWS_TO':
                return 'narrows to';
              case 'AFFECTS':
                return 'affects';
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
            return u
              .toLowerCase()
              .replace(/_/g, ' ')
              .replace(/\b\w/g, (c) => c.toUpperCase());
          };

          const prettyCP = (cp: string): string => {
            const c = String(cp || '').trim();
            if (!c) return '';
            const name = getEntityName(c);
            return name ? name : c;
          };

          const fmt = (() => {
            const l = String(rawLine || '').trim();
            if (!l)
              return null as null | {
                title: string;
                description?: string;
                text: string;
                canonicalPath?: string;
              };

            // Supply chain parse summary
            {
              const m = l.match(
                /^\[SupplyChainParse\]\s+chains=(\d+)\s+nodes=(\d+)\s+edges=(\d+)\s+fragments=(\d+)/i,
              );
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

            // Research started
            {
              const m = l.match(
                /^\>\s*research started\s*(?:\(\s*missing\s*=\s*(\d+)\s*\))?/i,
              );
              if (m) {
                const missing = parseInt(m[1] || '0', 10) || 0;
                const title = 'Research started';
                const description =
                  missing > 0
                    ? `Gathering missing info (${missing} item${missing === 1 ? '' : 's'})`
                    : 'Gathering missing info';
                return { title, description: short(description), text: title };
              }
            }

            // Fragment create/update
            {
              const m = l.match(
                /^\[SupplyChainFragmentCreate\]\s+(OK|FAILED)\s+canonicalPath=([^\s]+)(?:\s+id=([^\s]+))?(?:\s+type=([A-Z_]+))?/i,
              );
              if (m) {
                const ok = String(m[1] || '').toUpperCase() === 'OK';
                const cp = String(m[2] || '').trim();
                const t = String(m[4] || '').trim();
                const title = ok ? 'Entity added' : 'Entity creation failed';
                const description = ok
                  ? `${typeName(t)} • ${prettyCP(cp)}`
                  : `${prettyCP(cp)} • ${short(l)}`;
                return { title, description: short(description), text: title, canonicalPath: cp || undefined };
              }
            }
            {
              const m = l.match(
                /^\[SupplyChainFragmentUpdate\]\s+(OK|FAILED)\s+canonicalPath=([^\s]+)(?:\s+id=([^\s]+))?/i,
              );
              if (m) {
                const ok = String(m[1] || '').toUpperCase() === 'OK';
                const cp = String(m[2] || '').trim();
                const title = ok ? 'Entity updated' : 'Entity update failed';
                const description = ok
                  ? prettyCP(cp)
                  : `${prettyCP(cp)} • ${short(l)}`;
                return { title, description: short(description), text: title, canonicalPath: cp || undefined };
              }
            }

            // Edge create
            {
              const m = l.match(
                /^\[SupplyChainEdgeCreate\]\s+(OK|FAILED)\s+([^\s]+)\s+-\[(\w+)\]->\s+([^\s]+)/i,
              );
              if (m) {
                const ok = String(m[1] || '').toUpperCase() === 'OK';
                const src = String(m[2] || '').trim();
                const et = String(m[3] || '').trim();
                const dst = String(m[4] || '').trim();
                const title = ok
                  ? 'Relationship added'
                  : 'Relationship creation failed';
                const description = `${prettyCP(src)} ${edgeVerb(et)} ${prettyCP(dst)}`;
                return { title, description: short(description), text: title };
              }
            }

            // Summary
            {
              const m = l.match(
                /^\[SupplyChainSummary\]\s+fragments\(created=(\d+)\s+updated=(\d+)\s+err=(\d+)\)\s+edges\(created=(\d+)\s+dupes=(\d+)\s+err=(\d+)\s+reversed=(\d+)\s+penalized=(\d+)\)\s+gaps=(\d+)/i,
              );
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
                  gaps
                    ? `${gaps} gap${gaps === 1 ? '' : 's'} identified`
                    : null,
                ].filter(Boolean);
                const description =
                  parts.length > 0 ? parts.join(' • ') : 'No changes';
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

            // IMPORTANT: for log-derived "Entity added/updated" events, show the created entity's canonicalPath,
            // not the scout job targetCanonicalPath (which is often /loc/{cc}/na/na for geo scope jobs).
            if (fmt.canonicalPath) {
              data = { ...(data || {}), canonicalPath: fmt.canonicalPath };
            }
          } else {
            const line = short(rawLine);
            text = line || 'Scout activity';
            humanReadable = { title: 'Scout Activity', description: line };
          }
        } else if (type === 'horizon.scout.job.created') {
          icon = 'scout';
          const target =
            typeof data?.targetCanonicalPath === 'string'
              ? data.targetCanonicalPath
              : '';
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
          const target =
            typeof data?.targetCanonicalPath === 'string'
              ? data.targetCanonicalPath
              : '';
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
          const target =
            typeof data?.targetCanonicalPath === 'string'
              ? data.targetCanonicalPath
              : typeof data?.canonicalPath === 'string'
                ? data.canonicalPath
              : '';
          const entityName = target ? getEntityName(target) : 'Unknown entity';
          text = `Scout job completed for ${entityName}`;
          humanReadable = {
            title: 'Scout Job Completed',
            description: `Finished analyzing ${entityName}`,
            entityName: entityName,
          };
        } else if (type === 'horizon.graph.node.upserted') {
          icon = 'node';
          const cp =
            typeof data?.canonicalPath === 'string' ? data.canonicalPath : '';
          const st = typeof data?.status === 'string' ? data.status : '';
          const entityName = cp ? getEntityName(cp) : 'Unknown node';
          const statusText =
            st === 'cached'
              ? 'loaded from cache'
              : st === 'verified'
                ? 'verified'
                : st || 'updated';
          text = `${entityName} ${statusText}`;
          humanReadable = {
            title: `${entityName} ${statusText}`,
            description: cp ? `Entity: ${cp}` : undefined,
            entityName: entityName,
          };
        } else if (type === 'horizon.ontology.fact.created') {
          icon = 'info';
          const kind = typeof data?.kind === 'string' ? data.kind : '';
          const fType = typeof data?.type === 'string' ? data.type : '';
          const title = typeof data?.title === 'string' ? data.title : '';
          const canonicalPaths = Array.isArray(data?.canonicalPaths)
            ? data.canonicalPaths
            : [];
          const actorPaths = Array.isArray(data?.actorPaths)
            ? data.actorPaths
            : [];
          const pickPaths = (arr: any[]) =>
            arr
              .map((v) => (typeof v === 'string' ? v.trim() : ''))
              .filter((v) => v);
          const paths = Array.from(
            new Set([...pickPaths(actorPaths), ...pickPaths(canonicalPaths)]),
          );
          const first = paths[0] ? getEntityName(paths[0]) : '';
          const second = paths[1] ? getEntityName(paths[1]) : '';
          const kindLabel = kind ? formatTypeLabel(kind) : 'Fact';
          const typeLabel = fType ? formatTypeLabel(fType) : '';

          let description = '';
          if (kind === 'relation') {
            icon = 'edge';
            const verb = relationVerb(fType);
            if (first && second) {
              description = `${first} ${verb} ${second}`;
            } else if (title) {
              description = title;
            } else if (typeLabel) {
              description = typeLabel;
            }
            text = description ? short(description) : 'Relationship created';
            humanReadable = {
              title: 'Relationship created',
              description: description ? short(description) : undefined,
              entityName: first || undefined,
            };
          } else {
            const headline = title || typeLabel || kindLabel;
            text = headline ? short(headline) : `${kindLabel} created`;
            description =
              title && typeLabel && title !== typeLabel
                ? `${typeLabel} • ${title}`
                : title || typeLabel;
            humanReadable = {
              title: `${kindLabel} created`,
              description: description ? short(description) : undefined,
              entityName: first || undefined,
            };
          }
        } else if (type === 'horizon.graph.edge.upserted') {
          icon = 'edge';
          const resolveNodePath = (id: string): string => {
            const nodes = get().nodes || {};
            const node = nodes[id];
            return typeof node?.canonicalPath === 'string'
              ? node.canonicalPath
              : id;
          };
          const src =
            typeof data?.sourceCanonicalPath === 'string' &&
            data.sourceCanonicalPath
              ? data.sourceCanonicalPath
              : typeof data?.sourceFragmentId === 'string'
                ? resolveNodePath(data.sourceFragmentId)
                : '';
          const dst =
            typeof data?.targetCanonicalPath === 'string' &&
            data.targetCanonicalPath
              ? data.targetCanonicalPath
              : typeof data?.targetFragmentId === 'string'
                ? resolveNodePath(data.targetFragmentId)
                : '';
          const edgeType =
            typeof data?.type === 'string' ? data.type : 'Connection';
          // Format edge type: "SUPPLIES_TO" -> "Supplies To"
          const formattedType = edgeType
            .replace(/_/g, ' ')
            .toLowerCase()
            .replace(/\b\w/g, (c: string) => c.toUpperCase());
          text = formattedType;
          humanReadable = {
            title: formattedType, // Just the type, UI will render "Link <chip> created"
            description: src && dst ? `${src} → ${dst}` : 'Connection details', // Full canonical paths
          };
        } else if (type === 'horizon.policy.check_complete') {
          // Policy check completed
          const violations =
            typeof data?.violationsFound === 'number'
              ? data.violationsFound
              : 0;
          const critical =
            typeof data?.criticalCount === 'number' ? data.criticalCount : 0;
          const autoFixable =
            typeof data?.autoFixableCount === 'number'
              ? data.autoFixableCount
              : 0;
          const subjectPath =
            typeof data?.subjectCanonicalPath === 'string'
              ? data.subjectCanonicalPath
              : '';
          const subjectLabel =
            typeof data?.subjectLabel === 'string' ? data.subjectLabel : '';
          const sample = Array.isArray(data?.violationsSample)
            ? data.violationsSample
            : [];
          const sampleText = sample
            .map((v: any) => {
              const t = typeof v?.type === 'string' ? v.type : '';
              const s = typeof v?.severity === 'string' ? v.severity : '';
              const d = typeof v?.description === 'string' ? v.description : '';
              const edgeType =
                typeof v?.edgeType === 'string' ? v.edgeType : '';
              const srcPath =
                typeof v?.sourcePath === 'string' ? v.sourcePath : '';
              const dstPath =
                typeof v?.targetPath === 'string' ? v.targetPath : '';
              const fragPath =
                typeof v?.fragmentPath === 'string' ? v.fragmentPath : '';
              const tag = [s, t].filter(Boolean).join(': ');
              const edgeHint =
                edgeType && srcPath && dstPath
                  ? `${srcPath} → ${dstPath} [${edgeType}]`
                  : '';
              const objHint = fragPath && !edgeHint ? fragPath : '';
              const hints = [edgeHint, objHint].filter(Boolean).join(' • ');
              const base = d ? `${tag ? `${tag} — ` : ''}${d}` : tag;
              return hints ? `${base} • ${hints}` : base;
            })
            .filter(Boolean)
            .slice(0, 3)
            .join(' | ');
          icon = violations === 0 ? 'success' : critical > 0 ? 'error' : 'info';
          if (violations === 0) {
            text = 'Policy check passed';
            humanReadable = {
              title: 'Policy Check Passed',
              description: 'No violations found',
            };
          } else {
            text = `Policy check: ${violations} violation${violations !== 1 ? 's' : ''} found`;
            const parts = [
              critical > 0 ? `${critical} critical` : null,
              autoFixable > 0 ? `${autoFixable} auto-fixable` : null,
            ].filter(Boolean);
            const subject =
              subjectLabel || subjectPath
                ? `on ${[subjectLabel, subjectPath].filter(Boolean).join(' ')}`
                : '';
            humanReadable = {
              title: 'Policy Check Complete',
              description: sampleText
                ? `${parts.length > 0 ? parts.join(' • ') : `${violations} violation${violations !== 1 ? 's' : ''}`}${subject ? ` • ${subject}` : ''} • ${short(sampleText)}`
                : parts.length > 0
                  ? parts.join(' • ')
                  : `${violations} violation${violations !== 1 ? 's' : ''}`,
            };
          }
        } else if (type === 'horizon.policy.violation_detected') {
          // Individual violation detected
          icon = 'error';
          const violationType =
            typeof data?.violationType === 'string' ? data.violationType : '';
          const severity =
            typeof data?.severity === 'string' ? data.severity : '';
          const description =
            typeof data?.description === 'string' ? data.description : '';
          const typeLabel = violationType
            .replace(/_/g, ' ')
            .toLowerCase()
            .replace(/\b\w/g, (c: string) => c.toUpperCase());
          text = description || `Violation: ${typeLabel}`;
          humanReadable = {
            title:
              severity === 'critical'
                ? 'Critical Violation'
                : 'Violation Detected',
            description: description || typeLabel,
          };
        } else if (type === 'horizon.policy.enforcement_progress') {
          // Enforcement job progress
          const status = typeof data?.status === 'string' ? data.status : '';
          const taskType =
            typeof data?.taskType === 'string' ? data.taskType : '';
          const msgText = typeof data?.message === 'string' ? data.message : '';
          const targetPath =
            typeof data?.targetCanonicalPath === 'string' &&
            data.targetCanonicalPath
              ? data.targetCanonicalPath
              : typeof data?.targetId === 'string'
                ? resolveNodePath(data.targetId)
                : '';
          const targetLabel =
            typeof data?.targetLabel === 'string' ? data.targetLabel : '';
          icon =
            status === 'completed'
              ? 'success'
              : status === 'failed'
                ? 'error'
                : 'info';
          const taskLabel = taskType
            .replace(/_/g, ' ')
            .toLowerCase()
            .replace(/\b\w/g, (c: string) => c.toUpperCase());
          const statusLabel = status.charAt(0).toUpperCase() + status.slice(1);
          text = `Enforcement: ${taskLabel} ${statusLabel}`;
          humanReadable = {
            title: `Enforcement ${statusLabel}`,
            description: msgText
              ? short(msgText)
              : `${taskLabel}${targetLabel || targetPath ? ` • ${[targetLabel, targetPath].filter(Boolean).join(' ')}` : ''}`,
          };
        } else if (type === 'horizon.policy.enforcement_batch_complete') {
          // Enforcement batch completed
          const completed =
            typeof data?.completed === 'number' ? data.completed : 0;
          const failed = typeof data?.failed === 'number' ? data.failed : 0;
          const total =
            typeof data?.totalJobs === 'number' ? data.totalJobs : 0;
          const jobsSample = Array.isArray(data?.jobsSample)
            ? data.jobsSample
            : [];
          icon = failed === 0 ? 'success' : 'error';
          text = `Enforcement complete: ${completed}/${total} fixes applied`;
          const parts = [
            completed > 0 ? `${completed} fixed` : null,
            failed > 0 ? `${failed} failed` : null,
          ].filter(Boolean);
          const details = jobsSample
            .map((j: any) => {
              const status = typeof j?.status === 'string' ? j.status : '';
              const taskType =
                typeof j?.taskType === 'string' ? j.taskType : '';
              const targetPath =
                typeof j?.targetCanonicalPath === 'string'
                  ? j.targetCanonicalPath
                  : '';
              const targetLabel =
                typeof j?.targetLabel === 'string' ? j.targetLabel : '';
              const err = typeof j?.error === 'string' ? j.error : '';
              const who = [targetLabel, targetPath].filter(Boolean).join(' ');
              const prefix = `${taskType}${who ? ` — ${who}` : ''}`;
              if (status === 'failed')
                return `${prefix}${err ? ` • failed: ${short(err)}` : ' • failed'}`;
              return prefix;
            })
            .filter(Boolean)
            .slice(0, 3)
            .join(' | ');
          humanReadable = {
            title: 'Enforcement Complete',
            description: `${parts.join(' • ') || 'No changes'}${details ? ` • ${short(details)}` : ''}`,
          };
        } else if (type.startsWith('horizon.propagation.')) {
          icon = 'propagation';
          const nodesUpdated =
            typeof data?.nodesUpdated === 'number' ? data.nodesUpdated : 0;
          text = `Propagation: ${nodesUpdated} node${nodesUpdated !== 1 ? 's' : ''} updated`;
          humanReadable = {
            title: 'Propagation',
            description:
              nodesUpdated > 0
                ? `Updated ${nodesUpdated} node${nodesUpdated !== 1 ? 's' : ''}`
                : undefined,
          };
        }

        // If backend provided a humanReadable envelope and we didn't format one above, use it.
        if (!humanReadable) {
          const hr = data?.humanReadable;
          if (hr && typeof hr?.title === 'string' && hr.title.trim()) {
            const title = hr.title.trim();
            const description =
              typeof hr?.description === 'string' ? hr.description.trim() : '';
            text = description ? `${title} — ${short(description)}` : title;
            humanReadable = {
              title,
              description: description ? short(description) : undefined,
              entityName:
                typeof hr?.entityName === 'string' ? hr.entityName : undefined,
            };
          }
        }

        const event: HorizonRealtimeEvent = mk(text);
        if (icon) event.icon = icon;
        if (humanReadable) event.humanReadable = humanReadable;

        set((s) => ({
          events: [...(s.events || []), event].slice(-1000),
        }));
      } catch {
        // ignore
      }

      // Assembly orchestration
      if (
        type === 'horizon.assembly.started' ||
        type === 'horizon.assembly.phase' || // legacy
        type === 'horizon.assembly.stage' || // preferred
        type === 'horizon.assembly.progress' ||
        type === 'horizon.assembly.ready' ||
        type === 'horizon.assembly.failed'
      ) {
        const stage =
          typeof data?.stage === 'string'
            ? data.stage
            : typeof data?.phase === 'string'
              ? data.phase
              : '';
        set((s) => ({
          assembly: {
            ...s.assembly,
            stage: stage || s.assembly.stage || s.assembly.phase,
            // Keep legacy field in sync for older UI code paths.
            phase: (stage || data?.phase) ?? s.assembly.phase,
            progress:
              typeof data?.progress === 'number'
                ? data.progress
                : s.assembly.progress,
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
      if (
        type === 'horizon.scout.job.created' ||
        type === 'horizon.scout.job.state' ||
        type === 'horizon.scout.job.completed' ||
        type === 'horizon.scout.job.log'
      ) {
        const jobId = typeof data?.jobId === 'string' ? data.jobId : '';
        if (!jobId) return;

        set((s) => {
          const prev = s.jobs[jobId] || { jobId, logs: [] };
          const next: HorizonScoutJobState = {
            ...prev,
            jobId,
            targetCanonicalPath:
              data?.targetCanonicalPath ?? prev.targetCanonicalPath,
            state: data?.state ?? prev.state,
            progress:
              typeof data?.progress === 'number'
                ? data.progress
                : prev.progress,
            hint: data?.hint ?? prev.hint,
            updatedAt: now,
            logs: prev.logs,
          };

          if (type === 'horizon.scout.job.log') {
            const line = typeof data?.line === 'string' ? data.line : '';
            if (line) {
              next.logs = [
                ...(prev.logs || []),
                { ts: data?.ts, level: data?.level, line },
              ].slice(-1000);
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
              type: data?.type ?? s.nodes[k]?.type,
              label: data?.label ?? s.nodes[k]?.label,
              status: data?.status ?? s.nodes[k]?.status,
              epistemicStatus:
                data?.epistemicStatus ?? s.nodes[k]?.epistemicStatus,
              version:
                typeof data?.version === 'number'
                  ? data.version
                  : s.nodes[k]?.version,
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
              sourceFragmentId:
                data?.sourceFragmentId ?? s.edges[edgeId]?.sourceFragmentId,
              targetFragmentId:
                data?.targetFragmentId ?? s.edges[edgeId]?.targetFragmentId,
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
        set((s) => ({
          assembly: { ...s.assembly, stage: 'propagation', phase: 'propagation', lastEventAt: now },
        }));
        return;
      }
      if (type === 'horizon.propagation.node_updated') {
        // Best-effort: bump activity timestamp; UI can infer progress from node upserts + job states
        set((s) => ({
          assembly: { ...s.assembly, stage: 'propagation', phase: 'propagation', lastEventAt: now },
        }));
        return;
      }
      if (type === 'horizon.propagation.completed') {
        // Best-effort: mark phase back to ready-ish; the actual ready event still comes from assembly.ready
        set((s) => ({
          assembly: {
            ...s.assembly,
            stage:
              (s.assembly.stage || s.assembly.phase) === 'propagation'
                ? 'scouting'
                : (s.assembly.stage || s.assembly.phase),
            // Keep legacy field in sync.
            phase:
              s.assembly.phase === 'propagation'
                ? 'scouting'
                : s.assembly.phase,
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
  }),
);
