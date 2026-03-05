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

	col := fs.Collection("horizon_fragments")
	q := col.
		Where("tenantId", "==", "global").
		Where("canonicalPath", ">=", "/zone/").
		Where("canonicalPath", "<", "/zone/\ufffd").
		OrderBy("canonicalPath", firestore.Asc).
		Limit(10000)
	docs, err := q.Documents(ctx).GetAll()
	if err != nil {
		log.Fatalf("query: %v", err)
	}

	var total, withCoords, withRadius, withBoth int
	var noCoords, noRadius []string

	for _, d := range docs {
		var f models.HorizonFragment
		if err := d.DataTo(&f); err != nil {
			continue
		}
		if f.ID == "" {
			f.ID = d.Ref.ID
		}
		if f.Type != models.FragmentTypeAffectedZone {
			continue
		}
		total++

		_, _, hasCoords := hrepo.ResolveFragmentCoordinates(&f)
		if hasCoords {
			withCoords++
		} else {
			noCoords = append(noCoords, f.CanonicalPath)
		}

		if f.RadiusKm > 0 {
			withRadius++
		} else {
			noRadius = append(noRadius, f.CanonicalPath)
		}

		if hasCoords && f.RadiusKm > 0 {
			withBoth++
		}
	}

	log.Printf("=== AFFECTED_ZONE in Firestore ===")
	log.Printf("total:              %d", total)
	log.Printf("with coordinates:   %d", withCoords)
	log.Printf("with radiusKm > 0:   %d", withRadius)
	log.Printf("with BOTH (map):     %d", withBoth)
	log.Printf("")
	if len(noCoords) > 0 {
		log.Printf("--- no coordinates (%d) ---", len(noCoords))
		for i, cp := range noCoords {
			if i >= 15 {
				log.Printf("  ... and %d more", len(noCoords)-15)
				break
			}
			log.Printf("  %s", cp)
		}
	}
	if len(noRadius) > 0 {
		log.Printf("--- radiusKm=0 or missing (%d) ---", len(noRadius))
		for i, cp := range noRadius {
			if i >= 15 {
				log.Printf("  ... and %d more", len(noRadius)-15)
				break
			}
			log.Printf("  %s", cp)
		}
	}
}
