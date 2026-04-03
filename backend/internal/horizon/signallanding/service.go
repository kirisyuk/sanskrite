package signallanding

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonproductdict "github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

var nonAlphaNumeric = regexp.MustCompile(`[^a-z0-9]+`)

type Service struct {
	sessionsRepo     *horizonrepo.SignalLandingSessionsRepository
	claimsRepo       *horizonrepo.SignalImpactClaimsRepository
	hitsRepo         *horizonrepo.SignalLandingHitsRepository
	summariesRepo    *horizonrepo.SignalLandingSummariesRepository
	candidatesRepo   *horizonrepo.SignalMissingAnchorCandidatesRepository
	fragmentsRepo    *horizonrepo.HorizonFragmentsRepository
	materializedRepo *horizonrepo.MaterializedArtifactsRepository
	productDict      *horizonproductdict.Runtime
	vertexSearch     *ai.VertexAISearchClient
}

type Result struct {
	Session    *horizonmodels.SignalLandingSession
	Claims     []*horizonmodels.SignalImpactClaim
	Hits       []*horizonmodels.SignalLandingHit
	Candidates []*horizonmodels.SignalMissingAnchorCandidate
	Summaries  []*horizonmodels.SignalLandingSummary
}

func NewService(
	sessionsRepo *horizonrepo.SignalLandingSessionsRepository,
	claimsRepo *horizonrepo.SignalImpactClaimsRepository,
	hitsRepo *horizonrepo.SignalLandingHitsRepository,
	summariesRepo *horizonrepo.SignalLandingSummariesRepository,
	candidatesRepo *horizonrepo.SignalMissingAnchorCandidatesRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
) *Service {
	return &Service{
		sessionsRepo:   sessionsRepo,
		claimsRepo:     claimsRepo,
		hitsRepo:       hitsRepo,
		summariesRepo:  summariesRepo,
		candidatesRepo: candidatesRepo,
		fragmentsRepo:  fragmentsRepo,
	}
}

func (s *Service) WithMaterializedRepo(repo *horizonrepo.MaterializedArtifactsRepository) *Service {
	s.materializedRepo = repo
	return s
}

func (s *Service) WithProductDictionary(dict *horizonproductdict.Runtime) *Service {
	s.productDict = dict
	return s
}

func (s *Service) WithVertexSearch(client *ai.VertexAISearchClient) *Service {
	s.vertexSearch = client
	return s
}

func (s *Service) LandEvent(ctx context.Context, event *coremodels.OntologyFact, signals []*horizonmodels.SignalDescriptor) (*Result, error) {
	if s == nil {
		return nil, nil
	}
	if event == nil {
		return nil, fmt.Errorf("event is required")
	}
	tenantID := strings.TrimSpace(firstNonEmpty(event.TenantID, "global"))
	session := &horizonmodels.SignalLandingSession{
		TenantID:             tenantID,
		SourceEventID:        strings.TrimSpace(event.ID),
		SourceFactID:         strings.TrimSpace(event.ID),
		SourceDocumentRefs:   eventSourceDocumentRefs(event),
		Status:               "active",
		LandingVersion:       "v1",
		DecompositionVersion: "v1",
		GroundingVersion:     "v1",
		ExpansionVersion:     "v1",
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
	if strings.TrimSpace(session.ID) == "" {
		session.ID = "sls_" + normalizeCandidateLabel(strings.Join([]string{
			session.TenantID,
			session.SourceEventID,
			session.SourceFactID,
		}, "|"))
	}
	if s.sessionsRepo != nil {
		if err := s.sessionsRepo.Upsert(ctx, session); err != nil {
			return nil, err
		}
	}

	result := &Result{Session: session}
	summaryBuilders := map[string]*summaryBuilder{}
	primaryText := strings.TrimSpace(strings.Join([]string{event.Title, event.Summary}, "\n"))

	for idx, signal := range signals {
		if signal == nil {
			continue
		}
		claim := s.buildClaim(session, event, signal, idx, primaryText)
		if s.claimsRepo != nil {
			if err := s.claimsRepo.Upsert(ctx, claim); err != nil {
				return nil, err
			}
		}
		result.Claims = append(result.Claims, claim)

		hits, candidates := s.landClaim(ctx, session, event, signal, claim, primaryText)
		for _, hit := range hits {
			if s.hitsRepo != nil {
				if err := s.hitsRepo.Upsert(ctx, hit); err != nil {
					return nil, err
				}
			}
			result.Hits = append(result.Hits, hit)
		}
		for _, candidate := range candidates {
			if s.candidatesRepo != nil {
				if err := s.candidatesRepo.Upsert(ctx, candidate); err != nil {
					return nil, err
				}
			}
			result.Candidates = append(result.Candidates, candidate)
		}
		enrichClaimSeeds(signal, hits)
		accumulateSummaries(summaryBuilders, session, claim, hits, candidates)
	}

	for _, builder := range summaryBuilders {
		summary := builder.Build()
		if summary == nil {
			continue
		}
		if s.summariesRepo != nil {
			if err := s.summariesRepo.Upsert(ctx, summary); err != nil {
				return nil, err
			}
		}
		result.Summaries = append(result.Summaries, summary)
	}

	session.CoverageSummary = map[string]interface{}{
		"claimCount":              len(result.Claims),
		"hitCount":                len(result.Hits),
		"candidateCount":          len(result.Candidates),
		"summaryCount":            len(result.Summaries),
		"confirmedHitCount":       countHitsByClass(result.Hits, horizonmodels.SignalLandingClassConfirmed),
		"inferredHitCount":        countHitsByClass(result.Hits, horizonmodels.SignalLandingClassInferred),
		"missingPreciseZoneCount": countClaimsMissingZone(result.Claims),
	}
	session.UpdatedAt = time.Now().UTC()
	if s.sessionsRepo != nil {
		if err := s.sessionsRepo.Upsert(ctx, session); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Service) buildClaim(session *horizonmodels.SignalLandingSession, event *coremodels.OntologyFact, signal *horizonmodels.SignalDescriptor, idx int, primaryText string) *horizonmodels.SignalImpactClaim {
	labels := compactUniqueStrings(append([]string{}, signal.SeedNames...))
	if len(labels) == 0 {
		labels = compactUniqueStrings([]string{strings.TrimSpace(firstNonEmpty(event.Title, signal.Mechanism, signal.Axis))})
	}
	productHints := s.productHintsFromText(primaryText)
	productHints = compactUniqueStrings(append(productHints, extractPathsWithPrefix(event.CanonicalPaths, "/product/")...))
	routeHints := s.routeHintsFromText(primaryText)
	routeHints = compactUniqueStrings(append(routeHints, extractPathsWithPrefix(event.CanonicalPaths, "/zone/")...))
	facilityHints := compactUniqueStrings(append(extractPathsWithPrefix(signal.SeedPaths, "/fac/"), extractPathsWithPrefix(event.CanonicalPaths, "/fac/")...))
	orgHints := compactUniqueStrings(append(extractPathsWithPrefix(signal.ActorPaths, "/org/"), extractPathsWithPrefix(event.CanonicalPaths, "/org/")...))
	affectedZoneRefs := compactUniqueStrings(append(extractPathsWithPrefix(signal.SeedPaths, "/zone/"), extractPathsWithPrefix(event.CanonicalPaths, "/zone/")...))
	claim := &horizonmodels.SignalImpactClaim{
		SessionID:          strings.TrimSpace(session.ID),
		TenantID:           strings.TrimSpace(session.TenantID),
		SignalID:           strings.TrimSpace(signal.ID),
		ClaimKind:          "signal",
		Axis:               strings.TrimSpace(signal.Axis),
		Mechanism:          strings.TrimSpace(signal.Mechanism),
		Severity:           clamp01(maxFloat(signal.Amplitude, signal.Constraints.Impact)),
		Confidence:         clamp01(maxFloat(signal.Confidence, signal.Constraints.EscalationConfidence)),
		TimeHorizon:        strings.TrimSpace(firstNonEmpty(signal.Constraints.TimeHorizon, signal.Constraints.Timeframe)),
		SourceTextSpans:    compactUniqueStrings([]string{strings.TrimSpace(event.Title), strings.TrimSpace(event.Summary)}),
		SourceEvidenceRefs: compactUniqueStrings(append(eventSourceDocumentRefs(event), signal.SourceRefs...)),
		ClaimLabels:        labels,
		ClaimAliases:       compactUniqueStrings(append([]string{}, signal.SeedNames...)),
		CountryHints:       compactUniqueStrings([]string{countryFromPath(strings.TrimSpace(firstNonEmpty(signal.LocationPath, derefString(event.LocationPath))))}),
		ProductHints:       productHints,
		RouteHints:         routeHints,
		FacilityHints:      facilityHints,
		OrgHints:           orgHints,
		ZoneEligibility:    determineZoneEligibility(event, signal, primaryText),
		AffectedZoneRefs:   affectedZoneRefs,
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	}
	if claim.TimeHorizon == "" {
		claim.TimeHorizon = horizonmodels.SignalTimeHorizonShortTerm
	}
	if claim.Confidence == 0 {
		claim.Confidence = clamp01(signal.Confidence)
	}
	claim.Normalize()
	claim.ID = horizonrepoSignalClaimID(session, signal, idx, claim)
	return claim
}

func (s *Service) landClaim(ctx context.Context, session *horizonmodels.SignalLandingSession, event *coremodels.OntologyFact, signal *horizonmodels.SignalDescriptor, claim *horizonmodels.SignalImpactClaim, primaryText string) ([]*horizonmodels.SignalLandingHit, []*horizonmodels.SignalMissingAnchorCandidate) {
	hits := make([]*horizonmodels.SignalLandingHit, 0, 16)
	candidates := make([]*horizonmodels.SignalMissingAnchorCandidate, 0, 8)
	seenHits := map[string]struct{}{}

	addHit := func(ref, why string, class horizonmodels.SignalLandingClass, method horizonmodels.SignalLandingMethod, confidence, importance float64, paths ...string) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}
		key := ref + "|" + string(class) + "|" + string(method)
		if _, ok := seenHits[key]; ok {
			return
		}
		seenHits[key] = struct{}{}
		hit := &horizonmodels.SignalLandingHit{
			SessionID:              strings.TrimSpace(session.ID),
			ClaimID:                strings.TrimSpace(claim.ID),
			TenantID:               strings.TrimSpace(session.TenantID),
			CanonicalRef:           ref,
			RefKind:                refKindFromCanonicalPath(ref),
			LandingClass:           class,
			LandingMethod:          method,
			Confidence:             clamp01(confidence),
			Importance:             clamp01(importance),
			Why:                    strings.TrimSpace(why),
			SupportingEvidenceRefs: claim.SourceEvidenceRefs,
			SupportingPaths:        compactUniqueStrings(paths),
			CreatedAt:              time.Now().UTC(),
		}
		hit.Normalize()
		hits = append(hits, hit)
	}

	for _, ref := range compactUniqueStrings(append(append([]string{}, event.CanonicalPaths...), signal.SeedPaths...)) {
		method := horizonmodels.SignalLandingMethodExactCanonical
		if strings.HasPrefix(ref, "/zone/") {
			method = horizonmodels.SignalLandingMethodZoneAnchor
		}
		addHit(ref, "event or signal already carried canonical reference", horizonmodels.SignalLandingClassConfirmed, method, claim.Confidence, claim.Severity, ref)
	}
	for _, ref := range signal.ActorPaths {
		addHit(ref, "signal actor path anchored the claim", horizonmodels.SignalLandingClassConfirmed, horizonmodels.SignalLandingMethodExactCanonical, claim.Confidence, claim.Severity)
	}
	if loc := strings.TrimSpace(firstNonEmpty(signal.LocationPath, derefString(event.LocationPath))); loc != "" {
		addHit(loc, "signal location path anchored the claim", horizonmodels.SignalLandingClassConfirmed, horizonmodels.SignalLandingMethodCountryAnchor, claim.Confidence, claim.Severity)
	}
	for _, ref := range s.knownChokepointRefs(primaryText) {
		addHit(ref, "recognized chokepoint or route phrase in source text", horizonmodels.SignalLandingClassConfirmed, horizonmodels.SignalLandingMethodExactCanonical, clamp01(claim.Confidence+0.05), claim.Severity)
	}

	if s.productDict != nil {
		for _, query := range claim.ProductHints {
			paths, err := s.productDict.SearchCanonicalProductPathsForTenant(ctx, session.TenantID, query, 4)
			if err != nil {
				continue
			}
			for _, path := range paths {
				addHit(path, "product hint resolved through canonical product dictionary", horizonmodels.SignalLandingClassConfirmed, horizonmodels.SignalLandingMethodCanonicalDict, clamp01(claim.Confidence+0.05), claim.Severity, query)
			}
		}
	}

	if s.vertexSearch != nil && s.vertexSearch.IsConfigured() {
		queries := compactUniqueStrings(append(append(append([]string{}, claim.ClaimLabels...), claim.RouteHints...), claim.FacilityHints...))
		for _, query := range queries {
			if len(hits) >= 18 {
				break
			}
			if ref := s.resolveRefByVertex(ctx, query); ref != "" {
				addHit(ref, "semantic grounding resolved an additional canonical reference", horizonmodels.SignalLandingClassConfirmed, horizonmodels.SignalLandingMethodVertexGrounding, clamp01(claim.Confidence), maxFloat(claim.Severity, 0.55), query)
			}
		}
	}

	if s.materializedRepo != nil {
		anchorRefs := make([]string, 0, len(hits))
		for _, hit := range hits {
			if hit == nil || strings.TrimSpace(hit.CanonicalRef) == "" {
				continue
			}
			if hit.RefKind == "artifact" {
				continue
			}
			anchorRefs = append(anchorRefs, hit.CanonicalRef)
		}
		artifacts, err := s.materializedRepo.FindByNodeRefsAny(ctx, session.TenantID, compactUniqueStrings(anchorRefs), horizonmodels.MaterializedKindSupplyChainPath, 12)
		if err == nil {
			for _, artifact := range artifacts {
				if artifact == nil {
					continue
				}
				for _, ref := range firstNStrings(compactUniqueStrings(artifact.NodeRefs), 8) {
					if containsString(anchorRefs, ref) {
						continue
					}
					addHit(ref, "materialized supply-chain path expanded the landing neighborhood", horizonmodels.SignalLandingClassInferred, horizonmodels.SignalLandingMethodMaterialized, clamp01(claim.Confidence*0.8), maxFloat(0.45, claim.Severity*0.8), artifact.CanonicalKey)
				}
				for _, ref := range compactUniqueStrings(append(artifact.ZoneRefs, artifact.AffectedZoneRefs...)) {
					addHit(ref, "materialized path exposed affected zone overlap", horizonmodels.SignalLandingClassInferred, horizonmodels.SignalLandingMethodPathFamilyExpand, clamp01(claim.Confidence*0.8), maxFloat(0.45, claim.Severity*0.8), artifact.CanonicalKey)
				}
				if axis := strings.TrimSpace(artifact.ProductAxis); axis != "" {
					addHit(axis, "materialized path exposed product axis continuity", horizonmodels.SignalLandingClassInferred, horizonmodels.SignalLandingMethodPathFamilyExpand, clamp01(claim.Confidence*0.8), maxFloat(0.45, claim.Severity*0.8), artifact.CanonicalKey)
				}
			}
		}
	}

	zoneHits := hitsWithPrefix(hits, "/zone/")
	if claim.ZoneEligibility == horizonmodels.SignalZoneEligibilityRequired && len(zoneHits) == 0 {
		claim.MissingPreciseZone = true
		claim.UpdatedAt = time.Now().UTC()
		candidates = append(candidates, &horizonmodels.SignalMissingAnchorCandidate{
			SessionID:              strings.TrimSpace(session.ID),
			ClaimID:                strings.TrimSpace(claim.ID),
			TenantID:               strings.TrimSpace(session.TenantID),
			CandidateKind:          horizonmodels.SignalMissingCandidateEntity,
			Label:                  strings.TrimSpace(firstNonEmpty(derefString(event.LocationPath), signal.LocationPath, event.Title, claim.Axis)),
			NormalizedLabel:        normalizeCandidateLabel(firstNonEmpty(derefString(event.LocationPath), signal.LocationPath, event.Title, claim.Axis)),
			HintCountry:            countryFromPath(strings.TrimSpace(firstNonEmpty(signal.LocationPath, derefString(event.LocationPath)))),
			HintGeo:                strings.TrimSpace(firstNonEmpty(signal.LocationPath, derefString(event.LocationPath))),
			Reason:                 "zone-eligible disruption claim did not resolve to a precise affected zone",
			Importance:             clamp01(maxFloat(claim.Severity, 0.6)),
			Confidence:             clamp01(claim.Confidence),
			SuggestedSourceRefs:    claim.SourceEvidenceRefs,
			Needs:                  []string{"fragment_bootstrap"},
			SuggestedCanonicalPath: strings.TrimSpace(firstNonEmpty(signal.LocationPath, derefString(event.LocationPath))),
			CreatedAt:              time.Now().UTC(),
			UpdatedAt:              time.Now().UTC(),
		})
	}

	if len(hitsWithPrefix(hits, "/product/")) == 0 {
		for _, query := range firstNStrings(claim.ProductHints, 4) {
			candidates = append(candidates, &horizonmodels.SignalMissingAnchorCandidate{
				SessionID:           strings.TrimSpace(session.ID),
				ClaimID:             strings.TrimSpace(claim.ID),
				TenantID:            strings.TrimSpace(session.TenantID),
				CandidateKind:       horizonmodels.SignalMissingCandidateProduct,
				Label:               query,
				NormalizedLabel:     normalizeCandidateLabel(query),
				HintCountry:         countryFromPath(strings.TrimSpace(firstNonEmpty(signal.LocationPath, derefString(event.LocationPath)))),
				HintProductClass:    query,
				Reason:              "text suggests product-level impact, but no canonical product ref landed",
				Importance:          clamp01(maxFloat(claim.Severity*0.9, 0.45)),
				Confidence:          clamp01(claim.Confidence * 0.8),
				SuggestedSourceRefs: claim.SourceEvidenceRefs,
				Needs:               []string{"canonical_product_review"},
				CreatedAt:           time.Now().UTC(),
				UpdatedAt:           time.Now().UTC(),
			})
		}
	}

	if len(hitsWithKinds(hits, "route", "zone", "geo")) == 0 {
		for _, query := range firstNStrings(claim.RouteHints, 3) {
			candidates = append(candidates, &horizonmodels.SignalMissingAnchorCandidate{
				SessionID:           strings.TrimSpace(session.ID),
				ClaimID:             strings.TrimSpace(claim.ID),
				TenantID:            strings.TrimSpace(session.TenantID),
				CandidateKind:       horizonmodels.SignalMissingCandidateRoute,
				Label:               query,
				NormalizedLabel:     normalizeCandidateLabel(query),
				HintCountry:         countryFromPath(strings.TrimSpace(firstNonEmpty(signal.LocationPath, derefString(event.LocationPath)))),
				HintRouteClass:      query,
				Reason:              "text suggests route or chokepoint impact, but no canonical route ref landed",
				Importance:          clamp01(maxFloat(claim.Severity*0.85, 0.4)),
				Confidence:          clamp01(claim.Confidence * 0.75),
				SuggestedSourceRefs: claim.SourceEvidenceRefs,
				Needs:               []string{"route_profile_review"},
				CreatedAt:           time.Now().UTC(),
				UpdatedAt:           time.Now().UTC(),
			})
		}
	}

	for _, candidate := range candidates {
		candidate.Normalize()
	}
	return hits, dedupeCandidates(candidates)
}

func (s *Service) resolveRefByVertex(ctx context.Context, query string) string {
	query = strings.TrimSpace(query)
	if query == "" || s == nil || s.vertexSearch == nil || !s.vertexSearch.IsConfigured() {
		return ""
	}
	resp, err := s.vertexSearch.Search(ctx, &ai.SearchRequest{
		Query:    query,
		PageSize: 3,
	})
	if err != nil || resp == nil {
		return ""
	}
	for _, item := range resp.Results {
		ref := strings.TrimSpace(firstNonEmpty(
			asString(item.Document.StructData["canonicalPath"]),
			asString(item.Document.StructData["targetPath"]),
			asString(item.Document.DerivedStructData["canonicalPath"]),
			asString(item.Document.DerivedStructData["targetPath"]),
		))
		if strings.HasPrefix(ref, "/") {
			return ref
		}
	}
	return ""
}

func (s *Service) knownChokepointRefs(text string) []string {
	normalized := strings.ToLower(strings.TrimSpace(text))
	out := []string{}
	if strings.Contains(normalized, "strait of hormuz") || strings.Contains(normalized, "hormuz") {
		out = append(out, "/loc/int/strait-of-hormuz")
	}
	if strings.Contains(normalized, "bab el-mandeb") || strings.Contains(normalized, "bab el mandeb") {
		out = append(out, "/loc/int/bab-el-mandeb")
	}
	if strings.Contains(normalized, "suez") {
		out = append(out, "/loc/int/suez-canal")
	}
	if strings.Contains(normalized, "ras laffan") {
		out = append(out, "/fac/qa/ras-laffan-industrial-city")
	}
	return compactUniqueStrings(out)
}

func (s *Service) productHintsFromText(text string) []string {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return nil
	}
	out := []string{}
	for phrase, query := range map[string]string{
		"helium":                    "helium",
		"liquid helium":             "liquid helium",
		"crude helium":              "crude helium",
		"lng":                       "lng",
		"liquefied natural gas":     "liquefied natural gas",
		"natural gas":               "natural gas",
		"reverse osmosis membrane":  "reverse osmosis membrane",
		"reverse osmosis membranes": "reverse osmosis membrane",
		"desalination membrane":     "desalination membrane",
		"battery cell":              "battery cell",
		"battery module":            "battery module",
		"battery pack":              "battery pack",
		"semiconductor wafer":       "semiconductor wafer",
	} {
		if strings.Contains(normalized, phrase) {
			out = append(out, query)
		}
	}
	return compactUniqueStrings(out)
}

func (s *Service) routeHintsFromText(text string) []string {
	normalized := strings.ToLower(strings.TrimSpace(text))
	out := []string{}
	for _, phrase := range []string{
		"strait of hormuz",
		"bab el-mandeb",
		"bab el mandeb",
		"suez canal",
		"port of hamburg",
		"ras laffan",
	} {
		if strings.Contains(normalized, phrase) {
			out = append(out, phrase)
		}
	}
	return compactUniqueStrings(out)
}

type summaryBuilder struct {
	tenantID           string
	scopeRef           string
	sessionIDs         []string
	confirmed          []string
	inferred           []string
	missing            []string
	claims             []string
	products           []string
	routes             []string
	facilities         []string
	chokepoints        []string
	zones              []string
	missingPreciseZone bool
	updatedAt          time.Time
}

func (b *summaryBuilder) Build() *horizonmodels.SignalLandingSummary {
	if b == nil || strings.TrimSpace(b.scopeRef) == "" || strings.TrimSpace(b.tenantID) == "" {
		return nil
	}
	return &horizonmodels.SignalLandingSummary{
		TenantID:             strings.TrimSpace(b.tenantID),
		ScopeRef:             strings.TrimSpace(b.scopeRef),
		SessionIDs:           compactUniqueStrings(b.sessionIDs),
		TopConfirmedRefs:     firstNStrings(compactUniqueStrings(b.confirmed), 12),
		TopInferredRefs:      firstNStrings(compactUniqueStrings(b.inferred), 12),
		TopMissingCandidates: firstNStrings(compactUniqueStrings(b.missing), 8),
		TopClaims:            firstNStrings(compactUniqueStrings(b.claims), 8),
		Products:             firstNStrings(compactUniqueStrings(b.products), 12),
		Routes:               firstNStrings(compactUniqueStrings(b.routes), 10),
		Facilities:           firstNStrings(compactUniqueStrings(b.facilities), 10),
		Chokepoints:          firstNStrings(compactUniqueStrings(b.chokepoints), 10),
		AffectedZoneRefs:     firstNStrings(compactUniqueStrings(b.zones), 10),
		MissingPreciseZone:   b.missingPreciseZone,
		UpdatedAt:            nonZeroTime(b.updatedAt),
	}
}

func accumulateSummaries(builders map[string]*summaryBuilder, session *horizonmodels.SignalLandingSession, claim *horizonmodels.SignalImpactClaim, hits []*horizonmodels.SignalLandingHit, candidates []*horizonmodels.SignalMissingAnchorCandidate) {
	scopeRefs := compactUniqueStrings(append(append([]string{}, claim.AffectedZoneRefs...), claim.CountryHints...))
	for _, hit := range hits {
		if hit == nil {
			continue
		}
		if strings.HasPrefix(hit.CanonicalRef, "/zone/") || strings.HasPrefix(hit.CanonicalRef, "/loc/") {
			scopeRefs = append(scopeRefs, hit.CanonicalRef)
		}
	}
	scopeRefs = compactUniqueStrings(scopeRefs)
	if len(scopeRefs) == 0 && len(hits) > 0 {
		scopeRefs = []string{hits[0].CanonicalRef}
	}
	if len(scopeRefs) == 0 {
		scopeRefs = []string{"/signal-landing/" + strings.TrimSpace(session.ID)}
	}
	for _, scopeRef := range scopeRefs {
		builder := builders[scopeRef]
		if builder == nil {
			builder = &summaryBuilder{
				tenantID: session.TenantID,
				scopeRef: scopeRef,
			}
			builders[scopeRef] = builder
		}
		builder.sessionIDs = append(builder.sessionIDs, session.ID)
		builder.claims = append(builder.claims, firstNonEmpty(strings.Join(claim.ClaimLabels, ", "), claim.Axis))
		builder.zones = append(builder.zones, claim.AffectedZoneRefs...)
		builder.missingPreciseZone = builder.missingPreciseZone || claim.MissingPreciseZone
		for _, hit := range hits {
			if hit == nil {
				continue
			}
			switch hit.LandingClass {
			case horizonmodels.SignalLandingClassConfirmed:
				builder.confirmed = append(builder.confirmed, hit.CanonicalRef)
			case horizonmodels.SignalLandingClassInferred:
				builder.inferred = append(builder.inferred, hit.CanonicalRef)
			}
			switch hit.RefKind {
			case "product":
				builder.products = append(builder.products, hit.CanonicalRef)
			case "route", "zone", "geo":
				builder.routes = append(builder.routes, hit.CanonicalRef)
				if strings.Contains(hit.CanonicalRef, "hormuz") || strings.Contains(hit.CanonicalRef, "suez") || strings.Contains(hit.CanonicalRef, "mandeb") {
					builder.chokepoints = append(builder.chokepoints, hit.CanonicalRef)
				}
			case "facility":
				builder.facilities = append(builder.facilities, hit.CanonicalRef)
			}
		}
		for _, candidate := range candidates {
			if candidate == nil {
				continue
			}
			builder.missing = append(builder.missing, candidate.Label)
		}
		builder.updatedAt = time.Now().UTC()
	}
}

func determineZoneEligibility(event *coremodels.OntologyFact, signal *horizonmodels.SignalDescriptor, text string) horizonmodels.SignalZoneEligibility {
	normalized := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(event.Type),
		strings.TrimSpace(signal.Axis),
		strings.TrimSpace(signal.Mechanism),
		strings.TrimSpace(text),
	}, " ")))
	switch {
	case hasAny(normalized,
		"military_conflict", "infrastructure_disruption", "cyber_disruption", "humanitarian_crisis",
		"disease_outbreak", "natural_disaster", "earthquake", "flood", "wildfire", "pandemic",
		"attack", "airstrike", "missile", "blackout", "outage", "port closure", "blockade"):
		return horizonmodels.SignalZoneEligibilityRequired
	case hasAny(normalized,
		"shipping disruption", "strike", "weather disruption", "security incident", "gps jamming", "transport disruption"):
		return horizonmodels.SignalZoneEligibilityAllowed
	default:
		return horizonmodels.SignalZoneEligibilityForbidden
	}
}

func horizonrepoSignalClaimID(session *horizonmodels.SignalLandingSession, signal *horizonmodels.SignalDescriptor, idx int, claim *horizonmodels.SignalImpactClaim) string {
	return fmt.Sprintf("slc_%s", normalizeCandidateLabel(strings.Join([]string{
		strings.TrimSpace(session.ID),
		strings.TrimSpace(signal.ID),
		strconvI(idx),
		strings.TrimSpace(claim.Axis),
		strings.TrimSpace(claim.Mechanism),
		strings.Join(claim.ClaimLabels, "|"),
	}, "|")))
}

func enrichClaimSeeds(signal *horizonmodels.SignalDescriptor, hits []*horizonmodels.SignalLandingHit) {
	if signal == nil || len(signal.SeedPaths) > 0 {
		return
	}
	seeds := make([]string, 0, 4)
	for _, hit := range hits {
		if hit == nil || hit.LandingClass != horizonmodels.SignalLandingClassConfirmed {
			continue
		}
		switch hit.RefKind {
		case "product", "facility", "organization", "geo", "zone", "market":
			seeds = append(seeds, hit.CanonicalRef)
		}
		if len(seeds) >= 4 {
			break
		}
	}
	signal.SeedPaths = compactUniqueStrings(seeds)
}

func countHitsByClass(hits []*horizonmodels.SignalLandingHit, class horizonmodels.SignalLandingClass) int {
	out := 0
	for _, hit := range hits {
		if hit != nil && hit.LandingClass == class {
			out++
		}
	}
	return out
}

func countClaimsMissingZone(claims []*horizonmodels.SignalImpactClaim) int {
	out := 0
	for _, claim := range claims {
		if claim != nil && claim.MissingPreciseZone {
			out++
		}
	}
	return out
}

func hitsWithPrefix(hits []*horizonmodels.SignalLandingHit, prefix string) []string {
	out := []string{}
	for _, hit := range hits {
		if hit == nil {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(hit.CanonicalRef), prefix) {
			out = append(out, strings.TrimSpace(hit.CanonicalRef))
		}
	}
	return compactUniqueStrings(out)
}

func hitsWithKinds(hits []*horizonmodels.SignalLandingHit, kinds ...string) []string {
	kindSet := map[string]struct{}{}
	for _, kind := range kinds {
		kindSet[strings.TrimSpace(kind)] = struct{}{}
	}
	out := []string{}
	for _, hit := range hits {
		if hit == nil {
			continue
		}
		if _, ok := kindSet[strings.TrimSpace(hit.RefKind)]; ok {
			out = append(out, hit.CanonicalRef)
		}
	}
	return compactUniqueStrings(out)
}

func refKindFromCanonicalPath(path string) string {
	path = strings.TrimSpace(path)
	switch {
	case strings.HasPrefix(path, "/product/"):
		return "product"
	case strings.HasPrefix(path, "/fac/"):
		return "facility"
	case strings.HasPrefix(path, "/org/"):
		return "organization"
	case strings.HasPrefix(path, "/loc/"):
		return "geo"
	case strings.HasPrefix(path, "/zone/"):
		return "zone"
	case strings.HasPrefix(path, "/market/"):
		return "market"
	case strings.HasPrefix(path, "/route/"):
		return "route"
	default:
		return "entity"
	}
}

func extractPathsWithPrefix(paths []string, prefix string) []string {
	out := []string{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if strings.HasPrefix(path, prefix) {
			out = append(out, path)
		}
	}
	return compactUniqueStrings(out)
}

func eventSourceDocumentRefs(event *coremodels.OntologyFact) []string {
	if event == nil {
		return nil
	}
	out := compactUniqueStrings(append([]string{}, event.SourceIDs...))
	for _, ref := range event.SourceRefs {
		if src := strings.TrimSpace(firstNonEmpty(ref.SourceID, ref.SourceType)); src != "" {
			out = append(out, src)
		}
	}
	return compactUniqueStrings(out)
}

func compactUniqueStrings(values []string) []string {
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

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func firstNStrings(values []string, n int) []string {
	values = compactUniqueStrings(values)
	if n <= 0 || len(values) <= n {
		return values
	}
	return values[:n]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func normalizeCandidateLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = nonAlphaNumeric.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "unknown"
	}
	return value
}

func countryFromPath(path string) string {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/loc/") && !strings.HasPrefix(path, "/zone/") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(parts[1]))
}

func hasAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(needle))) {
			return true
		}
	}
	return false
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func asString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return ""
	}
}

func nonZeroTime(v time.Time) time.Time {
	if v.IsZero() {
		return time.Now().UTC()
	}
	return v
}

func dedupeCandidates(items []*horizonmodels.SignalMissingAnchorCandidate) []*horizonmodels.SignalMissingAnchorCandidate {
	out := make([]*horizonmodels.SignalMissingAnchorCandidate, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		if item == nil {
			continue
		}
		key := strings.Join([]string{
			strings.TrimSpace(item.SessionID),
			strings.TrimSpace(item.ClaimID),
			string(item.CandidateKind),
			strings.TrimSpace(item.NormalizedLabel),
			strings.TrimSpace(item.SuggestedCanonicalPath),
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Importance == out[j].Importance {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Importance > out[j].Importance
	})
	return out
}

func strconvI(v int) string {
	return fmt.Sprintf("%d", v)
}
