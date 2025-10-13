package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/prompts"
)

// SourceProfiler handles AI-based source profiling
type SourceProfiler struct {
	executor *ai.AITaskExecutor
	logger   *log.Logger
}

// NewSourceProfiler creates a new source profiler
func NewSourceProfiler(executor *ai.AITaskExecutor, logger *log.Logger) *SourceProfiler {
	return &SourceProfiler{
		executor: executor,
		logger:   logger,
	}
}

// ProfileRequest contains the data needed for profiling
type ProfileRequest struct {
	SourceID      string
	ExtractedText string
	Language      string // "en", "ru", etc.
}

// ProfileSource performs AI-based analysis of a source and returns structured profile data
func (p *SourceProfiler) ProfileSource(ctx context.Context, req ProfileRequest) (*models.SourceProfileData, error) {
	if req.Language == "" {
		req.Language = "en"
	}

	// Format the profiling prompt
	promptText := prompts.Format(prompts.SourceProfilingPrompt, map[string]string{
		"sourceId":      req.SourceID,
		"extractedText": req.ExtractedText,
		"language":      req.Language,
	})

	// Create AI task
	task := &ai.AITask{
		TaskType:       "source_profiling",
		InputContent:   promptText,
		AIRequirements: ai.AIRequirements{TextGenerationCapability: true},
		Verifier:       &SourceProfileVerifier{},
	}

	// Execute structured task
	result, err := p.executor.ExecuteStructuredTask(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("AI profiling failed: %w", err)
	}

	// Type assertion
	profile, ok := result.(*models.SourceProfileData)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", result)
	}

	// Best-practice normalization (dedupe, clamp, renormalize shares)
	normalizeProfile(profile)
	return profile, nil
}

// SourceProfileVerifier verifies and parses source profile JSON
type SourceProfileVerifier struct{}

// VerifyAndParse implements AITaskResultVerifier
func (v *SourceProfileVerifier) VerifyAndParse(rawJSON string) (interface{}, error) {
	var profile models.SourceProfileData

	// Use strict JSON decoder
	decoder := json.NewDecoder(bytes.NewReader([]byte(rawJSON)))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&profile); err != nil {
		return nil, fmt.Errorf("JSON decode failed: %w", err)
	}

	// Validate ranges
	if err := validateProfile(&profile); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return &profile, nil
}

// validateProfile validates numeric ranges and constraints
func validateProfile(p *models.SourceProfileData) error {
	// Validate confidence scores [0..1]
	if p.EditorialStance.Confidence < 0 || p.EditorialStance.Confidence > 1 {
		return fmt.Errorf("editorialStance.confidence must be in [0, 1]")
	}
	if p.TrustScore < 0 || p.TrustScore > 1 {
		return fmt.Errorf("trustScore must be in [0, 1]")
	}
	if p.BehaviorPatterns.EventResponsiveness < 0 || p.BehaviorPatterns.EventResponsiveness > 1 {
		return fmt.Errorf("behaviorPatterns.eventResponsiveness must be in [0, 1]")
	}
	if p.BehaviorPatterns.ContentConsistency < 0 || p.BehaviorPatterns.ContentConsistency > 1 {
		return fmt.Errorf("behaviorPatterns.contentConsistency must be in [0, 1]")
	}
	if p.BehaviorPatterns.OriginalityScore < 0 || p.BehaviorPatterns.OriginalityScore > 1 {
		return fmt.Errorf("behaviorPatterns.originalityScore must be in [0, 1]")
	}

	// Validate topics shares sum <= 1
	if p.Topics != nil {
		totalShare := 0.0
		for _, topic := range p.Topics {
			if topic.Share < 0 || topic.Share > 1 {
				return fmt.Errorf("topic '%s' share must be in [0, 1]", topic.Name)
			}
			totalShare += topic.Share
		}
		if totalShare > 1.01 { // Allow small floating point error
			return fmt.Errorf("total topics share exceeds 1.0: %.2f", totalShare)
		}
	}

	// Validate editorial methods shares
	if p.EditorialMethods != nil {
		totalShare := 0.0
		for _, method := range p.EditorialMethods {
			if method.Share < 0 || method.Share > 1 {
				return fmt.Errorf("editorial method '%s' share must be in [0, 1]", method.Method)
			}
			totalShare += method.Share
		}
		if totalShare > 1.01 {
			return fmt.Errorf("total editorial methods share exceeds 1.0: %.2f", totalShare)
		}
	}

	// Validate goals confidence
	if p.Goals != nil {
		for _, goal := range p.Goals {
			if goal.Confidence < 0 || goal.Confidence > 1 {
				return fmt.Errorf("goal '%s' confidence must be in [0, 1]", goal.Goal)
			}
		}
	}

	// Validate goalAlignmentScore
	if p.GoalAlignmentScore != nil {
		if *p.GoalAlignmentScore < 0 || *p.GoalAlignmentScore > 1 {
			return fmt.Errorf("goalAlignmentScore must be in [0, 1]")
		}
	}

	// Validate emotional tone distribution
	if p.ContentPatterns != nil && p.ContentPatterns.EmotionalToneDistribution != nil {
		etd := p.ContentPatterns.EmotionalToneDistribution
		total := 0.0
		if etd.Neutral != nil {
			if *etd.Neutral < 0 || *etd.Neutral > 1 {
				return fmt.Errorf("emotionalTone.neutral must be in [0, 1]")
			}
			total += *etd.Neutral
		}
		if etd.Outrage != nil {
			if *etd.Outrage < 0 || *etd.Outrage > 1 {
				return fmt.Errorf("emotionalTone.outrage must be in [0, 1]")
			}
			total += *etd.Outrage
		}
		if etd.Fear != nil {
			if *etd.Fear < 0 || *etd.Fear > 1 {
				return fmt.Errorf("emotionalTone.fear must be in [0, 1]")
			}
			total += *etd.Fear
		}
		if etd.Hope != nil {
			if *etd.Hope < 0 || *etd.Hope > 1 {
				return fmt.Errorf("emotionalTone.hope must be in [0, 1]")
			}
			total += *etd.Hope
		}
		if total > 1.01 {
			return fmt.Errorf("total emotional tone distribution exceeds 1.0: %.2f", total)
		}
	}

	// Validate red flags scores
	if p.RedFlags != nil {
		for _, flag := range p.RedFlags {
			if flag.Score < 0 || flag.Score > 1 {
				return fmt.Errorf("redFlag '%s' score must be in [0, 1]", flag.Code)
			}
		}
	}

	// Validate coordinated sources scores
	if p.CoordinatedSources != nil {
		for _, cs := range p.CoordinatedSources {
			if cs.CoordinationScore < 0 || cs.CoordinationScore > 1 {
				return fmt.Errorf("coordinatedSource '%s' score must be in [0, 1]", cs.SourceID)
			}
		}
	}

	// Validate metrics
	if p.Metrics != nil {
		if p.Metrics.ClaimDensity != nil && (*p.Metrics.ClaimDensity < 0 || *p.Metrics.ClaimDensity > 1) {
			return fmt.Errorf("metrics.claimDensity must be in [0, 1]")
		}
		if p.Metrics.ExternalRefShare != nil && (*p.Metrics.ExternalRefShare < 0 || *p.Metrics.ExternalRefShare > 1) {
			return fmt.Errorf("metrics.externalRefShare must be in [0, 1]")
		}
		if p.Metrics.Polarization != nil && (*p.Metrics.Polarization < 0 || *p.Metrics.Polarization > 1) {
			return fmt.Errorf("metrics.polarization must be in [0, 1]")
		}
		if p.Metrics.CTAShare != nil && (*p.Metrics.CTAShare < 0 || *p.Metrics.CTAShare > 1) {
			return fmt.Errorf("metrics.ctaShare must be in [0, 1]")
		}
		if p.Metrics.PromoLinkShare != nil && (*p.Metrics.PromoLinkShare < 0 || *p.Metrics.PromoLinkShare > 1) {
			return fmt.Errorf("metrics.promoLinkShare must be in [0, 1]")
		}
		if p.Metrics.EvidenceQuality != nil && (*p.Metrics.EvidenceQuality < 0 || *p.Metrics.EvidenceQuality > 1) {
			return fmt.Errorf("metrics.evidenceQuality must be in [0, 1]")
		}
	}

	return nil
}

// normalizeProfile applies non-destructive best-practice cleanup:
// - trims/normalizes strings
// - merges duplicates case-insensitively
// - thresholds tiny shares and renormalizes to sum ≤ 1
// - clamps numeric scores to [0,1]
// - validates/canonicalizes simple enums
func normalizeProfile(p *models.SourceProfileData) {
	// Editorial stance evidence: trim empties
	if p.EditorialStance.Evidence != nil {
		kept := make([]string, 0, len(p.EditorialStance.Evidence))
		for _, e := range p.EditorialStance.Evidence {
			t := strings.TrimSpace(e)
			if t != "" {
				kept = append(kept, t)
			}
		}
		p.EditorialStance.Evidence = kept
	}

	// Agenda
	if p.Agenda != nil {
		if p.Agenda.PrimaryGoal != nil {
			v := strings.TrimSpace(*p.Agenda.PrimaryGoal)
			p.Agenda.PrimaryGoal = &v
		}
		if p.Agenda.Description != nil {
			v := strings.TrimSpace(*p.Agenda.Description)
			p.Agenda.Description = &v
		}
		// promoted/avoided themes – dedup lower
		dedupStrSlice := func(in []string) []string {
			seen := map[string]struct{}{}
			out := make([]string, 0, len(in))
			for _, s := range in {
				t := strings.TrimSpace(s)
				if t == "" {
					continue
				}
				key := strings.ToLower(t)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, t)
			}
			return out
		}
		p.Agenda.PromotedThemes = dedupStrSlice(p.Agenda.PromotedThemes)
		p.Agenda.AvoidedThemes = dedupStrSlice(p.Agenda.AvoidedThemes)
		if p.Agenda.TargetAudience != nil {
			v := strings.TrimSpace(*p.Agenda.TargetAudience)
			p.Agenda.TargetAudience = &v
		}
	}

	// Clamp behavior patterns
	clamp01 := func(x float64) float64 {
		if x < 0 {
			return 0
		}
		if x > 1 {
			return 1
		}
		return x
	}
	p.BehaviorPatterns.EventResponsiveness = clamp01(p.BehaviorPatterns.EventResponsiveness)
	p.BehaviorPatterns.ContentConsistency = clamp01(p.BehaviorPatterns.ContentConsistency)
	p.BehaviorPatterns.OriginalityScore = clamp01(p.BehaviorPatterns.OriginalityScore)

	// Emotional tone distribution: clamp and renormalize if > 1
	if p.ContentPatterns != nil && p.ContentPatterns.EmotionalToneDistribution != nil {
		etd := p.ContentPatterns.EmotionalToneDistribution
		vals := []*float64{etd.Neutral, etd.Outrage, etd.Fear, etd.Hope}
		sum := 0.0
		for _, ref := range vals {
			if ref != nil {
				v := clamp01(*ref)
				*ref = v
				sum += v
			}
		}
		if sum > 1.0001 {
			// renormalize proportionally
			for _, ref := range vals {
				if ref != nil {
					*ref = *ref / sum
				}
			}
		}
	}

	// Focus entities: dedup by entityName (case-insensitive), sum mentions, avg sentiment weighted
	if p.ContentPatterns != nil && p.ContentPatterns.FocusEntityNames != nil {
		type agg struct {
			count    int
			mentions int
			sentSum  float64
		}
		by := map[string]agg{}
		for _, fe := range p.ContentPatterns.FocusEntityNames {
			name := strings.TrimSpace(fe.EntityName)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			a := by[key]
			a.count++
			a.mentions += fe.MentionCount
			a.sentSum += fe.AverageSentiment * float64(maxInt(1, fe.MentionCount))
			by[key] = a
		}
		out := make([]models.FocusEntity, 0, len(by))
		for k, a := range by {
			avg := 0.0
			if a.mentions > 0 {
				avg = a.sentSum / float64(a.mentions)
			}
			out = append(out, models.FocusEntity{EntityName: k, MentionCount: a.mentions, AverageSentiment: avg})
		}
		// sort by mentions desc
		sort.Slice(out, func(i, j int) bool { return out[i].MentionCount > out[j].MentionCount })
		if len(out) > 20 {
			out = out[:20]
		}
		p.ContentPatterns.FocusEntityNames = out
	}

	// Topics: merge duplicates (case-insensitive), drop tiny, renormalize, sort
	if p.Topics != nil {
		type topic struct {
			name  string
			share float64
			trend *string
		}
		by := map[string]topic{}
		for _, t := range p.Topics {
			name := strings.TrimSpace(t.Name)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			cur := by[key]
			cur.name = name
			cur.share += t.Share
			if t.Trend != nil {
				tv := strings.ToLower(strings.TrimSpace(*t.Trend))
				if tv == "up" || tv == "down" || tv == "flat" {
					cur.trend = &tv
				}
			}
			by[key] = cur
		}
		// threshold and collect
		const minShare = 0.02
		merged := make([]models.Topic, 0, len(by))
		sum := 0.0
		for _, v := range by {
			if v.share < minShare {
				continue
			}
			sum += v.share
		}
		// renormalize if needed
		for _, v := range by {
			if v.share < minShare {
				continue
			}
			s := v.share
			if sum > 0 {
				s = s / sum
			}
			merged = append(merged, models.Topic{Name: v.name, Share: s, Trend: v.trend})
		}
		sort.Slice(merged, func(i, j int) bool { return merged[i].Share > merged[j].Share })
		if len(merged) > 12 {
			merged = merged[:12]
		}
		p.Topics = merged
	}

	// Editorial methods: merge by method, threshold and renormalize
	if p.EditorialMethods != nil {
		by := map[string]float64{}
		for _, m := range p.EditorialMethods {
			name := strings.TrimSpace(m.Method)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			by[key] += m.Share
		}
		const minShare = 0.02
		sum := 0.0
		for _, s := range by {
			if s >= minShare {
				sum += s
			}
		}
		out := make([]models.EditorialMethod, 0, len(by))
		for k, s := range by {
			if s < minShare {
				continue
			}
			if sum > 0 {
				s = s / sum
			}
			out = append(out, models.EditorialMethod{Method: k, Share: s})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Share > out[j].Share })
		if len(out) > 12 {
			out = out[:12]
		}
		p.EditorialMethods = out
	}

	// Goals: dedup by text, keep max confidence
	if p.Goals != nil {
		by := map[string]float64{}
		for _, g := range p.Goals {
			name := strings.TrimSpace(g.Goal)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			c := clamp01(g.Confidence)
			if c > by[key] {
				by[key] = c
			}
		}
		out := make([]models.Goal, 0, len(by))
		for k, c := range by {
			out = append(out, models.Goal{Goal: k, Confidence: c})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
		if len(out) > 20 {
			out = out[:20]
		}
		p.Goals = out
	}

	// Target audience countries: enforce ISO-3166-1 alpha-2 uppercase
	if p.TargetAudienceCountries != nil {
		out := make([]string, 0, len(p.TargetAudienceCountries))
		seen := map[string]struct{}{}
		for _, c := range p.TargetAudienceCountries {
			code := strings.ToUpper(strings.TrimSpace(c))
			if len(code) != 2 {
				continue
			}
			if code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
				continue
			}
			if _, ok := seen[code]; ok {
				continue
			}
			seen[code] = struct{}{}
			out = append(out, code)
		}
		p.TargetAudienceCountries = out
	}

	// Red flags: dedup by code, keep max score, clamp 0..1
	if p.RedFlags != nil {
		by := map[string]models.RedFlag{}
		for _, rf := range p.RedFlags {
			code := strings.TrimSpace(rf.Code)
			if code == "" {
				continue
			}
			if rf.Score < 0 {
				rf.Score = 0
			} else if rf.Score > 1 {
				rf.Score = 1
			}
			if old, ok := by[code]; !ok || rf.Score > old.Score {
				by[code] = models.RedFlag{Code: code, Score: rf.Score, Rationale: rf.Rationale}
			}
		}
		out := make([]models.RedFlag, 0, len(by))
		for _, v := range by {
			out = append(out, v)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
		if len(out) > 20 {
			out = out[:20]
		}
		p.RedFlags = out
	}

	// Coordinated sources: dedup by sourceId, keep max score, clamp 0..1
	if p.CoordinatedSources != nil {
		by := map[string]float64{}
		for _, cs := range p.CoordinatedSources {
			id := strings.TrimSpace(cs.SourceID)
			if id == "" {
				continue
			}
			s := clamp01(cs.CoordinationScore)
			if s > by[id] {
				by[id] = s
			}
		}
		out := make([]models.CoordinatedSource, 0, len(by))
		for id, s := range by {
			out = append(out, models.CoordinatedSource{SourceID: id, CoordinationScore: s})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].CoordinationScore > out[j].CoordinationScore })
		if len(out) > 20 {
			out = out[:20]
		}
		p.CoordinatedSources = out
	}

	// Metrics: clamp 0..1
	if p.Metrics != nil {
		if p.Metrics.ClaimDensity != nil {
			v := clamp01(*p.Metrics.ClaimDensity)
			p.Metrics.ClaimDensity = &v
		}
		if p.Metrics.ExternalRefShare != nil {
			v := clamp01(*p.Metrics.ExternalRefShare)
			p.Metrics.ExternalRefShare = &v
		}
		if p.Metrics.Polarization != nil {
			v := clamp01(*p.Metrics.Polarization)
			p.Metrics.Polarization = &v
		}
		if p.Metrics.CTAShare != nil {
			v := clamp01(*p.Metrics.CTAShare)
			p.Metrics.CTAShare = &v
		}
		if p.Metrics.PromoLinkShare != nil {
			v := clamp01(*p.Metrics.PromoLinkShare)
			p.Metrics.PromoLinkShare = &v
		}
		if p.Metrics.EvidenceQuality != nil {
			v := clamp01(*p.Metrics.EvidenceQuality)
			p.Metrics.EvidenceQuality = &v
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
