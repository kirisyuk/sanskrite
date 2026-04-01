package lensresearch

import (
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	SourceKindLensArtifactFragment = "lens_artifact_fragment"
	SourceKindMaterializedNodeRef  = "materialized_node_ref"
	SourceKindScopeArtifactNodeRef = "scope_artifact_node_ref"
)

// NormalizeObjectRef keeps only fragment-like canonical refs and excludes query/domain/artifact meta refs.
func NormalizeObjectRef(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || !strings.HasPrefix(value, "/") {
		return ""
	}
	switch {
	case strings.HasPrefix(value, "/query/"),
		strings.HasPrefix(value, "/domain/"),
		strings.HasPrefix(value, "/artifact/"),
		strings.HasPrefix(value, "/exposure/"):
		return ""
	default:
		return value
	}
}

func BuildFromCanonicalPaths(paths []string, sourceKind string, seenAt time.Time, lastSourceJobID string) map[string]models.LensResearchObjectRef {
	out := make(map[string]models.LensResearchObjectRef)
	for _, raw := range paths {
		ref := NormalizeObjectRef(raw)
		if ref == "" {
			continue
		}
		item := models.LensResearchObjectRef{
			CanonicalPath: ref,
		}
		if kind := strings.TrimSpace(sourceKind); kind != "" {
			item.SourceKinds = []string{kind}
		}
		if jobID := strings.TrimSpace(lastSourceJobID); jobID != "" {
			item.LastSourceJobID = jobID
		}
		if !seenAt.IsZero() {
			seen := seenAt.UTC()
			item.FirstSeenAt = &seen
			item.LastSeenAt = &seen
		}
		out[ref] = item
	}
	return out
}

func BuildFromLensArtifacts(artifacts []*models.LensArtifact) map[string]models.LensResearchObjectRef {
	out := make(map[string]models.LensResearchObjectRef)
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Type != models.LensArtifactTypeFragment {
			continue
		}
		seenAt := artifact.UpdatedAt
		if seenAt.IsZero() {
			seenAt = artifact.AddedAt
		}
		out = MergeResearchObjects(out, BuildFromCanonicalPaths(
			[]string{artifact.CanonicalPath},
			SourceKindLensArtifactFragment,
			seenAt,
			artifact.SourceJobID,
		))
	}
	return out
}

func BuildFromMaterializedArtifacts(artifacts []*models.MaterializedArtifact) map[string]models.LensResearchObjectRef {
	out := make(map[string]models.LensResearchObjectRef)
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		out = MergeResearchObjects(out, BuildFromCanonicalPaths(
			artifact.NodeRefs,
			SourceKindMaterializedNodeRef,
			artifact.UpdatedAt,
			"",
		))
	}
	return out
}

func BuildFromScopeArtifacts(scopeArtifacts map[string]models.ScopeArtifactRef, seenAt time.Time) map[string]models.LensResearchObjectRef {
	if len(scopeArtifacts) == 0 {
		return nil
	}
	out := make(map[string]models.LensResearchObjectRef)
	for _, artifact := range scopeArtifacts {
		out = MergeResearchObjects(out, BuildFromCanonicalPaths(
			artifact.NodeRefs,
			SourceKindScopeArtifactNodeRef,
			seenAt,
			"",
		))
	}
	return out
}

func MergeResearchObjects(existing, incoming map[string]models.LensResearchObjectRef) map[string]models.LensResearchObjectRef {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}
	out := make(map[string]models.LensResearchObjectRef, len(existing)+len(incoming))
	for key, item := range existing {
		ref := canonicalKeyOrValue(key, item)
		if ref == "" {
			continue
		}
		item.CanonicalPath = ref
		item.SourceKinds = uniqueSortedStrings(item.SourceKinds)
		out[ref] = item
	}
	for key, item := range incoming {
		ref := canonicalKeyOrValue(key, item)
		if ref == "" {
			continue
		}
		item.CanonicalPath = ref
		item.SourceKinds = uniqueSortedStrings(item.SourceKinds)
		current, ok := out[ref]
		if !ok {
			out[ref] = item
			continue
		}
		current.CanonicalPath = ref
		current.SourceKinds = uniqueSortedStrings(append(current.SourceKinds, item.SourceKinds...))
		if strings.TrimSpace(item.LastSourceJobID) != "" {
			current.LastSourceJobID = strings.TrimSpace(item.LastSourceJobID)
		}
		current.FirstSeenAt = minTimePtr(current.FirstSeenAt, item.FirstSeenAt)
		current.LastSeenAt = maxTimePtr(current.LastSeenAt, item.LastSeenAt)
		out[ref] = current
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ReplaceResearchObjectCanonicalPath(objects map[string]models.LensResearchObjectRef, fromPath, toPath string) bool {
	fromPath = NormalizeObjectRef(fromPath)
	toPath = NormalizeObjectRef(toPath)
	if len(objects) == 0 || fromPath == "" || toPath == "" || fromPath == toPath {
		return false
	}
	current, ok := findResearchObject(objects, fromPath)
	if !ok {
		return false
	}
	current.CanonicalPath = toPath
	existing, hasExisting := findResearchObject(objects, toPath)
	for key, item := range objects {
		normKey := NormalizeObjectRef(key)
		normValue := NormalizeObjectRef(item.CanonicalPath)
		if normKey == fromPath || normValue == fromPath || normKey == toPath || normValue == toPath {
			delete(objects, key)
		}
	}
	base := map[string]models.LensResearchObjectRef{}
	if hasExisting {
		existing.CanonicalPath = toPath
		base[toPath] = existing
	}
	objects[toPath] = MergeResearchObjects(base, map[string]models.LensResearchObjectRef{toPath: current})[toPath]
	return true
}

func findResearchObject(objects map[string]models.LensResearchObjectRef, target string) (models.LensResearchObjectRef, bool) {
	target = NormalizeObjectRef(target)
	if target == "" {
		return models.LensResearchObjectRef{}, false
	}
	if item, ok := objects[target]; ok {
		item.CanonicalPath = target
		return item, true
	}
	for key, item := range objects {
		if NormalizeObjectRef(key) == target || NormalizeObjectRef(item.CanonicalPath) == target {
			item.CanonicalPath = target
			return item, true
		}
	}
	return models.LensResearchObjectRef{}, false
}

func canonicalKeyOrValue(key string, item models.LensResearchObjectRef) string {
	if ref := NormalizeObjectRef(item.CanonicalPath); ref != "" {
		return ref
	}
	return NormalizeObjectRef(key)
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func minTimePtr(a, b *time.Time) *time.Time {
	switch {
	case a == nil:
		return cloneTimePtr(b)
	case b == nil:
		return cloneTimePtr(a)
	case a.UTC().Before(b.UTC()):
		return cloneTimePtr(a)
	default:
		return cloneTimePtr(b)
	}
}

func maxTimePtr(a, b *time.Time) *time.Time {
	switch {
	case a == nil:
		return cloneTimePtr(b)
	case b == nil:
		return cloneTimePtr(a)
	case a.UTC().After(b.UTC()):
		return cloneTimePtr(a)
	default:
		return cloneTimePtr(b)
	}
}

func cloneTimePtr(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	value := in.UTC()
	return &value
}
