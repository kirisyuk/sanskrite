package recommendations

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

const (
	strategicEnrichmentJobKind     = "strategic_enrichment"
	strategicEnrichmentStageTarget = "targeted"
	strategicEnrichmentStageDeep   = "deep_research"
)

type Logger interface {
	Printf(format string, v ...interface{})
}

type DetectionInput struct {
	TenantID             string
	ProjectID            string
	LensID               string
	SourceID             string
	EventTitle           string
	EventSummary         string
	SubjectCanonicalPath string
	SubjectType          horizonmodels.StrategicRecommendationSubjectType
	SignalAxis           string
	BusinessEndpoints    int
	RemoteEndpoints      int
	PathFamilies         int
	OccurredAt           time.Time
}

type subjectSnapshot struct {
	subject       *horizonmodels.HorizonFragment
	directTargets []*horizonmodels.HorizonFragment
	nodes         []*horizonmodels.HorizonFragment
	paths         []string
	labels        []string
	anchorPaths   []string
	anchorLabels  []string
}

type llmAdjudication struct {
	ShouldCreate       bool     `json:"shouldCreate"`
	Confidence         float64  `json:"confidence"`
	Headline           string   `json:"headline"`
	Reason             string   `json:"reason"`
	DomainContractHint string   `json:"domainContractHint"`
	MissingFamilies    []string `json:"missingFamilies"`
}

type Service struct {
	repo             *horizonrepo.StrategicRecommendationsRepository
	fragmentsRepo    *horizonrepo.HorizonFragmentsRepository
	edgesRepo        *horizonrepo.DependencyEdgesRepository
	scoutingJobsRepo *horizonrepo.ScoutingJobsRepository
	exec             *ai.AITaskExecutor
	realtime         *realtime.Publisher
	logger           Logger
}

func NewService(
	repo *horizonrepo.StrategicRecommendationsRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	edgesRepo *horizonrepo.DependencyEdgesRepository,
	scoutingJobsRepo *horizonrepo.ScoutingJobsRepository,
	exec *ai.AITaskExecutor,
	realtimePub *realtime.Publisher,
	logger Logger,
) *Service {
	return &Service{
		repo:             repo,
		fragmentsRepo:    fragmentsRepo,
		edgesRepo:        edgesRepo,
		scoutingJobsRepo: scoutingJobsRepo,
		exec:             exec,
		realtime:         realtimePub,
		logger:           logger,
	}
}

func (s *Service) List(ctx context.Context, tenantID string, filter horizonrepo.StrategicRecommendationsListFilter) ([]*horizonmodels.StrategicRecommendation, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	return s.repo.ListByTenant(ctx, tenantID, filter)
}

func (s *Service) CountPending(ctx context.Context, tenantID string) (int, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	return s.repo.CountPendingByTenant(ctx, tenantID)
}

func (s *Service) Get(ctx context.Context, id string) (*horizonmodels.StrategicRecommendation, error) {
	if s == nil || s.repo == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) Detect(ctx context.Context, input DetectionInput) (*horizonmodels.StrategicRecommendation, bool, error) {
	if s == nil || s.repo == nil || s.fragmentsRepo == nil || s.edgesRepo == nil {
		return nil, false, nil
	}
	rec, err := s.buildRecommendationCandidate(ctx, input)
	if err != nil || rec == nil {
		return nil, false, err
	}
	createdRec, created, err := s.repo.CreateOrRefresh(ctx, rec)
	if err != nil {
		return nil, false, err
	}
	if createdRec != nil {
		eventType := "horizon.recommendation.updated"
		if created {
			eventType = "horizon.recommendation.created"
		}
		s.publish(ctx, input.TenantID, eventType, createdRec)
	}
	return createdRec, created, nil
}

func (s *Service) Reconcile(ctx context.Context, id string) (*horizonmodels.StrategicRecommendation, bool, error) {
	if s == nil || s.repo == nil {
		return nil, false, horizonrepo.ErrInvalidDocument
	}
	rec, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if rec == nil {
		return nil, false, nil
	}
	input := DetectionInput{
		TenantID:             strings.TrimSpace(rec.TenantID),
		ProjectID:            strings.TrimSpace(rec.ProjectID),
		LensID:               strings.TrimSpace(rec.LensID),
		SubjectCanonicalPath: strings.TrimSpace(rec.SubjectCanonicalPath),
		SubjectType:          rec.SubjectType,
	}
	candidate, err := s.buildRecommendationCandidate(ctx, input)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	if candidate == nil {
		updated, updateErr := s.repo.UpdateStatus(ctx, rec.ID, func(existing *horizonmodels.StrategicRecommendation) error {
			existing.Status = horizonmodels.StrategicRecommendationStatusStale
			existing.Execution.LastEvaluatedAt = &now
			existing.Execution.ResultSummary = "Recommendation no longer qualifies under current strategic anchor coverage rules."
			existing.Execution.GapStillOpen = false
			return nil
		})
		if updateErr != nil {
			return nil, false, updateErr
		}
		s.publish(ctx, rec.TenantID, "horizon.recommendation.updated", updated)
		return updated, false, nil
	}
	if candidate.DedupeKey != rec.DedupeKey {
		candidate.SupersedesID = rec.ID
		refreshed, _, createErr := s.repo.CreateOrRefresh(ctx, candidate)
		if createErr != nil {
			return nil, false, createErr
		}
		_, updateErr := s.repo.UpdateStatus(ctx, rec.ID, func(existing *horizonmodels.StrategicRecommendation) error {
			existing.Status = horizonmodels.StrategicRecommendationStatusStale
			existing.SupersedesID = strings.TrimSpace(refreshed.ID)
			existing.Execution.LastEvaluatedAt = &now
			existing.Execution.ResultSummary = "Recommendation superseded after strategic gap recalculation."
			existing.Execution.GapStillOpen = false
			return nil
		})
		if updateErr != nil {
			return nil, false, updateErr
		}
		s.publish(ctx, rec.TenantID, "horizon.recommendation.updated", refreshed)
		return refreshed, true, nil
	}
	refreshed, _, err := s.repo.CreateOrRefresh(ctx, candidate)
	if err != nil {
		return nil, false, err
	}
	s.publish(ctx, rec.TenantID, "horizon.recommendation.updated", refreshed)
	return refreshed, false, nil
}

func (s *Service) ReconcileTenant(ctx context.Context, tenantID string, limit int) (int, int, error) {
	if s == nil || s.repo == nil {
		return 0, 0, horizonrepo.ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 25
	}
	statuses := []string{
		string(horizonmodels.StrategicRecommendationStatusPending),
		string(horizonmodels.StrategicRecommendationStatusApproved),
		string(horizonmodels.StrategicRecommendationStatusExecuting),
	}
	items := make([]*horizonmodels.StrategicRecommendation, 0, limit)
	seen := make(map[string]struct{})
	for _, status := range statuses {
		remaining := limit - len(items)
		if remaining <= 0 {
			break
		}
		group, err := s.repo.ListByTenant(ctx, strings.TrimSpace(tenantID), horizonrepo.StrategicRecommendationsListFilter{
			Status: status,
			Limit:  remaining,
		})
		if err != nil {
			return 0, 0, err
		}
		for _, item := range group {
			if item == nil || strings.TrimSpace(item.ID) == "" {
				continue
			}
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			items = append(items, item)
		}
	}
	reconciled := 0
	stale := 0
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.ID) == "" {
			continue
		}
		updated, _, reconcileErr := s.Reconcile(ctx, item.ID)
		if reconcileErr != nil {
			return reconciled, stale, reconcileErr
		}
		reconciled++
		if updated != nil && updated.Status == horizonmodels.StrategicRecommendationStatusStale {
			stale++
		}
	}
	return reconciled, stale, nil
}

func (s *Service) buildRecommendationCandidate(ctx context.Context, input DetectionInput) (*horizonmodels.StrategicRecommendation, error) {
	input.TenantID = strings.TrimSpace(input.TenantID)
	if input.TenantID == "" {
		input.TenantID = "global"
	}
	input.SubjectCanonicalPath = strings.TrimSpace(input.SubjectCanonicalPath)
	if input.SubjectCanonicalPath == "" {
		return nil, nil
	}
	snapshot, err := s.loadSubjectSnapshot(ctx, input.TenantID, input.SubjectCanonicalPath)
	if err != nil || snapshot == nil || snapshot.subject == nil {
		return nil, err
	}
	profile := matchStrategicCoverageProfile(snapshot)
	if profile == nil {
		return nil, nil
	}
	if shouldSuppressBroadSubjectRecommendation(profile, snapshot) {
		return nil, nil
	}
	stats, missingFamilies := evaluateCoverageProfile(profile, snapshot, input)
	if len(missingFamilies) == 0 {
		return nil, nil
	}
	if stats.MatchedFamilies >= profile.MinMatchedFamilies &&
		input.RemoteEndpoints >= profile.MinRemoteEndpoints &&
		input.BusinessEndpoints > 0 {
		return nil, nil
	}
	confidence := computeRecommendationConfidence(snapshot.subject, profile, stats, input)
	headline := profile.HeadlineTemplate
	reason := profile.ReasonTemplate
	domainContractHint := profile.DomainContractHint
	if s.exec != nil && confidence >= 0.45 && confidence <= 0.72 {
		if llm, ok := s.maybeAdjudicateGapWithLLM(ctx, snapshot, profile, missingFamilies, confidence); ok {
			if !llm.ShouldCreate {
				return nil, nil
			}
			if llm.Confidence > 0 {
				confidence = llm.Confidence
			}
			if strings.TrimSpace(llm.Headline) != "" {
				headline = llm.Headline
			}
			if strings.TrimSpace(llm.Reason) != "" {
				reason = llm.Reason
			}
			if strings.TrimSpace(llm.DomainContractHint) != "" {
				domainContractHint = llm.DomainContractHint
			}
			if len(llm.MissingFamilies) > 0 {
				missingFamilies = horizonrepo.NormalizeStrategicRecommendationTargetFamilies(llm.MissingFamilies)
			}
		}
	}
	rec := &horizonmodels.StrategicRecommendation{
		TenantID:               input.TenantID,
		ProjectID:              strings.TrimSpace(input.ProjectID),
		LensID:                 strings.TrimSpace(input.LensID),
		SubjectType:            input.SubjectType,
		SubjectCanonicalPath:   input.SubjectCanonicalPath,
		RecommendationType:     horizonmodels.StrategicRecommendationTypeStrategicGap,
		Status:                 horizonmodels.StrategicRecommendationStatusPending,
		Priority:               recommendationPriority(profile, snapshot.subject),
		Confidence:             confidence,
		Headline:               headline,
		Reason:                 reason,
		Evidence:               buildEvidence(snapshot, input, stats),
		ExpectedMissingTargets: missingFamilies,
		CurrentCoverageStats:   stats,
		ProposedAction:         horizonmodels.StrategicRecommendationActionStagedEnrich,
		DomainContractHint:     domainContractHint,
	}
	rec.DedupeKey = horizonrepo.BuildStrategicRecommendationDedupeKey(rec.TenantID, rec.SubjectCanonicalPath, rec.RecommendationType, rec.ExpectedMissingTargets)
	rec.ID = horizonrepo.StrategicRecommendationID(rec.DedupeKey)
	return rec, nil
}

func (s *Service) Dismiss(ctx context.Context, tenantID, id string) (*horizonmodels.StrategicRecommendation, error) {
	if s == nil || s.repo == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	rec, err := s.repo.UpdateStatus(ctx, id, func(rec *horizonmodels.StrategicRecommendation) error {
		if rec == nil {
			return nil
		}
		rec.Status = horizonmodels.StrategicRecommendationStatusDismissed
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, tenantID, "horizon.recommendation.updated", rec)
	return rec, nil
}

func (s *Service) Approve(ctx context.Context, tenantID, userID, id string) (*horizonmodels.StrategicRecommendation, error) {
	if s == nil || s.repo == nil || s.scoutingJobsRepo == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	rec, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	job, err := s.createStrategicEnrichmentJob(ctx, rec, userID, strategicEnrichmentStageTarget, horizonmodels.ScoutingStrategySnapshot)
	if err != nil {
		return nil, err
	}
	updated, err := s.repo.UpdateStatus(ctx, id, func(rec *horizonmodels.StrategicRecommendation) error {
		rec.Status = horizonmodels.StrategicRecommendationStatusExecuting
		now := time.Now().UTC()
		rec.Execution.JobID = strings.TrimSpace(job.ID)
		rec.Execution.JobKind = strategicEnrichmentJobKind
		rec.Execution.JobStage = strategicEnrichmentStageTarget
		rec.Execution.LastEvaluatedAt = &now
		rec.Execution.GapStillOpen = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, tenantID, "horizon.recommendation.executing", updated)
	return updated, nil
}

func (s *Service) HandleScoutingJobComplete(ctx context.Context, job *horizonmodels.ScoutingJob) error {
	if s == nil || s.repo == nil || s.scoutingJobsRepo == nil || job == nil || job.State == nil {
		return nil
	}
	if strings.TrimSpace(asString(job.State["jobKind"])) != strategicEnrichmentJobKind {
		return nil
	}
	recID := strings.TrimSpace(asString(job.State["strategicRecommendationId"]))
	if recID == "" {
		return nil
	}
	rec, err := s.repo.Get(ctx, recID)
	if err != nil {
		return err
	}
	if rec == nil {
		return nil
	}
	stage := strings.TrimSpace(asString(job.State["strategicStage"]))
	input := DetectionInput{
		TenantID:             firstNonEmptyString(strings.TrimSpace(job.TenantID), rec.TenantID),
		ProjectID:            firstNonEmptyString(strings.TrimSpace(job.ProjectID), rec.ProjectID),
		LensID:               firstNonEmptyString(strings.TrimSpace(job.LensID), rec.LensID),
		SubjectCanonicalPath: rec.SubjectCanonicalPath,
		SubjectType:          rec.SubjectType,
	}
	newRec, _, detectErr := s.Detect(ctx, input)
	if detectErr != nil {
		return detectErr
	}
	now := time.Now().UTC()
	if newRec == nil || newRec.ID != rec.ID {
		updated, err := s.repo.UpdateStatus(ctx, rec.ID, func(rec *horizonmodels.StrategicRecommendation) error {
			rec.Status = horizonmodels.StrategicRecommendationStatusCompleted
			rec.Execution.JobID = strings.TrimSpace(job.ID)
			rec.Execution.JobKind = strategicEnrichmentJobKind
			rec.Execution.JobStage = stage
			rec.Execution.LastCompletedAt = &now
			rec.Execution.LastEvaluatedAt = &now
			rec.Execution.ResultSummary = "Coverage gap closed after enrichment."
			rec.Execution.GapStillOpen = false
			return nil
		})
		if err != nil {
			return err
		}
		s.publish(ctx, rec.TenantID, "horizon.recommendation.completed", updated)
		return nil
	}

	if stage == strategicEnrichmentStageTarget {
		deepJob, err := s.createStrategicEnrichmentJob(ctx, rec, strings.TrimSpace(job.UserID), strategicEnrichmentStageDeep, horizonmodels.ScoutingStrategyDeepDive)
		if err != nil {
			_, _ = s.repo.UpdateStatus(ctx, rec.ID, func(rec *horizonmodels.StrategicRecommendation) error {
				rec.Status = horizonmodels.StrategicRecommendationStatusFailed
				rec.Execution.JobID = strings.TrimSpace(job.ID)
				rec.Execution.JobStage = stage
				rec.Execution.LastCompletedAt = &now
				rec.Execution.LastEvaluatedAt = &now
				rec.Execution.ResultSummary = "Targeted enrichment finished, but deep-research escalation failed to start."
				rec.Execution.GapStillOpen = true
				return nil
			})
			return err
		}
		updated, err := s.repo.UpdateStatus(ctx, rec.ID, func(rec *horizonmodels.StrategicRecommendation) error {
			rec.Status = horizonmodels.StrategicRecommendationStatusExecuting
			rec.Execution.JobID = strings.TrimSpace(deepJob.ID)
			rec.Execution.JobKind = strategicEnrichmentJobKind
			rec.Execution.JobStage = strategicEnrichmentStageDeep
			rec.Execution.LastCompletedAt = &now
			rec.Execution.LastEvaluatedAt = &now
			rec.Execution.ResultSummary = "Targeted enrichment completed; escalating to deeper research."
			rec.Execution.GapStillOpen = true
			rec.Execution.EscalatedDeepDive = true
			return nil
		})
		if err != nil {
			return err
		}
		s.publish(ctx, rec.TenantID, "horizon.recommendation.executing", updated)
		return nil
	}

	updated, err := s.repo.UpdateStatus(ctx, rec.ID, func(rec *horizonmodels.StrategicRecommendation) error {
		rec.Status = horizonmodels.StrategicRecommendationStatusCompleted
		rec.Execution.JobID = strings.TrimSpace(job.ID)
		rec.Execution.JobKind = strategicEnrichmentJobKind
		rec.Execution.JobStage = stage
		rec.Execution.LastCompletedAt = &now
		rec.Execution.LastEvaluatedAt = &now
		rec.Execution.ResultSummary = "Deep research completed but the strategic gap remains open."
		rec.Execution.GapStillOpen = true
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, rec.TenantID, "horizon.recommendation.completed", updated)
	return nil
}

func (s *Service) createStrategicEnrichmentJob(ctx context.Context, rec *horizonmodels.StrategicRecommendation, userID, stage string, strategy horizonmodels.ScoutingStrategy) (*horizonmodels.ScoutingJob, error) {
	if rec == nil {
		return nil, fmt.Errorf("recommendation is required")
	}
	now := time.Now().UTC()
	effectiveProjectID := strings.TrimSpace(rec.ProjectID)
	if effectiveProjectID == "" {
		effectiveProjectID = "tenant-global"
	}
	domainHint := strings.TrimSpace(rec.DomainContractHint)
	missingFamilies := append([]string(nil), rec.ExpectedMissingTargets...)
	anchorTerms := buildStrategicAnchorTerms(rec)
	state := map[string]interface{}{
		"jobKind":                     strategicEnrichmentJobKind,
		"strategicRecommendationId":   strings.TrimSpace(rec.ID),
		"strategicStage":              strings.TrimSpace(stage),
		"expectedMissingTargets":      missingFamilies,
		"strategicGapReason":          strings.TrimSpace(rec.Reason),
		"strategicCoverageProfile":    strings.TrimSpace(rec.CurrentCoverageStats.ProfileID),
		"strategicDomainContractHint": domainHint,
		"strategicAnchorTerms":        anchorTerms,
		"strategicSubjectType":        strings.TrimSpace(string(rec.SubjectType)),
		"originalQuery":               strings.TrimSpace(rec.Headline),
		"focus":                       strategicFocus(rec),
		"keywords":                    append([]string{strings.TrimSpace(rec.SubjectCanonicalPath)}, anchorTerms...),
		"targetCanonicalPath":         strings.TrimSpace(rec.SubjectCanonicalPath),
		"scoutingStrategy":            string(strategy),
		"lensId":                      strings.TrimSpace(rec.LensID),
		"currentState":                string(horizonmodels.ScoutingJobStatePending),
		"strategicOriginalProjectId":  strings.TrimSpace(rec.ProjectID),
		"strategicProjectScope":       "tenant_global",
	}
	if strings.TrimSpace(rec.ProjectID) != "" {
		state["strategicProjectScope"] = "project"
	}
	if domainHint != "" {
		state["domains"] = []map[string]interface{}{
			{
				"canonicalPath": domainHint,
				"name":          strings.TrimSpace(rec.CurrentCoverageStats.ProfileID),
				"confidence":    rec.Confidence,
			},
		}
	}
	job := &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			TenantID:  strings.TrimSpace(rec.TenantID),
			ProjectID: effectiveProjectID,
			UserID:    strings.TrimSpace(userID),
			AgentID:   "scout",
			Title:     strings.TrimSpace(rec.Headline),
			Status:    coremodels.AgentJobStatusPending,
			Progress:  0,
			CreatedAt: now,
			UpdatedAt: now,
			AutoMode:  true,
			PlanMode:  coremodels.PlanModeAuto,
			UserQuery: strings.TrimSpace(rec.Headline),
			State:     state,
		},
		LensID:              strings.TrimSpace(rec.LensID),
		TargetCanonicalPath: strings.TrimSpace(rec.SubjectCanonicalPath),
		ScoutingStrategy:    string(strategy),
		BillingContext: horizonmodels.BillingContext{
			ChargeModel: string(horizonmodels.ChargeModelAccess),
			MaxCost:     0,
			ProjectID:   effectiveProjectID,
		},
		CurrentState: horizonmodels.ScoutingJobStatePending,
	}
	if _, err := s.scoutingJobsRepo.Create(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *Service) loadSubjectSnapshot(ctx context.Context, tenantID, canonicalPath string) (*subjectSnapshot, error) {
	subject, err := s.fragmentsRepo.GetByCanonicalPath(ctx, canonicalPath)
	if err != nil || subject == nil {
		return nil, err
	}
	maxTraversalHops := 2
	if isCountryScopedZoneFragment(subject) {
		maxTraversalHops = 1
	}
	directEdges, err := s.listEdgesBySourceTenantAware(ctx, tenantID, strings.TrimSpace(subject.ID), 96)
	if err != nil {
		return nil, err
	}
	directTargets := make([]*horizonmodels.HorizonFragment, 0, len(directEdges))
	nodesByID := map[string]*horizonmodels.HorizonFragment{
		strings.TrimSpace(subject.ID): subject,
	}
	for _, edge := range directEdges {
		if edge == nil || strings.TrimSpace(edge.TargetFragmentID) == "" {
			continue
		}
		frag, ferr := s.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.TargetFragmentID))
		if ferr != nil || frag == nil {
			continue
		}
		directTargets = append(directTargets, frag)
		nodesByID[strings.TrimSpace(frag.ID)] = frag
	}
	visited := map[string]int{strings.TrimSpace(subject.ID): 0}
	queue := append([]*horizonmodels.HorizonFragment{}, directTargets...)
	for _, frag := range directTargets {
		visited[strings.TrimSpace(frag.ID)] = 1
	}
	edgeCount := len(directEdges)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		curID := strings.TrimSpace(cur.ID)
		hops := visited[curID]
		if hops >= maxTraversalHops {
			continue
		}
		outgoing, _ := s.listEdgesBySourceTenantAware(ctx, tenantID, curID, 32)
		incoming, _ := s.listEdgesByTargetTenantAware(ctx, tenantID, curID, 32)
		adjacent := append(outgoing, incoming...)
		edgeCount += len(adjacent)
		for _, edge := range adjacent {
			if edge == nil {
				continue
			}
			nextID := strings.TrimSpace(edge.TargetFragmentID)
			if strings.TrimSpace(edge.TargetFragmentID) == curID {
				nextID = strings.TrimSpace(edge.SourceFragmentID)
			}
			if nextID == "" {
				continue
			}
			if existingHops, ok := visited[nextID]; ok && existingHops <= hops+1 {
				continue
			}
			frag, ferr := s.fragmentsRepo.Get(ctx, nextID)
			if ferr != nil || frag == nil {
				continue
			}
			visited[nextID] = hops + 1
			nodesByID[nextID] = frag
			queue = append(queue, frag)
		}
	}
	nodes := make([]*horizonmodels.HorizonFragment, 0, len(nodesByID))
	paths := make([]string, 0, len(nodesByID))
	labels := make([]string, 0, len(nodesByID))
	anchorPaths := []string{strings.ToLower(strings.TrimSpace(subject.CanonicalPath))}
	anchorLabels := []string{strings.ToLower(strings.TrimSpace(labelOfFragment(subject)))}
	for _, frag := range nodesByID {
		nodes = append(nodes, frag)
		paths = append(paths, strings.ToLower(strings.TrimSpace(frag.CanonicalPath)))
		labels = append(labels, strings.ToLower(strings.TrimSpace(labelOfFragment(frag))))
	}
	for _, frag := range directTargets {
		if frag == nil {
			continue
		}
		anchorPaths = append(anchorPaths, strings.ToLower(strings.TrimSpace(frag.CanonicalPath)))
		anchorLabels = append(anchorLabels, strings.ToLower(strings.TrimSpace(labelOfFragment(frag))))
	}
	sort.Strings(paths)
	sort.Strings(labels)
	return &subjectSnapshot{
		subject:       subject,
		directTargets: uniqueFragments(directTargets),
		nodes:         nodes,
		paths:         paths,
		labels:        labels,
		anchorPaths:   uniqueStrings(anchorPaths),
		anchorLabels:  uniqueStrings(anchorLabels),
	}, nil
}

func matchStrategicCoverageProfile(snapshot *subjectSnapshot) *strategicCoverageProfile {
	if snapshot == nil || snapshot.subject == nil {
		return nil
	}
	haystack := strings.ToLower(strings.Join(append(append([]string{
		strings.TrimSpace(snapshot.subject.CanonicalPath),
		firstNonEmptyString(snapshot.subject.Labels["en"], snapshot.subject.Description),
	}, snapshot.anchorPaths...), snapshot.anchorLabels...), " "))
	for i := range strategicCoverageProfiles {
		profile := &strategicCoverageProfiles[i]
		for _, prefix := range profile.TriggerPathPrefixes {
			if strings.Contains(haystack, strings.ToLower(strings.TrimSpace(prefix))) {
				return profile
			}
		}
		for _, term := range profile.TriggerTerms {
			if strings.Contains(haystack, strings.ToLower(strings.TrimSpace(term))) {
				return profile
			}
		}
	}
	return nil
}

func evaluateCoverageProfile(profile *strategicCoverageProfile, snapshot *subjectSnapshot, input DetectionInput) (horizonmodels.StrategicCoverageStats, []string) {
	stats := horizonmodels.StrategicCoverageStats{
		ProfileID:         profile.ID,
		ProfileVersion:    profile.Version,
		DirectTargets:     len(snapshot.directTargets),
		NeighborhoodNodes: len(snapshot.nodes),
		MaxHops:           2,
		RemoteMatches:     input.RemoteEndpoints,
		BusinessEndpoints: input.BusinessEndpoints,
		PathFamilies:      input.PathFamilies,
		ExpectedFamilies:  len(profile.ExpectedFamilies),
	}
	missingFamilies := make([]string, 0, len(profile.ExpectedFamilies))
	details := make([]horizonmodels.StrategicCoverageFamilyStat, 0, len(profile.ExpectedFamilies))
	haystack := append(append([]string{}, snapshot.paths...), snapshot.labels...)
	for _, family := range profile.ExpectedFamilies {
		matchedRefs := make([]string, 0, 4)
		for _, ref := range haystack {
			for _, term := range family.Terms {
				if strings.Contains(ref, strings.ToLower(strings.TrimSpace(term))) {
					matchedRefs = append(matchedRefs, ref)
					break
				}
			}
			if len(matchedRefs) >= maxInt(family.MinRefs, 1) {
				break
			}
		}
		matched := len(matchedRefs) >= maxInt(family.MinRefs, 1)
		if matched {
			stats.MatchedFamilies++
		} else {
			missingFamilies = append(missingFamilies, family.ID)
		}
		details = append(details, horizonmodels.StrategicCoverageFamilyStat{
			Family:      family.ID,
			Matched:     matched,
			MatchedRefs: matchedRefs,
		})
	}
	stats.MissingFamilyDetails = details
	return stats, horizonrepo.NormalizeStrategicRecommendationTargetFamilies(missingFamilies)
}

func computeRecommendationConfidence(subject *horizonmodels.HorizonFragment, profile *strategicCoverageProfile, stats horizonmodels.StrategicCoverageStats, input DetectionInput) float64 {
	confidence := 0.52
	if subject != nil {
		if hype := qualifierFloat(subject.Qualifiers, "zone_hype_score", "hypeScore"); hype > 0 {
			confidence += minFloat(hype/100.0, 0.25)
		}
		if status := strings.ToLower(strings.TrimSpace(asString(subject.Qualifiers["hypeStatus"]))); status == "hot" {
			confidence += 0.08
		}
		if strings.EqualFold(asString(subject.Qualifiers["conditionType"]), "military_conflict") {
			confidence += 0.05
		}
	}
	if stats.ExpectedFamilies > 0 {
		missingRatio := float64(stats.ExpectedFamilies-stats.MatchedFamilies) / float64(stats.ExpectedFamilies)
		confidence += missingRatio * 0.22
	}
	if input.RemoteEndpoints == 0 {
		confidence += 0.08
	}
	if input.BusinessEndpoints == 0 {
		confidence += 0.04
	}
	return clamp01(confidence)
}

func recommendationPriority(profile *strategicCoverageProfile, subject *horizonmodels.HorizonFragment) horizonmodels.StrategicRecommendationPriority {
	if profile != nil && strings.EqualFold(profile.PriorityHint, "critical") {
		return horizonmodels.StrategicRecommendationPriorityCritical
	}
	if subject != nil && strings.EqualFold(asString(subject.Qualifiers["hypeStatus"]), "hot") {
		return horizonmodels.StrategicRecommendationPriorityCritical
	}
	return horizonmodels.StrategicRecommendationPriorityHigh
}

func buildEvidence(snapshot *subjectSnapshot, input DetectionInput, stats horizonmodels.StrategicCoverageStats) []string {
	evidence := []string{}
	if title := strings.TrimSpace(input.EventTitle); title != "" {
		evidence = append(evidence, "Source event: "+title)
	}
	if snapshot != nil && snapshot.subject != nil {
		evidence = append(evidence, "Subject: "+strings.TrimSpace(snapshot.subject.CanonicalPath))
	}
	evidence = append(evidence,
		fmt.Sprintf("Direct targets: %d", stats.DirectTargets),
		fmt.Sprintf("Matched families: %d/%d", stats.MatchedFamilies, stats.ExpectedFamilies),
		fmt.Sprintf("Remote endpoints: %d", stats.RemoteMatches),
	)
	return evidence
}

func strategicFocus(rec *horizonmodels.StrategicRecommendation) string {
	if rec == nil {
		return ""
	}
	targets := strings.Join(rec.ExpectedMissingTargets, ", ")
	if targets == "" {
		targets = "strategic downstream dependencies"
	}
	return fmt.Sprintf("Investigate and expand missing strategic downstream coverage for %s around %s.", targets, strings.TrimSpace(rec.SubjectCanonicalPath))
}

func buildStrategicAnchorTerms(rec *horizonmodels.StrategicRecommendation) []string {
	if rec == nil {
		return nil
	}
	terms := []string{strings.TrimSpace(rec.SubjectCanonicalPath)}
	if hint := strings.TrimSpace(rec.DomainContractHint); hint != "" {
		terms = append(terms, hint)
	}
	for _, family := range rec.ExpectedMissingTargets {
		family = strings.TrimSpace(strings.ReplaceAll(family, "_", " "))
		if family != "" {
			terms = append(terms, family)
		}
	}
	return uniqueStrings(terms)
}

func (s *Service) maybeAdjudicateGapWithLLM(ctx context.Context, snapshot *subjectSnapshot, profile *strategicCoverageProfile, missingFamilies []string, confidence float64) (*llmAdjudication, bool) {
	if s == nil || s.exec == nil || snapshot == nil || snapshot.subject == nil || profile == nil || len(missingFamilies) == 0 {
		return nil, false
	}
	prompt := fmt.Sprintf(`You are reviewing a graph coverage gap recommendation.
Subject: %s
Subject label: %s
Profile: %s
Missing families: %s
Current confidence: %.2f
Direct targets: %d
Neighborhood nodes: %d

Return strict JSON:
{
  "shouldCreate": true,
  "confidence": 0.0,
  "headline": "short headline",
  "reason": "short reason",
  "domainContractHint": "/domain/...",
  "missingFamilies": ["family"]
}

Rules:
- Only set shouldCreate=true if the missing families are strategically relevant consequences.
- Keep headline and reason concise.
- If uncertain, preserve missingFamilies rather than inventing new ones.
`, strings.TrimSpace(snapshot.subject.CanonicalPath), strings.TrimSpace(firstNonEmptyString(labelOfFragment(snapshot.subject), snapshot.subject.Description)), profile.ID, strings.Join(missingFamilies, ", "), confidence, len(snapshot.directTargets), len(snapshot.nodes))
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       4,
			MaxOutputSize:            1200,
		},
		Verifier: ai.NewSimpleJSONVerifier(),
	}
	raw, _, err := s.exec.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, false
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var out llmAdjudication
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, false
	}
	out.Confidence = clamp01(out.Confidence)
	out.MissingFamilies = horizonrepo.NormalizeStrategicRecommendationTargetFamilies(out.MissingFamilies)
	return &out, true
}

func (s *Service) publish(ctx context.Context, tenantID, eventType string, rec *horizonmodels.StrategicRecommendation) {
	if s == nil || s.realtime == nil || rec == nil {
		return
	}
	payload := map[string]interface{}{
		"id":                     rec.ID,
		"tenantId":               rec.TenantID,
		"projectId":              rec.ProjectID,
		"lensId":                 rec.LensID,
		"subjectCanonicalPath":   rec.SubjectCanonicalPath,
		"subjectType":            rec.SubjectType,
		"status":                 rec.Status,
		"priority":               rec.Priority,
		"confidence":             rec.Confidence,
		"headline":               rec.Headline,
		"reason":                 rec.Reason,
		"expectedMissingTargets": rec.ExpectedMissingTargets,
		"currentCoverageStats":   rec.CurrentCoverageStats,
		"proposedAction":         rec.ProposedAction,
		"domainContractHint":     rec.DomainContractHint,
		"execution":              rec.Execution,
		"updatedAt":              rec.UpdatedAt,
		"createdAt":              rec.CreatedAt,
	}
	_ = s.realtime.PublishJSON(ctx, realtime.EventType(eventType), tenantID, payload)
}

func (s *Service) listEdgesBySourceTenantAware(ctx context.Context, tenantID, sourceID string, limit int) ([]*horizonmodels.DependencyEdge, error) {
	edges, err := s.edgesRepo.ListBySource(ctx, tenantID, sourceID, nil, limit)
	if err == nil && len(edges) > 0 {
		return edges, nil
	}
	if strings.TrimSpace(tenantID) == "global" {
		return edges, err
	}
	fallback, ferr := s.edgesRepo.ListBySource(ctx, "global", sourceID, nil, limit)
	if ferr == nil && len(fallback) > 0 {
		return fallback, nil
	}
	if err != nil {
		return edges, err
	}
	return fallback, ferr
}

func (s *Service) listEdgesByTargetTenantAware(ctx context.Context, tenantID, targetID string, limit int) ([]*horizonmodels.DependencyEdge, error) {
	edges, err := s.edgesRepo.ListByTarget(ctx, tenantID, targetID, nil, limit)
	if err == nil && len(edges) > 0 {
		return edges, nil
	}
	if strings.TrimSpace(tenantID) == "global" {
		return edges, err
	}
	fallback, ferr := s.edgesRepo.ListByTarget(ctx, "global", targetID, nil, limit)
	if ferr == nil && len(fallback) > 0 {
		return fallback, nil
	}
	if err != nil {
		return edges, err
	}
	return fallback, ferr
}

func uniqueFragments(in []*horizonmodels.HorizonFragment) []*horizonmodels.HorizonFragment {
	out := make([]*horizonmodels.HorizonFragment, 0, len(in))
	seen := map[string]struct{}{}
	for _, frag := range in {
		if frag == nil {
			continue
		}
		id := strings.TrimSpace(frag.ID)
		if id == "" {
			id = strings.TrimSpace(frag.CanonicalPath)
		}
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, frag)
	}
	return out
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
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

func qualifierFloat(qualifiers map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		v := qualifiers[key]
		switch value := v.(type) {
		case float64:
			return value
		case int:
			return float64(value)
		case int64:
			return float64(value)
		}
	}
	return 0
}

func asString(v interface{}) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
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

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if s := strings.TrimSpace(value); s != "" {
			return s
		}
	}
	return ""
}

func labelOfFragment(frag *horizonmodels.HorizonFragment) string {
	if frag == nil {
		return ""
	}
	shortName := ""
	legalName := ""
	if frag.Identifiers != nil {
		shortName = frag.Identifiers["shortName"]
		legalName = frag.Identifiers["legalName"]
	}
	return firstNonEmptyString(frag.Labels["en"], shortName, legalName)
}

func shouldSuppressBroadSubjectRecommendation(profile *strategicCoverageProfile, snapshot *subjectSnapshot) bool {
	if profile == nil || snapshot == nil || snapshot.subject == nil {
		return false
	}
	if !isCountryScopedZoneFragment(snapshot.subject) {
		return false
	}
	if fragmentMatchesProfileAnchor(profile, snapshot.subject) {
		return false
	}
	return hasDirectProfileAnchor(profile, snapshot.directTargets)
}

func isCountryScopedZoneFragment(frag *horizonmodels.HorizonFragment) bool {
	if frag == nil {
		return false
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(frag.CanonicalPath), "/"), "/")
	if len(parts) != 4 || parts[0] != "zone" {
		return false
	}
	return len(strings.TrimSpace(parts[1])) == 2
}

func hasDirectProfileAnchor(profile *strategicCoverageProfile, targets []*horizonmodels.HorizonFragment) bool {
	if profile == nil {
		return false
	}
	for _, target := range targets {
		if target == nil {
			continue
		}
		if fragmentMatchesProfileAnchor(profile, target) {
			return true
		}
	}
	return false
}

func fragmentMatchesProfileAnchor(profile *strategicCoverageProfile, frag *horizonmodels.HorizonFragment) bool {
	if profile == nil || frag == nil {
		return false
	}
	path := strings.ToLower(strings.TrimSpace(frag.CanonicalPath))
	label := strings.ToLower(strings.TrimSpace(labelOfFragment(frag)))
	for _, prefix := range profile.TriggerPathPrefixes {
		if prefix = strings.ToLower(strings.TrimSpace(prefix)); prefix != "" && strings.HasPrefix(path, prefix) {
			return true
		}
	}
	for _, term := range profile.TriggerTerms {
		if term = strings.ToLower(strings.TrimSpace(term)); term != "" && strings.Contains(label, term) {
			return true
		}
	}
	return false
}
