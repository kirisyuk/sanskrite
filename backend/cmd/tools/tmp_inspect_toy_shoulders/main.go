package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/simscope"
)

func main() {
	project := flag.String("project", "nv-horizon", "gcp project")
	runID := flag.String("run", "", "simulation run id")
	country := flag.String("country", "", "country code to inspect")
	limit := flag.Int("limit", 12, "max artifacts")
	flag.Parse()
	if strings.TrimSpace(*runID) == "" || strings.TrimSpace(*country) == "" {
		log.Fatal("--run and --country are required")
	}
	ctx := context.Background()
	client, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*project))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	runs := horizonrepo.NewSimulationRunsRepository(client.Client())
	materialized := horizonrepo.NewMaterializedArtifactsRepository(client.Client())
	run, err := runs.Get(ctx, strings.TrimSpace(*runID))
	if err != nil {
		log.Fatal(err)
	}
	profile := simscope.BuildProfile(strings.TrimSpace(run.Request.Question), nil, run.BaselineRefs, run.Recognition)
	artifacts, err := materialized.ListByLens(ctx, strings.TrimSpace(run.BaselineRefs.LensID), 256)
	if err != nil {
		log.Fatal(err)
	}
	filtered := simscope.FilterArtifacts(artifacts, profile)
	code := strings.ToLower(strings.TrimSpace(*country))
	printed := 0
	for _, artifact := range filtered {
		if artifact == nil {
			continue
		}
		matched := false
		for _, ref := range artifact.NodeRefs {
			if countryCodeFromPath(ref) == code {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		printed++
		fmt.Printf("\nARTIFACT %d\n", printed)
		fmt.Printf("key=%s\n", strings.TrimSpace(artifact.CanonicalKey))
		fmt.Printf("product=%s\n", strings.TrimSpace(artifact.ProductAxis))
		fmt.Printf("supplier=%s\n", strings.TrimSpace(artifact.AnchorSupplier))
		fmt.Printf("customer=%s\n", strings.TrimSpace(artifact.AnchorCustomer))
		for _, ref := range artifact.NodeRefs {
			if c := countryCodeFromPath(ref); c == code || c == "cn" || c == "kz" || c == "ru" || c == "de" || c == "us" || c == "cz" || c == "hu" {
				fmt.Println(" ", ref)
			}
		}
		if printed >= *limit {
			break
		}
	}
	fmt.Printf("\nprinted=%d filtered=%d\n", printed, len(filtered))
}

func countryCodeFromPath(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "loc", "fac", "org", "market", "gov":
		return strings.ToLower(strings.TrimSpace(parts[1]))
	default:
		return ""
	}
}
