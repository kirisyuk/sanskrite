package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

// ScoutingJobsRepository репозиторий для работы с ScoutingJob
// Использует существующий AgentJobsRepository для хранения
type ScoutingJobsRepository struct {
	agentJobsRepo *repo.AgentJobsRepository
	onCreate      func(ctx context.Context, job *models.ScoutingJob) error
}

// NewScoutingJobsRepository создает новый репозиторий
func NewScoutingJobsRepository(agentJobsRepo *repo.AgentJobsRepository) *ScoutingJobsRepository {
	return &ScoutingJobsRepository{
		agentJobsRepo: agentJobsRepo,
	}
}

// WithOnCreate installs an optional hook invoked after a job is created.
// Used by the server to enqueue Cloud Tasks, while keeping tools/tests able to write jobs directly.
func (r *ScoutingJobsRepository) WithOnCreate(hook func(ctx context.Context, job *models.ScoutingJob) error) *ScoutingJobsRepository {
	r.onCreate = hook
	return r
}

// Create создает новый ScoutingJob через AgentJobsRepository
func (r *ScoutingJobsRepository) Create(ctx context.Context, job *models.ScoutingJob) (string, error) {
	if job == nil {
		return "", ErrInvalidDocument
	}
	// ScoutingJob встраивает AgentJob, поэтому используем его напрямую
	agentJob := &job.AgentJob
	lockKey := buildScoutJobDedupKey(job)
	trace.Log(ctx, "root_jobs", "scout_job.create.dedup.start", map[string]any{
		"tenantId":            strings.TrimSpace(job.TenantID),
		"projectId":           strings.TrimSpace(job.ProjectID),
		"lensId":              strings.TrimSpace(job.LensID),
		"agentId":             strings.TrimSpace(job.AgentID),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"lockKeySha1":         trace.HashText(lockKey),
	})
	id, created, err := r.agentJobsRepo.CreateDedup(ctx, agentJob, lockKey)
	if err != nil {
		trace.Log(ctx, "root_jobs", "scout_job.create.dedup.error", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(job.LensID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"lockKeySha1":         trace.HashText(lockKey),
			"error":               err.Error(),
		})
		return "", fmt.Errorf("failed to create scouting job: %w", err)
	}
	job.ID = id
	trace.Log(ctx, "root_jobs", "scout_job.create.dedup.result", map[string]any{
		"tenantId":            strings.TrimSpace(job.TenantID),
		"projectId":           strings.TrimSpace(job.ProjectID),
		"lensId":              strings.TrimSpace(job.LensID),
		"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
		"jobId":               id,
		"created":             created,
	})
	// Only enqueue when a new job was actually created.
	if created && r.onCreate != nil {
		trace.Log(ctx, "root_jobs", "scout_job.create.enqueue_hook.start", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(job.LensID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"jobId":               id,
		})
		if err := r.onCreate(ctx, job); err != nil {
			trace.Log(ctx, "root_jobs", "scout_job.create.enqueue_hook.error", map[string]any{
				"tenantId":            strings.TrimSpace(job.TenantID),
				"projectId":           strings.TrimSpace(job.ProjectID),
				"lensId":              strings.TrimSpace(job.LensID),
				"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
				"jobId":               id,
				"error":               err.Error(),
			})
			return "", fmt.Errorf("scouting job created but enqueue hook failed: %w", err)
		}
		trace.Log(ctx, "root_jobs", "scout_job.create.enqueue_hook.ok", map[string]any{
			"tenantId":            strings.TrimSpace(job.TenantID),
			"projectId":           strings.TrimSpace(job.ProjectID),
			"lensId":              strings.TrimSpace(job.LensID),
			"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
			"jobId":               id,
		})
	}

	// Best-effort: if we deduped onto an existing job, merge incoming sourceIds into its state.
	// This avoids dropping watchdog-driven triggers when multiple sources change concurrently.
	if !created && r.agentJobsRepo != nil {
		srcIDs := extractSourceIDsFromScoutingJob(job)
		if len(srcIDs) > 0 {
			trace.Log(ctx, "root_jobs", "scout_job.create.dedup.merge_source_ids", map[string]any{
				"tenantId":            strings.TrimSpace(job.TenantID),
				"projectId":           strings.TrimSpace(job.ProjectID),
				"lensId":              strings.TrimSpace(job.LensID),
				"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
				"jobId":               id,
				"sourceIdsN":          len(srcIDs),
			})
			_ = r.agentJobsRepo.MergeStateStringArray(ctx, id, "sourceIds", srcIDs)
		}
		// Also merge specialties (optional task focus modules) so UI can "upgrade" an existing pending/running job.
		specs := extractSpecialtiesFromScoutingJob(job)
		if len(specs) > 0 {
			trace.Log(ctx, "root_jobs", "scout_job.create.dedup.merge_specialties", map[string]any{
				"tenantId":            strings.TrimSpace(job.TenantID),
				"projectId":           strings.TrimSpace(job.ProjectID),
				"lensId":              strings.TrimSpace(job.LensID),
				"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
				"jobId":               id,
				"specialtiesN":        len(specs),
				"specialties":         trace.StrSlice(specs, 10),
			})
			_ = r.agentJobsRepo.MergeStateStringArray(ctx, id, "specialties", specs)
		}
		// Keep reused job state aligned with the latest intent payload.
		// Without this, an active deduped job may continue with stale domains/title from a previous request.
		if err := r.syncReusedJobIntentState(ctx, id, job); err != nil {
			trace.Log(ctx, "root_jobs", "scout_job.create.dedup.sync_intent_state.error", map[string]any{
				"tenantId":            strings.TrimSpace(job.TenantID),
				"projectId":           strings.TrimSpace(job.ProjectID),
				"lensId":              strings.TrimSpace(job.LensID),
				"targetCanonicalPath": strings.TrimSpace(job.TargetCanonicalPath),
				"jobId":               id,
				"error":               err.Error(),
			})
		}
	}
	return id, nil
}

func (r *ScoutingJobsRepository) syncReusedJobIntentState(ctx context.Context, jobID string, incoming *models.ScoutingJob) error {
	if r == nil || r.agentJobsRepo == nil || incoming == nil || incoming.State == nil || strings.TrimSpace(jobID) == "" {
		return nil
	}
	existing, err := r.agentJobsRepo.Get(ctx, jobID)
	if err != nil || existing == nil {
		return err
	}
	if existing.State == nil {
		existing.State = map[string]interface{}{}
	}
	changed := false
	copyIfPresent := func(key string) {
		if v, ok := incoming.State[key]; ok && v != nil {
			existing.State[key] = v
			changed = true
		}
	}
	// Intent payload keys that should reflect the latest request when job is reused.
	copyIfPresent("domains")
	copyIfPresent("focus")
	copyIfPresent("spherePaths")
	copyIfPresent("keywords")
	copyIfPresent("originalQuery")
	copyIfPresent("decompositionHints")
	copyIfPresent("requestedMetrics")
	copyIfPresent("targetIdentityKey")
	if !changed {
		return nil
	}
	return r.agentJobsRepo.Update(ctx, existing)
}

// RelationHashForDedup computes a short hash for (sourcePath, targetPath, relationType)
// to distinguish child jobs from different relations to the same target.
func RelationHashForDedup(sourcePath, targetPath, relationType string) string {
	s := strings.TrimSpace(sourcePath) + "|" + strings.TrimSpace(targetPath) + "|" + strings.TrimSpace(relationType)
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

func buildScoutJobDedupKey(job *models.ScoutingJob) string {
	if job == nil {
		return ""
	}
	tenantID := strings.TrimSpace(job.TenantID)
	projectID := strings.TrimSpace(job.ProjectID)
	agentID := strings.TrimSpace(job.AgentID)
	if agentID == "" {
		agentID = "scout"
	}

	lensID := strings.TrimSpace(job.LensID)
	if lensID == "" && job.State != nil {
		if v, ok := job.State["lensId"].(string); ok {
			lensID = strings.TrimSpace(v)
		}
	}
	target := strings.TrimSpace(job.TargetCanonicalPath)
	if target == "" && job.State != nil {
		if v, ok := job.State["targetCanonicalPath"].(string); ok {
			target = strings.TrimSpace(v)
		}
	}
	identityKey := ""
	if job.State != nil {
		if v, ok := job.State["targetIdentityKey"].(string); ok {
			identityKey = strings.TrimSpace(v)
		}
	}

	// Cascade child idempotency: include cascadeRootJobId + relationHash when present.
	// Prevents duplicate child jobs when the same (source,target,relationType) is scheduled multiple times.
	cascadeID := ""
	relationHash := ""
	if job.State != nil {
		if v, ok := job.State["cascadeRootJobId"].(string); ok && strings.TrimSpace(v) != "" {
			cascadeID = strings.TrimSpace(v)
		}
		if v, ok := job.State["relationHash"].(string); ok && strings.TrimSpace(v) != "" {
			relationHash = strings.TrimSpace(v)
		}
	}

	// The core idempotency scope:
	// (tenant, project, lens, agent, identityKey|canonicalPath)
	// For cascade children: (..., cascade, target, relationHash)
	if cascadeID != "" {
		if relationHash != "" {
			return fmt.Sprintf("agent_job_dedup|tenant=%s|project=%s|lens=%s|agent=%s|cascade=%s|target=%s|relationHash=%s", tenantID, projectID, lensID, agentID, cascadeID, target, relationHash)
		}
		return fmt.Sprintf("agent_job_dedup|tenant=%s|project=%s|lens=%s|agent=%s|cascade=%s|target=%s", tenantID, projectID, lensID, agentID, cascadeID, target)
	}

	if identityKey != "" {
		return fmt.Sprintf("agent_job_dedup|tenant=%s|project=%s|lens=%s|agent=%s|identity=%s", tenantID, projectID, lensID, agentID, identityKey)
	}
	return fmt.Sprintf("agent_job_dedup|tenant=%s|project=%s|lens=%s|agent=%s|target=%s", tenantID, projectID, lensID, agentID, target)
}

func extractSourceIDsFromScoutingJob(job *models.ScoutingJob) []string {
	if job == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range job.SourceIDs {
		add(s)
	}
	if job.State != nil {
		if v, ok := job.State["sourceIds"]; ok {
			switch t := v.(type) {
			case []string:
				for _, s := range t {
					add(s)
				}
			case []interface{}:
				for _, it := range t {
					if s, ok2 := it.(string); ok2 {
						add(s)
					}
				}
			case string:
				// allow comma-separated
				for _, p := range strings.Split(t, ",") {
					add(p)
				}
			}
		}
	}
	if len(out) > 1 {
		sort.Strings(out)
	}
	return out
}

func extractSpecialtiesFromScoutingJob(job *models.ScoutingJob) []string {
	if job == nil {
		return nil
	}
	raw := []string{}
	if job.State != nil {
		if v, ok := job.State["specialties"]; ok {
			switch t := v.(type) {
			case []string:
				raw = append(raw, t...)
			case []interface{}:
				for _, it := range t {
					if s, ok2 := it.(string); ok2 {
						raw = append(raw, s)
					}
				}
			case string:
				raw = append(raw, t)
			}
		}
	}
	specs := taxonomy.NormalizeSpecialties(raw)
	if len(specs) == 0 {
		return nil
	}
	// Keep deterministic order for idempotent merges
	if len(specs) > 1 {
		sort.Strings(specs)
	}
	return specs
}

// Get получает ScoutingJob по ID
func (r *ScoutingJobsRepository) Get(ctx context.Context, id string) (*models.ScoutingJob, error) {
	if id == "" {
		return nil, ErrInvalidDocument
	}
	agentJob, err := r.agentJobsRepo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get scouting job: %w", err)
	}
	// Преобразуем AgentJob обратно в ScoutingJob
	// В реальной реализации нужно будет хранить дополнительные поля ScoutingJob отдельно
	// или использовать State в AgentJob для хранения специфичных полей
	scoutingJob := &models.ScoutingJob{
		AgentJob: *agentJob,
		// Дополнительные поля будут извлекаться из State или отдельной коллекции
	}
	r.populateFromState(scoutingJob)
	return scoutingJob, nil
}

func (r *ScoutingJobsRepository) populateFromState(job *models.ScoutingJob) {
	if job == nil || job.State == nil {
		return
	}
	if job.TargetCanonicalPath == "" {
		if v, ok := job.State["targetCanonicalPath"].(string); ok {
			job.TargetCanonicalPath = v
		}
	}
	if job.LensID == "" {
		if v, ok := job.State["lensId"].(string); ok {
			job.LensID = v
		}
	}
	if job.CurrentState == "" {
		if v, ok := job.State["currentState"].(string); ok {
			job.CurrentState = models.ScoutingJobState(v)
		}
	}
	if job.ScoutingStrategy == "" {
		if v, ok := job.State["scoutingStrategy"].(string); ok {
			job.ScoutingStrategy = v
		}
	}
	// Best-effort: SourceIDs may be persisted either as top-level field or in state.
	if len(job.SourceIDs) == 0 {
		if v, ok := job.State["sourceIds"]; ok {
			switch t := v.(type) {
			case []string:
				job.SourceIDs = t
			case []interface{}:
				out := make([]string, 0, len(t))
				for _, it := range t {
					if s, ok2 := it.(string); ok2 {
						out = append(out, s)
					}
				}
				if len(out) > 0 {
					job.SourceIDs = out
				}
			}
		}
	}
	// Compatibility: some producers keep sourceIds in AgentJob.Task, not State.
	if len(job.SourceIDs) == 0 && job.Task != nil {
		if v, ok := job.Task["sourceIds"]; ok {
			switch t := v.(type) {
			case []string:
				job.SourceIDs = t
			case []interface{}:
				out := make([]string, 0, len(t))
				for _, it := range t {
					if s, ok2 := it.(string); ok2 {
						s = strings.TrimSpace(s)
						if s != "" {
							out = append(out, s)
						}
					}
				}
				if len(out) > 0 {
					job.SourceIDs = out
				}
			case string:
				parts := strings.Split(t, ",")
				out := make([]string, 0, len(parts))
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p != "" {
						out = append(out, p)
					}
				}
				if len(out) > 0 {
					job.SourceIDs = out
				}
			}
		}
	}
	if len(job.SourceIDs) > 1 {
		seen := map[string]struct{}{}
		dedup := make([]string, 0, len(job.SourceIDs))
		for _, s := range job.SourceIDs {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			dedup = append(dedup, s)
		}
		sort.Strings(dedup)
		job.SourceIDs = dedup
	}
}

// ListByProjectAndLens lists scout jobs for a given project and (optionally) lensId.
// This is preferred over ListByLens because it relies on a more commonly provisioned index:
// (tenantId, projectId, createdAt DESC).
func (r *ScoutingJobsRepository) ListByProjectAndLens(ctx context.Context, tenantID, projectID, lensID string, limit int) ([]*models.ScoutingJob, error) {
	if r == nil || r.agentJobsRepo == nil {
		return nil, fmt.Errorf("scouting jobs repo not configured")
	}
	if tenantID == "" || projectID == "" {
		return nil, ErrInvalidDocument
	}
	if limit <= 0 {
		limit = 50
	}
	// Pull more than requested and filter client-side.
	agentJobs, err := r.agentJobsRepo.ListByProject(ctx, tenantID, projectID, limit*4)
	if err != nil {
		return nil, err
	}
	out := make([]*models.ScoutingJob, 0, limit)
	for _, aj := range agentJobs {
		if aj == nil || aj.AgentID != "scout" {
			continue
		}
		sj := &models.ScoutingJob{AgentJob: *aj}
		r.populateFromState(sj)
		if lensID != "" && sj.LensID != lensID {
			continue
		}
		out = append(out, sj)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Update обновляет ScoutingJob
func (r *ScoutingJobsRepository) Update(ctx context.Context, job *models.ScoutingJob) error {
	if job == nil || job.ID == "" {
		return ErrInvalidDocument
	}
	agentJob := &job.AgentJob
	return r.agentJobsRepo.Update(ctx, agentJob)
}

// UpdateState performs a partial update of individual fields within the job's State map.
func (r *ScoutingJobsRepository) UpdateState(ctx context.Context, jobID string, fields map[string]interface{}) error {
	if jobID == "" {
		return ErrInvalidDocument
	}
	return r.agentJobsRepo.UpdateState(ctx, jobID, fields)
}

// ListByLens получает список ScoutingJob для Lens
// Если lensID пустой, возвращает все ScoutingJob для tenant (фильтрованные по AgentID="scout")
func (r *ScoutingJobsRepository) ListByLens(ctx context.Context, tenantID, lensID string, limit int) ([]*models.ScoutingJob, error) {
	// Получаем все AgentJob для tenant с AgentID="scout"
	// Используем ListByStatus для получения всех jobs по разным статусам, затем фильтруем по AgentID
	allStatuses := []coremodels.AgentJobStatus{
		coremodels.AgentJobStatusPending,
		coremodels.AgentJobStatusRunning,
		coremodels.AgentJobStatusCompleted,
		coremodels.AgentJobStatusFailed,
		coremodels.AgentJobStatusPartial,
	}

	var allAgentJobs []*coremodels.AgentJob
	for _, status := range allStatuses {
		jobs, err := r.agentJobsRepo.ListByStatus(ctx, tenantID, status, limit*3) // Увеличиваем лимит для фильтрации
		if err != nil {
			continue // Пропускаем ошибки, продолжаем с другими статусами
		}
		allAgentJobs = append(allAgentJobs, jobs...)
	}

	var scoutingJobs []*models.ScoutingJob
	for _, agentJob := range allAgentJobs {
		// Фильтруем только ScoutingJob (AgentID="scout")
		if agentJob.AgentID != "scout" {
			continue
		}

		// Преобразуем AgentJob в ScoutingJob
		scoutingJob := &models.ScoutingJob{
			AgentJob: *agentJob,
		}
		r.populateFromState(scoutingJob)

		// Если lensID указан, фильтруем по нему
		if lensID != "" {
			if scoutingJob.LensID != lensID {
				continue
			}
		}

		scoutingJobs = append(scoutingJobs, scoutingJob)
		if len(scoutingJobs) >= limit {
			break
		}
	}
	return scoutingJobs, nil
}

// ErrInvalidDocument определен в fragments.go
