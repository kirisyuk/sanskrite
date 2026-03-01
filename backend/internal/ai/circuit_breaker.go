package ai

import (
	"strings"
	"sync"
	"time"
)

const (
	circuitBreakerConsecutiveThreshold = 5
	circuitBreakerOpenDuration         = 30 * time.Second
)

// CircuitBreakerState tracks per-provider circuit state.
type CircuitBreakerState struct {
	ConsecutiveErrors int
	OpenedAt          time.Time
}

// CircuitBreaker opens after N consecutive errors per provider.
type CircuitBreaker struct {
	mu sync.Mutex

	threshold   int
	openDur     time.Duration
	state       map[AIProvider]CircuitBreakerState
}

// NewCircuitBreaker creates a circuit breaker with default thresholds.
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		threshold: circuitBreakerConsecutiveThreshold,
		openDur:   circuitBreakerOpenDuration,
		state:     make(map[AIProvider]CircuitBreakerState),
	}
}

// RecordFailure records a provider failure. ResourceExhausted opens immediately.
func (c *CircuitBreaker) RecordFailure(provider AIProvider, err error) {
	if c == nil || err == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == nil {
		c.state = make(map[AIProvider]CircuitBreakerState)
	}
	st := c.state[provider]
	st.ConsecutiveErrors++
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "resource exhausted") || strings.Contains(msg, "quota exceeded") {
		st.OpenedAt = time.Now().UTC()
	} else if st.ConsecutiveErrors >= c.threshold {
		st.OpenedAt = time.Now().UTC()
	}
	c.state[provider] = st
}

// RecordSuccess resets the circuit for the provider.
func (c *CircuitBreaker) RecordSuccess(provider AIProvider) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == nil {
		return
	}
	st := c.state[provider]
	st.ConsecutiveErrors = 0
	st.OpenedAt = time.Time{}
	c.state[provider] = st
}

// IsOpen returns true if the circuit is open for the provider.
func (c *CircuitBreaker) IsOpen(provider AIProvider) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.state[provider]
	if st.OpenedAt.IsZero() {
		return false
	}
	if time.Since(st.OpenedAt) >= c.openDur {
		st.OpenedAt = time.Time{}
		st.ConsecutiveErrors = st.ConsecutiveErrors / 2
		c.state[provider] = st
		return false
	}
	return true
}

// OpenProviders returns the set of providers with open circuits.
func (c *CircuitBreaker) OpenProviders() map[AIProvider]struct{} {
	out := map[AIProvider]struct{}{}
	if c == nil {
		return out
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC()
	for p, st := range c.state {
		if st.OpenedAt.IsZero() {
			continue
		}
		if st.OpenedAt.Add(c.openDur).After(now) {
			out[p] = struct{}{}
		} else {
			st.OpenedAt = time.Time{}
			st.ConsecutiveErrors = st.ConsecutiveErrors / 2
			c.state[p] = st
		}
	}
	return out
}
