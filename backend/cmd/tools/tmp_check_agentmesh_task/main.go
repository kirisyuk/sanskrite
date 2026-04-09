package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
)

func main() {
	projectID := flag.String("project", strings.TrimSpace(os.Getenv("GCP_PROJECT_ID")), "GCP project ID")
	tenantID := flag.String("tenant", "global", "tenant ID")
	dedupKey := flag.String("dedup-key", "", "task dedup key")
	recent := flag.Bool("recent", false, "list recent tasks instead of querying one dedup key")
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if !*recent && strings.TrimSpace(*dedupKey) == "" {
		log.Fatal("--dedup-key is required")
	}

	ctx := context.Background()
	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fs.Close()

	if *recent {
		docs, err := fs.Client().Collection("agentmesh_tasks").
			Where("tenantId", "==", strings.TrimSpace(*tenantID)).
			Limit(20).
			Documents(ctx).
			GetAll()
		if err != nil {
			log.Fatalf("recent query failed: %v", err)
		}
		fmt.Printf("recent_count=%d\n", len(docs))
		for _, doc := range docs {
			data := doc.Data()
			lease, _ := data["lease"].(map[string]any)
			fmt.Printf("id=%s createdAt=%v status=%v capability=%v dedup=%v routedAgentId=%v\n",
				doc.Ref.ID,
				data["createdAt"],
				data["status"],
				data["capability"],
				lease["dedupKey"],
				data["routedAgentId"],
			)
		}
		return
	}

	docs, err := fs.Client().Collection("agentmesh_tasks").
		Where("tenantId", "==", strings.TrimSpace(*tenantID)).
		Where("lease.dedupKey", "==", strings.TrimSpace(*dedupKey)).
		Limit(5).
		Documents(ctx).
		GetAll()
	if err != nil {
		log.Fatalf("query failed: %v", err)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Ref.ID < docs[j].Ref.ID })
	fmt.Printf("count=%d\n", len(docs))
	for _, doc := range docs {
		data := doc.Data()
		lease, _ := data["lease"].(map[string]any)
		fmt.Printf("id=%s status=%v capability=%v dedup=%v routedAgentId=%v sessionRef=%v memoryBankRef=%v\n",
			doc.Ref.ID,
			data["status"],
			data["capability"],
			lease["dedupKey"],
			data["routedAgentId"],
			data["sessionRef"],
			data["memoryBankRef"],
		)
	}
}
