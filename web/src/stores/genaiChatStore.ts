import { create } from 'zustand';
import { api } from '../lib/api';
import type { SxInstruction, SxEntityMark, SxMonitorMetric } from '../types/sx';
import { parseSxInstructionsFromContent } from '../lib/sxUtils';
import type { AnalyzeInteractionRef, AnalyzeRequest } from '../types/stance';
import type { GenaiToolCall } from './genaiStore';
import { useGenAISettings } from './genaiSettingsStore';
import { useProjectsStore } from './projectsStore';
import { useWsStore } from './wsStore';

export type ChatRole = 'user' | 'assistant';

export interface ChatMessage {
  id: string;
  role: ChatRole;
  content: string;
}

export interface StartStreamArgs {
  prompt: string;
  tools?: any[];
  options?: any;
  agentMeta?: { displayName?: string; replyTo?: string; subkind?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } };
  suppressUserEcho?: boolean;
}

type InteractionKind = 'user' | 'assistant' | 'system' | 'note' | 'section_start' | 'section_end' | 'action';
export interface LocalInteraction {
  id: string;
  projectId: string; // temporary local id binding
  kind: InteractionKind;
  subkind?: string;
  content: string;
  contentSanitized?: string;
  sxInstructions?: SxInstruction[];
  summary?: string;
  originalMessageId?: string;
  model?: string;
  usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number };
  // Extended fields (optional; defaults keep UI working)
  author?: { participantId: string; kind: 'human'|'ai'|'system'|'tool'; displayName?: string };
  recipients?: string[];
  replyTo?: string;
  relations?: Array<{ type: 'answers'|'refines'|'evaluates'; targetId: string }>;
  agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> };
  systemInstructionApplied?: string;
  language?: string;
  generationParams?: { temperature?: number; maxOutputTokens?: number; groundingEnabled?: boolean };
  highRisk?: { level: 'High'|'Medium'|'Low'; score: number; rationale?: string; triggers?: string[] };
  evaluations?: Record<string, any>;
  coreDocIds?: string[];
  metadata?: { pinned?: boolean; notes?: string[] };
  graphData?: { chartTitle: string; table: any[][]; sources: string[] };
  // Anchored stream meta (per assistant message)
  searchQueries?: string[];
  sources?: string[];
  createdAt: string;
}

interface GenaiChatState {
  messages: ChatMessage[];
  streaming: boolean;
  error: string | null;
  toolCalls: GenaiToolCall[];
  toolResults: GenaiToolCall[];
  attributions: Array<{ title?: string; url?: string }>;
  searchQueries: string[];
  sources: string[];
  // Per-message structured evaluations
  evaluations: Record<string, { biasLevel: number; thoughtfulness: number; biases: string[]; biasTypes?: string[] }>;
  // Pre-stance preliminary call results (per user message)
  preStanceLatency: Record<string, number>;
  preStanceExtract: Record<string, { user_request: string }>;
  // Per-message UI meta (e.g., custom header for agent messages)
  messageMeta: Record<string, { headerText?: string; title?: string; leftAccent?: boolean; bgOverride?: string; refineEnabled?: boolean; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } }>;
  usage?: { promptTokens: number; outputTokens: number; totalTokens: number };

  // local-only interactions cache for Unfold
  projectId: string; // temporary project id (frontend-owned)
  interactions: LocalInteraction[];
  // Centralized artifacts extracted from @sx instructions (deduplicated)
  artifacts: { entities: SxEntityMark[]; metrics: SxMonitorMetric[] };
  // Metric overlay (full-screen over chat) state and cache
  metricOverlayOpenKey?: string | null;
  metricOverlayLoading?: boolean;
  metricOverlayCache: Record<string, { key: string; metric: SxMonitorMetric; content: string; fetchedAt: string }>;
  // Mapping of local ids/message ids -> server interaction ids
  persistedByLocalId: Record<string, string>;
  pendingAssistantMeta?: { displayName?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } };
  pendingAssistantOptions?: any;
  pendingAssistantPrompt?: string;
  pendingAssistantTools?: any[];
  pendingAttachedDocIds: string[];

  startStream: (args: StartStreamArgs) => Promise<void>;
  setPendingAttachedDocIds: (ids: string[]) => void;
  reset: () => void;
  openMetricOverlay: (metric: SxMonitorMetric) => Promise<void>;
  closeMetricOverlay: () => void;

  onStart: () => void;
  onChunk: (text: string) => void;
  onDone: () => void;
  onError: (err: string) => void;
  onToolCall: (calls: GenaiToolCall[]) => void;
  onToolResult: (calls: GenaiToolCall[]) => void;
  onDoneMeta: (meta: { usage?: { promptTokens: number; outputTokens: number; totalTokens: number }; attributions?: Array<{ title?: string; url?: string }>; searchQueries?: string[]; sources?: string[] }) => void;
  setEvaluation: (messageId: string, ev: { biasLevel: number; thoughtfulness: number; biases: string[]; biasTypes?: string[] }) => void;
  setPreStanceResult: (messageId: string, latencyMs: number, extract?: { user_request: string }) => void;

  // helpers to manage local interactions
  setProjectId: (id: string) => void;
  ensureProject: () => Promise<string>;
  hydrateInteractions: () => Promise<void>;
  deleteInteraction: (interactionId: string) => Promise<void>;

  // Your stance (meta aggregate) cache and actions
  stanceAggregate?: any;
  stanceLoading: boolean;
  loadStanceAggregate: () => Promise<void>;
  ensureStanceAggregate: () => Promise<void>;
}

export const useGenaiChatStore = create<GenaiChatState>((set, get) => ({
  messages: [],
  streaming: false,
  error: null,
  toolCalls: [],
  toolResults: [],
  attributions: [],
  searchQueries: [],
  sources: [],
  evaluations: {},
  preStanceLatency: {},
  preStanceExtract: {},
  messageMeta: {},
  projectId: 'local-unfold',
  interactions: [],
  artifacts: { entities: [], metrics: [] },
  metricOverlayOpenKey: null,
  metricOverlayLoading: false,
  metricOverlayCache: {},
  persistedByLocalId: {},
  stanceAggregate: undefined,
  stanceLoading: false,
  pendingAttachedDocIds: [],

  reset: () => set({ messages: [], streaming: false, error: null, toolCalls: [], toolResults: [], attributions: [], searchQueries: [], sources: [], evaluations: {}, messageMeta: {}, usage: undefined, interactions: [], artifacts: { entities: [], metrics: [] }, metricOverlayOpenKey: null, metricOverlayLoading: false, metricOverlayCache: {}, persistedByLocalId: {}, stanceAggregate: undefined, stanceLoading: false, pendingAttachedDocIds: [] }),
  
  setPendingAttachedDocIds: (ids) => set({ pendingAttachedDocIds: ids }),

  setProjectId: (id) => set({
    projectId: id || 'local-unfold',
    // clear chat-local state to avoid mixing between projects
    messages: [],
    interactions: [],
    artifacts: { entities: [], metrics: [] },
    persistedByLocalId: {},
    evaluations: {},
    messageMeta: {},
    metricOverlayCache: {},
    metricOverlayOpenKey: null,
    metricOverlayLoading: false,
    error: null,
    streaming: false,
    stanceAggregate: undefined,
    stanceLoading: false,
    pendingAttachedDocIds: [],
  }),

  ensureProject: async () => {
    let pid = get().projectId;
    if (!pid || pid === 'local-unfold') {
      try {
        console.log('[Chat] ensureProject: creating Draft project');
        const res = await api.createProject({ displayName: 'Draft', view: 'draft' });
        pid = res.id as unknown as string;
        set({ projectId: pid });
        // Reflect immediately in projects store as current chat
        try {
          useProjectsStore.setState((prev) => ({
            projects: [res as any, ...prev.projects.filter((p) => p.id !== (res as any).id)],
            currentProject: res as any,
          }));
        } catch {}
        console.log('[Chat] ensureProject: project created', { projectId: pid });
      } catch (e) {
        console.error('[ensureProject] failed to create draft project', e);
        throw e;
      }
    }
    console.log('[Chat] ensureProject: using project', { projectId: pid });
    return pid;
  },

  hydrateInteractions: async () => {
    const projectId = get().projectId;
    if (!projectId || projectId === 'local-unfold') return;
    try {
      const serverItems = await api.getProjectInteractions(projectId);
      set((s) => {
        const byServerId = new Set<string>((serverItems as any[]).map((it: any) => it.id));
        const map = (s as any).persistedByLocalId || {} as Record<string,string>;
        const keepLocals: any[] = [];
        for (const l of s.interactions) {
          // Drop local if server already has this id, or local maps to a server id present
          const mapped = map[l.id as any];
          if (byServerId.has(l.id as any)) continue;
          if (mapped && byServerId.has(mapped)) continue;
          keepLocals.push(l);
        }
        const merged = [...(serverItems as any[]), ...keepLocals];
        
        // Restore searchQueries and sources from last assistant interaction
        const lastAssistant = [...merged].reverse().find((it: any) => it.kind === 'assistant');
        const restoredQueries = Array.isArray(lastAssistant?.searchQueries) ? (lastAssistant as any).searchQueries.filter(Boolean) : [];
        const restoredSources = Array.isArray(lastAssistant?.sources) ? (lastAssistant as any).sources.filter(Boolean) : [];
        const prevQueries = Array.isArray(s.searchQueries) ? s.searchQueries.filter(Boolean) : [];
        const prevSources = Array.isArray(s.sources) ? s.sources.filter(Boolean) : [];
        const nextQueries = (restoredQueries.length > 0) ? Array.from(new Set([...(prevQueries || []), ...restoredQueries])) : prevQueries;
        const nextSources = (restoredSources.length > 0) ? Array.from(new Set([...(prevSources || []), ...restoredSources])) : prevSources;
        
        // Merge restored sources into attributions with dedup
        const allAttributions = [...(s.attributions || [])];
        const seenUrls = new Set(allAttributions.map(a => a.url).filter(Boolean));
        for (const url of nextSources) {
          if (url && !seenUrls.has(url)) {
            allAttributions.push({ url });
            seenUrls.add(url);
          }
        }
        
        return { 
          interactions: merged,
          searchQueries: nextQueries,
          sources: nextSources,
          attributions: allAttributions
        } as any;
      });
    } catch (e: any) {
      // Log with status code for debugging
      const status = e?.response?.status;
      if (status === 500) {
        console.error('[hydrateInteractions] Server error (500) - project may be initializing', { projectId });
      } else {
        console.error('[hydrateInteractions] Failed to load interactions', { projectId, status, error: e });
      }
    }
  },

  loadStanceAggregate: async () => {
    const projectId = get().projectId;
    if (!projectId || projectId === 'local-unfold') return;
    set({ stanceLoading: true });
    try {
      const agg = await api.metaGetAggregate(projectId);
      set({ stanceAggregate: agg, stanceLoading: false });
    } catch (e: any) {
      // 404 is normal for projects without aggregate yet
      if (e?.response?.status === 404) {
        console.log('[loadStanceAggregate] No aggregate yet (404) - normal for new project');
      } else {
        console.warn('[loadStanceAggregate] Failed to load aggregate', e);
      }
      set({ stanceLoading: false });
    }
  },

  ensureStanceAggregate: async () => {
    const projectId = get().projectId;
    if (!projectId || projectId === 'local-unfold') return;
    
    // Don't load aggregate if project is empty (no interactions yet)
    const hasInteractions = get().interactions.length > 0;
    if (!hasInteractions) {
      console.log('[ensureStanceAggregate] Skipping for empty project - no interactions yet');
      return;
    }
    
    // Only load existing aggregate; do not run analysis here
    try {
      set({ stanceLoading: true });
      await get().loadStanceAggregate();
    } catch (e: any) {
      // 404 is expected for new projects without analysis yet
      if (e?.response?.status !== 404) {
        console.warn('[ensureStanceAggregate] Error loading aggregate', e);
      }
    } finally {
      set({ stanceLoading: false });
    }
  },

  deleteInteraction: async (interactionId: string) => {
    const projectId = get().projectId;
    if (!projectId || projectId === 'local-unfold') return;

    const interactions = get().interactions;
    const targetIdx = interactions.findIndex((it) => it.id === interactionId);
    if (targetIdx === -1) return;

    // Build cascade set: include selected id and any evaluations/system notes tied via replyTo or relations
    const idSet = new Set<string>();
    idSet.add(interactionId);

    // Expand with local/server mapping variants
    try {
      const map = (get() as any).persistedByLocalId || ({} as Record<string, string>);
      // If selected id maps to server id, include it
      if (map[interactionId]) idSet.add(map[interactionId]);
      // Reverse map: collect all locals that point to selected id
      const reverse: Record<string, string[]> = {};
      Object.entries(map).forEach(([local, server]) => {
        const serverId = String(server);
        const localId = String(local);
        if (!reverse[serverId]) reverse[serverId] = [];
        reverse[serverId].push(localId);
      });
      const locals = reverse[interactionId] || [];
      for (const lid of locals) idSet.add(lid);
    } catch {}

    // Find dependent interactions (evaluations or system/note referencing target)
    for (const it of interactions) {
      const repliesToTarget = it.replyTo && idSet.has(it.replyTo);
      const relatesToTarget = Array.isArray(it.relations) && it.relations.some((r) => r && idSet.has(r.targetId));
      const isEval = typeof it.subkind === 'string' && it.subkind.startsWith('eval.');
      const isSystemOrNote = it.kind === 'system' || it.kind === 'note';
      if ((repliesToTarget || relatesToTarget) && (isEval || isSystemOrNote)) {
        idSet.add(it.id);
      }
    }

    const idsToDelete = Array.from(idSet);

    // Delete on server (best-effort for each; ignore 404s)
    try {
      await Promise.all(idsToDelete.map(async (id) => {
        try {
          await api.deleteProjectInteraction(projectId, id);
        } catch (err: any) {
          const status = err?.response?.status;
          if (status !== 404) {
            // Log but don't block local cleanup for dependents
            console.warn('[deleteInteraction] Server delete failed', { id, status, err });
            // Only throw if it's the main selected id and error isn't 404
            if (id === interactionId) throw err;
          }
        }
      }));
    } catch (e) {
      console.error('[deleteInteraction] Failed to delete selected interaction on server', e);
      throw e;
    }

    // Local cleanup: remove interactions and any mapped ids from persisted map
    set((s) => {
      const toDelete = new Set(idsToDelete);
      const nextInteractions = s.interactions.filter((it) => !toDelete.has(it.id));
      const nextMessages = s.messages.filter((m) => !toDelete.has(m.id));
      const nextMap: Record<string,string> = { ...(s.persistedByLocalId || {}) } as any;
      for (const [k, v] of Object.entries(nextMap)) {
        if (toDelete.has(k) || toDelete.has(v)) {
          delete nextMap[k];
        }
      }
      return { interactions: nextInteractions, messages: nextMessages, persistedByLocalId: nextMap } as any;
    });

    console.log('[deleteInteraction] Deleted interactions (cascade)', { idsToDelete });
  },

  openMetricOverlay: async (metric: SxMonitorMetric) => {
    const key = String((metric as any).id || (metric as any).metricName || (metric as any).name || '').toLowerCase();
    if (!key) return;
    const st = get();
    // If already cached, just open
    if (st.metricOverlayCache[key]?.content) {
      set({ metricOverlayOpenKey: key });
      return;
    }
    set({ metricOverlayLoading: true, metricOverlayOpenKey: key });
    try {
      const settings = useGenAISettings.getState();
      const model = settings.model;
      const preferred = (typeof window !== 'undefined' ? (window.localStorage.getItem('preferredLanguage') || 'EN') : 'EN');
      // First user request in this Unfold session, fallback to earliest message
      let firstRequest = '';
      try {
        const inters = get().interactions;
        const firstUser = inters.find((it) => it.kind === 'user');
        if (firstUser) firstRequest = firstUser.content || '';
        if (!firstRequest) {
          const msgs = get().messages;
          const firstMsg = msgs.find((m) => m.role === 'user');
          if (firstMsg) firstRequest = firstMsg.content || '';
        }
      } catch {}
      const today = new Date();
      const dateStr = today.toISOString().slice(0, 10);
      const metricJson = JSON.stringify(metric, null, 2);
      const prompt = `User requirement: you should create your answer in ${preferred} language. Use English or other languages inline only if you use well-known and accepted in target language terms.\n\nCurrentDate: ${dateStr}\n\nWe need a detailed, up-to-date insight for the following metric. Return ONLY Markdown.\n\nRequirements:\n- Start with a compact table summarizing current understanding of the metric (name, definition, unit, frequency, thresholds if any, typical ranges, data sources).\n- Follow with a concise article: definition, why it matters, how to track, caveats, and suggested next steps.\n- Use headings (###), bullet lists, and short paragraphs.\n- If data is uncertain, clearly say so and propose how to validate.\n\nContext:\n- METRIC:\n${metricJson}\n\n- ORIGINAL_USER_REQUEST:\n${firstRequest}\n\nUse neutral, factual tone.`;
      const maxTok = Math.max(2048, settings.maxOutputTokens || 4096);
      const res = await api.genaiGenerate({ model, prompt, strictJSONEnabled: false, maxOutputTokens: maxTok, webSearchEnabled: true, systemInstruction: settings.systemInstruction || undefined });
      const content = (res?.content || '').toString().trim();
      set((s) => ({ metricOverlayCache: { ...s.metricOverlayCache, [key]: { key, metric, content, fetchedAt: new Date().toISOString() } }, metricOverlayLoading: false, metricOverlayOpenKey: key } as any));
    } catch (e) {
      console.error('[metricOverlay] fetch failed', e);
      set({ metricOverlayLoading: false });
    }
  },
  closeMetricOverlay: () => {
    set({ metricOverlayOpenKey: null });
  },

  startStream: async ({ prompt, tools, options, agentMeta, suppressUserEcho }) => {
    if (get().streaming) return;
    // ensure we have a real project id (create Draft on demand)
    try { await get().ensureProject(); } catch {}
    // Ensure WS subscription is active before starting stream to avoid missing early chunks
    try {
      const pid = get().projectId;
      if ((window as any).wsSubscribeProject && pid) {
        (window as any).wsSubscribeProject(pid);
        // Wait for ws.subscribed ACK (best-effort, short timeout)
        const waitForWsSubscribed = async (projectId: string, timeoutMs = 1500) => {
          const start = Date.now();
          // eslint-disable-next-line no-constant-condition
          while (true) {
            try {
              const ws = useWsStore.getState();
              if (ws.connected && ws.subscribedProjectId === projectId) return;
            } catch {}
            if (Date.now() - start > timeoutMs) return;
            await new Promise((r) => setTimeout(r, 60));
          }
        };
        await waitForWsSubscribed(pid);
      }
    } catch {}
    try { console.log('[Chat] startStream: project id', { projectId: get().projectId }); } catch {}
    const now = Date.now().toString();
    const projectId = get().projectId;
    const createdAt = new Date().toISOString();
    // add user message and a placeholder assistant message
    set((s) => {
      // prune trailing empty assistant placeholders to avoid stale skeletons
      const pruned = [...s.messages];
      while (pruned.length > 0) {
        const last = pruned[pruned.length - 1];
        if (last.role === 'assistant' && (!last.content || last.content.trim().length === 0)) {
          pruned.pop();
          continue;
        }
        break;
      }
      return ({
      messages: [
        ...pruned,
        ...(suppressUserEcho ? [] : [{ id: now + '-u', role: 'user', content: prompt } as ChatMessage]),
        { id: now + '-a', role: 'assistant', content: '' },
      ],
      streaming: true,
      error: null,
      pendingAssistantMeta: agentMeta,
      pendingAssistantOptions: options,
      pendingAssistantPrompt: prompt,
      pendingAssistantTools: tools,
      // also record local interaction for user unless suppressed
      interactions: [
        ...s.interactions,
        ...(suppressUserEcho ? [] : [{ id: now + '-iu', projectId, kind: 'user' as InteractionKind, content: prompt, createdAt, originalMessageId: now + '-u' } as LocalInteraction]),
      ],
    }) as any;});
    
    // Preliminary stance structured call (extract user_request) - after UI update, before server sync
    if (!suppressUserEcho) {
      try {
        const stanceModel = useGenAISettings.getState().stanceModel;
        const evalModel = (stanceModel && stanceModel.trim()) ? stanceModel : 'gemini_fast';
        const schemaUr = {
          type: 'object',
          additionalProperties: false,
          properties: { 
            run: { type: 'string' },
            command: { type: 'string' },
            params: { type: 'string' }
          },
          required: ['run', 'command']
        } as any;
        const availableCommands = 'Available commands: /analyze entities, /analyze facts, /analyze events, /analyze metrics, /analyze decisions, /section <title>, /end';
        const promptUr = `You are performing a preliminary assessment of the user's request before execution. In the normal case, this is a regular LLM chat and command execution is not required, but in rare cases the user may request execution of system commands in natural language.\n\n${availableCommands}\n\nDetermine if the user's request is a demand to execute a system command from the list above. If it is - select the single most appropriate command. If command execution is not required - return empty string in the 'run' field.\n\nReturn STRICTLY JSON with this exact shape: { "run": string, "command": string, "params": string } (no prose, no markdown, no code fences).\n- "run": the extracted user request text if command execution is required, OR empty string "" if it's a regular chat request\n- "command": the matched command (e.g., "/analyze entities", "/section") if found, OR empty string "" if no command is required\n- "params": the remaining text/arguments for the command (e.g., section title or analyze context); empty string "" if not applicable\n\nUSER_MESSAGE:\n${prompt}`;
        const t0 = (typeof performance !== 'undefined' && performance.now) ? performance.now() : Date.now();
        const urRes = await api.genaiGenerate({
          model: evalModel,
          prompt: promptUr,
          strictJSONEnabled: true,
          jsonSchema: schemaUr,
          maxOutputTokens: 2048,
          webSearchEnabled: false,
        });
        const t1 = (typeof performance !== 'undefined' && performance.now) ? performance.now() : Date.now();
        const preLatency = Math.max(0, Math.floor(t1 - t0));
        let preExtract: { user_request: string } | undefined = undefined;
        let detectedCommand: string | undefined = undefined;
        let detectedParams: string | undefined = undefined;
        try {
          let parsedUr: any = null;
          if (typeof (urRes as any)?.rawJSON === 'string' && (urRes as any).rawJSON.trim()) {
            try { parsedUr = JSON.parse((urRes as any).rawJSON); } catch {}
          }
          if (!parsedUr && typeof urRes?.content === 'string') {
            const s = urRes.content.trim();
            if ((s.startsWith('{') || s.startsWith('['))) parsedUr = JSON.parse(s);
          }
          if (parsedUr && typeof parsedUr.run === 'string') {
            // If run is not empty, it contains the user request (command scenario)
            // If run is empty, use original prompt (normal chat scenario)
            const extracted = parsedUr.run.trim();
            preExtract = { user_request: extracted || prompt.trim() };
          } else {
            // Fallback: use the original user prompt
            if (prompt && typeof prompt === 'string' && prompt.trim()) {
              preExtract = { user_request: prompt.trim() };
            }
          }
          
          // Check if a command was detected
          if (parsedUr && typeof parsedUr.command === 'string') {
            const command = parsedUr.command.trim();
            if (command) {
              detectedCommand = command;
            }
          }
          // Capture params if provided
          if (parsedUr && typeof parsedUr.params === 'string') {
            const p = parsedUr.params.trim();
            if (p) detectedParams = p;
          }
        } catch {}
        
        // Store preStance result for this user message
        if (preLatency > 0) {
          try {
            if (detectedCommand) {
              console.log('[PreStance] detected command', { command: detectedCommand, params: detectedParams, latencyMs: preLatency });
            }
            console.log('[PreStance] measured', { latencyMs: preLatency, user_request: preExtract?.user_request, command: detectedCommand, params: detectedParams });
            get().setPreStanceResult(now + '-u', preLatency, preExtract);
          } catch {}
        }

        // If command detected, execute it instead of running GenAI
        if (detectedCommand) {
          try {
            // Remove assistant placeholder
            set((s) => {
              const messages = [...s.messages];
              if (messages.length > 0 && messages[messages.length - 1].role === 'assistant' && !messages[messages.length - 1].content) {
                messages.pop(); // Remove placeholder
              }
              return { messages, streaming: false } as any;
            });

            // Ensure USER interaction is persisted before creating ACTION
            if (!suppressUserEcho) {
              try {
                const userLocalId = now + '-iu';
                const meta = agentMeta as any;
                const payload: any = { kind: 'user', content: prompt, originalMessageId: now + '-u' };
                if (meta?.subkind) payload.subkind = meta.subkind;
                if (meta?.replyTo) {
                  const map = (get() as any).persistedByLocalId || {};
                  const serverReply = map[meta.replyTo] || map[String(meta.replyTo)];
                  if (serverReply) payload.replyTo = serverReply;
                }
                if (meta?.agentProfile) payload.agentProfile = meta.agentProfile;
                // Attach document IDs if any
                const attachedDocIds = get().pendingAttachedDocIds;
                if (attachedDocIds && attachedDocIds.length > 0) {
                  payload.coreDocIds = attachedDocIds;
                  try { console.log('[Chat] Attaching documents to interaction', { docIds: attachedDocIds }); } catch {}
                }
                const resUser = await api.createProjectInteraction(projectId, payload);
                // Clear attached docs after creating interaction
                set({ pendingAttachedDocIds: [] });
                set((st) => {
                  const items = [...st.interactions];
                  const idx = items.findIndex((it) => it.id === userLocalId);
                  if (idx >= 0) { (items[idx] as any).id = resUser.id; }
                  const map2 = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
                  map2[userLocalId] = resUser.id;
                  map2[now + '-u'] = resUser.id;
                  return { interactions: items, persistedByLocalId: map2 } as any;
                });
              } catch (e) {
                console.warn('[persist user-before-action] failed', e);
              }
            }

            // Create action interaction (to show intent in chat)
            try {
              const actionId = Date.now().toString() + '-ac';
              const createdAt = new Date().toISOString();
              const actionText = detectedCommand + (detectedParams ? (' ' + detectedParams) : '');
              const actionInter: LocalInteraction = {
                id: actionId,
                projectId,
                kind: 'action',
                content: actionText,
                createdAt,
                originalMessageId: now + '-u',
              };
              set((s) => ({ interactions: [...s.interactions, actionInter] }));
              // Persist action (best-effort)
              (async () => {
                try {
                  const res = await api.createProjectInteraction(projectId, { kind: 'action', content: actionText, originalMessageId: now + '-u' });
                  set((s) => {
                    const map = { ...(s.persistedByLocalId || {}) } as Record<string, string>;
                    map[actionId] = res.id;
                    const items = [...s.interactions];
                    const idx = items.findIndex((it) => it.id === actionId);
                    if (idx >= 0) { (items[idx] as any).id = res.id; }
                    return { interactions: items, persistedByLocalId: map } as any;
                  });
                } catch (e) {
                  console.warn('[Action] persist failed', e);
                }
              })();
            } catch {}

            // Execute command
            const cmd = detectedCommand.toLowerCase();
            const params = (detectedParams || '').trim();
            try {
              if (cmd === '/end') {
                await api.createProjectInteraction(projectId, { kind: 'section_end', content: '' });
                await get().hydrateInteractions();
                return;
              }
              if (cmd.startsWith('/section')) {
                // Use params as title
                await api.createProjectInteraction(projectId, { kind: 'section_start', content: params });
                await get().hydrateInteractions();
                return;
              }
              if (cmd === '/analyze entities') { await api.analyzeEntities(projectId, params); return; }
              if (cmd === '/analyze facts') { await api.analyzeFacts(projectId, params); return; }
              if (cmd === '/analyze events') { await api.analyzeEvents(projectId, params); return; }
              if (cmd === '/analyze metrics') { await api.analyzeMetrics(projectId, params); return; }
              if (cmd === '/analyze decisions') { await api.analyzeDecisions(projectId, params); return; }
              // Unknown command pattern: do nothing (already persisted user); simply stop streaming
              return;
            } catch (e) {
              console.error('[CommandExecute] failed', e);
              return;
            }

            // Exit early, don't run GenAI
            return;
          } catch (e) {
            console.error('[Action] failed', e);
          }
        }
      } catch {
        // PreStance is non-blocking, continue even if it fails
      }
    }
    
    // Persist user interaction on server (only if not suppressed)
    if (!suppressUserEcho) {
      try {
        const userLocalId = now + '-iu';
        const meta = agentMeta as any;
        const payload: any = { kind: 'user', content: prompt, originalMessageId: now + '-u' };
        if (meta?.subkind) payload.subkind = meta.subkind;
        if (meta?.replyTo) {
          const map = (get() as any).persistedByLocalId || {};
          const serverReply = map[meta.replyTo] || map[String(meta.replyTo)];
          if (serverReply) payload.replyTo = serverReply;
        }
        if (meta?.agentProfile) payload.agentProfile = meta.agentProfile;
        // Attach document IDs if any
        const attachedDocIds = get().pendingAttachedDocIds;
        if (attachedDocIds && attachedDocIds.length > 0) {
          payload.coreDocIds = attachedDocIds;
          console.log('[Chat] Attaching documents to interaction', { docIds: attachedDocIds });
        }
        const res = await api.createProjectInteraction(projectId, payload);
        // Clear attached docs after creating interaction
        set({ pendingAttachedDocIds: [] });
        set((st) => {
          const items = [...st.interactions];
          const idx = items.findIndex((it) => it.id === userLocalId);
          if (idx >= 0) { (items[idx] as any).id = res.id; }
          const map = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
          map[userLocalId] = res.id;
          map[now + '-u'] = res.id;
          return { interactions: items, persistedByLocalId: map } as any;
        });
      } catch (e) {
        console.warn('[persist user] failed', e);
      }
    }
    try {
      // Build messages from local interactions (summary preferred, else content)
      const interactions = get().interactions;
      const messages: Array<{ role: 'user'|'assistant'|'system'|'note'; text: string }> = interactions.map((it) => ({
        role: (it.kind === 'assistant' || it.kind === 'system') ? (it.kind as 'assistant'|'system') : (it.kind === 'note' ? 'note' : 'user'),
        text: (it.summary && it.summary.trim()) ? it.summary : it.content,
      }));
      // Append current user prompt as the latest message
      messages.push({ role: 'user', text: prompt });
      try {
        const modelOverride = options?.modelOverride;
        const provider = (agentMeta as any)?.agentProfile?.provider;
        const model = (agentMeta as any)?.agentProfile?.model || modelOverride;
        console.log('[GenAI Stream] start', {
          kind: (agentMeta as any)?.subkind || 'chat',
          agentName: agentMeta?.displayName,
          provider,
          model,
          promptChars: (prompt || '').length,
          hasTools: Array.isArray(tools) && tools.length > 0,
          groundingEnabled: !!options?.groundingEnabled,
          temperature: options?.temperature,
          maxOutputTokens: options?.maxOutputTokens,
          lang: options?.__lang,
        });
      } catch {}
      // Generate per-stream UUID for FE-side filtering
      const streamId = (crypto && 'randomUUID' in crypto) ? (crypto as any).randomUUID() : (Date.now().toString(36) + Math.random().toString(36).slice(2));
      await api.genaiStream({ prompt, tools, options, messages, projectId, streamId });
    } catch (e: any) {
      set({ streaming: false, error: e?.message || 'Stream start failed' });
    }
  },

  onStart: () => set((s) => {
    // Ensure we have an assistant message placeholder for streaming
    const msgs = [...s.messages];
    const hasAssistant = msgs.some(m => m.role === 'assistant');
    
    if (!hasAssistant) {
      // After hot-reload, create assistant message placeholder
      const id = Date.now().toString() + '-a';
      msgs.push({ id, role: 'assistant', content: '' });
    }
    
    return { 
      streaming: true, 
      error: null, 
      toolCalls: [], 
      toolResults: [], 
      attributions: [], 
      usage: undefined,
      messages: msgs 
    } as any;
  }),
  onChunk: (text: string) => {
    set((s) => {
      const msgs = [...s.messages];
      
      // Find last assistant message
      let found = false;
      for (let i = msgs.length - 1; i >= 0; i--) {
        if (msgs[i].role === 'assistant') {
          msgs[i] = { ...msgs[i], content: msgs[i].content + text };
          found = true;
          break;
        }
      }
      
      // If no assistant message exists (e.g., after hot-reload), create one
      if (!found) {
        const id = Date.now().toString() + '-a';
        msgs.push({ id, role: 'assistant', content: text });
      }
      
      return { messages: msgs } as any;
    });
  },
  onDone: () =>
    set((s) => {
      // capture final assistant message into local interactions
      const projectId = s.projectId;
      const lastAssistant = [...s.messages].reverse().find((m) => m.role === 'assistant');
      if (lastAssistant && lastAssistant.content.trim()) {
        const id = Date.now().toString() + '-ia';
        const createdAt = new Date().toISOString();
        const meta = s.pendingAssistantMeta;
        const opts = s.pendingAssistantOptions || {};
        const nextMeta = { ...s.messageMeta } as Record<string, { headerText?: string; title?: string; leftAccent?: boolean; bgOverride?: string; refineEnabled?: boolean; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } }>;
        if (meta?.displayName) {
          nextMeta[lastAssistant.id] = { headerText: meta.displayName, leftAccent: true, refineEnabled: !!meta?.agentProfile?.capabilities?.refinePrevious, agentProfile: meta?.agentProfile };
        }
        // Parse hidden @sx with minimal validation and sanitize content centrally
        const { sanitized, instructions: sx } = parseSxInstructionsFromContent(lastAssistant.content);

        // Try to parse graph-format response (chartTitle, table, sources)
        let parsedGraph: any = null;
        let detectedGraphData: { chartTitle: string; table: any[][]; sources: string[] } | undefined = undefined;
        try {
          let content = (lastAssistant.content || '').toString().trim();
          // Strip code fences
          content = content.replace(/^```[a-zA-Z]*\n([\s\S]*?)\n```$/m, '$1').trim();
          // Try direct JSON parse
          if ((content.startsWith('{') || content.startsWith('['))) {
            parsedGraph = JSON.parse(content);
          } else {
            // Try to find JSON within content
            const match = content.match(/\{[\s\S]*\}/);
            if (match && match[0]) parsedGraph = JSON.parse(match[0]);
          }
          // Validate graph structure
          if (
            parsedGraph &&
            typeof parsedGraph === 'object' &&
            typeof parsedGraph.chartTitle === 'string' &&
            Array.isArray(parsedGraph.table) &&
            Array.isArray(parsedGraph.sources)
          ) {
            detectedGraphData = {
              chartTitle: parsedGraph.chartTitle,
              table: parsedGraph.table,
              sources: parsedGraph.sources,
            };
            console.log('[Graph] Detected valid graph response', { chartTitle: detectedGraphData.chartTitle, tableRows: detectedGraphData.table.length, sources: detectedGraphData.sources.length });
          }
        } catch (e) {
          // Not a graph response, continue with normal flow
        }

        const base = { id, projectId, kind: 'assistant' as InteractionKind, subkind: detectedGraphData ? 'graph' : (s.pendingAssistantMeta as any)?.subkind, content: lastAssistant.content, contentSanitized: sanitized, sxInstructions: sx, graphData: detectedGraphData, createdAt, author: meta ? { participantId: `agent:${meta.displayName || 'model'}`, kind: 'ai', displayName: meta.displayName } : undefined, agentProfile: meta?.agentProfile, systemInstructionApplied: typeof opts?.systemInstruction === 'string' ? opts.systemInstruction : undefined, language: opts?.__lang, generationParams: { temperature: opts?.temperature, maxOutputTokens: opts?.maxOutputTokens, groundingEnabled: opts?.groundingEnabled }, originalMessageId: lastAssistant.id, searchQueries: (s.searchQueries || []).filter(Boolean), sources: (s.sources || []).filter(Boolean) } as LocalInteraction;
        const replyTo = (s.pendingAssistantMeta as any)?.replyTo as string | undefined;
        if (replyTo) {
          (base as any).replyTo = replyTo;
          (base as any).relations = [{ type: 'answers', targetId: replyTo }];
        }
        // duplicate meta under interaction id as well for interaction-based rendering
        if (meta?.displayName) {
          (nextMeta as any)[id] = { headerText: meta.displayName, leftAccent: true, refineEnabled: !!meta?.agentProfile?.capabilities?.refinePrevious, agentProfile: meta?.agentProfile };
        }
        // Merge artifacts into centralized state (deduplicated)
        const currentArtifacts = s.artifacts || { entities: [], metrics: [] };
        const nextEntities: SxEntityMark[] = [...currentArtifacts.entities];
        const nextMetrics: SxMonitorMetric[] = [...currentArtifacts.metrics];
        const seenEntity = new Set(nextEntities.map((e) => `entity:${(e as any).kind || 'other'}:${String((e as any).name || (e as any).id || '').toLowerCase()}`));
        const seenMetric = new Set(nextMetrics.map((m) => `metric:${String((m as any).id || (m as any).metricName || (m as any).name || '').toLowerCase()}`));
        for (const instr of sx) {
          if (instr.type === 'entity_mark') {
            const e = instr as SxEntityMark;
            const key = `entity:${(e as any).kind || 'other'}:${String((e as any).name || (e as any).id || '').toLowerCase()}`;
            if (!seenEntity.has(key)) { seenEntity.add(key); nextEntities.push(e); }
          } else if (instr.type === 'monitor_metric') {
            const m = instr as SxMonitorMetric;
            const key = `metric:${String((m as any).id || (m as any).metricName || (m as any).name || '').toLowerCase()}`;
            if (!seenMetric.has(key)) { seenMetric.add(key); nextMetrics.push(m); }
          }
        }
        const nextInteractions = [...s.interactions, base];
        (async () => {
          try {
            const payload: any = {
              kind: 'assistant',
              content: lastAssistant.content,
              subkind: detectedGraphData ? 'graph' : (meta as any)?.subkind,
              contentSanitized: sanitized,
              author: meta ? { participantId: `agent:${meta.displayName || 'model'}`, kind: 'ai', displayName: meta.displayName } : undefined,
              agentProfile: meta?.agentProfile,
              systemInstructionApplied: typeof opts?.systemInstruction === 'string' ? opts.systemInstruction : undefined,
              language: opts?.__lang,
              generationParams: { temperature: opts?.temperature, maxOutputTokens: opts?.maxOutputTokens, groundingEnabled: opts?.groundingEnabled },
              originalMessageId: lastAssistant.id,
              usage: s.usage,
              searchQueries: s.searchQueries,
              sources: s.sources,
              graphData: detectedGraphData,
            };
            const map = (get() as any).persistedByLocalId || {};
            const replyToMsg = (s.pendingAssistantMeta as any)?.replyTo as string | undefined;
            if (replyToMsg && map[replyToMsg]) {
              payload.replyTo = map[replyToMsg];
              payload.relations = [{ type: 'answers', targetId: map[replyToMsg] }];
            } else if ((base as any).replyTo) {
              payload.replyTo = (base as any).replyTo;
              if ((base as any).relations) payload.relations = (base as any).relations;
            }
            const res = await api.createProjectInteraction(projectId, payload);
            set((st) => {
              const items = [...st.interactions];
              const idx = items.findIndex((it) => it.id === id);
              if (idx >= 0) { (items[idx] as any).id = res.id; }
              const m = { ...(st.persistedByLocalId || {}) } as Record<string, string>;
              m[id] = res.id;
              m[lastAssistant.id] = res.id;
              return { interactions: items, persistedByLocalId: m } as any;
            });
          } catch (e) {
            console.warn('[persist assistant] failed', e);
          }
        })();

        // Trigger meta-analysis only for standard USER→ASSISTANT exchanges on server-backed projects
        (async () => {
          try {
            if (!projectId || projectId === 'local-unfold') return;
            // Skip if this is an agent reply (has displayName)
            if (meta?.displayName) return;
            const inters = get().interactions || [];
            if (inters.length === 0) return;
            // Freshness guard: do not analyze if aggregate is up-to-date
            const agg = get().stanceAggregate;
            const last = inters[inters.length - 1];
            const aggUpdatedAt = agg && (agg.updatedAt || agg.UpdatedAt);
            if (aggUpdatedAt && last?.createdAt) {
              try {
                const a = new Date(aggUpdatedAt);
                const b = new Date(last.createdAt);
                if (!Number.isNaN(a.getTime()) && !Number.isNaN(b.getTime()) && a >= b) {
                  console.log('[Meta] Skip analyze: aggregate is fresh');
                  return;
                }
              } catch {}
            }
            // Decide mode: periodic full recompute every ~12 exchanges
            const messagesSinceLastFull = (agg?.lastAnalyzedMessageCount as number) || 0;
            const doFull = (inters.length - messagesSinceLastFull) >= 12 || !agg;
            let req: AnalyzeRequest;
            if (doFull) {
              // Full recompute: last up to 20 refs
              const refs: AnalyzeInteractionRef[] = [];
              const take = Math.min(20, inters.length);
              for (let i = Math.max(0, inters.length - take); i < inters.length; i++) {
                const it = inters[i] as any;
                if (!it || !it.content) continue;
                const isServerBacked = it.id && typeof it.id === 'string' && !it.id.endsWith('-iu') && !it.id.endsWith('-ia') && it.id.indexOf('-eval-') === -1 && it.id.indexOf('eval-bias-auto') === -1;
                if (isServerBacked) refs.push({ refId: it.id });
                else {
                  const role = (it.kind === 'assistant') ? 'assistant' : (it.kind === 'system' ? 'system' : 'user');
                  refs.push({ inline: { role, text: String(it.content), timestamp: it.createdAt } });
                }
              }
              req = { analyzers: ['thinking','biases','decisions','comm','learning'], interactions: refs } as any;
              console.log('[Meta] Full recompute', { count: refs.length });
            } else {
              // Incremental: previous aggregate + last pair (user, assistant)
              const prev = (agg?.lastResults || agg?.result || agg) as any;
              const refs: AnalyzeInteractionRef[] = [];
              const take = Math.min(2, inters.length);
              for (let i = Math.max(0, inters.length - take); i < inters.length; i++) {
                const it = inters[i] as any;
                if (!it || !it.content) continue;
                const role = (it.kind === 'assistant') ? 'assistant' : (it.kind === 'system' ? 'system' : 'user');
                refs.push({ inline: { role, text: String(it.content), timestamp: it.createdAt } });
              }
              req = { analyzers: ['thinking','biases','decisions','comm','learning'], interactions: refs, previousAggregate: prev } as any;
              console.log('[Meta] Incremental update', { pair: refs.length });
            }
            await api.metaAnalyze(projectId, req as any);
            await get().loadStanceAggregate();
          } catch (e) {
            console.warn('[Meta] analysis failed', e);
          }
        })();
        return { streaming: false, pendingAssistantMeta: undefined, pendingAssistantOptions: undefined, pendingAssistantPrompt: undefined, pendingAssistantTools: undefined, messageMeta: nextMeta, interactions: nextInteractions, artifacts: { entities: nextEntities, metrics: nextMetrics } } as any;
      }
      // If we are here, assistant produced no chunks and content is empty.
      // Remove the trailing empty placeholder and create a stub interaction so the user sees the outcome.
      const msgs = [...s.messages];
      if (msgs.length > 0 && msgs[msgs.length - 1].role === 'assistant' && !msgs[msgs.length - 1].content) {
        msgs.pop();
      }
      const id = Date.now().toString() + '-ia';
      const createdAt = new Date().toISOString();
      const meta = s.pendingAssistantMeta;
      const opts = s.pendingAssistantOptions || {};
      const nextMeta = { ...s.messageMeta } as Record<string, { headerText?: string; title?: string; leftAccent?: boolean; bgOverride?: string; refineEnabled?: boolean; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } }>;
      const stub = 'Empty reply (no text was returned by the model).';
      const base: LocalInteraction = {
        id,
        projectId,
        kind: 'assistant',
        subkind: (s.pendingAssistantMeta as any)?.subkind,
        content: stub,
        contentSanitized: stub,
        createdAt,
        author: meta ? { participantId: `agent:${meta.displayName || 'model'}`, kind: 'ai', displayName: meta.displayName } : undefined,
        agentProfile: meta?.agentProfile,
        systemInstructionApplied: typeof opts?.systemInstruction === 'string' ? opts.systemInstruction : undefined,
        language: opts?.__lang,
        generationParams: { temperature: opts?.temperature, maxOutputTokens: opts?.maxOutputTokens, groundingEnabled: opts?.groundingEnabled },
        originalMessageId: id,
      } as any;
      const replyTo = (s.pendingAssistantMeta as any)?.replyTo as string | undefined;
      if (replyTo) {
        (base as any).replyTo = replyTo;
        (base as any).relations = [{ type: 'answers', targetId: replyTo }];
      }
      if (meta?.displayName) {
        nextMeta[id] = { headerText: meta.displayName, leftAccent: true, refineEnabled: !!meta?.agentProfile?.capabilities?.refinePrevious, agentProfile: meta?.agentProfile };
      }
      console.warn('[GenAI Stream] done without chunks: created empty reply stub');
      return { streaming: false, messages: msgs, pendingAssistantMeta: undefined, pendingAssistantOptions: undefined, pendingAssistantPrompt: undefined, pendingAssistantTools: undefined, messageMeta: nextMeta, interactions: [...s.interactions, base] } as any;
    }),
  onError: (err) => {
    try {
      const st = get();
      console.error('[GenAI Stream] error', {
        error: err,
        kind: (st.pendingAssistantMeta as any)?.subkind || 'chat',
        agentName: st.pendingAssistantMeta?.displayName,
        provider: (st.pendingAssistantMeta as any)?.agentProfile?.provider,
        model: (st.pendingAssistantMeta as any)?.agentProfile?.model || st.pendingAssistantOptions?.modelOverride,
        promptChars: (st.pendingAssistantPrompt || '').length,
        lang: st.pendingAssistantOptions?.__lang,
        hasTools: Array.isArray(st.pendingAssistantTools) && st.pendingAssistantTools.length > 0,
        groundingEnabled: !!st.pendingAssistantOptions?.groundingEnabled,
      });
    } catch {}
    // Also prune trailing empty assistant placeholder so skeleton doesn't stick
    const st = get();
    const pruned = [...st.messages];
    if (pruned.length > 0 && pruned[pruned.length - 1].role === 'assistant' && (!pruned[pruned.length - 1].content || pruned[pruned.length - 1].content.trim().length === 0)) {
      pruned.pop();
    }
    set({ streaming: false, messages: pruned, error: err || 'Stream error', pendingAssistantMeta: undefined, pendingAssistantOptions: get().pendingAssistantOptions, pendingAssistantPrompt: get().pendingAssistantPrompt, pendingAssistantTools: get().pendingAssistantTools });
  },
  onToolCall: (calls) => set({ toolCalls: calls || [] }),
  onToolResult: (calls) => set((s) => ({ toolResults: [...s.toolResults, ...(calls || [])] })),
  onDoneMeta: (meta) => set((s) => {
    // Merge sources/queries non-destructively and fold into attributions
    const prevQueries = Array.isArray(s.searchQueries) ? s.searchQueries.filter(Boolean) : [];
    const prevSources = Array.isArray(s.sources) ? s.sources.filter(Boolean) : [];
    const incomingQueries = Array.isArray(meta.searchQueries) ? meta.searchQueries.filter(Boolean) : [];
    const incomingSources = Array.isArray(meta.sources) ? meta.sources.filter(Boolean) : [];
    const nextQueries = incomingQueries.length > 0 ? Array.from(new Set([...(prevQueries || []), ...incomingQueries])) : prevQueries;
    const nextSources = incomingSources.length > 0 ? Array.from(new Set([...(prevSources || []), ...incomingSources])) : prevSources;

    // Merge attributions: keep existing + incoming attributions + nextSources as URLs
    const allAttributions = [...(s.attributions || []), ...((meta.attributions || []) as any[])];
    const seen = new Set(allAttributions.map((a) => a.url).filter(Boolean));
    for (const url of nextSources) {
      if (url && !seen.has(url)) {
        allAttributions.push({ url });
        seen.add(url);
      }
    }
    
    return { 
      usage: meta.usage, 
      attributions: allAttributions, 
      searchQueries: nextQueries, 
      sources: nextSources 
    } as any;
  }),
  setEvaluation: (messageId, ev) => set((s) => {
    const next = { ...s.evaluations, [messageId]: ev };
    // Always anchor eval to messageId locally for stable UI
    const replyToId = messageId;
    const exists = s.interactions.find((it) => it.subkind === 'eval.bias' && it.replyTo === replyToId);
    const interactions = exists ? s.interactions : [
      ...s.interactions,
      {
        id: Date.now().toString() + '-eval-bias-auto',
        projectId: s.projectId,
        kind: 'system',
        subkind: 'eval.bias',
        content: JSON.stringify(ev),
        contentSanitized: JSON.stringify(ev),
        replyTo: replyToId,
        relations: [{ type: 'evaluates', targetId: replyToId }],
        createdAt: new Date().toISOString(),
      } as any,
    ];
    // Best-effort persist eval interaction on server
    try {
      const map = (s as any).persistedByLocalId || {};
      const serverReplyTo = map[replyToId];
      const payload: any = { kind: 'system', subkind: 'eval.bias', content: JSON.stringify(ev) };
      if (serverReplyTo) {
        payload.replyTo = serverReplyTo;
        payload.relations = [{ type: 'evaluates', targetId: serverReplyTo }];
      }
      api.createProjectInteraction(s.projectId, payload).catch(() => {});
    } catch {}
    return { evaluations: next, interactions } as any;
  }),
  setPreStanceResult: (messageId, latencyMs, extract) => set((s) => {
    const nextLatency = { ...(s.preStanceLatency || {}) } as Record<string, number>;
    const nextExtract = { ...(s.preStanceExtract || {}) } as Record<string, { user_request: string }>;
    nextLatency[messageId] = Math.max(0, Math.floor(Number(latencyMs) || 0));
    if (extract && typeof extract.user_request === 'string') {
      nextExtract[messageId] = { user_request: extract.user_request };
    }
    try {
      console.log('[PreStance] state', { messageId, latencyMs: nextLatency[messageId], user_request: nextExtract[messageId]?.user_request });
    } catch {}
    // Also persist as a system interaction for traceability (best-effort)
    try {
      const projectId = s.projectId;
      const map = (s as any).persistedByLocalId || {};
      const serverReplyTo = map[messageId];
      const payload: any = { kind: 'system', subkind: 'eval.user_request', content: JSON.stringify({ latencyMs: nextLatency[messageId], ...(nextExtract[messageId] || {}) }) };
      if (serverReplyTo) {
        payload.replyTo = serverReplyTo;
        payload.relations = [{ type: 'evaluates', targetId: serverReplyTo }];
      }
      api.createProjectInteraction(projectId, payload).catch(() => {});
    } catch {}
    return { preStanceLatency: nextLatency, preStanceExtract: nextExtract } as any;
  }),
}));


