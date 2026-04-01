# Latest Lens Recognition Audit

- tenant: `igor@nordicvision.org`
- generatedAt: `2026-03-24T05:12:57Z`
- lensesAudited: `8`

## Goal

Audit the **first stage only**: how the system recognizes axes, objects, and scope volume from a natural-language request plus existing lens context, before full multi-agent simulation.

## Summary

- axis grounding matched on `8/8` lenses
- object grounding matched strongly on `8/8` lenses
- volume/profile classification matched on `8/8` lenses

## Per Lens

### China Aerospace Science and Technology Corporation

- `lensId`: `UQPGaI89O3Mr2WQSMYXS`
- `projectId`: `DRGeH8tCC0EIoRpLlv3D`
- query used: `Space Industry Supply Chain(s) / Value chain(s) in BRICS+ or at least GCC`
- expected axes: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`
- recognized primary axis: `physical_flow/supply`
- axis overlap: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`
- expected objects: `/domain/v1/supply-chain/geo/brics-gcc/sector/space`, `/fac/in/satish-dhawan-space-centre`, `/org/ru/roscosmos`, `/market/brics/aerospace/satellite-services`, `/org/cn/casc`, `/org/ae/mbrsc`, `/loc/brics`
- recognized objects: `/domain/v1/supply-chain/geo/brics-gcc/sector/space`, `/gov/in/isro`, `/org/ru/roscosmos`, `/gov/cn/cnsa`, `/loc/brics`, `/org/ae/mbrsc`, `/org/cn/casc`, `/sphere/other/aerospace`, `/sphere/other/brics`, `/sphere/other/gcc`, `/gov/sa/saudi-space-agency`, `/fac/in/satish-dhawan-space-centre`
- object overlap: `/domain/v1/supply-chain/geo/brics-gcc/sector/space`, `/fac/in/satish-dhawan-space-centre`, `/org/ru/roscosmos`, `/org/cn/casc`, `/org/ae/mbrsc`, `/loc/brics`
- expected profile: `sector_network`
- recognized profile: `sector_network`
- generic agent classes: `consumer`, `facility_operator`, `firm`, `logistics_operator`, `market_intelligence_analyst`, `procurement_director`, `regional_official`, `supplier`
- recognition summary: Primary axis: physical_flow/supply. Main objects: /domain/v1/supply-chain/geo/brics-gcc/sector/space, /gov/in/isro, /org/ru/roscosmos. Scope profile: sector_network.

### Helium · Natural Gas

- `lensId`: `XvFkNQGFApSQJ8waIA1m`
- `projectId`: `EB3GYD7UaacWTFTIfH9z`
- query used: `Identify the main exposure contours and critical objects for Helium · Natural Gas.`
- expected axes: `physical_flow/supply`
- recognized primary axis: `physical_flow/supply`
- axis overlap: `physical_flow/supply`
- expected objects: `/product/finished/mechanical/assembly/helium`, `/product/raw/hydrocarbon/extraction/natural-gas`
- recognized objects: `/product/finished/mechanical/assembly/helium`, `/product/raw/hydrocarbon/extraction/natural-gas`
- object overlap: `/product/finished/mechanical/assembly/helium`, `/product/raw/hydrocarbon/extraction/natural-gas`
- expected profile: `focused_object`
- recognized profile: `focused_object`
- generic agent classes: `consumer`, `firm`, `logistics_operator`, `market_intelligence_analyst`, `procurement_director`, `supplier`
- recognition summary: Primary axis: physical_flow/supply. Main objects: /product/finished/mechanical/assembly/helium, /product/raw/hydrocarbon/extraction/natural-gas. Scope profile: focused_object.

### Australia

- `lensId`: `BakrpGUDgGR0wGF8tJXZ`
- `projectId`: `EB3GYD7UaacWTFTIfH9z`
- query used: `Identify the main exposure contours and critical objects for Australia.`
- expected axes: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`, `financial/settlement`
- recognized primary axis: `logistics/capacity`
- axis overlap: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`, `financial/settlement`
- expected objects: `/loc/au`, `/org/au/fortescue`, `/fac/au/port-hedland`, `/domain/v1/supply-chain/geo/au/material/raw-materials`, `/fac/au/pilbara-operations`, `/product/raw/mineral/extraction/iron-ore`, `/org/au/bhp`, `/org/au/rio-tinto-plc`
- recognized objects: `/loc/au`, `/org/au/fortescue`, `/org/au/bhp`, `/sphere/energy`, `/sphere/other`, `/sphere/trade`, `/domain/v1/supply-chain/geo/au/material/raw-materials`, `/fac/au/port-hedland`, `/market/au/mining/critical-minerals`, `/org/au/rio-tinto-plc`, `/fac/au/pilbara-operations`, `/product/raw/mineral/extraction/iron-ore`
- object overlap: `/loc/au`, `/org/au/fortescue`, `/fac/au/port-hedland`, `/domain/v1/supply-chain/geo/au/material/raw-materials`, `/fac/au/pilbara-operations`, `/product/raw/mineral/extraction/iron-ore`, `/org/au/bhp`, `/org/au/rio-tinto-plc`
- expected profile: `sector_network`
- recognized profile: `sector_network`
- generic agent classes: `bank`, `facility_operator`, `firm`, `logistics_operator`, `pr_agent`, `procurement_director`, `warehouse_operator`
- recognition summary: Primary axis: logistics/capacity. Main objects: /loc/au, /org/au/fortescue, /org/au/bhp. Scope profile: sector_network.

### French Automotive Industry

- `lensId`: `dHZSv4TE0Zanfdrz1YUv`
- `projectId`: `p0l7v6deNSoqe47ao1sl`
- query used: `French whole automotive industry and its supply chain`
- expected axes: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`, `financial/settlement`
- recognized primary axis: `physical_flow/supply`
- axis overlap: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`, `financial/settlement`
- expected objects: `/domain/v1/sector-overview/geo/fr/industry/automotive`, `/domain/v1/supply-chain/geo/fr/industry/automotive`, `/org/fr/michelin-sca`, `/org/fr/renault-group-sa`, `/product/component/electronic/manufacturing/automotive-sensor`, `/product/component/electronic/manufacturing/ev-battery-cells`, `/product/component/mechanical/manufacturing/tire`, `/market/fr/industry/automotive`
- recognized objects: `/domain/v1/sector-overview/geo/fr/industry/automotive`, `/domain/v1/supply-chain/geo/fr/industry/automotive`, `/product/component/mechanical/manufacturing/tire`, `/market/fr/industry/automotive`, `/org/fr/forvia`, `/sphere/other`, `/org/fr/renault-group`, `/org/fr/michelin-sca`, `/org/fr/renault-group-sa`, `/product/component/electronic/manufacturing/automotive-sensor`, `/product/component/electronic/manufacturing/ev-battery-cells`
- object overlap: `/domain/v1/sector-overview/geo/fr/industry/automotive`, `/domain/v1/supply-chain/geo/fr/industry/automotive`, `/org/fr/michelin-sca`, `/org/fr/renault-group-sa`, `/product/component/electronic/manufacturing/automotive-sensor`, `/product/component/electronic/manufacturing/ev-battery-cells`, `/product/component/mechanical/manufacturing/tire`, `/market/fr/industry/automotive`
- expected profile: `sector_network`
- recognized profile: `sector_network`
- generic agent classes: `consumer`, `firm`, `logistics_operator`, `market_intelligence_analyst`, `procurement_director`, `supplier`
- recognition summary: Primary axis: physical_flow/supply. Main objects: /domain/v1/sector-overview/geo/fr/industry/automotive, /domain/v1/supply-chain/geo/fr/industry/automotive, /product/component/mechanical/manufacturing/tire. Scope profile: sector_network.

### Hayel Saeed Anam Group

- `lensId`: `gHuvNWmL58FANfJzpxLx`
- `projectId`: `xd0EAoHYvU6lAzu5l5cC`
- query used: `Yemeni retail grocery supply chain detailed`
- expected axes: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`, `financial/settlement`
- recognized primary axis: `logistics/capacity`
- axis overlap: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`, `financial/settlement`
- expected objects: `/domain/v1/supply-chain/geo/ye/sector/retail-grocery`, `/loc/ye`, `/loc/ye/hodeidah`, `/product/finished/mechanical/assembly/vegetable-oil`, `/product/finished/mechanical/assembly/wheat-flour`, `/org/ye/hsa-group`, `/org/ye/thabet-group`, `/fac/ye/port-of-hodeidah`
- recognized objects: `/domain/v1/supply-chain/geo/ye/sector/retail-grocery`, `/loc/ye`, `/loc/ye/hodeidah`, `/market/ye/retail/grocery`, `/sphere/logistics`, `/sphere/other`, `/fac/ye/port-of-aden`, `/fac/ye/port-of-hodeidah`, `/org/na/world-food-programme-yemen`, `/org/ye/hsa-group`, `/org/ye/thabet-group`, `/product/finished/mechanical/assembly/vegetable-oil`
- object overlap: `/domain/v1/supply-chain/geo/ye/sector/retail-grocery`, `/loc/ye`, `/loc/ye/hodeidah`, `/product/finished/mechanical/assembly/vegetable-oil`, `/org/ye/hsa-group`, `/org/ye/thabet-group`, `/fac/ye/port-of-hodeidah`
- expected profile: `sector_network`
- recognized profile: `sector_network`
- generic agent classes: `bank`, `facility_operator`, `firm`, `logistics_operator`, `pr_agent`, `procurement_director`, `warehouse_operator`
- recognition summary: Primary axis: logistics/capacity. Main objects: /domain/v1/supply-chain/geo/ye/sector/retail-grocery, /loc/ye, /loc/ye/hodeidah. Scope profile: sector_network.

### Stellantis Italy

- `lensId`: `lWQgcy5xuKw9fZzz45Zn`
- `projectId`: `p0l7v6deNSoqe47ao1sl`
- query used: `Italian automotive industry supply chain`
- expected axes: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`, `financial/settlement`
- recognized primary axis: `physical_flow/supply`
- axis overlap: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`, `financial/settlement`
- expected objects: `/domain/v1/supply-chain/geo/it/sector/automotive`, `/fac/ferrari/maranello-factory`, `/fac/na/stuttgart-logistics-hub`, `/fac/norsk-hydro/sunndal-smelter`, `/product/component/mechanical/manufacturing/suspension-systems`, `/product/finished/mechanical/assembly/original-equipment-tire`, `/org/it/stellantis`, `/market/it/manufacturing/automotive`
- recognized objects: `/domain/v1/supply-chain/geo/it/sector/automotive`, `/loc/it/na/turin`, `/market/it/manufacturing/automotive`, `/org/it/stellantis`, `/sphere/logistics`, `/sphere/other`, `/deposit/lithium-ore/yichun-lithium-mine`, `/fac/ferrari/maranello-factory`, `/fac/na/stuttgart-logistics-hub`, `/fac/norsk-hydro/sunndal-smelter`, `/product/component/mechanical/manufacturing/suspension-systems`, `/product/finished/mechanical/assembly/original-equipment-tire`
- object overlap: `/domain/v1/supply-chain/geo/it/sector/automotive`, `/fac/ferrari/maranello-factory`, `/fac/na/stuttgart-logistics-hub`, `/fac/norsk-hydro/sunndal-smelter`, `/product/component/mechanical/manufacturing/suspension-systems`, `/product/finished/mechanical/assembly/original-equipment-tire`, `/org/it/stellantis`, `/market/it/manufacturing/automotive`
- expected profile: `sector_network`
- recognized profile: `sector_network`
- generic agent classes: `consumer`, `facility_operator`, `firm`, `logistics_operator`, `market_intelligence_analyst`, `procurement_director`, `supplier`
- recognition summary: Primary axis: physical_flow/supply. Main objects: /domain/v1/supply-chain/geo/it/sector/automotive, /loc/it/na/turin, /market/it/manufacturing/automotive. Scope profile: sector_network.

### Qatarenergy

- `lensId`: `Pa5qjiSQDeZyKqCnrlEb`
- `projectId`: `xd0EAoHYvU6lAzu5l5cC`
- query used: `Map the supply chain for Qatari LNG delivered to Europe, including producers, liquefaction facilities, export terminals, shipping routes through the Strait of Hormuz, LNG carriers, receiving terminals in Europe, gas market dependencies, major power-intensive industrial consumers, and key downstream manufacturing sectors.`
- expected axes: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`, `financial/settlement`
- recognized primary axis: `logistics/capacity`
- axis overlap: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`, `financial/settlement`
- expected objects: `/domain/v1/supply-chain/geo/qa-eu/product/lng`, `/fac/be/zeebrugge-lng-terminal`, `/fac/gb/south-hook-lng-terminal`, `/loc/qa`, `/market/global/energy/lng`, `/org/qa/qatarenergy`, `/product/finished/mechanical/assembly/liquefied-natural-gas-lng`, `/fac/qa/ras-laffan-industrial-city`
- recognized objects: `/domain/v1/supply-chain/geo/qa-eu/product/lng`, `/org/qa/qatarenergy`, `/loc/qa`, `/market/global/energy/lng`, `/sphere/energy`, `/sphere/geopolitics`, `/sphere/logistics`, `/fac/qa/ras-laffan-industrial-city`, `/fac/be/zeebrugge-lng-terminal`, `/fac/gb/south-hook-lng-terminal`, `/product/finished/mechanical/assembly/liquefied-natural-gas-lng`
- object overlap: `/domain/v1/supply-chain/geo/qa-eu/product/lng`, `/fac/be/zeebrugge-lng-terminal`, `/fac/gb/south-hook-lng-terminal`, `/loc/qa`, `/market/global/energy/lng`, `/org/qa/qatarenergy`, `/product/finished/mechanical/assembly/liquefied-natural-gas-lng`, `/fac/qa/ras-laffan-industrial-city`
- expected profile: `sector_network`
- recognized profile: `sector_network`
- generic agent classes: `bank`, `facility_operator`, `firm`, `logistics_operator`, `pr_agent`, `procurement_director`, `warehouse_operator`
- recognition summary: Primary axis: logistics/capacity. Main objects: /domain/v1/supply-chain/geo/qa-eu/product/lng, /org/qa/qatarenergy, /loc/qa. Scope profile: sector_network.

### ASML Holding N.V.

- `lensId`: `X1vHPtV0R0z9hR4vK673`
- `projectId`: `p0l7v6deNSoqe47ao1sl`
- query used: `Europe wide semiconductor industry supply chain`
- expected axes: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`, `financial/settlement`
- recognized primary axis: `physical_flow/supply`
- axis overlap: `physical_flow/supply`, `logistics/capacity`, `physical_flow/demand`, `financial/settlement`
- expected objects: `/domain/v1/supply-chain/geo/eu/sector/semiconductors`, `/loc/nl/na/na`, `/org/de/carl-zeiss-smt-gmbh`, `/org/de/infineon-technologies-ag`, `/org/de/trumpf-se-co-kg-gmbh`, `/org/nl/asml-holding-nv`, `/product/component/electronic/manufacturing/euv-lithography-system`, `/org/nl/asml`
- recognized objects: `/domain/v1/supply-chain/geo/eu/sector/semiconductors`, `/loc/nl/na/na`, `/market/eu/semiconductors/general`, `/org/de/infineon`, `/org/nl/asml`, `/sphere/other/europe`, `/sphere/other/semiconductors`, `/org/de/carl-zeiss-smt-gmbh`, `/org/de/infineon-technologies-ag`, `/org/de/trumpf-se-co-kg-gmbh`, `/org/nl/asml-holding-nv`, `/product/component/electronic/manufacturing/euv-lithography-system`
- object overlap: `/domain/v1/supply-chain/geo/eu/sector/semiconductors`, `/loc/nl/na/na`, `/org/de/carl-zeiss-smt-gmbh`, `/org/de/infineon-technologies-ag`, `/org/de/trumpf-se-co-kg-gmbh`, `/org/nl/asml-holding-nv`, `/product/component/electronic/manufacturing/euv-lithography-system`, `/org/nl/asml`
- expected profile: `sector_network`
- recognized profile: `sector_network`
- generic agent classes: `consumer`, `firm`, `logistics_operator`, `market_intelligence_analyst`, `procurement_director`, `supplier`
- recognition summary: Primary axis: physical_flow/supply. Main objects: /domain/v1/supply-chain/geo/eu/sector/semiconductors, /loc/nl/na/na, /market/eu/semiconductors/general. Scope profile: sector_network.

## Primary Changes Applied

- Added a simulation input recognition layer that infers `axisCandidates`, `objectCandidates`, and `volumeProfile` from natural-language query plus existing lens context.
- Wired recognition into `CreateRun`, so partially specified simulation requests now auto-fill primary axis, actor paths, location path, and seed scope.
- Upgraded generic agent assignment so non-hot-path scenarios use recognition-aware role mixes instead of the old flat `consumer/firm/media/regulator/bank` roster.

## Files

- [`/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/simulation/recognition.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/simulation/recognition.go)
- [`/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/simulation/service.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/simulation/service.go)
- [`/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/models/simulation.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/horizon/models/simulation.go)
- [`/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/processor.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/processor.go)
- [`/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/population_preview.go`](/Users/kirisyuk/cursor/sanskrite/backend/internal/agentmesh/simulation/population_preview.go)
