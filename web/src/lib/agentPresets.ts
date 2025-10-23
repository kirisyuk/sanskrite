export type AgentPreset = {
  id: string;
  displayName: string;
  description: string;
  expectedOutcome?: string;
  useCases?: string[];
  provider: 'gemini' | 'openai' | 'grok';
  modelAlias: 'gemini_quality' | 'gemini_balanced' | 'gemini_fast' | 'gpt_quality' | 'gpt_balanced' | 'gpt_fast' | 'grok_quality' | 'grok_fast';
  capabilities?: { tools?: boolean; webSearch?: boolean; reasoning?: boolean; refinePrevious?: boolean };
  params?: { temperature?: number; systemInstruction?: string };
  // If true, the primary model should immediately respond to this agent's reply
  requireImmediatePrimaryReply?: boolean;
};

// Two predefined presets available on the frontend without requiring backend storage.
// These can be used by UI (e.g., Bring In) to select a model for the next turn.
export const AGENT_PRESETS: AgentPreset[] = [
  {
    id: 'balanced-analyst',
    displayName: 'Balanced Analyst',
    description: 'A measured analyst focusing on clarity and balanced reasoning.',
    expectedOutcome: 'Concise, well‑structured synthesis with balanced trade‑offs and explicit uncertainties.',
    useCases: ['Draft neutral executive summary', 'Compare alternatives with pros/cons', 'Clarify assumptions and data gaps'],
    provider: 'gemini',
    modelAlias: 'gemini_balanced',
    capabilities: { tools: true, webSearch: true, reasoning: true, refinePrevious: true },
    params: { temperature: 0.4 },
    requireImmediatePrimaryReply: false,
  },
  {
    id: 'socratic-counterpoint',
    displayName: 'Socratic Counterpoint',
    description: 'Challenges assumptions and brings counter‑arguments to deepen the discussion.',
    expectedOutcome: 'Constructive counter‑position that exposes hidden risks and strengthens reasoning.',
    useCases: ['Stress‑test a proposal', 'Identify missing alternatives', 'Reveal hidden premises and biases'],
    provider: 'openai',
    modelAlias: 'gpt_quality',
    capabilities: { tools: true, webSearch: true, reasoning: true, refinePrevious: true },
    params: { temperature: 0.3, systemInstruction: 'Adopt a Socratic method; question assumptions constructively.' },
    requireImmediatePrimaryReply: true,
  },
  {
    id: 'thoughtful-opponent',
    displayName: 'Thoughtful Opponent',
    description: 'Presents a respectful, well‑reasoned opposing view that tests robustness.',
    expectedOutcome: 'Respectful critique that integrates strongest opposing arguments with citations when possible.',
    useCases: ['Prepare for stakeholder pushback', 'Test robustness before publishing', 'Map risks and counter‑measures'],
    provider: 'openai',
    modelAlias: 'gpt_quality',
    capabilities: { tools: true, webSearch: true, reasoning: true, refinePrevious: true },
    params: {
      temperature: 0.3,
      systemInstruction:
        'Adopt a thoughtful, good-faith opposing stance. Surface the strongest counter-arguments, risks and trade-offs; avoid strawmen; integrate evidence if available; remain concise and constructive. You may question even deeply entrenched narratives, claims, and intentions when doing so does not contradict overall logic; challenge premises rigorously but fairly.'
    },
    requireImmediatePrimaryReply: true,
  },
];

export const getDefaultAgentPreset = (): AgentPreset => AGENT_PRESETS[0];


