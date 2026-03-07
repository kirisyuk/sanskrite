#!/usr/bin/env node
/**
 * Derives air corridor LineString coordinates from hub airport positions
 * using great-circle interpolation. Hub coordinates from OpenFlights (airports.dat).
 * Output: JSON with shape { "featureId": [[lon, lat], ...] } for use as air_corridor_updates.json
 *
 * Usage: node scripts/derive-air-corridors.mjs
 */

function toRad(x) {
  return (x * Math.PI) / 180;
}
function toDeg(x) {
  return (x * 180) / Math.PI;
}

/** Returns [lon, lat] for point at fraction t (0..1) along great circle from (lat1,lon1) to (lat2,lon2). */
function greatCirclePoint(lat1, lon1, lat2, lon2, t) {
  const lat1r = toRad(lat1);
  const lon1r = toRad(lon1);
  const lat2r = toRad(lat2);
  const lon2r = toRad(lon2);
  const d =
    2 *
    Math.asin(
      Math.sqrt(
        Math.pow(Math.sin((lat1r - lat2r) / 2), 2) +
          Math.cos(lat1r) * Math.cos(lat2r) * Math.pow(Math.sin((lon1r - lon2r) / 2), 2)
      )
    );
  if (d === 0) return [lon1, lat1];
  const A = Math.sin((1 - t) * d) / Math.sin(d);
  const B = Math.sin(t * d) / Math.sin(d);
  const x = A * Math.cos(lat1r) * Math.cos(lon1r) + B * Math.cos(lat2r) * Math.cos(lon2r);
  const y = A * Math.cos(lat1r) * Math.sin(lon1r) + B * Math.cos(lat2r) * Math.sin(lon2r);
  const z = A * Math.sin(lat1r) + B * Math.sin(lat2r);
  const lat = Math.atan2(z, Math.sqrt(x * x + y * y));
  const lon = Math.atan2(y, x);
  return [toDeg(lon), toDeg(lat)];
}

/** Generate n points (including start/end) along great circle. Returns [[lon,lat], ...]. */
function greatCircleSegment(lat1, lon1, lat2, lon2, n = 20) {
  const coords = [];
  for (let i = 0; i <= n; i++) {
    coords.push(greatCirclePoint(lat1, lon1, lat2, lon2, i / n));
  }
  return coords;
}

/** Chain multiple segments (each [lat, lon]); returns single coordinates array. */
function chainSegments(segments, pointsPerSegment = 18) {
  const out = [];
  for (let i = 0; i < segments.length - 1; i++) {
    const [lat1, lon1] = segments[i];
    const [lat2, lon2] = segments[i + 1];
    const pts = greatCircleSegment(lat1, lon1, lat2, lon2, pointsPerSegment);
    if (i === 0) out.push(...pts);
    else out.push(...pts.slice(1));
  }
  return out;
}

// Hub coordinates (lat, lon) — OpenFlights/OurAirports; major hubs
const hubs = {
  JFK: [40.6398, -73.7789],
  LHR: [51.4700, -0.4543],
  FRA: [50.0379, 8.5622],
  AMS: [52.3105, 4.7683],
  DXB: [25.2532, 55.3644],
  DEL: [28.5562, 77.0881],
  BOM: [19.0896, 72.8656],
  SIN: [1.3644, 103.9915],
  BKK: [13.6900, 100.7501],
  NRT: [35.7647, 140.3863],
  PVG: [31.1434, 121.8052],
  LAX: [33.9425, -118.4081],
  SFO: [37.619, -122.375],
  SYD: [-33.9461, 151.1772],
  EZE: [-34.8222, -58.5358],
  MIA: [25.7954, -80.2901],
  NBO: [-1.3192, 36.9253],
  ADD: [8.9779, 38.7991],
};

const updates = {};

// 1) North Atlantic Air Bridge — JFK to LHR (primary NAT corridor)
updates.air_northatlantic_bridge = greatCircleSegment(
  ...hubs.JFK,
  ...hubs.LHR,
  25
);

// 2) Europe–Gulf–India — FRA → DXB → DEL
updates.air_europe_gulf_india = chainSegments(
  [hubs.FRA, hubs.DXB, hubs.DEL],
  20
);

// 3) Gulf–Southeast Asia — DXB → SIN
updates.air_gulf_seasia = greatCircleSegment(
  ...hubs.DXB,
  ...hubs.SIN,
  22
);

// 4) Transpacific East Asia–US West — NRT (Tokyo) → LAX
updates.air_transpacific_eastasia_usw = greatCircleSegment(
  ...hubs.NRT,
  ...hubs.LAX,
  25
);

// 5) Intra-Europe Core — LHR → FRA → WAW (Warsaw 52.1657, 20.9671)
const WAW = [52.1657, 20.9671];
updates.air_intra_europe_core = chainSegments(
  [hubs.LHR, hubs.FRA, WAW],
  12
);

// 6) North America Coast-to-Coast — SFO → JFK
updates.air_northamerica_coast_coast = greatCircleSegment(
  ...hubs.SFO,
  ...hubs.JFK,
  18
);

// 7) Southeast Asia–Australia — SIN → SYD
updates.air_seasia_australia = greatCircleSegment(
  ...hubs.SIN,
  ...hubs.SYD,
  22
);

// 8) LATAM–US — EZE → MIA
updates.air_latam_us = greatCircleSegment(
  ...hubs.EZE,
  ...hubs.MIA,
  20
);

// 9) East Asia–Europe Polar — PVG → FRA (over pole: high-lat waypoints)
// Great circle PVG–FRA goes north; we use direct great circle (passes over Siberia/Russia)
updates.air_eastasia_europe_polar = greatCircleSegment(
  ...hubs.PVG,
  ...hubs.FRA,
  28
);

// 10) India–Gulf–East Africa — BOM → DXB → NBO
updates.air_india_africa_gulf = chainSegments(
  [hubs.BOM, hubs.DXB, hubs.NBO],
  18
);

console.log(JSON.stringify(updates, null, 2));
