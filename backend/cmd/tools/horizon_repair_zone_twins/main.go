package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/importance"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
)

type repairRow struct {
	IdentityKey    string  `json:"identityKey"`
	PrimaryID      string  `json:"primaryId"`
	PrimaryPath    string  `json:"primaryPath"`
	PrimaryScore   float64 `json:"primaryScore"`
	DuplicateID    string  `json:"duplicateId"`
	DuplicatePath  string  `json:"duplicatePath"`
	DuplicateScore float64 `json:"duplicateScore"`
	Reason         string  `json:"reason"`
	Applied        bool    `json:"applied"`
}

func main() {
	var (
		projectID = flag.String("project", os.Getenv("GCP_PROJECT_ID"), "GCP project id")
		tenantID  = flag.String("tenant", "global", "tenant id")
		limit     = flag.Int("limit", 1000, "max affected zones to scan")
		apply     = flag.Bool("apply", false, "merge detected weak twins into richer siblings")
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
	mergeAuditRepo := horizonrepo.NewFragmentMergeAuditRepository(fsClient.Client())
	redirectsRepo := horizonrepo.NewCanonicalPathRedirectsRepository(fsClient.Client())
	mergeRepo := horizonrepo.NewFragmentsMergeRepository(fragmentsRepo, edgesRepo, mergeAuditRepo).WithRedirectsRepo(redirectsRepo)
	scorer := importance.NewScorer(fragmentsRepo, edgesRepo)

	items, err := fragmentsRepo.ListByTypeAndTenant(ctx, strings.TrimSpace(*tenantID), models.FragmentTypeAffectedZone, *limit)
	if err != nil {
		log.Fatalf("list affected zones: %v", err)
	}

	type group struct {
		identity scout.AffectedZoneIdentity
		items    []*models.HorizonFragment
	}
	groups := map[string]*group{}
	for _, frag := range items {
		if frag == nil || frag.Type != models.FragmentTypeAffectedZone {
			continue
		}
		identity, ok := scout.ParseAffectedZoneIdentity(frag.CanonicalPath)
		if !ok {
			continue
		}
		key := identity.Geo + "|" + identity.ConditionFamily + "|" + identity.IdentitySlug
		entry := groups[key]
		if entry == nil {
			entry = &group{identity: identity}
			groups[key] = entry
		}
		entry.items = append(entry.items, frag)
	}

	rows := make([]repairRow, 0, 32)
	touchedPrimary := map[string]struct{}{}

	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)

	for _, key := range groupKeys {
		entry := groups[key]
		if entry == nil || len(entry.items) < 2 {
			continue
		}
		sort.Slice(entry.items, func(i, j int) bool {
			si := scout.AffectedZoneEvidenceScore(entry.items[i])
			sj := scout.AffectedZoneEvidenceScore(entry.items[j])
			if si == sj {
				return strings.TrimSpace(entry.items[i].CanonicalPath) < strings.TrimSpace(entry.items[j].CanonicalPath)
			}
			return si > sj
		})
		primary := entry.items[0]
		for _, dup := range entry.items[1:] {
			ok, reason := scout.ShouldAutoMergeAffectedZoneTwin(primary, dup)
			if !ok {
				continue
			}
			row := repairRow{
				IdentityKey:    key,
				PrimaryID:      strings.TrimSpace(primary.ID),
				PrimaryPath:    strings.TrimSpace(primary.CanonicalPath),
				PrimaryScore:   scout.AffectedZoneEvidenceScore(primary),
				DuplicateID:    strings.TrimSpace(dup.ID),
				DuplicatePath:  strings.TrimSpace(dup.CanonicalPath),
				DuplicateScore: scout.AffectedZoneEvidenceScore(dup),
				Reason:         reason,
			}
			if *apply {
				if _, err := mergeRepo.MergeFragment(ctx, row.PrimaryID, row.DuplicateID, strings.TrimSpace(*tenantID), "tool_dedup", "horizon_repair_zone_twins", "", ""); err != nil {
					log.Printf("merge failed primary=%s duplicate=%s err=%v", row.PrimaryPath, row.DuplicatePath, err)
				} else {
					row.Applied = true
					touchedPrimary[row.PrimaryID] = struct{}{}
				}
			}
			rows = append(rows, row)
		}
	}

	if *apply && len(touchedPrimary) > 0 {
		now := time.Now().UTC()
		for primaryID := range touchedPrimary {
			score, frag, err := scorer.ScoreFragment(ctx, strings.TrimSpace(*tenantID), primaryID)
			if err != nil || frag == nil {
				if err != nil {
					log.Printf("rescore failed fragmentId=%s err=%v", primaryID, err)
				}
				continue
			}
			clone := *frag
			importance.ApplyScorecard(&clone, score, now)
			if err := fragmentsRepo.UpdateDerived(ctx, &clone); err != nil {
				log.Printf("rescore apply failed fragmentId=%s path=%s err=%v", primaryID, clone.CanonicalPath, err)
			}
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].PrimaryScore == rows[j].PrimaryScore {
			if rows[i].DuplicateScore == rows[j].DuplicateScore {
				return rows[i].DuplicatePath < rows[j].DuplicatePath
			}
			return rows[i].DuplicateScore < rows[j].DuplicateScore
		}
		return rows[i].PrimaryScore > rows[j].PrimaryScore
	})

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rows); err != nil {
		log.Fatalf("encode output: %v", err)
	}
}
