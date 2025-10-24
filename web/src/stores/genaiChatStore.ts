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

type InteractionKind = 'user' | 'assistant' | 'system' | 'note';
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

  startStream: (args: StartStreamArgs) => Promise<void>;
  reset: () => void;
  openMetricOverlay: (metric: SxMonitorMetric) => Promise<void>;
  closeMetricOverlay: () => void;

  onStart: () => void;
  onChunk: (text: string) => void;
  onDone: () => void;
  onError: (err: string) => void;
  onToolCall: (calls: GenaiToolCall[]) => void;
  onToolResult: (calls: GenaiToolCall[]) => void;
  onDoneMeta: (meta: { usage?: { promptTokens: number; outputTokens: number; totalTokens: number }; attributions?: Array<{ title?: string; url?: string }>; searchQueries?: string[] }) => void;
  setEvaluation: (messageId: string, ev: { biasLevel: number; thoughtfulness: number; biases: string[]; biasTypes?: string[] }) => void;

  // helpers to manage local interactions
  setProjectId: (id: string) => void;
  ensureProject: () => Promise<string>;
  hydrateInteractions: () => Promise<void>;

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

  reset: () => set({ messages: [], streaming: false, error: null, toolCalls: [], toolResults: [], attributions: [], searchQueries: [], evaluations: {}, messageMeta: {}, usage: undefined, interactions: [], artifacts: { entities: [], metrics: [] }, metricOverlayOpenKey: null, metricOverlayLoading: false, metricOverlayCache: {}, persistedByLocalId: {}, stanceAggregate: undefined, stanceLoading: false }),

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
        return { interactions: merged } as any;
      });
    } catch (e) {
      console.error('[hydrateInteractions] failed', e);
    }
  },

  loadStanceAggregate: async () => {
    const projectId = get().projectId;
    if (!projectId || projectId === 'local-unfold') return;
    set({ stanceLoading: true });
    try {
      const agg = await api.metaGetAggregate(projectId);
      set({ stanceAggregate: agg, stanceLoading: false });
    } catch (e) {
      set({ stanceLoading: false });
    }
  },

  ensureStanceAggregate: async () => {
    const projectId = get().projectId;
    if (!projectId || projectId === 'local-unfold') return;
    // Avoid 404: check if any meta events exist first; if none, run analyze before first GET
    try {
      set({ stanceLoading: true });
      const events = await api.metaListEvents(projectId);
      if (Array.isArray(events) && events.length > 0) {
        await get().loadStanceAggregate();
        const current = get().stanceAggregate;
        if (current && typeof current === 'object') { set({ stanceLoading: false }); return; }
      }
    } catch {}
    // Run meta:analyze over recent interactions (use server ids if present)
    try {
      const inters = get().interactions || [];
      const refs: AnalyzeInteractionRef[] = [];
      const take = Math.min(20, inters.length);
      for (let i = Math.max(0, inters.length - take); i < inters.length; i++) {
        const it = inters[i] as any;
        if (!it || !it.content) continue;
        // Use refId only for server-backed interactions; exclude known local-only ids
        if (
          it.id && typeof it.id === 'string'
          && !it.id.endsWith('-iu') && !it.id.endsWith('-ia')
          && it.id.indexOf('-eval-') === -1 && it.id.indexOf('eval-bias-auto') === -1
        ) {
          refs.push({ refId: it.id });
        } else {
          const role = (it.kind === 'assistant') ? 'assistant' : (it.kind === 'system' ? 'system' : 'user');
          refs.push({ inline: { role, text: String(it.content), timestamp: it.createdAt } });
        }
      }
      const req: AnalyzeRequest = { analyzers: ['thinking','biases','decisions','comm','learning'], interactions: refs } as any;
      await api.metaAnalyze(projectId, req as any);
      // Refresh aggregate after analysis
      await get().loadStanceAggregate();
      // Optionally persist a compact stance snapshot as an interaction for quick UI access
      try {
        const agg = get().stanceAggregate;
        if (agg) {
          await api.createProjectInteraction(projectId, {
            kind: 'system',
            content: JSON.stringify({ stance: 'aggregate', common: agg?.common, lastResults: agg?.lastResults }),
            // tag as stance snapshot
            // subkind is accepted via extended fields already supported server-side
            // @ts-ignore
            subkind: 'stance',
          } as any);
        }
      } catch {}
    } catch (e) {
      // ignore
    } finally { set({ stanceLoading: false }); }
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
  closeMetricOverlay: () => set({ metricOverlayOpenKey: null }),

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
        const payload: any = { kind: 'user', content: prompt };
        if (meta?.subkind) payload.subkind = meta.subkind;
        if (meta?.replyTo) {
          const map = (get() as any).persistedByLocalId || {};
          const serverReply = map[meta.replyTo] || map[String(meta.replyTo)];
          if (serverReply) payload.replyTo = serverReply;
        }
        if (meta?.agentProfile) payload.agentProfile = meta.agentProfile;
        const res = await api.createProjectInteraction(projectId, payload);
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

  onStart: () => set({ streaming: true, error: null, toolCalls: [], toolResults: [], attributions: [], usage: undefined }),
  onChunk: (text: string) =>
    set((s) => {
      const msgs = [...s.messages];
      // append to last assistant message
      for (let i = msgs.length - 1; i >= 0; i--) {
        if (msgs[i].role === 'assistant') {
          msgs[i] = { ...msgs[i], content: msgs[i].content + text };
          break;
        }
      }
      return { messages: msgs } as any;
    }),
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
    // If we have usage and last assistant persisted, patch it on server
    try {
      if (meta?.usage) {
        const msgs = [...s.messages];
        const lastAssistant = [...msgs].reverse().find((m) => m.role === 'assistant');
        if (lastAssistant) {
          const map = (s as any).persistedByLocalId || {};
          const serverId = map[lastAssistant.id];
          if (serverId) {
            api.updateProjectInteraction(s.projectId, serverId, { usage: meta.usage as any }).catch(() => {});
          }
        }
      }
    } catch {}
    return { usage: meta.usage, attributions: meta.attributions || [], searchQueries: meta.searchQueries || [] } as any;
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


