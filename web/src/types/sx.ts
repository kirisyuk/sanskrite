export type SxType = 'entity_mark' | 'monitor_metric' | 'watchlist_add' | 'link_reference';

export interface SxBase {
  v: '1';
  type: SxType;
  id: string;
  confidence?: number; // 0..1
  note?: string;
  lang?: string;
}

export interface SxEntityMark extends SxBase {
  type: 'entity_mark';
  kind: 'person' | 'org' | 'product' | 'location' | 'event' | 'other';
  name: string;
  description: string;
  aliases?: string[];
  tags?: string[];
}

export interface SxMonitorMetric extends SxBase {
  type: 'monitor_metric';
  metricName: string;
  description: string;
  source?: string;
  frequency?: 'realtime' | 'hourly' | 'daily' | 'weekly' | 'monthly' | 'quarterly';
  thresholds?: { lower?: number; upper?: number };
  unit?: string;
  dimensions?: string[];
  owner?: string;
  priority?: 'low' | 'medium' | 'high';
}

export interface SxWatchlistAdd extends SxBase {
  type: 'watchlist_add';
  objectKind: string;
  objectName: string;
  rationale: string;
  expiresAt?: string;
  priority?: 'low' | 'medium' | 'high';
}

export interface SxLinkReference extends SxBase {
  type: 'link_reference';
  fromId: string;
  toId: string;
  relation: string;
  rationale?: string;
}

export type SxInstruction = SxEntityMark | SxMonitorMetric | SxWatchlistAdd | SxLinkReference;


