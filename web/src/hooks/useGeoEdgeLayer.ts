import { useEffect, useRef, useState } from 'react';
import maplibregl from 'maplibre-gl';
import { api } from '../lib/api';
import type { GeoObject } from './useGeoObjects';
import { getEdgeColor } from '../components/HorizonGraphNodeTypes';

const SRC = 'geo-edges';
const LAYER = 'geo-edges-lines';

function toPastel(hex: string, blend = 0.55): string {
  const m = hex.match(/^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i);
  if (!m) return hex;
  const r = Math.round(parseInt(m[1], 16) * (1 - blend) + 255 * blend);
  const g = Math.round(parseInt(m[2], 16) * (1 - blend) + 255 * blend);
  const b = Math.round(parseInt(m[3], 16) * (1 - blend) + 255 * blend);
  return `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${b.toString(16).padStart(2, '0')}`;
}

function arcPoints(lon1: number, lat1: number, lon2: number, lat2: number, numPoints = 16): [number, number][] {
  const midLon = (lon1 + lon2) / 2;
  const midLat = (lat1 + lat2) / 2;
  const dx = lon2 - lon1;
  const dy = lat2 - lat1;
  const len = Math.sqrt(dx * dx + dy * dy) || 1;
  const perpX = -dy / len;
  const perpY = dx / len;
  const bulge = Math.min(len * 0.25, 4);
  const offsetLon = midLon + perpX * bulge;
  const offsetLat = midLat + perpY * bulge;
  const points: [number, number][] = [];
  for (let i = 0; i <= numPoints; i++) {
    const t = i / numPoints;
    const t1 = 1 - t;
    const lon = t1 * t1 * lon1 + 2 * t1 * t * offsetLon + t * t * lon2;
    const lat = t1 * t1 * lat1 + 2 * t1 * t * offsetLat + t * t * lat2;
    points.push([lon, lat]);
  }
  return points;
}

function fragmentToGeoObject(
  f: { id?: string; canonicalPath?: string; type?: string; label?: string; [k: string]: unknown },
  coordsMap: Record<string, { lat: number; lon: number }>,
): GeoObject | null {
  const id = f?.id ?? (f as any)?.ID;
  if (!id || typeof id !== 'string') return null;
  const lat = (f as any)?.coordinates?.lat ?? coordsMap[id]?.lat;
  const lon = (f as any)?.coordinates?.lon ?? coordsMap[id]?.lon;
  if (!Number.isFinite(lat) || !Number.isFinite(lon)) return null;
  return {
    id,
    canonicalPath: (f?.canonicalPath ?? (f as any)?.CanonicalPath ?? '') as string,
    type: (f?.type ?? (f as any)?.Type ?? 'OTHER') as string,
    label: (f?.label ?? (f as any)?.Label ?? '') as string | undefined,
    coordinates: { lat: lat as number, lon: lon as number },
  };
}

export function useGeoEdgeLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  selectedObject: GeoObject | null,
  objects: GeoObject[],
  hops: number = 3,
) {
  const [edgesLoading, setEdgesLoading] = useState(false);
  const [edgesCount, setEdgesCount] = useState(0);
  const [discoveredObjects, setDiscoveredObjects] = useState<GeoObject[]>([]);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  useEffect(() => {
    if (!map || !isReady) return;

    const removeLayers = () => {
      try {
        if (map.getLayer(LAYER)) map.removeLayer(LAYER);
        if (map.getSource(SRC)) map.removeSource(SRC);
      } catch { /* disposed */ }
    };

    if (!selectedObject) {
      removeLayers();
      setEdgesLoading(false);
      setEdgesCount(0);
      return;
    }

    const idToCoords = new Map<string, { lon: number; lat: number }>();
    for (const obj of objects) {
      const lat = obj.coordinates?.lat;
      const lon = obj.coordinates?.lon;
      if (Number.isFinite(lat) && Number.isFinite(lon)) {
        idToCoords.set(obj.id, { lon: lon!, lat: lat! });
      }
    }

    let cancelled = false;
    setEdgesLoading(true);
    setEdgesCount(0);

    api
      .getFragmentDependencies(selectedObject.id, {
        direction: 'both',
        includeFragments: true,
        includeCoordinates: true,
        limit: 200,
        hops: Math.min(5, Math.max(1, hops)),
      })
      .then((res) => {
        if (cancelled || !mountedRef.current) return;
        const edges = res?.edges ?? [];
        const serverCoords = res?.coordinates ?? {};
        const fragments = res?.fragments ?? [];
        const objectIds = new Set(objects.map((o) => o.id));
        for (const [id, c] of Object.entries(serverCoords)) {
          if (c && Number.isFinite(c.lat) && Number.isFinite(c.lon)) {
            idToCoords.set(id, { lon: c.lon, lat: c.lat });
          }
        }
        const features: GeoJSON.Feature<GeoJSON.LineString>[] = [];
        for (const e of edges) {
          const src = e?.sourceFragmentId ?? e?.sourceFragmentID;
          const tgt = e?.targetFragmentId ?? e?.targetFragmentID;
          const edgeType = (e?.type ?? '').trim();
          const srcCoords = src ? idToCoords.get(src) : undefined;
          const tgtCoords = tgt ? idToCoords.get(tgt) : undefined;
          if (!srcCoords || !tgtCoords) continue;
          features.push({
            type: 'Feature',
            geometry: { type: 'LineString', coordinates: arcPoints(srcCoords.lon, srcCoords.lat, tgtCoords.lon, tgtCoords.lat) },
            properties: { type: edgeType, color: toPastel(getEdgeColor(edgeType)) },
          });
        }
        const newDiscovered: GeoObject[] = [];
        for (const f of fragments) {
          const obj = fragmentToGeoObject(f, serverCoords);
          if (obj && !objectIds.has(obj.id)) {
            newDiscovered.push(obj);
            objectIds.add(obj.id);
          }
        }
        if (cancelled || !mountedRef.current) return;
        setDiscoveredObjects((prev) => {
          const byId = new Map(prev.map((o) => [o.id, o]));
          for (const o of newDiscovered) {
            if (!byId.has(o.id)) byId.set(o.id, o);
          }
          return Array.from(byId.values());
        });
        removeLayers();
        setEdgesCount(features.length);
        if (features.length === 0) {
          setEdgesLoading(false);
          return;
        }
        map.addSource(SRC, { type: 'geojson', data: { type: 'FeatureCollection', features } });
        map.addLayer({
          id: LAYER,
          type: 'line',
          source: SRC,
          layout: { 'line-cap': 'round', 'line-join': 'round' },
          paint: {
            'line-color': ['get', 'color'],
            'line-width': 1.25,
            'line-opacity': 0.75,
            'line-dasharray': [2, 3],
          },
        });
        if (mountedRef.current) setEdgesLoading(false);
      })
      .catch(() => {
        if (!cancelled && mountedRef.current) setEdgesLoading(false);
      });

    return () => {
      cancelled = true;
      removeLayers();
    };
  }, [selectedObject?.id, objects, hops, map, isReady, styleEpoch]);

  return { edgesLoading, edgesCount, discoveredObjects };
}
