package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"

	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	projectID := flag.String("project", "", "GCP project id")
	lensID := flag.String("lens-id", "", "lens id")
	filter := flag.String("filter", "", "optional case-insensitive substring filter")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if strings.TrimSpace(*lensID) == "" {
		log.Fatal("--lens-id is required")
	}

	ctx := context.Background()
	client, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore client: %v", err)
	}
	defer client.Close()

	repo := horizonrepo.NewLensesRepository(client)
	lens, err := repo.Get(ctx, strings.TrimSpace(*lensID))
	if err != nil {
		log.Fatalf("load lens: %v", err)
	}

	refs := map[string]struct{}{}
	for _, path := range lens.Scope {
		path = strings.TrimSpace(path)
		if path != "" {
			refs[path] = struct{}{}
		}
	}
	for _, ref := range lens.ScopeArtifacts {
		for _, path := range ref.NodeRefs {
			path = strings.TrimSpace(path)
			if path != "" {
				refs[path] = struct{}{}
			}
		}
		for _, path := range ref.GeoRefs {
			path = strings.TrimSpace(path)
			if path != "" {
				refs[path] = struct{}{}
			}
		}
		for _, path := range ref.FlowRefs {
			path = strings.TrimSpace(path)
			if path != "" {
				refs[path] = struct{}{}
			}
		}
		for _, path := range ref.ZoneRefs {
			path = strings.TrimSpace(path)
			if path != "" {
				refs[path] = struct{}{}
			}
		}
		for _, path := range ref.AffectedZoneRefs {
			path = strings.TrimSpace(path)
			if path != "" {
				refs[path] = struct{}{}
			}
		}
	}

	items := make([]string, 0, len(refs))
	for ref := range refs {
		items = append(items, ref)
	}
	sort.Strings(items)

	needle := strings.ToLower(strings.TrimSpace(*filter))
	fmt.Printf("lens=%s name=%q tenant=%s refs=%d\n", lens.ID, lens.Name, lens.TenantID, len(items))
	for _, item := range items {
		if needle != "" && !strings.Contains(strings.ToLower(item), needle) {
			continue
		}
		fmt.Println(item)
	}
}
