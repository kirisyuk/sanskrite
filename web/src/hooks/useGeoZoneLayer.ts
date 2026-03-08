import { useEffect } from 'react';
import maplibregl from 'maplibre-gl';
import type { GeoObject } from './useGeoObjects';
import { extractZoneConditionType, getAffectedZoneMapAccent, getHorizonNodeVisualType, getNodeColors } from '../components/HorizonGraphNodeTypes';

const SRC = 'geo-zones';
const SHARED_SRC = 'geo-zones-shared';
const FIELD_LAYER = 'geo-zones-field';
const SHARED_CRITICAL_SRC = 'geo-zones-shared-critical';
const FIELD_CRITICAL_LAYER = 'geo-zones-field-critical';
const FIELD_OUTLINE_LAYER = 'geo-zones-field-outline';
const LAYER = 'geo-zones-fill';
const OUTLINE_LAYER = 'geo-zones-outline';
const CENTER_SRC = 'geo-zones-centers';
const CENTER_GLOW_LAYER = 'geo-zones-center-glow';
const CENTER_OUTER_RING_LAYER = 'geo-zones-center-outer-ring';
const CENTER_RING_LAYER = 'geo-zones-center-ring';
const CENTER_CORE_LAYER = 'geo-zones-center-core';

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

function fieldPolygon(obj: GeoObject, lat: number, lon: number, radiusKm: number, points = 56): GeoJSON.Polygon {
  const text = `${obj.canonicalPath || ''} ${obj.zoneType || ''} ${obj.zoneSubType || ''} ${obj.label || ''}`.toLowerCase();
  const hash = hashString(text || obj.id || `${lat}:${lon}:${radiusKm}`);
  const kmPerDegLat = 111.32;
  const kmPerDegLon = 111.32 * Math.max(0.2, Math.cos((lat * Math.PI) / 180));

  const isConflict = keywordScore(text, ['conflict', 'military', 'terror', 'missile', 'strike', 'war', 'tension']);
  const isDisaster = keywordScore(text, ['earthquake', 'tsunami', 'flood', 'wildfire', 'eruption', 'storm', 'disaster']);
  const isHealth = keywordScore(text, ['health', 'outbreak', 'disease', 'epidemic', 'pandemic']);

  const elongation = isConflict
    ? 1.55 + unitFromHash(hash, 11) * 0.35
    : isHealth
      ? 1.18 + unitFromHash(hash, 13) * 0.15
      : 1.22 + unitFromHash(hash, 17) * 0.28;
  const compression = isConflict
    ? 0.76 + unitFromHash(hash, 19) * 0.12
    : isHealth
      ? 0.92 + unitFromHash(hash, 23) * 0.08
      : 0.86 + unitFromHash(hash, 29) * 0.12;
  const lobeCount = isDisaster ? 5 : isConflict ? 4 : 3;
  const wobblePrimary = isDisaster ? 0.24 : isConflict ? 0.18 : 0.12;
  const wobbleSecondary = isHealth ? 0.06 : 0.1;
  const rotation = unitFromHash(hash, 31) * Math.PI * 2;

  const dLatBase = radiusKm / kmPerDegLat;
  const dLonBase = radiusKm / kmPerDegLon;
  const ring: [number, number][] = [];

  for (let i = 0; i <= points; i++) {
    const angle = (2 * Math.PI * i) / points;
    const wobble =
      1 +
      Math.sin(angle * lobeCount + rotation) * wobblePrimary +
      Math.cos(angle * (lobeCount + 2) - rotation * 0.6) * wobbleSecondary;
    const localStretch = Math.cos(angle - rotation) >= 0 ? elongation : compression;
    const rx = dLonBase * wobble * localStretch;
    const ry = dLatBase * wobble * (2 - localStretch * 0.72);
    const x = Math.cos(angle) * rx;
    const y = Math.sin(angle) * ry;
    const xr = x * Math.cos(rotation) - y * Math.sin(rotation);
    const yr = x * Math.sin(rotation) + y * Math.cos(rotation);
    ring.push([lon + xr, lat + yr]);
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

type SharedFieldCollections = {
  ambient: GeoJSON.FeatureCollection;
  critical: GeoJSON.FeatureCollection;
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
        const mergeDistance = clamp((current.radiusKm + candidate.radiusKm) * 0.42 + 120, 90, 900);
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
  const points = 72;
  const rawReach: number[] = [];

  for (let i = 0; i < points; i++) {
    const angle = (2 * Math.PI * i) / points;
    const dirX = Math.cos(angle);
    const dirY = Math.sin(angle);
    const contributions: number[] = [];

    for (const member of adjusted) {
      const proj = member.x * dirX + member.y * dirY;
      const perp = Math.abs(-member.x * dirY + member.y * dirX);
      const width = member.influenceRadius * 1.28;
      const cross = clamp(1 - perp / Math.max(1, width), 0, 1);
      if (cross <= 0) continue;
      const reach = proj + member.influenceRadius * (0.74 + cross * 0.68) * member.weight;
      contributions.push(reach);
    }

    contributions.sort((a, b) => b - a);
    let reach = Math.max(minReach, contributions[0] ?? 0);
    if ((contributions[1] ?? 0) > 0) reach += contributions[1] * 0.24;
    if ((contributions[2] ?? 0) > 0) reach += contributions[2] * 0.11;
    reach += maxInfluence * densityBoost * 0.16;
    rawReach.push(clamp(reach, minReach, maxReach));
  }

  const smoothed = rawReach.map((_, i) => {
    const prev2 = rawReach[(i - 2 + points) % points];
    const prev = rawReach[(i - 1 + points) % points];
    const curr = rawReach[i];
    const next = rawReach[(i + 1) % points];
    const next2 = rawReach[(i + 2) % points];
    return prev2 * 0.08 + prev * 0.18 + curr * 0.48 + next * 0.18 + next2 * 0.08;
  });

  const ring: [number, number][] = [];
  for (let i = 0; i <= points; i++) {
    const angle = (2 * Math.PI * (i % points)) / points;
    const reach = smoothed[i % points];
    ring.push(fromLocalKm(centroidLat, centroidLon, Math.cos(angle) * reach, Math.sin(angle) * reach));
  }

  return { type: 'Polygon', coordinates: [ring] };
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
    features.push({
      type: 'Feature',
      geometry: fieldPolygon(obj, lat!, lon!, Math.min(radiusKm, 2000)),
      properties: {
        color: zoneColor(obj, theme),
        fillOpacity: style.fillOpacity * radiusAttenuation,
        lineOpacity: style.lineOpacity * clamp(0.7 + radiusAttenuation * 0.5, 0.4, 1),
        lineWidth: style.lineWidth * clamp(0.8 + radiusAttenuation * 0.45, 0.65, 1.1),
        dashA: style.dash[0],
        dashB: style.dash[1],
      },
    });
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
          sharedStyle.fillOpacity * (0.88 + overlapFactor * 0.34) * (criticalMembers.length > 0 ? 0.82 : 1),
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
          (sharedStyle.fillOpacity * 0.56 + criticalAvgHype / 480) * (0.42 + criticalShare * 1.1),
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

    const fc = buildZoneCollection(objects, theme);
    const shared = buildSharedFieldCollections(objects, theme);
    const centers = buildZoneCenterCollection(objects, theme);

    map.addSource(SHARED_SRC, { type: 'geojson', data: shared.ambient });
    map.addSource(SHARED_CRITICAL_SRC, { type: 'geojson', data: shared.critical });
    map.addSource(SRC, { type: 'geojson', data: fc });
    map.addSource(CENTER_SRC, { type: 'geojson', data: centers });
    map.addLayer({
      id: FIELD_LAYER,
      type: 'fill',
      source: SHARED_SRC,
      paint: {
        'fill-color': ['get', 'color'],
        'fill-opacity': [
          'interpolate', ['linear'], ['zoom'],
          1, 0,
          4, ['*', ['coalesce', ['get', 'fillOpacity'], 0.1], 0.288],
          7, ['*', ['coalesce', ['get', 'fillOpacity'], 0.1], 0.744],
          10, ['*', ['coalesce', ['get', 'fillOpacity'], 0.1], 1.296],
          12, ['*', ['coalesce', ['get', 'fillOpacity'], 0.1], 1.608],
          14, ['*', ['coalesce', ['get', 'fillOpacity'], 0.1], 1.896],
        ],
      },
    });
    map.addLayer({
      id: FIELD_CRITICAL_LAYER,
      type: 'fill',
      source: SHARED_CRITICAL_SRC,
      paint: {
        'fill-color': ['get', 'color'],
        'fill-opacity': [
          'interpolate', ['linear'], ['zoom'],
          1, 0,
          4, ['*', ['coalesce', ['get', 'fillOpacity'], 0.1], 0.24],
          7, ['*', ['coalesce', ['get', 'fillOpacity'], 0.1], 0.62],
          10, ['*', ['coalesce', ['get', 'fillOpacity'], 0.1], 1.06],
          12, ['*', ['coalesce', ['get', 'fillOpacity'], 0.1], 1.24],
          14, ['*', ['coalesce', ['get', 'fillOpacity'], 0.1], 1.38],
        ],
      },
    });
    map.addLayer({
      id: FIELD_OUTLINE_LAYER,
      type: 'line',
      source: SHARED_SRC,
      paint: {
        'line-color': ['coalesce', ['get', 'outlineColor'], ['get', 'color']],
        'line-width': [
          'interpolate', ['linear'], ['zoom'],
          3, ['*', ['coalesce', ['get', 'lineWidth'], 1], 0.45],
          7, ['*', ['coalesce', ['get', 'lineWidth'], 1], 0.72],
          10, ['coalesce', ['get', 'lineWidth'], 1],
          12, ['*', ['coalesce', ['get', 'lineWidth'], 1], 1.14],
          14, ['*', ['coalesce', ['get', 'lineWidth'], 1], 1.26],
        ],
        'line-opacity': [
          'interpolate', ['linear'], ['zoom'],
          3, ['*', ['coalesce', ['get', 'lineOpacity'], 0.2], 0.336],
          7, ['*', ['coalesce', ['get', 'lineOpacity'], 0.2], 0.672],
          10, ['*', ['coalesce', ['get', 'lineOpacity'], 0.2], 1.2],
          12, ['*', ['coalesce', ['get', 'lineOpacity'], 0.2], 1.344],
          14, ['*', ['coalesce', ['get', 'lineOpacity'], 0.2], 1.44],
        ],
      },
    });
    map.addLayer({
      id: LAYER,
      type: 'fill',
      source: SRC,
      paint: {
        'fill-color': ['get', 'color'],
        'fill-opacity': [
          'interpolate', ['linear'], ['zoom'],
          1, 0,
          3, ['*', ['coalesce', ['get', 'fillOpacity'], 0.08], 0.12],
          5, ['*', ['coalesce', ['get', 'fillOpacity'], 0.08], 0.336],
          7, ['*', ['coalesce', ['get', 'fillOpacity'], 0.08], 0.54],
          10, ['*', ['coalesce', ['get', 'fillOpacity'], 0.08], 0.936],
          12, ['*', ['coalesce', ['get', 'fillOpacity'], 0.08], 1.14],
          14, ['*', ['coalesce', ['get', 'fillOpacity'], 0.08], 1.32],
        ],
      },
    });
    map.addLayer({
      id: OUTLINE_LAYER,
      type: 'line',
      source: SRC,
      paint: {
        'line-color': ['get', 'color'],
        'line-width': [
          'interpolate', ['linear'], ['zoom'],
          2, ['*', ['coalesce', ['get', 'lineWidth'], 1], 0.6],
          5, ['*', ['coalesce', ['get', 'lineWidth'], 1], 0.9],
          8, ['coalesce', ['get', 'lineWidth'], 1],
          10, ['*', ['coalesce', ['get', 'lineWidth'], 1], 1.2],
          12, ['*', ['coalesce', ['get', 'lineWidth'], 1], 1.55],
          14, ['*', ['coalesce', ['get', 'lineWidth'], 1], 1.8],
        ],
        'line-opacity': [
          'interpolate', ['linear'], ['zoom'],
          1, 0.04,
          3, ['*', ['coalesce', ['get', 'lineOpacity'], 0.3], 0.192],
          5, ['*', ['coalesce', ['get', 'lineOpacity'], 0.3], 0.408],
          7, ['*', ['coalesce', ['get', 'lineOpacity'], 0.3], 0.624],
          10, ['*', ['coalesce', ['get', 'lineOpacity'], 0.3], 0.912],
          12, ['*', ['coalesce', ['get', 'lineOpacity'], 0.3], 1.08],
          14, ['*', ['coalesce', ['get', 'lineOpacity'], 0.3], 1.224],
        ],
        'line-dasharray': [
          'match',
          ['get', 'dashA'],
          2, ['literal', [2, 1.4]],
          3, ['literal', [3, 2.2]],
          4, ['literal', [4, 3]],
          ['literal', [1, 0]],
        ],
      },
    });
    map.addLayer({
      id: CENTER_GLOW_LAYER,
      type: 'circle',
      source: CENTER_SRC,
      paint: {
        'circle-color': ['get', 'color'],
        'circle-radius': [
          'interpolate', ['linear'], ['zoom'],
          2, ['*', ['get', 'hotspotScale'], 7],
          6, ['*', ['get', 'hotspotScale'], 12],
          10, ['*', ['get', 'hotspotScale'], 22],
          12, ['*', ['get', 'hotspotScale'], 25],
          14, ['*', ['get', 'hotspotScale'], 29],
        ],
        'circle-opacity': [
          'interpolate', ['linear'], ['zoom'],
          1, 0,
          5, ['*', ['coalesce', ['get', 'glowOpacity'], 0.08], 0.264],
          9, ['*', ['coalesce', ['get', 'glowOpacity'], 0.08], 0.336],
          12, ['*', ['coalesce', ['get', 'glowOpacity'], 0.08], 0.408],
          14, ['*', ['coalesce', ['get', 'glowOpacity'], 0.08], 0.504],
        ],
        'circle-blur': 0.8,
      },
    });
    map.addLayer({
      id: CENTER_OUTER_RING_LAYER,
      type: 'circle',
      source: CENTER_SRC,
      paint: {
        'circle-color': 'rgba(0,0,0,0)',
        'circle-radius': [
          'interpolate', ['linear'], ['zoom'],
          2, ['*', ['get', 'hotspotScale'], 7.5],
          6, ['*', ['get', 'hotspotScale'], 11],
          10, ['*', ['get', 'hotspotScale'], 19],
          12, ['*', ['get', 'hotspotScale'], 21],
          14, ['*', ['get', 'hotspotScale'], 23.5],
        ],
        'circle-stroke-color': ['get', 'color'],
        'circle-stroke-width': [
          'interpolate', ['linear'], ['zoom'],
          2, 0.8,
          8, 1.2,
          10, 2,
          12, 2.35,
          14, 2.7,
        ],
        'circle-stroke-opacity': [
          'interpolate', ['linear'], ['zoom'],
          2, 0,
          10, ['*', ['coalesce', ['get', 'outerRingOpacity'], 0.08], 0.264],
          12, ['*', ['coalesce', ['get', 'outerRingOpacity'], 0.08], 0.336],
          14, ['*', ['coalesce', ['get', 'outerRingOpacity'], 0.08], 0.408],
        ],
      },
    });
    map.addLayer({
      id: CENTER_RING_LAYER,
      type: 'circle',
      source: CENTER_SRC,
      paint: {
        'circle-color': 'rgba(0,0,0,0)',
        'circle-radius': [
          'interpolate', ['linear'], ['zoom'],
          2, ['*', ['get', 'hotspotScale'], 4.2],
          6, ['*', ['get', 'hotspotScale'], 7.2],
          10, ['*', ['get', 'hotspotScale'], 12.6],
          12, ['*', ['get', 'hotspotScale'], 14.1],
          14, ['*', ['get', 'hotspotScale'], 15.8],
        ],
        'circle-stroke-color': ['get', 'color'],
        'circle-stroke-width': [
          'interpolate', ['linear'], ['zoom'],
          2, 0.9,
          8, 1.7,
          10, 2.8,
          12, 3.2,
          14, 3.6,
        ],
        'circle-stroke-opacity': [
          'interpolate', ['linear'], ['zoom'],
          2, 0,
          10, ['*', ['coalesce', ['get', 'ringOpacity'], 0.4], 0.408],
          12, ['*', ['coalesce', ['get', 'ringOpacity'], 0.4], 0.504],
          14, ['*', ['coalesce', ['get', 'ringOpacity'], 0.4], 0.6],
        ],
      },
    });
    map.addLayer({
      id: CENTER_CORE_LAYER,
      type: 'circle',
      source: CENTER_SRC,
      paint: {
        'circle-color': ['get', 'color'],
        'circle-radius': [
          'interpolate', ['linear'], ['zoom'],
          2, ['*', ['get', 'hotspotScale'], 1.7],
          6, ['*', ['get', 'hotspotScale'], 2.8],
          10, ['*', ['get', 'hotspotScale'], 5.3],
          12, ['*', ['get', 'hotspotScale'], 6],
          14, ['*', ['get', 'hotspotScale'], 6.8],
        ],
        'circle-opacity': [
          'interpolate', ['linear'], ['zoom'],
          2, 0,
          10, ['*', ['coalesce', ['get', 'coreOpacity'], 0.6], 0.408],
          12, ['*', ['coalesce', ['get', 'coreOpacity'], 0.6], 0.504],
          14, ['*', ['coalesce', ['get', 'coreOpacity'], 0.6], 0.6],
        ],
      },
    });

    return () => {
      try {
        if (map.getLayer(CENTER_CORE_LAYER)) map.removeLayer(CENTER_CORE_LAYER);
        if (map.getLayer(CENTER_RING_LAYER)) map.removeLayer(CENTER_RING_LAYER);
        if (map.getLayer(CENTER_OUTER_RING_LAYER)) map.removeLayer(CENTER_OUTER_RING_LAYER);
        if (map.getLayer(CENTER_GLOW_LAYER)) map.removeLayer(CENTER_GLOW_LAYER);
        if (map.getLayer(OUTLINE_LAYER)) map.removeLayer(OUTLINE_LAYER);
        if (map.getLayer(LAYER)) map.removeLayer(LAYER);
        if (map.getLayer(FIELD_OUTLINE_LAYER)) map.removeLayer(FIELD_OUTLINE_LAYER);
        if (map.getLayer(FIELD_CRITICAL_LAYER)) map.removeLayer(FIELD_CRITICAL_LAYER);
        if (map.getLayer(FIELD_LAYER)) map.removeLayer(FIELD_LAYER);
        if (map.getSource(CENTER_SRC)) map.removeSource(CENTER_SRC);
        if (map.getSource(SRC)) map.removeSource(SRC);
        if (map.getSource(SHARED_CRITICAL_SRC)) map.removeSource(SHARED_CRITICAL_SRC);
        if (map.getSource(SHARED_SRC)) map.removeSource(SHARED_SRC);
      } catch { /* disposed */ }
    };
  }, [styleEpoch]);

  // Update data between style reloads.
  useEffect(() => {
    if (!map || !isReady) return;
    if (!canAccessMapStyle(map)) return;
    const sharedSrc = map.getSource(SHARED_SRC) as maplibregl.GeoJSONSource | undefined;
    const sharedCriticalSrc = map.getSource(SHARED_CRITICAL_SRC) as maplibregl.GeoJSONSource | undefined;
    const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
    const centerSrc = map.getSource(CENTER_SRC) as maplibregl.GeoJSONSource | undefined;
    if (!sharedSrc || !sharedCriticalSrc || !src || !centerSrc) return;
    const shared = buildSharedFieldCollections(objects, theme);
    sharedSrc.setData(shared.ambient);
    sharedCriticalSrc.setData(shared.critical);
    src.setData(buildZoneCollection(objects, theme));
    centerSrc.setData(buildZoneCenterCollection(objects, theme));
  }, [objects, theme]);
}
