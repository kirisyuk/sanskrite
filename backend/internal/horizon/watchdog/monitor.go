package watchdog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// Monitor осуществляет мониторинг источников и обнаружение изменений
type Monitor struct {
	dataSourcesRepo    *repo.DataSourcesRepository
	documentsRepo      *repo.DocumentsRepository
	fragmentsRepo      *horizonrepo.HorizonFragmentsRepository
	factsRepo          *repo.OntologyFactsRepository
	scoutingJobsRepo   *horizonrepo.ScoutingJobsRepository
	riskJobsRepo       *horizonrepo.RiskJobsRepository
	scoutEngine        *scout.ScoutEngine
	executor           *scout.SourceExecutor
	changeAssessor     *ChangeAssessor
	embeddings         ai.EmbeddingProvider
	tuning             changeTuning
	scopeAgentWakeHook func(ctx context.Context, tenantID, canonicalPath, triggerType, triggerReason string, trace map[string]any) error
	triggerHook        func(ctx context.Context, config *horizonmodels.WatchdogConfig, fragment *horizonmodels.HorizonFragment, trigger *horizonmodels.WatchTrigger) error
}

// NewMonitor создает новый мониторинг
func NewMonitor(
	dataSourcesRepo *repo.DataSourcesRepository,
	documentsRepo *repo.DocumentsRepository,
	fragmentsRepo *horizonrepo.HorizonFragmentsRepository,
	factsRepo *repo.OntologyFactsRepository,
	scoutingJobsRepo *horizonrepo.ScoutingJobsRepository,
	scoutEngine *scout.ScoutEngine,
	executor *scout.SourceExecutor,
	aiExecutor *ai.AITaskExecutor,
) *Monitor {
	var emb ai.EmbeddingProvider
	if aiExecutor != nil {
		// Prefer OpenAI embeddings (if configured). If not available in the executor, embedding calls will fail and we will fall back.
		emb = ai.NewOpenAIEmbedding(aiExecutor, "text-embedding-3-small")
	}
	return &Monitor{
		dataSourcesRepo:  dataSourcesRepo,
		documentsRepo:    documentsRepo,
		fragmentsRepo:    fragmentsRepo,
		factsRepo:        factsRepo,
		scoutingJobsRepo: scoutingJobsRepo,
		scoutEngine:      scoutEngine,
		executor:         executor,
		changeAssessor:   NewChangeAssessor(aiExecutor),
		embeddings:       emb,
		tuning:           defaultChangeTuning(),
	}
}

func (m *Monitor) WithRiskJobsRepository(r *horizonrepo.RiskJobsRepository) *Monitor {
	m.riskJobsRepo = r
	return m
}

func (m *Monitor) WithScopeAgentWakeHook(hook func(ctx context.Context, tenantID, canonicalPath, triggerType, triggerReason string, trace map[string]any) error) *Monitor {
	m.scopeAgentWakeHook = hook
	return m
}

func (m *Monitor) WithTriggerHook(hook func(ctx context.Context, config *horizonmodels.WatchdogConfig, fragment *horizonmodels.HorizonFragment, trigger *horizonmodels.WatchTrigger) error) *Monitor {
	m.triggerHook = hook
	return m
}

// ScanSources сканирует источники для WatchdogConfig
func (m *Monitor) ScanSources(ctx context.Context, config *horizonmodels.WatchdogConfig, budget *RunBudget) error {
	// Preferred mode: process normalized ontology facts (incremental, no source fetch).
	if m.factsRepo != nil {
		return m.scanFacts(ctx, config)
	}
	if m.executor == nil {
		return fmt.Errorf("source executor not configured")
	}
	// Получаем источники для сканирования
	var sources []*models.DataSource
	var err error

	if config.UseProjectSources {
		// Получаем все источники проекта через fragment
		if m.fragmentsRepo == nil {
			return fmt.Errorf("fragments repo not configured")
		}
		fragment, err := m.fragmentsRepo.Get(ctx, config.TargetFragmentID)
		if err != nil {
			return fmt.Errorf("failed to get fragment for project sources: %w", err)
		}
		if fragment.Governance.CreatedByProjectID == nil || *fragment.Governance.CreatedByProjectID == "" {
			return fmt.Errorf("fragment has no createdByProjectId; cannot use project sources")
		}
		sources, err = m.dataSourcesRepo.ListByProject(ctx, "global", *fragment.Governance.CreatedByProjectID)
		if err != nil {
			return fmt.Errorf("failed to get project sources: %w", err)
		}
	} else {
		sources, err = m.getSourcesByIDs(ctx, config.SourceIDs)
		if err != nil {
			return fmt.Errorf("failed to get sources: %w", err)
		}
	}

	// Сканируем каждый источник
	scanErrs := make([]string, 0)
	for _, source := range sources {
		if err := m.scanSource(ctx, source, config, budget); err != nil {
			logf("phase=scan_source_error sourceId=%s configId=%s error=%v", strings.TrimSpace(source.ID), strings.TrimSpace(config.ID), err)
			scanErrs = append(scanErrs, fmt.Sprintf("sourceId=%s err=%v", strings.TrimSpace(source.ID), err))
		}
	}
	if len(scanErrs) > 0 {
		return fmt.Errorf("scan sources failed: %s", strings.Join(scanErrs, "; "))
	}

	return nil
}

// scanFacts processes incremental ontology facts since the last run for the target fragment canonical path.
// This avoids scanning sources directly and resolves the global/local sources knot.
func (m *Monitor) scanFacts(ctx context.Context, config *horizonmodels.WatchdogConfig) error {
	if m.fragmentsRepo == nil {
		return fmt.Errorf("fragments repo not configured")
	}
	fragment, err := m.fragmentsRepo.Get(ctx, config.TargetFragmentID)
	if err != nil {
		return fmt.Errorf("failed to get fragment: %w", err)
	}
	targetCanonicalPath := strings.TrimSpace(fragment.CanonicalPath)
	if targetCanonicalPath == "" {
		return fmt.Errorf("fragment has empty canonicalPath")
	}

	// Watermark: do not re-process facts older than last scan (or config creation time).
	since := config.CreatedAt
	if config.LastScanAt != nil && config.LastScanAt.After(since) {
		since = *config.LastScanAt
	}

	facts, err := m.factsRepo.ListUpdatedSinceByCanonicalPath(ctx, config.TenantID, targetCanonicalPath, since, 200)
	if err != nil {
		return fmt.Errorf("failed to list facts: %w", err)
	}
	logf("phase=facts_fetch fragmentId=%s canonicalPath=%s since=%s facts=%d", config.TargetFragmentID, targetCanonicalPath, since.UTC().Format(time.RFC3339), len(facts))
	if len(facts) == 0 {
		return nil
	}

	// Watchdog acts as a guardian over risk/opportunity semantics:
	// process only signal facts and trigger by semantic score, not raw confidence.
	signalFacts := make([]*models.OntologyFact, 0, len(facts))
	for _, f := range facts {
		if isSignalRiskOpportunityFact(f) {
			signalFacts = append(signalFacts, f)
		}
	}
	logf("phase=facts_filter fragmentId=%s canonicalPath=%s signalFacts=%d totalFacts=%d", config.TargetFragmentID, targetCanonicalPath, len(signalFacts), len(facts))
	if len(signalFacts) == 0 {
		return nil
	}

	sourceSet := map[string]struct{}{}
	maxSemanticScore := 0.0
	qualifyingSignals := 0
	criticalEscalationSeen := false
	for _, f := range signalFacts {
		score, details := evaluateSignalSemanticScore(f)
		if score > maxSemanticScore {
			maxSemanticScore = score
		}
		if strings.TrimSpace(details.EscalationLevel) == horizonmodels.EscalationLevelCritical {
			criticalEscalationSeen = true
		}
		qualifies := score >= config.TriggerThreshold || strings.TrimSpace(details.EscalationLevel) == horizonmodels.EscalationLevelCritical
		if !qualifies {
			continue
		}
		qualifyingSignals++
		for _, sid := range f.SourceIDs {
			sid = strings.TrimSpace(sid)
			if sid == "" {
				continue
			}
			sourceSet[sid] = struct{}{}
		}
		for _, ref := range f.SourceRefs {
			sid := strings.TrimSpace(ref.SourceID)
			if sid == "" {
				continue
			}
			sourceSet[sid] = struct{}{}
		}
	}
	logf("phase=facts_decision fragmentId=%s canonicalPath=%s maxSemanticScore=%.3f threshold=%.3f qualifyingSignals=%d criticalEscalation=%v sources=%d",
		config.TargetFragmentID, targetCanonicalPath, maxSemanticScore, config.TriggerThreshold, qualifyingSignals, criticalEscalationSeen, len(sourceSet))

	if maxSemanticScore < config.TriggerThreshold && !criticalEscalationSeen {
		return nil
	}

	// Resolve source IDs from qualifying facts.
	sourceIDs := make([]string, 0, len(sourceSet))
	for sid := range sourceSet {
		sourceIDs = append(sourceIDs, sid)
	}
	// Fallback: if facts had no source IDs but config has explicit source pins, keep watchdog deterministic.
	if len(sourceIDs) == 0 && len(config.SourceIDs) > 0 {
		sourceIDs = append(sourceIDs, config.SourceIDs...)
		logf("phase=facts_decision fallback=config_sources fragmentId=%s sourceIds=%d", config.TargetFragmentID, len(sourceIDs))
	}
	// If still empty, we still create a job (planning mode) to avoid silent watchdog no-op.
	if len(sourceIDs) == 0 {
		logf("phase=facts_decision warning=no_sources fragmentId=%s canonicalPath=%s creating_planning_job=true", config.TargetFragmentID, targetCanonicalPath)
	}

	// Risk runtime is the only valid watchdog execution target for risk semantics.
	if m.riskJobsRepo == nil {
		return fmt.Errorf("risk jobs repo not configured for watchdog risk runtime")
	}
	polarity := dominantPolarityFromFacts(signalFacts)
	if err := m.createRiskJobFromWatchdog(ctx, config, fragment, sourceIDs, polarity, maxSemanticScore, qualifyingSignals); err != nil {
		return err
	}
	m.emitUnifiedTrigger(ctx, config, fragment, &horizonmodels.WatchTrigger{
		SubjectID:      strings.TrimSpace(config.SubjectID),
		TriggerType:    horizonmodels.WatchTriggerFactDelta,
		TriggerReason:  fmt.Sprintf("watchdog facts crossed threshold for %s", targetCanonicalPath),
		CanonicalPaths: []string{targetCanonicalPath},
		SourceIDs:      append([]string(nil), sourceIDs...),
		Polarity:       polarity,
		SignalStrength: clamp01(maxSemanticScore),
		Confidence:     clamp01(maxSemanticScore),
		Metadata: map[string]any{
			"watchdogConfigId":  strings.TrimSpace(config.ID),
			"qualifyingSignals": qualifyingSignals,
		},
	})
	return nil
}

func (m *Monitor) createRiskJobFromWatchdog(
	ctx context.Context,
	config *horizonmodels.WatchdogConfig,
	fragment *horizonmodels.HorizonFragment,
	sourceIDs []string,
	polarity string,
	maxSemanticScore float64,
	qualifyingSignals int,
) error {
	if m.riskJobsRepo == nil {
		return fmt.Errorf("risk jobs repo not configured")
	}
	if fragment == nil || strings.TrimSpace(fragment.CanonicalPath) == "" {
		return fmt.Errorf("fragment canonical path is required for risk trigger")
	}
	polarity = strings.ToLower(strings.TrimSpace(polarity))
	if polarity != "risk" && polarity != "opportunity" {
		polarity = "risk"
	}

	projectID := ""
	if m.dataSourcesRepo != nil && len(sourceIDs) > 0 {
		if pid, err := m.dataSourcesRepo.ResolveProjectIDForSource(ctx, fragment.TenantID, strings.TrimSpace(sourceIDs[0])); err == nil {
			projectID = strings.TrimSpace(pid)
		}
	}
	if projectID == "" && fragment.Governance.CreatedByProjectID != nil {
		projectID = strings.TrimSpace(*fragment.Governance.CreatedByProjectID)
	}
	if projectID == "" {
		return fmt.Errorf("cannot resolve projectId for risk job")
	}

	triggerRef := "watchdog:" + strings.TrimSpace(config.ID)
	dedupKey := strings.Join([]string{
		"risk_job_dedup",
		strings.TrimSpace(fragment.TenantID),
		projectID,
		"",
		strings.TrimSpace(fragment.CanonicalPath),
		triggerRef,
		polarity,
	}, "|")

	job := &horizonmodels.RiskJob{
		TenantID:            strings.TrimSpace(fragment.TenantID),
		ProjectID:           projectID,
		TriggerType:         "watchdog",
		TriggerRef:          triggerRef,
		DedupKey:            dedupKey,
		TargetCanonicalPath: strings.TrimSpace(fragment.CanonicalPath),
		Polarity:            polarity,
		CurrentStage:        horizonmodels.RiskStageIdentify,
		CurrentState:        horizonmodels.RiskJobStatePending,
		State: map[string]any{
			"watchdogConfigId":    strings.TrimSpace(config.ID),
			"sourceIds":           sourceIDs,
			"triggerThreshold":    config.TriggerThreshold,
			"maxSemanticScore":    maxSemanticScore,
			"qualifyingSignals":   qualifyingSignals,
			"targetFragmentId":    strings.TrimSpace(fragment.ID),
			"targetCanonicalPath": strings.TrimSpace(fragment.CanonicalPath),
		},
	}
	id, created, err := m.riskJobsRepo.Create(ctx, job)
	if err != nil {
		return fmt.Errorf("failed to create risk job: %w", err)
	}
	logf("phase=risk_job_enqueued configId=%s fragmentId=%s canonicalPath=%s polarity=%s jobId=%s created=%v", config.ID, fragment.ID, fragment.CanonicalPath, polarity, id, created)
	if m.scopeAgentWakeHook != nil {
		_ = m.scopeAgentWakeHook(ctx, fragment.TenantID, fragment.CanonicalPath, "watchdog", fmt.Sprintf("watchdog:%s:%s", config.ID, polarity), map[string]any{
			"watchdogConfigId":    strings.TrimSpace(config.ID),
			"riskJobId":           id,
			"created":             created,
			"polarity":            polarity,
			"maxSemanticScore":    maxSemanticScore,
			"qualifyingSignals":   qualifyingSignals,
			"targetFragmentId":    strings.TrimSpace(fragment.ID),
			"targetCanonicalPath": strings.TrimSpace(fragment.CanonicalPath),
		})
	}
	return nil
}

func dominantPolarityFromFacts(facts []*models.OntologyFact) string {
	risk := 0
	oppty := 0
	for _, f := range facts {
		if f == nil {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(f.Type))
		switch {
		case strings.HasPrefix(typ, "opportunity:"):
			oppty++
		case strings.HasPrefix(typ, "risk:"):
			risk++
		default:
			for _, t := range f.Tags {
				tt := strings.ToLower(strings.TrimSpace(t))
				if strings.HasPrefix(tt, "signal_polarity:") {
					pol := strings.TrimSpace(strings.TrimPrefix(tt, "signal_polarity:"))
					if pol == "opportunity" {
						oppty++
					}
					if pol == "risk" {
						risk++
					}
				}
			}
		}
	}
	if oppty > risk {
		return "opportunity"
	}
	return "risk"
}

type signalSemanticDetails struct {
	Polarity        string
	Axis            string
	Impact          float64
	Likelihood      float64
	Confidence      float64
	Modality        string
	TimeHorizon     string
	EscalationLevel string
}

func isSignalRiskOpportunityFact(f *models.OntologyFact) bool {
	if f == nil || strings.TrimSpace(f.Kind) != "signal" {
		return false
	}
	polarity, _ := parseSignalType(strings.TrimSpace(f.Type))
	return polarity == "risk" || polarity == "opportunity"
}

func evaluateSignalSemanticScore(f *models.OntologyFact) (float64, signalSemanticDetails) {
	details := signalSemanticDetails{
		Confidence: clamp01(f.Confidence),
	}
	details.Polarity, details.Axis = parseSignalType(strings.TrimSpace(f.Type))

	kv := parseSummaryKeyValues(strings.TrimSpace(f.Summary))
	tagFloat := func(prefix string) float64 {
		if v, ok := getTagValue(f.Tags, prefix); ok {
			return clamp01(parseClampedFloat(v))
		}
		return -1
	}
	summaryFloat := func(k string, fallback float64) float64 {
		if v, ok := kv[strings.ToLower(strings.TrimSpace(k))]; ok {
			return clamp01(parseClampedFloat(v))
		}
		return fallback
	}

	impact := tagFloat("signal_impact:")
	if impact < 0 {
		impact = summaryFloat("impact", -1)
	}
	if impact < 0 {
		impact = summaryFloat("amplitude", details.Confidence)
	}

	likelihood := tagFloat("signal_likelihood:")
	if likelihood < 0 {
		likelihood = summaryFloat("likelihood", details.Confidence)
	}

	modality := strings.TrimSpace(getSignalSemanticString(f, "signal_modality:", kv, "modality"))
	if modality != "" {
		modality = horizonmodels.NormalizeSignalModality(modality)
	}
	if modality == "" {
		modality = horizonmodels.SignalModalityPotential
	}

	timeHorizon := strings.TrimSpace(getSignalSemanticString(f, "signal_horizon:", kv, "horizon"))
	if timeHorizon == "" {
		timeHorizon = strings.TrimSpace(getSignalSemanticString(f, "signal_time_horizon:", kv, "timehorizon"))
	}
	if h := horizonmodels.NormalizeSignalTimeHorizon(timeHorizon); h != "" {
		timeHorizon = h
	}

	escalation := strings.ToLower(strings.TrimSpace(getSignalSemanticString(f, "signal_escalation:", kv, "escalation")))
	if escalation == "" {
		escalation = horizonmodels.EscalationLevelNone
	}
	if !horizonmodels.IsValidEscalationLevel(escalation) {
		escalation = horizonmodels.EscalationLevelNone
	}

	details.Impact = clamp01(impact)
	details.Likelihood = clamp01(likelihood)
	details.Modality = modality
	details.TimeHorizon = timeHorizon
	details.EscalationLevel = escalation

	// Tail-aware base:
	// expected (impact*likelihood) + discounted tail-impact component.
	base := details.Impact*details.Likelihood + 0.5*details.Impact*(1.0-details.Likelihood)
	score := base
	score *= modalityWeight(modality)
	score *= horizonWeight(timeHorizon)
	score *= (0.7 + 0.3*details.Confidence)
	score += escalationBoost(escalation)
	return clamp01(score), details
}

func getSignalSemanticString(f *models.OntologyFact, tagPrefix string, summaryKV map[string]string, summaryKey string) string {
	if f != nil {
		if v, ok := getTagValue(f.Tags, tagPrefix); ok {
			return strings.TrimSpace(v)
		}
	}
	if summaryKV != nil {
		if v, ok := summaryKV[strings.ToLower(strings.TrimSpace(summaryKey))]; ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseSignalType(signalType string) (polarity string, axis string) {
	parts := strings.Split(strings.TrimSpace(signalType), ":")
	if len(parts) >= 1 {
		polarity = strings.TrimSpace(parts[0])
	}
	if len(parts) >= 2 {
		axis = strings.TrimSpace(parts[1])
	}
	return polarity, axis
}

func parseSummaryKeyValues(summary string) map[string]string {
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	out := map[string]string{}
	parts := strings.Split(summary, ";")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		idx := strings.Index(p, ":")
		if idx <= 0 || idx >= len(p)-1 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(p[:idx]))
		val := strings.TrimSpace(p[idx+1:])
		if key == "" || val == "" {
			continue
		}
		out[key] = val
	}
	return out
}

func getTagValue(tags []string, prefix string) (string, bool) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", false
	}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(strings.ToLower(t), strings.ToLower(prefix)) {
			return strings.TrimSpace(t[len(prefix):]), true
		}
	}
	return "", false
}

func parseClampedFloat(raw string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	return clamp01(v)
}

func modalityWeight(modality string) float64 {
	switch strings.TrimSpace(modality) {
	case horizonmodels.SignalModalityRhetoric:
		return 0.55
	case horizonmodels.SignalModalityPotential:
		return 0.70
	case horizonmodels.SignalModalityProjected:
		return 0.80
	case horizonmodels.SignalModalityEscalation:
		return 0.95
	case horizonmodels.SignalModalityDisruption:
		return 1.05
	case horizonmodels.SignalModalityConfirmed:
		return 1.10
	case horizonmodels.SignalModalityActual:
		return 1.15
	default:
		return 0.75
	}
}

func horizonWeight(h string) float64 {
	switch strings.TrimSpace(h) {
	case horizonmodels.SignalTimeHorizonImmediate:
		return 1.10
	case horizonmodels.SignalTimeHorizonShortTerm:
		return 1.00
	case horizonmodels.SignalTimeHorizonMediumTerm:
		return 0.90
	case horizonmodels.SignalTimeHorizonLongTerm:
		return 0.80
	default:
		return 1.00
	}
}

func escalationBoost(level string) float64 {
	switch strings.TrimSpace(level) {
	case horizonmodels.EscalationLevelWatch:
		return 0.05
	case horizonmodels.EscalationLevelWarning:
		return 0.12
	case horizonmodels.EscalationLevelCritical:
		return 0.20
	default:
		return 0.0
	}
}

func clamp01(v float64) float64 {
	return math.Max(0.0, math.Min(1.0, v))
}

// scanSource сканирует один источник и проверяет изменения
func (m *Monitor) scanSource(ctx context.Context, source *models.DataSource, config *horizonmodels.WatchdogConfig, budget *RunBudget) error {
	// Получаем контент источника через SourceExecutor
	result, err := m.executor.ExecuteDataSource(ctx, source)
	if err != nil {
		return fmt.Errorf("failed to fetch content: %w", err)
	}

	if result.Content == "" {
		return nil // Нет контента для проверки
	}

	// Вычисляем хеш контента
	contentHash := m.calculateContentHash(result.Content)

	// Получаем предыдущий хеш из последнего Document источника
	previousHash := m.getPreviousHash(ctx, source.ID)

	// Сравниваем хеши
	if contentHash == previousHash {
		logf("phase=hash decision=no_change sourceId=%s reason=content_hash_equal", source.ID)
		return nil // Изменений нет
	}

	// Получаем targetCanonicalPath из fragment для контекста
	targetCanonicalPath := ""
	if fragment, err := m.fragmentsRepo.Get(ctx, config.TargetFragmentID); err == nil {
		targetCanonicalPath = fragment.CanonicalPath
	}

	// Multi-phase change detection to avoid unnecessary LLM calls.
	oldContent := ""
	if previousHash != "" {
		oldDocs, err := m.documentsRepo.ListBySource(ctx, "", source.ID, 1)
		if err == nil && len(oldDocs) > 0 {
			oldContent = oldDocs[0].FullText
		}
	}

	decision := m.assessChangeMultiPhase(ctx, oldContent, result.Content, targetCanonicalPath, budget)
	m.logDecision(source.ID, config.TargetFragmentID, decision)
	changeAmount := decision.ChangeAmount

	if changeAmount < config.TriggerThreshold {
		// Изменение незначительно, обновляем хеш но не создаем запрос
		m.saveHash(ctx, source.ID, contentHash, result.DocumentID)
		return nil
	}

	// Risk runtime is preferred when configured, even for source-hash based watchdog mode.
	if m.riskJobsRepo != nil {
		if m.fragmentsRepo == nil {
			return fmt.Errorf("fragments repo not configured")
		}
		fragment, err := m.fragmentsRepo.Get(ctx, config.TargetFragmentID)
		if err != nil {
			return fmt.Errorf("failed to get fragment for risk trigger: %w", err)
		}
		if err := m.createRiskJobFromWatchdog(
			ctx,
			config,
			fragment,
			[]string{source.ID},
			"risk",
			clamp01(changeAmount),
			1,
		); err != nil {
			return fmt.Errorf("failed to create risk job: %w", err)
		}
	} else {
		// Legacy fallback for pure scout-only deployments.
		if err := m.createScoutRequest(ctx, config, source); err != nil {
			return fmt.Errorf("failed to create scout request: %w", err)
		}
	}
	if fragment, err := m.fragmentsRepo.Get(ctx, config.TargetFragmentID); err == nil && fragment != nil {
		m.emitUnifiedTrigger(ctx, config, fragment, &horizonmodels.WatchTrigger{
			SubjectID:      strings.TrimSpace(config.SubjectID),
			TriggerType:    horizonmodels.WatchTriggerSourceDelta,
			TriggerReason:  fmt.Sprintf("watchdog source delta on %s", strings.TrimSpace(source.ID)),
			CanonicalPaths: []string{strings.TrimSpace(fragment.CanonicalPath)},
			SourceIDs:      []string{strings.TrimSpace(source.ID)},
			SignalStrength: clamp01(changeAmount),
			Confidence:     clamp01(changeAmount),
			Metadata: map[string]any{
				"watchdogConfigId": strings.TrimSpace(config.ID),
				"changeAmount":     changeAmount,
			},
		})
	}

	// Сохраняем новый хеш
	m.saveHash(ctx, source.ID, contentHash, result.DocumentID)

	return nil
}

// calculateContentHash вычисляет SHA256 хеш контента
func (m *Monitor) calculateContentHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// getPreviousHash получает предыдущий хеш источника из последнего Document
func (m *Monitor) getPreviousHash(ctx context.Context, sourceID string) string {
	// Получаем последний документ источника (tenantID может быть пустым для публичных источников)
	docs, err := m.documentsRepo.ListBySource(ctx, "", sourceID, 1)
	if err != nil || len(docs) == 0 {
		return ""
	}
	// Используем ContentHash из Document
	return docs[0].ContentHash
}

// saveHash сохраняет хеш источника через обновление Document
func (m *Monitor) saveHash(ctx context.Context, sourceID string, hash string, documentID string) {
	_ = ctx
	_ = sourceID
	_ = hash
	if documentID == "" {
		return
	}
	// Хеш уже сохранен в Document при создании через SourceExecutor
	// Дополнительных действий не требуется
}

func (m *Monitor) assessChangeMultiPhase(
	ctx context.Context,
	oldContent string,
	newContent string,
	targetFragmentCanonicalPath string,
	budget *RunBudget,
) ChangeDecision {
	tuning := m.tuning

	// Phase 1: normalized hash check (very cheap).
	oldNorm := normalizeContent(oldContent)
	newNorm := normalizeContent(newContent)
	if oldNorm != "" && oldNorm == newNorm {
		return ChangeDecision{
			ChangeAmount:        0.0,
			Method:              "normalized_hash",
			Reason:              "normalized_content_equal",
			NormalizedHashEqual: true,
			SimHashSimilarity:   nil,
			EmbeddingCosineSim:  nil,
			LLMChangeAmount:     nil,
		}
	}

	// Phase 2: SimHash similarity (cheap heuristic).
	oldSH := simHash64(oldNorm)
	newSH := simHash64(newNorm)
	sim := simHashSimilarity(oldSH, newSH)
	simCopy := sim
	if sim >= tuning.simHashNoChangeThreshold {
		return ChangeDecision{
			ChangeAmount:        1.0 - sim,
			Method:              "simhash",
			Reason:              "simhash_high_similarity",
			SimHashSimilarity:   &simCopy,
			NormalizedHashEqual: false,
		}
	}
	if sim <= tuning.simHashSignificantThreshold {
		return ChangeDecision{
			ChangeAmount:        1.0 - sim,
			Method:              "simhash",
			Reason:              "simhash_low_similarity",
			SimHashSimilarity:   &simCopy,
			NormalizedHashEqual: false,
		}
	}

	// Phase 3: embeddings (moderately expensive) – only if we have budget and a provider.
	if m.embeddings != nil && budget != nil && budget.TryUseEmbeddingPair() {
		// Keep payload bounded.
		clip := func(s string, n int) string {
			s = strings.TrimSpace(s)
			if len(s) <= n {
				return s
			}
			return s[:n]
		}
		oldClip := clip(oldNorm, 8000)
		newClip := clip(newNorm, 8000)
		vecs, err := m.embeddings.EmbedTexts(ctx, []string{oldClip, newClip})
		if err == nil && len(vecs) == 2 {
			cos, cosErr := cosineSimilarity(vecs[0], vecs[1])
			if cosErr == nil {
				cosCopy := cos
				if cos >= tuning.embeddingNoChangeThreshold {
					return ChangeDecision{
						ChangeAmount:       1.0 - cos,
						Method:             "embeddings",
						Reason:             "embedding_high_similarity",
						SimHashSimilarity:  &simCopy,
						EmbeddingCosineSim: &cosCopy,
					}
				}
				if cos <= tuning.embeddingSignificantThreshold {
					return ChangeDecision{
						ChangeAmount:       1.0 - cos,
						Method:             "embeddings",
						Reason:             "embedding_low_similarity",
						SimHashSimilarity:  &simCopy,
						EmbeddingCosineSim: &cosCopy,
					}
				}
				// grey zone -> possibly LLM
			}
		}
	}

	// Phase 4: LLM (most expensive) – only if budget allows; otherwise fallback to a safe deterministic estimate.
	if budget != nil && budget.TryUseLLMAssessment() {
		llmAmt, err := m.changeAssessor.AssessChangeAmount(ctx, oldContent, newContent, targetFragmentCanonicalPath)
		if err == nil {
			llmCopy := llmAmt
			return ChangeDecision{
				ChangeAmount:      llmAmt,
				Method:            "llm",
				Reason:            "llm_assessment",
				SimHashSimilarity: &simCopy,
				LLMChangeAmount:   &llmCopy,
			}
		}
	}

	// Deterministic fallback: use simhash-based estimate from the grey zone.
	return ChangeDecision{
		ChangeAmount:      1.0 - sim,
		Method:            "simhash_fallback",
		Reason:            "budget_or_provider_unavailable",
		SimHashSimilarity: &simCopy,
	}
}

func (m *Monitor) logDecision(sourceID string, fragmentID string, d ChangeDecision) {
	parts := []string{
		fmt.Sprintf("phase=decision sourceId=%s fragmentId=%s method=%s changeAmount=%.4f", sourceID, fragmentID, d.Method, d.ChangeAmount),
		fmt.Sprintf("reason=%s", d.Reason),
	}
	if d.SimHashSimilarity != nil {
		parts = append(parts, fmt.Sprintf("simhashSim=%.4f", *d.SimHashSimilarity))
	}
	if d.EmbeddingCosineSim != nil {
		parts = append(parts, fmt.Sprintf("embedCos=%.4f", *d.EmbeddingCosineSim))
	}
	if d.LLMChangeAmount != nil {
		parts = append(parts, fmt.Sprintf("llmAmt=%.4f", *d.LLMChangeAmount))
	}
	if d.NormalizedHashEqual {
		parts = append(parts, "normEqual=true")
	}
	logf(strings.Join(parts, " "))
}

// createScoutRequest создает запрос на разведку через Scout Engine
func (m *Monitor) createScoutRequest(ctx context.Context, config *horizonmodels.WatchdogConfig, source *models.DataSource) error {
	if m.scoutingJobsRepo == nil {
		return fmt.Errorf("scouting jobs repo not configured")
	}
	if m.fragmentsRepo == nil {
		return fmt.Errorf("fragments repo not configured")
	}

	// Получаем fragment для определения targetCanonicalPath
	fragment, err := m.fragmentsRepo.Get(ctx, config.TargetFragmentID)
	if err != nil {
		return fmt.Errorf("failed to get fragment: %w", err)
	}
	if fragment.Governance.CreatedByProjectID == nil || *fragment.Governance.CreatedByProjectID == "" {
		return fmt.Errorf("fragment has no createdByProjectId; cannot create scouting job")
	}

	// Создаем ScoutingJob для обновления fragment
	scoutingJob := &horizonmodels.ScoutingJob{
		AgentJob: models.AgentJob{
			TenantID:  fragment.TenantID,
			ProjectID: *fragment.Governance.CreatedByProjectID,
			Status:    "pending",
			AgentID:   "scout",
			Task: map[string]interface{}{
				"targetCanonicalPath": fragment.CanonicalPath,
				"sourceIds":           []string{source.ID},
				"scoutingStrategy":    "Snapshot",
			},
			State: map[string]interface{}{
				"targetCanonicalPath": fragment.CanonicalPath,
				"lensId":              "",
				"currentState":        string(horizonmodels.ScoutingJobStatePending),
				"sourceIds":           []string{source.ID},
				"watchdogTriggered":   true,
			},
		},
		TargetCanonicalPath: fragment.CanonicalPath,
		SourceIDs:           []string{source.ID},
		ScoutingStrategy:    string(horizonmodels.ScoutingStrategySnapshot),
		BillingContext: horizonmodels.BillingContext{
			ChargeModel: string(horizonmodels.ChargeModelAccess),
			ProjectID:   *fragment.Governance.CreatedByProjectID,
		},
		CurrentState: horizonmodels.ScoutingJobStatePending,
	}

	// Запускаем ScoutingJob через Scout Engine
	_, err = m.scoutingJobsRepo.Create(ctx, scoutingJob)
	if err == nil {
		logf("phase=scout_enqueued fragmentId=%s jobId=%s sources=1", config.TargetFragmentID, scoutingJob.ID)
	}
	return err
}

// getSourcesByIDs получает источники по ID
func (m *Monitor) getSourcesByIDs(ctx context.Context, sourceIDs []string) ([]*models.DataSource, error) {
	var sources []*models.DataSource
	for _, id := range sourceIDs {
		source, err := m.dataSourcesRepo.Get(ctx, id)
		if err != nil {
			continue // Пропускаем недоступные источники
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func (m *Monitor) emitUnifiedTrigger(ctx context.Context, config *horizonmodels.WatchdogConfig, fragment *horizonmodels.HorizonFragment, trigger *horizonmodels.WatchTrigger) {
	if m == nil || m.triggerHook == nil || config == nil || fragment == nil || trigger == nil {
		return
	}
	if strings.TrimSpace(trigger.SubjectID) == "" {
		trigger.SubjectID = strings.TrimSpace(config.SubjectID)
	}
	_ = m.triggerHook(ctx, config, fragment, trigger)
}
