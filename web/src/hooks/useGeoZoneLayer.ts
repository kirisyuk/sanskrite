import { useEffect } from 'react';
import maplibregl from 'maplibre-gl';
import type { GeoObject } from './useGeoObjects';
import { extractZoneConditionType, getAffectedZoneMapAccent, getHorizonNodeVisualType, getNodeColors } from '../components/HorizonGraphNodeTypes';

const SRC = 'geo-zones';
const SHARED_SRC = 'geo-zones-shared';
const SHARED_CRITICAL_SRC = 'geo-zones-shared-critical';
const SHARD_SRC = 'geo-zones-shards';
const SHARD_LAYER = 'geo-zones-shards';
const SHARD_ACCENT_LAYER = 'geo-zones-shards-accent';

function canAccessMapStyle(map: maplibregl.Map): boolean {
  try {
    return Boolean((map as unknown as { style?: unknown }).style);
  } catch {
    return false;
  }
}

function circlePolygon(lat: number, lon: number, radiusKm: number, points = 32): GeoJSON.Polygon {
  const kmPerDegLat = 111.32;
  const latRad = (lat * Math.PI) / 180;
  const kmPerDegLon = 111.32 * Math.cos(latRad);
  const dLat = radiusKm / kmPerDegLat;
  const dLon = radiusKm / kmPerDegLon;
  const ring: [number, number][] = [];
  for (let i = 0; i <= points; i++) {
    const angle = (2 * Math.PI * i) / points;
    ring.push([lon + dLon * Math.cos(angle), lat + dLat * Math.sin(angle)]);
  }
  return { type: 'Polygon', coordinates: [ring] };
}

function haversineKm(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const r = 6371;
  const dLat = ((lat2 - lat1) * Math.PI) / 180;
  const dLon = ((lon2 - lon1) * Math.PI) / 180;
  const a =
    Math.sin(dLat / 2) * Math.sin(dLat / 2) +
    Math.cos((lat1 * Math.PI) / 180) *
      Math.cos((lat2 * Math.PI) / 180) *
      Math.sin(dLon / 2) *
      Math.sin(dLon / 2);
  return 2 * r * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
}

function toLocalKm(lat0: number, lon0: number, lat: number, lon: number): { x: number; y: number } {
  const kmPerDegLat = 111.32;
  const kmPerDegLon = 111.32 * Math.max(0.2, Math.cos((lat0 * Math.PI) / 180));
  return {
    x: (lon - lon0) * kmPerDegLon,
    y: (lat - lat0) * kmPerDegLat,
  };
}

function fromLocalKm(lat0: number, lon0: number, x: number, y: number): [number, number] {
  const kmPerDegLat = 111.32;
  const kmPerDegLon = 111.32 * Math.max(0.2, Math.cos((lat0 * Math.PI) / 180));
  return [lon0 + x / kmPerDegLon, lat0 + y / kmPerDegLat];
}

function hashString(input: string): number {
  let hash = 2166136261;
  for (let i = 0; i < input.length; i++) {
    hash ^= input.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}

function unitFromHash(hash: number, salt: number): number {
  const mixed = Math.imul(hash ^ salt, 2246822519) >>> 0;
  return (mixed % 10000) / 10000;
}

function keywordScore(text: string, keywords: string[]): boolean {
  return keywords.some((keyword) => text.includes(keyword));
}

type FieldBias = {
  x: number;
  y: number;
  strength: number;
};

function fieldPolygon(
  obj: GeoObject,
  lat: number,
  lon: number,
  radiusKm: number,
  bias: FieldBias | undefined,
  points = 56,
): GeoJSON.Polygon {
  const text = `${obj.canonicalPath || ''} ${obj.zoneType || ''} ${obj.zoneSubType || ''} ${obj.label || ''}`.toLowerCase();
  const hash = hashString(text || obj.id || `${lat}:${lon}:${radiusKm}`);
  const kmPerDegLat = 111.32;
  const kmPerDegLon = 111.32 * Math.max(0.2, Math.cos((lat * Math.PI) / 180));

  const isConflict = keywordScore(text, ['conflict', 'military', 'terror', 'missile', 'strike', 'war', 'tension']);
  const isDisaster = keywordScore(text, ['earthquake', 'tsunami', 'flood', 'wildfire', 'eruption', 'storm', 'disaster']);
  const isHealth = keywordScore(text, ['health', 'outbreak', 'disease', 'epidemic', 'pandemic']);

  const majorScale = isConflict
    ? 1.22 + unitFromHash(hash, 11) * 0.18
    : isHealth
      ? 1.08 + unitFromHash(hash, 13) * 0.08
      : 1.1 + unitFromHash(hash, 17) * 0.14;
  const minorScale = isConflict
    ? 0.84 + unitFromHash(hash, 19) * 0.12
    : isHealth
      ? 0.96 + unitFromHash(hash, 23) * 0.06
      : 0.9 + unitFromHash(hash, 29) * 0.08;
  const lobeCount = isDisaster ? 3 : 2;
  const wobblePrimary = isDisaster ? 0.06 : isConflict ? 0.045 : 0.032;
  const wobbleSecondary = isHealth ? 0.01 : 0.018;
  const forwardBias = isConflict ? 0.12 : isDisaster ? 0.08 : isHealth ? 0.04 : 0.055;
  const lateralBias = isDisaster ? 0.07 : isHealth ? 0.025 : 0.04;
  const rotation = unitFromHash(hash, 31) * Math.PI * 2;
  const biasStrength = clamp(bias?.strength ?? 0, 0, 1);
  const biasDirX = bias && Number.isFinite(bias.x) ? bias.x : 0;
  const biasDirY = bias && Number.isFinite(bias.y) ? bias.y : 0;

  const dLatBase = radiusKm / kmPerDegLat;
  const dLonBase = radiusKm / kmPerDegLon;
  const centerOffsetKm = radiusKm * clamp(0.08 + biasStrength * 0.12, 0.08, 0.2);
  const offsetLon = (biasDirX * centerOffsetKm) / kmPerDegLon;
  const offsetLat = (biasDirY * centerOffsetKm) / kmPerDegLat;
  const ring: [number, number][] = [];

  for (let i = 0; i <= points; i++) {
    const angle = (2 * Math.PI * i) / points;
    const axisAngle = angle - rotation;
    const dirX = Math.cos(angle);
    const dirY = Math.sin(angle);
    const directionalDot = dirX * biasDirX + dirY * biasDirY;
    const forward = Math.max(0, Math.cos(axisAngle));
    const lateral = Math.sin(axisAngle);
    const contour =
      1 +
      Math.max(0, directionalDot) * biasStrength * 0.18 -
      Math.max(0, -directionalDot) * biasStrength * 0.07 +
      forward * forward * forwardBias +
      lateral * lateral * lateralBias +
      Math.sin(angle * lobeCount + rotation) * wobblePrimary +
      Math.cos(angle * (lobeCount + 1) - rotation * 0.45) * wobbleSecondary;
    const x = Math.cos(axisAngle) * dLonBase * majorScale * contour;
    const y = Math.sin(axisAngle) * dLatBase * minorScale * contour;
    const xr = x * Math.cos(rotation) - y * Math.sin(rotation);
    const yr = x * Math.sin(rotation) + y * Math.cos(rotation);
    ring.push([lon + offsetLon + xr, lat + offsetLat + yr]);
  }

  return { type: 'Polygon', coordinates: [ring] };
}

function zoneColor(obj: GeoObject, theme: 'dark' | 'light'): string {
  if (obj.type === 'AFFECTED_ZONE') return getAffectedZoneMapAccent(obj.canonicalPath, theme);
  const vt = getHorizonNodeVisualType(obj.type, obj.canonicalPath, obj.label, (obj as any)?.facilityKind);
  return getNodeColors(vt || obj.type, theme).accent;
}

function zoneText(obj: GeoObject): string {
  return `${obj.canonicalPath || ''} ${obj.zoneType || ''} ${obj.zoneSubType || ''} ${obj.label || ''}`.toLowerCase();
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v));
}

function zoneHype(obj: GeoObject): number {
  const direct = Number(obj.hype);
  if (Number.isFinite(direct)) return clamp(direct, 0, 100);
  const indicator = Number(obj.hypeIndicator);
  if (Number.isFinite(indicator)) return clamp(indicator * 20, 0, 100);
  return 15;
}

function zoneStatus(obj: GeoObject, hype: number): 'hot' | 'active' | 'watch' | 'cooling' | 'inactive' {
  const fromObj = String(obj.activityStatus || '').trim().toLowerCase();
  if (fromObj === 'hot' || fromObj === 'active' || fromObj === 'watch' || fromObj === 'cooling' || fromObj === 'inactive') {
    return fromObj;
  }
  if (hype >= 72) return 'hot';
  if (hype >= 48) return 'active';
  if (hype >= 26) return 'watch';
  if (hype >= 10) return 'cooling';
  return 'inactive';
}

function zoneStyle(status: 'hot' | 'active' | 'watch' | 'cooling' | 'inactive', hype: number) {
  const factor = clamp(hype / 100, 0, 1);
  switch (status) {
    case 'hot':
      return {
        fillOpacity: 0.085 + factor * 0.07,
        lineOpacity: 0.68 + factor * 0.2,
        lineWidth: 1.5 + factor * 1.45,
        centerGlow: 0.28 + factor * 0.16,
        centerOuterRing: 0.38 + factor * 0.14,
        centerRing: 0.82 + factor * 0.18,
        centerCore: 0.92,
        dash: [1, 0],
      };
    case 'active':
      return {
        fillOpacity: 0.064 + factor * 0.052,
        lineOpacity: 0.56 + factor * 0.16,
        lineWidth: 1.25 + factor * 1.12,
        centerGlow: 0.2 + factor * 0.12,
        centerOuterRing: 0.24 + factor * 0.1,
        centerRing: 0.64 + factor * 0.14,
        centerCore: 0.86,
        dash: [1, 0],
      };
    case 'watch':
      return {
        fillOpacity: 0.045 + factor * 0.034,
        lineOpacity: 0.4 + factor * 0.11,
        lineWidth: 1.02 + factor * 0.55,
        centerGlow: 0.14 + factor * 0.06,
        centerOuterRing: 0.13 + factor * 0.04,
        centerRing: 0.48 + factor * 0.11,
        centerCore: 0.74,
        dash: [2, 1.4],
      };
    case 'cooling':
      return {
        fillOpacity: 0.026 + factor * 0.018,
        lineOpacity: 0.28 + factor * 0.07,
        lineWidth: 0.9 + factor * 0.32,
        centerGlow: 0.065 + factor * 0.03,
        centerOuterRing: 0.06,
        centerRing: 0.3 + factor * 0.06,
        centerCore: 0.58,
        dash: [3, 2.2],
      };
    default:
      return {
        fillOpacity: 0.015,
        lineOpacity: 0.18,
        lineWidth: 0.8,
        centerGlow: 0.03,
        centerOuterRing: 0.04,
        centerRing: 0.2,
        centerCore: 0.44,
        dash: [4, 3],
      };
  }
}

type ZoneFieldMember = {
  id: string;
  lat: number;
  lon: number;
  radiusKm: number;
  hype: number;
  status: 'hot' | 'active' | 'watch' | 'cooling' | 'inactive';
  color: string;
  text: string;
  conditionType: string;
  severityBand: 'critical' | 'ambient';
};

type LocalZoneFieldMember = ZoneFieldMember & {
  x: number;
  y: number;
  weight: number;
  influenceRadius: number;
};

type LocalPoint = {
  x: number;
  y: number;
};

type Bounds = {
  minLon: number;
  minLat: number;
  maxLon: number;
  maxLat: number;
};

type LonLat = [number, number];

type SharedFieldCollections = {
  ambient: GeoJSON.FeatureCollection;
  critical: GeoJSON.FeatureCollection;
};

type ShardSeed = {
  id: string;
  geometry: GeoJSON.Polygon | GeoJSON.MultiPolygon;
  centroidLat: number;
  centroidLon: number;
  color: string;
  hype: number;
  status: 'hot' | 'active' | 'watch' | 'cooling' | 'inactive';
  radiusKm: number;
  detailBoost: number;
  accentBoost: number;
};

function parseHexColor(input: string): [number, number, number] | null {
  const value = String(input || '').trim().replace(/^#/, '');
  const normalized = value.length === 3
    ? value.split('').map((part) => part + part).join('')
    : value;
  if (!/^[0-9a-f]{6}$/i.test(normalized)) return null;
  return [
    parseInt(normalized.slice(0, 2), 16),
    parseInt(normalized.slice(2, 4), 16),
    parseInt(normalized.slice(4, 6), 16),
  ];
}

function toHexColor(rgb: [number, number, number]): string {
  return `#${rgb.map((part) => clamp(Math.round(part), 0, 255).toString(16).padStart(2, '0')).join('')}`;
}

function mixHexColors(entries: Array<{ color: string; weight: number }>, fallback: string): string {
  let r = 0;
  let g = 0;
  let b = 0;
  let total = 0;
  for (const entry of entries) {
    const rgb = parseHexColor(entry.color);
    const weight = Number(entry.weight);
    if (!rgb || !Number.isFinite(weight) || weight <= 0) continue;
    r += rgb[0] * weight;
    g += rgb[1] * weight;
    b += rgb[2] * weight;
    total += weight;
  }
  if (total <= 0) return fallback;
  return toHexColor([r / total, g / total, b / total]);
}

function convexHull(points: LocalPoint[]): LocalPoint[] {
  if (points.length <= 1) return points.slice();
  const sorted = points
    .filter((point) => Number.isFinite(point.x) && Number.isFinite(point.y))
    .sort((a, b) => (a.x === b.x ? a.y - b.y : a.x - b.x));
  if (sorted.length <= 1) return sorted;

  const unique: LocalPoint[] = [];
  for (const point of sorted) {
    const last = unique[unique.length - 1];
    if (!last || last.x !== point.x || last.y !== point.y) unique.push(point);
  }
  if (unique.length <= 2) return unique;

  const cross = (o: LocalPoint, a: LocalPoint, b: LocalPoint): number =>
    (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x);

  const lower: LocalPoint[] = [];
  for (const point of unique) {
    while (lower.length >= 2 && cross(lower[lower.length - 2], lower[lower.length - 1], point) <= 0) lower.pop();
    lower.push(point);
  }

  const upper: LocalPoint[] = [];
  for (let i = unique.length - 1; i >= 0; i--) {
    const point = unique[i];
    while (upper.length >= 2 && cross(upper[upper.length - 2], upper[upper.length - 1], point) <= 0) upper.pop();
    upper.push(point);
  }

  lower.pop();
  upper.pop();
  return lower.concat(upper);
}

function smoothClosedRing(points: LocalPoint[], iterations = 2): LocalPoint[] {
  let ring = points.slice();
  for (let iter = 0; iter < iterations; iter++) {
    if (ring.length < 3) return ring;
    const next: LocalPoint[] = [];
    for (let i = 0; i < ring.length; i++) {
      const current = ring[i];
      const after = ring[(i + 1) % ring.length];
      next.push({
        x: current.x * 0.75 + after.x * 0.25,
        y: current.y * 0.75 + after.y * 0.25,
      });
      next.push({
        x: current.x * 0.25 + after.x * 0.75,
        y: current.y * 0.25 + after.y * 0.75,
      });
    }
    ring = next;
  }
  return ring;
}

function blendHexColors(base: string, overlay: string, ratio: number): string {
  const baseRgb = parseHexColor(base);
  const overlayRgb = parseHexColor(overlay);
  if (!baseRgb) return overlay;
  if (!overlayRgb) return base;
  const t = clamp(ratio, 0, 1);
  return toHexColor([
    baseRgb[0] + (overlayRgb[0] - baseRgb[0]) * t,
    baseRgb[1] + (overlayRgb[1] - baseRgb[1]) * t,
    baseRgb[2] + (overlayRgb[2] - baseRgb[2]) * t,
  ]);
}

function isCriticalZone(obj: GeoObject): boolean {
  const conditionType = extractZoneConditionType(obj.canonicalPath);
  if (conditionType === 'military_conflict' || conditionType === 'security_conflict') return true;
  const text = zoneText(obj);
  return keywordScore(text, [
    'attack',
    'armed_tension',
    'bombard',
    'combat',
    'conflict',
    'cross-border strike',
    'drone strike',
    'hostilities',
    'missile',
    'rocket',
    'shelling',
    'strike',
    'terror',
    'war',
  ]);
}

function collectZoneFieldMembers(objects: GeoObject[], theme: 'dark' | 'light'): ZoneFieldMember[] {
  const members: ZoneFieldMember[] = [];
  for (const obj of objects) {
    const radiusKm = Number(obj.radiusKm);
    const lat = obj.coordinates?.lat;
    const lon = obj.coordinates?.lon;
    if (obj.type !== 'AFFECTED_ZONE' || !Number.isFinite(radiusKm) || radiusKm <= 0) continue;
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) continue;
    const hype = zoneHype(obj);
    members.push({
      id: obj.id,
      lat: lat!,
      lon: lon!,
      radiusKm,
      hype,
      status: zoneStatus(obj, hype),
      color: zoneColor(obj, theme),
      text: zoneText(obj),
      conditionType: extractZoneConditionType(obj.canonicalPath),
      severityBand: isCriticalZone(obj) ? 'critical' : 'ambient',
    });
  }
  return members;
}

function clusterZoneFieldMembers(members: ZoneFieldMember[]): ZoneFieldMember[][] {
  const clusters: ZoneFieldMember[][] = [];
  const used = new Set<string>();
  for (const seed of members) {
    if (used.has(seed.id)) continue;
    const cluster: ZoneFieldMember[] = [];
    const queue = [seed];
    used.add(seed.id);
    while (queue.length) {
      const current = queue.shift()!;
      cluster.push(current);
      for (const candidate of members) {
        if (used.has(candidate.id)) continue;
        const distance = haversineKm(current.lat, current.lon, candidate.lat, candidate.lon);
        const mergeDistance = clamp(
          (current.radiusKm + candidate.radiusKm) * 0.3 + Math.min(current.radiusKm, candidate.radiusKm) * 0.16 + 45,
          40,
          620,
        );
        if (distance <= mergeDistance) {
          used.add(candidate.id);
          queue.push(candidate);
        }
      }
    }
    clusters.push(cluster);
  }
  return clusters;
}

function deriveFieldBias(obj: GeoObject, objects: GeoObject[]): FieldBias | undefined {
  const lat = obj.coordinates?.lat;
  const lon = obj.coordinates?.lon;
  const radiusKm = Number(obj.radiusKm);
  if (!Number.isFinite(lat) || !Number.isFinite(lon) || !Number.isFinite(radiusKm) || radiusKm <= 0) return undefined;

  const reachKm = clamp(radiusKm * 0.7 + 140, 120, 680);
  let sumX = 0;
  let sumY = 0;
  let total = 0;

  for (const candidate of objects) {
    if (candidate.id === obj.id) continue;
    const candidateLat = candidate.coordinates?.lat;
    const candidateLon = candidate.coordinates?.lon;
    if (!Number.isFinite(candidateLat) || !Number.isFinite(candidateLon)) continue;

    const distance = haversineKm(lat!, lon!, candidateLat!, candidateLon!);
    if (!Number.isFinite(distance) || distance <= 0 || distance > reachKm) continue;

    const { x, y } = toLocalKm(lat!, lon!, candidateLat!, candidateLon!);
    const baseWeight = clamp(1 - distance / reachKm, 0, 1);
    if (baseWeight <= 0) continue;
    const typeWeight = candidate.type === 'AFFECTED_ZONE' ? 0.7 : 1.05;
    const weight = baseWeight * typeWeight;
    sumX += x * weight;
    sumY += y * weight;
    total += weight;
  }

  if (total <= 0) return undefined;
  const norm = Math.hypot(sumX, sumY);
  if (!Number.isFinite(norm) || norm < 5) return undefined;

  return {
    x: sumX / norm,
    y: sumY / norm,
    strength: clamp(norm / (reachKm * Math.max(total, 1) * 0.42), 0.12, 1),
  };
}

function buildSharedEnvelope(
  centroidLat: number,
  centroidLon: number,
  members: LocalZoneFieldMember[],
  options?: {
    radiusScale?: number;
    weightScale?: number;
    densityScale?: number;
    minReachScale?: number;
    maxReachScale?: number;
  },
): GeoJSON.Polygon | null {
  if (!members.length) return null;

  const radiusScale = options?.radiusScale ?? 1;
  const weightScale = options?.weightScale ?? 1;
  const densityScale = options?.densityScale ?? 1;
  const minReachScale = options?.minReachScale ?? 1;
  const maxReachScale = options?.maxReachScale ?? 1;
  const adjusted = members.map((member) => ({
    ...member,
    weight: member.weight * weightScale,
    influenceRadius: member.influenceRadius * radiusScale,
  }));

  const clusterSpread = Math.max(...adjusted.map((member) => Math.hypot(member.x, member.y)), 0);
  const maxInfluence = Math.max(...adjusted.map((member) => member.influenceRadius), 0);
  const minReach = clamp((clusterSpread * 0.55 + maxInfluence * 0.24) * minReachScale, 24, 520);
  const maxReach = clamp((clusterSpread + maxInfluence * 1.15) * maxReachScale, 42, 1600);
  const densityBoost = clamp(((adjusted.length - 1) * 0.12 + 0.16) * densityScale, 0.12, 0.72);
  const sampleCount = adjusted.length >= 5 ? 20 : 16;
  const samplePoints: LocalPoint[] = [];

  for (const member of adjusted) {
    const effectiveRadius = clamp(member.influenceRadius * (0.9 + member.weight * 0.08), 12, maxReach);
    for (let i = 0; i < sampleCount; i++) {
      const angle = (2 * Math.PI * i) / sampleCount;
      samplePoints.push({
        x: member.x + Math.cos(angle) * effectiveRadius,
        y: member.y + Math.sin(angle) * effectiveRadius,
      });
    }
  }

  const hull = convexHull(samplePoints);
  if (hull.length < 3) return null;

  const paddingKm = clamp(maxInfluence * (0.06 + densityBoost * 0.08) + clusterSpread * 0.025, 10, 72);
  const expanded = hull.map((point) => {
    const dist = Math.hypot(point.x, point.y);
    if (dist < 1e-6) return { x: paddingKm, y: 0 };
    const extra = paddingKm * clamp(0.82 + dist / Math.max(clusterSpread + maxInfluence, 1) * 0.2, 0.82, 1.08);
    return {
      x: point.x + (point.x / dist) * extra,
      y: point.y + (point.y / dist) * extra,
    };
  });
  const smoothed = smoothClosedRing(expanded, 2);
  const ring: [number, number][] = smoothed.map((point) => fromLocalKm(centroidLat, centroidLon, point.x, point.y));
  if (ring.length > 0) ring.push(ring[0]);

  return { type: 'Polygon', coordinates: [ring] };
}

function geometryBounds(geometry: GeoJSON.Polygon | GeoJSON.MultiPolygon): Bounds {
  let minLon = Infinity;
  let minLat = Infinity;
  let maxLon = -Infinity;
  let maxLat = -Infinity;
  const polygons = geometry.type === 'Polygon' ? [geometry.coordinates] : geometry.coordinates;
  for (const polygon of polygons) {
    for (const ring of polygon) {
      for (const point of ring) {
        minLon = Math.min(minLon, point[0]);
        minLat = Math.min(minLat, point[1]);
        maxLon = Math.max(maxLon, point[0]);
        maxLat = Math.max(maxLat, point[1]);
      }
    }
  }
  return { minLon, minLat, maxLon, maxLat };
}

function pointInRing(point: LonLat, ring: number[][]): boolean {
  let inside = false;
  const [x, y] = point;
  for (let i = 0, j = ring.length - 1; i < ring.length; j = i++) {
    const xi = ring[i][0];
    const yi = ring[i][1];
    const xj = ring[j][0];
    const yj = ring[j][1];
    const intersect = ((yi > y) !== (yj > y))
      && (x < ((xj - xi) * (y - yi)) / ((yj - yi) || Number.EPSILON) + xi);
    if (intersect) inside = !inside;
  }
  return inside;
}

function pointInPolygonRings(point: LonLat, polygon: number[][][]): boolean {
  if (!polygon.length) return false;
  if (!pointInRing(point, polygon[0])) return false;
  for (let i = 1; i < polygon.length; i++) {
    if (pointInRing(point, polygon[i])) return false;
  }
  return true;
}

function geometryContainsPoint(geometry: GeoJSON.Polygon | GeoJSON.MultiPolygon, point: LonLat): boolean {
  if (geometry.type === 'Polygon') return pointInPolygonRings(point, geometry.coordinates);
  return geometry.coordinates.some((polygon) => pointInPolygonRings(point, polygon));
}

function maxGeometryRadiusKm(geometry: GeoJSON.Polygon | GeoJSON.MultiPolygon, lat: number, lon: number): number {
  const polygons = geometry.type === 'Polygon' ? [geometry.coordinates] : geometry.coordinates;
  let maxDistance = 0;
  for (const polygon of polygons) {
    for (const ring of polygon) {
      for (const point of ring) {
        maxDistance = Math.max(maxDistance, haversineKm(lat, lon, point[1], point[0]));
      }
    }
  }
  return Math.max(maxDistance, 1);
}

function buildTrianglePolygon(
  lat0: number,
  lon0: number,
  vertices: [LocalPoint, LocalPoint, LocalPoint],
): GeoJSON.Polygon {
  const ring: [number, number][] = vertices.map((point) => fromLocalKm(lat0, lon0, point.x, point.y));
  ring.push(ring[0]);
  return { type: 'Polygon', coordinates: [ring] };
}

function buildTriangleShardFeatures(seed: ShardSeed, theme: 'dark' | 'light'): GeoJSON.Feature[] {
  const bounds = geometryBounds(seed.geometry);
  const cellKm = clamp(seed.radiusKm * 0.16 * seed.detailBoost, 10, 62);
  const minX = toLocalKm(seed.centroidLat, seed.centroidLon, seed.centroidLat, bounds.minLon).x - cellKm;
  const maxX = toLocalKm(seed.centroidLat, seed.centroidLon, seed.centroidLat, bounds.maxLon).x + cellKm;
  const minY = toLocalKm(seed.centroidLat, seed.centroidLon, bounds.minLat, seed.centroidLon).y - cellKm;
  const maxY = toLocalKm(seed.centroidLat, seed.centroidLon, bounds.maxLat, seed.centroidLon).y + cellKm;
  const maxRadiusKm = Math.max(maxGeometryRadiusKm(seed.geometry, seed.centroidLat, seed.centroidLon), cellKm * 1.5);
  const mutedColor = blendHexColors(seed.color, theme === 'dark' ? '#08111d' : '#f8fafc', theme === 'dark' ? 0.5 : 0.62);
  const baseOpacity = clamp(0.038 + seed.hype / 920, 0.038, 0.17);
  const accentOpacity = clamp(0.028 + seed.hype / 1200, 0.028, 0.13) * seed.accentBoost;
  const features: GeoJSON.Feature[] = [];

  let rowIndex = 0;
  for (let y = minY; y <= maxY; y += cellKm) {
    let colIndex = 0;
    for (let x = minX; x <= maxX; x += cellKm) {
      const tl: LocalPoint = { x, y };
      const tr: LocalPoint = { x: x + cellKm, y };
      const br: LocalPoint = { x: x + cellKm, y: y + cellKm };
      const bl: LocalPoint = { x, y: y + cellKm };
      const flip = ((rowIndex + colIndex) & 1) === 0;
      const triangles: [LocalPoint, LocalPoint, LocalPoint][] = flip
        ? [[tl, tr, bl], [tr, br, bl]]
        : [[tl, tr, br], [tl, br, bl]];

      for (let triangleIndex = 0; triangleIndex < triangles.length; triangleIndex++) {
        const triangle = triangles[triangleIndex]!;
        const center = {
          x: (triangle[0].x + triangle[1].x + triangle[2].x) / 3,
          y: (triangle[0].y + triangle[1].y + triangle[2].y) / 3,
        };
        const centroidLonLat = fromLocalKm(seed.centroidLat, seed.centroidLon, center.x, center.y);
        if (!geometryContainsPoint(seed.geometry, centroidLonLat)) continue;
        const vertexLonLat = triangle.map((point) => fromLocalKm(seed.centroidLat, seed.centroidLon, point.x, point.y)) as LonLat[];
        if (!vertexLonLat.every((point) => geometryContainsPoint(seed.geometry, point))) continue;

        const distance = Math.hypot(center.x, center.y);
        const edgeFactor = 1 - clamp(distance / maxRadiusKm, 0, 1);
        if (edgeFactor <= 0.02) continue;

        const jitter = unitFromHash(hashString(`${seed.id}:${rowIndex}:${colIndex}:${triangleIndex}`), 97);
        const keepThreshold = clamp(0.42 + edgeFactor * 0.72 + seed.hype / 360, 0.46, 1);
        if (jitter > keepThreshold) continue;

        const colorMix = clamp(Math.pow(edgeFactor, 0.9) * 0.9 + jitter * 0.08, 0, 1);
        const triangleColor = blendHexColors(mutedColor, seed.color, colorMix);
        const innerAccent = edgeFactor > 0.34 ? Math.pow((edgeFactor - 0.34) / 0.66, 1.08) : 0;

        features.push({
          type: 'Feature',
          geometry: buildTrianglePolygon(seed.centroidLat, seed.centroidLon, triangle),
          properties: {
            color: triangleColor,
            fillOpacity: baseOpacity * (0.22 + Math.pow(edgeFactor, 1.02) * 1.14) * (0.9 + jitter * 0.18),
            accentColor: seed.color,
            accentOpacity: accentOpacity * innerAccent * (0.82 + jitter * 0.22),
          },
        });
      }
      colIndex += 1;
    }
    rowIndex += 1;
  }

  return features;
}

function buildZoneCollection(objects: GeoObject[], theme: 'dark' | 'light'): GeoJSON.FeatureCollection {
  const clusteredIds = new Set(
    clusterZoneFieldMembers(collectZoneFieldMembers(objects, theme))
      .filter((cluster) => cluster.length > 1)
      .flatMap((cluster) => cluster.map((member) => member.id)),
  );
  const features: GeoJSON.Feature[] = [];
  for (const obj of objects) {
    const radiusKm = Number(obj.radiusKm);
    if (obj.type !== 'AFFECTED_ZONE' || !Number.isFinite(radiusKm) || radiusKm <= 0) continue;
    if (clusteredIds.has(obj.id)) continue;
    const lat = obj.coordinates?.lat;
    const lon = obj.coordinates?.lon;
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) continue;
    const hype = zoneHype(obj);
    const status = zoneStatus(obj, hype);
    const style = zoneStyle(status, hype);
    const radiusAttenuation = clamp(110 / (radiusKm + 40), 0.18, 1);
    const fieldBias = deriveFieldBias(obj, objects);
    features.push({
      type: 'Feature',
      geometry: fieldPolygon(obj, lat!, lon!, Math.min(radiusKm, 2000), fieldBias),
      properties: {
        color: zoneColor(obj, theme),
        fillOpacity: style.fillOpacity * radiusAttenuation * 0.62,
        lineOpacity: style.lineOpacity * clamp(0.7 + radiusAttenuation * 0.5, 0.4, 1),
        lineWidth: style.lineWidth * clamp(0.8 + radiusAttenuation * 0.45, 0.65, 1.1),
        dashA: style.dash[0],
        dashB: style.dash[1],
      },
    });
  }
  return { type: 'FeatureCollection', features };
}

function buildZoneShardCollection(objects: GeoObject[], theme: 'dark' | 'light'): GeoJSON.FeatureCollection {
  const clusteredIds = new Set(
    clusterZoneFieldMembers(collectZoneFieldMembers(objects, theme))
      .filter((cluster) => cluster.length > 1)
      .flatMap((cluster) => cluster.map((member) => member.id)),
  );
  const features: GeoJSON.Feature[] = [];
  for (const obj of objects) {
    const radiusKm = Number(obj.radiusKm);
    if (obj.type !== 'AFFECTED_ZONE' || !Number.isFinite(radiusKm) || radiusKm <= 0) continue;
    if (clusteredIds.has(obj.id)) continue;
    const lat = obj.coordinates?.lat;
    const lon = obj.coordinates?.lon;
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) continue;
    const hype = zoneHype(obj);
    const status = zoneStatus(obj, hype);
    const fieldBias = deriveFieldBias(obj, objects);
    const geometry = fieldPolygon(obj, lat!, lon!, Math.min(radiusKm, 2000), fieldBias);
    features.push(...buildTriangleShardFeatures({
      id: obj.id,
      geometry,
      centroidLat: lat!,
      centroidLon: lon!,
      color: zoneColor(obj, theme),
      hype,
      status,
      radiusKm: Math.min(radiusKm, 2000),
      detailBoost: status === 'hot' || status === 'active' ? 1.08 : 0.96,
      accentBoost: isCriticalZone(obj) ? 1.15 : 0.9,
    }, theme));
  }
  return { type: 'FeatureCollection', features };
}

function buildZoneCenterCollection(objects: GeoObject[], theme: 'dark' | 'light'): GeoJSON.FeatureCollection {
  const clusteredIds = new Set(
    clusterZoneFieldMembers(collectZoneFieldMembers(objects, theme))
      .filter((cluster) => cluster.length > 1)
      .flatMap((cluster) => cluster.map((member) => member.id)),
  );
  const features: GeoJSON.Feature[] = [];
  for (const obj of objects) {
    const radiusKm = Number(obj.radiusKm);
    if (obj.type !== 'AFFECTED_ZONE' || !Number.isFinite(radiusKm) || radiusKm <= 0) continue;
    if (clusteredIds.has(obj.id)) continue;
    const lat = obj.coordinates?.lat;
    const lon = obj.coordinates?.lon;
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) continue;
    const hype = zoneHype(obj);
    const status = zoneStatus(obj, hype);
    const style = zoneStyle(status, hype);
    const radiusFactor = clamp(Math.log10(radiusKm + 10) / 2.2, 0.45, 1.2);
    const fieldWeight = clamp(0.34 + hype / 120 + radiusFactor * 0.18, 0.34, 1);
    const fieldScale = clamp(0.9 + radiusFactor * 0.9 + hype / 180, 0.95, 2.1);
    features.push({
      type: 'Feature',
      geometry: { type: 'Point', coordinates: [lon!, lat!] },
      properties: {
        color: zoneColor(obj, theme),
        fieldWeight,
        fieldScale,
        glowOpacity: style.centerGlow,
        outerRingOpacity: style.centerOuterRing,
        ringOpacity: style.centerRing,
        coreOpacity: style.centerCore,
        hotspotScale: clamp(0.7 + hype / 150, 0.7, 1.4) * radiusFactor,
      },
    });
  }
  return { type: 'FeatureCollection', features };
}

function buildSharedFieldCollections(objects: GeoObject[], theme: 'dark' | 'light'): SharedFieldCollections {
  const members = collectZoneFieldMembers(objects, theme);
  const clusters = clusterZoneFieldMembers(members);

  const ambientFeatures: GeoJSON.Feature[] = [];
  const criticalFeatures: GeoJSON.Feature[] = [];
  for (const cluster of clusters) {
    if (cluster.length < 2) continue;

    const totalWeight = cluster.reduce((sum, member) => sum + clamp(0.7 + member.hype / 90, 0.7, 1.8), 0);
    const centroidLat =
      cluster.reduce((sum, member) => sum + member.lat * clamp(0.7 + member.hype / 90, 0.7, 1.8), 0) / totalWeight;
    const centroidLon =
      cluster.reduce((sum, member) => sum + member.lon * clamp(0.7 + member.hype / 90, 0.7, 1.8), 0) / totalWeight;

    const localMembers: LocalZoneFieldMember[] = cluster.map((member) => {
      const { x, y } = toLocalKm(centroidLat, centroidLon, member.lat, member.lon);
      return {
        ...member,
        x,
        y,
        weight: clamp(0.75 + member.hype / 85, 0.75, 1.95),
        influenceRadius: member.radiusKm * clamp(0.54 + member.hype / 180, 0.54, 1.1),
      };
    });

    const avgHype = cluster.reduce((sum, member) => sum + member.hype, 0) / cluster.length;
    const sharedEnvelope = buildSharedEnvelope(centroidLat, centroidLon, localMembers, {
      densityScale: 1 + avgHype / 220,
    });
    if (!sharedEnvelope) continue;

    const criticalMembers = localMembers.filter((member) => member.severityBand === 'critical');
    const ambientMembers = localMembers.filter((member) => member.severityBand !== 'critical');
    const criticalWeight = criticalMembers.reduce((sum, member) => sum + member.weight, 0);
    const ambientWeight = ambientMembers.reduce((sum, member) => sum + member.weight, 0);
    const criticalShare = criticalWeight / Math.max(criticalWeight + ambientWeight, 0.0001);
    const criticalAvgHype = criticalMembers.length > 0
      ? criticalMembers.reduce((sum, member) => sum + member.hype, 0) / criticalMembers.length
      : 0;
    const fallbackAmbient = theme === 'dark' ? '#f59e0b' : '#d97706';
    const fallbackCritical = theme === 'dark' ? '#ef4444' : '#dc2626';
    const ambientColor = mixHexColors(
      (ambientMembers.length > 0 ? ambientMembers : localMembers).map((member) => ({ color: member.color, weight: member.weight })),
      fallbackAmbient,
    );
    const criticalColor = mixHexColors(
      criticalMembers.map((member) => ({ color: member.color, weight: member.weight * 1.2 })),
      fallbackCritical,
    );
    const sharedStatus =
      avgHype >= 70 ? 'hot' : avgHype >= 46 ? 'active' : avgHype >= 24 ? 'watch' : avgHype >= 10 ? 'cooling' : 'inactive';
    const sharedStyle = zoneStyle(sharedStatus, avgHype);
    const overlapFactor = clamp(cluster.length / 6, 0.35, 1.45);
    const ambientTint = criticalMembers.length > 0
      ? blendHexColors(ambientColor, criticalColor, 0.1 + criticalShare * 0.18)
      : ambientColor;

    ambientFeatures.push({
      type: 'Feature',
      geometry: sharedEnvelope,
      properties: {
        color: ambientTint,
        clusterSize: cluster.length,
        fillOpacity: clamp(
          sharedStyle.fillOpacity * (0.88 + overlapFactor * 0.34) * (criticalMembers.length > 0 ? 0.82 : 1) * 0.58,
          0.08,
          0.24,
        ),
        lineOpacity: clamp(sharedStyle.lineOpacity * (0.55 + overlapFactor * 0.16) + criticalShare * 0.12, 0.18, 0.56),
        lineWidth: clamp(1 + overlapFactor * 0.7 + criticalShare * 0.25, 1, 2.35),
        outlineColor: criticalMembers.length > 0 && criticalShare >= 0.18 ? criticalColor : ambientTint,
      },
    });

    if (criticalMembers.length === 0) continue;

    const criticalEnvelope = buildSharedEnvelope(centroidLat, centroidLon, criticalMembers, {
      radiusScale: 0.76,
      weightScale: 1.18,
      densityScale: 0.72,
      minReachScale: 0.84,
      maxReachScale: 0.9,
    });
    if (!criticalEnvelope) continue;

    criticalFeatures.push({
      type: 'Feature',
      geometry: criticalEnvelope,
      properties: {
        color: criticalColor,
        clusterSize: criticalMembers.length,
        fillOpacity: clamp(
          (sharedStyle.fillOpacity * 0.56 + criticalAvgHype / 480) * (0.42 + criticalShare * 1.1) * 0.56,
          0.08,
          0.3,
        ),
      },
    });
  }

  return {
    ambient: { type: 'FeatureCollection', features: ambientFeatures },
    critical: { type: 'FeatureCollection', features: criticalFeatures },
  };
}

function buildSharedShardCollection(objects: GeoObject[], theme: 'dark' | 'light'): GeoJSON.FeatureCollection {
  const members = collectZoneFieldMembers(objects, theme);
  const clusters = clusterZoneFieldMembers(members);
  const features: GeoJSON.Feature[] = [];

  for (const cluster of clusters) {
    if (cluster.length < 2) continue;

    const totalWeight = cluster.reduce((sum, member) => sum + clamp(0.7 + member.hype / 90, 0.7, 1.8), 0);
    const centroidLat =
      cluster.reduce((sum, member) => sum + member.lat * clamp(0.7 + member.hype / 90, 0.7, 1.8), 0) / totalWeight;
    const centroidLon =
      cluster.reduce((sum, member) => sum + member.lon * clamp(0.7 + member.hype / 90, 0.7, 1.8), 0) / totalWeight;

    const localMembers: LocalZoneFieldMember[] = cluster.map((member) => {
      const { x, y } = toLocalKm(centroidLat, centroidLon, member.lat, member.lon);
      return {
        ...member,
        x,
        y,
        weight: clamp(0.75 + member.hype / 85, 0.75, 1.95),
        influenceRadius: member.radiusKm * clamp(0.54 + member.hype / 180, 0.54, 1.1),
      };
    });

    const avgHype = cluster.reduce((sum, member) => sum + member.hype, 0) / cluster.length;
    const sharedEnvelope = buildSharedEnvelope(centroidLat, centroidLon, localMembers, {
      densityScale: 1 + avgHype / 220,
    });
    if (!sharedEnvelope) continue;

    const criticalMembers = localMembers.filter((member) => member.severityBand === 'critical');
    const ambientMembers = localMembers.filter((member) => member.severityBand !== 'critical');
    const criticalWeight = criticalMembers.reduce((sum, member) => sum + member.weight, 0);
    const ambientWeight = ambientMembers.reduce((sum, member) => sum + member.weight, 0);
    const criticalShare = criticalWeight / Math.max(criticalWeight + ambientWeight, 0.0001);
    const fallbackAmbient = theme === 'dark' ? '#f59e0b' : '#d97706';
    const fallbackCritical = theme === 'dark' ? '#ef4444' : '#dc2626';
    const ambientColor = mixHexColors(
      (ambientMembers.length > 0 ? ambientMembers : localMembers).map((member) => ({ color: member.color, weight: member.weight })),
      fallbackAmbient,
    );
    const criticalColor = mixHexColors(
      criticalMembers.map((member) => ({ color: member.color, weight: member.weight * 1.2 })),
      fallbackCritical,
    );

    features.push(...buildTriangleShardFeatures({
      id: `shared:${cluster.map((member) => member.id).sort().join('|')}`,
      geometry: sharedEnvelope,
      centroidLat,
      centroidLon,
      color: criticalMembers.length > 0
        ? blendHexColors(ambientColor, criticalColor, 0.12 + criticalShare * 0.26)
        : ambientColor,
      hype: avgHype,
      status: avgHype >= 70 ? 'hot' : avgHype >= 46 ? 'active' : avgHype >= 24 ? 'watch' : avgHype >= 10 ? 'cooling' : 'inactive',
      radiusKm: Math.max(...localMembers.map((member) => Math.hypot(member.x, member.y) + member.influenceRadius), 80),
      detailBoost: 1.02 + Math.min(cluster.length, 5) * 0.04,
      accentBoost: 0.86 + criticalShare * 0.5,
    }, theme));
  }

  return { type: 'FeatureCollection', features };
}

export function useGeoZoneLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  objects: GeoObject[],
  theme: 'dark' | 'light',
) {
  // Setup: re-runs on every style reload (styleEpoch change).
  useEffect(() => {
    if (!map || !isReady) return;

    const shards: GeoJSON.FeatureCollection = {
      type: 'FeatureCollection',
      features: [
        ...buildSharedShardCollection(objects, theme).features,
        ...buildZoneShardCollection(objects, theme).features,
      ],
    };
    map.addSource(SHARD_SRC, { type: 'geojson', data: shards });
    map.addLayer({
      id: SHARD_LAYER,
      type: 'fill',
      source: SHARD_SRC,
      paint: {
        'fill-color': ['get', 'color'],
        'fill-opacity': [
          'interpolate', ['linear'], ['zoom'],
          1, 0,
          4, ['*', ['coalesce', ['get', 'fillOpacity'], 0.04], 0.5],
          7, ['*', ['coalesce', ['get', 'fillOpacity'], 0.04], 0.82],
          10, ['*', ['coalesce', ['get', 'fillOpacity'], 0.04], 1.04],
          12, ['*', ['coalesce', ['get', 'fillOpacity'], 0.04], 1.16],
          14, ['*', ['coalesce', ['get', 'fillOpacity'], 0.04], 1.24],
        ],
      },
    });
    map.addLayer({
      id: SHARD_ACCENT_LAYER,
      type: 'fill',
      source: SHARD_SRC,
      paint: {
        'fill-color': ['coalesce', ['get', 'accentColor'], ['get', 'color']],
        'fill-opacity': [
          'interpolate', ['linear'], ['zoom'],
          1, 0,
          5, ['*', ['coalesce', ['get', 'accentOpacity'], 0.02], 0.34],
          8, ['*', ['coalesce', ['get', 'accentOpacity'], 0.02], 0.76],
          10, ['*', ['coalesce', ['get', 'accentOpacity'], 0.02], 1.08],
          12, ['*', ['coalesce', ['get', 'accentOpacity'], 0.02], 1.18],
          14, ['*', ['coalesce', ['get', 'accentOpacity'], 0.02], 1.24],
        ],
      },
    });

    return () => {
      try {
        if (map.getLayer(SHARD_ACCENT_LAYER)) map.removeLayer(SHARD_ACCENT_LAYER);
        if (map.getLayer(SHARD_LAYER)) map.removeLayer(SHARD_LAYER);
        if (map.getSource(SHARD_SRC)) map.removeSource(SHARD_SRC);
      } catch { /* disposed */ }
    };
  }, [styleEpoch]);

  // Update data between style reloads.
  useEffect(() => {
    if (!map || !isReady) return;
    if (!canAccessMapStyle(map)) return;
    const shardSrc = map.getSource(SHARD_SRC) as maplibregl.GeoJSONSource | undefined;
    if (!shardSrc) return;
    shardSrc.setData({
      type: 'FeatureCollection',
      features: [
        ...buildSharedShardCollection(objects, theme).features,
        ...buildZoneShardCollection(objects, theme).features,
      ],
    });
  }, [objects, theme]);
}
