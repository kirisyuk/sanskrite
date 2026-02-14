import React from 'react';
import maplibregl from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { renderToStaticMarkup } from 'react-dom/server';
import { api } from '../lib/api';
import { ThemeContext } from './ThemeProvider';
import { HorizonNodeIcon, formatHorizonFragmentType, getHorizonNodeVisualType, getNodeColors } from './HorizonGraphNodeTypes';

type GeoObject = {
  id: string;
  canonicalPath: string;
  type: string;
  label?: string;
  description?: string;
  status?: string;
  coordinates?: { lat: number; lon: number };
};

export const WorldMapPanel: React.FC = () => {
  const { theme } = React.useContext(ThemeContext);
  const mapNodeRef = React.useRef<HTMLDivElement | null>(null);
  const mapRef = React.useRef<maplibregl.Map | null>(null);
  const markersRef = React.useRef<maplibregl.Marker[]>([]);

  const [loadingMap, setLoadingMap] = React.useState(true);
  const [loadingData, setLoadingData] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [objects, setObjects] = React.useState<GeoObject[]>([]);
  const [selectedObject, setSelectedObject] = React.useState<GeoObject | null>(null);

  React.useEffect(() => {
    const node = mapNodeRef.current;
    if (!node || mapRef.current) return;

    const map = new maplibregl.Map({
      container: node,
      style: {
        version: 8,
        name: 'Worldmonitor Dark',
        sources: {
          'carto-dark': {
            type: 'raster',
            tiles: [
              'https://a.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}@2x.png',
              'https://b.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}@2x.png',
              'https://c.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}@2x.png',
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
              'background-color': '#0a0f0c',
            },
          },
          {
            id: 'carto-dark-layer',
            type: 'raster',
            source: 'carto-dark',
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
  }, []);

  React.useEffect(() => {
    let cancelled = false;
    async function loadObjects(): Promise<void> {
      setLoadingData(true);
      try {
        const response = await api.listHorizonGeoObjects({ limit: 2500 });
        if (cancelled) return;
        const valid = (response.items || []).filter((item) => {
          const lat = item.coordinates?.lat;
          const lon = item.coordinates?.lon;
          return Number.isFinite(lat) && Number.isFinite(lon);
        });
        setObjects(valid);
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
  }, []);

  React.useEffect(() => {
    const map = mapRef.current;
    if (!map) return;

    markersRef.current.forEach((marker) => marker.remove());
    markersRef.current = [];

    const paletteTheme = theme === 'dark' ? 'dark' : 'light';
    objects.forEach((obj) => {
      const lat = obj.coordinates?.lat;
      const lon = obj.coordinates?.lon;
      const latNum = Number(lat);
      const lonNum = Number(lon);
      if (!Number.isFinite(latNum) || !Number.isFinite(lonNum)) return;

      const visualType = getHorizonNodeVisualType(obj.type, obj.canonicalPath, obj.label);
      const color = getNodeColors(visualType || obj.type, paletteTheme).accent;
      const iconSvg = renderToStaticMarkup(
        <HorizonNodeIcon type={visualType || obj.type} color={color} size={12} />,
      );

      const el = document.createElement('button');
      el.type = 'button';
      el.title = obj.label || obj.canonicalPath;
      el.style.width = '24px';
      el.style.height = '24px';
      el.style.borderRadius = '999px';
      el.style.border = `1.5px solid ${theme === 'dark' ? 'rgba(226,232,240,0.35)' : 'rgba(15,23,42,0.25)'}`;
      el.style.background = theme === 'dark' ? 'rgba(2,6,23,0.86)' : 'rgba(255,255,255,0.92)';
      el.style.boxShadow = `0 0 0 2px ${theme === 'dark' ? 'rgba(2,6,23,0.65)' : 'rgba(255,255,255,0.85)'}`;
      el.style.cursor = 'pointer';
      el.style.padding = '0';
      el.style.display = 'grid';
      el.style.placeItems = 'center';
      el.innerHTML = iconSvg;
      el.addEventListener('click', (event) => {
        event.preventDefault();
        event.stopPropagation();
        setSelectedObject(obj);
      });

      const marker = new maplibregl.Marker({ element: el, anchor: 'center' })
        .setLngLat([lonNum, latNum])
        .addTo(map);
      markersRef.current.push(marker);
    });
  }, [objects, theme]);

  const selectedVisualType = selectedObject
    ? getHorizonNodeVisualType(selectedObject.type, selectedObject.canonicalPath, selectedObject.label)
    : '';
  const selectedAccent = getNodeColors(
    selectedVisualType || selectedObject?.type || '',
    theme === 'dark' ? 'dark' : 'light',
  ).accent;
  const loading = loadingMap || loadingData;

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

        <div
          style={{
            position: 'absolute',
            left: 10,
            bottom: 10,
            padding: '6px 10px',
            borderRadius: 8,
            backgroundColor: 'rgba(5, 11, 17, 0.78)',
            border: '1px solid rgba(255,255,255,0.12)',
            color: 'var(--text-secondary-strong)',
            fontSize: 11,
          }}
        >
          Objects: {objects.length}
        </div>

        {selectedObject && (
          <div
            style={{
              position: 'absolute',
              left: 10,
              top: 10,
              maxWidth: 340,
              padding: '10px 12px',
              borderRadius: 10,
              backgroundColor: 'rgba(6, 14, 22, 0.9)',
              border: '1px solid rgba(255,255,255,0.14)',
              color: 'var(--text-heading)',
              boxShadow: '0 8px 30px rgba(0,0,0,0.35)',
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
