package regulatorytransform

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// Pair describes an authoritative decision→mechanism or mechanism→condition
// edge from the regulatory transform dictionary.
//
// Under the AgentHorde taxonomy:
//   - "uses_mechanism" maps to ah.RelationNameWields (decision → mechanism).
//   - "imposes_condition" maps to ah.RelationNameImposes (mechanism → target).
//
// CanonicalType, when set, overrides the inferred relation. Empty CanonicalType
// triggers RelationKind-based inference.
type Pair struct {
	FromPath      string
	ToPath        string
	RelationKind  string
	CanonicalType ah.RelationName
}

// Registry is an in-memory transform pair source (Firestore wiring can populate
// this at runtime).
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

// RegisterPair registers a transform pair. Duplicate from+to keys overwrite the
// previous entry (last write wins).
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

// NormalizeRegulatoryEdgeType adjusts the edge relation when both endpoints
// match a registered pair.
func (r *Registry) NormalizeRegulatoryEdgeType(sourcePath, targetPath string, edgeType ah.RelationName) (ah.RelationName, bool) {
	if r == nil || len(r.byKey) == 0 {
		return edgeType, false
	}
	sp := strings.TrimSpace(sourcePath)
	tp := strings.TrimSpace(targetPath)
	p, ok := r.byKey[pairKey(sp, tp)]
	if !ok {
		return edgeType, false
	}
	if p.CanonicalType != "" {
		return p.CanonicalType, true
	}
	switch strings.ToLower(strings.TrimSpace(p.RelationKind)) {
	case "uses_mechanism", "uses-mechanism", "wields":
		return ah.RelationNameWields, true
	case "imposes_condition", "imposes-condition", "imposes":
		return ah.RelationNameImposes, true
	}
	return edgeType, false
}
