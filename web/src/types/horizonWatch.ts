export interface HorizonWatchBrief {
  id: string;
  tenantId: string;
  targetId: string;
  subjectId?: string;
  canonicalPath: string;
  label: string;
  scopeType: string;
  interestTitle?: string;
  interestQuery?: string;
  queryOverrides?: string[];
  pageFraming?: Record<string, any>;
  headline: string;
  summary: string;
  hotness: number;
  confidence: number;
  riskPosture?: string;
  dominantThemes?: string[];
  topAffectedTargets?: string[];
  topSignals?: string[];
  topEvidence?: string[];
  explanation?: string;
  forecast?: Record<string, any>;
  metadata?: Record<string, any>;
  createdAt?: string;
  updatedAt?: string;
}

export interface HorizonWatchFeedEntry {
  id?: string;
  tenantId: string;
  targetId: string;
  subjectId?: string;
  briefId?: string;
  entryType: string;
  title: string;
  summary?: string;
  canonicalPaths?: string[];
  proposalId?: string;
  decisionId?: string;
  riskJobId?: string;
  metadata?: Record<string, any>;
  createdAt: string;
}

export interface HorizonWatchPreferences {
  id?: string;
  tenantId: string;
  userId: string;
  mode: string;
  timeHorizon?: string;
  confidenceFloor?: number;
  onlyWithImpactPaths?: boolean;
  onlyTrustedSources?: boolean;
  filters?: Record<string, any>;
  createdAt?: string;
  updatedAt?: string;
}

export interface HorizonWatchDashboardWidget {
  id: string;
  kind: string;
  title: string;
  value: string;
  subtitle?: string;
  tone?: string;
  metadata?: Record<string, any>;
}

export interface HorizonWatchMapFragment {
  id: string;
  title: string;
  caption?: string;
  canonicalPaths?: string[];
  layerHints?: string[];
  scopeType?: string;
  briefId?: string;
}

export interface HorizonWatchMonitoringSnapshot {
  id: string;
  label: string;
  canonicalPath: string;
  headline: string;
  summary: string;
  riskPosture?: string;
  hotness?: number;
  confidence?: number;
  status?: string;
  updatedAt?: string;
  dominantThemes?: string[];
  topAffectedTargets?: string[];
}

export interface HorizonWatchMaterialSignalBundle {
  id: string;
  headline: string;
  summary?: string;
  canonicalPaths?: string[];
  sourceRefs?: string[];
  signalIds?: string[];
  axes?: string[];
  polarities?: string[];
  signalCount: number;
  latestObserved?: string;
}

export interface HorizonWatchMaterialSignalSubstrate {
  bundles: HorizonWatchMaterialSignalBundle[];
  signalCount: number;
  sourceCount: number;
  coverageScore: number;
  latestObserved?: string;
}

export interface HorizonWatchSubjectMonitoringSnapshot {
  subjectId: string;
  subjectKind: string;
  displayLabel: string;
  anchorCanonicalPath: string;
  currentPosition?: Record<string, any>;
  directSignals?: any[];
  propagatedExposure?: any[];
  artifactExposure?: any[];
  sourceCoverage?: Record<string, any>;
  threats?: any[];
  tailwinds?: any[];
  openQuestions?: string[];
  recommendedEscalation?: string;
  materialSubstrate: HorizonWatchMaterialSignalSubstrate;
  generatedAt: string;
}

export interface HorizonWideRiskObserverPoint {
  id: string;
  ownerSubjectId?: string;
  subjectId: string;
  canonicalPath: string;
  label: string;
  headline: string;
  summary?: string;
  hotness: number;
  confidence: number;
  riskPosture?: string;
  why?: string;
  affectedSubjects?: string[];
  backingIds?: string[];
  generatedAt?: string;
}

export interface HorizonMonitoringDigest {
  id: string;
  tenantId: string;
  scopeType: string;
  scopeId: string;
  scopeLabel?: string;
  dateKey: string;
  windowStart: string;
  windowEnd: string;
  headline: string;
  summary?: string;
  riskPosture?: string;
  priorityPoints?: Array<Record<string, any>>;
  affectedSubjects?: string[];
  coverageGaps?: string[];
  signalCount: number;
  sourceCount: number;
  subjectCount: number;
  metadata?: Record<string, any>;
  createdAt?: string;
  updatedAt?: string;
}

export interface HorizonWatchPortfolioEntry {
  id: string;
  entryType: string;
  label: string;
  canonicalPath?: string;
  subjectId?: string;
  sourceTenantId?: string;
  sourceLensId?: string;
  relatedCanonicalPaths?: string[];
  autoEnsureFocus?: boolean;
  metadata?: Record<string, any>;
  createdAt?: string;
  updatedAt?: string;
}

export interface HorizonWatchPortfolio {
  id: string;
  tenantId: string;
  userId?: string;
  name: string;
  kind: string;
  description?: string;
  status: string;
  entries?: HorizonWatchPortfolioEntry[];
  metadata?: Record<string, any>;
  createdAt?: string;
  updatedAt?: string;
}

export interface HorizonWatchPortfolioEntryView {
  entryId: string;
  entryType: string;
  label: string;
  canonicalPath?: string;
  sourceTenantId?: string;
  subjectId?: string;
  monitoringSnapshot?: HorizonWatchSubjectMonitoringSnapshot | null;
  resolutionError?: string;
}

export interface HorizonWatchPortfolioView {
  portfolio: HorizonWatchPortfolio;
  entries: HorizonWatchPortfolioEntryView[];
  priorityPoints: HorizonWideRiskObserverPoint[];
  materialSubstrate: HorizonWatchMaterialSignalSubstrate;
  coverageGaps?: string[];
  generatedAt: string;
}

export interface HorizonWatchLensAttentionSubject {
  subjectId: string;
  subjectKind: string;
  displayLabel: string;
  anchorCanonicalPath: string;
  anchorFragmentId?: string;
  status: string;
  domainModes?: string[];
  roles?: string[];
  macroAgentClass?: string;
  universePaths?: string[];
  artifactSummary?: Record<string, any>;
  sourceMixPolicy?: Record<string, any>;
  lastRun?: {
    id?: string;
    updatedAt?: string;
  };
  lastTrigger?: {
    id?: string;
    triggerType?: string;
    decision?: string;
    createdAt?: string;
  };
  promotedProposalIds?: string[];
  coverageGaps?: string[];
}

export interface HorizonWatchLensAttentionFeedEntry {
  kind: string;
  subjectId: string;
  title: string;
  summary?: string;
  canonicalPaths?: string[];
  proposalId?: string;
  decision?: string;
  createdAt: string;
  metadata?: Record<string, any>;
}

export interface HorizonWatchLensAttention {
  lensId: string;
  lensName?: string;
  subjects: HorizonWatchLensAttentionSubject[];
  feed: HorizonWatchLensAttentionFeedEntry[];
  generatedAt: string;
}

export interface HorizonWatchScenarioPreset {
  id: string;
  title: string;
  description?: string;
  contractType: string;
  domainModes?: string[];
  targetPaths?: string[];
  seedFragmentIds?: string[];
  triggerConditions?: string[];
  disconfirmers?: string[];
  parameters?: Record<string, any>;
}

export interface HorizonWatchScenarioWorkspace {
  scope: string;
  lensId?: string;
  lensName?: string;
  baselineRef?: string;
  targetPaths?: string[];
  contracts: HorizonWatchScenarioPreset[];
  generatedAt: string;
}

export interface HorizonWatchSnapshot {
  mode: string;
  scope: string;
  serviceLine: string;
  lensId?: string;
  lensName?: string;
  timeHorizon?: string;
  globalHotSlices: HorizonWatchBrief[];
  myWatch: HorizonWatchBrief[];
  feed: HorizonWatchFeedEntry[];
  preferences?: HorizonWatchPreferences | null;
  dashboards?: HorizonWatchDashboardWidget[];
  mapFragments?: HorizonWatchMapFragment[];
  monitoring?: HorizonWatchMonitoringSnapshot[];
  lensAttention?: HorizonWatchLensAttention | null;
  scenarioWorkspace?: HorizonWatchScenarioWorkspace | null;
  generatedAt: string;
  cached?: boolean;
}

export interface HorizonWatchSearchResult {
  kind: string;
  label: string;
  canonicalPath: string;
  fragmentId?: string;
}

export interface HorizonWatchPageHero {
  eyebrow?: string;
  title?: string;
  summary?: string;
  whyNow?: string;
  timeHorizon?: string;
  riskPosture?: string;
  sourceRefs?: string[];
  backingIds?: string[];
  generatedAt?: string;
}

export interface HorizonWatchPageDashboardMetric {
  id: string;
  title: string;
  value: string;
  subtitle?: string;
  tone?: string;
  deltaValue?: string;
  deltaDirection?: string;
  caption?: string;
  sourceRefs?: string[];
  backingIds?: string[];
  generatedAt?: string;
}

export interface HorizonWatchPageRiskItem {
  id: string;
  title: string;
  summary?: string;
  riskPosture?: string;
  canonicalPath?: string;
  mechanisms?: string[];
  sourceRefs?: string[];
  backingIds?: string[];
  generatedAt?: string;
}

export interface HorizonWatchPageAffectedItem {
  id: string;
  label: string;
  canonicalPath: string;
  reason?: string;
  rank?: number;
  sourceRefs?: string[];
  backingIds?: string[];
  generatedAt?: string;
}

export interface HorizonWatchPageScopeItem {
  id: string;
  label: string;
  canonicalPath: string;
  kind?: string;
  summary?: string;
  sourceRefs?: string[];
  backingIds?: string[];
  generatedAt?: string;
}

export interface HorizonWatchPageSupplyPath {
  id: string;
  title: string;
  summary?: string;
  productAxis?: string;
  anchorSupplier?: string;
  anchorCustomer?: string;
  stressScore?: number;
  bindingConfidence?: number;
  routeRefs?: string[];
  affectedZoneRefs?: string[];
  backingIds?: string[];
  generatedAt?: string;
}

export interface HorizonWatchPageMapFragment {
  id: string;
  title: string;
  caption?: string;
  canonicalPaths?: string[];
  layerHints?: string[];
  scopeType?: string;
  briefId?: string;
  backingIds?: string[];
  generatedAt?: string;
}

export interface HorizonWatchPageEvidenceItem {
  id: string;
  kind: string;
  title: string;
  summary?: string;
  canonicalPaths?: string[];
  briefId?: string;
  sourceRefs?: string[];
  generatedAt?: string;
}

export interface HorizonWatchPageSignalItem {
  id: string;
  kind: string;
  title: string;
  summary?: string;
  canonicalPaths?: string[];
  sourceLabel?: string;
  sourceKind?: string;
  backingIds?: string[];
  generatedAt?: string;
}

export interface HorizonWatchPageScenarioItem {
  id: string;
  title: string;
  description?: string;
  targetPaths?: string[];
  triggerConditions?: string[];
  disconfirmers?: string[];
  summary?: string;
  backingIds?: string[];
  generatedAt?: string;
}

export interface HorizonWatchPageSectionEnrichment {
  key: string;
  content?: string;
  sourceRefs?: string[];
  backingIds?: string[];
  model?: string;
  fallback?: boolean;
  generatedAt?: string;
}

export interface HorizonWatchPageSnapshot {
  id?: string;
  tenantId: string;
  lensId: string;
  lensName?: string;
  projectId?: string;
  timeHorizon?: string;
  interestTitle?: string;
  interestQuery?: string;
  queryOverrides?: string[];
  pageFraming?: Record<string, any>;
  buildStatus: 'NOT_BUILT' | 'READY' | 'STALE' | 'FAILED';
  buildError?: string;
  freshAt: string;
  staleAfterAt: string;
  lastRefreshRequestedAt?: string;
  hero: HorizonWatchPageHero;
  anchors?: HorizonWatchPageScopeItem[];
  reach?: HorizonWatchPageScopeItem[];
  products?: HorizonWatchPageScopeItem[];
  supplyPaths?: HorizonWatchPageSupplyPath[];
  whatIsAtRisk?: HorizonWatchPageRiskItem[];
  affected?: HorizonWatchPageAffectedItem[];
  directlyAffected?: HorizonWatchPageAffectedItem[];
  indirectlyAffected?: HorizonWatchPageAffectedItem[];
  dashboards?: HorizonWatchPageDashboardMetric[];
  mapFragments?: HorizonWatchPageMapFragment[];
  signals?: HorizonWatchPageSignalItem[];
  scenarios?: HorizonWatchPageScenarioItem[];
  evidence?: HorizonWatchPageEvidenceItem[];
  sectionEnrichments?: Record<string, HorizonWatchPageSectionEnrichment>;
  metadata?: Record<string, any>;
  createdAt?: string;
  updatedAt?: string;
}
