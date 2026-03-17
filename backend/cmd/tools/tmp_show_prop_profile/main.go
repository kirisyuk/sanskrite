package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	projectID := flag.String("project", "", "GCP project id")
	axis := flag.String("axis", "physical_flow/supply", "axis key")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}

	ctx := context.Background()
	client, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	repo := hrepo.NewPropagationProfilesRepository(client.Client())
	profile, err := repo.Get(ctx, strings.TrimSpace(*axis))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("axis=%s damping=%.2f hops=%d mode=%s\n", profile.AxisKey, profile.DefaultDamping, profile.DefaultMaxHops, profile.PropagationMode)
	for _, spec := range profile.StrongEdges {
		fmt.Printf("%s %s %.2f\n", spec.Type, spec.Direction, spec.Weight)
	}
}
