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
import {
  mapCaptionTextFont,
  mapMarkerRadialNodeCaptionLayout,
  mapNodeCaptionTextPaint,
} from '../lib/mapCaptionCanon';
import { spreadMarkerPresentation } from '../lib/mapMarkerSpread';
import { humanizeCanonicalPath } from '../lib/uiUtils';

const SRC = 'geo-markers';
const CLUSTER_GLOW = 'geo-cluster-glow';
const CLUSTERS = 'geo-clusters';
const CLUSTER_CORE = 'geo-cluster-core';
const CLUSTER_COUNT = 'geo-cluster-count';
const POINTS = 'geo-unclustered';
const HIGHLIGHT_SRC = 'geo-highlight';
const HIGHLIGHT_CONNECTORS = 'geo-highlight-connectors';
const HIGHLIGHT_GLOW = 'geo-highlight-glow';
const HIGHLIGHT_RING = 'geo-highlight-ring';
const HIGHLIGHT_ICON = 'geo-highlight-icon';
const HIGHLIGHT_LABEL_RIGHT = 'geo-highlight-label-right';
const HIGHLIGHT_LABEL_LEFT = 'geo-highlight-label-left';
const HIGHLIGHT_LABEL_TOP = 'geo-highlight-label-top';
const HIGHLIGHT_LABEL_BOTTOM = 'geo-highlight-label-bottom';
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
const HIGHLIGHT_LABEL_LAYERS = [
  HIGHLIGHT_LABEL_RIGHT,
  HIGHLIGHT_LABEL_LEFT,
  HIGHLIGHT_LABEL_TOP,
  HIGHLIGHT_LABEL_BOTTOM,
] as const;
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
  mapInteractionSpotlightObjectIds?: ReadonlySet<string> | null,
  hiddenObjectIds?: ReadonlySet<string>,
): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = objects.map((obj) => {
    const baseMuted = Boolean(mutedObjectIds?.has(obj.id));
    const spotlightOn = Boolean(mapInteractionSpotlightObjectIds && mapInteractionSpotlightObjectIds.size > 0);
    const inSpotlight = !spotlightOn || mapInteractionSpotlightObjectIds!.has(obj.id);
    const labelled = Boolean(labelledObjectIds?.has(obj.id));
    const spotlightPeerMute = spotlightOn && !inSpotlight && !baseMuted && !labelled;
    const feature = buildPointFeature(obj, theme, baseMuted || spotlightPeerMute);
    if (spotlightPeerMute) {
      (feature.properties as Record<string, unknown>).opacity = 0.007;
    }
    if (hiddenObjectIds && hiddenObjectIds.has(obj.id)) {
      (feature.properties as Record<string, unknown>).hideBaseMarker = 1;
    }
    if (labelledObjectIds && labelledObjectIds.has(obj.id)) {
      (feature.properties as Record<string, unknown>).showLabel = 1;
    }
    return feature;
  });
  return { type: 'FeatureCollection', features };
}

function buildHighlightFeatureCollection(
  map: maplibregl.Map,
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

  const rolePriority = (role: GeoMarkerHighlightRole): number => {
    if (role === 'selected') return 3;
    if (role === 'search') return 2;
    return 1;
  };

  const highlighted = objects
    .filter((obj) => highlightById.has(obj.id) && Number.isFinite(obj.coordinates?.lat) && Number.isFinite(obj.coordinates?.lon))
    .sort((left, right) => {
      const roleDelta = rolePriority(highlightById.get(right.id) || 'gateway') - rolePriority(highlightById.get(left.id) || 'gateway');
      if (roleDelta !== 0) return roleDelta;
      return String(left.label || left.canonicalPath || left.id).localeCompare(String(right.label || right.canonicalPath || right.id));
    });

  if (highlighted.length === 0) return { type: 'FeatureCollection', features: [] };

  const highlightedIdSet = new Set(highlighted.map((obj) => obj.id));
  const obstacles = objects
    .filter((obj) => !highlightedIdSet.has(obj.id) && Number.isFinite(obj.coordinates?.lat) && Number.isFinite(obj.coordinates?.lon))
    .map((obj) => {
      const projected = map.project({ lng: Number(obj.coordinates!.lon), lat: Number(obj.coordinates!.lat) });
      return { x: projected.x, y: projected.y };
    });

  const placements = spreadMarkerPresentation(
    highlighted.map((obj) => {
      const projected = map.project({ lng: Number(obj.coordinates!.lon), lat: Number(obj.coordinates!.lat) });
      const role = highlightById.get(obj.id) || 'gateway';
      return {
        id: obj.id,
        x: projected.x,
        y: projected.y,
        label: String(obj.label || humanizeCanonicalPath(obj.canonicalPath) || obj.canonicalPath),
        priority: rolePriority(role) * 1000,
      };
    }),
    obstacles,
    { zoom: map.getZoom() },
  );

  const placementById = new Map(placements.map((placement) => [placement.id, placement] as const));
  const features = highlighted.flatMap((highlighted) => {
      const feature = buildPointFeature(highlighted, theme);
      const role = highlightById.get(highlighted.id) || 'gateway';
      const isSelected = role === 'selected';
      const isSearch = role === 'search';
      const featureProps = (feature.properties || {}) as Record<string, unknown>;
      const baseColor = String(featureProps.color || '#60a5fa');
      const placement = placementById.get(highlighted.id);
      if (!placement) return [];
      const displayLngLat = map.unproject([placement.x, placement.y]);
      const glowColor = isSelected
        ? (theme === 'dark' ? '#7dd3fc' : '#2563eb')
        : isSearch
          ? (theme === 'dark' ? '#fde68a' : '#d97706')
          : baseColor;
      const pointFeature: GeoJSON.Feature = {
        ...feature,
        geometry: {
          type: 'Point',
          coordinates: [displayLngLat.lng, displayLngLat.lat],
        },
        properties: {
          ...(feature.properties || {}),
          opacity: 1,
          showLabel: 1,
          labelAnchor: placement.anchor,
          labelPriority: rolePriority(role) * 1000,
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
      if (!placement.displaced) return [pointFeature];
      const connectorFeature: GeoJSON.Feature<GeoJSON.LineString> = {
        type: 'Feature',
        geometry: {
          type: 'LineString',
          coordinates: [
            [Number(highlighted.coordinates!.lon), Number(highlighted.coordinates!.lat)],
            [displayLngLat.lng, displayLngLat.lat],
          ],
        },
        properties: {
          id: `${highlighted.id}:connector`,
          connectorColor: isSelected
            ? (theme === 'dark' ? 'rgba(125,211,252,0.6)' : 'rgba(37,99,235,0.48)')
            : isSearch
              ? (theme === 'dark' ? 'rgba(253,224,71,0.54)' : 'rgba(217,119,6,0.42)')
              : theme === 'dark' ? 'rgba(148,163,184,0.38)' : 'rgba(71,85,105,0.3)',
          connectorWidth: isSelected ? 1.35 : 1.05,
          connectorOpacity: isSelected ? 0.82 : isSearch ? 0.72 : 0.6,
        },
      };
      return [
        connectorFeature,
        pointFeature,
      ];
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
      opacity: muted ? 0.015 : obj.inScope === false ? 0.5 : 1.0,
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
    const radius = 44 + Math.sqrt(Math.max(n, 1)) * 10 + (n > 8 ? i * 5 : 0);
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
  /** While edge/marker hover focuses a subgraph, only these object ids stay at full opacity. */
  mapInteractionSpotlightObjectIds?: readonly string[] | null;
  /** Fired when the pointer enters/leaves an unclustered map object marker. */
  onMapObjectHover?: (objectId: string | null) => void;
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
  const glowOpacity: number | maplibregl.ExpressionSpecification = dimmed
    ? ['interpolate', ['linear'], focusRatio, 0, 0.03, 0.2, 0.05, 0.5, 0.08, 1, 0.16]
    : baseGlowOpacity;
  const clusterOpacity: number | maplibregl.ExpressionSpecification = dimmed
    ? ['interpolate', ['linear'], focusRatio, 0, 0.025, 0.2, 0.05, 0.5, 0.18, 1, 1]
    : 1;
  const clusterStrokeOpacity: number | maplibregl.ExpressionSpecification = dimmed
    ? ['interpolate', ['linear'], focusRatio, 0, 0.04, 0.2, 0.07, 0.5, 0.22, 1, 1]
    : 1;
  const coreOpacity: number | maplibregl.ExpressionSpecification = dimmed
    ? ['interpolate', ['linear'], focusRatio, 0, 0.03, 0.2, 0.06, 0.5, 0.16, 1, 1]
    : 1;
  const textOpacity: number | maplibregl.ExpressionSpecification = dimmed
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
  const mapInteractionSpotlightIdsRef = useRef<ReadonlySet<string> | null>(null);
  mapInteractionSpotlightIdsRef.current = options?.mapInteractionSpotlightObjectIds?.length
    ? new Set(options.mapInteractionSpotlightObjectIds.map((id) => String(id || '').trim()).filter(Boolean))
    : null;
  const onMapObjectHoverRef = useRef(options?.onMapObjectHover);
  onMapObjectHoverRef.current = options?.onMapObjectHover;
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

    const addHighlightLabelLayer = (
      id: string,
      physicalAnchor: 'right' | 'left' | 'top' | 'bottom',
      textAnchor: 'left' | 'right' | 'top' | 'bottom',
      textOffset: [number, number],
    ) => {
      map.addLayer({
        id,
        type: 'symbol',
        source: HIGHLIGHT_SRC,
        filter: [
          'all',
          ['==', '$type', 'Point'],
          ['==', ['coalesce', ['get', 'showLabel'], 0], 1],
          ['==', ['get', 'labelAnchor'], physicalAnchor],
        ],
        layout: {
          'symbol-sort-key': ['coalesce', ['get', 'labelPriority'], 0],
          'text-field': ['get', 'label'],
          'text-font': mapCaptionTextFont(),
          'text-size': 12.25,
          'text-max-width': 18,
          'text-anchor': textAnchor,
          'text-offset': textOffset,
          'text-allow-overlap': true,
          'text-ignore-placement': true,
        },
        paint: {
          ...mapNodeCaptionTextPaint(isDark ? 'dark' : 'light', { variant: 'selected', haloWidth: 1.65 }),
          'text-opacity': 1,
        },
      });
    };

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
        'text-font': mapCaptionTextFont(),
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
        ...mapMarkerRadialNodeCaptionLayout(),
      },
      paint: {
        'icon-opacity': [
          '*',
          ['get', 'opacity'],
          ['case', ['==', ['coalesce', ['get', 'hideBaseMarker'], 0], 1], 0, 1],
        ],
        ...mapNodeCaptionTextPaint(isDark ? 'dark' : 'light', { haloWidth: 1.4 }),
        'text-opacity': ['case', ['==', ['coalesce', ['get', 'showLabel'], 0], 1], 1, 0],
      },
    });

    map.addLayer({
      id: HIGHLIGHT_CONNECTORS,
      type: 'line',
      source: HIGHLIGHT_SRC,
      filter: ['==', '$type', 'LineString'],
      layout: {
        'line-cap': 'round',
        'line-join': 'round',
      },
      paint: {
        'line-color': ['coalesce', ['get', 'connectorColor'], isDark ? 'rgba(148,163,184,0.38)' : 'rgba(71,85,105,0.3)'],
        'line-width': ['coalesce', ['get', 'connectorWidth'], 1.05],
        'line-opacity': ['coalesce', ['get', 'connectorOpacity'], 0.62],
        'line-dasharray': [0.8, 1.3],
      },
    });

    map.addLayer({
      id: HIGHLIGHT_GLOW,
      type: 'circle',
      source: HIGHLIGHT_SRC,
      filter: ['==', '$type', 'Point'],
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
      filter: ['==', '$type', 'Point'],
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
      filter: ['==', '$type', 'Point'],
      layout: {
        'icon-image': ['get', 'iconKey'],
        'icon-size': ['coalesce', ['get', 'iconSize'], 1.04],
        'icon-allow-overlap': true,
        'icon-ignore-placement': true,
      },
      paint: {
        'icon-opacity': 1,
      },
    });

    addHighlightLabelLayer(HIGHLIGHT_LABEL_RIGHT, 'right', 'left', [1.02, 0]);
    addHighlightLabelLayer(HIGHLIGHT_LABEL_LEFT, 'left', 'right', [-1.02, 0]);
    addHighlightLabelLayer(HIGHLIGHT_LABEL_TOP, 'top', 'bottom', [0, -1.08]);
    addHighlightLabelLayer(HIGHLIGHT_LABEL_BOTTOM, 'bottom', 'top', [0, 1.08]);

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
      const baseLayers = [POINTS, CLUSTERS, SPIDER_POINTS, HIGHLIGHT_GLOW, HIGHLIGHT_RING, HIGHLIGHT_ICON, ...HIGHLIGHT_LABEL_LAYERS] as const;
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

    const handlePointHoverEnter = (e: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      const feat = e.features?.[0];
      const id = String(feat?.properties?.id || '').trim();
      if (!id) return;
      onMapObjectHoverRef.current?.(id);
    };
    const handlePointHoverLeave = () => {
      onMapObjectHoverRef.current?.(null);
    };
    const pointClickLayers = [POINTS, HIGHLIGHT_GLOW, HIGHLIGHT_RING, HIGHLIGHT_ICON, ...HIGHLIGHT_LABEL_LAYERS] as const;
    const hoverableLayers = [POINTS, HIGHLIGHT_GLOW, HIGHLIGHT_RING, HIGHLIGHT_ICON, ...HIGHLIGHT_LABEL_LAYERS] as const;
    const pointerLayers = [...hoverableLayers, CLUSTERS, SPIDER_POINTS] as const;

    for (const layerId of pointClickLayers) {
      map.on('click', layerId, handlePointClick);
    }
    map.on('click', CLUSTERS, handleClusterClick);
    map.on('click', SPIDER_POINTS, handleSpiderClick);
    map.on('click', handleMapClick);
    for (const layerId of hoverableLayers) {
      map.on('mouseenter', layerId, handlePointHoverEnter);
      map.on('mouseleave', layerId, handlePointHoverLeave);
    }
    for (const layerId of pointerLayers) {
      map.on('mouseenter', layerId, setPointerCursor);
      map.on('mouseleave', layerId, resetCursor);
    }

    return () => {
      clearSpider(map);
      for (const layerId of pointClickLayers) {
        map.off('click', layerId, handlePointClick);
      }
      map.off('click', CLUSTERS, handleClusterClick);
      map.off('click', SPIDER_POINTS, handleSpiderClick);
      map.off('click', handleMapClick);
      for (const layerId of hoverableLayers) {
        map.off('mouseenter', layerId, handlePointHoverEnter);
        map.off('mouseleave', layerId, handlePointHoverLeave);
      }
      for (const layerId of pointerLayers) {
        map.off('mouseenter', layerId, setPointerCursor);
        map.off('mouseleave', layerId, resetCursor);
      }
      try {
        for (const layerId of [...HIGHLIGHT_LABEL_LAYERS].reverse()) {
          if (map.getLayer(layerId)) map.removeLayer(layerId);
        }
        if (map.getLayer(HIGHLIGHT_ICON)) map.removeLayer(HIGHLIGHT_ICON);
        if (map.getLayer(HIGHLIGHT_RING)) map.removeLayer(HIGHLIGHT_RING);
        if (map.getLayer(HIGHLIGHT_GLOW)) map.removeLayer(HIGHLIGHT_GLOW);
        if (map.getLayer(HIGHLIGHT_CONNECTORS)) map.removeLayer(HIGHLIGHT_CONNECTORS);
        if (map.getLayer(CLUSTER_COUNT)) map.removeLayer(CLUSTER_COUNT);
        if (map.getLayer(CLUSTER_CORE)) map.removeLayer(CLUSTER_CORE);
        if (map.getLayer(CLUSTERS)) map.removeLayer(CLUSTERS);
        if (map.getLayer(CLUSTER_GLOW)) map.removeLayer(CLUSTER_GLOW);
        if (map.getLayer(POINTS)) map.removeLayer(POINTS);
        if (map.getSource(HIGHLIGHT_SRC)) map.removeSource(HIGHLIGHT_SRC);
        if (map.getSource(SRC)) map.removeSource(SRC);
      } catch { /* disposed */ }
    };
  }, [map, isReady, styleEpoch, theme, clearSpider, onSelect]);

  // ---- Effect 2: load icon images & push data on object/theme changes ----
  useEffect(() => {
    if (!map || !isReady) return;
    let cancelled = false;
    const dimClusters = mutedObjectIdsRef.current.length > 0
      || Boolean(mapInteractionSpotlightIdsRef.current && mapInteractionSpotlightIdsRef.current.size > 0);

    (async () => {
      if (!canAccessMapStyle(map)) return;
      try {
        await ensureMarkerImages(map, objects, theme);
      } catch { /* map may have been disposed during style transition */ }
      if (cancelled) return;
      try {
        if (!canAccessMapStyle(map)) return;
        const highlightedIds = new Set<string>();
        for (const item of highlightedObjectsRef.current || []) {
          const id = String(item?.id || '').trim();
          if (id) highlightedIds.add(id);
        }
        const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
        if (src) {
          src.setData(buildFeatureCollection(
            objects,
            theme,
            new Set(mutedObjectIdsRef.current),
            undefined,
            mapInteractionSpotlightIdsRef.current,
            highlightedIds,
          ));
        }
        const highlightSrc = map.getSource(HIGHLIGHT_SRC) as maplibregl.GeoJSONSource | undefined;
        if (highlightSrc) {
          highlightSrc.setData(buildHighlightFeatureCollection(
            map,
            objects,
            highlightedObjectsRef.current,
            theme,
          ));
        }
        applyClusterMuteState(map, theme === 'dark', dimClusters);
      } catch { /* source might not exist during style transition */ }
    })();

    return () => { cancelled = true; };
  }, [
    map,
    isReady,
    objects,
    theme,
    styleEpoch,
    options?.highlightedObjects,
    options?.mutedObjectIds,
    options?.mapInteractionSpotlightObjectIds,
  ]);

  useEffect(() => {
    if (!map || !isReady) return;
    let rafId = 0;

    const scheduleRefresh = () => {
      if (rafId) return;
      rafId = window.requestAnimationFrame(() => {
        rafId = 0;
        try {
          if (!canAccessMapStyle(map)) return;
          const highlightSrc = map.getSource(HIGHLIGHT_SRC) as maplibregl.GeoJSONSource | undefined;
          if (!highlightSrc) return;
          highlightSrc.setData(buildHighlightFeatureCollection(
            map,
            objectsRef.current,
            highlightedObjectsRef.current,
            theme,
          ));
        } catch {
          /* source might be missing during style transition */
        }
      });
    };

    scheduleRefresh();
    map.on('move', scheduleRefresh);
    map.on('resize', scheduleRefresh);

    return () => {
      if (rafId) window.cancelAnimationFrame(rafId);
      map.off('move', scheduleRefresh);
      map.off('resize', scheduleRefresh);
    };
  }, [map, isReady, styleEpoch, theme]);
}
