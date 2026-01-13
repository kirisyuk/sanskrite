package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func scanCandidates(ctx context.Context, db *firestore.Client, tenant string, featSet, typeSet map[string]struct{}) ([]*models.HorizonFragment, cursorState, error) {
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		tenant = "global"
	}

	var st cursorState
	st.StartAfter = strings.TrimSpace(*startAfter)
	st.UpdatedAt = time.Now().UTC()

	col := db.Collection("horizon_fragments")
	last := strings.TrimSpace(*startAfter)
	pages := 0

	out := make([]*models.HorizonFragment, 0, max(10, *limit))

	for {
		if *limit > 0 && len(out) >= *limit {
			break
		}
		if *pageSize <= 0 {
			*pageSize = 500
		}

		q := col.Where("tenantId", "==", tenant).
			OrderBy(firestore.DocumentID, firestore.Asc).
			Limit(*pageSize)
		if last != "" {
			q = q.StartAfter(last)
		}

		iter := q.Documents(ctx)
		pageDocs := make([]*firestore.DocumentSnapshot, 0, *pageSize)
		for {
			doc, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return out, st, err
			}
			pageDocs = append(pageDocs, doc)
		}
		if len(pageDocs) == 0 {
			break
		}

		pages++
		last = pageDocs[len(pageDocs)-1].Ref.ID
		st.StartAfter = last

		for _, doc := range pageDocs {
			if doc == nil || !doc.Exists() {
				continue
			}
			st.Scanned++
			st.Collected = len(out)
			if *limit > 0 && len(out) >= *limit {
				break
			}

			var frag models.HorizonFragment
			if err := doc.DataTo(&frag); err != nil {
				continue
			}
			if strings.TrimSpace(frag.ID) == "" {
				frag.ID = doc.Ref.ID
			}

			// Type filter
			if len(typeSet) > 0 {
				if _, ok := typeSet[string(frag.Type)]; !ok {
					continue
				}
			}

			ok, _ := isCandidate(&frag, featSet)
			if !ok {
				continue
			}

			cp := strings.TrimSpace(frag.CanonicalPath)
			if cp == "" {
				continue
			}
			out = append(out, &frag)
		}

		if *scanProgressEveryPages > 0 && pages%*scanProgressEveryPages == 0 {
			logScanProgress(st, len(out), last, pages)
		}
		if strings.TrimSpace(*cursorFile) != "" && *saveCursorEveryPages > 0 && pages%*saveCursorEveryPages == 0 {
			st.Collected = len(out)
			_ = writeCursorFile(*cursorFile, &st)
		}

		if len(pageDocs) < *pageSize {
			break
		}
		if *maxScan > 0 && st.Scanned >= *maxScan {
			break
		}
	}

	st.Collected = len(out)
	return out, st, nil
}

func logScanProgress(st cursorState, collected int, last string, pages int) {
	_ = pages
	fmt.Printf("[horizon_llm_backfill] scan progress: scanned=%d collected=%d last=%s\n",
		st.Scanned, collected, strings.TrimSpace(last))
}

type missingInfo struct {
	// org_classifiers
	MissingOrgSize  bool
	MissingSegments bool
	// market_profile
	MissingMarketDomain   bool
	MissingMarketVertical bool
	MissingMarketChannels bool
	// market_metrics
	MissingMarketMetrics map[string]bool
}

func isCandidate(f *models.HorizonFragment, featSet map[string]struct{}) (bool, *missingInfo) {
	if f == nil {
		return false, nil
	}
	mi := &missingInfo{MissingMarketMetrics: map[string]bool{}}

	if _, ok := featSet[string(featureOrgClassifiers)]; ok && f.Type == models.FragmentTypeOrganization {
		cur := strings.ToLower(strings.TrimSpace(f.OrgSize))
		missingOrgSize := cur == ""
		if *includeNA && cur == "na" {
			missingOrgSize = true
		}
		missingSegments := len(f.MarketSegments) == 0
		mi.MissingOrgSize = missingOrgSize
		mi.MissingSegments = missingSegments

		switch strings.ToLower(strings.TrimSpace(*requireMissing)) {
		case "both":
			if missingOrgSize && missingSegments {
				return true, mi
			}
		case "orgsize":
			if missingOrgSize {
				return true, mi
			}
		case "segments":
			if missingSegments {
				return true, mi
			}
		case "either", "":
			if missingOrgSize || missingSegments {
				return true, mi
			}
		default:
			if missingOrgSize || missingSegments {
				return true, mi
			}
		}
	}

	if f.Type == models.FragmentTypeMarket {
		missingAny := false
		if _, ok := featSet[string(featureMarketProfile)]; ok {
			mi.MissingMarketDomain = strings.TrimSpace(f.MarketDomain) == ""
			mi.MissingMarketVertical = strings.TrimSpace(f.MarketVertical) == ""
			mi.MissingMarketChannels = len(f.MarketChannels) == 0
			if mi.MissingMarketDomain || mi.MissingMarketVertical || mi.MissingMarketChannels {
				missingAny = true
			}
		}

		if _, ok := featSet[string(featureMarketMetrics)]; ok {
			need := []string{
				MetricMarketTAMUSD,
				MetricMarketCAGRPercent,
				MetricMarketHHI,
				MetricMarketCR4Percent,
				// NOTE: supply_chain_risk is useful but often not reliably available; keep it OPTIONAL for backfill.
				// MetricMarketSupplyChainRisk,
			}
			for _, k := range need {
				if !hasAnyStateKey(f, k) {
					mi.MissingMarketMetrics[k] = true
				}
			}
			if len(mi.MissingMarketMetrics) > 0 {
				missingAny = true
			}
		}

		if missingAny {
			return true, mi
		}
	}

	return false, mi
}

func hasAnyStateKey(f *models.HorizonFragment, canonical string) bool {
	if f == nil || f.State == nil {
		return false
	}
	if _, ok := f.State[canonical]; ok {
		return true
	}
	// Legacy aliases (pre-canonical /metric/market keys).
	switch canonical {
	case MetricMarketTAMUSD:
		_, ok := f.State["market_tam_usd"]
		return ok
	case MetricMarketCAGRPercent:
		_, ok := f.State["market_cagr_percent"]
		return ok
	case MetricMarketHHI:
		_, ok := f.State["market_hhi"]
		return ok
	case MetricMarketCR4Percent:
		_, ok := f.State["market_cr4_percent"]
		return ok
	case MetricMarketSupplyChainRisk:
		_, ok := f.State["market_supply_chain_risk"]
		return ok
	}
	return false
}
