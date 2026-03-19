import { useEffect, useRef, useCallback, createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import maplibregl from 'maplibre-gl';
import type { GeoObject } from './useGeoObjects';
import {
  HorizonNodeIcon,
  getAffectedZoneMapAccent,
  getHorizonNodeVisualType,
  getNodeColors,
} from '../components/HorizonGraphNodeTypes';
import { MAP_TEXT_FONT_STACK } from './useMapInstance';
import { humanizeCanonicalPath } from '../lib/uiUtils';

const SRC = 'geo-markers';
const CLUSTER_GLOW = 'geo-cluster-glow';
const CLUSTERS = 'geo-clusters';
const CLUSTER_CORE = 'geo-cluster-core';
const CLUSTER_COUNT = 'geo-cluster-count';
const POINTS = 'geo-unclustered';
const HIGHLIGHT_SRC = 'geo-highlight';
const HIGHLIGHT_GLOW = 'geo-highlight-glow';
const HIGHLIGHT_RING = 'geo-highlight-ring';
const HIGHLIGHT_ICON = 'geo-highlight-icon';
const SPIDER_SRC = 'geo-spider';
const SPIDER_POINTS = 'geo-spider-points';
const SPIDER_LEGS = 'geo-spider-legs';

const CLUSTER_MAX_ZOOM = 5;
const CLUSTER_RADIUS = 40;
const MARKER_PX = 28;
const ICON_PX = 16;
const PIXEL_RATIO = 2;
const SAME_COORDINATE_PRECISION = 6;

const CLUSTER_RING_RADIUS: maplibregl.ExpressionSpecification = ['step', ['get', 'point_count'], 13, 10, 17, 50, 22, 200, 28];
const CLUSTER_GLOW_RADIUS: maplibregl.ExpressionSpecification = ['step', ['get', 'point_count'], 16, 10, 19, 50, 24, 200, 30];
const CLUSTER_CORE_RADIUS: maplibregl.ExpressionSpecification = ['step', ['get', 'point_count'], 2.25, 10, 2.75, 50, 3.25, 200, 4.25];
const CLUSTER_RING_STROKE_WIDTH: maplibregl.ExpressionSpecification = ['step', ['get', 'point_count'], 1.15, 10, 1.35, 50, 1.65, 200, 1.95];
const CLUSTER_TEXT_SIZE: maplibregl.ExpressionSpecification = ['step', ['get', 'point_count'], 10, 10, 11, 50, 12, 200, 13];
export const GEO_MARKER_LAYER_ANCHOR = CLUSTER_GLOW;
export const GEO_MARKER_SOURCE_ID = SRC;
export const GEO_MARKER_CLUSTER_LAYER_ID = CLUSTERS;
export const GEO_MARKER_POINT_LAYER_ID = POINTS;
export const GEO_MARKER_CLUSTER_MAX_ZOOM = CLUSTER_MAX_ZOOM;

// ---------------------------------------------------------------------------
// Icon key helpers
// ---------------------------------------------------------------------------

export function resolveVisualTypeAndColor(
  obj: GeoObject,
  theme: 'dark' | 'light',
): { vt: string; color: string } {
  const vt = getHorizonNodeVisualType(obj.type, obj.canonicalPath, obj.label, obj.facilityKind) || obj.type;
  const color =
    obj.type === 'AFFECTED_ZONE'
      ? getAffectedZoneMapAccent(obj.canonicalPath, theme)
      : getNodeColors(vt, theme).accent;
  return { vt, color };
}

export type GeoMarkerHighlightRole = 'selected' | 'gateway' | 'search';
export type GeoMarkerHighlight = { id: string; role: GeoMarkerHighlightRole };

export function iconKey(vt: string, color: string): string {
  return `marker-${vt}-${color.replace('#', '')}`;
}

function canAccessMapStyle(map: maplibregl.Map): boolean {
  try {
    return Boolean((map as unknown as { style?: unknown }).style);
  } catch {
    return false;
  }
}

function coordinateKey(lon: number, lat: number): string {
  return `${lon.toFixed(SAME_COORDINATE_PRECISION)}:${lat.toFixed(SAME_COORDINATE_PRECISION)}`;
}

// ---------------------------------------------------------------------------
// Canvas-based marker image rendering
// ---------------------------------------------------------------------------

function loadImageFromDataUri(svgString: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = reject;
    img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svgString);
  });
}

async function renderMarkerImage(
  vt: string,
  color: string,
  theme: 'dark' | 'light',
): Promise<{ width: number; height: number; data: Uint8ClampedArray }> {
  const s = MARKER_PX * PIXEL_RATIO;
  const iconS = ICON_PX * PIXEL_RATIO;
  const offset = (s - iconS) / 2;

  const canvas = document.createElement('canvas');
  canvas.width = s;
  canvas.height = s;
  const ctx = canvas.getContext('2d')!;

  const bg = theme === 'dark' ? 'rgba(2,6,23,0.86)' : 'rgba(255,255,255,0.92)';
  const border = theme === 'dark' ? 'rgba(226,232,240,0.35)' : 'rgba(15,23,42,0.25)';
  ctx.beginPath();
  ctx.arc(s / 2, s / 2, s / 2 - 2, 0, Math.PI * 2);
  ctx.fillStyle = bg;
  ctx.fill();
  ctx.strokeStyle = border;
  ctx.lineWidth = 2;
  ctx.stroke();

  try {
    const iconHtml = renderToStaticMarkup(
      createElement(HorizonNodeIcon, { type: vt, color, size: 24 }),
    );
    // Make standalone SVG: strip React CSS style, add xmlns + explicit dimensions.
    const svgStr = iconHtml
      .replace(/style="[^"]*"/, '')
      .replace('<svg ', `<svg xmlns="http://www.w3.org/2000/svg" width="${iconS}" height="${iconS}" `);

    const img = await loadImageFromDataUri(svgStr);
    ctx.drawImage(img, offset, offset, iconS, iconS);
  } catch {
    // Fallback: small accent-colored dot.
    ctx.beginPath();
    ctx.arc(s / 2, s / 2, 5, 0, Math.PI * 2);
    ctx.fillStyle = color;
    ctx.fill();
  }

  const imageData = ctx.getImageData(0, 0, s, s);
  return { width: s, height: s, data: imageData.data };
}

export async function ensureMarkerImages(
  map: maplibregl.Map,
  objects: GeoObject[],
  theme: 'dark' | 'light',
): Promise<void> {
  if (!canAccessMapStyle(map)) return;
  const needed = new Map<string, { vt: string; color: string }>();
  for (const obj of objects) {
    const { vt, color } = resolveVisualTypeAndColor(obj, theme);
    const key = iconKey(vt, color);
    if (canAccessMapStyle(map) && !map.hasImage(key) && !needed.has(key)) {
      needed.set(key, { vt, color });
    }
  }
  if (needed.size === 0) return;

  const results = await Promise.allSettled(
    Array.from(needed.entries()).map(async ([key, { vt, color }]) => {
      const imgData = await renderMarkerImage(vt, color, theme);
      if (canAccessMapStyle(map) && !map.hasImage(key)) {
        map.addImage(key, imgData, { pixelRatio: PIXEL_RATIO });
      }
    }),
  );

  for (const r of results) {
    if (r.status === 'rejected') {
      console.warn('[useGeoMarkerLayer] icon render failed:', r.reason);
    }
  }
}

// ---------------------------------------------------------------------------
// GeoJSON feature collection
// ---------------------------------------------------------------------------

function buildFeatureCollection(
  objects: GeoObject[],
  theme: 'dark' | 'light',
  mutedObjectIds?: ReadonlySet<string>,
  labelledObjectIds?: ReadonlySet<string>,
): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = objects.map((obj) => {
    const feature = buildPointFeature(obj, theme, mutedObjectIds?.has(obj.id));
    if (labelledObjectIds && labelledObjectIds.has(obj.id)) {
      (feature.properties as any).showLabel = 1;
    }
    return feature;
  });
  return { type: 'FeatureCollection', features };
}

function buildHighlightFeatureCollection(
  objects: GeoObject[],
  highlightedObjects: readonly GeoMarkerHighlight[] | null | undefined,
  theme: 'dark' | 'light',
): GeoJSON.FeatureCollection {
  const highlightById = new Map<string, GeoMarkerHighlightRole>();
  for (const item of highlightedObjects || []) {
    const id = String(item?.id || '').trim();
    if (!id) continue;
    if (item.role === 'selected' || !highlightById.has(id)) {
      highlightById.set(id, item.role);
    }
  }
  if (highlightById.size === 0) return { type: 'FeatureCollection', features: [] };
  const features = objects
    .filter((obj) => highlightById.has(obj.id))
    .map((highlighted) => {
      const feature = buildPointFeature(highlighted, theme);
      const role = highlightById.get(highlighted.id) || 'gateway';
      const isSelected = role === 'selected';
      const isSearch = role === 'search';
      const baseColor = String((feature.properties as any)?.color || '#60a5fa');
      const glowColor = isSelected
        ? (theme === 'dark' ? '#7dd3fc' : '#2563eb')
        : isSearch
          ? (theme === 'dark' ? '#fde68a' : '#d97706')
          : baseColor;
      return {
        ...feature,
        properties: {
          ...(feature.properties || {}),
          opacity: 1,
          highlightRole: role,
          glowColor,
          glowRadius: isSelected ? 30 : isSearch ? 24 : 18,
          glowOpacity: isSelected ? (theme === 'dark' ? 0.26 : 0.18) : isSearch ? 0.16 : 0.1,
          ringRadius: isSelected ? 22 : isSearch ? 18 : 15,
          ringFillColor: isSelected
            ? (theme === 'dark' ? 'rgba(8,20,32,0.22)' : 'rgba(255,255,255,0.55)')
            : theme === 'dark' ? 'rgba(2,6,23,0.02)' : 'rgba(255,255,255,0.01)',
          ringStrokeColor: isSelected
            ? (theme === 'dark' ? 'rgba(186,230,253,0.98)' : 'rgba(29,78,216,0.94)')
            : isSearch
              ? (theme === 'dark' ? 'rgba(253,224,71,0.94)' : 'rgba(180,83,9,0.9)')
              : baseColor,
          ringStrokeWidth: isSelected ? 3.2 : isSearch ? 2.4 : 1.6,
          ringStrokeOpacity: isSelected ? 0.98 : isSearch ? 0.9 : 0.7,
          iconSize: isSelected ? 1.1 : isSearch ? 1.05 : 1,
        },
      };
    });
  if (features.length === 0) return { type: 'FeatureCollection', features: [] };
  return {
    type: 'FeatureCollection',
    features,
  };
}

export function buildPointFeature(
  obj: GeoObject,
  theme: 'dark' | 'light',
  muted = false,
): GeoJSON.Feature<GeoJSON.Point> {
  const lat = obj.coordinates?.lat ?? 0;
  const lon = obj.coordinates?.lon ?? 0;
  const { vt, color } = resolveVisualTypeAndColor(obj, theme);
  return {
    type: 'Feature',
    geometry: { type: 'Point', coordinates: [lon, lat] },
    properties: {
      id: obj.id,
      canonicalPath: obj.canonicalPath,
      iconKey: iconKey(vt, color),
      color,
      opacity: muted ? 0.04 : obj.inScope === false ? 0.5 : 1.0,
      focusState: muted ? 0 : 1,
      type: obj.type,
      label: obj.label || humanizeCanonicalPath(obj.canonicalPath) || obj.canonicalPath,
    },
  };
}

function buildSpiderFeatureCollection(
  map: maplibregl.Map,
  origin: [number, number],
  pointFeatures: Array<GeoJSON.Feature<GeoJSON.Point>>,
): GeoJSON.FeatureCollection {
  const projected = map.project(origin as maplibregl.LngLatLike);
  const spiderFeatures: GeoJSON.Feature[] = [];
  const legFeatures: GeoJSON.Feature[] = [];
  const n = pointFeatures.length;

  for (let i = 0; i < n; i++) {
    const angle = (2 * Math.PI * i) / Math.max(n, 1);
    const radius = 30 + (n > 8 ? i * 4 : 0);
    const offsetPx = {
      x: projected.x + Math.cos(angle) * radius,
      y: projected.y + Math.sin(angle) * radius,
    };
    const offsetLngLat = map.unproject(offsetPx as maplibregl.PointLike);
    const leaf = pointFeatures[i];

    spiderFeatures.push({
      type: 'Feature',
      geometry: { type: 'Point', coordinates: [offsetLngLat.lng, offsetLngLat.lat] },
      properties: { ...(leaf.properties || {}), _spider: true },
    });
    legFeatures.push({
      type: 'Feature',
      geometry: {
        type: 'LineString',
        coordinates: [origin, [offsetLngLat.lng, offsetLngLat.lat]],
      },
      properties: {},
    });
  }

  return {
    type: 'FeatureCollection',
    features: [...legFeatures, ...spiderFeatures],
  };
}

function clonePointFeature(feature: GeoJSON.Feature<GeoJSON.Point>): GeoJSON.Feature<GeoJSON.Point> {
  const coordinates = (feature.geometry as GeoJSON.Point).coordinates as [number, number];
  return {
    type: 'Feature',
    geometry: { type: 'Point', coordinates: [...coordinates] as [number, number] },
    properties: { ...(feature.properties || {}) },
  };
}

export type GeoMarkerSelection =
  | { kind: 'clear' }
  | { kind: 'object'; object: GeoObject | null; anchor?: [number, number] }
  | {
      kind: 'stack';
      objects: GeoObject[];
      anchor: [number, number];
      canSpiderfy: boolean;
      spiderfy?: () => void;
      clearTransient?: () => void;
    };

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export interface UseGeoMarkerLayerOptions {
  /** Additional layer IDs that prevent map click from clearing selection (e.g. corridor badges, edges). */
  extraInteractiveLayers?: readonly string[];
  /** When true, cluster click zooms in without clearing selection (trace/corridor badges stay visible). */
  suppressClearOnClusterClick?: boolean;
  /** Object ids to visually mute while a corridor region is expanded. */
  mutedObjectIds?: readonly string[];
  /** Highlighted marker roles with different visual emphasis. */
  highlightedObjects?: readonly GeoMarkerHighlight[];
}

function applyClusterMuteState(
  map: maplibregl.Map,
  isDark: boolean,
  dimmed: boolean,
): void {
  const baseGlowOpacity: maplibregl.ExpressionSpecification = [
    'step', ['get', 'point_count'],
    0.08,
    10, 0.1,
    50, 0.13,
    200, 0.16,
  ];
  const focusRatio: maplibregl.ExpressionSpecification = [
    'case',
    ['>', ['coalesce', ['get', 'point_count'], 0], 0],
    ['/', ['coalesce', ['get', 'focusCount'], 0], ['get', 'point_count']],
    1,
  ];
  const glowOpacity: any = dimmed
    ? ['interpolate', ['linear'], focusRatio, 0, 0.03, 0.2, 0.05, 0.5, 0.08, 1, 0.16]
    : baseGlowOpacity;
  const clusterOpacity: any = dimmed
    ? ['interpolate', ['linear'], focusRatio, 0, 0.025, 0.2, 0.05, 0.5, 0.18, 1, 1]
    : 1;
  const clusterStrokeOpacity: any = dimmed
    ? ['interpolate', ['linear'], focusRatio, 0, 0.04, 0.2, 0.07, 0.5, 0.22, 1, 1]
    : 1;
  const coreOpacity: any = dimmed
    ? ['interpolate', ['linear'], focusRatio, 0, 0.03, 0.2, 0.06, 0.5, 0.16, 1, 1]
    : 1;
  const textOpacity: any = dimmed
    ? ['interpolate', ['linear'], focusRatio, 0, 0.04, 0.2, 0.1, 0.5, 0.24, 1, 1]
    : 1;

  try {
    if (map.getLayer(CLUSTER_GLOW)) {
      map.setPaintProperty(CLUSTER_GLOW, 'circle-opacity', glowOpacity);
    }
    if (map.getLayer(CLUSTERS)) {
      map.setPaintProperty(CLUSTERS, 'circle-opacity', clusterOpacity);
      map.setPaintProperty(CLUSTERS, 'circle-stroke-opacity', clusterStrokeOpacity);
    }
    if (map.getLayer(CLUSTER_CORE)) {
      map.setPaintProperty(CLUSTER_CORE, 'circle-opacity', coreOpacity);
    }
    if (map.getLayer(CLUSTER_COUNT)) {
      map.setPaintProperty(CLUSTER_COUNT, 'text-opacity', textOpacity);
    }
  } catch {
    /* style may be reloading */
  }
}

export function useGeoMarkerLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  objects: GeoObject[],
  theme: 'dark' | 'light',
  onSelect: (selection: GeoMarkerSelection) => void,
  options?: UseGeoMarkerLayerOptions,
) {
  const extraInteractiveLayersRef = useRef(options?.extraInteractiveLayers ?? []);
  extraInteractiveLayersRef.current = options?.extraInteractiveLayers ?? [];
  const suppressClearOnClusterClickRef = useRef(options?.suppressClearOnClusterClick ?? false);
  suppressClearOnClusterClickRef.current = options?.suppressClearOnClusterClick ?? false;
  const mutedObjectIdsRef = useRef(options?.mutedObjectIds ?? []);
  mutedObjectIdsRef.current = options?.mutedObjectIds ?? [];
  const highlightedObjectsRef = useRef(options?.highlightedObjects ?? []);
  highlightedObjectsRef.current = options?.highlightedObjects ?? [];
  const objectsRef = useRef(objects);
  objectsRef.current = objects;
  const spiderActiveRef = useRef(false);
  const suppressMapClickClearRef = useRef(false);

  const clearSpider = useCallback((m: maplibregl.Map) => {
    if (!spiderActiveRef.current) return;
    try {
      if (m.getLayer(SPIDER_POINTS)) m.removeLayer(SPIDER_POINTS);
      if (m.getLayer(SPIDER_LEGS)) m.removeLayer(SPIDER_LEGS);
      if (m.getSource(SPIDER_SRC)) m.removeSource(SPIDER_SRC);
    } catch { /* map may be disposed */ }
    spiderActiveRef.current = false;
  }, []);

  // ---- Effect 1: set up source, layers, handlers on each style reload ----
  useEffect(() => {
    if (!map || !isReady) return;

    const isDark = theme === 'dark';

    const renderSpider = (origin: [number, number], pointFeatures: Array<GeoJSON.Feature<GeoJSON.Point>>) => {
      if (pointFeatures.length === 0) return;
      clearSpider(map);
      const spiderFc = buildSpiderFeatureCollection(map, origin, pointFeatures);
      map.addSource(SPIDER_SRC, { type: 'geojson', data: spiderFc });
      map.addLayer({
        id: SPIDER_LEGS,
        type: 'line',
        source: SPIDER_SRC,
        filter: ['==', '$type', 'LineString'],
        paint: {
          'line-color': isDark ? 'rgba(148,163,184,0.5)' : 'rgba(100,116,139,0.4)',
          'line-width': 1,
          'line-dasharray': [2, 2],
        },
      });
      map.addLayer({
        id: SPIDER_POINTS,
        type: 'symbol',
        source: SPIDER_SRC,
        filter: ['==', '$type', 'Point'],
        layout: {
          'icon-image': ['get', 'iconKey'],
          'icon-size': 1,
          'icon-allow-overlap': true,
          'icon-ignore-placement': true,
        },
      });
      spiderActiveRef.current = true;
    };

    const resolveObjectsByIds = (ids: string[]): GeoObject[] => {
      const byId = new Map(objectsRef.current.map((obj) => [obj.id, obj] as const));
      return ids
        .map((id) => byId.get(id))
        .filter((obj): obj is GeoObject => Boolean(obj));
    };

    const sortObjectsForPresentation = (items: GeoObject[]): GeoObject[] => {
      const highlightedIds = new Set((highlightedObjectsRef.current || []).map((item) => item.id));
      return [...items].sort((a, b) => {
        const highlightedDelta = Number(highlightedIds.has(b.id)) - Number(highlightedIds.has(a.id));
        if (highlightedDelta !== 0) return highlightedDelta;
        const scopeDelta = Number(b.inScope !== false) - Number(a.inScope !== false);
        if (scopeDelta !== 0) return scopeDelta;
        return String(a.label || a.canonicalPath || a.id).localeCompare(String(b.label || b.canonicalPath || b.id));
      });
    };

    const resolveObjectsAtCoordinate = (coords: [number, number]): GeoObject[] => {
      const key = coordinateKey(coords[0], coords[1]);
      return sortObjectsForPresentation(
        objectsRef.current.filter((obj) => {
          const lat = obj.coordinates?.lat;
          const lon = obj.coordinates?.lon;
          return Number.isFinite(lat) && Number.isFinite(lon) && coordinateKey(lon!, lat!) === key;
        }),
      );
    };

    map.addSource(SRC, {
      type: 'geojson',
      data: { type: 'FeatureCollection', features: [] },
      cluster: true,
      clusterMaxZoom: CLUSTER_MAX_ZOOM,
      clusterRadius: CLUSTER_RADIUS,
      clusterProperties: {
        focusCount: ['+', ['coalesce', ['get', 'focusState'], 0]],
      },
    });
    map.addSource(HIGHLIGHT_SRC, {
      type: 'geojson',
      data: { type: 'FeatureCollection', features: [] },
    });

    map.addLayer({
      id: CLUSTER_GLOW,
      type: 'circle',
      source: SRC,
      filter: ['has', 'point_count'],
      paint: {
        'circle-color': isDark ? 'rgba(125, 170, 235, 0.18)' : 'rgba(59, 130, 246, 0.12)',
        'circle-radius': CLUSTER_GLOW_RADIUS,
        'circle-blur': 0.82,
        'circle-opacity': ['step', ['get', 'point_count'], 0.08, 10, 0.1, 50, 0.13, 200, 0.16],
      },
    });

    map.addLayer({
      id: CLUSTERS,
      type: 'circle',
      source: SRC,
      filter: ['has', 'point_count'],
      paint: {
        'circle-color': isDark ? 'rgba(147, 197, 253, 0.08)' : 'rgba(255, 255, 255, 0.04)',
        'circle-radius': CLUSTER_RING_RADIUS,
        'circle-stroke-width': CLUSTER_RING_STROKE_WIDTH,
        'circle-stroke-color': isDark ? 'rgba(168, 196, 228, 0.62)' : 'rgba(59, 130, 246, 0.52)',
      },
    });

    map.addLayer({
      id: CLUSTER_CORE,
      type: 'circle',
      source: SRC,
      filter: ['has', 'point_count'],
      paint: {
        'circle-color': isDark ? 'rgba(191, 219, 254, 0.72)' : 'rgba(37, 99, 235, 0.58)',
        'circle-radius': CLUSTER_CORE_RADIUS,
      },
    });

    map.addLayer({
      id: CLUSTER_COUNT,
      type: 'symbol',
      source: SRC,
      filter: ['has', 'point_count'],
      layout: {
        'text-field': '{point_count_abbreviated}',
        'text-font': [...MAP_TEXT_FONT_STACK],
        'text-size': CLUSTER_TEXT_SIZE,
        'text-offset': [0, 0.05],
      },
      paint: {
        'text-color': isDark ? 'rgba(226, 232, 240, 0.9)' : 'rgba(15, 23, 42, 0.78)',
        'text-halo-color': isDark ? 'rgba(2, 6, 23, 0.82)' : 'rgba(248, 250, 252, 0.92)',
        'text-halo-width': 0.75,
      },
    });

    map.addLayer({
      id: POINTS,
      type: 'symbol',
      source: SRC,
      filter: ['!', ['has', 'point_count']],
      layout: {
        'icon-image': ['get', 'iconKey'],
        'icon-size': 1,
        'icon-allow-overlap': true,
        'icon-ignore-placement': true,
        'text-field': ['case', ['==', ['coalesce', ['get', 'showLabel'], 0], 1], ['get', 'label'], ''],
        'text-font': [...MAP_TEXT_FONT_STACK],
        'text-size': ['interpolate', ['linear'], ['zoom'], 4, 11, 7, 11.5, 10, 12.5],
        'text-variable-anchor': ['literal', ['right', 'left', 'top-right', 'bottom-right', 'top', 'bottom']],
        'text-radial-offset': 1.3,
        'text-max-width': 10,
        'text-allow-overlap': true,
        'text-ignore-placement': true,
      },
      paint: {
        'icon-opacity': ['get', 'opacity'],
        'text-color': isDark ? '#f8fafc' : 'rgba(2,6,23,0.96)',
        'text-halo-color': isDark ? 'rgba(2,6,23,0.96)' : 'rgba(255,255,255,0.96)',
        'text-halo-width': 1.4,
        'text-opacity': ['case', ['==', ['coalesce', ['get', 'showLabel'], 0], 1], 1, 0],
      },
    });

    map.addLayer({
      id: HIGHLIGHT_GLOW,
      type: 'circle',
      source: HIGHLIGHT_SRC,
      paint: {
        'circle-radius': ['coalesce', ['get', 'glowRadius'], 24],
        'circle-color': ['coalesce', ['get', 'glowColor'], ['get', 'color']],
        'circle-opacity': ['coalesce', ['get', 'glowOpacity'], isDark ? 0.16 : 0.12],
        'circle-blur': 0.72,
      },
    });

    map.addLayer({
      id: HIGHLIGHT_RING,
      type: 'circle',
      source: HIGHLIGHT_SRC,
      paint: {
        'circle-radius': ['coalesce', ['get', 'ringRadius'], 18],
        'circle-color': ['coalesce', ['get', 'ringFillColor'], isDark ? 'rgba(2,6,23,0.02)' : 'rgba(255,255,255,0.01)'],
        'circle-stroke-color': ['coalesce', ['get', 'ringStrokeColor'], ['get', 'color']],
        'circle-stroke-width': ['coalesce', ['get', 'ringStrokeWidth'], 2.4],
        'circle-stroke-opacity': ['coalesce', ['get', 'ringStrokeOpacity'], 0.92],
      },
    });

    map.addLayer({
      id: HIGHLIGHT_ICON,
      type: 'symbol',
      source: HIGHLIGHT_SRC,
      layout: {
        'icon-image': ['get', 'iconKey'],
        'icon-size': ['coalesce', ['get', 'iconSize'], 1.04],
        'icon-allow-overlap': true,
        'icon-ignore-placement': true,
        'text-field': [
          'case',
          ['==', ['get', 'highlightRole'], 'selected'],
          ['get', 'label'],
          '',
        ],
        'text-font': [...MAP_TEXT_FONT_STACK],
        'text-size': 12.5,
        'text-variable-anchor': ['literal', ['right', 'left', 'top-right', 'bottom-right', 'top', 'bottom']],
        'text-radial-offset': 1.3,
        'text-max-width': 10,
        'text-allow-overlap': true,
        'text-ignore-placement': true,
      },
      paint: {
        'icon-opacity': 1,
        'text-color': isDark ? '#ffffff' : 'rgba(2,6,23,0.98)',
        'text-halo-color': isDark ? 'rgba(2,6,23,0.98)' : 'rgba(255,255,255,0.98)',
        'text-halo-width': 1.6,
        'text-opacity': [
          'case',
          ['==', ['get', 'highlightRole'], 'selected'],
          1,
          0,
        ],
      },
    });

    // ---- click handlers ----

    const handlePointClick = (e: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      suppressMapClickClearRef.current = true;
      const feat = e.features?.[0];
      if (!feat) return;
      const clickedCoords = (feat.geometry as GeoJSON.Point).coordinates as [number, number];
      const overlapObjects = resolveObjectsAtCoordinate(clickedCoords);
      const anchor: [number, number] = [e.lngLat.lng, e.lngLat.lat];

      if (overlapObjects.length > 1) {
        const spiderFeatures = overlapObjects.map((obj) => buildPointFeature(obj, theme));
        onSelect({
          kind: 'stack',
          objects: overlapObjects,
          anchor,
          canSpiderfy: true,
          spiderfy: () => renderSpider(clickedCoords, spiderFeatures),
          clearTransient: () => clearSpider(map),
        });
        return;
      }

      const id = feat.properties?.id;
      const match = objectsRef.current.find((o) => o.id === id) ?? overlapObjects[0] ?? null;
      onSelect({ kind: 'object', object: match ?? null, anchor });
    };

    const handleClusterClick = (e: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      suppressMapClickClearRef.current = true;
      const feat = e.features?.[0];
      if (!feat) return;
      const clusterId = feat.properties?.cluster_id;
      const pointCount = Number(feat.properties?.point_count);
      const src = map.getSource(SRC) as maplibregl.GeoJSONSource;
      if (!src || clusterId == null) return;

      Promise.all([
        src.getClusterExpansionZoom(clusterId),
        src.getClusterLeaves(clusterId, Number.isFinite(pointCount) ? Math.min(pointCount, 250) : 50, 0),
      ]).then(([zoom, leaves]) => {
        if (!leaves.length) return;
        const center = (feat.geometry as GeoJSON.Point).coordinates as [number, number];
        const uniqueCoords = new Set(
          leaves
            .filter((leaf) => leaf.geometry.type === 'Point')
            .map((leaf) => {
              const coords = (leaf.geometry as GeoJSON.Point).coordinates as [number, number];
              return coordinateKey(coords[0], coords[1]);
            }),
        );

        if (uniqueCoords.size === 1) {
          const origin = (leaves[0].geometry as GeoJSON.Point).coordinates as [number, number];
          const stackObjects = sortObjectsForPresentation(resolveObjectsByIds(
            Array.from(new Set(leaves.map((leaf) => String(leaf.properties?.id || '')).filter(Boolean))),
          ));
          const spiderFeatures = leaves
            .filter((leaf): leaf is GeoJSON.Feature<GeoJSON.Point> => leaf.geometry.type === 'Point')
            .map((leaf) => clonePointFeature(leaf));
          clearSpider(map);
          onSelect({
            kind: 'stack',
            objects: stackObjects,
            anchor: [e.lngLat.lng, e.lngLat.lat],
            canSpiderfy: spiderFeatures.length > 1,
            spiderfy: spiderFeatures.length > 1 ? () => renderSpider(origin, spiderFeatures) : undefined,
            clearTransient: () => clearSpider(map),
          });
          return;
        }

        if (zoom > CLUSTER_MAX_ZOOM) {
          const origin = (leaves[0].geometry as GeoJSON.Point).coordinates as [number, number];
          const spiderFeatures = leaves
            .filter((leaf): leaf is GeoJSON.Feature<GeoJSON.Point> => leaf.geometry.type === 'Point')
            .map((leaf) => clonePointFeature(leaf));
          renderSpider(origin, spiderFeatures);
        } else {
          clearSpider(map);
          if (!suppressClearOnClusterClickRef.current) {
            onSelect({ kind: 'clear' });
          }
          map.easeTo({ center, zoom });
        }
      });
    };

    const handleSpiderClick = (e: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      suppressMapClickClearRef.current = true;
      const feat = e.features?.[0];
      if (!feat) return;
      const id = feat.properties?.id;
      const match = objectsRef.current.find((o) => o.id === id);
      const anchor: [number, number] = [e.lngLat.lng, e.lngLat.lat];
      onSelect({ kind: 'object', object: match ?? null, anchor });
    };

    const handleMapClick = (e: maplibregl.MapMouseEvent) => {
      if ((map as unknown as { __suppressGeoMarkerClear?: boolean }).__suppressGeoMarkerClear) {
        (map as unknown as { __suppressGeoMarkerClear?: boolean }).__suppressGeoMarkerClear = false;
        return;
      }
      if (suppressMapClickClearRef.current) {
        suppressMapClickClearRef.current = false;
        return;
      }
      const baseLayers = [POINTS, CLUSTERS, SPIDER_POINTS, HIGHLIGHT_GLOW, HIGHLIGHT_RING, HIGHLIGHT_ICON] as const;
      const interactiveLayers = [...baseLayers, ...extraInteractiveLayersRef.current].filter((id) =>
        map.getLayer(id),
      );
      const interactiveFeatures =
        interactiveLayers.length > 0 ? map.queryRenderedFeatures(e.point, { layers: interactiveLayers }) : [];
      if (interactiveFeatures.length > 0) return;
      if (spiderActiveRef.current) clearSpider(map);
      onSelect({ kind: 'clear' });
    };

    const setPointerCursor = () => { map.getCanvas().style.cursor = 'pointer'; };
    const resetCursor = () => { map.getCanvas().style.cursor = ''; };

    map.on('click', POINTS, handlePointClick);
    map.on('click', HIGHLIGHT_GLOW, handlePointClick);
    map.on('click', HIGHLIGHT_RING, handlePointClick);
    map.on('click', HIGHLIGHT_ICON, handlePointClick);
    map.on('click', CLUSTERS, handleClusterClick);
    map.on('click', SPIDER_POINTS, handleSpiderClick);
    map.on('click', handleMapClick);
    map.on('mouseenter', POINTS, setPointerCursor);
    map.on('mouseleave', POINTS, resetCursor);
    map.on('mouseenter', HIGHLIGHT_GLOW, setPointerCursor);
    map.on('mouseleave', HIGHLIGHT_GLOW, resetCursor);
    map.on('mouseenter', HIGHLIGHT_RING, setPointerCursor);
    map.on('mouseleave', HIGHLIGHT_RING, resetCursor);
    map.on('mouseenter', HIGHLIGHT_ICON, setPointerCursor);
    map.on('mouseleave', HIGHLIGHT_ICON, resetCursor);
    map.on('mouseenter', CLUSTERS, setPointerCursor);
    map.on('mouseleave', CLUSTERS, resetCursor);
    map.on('mouseenter', SPIDER_POINTS, setPointerCursor);
    map.on('mouseleave', SPIDER_POINTS, resetCursor);

    return () => {
      clearSpider(map);
      map.off('click', POINTS, handlePointClick);
      map.off('click', HIGHLIGHT_GLOW, handlePointClick);
      map.off('click', HIGHLIGHT_RING, handlePointClick);
      map.off('click', HIGHLIGHT_ICON, handlePointClick);
      map.off('click', CLUSTERS, handleClusterClick);
      map.off('click', SPIDER_POINTS, handleSpiderClick);
      map.off('click', handleMapClick);
      map.off('mouseenter', POINTS, setPointerCursor);
      map.off('mouseleave', POINTS, resetCursor);
      map.off('mouseenter', HIGHLIGHT_GLOW, setPointerCursor);
      map.off('mouseleave', HIGHLIGHT_GLOW, resetCursor);
      map.off('mouseenter', HIGHLIGHT_RING, setPointerCursor);
      map.off('mouseleave', HIGHLIGHT_RING, resetCursor);
      map.off('mouseenter', HIGHLIGHT_ICON, setPointerCursor);
      map.off('mouseleave', HIGHLIGHT_ICON, resetCursor);
      map.off('mouseenter', CLUSTERS, setPointerCursor);
      map.off('mouseleave', CLUSTERS, resetCursor);
      map.off('mouseenter', SPIDER_POINTS, setPointerCursor);
      map.off('mouseleave', SPIDER_POINTS, resetCursor);
      try {
        if (map.getLayer(HIGHLIGHT_ICON)) map.removeLayer(HIGHLIGHT_ICON);
        if (map.getLayer(HIGHLIGHT_RING)) map.removeLayer(HIGHLIGHT_RING);
        if (map.getLayer(HIGHLIGHT_GLOW)) map.removeLayer(HIGHLIGHT_GLOW);
        if (map.getLayer(CLUSTER_COUNT)) map.removeLayer(CLUSTER_COUNT);
        if (map.getLayer(CLUSTER_CORE)) map.removeLayer(CLUSTER_CORE);
        if (map.getLayer(CLUSTERS)) map.removeLayer(CLUSTERS);
        if (map.getLayer(CLUSTER_GLOW)) map.removeLayer(CLUSTER_GLOW);
        if (map.getLayer(POINTS)) map.removeLayer(POINTS);
        if (map.getSource(HIGHLIGHT_SRC)) map.removeSource(HIGHLIGHT_SRC);
        if (map.getSource(SRC)) map.removeSource(SRC);
      } catch { /* disposed */ }
    };
  }, [styleEpoch, theme, clearSpider, onSelect]);

  // ---- Effect 2: load icon images & push data on object/theme changes ----
  useEffect(() => {
    if (!map || !isReady) return;
    let cancelled = false;
    const dimClusters = mutedObjectIdsRef.current.length > 0;

    (async () => {
      if (!canAccessMapStyle(map)) return;
      try {
        await ensureMarkerImages(map, objects, theme);
      } catch { /* map may have been disposed during style transition */ }
      if (cancelled) return;
      try {
        if (!canAccessMapStyle(map)) return;
        const labelledIds = new Set<string>();
        for (const item of highlightedObjectsRef.current || []) {
          const id = String(item?.id || '').trim();
          if (id && item.role === 'gateway') labelledIds.add(id);
        }
        const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
        if (src) src.setData(buildFeatureCollection(objects, theme, new Set(mutedObjectIdsRef.current), labelledIds));
        const highlightSrc = map.getSource(HIGHLIGHT_SRC) as maplibregl.GeoJSONSource | undefined;
        if (highlightSrc) {
          highlightSrc.setData(buildHighlightFeatureCollection(objects, highlightedObjectsRef.current, theme));
        }
        applyClusterMuteState(map, theme === 'dark', dimClusters);
      } catch { /* source might not exist during style transition */ }
    })();

    return () => { cancelled = true; };
  }, [objects, theme, styleEpoch, options?.highlightedObjects, options?.mutedObjectIds]);
}
