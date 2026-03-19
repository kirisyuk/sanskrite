# CriticalChain — Supply Chain Monitoring

CriticalChain monitors supply chains for critical materials (lithium, cobalt, REE, helium, silicon, crude oil, natural gas). Data pipeline runs every 6 hours via Cloud Scheduler.

## API Keys & Secrets (Secret Manager)

Adapters use environment variables or Secret Manager. When a key is missing, the adapter returns empty results and logs a warning (graceful degradation).

| Variable | Adapter | Purpose |
|----------|---------|---------|
| `GEMINI_API_KEY` | Worker 8 (scoring) | Gemini API key — Search Grounding for risk scoring (env or Secret Manager `gemini_api_key`) |
| `EIA_API_KEY` | EIA | US Energy Information Administration — oil/gas statistics |
| `UN_COMTRADE_KEY` | Comtrade | UN Comtrade+ — trade flows (~$5K/year commercial) |
| `GLEIF_*` | GLEIF | Global Legal Entity Identifier — canonical org IDs |
| ENTSO-G | ENTSO-G | European gas flows — requires Transparency Platform registration |
| JODI | JODI | Joint Organisations Data Initiative — oil/gas statistics |

### Setup

```bash
# Create secrets (example)
echo -n "your-eia-key" | gcloud secrets create EIA_API_KEY --data-file=-
```

Configure Cloud Run `etigram-api` to access these secrets via environment or Secret Manager integration.

## Firestore Collections

- `criticalchain_nodes` — materials and facilities
- `criticalchain_edges` — processing steps and routes
- `criticalchain_conflict_zones` — risk zones

## Pipeline Workers

| Worker | Adapters | Purpose |
|--------|----------|---------|
| 1 | USGS MRDS | Minerals & facilities |
| 2 | EIA, ENTSO-G, JODI | Oil & gas |
| 3 | UN Comtrade+ | Trade flows |
| 4 | State Dept, FCDO, DFAT | Country risk |
| 5 | OFAC SDN, EU Consolidated | Sanctions |
| 6 | GDELT, NGA, FAA NOTAM, aisstream | Events & signals |
| 7 | OSM Overpass, Natural Earth, GLEIF | Geo enrichment |
| 8 | Vertex AI Gemini | AI risk scoring |

## Deploy

```bash
# Full deploy (includes CriticalChain)
scripts/deploy

# CriticalChain infra only (Pub/Sub topic, Scheduler job)
scripts/deploy --api-criticalchain
```
