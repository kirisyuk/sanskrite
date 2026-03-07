import { useEffect } from 'react';
import maplibregl from 'maplibre-gl';
import type { GeoObject } from './useGeoObjects';
import { getAffectedZoneMapAccent, getHorizonNodeVisualType, getNodeColors } from '../components/HorizonGraphNodeTypes';

const SRC = 'geo-zones';
const LAYER = 'geo-zones-fill';
const OUTLINE_LAYER = 'geo-zones-outline';

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

function zoneColor(obj: GeoObject, theme: 'dark' | 'light'): string {
  if (obj.type === 'AFFECTED_ZONE') return getAffectedZoneMapAccent(obj.canonicalPath, theme);
  const vt = getHorizonNodeVisualType(obj.type, obj.canonicalPath, obj.label, (obj as any)?.facilityKind);
  return getNodeColors(vt || obj.type, theme).accent;
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
      return { fillOpacity: 0.30 + factor * 0.12, lineOpacity: 0.86, lineWidth: 2.8 + factor * 1.8 };
    case 'active':
      return { fillOpacity: 0.22 + factor * 0.08, lineOpacity: 0.78, lineWidth: 2.2 + factor * 1.2 };
    case 'watch':
      return { fillOpacity: 0.14 + factor * 0.06, lineOpacity: 0.66, lineWidth: 1.7 + factor * 0.8 };
    case 'cooling':
      return { fillOpacity: 0.08 + factor * 0.04, lineOpacity: 0.48, lineWidth: 1.2 + factor * 0.5 };
    default:
      return { fillOpacity: 0.04, lineOpacity: 0.35, lineWidth: 0.9 };
  }
}

function buildZoneCollection(objects: GeoObject[], theme: 'dark' | 'light'): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = [];
  for (const obj of objects) {
    const radiusKm = Number(obj.radiusKm);
    if (obj.type !== 'AFFECTED_ZONE' || !Number.isFinite(radiusKm) || radiusKm <= 0) continue;
    const lat = obj.coordinates?.lat;
    const lon = obj.coordinates?.lon;
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) continue;
    const hype = zoneHype(obj);
    const status = zoneStatus(obj, hype);
    const style = zoneStyle(status, hype);
    features.push({
      type: 'Feature',
      geometry: circlePolygon(lat!, lon!, Math.min(radiusKm, 2000)),
      properties: {
        color: zoneColor(obj, theme),
        fillOpacity: style.fillOpacity,
        lineOpacity: style.lineOpacity,
        lineWidth: style.lineWidth,
      },
    });
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

    const fc = buildZoneCollection(objects, theme);

    map.addSource(SRC, { type: 'geojson', data: fc });
    map.addLayer({
      id: LAYER,
      type: 'fill',
      source: SRC,
      paint: {
        'fill-color': ['get', 'color'],
        'fill-opacity': ['coalesce', ['get', 'fillOpacity'], 0.12],
      },
    });
    map.addLayer({
      id: OUTLINE_LAYER,
      type: 'line',
      source: SRC,
      paint: {
        'line-color': ['get', 'color'],
        'line-width': ['coalesce', ['get', 'lineWidth'], 1],
        'line-opacity': ['coalesce', ['get', 'lineOpacity'], 0.4],
      },
    });

    return () => {
      try {
        if (map.getLayer(OUTLINE_LAYER)) map.removeLayer(OUTLINE_LAYER);
        if (map.getLayer(LAYER)) map.removeLayer(LAYER);
        if (map.getSource(SRC)) map.removeSource(SRC);
      } catch { /* disposed */ }
    };
  }, [styleEpoch]);

  // Update data between style reloads.
  useEffect(() => {
    if (!map || !isReady) return;
    const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
    if (!src) return;
    src.setData(buildZoneCollection(objects, theme));
  }, [objects, theme]);
}
