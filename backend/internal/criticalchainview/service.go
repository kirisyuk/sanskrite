package criticalchainview

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchainview/config"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchainview/signal"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

var criticalChainCategories = map[string]struct{}{
	"critical_mineral":  {},
	"energy_commodity":  {},
	"agricultural":      {},
	"critical_minerals": {},
}

// Service projects the global Horizon graph into CriticalChain legacy JSON shapes (read-only).
type Service struct {
	frags *repo.HorizonFragmentsRepository
	edges *repo.DependencyEdgesRepository
}

// NewService returns a CriticalChain view. Repos may be nil (endpoints return config-only / empty overlays).
func NewService(frags *repo.HorizonFragmentsRepository, edges *repo.DependencyEdgesRepository) *Service {
	return &Service{frags: frags, edges: edges}
}

func (s *Service) includeProduct(p *horizonmodels.HorizonFragment, configIDs map[string]struct{}) bool {
	if p == nil {
		return false
	}
	slug := materialSlugFromFragment(p)
	if _, ok := configIDs[slug]; ok {
		return true
	}
	cat := productCategoryFromFragment(p)
	if cat == "" {
		return false
	}
	_, ok := criticalChainCategories[strings.ToLower(cat)]
	return ok
}

// ListMaterials returns the embedded catalog merged with AH /product/* overlays when repos are configured.
func (s *Service) ListMaterials(ctx context.Context) ([]*Material, error) {
	defs, err := config.LoadAllMaterials()
	if err != nil {
		return nil, err
	}
	configIDs := make(map[string]struct{}, len(defs))
	for _, d := range defs {
		configIDs[d.MaterialID] = struct{}{}
	}
	bySlug := map[string]*horizonmodels.HorizonFragment{}
	if s.frags != nil {
		prods, err := s.frags.ListByTypeAndTenant(ctx, globalTenant, ah.FragmentClassProduct, 1000)
		if err != nil {
			return nil, err
		}
		for _, p := range prods {
			if !s.includeProduct(p, configIDs) {
				continue
			}
			slug := materialSlugFromFragment(p)
			if slug == "" {
				continue
			}
			bySlug[slug] = p
		}
	}
	var out []*Material
	for _, def := range defs {
		m := materialFromDefinition(def)
		if p := bySlug[def.MaterialID]; p != nil {
			ApplyAHProductOverlay(m, p)
		}
		m.RiskDelta7d = calculateRiskDelta7d(m.RiskScore, m.RiskHistory)
		out = append(out, m)
	}
	for slug, p := range bySlug {
		if _, ok := configIDs[slug]; ok {
			continue
		}
		m := &Material{
			ID:               slug,
			Name:             strings.TrimSpace(p.Description),
			Category:         productCategoryFromFragment(p),
			CriticalityScore: 3,
			RiskScore:        1,
		}
		if m.Name == "" {
			m.Name = slug
		}
		ApplyAHProductOverlay(m, p)
		m.RiskDelta7d = calculateRiskDelta7d(m.RiskScore, m.RiskHistory)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// GetMaterial returns one catalog row by id.
func (s *Service) GetMaterial(ctx context.Context, id string) (*Material, error) {
	def, defErr := config.LoadMaterial(id)
	var m *Material
	if defErr != nil {
		m = &Material{ID: id, Name: id, Category: "", CriticalityScore: 3, RiskScore: 1}
	} else {
		m = materialFromDefinition(def)
	}
	foundAH := false
	if s.frags != nil {
		prods, err := s.frags.ListByTypeAndTenant(ctx, globalTenant, ah.FragmentClassProduct, 1000)
		if err != nil {
			return nil, err
		}
		for _, p := range prods {
			if materialSlugFromFragment(p) == id {
				foundAH = true
				ApplyAHProductOverlay(m, p)
				break
			}
		}
	}
	if defErr != nil && !foundAH {
		return nil, fmt.Errorf("material %q: %w", id, defErr)
	}
	m.RiskDelta7d = calculateRiskDelta7d(m.RiskScore, m.RiskHistory)
	return m, nil
}

func (s *Service) siteFacilities(ctx context.Context) ([]*horizonmodels.HorizonFragment, error) {
	if s.frags == nil {
		return nil, nil
	}
	subtypes := []string{"deposit", "refinery", "processing_plant", "factory", "mine", "processor"}
	seen := map[string]*horizonmodels.HorizonFragment{}
	for _, sub := range subtypes {
		fs, err := s.frags.ListByClassSubtypeAndTenant(ctx, globalTenant, ah.FragmentClassSite, sub, 500)
		if err != nil {
			return nil, err
		}
		for _, f := range fs {
			seen[f.ID] = f
		}
	}
	// Sites carrying transforms but no subtype (legacy) — best-effort include.
	broad, err := s.frags.ListByTypeAndTenant(ctx, globalTenant, ah.FragmentClassSite, 500)
	if err != nil {
		return nil, err
	}
	for _, f := range broad {
		if f == nil {
			continue
		}
		if _, dup := seen[f.ID]; dup {
			continue
		}
		if len(f.SiteTransforms()) > 0 {
			seen[f.ID] = f
		}
	}
	out := make([]*horizonmodels.HorizonFragment, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// facilityLinkedToMaterial is true when the site produces or supplies the given catalog material id (product slug).
func (s *Service) facilityLinkedToMaterial(ctx context.Context, site *horizonmodels.HorizonFragment, productID string) bool {
	if s == nil || s.edges == nil || s.frags == nil || site == nil || productID == "" {
		return false
	}
	edges, err := s.edges.ListBySource(ctx, globalTenant, site.ID, ptrRelation(ah.RelationNameProduces), 80)
	if err == nil {
		for _, e := range edges {
			if tgt, err := s.frags.Get(ctx, e.TargetFragmentID); err == nil && tgt != nil {
				if tgt.Class == ah.FragmentClassProduct && materialSlugFromFragment(tgt) == productID {
					return true
				}
			}
		}
	}
	edges2, err := s.edges.ListBySource(ctx, globalTenant, site.ID, ptrRelation(ah.RelationNameSupplies), 120)
	if err != nil {
		return false
	}
	for _, e := range edges2 {
		tgt, err := s.frags.Get(ctx, e.TargetFragmentID)
		if err != nil || tgt == nil {
			continue
		}
		if tgt.Class == ah.FragmentClassProduct && materialSlugFromFragment(tgt) == productID {
			return true
		}
	}
	return false
}

func (s *Service) sanctionedSiteIDs(ctx context.Context) map[string]struct{} {
	out := make(map[string]struct{})
	if s.frags == nil || s.edges == nil {
		return out
	}
	sanctions, err := s.frags.ListByClassSubtypeAndTenant(ctx, globalTenant, ah.FragmentClassEvidence, "sanction", 400)
	if err != nil {
		return out
	}
	rel := ah.RelationNameSubjectTo
	for _, frag := range sanctions {
		if frag == nil {
			continue
		}
		edges, err := s.edges.ListBySource(ctx, globalTenant, frag.ID, &rel, 120)
		if err != nil {
			continue
		}
		for _, e := range edges {
			tid := strings.TrimSpace(e.TargetFragmentID)
			if tid == "" {
				continue
			}
			tgt, err := s.frags.Get(ctx, tid)
			if err != nil || tgt == nil {
				continue
			}
			if tgt.Class == ah.FragmentClassSite {
				out[strings.TrimSpace(tgt.ID)] = struct{}{}
			}
		}
	}
	return out
}

func (s *Service) siteSubjectToSanction(ctx context.Context, siteID string) bool {
	if s.frags == nil || s.edges == nil || strings.TrimSpace(siteID) == "" {
		return false
	}
	rel := ah.RelationNameSubjectTo
	edges, err := s.edges.ListByTarget(ctx, globalTenant, siteID, &rel, 80)
	if err != nil {
		return false
	}
	for _, e := range edges {
		src, err := s.frags.Get(ctx, e.SourceFragmentID)
		if err != nil || src == nil {
			continue
		}
		if src.Class == ah.FragmentClassEvidence && strings.EqualFold(strings.TrimSpace(src.Subtype), "sanction") {
			return true
		}
	}
	return false
}

func ptrRelation(r ah.RelationName) *ah.RelationName {
	return &r
}

// ListFacilities lists AH industrial sites with CriticalChain filters.
func (s *Service) ListFacilities(ctx context.Context, opts *ListFacilitiesOpts) ([]*Facility, error) {
	sites, err := s.siteFacilities(ctx)
	if err != nil {
		return nil, err
	}
	sanctioned := s.sanctionedSiteIDs(ctx)
	var out []*Facility
	for _, sf := range sites {
		_, isSanctioned := sanctioned[strings.TrimSpace(sf.ID)]
		f := facilityFromSiteFragment(sf, "", isSanctioned)
		if f == nil {
			continue
		}
		if opts != nil {
			if opts.MaterialID != "" && !s.facilityLinkedToMaterial(ctx, sf, opts.MaterialID) {
				// Allow attribute hint match when edges are missing.
				if attrString(sf.Attributes, "material_id") != opts.MaterialID {
					continue
				}
			}
			if opts.Country != "" && !strings.EqualFold(f.Country, opts.Country) {
				continue
			}
			if opts.MinRiskScore > 0 && f.RiskScore < opts.MinRiskScore {
				continue
			}
		}
		out = append(out, f)
	}
	return out, nil
}

// GetFacility returns a facility projection by Horizon fragment id.
func (s *Service) GetFacility(ctx context.Context, id string) (*Facility, error) {
	if s.frags == nil {
		return nil, repo.ErrFragmentNotFound
	}
	sf, err := s.frags.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	isSanctioned := s.siteSubjectToSanction(ctx, id)
	f := facilityFromSiteFragment(sf, "", isSanctioned)
	if f == nil {
		return nil, repo.ErrFragmentNotFound
	}
	return f, nil
}

func (s *Service) legEndpointIDs(ctx context.Context, fragID string) (from, to string) {
	if s == nil || s.edges == nil || fragID == "" {
		return "", ""
	}
	outE, _ := s.edges.ListBySource(ctx, globalTenant, fragID, ptrRelation(ah.RelationNameLegConnects), 20)
	for _, e := range outE {
		if e.TargetFragmentID != "" {
			return fragID, e.TargetFragmentID
		}
	}
	inE, _ := s.edges.ListByTarget(ctx, globalTenant, fragID, ptrRelation(ah.RelationNameLegConnects), 20)
	for _, e := range inE {
		if e.SourceFragmentID != "" {
			return e.SourceFragmentID, fragID
		}
	}
	return "", ""
}

// ListRoutes projects route-class fragments and route-subtype sites with leg_connects endpoints.
func (s *Service) ListRoutes(ctx context.Context) ([]*Route, error) {
	if s.frags == nil {
		return []*Route{}, nil
	}
	seen := map[string]struct{}{}
	var out []*Route
	routeFrags, err := s.frags.ListByTypeAndTenant(ctx, globalTenant, ah.FragmentClassRoute, 400)
	if err != nil {
		return nil, err
	}
	for _, rf := range routeFrags {
		from, to := s.legEndpointIDs(ctx, rf.ID)
		r := routeFromAHFragment(rf, from, to)
		if r == nil {
			continue
		}
		if r.FromFacility == "" {
			r.FromFacility = from
		}
		if r.ToFacility == "" {
			r.ToFacility = to
		}
		out = append(out, r)
		seen[r.ID] = struct{}{}
	}
	sites, err := s.frags.ListByClassSubtypeAndTenant(ctx, globalTenant, ah.FragmentClassSite, "route", 200)
	if err != nil {
		return nil, err
	}
	for _, sf := range sites {
		if _, ok := seen[sf.ID]; ok {
			continue
		}
		from, to := s.legEndpointIDs(ctx, sf.ID)
		r := routeFromAHFragment(sf, from, to)
		if r != nil {
			out = append(out, r)
		}
	}
	return out, nil
}

// ListConflictZones lists geo fragments with subtype affected_zone.
func (s *Service) ListConflictZones(ctx context.Context) ([]*ConflictZone, error) {
	if s.frags == nil {
		return []*ConflictZone{}, nil
	}
	var raw []*horizonmodels.HorizonFragment
	modern, err := s.frags.ListByClassSubtypeAndTenant(ctx, globalTenant, ah.FragmentClassGeo, "affected_zone", 500)
	if err != nil {
		return nil, err
	}
	raw = append(raw, modern...)
	seen := map[string]struct{}{}
	var out []*ConflictZone
	for _, f := range raw {
		if f == nil {
			continue
		}
		if _, dup := seen[f.ID]; dup {
			continue
		}
		seen[f.ID] = struct{}{}
		if cz := conflictZoneFromGeoFragment(f); cz != nil {
			out = append(out, cz)
		}
	}
	return out, nil
}

// GetCountryRisk maps country code to risk level for choropleth.
func (s *Service) GetCountryRisk(ctx context.Context) (map[string]float64, error) {
	zones, err := s.ListConflictZones(ctx)
	if err != nil {
		return nil, err
	}
	severityToLevel := map[string]float64{
		"LOW": 1.5, "MEDIUM": 3.0, "HIGH": 4.0, "CRITICAL": 5.0,
	}
	out := make(map[string]float64)
	for _, z := range zones {
		cc := z.Country
		if cc == "" {
			continue
		}
		level := severityToLevel[string(z.Severity)]
		if level == 0 {
			level = 2.0
		}
		if existing, ok := out[cc]; !ok || level > existing {
			out[cc] = level
		}
	}
	return out, nil
}

// ListChokepoints returns static chokepoints merged with YAML config share weightings.
func (s *Service) ListChokepoints(ctx context.Context, materialID string) ([]*Chokepoint, error) {
	defs, _ := config.LoadAllMaterials()
	pathToChoke := make(map[string]Chokepoint)
	for _, c := range knownChokepoints {
		pathToChoke[c.Path] = c
	}
	seen := map[string]bool{}
	var out []*Chokepoint
	for _, def := range defs {
		if materialID != "" && def.MaterialID != materialID {
			continue
		}
		for _, kc := range def.KeyChokepoints {
			if cp, ok := pathToChoke[kc.Path]; ok && !seen[cp.ID] {
				seen[cp.ID] = true
				cp2 := cp
				cp2.ShareOfSupply = kc.ShareOfGlobalSupply
				cp2.RiskMultiplier = kc.RiskMultiplier
				cp2.MaterialID = def.MaterialID
				out = append(out, &cp2)
			}
		}
	}
	if len(out) == 0 {
		for i := range knownChokepoints {
			cp := knownChokepoints[i]
			out = append(out, &cp)
		}
	}
	return out, nil
}

// GetSignals fuses conflict-zone severities into region rollups.
func (s *Service) GetSignals(ctx context.Context) ([]signal.FusedSignal, error) {
	zones, err := s.ListConflictZones(ctx)
	if err != nil {
		return nil, err
	}
	regions := make(map[string]*signal.RegionSignals)
	severityToLevel := map[string]float64{
		"LOW": 1.5, "MEDIUM": 3.0, "HIGH": 4.0, "CRITICAL": 5.0,
	}
	for _, z := range zones {
		level := severityToLevel[string(z.Severity)]
		if level == 0 {
			level = 2.0
		}
		key := z.Name
		if key == "" {
			key = z.ID
		}
		if key == "" {
			key = "unknown"
		}
		if regions[key] == nil {
			regions[key] = &signal.RegionSignals{Region: key, Sources: nil}
		}
		regions[key].Sources = append(regions[key].Sources, signal.SourceSignal{
			Name:      z.Source,
			Detail:    string(z.Severity),
			RiskLevel: level,
		})
	}
	var regionList []signal.RegionSignals
	for _, r := range regions {
		regionList = append(regionList, *r)
	}
	return signal.Fuse(regionList), nil
}

func (s *Service) processingStepsFromGraph(ctx context.Context, nodeIDs map[string]struct{}) []*ProcessingStep {
	if s.edges == nil || len(nodeIDs) == 0 {
		return nil
	}
	allow := map[ah.RelationName]struct{}{
		ah.RelationNameSupplies: {},
		ah.RelationNameProduces: {},
	}
	seen := map[string]bool{}
	var steps []*ProcessingStep
	for id := range nodeIDs {
		edges, err := s.edges.ListBySource(ctx, globalTenant, id, nil, 120)
		if err != nil {
			continue
		}
		for _, e := range edges {
			if _, ok := allow[e.Relation]; !ok {
				continue
			}
			if _, ok := nodeIDs[e.TargetFragmentID]; !ok {
				continue
			}
			if _, ok := nodeIDs[e.SourceFragmentID]; !ok {
				continue
			}
			st := &ProcessingStep{
				From: e.SourceFragmentID,
				To:   e.TargetFragmentID,
				Type: stepTypeForAHRelation(e.Relation),
			}
			key := st.From + "|" + st.To + "|" + string(st.Type)
			if seen[key] {
				continue
			}
			seen[key] = true
			steps = append(steps, st)
		}
	}
	return steps
}

// GetGraph builds the demo graph for a material id (YAML stages + AH facilities + AH edges).
func (s *Service) GetGraph(ctx context.Context, materialID string) (*GraphResult, error) {
	material, _ := s.GetMaterial(ctx, materialID)
	facilities, err := s.ListFacilities(ctx, &ListFacilitiesOpts{MaterialID: materialID})
	if err != nil {
		return nil, err
	}
	var def *config.MaterialDefinition
	if loaded, loadErr := config.LoadMaterial(materialID); loadErr == nil {
		def = loaded
	}
	graphMaterials, cfgSteps := buildGraphMaterialsAndSteps(material, def, facilities, nil)
	nodeIDs := map[string]struct{}{}
	for _, m := range graphMaterials {
		nodeIDs[m.ID] = struct{}{}
	}
	for _, f := range facilities {
		nodeIDs[f.ID] = struct{}{}
	}
	ahSteps := s.processingStepsFromGraph(ctx, nodeIDs)
	allSteps := append(append([]*ProcessingStep(nil), cfgSteps...), ahSteps...)
	return &GraphResult{
		Material:        material,
		Materials:       graphMaterials,
		Facilities:      facilities,
		ProcessingSteps: allSteps,
	}, nil
}

func calculateRiskDelta7d(current float64, history []RiskPoint) float64 {
	if len(history) == 0 {
		return 0
	}
	target := time.Now().Add(-7 * 24 * time.Hour)
	bestDiff := math.MaxFloat64
	var historicVal float64 = current
	found := false

	for _, pt := range history {
		diff := math.Abs(float64(pt.Timestamp.Sub(target)))
		if diff < bestDiff {
			bestDiff = diff
			historicVal = pt.Value
			found = true
		}
	}
	if !found {
		return 0
	}
	return current - historicVal
}
