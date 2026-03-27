package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	gcp "github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

func main() {
	project := flag.String("project", "etigram", "GCP project id")
	pathsArg := flag.String("paths", "", "comma-separated canonical paths")
	clear := flag.Bool("clear", false, "delete matching metrics instead of expiring them")
	sourceKind := flag.String("source-kind", "grounded_search", "only touch metrics with this source kind")
	prefix := flag.String("prefix", "/metric/", "metric path prefix to touch")
	flag.Parse()

	paths := compactStrings(strings.Split(strings.TrimSpace(*pathsArg), ","))
	if len(paths) == 0 {
		log.Fatal("--paths is required")
	}

	ctx := context.Background()
	client, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*project))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	repo := horizonrepo.NewHorizonFragmentsRepository(client.Client())
	expiredAt := time.Now().UTC().Add(-1 * time.Minute)
	updated := 0
	for _, path := range paths {
		fragment, err := repo.GetByCanonicalPath(ctx, path)
		if err != nil {
			log.Printf("skip %s: %v", path, err)
			continue
		}
		if fragment == nil {
			log.Printf("skip %s: fragment not found", path)
			continue
		}
		changed := false
		for metricPath, metric := range fragment.State {
			if !strings.HasPrefix(strings.TrimSpace(metricPath), strings.TrimSpace(*prefix)) {
				continue
			}
			metricSourceKind := ""
			if metric.SourceKind != nil {
				metricSourceKind = strings.TrimSpace(*metric.SourceKind)
			}
			if strings.TrimSpace(*sourceKind) != "" && !strings.EqualFold(metricSourceKind, strings.TrimSpace(*sourceKind)) {
				continue
			}
			if *clear {
				delete(fragment.State, metricPath)
				changed = true
				updated++
				continue
			}
			metric.FreshUntil = &expiredAt
			fragment.State[metricPath] = metric
			changed = true
			updated++
		}
		if !changed {
			continue
		}
		fragment.UpdatedAt = time.Now().UTC()
		if fragment.State == nil {
			fragment.State = map[string]horizonmodels.MetricValue{}
		}
		if err := repo.UpdateDerived(ctx, fragment); err != nil {
			log.Fatalf("update %s: %v", path, err)
		}
		fmt.Printf("updated %s\n", path)
	}
	fmt.Printf("metrics_touched=%d\n", updated)
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
