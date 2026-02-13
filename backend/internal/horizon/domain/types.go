package domain

import (
	"context"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type EventPublisher interface {
	Publish(ctx context.Context, eventType string, data map[string]interface{})
}

type Contract struct {
	CanonicalPath      string
	ContractType       string
	Title              string
	Description        string
	Facets             map[string][]string
	FirstOrderEntities []string
	RelatedDomains     []string
	AffectedDomains    []string
	TemporalStart      *time.Time
	TemporalEnd        *time.Time
	Polarity           []string
}

type CrossFacetExpectation struct {
	FacetA              string
	FacetB              string
	ExpectedRelationSet []string
}

type EnrichmentTask struct {
	Priority    string
	GapType     string
	FacetKey    string
	Hint        string
	TargetPath  string
	MissingInfo []string
}

type RunInput struct {
	Job                     *models.ScoutingJob
	Contract                Contract
	GroundingEvidence       *ai.GroundingEvidence
	ExecuteBaseCollect      func(ctx context.Context, groundingEvidence *ai.GroundingEvidence) error
	ExecuteStrategicCollect func(ctx context.Context, groundingEvidence *ai.GroundingEvidence) error
}

type RunResult struct {
	Snapshot      *models.DomainAnswerSnapshot
	ViewDirective *models.ViewDirective
}
