package signals

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// EscalationAssessment is a deterministic baseline verdict for "is this serious this time?".
type EscalationAssessment struct {
	Level      string         `json:"level"`
	Confidence float64        `json:"confidence"`
	Reasons    []string       `json:"reasons,omitempty"`
	Buckets    map[string]int `json:"buckets,omitempty"` // intent/capability/preparation/corroboration
}

type EscalationClassifier struct {
	factsRepo  *corerepo.OntologyFactsRepository
	aiExecutor *ai.AITaskExecutor
	lookback   time.Duration
	maxFacts   int
}

func NewEscalationClassifier(factsRepo *corerepo.OntologyFactsRepository) *EscalationClassifier {
	return &EscalationClassifier{
		factsRepo: factsRepo,
		lookback:  30 * 24 * time.Hour,
		maxFacts:  200,
	}
}

func (c *EscalationClassifier) WithAIExecutor(aiExecutor *ai.AITaskExecutor) *EscalationClassifier {
	c.aiExecutor = aiExecutor
	return c
}

type escalationSample struct {
	ObservedAt time.Time
	Axis       string
	Amplitude  float64
	Modality   string
	SourceKey  string
}

func (c *EscalationClassifier) Assess(ctx context.Context, signal *horizonmodels.SignalDescriptor) (*EscalationAssessment, error) {
	if signal == nil {
		return nil, nil
	}

	samples := make([]escalationSample, 0, c.maxFacts+1)
	samples = append(samples, escalationSample{
		ObservedAt: signal.ObservedAt,
		Axis:       strings.TrimSpace(signal.Axis),
		Amplitude:  clamp01(signal.Amplitude),
		Modality:   horizonmodels.NormalizeSignalModality(signal.Constraints.Modality),
		SourceKey:  sourceKeyFromSignal(signal),
	})

	if c != nil && c.factsRepo != nil {
		paths := candidateHistoryPaths(signal)
		seenFactIDs := map[string]struct{}{}
		for _, p := range paths {
			facts, err := c.factsRepo.ListByCanonicalPathAndKind(ctx, "global", p, "signal", c.maxFacts)
			if err != nil {
				continue
			}
			for _, f := range facts {
				if f == nil {
					continue
				}
				if _, ok := seenFactIDs[f.ID]; ok {
					continue
				}
				seenFactIDs[f.ID] = struct{}{}
				if !matchesSignalContext(signal, f.ActorPaths, f.LocationPath) {
					continue
				}
				obs := f.UpdatedAt
				if f.ObservedAt != nil {
					obs = *f.ObservedAt
				}
				if !obs.IsZero() && time.Since(obs) > c.lookback {
					continue
				}
				samples = append(samples, escalationSample{
					ObservedAt: obs,
					Axis:       axisFromFactType(f.Type),
					Amplitude:  amplitudeFromFactSummary(f.Summary),
					Modality:   modalityFromFactTagsOrSummary(f.Tags, f.Summary),
					SourceKey:  sourceKeyFromFactSources(f.SourceIDs, f.SourceRefs),
				})
			}
		}
	}

	if len(samples) == 0 {
		return &EscalationAssessment{
			Level:      horizonmodels.EscalationLevelNone,
			Confidence: 0.25,
			Reasons:    []string{"no_samples"},
			Buckets: map[string]int{
				"intent":        0,
				"capability":    0,
				"preparation":   0,
				"corroboration": 0,
			},
		}, nil
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i].ObservedAt.Before(samples[j].ObservedAt) })

	count := len(samples)
	axisSet := map[string]struct{}{}
	sourceSet := map[string]struct{}{}
	maxAmp := 0.0
	for _, s := range samples {
		if strings.TrimSpace(s.Axis) != "" {
			axisSet[s.Axis] = struct{}{}
		}
		if strings.TrimSpace(s.SourceKey) != "" {
			sourceSet[s.SourceKey] = struct{}{}
		}
		if s.Amplitude > maxAmp {
			maxAmp = s.Amplitude
		}
	}

	rising := hasRisingAmplitudeTrend(samples)
	axisDiversity := len(axisSet)
	sourceDiversity := len(sourceSet)
	corroborated := sourceDiversity >= 2

	level := horizonmodels.EscalationLevelNone
	reasons := make([]string, 0, 4)
	if count >= 2 {
		level = horizonmodels.EscalationLevelWatch
		reasons = append(reasons, "signal_count>=2")
	}
	if count >= 3 && (rising || axisDiversity >= 2) {
		level = horizonmodels.EscalationLevelWarning
		reasons = append(reasons, "signal_count>=3_and_rising_or_multi_axis")
	}
	if count >= 4 && rising && corroborated && maxAmp >= 0.7 {
		level = horizonmodels.EscalationLevelCritical
		reasons = append(reasons, "signal_count>=4_and_rising_and_corroborated_and_high_amplitude")
	}

	conf := 0.35
	conf += 0.08 * float64(minInt(count, 5))
	if rising {
		conf += 0.12
	}
	if axisDiversity >= 2 {
		conf += 0.10
	}
	if corroborated {
		conf += 0.12
	}
	if maxAmp >= 0.7 {
		conf += 0.10
	}
	conf = clamp01(conf)

	buckets := evidenceBuckets(samples, sourceDiversity)
	assessment := &EscalationAssessment{
		Level:      level,
		Confidence: conf,
		Reasons:    reasons,
		Buckets:    buckets,
	}

	if shouldLLMEnrichEscalation(level) {
		if llmAssessment, err := c.assessWithLLM(ctx, signal, samples, assessment); err == nil && llmAssessment != nil {
			assessment = mergeEscalationAssessments(assessment, llmAssessment)
		}
	}

	return assessment, nil
}

func candidateHistoryPaths(signal *horizonmodels.SignalDescriptor) []string {
	out := make([]string, 0, 3)
	seen := map[string]struct{}{}
	appendPath := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	for _, sp := range signal.SeedPaths {
		appendPath(sp)
		if len(out) >= 2 {
			break
		}
	}
	if len(out) == 0 {
		appendPath(signal.LocationPath)
	}
	return out
}

func matchesSignalContext(signal *horizonmodels.SignalDescriptor, actorPaths []string, locationPath *string) bool {
	// Location filter
	if strings.TrimSpace(signal.LocationPath) != "" && locationPath != nil && strings.TrimSpace(*locationPath) != "" {
		if strings.TrimSpace(signal.LocationPath) != strings.TrimSpace(*locationPath) {
			return false
		}
	}
	// Actor overlap (if current signal has actors and historical one has actors).
	if len(signal.ActorPaths) > 0 && len(actorPaths) > 0 {
		set := map[string]struct{}{}
		for _, a := range signal.ActorPaths {
			a = strings.TrimSpace(a)
			if a != "" {
				set[a] = struct{}{}
			}
		}
		overlap := false
		for _, a := range actorPaths {
			a = strings.TrimSpace(a)
			if _, ok := set[a]; ok {
				overlap = true
				break
			}
		}
		if !overlap {
			return false
		}
	}
	return true
}

func hasRisingAmplitudeTrend(samples []escalationSample) bool {
	if len(samples) < 4 {
		return false
	}
	window := minInt(3, len(samples)/2)
	if window < 2 {
		return false
	}
	prev := samples[len(samples)-2*window : len(samples)-window]
	last := samples[len(samples)-window:]
	prevAvg := avgAmplitude(prev)
	lastAvg := avgAmplitude(last)
	return lastAvg > prevAvg+0.05
}

func avgAmplitude(samples []escalationSample) float64 {
	if len(samples) == 0 {
		return 0
	}
	sum := 0.0
	for _, s := range samples {
		sum += s.Amplitude
	}
	return sum / float64(len(samples))
}

func evidenceBuckets(samples []escalationSample, sourceDiversity int) map[string]int {
	out := map[string]int{
		"intent":        0,
		"capability":    0,
		"preparation":   0,
		"corroboration": sourceDiversity,
	}
	for _, s := range samples {
		switch strings.TrimSpace(s.Axis) {
		case horizonmodels.AxisGeoConflict, horizonmodels.AxisLegalAccess:
			out["intent"]++
		case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisOperationalContinuity:
			out["capability"]++
		}
		switch horizonmodels.NormalizeSignalModality(s.Modality) {
		case horizonmodels.SignalModalityEscalation, horizonmodels.SignalModalityDisruption, horizonmodels.SignalModalityConfirmed, horizonmodels.SignalModalityActual:
			out["preparation"]++
		}
	}
	return out
}

func axisFromFactType(t string) string {
	parts := strings.Split(strings.TrimSpace(t), ":")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func amplitudeFromFactSummary(summary string) float64 {
	s := strings.TrimSpace(summary)
	if s == "" {
		return 0.0
	}
	needle := "amplitude:"
	idx := strings.Index(strings.ToLower(s), needle)
	if idx < 0 {
		return 0.0
	}
	raw := strings.TrimSpace(s[idx+len(needle):])
	if semi := strings.Index(raw, ";"); semi >= 0 {
		raw = strings.TrimSpace(raw[:semi])
	}
	if comma := strings.Index(raw, ","); comma >= 0 {
		raw = strings.TrimSpace(raw[:comma])
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0.0
	}
	return clamp01(v)
}

func modalityFromFactTagsOrSummary(tags []string, summary string) string {
	for _, t := range tags {
		t = strings.TrimSpace(strings.ToLower(t))
		if strings.HasPrefix(t, "signal_modality:") {
			return horizonmodels.NormalizeSignalModality(strings.TrimPrefix(t, "signal_modality:"))
		}
	}
	needle := "modality:"
	idx := strings.Index(strings.ToLower(summary), needle)
	if idx < 0 {
		return ""
	}
	raw := strings.TrimSpace(summary[idx+len(needle):])
	if semi := strings.Index(raw, ";"); semi >= 0 {
		raw = strings.TrimSpace(raw[:semi])
	}
	if comma := strings.Index(raw, ","); comma >= 0 {
		raw = strings.TrimSpace(raw[:comma])
	}
	return horizonmodels.NormalizeSignalModality(raw)
}

func sourceKeyFromSignal(signal *horizonmodels.SignalDescriptor) string {
	parts := make([]string, 0, len(signal.SourceRefs)+1)
	if strings.TrimSpace(signal.FactID) != "" {
		parts = append(parts, strings.TrimSpace(signal.FactID))
	}
	for _, s := range signal.SourceRefs {
		s = strings.TrimSpace(s)
		if s != "" {
			parts = append(parts, s)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func sourceKeyFromFactSources(ids []string, refs []coremodels.SourceRef) string {
	parts := make([]string, 0, len(ids)+len(refs))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			parts = append(parts, id)
		}
	}
	for _, ref := range refs {
		if strings.TrimSpace(ref.SourceID) != "" {
			parts = append(parts, strings.TrimSpace(ref.SourceID))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func clamp01(v float64) float64 {
	return math.Max(0.0, math.Min(1.0, v))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func shouldLLMEnrichEscalation(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case horizonmodels.EscalationLevelWarning, horizonmodels.EscalationLevelCritical:
		return true
	default:
		return false
	}
}

func (c *EscalationClassifier) assessWithLLM(
	ctx context.Context,
	signal *horizonmodels.SignalDescriptor,
	samples []escalationSample,
	deterministic *EscalationAssessment,
) (*EscalationAssessment, error) {
	if c == nil || c.aiExecutor == nil || signal == nil || deterministic == nil {
		return nil, nil
	}

	evidence := c.collectEvidenceSnippets(ctx, signal)
	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentAnalysis,
		InputContent: buildEscalationLLMPrompt(signal, samples, deterministic, evidence),
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       6,
			MaxOutputSize:            1536,
			MinContextWindow:         4000,
		},
		Verifier: ai.NewMapVerifier([]string{"level", "confidence", "buckets"}, nil),
	}
	parsed, err := c.aiExecutor.ExecuteStructuredTask(ctx, task)
	if err != nil || parsed == nil {
		return nil, err
	}
	data, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected llm escalation result type: %T", parsed)
	}
	return parseLLMEscalationAssessment(data), nil
}

func buildEscalationLLMPrompt(
	signal *horizonmodels.SignalDescriptor,
	samples []escalationSample,
	deterministic *EscalationAssessment,
	evidence []string,
) string {
	if signal == nil || deterministic == nil {
		return ""
	}
	axisCounts := map[string]int{}
	for _, s := range samples {
		if axis := strings.TrimSpace(s.Axis); axis != "" {
			axisCounts[axis]++
		}
	}
	axisSummary := make([]string, 0, len(axisCounts))
	for axis, cnt := range axisCounts {
		axisSummary = append(axisSummary, fmt.Sprintf("- %s: %d", axis, cnt))
	}
	sort.Strings(axisSummary)
	if len(axisSummary) == 0 {
		axisSummary = append(axisSummary, "- n/a")
	}

	evidenceText := "- none"
	if len(evidence) > 0 {
		evidenceText = strings.Join(evidence, "\n")
	}

	return fmt.Sprintf(`Classify escalation level for an external systemic-risk signal.

Current Signal:
- axis: %s
- mechanism: %s
- polarity: %s
- amplitude: %.2f
- modality: %s
- location: %s
- observedAt: %s

Deterministic Baseline:
- level: %s
- confidence: %.2f
- buckets: intent=%d capability=%d preparation=%d corroboration=%d

Recent Sample Summary:
- samples: %d
%s

Evidence Snippets:
%s

Task:
Return strict JSON:
{
  "level": "none|watch|warning|critical",
  "confidence": 0.0,
  "buckets": {
    "intent": 0,
    "capability": 0,
    "preparation": 0,
    "corroboration": 0
  },
  "reasons": ["short reason 1", "short reason 2"]
}

Rules:
- Be conservative.
- If evidence is weak/noisy, do not over-escalate.
- Only output JSON.`,
		strings.TrimSpace(signal.Axis),
		strings.TrimSpace(signal.Mechanism),
		strings.TrimSpace(signal.Polarity),
		clamp01(signal.Amplitude),
		horizonmodels.NormalizeSignalModality(signal.Constraints.Modality),
		strings.TrimSpace(signal.LocationPath),
		signal.ObservedAt.UTC().Format(time.RFC3339),
		strings.TrimSpace(deterministic.Level),
		clamp01(deterministic.Confidence),
		deterministic.Buckets["intent"],
		deterministic.Buckets["capability"],
		deterministic.Buckets["preparation"],
		deterministic.Buckets["corroboration"],
		len(samples),
		strings.Join(axisSummary, "\n"),
		evidenceText,
	)
}

func parseLLMEscalationAssessment(data map[string]any) *EscalationAssessment {
	level := normalizeEscalationLevel(asString(data["level"]))
	if level == "" {
		level = horizonmodels.EscalationLevelNone
	}
	confidence := clamp01(asFloat(data["confidence"]))

	buckets := map[string]int{
		"intent":        0,
		"capability":    0,
		"preparation":   0,
		"corroboration": 0,
	}
	if raw, ok := data["buckets"].(map[string]any); ok {
		for key := range buckets {
			buckets[key] = int(math.Max(0, math.Round(asFloat(raw[key]))))
		}
	}

	reasons := make([]string, 0, 4)
	if raw, ok := data["reasons"].([]any); ok {
		for _, item := range raw {
			s := strings.TrimSpace(asString(item))
			if s != "" {
				reasons = append(reasons, "llm:"+s)
			}
		}
	}

	return &EscalationAssessment{
		Level:      level,
		Confidence: confidence,
		Reasons:    reasons,
		Buckets:    buckets,
	}
}

func mergeEscalationAssessments(det, llm *EscalationAssessment) *EscalationAssessment {
	if det == nil {
		return llm
	}
	if llm == nil {
		return det
	}
	level := strongerEscalationLevel(det.Level, llm.Level)
	conf := clamp01(det.Confidence*0.75 + llm.Confidence*0.25)
	if level == llm.Level && llm.Confidence > conf {
		conf = llm.Confidence
	}

	out := &EscalationAssessment{
		Level:      level,
		Confidence: conf,
		Reasons:    append(append([]string{}, det.Reasons...), llm.Reasons...),
		Buckets: map[string]int{
			"intent":        det.Buckets["intent"],
			"capability":    det.Buckets["capability"],
			"preparation":   det.Buckets["preparation"],
			"corroboration": det.Buckets["corroboration"],
		},
	}
	for key, v := range llm.Buckets {
		if v > out.Buckets[key] {
			out.Buckets[key] = v
		}
	}
	return out
}

func strongerEscalationLevel(a, b string) string {
	ar := escalationLevelRank(normalizeEscalationLevel(a))
	br := escalationLevelRank(normalizeEscalationLevel(b))
	if br > ar {
		return normalizeEscalationLevel(b)
	}
	return normalizeEscalationLevel(a)
}

func escalationLevelRank(level string) int {
	switch normalizeEscalationLevel(level) {
	case horizonmodels.EscalationLevelWatch:
		return 1
	case horizonmodels.EscalationLevelWarning:
		return 2
	case horizonmodels.EscalationLevelCritical:
		return 3
	default:
		return 0
	}
}

func normalizeEscalationLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case horizonmodels.EscalationLevelWatch:
		return horizonmodels.EscalationLevelWatch
	case horizonmodels.EscalationLevelWarning:
		return horizonmodels.EscalationLevelWarning
	case horizonmodels.EscalationLevelCritical:
		return horizonmodels.EscalationLevelCritical
	default:
		return horizonmodels.EscalationLevelNone
	}
}

func (c *EscalationClassifier) collectEvidenceSnippets(ctx context.Context, signal *horizonmodels.SignalDescriptor) []string {
	out := make([]string, 0, 6)
	if c == nil || c.factsRepo == nil || signal == nil {
		return out
	}
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(signal.SourceRefs)+2)
	if strings.TrimSpace(signal.FactID) != "" {
		ids = append(ids, strings.TrimSpace(signal.FactID))
	}
	ids = append(ids, signal.SourceRefs...)
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if len(out) >= 6 {
			break
		}
		f, err := c.factsRepo.Get(ctx, id)
		if err != nil || f == nil {
			continue
		}
		snippet := strings.TrimSpace(f.Title)
		if strings.TrimSpace(f.Summary) != "" {
			if snippet != "" {
				snippet += " | "
			}
			snippet += strings.TrimSpace(f.Summary)
		}
		if snippet == "" {
			continue
		}
		out = append(out, "- "+truncateText(snippet, 220))
	}
	return out
}

func truncateText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprint(v)
	}
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int32:
		return float64(t)
	case int64:
		return float64(t)
	case uint:
		return float64(t)
	case uint32:
		return float64(t)
	case uint64:
		return float64(t)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err == nil {
			return f
		}
		return 0
	default:
		return 0
	}
}
