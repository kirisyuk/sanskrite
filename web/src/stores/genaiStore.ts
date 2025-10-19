import { create } from 'zustand';
import { api } from '../lib/api';

export type GenaiChunk = { text: string };
export type GenaiToolCall = { name: string; arguments: string };

interface GenaiState {
  streaming: boolean;
  chunks: GenaiChunk[];
  toolCalls: GenaiToolCall[];
  error: string | null;
  usage?: { promptTokens: number; outputTokens: number; totalTokens: number };
  model?: string;

  reset: () => void;
  startStream: (data: { prompt: string; tools?: any[]; options?: any }) => Promise<void>;
  adhocJSON: (data: { prompt: string; options?: any }) => Promise<{ validJson: boolean; json: any; raw: string; model: string }>;
}

export const useGenaiStore = create<GenaiState>((set, get) => ({
  streaming: false,
  chunks: [],
  toolCalls: [],
  error: null,

  reset: () => set({ streaming: false, chunks: [], toolCalls: [], error: null, usage: undefined, model: undefined }),

  startStream: async (data) => {
    set({ streaming: true, chunks: [], toolCalls: [], error: null, usage: undefined, model: undefined });
    await api.genaiStream(data);
  },

  adhocJSON: async (data) => {
    const res = await api.genaiAdhocJSON(data);
    return { validJson: res.validJson, json: res.json, raw: res.raw, model: res.model };
  },
}));


