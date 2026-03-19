package criticalchain

import (
	"context"
	"math"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/config"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/repo"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/signal"
)

// Service provides CriticalChain business logic.
type Service struct {
	nodesRepo *repo.NodesRepository
	edgesRepo *repo.EdgesRepository
	zonesRepo *repo.ConflictZonesRepository
}

// NewService creates a new CriticalChain service.
func NewService(db *firestore.Client) *Service {
	return &Service{
		nodesRepo: repo.NewNodesRepository(db),
		edgesRepo: repo.NewEdgesRepository(db),
		zonesRepo: repo.NewConflictZonesRepository(db),
	}
}

// GetMaterial returns a material by ID.
func (s *Service) GetMaterial(ctx context.Context, id string) (*models.Material, error) {
	def, err := config.LoadMaterial(id)
	if err != nil {
		// No config: try Firestore only
		return s.nodesRepo.GetMaterial(ctx, id)
	}
	name := def.DisplayName["en"]
	if name == "" {
		name = id
	}
	m := &models.Material{
		ID:               def.MaterialID,
		Name:             name,
		Category:         def.Category,
		CriticalityScore: 3.0,
		RiskScore:        1.0,
	}
	if stored, err := s.nodesRepo.GetMaterial(ctx, id); err == nil {
		m.RiskScore = stored.RiskScore
		m.CriticalityScore = stored.CriticalityScore
		m.Aliases = stored.Aliases
		m.HSCodes = stored.HSCodes
		m.CASNumber = stored.CASNumber
		m.RiskHistory = stored.RiskHistory
		m.RiskDelta7d = calculateRiskDelta7d(m.RiskScore, m.RiskHistory)
	}
	return m, nil
}

// ListMaterials returns all materials (from config + Firestore).
func (s *Service) ListMaterials(ctx context.Context) ([]*models.Material, error) {
	defs, err := config.LoadAllMaterials()
	if err != nil {
		return nil, err
	}
	// Merge with Firestore data if any
	stored, _ := s.nodesRepo.ListMaterials(ctx)
	storedByID := make(map[string]*models.Material)
	for _, m := range stored {
		storedByID[m.ID] = m
	}
	var out []*models.Material
	for _, def := range defs {
		name := def.DisplayName["en"]
		if name == "" {
			name = def.MaterialID
		}
		m := &models.Material{
			ID:               def.MaterialID,
			Name:             name,
			Category:         def.Category,
			CriticalityScore: 3.0,
			RiskScore:        1.0,
		}
		if stored, ok := storedByID[def.MaterialID]; ok {
			m.RiskScore = stored.RiskScore
			m.CriticalityScore = stored.CriticalityScore
			m.Aliases = stored.Aliases
			m.HSCodes = stored.HSCodes
			m.CASNumber = stored.CASNumber
			m.RiskHistory = stored.RiskHistory
			m.RiskDelta7d = calculateRiskDelta7d(m.RiskScore, m.RiskHistory)
		}
		out = append(out, m)
	}
	return out, nil
}

// ListFacilities returns facilities with optional filters.
func (s *Service) ListFacilities(ctx context.Context, opts *repo.ListFacilitiesOpts) ([]*models.Facility, error) {
	return s.nodesRepo.ListFacilities(ctx, opts)
}

// GetFacility returns a facility by ID.
func (s *Service) GetFacility(ctx context.Context, id string) (*models.Facility, error) {
	return s.nodesRepo.GetFacility(ctx, id)
}

// ListRoutes returns all routes.
func (s *Service) ListRoutes(ctx context.Context) ([]*models.Route, error) {
	return s.edgesRepo.ListRoutes(ctx)
}

// ListConflictZones returns all conflict zones.
func (s *Service) ListConflictZones(ctx context.Context) ([]*models.ConflictZone, error) {
	return s.zonesRepo.List(ctx)
}

// GetCountryRisk returns country ISO -> risk score (1-5) for choropleth (whitepaper 4.4).
func (s *Service) GetCountryRisk(ctx context.Context) (map[string]float64, error) {
	zones, err := s.zonesRepo.List(ctx)
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

// ListChokepoints returns chokepoints for map markers (whitepaper 4.4).
// If materialID is set, returns only chokepoints relevant to that material.
func (s *Service) ListChokepoints(ctx context.Context, materialID string) ([]*Chokepoint, error) {
	defs, _ := config.LoadAllMaterials()
	pathToChoke := make(map[string]Chokepoint)
	for _, c := range knownChokepoints {
		pathToChoke[c.Path] = c
	}
	seen := make(map[string]bool)
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
			out = append(out, &knownChokepoints[i])
		}
	}
	return out, nil
}

// GetSignals returns fused risk signals for UI (whitepaper 3.4).
func (s *Service) GetSignals(ctx context.Context) ([]signal.FusedSignal, error) {
	zones, err := s.zonesRepo.List(ctx)
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

// GetGraph returns the supply chain graph for a material (nodes + edges).
func (s *Service) GetGraph(ctx context.Context, materialID string) (*GraphResult, error) {
	material, _ := s.GetMaterial(ctx, materialID)
	facilities, err := s.nodesRepo.ListFacilities(ctx, &repo.ListFacilitiesOpts{MaterialID: materialID})
	if err != nil {
		return nil, err
	}
	steps, err := s.edgesRepo.ListProcessingSteps(ctx)
	if err != nil {
		return nil, err
	}
	var def *config.MaterialDefinition
	if loaded, loadErr := config.LoadMaterial(materialID); loadErr == nil {
		def = loaded
	}
	graphMaterials, relevantSteps := buildGraphMaterialsAndSteps(material, def, facilities, steps)
	return &GraphResult{
		Materials:       graphMaterials,
		Material:        material,
		Facilities:      facilities,
		ProcessingSteps: relevantSteps,
	}, nil
}

// GraphResult is the graph response for a material.
type GraphResult struct {
	Material        *models.Material         `json:"material,omitempty"`
	Materials       []*models.Material       `json:"materials,omitempty"`
	Facilities      []*models.Facility       `json:"facilities"`
	ProcessingSteps []*models.ProcessingStep `json:"processingSteps"`
}

func calculateRiskDelta7d(current float64, history []models.RiskPoint) float64 {
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
