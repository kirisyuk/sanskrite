import React from 'react';
import maplibregl from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { renderToStaticMarkup } from 'react-dom/server';
import { api } from '../lib/api';
import { ThemeContext } from './ThemeProvider';
import { HorizonNodeIcon, formatHorizonFragmentType, getAffectedZoneMapAccent, getEdgeColor, getHorizonNodeVisualType, getNodeColors } from './HorizonGraphNodeTypes';

type GeoObject = {
  id: string;
  canonicalPath: string;
  type: string;
  label?: string;
  description?: string;
  status?: string;
  coordinates?: { lat: number; lon: number };
  radiusKm?: number;
  createdAt?: string;
};

type TypeCount = { type: string; count: number };

function deriveTypeCounts(objects: GeoObject[]): TypeCount[] {
  const counts = new Map<string, number>();
  for (const obj of objects) {
    counts.set(obj.type, (counts.get(obj.type) || 0) + 1);
  }
  return Array.from(counts.entries())
    .map(([type, count]) => ({ type, count }))
    .sort((a, b) => b.count - a.count);
}

/** Convert hex to pastel by blending with white. */
function toPastel(hex: string, blend = 0.55): string {
  const m = hex.match(/^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i);
  if (!m) return hex;
  const r = Math.round(parseInt(m[1], 16) * (1 - blend) + 255 * blend);
  const g = Math.round(parseInt(m[2], 16) * (1 - blend) + 255 * blend);
  const b = Math.round(parseInt(m[3], 16) * (1 - blend) + 255 * blend);
  return `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${b.toString(16).padStart(2, '0')}`;
}

/** Build curved line points between two coordinates (quadratic bezier arc). */
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

/** Build GeoJSON polygon approximating a circle (lat, lon, radiusKm). */
function circlePolygon(lat: number, lon: number, radiusKm: number, points = 32) {
  const kmPerDegLat = 111.32;
  const latRad = (lat * Math.PI) / 180;
  const kmPerDegLon = 111.32 * Math.cos(latRad);
  const dLat = radiusKm / kmPerDegLat;
  const dLon = radiusKm / kmPerDegLon;
  const ring: [number, number][] = [];
  for (let i = 0; i <= points; i++) {
    const θ = (2 * Math.PI * i) / points;
    ring.push([lon + dLon * Math.cos(θ), lat + dLat * Math.sin(θ)]);
  }
  return { type: 'Polygon' as const, coordinates: [ring] };
}

export const WorldMapPanel: React.FC = () => {
  const { theme } = React.useContext(ThemeContext);
  const mapNodeRef = React.useRef<HTMLDivElement | null>(null);
  const mapRef = React.useRef<maplibregl.Map | null>(null);
  const markersRef = React.useRef<maplibregl.Marker[]>([]);
  const zoneCircleRef = React.useRef<{ sourceId: string; layerId: string }[]>([]);
  const edgeLayersRef = React.useRef<{ sourceId: string; layerId: string } | null>(null);
  const mountedRef = React.useRef(true);

  const [loadingMap, setLoadingMap] = React.useState(true);
  const [loadingData, setLoadingData] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [allObjects, setAllObjects] = React.useState<GeoObject[]>([]);
  const [selectedObject, setSelectedObject] = React.useState<GeoObject | null>(null);
  const [edgesLoading, setEdgesLoading] = React.useState(false);
  const [edgesCount, setEdgesCount] = React.useState(0);
  const [activeTypes, setActiveTypes] = React.useState<Set<string> | null>(null);
  const [dateFrom, setDateFrom] = React.useState<string>('');
  const [dateTo, setDateTo] = React.useState<string>('');

  const isDark = theme === 'dark';
  const cartoVariant = isDark ? 'dark_all' : 'light_all';
  const bgColor = isDark ? '#0a0f0c' : '#f8fafc';
  const paletteTheme = isDark ? 'dark' as const : 'light' as const;

  React.useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const typeCounts = React.useMemo(() => deriveTypeCounts(allObjects), [allObjects]);

  const filteredObjects = React.useMemo(() => {
    if (!activeTypes) return allObjects;
    return allObjects.filter((obj) => activeTypes.has(obj.type));
  }, [allObjects, activeTypes]);

  React.useEffect(() => {
    const node = mapNodeRef.current;
    if (!node || mapRef.current) return;

    setLoadingMap(true);
    const map = new maplibregl.Map({
      container: node,
      style: {
        version: 8,
        name: isDark ? 'Worldmonitor Dark' : 'Worldmonitor Light',
        sources: {
          'carto-basemap': {
            type: 'raster',
            tiles: [
              `https://a.basemaps.cartocdn.com/${cartoVariant}/{z}/{x}/{y}@2x.png`,
              `https://b.basemaps.cartocdn.com/${cartoVariant}/{z}/{x}/{y}@2x.png`,
              `https://c.basemaps.cartocdn.com/${cartoVariant}/{z}/{x}/{y}@2x.png`,
            ],
            tileSize: 256,
            attribution: '&copy; <a href="https://carto.com/">CARTO</a>',
          },
        },
        layers: [
          {
            id: 'background',
            type: 'background',
            paint: {
              'background-color': bgColor,
            },
          },
          {
            id: 'carto-basemap-layer',
            type: 'raster',
            source: 'carto-basemap',
            minzoom: 0,
            maxzoom: 22,
          },
        ],
        glyphs: 'https://demotiles.maplibre.org/font/{fontstack}/{range}.pbf',
      },
      center: [15, 20],
      zoom: 1.5,
      minZoom: 1,
      maxZoom: 8,
      attributionControl: false,
      dragRotate: false,
      touchPitch: false,
      pitchWithRotate: false,
      renderWorldCopies: true,
    });

    mapRef.current = map;
    map.addControl(new maplibregl.NavigationControl({ visualizePitch: false }), 'top-right');
    map.addControl(new maplibregl.AttributionControl({ compact: true }), 'bottom-left');

    map.on('load', () => {
      setLoadingMap(false);
    });

    map.on('error', (event) => {
      const msg = event?.error?.message || 'Map rendering error';
      setError(msg);
      setLoadingMap(false);
    });

    return () => {
      markersRef.current.forEach((marker) => marker.remove());
      markersRef.current = [];
      map.remove();
      mapRef.current = null;
    };
  }, [theme]);

  React.useEffect(() => {
    let cancelled = false;
    async function loadObjects(): Promise<void> {
      setLoadingData(true);
      try {
        const opts: { limit: number; createdAfter?: string; createdBefore?: string } = { limit: 2500 };
        if (dateFrom) opts.createdAfter = dateFrom;
        if (dateTo) opts.createdBefore = dateTo;
        const response = await api.listHorizonGeoObjects(opts);
        if (cancelled) return;
        const valid = (response.items || []).filter((item) => {
          const lat = item.coordinates?.lat;
          const lon = item.coordinates?.lon;
          return Number.isFinite(lat) && Number.isFinite(lon);
        });
        setAllObjects(valid);
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load geo objects');
        }
      } finally {
        if (!cancelled) setLoadingData(false);
      }
    }
    void loadObjects();
    return () => {
      cancelled = true;
    };
  }, [dateFrom, dateTo]);

  React.useEffect(() => {
    const map = mapRef.current;
    if (!map) return;

    markersRef.current.forEach((marker) => marker.remove());
    markersRef.current = [];

    // Remove previous zone circles (layer first, then source)
    for (const { layerId, sourceId } of zoneCircleRef.current) {
      if (map.getLayer(layerId)) map.removeLayer(layerId);
      if (map.getSource(sourceId)) map.removeSource(sourceId);
    }
    zoneCircleRef.current = [];

    filteredObjects.forEach((obj) => {
      const lat = obj.coordinates?.lat;
      const lon = obj.coordinates?.lon;
      const latNum = Number(lat);
      const lonNum = Number(lon);
      if (!Number.isFinite(latNum) || !Number.isFinite(lonNum)) return;

      const visualType = getHorizonNodeVisualType(obj.type, obj.canonicalPath, obj.label);
      const color =
        obj.type === 'AFFECTED_ZONE'
          ? getAffectedZoneMapAccent(obj.canonicalPath, paletteTheme)
          : getNodeColors(visualType || obj.type, paletteTheme).accent;
      const iconSvg = renderToStaticMarkup(
        <HorizonNodeIcon type={visualType || obj.type} color={color} size={12} />,
      );

      const el = document.createElement('button');
      el.type = 'button';
      el.title = obj.label || obj.canonicalPath;
      el.style.width = '24px';
      el.style.height = '24px';
      el.style.borderRadius = '999px';
      el.style.border = `1.5px solid ${isDark ? 'rgba(226,232,240,0.35)' : 'rgba(15,23,42,0.25)'}`;
      el.style.background = isDark ? 'rgba(2,6,23,0.86)' : 'rgba(255,255,255,0.92)';
      el.style.boxShadow = `0 0 0 2px ${isDark ? 'rgba(2,6,23,0.65)' : 'rgba(255,255,255,0.85)'}`;
      el.style.cursor = 'pointer';
      el.style.padding = '0';
      el.style.display = 'grid';
      el.style.placeItems = 'center';
      el.innerHTML = iconSvg;
      el.addEventListener('click', (event) => {
        event.preventDefault();
        event.stopPropagation();
        setSelectedObject((prev) => (prev?.id === obj.id ? null : obj));
      });

      const marker = new maplibregl.Marker({ element: el, anchor: 'center' })
        .setLngLat([lonNum, latNum])
        .addTo(map);
      markersRef.current.push(marker);

      // AFFECTED_ZONE with radiusKm: draw circle polygon
      const radiusKm = Number(obj.radiusKm);
      if (obj.type === 'AFFECTED_ZONE' && Number.isFinite(radiusKm) && radiusKm > 0) {
        const sourceId = `zone-circle-${obj.id}`;
        const layerId = `zone-circle-layer-${obj.id}`;
        try {
          const poly = circlePolygon(latNum, lonNum, Math.min(radiusKm, 2000));
          const feature = { type: 'Feature' as const, geometry: poly, properties: {} };
          if (!map.getSource(sourceId)) {
            map.addSource(sourceId, { type: 'geojson', data: feature });
          } else {
            (map.getSource(sourceId) as maplibregl.GeoJSONSource).setData(feature);
          }
          if (!map.getLayer(layerId)) {
            map.addLayer({
              id: layerId,
              type: 'fill',
              source: sourceId,
              paint: {
                'fill-color': color,
                'fill-opacity': 0.15,
                'fill-outline-color': color,
              },
            });
          }
          zoneCircleRef.current.push({ sourceId, layerId });
        } catch {
          // ignore addLayer errors (e.g. map not ready)
        }
      }
    });
  }, [filteredObjects, theme]);

  // Fetch and draw dependency edges when an object is selected
  React.useEffect(() => {
    const map = mapRef.current;
    if (!map || loadingMap) return;

    const sourceId = 'map-edges-source';
    const layerId = 'map-edges-layer';

    const removeEdgeLayers = () => {
      if (edgeLayersRef.current && map) {
        try {
          if (map.getLayer(layerId)) map.removeLayer(layerId);
          if (map.getSource(sourceId)) map.removeSource(sourceId);
        } catch {
          // Map may be disposed during unmount
        }
        edgeLayersRef.current = null;
      }
    };

    if (!selectedObject) {
      removeEdgeLayers();
      setEdgesLoading(false);
      setEdgesCount(0);
      return;
    }

    const idToCoords = new Map<string, { lon: number; lat: number }>();
    for (const obj of filteredObjects) {
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
      .getFragmentDependencies(selectedObject.id, { direction: 'both', includeFragments: false, limit: 200 })
      .then((res) => {
        if (cancelled || !mountedRef.current) return;
        const edges = res?.edges ?? [];
        const features: GeoJSON.Feature<GeoJSON.LineString, { type: string; color: string }>[] = [];
        for (const e of edges) {
          const src = e?.sourceFragmentId ?? e?.sourceFragmentID;
          const tgt = e?.targetFragmentId ?? e?.targetFragmentID;
          const edgeType = (e?.type ?? '').trim();
          const srcCoords = src ? idToCoords.get(src) : undefined;
          const tgtCoords = tgt ? idToCoords.get(tgt) : undefined;
          if (!srcCoords || !tgtCoords) continue;
          const coords = arcPoints(srcCoords.lon, srcCoords.lat, tgtCoords.lon, tgtCoords.lat);
          features.push({
            type: 'Feature',
            geometry: { type: 'LineString', coordinates: coords },
            properties: { type: edgeType, color: toPastel(getEdgeColor(edgeType)) },
          });
        }
        if (cancelled || !mountedRef.current) return;
        removeEdgeLayers();
        setEdgesCount(features.length);
        if (features.length === 0) {
          if (mountedRef.current) setEdgesLoading(false);
          return;
        }
        if (!mountedRef.current) return;
        map.addSource(sourceId, {
          type: 'geojson',
          data: { type: 'FeatureCollection', features },
        });
        map.addLayer({
          id: layerId,
          type: 'line',
          source: sourceId,
          layout: {
            'line-cap': 'round',
            'line-join': 'round',
          },
          paint: {
            'line-color': ['get', 'color'],
            'line-width': 1.25,
            'line-opacity': 0.75,
            'line-dasharray': [2, 3],
          },
        });
        edgeLayersRef.current = { sourceId, layerId };
        if (mountedRef.current) setEdgesLoading(false);
      })
      .catch(() => {
        if (!cancelled && mountedRef.current) setEdgesLoading(false);
      });

    return () => {
      cancelled = true;
      removeEdgeLayers();
    };
  }, [selectedObject?.id, filteredObjects, loadingMap]);

  const handleToggleType = React.useCallback((type: string) => {
    setActiveTypes((prev) => {
      if (!prev) {
        const all = new Set(typeCounts.map((tc) => tc.type));
        all.delete(type);
        return all;
      }
      const next = new Set(prev);
      if (next.has(type)) {
        next.delete(type);
      } else {
        next.add(type);
      }
      if (next.size === typeCounts.length) return null;
      return next;
    });
  }, [typeCounts]);

  const handleSelectAll = React.useCallback(() => setActiveTypes(null), []);

  const handleSelectNone = React.useCallback(() => setActiveTypes(new Set()), []);

  const selectedVisualType = selectedObject
    ? getHorizonNodeVisualType(selectedObject.type, selectedObject.canonicalPath, selectedObject.label)
    : '';
  const selectedAccent = getNodeColors(
    selectedVisualType || selectedObject?.type || '',
    paletteTheme,
  ).accent;
  const loading = loadingMap || loadingData;
  const isAllSelected = activeTypes === null;
  const filterActive = activeTypes !== null;

  return (
    <div
      className="flex-1 min-h-0"
      style={{
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <div style={{ position: 'relative', flex: 1, minHeight: 0 }}>
        <div
          ref={mapNodeRef}
          style={{
            width: '100%',
            height: '100%',
            minHeight: 280,
          }}
        />

        {loading && (
          <div
            style={{
              position: 'absolute',
              inset: 0,
              display: 'grid',
              placeItems: 'center',
              color: 'var(--text-secondary-strong)',
              fontSize: 13,
              background: 'rgba(0,0,0,0.2)',
            }}
          >
            Loading geo objects...
          </div>
        )}

        {error && (
          <div
            style={{
              position: 'absolute',
              left: 10,
              right: 10,
              bottom: 10,
              padding: '8px 10px',
              borderRadius: 8,
              border: '1px solid rgba(253,164,175,0.45)',
              backgroundColor: 'rgba(127,29,29,0.35)',
              color: '#fecdd3',
              fontSize: 12,
            }}
          >
            {error}
          </div>
        )}

        {/* Filters overlay — always visible, top-left */}
        <div
          style={{
            position: 'absolute',
            left: 10,
            top: 10,
            width: 240,
            maxHeight: 'calc(100% - 100px)',
            overflowY: 'auto',
            padding: '12px 12px 10px',
            borderRadius: 10,
            backgroundColor: isDark ? 'rgba(6, 14, 22, 0.94)' : 'rgba(255,255,255,0.97)',
            border: isDark ? '1px solid rgba(255,255,255,0.18)' : '1px solid rgba(15,23,42,0.2)',
            boxShadow: isDark ? '0 8px 32px rgba(0,0,0,0.5)' : '0 8px 32px rgba(15,23,42,0.15)',
            color: 'var(--text-heading)',
            fontSize: 11,
          }}
        >
          <div style={{ fontWeight: 600, fontSize: 12, marginBottom: 10, letterSpacing: '0.02em' }}>
            Filters
          </div>

          {/* Date range */}
          <div style={{ marginBottom: 12 }}>
            <div style={{ fontSize: 10, color: 'var(--text-secondary-strong)', marginBottom: 4 }}>Date range</div>
            <div style={{ display: 'flex', gap: 6 }}>
              <input
                type="date"
                value={dateFrom}
                onChange={(e) => setDateFrom(e.target.value)}
                style={{
                  flex: 1,
                  padding: '6px 8px',
                  borderRadius: 6,
                  border: isDark ? '1px solid rgba(255,255,255,0.2)' : '1px solid rgba(15,23,42,0.25)',
                  background: isDark ? 'rgba(0,0,0,0.3)' : 'rgba(255,255,255,0.9)',
                  color: 'var(--text-heading)',
                  fontSize: 11,
                }}
                placeholder="From"
              />
              <input
                type="date"
                value={dateTo}
                onChange={(e) => setDateTo(e.target.value)}
                style={{
                  flex: 1,
                  padding: '6px 8px',
                  borderRadius: 6,
                  border: isDark ? '1px solid rgba(255,255,255,0.2)' : '1px solid rgba(15,23,42,0.25)',
                  background: isDark ? 'rgba(0,0,0,0.3)' : 'rgba(255,255,255,0.9)',
                  color: 'var(--text-heading)',
                  fontSize: 11,
                }}
                placeholder="To"
              />
            </div>
          </div>

          {/* Type filter */}
          <div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
              <span style={{ fontSize: 10, color: 'var(--text-secondary-strong)' }}>Object types</span>
              <div style={{ display: 'flex', gap: 6 }}>
                <button
                  type="button"
                  onClick={handleSelectAll}
                  style={{
                    background: 'none',
                    border: 'none',
                    cursor: 'pointer',
                    fontSize: 10,
                    color: isAllSelected ? 'var(--text-secondary-strong)' : (isDark ? '#60a5fa' : '#2563eb'),
                    padding: '2px 4px',
                    borderRadius: 4,
                  }}
                >
                  All
                </button>
                <button
                  type="button"
                  onClick={handleSelectNone}
                  style={{
                    background: 'none',
                    border: 'none',
                    cursor: 'pointer',
                    fontSize: 10,
                    color: isDark ? '#60a5fa' : '#2563eb',
                    padding: '2px 4px',
                    borderRadius: 4,
                  }}
                >
                  None
                </button>
              </div>
            </div>
            {typeCounts.length > 0 ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                {typeCounts.map(({ type, count }) => {
                  const isActive = isAllSelected || (activeTypes?.has(type) ?? false);
                  const visualType = getHorizonNodeVisualType(type, '', '');
                  const accent = getNodeColors(visualType || type, paletteTheme).accent;
                  return (
                    <button
                      key={type}
                      type="button"
                      onClick={() => handleToggleType(type)}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: 7,
                        padding: '4px 6px',
                        borderRadius: 6,
                        border: 'none',
                        background: isActive
                          ? (isDark ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.06)')
                          : 'transparent',
                        cursor: 'pointer',
                        textAlign: 'left',
                        width: '100%',
                        opacity: isActive ? 1 : 0.45,
                        transition: 'opacity 0.15s, background 0.15s',
                      }}
                    >
                      <HorizonNodeIcon type={visualType || type} color={accent} size={12} />
                      <span style={{
                        flex: 1,
                        fontSize: 11,
                        color: 'var(--text-heading)',
                        whiteSpace: 'nowrap',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                      }}>
                        {formatHorizonFragmentType(type, '', '')}
                      </span>
                      <span style={{
                        fontSize: 10,
                        color: 'var(--text-secondary-strong)',
                        minWidth: 20,
                        textAlign: 'right',
                        fontVariantNumeric: 'tabular-nums',
                      }}>
                        {count}
                      </span>
                    </button>
                  );
                })}
              </div>
            ) : (
              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)' }}>
                {loadingData ? 'Loading…' : 'No types yet'}
              </div>
            )}
          </div>
        </div>

        {/* Object count badge */}
        <div
          style={{
            position: 'absolute',
            left: 10,
            bottom: 10,
            padding: '6px 10px',
            borderRadius: 8,
            backgroundColor: isDark ? 'rgba(5, 11, 17, 0.78)' : 'rgba(248, 250, 252, 0.92)',
            border: isDark ? '1px solid rgba(255,255,255,0.12)' : '1px solid rgba(15,23,42,0.15)',
            color: 'var(--text-secondary-strong)',
            fontSize: 11,
          }}
        >
          {filterActive
            ? `Shown: ${filteredObjects.length} / ${allObjects.length}`
            : `Objects: ${allObjects.length}`
          }
        </div>

        {selectedObject && (
          <div
            style={{
              position: 'absolute',
              left: 260,
              top: 10,
              maxWidth: 340,
              padding: '10px 12px',
              borderRadius: 10,
              backgroundColor: isDark ? 'rgba(6, 14, 22, 0.9)' : 'rgba(255, 255, 255, 0.95)',
              border: isDark ? '1px solid rgba(255,255,255,0.14)' : '1px solid rgba(15,23,42,0.2)',
              color: 'var(--text-heading)',
              boxShadow: isDark ? '0 8px 30px rgba(0,0,0,0.35)' : '0 8px 30px rgba(15,23,42,0.12)',
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <HorizonNodeIcon type={selectedVisualType || selectedObject.type} color={selectedAccent} size={14} />
              <div style={{ fontSize: 13, fontWeight: 700 }}>
                {selectedObject.label || selectedObject.canonicalPath}
              </div>
            </div>
            <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 2 }}>
              {formatHorizonFragmentType(selectedObject.type, selectedObject.canonicalPath, selectedObject.label)}
            </div>
            <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 8 }}>
              {selectedObject.canonicalPath}
            </div>
            {selectedObject.description && (
              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 8 }}>
                {selectedObject.description}
              </div>
            )}
            <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 8 }}>
              Status: {selectedObject.status || 'ACTIVE'}
            </div>
            {edgesLoading && (
              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 6 }}>
                Loading connections…
              </div>
            )}
            {!edgesLoading && edgesCount > 0 && (
              <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', marginTop: 6 }}>
                Connections: {edgesCount}
              </div>
            )}
            <button
              type="button"
              className="sidebar-icon-btn"
              style={{ marginTop: 8 }}
              onClick={() => setSelectedObject(null)}
              title="Close details"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M18 6 6 18" />
                <path d="M6 6l12 12" />
              </svg>
            </button>
          </div>
        )}
      </div>
    </div>
  );
};

export default WorldMapPanel;
