import { create } from 'zustand';
import { isSphereCanonicalPath, isSphereEntity } from '../lib/horizonSphere';
import { humanizeCanonicalPath } from '../lib/uiUtils';

export type HorizonAssemblyStage =
  | 'decomposition'
  | 'cache_hit'
  | 'scouting'
  | 'intra_graph'
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
  sourceCanonicalPath?: string;
  targetCanonicalPath?: string;
  type?: string;
  properties?: any;
  qualifiers?: any;
  updatedAt?: number;
}

export interface HorizonArtifactState {
  id: string;
  lensId?: string;
  canonicalKey?: string;
  displayName?: string;
  anchorSupplier?: string;
  anchorCustomer?: string;
  productAxis?: string;
  nodeRefs?: string[];
  flowRefs?: string[];
  zoneRefs?: string[];
  affectedZoneRefs?: string[];
  legCount?: number;
  bindingScore?: number;
  pathStressScore?: number;
  riskOverlay?: any;
  payload?: any;
  updatedAt?: number;
}

export interface HorizonIdentifyStressJobState {
  jobId: string;
  lensId?: string;
  projectId?: string;
  state?: 'started' | 'running' | 'propagating' | 'completed' | 'partial_success' | 'failed' | string;
  findingsApplied?: number;
  eventsPersisted?: number;
  affectedZonesCreated?: number;
  artifactsUpdated?: number;
  supplyLinksUpdated?: number;
  propagationErrors?: number;
  acceptedNodes?: number;
  acceptedArtifacts?: number;
  acceptedSupplyLinks?: number;
  touchedArtifactKeys?: string[];
  touchedNodeRefs?: string[];
  touchedSupplyLinks?: string[];
  selectedCanonicalPath?: string;
  requiresRefetch?: boolean;
  error?: string;
  errors?: string[];
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
  identifyJobs: Record<string, HorizonIdentifyStressJobState>;
  nodes: Record<string, HorizonNodeState>; // key: fragmentId || canonicalPath
  edges: Record<string, HorizonEdgeState>; // key: edgeId
  artifacts: Record<string, HorizonArtifactState>; // key: artifact id || canonicalKey
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

const MAX_NODES = 5000;
const MAX_EDGES = 10000;

function pruneNodes(nodes: Record<string, HorizonNodeState>): Record<string, HorizonNodeState> {
  const keys = Object.keys(nodes);
  if (keys.length <= MAX_NODES) return nodes;
  const sorted = keys.sort((a, b) => (nodes[b].updatedAt ?? 0) - (nodes[a].updatedAt ?? 0));
  const toKeep = sorted.slice(0, MAX_NODES);
  const next: Record<string, HorizonNodeState> = {};
  for (const k of toKeep) next[k] = nodes[k];
  return next;
}

function pruneEdges(edges: Record<string, HorizonEdgeState>): Record<string, HorizonEdgeState> {
  const keys = Object.keys(edges);
  if (keys.length <= MAX_EDGES) return edges;
  const sorted = keys.sort((a, b) => (edges[b].updatedAt ?? 0) - (edges[a].updatedAt ?? 0));
  const toKeep = sorted.slice(0, MAX_EDGES);
  const next: Record<string, HorizonEdgeState> = {};
  for (const k of toKeep) next[k] = edges[k];
  return next;
}

export const useHorizonRealtimeStore = create<HorizonRealtimeStore>(
  (set, get) => ({
    assembly: {},
    jobs: {},
    identifyJobs: {},
    nodes: {},
    edges: {},
    artifacts: {},
    events: [],

    reset: () => set({ assembly: {}, jobs: {}, identifyJobs: {}, nodes: {}, edges: {}, artifacts: {} }),
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
            intra_graph: 'Linking entities',
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
        } else if (type.startsWith('horizon.intra_graph.')) {
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
            };
          } else {
            text = 'Intra-graph link inference';
            humanReadable = { title: 'Intra-graph link inference' };
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
        } else if (type === 'horizon.supply_chain.depth.materialized') {
          icon = 'success';
          const depth = typeof data?.depth === 'number' ? data.depth : undefined;
          const chainsN =
            typeof data?.chainsN === 'number' ? data.chainsN : undefined;
          const stageRaw = typeof data?.stage === 'string' ? data.stage.trim() : '';
          const stage = stageRaw ? stageRaw : '';
          const directionRaw =
            typeof data?.direction === 'string' ? data.direction.trim() : '';
          const direction =
            directionRaw === 'upstream'
              ? 'upstream'
              : directionRaw === 'downstream'
                ? 'downstream'
                : directionRaw === 'both'
                  ? 'both'
                  : '';

          const parts: string[] = [];
          if (stage) parts.push(`stage=${stage}`);
          if (typeof depth === 'number') parts.push(`depth=${depth}`);
          if (typeof chainsN === 'number') parts.push(`chains=${chainsN}`);
          if (direction) parts.push(`direction=${direction}`);

          const title = 'Supply chain materialized';
          text = parts.length > 0 ? `${title} — ${parts.join(' • ')}` : title;
          humanReadable = {
            title,
            description: parts.length > 0 ? parts.join(' • ') : undefined,
          };
        } else if (type === 'horizon.supply_chain.exploration.materialized') {
          icon = 'success';
          const direction = typeof data?.direction === 'string' ? data.direction.trim() : '';
          const renderTarget = typeof data?.renderTarget === 'string' ? data.renderTarget.trim() : '';
          const nodesN = Array.isArray(data?.nodes) ? data.nodes.length : 0;
          const edgesN = Array.isArray(data?.edges) ? data.edges.length : 0;
          const artifactsN = Array.isArray(data?.artifacts) ? data.artifacts.length : 0;
          const parts = [
            direction ? `direction=${direction}` : '',
            renderTarget ? `target=${renderTarget}` : '',
            nodesN > 0 ? `nodes=${nodesN}` : '',
            edgesN > 0 ? `edges=${edgesN}` : '',
            artifactsN > 0 ? `artifacts=${artifactsN}` : '',
          ].filter(Boolean);
          text = parts.length > 0 ? `Directed exploration materialized — ${parts.join(' • ')}` : 'Directed exploration materialized';
          humanReadable = {
            title: 'Directed Exploration Materialized',
            description: parts.length > 0 ? parts.join(' • ') : undefined,
          };
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

          if (data?.humanReadable) {
            // Already handled at the end, but let's just set the defaults if it's not present
          } else {
            const l = String(rawLine || '').trim();
            const supplyPathMatch = l.match(/^\[SupplyChainMaterialize\]\s+artifact=([^\s]+)\s+chain=(\d+)\s+segments=(\d+)\s*$/i);
            const supplyDoneMatch = l.match(/^\[SupplyChainMaterialize\]\s+done\s+artifacts=(\d+)\s+skipped=(\d+)\s+merged=(\d+)\s+scopeAdded=(\d+)\s*$/i);
            const edgePropagationMatch = l.match(/^\[EdgeTriggeredPropagation\]\s+enqueued=(\d+)\s+signals=(\d+)\s+edge=([^\s]+)\s*$/i);
            if (supplyPathMatch) {
              const chainIndex = Number(supplyPathMatch[2] || 0);
              const segmentCount = Number(supplyPathMatch[3] || 0);
              const segmentLabel = segmentCount === 1 ? 'segment' : 'segments';
              text = `Materialized supply path ${chainIndex + 1}`;
              humanReadable = {
                title: 'Supply Path Materialized',
                description: `${segmentCount} ${segmentLabel} in this path`,
              };
            } else if (supplyDoneMatch) {
              const created = Number(supplyDoneMatch[1] || 0);
              const reused = Number(supplyDoneMatch[2] || 0);
              const merged = Number(supplyDoneMatch[3] || 0);
              const scopeAdded = Number(supplyDoneMatch[4] || 0);
              text = `Supply path materialization complete`;
              humanReadable = {
                title: 'Supply Paths Updated',
                description: `${created} new, ${reused} reused, ${merged} merged, ${scopeAdded} added to lens`,
              };
            } else if (edgePropagationMatch) {
              const enqueued = Number(edgePropagationMatch[1] || 0);
              const signals = Number(edgePropagationMatch[2] || 0);
              text = 'I queued downstream propagation';
              humanReadable = {
                title: 'Downstream Propagation Queued',
                description: `${signals} signal(s) across the new link • ${enqueued} fragment(s) added to the queue`,
              };
            } else {
            const cleaned = l.replace(/^\[[^\]]+\]\s*/, '');
            const line = short(cleaned);
            text = line || 'Scout activity';
            humanReadable = { title: 'Scout Activity', description: line };
            }
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
            // Unify with horizon.graph.edge.upserted: use type as chip, source→target as description
            const src =
              typeof data?.sourceCanonicalPath === 'string' && data.sourceCanonicalPath
                ? data.sourceCanonicalPath
                : paths[0] || '';
            const dst =
              typeof data?.targetCanonicalPath === 'string' && data.targetCanonicalPath
                ? data.targetCanonicalPath
                : paths[1] || '';
            const srcDisplay = src ? (humanizeCanonicalPath(src) || src) : '';
            const dstDisplay = dst ? (humanizeCanonicalPath(dst) || dst) : '';
            description =
              srcDisplay && dstDisplay
                ? `${srcDisplay} → ${dstDisplay}`
                : first && second
                  ? `${first} → ${second}`
                  : title || typeLabel || '';
            const formattedType = typeLabel || (fType ? formatTypeLabel(fType) : 'Relationship');
            text = formattedType;
            humanReadable = {
              title: formattedType,
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
          // Prefer humanized names for display; fallback to canonical paths (never raw fragment IDs)
          const srcDisplay = src ? (humanizeCanonicalPath(src) || src) : '';
          const dstDisplay = dst ? (humanizeCanonicalPath(dst) || dst) : '';
          humanReadable = {
            title: formattedType, // Just the type, UI will render "Link <chip> created"
            description: srcDisplay && dstDisplay ? `${srcDisplay} → ${dstDisplay}` : src && dst ? `${src} → ${dst}` : 'Connection details',
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
        } else if (type === 'horizon.canvas.newsByFragments.completed') {
          icon = 'success';
          const eventsCreated = typeof data?.eventsCreated === 'number' ? data.eventsCreated : 0;
          const signalsProcessed = typeof data?.signalsProcessed === 'number' ? data.signalsProcessed : 0;
          const pathCount = data?.newsByPath && typeof data.newsByPath === 'object' ? Object.keys(data.newsByPath).length : 0;
          text = `News loaded: ${eventsCreated} events, ${signalsProcessed} signals for ${pathCount} node${pathCount !== 1 ? 's' : ''}`;
          humanReadable = {
            title: 'News & ontology facts',
            description: `Created ${eventsCreated} event${eventsCreated !== 1 ? 's' : ''}, processed ${signalsProcessed} signal${signalsProcessed !== 1 ? 's' : ''} for ${pathCount} entit${pathCount !== 1 ? 'ies' : 'y'}.`,
          };
        } else if (type.startsWith('horizon.identify_stresses.')) {
          const jobId = typeof data?.jobId === 'string' ? data.jobId : '';
          const findingsApplied =
            typeof data?.findingsApplied === 'number' ? data.findingsApplied : 0;
          const touchedArtifacts =
            typeof data?.touchedArtifactCount === 'number'
              ? data.touchedArtifactCount
              : Array.isArray(data?.touchedArtifactKeys)
                ? data.touchedArtifactKeys.length
                : 0;
          const affectedZones =
            typeof data?.affectedZonesCreated === 'number' ? data.affectedZonesCreated : 0;
          if (type.endsWith('.started')) {
            icon = 'info';
            text = 'Identify stresses started';
            humanReadable = {
              title: 'Identify Stresses',
              description: jobId ? `Job ${jobId.slice(0, 8)} started` : 'Started',
            };
          } else if (type.endsWith('.scope_resolved')) {
            icon = 'info';
            const nodeCount = Array.isArray(data?.acceptedNodeRefs) ? data.acceptedNodeRefs.length : 0;
            const artifactCount = Array.isArray(data?.acceptedArtifactKeys) ? data.acceptedArtifactKeys.length : 0;
            text = `Stress scope resolved: ${nodeCount} nodes • ${artifactCount} routes`;
            humanReadable = {
              title: 'Stress Scope Resolved',
              description: `${nodeCount} nodes • ${artifactCount} routes`,
            };
          } else if (type.endsWith('.finding_applied')) {
            icon = 'success';
            text = `Applied ${findingsApplied} stress findings`;
            humanReadable = {
              title: 'Stress Findings Applied',
              description: `${findingsApplied} findings • ${touchedArtifacts} routes • ${affectedZones} zones`,
            };
          } else if (type.endsWith('.events_persisted')) {
            icon = 'success';
            const persisted =
              typeof data?.eventsPersisted === 'number' ? data.eventsPersisted : 0;
            text = `Stress events persisted: ${persisted}`;
            humanReadable = {
              title: 'Stress Events Persisted',
              description: `${persisted} ontology events`,
            };
          } else if (type.endsWith('.propagation_started')) {
            icon = 'propagation';
            const eventsInBatch =
              typeof data?.eventsInBatch === 'number' ? data.eventsInBatch : 0;
            text = `Stress propagation started`;
            humanReadable = {
              title: 'Stress Propagation',
              description: `${eventsInBatch} events in this batch`,
            };
          } else if (type.endsWith('.completed')) {
            const status = typeof data?.status === 'string' ? data.status.trim() : 'completed';
            const propagationErrors =
              typeof data?.propagationErrors === 'number' ? data.propagationErrors : 0;
            icon = status === 'partial_success' ? 'info' : 'success';
            text = status === 'partial_success' ? 'Identify stresses partially completed' : 'Identify stresses completed';
            humanReadable = {
              title: status === 'partial_success' ? 'Identify Stresses Partial Success' : 'Identify Stresses Completed',
              description: `${findingsApplied} findings • ${touchedArtifacts} routes • ${affectedZones} zones${propagationErrors > 0 ? ` • ${propagationErrors} propagation errors` : ''}`,
            };
          } else if (type.endsWith('.failed')) {
            icon = 'error';
            const errorText = typeof data?.error === 'string' ? data.error : 'Unknown error';
            text = `Identify stresses failed`;
            humanReadable = {
              title: 'Identify Stresses Failed',
              description: short(errorText),
            };
          } else if (type.endsWith('.artifact_upserted')) {
            icon = 'info';
            const displayName =
              typeof data?.displayName === 'string' && data.displayName.trim()
                ? data.displayName.trim()
                : typeof data?.canonicalKey === 'string'
                  ? humanizeCanonicalPath(data.canonicalKey)
                  : 'Supply route';
            text = `Route stress updated: ${displayName}`;
            humanReadable = {
              title: 'Route Stress Updated',
              description: displayName,
            };
          } else if (type.endsWith('.overlay_updated')) {
            icon = 'info';
            text = `Stress overlay updated`;
            humanReadable = {
              title: 'Stress Overlay Updated',
              description: `${touchedArtifacts} routes touched`,
            };
          }
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

      // Intra-graph: update assembly stage while linking entities
      if (type === 'horizon.intra_graph.started' || type === 'horizon.intra_graph.completed') {
        const stage = type === 'horizon.intra_graph.started' ? 'intra_graph' : 'ready';
        set((s) => ({
          assembly: {
            ...s.assembly,
            stage,
            phase: stage,
            lensId: data?.lensId ?? s.assembly.lensId,
            projectId: data?.projectId ?? s.assembly.projectId,
            lastEventAt: now,
          },
        }));
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

      if (type.startsWith('horizon.identify_stresses.')) {
        const lensId =
          typeof data?.lensId === 'string' && data.lensId.trim()
            ? data.lensId.trim()
            : undefined;
        const projectId =
          typeof data?.projectId === 'string' && data.projectId.trim()
            ? data.projectId.trim()
            : undefined;
        const jobId =
          typeof data?.jobId === 'string' && data.jobId.trim()
            ? data.jobId.trim()
            : '';
        const touchedArtifactKeys = Array.isArray(data?.touchedArtifactKeys)
          ? data.touchedArtifactKeys
              .map((value: any) => String(value || '').trim())
              .filter(Boolean)
          : [];
        const touchedNodeRefs = Array.isArray(data?.touchedNodes)
          ? data.touchedNodes
              .map((value: any) => String(value || '').trim())
              .filter(Boolean)
          : [];
        const touchedSupplyLinks = Array.isArray(data?.touchedSupplyLinks)
          ? data.touchedSupplyLinks
              .map((value: any) => String(value || '').trim())
              .filter(Boolean)
          : [];
        const upsertJob = (
          prev: HorizonIdentifyStressJobState | undefined,
        ): HorizonIdentifyStressJobState => ({
          jobId: jobId || prev?.jobId || '',
          lensId: lensId ?? prev?.lensId,
          projectId: projectId ?? prev?.projectId,
          state: prev?.state,
          findingsApplied:
            typeof data?.findingsApplied === 'number'
              ? data.findingsApplied
              : prev?.findingsApplied,
          eventsPersisted:
            typeof data?.eventsPersisted === 'number'
              ? data.eventsPersisted
              : prev?.eventsPersisted,
          affectedZonesCreated:
            typeof data?.affectedZonesCreated === 'number'
              ? data.affectedZonesCreated
              : prev?.affectedZonesCreated,
          artifactsUpdated:
            typeof data?.artifactsUpdated === 'number'
              ? data.artifactsUpdated
              : prev?.artifactsUpdated,
          supplyLinksUpdated:
            typeof data?.supplyLinksUpdated === 'number'
              ? data.supplyLinksUpdated
              : prev?.supplyLinksUpdated,
          propagationErrors:
            typeof data?.propagationErrors === 'number'
              ? data.propagationErrors
              : prev?.propagationErrors,
          acceptedNodes:
            Array.isArray(data?.acceptedNodeRefs)
              ? data.acceptedNodeRefs.length
              : prev?.acceptedNodes,
          acceptedArtifacts:
            Array.isArray(data?.acceptedArtifactKeys)
              ? data.acceptedArtifactKeys.length
              : prev?.acceptedArtifacts,
          acceptedSupplyLinks:
            Array.isArray(data?.acceptedSupplyLinks)
              ? data.acceptedSupplyLinks.length
              : prev?.acceptedSupplyLinks,
          touchedArtifactKeys:
            touchedArtifactKeys.length > 0
              ? touchedArtifactKeys
              : prev?.touchedArtifactKeys,
          touchedNodeRefs:
            touchedNodeRefs.length > 0 ? touchedNodeRefs : prev?.touchedNodeRefs,
          touchedSupplyLinks:
            touchedSupplyLinks.length > 0
              ? touchedSupplyLinks
              : prev?.touchedSupplyLinks,
          selectedCanonicalPath:
            typeof data?.selectedCanonicalPath === 'string'
              ? data.selectedCanonicalPath
              : prev?.selectedCanonicalPath,
          requiresRefetch:
            typeof data?.requiresRefetch === 'boolean'
              ? data.requiresRefetch
              : prev?.requiresRefetch,
          error:
            typeof data?.error === 'string' && data.error.trim()
              ? data.error.trim()
              : prev?.error,
          errors:
            Array.isArray(data?.errors)
              ? data.errors.map((value: any) => String(value || '').trim()).filter(Boolean)
              : prev?.errors,
          updatedAt: now,
        });

        set((s) => {
          const nextJobs = { ...s.identifyJobs };
          const keys = jobId
            ? [jobId]
            : Object.keys(nextJobs).filter((key) => {
                const candidate = nextJobs[key];
                return candidate?.lensId && lensId && candidate.lensId === lensId;
              });
          if (keys.length === 0 && jobId) keys.push(jobId);
          if (keys.length === 0) {
            return s;
          }
          for (const key of keys) {
            const prev = nextJobs[key];
            const next = upsertJob(prev);
            if (type.endsWith('.started')) next.state = 'started';
            else if (type.endsWith('.scope_resolved')) next.state = 'running';
            else if (type.endsWith('.finding_applied')) next.state = 'running';
            else if (type.endsWith('.events_persisted')) next.state = 'running';
            else if (type.endsWith('.propagation_started')) next.state = 'propagating';
            else if (type.endsWith('.completed')) {
              next.state =
                typeof data?.status === 'string' && data.status.trim()
                  ? data.status.trim()
                  : 'completed';
            }
            else if (type.endsWith('.failed')) next.state = 'failed';
            nextJobs[key] = next;
          }
          return { identifyJobs: nextJobs };
        });

        if (type.endsWith('.artifact_upserted')) {
          const artifactId =
            typeof data?.id === 'string' && data.id.trim()
              ? data.id.trim()
              : typeof data?.canonicalKey === 'string'
                ? data.canonicalKey.trim()
                : '';
          if (artifactId) {
            set((s) => ({
              artifacts: {
                ...s.artifacts,
                [artifactId]: {
                  ...(s.artifacts[artifactId] || { id: artifactId }),
                  id: artifactId,
                  lensId: data?.lensId ?? s.artifacts[artifactId]?.lensId,
                  canonicalKey: data?.canonicalKey ?? s.artifacts[artifactId]?.canonicalKey,
                  displayName: data?.displayName ?? s.artifacts[artifactId]?.displayName,
                  anchorSupplier: data?.anchorSupplier ?? s.artifacts[artifactId]?.anchorSupplier,
                  anchorCustomer: data?.anchorCustomer ?? s.artifacts[artifactId]?.anchorCustomer,
                  productAxis: data?.productAxis ?? s.artifacts[artifactId]?.productAxis,
                  nodeRefs: data?.nodeRefs ?? s.artifacts[artifactId]?.nodeRefs,
                  flowRefs: data?.flowRefs ?? s.artifacts[artifactId]?.flowRefs,
                  zoneRefs: data?.zoneRefs ?? s.artifacts[artifactId]?.zoneRefs,
                  affectedZoneRefs: data?.affectedZoneRefs ?? s.artifacts[artifactId]?.affectedZoneRefs,
                  legCount:
                    typeof data?.legCount === 'number'
                      ? data.legCount
                      : s.artifacts[artifactId]?.legCount,
                  bindingScore:
                    typeof data?.bindingScore === 'number'
                      ? data.bindingScore
                      : s.artifacts[artifactId]?.bindingScore,
                  pathStressScore:
                    typeof data?.pathStressScore === 'number'
                      ? data.pathStressScore
                      : s.artifacts[artifactId]?.pathStressScore,
                  riskOverlay: data?.riskOverlay ?? s.artifacts[artifactId]?.riskOverlay,
                  payload: data?.payload ?? s.artifacts[artifactId]?.payload,
                  updatedAt: now,
                },
              },
            }));
          }
          return;
        }

        return;
      }

      // Graph nodes
      if (type === 'horizon.graph.node.upserted') {
        const k = keyForNode(data);
        if (!k) return;
        set((s) => {
          const nextNodes = {
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
          };
          return { nodes: pruneNodes(nextNodes) };
        });
        return;
      }

      // Graph edges
      if (type === 'horizon.graph.edge.upserted') {
        const edgeId = typeof data?.edgeId === 'string' ? data.edgeId : '';
        if (!edgeId) return;
        set((s) => {
          const nextEdges = {
            ...s.edges,
            [edgeId]: {
              ...(s.edges[edgeId] || { edgeId }),
              edgeId,
              sourceFragmentId:
                data?.sourceFragmentId ?? s.edges[edgeId]?.sourceFragmentId,
              targetFragmentId:
                data?.targetFragmentId ?? s.edges[edgeId]?.targetFragmentId,
              sourceCanonicalPath:
                data?.sourceCanonicalPath ?? s.edges[edgeId]?.sourceCanonicalPath,
              targetCanonicalPath:
                data?.targetCanonicalPath ?? s.edges[edgeId]?.targetCanonicalPath,
              type: data?.type ?? s.edges[edgeId]?.type,
              properties: data?.properties ?? s.edges[edgeId]?.properties,
              qualifiers: data?.qualifiers ?? s.edges[edgeId]?.qualifiers,
              updatedAt: now,
            },
          };
          return { edges: pruneEdges(nextEdges) };
        });
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
      if (type === 'horizon.supply_chain.exploration.materialized') {
        set((s) => {
          const nextNodes = { ...s.nodes };
          const nextEdges = { ...s.edges };
          const nextArtifacts = { ...s.artifacts };
          for (const raw of Array.isArray(data?.nodes) ? data.nodes : []) {
            const key = keyForNode(raw);
            if (!key) continue;
            nextNodes[key] = {
              ...nextNodes[key],
              canonicalPath: raw?.canonicalPath ?? nextNodes[key]?.canonicalPath,
              label: raw?.label ?? nextNodes[key]?.label,
              type: raw?.type ?? nextNodes[key]?.type,
              updatedAt: now,
            };
          }
          for (const raw of Array.isArray(data?.edges) ? data.edges : []) {
            const edgeId = typeof raw?.edgeId === 'string' ? raw.edgeId : '';
            if (!edgeId) continue;
            nextEdges[edgeId] = {
              ...(nextEdges[edgeId] || { edgeId }),
              edgeId,
              sourceCanonicalPath: raw?.sourceCanonicalPath ?? nextEdges[edgeId]?.sourceCanonicalPath,
              targetCanonicalPath: raw?.targetCanonicalPath ?? nextEdges[edgeId]?.targetCanonicalPath,
              type: raw?.type ?? nextEdges[edgeId]?.type,
              qualifiers: raw?.qualifiers ?? nextEdges[edgeId]?.qualifiers,
              updatedAt: now,
            };
          }
          for (const raw of Array.isArray(data?.artifacts) ? data.artifacts : []) {
            const artifactId =
              typeof raw?.id === 'string' && raw.id.trim()
                ? raw.id.trim()
                : typeof raw?.canonicalKey === 'string'
                  ? raw.canonicalKey.trim()
                  : '';
            if (!artifactId) continue;
            nextArtifacts[artifactId] = {
              ...(nextArtifacts[artifactId] || { id: artifactId }),
              id: artifactId,
              lensId: raw?.lensId ?? nextArtifacts[artifactId]?.lensId,
              canonicalKey: raw?.canonicalKey ?? nextArtifacts[artifactId]?.canonicalKey,
              displayName: raw?.displayName ?? nextArtifacts[artifactId]?.displayName,
              anchorSupplier: raw?.anchorSupplier ?? nextArtifacts[artifactId]?.anchorSupplier,
              anchorCustomer: raw?.anchorCustomer ?? nextArtifacts[artifactId]?.anchorCustomer,
              productAxis: raw?.productAxis ?? nextArtifacts[artifactId]?.productAxis,
              nodeRefs: raw?.nodeRefs ?? nextArtifacts[artifactId]?.nodeRefs,
              flowRefs: raw?.flowRefs ?? nextArtifacts[artifactId]?.flowRefs,
              zoneRefs: raw?.zoneRefs ?? nextArtifacts[artifactId]?.zoneRefs,
              affectedZoneRefs: raw?.affectedZoneRefs ?? nextArtifacts[artifactId]?.affectedZoneRefs,
              legCount:
                typeof raw?.legCount === 'number'
                  ? raw.legCount
                  : nextArtifacts[artifactId]?.legCount,
              bindingScore:
                typeof raw?.bindingScore === 'number'
                  ? raw.bindingScore
                  : nextArtifacts[artifactId]?.bindingScore,
              pathStressScore:
                typeof raw?.pathStressScore === 'number'
                  ? raw.pathStressScore
                  : nextArtifacts[artifactId]?.pathStressScore,
              riskOverlay: raw?.riskOverlay ?? nextArtifacts[artifactId]?.riskOverlay,
              payload: raw?.payload ?? nextArtifacts[artifactId]?.payload,
              updatedAt: now,
            };
          }
          return {
            nodes: pruneNodes(nextNodes),
            edges: pruneEdges(nextEdges),
            artifacts: nextArtifacts,
          };
        });
        return;
      }
    },
  }),
);
