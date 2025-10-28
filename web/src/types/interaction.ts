/**
 * Core Interaction types matching backend schema
 * 
 * NOTE: These types are manually maintained to match:
 * - backend/internal/repo/interactions.go
 * 
 * These are NOT auto-generated because the Interaction model is in the `repo` package,
 * not in `models` package which is used for JSON Schema generation.
 * 
 * When updating backend Interaction struct, manually update these types.
 */

export interface InteractionMetadata {
  pinned?: boolean;
  notes?: string[];
}

export interface ParticipantRef {
  participantId: string;
  kind: 'human' | 'ai' | 'system' | 'tool';
  displayName?: string;
}

export interface AgentProfileSnapshot {
  provider: string;
  model: string;
  version?: string;
  capabilities?: Record<string, boolean>;
  params?: Record<string, any>;
}

export interface UsageMeta {
  promptTokens?: number;
  outputTokens?: number;
  totalTokens?: number;
  reasoningTokens?: number;
  toolUsePromptTokens?: number;
}

export interface InteractionRelation {
  type: 'answers' | 'refines' | 'evaluates';
  targetId: string;
}

export interface GenerationParams {
  temperature?: number;
  maxOutputTokens?: number;
  groundingEnabled?: boolean;
}

export interface HighRisk {
  level: 'High' | 'Medium' | 'Low';
  score: number;
  rationale?: string;
  triggers?: string[];
}

export interface Interaction {
  id: string;
  tenantId: string;
  projectId: string;
  kind: string; // e.g., "discovery.answer", "user", "assistant", "system", "note"
  subkind?: string;
  content: string;
  contentSanitized?: string;
  summary?: string;
  // Sections
  sectionId?: string;
  model?: string;
  usage?: UsageMeta;
  author?: ParticipantRef;
  recipients?: string[];
  replyTo?: string;
  agentProfile?: AgentProfileSnapshot;
  evaluations?: Record<string, any>;
  metadata?: InteractionMetadata;
  relations?: InteractionRelation[];
  language?: string;
  systemInstructionApplied?: string;
  generationParams?: GenerationParams;
  highRisk?: HighRisk;
  originalMessageId?: string;
  coreDocIds?: string[];
  searchQueries?: string[];
  sources?: string[];
  createdAt: string;
}

export type InteractionKind = 'user' | 'assistant' | 'system' | 'note' | 'section_start' | 'section_end';

