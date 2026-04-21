package prompts

import "strings"

// SourceProfilingPrompt defines a STRICT JSON contract aligned with the legacy
// SourceProfileDataDto (see the AgentHorde submodule web DTO types for historical alignment).
// The model MUST return exactly one JSON object that conforms to this schema.
const SourceProfilingPrompt = `You are an expert media analyst. Analyze this source content and produce a STRICT JSON object following the schema below. Do NOT include any explanations or markdown. Output ONE JSON object only.

SOURCE: {sourceId}
LANGUAGE: {language}

CONTENT:
{extractedText}

TASK:
- Build a structured source profile for Discovery. Use ONLY the provided CONTENT (no external knowledge or links fetching).
- If CONTENT is in a different language, still produce textual fields in {language} while preserving meaning.
- When evidence is insufficient, set fields to null or omit them; never invent facts.
- All ratio-like numeric values MUST be normalized to [0,1]. Topic/method shares across arrays SHOULD sum to ≤ 1.0.
- Keep numbers with 2 decimal digits where applicable.
- For targetAudienceCountries, output ISO 3166-1 alpha-2 country codes in UPPERCASE (e.g., RU, US, DE). Do not use full country names.

REQUIRED JSON SCHEMA (keys and types MUST match exactly; omit unknown fields or use nulls where appropriate):
{
  "editorialStance": {
    "political": "string",
    "confidence": 0.0,
    "evidence": ["string"]
  },
  "agenda": {
    "primaryGoal": "string",
    "description": "string",
    "promotedThemes": ["string"],
    "avoidedThemes": ["string"],
    "targetAudience": "string"
  },
  "motivation": "string",
  "trustScore": 0.0,
  "trustRationale": "string",
  "behaviorPatterns": {
    "publishingCadence": "string",
    "eventResponsiveness": 0.0,
    "contentConsistency": 0.0,
    "originalityScore": 0.0
  },
  "contentPatterns": {
    "dominantFramings": ["string"],
    "emotionalToneDistribution": {
      "neutral": 0.0,
      "outrage": 0.0,
      "fear": 0.0,
      "hope": 0.0
    },
    "focusEntityNames": [
      { "entityName": "string", "mentionCount": 0, "averageSentiment": 0.0 }
    ]
  },
  "topics": [ { "name": "string", "share": 0.0, "trend": "up|down|flat" } ],
  "editorialMethods": [ { "method": "string", "share": 0.0 } ],
  "goals": [ { "goal": "string", "confidence": 0.0 } ],
  "goalAlignmentScore": 0.0,
  "metrics": {
    "claimDensity": 0.0,
    "externalRefShare": 0.0,
    "responseLatencyAvg": 0.0,
    "polarization": 0.0,
    "ctaShare": 0.0,
    "promoLinkShare": 0.0,
    "evidenceQuality": 0.0
  },
  "targetAudienceCountries": ["ISO_3166-1_alpha-2"],
  "redFlags": [ { "code": "string", "score": 0.0, "rationale": "string" } ],
  "coordinatedSources": [ { "sourceId": "string", "coordinationScore": 0.0 } ],
  "flags": ["string"]
}

TOPIC EXTRACTION GUIDELINES:
- Return 3-8 topics that reflect distinct, recurring themes observed in CONTENT (not generic labels).
- Topic names must be concise noun phrases (≤ 4 words), human-readable, and specific; avoid generic terms like "news", "politics", "economy".
- Merge synonyms/variants into a single canonical topic (e.g., "AI" and "artificial intelligence" → "Artificial Intelligence").
- For each topic, set share ∈ [0,1] proportional to its relative prominence; the sum of all topic shares MUST be ≤ 1.0.
- Drop very minor topics (< 0.02 share) before normalization; deduplicate strictly (no duplicate names).
- Set trend to one of {up,down,flat} based on relative prominence changes implied by the text; if unclear, use "flat".

STRICT RULES:
- Output MUST be valid JSON matching the schema (no extra top-level keys).
- Numeric values MUST be within [0..1] when applicable.
- Use null or omit fields if data cannot be inferred.
- Do NOT wrap in code fences; return raw JSON only.
`

// AnswerSynthesisPrompt instructs the model to generate a concise, well-grounded
// textual answer that directly addresses the user's query based on multiple
// previously extracted source profiles.
const AnswerSynthesisPrompt = `You are an expert analyst synthesizing information from multiple sources.

USER QUERY: {queryText}
LANGUAGE: {language}
SOURCES ANALYZED: {sourcesCount}

SOURCE PROFILES (JSON array):
{profilesJson}

TASK:
- Produce a user-facing answer that directly addresses the query using ONLY the provided source profiles.
- Do NOT introduce external facts. If evidence is insufficient or conflicting, explicitly note limitations.
- Maintain neutral tone; avoid speculation.

Generate a comprehensive, clear answer that:
1) Directly addresses the user's query in {language} language
2) Summarizes key findings from analyzed sources
3) Highlights patterns and insights discovered across sources
4) Notes source reliability if relevant concerns exist
5) Provides necessary context for understanding the information

ANSWER REQUIREMENTS:
- Write in {language}
- Be concise but informative (3-5 paragraphs)
- Use clear, accessible language
- If sources conflict, briefly note discrepancies

CRITICAL:
- Return plain text answer only (no JSON, no markdown formatting).
`

// Format replaces placeholders in a template with provided values.
// Placeholders are in the form {key}. Missing keys will remain unchanged.
func Format(template string, params map[string]string) string {
	result := template
	for k, v := range params {
		result = strings.ReplaceAll(result, "{"+k+"}", v)
	}
	return result
}

const DocumentExtractionPrompt = `You are an expert content analyst. Extract a STRICT JSON object exactly following the schema below. Do NOT include explanations or markdown. Output ONE JSON object only.

TEXT LANGUAGE: {language}

CONTENT:
{text}

TAXONOMY RULES (REQUIRED - extract only what fits):
{taxonomyRules}

TASK:
- Extract fields precisely as per schema. Use only the provided CONTENT.
- Language must be ISO 639-1 (2 letters, lowercase).
- Sentiment must be in [-1.0, 1.0].
- Categories must have labels of 1-2 words only, with confidence in [0,1].
- Document type must be one of: article, post, report, announcement, interview, transcript, other.
- Dates must be ISO (YYYY-MM-DD) or RFC3339.
- If you cannot map confidently, use "other".
- If unsure, prefer conservative outputs; omit or use nulls instead of inventing facts.
- Include intangible events when they are clearly stated (partnerships, alliances/coalitions, union actions, new legal entities/incorporations, factory openings/closures, product/model releases & deprecations, pricing/availability changes, policy/regulatory changes, sanctions/licensing, investigations, dissolutions).
- For event locations:
  - address: ONLY real physical addresses (e.g. "123 Main St, Houston, TX, USA"). Never use event titles, headlines, metrics, or non-address text.
  - name: Place names for geocoding when address is unknown (e.g. "Dubai airport", "Port of Houston"). Short, location-like only.
  - Extract countryAlpha2/admin1/city for structured geo. Place and address are used for geocoding.
-
- METRIC UPDATES:
  You may extract structured numeric metric updates when the content provides clear values.
  Use the preferred metric vocabulary below when possible, BUT you are not limited to it.
  If not mappable, create a new metricName using snake_case and a prefix: org_, fac_, product_, program_, market_, logistics_, risk_, incident_.
  Use units from this allowed set only: usd, eur, rub, units, units_per_day, days, hours, ratio, index_point, code.
  IMPORTANT: use ratio (0..1) for shares/rates; do NOT output percent.

PREFERRED METRIC VOCABULARY (non-exhaustive):
- org_headcount (units)
- org_revenue (usd)
- org_operating_margin (ratio)
- org_net_margin (ratio)
- org_cash (usd)
- org_debt (usd)
- org_market_cap (usd)
- org_esg_risk_score (ratio)
- capacity_units_per_day (units_per_day)
- capacity_utilization (ratio)
- downtime_hours (hours)
- output_volume (units)
- lead_time_days (days)
- inventory_days (days)
- price (usd)
- market_share (ratio)
- demand_index (index_point)
- defect_rate (ratio)
- on_time_delivery_rate (ratio)
- shipment_delay_days (days)
- freight_cost (usd)
- port_congestion_index (index_point)
- incident_severity (ratio)
- incident_duration_hours (hours)
- impact_confidence (ratio)
- status_code (code)
- sanctions_risk (ratio)
- supply_risk (ratio)

REQUIRED JSON SCHEMA:
{
  "language": "ISO 639-1 code (lowercase)",
  "sentiment": -1.0,
  "categories": [{"label": "1-2 words", "confidence": 0.0}],
  "topics": [{"name": "string", "share": 0.0}],
  "entities": [{"type": "Organization|Person|Product|Facility|Program|Market|GovernmentProject|SupplyChainNode", "value": "string", "confidence": 0.0}],
  "keyPoints": ["string"],
  "documentType": "article|post|report|announcement|interview|transcript|other",
  "outlinks": [{"url": "string", "kind": "reference|citation|internal|external"}],
  "events": [{
    "title": "string",
    "eventType": "string (optional; must be from TAXONOMY RULES event types)",
    "dateISO": "YYYY-MM-DD or RFC3339",
    "context": "string?",
    "summary": "string?",
    "location": {
      "name": "place name for geocoding when address unknown (e.g. Dubai airport, Port of Houston)?",
      "address": "ONLY real physical address (street, city, country). Never event title, headline, or metric. Example: 123 Main St, Houston, TX, USA?",
      "iso6709": "coordinates in ISO6709 format (e.g. +25.27+55.30/)?",
      "countryAlpha2": "ISO3166-1 alpha2 lowercase (e.g., us, cn, de)?",
      "admin1": "state/province code or slug lowercase; use 'na' if unknown/not applicable?",
      "city": "city slug lowercase; use 'na' if unknown/not applicable?"
    },
    "spherePaths": ["string (optional, canonical /sphere/... taxonomy paths)"],
    "participants": [{"entityName": "string", "role": "agent|target|mediator|mentioned|beneficiary|affected|other"}]
  }],
  "claims": [{
    "text": "string",
    "summary": "string?",
    "confidence": 0.0,
    "evidenceUrls": ["string"],
    "subjectEntityName": "string?",
    "spherePaths": ["string (optional, canonical /sphere/... taxonomy paths)"]
  }],
  "metricUpdates": [{
    "metricName": "snake_case (prefer vocabulary)",
    "value": 0.0,
    "unit": "usd|eur|rub|units|units_per_day|days|hours|ratio|index_point|code",
    "confidence": 0.0,
    "observedAt": "YYYY-MM-DD or RFC3339?",
    "subjectEntityName": "string?"
  }]
}

STRICT RULES:
- Return valid JSON matching the schema keys and types.
- Do NOT wrap in code fences; return raw JSON only.`
