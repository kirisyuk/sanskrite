import { create } from 'zustand';

export type GenAIModel =
  | 'gemini_quality' // gemini-2.5-pro
  | 'gemini_fast'    // gemini-2.5-flash-lite
  | 'gpt_quality'    // gpt-5
  | 'gpt_balanced'   // gpt-5-mini
  | 'gpt_fast';      // gpt-5-nano

export interface GenAICapabilities {
  grounding: boolean;
  tools: boolean;
}

export const modelCapabilities: Record<GenAIModel, GenAICapabilities> = {
  gemini_quality: { grounding: true, tools: true },
  // 2.5 flash-lite поддерживает grounding
  gemini_fast: { grounding: true, tools: true },
  // OpenAI GPT-5 family: enable grounding (mapped to OpenAI search mode) and tools
  gpt_quality: { grounding: true, tools: true },
  gpt_balanced: { grounding: true, tools: true },
  gpt_fast: { grounding: true, tools: true },
};

export interface GenAISettingsState {
  model: GenAIModel;
  groundingEnabled: boolean;
  temperature: number;
  maxOutputTokens: number;
  systemInstruction: string;
  enableTelegramTool: boolean;

  setModel: (m: GenAIModel) => void;
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

  const initial: Omit<GenAISettingsState, 'setModel' | 'setGrounding' | 'setTemperature' | 'setMaxOutputTokens' | 'setSystemInstruction' | 'setEnableTelegramTool' | 'save'> = {
    model: (persisted?.model as GenAIModel) || 'gemini_quality',
    groundingEnabled: typeof persisted?.groundingEnabled === 'boolean' ? !!persisted?.groundingEnabled : true,
    temperature: typeof persisted?.temperature === 'number' ? (persisted?.temperature as number) : 0.2,
    // Default to 8192 if not previously set
    maxOutputTokens: typeof persisted?.maxOutputTokens === 'number' ? (persisted?.maxOutputTokens as number) : 8192,
    systemInstruction: typeof persisted?.systemInstruction === 'string' ? (persisted?.systemInstruction as string) : '',
    enableTelegramTool: typeof persisted?.enableTelegramTool === 'boolean' ? !!persisted?.enableTelegramTool : false,
  };

  return ({
    ...initial,

    setModel: (m) => {
      const caps = modelCapabilities[m];
      const current = get();
      // Auto-disable unsupported settings when switching model
      set({
        model: m,
        groundingEnabled: caps.grounding ? current.groundingEnabled : false,
        enableTelegramTool: caps.tools ? current.enableTelegramTool : false,
      });
    },
    setGrounding: (v) => set({ groundingEnabled: v }),
    setTemperature: (v) => set({ temperature: v }),
    setMaxOutputTokens: (v) => set({ maxOutputTokens: v }),
    setSystemInstruction: (v) => set({ systemInstruction: v }),
    setEnableTelegramTool: (v) => set({ enableTelegramTool: v }),
    save: () => {
      try {
        const s = get();
        const toPersist = {
          model: s.model,
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


