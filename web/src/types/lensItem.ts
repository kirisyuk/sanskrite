/**
 * LensItem types matching backend schema
 * 
 * NOTE: These types are manually maintained to match:
 * - backend/internal/horizon/models/lens_item.go
 * 
 * These are NOT auto-generated because the LensItem model is in the `horizon/models` package,
 * not in the main `models` package which is used for JSON Schema generation.
 * 
 * When updating backend LensItem struct, manually update these types.
 */

export interface LensItemMetadata {
  pinned?: boolean;
  notes?: string[];
  visibility?: 'visible' | 'hidden' | 'collapsed';
  color?: string; // For custom color coding
}

export interface LensItemPosition {
  x: number;
  y: number;
}

export type LensItemKind = 'node' | 'edge' | 'viewport' | 'layout';

export type DependencyType = 
  | 'SUPPLIES_TO'
  | 'LOCATED_IN'
  | 'OWNS'
  | 'PRODUCES'
  | 'OPERATES_IN'
  | 'REGULATES'
  | 'COMPETES_WITH'
  | 'DEPENDS_ON'
  | 'INFLUENCES';

export type EdgeDirection = 'forward' | 'reverse' | 'bidirectional';

export interface LensItem {
  id: string;
  tenantId: string;
  projectId: string;
  lensId: string;
  kind: LensItemKind;
  subkind?: string; // For nodes: "fragment" | "scope" | ...; For edges: DependencyType
  
  // For kind="node"
  fragmentId?: string;
  canonicalPath?: string;
  label?: string; // labels["en"]
  type?: string; // FragmentType
  state?: Record<string, number>; // Key metrics for display
  status?: string;
  
  // UI state for nodes (boolean fields instead of subkind)
  expanded?: boolean;
  collapsed?: boolean;
  critical?: boolean;
  
  // Position for graph (priority)
  position?: LensItemPosition;
  
  // For kind="edge"
  sourceFragmentId?: string;
  targetFragmentId?: string;
  edgeType?: DependencyType; // Duplicates subkind for convenience
  criticality?: number;
  amplification?: number;
  latencyHours?: number;
  
  // Edge direction for visualization
  direction?: EdgeDirection;
  
  // For kind="viewport" - viewport settings (zoom, pan, filters)
  viewportConfig?: Record<string, any>;
  
  // For kind="layout" - saved layout
  layoutData?: Record<string, any>;
  
  // Metadata
  metadata?: LensItemMetadata;
  createdAt: string;
  updatedAt: string;
}







