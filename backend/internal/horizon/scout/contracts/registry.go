package contracts

import (
	"strings"
	"sync"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
)

type Registry struct {
	handlers map[string]pipeline.ContractHandler
}

func NewRegistry() *Registry {
	return &Registry{handlers: map[string]pipeline.ContractHandler{}}
}

func (r *Registry) Register(h pipeline.ContractHandler) {
	if r == nil || h == nil {
		return
	}
	ct := strings.TrimSpace(h.ContractType())
	if ct == "" {
		return
	}
	r.handlers[strings.ToLower(ct)] = h
}

func (r *Registry) Get(contractType string) pipeline.ContractHandler {
	if r == nil {
		return nil
	}
	ct := strings.TrimSpace(strings.ToLower(contractType))
	if ct == "" {
		return nil
	}
	return r.handlers[ct]
}

var (
	defaultRegistryOnce sync.Once
	defaultRegistry     *Registry
)

func DefaultRegistry() *Registry {
	defaultRegistryOnce.Do(func() {
		defaultRegistry = NewRegistry()
		registerBuiltinHandlers(defaultRegistry)
	})
	return defaultRegistry
}
