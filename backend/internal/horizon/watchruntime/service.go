package watchruntime

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
)

type SubjectWakeHook func(ctx context.Context, tenantID, subjectID, triggerType, triggerReason string, trace map[string]any) error

type Service struct {
	subjectsRepo          *horizonrepo.WatchSubjectsRepository
	triggersRepo          *horizonrepo.WatchTriggersRepository
	targetsRepo           *horizonrepo.WatchTargetsRepository
	watchdogRepo          *horizonrepo.WatchdogConfigsRepository
	scopeAgentConfigsRepo *horizonrepo.ScopeAgentConfigsRepository
	lensesRepo            *horizonrepo.LensesRepository
	lensArtifactsRepo     *horizonrepo.LensArtifactsRepository
	fragmentsRepo         *horizonrepo.HorizonFragmentsRepository
	edgesRepo             *horizonrepo.DependencyEdgesRepository
	wakeHook              SubjectWakeHook
}

type TriggerDecision struct {
	WakeScopeAgent bool
	WarmOnly       bool
	Noop           bool
	Reason         string
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
	}
}

func (s *Service) WithWakeHook(fn SubjectWakeHook) *Service {
	if s == nil {
		return s
	}
	s.wakeHook = fn
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
		Status:              mapTargetStatus(target.Status),
		SourceMode:          sourceMode,
		RuntimeMode:         runtimeMode,
		DomainModes:         append([]string(nil), target.DomainModes...),
		Roles:               normalizeRoles(target.Roles),
		Priority:            normalizedPriority(target.Priority),
		LensBindings:        s.collectLensBindings(ctx, target.TenantID, target.FragmentID, target.CanonicalPath),
		UniversePaths:       compactUnique([]string{strings.TrimSpace(target.CanonicalPath)}),
		Metadata: map[string]any{
			"kind":          strings.TrimSpace(target.Kind),
			"watchTargetId": strings.TrimSpace(target.ID),
			"source":        strings.TrimSpace(target.Source),
		},
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
		Status:              mapWatchdogStatus(cfg.Status),
		SourceMode:          sourceModeFromWatchdog(cfg),
		RuntimeMode:         horizonmodels.WatchSubjectRuntimeCustomFragment,
		Roles:               []string{"monitor", "risk", "scout"},
		Priority:            60,
		LensBindings:        s.collectLensBindings(ctx, cfg.TenantID, fragment.ID, fragment.CanonicalPath),
		UniversePaths:       compactUnique([]string{strings.TrimSpace(fragment.CanonicalPath)}),
		Metadata: map[string]any{
			"watchdogConfigId": strings.TrimSpace(cfg.ID),
		},
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
	if err := s.subjectsRepo.Upsert(ctx, subject); err != nil {
		return err
	}
	return s.ensureDerivedScopeAgentConfig(ctx, subject)
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
	if strings.TrimSpace(trigger.DedupeKey) == "" {
		trigger.DedupeKey = buildWatchTriggerDedupeKey(trigger)
	}
	return s.triggersRepo.CreateOrRefresh(ctx, trigger)
}

func (s *Service) EmitAndProcessTrigger(ctx context.Context, trigger *horizonmodels.WatchTrigger) (*horizonmodels.WatchTrigger, bool, error) {
	if s == nil || trigger == nil {
		return nil, false, horizonrepo.ErrInvalidDocument
	}
	stored, created, err := s.EmitTrigger(ctx, trigger)
	if err != nil || stored == nil {
		return stored, created, err
	}
	return stored, created, s.ProcessTrigger(ctx, stored)
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

func (s *Service) AssessTrigger(subject *horizonmodels.WatchSubject, trigger *horizonmodels.WatchTrigger) TriggerDecision {
	if subject == nil || trigger == nil {
		return TriggerDecision{Noop: true, Reason: "missing_subject_or_trigger"}
	}
	if trigger.Confidence < 0.20 {
		return TriggerDecision{Noop: true, Reason: "low_confidence"}
	}
	if subject.Status != horizonmodels.WatchSubjectStatusActive {
		return TriggerDecision{Noop: true, Reason: "subject_not_active"}
	}
	if trigger.SignalStrength < 0.20 {
		return TriggerDecision{WarmOnly: true, Reason: "weak_signal"}
	}
	return TriggerDecision{WakeScopeAgent: true, Reason: "wake_scope_agent"}
}

func (s *Service) ProcessTrigger(ctx context.Context, trigger *horizonmodels.WatchTrigger) error {
	if s == nil || s.subjectsRepo == nil || trigger == nil {
		return nil
	}
	subject, err := s.subjectsRepo.Get(ctx, trigger.SubjectID)
	if err != nil || subject == nil {
		return err
	}
	decision := s.AssessTrigger(subject, trigger)
	if !decision.WakeScopeAgent || s.wakeHook == nil {
		return nil
	}
	trace := map[string]any{
		"watchSubjectId": strings.TrimSpace(subject.ID),
		"watchTriggerId": strings.TrimSpace(trigger.ID),
		"decision":       decision.Reason,
		"triggerType":    string(trigger.TriggerType),
		"canonicalPaths": compactUnique(append([]string{subject.AnchorCanonicalPath}, trigger.CanonicalPaths...)),
	}
	return s.wakeHook(ctx, subject.TenantID, subject.ID, string(trigger.TriggerType), firstNonEmpty(trigger.TriggerReason, decision.Reason), trace)
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
			"focusMode":        "watch_subject",
			"watchUniverse":    true,
			"universeSizeHint": len(subject.UniversePaths),
		},
		Schedule: map[string]any{
			"debounceSeconds":    180,
			"batchWindowSeconds": 180,
		},
		LLMPolicy: horizonmodels.LLMPolicy{
			PrimaryModel:         "gemini-3.1-flash-lite-preview",
			EscalationModel:      "gemini-3-pro-preview",
			PreferredVertexModel: "gemini-3.1-flash-lite-preview",
			UseSearchGrounding:   true,
			MaxBatchSize:         8,
			BatchWindowSeconds:   180,
		},
		Metadata: map[string]any{
			"watchSubjectId": strings.TrimSpace(subject.ID),
			"subjectKind":    string(subject.SubjectKind),
		},
	}
	return s.scopeAgentConfigsRepo.Upsert(ctx, cfg)
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
	raw := strings.Join([]string{
		strings.TrimSpace(trigger.SubjectID),
		string(trigger.TriggerType),
		strings.TrimSpace(trigger.TriggerReason),
		strings.Join(paths, ","),
		strings.Join(sourceIDs, ","),
		strings.TrimSpace(trigger.Polarity),
		fmt.Sprintf("%.4f", trigger.SignalStrength),
		fmt.Sprintf("%.4f", trigger.Confidence),
	}, "|")
	sum := sha1.Sum([]byte(raw))
	return "watch_trigger:" + hex.EncodeToString(sum[:])
}

func scopeTypeForSubject(kind horizonmodels.WatchSubjectKind) string {
	switch kind {
	case horizonmodels.WatchSubjectKindLensUniverse:
		return "lens_universe"
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
