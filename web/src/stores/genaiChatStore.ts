import { create } from 'zustand';
import { api } from '../lib/api';
import type { SxInstruction, SxEntityMark, SxMonitorMetric } from '../types/sx';
import { parseSxInstructionsFromContent } from '../lib/sxUtils';
import type { AnalyzeInteractionRef, AnalyzeRequest } from '../types/stance';
import type { GenaiToolCall } from './genaiStore';
import { useGenAISettings } from './genaiSettingsStore';
import { useProjectsStore } from './projectsStore';

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

type InteractionKind = 'user' | 'assistant' | 'system' | 'note' | 'section_start' | 'section_end';
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
        const restoredQueries = lastAssistant?.searchQueries || [];
        const restoredSources = lastAssistant?.sources || [];
        
        // Merge restored sources into attributions with dedup
        const allAttributions = [...s.attributions];
        const seenUrls = new Set(allAttributions.map(a => a.url).filter(Boolean));
        for (const url of restoredSources) {
          if (url && !seenUrls.has(url)) {
            allAttributions.push({ url });
            seenUrls.add(url);
          }
        }
        
        return { 
          interactions: merged,
          searchQueries: restoredQueries,
          sources: restoredSources,
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
      await api.genaiStream({ prompt, tools, options, messages });
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
        const base = { id, projectId, kind: 'assistant' as InteractionKind, subkind: (s.pendingAssistantMeta as any)?.subkind, content: lastAssistant.content, contentSanitized: sanitized, sxInstructions: sx, createdAt, author: meta ? { participantId: `agent:${meta.displayName || 'model'}`, kind: 'ai', displayName: meta.displayName } : undefined, agentProfile: meta?.agentProfile, systemInstructionApplied: typeof opts?.systemInstruction === 'string' ? opts.systemInstruction : undefined, language: opts?.__lang, generationParams: { temperature: opts?.temperature, maxOutputTokens: opts?.maxOutputTokens, groundingEnabled: opts?.groundingEnabled }, originalMessageId: lastAssistant.id } as LocalInteraction;
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
              subkind: (meta as any)?.subkind,
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
    // Merge sources into attributions with dedup by url
    const allAttributions = [...(meta.attributions || [])];
    const seenUrls = new Set(allAttributions.map(a => a.url).filter(Boolean));
    for (const url of (meta.sources || [])) {
      if (url && !seenUrls.has(url)) {
        allAttributions.push({ url });
        seenUrls.add(url);
      }
    }
    
    return { 
      usage: meta.usage, 
      attributions: allAttributions, 
      searchQueries: meta.searchQueries || [], 
      sources: meta.sources || [] 
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
}));


