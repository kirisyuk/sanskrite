package signals

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	redis "github.com/redis/go-redis/v9"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
)

type ApplicabilityDecision struct {
	Applicable        bool     `json:"applicable"`
	Score             float64  `json:"score"`
	Reasons           []string `json:"reasons,omitempty"`
	ConfidencePenalty float64  `json:"confidencePenalty,omitempty"` // [0..1], 1 means no penalty
}

type ApplicabilityEvaluator interface {
	Evaluate(ctx context.Context, signal *horizonmodels.SignalDescriptor, fragment *horizonmodels.HorizonFragment) ApplicabilityDecision
}

// ExternalApplicabilityEvaluator determines whether an external signal applies to a fragment.
// It intentionally ignores internal operational telemetry and relies on external exposure features.
type ExternalApplicabilityEvaluator struct {
	minScore          float64
	maxExposureHops   int
	fragmentsRepo     *repo.HorizonFragmentsRepository
	edgesRepo         *repo.DependencyEdgesRepository
	exposurePathsRepo *repo.ExposurePathsRepository

	mu         sync.RWMutex
	lensCached map[string]cachedExposureList
	pathCached map[string]bool
	adjCached  map[string][]string
	redis      *redis.Client
	redisTTL   time.Duration
}

type cachedExposureList struct {
	items []*horizonmodels.ExposurePathArtifact
	at    time.Time
}

func NewExternalApplicabilityEvaluator() *ExternalApplicabilityEvaluator {
	e := &ExternalApplicabilityEvaluator{
		minScore:        0.35,
		maxExposureHops: 2,
		lensCached:      make(map[string]cachedExposureList),
		pathCached:      make(map[string]bool),
		adjCached:       make(map[string][]string),
		redisTTL:        15 * time.Minute,
	}
	e.redis = newRedisApplicabilityClient()
	return e
}

func (e *ExternalApplicabilityEvaluator) WithFragmentsRepo(r *repo.HorizonFragmentsRepository) *ExternalApplicabilityEvaluator {
	e.fragmentsRepo = r
	return e
}

func (e *ExternalApplicabilityEvaluator) WithEdgesRepo(r *repo.DependencyEdgesRepository) *ExternalApplicabilityEvaluator {
	e.edgesRepo = r
	return e
}

func (e *ExternalApplicabilityEvaluator) WithExposurePathsRepo(r *repo.ExposurePathsRepository) *ExternalApplicabilityEvaluator {
	e.exposurePathsRepo = r
	return e
}

func (e *ExternalApplicabilityEvaluator) WithMaxExposureHops(hops int) *ExternalApplicabilityEvaluator {
	if hops > 0 {
		e.maxExposureHops = hops
	}
	return e
}

func (e *ExternalApplicabilityEvaluator) Evaluate(ctx context.Context, signal *horizonmodels.SignalDescriptor, fragment *horizonmodels.HorizonFragment) ApplicabilityDecision {
	if signal == nil || fragment == nil {
		return ApplicabilityDecision{Applicable: false, Score: 0}
	}
	score := 0.0
	reasons := make([]string, 0, 6)

	tScore, tReason := axisTypeFit(signal.Axis, fragment.Type)
	score += tScore * 0.6
	if tReason != "" {
		reasons = append(reasons, tReason)
	}

	lScore, lReason := locationFit(signal.LocationPath, fragment)
	score += lScore * 0.3
	if lReason != "" {
		reasons = append(reasons, lReason)
	}

	sScore, sReason := sphereFit(signal.SpherePaths, fragment)
	score += sScore * 0.1
	if sReason != "" {
		reasons = append(reasons, sReason)
	}

	if score > 1 {
		score = 1
	}
	decision := ApplicabilityDecision{
		Applicable:        score >= e.minScore,
		Score:             score,
		Reasons:           reasons,
		ConfidencePenalty: 1.0,
	}
	if !decision.Applicable {
		return decision
	}

	pathOK := e.hasQualifyingExposurePath(ctx, signal, fragment)
	if pathOK {
		decision.Reasons = append(decision.Reasons, "exposure_path_gate_passed")
		return decision
	}

	if isHighStakesAxis(signal.Axis) {
		decision.Applicable = false
		decision.Reasons = append(decision.Reasons, "exposure_path_required_missing")
		return decision
	}

	decision.Score = decision.Score * 0.65
	decision.Applicable = decision.Score >= e.minScore
	decision.ConfidencePenalty = 0.65
	decision.Reasons = append(decision.Reasons, "confidence_penalty:sparse_graph", "uncertainty_uplift:sparse_graph")
	return decision
}

func isHighStakesAxis(axis string) bool {
	switch strings.TrimSpace(axis) {
	case horizonmodels.AxisLogisticsCapacity,
		horizonmodels.AxisLogisticsLatency,
		horizonmodels.AxisPhysicalFlowSupply,
		horizonmodels.AxisGeoConflict:
		return true
	default:
		return false
	}
}

func (e *ExternalApplicabilityEvaluator) hasQualifyingExposurePath(ctx context.Context, signal *horizonmodels.SignalDescriptor, fragment *horizonmodels.HorizonFragment) bool {
	if e == nil || signal == nil || fragment == nil {
		return false
	}

	fragmentPath := strings.TrimSpace(fragment.CanonicalPath)
	for _, sp := range signal.SeedPaths {
		if strings.TrimSpace(sp) == fragmentPath {
			return true
		}
	}

	seedIDs, tenantID := e.resolveSeedIDs(ctx, signal)
	targetID := strings.TrimSpace(fragment.ID)
	if targetID == "" || len(seedIDs) == 0 {
		return false
	}
	for _, sid := range seedIDs {
		if sid == targetID {
			return true
		}
	}

	if lensID := lensIDFromContext(ctx); lensID != "" && e.exposurePathsRepo != nil {
		if e.hasExposureArtifactPath(ctx, tenantID, lensID, seedIDs, targetID) {
			return true
		}
	}
	if e.edgesRepo != nil {
		for _, sid := range seedIDs {
			if e.hasGraphPathBounded(ctx, tenantID, sid, targetID, e.maxExposureHops) {
				return true
			}
		}
	}
	return false
}

func (e *ExternalApplicabilityEvaluator) resolveSeedIDs(ctx context.Context, signal *horizonmodels.SignalDescriptor) ([]string, string) {
	tenantID := "global"
	if e == nil || signal == nil {
		return nil, tenantID
	}
	seen := map[string]struct{}{}
	seedIDs := make([]string, 0, len(signal.SeedPaths)+1)
	appendID := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		seedIDs = append(seedIDs, id)
	}

	if e.fragmentsRepo != nil {
		for _, p := range signal.SeedPaths {
			frag, err := e.fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(p))
			if err != nil || frag == nil {
				continue
			}
			if strings.TrimSpace(frag.TenantID) != "" {
				tenantID = strings.TrimSpace(frag.TenantID)
			}
			appendID(frag.ID)
		}
		if strings.TrimSpace(signal.LocationPath) != "" {
			if loc, err := e.fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(signal.LocationPath)); err == nil && loc != nil {
				if strings.TrimSpace(loc.TenantID) != "" {
					tenantID = strings.TrimSpace(loc.TenantID)
				}
				appendID(loc.ID)
			}
		}
	}
	return seedIDs, tenantID
}

func lensIDFromContext(ctx context.Context) string {
	if rc, ok := rtctx.Get(ctx); ok {
		return strings.TrimSpace(rc.LensID)
	}
	return ""
}

func (e *ExternalApplicabilityEvaluator) hasExposureArtifactPath(ctx context.Context, tenantID, lensID string, seedIDs []string, targetID string) bool {
	if e == nil || e.exposurePathsRepo == nil || lensID == "" || targetID == "" || len(seedIDs) == 0 {
		return false
	}
	if tenantID == "" {
		tenantID = "global"
	}
	items, err := e.loadLensExposureArtifacts(ctx, tenantID, lensID)
	if err != nil || len(items) == 0 {
		return false
	}
	seedSet := map[string]struct{}{}
	for _, sid := range seedIDs {
		if strings.TrimSpace(sid) != "" {
			seedSet[strings.TrimSpace(sid)] = struct{}{}
		}
	}
	for _, a := range items {
		if a == nil {
			continue
		}
		if artifactTouchesNodes(a, seedSet, targetID) {
			return true
		}
	}
	return false
}

func artifactTouchesNodes(a *horizonmodels.ExposurePathArtifact, seedSet map[string]struct{}, targetID string) bool {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return false
	}
	hasTarget := strings.TrimSpace(a.ObjectiveID) == targetID || strings.TrimSpace(a.RelatedID) == targetID
	if !hasTarget {
		for _, nid := range a.NodeIDs {
			if strings.TrimSpace(nid) == targetID {
				hasTarget = true
				break
			}
		}
	}
	if !hasTarget {
		return false
	}
	for sid := range seedSet {
		if sid == strings.TrimSpace(a.ObjectiveID) || sid == strings.TrimSpace(a.RelatedID) {
			return true
		}
		for _, nid := range a.NodeIDs {
			if strings.TrimSpace(nid) == sid {
				return true
			}
		}
	}
	return false
}

func (e *ExternalApplicabilityEvaluator) loadLensExposureArtifacts(ctx context.Context, tenantID, lensID string) ([]*horizonmodels.ExposurePathArtifact, error) {
	cacheKey := strings.TrimSpace(tenantID) + "|" + strings.TrimSpace(lensID)
	now := time.Now().UTC()
	e.mu.RLock()
	cached, ok := e.lensCached[cacheKey]
	e.mu.RUnlock()
	if ok && now.Sub(cached.at) < 2*time.Minute {
		return cached.items, nil
	}
	items, err := e.exposurePathsRepo.ListByLens(ctx, tenantID, lensID, nil, 1000)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.lensCached[cacheKey] = cachedExposureList{items: items, at: now}
	e.mu.Unlock()
	return items, nil
}

func (e *ExternalApplicabilityEvaluator) hasGraphPathBounded(ctx context.Context, tenantID, sourceID, targetID string, maxHops int) bool {
	if e == nil || e.edgesRepo == nil {
		return false
	}
	sourceID = strings.TrimSpace(sourceID)
	targetID = strings.TrimSpace(targetID)
	if sourceID == "" || targetID == "" {
		return false
	}
	if sourceID == targetID {
		return true
	}
	if maxHops <= 0 {
		maxHops = 1
	}
	if tenantID == "" {
		tenantID = "global"
	}
	cacheKey := fmt.Sprintf("%s|%s|%s|%d", tenantID, sourceID, targetID, maxHops)
	if v, ok := e.getPathCache(cacheKey); ok {
		return v
	}

	type state struct {
		id   string
		hops int
	}
	seen := map[string]struct{}{sourceID: {}}
	queue := []state{{id: sourceID, hops: 0}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.hops >= maxHops {
			continue
		}
		nextIDs := e.getAdjacentNodeIDs(ctx, tenantID, cur.id)
		for _, nid := range nextIDs {
			if nid == "" {
				continue
			}
			if nid == targetID {
				e.setPathCache(cacheKey, true)
				return true
			}
			if _, ok := seen[nid]; ok {
				continue
			}
			seen[nid] = struct{}{}
			queue = append(queue, state{id: nid, hops: cur.hops + 1})
		}
	}
	e.setPathCache(cacheKey, false)
	return false
}

func (e *ExternalApplicabilityEvaluator) getAdjacentNodeIDs(ctx context.Context, tenantID, nodeID string) []string {
	if e == nil || e.edgesRepo == nil {
		return nil
	}
	key := tenantID + "|" + strings.TrimSpace(nodeID)
	e.mu.RLock()
	if cached, ok := e.adjCached[key]; ok {
		out := make([]string, len(cached))
		copy(out, cached)
		e.mu.RUnlock()
		return out
	}
	e.mu.RUnlock()

	outEdges, _ := e.edgesRepo.ListBySource(ctx, tenantID, nodeID, nil, 80)
	inEdges, _ := e.edgesRepo.ListByTarget(ctx, tenantID, nodeID, nil, 80)

	nextIDs := make([]string, 0, len(outEdges)+len(inEdges))
	for _, ed := range outEdges {
		if ed != nil {
			nextIDs = append(nextIDs, strings.TrimSpace(ed.TargetFragmentID))
		}
	}
	for _, ed := range inEdges {
		if ed != nil {
			nextIDs = append(nextIDs, strings.TrimSpace(ed.SourceFragmentID))
		}
	}
	e.mu.Lock()
	e.adjCached[key] = append([]string(nil), nextIDs...)
	e.mu.Unlock()
	return nextIDs
}

func (e *ExternalApplicabilityEvaluator) getPathCache(key string) (bool, bool) {
	if e == nil || strings.TrimSpace(key) == "" {
		return false, false
	}
	e.mu.RLock()
	if v, ok := e.pathCached[key]; ok {
		e.mu.RUnlock()
		return v, true
	}
	e.mu.RUnlock()
	if e.redis == nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Millisecond)
	defer cancel()
	val, err := e.redis.Get(ctx, "h:app:path:"+key).Result()
	if err != nil {
		return false, false
	}
	if val == "1" {
		e.mu.Lock()
		e.pathCached[key] = true
		e.mu.Unlock()
		return true, true
	}
	if val == "0" {
		e.mu.Lock()
		e.pathCached[key] = false
		e.mu.Unlock()
		return false, true
	}
	return false, false
}

func (e *ExternalApplicabilityEvaluator) setPathCache(key string, value bool) {
	if e == nil || strings.TrimSpace(key) == "" {
		return
	}
	e.mu.Lock()
	e.pathCached[key] = value
	e.mu.Unlock()
	if e.redis == nil {
		return
	}
	raw := "0"
	if value {
		raw = "1"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Millisecond)
	defer cancel()
	_ = e.redis.Set(ctx, "h:app:path:"+key, raw, e.redisTTL).Err()
}

func newRedisApplicabilityClient() *redis.Client {
	addr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if addr == "" {
		if u := strings.TrimSpace(os.Getenv("REDIS_URL")); u != "" {
			if opts, err := redis.ParseURL(u); err == nil {
				return redis.NewClient(opts)
			}
		}
		return nil
	}
	return redis.NewClient(&redis.Options{
		Addr: addr,
	})
}

func axisTypeFit(axis string, ft horizonmodels.FragmentType) (float64, string) {
	axis = strings.TrimSpace(axis)
	switch axis {
	case horizonmodels.AxisGeoConflict:
		if ft == horizonmodels.FragmentTypeGeoRegion || ft == horizonmodels.FragmentTypeOrganization || ft == horizonmodels.FragmentTypeFacility || ft == horizonmodels.FragmentTypeMarket {
			return 1, "geo_conflict_type_fit"
		}
	case horizonmodels.AxisLogisticsCapacity, horizonmodels.AxisLogisticsLatency, horizonmodels.AxisPhysicalFlowSupply, horizonmodels.AxisPhysicalFlowDemand:
		if ft == horizonmodels.FragmentTypeOrganization || ft == horizonmodels.FragmentTypeFacility || ft == horizonmodels.FragmentTypeProduct || ft == horizonmodels.FragmentTypeMarket || ft == horizonmodels.FragmentTypeDeposit {
			return 1, "flow_logistics_type_fit"
		}
	case horizonmodels.AxisLegalAccess, horizonmodels.AxisLegalCompliance:
		if ft == horizonmodels.FragmentTypeOrganization || ft == horizonmodels.FragmentTypeFacility || ft == horizonmodels.FragmentTypeMarket || ft == horizonmodels.FragmentTypeProgram {
			return 1, "legal_type_fit"
		}
	case horizonmodels.AxisFinancialSettlement, horizonmodels.AxisFinancialCredit:
		if ft == horizonmodels.FragmentTypeOrganization || ft == horizonmodels.FragmentTypeMarket || ft == horizonmodels.FragmentTypeProgram {
			return 1, "financial_type_fit"
		}
	case horizonmodels.AxisInfoUncertainty, horizonmodels.AxisInfoReputation, horizonmodels.AxisOperationalContinuity:
		if ft != "" {
			return 0.8, "broad_external_type_fit"
		}
	}
	return 0.0, "axis_type_mismatch"
}

func locationFit(signalLoc string, fragment *horizonmodels.HorizonFragment) (float64, string) {
	signalLoc = strings.TrimSpace(signalLoc)
	if signalLoc == "" {
		return 0.5, "no_signal_location"
	}
	fragCountry := countryFromFragment(fragment)
	sigCountry := countryFromPath(signalLoc)
	if fragCountry == "" || sigCountry == "" {
		return 0.5, "location_partial_unknown"
	}
	if fragCountry == sigCountry {
		return 1.0, "location_country_match"
	}
	return 0.1, "location_country_mismatch"
}

func sphereFit(spheres []string, fragment *horizonmodels.HorizonFragment) (float64, string) {
	if len(spheres) == 0 {
		return 0.5, "no_signal_sphere"
	}
	// If we have sphere coordinates in indexPaths, prefer those.
	idx := fragment.IndexPaths
	if len(idx) == 0 {
		return 0.5, "no_fragment_sphere_index"
	}
	for _, s := range spheres {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		for _, ip := range idx {
			if strings.HasPrefix(strings.TrimSpace(ip), s) {
				return 1.0, "sphere_match"
			}
		}
	}
	return 0.2, "sphere_mismatch"
}

func countryFromFragment(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.HomeLocation != nil && fragment.HomeLocation.ISO3166_1 != nil {
		return strings.ToLower(strings.TrimSpace(*fragment.HomeLocation.ISO3166_1))
	}
	return countryFromPath(strings.TrimSpace(fragment.CanonicalPath))
}

func countryFromPath(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "org", "gov", "loc", "market", "community", "fac":
		return strings.ToLower(strings.TrimSpace(parts[1]))
	case "track":
		if len(parts) >= 3 {
			return strings.ToLower(strings.TrimSpace(parts[2]))
		}
		return ""
	default:
		return ""
	}
}
