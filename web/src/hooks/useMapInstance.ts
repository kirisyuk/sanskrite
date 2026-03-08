import { useRef, useState, useEffect, useContext, useCallback } from 'react';
import maplibregl from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { ThemeContext } from '../components/ThemeProvider';

export const MAP_GLYPHS_URL = 'https://demotiles.maplibre.org/font/{fontstack}/{range}.pbf';
export const MAP_TEXT_FONT_STACK = ['Open Sans Regular', 'Arial Unicode MS Regular'] as const;

function buildCartoStyle(theme: 'dark' | 'light'): maplibregl.StyleSpecification {
  const isDark = theme === 'dark';
  const variant = isDark ? 'dark_all' : 'light_all';
  const bgColor = isDark ? '#0a0f0c' : '#f8fafc';
  return {
    version: 8,
    name: isDark ? 'Worldmonitor Dark' : 'Worldmonitor Light',
    sources: {
      'carto-basemap': {
        type: 'raster',
        tiles: [
          `https://a.basemaps.cartocdn.com/${variant}/{z}/{x}/{y}@2x.png`,
          `https://b.basemaps.cartocdn.com/${variant}/{z}/{x}/{y}@2x.png`,
          `https://c.basemaps.cartocdn.com/${variant}/{z}/{x}/{y}@2x.png`,
        ],
        tileSize: 256,
        attribution: '&copy; <a href="https://carto.com/">CARTO</a>',
      },
    },
    layers: [
      { id: 'background', type: 'background', paint: { 'background-color': bgColor } },
      { id: 'carto-basemap-layer', type: 'raster', source: 'carto-basemap', minzoom: 0, maxzoom: 22 },
    ],
    glyphs: MAP_GLYPHS_URL,
  };
}

/**
 * `styleEpoch` is a monotonic counter that increments every time the map style
 * finishes loading (initial load + every setStyle hot-swap). Downstream hooks
 * should include it in their dependency arrays so they re-add sources/layers
 * after a style reload wipes the canvas.
 */
export function useMapInstance(containerRef: React.RefObject<HTMLDivElement | null>) {
  const { theme } = useContext(ThemeContext);
  const mapRef = useRef<maplibregl.Map | null>(null);
  const initialThemeRef = useRef<string | null>(theme);
  const [styleEpoch, setStyleEpoch] = useState(0);
  const [styleReady, setStyleReady] = useState(false);
  const isReady = styleReady && styleEpoch > 0;

  const onStyleLoad = useCallback(() => {
    setStyleReady(true);
    setStyleEpoch((n) => n + 1);
  }, []);

  useEffect(() => {
    const node = containerRef.current;
    if (!node) return;

    const map = new maplibregl.Map({
      container: node,
      style: buildCartoStyle(theme === 'dark' ? 'dark' : 'light'),
      center: [15, 20],
      zoom: 1.5,
      minZoom: 1,
      maxZoom: 14,
      attributionControl: false,
      dragRotate: false,
      touchPitch: false,
      pitchWithRotate: false,
      renderWorldCopies: true,
    });
    mapRef.current = map;
    map.addControl(new maplibregl.NavigationControl({ visualizePitch: false }), 'top-right');
    map.addControl(new maplibregl.AttributionControl({ compact: true }), 'bottom-left');

    map.on('style.load', onStyleLoad);

    return () => {
      map.off('style.load', onStyleLoad);
      map.remove();
      mapRef.current = null;
      setStyleReady(false);
      setStyleEpoch(0);
    };
  }, []);

  // Hot-swap theme without destroying the map.
  // Skip the first run — the map was just created with this theme.
  useEffect(() => {
    const map = mapRef.current;
    if (!map || !isReady) return;
    if (initialThemeRef.current === theme) {
      initialThemeRef.current = null;
      return;
    }
    initialThemeRef.current = null;
    setStyleReady(false);
    map.setStyle(buildCartoStyle(theme === 'dark' ? 'dark' : 'light'), { diff: false });
  }, [theme]);

  return {
    map: mapRef.current,
    isReady,
    styleEpoch,
    theme: theme === 'dark' ? 'dark' as const : 'light' as const,
  };
}
