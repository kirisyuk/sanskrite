package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	h "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type dupRow struct {
	Path  string
	Count int
	IDs   []string
}

func main() {
	ctx := context.Background()
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	prefix := flag.String("prefix", "", "canonical path prefix filter")
	limit := flag.Int("limit", 25, "top duplicate paths to print")
	flag.Parse()

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		panic(err)
	}
	defer fs.Close()

	q := fs.Client().Collection("horizon_fragments").Where("tenantId", "==", strings.TrimSpace(*tenantID))
	if p := strings.TrimSpace(*prefix); p != "" {
		q = q.Where("canonicalPath", ">=", p).Where("canonicalPath", "<", p+"\ufffd").OrderBy("canonicalPath", firestore.Asc)
	}

	paths := map[string][]string{}
	iter := q.Documents(ctx)
	defer iter.Stop()
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			panic(err)
		}
		var frag h.HorizonFragment
		if err := doc.DataTo(&frag); err != nil {
			continue
		}
		cp := strings.TrimSpace(frag.CanonicalPath)
		if cp == "" {
			continue
		}
		id := strings.TrimSpace(frag.ID)
		if id == "" {
			id = strings.TrimSpace(doc.Ref.ID)
		}
		paths[cp] = append(paths[cp], id)
	}

	rows := make([]dupRow, 0, len(paths))
	for path, ids := range paths {
		if len(ids) < 2 {
			continue
		}
		sort.Strings(ids)
		rows = append(rows, dupRow{Path: path, Count: len(ids), IDs: ids})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count == rows[j].Count {
			return rows[i].Path < rows[j].Path
		}
		return rows[i].Count > rows[j].Count
	})
	if *limit > 0 && len(rows) > *limit {
		rows = rows[:*limit]
	}

	fmt.Printf("tenant=%s project=%s prefix=%q duplicate_paths=%d\n", strings.TrimSpace(*tenantID), strings.TrimSpace(*projectID), strings.TrimSpace(*prefix), len(rows))
	for _, row := range rows {
		fmt.Printf("- %s count=%d ids=%s\n", row.Path, row.Count, strings.Join(row.IDs, ","))
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
