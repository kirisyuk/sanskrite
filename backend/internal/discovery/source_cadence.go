package discovery

import (
	"math"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

type resolvedSourceCadencePolicy struct {
	baseInterval         time.Duration
	minInterval          time.Duration
	maxInterval          time.Duration
	emptyBackoff         float64
	errorBackoff         float64
	freshnessBoostWindow time.Duration
	leaseDuration        time.Duration
}

type sourceRunOutcome struct {
	foundNew           bool
	hadError           bool
	documentsProcessed int
	documentsCreated   int
}

func configuredPollFloor(ds *models.DataSource) time.Duration {
	if ds != nil && ds.Details.PollIntervalMinutes != nil && *ds.Details.PollIntervalMinutes > 0 {
		return time.Duration(*ds.Details.PollIntervalMinutes) * time.Minute
	}
	return 0
}

func legacyAdaptivePollCadence(ds *models.DataSource, foundNew bool) time.Duration {
	if ds != nil && models.IsHorizonMonitorFeedKind(ds.Kind) {
		if foundNew {
			return 30 * time.Minute
		}
		if ds.LastFoundAt != nil {
			age := time.Since(*ds.LastFoundAt)
			switch {
			case age < 2*time.Hour:
				return 30 * time.Minute
			case age < 24*time.Hour:
				return 2 * time.Hour
			default:
				return 6 * time.Hour
			}
		}
		return 30 * time.Minute
	}
	if foundNew {
		return 10 * time.Minute
	}
	if ds != nil && ds.LastFoundAt != nil {
		age := time.Since(*ds.LastFoundAt)
		switch {
		case age < 15*time.Minute:
			return 10 * time.Minute
		case age < 2*time.Hour:
			return 30 * time.Minute
		case age < 24*time.Hour:
			return 2 * time.Hour
		default:
			return 6 * time.Hour
		}
	}
	return 30 * time.Minute
}

func defaultPolicyBaseInterval(ds *models.DataSource) time.Duration {
	if floor := configuredPollFloor(ds); floor > 0 {
		return floor
	}
	if ds != nil && models.IsGlobalSystemSource(ds) && models.UsesHeuristicProcessing(ds) {
		return time.Hour
	}
	if ds != nil && models.IsHorizonMonitorFeedKind(ds.Kind) {
		return 30 * time.Minute
	}
	return 30 * time.Minute
}

func resolveSourceCadencePolicy(ds *models.DataSource) resolvedSourceCadencePolicy {
	base := defaultPolicyBaseInterval(ds)
	min := base
	max := 6 * time.Hour
	emptyBackoff := 2.0
	errorBackoff := 2.0
	freshnessBoost := 2 * time.Hour
	leaseDuration := 90 * time.Minute

	if ds != nil && ds.Details.CadencePolicy != nil {
		policy := ds.Details.CadencePolicy
		if policy.BaseIntervalMinutes != nil && *policy.BaseIntervalMinutes > 0 {
			base = time.Duration(*policy.BaseIntervalMinutes) * time.Minute
		}
		if policy.MinIntervalMinutes != nil && *policy.MinIntervalMinutes > 0 {
			min = time.Duration(*policy.MinIntervalMinutes) * time.Minute
		}
		if policy.MaxIntervalMinutes != nil && *policy.MaxIntervalMinutes > 0 {
			max = time.Duration(*policy.MaxIntervalMinutes) * time.Minute
		}
		if policy.EmptyBackoffMultiplier != nil && *policy.EmptyBackoffMultiplier >= 1 {
			emptyBackoff = *policy.EmptyBackoffMultiplier
		}
		if policy.ErrorBackoffMultiplier != nil && *policy.ErrorBackoffMultiplier >= 1 {
			errorBackoff = *policy.ErrorBackoffMultiplier
		}
		if policy.FreshnessBoostWindowMinutes != nil && *policy.FreshnessBoostWindowMinutes > 0 {
			freshnessBoost = time.Duration(*policy.FreshnessBoostWindowMinutes) * time.Minute
		}
		if policy.LeaseDurationMinutes != nil && *policy.LeaseDurationMinutes > 0 {
			leaseDuration = time.Duration(*policy.LeaseDurationMinutes) * time.Minute
		}
	}

	if floor := configuredPollFloor(ds); floor > 0 && min < floor {
		min = floor
	}
	if base < min {
		base = min
	}
	if max < base {
		max = base
	}

	return resolvedSourceCadencePolicy{
		baseInterval:         base,
		minInterval:          min,
		maxInterval:          max,
		emptyBackoff:         emptyBackoff,
		errorBackoff:         errorBackoff,
		freshnessBoostWindow: freshnessBoost,
		leaseDuration:        leaseDuration,
	}
}

func scaleDuration(base time.Duration, multiplier float64, steps int, max time.Duration) time.Duration {
	if steps <= 0 || multiplier <= 1 {
		if base > max {
			return max
		}
		return base
	}
	scaled := float64(base)
	for i := 0; i < steps; i++ {
		scaled *= multiplier
		if time.Duration(scaled) >= max {
			return max
		}
	}
	out := time.Duration(math.Round(scaled))
	if out > max {
		return max
	}
	return out
}

func effectivePollCadence(ds *models.DataSource, foundNew bool) time.Duration {
	policy := resolveSourceCadencePolicy(ds)
	interval := legacyAdaptivePollCadence(ds, foundNew)
	if interval < policy.minInterval {
		interval = policy.minInterval
	}
	if interval > policy.maxInterval {
		interval = policy.maxInterval
	}
	return interval
}

func computeNextInterval(ds *models.DataSource, state *models.SourceRuntimeState, outcome sourceRunOutcome) time.Duration {
	policy := resolveSourceCadencePolicy(ds)
	interval := policy.baseInterval
	now := time.Now().UTC()

	if outcome.hadError {
		errors := 1
		if state != nil && state.ConsecutiveErrors > 0 {
			errors = state.ConsecutiveErrors
		}
		interval = scaleDuration(policy.baseInterval, policy.errorBackoff, errors, policy.maxInterval)
		if interval < policy.minInterval {
			interval = policy.minInterval
		}
		return interval
	}

	emptyRuns := 0
	if state != nil {
		emptyRuns = state.ConsecutiveEmptyPolls
	}
	if outcome.documentsCreated == 0 && outcome.documentsProcessed == 0 && !outcome.foundNew {
		emptyRuns++
	} else if outcome.foundNew {
		emptyRuns = 0
	}
	if emptyRuns > 0 {
		interval = scaleDuration(policy.baseInterval, policy.emptyBackoff, emptyRuns, policy.maxInterval)
	}

	lastFoundAt := (*time.Time)(nil)
	if outcome.foundNew {
		lastFoundAt = &now
	} else if state != nil {
		lastFoundAt = state.LastFoundAt
	} else if ds != nil {
		lastFoundAt = ds.LastFoundAt
	}
	if lastFoundAt != nil && now.Sub(*lastFoundAt) <= policy.freshnessBoostWindow {
		if interval < policy.minInterval {
			interval = policy.minInterval
		}
		if interval > policy.baseInterval {
			interval = policy.baseInterval
		}
	}

	if interval < policy.minInterval {
		interval = policy.minInterval
	}
	if interval > policy.maxInterval {
		interval = policy.maxInterval
	}
	return interval
}
