package watchruntime

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

const (
	defaultWatchRuntimeProjectID   = "horizon-watch-bootstrap-global"
	strategicEnrichmentJobKind     = "strategic_enrichment"
	strategicEnrichmentStageTarget = "targeted"
)

type SubjectWakeHook func(ctx context.Context, tenantID, subjectID, triggerType, triggerReason string, trace map[string]any) ([]string, error)

type RecommendationActionInput struct {
	TenantID             string
	ProjectID            string
	LensID               string
	SubjectCanonicalPath string
	SubjectType          horizonmodels.StrategicRecommendationSubjectType
	TriggerType          horizonmodels.WatchTriggerType
	TriggerReason        string
	EventTitle           string
	EventSummary         string
	SignalAxis           string
	BusinessEndpoints    int
	RemoteEndpoints      int
	PathFamilies         int
	OccurredAt           time.Time
}

type RecommendationActionHook func(ctx context.Context, input RecommendationActionInput) error
type RecommendationExecutionHook func(ctx context.Context, recommendationID, jobID, stage, summary string) error
type RecommendationCompletionHook func(ctx context.Context, job *horizonmodels.ScoutingJob) error

type MetricsSnapshot struct {
	Counters         map[string]int64 `json:"counters"`
	LatencyCount     int64            `json:"latencyCount"`
	LatencyTotalMs   float64          `json:"latencyTotalMs"`
	LatencyAverageMs float64          `json:"latencyAverageMs"`
	LastUpdatedAt    time.Time        `json:"lastUpdatedAt"`
}

type runtimeMetrics struct {
	mu             sync.Mutex
	counters       map[string]int64
	latencyCount   int64
	latencyTotalMs float64
	lastUpdatedAt  time.Time
}

type Service struct {
	subjectsRepo                 *horizonrepo.WatchSubjectsRepository
	triggersRepo                 *horizonrepo.WatchTriggersRepository
	targetsRepo                  *horizonrepo.WatchTargetsRepository
	watchdogRepo                 *horizonrepo.WatchdogConfigsRepository
	scopeAgentConfigsRepo        *horizonrepo.ScopeAgentConfigsRepository
	scopeAgentRunsRepo           *horizonrepo.ScopeAgentRunsRepository
	proposalsRepo                *horizonrepo.AgentProposalsRepository
	decisionsRepo                *horizonrepo.ArbitrationDecisionsRepository
	watchBriefsRepo              *horizonrepo.WatchBriefsRepository
	watchFeedEntriesRepo         *horizonrepo.WatchFeedEntriesRepository
	lensesRepo                   *horizonrepo.LensesRepository
	lensArtifactsRepo            *horizonrepo.LensArtifactsRepository
	materializedArtifactsRepo    *horizonrepo.MaterializedArtifactsRepository
	macroAgentPoliciesRepo       *horizonrepo.MacroAgentPoliciesRepository
	fragmentsRepo                *horizonrepo.HorizonFragmentsRepository
	edgesRepo                    *horizonrepo.DependencyEdgesRepository
	riskJobsRepo                 *horizonrepo.RiskJobsRepository
	scoutingJobsRepo             *horizonrepo.ScoutingJobsRepository
	wakeHook                     SubjectWakeHook
	recommendationActionHook     RecommendationActionHook
	recommendationExecutionHook  RecommendationExecutionHook
	recommendationCompletionHook RecommendationCompletionHook
	metrics                      *runtimeMetrics
}

type SyncReport struct {
	TenantID                 string   `json:"tenantId"`
	DryRun                   bool     `json:"dryRun"`
	LensesSynced             int      `json:"lensesSynced"`
	TargetsSynced            int      `json:"targetsSynced"`
	WatchdogsSynced          int      `json:"watchdogsSynced"`
	FragmentsSynced          int      `json:"fragmentsSynced"`
	SubjectsCreated          int      `json:"subjectsCreated"`
	SubjectsRefreshed        int      `json:"subjectsRefreshed"`
	ConfigsLinked            int      `json:"configsLinked"`
	ScopeAgentConfigsEnsured int      `json:"scopeAgentConfigsEnsured"`
	LegacyConfigsPaused      int      `json:"legacyConfigsPaused"`
	Conflicts                int      `json:"conflicts"`
	SubjectIDs               []string `json:"subjectIds,omitempty"`
}

type SubjectDetail struct {
	Subject      *horizonmodels.WatchSubject       `json:"subject"`
	Configs      []*horizonmodels.ScopeAgentConfig `json:"configs,omitempty"`
	Runs         []*horizonmodels.ScopeAgentRun    `json:"runs,omitempty"`
	Triggers     []*horizonmodels.WatchTrigger     `json:"triggers,omitempty"`
	RiskJobs     []*horizonmodels.RiskJob          `json:"riskJobs,omitempty"`
	ScoutingJobs []*horizonmodels.ScoutingJob      `json:"scoutingJobs,omitempty"`
}

type TriggerDecision struct {
	Decision    horizonmodels.WatchTriggerDecision
	Reason      string
	DirectMatch bool
}

func NewService(
	subjectsRepo *horizonrepo.WatchSubjectsRepository,
	triggersRepo *horizonrepo.WatchTriggersRepository,
	targetsRepo *horizonrepo.WatchTargetsRepository,
	watchdogRepo *horizonrepo.WatchdogConfigsRepository,
	scopeAgentConfigsRepo *horizonrepo.ScopeAgentConfigsRepository,
	lensesRepo *horizonrepo.LensesRepository,
	lensArtifactsRepo *horizonrepo.LensArtifactsRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	edgesRepo *horizonrepo.DependencyEdgesRepository,
) *Service {
	return &Service{
		subjectsRepo:          subjectsRepo,
		triggersRepo:          triggersRepo,
		targetsRepo:           targetsRepo,
		watchdogRepo:          watchdogRepo,
		scopeAgentConfigsRepo: scopeAgentConfigsRepo,
		lensesRepo:            lensesRepo,
		lensArtifactsRepo:     lensArtifactsRepo,
		fragmentsRepo:         fragmentsRepo,
		edgesRepo:             edgesRepo,
		metrics: &runtimeMetrics{
			counters: make(map[string]int64),
		},
	}
}

func (s *Service) WithWakeHook(fn SubjectWakeHook) *Service {
	if s == nil {
		return s
	}
	s.wakeHook = fn
	return s
}

func (s *Service) WithScopeAgentRunsRepository(repo *horizonrepo.ScopeAgentRunsRepository) *Service {
	if s == nil {
		return s
	}
	s.scopeAgentRunsRepo = repo
	return s
}

func (s *Service) WithMaterializedArtifactsRepository(repo *horizonrepo.MaterializedArtifactsRepository) *Service {
	if s == nil {
		return s
	}
	s.materializedArtifactsRepo = repo
	return s
}

func (s *Service) WithMacroAgentPoliciesRepository(repo *horizonrepo.MacroAgentPoliciesRepository) *Service {
	if s == nil {
		return s
	}
	s.macroAgentPoliciesRepo = repo
	return s
}

func (s *Service) WithWatchBriefsRepository(repo *horizonrepo.WatchBriefsRepository) *Service {
	if s == nil {
		return s
	}
	s.watchBriefsRepo = repo
	return s
}

func (s *Service) WithAgentProposalsRepository(repo *horizonrepo.AgentProposalsRepository) *Service {
	if s == nil {
		return s
	}
	s.proposalsRepo = repo
	return s
}

func (s *Service) WithArbitrationDecisionsRepository(repo *horizonrepo.ArbitrationDecisionsRepository) *Service {
	if s == nil {
		return s
	}
	s.decisionsRepo = repo
	return s
}

func (s *Service) WithWatchFeedEntriesRepository(repo *horizonrepo.WatchFeedEntriesRepository) *Service {
	if s == nil {
		return s
	}
	s.watchFeedEntriesRepo = repo
	return s
}

func (s *Service) WithRiskJobsRepository(repo *horizonrepo.RiskJobsRepository) *Service {
	if s == nil {
		return s
	}
	s.riskJobsRepo = repo
	return s
}

func (s *Service) WithScoutingJobsRepository(repo *horizonrepo.ScoutingJobsRepository) *Service {
	if s == nil {
		return s
	}
	s.scoutingJobsRepo = repo
	return s
}

func (s *Service) WithRecommendationActionHook(fn RecommendationActionHook) *Service {
	if s == nil {
		return s
	}
	s.recommendationActionHook = fn
	return s
}

func (s *Service) WithRecommendationExecutionHook(fn RecommendationExecutionHook) *Service {
	if s == nil {
		return s
	}
	s.recommendationExecutionHook = fn
	return s
}

func (s *Service) WithRecommendationCompletionHook(fn RecommendationCompletionHook) *Service {
	if s == nil {
		return s
	}
	s.recommendationCompletionHook = fn
	return s
}

func (s *Service) EnsureSubjectForTarget(ctx context.Context, target *horizonmodels.WatchTarget) (*horizonmodels.WatchSubject, error) {
	if s == nil || s.subjectsRepo == nil || target == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	kind := horizonmodels.WatchSubjectKindExplicitTarget
	runtimeMode := horizonmodels.WatchSubjectRuntimeCombined
	sourceMode := horizonmodels.WatchSubjectSourceHybrid
	if strings.TrimSpace(target.FragmentID) != "" || strings.EqualFold(strings.TrimSpace(target.Kind), "fragment") {
		kind = horizonmodels.WatchSubjectKindFragment
		runtimeMode = horizonmodels.WatchSubjectRuntimeCustomFragment
		sourceMode = horizonmodels.WatchSubjectSourceFacts
	}
	subject := &horizonmodels.WatchSubject{
		ID:                  strings.TrimSpace(target.SubjectID),
		TenantID:            strings.TrimSpace(firstNonEmpty(target.TenantID, "global")),
		PackID:              strings.TrimSpace(target.PackID),
		UserID:              strings.TrimSpace(target.UserID),
		SubjectKind:         kind,
		AnchorCanonicalPath: strings.TrimSpace(target.CanonicalPath),
		AnchorFragmentID:    strings.TrimSpace(target.FragmentID),
		DisplayLabel:        strings.TrimSpace(firstNonEmpty(target.Label, target.CanonicalPath)),
		InterestTitle:       strings.TrimSpace(target.InterestTitle),
		InterestQuery:       strings.TrimSpace(target.InterestQuery),
		QueryOverrides:      compactUnique(append([]string{}, target.QueryOverrides...)),
		PageFraming:         cloneMap(target.PageFraming),
		Status:              mapTargetStatus(target.Status),
		SourceMode:          sourceMode,
		RuntimeMode:         runtimeMode,
		DomainModes:         append([]string(nil), target.DomainModes...),
		Roles:               normalizeRoles(target.Roles),
		Priority:            normalizedPriority(target.Priority),
		LensBindings:        s.collectLensBindings(ctx, target.TenantID, target.FragmentID, target.CanonicalPath),
		UniversePaths:       compactUnique([]string{strings.TrimSpace(target.CanonicalPath)}),
		Metadata: map[string]any{
			"kind":           strings.TrimSpace(target.Kind),
			"watchTargetId":  strings.TrimSpace(target.ID),
			"source":         strings.TrimSpace(target.Source),
			"interestTitle":  strings.TrimSpace(target.InterestTitle),
			"interestQuery":  strings.TrimSpace(target.InterestQuery),
			"queryOverrides": compactUnique(append([]string{}, target.QueryOverrides...)),
		},
	}
	if len(target.PageFraming) > 0 {
		subject.Metadata["pageFraming"] = cloneMap(target.PageFraming)
	}
	if err := s.subjectsRepo.Upsert(ctx, subject); err != nil {
		return nil, err
	}
	target.SubjectID = subject.ID
	if s.targetsRepo != nil {
		_ = s.targetsRepo.Upsert(ctx, target)
	}
	return subject, nil
}

func (s *Service) EnsureSubjectForWatchdogConfig(ctx context.Context, cfg *horizonmodels.WatchdogConfig) (*horizonmodels.WatchSubject, error) {
	if s == nil || s.subjectsRepo == nil || cfg == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	fragment, err := s.fragmentsRepo.Get(ctx, cfg.TargetFragmentID)
	if err != nil {
		return nil, err
	}
	subject := &horizonmodels.WatchSubject{
		ID:                  strings.TrimSpace(cfg.SubjectID),
		TenantID:            strings.TrimSpace(firstNonEmpty(cfg.TenantID, "global")),
		SubjectKind:         horizonmodels.WatchSubjectKindFragment,
		AnchorCanonicalPath: strings.TrimSpace(fragment.CanonicalPath),
		AnchorFragmentID:    strings.TrimSpace(fragment.ID),
		DisplayLabel:        strings.TrimSpace(fragmentDisplayName(fragment)),
		InterestTitle:       strings.TrimSpace(cfg.InterestTitle),
		InterestQuery:       strings.TrimSpace(cfg.InterestQuery),
		QueryOverrides:      compactUnique(append([]string{}, cfg.QueryOverrides...)),
		PageFraming:         cloneMap(cfg.PageFraming),
		Status:              mapWatchdogStatus(cfg.Status),
		SourceMode:          sourceModeFromWatchdog(cfg),
		RuntimeMode:         horizonmodels.WatchSubjectRuntimeCustomFragment,
		Roles:               []string{"monitor", "risk", "scout"},
		Priority:            60,
		LensBindings:        s.collectLensBindings(ctx, cfg.TenantID, fragment.ID, fragment.CanonicalPath),
		UniversePaths:       compactUnique([]string{strings.TrimSpace(fragment.CanonicalPath)}),
		Metadata: map[string]any{
			"watchdogConfigId": strings.TrimSpace(cfg.ID),
			"interestTitle":    strings.TrimSpace(cfg.InterestTitle),
			"interestQuery":    strings.TrimSpace(cfg.InterestQuery),
			"queryOverrides":   compactUnique(append([]string{}, cfg.QueryOverrides...)),
		},
	}
	if len(cfg.PageFraming) > 0 {
		subject.Metadata["pageFraming"] = cloneMap(cfg.PageFraming)
	}
	if err := s.subjectsRepo.Upsert(ctx, subject); err != nil {
		return nil, err
	}
	cfg.SubjectID = subject.ID
	if s.watchdogRepo != nil && strings.TrimSpace(cfg.ID) != "" {
		_ = s.watchdogRepo.Update(ctx, cfg)
	}
	if err := s.ensureDerivedScopeAgentConfig(ctx, subject); err != nil {
		return nil, err
	}
	return subject, nil
}

func (s *Service) SyncWatchSubjectsForLens(ctx context.Context, lensID string) error {
	if s == nil || s.subjectsRepo == nil || s.lensesRepo == nil || strings.TrimSpace(lensID) == "" {
		return horizonrepo.ErrInvalidDocument
	}
	lens, err := s.lensesRepo.Get(ctx, strings.TrimSpace(lensID))
	if err != nil {
		return err
	}
	lensPolicy := s.activeLensUniversePolicy(ctx, strings.TrimSpace(firstNonEmpty(lens.TenantID, "global")), strings.TrimSpace(lens.ID))
	subject := &horizonmodels.WatchSubject{
		TenantID:            strings.TrimSpace(firstNonEmpty(lens.TenantID, "global")),
		ProjectID:           strings.TrimSpace(lens.ProjectID),
		LensID:              strings.TrimSpace(lens.ID),
		SubjectKind:         horizonmodels.WatchSubjectKindLensUniverse,
		AnchorCanonicalPath: lensCanonicalPath(lens.ID),
		DisplayLabel:        strings.TrimSpace(firstNonEmpty(lens.Name, lens.ID)),
		Status:              mapLensStatus(lens.Status),
		SourceMode:          horizonmodels.WatchSubjectSourceHybrid,
		RuntimeMode:         horizonmodels.WatchSubjectRuntimeGeneralized,
		DomainModes:         append([]string(nil), lens.DomainModes...),
		Roles:               []string{"monitor", "risk", "scout"},
		Priority:            70,
		LensBindings:        []string{strings.TrimSpace(lens.ID)},
		UniversePaths:       s.buildWatchUniverse(ctx, lens),
		Metadata: map[string]any{
			"projectId": strings.TrimSpace(lens.ProjectID),
			"lensName":  strings.TrimSpace(lens.Name),
		},
	}
	applyLensUniversePolicyToSubject(subject, lensPolicy)
	if err := s.subjectsRepo.Upsert(ctx, subject); err != nil {
		return err
	}
	if err := s.ensureDerivedScopeAgentConfig(ctx, subject); err != nil {
		return err
	}
	return s.syncDerivedPathOverlaySubjects(ctx, lens, subject, lensPolicy)
}

func (s *Service) SyncSubjectsForFragment(ctx context.Context, fragmentID string) error {
	if s == nil || s.fragmentsRepo == nil || strings.TrimSpace(fragmentID) == "" {
		return horizonrepo.ErrInvalidDocument
	}
	fragment, err := s.fragmentsRepo.Get(ctx, strings.TrimSpace(fragmentID))
	if err != nil {
		return err
	}
	baseSubject := &horizonmodels.WatchSubject{
		TenantID:            strings.TrimSpace(firstNonEmpty(fragment.TenantID, "global")),
		SubjectKind:         horizonmodels.WatchSubjectKindFragment,
		AnchorCanonicalPath: strings.TrimSpace(fragment.CanonicalPath),
		AnchorFragmentID:    strings.TrimSpace(fragment.ID),
		DisplayLabel:        strings.TrimSpace(fragmentDisplayName(fragment)),
		Status:              horizonmodels.WatchSubjectStatusActive,
		SourceMode:          horizonmodels.WatchSubjectSourceFacts,
		RuntimeMode:         horizonmodels.WatchSubjectRuntimeCustomFragment,
		Roles:               []string{"monitor", "risk", "scout"},
		Priority:            60,
		LensBindings:        s.collectLensBindings(ctx, fragment.TenantID, fragment.ID, fragment.CanonicalPath),
		UniversePaths:       compactUnique([]string{strings.TrimSpace(fragment.CanonicalPath)}),
	}
	if err := s.subjectsRepo.Upsert(ctx, baseSubject); err != nil {
		return err
	}
	if err := s.ensureDerivedScopeAgentConfig(ctx, baseSubject); err != nil {
		return err
	}
	if s.watchdogRepo != nil {
		if cfg, cfgErr := s.watchdogRepo.GetByTargetFragmentID(ctx, strings.TrimSpace(firstNonEmpty(fragment.TenantID, "global")), strings.TrimSpace(fragment.ID)); cfgErr == nil && cfg != nil {
			if _, err := s.EnsureSubjectForWatchdogConfig(ctx, cfg); err != nil {
				return err
			}
		} else if cfgErr != nil && cfgErr != horizonrepo.ErrWatchdogConfigNotFound {
			return cfgErr
		}
	}
	lensIDs := s.collectLensBindings(ctx, fragment.TenantID, fragment.ID, fragment.CanonicalPath)
	for _, lensID := range lensIDs {
		if err := s.SyncWatchSubjectsForLens(ctx, lensID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) EmitTrigger(ctx context.Context, trigger *horizonmodels.WatchTrigger) (*horizonmodels.WatchTrigger, bool, error) {
	if s == nil || s.triggersRepo == nil || trigger == nil {
		return nil, false, horizonrepo.ErrInvalidDocument
	}
	trigger.SemanticWatermark = firstNonEmpty(strings.TrimSpace(trigger.SemanticWatermark), semanticWatermarkFromTrigger(trigger))
	if strings.TrimSpace(trigger.DedupeKey) == "" {
		trigger.DedupeKey = buildWatchTriggerDedupeKey(trigger)
	}
	stored, created, err := s.triggersRepo.CreateOrRefresh(ctx, trigger)
	if err == nil && stored != nil {
		s.bumpMetric("watch_triggers_total")
		s.bumpMetric("watch_triggers_total:type=" + string(stored.TriggerType))
		if created {
			s.bumpMetric("watch_triggers_total:created")
		} else {
			s.bumpMetric("watch_trigger_dedup_hits_total")
		}
	}
	return stored, created, err
}

func (s *Service) EmitAndProcessTrigger(ctx context.Context, trigger *horizonmodels.WatchTrigger) (*horizonmodels.WatchTrigger, bool, error) {
	if s == nil || trigger == nil {
		return nil, false, horizonrepo.ErrInvalidDocument
	}
	stored, created, err := s.EmitTrigger(ctx, trigger)
	if err != nil || stored == nil {
		return stored, created, err
	}
	if !created && stored.ProcessedAt != nil && stored.Decision != "" {
		return stored, created, nil
	}
	return stored, created, s.ProcessTrigger(ctx, stored)
}

func (s *Service) EmitAndProcessTriggerForSubject(
	ctx context.Context,
	tenantID, subjectID string,
	triggerType horizonmodels.WatchTriggerType,
	reason string,
	signalStrength, confidence float64,
	metadata map[string]any,
) (*horizonmodels.WatchTrigger, bool, error) {
	if s == nil || s.subjectsRepo == nil || strings.TrimSpace(subjectID) == "" {
		return nil, false, horizonrepo.ErrInvalidDocument
	}
	subject, err := s.subjectsRepo.Get(ctx, strings.TrimSpace(subjectID))
	if err != nil {
		return nil, false, err
	}
	if subject == nil {
		return nil, false, horizonrepo.ErrInvalidDocument
	}
	if tid := strings.TrimSpace(firstNonEmpty(tenantID, "global")); tid != "" && strings.TrimSpace(firstNonEmpty(subject.TenantID, "global")) != tid {
		return nil, false, horizonrepo.ErrInvalidDocument
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	trigger := &horizonmodels.WatchTrigger{
		SubjectID:      strings.TrimSpace(subject.ID),
		TriggerType:    triggerType,
		TriggerReason:  strings.TrimSpace(reason),
		CanonicalPaths: compactUnique(append([]string{strings.TrimSpace(subject.AnchorCanonicalPath)}, parseStringSlice(metadata["canonicalPaths"])...)),
		SignalStrength: signalStrength,
		Confidence:     confidence,
		MatchMode:      horizonmodels.WatchTriggerMatchDerived,
		Metadata:       metadata,
	}
	return s.EmitAndProcessTrigger(ctx, trigger)
}

func (s *Service) EmitRecommendationApprovedByCanonicalPath(ctx context.Context, tenantID, canonicalPath, reason string, metadata map[string]any) error {
	if s == nil || s.subjectsRepo == nil {
		return nil
	}
	subjects, err := s.subjectsRepo.ListByAnchorCanonicalPath(ctx, strings.TrimSpace(firstNonEmpty(tenantID, "global")), strings.TrimSpace(canonicalPath), 25)
	if err != nil {
		return err
	}
	for _, subject := range subjects {
		if subject == nil || strings.TrimSpace(subject.ID) == "" {
			continue
		}
		_, _, err := s.EmitAndProcessTrigger(ctx, &horizonmodels.WatchTrigger{
			SubjectID:      strings.TrimSpace(subject.ID),
			TriggerType:    horizonmodels.WatchTriggerRecommendationApproved,
			TriggerReason:  strings.TrimSpace(firstNonEmpty(reason, "strategic recommendation approved")),
			CanonicalPaths: compactUnique([]string{strings.TrimSpace(canonicalPath)}),
			SignalStrength: 0.95,
			Confidence:     0.95,
			Metadata:       metadata,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListSubjects(ctx context.Context, tenantID, lensID, anchorCanonicalPath string, limit int) ([]*horizonmodels.WatchSubject, error) {
	if s == nil || s.subjectsRepo == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	switch {
	case strings.TrimSpace(lensID) != "":
		return s.subjectsRepo.ListByLens(ctx, strings.TrimSpace(tenantID), strings.TrimSpace(lensID), limit)
	case strings.TrimSpace(anchorCanonicalPath) != "":
		return s.subjectsRepo.ListByAnchorCanonicalPath(ctx, strings.TrimSpace(tenantID), strings.TrimSpace(anchorCanonicalPath), limit)
	default:
		return s.subjectsRepo.ListByTenant(ctx, strings.TrimSpace(tenantID), limit)
	}
}

func (s *Service) GetSubject(ctx context.Context, id string) (*horizonmodels.WatchSubject, error) {
	if s == nil || s.subjectsRepo == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	return s.subjectsRepo.Get(ctx, strings.TrimSpace(id))
}

func (s *Service) ListTriggers(ctx context.Context, subjectID string, limit int) ([]*horizonmodels.WatchTrigger, error) {
	if s == nil || s.triggersRepo == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	return s.triggersRepo.ListBySubject(ctx, strings.TrimSpace(subjectID), limit)
}

func (s *Service) GetTrigger(ctx context.Context, id string) (*horizonmodels.WatchTrigger, error) {
	if s == nil || s.triggersRepo == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	return s.triggersRepo.Get(ctx, strings.TrimSpace(id))
}

func (s *Service) SubjectDetail(ctx context.Context, subjectID string, limit int) (*SubjectDetail, error) {
	if s == nil || s.subjectsRepo == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	subject, err := s.subjectsRepo.Get(ctx, strings.TrimSpace(subjectID))
	if err != nil {
		return nil, err
	}
	detail := &SubjectDetail{Subject: subject}
	if s.scopeAgentConfigsRepo != nil {
		detail.Configs, _ = s.scopeAgentConfigsRepo.ListActiveBySubjectID(ctx, subject.TenantID, subject.ID)
	}
	if s.scopeAgentRunsRepo != nil {
		for _, cfg := range detail.Configs {
			if cfg == nil || strings.TrimSpace(cfg.ID) == "" {
				continue
			}
			runs, _ := s.scopeAgentRunsRepo.ListByConfigID(ctx, cfg.ID, limit)
			detail.Runs = append(detail.Runs, runs...)
		}
	}
	detail.Triggers, _ = s.ListTriggers(ctx, subject.ID, limit)
	scoutIDs := map[string]struct{}{}
	riskIDs := map[string]struct{}{}
	for _, trg := range detail.Triggers {
		if trg == nil {
			continue
		}
		for _, ref := range trg.JobRefs {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			switch {
			case strings.HasPrefix(ref, "risk:"):
				riskIDs[strings.TrimPrefix(ref, "risk:")] = struct{}{}
			case strings.HasPrefix(ref, "scout:"):
				scoutIDs[strings.TrimPrefix(ref, "scout:")] = struct{}{}
			}
		}
	}
	if s.riskJobsRepo != nil {
		for _, id := range sortedKeys(riskIDs) {
			if job, err := s.riskJobsRepo.Get(ctx, id); err == nil && job != nil {
				detail.RiskJobs = append(detail.RiskJobs, job)
			}
		}
	}
	if s.scoutingJobsRepo != nil {
		for _, id := range sortedKeys(scoutIDs) {
			if job, err := s.scoutingJobsRepo.Get(ctx, id); err == nil && job != nil {
				detail.ScoutingJobs = append(detail.ScoutingJobs, job)
			}
		}
	}
	return detail, nil
}

func (s *Service) ReplayTrigger(ctx context.Context, triggerID string) (*horizonmodels.WatchTrigger, error) {
	if s == nil || s.triggersRepo == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	trigger, err := s.triggersRepo.Get(ctx, strings.TrimSpace(triggerID))
	if err != nil {
		return nil, err
	}
	if trigger == nil {
		return nil, nil
	}
	trigger.ProcessedAt = nil
	trigger.Decision = ""
	trigger.DecisionReason = ""
	trigger.ProcessingError = ""
	if err := s.ProcessTrigger(ctx, trigger); err != nil {
		return nil, err
	}
	return s.triggersRepo.Get(ctx, trigger.ID)
}

func (s *Service) MetricsSnapshot() MetricsSnapshot {
	if s == nil || s.metrics == nil {
		return MetricsSnapshot{Counters: map[string]int64{}}
	}
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()
	out := make(map[string]int64, len(s.metrics.counters))
	for k, v := range s.metrics.counters {
		out[k] = v
	}
	avg := 0.0
	if s.metrics.latencyCount > 0 {
		avg = s.metrics.latencyTotalMs / float64(s.metrics.latencyCount)
	}
	return MetricsSnapshot{
		Counters:         out,
		LatencyCount:     s.metrics.latencyCount,
		LatencyTotalMs:   s.metrics.latencyTotalMs,
		LatencyAverageMs: avg,
		LastUpdatedAt:    s.metrics.lastUpdatedAt,
	}
}

func (s *Service) EmitTriggerByAnchor(ctx context.Context, tenantID, anchorCanonicalPath string, triggerType horizonmodels.WatchTriggerType, reason string, signalStrength, confidence float64, metadata map[string]any) (int, error) {
	if s == nil || s.subjectsRepo == nil {
		return 0, horizonrepo.ErrInvalidDocument
	}
	tenantID = strings.TrimSpace(firstNonEmpty(tenantID, "global"))
	direct, err := s.subjectsRepo.ListByAnchorCanonicalPath(ctx, tenantID, strings.TrimSpace(anchorCanonicalPath), 50)
	if err != nil {
		return 0, err
	}
	expanded, err := s.subjectsRepo.ListByUniversePath(ctx, tenantID, strings.TrimSpace(anchorCanonicalPath), 100)
	if err != nil {
		return 0, err
	}
	subjects := mergeSubjectsByID(direct, expanded)
	count := 0
	for _, subject := range subjects {
		if subject == nil || strings.TrimSpace(subject.ID) == "" {
			continue
		}
		matchMode := horizonmodels.WatchTriggerMatchUniverse
		if strings.TrimSpace(subject.AnchorCanonicalPath) == strings.TrimSpace(anchorCanonicalPath) {
			matchMode = horizonmodels.WatchTriggerMatchAnchor
		}
		_, _, err := s.EmitAndProcessTrigger(ctx, &horizonmodels.WatchTrigger{
			SubjectID:      strings.TrimSpace(subject.ID),
			TriggerType:    triggerType,
			TriggerReason:  strings.TrimSpace(reason),
			CanonicalPaths: []string{strings.TrimSpace(anchorCanonicalPath)},
			SignalStrength: signalStrength,
			Confidence:     confidence,
			MatchMode:      matchMode,
			Metadata:       metadata,
		})
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *Service) PreviewSyncTenant(ctx context.Context, tenantID string, includeGlobal bool, limit int) (*SyncReport, error) {
	return s.syncTenant(ctx, tenantID, includeGlobal, limit, true)
}

func (s *Service) SyncTenant(ctx context.Context, tenantID string, includeGlobal bool, limit int) (*SyncReport, error) {
	return s.syncTenant(ctx, tenantID, includeGlobal, limit, false)
}

func (s *Service) syncTenant(ctx context.Context, tenantID string, includeGlobal bool, limit int, dryRun bool) (*SyncReport, error) {
	if s == nil {
		return nil, horizonrepo.ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 200
	}
	report := &SyncReport{TenantID: strings.TrimSpace(firstNonEmpty(tenantID, "global")), DryRun: dryRun}
	subjectSet := map[string]struct{}{}
	syncOneTenant := func(tid string) error {
		if s.lensesRepo != nil {
			lenses, err := s.lensesRepo.ListActive(ctx, tid, limit)
			if err != nil {
				return err
			}
			for _, lens := range lenses {
				if lens == nil {
					continue
				}
				existing, _ := s.subjectsRepo.GetByIdentity(ctx, tid, lens.ProjectID, lens.ID, "", "", horizonmodels.WatchSubjectKindLensUniverse, lensCanonicalPath(lens.ID))
				if dryRun {
					if existing == nil {
						report.SubjectsCreated++
						report.ScopeAgentConfigsEnsured++
					} else {
						report.SubjectsRefreshed++
					}
				} else {
					if err := s.SyncWatchSubjectsForLens(ctx, lens.ID); err != nil {
						return err
					}
					if existing == nil {
						report.SubjectsCreated++
						report.ScopeAgentConfigsEnsured++
					} else {
						report.SubjectsRefreshed++
					}
				}
				report.LensesSynced++
				subjectSet[horizonrepo.BuildWatchSubjectID(tid, lens.ProjectID, lens.ID, "", "", horizonmodels.WatchSubjectKindLensUniverse, lensCanonicalPath(lens.ID))] = struct{}{}
			}
		}
		if s.macroAgentPoliciesRepo != nil {
			policies, err := s.macroAgentPoliciesRepo.ListActiveByTenant(ctx, tid, limit)
			if err != nil {
				return err
			}
			for _, policy := range policies {
				if policy == nil {
					continue
				}
				if dryRun {
					report.SubjectsCreated++
					report.ScopeAgentConfigsEnsured++
					continue
				}
				subjects, compileErr := s.CompileMacroAgentPolicy(ctx, policy)
				if compileErr != nil {
					report.Conflicts++
					return compileErr
				}
				if len(subjects) > 0 {
					report.ScopeAgentConfigsEnsured += len(subjects)
					report.SubjectsRefreshed += len(subjects)
					for _, subject := range subjects {
						if subject != nil && strings.TrimSpace(subject.ID) != "" {
							subjectSet[subject.ID] = struct{}{}
						}
					}
				}
			}
		}
		if s.targetsRepo != nil {
			targets, err := s.targetsRepo.ListByTenant(ctx, tid, false, limit)
			if err != nil {
				return err
			}
			for _, target := range targets {
				if target == nil {
					continue
				}
				subjectID := strings.TrimSpace(target.SubjectID)
				var subject *horizonmodels.WatchSubject
				if dryRun {
					kind := horizonmodels.WatchSubjectKindExplicitTarget
					if strings.TrimSpace(target.FragmentID) != "" || strings.EqualFold(strings.TrimSpace(target.Kind), "fragment") {
						kind = horizonmodels.WatchSubjectKindFragment
					}
					subject, _ = s.subjectsRepo.GetByIdentity(ctx, strings.TrimSpace(firstNonEmpty(target.TenantID, "global")), "", "", strings.TrimSpace(target.PackID), strings.TrimSpace(target.UserID), kind, strings.TrimSpace(target.CanonicalPath))
				} else {
					var ensureErr error
					subject, ensureErr = s.EnsureSubjectForTarget(ctx, target)
					if ensureErr != nil {
						return ensureErr
					}
				}
				report.TargetsSynced++
				if subject != nil {
					subjectSet[subject.ID] = struct{}{}
					if subjectID == "" || subjectID != subject.ID {
						report.ConfigsLinked++
					}
					if !dryRun {
						if linked, linkErr := s.backfillTargetArtifacts(ctx, target, subject); linkErr != nil {
							report.Conflicts++
							return linkErr
						} else {
							report.ConfigsLinked += linked
						}
					}
					if dryRun {
						if strings.TrimSpace(subjectID) == "" {
							report.SubjectsCreated++
						} else {
							report.SubjectsRefreshed++
						}
					}
				}
			}
		}
		if s.watchdogRepo != nil {
			configs, err := s.watchdogRepo.ListByTenant(ctx, tid, limit)
			if err != nil {
				return err
			}
			for _, cfg := range configs {
				if cfg == nil {
					continue
				}
				subjectID := strings.TrimSpace(cfg.SubjectID)
				var subject *horizonmodels.WatchSubject
				if dryRun {
					if fragment, ferr := s.fragmentsRepo.Get(ctx, cfg.TargetFragmentID); ferr == nil && fragment != nil {
						subject, _ = s.subjectsRepo.GetByIdentity(ctx, strings.TrimSpace(firstNonEmpty(cfg.TenantID, "global")), "", "", "", "", horizonmodels.WatchSubjectKindFragment, strings.TrimSpace(fragment.CanonicalPath))
					}
				} else {
					var ensureErr error
					subject, ensureErr = s.EnsureSubjectForWatchdogConfig(ctx, cfg)
					if ensureErr != nil {
						return ensureErr
					}
				}
				report.WatchdogsSynced++
				if subject != nil {
					subjectSet[subject.ID] = struct{}{}
					if subjectID == "" || subjectID != subject.ID {
						report.ConfigsLinked++
					}
					if dryRun {
						if strings.TrimSpace(subjectID) == "" {
							report.SubjectsCreated++
						} else {
							report.SubjectsRefreshed++
						}
					}
				}
			}
		}
		if s.scopeAgentConfigsRepo != nil {
			configs, err := s.scopeAgentConfigsRepo.ListByTenant(ctx, tid, limit)
			if err != nil {
				return err
			}
			for _, cfg := range configs {
				if cfg == nil || strings.TrimSpace(cfg.ScopeCanonicalPath) == "" {
					continue
				}
				subjects, lookupErr := s.subjectsRepo.ListByAnchorCanonicalPath(ctx, tid, cfg.ScopeCanonicalPath, 8)
				if lookupErr != nil || len(subjects) == 0 {
					continue
				}
				chosen := subjects[0]
				if chosen == nil || strings.TrimSpace(chosen.ID) == "" {
					continue
				}
				if strings.TrimSpace(cfg.SubjectID) != strings.TrimSpace(chosen.ID) {
					report.ConfigsLinked++
					if !dryRun {
						cfg.SubjectID = chosen.ID
						if err := s.scopeAgentConfigsRepo.Upsert(ctx, cfg); err != nil {
							report.Conflicts++
							return err
						}
					}
				}
				if !dryRun {
					linked, linkErr := s.backfillScopeRunsForConfig(ctx, cfg, chosen.ID, limit)
					if linkErr != nil {
						report.Conflicts++
						return linkErr
					}
					report.ConfigsLinked += linked
				}
			}
			if !dryRun {
				paused, pauseErr := s.pauseLegacyScopeConfigs(ctx, configs)
				if pauseErr != nil {
					report.Conflicts++
					return pauseErr
				}
				report.LegacyConfigsPaused += paused
			}
		}
		return nil
	}
	if err := syncOneTenant(report.TenantID); err != nil {
		return nil, err
	}
	if includeGlobal && report.TenantID != "global" {
		if err := syncOneTenant("global"); err != nil {
			return nil, err
		}
	}
	report.SubjectIDs = sortedKeys(subjectSet)
	return report, nil
}

func (s *Service) AssessTrigger(subject *horizonmodels.WatchSubject, trigger *horizonmodels.WatchTrigger) TriggerDecision {
	if subject == nil || trigger == nil {
		return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionNoop, Reason: "missing_subject_or_trigger"}
	}
	if subject.Status != horizonmodels.WatchSubjectStatusActive {
		return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionNoop, Reason: "subject_not_active"}
	}
	directMatch := trigger.MatchMode == horizonmodels.WatchTriggerMatchAnchor || pathSliceContains(trigger.CanonicalPaths, subject.AnchorCanonicalPath)
	if trigger.Confidence < 0.15 {
		return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionNoop, Reason: "low_confidence", DirectMatch: directMatch}
	}
	if parseBool(trigger.Metadata["scoutCompletion"]) {
		return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionWarmOnly, Reason: "scout_completion_observed", DirectMatch: directMatch}
	}
	if trigger.SignalStrength < 0.20 {
		return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionWarmOnly, Reason: "weak_signal", DirectMatch: directMatch}
	}
	switch trigger.TriggerType {
	case horizonmodels.WatchTriggerRecommendationApproved:
		if strings.TrimSpace(asString(trigger.Metadata["jobId"])) != "" {
			return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionWarmOnly, Reason: "recommendation_job_already_started", DirectMatch: true}
		}
		return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionEnqueueScout, Reason: "approved_recommendation_requires_enrichment", DirectMatch: true}
	case horizonmodels.WatchTriggerPropagationGap:
		if trigger.Confidence < 0.35 || trigger.SignalStrength < 0.30 {
			return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionWarmOnly, Reason: "weak_gap_signal", DirectMatch: directMatch}
		}
		return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionCreateOrRefreshRecommendation, Reason: "propagation_gap_requires_followup", DirectMatch: directMatch}
	case horizonmodels.WatchTriggerFactDelta, horizonmodels.WatchTriggerSourceDelta:
		if subject.SubjectKind == horizonmodels.WatchSubjectKindFragment && directMatch {
			return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionEnqueueRisk, Reason: "fragment_delta_requires_risk_runtime", DirectMatch: true}
		}
		return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionWakeScopeAgent, Reason: "delta_wakes_scope_agent", DirectMatch: directMatch}
	case horizonmodels.WatchTriggerAffectedZoneChanged:
		if directMatch && subject.SubjectKind == horizonmodels.WatchSubjectKindAffectedZone {
			return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionEnqueueRisk, Reason: "zone_anchor_requires_risk_runtime", DirectMatch: true}
		}
		return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionWakeScopeAgent, Reason: "zone_change_wakes_scope_agent", DirectMatch: directMatch}
	case horizonmodels.WatchTriggerSignalDetected:
		if subject.RuntimeMode == horizonmodels.WatchSubjectRuntimeScoutOnly && directMatch {
			return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionEnqueueScout, Reason: "direct_signal_requires_scout_runtime", DirectMatch: true}
		}
		return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionWakeScopeAgent, Reason: "signal_detected_wakes_scope_agent", DirectMatch: directMatch}
	case horizonmodels.WatchTriggerManual:
		if subject.RuntimeMode == horizonmodels.WatchSubjectRuntimeRiskOnly {
			return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionEnqueueRisk, Reason: "manual_risk_runtime", DirectMatch: directMatch}
		}
		if subject.RuntimeMode == horizonmodels.WatchSubjectRuntimeScoutOnly || subject.RuntimeMode == horizonmodels.WatchSubjectRuntimeCombined || subject.RuntimeMode == horizonmodels.WatchSubjectRuntimeGeneralized {
			return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionEnqueueScout, Reason: "manual_scout_runtime", DirectMatch: directMatch}
		}
		return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionWakeScopeAgent, Reason: "manual_scope_wake", DirectMatch: directMatch}
	default:
		return TriggerDecision{Decision: horizonmodels.WatchTriggerDecisionWakeScopeAgent, Reason: "default_scope_wake", DirectMatch: directMatch}
	}
}

func (s *Service) ProcessTrigger(ctx context.Context, trigger *horizonmodels.WatchTrigger) error {
	if s == nil || s.subjectsRepo == nil || trigger == nil {
		return nil
	}
	startedAt := time.Now().UTC()
	subject, err := s.subjectsRepo.Get(ctx, trigger.SubjectID)
	if err != nil || subject == nil {
		return err
	}
	decision := s.AssessTrigger(subject, trigger)
	now := time.Now().UTC()
	trigger.ProcessedAt = &now
	trigger.Decision = decision.Decision
	trigger.DecisionReason = decision.Reason
	trigger.ProcessingError = ""
	if trigger.MatchMode == "" {
		if decision.DirectMatch {
			trigger.MatchMode = horizonmodels.WatchTriggerMatchAnchor
		} else if len(trigger.CanonicalPaths) > 0 {
			trigger.MatchMode = horizonmodels.WatchTriggerMatchUniverse
		}
	}
	var runRefs []string
	var jobRefs []string
	var processErr error
	switch decision.Decision {
	case horizonmodels.WatchTriggerDecisionNoop, horizonmodels.WatchTriggerDecisionWarmOnly:
		if s.recommendationActionHook != nil && parseBool(trigger.Metadata["scoutCompletion"]) {
			processErr = s.recommendationActionHook(ctx, s.recommendationInputFor(subject, trigger))
		}
	case horizonmodels.WatchTriggerDecisionWakeScopeAgent:
		runRefs, processErr = s.wakeScopeAgent(ctx, subject, trigger, decision)
	case horizonmodels.WatchTriggerDecisionEnqueueRisk:
		jobRefs, processErr = s.enqueueRisk(ctx, subject, trigger, decision)
	case horizonmodels.WatchTriggerDecisionEnqueueScout:
		jobRefs, processErr = s.enqueueScout(ctx, subject, trigger, decision)
	case horizonmodels.WatchTriggerDecisionCreateOrRefreshRecommendation:
		if s.recommendationActionHook != nil {
			processErr = s.recommendationActionHook(ctx, s.recommendationInputFor(subject, trigger))
		}
	}
	trigger.RunRefs = compactUnique(append(trigger.RunRefs, runRefs...))
	trigger.JobRefs = compactUnique(append(trigger.JobRefs, jobRefs...))
	if processErr != nil {
		trigger.ProcessingError = strings.TrimSpace(processErr.Error())
	} else {
		s.bumpMetric("watch_trigger_decisions_total")
		s.bumpMetric("watch_trigger_decisions_total:decision=" + string(decision.Decision))
		s.bumpMetric("watch_trigger_decisions_total:reason=" + decision.Reason)
	}
	s.observeLatency(time.Since(startedAt))
	if updateErr := s.triggersRepo.UpdateProcessing(ctx, trigger); updateErr != nil && processErr == nil {
		processErr = updateErr
	}
	return processErr
}

func (s *Service) wakeScopeAgent(ctx context.Context, subject *horizonmodels.WatchSubject, trigger *horizonmodels.WatchTrigger, decision TriggerDecision) ([]string, error) {
	if s == nil || s.wakeHook == nil || subject == nil || trigger == nil {
		return nil, nil
	}
	trace := map[string]any{
		"watchSubjectId": strings.TrimSpace(subject.ID),
		"watchTriggerId": strings.TrimSpace(trigger.ID),
		"decision":       decision.Reason,
		"decisionKind":   string(decision.Decision),
		"triggerType":    string(trigger.TriggerType),
		"canonicalPaths": compactUnique(append([]string{subject.AnchorCanonicalPath}, trigger.CanonicalPaths...)),
	}
	runRefs, err := s.wakeHook(ctx, subject.TenantID, subject.ID, string(trigger.TriggerType), firstNonEmpty(trigger.TriggerReason, decision.Reason), trace)
	if err == nil && len(runRefs) > 0 {
		s.bumpMetric("watch_scope_wakes_total")
	}
	return compactUnique(runRefs), err
}

func (s *Service) enqueueRisk(ctx context.Context, subject *horizonmodels.WatchSubject, trigger *horizonmodels.WatchTrigger, decision TriggerDecision) ([]string, error) {
	if s == nil || s.riskJobsRepo == nil || subject == nil || trigger == nil {
		return nil, nil
	}
	projectID := s.resolveProjectIDForSubject(ctx, subject)
	polarity := strings.ToLower(strings.TrimSpace(firstNonEmpty(trigger.Polarity, asString(trigger.Metadata["polarity"]))))
	if polarity != "opportunity" {
		polarity = "risk"
	}
	job := &horizonmodels.RiskJob{
		TenantID:            strings.TrimSpace(firstNonEmpty(subject.TenantID, "global")),
		ProjectID:           projectID,
		TriggerType:         "watchruntime",
		TriggerRef:          "watchtrigger:" + strings.TrimSpace(trigger.ID),
		DedupKey:            buildRiskJobDedupKey(subject, trigger, projectID, polarity),
		TargetCanonicalPath: strings.TrimSpace(subject.AnchorCanonicalPath),
		Polarity:            polarity,
		CurrentStage:        horizonmodels.RiskStageIdentify,
		CurrentState:        horizonmodels.RiskJobStatePending,
		State: map[string]any{
			"watchSubjectId":      strings.TrimSpace(subject.ID),
			"watchTriggerId":      strings.TrimSpace(trigger.ID),
			"watchDecision":       string(decision.Decision),
			"watchDecisionReason": decision.Reason,
			"matchMode":           string(trigger.MatchMode),
			"triggerType":         string(trigger.TriggerType),
			"signalStrength":      trigger.SignalStrength,
			"confidence":          trigger.Confidence,
			"canonicalPaths":      compactUnique(trigger.CanonicalPaths),
			"sourceIds":           compactUnique(trigger.SourceIDs),
			"semanticWatermark":   strings.TrimSpace(trigger.SemanticWatermark),
		},
	}
	id, _, err := s.riskJobsRepo.Create(ctx, job)
	if err != nil {
		return nil, err
	}
	s.bumpMetric("watch_runtime_jobs_total")
	s.bumpMetric("watch_runtime_jobs_total:kind=risk")
	return []string{"risk:" + strings.TrimSpace(id)}, nil
}

func (s *Service) enqueueScout(ctx context.Context, subject *horizonmodels.WatchSubject, trigger *horizonmodels.WatchTrigger, decision TriggerDecision) ([]string, error) {
	if s == nil || s.scoutingJobsRepo == nil || subject == nil || trigger == nil {
		return nil, nil
	}
	projectID := s.resolveProjectIDForSubject(ctx, subject)
	lensID := strings.TrimSpace(firstNonEmpty(subject.LensID, asString(trigger.Metadata["lensId"])))
	title := strings.TrimSpace(firstNonEmpty(asString(trigger.Metadata["headline"]), trigger.TriggerReason, subject.DisplayLabel))
	state := map[string]any{
		"watchSubjectId":      strings.TrimSpace(subject.ID),
		"watchTriggerId":      strings.TrimSpace(trigger.ID),
		"watchDecision":       string(decision.Decision),
		"watchDecisionReason": decision.Reason,
		"targetCanonicalPath": strings.TrimSpace(subject.AnchorCanonicalPath),
		"currentState":        string(horizonmodels.ScoutingJobStatePending),
		"sourceIds":           compactUnique(trigger.SourceIDs),
		"canonicalPaths":      compactUnique(trigger.CanonicalPaths),
		"matchMode":           string(trigger.MatchMode),
		"signalStrength":      trigger.SignalStrength,
		"confidence":          trigger.Confidence,
		"semanticWatermark":   strings.TrimSpace(trigger.SemanticWatermark),
		"lensId":              lensID,
		"triggerType":         string(trigger.TriggerType),
		"watchActionDedupKey": buildWatchScoutActionDedupKey(subject, trigger, projectID, lensID),
	}
	strategy := horizonmodels.ScoutingStrategySnapshot
	if strings.TrimSpace(asString(trigger.Metadata["recommendationId"])) != "" {
		state["jobKind"] = strategicEnrichmentJobKind
		state["strategicRecommendationId"] = strings.TrimSpace(asString(trigger.Metadata["recommendationId"]))
		state["strategicStage"] = strings.TrimSpace(firstNonEmpty(asString(trigger.Metadata["jobStage"]), strategicEnrichmentStageTarget))
		state["strategicGapReason"] = strings.TrimSpace(asString(trigger.Metadata["reason"]))
		state["strategicDomainContractHint"] = strings.TrimSpace(asString(trigger.Metadata["domainHint"]))
		state["strategicSubjectType"] = strings.TrimSpace(asString(trigger.Metadata["subjectType"]))
		state["keywords"] = compactUnique(append([]string{strings.TrimSpace(subject.AnchorCanonicalPath)}, normalizeAnySlice(trigger.Metadata["expectedMissingTargets"])...))
		state["focus"] = "strategic_gap_enrichment"
		strategy = horizonmodels.ScoutingStrategySnapshot
	}
	job := &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			TenantID:  strings.TrimSpace(firstNonEmpty(subject.TenantID, "global")),
			ProjectID: projectID,
			UserID:    "system",
			AgentID:   "scout",
			Title:     title,
			Status:    coremodels.AgentJobStatusPending,
			Progress:  0,
			UserQuery: title,
			AutoMode:  true,
			PlanMode:  coremodels.PlanModeAuto,
			Task: map[string]any{
				"targetCanonicalPath": strings.TrimSpace(subject.AnchorCanonicalPath),
				"sourceIds":           compactUnique(trigger.SourceIDs),
				"scoutingStrategy":    string(strategy),
			},
			State: state,
		},
		LensID:              lensID,
		TargetCanonicalPath: strings.TrimSpace(subject.AnchorCanonicalPath),
		SourceIDs:           compactUnique(trigger.SourceIDs),
		ScoutingStrategy:    string(strategy),
		BillingContext: horizonmodels.BillingContext{
			ChargeModel: string(horizonmodels.ChargeModelAccess),
			ProjectID:   projectID,
		},
		CurrentState: horizonmodels.ScoutingJobStatePending,
	}
	if _, err := s.scoutingJobsRepo.Create(ctx, job); err != nil {
		return nil, err
	}
	if recID := strings.TrimSpace(asString(trigger.Metadata["recommendationId"])); recID != "" && s.recommendationExecutionHook != nil {
		stage := strings.TrimSpace(firstNonEmpty(asString(trigger.Metadata["jobStage"]), strategicEnrichmentStageTarget))
		if err := s.recommendationExecutionHook(ctx, recID, strings.TrimSpace(job.ID), stage, "Execution started from unified watch runtime."); err != nil {
			return nil, err
		}
	}
	s.bumpMetric("watch_runtime_jobs_total")
	s.bumpMetric("watch_runtime_jobs_total:kind=scout")
	return []string{"scout:" + strings.TrimSpace(job.ID)}, nil
}

func (s *Service) HandleScoutingJobComplete(ctx context.Context, job *horizonmodels.ScoutingJob) error {
	if s == nil || job == nil {
		return nil
	}
	if strings.TrimSpace(asString(job.State["strategicRecommendationId"])) != "" {
		if s.recommendationCompletionHook != nil {
			return s.recommendationCompletionHook(ctx, job)
		}
		return nil
	}
	subjectID := strings.TrimSpace(asString(job.State["watchSubjectId"]))
	tenantID := strings.TrimSpace(firstNonEmpty(job.TenantID, "global"))
	targetCanonicalPath := strings.TrimSpace(firstNonEmpty(job.TargetCanonicalPath, asString(job.State["targetCanonicalPath"])))
	if subjectID != "" {
		_, _, err := s.EmitAndProcessTrigger(ctx, &horizonmodels.WatchTrigger{
			SubjectID:         subjectID,
			TriggerType:       horizonmodels.WatchTriggerManual,
			TriggerReason:     "scouting job completed",
			CanonicalPaths:    compactUnique([]string{targetCanonicalPath}),
			SignalStrength:    0.65,
			Confidence:        0.90,
			SemanticWatermark: firstNonEmpty(strings.TrimSpace(job.ID), strings.TrimSpace(asString(job.State["watchTriggerId"]))),
			Metadata: map[string]any{
				"scoutCompletion":     true,
				"jobId":               strings.TrimSpace(job.ID),
				"currentState":        strings.TrimSpace(string(job.CurrentState)),
				"targetCanonicalPath": targetCanonicalPath,
			},
		})
		return err
	}
	if targetCanonicalPath == "" {
		return nil
	}
	_, err := s.EmitTriggerByAnchor(ctx, tenantID, targetCanonicalPath, horizonmodels.WatchTriggerManual, "scouting job completed", 0.65, 0.90, map[string]any{
		"scoutCompletion":     true,
		"jobId":               strings.TrimSpace(job.ID),
		"currentState":        strings.TrimSpace(string(job.CurrentState)),
		"targetCanonicalPath": targetCanonicalPath,
	})
	return err
}

func (s *Service) recommendationInputFor(subject *horizonmodels.WatchSubject, trigger *horizonmodels.WatchTrigger) RecommendationActionInput {
	input := RecommendationActionInput{
		TenantID:             strings.TrimSpace(firstNonEmpty(subject.TenantID, "global")),
		ProjectID:            strings.TrimSpace(firstNonEmpty(subject.ProjectID, asString(trigger.Metadata["projectId"]))),
		LensID:               strings.TrimSpace(firstNonEmpty(subject.LensID, asString(trigger.Metadata["lensId"]))),
		SubjectCanonicalPath: strings.TrimSpace(firstNonEmpty(subject.AnchorCanonicalPath, firstCanonicalPath(trigger.CanonicalPaths))),
		TriggerType:          trigger.TriggerType,
		TriggerReason:        strings.TrimSpace(trigger.TriggerReason),
		EventTitle:           strings.TrimSpace(firstNonEmpty(asString(trigger.Metadata["headline"]), asString(trigger.Metadata["eventTitle"]))),
		EventSummary:         strings.TrimSpace(firstNonEmpty(asString(trigger.Metadata["summary"]), asString(trigger.Metadata["eventSummary"]), asString(trigger.Metadata["reason"]))),
		SignalAxis:           strings.TrimSpace(asString(trigger.Metadata["signalAxis"])),
		BusinessEndpoints:    asInt(trigger.Metadata["businessEndpoints"]),
		RemoteEndpoints:      asInt(trigger.Metadata["remoteEndpoints"]),
		PathFamilies:         asInt(trigger.Metadata["pathFamilies"]),
		OccurredAt:           time.Now().UTC(),
	}
	switch subject.SubjectKind {
	case horizonmodels.WatchSubjectKindAffectedZone:
		input.SubjectType = horizonmodels.StrategicRecommendationSubjectAffectedZone
	case horizonmodels.WatchSubjectKindLensUniverse:
		input.SubjectType = horizonmodels.StrategicRecommendationSubjectSignalBundle
	default:
		input.SubjectType = horizonmodels.StrategicRecommendationSubjectFragment
	}
	return input
}

func (s *Service) buildWatchUniverse(ctx context.Context, lens *horizonmodels.Lens) []string {
	if lens == nil {
		return nil
	}
	pathSet := map[string]struct{}{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		pathSet[path] = struct{}{}
	}
	for _, path := range lens.Scope {
		add(path)
	}
	for _, paths := range lens.ResolvedQueries {
		for _, path := range paths {
			add(path)
		}
	}
	for key, ref := range lens.ScopeArtifacts {
		add(key)
		for _, path := range ref.NodeRefs {
			add(path)
		}
		for _, path := range ref.GeoRefs {
			add(path)
		}
		for _, path := range ref.FlowRefs {
			add(path)
		}
		for _, path := range ref.ZoneRefs {
			add(path)
		}
		for _, path := range ref.AffectedZoneRefs {
			add(path)
		}
	}
	if s.lensArtifactsRepo != nil {
		if artifacts, err := s.lensArtifactsRepo.ListByLens(ctx, lens.ID, 500); err == nil {
			for _, artifact := range artifacts {
				if artifact == nil {
					continue
				}
				add(artifact.CanonicalPath)
				add(artifact.SourcePath)
				add(artifact.TargetPath)
			}
		}
	}
	if s.fragmentsRepo != nil && s.edgesRepo != nil {
		seenFragments := map[string]struct{}{}
		for _, path := range sortedKeys(pathSet) {
			fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, path)
			if err != nil || fragment == nil || strings.TrimSpace(fragment.ID) == "" {
				continue
			}
			if _, ok := seenFragments[fragment.ID]; ok {
				continue
			}
			seenFragments[fragment.ID] = struct{}{}
			if edges, err := s.edgesRepo.ListBySourceWithOptions(ctx, lens.TenantID, fragment.ID, nil, 24, nil); err == nil {
				for _, edge := range edges {
					if edge == nil {
						continue
					}
					if neighbor, err := s.fragmentsRepo.Get(ctx, edge.TargetFragmentID); err == nil && neighbor != nil {
						add(neighbor.CanonicalPath)
					}
				}
			}
			if edges, err := s.edgesRepo.ListByTargetWithOptions(ctx, lens.TenantID, fragment.ID, nil, 24, nil); err == nil {
				for _, edge := range edges {
					if edge == nil {
						continue
					}
					if neighbor, err := s.fragmentsRepo.Get(ctx, edge.SourceFragmentID); err == nil && neighbor != nil {
						add(neighbor.CanonicalPath)
					}
				}
			}
		}
	}
	return sortedKeys(pathSet)
}

func (s *Service) collectLensBindings(ctx context.Context, tenantID, fragmentID, canonicalPath string) []string {
	set := map[string]struct{}{}
	if s.lensesRepo != nil && strings.TrimSpace(canonicalPath) != "" {
		if lenses, err := s.lensesRepo.FindByScope(ctx, strings.TrimSpace(firstNonEmpty(tenantID, "global")), strings.TrimSpace(canonicalPath), 100); err == nil {
			for _, lens := range lenses {
				if lens != nil && strings.TrimSpace(lens.ID) != "" {
					set[strings.TrimSpace(lens.ID)] = struct{}{}
				}
			}
		}
	}
	if s.lensArtifactsRepo != nil && strings.TrimSpace(fragmentID) != "" {
		if lensIDs, err := s.lensArtifactsRepo.ListLensesByFragment(ctx, strings.TrimSpace(fragmentID), 100); err == nil {
			for _, lensID := range lensIDs {
				lensID = strings.TrimSpace(lensID)
				if lensID != "" {
					set[lensID] = struct{}{}
				}
			}
		}
	}
	return sortedKeys(set)
}

func (s *Service) ensureDerivedScopeAgentConfig(ctx context.Context, subject *horizonmodels.WatchSubject) error {
	if s == nil || s.scopeAgentConfigsRepo == nil || subject == nil {
		return nil
	}
	existing, _ := s.scopeAgentConfigsRepo.ListActiveBySubjectID(ctx, strings.TrimSpace(subject.TenantID), strings.TrimSpace(subject.ID))
	focusMode := focusModeForSubject(subject)
	metadata := map[string]any{
		"watchSubjectId":       strings.TrimSpace(subject.ID),
		"subjectKind":          string(subject.SubjectKind),
		"macroAgentClass":      asString(subject.Metadata["macroAgentClass"]),
		"lensId":               strings.TrimSpace(firstNonEmpty(subject.LensID, asString(subject.Metadata["lensId"]))),
		"artifactCanonicalKey": strings.TrimSpace(asString(subject.Metadata["artifactCanonicalKey"])),
		"interestTitle":        strings.TrimSpace(firstNonEmpty(subject.InterestTitle, asString(subject.Metadata["interestTitle"]))),
		"interestQuery":        strings.TrimSpace(firstNonEmpty(subject.InterestQuery, asString(subject.Metadata["interestQuery"]))),
		"queryOverrides":       compactUnique(append(append([]string{}, subject.QueryOverrides...), parseAnyStringSlice(subject.Metadata["queryOverrides"])...)),
	}
	if len(subject.PageFraming) > 0 {
		metadata["pageFraming"] = cloneMap(subject.PageFraming)
	} else if framing := mapFromAny(subject.Metadata["pageFraming"]); len(framing) > 0 {
		metadata["pageFraming"] = cloneMap(framing)
	}
	cfg := &horizonmodels.ScopeAgentConfig{
		TenantID:           strings.TrimSpace(subject.TenantID),
		SubjectID:          strings.TrimSpace(subject.ID),
		TargetFragmentID:   strings.TrimSpace(subject.AnchorFragmentID),
		Name:               fmt.Sprintf("%s watch subject", strings.TrimSpace(subject.DisplayLabel)),
		ScopeCanonicalPath: strings.TrimSpace(subject.AnchorCanonicalPath),
		ScopeType:          scopeTypeForSubject(subject.SubjectKind),
		DomainModes:        append([]string(nil), subject.DomainModes...),
		Roles:              normalizeRoles(subject.Roles),
		Priority:           normalizedPriority(subject.Priority),
		Status:             string(horizonmodels.ScopeAgentStatusRunning),
		ExpansionPolicy: map[string]any{
			"focusMode":        focusMode,
			"watchUniverse":    true,
			"universeSizeHint": len(subject.UniversePaths),
		},
		Schedule: map[string]any{
			"debounceSeconds":    180,
			"batchWindowSeconds": 180,
		},
		LLMPolicy: horizonmodels.LLMPolicy{
			PrimaryModel:         "gemini-3-flash-preview",
			EscalationModel:      "gemini-3-pro-preview",
			PreferredVertexModel: "vertex-gemini-3-flash-preview",
			FallbackModels:       []string{"gemini-3.1-flash-lite-preview", "gemini-2.5-flash", "gemini-2.5-pro"},
			UseSearchGrounding:   true,
			MaxBatchSize:         8,
			BatchWindowSeconds:   180,
		},
		Metadata: metadata,
	}
	if err := s.scopeAgentConfigsRepo.Upsert(ctx, cfg); err != nil {
		return err
	}
	if len(existing) == 0 {
		s.bumpMetric("watch_scope_configs_ensured_total")
	}
	return nil
}

func (s *Service) backfillTargetArtifacts(ctx context.Context, target *horizonmodels.WatchTarget, subject *horizonmodels.WatchSubject) (int, error) {
	if s == nil || target == nil || subject == nil {
		return 0, nil
	}
	linked := 0
	if s.watchBriefsRepo != nil && strings.TrimSpace(target.LastBriefID) != "" {
		brief, err := s.watchBriefsRepo.Get(ctx, strings.TrimSpace(target.LastBriefID))
		if err == nil && brief != nil && strings.TrimSpace(brief.SubjectID) != strings.TrimSpace(subject.ID) {
			brief.SubjectID = strings.TrimSpace(subject.ID)
			if err := s.watchBriefsRepo.Upsert(ctx, brief); err != nil {
				return linked, err
			}
			linked++
		}
	}
	if s.watchFeedEntriesRepo != nil && strings.TrimSpace(target.ID) != "" {
		entries, err := s.watchFeedEntriesRepo.ListByTarget(ctx, strings.TrimSpace(target.TenantID), strings.TrimSpace(target.ID), 50)
		if err != nil {
			return linked, err
		}
		for _, entry := range entries {
			if entry == nil || strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.SubjectID) == strings.TrimSpace(subject.ID) {
				continue
			}
			entry.SubjectID = strings.TrimSpace(subject.ID)
			if err := s.watchFeedEntriesRepo.Update(ctx, entry); err != nil {
				return linked, err
			}
			linked++
		}
	}
	return linked, nil
}

func (s *Service) backfillScopeRunsForConfig(ctx context.Context, cfg *horizonmodels.ScopeAgentConfig, subjectID string, limit int) (int, error) {
	if s == nil || s.scopeAgentRunsRepo == nil || cfg == nil || strings.TrimSpace(cfg.ID) == "" || strings.TrimSpace(subjectID) == "" {
		return 0, nil
	}
	runs, err := s.scopeAgentRunsRepo.ListByConfigID(ctx, strings.TrimSpace(cfg.ID), limit)
	if err != nil {
		return 0, err
	}
	linked := 0
	for _, run := range runs {
		if run == nil || strings.TrimSpace(run.ID) == "" || strings.TrimSpace(run.SubjectID) == strings.TrimSpace(subjectID) {
			continue
		}
		run.SubjectID = strings.TrimSpace(subjectID)
		if err := s.scopeAgentRunsRepo.Update(ctx, run); err != nil {
			return linked, err
		}
		linked++
	}
	return linked, nil
}

func (s *Service) pauseLegacyScopeConfigs(ctx context.Context, configs []*horizonmodels.ScopeAgentConfig) (int, error) {
	if s == nil || s.scopeAgentConfigsRepo == nil || len(configs) == 0 {
		return 0, nil
	}
	byScope := make(map[string][]*horizonmodels.ScopeAgentConfig)
	for _, cfg := range configs {
		if cfg == nil || strings.TrimSpace(cfg.ID) == "" || !strings.EqualFold(strings.TrimSpace(cfg.Status), string(horizonmodels.ScopeAgentStatusRunning)) {
			continue
		}
		scope := strings.TrimSpace(cfg.ScopeCanonicalPath)
		if scope == "" {
			continue
		}
		byScope[scope] = append(byScope[scope], cfg)
	}
	paused := 0
	for _, group := range byScope {
		if len(group) < 2 {
			continue
		}
		sort.SliceStable(group, func(i, j int) bool {
			left := strings.TrimSpace(group[i].SubjectID) != ""
			right := strings.TrimSpace(group[j].SubjectID) != ""
			if left != right {
				return left
			}
			return group[i].UpdatedAt.After(group[j].UpdatedAt)
		})
		keeper := group[0]
		for _, cfg := range group[1:] {
			if cfg == nil || strings.TrimSpace(cfg.ID) == strings.TrimSpace(keeper.ID) {
				continue
			}
			cfg.Status = string(horizonmodels.ScopeAgentStatusPaused)
			if cfg.Metadata == nil {
				cfg.Metadata = map[string]any{}
			}
			cfg.Metadata["legacyPausedByWatchRuntime"] = true
			cfg.Metadata["legacyPausedForSubjectId"] = strings.TrimSpace(keeper.SubjectID)
			if err := s.scopeAgentConfigsRepo.Upsert(ctx, cfg); err != nil {
				return paused, err
			}
			paused++
		}
	}
	return paused, nil
}

func (s *Service) resolveProjectIDForSubject(ctx context.Context, subject *horizonmodels.WatchSubject) string {
	if subject == nil {
		return defaultWatchRuntimeProjectID
	}
	if v := strings.TrimSpace(subject.ProjectID); v != "" {
		return v
	}
	if s.fragmentsRepo != nil {
		if strings.TrimSpace(subject.AnchorFragmentID) != "" {
			if fragment, err := s.fragmentsRepo.Get(ctx, strings.TrimSpace(subject.AnchorFragmentID)); err == nil && fragment != nil && fragment.Governance.CreatedByProjectID != nil && strings.TrimSpace(*fragment.Governance.CreatedByProjectID) != "" {
				return strings.TrimSpace(*fragment.Governance.CreatedByProjectID)
			}
		}
		if strings.TrimSpace(subject.AnchorCanonicalPath) != "" {
			if fragment, err := s.fragmentsRepo.GetByCanonicalPath(ctx, strings.TrimSpace(subject.AnchorCanonicalPath)); err == nil && fragment != nil && fragment.Governance.CreatedByProjectID != nil && strings.TrimSpace(*fragment.Governance.CreatedByProjectID) != "" {
				return strings.TrimSpace(*fragment.Governance.CreatedByProjectID)
			}
		}
	}
	return defaultWatchRuntimeProjectID
}

func buildRiskJobDedupKey(subject *horizonmodels.WatchSubject, trigger *horizonmodels.WatchTrigger, projectID, polarity string) string {
	return strings.Join([]string{
		"risk_job_dedup",
		strings.TrimSpace(subject.TenantID),
		strings.TrimSpace(projectID),
		strings.TrimSpace(subject.LensID),
		strings.TrimSpace(subject.AnchorCanonicalPath),
		"semantic:" + buildWatchActionSemanticKey(trigger),
		strings.TrimSpace(polarity),
	}, "|")
}

func buildWatchScoutActionDedupKey(subject *horizonmodels.WatchSubject, trigger *horizonmodels.WatchTrigger, projectID, lensID string) string {
	if subject == nil {
		return ""
	}
	if recID := strings.TrimSpace(asString(trigger.Metadata["recommendationId"])); recID != "" {
		stage := strings.TrimSpace(firstNonEmpty(asString(trigger.Metadata["jobStage"]), strategicEnrichmentStageTarget))
		return strings.Join([]string{
			"watch_scout_dedup",
			strings.TrimSpace(subject.TenantID),
			strings.TrimSpace(projectID),
			strings.TrimSpace(lensID),
			strings.TrimSpace(subject.ID),
			"recommendation:" + recID,
			"stage:" + stage,
		}, "|")
	}
	return strings.Join([]string{
		"watch_scout_dedup",
		strings.TrimSpace(subject.TenantID),
		strings.TrimSpace(projectID),
		strings.TrimSpace(lensID),
		strings.TrimSpace(subject.ID),
		"semantic:" + buildWatchActionSemanticKey(trigger),
	}, "|")
}

func buildWatchActionSemanticKey(trigger *horizonmodels.WatchTrigger) string {
	if trigger == nil {
		return "unknown"
	}
	base := firstNonEmpty(strings.TrimSpace(trigger.SemanticWatermark), semanticWatermarkFromTrigger(trigger))
	if strings.TrimSpace(base) == "" {
		base = "trigger:" + strings.TrimSpace(trigger.ID)
	}
	return strings.TrimSpace(base)
}

func sourceModeFromWatchdog(cfg *horizonmodels.WatchdogConfig) horizonmodels.WatchSubjectSourceMode {
	if cfg == nil {
		return horizonmodels.WatchSubjectSourceFacts
	}
	switch {
	case cfg.UseProjectSources && len(cfg.SourceIDs) > 0:
		return horizonmodels.WatchSubjectSourceHybrid
	case cfg.UseProjectSources:
		return horizonmodels.WatchSubjectSourceProjectSources
	case len(cfg.SourceIDs) > 0:
		return horizonmodels.WatchSubjectSourcePinnedSources
	default:
		return horizonmodels.WatchSubjectSourceFacts
	}
}

func buildWatchTriggerDedupeKey(trigger *horizonmodels.WatchTrigger) string {
	if trigger == nil {
		return ""
	}
	paths := compactUnique(trigger.CanonicalPaths)
	sourceIDs := compactUnique(trigger.SourceIDs)
	watermark := firstNonEmpty(strings.TrimSpace(trigger.SemanticWatermark), semanticWatermarkFromTrigger(trigger))
	raw := strings.Join([]string{
		strings.TrimSpace(trigger.SubjectID),
		string(trigger.TriggerType),
		strings.TrimSpace(watermark),
		strings.Join(paths, ","),
		strings.Join(sourceIDs, ","),
		strings.TrimSpace(trigger.Polarity),
		string(trigger.MatchMode),
	}, "|")
	sum := sha1.Sum([]byte(raw))
	return "watch_trigger:" + hex.EncodeToString(sum[:])
}

func scopeTypeForSubject(kind horizonmodels.WatchSubjectKind) string {
	switch kind {
	case horizonmodels.WatchSubjectKindLensUniverse:
		return "lens_universe"
	case horizonmodels.WatchSubjectKindPathOverlay:
		return "path_overlay"
	case horizonmodels.WatchSubjectKindFragment:
		return "fragment"
	case horizonmodels.WatchSubjectKindAffectedZone:
		return "affected_zone"
	case horizonmodels.WatchSubjectKindMarket:
		return "market"
	case horizonmodels.WatchSubjectKindGeography:
		return "geography"
	default:
		return "watch_subject"
	}
}

func focusModeForSubject(subject *horizonmodels.WatchSubject) string {
	if subject == nil {
		return "watch_subject"
	}
	switch subject.SubjectKind {
	case horizonmodels.WatchSubjectKindLensUniverse:
		return "lens_universe"
	case horizonmodels.WatchSubjectKindPathOverlay:
		return "artifact_overlay"
	case horizonmodels.WatchSubjectKindMarket:
		return "macro_market"
	case horizonmodels.WatchSubjectKindGeography:
		return "macro_country"
	case horizonmodels.WatchSubjectKindExplicitTarget:
		if strings.EqualFold(asString(subject.Metadata["macroAgentClass"]), string(horizonmodels.MacroAgentClassEntity)) {
			return "macro_entity"
		}
		return "watch_subject"
	default:
		return "watch_subject"
	}
}

func lensCanonicalPath(lensID string) string {
	lensID = strings.TrimSpace(lensID)
	if lensID == "" {
		return "/lens/unknown"
	}
	return "/lens/" + lensID
}

func mapTargetStatus(status string) horizonmodels.WatchSubjectStatus {
	switch strings.TrimSpace(status) {
	case string(horizonmodels.WatchTargetStatusPaused):
		return horizonmodels.WatchSubjectStatusPaused
	case string(horizonmodels.WatchTargetStatusDeleted):
		return horizonmodels.WatchSubjectStatusDeleted
	default:
		return horizonmodels.WatchSubjectStatusActive
	}
}

func mapWatchdogStatus(status string) horizonmodels.WatchSubjectStatus {
	switch strings.TrimSpace(status) {
	case string(horizonmodels.WatchdogStatusPaused):
		return horizonmodels.WatchSubjectStatusPaused
	default:
		return horizonmodels.WatchSubjectStatusActive
	}
}

func mapLensStatus(status string) horizonmodels.WatchSubjectStatus {
	switch strings.TrimSpace(status) {
	case string(horizonmodels.LensStatusArchived):
		return horizonmodels.WatchSubjectStatusPaused
	default:
		return horizonmodels.WatchSubjectStatusActive
	}
}

func fragmentDisplayName(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if fragment.Labels != nil {
		if v := strings.TrimSpace(fragment.Labels["en"]); v != "" {
			return v
		}
		for _, value := range fragment.Labels {
			if v := strings.TrimSpace(value); v != "" {
				return v
			}
		}
	}
	return strings.TrimSpace(fragment.CanonicalPath)
}

func normalizeRoles(in []string) []string {
	if len(in) == 0 {
		return []string{"monitor"}
	}
	return compactUnique(in)
}

func normalizedPriority(v int) int {
	if v <= 0 {
		return 50
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func pathSliceContains(paths []string, canonicalPath string) bool {
	canonicalPath = strings.TrimSpace(canonicalPath)
	if canonicalPath == "" {
		return false
	}
	for _, path := range paths {
		if strings.TrimSpace(path) == canonicalPath {
			return true
		}
	}
	return false
}

func firstCanonicalPath(paths []string) string {
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path != "" {
			return path
		}
	}
	return ""
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float32:
		return int(t)
	case float64:
		return int(t)
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return 0
		}
		var out int
		fmt.Sscanf(t, "%d", &out)
		return out
	default:
		return 0
	}
}

func parseBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "1", "true", "yes":
			return true
		}
	}
	return false
}

func normalizeAnySlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return compactUnique(t)
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := asString(item); s != "" {
				out = append(out, s)
			}
		}
		return compactUnique(out)
	default:
		if s := asString(v); s != "" && s != "<nil>" {
			return []string{s}
		}
		return nil
	}
}

func parseStringSlice(v any) []string {
	return normalizeAnySlice(v)
}

func parseAnyStringSlice(v any) []string {
	return normalizeAnySlice(v)
}

func semanticWatermarkFromTrigger(trigger *horizonmodels.WatchTrigger) string {
	if trigger == nil {
		return ""
	}
	candidates := []string{
		asString(trigger.Metadata["semanticWatermark"]),
		asString(trigger.Metadata["watermark"]),
		asString(trigger.Metadata["factWatermark"]),
		asString(trigger.Metadata["sourceDocumentId"]),
		asString(trigger.Metadata["documentId"]),
		asString(trigger.Metadata["zoneUpdatedAt"]),
		asString(trigger.Metadata["jobId"]),
		asString(trigger.Metadata["recommendationId"]),
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) != "" && strings.TrimSpace(candidate) != "<nil>" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

func (s *Service) bumpMetric(key string) {
	if s == nil || s.metrics == nil || strings.TrimSpace(key) == "" {
		return
	}
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()
	s.metrics.counters[strings.TrimSpace(key)]++
	s.metrics.lastUpdatedAt = time.Now().UTC()
}

func (s *Service) observeLatency(d time.Duration) {
	if s == nil || s.metrics == nil {
		return
	}
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()
	s.metrics.latencyCount++
	s.metrics.latencyTotalMs += float64(d.Milliseconds())
	s.metrics.lastUpdatedAt = time.Now().UTC()
}

func compactUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func sortedKeys[T any](in map[string]T) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func mergeSubjectsByID(groups ...[]*horizonmodels.WatchSubject) []*horizonmodels.WatchSubject {
	byID := make(map[string]*horizonmodels.WatchSubject)
	for _, group := range groups {
		for _, subject := range group {
			if subject == nil || strings.TrimSpace(subject.ID) == "" {
				continue
			}
			byID[strings.TrimSpace(subject.ID)] = subject
		}
	}
	out := make([]*horizonmodels.WatchSubject, 0, len(byID))
	for _, subject := range byID {
		out = append(out, subject)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].ID) < strings.TrimSpace(out[j].ID)
	})
	return out
}
