package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type batchInput struct {
	Inputs      []ItemInput
	ByID        map[string]*models.HorizonFragment
	MissingByID map[string]*missingInfo
}

type ItemInput struct {
	Fragment FragmentLite `json:"fragment"`
	Missing  []string     `json:"missing"`
}

type FragmentLite struct {
	ID            string              `json:"id"`
	CanonicalPath string              `json:"canonicalPath,omitempty"`
	Type          models.FragmentType `json:"type,omitempty"`
	LabelEN       string              `json:"labelEn,omitempty"`
	LegalName     string              `json:"legalName,omitempty"`
	ShortName     string              `json:"shortName,omitempty"`
	Description   string              `json:"description,omitempty"`
	EntityPath    string              `json:"entityPath,omitempty"`
	SectorPath    string              `json:"sectorPath,omitempty"`

	OrgSize        string   `json:"orgSize,omitempty"`
	MarketSegments []string `json:"marketSegments,omitempty"`

	MarketDomain   string   `json:"marketDomain,omitempty"`
	MarketVertical string   `json:"marketVertical,omitempty"`
	MarketChannels []string `json:"marketChannels,omitempty"`

	ExistingStateKeys []string `json:"existingStateKeys,omitempty"`
}

type LLMResponse struct {
	SchemaVersion string            `json:"schemaVersion"`
	Items         []LLMResponseItem `json:"items"`
}

type LLMResponseItem struct {
	FragmentID string `json:"fragmentId"`

	OrgSize        string   `json:"orgSize"`
	MarketSegments []string `json:"marketSegments"`

	MarketDomain   string   `json:"marketDomain"`
	MarketVertical string   `json:"marketVertical"`
	MarketChannels []string `json:"marketChannels"`

	State map[string]models.MetricValue `json:"state"`

	Confidence float64 `json:"confidence"`
	Rationale  string  `json:"rationale"`
}

func backfillBatch(
	ctx context.Context,
	db *firestore.Client,
	client *gen_combined.Client,
	exec *ai.AITaskExecutor,
	mm *ai.AIModelManager,
	featSet map[string]struct{},
	batch []*models.HorizonFragment,
	writeRow func(*Row),
) error {
	if len(batch) == 0 {
		return nil
	}
	inp := buildBatchInput(batch, featSet)
	prompt := buildPrompt(inp.Inputs)
	schema := llmSchema()

	// IMPORTANT: fail-soft verification.
	// We only require that JSON parses into the LLMResponse struct.
	// Missing fields/metrics MUST NOT fail the whole batch; we handle them per-item and log in JSONL.
	verify := func(out LLMResponse) error { _ = out; return nil }

	parsed, raw, pt, ot, tt, durMs, err := callLLMUnified(
		ctx,
		client,
		exec,
		mm,
		strings.TrimSpace(*model),
		strings.TrimSpace(*engine),
		*grounding,
		*maxOutputTokens,
		prompt,
		schema,
		verify,
	)
	if err != nil {
		return err
	}

	repo := hrepo.NewHorizonFragmentsRepository(db)
	now := time.Now().UTC()

	// Index items by fragmentId (best-effort).
	itemsByID := map[string]LLMResponseItem{}
	for _, it := range parsed.Items {
		id := strings.TrimSpace(it.FragmentID)
		if id == "" {
			continue
		}
		if _, ok := itemsByID[id]; !ok {
			itemsByID[id] = it
		}
	}

	// Emit one row per requested fragment, even if LLM output is missing/partial.
	for _, f := range batch {
		if f == nil {
			continue
		}
		id := strings.TrimSpace(f.ID)
		if id == "" {
			continue
		}
		it, ok := itemsByID[id]
		row := &Row{
			FragmentID:      id,
			CanonicalPath:   strings.TrimSpace(f.CanonicalPath),
			Type:            f.Type,
			Features:        sortedKeys(featSet),
			SelectedBecause: inp.InputsByIDReason(id),
			Applied:         false,
			Reason:          "",
			ScannedAt:       now,
		}
		row.LLM.Model = strings.TrimSpace(*model)
		row.LLM.PromptTokens = pt
		row.LLM.OutputTokens = ot
		row.LLM.TotalTokens = tt
		row.LLM.DurationMs = durMs
		row.LLM.Raw = safePreview(raw, 2000)
		row.LLM.SchemaVersion = parsed.SchemaVersion
		if ok {
			row.LLM.Confidence = it.Confidence
			row.LLM.Rationale = strings.TrimSpace(it.Rationale)
		}

		// Apply on a fresh fragment to avoid clobbering concurrent updates.
		cur := f
		if *apply {
			loaded, err := repo.Get(ctx, id)
			if err != nil || loaded == nil {
				row.Reason = "apply failed: cannot load fragment"
				writeRow(row)
				continue
			}
			cur = loaded
		}

		if !ok {
			row.Reason = "partial_llm_missing_item"
			writeRow(row)
			continue
		}

		changed := false
		if _, ok := featSet[string(featureOrgClassifiers)]; ok && cur.Type == models.FragmentTypeOrganization {
			if applyOrgClassifiers(cur, it, row) {
				changed = true
			}
		}
		if cur.Type == models.FragmentTypeMarket {
			if _, ok := featSet[string(featureMarketProfile)]; ok {
				if applyMarketProfile(cur, it, row) {
					changed = true
				}
			}
			if _, ok := featSet[string(featureMarketMetrics)]; ok {
				if applyMarketMetrics(cur, it, row) {
					changed = true
				}
			}
		}

		if !changed {
			if strings.TrimSpace(row.LLM.Rationale) == "" {
				row.Reason = "partial_empty_item"
			} else {
				row.Reason = "noop"
			}
			writeRow(row)
			continue
		}

		if *apply {
			if err := repo.Update(ctx, cur); err != nil {
				row.Reason = "apply failed: update error: " + err.Error()
				writeRow(row)
				continue
			}
			row.Applied = true
		}
		row.Reason = "ok"
		writeRow(row)
	}

	return nil
}

func buildBatchInput(batch []*models.HorizonFragment, featSet map[string]struct{}) *batchInput {
	out := &batchInput{
		Inputs:      make([]ItemInput, 0, len(batch)),
		ByID:        map[string]*models.HorizonFragment{},
		MissingByID: map[string]*missingInfo{},
	}
	for _, f := range batch {
		if f == nil {
			continue
		}
		id := strings.TrimSpace(f.ID)
		out.ByID[id] = f
		_, mi := isCandidate(f, featSet)
		out.MissingByID[id] = mi

		in := ItemInput{
			Fragment: makeLite(f),
			Missing:  missingStrings(f, mi, featSet),
		}
		out.Inputs = append(out.Inputs, in)
	}
	return out
}

func (b *batchInput) InputsByIDReason(id string) []string {
	mi := b.MissingByID[strings.TrimSpace(id)]
	if mi == nil {
		return nil
	}
	out := []string{}
	if mi.MissingOrgSize {
		out = append(out, "orgSize")
	}
	if mi.MissingSegments {
		out = append(out, "marketSegments")
	}
	if mi.MissingMarketDomain {
		out = append(out, "marketDomain")
	}
	if mi.MissingMarketVertical {
		out = append(out, "marketVertical")
	}
	if mi.MissingMarketChannels {
		out = append(out, "marketChannels")
	}
	for k := range mi.MissingMarketMetrics {
		out = append(out, "state:"+k)
	}
	sort.Strings(out)
	return out
}

func makeLite(f *models.HorizonFragment) FragmentLite {
	if f == nil {
		return FragmentLite{}
	}
	keys := []string{}
	for k := range f.State {
		k2 := strings.TrimSpace(k)
		if k2 != "" {
			keys = append(keys, k2)
		}
	}
	sort.Strings(keys)
	legal := strings.TrimSpace(f.Identifiers["legalName"])
	short := strings.TrimSpace(f.Identifiers["shortName"])
	return FragmentLite{
		ID:                strings.TrimSpace(f.ID),
		CanonicalPath:     strings.TrimSpace(f.CanonicalPath),
		Type:              f.Type,
		LabelEN:           bestLabel(f),
		LegalName:         legal,
		ShortName:         short,
		Description:       strings.TrimSpace(f.Description),
		EntityPath:        strings.TrimSpace(f.EntityPath),
		SectorPath:        strings.TrimSpace(f.SectorPath),
		OrgSize:           strings.TrimSpace(f.OrgSize),
		MarketSegments:    append([]string{}, f.MarketSegments...),
		MarketDomain:      strings.TrimSpace(f.MarketDomain),
		MarketVertical:    strings.TrimSpace(f.MarketVertical),
		MarketChannels:    append([]string{}, f.MarketChannels...),
		ExistingStateKeys: keys,
	}
}

func missingStrings(f *models.HorizonFragment, mi *missingInfo, featSet map[string]struct{}) []string {
	if f == nil || mi == nil {
		return nil
	}
	out := []string{}
	if _, ok := featSet[string(featureOrgClassifiers)]; ok && f.Type == models.FragmentTypeOrganization {
		if mi.MissingOrgSize {
			out = append(out, "orgSize")
		}
		if mi.MissingSegments {
			out = append(out, "marketSegments")
		}
	}
	if f.Type == models.FragmentTypeMarket {
		if _, ok := featSet[string(featureMarketProfile)]; ok {
			if mi.MissingMarketDomain {
				out = append(out, "marketDomain")
			}
			if mi.MissingMarketVertical {
				out = append(out, "marketVertical")
			}
			if mi.MissingMarketChannels {
				out = append(out, "marketChannels")
			}
		}
		if _, ok := featSet[string(featureMarketMetrics)]; ok {
			keys := []string{}
			for k := range mi.MissingMarketMetrics {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				out = append(out, "state:"+k)
			}
		}
	}
	return out
}

func buildPrompt(items []ItemInput) string {
	b, _ := json.MarshalIndent(items, "", "  ")

	marketKeys := []string{
		MetricMarketTAMUSD,
		MetricMarketCAGRPercent,
		MetricMarketHHI,
		MetricMarketCR4Percent,
		MetricMarketSupplyChainRisk, // optional
	}

	return fmt.Sprintf(`Return ONLY valid JSON (no markdown, no code fences) matching schema:
{
  "schemaVersion": "v1",
  "items": [
    {
      "fragmentId": "string",
      "orgSize": "sme|le|enterprise|mega|na",                   // ONLY for org_classifiers
      "marketSegments": ["/segment/{domain}/{vertical}/{geo}"], // ONLY for org_classifiers
      "marketDomain": "finance|technology|industrial|consumer|healthcare|energy|materials|telecom|real-estate|unknown",
      "marketVertical": "string|unknown",
      "marketChannels": ["b2b","b2c","public-sector",...],
      "state": {
        "%s": {
          "value": 0.0,                 // number (no commas)
          "unit": "usd|percent|index",  // required for tam_usd/cagr_percent/cr4_percent/hhi
          "confidence": 0.0,            // 0..1
          "observedAt": "YYYY-MM-DD",   // ISO8601 date or RFC3339 (see observedAt rules below)
          "provenance": { "sourceIds": ["https://..."], "method": "grounded_market_backfill" }
        }
      },
      "confidence": 0.0,               // overall item confidence 0..1
      "rationale": "string"            // short explanation, cite sources used
    }
  ]
}

Rules:
- items MUST have exactly the same length as input, one item per input element.
- fragmentId MUST match input fragment.id.
- IMPORTANT: input.missing is authoritative. For EACH item you MUST:
  - If "orgSize" is NOT listed in missing: orgSize MUST be "na"
  - If "marketSegments" is NOT listed in missing: marketSegments MUST be []
  - If "marketDomain" is NOT listed in missing: marketDomain MUST be ""
  - If "marketVertical" is NOT listed in missing: marketVertical MUST be ""
  - If "marketChannels" is NOT listed in missing: marketChannels MUST be []
  - For state metrics: if "state:/metric/market/..." is NOT listed in missing: state MUST NOT include that key.
- When marketDomain/marketVertical ARE requested but truly unknown:
  - marketDomain MUST be "unknown" (not empty)
  - marketVertical MUST be "unknown" (not empty)
- Do NOT invent numbers. For market metrics, only output values with sources in provenance.sourceIds.
- market metric keys (when requested) MUST use these canonical keys only: %s
  - supply_chain_risk is OPTIONAL; include it only if you can source it reliably.

observedAt rules (IMPORTANT):
- observedAt MUST reflect the time the metric value is true "as of" in the SOURCE.
- Prefer, in order:
  1) The explicit "as of" date in the source (e.g., "as of 2024-09-30") → use that date.
  2) The end date of a stated period (e.g., "FY2024", "2024 revenue") → use the period end date if explicitly stated in the source.
  3) If only a YEAR is known (e.g., "2024 market size") and no day/month is stated:
     - Use "YYYY-01-01T00:00:00Z" (start-of-year) to indicate year-only precision.
     - DO NOT default to "YYYY-12-31" unless the source explicitly says end-of-year / year-end.
- If you cannot infer at least a year from the source, OMIT the metric (do not guess a date).

INPUT (JSON array):
%s`, MetricMarketTAMUSD, strings.Join(marketKeys, ", "), string(b))
}

func llmSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"schemaVersion": map[string]any{"type": "string"},
			"items": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"fragmentId": map[string]any{"type": "string"},
						"orgSize": map[string]any{
							"type": "string",
							"enum": []string{"sme", "le", "enterprise", "mega", "na"},
						},
						"marketSegments": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"marketDomain":   map[string]any{"type": "string"},
						"marketVertical": map[string]any{"type": "string"},
						"marketChannels": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"state": map[string]any{
							"type":                 "object",
							"additionalProperties": map[string]any{"type": "object"},
						},
						"confidence": map[string]any{"type": "number"},
						"rationale":  map[string]any{"type": "string"},
					},
					"required": []string{"fragmentId", "orgSize", "marketSegments", "marketDomain", "marketVertical", "marketChannels", "state", "confidence", "rationale"},
				},
			},
		},
		"required": []string{"schemaVersion", "items"},
	}
}

// verifyLLMResponse is a strict verifier retained for optional strict-mode.
// Default behavior is fail-soft and does NOT use this verifier (see backfillBatch()).
func verifyLLMResponse(out LLMResponse, inp *batchInput, featSet map[string]struct{}) error {
	if strings.TrimSpace(out.SchemaVersion) == "" {
		return fmt.Errorf("schemaVersion is required")
	}
	if inp == nil {
		return fmt.Errorf("internal: nil batch input")
	}
	if len(out.Items) != len(inp.Inputs) {
		return fmt.Errorf("items length mismatch: got %d want %d", len(out.Items), len(inp.Inputs))
	}
	seen := map[string]struct{}{}
	for i, it := range out.Items {
		id := strings.TrimSpace(it.FragmentID)
		if id == "" {
			return fmt.Errorf("items[%d].fragmentId is required", i)
		}
		if _, ok := inp.ByID[id]; !ok {
			return fmt.Errorf("items[%d].fragmentId is unknown: %s", i, id)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate fragmentId: %s", id)
		}
		seen[id] = struct{}{}

		if it.Confidence < 0 || it.Confidence > 1 || math.IsNaN(it.Confidence) || math.IsInf(it.Confidence, 0) {
			return fmt.Errorf("items[%d].confidence out of range", i)
		}
		if strings.TrimSpace(it.Rationale) == "" {
			return fmt.Errorf("items[%d].rationale is required", i)
		}

		f := inp.ByID[id]
		mi := inp.MissingByID[id]
		if f == nil || mi == nil {
			continue
		}

		// ORG classifiers validation
		if _, ok := featSet[string(featureOrgClassifiers)]; ok && f.Type == models.FragmentTypeOrganization {
			// orgSize must be present and valid
			osz := strings.TrimSpace(it.OrgSize)
			if osz == "" {
				return fmt.Errorf("items[%d].orgSize is required for ORGANIZATION", i)
			}
			if taxonomy.NormalizeOrgSize(osz) == "" {
				return fmt.Errorf("items[%d].orgSize invalid: %q", i, osz)
			}
			if it.MarketSegments == nil {
				return fmt.Errorf("items[%d].marketSegments must be present (can be empty)", i)
			}
			if *strictSegments {
				for j, s := range it.MarketSegments {
					s2 := strings.TrimSpace(s)
					if s2 == "" {
						return fmt.Errorf("items[%d].marketSegments[%d] empty", i, j)
					}
					if !strings.HasPrefix(s2, "/segment/") {
						return fmt.Errorf("items[%d].marketSegments[%d] must start with /segment/: %q", i, j, s2)
					}
					n, ok := taxonomy.NormalizeSegmentPath(s2)
					if !ok || n == "" {
						return fmt.Errorf("items[%d].marketSegments[%d] invalid segment path: %q", i, j, s2)
					}
					if n != s2 {
						return fmt.Errorf("items[%d].marketSegments[%d] not canonical: got %q normalized %q", i, j, s2, n)
					}
				}
			}
		}
		// If org_classifiers is NOT enabled, orgSize/marketSegments must stay empty defaults for all items.
		if _, ok := featSet[string(featureOrgClassifiers)]; !ok {
			if strings.TrimSpace(it.OrgSize) != "na" {
				return fmt.Errorf("items[%d].orgSize must be 'na' when org_classifiers is disabled", i)
			}
			if it.MarketSegments == nil {
				return fmt.Errorf("items[%d].marketSegments must be present (can be empty)", i)
			}
			if len(it.MarketSegments) != 0 {
				return fmt.Errorf("items[%d].marketSegments must be empty when org_classifiers is disabled", i)
			}
		}

		// MARKET profile validation (only enforce if missing)
		if _, ok := featSet[string(featureMarketProfile)]; ok && f.Type == models.FragmentTypeMarket {
			if mi.MissingMarketDomain && strings.TrimSpace(it.MarketDomain) == "" {
				return fmt.Errorf("items[%d].marketDomain required (missing in fragment)", i)
			}
			if mi.MissingMarketVertical && strings.TrimSpace(it.MarketVertical) == "" {
				return fmt.Errorf("items[%d].marketVertical required (missing in fragment)", i)
			}
			// Encourage explicit unknowns to avoid verifier failure due to empty strings.
			if mi.MissingMarketDomain && strings.TrimSpace(it.MarketDomain) == "" {
				return fmt.Errorf("items[%d].marketDomain must be non-empty (use 'unknown' if needed)", i)
			}
			if mi.MissingMarketVertical && strings.TrimSpace(it.MarketVertical) == "" {
				return fmt.Errorf("items[%d].marketVertical must be non-empty (use 'unknown' if needed)", i)
			}
			// channels may remain empty
			if it.MarketChannels == nil {
				return fmt.Errorf("items[%d].marketChannels must be present (can be empty)", i)
			}
		}

		// MARKET metrics validation (enforce missing keys)
		if _, ok := featSet[string(featureMarketMetrics)]; ok && f.Type == models.FragmentTypeMarket {
			if it.State == nil {
				return fmt.Errorf("items[%d].state must be present (can be empty object)", i)
			}
			for k := range mi.MissingMarketMetrics {
				mv, ok := it.State[k]
				if !ok {
					return fmt.Errorf("items[%d].state missing required key %s", i, k)
				}
				if !isFinite(mv.Value) {
					return fmt.Errorf("items[%d].state[%s].value invalid", i, k)
				}
				if mv.Confidence < 0 || mv.Confidence > 1 || math.IsNaN(mv.Confidence) || math.IsInf(mv.Confidence, 0) {
					return fmt.Errorf("items[%d].state[%s].confidence out of range", i, k)
				}
				if mv.ObservedAt.IsZero() {
					return fmt.Errorf("items[%d].state[%s].observedAt is required", i, k)
				}
				if strings.TrimSpace(mv.Provenance.Method) == "" {
					return fmt.Errorf("items[%d].state[%s].provenance.method is required", i, k)
				}
				if len(mv.Provenance.SourceIDs) == 0 {
					return fmt.Errorf("items[%d].state[%s].provenance.sourceIds is required", i, k)
				}
				if !validateMarketMetricRange(k, mv.Value) {
					return fmt.Errorf("items[%d].state[%s].value out of expected range: %v", i, k, mv.Value)
				}
			}
		}
	}
	return nil
}

func isFinite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func validateMarketMetricRange(key string, v float64) bool {
	switch key {
	case MetricMarketSupplyChainRisk:
		return v >= 0 && v <= 1
	case MetricMarketHHI:
		return v >= 0 && v <= 10000
	case MetricMarketCR4Percent:
		return v >= 0 && v <= 100
	case MetricMarketCAGRPercent:
		// guardrail; real-world CAGR above 200% is rare. Keep wide but bounded.
		return v >= -100 && v <= 300
	case MetricMarketTAMUSD:
		return v >= 0
	default:
		return true
	}
}

func applyOrgClassifiers(cur *models.HorizonFragment, it LLMResponseItem, row *Row) bool {
	if cur == nil {
		return false
	}
	oldOrgSize := strings.TrimSpace(cur.OrgSize)
	oldSegments := append([]string{}, cur.MarketSegments...)

	newOrgSize := taxonomy.NormalizeOrgSize(strings.TrimSpace(it.OrgSize))
	if newOrgSize == "" {
		newOrgSize = "na"
	}
	// Normalize segments (if strict, verifier already enforced canonical).
	newSegments := it.MarketSegments
	newSegmentsNorm := taxonomy.NormalizeSegmentPaths(newSegments)

	row.OldOrgSize = oldOrgSize
	row.OldSegments = oldSegments
	row.NewOrgSize = newOrgSize
	row.NewSegments = newSegmentsNorm
	row.Validation.SegmentsNormalized = newSegmentsNorm
	if len(newSegments) > 0 && len(newSegmentsNorm) == 0 {
		row.Validation.SegmentsDropped = len(newSegments)
	}

	changed := false
	curOld := strings.ToLower(strings.TrimSpace(cur.OrgSize))
	if *overwriteOrgSize || curOld == "" || (*includeNA && curOld == "na") {
		if strings.TrimSpace(cur.OrgSize) != newOrgSize {
			cur.OrgSize = newOrgSize
			changed = true
		}
	}

	if *overwriteSegments {
		if !equalStringSlices(cur.MarketSegments, newSegmentsNorm) {
			cur.MarketSegments = newSegmentsNorm
			changed = true
		}
	} else {
		union := append([]string{}, cur.MarketSegments...)
		seen := map[string]bool{}
		for _, s := range union {
			s = strings.TrimSpace(s)
			if s != "" {
				seen[s] = true
			}
		}
		for _, s := range newSegmentsNorm {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			union = append(union, s)
		}
		union = taxonomy.NormalizeSegmentPaths(union)
		if !equalStringSlices(cur.MarketSegments, union) {
			cur.MarketSegments = union
			changed = true
		}
	}
	return changed
}

func applyMarketProfile(cur *models.HorizonFragment, it LLMResponseItem, row *Row) bool {
	if cur == nil {
		return false
	}
	oldDom := strings.TrimSpace(cur.MarketDomain)
	oldVert := strings.TrimSpace(cur.MarketVertical)
	oldCh := append([]string{}, cur.MarketChannels...)

	inDom := strings.TrimSpace(it.MarketDomain)
	inVert := strings.TrimSpace(it.MarketVertical)
	inCh := dedupNonEmptyStrings(it.MarketChannels)

	row.OldMarketDomain, row.OldMarketVertical, row.OldMarketChannels = oldDom, oldVert, oldCh
	row.NewMarketDomain, row.NewMarketVertical, row.NewMarketChannels = inDom, inVert, inCh

	changed := false
	if *overwriteMarket || strings.TrimSpace(cur.MarketDomain) == "" {
		if inDom != "" && inDom != cur.MarketDomain {
			cur.MarketDomain = inDom
			changed = true
		}
	}
	if *overwriteMarket || strings.TrimSpace(cur.MarketVertical) == "" {
		if inVert != "" && inVert != cur.MarketVertical {
			cur.MarketVertical = inVert
			changed = true
		}
	}
	if *overwriteMarket || len(cur.MarketChannels) == 0 {
		if len(inCh) > 0 && !equalStringSlices(cur.MarketChannels, inCh) {
			cur.MarketChannels = inCh
			changed = true
		}
	}
	return changed
}

func applyMarketMetrics(cur *models.HorizonFragment, it LLMResponseItem, row *Row) bool {
	if cur == nil {
		return false
	}
	if cur.State == nil {
		cur.State = map[string]models.MetricValue{}
	}
	allowed := []string{
		MetricMarketTAMUSD,
		MetricMarketCAGRPercent,
		MetricMarketHHI,
		MetricMarketCR4Percent,
		MetricMarketSupplyChainRisk,
	}
	old := map[string]models.MetricValue{}
	for _, k := range allowed {
		if mv, ok := cur.State[k]; ok {
			old[k] = mv
		}
	}
	row.OldMarketState = old

	changed := false
	setN := 0
	keptN := 0
	for _, k := range allowed {
		mv, ok := it.State[k]
		if !ok {
			continue
		}
		// default units (best-effort)
		switch k {
		case MetricMarketTAMUSD:
			if mv.Unit == nil {
				u := "usd"
				mv.Unit = &u
			}
		case MetricMarketCAGRPercent, MetricMarketCR4Percent:
			if mv.Unit == nil {
				u := "percent"
				mv.Unit = &u
			}
		case MetricMarketHHI:
			if mv.Unit == nil {
				u := "index"
				mv.Unit = &u
			}
		}

		if *overwriteMarketMet || !hasAnyStateKey(cur, k) {
			cur.State[k] = mv
			changed = true
			setN++
		} else {
			keptN++
		}
	}
	row.Validation.MarketMetricsKept = keptN
	row.Validation.MarketMetricsSet = setN
	if changed {
		newState := map[string]models.MetricValue{}
		for _, k := range allowed {
			if mv, ok := cur.State[k]; ok {
				newState[k] = mv
			}
		}
		row.NewMarketState = newState
	}
	return changed
}

func safePreview(s string, maxN int) string {
	s = strings.TrimSpace(s)
	if s == "" || maxN <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxN {
		return s
	}
	return string(r[:maxN]) + "...(truncated)"
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}

func dedupNonEmptyStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
