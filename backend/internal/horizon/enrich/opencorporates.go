package enrich

import (
	"context"
	"os"
	"strings"

	hmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/http"
)

// OpenCorporatesClientInterface abstracts OpenCorporates API for enrichment.
type OpenCorporatesClientInterface interface {
	SearchCompanies(ctx context.Context, query string, perPage int) (*http.OpenCorporatesSearchResponse, error)
}

// AttachOpenCorporatesIDIfMissing searches OpenCorporates for an ORGANIZATION fragment
// and attaches openCorporatesId when a confident match is found.
// Returns true if ID was attached.
func AttachOpenCorporatesIDIfMissing(
	ctx context.Context,
	client OpenCorporatesClientInterface,
	fragment *hmodels.HorizonFragment,
) (attached bool, id string) {
	if client == nil || fragment == nil {
		return false, ""
	}
	if fragment.Class != FragmentTypeOrganization {
		return false, ""
	}
	// Skip if already has OpenCorporates ID
	if existing := extractOpenCorporatesID(fragment); existing != "" {
		return false, existing
	}

	// Build search query from fragment names
	query := buildOrgSearchQuery(fragment)
	if query == "" {
		return false, ""
	}

	resp, err := client.SearchCompanies(ctx, query, 5)
	if err != nil || resp == nil || len(resp.Results.Companies) == 0 {
		return false, ""
	}

	// Take top result if name is close enough (simple heuristic: first result when ordered by score)
	top := resp.Results.Companies[0].Company
	ocID := top.CompanyID()
	if ocID == "" {
		return false, ""
	}
	// Prefer non-inactive companies
	if top.Inactive && len(resp.Results.Companies) > 1 {
		for _, c := range resp.Results.Companies[1:] {
			if !c.Company.Inactive {
				top = c.Company
				ocID = top.CompanyID()
				break
			}
		}
	}

	// Attach to fragment
	if fragment.Identifiers == nil {
		fragment.Identifiers = make(map[string]string)
	}
	fragment.Identifiers["openCorporatesId"] = ocID

	// Add KBRef
	hasRef := false
	for i := range fragment.KBRefs {
		if strings.EqualFold(strings.TrimSpace(fragment.KBRefs[i].KB), "OpenCorporates") {
			hasRef = true
			fragment.KBRefs[i].ExternalID = ocID
			break
		}
	}
	if !hasRef {
		fragment.KBRefs = append(fragment.KBRefs, hmodels.KBReference{
			KB:         "OpenCorporates",
			ExternalID: ocID,
			Confidence: 0.85,
		})
	}

	return true, ocID
}

func extractOpenCorporatesID(f *hmodels.HorizonFragment) string {
	if f.Identifiers != nil {
		if v := strings.TrimSpace(f.Identifiers["openCorporatesId"]); v != "" {
			return v
		}
	}
	for _, r := range f.KBRefs {
		if strings.EqualFold(strings.TrimSpace(r.KB), "OpenCorporates") && strings.TrimSpace(r.ExternalID) != "" {
			return strings.TrimSpace(r.ExternalID)
		}
	}
	return ""
}

func buildOrgSearchQuery(f *hmodels.HorizonFragment) string {
	// Prefer legal name, then short name, then label
	if f.Identifiers != nil {
		if v := strings.TrimSpace(f.Identifiers["legalName"]); v != "" {
			return normalizeCompanyNameForSearch(v)
		}
		if v := strings.TrimSpace(f.Identifiers["shortName"]); v != "" {
			return normalizeCompanyNameForSearch(v)
		}
	}
	if f.Labels != nil {
		if v := strings.TrimSpace(f.Labels["en"]); v != "" {
			return normalizeCompanyNameForSearch(v)
		}
	}
	// Fallback: slug from canonical path (e.g. /org/us/tsmc -> tsmc)
	if f.CanonicalPath != "" {
		parts := strings.Split(strings.Trim(f.CanonicalPath, "/"), "/")
		if len(parts) >= 3 {
			return strings.ReplaceAll(parts[len(parts)-1], "-", " ")
		}
	}
	return ""
}

func normalizeCompanyNameForSearch(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// OpenCorporatesEnabled returns true if OpenCorporates enrichment should run.
// Default ON when client is wired; set HORIZON_ENABLE_OPEN_CORPORATES=false to disable.
func OpenCorporatesEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("HORIZON_ENABLE_OPEN_CORPORATES")))
	if v == "0" || v == "false" || v == "no" || v == "n" || v == "off" {
		return false
	}
	return true
}
