package analytical

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type AggregateScopeInput struct {
	TenantID        string
	ScopeType       string
	ScopeRef        string
	Layer           string
	SourceClass     string
	SubjectRef      string
	LensRefs        []string
	WatchSubjectIDs []string
	RefreshClass    string
	Primitives      []*hmodels.AnalyticalPrimitive
	ComputedAt      time.Time
}

type RefRefreshResult struct {
	ChangedRefs            []string
	UpdatedPrimitiveCount  int
	UpdatedFragmentScopes  []string
	UpdatedLensIDs         []string
	UpdatedWatchSubjectIDs []string
}

func BuildScopeAggregates(in AggregateScopeInput) []*hmodels.AnalyticalAggregate {
	if len(in.Primitives) == 0 {
		return nil
	}
	weakPoints := buildWeakPointsAggregate(in, in.Primitives)
	if weakPoints == nil {
		return nil
	}
	return []*hmodels.AnalyticalAggregate{weakPoints}
}

func PersistScopeAggregates(ctx context.Context, repo *hrepo.AnalyticalAggregatesRepository, in AggregateScopeInput) ([]*hmodels.AnalyticalAggregate, error) {
	if repo == nil {
		return nil, nil
	}
	aggregates := BuildScopeAggregates(in)
	for _, aggregate := range aggregates {
		if aggregate == nil {
			continue
		}
		if err := repo.Upsert(ctx, aggregate); err != nil {
			return aggregates, err
		}
	}
	return aggregates, nil
}

func EnsureLensAggregate(
	ctx context.Context,
	primitivesRepo *hrepo.AnalyticalPrimitivesRepository,
	aggregatesRepo *hrepo.AnalyticalAggregatesRepository,
	materializedRepo *hrepo.MaterializedArtifactsRepository,
	lens *hmodels.Lens,
	scopeRefs []string,
	computedAt time.Time,
) (*hmodels.AnalyticalAggregate, error) {
	if primitivesRepo == nil || aggregatesRepo == nil || lens == nil {
		return nil, nil
	}
	existing, err := aggregatesRepo.ListByScopeRefs(ctx, strings.TrimSpace(lens.TenantID), []string{strings.TrimSpace(lens.ID)}, 8)
	if err == nil {
		for _, item := range existing {
			if item == nil || !strings.EqualFold(strings.TrimSpace(item.AggregateType), "weak_points_summary") {
				continue
			}
			if item.StaleAt == nil || item.StaleAt.After(time.Now().UTC()) {
				return item, nil
			}
		}
	}
	impactedRefs := lensAggregateRefs(scopeRefs)
	if materializedRepo != nil {
		if artifacts, listErr := materializedRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 240); listErr == nil {
			impactedRefs = append(impactedRefs, lensAggregateArtifactRefs(artifacts)...)
		}
	}
	primitives, err := primitivesRepo.ListByImpactedRefs(ctx, strings.TrimSpace(lens.TenantID), compactStrings(impactedRefs), 480)
	if err != nil || len(primitives) == 0 {
		return nil, err
	}
	aggregates, err := PersistScopeAggregates(ctx, aggregatesRepo, AggregateScopeInput{
		TenantID:     strings.TrimSpace(lens.TenantID),
		ScopeType:    "lens",
		ScopeRef:     strings.TrimSpace(lens.ID),
		Layer:        "baseline",
		SourceClass:  "on_read_lens_refresh",
		SubjectRef:   strings.TrimSpace(firstNonEmpty(lens.Name, lens.ID)),
		LensRefs:     compactStrings([]string{strings.TrimSpace(lens.ID)}),
		RefreshClass: "on_read",
		Primitives:   primitives,
		ComputedAt:   computedAt,
	})
	if err != nil || len(aggregates) == 0 {
		return nil, err
	}
	return aggregates[0], nil
}

func BuildSimulationRunAggregate(run *hmodels.SimulationRun, bundle *hmodels.SimulationBundle, computedAt time.Time) *hmodels.AnalyticalAggregate {
	if run == nil {
		return nil
	}
	now := computedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	basisRefs := compactStrings(append(append([]string{}, run.BaselineRefs.ScopePaths...), run.BaselineRefs.ArtifactKeys...))
	lensRefs := compactStrings([]string{strings.TrimSpace(firstNonEmpty(run.Request.LensID, run.BaselineRefs.LensID))})
	productRefs, affectedRefs := simulationOverlayRefs(bundle)
	severity := simulationScenarioSeverity(run, bundle)
	confidence := simulationScenarioConfidence(run, bundle)
	explanation := simulationScenarioExplanation(run, bundle)
	staleAt := run.ExpiresAt
	if staleAt.IsZero() {
		value := now.Add(72 * time.Hour)
		staleAt = value
	}
	aggregate := &hmodels.AnalyticalAggregate{
		TenantID:       strings.TrimSpace(run.TenantID),
		ScopeType:      "simulation_run",
		ScopeRef:       strings.TrimSpace(run.ID),
		AggregateType:  "disruption_impact_summary",
		Layer:          "scenario",
		SourceClass:    "simulation_bundle",
		SubjectRef:     strings.TrimSpace(firstNonEmpty(run.Request.Question, run.BaselineRefs.LensID)),
		LensRefs:       lensRefs,
		ProductRefs:    productRefs,
		AffectedRefs:   affectedRefs,
		BasisRefs:      basisRefs,
		BasisDigest:    aggregateDigest(strings.TrimSpace(run.ID), basisRefs, productRefs, affectedRefs, strings.TrimSpace(run.BaselineRefs.SnapshotDigest)),
		RefreshClass:   "scenario_only",
		SeverityScore:  severity,
		Confidence:     confidence,
		CoverageStatus: simulationCoverageStatus(run, bundle),
		GateStatus:     simulationGateStatus(run),
		Explanation:    explanation,
		MissingData:    simulationMissingData(run, bundle),
		ComputedAt:     now,
		StaleAt:        &staleAt,
	}
	ApplyAggregatePolicy(aggregate, now)
	aggregate.Normalize()
	return aggregate
}

func PersistSimulationRunAggregate(ctx context.Context, repo *hrepo.AnalyticalAggregatesRepository, run *hmodels.SimulationRun, bundle *hmodels.SimulationBundle, computedAt time.Time) (*hmodels.AnalyticalAggregate, error) {
	if repo == nil || run == nil {
		return nil, nil
	}
	aggregate := BuildSimulationRunAggregate(run, bundle, computedAt)
	if aggregate == nil {
		return nil, nil
	}
	if err := repo.Upsert(ctx, aggregate); err != nil {
		return nil, err
	}
	return aggregate, nil
}

func RefreshWatchSubjectAggregates(
	ctx context.Context,
	primitivesRepo *hrepo.AnalyticalPrimitivesRepository,
	aggregatesRepo *hrepo.AnalyticalAggregatesRepository,
	watchSubjectsRepo *hrepo.WatchSubjectsRepository,
	tenantID string,
	changedRefs []string,
	computedAt time.Time,
) error {
	if primitivesRepo == nil || aggregatesRepo == nil || watchSubjectsRepo == nil {
		return nil
	}
	subjects, err := impactedWatchSubjects(ctx, watchSubjectsRepo, tenantID, changedRefs)
	if err != nil {
		return err
	}
	for _, subject := range subjects {
		if subject == nil {
			continue
		}
		scopeRefs := watchSubjectScopeRefs(subject)
		if len(scopeRefs) == 0 {
			continue
		}
		primitives, err := primitivesRepo.ListByImpactedRefs(ctx, strings.TrimSpace(firstNonEmpty(subject.TenantID, tenantID, "global")), scopeRefs, 240)
		if err != nil {
			return err
		}
		_, err = PersistScopeAggregates(ctx, aggregatesRepo, AggregateScopeInput{
			TenantID:        strings.TrimSpace(firstNonEmpty(subject.TenantID, tenantID, "global")),
			ScopeType:       "watch_subject",
			ScopeRef:        strings.TrimSpace(subject.ID),
			Layer:           "baseline",
			SourceClass:     "watch_subject_refresh",
			SubjectRef:      strings.TrimSpace(subject.AnchorCanonicalPath),
			LensRefs:        watchSubjectLensRefs(subject),
			WatchSubjectIDs: []string{strings.TrimSpace(subject.ID)},
			RefreshClass:    refreshClassForWatchSubject(subject),
			Primitives:      primitives,
			ComputedAt:      computedAt,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func ListImpactedWatchSubjects(
	ctx context.Context,
	repo *hrepo.WatchSubjectsRepository,
	tenantID string,
	refs []string,
) ([]*hmodels.WatchSubject, error) {
	return impactedWatchSubjects(ctx, repo, tenantID, refs)
}

func RefreshKnowledgeForRefs(
	ctx context.Context,
	fragmentsRepo *hrepo.HorizonFragmentsRepository,
	lensesRepo *hrepo.LensesRepository,
	materializedRepo *hrepo.MaterializedArtifactsRepository,
	watchSubjectsRepo *hrepo.WatchSubjectsRepository,
	primitivesRepo *hrepo.AnalyticalPrimitivesRepository,
	aggregatesRepo *hrepo.AnalyticalAggregatesRepository,
	tenantID string,
	changedRefs []string,
	computedAt time.Time,
) (*RefRefreshResult, error) {
	if primitivesRepo == nil || aggregatesRepo == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	changedRefs = compactStrings(changedRefs)
	if len(changedRefs) == 0 {
		return &RefRefreshResult{}, nil
	}
	now := computedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := &RefRefreshResult{
		ChangedRefs: changedRefs,
	}
	for _, ref := range changedRefs {
		if fragmentsRepo != nil {
			fragment, err := fragmentsRepo.GetByCanonicalPath(ctx, ref)
			if err == nil && fragment != nil {
				primitives := BuildFragmentStatePrimitives(fragment, now)
				for _, primitive := range primitives {
					if primitive == nil {
						continue
					}
					if err := primitivesRepo.Upsert(ctx, primitive); err != nil {
						return result, err
					}
					result.UpdatedPrimitiveCount++
				}
			}
		}
		primitives, err := primitivesRepo.ListByImpactedRefs(ctx, tenantID, []string{ref}, 160)
		if err != nil {
			return result, err
		}
		if len(primitives) > 0 {
			if _, err := PersistScopeAggregates(ctx, aggregatesRepo, AggregateScopeInput{
				TenantID:     tenantID,
				ScopeType:    "fragment",
				ScopeRef:     ref,
				Layer:        "baseline",
				SourceClass:  "fragment_refresh",
				SubjectRef:   ref,
				RefreshClass: "metric_driven",
				Primitives:   primitives,
				ComputedAt:   now,
			}); err != nil {
				return result, err
			}
			result.UpdatedFragmentScopes = append(result.UpdatedFragmentScopes, ref)
		}
	}
	if watchSubjectsRepo != nil {
		if err := RefreshWatchSubjectAggregates(ctx, primitivesRepo, aggregatesRepo, watchSubjectsRepo, tenantID, changedRefs, now); err != nil {
			return result, err
		}
		subjectSet := make(map[string]struct{})
		for _, ref := range changedRefs {
			subjects, err := listWatchSubjectsByAnchor(ctx, watchSubjectsRepo, tenantID, ref, 40)
			if err != nil {
				return result, err
			}
			for _, subject := range subjects {
				if subject == nil || strings.TrimSpace(subject.ID) == "" {
					continue
				}
				subjectSet[strings.TrimSpace(subject.ID)] = struct{}{}
			}
		}
		result.UpdatedWatchSubjectIDs = sortedKeysAnalytical(subjectSet)
	}
	if lensesRepo != nil {
		lensSet := make(map[string]*hmodels.Lens)
		for _, ref := range changedRefs {
			lenses, err := findLensesByScope(ctx, lensesRepo, tenantID, ref, 40)
			if err != nil {
				return result, err
			}
			for _, lens := range lenses {
				if lens == nil || strings.TrimSpace(lens.ID) == "" {
					continue
				}
				lensSet[strings.TrimSpace(lens.ID)] = lens
			}
			if materializedRepo != nil {
				artifacts, err := materializedRepo.FindByNodeRefAnyTenant(ctx, ref, hmodels.MaterializedKindSupplyChainPath, 200)
				if err != nil {
					return result, err
				}
				for _, artifact := range artifacts {
					if artifact == nil || strings.TrimSpace(artifact.LensID) == "" {
						continue
					}
					if _, ok := lensSet[strings.TrimSpace(artifact.LensID)]; ok {
						continue
					}
					lens, err := lensesRepo.Get(ctx, strings.TrimSpace(artifact.LensID))
					if err != nil || lens == nil {
						continue
					}
					lensSet[strings.TrimSpace(lens.ID)] = lens
				}
			}
		}
		for _, lensID := range sortedLensKeys(lensSet) {
			lens := lensSet[lensID]
			if lens == nil {
				continue
			}
			impactedRefs := lensAggregateRefs(lens.Scope)
			if materializedRepo != nil {
				if artifacts, listErr := materializedRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), 240); listErr == nil {
					impactedRefs = append(impactedRefs, lensAggregateArtifactRefs(artifacts)...)
				}
			}
			primitives, err := primitivesRepo.ListByImpactedRefs(ctx, strings.TrimSpace(firstNonEmpty(lens.TenantID, tenantID, "global")), compactStrings(impactedRefs), 480)
			if err != nil {
				return result, err
			}
			if len(primitives) == 0 {
				continue
			}
			if _, err := PersistScopeAggregates(ctx, aggregatesRepo, AggregateScopeInput{
				TenantID:     strings.TrimSpace(firstNonEmpty(lens.TenantID, tenantID, "global")),
				ScopeType:    "lens",
				ScopeRef:     strings.TrimSpace(lens.ID),
				Layer:        "baseline",
				SourceClass:  "fragment_refresh",
				SubjectRef:   strings.TrimSpace(firstNonEmpty(lens.Name, lens.ID)),
				LensRefs:     []string{strings.TrimSpace(lens.ID)},
				RefreshClass: "metric_driven",
				Primitives:   primitives,
				ComputedAt:   now,
			}); err != nil {
				return result, err
			}
			result.UpdatedLensIDs = append(result.UpdatedLensIDs, strings.TrimSpace(lens.ID))
		}
	}
	result.UpdatedFragmentScopes = compactStrings(result.UpdatedFragmentScopes)
	result.UpdatedLensIDs = compactStrings(result.UpdatedLensIDs)
	result.UpdatedWatchSubjectIDs = compactStrings(result.UpdatedWatchSubjectIDs)
	return result, nil
}

func buildWeakPointsAggregate(in AggregateScopeInput, primitives []*hmodels.AnalyticalPrimitive) *hmodels.AnalyticalAggregate {
	now := in.ComputedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	sort.Slice(primitives, func(i, j int) bool {
		if primitives[i].SeverityScore == primitives[j].SeverityScore {
			return primitives[i].UpdatedAt.After(primitives[j].UpdatedAt)
		}
		return primitives[i].SeverityScore > primitives[j].SeverityScore
	})
	top := make([]*hmodels.AnalyticalPrimitive, 0, minAggregate(len(primitives), 5))
	for _, primitive := range primitives {
		if primitive == nil {
			continue
		}
		top = append(top, primitive)
		if len(top) >= 5 {
			break
		}
	}
	if len(top) == 0 {
		return nil
	}
	basisRefs := map[string]struct{}{}
	productRefs := map[string]struct{}{}
	nodeRefs := map[string]struct{}{}
	routeRefs := map[string]struct{}{}
	zoneRefs := map[string]struct{}{}
	affectedRefs := map[string]struct{}{}
	primitiveTypes := map[string]struct{}{}
	primitiveIDs := make([]string, 0, len(primitives))
	missingData := map[string]struct{}{}
	severity := 0.0
	confidenceTotal := 0.0
	confidenceCount := 0.0
	coverageStatus := ""
	gateStatus := ""
	staleAt := (*time.Time)(nil)
	for _, primitive := range primitives {
		if primitive == nil {
			continue
		}
		if strings.TrimSpace(primitive.ID) != "" {
			primitiveIDs = append(primitiveIDs, strings.TrimSpace(primitive.ID))
		}
		primitiveTypes[strings.TrimSpace(primitive.PrimitiveType)] = struct{}{}
		severity = maxFloatAnalytical(severity, primitive.SeverityScore)
		if primitive.Confidence > 0 {
			confidenceTotal += primitive.Confidence
			confidenceCount++
		}
		if coverageStatus == "" || analyticalCoverageRank(primitive.CoverageStatus) > analyticalCoverageRank(coverageStatus) {
			coverageStatus = strings.TrimSpace(primitive.CoverageStatus)
		}
		if gateStatus == "" || analyticalGateRank(primitive.GateStatus) > analyticalGateRank(gateStatus) {
			gateStatus = strings.TrimSpace(primitive.GateStatus)
		}
		if primitive.StaleAt != nil && (staleAt == nil || primitive.StaleAt.Before(*staleAt)) {
			value := primitive.StaleAt.UTC()
			staleAt = &value
		}
		for _, ref := range primitive.BasisRefs {
			basisRefs[strings.TrimSpace(ref)] = struct{}{}
		}
		for _, ref := range primitive.ProductRefs {
			productRefs[strings.TrimSpace(ref)] = struct{}{}
		}
		for _, ref := range primitive.NodeRefs {
			nodeRefs[strings.TrimSpace(ref)] = struct{}{}
		}
		for _, ref := range primitive.RouteRefs {
			routeRefs[strings.TrimSpace(ref)] = struct{}{}
		}
		for _, ref := range primitive.ZoneRefs {
			zoneRefs[strings.TrimSpace(ref)] = struct{}{}
		}
		for _, ref := range primitive.AffectedRefs {
			affectedRefs[strings.TrimSpace(ref)] = struct{}{}
		}
		for _, marker := range primitive.MissingData {
			missingData[strings.TrimSpace(marker)] = struct{}{}
		}
	}
	explanation := renderWeakPointsExplanation(top)
	confidence := 0.0
	if confidenceCount > 0 {
		confidence = confidenceTotal / confidenceCount
	}
	aggregate := &hmodels.AnalyticalAggregate{
		TenantID:            strings.TrimSpace(in.TenantID),
		ScopeType:           strings.TrimSpace(in.ScopeType),
		ScopeRef:            strings.TrimSpace(in.ScopeRef),
		AggregateType:       "weak_points_summary",
		Layer:               strings.TrimSpace(firstNonEmpty(in.Layer, "baseline")),
		SourceClass:         strings.TrimSpace(firstNonEmpty(in.SourceClass, "aggregate_refresh")),
		SubjectRef:          strings.TrimSpace(in.SubjectRef),
		LensRefs:            compactStrings(in.LensRefs),
		WatchSubjectIDs:     compactStrings(in.WatchSubjectIDs),
		ProductRefs:         setKeys(productRefs),
		NodeRefs:            setKeys(nodeRefs),
		RouteRefs:           setKeys(routeRefs),
		ZoneRefs:            setKeys(zoneRefs),
		AffectedRefs:        setKeys(affectedRefs),
		BasisRefs:           setKeys(basisRefs),
		DerivedPrimitiveIDs: compactStrings(primitiveIDs),
		PrimitiveTypes:      setKeys(primitiveTypes),
		PrimitiveCount:      len(primitives),
		BasisDigest:         aggregateDigest(in.ScopeRef, setKeys(basisRefs), compactStrings(primitiveIDs), setKeys(primitiveTypes), strconv.FormatFloat(severity, 'f', 4, 64)),
		RefreshClass:        strings.TrimSpace(firstNonEmpty(in.RefreshClass, "immediate")),
		SeverityScore:       clamp01(severity),
		Confidence:          clamp01(confidence),
		CoverageStatus:      coverageStatus,
		GateStatus:          gateStatus,
		Explanation:         explanation,
		MissingData:         setKeys(missingData),
		ComputedAt:          now,
		StaleAt:             staleAt,
	}
	ApplyAggregatePolicy(aggregate, now)
	aggregate.Normalize()
	return aggregate
}

func impactedWatchSubjects(ctx context.Context, repo *hrepo.WatchSubjectsRepository, tenantID string, refs []string) ([]*hmodels.WatchSubject, error) {
	refs = compactStrings(refs)
	if repo == nil || len(refs) == 0 {
		return nil, nil
	}
	seen := make(map[string]*hmodels.WatchSubject)
	for _, ref := range refs {
		items, err := listWatchSubjectsByAnchor(ctx, repo, tenantID, ref, 50)
		if err != nil {
			return nil, err
		}
		for _, subject := range items {
			if subject != nil && strings.TrimSpace(subject.ID) != "" {
				seen[strings.TrimSpace(subject.ID)] = subject
			}
		}
		items, err = listWatchSubjectsByUniverse(ctx, repo, tenantID, ref, 50)
		if err != nil {
			return nil, err
		}
		for _, subject := range items {
			if subject != nil && strings.TrimSpace(subject.ID) != "" {
				seen[strings.TrimSpace(subject.ID)] = subject
			}
		}
	}
	out := make([]*hmodels.WatchSubject, 0, len(seen))
	for _, subject := range seen {
		out = append(out, subject)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func listWatchSubjectsByAnchor(ctx context.Context, repo *hrepo.WatchSubjectsRepository, tenantID, ref string, limit int) ([]*hmodels.WatchSubject, error) {
	if repo == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	if tenantID == "global" {
		return repo.ListByAnchorCanonicalPathAnyTenant(ctx, ref, limit)
	}
	return repo.ListByAnchorCanonicalPath(ctx, tenantID, ref, limit)
}

func listWatchSubjectsByUniverse(ctx context.Context, repo *hrepo.WatchSubjectsRepository, tenantID, ref string, limit int) ([]*hmodels.WatchSubject, error) {
	if repo == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	if tenantID == "global" {
		return repo.ListByUniversePathAnyTenant(ctx, ref, limit)
	}
	return repo.ListByUniversePath(ctx, tenantID, ref, limit)
}

func findLensesByScope(ctx context.Context, repo *hrepo.LensesRepository, tenantID, ref string, limit int) ([]*hmodels.Lens, error) {
	if repo == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	if tenantID == "global" {
		return repo.FindByScopeAnyTenant(ctx, ref, limit)
	}
	return repo.FindByScope(ctx, tenantID, ref, limit)
}

func watchSubjectScopeRefs(subject *hmodels.WatchSubject) []string {
	if subject == nil {
		return nil
	}
	refs := []string{strings.TrimSpace(subject.AnchorCanonicalPath)}
	refs = append(refs, subject.UniversePaths...)
	return compactStrings(refs)
}

func watchSubjectLensRefs(subject *hmodels.WatchSubject) []string {
	if subject == nil {
		return nil
	}
	refs := append([]string{strings.TrimSpace(subject.LensID)}, subject.LensBindings...)
	return compactStrings(refs)
}

func refreshClassForWatchSubject(subject *hmodels.WatchSubject) string {
	if subject == nil {
		return "scheduled"
	}
	switch subject.RuntimeMode {
	case hmodels.WatchSubjectRuntimeRiskOnly, hmodels.WatchSubjectRuntimeGeneralized:
		return "signal_driven"
	default:
		return "scheduled"
	}
}

func renderWeakPointsExplanation(primitives []*hmodels.AnalyticalPrimitive) string {
	lines := make([]string, 0, len(primitives))
	for _, primitive := range primitives {
		if primitive == nil {
			continue
		}
		label := strings.TrimSpace(primitive.PrimitiveType)
		subject := strings.TrimSpace(firstNonEmpty(primitive.SubjectRef, primitive.ScopeRef))
		if label == "" || subject == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s on %s (severity %s)", label, subject, strconv.FormatFloat(clamp01(primitive.SeverityScore), 'f', 2, 64)))
		if len(lines) >= 3 {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Top weak points: " + strings.Join(lines, "; ")
}

func aggregateDigest(parts ...interface{}) string {
	flat := make([]string, 0, len(parts))
	for _, part := range parts {
		switch value := part.(type) {
		case string:
			flat = append(flat, strings.TrimSpace(value))
		case []string:
			flat = append(flat, compactStrings(value)...)
		}
	}
	sum := sha1.Sum([]byte(strings.Join(compactStrings(flat), "\n")))
	return hex.EncodeToString(sum[:10])
}

func setKeys(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func analyticalCoverageRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "missing":
		return 4
	case "partial":
		return 3
	case "estimated":
		return 2
	case "complete":
		return 1
	default:
		return 0
	}
}

func analyticalGateRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "blocked":
		return 3
	case "degraded":
		return 2
	case "ready":
		return 1
	default:
		return 0
	}
}

func maxFloatAnalytical(left, right float64) float64 {
	if right > left {
		return right
	}
	return left
}

func sortedKeysAnalytical(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for key := range values {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(key))
	}
	sort.Strings(out)
	return out
}

func sortedLensKeys(values map[string]*hmodels.Lens) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for key := range values {
		if strings.TrimSpace(key) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(key))
	}
	sort.Strings(out)
	return out
}

func lensAggregateRefs(scopeRefs []string) []string {
	return compactStrings(scopeRefs)
}

func lensAggregateArtifactRefs(artifacts []*hmodels.MaterializedArtifact) []string {
	if len(artifacts) == 0 {
		return nil
	}
	refs := make([]string, 0, len(artifacts)*8)
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		refs = append(refs, strings.TrimSpace(artifact.CanonicalKey))
		refs = append(refs, strings.TrimSpace(artifact.ProductAxis))
		refs = append(refs, strings.TrimSpace(artifact.AnchorSupplier))
		refs = append(refs, strings.TrimSpace(artifact.AnchorCustomer))
		refs = append(refs, artifact.NodeRefs...)
		refs = append(refs, artifact.FlowRefs...)
		refs = append(refs, artifact.ZoneRefs...)
		refs = append(refs, artifact.AffectedZoneRefs...)
	}
	return compactStrings(refs)
}

func minAggregate(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func simulationOverlayRefs(bundle *hmodels.SimulationBundle) ([]string, []string) {
	if bundle == nil || len(bundle.ResultOverlays) == 0 {
		return nil, nil
	}
	productRefs := make([]string, 0, 8)
	affectedRefs := make([]string, 0, 24)
	for _, item := range mapSlice(bundle.ResultOverlays["products"]) {
		productRefs = append(productRefs, strings.TrimSpace(stringValue(item, "path")))
		affectedRefs = append(affectedRefs, stringSliceValue(item["targetRefs"])...)
	}
	for _, section := range []string{"shoulders", "pathChains", "cascades", "roles"} {
		for _, item := range mapSlice(bundle.ResultOverlays[section]) {
			affectedRefs = append(affectedRefs, stringSliceValue(item["targetRefs"])...)
			affectedRefs = append(affectedRefs, stringSliceValue(item["refs"])...)
		}
	}
	return compactStrings(productRefs), compactStrings(affectedRefs)
}

func simulationScenarioSeverity(run *hmodels.SimulationRun, bundle *hmodels.SimulationBundle) float64 {
	severity := 0.0
	if run != nil {
		for _, value := range run.Summary.OutcomeDistribution {
			severity = maxFloatAnalytical(severity, value)
		}
	}
	if bundle != nil {
		for _, value := range bundle.OutcomeDistribution {
			severity = maxFloatAnalytical(severity, value)
		}
		for _, section := range []string{"shoulders", "products", "pathChains", "cascades", "roles"} {
			for _, item := range mapSlice(bundle.ResultOverlays[section]) {
				severity = maxFloatAnalytical(severity, numberValue(item, "score"))
				severity = maxFloatAnalytical(severity, numberValue(item, "riskScore"))
			}
		}
	}
	return clamp01(severity)
}

func simulationScenarioConfidence(run *hmodels.SimulationRun, bundle *hmodels.SimulationBundle) float64 {
	if bundle != nil && bundle.GroundingCoverage > 0 {
		return clamp01(bundle.GroundingCoverage)
	}
	if run != nil && run.Review.GroundingCoverage > 0 {
		return clamp01(run.Review.GroundingCoverage)
	}
	if run != nil && run.Summary.GroundingCoverage > 0 {
		return clamp01(run.Summary.GroundingCoverage)
	}
	return 0.55
}

func simulationScenarioExplanation(run *hmodels.SimulationRun, bundle *hmodels.SimulationBundle) string {
	parts := make([]string, 0, 4)
	if run != nil && strings.TrimSpace(run.Request.Question) != "" {
		parts = append(parts, "Scenario: "+strings.TrimSpace(run.Request.Question))
	}
	mechanisms := []string(nil)
	if bundle != nil && len(bundle.KeyMechanisms) > 0 {
		mechanisms = append(mechanisms, bundle.KeyMechanisms...)
	} else if run != nil && len(run.Summary.KeyMechanisms) > 0 {
		mechanisms = append(mechanisms, run.Summary.KeyMechanisms...)
	}
	if len(mechanisms) > 0 {
		parts = append(parts, "Key mechanisms: "+strings.Join(compactStrings(mechanisms[:minAggregate(len(mechanisms), 4)]), ", "))
	}
	if bundle != nil && len(bundle.ExecutiveNarrative) > 0 {
		parts = append(parts, strings.TrimSpace(bundle.ExecutiveNarrative[0]))
	}
	return strings.Join(compactStrings(parts), " ")
}

func simulationCoverageStatus(run *hmodels.SimulationRun, bundle *hmodels.SimulationBundle) string {
	conf := simulationScenarioConfidence(run, bundle)
	switch {
	case conf >= 0.75:
		return "complete"
	case conf >= 0.45:
		return "partial"
	default:
		return "missing"
	}
}

func simulationGateStatus(run *hmodels.SimulationRun) string {
	if run == nil {
		return ""
	}
	switch strings.ToUpper(strings.TrimSpace(run.Status)) {
	case string(hmodels.SimulationRunStatusSucceeded), string(hmodels.SimulationRunStatusDegraded):
		return "ready"
	case string(hmodels.SimulationRunStatusFailed):
		return "blocked"
	default:
		return "degraded"
	}
}

func simulationMissingData(run *hmodels.SimulationRun, bundle *hmodels.SimulationBundle) []string {
	out := make([]string, 0, 4)
	if bundle == nil {
		out = append(out, "missing:simulation_bundle")
	}
	if simulationScenarioConfidence(run, bundle) < 0.45 {
		out = append(out, "missing:grounding_coverage")
	}
	if bundle != nil && len(bundle.ResultOverlays) == 0 {
		out = append(out, "missing:result_overlays")
	}
	return compactStrings(out)
}

func mapSlice(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func stringValue(item map[string]any, key string) string {
	if item == nil {
		return ""
	}
	value, _ := item[key].(string)
	return strings.TrimSpace(value)
}

func stringSliceValue(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return compactStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok {
				out = append(out, strings.TrimSpace(value))
			}
		}
		return compactStrings(out)
	default:
		return nil
	}
}

func numberValue(item map[string]any, key string) float64 {
	if item == nil {
		return 0
	}
	switch value := item[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}
