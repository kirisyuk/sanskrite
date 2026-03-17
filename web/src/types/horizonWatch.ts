export interface HorizonWatchBrief {
  id: string;
  tenantId: string;
  targetId: string;
  subjectId?: string;
  canonicalPath: string;
  label: string;
  scopeType: string;
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
