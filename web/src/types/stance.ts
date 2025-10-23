export type RiskLevel = 'High' | 'Medium' | 'Low';

export interface StanceJSONResult {
  stance: string;
  riskProfile: {
    score: number; // 0..1
    level: RiskLevel; // High/Medium/Low
    rationale: string; // brief explanation in the chosen language
    triggerCategories: string[]; // selected categories from the predefined dictionary
  };
}

// --- Meta analysis (client types) ---
export type MetaAnalyzer = 'thinking' | 'biases' | 'decisions' | 'comm' | 'learning';

export interface EnumScore {
  label: string;
  intensity: number; // 0..1
  confidence?: number; // 0..1
  rationale?: string;
  evidence?: string[];
}

export interface ThinkingPatternsResult {
  depth: EnumScore;
  timeHorizon: EnumScore[];
  uncertainty: EnumScore;
  thinkingType: EnumScore[];
}

export interface BiasEntry extends EnumScore { label: string; }
export interface CognitiveBiasesResult {
  biases: BiasEntry[];
  metrics?: Record<string, number>;
}

export interface DecisionMakingResult {
  process: EnumScore;
  riskAttitude: EnumScore;
  criteriaStyle: EnumScore;
  speed: EnumScore;
}

export interface CommunicationPatternsResult {
  clarity: EnumScore;
  abstraction: EnumScore;
  contextCompleteness: EnumScore;
  feedbackLoop: EnumScore;
}

export interface LearningTrajectoryResult {
  dynamics: EnumScore;
  integration: EnumScore;
  metaAwareness: EnumScore;
}

export interface AnalyzeInteractionRef {
  refId?: string;
  inline?: { role: 'user'|'assistant'|'system'|'tool'; text: string; timestamp?: string };
  id?: string; // sentinel "0" treated as inline
  role?: 'user'|'assistant'|'system'|'tool';
  text?: string;
  timestamp?: string;
}

export interface AnalyzeRequest {
  analyzers: MetaAnalyzer[];
  interactions: AnalyzeInteractionRef[];
}

export interface AnalyzeResult {
  analyzer: MetaAnalyzer;
  result: any; // one of results above
}

export interface AnalyzeResponse {
  results: AnalyzeResult[];
  whoLeads: 'User'|'AI'|'Mixed';
  leadershipIndex: number;
  focusRetention: number;
  structureScore: number;
  version: string;
  eventId: string;
}


