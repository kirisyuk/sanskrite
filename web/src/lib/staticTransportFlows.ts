import rawFlows from '../data/transportFlows.v1.json';
import curatedFlows from '../data/transportFlows.curated.v1.json';

export type TransportFlowMode =
  | 'sea'
  | 'air'
  | 'road'
  | 'rail'
  | 'inland_waterway'
  | 'intermodal'
  | 'pipeline_oil'
  | 'pipeline_gas'
  | 'pipeline_h2'
  | 'pipeline_co2'
  | 'power_hvdc'
  | 'power_hvac'
  | 'ferry_roro'
  | 'internet_cable';

export interface TransportFlowProperties {
  flowMode: TransportFlowMode;
  flowSubType?: string;
  name: string;
  priority: number;
  minZoom?: number;
  maxZoom?: number;
  tags?: string[];
  sources?: string[];
  status?: string;
  [k: string]: unknown;
}

export type TransportFlowGeometry = GeoJSON.LineString | GeoJSON.MultiLineString;

export type TransportFlowFeature = GeoJSON.Feature<TransportFlowGeometry, TransportFlowProperties> & {
  id: string;
};

export interface TransportFlowsCollection extends GeoJSON.FeatureCollection<TransportFlowGeometry, TransportFlowProperties> {
  features: TransportFlowFeature[];
  meta?: {
    schema?: string;
    version?: number;
    updatedAt?: string;
    defaultMinZoom?: number;
    notes?: string;
  };
}

export interface TransportFlowModeCount {
  mode: TransportFlowMode;
  count: number;
}

export const TRANSPORT_FLOW_MODE_LABELS: Record<TransportFlowMode, string> = {
  sea: 'Sea lanes',
  air: 'Air corridors',
  road: 'Road corridors',
  rail: 'Rail corridors',
  inland_waterway: 'Inland waterways',
  intermodal: 'Intermodal links',
  pipeline_oil: 'Oil pipelines',
  pipeline_gas: 'Gas pipelines',
  pipeline_h2: 'Hydrogen pipelines',
  pipeline_co2: 'CO2 pipelines',
  power_hvdc: 'Power corridors (HVDC)',
  power_hvac: 'Power corridors (HVAC)',
  ferry_roro: 'Ferry / Ro-Ro lanes',
  internet_cable: 'Internet cables',
};

function mergeTransportFlows(
  base: TransportFlowsCollection,
  overlay: TransportFlowsCollection,
): TransportFlowsCollection {
  const byId = new Map<string, TransportFlowFeature>();
  for (const feature of base.features || []) {
    const id = String(feature?.id || '').trim();
    if (id) byId.set(id, feature);
  }
  for (const feature of overlay.features || []) {
    const id = String(feature?.id || '').trim();
    if (id) byId.set(id, feature);
  }
  return {
    ...(base as TransportFlowsCollection),
    meta: {
      ...(base.meta || {}),
      ...(overlay.meta || {}),
    },
    features: Array.from(byId.values()),
  };
}

export const STATIC_TRANSPORT_FLOW_DATA: TransportFlowsCollection = mergeTransportFlows(
  rawFlows as TransportFlowsCollection,
  curatedFlows as TransportFlowsCollection,
);

export function deriveTransportFlowModeCounts(features: TransportFlowFeature[]): TransportFlowModeCount[] {
  const counts = new Map<TransportFlowMode, number>();
  for (const f of features) {
    const mode = f.properties?.flowMode;
    if (!mode) continue;
    counts.set(mode, (counts.get(mode) || 0) + 1);
  }
  return Array.from(counts.entries())
    .map(([mode, count]) => ({ mode, count }))
    .sort((a, b) => b.count - a.count);
}

export function formatTransportFlowMode(mode: TransportFlowMode): string {
  return TRANSPORT_FLOW_MODE_LABELS[mode] || mode;
}

export function transportFlowModeColor(mode: TransportFlowMode, theme: 'dark' | 'light'): string {
  const darkPalette: Record<TransportFlowMode, string> = {
    sea: '#38bdf8',
    air: '#a78bfa',
    road: '#f59e0b',
    rail: '#22c55e',
    inland_waterway: '#06b6d4',
    intermodal: '#f43f5e',
    pipeline_oil: '#f97316',
    pipeline_gas: '#84cc16',
    pipeline_h2: '#e879f9',
    pipeline_co2: '#94a3b8',
    power_hvdc: '#f43f5e',
    power_hvac: '#fb7185',
    ferry_roro: '#60a5fa',
    internet_cable: '#2dd4bf',
  };
  const lightPalette: Record<TransportFlowMode, string> = {
    sea: '#0369a1',
    air: '#6d28d9',
    road: '#b45309',
    rail: '#15803d',
    inland_waterway: '#0e7490',
    intermodal: '#be123c',
    pipeline_oil: '#c2410c',
    pipeline_gas: '#4d7c0f',
    pipeline_h2: '#a21caf',
    pipeline_co2: '#475569',
    power_hvdc: '#be123c',
    power_hvac: '#be185d',
    ferry_roro: '#1d4ed8',
    internet_cable: '#0f766e',
  };
  return (theme === 'dark' ? darkPalette : lightPalette)[mode] || (theme === 'dark' ? '#38bdf8' : '#0369a1');
}

export function transportFlowModeStrokeWidth(mode: TransportFlowMode): number {
  switch (mode) {
    case 'sea': return 2.4;
    case 'air': return 1.8;
    case 'road': return 1.7;
    case 'rail': return 2.0;
    case 'inland_waterway': return 1.8;
    case 'intermodal': return 2.2;
    case 'pipeline_oil': return 1.9;
    case 'pipeline_gas': return 1.9;
    case 'pipeline_h2': return 1.7;
    case 'pipeline_co2': return 1.7;
    case 'power_hvdc': return 2.0;
    case 'power_hvac': return 1.7;
    case 'ferry_roro': return 1.8;
    case 'internet_cable': return 1.6;
    default: return 1.8;
  }
}

export function transportFlowModeOpacity(mode: TransportFlowMode): number {
  switch (mode) {
    case 'sea': return 0.82;
    case 'air': return 0.72;
    case 'road': return 0.74;
    case 'rail': return 0.78;
    case 'inland_waterway': return 0.72;
    case 'intermodal': return 0.86;
    case 'pipeline_oil': return 0.86;
    case 'pipeline_gas': return 0.86;
    case 'pipeline_h2': return 0.8;
    case 'pipeline_co2': return 0.75;
    case 'power_hvdc': return 0.78;
    case 'power_hvac': return 0.72;
    case 'ferry_roro': return 0.78;
    case 'internet_cable': return 0.82;
    default: return 0.75;
  }
}
