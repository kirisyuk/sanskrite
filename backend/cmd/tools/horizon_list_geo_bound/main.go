package main

import (
	"context"
	"flag"
	"log"
	"strings"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var projectID = flag.String("project", "etigram", "GCP project ID")

func main() {
	flag.Parse()
	ctx := context.Background()

	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("Firestore: %v", err)
	}
	defer fs.Close()

	repo := hrepo.NewHorizonFragmentsRepository(fs)
	fragments, err := repo.ListGeoBound(ctx, 2500, "", nil)
	if err != nil {
		log.Fatalf("ListGeoBound: %v", err)
	}

	zones := 0
	for _, f := range fragments {
		if f.Type == models.FragmentTypeAffectedZone {
			zones++
		}
	}

	log.Printf("ListGeoBound(limit=2500, pathPrefix=\"\"): %d total, %d AFFECTED_ZONE", len(fragments), zones)
}
