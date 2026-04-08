package lensruntime

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	neturl "net/url"
	"os"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscopeagent "github.com/igorkirisyuk/sanskrite/internal/horizon/scopeagent"
	horizonwatchruntime "github.com/igorkirisyuk/sanskrite/internal/horizon/watchruntime"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

const (
	defaultLensRuntimeOwner        = "horizon-lens-runtime@system.local"
	defaultLensRuntimePollMinutes  = 60
	minLensRuntimeGeographyTargets = 18
	maxLensRuntimeTargetsPerLens   = 24
	maxLensRuntimeGeoTerms         = 10
	maxLensRuntimeQueryTerms       = 10
	maxLensRuntimeArtifactsPerLens = 80
	lensRuntimeDefaultSyncLimit    = 100
	lensRuntimeTargetKindGeography = "geography"
	lensRuntimeTargetKindFragment  = "fragment"
)

type Service struct {
	lensesRepo       *horizonrepo.LensesRepository
	fragmentsRepo    *horizonrepo.HorizonFragmentsRepository
	materializedRepo *horizonrepo.MaterializedArtifactsRepository
	targetsRepo      *horizonrepo.WatchTargetsRepository
	dataSourcesRepo  *corerepo.DataSourcesRepository
	watchRuntime     *horizonwatchruntime.Service
	scopeAgent       *horizonscopeagent.Service
}

type SyncReport struct {
	RequestedTenant string            `json:"requestedTenant,omitempty"`
	IncludeGlobal   bool              `json:"includeGlobal"`
	Limit           int               `json:"limit"`
	LensesScanned   int               `json:"lensesScanned"`
	LensesSynced    int               `json:"lensesSynced"`
	TargetsCreated  int               `json:"targetsCreated"`
	TargetsUpdated  int               `json:"targetsUpdated"`
	SourcesCreated  int               `json:"sourcesCreated"`
	SourcesUpdated  int               `json:"sourcesUpdated"`
	Results         []*LensSyncResult `json:"results,omitempty"`
	Errors          []string          `json:"errors,omitempty"`
	StartedAt       time.Time         `json:"startedAt"`
	FinishedAt      time.Time         `json:"finishedAt"`
}

type LensSyncResult struct {
	LensID         string   `json:"lensId"`
	LensName       string   `json:"lensName,omitempty"`
	TenantID       string   `json:"tenantId,omitempty"`
	ScopePaths     int      `json:"scopePaths"`
	CandidatePaths int      `json:"candidatePaths"`
	WatchTargets   []string `json:"watchTargets,omitempty"`
	CreatedTargets int      `json:"createdTargets"`
	UpdatedTargets int      `json:"updatedTargets"`
	Sources        []string `json:"sources,omitempty"`
	CreatedSources int      `json:"createdSources"`
	UpdatedSources int      `json:"updatedSources"`
	Errors         []string `json:"errors,omitempty"`
}

type geoInterest struct {
	CanonicalPath string
	Label         string
	Score         int
}

type sourceTemplate struct {
	KeySuffix  string
	Title      string
	Category   string
	SignalType string
	QueryTerms []string
}

type sourceSpec struct {
	ID                string
	DisplayName       string
	URL               string
	SearchQuery       string
	CategoryPath      string
	Category          string
	FeedType          string
	SignalType        string
	GeoFacets         []string
	PrimaryGeoFacet   string
	SourceFormatFacet string
}

func NewService(
	lensesRepo *horizonrepo.LensesRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	materializedRepo *horizonrepo.MaterializedArtifactsRepository,
	targetsRepo *horizonrepo.WatchTargetsRepository,
	dataSourcesRepo *corerepo.DataSourcesRepository,
	watchRuntime *horizonwatchruntime.Service,
	scopeAgent *horizonscopeagent.Service,
) *Service {
	return &Service{
		lensesRepo:       lensesRepo,
		fragmentsRepo:    fragmentsRepo,
		materializedRepo: materializedRepo,
		targetsRepo:      targetsRepo,
		dataSourcesRepo:  dataSourcesRepo,
		watchRuntime:     watchRuntime,
		scopeAgent:       scopeAgent,
	}
}

func (s *Service) SyncAllActiveLenses(ctx context.Context, tenantID string, includeGlobal bool, limit int) (*SyncReport, error) {
	if s == nil || s.lensesRepo == nil {
		return nil, fmt.Errorf("lens runtime service is not configured")
	}
	if limit <= 0 {
		limit = lensRuntimeDefaultSyncLimit
	}
	report := &SyncReport{
		RequestedTenant: strings.TrimSpace(tenantID),
		IncludeGlobal:   includeGlobal,
		Limit:           limit,
		StartedAt:       time.Now().UTC(),
	}

	lenses, err := s.listActiveLenses(ctx, tenantID, includeGlobal, limit)
	if err != nil {
		return nil, err
	}
	report.LensesScanned = len(lenses)
	for _, lens := range lenses {
		if lens == nil {
			continue
		}
		result, syncErr := s.SyncLens(ctx, strings.TrimSpace(lens.ID))
		if syncErr != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %v", strings.TrimSpace(lens.ID), syncErr))
			continue
		}
		report.LensesSynced++
		report.TargetsCreated += result.CreatedTargets
		report.TargetsUpdated += result.UpdatedTargets
		report.SourcesCreated += result.CreatedSources
		report.SourcesUpdated += result.UpdatedSources
		report.Results = append(report.Results, result)
	}
	report.FinishedAt = time.Now().UTC()
	return report, nil
}

func (s *Service) SyncLens(ctx context.Context, lensID string) (*LensSyncResult, error) {
	if s == nil || s.lensesRepo == nil {
		return nil, fmt.Errorf("lens runtime service is not configured")
	}
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		return nil, fmt.Errorf("lensID is required")
	}
	lens, err := s.lensesRepo.Get(ctx, lensID)
	if err != nil {
		return nil, err
	}
	verbosef("sync lens=%s name=%q tenant=%s", lensID, strings.TrimSpace(lens.Name), strings.TrimSpace(lens.TenantID))
	result := &LensSyncResult{
		LensID:   lensID,
		LensName: strings.TrimSpace(lens.Name),
		TenantID: strings.TrimSpace(lens.TenantID),
	}

	scopePaths := workscope.LensScopePathsWithResolvedQueries(lens)
	artifactRefs, artifactsErr := s.collectArtifactRefs(ctx, lens)
	if artifactsErr != nil {
		result.Errors = append(result.Errors, artifactsErr.Error())
	}
	allPaths := compactUniqueStrings(append(scopePaths, artifactRefs...))
	verbosef("lens=%s scopePaths=%d artifactRefs=%d allPaths=%d", lensID, len(scopePaths), len(artifactRefs), len(allPaths))
	result.ScopePaths = len(scopePaths)
	result.CandidatePaths = len(allPaths)

	targetSpecs := s.buildWatchTargets(ctx, lens, scopePaths, allPaths)
	verbosef("lens=%s targetSpecs=%d", lensID, len(targetSpecs))
	for _, target := range targetSpecs {
		verbosef("lens=%s ensure target=%s kind=%s", lensID, strings.TrimSpace(target.CanonicalPath), strings.TrimSpace(target.Kind))
		status, subjectID, targetErr := s.ensureWatchTarget(ctx, lens, target)
		if targetErr != nil {
			result.Errors = append(result.Errors, targetErr.Error())
			continue
		}
		if status == "created" {
			result.CreatedTargets++
		} else {
			result.UpdatedTargets++
		}
		targetRef := strings.TrimSpace(target.CanonicalPath)
		if subjectID != "" {
			targetRef = targetRef + " -> " + subjectID
		}
		result.WatchTargets = append(result.WatchTargets, targetRef)
	}

	sourceSpecs := s.buildSourceSpecs(ctx, lens, scopePaths, allPaths)
	verbosef("lens=%s sourceSpecs=%d", lensID, len(sourceSpecs))
	for _, spec := range sourceSpecs {
		verbosef("lens=%s ensure source=%s", lensID, strings.TrimSpace(spec.DisplayName))
		status, sourceErr := s.ensureDataSource(ctx, spec)
		if sourceErr != nil {
			result.Errors = append(result.Errors, sourceErr.Error())
			continue
		}
		if status == "created" {
			result.CreatedSources++
		} else {
			result.UpdatedSources++
		}
		result.Sources = append(result.Sources, spec.DisplayName)
	}

	sort.Strings(result.WatchTargets)
	sort.Strings(result.Sources)
	return result, nil
}

func (s *Service) listActiveLenses(ctx context.Context, tenantID string, includeGlobal bool, limit int) ([]*horizonmodels.Lens, error) {
	tenantID = strings.TrimSpace(tenantID)
	switch {
	case tenantID == "" && includeGlobal:
		return s.lensesRepo.ListAllActive(ctx, limit)
	case tenantID == "":
		return s.lensesRepo.ListAllActive(ctx, limit)
	default:
		out, err := s.lensesRepo.ListActive(ctx, tenantID, limit)
		if err != nil {
			return nil, err
		}
		if includeGlobal && tenantID != "global" {
			global, gErr := s.lensesRepo.ListActive(ctx, "global", limit)
			if gErr != nil {
				return nil, gErr
			}
			out = append(out, global...)
		}
		return dedupeLenses(out), nil
	}
}

func (s *Service) collectArtifactRefs(ctx context.Context, lens *horizonmodels.Lens) ([]string, error) {
	if lens == nil {
		return nil, nil
	}
	refs := make([]string, 0, 64)
	artifactKeys := make([]string, 0, len(lens.ScopeArtifacts)+len(lens.Scope))
	var errs []string
	for _, raw := range lens.Scope {
		if key := normalizedArtifactCanonicalKey(raw); strings.HasPrefix(strings.TrimSpace(raw), "/artifact/") && key != "" {
			artifactKeys = append(artifactKeys, key)
		}
	}
	for key, ref := range lens.ScopeArtifacts {
		artifactKeys = append(artifactKeys, normalizedArtifactCanonicalKey(key))
		refs = append(refs, strings.TrimSpace(key), strings.TrimSpace(ref.AnchorSupplier), strings.TrimSpace(ref.AnchorCustomer))
		refs = append(refs, ref.NodeRefs...)
		refs = append(refs, ref.GeoRefs...)
		refs = append(refs, ref.ZoneRefs...)
		refs = append(refs, ref.AffectedZoneRefs...)
	}
	for _, artifactKey := range compactUniqueStrings(artifactKeys) {
		if artifactKey == "" || s.materializedRepo == nil {
			continue
		}
		artifact, err := s.lookupMaterializedArtifact(ctx, strings.TrimSpace(lens.TenantID), artifactKey)
		if err != nil {
			errs = append(errs, fmt.Sprintf("load artifact %s: %v", artifactKey, err))
			continue
		}
		refs = append(refs, collectMaterializedArtifactRefs(artifact)...)
	}
	if s.materializedRepo != nil {
		if artifacts, err := s.materializedRepo.ListByLens(ctx, strings.TrimSpace(lens.ID), maxLensRuntimeArtifactsPerLens); err == nil {
			for _, artifact := range artifacts {
				refs = append(refs, collectMaterializedArtifactRefs(artifact)...)
			}
		} else {
			errs = append(errs, fmt.Sprintf("list lens artifacts %s: %v", strings.TrimSpace(lens.ID), err))
		}
	}
	refs = compactUniqueStrings(expandMaritimeNeighborhoodRefs(refs))
	if len(errs) > 0 {
		return refs, errors.New(strings.Join(errs, "; "))
	}
	return refs, nil
}

func (s *Service) buildWatchTargets(ctx context.Context, lens *horizonmodels.Lens, directScope []string, allPaths []string) []*horizonmodels.WatchTarget {
	if lens == nil {
		return nil
	}
	allPaths = augmentWithCountryFacetPaths(allPaths)
	directSet := make(map[string]struct{}, len(directScope))
	for _, path := range directScope {
		path = strings.TrimSpace(path)
		if path != "" {
			directSet[path] = struct{}{}
		}
	}

	fragmentsByPath := s.lookupFragments(ctx, allPaths)
	scored := make([]geoInterest, 0, len(allPaths))
	for _, path := range allPaths {
		path = strings.TrimSpace(path)
		if !watchableCanonicalPath(path) {
			continue
		}
		score := canonicalPathWatchScore(path)
		if _, ok := directSet[path]; ok {
			score += 35
		}
		score += maritimeNeighborhoodBonus(path)
		frag := fragmentsByPath[path]
		label := labelFromCanonicalPath(path)
		if countryPath, countryLabel := countryFacetForPath(path); countryPath == path && countryLabel != "" {
			label = countryLabel
		}
		if frag != nil {
			label = strings.TrimSpace(firstNonEmpty(fragmentDisplayName(frag), label))
			if countryPath, countryLabel := countryFacetForPath(path); countryPath == path && countryLabel != "" && len(strings.TrimSpace(label)) <= 3 {
				label = countryLabel
			}
			if strings.TrimSpace(frag.ID) != "" {
				score += 5
			}
		}
		scored = append(scored, geoInterest{
			CanonicalPath: path,
			Label:         label,
			Score:         score,
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].CanonicalPath < scored[j].CanonicalPath
		}
		return scored[i].Score > scored[j].Score
	})
	ordered := make([]geoInterest, 0, minInt(len(scored), maxLensRuntimeTargetsPerLens))
	geographyNeeded := minInt(minLensRuntimeGeographyTargets, maxLensRuntimeTargetsPerLens)
	for _, item := range scored {
		if targetKindForPath(item.CanonicalPath) != lensRuntimeTargetKindGeography {
			continue
		}
		ordered = append(ordered, item)
		if len(ordered) >= geographyNeeded {
			break
		}
	}
	for _, item := range scored {
		if len(ordered) >= maxLensRuntimeTargetsPerLens {
			break
		}
		alreadyIncluded := false
		for _, existing := range ordered {
			if existing.CanonicalPath == item.CanonicalPath {
				alreadyIncluded = true
				break
			}
		}
		if alreadyIncluded {
			continue
		}
		ordered = append(ordered, item)
	}
	out := make([]*horizonmodels.WatchTarget, 0, len(ordered))
	packID := lensRuntimePackID(lens)
	for _, item := range ordered {
		frag := fragmentsByPath[item.CanonicalPath]
		target := &horizonmodels.WatchTarget{
			TenantID:       strings.TrimSpace(firstNonEmpty(lens.TenantID, "global")),
			PackID:         packID,
			Kind:           targetKindForPath(item.CanonicalPath),
			Label:          item.Label,
			CanonicalPath:  item.CanonicalPath,
			InterestTitle:  strings.TrimSpace(firstNonEmpty(lens.Name, item.Label)),
			InterestQuery:  targetInterestQuery(lens, item.Label),
			DomainModes:    compactUniqueStrings(append([]string(nil), lens.DomainModes...)),
			Roles:          []string{"monitor", "risk", "scout"},
			Priority:       clampInt(item.Score, 55, 99),
			Status:         string(horizonmodels.WatchTargetStatusActive),
			Source:         "lens_runtime",
			QueryOverrides: buildTargetQueryOverrides(lens, item.Label),
			Metadata: map[string]any{
				"lensId":        strings.TrimSpace(lens.ID),
				"lensName":      strings.TrimSpace(lens.Name),
				"originalQuery": strings.TrimSpace(lens.OriginalQuery),
				"provisionedBy": "lens_runtime",
			},
		}
		if frag != nil {
			target.FragmentID = strings.TrimSpace(frag.ID)
		}
		out = append(out, target)
		if len(out) >= maxLensRuntimeTargetsPerLens {
			break
		}
	}
	return out
}

func (s *Service) buildSourceSpecs(ctx context.Context, lens *horizonmodels.Lens, directScope []string, allPaths []string) []sourceSpec {
	if lens == nil {
		return nil
	}
	geoTerms := s.collectGeoTerms(ctx, directScope, allPaths)
	if len(geoTerms) == 0 {
		fallback := strings.TrimSpace(firstNonEmpty(lens.Name, lens.OriginalQuery))
		if fallback != "" {
			geoTerms = []geoInterest{{CanonicalPath: "", Label: fallback, Score: 40}}
		}
	}
	geoLabels := make([]string, 0, minInt(len(geoTerms), maxLensRuntimeGeoTerms))
	geoFacets := make([]string, 0, minInt(len(geoTerms), maxLensRuntimeGeoTerms))
	for _, item := range geoTerms {
		label := strings.TrimSpace(item.Label)
		if countryPath, countryLabel := countryFacetForPath(item.CanonicalPath); countryPath == item.CanonicalPath && countryLabel != "" && len(label) <= 3 {
			label = countryLabel
		}
		label = normalizeGeoQueryLabel(label)
		if label != "" && len(geoLabels) < maxLensRuntimeGeoTerms {
			geoLabels = append(geoLabels, label)
		}
		if item.CanonicalPath != "" && len(geoFacets) < maxLensRuntimeGeoTerms {
			geoFacets = append(geoFacets, item.CanonicalPath)
		}
	}
	templates := buildTemplatesForLens(lens)
	out := make([]sourceSpec, 0, len(templates))
	lensSlug := taxonomy.Slugify(strings.TrimSpace(firstNonEmpty(lens.Name, lens.ID)))
	if lensSlug == "" {
		lensSlug = "lens-runtime"
	}
	for _, tpl := range templates {
		query := buildGoogleNewsQuery(geoLabels, tpl.QueryTerms)
		if query == "" {
			continue
		}
		url := googleNewsQueryRSS(query)
		display := strings.TrimSpace(firstNonEmpty(strings.TrimSpace(lens.Name), lens.ID)) + ": " + tpl.Title
		feedType := taxonomy.Slugify(lensSlug + "-" + tpl.KeySuffix)
		if feedType == "" {
			feedType = lensSlug
		}
		id := buildSourceID("lens-runtime-" + feedType)
		out = append(out, sourceSpec{
			ID:                id,
			DisplayName:       display,
			URL:               url,
			SearchQuery:       query,
			CategoryPath:      "/source/lens-runtime/" + taxonomy.Slugify(firstNonEmpty(tpl.Category, "monitoring")),
			Category:          tpl.Category,
			FeedType:          feedType,
			SignalType:        tpl.SignalType,
			GeoFacets:         append([]string(nil), geoFacets...),
			PrimaryGeoFacet:   firstNonEmpty(firstString(geoFacets), "global"),
			SourceFormatFacet: "canvas_rss",
		})
	}
	out = append(out, buildStaticConflictFeedSpecs(geoFacets, geoLabels)...)
	return out
}

func buildStaticConflictFeedSpecs(geoFacets []string, geoLabels []string) []sourceSpec {
	if !needsMENAConflictFeeds(geoFacets, geoLabels) {
		return nil
	}
	baseFacets := compactUniqueStrings(append([]string{
		"/loc/int/strait-of-hormuz",
		"/loc/int/suez-canal",
		"/loc/int/red-sea",
		"/loc/ly",
		"/loc/eg",
		"/loc/ir",
		"/loc/kw",
		"/loc/qa",
		"/loc/sa",
		"/loc/ae",
	}, geoFacets...))
	return []sourceSpec{
		{
			ID:                buildSourceID("system-bbc-middle-east-rss"),
			DisplayName:       "System Conflict Feed: BBC Middle East",
			URL:               "http://feeds.bbci.co.uk/news/world/middle_east/rss.xml",
			CategoryPath:      "/source/lens-runtime/conflict",
			Category:          "conflict",
			FeedType:          "bbc-middle-east",
			SignalType:        "risk:regional_conflict_monitoring",
			GeoFacets:         baseFacets,
			PrimaryGeoFacet:   firstNonEmpty(firstString(baseFacets), "/loc/int/strait-of-hormuz"),
			SourceFormatFacet: "canvas_rss",
		},
		{
			ID:                buildSourceID("system-aljazeera-world-rss"),
			DisplayName:       "System Conflict Feed: Al Jazeera",
			URL:               "https://www.aljazeera.com/xml/rss/all.xml",
			CategoryPath:      "/source/lens-runtime/conflict",
			Category:          "conflict",
			FeedType:          "aljazeera-world",
			SignalType:        "risk:regional_conflict_monitoring",
			GeoFacets:         baseFacets,
			PrimaryGeoFacet:   firstNonEmpty(firstString(baseFacets), "/loc/int/strait-of-hormuz"),
			SourceFormatFacet: "canvas_rss",
		},
	}
}

func (s *Service) ensureWatchTarget(ctx context.Context, lens *horizonmodels.Lens, target *horizonmodels.WatchTarget) (string, string, error) {
	if s == nil || s.targetsRepo == nil || target == nil {
		return "", "", fmt.Errorf("watch target runtime is not configured")
	}
	existing, err := s.targetsRepo.GetByCanonicalPath(
		ctx,
		strings.TrimSpace(target.TenantID),
		strings.TrimSpace(target.UserID),
		strings.TrimSpace(target.PackID),
		strings.TrimSpace(target.Kind),
		strings.TrimSpace(target.CanonicalPath),
	)
	status := "created"
	if err == nil && existing != nil {
		status = "updated"
		target.ID = existing.ID
		target.CreatedAt = existing.CreatedAt
		if strings.TrimSpace(target.SubjectID) == "" {
			target.SubjectID = strings.TrimSpace(existing.SubjectID)
		}
		if strings.TrimSpace(target.FragmentID) == "" {
			target.FragmentID = strings.TrimSpace(existing.FragmentID)
		}
	}
	if err := s.targetsRepo.Upsert(ctx, target); err != nil {
		return "", "", fmt.Errorf("upsert watch target %s: %w", strings.TrimSpace(target.CanonicalPath), err)
	}
	verbosef("target upserted=%s status=%s", strings.TrimSpace(target.CanonicalPath), status)
	subjectID := strings.TrimSpace(target.SubjectID)
	if s.watchRuntime != nil {
		subject, ensureErr := s.watchRuntime.EnsureSubjectForTarget(ctx, target)
		if ensureErr != nil {
			return "", "", fmt.Errorf("ensure subject %s: %w", strings.TrimSpace(target.CanonicalPath), ensureErr)
		}
		if subject != nil {
			subjectID = strings.TrimSpace(subject.ID)
			target.SubjectID = subjectID
			_ = s.targetsRepo.Upsert(ctx, target)
			verbosef("target subject=%s anchor=%s", subjectID, strings.TrimSpace(target.CanonicalPath))
		}
	}
	if s.scopeAgent != nil {
		if _, ensureErr := s.scopeAgent.EnsureRuntimeForTarget(ctx, target); ensureErr != nil {
			return "", "", fmt.Errorf("ensure runtime %s: %w", strings.TrimSpace(target.CanonicalPath), ensureErr)
		}
		verbosef("target runtime ready=%s", strings.TrimSpace(target.CanonicalPath))
	}
	return status, subjectID, nil
}

func (s *Service) ensureDataSource(ctx context.Context, spec sourceSpec) (string, error) {
	if s == nil || s.dataSourcesRepo == nil {
		return "", fmt.Errorf("data source runtime is not configured")
	}
	now := time.Now().UTC()
	existing, err := s.dataSourcesRepo.FindByCanonicalSource(ctx, "", strings.TrimSpace(spec.URL))
	status := "created"
	ds := &coremodels.DataSource{
		ID:          strings.TrimSpace(spec.ID),
		DisplayName: strings.TrimSpace(spec.DisplayName),
		Kind:        coremodels.SourceKindRSS,
		Status:      "active",
		OwnerUserID: defaultLensRuntimeOwner,
		CreatedAt:   now,
		UpdatedAt:   now,
		FirstSeen:   now,
		LastSeen:    now,
		NextPollAt:  &now,
		TrustLevel:  0.82,
		SourceCategoryPaths: []string{
			strings.TrimSpace(spec.CategoryPath),
		},
		PrimarySourceCategoryPath: strPtr(strings.TrimSpace(spec.CategoryPath)),
		SourceGeoFacets:           compactUniqueStrings(spec.GeoFacets),
		SourceFormatFacets:        compactUniqueStrings([]string{strings.TrimSpace(spec.SourceFormatFacet)}),
		Details: coremodels.SourceDetails{
			RSSURL:                     strPtr(strings.TrimSpace(spec.URL)),
			SearchQuery:                strPtr(strings.TrimSpace(spec.SearchQuery)),
			OriginalSearchQuery:        strPtr(strings.TrimSpace(spec.SearchQuery)),
			Category:                   strPtr(strings.TrimSpace(spec.Category)),
			HorizonMonitorCategory:     strPtr(strings.TrimSpace(spec.Category)),
			HorizonMonitorFeedType:     strPtr(strings.TrimSpace(spec.FeedType)),
			HorizonMonitorSignalType:   strPtr(strings.TrimSpace(spec.SignalType)),
			HorizonMonitorIngestMode:   strPtr(coremodels.SourceOutputModeOntologyFact),
			HorizonMonitorRequiresAuth: boolPtr(false),
			PollIntervalMinutes:        intPtr(defaultLensRuntimePollMinutes),
			SourceScope:                strPtr(coremodels.SourceScopeGlobalSystem),
			ProcessingMode:             strPtr(coremodels.SourceProcessingModeCanvasLLM),
			OutputMode:                 strPtr(coremodels.SourceOutputModeOntologyFact),
			CadencePolicy: &coremodels.SourceCadencePolicy{
				BaseIntervalMinutes:         intPtr(defaultLensRuntimePollMinutes),
				MinIntervalMinutes:          intPtr(defaultLensRuntimePollMinutes),
				MaxIntervalMinutes:          intPtr(360),
				EmptyBackoffMultiplier:      floatPtr(2),
				ErrorBackoffMultiplier:      floatPtr(2),
				FreshnessBoostWindowMinutes: intPtr(180),
				LeaseDurationMinutes:        intPtr(90),
			},
			DisableLLMProcessing: boolPtr(false),
		},
	}
	if err == nil && existing != nil {
		status = "updated"
		ds.ID = strings.TrimSpace(firstNonEmpty(existing.ID, ds.ID))
		ds.CreatedAt = existing.CreatedAt
	}
	ds.SyncCompatForWrite()
	if err := s.dataSourcesRepo.Upsert(ctx, ds); err != nil {
		return "", fmt.Errorf("upsert source %s: %w", strings.TrimSpace(spec.DisplayName), err)
	}
	verbosef("source upserted=%s status=%s", strings.TrimSpace(spec.DisplayName), status)
	return status, nil
}

func (s *Service) lookupFragments(ctx context.Context, paths []string) map[string]*horizonmodels.HorizonFragment {
	out := make(map[string]*horizonmodels.HorizonFragment, len(paths))
	if s == nil || s.fragmentsRepo == nil || len(paths) == 0 {
		return out
	}
	fetched, err := s.fragmentsRepo.BatchGetByCanonicalPaths(ctx, compactUniqueStrings(paths))
	if err != nil {
		return out
	}
	for path, frag := range fetched {
		out[strings.TrimSpace(path)] = frag
	}
	return out
}

func (s *Service) collectGeoTerms(ctx context.Context, directScope []string, allPaths []string) []geoInterest {
	allPaths = augmentWithCountryFacetPaths(allPaths)
	fragmentsByPath := s.lookupFragments(ctx, allPaths)
	directSet := make(map[string]struct{}, len(directScope))
	for _, path := range directScope {
		path = strings.TrimSpace(path)
		if path != "" {
			directSet[path] = struct{}{}
		}
	}
	grouped := map[string]geoInterest{}
	for _, path := range allPaths {
		path = strings.TrimSpace(path)
		if sourceRelevantCanonicalPath(path) {
			score := canonicalPathWatchScore(path)
			if _, ok := directSet[path]; ok {
				score += 30
			}
			score += maritimeNeighborhoodBonus(path)
			if strings.HasPrefix(path, "/loc/") || strings.HasPrefix(path, "/zone/") {
				score += 18
			}
			if strings.HasPrefix(path, "/loc/int/") || strings.HasPrefix(path, "/zone/int/") {
				score += 8
			}
			if strings.HasPrefix(path, "/fac/") {
				score -= 12
			}
			label := labelFromCanonicalPath(path)
			if countryPath, countryLabel := countryFacetForPath(path); countryPath == path && countryLabel != "" {
				label = countryLabel
			}
			if frag := fragmentsByPath[path]; frag != nil {
				label = strings.TrimSpace(firstNonEmpty(fragmentDisplayName(frag), label))
				if countryPath, countryLabel := countryFacetForPath(path); countryPath == path && countryLabel != "" && len(strings.TrimSpace(label)) <= 3 {
					label = countryLabel
				}
				score += 5
			}
			if label != "" {
				key := strings.ToLower(strings.TrimSpace(label))
				current := grouped[key]
				if current.Label == "" || score > current.Score {
					grouped[key] = geoInterest{
						CanonicalPath: path,
						Label:         label,
						Score:         score,
					}
				}
			}
		}
		if countryPath, countryLabel := countryFacetForPath(path); countryLabel != "" {
			score := 62
			if _, ok := directSet[path]; ok {
				score += 18
			}
			score += maritimeNeighborhoodBonus(countryPath)
			key := strings.ToLower(strings.TrimSpace(countryLabel))
			current := grouped[key]
			if current.Label == "" || score > current.Score {
				grouped[key] = geoInterest{
					CanonicalPath: countryPath,
					Label:         countryLabel,
					Score:         score,
				}
			}
		}
	}
	out := make([]geoInterest, 0, len(grouped))
	for _, item := range grouped {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Label < out[j].Label
		}
		return out[i].Score > out[j].Score
	})
	if len(out) > maxLensRuntimeGeoTerms {
		out = out[:maxLensRuntimeGeoTerms]
	}
	return out
}

func buildTemplatesForLens(lens *horizonmodels.Lens) []sourceTemplate {
	queryText := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(lens.Name),
		strings.TrimSpace(lens.OriginalQuery),
		strings.Join(lens.DomainModes, " "),
	}, " ")))
	added := make(map[string]struct{}, 8)
	templates := make([]sourceTemplate, 0, 6)
	addTemplate := func(t sourceTemplate) {
		if _, ok := added[t.KeySuffix]; ok {
			return
		}
		added[t.KeySuffix] = struct{}{}
		t.QueryTerms = trimQueryTerms(t.QueryTerms)
		if len(t.QueryTerms) == 0 {
			return
		}
		templates = append(templates, t)
	}

	conflictLike := containsAny(queryText, "conflict", "war", "military", "oil", "gas", "lng", "maritime", "shipping", "hormuz", "suez")
	supplyLike := containsAny(queryText, "supply", "logistics", "transport", "port", "shipping", "customs", "export", "import", "energy", "infrastructure")
	techLike := containsAny(queryText, "chip", "semiconductor", "fab", "lithography", "electronics")
	autoLike := containsAny(queryText, "automotive", "vehicle", "battery", "ev", "auto")
	resourceLike := containsAny(queryText, "iron ore", "mining", "natural resource", "coal", "copper", "lithium", "pilbara")
	regLike := containsAny(queryText, "regulatory", "governance", "policy", "safety", "compliance", "sanction", "export control", "chips act")

	if conflictLike {
		addTemplate(sourceTemplate{
			KeySuffix:  "strike-geography",
			Title:      "Conflict and Strike Geography",
			Category:   "conflict",
			SignalType: "risk:regional_strike_geography",
			QueryTerms: []string{"strike", "attack", "missile", "drone", "bombing", "blast", "military", "intercept", "war"},
		})
	}
	if conflictLike || supplyLike {
		addTemplate(sourceTemplate{
			KeySuffix:  "energy-sites",
			Title:      "Energy and Infrastructure Sites",
			Category:   "critical_infrastructure",
			SignalType: "risk:critical_infrastructure_disruption",
			QueryTerms: []string{"refinery", "oil", "gas", "lng", "terminal", "pipeline", "port", "power plant", "substation", "outage", "damage"},
		})
		addTemplate(sourceTemplate{
			KeySuffix:  "logistics-cascade",
			Title:      "Logistics and Route Cascade",
			Category:   "supply_chain",
			SignalType: "risk:logistics_disruption",
			QueryTerms: []string{"shipping", "tanker", "vessel", "container", "port", "rail", "customs", "corridor", "canal", "strait", "rerouting", "closure"},
		})
	}
	if techLike {
		addTemplate(sourceTemplate{
			KeySuffix:  "semiconductor-policy",
			Title:      "Semiconductor Policy and Capacity",
			Category:   "technology",
			SignalType: "risk:technology_supply_disruption",
			QueryTerms: []string{"semiconductor", "chip", "fab", "wafer", "lithography", "export control", "subsidy", "chips act", "capacity"},
		})
		addTemplate(sourceTemplate{
			KeySuffix:  "semiconductor-inputs",
			Title:      "Semiconductor Inputs and Equipment",
			Category:   "technology",
			SignalType: "risk:industrial_input_disruption",
			QueryTerms: []string{"neon", "photoresist", "gallium", "germanium", "packaging", "equipment", "tooling", "supply chain"},
		})
	}
	if autoLike {
		addTemplate(sourceTemplate{
			KeySuffix:  "automotive-ops",
			Title:      "Automotive Operations and Suppliers",
			Category:   "automotive",
			SignalType: "risk:industrial_supply_disruption",
			QueryTerms: []string{"automotive", "vehicle", "ev", "battery", "assembly", "supplier", "plant", "logistics", "shutdown"},
		})
	}
	if resourceLike {
		addTemplate(sourceTemplate{
			KeySuffix:  "resource-output",
			Title:      "Natural Resources Output and Exports",
			Category:   "natural_resources",
			SignalType: "risk:commodity_supply_disruption",
			QueryTerms: []string{"iron ore", "mining", "mine", "port", "rail", "shipment", "export", "terminal", "output"},
		})
	}
	if regLike || (!techLike && !autoLike && !resourceLike) {
		addTemplate(sourceTemplate{
			KeySuffix:  "policy-regulatory",
			Title:      "Policy, Regulation, and Market Controls",
			Category:   "regulatory",
			SignalType: "risk:policy_regulatory_shift",
			QueryTerms: []string{"regulation", "policy", "sanction", "export control", "compliance", "recall", "product safety", "customs"},
		})
	}
	if len(templates) == 0 {
		addTemplate(sourceTemplate{
			KeySuffix:  "general-monitoring",
			Title:      "General Monitoring",
			Category:   "monitoring",
			SignalType: "risk:external_monitoring",
			QueryTerms: []string{"disruption", "closure", "incident", "policy", "export", "port", "facility"},
		})
	}
	return templates
}

func buildGoogleNewsQuery(geoLabels []string, terms []string) string {
	for i := range geoLabels {
		geoLabels[i] = normalizeGeoQueryLabel(geoLabels[i])
	}
	geoLabels = trimQueryTerms(geoLabels)
	terms = trimQueryTerms(terms)
	if len(geoLabels) == 0 && len(terms) == 0 {
		return ""
	}
	parts := make([]string, 0, 2)
	if len(geoLabels) > 0 {
		parts = append(parts, "("+strings.Join(geoLabels, " OR ")+")")
	}
	if len(terms) > 0 {
		parts = append(parts, "("+strings.Join(terms, " OR ")+")")
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func buildTargetQueryOverrides(lens *horizonmodels.Lens, label string) []string {
	base := strings.TrimSpace(label)
	if base == "" {
		base = strings.TrimSpace(lens.Name)
	}
	queries := []string{
		base,
		base + " disruption",
		base + " supply chain",
	}
	if containsAny(strings.ToLower(strings.Join(lens.DomainModes, " ")), "conflict", "energy", "maritime") {
		queries = append(queries, base+" attack", base+" logistics")
	}
	return trimQueryTerms(queries)
}

func lensRuntimePackID(lens *horizonmodels.Lens) string {
	if lens == nil {
		return ""
	}
	return "lens-runtime-" + strings.TrimSpace(lens.ID)
}

func buildSourceID(seed string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(seed)))
	return "lensrt_" + hex.EncodeToString(sum[:8])
}

func googleNewsQueryRSS(query string) string {
	return "https://news.google.com/rss/search?q=" + neturl.QueryEscape(strings.TrimSpace(query)) + "&hl=en-US&gl=US&ceid=US:en"
}

func targetInterestQuery(lens *horizonmodels.Lens, label string) string {
	parts := []string{strings.TrimSpace(label)}
	if q := strings.TrimSpace(lens.OriginalQuery); q != "" {
		parts = append(parts, q)
	}
	if len(lens.DomainModes) > 0 {
		parts = append(parts, strings.Join(compactUniqueStrings(lens.DomainModes), ", "))
	}
	return strings.TrimSpace(strings.Join(parts, " | "))
}

func buildSourceIDSeed(displayName, url string) string {
	return strings.TrimSpace(displayName) + "|" + strings.TrimSpace(url)
}

func fragmentDisplayName(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if label := strings.TrimSpace(fragment.Labels["en"]); label != "" {
		return label
	}
	if label := strings.TrimSpace(fragment.Description); label != "" {
		return label
	}
	return labelFromCanonicalPath(fragment.CanonicalPath)
}

func labelFromCanonicalPath(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	last = strings.ReplaceAll(last, "-", " ")
	last = strings.ReplaceAll(last, "_", " ")
	return strings.Title(strings.TrimSpace(last))
}

func targetKindForPath(path string) string {
	switch {
	case strings.HasPrefix(strings.TrimSpace(path), "/loc/"), strings.HasPrefix(strings.TrimSpace(path), "/zone/"):
		return lensRuntimeTargetKindGeography
	default:
		return lensRuntimeTargetKindFragment
	}
}

func watchableCanonicalPath(path string) bool {
	path = strings.TrimSpace(path)
	switch {
	case strings.HasPrefix(path, "/loc/"),
		strings.HasPrefix(path, "/zone/"),
		strings.HasPrefix(path, "/fac/"),
		strings.HasPrefix(path, "/org/"),
		strings.HasPrefix(path, "/market/"),
		strings.HasPrefix(path, "/product/"):
		return true
	default:
		return false
	}
}

func sourceRelevantCanonicalPath(path string) bool {
	path = strings.TrimSpace(path)
	switch {
	case strings.HasPrefix(path, "/loc/"),
		strings.HasPrefix(path, "/zone/"),
		strings.HasPrefix(path, "/fac/"):
		return true
	default:
		return false
	}
}

func canonicalPathWatchScore(path string) int {
	path = strings.TrimSpace(path)
	switch {
	case strings.HasPrefix(path, "/loc/int/"):
		return 96
	case strings.HasPrefix(path, "/zone/int/"):
		return 92
	case strings.HasPrefix(path, "/fac/"):
		return 89
	case strings.HasPrefix(path, "/loc/"):
		return 85
	case strings.HasPrefix(path, "/zone/"):
		return 78
	case strings.HasPrefix(path, "/org/"):
		return 74
	case strings.HasPrefix(path, "/product/"):
		return 68
	default:
		return 60
	}
}

func expandMaritimeNeighborhoodRefs(paths []string) []string {
	out := append([]string(nil), paths...)
	for _, path := range paths {
		normalized := normalizePath(path)
		switch {
		case strings.Contains(normalized, "hormuz"):
			out = append(out, "/loc/int/strait-of-hormuz", "/zone/int/military_conflict/strait-of-hormuz")
			out = append(out, "/loc/ir", "/loc/qa", "/loc/kw", "/loc/ae", "/loc/om", "/loc/sa")
		case strings.Contains(normalized, "suez"):
			out = append(out, "/loc/int/suez-canal", "/zone/int/military_conflict/suez-canal")
			out = append(out, "/loc/eg", "/loc/ly", "/loc/sa", "/loc/il", "/loc/jo", "/loc/sd")
		case strings.Contains(normalized, "red-sea"), strings.Contains(normalized, "bab-el-mandeb"):
			out = append(out, "/loc/int/red-sea", "/zone/int/military_conflict/red-sea")
			out = append(out, "/loc/ye", "/loc/dj", "/loc/er", "/loc/sa", "/loc/eg", "/loc/sd", "/loc/ly")
		case strings.Contains(normalized, "black-sea"):
			out = append(out, "/loc/int/black-sea")
			out = append(out, "/loc/ua", "/loc/ru", "/loc/tr", "/loc/ro", "/loc/bg", "/loc/ge")
		}
	}
	return out
}

func augmentWithCountryFacetPaths(paths []string) []string {
	out := append([]string(nil), paths...)
	for _, path := range paths {
		countryPath, _ := countryFacetForPath(path)
		if countryPath != "" {
			out = append(out, countryPath)
		}
	}
	return compactUniqueStrings(out)
}

func maritimeNeighborhoodBonus(path string) int {
	switch normalizePath(path) {
	case "/loc/int/strait-of-hormuz",
		"/zone/int/military_conflict/strait-of-hormuz",
		"/loc/int/suez-canal",
		"/zone/int/military_conflict/suez-canal",
		"/loc/int/red-sea",
		"/zone/int/military_conflict/red-sea":
		return 22
	case "/loc/ir", "/loc/qa", "/loc/kw", "/loc/ae", "/loc/om", "/loc/sa",
		"/loc/eg", "/loc/ye", "/loc/dj", "/loc/er", "/loc/sd":
		return 16
	case "/loc/ly":
		return 24
	default:
		return 0
	}
}

func normalizeGeoQueryLabel(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "ae":
		return "United Arab Emirates"
	case "bh":
		return "Bahrain"
	case "dj":
		return "Djibouti"
	case "eg":
		return "Egypt"
	case "er":
		return "Eritrea"
	case "ir":
		return "Iran"
	case "kw":
		return "Kuwait"
	case "ly":
		return "Libya"
	case "om":
		return "Oman"
	case "qa":
		return "Qatar"
	case "sa":
		return "Saudi Arabia"
	case "sd":
		return "Sudan"
	case "ye":
		return "Yemen"
	default:
		return strings.TrimSpace(label)
	}
}

func needsMENAConflictFeeds(geoFacets []string, geoLabels []string) bool {
	for _, facet := range geoFacets {
		switch normalizePath(facet) {
		case "/loc/int/strait-of-hormuz",
			"/zone/int/military_conflict/strait-of-hormuz",
			"/loc/int/suez-canal",
			"/zone/int/military_conflict/suez-canal",
			"/loc/int/red-sea",
			"/zone/int/military_conflict/red-sea",
			"/loc/ly",
			"/loc/eg",
			"/loc/ir",
			"/loc/kw",
			"/loc/qa",
			"/loc/sa",
			"/loc/ae",
			"/loc/om",
			"/loc/bh":
			return true
		}
	}
	for _, label := range geoLabels {
		if containsAny(strings.ToLower(strings.TrimSpace(label)),
			"hormuz", "suez", "red sea", "libya", "egypt", "iran", "kuwait", "qatar", "oman", "saudi", "emirates", "bahrain") {
			return true
		}
	}
	return false
}

func (s *Service) lookupMaterializedArtifact(ctx context.Context, tenantID, canonicalKey string) (*horizonmodels.MaterializedArtifact, error) {
	if s == nil || s.materializedRepo == nil {
		return nil, nil
	}
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	canonicalKey = normalizedArtifactCanonicalKey(canonicalKey)
	if canonicalKey == "" {
		return nil, nil
	}
	artifact, err := s.materializedRepo.GetByCanonicalKeyAnyKind(ctx, tenantID, canonicalKey)
	if err == nil && artifact != nil {
		return artifact, nil
	}
	if tenantID != "global" {
		if fallback, fallbackErr := s.materializedRepo.GetByCanonicalKeyAnyKind(ctx, "global", canonicalKey); fallbackErr == nil && fallback != nil {
			return fallback, nil
		}
	}
	return nil, err
}

func collectMaterializedArtifactRefs(artifact *horizonmodels.MaterializedArtifact) []string {
	if artifact == nil {
		return nil
	}
	refs := []string{
		strings.TrimSpace(artifact.AnchorSupplier),
		strings.TrimSpace(artifact.AnchorCustomer),
	}
	refs = append(refs, artifact.NodeRefs...)
	refs = append(refs, artifact.GeoRefs...)
	refs = append(refs, artifact.FlowRefs...)
	refs = append(refs, artifact.ZoneRefs...)
	refs = append(refs, artifact.AffectedZoneRefs...)

	switch artifact.Kind {
	case horizonmodels.MaterializedKindSupplyChainPath, horizonmodels.MaterializedKindServiceChainPath:
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			return compactUniqueStrings(refs)
		}
		for _, leg := range parsed.Legs {
			refs = append(refs,
				strings.TrimSpace(leg.SupplierRef),
				strings.TrimSpace(leg.CustomerRef),
				strings.TrimSpace(leg.ProductRef),
				strings.TrimSpace(leg.SupplyLinkCanonicalPath),
			)
			refs = append(refs, leg.TransportViaRefs...)
			refs = append(refs, leg.GeoRefs...)
			refs = append(refs, leg.FlowRefs...)
			refs = append(refs, leg.AlternateFlowRefs...)
			refs = append(refs, leg.ZoneRefs...)
			refs = append(refs, leg.AffectedZoneRefs...)
			for _, overlay := range leg.RouteOverlays {
				refs = append(refs, overlay.OrderedRefs...)
				refs = append(refs, overlay.TransportViaRefs...)
				refs = append(refs, overlay.FlowRefs...)
				refs = append(refs, overlay.AlternateFlowRefs...)
				refs = append(refs, overlay.GeoRefs...)
				refs = append(refs, overlay.ZoneRefs...)
				refs = append(refs, overlay.AffectedZoneRefs...)
			}
		}
	}

	return compactUniqueStrings(refs)
}

func normalizedArtifactCanonicalKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/artifact/") {
		return "/" + strings.Trim(strings.TrimPrefix(raw, "/artifact/"), "/")
	}
	if strings.HasPrefix(raw, "/") {
		return raw
	}
	return "/" + strings.Trim(raw, "/")
}

func normalizePath(path string) string {
	path = strings.ToLower(strings.TrimSpace(path))
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
}

func countryFacetForPath(path string) (string, string) {
	code := countryCodeFromPath(path)
	if code == "" || code == "global" || code == "int" {
		return "", ""
	}
	return "/loc/" + code, countryCodeLabel(code)
}

func countryCodeFromPath(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "org", "gov", "loc", "market", "community", "fac", "zone":
		return strings.ToLower(strings.TrimSpace(parts[1]))
	case "track":
		if len(parts) >= 3 {
			return strings.ToLower(strings.TrimSpace(parts[2]))
		}
	}
	return ""
}

func countryCodeLabel(code string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "ae":
		return "United Arab Emirates"
	case "au":
		return "Australia"
	case "be":
		return "Belgium"
	case "bh":
		return "Bahrain"
	case "cn":
		return "China"
	case "de":
		return "Germany"
	case "eg":
		return "Egypt"
	case "eu":
		return "European Union"
	case "gb", "uk":
		return "United Kingdom"
	case "in":
		return "India"
	case "ir":
		return "Iran"
	case "it":
		return "Italy"
	case "kw":
		return "Kuwait"
	case "kz":
		return "Kazakhstan"
	case "ly":
		return "Libya"
	case "nl":
		return "Netherlands"
	case "om":
		return "Oman"
	case "qa":
		return "Qatar"
	case "ru":
		return "Russia"
	case "sa":
		return "Saudi Arabia"
	case "tr":
		return "Turkey"
	case "ua":
		return "Ukraine"
	case "us":
		return "United States"
	default:
		return strings.ToUpper(code)
	}
}

func dedupeLenses(in []*horizonmodels.Lens) []*horizonmodels.Lens {
	seen := map[string]struct{}{}
	out := make([]*horizonmodels.Lens, 0, len(in))
	for _, lens := range in {
		if lens == nil || strings.TrimSpace(lens.ID) == "" {
			continue
		}
		if _, ok := seen[strings.TrimSpace(lens.ID)]; ok {
			continue
		}
		seen[strings.TrimSpace(lens.ID)] = struct{}{}
		out = append(out, lens)
	}
	return out
}

func compactUniqueStrings(values []string) []string {
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
	return out
}

func trimQueryTerms(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len(value) > 80 {
			value = strings.TrimSpace(value[:80])
		}
		if _, ok := seen[strings.ToLower(value)]; ok {
			continue
		}
		seen[strings.ToLower(value)] = struct{}{}
		out = append(out, value)
		if len(out) >= maxLensRuntimeQueryTerms {
			break
		}
	}
	return out
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(text, strings.ToLower(strings.TrimSpace(needle))) {
			return true
		}
	}
	return false
}

func clampInt(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func strPtr(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	out := strings.TrimSpace(v)
	return &out
}

func boolPtr(v bool) *bool {
	return &v
}

func intPtr(v int) *int {
	return &v
}

func floatPtr(v float64) *float64 {
	return &v
}

func verbosef(format string, args ...any) {
	if strings.TrimSpace(os.Getenv("HORIZON_LENS_RUNTIME_VERBOSE")) == "" {
		return
	}
	log.Printf("lensruntime: "+format, args...)
}
