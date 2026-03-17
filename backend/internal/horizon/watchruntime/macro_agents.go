package watchruntime

import (
	"context"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

const (
	maxDerivedPathOverlaySubjects = 6
	maxMacroUniverseNeighbors     = 24
)

func (s *Service) EnsureSubject(ctx context.Context, subject *horizonmodels.WatchSubject) (*horizonmodels.WatchSubject, error) {
	if s == nil || s.subjectsRepo == nil || subject == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	if err := s.subjectsRepo.Upsert(ctx, subject); err != nil {
		return nil, err
	}
	if err := s.ensureDerivedScopeAgentConfig(ctx, subject); err != nil {
		return nil, err
	}
	return subject, nil
}

func (s *Service) CompileMacroAgentPolicy(ctx context.Context, policy *horizonmodels.MacroAgentPolicy) ([]*horizonmodels.WatchSubject, error) {
	if s == nil || policy == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	switch strings.TrimSpace(policy.AgentClass) {
	case string(horizonmodels.MacroAgentClassLensUniverse):
		if err := s.SyncWatchSubjectsForLens(ctx, policy.LensID); err != nil {
			return nil, err
		}
		subjects, err := s.ListSubjects(ctx, policy.TenantID, policy.LensID, "", 100)
		if err != nil {
			return nil, err
		}
		return subjects, nil
	case string(horizonmodels.MacroAgentClassPathOverlay):
		subject, err := s.compilePathOverlayPolicy(ctx, policy)
		if err != nil || subject == nil {
			return nil, err
		}
		return []*horizonmodels.WatchSubject{subject}, nil
	default:
		subject, err := s.compileAnchorMacroPolicy(ctx, policy)
		if err != nil || subject == nil {
			return nil, err
		}
		return []*horizonmodels.WatchSubject{subject}, nil
	}
}

func (s *Service) compileAnchorMacroPolicy(ctx context.Context, policy *horizonmodels.MacroAgentPolicy) (*horizonmodels.WatchSubject, error) {
	anchor := strings.TrimSpace(policy.AnchorCanonicalPath)
	if anchor == "" {
		return nil, nil
	}
	displayLabel := labelFromCanonicalPath(anchor)
	fragmentID := ""
	universe := s.buildMacroUniverse(ctx, strings.TrimSpace(policy.TenantID), anchor, "")
	if s.fragmentsRepo != nil {
		if fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, anchor); err == nil && fragment != nil {
			fragmentID = strings.TrimSpace(fragment.ID)
			displayLabel = strings.TrimSpace(firstNonEmpty(fragmentDisplayName(fragment), displayLabel))
		}
	}
	subject := &horizonmodels.WatchSubject{
		TenantID:            strings.TrimSpace(firstNonEmpty(policy.TenantID, "global")),
		ProjectID:           strings.TrimSpace(policy.ProjectID),
		LensID:              strings.TrimSpace(policy.LensID),
		SubjectKind:         subjectKindForMacroClass(policy.AgentClass),
		AnchorCanonicalPath: anchor,
		AnchorFragmentID:    fragmentID,
		DisplayLabel:        displayLabel,
		Status:              mapMacroPolicyStatus(policy.Status),
		SourceMode:          horizonmodels.WatchSubjectSourceHybrid,
		RuntimeMode:         horizonmodels.WatchSubjectRuntimeGeneralized,
		DomainModes:         compactUnique(policy.DomainModes),
		Roles:               normalizeRoles(policy.Roles),
		Priority:            normalizedPriority(policy.Priority),
		LensBindings:        compactUnique([]string{strings.TrimSpace(policy.LensID)}),
		UniversePaths:       universe,
		Metadata: map[string]any{
			"macroAgentPolicyId": strings.TrimSpace(policy.ID),
			"macroAgentClass":    strings.TrimSpace(policy.AgentClass),
			"originType":         "canonical_scope",
			"sourceMixPolicy":    cloneMap(policy.SourceMixPolicy),
			"timeWindowPolicy":   cloneMap(policy.TimeWindowPolicy),
		},
	}
	return s.EnsureSubject(ctx, subject)
}

func (s *Service) compilePathOverlayPolicy(ctx context.Context, policy *horizonmodels.MacroAgentPolicy) (*horizonmodels.WatchSubject, error) {
	if s == nil || s.materializedArtifactsRepo == nil {
		return nil, nil
	}
	tenantID := strings.TrimSpace(firstNonEmpty(policy.TenantID, "global"))
	artifact, err := s.materializedArtifactsRepo.GetByCanonicalKeyAnyKind(ctx, tenantID, strings.TrimSpace(policy.ArtifactCanonicalKey))
	if err != nil || artifact == nil {
		return nil, err
	}
	return s.ensurePathOverlaySubject(ctx, policy.LensID, artifact, nil, policy)
}

func (s *Service) syncDerivedPathOverlaySubjects(ctx context.Context, lens *horizonmodels.Lens, lensSubject *horizonmodels.WatchSubject, lensPolicy *horizonmodels.MacroAgentPolicy) error {
	if s == nil || lens == nil || lensSubject == nil {
		return nil
	}
	artifacts, err := s.collectDerivedPathOverlayArtifacts(ctx, lens)
	if err != nil {
		return err
	}
	activeAnchorPaths := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		subject, err := s.ensurePathOverlaySubject(ctx, lens.ID, artifact, lensSubject, nil)
		if err != nil {
			return err
		}
		if subject != nil {
			activeAnchorPaths[strings.TrimSpace(subject.AnchorCanonicalPath)] = struct{}{}
		}
	}
	if s.macroAgentPoliciesRepo != nil {
		policies, err := s.macroAgentPoliciesRepo.ListByLens(ctx, strings.TrimSpace(firstNonEmpty(lens.TenantID, "global")), strings.TrimSpace(lens.ID), 100)
		if err != nil {
			return err
		}
		for _, policy := range policies {
			if policy == nil || !strings.EqualFold(strings.TrimSpace(policy.Status), string(horizonmodels.MacroAgentStatusActive)) {
				continue
			}
			if strings.TrimSpace(policy.AgentClass) != string(horizonmodels.MacroAgentClassPathOverlay) {
				continue
			}
			subject, err := s.compilePathOverlayPolicy(ctx, policy)
			if err != nil {
				return err
			}
			if subject != nil {
				if lensPolicy != nil {
					mergeInheritedOverlayPolicies(subject, lensSubject, lensPolicy)
					if _, err := s.EnsureSubject(ctx, subject); err != nil {
						return err
					}
				}
				activeAnchorPaths[strings.TrimSpace(subject.AnchorCanonicalPath)] = struct{}{}
			}
		}
	}
	return s.pauseStalePathOverlaySubjects(ctx, lens, activeAnchorPaths)
}

func (s *Service) collectDerivedPathOverlayArtifacts(ctx context.Context, lens *horizonmodels.Lens) ([]*horizonmodels.MaterializedArtifact, error) {
	if lens == nil {
		return nil, nil
	}
	artifactsByKey := map[string]*horizonmodels.MaterializedArtifact{}
	addArtifact := func(artifact *horizonmodels.MaterializedArtifact) {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			return
		}
		if strings.TrimSpace(artifact.CanonicalKey) == "" {
			return
		}
		artifactsByKey[strings.TrimSpace(artifact.CanonicalKey)] = artifact
	}
	if s.materializedArtifactsRepo != nil {
		stored, err := s.materializedArtifactsRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 64)
		if err != nil {
			return nil, err
		}
		for _, artifact := range stored {
			addArtifact(artifact)
		}
	}
	if s.materializedArtifactsRepo != nil && len(lens.ScopeArtifacts) > 0 {
		keys := make([]string, 0, len(lens.ScopeArtifacts))
		for artKey, ref := range lens.ScopeArtifacts {
			if strings.TrimSpace(ref.Kind) != string(horizonmodels.MaterializedKindSupplyChainPath) {
				continue
			}
			keys = append(keys, normalizedArtifactCanonicalKey(artKey))
		}
		if fetched, err := s.materializedArtifactsRepo.BatchGetByCanonicalKeys(ctx, strings.TrimSpace(firstNonEmpty(lens.TenantID, "global")), horizonmodels.MaterializedKindSupplyChainPath, compactUnique(keys)); err == nil {
			for _, artifact := range fetched {
				addArtifact(artifact)
			}
		}
	}
	type scored struct {
		artifact *horizonmodels.MaterializedArtifact
		score    float64
	}
	scoredArtifacts := make([]scored, 0, len(artifactsByKey))
	for _, artifact := range artifactsByKey {
		scoredArtifacts = append(scoredArtifacts, scored{artifact: artifact, score: derivedPathOverlayScore(artifact)})
	}
	sort.SliceStable(scoredArtifacts, func(i, j int) bool {
		if scoredArtifacts[i].score == scoredArtifacts[j].score {
			return strings.TrimSpace(scoredArtifacts[i].artifact.CanonicalKey) < strings.TrimSpace(scoredArtifacts[j].artifact.CanonicalKey)
		}
		return scoredArtifacts[i].score > scoredArtifacts[j].score
	})
	out := make([]*horizonmodels.MaterializedArtifact, 0, minInt(len(scoredArtifacts), maxDerivedPathOverlaySubjects))
	for _, item := range scoredArtifacts {
		out = append(out, item.artifact)
		if len(out) >= maxDerivedPathOverlaySubjects {
			break
		}
	}
	return out, nil
}

func (s *Service) ensurePathOverlaySubject(ctx context.Context, lensID string, artifact *horizonmodels.MaterializedArtifact, lensSubject *horizonmodels.WatchSubject, policy *horizonmodels.MacroAgentPolicy) (*horizonmodels.WatchSubject, error) {
	if s == nil || artifact == nil {
		return nil, nil
	}
	tenantID := strings.TrimSpace(firstNonEmpty(artifact.TenantID, "global"))
	anchorPath := "/artifact/" + strings.Trim(strings.TrimSpace(artifact.CanonicalKey), "/")
	lensBindings := []string{}
	if strings.TrimSpace(lensID) != "" {
		lensBindings = append(lensBindings, strings.TrimSpace(lensID))
	}
	universe := buildPathOverlayUniverse(artifact)
	subject := &horizonmodels.WatchSubject{
		TenantID:            tenantID,
		ProjectID:           strings.TrimSpace(artifact.ProjectID),
		LensID:              strings.TrimSpace(lensID),
		SubjectKind:         horizonmodels.WatchSubjectKindPathOverlay,
		AnchorCanonicalPath: anchorPath,
		DisplayLabel:        strings.TrimSpace(firstNonEmpty(artifact.DisplayName, labelFromCanonicalPath(anchorPath))),
		Status:              horizonmodels.WatchSubjectStatusActive,
		SourceMode:          horizonmodels.WatchSubjectSourceHybrid,
		RuntimeMode:         horizonmodels.WatchSubjectRuntimeGeneralized,
		DomainModes:         inferredDomainModesForPathOverlay(lensSubject, policy),
		Roles:               inferredRolesForPathOverlay(lensSubject, policy),
		Priority:            inferredPriorityForPathOverlay(artifact, lensSubject, policy),
		LensBindings:        compactUnique(lensBindings),
		UniversePaths:       universe,
		Metadata: map[string]any{
			"macroAgentClass":        string(horizonmodels.MacroAgentClassPathOverlay),
			"originType":             "materialized_artifact",
			"lensId":                 strings.TrimSpace(lensID),
			"artifactCanonicalKey":   strings.TrimSpace(artifact.CanonicalKey),
			"materializedArtifactId": strings.TrimSpace(artifact.ID),
			"materializedKind":       string(artifact.Kind),
			"backingCanonicalPaths":  compactUnique(append([]string{}, artifact.NodeRefs...)),
			"legIndices":             topLegIndicesForArtifact(artifact),
			"promotionPolicy":        map[string]any{"allowCanonicalSeed": true, "allowOverlayOnly": true},
		},
	}
	if policy != nil {
		subject.Metadata["macroAgentPolicyId"] = strings.TrimSpace(policy.ID)
		subject.Metadata["sourceMixPolicy"] = cloneMap(policy.SourceMixPolicy)
		subject.Metadata["timeWindowPolicy"] = cloneMap(policy.TimeWindowPolicy)
	} else {
		mergeInheritedOverlayPolicies(subject, lensSubject, nil)
	}
	return s.EnsureSubject(ctx, subject)
}

func (s *Service) pauseStalePathOverlaySubjects(ctx context.Context, lens *horizonmodels.Lens, activeAnchorPaths map[string]struct{}) error {
	if s == nil || s.subjectsRepo == nil || lens == nil {
		return nil
	}
	subjects, err := s.subjectsRepo.ListByLens(ctx, strings.TrimSpace(firstNonEmpty(lens.TenantID, "global")), strings.TrimSpace(lens.ID), 200)
	if err != nil {
		return err
	}
	for _, subject := range subjects {
		if subject == nil || subject.SubjectKind != horizonmodels.WatchSubjectKindPathOverlay {
			continue
		}
		if strings.TrimSpace(asString(subject.Metadata["macroAgentPolicyId"])) != "" {
			continue
		}
		if _, ok := activeAnchorPaths[strings.TrimSpace(subject.AnchorCanonicalPath)]; ok {
			continue
		}
		if subject.Status == horizonmodels.WatchSubjectStatusPaused {
			continue
		}
		subject.Status = horizonmodels.WatchSubjectStatusPaused
		if subject.Metadata == nil {
			subject.Metadata = map[string]any{}
		}
		subject.Metadata["staleDerivedPathOverlay"] = true
		if err := s.subjectsRepo.Upsert(ctx, subject); err != nil {
			return err
		}
	}
	return nil
}

func buildPathOverlayUniverse(artifact *horizonmodels.MaterializedArtifact) []string {
	if artifact == nil {
		return nil
	}
	paths := []string{"/artifact/" + strings.Trim(strings.TrimSpace(artifact.CanonicalKey), "/")}
	paths = append(paths, artifact.NodeRefs...)
	paths = append(paths, artifact.GeoRefs...)
	paths = append(paths, artifact.FlowRefs...)
	paths = append(paths, artifact.ZoneRefs...)
	paths = append(paths, artifact.AffectedZoneRefs...)
	if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
		for _, node := range parsed.Nodes {
			paths = append(paths, strings.TrimSpace(node.Ref))
		}
	}
	if legs, err := horizonmodels.GetSupplyChainPathLegs(artifact); err == nil {
		for idx, leg := range legs {
			if idx >= 3 {
				break
			}
			paths = append(paths, leg.SupplierRef, leg.CustomerRef, leg.ProductRef)
			paths = append(paths, leg.GeoRefs...)
			paths = append(paths, leg.TransportViaRefs...)
			paths = append(paths, leg.ZoneRefs...)
			paths = append(paths, leg.AffectedZoneRefs...)
		}
	}
	return compactUnique(paths)
}

func derivedPathOverlayScore(artifact *horizonmodels.MaterializedArtifact) float64 {
	if artifact == nil {
		return 0
	}
	score := artifact.PathStressScore*100 + artifact.BindingScore*50 + artifact.SufficiencyScore*20
	score += float64(artifact.GapCount*7 + artifact.UnresolvedJoinCount*9 + len(artifact.AffectedZoneRefs)*5)
	if artifact.BottleneckLeg >= 0 {
		score += 10
	}
	return score
}

func inferredDomainModesForPathOverlay(lensSubject *horizonmodels.WatchSubject, policy *horizonmodels.MacroAgentPolicy) []string {
	if policy != nil && len(policy.DomainModes) > 0 {
		return compactUnique(policy.DomainModes)
	}
	if lensSubject != nil && len(lensSubject.DomainModes) > 0 {
		return compactUnique(append([]string{"supply_chain"}, lensSubject.DomainModes...))
	}
	return []string{"supply_chain"}
}

func inferredRolesForPathOverlay(lensSubject *horizonmodels.WatchSubject, policy *horizonmodels.MacroAgentPolicy) []string {
	if policy != nil && len(policy.Roles) > 0 {
		return normalizeRoles(policy.Roles)
	}
	if lensSubject != nil && len(lensSubject.Roles) > 0 {
		return normalizeRoles(lensSubject.Roles)
	}
	return []string{"monitor", "risk", "scout", "forecast"}
}

func inferredPriorityForPathOverlay(artifact *horizonmodels.MaterializedArtifact, lensSubject *horizonmodels.WatchSubject, policy *horizonmodels.MacroAgentPolicy) int {
	if policy != nil && policy.Priority > 0 {
		return normalizedPriority(policy.Priority)
	}
	base := 70
	if lensSubject != nil && lensSubject.Priority > 0 {
		base = lensSubject.Priority
	}
	if artifact != nil {
		base += int(artifact.PathStressScore * 15)
		base += artifact.GapCount * 2
	}
	return normalizedPriority(base)
}

func topLegIndicesForArtifact(artifact *horizonmodels.MaterializedArtifact) []int {
	if artifact == nil {
		return nil
	}
	legs, err := horizonmodels.GetSupplyChainPathLegs(artifact)
	if err != nil || len(legs) == 0 {
		if artifact.BottleneckLeg >= 0 {
			return []int{artifact.BottleneckLeg}
		}
		return nil
	}
	type legScore struct {
		index int
		score float64
	}
	scores := make([]legScore, 0, len(legs))
	for idx, leg := range legs {
		score := leg.BindingConfidence*10 + float64(len(leg.AffectedZoneRefs)*5+len(leg.ZoneRefs)*3)
		if leg.Stress != nil {
			score += leg.Stress.StressScore * 20
		}
		scores = append(scores, legScore{index: idx, score: score})
	}
	sort.SliceStable(scores, func(i, j int) bool { return scores[i].score > scores[j].score })
	out := make([]int, 0, minInt(3, len(scores)))
	for _, score := range scores {
		out = append(out, score.index)
		if len(out) >= 3 {
			break
		}
	}
	return out
}

func (s *Service) buildMacroUniverse(ctx context.Context, tenantID, anchorCanonicalPath, fragmentID string) []string {
	pathSet := map[string]struct{}{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		pathSet[path] = struct{}{}
	}
	add(anchorCanonicalPath)
	var fragment *horizonmodels.HorizonFragment
	if s.fragmentsRepo != nil {
		if strings.TrimSpace(fragmentID) != "" {
			fragment, _ = s.fragmentsRepo.Get(ctx, strings.TrimSpace(fragmentID))
		}
		if fragment == nil && strings.TrimSpace(anchorCanonicalPath) != "" {
			fragment, _ = s.fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(anchorCanonicalPath))
		}
	}
	if fragment != nil {
		add(fragment.ParentPath)
		add(fragment.ParentGovPath)
		add(fragment.ParentMarketPath)
		add(fragment.AffiliatedOrgPath)
		add(fragment.EntityPath)
		add(fragment.SectorPath)
		for _, path := range fragment.SpherePaths {
			add(path)
		}
		for _, path := range fragment.IndustryPaths {
			add(path)
		}
		for _, path := range fragment.MarketSegments {
			add(path)
		}
		if fragment.SupplierFacilityRef != nil {
			add(strings.TrimSpace(*fragment.SupplierFacilityRef))
		}
		if fragment.CustomerFacilityRef != nil {
			add(strings.TrimSpace(*fragment.CustomerFacilityRef))
		}
	}
	if s.edgesRepo != nil && fragment != nil {
		if edges, err := s.edgesRepo.ListBySourceWithOptions(ctx, tenantID, fragment.ID, nil, maxMacroUniverseNeighbors, nil); err == nil {
			for _, edge := range edges {
				if edge == nil {
					continue
				}
				if neighbor, nerr := s.fragmentsRepo.Get(ctx, edge.TargetFragmentID); nerr == nil && neighbor != nil {
					add(neighbor.CanonicalPath)
				}
			}
		}
		if edges, err := s.edgesRepo.ListByTargetWithOptions(ctx, tenantID, fragment.ID, nil, maxMacroUniverseNeighbors, nil); err == nil {
			for _, edge := range edges {
				if edge == nil {
					continue
				}
				if neighbor, nerr := s.fragmentsRepo.Get(ctx, edge.SourceFragmentID); nerr == nil && neighbor != nil {
					add(neighbor.CanonicalPath)
				}
			}
		}
	}
	if s.materializedArtifactsRepo != nil && strings.TrimSpace(anchorCanonicalPath) != "" {
		if artifacts, err := s.materializedArtifactsRepo.FindByNodeRef(ctx, tenantID, strings.TrimSpace(anchorCanonicalPath), horizonmodels.MaterializedKindSupplyChainPath); err == nil {
			for _, artifact := range artifacts {
				for _, path := range buildPathOverlayUniverse(artifact) {
					add(path)
				}
			}
		}
	}
	return sortedKeys(pathSet)
}

func normalizedArtifactCanonicalKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/artifact/") {
		return "/" + strings.Trim(strings.TrimPrefix(raw, "/artifact/"), "/")
	}
	if strings.HasPrefix(raw, "/") {
		return raw
	}
	return "/" + strings.Trim(raw, "/")
}

func mapMacroPolicyStatus(status string) horizonmodels.WatchSubjectStatus {
	switch strings.TrimSpace(status) {
	case string(horizonmodels.MacroAgentStatusPaused):
		return horizonmodels.WatchSubjectStatusPaused
	default:
		return horizonmodels.WatchSubjectStatusActive
	}
}

func subjectKindForMacroClass(class string) horizonmodels.WatchSubjectKind {
	switch strings.TrimSpace(class) {
	case string(horizonmodels.MacroAgentClassCountry):
		return horizonmodels.WatchSubjectKindGeography
	case string(horizonmodels.MacroAgentClassMarket):
		return horizonmodels.WatchSubjectKindMarket
	case string(horizonmodels.MacroAgentClassPathOverlay):
		return horizonmodels.WatchSubjectKindPathOverlay
	default:
		return horizonmodels.WatchSubjectKindExplicitTarget
	}
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Service) activeLensUniversePolicy(ctx context.Context, tenantID, lensID string) *horizonmodels.MacroAgentPolicy {
	if s == nil || s.macroAgentPoliciesRepo == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(lensID) == "" {
		return nil
	}
	policies, err := s.macroAgentPoliciesRepo.ListByLens(ctx, tenantID, lensID, 100)
	if err != nil {
		return nil
	}
	var selected *horizonmodels.MacroAgentPolicy
	for _, policy := range policies {
		if policy == nil || !strings.EqualFold(strings.TrimSpace(policy.Status), string(horizonmodels.MacroAgentStatusActive)) {
			continue
		}
		if strings.TrimSpace(policy.AgentClass) != string(horizonmodels.MacroAgentClassLensUniverse) {
			continue
		}
		if selected == nil || policy.Priority > selected.Priority || policy.UpdatedAt.After(selected.UpdatedAt) {
			selected = policy
		}
	}
	return selected
}

func applyLensUniversePolicyToSubject(subject *horizonmodels.WatchSubject, policy *horizonmodels.MacroAgentPolicy) {
	if subject == nil || policy == nil {
		return
	}
	if len(policy.DomainModes) > 0 {
		subject.DomainModes = compactUnique(policy.DomainModes)
	}
	if len(policy.Roles) > 0 {
		subject.Roles = normalizeRoles(policy.Roles)
	}
	if policy.Priority > 0 {
		subject.Priority = normalizedPriority(policy.Priority)
	}
	if subject.Metadata == nil {
		subject.Metadata = map[string]any{}
	}
	subject.Metadata["macroAgentPolicyId"] = strings.TrimSpace(policy.ID)
	subject.Metadata["macroAgentClass"] = string(horizonmodels.MacroAgentClassLensUniverse)
	subject.Metadata["sourceMixPolicy"] = cloneMap(policy.SourceMixPolicy)
	subject.Metadata["timeWindowPolicy"] = cloneMap(policy.TimeWindowPolicy)
}

func mergeInheritedOverlayPolicies(subject *horizonmodels.WatchSubject, lensSubject *horizonmodels.WatchSubject, policy *horizonmodels.MacroAgentPolicy) {
	if subject == nil {
		return
	}
	if subject.Metadata == nil {
		subject.Metadata = map[string]any{}
	}
	if policy != nil {
		if _, ok := subject.Metadata["sourceMixPolicy"]; !ok {
			subject.Metadata["sourceMixPolicy"] = cloneMap(policy.SourceMixPolicy)
		}
		if _, ok := subject.Metadata["timeWindowPolicy"]; !ok {
			subject.Metadata["timeWindowPolicy"] = cloneMap(policy.TimeWindowPolicy)
		}
	}
	if lensSubject != nil {
		if _, ok := subject.Metadata["sourceMixPolicy"]; !ok {
			if inherited := mapFromAny(lensSubject.Metadata["sourceMixPolicy"]); len(inherited) > 0 {
				subject.Metadata["sourceMixPolicy"] = cloneMap(inherited)
			}
		}
		if _, ok := subject.Metadata["timeWindowPolicy"]; !ok {
			if inherited := mapFromAny(lensSubject.Metadata["timeWindowPolicy"]); len(inherited) > 0 {
				subject.Metadata["timeWindowPolicy"] = cloneMap(inherited)
			}
		}
	}
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
