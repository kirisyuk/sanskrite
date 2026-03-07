# Refined Data Sources for Transport Flows

## Road, rail, inland waterway, intermodal, internet cable

Coordinates for **road**, **rail**, **inland_waterway**, **intermodal**, and **internet_cable** are derived by `scripts/derive-remaining-flows.mjs` from known corridor waypoints (major cities, ports, river mouths). The script uses great-circle interpolation between waypoints and writes:

- `web/src/data/road_updates.json`
- `web/src/data/rail_updates.json`
- `web/src/data/inland_waterway_updates.json`
- `web/src/data/intermodal_updates.json`
- `web/src/data/internet_cable_updates.json`
- `web/src/data/remaining_flow_updates.json` (merged)

Waypoints are from OSM/Wikipedia (decimal lat/lon). To refine further, edit the `W` object in `derive-remaining-flows.mjs` and re-run, then run `refine-transport-flows.mjs`.

### China–Europe / New Silk Road (multimodal)

The Belt and Road / New Silk Road is represented in two ways:

1. **Rail (western corridor)** — `rail_china_europe_landbridge`: **Chongqing** → Xi'an → Lanzhou → Urumqi → **Khorgos** (border hub) → Almaty → Moscow → Minsk → **Duisburg**. Inland rail freight from major Chinese hubs to the main European rail hub (Duisburg). Cargo can reach Chongqing/Xi'an by river (Yangtze), road, or rail from ports.

2. **Intermodal (port–rail)** — `intermodal_china_europe_silk_road`: **Shanghai** (port) → Xi'an (inland hub) → Lanzhou → Urumqi → Khorgos → Almaty → Moscow → Duisburg. Models the sea–rail link: containers from the coast by rail to the landbridge, then via Khorgos to Europe.

3. **Khorgos hub only** — `intermodal_khorgos_landbridge`: Khorgos → Almaty → Tashkent (Central Asia leg from the border).

So the “new replacement for the Great Silk Road” is **multimodal**: it can start from **Chinese ports** (e.g. Shanghai, Ningbo) or **inland hubs** (Chongqing, Zhengzhou, Xi'an); rail runs through **Khorgos** (China–Kazakhstan border), then Kazakhstan, Russia, Belarus, to **Duisburg** (and other EU terminals). The two flows above cover the main western corridor; central (Zhengzhou–Erenhot–Mongolia) and eastern (Manzhouli) corridors can be added as separate routes if needed.

**Regenerate:** `node scripts/derive-remaining-flows.mjs` then `node scripts/refine-transport-flows.mjs`.

### Port endpoints (sea / ferry)

`web/src/data/sea_ferry_port_endpoints.json` defines origin/dest as **[lon, lat]** so that sea and ferry routes start and end at actual ports (e.g. Rotterdam 51.885°N 4.2867°E, Hamad Port Qatar 25.015°N 51.602°E, Jebel Ali 25.011°N 55.061°E, Murmansk, JNPT Mumbai, Port Said, etc.). The refine script uses these when calling searoute and snaps the first/last point of every route that has an entry there.

---

# Refined Data Sources for Pipeline & Power Routes

This document lists **feature IDs** in `web/src/data/transportFlows.v1.json` that use `flowMode` **pipeline_*** or **power_***, and points to public GeoJSON/GIS data sources for real-world LineString geometry. Coordinates produced from these sources are stored in `web/src/data/pipeline_power_updates.json` (shape: `{ "featureId": [[lon, lat], ...] }`).

---

## Feature IDs (pipeline_* and power_*)

| featureId | flowMode | name (from JSON) |
|-----------|----------|------------------|
| `pipeline_oil_saudi_eastwest` | pipeline_oil | Saudi East-West Crude Pipeline Corridor |
| `pipeline_oil_iraq_turkiye` | pipeline_oil | Iraq-Turkiye Oil Export Trunkline |
| `pipeline_oil_cpc_kazakhstan` | pipeline_oil | CPC Kazakhstan-Black Sea Oil Corridor |
| `pipeline_oil_enbridge_mainline` | pipeline_oil | Enbridge Mainline Export Corridor |
| `pipeline_oil_btc_caspian_med` | pipeline_oil | Baku-Tbilisi-Ceyhan Oil Corridor |
| `pipeline_oil_druzhba_europe` | pipeline_oil | Druzhba Europe Oil Distribution Corridor |
| `pipeline_gas_norway_uk_langeled` | pipeline_gas | Norway-UK Langeled Gas Trunkline |
| `pipeline_gas_norway_germany` | pipeline_gas | Norway-Germany Gas Offshore Corridor |
| `pipeline_gas_algeria_spain_medgaz` | pipeline_gas | Algeria-Spain Medgaz Gas Pipeline |
| `pipeline_gas_tanap_tap` | pipeline_gas | TANAP-TAP Gas Corridor |
| `pipeline_gas_qatar_uae_dolphin` | pipeline_gas | Qatar-UAE Dolphin Gas Pipeline |
| `pipeline_gas_centralasia_china` | pipeline_gas | Central Asia-China Gas Corridor |
| `pipeline_gas_transmed_algeria_italy` | pipeline_gas | TransMed Algeria-Italy Gas Corridor |
| `pipeline_h2_h2med_iberia` | pipeline_h2 | H2Med Iberia-France Hydrogen Corridor |
| `pipeline_h2_northsea_backbone` | pipeline_h2 | North Sea Hydrogen Backbone |
| `pipeline_h2_rhine_ruhr` | pipeline_h2 | Rhine-Ruhr Hydrogen Industrial Link |
| `pipeline_h2_neom_jubail` | pipeline_h2 | NEOM-Jubail Hydrogen Corridor |
| `pipeline_co2_northern_lights` | pipeline_co2 | Northern Lights CO2 Offshore Corridor |
| `pipeline_co2_rotterdam_northsea` | pipeline_co2 | Rotterdam-North Sea CO2 Corridor |
| `pipeline_co2_humber_northsea` | pipeline_co2 | Humber-North Sea CO2 Corridor |
| `pipeline_co2_zeebrugge_northsea` | pipeline_co2 | Zeebrugge-North Sea CO2 Corridor |
| `pipeline_co2_gulf_industrial_storage` | pipeline_co2 | Gulf Industrial CO2 Capture Corridor |
| `power_hvdc_north_sea_link` | power_hvdc | North Sea Link HVDC (UK-Norway) |
| `power_hvdc_nordlink` | power_hvdc | NordLink HVDC (Germany-Norway) |
| `power_hvdc_saudi_egypt` | power_hvdc | Saudi-Egypt HVDC Corridor |
| `power_hvdc_china_west_east` | power_hvdc | China West-East UHVDC Backbone |
| `power_hvdc_india_north_south` | power_hvdc | India North-South HVDC |
| `power_hvdc_brazil_north_southeast` | power_hvdc | Brazil North-Southeast HVDC |
| `power_hvdc_basslink_australia` | power_hvdc | Basslink Australia HVDC |
| `power_hvac_eu_core_ring` | power_hvac | EU Core HVAC Grid Ring |
| `power_hvac_gcc_grid` | power_hvac | GCC HVAC Grid Corridor |
| `power_hvac_us_eastern_backbone` | power_hvac | US Eastern HVAC Backbone |
| `power_hvac_india_grid_ring` | power_hvac | India HVAC Grid Ring |
| `power_hvac_maghreb_spine` | power_hvac | Maghreb HVAC Transmission Spine |

---

## Public data sources (URLs and formats)

### Pipelines (oil & gas)

- **Global Energy Monitor – Global Oil Infrastructure Tracker (GOIT)**  
  - Download (form): https://globalenergymonitor.org/projects/global-oil-infrastructure-tracker/download-data/  
  - Covers crude/NGL transmission pipelines; GIS-compatible formats; CC BY 4.0.

- **Global Energy Monitor – Global Gas Infrastructure Tracker (GGIT)**  
  - Download: https://globalenergymonitor.org/projects/global-gas-infrastructure-tracker/download-data/  
  - Gas pipelines and LNG; GIS-compatible; CC BY 4.0.

- **ENTSOG (European gas)**  
  - Maps: https://www.entsog.eu/maps (and uat-showcase.entsog.eu/maps)  
  - Transparency Platform API: https://transparency.entsog.eu/api/  
  - Python client: https://github.com/gasparency/entsog-py  
  - Best for European gas corridors (e.g. TANAP-TAP, TransMed, Medgaz, Norway-UK/DE).

- **GEM Wiki (project-level references)**  
  - e.g. Langeled: https://www.gem.wiki/Langeled_Gas_Pipeline  
  - Useful for endpoint/waypoint names and approximate routes.

### Power (HVDC / HVAC)

- **World Bank / Open Net Zero – Global Transmission Network**  
  - Dataset: https://opennetzero.org/world-bank-grou/global-transmission-network  
  - Shapefile (OSM-derived): https://energydata.info/dataset/e0e878a7-7712-465d-a4c2-69b2b3518513/resource/e0f33c3e-6dab-41d1-919d-d266f300d0bc/download/osm_power_tmm.zip  
  - Global power lines (2016); match by region/name.

- **OpenEnergyDataPortal (ORNL) – Transmission lines**  
  - https://openenergyhub.ornl.gov/explore/dataset/transmission-lines/  
  - US-focused; 69–765 kV; good for US Eastern backbone.

- **California Electric Transmission Lines (GeoJSON)**  
  - https://data.ca.gov/dataset/california-electric-transmission-lines  
  - GeoJSON; regional.

- **HVDC World – map and project list**  
  - https://www.hvdcworld.com/hvdc-map  
  - Project names and regions; no direct GeoJSON.

- **UNDP GeoHub – Australia electricity**  
  - https://undpgeohub.org/ (search “electricity transmission”)  
  - Flatgeobuf/GeoJSON; useful for Basslink and AU grid.

### CO2 / H2 (planned or niche)

- **Northern Lights (Equinor/partners)**  
  - Project: https://www.equinor.com/energy/northern-lights  
  - Route: Øygarden (≈ 4.885°E, 60.56°N) to offshore storage; documented in project/impact docs.

- **H2 / CO2 backbones**  
  - Many are planned; coordinates often from TSO/operator maps or ENTSOG/GIE. No single global GeoJSON; match by name/region to operator or GEM data where available.

### OpenStreetMap (all types)

- **Overpass API**  
  - https://wiki.openstreetmap.org/wiki/Overpass_API  
  - Query `power=line`, `man_made=pipeline`, etc.; export as GeoJSON via Overpass Turbo.  
  - https://wiki.openstreetmap.org/wiki/Overpass_turbo/GeoJSON  

---

## Coordinates already in `pipeline_power_updates.json`

| featureId | source of coordinates |
|-----------|------------------------|
| `pipeline_gas_norway_uk_langeled` | Nyhamna (GEM wiki), Sleipner/Easington (Wikipedia/GeoHack) |
| `pipeline_gas_norway_germany` | Europipe II: Kårstø–Dornum (approximate from public descriptions) |
| `power_hvdc_nordlink` | Tonstad–Wilster (Wikipedia/GeoHack) |
| `power_hvdc_north_sea_link` | Blyth–Kvilldal (North Sea Link project / GeoHack) |
| `pipeline_co2_northern_lights` | Øygarden to offshore (Equinor/Northern Lights project) |

All other feature IDs above still need matching geometry from the sources in this document (GEM download, ENTSOG, World Bank SHP, OSM Overpass, or operator maps). After obtaining LineStrings, add them to `pipeline_power_updates.json` as `"featureId": [[lon, lat], ...]`.
