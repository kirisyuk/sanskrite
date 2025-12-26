package scout

import (
	"context"
	"fmt"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coreontology "github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// =============================================================================
// ORGANIZATION DEDUPLICATION POLICY
// =============================================================================
// Organizations are frequently mentioned under different names (aliases, legal names,
// abbreviations). This policy defines how to identify and merge duplicates.
//
// Deduplication Priority:
// 1. Wikidata ID match - highest confidence, same real-world entity
// 2. Exact shortName match (case-insensitive) - high confidence
// 3. Normalized legalName match - medium confidence (removes JSC, LLC, Inc, etc.)

// OrgDeduplicator handles organization deduplication using Wikidata and name matching.
type OrgDeduplicator struct {
	fragmentsRepo  *repo.HorizonFragmentsRepository
	wikidataClient coreontology.WikidataClientInterface
}

// NewOrgDeduplicator creates a new organization deduplicator.
func NewOrgDeduplicator(fragmentsRepo *repo.HorizonFragmentsRepository, client coreontology.WikidataClientInterface) *OrgDeduplicator {
	return &OrgDeduplicator{
		fragmentsRepo:  fragmentsRepo,
		wikidataClient: client,
	}
}

// DeduplicateOrg attempts to find an existing organization fragment that matches
// the given fragment. If found, returns the existing fragment; otherwise returns
// the original fragment unchanged.
//
// Matching priority:
// 1. Wikidata ID from LinkedEntity.KBRefs
// 2. Wikidata ID from Identifiers["wikidataId"]
// 3. Exact shortName match
func (d *OrgDeduplicator) DeduplicateOrg(ctx context.Context, fragment *models.HorizonFragment) (*models.HorizonFragment, error) {
	if d == nil || d.fragmentsRepo == nil {
		return fragment, nil
	}
	if fragment == nil || fragment.Type != models.FragmentTypeOrganization {
		return fragment, nil
	}

	// Strategy 1: Match by Wikidata ID
	wikidataID := extractWikidataIDFromFragment(fragment)
	if wikidataID != "" {
		existing, err := d.findByWikidataID(ctx, wikidataID)
		if err == nil && existing != nil {
			return existing, nil
		}
	}

	// Strategy 2: Match by shortName
	shortName := strings.TrimSpace(fragment.Identifiers["shortName"])
	if shortName != "" {
		existing, err := d.findByShortName(ctx, shortName)
		if err == nil && existing != nil && existing.ID != fragment.ID {
			return existing, nil
		}
	}

	// Strategy 3: Try to resolve Wikidata ID via API if we have a client
	if d.wikidataClient != nil && wikidataID == "" {
		resolvedID := d.resolveWikidataID(ctx, fragment)
		if resolvedID != "" {
			existing, err := d.findByWikidataID(ctx, resolvedID)
			if err == nil && existing != nil {
				return existing, nil
			}
			// Store resolved Wikidata ID in the fragment for future use
			if fragment.Identifiers == nil {
				fragment.Identifiers = make(map[string]string)
			}
			fragment.Identifiers["wikidataId"] = resolvedID
		}
	}

	return fragment, nil
}

// findByWikidataID searches for an existing fragment with the given Wikidata ID.
func (d *OrgDeduplicator) findByWikidataID(ctx context.Context, wikidataID string) (*models.HorizonFragment, error) {
	if wikidataID == "" {
		return nil, nil
	}

	// Search by identifier field (returns slice)
	existing, err := d.fragmentsRepo.FindByIdentifier(ctx, "wikidataId", wikidataID)
	if err == nil && len(existing) > 0 {
		return existing[0], nil
	}

	return nil, nil
}

// findByShortName searches for an existing organization with the same shortName.
func (d *OrgDeduplicator) findByShortName(ctx context.Context, shortName string) (*models.HorizonFragment, error) {
	if shortName == "" {
		return nil, nil
	}

	existing, err := d.fragmentsRepo.FindByIdentifier(ctx, "shortName", shortName)
	if err == nil && len(existing) > 0 && existing[0].Type == models.FragmentTypeOrganization {
		return existing[0], nil
	}

	return nil, nil
}

// resolveWikidataID attempts to find a Wikidata ID for the organization using the API.
func (d *OrgDeduplicator) resolveWikidataID(ctx context.Context, fragment *models.HorizonFragment) string {
	if d.wikidataClient == nil {
		return ""
	}

	// Try to search by organization name
	name := strings.TrimSpace(fragment.Labels["en"])
	if name == "" {
		name = strings.TrimSpace(fragment.Identifiers["legalName"])
	}
	if name == "" {
		name = strings.TrimSpace(fragment.Identifiers["shortName"])
	}
	if name == "" {
		return ""
	}

	// Search Wikidata for organizations matching this name
	response, err := d.wikidataClient.Search(ctx, name, 3)
	if err != nil || response == nil || len(response.Results) == 0 {
		return ""
	}

	// Return the first result's ID
	return response.Results[0].ID
}

// extractWikidataIDFromFragment extracts Wikidata ID from various locations.
func extractWikidataIDFromFragment(f *models.HorizonFragment) string {
	if f == nil {
		return ""
	}

	// Check fragment's KBRefs
	for _, ref := range f.KBRefs {
		if strings.EqualFold(ref.KB, "Wikidata") || strings.EqualFold(ref.KB, "wikidata") {
			return strings.TrimSpace(ref.ExternalID)
		}
	}

	// Check Identifiers
	if wid := strings.TrimSpace(f.Identifiers["wikidataId"]); wid != "" {
		return wid
	}
	if wid := strings.TrimSpace(f.Identifiers["wikidata"]); wid != "" {
		return wid
	}

	return ""
}

// =============================================================================
// LEGAL NAME NORMALIZATION POLICY
// =============================================================================
// Legal names often contain suffixes that differ between entries but refer to
// the same organization. This policy normalizes them for matching.

// LegalSuffixesToRemove defines common legal suffixes to strip during normalization.
var LegalSuffixesToRemove = []string{
	" jsc", " llc", " inc", " ltd", " co", " corp", " corporation",
	" group", " holdings", " limited", " company", " pty ltd",
	" sa", " ag", " se", " gmbh", " ab", " oy", " as", " spa",
	" nv", " bv", " sarl", " plc", " lp", " llp",
	" s.a.", " a.g.", " s.p.a.", " n.v.", " b.v.", " s.à r.l.", " p.l.c.",
}

// NormalizeLegalName removes common legal suffixes and normalizes spacing/punctuation.
func NormalizeLegalName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}

	// Remove common legal suffixes
	for _, suffix := range LegalSuffixesToRemove {
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
			break
		}
	}

	// Normalize punctuation and spacing
	name = strings.ReplaceAll(name, ".", "")
	name = strings.ReplaceAll(name, ",", "")
	name = strings.ReplaceAll(name, "&", "and")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.Join(strings.Fields(name), " ")

	return name
}

func (d *OrgDeduplicator) String() string {
	if d == nil {
		return "<nil>"
	}
	hasClient := d.wikidataClient != nil
	return fmt.Sprintf("OrgDeduplicator(repo=%v, wikidata=%v)", d.fragmentsRepo != nil, hasClient)
}
