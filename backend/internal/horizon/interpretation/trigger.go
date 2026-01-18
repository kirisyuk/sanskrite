package interpretation

import (
	"context"
	"log"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	signalsys "github.com/igorkirisyuk/sanskrite/internal/horizon/signal"
)

// TriggerPriority represents the priority of an interpretation trigger.
type TriggerPriority string

const (
	TriggerPriorityHigh   TriggerPriority = "HIGH"
	TriggerPriorityMedium TriggerPriority = "MEDIUM"
	TriggerPriorityLow    TriggerPriority = "LOW"
	TriggerPriorityAlways TriggerPriority = "ALWAYS"
)

// TriggerType represents the type of trigger.
type TriggerType string

const (
	TriggerTypeScoreThreshold TriggerType = "score_threshold"
	TriggerTypeRapidChange    TriggerType = "rapid_change"
	TriggerTypeVIP            TriggerType = "vip"
	TriggerTypeUserQuery      TriggerType = "user_query"
)

// Trigger represents a condition that causes a node to be queued for interpretation.
type Trigger struct {
	FragmentID  string         `json:"fragmentId"`
	Priority    TriggerPriority `json:"priority"`
	Type        TriggerType    `json:"type"`
	Value       float64        `json:"value"`        // Score or delta value
	Polarity    string         `json:"polarity"`    // "risk" | "opportunity"
	TriggeredAt time.Time      `json:"triggeredAt"`
}

// TriggerEvaluator evaluates nodes for interpretation triggers.
type TriggerEvaluator struct {
	scoreComputer *ScoreComputer
	vipNodes      map[string]bool // Set of VIP node IDs
	fragmentsRepo *repo.HorizonFragmentsRepository
	versionsRepo  *repo.HorizonFragmentVersionsRepository
}

// TriggerConfig holds configuration for trigger evaluation.
type TriggerConfig struct {
	ScoreThreshold    float64       // Default: 0.7
	RapidChangeDelta  float64       // Default: 0.3
	RapidChangeWindow time.Duration // Default: 24 hours
}

// DefaultTriggerConfig returns default trigger configuration.
func DefaultTriggerConfig() TriggerConfig {
	return TriggerConfig{
		ScoreThreshold:    0.7,
		RapidChangeDelta:  0.3,
		RapidChangeWindow: 24 * time.Hour,
	}
}

// NewTriggerEvaluator creates a new TriggerEvaluator.
func NewTriggerEvaluator(
	scoreComputer *ScoreComputer,
	fragmentsRepo *repo.HorizonFragmentsRepository,
	versionsRepo *repo.HorizonFragmentVersionsRepository,
) *TriggerEvaluator {
	return &TriggerEvaluator{
		scoreComputer: scoreComputer,
		vipNodes:      make(map[string]bool),
		fragmentsRepo: fragmentsRepo,
		versionsRepo:  versionsRepo,
	}
}

// SetVIPNodes sets the list of VIP node IDs.
func (t *TriggerEvaluator) SetVIPNodes(nodeIDs []string) {
	t.vipNodes = make(map[string]bool)
	for _, id := range nodeIDs {
		t.vipNodes[id] = true
	}
}

// Evaluate evaluates a fragment for interpretation triggers.
// Returns list of triggers that fired.
func (t *TriggerEvaluator) Evaluate(ctx context.Context, fragment *horizonmodels.HorizonFragment, config TriggerConfig) ([]Trigger, error) {
	if fragment == nil {
		return nil, nil
	}

	var triggers []Trigger

	// Check VIP node trigger
	if t.isVIPNode(fragment.ID) {
		if t.hasSignificantMetric(fragment) {
			triggers = append(triggers, Trigger{
				FragmentID:  fragment.ID,
				Priority:    TriggerPriorityAlways,
				Type:        TriggerTypeVIP,
				Value:       0.0,
				Polarity:    "risk", // Check both, but start with risk
				TriggeredAt: time.Now(),
			})
		}
	}

	// Check score threshold for risk
	riskScore := t.scoreComputer.ComputeScore(fragment, "risk")
	if riskScore > config.ScoreThreshold {
		triggers = append(triggers, Trigger{
			FragmentID:  fragment.ID,
			Priority:    TriggerPriorityHigh,
			Type:        TriggerTypeScoreThreshold,
			Value:       riskScore,
			Polarity:    "risk",
			TriggeredAt: time.Now(),
		})
	}

	// Check score threshold for opportunity
	opportunityScore := t.scoreComputer.ComputeScore(fragment, "opportunity")
	if opportunityScore > config.ScoreThreshold {
		triggers = append(triggers, Trigger{
			FragmentID:  fragment.ID,
			Priority:    TriggerPriorityHigh,
			Type:        TriggerTypeScoreThreshold,
			Value:       opportunityScore,
			Polarity:    "opportunity",
			TriggeredAt: time.Now(),
		})
	}

	// Check rapid change
	if t.versionsRepo != nil {
		rapidChangeTriggers := t.checkRapidChange(ctx, fragment, config)
		triggers = append(triggers, rapidChangeTriggers...)
	}

	if len(triggers) > 0 {
		log.Printf("%s fired fragmentId=%s risk=%.3f opp=%.3f triggers=%d",
			signalsys.LogTrigger,
			strings.TrimSpace(fragment.ID),
			riskScore,
			opportunityScore,
			len(triggers),
		)
	}

	return triggers, nil
}

// isVIPNode checks if a node is in the VIP list.
func (t *TriggerEvaluator) isVIPNode(fragmentID string) bool {
	return t.vipNodes[fragmentID]
}

// hasSignificantMetric checks if fragment has any metric > 0.1.
func (t *TriggerEvaluator) hasSignificantMetric(fragment *horizonmodels.HorizonFragment) bool {
	if fragment.State == nil {
		return false
	}
	for _, value := range fragment.State {
		if value.Value > 0.1 {
			return true
		}
	}
	return false
}

// checkRapidChange checks for rapid score changes in the last 24 hours.
func (t *TriggerEvaluator) checkRapidChange(ctx context.Context, fragment *horizonmodels.HorizonFragment, config TriggerConfig) []Trigger {
	var triggers []Trigger

	// Get previous version (24h ago)
	cutoffTime := time.Now().Add(-config.RapidChangeWindow)
	versions, err := t.versionsRepo.ListByFragment(ctx, fragment.TenantID, fragment.ID, 10, 0)
	if err != nil || len(versions) == 0 {
		return triggers
	}

	// Find version closest to cutoff time
	var prevVersion *repo.HorizonFragmentVersion
	for _, v := range versions {
		ts := v.Timestamp
		if ts.Before(cutoffTime) || ts.Equal(cutoffTime) {
			if prevVersion == nil || ts.After(prevVersion.Timestamp) {
				prevVersion = v
			}
		}
	}

	if prevVersion == nil {
		return triggers
	}

	// Compute scores for current and previous state
	currentRiskScore := t.scoreComputer.ComputeScore(fragment, "risk")
	prevRiskScore := t.scoreComputer.ComputeScoreFromState(prevVersion.State, "risk")
	riskDelta := currentRiskScore - prevRiskScore

	if riskDelta > config.RapidChangeDelta {
		triggers = append(triggers, Trigger{
			FragmentID:  fragment.ID,
			Priority:    TriggerPriorityMedium,
			Type:        TriggerTypeRapidChange,
			Value:       riskDelta,
			Polarity:    "risk",
			TriggeredAt: time.Now(),
		})
	}

	currentOppScore := t.scoreComputer.ComputeScore(fragment, "opportunity")
	prevOppScore := t.scoreComputer.ComputeScoreFromState(prevVersion.State, "opportunity")
	oppDelta := currentOppScore - prevOppScore

	if oppDelta > config.RapidChangeDelta {
		triggers = append(triggers, Trigger{
			FragmentID:  fragment.ID,
			Priority:    TriggerPriorityMedium,
			Type:        TriggerTypeRapidChange,
			Value:       oppDelta,
			Polarity:    "opportunity",
			TriggeredAt: time.Now(),
		})
	}

	return triggers
}

// ComputeScoreFromState computes score from a state map (for version comparison).
func (s *ScoreComputer) ComputeScoreFromState(state map[string]horizonmodels.MetricValue, polarity string) float64 {
	if state == nil {
		return 0.0
	}

	if polarity != "risk" && polarity != "opportunity" {
		return 0.0
	}

	score := 0.0
	for metricPath, value := range state {
		if !s.hasPolarity(metricPath, polarity) {
			continue
		}

		axis := s.extractAxis(metricPath)
		if axis == "" {
			continue
		}

		weight := s.getAxisWeight(axis)
		score += value.Value * weight
	}

	if score > 1.0 {
		score = 1.0
	}

	return score
}
