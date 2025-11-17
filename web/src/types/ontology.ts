/**
 * Ontology types matching backend/internal/models/ontology.go
 * 
 * These types represent the canonical ontology model for entities, concepts,
 * relations, claims, events, metrics, trends, scenarios, and more.
 */

// ============================================================================
// Base Ontology Fields (inherited by all ontology types)
// ============================================================================

export interface BaseOntologyFields {
  id: string;
  tenantId: string; // "global" or "system" for Wikidata, specific tenant ID otherwise
  version: number;
  createdAt: string; // ISO 8601
  updatedAt: string; // ISO 8601
  createdBy: string; // system/user
  updatedBy: string;
  labels: Record<string, string>; // lang -> string
  description?: string;
  tags?: string[];
  status: 'active' | 'deprecated' | 'draft' | 'archived';
}

// ============================================================================
// 1. Entity (Сущность)
// ============================================================================

export type EntityType =
  | 'Person'
  | 'Organization'
  | 'Product'
  | 'Location'
  | 'EventEntity'
  | 'Document'
  | 'Facility'
  | 'SupplyChainNode'
  | 'GovernmentProject'
  | 'IndexMetric'
  | 'Other';

export type DataQuality = 'low' | 'medium' | 'high';

export interface KBReference {
  kb: string; // Wikidata, DBpedia, etc.
  externalId: string;
  confidence: number; // 0..1
  lastSync?: string; // ISO 8601
}

export interface Location {
  name?: string;
  iso6709?: string; // ISO 6709 coordinates
}

export interface Governance {
  ownerTeam?: string;
  dataQuality?: DataQuality;
}

export interface Entity extends BaseOntologyFields {
  canonicalName: string;
  type: EntityType;
  aliases?: string[];
  kbRefs?: KBReference[];
  identifiers?: Record<string, string>; // scheme -> value
  attributes?: Record<string, any>; // flexible properties
  validFrom?: string; // ISO 8601
  validTo?: string; // ISO 8601
  homeLocation?: Location;
  governance?: Governance;
}

// ============================================================================
// 2. Concept (Концепция)
// ============================================================================

export type ConceptKind =
  | 'Domain'
  | 'Process'
  | 'Strategy'
  | 'TrendConcept'
  | 'Risk'
  | 'Opportunity'
  | 'RelationType'
  | 'Policy'
  | 'Other';

export interface ConceptRelation {
  type: string; // isA, partOf, etc.
  targetConceptId: string;
}

export interface ConceptHierarchy {
  parentId?: string;
  path?: string[]; // [id, id, ...]
}

export interface Concept extends BaseOntologyFields {
  name: string;
  kind: ConceptKind;
  hierarchy?: ConceptHierarchy;
  relationsToConcepts?: ConceptRelation[];
  kbRefs?: KBReference[];
}

// ============================================================================
// 3. Relation (Связь)
// ============================================================================

export type RelationDirection = 'subjectToObject' | 'bidirectional';

export interface Provenance {
  sourceIds: string[];
  observedAt?: string; // ISO 8601
  method?: string; // extracted, curated, imported
}

export interface Relation extends BaseOntologyFields {
  subjectId: string;
  predicate: string; // controlled vocabulary
  objectId: string;
  direction: RelationDirection;
  qualifiers?: Record<string, any>;
  confidence: number; // 0..1
  validFrom?: string; // ISO 8601
  validTo?: string; // ISO 8601
  provenance: Provenance;
}

// ============================================================================
// 4. Claim / Fact (Факт/Утверждение)
// ============================================================================

export type ReferenceKind = 'Entity' | 'Concept' | 'Literal';

export type Stance = 'supports' | 'contradicts' | 'neutral';

export type ExtractionMethod = 'NLP' | 'human' | 'API' | 'KBAlign';

export interface SubjectRef {
  id: string;
  kind: ReferenceKind;
}

export interface ObjectRef {
  kind: ReferenceKind;
  value?: any; // for Literal
  id?: string; // for Entity/Concept
}

export interface TemporalInfo {
  asOf?: string; // ISO 8601
  start?: string; // ISO 8601
  end?: string; // ISO 8601
}

export interface ClaimProvenance {
  sourceIds: string[];
  method: ExtractionMethod;
  extractedAt: string; // ISO 8601
  evidenceAnchors?: string[]; // strings/offset
  license?: string;
}

export interface Claim extends BaseOntologyFields {
  subjectRef: SubjectRef;
  predicate: string; // controlled vocabulary
  objectRef: ObjectRef;
  temporal?: TemporalInfo;
  qualifiers?: Record<string, any>;
  confidence: number; // 0..1
  stance: Stance;
  provenance: ClaimProvenance;
}

// ============================================================================
// 5. Event (Событие)
// ============================================================================

export interface Participant {
  entityId: string;
  role: string; // agent, target, mediator, etc.
}

export interface EventLinks {
  claims?: string[];
  metrics?: string[];
  relatedEvents?: string[];
}

export interface Event extends BaseOntologyFields {
  type: string; // from event type dictionary
  title: string;
  occurredAt?: string; // ISO 8601
  occurredStart?: string; // ISO 8601
  occurredEnd?: string; // ISO 8601
  location?: Location;
  participants?: Participant[];
  outcomeSummary?: string;
  links?: EventLinks;
  confidence: number; // 0..1
  provenance: Provenance;
}

// ============================================================================
// 6. MetricState (Состояние метрики)
// ============================================================================

export type SubjectRefKind = 'Entity' | 'Event' | 'Market';

export type AggregationType = 'point' | 'mean' | 'median' | 'sum' | 'yoy' | 'qoq';

export interface MetricSubjectRef {
  id: string;
  kind: SubjectRefKind;
}

export interface ErrorMargin {
  low?: number;
  high?: number;
  value?: number; // for symmetric interval
}

export interface TimePeriod {
  start?: string; // ISO 8601
  end?: string; // ISO 8601
}

export interface MetricState extends BaseOntologyFields {
  metricName: string; // from controlled vocabulary
  subjectRef: MetricSubjectRef;
  value: number | string; // number or string for categories
  unit: string; // %, USD, ms, indexPoint
  timestamp?: string; // ISO 8601
  period?: TimePeriod;
  aggregation: AggregationType;
  errorMargin?: ErrorMargin;
  confidence: number; // 0..1
  provenance: Provenance;
}

// ============================================================================
// 7. Trend (Тренд)
// ============================================================================

export type TrendDirection = 'increasing' | 'decreasing' | 'stable' | 'volatile';

export type TrendStage = 'Introduction' | 'Rise' | 'Peak' | 'Decline' | 'Obsolescence';

export interface TrendFocus {
  metricName: string;
  subjectScope?: {
    ids?: string[];
    filter?: string;
  };
}

export interface TrendEvidence {
  metricStates?: string[];
  events?: string[];
  claims?: string[];
}

export interface TrendTimeHorizon {
  lookback: string; // period
  forecastHorizon?: string;
}

export interface TrendState {
  atTime: string; // ISO 8601
  stage: TrendStage;
  level?: number;
  velocity?: number; // dM/dt
  acceleration?: number; // d²M/dt²
  volatility?: number;
  confidence: number; // 0..1
  evidenceRefs?: {
    metricStates?: string[];
    events?: string[];
  };
}

export interface Trend extends BaseOntologyFields {
  name: string;
  focus: TrendFocus;
  direction: TrendDirection;
  states: TrendState[]; // ordered by time
  evidence?: TrendEvidence;
  confidence: number; // 0..1
  timeHorizon: TrendTimeHorizon;
}

// ============================================================================
// 8. CausalLink (Причинно-следственная связь)
// ============================================================================

export type CausalRefKind = 'Event' | 'Trend' | 'TrendState' | 'MetricState';

export type CausalSign = 'positive' | 'negative';

export interface CausalRef {
  id: string;
  kind: CausalRefKind;
}

export interface CausalCondition {
  left: string; // expression
  op: string; // operator
  right: any; // value
}

export interface CausalLag {
  value?: string; // duration
  distribution?: string;
  unit?: string;
}

export interface CausalEvidence {
  claims?: string[];
  events?: string[];
  metrics?: string[];
  method?: string;
}

export interface CausalValidity {
  validFrom?: string; // ISO 8601
  validTo?: string; // ISO 8601
}

export interface CausalLink extends BaseOntologyFields {
  causeRef: CausalRef;
  effectRef: CausalRef;
  sign: CausalSign;
  strength: number; // 0..1
  probability: number; // 0..1
  lag?: CausalLag;
  conditions?: CausalCondition[];
  assumptions?: string[];
  evidence?: CausalEvidence;
  validity?: CausalValidity;
}

// ============================================================================
// 9. Scenario (Сценарий)
// ============================================================================

export type ConditionRefKind = 'Event' | 'TrendState' | 'MetricState';

export interface ScenarioCondition {
  ref: string; // id
  kind: ConditionRefKind;
  expr?: string; // expression
}

export interface ScenarioPathway {
  step: number;
  linkRef?: string; // CausalLink ID
  note?: string;
}

export type ImpactDirection = 'increase' | 'decrease' | 'stable';

export interface ImpactTargetRef {
  id: string;
  kind: ReferenceKind;
}

export interface ExpectedChange {
  delta?: number;
  percent?: number;
  direction?: ImpactDirection;
}

export interface ScenarioImpact {
  targetRef: ImpactTargetRef;
  metricName?: string;
  expectedChange: ExpectedChange;
  affectedCausalLinkId?: string;
}

export interface ScenarioHorizon {
  start?: string; // ISO 8601
  end?: string; // ISO 8601
  duration?: string; // duration
}

export interface MonitoringIndicator {
  ref: string | string; // id or metricName
  threshold?: number;
  direction?: ImpactDirection;
}

export interface Scenario extends BaseOntologyFields {
  title: string;
  description?: string;
  horizon: ScenarioHorizon;
  probability: number; // 0..1
  conditions?: ScenarioCondition[];
  pathways?: ScenarioPathway[];
  impacts?: ScenarioImpact[];
  actionableRecommendations?: string[]; // ActionableItem IDs
  assumptions?: string[];
  confidence: number; // 0..1
  monitoringIndicators?: MonitoringIndicator[];
}

// ============================================================================
// 10. ActionableItem (Действенный элемент)
// ============================================================================

export type Priority = 'low' | 'medium' | 'high' | 'critical';

export type ActionableStatus =
  | 'proposed'
  | 'approved'
  | 'inProgress'
  | 'blocked'
  | 'done'
  | 'discarded';

export type ReviewCadence = 'weekly' | 'biweekly' | 'monthly';

export interface Objective {
  linksTo: string | { scenarioId?: string; trendId?: string }; // id or object
  goalText?: string;
}

export interface Owner {
  role?: string;
  responsibleId?: string;
}

export interface DueDate {
  date?: string; // ISO 8601
  period?: TimePeriod;
  timeboxWeeks?: number;
}

export interface SuccessMetric {
  name: string;
  target: number | string;
  by: string; // ISO 8601
}

export interface ActionableRisk {
  riskRef?: string;
  description?: string;
  mitigation?: string;
}

export interface CostEstimate {
  amount?: number;
  currency?: string;
  effortPoints?: number;
}

export interface FeedbackLoop {
  reviewCadence: ReviewCadence;
  lastReview?: string; // ISO 8601
  outcomeNotes?: string;
}

export interface ActionableProvenance {
  initiatedFrom: string | { scenarioId?: string; trendId?: string; claimId?: string }; // Scenario/Trend/Claim ID
  createdBy: string;
  createdAt: string; // ISO 8601
}

export interface ActionableItem extends Omit<BaseOntologyFields, 'status'> {
  description: string;
  objective: Objective;
  owner?: Owner;
  priority: Priority;
  due?: DueDate;
  expectedImpact?: ScenarioImpact[];
  successMetrics?: SuccessMetric[];
  prerequisites?: string[];
  risks?: ActionableRisk[];
  costEstimate?: CostEstimate;
  status: ActionableStatus;
  feedbackLoop?: FeedbackLoop;
  provenance: ActionableProvenance;
}

// ============================================================================
// 11. Source / Trust (Источник и доверие)
// ============================================================================

export type SourceType =
  | 'news'
  | 'report'
  | 'KB'
  | 'api'
  | 'dataset'
  | 'humanExpert'
  | 'chatMessage';

export type UpdateFrequency = 'realtime' | 'daily' | 'weekly' | 'monthly' | 'adHoc';

export type BiasDimension = 'political' | 'regional' | 'commercial';

export interface SourceBias {
  dimension: BiasDimension;
  polarity: number; // -1..1
  notes?: string;
}

export interface Source extends BaseOntologyFields {
  name: string;
  type: SourceType;
  url?: string;
  license?: string;
  bias?: SourceBias;
  reliabilityPrior: number; // 0..1
  updateFrequency?: UpdateFrequency;
  lastChecked?: string; // ISO 8601
}

export interface TrustEvidenceHistory {
  claimId?: string;
  wasAccurate?: boolean;
  error?: number;
  checkedAt: string; // ISO 8601
}

export interface PolicyFlag {
  name: string;
  value: boolean;
}

export interface Trust extends BaseOntologyFields {
  sourceId: string;
  currentScore: number; // 0..1
  decayRate?: number;
  evidenceHistory?: TrustEvidenceHistory[];
  policyFlags?: PolicyFlag[];
  notes?: string;
}

// ============================================================================
// 12. UncertaintyModel (Неопределённость)
// ============================================================================

export type UncertaintyType =
  | 'conflict'
  | 'incompleteness'
  | 'temporalLag'
  | 'modelRisk'
  | 'ambiguity';

export type Materiality = 'low' | 'medium' | 'high' | 'critical';

export type UncertaintyStatus =
  | 'open'
  | 'investigating'
  | 'mitigated'
  | 'resolved'
  | 'deferred';

export type AnswerMode = 'range' | 'caveat' | 'withhold';

export interface AffectedRef {
  id: string;
  kind: ReferenceKind; // Claim, Event, MetricState, Trend, CausalLink, Scenario
}

export interface UncertaintyRepresentation {
  interval?: ErrorMargin;
  distribution?: string;
  confidence?: number;
}

export interface ImpactOnAnswers {
  modes: AnswerMode[];
  affectedScenarios?: string[];
}

export interface ResolutionPlan {
  actionableItemId?: string;
  due?: string; // ISO 8601
}

export interface UncertaintyModel extends Omit<BaseOntologyFields, 'status'> {
  type: UncertaintyType;
  affectedRefs: AffectedRef[];
  representation: UncertaintyRepresentation;
  materiality: Materiality;
  impactOnAnswers: ImpactOnAnswers;
  status: UncertaintyStatus;
  owner?: string;
  resolutionPlan?: ResolutionPlan;
  lastReviewedAt?: string; // ISO 8601
  notes?: string;
}

