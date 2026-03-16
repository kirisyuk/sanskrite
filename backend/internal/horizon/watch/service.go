package watch

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

type Dashboard struct {
	Mode            string                          `json:"mode"`
	GlobalHotSlices []*horizonmodels.WatchBrief     `json:"globalHotSlices"`
	MyWatch         []*horizonmodels.WatchBrief     `json:"myWatch"`
	Feed            []*horizonmodels.WatchFeedEntry `json:"feed"`
	Preferences     *horizonmodels.WatchPreferences `json:"preferences,omitempty"`
}

type SearchResult struct {
	Kind          string `json:"kind"`
	Label         string `json:"label"`
	CanonicalPath string `json:"canonicalPath"`
	FragmentID    string `json:"fragmentId,omitempty"`
}

type Service struct {
	targetsRepo        *horizonrepo.WatchTargetsRepository
	briefsRepo         *horizonrepo.WatchBriefsRepository
	feedRepo           *horizonrepo.WatchFeedEntriesRepository
	prefsRepo          *horizonrepo.WatchPreferencesRepository
	fragmentsRepo      *horizonrepo.HorizonFragmentsRepository
	riskJobsRepo       *horizonrepo.RiskJobsRepository
	proposalsRepo      *horizonrepo.AgentProposalsRepository
	decisionsRepo      *horizonrepo.ArbitrationDecisionsRepository
	factsRepo          *corerepo.OntologyFactsRepository
	runtimeProvisioner func(context.Context, *horizonmodels.WatchTarget) error
}

func NewService(
	targetsRepo *horizonrepo.WatchTargetsRepository,
	briefsRepo *horizonrepo.WatchBriefsRepository,
	feedRepo *horizonrepo.WatchFeedEntriesRepository,
	prefsRepo *horizonrepo.WatchPreferencesRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	riskJobsRepo *horizonrepo.RiskJobsRepository,
	proposalsRepo *horizonrepo.AgentProposalsRepository,
	decisionsRepo *horizonrepo.ArbitrationDecisionsRepository,
	factsRepo *corerepo.OntologyFactsRepository,
) *Service {
	return &Service{
		targetsRepo:   targetsRepo,
		briefsRepo:    briefsRepo,
		feedRepo:      feedRepo,
		prefsRepo:     prefsRepo,
		fragmentsRepo: fragmentsRepo,
		riskJobsRepo:  riskJobsRepo,
		proposalsRepo: proposalsRepo,
		decisionsRepo: decisionsRepo,
		factsRepo:     factsRepo,
	}
}

func (s *Service) WithRuntimeProvisioner(fn func(context.Context, *horizonmodels.WatchTarget) error) *Service {
	if s == nil {
		return s
	}
	s.runtimeProvisioner = fn
	return s
}

func (s *Service) GetDashboard(ctx context.Context, tenantID, userID, mode string) (*Dashboard, error) {
	if s == nil || s.targetsRepo == nil || s.briefsRepo == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	mode = strings.TrimSpace(firstNonEmpty(mode, string(horizonmodels.WatchModeCombined)))

	globalTargets, err := s.targetsRepo.ListByTenant(ctx, "global", false, 100)
	if err != nil {
		return nil, err
	}
	tenantTargets := []*horizonmodels.WatchTarget{}
	if tenantID != "global" {
		tenantTargets, err = s.targetsRepo.ListByTenant(ctx, tenantID, false, 100)
		if err != nil {
			return nil, err
		}
	}
	globalBriefs, err := s.refreshBriefs(ctx, globalTargets)
	if err != nil {
		return nil, err
	}
	myBriefs, err := s.refreshBriefs(ctx, tenantTargets)
	if err != nil {
		return nil, err
	}
	sort.Slice(globalBriefs, func(i, j int) bool { return globalBriefs[i].Hotness > globalBriefs[j].Hotness })
	sort.Slice(myBriefs, func(i, j int) bool { return myBriefs[i].Hotness > myBriefs[j].Hotness })

	feed := []*horizonmodels.WatchFeedEntry{}
	briefSource := globalBriefs
	switch mode {
	case string(horizonmodels.WatchModeMy):
		briefSource = myBriefs
	case string(horizonmodels.WatchModeCombined):
		briefSource = append(append([]*horizonmodels.WatchBrief{}, globalBriefs...), myBriefs...)
	}
	feedSeen := map[string]struct{}{}
	for _, brief := range briefSource {
		if brief == nil {
			continue
		}
		entries, err := s.feedRepo.ListByTarget(ctx, brief.TenantID, brief.TargetID, 3)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry == nil {
				continue
			}
			key := strings.TrimSpace(entry.ID)
			if key == "" {
				key = strings.Join([]string{entry.TargetID, entry.EntryType, entry.CreatedAt.Format(time.RFC3339Nano)}, "|")
			}
			if _, ok := feedSeen[key]; ok {
				continue
			}
			feedSeen[key] = struct{}{}
			feed = append(feed, entry)
		}
	}
	sort.Slice(feed, func(i, j int) bool { return feed[i].CreatedAt.After(feed[j].CreatedAt) })
	if len(feed) > 12 {
		feed = feed[:12]
	}

	var prefs *horizonmodels.WatchPreferences
	if s.prefsRepo != nil && strings.TrimSpace(userID) != "" {
		prefs, _ = s.prefsRepo.GetByUser(ctx, tenantID, userID)
	}
	return &Dashboard{
		Mode:            mode,
		GlobalHotSlices: truncateBriefs(globalBriefs, 12),
		MyWatch:         truncateBriefs(myBriefs, 24),
		Feed:            feed,
		Preferences:     prefs,
	}, nil
}

func (s *Service) CreateTarget(ctx context.Context, target *horizonmodels.WatchTarget) (*horizonmodels.WatchTarget, error) {
	if s == nil || s.targetsRepo == nil || target == nil {
		return nil, nil
	}
	if strings.TrimSpace(target.Label) == "" {
		target.Label = labelFromCanonicalPath(target.CanonicalPath)
	}
	if err := s.targetsRepo.Upsert(ctx, target); err != nil {
		return nil, err
	}
	if s.runtimeProvisioner != nil {
		if err := s.runtimeProvisioner(ctx, target); err != nil {
			return nil, err
		}
	}
	brief, err := s.refreshBrief(ctx, target)
	if err == nil && brief != nil {
		target.LastBriefID = brief.ID
		_ = s.targetsRepo.Upsert(ctx, target)
	}
	return target, nil
}

func (s *Service) DeleteTarget(ctx context.Context, tenantID, targetID string) error {
	if s == nil || s.targetsRepo == nil {
		return nil
	}
	target, err := s.targetsRepo.Get(ctx, targetID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(target.TenantID) != strings.TrimSpace(tenantID) {
		return fmt.Errorf("target belongs to another tenant")
	}
	return s.targetsRepo.Delete(ctx, targetID)
}

func (s *Service) UpdatePreferences(ctx context.Context, prefs *horizonmodels.WatchPreferences) error {
	if s == nil || s.prefsRepo == nil || prefs == nil {
		return nil
	}
	return s.prefsRepo.Upsert(ctx, prefs)
}

func (s *Service) GetBrief(ctx context.Context, briefID string) (*horizonmodels.WatchBrief, error) {
	if s == nil || s.briefsRepo == nil {
		return nil, nil
	}
	return s.briefsRepo.Get(ctx, briefID)
}

func (s *Service) GetBriefEvidence(ctx context.Context, briefID string) (map[string]any, error) {
	brief, err := s.GetBrief(ctx, briefID)
	if err != nil || brief == nil {
		return nil, err
	}
	proposals, facts, err := s.loadBriefInputs(ctx, brief)
	if err != nil {
		return nil, err
	}
	evidence := make([]map[string]any, 0, len(proposals)+len(facts))
	for _, proposal := range proposals {
		if proposal == nil {
			continue
		}
		evidence = append(evidence, map[string]any{
			"type":         "proposal",
			"id":           proposal.ID,
			"proposalType": proposal.ProposalType,
			"signalKey":    proposal.SignalKey,
			"confidence":   proposal.Confidence,
			"explanation":  proposal.Explanation,
			"sourceRefs":   proposal.SourceRefs,
		})
	}
	for _, fact := range facts {
		if fact == nil {
			continue
		}
		evidence = append(evidence, map[string]any{
			"type":           "fact",
			"id":             fact.ID,
			"kind":           fact.Kind,
			"factType":       fact.Type,
			"title":          fact.Title,
			"summary":        fact.Summary,
			"confidence":     fact.Confidence,
			"sourceRefs":     fact.SourceRefs,
			"canonicalPaths": fact.CanonicalPaths,
		})
	}
	return map[string]any{
		"briefId":     brief.ID,
		"targetId":    brief.TargetID,
		"evidence":    evidence,
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) GetBriefSources(ctx context.Context, briefID string) (map[string]any, error) {
	brief, err := s.GetBrief(ctx, briefID)
	if err != nil || brief == nil {
		return nil, err
	}
	proposals, facts, err := s.loadBriefInputs(ctx, brief)
	if err != nil {
		return nil, err
	}
	sources := []string{}
	for _, proposal := range proposals {
		for _, ref := range proposal.SourceRefs {
			if src := strings.TrimSpace(ref.SourceID); src != "" {
				sources = append(sources, src)
			}
		}
	}
	for _, fact := range facts {
		for _, ref := range fact.SourceRefs {
			if src := strings.TrimSpace(ref.SourceID); src != "" {
				sources = append(sources, src)
			}
		}
	}
	return map[string]any{
		"briefId":  brief.ID,
		"targetId": brief.TargetID,
		"sources":  compactUnique(sources),
	}, nil
}

func (s *Service) GetBriefPaths(ctx context.Context, briefID string) (map[string]any, error) {
	brief, err := s.GetBrief(ctx, briefID)
	if err != nil || brief == nil {
		return nil, err
	}
	proposals, _, err := s.loadBriefInputs(ctx, brief)
	if err != nil {
		return nil, err
	}
	paths := []string{brief.CanonicalPath}
	for _, proposal := range proposals {
		if proposal == nil {
			continue
		}
		paths = append(paths, proposal.TargetCanonicalPaths...)
	}
	return map[string]any{
		"briefId":            brief.ID,
		"targetId":           brief.TargetID,
		"canonicalPaths":     compactUnique(paths),
		"topAffectedTargets": compactUnique(append([]string{}, brief.TopAffectedTargets...)),
	}, nil
}

func (s *Service) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if s == nil || s.fragmentsRepo == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 12
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	slug := taxonomy.Slugify(query)
	candidates := []string{}
	if strings.HasPrefix(query, "/") {
		candidates = append(candidates, query)
	}
	for _, prefix := range []string{"/loc/", "/org/", "/org/us/", "/market/", "/corridor/", "/region/"} {
		candidates = append(candidates, prefix+slug)
	}
	seen := map[string]struct{}{}
	out := make([]SearchResult, 0, limit)
	addFragment := func(path string) {
		if len(out) >= limit {
			return
		}
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, path)
		if err != nil || fragment == nil {
			return
		}
		seen[path] = struct{}{}
		out = append(out, SearchResult{
			Kind:          strings.ToLower(string(fragment.Type)),
			Label:         fragmentDisplayLabel(fragment.CanonicalPath, fragment.Labels),
			CanonicalPath: fragment.CanonicalPath,
			FragmentID:    fragment.ID,
		})
	}
	for _, candidate := range candidates {
		addFragment(candidate)
	}
	for _, prefix := range []string{"/loc/" + slug, "/org/" + slug, "/market/" + slug} {
		fragments, err := s.fragmentsRepo.ListByCanonicalPathPrefix(ctx, prefix, limit)
		if err != nil {
			continue
		}
		for _, fragment := range fragments {
			if fragment == nil {
				continue
			}
			addFragment(fragment.CanonicalPath)
		}
	}
	return out, nil
}

func (s *Service) refreshBriefs(ctx context.Context, targets []*horizonmodels.WatchTarget) ([]*horizonmodels.WatchBrief, error) {
	out := make([]*horizonmodels.WatchBrief, 0, len(targets))
	for _, target := range targets {
		if target == nil || strings.TrimSpace(target.Status) == string(horizonmodels.WatchTargetStatusDeleted) {
			continue
		}
		brief, err := s.refreshBrief(ctx, target)
		if err != nil || brief == nil {
			continue
		}
		out = append(out, brief)
	}
	return out, nil
}

func (s *Service) refreshBrief(ctx context.Context, target *horizonmodels.WatchTarget) (*horizonmodels.WatchBrief, error) {
	if s == nil || s.briefsRepo == nil || target == nil {
		return nil, nil
	}
	riskJobs := []*horizonmodels.RiskJob{}
	if s.riskJobsRepo != nil {
		riskJobs, _ = s.riskJobsRepo.ListRecentByTargetCanonicalPath(ctx, target.TenantID, target.CanonicalPath, 8)
	}
	briefStub := &horizonmodels.WatchBrief{ID: target.LastBriefID, TenantID: target.TenantID, TargetID: target.ID, CanonicalPath: target.CanonicalPath}
	proposals, facts, _ := s.loadBriefInputs(ctx, briefStub)
	factsCount := 0
	topSignals := []string{}
	topEvidence := []string{}
	factsCount = len(facts)
	for _, fact := range facts {
		if len(topSignals) < 4 && strings.TrimSpace(fact.Type) != "" {
			topSignals = append(topSignals, strings.TrimSpace(fact.Type))
		}
		for _, ref := range fact.SourceRefs {
			if len(topEvidence) >= 4 {
				break
			}
			if u := strings.TrimSpace(ref.SourceID); u != "" {
				topEvidence = append(topEvidence, u)
			}
		}
		if len(topEvidence) >= 4 {
			break
		}
	}
	accepted := 0
	var latestAccepted *horizonmodels.AgentProposal
	for _, proposal := range proposals {
		if proposal == nil {
			continue
		}
		if strings.TrimSpace(proposal.Status) == string(horizonmodels.AgentProposalStatusAccepted) {
			accepted++
			if latestAccepted == nil || proposal.CreatedAt.After(latestAccepted.CreatedAt) {
				latestAccepted = proposal
			}
		}
	}
	hotness := float64(len(riskJobs))*0.35 + float64(accepted)*0.25 + float64(factsCount)*0.05
	if hotness > 1 {
		hotness = 1
	}
	confidence := 0.25 + minFloat(0.55, float64(accepted)*0.15) + minFloat(0.2, float64(factsCount)*0.02)
	if confidence > 1 {
		confidence = 1
	}
	headline := fmt.Sprintf("%s under active watch", target.Label)
	summary := buildSummary(target, riskJobs, accepted, factsCount)
	explanation := buildExplanation(target, riskJobs, accepted, factsCount)
	forecast := map[string]any{
		"watchpoints":       target.DomainModes,
		"acceptedProposals": accepted,
		"recentRiskJobs":    len(riskJobs),
	}
	topAffectedTargets := []string{}
	if latestAccepted != nil {
		headline = firstNonEmpty(stringValueFromMap(latestAccepted.Payload, "title"), headline)
		summary = firstNonEmpty(stringValueFromMap(latestAccepted.Payload, "summary"), summary)
		explanation = firstNonEmpty(strings.TrimSpace(latestAccepted.Explanation), explanation)
		hotness = maxFloat(hotness, metricValueFromPayload(latestAccepted.Payload, latestAccepted.Confidence))
		confidence = maxFloat(confidence, latestAccepted.Confidence)
		topSignals = compactUnique(append([]string{latestAccepted.SignalKey}, topSignals...))
		for _, ref := range latestAccepted.SourceRefs {
			if src := strings.TrimSpace(ref.SourceID); src != "" {
				topEvidence = append([]string{src}, topEvidence...)
			}
		}
		for _, path := range latestAccepted.TargetCanonicalPaths {
			path = strings.TrimSpace(path)
			if path == "" || path == strings.TrimSpace(target.CanonicalPath) {
				continue
			}
			topAffectedTargets = append(topAffectedTargets, path)
		}
		forecast = mergeMaps(forecast, map[string]any{
			"watchpoints":       compactUnique(append(sliceStringValueFromMap(latestAccepted.Payload, "watchpoints"), target.DomainModes...)),
			"triggerConditions": sliceStringValueFromMap(latestAccepted.Payload, "triggerConditions"),
			"timeHorizon":       stringValueFromMap(latestAccepted.Payload, "timeHorizon"),
			"proposalType":      latestAccepted.ProposalType,
			"proposalId":        latestAccepted.ID,
		})
	}
	brief := &horizonmodels.WatchBrief{
		ID:                 target.ID,
		TenantID:           target.TenantID,
		TargetID:           target.ID,
		SubjectID:          target.SubjectID,
		CanonicalPath:      target.CanonicalPath,
		Label:              target.Label,
		ScopeType:          target.Kind,
		Headline:           headline,
		Summary:            summary,
		Hotness:            hotness,
		Confidence:         confidence,
		RiskPosture:        deriveRiskPosture(riskJobs, accepted),
		DominantThemes:     compactUnique(append(append([]string{}, target.DomainModes...), topSignals...)),
		TopAffectedTargets: compactUnique(topAffectedTargets),
		TopSignals:         compactUnique(topSignals),
		TopEvidence:        compactUnique(topEvidence),
		Explanation:        explanation,
		Forecast:           forecast,
		Metadata: map[string]any{
			"recentRiskJobs":    len(riskJobs),
			"recentFacts":       factsCount,
			"acceptedProposals": accepted,
			"targetStatus":      target.Status,
			"generatedAt":       time.Now().UTC().Format(time.RFC3339),
		},
	}
	if err := s.briefsRepo.Upsert(ctx, brief); err != nil {
		return nil, err
	}
	target.LastBriefID = brief.ID
	_ = s.targetsRepo.Upsert(ctx, target)
	return brief, nil
}

func (s *Service) loadBriefInputs(ctx context.Context, brief *horizonmodels.WatchBrief) ([]*horizonmodels.AgentProposal, []*coremodels.OntologyFact, error) {
	proposals := []*horizonmodels.AgentProposal{}
	facts := []*coremodels.OntologyFact{}
	if s == nil || brief == nil {
		return proposals, facts, nil
	}
	if s.proposalsRepo != nil && strings.TrimSpace(brief.TargetID) != "" {
		proposals, _ = s.proposalsRepo.ListByWatchTarget(ctx, brief.TenantID, brief.TargetID, 8)
		if brief.TenantID != "global" && len(proposals) == 0 {
			proposals, _ = s.proposalsRepo.ListByWatchTarget(ctx, "global", brief.TargetID, 8)
		}
	}
	if s.factsRepo != nil && strings.TrimSpace(brief.CanonicalPath) != "" {
		facts, _ = s.factsRepo.ListByCanonicalPath(ctx, "global", brief.CanonicalPath, "", 12)
	}
	return proposals, facts, nil
}

func buildSummary(target *horizonmodels.WatchTarget, riskJobs []*horizonmodels.RiskJob, accepted, factsCount int) string {
	return fmt.Sprintf("%s tracks %d recent risk runs, %d accepted agent proposals and %d fresh facts around %s.",
		target.Label, len(riskJobs), accepted, factsCount, target.CanonicalPath)
}

func buildExplanation(target *horizonmodels.WatchTarget, riskJobs []*horizonmodels.RiskJob, accepted, factsCount int) string {
	themes := compactUnique(target.DomainModes)
	if len(themes) == 0 {
		themes = []string{"general"}
	}
	return fmt.Sprintf("Brief is synthesized from watchdog/risk activity, arbitrated agent proposals and fact freshness for %s. Active themes: %s. Recent jobs: %d. Accepted proposals: %d. Facts: %d.",
		target.CanonicalPath, strings.Join(themes, ", "), len(riskJobs), accepted, factsCount)
}

func deriveRiskPosture(riskJobs []*horizonmodels.RiskJob, accepted int) string {
	if len(riskJobs) >= 3 || accepted >= 3 {
		return "elevated"
	}
	if len(riskJobs) > 0 || accepted > 0 {
		return "monitoring"
	}
	return "baseline"
}

func fragmentDisplayLabel(canonicalPath string, labels map[string]string) string {
	if label := strings.TrimSpace(labels["en"]); label != "" {
		return label
	}
	return labelFromCanonicalPath(canonicalPath)
}

func labelFromCanonicalPath(canonicalPath string) string {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(canonicalPath, "/"), "/")
	if len(parts) == 0 {
		return canonicalPath
	}
	last := strings.ReplaceAll(parts[len(parts)-1], "-", " ")
	return strings.Title(last)
}

func truncateBriefs(in []*horizonmodels.WatchBrief, limit int) []*horizonmodels.WatchBrief {
	if limit <= 0 || len(in) <= limit {
		return in
	}
	return in[:limit]
}

func compactUnique(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func mergeMaps(base map[string]any, add map[string]any) map[string]any {
	if base == nil && add == nil {
		return nil
	}
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range add {
		out[k] = v
	}
	return out
}

func stringValueFromMap(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	if value, ok := payload[key]; ok {
		if s, ok := value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func sliceStringValueFromMap(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return compactUnique(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return compactUnique(out)
	default:
		return nil
	}
}

func metricValueFromPayload(payload map[string]any, fallback float64) float64 {
	if payload == nil {
		return fallback
	}
	if v, ok := payload["metricValue"]; ok {
		switch typed := v.(type) {
		case float64:
			return typed
		case int:
			return float64(typed)
		}
	}
	return fallback
}
