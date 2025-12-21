package scout

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type FactApplier struct {
	vertex *ai.VertexAISearchClient
}

type FactApplyResult struct {
	Applied          bool
	AppliedFacts     int
	AppliedFactIDs   []string
	MaxRelevance     float64
	SeenWatermark    time.Time
	AppliedWatermark time.Time
}

// TryApplyFactsFastPath attempts to apply recent facts to the fragment without LLM.
// It uses Vertex AI Search relevanceScore as a proxy for cosine similarity.
//
// Safety:
// - Only applies when relevanceScore >= relevanceThreshold AND fact.confidence >= factConfidenceThreshold
// - Only performs deterministic updates: provenance merge + optional metric updates
// - Always updates LastFactsSeenAt watermark to avoid rescans
func (a *FactApplier) TryApplyFactsFastPath(
	ctx context.Context,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	fragment *horizonmodels.HorizonFragment,
	facts []*models.OntologyFact,
	relevanceThreshold float64,
	factConfidenceThreshold float64,
) (*FactApplyResult, error) {
	if fragment == nil || fragment.ID == "" {
		return &FactApplyResult{Applied: false}, nil
	}
	if fragmentsRepo == nil {
		return nil, fmt.Errorf("fragments repo not configured")
	}
	if len(facts) == 0 {
		return &FactApplyResult{Applied: false}, nil
	}

	// Track watermark of seen facts
	var maxSeen time.Time
	for _, f := range facts {
		if f == nil {
			continue
		}
		if f.UpdatedAt.After(maxSeen) {
			maxSeen = f.UpdatedAt
		}
	}

	res := &FactApplyResult{
		Applied:        false,
		AppliedFacts:   0,
		AppliedFactIDs: nil,
		MaxRelevance:   0,
		SeenWatermark:  maxSeen,
	}

	now := time.Now().UTC()

	// Apply eligible facts
	for _, f := range facts {
		if f == nil {
			continue
		}
		if f.Confidence < factConfidenceThreshold {
			continue
		}
		rel := a.relevanceToFragment(ctx, fragment.CanonicalPath, f)
		if rel > res.MaxRelevance {
			res.MaxRelevance = rel
		}
		if rel < relevanceThreshold {
			continue
		}

		// Deterministic provenance merge
		mergeStringSet(&fragment.Provenance.SourceIDs, f.SourceIDs)
		mergeHorizonSourceRefs(&fragment.Provenance.SourceRefs, f.SourceRefs)
		// Best-effort: keep observedAt
		if fragment.Provenance.ObservedAt == nil {
			t := now
			fragment.Provenance.ObservedAt = &t
		} else if f.ObservedAt != nil && f.ObservedAt.After(*fragment.Provenance.ObservedAt) {
			t := f.ObservedAt.UTC()
			fragment.Provenance.ObservedAt = &t
		}
		fragment.Provenance.Method = "ontology_fact_fastpath"

		// Deterministic metric updates (optional)
		if len(f.MetricUpdates) > 0 {
			if fragment.State == nil {
				fragment.State = map[string]horizonmodels.MetricValue{}
			}
			for _, mu := range f.MetricUpdates {
				name := strings.TrimSpace(mu.MetricName)
				if name == "" {
					continue
				}
				obs := now
				if mu.ObservedAt != nil {
					obs = mu.ObservedAt.UTC()
				}
				conf := mu.Confidence
				if conf <= 0 {
					conf = f.Confidence
				}
				fragment.State[name] = horizonmodels.MetricValue{
					Value:      mu.Value,
					Unit:       mu.Unit,
					Confidence: conf,
					ObservedAt: obs,
					Provenance: horizonmodels.Provenance{
						SourceIDs:  dedupStrings(f.SourceIDs),
						SourceRefs: dedupHorizonSourceRefs(f.SourceRefs),
						ObservedAt: &obs,
						Method:     "ontology_fact_fastpath",
					},
				}
			}
		}

		res.AppliedFacts++
		if strings.TrimSpace(f.ID) != "" {
			res.AppliedFactIDs = append(res.AppliedFactIDs, strings.TrimSpace(f.ID))
		}
		res.Applied = true
	}

	// Update watermarks
	if !maxSeen.IsZero() {
		fragment.Metadata.LastFactsSeenAt = maxSeen
	}
	if res.Applied {
		res.AppliedWatermark = now
		fragment.Metadata.LastFactsAppliedAt = now
		// Cheap confirmation: do not force VERIFIED, but refresh LastVerifiedAt to mark recency without LLM.
		if fragment.Metadata.LastVerifiedAt.IsZero() || now.After(fragment.Metadata.LastVerifiedAt) {
			fragment.Metadata.LastVerifiedAt = now
		}
		// If we successfully applied facts, epistemic status can be upgraded if it was low-confidence.
		if fragment.Metadata.EpistemicStatus == horizonmodels.EpistemicStatusLowConfidence {
			fragment.Metadata.EpistemicStatus = horizonmodels.EpistemicStatusVerified
		}
	}

	if err := fragmentsRepo.Update(ctx, fragment); err != nil {
		return res, err
	}
	return res, nil
}

func (a *FactApplier) relevanceToFragment(ctx context.Context, fragmentCanonicalPath string, f *models.OntologyFact) float64 {
	if a == nil || a.vertex == nil || !a.vertex.IsConfigured() || f == nil {
		return 0
	}
	q := strings.TrimSpace(f.VectorText)
	if q == "" {
		q = strings.TrimSpace(f.Title + "\n" + f.Summary)
	}
	if q == "" {
		return 0
	}

	filterParts := []string{
		`structData.type: "horizon_fragment"`,
	}
	if strings.TrimSpace(fragmentCanonicalPath) != "" {
		filterParts = append(filterParts, fmt.Sprintf(`structData.canonicalPath: "%s"`, strings.TrimSpace(fragmentCanonicalPath)))
	}

	req := &ai.SearchRequest{
		Query:    q,
		PageSize: 1,
		Filter:   strings.Join(filterParts, " AND "),
	}
	resp, err := a.vertex.Search(ctx, req)
	if err != nil || resp == nil || len(resp.Results) == 0 {
		return 0
	}
	return resp.Results[0].RelevanceScore
}

func mergeStringSet(dst *[]string, src []string) {
	if dst == nil || len(src) == 0 {
		return
	}
	ex := make(map[string]struct{}, len(*dst))
	for _, s := range *dst {
		s = strings.TrimSpace(s)
		if s != "" {
			ex[s] = struct{}{}
		}
	}
	for _, s := range src {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := ex[s]; ok {
			continue
		}
		*dst = append(*dst, s)
		ex[s] = struct{}{}
	}
}

func mergeHorizonSourceRefs(dst *[]horizonmodels.SourceRef, src []models.SourceRef) {
	if dst == nil || len(src) == 0 {
		return
	}
	ex := make(map[string]struct{}, len(*dst))
	for _, r := range *dst {
		key := horizonSourceRefKey(r)
		if key != "" {
			ex[key] = struct{}{}
		}
	}
	for _, r := range src {
		hr := horizonmodels.SourceRef{SourceID: r.SourceID, SourceType: r.SourceType}
		key := horizonSourceRefKey(hr)
		if key == "" {
			continue
		}
		if _, ok := ex[key]; ok {
			continue
		}
		*dst = append(*dst, hr)
		ex[key] = struct{}{}
	}
}

func horizonSourceRefKey(r horizonmodels.SourceRef) string {
	sid := strings.TrimSpace(r.SourceID)
	st := strings.TrimSpace(r.SourceType)
	if sid == "" && st == "" {
		return ""
	}
	return st + "|" + sid
}

func dedupStrings(in []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
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

func dedupHorizonSourceRefs(in []models.SourceRef) []horizonmodels.SourceRef {
	out := []horizonmodels.SourceRef{}
	seen := map[string]struct{}{}
	for _, r := range in {
		hr := horizonmodels.SourceRef{SourceID: r.SourceID, SourceType: r.SourceType}
		k := horizonSourceRefKey(hr)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, hr)
	}
	return out
}
