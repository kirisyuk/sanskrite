import { useEffect, useMemo, useRef, useState } from 'react';
import maplibregl from 'maplibre-gl';
import type { GeoObject } from './useGeoObjects';
import type { MaterializedArtifact } from '../components/SupplyChainOverlay';
import { STATIC_TRANSPORT_FLOW_DATA, type TransportFlowFeature } from '../lib/staticTransportFlows';
import { buildPointFeature, ensureMarkerImages } from './useGeoMarkerLayer';
import { MAP_TEXT_FONT_STACK } from './useMapInstance';
import { geoCodeToShortName, humanizeCanonicalPath, humanizeSlug } from '../lib/uiUtils';
import { mapLayerColor, simulationStressColor } from '../lib/mapRenderPolicy';
import {
  artifactMatchesRouteVisibility,
  legMatchesRouteVisibility,
  type MapRouteVisibilitySelection,
} from '../lib/mapRouteVisibility';

const SRC = 'path-legs-src';
const ACTIVE_SRC = 'path-legs-active-src';
const GLOW_LAYER = 'path-legs-glow';
const PINNED_LAYER = 'path-legs-pinned';
const DISCOVERED_LAYER = 'path-legs-discovered';
const ACTIVE_GLOW_LAYER = 'path-legs-active-glow';
const ACTIVE_DISCOVERED_LAYER = 'path-legs-active-discovered';
const HIT_LAYER = 'path-legs-hit';
const HOVER_GLOW_LAYER = 'path-legs-hover-glow';
const HOVER_PINNED_LAYER = 'path-legs-hover-pinned';
const HOVER_DISCOVERED_LAYER = 'path-legs-hover-discovered';
const HOVER_BOTTLENECK_LAYER = 'path-legs-hover-bottlenecks';
const HOVER_OBJECT_ANCHOR_LAYER = 'path-legs-hover-object-anchors';
const HOVER_DIRECTION_LAYER = 'path-legs-hover-direction';
const HOVER_OBJECT_LAYER = 'path-legs-hover-objects';
const HOVER_OBJECT_LABEL_LAYER = 'path-legs-hover-object-labels';
const HOVER_MEANING_LABEL_LAYER = 'path-legs-hover-meaning-labels';
const BOTTLENECK_LAYER = 'path-legs-bottlenecks';
const LEG_OBJECT_ANCHOR_LAYER = 'path-legs-object-anchors';
const LEG_DIRECTION_LAYER = 'path-legs-direction';
const LEG_OBJECT_LAYER = 'path-legs-objects';
const LEG_OBJECT_LABEL_LAYER = 'path-legs-object-labels';
const LEG_MEANING_LABEL_LAYER = 'path-legs-meaning-labels';
const OBJECT_LABEL_CHIP_DARK = 'path-legs-object-label-chip-dark';
const OBJECT_LABEL_CHIP_LIGHT = 'path-legs-object-label-chip-light';
const MEANING_LABEL_CHIP_DARK = 'path-legs-meaning-label-chip-dark-v2';
const MEANING_LABEL_CHIP_LIGHT = 'path-legs-meaning-label-chip-light-v2';
const HOVER_ACTIVATE_DELAY_MS = 90;
const HOVER_CLEAR_DELAY_MS = 140;

type ThemeMode = 'dark' | 'light';

export interface PathLegLayerStats {
  artifactCount: number;
  legCount: number;
  boundLegCount: number;
  bottleneckCount: number;
}

export interface PathLegVisibilityPolicy {
  suppressedLegIds?: string[];
  revealedLegIds?: string[];
  rawVisibilityWeight?: number;
  visibleObjectIds?: string[];
  visibleObjectPaths?: string[];
  clusterAnchorByPath?: Record<string, [number, number]>;
  stressedLegIds?: string[];
  legCaptionById?: Record<string, string>;
}

function canAccessMapStyle(map: maplibregl.Map): boolean {
  try {
    return Boolean((map as unknown as { style?: unknown }).style);
  } catch {
    return false;
  }
}

function labelChipImageId(kind: 'object' | 'meaning', theme: ThemeMode): string {
  if (kind === 'object') {
    return theme === 'dark' ? OBJECT_LABEL_CHIP_DARK : OBJECT_LABEL_CHIP_LIGHT;
  }
  return theme === 'dark' ? MEANING_LABEL_CHIP_DARK : MEANING_LABEL_CHIP_LIGHT;
}

function drawRoundedRect(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  width: number,
  height: number,
  radius: number,
): void {
  const r = Math.max(0, Math.min(radius, width / 2, height / 2));
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.lineTo(x + width - r, y);
  ctx.quadraticCurveTo(x + width, y, x + width, y + r);
  ctx.lineTo(x + width, y + height - r);
  ctx.quadraticCurveTo(x + width, y + height, x + width - r, y + height);
  ctx.lineTo(x + r, y + height);
  ctx.quadraticCurveTo(x, y + height, x, y + height - r);
  ctx.lineTo(x, y + r);
  ctx.quadraticCurveTo(x, y, x + r, y);
  ctx.closePath();
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function artifactKeyForIds(ids: string[]): string {
  const normalized = Array.from(new Set(ids.map((value) => String(value || '').trim()).filter(Boolean))).sort();
  return normalized.length > 0 ? `|${normalized.join('|')}|` : '';
}

function artifactMatchExpression(artifactId: string): any {
  return ['>=', ['index-of', `|${artifactId}|`, ['coalesce', ['get', 'artifactKey'], '']], 0];
}

function glowOpacityExpression(dimmed = false): any {
  const base = ['case', ['boolean', ['get', 'isBottleneck'], false], ['*', ['get', 'glowOpacity'], 1.15], ['get', 'glowOpacity']];
  return dimmed
    ? ['case',
      ['==', ['get', 'stressFocusLeg'], 1],
      ['*', base, 1.9],
      ['==', ['get', 'pathActive'], 1],
      ['*', base, 1.15],
      ['*', base, 0.14],
    ]
    : base;
}

function pinnedLineOpacityExpression(dimmed = false): any {
  const base = ['get', 'lineOpacity'];
  return dimmed
    ? ['case',
      ['==', ['get', 'stressFocusLeg'], 1],
      ['*', base, 1.28],
      ['==', ['get', 'pathActive'], 1],
      ['*', base, 1.05],
      ['*', base, 0.18],
    ]
    : base;
}

function discoveredLineOpacityExpression(dimmed = false): any {
  const base = ['*', ['get', 'lineOpacity'], 0.82];
  return dimmed
    ? ['case',
      ['==', ['get', 'stressFocusLeg'], 1],
      ['*', base, 1.34],
      ['==', ['get', 'pathActive'], 1],
      ['*', base, 1.08],
      ['*', base, 0.18],
    ]
    : base;
}

function anchorOpacityExpression(dimmed = false): any {
  const base = ['*', ['coalesce', ['get', 'anchorOpacity'], 0.24], ['coalesce', ['get', 'visibilityWeight'], 1]];
  return dimmed
    ? ['case',
      ['==', ['get', 'pathActive'], 1],
      ['*', base, 0.95],
      ['*', base, 0.26],
    ]
    : base;
}

function bottleneckOpacityExpression(dimmed = false): any {
  const base = ['*', ['coalesce', ['get', 'circleOpacity'], 0.9], ['coalesce', ['get', 'visibilityWeight'], 1]];
  return dimmed ? ['*', base, 0.24] : base;
}

function directionOpacityExpression(dimmed = false): any {
  const base = [
    '*',
    ['case',
      ['==', ['get', 'isNarrativeActive'], true],
      0.88,
      0.7,
    ],
    ['coalesce', ['get', 'visibilityWeight'], 1],
  ];
  return dimmed
    ? ['case',
      ['==', ['get', 'stressFocusLeg'], 1],
      ['*', base, 1.05],
      ['==', ['get', 'pathActive'], 1],
      ['*', base, 0.82],
      ['*', base, 0.28],
    ]
    : base;
}

function objectIconOpacityExpression(dimmed = false): any {
  const base = [
    '*',
    ['case',
      ['==', ['get', 'pathActive'], 1],
      0.98,
      0.9,
    ],
    ['coalesce', ['get', 'visibilityWeight'], 1],
  ];
  return dimmed ? ['*', base, 0.34] : base;
}

function objectLabelOpacityExpression(dimmed = false): any {
  const base = [
    '*',
    ['case',
      ['==', ['get', 'pathActive'], 1],
      1,
      0.9,
    ],
    ['coalesce', ['get', 'visibilityWeight'], 1],
  ];
  return dimmed ? ['*', base, 0.3] : base;
}

function mapLegMeaningPromotedExpr(): any {
  return ['any', ['==', ['get', 'stressFocusLeg'], 1], ['==', ['get', 'meaningHighlight'], 1]];
}

function meaningLabelOpacityExpression(dimmed = false): any {
  const inner = [
    'case',
    ['!=', ['get', 'meaningVisible'], 1],
    0,
    mapLegMeaningPromotedExpr(),
    0.96,
    ['==', ['get', 'pathActive'], 1],
    0.9,
    ['==', ['get', 'focusMode'], 1],
    0.28,
    0.76,
  ];
  const base = ['*', inner, ['coalesce', ['get', 'visibilityWeight'], 1]];
  return dimmed
    ? ['case', mapLegMeaningPromotedExpr(), ['*', base, 0.88], ['*', base, 0.22]]
    : base;
}

function meaningChipOpacityExpression(dimmed = false): any {
  const inner = [
    'case',
    ['!=', ['get', 'meaningVisible'], 1],
    0,
    mapLegMeaningPromotedExpr(),
    0.94,
    ['==', ['get', 'pathActive'], 1],
    0.84,
    ['==', ['get', 'focusMode'], 1],
    0.2,
    0.68,
  ];
  const base = ['*', inner, ['coalesce', ['get', 'visibilityWeight'], 1]];
  return dimmed
    ? [
      'case',
      mapLegMeaningPromotedExpr(),
      ['*', base, 0.9],
      ['*', base, 0.22],
    ]
    : base;
}

function legFilter(isPinned?: boolean): any {
  const filter: any[] = ['all', ['==', ['geometry-type'], 'LineString'], ['==', ['get', 'kind'], 'leg']];
  if (typeof isPinned === 'boolean') {
    filter.push([isPinned ? '==' : '!=', ['get', 'isPinned'], true]);
  }
  return filter;
}

function pointKindFilter(kind: string): any {
  return ['all', ['==', ['geometry-type'], 'Point'], ['==', ['get', 'kind'], kind]];
}

function anchorFilter(): any {
  return ['all', ['==', ['geometry-type'], 'LineString'], ['==', ['get', 'kind'], 'leg-object-anchor']];
}

function hoverFilter(baseFilter: any, hoveredArtifactId: string | null): any {
  if (!hoveredArtifactId) return ['==', ['get', 'artifactKey'], '__none__'];
  return ['all', baseFilter, artifactMatchExpression(hoveredArtifactId)];
}

function artifactLegField<T = unknown>(leg: any, ...keys: string[]): T | undefined {
  for (const key of keys) {
    if (leg && leg[key] != null) return leg[key] as T;
  }
  return undefined;
}

function artifactLegString(leg: any, ...keys: string[]): string {
  return String(artifactLegField(leg, ...keys) || '').trim();
}

function artifactLegStringList(leg: any, ...keys: string[]): string[] {
  const value = artifactLegField<unknown>(leg, ...keys);
  if (!Array.isArray(value)) return [];
  return value.map((item) => String(item || '').trim()).filter(Boolean);
}

function artifactLegNumber(leg: any, ...keys: string[]): number | null {
  const value = artifactLegField<unknown>(leg, ...keys);
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim()) {
    const n = Number(value);
    if (Number.isFinite(n)) return n;
  }
  return null;
}

/** Align with WorldMapPanel simulationLegFocus: numeric index, string index, else array position. */
function resolvedArtifactLegIndex(leg: any, arrayIndex: number): number {
  const fromField = artifactLegNumber(leg, 'index', 'Index');
  if (fromField != null) return fromField;
  return arrayIndex;
}

function toFixedSafe(value: number | undefined, digits = 2): string {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(digits) : 'n/a';
}

function escapeHtml(input: string): string {
  return String(input || '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function titleizeUiToken(input: string): string {
  const value = humanizeSlug(input);
  if (!value) return '';
  return value
    .split(/\s+/g)
    .filter(Boolean)
    .map((word) => {
      if (!/[a-z]/i.test(word)) return word;
      if (word.slice(1) !== word.slice(1).toLowerCase()) return word;
      return word.charAt(0).toUpperCase() + word.slice(1);
    })
    .join(' ')
    .trim();
}

function capitalizeUiSentence(input: string): string {
  const value = String(input || '').trim();
  if (!value) return '';
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function joinHumanList(items: string[]): string {
  const clean = items.map((item) => String(item || '').trim()).filter(Boolean);
  if (clean.length === 0) return '';
  if (clean.length === 1) return clean[0];
  if (clean.length === 2) return `${clean[0]} and ${clean[1]}`;
  return `${clean.slice(0, -1).join(', ')}, and ${clean[clean.length - 1]}`;
}

function humanizeLegRef(value: string): string {
  const ref = String(value || '').trim();
  if (!ref) return '';
  if (ref.startsWith('/')) return humanizeCanonicalPath(ref) || ref;
  return titleizeUiToken(ref) || ref;
}

function humanizeDisplayChain(value: string): string {
  const raw = String(value || '').trim();
  if (!raw) return '';
  return raw
    .split(/\s*→\s*/g)
    .map((part) => titleizeUiToken(part) || part)
    .join(' → ');
}

function extractCountryCode(canonicalPath?: string): string | null {
  const path = String(canonicalPath || '').trim().toLowerCase();
  if (!path.startsWith('/')) return null;
  const parts = path.split('/').filter(Boolean);
  if (parts.length < 2) return null;
  const namespace = parts[0];
  if (!['org', 'gov', 'market', 'loc', 'fac', 'deposit', 'community', 'zone'].includes(namespace)) return null;
  let code = String(parts[1] || '').trim().toLowerCase();
  if (!code) return null;
  if (namespace === 'zone' && code.includes('-')) code = code.split('-')[0] || code;
  if (!/^[a-z]{2}$/.test(code) || code === 'na') return null;
  return code.toUpperCase();
}

function flagEmoji(countryCode?: string | null): string {
  const code = String(countryCode || '').trim().toUpperCase();
  if (!/^[A-Z]{2}$/.test(code)) return '';
  return String.fromCodePoint(...Array.from(code).map((char) => 127397 + char.charCodeAt(0)));
}

function renderPopupChip(options: {
  label: string;
  value: string;
  tone?: 'neutral' | 'info';
  flagCode?: string | null;
}): string {
  const label = String(options.label || '').trim();
  const value = String(options.value || '').trim();
  if (!value) return '';
  const tone = options.tone === 'info' ? 'info' : 'neutral';
  const border = tone === 'info' ? 'rgba(59,130,246,0.22)' : 'rgba(148,163,184,0.22)';
  const background = tone === 'info' ? 'rgba(59,130,246,0.10)' : 'rgba(255,255,255,0.04)';
  const emoji = flagEmoji(options.flagCode);
  const countryLabel = options.flagCode ? geoCodeToShortName(options.flagCode) : '';
  const title = [label, countryLabel, value].filter(Boolean).join(': ');
  return `
    <div title="${escapeHtml(title)}" style="display:inline-flex;align-items:center;gap:8px;min-width:0;max-width:100%;padding:6px 10px;border-radius:999px;border:1px solid ${border};background:${background};box-sizing:border-box;">
      <span style="font-size:10px;letter-spacing:0.06em;text-transform:uppercase;color:rgba(148,163,184,0.9);flex:0 0 auto;">${escapeHtml(label)}</span>
      ${emoji ? `<span aria-hidden="true" style="font-size:12px;line-height:1;flex:0 0 auto;">${emoji}</span>` : ''}
      <span style="font-size:11px;font-weight:700;line-height:1.2;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">${escapeHtml(value)}</span>
    </div>
  `;
}

function humanizeTraceRole(value: string): string {
  const raw = String(value || '').trim().toLowerCase();
  if (!raw) return '';
  switch (raw) {
    case 'pinned_context':
      return 'Pinned context';
    case 'focus_path':
      return 'Focused path';
    default:
      return titleizeUiToken(raw) || raw;
  }
}

function humanizeDiscoveryReason(value: string): string {
  const raw = String(value || '').trim().toLowerCase();
  if (!raw) return '';
  switch (raw) {
    case 'attached to lens scope':
      return 'Included in the current route analysis';
    case 'pinned context':
      return 'Kept visible as pinned context';
    default:
      return titleizeUiToken(raw) || raw;
  }
}

function humanizeGeometrySource(value: string): string {
  const raw = String(value || '').trim().toLowerCase();
  if (!raw) return '';
  switch (raw) {
    case 'fragment':
      return 'Approximate route';
    case 'flow':
      return 'Matched transport flow';
    case 'static_flow':
      return 'Matched mapped flow';
    default:
      return titleizeUiToken(raw) || raw;
  }
}

function humanizeFlowMode(value: string): string {
  const raw = String(value || '').trim().toLowerCase();
  if (!raw) return '';
  switch (raw) {
    case 'sea':
      return 'Sea';
    case 'ocean':
      return 'Ocean';
    case 'air':
      return 'Air';
    case 'rail':
      return 'Rail';
    case 'road':
      return 'Road';
    case 'pipeline':
      return 'Pipeline';
    case 'multimodal':
      return 'Multimodal';
    default:
      return titleizeUiToken(raw) || raw;
  }
}

function humanizeSpatialSemantic(value: string): string {
  const raw = String(value || '').trim().toLowerCase();
  if (!raw) return '';
  switch (raw) {
    case 'cross_border':
      return 'crosses a border';
    case 'zone_bound':
      return 'is bound to a mapped risk zone';
    case 'via_zone_bound':
      return 'passes through a mapped zone';
    case 'flow_bound':
      return 'is aligned to a mapped transport corridor';
    default:
      return humanizeSlug(raw).toLowerCase() || raw;
  }
}

function humanizeDominantFactor(value: string): string {
  const raw = String(value || '').trim().toLowerCase();
  if (!raw) return '';
  switch (raw) {
    case 'intermodal_mode':
      return 'requires a mode transfer';
    case 'affected_zone_pressure':
      return 'feels pressure from an affected zone';
    case 'zone_pressure':
      return 'depends on a stressed zone';
    case 'reroute_penalty':
      return 'has limited rerouting options';
    case 'fragility':
      return 'is operationally fragile';
    default:
      return humanizeSlug(raw).toLowerCase() || raw;
  }
}

function riskBandLabel(score: number): string {
  if (!Number.isFinite(score)) return 'Unknown';
  if (score >= 0.78) return 'Critical';
  if (score >= 0.58) return 'High';
  if (score >= 0.36) return 'Medium';
  return 'Low';
}

function formatRecoveryTime(hours: number): string {
  if (!Number.isFinite(hours) || hours < 0) return 'Unknown';
  if (hours < 1) return '<1h';
  if (hours < 24) return `${Math.round(hours)}h`;
  const days = hours / 24;
  return days >= 10 ? `${Math.round(days)}d` : `${days.toFixed(days >= 2 ? 0 : 1)}d`;
}

function buildLegSummary(args: {
  stressCaption: string;
  humanFlowMode: string;
  humanSpatial: string[];
  humanFactors: string[];
  hasAffectedZones: boolean;
  isBottleneck: boolean;
  linkLabel: string;
}): string {
  const {
    stressCaption,
    humanFlowMode,
    humanSpatial,
    humanFactors,
    hasAffectedZones,
    isBottleneck,
    linkLabel,
  } = args;
  if (stressCaption) {
    const normalized = stressCaption.replace(/\n+/g, ' ').trim();
    if (!normalized) return '';
    return /[.!?]$/.test(normalized) ? normalized : `${normalized}.`;
  }

  const clauses: string[] = [];
  if (humanFlowMode) clauses.push(`moves by ${humanFlowMode.toLowerCase()}`);
  clauses.push(...humanSpatial.slice(0, 2));
  clauses.push(...humanFactors.slice(0, 2));
  if (hasAffectedZones) clauses.push('touches an affected zone');
  if (isBottleneck) clauses.push('acts as a bottleneck');

  if (clauses.length === 0) {
    return linkLabel ? `This segment connects ${linkLabel}.` : 'This segment is part of the current route analysis.';
  }
  return `This segment ${joinHumanList(clauses)}.`;
}

function pathLegColor(stressScore: number, theme: ThemeMode): string {
  void stressScore;
  return mapLayerColor('supply', theme);
}

function stressFocusColor(theme: ThemeMode): string {
  return simulationStressColor(theme);
}

function legObjectLabel(obj: GeoObject): string {
  return String(obj.label || '').trim() || humanizeCanonicalPath(obj.canonicalPath) || obj.canonicalPath;
}

function legMeaningLabel(
  productRef: string,
  flowMode: string,
  flowRefs: string[],
  stressCaption?: string,
): string {
  if (stressCaption) return stressCaption;
  const productLabel = productRef ? (humanizeCanonicalPath(productRef) || productRef) : '';
  const modeLabel = flowMode || '';
  const vesselishRef = flowRefs.find((ref) => /vessel|ship|tanker/i.test(ref)) || '';
  const vesselLabel = vesselishRef ? humanizeCanonicalPath(vesselishRef) || vesselishRef : '';
  const parts = [productLabel, modeLabel, vesselLabel].filter(Boolean);
  if (parts.length === 0) return '';
  if (parts.length === 1) return parts[0];
  return `${parts[0]}\n${parts.slice(1).join(' • ')}`;
}

function pointAlongLine(
  coords: [number, number][],
  fraction: number,
): { point: [number, number]; bearing: number } | null {
  if (coords.length === 0) return null;
  if (coords.length === 1) return { point: coords[0], bearing: 0 };

  let total = 0;
  const segmentLengths: number[] = [];
  for (let index = 1; index < coords.length; index += 1) {
    const start = coords[index - 1];
    const end = coords[index];
    const dx = end[0] - start[0];
    const dy = end[1] - start[1];
    const length = Math.hypot(dx, dy);
    segmentLengths.push(length);
    total += length;
  }

  if (total <= 1e-9) {
    const center = coords[Math.floor(coords.length / 2)] || coords[0] || null;
    return center ? { point: center, bearing: 0 } : null;
  }

  const target = clamp(fraction, 0, 1) * total;
  let traversed = 0;
  for (let index = 1; index < coords.length; index += 1) {
    const start = coords[index - 1];
    const end = coords[index];
    const segmentLength = segmentLengths[index - 1] || 0;
    if (traversed + segmentLength < target) {
      traversed += segmentLength;
      continue;
    }
    const remaining = target - traversed;
    const ratio = segmentLength > 1e-9 ? remaining / segmentLength : 0;
    return {
      point: [
        start[0] + (end[0] - start[0]) * ratio,
        start[1] + (end[1] - start[1]) * ratio,
      ],
      bearing: Math.atan2(end[1] - start[1], end[0] - start[0]) * 180 / Math.PI,
    };
  }

  const last = coords[coords.length - 1] || null;
  const prev = coords[coords.length - 2] || last;
  return last ? {
    point: last,
    bearing: Math.atan2(last[1] - prev[1], last[0] - prev[0]) * 180 / Math.PI,
  } : null;
}

function midpoint(coords: [number, number][]): [number, number] | null {
  return pointAlongLine(coords, 0.5)?.point || null;
}

function dedupeCoords(coords: [number, number][]): [number, number][] {
  const out: [number, number][] = [];
  for (const [lon, lat] of coords) {
    const prev = out[out.length - 1];
    if (prev && Math.abs(prev[0] - lon) < 1e-7 && Math.abs(prev[1] - lat) < 1e-7) continue;
    out.push([lon, lat]);
  }
  return out;
}

type ScreenRect = {
  left: number;
  right: number;
  top: number;
  bottom: number;
};

function rectsOverlap(a: ScreenRect, b: ScreenRect, padding = 0): boolean {
  return !(
    a.right + padding < b.left
    || b.right + padding < a.left
    || a.bottom + padding < b.top
    || b.bottom + padding < a.top
  );
}

function lineLabelWidthPx(text: string, maxWidth: number): number {
  const longestLine = String(text || '')
    .split('\n')
    .reduce((max, line) => Math.max(max, line.trim().length), 0);
  return clamp(longestLine * 6.4, 34, maxWidth);
}

function legObjectMarkerRect(point: { x: number; y: number }): ScreenRect {
  const half = 14;
  return {
    left: point.x - half,
    right: point.x + half,
    top: point.y - half,
    bottom: point.y + half,
  };
}

function legObjectLabelRect(point: { x: number; y: number }, text: string): ScreenRect {
  const width = lineLabelWidthPx(text, 220);
  return {
    left: point.x + 20,
    right: point.x + 20 + width,
    top: point.y - 8,
    bottom: point.y + 8,
  };
}

function legMeaningLabelRect(point: { x: number; y: number }, text: string): ScreenRect {
  const lines = Math.max(1, String(text || '').split('\n').filter(Boolean).length);
  const width = lineLabelWidthPx(text, 180) + 20;
  const height = lines > 1 ? 34 : 22;
  return {
    left: point.x - width / 2,
    right: point.x + width / 2,
    top: point.y - height - 12,
    bottom: point.y - 12,
  };
}

function normalizeMeaningText(value: string): string {
  return String(value || '').trim().toLowerCase().replace(/\s+/g, ' ');
}

function stylePathLegPopup(popup: maplibregl.Popup, theme: ThemeMode): void {
  const root = popup.getElement();
  const content = root.querySelector('.maplibregl-popup-content') as HTMLElement | null;
  const tip = root.querySelector('.maplibregl-popup-tip') as HTMLElement | null;
  const background = theme === 'dark' ? 'rgba(5, 12, 24, 0.96)' : 'rgba(255,255,255,0.98)';
  const foreground = theme === 'dark' ? '#e2e8f0' : '#0f172a';
  const border = theme === 'dark' ? '1px solid rgba(148,163,184,0.24)' : '1px solid rgba(15,23,42,0.12)';

  if (content) {
    content.style.background = background;
    content.style.color = foreground;
    content.style.border = border;
    content.style.borderRadius = '16px';
    content.style.boxShadow = theme === 'dark'
      ? '0 20px 48px rgba(2,6,23,0.58)'
      : '0 20px 48px rgba(15,23,42,0.16)';
    content.style.padding = '14px 16px';
    content.style.maxWidth = '420px';
    content.style.width = 'min(420px, calc(100vw - 56px))';
    content.style.boxSizing = 'border-box';
    content.style.overflow = 'hidden';
  }
  root.style.maxWidth = '420px';
  if (tip) {
    tip.style.borderTopColor = background;
    tip.style.borderBottomColor = background;
    tip.style.borderLeftColor = background;
    tip.style.borderRightColor = background;
  }
}

function renderPathLegLabelChipImage(kind: 'object' | 'meaning', theme: ThemeMode): {
  width: number;
  height: number;
  data: Uint8ClampedArray;
  pixelRatio: number;
  stretchX: Array<[number, number]>;
  stretchY: Array<[number, number]>;
  content: [number, number, number, number];
} {
  const pixelRatio = 2;
  const width = kind === 'meaning' ? 280 : 112;
  const height = kind === 'meaning' ? 52 : 42;
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d')!;
  const fill = theme === 'dark'
    ? (kind === 'meaning' ? 'rgba(15, 12, 9, 0.82)' : 'rgba(7, 12, 18, 0.9)')
    : (kind === 'meaning' ? 'rgba(255, 249, 242, 0.94)' : 'rgba(255, 255, 255, 0.96)');
  const stroke = theme === 'dark'
    ? (kind === 'meaning' ? 'rgba(251, 146, 60, 0.24)' : 'rgba(96, 165, 250, 0.42)')
    : (kind === 'meaning' ? 'rgba(194, 65, 12, 0.16)' : 'rgba(37, 99, 235, 0.24)');
  const shadow = theme === 'dark'
    ? 'rgba(0, 0, 0, 0.22)'
    : (kind === 'meaning' ? 'rgba(120, 53, 15, 0.08)' : 'rgba(30, 64, 175, 0.12)');

  ctx.clearRect(0, 0, width, height);
  ctx.shadowColor = shadow;
  ctx.shadowBlur = 6;
  ctx.shadowOffsetY = 2;
  drawRoundedRect(ctx, 2, 4, width - 4, height - 8, 14);
  ctx.fillStyle = fill;
  ctx.fill();
  ctx.shadowColor = 'transparent';
  ctx.shadowBlur = 0;
  ctx.shadowOffsetY = 0;
  ctx.lineWidth = 1.2;
  ctx.strokeStyle = stroke;
  ctx.stroke();

  const imageData = ctx.getImageData(0, 0, width, height);
  return {
    width,
    height,
    data: imageData.data,
    pixelRatio,
    stretchX: [[24, width - 24]],
    stretchY: [[16, height - 16]],
    content: [20, 10, width - 20, height - 10],
  };
}

function ensurePathLegLabelChipImages(map: maplibregl.Map, theme: ThemeMode): void {
  for (const kind of ['object', 'meaning'] as const) {
    const id = labelChipImageId(kind, theme);
    if (map.hasImage(id)) continue;
    const image = renderPathLegLabelChipImage(kind, theme);
    map.addImage(id, image as any);
  }
}

function buildViewportPathLegFeatureCollection(
  featureCollection: GeoJSON.FeatureCollection,
  map: maplibregl.Map,
): GeoJSON.FeatureCollection {
  const features = (featureCollection.features || []).map((feature) => ({
    ...feature,
    geometry: feature.geometry ? JSON.parse(JSON.stringify(feature.geometry)) : feature.geometry,
    properties: { ...(feature.properties || {}) },
  })) as GeoJSON.Feature[];

  const zoom = map.getZoom();
  const anchorFeatures: GeoJSON.Feature[] = [];
  const legObjects = features
    .filter((feature) => feature.geometry?.type === 'Point' && String((feature.properties || {}).kind || '').trim() === 'leg-object')
    .filter((feature) => Number((feature.properties || {}).visibilityWeight ?? 1) > 0.01)
    .map((feature) => {
      const coordinates = (feature.geometry as GeoJSON.Point).coordinates as [number, number];
      const props = (feature.properties || {}) as Record<string, any>;
      return {
        feature,
        coordinates,
        projected: map.project({ lng: coordinates[0], lat: coordinates[1] }),
        labelPriority: Number(props.labelPriority || 0),
        pathActive: Number(props.pathActive || 0) === 1,
        objectRole: String(props.objectRole || '').trim(),
        labelText: String(props.labelText || '').trim(),
      };
    });

  const groups: Array<typeof legObjects> = [];
  const groupRadiusPx = zoom >= 8 ? 12 : zoom >= 6 ? 10 : 8;
  for (const item of legObjects) {
    let targetGroup: typeof legObjects | null = null;
    let bestDistanceSq = Number.POSITIVE_INFINITY;
    for (const group of groups) {
      const centroidX = group.reduce((sum, entry) => sum + entry.projected.x, 0) / group.length;
      const centroidY = group.reduce((sum, entry) => sum + entry.projected.y, 0) / group.length;
      const dx = item.projected.x - centroidX;
      const dy = item.projected.y - centroidY;
      const distSq = dx * dx + dy * dy;
      if (distSq <= groupRadiusPx * groupRadiusPx && distSq < bestDistanceSq) {
        bestDistanceSq = distSq;
        targetGroup = group;
      }
    }
    if (targetGroup) targetGroup.push(item);
    else groups.push([item]);
  }

  for (const group of groups) {
    if (group.length <= 1) continue;
    const centroidX = group.reduce((sum, entry) => sum + entry.projected.x, 0) / group.length;
    const centroidY = group.reduce((sum, entry) => sum + entry.projected.y, 0) / group.length;
    const sortedGroup = [...group].sort((left, right) => {
      if (left.pathActive !== right.pathActive) return Number(right.pathActive) - Number(left.pathActive);
      if (left.labelPriority !== right.labelPriority) return right.labelPriority - left.labelPriority;
      const roleRank = (role: string) => (role === 'supplier' || role === 'customer' ? 1 : 0);
      if (roleRank(left.objectRole) !== roleRank(right.objectRole)) return roleRank(right.objectRole) - roleRank(left.objectRole);
      return left.labelText.localeCompare(right.labelText);
    });
    const primaryCount = Math.min(sortedGroup.length, 6);
    const baseRadius = sortedGroup.length === 2 ? 16 : sortedGroup.length <= 4 ? 18 : 20;
    for (let index = 0; index < sortedGroup.length; index += 1) {
      const item = sortedGroup[index];
      const ring = Math.floor(index / 6);
      const ringSlots = ring === 0 ? primaryCount : Math.min(sortedGroup.length - ring * 6, 6);
      const slotIndex = ring === 0 ? index : index - ring * 6;
      const angle = (-Math.PI / 2) + ((Math.PI * 2) * slotIndex) / Math.max(ringSlots, 1);
      const radius = baseRadius + ring * 12;
      const offsetPoint = {
        x: centroidX + Math.cos(angle) * radius,
        y: centroidY + Math.sin(angle) * radius,
      };
      const offsetLngLat = map.unproject(offsetPoint as maplibregl.PointLike);
      item.feature.geometry = {
        type: 'Point',
        coordinates: [offsetLngLat.lng, offsetLngLat.lat],
      };
      const props = (item.feature.properties || {}) as Record<string, any>;
      props.spreadGroupSize = sortedGroup.length;
      props.spreadIndex = index;
      anchorFeatures.push({
        type: 'Feature',
        id: `${String(item.feature.id || props.objectId || props.canonicalPath || index)}:anchor`,
        geometry: {
          type: 'LineString',
          coordinates: [item.coordinates, [offsetLngLat.lng, offsetLngLat.lat]],
        },
        properties: {
          kind: 'leg-object-anchor',
          anchorColor: String(props.color || (props.pathActive ? '#cbd5e1' : '#94a3b8')),
          anchorOpacity: props.pathActive === 1 ? 0.34 : 0.2,
          anchorWidth: sortedGroup.length >= 4 ? 1.05 : 0.95,
          visibilityWeight: Number(props.visibilityWeight || 0),
          artifactKey: String(props.artifactKey || ''),
        },
      } as GeoJSON.Feature);
    }
  }

  const markerRects: ScreenRect[] = [];
  const objectLabelRects: ScreenRect[] = [];
  const meaningCandidates: Array<{
    feature: GeoJSON.Feature;
    rect: ScreenRect;
    cellKey: string;
    dedupeKey: string;
    active: boolean;
    stressFocus: boolean;
    stressScore: number;
  }> = [];

  for (const feature of features) {
    if (feature.geometry?.type !== 'Point') continue;
    const coordinates = (feature.geometry as GeoJSON.Point).coordinates as [number, number];
    const projected = map.project({ lng: coordinates[0], lat: coordinates[1] });
    const props = (feature.properties || {}) as Record<string, any>;
    const kind = String(props.kind || '').trim();
    if (kind === 'leg-object') {
      const labelText = String(props.labelText || '').trim();
      markerRects.push(legObjectMarkerRect(projected));
      if (labelText) objectLabelRects.push(legObjectLabelRect(projected, labelText));
      continue;
    }
    if (kind !== 'leg-meaning') continue;
    const meaningText = String(props.meaningText || '').trim();
    const rect = legMeaningLabelRect(projected, meaningText);
    const cellWidth = zoom >= 8 ? 150 : zoom >= 6 ? 180 : 240;
    const cellHeight = zoom >= 8 ? 110 : zoom >= 6 ? 130 : 170;
    const dedupeWidth = zoom >= 8 ? 180 : zoom >= 6 ? 220 : 320;
    const dedupeHeight = zoom >= 8 ? 140 : zoom >= 6 ? 170 : 240;
    const cellKey = `${Math.floor(projected.x / cellWidth)}:${Math.floor(projected.y / cellHeight)}`;
    const dedupeKey = `${normalizeMeaningText(meaningText)}:${Math.floor(projected.x / dedupeWidth)}:${Math.floor(projected.y / dedupeHeight)}`;
    const caption = String(props.stressCaption || '').trim();
    const stressFocusProp = Number(props.meaningHighlight || 0) === 1 || Boolean(caption);
    meaningCandidates.push({
      feature,
      rect,
      cellKey,
      dedupeKey,
      active: Number(props.pathActive || 0) === 1,
      stressFocus: stressFocusProp,
      stressScore: Number(props.stressScore || 0),
    });
  }

  meaningCandidates.sort((left, right) => {
    if (left.stressFocus !== right.stressFocus) return Number(right.stressFocus) - Number(left.stressFocus);
    if (left.active !== right.active) return Number(right.active) - Number(left.active);
    return right.stressScore - left.stressScore;
  });

  const acceptedRects: ScreenRect[] = [];
  const usedCells = new Map<string, number>();
  const usedMeaningKeys = new Set<string>();

  for (const candidate of meaningCandidates) {
    const props = (candidate.feature.properties || {}) as Record<string, any>;
    const suppressForObjectLabels = zoom >= 5.5;
    const suppressForMarkers = zoom >= 7.25;
    const objectLabelPadding = candidate.stressFocus ? 2 : zoom >= 8 ? 8 : zoom >= 6 ? 4 : 0;
    const markerPadding = candidate.stressFocus ? 0 : zoom >= 8 ? 8 : zoom >= 7 ? 3 : 0;
    const meaningPadding = candidate.stressFocus ? 2 : zoom >= 8 ? 10 : zoom >= 6 ? 6 : 2;
    const collidesWithObjectLabel = suppressForObjectLabels && !candidate.stressFocus
      ? objectLabelRects.some((rect) => rectsOverlap(rect, candidate.rect, objectLabelPadding))
      : false;
    const collidesWithMarker = suppressForMarkers && !candidate.stressFocus
      ? markerRects.some((rect) => rectsOverlap(rect, candidate.rect, markerPadding))
      : false;
    const collidesWithMeaning = acceptedRects.some((rect) => rectsOverlap(rect, candidate.rect, meaningPadding));
    const cellBudget = candidate.stressFocus ? 3 : zoom >= 8 ? 1 : zoom >= 6.5 ? 2 : zoom >= 5 ? 4 : 7;
    const cellCount = usedCells.get(candidate.cellKey) || 0;
    const duplicateMeaning = !candidate.stressFocus && usedMeaningKeys.has(candidate.dedupeKey);
    // Simulation / stress / context legs: always show mid-leg captions (otherwise factory labels + dedupe hide them).
    const visible = candidate.stressFocus
      ? true
      : !collidesWithObjectLabel && !collidesWithMarker && !collidesWithMeaning && !duplicateMeaning && cellCount < cellBudget;
    props.meaningVisible = visible ? 1 : 0;
    if (!visible) continue;
    acceptedRects.push(candidate.rect);
    usedCells.set(candidate.cellKey, cellCount + 1);
    usedMeaningKeys.add(candidate.dedupeKey);
  }

  return {
    type: 'FeatureCollection',
    features: [...features, ...anchorFeatures],
  };
}

function lineOptionsFromFeature(feature: TransportFlowFeature | undefined): [number, number][][] {
  const geometry = feature?.geometry;
  if (!geometry) return [];
  if (geometry.type === 'LineString') {
    return [dedupeCoords((geometry.coordinates || []) as [number, number][])];
  }
  if (geometry.type === 'MultiLineString') {
    return ((geometry.coordinates || []) as [number, number][][]).map((line) => dedupeCoords(line));
  }
  return [];
}

function distanceSq(a: [number, number], b: [number, number]): number {
  const dx = a[0] - b[0];
  const dy = a[1] - b[1];
  return dx * dx + dy * dy;
}

function orientLineForEndpoints(line: [number, number][], start: [number, number], end: [number, number]): [number, number][] {
  if (line.length < 2) return line;
  const first = line[0];
  const last = line[line.length - 1];
  const direct = distanceSq(start, first) + distanceSq(end, last);
  const reverse = distanceSq(start, last) + distanceSq(end, first);
  return reverse < direct ? [...line].reverse() : line;
}

function maybeConnectEndpoints(line: [number, number][], fallback: [number, number][]): [number, number][] {
  if (line.length < 2 || fallback.length < 2) return line;
  const start = fallback[0];
  const end = fallback[fallback.length - 1];
  const oriented = orientLineForEndpoints(line, start, end);
  const out = [...oriented];
  if (distanceSq(start, out[0]) > 1e-7) {
    out.unshift(start);
  }
  if (distanceSq(end, out[out.length - 1]) > 1e-7) {
    out.push(end);
  }
  return dedupeCoords(out);
}

function applyClusterAnchorsToCoords(
  coords: [number, number][],
  sourceRef: string,
  targetRef: string,
  clusterAnchorByPath: Record<string, [number, number]>,
): [number, number][] {
  if (coords.length < 2) return coords;
  const out = [...coords];
  const sourceAnchor = clusterAnchorByPath[sourceRef];
  const targetAnchor = clusterAnchorByPath[targetRef];
  if (sourceAnchor) out[0] = sourceAnchor;
  if (targetAnchor) out[out.length - 1] = targetAnchor;
  return dedupeCoords(out);
}

function selectFlowGeometry(
  flowRefs: string[],
  fallbackCoords: [number, number][],
  flowById: Map<string, TransportFlowFeature>,
): { coords: [number, number][]; flowId?: string; flowMode?: string; geometrySource: 'flow' | 'fragment'; snapped: boolean } {
  if (fallbackCoords.length < 2) {
    return { coords: fallbackCoords, geometrySource: 'fragment', snapped: false };
  }
  const start = fallbackCoords[0];
  const end = fallbackCoords[fallbackCoords.length - 1];
  let best: { coords: [number, number][]; score: number; flowId: string; flowMode?: string } | null = null;

  for (const ref of flowRefs) {
    const feature = flowById.get(ref);
    for (const option of lineOptionsFromFeature(feature)) {
      if (option.length < 2) continue;
      const oriented = orientLineForEndpoints(option, start, end);
      const score = distanceSq(start, oriented[0]) + distanceSq(end, oriented[oriented.length - 1]);
      if (!best || score < best.score) {
        best = {
          coords: maybeConnectEndpoints(oriented, fallbackCoords),
          score,
          flowId: ref,
          flowMode: feature?.properties?.flowMode ? String(feature.properties.flowMode) : undefined,
        };
      }
    }
  }

  if (!best) {
    return { coords: fallbackCoords, geometrySource: 'fragment', snapped: false };
  }
  return {
    coords: best.coords,
    flowId: best.flowId,
    flowMode: best.flowMode,
    geometrySource: 'flow',
    snapped: true,
  };
}

export function usePathLegLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  artifacts: MaterializedArtifact[],
  objects: GeoObject[],
  theme: ThemeMode,
  enabled: boolean,
  activeArtifactIds: string[] = [],
  visibilityPolicy?: PathLegVisibilityPolicy,
  routeVisibility?: MapRouteVisibilitySelection,
  simulationStressEnabled = false,
) {
  const [stats, setStats] = useState<PathLegLayerStats>({
    artifactCount: 0,
    legCount: 0,
    boundLegCount: 0,
    bottleneckCount: 0,
  });
  const [hoveredArtifactId, setHoveredArtifactId] = useState<string | null>(null);
  const popupRef = useRef<maplibregl.Popup | null>(null);
  const hoveredArtifactIdRef = useRef<string | null>(null);
  const hoverIntentTimerRef = useRef<number | null>(null);
  const hoverClearTimerRef = useRef<number | null>(null);

  const data = useMemo(() => {
  const activeArtifactSet = new Set((activeArtifactIds || []).map((value) => String(value || '').trim()).filter(Boolean));
  const hasActiveArtifacts = activeArtifactSet.size > 0;
  const suppressedLegIds = new Set((visibilityPolicy?.suppressedLegIds || []).map((value) => String(value || '').trim()).filter(Boolean));
  const revealedLegIds = new Set((visibilityPolicy?.revealedLegIds || []).map((value) => String(value || '').trim()).filter(Boolean));
    const stressedLegIds = new Set((visibilityPolicy?.stressedLegIds || []).map((value) => String(value || '').trim()).filter(Boolean));
    const legCaptionById = visibilityPolicy?.legCaptionById || {};
    const visibleObjectIds = new Set((visibilityPolicy?.visibleObjectIds || []).map((value) => String(value || '').trim()).filter(Boolean));
    const visibleObjectPaths = new Set((visibilityPolicy?.visibleObjectPaths || []).map((value) => String(value || '').trim()).filter(Boolean));
    const clusterAnchorByPath = visibilityPolicy?.clusterAnchorByPath || {};
    const rawVisibilityWeight = clamp(Number(visibilityPolicy?.rawVisibilityWeight ?? 1), 0, 1);
    const coordsByPath = new Map<string, { lon: number; lat: number }>();
    const objectByPath = new Map<string, GeoObject>();
    const flowById = new Map<string, TransportFlowFeature>();
    for (const obj of objects) {
      const cp = String(obj?.canonicalPath || '').trim();
      const lat = Number(obj?.coordinates?.lat);
      const lon = Number(obj?.coordinates?.lon);
      if (!cp || !Number.isFinite(lat) || !Number.isFinite(lon)) continue;
      coordsByPath.set(cp, { lon, lat });
      if (!objectByPath.has(cp)) objectByPath.set(cp, obj);
    }
    for (const feature of STATIC_TRANSPORT_FLOW_DATA.features || []) {
      const id = String(feature?.id || '').trim();
      if (id) flowById.set(id, feature);
    }

    let legCount = 0;
    let boundLegCount = 0;
    let bottleneckCount = 0;

    const features: GeoJSON.Feature[] = [];
    const legObjectFeatures = new Map<string, GeoJSON.Feature>();
    const legObjectEntries = new Map<string, GeoObject>();
    const legObjectVisibility = new Map<string, number>();
    const legObjectArtifactIds = new Map<string, Set<string>>();
    for (const artifact of artifacts || []) {
      if (!artifactMatchesRouteVisibility(artifact, routeVisibility)) continue;
      const artifactId = String(artifact.id || '').trim();
      const isNarrativeActive = !hasActiveArtifacts || activeArtifactSet.has(artifactId);
      const displayName = String(artifact.displayName || artifact.id || 'Supply path').trim();
      const legs = Array.isArray(artifact.payload?.legs) ? artifact.payload.legs : [];
      const bottleneckIndex = artifact.payload?.stress?.bottleneckLegIndex;
      for (let legArrayIndex = 0; legArrayIndex < legs.length; legArrayIndex += 1) {
        const leg = legs[legArrayIndex];
        if (!legMatchesRouteVisibility(artifact, leg, routeVisibility)) continue;
        const legIndex = resolvedArtifactLegIndex(leg, legArrayIndex);
        const legId = `${artifactId}:leg:${legIndex}`;
        const supplierRef = artifactLegString(leg, 'supplierRef', 'SupplierRef');
        const customerRef = artifactLegString(leg, 'customerRef', 'CustomerRef');
        const refs = [
          supplierRef,
          ...artifactLegStringList(leg, 'transportViaRefs', 'TransportViaRefs'),
          customerRef,
        ].filter(Boolean);
        const fallbackCoords = dedupeCoords(
          refs
            .map((ref) => coordsByPath.get(ref))
            .filter((point): point is { lon: number; lat: number } => Boolean(point))
            .map((point) => [point.lon, point.lat] as [number, number]),
        );
        const flowRefs = artifactLegStringList(leg, 'flowRefs', 'FlowRefs');
        const { coords, flowId, flowMode, geometrySource, snapped } = selectFlowGeometry(flowRefs, fallbackCoords, flowById);
        const anchoredCoords = applyClusterAnchorsToCoords(coords, supplierRef, customerRef, clusterAnchorByPath);
        if (anchoredCoords.length < 2) continue;
        legCount += 1;
        const explicitlyRevealed = revealedLegIds.has(legId);
        const suppressed = suppressedLegIds.has(legId) && !explicitlyRevealed;
        const localVisibilityWeight = suppressed
          ? 0
          : clamp(explicitlyRevealed ? Math.max(rawVisibilityWeight, 0.92) : rawVisibilityWeight, 0, 1);
        const visibleStressRefs = refs.filter((ref) => visibleObjectPaths.has(ref));
        const isStressContextLeg = visibleStressRefs.length >= 2
          || ((supplierRef && visibleObjectPaths.has(supplierRef)) && (customerRef && visibleObjectPaths.has(customerRef)));
        for (const [refIndex, ref] of refs.entries()) {
          const object = objectByPath.get(ref);
          if (!object?.coordinates) continue;
          const featureKey = object.id || ref;
          const role = refIndex === 0 ? 'supplier' : refIndex === refs.length - 1 ? 'customer' : 'via';
          const forcedEndpointVisible = hasActiveArtifacts && isNarrativeActive && (role === 'supplier' || role === 'customer');
          const nextPriority = forcedEndpointVisible ? 108 : role === 'supplier' || role === 'customer' ? 96 : 88;
          const objectNaturallyVisible = visibleObjectIds.has(object.id) || visibleObjectPaths.has(object.canonicalPath);
          const objectVisibilityWeight = objectNaturallyVisible || forcedEndpointVisible ? localVisibilityWeight : 0;
          const existing = legObjectFeatures.get(featureKey);
          if (!legObjectArtifactIds.has(featureKey)) legObjectArtifactIds.set(featureKey, new Set<string>());
          legObjectArtifactIds.get(featureKey)!.add(artifactId);
          if (!existing) {
            const pointFeature = buildPointFeature(object, theme, false) as GeoJSON.Feature<GeoJSON.Point>;
            pointFeature.id = `leg-object:${featureKey}`;
            pointFeature.properties = {
              ...(pointFeature.properties || {}),
              kind: 'leg-object',
              objectId: object.id,
              canonicalPath: object.canonicalPath,
              labelText: legObjectLabel(object),
              labelPriority: nextPriority,
              objectRole: role,
              pathActive: isNarrativeActive ? 1 : 0,
              visibilityWeight: objectVisibilityWeight,
              artifactKey: artifactKeyForIds([artifactId]),
            };
            legObjectFeatures.set(featureKey, pointFeature);
            legObjectEntries.set(featureKey, object);
            legObjectVisibility.set(featureKey, objectVisibilityWeight);
          } else {
            const props = (existing.properties || {}) as Record<string, any>;
            props.labelPriority = Math.max(Number(props.labelPriority || 0), nextPriority);
            props.pathActive = Number(props.pathActive || 0) || (isNarrativeActive ? 1 : 0);
            if (role === 'supplier' || role === 'customer') props.objectRole = role;
            props.visibilityWeight = Math.max(Number(props.visibilityWeight || 0), objectVisibilityWeight);
            legObjectVisibility.set(featureKey, Math.max(legObjectVisibility.get(featureKey) || 0, objectVisibilityWeight));
          }
        }
        const zoneRefs = artifactLegStringList(leg, 'zoneRefs', 'ZoneRefs');
        const affectedZoneRefs = artifactLegStringList(leg, 'affectedZoneRefs', 'AffectedZoneRefs');
        const spatialSemantics = artifactLegStringList(leg, 'spatialSemantics', 'SpatialSemantics');
        const stressPayload = artifactLegField<any>(leg, 'stress', 'Stress');
        const dominantFactors = Array.isArray(stressPayload?.dominantFactors || stressPayload?.DominantFactors)
          ? (stressPayload.dominantFactors || stressPayload.DominantFactors).map((item: unknown) => String(item || '').trim()).filter(Boolean)
          : [];
        const stressScore = clamp(Number(stressPayload?.stressScore ?? stressPayload?.StressScore) || 0, 0, 1);
        const bindingConfidence = clamp(Number(artifactLegField(leg, 'bindingConfidence', 'BindingConfidence')) || 0, 0, 1);
        const isBound = flowRefs.length > 0 || zoneRefs.length > 0 || affectedZoneRefs.length > 0 || spatialSemantics.length > 0;
        if (isBound) boundLegCount += 1;
        const isBottleneck = typeof bottleneckIndex === 'number' && bottleneckIndex === legIndex;
        if (isBottleneck) bottleneckCount += 1;
        const isStressFocusLeg = stressedLegIds.has(legId);
        const stressCaption = String(legCaptionById[legId] || '').trim();
        const emphasis = isNarrativeActive ? 1 : hasActiveArtifacts ? 0.08 : 0.24;
        const baseLineWidth = (1.35 + stressScore * 2.25 + (isBottleneck ? 0.7 : 0)) * (isStressFocusLeg ? 1.46 : isNarrativeActive ? 1 : hasActiveArtifacts ? 0.52 : 0.74);
        const baseLineOpacity = clamp((0.17 + bindingConfidence * 0.26 + stressScore * 0.18) * (isStressFocusLeg ? 1.75 : emphasis) * localVisibilityWeight, 0, isStressFocusLeg ? 0.96 : 0.62);
        const baseGlowOpacity = clamp((0.028 + stressScore * 0.08) * (isStressFocusLeg ? 1.9 : isNarrativeActive ? 1 : hasActiveArtifacts ? 0.08 : 0.2) * localVisibilityWeight, 0, isStressFocusLeg ? 0.22 : 0.11);

        features.push({
          type: 'Feature',
          id: legId,
          geometry: {
            type: 'LineString',
            coordinates: anchoredCoords,
          },
          properties: {
            kind: 'leg',
            artifactId,
            displayName,
            legIndex,
            role: String((leg as any)?.role || '').trim(),
            supplierRef,
            customerRef,
            productRef: artifactLegString(leg, 'productRef', 'ProductRef'),
            flowRefs,
            flowGeometryId: flowId || '',
            flowMode: flowMode || '',
            geometrySource,
            snappedToFlow: snapped,
            zoneRefs,
            affectedZoneRefs,
            spatialSemantics,
            dominantFactors,
            discoverySource: String(artifact.discoverySource || '').trim(),
            discoveryReason: String(artifact.discoveryReason || '').trim(),
            traceRole: String(artifact.traceRole || '').trim(),
            isPinned: Boolean(artifact.isPinned),
            stressScore,
            fragility: Number(stressPayload?.fragility ?? stressPayload?.Fragility) || 0,
            rerouteability: Number(stressPayload?.rerouteability ?? stressPayload?.Rerouteability) || 0,
            zonePressure: Number(stressPayload?.zonePressure ?? stressPayload?.ZonePressure) || 0,
            affectedZonePressure: Number(stressPayload?.affectedZonePressure ?? stressPayload?.AffectedZonePressure) || 0,
            recoveryLatencyHours: Number(stressPayload?.recoveryLatencyHours ?? stressPayload?.RecoveryLatencyHrs) || 0,
            bindingConfidence,
            isNarrativeActive,
            pathActive: isNarrativeActive ? 1 : 0,
            focusMode: hasActiveArtifacts ? 1 : 0,
            color: isStressFocusLeg ? stressFocusColor(theme) : pathLegColor(stressScore, theme),
            lineWidth: baseLineWidth,
            lineOpacity: baseLineOpacity,
            glowOpacity: baseGlowOpacity,
            isBottleneck,
            visibilityWeight: localVisibilityWeight,
            artifactKey: artifactKeyForIds([artifactId]),
            stressCaption,
            stressFocusLeg: isStressFocusLeg ? 1 : 0,
            stressContextLeg: isStressContextLeg ? 1 : 0,
          },
        } as GeoJSON.Feature);

        const meaningPoint = midpoint(anchoredCoords);
        let meaningLabel = legMeaningLabel(
          artifactLegString(leg, 'productRef', 'ProductRef'),
          String(flowMode || '').trim(),
          flowRefs,
          stressCaption,
        );
        if (!meaningLabel && (isStressFocusLeg || isStressContextLeg)) {
          const factorStr = dominantFactors.slice(0, 2).filter(Boolean).join(' · ');
          const stressStr = stressScore > 0 ? `stress ${stressScore.toFixed(2)}` : '';
          const parts = [String(stressCaption || '').trim(), factorStr, stressStr].filter(Boolean);
          meaningLabel = parts.length > 0 ? parts.join(' · ') : 'Simulation stress';
        }
        const meaningHighlight =
          (simulationStressEnabled && explicitlyRevealed)
          || isStressFocusLeg
          || isStressContextLeg
          || Boolean(String(stressCaption || '').trim())
          ? 1
          : 0;
        if (!meaningLabel && meaningHighlight) {
          const pr = artifactLegString(leg, 'productRef', 'ProductRef');
          if (pr) meaningLabel = humanizeCanonicalPath(pr) || pr;
          else if (stressScore > 0) meaningLabel = `stress ${stressScore.toFixed(2)}`;
          else meaningLabel = displayName.length > 0 ? displayName.slice(0, 48) : `Leg ${legIndex}`;
        }
        const meaningVisibilityWeight = Math.max(
          localVisibilityWeight,
          meaningHighlight ? 1 : 0,
        );
        if (meaningPoint && meaningLabel) {
          features.push({
            type: 'Feature',
            id: `${artifactId}:leg:${legIndex}:meaning`,
            geometry: {
              type: 'Point',
              coordinates: meaningPoint,
            },
            properties: {
              kind: 'leg-meaning',
              artifactId,
              legIndex,
              meaningText: meaningLabel,
              color: isStressFocusLeg ? stressFocusColor(theme) : pathLegColor(stressScore, theme),
              pathActive: isNarrativeActive || isStressFocusLeg || isStressContextLeg ? 1 : 0,
              focusMode: hasActiveArtifacts ? 1 : 0,
              stressScore,
              visibilityWeight: meaningVisibilityWeight,
              artifactKey: artifactKeyForIds([artifactId]),
              stressFocusLeg: isStressFocusLeg ? 1 : 0,
              stressContextLeg: isStressContextLeg ? 1 : 0,
              stressCaption,
              meaningHighlight,
            },
          } as GeoJSON.Feature);
        }

        if (isBottleneck) {
          const focusPoint = midpoint(anchoredCoords);
          if (focusPoint) {
            features.push({
              type: 'Feature',
              id: `${artifact.id}:leg:${legIndex}:bottleneck`,
              geometry: {
                type: 'Point',
                coordinates: focusPoint,
              },
              properties: {
                kind: 'bottleneck',
                artifactId,
                displayName,
                legIndex,
                stressScore,
                dominantFactors,
                bindingConfidence,
                isNarrativeActive,
                color: isStressFocusLeg ? stressFocusColor(theme) : pathLegColor(Math.max(stressScore, 0.6), theme),
                circleOpacity: (isNarrativeActive ? 0.74 : hasActiveArtifacts ? 0.08 : 0.22) * localVisibilityWeight,
                visibilityWeight: localVisibilityWeight,
                artifactKey: artifactKeyForIds([artifactId]),
              },
            } as GeoJSON.Feature);
          }
        }
      }
    }

    for (const [featureKey, feature] of legObjectFeatures.entries()) {
      const props = (feature.properties || {}) as Record<string, any>;
      props.visibilityWeight = Math.max(Number(props.visibilityWeight || 0), legObjectVisibility.get(featureKey) || 0);
      const artifactIds = Array.from(legObjectArtifactIds.get(featureKey) || []);
      props.artifactIds = artifactIds;
      props.artifactKey = artifactKeyForIds(artifactIds);
    }

    features.push(...Array.from(legObjectFeatures.values()));

    return {
      featureCollection: {
        type: 'FeatureCollection',
        features,
      } as GeoJSON.FeatureCollection,
      markerObjects: Array.from(legObjectEntries.values()),
      stats: {
        artifactCount: artifacts.length,
        legCount,
        boundLegCount,
        bottleneckCount,
      } satisfies PathLegLayerStats,
    };
  }, [activeArtifactIds, artifacts, objects, routeVisibility, simulationStressEnabled, theme, visibilityPolicy]);

  useEffect(() => {
    setStats(data.stats);
  }, [data]);

  useEffect(() => {
    hoveredArtifactIdRef.current = hoveredArtifactId;
  }, [hoveredArtifactId]);

  useEffect(() => {
    if (!map || !isReady || !canAccessMapStyle(map)) return;
    void ensureMarkerImages(map, data.markerObjects, theme);
    ensurePathLegLabelChipImages(map, theme);
    const viewportFeatureCollection = buildViewportPathLegFeatureCollection(data.featureCollection, map);
    const activeFeatureCollection: GeoJSON.FeatureCollection = {
      type: 'FeatureCollection',
      features: simulationStressEnabled
        ? (viewportFeatureCollection.features || []).filter((feature) => {
          const props = (feature.properties || {}) as Record<string, any>;
          return feature.geometry?.type === 'LineString'
            && String(props.kind || '').trim() === 'leg'
            && (Number(props.stressFocusLeg || 0) === 1 || Number(props.stressContextLeg || 0) === 1);
        })
        : [],
    };

    if (!map.getSource(SRC)) {
      map.addSource(SRC, {
        type: 'geojson',
        data: viewportFeatureCollection as any,
      });
    }
    if (!map.getSource(ACTIVE_SRC)) {
      map.addSource(ACTIVE_SRC, {
        type: 'geojson',
        data: activeFeatureCollection as any,
      });
    }

    if (!map.getLayer(GLOW_LAYER)) {
      map.addLayer({
        id: GLOW_LAYER,
        type: 'line',
        source: SRC,
        filter: legFilter(),
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['+', ['get', 'lineWidth'], 2.4],
          'line-opacity': glowOpacityExpression(),
          'line-blur': 0.95,
        },
      });
    }

    if (!map.getLayer(PINNED_LAYER)) {
      map.addLayer({
        id: PINNED_LAYER,
        type: 'line',
        source: SRC,
        filter: legFilter(true),
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
        },
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['get', 'lineWidth'],
          'line-opacity': pinnedLineOpacityExpression(),
          'line-dasharray': [
            'case',
            ['<', ['get', 'bindingConfidence'], 0.38],
            ['literal', [1.6, 1.6]],
            ['<', ['get', 'bindingConfidence'], 0.62],
            ['literal', [2.6, 1.4]],
            ['literal', [1, 0]],
          ],
        },
      });
    }

    if (!map.getLayer(DISCOVERED_LAYER)) {
      map.addLayer({
        id: DISCOVERED_LAYER,
        type: 'line',
        source: SRC,
        filter: legFilter(false),
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
        },
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['*', ['get', 'lineWidth'], 0.92],
          'line-opacity': discoveredLineOpacityExpression(),
          'line-dasharray': [
            'case',
            ['<', ['get', 'bindingConfidence'], 0.38],
            ['literal', [0.9, 2.6]],
            ['<', ['get', 'bindingConfidence'], 0.62],
            ['literal', [1.6, 2.2]],
            ['literal', [3.1, 1.8]],
          ],
        },
      });
    }

    if (!map.getLayer(ACTIVE_GLOW_LAYER)) {
      map.addLayer({
        id: ACTIVE_GLOW_LAYER,
        type: 'line',
        source: ACTIVE_SRC,
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
        },
        paint: {
          'line-color': [
            'case',
            ['==', ['get', 'stressFocusLeg'], 1],
            theme === 'dark' ? '#ffe1e1' : '#ffd7dc',
            theme === 'dark' ? '#ffd1d7' : '#ffc0c9',
          ],
          'line-width': ['+', ['get', 'lineWidth'], ['case', ['==', ['get', 'stressFocusLeg'], 1], 7.2, 5.8]],
          'line-opacity': ['case', ['==', ['get', 'stressFocusLeg'], 1], 0.98, 0.88],
          'line-blur': 1.9,
        },
      });
    }

    if (!map.getLayer(ACTIVE_DISCOVERED_LAYER)) {
      map.addLayer({
        id: ACTIVE_DISCOVERED_LAYER,
        type: 'line',
        source: ACTIVE_SRC,
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
        },
        paint: {
          'line-color': [
            'case',
            ['==', ['get', 'stressFocusLeg'], 1],
            theme === 'dark' ? '#ff9aa5' : '#df6a78',
            theme === 'dark' ? '#ffb4bd' : '#ef7d89',
          ],
          'line-width': ['+', ['get', 'lineWidth'], ['case', ['==', ['get', 'stressFocusLeg'], 1], 3.2, 2.4]],
          'line-opacity': ['case', ['==', ['get', 'stressFocusLeg'], 1], 1, 0.92],
        },
      });
    }

    if (!map.getLayer(HIT_LAYER)) {
      map.addLayer({
        id: HIT_LAYER,
        type: 'line',
        source: SRC,
        filter: legFilter(),
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
        },
        paint: {
          'line-color': '#000000',
          'line-width': ['+', ['get', 'lineWidth'], 14],
          'line-opacity': 0.001,
        },
      });
    }

    if (!map.getLayer(HOVER_GLOW_LAYER)) {
      map.addLayer({
        id: HOVER_GLOW_LAYER,
        type: 'line',
        source: SRC,
        filter: hoverFilter(legFilter(), null),
        layout: {
          visibility: 'none',
        },
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['+', ['*', ['get', 'lineWidth'], 1.3], 3.2],
          'line-opacity': ['case', ['boolean', ['get', 'isBottleneck'], false], ['*', ['get', 'glowOpacity'], 2.8], ['*', ['get', 'glowOpacity'], 2.05]],
          'line-blur': 1.08,
        },
      });
    }

    if (!map.getLayer(HOVER_PINNED_LAYER)) {
      map.addLayer({
        id: HOVER_PINNED_LAYER,
        type: 'line',
        source: SRC,
        filter: hoverFilter(legFilter(true), null),
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
          visibility: 'none',
        },
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['*', ['get', 'lineWidth'], 1.26],
          'line-opacity': ['*', ['get', 'lineOpacity'], 1.26],
          'line-dasharray': [
            'case',
            ['<', ['get', 'bindingConfidence'], 0.38],
            ['literal', [1.6, 1.6]],
            ['<', ['get', 'bindingConfidence'], 0.62],
            ['literal', [2.6, 1.4]],
            ['literal', [1, 0]],
          ],
        },
      });
    }

    if (!map.getLayer(HOVER_DISCOVERED_LAYER)) {
      map.addLayer({
        id: HOVER_DISCOVERED_LAYER,
        type: 'line',
        source: SRC,
        filter: hoverFilter(legFilter(false), null),
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
          visibility: 'none',
        },
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['*', ['get', 'lineWidth'], 1.18],
          'line-opacity': ['*', discoveredLineOpacityExpression(), 1.28],
          'line-dasharray': [
            'case',
            ['<', ['get', 'bindingConfidence'], 0.38],
            ['literal', [0.9, 2.6]],
            ['<', ['get', 'bindingConfidence'], 0.62],
            ['literal', [1.6, 2.2]],
            ['literal', [3.1, 1.8]],
          ],
        },
      });
    }

    if (!map.getLayer(LEG_OBJECT_ANCHOR_LAYER)) {
      map.addLayer({
        id: LEG_OBJECT_ANCHOR_LAYER,
        type: 'line',
        source: SRC,
        filter: anchorFilter(),
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
        },
        paint: {
          'line-color': ['coalesce', ['get', 'anchorColor'], theme === 'dark' ? '#94a3b8' : '#64748b'],
          'line-width': ['coalesce', ['get', 'anchorWidth'], 1],
          'line-opacity': anchorOpacityExpression(),
        },
      });
    }

    if (!map.getLayer(HOVER_OBJECT_ANCHOR_LAYER)) {
      map.addLayer({
        id: HOVER_OBJECT_ANCHOR_LAYER,
        type: 'line',
        source: SRC,
        filter: hoverFilter(anchorFilter(), null),
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
          visibility: 'none',
        },
        paint: {
          'line-color': ['coalesce', ['get', 'anchorColor'], theme === 'dark' ? '#cbd5e1' : '#334155'],
          'line-width': ['*', ['coalesce', ['get', 'anchorWidth'], 1], 1.16],
          'line-opacity': ['*', anchorOpacityExpression(), 1.15],
        },
      });
    }

    if (!map.getLayer(BOTTLENECK_LAYER)) {
      map.addLayer({
        id: BOTTLENECK_LAYER,
        type: 'circle',
        source: SRC,
        filter: pointKindFilter('bottleneck'),
        paint: {
          'circle-radius': ['+', 5, ['*', ['get', 'stressScore'], 7]],
          'circle-color': ['get', 'color'],
          'circle-stroke-color': theme === 'dark' ? '#f8fafc' : '#0f172a',
          'circle-stroke-width': 1.4,
          'circle-opacity': bottleneckOpacityExpression(),
        },
      });
    }

    if (!map.getLayer(HOVER_BOTTLENECK_LAYER)) {
      map.addLayer({
        id: HOVER_BOTTLENECK_LAYER,
        type: 'circle',
        source: SRC,
        filter: hoverFilter(pointKindFilter('bottleneck'), null),
        layout: {
          visibility: 'none',
        },
        paint: {
          'circle-radius': ['+', 6.5, ['*', ['get', 'stressScore'], 8]],
          'circle-color': ['get', 'color'],
          'circle-stroke-color': theme === 'dark' ? '#f8fafc' : '#0f172a',
          'circle-stroke-width': 1.7,
          'circle-opacity': ['*', bottleneckOpacityExpression(), 1.24],
        },
      });
    }

    if (!map.getLayer(LEG_DIRECTION_LAYER)) {
      map.addLayer({
        id: LEG_DIRECTION_LAYER,
        type: 'symbol',
        source: SRC,
        filter: legFilter(),
        layout: {
          'symbol-placement': 'line',
          'symbol-spacing': [
            'interpolate', ['linear'], ['zoom'],
            3, 170,
            5, 140,
            7, 116,
            9, 92,
          ],
          'text-field': '»',
          'text-font': [...MAP_TEXT_FONT_STACK],
          'text-size': [
            'interpolate', ['linear'], ['zoom'],
            3, 13,
            5, 15,
            7, 17,
            9, 19,
          ],
          'text-keep-upright': false,
          'text-allow-overlap': true,
          'text-ignore-placement': true,
        },
        paint: {
          'text-color': ['get', 'color'],
          'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.98)' : 'rgba(255,255,255,0.98)',
          'text-halo-width': 0.9,
          'text-opacity': directionOpacityExpression(),
        },
      });
    }

    if (!map.getLayer(HOVER_DIRECTION_LAYER)) {
      map.addLayer({
        id: HOVER_DIRECTION_LAYER,
        type: 'symbol',
        source: SRC,
        filter: hoverFilter(legFilter(), null),
        layout: {
          'symbol-placement': 'line',
          'symbol-spacing': [
            'interpolate', ['linear'], ['zoom'],
            3, 170,
            5, 140,
            7, 116,
            9, 92,
          ],
          'text-field': '»',
          'text-font': [...MAP_TEXT_FONT_STACK],
          'text-size': [
            'interpolate', ['linear'], ['zoom'],
            3, 13,
            5, 15,
            7, 17,
            9, 19,
          ],
          'text-keep-upright': false,
          'text-allow-overlap': true,
          'text-ignore-placement': true,
          visibility: 'none',
        },
        paint: {
          'text-color': ['get', 'color'],
          'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.98)' : 'rgba(255,255,255,0.98)',
          'text-halo-width': 1.05,
          'text-opacity': ['*', directionOpacityExpression(), 1.08],
        },
      });
    }

    if (!map.getLayer(LEG_OBJECT_LAYER)) {
      map.addLayer({
        id: LEG_OBJECT_LAYER,
        type: 'symbol',
        source: SRC,
        filter: pointKindFilter('leg-object'),
        layout: {
          'icon-image': ['get', 'iconKey'],
          'icon-size': 1,
          'icon-allow-overlap': true,
          'icon-ignore-placement': true,
        },
        paint: {
          'icon-opacity': objectIconOpacityExpression(),
        },
      });
    }

    if (!map.getLayer(HOVER_OBJECT_LAYER)) {
      map.addLayer({
        id: HOVER_OBJECT_LAYER,
        type: 'symbol',
        source: SRC,
        filter: hoverFilter(pointKindFilter('leg-object'), null),
        layout: {
          'icon-image': ['get', 'iconKey'],
          'icon-size': 1.08,
          'icon-allow-overlap': true,
          'icon-ignore-placement': true,
          visibility: 'none',
        },
        paint: {
          'icon-opacity': ['*', objectIconOpacityExpression(), 1.05],
        },
      });
    }

    if (!map.getLayer(LEG_OBJECT_LABEL_LAYER)) {
      map.addLayer({
        id: LEG_OBJECT_LABEL_LAYER,
        type: 'symbol',
        source: SRC,
        filter: pointKindFilter('leg-object'),
        layout: {
          'symbol-sort-key': ['get', 'labelPriority'],
          'text-field': ['get', 'labelText'],
          'text-font': [...MAP_TEXT_FONT_STACK],
          'text-size': [
            'interpolate', ['linear'], ['zoom'],
            4, 11,
            6, 11.5,
            8, 12,
            10, 12.5,
          ],
          'text-variable-anchor': ['literal', ['right', 'left', 'top-right', 'bottom-right', 'top', 'bottom']],
          'text-radial-offset': 1.35,
          'text-allow-overlap': true,
          'text-ignore-placement': true,
        },
        paint: {
          'text-color': theme === 'dark' ? '#f8fafc' : 'rgba(2,6,23,0.96)',
          'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.98)' : 'rgba(255,255,255,0.98)',
          'text-halo-width': 1.7,
          'text-opacity': objectLabelOpacityExpression(),
        },
      });
    }

    if (!map.getLayer(HOVER_OBJECT_LABEL_LAYER)) {
      map.addLayer({
        id: HOVER_OBJECT_LABEL_LAYER,
        type: 'symbol',
        source: SRC,
        filter: hoverFilter(pointKindFilter('leg-object'), null),
        layout: {
          'symbol-sort-key': ['get', 'labelPriority'],
          'text-field': ['get', 'labelText'],
          'text-font': [...MAP_TEXT_FONT_STACK],
          'text-size': [
            'interpolate', ['linear'], ['zoom'],
            4, 11,
            6, 11.5,
            8, 12,
            10, 12.5,
          ],
          'text-variable-anchor': ['literal', ['right', 'left', 'top-right', 'bottom-right', 'top', 'bottom']],
          'text-radial-offset': 1.35,
          'text-allow-overlap': true,
          'text-ignore-placement': true,
          visibility: 'none',
        },
        paint: {
          'text-color': theme === 'dark' ? '#f8fafc' : 'rgba(2,6,23,0.96)',
          'text-halo-color': theme === 'dark' ? 'rgba(2,6,23,0.98)' : 'rgba(255,255,255,0.98)',
          'text-halo-width': 1.85,
          'text-opacity': ['*', objectLabelOpacityExpression(), 1.06],
        },
      });
    }

    if (!map.getLayer(LEG_MEANING_LABEL_LAYER)) {
      map.addLayer({
        id: LEG_MEANING_LABEL_LAYER,
        type: 'symbol',
        source: SRC,
        filter: pointKindFilter('leg-meaning'),
        layout: {
          'icon-image': labelChipImageId('meaning', theme),
          'icon-anchor': 'center',
          'icon-text-fit': 'both',
          'icon-text-fit-padding': [4, 8, 4, 8],
          'icon-allow-overlap': true,
          'icon-ignore-placement': true,
          'text-field': ['get', 'meaningText'],
          'text-font': [...MAP_TEXT_FONT_STACK],
          'text-size': [
            'interpolate', ['linear'], ['zoom'],
            2, 11,
            3, 10.8,
            4, 10,
            6, 10.5,
            8, 11,
          ],
          'text-max-width': 22,
          'text-offset': [0, 0],
          'text-anchor': 'center',
          'text-allow-overlap': true,
          'text-ignore-placement': true,
        },
        paint: {
          'icon-opacity': meaningChipOpacityExpression(),
          'text-color': [
            'case',
            mapLegMeaningPromotedExpr(),
            theme === 'dark' ? '#fecaca' : '#b91c1c',
            ['==', ['get', 'pathActive'], 1],
            theme === 'dark' ? '#f2e7da' : 'rgba(124,45,18,0.86)',
            theme === 'dark' ? '#dccdbb' : 'rgba(120,53,15,0.74)',
          ],
          'text-halo-color': theme === 'dark' ? 'rgba(14,11,8,0.86)' : 'rgba(255,249,242,0.92)',
          'text-halo-width': [
            'case',
            mapLegMeaningPromotedExpr(),
            0.75,
            ['==', ['get', 'pathActive'], 1],
            0.64,
            0.5,
          ],
          'text-halo-blur': 0.2,
          'text-opacity': meaningLabelOpacityExpression(),
        },
      });
    }

    if (!map.getLayer(HOVER_MEANING_LABEL_LAYER)) {
      map.addLayer({
        id: HOVER_MEANING_LABEL_LAYER,
        type: 'symbol',
        source: SRC,
        filter: hoverFilter(pointKindFilter('leg-meaning'), null),
        layout: {
          'icon-image': labelChipImageId('meaning', theme),
          'icon-anchor': 'center',
          'icon-text-fit': 'both',
          'icon-text-fit-padding': [4, 8, 4, 8],
          'icon-allow-overlap': true,
          'icon-ignore-placement': true,
          'text-field': ['get', 'meaningText'],
          'text-font': [...MAP_TEXT_FONT_STACK],
          'text-size': [
            'interpolate', ['linear'], ['zoom'],
            2, 11,
            3, 10.8,
            4, 10,
            6, 10.5,
            8, 11,
          ],
          'text-max-width': 22,
          'text-offset': [0, 0],
          'text-anchor': 'center',
          'text-allow-overlap': true,
          'text-ignore-placement': true,
          visibility: 'none',
        },
        paint: {
          'icon-opacity': ['*', meaningChipOpacityExpression(), 1.1],
          'text-color': [
            'case',
            mapLegMeaningPromotedExpr(),
            theme === 'dark' ? '#fecaca' : '#b91c1c',
            ['==', ['get', 'pathActive'], 1],
            theme === 'dark' ? '#f2e7da' : 'rgba(124,45,18,0.86)',
            theme === 'dark' ? '#dccdbb' : 'rgba(120,53,15,0.74)',
          ],
          'text-halo-color': theme === 'dark' ? 'rgba(14,11,8,0.86)' : 'rgba(255,249,242,0.92)',
          'text-halo-width': [
            'case',
            mapLegMeaningPromotedExpr(),
            0.78,
            ['==', ['get', 'pathActive'], 1],
            0.68,
            0.56,
          ],
          'text-halo-blur': 0.2,
          'text-opacity': ['*', meaningLabelOpacityExpression(), 1.1],
        },
      });
    }

    const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
    src?.setData(viewportFeatureCollection as any);
    const activeSrc = map.getSource(ACTIVE_SRC) as maplibregl.GeoJSONSource | undefined;
    activeSrc?.setData(activeFeatureCollection as any);

    const visible = (enabled || activeArtifactIds.length > 0) && data.stats.legCount > 0;
    const activeFocusVisible = visible && activeFeatureCollection.features.length > 0;
    for (const layer of [GLOW_LAYER, PINNED_LAYER, DISCOVERED_LAYER, HIT_LAYER, LEG_OBJECT_ANCHOR_LAYER, BOTTLENECK_LAYER, LEG_DIRECTION_LAYER, LEG_OBJECT_LAYER, LEG_OBJECT_LABEL_LAYER, LEG_MEANING_LABEL_LAYER]) {
      if (map.getLayer(layer)) {
        map.setLayoutProperty(layer, 'visibility', visible ? 'visible' : 'none');
      }
    }
    for (const layer of [ACTIVE_GLOW_LAYER, ACTIVE_DISCOVERED_LAYER]) {
      if (map.getLayer(layer)) {
        map.setLayoutProperty(layer, 'visibility', activeFocusVisible ? 'visible' : 'none');
        map.moveLayer(layer);
      }
    }
  }, [map, isReady, styleEpoch, enabled, theme, data, activeArtifactIds, simulationStressEnabled]);

  useEffect(() => {
    if (!map || !isReady || !canAccessMapStyle(map)) return;
    const sceneVisible = (enabled || activeArtifactIds.length > 0) && data.stats.legCount > 0;
    const activeHoverId = sceneVisible ? String(hoveredArtifactId || '').trim() : '';
    const hasHover = Boolean(activeHoverId);

    const lineOpacityMode = hasHover || activeArtifactIds.length > 0;
    if (map.getLayer(GLOW_LAYER)) map.setPaintProperty(GLOW_LAYER, 'line-opacity', glowOpacityExpression(lineOpacityMode));
    if (map.getLayer(PINNED_LAYER)) map.setPaintProperty(PINNED_LAYER, 'line-opacity', pinnedLineOpacityExpression(lineOpacityMode));
    if (map.getLayer(DISCOVERED_LAYER)) map.setPaintProperty(DISCOVERED_LAYER, 'line-opacity', discoveredLineOpacityExpression(lineOpacityMode));
    if (map.getLayer(LEG_OBJECT_ANCHOR_LAYER)) map.setPaintProperty(LEG_OBJECT_ANCHOR_LAYER, 'line-opacity', anchorOpacityExpression(lineOpacityMode));
    if (map.getLayer(BOTTLENECK_LAYER)) map.setPaintProperty(BOTTLENECK_LAYER, 'circle-opacity', bottleneckOpacityExpression(lineOpacityMode));
    if (map.getLayer(LEG_DIRECTION_LAYER)) map.setPaintProperty(LEG_DIRECTION_LAYER, 'text-opacity', directionOpacityExpression(lineOpacityMode));
    if (map.getLayer(LEG_OBJECT_LAYER)) map.setPaintProperty(LEG_OBJECT_LAYER, 'icon-opacity', objectIconOpacityExpression(lineOpacityMode));
    if (map.getLayer(LEG_OBJECT_LABEL_LAYER)) map.setPaintProperty(LEG_OBJECT_LABEL_LAYER, 'text-opacity', objectLabelOpacityExpression(lineOpacityMode));
    if (map.getLayer(LEG_MEANING_LABEL_LAYER)) {
      map.setPaintProperty(LEG_MEANING_LABEL_LAYER, 'icon-opacity', meaningChipOpacityExpression(lineOpacityMode));
      map.setPaintProperty(LEG_MEANING_LABEL_LAYER, 'text-opacity', meaningLabelOpacityExpression(lineOpacityMode));
    }
    if (map.getLayer(ACTIVE_GLOW_LAYER)) {
      map.moveLayer(ACTIVE_GLOW_LAYER);
    }
    if (map.getLayer(ACTIVE_DISCOVERED_LAYER)) {
      map.moveLayer(ACTIVE_DISCOVERED_LAYER);
    }

    const hoverLayers: Array<{ id: string; filter: any }> = [
      { id: HOVER_GLOW_LAYER, filter: hoverFilter(legFilter(), activeHoverId || null) },
      { id: HOVER_PINNED_LAYER, filter: hoverFilter(legFilter(true), activeHoverId || null) },
      { id: HOVER_DISCOVERED_LAYER, filter: hoverFilter(legFilter(false), activeHoverId || null) },
      { id: HOVER_OBJECT_ANCHOR_LAYER, filter: hoverFilter(anchorFilter(), activeHoverId || null) },
      { id: HOVER_BOTTLENECK_LAYER, filter: hoverFilter(pointKindFilter('bottleneck'), activeHoverId || null) },
      { id: HOVER_DIRECTION_LAYER, filter: hoverFilter(legFilter(), activeHoverId || null) },
      { id: HOVER_OBJECT_LAYER, filter: hoverFilter(pointKindFilter('leg-object'), activeHoverId || null) },
      { id: HOVER_OBJECT_LABEL_LAYER, filter: hoverFilter(pointKindFilter('leg-object'), activeHoverId || null) },
      { id: HOVER_MEANING_LABEL_LAYER, filter: hoverFilter(pointKindFilter('leg-meaning'), activeHoverId || null) },
    ];

    for (const layer of hoverLayers) {
      if (!map.getLayer(layer.id)) continue;
      map.setFilter(layer.id, layer.filter);
      map.setLayoutProperty(layer.id, 'visibility', hasHover ? 'visible' : 'none');
    }
  }, [map, isReady, styleEpoch, enabled, activeArtifactIds, data.stats.legCount, hoveredArtifactId]);

  useEffect(() => {
    if (!map || !isReady || !canAccessMapStyle(map)) return;
    const refreshViewportLabels = () => {
      const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
      if (!src) return;
      src.setData(buildViewportPathLegFeatureCollection(data.featureCollection, map) as any);
    };
    refreshViewportLabels();
    map.on('zoomend', refreshViewportLabels);
    map.on('moveend', refreshViewportLabels);
    return () => {
      map.off('zoomend', refreshViewportLabels);
      map.off('moveend', refreshViewportLabels);
    };
  }, [map, isReady, styleEpoch, data.featureCollection]);

  useEffect(() => {
    if (!map || !isReady || !canAccessMapStyle(map)) return;
    const clearHoverIntentTimer = () => {
      if (hoverIntentTimerRef.current != null) {
        window.clearTimeout(hoverIntentTimerRef.current);
        hoverIntentTimerRef.current = null;
      }
    };
    const clearHoverClearTimer = () => {
      if (hoverClearTimerRef.current != null) {
        window.clearTimeout(hoverClearTimerRef.current);
        hoverClearTimerRef.current = null;
      }
    };
    const commitHover = (artifactId: string | null) => {
      setHoveredArtifactId((current) => (current === artifactId ? current : artifactId));
    };
    const scheduleHover = (artifactId: string | null) => {
      clearHoverClearTimer();
      if (!artifactId) {
        clearHoverIntentTimer();
        commitHover(null);
        return;
      }
      if (hoveredArtifactIdRef.current === artifactId) return;
      clearHoverIntentTimer();
      hoverIntentTimerRef.current = window.setTimeout(() => {
        commitHover(artifactId);
        hoverIntentTimerRef.current = null;
      }, HOVER_ACTIVATE_DELAY_MS);
    };
    const scheduleHoverClear = () => {
      clearHoverIntentTimer();
      clearHoverClearTimer();
      hoverClearTimerRef.current = window.setTimeout(() => {
        commitHover(null);
        hoverClearTimerRef.current = null;
      }, HOVER_CLEAR_DELAY_MS);
    };
    const onEnter = () => {
      clearHoverClearTimer();
      map.getCanvas().style.cursor = 'pointer';
    };
    const onLeave = () => {
      map.getCanvas().style.cursor = '';
      scheduleHoverClear();
      popupRef.current?.remove();
      popupRef.current = null;
    };
    const onHover = (event: maplibregl.MapLayerMouseEvent) => {
      const feature = event.features?.[0];
      const artifactId = String(feature?.properties?.artifactId || '').trim();
      scheduleHover(artifactId || null);
    };
    const onClick = (event: maplibregl.MapLayerMouseEvent) => {
      const feature = event.features?.[0];
      const props = (feature?.properties || {}) as Record<string, any>;
      if (!feature || props.kind !== 'leg') return;
      clearHoverIntentTimer();
      clearHoverClearTimer();
      commitHover(String(props.artifactId || '').trim() || null);
      popupRef.current?.remove();
      const parseList = (value: unknown): string[] => {
        if (Array.isArray(value)) return value.map((item) => String(item || '').trim()).filter(Boolean);
        if (typeof value !== 'string') return [];
        const trimmed = value.trim();
        if (!trimmed) return [];
        if (trimmed.startsWith('[')) {
          try {
            const parsed = JSON.parse(trimmed);
            return Array.isArray(parsed) ? parsed.map((item) => String(item || '').trim()).filter(Boolean) : [];
          } catch {
            return [];
          }
        }
        return trimmed.split(',').map((item) => item.trim()).filter(Boolean);
      };
      const dominantFactors = parseList(props.dominantFactors);
      const flowRefs = parseList(props.flowRefs);
      const zoneRefs = parseList(props.zoneRefs);
      const affectedZoneRefs = parseList(props.affectedZoneRefs);
      const spatialSemantics = parseList(props.spatialSemantics);
      const stressCaption = String(props.stressCaption || '').trim();
      const discoveryReason = String(props.discoveryReason || '').trim();
      const traceRole = String(props.traceRole || '').trim();
      const discoverySource = String(props.discoverySource || '').trim();
      const supplierRef = String(props.supplierRef || '').trim();
      const customerRef = String(props.customerRef || '').trim();
      const supplierLabel = supplierRef ? (humanizeCanonicalPath(supplierRef) || supplierRef) : '';
      const customerLabel = customerRef ? (humanizeCanonicalPath(customerRef) || customerRef) : '';
      const linkLabel = supplierLabel && customerLabel ? `${supplierLabel} → ${customerLabel}` : '';
      const productRef = String(props.productRef || '').trim();
      const productLabel = productRef ? (humanizeCanonicalPath(productRef) || productRef) : '';
      const supplierCountryCode = extractCountryCode(supplierRef);
      const customerCountryCode = extractCountryCode(customerRef);
      const humanFlowMode = humanizeFlowMode(String(props.flowMode || '').trim());
      const humanSpatial = Array.from(new Set(spatialSemantics.map(humanizeSpatialSemantic).filter(Boolean)));
      const humanFactors = Array.from(new Set(dominantFactors.map(humanizeDominantFactor).filter(Boolean)));
      const humanZones = Array.from(new Set([...zoneRefs, ...affectedZoneRefs].map(humanizeLegRef).filter(Boolean)));
      const humanFlows = Array.from(new Set(flowRefs.map(humanizeLegRef).filter(Boolean)));
      const humanDiscoveryReason = humanizeDiscoveryReason(discoveryReason);
      const humanTraceRole = humanizeTraceRole(traceRole);
      const humanGeometrySource = humanizeGeometrySource(String(props.geometrySource || '').trim());
      const displayTitle = humanizeDisplayChain(String(props.displayName || 'Supply path'));
      const headerChips = [
        renderPopupChip({ label: 'From', value: supplierLabel, flagCode: supplierCountryCode }),
        renderPopupChip({ label: 'Product', value: productLabel, tone: 'info' }),
        renderPopupChip({ label: 'To', value: customerLabel, flagCode: customerCountryCode }),
      ].filter(Boolean).join('');
      const summary = buildLegSummary({
        stressCaption,
        humanFlowMode,
        humanSpatial,
        humanFactors,
        hasAffectedZones: affectedZoneRefs.length > 0,
        isBottleneck: Boolean(props.isBottleneck),
        linkLabel,
      });
      const signalItems = Array.from(new Set([
        ...humanSpatial.map(capitalizeUiSentence),
        ...(humanFlowMode ? [`Moves by ${humanFlowMode}`] : []),
        ...humanFactors.map(capitalizeUiSentence),
        ...(props.isBottleneck ? ['Bottleneck segment'] : []),
      ])).slice(0, 4);
      const confidencePct = Number(props.bindingConfidence) * 100;
      const metricItems = [
        { label: 'Risk', value: riskBandLabel(Number(props.stressScore)) },
        { label: 'Confidence', value: `${toFixedSafe(confidencePct, 0)}%` },
        { label: 'Recovery', value: formatRecoveryTime(Number(props.recoveryLatencyHours)) },
      ];
      const legNumber = Number.isFinite(Number(props.legIndex)) ? String(Number(props.legIndex) + 1) : String(props.legIndex || '').trim();
      const bodyTextColor = theme === 'dark' ? 'rgba(255,255,255,0.94)' : 'rgba(15,23,42,0.9)';
      const mutedTextColor = theme === 'dark' ? 'rgba(255,255,255,0.7)' : 'rgba(51,65,85,0.74)';
      const chipBg = theme === 'dark' ? 'rgba(15,23,42,0.28)' : 'rgba(255,255,255,0.88)';
      const panelBg = theme === 'dark' ? 'rgba(15,23,42,0.18)' : 'rgba(248,250,252,0.88)';
      const panelBorder = theme === 'dark' ? 'rgba(148,163,184,0.16)' : 'rgba(148,163,184,0.22)';
      const contextRows: string[] = [];
      if (!headerChips && linkLabel) {
        contextRows.push(`<div style="font-size:11px;line-height:1.45;"><strong style="color:${mutedTextColor};">Route</strong><br><span style="display:block;word-break:break-word;">${escapeHtml(linkLabel)}</span></div>`);
      }
      if (signalItems.length > 0) {
        contextRows.push(`<div style="font-size:11px;line-height:1.45;"><strong style="color:${mutedTextColor};">Signals</strong><br>${escapeHtml(signalItems.join(' • '))}</div>`);
      }
      if (humanZones.length > 0) {
        contextRows.push(`<div style="font-size:11px;line-height:1.45;"><strong style="color:${mutedTextColor};">Zones</strong><br>${escapeHtml(humanZones.slice(0, 3).join(' • '))}</div>`);
      }
      const technicalRows: string[] = [];
      if (humanDiscoveryReason) {
        technicalRows.push(`<div style="font-size:11px;line-height:1.45;"><strong>Included because:</strong> ${escapeHtml(humanDiscoveryReason)}</div>`);
      } else if (discoverySource) {
        technicalRows.push(`<div style="font-size:11px;line-height:1.45;"><strong>Source:</strong> ${escapeHtml(titleizeUiToken(discoverySource) || discoverySource)}</div>`);
      }
      if (humanGeometrySource) {
        technicalRows.push(`<div style="font-size:11px;line-height:1.45;"><strong>Geometry:</strong> ${escapeHtml(humanGeometrySource)}${props.snappedToFlow ? ' • snapped to flow' : ''}</div>`);
      }
      if (humanTraceRole) {
        technicalRows.push(`<div style="font-size:11px;line-height:1.45;"><strong>Trace role:</strong> ${escapeHtml(humanTraceRole)}</div>`);
      }
      if (humanFlows.length > 0) {
        technicalRows.push(`<div style="font-size:11px;line-height:1.45;"><strong>Mapped flows:</strong> ${escapeHtml(humanFlows.slice(0, 3).join(' • '))}</div>`);
      }
      if (flowRefs.length > 0) {
        technicalRows.push(`<div style="font-size:11px;line-height:1.45;"><strong>Flow refs:</strong> ${escapeHtml(flowRefs.slice(0, 3).join(', '))}</div>`);
      }
      if (zoneRefs.length > 0) {
        technicalRows.push(`<div style="font-size:11px;line-height:1.45;"><strong>Zone refs:</strong> ${escapeHtml(zoneRefs.slice(0, 3).join(', '))}</div>`);
      }
      if (affectedZoneRefs.length > 0) {
        technicalRows.push(`<div style="font-size:11px;line-height:1.45;"><strong>Affected zone refs:</strong> ${escapeHtml(affectedZoneRefs.slice(0, 3).join(', '))}</div>`);
      }
      if (dominantFactors.length > 0) {
        technicalRows.push(`<div style="font-size:11px;line-height:1.45;"><strong>Factors:</strong> ${escapeHtml(dominantFactors.join(', '))}</div>`);
      }
      if (spatialSemantics.length > 0) {
        technicalRows.push(`<div style="font-size:11px;line-height:1.45;"><strong>Spatial semantics:</strong> ${escapeHtml(spatialSemantics.join(', '))}</div>`);
      }

      const html = `
        <div style="display:flex;flex-direction:column;gap:12px;width:100%;max-width:100%;box-sizing:border-box;word-break:normal;overflow-wrap:anywhere;">
          <div style="display:flex;flex-direction:column;gap:4px;">
            ${headerChips ? `<div style="display:flex;flex-wrap:wrap;gap:8px;max-width:100%;min-width:0;">${headerChips}</div>` : ''}
            ${!headerChips ? `<div style="font-size:13px;font-weight:800;line-height:1.4;word-break:break-word;">${escapeHtml(displayTitle || String(props.displayName || 'Supply path'))}</div>` : ''}
            <div style="font-size:11px;color:${mutedTextColor};">${props.isPinned ? 'Pinned route' : 'Discovered route'} • Leg ${escapeHtml(legNumber)}${props.isBottleneck ? ' • bottleneck' : ''}</div>
          </div>
          <div style="font-size:12px;line-height:1.55;color:${bodyTextColor};">${escapeHtml(summary)}</div>
          <div style="display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px;">
            ${metricItems.map((item) => `
              <div style="padding:8px 10px;border:1px solid ${panelBorder};border-radius:10px;background:${chipBg};min-width:0;">
                <div style="font-size:10px;letter-spacing:0.04em;text-transform:uppercase;color:${mutedTextColor};">${escapeHtml(item.label)}</div>
                <div style="font-size:12px;font-weight:700;line-height:1.35;">${escapeHtml(item.value)}</div>
              </div>
            `).join('')}
          </div>
          ${contextRows.length > 0 ? `<div style="display:flex;flex-direction:column;gap:8px;padding:9px 10px;border-radius:12px;background:${panelBg};border:1px solid ${panelBorder};">${contextRows.join('')}</div>` : ''}
          ${technicalRows.length > 0 ? `
            <details style="margin-top:2px;">
              <summary style="cursor:pointer;font-size:11px;font-weight:600;color:${mutedTextColor};user-select:none;">Technical details</summary>
              <div style="display:flex;flex-direction:column;gap:6px;margin-top:8px;padding-top:8px;border-top:1px solid ${panelBorder};">
                ${technicalRows.join('')}
                <div style="font-size:11px;line-height:1.45;"><strong>Raw stress:</strong> ${escapeHtml(toFixedSafe(Number(props.stressScore), 2))}</div>
                ${props.flowGeometryId ? `<div style="font-size:11px;line-height:1.45;"><strong>Flow ID:</strong> ${escapeHtml(String(props.flowGeometryId))}</div>` : ''}
              </div>
            </details>
          ` : ''}
        </div>
      `;
      popupRef.current = new maplibregl.Popup({
        closeButton: false,
        closeOnClick: false,
        offset: 12,
        maxWidth: '420px',
      })
        .setLngLat(event.lngLat)
        .setHTML(html)
        .addTo(map);
      stylePathLegPopup(popupRef.current, theme);
    };

    map.on('mouseenter', HIT_LAYER, onEnter);
    map.on('mousemove', HIT_LAYER, onHover);
    map.on('mouseleave', HIT_LAYER, onLeave);
    map.on('click', HIT_LAYER, onClick);
    return () => {
      clearHoverIntentTimer();
      clearHoverClearTimer();
      map.off('mouseenter', HIT_LAYER, onEnter);
      map.off('mousemove', HIT_LAYER, onHover);
      map.off('mouseleave', HIT_LAYER, onLeave);
      map.off('click', HIT_LAYER, onClick);
      popupRef.current?.remove();
      popupRef.current = null;
    };
  }, [map, isReady, styleEpoch, theme]);

  return stats;
}
