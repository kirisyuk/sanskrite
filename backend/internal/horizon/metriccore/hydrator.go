package metriccore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	gencombined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/simscope"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
)

const (
	sourceKindGraphDerived       = "graph_derived"
	sourceKindGroundedSearch     = "grounded_search"
	operationalRefreshBatchSize  = 2
	maxOperationalRefreshTargets = 8
)

type GenAIClient interface {
	GenAIGenerate(ctx context.Context, req gencombined.GenAIRequest) (*gencombined.GenAIResponse, error)
}

type OperationalHydrationRequest struct {
	TenantID     string
	LensID       string
	Question     string
	Recognition  map[string]any
	DomainModes  []string
	BaselineRefs horizonmodels.SimulationBaselineRefs
}

type OperationalSnapshot struct {
	Summary     OperationalSummary        `json:"summary,omitempty"`
	Targets     []OperationalTarget       `json:"targets,omitempty"`
	Paths       []OperationalPath         `json:"paths,omitempty"`
	Refreshed   []OperationalTargetUpdate `json:"refreshed,omitempty"`
	Missing     []string                  `json:"missing,omitempty"`
	GeneratedAt time.Time                 `json:"generatedAt,omitempty"`
}

type OperationalSummary struct {
	TargetCount                   int      `json:"targetCount,omitempty"`
	RefreshedTargetCount          int      `json:"refreshedTargetCount,omitempty"`
	MissingMetricCount            int      `json:"missingMetricCount,omitempty"`
	BlockedTargetCount            int      `json:"blockedTargetCount,omitempty"`
	HighFrictionPathCount         int      `json:"highFrictionPathCount,omitempty"`
	BlockageMax                   float64  `json:"blockageMax,omitempty"`
	TransitDelayHoursMax          float64  `json:"transitDelayHoursMax,omitempty"`
	ScreeningIntensityMax         float64  `json:"screeningIntensityMax,omitempty"`
	CustomsDelayHoursMax          float64  `json:"customsDelayHoursMax,omitempty"`
	RerouteCostMultiplierMax      float64  `json:"rerouteCostMultiplierMax,omitempty"`
	InsuranceAvailabilityMin      float64  `json:"insuranceAvailabilityMin,omitempty"`
	SettlementReliabilityMin      float64  `json:"settlementReliabilityMin,omitempty"`
	ProductSafetyEnforcementMax   float64  `json:"productSafetyEnforcementMax,omitempty"`
	CounterfeitSeizurePressureMax float64  `json:"counterfeitSeizurePressureMax,omitempty"`
	RecallExposureMax             float64  `json:"recallExposureMax,omitempty"`
	InformalChannelLeakageMax     float64  `json:"informalChannelLeakageMax,omitempty"`
	ConsumerTrustMin              float64  `json:"consumerTrustMin,omitempty"`
	BottleneckRefs                []string `json:"bottleneckRefs,omitempty"`
	DegradedTargetRefs            []string `json:"degradedTargetRefs,omitempty"`
}

type OperationalTarget struct {
	Ref            string                               `json:"ref,omitempty"`
	Name           string                               `json:"name,omitempty"`
	Class          string                               `json:"class,omitempty"`
	WhyInScope     string                               `json:"whyInScope,omitempty"`
	RefreshReasons []string                             `json:"refreshReasons,omitempty"`
	Metrics        map[string]horizonmodels.MetricValue `json:"metrics,omitempty"`
	Refreshed      bool                                 `json:"refreshed,omitempty"`
	Confidence     float64                              `json:"confidence,omitempty"`
	Freshness      string                               `json:"freshness,omitempty"`
}

type OperationalTargetUpdate struct {
	Ref          string    `json:"ref,omitempty"`
	MetricPaths  []string  `json:"metricPaths,omitempty"`
	Source       string    `json:"source,omitempty"`
	ObservedAt   time.Time `json:"observedAt,omitempty"`
	EvidenceURLs []string  `json:"evidenceUrls,omitempty"`
}

type OperationalPath struct {
	ArtifactID                      string           `json:"artifactId,omitempty"`
	ArtifactKey                     string           `json:"artifactKey,omitempty"`
	Title                           string           `json:"title,omitempty"`
	PathStatus                      string           `json:"pathStatus,omitempty"`
	ProductAxis                     string           `json:"productAxis,omitempty"`
	PathStressScore                 float64          `json:"pathStressScore,omitempty"`
	BindingScore                    float64          `json:"bindingScore,omitempty"`
	RecoveryLatencyHours            float64          `json:"recoveryLatencyHours,omitempty"`
	BlockageScore                   float64          `json:"blockageScore,omitempty"`
	TransitDelayHours               float64          `json:"transitDelayHours,omitempty"`
	ScreeningIntensityScore         float64          `json:"screeningIntensityScore,omitempty"`
	CustomsDelayHours               float64          `json:"customsDelayHours,omitempty"`
	RerouteCostMultiplier           float64          `json:"rerouteCostMultiplier,omitempty"`
	InsuranceAvailability           float64          `json:"insuranceAvailability,omitempty"`
	SettlementReliabilityScore      float64          `json:"settlementReliabilityScore,omitempty"`
	ProductSafetyEnforcementScore   float64          `json:"productSafetyEnforcementScore,omitempty"`
	CounterfeitSeizurePressureScore float64          `json:"counterfeitSeizurePressureScore,omitempty"`
	RecallExposureScore             float64          `json:"recallExposureScore,omitempty"`
	InformalChannelLeakageScore     float64          `json:"informalChannelLeakageScore,omitempty"`
	ConsumerTrustScore              float64          `json:"consumerTrustScore,omitempty"`
	OperationalScore                float64          `json:"operationalScore,omitempty"`
	BottleneckRef                   string           `json:"bottleneckRef,omitempty"`
	DominantReasons                 []string         `json:"dominantReasons,omitempty"`
	Legs                            []OperationalLeg `json:"legs,omitempty"`
}

type OperationalLeg struct {
	Index                           int      `json:"index"`
	SupplierRef                     string   `json:"supplierRef,omitempty"`
	CustomerRef                     string   `json:"customerRef,omitempty"`
	ProductRef                      string   `json:"productRef,omitempty"`
	ConstraintRefs                  []string `json:"constraintRefs,omitempty"`
	BlockageScore                   float64  `json:"blockageScore,omitempty"`
	TransitDelayHours               float64  `json:"transitDelayHours,omitempty"`
	ScreeningIntensityScore         float64  `json:"screeningIntensityScore,omitempty"`
	CustomsDelayHours               float64  `json:"customsDelayHours,omitempty"`
	RerouteCostMultiplier           float64  `json:"rerouteCostMultiplier,omitempty"`
	InsuranceAvailability           float64  `json:"insuranceAvailability,omitempty"`
	SettlementReliabilityScore      float64  `json:"settlementReliabilityScore,omitempty"`
	ProductSafetyEnforcementScore   float64  `json:"productSafetyEnforcementScore,omitempty"`
	CounterfeitSeizurePressureScore float64  `json:"counterfeitSeizurePressureScore,omitempty"`
	RecallExposureScore             float64  `json:"recallExposureScore,omitempty"`
	InformalChannelLeakageScore     float64  `json:"informalChannelLeakageScore,omitempty"`
	ConsumerTrustScore              float64  `json:"consumerTrustScore,omitempty"`
	OperationalScore                float64  `json:"operationalScore,omitempty"`
	DominantReasons                 []string `json:"dominantReasons,omitempty"`
}

type OperationalHydrator struct {
	fragments     *horizonrepo.HorizonFragmentsRepository
	materialized  *horizonrepo.MaterializedArtifactsRepository
	genai         GenAIClient
	logger        *log.Logger
	scopeResolver *workscope.Resolver
}

func NewOperationalHydrator(fragments *horizonrepo.HorizonFragmentsRepository, materialized *horizonrepo.MaterializedArtifactsRepository, genai GenAIClient, logger *log.Logger) *OperationalHydrator {
	if logger == nil {
		logger = log.Default()
	}
	return &OperationalHydrator{
		fragments:     fragments,
		materialized:  materialized,
		genai:         genai,
		logger:        logger,
		scopeResolver: workscope.NewResolver(materialized),
	}
}

func SnapshotPreviewOverlay(snapshot *OperationalSnapshot) map[string]any {
	if snapshot == nil {
		return nil
	}
	metrics := map[string]any{
		"operational_target_count":                     snapshot.Summary.TargetCount,
		"operational_targets_refreshed":                snapshot.Summary.RefreshedTargetCount,
		"operational_missing_metric_count":             snapshot.Summary.MissingMetricCount,
		"operational_blocked_target_count":             snapshot.Summary.BlockedTargetCount,
		"operational_high_friction_paths":              snapshot.Summary.HighFrictionPathCount,
		"operational_blockage_max":                     round3(snapshot.Summary.BlockageMax),
		"operational_transit_delay_max":                round3(snapshot.Summary.TransitDelayHoursMax),
		"operational_screening_intensity_max":          round3(snapshot.Summary.ScreeningIntensityMax),
		"operational_customs_delay_max":                round3(snapshot.Summary.CustomsDelayHoursMax),
		"operational_reroute_cost_max":                 round3(snapshot.Summary.RerouteCostMultiplierMax),
		"operational_insurance_availability_min":       round3(snapshot.Summary.InsuranceAvailabilityMin),
		"operational_settlement_reliability_min":       round3(snapshot.Summary.SettlementReliabilityMin),
		"operational_product_safety_enforcement_max":   round3(snapshot.Summary.ProductSafetyEnforcementMax),
		"operational_counterfeit_seizure_pressure_max": round3(snapshot.Summary.CounterfeitSeizurePressureMax),
		"operational_recall_exposure_max":              round3(snapshot.Summary.RecallExposureMax),
		"operational_informal_channel_leakage_max":     round3(snapshot.Summary.InformalChannelLeakageMax),
		"operational_consumer_trust_min":               round3(snapshot.Summary.ConsumerTrustMin),
	}
	out := map[string]any{
		"operationalSnapshot": snapshot,
		"metrics":             metrics,
	}
	for key, value := range metrics {
		out[key] = value
	}
	out["operational_bottleneck_refs"] = append([]string(nil), snapshot.Summary.BottleneckRefs...)
	return out
}

func (h *OperationalHydrator) Hydrate(ctx context.Context, req OperationalHydrationRequest) (*OperationalSnapshot, error) {
	if h == nil || h.fragments == nil || h.materialized == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	ws := workscope.FromBaselineRefs(req.TenantID, "", req.BaselineRefs)
	if strings.TrimSpace(ws.Refs.LensID) == "" && strings.TrimSpace(req.LensID) != "" {
		ws.Refs.LensID = strings.TrimSpace(req.LensID)
		ws = workscope.Normalize(ws)
	}
	resolved, _ := h.scopeResolver.Resolve(ctx, ws)
	refsNorm := workscope.ToBaselineRefs(ws)
	profile := simscope.BuildProfile(strings.TrimSpace(req.Question), nil, refsNorm, req.Recognition)
	artifacts := h.collectArtifactsFromResolved(ctx, resolved, profile)
	targets := h.collectTargets(ctx, req.TenantID, req.Recognition, artifacts, refsNorm.ScopePaths, profile, now)
	missingCount := 0
	toRefresh := make([]targetBinding, 0, len(targets))
	for i := range targets {
		if len(targets[i].refreshReasons) == 0 {
			continue
		}
		missingCount += len(targets[i].refreshReasons)
		toRefresh = append(toRefresh, targets[i])
	}
	updates := make([]OperationalTargetUpdate, 0, len(toRefresh))
	if len(toRefresh) > 0 && h.genai != nil {
		refreshed, err := h.refreshTargets(ctx, req, toRefresh[:min(len(toRefresh), maxOperationalRefreshTargets)], now)
		if err != nil && h.logger != nil {
			h.logger.Printf("[metriccore] operational refresh warning lens=%s err=%v", strings.TrimSpace(req.BaselineRefs.LensID), err)
		}
		for _, update := range refreshed {
			updates = append(updates, update.Update)
			for i := range targets {
				if targets[i].fragment == nil || strings.TrimSpace(targets[i].fragment.CanonicalPath) != strings.TrimSpace(update.Ref) {
					continue
				}
				targets[i].fragment = update.Fragment
				targets[i].refreshReasons = nil
			}
		}
	}
	snapshot := &OperationalSnapshot{
		Targets:     make([]OperationalTarget, 0, len(targets)),
		Paths:       h.buildPathConstraints(artifacts, targets),
		Refreshed:   updates,
		Missing:     nil,
		GeneratedAt: now,
	}
	summary := OperationalSummary{
		TargetCount:              len(targets),
		RefreshedTargetCount:     len(updates),
		MissingMetricCount:       missingCount,
		InsuranceAvailabilityMin: 1,
		SettlementReliabilityMin: 1,
		ConsumerTrustMin:         1,
	}
	for _, binding := range targets {
		target := binding.toSnapshot(now)
		snapshot.Targets = append(snapshot.Targets, target)
		if strings.EqualFold(target.Class, "chokepoint") && metricValue(target.Metrics, operationalMetricDefinitions["chokepointBlockageScore"].Path) >= 0.7 {
			summary.BlockedTargetCount++
		}
		if blockage := metricValue(target.Metrics, operationalMetricDefinitions["chokepointBlockageScore"].Path); blockage > summary.BlockageMax {
			summary.BlockageMax = blockage
		}
		if delay := metricValue(target.Metrics, operationalMetricDefinitions["transitDelayHours"].Path); delay > summary.TransitDelayHoursMax {
			summary.TransitDelayHoursMax = delay
		}
		if screening := metricValue(target.Metrics, operationalMetricDefinitions["screeningIntensityScore"].Path); screening > summary.ScreeningIntensityMax {
			summary.ScreeningIntensityMax = screening
		}
		if customs := metricValue(target.Metrics, operationalMetricDefinitions["customsDelayHours"].Path); customs > summary.CustomsDelayHoursMax {
			summary.CustomsDelayHoursMax = customs
		}
		if reroute := metricValue(target.Metrics, operationalMetricDefinitions["rerouteCostMultiplier"].Path); reroute > summary.RerouteCostMultiplierMax {
			summary.RerouteCostMultiplierMax = reroute
		}
		if insurance := metricValue(target.Metrics, operationalMetricDefinitions["insuranceCoverAvailability"].Path); insurance > 0 && insurance < summary.InsuranceAvailabilityMin {
			summary.InsuranceAvailabilityMin = insurance
		}
		if settlement := metricValue(target.Metrics, operationalMetricDefinitions["settlementReliabilityScore"].Path); settlement > 0 && settlement < summary.SettlementReliabilityMin {
			summary.SettlementReliabilityMin = settlement
		}
		if safety := metricValue(target.Metrics, operationalMetricDefinitions["productSafetyEnforcementScore"].Path); safety > summary.ProductSafetyEnforcementMax {
			summary.ProductSafetyEnforcementMax = safety
		}
		if counterfeit := metricValue(target.Metrics, operationalMetricDefinitions["counterfeitSeizurePressureScore"].Path); counterfeit > summary.CounterfeitSeizurePressureMax {
			summary.CounterfeitSeizurePressureMax = counterfeit
		}
		if recall := metricValue(target.Metrics, operationalMetricDefinitions["recallExposureScore"].Path); recall > summary.RecallExposureMax {
			summary.RecallExposureMax = recall
		}
		if leakage := metricValue(target.Metrics, operationalMetricDefinitions["informalChannelLeakageScore"].Path); leakage > summary.InformalChannelLeakageMax {
			summary.InformalChannelLeakageMax = leakage
		}
		if trust := metricValue(target.Metrics, operationalMetricDefinitions["consumerTrustScore"].Path); trust > 0 && trust < summary.ConsumerTrustMin {
			summary.ConsumerTrustMin = trust
		}
		if targetIsDegraded(target) {
			summary.DegradedTargetRefs = append(summary.DegradedTargetRefs, target.Ref)
		}
	}
	if summary.InsuranceAvailabilityMin == 1 {
		summary.InsuranceAvailabilityMin = 0
	}
	if summary.SettlementReliabilityMin == 1 {
		summary.SettlementReliabilityMin = 0
	}
	if summary.ConsumerTrustMin == 1 {
		summary.ConsumerTrustMin = 0
	}
	for _, path := range snapshot.Paths {
		if path.OperationalScore >= 0.55 {
			summary.HighFrictionPathCount++
		}
		if strings.TrimSpace(path.BottleneckRef) != "" {
			summary.BottleneckRefs = append(summary.BottleneckRefs, path.BottleneckRef)
		}
	}
	summary.BottleneckRefs = compactStrings(summary.BottleneckRefs)
	summary.DegradedTargetRefs = compactStrings(summary.DegradedTargetRefs)
	snapshot.Summary = summary
	return snapshot, nil
}

func inferOperationalTargetContract(fragment *horizonmodels.HorizonFragment) (string, []MetricRequirement) {
	if fragment == nil {
		return "", nil
	}
	path := strings.TrimSpace(fragment.CanonicalPath)
	marketChannelText := strings.ToLower(strings.Join(fragment.MarketChannels, " "))
	text := strings.ToLower(strings.Join(compactStrings([]string{
		path,
		strings.TrimSpace(fragment.Labels["en"]),
		strings.TrimSpace(fragment.Description),
		strings.TrimSpace(fragment.GeoFeatureType),
		strings.TrimSpace(fragment.GovType),
		strings.TrimSpace(fragment.MarketDomain),
		strings.TrimSpace(fragment.MarketVertical),
		marketChannelText,
		strings.Join(fragment.Tags, " "),
		strings.TrimSpace(fragment.Identifiers["facilityKind"]),
	}), " | "))
	addReqs := func(keys ...string) []MetricRequirement {
		reqs := make([]MetricRequirement, 0, len(keys))
		for _, key := range keys {
			def := operationalMetricDefinitions[key]
			reqs = append(reqs, MetricRequirement{
				Path:          def.Path,
				MaxAge:        def.MaxAge,
				MinConfidence: def.MinConfidence,
			})
		}
		return reqs
	}
	switch {
	case strings.HasPrefix(path, "/product/"):
		if strings.Contains(text, "toy") || strings.Contains(text, "game") || strings.Contains(text, "child") || strings.Contains(text, "consumer") {
			return "product_safety", addReqs("productSafetyEnforcementScore", "counterfeitSeizurePressureScore", "recallExposureScore", "consumerTrustScore")
		}
		return "", nil
	case strings.HasPrefix(path, "/domain/"):
		if strings.Contains(text, "toy") || strings.Contains(text, "consumer") || strings.Contains(text, "market") {
			return "market_surveillance", addReqs("informalChannelLeakageScore", "consumerTrustScore", "counterfeitSeizurePressureScore", "productSafetyEnforcementScore")
		}
		return "", nil
	case fragment.GeoFeatureType == "strait" || fragment.GeoFeatureType == "canal" || fragment.GeoFeatureType == "pass" || fragment.GeoFeatureType == "pipeline-corridor" ||
		strings.Contains(text, "hormuz") || strings.Contains(text, "suez") || strings.Contains(text, "bab-el-mandeb") || strings.Contains(text, "malacca") || strings.Contains(text, "strait") || strings.Contains(text, "chokepoint") ||
		strings.Contains(path, "checkpoint") || strings.Contains(path, "border-crossing") || strings.Contains(path, "land-port"):
		return "chokepoint", addReqs("chokepointBlockageScore", "transitDelayHours", "routeReliabilityScore", "rerouteCostMultiplier")
	case strings.Contains(text, "customs") || strings.Contains(text, "screening") || strings.Contains(text, "license") || strings.Contains(text, "regulator") || strings.Contains(text, "authority") ||
		strings.Contains(text, "revenue committee") || strings.Contains(text, "state revenue"):
		return "compliance_gate", addReqs("screeningIntensityScore", "customsDelayHours", "counterfeitSeizurePressureScore")
	case strings.Contains(text, "insurance") || strings.Contains(text, "insurer") || strings.Contains(text, "settlement") || strings.Contains(text, "bank") || strings.Contains(text, "trade credit"):
		return "finance_gate", addReqs("insuranceCoverAvailability", "settlementReliabilityScore")
	case strings.Contains(text, "consumer protection") || strings.Contains(text, "toy safety") || strings.Contains(text, "child safety") || strings.Contains(text, "product safety") ||
		strings.Contains(text, "technical regulation") || strings.Contains(text, "conformity") || strings.Contains(text, "certification") || strings.Contains(text, "inspection") ||
		strings.Contains(text, "recall") || strings.Contains(text, "standards"):
		return "product_safety", addReqs("productSafetyEnforcementScore", "counterfeitSeizurePressureScore", "recallExposureScore", "consumerTrustScore")
	case fragment.Class == FragmentTypeMarket ||
		fragment.IsLegacyDomain() ||
		strings.Contains(path, "/market/") ||
		(strings.Contains(path, "/domain/") && (strings.Contains(path, "/product/") || strings.Contains(text, "toy") || strings.Contains(text, "consumer"))) ||
		strings.Contains(text, "retail") || strings.Contains(text, "marketplace") || strings.Contains(text, "ecommerce") || strings.Contains(text, "consumer"):
		return "market_surveillance", addReqs("informalChannelLeakageScore", "consumerTrustScore", "counterfeitSeizurePressureScore", "productSafetyEnforcementScore")
	case strings.Contains(text, "port") || strings.Contains(text, "terminal") || strings.Contains(text, "logistics") || strings.Contains(text, "warehouse") || strings.Contains(text, "hub") ||
		strings.Contains(text, "gateway") || strings.Contains(text, "rail station") || strings.Contains(text, "railway port") || strings.Contains(text, "dry port"):
		return "logistics_hub", addReqs("transitDelayHours", "routeReliabilityScore", "rerouteCostMultiplier")
	default:
		return "", nil
	}
}

type operationalMetricDefinition struct {
	Path          string
	Unit          string
	MaxAge        time.Duration
	MinConfidence float64
}

var operationalMetricDefinitions = map[string]operationalMetricDefinition{
	"chokepointBlockageScore": {
		Path:          "/metric/logistics/chokepoint_blockage_score",
		Unit:          "score_0_1",
		MaxAge:        6 * time.Hour,
		MinConfidence: 0.68,
	},
	"transitDelayHours": {
		Path:          "/metric/logistics/transit_delay_hours",
		Unit:          "hours",
		MaxAge:        24 * time.Hour,
		MinConfidence: 0.64,
	},
	"routeReliabilityScore": {
		Path:          "/metric/logistics/route_reliability_score",
		Unit:          "score_0_1",
		MaxAge:        24 * time.Hour,
		MinConfidence: 0.64,
	},
	"rerouteCostMultiplier": {
		Path:          "/metric/logistics/reroute_cost_multiplier",
		Unit:          "multiplier",
		MaxAge:        24 * time.Hour,
		MinConfidence: 0.62,
	},
	"screeningIntensityScore": {
		Path:          "/metric/compliance/screening_intensity_score",
		Unit:          "score_0_1",
		MaxAge:        72 * time.Hour,
		MinConfidence: 0.62,
	},
	"customsDelayHours": {
		Path:          "/metric/compliance/customs_delay_hours",
		Unit:          "hours",
		MaxAge:        72 * time.Hour,
		MinConfidence: 0.62,
	},
	"insuranceCoverAvailability": {
		Path:          "/metric/finance/insurance_cover_availability",
		Unit:          "score_0_1",
		MaxAge:        72 * time.Hour,
		MinConfidence: 0.62,
	},
	"settlementReliabilityScore": {
		Path:          "/metric/finance/settlement_reliability_score",
		Unit:          "score_0_1",
		MaxAge:        72 * time.Hour,
		MinConfidence: 0.62,
	},
	"productSafetyEnforcementScore": {
		Path:          "/metric/compliance/product_safety_enforcement_score",
		Unit:          "score_0_1",
		MaxAge:        72 * time.Hour,
		MinConfidence: 0.62,
	},
	"counterfeitSeizurePressureScore": {
		Path:          "/metric/compliance/counterfeit_seizure_pressure_score",
		Unit:          "score_0_1",
		MaxAge:        72 * time.Hour,
		MinConfidence: 0.62,
	},
	"recallExposureScore": {
		Path:          "/metric/product/recall_exposure_score",
		Unit:          "score_0_1",
		MaxAge:        72 * time.Hour,
		MinConfidence: 0.60,
	},
	"informalChannelLeakageScore": {
		Path:          "/metric/market/informal_channel_leakage_score",
		Unit:          "score_0_1",
		MaxAge:        72 * time.Hour,
		MinConfidence: 0.60,
	},
	"consumerTrustScore": {
		Path:          "/metric/market/consumer_trust_score",
		Unit:          "score_0_1",
		MaxAge:        72 * time.Hour,
		MinConfidence: 0.60,
	},
}

type groundedHydrationEnvelope struct {
	Results []groundedHydrationResult `json:"results"`
}

type groundedHydrationResult struct {
	TargetRef    string                    `json:"targetRef"`
	Summary      string                    `json:"summary,omitempty"`
	EvidenceURLs []string                  `json:"evidenceUrls,omitempty"`
	Metrics      []groundedMetricCandidate `json:"metrics,omitempty"`
}

type groundedMetricCandidate struct {
	Key         string   `json:"key"`
	Value       float64  `json:"value"`
	Unit        string   `json:"unit,omitempty"`
	Confidence  float64  `json:"confidence"`
	ObservedAt  string   `json:"observedAt,omitempty"`
	Description string   `json:"description,omitempty"`
	SourceKind  string   `json:"sourceKind,omitempty"`
	SourceURLs  []string `json:"sourceUrls,omitempty"`
}

func (h *OperationalHydrator) refreshTargets(ctx context.Context, req OperationalHydrationRequest, bindings []targetBinding, now time.Time) ([]refreshedTarget, error) {
	if len(bindings) == 0 {
		return nil, nil
	}
	updates := make([]refreshedTarget, 0, len(bindings))
	errs := make([]string, 0)
	for start := 0; start < len(bindings); start += operationalRefreshBatchSize {
		end := min(len(bindings), start+operationalRefreshBatchSize)
		chunk := bindings[start:end]
		refreshed, err := h.refreshTargetBatch(ctx, req, chunk, now)
		if err == nil {
			updates = append(updates, refreshed...)
			continue
		}
		if len(chunk) > 1 {
			for _, single := range chunk {
				one, singleErr := h.refreshTargetBatch(ctx, req, []targetBinding{single}, now)
				if singleErr != nil {
					errs = append(errs, fmt.Sprintf("%s: %v", strings.TrimSpace(single.fragment.CanonicalPath), singleErr))
					continue
				}
				updates = append(updates, one...)
			}
			continue
		}
		if chunk[0].fragment != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", strings.TrimSpace(chunk[0].fragment.CanonicalPath), err))
		} else {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return updates, fmt.Errorf("%s", strings.Join(compactStrings(errs), "; "))
	}
	return updates, nil
}

func (h *OperationalHydrator) refreshTargetBatch(ctx context.Context, req OperationalHydrationRequest, bindings []targetBinding, now time.Time) ([]refreshedTarget, error) {
	payload := make([]map[string]any, 0, len(bindings))
	for _, binding := range bindings {
		if binding.fragment == nil {
			continue
		}
		payload = append(payload, map[string]any{
			"targetRef":       strings.TrimSpace(binding.fragment.CanonicalPath),
			"title":           strings.TrimSpace(binding.fragment.Labels["en"]),
			"class":           binding.class,
			"whyInScope":      binding.whyInScope,
			"requiredMetrics": metricNames(binding.requirements),
			"refreshReasons":  binding.refreshReasons,
		})
	}
	if len(payload) == 0 {
		return nil, nil
	}
	prompt := buildOperationalHydrationPrompt(req, payload, now)
	resp, err := h.genai.GenAIGenerate(ctx, gencombined.GenAIRequest{
		Model:             "gemini-3-flash-preview",
		Prompt:            prompt,
		WebSearchEnabled:  true,
		StrictJSONEnabled: true,
		MaxOutputTokens:   16384,
		JSONSchema:        operationalHydrationSchema(),
	})
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(resp.Content)
	envelope, err := parseGroundedHydrationEnvelope(content)
	if err != nil {
		if h.logger != nil {
			h.logger.Printf("[metriccore] operational refresh raw_response lens=%s targets=%d snippet=%q", strings.TrimSpace(req.BaselineRefs.LensID), len(payload), truncateForLog(content, 1200))
		}
		return nil, fmt.Errorf("decode operational hydration: %w", err)
	}
	updates := make([]refreshedTarget, 0, len(envelope.Results))
	groundedSources := groundedSourceURLs(resp)
	for _, result := range envelope.Results {
		ref := strings.TrimSpace(result.TargetRef)
		if ref == "" {
			continue
		}
		var binding *targetBinding
		for i := range bindings {
			if bindings[i].fragment != nil && strings.TrimSpace(bindings[i].fragment.CanonicalPath) == ref {
				binding = &bindings[i]
				break
			}
		}
		if binding == nil || binding.fragment == nil {
			continue
		}
		fragment := binding.fragment
		if fragment.State == nil {
			fragment.State = map[string]horizonmodels.MetricValue{}
		}
		written := make([]string, 0, len(result.Metrics))
		observedAt := now
		for _, metric := range result.Metrics {
			def, ok := operationalMetricDefinitions[strings.TrimSpace(metric.Key)]
			if !ok {
				continue
			}
			obsAt, ok := parseObservedAt(metric.ObservedAt, now)
			if !ok {
				obsAt = now
			}
			freshUntil := obsAt.Add(def.MaxAge)
			unit := strings.TrimSpace(metric.Unit)
			if unit == "" {
				unit = def.Unit
			}
			mv := NormalizeMetricValue(horizonmodels.MetricValue{
				Value:      normalizeMetricValue(def.Path, metric.Value),
				Unit:       optionalString(unit),
				Confidence: metric.Confidence,
				ObservedAt: obsAt,
				Provenance: horizonmodels.Provenance{
					Method:     "operational_hydrator_grounded_search",
					SourceRefs: buildSourceRefs(preferredGroundedSources(groundedSources, metric.SourceURLs, result.EvidenceURLs), nil),
				},
			}, firstNonEmptyString(strings.TrimSpace(metric.SourceKind), sourceKindGroundedSearch), &freshUntil)
			if cur, ok := fragment.State[def.Path]; !ok || BetterMetricValue(cur, mv) {
				fragment.State[def.Path] = mv
				written = append(written, def.Path)
			}
			if obsAt.After(observedAt) {
				observedAt = obsAt
			}
		}
		if len(written) == 0 {
			continue
		}
		if err := h.fragments.UpdateDerived(ctx, fragment); err != nil {
			return nil, err
		}
		updates = append(updates, refreshedTarget{
			Ref:      ref,
			Fragment: fragment,
			Update: OperationalTargetUpdate{
				Ref:          ref,
				MetricPaths:  compactStrings(written),
				Source:       sourceKindGroundedSearch,
				ObservedAt:   observedAt,
				EvidenceURLs: preferredGroundedSources(groundedSources, result.EvidenceURLs),
			},
		})
	}
	return updates, nil
}

func groundedSourceURLs(resp *gencombined.GenAIResponse) []string {
	if resp == nil {
		return nil
	}
	values := make([]string, 0, len(resp.Sources)+len(resp.Attributions))
	values = append(values, compactStrings(resp.Sources)...)
	for _, attr := range resp.Attributions {
		values = append(values, strings.TrimSpace(attr.URL))
	}
	return compactStrings(values)
}

func preferredGroundedSources(groundedSources []string, fallback ...[]string) []string {
	if preferred := compactStrings(groundedSources); len(preferred) > 0 {
		return preferred
	}
	values := make([]string, 0)
	for _, group := range fallback {
		values = append(values, compactStrings(group)...)
	}
	return compactStrings(values)
}

func (h *OperationalHydrator) buildPathConstraints(artifacts []*horizonmodels.MaterializedArtifact, targets []targetBinding) []OperationalPath {
	targetMetrics := make(map[string]map[string]horizonmodels.MetricValue, len(targets))
	for _, target := range targets {
		if target.fragment == nil {
			continue
		}
		targetMetrics[strings.TrimSpace(target.fragment.CanonicalPath)] = target.fragment.State
	}
	out := make([]OperationalPath, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			continue
		}
		horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
		legs, err := horizonmodels.GetSupplyChainPathLegs(artifact)
		if err != nil {
			continue
		}
		path := OperationalPath{
			ArtifactID:           strings.TrimSpace(artifact.ID),
			ArtifactKey:          strings.TrimSpace(artifact.CanonicalKey),
			Title:                firstNonEmptyString(strings.TrimSpace(artifact.DisplayName), strings.TrimSpace(artifact.Labels["title"])),
			PathStatus:           strings.TrimSpace(artifact.PathStatus),
			ProductAxis:          strings.TrimSpace(artifact.ProductAxis),
			PathStressScore:      round3(artifact.PathStressScore),
			BindingScore:         round3(artifact.BindingScore),
			RecoveryLatencyHours: round3(artifact.RecoveryLatencyHrs),
			Legs:                 make([]OperationalLeg, 0, len(legs)),
		}
		for _, ref := range compactStrings(append(append(append(append([]string{}, artifact.NodeRefs...), artifact.GeoRefs...), artifact.ZoneRefs...), artifact.AffectedZoneRefs...)) {
			metrics := targetMetrics[strings.TrimSpace(ref)]
			if len(metrics) == 0 {
				continue
			}
			path.BlockageScore = maxFloat(path.BlockageScore, metricValue(metrics, operationalMetricDefinitions["chokepointBlockageScore"].Path))
			path.TransitDelayHours = maxFloat(path.TransitDelayHours, metricValue(metrics, operationalMetricDefinitions["transitDelayHours"].Path))
			path.ScreeningIntensityScore = maxFloat(path.ScreeningIntensityScore, metricValue(metrics, operationalMetricDefinitions["screeningIntensityScore"].Path))
			path.CustomsDelayHours = maxFloat(path.CustomsDelayHours, metricValue(metrics, operationalMetricDefinitions["customsDelayHours"].Path))
			path.RerouteCostMultiplier = maxFloat(path.RerouteCostMultiplier, metricValue(metrics, operationalMetricDefinitions["rerouteCostMultiplier"].Path))
			path.ProductSafetyEnforcementScore = maxFloat(path.ProductSafetyEnforcementScore, metricValue(metrics, operationalMetricDefinitions["productSafetyEnforcementScore"].Path))
			path.CounterfeitSeizurePressureScore = maxFloat(path.CounterfeitSeizurePressureScore, metricValue(metrics, operationalMetricDefinitions["counterfeitSeizurePressureScore"].Path))
			path.RecallExposureScore = maxFloat(path.RecallExposureScore, metricValue(metrics, operationalMetricDefinitions["recallExposureScore"].Path))
			path.InformalChannelLeakageScore = maxFloat(path.InformalChannelLeakageScore, metricValue(metrics, operationalMetricDefinitions["informalChannelLeakageScore"].Path))
			if insurance := metricValue(metrics, operationalMetricDefinitions["insuranceCoverAvailability"].Path); insurance > 0 {
				if path.InsuranceAvailability == 0 || insurance < path.InsuranceAvailability {
					path.InsuranceAvailability = insurance
				}
			}
			if settlement := metricValue(metrics, operationalMetricDefinitions["settlementReliabilityScore"].Path); settlement > 0 {
				if path.SettlementReliabilityScore == 0 || settlement < path.SettlementReliabilityScore {
					path.SettlementReliabilityScore = settlement
				}
			}
			if trust := metricValue(metrics, operationalMetricDefinitions["consumerTrustScore"].Path); trust > 0 {
				if path.ConsumerTrustScore == 0 || trust < path.ConsumerTrustScore {
					path.ConsumerTrustScore = trust
				}
			}
		}
		bottleneckScore := -1.0
		for _, leg := range legs {
			legConstraint := buildLegConstraint(leg, targetMetrics)
			path.Legs = append(path.Legs, legConstraint)
			path.BlockageScore = maxFloat(path.BlockageScore, legConstraint.BlockageScore)
			path.TransitDelayHours = maxFloat(path.TransitDelayHours, legConstraint.TransitDelayHours)
			path.ScreeningIntensityScore = maxFloat(path.ScreeningIntensityScore, legConstraint.ScreeningIntensityScore)
			path.CustomsDelayHours = maxFloat(path.CustomsDelayHours, legConstraint.CustomsDelayHours)
			path.RerouteCostMultiplier = maxFloat(path.RerouteCostMultiplier, legConstraint.RerouteCostMultiplier)
			if legConstraint.InsuranceAvailability > 0 {
				if path.InsuranceAvailability == 0 || legConstraint.InsuranceAvailability < path.InsuranceAvailability {
					path.InsuranceAvailability = legConstraint.InsuranceAvailability
				}
			}
			if legConstraint.SettlementReliabilityScore > 0 {
				if path.SettlementReliabilityScore == 0 || legConstraint.SettlementReliabilityScore < path.SettlementReliabilityScore {
					path.SettlementReliabilityScore = legConstraint.SettlementReliabilityScore
				}
			}
			path.ProductSafetyEnforcementScore = maxFloat(path.ProductSafetyEnforcementScore, legConstraint.ProductSafetyEnforcementScore)
			path.CounterfeitSeizurePressureScore = maxFloat(path.CounterfeitSeizurePressureScore, legConstraint.CounterfeitSeizurePressureScore)
			path.RecallExposureScore = maxFloat(path.RecallExposureScore, legConstraint.RecallExposureScore)
			path.InformalChannelLeakageScore = maxFloat(path.InformalChannelLeakageScore, legConstraint.InformalChannelLeakageScore)
			if legConstraint.ConsumerTrustScore > 0 {
				if path.ConsumerTrustScore == 0 || legConstraint.ConsumerTrustScore < path.ConsumerTrustScore {
					path.ConsumerTrustScore = legConstraint.ConsumerTrustScore
				}
			}
			if legConstraint.OperationalScore > bottleneckScore {
				bottleneckScore = legConstraint.OperationalScore
				if len(legConstraint.ConstraintRefs) > 0 {
					path.BottleneckRef = legConstraint.ConstraintRefs[0]
				}
				path.DominantReasons = append([]string(nil), legConstraint.DominantReasons...)
			}
		}
		path.OperationalScore = round3(pathOperationalScore(path))
		path.BlockageScore = round3(path.BlockageScore)
		path.TransitDelayHours = round3(path.TransitDelayHours)
		path.ScreeningIntensityScore = round3(path.ScreeningIntensityScore)
		path.CustomsDelayHours = round3(path.CustomsDelayHours)
		path.RerouteCostMultiplier = round3(path.RerouteCostMultiplier)
		path.InsuranceAvailability = round3(path.InsuranceAvailability)
		path.SettlementReliabilityScore = round3(path.SettlementReliabilityScore)
		path.ProductSafetyEnforcementScore = round3(path.ProductSafetyEnforcementScore)
		path.CounterfeitSeizurePressureScore = round3(path.CounterfeitSeizurePressureScore)
		path.RecallExposureScore = round3(path.RecallExposureScore)
		path.InformalChannelLeakageScore = round3(path.InformalChannelLeakageScore)
		path.ConsumerTrustScore = round3(path.ConsumerTrustScore)
		out = append(out, path)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OperationalScore == out[j].OperationalScore {
			return out[i].ArtifactKey < out[j].ArtifactKey
		}
		return out[i].OperationalScore > out[j].OperationalScore
	})
	return out
}

func buildLegConstraint(leg horizonmodels.SupplyChainPathLeg, targetMetrics map[string]map[string]horizonmodels.MetricValue) OperationalLeg {
	out := OperationalLeg{
		Index:       leg.Index,
		SupplierRef: strings.TrimSpace(leg.SupplierRef),
		CustomerRef: strings.TrimSpace(leg.CustomerRef),
		ProductRef:  strings.TrimSpace(leg.ProductRef),
	}
	refs := compactStrings(append(append(append([]string{}, leg.TransportViaRefs...), leg.GeoRefs...), append(leg.ZoneRefs, leg.AffectedZoneRefs...)...))
	out.ConstraintRefs = refs
	for _, ref := range refs {
		metrics := targetMetrics[strings.TrimSpace(ref)]
		if len(metrics) == 0 {
			continue
		}
		out.BlockageScore = maxFloat(out.BlockageScore, metricValue(metrics, operationalMetricDefinitions["chokepointBlockageScore"].Path))
		out.TransitDelayHours = maxFloat(out.TransitDelayHours, metricValue(metrics, operationalMetricDefinitions["transitDelayHours"].Path))
		out.ScreeningIntensityScore = maxFloat(out.ScreeningIntensityScore, metricValue(metrics, operationalMetricDefinitions["screeningIntensityScore"].Path))
		out.CustomsDelayHours = maxFloat(out.CustomsDelayHours, metricValue(metrics, operationalMetricDefinitions["customsDelayHours"].Path))
		out.RerouteCostMultiplier = maxFloat(out.RerouteCostMultiplier, metricValue(metrics, operationalMetricDefinitions["rerouteCostMultiplier"].Path))
		out.ProductSafetyEnforcementScore = maxFloat(out.ProductSafetyEnforcementScore, metricValue(metrics, operationalMetricDefinitions["productSafetyEnforcementScore"].Path))
		out.CounterfeitSeizurePressureScore = maxFloat(out.CounterfeitSeizurePressureScore, metricValue(metrics, operationalMetricDefinitions["counterfeitSeizurePressureScore"].Path))
		out.RecallExposureScore = maxFloat(out.RecallExposureScore, metricValue(metrics, operationalMetricDefinitions["recallExposureScore"].Path))
		out.InformalChannelLeakageScore = maxFloat(out.InformalChannelLeakageScore, metricValue(metrics, operationalMetricDefinitions["informalChannelLeakageScore"].Path))
		if insurance := metricValue(metrics, operationalMetricDefinitions["insuranceCoverAvailability"].Path); insurance > 0 {
			if out.InsuranceAvailability == 0 || insurance < out.InsuranceAvailability {
				out.InsuranceAvailability = insurance
			}
		}
		if settlement := metricValue(metrics, operationalMetricDefinitions["settlementReliabilityScore"].Path); settlement > 0 {
			if out.SettlementReliabilityScore == 0 || settlement < out.SettlementReliabilityScore {
				out.SettlementReliabilityScore = settlement
			}
		}
		if trust := metricValue(metrics, operationalMetricDefinitions["consumerTrustScore"].Path); trust > 0 {
			if out.ConsumerTrustScore == 0 || trust < out.ConsumerTrustScore {
				out.ConsumerTrustScore = trust
			}
		}
	}
	out.OperationalScore = round3(legOperationalScore(leg, out))
	out.DominantReasons = dominantOperationalReasons(out)
	out.TransitDelayHours = round3(out.TransitDelayHours)
	out.BlockageScore = round3(out.BlockageScore)
	out.ScreeningIntensityScore = round3(out.ScreeningIntensityScore)
	out.CustomsDelayHours = round3(out.CustomsDelayHours)
	out.RerouteCostMultiplier = round3(out.RerouteCostMultiplier)
	out.InsuranceAvailability = round3(out.InsuranceAvailability)
	out.SettlementReliabilityScore = round3(out.SettlementReliabilityScore)
	out.ProductSafetyEnforcementScore = round3(out.ProductSafetyEnforcementScore)
	out.CounterfeitSeizurePressureScore = round3(out.CounterfeitSeizurePressureScore)
	out.RecallExposureScore = round3(out.RecallExposureScore)
	out.InformalChannelLeakageScore = round3(out.InformalChannelLeakageScore)
	out.ConsumerTrustScore = round3(out.ConsumerTrustScore)
	return out
}

func (binding targetBinding) toSnapshot(now time.Time) OperationalTarget {
	metrics := make(map[string]horizonmodels.MetricValue)
	if binding.fragment != nil {
		for _, requirement := range binding.requirements {
			if metric, ok := binding.fragment.State[strings.TrimSpace(requirement.Path)]; ok {
				metrics[strings.TrimSpace(requirement.Path)] = metric
			}
		}
	}
	confidence := 0.0
	freshness := "stale"
	if len(metrics) > 0 {
		for _, metric := range metrics {
			confidence = maxFloat(confidence, metric.Confidence)
			if metric.FreshUntil != nil && metric.FreshUntil.After(now) {
				freshness = "fresh"
			} else if MetricObservedAt(metric).After(now.Add(-24 * time.Hour)) {
				freshness = "fresh"
			}
		}
	}
	return OperationalTarget{
		Ref:            strings.TrimSpace(binding.fragment.CanonicalPath),
		Name:           strings.TrimSpace(binding.fragment.Labels["en"]),
		Class:          binding.class,
		WhyInScope:     binding.whyInScope,
		RefreshReasons: append([]string(nil), binding.refreshReasons...),
		Metrics:        metrics,
		Refreshed:      len(binding.refreshReasons) == 0,
		Confidence:     round3(confidence),
		Freshness:      freshness,
	}
}

func buildOperationalHydrationPrompt(req OperationalHydrationRequest, targets []map[string]any, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You hydrate live operational metrics for a Horizon supply-chain scope as of %s.\n", now.Format("2006-01-02"))
	fmt.Fprintf(&b, "Question: %s\n", strings.TrimSpace(req.Question))
	if len(req.DomainModes) > 0 {
		fmt.Fprintf(&b, "Domain modes: %s\n", strings.Join(compactStrings(req.DomainModes), ", "))
	}
	if len(targets) == 0 {
		b.WriteString("No targets.\n")
		return b.String()
	}
	b.WriteString("Return EXACTLY ONE JSON OBJECT with top-level key \"results\".\n")
	b.WriteString("Do not return markdown, prose, code fences, comments, or any keys other than the schema keys.\n")
	b.WriteString("Do not invent new targets, routes, entities, or metric names.\n")
	b.WriteString("Every item in results MUST use one of the exact targetRef values listed below.\n")
	b.WriteString("Keep the response compact so it stays valid JSON.\n")
	b.WriteString("Use at most 2 evidenceUrls per target and at most 2 sourceUrls per metric.\n")
	b.WriteString("Only include metrics that clearly matter for the target right now; do not pad the list.\n")
	b.WriteString("You may use grounded web evidence. Favor operator advisories, port notices, customs guidance, official institutions, insurers, banks, market infrastructure statements, and high-quality trade press.\n")
	b.WriteString("Metric semantics:\n")
	b.WriteString("- chokepointBlockageScore: 0=open 1=blocked.\n")
	b.WriteString("- transitDelayHours, customsDelayHours: realistic delay estimate.\n")
	b.WriteString("- routeReliabilityScore, insuranceCoverAvailability, settlementReliabilityScore: 0=broken/unavailable 1=healthy.\n")
	b.WriteString("- rerouteCostMultiplier: 1=baseline, >1 more expensive.\n")
	b.WriteString("- screeningIntensityScore: 0=normal processing 1=exceptionally high screening burden.\n")
	b.WriteString("- productSafetyEnforcementScore: 0=routine market surveillance, 1=active child-safety / conformity / inspection crackdown.\n")
	b.WriteString("- counterfeitSeizurePressureScore: 0=little visible seizure pressure, 1=active counterfeit seizure / anti-illicit enforcement.\n")
	b.WriteString("- recallExposureScore: 0=no meaningful live recall/safety alert exposure, 1=severe active recall or safety exposure.\n")
	b.WriteString("- informalChannelLeakageScore: 0=formal channels dominate, 1=grey/informal channels are materially carrying flow.\n")
	b.WriteString("- consumerTrustScore: 0=consumer trust is badly impaired, 1=consumer trust is healthy.\n")
	b.WriteString("Only fill metrics that are directly supported or conservatively bounded from current evidence.\n")
	b.WriteString("Keep summary short: one concise sentence per target with no subordinate clauses. Omit narrative detail that does not change the metric values.\n")
	b.WriteString("If evidence is weak for a target, still return that targetRef with an empty metrics array.\n")
	b.WriteString("Format example:\n")
	b.WriteString("{\"results\":[{\"targetRef\":\"/loc/example\",\"summary\":\"brief grounded summary\",\"evidenceUrls\":[\"https://example.com\"],\"metrics\":[{\"key\":\"chokepointBlockageScore\",\"value\":0.8,\"unit\":\"score\",\"confidence\":0.84,\"observedAt\":\"2026-03-26\",\"description\":\"short reason\",\"sourceKind\":\"grounded_search\",\"sourceUrls\":[\"https://example.com\"]},{\"key\":\"informalChannelLeakageScore\",\"value\":0.55,\"unit\":\"score\",\"confidence\":0.71,\"observedAt\":\"2026-03-26\",\"description\":\"short reason\",\"sourceKind\":\"grounded_search\",\"sourceUrls\":[\"https://example.com\"]}]}]}\n")
	b.WriteString("Targets:\n")
	raw, _ := json.MarshalIndent(targets, "", "  ")
	b.Write(raw)
	return b.String()
}

func operationalHydrationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"results": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]interface{}{
						"targetRef": map[string]interface{}{"type": "string"},
						"summary":   map[string]interface{}{"type": "string"},
						"evidenceUrls": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"metrics": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type":                 "object",
								"additionalProperties": false,
								"properties": map[string]interface{}{
									"key":         map[string]interface{}{"type": "string"},
									"value":       map[string]interface{}{"type": "number"},
									"unit":        map[string]interface{}{"type": "string"},
									"confidence":  map[string]interface{}{"type": "number"},
									"observedAt":  map[string]interface{}{"type": "string"},
									"description": map[string]interface{}{"type": "string"},
									"sourceKind":  map[string]interface{}{"type": "string"},
									"sourceUrls": map[string]interface{}{
										"type":  "array",
										"items": map[string]interface{}{"type": "string"},
									},
								},
								"required": []string{"key", "value", "confidence"},
							},
						},
					},
					"required": []string{"targetRef", "metrics"},
				},
			},
		},
		"required": []string{"results"},
	}
}

func buildSourceRefs(primary []string, fallback []string) []horizonmodels.SourceRef {
	out := make([]horizonmodels.SourceRef, 0, len(primary)+len(fallback))
	for _, raw := range compactStrings(append(primary, fallback...)) {
		out = append(out, horizonmodels.SourceRef{
			SourceID:   raw,
			SourceType: "web",
		})
	}
	return out
}

func metricNames(requirements []MetricRequirement) []string {
	out := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		for key, def := range operationalMetricDefinitions {
			if def.Path == strings.TrimSpace(requirement.Path) {
				out = append(out, key)
			}
		}
	}
	sort.Strings(out)
	return out
}

func metricValue(metrics map[string]horizonmodels.MetricValue, key string) float64 {
	if len(metrics) == 0 {
		return 0
	}
	return metrics[strings.TrimSpace(key)].Value
}

func normalizeMetricValue(metricPath string, value float64) float64 {
	switch metricPath {
	case operationalMetricDefinitions["chokepointBlockageScore"].Path,
		operationalMetricDefinitions["routeReliabilityScore"].Path,
		operationalMetricDefinitions["screeningIntensityScore"].Path,
		operationalMetricDefinitions["insuranceCoverAvailability"].Path,
		operationalMetricDefinitions["settlementReliabilityScore"].Path,
		operationalMetricDefinitions["productSafetyEnforcementScore"].Path,
		operationalMetricDefinitions["counterfeitSeizurePressureScore"].Path,
		operationalMetricDefinitions["recallExposureScore"].Path,
		operationalMetricDefinitions["informalChannelLeakageScore"].Path,
		operationalMetricDefinitions["consumerTrustScore"].Path:
		return clamp01(value)
	case operationalMetricDefinitions["rerouteCostMultiplier"].Path:
		if value < 1 {
			return 1
		}
		return math.Min(value, 5)
	default:
		if value < 0 {
			return 0
		}
		return value
	}
}

func legOperationalScore(leg horizonmodels.SupplyChainPathLeg, constraint OperationalLeg) float64 {
	legStress := 0.0
	if leg.Stress != nil {
		legStress = clamp01(leg.Stress.StressScore)
	}
	delayComponent := clamp01(constraint.TransitDelayHours / 168.0)
	customsComponent := clamp01(constraint.CustomsDelayHours / 96.0)
	rerouteComponent := clamp01((constraint.RerouteCostMultiplier - 1) / 2.0)
	insuranceDeficit := 0.0
	if constraint.InsuranceAvailability > 0 {
		insuranceDeficit = 1 - clamp01(constraint.InsuranceAvailability)
	}
	settlementDeficit := 0.0
	if constraint.SettlementReliabilityScore > 0 {
		settlementDeficit = 1 - clamp01(constraint.SettlementReliabilityScore)
	}
	return clamp01(
		legStress*0.28 +
			constraint.BlockageScore*0.22 +
			delayComponent*0.16 +
			constraint.ScreeningIntensityScore*0.10 +
			customsComponent*0.08 +
			rerouteComponent*0.08 +
			constraint.ProductSafetyEnforcementScore*0.03 +
			constraint.CounterfeitSeizurePressureScore*0.03 +
			constraint.RecallExposureScore*0.02 +
			constraint.InformalChannelLeakageScore*0.02 +
			clamp01(1-constraint.ConsumerTrustScore)*0.02 +
			insuranceDeficit*0.04 +
			settlementDeficit*0.04,
	)
}

func pathOperationalScore(path OperationalPath) float64 {
	delayComponent := clamp01(path.TransitDelayHours / 168.0)
	customsComponent := clamp01(path.CustomsDelayHours / 96.0)
	rerouteComponent := clamp01((path.RerouteCostMultiplier - 1) / 2.0)
	insuranceDeficit := 0.0
	if path.InsuranceAvailability > 0 {
		insuranceDeficit = 1 - clamp01(path.InsuranceAvailability)
	}
	settlementDeficit := 0.0
	if path.SettlementReliabilityScore > 0 {
		settlementDeficit = 1 - clamp01(path.SettlementReliabilityScore)
	}
	return clamp01(
		path.PathStressScore*0.26 +
			path.BlockageScore*0.20 +
			delayComponent*0.14 +
			path.ScreeningIntensityScore*0.10 +
			customsComponent*0.08 +
			rerouteComponent*0.08 +
			path.ProductSafetyEnforcementScore*0.03 +
			path.CounterfeitSeizurePressureScore*0.03 +
			path.RecallExposureScore*0.02 +
			path.InformalChannelLeakageScore*0.02 +
			clamp01(1-path.ConsumerTrustScore)*0.02 +
			insuranceDeficit*0.04 +
			settlementDeficit*0.04 +
			clamp01(path.RecoveryLatencyHours/120.0)*0.06,
	)
}

func dominantOperationalReasons(constraint OperationalLeg) []string {
	out := make([]string, 0, 4)
	if constraint.BlockageScore >= 0.45 {
		out = append(out, "chokepoint_blockage")
	}
	if constraint.TransitDelayHours >= 24 {
		out = append(out, "transit_delay")
	}
	if constraint.ScreeningIntensityScore >= 0.45 {
		out = append(out, "screening_burden")
	}
	if constraint.CustomsDelayHours >= 24 {
		out = append(out, "customs_delay")
	}
	if constraint.RerouteCostMultiplier >= 1.35 {
		out = append(out, "reroute_cost")
	}
	if constraint.ProductSafetyEnforcementScore >= 0.45 {
		out = append(out, "product_safety_enforcement")
	}
	if constraint.CounterfeitSeizurePressureScore >= 0.45 {
		out = append(out, "counterfeit_seizure_pressure")
	}
	if constraint.RecallExposureScore >= 0.35 {
		out = append(out, "recall_exposure")
	}
	if constraint.InformalChannelLeakageScore >= 0.45 {
		out = append(out, "informal_channel_leakage")
	}
	if constraint.ConsumerTrustScore > 0 && constraint.ConsumerTrustScore <= 0.55 {
		out = append(out, "consumer_trust_erosion")
	}
	if constraint.InsuranceAvailability > 0 && constraint.InsuranceAvailability <= 0.55 {
		out = append(out, "insurance_friction")
	}
	if constraint.SettlementReliabilityScore > 0 && constraint.SettlementReliabilityScore <= 0.55 {
		out = append(out, "settlement_friction")
	}
	return compactStrings(out)
}

type recognitionTargetCandidate struct {
	Path  string
	Kind  string
	Score float64
}

func recognitionObjectCandidates(recognition map[string]any) []recognitionTargetCandidate {
	out := make([]recognitionTargetCandidate, 0, 12)
	for _, raw := range mapSlice(recognition["objectCandidates"]) {
		path := strings.TrimSpace(stringValue(raw, "path"))
		if path == "" {
			continue
		}
		out = append(out, recognitionTargetCandidate{
			Path:  path,
			Kind:  strings.TrimSpace(stringValue(raw, "kind")),
			Score: floatValue(raw, "score", 0),
		})
	}
	return out
}

func recognitionKindPriority(kind string) float64 {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "gov":
		return 2.4
	case "fac":
		return 2.2
	case "market":
		return 2.2
	case "domain":
		return 2.1
	case "product":
		return 1.8
	case "loc", "geo":
		return 1.6
	case "org":
		return 1.2
	default:
		return 1.0
	}
}

func targetClassPriority(class string) float64 {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "chokepoint":
		return 3.2
	case "compliance_gate":
		return 3.0
	case "product_safety":
		return 3.0
	case "market_surveillance":
		return 2.8
	case "logistics_hub":
		return 2.6
	case "finance_gate":
		return 2.4
	default:
		return 1.0
	}
}

func mergeWhyInScope(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "":
		return right
	case right == "":
		return left
	case strings.Contains(left, right):
		return left
	default:
		return left + "; " + right
	}
}

func targetIsDegraded(target OperationalTarget) bool {
	metrics := target.Metrics
	if len(metrics) == 0 {
		return false
	}
	switch {
	case metricValue(metrics, operationalMetricDefinitions["chokepointBlockageScore"].Path) >= 0.45:
		return true
	case metricValue(metrics, operationalMetricDefinitions["transitDelayHours"].Path) >= 24:
		return true
	case metricValue(metrics, operationalMetricDefinitions["screeningIntensityScore"].Path) >= 0.45:
		return true
	case metricValue(metrics, operationalMetricDefinitions["customsDelayHours"].Path) >= 24:
		return true
	case metricValue(metrics, operationalMetricDefinitions["rerouteCostMultiplier"].Path) >= 1.25:
		return true
	case metricValue(metrics, operationalMetricDefinitions["productSafetyEnforcementScore"].Path) >= 0.45:
		return true
	case metricValue(metrics, operationalMetricDefinitions["counterfeitSeizurePressureScore"].Path) >= 0.45:
		return true
	case metricValue(metrics, operationalMetricDefinitions["recallExposureScore"].Path) >= 0.35:
		return true
	case metricValue(metrics, operationalMetricDefinitions["informalChannelLeakageScore"].Path) >= 0.45:
		return true
	}
	if v := metricValue(metrics, operationalMetricDefinitions["insuranceCoverAvailability"].Path); v > 0 && v <= 0.55 {
		return true
	}
	if v := metricValue(metrics, operationalMetricDefinitions["settlementReliabilityScore"].Path); v > 0 && v <= 0.55 {
		return true
	}
	if v := metricValue(metrics, operationalMetricDefinitions["consumerTrustScore"].Path); v > 0 && v <= 0.55 {
		return true
	}
	return false
}

func parseObservedAt(raw string, fallback time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, false
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts.UTC(), true
	}
	if ts, err := time.Parse("2006-01-02", raw); err == nil {
		if fallback.IsZero() {
			return ts.UTC(), true
		}
		ft := fallback.UTC()
		return time.Date(ts.Year(), ts.Month(), ts.Day(), ft.Hour(), ft.Minute(), ft.Second(), ft.Nanosecond(), time.UTC), true
	}
	return fallback, false
}

func parseGroundedHydrationEnvelope(raw string) (groundedHydrationEnvelope, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return groundedHydrationEnvelope{}, fmt.Errorf("empty response")
	}
	raw = trimJSONFence(raw)
	var generic any
	if err := json.Unmarshal([]byte(raw), &generic); err == nil {
		if results := extractGroundedHydrationResults(generic); len(results) > 0 {
			return groundedHydrationEnvelope{Results: results}, nil
		}
	}
	var envelope groundedHydrationEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err == nil {
		return envelope, nil
	}
	var results []groundedHydrationResult
	if err := json.Unmarshal([]byte(raw), &results); err == nil {
		return groundedHydrationEnvelope{Results: results}, nil
	}
	var wrapped struct {
		Items []groundedHydrationResult `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil && len(wrapped.Items) > 0 {
		return groundedHydrationEnvelope{Results: wrapped.Items}, nil
	}
	return groundedHydrationEnvelope{}, fmt.Errorf("unsupported response shape")
}

func extractGroundedHydrationResults(value any) []groundedHydrationResult {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"results", "items", "targets", "data", "updates", "response"} {
			if nested, ok := typed[key]; ok {
				if results := extractGroundedHydrationResults(nested); len(results) > 0 {
					return results
				}
			}
		}
		for _, nested := range typed {
			if results := extractGroundedHydrationResults(nested); len(results) > 0 {
				return results
			}
		}
	case []any:
		if len(typed) == 0 {
			return nil
		}
		raw, err := json.Marshal(typed)
		if err == nil {
			var direct []groundedHydrationResult
			if err := json.Unmarshal(raw, &direct); err == nil && len(direct) > 0 {
				return direct
			}
		}
		for _, nested := range typed {
			if results := extractGroundedHydrationResults(nested); len(results) > 0 {
				return results
			}
		}
	}
	return nil
}

func trimJSONFence(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "```") {
		return raw
	}
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```JSON")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "```")
	return strings.TrimSpace(raw)
}

func truncateForLog(raw string, max int) string {
	raw = strings.TrimSpace(raw)
	if max <= 0 || len(raw) <= max {
		return raw
	}
	return raw[:max]
}

func optionalString(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func compactStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	return out
}

func mapSlice(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if obj, ok := item.(map[string]any); ok {
				out = append(out, obj)
			}
		}
		return out
	default:
		return nil
	}
}

func stringValue(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}

func floatValue(input map[string]any, key string, fallback float64) float64 {
	if input == nil {
		return fallback
	}
	switch value := input[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case json.Number:
		if v, err := value.Float64(); err == nil {
			return v
		}
	}
	return fallback
}

func maxFloat(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
