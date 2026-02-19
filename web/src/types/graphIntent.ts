export const GRAPH_INTENT_VERSION_V1 = 'graph-intent.v1' as const;

export type GraphIntentClass = 'create' | 'update' | 'delete' | 'merge' | 'transform' | 'mixed' | 'unknown';
export type GraphIntentApplyMode = 'patch' | 'dry-run';
export type GraphIntentDirection = 'forward' | 'reverse' | 'bidirectional';

export interface GraphIntentPlanRequest {
  version: typeof GRAPH_INTENT_VERSION_V1;
  rawQuery: string;
  queryLanguage?: string;
  executionLanguage?: 'en';
  snapshot: {
    nodes?: Array<{
      nodeId?: string;
      fragmentId?: string;
      canonicalPath?: string;
      label?: string;
      type?: string;
      position?: { x: number; y: number };
    }>;
    edges?: Array<{
      edgeId?: string;
      sourceNodeId?: string;
      targetNodeId?: string;
      sourceFragmentId?: string;
      targetFragmentId?: string;
      relationType?: string;
      direction?: GraphIntentDirection;
    }>;
    selectedNodeIds?: string[];
    visibleNodeIds?: string[];
    viewState?: {
      zoom?: number;
      filters?: Record<string, any>;
    };
  };
  options?: {
    allowSearchGrounding?: boolean;
    allowInternalGrounding?: boolean;
    maxNewNodes?: number;
    maxNewEdges?: number;
  };
}

export interface GraphIntentNodeSpec {
  tempId?: string;
  label: string;
  type: string;
  canonicalPathHint?: string;
  state?: Record<string, any>;
  metadata?: Record<string, any>;
}

export interface GraphIntentEdgeSpec {
  tempId?: string;
  fromTempId?: string;
  toTempId?: string;
  fromNodeId?: string;
  toNodeId?: string;
  relationType: string;
  direction?: GraphIntentDirection;
  confidence?: number;
  qualifiers?: Record<string, any>;
  metrics?: Record<string, any>;
}

export interface GraphIntentPlanResponse {
  version: typeof GRAPH_INTENT_VERSION_V1;
  normalizedEnglishQuery: string;
  intentClass: GraphIntentClass;
  confidence: number;
  create: {
    groupings?: Array<{
      groupingId: string;
      groupingKind: string;
      title: string;
      anchor?: { canonicalPathHint?: string };
      entities?: GraphIntentNodeSpec[];
      relations?: GraphIntentEdgeSpec[];
      evidence?: {
        internalRefs?: string[];
        searchRefs?: string[];
      };
    }>;
    nodes?: GraphIntentNodeSpec[];
    edges?: GraphIntentEdgeSpec[];
  };
  update: {
    nodePatches?: Array<{
      patchId?: string;
      nodeId?: string;
      tempId?: string;
      fragmentId?: string;
      canonicalPath?: string;
      label?: string;
      type?: string;
      statePatch?: Record<string, any>;
      metadataPatch?: Record<string, any>;
    }>;
    edgePatches?: Array<{
      patchId?: string;
      edgeId?: string;
      sourceNodeId?: string;
      targetNodeId?: string;
      sourceFragmentId?: string;
      targetFragmentId?: string;
      relationType?: string;
      direction?: GraphIntentDirection;
      attributes?: Record<string, any>;
    }>;
    relationRewrite?: Array<{
      rewriteId?: string;
      match: {
        relationType?: string;
        sourceNodeIds?: string[];
        targetNodeIds?: string[];
      };
      replaceWith: {
        relationType: string;
        direction?: GraphIntentDirection;
      };
      reason?: string;
    }>;
  };
  delete?: {
    nodeIds?: string[];
    edgeIds?: string[];
  };
  merge?: {
    primaryNodeId?: string;
    duplicateNodeIds?: string[];
    mergeFragmentData?: boolean;
  };
  transform: {
    deriveChains?: Array<{
      chainId?: string;
      fromNodeId?: string;
      toNodeId?: string;
      constraints?: {
        allowedRelationTypes?: string[];
        maxHops?: number;
      };
      proposedPath?: string[];
      proposedEdges?: string[];
      confidence?: number;
    }>;
    restructure?: Array<{
      restructureId?: string;
      scopeNodeIds?: string[];
      mode?: string;
      params?: Record<string, any>;
    }>;
  };
  explanations?: string[];
  warnings?: string[];
  requiresConfirmation: boolean;
}

export interface GraphIntentApplyRequest {
  version: typeof GRAPH_INTENT_VERSION_V1;
  plan: GraphIntentPlanResponse;
  applyMode?: GraphIntentApplyMode;
  idempotencyKey: string;
}

export interface GraphIntentApplyResponse {
  applied: boolean;
  createdNodeCount: number;
  createdEdgeCount: number;
  updatedNodeCount: number;
  updatedEdgeCount: number;
  unresolvedItems?: Array<{
    tempId?: string;
    reason: string;
    suggestedAction?: string;
  }>;
  warnings?: string[];
  actionPreview?: {
    actions: Array<{
      actionId: string;
      kind: string;
      scope: string;
      refId?: string;
      payload?: Record<string, any>;
    }>;
  };
}
