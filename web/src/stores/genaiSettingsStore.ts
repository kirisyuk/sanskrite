import { create } from 'zustand';

export type GenAIProvider = 'gemini' | 'openai' | 'grok' | 'claude' | 'deepseek';
export type GenAIStrength = 'fast' | 'balanced' | 'quality';

// Backward-compat alias type (kept for minimal refactor use sites)
export type GenAIModel =
  | 'gemini_quality' // gemini-2.5-pro
  | 'gemini_fast'    // gemini-2.5-flash-lite
  | 'gemini_balanced'// gemini-2.5-flash
  | 'gpt_quality'    // gpt-5
  | 'gpt_balanced'   // gpt-5-mini
  | 'gpt_fast'       // gpt-5-nano
  | 'grok_quality'   // grok-4-0709
  | 'grok_fast'      // grok-4-fast-reasoning
  | 'claude_quality' // claude-opus-4-1-20250805
  | 'claude_balanced'// claude-sonnet-4-5-20250929
  | 'claude_fast'    // claude-haiku-4-5-20251001
  | 'deepseek_quality' // deepseek-reasoner
  | 'deepseek_fast'; // deepseek-chat

export interface GenAICapabilities {
  grounding: boolean;
  tools: boolean;
}

export const providerCapabilities: Record<GenAIProvider, GenAICapabilities> = {
  gemini: { grounding: true, tools: true },
  openai: { grounding: true, tools: true },
  grok:   { grounding: true, tools: true },
  claude: { grounding: false, tools: true },
  deepseek: { grounding: false, tools: true },
};

export function mapProviderStrengthToModel(provider: GenAIProvider, strength: GenAIStrength): GenAIModel {
  switch (provider) {
    case 'gemini':
      if (strength === 'fast') return 'gemini_fast';
      if (strength === 'balanced') return 'gemini_balanced';
      return 'gemini_quality';
    case 'openai':
      if (strength === 'fast') return 'gpt_fast';
      if (strength === 'balanced') return 'gpt_balanced';
      return 'gpt_quality';
    case 'grok':
      if (strength === 'quality') return 'grok_quality';
      // Grok не имеет balanced; map to fast
      return 'grok_fast';
    case 'claude':
      if (strength === 'fast') return 'claude_fast';
      if (strength === 'balanced') return 'claude_balanced';
      return 'claude_quality';
    case 'deepseek':
      if (strength === 'quality') return 'deepseek_quality';
      // DeepSeek не имеет balanced; map to fast
      return 'deepseek_fast';
  }
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

  const initial: Omit<GenAISettingsState, 'setProvider' | 'setStrength' | 'setStanceProvider' | 'setStanceStrength' | 'setStanceModel' | 'setGrounding' | 'setTemperature' | 'setMaxOutputTokens' | 'setSystemInstruction' | 'setEnableTelegramTool' | 'save'> = {
    provider: (persisted as any)?.provider || 'gemini',
    strength: (persisted as any)?.strength || 'fast',
    model: (persisted?.model as GenAIModel) || 'gemini_fast',
    stanceProvider: (persisted as any)?.stanceProvider || 'gemini',
    stanceStrength: (persisted as any)?.stanceStrength || 'fast',
    stanceModel: (persisted?.stanceModel as GenAIModel) || 'gemini_fast',
    groundingEnabled: typeof persisted?.groundingEnabled === 'boolean' ? !!persisted?.groundingEnabled : true,
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
    setGrounding: (v) => set({ groundingEnabled: v }),
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


