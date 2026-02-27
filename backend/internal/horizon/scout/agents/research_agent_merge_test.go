package agents

import (
	"strings"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestMergeFragment_HomeLocationPreservesExistingISOFields(t *testing.T) {
	a := NewResearchAgent(nil)

	iso3166 := "AE"
	iso6709 := "+25.204800+55.270800/"
	main := &models.HorizonFragment{
		HomeLocation: &models.Location{
			Name:      "Dubai",
			ISO3166_1: &iso3166,
			ISO6709:   &iso6709,
		},
	}
	partial := &models.HorizonFragment{
		HomeLocation: &models.Location{
			Name: "Dubai City",
		},
	}

	a.mergeFragment(main, partial)

	if main.HomeLocation == nil {
		t.Fatalf("homeLocation should remain present")
	}
	if main.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*main.HomeLocation.ISO3166_1) != "AE" {
		t.Fatalf("iso3166_1 should be preserved, got %+v", main.HomeLocation.ISO3166_1)
	}
	if main.HomeLocation.ISO6709 == nil || strings.TrimSpace(*main.HomeLocation.ISO6709) != "+25.204800+55.270800/" {
		t.Fatalf("iso6709 should be preserved, got %+v", main.HomeLocation.ISO6709)
	}
}

func TestMergeFragment_HomeLocationFillsMissingISOFields(t *testing.T) {
	a := NewResearchAgent(nil)

	iso3166 := "US"
	iso6709 := "+29.760400-95.369800/"
	main := &models.HorizonFragment{
		HomeLocation: &models.Location{
			Name: "Houston",
		},
	}
	partial := &models.HorizonFragment{
		HomeLocation: &models.Location{
			Name:      "Houston",
			ISO3166_1: &iso3166,
			ISO6709:   &iso6709,
		},
	}

	a.mergeFragment(main, partial)

	if main.HomeLocation == nil {
		t.Fatalf("homeLocation should remain present")
	}
	if main.HomeLocation.ISO3166_1 == nil || strings.TrimSpace(*main.HomeLocation.ISO3166_1) != "US" {
		t.Fatalf("iso3166_1 should be filled, got %+v", main.HomeLocation.ISO3166_1)
	}
	if main.HomeLocation.ISO6709 == nil || strings.TrimSpace(*main.HomeLocation.ISO6709) != "+29.760400-95.369800/" {
		t.Fatalf("iso6709 should be filled, got %+v", main.HomeLocation.ISO6709)
	}
}
