package telemetry

import (
	"context"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

// ExternalServiceOutcome is the result of an external service call.
const (
	OutcomeHit   = "hit"   // Got useful data
	OutcomeMiss  = "miss"  // Called successfully but no match
	OutcomeError = "error" // Service unavailable, timeout, HTTP error
)

// RecordExternalServiceCall records a single call to an external service.
// Use component="external_services", metric=serviceName (geonames, wikidata, opencorporates).
func RecordExternalServiceCall(
	ctx context.Context,
	telemetryRepo *repo.HorizonTelemetryRepository,
	service string,
	outcome string,
) {
	if telemetryRepo == nil {
		return
	}
	service = strings.TrimSpace(service)
	outcome = strings.TrimSpace(strings.ToLower(outcome))
	if service == "" {
		return
	}
	switch outcome {
	case OutcomeHit, OutcomeMiss, OutcomeError:
	default:
		outcome = OutcomeError
	}
	counters := map[string]int64{"calls": 1, outcome: 1}
	_ = telemetryRepo.Increment(ctx, "global", "external_services", service, time.Hour, nil, counters)
}
