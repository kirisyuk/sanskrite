package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

var (
	projectID = flag.String("project", "", "GCP project ID (required)")
	tenantID  = flag.String("tenant", "", "Optional: expected tenantId (warn if different)")
	idsCSV    = flag.String("ids", "", "Comma-separated fragment IDs to fetch (required)")
)

type outRow struct {
	ID              string            `json:"id"`
	TenantID        string            `json:"tenantId,omitempty"`
	Type            string            `json:"type,omitempty"`
	CanonicalPath   string            `json:"canonicalPath,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	Identifiers     map[string]string `json:"identifiers,omitempty"`
	Governance      any               `json:"governance,omitempty"`
	EntityID        *string           `json:"entityId,omitempty"`
	Raw             map[string]any    `json:"raw,omitempty"`
	Exists          bool              `json:"exists"`
	FacilityOrg     string            `json:"facilityOrg,omitempty"`
	FacilityNormKey string            `json:"facilityNormKey,omitempty"`
}

func main() {
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		log.Fatal("--project is required")
	}
	if strings.TrimSpace(*idsCSV) == "" {
		log.Fatal("--ids is required")
	}

	ctx := context.Background()
	fsClient, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("FATAL: failed to init Firestore client: %v", err)
	}
	defer fsClient.Close()
	db := fsClient.Client()

	ids := splitCSV(*idsCSV)
	out := make([]outRow, 0, len(ids))

	for _, id := range ids {
		r := outRow{ID: id}
		snap, err := db.Collection("horizon_fragments").Doc(id).Get(ctx)
		if err != nil {
			// If it doesn't exist, Firestore still returns error; keep raw error in output.
			r.Exists = false
			r.Raw = map[string]any{"error": err.Error()}
			out = append(out, r)
			continue
		}
		if !snap.Exists() {
			r.Exists = false
			out = append(out, r)
			continue
		}
		r.Exists = true

		// Decode into struct for convenience.
		var f hmodels.HorizonFragment
		if err := snap.DataTo(&f); err == nil {
			r.TenantID = strings.TrimSpace(f.TenantID)
			r.Type = string(f.Type)
			r.CanonicalPath = strings.TrimSpace(f.CanonicalPath)
			r.Labels = f.Labels
			r.Identifiers = f.Identifiers
			r.Governance = f.Governance
			r.EntityID = f.EntityID
			if f.Type == hmodels.FragmentTypeFacility {
				org := extractFacilityOrgFromPath(f.CanonicalPath)
				r.FacilityOrg = org
				seed := strings.TrimSpace(f.Labels["en"])
				if seed == "" {
					seed = strings.TrimSpace(f.Identifiers["legalName"])
				}
				if seed == "" {
					seed = strings.TrimSpace(f.Identifiers["shortName"])
				}
				if seed == "" {
					if slug := extractFacilityNameFromPath(f.CanonicalPath); slug != "" {
						seed = strings.ReplaceAll(slug, "-", " ")
					}
				}
				r.FacilityNormKey = normalizeFacilityAliasKey(seed)
			}
		}

		// Also dump raw map for diffing.
		m := map[string]any{}
		if err := snap.DataTo(&m); err == nil {
			r.Raw = m
			// If tenant/type missing in struct decode for some reason, use raw values.
			if r.TenantID == "" {
				if v, ok := m["tenantId"].(string); ok {
					r.TenantID = v
				}
			}
			if r.Type == "" {
				if v, ok := m["type"].(string); ok {
					r.Type = v
				}
			}
			if r.CanonicalPath == "" {
				if v, ok := m["canonicalPath"].(string); ok {
					r.CanonicalPath = v
				}
			}
		}

		if strings.TrimSpace(*tenantID) != "" && strings.TrimSpace(r.TenantID) != "" && strings.TrimSpace(r.TenantID) != strings.TrimSpace(*tenantID) {
			log.Printf("WARNING: fragment %s tenantId=%q (expected %q)", id, r.TenantID, strings.TrimSpace(*tenantID))
		}

		out = append(out, r)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func splitCSV(s string) []string {
	var out []string
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// --- FACILITY normalization helpers (same logic as smart_dedup, kept local for debugging) ---

func extractFacilityOrgFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/fac/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "fac" {
		return ""
	}
	org := strings.ToLower(strings.TrimSpace(parts[1]))
	if org == "" || org == "na" {
		return ""
	}
	return org
}

func extractFacilityNameFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/fac/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 3 || parts[0] != "fac" {
		return ""
	}
	slug := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	if slug == "" || slug == "fac" || slug == "na" {
		return ""
	}
	return slug
}

func normalizeFacilityAliasKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}
	// Replace non letters/digits with spaces
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	toks := strings.Fields(b.String())
	if len(toks) == 0 {
		return ""
	}
	stop := map[string]struct{}{
		"by": {}, "the": {}, "of": {}, "and": {}, "at": {},
		"hotel": {}, "hotels": {}, "resort": {}, "inn": {}, "suites": {}, "suite": {},
	}
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		if _, ok := stop[t]; ok {
			continue
		}
		out = append(out, t)
	}
	if len(out) < 3 {
		return ""
	}
	return strings.Join(out, " ")
}


