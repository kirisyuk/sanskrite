package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizoninterp "github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonprop "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	horizonqa "github.com/igorkirisyuk/sanskrite/internal/horizon/qa"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func initAIExecutor(ctx context.Context, projectID string) *ai.AITaskExecutor {
	mm := ai.NewAIModelManager()
	services := make([]ai.AIService, 0, 4)

	geminiKey := ""
	if projectID != "" {
		for _, name := range []string{"gemini_api_key", "GEMINI_API_KEY"} {
			if value, err := gcp.LoadSecret(ctx, projectID, name); err == nil && strings.TrimSpace(value) != "" {
				geminiKey = strings.TrimSpace(value)
				break
			}
		}
	}
	if geminiKey == "" {
		geminiKey = strings.TrimSpace(firstNonEmpty(
			gcp.GetEnvOrSecret(ctx, projectID, "GEMINI_API_KEY", "gemini_api_key"),
			os.Getenv("GEMINI_API_KEY"),
		))
	}
	if geminiKey != "" {
		services = append(services, ai.NewGeminiService(geminiKey, mm))
	}

	if projectID != "" {
		location := strings.TrimSpace(firstNonEmpty(os.Getenv("VERTEX_LOCATION"), "us-central1"))
		if vertex, err := ai.NewVertexAIService(projectID, location, mm); err == nil {
			if ds := resolveVertexDataStoreID(); ds != "" {
				vertex = vertex.WithDataStore(ds)
			}
			services = append(services, vertex)
		}
	}
	if len(services) == 0 {
		return nil
	}
	return ai.NewAITaskExecutor(services, mm)
}

func resolveVertexDataStoreID() string {
	for _, value := range []string{
		os.Getenv("VERTEX_SEARCH_DATA_STORE_ID"),
		os.Getenv("VERTEX_SEARCH_NV_HORIZON_DATA_STORE_ID"),
		os.Getenv("VERTEX_SEARCH_HORIZON_DATA_STORE_ID"),
	} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func summarizeSubject(subject *horizonmodels.WatchSubject, artifact map[string]any) *SubjectSummary {
	if subject == nil {
		return nil
	}
	return &SubjectSummary{
		ID:                  strings.TrimSpace(subject.ID),
		SubjectKind:         string(subject.SubjectKind),
		DisplayLabel:        strings.TrimSpace(subject.DisplayLabel),
		AnchorCanonicalPath: strings.TrimSpace(subject.AnchorCanonicalPath),
		LensID:              strings.TrimSpace(subject.LensID),
		MacroAgentPolicyID:  stringValue(subject.Metadata["macroAgentPolicyId"]),
		MacroAgentClass:     stringValue(subject.Metadata["macroAgentClass"]),
		UniversePaths:       compactUnique(subject.UniversePaths),
		ArtifactSummary:     artifact,
	}
}

func (rt *toolRuntime) materializedSummaryFromSubject(ctx context.Context, subject *horizonmodels.WatchSubject) map[string]any {
	if rt == nil || rt.materializedRepo == nil || subject == nil {
		return nil
	}
	key := strings.TrimSpace(stringValue(subject.Metadata["artifactCanonicalKey"]))
	if key == "" {
		return nil
	}
	artifact, err := rt.materializedRepo.GetByCanonicalKeyAnyKind(ctx, firstNonEmpty(subject.TenantID, "global"), key)
	if err != nil || artifact == nil {
		return map[string]any{
			"artifactCanonicalKey": key,
			"materializedKind":     stringValue(subject.Metadata["materializedKind"]),
			"legIndices":           subject.Metadata["legIndices"],
		}
	}
	return map[string]any{
		"id":                  artifact.ID,
		"canonicalKey":        artifact.CanonicalKey,
		"kind":                artifact.Kind,
		"displayName":         artifact.DisplayName,
		"pathStressScore":     artifact.PathStressScore,
		"bindingScore":        artifact.BindingScore,
		"gapCount":            artifact.GapCount,
		"unresolvedJoinCount": artifact.UnresolvedJoinCount,
		"bottleneckLegIndex":  artifact.BottleneckLeg,
		"nodeRefs":            compactUnique(artifact.NodeRefs),
		"flowRefs":            compactUnique(artifact.FlowRefs),
		"zoneRefs":            compactUnique(artifact.ZoneRefs),
		"affectedZoneRefs":    compactUnique(artifact.AffectedZoneRefs),
	}
}

func summarizeRuns(runs []*horizonmodels.ScopeAgentRun) []*RunSummary {
	out := make([]*RunSummary, 0, len(runs))
	for _, run := range runs {
		if run == nil {
			continue
		}
		out = append(out, summarizeRun(run))
	}
	return out
}

func summarizeRun(run *horizonmodels.ScopeAgentRun) *RunSummary {
	if run == nil {
		return nil
	}
	return &RunSummary{
		ID:                  run.ID,
		Status:              run.Status,
		TriggerType:         run.TriggerType,
		TriggerReason:       run.TriggerReason,
		Queries:             compactUnique(run.Queries),
		ProducedProposalIDs: compactUnique(run.ProducedProposalIDs),
		Trace:               run.Trace,
		StartedAt:           run.StartedAt,
		CompletedAt:         run.CompletedAt,
	}
}

func summarizeProposal(proposal *horizonmodels.AgentProposal, decision *horizonmodels.ArbitrationDecision) *ProposalSummary {
	if proposal == nil {
		return nil
	}
	out := &ProposalSummary{
		ID:                   proposal.ID,
		ProposalType:         proposal.ProposalType,
		SignalKey:            proposal.SignalKey,
		TargetCanonicalPaths: compactUnique(proposal.TargetCanonicalPaths),
		Confidence:           proposal.Confidence,
		Explanation:          proposal.Explanation,
		SourceRefs:           append([]horizonmodels.SourceRef(nil), proposal.SourceRefs...),
		Payload:              proposal.Payload,
		CreatedAt:            proposal.CreatedAt,
	}
	if decision != nil {
		out.Decision = decision.Decision
		out.ReasonCodes = append([]string(nil), decision.ReasonCodes...)
		out.AppliedArtifacts = append([]string(nil), decision.AppliedArtifacts...)
	}
	return out
}

func newestDecision(decisions []*horizonmodels.ArbitrationDecision) *horizonmodels.ArbitrationDecision {
	if len(decisions) == 0 {
		return nil
	}
	return decisions[0]
}

func factIDForProposal(proposal *horizonmodels.AgentProposal, canonicalPath string) string {
	sum := sha1.Sum([]byte(strings.Join([]string{proposal.ID, canonicalPath, proposal.DedupKey}, "|")))
	return "scope_agent_" + hex.EncodeToString(sum[:8])
}

func collectQueries(runs []*horizonmodels.ScopeAgentRun) []string {
	out := make([]string, 0, len(runs)*2)
	for _, run := range runs {
		if run == nil {
			continue
		}
		out = append(out, run.Queries...)
	}
	return compactUnique(out)
}

func collectRunIDs(runs []*horizonmodels.ScopeAgentRun) []string {
	out := make([]string, 0, len(runs))
	for _, run := range runs {
		if run != nil && strings.TrimSpace(run.ID) != "" {
			out = append(out, strings.TrimSpace(run.ID))
		}
	}
	return compactUnique(out)
}

func collectProposalIDsFromRuns(runs []*horizonmodels.ScopeAgentRun, before []*horizonmodels.ScopeAgentRun) []string {
	beforeSet := map[string]struct{}{}
	for _, run := range before {
		if run != nil && strings.TrimSpace(run.ID) != "" {
			beforeSet[run.ID] = struct{}{}
		}
	}
	out := make([]string, 0, len(runs))
	for _, run := range runs {
		if run == nil || strings.TrimSpace(run.ID) == "" {
			continue
		}
		if _, ok := beforeSet[run.ID]; ok {
			continue
		}
		out = append(out, run.ProducedProposalIDs...)
	}
	return compactUnique(out)
}

func collectProducedProposalIDs(runs []*horizonmodels.ScopeAgentRun) []string {
	out := make([]string, 0, len(runs)*2)
	for _, run := range runs {
		if run == nil {
			continue
		}
		out = append(out, run.ProducedProposalIDs...)
	}
	return compactUnique(out)
}

func newestRun(runs []*horizonmodels.ScopeAgentRun) *horizonmodels.ScopeAgentRun {
	if len(runs) == 0 {
		return nil
	}
	return runs[0]
}

func filterFreshRuns(after, before []*horizonmodels.ScopeAgentRun, startedAt time.Time) []*horizonmodels.ScopeAgentRun {
	beforeSet := map[string]struct{}{}
	for _, run := range before {
		if run != nil && strings.TrimSpace(run.ID) != "" {
			beforeSet[strings.TrimSpace(run.ID)] = struct{}{}
		}
	}
	out := make([]*horizonmodels.ScopeAgentRun, 0, len(after))
	for _, run := range after {
		if run == nil || strings.TrimSpace(run.ID) == "" {
			continue
		}
		if _, ok := beforeSet[strings.TrimSpace(run.ID)]; ok {
			continue
		}
		if run.CreatedAt.Before(startedAt.Add(-2 * time.Second)) {
			continue
		}
		out = append(out, run)
	}
	return out
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func matchesCanonicalContains(canonicalPath, needle string) bool {
	canonicalPath = strings.TrimSpace(strings.ToLower(canonicalPath))
	needle = strings.TrimSpace(strings.ToLower(needle))
	if needle == "" {
		return true
	}
	return strings.Contains(canonicalPath, needle)
}

func matchesSignalAxisPrefix(signal *horizonmodels.SignalDescriptor, prefix string) bool {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if prefix == "" {
		return true
	}
	if signal == nil {
		return false
	}
	axis := strings.TrimSpace(strings.ToLower(signal.Axis))
	return strings.HasPrefix(axis, prefix)
}

func stringValue(v any) string {
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func compactUnique(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func parseAnyStringSlice(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return compactUnique(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := stringValue(item); value != "" {
				out = append(out, value)
			}
		}
		return compactUnique(out)
	default:
		return nil
	}
}

func findProducedProposalIDs(reports any) []string {
	return findProducedProposalIDsTyped(reports)
}

func findProducedProposalIDsTyped(reports any) []string {
	out := []string{}
	value := reflect.ValueOf(reports)
	if value.Kind() != reflect.Slice {
		return nil
	}
	for i := 0; i < value.Len(); i++ {
		item := value.Index(i)
		if item.Kind() == reflect.Pointer {
			item = item.Elem()
		}
		if !item.IsValid() {
			continue
		}
		field := item.FieldByName("ProducedProposalIDs")
		if field.IsValid() {
			out = append(out, parseReflectStringSlice(field)...)
		}
	}
	return compactUnique(out)
}

func safeFactID(item *AcceptedSignalSummary) string {
	if item == nil || item.Fact == nil {
		return ""
	}
	return strings.TrimSpace(item.Fact.ID)
}

func subjectScore(rt *toolRuntime, ctx context.Context, subject *horizonmodels.WatchSubject) float64 {
	if subject == nil {
		return -1
	}
	score := float64(subject.Priority)
	switch subject.SubjectKind {
	case horizonmodels.WatchSubjectKindPathOverlay:
		score += 100
	case horizonmodels.WatchSubjectKindLensUniverse:
		score += 50
	}
	key := strings.TrimSpace(stringValue(subject.Metadata["artifactCanonicalKey"]))
	if key == "" || rt == nil || rt.materializedRepo == nil {
		return score
	}
	if artifact, err := rt.materializedRepo.GetByCanonicalKeyAnyKind(ctx, firstNonEmpty(subject.TenantID, "global"), key); err == nil && artifact != nil {
		score += artifact.PathStressScore*100 + artifact.BindingScore*20 - float64(artifact.GapCount*2)
	}
	return score
}

func summarizeLensCompanions(subjects []*horizonmodels.WatchSubject, selectedID string) []string {
	out := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		if subject == nil || subject.ID == selectedID {
			continue
		}
		out = append(out, firstNonEmpty(subject.DisplayLabel, subject.AnchorCanonicalPath))
	}
	sort.Strings(out)
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func dedupeAcceptedSignals(in []*AcceptedSignalSummary) []*AcceptedSignalSummary {
	seen := map[string]struct{}{}
	out := make([]*AcceptedSignalSummary, 0, len(in))
	for _, item := range in {
		if item == nil || item.Fact == nil || strings.TrimSpace(item.Fact.ID) == "" {
			continue
		}
		if _, ok := seen[item.Fact.ID]; ok {
			continue
		}
		seen[item.Fact.ID] = struct{}{}
		out = append(out, item)
	}
	return out
}

func affectedFragmentIDSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			out[id] = true
		}
	}
	return out
}

func (rt *toolRuntime) simulationFromRaw(ctx context.Context, raw *horizonprop.SignalSimulationResult, signal *horizonmodels.SignalDescriptor, seedIDs []string) *horizonmodels.SimulationResult {
	if raw == nil {
		return &horizonmodels.SimulationResult{}
	}
	fragments := rt.loadFragmentsByIDs(ctx, append([]string{}, raw.AffectedFragments...))
	fragByID := map[string]*horizonmodels.HorizonFragment{}
	seedSet := map[string]bool{}
	for _, id := range seedIDs {
		if id = strings.TrimSpace(id); id != "" {
			seedSet[id] = true
		}
	}
	for _, frag := range fragments {
		if frag != nil && frag.ID != "" {
			fragByID[frag.ID] = frag
		}
	}
	nodeImpacts := make([]horizonmodels.NodeImpact, 0, len(raw.Accumulated))
	pathFromSeed := buildPathFromSeed(raw, fragments)
	for fragID, acc := range raw.Accumulated {
		axisAmps := map[string]float64{}
		total := 0.0
		minHops := -1
		for _, item := range acc {
			if item.Amplitude <= 0.05 {
				continue
			}
			axisAmps[item.Axis] = maxFloat(axisAmps[item.Axis], item.Amplitude)
			total = maxFloat(total, item.Amplitude)
			if minHops < 0 || item.HopCount < minHops {
				minHops = item.HopCount
			}
		}
		if seedSet[fragID] && minHops < 0 {
			minHops = 0
		}
		frag := fragByID[fragID]
		name := fragID
		canon := fragID
		typ := ""
		if frag != nil {
			canon = frag.CanonicalPath
			typ = string(frag.Type)
			if label := firstNonEmpty(frag.Labels["en"], canon); label != "" {
				name = label
			}
		}
		nodeImpacts = append(nodeImpacts, horizonmodels.NodeImpact{
			FragmentID:     fragID,
			CanonicalPath:  canon,
			Name:           name,
			Type:           typ,
			AxisAmplitudes: axisAmps,
			TotalAmplitude: total,
			Hops:           maxInt(minHops, 0),
			PathFromSeed:   pathFromSeed[fragID],
		})
	}
	sort.SliceStable(nodeImpacts, func(i, j int) bool {
		if nodeImpacts[i].TotalAmplitude == nodeImpacts[j].TotalAmplitude {
			return nodeImpacts[i].Hops < nodeImpacts[j].Hops
		}
		return nodeImpacts[i].TotalAmplitude > nodeImpacts[j].TotalAmplitude
	})
	tracePaths := make([]horizonmodels.TracePath, 0, len(raw.PropagationPaths))
	for _, path := range raw.PropagationPaths {
		if len(path) == 0 {
			continue
		}
		canon := make([]string, 0, len(path))
		amps := make([]float64, 0, len(path))
		edgeTypes := make([]string, 0, maxInt(len(path)-1, 0))
		for _, fragID := range path {
			if frag, ok := fragByID[fragID]; ok && frag != nil && strings.TrimSpace(frag.CanonicalPath) != "" {
				canon = append(canon, strings.TrimSpace(frag.CanonicalPath))
			} else {
				canon = append(canon, strings.TrimSpace(fragID))
			}
			amp := 0.0
			for _, item := range raw.Accumulated[fragID] {
				amp = maxFloat(amp, item.Amplitude)
			}
			amps = append(amps, amp)
		}
		for _, event := range raw.Trace {
			if strings.TrimSpace(event.Outcome) != "propagated" {
				continue
			}
			if len(event.EdgeType) > 0 {
				edgeTypes = append(edgeTypes, strings.TrimSpace(event.EdgeType))
			}
		}
		tracePaths = append(tracePaths, horizonmodels.TracePath{
			NodeCanonicalPaths: canon,
			Axis:               signal.Axis,
			Amplitudes:         amps,
			EdgeTypes:          compactUnique(edgeTypes),
		})
	}
	sim := &horizonmodels.SimulationResult{
		NodeImpacts:        nodeImpacts,
		TracePaths:         tracePaths,
		HighlightedEdgeIDs: horizonqa.ExtractEdgeIDsFromTracePaths(tracePaths, mapCanonicalToID(fragments)),
	}
	if subjectArtifacts := rt.materializedArtifactsForFragments(ctx, fragments); len(subjectArtifacts) > 0 {
		sim.ChainImpacts = horizonqa.ComputeChainImpacts(subjectArtifacts, nodeImpacts)
	}
	return sim
}

func buildPathFromSeed(raw *horizonprop.SignalSimulationResult, fragments []*horizonmodels.HorizonFragment) map[string][]string {
	fragByID := map[string]string{}
	for _, frag := range fragments {
		if frag != nil && frag.ID != "" {
			fragByID[frag.ID] = frag.CanonicalPath
		}
	}
	out := map[string][]string{}
	for _, path := range raw.PropagationPaths {
		if len(path) == 0 {
			continue
		}
		last := strings.TrimSpace(path[len(path)-1])
		if last == "" {
			continue
		}
		if _, exists := out[last]; exists {
			continue
		}
		canon := make([]string, 0, len(path))
		for _, fragID := range path {
			if cp := strings.TrimSpace(fragByID[strings.TrimSpace(fragID)]); cp != "" {
				canon = append(canon, cp)
			}
		}
		if len(canon) > 0 {
			out[last] = canon
		}
	}
	return out
}

func mapCanonicalToID(fragments []*horizonmodels.HorizonFragment) map[string]string {
	out := map[string]string{}
	for _, frag := range fragments {
		if frag != nil && frag.ID != "" && frag.CanonicalPath != "" {
			out[frag.CanonicalPath] = frag.ID
		}
	}
	return out
}

func (rt *toolRuntime) loadFragmentsByIDs(ctx context.Context, ids []string) []*horizonmodels.HorizonFragment {
	out := make([]*horizonmodels.HorizonFragment, 0, len(ids))
	seen := map[string]struct{}{}
	ordered := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
	}
	if len(ordered) == 0 || rt == nil || rt.fragmentsRepo == nil {
		return out
	}
	if got, err := rt.fragmentsRepo.GetMany(ctx, ordered); err == nil && len(got) > 0 {
		for _, id := range ordered {
			if frag := got[id]; frag != nil {
				out = append(out, frag)
			}
		}
		return out
	}
	for _, id := range ordered {
		if frag, err := rt.fragmentsRepo.Get(ctx, id); err == nil && frag != nil {
			out = append(out, frag)
		}
	}
	return out
}

func (rt *toolRuntime) loadFragmentsForSimulation(ctx context.Context, sim *horizonmodels.SimulationResult) []*horizonmodels.HorizonFragment {
	if sim == nil {
		return nil
	}
	ids := make([]string, 0, len(sim.NodeImpacts))
	for _, impact := range sim.NodeImpacts {
		if strings.TrimSpace(impact.FragmentID) != "" {
			ids = append(ids, impact.FragmentID)
		}
	}
	return rt.loadFragmentsByIDs(ctx, ids)
}

func (rt *toolRuntime) materializedArtifactsForFragments(ctx context.Context, fragments []*horizonmodels.HorizonFragment) []*horizonmodels.MaterializedArtifact {
	if rt == nil || rt.materializedRepo == nil || len(fragments) == 0 {
		return nil
	}
	out := []*horizonmodels.MaterializedArtifact{}
	seen := map[string]struct{}{}
	for _, frag := range fragments {
		if frag == nil || strings.TrimSpace(frag.CanonicalPath) == "" {
			continue
		}
		if art, err := rt.materializedRepo.GetByCanonicalKeyAnyKind(ctx, firstNonEmpty(frag.TenantID, "global"), frag.CanonicalPath); err == nil && art != nil {
			if _, ok := seen[art.ID]; ok {
				continue
			}
			seen[art.ID] = struct{}{}
			out = append(out, art)
		}
	}
	return out
}

func topNodeImpacts(items []horizonmodels.NodeImpact, limit int, seedIDs []string) []horizonmodels.NodeImpact {
	if len(items) == 0 {
		return nil
	}
	seedSet := map[string]struct{}{}
	for _, id := range seedIDs {
		if id = strings.TrimSpace(id); id != "" {
			seedSet[id] = struct{}{}
		}
	}
	far := make([]horizonmodels.NodeImpact, 0, len(items))
	near := make([]horizonmodels.NodeImpact, 0, len(items))
	for _, item := range items {
		if _, ok := seedSet[item.FragmentID]; ok && item.Hops == 0 {
			continue
		}
		if item.Hops >= 2 {
			far = append(far, item)
		} else {
			near = append(near, item)
		}
	}
	out := make([]horizonmodels.NodeImpact, 0, minInt(limit, len(items)))
	farBudget := minInt(limit/2, len(far))
	if farBudget == 0 && len(far) > 0 {
		farBudget = 1
	}
	out = append(out, far[:farBudget]...)
	for _, item := range near {
		if len(out) >= limit {
			break
		}
		out = append(out, item)
	}
	for _, item := range far[farBudget:] {
		if len(out) >= limit {
			break
		}
		out = append(out, item)
	}
	return out
}

func escapedNodeImpacts(items []horizonmodels.NodeImpact, universe map[string]struct{}, seedIDs []string) []horizonmodels.NodeImpact {
	seedSet := map[string]struct{}{}
	for _, id := range seedIDs {
		if id = strings.TrimSpace(id); id != "" {
			seedSet[id] = struct{}{}
		}
	}
	out := []horizonmodels.NodeImpact{}
	for _, item := range items {
		if _, ok := seedSet[item.FragmentID]; ok {
			continue
		}
		if item.Hops < 2 {
			continue
		}
		if len(universe) > 0 {
			if _, ok := universe[strings.TrimSpace(item.CanonicalPath)]; ok {
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

func countNonSeed(items []horizonmodels.NodeImpact, seedIDs []string) int {
	seedSet := map[string]struct{}{}
	for _, id := range seedIDs {
		if id = strings.TrimSpace(id); id != "" {
			seedSet[id] = struct{}{}
		}
	}
	n := 0
	for _, item := range items {
		if _, ok := seedSet[item.FragmentID]; !ok {
			n++
		}
	}
	return n
}

func countTracePathsWithEdges(paths []horizonmodels.TracePath) int {
	n := 0
	for _, path := range paths {
		if len(path.EdgeTypes) > 0 {
			n++
		}
	}
	return n
}

func leafEvidenceForNode(sim *horizonmodels.SimulationResult, fragmentID string) []horizonmodels.LeafSignalEvidence {
	if sim == nil {
		return nil
	}
	out := []horizonmodels.LeafSignalEvidence{}
	for _, item := range sim.LeafEvidence {
		if strings.TrimSpace(item.FragmentID) == strings.TrimSpace(fragmentID) {
			out = append(out, item)
		}
	}
	return out
}

func firstSeedImpact(items []horizonmodels.NodeImpact) *horizonmodels.NodeImpact {
	for _, item := range items {
		if item.Hops == 0 {
			return &item
		}
	}
	if len(items) == 0 {
		return nil
	}
	return &items[0]
}

func seedIDsFromNodeImpacts(items []horizonmodels.NodeImpact) []string {
	out := []string{}
	for _, item := range items {
		if item.Hops == 0 {
			out = append(out, item.FragmentID)
		}
	}
	return compactUnique(out)
}

func collectEvidenceSummaries(leaf []horizonmodels.LeafSignalEvidence, interpretation *horizoninterp.RiskInterpretation) []string {
	out := []string{}
	for _, item := range leaf {
		if summary := strings.TrimSpace(item.Summary); summary != "" {
			out = append(out, summary)
		}
	}
	if interpretation != nil {
		for _, event := range interpretation.TopEvents {
			if summary := strings.TrimSpace(event.EventSummary); summary != "" {
				out = append(out, summary)
			}
		}
	}
	sort.Strings(out)
	if len(out) > 5 {
		out = out[:5]
	}
	return compactUnique(out)
}

func evaluateCriteria(report *E2EReport, opts toolOptions) []*StageVerdict {
	criteria := []*StageVerdict{
		evaluateAppearance(report, opts),
		evaluateSignalAdmission(report),
		evaluateFarEnd(report),
		evaluateInterpretability(report),
		evaluateMeaningfulness(report),
	}
	return criteria
}

func evaluateAppearance(report *E2EReport, opts toolOptions) *StageVerdict {
	stats := map[string]any{
		"webUrls":         len(valuesOrEmpty(report.Grounding, func(g *GroundingSummary) []string { return g.WebURLs })),
		"datastoreHits":   len(valuesOrEmpty(report.Grounding, func(g *GroundingSummary) []string { return g.DatastoreFragmentIDs })),
		"proposalCount":   len(report.Proposals),
		"acceptedSignals": len(report.AcceptedSignals),
	}
	if report.Runtime != nil {
		stats["freshExecution"] = report.Runtime.FreshExecution
	}
	groundedFactMaterialized := false
	for _, item := range report.AcceptedSignals {
		if item == nil || item.Fact == nil {
			continue
		}
		if item.Fact.ExtractionLineage != nil && item.Fact.ExtractionLineage.LLMUsed && len(item.Fact.SourceIDs) > 0 {
			groundedFactMaterialized = true
			break
		}
	}
	stats["groundedFactMaterialized"] = groundedFactMaterialized
	if (report.Mode == modeAnalyzeChain || report.Mode == modeReplayReport) && len(report.AcceptedSignals) > 0 {
		return &StageVerdict{Name: stageAppearance, Status: verdictWarn, Reason: "replayed_without_live_grounding", Stats: stats}
	}
	if report.Runtime != nil && !report.Runtime.FreshExecution {
		return &StageVerdict{Name: stageAppearance, Status: verdictFail, Reason: firstNonEmpty(report.Runtime.NoopReason, "no_fresh_execution"), Stats: stats}
	}
	grounded := false
	if report.Grounding != nil {
		grounded = len(report.Grounding.WebURLs) > 0 || len(report.Grounding.DatastoreFragmentIDs) > 0
	}
	if grounded && len(report.Proposals) > 0 && len(report.AcceptedSignals) > 0 {
		return &StageVerdict{Name: stageAppearance, Status: verdictPass, Stats: stats}
	}
	if groundedFactMaterialized && len(report.AcceptedSignals) > 0 {
		return &StageVerdict{Name: stageAppearance, Status: verdictPass, Reason: "grounded_materialized_without_citations", Stats: stats}
	}
	if grounded && opts.RequireMaterialChange {
		return &StageVerdict{Name: stageAppearance, Status: verdictFail, Reason: "grounded_no_materialized_signal", Stats: stats}
	}
	if !grounded {
		return &StageVerdict{Name: stageAppearance, Status: verdictFail, Reason: "no_grounding_evidence", Stats: stats}
	}
	return &StageVerdict{Name: stageAppearance, Status: verdictWarn, Reason: "partial_materialization", Stats: stats}
}

func evaluateSignalAdmission(report *E2EReport) *StageVerdict {
	stats := map[string]any{"acceptedSignals": len(report.AcceptedSignals)}
	if len(report.AcceptedSignals) == 0 {
		return &StageVerdict{Name: stageSignalAdmission, Status: verdictFail, Reason: "no_accepted_signal_facts", Stats: stats}
	}
	missing := 0
	invalidType := 0
	for _, item := range report.AcceptedSignals {
		if item == nil || item.Fact == nil {
			missing++
			continue
		}
		if !looksLikeSignalFactType(item.Fact.Type) {
			invalidType++
		}
		if (len(item.Fact.SourceRefs) == 0 && len(item.Fact.SourceIDs) == 0) || strings.TrimSpace(item.Fact.Summary) == "" {
			missing++
		}
	}
	stats["missingFields"] = missing
	stats["invalidType"] = invalidType
	if invalidType > 0 {
		return &StageVerdict{Name: stageSignalAdmission, Status: verdictFail, Reason: "non_axis_signal_fact_type", Stats: stats}
	}
	if missing > 0 {
		return &StageVerdict{Name: stageSignalAdmission, Status: verdictWarn, Reason: "accepted_signal_missing_source_or_summary", Stats: stats}
	}
	return &StageVerdict{Name: stageSignalAdmission, Status: verdictPass, Stats: stats}
}

func evaluateFarEnd(report *E2EReport) *StageVerdict {
	stats := map[string]any{}
	if report.PropagationAnalysis == nil {
		return &StageVerdict{Name: stageFarEndImpact, Status: verdictFail, Reason: "no_propagation_analysis", Stats: stats}
	}
	stats["meaningfulFarEnds"] = report.PropagationAnalysis.MeaningfulFarEnds
	stats["nonSeedEndpoints"] = report.PropagationAnalysis.NonSeedEndpoints
	stats["farHopEndpoints"] = countFarHopEndpoints(report.PropagationAnalysis)
	stats["multiNodeTracePaths"] = countMultiNodeTracePaths(report.PropagationAnalysis)
	if report.PropagationAnalysis.MeaningfulFarEnds > 0 {
		return &StageVerdict{Name: stageFarEndImpact, Status: verdictPass, Stats: stats}
	}
	if stats["farHopEndpoints"].(int) > 0 {
		return &StageVerdict{Name: stageFarEndImpact, Status: verdictPass, Reason: "far_hop_endpoints_detected", Stats: stats}
	}
	if stats["multiNodeTracePaths"].(int) > 0 && report.PropagationAnalysis.NonSeedEndpoints > 0 {
		return &StageVerdict{Name: stageFarEndImpact, Status: verdictPass, Reason: "multi_hop_paths_detected", Stats: stats}
	}
	if report.PropagationAnalysis.NonSeedEndpoints > 0 {
		return &StageVerdict{Name: stageFarEndImpact, Status: verdictWarn, Reason: "downstream_exists_but_not_far_enough", Stats: stats}
	}
	return &StageVerdict{Name: stageFarEndImpact, Status: verdictFail, Reason: "no_downstream_impact", Stats: stats}
}

func evaluateInterpretability(report *E2EReport) *StageVerdict {
	stats := map[string]any{}
	if report.InterpretationAnalysis == nil {
		return &StageVerdict{Name: stageInterpretable, Status: verdictFail, Reason: "no_interpretation_analysis", Stats: stats}
	}
	seedOK := report.InterpretationAnalysis.SeedInterpretation != nil && report.InterpretationAnalysis.SeedInterpretation.Interpretation != nil && strings.TrimSpace(report.InterpretationAnalysis.SeedInterpretation.Interpretation.Summary) != ""
	endpointOK := 0
	for _, item := range report.InterpretationAnalysis.EndpointInterpretations {
		if item != nil && item.Interpretation != nil && strings.TrimSpace(item.Interpretation.Summary) != "" {
			endpointOK++
		}
	}
	stats["seedInterpretation"] = seedOK
	stats["endpointInterpretations"] = endpointOK
	if seedOK && endpointOK > 0 {
		return &StageVerdict{Name: stageInterpretable, Status: verdictPass, Stats: stats}
	}
	if seedOK || endpointOK > 0 {
		return &StageVerdict{Name: stageInterpretable, Status: verdictWarn, Reason: "one_side_missing_interpretation", Stats: stats}
	}
	return &StageVerdict{Name: stageInterpretable, Status: verdictFail, Reason: "no_seed_or_endpoint_interpretation", Stats: stats}
}

func evaluateMeaningfulness(report *E2EReport) *StageVerdict {
	stats := map[string]any{}
	if report.PropagationAnalysis == nil || len(report.PropagationAnalysis.Signals) == 0 {
		return &StageVerdict{Name: stageMeaningful, Status: verdictFail, Reason: "no_propagation_signal_runs", Stats: stats}
	}
	selfOnly := 0
	withEdges := 0
	withWatchpoints := 0
	amplitudeDecayOK := 0
	multiNodePaths := countMultiNodeTracePaths(report.PropagationAnalysis)
	farHopEndpoints := countFarHopEndpoints(report.PropagationAnalysis)
	for _, proposal := range report.Proposals {
		if proposal == nil {
			continue
		}
		if len(parseAnyStringSlice(proposal.Payload["watchpoints"])) > 0 || len(parseAnyStringSlice(proposal.Payload["triggerConditions"])) > 0 {
			withWatchpoints++
		}
	}
	for _, signal := range report.PropagationAnalysis.Signals {
		if signal == nil || signal.Simulation == nil {
			continue
		}
		if len(signal.Simulation.TracePaths) == 0 {
			selfOnly++
			continue
		}
		if countTracePathsWithEdges(signal.Simulation.TracePaths) > 0 {
			withEdges++
		}
		if propagationLooksDecayed(signal.Simulation.TracePaths) {
			amplitudeDecayOK++
		}
		if propagationSelfOnly(signal.Simulation.TracePaths) {
			selfOnly++
		}
	}
	stats["traceWithEdges"] = withEdges
	stats["watchpointBearingProposals"] = withWatchpoints
	stats["decayConsistentSignals"] = amplitudeDecayOK
	stats["selfOnlySignals"] = selfOnly
	stats["multiNodeTracePaths"] = multiNodePaths
	stats["farHopEndpoints"] = farHopEndpoints
	if withEdges > 0 && amplitudeDecayOK > 0 && selfOnly == 0 {
		if withWatchpoints > 0 {
			return &StageVerdict{Name: stageMeaningful, Status: verdictPass, Stats: stats}
		}
		return &StageVerdict{Name: stageMeaningful, Status: verdictPass, Reason: "meaningful_trace_watchpoints_absent", Stats: stats}
	}
	if multiNodePaths > 0 && amplitudeDecayOK > 0 && farHopEndpoints > 0 {
		if withWatchpoints > 0 {
			return &StageVerdict{Name: stageMeaningful, Status: verdictPass, Reason: "multi_hop_semantics_without_edge_trace", Stats: stats}
		}
		return &StageVerdict{Name: stageMeaningful, Status: verdictPass, Reason: "multi_hop_semantics_without_edge_trace_watchpoints_absent", Stats: stats}
	}
	if multiNodePaths > 0 && farHopEndpoints > 0 {
		return &StageVerdict{Name: stageMeaningful, Status: verdictWarn, Reason: "multi_hop_paths_present_but_decay_unclear", Stats: stats}
	}
	if withEdges > 0 {
		return &StageVerdict{Name: stageMeaningful, Status: verdictWarn, Reason: "trace_present_but_semantics_thin", Stats: stats}
	}
	return &StageVerdict{Name: stageMeaningful, Status: verdictFail, Reason: "self_only_or_dropped_trace", Stats: stats}
}

func countFarHopEndpoints(analysis *PropagationAnalysisSummary) int {
	if analysis == nil {
		return 0
	}
	count := 0
	for _, signal := range analysis.Signals {
		if signal == nil {
			continue
		}
		for _, node := range signal.TopEndpoints {
			if node.Hops >= 2 {
				count++
			}
		}
	}
	return count
}

func countMultiNodeTracePaths(analysis *PropagationAnalysisSummary) int {
	if analysis == nil {
		return 0
	}
	count := 0
	for _, signal := range analysis.Signals {
		if signal == nil || signal.Simulation == nil {
			continue
		}
		for _, path := range signal.Simulation.TracePaths {
			if len(path.NodeCanonicalPaths) > 1 {
				count++
			}
		}
	}
	return count
}

func finalizeVerdict(report *E2EReport) *FinalVerdict {
	status := verdictPass
	for _, item := range report.Criteria {
		if item == nil {
			continue
		}
		status = worstStatus(status, item.Status)
	}
	summary := fmt.Sprintf("appearance=%s, admission=%s, far_end=%s, interpretability=%s, meaningfulness=%s",
		stageStatus(report, stageAppearance),
		stageStatus(report, stageSignalAdmission),
		stageStatus(report, stageFarEndImpact),
		stageStatus(report, stageInterpretable),
		stageStatus(report, stageMeaningful),
	)
	return &FinalVerdict{Status: status, Summary: summary}
}

func stageStatus(report *E2EReport, name string) string {
	for _, item := range report.Criteria {
		if item != nil && item.Name == name {
			return item.Status
		}
	}
	return verdictFail
}

func worstStatus(current, next string) string {
	order := map[string]int{verdictPass: 0, verdictWarn: 1, verdictFail: 2}
	if order[next] > order[current] {
		return next
	}
	return current
}

func looksLikeSignalFactType(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) < 2 {
		return false
	}
	if parts[0] != "risk" && parts[0] != "opportunity" {
		return false
	}
	return strings.Contains(parts[1], "/") || (len(parts) > 2 && strings.Contains(parts[1]+":"+parts[2], "/"))
}

func propagationLooksDecayed(paths []horizonmodels.TracePath) bool {
	for _, path := range paths {
		if len(path.Amplitudes) < 2 {
			continue
		}
		last := path.Amplitudes[0]
		consistent := true
		for _, amp := range path.Amplitudes[1:] {
			if amp > last+0.05 {
				consistent = false
				break
			}
			last = amp
		}
		if consistent {
			return true
		}
	}
	return false
}

func propagationSelfOnly(paths []horizonmodels.TracePath) bool {
	if len(paths) == 0 {
		return true
	}
	for _, path := range paths {
		if len(path.NodeCanonicalPaths) > 1 {
			return false
		}
	}
	return true
}

func summarizeGrounding(runs []*horizonmodels.ScopeAgentRun, proposals []*ProposalSummary, subject *horizonmodels.WatchSubject, cfg *horizonmodels.ScopeAgentConfig) *GroundingSummary {
	out := &GroundingSummary{}
	if len(runs) > 0 && runs[0] != nil {
		run := runs[0]
		out.Model = firstNonEmpty(stringValue(run.Trace["model"]), run.PrimaryModel, run.EscalationModel)
		out.CitationsCount = intValue(run.Trace["citations"])
		out.WebURLs = parseAnyStringSlice(run.Trace["webUrls"])
		out.DatastoreFragmentIDs = parseAnyStringSlice(run.Trace["datastoreFragmentIds"])
		out.HasWebResults = boolValue(run.Trace["hasWebResults"]) || len(out.WebURLs) > 0
		out.HasDatastoreResults = boolValue(run.Trace["hasDatastoreResults"]) || len(out.DatastoreFragmentIDs) > 0
	}
	for _, proposal := range proposals {
		if proposal == nil || proposal.Payload == nil {
			continue
		}
		sourceBundle := mapFromAny(proposal.Payload["sourceBundle"])
		if len(sourceBundle) == 0 {
			continue
		}
		if out.SearchWindow == nil {
			out.SearchWindow = mapFromAny(sourceBundle["searchWindow"])
		}
		if len(out.QueryFamilies) == 0 {
			rawFamilies := sourceBundle["queryFamilies"]
			if typed, ok := rawFamilies.([]any); ok {
				out.QueryFamilies = append([]any(nil), typed...)
			}
		}
	}
	if out.SearchWindow == nil {
		if subject != nil {
			if policy := mapFromAny(subject.Metadata["timeWindowPolicy"]); len(policy) > 0 {
				out.SearchWindow = policy
			}
		}
		if out.SearchWindow == nil && cfg != nil {
			if policy := mapFromAny(cfg.Metadata["sourceMixPolicy"]); len(policy) > 0 {
				out.SearchWindow = policy
			}
		}
	}
	return out
}

func collectReportRunIDs(reports any) []string {
	value := reflect.ValueOf(reports)
	if value.Kind() != reflect.Slice {
		return nil
	}
	out := []string{}
	for i := 0; i < value.Len(); i++ {
		item := value.Index(i)
		if item.Kind() == reflect.Pointer {
			item = item.Elem()
		}
		if !item.IsValid() {
			continue
		}
		batchField := item.FieldByName("Batch")
		if !batchField.IsValid() {
			continue
		}
		runIDs := batchField.FieldByName("RunIDs")
		out = append(out, parseReflectStringSlice(runIDs)...)
	}
	return compactUnique(out)
}

func parseReflectStringSlice(value reflect.Value) []string {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Slice {
		return nil
	}
	out := make([]string, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		item := value.Index(i)
		if item.Kind() == reflect.String {
			out = append(out, strings.TrimSpace(item.String()))
		}
	}
	return compactUnique(out)
}

func syntheticSeedEvidence(item *AcceptedSignalSummary) []horizonmodels.LeafSignalEvidence {
	if item == nil || item.Fact == nil {
		return nil
	}
	out := make([]horizonmodels.LeafSignalEvidence, 0, len(item.SeedFragmentIDs))
	for _, seedID := range item.SeedFragmentIDs {
		seedID = strings.TrimSpace(seedID)
		if seedID == "" {
			continue
		}
		out = append(out, horizonmodels.LeafSignalEvidence{
			FragmentID:    seedID,
			CanonicalPath: strings.TrimSpace(item.CanonicalPath),
			Name:          firstNonEmpty(item.Fact.Title, item.CanonicalPath),
			SignalType:    firstNonEmpty(item.Fact.Type, "signal"),
			Axis:          signalAxis(item.Signal),
			Amplitude:     item.Signal.Amplitude,
			Modality:      stringValue(item.Signal.Constraints.Modality),
			ObservedAt:    observedAt(item.Fact),
			Summary:       firstNonEmpty(item.Fact.Summary, item.Fact.Title),
		})
	}
	return out
}

func signalAxis(signal *horizonmodels.SignalDescriptor) string {
	if signal == nil {
		return ""
	}
	return strings.TrimSpace(signal.Axis)
}

func observedAt(fact *coremodels.OntologyFact) time.Time {
	if fact == nil {
		return time.Time{}
	}
	if fact.ObservedAt != nil && !fact.ObservedAt.IsZero() {
		return *fact.ObservedAt
	}
	return fact.UpdatedAt
}

func firstCanonicalPath(paths []string) string {
	for _, path := range paths {
		if path = strings.TrimSpace(path); path != "" {
			return path
		}
	}
	return ""
}

func sortAcceptedSignalsForAnalysis(items []*AcceptedSignalSummary) {
	sort.SliceStable(items, func(i, j int) bool {
		left := acceptedSignalPriority(items[i])
		right := acceptedSignalPriority(items[j])
		if left != right {
			return left > right
		}
		leftAmp := 0.0
		rightAmp := 0.0
		if items[i] != nil && items[i].Signal != nil {
			leftAmp = items[i].Signal.Amplitude
		}
		if items[j] != nil && items[j].Signal != nil {
			rightAmp = items[j].Signal.Amplitude
		}
		if leftAmp != rightAmp {
			return leftAmp > rightAmp
		}
		leftAt := time.Time{}
		rightAt := time.Time{}
		if items[i] != nil && items[i].Fact != nil {
			leftAt = observedAt(items[i].Fact)
		}
		if items[j] != nil && items[j].Fact != nil {
			rightAt = observedAt(items[j].Fact)
		}
		return leftAt.After(rightAt)
	})
}

func sortAcceptedSignalsForScan(items []*AcceptedSignalSummary) {
	sort.SliceStable(items, func(i, j int) bool {
		left := acceptedSignalScanPriority(items[i])
		right := acceptedSignalScanPriority(items[j])
		if left != right {
			return left > right
		}
		leftAmp := 0.0
		rightAmp := 0.0
		if items[i] != nil && items[i].Signal != nil {
			leftAmp = items[i].Signal.Amplitude
		}
		if items[j] != nil && items[j].Signal != nil {
			rightAmp = items[j].Signal.Amplitude
		}
		if leftAmp != rightAmp {
			return leftAmp > rightAmp
		}
		leftAt := time.Time{}
		rightAt := time.Time{}
		if items[i] != nil && items[i].Fact != nil {
			leftAt = observedAt(items[i].Fact)
		}
		if items[j] != nil && items[j].Fact != nil {
			rightAt = observedAt(items[j].Fact)
		}
		return leftAt.After(rightAt)
	})
}

func acceptedSignalPriority(item *AcceptedSignalSummary) int {
	if item == nil || item.Signal == nil {
		return 0
	}
	switch strings.TrimSpace(item.Signal.Axis) {
	case horizonmodels.AxisPhysicalFlowSupply:
		return 100
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		return 95
	case horizonmodels.AxisPhysicalFlowDemand:
		return 90
	case horizonmodels.AxisOperationalContinuity:
		return 85
	case horizonmodels.AxisGeoConflict:
		return 80
	case horizonmodels.AxisFinancialSettlement, horizonmodels.AxisFinancialCredit, horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance:
		return 70
	case horizonmodels.AxisInfoReputation:
		return 55
	case horizonmodels.AxisInfoUncertainty:
		return 40
	default:
		return 30
	}
}

func acceptedSignalScanPriority(item *AcceptedSignalSummary) int {
	if item == nil || item.Signal == nil {
		return 0
	}
	base := 0
	switch strings.TrimSpace(item.Signal.Axis) {
	case horizonmodels.AxisGeoConflict:
		base = 100
	case horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance:
		base = 95
	case horizonmodels.AxisFinancialSettlement, horizonmodels.AxisFinancialCredit:
		base = 90
	case horizonmodels.AxisOperationalContinuity:
		base = 85
	case horizonmodels.AxisInfoUncertainty, horizonmodels.AxisInfoReputation:
		base = 80
	case horizonmodels.AxisPhysicalFlowDemand:
		base = 70
	case horizonmodels.AxisPhysicalFlowSupply:
		base = 45
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency:
		base = 40
	default:
		base = 60
	}
	seedPenalty := 0
	if n := len(compactUnique(item.SeedFragmentIDs)); n > 0 {
		switch {
		case n > 24:
			seedPenalty = 20
		case n > 12:
			seedPenalty = 10
		case n > 6:
			seedPenalty = 4
		}
	}
	return base - seedPenalty
}

func mapFromAny(raw any) map[string]any {
	if typed, ok := raw.(map[string]any); ok {
		return typed
	}
	return nil
}

func intValue(raw any) int {
	switch typed := raw.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func boolValue(raw any) bool {
	typed, _ := raw.(bool)
	return typed
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
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

func valuesOrEmpty[T any](item *GroundingSummary, fn func(*GroundingSummary) []T) []T {
	if item == nil {
		return nil
	}
	return fn(item)
}
