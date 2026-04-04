package discovery

import (
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/models"
)

func TestShouldRunRSSSignalPipeline(t *testing.T) {
	t.Run("nil source", func(t *testing.T) {
		if shouldRunRSSSignalPipeline(nil) {
			t.Fatalf("expected nil source to skip signal pipeline")
		}
	})

	t.Run("plain rss source", func(t *testing.T) {
		ds := &models.DataSource{}
		if shouldRunRSSSignalPipeline(ds) {
			t.Fatalf("expected plain rss source to skip signal pipeline")
		}
	})

	t.Run("ontology fact output", func(t *testing.T) {
		mode := models.SourceOutputModeOntologyFact
		ds := &models.DataSource{Details: models.SourceDetails{OutputMode: &mode}}
		if !shouldRunRSSSignalPipeline(ds) {
			t.Fatalf("expected ontology fact source to run signal pipeline")
		}
	})

	t.Run("horizon monitor metadata", func(t *testing.T) {
		signalType := "risk:regional_strike_geography"
		ds := &models.DataSource{Details: models.SourceDetails{HorizonMonitorSignalType: &signalType}}
		if !shouldRunRSSSignalPipeline(ds) {
			t.Fatalf("expected horizon monitor metadata to run signal pipeline")
		}
	})

	t.Run("heuristic processing mode", func(t *testing.T) {
		mode := models.SourceProcessingModeHeuristic
		ds := &models.DataSource{Details: models.SourceDetails{ProcessingMode: &mode}}
		if !shouldRunRSSSignalPipeline(ds) {
			t.Fatalf("expected heuristic source to run signal pipeline")
		}
	})
}
