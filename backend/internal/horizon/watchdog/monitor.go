package watchdog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	dataSourcesRepo  *repo.DataSourcesRepository
	documentsRepo    *repo.DocumentsRepository
	fragmentsRepo    *horizonrepo.HorizonFragmentsRepository
	factsRepo        *repo.OntologyFactsRepository
	scoutingJobsRepo *horizonrepo.ScoutingJobsRepository
	scoutEngine      *scout.ScoutEngine
	executor         *scout.SourceExecutor
	changeAssessor   *ChangeAssessor
	embeddings       ai.EmbeddingProvider
	tuning           changeTuning
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
	for _, source := range sources {
		if err := m.scanSource(ctx, source, config, budget); err != nil {
			// Логируем ошибку, но продолжаем сканирование других источников
			_ = err
		}
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

	// Aggregate source IDs from relevant facts and apply TriggerThreshold as a confidence gate.
	sourceSet := map[string]struct{}{}
	maxConf := 0.0
	for _, f := range facts {
		if f == nil {
			continue
		}
		if f.Confidence > maxConf {
			maxConf = f.Confidence
		}
		for _, sid := range f.SourceIDs {
			sid = strings.TrimSpace(sid)
			if sid == "" {
				continue
			}
			sourceSet[sid] = struct{}{}
		}
	}
	changeAmount := maxConf
	logf("phase=facts_decision fragmentId=%s canonicalPath=%s maxConfidence=%.3f threshold=%.3f sources=%d",
		config.TargetFragmentID, targetCanonicalPath, maxConf, config.TriggerThreshold, len(sourceSet))

	if changeAmount < config.TriggerThreshold || len(sourceSet) == 0 {
		return nil
	}

	// Trigger a single scouting job with the aggregated sources.
	sourceIDs := make([]string, 0, len(sourceSet))
	for sid := range sourceSet {
		sourceIDs = append(sourceIDs, sid)
	}

	return m.createScoutRequestFromSourceIDs(ctx, config, sourceIDs)
}

func (m *Monitor) createScoutRequestFromSourceIDs(ctx context.Context, config *horizonmodels.WatchdogConfig, sourceIDs []string) error {
	if m.scoutingJobsRepo == nil {
		return fmt.Errorf("scouting jobs repo not configured")
	}
	if m.fragmentsRepo == nil {
		return fmt.Errorf("fragments repo not configured")
	}

	fragment, err := m.fragmentsRepo.Get(ctx, config.TargetFragmentID)
	if err != nil {
		return fmt.Errorf("failed to get fragment: %w", err)
	}
	// Resolve project context:
	// - Prefer project linked to the involved sources (rag_source_projects), since fragments are global.
	// - Fall back to fragment.Governance.CreatedByProjectID if available.
	projectID := ""
	if m.dataSourcesRepo != nil && len(sourceIDs) > 0 {
		if pid, err := m.dataSourcesRepo.ResolveProjectIDForSource(ctx, fragment.TenantID, sourceIDs[0]); err == nil {
			projectID = pid
		}
	}
	if projectID == "" && fragment.Governance.CreatedByProjectID != nil {
		projectID = strings.TrimSpace(*fragment.Governance.CreatedByProjectID)
	}
	if projectID == "" {
		return fmt.Errorf("cannot resolve projectId for scouting job (no source->project link and fragment.createdByProjectId missing)")
	}

	scoutingJob := &horizonmodels.ScoutingJob{
		AgentJob: models.AgentJob{
			TenantID:  fragment.TenantID,
			ProjectID: projectID,
			Status:    "pending",
			AgentID:   "scout",
			Task: map[string]interface{}{
				"targetCanonicalPath": fragment.CanonicalPath,
				"sourceIds":           sourceIDs,
				"scoutingStrategy":    "Snapshot",
			},
			State: map[string]interface{}{
				"targetCanonicalPath": fragment.CanonicalPath,
				"lensId":              "",
				"currentState":        string(horizonmodels.ScoutingJobStatePending),
			},
		},
		TargetCanonicalPath: fragment.CanonicalPath,
		SourceIDs:           sourceIDs,
		ScoutingStrategy:    string(horizonmodels.ScoutingStrategySnapshot),
		BillingContext: horizonmodels.BillingContext{
			ChargeModel: string(horizonmodels.ChargeModelAccess),
			ProjectID:   projectID,
		},
		CurrentState: horizonmodels.ScoutingJobStatePending,
	}

	_, err = m.scoutingJobsRepo.Create(ctx, scoutingJob)
	if err == nil {
		logf("phase=scout_enqueued fragmentId=%s jobId=%s sources=%d", config.TargetFragmentID, scoutingJob.ID, len(sourceIDs))
	}
	return err
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

	// Изменение превышает порог, создаем ScoutRequest
	if err := m.createScoutRequest(ctx, config, source); err != nil {
		return fmt.Errorf("failed to create scout request: %w", err)
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
