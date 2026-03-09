import { useEffect, useMemo, useRef, useState } from 'react';
import maplibregl from 'maplibre-gl';
import type { GeoObject } from './useGeoObjects';
import type { MaterializedArtifact } from '../components/SupplyChainOverlay';
import { STATIC_TRANSPORT_FLOW_DATA, type TransportFlowFeature } from '../lib/staticTransportFlows';

const SRC = 'path-legs-src';
const GLOW_LAYER = 'path-legs-glow';
const PINNED_LAYER = 'path-legs-pinned';
const DISCOVERED_LAYER = 'path-legs-discovered';
const BOTTLENECK_LAYER = 'path-legs-bottlenecks';

type ThemeMode = 'dark' | 'light';

export interface PathLegLayerStats {
  artifactCount: number;
  legCount: number;
  boundLegCount: number;
  bottleneckCount: number;
}

function canAccessMapStyle(map: maplibregl.Map): boolean {
  try {
    return Boolean((map as unknown as { style?: unknown }).style);
  } catch {
    return false;
  }
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function toFixedSafe(value: number | undefined, digits = 2): string {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(digits) : 'n/a';
}

function escapeHtml(input: string): string {
  return String(input || '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function pathLegColor(stressScore: number, theme: ThemeMode): string {
  if (stressScore >= 0.78) return theme === 'dark' ? '#fb7185' : '#dc2626';
  if (stressScore >= 0.58) return theme === 'dark' ? '#f59e0b' : '#b45309';
  if (stressScore >= 0.36) return theme === 'dark' ? '#38bdf8' : '#2563eb';
  return theme === 'dark' ? '#67e8f9' : '#0f766e';
}

function midpoint(coords: [number, number][]): [number, number] | null {
  if (coords.length === 0) return null;
  return coords[Math.floor(coords.length / 2)] || coords[0] || null;
}

function dedupeCoords(coords: [number, number][]): [number, number][] {
  const out: [number, number][] = [];
  for (const [lon, lat] of coords) {
    const prev = out[out.length - 1];
    if (prev && Math.abs(prev[0] - lon) < 1e-7 && Math.abs(prev[1] - lat) < 1e-7) continue;
    out.push([lon, lat]);
  }
  return out;
}

function lineOptionsFromFeature(feature: TransportFlowFeature | undefined): [number, number][][] {
  const geometry = feature?.geometry;
  if (!geometry) return [];
  if (geometry.type === 'LineString') {
    return [dedupeCoords((geometry.coordinates || []) as [number, number][])];
  }
  if (geometry.type === 'MultiLineString') {
    return ((geometry.coordinates || []) as [number, number][][]).map((line) => dedupeCoords(line));
  }
  return [];
}

function distanceSq(a: [number, number], b: [number, number]): number {
  const dx = a[0] - b[0];
  const dy = a[1] - b[1];
  return dx * dx + dy * dy;
}

function orientLineForEndpoints(line: [number, number][], start: [number, number], end: [number, number]): [number, number][] {
  if (line.length < 2) return line;
  const first = line[0];
  const last = line[line.length - 1];
  const direct = distanceSq(start, first) + distanceSq(end, last);
  const reverse = distanceSq(start, last) + distanceSq(end, first);
  return reverse < direct ? [...line].reverse() : line;
}

function maybeConnectEndpoints(line: [number, number][], fallback: [number, number][]): [number, number][] {
  if (line.length < 2 || fallback.length < 2) return line;
  const start = fallback[0];
  const end = fallback[fallback.length - 1];
  const oriented = orientLineForEndpoints(line, start, end);
  const out = [...oriented];
  if (distanceSq(start, out[0]) > 1e-7) {
    out.unshift(start);
  }
  if (distanceSq(end, out[out.length - 1]) > 1e-7) {
    out.push(end);
  }
  return dedupeCoords(out);
}

function selectFlowGeometry(
  flowRefs: string[],
  fallbackCoords: [number, number][],
  flowById: Map<string, TransportFlowFeature>,
): { coords: [number, number][]; flowId?: string; flowMode?: string; geometrySource: 'flow' | 'fragment'; snapped: boolean } {
  if (fallbackCoords.length < 2) {
    return { coords: fallbackCoords, geometrySource: 'fragment', snapped: false };
  }
  const start = fallbackCoords[0];
  const end = fallbackCoords[fallbackCoords.length - 1];
  let best: { coords: [number, number][]; score: number; flowId: string; flowMode?: string } | null = null;

  for (const ref of flowRefs) {
    const feature = flowById.get(ref);
    for (const option of lineOptionsFromFeature(feature)) {
      if (option.length < 2) continue;
      const oriented = orientLineForEndpoints(option, start, end);
      const score = distanceSq(start, oriented[0]) + distanceSq(end, oriented[oriented.length - 1]);
      if (!best || score < best.score) {
        best = {
          coords: maybeConnectEndpoints(oriented, fallbackCoords),
          score,
          flowId: ref,
          flowMode: feature?.properties?.flowMode ? String(feature.properties.flowMode) : undefined,
        };
      }
    }
  }

  if (!best) {
    return { coords: fallbackCoords, geometrySource: 'fragment', snapped: false };
  }
  return {
    coords: best.coords,
    flowId: best.flowId,
    flowMode: best.flowMode,
    geometrySource: 'flow',
    snapped: true,
  };
}

export function usePathLegLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  artifacts: MaterializedArtifact[],
  objects: GeoObject[],
  theme: ThemeMode,
  enabled: boolean,
) {
  const [stats, setStats] = useState<PathLegLayerStats>({
    artifactCount: 0,
    legCount: 0,
    boundLegCount: 0,
    bottleneckCount: 0,
  });
  const popupRef = useRef<maplibregl.Popup | null>(null);

  const data = useMemo(() => {
    const coordsByPath = new Map<string, { lon: number; lat: number }>();
    const flowById = new Map<string, TransportFlowFeature>();
    for (const obj of objects) {
      const cp = String(obj?.canonicalPath || '').trim();
      const lat = Number(obj?.coordinates?.lat);
      const lon = Number(obj?.coordinates?.lon);
      if (!cp || !Number.isFinite(lat) || !Number.isFinite(lon)) continue;
      coordsByPath.set(cp, { lon, lat });
    }
    for (const feature of STATIC_TRANSPORT_FLOW_DATA.features || []) {
      const id = String(feature?.id || '').trim();
      if (id) flowById.set(id, feature);
    }

    let legCount = 0;
    let boundLegCount = 0;
    let bottleneckCount = 0;

    const features: GeoJSON.Feature[] = [];
    for (const artifact of artifacts || []) {
      const displayName = String(artifact.displayName || artifact.id || 'Supply path').trim();
      const legs = Array.isArray(artifact.payload?.legs) ? artifact.payload.legs : [];
      const bottleneckIndex = artifact.payload?.stress?.bottleneckLegIndex;
      for (const leg of legs) {
        if (typeof leg?.index !== 'number') continue;
        const refs = [
          String(leg.supplierRef || '').trim(),
          ...((leg.transportViaRefs || []).map((ref) => String(ref || '').trim()).filter(Boolean)),
          String(leg.customerRef || '').trim(),
        ].filter(Boolean);
        const fallbackCoords = dedupeCoords(
          refs
            .map((ref) => coordsByPath.get(ref))
            .filter((point): point is { lon: number; lat: number } => Boolean(point))
            .map((point) => [point.lon, point.lat] as [number, number]),
        );
        const flowRefs = (leg.flowRefs || []).map((ref) => String(ref || '').trim()).filter(Boolean);
        const { coords, flowId, flowMode, geometrySource, snapped } = selectFlowGeometry(flowRefs, fallbackCoords, flowById);
        if (coords.length < 2) continue;
        legCount += 1;
        const zoneRefs = (leg.zoneRefs || []).map((ref) => String(ref || '').trim()).filter(Boolean);
        const affectedZoneRefs = (leg.affectedZoneRefs || []).map((ref) => String(ref || '').trim()).filter(Boolean);
        const spatialSemantics = (leg.spatialSemantics || []).map((item) => String(item || '').trim()).filter(Boolean);
        const dominantFactors = (leg.stress?.dominantFactors || []).map((item) => String(item || '').trim()).filter(Boolean);
        const stressScore = clamp(Number(leg.stress?.stressScore) || 0, 0, 1);
        const bindingConfidence = clamp(Number(leg.bindingConfidence) || 0, 0, 1);
        const isBound = flowRefs.length > 0 || zoneRefs.length > 0 || affectedZoneRefs.length > 0 || spatialSemantics.length > 0;
        if (isBound) boundLegCount += 1;
        const isBottleneck = typeof bottleneckIndex === 'number' && bottleneckIndex === leg.index;
        if (isBottleneck) bottleneckCount += 1;

        features.push({
          type: 'Feature',
          id: `${artifact.id}:leg:${leg.index}`,
          geometry: {
            type: 'LineString',
            coordinates: coords,
          },
          properties: {
            kind: 'leg',
            artifactId: artifact.id,
            displayName,
            legIndex: leg.index,
            role: String((leg as any)?.role || '').trim(),
            supplierRef: String(leg.supplierRef || '').trim(),
            customerRef: String(leg.customerRef || '').trim(),
            productRef: String(leg.productRef || '').trim(),
            flowRefs,
            flowGeometryId: flowId || '',
            flowMode: flowMode || '',
            geometrySource,
            snappedToFlow: snapped,
            zoneRefs,
            affectedZoneRefs,
            spatialSemantics,
            dominantFactors,
            discoverySource: String(artifact.discoverySource || '').trim(),
            discoveryReason: String(artifact.discoveryReason || '').trim(),
            traceRole: String(artifact.traceRole || '').trim(),
            isPinned: Boolean(artifact.isPinned),
            stressScore,
            fragility: Number(leg.stress?.fragility) || 0,
            rerouteability: Number(leg.stress?.rerouteability) || 0,
            zonePressure: Number(leg.stress?.zonePressure) || 0,
            affectedZonePressure: Number(leg.stress?.affectedZonePressure) || 0,
            recoveryLatencyHours: Number(leg.stress?.recoveryLatencyHours) || 0,
            bindingConfidence,
            color: pathLegColor(stressScore, theme),
            lineWidth: 2 + stressScore * 3.4 + (isBottleneck ? 1.2 : 0),
            lineOpacity: clamp(0.24 + bindingConfidence * 0.42 + stressScore * 0.28, 0.22, 0.96),
            glowOpacity: clamp(0.06 + stressScore * 0.2, 0.06, 0.28),
            isBottleneck,
          },
        } as GeoJSON.Feature);

        if (isBottleneck) {
          const focusPoint = midpoint(coords);
          if (focusPoint) {
            features.push({
              type: 'Feature',
              id: `${artifact.id}:leg:${leg.index}:bottleneck`,
              geometry: {
                type: 'Point',
                coordinates: focusPoint,
              },
              properties: {
                kind: 'bottleneck',
                artifactId: artifact.id,
                displayName,
                legIndex: leg.index,
                stressScore,
                dominantFactors,
                bindingConfidence,
                color: pathLegColor(Math.max(stressScore, 0.6), theme),
              },
            } as GeoJSON.Feature);
          }
        }
      }
    }

    return {
      featureCollection: {
        type: 'FeatureCollection',
        features,
      } as GeoJSON.FeatureCollection,
      stats: {
        artifactCount: artifacts.length,
        legCount,
        boundLegCount,
        bottleneckCount,
      } satisfies PathLegLayerStats,
    };
  }, [artifacts, objects, theme]);

  useEffect(() => {
    setStats(data.stats);
  }, [data]);

  useEffect(() => {
    if (!map || !isReady || !canAccessMapStyle(map)) return;

    if (!map.getSource(SRC)) {
      map.addSource(SRC, {
        type: 'geojson',
        data: data.featureCollection as any,
      });
    }

    if (!map.getLayer(GLOW_LAYER)) {
      map.addLayer({
        id: GLOW_LAYER,
        type: 'line',
        source: SRC,
        filter: ['==', ['geometry-type'], 'LineString'],
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['+', ['get', 'lineWidth'], 4],
          'line-opacity': ['case', ['boolean', ['get', 'isBottleneck'], false], ['*', ['get', 'glowOpacity'], 1.25], ['get', 'glowOpacity']],
          'line-blur': 1.3,
        },
      });
    }

    if (!map.getLayer(PINNED_LAYER)) {
      map.addLayer({
        id: PINNED_LAYER,
        type: 'line',
        source: SRC,
        filter: ['all', ['==', ['geometry-type'], 'LineString'], ['==', ['get', 'isPinned'], true]],
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
        },
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['get', 'lineWidth'],
          'line-opacity': ['get', 'lineOpacity'],
          'line-dasharray': [
            'case',
            ['<', ['get', 'bindingConfidence'], 0.38],
            ['literal', [1.6, 1.6]],
            ['<', ['get', 'bindingConfidence'], 0.62],
            ['literal', [2.6, 1.4]],
            ['literal', [1, 0]],
          ],
        },
      });
    }

    if (!map.getLayer(DISCOVERED_LAYER)) {
      map.addLayer({
        id: DISCOVERED_LAYER,
        type: 'line',
        source: SRC,
        filter: ['all', ['==', ['geometry-type'], 'LineString'], ['!=', ['get', 'isPinned'], true]],
        layout: {
          'line-cap': 'round',
          'line-join': 'round',
        },
        paint: {
          'line-color': ['get', 'color'],
          'line-width': ['*', ['get', 'lineWidth'], 0.92],
          'line-opacity': ['*', ['get', 'lineOpacity'], 0.88],
          'line-dasharray': [
            'case',
            ['<', ['get', 'bindingConfidence'], 0.38],
            ['literal', [0.9, 2.6]],
            ['<', ['get', 'bindingConfidence'], 0.62],
            ['literal', [1.6, 2.2]],
            ['literal', [3.1, 1.8]],
          ],
        },
      });
    }

    if (!map.getLayer(BOTTLENECK_LAYER)) {
      map.addLayer({
        id: BOTTLENECK_LAYER,
        type: 'circle',
        source: SRC,
        filter: ['all', ['==', ['geometry-type'], 'Point'], ['==', ['get', 'kind'], 'bottleneck']],
        paint: {
          'circle-radius': ['+', 5, ['*', ['get', 'stressScore'], 7]],
          'circle-color': ['get', 'color'],
          'circle-stroke-color': theme === 'dark' ? '#f8fafc' : '#0f172a',
          'circle-stroke-width': 1.4,
          'circle-opacity': 0.9,
        },
      });
    }

    const src = map.getSource(SRC) as maplibregl.GeoJSONSource | undefined;
    src?.setData(data.featureCollection as any);

    const visible = enabled && data.stats.legCount > 0;
    for (const layer of [GLOW_LAYER, PINNED_LAYER, DISCOVERED_LAYER, BOTTLENECK_LAYER]) {
      if (map.getLayer(layer)) {
        map.setLayoutProperty(layer, 'visibility', visible ? 'visible' : 'none');
      }
    }
  }, [map, isReady, styleEpoch, enabled, theme, data]);

  useEffect(() => {
    if (!map || !isReady || !canAccessMapStyle(map)) return;
    const onEnter = () => {
      map.getCanvas().style.cursor = 'pointer';
    };
    const onLeave = () => {
      map.getCanvas().style.cursor = '';
      popupRef.current?.remove();
      popupRef.current = null;
    };
    const onClick = (event: maplibregl.MapLayerMouseEvent) => {
      const feature = event.features?.[0];
      const props = (feature?.properties || {}) as Record<string, any>;
      if (!feature || props.kind !== 'leg') return;
      popupRef.current?.remove();
      const parseList = (value: unknown): string[] => {
        if (Array.isArray(value)) return value.map((item) => String(item || '').trim()).filter(Boolean);
        if (typeof value !== 'string') return [];
        const trimmed = value.trim();
        if (!trimmed) return [];
        if (trimmed.startsWith('[')) {
          try {
            const parsed = JSON.parse(trimmed);
            return Array.isArray(parsed) ? parsed.map((item) => String(item || '').trim()).filter(Boolean) : [];
          } catch {
            return [];
          }
        }
        return trimmed.split(',').map((item) => item.trim()).filter(Boolean);
      };
      const dominantFactors = parseList(props.dominantFactors);
      const flowRefs = parseList(props.flowRefs);
      const zoneRefs = parseList(props.zoneRefs);
      const affectedZoneRefs = parseList(props.affectedZoneRefs);
      const spatialSemantics = parseList(props.spatialSemantics);
      const discoveryReason = String(props.discoveryReason || '').trim();
      const traceRole = String(props.traceRole || '').trim();
      const discoverySource = String(props.discoverySource || '').trim();

      const html = `
        <div style="display:flex;flex-direction:column;gap:6px;min-width:240px;max-width:320px;">
          <div style="font-size:12px;font-weight:800;">${escapeHtml(String(props.displayName || 'Supply path'))}</div>
          <div style="font-size:11px;opacity:0.78;">${props.isPinned ? 'Pinned path' : 'Discovered path'} • Leg ${escapeHtml(String(props.legIndex ?? ''))}${props.isBottleneck ? ' • bottleneck' : ''}</div>
          <div style="font-size:11px;display:flex;flex-wrap:wrap;gap:6px;">
            <span>stress ${escapeHtml(toFixedSafe(Number(props.stressScore), 2))}</span>
            <span>bind ${escapeHtml(toFixedSafe(Number(props.bindingConfidence) * 100, 0))}%</span>
            <span>recovery ${escapeHtml(toFixedSafe(Number(props.recoveryLatencyHours), 0))}h</span>
          </div>
          ${traceRole ? `<div style="font-size:11px;"><strong>Trace role:</strong> ${escapeHtml(traceRole.replace(/_/g, ' '))}</div>` : ''}
          ${discoveryReason ? `<div style="font-size:11px;"><strong>Why shown:</strong> ${escapeHtml(discoveryReason)}</div>` : ''}
          ${!discoveryReason && discoverySource ? `<div style="font-size:11px;"><strong>Source:</strong> ${escapeHtml(discoverySource)}</div>` : ''}
          ${typeof props.flowMode === 'string' && props.flowMode ? `<div style="font-size:11px;"><strong>Mode:</strong> ${escapeHtml(String(props.flowMode))}${props.flowGeometryId ? ` • ${escapeHtml(String(props.flowGeometryId))}` : ''}</div>` : ''}
          ${typeof props.geometrySource === 'string' && props.geometrySource ? `<div style="font-size:11px;"><strong>Geometry:</strong> ${escapeHtml(String(props.geometrySource))}${props.snappedToFlow ? ' • snapped' : ''}</div>` : ''}
          ${spatialSemantics.length > 0 ? `<div style="font-size:11px;"><strong>Spatial:</strong> ${escapeHtml(spatialSemantics.join(', '))}</div>` : ''}
          ${flowRefs.length > 0 ? `<div style="font-size:11px;"><strong>Flows:</strong> ${escapeHtml(flowRefs.join(', '))}</div>` : ''}
          ${zoneRefs.length > 0 ? `<div style="font-size:11px;"><strong>Zones:</strong> ${escapeHtml(zoneRefs.join(', '))}</div>` : ''}
          ${affectedZoneRefs.length > 0 ? `<div style="font-size:11px;"><strong>Affected:</strong> ${escapeHtml(affectedZoneRefs.join(', '))}</div>` : ''}
          ${dominantFactors.length > 0 ? `<div style="font-size:11px;"><strong>Factors:</strong> ${escapeHtml(dominantFactors.join(', '))}</div>` : ''}
        </div>
      `;
      popupRef.current = new maplibregl.Popup({
        closeButton: false,
        closeOnClick: false,
        offset: 12,
      })
        .setLngLat(event.lngLat)
        .setHTML(html)
        .addTo(map);
    };

    map.on('mouseenter', PINNED_LAYER, onEnter);
    map.on('mouseleave', PINNED_LAYER, onLeave);
    map.on('click', PINNED_LAYER, onClick);
    map.on('mouseenter', DISCOVERED_LAYER, onEnter);
    map.on('mouseleave', DISCOVERED_LAYER, onLeave);
    map.on('click', DISCOVERED_LAYER, onClick);
    return () => {
      map.off('mouseenter', PINNED_LAYER, onEnter);
      map.off('mouseleave', PINNED_LAYER, onLeave);
      map.off('click', PINNED_LAYER, onClick);
      map.off('mouseenter', DISCOVERED_LAYER, onEnter);
      map.off('mouseleave', DISCOVERED_LAYER, onLeave);
      map.off('click', DISCOVERED_LAYER, onClick);
      popupRef.current?.remove();
      popupRef.current = null;
    };
  }, [map, isReady, styleEpoch]);

  return stats;
}
