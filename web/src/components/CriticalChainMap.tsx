/**
 * CriticalChain Map — whitepaper 4.4
 * Layers: Base (dark) | Choropleth | Chokepoints | Facilities | Routes | Conflict zones
 * Routes as MapLibre lines; deck.gl ArcLayer disabled (caused jumping artifacts)
 */
import React, { useRef, useEffect, useCallback, useMemo } from 'react';
import maplibregl from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import * as topojson from 'topojson-client';
import type {
  CriticalChainFacility,
  CriticalChainRoute,
  CriticalChainConflictZone,
  CriticalChainChokepoint,
} from '../lib/criticalchain-api';

const RISK_COLORS: Record<number, string> = {
  1: '#00C853',
  2: '#00C853',
  3: '#FFD700',
  4: '#FF8C00',
  5: '#FF4444',
};

function getRiskColor(score: number): string {
  const level = Math.min(5, Math.max(1, Math.round(score)));
  return RISK_COLORS[level] ?? '#484F58';
}

type CountryRiskLookup = {
  byIso: Record<string, number>;
  byName: Record<string, number>;
};

const countryDisplayNames =
  typeof Intl !== 'undefined' && 'DisplayNames' in Intl
    ? new Intl.DisplayNames(['en'], { type: 'region' })
    : null;

const COUNTRY_NAME_ALIASES: Record<string, string> = {
  'bolivia plurinational state of': 'bolivia',
  'bosnia and herz': 'bosnia and herzegovina',
  'czechia': 'czech republic',
  'dem rep congo': 'democratic republic of the congo',
  'dominican rep': 'dominican republic',
  'ivory coast': "cote d'ivoire",
  'north macedonia': 'macedonia',
  'palestine': 'palestinian territories',
  'russia': 'russian federation',
  'south korea': 'korea republic of',
  'syria': 'syrian arab republic',
  'taiwan': 'taiwan province of china',
  'tanzania': 'tanzania united republic of',
  'united states of america': 'united states',
  'venezuela': 'venezuela bolivarian republic of',
  'viet nam': 'vietnam',
};

function normalizeCountryName(name: string): string {
  return name
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/&/g, 'and')
    .replace(/[^a-z0-9]+/g, ' ')
    .trim();
}

function buildCountryRiskLookup(countryRisk: Record<string, number>): CountryRiskLookup {
  const byIso: Record<string, number> = {};
  const byName: Record<string, number> = {};

  Object.entries(countryRisk).forEach(([rawCode, score]) => {
    const code = rawCode.trim().toUpperCase();
    if (!code) return;
    byIso[code] = score;
    if (code.length === 2 && countryDisplayNames) {
      try {
        const name = countryDisplayNames.of(code);
        if (name) {
          const normalized = normalizeCountryName(name);
          byName[normalized] = Math.max(byName[normalized] ?? 0, score);
        }
      } catch {
        // Invalid region subtag (EU, XX, etc.) — skip DisplayNames lookup
      }
    }
  });

  Object.entries(COUNTRY_NAME_ALIASES).forEach(([source, target]) => {
    if (byName[target] != null) {
      byName[source] = byName[target];
    }
  });

  return { byIso, byName };
}

function getCountryRiskScore(
  properties: Record<string, unknown>,
  lookup: CountryRiskLookup
): number {
  const iso = String(properties.iso_a2 ?? properties.ISO_A2 ?? '').trim().toUpperCase();
  if (iso && lookup.byIso[iso] != null) {
    return lookup.byIso[iso];
  }
  const name = normalizeCountryName(String(properties.name ?? ''));
  if (!name) return 0;
  if (lookup.byName[name] != null) {
    return lookup.byName[name];
  }
  const alias = COUNTRY_NAME_ALIASES[name];
  if (alias && lookup.byName[alias] != null) {
    return lookup.byName[alias];
  }
  return 0;
}

export type MapViewMode = 'overview' | 'flow' | 'node';

export interface CriticalChainMapProps {
  facilities: CriticalChainFacility[];
  routes: CriticalChainRoute[];
  conflictZones: CriticalChainConflictZone[];
  chokepoints: CriticalChainChokepoint[];
  countryRisk: Record<string, number>;
  selectedMaterial?: string | null;
  viewMode?: MapViewMode;
  layerChoropleth?: boolean;
  layerChokepoints?: boolean;
  layerFacilities?: boolean;
  layerRoutes?: boolean;
  layerConflictZones?: boolean;
  onFeatureClick?: (feature: any) => void;
}

const COUNTRIES_TOPO =
  'https://cdn.jsdelivr.net/npm/world-atlas@2/countries-110m.json';

export function CriticalChainMap({
  facilities,
  routes,
  conflictZones,
  chokepoints,
  countryRisk,
  selectedMaterial,
  viewMode = 'overview',
  layerChoropleth = true,
  layerChokepoints = true,
  layerFacilities = true,
  layerRoutes = true,
  layerConflictZones = true,
  onFeatureClick,
}: CriticalChainMapProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<maplibregl.Map | null>(null);
  const countriesGeoRef = useRef<GeoJSON.FeatureCollection | null>(null);
  const countryRiskLookup = useMemo(() => buildCountryRiskLookup(countryRisk), [countryRisk]);
  const refs = useRef({
    facilities,
    routes,
    conflictZones,
    chokepoints,
    countryRiskLookup,
  });
  refs.current = {
    facilities,
    routes,
    conflictZones,
    chokepoints,
    countryRiskLookup,
  };

  useEffect(() => {
    const node = containerRef.current;
    if (!node) return;

    const style: maplibregl.StyleSpecification = {
      version: 8,
      sources: {
        'carto-basemap': {
          type: 'raster',
          tiles: [
            'https://a.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}@2x.png',
            'https://b.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}@2x.png',
          ],
          tileSize: 256,
        },
      },
      layers: [
        { id: 'bg', type: 'background', paint: { 'background-color': '#0D1117' } },
        { id: 'tiles', type: 'raster', source: 'carto-basemap', minzoom: 0, maxzoom: 22 },
      ],
    };

    const map = new maplibregl.Map({
      container: node,
      style,
      center: [20, 30],
      zoom: 2,
      minZoom: 1,
      maxZoom: 14,
    });
    map.addControl(new maplibregl.NavigationControl(), 'top-right');
    mapRef.current = map;

    // Cluster click: zoom in to expand
    map.on('click', 'facilities-clusters', (e) => {
      const feat = e.features?.[0];
      if (!feat?.properties?.cluster_id) return;
      const src = map.getSource('facilities') as maplibregl.GeoJSONSource & { getClusterExpansionZoom?: (id: number) => Promise<number> };
      if (src?.getClusterExpansionZoom) {
        src.getClusterExpansionZoom(feat.properties.cluster_id).then((zoom) => {
          map.easeTo({ center: (feat.geometry as GeoJSON.Point).coordinates as [number, number], zoom });
        });
      }
    });
    map.on('mouseenter', 'facilities-clusters', () => { map.getCanvas().style.cursor = 'pointer'; });
    map.on('mouseleave', 'facilities-clusters', () => { map.getCanvas().style.cursor = ''; });

    map.on('load', () => {
      addFacilitiesLayer(map);
      addRoutesLayer(map);
      addConflictZonesLayer(map);
      addChokepointsLayer(map);
      // deck.gl disabled — ArcLayer caused jumping triangles with zoom
      // map.addControl(new MapboxOverlay({ layers: [] }));
      updateAllLayers(map, refs.current);
      // Choropleth: fetch TopoJSON, convert to GeoJSON, add risk
      fetch(COUNTRIES_TOPO)
      .then((r) => r.json())
      .then((world: { objects: { countries: unknown } }) => {
        const countries = world.objects.countries;
        const geojson = topojson.feature(world as object, countries as object) as GeoJSON.FeatureCollection;
        const coords = (g: GeoJSON.Geometry): number[][] => {
          if (g.type === 'Point') return [g.coordinates];
          if (g.type === 'LineString') return g.coordinates;
          if (g.type === 'Polygon') return g.coordinates.flat();
          if (g.type === 'MultiPolygon') return g.coordinates.flat(2);
          return [];
        };
        const bounds = (pts: number[][]) => {
          if (pts.length === 0) return { height: 0, width: 0 };
          const lons = pts.map((p) => p[0]);
          const lats = pts.map((p) => p[1]);
          return { height: Math.max(...lats) - Math.min(...lats), width: Math.max(...lons) - Math.min(...lons) };
        };
        geojson.features = geojson.features.filter((f) => {
          const name = String((f.properties as Record<string, unknown>)?.name ?? '');
          if (/antarctica/i.test(name)) return false;
          const pts = coords(f.geometry);
          const { height, width } = bounds(pts);
          return height >= 0.5 && width >= 0.5; // skip degenerate
        });
        countriesGeoRef.current = geojson;
        geojson.features = geojson.features.map((f) => {
          const props = (f.properties || {}) as Record<string, unknown>;
          const score = getCountryRiskScore(props, refs.current.countryRiskLookup);
          return {
            ...f,
            properties: { ...props, risk_score: score },
          };
        });
        try {
          map.addSource('countries', { type: 'geojson', data: geojson });
          map.addLayer({
            id: 'choropleth',
            type: 'fill',
            source: 'countries',
            paint: {
              'fill-color': [
                'interpolate',
                ['linear'],
                ['get', 'risk_score'],
                0, '#484F58',
                1.5, '#00C853',
                3, '#FFD700',
                4, '#FF8C00',
                5, '#FF4444',
              ],
              'fill-opacity': ['case', ['>', ['get', 'risk_score'], 0], 0.5, 0.15],
              'fill-outline-color': '#21262D',
            },
          });
        } catch (e) {
          console.warn('[CriticalChain] Choropleth error:', e);
        }
      })
      .catch((e) => console.warn('[CriticalChain] Countries fetch:', e));
    });

    return () => {
      map.remove();
      mapRef.current = null;
    };
  }, []);

  const addFacilitiesLayer = (map: maplibregl.Map) => {
    try {
      // Supercluster for >500 objects (whitepaper 4.4). Always enable cluster; works with any count.
      map.on('click', 'facilities-circles', (e) => {
        if (onFeatureClick && e.features?.[0]) onFeatureClick(e.features[0].properties);
      });
      map.addSource('facilities', {
        type: 'geojson',
        data: { type: 'FeatureCollection' as const, features: [] },
        cluster: true,
        clusterMaxZoom: 12,
        clusterRadius: 50,
      });
      map.addLayer({
        id: 'facilities-clusters',
        type: 'circle',
        source: 'facilities',
        filter: ['has', 'point_count'],
        paint: {
          'circle-color': '#4FC3F7',
          'circle-radius': ['step', ['get', 'point_count'], 15, 10, 20, 50, 25],
          'circle-stroke-width': 2,
          'circle-stroke-color': '#E6EDF3',
        },
      });
      map.addLayer({
        id: 'facilities-cluster-count',
        type: 'symbol',
        source: 'facilities',
        filter: ['has', 'point_count'],
        layout: {
          'text-field': ['get', 'point_count_abbreviated'],
          'text-font': ['DIN Offc Pro Medium', 'Arial Unicode MS Bold'],
          'text-size': 12,
        },
        paint: { 'text-color': '#0D1117' },
      });
      map.addLayer({
        id: 'facilities-circles',
        type: 'circle',
        source: 'facilities',
        filter: ['!', ['has', 'point_count']],
        paint: {
          'circle-radius': 6,
          'circle-color': [
            'interpolate',
            ['linear'],
            ['get', 'riskScore'],
            1, '#00C853',
            3, '#FFD700',
            5, '#FF4444',
          ],
          'circle-stroke-width': 1,
          'circle-stroke-color': '#E6EDF3',
        },
      });
    } catch (e) {
      console.warn('[CriticalChain] Facilities layer:', e);
    }
  };

  const addRoutesLayer = (map: maplibregl.Map) => {
    try {
      map.addSource('routes', {
        type: 'geojson',
        data: { type: 'FeatureCollection' as const, features: [] },
      });
      map.addLayer({
        id: 'routes-line',
        type: 'line',
        source: 'routes',
        paint: {
          'line-color': '#4FC3F7',
          'line-width': 2,
          'line-opacity': 0.6,
          'line-dasharray': [2, 1],
        },
      });
    } catch (e) {
      console.warn('[CriticalChain] Routes layer:', e);
    }
  };

  const addConflictZonesLayer = (map: maplibregl.Map) => {
    try {
      map.addSource('conflict-zones', {
        type: 'geojson',
        data: { type: 'FeatureCollection' as const, features: [] },
      });
      map.addLayer({
        id: 'conflict-zones-fill',
        type: 'fill',
        source: 'conflict-zones',
        paint: {
          'fill-color': '#FF4444',
          'fill-opacity': 0.2,
        },
      });
    } catch (e) {
      console.warn('[CriticalChain] Conflict zones layer:', e);
    }
  };

  const addChokepointsLayer = (map: maplibregl.Map) => {
    try {
      map.addSource('chokepoints', {
        type: 'geojson',
        data: { type: 'FeatureCollection' as const, features: [] },
      });
      map.addLayer({
        id: 'chokepoints-circles',
        type: 'circle',
        source: 'chokepoints',
        paint: {
          'circle-radius': 10,
          'circle-color': '#FF8C00',
          'circle-stroke-width': 2,
          'circle-stroke-color': '#E6EDF3',
        },
      });
    } catch (e) {
      console.warn('[CriticalChain] Chokepoints layer:', e);
    }
  };

  const updateAllLayers = useCallback(
    (map: maplibregl.Map, data: typeof refs.current) => {
      const facs = data.facilities ?? [];
      const rts = data.routes ?? [];
      const zones = data.conflictZones ?? [];
      const chokes = data.chokepoints ?? [];

      const facSrc = map.getSource('facilities') as maplibregl.GeoJSONSource | undefined;
      if (facSrc) {
        const withCoords = facs.filter((f) => {
          const lat = f.coordinates?.latitude;
          const lon = f.coordinates?.longitude;
          return typeof lat === 'number' && typeof lon === 'number' &&
            !Number.isNaN(lat) && !Number.isNaN(lon) &&
            lon >= -180 && lon <= 180 && lat >= -90 && lat <= 90;
        });
        facSrc.setData({
          type: 'FeatureCollection',
          features: withCoords.map((f) => ({
            type: 'Feature',
            geometry: {
              type: 'Point',
              coordinates: [f.coordinates!.longitude, f.coordinates!.latitude],
            },
            properties: { id: f.id, name: f.name, riskScore: f.riskScore, country: f.country },
          })),
        });
      }

      const routeSrc = map.getSource('routes') as maplibregl.GeoJSONSource | undefined;
      if (routeSrc) {
        const isValidCoord = (lon: number, lat: number) =>
          typeof lon === 'number' && typeof lat === 'number' &&
          !Number.isNaN(lon) && !Number.isNaN(lat) &&
          lon >= -180 && lon <= 180 && lat >= -90 && lat <= 90;

        const feats = rts
          .filter((r) => r.waypoints && r.waypoints.length >= 2)
          .map((r) => {
            const coords = r.waypoints!.map((w) => [w.longitude, w.latitude] as [number, number]);
            const allValid = coords.every(([lon, lat]) => isValidCoord(lon, lat));
            if (!allValid) return null;
            return {
              type: 'Feature' as const,
              geometry: { type: 'LineString' as const, coordinates: coords },
              properties: { id: r.id, riskScore: r.riskScore },
            };
          })
          .filter((f): f is NonNullable<typeof f> => f != null);
        routeSrc.setData({ type: 'FeatureCollection' as const, features: feats });
      }

      const zoneSrc = map.getSource('conflict-zones') as maplibregl.GeoJSONSource | undefined;
      if (zoneSrc) {
        const isValidCoord = (lon: number, lat: number) =>
          typeof lon === 'number' && typeof lat === 'number' &&
          !Number.isNaN(lon) && !Number.isNaN(lat) &&
          lon >= -180 && lon <= 180 && lat >= -90 && lat <= 90;

        const feats = zones
          .filter((z) => z.boundingBox && z.boundingBox.length >= 3)
          .map((z) => {
            const coords = z.boundingBox!.map((p) => [p.longitude, p.latitude] as [number, number]);
            const allValid = coords.every(([lon, lat]) => isValidCoord(lon, lat));
            if (!allValid) return null;
            const lons = coords.map(([lon]) => lon);
            const lats = coords.map(([, lat]) => lat);
            const height = Math.max(...lats) - Math.min(...lats);
            const width = Math.max(...lons) - Math.min(...lons);
            if (height < 0.5 || width < 0.5) return null; // degenerate → red lines
            if (coords.length >= 4 && (coords[0][0] !== coords[coords.length - 1][0] || coords[0][1] !== coords[coords.length - 1][1])) {
              coords.push(coords[0]);
            }
            return {
              type: 'Feature' as const,
              geometry: { type: 'Polygon' as const, coordinates: [coords] },
              properties: { id: z.id, name: z.name, severity: z.severity },
            };
          })
          .filter((f): f is NonNullable<typeof f> => f != null);
        zoneSrc.setData({ type: 'FeatureCollection' as const, features: feats });
      }

      const chokeSrc = map.getSource('chokepoints') as maplibregl.GeoJSONSource | undefined;
      if (chokeSrc) {
        const isValidCoord = (lon: number, lat: number) =>
          typeof lon === 'number' && typeof lat === 'number' &&
          !Number.isNaN(lon) && !Number.isNaN(lat) &&
          lon >= -180 && lon <= 180 && lat >= -90 && lat <= 90;

        const features = chokes
          .filter((c) => isValidCoord(c.longitude, c.latitude))
          .map((c) => ({
            type: 'Feature' as const,
            geometry: {
              type: 'Point' as const,
              coordinates: [c.longitude, c.latitude] as [number, number],
            },
            properties: { id: c.id, name: c.name, riskMultiplier: c.riskMultiplier },
          }));
        chokeSrc.setData({ type: 'FeatureCollection' as const, features });
      }
    },
    []
  );

  useEffect(() => {
    const map = mapRef.current;
    const src = map?.getSource('countries') as maplibregl.GeoJSONSource | undefined;
    if (!src || !countriesGeoRef.current) return;
    const data = { ...countriesGeoRef.current };
    data.features = data.features.map((f) => {
      const props = (f.properties || {}) as Record<string, unknown>;
      const score = getCountryRiskScore(props, refs.current.countryRiskLookup);
      return { ...f, properties: { ...props, risk_score: score } };
    });
    src.setData(data);
  }, [countryRiskLookup]);

  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    updateAllLayers(map, refs.current);
  }, [facilities, routes, conflictZones, chokepoints, updateAllLayers]);


  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    const setVisibility = (id: string, visible: boolean) => {
      try {
        map.setLayoutProperty(id, 'visibility', visible ? 'visible' : 'none');
      } catch {}
    };
    setVisibility('choropleth', layerChoropleth && viewMode !== 'node');
    setVisibility('chokepoints-circles', layerChokepoints);
    setVisibility('facilities-circles', layerFacilities);
    setVisibility('facilities-clusters', layerFacilities);
    setVisibility('facilities-cluster-count', layerFacilities);
    setVisibility('routes-line', layerRoutes && viewMode !== 'node');
    setVisibility('conflict-zones-fill', layerConflictZones);
  }, [viewMode, layerChoropleth, layerChokepoints, layerFacilities, layerRoutes, layerConflictZones]);

  return (
    <>
      <style>{`
        @keyframes pulse {
          0% { transform: scale(1); opacity: 1; }
          100% { transform: scale(3); opacity: 0; }
        }
        .pulse-marker::after {
          content: '';
          position: absolute;
          top: 0; left: 0; right: 0; bottom: 0;
          border-radius: 50%;
          border: 2px solid #EF4444;
          animation: pulse 1.5s infinite ease-out;
        }
      `}</style>
      <div ref={containerRef} className="w-full h-full min-h-[400px]" />
    </>
  );
}
