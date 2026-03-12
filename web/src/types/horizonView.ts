import type { GraphViewId, PersistedGraphViewState } from '../lib/graphViews/types';

export interface HorizonViewDirective {
  contractType?: string;
  graphView?: GraphViewId;
  graphViewConfig?: Record<string, any>;
  hierarchyAxis?: string;
  highlightPaths?: Array<{
    nodePaths?: string[];
    label?: string;
    style?: string;
  }>;
  filterRelTypes?: string[];
  excludeRelTypes?: string[];
  groupBy?: string;
  focusNodePaths?: string[];
  annotations?: Array<{
    canonicalPath: string;
    key: string;
    value: string;
  }>;
  summary?: string;
  researchPlan?: {
    missingEdges?: Array<{
      fromPath: string;
      toPath: string;
      relationType: string;
    }>;
    missingNodes?: string[];
    reason?: string;
  };
}

export type HorizonGraphViewState = PersistedGraphViewState & {
  [viewId: string]: Record<string, any> | undefined;
};

export interface HorizonLensViewConfig {
  graphView?: GraphViewId;
  graphViewState?: HorizonGraphViewState;
  layoutMode?: GraphViewId | string;
  viewDirective?: HorizonViewDirective | null;
  domains?: Array<Record<string, any>>;
  [key: string]: any;
}

export interface HorizonLensRecord {
  id: string;
  tenantId: string;
  projectId: string;
  name: string;
  scope: string[];
  viewConfig?: HorizonLensViewConfig;
  alertConfig?: Record<string, any>;
  status: string;
  createdAt: string;
  updatedAt: string;
  scopeFragments?: Record<string, string>;
  resolvedQueries?: Record<string, string[]>;
  lastResolved?: string;
}
