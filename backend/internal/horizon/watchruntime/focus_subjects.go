package watchruntime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/focusobject"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

const (
	focusObjectMetadataKey          = "focusObject"
	focusObjectSummaryMetadataKey   = "focusSummary"
	focusObjectArtifactsMetadataKey = "artifactCanonicalKeys"
	executionCoverageMetadataKey    = "executionCoverage"
	focusRuntimeEdgeFanout          = 48
	focusRuntimeMaxFragments        = 96
	focusRuntimeMaxFactsPerPath     = 12
	focusRuntimeMaxArtifacts        = 96
)

type focusNeighborhoodItem struct {
	fragmentID string
	depth      int
}

type executionCoverageOwner struct {
	SubjectID                   string   `json:"subjectId"`
	SubjectKind                 string   `json:"subjectKind"`
	DisplayLabel                string   `json:"displayLabel,omitempty"`
	CoveredUniversePaths        []string `json:"coveredUniversePaths,omitempty"`
	SharedArtifactCanonicalKeys []string `json:"sharedArtifactCanonicalKeys,omitempty"`
	OverlapScore                float64  `json:"overlapScore"`
	Reason                      string   `json:"reason,omitempty"`
}

type executionOverlapPlan struct {
	PrimaryOwnerSubjectID       string                   `json:"primaryOwnerSubjectId,omitempty"`
	OwnerSubjectIDs             []string                 `json:"ownerSubjectIds,omitempty"`
	SharedUniversePaths         []string                 `json:"sharedUniversePaths,omitempty"`
	ResidualUniversePaths       []string                 `json:"residualUniversePaths,omitempty"`
	SharedArtifactCanonicalKeys []string                 `json:"sharedArtifactCanonicalKeys,omitempty"`
	Owners                      []executionCoverageOwner `json:"owners,omitempty"`
}

func isFocusTarget(target *horizonmodels.WatchTarget) bool {
	if target == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(target.Kind), string(horizonmodels.WatchSubjectKindFocusObject)) {
		return true
	}
	if len(parseFocusMetadata(target.Metadata)) > 0 {
		return true
	}
	return parseBool(target.Metadata["focusObject"])
}

func isFocusSubject(subject *horizonmodels.WatchSubject) bool {
	if subject == nil {
		return false
	}
	if subject.SubjectKind == horizonmodels.WatchSubjectKindFocusObject {
		return true
	}
	return len(parseFocusMetadata(subject.Metadata)) > 0
}

func subjectIDOrEmpty(subject *horizonmodels.WatchSubject) string {
	if subject == nil {
		return ""
	}
	return strings.TrimSpace(subject.ID)
}

func existingUniversePaths(subject *horizonmodels.WatchSubject) []string {
	if subject == nil {
		return nil
	}
	return compactUnique(append([]string(nil), subject.UniversePaths...))
}

func (s *Service) findOverlappingTargetSubject(ctx context.Context, target *horizonmodels.WatchTarget) *horizonmodels.WatchSubject {
	if s == nil || s.subjectsRepo == nil || target == nil || strings.TrimSpace(target.CanonicalPath) == "" {
		return nil
	}
	subjects, err := s.subjectsRepo.ListByAnchorCanonicalPath(ctx, strings.TrimSpace(firstNonEmpty(target.TenantID, "global")), strings.TrimSpace(target.CanonicalPath), 24)
	if err != nil || len(subjects) == 0 {
		return nil
	}
	var best *horizonmodels.WatchSubject
	bestRank := -1
	for _, subject := range subjects {
		if !sameTargetScope(subject, target) {
			continue
		}
		rank := overlappingSubjectRank(subject)
		if rank > bestRank {
			best = subject
			bestRank = rank
		}
	}
	return best
}

func sameTargetScope(subject *horizonmodels.WatchSubject, target *horizonmodels.WatchTarget) bool {
	if subject == nil || target == nil {
		return false
	}
	if strings.TrimSpace(subject.TenantID) != strings.TrimSpace(firstNonEmpty(target.TenantID, "global")) {
		return false
	}
	if strings.TrimSpace(subject.PackID) != strings.TrimSpace(target.PackID) {
		return false
	}
	if strings.TrimSpace(subject.UserID) != strings.TrimSpace(target.UserID) {
		return false
	}
	if strings.TrimSpace(subject.AnchorCanonicalPath) != strings.TrimSpace(target.CanonicalPath) {
		return false
	}
	return true
}

func overlappingSubjectRank(subject *horizonmodels.WatchSubject) int {
	if subject == nil {
		return 0
	}
	switch subject.SubjectKind {
	case horizonmodels.WatchSubjectKindFocusObject:
		return 3
	case horizonmodels.WatchSubjectKindFragment:
		return 2
	case horizonmodels.WatchSubjectKindExplicitTarget:
		return 1
	default:
		return 0
	}
}

func (s *Service) ensureFocusSubjectForTarget(
	ctx context.Context,
	target *horizonmodels.WatchTarget,
	existing *horizonmodels.WatchSubject,
) (*horizonmodels.WatchSubject, error) {
	if s == nil || s.subjectsRepo == nil || target == nil {
		return nil, fmt.Errorf("focus runtime is not configured")
	}
	focusInput := mergeFocusInputs(focusInputFromSubject(existing), focusInputFromTarget(target))
	focusInput.AnchorCanonicalPath = strings.TrimSpace(target.CanonicalPath)
	if strings.TrimSpace(focusInput.DisplayLabel) == "" {
		focusInput.DisplayLabel = strings.TrimSpace(firstNonEmpty(target.Label, subjectLabelOrEmpty(existing), target.CanonicalPath))
	}
	if strings.TrimSpace(focusInput.AnchorCanonicalPath) == "" {
		return nil, fmt.Errorf("focus anchor canonicalPath is required")
	}
	snapshot, err := s.buildFocusSnapshot(ctx, strings.TrimSpace(firstNonEmpty(target.TenantID, "global")), existing, focusInput)
	if err != nil {
		return nil, err
	}
	subjectMetadata := cloneMap(nil)
	if existing != nil {
		subjectMetadata = cloneMap(existing.Metadata)
	}
	if subjectMetadata == nil {
		subjectMetadata = map[string]any{}
	}
	subjectMetadata["kind"] = string(horizonmodels.WatchSubjectKindFocusObject)
	subjectMetadata["watchTargetId"] = strings.TrimSpace(target.ID)
	subjectMetadata["source"] = strings.TrimSpace(target.Source)
	subjectMetadata["interestTitle"] = strings.TrimSpace(target.InterestTitle)
	subjectMetadata["interestQuery"] = strings.TrimSpace(target.InterestQuery)
	subjectMetadata["queryOverrides"] = compactUnique(append([]string(nil), snapshot.Focus.QueryOverrides...))
	subjectMetadata[focusObjectMetadataKey] = focusMetadataFromInput(focusInput)
	subjectMetadata[focusObjectSummaryMetadataKey] = focusSummaryFromSnapshot(snapshot)
	subjectMetadata[focusObjectArtifactsMetadataKey] = append([]string(nil), snapshot.Focus.ArtifactCanonicalKeys...)
	subjectMetadata["backingCanonicalPaths"] = append([]string(nil), snapshot.Focus.BackingCanonicalPaths...)

	pageFraming := cloneMap(target.PageFraming)
	if len(pageFraming) == 0 && existing != nil {
		pageFraming = cloneMap(existing.PageFraming)
	}

	lensBindings := compactUnique(append(existingLensBindings(existing), s.collectLensBindings(ctx, target.TenantID, target.FragmentID, target.CanonicalPath)...))
	plan := s.resolveExecutionOverlapPlan(
		ctx,
		strings.TrimSpace(firstNonEmpty(target.TenantID, "global")),
		lensBindings,
		strings.TrimSpace(firstNonEmpty(target.PackID, subjectPackOrEmpty(existing))),
		strings.TrimSpace(firstNonEmpty(target.UserID, subjectUserOrEmpty(existing))),
		strings.TrimSpace(subjectIDOrEmpty(existing)),
		snapshot,
	)
	if len(plan.OwnerSubjectIDs) > 0 {
		subjectMetadata[executionCoverageMetadataKey] = executionCoverageToMetadata(plan)
	} else {
		delete(subjectMetadata, executionCoverageMetadataKey)
	}

	subject := &horizonmodels.WatchSubject{
		ID:                  strings.TrimSpace(firstNonEmpty(target.SubjectID, subjectIDOrEmpty(existing))),
		TenantID:            strings.TrimSpace(firstNonEmpty(target.TenantID, subjectTenantOrEmpty(existing), "global")),
		ProjectID:           strings.TrimSpace(subjectProjectOrEmpty(existing)),
		LensID:              strings.TrimSpace(subjectLensOrEmpty(existing)),
		PackID:              strings.TrimSpace(firstNonEmpty(target.PackID, subjectPackOrEmpty(existing))),
		UserID:              strings.TrimSpace(firstNonEmpty(target.UserID, subjectUserOrEmpty(existing))),
		SubjectKind:         horizonmodels.WatchSubjectKindFocusObject,
		AnchorCanonicalPath: strings.TrimSpace(snapshot.Focus.AnchorCanonicalPath),
		AnchorFragmentID:    strings.TrimSpace(firstNonEmpty(target.FragmentID, subjectFragmentIDOrEmpty(existing))),
		DisplayLabel:        strings.TrimSpace(firstNonEmpty(snapshot.Focus.DisplayLabel, target.Label, target.CanonicalPath)),
		InterestTitle:       strings.TrimSpace(target.InterestTitle),
		InterestQuery:       strings.TrimSpace(target.InterestQuery),
		QueryOverrides:      compactUnique(append([]string(nil), snapshot.Focus.QueryOverrides...)),
		PageFraming:         pageFraming,
		Status:              mapTargetStatus(target.Status),
		SourceMode:          horizonmodels.WatchSubjectSourceHybrid,
		RuntimeMode:         horizonmodels.WatchSubjectRuntimeFocusRuntime,
		DomainModes:         compactUnique(append(append([]string(nil), snapshot.Focus.DomainModes...), target.DomainModes...)),
		Roles:               normalizeRoles(append([]string{"monitor", "risk", "scout"}, target.Roles...)),
		Priority:            maxInt(normalizedPriority(target.Priority), subjectPriorityOrDefault(existing)),
		LensBindings:        lensBindings,
		UniversePaths:       compactUnique(append(append(existingUniversePaths(existing), snapshot.Focus.UniversePaths...), snapshot.Focus.BackingCanonicalPaths...)),
		Metadata:            subjectMetadata,
	}
	if err := s.subjectsRepo.Upsert(ctx, subject); err != nil {
		return nil, err
	}
	target.SubjectID = subject.ID
	if target.Metadata == nil {
		target.Metadata = map[string]any{}
	}
	target.Metadata[focusObjectMetadataKey] = focusMetadataFromInput(focusInput)
	if s.targetsRepo != nil {
		_ = s.targetsRepo.Upsert(ctx, target)
	}
	return subject, nil
}

func (s *Service) buildFocusSnapshotFromSubject(ctx context.Context, subject *horizonmodels.WatchSubject) (*focusobject.Snapshot, error) {
	if s == nil || subject == nil {
		return nil, fmt.Errorf("focus subject is required")
	}
	focusInput := focusInputFromSubject(subject)
	if strings.TrimSpace(focusInput.AnchorCanonicalPath) == "" {
		focusInput.AnchorCanonicalPath = strings.TrimSpace(subject.AnchorCanonicalPath)
	}
	if strings.TrimSpace(focusInput.DisplayLabel) == "" {
		focusInput.DisplayLabel = strings.TrimSpace(subject.DisplayLabel)
	}
	return s.buildFocusSnapshot(ctx, strings.TrimSpace(firstNonEmpty(subject.TenantID, "global")), subject, focusInput)
}

func (s *Service) buildFocusSnapshot(
	ctx context.Context,
	tenantID string,
	subject *horizonmodels.WatchSubject,
	focusInput focusobject.FocusObjectInput,
) (*focusobject.Snapshot, error) {
	focusInput.AnchorCanonicalPath = strings.TrimSpace(focusInput.AnchorCanonicalPath)
	if focusInput.AnchorCanonicalPath == "" {
		return nil, fmt.Errorf("focus anchor canonicalPath is required")
	}
	seedPaths := []string{focusInput.AnchorCanonicalPath}
	seedPaths = append(seedPaths, focusInput.RelatedCanonicalPaths...)
	if subject != nil {
		seedPaths = append(seedPaths, subject.UniversePaths...)
	}
	fragments, edges, discoveredPaths := s.collectFocusNeighborhood(ctx, tenantID, compactUnique(seedPaths), firstPositive(focusInput.UniverseDepth, 2))
	artifacts := s.collectFocusArtifacts(ctx, tenantID, compactUnique(append(discoveredPaths, focusInput.AnchorCanonicalPath)))
	facts := s.collectFocusFacts(ctx, tenantID, compactUnique(append(discoveredPaths, focusInput.AnchorCanonicalPath)), firstPositive(focusInput.LookbackHours, 24))
	signals := make([]*horizonmodels.SignalDescriptor, 0, len(facts))
	for _, fact := range facts {
		if fact == nil || strings.TrimSpace(fact.Kind) != "signal" {
			continue
		}
		if descriptor := interpretation.DescriptorFromSignalFact(fact); descriptor != nil {
			signals = append(signals, descriptor)
		}
	}
	if focusInput.DisplayLabel == "" && subject != nil {
		focusInput.DisplayLabel = strings.TrimSpace(subject.DisplayLabel)
	}
	snapshot, err := focusobject.NewAnalyzer().Analyze(focusobject.AnalyzeRequest{
		Focus:     focusInput,
		Fragments: fragments,
		Edges:     edges,
		Artifacts: artifacts,
		Facts:     facts,
		Signals:   signals,
		Now:       time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (s *Service) collectFocusNeighborhood(
	ctx context.Context,
	tenantID string,
	seedPaths []string,
	depth int,
) ([]*horizonmodels.HorizonFragment, []*horizonmodels.DependencyEdge, []string) {
	seedPaths = compactUnique(seedPaths)
	if len(seedPaths) == 0 {
		return nil, nil, nil
	}
	fragmentsByID := map[string]*horizonmodels.HorizonFragment{}
	fragmentsByPath := map[string]*horizonmodels.HorizonFragment{}
	for _, fragment := range s.loadFragmentsByPaths(ctx, seedPaths) {
		if fragment == nil {
			continue
		}
		fragmentsByID[strings.TrimSpace(fragment.ID)] = fragment
		fragmentsByPath[strings.TrimSpace(fragment.CanonicalPath)] = fragment
	}
	queue := make([]focusNeighborhoodItem, 0, len(fragmentsByID))
	visited := map[string]int{}
	enqueue := func(fragment *horizonmodels.HorizonFragment, currentDepth int) {
		if fragment == nil || strings.TrimSpace(fragment.ID) == "" {
			return
		}
		if len(fragmentsByID) >= focusRuntimeMaxFragments {
			return
		}
		id := strings.TrimSpace(fragment.ID)
		if prior, ok := visited[id]; ok && prior <= currentDepth {
			return
		}
		visited[id] = currentDepth
		fragmentsByID[id] = fragment
		fragmentsByPath[strings.TrimSpace(fragment.CanonicalPath)] = fragment
		queue = append(queue, focusNeighborhoodItem{fragmentID: id, depth: currentDepth})
	}
	for _, fragment := range fragmentsByID {
		enqueue(fragment, 0)
	}
	edgesByKey := map[string]*horizonmodels.DependencyEdge{}
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if item.depth >= depth || s.edgesRepo == nil {
			continue
		}
		for _, tid := range runtimeTenantCandidates(tenantID) {
			for _, edge := range s.loadIncidentEdges(ctx, tid, item.fragmentID) {
				if edge == nil {
					continue
				}
				edgeKey := dependencyEdgeKey(edge)
				if edgeKey == "" {
					continue
				}
				edgesByKey[edgeKey] = edge
				neighborID := strings.TrimSpace(edge.SourceFragmentID)
				if neighborID == item.fragmentID {
					neighborID = strings.TrimSpace(edge.TargetFragmentID)
				}
				if neighborID == "" {
					continue
				}
				if _, ok := fragmentsByID[neighborID]; !ok && s.fragmentsRepo != nil {
					if fragment, err := s.fragmentsRepo.Get(ctx, neighborID); err == nil && fragment != nil {
						enqueue(fragment, item.depth+1)
					}
					continue
				}
				enqueue(fragmentsByID[neighborID], item.depth+1)
			}
		}
	}
	fragments := make([]*horizonmodels.HorizonFragment, 0, len(fragmentsByID))
	paths := make([]string, 0, len(fragmentsByPath)+len(seedPaths))
	paths = append(paths, seedPaths...)
	for _, fragment := range fragmentsByID {
		fragments = append(fragments, fragment)
		paths = append(paths, strings.TrimSpace(fragment.CanonicalPath))
	}
	sort.SliceStable(fragments, func(i, j int) bool {
		return strings.TrimSpace(fragments[i].CanonicalPath) < strings.TrimSpace(fragments[j].CanonicalPath)
	})
	edges := make([]*horizonmodels.DependencyEdge, 0, len(edgesByKey))
	for _, edge := range edgesByKey {
		edges = append(edges, edge)
	}
	sort.SliceStable(edges, func(i, j int) bool {
		return dependencyEdgeKey(edges[i]) < dependencyEdgeKey(edges[j])
	})
	return fragments, edges, compactUnique(paths)
}

func (s *Service) loadFragmentsByPaths(ctx context.Context, paths []string) []*horizonmodels.HorizonFragment {
	if s == nil || s.fragmentsRepo == nil || len(paths) == 0 {
		return nil
	}
	items, err := s.fragmentsRepo.ListByCanonicalPaths(ctx, compactUnique(paths))
	if err != nil {
		return nil
	}
	out := make([]*horizonmodels.HorizonFragment, 0, len(items))
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.ID) == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (s *Service) loadIncidentEdges(ctx context.Context, tenantID, fragmentID string) []*horizonmodels.DependencyEdge {
	if s == nil || s.edgesRepo == nil || strings.TrimSpace(fragmentID) == "" {
		return nil
	}
	outgoing, _ := s.edgesRepo.ListBySource(ctx, tenantID, strings.TrimSpace(fragmentID), nil, focusRuntimeEdgeFanout)
	incoming, _ := s.edgesRepo.ListByTarget(ctx, tenantID, strings.TrimSpace(fragmentID), nil, focusRuntimeEdgeFanout)
	edges := make([]*horizonmodels.DependencyEdge, 0, len(outgoing)+len(incoming))
	edges = append(edges, outgoing...)
	edges = append(edges, incoming...)
	return edges
}

func (s *Service) collectFocusArtifacts(ctx context.Context, tenantID string, paths []string) []*horizonmodels.MaterializedArtifact {
	if s == nil || s.materializedArtifactsRepo == nil || len(paths) == 0 {
		return nil
	}
	artifactsByKey := map[string]*horizonmodels.MaterializedArtifact{}
	for _, tid := range runtimeTenantCandidates(tenantID) {
		items, err := s.materializedArtifactsRepo.FindByNodeRefsAny(ctx, tid, compactUnique(paths), horizonmodels.MaterializedKindSupplyChainPath, focusRuntimeMaxArtifacts)
		if err != nil {
			continue
		}
		for _, item := range items {
			if item == nil {
				continue
			}
			key := strings.TrimSpace(firstNonEmpty(item.ID, item.CanonicalKey))
			if key == "" {
				continue
			}
			artifactsByKey[key] = item
		}
	}
	out := make([]*horizonmodels.MaterializedArtifact, 0, len(artifactsByKey))
	for _, item := range artifactsByKey {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].CanonicalKey) < strings.TrimSpace(out[j].CanonicalKey)
	})
	return out
}

func (s *Service) collectFocusFacts(ctx context.Context, tenantID string, paths []string, lookbackHours int) []*coremodels.OntologyFact {
	if s == nil || s.factsRepo == nil || len(paths) == 0 {
		return nil
	}
	since := time.Now().UTC().Add(-time.Duration(firstPositive(lookbackHours, 24)) * time.Hour)
	factsByID := map[string]*coremodels.OntologyFact{}
	for _, path := range compactUnique(paths) {
		for _, tid := range runtimeTenantCandidates(tenantID) {
			items, err := s.factsRepo.ListByCanonicalPath(ctx, tid, path, "signal", focusRuntimeMaxFactsPerPath)
			if err != nil {
				continue
			}
			for _, item := range items {
				if item == nil || strings.TrimSpace(item.ID) == "" {
					continue
				}
				if factObservedAt(item).Before(since) {
					continue
				}
				factsByID[strings.TrimSpace(item.ID)] = item
			}
		}
	}
	out := make([]*coremodels.OntologyFact, 0, len(factsByID))
	for _, item := range factsByID {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return factObservedAt(out[i]).After(factObservedAt(out[j]))
	})
	return out
}

func focusInputFromTarget(target *horizonmodels.WatchTarget) focusobject.FocusObjectInput {
	if target == nil {
		return focusobject.FocusObjectInput{}
	}
	meta := parseFocusMetadata(target.Metadata)
	return focusobject.FocusObjectInput{
		AnchorCanonicalPath:   strings.TrimSpace(target.CanonicalPath),
		DisplayLabel:          strings.TrimSpace(target.Label),
		RelatedCanonicalPaths: compactUnique(append(parseAnyStringSlice(meta["relatedCanonicalPaths"]), parseAnyStringSlice(target.Metadata["relatedCanonicalPaths"])...)),
		DomainModes:           compactUnique(append([]string(nil), target.DomainModes...)),
		QueryOverrides:        compactUnique(append([]string(nil), target.QueryOverrides...)),
		UniverseDepth:         firstPositive(asInt(meta["universeDepth"]), asInt(target.Metadata["universeDepth"])),
		LookbackHours:         firstPositive(asInt(meta["lookbackHours"]), asInt(target.Metadata["lookbackHours"])),
		MaxImpactedEntities:   firstPositive(asInt(meta["maxImpactedEntities"]), asInt(target.Metadata["maxImpactedEntities"])),
		MaxPropagationHops:    firstPositive(asInt(meta["maxPropagationHops"]), asInt(target.Metadata["maxPropagationHops"])),
	}
}

func focusInputFromSubject(subject *horizonmodels.WatchSubject) focusobject.FocusObjectInput {
	if subject == nil {
		return focusobject.FocusObjectInput{}
	}
	meta := parseFocusMetadata(subject.Metadata)
	return focusobject.FocusObjectInput{
		AnchorCanonicalPath:   strings.TrimSpace(subject.AnchorCanonicalPath),
		DisplayLabel:          strings.TrimSpace(subject.DisplayLabel),
		RelatedCanonicalPaths: compactUnique(append(parseAnyStringSlice(meta["relatedCanonicalPaths"]), parseAnyStringSlice(subject.Metadata["backingCanonicalPaths"])...)),
		DomainModes:           compactUnique(append([]string(nil), subject.DomainModes...)),
		QueryOverrides:        compactUnique(append([]string(nil), subject.QueryOverrides...)),
		UniverseDepth:         firstPositive(asInt(meta["universeDepth"]), 2),
		LookbackHours:         firstPositive(asInt(meta["lookbackHours"]), 24),
		MaxImpactedEntities:   firstPositive(asInt(meta["maxImpactedEntities"]), 8),
		MaxPropagationHops:    firstPositive(asInt(meta["maxPropagationHops"]), 5),
	}
}

func mergeFocusInputs(base, override focusobject.FocusObjectInput) focusobject.FocusObjectInput {
	out := base
	if strings.TrimSpace(override.AnchorCanonicalPath) != "" {
		out.AnchorCanonicalPath = strings.TrimSpace(override.AnchorCanonicalPath)
	}
	if strings.TrimSpace(override.DisplayLabel) != "" {
		out.DisplayLabel = strings.TrimSpace(override.DisplayLabel)
	}
	out.RelatedCanonicalPaths = compactUnique(append(out.RelatedCanonicalPaths, override.RelatedCanonicalPaths...))
	out.DomainModes = compactUnique(append(out.DomainModes, override.DomainModes...))
	out.QueryOverrides = compactUnique(append(out.QueryOverrides, override.QueryOverrides...))
	out.UniverseDepth = firstPositive(override.UniverseDepth, out.UniverseDepth)
	out.LookbackHours = firstPositive(override.LookbackHours, out.LookbackHours)
	out.MaxImpactedEntities = firstPositive(override.MaxImpactedEntities, out.MaxImpactedEntities)
	out.MaxPropagationHops = firstPositive(override.MaxPropagationHops, out.MaxPropagationHops)
	return out
}

func parseFocusMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	if typed, ok := metadata[focusObjectMetadataKey].(map[string]any); ok {
		return typed
	}
	return nil
}

func focusMetadataFromInput(input focusobject.FocusObjectInput) map[string]any {
	return map[string]any{
		"anchorCanonicalPath":   strings.TrimSpace(input.AnchorCanonicalPath),
		"displayLabel":          strings.TrimSpace(input.DisplayLabel),
		"relatedCanonicalPaths": append([]string(nil), compactUnique(input.RelatedCanonicalPaths)...),
		"universeDepth":         firstPositive(input.UniverseDepth, 2),
		"lookbackHours":         firstPositive(input.LookbackHours, 24),
		"maxImpactedEntities":   firstPositive(input.MaxImpactedEntities, 8),
		"maxPropagationHops":    firstPositive(input.MaxPropagationHops, 5),
	}
}

func focusSummaryFromSnapshot(snapshot *focusobject.Snapshot) map[string]any {
	if snapshot == nil {
		return nil
	}
	return map[string]any{
		"riskPressure":        snapshot.Position.RiskPressure,
		"opportunityPressure": snapshot.Position.OpportunityPressure,
		"artifactExposure":    snapshot.Position.ArtifactExposure,
		"coverageScore":       snapshot.Position.CoverageScore,
		"monitoringReadiness": snapshot.Position.MonitoringReadiness,
		"endpointCount":       len(snapshot.EndpointHits),
		"clusterCount":        len(snapshot.AffinityClusters),
		"pathExposureCount":   len(snapshot.PathExposures),
		"todayMentions":       len(snapshot.TodayMentions),
	}
}

func (s *Service) resolveExecutionOverlapPlan(
	ctx context.Context,
	tenantID string,
	lensBindings []string,
	packID string,
	userID string,
	selfID string,
	snapshot *focusobject.Snapshot,
) executionOverlapPlan {
	if s == nil || s.subjectsRepo == nil || snapshot == nil {
		return executionOverlapPlan{}
	}
	universePaths := compactUnique(append([]string(nil), snapshot.Focus.UniversePaths...))
	if len(universePaths) == 0 {
		return executionOverlapPlan{}
	}
	candidatesByID := map[string]*horizonmodels.WatchSubject{}
	for _, path := range universePaths {
		subjects, err := s.subjectsRepo.ListByUniversePath(ctx, tenantID, path, 50)
		if err != nil {
			continue
		}
		for _, subject := range subjects {
			if subject == nil || strings.TrimSpace(subject.ID) == "" || strings.TrimSpace(subject.ID) == strings.TrimSpace(selfID) {
				continue
			}
			if !subjectEligibleForExecutionSharing(subject, tenantID, lensBindings, packID, userID) {
				continue
			}
			candidatesByID[strings.TrimSpace(subject.ID)] = subject
		}
	}
	if len(candidatesByID) == 0 {
		return executionOverlapPlan{ResidualUniversePaths: universePaths}
	}
	candidates := make([]*horizonmodels.WatchSubject, 0, len(candidatesByID))
	for _, subject := range candidatesByID {
		candidates = append(candidates, subject)
	}
	return s.resolveExecutionOverlapPlanFromCandidates(tenantID, lensBindings, packID, userID, selfID, snapshot, candidates)
}

func (s *Service) resolveExecutionOverlapPlanFromCandidates(
	tenantID string,
	lensBindings []string,
	packID string,
	userID string,
	selfID string,
	snapshot *focusobject.Snapshot,
	candidates []*horizonmodels.WatchSubject,
) executionOverlapPlan {
	if snapshot == nil {
		return executionOverlapPlan{}
	}
	universePaths := compactUnique(append([]string(nil), snapshot.Focus.UniversePaths...))
	if len(universePaths) == 0 {
		return executionOverlapPlan{}
	}
	filtered := make([]*horizonmodels.WatchSubject, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || strings.TrimSpace(candidate.ID) == "" || strings.TrimSpace(candidate.ID) == strings.TrimSpace(selfID) {
			continue
		}
		if !subjectEligibleForExecutionSharing(candidate, tenantID, lensBindings, packID, userID) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return executionOverlapPlan{ResidualUniversePaths: universePaths}
	}
	newArtifacts := compactUnique(append([]string(nil), snapshot.Focus.ArtifactCanonicalKeys...))
	ownersByID := map[string]*executionCoverageOwner{}
	pathOwners := map[string]string{}
	for _, path := range universePaths {
		bestScore := 0.0
		var best *horizonmodels.WatchSubject
		var bestReason string
		for _, candidate := range filtered {
			sharedPaths := intersectStrings(universePaths, candidate.UniversePaths)
			if !containsString(sharedPaths, path) {
				continue
			}
			candidateArtifacts := subjectArtifactCanonicalKeys(candidate)
			sharedArtifacts := intersectStrings(newArtifacts, candidateArtifacts)
			score, reason := executionOwnerScore(candidate, len(universePaths), len(sharedPaths), len(newArtifacts), len(sharedArtifacts))
			if score > bestScore {
				bestScore = score
				best = candidate
				bestReason = reason
			}
		}
		if best == nil || bestScore < 0.45 {
			continue
		}
		pathOwners[path] = best.ID
		owner := ownersByID[best.ID]
		if owner == nil {
			sharedArtifacts := intersectStrings(newArtifacts, subjectArtifactCanonicalKeys(best))
			owner = &executionCoverageOwner{
				SubjectID:                   strings.TrimSpace(best.ID),
				SubjectKind:                 string(best.SubjectKind),
				DisplayLabel:                strings.TrimSpace(best.DisplayLabel),
				SharedArtifactCanonicalKeys: sharedArtifacts,
				OverlapScore:                bestScore,
				Reason:                      bestReason,
			}
			ownersByID[best.ID] = owner
		}
		owner.CoveredUniversePaths = append(owner.CoveredUniversePaths, path)
		if bestScore > owner.OverlapScore {
			owner.OverlapScore = bestScore
			owner.Reason = bestReason
		}
	}
	owners := make([]executionCoverageOwner, 0, len(ownersByID))
	sharedUniverse := make([]string, 0, len(pathOwners))
	residual := make([]string, 0, len(universePaths))
	for _, path := range universePaths {
		if _, ok := pathOwners[path]; ok {
			sharedUniverse = append(sharedUniverse, path)
		} else {
			residual = append(residual, path)
		}
	}
	for _, owner := range ownersByID {
		owner.CoveredUniversePaths = compactUnique(owner.CoveredUniversePaths)
		owners = append(owners, *owner)
	}
	sort.SliceStable(owners, func(i, j int) bool {
		if owners[i].OverlapScore == owners[j].OverlapScore {
			return owners[i].SubjectID < owners[j].SubjectID
		}
		return owners[i].OverlapScore > owners[j].OverlapScore
	})
	ownerIDs := make([]string, 0, len(owners))
	sharedArtifacts := make([]string, 0, len(newArtifacts))
	for _, owner := range owners {
		ownerIDs = append(ownerIDs, owner.SubjectID)
		sharedArtifacts = append(sharedArtifacts, owner.SharedArtifactCanonicalKeys...)
	}
	plan := executionOverlapPlan{
		OwnerSubjectIDs:             compactUnique(ownerIDs),
		SharedUniversePaths:         compactUnique(sharedUniverse),
		ResidualUniversePaths:       compactUnique(residual),
		SharedArtifactCanonicalKeys: compactUnique(sharedArtifacts),
		Owners:                      owners,
	}
	if len(owners) > 0 {
		plan.PrimaryOwnerSubjectID = owners[0].SubjectID
	}
	return plan
}

func subjectEligibleForExecutionSharing(subject *horizonmodels.WatchSubject, tenantID string, lensBindings []string, packID, userID string) bool {
	if subject == nil || subject.Status != horizonmodels.WatchSubjectStatusActive {
		return false
	}
	if strings.TrimSpace(firstNonEmpty(subject.TenantID, "global")) != strings.TrimSpace(firstNonEmpty(tenantID, "global")) {
		return false
	}
	if subject.SubjectKind == horizonmodels.WatchSubjectKindLensUniverse {
		return true
	}
	if strings.TrimSpace(packID) != "" && strings.TrimSpace(subject.PackID) == strings.TrimSpace(packID) {
		return true
	}
	if strings.TrimSpace(userID) != "" && strings.TrimSpace(subject.UserID) == strings.TrimSpace(userID) {
		return true
	}
	return len(intersectStrings(lensBindings, subject.LensBindings)) > 0
}

func executionOwnerScore(candidate *horizonmodels.WatchSubject, newUniverseSize, sharedUniverseSize, newArtifactSize, sharedArtifactSize int) (float64, string) {
	if candidate == nil || newUniverseSize <= 0 || sharedUniverseSize <= 0 {
		return 0, ""
	}
	coverageRatio := float64(sharedUniverseSize) / float64(newUniverseSize)
	artifactRatio := 0.0
	if newArtifactSize > 0 {
		artifactRatio = float64(sharedArtifactSize) / float64(newArtifactSize)
	}
	generality := 0.35
	reason := "peer_overlap"
	switch candidate.SubjectKind {
	case horizonmodels.WatchSubjectKindLensUniverse:
		generality = 1.00
		reason = "lens_universe_covers_graph_segment"
	case horizonmodels.WatchSubjectKindFocusObject:
		generality = 0.70
		reason = "focus_subject_covers_graph_segment"
	case horizonmodels.WatchSubjectKindPathOverlay:
		generality = 0.65
		reason = "path_overlay_covers_artifact_segment"
	case horizonmodels.WatchSubjectKindFragment, horizonmodels.WatchSubjectKindExplicitTarget:
		generality = 0.45
		reason = "subject_anchor_covers_graph_segment"
	}
	broaderBonus := 0.0
	if len(candidate.UniversePaths) >= newUniverseSize {
		broaderBonus = 0.10
	}
	score := coverageRatio*0.65 + artifactRatio*0.15 + generality*0.20 + broaderBonus
	if coverageRatio < 0.20 {
		score *= 0.5
	}
	return score, reason
}

func executionCoverageToMetadata(plan executionOverlapPlan) map[string]any {
	if len(plan.OwnerSubjectIDs) == 0 && len(plan.SharedUniversePaths) == 0 {
		return nil
	}
	owners := make([]map[string]any, 0, len(plan.Owners))
	for _, owner := range plan.Owners {
		owners = append(owners, map[string]any{
			"subjectId":                   owner.SubjectID,
			"subjectKind":                 owner.SubjectKind,
			"displayLabel":                owner.DisplayLabel,
			"coveredUniversePaths":        append([]string(nil), owner.CoveredUniversePaths...),
			"sharedArtifactCanonicalKeys": append([]string(nil), owner.SharedArtifactCanonicalKeys...),
			"overlapScore":                owner.OverlapScore,
			"reason":                      owner.Reason,
		})
	}
	return map[string]any{
		"primaryOwnerSubjectId":       strings.TrimSpace(plan.PrimaryOwnerSubjectID),
		"ownerSubjectIds":             append([]string(nil), compactUnique(plan.OwnerSubjectIDs)...),
		"sharedUniversePaths":         append([]string(nil), compactUnique(plan.SharedUniversePaths)...),
		"residualUniversePaths":       append([]string(nil), compactUnique(plan.ResidualUniversePaths)...),
		"sharedArtifactCanonicalKeys": append([]string(nil), compactUnique(plan.SharedArtifactCanonicalKeys)...),
		"owners":                      owners,
	}
}

func executionCoverageMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	if typed, ok := metadata[executionCoverageMetadataKey].(map[string]any); ok {
		return typed
	}
	return nil
}

func subjectExecutionCoveredByOwner(subject *horizonmodels.WatchSubject, trigger *horizonmodels.WatchTrigger) bool {
	if subject == nil || trigger == nil {
		return false
	}
	coverage := executionCoverageMetadata(subject.Metadata)
	if len(coverage) == 0 || strings.TrimSpace(asString(coverage["primaryOwnerSubjectId"])) == "" {
		return false
	}
	sharedUniverse := parseAnyStringSlice(coverage["sharedUniversePaths"])
	if len(sharedUniverse) == 0 {
		return false
	}
	paths := append([]string{}, trigger.CanonicalPaths...)
	paths = append(paths, firstNonEmpty(subject.AnchorCanonicalPath))
	paths = compactUnique(paths)
	if len(paths) == 0 {
		return false
	}
	covered := 0
	for _, path := range paths {
		if containsString(sharedUniverse, path) {
			covered++
		}
	}
	return covered == len(paths)
}

func subjectArtifactCanonicalKeys(subject *horizonmodels.WatchSubject) []string {
	if subject == nil || len(subject.Metadata) == 0 {
		return nil
	}
	keys := parseAnyStringSlice(subject.Metadata[focusObjectArtifactsMetadataKey])
	if len(keys) == 0 {
		keys = parseAnyStringSlice(subject.Metadata["artifactCanonicalKeys"])
	}
	if len(keys) == 0 {
		keys = parseAnyStringSlice(subject.Metadata["backingCanonicalPaths"])
	}
	if len(keys) == 0 {
		if artifactSummary, ok := subject.Metadata["artifactSummary"].(map[string]any); ok {
			keys = append(keys, parseAnyStringSlice(artifactSummary["artifactCanonicalKey"])...)
		}
	}
	return compactUnique(keys)
}

func intersectStrings(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	for _, item := range compactUnique(right) {
		seen[item] = struct{}{}
	}
	out := make([]string, 0, minInt(len(left), len(right)))
	for _, item := range compactUnique(left) {
		if _, ok := seen[item]; ok {
			out = append(out, item)
		}
	}
	return compactUnique(out)
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func runtimeTenantCandidates(tenantID string) []string {
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	if tenantID == "global" {
		return []string{"global"}
	}
	return []string{tenantID, "global"}
}

func dependencyEdgeKey(edge *horizonmodels.DependencyEdge) string {
	if edge == nil {
		return ""
	}
	if strings.TrimSpace(edge.ID) != "" {
		return strings.TrimSpace(edge.ID)
	}
	return strings.Join([]string{
		strings.TrimSpace(edge.SourceFragmentID),
		strings.TrimSpace(edge.TargetFragmentID),
		string(edge.Type),
	}, "|")
}

func factObservedAt(fact *coremodels.OntologyFact) time.Time {
	if fact == nil {
		return time.Time{}
	}
	if fact.ObservedAt != nil && !fact.ObservedAt.IsZero() {
		return fact.ObservedAt.UTC()
	}
	if !fact.UpdatedAt.IsZero() {
		return fact.UpdatedAt.UTC()
	}
	return fact.CreatedAt.UTC()
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func subjectLabelOrEmpty(subject *horizonmodels.WatchSubject) string {
	if subject == nil {
		return ""
	}
	return strings.TrimSpace(subject.DisplayLabel)
}

func subjectTenantOrEmpty(subject *horizonmodels.WatchSubject) string {
	if subject == nil {
		return ""
	}
	return strings.TrimSpace(subject.TenantID)
}

func subjectProjectOrEmpty(subject *horizonmodels.WatchSubject) string {
	if subject == nil {
		return ""
	}
	return strings.TrimSpace(subject.ProjectID)
}

func subjectLensOrEmpty(subject *horizonmodels.WatchSubject) string {
	if subject == nil {
		return ""
	}
	return strings.TrimSpace(subject.LensID)
}

func subjectPackOrEmpty(subject *horizonmodels.WatchSubject) string {
	if subject == nil {
		return ""
	}
	return strings.TrimSpace(subject.PackID)
}

func subjectUserOrEmpty(subject *horizonmodels.WatchSubject) string {
	if subject == nil {
		return ""
	}
	return strings.TrimSpace(subject.UserID)
}

func subjectFragmentIDOrEmpty(subject *horizonmodels.WatchSubject) string {
	if subject == nil {
		return ""
	}
	return strings.TrimSpace(subject.AnchorFragmentID)
}

func subjectPriorityOrDefault(subject *horizonmodels.WatchSubject) int {
	if subject == nil {
		return 50
	}
	return normalizedPriority(subject.Priority)
}

func existingLensBindings(subject *horizonmodels.WatchSubject) []string {
	if subject == nil {
		return nil
	}
	return compactUnique(append([]string(nil), subject.LensBindings...))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
