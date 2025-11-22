import { create } from 'zustand';

export type GenAIProvider = 'gemini' | 'openai' | 'grok' | 'claude' | 'deepseek';
export type GenAIStrength = 'fast' | 'balanced' | 'quality' | 'preview';

// Backward-compat alias type (kept for minimal refactor use sites)
export type GenAIModel =
  | 'gemini_quality' // gemini-2.5-pro
  | 'gemini_fast'    // gemini-2.5-flash-lite
  | 'gemini_balanced'// gemini-2.5-flash
  | 'gemini_preview' // gemini-3-pro-preview
  | 'gpt_quality'    // gpt-5.1
  | 'gpt_balanced'   // gpt-5-mini
  | 'gpt_fast'       // gpt-5-nano
  | 'grok_quality'   // grok-4-0709
  | 'grok_balanced'  // grok-4-1-fast-reasoning
  | 'grok_fast'      // grok-4-1-fast-non-reasoning
  | 'claude_balanced'// claude-sonnet-4-5-20250929 (quality option disabled - too expensive)
  | 'claude_fast'    // claude-haiku-4-5-20251001
  | 'deepseek_quality' // deepseek-reasoner
  | 'deepseek_fast'; // deepseek-chat

export interface GenAICapabilities {
  grounding: boolean;
  tools: boolean;
  streamingWithTools: boolean; // Whether the provider supports streaming with tool calling
}

export const providerCapabilities: Record<GenAIProvider, GenAICapabilities> = {
  gemini: { grounding: true, tools: true, streamingWithTools: false }, // Gemini doesn't support function calling in streaming mode
  openai: { grounding: true, tools: true, streamingWithTools: true },
  grok:   { grounding: true, tools: true, streamingWithTools: true },
  claude: { grounding: false, tools: true, streamingWithTools: true },
  deepseek: { grounding: false, tools: true, streamingWithTools: true },
};

export function mapProviderStrengthToModel(provider: GenAIProvider, strength: GenAIStrength): GenAIModel {
  switch (provider) {
    case 'gemini':
      if (strength === 'fast') return 'gemini_fast';
      if (strength === 'balanced') return 'gemini_balanced';
      if (strength === 'preview') return 'gemini_preview';
      return 'gemini_quality';
    case 'openai':
      if (strength === 'fast') return 'gpt_fast';
      if (strength === 'balanced') return 'gpt_balanced';
      return 'gpt_quality';
    case 'grok':
      if (strength === 'quality') return 'grok_quality';
      if (strength === 'balanced') return 'grok_balanced';
      return 'grok_fast';
    case 'claude':
      if (strength === 'fast') return 'claude_fast';
      // Claude quality (Opus) disabled - too expensive, fallback to balanced
      if (strength === 'quality') return 'claude_balanced';
      return 'claude_balanced';
    case 'deepseek':
      if (strength === 'quality') return 'deepseek_quality';
      // DeepSeek не имеет balanced; map to fast
      return 'deepseek_fast';
  }
}

// Context window sizes (in tokens) for each model alias
// These values must match the ContextWindow values in backend/internal/ai/model_manager.go
export const MODEL_CONTEXT_WINDOWS: Record<GenAIModel, number> = {
  // Gemini models
  'gemini_fast': 1_000_000,      // gemini-2.5-flash-lite
  'gemini_balanced': 1_000_000,  // gemini-2.5-flash
  'gemini_quality': 2_000_000,   // gemini-2.5-pro
  'gemini_preview': 2_000_000,   // gemini-3-pro-preview
  
  // OpenAI models
  'gpt_fast': 400_000,           // gpt-5-nano
  'gpt_balanced': 400_000,       // gpt-5-mini
  'gpt_quality': 400_000,        // gpt-5.1
  
  // Grok models
  'grok_fast': 2_000_000,        // grok-4-1-fast-non-reasoning
  'grok_balanced': 2_000_000,    // grok-4-1-fast-reasoning
  'grok_quality': 256_000,       // grok-4-0709
  
  // Claude models
  'claude_fast': 200_000,        // claude-haiku-4-5-20251001
  'claude_balanced': 200_000,    // claude-sonnet-4-5-20250929
  
  // DeepSeek models
  'deepseek_fast': 128_000,      // deepseek-chat
  'deepseek_quality': 128_000,   // deepseek-reasoner
};

// Helper function to get context window size for a model alias
export function getModelContextWindow(model: GenAIModel): number {
  return MODEL_CONTEXT_WINDOWS[model] || 0;
}

export interface GenAISettingsState {
  provider: GenAIProvider;
  strength: GenAIStrength;
  // Legacy: computed alias if needed in old code paths
  model: GenAIModel;
  // Stance evaluation model (separate from chat model)
  stanceProvider?: GenAIProvider;
  stanceStrength?: GenAIStrength;
  stanceModel?: GenAIModel; // default gemini_fast
  groundingEnabled: boolean;
  toolUsageEnabled: boolean;
  researchEnabled: boolean;
  temperature: number;
  maxOutputTokens: number;
  systemInstruction: string;
  enableTelegramTool: boolean;

  setProvider: (p: GenAIProvider) => void;
  setStrength: (s: GenAIStrength) => void;
  setStanceProvider: (p: GenAIProvider) => void;
  setStanceStrength: (s: GenAIStrength) => void;
  setStanceModel: (m: GenAIModel) => void;
  setGrounding: (v: boolean) => void;
  setToolUsage: (v: boolean) => void;
  setResearch: (v: boolean) => void;
  setTemperature: (v: number) => void;
  setMaxOutputTokens: (v: number) => void;
  setSystemInstruction: (v: string) => void;
  setEnableTelegramTool: (v: boolean) => void;
  save: () => void;
}

export const useGenAISettings = create<GenAISettingsState>((set, get) => {
  // Load persisted settings if present
  let persisted: Partial<GenAISettingsState> | null = null;
  try {
    const raw = typeof window !== 'undefined' ? window.localStorage.getItem('genaiSettings') : null;
    if (raw) {
      persisted = JSON.parse(raw);
    }
  } catch {}

  // Migrate old claude_quality to claude_balanced
  if (persisted && persisted.model === 'claude_quality' as any) {
    persisted.model = 'claude_balanced';
    persisted.strength = 'balanced';
  }
  if (persisted && persisted.stanceModel === 'claude_quality' as any) {
    persisted.stanceModel = 'claude_balanced';
    persisted.stanceStrength = 'balanced';
  }

  const initial: Omit<GenAISettingsState, 'setProvider' | 'setStrength' | 'setStanceProvider' | 'setStanceStrength' | 'setStanceModel' | 'setGrounding' | 'setToolUsage' | 'setResearch' | 'setTemperature' | 'setMaxOutputTokens' | 'setSystemInstruction' | 'setEnableTelegramTool' | 'save'> = {
    provider: (persisted as any)?.provider || 'gemini',
    strength: (persisted as any)?.strength || 'fast',
    model: (persisted?.model as GenAIModel) || 'gemini_fast',
    stanceProvider: (persisted as any)?.stanceProvider || 'gemini',
    stanceStrength: (persisted as any)?.stanceStrength || 'fast',
    stanceModel: (persisted?.stanceModel as GenAIModel) || 'gemini_fast',
    groundingEnabled: typeof persisted?.groundingEnabled === 'boolean' ? !!persisted?.groundingEnabled : true,
    toolUsageEnabled: typeof persisted?.toolUsageEnabled === 'boolean' ? !!persisted?.toolUsageEnabled : false,
    researchEnabled: typeof persisted?.researchEnabled === 'boolean' ? !!persisted?.researchEnabled : false,
    temperature: typeof persisted?.temperature === 'number' ? (persisted?.temperature as number) : 0.2,
    // Default to 8192 if not previously set
    maxOutputTokens: typeof persisted?.maxOutputTokens === 'number' ? (persisted?.maxOutputTokens as number) : 8192,
    systemInstruction: typeof persisted?.systemInstruction === 'string' ? (persisted?.systemInstruction as string) : '',
    enableTelegramTool: typeof persisted?.enableTelegramTool === 'boolean' ? !!persisted?.enableTelegramTool : false,
  };

  return ({
    ...initial,

    setProvider: (p) => {
      const current = get();
      const caps = providerCapabilities[p];
      // Update provider and recompute alias from current strength
      const alias = mapProviderStrengthToModel(p, current.strength);
      set({
        provider: p,
        model: alias,
        groundingEnabled: caps.grounding ? current.groundingEnabled : false,
        toolUsageEnabled: caps.tools ? current.toolUsageEnabled : false,
        enableTelegramTool: caps.tools ? current.enableTelegramTool : false,
      });
      try { get().save(); } catch {}
    },
    setStrength: (s) => {
      const current = get();
      const alias = mapProviderStrengthToModel(current.provider, s);
      set({ strength: s, model: alias });
      try { get().save(); } catch {}
    },
    setStanceProvider: (p) => {
      const current = get();
      const caps = providerCapabilities[p];
      const strength = current.stanceStrength || 'fast';
      const alias = mapProviderStrengthToModel(p, strength);
      set({ stanceProvider: p, stanceModel: alias });
      // no change to chat capabilities
      try { get().save(); } catch {}
    },
    setStanceStrength: (s) => {
      const current = get();
      const prov = current.stanceProvider || 'gemini';
      const alias = mapProviderStrengthToModel(prov, s);
      set({ stanceStrength: s, stanceModel: alias });
      try { get().save(); } catch {}
    },
    setStanceModel: (m) => set({ stanceModel: m }),
    setGrounding: (v) => {
      // If enabling grounding, disable tool usage and research
      set({ 
        groundingEnabled: v, 
        toolUsageEnabled: v ? false : get().toolUsageEnabled,
        researchEnabled: v ? false : get().researchEnabled,
      });
      try { get().save(); } catch {}
    },
    setToolUsage: (v) => {
      // If enabling tool usage, disable grounding and research
      set({ 
        toolUsageEnabled: v, 
        groundingEnabled: v ? false : get().groundingEnabled,
        researchEnabled: v ? false : get().researchEnabled,
      });
      try { get().save(); } catch {}
    },
    setResearch: (v) => {
      // If enabling research, disable grounding and tool usage
      set({ 
        researchEnabled: v, 
        groundingEnabled: v ? false : get().groundingEnabled,
        toolUsageEnabled: v ? false : get().toolUsageEnabled,
      });
      try { get().save(); } catch {}
    },
    setTemperature: (v) => set({ temperature: v }),
    setMaxOutputTokens: (v) => set({ maxOutputTokens: v }),
    setSystemInstruction: (v) => set({ systemInstruction: v }),
    setEnableTelegramTool: (v) => set({ enableTelegramTool: v }),
    save: () => {
      try {
        const s = get();
        const toPersist = {
          provider: s.provider,
          strength: s.strength,
          model: s.model,
          stanceProvider: s.stanceProvider,
          stanceStrength: s.stanceStrength,
          stanceModel: s.stanceModel,
          groundingEnabled: s.groundingEnabled,
          toolUsageEnabled: s.toolUsageEnabled,
          researchEnabled: s.researchEnabled,
          temperature: s.temperature,
          maxOutputTokens: s.maxOutputTokens,
          systemInstruction: s.systemInstruction,
          enableTelegramTool: s.enableTelegramTool,
        };
        if (typeof window !== 'undefined') {
          window.localStorage.setItem('genaiSettings', JSON.stringify(toPersist));
        }
      } catch {}
    },
  });
});


