import rawZones from '../data/globalZones.v1.json';

export type StaticZoneType =
  | 'industrial_cluster'
  | 'energy_hub'
  | 'logistics_chokepoint'
  | 'special_economic_zone'
  | 'resource_extraction'
  | 'military_industrial'
  | 'data_infrastructure'
  | 'climate_exposure';

export interface StaticZoneProperties {
  countryIso2: string;
  countryName: string;
  zoneType: StaticZoneType;
  zoneSubType?: string;
  euActs?: string[];
  name: string;
  priority: number;
  minZoom?: number;
  maxZoom?: number;
  overlayTitle?: string;
  overlayText?: string;
  tags?: string[];
  sources?: string[];
  status?: string;
  [k: string]: unknown;
}

export type StaticZoneGeometry = GeoJSON.Polygon | GeoJSON.MultiPolygon;

export type StaticZoneFeature = GeoJSON.Feature<StaticZoneGeometry, StaticZoneProperties> & {
  id: string;
};

export interface StaticZonesCollection extends GeoJSON.FeatureCollection<StaticZoneGeometry, StaticZoneProperties> {
  features: StaticZoneFeature[];
  meta?: {
    schema?: string;
    version?: number;
    updatedAt?: string;
    defaultMinZoom?: number;
    notes?: string;
  };
}

export const STATIC_ZONE_TYPE_LABELS: Record<StaticZoneType, string> = {
  industrial_cluster: 'Industrial clusters',
  energy_hub: 'Energy hubs',
  logistics_chokepoint: 'Logistics chokepoints',
  special_economic_zone: 'SEZ / free zones',
  resource_extraction: 'Resource extraction',
  military_industrial: 'Military-industrial',
  data_infrastructure: 'Data infrastructure',
  climate_exposure: 'Climate exposure',
};

export const STATIC_ZONE_DATA: StaticZonesCollection = rawZones as StaticZonesCollection;

export interface ZoneTypeCount {
  type: StaticZoneType;
  count: number;
}

export function deriveZoneTypeCounts(features: StaticZoneFeature[]): ZoneTypeCount[] {
  const counts = new Map<StaticZoneType, number>();
  for (const f of features) {
    const type = f.properties?.zoneType;
    if (!type) continue;
    counts.set(type, (counts.get(type) || 0) + 1);
  }
  return Array.from(counts.entries())
    .map(([type, count]) => ({ type, count }))
    .sort((a, b) => b.count - a.count);
}

export function formatZoneType(type: StaticZoneType): string {
  return STATIC_ZONE_TYPE_LABELS[type] || type;
}

export function zoneTypeColor(type: StaticZoneType, theme: 'dark' | 'light'): string {
  const darkPalette: Record<StaticZoneType, string> = {
    industrial_cluster: '#f59e0b',
    energy_hub: '#ef4444',
    logistics_chokepoint: '#38bdf8',
    special_economic_zone: '#14b8a6',
    resource_extraction: '#f97316',
    military_industrial: '#94a3b8',
    data_infrastructure: '#22d3ee',
    climate_exposure: '#facc15',
  };
  const lightPalette: Record<StaticZoneType, string> = {
    industrial_cluster: '#b45309',
    energy_hub: '#b91c1c',
    logistics_chokepoint: '#0369a1',
    special_economic_zone: '#0f766e',
    resource_extraction: '#c2410c',
    military_industrial: '#475569',
    data_infrastructure: '#0e7490',
    climate_exposure: '#a16207',
  };
  const palette = theme === 'dark' ? darkPalette : lightPalette;
  return palette[type] || (theme === 'dark' ? '#38bdf8' : '#0369a1');
}
