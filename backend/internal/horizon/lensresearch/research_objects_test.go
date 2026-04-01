package lensresearch

import (
	"testing"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestMergeResearchObjectsDedupesAndPreservesBounds(t *testing.T) {
	first := time.Date(2026, 3, 29, 8, 0, 0, 0, time.UTC)
	last := time.Date(2026, 3, 30, 9, 0, 0, 0, time.UTC)
	merged := MergeResearchObjects(
		BuildFromCanonicalPaths([]string{"/org/us/albemarle"}, SourceKindLensArtifactFragment, last, "job-new"),
		BuildFromCanonicalPaths([]string{"/org/us/albemarle"}, SourceKindMaterializedNodeRef, first, ""),
	)

	item, ok := merged["/org/us/albemarle"]
	if !ok {
		t.Fatalf("expected merged item")
	}
	if got := len(item.SourceKinds); got != 2 {
		t.Fatalf("expected 2 source kinds, got %d", got)
	}
	if item.FirstSeenAt == nil || !item.FirstSeenAt.Equal(first) {
		t.Fatalf("expected firstSeenAt=%s, got %#v", first, item.FirstSeenAt)
	}
	if item.LastSeenAt == nil || !item.LastSeenAt.Equal(last) {
		t.Fatalf("expected lastSeenAt=%s, got %#v", last, item.LastSeenAt)
	}
	if item.LastSourceJobID != "job-new" {
		t.Fatalf("expected lastSourceJobID to survive, got %q", item.LastSourceJobID)
	}
}

func TestReplaceResearchObjectCanonicalPathMergesTarget(t *testing.T) {
	now := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)
	objects := map[string]models.LensResearchObjectRef{}
	for key, value := range BuildFromCanonicalPaths([]string{"/org/legacy"}, SourceKindLensArtifactFragment, now, "job-a") {
		objects[key] = value
	}
	for key, value := range BuildFromCanonicalPaths([]string{"/org/canonical"}, SourceKindMaterializedNodeRef, now.Add(time.Hour), "") {
		objects[key] = value
	}

	if !ReplaceResearchObjectCanonicalPath(objects, "/org/legacy", "/org/canonical") {
		t.Fatalf("expected rewrite to happen")
	}
	if _, ok := objects["/org/legacy"]; ok {
		t.Fatalf("legacy key should be removed")
	}
	item, ok := objects["/org/canonical"]
	if !ok {
		t.Fatalf("canonical key should exist")
	}
	if got := len(item.SourceKinds); got != 2 {
		t.Fatalf("expected merged source kinds, got %d", got)
	}
	if item.LastSourceJobID != "job-a" {
		t.Fatalf("expected lastSourceJobID to merge, got %q", item.LastSourceJobID)
	}
}
