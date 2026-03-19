package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// Tool: horizon_backfill_facility_kind
//
// Backfills identifiers.facilityKind for FACILITY fragments in Firestore.
// Strategy:
// 1) Keep existing concrete values by default.
// 2) Infer from canonicalPath + contextual signals (name/description/entity/industry/facilityType).
// 3) Optionally set "other" when no concrete subtype can be inferred (to maximize completeness).

func envProjectID() string {
	for _, k := range []string{"GCP_PROJECT_ID", "GOOGLE_CLOUD_PROJECT", "GCLOUD_PROJECT"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func gcloudProjectID() string {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gcloud", "config", "get-value", "project")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if v == "" || strings.Contains(strings.ToLower(v), "unset") {
		return ""
	}
	return v
}

func asString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func asMap(v interface{}) map[string]interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return t
	case map[string]string:
		out := make(map[string]interface{}, len(t))
		for k, vv := range t {
			out[k] = vv
		}
		return out
	default:
		return nil
	}
}

func firstMapString(m map[string]interface{}, keys ...string) string {
	if len(m) == 0 {
		return ""
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s := asString(v)
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func asStringSlice(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := asString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func isConcrete(raw string, norm taxonomy.FacilityKind) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	return norm != taxonomy.FacilityKindUnknown && norm != taxonomy.FacilityKindOther
}

func main() {
	var (
		projectID         = flag.String("project", "", "GCP project ID (fallback: env, then gcloud config)")
		tenantID          = flag.String("tenant", "global", "tenantId filter")
		pageSize          = flag.Int("page-size", 500, "Firestore page size (max 1000)")
		limit             = flag.Int("limit", 0, "Max docs to scan (0 = all)")
		startAfter        = flag.String("start-after", "", "Resume scan after document ID (__name__)")
		apply             = flag.Bool("apply", false, "Apply writes (default: dry-run)")
		fillOther         = flag.Bool("fill-other", true, "Set facilityKind=other when concrete type cannot be inferred")
		overwriteConcrete = flag.Bool("overwrite-concrete", false, "Recompute even if current facilityKind is already concrete")
	)
	flag.Parse()

	pid := strings.TrimSpace(*projectID)
	if pid == "" {
		pid = envProjectID()
	}
	if pid == "" {
		pid = gcloudProjectID()
	}
	if pid == "" {
		log.Fatal("project id not provided; set --project or env GOOGLE_CLOUD_PROJECT (or configure gcloud project)")
	}
	tid := strings.TrimSpace(*tenantID)
	if tid == "" {
		tid = "global"
	}

	ps := *pageSize
	if ps <= 0 {
		ps = 500
	}
	if ps > 1000 {
		ps = 1000
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()

	client, err := firestore.NewClient(ctx, pid)
	if err != nil {
		log.Fatalf("firestore.NewClient failed: %v", err)
	}
	defer client.Close()

	col := client.Collection("horizon_fragments")
	base := col.Where("tenantId", "==", tid).OrderBy(firestore.DocumentID, firestore.Asc).Limit(ps)

	var last *firestore.DocumentSnapshot
	if sa := strings.TrimSpace(*startAfter); sa != "" {
		snap, err := col.Doc(sa).Get(ctx)
		if err != nil {
			log.Fatalf("failed to resolve --start-after doc %q: %v", sa, err)
		}
		last = snap
	}

	var (
		scanned       int
		facilities    int
		currentFilled int
		currentConcr  int
		proposedFill  int
		proposedConcr int
		candidates    int
		applied       int
		writeErrors   int
	)

	stop := false
	for {
		q := base
		if last != nil {
			q = q.StartAfter(last)
		}
		q = q.Select("type", "canonicalPath", "labels", "description", "entityPath", "industryPaths", "identifiers")
		docs, err := q.Documents(ctx).GetAll()
		if err != nil {
			log.Fatalf("firestore query failed after scanned=%d: %v", scanned, err)
		}
		if len(docs) == 0 {
			break
		}
		last = docs[len(docs)-1]

		for _, d := range docs {
			if *limit > 0 && scanned >= *limit {
				stop = true
				break
			}
			scanned++

			data := d.Data()
			cp := asString(data["canonicalPath"])
			typ := strings.ToUpper(asString(data["type"]))
			if typ != "FACILITY" && !strings.HasPrefix(strings.ToLower(cp), "/fac/") {
				continue
			}
			facilities++

			ident := asMap(data["identifiers"])
			if ident == nil {
				ident = map[string]interface{}{}
			}
			raw := firstMapString(ident, "facilityKind", "facility_kind")
			norm := taxonomy.NormalizeFacilityKind(raw)
			if strings.TrimSpace(raw) != "" {
				currentFilled++
			}
			if isConcrete(raw, norm) {
				currentConcr++
			}

			if isConcrete(raw, norm) && !*overwriteConcrete {
				proposedFill++
				proposedConcr++
				continue
			}

			lbls := asMap(data["labels"])
			labelEN := firstMapString(lbls, "en")
			desc := asString(data["description"])
			entityPath := asString(data["entityPath"])
			industryPaths := asStringSlice(data["industryPaths"])
			facilityType := firstMapString(ident, "facilityType")
			supplyRole := firstMapString(ident, "supplyChainRole")

			inferred := taxonomy.InferFacilityKind(
				cp,
				labelEN,
				desc,
				entityPath,
				strings.Join(industryPaths, " "),
				facilityType,
				supplyRole,
			)
			if inferred == taxonomy.FacilityKindUnknown {
				inferred = taxonomy.FacilityKindOther
			}
			if inferred == taxonomy.FacilityKindOther && !*fillOther {
				// keep original value as-is when other is disabled
				if strings.TrimSpace(raw) != "" {
					proposedFill++
				}
				if isConcrete(raw, norm) {
					proposedConcr++
				}
				continue
			}

			target := strings.TrimSpace(string(inferred))
			if target == "" {
				if strings.TrimSpace(raw) != "" {
					proposedFill++
				}
				if isConcrete(raw, norm) {
					proposedConcr++
				}
				continue
			}

			if strings.EqualFold(strings.TrimSpace(raw), target) {
				proposedFill++
				if inferred != taxonomy.FacilityKindOther {
					proposedConcr++
				}
				continue
			}

			candidates++
			proposedFill++
			if inferred != taxonomy.FacilityKindOther {
				proposedConcr++
			}

			if *apply {
				ident["facilityKind"] = target
				_, err := d.Ref.Set(ctx, map[string]interface{}{
					"identifiers": ident,
				}, firestore.MergeAll)
				if err != nil {
					writeErrors++
					log.Printf("WRITE ERROR: doc=%s canonicalPath=%s: %v", d.Ref.ID, cp, err)
				} else {
					applied++
				}
			}
		}

		if stop {
			break
		}
	}

	fmt.Printf("=== FacilityKind Backfill ===\n")
	fmt.Printf("project=%s tenant=%s apply=%v fill_other=%v overwrite_concrete=%v\n", pid, tid, *apply, *fillOther, *overwriteConcrete)
	fmt.Printf("scanned=%d facilities=%d candidates=%d applied=%d write_errors=%d\n", scanned, facilities, candidates, applied, writeErrors)
	fmt.Println()
	fmt.Printf("current_filled=%d (%.2f%%)\n", currentFilled, pct(currentFilled, facilities))
	fmt.Printf("current_concrete=%d (%.2f%%)\n", currentConcr, pct(currentConcr, facilities))
	fmt.Printf("projected_filled=%d (%.2f%%)\n", proposedFill, pct(proposedFill, facilities))
	fmt.Printf("projected_concrete=%d (%.2f%%)\n", proposedConcr, pct(proposedConcr, facilities))
	if !*apply {
		fmt.Println("dry-run only: rerun with --apply to persist changes")
	}
}

func pct(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}
