#!/usr/bin/env node
/**
 * Refines transport flow coordinates using real-world data:
 * - sea + ferry_roro: searoute-js (Oak Ridge / Eurostat maritime network)
 * - air: coordinates from web/src/data/air_corridor_updates.json (great-circle from OpenFlights hubs)
 * - pipeline_* + power_*: coordinates from web/src/data/pipeline_power_updates.json (GEM, ENTSOG, etc.; see REFINED_SOURCES.md)
 * - road, rail, inland_waterway, intermodal, internet_cable: from remaining_flow_updates.json (derive-remaining-flows.mjs)
 * - others: pass-through
 *
 * Usage: node scripts/refine-transport-flows.mjs [path-to-transportFlows.v1.json]
 * Writes to the same path (backup as .bak first).
 */

import { readFileSync, writeFileSync, existsSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';

const __dirname = dirname(fileURLToPath(import.meta.url));
const defaultPath = join(__dirname, '../src/data/transportFlows.v1.json');

async function main() {
  const path = process.argv[2] || defaultPath;
  const raw = readFileSync(path, 'utf8');
  const data = JSON.parse(raw);

  const searoute = (await import('searoute-js')).default;
  let updated = 0;
  let skipped = 0;
  let failed = 0;

  const dataDir = dirname(path);
  const airUpdatesPath = join(dataDir, 'air_corridor_updates.json');
  let airUpdates = null;
  if (existsSync(airUpdatesPath)) {
    try {
      airUpdates = JSON.parse(readFileSync(airUpdatesPath, 'utf8'));
    } catch (e) {
      console.warn('[warn] Could not load air_corridor_updates.json:', e.message);
    }
  }

  const pipelinePowerUpdatesPath = join(dataDir, 'pipeline_power_updates.json');
  let pipelinePowerUpdates = null;
  if (existsSync(pipelinePowerUpdatesPath)) {
    try {
      const raw = JSON.parse(readFileSync(pipelinePowerUpdatesPath, 'utf8'));
      pipelinePowerUpdates = Object.fromEntries(
        Object.entries(raw).filter(([k]) => !k.startsWith('_'))
      );
    } catch (e) {
      console.warn('[warn] Could not load pipeline_power_updates.json:', e.message);
    }
  }

  const remainingUpdatesPath = join(dataDir, 'remaining_flow_updates.json');
  let remainingUpdates = null;
  if (existsSync(remainingUpdatesPath)) {
    try {
      const raw = JSON.parse(readFileSync(remainingUpdatesPath, 'utf8'));
      remainingUpdates = Object.fromEntries(
        Object.entries(raw).filter(([k]) => !k.startsWith('_'))
      );
    } catch (e) {
      console.warn('[warn] Could not load remaining_flow_updates.json:', e.message);
    }
  }

  const portEndpointsPath = join(dataDir, 'sea_ferry_port_endpoints.json');
  let portEndpoints = null;
  if (existsSync(portEndpointsPath)) {
    try {
      const raw = JSON.parse(readFileSync(portEndpointsPath, 'utf8'));
      portEndpoints = Object.fromEntries(
        Object.entries(raw).filter(([k]) => !k.startsWith('_'))
      );
    } catch (e) {
      console.warn('[warn] Could not load sea_ferry_port_endpoints.json:', e.message);
    }
  }

  function snapToPortEndpoints(feature) {
    if (!portEndpoints || !feature.geometry?.coordinates?.length) return;
    const p = portEndpoints[feature.id];
    if (!p?.origin || !p?.dest) return;
    const c = feature.geometry.coordinates;
    if (c.length >= 2) {
      c[0] = Array.isArray(p.origin) ? [...p.origin] : p.origin;
      c[c.length - 1] = Array.isArray(p.dest) ? [...p.dest] : p.dest;
    }
  }

  const MARITIME_MODES = new Set(['sea', 'ferry_roro']);
  /** Keep existing geometry; searoute returns no route for these short crossings */
  const SKIP_SEAROUTE_IDS = new Set([
    'ferry_roro_dover_calais',
    'ferry_roro_singapore_batam',
    'ferry_roro_rostock_trelleborg',
  ]);
  const PIPELINE_POWER_PREFIXES = ['pipeline_', 'power_'];

  for (const feature of data.features) {
    const mode = feature.properties?.flowMode;

    if (
      pipelinePowerUpdates &&
      feature.id &&
      pipelinePowerUpdates[feature.id] &&
      PIPELINE_POWER_PREFIXES.some((p) => mode && mode.startsWith(p))
    ) {
      const coords = pipelinePowerUpdates[feature.id];
      if (Array.isArray(coords) && coords.length >= 2) {
        feature.geometry = { type: 'LineString', coordinates: coords };
        updated++;
        console.log(`[ok] ${feature.id} (${mode}): ${coords.length} points (pipeline_power_updates)`);
      } else {
        failed++;
        console.warn(`[skip] ${feature.id}: invalid pipeline_power update coordinates`);
      }
      continue;
    }

    if (mode === 'air' && airUpdates && feature.id && airUpdates[feature.id]) {
      const coords = airUpdates[feature.id];
      if (Array.isArray(coords) && coords.length >= 2) {
        feature.geometry = { type: 'LineString', coordinates: coords };
        updated++;
        console.log(`[ok] ${feature.id} (air): ${coords.length} points (air_corridor_updates)`);
      } else {
        failed++;
        console.warn(`[skip] ${feature.id}: invalid air update coordinates`);
      }
      continue;
    }

    if (remainingUpdates && feature.id && remainingUpdates[feature.id]) {
      const coords = remainingUpdates[feature.id];
      if (Array.isArray(coords) && coords.length >= 2) {
        feature.geometry = { type: 'LineString', coordinates: coords.map((x) => [...x]) };
        snapToPortEndpoints(feature);
        updated++;
        console.log(`[ok] ${feature.id} (${mode}): ${feature.geometry.coordinates.length} points (remaining_flow_updates)`);
      } else {
        failed++;
        console.warn(`[skip] ${feature.id}: invalid remaining update coordinates`);
      }
      continue;
    }

    if (!MARITIME_MODES.has(mode)) {
      skipped++;
      continue;
    }
    if (SKIP_SEAROUTE_IDS.has(feature.id)) {
      skipped++;
      continue;
    }

    const coords = getLineCoordinates(feature);
    if (!coords || coords.length < 2) {
      failed++;
      console.warn(`[skip] ${feature.id}: not a line`);
      continue;
    }

    const port = portEndpoints?.[feature.id];
    const origin = port?.origin ?? coords[0];
    const dest = port?.dest ?? coords[coords.length - 1];

    const originPoint = {
      type: 'Feature',
      properties: {},
      geometry: { type: 'Point', coordinates: Array.isArray(origin) ? [origin[0], origin[1]] : [origin.lon ?? origin[0], origin.lat ?? origin[1]] },
    };
    const destPoint = {
      type: 'Feature',
      properties: {},
      geometry: { type: 'Point', coordinates: Array.isArray(dest) ? [dest[0], dest[1]] : [dest.lon ?? dest[0], dest.lat ?? dest[1]] },
    };

    try {
      const routeFeature = searoute(originPoint, destPoint);
      if (routeFeature?.geometry?.type === 'LineString' && Array.isArray(routeFeature.geometry.coordinates)) {
        feature.geometry = {
          type: 'LineString',
          coordinates: routeFeature.geometry.coordinates,
        };
        snapToPortEndpoints(feature);
        updated++;
        console.log(`[ok] ${feature.id} (${mode}): ${feature.geometry.coordinates.length} points`);
      } else {
        failed++;
        console.warn(`[fail] ${feature.id}: invalid route result`);
      }
    } catch (err) {
      failed++;
      console.warn(`[fail] ${feature.id}:`, err.message);
    }
  }

  for (const feature of data.features) {
    snapToPortEndpoints(feature);
  }

  console.log(`\nDone: ${updated} updated, ${skipped} skipped, ${failed} failed.`);

  if (updated > 0 || portEndpoints) {
    const backupPath = path + '.bak';
    writeFileSync(backupPath, raw, 'utf8');
    console.log(`Backup: ${backupPath}`);
    writeFileSync(path, JSON.stringify(data, null, 2), 'utf8');
    console.log(`Written: ${path}`);
  }
}

function getLineCoordinates(feature) {
  const g = feature.geometry;
  if (!g) return null;
  if (g.type === 'LineString' && Array.isArray(g.coordinates)) return g.coordinates;
  if (g.type === 'MultiLineString' && Array.isArray(g.coordinates)) {
    return g.coordinates.flat();
  }
  return null;
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
