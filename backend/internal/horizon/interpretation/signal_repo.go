package interpretation

import (
	"context"
	"strconv"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// OntologySignalFactsRepo adapts ontology_facts storage to Interpreter SignalsRepo.
type OntologySignalFactsRepo struct {
	factsRepo *corerepo.OntologyFactsRepository
}

func NewOntologySignalFactsRepo(factsRepo *corerepo.OntologyFactsRepository) *OntologySignalFactsRepo {
	return &OntologySignalFactsRepo{factsRepo: factsRepo}
}

func (r *OntologySignalFactsRepo) GetSignal(ctx context.Context, signalID string) (*horizonmodels.SignalDescriptor, error) {
	if r == nil || r.factsRepo == nil {
		return nil, nil
	}
	fact, err := r.factsRepo.Get(ctx, strings.TrimSpace(signalID))
	if err != nil || fact == nil {
		return nil, err
	}
	if strings.TrimSpace(fact.Kind) != "signal" {
		return nil, nil
	}
	return descriptorFromSignalFact(fact), nil
}

func descriptorFromSignalFact(f *coremodels.OntologyFact) *horizonmodels.SignalDescriptor {
	s := &horizonmodels.SignalDescriptor{
		ID:          strings.TrimSpace(f.ID),
		Amplitude:   clamp01(parseSignalTagFloat(f.Tags, "signal_amplitude:", f.Confidence)),
		Confidence:  clamp01(f.Confidence),
		CreatedAt:   f.CreatedAt,
		ObservedAt:  f.UpdatedAt,
		SeedPaths:   append([]string{}, f.CanonicalPaths...),
		ActorPaths:  append([]string{}, f.ActorPaths...),
		SpherePaths: append([]string{}, f.SpherePaths...),
		Constraints: horizonmodels.SignalConstraints{
			Impact:               clamp01(parseSignalTagFloat(f.Tags, "signal_impact:", -1)),
			Likelihood:           clamp01(parseSignalTagFloat(f.Tags, "signal_likelihood:", -1)),
			Modality:             parseSignalTagString(f.Tags, "signal_modality:"),
			TimeHorizon:          parseSignalTagString(f.Tags, "signal_horizon:"),
			Timeframe:            parseSignalTagString(f.Tags, "signal_horizon:"),
			EscalationLevel:      parseSignalTagString(f.Tags, "signal_escalation:"),
			EscalationConfidence: clamp01(parseSignalTagFloat(f.Tags, "signal_confidence:", f.Confidence)),
		},
	}
	if f.LocationPath != nil {
		s.LocationPath = strings.TrimSpace(*f.LocationPath)
	}
	if f.ObservedAt != nil {
		s.ObservedAt = *f.ObservedAt
	}
	parts := strings.Split(strings.TrimSpace(f.Type), ":")
	if len(parts) > 0 {
		s.Polarity = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		s.Axis = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		s.Mechanism = strings.TrimSpace(parts[2])
	}
	s.Axis, s.Mechanism = normalizeSignalAxisAlias(s.Axis, s.Mechanism)
	if s.Polarity == "" {
		s.Polarity = "risk"
	}
	if s.Axis == "" {
		s.Axis = horizonmodels.AxisInfoUncertainty
	}
	if s.Constraints.Impact < 0 {
		s.Constraints.Impact = s.Amplitude
	}
	if s.Constraints.Likelihood < 0 {
		s.Constraints.Likelihood = s.Confidence
	}
	if strings.TrimSpace(s.Constraints.Modality) == "" {
		s.Constraints.Modality = horizonmodels.SignalModalityPotential
	}
	if strings.TrimSpace(s.Constraints.TimeHorizon) == "" {
		s.Constraints.TimeHorizon = horizonmodels.SignalTimeHorizonShortTerm
		s.Constraints.Timeframe = horizonmodels.SignalTimeHorizonShortTerm
	}
	return s
}

func DescriptorFromSignalFact(f *coremodels.OntologyFact) *horizonmodels.SignalDescriptor {
	return descriptorFromSignalFact(f)
}

func normalizeSignalAxisAlias(axis, mechanism string) (string, string) {
	axis = strings.TrimSpace(axis)
	mechanism = strings.TrimSpace(mechanism)
	if axis == "" {
		return horizonmodels.AxisInfoUncertainty, firstNonEmpty(mechanism, "watch_pressure")
	}
	if strings.Contains(axis, "/") {
		return axis, mechanism
	}
	switch axis {
	case "supply_chain_disruption", "supply_shortage", "input_shortage", "production_shortfall":
		return horizonmodels.AxisPhysicalFlowSupply, firstNonEmpty(mechanism, axis)
	case "demand_shock", "demand_drop", "demand_surge":
		return horizonmodels.AxisPhysicalFlowDemand, firstNonEmpty(mechanism, axis)
	case "logistics_disruption", "route_disruption", "shipping_constraint", "capacity_constraint", "port_disruption":
		return horizonmodels.AxisLogisticsCapacity, firstNonEmpty(mechanism, axis)
	case "logistics_delay", "shipping_delay", "delivery_delay", "lead_time_increase":
		return horizonmodels.AxisLogisticsLatency, firstNonEmpty(mechanism, axis)
	case "regulatory_pressure", "compliance_risk", "sanction_exposure", "policy_restriction":
		return horizonmodels.AxisLegalCompliance, firstNonEmpty(mechanism, axis)
	case "regulatory_uncertainty", "policy_uncertainty", "uncertainty", "watch_pressure":
		return horizonmodels.AxisInfoUncertainty, firstNonEmpty(mechanism, axis)
	case "geopolitical_risk", "conflict_risk", "conflict_escalation":
		return horizonmodels.AxisGeoConflict, firstNonEmpty(mechanism, axis)
	case "operational_disruption", "operational_outage", "continuity_risk":
		return horizonmodels.AxisOperationalContinuity, firstNonEmpty(mechanism, axis)
	case "settlement_risk", "payment_disruption", "payment_delay":
		return horizonmodels.AxisFinancialSettlement, firstNonEmpty(mechanism, axis)
	case "credit_risk", "counterparty_risk":
		return horizonmodels.AxisFinancialCredit, firstNonEmpty(mechanism, axis)
	case "reputational_pressure", "brand_risk":
		return horizonmodels.AxisInfoReputation, firstNonEmpty(mechanism, axis)
	case "market_access_restriction", "access_restriction":
		return horizonmodels.AxisLegalAccess, firstNonEmpty(mechanism, axis)
	default:
		return horizonmodels.AxisInfoUncertainty, firstNonEmpty(mechanism, axis)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseSignalTagFloat(tags []string, prefix string, fallback float64) float64 {
	prefix = strings.TrimSpace(prefix)
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, prefix) {
			v := strings.TrimSpace(strings.TrimPrefix(t, prefix))
			f, err := strconv.ParseFloat(v, 64)
			if err == nil {
				return f
			}
		}
	}
	return fallback
}

func parseSignalTagString(tags []string, prefix string) string {
	prefix = strings.TrimSpace(prefix)
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(t, prefix))
		}
	}
	return ""
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
