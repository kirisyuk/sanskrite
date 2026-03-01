package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"cloud.google.com/go/firestore"

	hrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

var (
	projectID = flag.String("project", "etigram", "GCP project ID")
	inPath    = flag.String("in", "audit_zones_full.jsonl", "Input JSONL from horizon_audit_zone_radii")
	apply     = flag.Bool("apply", false, "Apply changes to Firestore (default: dry-run)")
	dryRun    = flag.Bool("dry-run", true, "Dry-run only, do not write (default: true)")
)

type auditRow struct {
	CanonicalPath string  `json:"canonicalPath"`
	RadiusKm      float64 `json:"radiusKm"`
	Verdict       string  `json:"verdict"`
	SuggestedKm   float64 `json:"suggestedKm"`
	Label         string  `json:"label"`
}

func main() {
	flag.Parse()
	ctx := context.Background()

	doApply := *apply && !*dryRun
	if doApply {
		log.Printf("APPLY mode: will update fragments in Firestore")
	} else {
		log.Printf("DRY-RUN mode: no changes will be written")
	}

	fs, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("Firestore: %v", err)
	}
	defer fs.Close()

	repo := hrepo.NewHorizonFragmentsRepository(fs)

	f, err := os.Open(strings.TrimSpace(*inPath))
	if err != nil {
		log.Fatalf("open %s: %v", *inPath, err)
	}
	defer f.Close()

	var toApply []auditRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var row auditRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			log.Printf("skip invalid JSON: %v", err)
			continue
		}
		if (row.Verdict == "overly_broad" || row.Verdict == "suggest_radius") && row.SuggestedKm > 0 {
			toApply = append(toApply, row)
		}
	}
	if err := sc.Err(); err != nil {
		log.Fatalf("scan: %v", err)
	}

	log.Printf("Found %d zones to update (overly_broad/suggest_radius with suggestedKm > 0)", len(toApply))
	if len(toApply) == 0 {
		return
	}

	updated := 0
	errors := 0
	for _, row := range toApply {
		cp := strings.TrimSpace(row.CanonicalPath)
		if cp == "" {
			continue
		}
		frag, err := repo.GetByCanonicalPath(ctx, cp)
		if err != nil {
			log.Printf("[skip] %s: %v", cp, err)
			errors++
			continue
		}
		if frag == nil {
			log.Printf("[skip] %s: fragment not found", cp)
			errors++
			continue
		}
		prev := frag.RadiusKm
		if prev == row.SuggestedKm {
			log.Printf("[skip] %s: already has radiusKm=%.0f", cp, prev)
			continue
		}
		log.Printf("[%s] %s: %.0f km -> %.0f km (verdict=%s)", frag.ID, cp, prev, row.SuggestedKm, row.Verdict)
		if !doApply {
			updated++
			continue
		}
		frag.RadiusKm = row.SuggestedKm
		if err := repo.Update(ctx, frag); err != nil {
			log.Printf("[error] %s: update failed: %v", cp, err)
			errors++
			continue
		}
		updated++
	}
	log.Printf("Done. Updated %d zones (errors=%d)", updated, errors)
}
