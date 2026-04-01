package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	edgeID := flag.String("id", "", "edge document id")
	flag.Parse()

	if strings.TrimSpace(*edgeID) == "" {
		log.Fatal("--id is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to init Firestore: %v", err)
	}
	defer fsClient.Close()

	db := fsClient.Client()
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(db)
	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(db)

	edge, err := edgesRepo.Get(ctx, strings.TrimSpace(*edgeID))
	if err != nil {
		log.Fatalf("failed to load edge: %v", err)
	}
	if edge == nil {
		log.Fatalf("edge not found: %s", strings.TrimSpace(*edgeID))
	}

	sourceFrag, _ := fragmentsRepo.Get(ctx, edge.SourceFragmentID)
	targetFrag, _ := fragmentsRepo.Get(ctx, edge.TargetFragmentID)

	fmt.Printf("edge: id=%s tenant=%s type=%s\n", edge.ID, edge.TenantID, edge.Type)
	fmt.Printf("source: id=%s path=%s\n", strings.TrimSpace(edge.SourceFragmentID), fragmentPath(sourceFrag))
	fmt.Printf("target: id=%s path=%s\n", strings.TrimSpace(edge.TargetFragmentID), fragmentPath(targetFrag))
	fmt.Printf("qualifier.sourceCanonicalPath=%s\n", qualifier(edge.Qualifiers, "sourceCanonicalPath"))
	fmt.Printf("qualifier.targetCanonicalPath=%s\n", qualifier(edge.Qualifiers, "targetCanonicalPath"))
	fmt.Printf("qualifier.productRelationKind=%s\n", qualifier(edge.Qualifiers, "productRelationKind"))
	fmt.Printf("qualifier.repairSource=%s\n", qualifier(edge.Qualifiers, "repairSource"))
	fmt.Printf("qualifier.repairSourceMode=%s\n", qualifier(edge.Qualifiers, "repairSourceMode"))
	fmt.Printf("qualifier.normalizedByProductRelationDictionary=%s\n", qualifier(edge.Qualifiers, "normalizedByProductRelationDictionary"))
	fmt.Printf("qualifier.normalizedFromEdgeType=%s\n", qualifier(edge.Qualifiers, "normalizedFromEdgeType"))
	fmt.Printf("qualifier.reason=%s\n", qualifier(edge.Qualifiers, "reason"))
}

func fragmentPath(frag *models.HorizonFragment) string {
	if frag == nil {
		return ""
	}
	return strings.TrimSpace(frag.CanonicalPath)
}

func qualifier(in map[string]interface{}, key string) string {
	if in == nil {
		return ""
	}
	value, ok := in[strings.TrimSpace(key)]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
