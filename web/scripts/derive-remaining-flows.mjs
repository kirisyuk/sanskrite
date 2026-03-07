#!/usr/bin/env node
/**
 * Derives road, rail, inland_waterway, intermodal, internet_cable LineString coordinates
 * from known corridor waypoints (cities, ports, key nodes). Uses great-circle interpolation.
 * Output: JSON files in web/src/data/ for use by refine-transport-flows.mjs
 *
 * Usage: node scripts/derive-remaining-flows.mjs
 */

import { writeFileSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const dataDir = join(__dirname, '../src/data');

function toRad(x) {
  return (x * Math.PI) / 180;
}
function toDeg(x) {
  return (x * 180) / Math.PI;
}

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
  if (d === 0) return [toDeg(lon1), toDeg(lat1)];
  const A = Math.sin((1 - t) * d) / Math.sin(d);
  const B = Math.sin(t * d) / Math.sin(d);
  const x = A * Math.cos(lat1r) * Math.cos(lon1r) + B * Math.cos(lat2r) * Math.cos(lon2r);
  const y = A * Math.cos(lat1r) * Math.sin(lon1r) + B * Math.cos(lat2r) * Math.sin(lon2r);
  const z = A * Math.sin(lat1r) + B * Math.sin(lat2r);
  const lat = Math.atan2(z, Math.sqrt(x * x + y * y));
  const lon = Math.atan2(y, x);
  return [toDeg(lon), toDeg(lat)];
}

function greatCircleSegment(lat1, lon1, lat2, lon2, n = 20) {
  const coords = [];
  for (let i = 0; i <= n; i++) {
    coords.push(greatCirclePoint(lat1, lon1, lat2, lon2, i / n));
  }
  return coords;
}

/** segments: array of [lat, lon]; returns [[lon,lat], ...] */
function chainSegments(segments, pointsPerSegment = 14) {
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

// Waypoints [lat, lon] — major cities/ports along corridors (OSM/Wikipedia)
const W = {
  // Europe (port coords where applicable)
  Rotterdam: [51.885, 4.2867],
  Genoa: [44.4056, 8.9463],
  Milan: [45.4642, 9.1900],
  Basel: [47.5596, 7.5886],
  Karlsruhe: [49.0069, 8.4037],
  Lyon: [45.7640, 4.8357],
  Paris: [48.8566, 2.3522],
  Barcelona: [41.3851, 2.1734],
  Madrid: [40.4168, -3.7038],
  Lisbon: [38.7223, -9.1393],
  Cologne: [50.9375, 6.9603],
  Frankfurt: [50.1109, 8.6821],
  Munich: [48.1351, 11.5820],
  Vienna: [48.2082, 16.3738],
  Warsaw: [52.2297, 21.0122],
  Gdansk: [54.3520, 18.6466],
  Ljubljana: [46.0569, 14.5058],
  Trieste: [45.6495, 13.7768],
  Verona: [45.4384, 10.9916],
  Bologna: [44.4949, 11.3426],
  Naples: [40.8518, 14.2681],
  Istanbul: [41.0082, 28.9784],
  Edirne: [41.6771, 26.5557],
  Sofia: [42.6977, 23.3219],
  Belgrade: [44.7866, 20.4489],
  Budapest: [47.4979, 19.0402],
  // US
  Miami: [25.7617, -80.1918],
  Jacksonville: [30.3322, -81.6557],
  Washington: [38.9072, -77.0369],
  Philadelphia: [39.9526, -75.1652],
  NewYork: [40.7128, -74.0060],
  Boston: [42.3601, -71.0589],
  Laredo: [27.5036, -99.5075],
  SanAntonio: [29.4241, -98.4936],
  Dallas: [32.7767, -96.7970],
  OklahomaCity: [35.4676, -97.5164],
  KansasCity: [39.0997, -94.5786],
  Minneapolis: [44.9778, -93.2650],
  SanFrancisco: [37.7749, -122.4194],
  Sacramento: [38.5816, -121.4944],
  Denver: [39.7392, -104.9903],
  Omaha: [41.2565, -95.9345],
  Chicago: [41.8781, -87.6298],
  // Asia
  Shanghai: [31.2304, 121.4737],
  Hangzhou: [30.2741, 120.1551],
  Guangzhou: [23.1291, 113.2644],
  HongKong: [22.3193, 114.1694],
  Delhi: [28.7041, 77.1025],
  Mumbai: [19.0760, 72.8777],
  Chennai: [13.0827, 80.2707],
  Kolkata: [22.5726, 88.3639],
  Rewari: [28.1990, 76.6193],
  Ahmedabad: [23.0225, 72.5714],
  Ludhiana: [30.9010, 75.8573],
  XiAn: [34.3416, 108.9398],
  Chongqing: [29.5630, 106.5516],
  Zhengzhou: [34.7466, 113.6253],
  Lanzhou: [36.0611, 103.8343],
  Urumqi: [43.8256, 87.6168],
  Almaty: [43.2220, 76.8512],
  Moscow: [55.7558, 37.6173],
  Minsk: [53.9045, 27.5615],
  Yekaterinburg: [56.8389, 60.6057],
  Omsk: [54.9885, 73.3242],
  Novosibirsk: [55.0084, 82.9357],
  Irkutsk: [52.2897, 104.2806],
  Vladivostok: [43.1198, 131.8869],
  NizhnyNovgorod: [56.2965, 43.9361],
  // Gulf / Middle East
  Kuwait: [29.3759, 47.9774],
  Dammam: [26.441, 50.1485],
  Doha: [25.015, 51.602],
  Dubai: [25.01126, 55.06116],
  Muscat: [23.5880, 58.3829],
  Riyadh: [24.7136, 46.6753],
  // Africa / Americas
  Rio: [-22.9068, -43.1729],
  SaoPaulo: [-23.5505, -46.6333],
  Curitiba: [-25.4284, -49.2733],
  PortoAlegre: [-30.0346, -51.2177],
  Santos: [-23.9608, -46.3332],
  Brasilia: [-15.8267, -47.9218],
  Belem: [-1.4560, -48.5039],
  Caracas: [10.4806, -66.9036],
  Bogota: [4.7110, -74.0721],
  Quito: [-0.1807, -78.4678],
  Lima: [-12.0464, -77.0428],
  LaPaz: [-16.5000, -68.1500],
  Santiago: [-33.4489, -70.6693],
  // Rivers (mouth → source or key points)
  RhineMouth: [51.9819, 4.0933],
  RhineBasel: [47.5596, 7.5886],
  DanubeMouth: [45.1833, 29.7333],
  DanubeVienna: [48.2082, 16.3738],
  MississippiMouth: [29.1572, -89.2492],
  MississippiStLouis: [38.6270, -90.1994],
  MississippiMinneapolis: [44.9778, -93.2650],
  YangtzeMouth: [31.2304, 121.4737],
  YangtzeWuhan: [30.5928, 114.3055],
  YangtzeChongqing: [29.5630, 106.5516],
  AmazonMouth: [-1.4927, -48.4896],
  AmazonManaus: [-3.1190, -60.0217],
  AmazonIquitos: [-3.7434, -73.2480],
  NileMouth: [31.1990, 29.8942],
  NileCairo: [30.0444, 31.2357],
  NileKhartoum: [15.5007, 32.5599],
  // Ports / intermodal
  Duisburg: [51.4344, 6.7623],
  Ruhr: [51.4818, 7.2162],
  JebelAli: [24.9854, 55.0273],
  DWC: [24.8967, 55.1611],
  Singapore: [1.3521, 103.8198],
  Johor: [1.4927, 103.7414],
  LosAngeles: [33.7701, -118.1937],
  LongBeach: [33.7701, -118.1897],
  Colon: [9.3589, -79.9000],
  Balboa: [8.9480, -79.5667],
  // Cable landing cities
  London: [51.5074, -0.1278],
  Marseille: [43.2965, 5.3698],
  Fortaleza: [-3.7172, -38.5433],
  Tokyo: [35.6762, 139.6503],
  Oregon: [45.5231, -124.0675],
  Chiba: [35.6074, 140.1063],
  HongKongCable: [22.3193, 114.1694],
  Alexandria: [31.2001, 29.9187],
  Perth: [-31.9505, 115.8605],
  Auckland: [-36.8509, 174.7645],
  Manila: [14.5995, 120.9842],
  Lagos: [6.5244, 3.3792],
  CapeTown: [-33.9249, 18.4241],
  MumbaiCable: [19.0760, 72.8777],
  BuenosAires: [-34.6037, -58.3816],
  Valparaiso: [-33.0472, -71.6127],
  SanBernardino: [34.1083, -117.2898],
  Nanjing: [32.0603, 118.7969],
  Sydney: [-33.9461, 151.1772],
  // Russia & Northern Sea Route (port coords)
  Murmansk: [68.98, 33.056],
  Dikson: [73.5069, 80.5464],
  Tiksi: [71.65, 128.8667],
  Pevek: [69.70472, 170.26361],
  BeringStrait: [66.0, -169.0],
  StPetersburg: [59.9343, 30.3351],
  Helsinki: [60.1699, 24.9384],
  Minsk: [53.9045, 27.5615],
  Beijing: [39.9042, 116.4074],
  Novorossiysk: [44.7235, 37.7685],
  Khorgos: [44.22, 80.42],
  Nadym: [65.5333, 72.5167],
  Blagoveshchensk: [50.2796, 127.5405],
  Yakutsk: [62.0355, 129.6755],
  Astrakhan: [46.3497, 48.0358],
  Volgograd: [48.7080, 44.5133],
  // Central Asia
  Tashkent: [41.2995, 69.2401],
  Bishkek: [42.8746, 74.5698],
  Samarkand: [39.6542, 66.9597],
  Dushanbe: [38.5598, 68.7738],
  Ashgabat: [37.9601, 58.3261],
  ObMouth: [66.5, 69.0],
};

function seg(...waypointNames) {
  const pts = waypointNames.map((k) => W[k]);
  return chainSegments(pts, 12);
}

const road = {};
road.road_rhine_alpine = seg('Genoa', 'Milan', 'Basel', 'Karlsruhe', 'Rotterdam');
road.road_iberia_france_eu = seg('Lisbon', 'Madrid', 'Barcelona', 'Lyon', 'Paris');
road.road_us_i95_megacorridor = seg('Miami', 'Jacksonville', 'Washington', 'Philadelphia', 'NewYork', 'Boston');
road.road_us_i35_nafta = seg('Laredo', 'SanAntonio', 'Dallas', 'OklahomaCity', 'KansasCity', 'Minneapolis');
road.road_china_coast_inland = seg('Shanghai', 'Hangzhou', 'Guangzhou', 'HongKong');
road.road_india_golden_quad = seg('Delhi', 'Mumbai', 'Chennai', 'Kolkata', 'Delhi');
road.road_gcc_backbone = seg('Kuwait', 'Dammam', 'Doha', 'Dubai', 'Muscat');
road.road_brazil_southeast = seg('Rio', 'SaoPaulo', 'Curitiba', 'PortoAlegre');
road.road_turkiye_eu_gateway = seg('Istanbul', 'Edirne', 'Sofia', 'Belgrade', 'Budapest');
road.road_andean_spine = seg('Caracas', 'Bogota', 'Quito', 'Lima', 'LaPaz', 'Santiago');
road.road_russia_europe_asia = seg('Moscow', 'Yekaterinburg', 'Omsk', 'Novosibirsk', 'Irkutsk', 'Vladivostok');
road.road_central_asia_corridor = seg('Almaty', 'Bishkek', 'Tashkent', 'Samarkand');

const rail = {};
rail.rail_transsiberian_mainline = seg('Moscow', 'NizhnyNovgorod', 'Yekaterinburg', 'Omsk', 'Novosibirsk', 'Irkutsk', 'Vladivostok');
rail.rail_china_europe_landbridge = seg('Chongqing', 'XiAn', 'Lanzhou', 'Urumqi', 'Khorgos', 'Almaty', 'Moscow', 'Minsk', 'Duisburg');
rail.rail_eu_northsea_med = seg('Rotterdam', 'Cologne', 'Frankfurt', 'Munich', 'Verona', 'Bologna', 'Naples');
rail.rail_eu_baltic_adriatic = seg('Gdansk', 'Warsaw', 'Vienna', 'Ljubljana', 'Trieste');
rail.rail_us_transcontinental = seg('SanFrancisco', 'Sacramento', 'Denver', 'Omaha', 'Chicago', 'NewYork');
rail.rail_india_dfc_west = seg('Delhi', 'Rewari', 'Ahmedabad', 'Mumbai');
rail.rail_india_dfc_east = seg('Ludhiana', 'Delhi', 'Kolkata');
rail.rail_gcc_northsouth = seg('Kuwait', 'Riyadh', 'Dammam', 'Dubai');
rail.rail_kazakh_transit = seg('Urumqi', 'Almaty', 'Moscow');
rail.rail_brazil_northsouth = seg('Santos', 'SaoPaulo', 'Brasilia', 'Belem');
rail.rail_russia_europe = seg('Moscow', 'StPetersburg', 'Helsinki');
rail.rail_central_asia_south = seg('Tashkent', 'Samarkand', 'Dushanbe');

const inland_waterway = {};
inland_waterway.waterway_rhine_axis = seg('RhineMouth', 'Rotterdam', 'Cologne', 'Basel');
inland_waterway.waterway_danube_axis = seg('DanubeMouth', 'Belgrade', 'Budapest', 'Vienna');
inland_waterway.waterway_mississippi_axis = seg('MississippiMouth', 'MississippiStLouis', 'MississippiMinneapolis');
inland_waterway.waterway_yangtze_axis = seg('YangtzeMouth', 'YangtzeWuhan', 'YangtzeChongqing');
inland_waterway.waterway_amazon_axis = seg('AmazonMouth', 'AmazonManaus', 'AmazonIquitos');
inland_waterway.waterway_nile_axis = seg('NileMouth', 'NileCairo', 'NileKhartoum');
inland_waterway.waterway_ob_irtysh_axis = seg('ObMouth', 'Novosibirsk', 'Omsk');
inland_waterway.waterway_volga_caspian_axis = seg('Astrakhan', 'Volgograd', 'NizhnyNovgorod');

const intermodal = {};
intermodal.intermodal_rotterdam_ruhr = seg('Rotterdam', 'Duisburg', 'Ruhr');
intermodal.intermodal_duisburg_hub = seg('Rotterdam', 'Duisburg', 'Cologne');
intermodal.intermodal_singapore_johor = seg('Singapore', 'Johor');
intermodal.intermodal_jebelali_dwc = seg('JebelAli', 'Dubai', 'DWC');
intermodal.intermodal_la_lb_inland = seg('LosAngeles', 'LongBeach', 'SanBernardino');
intermodal.intermodal_panama_colon_balboa = seg('Colon', 'Balboa');
intermodal.intermodal_shanghai_yangtze_delta = seg('Shanghai', 'Hangzhou', 'Nanjing');
intermodal.intermodal_mumbai_jnpt_dmic = seg('Mumbai', 'Delhi');
intermodal.intermodal_novorossiysk_black_sea = seg('Novorossiysk', 'Istanbul');
intermodal.intermodal_khorgos_landbridge = seg('Khorgos', 'Almaty', 'Tashkent');
intermodal.intermodal_china_europe_silk_road = seg('Shanghai', 'XiAn', 'Lanzhou', 'Urumqi', 'Khorgos', 'Almaty', 'Moscow', 'Duisburg');

const sea = {};
sea.sea_northern_sea_route = chainSegments(
  [W.Murmansk, W.Dikson, W.Tiksi, W.Pevek, W.BeringStrait],
  16
);

const pipeline = {};
pipeline.pipeline_gas_power_of_siberia = chainSegments([W.Nadym, W.Yakutsk, W.Blagoveshchensk, W.Beijing], 12);
pipeline.pipeline_gas_russia_europe = chainSegments([W.Nadym, W.Minsk, W.Warsaw], 12);

const air = {};
air.air_russia_asia = chainSegments([W.Moscow, W.Beijing], 18);
air.air_central_asia_hubs = chainSegments([W.Almaty, W.Tashkent, W.Dubai], 14);

const internet_cable = {};
// Great-circle between landing points (submarine cables follow geodesics)
internet_cable.internet_cable_transatlantic_ny_london = greatCircleSegment(...W.NewYork, ...W.London, 24);
internet_cable.internet_cable_transatlantic_marseille_ny = greatCircleSegment(...W.Marseille, ...W.NewYork, 24);
internet_cable.internet_cable_lisbon_fortaleza = greatCircleSegment(...W.Lisbon, ...W.Fortaleza, 22);
internet_cable.internet_cable_miami_caribbean_panama = chainSegments([W.Miami, W.Colon], 18);
internet_cable.internet_cable_transpacific_la_tokyo = greatCircleSegment(...W.LosAngeles, ...W.Tokyo, 26);
internet_cable.internet_cable_transpacific_oregon_chiba = greatCircleSegment(...W.Oregon, ...W.Chiba, 24);
internet_cable.internet_cable_singapore_hk_tokyo = chainSegments([W.Singapore, W.HongKongCable, W.Tokyo], 16);
internet_cable.internet_cable_sea_me_we_style = chainSegments([W.Marseille, W.Alexandria, W.MumbaiCable, W.Singapore], 20);
internet_cable.internet_cable_marseille_alexandria = greatCircleSegment(...W.Marseille, ...W.Alexandria, 18);
internet_cable.internet_cable_redsea_gulf = chainSegments([W.Alexandria, W.Dubai], 14);
internet_cable.internet_cable_east_africa_india = greatCircleSegment(...W.MumbaiCable, ...W.Lagos, 22);
internet_cable.internet_cable_west_africa_europe = greatCircleSegment(...W.Lagos, ...W.London, 20);
internet_cable.internet_cable_singapore_perth = greatCircleSegment(...W.Singapore, ...W.Perth, 20);
internet_cable.internet_cable_sydney_auckland = greatCircleSegment(...W.Sydney, ...W.Auckland, 16);
internet_cable.internet_cable_japan_ph_singapore = chainSegments([W.Tokyo, W.Manila, W.Singapore], 18);
internet_cable.internet_cable_india_me_europe = chainSegments([W.MumbaiCable, W.Dubai, W.Marseille], 20);
internet_cable.internet_cable_south_america_cone = greatCircleSegment(...W.BuenosAires, ...W.Valparaiso, 14);

// Single merged object for "remaining" updates (incl. NSR, Russia/Central Asia)
const remaining = { ...road, ...rail, ...inland_waterway, ...intermodal, ...internet_cable, ...sea, ...pipeline, ...air };

writeFileSync(join(dataDir, 'road_updates.json'), JSON.stringify(road, null, 2));
writeFileSync(join(dataDir, 'rail_updates.json'), JSON.stringify(rail, null, 2));
writeFileSync(join(dataDir, 'inland_waterway_updates.json'), JSON.stringify(inland_waterway, null, 2));
writeFileSync(join(dataDir, 'intermodal_updates.json'), JSON.stringify(intermodal, null, 2));
writeFileSync(join(dataDir, 'internet_cable_updates.json'), JSON.stringify(internet_cable, null, 2));
writeFileSync(join(dataDir, 'remaining_flow_updates.json'), JSON.stringify(remaining, null, 2));

console.log('Wrote road_updates, rail_updates, inland_waterway_updates, intermodal_updates, internet_cable_updates, remaining_flow_updates to src/data/');
