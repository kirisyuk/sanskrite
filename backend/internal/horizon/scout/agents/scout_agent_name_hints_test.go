package agents

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestApplyExtractedNameHints_DoesNotOverrideExistingLabel(t *testing.T) {
	f := &models.HorizonFragment{
		Labels: map[string]string{
			"en": "Ri Chol Man",
		},
		Identifiers: map[string]string{},
	}

	applyExtractedNameHints(f, "ri-chol-man", "")

	if got := f.Labels["en"]; got != "Ri Chol Man" {
		t.Fatalf("labels.en overridden: got %q want %q", got, "Ri Chol Man")
	}
	if got := f.Identifiers["legalName"]; got != "ri-chol-man" {
		t.Fatalf("identifiers.legalName should still be filled when missing: got %q", got)
	}
}

func TestApplyExtractedNameHints_FillsMissingFields(t *testing.T) {
	f := &models.HorizonFragment{}

	applyExtractedNameHints(f, "Kim Jong Un", "Kim")

	if got := f.Labels["en"]; got != "Kim Jong Un" {
		t.Fatalf("labels.en = %q, want %q", got, "Kim Jong Un")
	}
	if got := f.Identifiers["legalName"]; got != "Kim Jong Un" {
		t.Fatalf("identifiers.legalName = %q, want %q", got, "Kim Jong Un")
	}
	if got := f.Identifiers["shortName"]; got != "Kim" {
		t.Fatalf("identifiers.shortName = %q, want %q", got, "Kim")
	}
}
