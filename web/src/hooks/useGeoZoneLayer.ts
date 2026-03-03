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
  const vt = getHorizonNodeVisualType(obj.type, obj.canonicalPath, obj.label);
  return getNodeColors(vt || obj.type, theme).accent;
}

function buildZoneCollection(objects: GeoObject[], theme: 'dark' | 'light'): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = [];
  for (const obj of objects) {
    const radiusKm = Number(obj.radiusKm);
    if (obj.type !== 'AFFECTED_ZONE' || !Number.isFinite(radiusKm) || radiusKm <= 0) continue;
    const lat = obj.coordinates?.lat;
    const lon = obj.coordinates?.lon;
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) continue;
    features.push({
      type: 'Feature',
      geometry: circlePolygon(lat!, lon!, Math.min(radiusKm, 2000)),
      properties: { color: zoneColor(obj, theme) },
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
        'fill-opacity': 0.12,
      },
    });
    map.addLayer({
      id: OUTLINE_LAYER,
      type: 'line',
      source: SRC,
      paint: {
        'line-color': ['get', 'color'],
        'line-width': 1,
        'line-opacity': 0.4,
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
