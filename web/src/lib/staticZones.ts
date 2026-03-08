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
    industrial_cluster: '#7b8ea4',
    energy_hub: '#8f7a6a',
    logistics_chokepoint: '#5f8fa8',
    special_economic_zone: '#5d9a8c',
    resource_extraction: '#8d7663',
    military_industrial: '#7c8798',
    data_infrastructure: '#5f90a1',
    climate_exposure: '#8d8864',
  };
  const lightPalette: Record<StaticZoneType, string> = {
    industrial_cluster: '#6b7d92',
    energy_hub: '#786657',
    logistics_chokepoint: '#4f7892',
    special_economic_zone: '#4f8378',
    resource_extraction: '#775f4f',
    military_industrial: '#647183',
    data_infrastructure: '#4d7788',
    climate_exposure: '#76714f',
  };
  const palette = theme === 'dark' ? darkPalette : lightPalette;
  return palette[type] || (theme === 'dark' ? '#38bdf8' : '#0369a1');
}
