// === Auto-generated types from backend Go models ===
// Generated via: npm run gen:types (from JSON Schema files)
// Source: backend/internal/models/
export type JSONSchema = Record<string, unknown>;

// JSON Schemas
export { default as UserSchema } from "../types/schemas/User.schema.json";
export { default as ProjectSchema } from "../types/schemas/Project.schema.json";
export { default as DataSourceSchema } from "../types/schemas/DataSource.schema.json";

// Auto-generated TypeScript types
export type {
  User,
  Project,
  DataSource,
} from './generated';

// === Manually maintained types ===
// These types are manually maintained because they come from backend/internal/repo
// which is not part of the JSON Schema generation pipeline
export type {
  Interaction,
  InteractionMetadata,
  InteractionKind,
  ParticipantRef,
  AgentProfileSnapshot,
  UsageMeta,
  InteractionRelation,
  GenerationParams,
  HighRisk,
} from './interaction';
