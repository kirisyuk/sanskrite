package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/importance"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type outputRow struct {
	ID            string                     `json:"id"`
	CanonicalPath string                     `json:"canonicalPath"`
	Type          horizonmodels.FragmentType `json:"type"`
	Applied       bool                       `json:"applied"`
	Scorecard     importance.Scorecard       `json:"scorecard"`
}

func main() {
	var (
		projectID  = flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id")
		tenantID   = flag.String("tenant", "global", "tenant id")
		fragmentID = flag.String("id", "", "fragment id")
		path       = flag.String("path", "", "canonical path")
		typeCSV    = flag.String("type", "", "fragment type or comma-separated fragment types")
		prefix     = flag.String("prefix", "", "canonical path prefix")
		limit      = flag.Int("limit", 50, "max fragments to score in batch mode")
		workers    = flag.Int("workers", 8, "number of concurrent workers in batch mode")
		apply      = flag.Bool("apply", false, "persist metrics into fragment.State using UpdateFast")
	)
	flag.Parse()

	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required (or set GCP_PROJECT_ID)")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()

	fragmentsRepo := horizonrepo.NewHorizonFragmentsRepository(fsClient.Client())
	edgesRepo := horizonrepo.NewDependencyEdgesRepository(fsClient.Client())
	scorer := importance.NewScorer(fragmentsRepo, edgesRepo)

	fragments, err := resolveTargets(ctx, fragmentsRepo, *tenantID, strings.TrimSpace(*fragmentID), strings.TrimSpace(*path), strings.TrimSpace(*typeCSV), strings.TrimSpace(*prefix), *limit)
	if err != nil {
		log.Fatal(err)
	}
	if len(fragments) == 0 {
		log.Fatal("no fragments matched")
	}

	now := time.Now().UTC()
	if *workers < 1 {
		*workers = 1
	}
	if *workers > len(fragments) {
		*workers = len(fragments)
	}

	rows := make([]outputRow, 0, len(fragments))
	var rowsMu sync.Mutex
	var processed atomic.Int64
	var appliedCount atomic.Int64
	var failed atomic.Int64

	jobs := make(chan *horizonmodels.HorizonFragment)
	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for frag := range jobs {
				if frag == nil {
					continue
				}
				score, _, err := scorer.ScoreFragment(ctx, *tenantID, frag.ID)
				if err != nil {
					failed.Add(1)
					log.Printf("score failed id=%s path=%s: %v", frag.ID, frag.CanonicalPath, err)
					n := processed.Add(1)
					logProgress(n, len(fragments), appliedCount.Load(), failed.Load())
					continue
				}

				row := outputRow{
					ID:            frag.ID,
					CanonicalPath: frag.CanonicalPath,
					Type:          frag.Type,
					Scorecard:     score,
				}
				if *apply {
					clone := *frag
					importance.ApplyScorecard(&clone, score, now)
					if err := fragmentsRepo.UpdateDerived(ctx, &clone); err != nil {
						failed.Add(1)
						log.Printf("apply failed id=%s path=%s: %v", frag.ID, frag.CanonicalPath, err)
					} else {
						row.Applied = true
						appliedCount.Add(1)
					}
				}

				rowsMu.Lock()
				rows = append(rows, row)
				rowsMu.Unlock()

				n := processed.Add(1)
				logProgress(n, len(fragments), appliedCount.Load(), failed.Load())
			}
		}()
	}
	for _, frag := range fragments {
		jobs <- frag
	}
	close(jobs)
	wg.Wait()

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CanonicalPath == rows[j].CanonicalPath {
			return rows[i].ID < rows[j].ID
		}
		return rows[i].CanonicalPath < rows[j].CanonicalPath
	})

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rows); err != nil {
		log.Fatalf("encode output: %v", err)
	}
}

func logProgress(processed int64, total int, applied int64, failed int64) {
	if processed == 1 || processed%25 == 0 || int(processed) == total {
		log.Printf("progress processed=%d/%d applied=%d failed=%d", processed, total, applied, failed)
	}
}

func resolveTargets(
	ctx context.Context,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	tenantID, fragmentID, path, typeCSV, prefix string,
	limit int,
) ([]*horizonmodels.HorizonFragment, error) {
	switch {
	case fragmentID != "":
		frag, err := fragmentsRepo.Get(ctx, fragmentID)
		if err != nil {
			return nil, err
		}
		if frag == nil {
			return nil, fmt.Errorf("fragment not found: %s", fragmentID)
		}
		return []*horizonmodels.HorizonFragment{frag}, nil
	case path != "":
		frag, err := fragmentsRepo.GetByCanonicalPath(ctx, path)
		if err != nil {
			return nil, err
		}
		if frag == nil {
			return nil, fmt.Errorf("fragment not found: %s", path)
		}
		return []*horizonmodels.HorizonFragment{frag}, nil
	case prefix != "":
		return fragmentsRepo.ListByCanonicalPathPrefix(ctx, prefix, limit)
	case typeCSV != "":
		var out []*horizonmodels.HorizonFragment
		seen := map[string]struct{}{}
		for _, rawType := range strings.Split(typeCSV, ",") {
			rawType = strings.ToUpper(strings.TrimSpace(rawType))
			if rawType == "" {
				continue
			}
			items, err := fragmentsRepo.ListByTypeAndTenant(ctx, tenantID, horizonmodels.FragmentType(rawType), limit)
			if err != nil {
				return nil, err
			}
			for _, frag := range items {
				if frag == nil || frag.ID == "" {
					continue
				}
				if _, ok := seen[frag.ID]; ok {
					continue
				}
				seen[frag.ID] = struct{}{}
				out = append(out, frag)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("one of --id, --path, --type, or --prefix is required")
	}
}
