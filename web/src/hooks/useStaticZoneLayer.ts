import { useEffect, useRef } from 'react';
import maplibregl from 'maplibre-gl';
import { formatZoneType, zoneTypeColor, type StaticZoneFeature, type StaticZoneType } from '../lib/staticZones';

const SRC = 'static-zones-src';
const FILL_LAYER = 'static-zones-fill';
const OUTLINE_LAYER = 'static-zones-outline';

interface ZoneRenderProperties {
  id: string;
  name: string;
  countryName: string;
  zoneType: StaticZoneType;
  zoneSubType: string;
  tagsText: string;
  euActsText: string;
  color: string;
  hype: number;
  status: string;
  fillOpacity: number;
  lineOpacity: number;
  lineWidth: number;
  minZoom?: number;
  maxZoom?: number;
}

interface ZoneActivitySnapshot {
  hype?: number;
  status?: 'hot' | 'active' | 'watch' | 'cooling' | 'inactive';
}

const POPUP_STYLE_ID = 'static-zones-popup-style';

function ensurePopupStyles(): void {
  if (typeof document === 'undefined') return;
  if (document.getElementById(POPUP_STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = POPUP_STYLE_ID;
  style.textContent = `
.static-zones-popup-dark .maplibregl-popup-content {
  background: rgba(8, 14, 24, 0.96);
  color: #e2e8f0;
  border: 1px solid rgba(148, 163, 184, 0.35);
  border-radius: 10px;
  box-shadow: 0 14px 34px rgba(2, 6, 23, 0.6);
  padding: 10px 12px;
}
.static-zones-popup-light .maplibregl-popup-content {
  background: rgba(255, 255, 255, 0.97);
  color: #0f172a;
  border: 1px solid rgba(30, 41, 59, 0.2);
  border-radius: 10px;
  box-shadow: 0 12px 28px rgba(15, 23, 42, 0.2);
  padding: 10px 12px;
}
.static-zones-popup-dark .maplibregl-popup-close-button {
  color: rgba(226, 232, 240, 0.9);
}
.static-zones-popup-light .maplibregl-popup-close-button {
  color: rgba(15, 23, 42, 0.75);
}
.static-zones-popup-dark.maplibregl-popup-anchor-top .maplibregl-popup-tip,
.static-zones-popup-dark.maplibregl-popup-anchor-top-left .maplibregl-popup-tip,
.static-zones-popup-dark.maplibregl-popup-anchor-top-right .maplibregl-popup-tip {
  border-bottom-color: rgba(8, 14, 24, 0.96);
}
.static-zones-popup-dark.maplibregl-popup-anchor-bottom .maplibregl-popup-tip,
.static-zones-popup-dark.maplibregl-popup-anchor-bottom-left .maplibregl-popup-tip,
.static-zones-popup-dark.maplibregl-popup-anchor-bottom-right .maplibregl-popup-tip {
  border-top-color: rgba(8, 14, 24, 0.96);
}
.static-zones-popup-dark.maplibregl-popup-anchor-left .maplibregl-popup-tip {
  border-right-color: rgba(8, 14, 24, 0.96);
}
.static-zones-popup-dark.maplibregl-popup-anchor-right .maplibregl-popup-tip {
  border-left-color: rgba(8, 14, 24, 0.96);
}
.static-zones-popup-light.maplibregl-popup-anchor-top .maplibregl-popup-tip,
.static-zones-popup-light.maplibregl-popup-anchor-top-left .maplibregl-popup-tip,
.static-zones-popup-light.maplibregl-popup-anchor-top-right .maplibregl-popup-tip {
  border-bottom-color: rgba(255, 255, 255, 0.97);
}
.static-zones-popup-light.maplibregl-popup-anchor-bottom .maplibregl-popup-tip,
.static-zones-popup-light.maplibregl-popup-anchor-bottom-left .maplibregl-popup-tip,
.static-zones-popup-light.maplibregl-popup-anchor-bottom-right .maplibregl-popup-tip {
  border-top-color: rgba(255, 255, 255, 0.97);
}
.static-zones-popup-light.maplibregl-popup-anchor-left .maplibregl-popup-tip {
  border-right-color: rgba(255, 255, 255, 0.97);
}
.static-zones-popup-light.maplibregl-popup-anchor-right .maplibregl-popup-tip {
  border-left-color: rgba(255, 255, 255, 0.97);
}
`;
  document.head.appendChild(style);
}

function escapeHtml(input: string): string {
  return input
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function toEnglishZoneNarrative(props: ZoneRenderProperties): string {
  const byType: Record<StaticZoneType, string> = {
    industrial_cluster: 'Manufacturing capacity and supplier density are concentrated in this area.',
    energy_hub: 'Energy processing, storage, and export infrastructure are concentrated in this area.',
    logistics_chokepoint: 'Freight dependency is concentrated around port, rail, and road transfer points.',
    special_economic_zone: 'Export-oriented operations are concentrated under preferential regulatory conditions.',
    resource_extraction: 'Primary resource extraction and first-stage processing are concentrated here.',
    military_industrial: 'Defense-adjacent industrial assets and strategic production are concentrated here.',
    data_infrastructure: 'High-tech operations and critical digital infrastructure are concentrated here.',
    climate_exposure: 'Production and logistics here are sensitive to recurring climate stress.',
  };

  const zoneType = props.zoneType;
  const base = byType[zoneType] || byType.industrial_cluster;
  const subtype = props.zoneSubType ? ` Focus: ${props.zoneSubType.replace(/_/g, ' ')}.` : '';
  const tags = props.tagsText ? ` Signals: ${props.tagsText}.` : '';
  const policy = props.euActsText ? ` Policy context: ${props.euActsText}.` : '';
  return `${base}${subtype}${tags}${policy}`;
}

function normalizeZoneType(input: string): StaticZoneType {
  const key = (input || '').trim() as StaticZoneType;
  if (
    key === 'industrial_cluster' ||
    key === 'energy_hub' ||
    key === 'logistics_chokepoint' ||
    key === 'special_economic_zone' ||
    key === 'resource_extraction' ||
    key === 'military_industrial' ||
    key === 'data_infrastructure' ||
    key === 'climate_exposure'
  ) {
    return key;
  }
  return 'industrial_cluster';
}

function statusTint(status: string, fallback: string): string {
  switch (status) {
    case 'hot': return '#ef4444';
    case 'active': return '#f97316';
    case 'watch': return '#f59e0b';
    case 'cooling': return '#22c55e';
    default: return fallback;
  }
}

function buildZoneCollection(
  features: StaticZoneFeature[],
  theme: 'dark' | 'light',
  activityById: Record<string, ZoneActivitySnapshot>,
): GeoJSON.FeatureCollection {
  const mapped: GeoJSON.Feature[] = [];
  for (const feature of features) {
    if (!feature?.geometry || !feature?.properties) continue;
    const id = String(feature.id || feature.properties.name);
    const activity = activityById[id] || {};
    const hype = Math.max(0, Math.min(100, Number(activity.hype || 0)));
    const status = String(activity.status || 'inactive');
    const baseColor = zoneTypeColor(feature.properties.zoneType, theme);
    const color = statusTint(status, baseColor);
    const fillOpacity = Math.min(0.56, 0.12 + hype * 0.0042);
    const lineOpacity = Math.min(0.95, 0.36 + hype * 0.0052);
    const lineWidth = Math.min(3.2, 1.1 + hype * 0.018);
    const props: ZoneRenderProperties = {
      id,
      name: String(feature.properties.name || 'Unnamed zone'),
      countryName: String(feature.properties.countryName || ''),
      zoneType: feature.properties.zoneType as StaticZoneType,
      zoneSubType: String(feature.properties.zoneSubType || ''),
      tagsText: Array.isArray(feature.properties.tags)
        ? feature.properties.tags.map((x) => String(x)).slice(0, 4).join(', ')
        : '',
      euActsText: Array.isArray(feature.properties.euActs)
        ? feature.properties.euActs.map((x) => String(x)).slice(0, 3).join(' | ')
        : '',
      color,
      hype,
      status,
      fillOpacity,
      lineOpacity,
      lineWidth,
      minZoom: typeof feature.properties.minZoom === 'number' ? feature.properties.minZoom : undefined,
      maxZoom: typeof feature.properties.maxZoom === 'number' ? feature.properties.maxZoom : undefined,
    };
    mapped.push({
      type: 'Feature',
      id: props.id,
      geometry: feature.geometry,
      properties: props as unknown as GeoJSON.GeoJsonProperties,
    });
  }
  return { type: 'FeatureCollection', features: mapped };
}

export function useStaticZoneLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  features: StaticZoneFeature[],
  theme: 'dark' | 'light',
  visible: boolean,
  activityById: Record<string, ZoneActivitySnapshot>,
) {
  const popupRef = useRef<maplibregl.Popup | null>(null);

  useEffect(() => {
    if (!map || !isReady) return;
    ensurePopupStyles();

    const initial = buildZoneCollection([], theme, {});
    map.addSource(SRC, { type: 'geojson', data: initial });
    const beforeId = map.getLayer('geo-clusters') ? 'geo-clusters' : undefined;

    map.addLayer({
      id: FILL_LAYER,
      type: 'fill',
      source: SRC,
      layout: {
        visibility: visible ? 'visible' : 'none',
      },
      paint: {
        'fill-color': ['get', 'color'],
        'fill-opacity': ['coalesce', ['get', 'fillOpacity'], 0.18],
      },
    }, beforeId);

    map.addLayer({
      id: OUTLINE_LAYER,
      type: 'line',
      source: SRC,
      layout: {
        visibility: visible ? 'visible' : 'none',
      },
      paint: {
        'line-color': ['get', 'color'],
        'line-width': ['coalesce', ['get', 'lineWidth'], 1.35],
        'line-opacity': ['coalesce', ['get', 'lineOpacity'], 0.7],
      },
    }, beforeId);

    const handleMoveEnter = () => {
      map.getCanvas().style.cursor = 'pointer';
    };

    const handleMoveLeave = () => {
      map.getCanvas().style.cursor = '';
    };

    const handleClick = (e: maplibregl.MapMouseEvent & { features?: maplibregl.MapGeoJSONFeature[] }) => {
      try {
        const feature = e.features?.[0];
        if (!feature?.properties) return;
        const raw = feature.properties as Record<string, unknown>;
        const props: ZoneRenderProperties = {
          id: String(raw.id || feature.id || ''),
          name: String(raw.name || 'Zone'),
          countryName: String(raw.countryName || ''),
          zoneType: normalizeZoneType(String(raw.zoneType || '')),
          zoneSubType: String(raw.zoneSubType || ''),
          tagsText: String(raw.tagsText || ''),
          euActsText: String(raw.euActsText || ''),
          color: String(raw.color || ''),
          hype: Number(raw.hype || 0),
          status: String(raw.status || 'inactive'),
          fillOpacity: Number(raw.fillOpacity || 0.18),
          lineOpacity: Number(raw.lineOpacity || 0.7),
          lineWidth: Number(raw.lineWidth || 1.35),
        };
        const title = escapeHtml(props.name);
        const subtitle = escapeHtml(props.countryName);
        const zoneType = escapeHtml(formatZoneType(props.zoneType));
        const text = escapeHtml(toEnglishZoneNarrative(props));
        const hype = `${Math.round(props.hype)}/100`;
        const status = escapeHtml(props.status);

        popupRef.current?.remove();
        const popup = new maplibregl.Popup({
          closeButton: true,
          closeOnClick: true,
          maxWidth: '320px',
          className: `static-zones-popup ${theme === 'dark' ? 'static-zones-popup-dark' : 'static-zones-popup-light'}`,
        })
          .setLngLat(e.lngLat)
          .setHTML(
            `<div style="font-family: ui-sans-serif,system-ui,sans-serif; font-size:12px; line-height:1.4;">` +
              `<div style="font-weight:700; margin-bottom:2px;">${title}</div>` +
              `<div style="font-size:11px; opacity:.8; margin-bottom:6px;">${subtitle}</div>` +
              `<div style="font-size:11px; margin-bottom:6px;">${text}</div>` +
              `<div style="font-size:10px; margin-bottom:4px; opacity:.9;">Hype: ${hype} • Status: ${status}</div>` +
              `<div style="font-size:10px; opacity:.75;">${zoneType}</div>` +
            `</div>`
          )
          .addTo(map);
        popupRef.current = popup;
      } catch (err) {
        console.warn('[useStaticZoneLayer] popup render failed:', err);
      }
    };

    map.on('mouseenter', FILL_LAYER, handleMoveEnter);
    map.on('mouseleave', FILL_LAYER, handleMoveLeave);
    map.on('click', FILL_LAYER, handleClick);

    return () => {
      popupRef.current?.remove();
      popupRef.current = null;
      try {
        map.off('mouseenter', FILL_LAYER, handleMoveEnter);
        map.off('mouseleave', FILL_LAYER, handleMoveLeave);
        map.off('click', FILL_LAYER, handleClick);
        if (map.getLayer(OUTLINE_LAYER)) map.removeLayer(OUTLINE_LAYER);
        if (map.getLayer(FILL_LAYER)) map.removeLayer(FILL_LAYER);
        if (map.getSource(SRC)) map.removeSource(SRC);
      } catch {
        // map disposed
      }
    };
  }, [map, isReady, styleEpoch]);

  useEffect(() => {
    if (!map || !isReady) return;
    const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
    if (!src) return;
    src.setData(buildZoneCollection(features, theme, activityById || {}));

    const visibility = visible ? 'visible' : 'none';
    if (map.getLayer(FILL_LAYER)) map.setLayoutProperty(FILL_LAYER, 'visibility', visibility);
    if (map.getLayer(OUTLINE_LAYER)) map.setLayoutProperty(OUTLINE_LAYER, 'visibility', visibility);
  }, [map, isReady, features, theme, visible, activityById]);
}
