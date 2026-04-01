package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/focusobject"
)

func main() {
	var (
		fixturePath = flag.String("fixture", "", "path to focusobject fixture json")
		format      = flag.String("format", "markdown", "output format: markdown|json")
		nowFlag     = flag.String("now", "", "RFC3339 override for analysis time")
		anchorFlag  = flag.String("anchor", "", "override focus anchor canonical path")
		labelFlag   = flag.String("label", "", "override focus display label")
	)
	flag.Parse()

	if strings.TrimSpace(*fixturePath) == "" {
		cwd, _ := os.Getwd()
		*fixturePath = filepath.Join(cwd, "backend", "internal", "horizon", "focusobject", "testdata", "focus_battery_graph.json")
	}

	fixture, err := focusobject.LoadFixture(strings.TrimSpace(*fixturePath))
	if err != nil {
		log.Fatalf("load fixture: %v", err)
	}
	if strings.TrimSpace(*anchorFlag) != "" {
		fixture.Focus.AnchorCanonicalPath = strings.TrimSpace(*anchorFlag)
	}
	if strings.TrimSpace(*labelFlag) != "" {
		fixture.Focus.DisplayLabel = strings.TrimSpace(*labelFlag)
	}

	now := time.Now().UTC()
	if strings.TrimSpace(*nowFlag) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*nowFlag))
		if err != nil {
			log.Fatalf("parse -now: %v", err)
		}
		now = parsed.UTC()
	}

	snapshot, err := focusobject.NewAnalyzer().Analyze(focusobject.AnalyzeRequest{
		Focus:     fixture.Focus,
		Fragments: fixture.Fragments,
		Edges:     fixture.Edges,
		Artifacts: fixture.Artifacts,
		Facts:     fixture.Facts,
		Signals:   fixture.Signals,
		Now:       now,
	})
	if err != nil {
		log.Fatalf("analyze focus object: %v", err)
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(snapshot); err != nil {
			log.Fatalf("encode snapshot: %v", err)
		}
	default:
		fmt.Println(focusobject.Markdown(snapshot))
	}
}
