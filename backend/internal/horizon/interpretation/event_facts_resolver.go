package interpretation

import (
	"context"
	"strings"

	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

// EventFactsResolver resolves OntologyFact IDs to human-readable summaries for interpretation.
// Used when Contribution.SourceEventID references an event fact; we fetch Title/Summary for the LLM prompt.
type EventFactsResolver interface {
	ResolveEventSummary(ctx context.Context, factID string) (string, error)
}

// OntologyEventFactsResolver implements EventFactsResolver using OntologyFactsRepository.
func NewOntologyEventFactsResolver(factsRepo *corerepo.OntologyFactsRepository) EventFactsResolver {
	return &ontologyEventFactsResolver{factsRepo: factsRepo}
}

type ontologyEventFactsResolver struct {
	factsRepo *corerepo.OntologyFactsRepository
}

func (r *ontologyEventFactsResolver) ResolveEventSummary(ctx context.Context, factID string) (string, error) {
	if r == nil || r.factsRepo == nil {
		return "", nil
	}
	factID = strings.TrimSpace(factID)
	if factID == "" {
		return "", nil
	}
	fact, err := r.factsRepo.Get(ctx, factID)
	if err != nil || fact == nil {
		return "", err
	}
	if s := strings.TrimSpace(fact.Summary); s != "" {
		return s, nil
	}
	if s := strings.TrimSpace(fact.Title); s != "" {
		return s, nil
	}
	return "", nil
}
