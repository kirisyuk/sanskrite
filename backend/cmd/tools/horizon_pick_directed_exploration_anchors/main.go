package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/firestore"

	util "github.com/igorkirisyuk/sanskrite/cmd/tools/internal/directedexplorationutil"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	projectID := flag.String("project", "etigram", "GCP / Firestore project id")
	kindsRaw := flag.String("kinds", "org,gov,fac,product,deposit", "Comma-separated anchor kinds")
	limit := flag.Int("limit", 8, "Max samples per kind")
	flag.Parse()

	ctx := context.Background()
	fsClient, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient)

	kinds := splitCSV(*kindsRaw)
	if len(kinds) == 0 {
		log.Fatal("--kinds must not be empty")
	}

	out := make(map[string][]util.AnchorSample, len(kinds))
	for _, kind := range kinds {
		samples, err := util.PickAnchors(ctx, fragmentsRepo, kind, *limit)
		if err != nil {
			log.Fatalf("pick anchors kind=%s: %v", kind, err)
		}
		out[kind] = samples
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		log.Fatalf("encode output failed: %v", err)
	}
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}
