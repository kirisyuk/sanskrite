package repo

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestDocumentsRepositoryGetObjectPathUsesGlobalSegmentForPublicSources(t *testing.T) {
	repo := &DocumentsRepository{bucket: "test-bucket"}
	doc := &models.Document{
		ID:       "doc-1",
		SourceID: "source-1",
		TenantID: "",
	}
	if got, want := repo.getObjectPath(doc), "global/source-1/doc-1.txt"; got != want {
		t.Fatalf("getObjectPath() = %q, want %q", got, want)
	}
	if got, want := repo.getStoragePath(doc), "gs://test-bucket/global/source-1/doc-1.txt"; got != want {
		t.Fatalf("getStoragePath() = %q, want %q", got, want)
	}
}

