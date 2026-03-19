import axios from 'axios';

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080';

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: { 'Content-Type': 'application/json' },
});

export interface CriticalChainMaterial {
  id: string;
  name: string;
  aliases?: string[];
  category: string;
  criticalityScore: number;
  riskScore: number;
  riskHistory?: Array<{ timestamp: string; value: number }>;
  riskDelta7d?: number;
  hsCodes?: string[];
  casNumber?: string;
  updatedAt?: string;
}

export interface CriticalChainFacility {
  id: string;
  canonicalPath: string;
  name: string;
  facilityKind: string;
  facilityRole: string;
  country: string;
  coordinates?: { latitude: number; longitude: number };
  operators?: string[];
  capacity?: number;
  capacityUnit?: string;
  productionMaterials?: string[];
  materialId: string;
  stage: number;
  riskScore: number;
  sanctioned: boolean;
  updatedAt?: string;
}

export interface CriticalChainRoute {
  id: string;
  type: string;
  waypoints?: Array<{ latitude: number; longitude: number }>;
  fromFacility: string;
  toFacility: string;
  typicalVolume?: number;
  volumeUnit?: string;
  riskScore: number;
  updatedAt?: string;
}

export interface CriticalChainConflictZone {
  id: string;
  name: string;
  boundingBox?: Array<{ latitude: number; longitude: number }>;
  severity: string;
  type: string;
  affectedMaterials?: string[];
  source: string;
  updatedAt?: string;
}

export interface CriticalChainGraph {
  material?: CriticalChainMaterial;
  materials?: CriticalChainMaterial[];
  facilities: CriticalChainFacility[];
  processingSteps: Array<{
    from: string;
    to: string;
    type: string;
    yieldRatio?: number;
    geographicConcentration?: number;
  }>;
}

export async function fetchMaterials(): Promise<CriticalChainMaterial[]> {
  const { data } = await api.get<CriticalChainMaterial[]>('/api/v1/criticalchain/materials');
  return data;
}

export async function fetchMaterial(id: string): Promise<CriticalChainMaterial> {
  const { data } = await api.get<CriticalChainMaterial>(`/api/v1/criticalchain/materials/${id}`);
  return data;
}

export async function fetchFacilities(opts?: {
  material?: string;
  country?: string;
  region?: string;
  riskMin?: number;
}): Promise<CriticalChainFacility[]> {
  const params = new URLSearchParams();
  if (opts?.material) params.set('material', opts.material);
  if (opts?.country) params.set('country', opts.country);
  if (opts?.region) params.set('region', opts.region);
  if (opts?.riskMin != null) params.set('riskMin', String(opts.riskMin));
  const { data } = await api.get<CriticalChainFacility[]>(
    `/api/v1/criticalchain/facilities?${params.toString()}`
  );
  return data;
}

export interface CriticalChainFusedSignal {
  region: string;
  risk_level: number;
  confidence: string;
  signals: string[];
  freshness: string;
  source_count: number;
}

export async function fetchSignals(): Promise<{ regions: CriticalChainFusedSignal[] }> {
  const { data } = await api.get<{ regions: CriticalChainFusedSignal[] }>(
    '/api/v1/criticalchain/signals'
  );
  return data;
}

export async function fetchRoutes(): Promise<CriticalChainRoute[]> {
  const { data } = await api.get<CriticalChainRoute[]>('/api/v1/criticalchain/routes');
  return data;
}

export async function fetchConflictZones(): Promise<CriticalChainConflictZone[]> {
  const { data } = await api.get<CriticalChainConflictZone[]>('/api/v1/criticalchain/conflict-zones');
  return data;
}

export async function fetchGraph(materialId: string): Promise<CriticalChainGraph> {
  const { data } = await api.get<CriticalChainGraph>(`/api/v1/criticalchain/graph/${materialId}`);
  return data;
}

/** Country ISO code -> risk score (1-5) for choropleth */
export async function fetchCountryRisk(): Promise<Record<string, number>> {
  const { data } = await api.get<Record<string, number>>('/api/v1/criticalchain/country-risk');
  return data ?? {};
}

export interface CriticalChainChokepoint {
  id: string;
  path: string;
  name: string;
  latitude: number;
  longitude: number;
  shareOfSupply?: number;
  riskMultiplier?: number;
  materialId?: string;
}

export async function fetchChokepoints(material?: string): Promise<CriticalChainChokepoint[]> {
  const params = material ? `?material=${encodeURIComponent(material)}` : '';
  const { data } = await api.get<CriticalChainChokepoint[]>(`/api/v1/criticalchain/chokepoints${params}`);
  return data ?? [];
}
