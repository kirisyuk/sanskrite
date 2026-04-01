package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/cmd/tools/internal/artifactrewrite"
	"github.com/igorkirisyuk/sanskrite/internal/gcp"
)

func main() {
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "nv-horizon"), "GCP project id")
	nodeRef := flag.String("node-ref", "", "canonical fragment path to rewrite")
	lensID := flag.String("lens-id", "", "optional lens id filter")
	limit := flag.Int("limit", 200, "max artifacts to inspect")
	apply := flag.Bool("apply", false, "persist rewritten artifacts")
	flag.Parse()

	if strings.TrimSpace(*nodeRef) == "" {
		log.Fatal("--node-ref is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("failed to create firestore client: %v", err)
	}
	defer fs.Close()

	if _, err := artifactrewrite.RewriteNodeArtifacts(
		ctx,
		fs.Client(),
		strings.TrimSpace(*nodeRef),
		strings.TrimSpace(*lensID),
		*limit,
		*apply,
		os.Stdout,
	); err != nil {
		log.Fatalf("rewrite node artifacts failed: %v", err)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
