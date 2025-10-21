import { create } from 'zustand';
import { api } from '../lib/api';
import type { GenaiToolCall } from './genaiStore';

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
  agentMeta?: { displayName?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } };
  suppressUserEcho?: boolean;
}

type InteractionKind = 'user' | 'assistant' | 'system' | 'note';
export interface LocalInteraction {
  id: string;
  projectId: string; // temporary local id binding
  kind: InteractionKind;
  content: string;
  summary?: string;
  model?: string;
  usage?: { promptTokens?: number; outputTokens?: number; totalTokens?: number; reasoningTokens?: number; toolUsePromptTokens?: number };
  // Extended fields (optional; defaults keep UI working)
  author?: { participantId: string; kind: 'human'|'ai'|'system'|'tool'; displayName?: string };
  recipients?: string[];
  replyTo?: string;
  agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> };
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
  messageMeta: Record<string, { headerText?: string; title?: string; leftAccent?: boolean; bgOverride?: string; refineEnabled?: boolean }>;
  usage?: { promptTokens: number; outputTokens: number; totalTokens: number };

  // local-only interactions cache for Unfold
  projectId: string; // temporary project id (frontend-owned)
  interactions: LocalInteraction[];
  pendingAssistantMeta?: { displayName?: string; agentProfile?: { provider: string; model: string; version?: string; capabilities?: Record<string, boolean>; params?: Record<string, any> } };

  startStream: (args: StartStreamArgs) => Promise<void>;
  reset: () => void;

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

  reset: () => set({ messages: [], streaming: false, error: null, toolCalls: [], toolResults: [], attributions: [], searchQueries: [], evaluations: {}, messageMeta: {}, usage: undefined, interactions: [] }),

  setProjectId: (id) => set({ projectId: id || 'local-unfold' }),

  startStream: async ({ prompt, tools, options, agentMeta, suppressUserEcho }) => {
    if (get().streaming) return;
    const now = Date.now().toString();
    const projectId = get().projectId;
    const createdAt = new Date().toISOString();
    // add user message and a placeholder assistant message
    set((s) => ({
      messages: [
        ...s.messages,
        ...(suppressUserEcho ? [] : [{ id: now + '-u', role: 'user', content: prompt } as ChatMessage]),
        { id: now + '-a', role: 'assistant', content: '' },
      ],
      streaming: true,
      error: null,
      pendingAssistantMeta: agentMeta,
      // also record local interaction for user unless suppressed
      interactions: [
        ...s.interactions,
        ...(suppressUserEcho ? [] : [{ id: now + '-iu', projectId, kind: 'user' as InteractionKind, content: prompt, createdAt } as LocalInteraction]),
      ],
    }));
    try {
      // Build messages from local interactions (summary preferred, else content)
      const interactions = get().interactions;
      const messages: Array<{ role: 'user'|'assistant'|'system'|'note'; text: string }> = interactions.map((it) => ({
        role: (it.kind === 'assistant' || it.kind === 'system') ? (it.kind as 'assistant'|'system') : (it.kind === 'note' ? 'note' : 'user'),
        text: (it.summary && it.summary.trim()) ? it.summary : it.content,
      }));
      // Append current user prompt as the latest message
      messages.push({ role: 'user', text: prompt });
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
        const nextMeta = { ...s.messageMeta } as Record<string, { headerText?: string; title?: string; leftAccent?: boolean; bgOverride?: string; refineEnabled?: boolean }>;
        if (meta?.displayName) {
          nextMeta[lastAssistant.id] = { headerText: meta.displayName, leftAccent: true, refineEnabled: !!meta?.agentProfile?.capabilities?.refinePrevious };
        }
        return { streaming: false, pendingAssistantMeta: undefined, messageMeta: nextMeta, interactions: [...s.interactions, { id, projectId, kind: 'assistant', content: lastAssistant.content, createdAt, author: meta ? { participantId: `agent:${meta.displayName || 'model'}`, kind: 'ai', displayName: meta.displayName } : undefined, agentProfile: meta?.agentProfile }] } as any;
      }
      return { streaming: false } as any;
    }),
  onError: (err) => set({ streaming: false, error: err || 'Stream error' }),
  onToolCall: (calls) => set({ toolCalls: calls || [] }),
  onToolResult: (calls) => set((s) => ({ toolResults: [...s.toolResults, ...(calls || [])] })),
  onDoneMeta: (meta) => set({ usage: meta.usage, attributions: meta.attributions || [], searchQueries: meta.searchQueries || [] }),
  setEvaluation: (messageId, ev) => set((s) => ({ evaluations: { ...s.evaluations, [messageId]: ev } })),
}));


