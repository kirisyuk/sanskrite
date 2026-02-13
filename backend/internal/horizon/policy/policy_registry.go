package policy

import "sync"

// PolicyRegistry stores named policy instances to keep runtime wiring explicit.
type PolicyRegistry struct {
	mu       sync.RWMutex
	policies map[string]Policy
}

func NewPolicyRegistry(policies ...Policy) *PolicyRegistry {
	r := &PolicyRegistry{
		policies: make(map[string]Policy, len(policies)),
	}
	for _, p := range policies {
		r.Register(p)
	}
	return r
}

func (r *PolicyRegistry) Register(policy Policy) {
	if r == nil || policy == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policies[policy.Name()] = policy
}

func (r *PolicyRegistry) Get(name string) (Policy, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.policies[name]
	return p, ok
}

func (r *PolicyRegistry) List() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.policies))
	for name := range r.policies {
		out = append(out, name)
	}
	return out
}
