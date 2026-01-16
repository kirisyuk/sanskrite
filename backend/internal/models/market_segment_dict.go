package models

import "time"

// MarketSegmentDictionary is a Firestore document that defines normalization rules for /segment/... paths.
// Collection: horizon_market_segment_dict, DocID: version (e.g. "v1").
//
// It is used to normalize marketSegments into stable (normDomain, normVertical) pairs for:
// - UI rendering (icons / grouping)
// - future filtering/indexing
type MarketSegmentDictionary struct {
	ID        string    `json:"id" firestore:"id"`
	Version   string    `json:"version" firestore:"version"`
	UpdatedAt time.Time `json:"updatedAt" firestore:"updatedAt"`

	// Domain values treated as "channel" (context), e.g. b2b/b2c/public-sector.
	SegmentChannelDomains []string `json:"segmentChannelDomains,omitempty" firestore:"segmentChannelDomains,omitempty"`

	// Domain aliases: raw domain -> normalized domain.
	SegmentDomainAliases map[string]string `json:"segmentDomainAliases,omitempty" firestore:"segmentDomainAliases,omitempty"`

	// Vertical aliases: raw vertical -> normalized vertical.
	SegmentVerticalAliases map[string]string `json:"segmentVerticalAliases,omitempty" firestore:"segmentVerticalAliases,omitempty"`

	// Vertical family mapping: vertical -> normalized domain (family) when segment uses a channel-like domain.
	SegmentVerticalFamilies map[string]string `json:"segmentVerticalFamilies,omitempty" firestore:"segmentVerticalFamilies,omitempty"`

	// Explicit overrides keyed by "/segment/{domain}/{vertical}" (geo ignored).
	SegmentNormRules map[string]MarketSegmentNormRuleData `json:"segmentNormRules,omitempty" firestore:"segmentNormRules,omitempty"`

	// --- Segment-first market hierarchy support ---
	// VerticalParents defines a best-effort hierarchy between verticals (within the segment taxonomy):
	// childVertical -> parentVertical (geo ignored).
	// Example: "event-marketing" -> "marketing", "marketing" -> "advertising".
	SegmentVerticalParents map[string]string `json:"segmentVerticalParents,omitempty" firestore:"segmentVerticalParents,omitempty"`

	// VerticalSynonyms defines best-effort synonym mapping for vertical names:
	// rawVertical -> canonicalVertical (geo ignored).
	// Example: "experiential-marketing" -> "event-marketing".
	SegmentVerticalSynonyms map[string]string `json:"segmentVerticalSynonyms,omitempty" firestore:"segmentVerticalSynonyms,omitempty"`
}

// MarketSegmentNormRuleData is the Firestore representation of an explicit override.
type MarketSegmentNormRuleData struct {
	NormDomain   string `json:"normDomain" firestore:"normDomain"`
	NormVertical string `json:"normVertical,omitempty" firestore:"normVertical,omitempty"`
	Channel      string `json:"channel,omitempty" firestore:"channel,omitempty"`
}
