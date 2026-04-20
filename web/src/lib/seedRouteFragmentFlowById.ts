/**
 * P12.04: path legs reference `/route/...` fragments via flowRefs; curated flows use opaque ids.
 * When a materialized leg carries an explicit polyline and a single `/route/` ref, register that
 * LineString on the flow map so selectFlowGeometry can align legs to corridor geometry.
 */
import type { MaterializedArtifact } from '../components/SupplyChainOverlay';
import type { TransportFlowFeature } from './staticTransportFlows';
import { readLegRouteOverlays, selectPrimaryLegRouteOverlay } from './supplyRouteOverlays';

function isRouteCanonicalPathRef(ref: string): boolean {
  return String(ref || '').trim().toLowerCase().startsWith('/route/');
}

function dedupeCoordsLonLat(coords: [number, number][]): [number, number][] {
  const out: [number, number][] = [];
  for (const [lon, lat] of coords) {
    const prev = out[out.length - 1];
    if (prev && Math.abs(prev[0] - lon) < 1e-7 && Math.abs(prev[1] - lat) < 1e-7) continue;
    out.push([lon, lat]);
  }
  return out;
}

function readLegField<T>(leg: any, ...keys: string[]): T | undefined {
  for (const k of keys) {
    if (leg && leg[k] != null) return leg[k] as T;
  }
  return undefined;
}

function readLegString(leg: any, ...keys: string[]): string {
  return String(readLegField(leg, ...keys) || '').trim();
}

function readLegStringList(leg: any, ...keys: string[]): string[] {
  const v = readLegField<unknown>(leg, ...keys);
  if (!Array.isArray(v)) return [];
  return v.map((x) => String(x || '').trim()).filter(Boolean);
}

function readLegCoords(leg: any, ...keys: string[]): [number, number][] {
  const v = readLegField<unknown>(leg, ...keys);
  if (!Array.isArray(v)) return [];
  return dedupeCoordsLonLat(
    v
      .map((item) => {
        const lat = Number((item as any)?.lat);
        const lon = Number((item as any)?.lon);
        return Number.isFinite(lat) && Number.isFinite(lon) ? ([lon, lat] as [number, number]) : null;
      })
      .filter((x): x is [number, number] => Boolean(x)),
  );
}

function isAbstractRailNetworkRef(ref: string): boolean {
  const n = String(ref || '').trim().toLowerCase();
  return (
    n.endsWith('-rail-network') ||
    n.includes('/rail-network') ||
    n.endsWith('-distribution-network') ||
    n.includes('/distribution-network')
  );
}

function uniqueLegStrings(values: string[]): string[] {
  return Array.from(new Set(values.map((v) => String(v || '').trim()).filter(Boolean)));
}

function collapseAbstractRailNetworkLegs(legs: any[]): any[] {
  const out: any[] = [];
  for (let idx = 0; idx < legs.length; idx += 1) {
    const current = legs[idx];
    const currentCustomer = readLegString(current, 'customerRef', 'CustomerRef');
    if (idx + 1 < legs.length && isAbstractRailNetworkRef(currentCustomer)) {
      const next = legs[idx + 1];
      const nextSupplier = readLegString(next, 'supplierRef', 'SupplierRef');
      const sameProduct =
        readLegString(current, 'productRef', 'ProductRef') === readLegString(next, 'productRef', 'ProductRef');
      if (currentCustomer && currentCustomer === nextSupplier && sameProduct) {
        out.push({
          ...current,
          customerRef: readLegString(next, 'customerRef', 'CustomerRef'),
          CustomerRef: readLegString(next, 'customerRef', 'CustomerRef'),
          transportViaRefs: uniqueLegStrings([
            ...readLegStringList(current, 'transportViaRefs', 'TransportViaRefs'),
            ...readLegStringList(next, 'transportViaRefs', 'TransportViaRefs'),
          ].filter((r) => !isAbstractRailNetworkRef(r))),
          TransportViaRefs: uniqueLegStrings([
            ...readLegStringList(current, 'transportViaRefs', 'TransportViaRefs'),
            ...readLegStringList(next, 'transportViaRefs', 'TransportViaRefs'),
          ].filter((r) => !isAbstractRailNetworkRef(r))),
          flowRefs: uniqueLegStrings([
            ...readLegStringList(current, 'flowRefs', 'FlowRefs'),
            ...readLegStringList(next, 'flowRefs', 'FlowRefs'),
          ]),
          FlowRefs: uniqueLegStrings([
            ...readLegStringList(current, 'flowRefs', 'FlowRefs'),
            ...readLegStringList(next, 'flowRefs', 'FlowRefs'),
          ]),
          alternateFlowRefs: uniqueLegStrings([
            ...readLegStringList(current, 'alternateFlowRefs', 'AlternateFlowRefs'),
            ...readLegStringList(next, 'alternateFlowRefs', 'AlternateFlowRefs'),
          ]),
          AlternateFlowRefs: uniqueLegStrings([
            ...readLegStringList(current, 'alternateFlowRefs', 'AlternateFlowRefs'),
            ...readLegStringList(next, 'alternateFlowRefs', 'AlternateFlowRefs'),
          ]),
          routeCoordinates: [],
          RouteCoordinates: [],
          routeGeometrySource: '',
          RouteGeometrySource: '',
          mode: readLegString(current, 'mode', 'Mode') || readLegString(next, 'mode', 'Mode'),
          Mode: readLegString(current, 'mode', 'Mode') || readLegString(next, 'mode', 'Mode'),
          routeOverlays: [],
          RouteOverlays: [],
        });
        idx += 1;
        continue;
      }
    }
    out.push(current);
  }
  return out;
}

function syntheticRouteFlowFeature(ref: string, coords: [number, number][]): TransportFlowFeature {
  const coordinates = dedupeCoordsLonLat(coords);
  return {
    type: 'Feature',
    id: ref,
    geometry: { type: 'LineString', coordinates },
    properties: {
      flowMode: 'intermodal',
      name: ref,
      priority: 2,
      tags: ['route-canonical-path'],
    },
  } as TransportFlowFeature;
}

export function seedRouteFragmentPolylinesIntoFlowMap(
  flowById: Map<string, TransportFlowFeature>,
  artifacts: MaterializedArtifact[] | null | undefined,
): void {
  const claimed = new Set<string>();
  for (const artifact of artifacts || []) {
    const raw = Array.isArray(artifact.payload?.legs) ? artifact.payload.legs : [];
    const legs = collapseAbstractRailNetworkLegs(raw);
    for (const leg of legs) {
      const overlays = readLegRouteOverlays(leg);
      const primary = selectPrimaryLegRouteOverlay(overlays);
      const routeRefs = uniqueLegStrings([
        ...(primary?.flowRefs || []).filter(isRouteCanonicalPathRef),
        ...(primary?.orderedRefs || []).filter(isRouteCanonicalPathRef),
        ...readLegStringList(leg, 'flowRefs', 'FlowRefs').filter(isRouteCanonicalPathRef),
      ]);
      if (routeRefs.length !== 1) continue;
      const ref = routeRefs[0];
      if (claimed.has(ref)) continue;
      let line: [number, number][] = [];
      if (primary?.routeCoordinates && primary.routeCoordinates.length >= 2) {
        line = dedupeCoordsLonLat(
          primary.routeCoordinates
            .map((item) => {
              const lat = Number(item.lat);
              const lon = Number(item.lon);
              return Number.isFinite(lat) && Number.isFinite(lon) ? ([lon, lat] as [number, number]) : null;
            })
            .filter((x): x is [number, number] => Boolean(x)),
        );
      }
      if (line.length < 2) {
        line = readLegCoords(leg, 'routeCoordinates', 'RouteCoordinates');
      }
      if (line.length < 2) continue;
      claimed.add(ref);
      if (!flowById.has(ref)) {
        flowById.set(ref, syntheticRouteFlowFeature(ref, line));
      }
    }
  }
}
