import { useEffect, useRef } from 'react';
import maplibregl from 'maplibre-gl';
import {
  formatTransportFlowMode,
  transportFlowModeColor,
  transportFlowModeOpacity,
  transportFlowModeStrokeWidth,
  type TransportFlowFeature,
  type TransportFlowMode,
} from '../lib/staticTransportFlows';

const SRC = 'transport-flows-src';
const GLOW_LAYER = 'transport-flows-glow';
const MAIN_LAYER = 'transport-flows-main';
const ENDPOINT_SRC = 'transport-flows-endpoints-src';
const ENDPOINT_GLOW_LAYER = 'transport-flows-endpoints-glow';
const ENDPOINT_LAYER = 'transport-flows-endpoints';
const ENDPOINT_ICON_LAYER = 'transport-flows-endpoints-icon';

interface FlowRenderProperties {
  id: string;
  name: string;
  flowMode: TransportFlowMode;
  flowSubType: string;
  tagsText: string;
  color: string;
  width: number;
  opacity: number;
  hype: number;
  status: string;
}

type FlowEndpointSide = 'origin' | 'destination';
type EndpointIconKind =
  | 'sea'
  | 'air'
  | 'road'
  | 'rail'
  | 'inland'
  | 'intermodal'
  | 'pipeline'
  | 'power'
  | 'ferry'
  | 'internet';

interface EndpointRenderProperties extends FlowRenderProperties {
  endpointSide: FlowEndpointSide;
  iconId: string;
  iconScale: number;
}

interface FlowActivitySnapshot {
  hype?: number;
  status?: 'hot' | 'active' | 'watch' | 'cooling' | 'inactive';
}

type LonLat = [number, number];
type Vec3 = [number, number, number];

const POPUP_STYLE_ID = 'transport-flows-popup-style';
const ENDPOINT_ICON_KINDS: EndpointIconKind[] = ['sea', 'air', 'road', 'rail', 'inland', 'intermodal', 'pipeline', 'power', 'ferry', 'internet'];

function canAccessMapStyle(map: maplibregl.Map): boolean {
  try {
    return Boolean((map as unknown as { style?: unknown }).style);
  } catch {
    return false;
  }
}

function endpointIconKind(mode: TransportFlowMode): EndpointIconKind {
  switch (mode) {
    case 'sea': return 'sea';
    case 'air': return 'air';
    case 'road': return 'road';
    case 'rail': return 'rail';
    case 'inland_waterway': return 'inland';
    case 'intermodal': return 'intermodal';
    case 'pipeline_oil':
    case 'pipeline_gas':
    case 'pipeline_h2':
    case 'pipeline_co2': return 'pipeline';
    case 'power_hvdc':
    case 'power_hvac': return 'power';
    case 'ferry_roro': return 'ferry';
    case 'internet_cable': return 'internet';
    default: return 'road';
  }
}

function endpointIconId(kind: EndpointIconKind): string {
  return `transport-flow-endpoint-icon-${kind}`;
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function makeEndpointIcon(kind: EndpointIconKind): ImageData | null {
  if (typeof document === 'undefined') return null;
  const size = 48;
  const c = document.createElement('canvas');
  c.width = size;
  c.height = size;
  const ctx = c.getContext('2d');
  if (!ctx) return null;

  ctx.clearRect(0, 0, size, size);
  ctx.strokeStyle = '#f8fafc';
  ctx.fillStyle = '#f8fafc';
  ctx.lineWidth = 3.2;
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';

  const line = (x1: number, y1: number, x2: number, y2: number) => {
    ctx.beginPath();
    ctx.moveTo(x1, y1);
    ctx.lineTo(x2, y2);
    ctx.stroke();
  };

  switch (kind) {
    case 'sea':
      for (const y of [20, 27, 34]) {
        ctx.beginPath();
        ctx.moveTo(9, y);
        ctx.quadraticCurveTo(16, y - 4, 24, y);
        ctx.quadraticCurveTo(32, y + 4, 39, y);
        ctx.stroke();
      }
      break;
    case 'air':
      line(8, 24, 40, 24);
      line(22, 24, 13, 14);
      line(22, 24, 13, 34);
      line(32, 24, 38, 18);
      line(32, 24, 38, 30);
      break;
    case 'road':
      line(15, 10, 11, 38);
      line(33, 10, 37, 38);
      ctx.setLineDash([4, 4]);
      line(24, 10, 24, 38);
      ctx.setLineDash([]);
      break;
    case 'rail':
      line(15, 9, 15, 39);
      line(33, 9, 33, 39);
      for (const y of [12, 17, 22, 27, 32, 37]) line(14, y, 34, y);
      break;
    case 'inland':
      line(10, 18, 34, 18);
      line(14, 24, 26, 24);
      for (const y of [30, 36]) {
        ctx.beginPath();
        ctx.moveTo(9, y);
        ctx.quadraticCurveTo(16, y - 3, 24, y);
        ctx.quadraticCurveTo(32, y + 3, 39, y);
        ctx.stroke();
      }
      break;
    case 'intermodal':
      ctx.strokeRect(8, 20, 10, 10);
      ctx.strokeRect(20, 15, 10, 10);
      ctx.strokeRect(32, 20, 8, 10);
      line(18, 25, 20, 20);
      line(30, 20, 32, 25);
      break;
    case 'pipeline':
      line(8, 24, 40, 24);
      ctx.beginPath();
      ctx.arc(13, 24, 3.5, 0, Math.PI * 2);
      ctx.stroke();
      ctx.beginPath();
      ctx.arc(35, 24, 3.5, 0, Math.PI * 2);
      ctx.stroke();
      break;
    case 'power':
      ctx.beginPath();
      ctx.moveTo(27, 8);
      ctx.lineTo(17, 24);
      ctx.lineTo(25, 24);
      ctx.lineTo(20, 40);
      ctx.lineTo(33, 21);
      ctx.lineTo(24, 21);
      ctx.closePath();
      ctx.fill();
      break;
    case 'ferry':
      ctx.beginPath();
      ctx.moveTo(10, 28);
      ctx.lineTo(38, 28);
      ctx.lineTo(33, 34);
      ctx.lineTo(15, 34);
      ctx.closePath();
      ctx.stroke();
      ctx.strokeRect(18, 20, 8, 7);
      line(29, 20, 29, 27);
      break;
    case 'internet':
      ctx.beginPath();
      ctx.arc(24, 24, 12, 0, Math.PI * 2);
      ctx.stroke();
      line(12, 24, 36, 24);
      line(24, 12, 24, 36);
      ctx.beginPath();
      ctx.ellipse(24, 24, 6, 12, 0, 0, Math.PI * 2);
      ctx.stroke();
      break;
    default:
      line(10, 24, 38, 24);
  }

  return ctx.getImageData(0, 0, size, size);
}

function ensureEndpointIcons(map: maplibregl.Map): void {
  if (!canAccessMapStyle(map)) return;
  for (const kind of ENDPOINT_ICON_KINDS) {
    const id = endpointIconId(kind);
    if (map.hasImage(id)) continue;
    const imageData = makeEndpointIcon(kind);
    if (!imageData) continue;
    map.addImage(id, imageData, { pixelRatio: 2 });
  }
}

function ensurePopupStyles(): void {
  if (typeof document === 'undefined') return;
  if (document.getElementById(POPUP_STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = POPUP_STYLE_ID;
  style.textContent = `
.transport-flows-popup-dark .maplibregl-popup-content {
  background: rgba(7, 12, 20, 0.97);
  color: #e2e8f0;
  border: 1px solid rgba(148,163,184,0.35);
  border-radius: 10px;
  box-shadow: 0 14px 34px rgba(2,6,23,0.6);
  padding: 10px 12px;
}
.transport-flows-popup-light .maplibregl-popup-content {
  background: rgba(255,255,255,0.98);
  color: #0f172a;
  border: 1px solid rgba(30,41,59,0.2);
  border-radius: 10px;
  box-shadow: 0 12px 28px rgba(15,23,42,0.22);
  padding: 10px 12px;
}
.transport-flows-popup-dark .maplibregl-popup-close-button { color: rgba(226,232,240,0.9); }
.transport-flows-popup-light .maplibregl-popup-close-button { color: rgba(15,23,42,0.75); }
.transport-flows-popup-dark.maplibregl-popup-anchor-top .maplibregl-popup-tip,
.transport-flows-popup-dark.maplibregl-popup-anchor-top-left .maplibregl-popup-tip,
.transport-flows-popup-dark.maplibregl-popup-anchor-top-right .maplibregl-popup-tip { border-bottom-color: rgba(7,12,20,0.97); }
.transport-flows-popup-dark.maplibregl-popup-anchor-bottom .maplibregl-popup-tip,
.transport-flows-popup-dark.maplibregl-popup-anchor-bottom-left .maplibregl-popup-tip,
.transport-flows-popup-dark.maplibregl-popup-anchor-bottom-right .maplibregl-popup-tip { border-top-color: rgba(7,12,20,0.97); }
.transport-flows-popup-dark.maplibregl-popup-anchor-left .maplibregl-popup-tip { border-right-color: rgba(7,12,20,0.97); }
.transport-flows-popup-dark.maplibregl-popup-anchor-right .maplibregl-popup-tip { border-left-color: rgba(7,12,20,0.97); }
.transport-flows-popup-light.maplibregl-popup-anchor-top .maplibregl-popup-tip,
.transport-flows-popup-light.maplibregl-popup-anchor-top-left .maplibregl-popup-tip,
.transport-flows-popup-light.maplibregl-popup-anchor-top-right .maplibregl-popup-tip { border-bottom-color: rgba(255,255,255,0.98); }
.transport-flows-popup-light.maplibregl-popup-anchor-bottom .maplibregl-popup-tip,
.transport-flows-popup-light.maplibregl-popup-anchor-bottom-left .maplibregl-popup-tip,
.transport-flows-popup-light.maplibregl-popup-anchor-bottom-right .maplibregl-popup-tip { border-top-color: rgba(255,255,255,0.98); }
.transport-flows-popup-light.maplibregl-popup-anchor-left .maplibregl-popup-tip { border-right-color: rgba(255,255,255,0.98); }
.transport-flows-popup-light.maplibregl-popup-anchor-right .maplibregl-popup-tip { border-left-color: rgba(255,255,255,0.98); }
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

function normalizeTransportMode(input: string): TransportFlowMode {
  const key = (input || '').trim() as TransportFlowMode;
  if (
    key === 'sea' ||
    key === 'air' ||
    key === 'road' ||
    key === 'rail' ||
    key === 'inland_waterway' ||
    key === 'intermodal' ||
    key === 'pipeline_oil' ||
    key === 'pipeline_gas' ||
    key === 'pipeline_h2' ||
    key === 'pipeline_co2' ||
    key === 'power_hvdc' ||
    key === 'power_hvac' ||
    key === 'ferry_roro' ||
    key === 'internet_cable'
  ) return key;
  return 'road';
}

function toEnglishFlowNarrative(props: FlowRenderProperties): string {
  const byMode: Record<TransportFlowMode, string> = {
    sea: 'High-volume maritime cargo moves through this lane.',
    air: 'Time-sensitive cargo and passenger transfer concentrates in this corridor.',
    road: 'Truck-based inland freight dependency is concentrated along this corridor.',
    rail: 'Bulk and scheduled freight transfer concentrates along this rail corridor.',
    inland_waterway: 'River and canal barge transport supports heavy and bulk cargo here.',
    intermodal: 'Container handoff between port, rail, road, and air nodes is concentrated here.',
    pipeline_oil: 'Crude and refined oil pipeline transfer is concentrated on this route.',
    pipeline_gas: 'Natural gas transmission capacity is concentrated on this pipeline route.',
    pipeline_h2: 'Hydrogen backbone transport is planned or operating in this corridor.',
    pipeline_co2: 'Captured CO2 transfer toward utilization or storage is concentrated here.',
    power_hvdc: 'Long-distance high-voltage direct current power transfer runs through this corridor.',
    power_hvac: 'Regional high-voltage alternating current grid transfer concentrates here.',
    ferry_roro: 'Scheduled ferry and Ro-Ro roll-on cargo transfer is concentrated on this lane.',
    internet_cable: 'Subsea and backbone internet cable capacity is concentrated on this route.',
  };
  const modeText = byMode[props.flowMode] || byMode.road;
  const subtype = props.flowSubType ? ` Focus: ${props.flowSubType.replace(/_/g, ' ')}.` : '';
  const tags = props.tagsText ? ` Signals: ${props.tagsText}.` : '';
  return `${modeText}${subtype}${tags}`;
}

function roundCoord(v: number): number {
  return Math.round(v * 1e6) / 1e6;
}

function modeDensity(mode: TransportFlowMode): number {
  switch (mode) {
    case 'sea': return 1.6;
    case 'air': return 1.8;
    case 'road': return 1.4;
    case 'rail': return 1.5;
    case 'inland_waterway': return 1.5;
    case 'intermodal': return 1.7;
    case 'pipeline_oil': return 1.8;
    case 'pipeline_gas': return 1.8;
    case 'pipeline_h2': return 1.8;
    case 'pipeline_co2': return 1.8;
    case 'power_hvdc': return 1.9;
    case 'power_hvac': return 1.8;
    case 'ferry_roro': return 2.0;
    case 'internet_cable': return 2.2;
    default: return 1.6;
  }
}

function modeNativeQualityThreshold(mode: TransportFlowMode): number {
  switch (mode) {
    case 'sea': return 0.9;
    case 'air': return 0.9;
    case 'road': return 0.45;
    case 'rail': return 0.45;
    case 'inland_waterway': return 0.35;
    case 'intermodal': return 0.3;
    case 'pipeline_oil': return 0.35;
    case 'pipeline_gas': return 0.35;
    case 'pipeline_h2': return 0.35;
    case 'pipeline_co2': return 0.35;
    case 'power_hvdc': return 0.45;
    case 'power_hvac': return 0.45;
    case 'ferry_roro': return 0.3;
    case 'internet_cable': return 0.75;
    default: return 0.5;
  }
}

function toRadians(deg: number): number {
  return (deg * Math.PI) / 180;
}

function toDegrees(rad: number): number {
  return (rad * 180) / Math.PI;
}

function lonLatToVec3(lon: number, lat: number): Vec3 {
  const lonRad = toRadians(lon);
  const latRad = toRadians(lat);
  const c = Math.cos(latRad);
  return [c * Math.cos(lonRad), c * Math.sin(lonRad), Math.sin(latRad)];
}

function vec3ToLonLat([x, y, z]: Vec3): LonLat {
  const h = Math.sqrt(x * x + y * y) || 1;
  return [toDegrees(Math.atan2(y, x)), toDegrees(Math.atan2(z, h))];
}

function normalizeVec3([x, y, z]: Vec3): Vec3 {
  const len = Math.sqrt(x * x + y * y + z * z) || 1;
  return [x / len, y / len, z / len];
}

function lerp(a: number, b: number, t: number): number {
  return a + (b - a) * t;
}

function angularDistanceDeg(a: LonLat, b: LonLat): number {
  const v1 = lonLatToVec3(a[0], a[1]);
  const v2 = lonLatToVec3(b[0], b[1]);
  const dot = Math.max(-1, Math.min(1, v1[0] * v2[0] + v1[1] * v2[1] + v1[2] * v2[2]));
  return toDegrees(Math.acos(dot));
}

function densifySegment(a: LonLat, b: LonLat, mode: TransportFlowMode): LonLat[] {
  const lon1 = a[0];
  const lat1 = a[1];
  let lon2 = b[0];
  const lat2 = b[1];
  while (lon2 - lon1 > 180) lon2 -= 360;
  while (lon2 - lon1 < -180) lon2 += 360;

  const v1 = lonLatToVec3(lon1, lat1);
  const v2 = lonLatToVec3(lon2, lat2);
  const dot = Math.max(-1, Math.min(1, v1[0] * v2[0] + v1[1] * v2[1] + v1[2] * v2[2]));
  const omega = Math.acos(dot);
  const omegaDeg = toDegrees(omega);
  const steps = Math.max(6, Math.min(240, Math.ceil(omegaDeg * modeDensity(mode))));

  const points: LonLat[] = [];
  if (omega < 1e-7) {
    for (let i = 0; i <= steps; i++) {
      const t = i / steps;
      points.push([lerp(lon1, lon2, t), lerp(lat1, lat2, t)]);
    }
    return points;
  }

  const sinOmega = Math.sin(omega);
  for (let i = 0; i <= steps; i++) {
    const t = i / steps;
    const s1 = Math.sin((1 - t) * omega) / sinOmega;
    const s2 = Math.sin(t * omega) / sinOmega;
    const vec = normalizeVec3([
      v1[0] * s1 + v2[0] * s2,
      v1[1] * s1 + v2[1] * s2,
      v1[2] * s1 + v2[2] * s2,
    ]);
    points.push(vec3ToLonLat(vec));
  }
  return points;
}

function unwrapLine(line: LonLat[]): LonLat[] {
  if (line.length === 0) return [];
  const out: LonLat[] = [[line[0][0], line[0][1]]];
  for (let i = 1; i < line.length; i++) {
    let lon = line[i][0];
    const lat = line[i][1];
    const prevLon = out[out.length - 1][0];
    while (lon - prevLon > 180) lon -= 360;
    while (lon - prevLon < -180) lon += 360;
    out.push([lon, lat]);
  }
  return out;
}

function wrapLonTo180(lon: number): number {
  const wrapped = ((lon + 180) % 360 + 360) % 360 - 180;
  if (wrapped === -180 && lon > 0) return 180;
  return wrapped;
}

function lonZone(lon: number): number {
  return Math.floor((lon + 180 - 1e-9) / 360);
}

function splitAtDateline(unwrapped: LonLat[]): LonLat[][] {
  if (unwrapped.length === 0) return [];
  const segments: LonLat[][] = [];
  let prev: LonLat = unwrapped[0];
  let current: LonLat[] = [[roundCoord(wrapLonTo180(prev[0])), roundCoord(prev[1])]];

  for (let i = 1; i < unwrapped.length; i++) {
    const next = unwrapped[i];
    const prevZone = lonZone(prev[0]);
    const nextZone = lonZone(next[0]);

    if (prevZone === nextZone) {
      current.push([roundCoord(wrapLonTo180(next[0])), roundCoord(next[1])]);
      prev = next;
      continue;
    }

    const movingEast = nextZone > prevZone;
    const boundaryUnwrapped = movingEast ? 180 + prevZone * 360 : -180 + prevZone * 360;
    const denom = next[0] - prev[0];
    if (Math.abs(denom) < 1e-9) {
      current.push([roundCoord(wrapLonTo180(next[0])), roundCoord(next[1])]);
      prev = next;
      continue;
    }
    const t = (boundaryUnwrapped - prev[0]) / denom;
    const latCross = prev[1] + t * (next[1] - prev[1]);
    const endLon: number = movingEast ? 180 : -180;
    const startLon: number = movingEast ? -180 : 180;

    current.push([endLon, roundCoord(latCross)]);
    if (current.length > 1) segments.push(current);

    current = [[startLon, roundCoord(latCross)], [roundCoord(wrapLonTo180(next[0])), roundCoord(next[1])]];
    prev = next;
  }
  if (current.length > 1) segments.push(current);
  return segments;
}

function smoothAndSplitLine(line: LonLat[], mode: TransportFlowMode): LonLat[][] {
  if (line.length < 2) return [];
  if (line.length >= 20) {
    let total = 0;
    for (let i = 0; i < line.length - 1; i++) total += angularDistanceDeg(line[i], line[i + 1]);
    const avg = total / (line.length - 1);
    if (avg <= modeNativeQualityThreshold(mode)) {
      return splitAtDateline(unwrapLine(line));
    }
  }
  const densified: LonLat[] = [];
  for (let i = 0; i < line.length - 1; i++) {
    const seg = densifySegment(line[i], line[i + 1], mode);
    if (i > 0) seg.shift();
    densified.push(...seg);
  }
  return splitAtDateline(unwrapLine(densified));
}

const FLOW_DASHARRAY_EXPRESSION: unknown = [
  'match',
  ['get', 'flowMode'],
  'air', ['literal', [1.4, 1.4]],
  'pipeline_oil', ['literal', [2.4, 0.8]],
  'pipeline_gas', ['literal', [1.2, 0.8]],
  'pipeline_h2', ['literal', [0.6, 1.1]],
  'pipeline_co2', ['literal', [0.4, 1.3]],
  'power_hvdc', ['literal', [2.1, 0.9]],
  'power_hvac', ['literal', [1.0, 1.0]],
  'ferry_roro', ['literal', [0.5, 1.2]],
  'internet_cable', ['literal', [2.8, 1.2]],
  ['literal', [1, 0.001]],
];
const HOVER_FLAG_EXPRESSION: unknown = ['boolean', ['feature-state', 'hover'], false];

function flowPropsFromFeature(
  feature: TransportFlowFeature,
  theme: 'dark' | 'light',
  activityById: Record<string, FlowActivitySnapshot>,
): FlowRenderProperties | null {
  if (!feature?.properties) return null;
  const flowMode = normalizeTransportMode(String(feature.properties.flowMode || ''));
  const id = String(feature.id || feature.properties.name);
  const activity = activityById[id] || {};
  const hype = Math.max(0, Math.min(100, Number(activity.hype || 0)));
  const status = String(activity.status || 'inactive');
  const baseColor = transportFlowModeColor(flowMode, theme);
  const color = status === 'hot'
    ? '#ef4444'
    : status === 'active'
      ? '#f97316'
      : status === 'watch'
        ? '#f59e0b'
        : baseColor;
  const width = transportFlowModeStrokeWidth(flowMode) * (1 + hype * 0.011);
  const opacity = Math.min(0.98, transportFlowModeOpacity(flowMode) * (0.45 + hype / 100));
  return {
    id,
    name: String(feature.properties.name || 'Transport flow'),
    flowMode,
    flowSubType: String(feature.properties.flowSubType || ''),
    tagsText: Array.isArray(feature.properties.tags)
      ? feature.properties.tags.map((x) => String(x)).slice(0, 4).join(', ')
      : '',
    color,
    width,
    opacity,
    hype,
    status,
  };
}

function buildFlowCollection(
  features: TransportFlowFeature[],
  theme: 'dark' | 'light',
  activityById: Record<string, FlowActivitySnapshot>,
): GeoJSON.FeatureCollection {
  const mapped: GeoJSON.Feature[] = [];
  for (const feature of features) {
    if (!feature?.geometry || !feature?.properties) continue;
    const props = flowPropsFromFeature(feature, theme, activityById);
    if (!props) continue;
    const flowMode = props.flowMode;
    const lineStrings: LonLat[][] = feature.geometry.type === 'LineString'
      ? [feature.geometry.coordinates as LonLat[]]
      : (feature.geometry.coordinates as LonLat[][]);
    const segments: LonLat[][] = [];
    for (const line of lineStrings) {
      segments.push(...smoothAndSplitLine(line, flowMode));
    }
    if (segments.length === 0) continue;

    mapped.push({
      type: 'Feature',
      id: props.id,
      geometry: segments.length === 1
        ? { type: 'LineString', coordinates: segments[0] }
        : { type: 'MultiLineString', coordinates: segments },
      properties: props as unknown as GeoJSON.GeoJsonProperties,
    });
  }
  return { type: 'FeatureCollection', features: mapped };
}

function buildEndpointCollection(
  features: TransportFlowFeature[],
  theme: 'dark' | 'light',
  activityById: Record<string, FlowActivitySnapshot>,
): GeoJSON.FeatureCollection {
  const mapped: GeoJSON.Feature[] = [];
  for (const feature of features) {
    if (!feature?.geometry || !feature?.properties) continue;
    const props = flowPropsFromFeature(feature, theme, activityById);
    if (!props) continue;
    const iconId = endpointIconId(endpointIconKind(props.flowMode));
    const iconScale = clamp(0.9 + props.hype / 190, 0.9, 1.42);
    const lineStrings: LonLat[][] = feature.geometry.type === 'LineString'
      ? [feature.geometry.coordinates as LonLat[]]
      : (feature.geometry.coordinates as LonLat[][]);

    for (let i = 0; i < lineStrings.length; i++) {
      const line = lineStrings[i];
      if (!line || line.length < 2) continue;
      const start = line[0];
      const end = line[line.length - 1];

      const originProps: EndpointRenderProperties = {
        ...props,
        endpointSide: 'origin',
        iconId,
        iconScale,
      };
      mapped.push({
        type: 'Feature',
        id: `${props.id}::${i}::origin`,
        geometry: { type: 'Point', coordinates: [roundCoord(start[0]), roundCoord(start[1])] },
        properties: originProps as unknown as GeoJSON.GeoJsonProperties,
      });

      if (start[0] === end[0] && start[1] === end[1]) continue;
      const destinationProps: EndpointRenderProperties = {
        ...props,
        endpointSide: 'destination',
        iconId,
        iconScale,
      };
      mapped.push({
        type: 'Feature',
        id: `${props.id}::${i}::destination`,
        geometry: { type: 'Point', coordinates: [roundCoord(end[0]), roundCoord(end[1])] },
        properties: destinationProps as unknown as GeoJSON.GeoJsonProperties,
      });
    }
  }
  return { type: 'FeatureCollection', features: mapped };
}

export function useTransportFlowLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  features: TransportFlowFeature[],
  theme: 'dark' | 'light',
  visible: boolean,
  activityById: Record<string, FlowActivitySnapshot>,
) {
  const popupRef = useRef<maplibregl.Popup | null>(null);
  const hoverPopupRef = useRef<maplibregl.Popup | null>(null);
  const hoveredLineIdRef = useRef<string | number | null>(null);
  const hoveredEndpointIdRef = useRef<string | number | null>(null);

  const clearHoverState = (targetMap: maplibregl.Map) => {
    try {
      if (hoveredLineIdRef.current !== null) {
        targetMap.setFeatureState({ source: SRC, id: hoveredLineIdRef.current }, { hover: false });
      }
      if (hoveredEndpointIdRef.current !== null) {
        targetMap.setFeatureState({ source: ENDPOINT_SRC, id: hoveredEndpointIdRef.current }, { hover: false });
      }
    } catch {
      // source/style may be disposed
    }
    hoveredLineIdRef.current = null;
    hoveredEndpointIdRef.current = null;
  };

  const setHoverState = (
    targetMap: maplibregl.Map,
    lineId: string | number | null,
    endpointId: string | number | null,
  ) => {
    if (hoveredLineIdRef.current === lineId && hoveredEndpointIdRef.current === endpointId) return;
    clearHoverState(targetMap);
    try {
      if (lineId !== null) targetMap.setFeatureState({ source: SRC, id: lineId }, { hover: true });
      if (endpointId !== null) targetMap.setFeatureState({ source: ENDPOINT_SRC, id: endpointId }, { hover: true });
    } catch {
      // source/style reloading
    }
    hoveredLineIdRef.current = lineId;
    hoveredEndpointIdRef.current = endpointId;
  };

  const renderPopupHtml = (props: FlowRenderProperties, endpointSide?: FlowEndpointSide): string => {
    const title = escapeHtml(props.name);
    const mode = escapeHtml(formatTransportFlowMode(props.flowMode));
    const endpointText = endpointSide === 'origin'
      ? 'Origin endpoint. '
      : endpointSide === 'destination'
        ? 'Destination endpoint. '
        : '';
    const text = escapeHtml(`${endpointText}${toEnglishFlowNarrative(props)}`);
    const hype = `${Math.round(props.hype)}/100`;
    const status = escapeHtml(props.status);
    return (
      `<div style="font-family: ui-sans-serif,system-ui,sans-serif; font-size:12px; line-height:1.4;">` +
        `<div style="font-weight:700; margin-bottom:2px;">${title}</div>` +
        `<div style="font-size:11px; opacity:.8; margin-bottom:6px;">${mode}</div>` +
        `<div style="font-size:11px; margin-bottom:2px;">${text}</div>` +
        `<div style="font-size:10px; opacity:.88;">Hype: ${hype} • Status: ${status}</div>` +
      `</div>`
    );
  };

  const parseFeature = (feature: maplibregl.MapGeoJSONFeature | undefined) => {
    if (!feature?.properties) return null;
    const raw = feature.properties as Record<string, unknown>;
    const flowMode = normalizeTransportMode(String(raw.flowMode || ''));
    const lineIdRaw = String(raw.id || feature.id || '');
    const lineId = lineIdRaw || null;
    const endpointId = feature.geometry.type === 'Point' ? (feature.id as string | number | undefined) ?? null : null;
    const endpointSideRaw = String(raw.endpointSide || '');
    const endpointSide: FlowEndpointSide | undefined = endpointSideRaw === 'origin' || endpointSideRaw === 'destination'
      ? endpointSideRaw
      : undefined;
    const props: FlowRenderProperties = {
      id: lineIdRaw,
      name: String(raw.name || 'Transport flow'),
      flowMode,
      flowSubType: String(raw.flowSubType || ''),
      tagsText: String(raw.tagsText || ''),
      color: String(raw.color || ''),
      width: Number(raw.width || 1.8),
      opacity: Number(raw.opacity || 0.8),
      hype: Number(raw.hype || 0),
      status: String(raw.status || 'inactive'),
    };
    return { props, lineId, endpointId, endpointSide };
  };

  useEffect(() => {
    if (!map || !isReady) return;
    ensurePopupStyles();
    ensureEndpointIcons(map);

    map.addSource(SRC, {
      type: 'geojson',
      data: buildFlowCollection([], theme, {}),
    });
    map.addSource(ENDPOINT_SRC, {
      type: 'geojson',
      data: buildEndpointCollection([], theme, {}),
    });

    const beforeId = map.getLayer('geo-clusters') ? 'geo-clusters' : undefined;

    map.addLayer({
      id: GLOW_LAYER,
      type: 'line',
      source: SRC,
      layout: {
        visibility: visible ? 'visible' : 'none',
        'line-cap': 'round',
        'line-join': 'round',
      },
      paint: {
        'line-color': ['get', 'color'],
        'line-width': [
          'interpolate', ['linear'], ['zoom'],
          2, ['*', ['*', ['get', 'width'], 1.1], ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 1.2, 1]],
          7, ['*', ['*', ['get', 'width'], 1.8], ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 1.2, 1]],
        ],
        'line-opacity': ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, ['*', ['get', 'opacity'], 0.55], ['*', ['get', 'opacity'], 0.12]],
        'line-dasharray': FLOW_DASHARRAY_EXPRESSION as maplibregl.ExpressionSpecification,
        'line-blur': 0.6,
      },
    }, beforeId);

    map.addLayer({
      id: MAIN_LAYER,
      type: 'line',
      source: SRC,
      layout: {
        visibility: visible ? 'visible' : 'none',
        'line-cap': 'round',
        'line-join': 'round',
      },
      paint: {
        'line-color': ['get', 'color'],
        'line-width': [
          'interpolate', ['linear'], ['zoom'],
          2, ['*', ['*', ['get', 'width'], 0.7], ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 1.45, 1]],
          7, ['*', ['get', 'width'], ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 1.45, 1]],
          10, ['*', ['*', ['get', 'width'], 1.3], ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 1.45, 1]],
        ],
        'line-opacity': ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, ['*', ['get', 'opacity'], 1.05], ['*', ['get', 'opacity'], 0.34]],
        'line-dasharray': FLOW_DASHARRAY_EXPRESSION as maplibregl.ExpressionSpecification,
      },
    }, beforeId);

    map.addLayer({
      id: ENDPOINT_GLOW_LAYER,
      type: 'circle',
      source: ENDPOINT_SRC,
      layout: { visibility: visible ? 'visible' : 'none' },
      paint: {
        'circle-color': ['get', 'color'],
        'circle-radius': [
          'interpolate', ['linear'], ['zoom'],
          2, ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 5.2, 3.5],
          7, ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 9, 6.5],
          10, ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 11, 8],
        ],
        'circle-opacity': ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 0.55, 0.2],
        'circle-blur': 0.7,
      },
    }, beforeId);

    map.addLayer({
      id: ENDPOINT_LAYER,
      type: 'circle',
      source: ENDPOINT_SRC,
      layout: { visibility: visible ? 'visible' : 'none' },
      paint: {
        'circle-color': ['get', 'color'],
        'circle-radius': [
          'interpolate', ['linear'], ['zoom'],
          2, ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 3.2, 2.1],
          7, ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 5.7, 3.8],
          10, ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 7, 5.2],
        ],
        'circle-opacity': ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 0.96, 0.52],
        'circle-stroke-color': theme === 'dark' ? '#0b1220' : '#f8fafc',
        'circle-stroke-width': ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 1.8, 1.2],
      },
    }, beforeId);

    map.addLayer({
      id: ENDPOINT_ICON_LAYER,
      type: 'symbol',
      source: ENDPOINT_SRC,
      layout: {
        visibility: visible ? 'visible' : 'none',
        'icon-image': ['get', 'iconId'] as unknown as maplibregl.ExpressionSpecification,
        'icon-size': [
          'interpolate', ['linear'], ['zoom'],
          2, ['*', ['coalesce', ['get', 'iconScale'], 1], 0.34],
          7, ['*', ['coalesce', ['get', 'iconScale'], 1], 0.46],
          10, ['*', ['coalesce', ['get', 'iconScale'], 1], 0.56],
        ],
        'icon-allow-overlap': true,
        'icon-ignore-placement': true,
      },
      paint: {
        'icon-opacity': ['case', HOVER_FLAG_EXPRESSION as maplibregl.ExpressionSpecification, 1, 0.65],
      },
    }, beforeId);

    const interactiveLayers: string[] = [ENDPOINT_ICON_LAYER, ENDPOINT_LAYER, MAIN_LAYER];
    const getQueryableLayers = (): string[] => interactiveLayers.filter((layerId) => Boolean(map.getLayer(layerId)));

    const handleMouseMove = (e: maplibregl.MapMouseEvent) => {
      try {
        const queryLayers = getQueryableLayers();
        if (queryLayers.length === 0) {
          map.getCanvas().style.cursor = '';
          clearHoverState(map);
          hoverPopupRef.current?.remove();
          hoverPopupRef.current = null;
          return;
        }
        const hits = map.queryRenderedFeatures(e.point, { layers: queryLayers }) as maplibregl.MapGeoJSONFeature[];
        const parsed = parseFeature(hits[0]);
        if (!parsed) {
          map.getCanvas().style.cursor = '';
          clearHoverState(map);
          hoverPopupRef.current?.remove();
          hoverPopupRef.current = null;
          return;
        }
        map.getCanvas().style.cursor = 'pointer';
        setHoverState(map, parsed.lineId, parsed.endpointId);

        hoverPopupRef.current?.remove();
        hoverPopupRef.current = new maplibregl.Popup({
          closeButton: false,
          closeOnClick: false,
          closeOnMove: false,
          maxWidth: '320px',
          className: `transport-flows-popup ${theme === 'dark' ? 'transport-flows-popup-dark' : 'transport-flows-popup-light'}`,
          offset: 10,
        })
          .setLngLat(e.lngLat)
          .setHTML(renderPopupHtml(parsed.props, parsed.endpointSide))
          .addTo(map);
      } catch {
        // transient during style reload
      }
    };

    const handleMouseOut = () => {
      map.getCanvas().style.cursor = '';
      clearHoverState(map);
      hoverPopupRef.current?.remove();
      hoverPopupRef.current = null;
    };

    const handleClick = (e: maplibregl.MapMouseEvent) => {
      try {
        const queryLayers = getQueryableLayers();
        if (queryLayers.length === 0) return;
        const hits = map.queryRenderedFeatures(e.point, { layers: queryLayers }) as maplibregl.MapGeoJSONFeature[];
        const parsed = parseFeature(hits[0]);
        if (!parsed) return;
        popupRef.current?.remove();
        popupRef.current = new maplibregl.Popup({
          closeButton: true,
          closeOnClick: true,
          maxWidth: '320px',
          className: `transport-flows-popup ${theme === 'dark' ? 'transport-flows-popup-dark' : 'transport-flows-popup-light'}`,
        })
          .setLngLat(e.lngLat)
          .setHTML(renderPopupHtml(parsed.props, parsed.endpointSide))
          .addTo(map);
      } catch (err) {
        console.warn('[useTransportFlowLayer] popup render failed:', err);
      }
    };

    map.on('mousemove', handleMouseMove);
    map.on('mouseout', handleMouseOut);
    map.on('click', handleClick);

    return () => {
      popupRef.current?.remove();
      popupRef.current = null;
      hoverPopupRef.current?.remove();
      hoverPopupRef.current = null;
      clearHoverState(map);
      try {
        map.off('mousemove', handleMouseMove);
        map.off('mouseout', handleMouseOut);
        map.off('click', handleClick);
        if (map.getLayer(ENDPOINT_ICON_LAYER)) map.removeLayer(ENDPOINT_ICON_LAYER);
        if (map.getLayer(ENDPOINT_LAYER)) map.removeLayer(ENDPOINT_LAYER);
        if (map.getLayer(ENDPOINT_GLOW_LAYER)) map.removeLayer(ENDPOINT_GLOW_LAYER);
        if (map.getLayer(MAIN_LAYER)) map.removeLayer(MAIN_LAYER);
        if (map.getLayer(GLOW_LAYER)) map.removeLayer(GLOW_LAYER);
        if (map.getSource(ENDPOINT_SRC)) map.removeSource(ENDPOINT_SRC);
        if (map.getSource(SRC)) map.removeSource(SRC);
      } catch {
        // map disposed
      }
    };
  }, [map, isReady, styleEpoch]);

  useEffect(() => {
    if (!map || !isReady) return;
    if (!canAccessMapStyle(map)) return;
    const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
    const endpointSrc = map.getSource(ENDPOINT_SRC) as maplibregl.GeoJSONSource | undefined;
    if (src) src.setData(buildFlowCollection(features, theme, activityById || {}));
    if (endpointSrc) endpointSrc.setData(buildEndpointCollection(features, theme, activityById || {}));

    const visibility = visible ? 'visible' : 'none';
    if (map.getLayer(MAIN_LAYER)) map.setLayoutProperty(MAIN_LAYER, 'visibility', visibility);
    if (map.getLayer(GLOW_LAYER)) map.setLayoutProperty(GLOW_LAYER, 'visibility', visibility);
    if (map.getLayer(ENDPOINT_GLOW_LAYER)) map.setLayoutProperty(ENDPOINT_GLOW_LAYER, 'visibility', visibility);
    if (map.getLayer(ENDPOINT_LAYER)) map.setLayoutProperty(ENDPOINT_LAYER, 'visibility', visibility);
    if (map.getLayer(ENDPOINT_ICON_LAYER)) map.setLayoutProperty(ENDPOINT_ICON_LAYER, 'visibility', visibility);
    if (!visible) {
      clearHoverState(map);
      map.getCanvas().style.cursor = '';
      hoverPopupRef.current?.remove();
      hoverPopupRef.current = null;
      popupRef.current?.remove();
      popupRef.current = null;
    }
  }, [map, isReady, features, theme, visible, activityById]);
}
