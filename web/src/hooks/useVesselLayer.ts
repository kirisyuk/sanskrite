/**
 * Real-time vessel (AIS) layer.
 * Uses backend proxy (AISStream) when VITE_VESSEL_API_URL is not set; otherwise custom URL.
 * See web/docs/VESSEL_TRACKING_SERVICES.md. Backend needs AISSTREAM_API_KEY in env.
 */

import { useEffect, useRef, useCallback } from 'react';
import maplibregl from 'maplibre-gl';

const SRC = 'vessels-src';
const LAYER = 'vessels-layer';
const REFRESH_MS = 60000;
const DEBOUNCE_MS = 800;

export interface VesselPoint {
  lat: number;
  lon: number;
  mmsi: string | number;
  name?: string;
  sog?: number;
  cog?: number;
  shipType?: string;
}

const env = (import.meta as unknown as { env: Record<string, string> }).env ?? {};

function getApiUrl(): string {
  const custom = env.VITE_VESSEL_API_URL;
  if (custom) return custom;
  const base = env.VITE_API_BASE_URL || 'http://localhost:8080';
  return `${base.replace(/\/$/, '')}/api/v1/vessels/bbox`;
}

function isBackendProxy(): boolean {
  return getApiUrl().includes('/api/v1/vessels/bbox');
}

/** Vessels are available when using backend proxy (no key on frontend) or when custom API key/URL is set. */
export function vesselsApiAvailable(): boolean {
  if (isBackendProxy()) return true;
  return !!env.VITE_VESSEL_API_KEY || !!env.VITE_MARINESIA_API_KEY;
}

async function fetchVesselsInBbox(
  minLat: number,
  maxLat: number,
  minLon: number,
  maxLon: number,
): Promise<VesselPoint[]> {
  const base = getApiUrl();
  const params = new URLSearchParams({
    minLat: String(minLat),
    maxLat: String(maxLat),
    minLon: String(minLon),
    maxLon: String(maxLon),
  });
  if (!isBackendProxy()) {
    const key = env.VITE_VESSEL_API_KEY ?? env.VITE_MARINESIA_API_KEY;
    if (key) params.set('key', key);
    else params.set('lat', String((minLat + maxLat) / 2));
    params.set('lng', String((minLon + maxLon) / 2));
    params.set('min_lat', String(minLat));
    params.set('max_lat', String(maxLat));
    params.set('min_lng', String(minLon));
    params.set('max_lng', String(maxLon));
  }
  const url = `${base}?${params.toString()}`;
  const res = await fetch(url);
  if (!res.ok) return [];

  const json = await res.json();
  if (json?.data && Array.isArray(json.data)) {
    return json.data.map((v: { lat?: number; lng?: number; lon?: number; mmsi?: string | number; name?: string; sog?: number; cog?: number }) => ({
      lat: v.lat ?? 0,
      lon: v.lng ?? v.lon ?? 0,
      mmsi: v.mmsi ?? '',
      name: v.name,
      sog: v.sog,
      cog: v.cog,
    })).filter((v: VesselPoint) => v.lat && v.lon);
  }
  if (Array.isArray(json)) {
    return json.map((v: { lat?: number; lng?: number; lon?: number; mmsi?: string | number; name?: string; sog?: number; cog?: number }) => ({
      lat: v.lat ?? 0,
      lon: v.lng ?? v.lon ?? 0,
      mmsi: v.mmsi ?? '',
      name: v.name,
      sog: v.sog,
      cog: v.cog,
    })).filter((v: VesselPoint) => v.lat && v.lon);
  }
  return [];
}

function toGeoJSON(vessels: VesselPoint[]): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature<GeoJSON.Point, { mmsi: string; name?: string; sog?: number; cog?: number }>[] = vessels.map((v) => ({
    type: 'Feature',
    geometry: { type: 'Point', coordinates: [v.lon, v.lat] },
    properties: {
      mmsi: String(v.mmsi),
      name: v.name,
      sog: v.sog,
      cog: v.cog,
    },
  }));
  return { type: 'FeatureCollection', features };
}

export function useVesselLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  enabled: boolean,
  theme: 'dark' | 'light',
) {
  const popupRef = useRef<maplibregl.Popup | null>(null);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const fetchAndSet = useCallback(async () => {
    if (!map || !enabled) return;
    const b = map.getBounds();
    if (!b) return;
    const minLat = b.getSouth();
    const maxLat = b.getNorth();
    const minLon = b.getWest();
    const maxLon = b.getEast();
    const vessels = await fetchVesselsInBbox(minLat, maxLat, minLon, maxLon);
    const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
    if (src) src.setData(toGeoJSON(vessels));
  }, [map, enabled]);

  useEffect(() => {
    if (!map || !isReady) return;

    const available = vesselsApiAvailable();
    if (!available || !enabled) {
      if (map.getLayer(LAYER)) map.removeLayer(LAYER);
      if (map.getSource(SRC)) map.removeSource(SRC);
      return;
    }

    if (!map.getSource(SRC)) {
      map.addSource(SRC, { type: 'geojson', data: toGeoJSON([]) });
    }
    if (!map.getLayer(LAYER)) {
      const beforeId = map.getLayer('geo-clusters') ? 'geo-clusters' : undefined;
      map.addLayer({
        id: LAYER,
        type: 'circle',
        source: SRC,
        layout: { visibility: 'visible' },
        paint: {
          'circle-radius': ['interpolate', ['linear'], ['zoom'], 2, 3, 6, 5, 10, 8],
          'circle-color': theme === 'dark' ? '#38bdf8' : '#0369a1',
          'circle-opacity': 0.9,
          'circle-stroke-width': 1,
          'circle-stroke-color': theme === 'dark' ? '#0f172a' : '#f8fafc',
        },
      }, beforeId);
    }

    fetchAndSet();
    timerRef.current = setInterval(fetchAndSet, REFRESH_MS);

    const onMoveEnd = () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(fetchAndSet, DEBOUNCE_MS);
    };
    map.on('moveend', onMoveEnd);

    const onClick = (e: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      const f = e.features?.[0];
      if (!f?.properties) return;
      const p = f.properties as { mmsi?: string; name?: string; sog?: number; cog?: number };
      popupRef.current?.remove();
      popupRef.current = new maplibregl.Popup({ closeButton: true, closeOnClick: true })
        .setLngLat(e.lngLat)
        .setHTML(
          `<div style="font-size:12px; font-family: system-ui, sans-serif;">` +
            `<div><b>${(p.name || '—').toString().replace(/</g, '&lt;')}</b></div>` +
            `<div>MMSI: ${(p.mmsi ?? '—').toString()}</div>` +
            (p.sog != null ? `<div>Speed: ${p.sog} kn</div>` : '') +
            (p.cog != null ? `<div>Course: ${p.cog}°</div>` : '') +
            `</div>`
        )
        .addTo(map);
    };
    map.on('click', LAYER, onClick);

    return () => {
      map.off('moveend', onMoveEnd);
      map.off('click', LAYER, onClick);
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (timerRef.current) clearInterval(timerRef.current);
      timerRef.current = null;
      popupRef.current?.remove();
      popupRef.current = null;
      try {
        if (map.getLayer(LAYER)) map.removeLayer(LAYER);
        if (map.getSource(SRC)) map.removeSource(SRC);
      } catch {
        // map disposed
      }
    };
  }, [map, isReady, styleEpoch, enabled, theme, fetchAndSet]);

  useEffect(() => {
    if (!map || !isReady || !enabled) return;
    const visibility = enabled && vesselsApiAvailable() ? 'visible' : 'none';
    if (map.getLayer(LAYER)) map.setLayoutProperty(LAYER, 'visibility', visibility);
  }, [map, isReady, enabled]);
}
