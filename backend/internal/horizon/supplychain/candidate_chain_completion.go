package supplychain

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/canonical"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type CandidateChainCompletionService struct {
	lensesRepo       *horizonrepo.LensesRepository
	materializedRepo *horizonrepo.MaterializedArtifactsRepository
}

type CandidateChainCompletionDeps struct {
	LensesRepo       *horizonrepo.LensesRepository
	MaterializedRepo *horizonrepo.MaterializedArtifactsRepository
}

type CandidateChainCompletionOptions struct {
	LensID               string
	MaxArtifactsPerChain int
	MaxCandidates        int
	MinUnionCountries    int
	DryRun               bool
}

type CandidateChainCompletionResult struct {
	LensID          string
	Created         int
	Updated         int
	Deleted         int
	Skipped         int
	CandidateKeys   []string
	CandidateTitles []string
}

type candidateChainArtifact struct {
	artifact       *horizonmodels.MaterializedArtifact
	parsed         *horizonmodels.SupplyChainPathPayload
	productPath    string
	countries      []string
	riskScore      float64
	bindingScore   float64
	recoveryHours  float64
	maxStressScore float64
}

type candidateChainGroup struct {
	countryCode string
	productPath string
	artifacts   []*candidateChainArtifact
}

func NewCandidateChainCompletionService(deps CandidateChainCompletionDeps) *CandidateChainCompletionService {
	if deps.LensesRepo == nil || deps.MaterializedRepo == nil {
		return nil
	}
	return &CandidateChainCompletionService{
		lensesRepo:       deps.LensesRepo,
		materializedRepo: deps.MaterializedRepo,
	}
}

func (s *CandidateChainCompletionService) CompleteLens(ctx context.Context, opts CandidateChainCompletionOptions) (*CandidateChainCompletionResult, error) {
	if s == nil || s.lensesRepo == nil || s.materializedRepo == nil {
		return nil, fmt.Errorf("candidate chain completion service is not configured")
	}
	lensID := strings.TrimSpace(opts.LensID)
	if lensID == "" {
		return nil, fmt.Errorf("lens id is required")
	}
	if opts.MaxArtifactsPerChain <= 0 {
		opts.MaxArtifactsPerChain = 4
	}
	if opts.MaxCandidates <= 0 {
		opts.MaxCandidates = 8
	}
	if opts.MinUnionCountries <= 0 {
		opts.MinUnionCountries = 3
	}

	lens, err := s.lensesRepo.Get(ctx, lensID)
	if err != nil {
		return nil, fmt.Errorf("load lens failed: %w", err)
	}
	artifacts, err := s.materializedRepo.ListByLens(ctx, lensID, 512)
	if err != nil {
		return nil, fmt.Errorf("list lens artifacts failed: %w", err)
	}

	primaryGeo := detectPrimaryGeo(lens)
	deleted, err := s.pruneManagedCandidates(ctx, lens, artifacts, primaryGeo)
	if err != nil {
		return nil, fmt.Errorf("prune candidate artifacts failed: %w", err)
	}
	artifacts, err = s.materializedRepo.ListByLens(ctx, lensID, 512)
	if err != nil {
		return nil, fmt.Errorf("reload lens artifacts failed: %w", err)
	}
	groups := buildCandidateGroups(artifacts, primaryGeo)
	result := &CandidateChainCompletionResult{LensID: lensID, Deleted: deleted}

	for _, group := range groups {
		if result.Created+result.Updated >= opts.MaxCandidates {
			break
		}
		selected, unionCountries := selectCandidateArtifacts(group.artifacts, opts.MaxArtifactsPerChain)
		if len(selected) < 2 || len(unionCountries) < opts.MinUnionCountries {
			result.Skipped++
			continue
		}
		maxSingleCountries := 0
		for _, artifact := range selected {
			if artifact != nil && len(artifact.countries) > maxSingleCountries {
				maxSingleCountries = len(artifact.countries)
			}
		}
		if len(unionCountries) <= maxSingleCountries {
			result.Skipped++
			continue
		}

		candidate, artKey, title, err := buildCandidateArtifact(lens, group.countryCode, group.productPath, selected)
		if err != nil {
			result.Skipped++
			continue
		}
		if opts.DryRun {
			result.CandidateKeys = append(result.CandidateKeys, candidate.CanonicalKey)
			result.CandidateTitles = append(result.CandidateTitles, title)
			continue
		}

		existing, err := s.materializedRepo.GetByCanonicalKey(ctx, candidate.TenantID, horizonmodels.MaterializedKindSupplyChainPath, candidate.CanonicalKey)
		if err == nil && existing != nil {
			candidate.ID = existing.ID
			candidate.CreatedAt = existing.CreatedAt
			candidate.CreatedBy = existing.CreatedBy
			if err := s.materializedRepo.Update(ctx, candidate); err != nil {
				return nil, fmt.Errorf("update candidate artifact failed: %w", err)
			}
			result.Updated++
		} else {
			if _, err := s.materializedRepo.Create(ctx, candidate); err != nil {
				return nil, fmt.Errorf("create candidate artifact failed: %w", err)
			}
			result.Created++
		}
		if err := s.lensesRepo.UpsertScopeArtifact(ctx, lens.ID, artKey, scopeArtifactRefFromArtifact(candidate)); err != nil {
			return nil, fmt.Errorf("upsert scope artifact failed: %w", err)
		}
		result.CandidateKeys = append(result.CandidateKeys, candidate.CanonicalKey)
		result.CandidateTitles = append(result.CandidateTitles, title)
	}
	return result, nil
}

func buildCandidateGroups(artifacts []*horizonmodels.MaterializedArtifact, primaryGeo string) []candidateChainGroup {
	groupMap := map[string]*candidateChainGroup{}
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			continue
		}
		if !candidateEligibleSourceArtifact(artifact) {
			continue
		}
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		productPath := strings.TrimSpace(artifact.ProductAxis)
		if productPath == "" {
			continue
		}
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		countries := artifactCountries(artifact, primaryGeo)
		if len(countries) == 0 {
			continue
		}
		row := &candidateChainArtifact{
			artifact:       artifact,
			parsed:         parsed,
			productPath:    productPath,
			countries:      countries,
			riskScore:      candidateArtifactRiskScore(artifact),
			bindingScore:   artifact.BindingScore,
			recoveryHours:  artifact.RecoveryLatencyHrs,
			maxStressScore: artifact.PathStressScore,
		}
		for _, countryCode := range countries {
			groupKey := countryCode + "::" + productPath
			group := groupMap[groupKey]
			if group == nil {
				group = &candidateChainGroup{countryCode: countryCode, productPath: productPath}
				groupMap[groupKey] = group
			}
			group.artifacts = append(group.artifacts, row)
		}
	}

	out := make([]candidateChainGroup, 0, len(groupMap))
	for _, group := range groupMap {
		if group == nil || len(group.artifacts) == 0 {
			continue
		}
		sort.SliceStable(group.artifacts, func(i, j int) bool {
			if group.artifacts[i].riskScore == group.artifacts[j].riskScore {
				return strings.TrimSpace(group.artifacts[i].artifact.CanonicalKey) < strings.TrimSpace(group.artifacts[j].artifact.CanonicalKey)
			}
			return group.artifacts[i].riskScore > group.artifacts[j].riskScore
		})
		out = append(out, *group)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := 0.0
		right := 0.0
		if len(out[i].artifacts) > 0 {
			left = out[i].artifacts[0].riskScore
		}
		if len(out[j].artifacts) > 0 {
			right = out[j].artifacts[0].riskScore
		}
		if left == right {
			return out[i].countryCode+out[i].productPath < out[j].countryCode+out[j].productPath
		}
		return left > right
	})
	return out
}

func selectCandidateArtifacts(items []*candidateChainArtifact, maxArtifacts int) ([]*candidateChainArtifact, []string) {
	if len(items) == 0 {
		return nil, nil
	}
	selected := make([]*candidateChainArtifact, 0, candidateMinInt(maxArtifacts, len(items)))
	seen := map[string]struct{}{}
	unionCountries := map[string]struct{}{}
	for _, item := range items {
		if item == nil || item.artifact == nil {
			continue
		}
		key := strings.TrimSpace(item.artifact.CanonicalKey)
		if key == "" {
			key = strings.TrimSpace(item.artifact.ID)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		selected = append(selected, item)
		for _, code := range item.countries {
			unionCountries[code] = struct{}{}
		}
		if len(selected) >= maxArtifacts {
			break
		}
	}
	return selected, sortedStringKeys(unionCountries)
}

func buildCandidateArtifact(lens *horizonmodels.Lens, countryCode, productPath string, source []*candidateChainArtifact) (*horizonmodels.MaterializedArtifact, string, string, error) {
	if lens == nil || len(source) == 0 {
		return nil, "", "", fmt.Errorf("candidate source is empty")
	}
	mergedPayload := source[0].artifact.Payload
	selectedKeys := make([]string, 0, len(source))
	selectedIDs := make([]string, 0, len(source))
	for idx, item := range source {
		if item == nil || item.artifact == nil {
			continue
		}
		if idx > 0 {
			mergedPayload = horizonmodels.MergePayloads(mergedPayload, item.artifact.Payload)
		}
		selectedKeys = append(selectedKeys, strings.TrimSpace(item.artifact.CanonicalKey))
		selectedIDs = append(selectedIDs, strings.TrimSpace(item.artifact.ID))
	}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(mergedPayload)
	if err != nil || parsed == nil {
		return nil, "", "", fmt.Errorf("parse merged payload failed: %w", err)
	}

	for i := range parsed.Legs {
		parsed.Legs[i].Index = i
		parsed.Legs[i].BindingConfidence = candidateMinFloat(nonZeroOr(parsed.Legs[i].BindingConfidence, 0.34), 0.34)
		parsed.Legs[i].SegmentConfidence = candidateMinFloat(nonZeroOr(parsed.Legs[i].SegmentConfidence, 0.42), 0.42)
		if parsed.Legs[i].Stress == nil {
			parsed.Legs[i].Stress = &horizonmodels.SupplyChainLegStress{}
		}
		parsed.Legs[i].Stress.DominantFactors = compactStringsLocal(append(parsed.Legs[i].Stress.DominantFactors, "candidate_chain_completion"))
	}
	if parsed.Sufficiency == nil {
		parsed.Sufficiency = &horizonmodels.SupplyChainPathSufficiency{}
	}
	parsed.Sufficiency.Status = horizonmodels.SupplyChainSufficiencyHeuristic
	if parsed.Sufficiency.Score <= 0 || parsed.Sufficiency.Score > 0.58 {
		parsed.Sufficiency.Score = 0.48
	}
	parsed.Sufficiency.OpenGaps = compactStringsLocal(append(parsed.Sufficiency.OpenGaps,
		"candidate_chain_completion",
		"missing_validated_leg_family",
	))
	parsed.Sufficiency.Warnings = compactStringsLocal(append(parsed.Sufficiency.Warnings,
		"inferred route family stitched from multiple validated paths",
	))
	parsed.Sufficiency.Summary = "Inferred route family assembled from multiple validated paths; continuity is useful but not fully validated."
	parsed.Family = &horizonmodels.SupplyChainPathFamily{
		ID:                 candidateFamilyID(lens.ID, countryCode, productPath),
		ProductAxis:        productPath,
		PathCount:          len(source),
		ValidatedPathCount: 0,
		HeuristicPathCount: len(source),
		GapPathCount:       1,
		OpenGaps:           append([]string(nil), parsed.Sufficiency.OpenGaps...),
	}

	maxStress := 0.0
	for _, item := range source {
		if item != nil {
			maxStress = candidateMaxFloat(maxStress, item.maxStressScore)
		}
	}
	if parsed.Stress == nil {
		parsed.Stress = &horizonmodels.SupplyChainPathStress{}
	}
	parsed.Stress.PathScore = candidateMaxFloat(parsed.Stress.PathScore, maxStress)
	parsed.Stress.DominantFactors = compactStringsLocal(append(parsed.Stress.DominantFactors, "candidate_chain_completion"))

	notes := cloneMapLocal(parsed.Notes)
	notes["candidateChain"] = true
	notes["candidateCountryCode"] = countryCode
	notes["candidateCountryName"] = countryNameLocal(countryCode)
	notes["candidateProductAxis"] = productPath
	notes["candidateSourceArtifactIds"] = compactStringsLocal(selectedIDs)
	notes["candidateSourceArtifactKeys"] = compactStringsLocal(selectedKeys)
	notes["buildMode"] = "candidate_chain_completion"
	notes["candidateDisplayStatus"] = "inferred"
	notes["generatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	parsed.Notes = notes

	payload := horizonmodels.BuildSupplyChainPathPayloadDetailed(parsed.Nodes, parsed.Edges, parsed.Legs, parsed.Stress, parsed.Sufficiency, parsed.Family, parsed.Notes)
	parsedForSummary, _ := horizonmodels.ParseSupplyChainPathPayload(payload)
	summary := horizonmodels.SummarizeSupplyChainPathBindings(parsedForSummary)

	title := fmt.Sprintf("%s shoulder -> %s (inferred route family)", countryNameLocal(countryCode), titleWordsLocal(lastPathSegmentLocal(productPath)))
	canonicalKey := buildCandidateChainCanonicalKey(lens.ID, countryCode, productPath)
	artifact := &horizonmodels.MaterializedArtifact{
		TenantID:     candidateTenantID(source),
		LensID:       lens.ID,
		Kind:         horizonmodels.MaterializedKindSupplyChainPath,
		CanonicalKey: canonicalKey,
		Payload:      payload,
		PathStatus:   "hypothesis",
		DisplayName:  title,
		Status:       "active",
		Labels: map[string]string{
			"productAxis":      productPath,
			"candidateChain":   "1",
			"candidateCountry": countryCode,
			"segmentCount":     fmt.Sprintf("%d", len(parsed.Legs)),
		},
		NodeRefs:         collectParsedNodeRefsLocal(parsedForSummary),
		GeoRefs:          append([]string(nil), summary.GeoRefs...),
		FlowRefs:         append([]string(nil), summary.FlowRefs...),
		ZoneRefs:         append([]string(nil), summary.ZoneRefs...),
		AffectedZoneRefs: append([]string(nil), summary.AffectedZoneRefs...),
		AnchorSupplier:   parsed.Family.AnchorSupplier,
		AnchorCustomer:   parsed.Family.AnchorCustomer,
		ProductAxis:      productPath,
		CreatedBy:        "candidate_chain_completion",
		UpdatedBy:        "candidate_chain_completion",
	}
	horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
	return artifact, strings.TrimPrefix(canonicalKey, "/"), title, nil
}

func scopeArtifactRefFromArtifact(artifact *horizonmodels.MaterializedArtifact) horizonmodels.ScopeArtifactRef {
	ref := horizonmodels.ScopeArtifactRef{
		Kind:               string(artifact.Kind),
		DisplayName:        artifact.DisplayName,
		AnchorSupplier:     artifact.AnchorSupplier,
		AnchorCustomer:     artifact.AnchorCustomer,
		ProductAxis:        artifact.ProductAxis,
		NodeRefs:           append([]string(nil), artifact.NodeRefs...),
		Status:             firstNonEmptyTrimmedLocal(artifact.PathStatus, artifact.Status),
		GeoRefs:            append([]string(nil), artifact.GeoRefs...),
		FlowRefs:           append([]string(nil), artifact.FlowRefs...),
		ZoneRefs:           append([]string(nil), artifact.ZoneRefs...),
		AffectedZoneRefs:   append([]string(nil), artifact.AffectedZoneRefs...),
		LegCount:           artifact.LegCount,
		BindingConfidence:  artifact.BindingScore,
		PathStressScore:    artifact.PathStressScore,
		BottleneckLegIndex: artifact.BottleneckLeg,
		RecoveryLatencyHrs: artifact.RecoveryLatencyHrs,
	}
	return ref
}

func candidateArtifactRiskScore(artifact *horizonmodels.MaterializedArtifact) float64 {
	if artifact == nil {
		return 0
	}
	stress := clampLocal(artifact.PathStressScore, 0, 1)
	bindingDeficit := 1 - clampLocal(artifact.BindingScore, 0, 1)
	recovery := clampLocal(artifact.RecoveryLatencyHrs/96.0, 0, 1)
	return round3Local(stress*1.4 + bindingDeficit*0.7 + recovery*0.5)
}

func artifactCountries(artifact *horizonmodels.MaterializedArtifact, primaryGeo string) []string {
	if artifact == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	for _, ref := range compactStringsLocal(artifact.NodeRefs) {
		code := countryCodeLocal(ref)
		if code == "" || code == primaryGeo {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

func detectPrimaryGeo(lens *horizonmodels.Lens) string {
	if lens == nil {
		return ""
	}
	for _, path := range compactStringsLocal(lens.Scope) {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) >= 2 && parts[0] == "loc" {
			return strings.ToLower(strings.TrimSpace(parts[1]))
		}
	}
	return ""
}

func buildCandidateChainCanonicalKey(lensID, countryCode, productPath string) string {
	return "/artifact/supply-chain/candidate/lens/" + strings.TrimSpace(lensID) +
		"/shoulder/" + strings.ToLower(strings.TrimSpace(countryCode)) +
		"/product/" + canonical.NormalizeCanonicalPathForPersist(strings.TrimSpace(productPath))
}

func candidateFamilyID(lensID, countryCode, productPath string) string {
	return strings.TrimSpace(lensID) + "::" + strings.ToLower(strings.TrimSpace(countryCode)) + "::" + strings.TrimSpace(productPath)
}

func candidateTenantID(items []*candidateChainArtifact) string {
	for _, item := range items {
		if item != nil && item.artifact != nil {
			tenantID := strings.TrimSpace(item.artifact.TenantID)
			if tenantID != "" {
				return tenantID
			}
		}
	}
	return "system"
}

func (s *CandidateChainCompletionService) pruneManagedCandidates(ctx context.Context, lens *horizonmodels.Lens, artifacts []*horizonmodels.MaterializedArtifact, primaryGeo string) (int, error) {
	if lens == nil {
		return 0, nil
	}
	byID := make(map[string]*horizonmodels.MaterializedArtifact, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		byID[strings.TrimSpace(artifact.ID)] = artifact
	}
	deleted := 0
	for _, artifact := range artifacts {
		if !managedCandidateArtifact(artifact) {
			continue
		}
		remove, err := shouldPruneManagedCandidate(artifact, byID, primaryGeo)
		if err != nil {
			return deleted, err
		}
		if !remove {
			continue
		}
		if err := s.materializedRepo.Delete(ctx, strings.TrimSpace(artifact.ID)); err != nil {
			return deleted, err
		}
		if err := s.lensesRepo.RemoveScopeArtifact(ctx, lens.ID, strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/")); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func managedCandidateArtifact(artifact *horizonmodels.MaterializedArtifact) bool {
	if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
		return false
	}
	if strings.TrimSpace(artifact.PathStatus) != "hypothesis" {
		return false
	}
	if strings.TrimSpace(artifact.Labels["candidateChain"]) == "1" {
		return true
	}
	rootNotes, _ := artifact.Payload["notes"].(map[string]any)
	if rootNotes == nil {
		rootNotes, _ = artifact.Payload["Notes"].(map[string]any)
	}
	return strings.TrimSpace(toStringLocal(rootNotes["buildMode"])) == "candidate_chain_completion"
}

func shouldPruneManagedCandidate(candidate *horizonmodels.MaterializedArtifact, byID map[string]*horizonmodels.MaterializedArtifact, primaryGeo string) (bool, error) {
	sourceIDs, _ := candidateSourceRefs(candidate.Payload)
	if len(sourceIDs) < 2 {
		return true, nil
	}
	validatedCount := 0
	unionCountries := map[string]struct{}{}
	maxSingleCountries := 0
	for _, sourceID := range sourceIDs {
		source := byID[strings.TrimSpace(sourceID)]
		if source == nil {
			return true, nil
		}
		if !candidateEligibleSourceArtifact(source) {
			return true, nil
		}
		validatedCount++
		countries := artifactCountries(source, primaryGeo)
		if len(countries) > maxSingleCountries {
			maxSingleCountries = len(countries)
		}
		for _, code := range countries {
			unionCountries[code] = struct{}{}
		}
	}
	if validatedCount < 2 {
		return true, nil
	}
	return len(unionCountries) <= maxSingleCountries, nil
}

func candidateEligibleSourceArtifact(artifact *horizonmodels.MaterializedArtifact) bool {
	if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
		return false
	}
	if managedCandidateArtifact(artifact) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(artifact.PathStatus), "validated") || artifact.IsConfirmedPath {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(artifact.SufficiencyStatus))
	if status == "" {
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		status = strings.ToLower(strings.TrimSpace(artifact.SufficiencyStatus))
	}
	return status == "validated"
}

func candidateSourceRefs(payload map[string]any) ([]string, []string) {
	rootNotes, _ := payload["notes"].(map[string]any)
	if rootNotes == nil {
		rootNotes, _ = payload["Notes"].(map[string]any)
	}
	return toStringSliceLocal(rootNotes["candidateSourceArtifactIds"]), toStringSliceLocal(rootNotes["candidateSourceArtifactKeys"])
}

func toStringSliceLocal(value any) []string {
	switch raw := value.(type) {
	case []string:
		return compactStringsLocal(raw)
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return compactStringsLocal(out)
	default:
		return nil
	}
}

func toStringLocal(value any) string {
	return strings.TrimSpace(fmt.Sprint(value))
}

func collectParsedNodeRefsLocal(parsed *horizonmodels.SupplyChainPathPayload) []string {
	if parsed == nil {
		return nil
	}
	out := make([]string, 0, len(parsed.Nodes))
	for _, node := range parsed.Nodes {
		ref := strings.TrimSpace(node.Ref)
		if ref != "" {
			out = append(out, ref)
		}
	}
	return compactStringsLocal(out)
}

func firstNonEmptyTrimmedLocal(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func compactStringsLocal(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
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

func sortedStringKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func cloneMapLocal(input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func countryCodeLocal(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "loc", "fac", "org", "market", "gov":
		return strings.ToLower(strings.TrimSpace(parts[1]))
	default:
		return ""
	}
}

func countryNameLocal(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "ae":
		return "UAE"
	case "cn":
		return "China"
	case "in":
		return "India"
	case "kz":
		return "Kazakhstan"
	case "tr":
		return "Turkey"
	case "ru":
		return "Russia"
	default:
		return strings.ToUpper(strings.TrimSpace(code))
	}
}

func lastPathSegmentLocal(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func titleWordsLocal(value string) string {
	parts := strings.Fields(strings.ReplaceAll(strings.TrimSpace(value), "-", " "))
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		runes := []rune(strings.ToLower(parts[i]))
		runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func nonZeroOr(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}

func clampLocal(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func round3Local(value float64) float64 {
	return float64(int(value*1000+0.5)) / 1000
}

func candidateMinFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func candidateMaxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func candidateMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
