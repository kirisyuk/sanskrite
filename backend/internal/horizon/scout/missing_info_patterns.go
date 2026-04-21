package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// =============================================================================
// MISSING INFO PATTERNS
// =============================================================================
// This file defines structured patterns for generating and resolving missing info
// questions during the scout pipeline. These patterns help critics and research
// agents produce more targeted questions and avoid creating duplicate entities.

// MissingInfoPattern defines a structured pattern for a type of missing information.
type MissingInfoPattern struct {
	// Key is a unique identifier for the pattern (used in MissingInfoStatus).
	Key string
	// Question is the template for the missing info question.
	// Supports placeholders: {entity}, {platform}, {geo}, {market}, {facet}
	Question string
	// Context provides additional guidance for resolution.
	Context map[string]string
	// AppliesTo defines which fragment types this pattern applies to.
	AppliesTo []FragmentType
	// TriggerCondition describes when this pattern should be applied.
	TriggerCondition string
	// ExpectedResolution describes what kind of resolution is expected.
	ExpectedResolution []string
	// Priority determines the order in which patterns are evaluated (lower = higher priority).
	Priority int
}

// =============================================================================
// PLATFORM → CHANNEL RESOLUTION PATTERNS
// =============================================================================
// When a dependency on a platform (Telegram, WhatsApp, Signal) is detected,
// these patterns guide resolution to specific channels.

// PlatformChannelResolutionPattern generates missing info for platform → channel resolution.
var PlatformChannelResolutionPattern = MissingInfoPattern{
	Key:      "platform_channel_resolution",
	Question: "Which specific channels/groups on {platform} does {entity} use for communication?",
	Context: map[string]string{
		"guidance":    "Look for specific Telegram channels, WhatsApp groups, Signal groups that the entity uses",
		"output_type": models.FormatClassSubtype(ah.FragmentClassOrg, "media_channel"),
		"output_path": "/media/{platform}/{channel-slug}",
		"edge_type":   "operates_via",
		"replaces":    "generic depends_on → platform organization",
		"facet":       "communication",
	},
	AppliesTo:        []FragmentType{ah.FragmentClassCommunity, ah.FragmentClassOrg, ah.FragmentClassMarket},
	TriggerCondition: "Entity has depends_on edge to a messaging platform organization (Telegram, WhatsApp, Signal, etc.) without specific channel resolution",
	ExpectedResolution: []string{
		"Create /media/{platform}/{channel-slug} fragments",
		"Update edge to operates_via pointing to specific channels",
		"Remove generic platform dependency",
	},
	Priority: 10,
}

// =============================================================================
// MARKET → COMMUNITY DECOMPOSITION PATTERNS
// =============================================================================
// When a market (especially illicit) has no operator information.

// MarketOperatorCommunitiesPattern generates missing info for market operator discovery.
var MarketOperatorCommunitiesPattern = MissingInfoPattern{
	Key:      "market_operator_communities",
	Question: "Which communities or informal networks operate within {market}?",
	Context: map[string]string{
		"guidance":    "Look for informal operator networks, dealer networks, distribution communities",
		"output_type": "COMMUNITY",
		"output_path": "/community/{geo}/{type}/{slug}",
		"edge_type":   "participates_in",
	},
	AppliesTo:        []FragmentType{ah.FragmentClassMarket},
	TriggerCondition: "Market fragment has no participates_in edges from COMMUNITY fragments",
	ExpectedResolution: []string{
		"Create /community/{geo}/{type}/{slug} fragments",
		"Create participates_in edges from community to market",
	},
	Priority: 20,
}

// =============================================================================
// ORGANIZATION vs COMMUNITY DISAMBIGUATION PATTERNS
// =============================================================================
// When an entity might be misclassified as ORGANIZATION when it should be COMMUNITY.

// OrgCommunityDisambiguationPattern generates missing info for org/community disambiguation.
var OrgCommunityDisambiguationPattern = MissingInfoPattern{
	Key:      "org_community_disambiguation",
	Question: "Is {entity} a formally registered organization or an informal community/network?",
	Context: map[string]string{
		"guidance":      "Check for legal registration, formal structure, official status",
		"signals_org":   "Has legal name suffix (Inc., Ltd., LLC), registered address, official website with legal info",
		"signals_comm":  "No legal registration, informal network, operates through channels/platforms, no formal leadership structure",
		"action_if_org": "Keep as ORGANIZATION",
		"action_if_com": "Retype to COMMUNITY, update canonical path to /community/{geo}/{type}/{slug}",
	},
	AppliesTo:        []FragmentType{ah.FragmentClassOrg},
	TriggerCondition: "ORGANIZATION fragment has signals suggesting it might be an informal community (no legal name, no registration, network-like structure)",
	ExpectedResolution: []string{
		"Confirm organization status OR",
		"Retype to COMMUNITY and update canonical path",
	},
	Priority: 15,
}

// =============================================================================
// Org/media_channel subtype → COMMUNITY EXTRACTION PATTERNS
// =============================================================================
// When a media channel has an identifiable operator community.

// ChannelCommunityExtractionPattern generates missing info for channel → community extraction.
var ChannelCommunityExtractionPattern = MissingInfoPattern{
	Key:      "channel_community_extraction",
	Question: "Is there an identifiable community behind {entity}?",
	Context: map[string]string{
		"guidance":    "Look for evidence of an organized group operating the channel",
		"output_type": "COMMUNITY",
		"output_path": "/community/{geo}/{type}/{slug}",
		"edge_type":   "operates_via",
		"edge_dir":    "COMMUNITY → org/media_channel",
	},
	// MIGRATION NOTE: legacy ah.FragmentClassOrg collapses onto
	// (org, media_channel). The pattern now applies to any /media/-pathed
	// org fragment; precise (Class, Subtype) routing happens in the caller.
	AppliesTo: []FragmentType{ah.FragmentClassOrg},
	TriggerCondition: "Media channel fragments have no incoming operates_via edges and show signs of organized operation",
	ExpectedResolution: []string{
		"Create /community/{geo}/{type}/{slug} fragment",
		"Create operates_via edge from community to channel",
	},
	Priority: 25,
}

// =============================================================================
// PAYMENT MECHANISM RESOLUTION PATTERNS
// =============================================================================
// When payment dependencies are too generic.

// PaymentMechanismResolutionPattern generates missing info for payment mechanism resolution.
var PaymentMechanismResolutionPattern = MissingInfoPattern{
	Key:      "payment_mechanism_resolution",
	Question: "Which specific payment mechanisms/operators does {entity} use?",
	Context: map[string]string{
		"guidance":     "Look for specific crypto currencies, exchanges, hawala operators, payment processors",
		"facet":        "payment",
		"output_types": "PRODUCT (crypto/payment service), ORGANIZATION (exchange/processor), COMMUNITY (hawala network)",
	},
	AppliesTo:        []FragmentType{ah.FragmentClassCommunity, ah.FragmentClassOrg, ah.FragmentClassMarket},
	TriggerCondition: "Entity has generic depends_on edge with facet=payment but no specific mechanism",
	ExpectedResolution: []string{
		"Create specific payment mechanism dependencies",
		"Add depends_on(facet=payment) edges to specific mechanisms",
	},
	Priority: 30,
}

// =============================================================================
// DISTRIBUTION NETWORK RESOLUTION PATTERNS
// =============================================================================
// When distribution network is not decomposed.

// DistributionNetworkResolutionPattern generates missing info for distribution network resolution.
var DistributionNetworkResolutionPattern = MissingInfoPattern{
	Key:      "distribution_network_resolution",
	Question: "Who operates the distribution network for {product} in {geo}?",
	Context: map[string]string{
		"guidance":    "Look for distribution communities, logistics operators, courier networks",
		"facet":       "distribution",
		"output_type": "COMMUNITY or ORGANIZATION",
	},
	AppliesTo:        []FragmentType{ah.FragmentClassMarket, ah.FragmentClassCommunity},
	TriggerCondition: "Entity has supply chain dependencies but no distribution network information",
	ExpectedResolution: []string{
		"Create distribution operator fragments (COMMUNITY or ORGANIZATION)",
		"Create depends_on(facet=distribution) edges",
	},
	Priority: 35,
}

// =============================================================================
// COMMUNITY ENRICHMENT PATTERNS
// =============================================================================
// When COMMUNITY fragments are missing key information.

// CommunityPlatformsPattern generates missing info for community platform discovery.
var CommunityPlatformsPattern = MissingInfoPattern{
	Key:      "community_platforms",
	Question: "Which communication platforms/channels does {entity} use?",
	Context: map[string]string{
		"guidance":    "Look for Telegram channels, WhatsApp groups, forums, websites used by the community",
		"output_type": models.FormatClassSubtype(ah.FragmentClassOrg, "media_channel"),
		"edge_type":   "operates_via",
	},
	AppliesTo:        []FragmentType{ah.FragmentClassCommunity},
	TriggerCondition: "COMMUNITY fragment has empty communityPlatforms and no operates_via edges",
	ExpectedResolution: []string{
		"Populate communityPlatforms field",
		"Create operates_via edges to org/media_channel fragments",
	},
	Priority: 40,
}

// CommunityAffiliationPattern generates missing info for community affiliation discovery.
var CommunityAffiliationPattern = MissingInfoPattern{
	Key:      "community_affiliation",
	Question: "Is {entity} affiliated with any formal organizations?",
	Context: map[string]string{
		"guidance":    "Look for connections to registered companies, NGOs, government bodies",
		"output_type": "ORGANIZATION",
		"edge_type":   "affiliated_with",
	},
	AppliesTo:        []FragmentType{ah.FragmentClassCommunity},
	TriggerCondition: "COMMUNITY fragment has empty affiliatedOrgPath and no affiliated_with edges",
	ExpectedResolution: []string{
		"Populate affiliatedOrgPath field if applicable",
		"Create affiliated_with edges to ORGANIZATION fragments",
	},
	Priority: 45,
}

// =============================================================================
// COMMUNITY HIERARCHY PATTERNS (DYNASTY / TRIBE)
// =============================================================================
// When COMMUNITY fragments of type dynasty or tribe have no hierarchy.

// DynastyBranchesPattern generates missing info for dynasty branch discovery.
var DynastyBranchesPattern = MissingInfoPattern{
	Key:      "dynasty_branches",
	Question: "What are the main branches of {entity}?",
	Context: map[string]string{
		"guidance":    "Look for family branches, cadet branches, sub-dynasties (e.g., Sudairi Seven for House of Saud)",
		"output_type": "COMMUNITY",
		"output_path": "/community/{geo}/dynasty/{branch-slug}",
		"edge_type":   "has_subgroup",
		"edge_dir":    "parent dynasty → branch",
		"examples":    "House of Saud → Sudairi Seven, Kennedy family → JFK branch",
	},
	AppliesTo:        []FragmentType{ah.FragmentClassCommunity},
	TriggerCondition: "COMMUNITY fragment has communityType='dynasty' and no has_subgroup edges",
	ExpectedResolution: []string{
		"Create /community/{geo}/dynasty/{branch-slug} fragments for branches",
		"Create has_subgroup edges from parent dynasty to branches",
	},
	Priority: 42,
}

// TribeSubclansPattern generates missing info for tribe sub-clan discovery.
var TribeSubclansPattern = MissingInfoPattern{
	Key:      "tribe_subclans",
	Question: "What are the major sub-tribes or clans of {entity}?",
	Context: map[string]string{
		"guidance":    "Look for sub-tribes, clans, lineages (e.g., Durrani/Ghilzai for Pashtun, Uysun for Senior Zhuz)",
		"output_type": "COMMUNITY",
		"output_path": "/community/{geo}/tribe/{subclan-slug}",
		"edge_type":   "has_subgroup",
		"edge_dir":    "parent tribe → sub-tribe/clan",
		"examples":    "Pashtun → Durrani → Popalzai, Senior Zhuz → Uysun",
	},
	AppliesTo:        []FragmentType{ah.FragmentClassCommunity},
	TriggerCondition: "COMMUNITY fragment has communityType='tribe' and no has_subgroup edges",
	ExpectedResolution: []string{
		"Create /community/{geo}/tribe/{subclan-slug} fragments for sub-tribes",
		"Create has_subgroup edges from parent tribe to sub-tribes",
	},
	Priority: 43,
}

// CommunityHierarchyParentPattern generates missing info for community parent discovery.
var CommunityHierarchyParentPattern = MissingInfoPattern{
	Key:      "community_hierarchy_parent",
	Question: "Is {entity} part of a larger dynasty/tribe/group?",
	Context: map[string]string{
		"guidance":    "Look for parent dynasties, parent tribes, broader community that contains this one",
		"output_type": "COMMUNITY",
		"edge_type":   "has_subgroup",
		"edge_dir":    "parent community → this community",
	},
	AppliesTo:        []FragmentType{ah.FragmentClassCommunity},
	TriggerCondition: "COMMUNITY fragment (dynasty/tribe) has has_subgroup children but no incoming has_subgroup from a parent",
	ExpectedResolution: []string{
		"Identify parent community if exists",
		"Create has_subgroup edge from parent to this community",
	},
	Priority: 44,
}

// =============================================================================
// DEPENDENCY FACET RESOLUTION PATTERNS
// =============================================================================
// When depends_on edges lack facet qualifiers.

// DependencyFacetResolutionPattern generates missing info for facet classification.
var DependencyFacetResolutionPattern = MissingInfoPattern{
	Key:      "dependency_facet_resolution",
	Question: "What is the nature of the dependency between {source} and {target}?",
	Context: map[string]string{
		"guidance":     "Classify the dependency facet: communication, infrastructure, payment, distribution, logistics, supply, labor, legitimization, enforcement, regulatory",
		"valid_facets": "communication, infrastructure, payment, distribution, logistics, supply, labor, legitimization, enforcement, regulatory",
	},
	AppliesTo:        []FragmentType{ah.FragmentClassCommunity, ah.FragmentClassOrg, ah.FragmentClassMarket},
	TriggerCondition: "Entity has depends_on edge without facet qualifier",
	ExpectedResolution: []string{
		"Add facet qualifier to depends_on edge",
		"Optionally add substitutability score",
	},
	Priority: 50,
}

// =============================================================================
// PATTERN REGISTRY
// =============================================================================

// AllMissingInfoPatterns returns all registered missing info patterns.
func AllMissingInfoPatterns() []MissingInfoPattern {
	return []MissingInfoPattern{
		PlatformChannelResolutionPattern,
		MarketOperatorCommunitiesPattern,
		OrgCommunityDisambiguationPattern,
		ChannelCommunityExtractionPattern,
		PaymentMechanismResolutionPattern,
		DistributionNetworkResolutionPattern,
		CommunityPlatformsPattern,
		CommunityAffiliationPattern,
		DynastyBranchesPattern,
		TribeSubclansPattern,
		CommunityHierarchyParentPattern,
		DependencyFacetResolutionPattern,
	}
}

// GetPatternByKey returns a pattern by its key.
func GetPatternByKey(key string) *MissingInfoPattern {
	for _, p := range AllMissingInfoPatterns() {
		if p.Key == key {
			return &p
		}
	}
	return nil
}

// GetPatternsForFragmentType returns all patterns applicable to a fragment type.
func GetPatternsForFragmentType(ft FragmentType) []MissingInfoPattern {
	var result []MissingInfoPattern
	for _, p := range AllMissingInfoPatterns() {
		for _, t := range p.AppliesTo {
			if t == ft {
				result = append(result, p)
				break
			}
		}
	}
	return result
}

// =============================================================================
// PATTERN MATCHING HELPERS
// =============================================================================

// KnownMessagingPlatforms is a best-effort list of known messaging platform names.
// Used for heuristic detection — not exhaustive, just common platforms.
var KnownMessagingPlatforms = []string{
	"telegram", "whatsapp", "signal", "wechat", "weixin",
	"line", "kakao", "kakaotalk", "viber", "messenger",
	"discord", "slack", "matrix", "element", "threema",
	"wickr", "session", "briar", "keybase",
}

// IsPlatformOrganization checks if a canonical path or name refers to a messaging platform.
// This is a best-effort heuristic based on platform names.
func IsPlatformOrganization(canonicalPathOrName string) bool {
	s := strings.ToLower(strings.TrimSpace(canonicalPathOrName))
	if s == "" {
		return false
	}
	for _, platform := range KnownMessagingPlatforms {
		if strings.Contains(s, platform) {
			return true
		}
	}
	return false
}

// IsPlatformName checks if a name matches a known messaging platform.
func IsPlatformName(name string) bool {
	s := strings.ToLower(strings.TrimSpace(name))
	for _, platform := range KnownMessagingPlatforms {
		if s == platform || strings.HasPrefix(s, platform+" ") || strings.HasSuffix(s, " "+platform) {
			return true
		}
	}
	return false
}

// ExtractPlatformFromPath extracts the platform name from a media channel path.
// Returns empty string if not a media channel path.
func ExtractPlatformFromPath(canonicalPath string) string {
	cp := strings.TrimSpace(canonicalPath)
	if !strings.HasPrefix(cp, "/media/") {
		return ""
	}
	parts := strings.Split(strings.Trim(cp, "/"), "/")
	if len(parts) >= 2 {
		return parts[1] // /media/{platform}/{slug}
	}
	return ""
}

// AllowedCommunityTypes is the canonical list of community type slugs for /community/{geo}/{type}/{slug}.
var AllowedCommunityTypes = []string{
	"trade", "activist", "professional", "criminal", "religious", "ethnic", "interest",
	"dynasty", "tribe", "representatives", "industry-association", "key-community",
}

// SuggestCommunityType suggests a community type based on context.
func SuggestCommunityType(marketPath, description string) string {
	desc := strings.ToLower(description)
	mp := strings.ToLower(marketPath)

	// Criminal indicators
	if strings.Contains(desc, "drug") || strings.Contains(desc, "narcotic") ||
		strings.Contains(desc, "illicit") || strings.Contains(desc, "illegal") ||
		strings.Contains(desc, "trafficking") || strings.Contains(desc, "smuggling") ||
		strings.Contains(mp, "drug") || strings.Contains(mp, "illicit") {
		return "criminal"
	}

	// Trade indicators
	if strings.Contains(desc, "hawala") || strings.Contains(desc, "trade") ||
		strings.Contains(desc, "exchange") || strings.Contains(desc, "dealer") ||
		strings.Contains(mp, "hawala") || strings.Contains(mp, "exchange") {
		return "trade"
	}

	// Activist indicators
	if strings.Contains(desc, "activist") || strings.Contains(desc, "protest") ||
		strings.Contains(desc, "political") || strings.Contains(desc, "opposition") {
		return "activist"
	}

	// Professional indicators
	if strings.Contains(desc, "professional") || strings.Contains(desc, "researcher") ||
		strings.Contains(desc, "developer") || strings.Contains(desc, "osint") {
		return "professional"
	}

	// Industry association indicators
	if strings.Contains(desc, "association") || strings.Contains(desc, "trade body") ||
		strings.Contains(desc, "industry group") || strings.Contains(desc, "lobby") {
		return "industry-association"
	}

	// Representatives indicators
	if strings.Contains(desc, "representative") || strings.Contains(desc, "delegation") ||
		strings.Contains(desc, "delegate") {
		return "representatives"
	}

	// Default
	return "interest"
}
