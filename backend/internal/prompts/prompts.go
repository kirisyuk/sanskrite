package prompts

import "strings"

// SourceProfilingPrompt defines a STRICT JSON contract aligned with the legacy
// SourceProfileDataDto (see temp-repos/etigram-backend/web/src/types/dto.ts lines ~784+).
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

TASK:
- Extract fields precisely as per schema. Use only the provided CONTENT.
- Language must be ISO 639-1 (2 letters, lowercase).
- Sentiment must be in [-1.0, 1.0].
- Categories must have labels of 1-2 words only, with confidence in [0,1].
- Document type must be one of: article, post, report, announcement, interview, transcript, other.
- Dates must be ISO (YYYY-MM-DD) or RFC3339.
- If unsure, prefer conservative outputs; omit or use nulls instead of inventing facts.

REQUIRED JSON SCHEMA:
{
  "language": "ISO 639-1 code (lowercase)",
  "sentiment": -1.0,
  "categories": [{"label": "1-2 words", "confidence": 0.0}],
  "topics": [{"name": "string", "share": 0.0}],
  "entities": [{"type": "Person|Organization|Location|Product|Document|Facility|Other", "value": "string", "confidence": 0.0}],
  "keyPoints": ["string"],
  "documentType": "article|post|report|announcement|interview|transcript|other",
  "outlinks": [{"url": "string", "kind": "reference|citation|internal|external"}],
  "events": [{"title": "string", "dateISO": "YYYY-MM-DD or RFC3339", "context": "string?"}],
  "claims": [{"text": "string", "summary": "string?", "confidence": 0.0, "evidenceUrls": ["string"]}]
}

STRICT RULES:
- Return valid JSON matching the schema keys and types.
- Do NOT wrap in code fences; return raw JSON only.`
