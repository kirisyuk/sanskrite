package regulatorytransform

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// Pair describes an authoritative decision→mechanism or mechanism→condition edge from the regulatory transform dictionary.
type Pair struct {
	FromPath      string
	ToPath        string
	RelationKind  string
	CanonicalType models.DependencyType
}

// Registry is an in-memory transform pair source (Firestore wiring can populate this at runtime).
type Registry struct {
	byKey map[string]Pair
}

func pairKey(fromPath, toPath string) string {
	return strings.ToLower(strings.TrimSpace(fromPath)) + "\x00" + strings.ToLower(strings.TrimSpace(toPath))
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byKey: make(map[string]Pair)}
}

// RegisterPair registers a transform pair. Duplicate from+to keys overwrite the previous entry (last write wins).
func (r *Registry) RegisterPair(p Pair) {
	if r == nil {
		return
	}
	p.FromPath = strings.TrimSpace(p.FromPath)
	p.ToPath = strings.TrimSpace(p.ToPath)
	if p.FromPath == "" || p.ToPath == "" {
		return
	}
	if r.byKey == nil {
		r.byKey = make(map[string]Pair)
	}
	r.byKey[pairKey(p.FromPath, p.ToPath)] = p
}

// PairCount returns how many distinct from+to pairs are registered.
func (r *Registry) PairCount() int {
	if r == nil {
		return 0
	}
	return len(r.byKey)
}

// NormalizeRegulatoryEdgeType adjusts edge type when both endpoints match a registered pair.
func (r *Registry) NormalizeRegulatoryEdgeType(sourcePath, targetPath string, edgeType models.DependencyType) (models.DependencyType, bool) {
	if r == nil || len(r.byKey) == 0 {
		return edgeType, false
	}
	sp := strings.TrimSpace(sourcePath)
	tp := strings.TrimSpace(targetPath)
	p, ok := r.byKey[pairKey(sp, tp)]
	if !ok {
		_ = taxonomy.NormalizeRelationType(string(edgeType))
		return edgeType, false
	}
	if p.CanonicalType != "" {
		return p.CanonicalType, true
	}
	switch strings.ToLower(strings.TrimSpace(p.RelationKind)) {
	case "uses_mechanism", "uses-mechanism":
		return models.DependencyTypeUsesMechanism, true
	case "imposes_condition", "imposes-condition":
		return models.DependencyTypeImposesCondition, true
	}
	_ = taxonomy.NormalizeRelationType(string(edgeType))
	return edgeType, false
}
