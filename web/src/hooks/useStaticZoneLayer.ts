import { useEffect, useRef } from 'react';
import maplibregl from 'maplibre-gl';
import { formatZoneType, zoneTypeColor, type StaticZoneFeature, type StaticZoneType } from '../lib/staticZones';
import type { GeoObject } from './useGeoObjects';

const SRC = 'static-zones-src';
const HIVE_SRC = 'static-zones-hive-src';
const HIVE_FILL_LAYER = 'static-zones-hive-fill';
const HIVE_OUTLINE_LAYER = 'static-zones-hive-outline';

interface ZoneRenderProperties {
  id: string;
  name: string;
  countryName: string;
  zoneType: StaticZoneType;
  zoneSubType: string;
  tagsText: string;
  euActsText: string;
  color: string;
  hype: number;
  status: string;
  objectCount: number;
  fillOpacity: number;
  lineOpacity: number;
  lineWidth: number;
  cellBaseOpacity: number;
  cellBoostOpacity: number;
  cellLineOpacity: number;
  minZoom?: number;
  maxZoom?: number;
}

interface ZoneActivitySnapshot {
  hype?: number;
  status?: 'hot' | 'active' | 'watch' | 'cooling' | 'inactive';
}

interface ZoneCollections {
  zones: GeoJSON.FeatureCollection;
  hive: GeoJSON.FeatureCollection;
}

interface Bounds {
  minLon: number;
  minLat: number;
  maxLon: number;
  maxLat: number;
}

type LonLat = [number, number];

const POPUP_STYLE_ID = 'static-zones-popup-style';
const KM_PER_DEG_LAT = 111.32;

function canAccessMapStyle(map: maplibregl.Map): boolean {
  try {
    return Boolean((map as unknown as { style?: unknown }).style);
  } catch {
    return false;
  }
}

function ensurePopupStyles(): void {
  if (typeof document === 'undefined') return;
  if (document.getElementById(POPUP_STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = POPUP_STYLE_ID;
  style.textContent = `
.static-zones-popup-dark .maplibregl-popup-content,
.static-zones-popup-light .maplibregl-popup-content {
  padding: 0;
  overflow: hidden;
}
.static-zones-popup-dark .maplibregl-popup-content {
  background: rgba(8, 14, 24, 0.96);
  color: #e2e8f0;
  border: 1px solid rgba(148, 163, 184, 0.35);
  border-radius: 14px;
  box-shadow: 0 18px 44px rgba(2, 6, 23, 0.56);
}
.static-zones-popup-light .maplibregl-popup-content {
  background: rgba(255, 255, 255, 0.97);
  color: #0f172a;
  border: 1px solid rgba(30, 41, 59, 0.2);
  border-radius: 14px;
  box-shadow: 0 16px 34px rgba(15, 23, 42, 0.18);
}
.static-zones-popup-dark .maplibregl-popup-close-button,
.static-zones-popup-light .maplibregl-popup-close-button {
  top: 10px;
  right: 10px;
  width: 24px;
  height: 24px;
  border-radius: 999px;
  display: grid;
  place-items: center;
  font-size: 15px;
  line-height: 1;
  padding: 0;
  transition: background-color 120ms ease, color 120ms ease, transform 120ms ease;
}
.static-zones-popup-dark .maplibregl-popup-close-button {
  color: rgba(226, 232, 240, 0.88);
  background: rgba(148, 163, 184, 0.12);
}
.static-zones-popup-light .maplibregl-popup-close-button {
  color: rgba(15, 23, 42, 0.74);
  background: rgba(15, 23, 42, 0.06);
}
.static-zones-popup-dark .maplibregl-popup-close-button:hover {
  color: #f8fafc;
  background: rgba(148, 163, 184, 0.22);
  transform: translateY(-1px);
}
.static-zones-popup-light .maplibregl-popup-close-button:hover {
  color: #020617;
  background: rgba(15, 23, 42, 0.1);
  transform: translateY(-1px);
}
.static-zones-popup-card {
  position: relative;
  min-width: 248px;
  max-width: 320px;
  padding: 14px 44px 14px 14px;
}
.static-zones-popup-card::before {
  content: '';
  position: absolute;
  inset: 0 auto 0 0;
  width: 3px;
  background: var(--popup-accent, rgba(148, 163, 184, 0.8));
}
.static-zones-popup-kicker {
  margin-bottom: 6px;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.11em;
  text-transform: uppercase;
  color: var(--popup-accent, inherit);
}
.static-zones-popup-title {
  margin-bottom: 3px;
  font-size: 18px;
  line-height: 1.15;
  font-weight: 700;
  letter-spacing: -0.01em;
}
.static-zones-popup-subtitle {
  margin-bottom: 10px;
  font-size: 12px;
  line-height: 1.3;
  opacity: 0.72;
}
.static-zones-popup-body {
  margin-bottom: 12px;
  font-size: 12px;
  line-height: 1.5;
}
.static-zones-popup-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.static-zones-popup-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 8px;
  border-radius: 999px;
  font-size: 10px;
  line-height: 1;
  font-weight: 600;
  letter-spacing: 0.01em;
}
.static-zones-popup-chip strong {
  font-size: 10px;
  font-weight: 700;
}
.static-zones-popup-dark .static-zones-popup-chip {
  background: rgba(148, 163, 184, 0.12);
  border: 1px solid rgba(148, 163, 184, 0.16);
  color: rgba(226, 232, 240, 0.94);
}
.static-zones-popup-light .static-zones-popup-chip {
  background: rgba(15, 23, 42, 0.05);
  border: 1px solid rgba(15, 23, 42, 0.08);
  color: rgba(15, 23, 42, 0.82);
}
.static-zones-popup-dark.maplibregl-popup-anchor-top .maplibregl-popup-tip,
.static-zones-popup-dark.maplibregl-popup-anchor-top-left .maplibregl-popup-tip,
.static-zones-popup-dark.maplibregl-popup-anchor-top-right .maplibregl-popup-tip {
  border-bottom-color: rgba(8, 14, 24, 0.96);
}
.static-zones-popup-dark.maplibregl-popup-anchor-bottom .maplibregl-popup-tip,
.static-zones-popup-dark.maplibregl-popup-anchor-bottom-left .maplibregl-popup-tip,
.static-zones-popup-dark.maplibregl-popup-anchor-bottom-right .maplibregl-popup-tip {
  border-top-color: rgba(8, 14, 24, 0.96);
}
.static-zones-popup-dark.maplibregl-popup-anchor-left .maplibregl-popup-tip {
  border-right-color: rgba(8, 14, 24, 0.96);
}
.static-zones-popup-dark.maplibregl-popup-anchor-right .maplibregl-popup-tip {
  border-left-color: rgba(8, 14, 24, 0.96);
}
.static-zones-popup-light.maplibregl-popup-anchor-top .maplibregl-popup-tip,
.static-zones-popup-light.maplibregl-popup-anchor-top-left .maplibregl-popup-tip,
.static-zones-popup-light.maplibregl-popup-anchor-top-right .maplibregl-popup-tip {
  border-bottom-color: rgba(255, 255, 255, 0.97);
}
.static-zones-popup-light.maplibregl-popup-anchor-bottom .maplibregl-popup-tip,
.static-zones-popup-light.maplibregl-popup-anchor-bottom-left .maplibregl-popup-tip,
.static-zones-popup-light.maplibregl-popup-anchor-bottom-right .maplibregl-popup-tip {
  border-top-color: rgba(255, 255, 255, 0.97);
}
.static-zones-popup-light.maplibregl-popup-anchor-left .maplibregl-popup-tip {
  border-right-color: rgba(255, 255, 255, 0.97);
}
.static-zones-popup-light.maplibregl-popup-anchor-right .maplibregl-popup-tip {
  border-left-color: rgba(255, 255, 255, 0.97);
}
`;
  document.head.appendChild(style);
}

function escapeHtml(input: string): string {
  return input
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function toEnglishZoneNarrative(props: ZoneRenderProperties): string {
  const byType: Record<StaticZoneType, string> = {
    industrial_cluster: 'Manufacturing capacity and supplier density are concentrated in this area.',
    energy_hub: 'Energy processing, storage, and export infrastructure are concentrated in this area.',
    logistics_chokepoint: 'Freight dependency is concentrated around port, rail, and road transfer points.',
    special_economic_zone: 'Export-oriented operations are concentrated under preferential regulatory conditions.',
    resource_extraction: 'Primary resource extraction and first-stage processing are concentrated here.',
    military_industrial: 'Defense-adjacent industrial assets and strategic production are concentrated here.',
    data_infrastructure: 'High-tech operations and critical digital infrastructure are concentrated here.',
    climate_exposure: 'Production and logistics here are sensitive to recurring climate stress.',
  };

  const zoneType = props.zoneType;
  const base = byType[zoneType] || byType.industrial_cluster;
  const subtype = props.zoneSubType ? ` Focus: ${props.zoneSubType.replace(/_/g, ' ')}.` : '';
  const tags = props.tagsText ? ` Signals: ${props.tagsText}.` : '';
  const policy = props.euActsText ? ` Policy context: ${props.euActsText}.` : '';
  return `${base}${subtype}${tags}${policy}`;
}

function normalizeZoneType(input: string): StaticZoneType {
  const key = (input || '').trim() as StaticZoneType;
  if (
    key === 'industrial_cluster' ||
    key === 'energy_hub' ||
    key === 'logistics_chokepoint' ||
    key === 'special_economic_zone' ||
    key === 'resource_extraction' ||
    key === 'military_industrial' ||
    key === 'data_infrastructure' ||
    key === 'climate_exposure'
  ) {
    return key;
  }
  return 'industrial_cluster';
}

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

function statusTint(status: string, fallback: string): string {
  switch (status) {
    case 'hot': return blendHexColors(fallback, '#f8fafc', 0.14);
    case 'active': return blendHexColors(fallback, '#e2e8f0', 0.09);
    case 'watch': return blendHexColors(fallback, '#cbd5e1', 0.04);
    case 'cooling': return blendHexColors(fallback, '#94a3b8', 0.08);
    default: return fallback;
  }
}

function kmPerDegLonAtLat(lat: number): number {
  return KM_PER_DEG_LAT * Math.max(0.2, Math.cos((lat * Math.PI) / 180));
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

function projectedDistanceSqKm(a: LonLat, b: LonLat, refLat: number): number {
  const dx = (a[0] - b[0]) * kmPerDegLonAtLat(refLat);
  const dy = (a[1] - b[1]) * KM_PER_DEG_LAT;
  return dx * dx + dy * dy;
}

function selectHexSizeKm(bounds: Bounds, objectCount: number): number {
  const centerLat = (bounds.minLat + bounds.maxLat) / 2;
  const widthKm = Math.max(8, (bounds.maxLon - bounds.minLon) * kmPerDegLonAtLat(centerLat));
  const heightKm = Math.max(8, (bounds.maxLat - bounds.minLat) * KM_PER_DEG_LAT);
  const minSideKm = Math.max(8, Math.min(widthKm, heightKm));
  let sizeKm = clamp(minSideKm / 5.8, 5.5, 22);
  if (objectCount > 0) {
    sizeKm *= clamp(1 - Math.log2(objectCount + 1) * 0.048, 0.58, 1);
  }
  let estimated = ((widthKm / (Math.sqrt(3) * sizeKm)) + 1.5) * ((heightKm / (1.5 * sizeKm)) + 1.5);
  while (estimated > 120 && sizeKm < 28) {
    sizeKm *= 1.12;
    estimated = ((widthKm / (Math.sqrt(3) * sizeKm)) + 1.5) * ((heightKm / (1.5 * sizeKm)) + 1.5);
  }
  while (estimated < 20 && sizeKm > 5.5) {
    sizeKm *= 0.9;
    estimated = ((widthKm / (Math.sqrt(3) * sizeKm)) + 1.5) * ((heightKm / (1.5 * sizeKm)) + 1.5);
  }
  return clamp(sizeKm, 5.5, 28);
}

function buildHexagon(center: LonLat, sizeKm: number, gapRatio = 0.94): GeoJSON.Polygon {
  const [lon, lat] = center;
  const effectiveKm = sizeKm * gapRatio;
  const radiusLon = effectiveKm / kmPerDegLonAtLat(lat);
  const radiusLat = effectiveKm / KM_PER_DEG_LAT;
  const ring: LonLat[] = [];
  for (let i = 0; i < 6; i++) {
    const angle = ((60 * i) - 30) * (Math.PI / 180);
    ring.push([lon + radiusLon * Math.cos(angle), lat + radiusLat * Math.sin(angle)]);
  }
  ring.push(ring[0]);
  return { type: 'Polygon', coordinates: [ring] };
}

function collectObjectsInGeometry(
  objects: GeoObject[],
  geometry: GeoJSON.Polygon | GeoJSON.MultiPolygon,
  bounds: Bounds,
): GeoObject[] {
  return objects.filter((obj) => {
    if (obj.type === 'AFFECTED_ZONE') return false;
    const lat = obj.coordinates?.lat;
    const lon = obj.coordinates?.lon;
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) return false;
    if (lon! < bounds.minLon || lon! > bounds.maxLon || lat! < bounds.minLat || lat! > bounds.maxLat) return false;
    return geometryContainsPoint(geometry, [lon!, lat!]);
  });
}

function zoneStyle(status: string, hype: number, objectCount: number) {
  const factor = clamp(hype / 100, 0, 1);
  const density = clamp(objectCount / 10, 0, 1);
  switch (status) {
    case 'hot':
      return {
        fillOpacity: 0.052 + factor * 0.039 + density * 0.021,
        lineOpacity: 0.7 + factor * 0.14,
        lineWidth: 1.55 + factor * 0.8,
        cellBaseOpacity: 0.046 + density * 0.016,
        cellBoostOpacity: 0.186 + factor * 0.104,
        cellLineOpacity: 0.31 + factor * 0.15,
      };
    case 'active':
      return {
        fillOpacity: 0.039 + factor * 0.029 + density * 0.016,
        lineOpacity: 0.6 + factor * 0.12,
        lineWidth: 1.3 + factor * 0.55,
        cellBaseOpacity: 0.04 + density * 0.013,
        cellBoostOpacity: 0.138 + factor * 0.084,
        cellLineOpacity: 0.272 + factor * 0.13,
      };
    case 'watch':
      return {
        fillOpacity: 0.027 + factor * 0.021 + density * 0.013,
        lineOpacity: 0.48 + factor * 0.095,
        lineWidth: 1.05 + factor * 0.4,
        cellBaseOpacity: 0.031 + density * 0.01,
        cellBoostOpacity: 0.102 + factor * 0.058,
        cellLineOpacity: 0.22 + factor * 0.102,
      };
    case 'cooling':
      return {
        fillOpacity: 0.016 + factor * 0.013 + density * 0.008,
        lineOpacity: 0.33 + factor * 0.072,
        lineWidth: 0.9 + factor * 0.25,
        cellBaseOpacity: 0.021 + density * 0.007,
        cellBoostOpacity: 0.06 + factor * 0.033,
        cellLineOpacity: 0.148 + factor * 0.058,
      };
    default:
      return {
        fillOpacity: 0.008 + density * 0.005,
        lineOpacity: 0.19 + density * 0.09,
        lineWidth: 0.8 + density * 0.18,
        cellBaseOpacity: 0.016 + density * 0.004,
        cellBoostOpacity: 0.027 + density * 0.02,
        cellLineOpacity: 0.11 + density * 0.04,
      };
  }
}

function buildHexCellFeatures(
  feature: StaticZoneFeature,
  props: ZoneRenderProperties,
  objectsInZone: GeoObject[],
  theme: 'dark' | 'light',
): GeoJSON.Feature[] {
  const emptyCellOpacityMultiplier = 0.34;
  const bounds = geometryBounds(feature.geometry);
  const sizeKm = selectHexSizeKm(bounds, objectsInZone.length);
  const centerLat = (bounds.minLat + bounds.maxLat) / 2;
  const rowStepLat = (1.5 * sizeKm) / KM_PER_DEG_LAT;
  const colStepLon = (Math.sqrt(3) * sizeKm) / kmPerDegLonAtLat(centerLat);
  const centers: LonLat[] = [];

  let row = 0;
  for (let lat = bounds.minLat - rowStepLat; lat <= bounds.maxLat + rowStepLat; lat += rowStepLat) {
    const offsetLon = row % 2 === 0 ? 0 : colStepLon / 2;
    for (let lon = bounds.minLon - colStepLon; lon <= bounds.maxLon + colStepLon; lon += colStepLon) {
      const center: LonLat = [lon + offsetLon, lat];
      if (!geometryContainsPoint(feature.geometry, center)) continue;
      centers.push(center);
    }
    row += 1;
  }

  if (!centers.length) return [];

  const counts = new Array<number>(centers.length).fill(0);
  for (const obj of objectsInZone) {
    const point: LonLat = [obj.coordinates!.lon, obj.coordinates!.lat];
    let bestIdx = 0;
    let bestDistance = Number.POSITIVE_INFINITY;
    for (let i = 0; i < centers.length; i++) {
      const distance = projectedDistanceSqKm(point, centers[i], centerLat);
      if (distance < bestDistance) {
        bestDistance = distance;
        bestIdx = i;
      }
    }
    counts[bestIdx] += 1;
  }

  const maxCount = counts.reduce((acc, value) => Math.max(acc, value), 0);
  const renderEmptyCells = props.status === 'hot' || props.status === 'active' || props.hype >= 35;

  return centers.flatMap((center, idx) => {
    const count = counts[idx];
    if (!count && !renderEmptyCells) return [];
    const normalized = maxCount > 0 ? Math.log(count + 1) / Math.log(maxCount + 1) : 0;
    const isOccupied = count > 0;
    const occupiedSignal = isOccupied ? clamp(0.38 + normalized * 0.82, 0.38, 1.2) : 0;
    const fillOpacity =
      props.cellBaseOpacity * (isOccupied ? 1.62 : emptyCellOpacityMultiplier) +
      props.cellBoostOpacity * occupiedSignal;
    const lineOpacity = isOccupied
      ? props.cellLineOpacity * (1.18 + normalized * 0.42)
      : props.cellLineOpacity * 0.24;
    const fillColor = isOccupied
      ? blendHexColors(props.color, theme === 'dark' ? '#e2e8f0' : '#ffffff', theme === 'dark' ? 0.14 : 0.1)
      : props.color;
    const outlineColor = isOccupied
      ? blendHexColors(props.color, theme === 'dark' ? '#f8fafc' : '#0f172a', theme === 'dark' ? 0.28 : 0.12)
      : blendHexColors(props.color, theme === 'dark' ? '#94a3b8' : '#ffffff', theme === 'dark' ? 0.08 : 0.06);
    if (fillOpacity < 0.01 && lineOpacity < 0.05) return [];
    return [{
      type: 'Feature' as const,
      geometry: buildHexagon(center, sizeKm),
      properties: {
        ...(props as unknown as Record<string, unknown>),
        cellCount: count,
        fillColor,
        outlineColor,
        fillOpacity,
        lineOpacity,
        lineWidth: isOccupied ? 0.72 + normalized * 0.44 : 0.28,
      } as GeoJSON.GeoJsonProperties,
    }];
  });
}

function buildZoneCollections(
  features: StaticZoneFeature[],
  theme: 'dark' | 'light',
  activityById: Record<string, ZoneActivitySnapshot>,
  objects: GeoObject[],
): ZoneCollections {
  const mappedZones: GeoJSON.Feature[] = [];
  const mappedHive: GeoJSON.Feature[] = [];
  const candidateObjects = objects.filter((obj) => Number.isFinite(obj.coordinates?.lat) && Number.isFinite(obj.coordinates?.lon));

  for (const feature of features) {
    if (!feature?.geometry || !feature?.properties) continue;
    const id = String(feature.id || feature.properties.name);
    const activity = activityById[id] || {};
    const hype = clamp(Number(activity.hype || 0), 0, 100);
    const status = String(activity.status || 'inactive');
    const baseColor = zoneTypeColor(feature.properties.zoneType, theme);
    const color = statusTint(status, baseColor);
    const objectsInZone = collectObjectsInGeometry(candidateObjects, feature.geometry, geometryBounds(feature.geometry));
    const style = zoneStyle(status, hype, objectsInZone.length);
    const props: ZoneRenderProperties = {
      id,
      name: String(feature.properties.name || 'Unnamed zone'),
      countryName: String(feature.properties.countryName || ''),
      zoneType: feature.properties.zoneType as StaticZoneType,
      zoneSubType: String(feature.properties.zoneSubType || ''),
      tagsText: Array.isArray(feature.properties.tags)
        ? feature.properties.tags.map((x) => String(x)).slice(0, 4).join(', ')
        : '',
      euActsText: Array.isArray(feature.properties.euActs)
        ? feature.properties.euActs.map((x) => String(x)).slice(0, 3).join(' | ')
        : '',
      color,
      hype,
      status,
      objectCount: objectsInZone.length,
      fillOpacity: style.fillOpacity,
      lineOpacity: style.lineOpacity,
      lineWidth: style.lineWidth,
      cellBaseOpacity: style.cellBaseOpacity,
      cellBoostOpacity: style.cellBoostOpacity,
      cellLineOpacity: style.cellLineOpacity,
      minZoom: typeof feature.properties.minZoom === 'number' ? feature.properties.minZoom : undefined,
      maxZoom: typeof feature.properties.maxZoom === 'number' ? feature.properties.maxZoom : undefined,
    };

    mappedZones.push({
      type: 'Feature',
      id: props.id,
      geometry: feature.geometry,
      properties: props as unknown as GeoJSON.GeoJsonProperties,
    });

    mappedHive.push(...buildHexCellFeatures(feature, props, objectsInZone, theme));
  }

  return {
    zones: { type: 'FeatureCollection', features: mappedZones },
    hive: { type: 'FeatureCollection', features: mappedHive },
  };
}

export function useStaticZoneLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  features: StaticZoneFeature[],
  theme: 'dark' | 'light',
  visible: boolean,
  activityById: Record<string, ZoneActivitySnapshot>,
  objects: GeoObject[],
) {
  const popupRef = useRef<maplibregl.Popup | null>(null);

  useEffect(() => {
    if (!map || !isReady) return;
    ensurePopupStyles();

    const initial: GeoJSON.FeatureCollection = { type: 'FeatureCollection', features: [] };
    map.addSource(SRC, { type: 'geojson', data: initial });
    map.addSource(HIVE_SRC, { type: 'geojson', data: initial });
    const beforeId = map.getLayer('geo-clusters') ? 'geo-clusters' : undefined;

    map.addLayer({
      id: HIVE_FILL_LAYER,
      type: 'fill',
      source: HIVE_SRC,
      layout: {
        visibility: visible ? 'visible' : 'none',
      },
      paint: {
        'fill-color': ['coalesce', ['get', 'fillColor'], ['get', 'color']],
        'fill-opacity': ['coalesce', ['get', 'fillOpacity'], 0.02],
      },
    }, beforeId);

    map.addLayer({
      id: HIVE_OUTLINE_LAYER,
      type: 'line',
      source: HIVE_SRC,
      layout: {
        visibility: visible ? 'visible' : 'none',
      },
      paint: {
        'line-color': ['coalesce', ['get', 'outlineColor'], ['get', 'color']],
        'line-width': ['coalesce', ['get', 'lineWidth'], 0.6],
        'line-opacity': ['coalesce', ['get', 'lineOpacity'], 0.08],
      },
    }, beforeId);

    const interactiveLayers = [HIVE_FILL_LAYER, HIVE_OUTLINE_LAYER];

    const handleMoveEnter = () => {
      map.getCanvas().style.cursor = 'pointer';
    };

    const handleMoveLeave = () => {
      map.getCanvas().style.cursor = '';
    };

    const handleClick = (e: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      try {
        const feature = e.features?.[0];
        if (!feature?.properties) return;
        const raw = feature.properties as Record<string, unknown>;
        const props: ZoneRenderProperties = {
          id: String(raw.id || feature.id || ''),
          name: String(raw.name || 'Zone'),
          countryName: String(raw.countryName || ''),
          zoneType: normalizeZoneType(String(raw.zoneType || '')),
          zoneSubType: String(raw.zoneSubType || ''),
          tagsText: String(raw.tagsText || ''),
          euActsText: String(raw.euActsText || ''),
          color: String(raw.color || ''),
          hype: Number(raw.hype || 0),
          status: String(raw.status || 'inactive'),
          objectCount: Number(raw.objectCount || 0),
          fillOpacity: Number(raw.fillOpacity || 0.05),
          lineOpacity: Number(raw.lineOpacity || 0.3),
          lineWidth: Number(raw.lineWidth || 1),
          cellBaseOpacity: Number(raw.cellBaseOpacity || 0),
          cellBoostOpacity: Number(raw.cellBoostOpacity || 0),
          cellLineOpacity: Number(raw.cellLineOpacity || 0),
        };
        const title = escapeHtml(props.name);
        const subtitle = escapeHtml(props.countryName);
        const zoneType = escapeHtml(formatZoneType(props.zoneType));
        const text = escapeHtml(toEnglishZoneNarrative(props));
        const hype = `${Math.round(props.hype)}/100`;
        const status = escapeHtml(props.status);
        const fragments = Math.max(0, Math.round(props.objectCount));
        const accent = escapeHtml(props.color || (theme === 'dark' ? '#94a3b8' : '#475569'));

        popupRef.current?.remove();
        const popup = new maplibregl.Popup({
          closeButton: true,
          closeOnClick: true,
          maxWidth: '320px',
          className: `static-zones-popup ${theme === 'dark' ? 'static-zones-popup-dark' : 'static-zones-popup-light'}`,
        })
          .setLngLat(e.lngLat)
          .setHTML(
            `<div class="static-zones-popup-card" style="--popup-accent:${accent}; font-family: ui-sans-serif,system-ui,sans-serif;">` +
              `<div class="static-zones-popup-kicker">${zoneType}</div>` +
              `<div class="static-zones-popup-title">${title}</div>` +
              `<div class="static-zones-popup-subtitle">${subtitle}</div>` +
              `<div class="static-zones-popup-body">${text}</div>` +
              `<div class="static-zones-popup-meta">` +
                `<span class="static-zones-popup-chip"><strong>${hype}</strong><span>Hype</span></span>` +
                `<span class="static-zones-popup-chip"><strong>${status}</strong><span>Status</span></span>` +
                `<span class="static-zones-popup-chip"><strong>${fragments}</strong><span>Fragments</span></span>` +
              `</div>` +
            `</div>`
          )
          .addTo(map);
        popupRef.current = popup;
      } catch (err) {
        console.warn('[useStaticZoneLayer] popup render failed:', err);
      }
    };

    for (const layerId of interactiveLayers) {
      map.on('mouseenter', layerId, handleMoveEnter);
      map.on('mouseleave', layerId, handleMoveLeave);
      map.on('click', layerId, handleClick);
    }

    return () => {
      popupRef.current?.remove();
      popupRef.current = null;
      try {
        for (const layerId of interactiveLayers) {
          map.off('mouseenter', layerId, handleMoveEnter);
          map.off('mouseleave', layerId, handleMoveLeave);
          map.off('click', layerId, handleClick);
        }
        if (map.getLayer(HIVE_OUTLINE_LAYER)) map.removeLayer(HIVE_OUTLINE_LAYER);
        if (map.getLayer(HIVE_FILL_LAYER)) map.removeLayer(HIVE_FILL_LAYER);
        if (map.getSource(HIVE_SRC)) map.removeSource(HIVE_SRC);
        if (map.getSource(SRC)) map.removeSource(SRC);
      } catch {
        // map disposed
      }
    };
  }, [map, isReady, styleEpoch]);

  useEffect(() => {
    if (!map || !isReady) return;
    if (!canAccessMapStyle(map)) return;
    const zoneSrc = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
    const hiveSrc = map.getSource(HIVE_SRC) as maplibregl.GeoJSONSource | undefined;
    if (!zoneSrc || !hiveSrc) return;

    const collections = buildZoneCollections(features, theme, activityById || {}, objects || []);
    zoneSrc.setData(collections.zones);
    hiveSrc.setData(collections.hive);

    const visibility = visible ? 'visible' : 'none';
    for (const layerId of [HIVE_FILL_LAYER, HIVE_OUTLINE_LAYER]) {
      if (map.getLayer(layerId)) map.setLayoutProperty(layerId, 'visibility', visibility);
    }
  }, [map, isReady, features, theme, visible, activityById, objects]);
}
