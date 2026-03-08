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

const SRC = 'geo-markers';
const CLUSTER_GLOW = 'geo-cluster-glow';
const CLUSTERS = 'geo-clusters';
const CLUSTER_CORE = 'geo-cluster-core';
const CLUSTER_COUNT = 'geo-cluster-count';
const POINTS = 'geo-unclustered';
const SPIDER_SRC = 'geo-spider';
const SPIDER_POINTS = 'geo-spider-points';
const SPIDER_LEGS = 'geo-spider-legs';

const CLUSTER_MAX_ZOOM = 5;
const CLUSTER_RADIUS = 40;
const MARKER_PX = 28;
const ICON_PX = 16;
const PIXEL_RATIO = 2;

const CLUSTER_RING_RADIUS: maplibregl.ExpressionSpecification = ['step', ['get', 'point_count'], 13, 10, 17, 50, 22, 200, 28];
const CLUSTER_GLOW_RADIUS: maplibregl.ExpressionSpecification = ['step', ['get', 'point_count'], 16, 10, 19, 50, 24, 200, 30];
const CLUSTER_CORE_RADIUS: maplibregl.ExpressionSpecification = ['step', ['get', 'point_count'], 2.25, 10, 2.75, 50, 3.25, 200, 4.25];
const CLUSTER_RING_STROKE_WIDTH: maplibregl.ExpressionSpecification = ['step', ['get', 'point_count'], 1.15, 10, 1.35, 50, 1.65, 200, 1.95];
const CLUSTER_TEXT_SIZE: maplibregl.ExpressionSpecification = ['step', ['get', 'point_count'], 10, 10, 11, 50, 12, 200, 13];

// ---------------------------------------------------------------------------
// Icon key helpers
// ---------------------------------------------------------------------------

function resolveVisualTypeAndColor(
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

function iconKey(vt: string, color: string): string {
  return `marker-${vt}-${color.replace('#', '')}`;
}

function canAccessMapStyle(map: maplibregl.Map): boolean {
  try {
    return Boolean((map as unknown as { style?: unknown }).style);
  } catch {
    return false;
  }
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

async function ensureMarkerImages(
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
): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = objects.map((obj) => {
    const lat = obj.coordinates?.lat ?? 0;
    const lon = obj.coordinates?.lon ?? 0;
    const { vt, color } = resolveVisualTypeAndColor(obj, theme);
    return {
      type: 'Feature',
      geometry: { type: 'Point', coordinates: [lon, lat] },
      properties: {
        id: obj.id,
        iconKey: iconKey(vt, color),
        color,
        opacity: obj.inScope === false ? 0.5 : 1.0,
        type: obj.type,
        label: obj.label || obj.canonicalPath,
      },
    };
  });
  return { type: 'FeatureCollection', features };
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export function useGeoMarkerLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  objects: GeoObject[],
  theme: 'dark' | 'light',
  onSelect: (obj: GeoObject | null, anchor?: [number, number]) => void,
) {
  const objectsRef = useRef(objects);
  objectsRef.current = objects;
  const spiderActiveRef = useRef(false);

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

    map.addSource(SRC, {
      type: 'geojson',
      data: { type: 'FeatureCollection', features: [] },
      cluster: true,
      clusterMaxZoom: CLUSTER_MAX_ZOOM,
      clusterRadius: CLUSTER_RADIUS,
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
      },
      paint: {
        'icon-opacity': ['get', 'opacity'],
      },
    });

    // ---- click handlers ----

    const handlePointClick = (e: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      const feat = e.features?.[0];
      if (!feat) return;
      const id = feat.properties?.id;
      const match = objectsRef.current.find((o) => o.id === id);
      const anchor: [number, number] = [e.lngLat.lng, e.lngLat.lat];
      onSelect(match ?? null, anchor);
    };

    const handleClusterClick = (e: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      const feat = e.features?.[0];
      if (!feat) return;
      const clusterId = feat.properties?.cluster_id;
      const src = map.getSource(SRC) as maplibregl.GeoJSONSource;
      if (!src || clusterId == null) return;

      src.getClusterExpansionZoom(clusterId).then((zoom) => {
        const center = (feat.geometry as GeoJSON.Point).coordinates as [number, number];
        if (zoom > CLUSTER_MAX_ZOOM) {
          src.getClusterLeaves(clusterId, 50, 0).then((leaves) => {
            if (!leaves.length) return;
            clearSpider(map);
            const origin = (leaves[0].geometry as GeoJSON.Point).coordinates as [number, number];
            const n = leaves.length;
            const spiderFeatures: GeoJSON.Feature[] = [];
            const legFeatures: GeoJSON.Feature[] = [];

            for (let i = 0; i < n; i++) {
              const angle = (2 * Math.PI * i) / n;
              const radius = 30 + (n > 8 ? i * 4 : 0);
              const projected = map.project(origin as maplibregl.LngLatLike);
              const offsetPx = {
                x: projected.x + Math.cos(angle) * radius,
                y: projected.y + Math.sin(angle) * radius,
              };
              const offsetLngLat = map.unproject(offsetPx as maplibregl.PointLike);
              const leaf = leaves[i];

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

            const spiderFc: GeoJSON.FeatureCollection = {
              type: 'FeatureCollection',
              features: [...legFeatures, ...spiderFeatures],
            };
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
          });
        } else {
          clearSpider(map);
          map.easeTo({ center, zoom });
        }
      });
    };

    const handleSpiderClick = (e: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      const feat = e.features?.[0];
      if (!feat) return;
      const id = feat.properties?.id;
      const match = objectsRef.current.find((o) => o.id === id);
      const anchor: [number, number] = [e.lngLat.lng, e.lngLat.lat];
      onSelect(match ?? null, anchor);
    };

    const handleMapClick = () => {
      if (spiderActiveRef.current) clearSpider(map);
    };

    map.on('click', POINTS, handlePointClick);
    map.on('click', CLUSTERS, handleClusterClick);
    map.on('click', SPIDER_POINTS, handleSpiderClick);
    map.on('click', handleMapClick);
    map.on('mouseenter', POINTS, () => { map.getCanvas().style.cursor = 'pointer'; });
    map.on('mouseleave', POINTS, () => { map.getCanvas().style.cursor = ''; });
    map.on('mouseenter', CLUSTERS, () => { map.getCanvas().style.cursor = 'pointer'; });
    map.on('mouseleave', CLUSTERS, () => { map.getCanvas().style.cursor = ''; });
    map.on('mouseenter', SPIDER_POINTS, () => { map.getCanvas().style.cursor = 'pointer'; });
    map.on('mouseleave', SPIDER_POINTS, () => { map.getCanvas().style.cursor = ''; });

    return () => {
      clearSpider(map);
      map.off('click', POINTS, handlePointClick);
      map.off('click', CLUSTERS, handleClusterClick);
      map.off('click', SPIDER_POINTS, handleSpiderClick);
      map.off('click', handleMapClick);
      try {
        if (map.getLayer(CLUSTER_COUNT)) map.removeLayer(CLUSTER_COUNT);
        if (map.getLayer(CLUSTER_CORE)) map.removeLayer(CLUSTER_CORE);
        if (map.getLayer(CLUSTERS)) map.removeLayer(CLUSTERS);
        if (map.getLayer(CLUSTER_GLOW)) map.removeLayer(CLUSTER_GLOW);
        if (map.getLayer(POINTS)) map.removeLayer(POINTS);
        if (map.getSource(SRC)) map.removeSource(SRC);
      } catch { /* disposed */ }
    };
  }, [styleEpoch]);

  // ---- Effect 2: load icon images & push data on object/theme changes ----
  useEffect(() => {
    if (!map || !isReady) return;
    let cancelled = false;

    (async () => {
      if (!canAccessMapStyle(map)) return;
      try {
        await ensureMarkerImages(map, objects, theme);
      } catch { /* map may have been disposed during style transition */ }
      if (cancelled) return;
      try {
        if (!canAccessMapStyle(map)) return;
        const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
        if (src) src.setData(buildFeatureCollection(objects, theme));
      } catch { /* source might not exist during style transition */ }
    })();

    return () => { cancelled = true; };
  }, [objects, theme, styleEpoch]);
}
