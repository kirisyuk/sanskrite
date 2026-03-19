package pipeline

import (
	"context"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/adapters"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/config"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/datasource"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/models"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/repo"
	"github.com/igorkirisyuk/sanskrite/internal/criticalchain/scoring"
)

// Runner orchestrates the 6-hour data pipeline.
type Runner struct {
	db            *firestore.Client
	nodesRepo     *repo.NodesRepository
	edgesRepo     *repo.EdgesRepository
	zonesRepo     *repo.ConflictZonesRepository
	sanctionsRepo *repo.SanctionsRepository
}

// NewRunner creates a new pipeline runner.
func NewRunner(db *firestore.Client) *Runner {
	return &Runner{
		db:            db,
		nodesRepo:     repo.NewNodesRepository(db),
		edgesRepo:     repo.NewEdgesRepository(db),
		zonesRepo:     repo.NewConflictZonesRepository(db),
		sanctionsRepo: repo.NewSanctionsRepository(db),
	}
}

// Run executes the full pipeline (all workers).
func (r *Runner) Run(ctx context.Context) error {
	since := time.Now().Add(-6 * time.Hour)
	r.runParallel(ctx, since, []func(context.Context, time.Time) error{
		r.runWorker1, r.runWorker2, r.runWorker3, r.runWorker4, r.runWorker5, r.runWorker6, r.runWorker7,
	})
	if err := r.runWorker8(ctx, since); err != nil {
		log.Printf("[CriticalChain] Worker error: %v", err)
	}
	return nil
}

func (r *Runner) runParallel(ctx context.Context, since time.Time, workers []func(context.Context, time.Time) error) {
	var wg sync.WaitGroup
	for _, w := range workers {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w(ctx, since); err != nil {
				log.Printf("[CriticalChain] Worker error: %v", err)
			}
		}()
	}
	wg.Wait()
}

func (r *Runner) runWorker1(ctx context.Context, since time.Time) error {
	adapter := adapters.NewUSGSMRDSAdapter()
	raws, err := adapter.Fetch(ctx, since)
	if err != nil || len(raws) == 0 {
		return err
	}
	for _, raw := range raws {
		rec, err := adapter.Normalize(raw)
		if err != nil {
			continue
		}
		if rec.Kind == "facility" && rec.Payload != nil {
			if fac, ok := rec.Payload.(*models.Facility); ok {
				_ = r.nodesRepo.SetFacility(ctx, fac)
			}
		}
	}
	return nil
}

func (r *Runner) runWorker2(ctx context.Context, since time.Time) error {
	// EIA, ENTSO-G, JODI (Oil & Gas)
	hub := ensureEUGasHub()
	if err := r.nodesRepo.SetFacility(ctx, hub); err != nil {
		log.Printf("[CriticalChain] Worker2: failed to upsert EU gas hub: %v", err)
	}
	saveFacilities := func(a datasource.Adapter) {
		raws, _ := a.Fetch(ctx, since)
		for _, raw := range raws {
			rec, err := a.Normalize(raw)
			if err != nil {
				continue
			}
			if rec.Kind == "facility" && rec.Payload != nil {
				if fac, ok := rec.Payload.(*models.Facility); ok {
					if a.Name() == "entsog" {
						if rawMap, ok := raw.Data.(map[string]interface{}); ok {
							enrichENTSOGFacility(fac, rawMap)
							if route := buildENTSOGRoute(hub, fac, rawMap); route != nil {
								if err := r.edgesRepo.SetRoute(ctx, route); err != nil {
									log.Printf("[CriticalChain] Worker2: failed to upsert route %s: %v", route.ID, err)
								}
							}
						}
					}
					_ = r.nodesRepo.SetFacility(ctx, fac)
				}
			}
		}
	}
	saveFacilities(adapters.NewEIAAdapter())
	saveFacilities(adapters.NewENTSOGAdapter())
	saveFacilities(adapters.NewJODIAdapter())
	return nil
}

func (r *Runner) runWorker3(ctx context.Context, since time.Time) error {
	_, _ = adapters.NewComtradeAdapter().Fetch(ctx, since)
	return nil
}

func (r *Runner) runWorker4(ctx context.Context, since time.Time) error {
	a := adapters.NewCountryRiskAdapter()
	raws, _ := a.Fetch(ctx, since)
	for _, raw := range raws {
		rec, err := a.Normalize(raw)
		if err != nil {
			continue
		}
		if rec.Kind == "conflict_zone" && rec.Payload != nil {
			if cz, ok := rec.Payload.(*models.ConflictZone); ok {
				_ = r.zonesRepo.Set(ctx, cz)
			}
		}
	}
	return nil
}

func (r *Runner) runWorker5(ctx context.Context, since time.Time) error {
	saveSanctions := func(a datasource.Adapter) {
		raws, _ := a.Fetch(ctx, since)
		for _, raw := range raws {
			rec, err := a.Normalize(raw)
			if err != nil {
				continue
			}
			if rec.Kind == "sanction" && rec.Payload != nil {
				payload, ok := rec.Payload.(map[string]interface{})
				if !ok {
					continue
				}
				name, _ := payload["name"].(string)
				source, _ := payload["source"].(string)
				if name == "" {
					name = rec.ID
				}
				if source == "" {
					source = a.Name()
				}
				_ = r.sanctionsRepo.Set(ctx, &repo.SanctionRecord{
					ID:        rec.ID,
					Name:      name,
					Source:    source,
					Payload:   payload,
					UpdatedAt: time.Now(),
				})
			}
		}
	}
	saveSanctions(adapters.NewOFACSDNAdapter())
	saveSanctions(adapters.NewEUSanctionsAdapter())
	return nil
}

func (r *Runner) runWorker6(ctx context.Context, since time.Time) error {
	_, _ = adapters.NewGDELTAdapter().Fetch(ctx, since)
	return nil
}

func (r *Runner) runWorker7(ctx context.Context, since time.Time) error {
	a := adapters.NewGeoEnrichAdapter()
	raws, _ := a.Fetch(ctx, since)
	for _, raw := range raws {
		rec, err := a.Normalize(raw)
		if err != nil {
			continue
		}
		if rec.Kind == "facility" && rec.Payload != nil {
			if fac, ok := rec.Payload.(*models.Facility); ok {
				_ = r.nodesRepo.SetFacility(ctx, fac)
			}
		}
	}
	return nil
}

func (r *Runner) runWorker8(ctx context.Context, since time.Time) error {
	// Gemini Search Grounding batch risk scoring (whitepaper 3.6)
	scorer := scoring.NewVertexAIRiskScorer()
	facilities, err := r.nodesRepo.ListFacilities(ctx, nil)
	if err != nil || len(facilities) == 0 {
		return err
	}
	zones, err := r.zonesRepo.List(ctx)
	if err != nil {
		return err
	}
	sanctions, err := r.sanctionsRepo.List(ctx)
	if err != nil {
		return err
	}
	countryRisk := aggregateCountryRisk(zones)
	conflictRisk := aggregateConflictRisk(zones)
	materialDefs, _ := config.LoadAllMaterials()
	defByID := make(map[string]*config.MaterialDefinition, len(materialDefs))
	for _, def := range materialDefs {
		defByID[def.MaterialID] = def
	}
	materialFacilities := make(map[string][]*models.Facility)
	for _, f := range facilities {
		def := defByID[f.MaterialID]
		sanctioned := matchesSanctions(f, sanctions)
		f.Sanctioned = sanctioned
		countrySignal := countryRisk[strings.ToUpper(strings.TrimSpace(f.Country))]
		if countrySignal == 0 {
			countrySignal = 1.0
		}
		conflictSignal := conflictRisk[strings.ToUpper(strings.TrimSpace(f.Country))]
		if sanctioned {
			conflictSignal = math.Max(conflictSignal, 4.5)
		}
		if conflictSignal == 0 {
			conflictSignal = 1.0
		}
		supplySignal := 1.0
		freightSignal := 1.0
		if def != nil {
			supplySignal = scoring.HHIToRisk(def.ConcentrationHHI)
			freightSignal = chokepointSignal(def)
		}
		signals := scoring.SignalValues{
			CountryRisk:         countrySignal,
			ConflictEvents:      conflictSignal,
			SupplyConcentration: supplySignal,
			PriceVolatility:     1.5,
			FreightStress:       freightSignal,
		}
		score := scoring.RoundToScale(scorer.ScoreFacility(ctx, f, signals))
		if sanctioned {
			score = math.Max(score, 4.0)
		}
		f.RiskScore = score
		f.UpdatedAt = time.Now()
		if err := r.nodesRepo.SetFacility(ctx, f); err != nil {
			log.Printf("[CriticalChain] Worker8: failed to update facility %s: %v", f.ID, err)
		}
		materialFacilities[f.MaterialID] = append(materialFacilities[f.MaterialID], f)
	}
	for materialID, facs := range materialFacilities {
		def := defByID[materialID]
		criticality := 3.0
		if def != nil {
			criticality = scoring.RoundToScale(scoring.HHIToRisk(def.ConcentrationHHI))
		}
		risk := aggregateMaterialRisk(facs)
		name := materialID
		category := ""
		if def != nil {
			if display := strings.TrimSpace(def.DisplayName["en"]); display != "" {
				name = display
			}
			category = def.Category
		}
		if err := r.nodesRepo.SetMaterial(ctx, &models.Material{
			ID:               materialID,
			Name:             name,
			Category:         category,
			CriticalityScore: criticality,
			RiskScore:        risk,
			UpdatedAt:        time.Now(),
		}); err != nil {
			log.Printf("[CriticalChain] Worker8: failed to update material %s: %v", materialID, err)
		}
	}
	_ = since
	return nil
}

func aggregateCountryRisk(zones []*models.ConflictZone) map[string]float64 {
	out := make(map[string]float64)
	for _, zone := range zones {
		country := strings.ToUpper(strings.TrimSpace(zone.Country))
		if country == "" {
			continue
		}
		level := severityScore(zone.Severity)
		if level > out[country] {
			out[country] = level
		}
	}
	return out
}

func aggregateConflictRisk(zones []*models.ConflictZone) map[string]float64 {
	out := make(map[string]float64)
	for _, zone := range zones {
		country := strings.ToUpper(strings.TrimSpace(zone.Country))
		if country == "" {
			continue
		}
		level := severityScore(zone.Severity)
		switch zone.Type {
		case models.ConflictTypeArmed, models.ConflictTypeSanctions:
			level = math.Min(5, level+0.5)
		}
		if level > out[country] {
			out[country] = level
		}
	}
	return out
}

func severityScore(severity models.ConflictZoneSeverity) float64 {
	switch severity {
	case models.SeverityLow:
		return 1.5
	case models.SeverityMedium:
		return 3.0
	case models.SeverityHigh:
		return 4.0
	case models.SeverityCritical:
		return 5.0
	default:
		return 2.0
	}
}

func chokepointSignal(def *config.MaterialDefinition) float64 {
	if def == nil || len(def.KeyChokepoints) == 0 {
		return 1.0
	}
	maxSignal := 1.0
	for _, cp := range def.KeyChokepoints {
		if cp.RiskMultiplier > maxSignal {
			maxSignal = cp.RiskMultiplier
		}
	}
	return math.Min(5, math.Max(1, maxSignal))
}

func matchesSanctions(facility *models.Facility, sanctions []*repo.SanctionRecord) bool {
	if facility == nil {
		return false
	}
	name := normalizeName(facility.Name)
	if name == "" {
		return false
	}
	for _, record := range sanctions {
		recordName := normalizeName(record.Name)
		if recordName == "" {
			continue
		}
		if name == recordName || strings.Contains(name, recordName) || strings.Contains(recordName, name) {
			return true
		}
	}
	return false
}

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	replacer := strings.NewReplacer(",", " ", ".", " ", "-", " ", "_", " ", "/", " ", "(", " ", ")", " ")
	return strings.Join(strings.Fields(replacer.Replace(s)), " ")
}

func aggregateMaterialRisk(facilities []*models.Facility) float64 {
	if len(facilities) == 0 {
		return 1.0
	}
	total := 0.0
	maxScore := 1.0
	for _, facility := range facilities {
		total += facility.RiskScore
		if facility.RiskScore > maxScore {
			maxScore = facility.RiskScore
		}
	}
	avg := total / float64(len(facilities))
	return scoring.RoundToScale(math.Min(5, (avg*0.7)+(maxScore*0.3)))
}
