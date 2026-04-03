package models

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type SignalZoneEligibility string

const (
	SignalZoneEligibilityRequired  SignalZoneEligibility = "required"
	SignalZoneEligibilityAllowed   SignalZoneEligibility = "allowed"
	SignalZoneEligibilityForbidden SignalZoneEligibility = "forbidden"
)

type SignalLandingClass string

const (
	SignalLandingClassConfirmed SignalLandingClass = "confirmed"
	SignalLandingClassInferred  SignalLandingClass = "inferred"
)

type SignalLandingMethod string

const (
	SignalLandingMethodExactCanonical   SignalLandingMethod = "exact_canonical"
	SignalLandingMethodCanonicalDict    SignalLandingMethod = "canonical_dictionary"
	SignalLandingMethodVertexGrounding  SignalLandingMethod = "vertex_grounding"
	SignalLandingMethodGraphExpansion   SignalLandingMethod = "graph_expansion"
	SignalLandingMethodPathFamilyExpand SignalLandingMethod = "path_family_expansion"
	SignalLandingMethodZoneAnchor       SignalLandingMethod = "zone_anchor"
	SignalLandingMethodCountryAnchor    SignalLandingMethod = "country_anchor"
	SignalLandingMethodMaterialized     SignalLandingMethod = "materialized_chain"
)

type SignalMissingCandidateKind string

const (
	SignalMissingCandidateEntity     SignalMissingCandidateKind = "entity"
	SignalMissingCandidateFacility   SignalMissingCandidateKind = "facility"
	SignalMissingCandidateProduct    SignalMissingCandidateKind = "product"
	SignalMissingCandidateRoute      SignalMissingCandidateKind = "route"
	SignalMissingCandidateChokepoint SignalMissingCandidateKind = "chokepoint"
	SignalMissingCandidateDependency SignalMissingCandidateKind = "dependency"
)

type SignalLandingSession struct {
	ID                   string                 `json:"id" firestore:"id"`
	TenantID             string                 `json:"tenantId" firestore:"tenantId"`
	SourceEventID        string                 `json:"sourceEventId,omitempty" firestore:"sourceEventId,omitempty"`
	SourceFactID         string                 `json:"sourceFactId,omitempty" firestore:"sourceFactId,omitempty"`
	SourceDocumentRefs   []string               `json:"sourceDocumentRefs,omitempty" firestore:"sourceDocumentRefs,omitempty"`
	Status               string                 `json:"status,omitempty" firestore:"status,omitempty"`
	LandingVersion       string                 `json:"landingVersion,omitempty" firestore:"landingVersion,omitempty"`
	DecompositionVersion string                 `json:"decompositionVersion,omitempty" firestore:"decompositionVersion,omitempty"`
	GroundingVersion     string                 `json:"groundingVersion,omitempty" firestore:"groundingVersion,omitempty"`
	ExpansionVersion     string                 `json:"expansionVersion,omitempty" firestore:"expansionVersion,omitempty"`
	CoverageSummary      map[string]interface{} `json:"coverageSummary,omitempty" firestore:"coverageSummary,omitempty"`
	CreatedAt            time.Time              `json:"createdAt" firestore:"createdAt"`
	UpdatedAt            time.Time              `json:"updatedAt" firestore:"updatedAt"`
}

func (s *SignalLandingSession) Normalize() {
	if s == nil {
		return
	}
	s.TenantID = strings.TrimSpace(s.TenantID)
	s.SourceEventID = strings.TrimSpace(s.SourceEventID)
	s.SourceFactID = strings.TrimSpace(s.SourceFactID)
	s.SourceDocumentRefs = compactUniqueLandingStrings(s.SourceDocumentRefs)
	s.Status = strings.TrimSpace(s.Status)
	s.LandingVersion = strings.TrimSpace(s.LandingVersion)
	s.DecompositionVersion = strings.TrimSpace(s.DecompositionVersion)
	s.GroundingVersion = strings.TrimSpace(s.GroundingVersion)
	s.ExpansionVersion = strings.TrimSpace(s.ExpansionVersion)
	if s.Status == "" {
		s.Status = "active"
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = s.CreatedAt
	}
}

type SignalImpactClaim struct {
	ID                 string                `json:"id" firestore:"id"`
	SessionID          string                `json:"sessionId" firestore:"sessionId"`
	TenantID           string                `json:"tenantId" firestore:"tenantId"`
	SignalID           string                `json:"signalId,omitempty" firestore:"signalId,omitempty"`
	ClaimKind          string                `json:"claimKind,omitempty" firestore:"claimKind,omitempty"`
	Axis               string                `json:"axis,omitempty" firestore:"axis,omitempty"`
	Mechanism          string                `json:"mechanism,omitempty" firestore:"mechanism,omitempty"`
	Severity           float64               `json:"severity,omitempty" firestore:"severity,omitempty"`
	Confidence         float64               `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	TimeHorizon        string                `json:"timeHorizon,omitempty" firestore:"timeHorizon,omitempty"`
	SourceTextSpans    []string              `json:"sourceTextSpans,omitempty" firestore:"sourceTextSpans,omitempty"`
	SourceEvidenceRefs []string              `json:"sourceEvidenceRefs,omitempty" firestore:"sourceEvidenceRefs,omitempty"`
	ClaimLabels        []string              `json:"claimLabels,omitempty" firestore:"claimLabels,omitempty"`
	ClaimAliases       []string              `json:"claimAliases,omitempty" firestore:"claimAliases,omitempty"`
	CountryHints       []string              `json:"countryHints,omitempty" firestore:"countryHints,omitempty"`
	ProductHints       []string              `json:"productHints,omitempty" firestore:"productHints,omitempty"`
	RouteHints         []string              `json:"routeHints,omitempty" firestore:"routeHints,omitempty"`
	FacilityHints      []string              `json:"facilityHints,omitempty" firestore:"facilityHints,omitempty"`
	OrgHints           []string              `json:"orgHints,omitempty" firestore:"orgHints,omitempty"`
	ZoneEligibility    SignalZoneEligibility `json:"zoneEligibility,omitempty" firestore:"zoneEligibility,omitempty"`
	AffectedZoneRefs   []string              `json:"affectedZoneRefs,omitempty" firestore:"affectedZoneRefs,omitempty"`
	MissingPreciseZone bool                  `json:"missingPreciseZone,omitempty" firestore:"missingPreciseZone,omitempty"`
	CreatedAt          time.Time             `json:"createdAt" firestore:"createdAt"`
	UpdatedAt          time.Time             `json:"updatedAt" firestore:"updatedAt"`
}

func (c *SignalImpactClaim) Normalize() {
	if c == nil {
		return
	}
	c.SessionID = strings.TrimSpace(c.SessionID)
	c.TenantID = strings.TrimSpace(c.TenantID)
	c.SignalID = strings.TrimSpace(c.SignalID)
	c.ClaimKind = strings.TrimSpace(c.ClaimKind)
	c.Axis = strings.TrimSpace(c.Axis)
	c.Mechanism = strings.TrimSpace(c.Mechanism)
	c.TimeHorizon = strings.TrimSpace(c.TimeHorizon)
	c.SourceTextSpans = compactUniqueLandingStrings(c.SourceTextSpans)
	c.SourceEvidenceRefs = compactUniqueLandingStrings(c.SourceEvidenceRefs)
	c.ClaimLabels = compactUniqueLandingStrings(c.ClaimLabels)
	c.ClaimAliases = compactUniqueLandingStrings(c.ClaimAliases)
	c.CountryHints = compactUniqueLandingStrings(c.CountryHints)
	c.ProductHints = compactUniqueLandingStrings(c.ProductHints)
	c.RouteHints = compactUniqueLandingStrings(c.RouteHints)
	c.FacilityHints = compactUniqueLandingStrings(c.FacilityHints)
	c.OrgHints = compactUniqueLandingStrings(c.OrgHints)
	c.AffectedZoneRefs = compactUniqueLandingStrings(c.AffectedZoneRefs)
	switch c.ZoneEligibility {
	case SignalZoneEligibilityRequired, SignalZoneEligibilityAllowed, SignalZoneEligibilityForbidden:
	default:
		c.ZoneEligibility = SignalZoneEligibilityForbidden
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = c.CreatedAt
	}
}

type SignalLandingHit struct {
	ID                     string              `json:"id" firestore:"id"`
	SessionID              string              `json:"sessionId" firestore:"sessionId"`
	ClaimID                string              `json:"claimId" firestore:"claimId"`
	TenantID               string              `json:"tenantId" firestore:"tenantId"`
	CanonicalRef           string              `json:"canonicalRef" firestore:"canonicalRef"`
	RefKind                string              `json:"refKind,omitempty" firestore:"refKind,omitempty"`
	LandingClass           SignalLandingClass  `json:"landingClass" firestore:"landingClass"`
	LandingMethod          SignalLandingMethod `json:"landingMethod" firestore:"landingMethod"`
	Confidence             float64             `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	Importance             float64             `json:"importance,omitempty" firestore:"importance,omitempty"`
	Why                    string              `json:"why,omitempty" firestore:"why,omitempty"`
	SupportingEvidenceRefs []string            `json:"supportingEvidenceRefs,omitempty" firestore:"supportingEvidenceRefs,omitempty"`
	SupportingPaths        []string            `json:"supportingPaths,omitempty" firestore:"supportingPaths,omitempty"`
	CreatedAt              time.Time           `json:"createdAt" firestore:"createdAt"`
}

func (h *SignalLandingHit) Normalize() {
	if h == nil {
		return
	}
	h.SessionID = strings.TrimSpace(h.SessionID)
	h.ClaimID = strings.TrimSpace(h.ClaimID)
	h.TenantID = strings.TrimSpace(h.TenantID)
	h.CanonicalRef = strings.TrimSpace(h.CanonicalRef)
	h.RefKind = strings.TrimSpace(h.RefKind)
	h.Why = strings.TrimSpace(h.Why)
	h.SupportingEvidenceRefs = compactUniqueLandingStrings(h.SupportingEvidenceRefs)
	h.SupportingPaths = compactUniqueLandingStrings(h.SupportingPaths)
	if h.LandingClass != SignalLandingClassConfirmed && h.LandingClass != SignalLandingClassInferred {
		h.LandingClass = SignalLandingClassConfirmed
	}
	switch h.LandingMethod {
	case SignalLandingMethodExactCanonical,
		SignalLandingMethodCanonicalDict,
		SignalLandingMethodVertexGrounding,
		SignalLandingMethodGraphExpansion,
		SignalLandingMethodPathFamilyExpand,
		SignalLandingMethodZoneAnchor,
		SignalLandingMethodCountryAnchor,
		SignalLandingMethodMaterialized:
	default:
		h.LandingMethod = SignalLandingMethodExactCanonical
	}
	if h.CreatedAt.IsZero() {
		h.CreatedAt = time.Now().UTC()
	}
}

type SignalMissingAnchorCandidate struct {
	ID                     string                     `json:"id" firestore:"id"`
	SessionID              string                     `json:"sessionId" firestore:"sessionId"`
	ClaimID                string                     `json:"claimId" firestore:"claimId"`
	TenantID               string                     `json:"tenantId" firestore:"tenantId"`
	CandidateKind          SignalMissingCandidateKind `json:"candidateKind" firestore:"candidateKind"`
	Label                  string                     `json:"label" firestore:"label"`
	NormalizedLabel        string                     `json:"normalizedLabel,omitempty" firestore:"normalizedLabel,omitempty"`
	CandidateAliases       []string                   `json:"candidateAliases,omitempty" firestore:"candidateAliases,omitempty"`
	HintCountry            string                     `json:"hintCountry,omitempty" firestore:"hintCountry,omitempty"`
	HintGeo                string                     `json:"hintGeo,omitempty" firestore:"hintGeo,omitempty"`
	HintProductClass       string                     `json:"hintProductClass,omitempty" firestore:"hintProductClass,omitempty"`
	HintRouteClass         string                     `json:"hintRouteClass,omitempty" firestore:"hintRouteClass,omitempty"`
	HintIndustry           string                     `json:"hintIndustry,omitempty" firestore:"hintIndustry,omitempty"`
	SuggestedCanonicalPath string                     `json:"suggestedCanonicalPath,omitempty" firestore:"suggestedCanonicalPath,omitempty"`
	Reason                 string                     `json:"reason,omitempty" firestore:"reason,omitempty"`
	Importance             float64                    `json:"importance,omitempty" firestore:"importance,omitempty"`
	Confidence             float64                    `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	SuggestedSourceRefs    []string                   `json:"suggestedSourceRefs,omitempty" firestore:"suggestedSourceRefs,omitempty"`
	Needs                  []string                   `json:"needs,omitempty" firestore:"needs,omitempty"`
	Status                 string                     `json:"status,omitempty" firestore:"status,omitempty"`
	CreatedAt              time.Time                  `json:"createdAt" firestore:"createdAt"`
	UpdatedAt              time.Time                  `json:"updatedAt" firestore:"updatedAt"`
}

func (c *SignalMissingAnchorCandidate) Normalize() {
	if c == nil {
		return
	}
	c.SessionID = strings.TrimSpace(c.SessionID)
	c.ClaimID = strings.TrimSpace(c.ClaimID)
	c.TenantID = strings.TrimSpace(c.TenantID)
	c.Label = strings.TrimSpace(c.Label)
	c.NormalizedLabel = strings.TrimSpace(c.NormalizedLabel)
	c.CandidateAliases = compactUniqueLandingStrings(c.CandidateAliases)
	c.HintCountry = strings.TrimSpace(c.HintCountry)
	c.HintGeo = strings.TrimSpace(c.HintGeo)
	c.HintProductClass = strings.TrimSpace(c.HintProductClass)
	c.HintRouteClass = strings.TrimSpace(c.HintRouteClass)
	c.HintIndustry = strings.TrimSpace(c.HintIndustry)
	c.SuggestedCanonicalPath = strings.TrimSpace(c.SuggestedCanonicalPath)
	c.Reason = strings.TrimSpace(c.Reason)
	c.SuggestedSourceRefs = compactUniqueLandingStrings(c.SuggestedSourceRefs)
	c.Needs = compactUniqueLandingStrings(c.Needs)
	c.Status = strings.TrimSpace(c.Status)
	if c.Status == "" {
		c.Status = "open"
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = c.CreatedAt
	}
}

type SignalLandingSummary struct {
	ID                   string    `json:"id" firestore:"id"`
	TenantID             string    `json:"tenantId" firestore:"tenantId"`
	ScopeRef             string    `json:"scopeRef" firestore:"scopeRef"`
	SessionIDs           []string  `json:"sessionIds,omitempty" firestore:"sessionIds,omitempty"`
	TopConfirmedRefs     []string  `json:"topConfirmedRefs,omitempty" firestore:"topConfirmedRefs,omitempty"`
	TopInferredRefs      []string  `json:"topInferredRefs,omitempty" firestore:"topInferredRefs,omitempty"`
	TopMissingCandidates []string  `json:"topMissingCandidates,omitempty" firestore:"topMissingCandidates,omitempty"`
	TopClaims            []string  `json:"topClaims,omitempty" firestore:"topClaims,omitempty"`
	Products             []string  `json:"products,omitempty" firestore:"products,omitempty"`
	Routes               []string  `json:"routes,omitempty" firestore:"routes,omitempty"`
	Facilities           []string  `json:"facilities,omitempty" firestore:"facilities,omitempty"`
	Chokepoints          []string  `json:"chokepoints,omitempty" firestore:"chokepoints,omitempty"`
	AffectedZoneRefs     []string  `json:"affectedZoneRefs,omitempty" firestore:"affectedZoneRefs,omitempty"`
	MissingPreciseZone   bool      `json:"missingPreciseZone,omitempty" firestore:"missingPreciseZone,omitempty"`
	UpdatedAt            time.Time `json:"updatedAt" firestore:"updatedAt"`
}

func (s *SignalLandingSummary) Normalize() {
	if s == nil {
		return
	}
	s.TenantID = strings.TrimSpace(s.TenantID)
	s.ScopeRef = strings.TrimSpace(s.ScopeRef)
	s.SessionIDs = compactUniqueLandingStrings(s.SessionIDs)
	s.TopConfirmedRefs = compactUniqueLandingStrings(s.TopConfirmedRefs)
	s.TopInferredRefs = compactUniqueLandingStrings(s.TopInferredRefs)
	s.TopMissingCandidates = compactUniqueLandingStrings(s.TopMissingCandidates)
	s.TopClaims = compactUniqueLandingStrings(s.TopClaims)
	s.Products = compactUniqueLandingStrings(s.Products)
	s.Routes = compactUniqueLandingStrings(s.Routes)
	s.Facilities = compactUniqueLandingStrings(s.Facilities)
	s.Chokepoints = compactUniqueLandingStrings(s.Chokepoints)
	s.AffectedZoneRefs = compactUniqueLandingStrings(s.AffectedZoneRefs)
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = time.Now().UTC()
	}
}

func (s *SignalLandingSummary) Validate() error {
	if s == nil {
		return fmt.Errorf("signal landing summary is required")
	}
	s.Normalize()
	if s.TenantID == "" {
		return fmt.Errorf("tenantId is required")
	}
	if s.ScopeRef == "" {
		return fmt.Errorf("scopeRef is required")
	}
	return nil
}

func compactUniqueLandingStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
