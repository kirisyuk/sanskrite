import { useEffect, useMemo, useState } from 'react';
import maplibregl from 'maplibre-gl';
import type { GeoObject } from './useGeoObjects';
import type { MaterializedArtifact } from '../components/SupplyChainOverlay';
import type { PathLegVisibilityPolicy } from './usePathLegLayer';
import { STATIC_TRANSPORT_FLOW_DATA, type TransportFlowFeature } from '../lib/staticTransportFlows';
import { seedRouteFragmentPolylinesIntoFlowMap } from '../lib/seedRouteFragmentFlowById';
import {
  buildPointFeature,
  ensureMarkerImages,
  GEO_MARKER_CLUSTER_MAX_ZOOM,
} from './useGeoMarkerLayer';

type ThemeMode = 'dark' | 'light';

const MEMBER_CLUSTER_SRC = 'path-leg-cluster-members-src';
const MEMBER_CLUSTER_RADIUS = 40;
const MEMBER_CLUSTER_LAYER = 'path-leg-cluster-members-clusters';
const MEMBER_POINT_LAYER = 'path-leg-cluster-members-points';

export const PATH_LEG_CLUSTER_INTERACTIVE_LAYERS = [] as const;

type LegStatus = 'validated' | 'heuristic' | 'forensic_gap';

type NormalizedLeg = {
  id: string;
  artifactId: string;
  artifactFamilyId: string;
  sourceRef: string;
  targetRef: string;
  focusBoost: number;
  bounds: [number, number, number, number];
};

type MarkerClusterMembership = {
  clusterKey: string;
  clusterId: number;
  centroid: [number, number];
  pointCount: number;
  expansionZoom: number | null;
};

type MarkerClusterSnapshot = {
  visibleObjectIds: Set<string>;
  visibleCanonicalPaths: Set<string>;
  membershipByObjectId: Map<string, MarkerClusterMembership>;
  membershipByCanonicalPath: Map<string, MarkerClusterMembership>;
};

export interface PathLegClusterStats {
  artifactCount: number;
  legCount: number;
  supernodeCount: number;
  superedgeCount: number;
  rawVisibilityWeight: number;
}

export interface PathLegClusterLayerState {
  stats: PathLegClusterStats;
  visibilityPolicy: PathLegVisibilityPolicy;
}

interface PathLegClusterLayerOptions {
  selectedObject?: GeoObject | null;
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function smoothstep(value: number, edge0: number, edge1: number): number {
  if (edge0 === edge1) return value >= edge1 ? 1 : 0;
  const t = clamp((value - edge0) / (edge1 - edge0), 0, 1);
  return t * t * (3 - 2 * t);
}

function canAccessMapStyle(map: maplibregl.Map): boolean {
  try {
    return Boolean((map as unknown as { style?: unknown }).style);
  } catch {
    return false;
  }
}

function artifactLegField<T = unknown>(leg: any, ...keys: string[]): T | undefined {
  for (const key of keys) {
    if (leg && leg[key] != null) return leg[key] as T;
  }
  return undefined;
}

function artifactLegString(leg: any, ...keys: string[]): string {
  return String(artifactLegField(leg, ...keys) || '').trim();
}

function artifactLegStringList(leg: any, ...keys: string[]): string[] {
  const value = artifactLegField<unknown>(leg, ...keys);
  if (!Array.isArray(value)) return [];
  return value.map((item) => String(item || '').trim()).filter(Boolean);
}

function artifactLegCoordinateList(leg: any, ...keys: string[]): [number, number][] {
  const value = artifactLegField<unknown>(leg, ...keys);
  if (!Array.isArray(value)) return [];
  return dedupeCoords(
    value
      .map((item) => {
        const lat = Number((item as any)?.lat);
        const lon = Number((item as any)?.lon);
        return Number.isFinite(lat) && Number.isFinite(lon) ? [lon, lat] as [number, number] : null;
      })
      .filter((item): item is [number, number] => Boolean(item)),
  );
}

function isAbstractRailNetworkRef(ref: string): boolean {
  const normalized = String(ref || '').trim().toLowerCase();
  return (
    normalized.endsWith('-rail-network') ||
    normalized.includes('/rail-network') ||
    normalized.endsWith('-distribution-network') ||
    normalized.includes('/distribution-network')
  );
}

function uniqueLegStrings(values: string[]): string[] {
  return Array.from(new Set(values.map((value) => String(value || '').trim()).filter(Boolean)));
}

function collapseAbstractRailNetworkLegs(legs: any[]): any[] {
  const out: any[] = [];
  for (let idx = 0; idx < legs.length; idx += 1) {
    const current = legs[idx];
    const currentCustomer = artifactLegString(current, 'customerRef', 'CustomerRef');
    if (idx + 1 < legs.length && isAbstractRailNetworkRef(currentCustomer)) {
      const next = legs[idx + 1];
      const nextSupplier = artifactLegString(next, 'supplierRef', 'SupplierRef');
      const sameProduct = artifactLegString(current, 'productRef', 'ProductRef') === artifactLegString(next, 'productRef', 'ProductRef');
      if (currentCustomer && currentCustomer === nextSupplier && sameProduct) {
        out.push({
          ...current,
          customerRef: artifactLegString(next, 'customerRef', 'CustomerRef'),
          CustomerRef: artifactLegString(next, 'customerRef', 'CustomerRef'),
          transportViaRefs: uniqueLegStrings([
            ...artifactLegStringList(current, 'transportViaRefs', 'TransportViaRefs'),
            ...artifactLegStringList(next, 'transportViaRefs', 'TransportViaRefs'),
          ].filter((ref) => !isAbstractRailNetworkRef(ref))),
          TransportViaRefs: uniqueLegStrings([
            ...artifactLegStringList(current, 'transportViaRefs', 'TransportViaRefs'),
            ...artifactLegStringList(next, 'transportViaRefs', 'TransportViaRefs'),
          ].filter((ref) => !isAbstractRailNetworkRef(ref))),
          flowRefs: uniqueLegStrings([
            ...artifactLegStringList(current, 'flowRefs', 'FlowRefs'),
            ...artifactLegStringList(next, 'flowRefs', 'FlowRefs'),
          ]),
          FlowRefs: uniqueLegStrings([
            ...artifactLegStringList(current, 'flowRefs', 'FlowRefs'),
            ...artifactLegStringList(next, 'flowRefs', 'FlowRefs'),
          ]),
          routeCoordinates: [],
          RouteCoordinates: [],
          routeGeometrySource: '',
          RouteGeometrySource: '',
          mode: artifactLegString(current, 'mode', 'Mode') || artifactLegString(next, 'mode', 'Mode'),
          Mode: artifactLegString(current, 'mode', 'Mode') || artifactLegString(next, 'mode', 'Mode'),
        });
        idx += 1;
        continue;
      }
    }
    out.push(current);
  }
  return out;
}

function artifactLegNumber(leg: any, ...keys: string[]): number | null {
  const value = artifactLegField<unknown>(leg, ...keys);
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim()) {
    const n = Number(value);
    if (Number.isFinite(n)) return n;
  }
  return null;
}

function resolvedArtifactLegIndex(leg: any, arrayIndex: number): number {
  const fromField = artifactLegNumber(leg, 'index', 'Index');
  if (fromField != null) return fromField;
  return arrayIndex;
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

function distanceSq(a: [number, number], b: [number, number]): number {
  const dx = a[0] - b[0];
  const dy = a[1] - b[1];
  return dx * dx + dy * dy;
}

function approximateDistanceKm(lon1: number, lat1: number, lon2: number, lat2: number): number {
  const toRad = (deg: number) => (deg * Math.PI) / 180;
  const dLat = toRad(lat2 - lat1);
  const dLon = toRad(lon2 - lon1);
  const a =
    Math.sin(dLat / 2) ** 2
    + Math.cos(toRad(lat1)) * Math.cos(toRad(lat2)) * Math.sin(dLon / 2) ** 2;
  return 6371 * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
}

function orientLineForEndpoints(line: [number, number][], start: [number, number], end: [number, number]): [number, number][] {
  if (line.length < 2) return line;
  const first = line[0];
  const last = line[line.length - 1];
  const direct = distanceSq(start, first) + distanceSq(end, last);
  const reverse = distanceSq(start, last) + distanceSq(end, first);
  return reverse < direct ? [...line].reverse() : line;
}

const MAX_FLOW_ENDPOINT_CONNECT_KM = 420;

function maybeConnectEndpoints(line: [number, number][], fallback: [number, number][]): [number, number][] | null {
  if (line.length < 2 || fallback.length < 2) return line;
  const start = fallback[0];
  const end = fallback[fallback.length - 1];
  const oriented = orientLineForEndpoints(line, start, end);
  const startGapKm = approximateDistanceKm(start[0], start[1], oriented[0][0], oriented[0][1]);
  const endGapKm = approximateDistanceKm(end[0], end[1], oriented[oriented.length - 1][0], oriented[oriented.length - 1][1]);
  if (startGapKm > MAX_FLOW_ENDPOINT_CONNECT_KM || endGapKm > MAX_FLOW_ENDPOINT_CONNECT_KM) {
    return null;
  }
  const out = [...oriented];
  if (distanceSq(start, out[0]) > 1e-7) out.unshift(start);
  if (distanceSq(end, out[out.length - 1]) > 1e-7) out.push(end);
  return dedupeCoords(out);
}

function lineOptionsFromFeature(feature: TransportFlowFeature | undefined): [number, number][][] {
  const geometry = feature?.geometry;
  if (!geometry) return [];
  if (geometry.type === 'LineString') return [dedupeCoords((geometry.coordinates || []) as [number, number][])];
  if (geometry.type === 'MultiLineString') {
    return ((geometry.coordinates || []) as [number, number][][]).map((line) => dedupeCoords(line));
  }
  return [];
}

function selectFlowGeometry(
  flowRefs: string[],
  fallbackCoords: [number, number][],
  flowById: Map<string, TransportFlowFeature>,
): [number, number][] {
  if (fallbackCoords.length < 2) return fallbackCoords;
  if (flowRefs.length === 0) {
    return fallbackCoords;
  }
  const start = fallbackCoords[0];
  const end = fallbackCoords[fallbackCoords.length - 1];
  let best: { coords: [number, number][]; score: number } | null = null;
  for (const ref of flowRefs) {
    const feature = flowById.get(ref);
    for (const option of lineOptionsFromFeature(feature)) {
      if (option.length < 2) continue;
      const oriented = orientLineForEndpoints(option, start, end);
      const score = distanceSq(start, oriented[0]) + distanceSq(end, oriented[oriented.length - 1]);
      const connected = maybeConnectEndpoints(oriented, fallbackCoords);
      if (!connected || connected.length < 2) continue;
      if (!best || score < best.score) {
        best = {
          coords: connected,
          score,
        };
      }
    }
  }
  if (best) return best.coords;
  return fallbackCoords;
}

function isApproximateRouteGeometrySource(value: string): boolean {
  const raw = String(value || '').trim().toLowerCase();
  return raw === 'fragmentary_waypoint_sequence' || raw === 'partial_segment_geometry';
}

function shouldPreferFlowGeometry(
  explicitCoords: [number, number][],
  flowRefs: string[],
  routeGeometrySource: string,
): boolean {
  return explicitCoords.length >= 2
    && flowRefs.length > 0
    && isApproximateRouteGeometrySource(routeGeometrySource);
}

function boundsFromCoords(coords: [number, number][]): [number, number, number, number] {
  let minLon = Number.POSITIVE_INFINITY;
  let minLat = Number.POSITIVE_INFINITY;
  let maxLon = Number.NEGATIVE_INFINITY;
  let maxLat = Number.NEGATIVE_INFINITY;
  for (const [lon, lat] of coords) {
    minLon = Math.min(minLon, lon);
    minLat = Math.min(minLat, lat);
    maxLon = Math.max(maxLon, lon);
    maxLat = Math.max(maxLat, lat);
  }
  if (!Number.isFinite(minLon) || !Number.isFinite(minLat) || !Number.isFinite(maxLon) || !Number.isFinite(maxLat)) {
    return [0, 0, 0, 0];
  }
  return [minLon, minLat, maxLon, maxLat];
}

function boundsIntersectsViewport(bounds: [number, number, number, number], viewport: maplibregl.LngLatBounds): boolean {
  return !(
    bounds[2] < viewport.getWest()
    || bounds[0] > viewport.getEast()
    || bounds[3] < viewport.getSouth()
    || bounds[1] > viewport.getNorth()
  );
}

function inferArtifactStatus(artifact: MaterializedArtifact): LegStatus {
  const raw = String(artifact.sufficiencyStatus || artifact.payload?.sufficiency?.status || '').trim();
  if (raw === 'validated' || raw === 'forensic_gap') return raw;
  return 'heuristic';
}

function buildClusterMemberFeatureCollection(
  objects: GeoObject[],
  theme: ThemeMode,
): GeoJSON.FeatureCollection {
  return {
    type: 'FeatureCollection',
    features: objects.map((object) => buildPointFeature(object, theme, false)),
  };
}

async function loadClusterLeaves(
  source: maplibregl.GeoJSONSource,
  clusterId: number,
  pointCount: number,
): Promise<Array<GeoJSON.Feature<GeoJSON.Geometry, GeoJSON.GeoJsonProperties>>> {
  const pageSize = 250;
  const leaves: Array<GeoJSON.Feature<GeoJSON.Geometry, GeoJSON.GeoJsonProperties>> = [];
  for (let offset = 0; offset < pointCount; offset += pageSize) {
    const page = await source.getClusterLeaves(clusterId, Math.min(pageSize, pointCount - offset), offset);
    leaves.push(...page);
    if (page.length < pageSize) break;
  }
  return leaves;
}

export function usePathLegClusterLayer(
  map: maplibregl.Map | null,
  isReady: boolean,
  styleEpoch: number,
  artifacts: MaterializedArtifact[],
  objects: GeoObject[],
  theme: ThemeMode,
  enabled: boolean,
  activeArtifactIds: string[] = [],
  options?: PathLegClusterLayerOptions,
): PathLegClusterLayerState {
  const [cameraSnapshot, setCameraSnapshot] = useState(() => ({
    zoom: map?.getZoom() ?? 0,
    west: map?.getBounds().getWest() ?? -180,
    south: map?.getBounds().getSouth() ?? -85,
    east: map?.getBounds().getEast() ?? 180,
    north: map?.getBounds().getNorth() ?? 85,
  }));
  const [markerClusters, setMarkerClusters] = useState<MarkerClusterSnapshot>(() => ({
    visibleObjectIds: new Set<string>(),
    visibleCanonicalPaths: new Set<string>(),
    membershipByObjectId: new Map<string, MarkerClusterMembership>(),
    membershipByCanonicalPath: new Map<string, MarkerClusterMembership>(),
  }));
  const [memberClusterVersion, setMemberClusterVersion] = useState(0);

  useEffect(() => {
    if (!map || !isReady) return;
    let raf = 0;
    const sync = () => {
      const bounds = map.getBounds();
      setCameraSnapshot({
        zoom: map.getZoom(),
        west: bounds.getWest(),
        south: bounds.getSouth(),
        east: bounds.getEast(),
        north: bounds.getNorth(),
      });
    };
    const scheduleSync = () => {
      if (raf) cancelAnimationFrame(raf);
      raf = requestAnimationFrame(sync);
    };
    scheduleSync();
    map.on('zoom', scheduleSync);
    map.on('move', scheduleSync);
    map.on('resize', scheduleSync);
    return () => {
      if (raf) cancelAnimationFrame(raf);
      map.off('zoom', scheduleSync);
      map.off('move', scheduleSync);
      map.off('resize', scheduleSync);
    };
  }, [isReady, map]);

  useEffect(() => {
    if (!map || !isReady || !enabled || !canAccessMapStyle(map)) {
      setMarkerClusters({
        visibleObjectIds: new Set<string>(),
        visibleCanonicalPaths: new Set<string>(),
        membershipByObjectId: new Map<string, MarkerClusterMembership>(),
        membershipByCanonicalPath: new Map<string, MarkerClusterMembership>(),
      });
      return;
    }
    if (!map.getSource(MEMBER_CLUSTER_SRC)) return;

    let cancelled = false;
    const sync = async () => {
      try {
        const canvas = map.getCanvas();
        const width = Math.max(1, Math.round(canvas.clientWidth || 0));
        const height = Math.max(1, Math.round(canvas.clientHeight || 0));
        const sourceFeatures = map.queryRenderedFeatures(
          [[0, 0], [width, height]],
          { layers: [MEMBER_CLUSTER_LAYER, MEMBER_POINT_LAYER] },
        ) as maplibregl.MapGeoJSONFeature[];

        const pointFeatures: maplibregl.MapGeoJSONFeature[] = [];
        const clusterFeaturesById = new Map<number, maplibregl.MapGeoJSONFeature>();
        const pointIds = new Set<string>();

        for (const feature of sourceFeatures) {
          if (feature.geometry?.type !== 'Point') continue;
          const coordinates = feature.geometry.coordinates as [number, number];
          const clusterId = Number(feature.properties?.cluster_id);
          const pointCount = Number(feature.properties?.point_count);
          if (Number.isFinite(clusterId) && Number.isFinite(pointCount) && pointCount > 0) {
            if (!clusterFeaturesById.has(clusterId)) clusterFeaturesById.set(clusterId, feature);
            continue;
          }
          const objectId = String(feature.properties?.id || '').trim();
          const dedupeKey = objectId || `${coordinates[0].toFixed(6)}:${coordinates[1].toFixed(6)}`;
          if (pointIds.has(dedupeKey)) continue;
          pointIds.add(dedupeKey);
          pointFeatures.push(feature);
        }

        const source = map.getSource(MEMBER_CLUSTER_SRC) as maplibregl.GeoJSONSource | undefined;
        if (!source) return;

        const visibleObjectIds = new Set<string>();
        const visibleCanonicalPaths = new Set<string>();
        for (const feature of pointFeatures) {
          const objectId = String(feature.properties?.id || '').trim();
          const canonicalPath = String(feature.properties?.canonicalPath || '').trim();
          if (objectId) visibleObjectIds.add(objectId);
          if (canonicalPath) visibleCanonicalPaths.add(canonicalPath);
        }

        const membershipByObjectId = new Map<string, MarkerClusterMembership>();
        const membershipByCanonicalPath = new Map<string, MarkerClusterMembership>();
        await Promise.all(Array.from(clusterFeaturesById.values()).map(async (feature) => {
          const clusterId = Number(feature.properties?.cluster_id);
          const pointCount = Number(feature.properties?.point_count);
          const coords = feature.geometry?.type === 'Point'
            ? feature.geometry.coordinates as [number, number]
            : null;
          if (!Number.isFinite(clusterId) || !Number.isFinite(pointCount) || !coords) return;
          const [expansionZoom, leaves] = await Promise.all([
            source.getClusterExpansionZoom(clusterId).catch(() => null),
            loadClusterLeaves(source, clusterId, Math.max(1, pointCount)).catch(() => []),
          ]);
          const membership: MarkerClusterMembership = {
            clusterKey: `geo-cluster:${clusterId}`,
            clusterId,
            centroid: coords,
            pointCount: Math.max(1, pointCount),
            expansionZoom: typeof expansionZoom === 'number' && Number.isFinite(expansionZoom) ? expansionZoom : null,
          };
          for (const leaf of leaves) {
            const objectId = String(leaf.properties?.id || '').trim();
            const canonicalPath = String(leaf.properties?.canonicalPath || '').trim();
            if (objectId) membershipByObjectId.set(objectId, membership);
            if (canonicalPath) membershipByCanonicalPath.set(canonicalPath, membership);
          }
        }));

        if (cancelled) return;
        setMarkerClusters({
          visibleObjectIds,
          visibleCanonicalPaths,
          membershipByObjectId,
          membershipByCanonicalPath,
        });
      } catch {
        if (cancelled) return;
        setMarkerClusters({
          visibleObjectIds: new Set<string>(),
          visibleCanonicalPaths: new Set<string>(),
          membershipByObjectId: new Map<string, MarkerClusterMembership>(),
          membershipByCanonicalPath: new Map<string, MarkerClusterMembership>(),
        });
      }
    };

    const raf = requestAnimationFrame(() => {
      void sync();
    });
    return () => {
      cancelled = true;
      cancelAnimationFrame(raf);
    };
  }, [cameraSnapshot, enabled, isReady, map, memberClusterVersion, styleEpoch]);

  const normalized = useMemo(() => {
    const coordsByPath = new Map<string, { lon: number; lat: number }>();
    const objectByPath = new Map<string, GeoObject>();
    const flowById = new Map<string, TransportFlowFeature>();
    const memberObjects = new Map<string, GeoObject>();
    const activeArtifactSet = new Set((activeArtifactIds || []).map((value) => String(value || '').trim()).filter(Boolean));
    const selectedObjectPath = String(options?.selectedObject?.canonicalPath || '').trim();
    const focusFamilyIds = new Set<string>();

    for (const artifact of artifacts || []) {
      const artifactId = String(artifact.id || '').trim();
      const familyId = String(artifact.familyId || artifact.payload?.family?.id || '').trim();
      if (artifactId && familyId && activeArtifactSet.has(artifactId)) focusFamilyIds.add(familyId);
    }
    for (const obj of objects) {
      const canonicalPath = String(obj?.canonicalPath || '').trim();
      const lat = Number(obj?.coordinates?.lat);
      const lon = Number(obj?.coordinates?.lon);
      if (!canonicalPath || !Number.isFinite(lat) || !Number.isFinite(lon)) continue;
      coordsByPath.set(canonicalPath, { lon, lat });
      objectByPath.set(canonicalPath, obj);
    }
    for (const feature of STATIC_TRANSPORT_FLOW_DATA.features || []) {
      const id = String(feature?.id || '').trim();
      if (id) flowById.set(id, feature);
    }
    seedRouteFragmentPolylinesIntoFlowMap(flowById, artifacts);

    const legs: NormalizedLeg[] = [];
    for (const artifact of artifacts || []) {
      const artifactId = String(artifact.id || '').trim();
      if (!artifactId) continue;
      const artifactStatus = inferArtifactStatus(artifact);
      const artifactFamilyId = String(artifact.familyId || artifact.payload?.family?.id || '').trim();
      const artifactFocus = activeArtifactSet.has(artifactId) || (artifactFamilyId && focusFamilyIds.has(artifactFamilyId));
      const legsPayload = Array.isArray(artifact.payload?.legs) ? artifact.payload.legs : [];
      const legsSource = collapseAbstractRailNetworkLegs(legsPayload);

      for (let legArrayIndex = 0; legArrayIndex < legsSource.length; legArrayIndex += 1) {
        const leg = legsSource[legArrayIndex];
        const legIndex = resolvedArtifactLegIndex(leg, legArrayIndex);
        const sourceRef = artifactLegString(leg, 'supplierRef', 'SupplierRef');
        const targetRef = artifactLegString(leg, 'customerRef', 'CustomerRef');
        if (!sourceRef || !targetRef) continue;
        const transportViaRefs = artifactLegStringList(leg, 'transportViaRefs', 'TransportViaRefs');
        const refs = [sourceRef, ...transportViaRefs, targetRef].filter(Boolean);
        const fallbackCoords = dedupeCoords(
          refs
            .map((ref) => coordsByPath.get(ref))
            .filter((value): value is { lon: number; lat: number } => Boolean(value))
            .map((value) => [value.lon, value.lat] as [number, number]),
        );
        const explicitCoords = artifactLegCoordinateList(leg, 'routeCoordinates', 'RouteCoordinates');
        const baseCoords = explicitCoords.length >= 2 ? explicitCoords : fallbackCoords;
        if (baseCoords.length < 2) continue;
        const flowRefs = artifactLegStringList(leg, 'flowRefs', 'FlowRefs');
        const routeGeometrySource = artifactLegString(leg, 'routeGeometrySource', 'RouteGeometrySource');
        const preferFlowGeometry = shouldPreferFlowGeometry(explicitCoords, flowRefs, routeGeometrySource);
        const coords = !preferFlowGeometry && explicitCoords.length >= 2
          ? explicitCoords
          : selectFlowGeometry(flowRefs, fallbackCoords, flowById);
        if (explicitCoords.length < 2 && flowRefs.length === 0 && routeGeometrySource === 'fragmentary_waypoint_sequence') continue;
        if (coords.length < 2) continue;

        const sourceObject = objectByPath.get(sourceRef);
        const targetObject = objectByPath.get(targetRef);
        if (sourceObject?.id) memberObjects.set(sourceObject.id, sourceObject);
        if (targetObject?.id) memberObjects.set(targetObject.id, targetObject);

        const selectedObjectBoost = selectedObjectPath && (selectedObjectPath === sourceRef || selectedObjectPath === targetRef || transportViaRefs.includes(selectedObjectPath)) ? 0.42 : 0;
        const statusBoost = artifactStatus === 'forensic_gap' ? 0.08 : artifactStatus === 'validated' ? 0.04 : 0;

        legs.push({
          id: `${artifactId}:leg:${legIndex}`,
          artifactId,
          artifactFamilyId,
          sourceRef,
          targetRef,
          focusBoost: clamp((artifactFocus ? 0.48 : 0) + selectedObjectBoost + statusBoost, 0, 1),
          bounds: boundsFromCoords(coords),
        });
      }
    }

    return {
      legs,
      memberObjects: Array.from(memberObjects.values()),
    };
  }, [activeArtifactIds, artifacts, objects, options?.selectedObject]);

  useEffect(() => {
    if (!map || !isReady || !canAccessMapStyle(map)) return;
    void ensureMarkerImages(map, normalized.memberObjects, theme);
    const featureCollection = buildClusterMemberFeatureCollection(normalized.memberObjects, theme);
    if (!map.getSource(MEMBER_CLUSTER_SRC)) {
      map.addSource(MEMBER_CLUSTER_SRC, {
        type: 'geojson',
        data: featureCollection as any,
        cluster: true,
        clusterMaxZoom: GEO_MARKER_CLUSTER_MAX_ZOOM,
        clusterRadius: MEMBER_CLUSTER_RADIUS,
      });
      setMemberClusterVersion((value) => value + 1);
    }
    const source = map.getSource(MEMBER_CLUSTER_SRC) as maplibregl.GeoJSONSource | undefined;
    source?.setData(featureCollection as any);
    if (!map.getLayer(MEMBER_CLUSTER_LAYER)) {
      map.addLayer({
        id: MEMBER_CLUSTER_LAYER,
        type: 'circle',
        source: MEMBER_CLUSTER_SRC,
        filter: ['has', 'point_count'],
        paint: {
          'circle-radius': ['step', ['get', 'point_count'], 12, 8, 14, 32, 18],
          'circle-color': '#000000',
          'circle-opacity': 0,
          'circle-stroke-width': 0,
        },
      });
    }
    if (!map.getLayer(MEMBER_POINT_LAYER)) {
      map.addLayer({
        id: MEMBER_POINT_LAYER,
        type: 'circle',
        source: MEMBER_CLUSTER_SRC,
        filter: ['!', ['has', 'point_count']],
        paint: {
          'circle-radius': 6,
          'circle-color': '#000000',
          'circle-opacity': 0,
          'circle-stroke-width': 0,
        },
      });
    }
    setMemberClusterVersion((value) => value + 1);
  }, [isReady, map, normalized.memberObjects, styleEpoch, theme]);

  const projection = useMemo(() => {
    if (!map || !isReady || !enabled || normalized.legs.length === 0) {
      return {
        stats: {
          artifactCount: artifacts.length,
          legCount: normalized.legs.length,
          supernodeCount: 0,
          superedgeCount: 0,
          rawVisibilityWeight: 1,
        } satisfies PathLegClusterStats,
        visibilityPolicy: {
          rawVisibilityWeight: 1,
          suppressedLegIds: [],
          revealedLegIds: [],
          visibleObjectIds: [],
          visibleObjectPaths: [],
          clusterAnchorByPath: {},
        } satisfies PathLegVisibilityPolicy,
      };
    }

    const zoom = cameraSnapshot.zoom;
    const viewport = new maplibregl.LngLatBounds(
      [cameraSnapshot.west, cameraSnapshot.south],
      [cameraSnapshot.east, cameraSnapshot.north],
    );
    const viewportLegs = normalized.legs.filter((leg) => boundsIntersectsViewport(leg.bounds, viewport));
    const viewportLegCount = viewportLegs.length || normalized.legs.length;
    const densityLoad = clamp((viewportLegCount - 18) / 120, 0, 1);
    const rawVisibilityBase = clamp(
      smoothstep(zoom, GEO_MARKER_CLUSTER_MAX_ZOOM - 1.2, GEO_MARKER_CLUSTER_MAX_ZOOM + 0.7) + (densityLoad < 0.18 ? 0.08 : 0),
      0.24,
      1,
    );
    const rawVisibilityWeight = rawVisibilityBase;

    const clusterAnchorByPath: Record<string, [number, number]> = {};
    for (const [canonicalPath, membership] of markerClusters.membershipByCanonicalPath.entries()) {
      if (markerClusters.visibleCanonicalPaths.has(canonicalPath)) continue;
      clusterAnchorByPath[canonicalPath] = membership.centroid;
    }

    const revealedRawLegIds = new Set<string>();
    for (const leg of normalized.legs) {
      const sourceClustered = Boolean(clusterAnchorByPath[leg.sourceRef]);
      const targetClustered = Boolean(clusterAnchorByPath[leg.targetRef]);
      if (sourceClustered || targetClustered || leg.focusBoost > 0.18) {
        revealedRawLegIds.add(leg.id);
      }
    }

    return {
      stats: {
        artifactCount: artifacts.length,
        legCount: normalized.legs.length,
        supernodeCount: 0,
        superedgeCount: 0,
        rawVisibilityWeight,
      } satisfies PathLegClusterStats,
      visibilityPolicy: {
        rawVisibilityWeight,
        suppressedLegIds: [],
        revealedLegIds: Array.from(revealedRawLegIds),
        visibleObjectIds: Array.from(markerClusters.visibleObjectIds),
        visibleObjectPaths: Array.from(markerClusters.visibleCanonicalPaths),
        clusterAnchorByPath,
      } satisfies PathLegVisibilityPolicy,
    };
  }, [activeArtifactIds.length, artifacts.length, cameraSnapshot, enabled, isReady, map, markerClusters, normalized.legs]);

  return {
    stats: projection.stats,
    visibilityPolicy: projection.visibilityPolicy,
  };
}
