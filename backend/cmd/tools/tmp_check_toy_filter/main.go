package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/simscope"
)

func main() {
	ctx := context.Background()
	client, err := gcp.NewFirestoreClient(ctx, "nv-horizon")
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	runs := horizonrepo.NewSimulationRunsRepository(client.Client())
	materialized := horizonrepo.NewMaterializedArtifactsRepository(client.Client())

	run, err := runs.Get(ctx, "rthGGi2htvYVhKVooBPF")
	if err != nil {
		log.Fatal(err)
	}
	profile := simscope.BuildProfile(strings.TrimSpace(run.Request.Question), nil, run.BaselineRefs, run.Recognition)
	artifacts, err := materialized.ListByLens(ctx, strings.TrimSpace(run.BaselineRefs.LensID), 256)
	if err != nil {
		log.Fatal(err)
	}
	filtered := simscope.FilterArtifacts(artifacts, profile)
	fmt.Printf("domainTokens=%v\n", profile.DomainProductTokens)
	fmt.Printf("allArtifacts=%d filteredArtifacts=%d\n", len(artifacts), len(filtered))
	countries := map[string]int{}
	products := map[string]int{}
	for _, artifact := range filtered {
		if artifact == nil {
			continue
		}
		productAxis := strings.TrimSpace(artifact.ProductAxis)
		if productAxis == "" {
			continue
		}
		products[productAxis]++
		for _, ref := range artifact.NodeRefs {
			if code := countryCodeFromPath(ref); code != "" {
				countries[code]++
			}
		}
		if strings.Contains(productAxis, "automotive") ||
			strings.Contains(productAxis, "nuclear") ||
			strings.Contains(productAxis, "container-transshipment") ||
			strings.Contains(productAxis, "electronics") ||
			strings.Contains(productAxis, "natural-gas") {
			fmt.Printf("OFFTOPIC product=%s key=%s\n", productAxis, strings.TrimSpace(artifact.CanonicalKey))
		}
	}
	printTop("countries", countries, 8)
	printTop("products", products, 12)
}

func countryCodeFromPath(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "loc", "fac", "org", "market", "gov":
		return strings.ToLower(strings.TrimSpace(parts[1]))
	}
	return ""
}

func printTop(label string, counts map[string]int, limit int) {
	type row struct {
		key   string
		count int
	}
	rows := make([]row, 0, len(counts))
	for key, count := range counts {
		rows = append(rows, row{key: key, count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count == rows[j].count {
			return rows[i].key < rows[j].key
		}
		return rows[i].count > rows[j].count
	})
	fmt.Printf("%s:\n", label)
	for i := 0; i < len(rows) && i < limit; i++ {
		fmt.Printf("  %d  %s\n", rows[i].count, rows[i].key)
	}
}
