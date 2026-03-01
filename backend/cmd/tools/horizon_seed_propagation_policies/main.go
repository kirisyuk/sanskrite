package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/policy"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	var projectID = flag.String("project", "etigram", "GCP project id")
	flag.Parse()

	ctx := context.Background()
	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init: %v", err)
	}
	defer fs.Close()

	repo := horizonrepo.NewPropagationPoliciesRepository(fs)

	// Minimal propagation policies for semantic risk propagation
	policies := []*policy.PropagationPolicyRule{
		// Risk metrics flow strongly through supply chain relationships
		{
			MetricPattern:     "/metric/risk/*",
			RelationType:      "SUPPLIES_TO",
			Passes:            true,
			Weight:            1.2,
			MaxDepth:          4,
			DampingOverride:   floatPtr(0.85), // Less damping
			Direction:         "both",
			CriticalPathBoost: 1.3,
			Priority:          100,
		},
		{
			MetricPattern:     "/metric/risk/*",
			RelationType:      "DEPENDS_ON",
			Passes:            true,
			Weight:            1.1,
			MaxDepth:          4,
			DampingOverride:   floatPtr(0.80),
			Direction:         "both",
			CriticalPathBoost: 1.2,
			Priority:          90,
		},
		// Risk flows weaker through competition (indirect effect)
		{
			MetricPattern:     "/metric/risk/*",
			RelationType:      "COMPETES_WITH",
			Passes:            true,
			Weight:            0.3,
			MaxDepth:          2,
			DampingOverride:   floatPtr(0.5),
			Direction:         "both",
			CriticalPathBoost: 1.0,
			Priority:          50,
		},
		// Risk flows through ownership strongly
		{
			MetricPattern:     "/metric/risk/*",
			RelationType:      "OWNS",
			Passes:            true,
			Weight:            1.0,
			MaxDepth:          3,
			DampingOverride:   floatPtr(0.9), // Minimal damping for ownership
			Direction:         "both",
			CriticalPathBoost: 1.2,
			Priority:          80,
		},
		// Location-based risks (geopolitical)
		{
			MetricPattern:     "/metric/risk/geopolitical*",
			RelationType:      "LOCATED_IN",
			Passes:            true,
			Weight:            1.5,
			MaxDepth:          2,
			DampingOverride:   floatPtr(0.7),
			Direction:         "source_to_target", // Location affects entities, not reverse
			CriticalPathBoost: 1.0,
			Priority:          70,
		},
		// Default: all metrics through INFLUENCES (sphere linkage)
		{
			MetricPattern:     "*",
			RelationType:      "INFLUENCES",
			Passes:            true,
			Weight:            0.8,
			MaxDepth:          3,
			DampingOverride:   nil, // Use default damping
			Direction:         "both",
			CriticalPathBoost: 1.1,
			Priority:          10,
		},
		// Block unrelated relation types for risks (example)
		{
			MetricPattern:     "/metric/risk/*",
			RelationType:      "PRODUCES",
			Passes:            false,
			Weight:            0,
			MaxDepth:          0,
			DampingOverride:   nil,
			Direction:         "",
			CriticalPathBoost: 1.0,
			Priority:          60,
		},
	}

	created := 0
	for _, p := range policies {
		id, wasCreated, err := repo.UpsertByMetricPatternAndRelationType(ctx, p)
		if err != nil {
			log.Printf("Failed to create policy %s×%s: %v", p.MetricPattern, p.RelationType, err)
			continue
		}
		if wasCreated {
			fmt.Printf("Created policy: id=%s metric=%s rel=%s passes=%v weight=%.2f\n",
				id, p.MetricPattern, p.RelationType, p.Passes, p.Weight)
			created++
		} else {
			fmt.Printf("Updated policy: id=%s metric=%s rel=%s passes=%v weight=%.2f\n",
				id, p.MetricPattern, p.RelationType, p.Passes, p.Weight)
		}
	}

	fmt.Printf("\n=== Seeded %d propagation policies ===\n", created)
}

func floatPtr(f float64) *float64 {
	return &f
}
