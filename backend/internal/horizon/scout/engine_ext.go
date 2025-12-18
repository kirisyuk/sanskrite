package scout

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	agentmodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func parseStateInt(v interface{}, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return def
		}
		if i, err := strconv.Atoi(t); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return int(f)
		}
		return def
	default:
		return def
	}
}

// processDiscoveredRelations обрабатывает найденные связи и запускает декомпозицию
func (e *ScoutEngine) processDiscoveredRelations(
	ctx context.Context,
	job *models.ScoutingJob,
	sourceFragment *models.HorizonFragment,
	relations []agents.DiscoveredRelation,
) error {
	if len(relations) == 0 {
		return nil
	}

	for _, rel := range relations {
		if rel.TargetName == "" {
			continue
		}

		// 1. Determine target canonical path
		targetPath, err := e.resolveCanonicalPath(ctx, rel)
		if err != nil {
			fmt.Printf("Canonicalization failed for relation target=%q: %v\n", rel.TargetName, err)
			continue
		}

		// 2. Find or Create Target Fragment
		targetFragment, err := e.fragmentsRepo.GetByCanonicalPath(ctx, targetPath)
		var targetID string

		if err != nil {
			// Fragment doesn't exist -> Create minimal fragment (low confidence) and schedule scouting to fill it.
			// We do NOT use /discovered paths; canonicalPath must already be stable here.
			ft := normalizeFragmentType(rel.TargetType)
			if ft == "" {
				ft = models.FragmentTypeOrganization
			}
			now := time.Now().UTC()
			legalName := rel.TargetName
			if rel.TargetLegalName != "" {
				legalName = rel.TargetLegalName
			}
			newFragment := &models.HorizonFragment{
				TenantID:      "global",
				CanonicalPath: targetPath,
				Type:          ft,
				Status:        "active",
				State:         make(map[string]models.MetricValue),
				Labels:        map[string]string{"en": legalName},
				Identifiers: map[string]string{
					"legalName": legalName,
				},
				Governance:  models.Governance{Owner: "SYSTEM", Status: string(models.FragmentStatusActive)},
				Description: fmt.Sprintf("Discovered via relation from %s", sourceFragment.CanonicalPath),
				Metadata: models.FragmentMetadata{
					CreatedAt:       now,
					LastUpdatedAt:   now,
					LastVerifiedAt:  time.Time{}, // IMPORTANT: keep zero so freshness check does not skip scouting
					EpistemicStatus: models.EpistemicStatusLowConfidence,
				},
			}
			if rel.TargetShortName != "" {
				newFragment.Identifiers["shortName"] = rel.TargetShortName
			}

			id, err := e.fragmentsRepo.Create(ctx, newFragment)
			if err != nil {
				fmt.Printf("Failed to create discovered fragment %s: %v\n", targetPath, err)
				continue
			}
			targetID = id

			// Create Recursive Scouting Job (Decomposition)
			// Проверяем глубину рекурсии, чтобы не уйти в бесконечность.
			// Читаем текущую глубину/лимит из job.State (это позволяет прод-ядру работать так же,
			// как тестовый сценарий).
			currentDepth := 0
			if d, ok := job.State["recursionDepth"].(float64); ok {
				currentDepth = int(d)
			}

			maxDepth := 2
			if job != nil && job.State != nil {
				if v, ok := job.State["maxRecursionDepth"]; ok {
					maxDepth = parseStateInt(v, 2)
				}
			}
			if maxDepth < 0 {
				maxDepth = 0
			}

			// Ограничиваем глубину
			if currentDepth < maxDepth {
				agentID := job.AgentID
				if agentID == "" {
					agentID = "scout"
				}
				now := time.Now().UTC()
				childState := map[string]interface{}{
					"targetCanonicalPath": targetPath,
					"currentState":        string(models.ScoutingJobStatePending),
					"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
					"recursionDepth":      currentDepth + 1,
					"maxRecursionDepth":   float64(maxDepth),
					"parentJobId":         job.ID,
				}
				// Propagate requestedMetrics, if present (ensures identical dynamic metrics behavior across prod/test).
				if job != nil && job.State != nil {
					if rm, ok := job.State["requestedMetrics"]; ok {
						childState["requestedMetrics"] = rm
					}
				}
				scoutingJob := &models.ScoutingJob{
					AgentJob: agentmodels.AgentJob{
						TenantID:  job.TenantID,
						ProjectID: job.ProjectID,
						Status:    agentmodels.AgentJobStatusPending,
						AgentID:   agentID, // inherit agentId to allow deterministic test workers and consistent routing
						CreatedAt: now,
						UpdatedAt: now,
						State:     childState,
					},
					TargetCanonicalPath: targetPath,
					ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
					BillingContext: models.BillingContext{
						ChargeModel: string(models.ChargeModelBounty),
						ProjectID:   job.ProjectID,
					},
				}
				if _, err := e.scoutingJobsRepo.Create(ctx, scoutingJob); err != nil {
					fmt.Printf("Failed to create recursive job for %s: %v\n", targetPath, err)
				} else {
					fmt.Printf("Triggered recursive scouting for: %s\n", targetPath)
				}
			}

		} else {
			targetID = targetFragment.ID

			// If fragment exists but is not verified (or never verified), schedule scouting to enrich it.
			// This is what makes the graph "expand" beyond already-known nodes (e.g., Panasonic) when needed.
			currentDepth := 0
			if d, ok := job.State["recursionDepth"].(float64); ok {
				currentDepth = int(d)
			}
			shouldScout := targetFragment.Metadata.LastVerifiedAt.IsZero() || targetFragment.Metadata.EpistemicStatus == models.EpistemicStatusLowConfidence
			maxDepth := 2
			if job != nil && job.State != nil {
				if v, ok := job.State["maxRecursionDepth"]; ok {
					maxDepth = parseStateInt(v, 2)
				}
			}
			if maxDepth < 0 {
				maxDepth = 0
			}

			if shouldScout && currentDepth < maxDepth {
				agentID := job.AgentID
				if agentID == "" {
					agentID = "scout"
				}
				now := time.Now().UTC()
				childState := map[string]interface{}{
					"targetCanonicalPath": targetPath,
					"currentState":        string(models.ScoutingJobStatePending),
					"scoutingStrategy":    string(models.ScoutingStrategySnapshot),
					"recursionDepth":      currentDepth + 1,
					"maxRecursionDepth":   float64(maxDepth),
					"parentJobId":         job.ID,
				}
				if job != nil && job.State != nil {
					if rm, ok := job.State["requestedMetrics"]; ok {
						childState["requestedMetrics"] = rm
					}
				}
				scoutingJob := &models.ScoutingJob{
					AgentJob: agentmodels.AgentJob{
						TenantID:  job.TenantID,
						ProjectID: job.ProjectID,
						Status:    agentmodels.AgentJobStatusPending,
						AgentID:   agentID,
						CreatedAt: now,
						UpdatedAt: now,
						State:     childState,
					},
					TargetCanonicalPath: targetPath,
					ScoutingStrategy:    string(models.ScoutingStrategySnapshot),
					BillingContext: models.BillingContext{
						ChargeModel: string(models.ChargeModelBounty),
						ProjectID:   job.ProjectID,
					},
				}
				if _, err := e.scoutingJobsRepo.Create(ctx, scoutingJob); err == nil {
					fmt.Printf("Triggered recursive scouting (existing fragment) for: %s\n", targetPath)
				}
			}
		}

		// 3. Create Dependency Edge
		// Валидация типа ребра
		edgeType := models.DependencyType(rel.RelationType)
		if edgeType == "" {
			edgeType = models.DependencyTypeSuppliesTo
		}

		// Проверка на дубликат ребра
		if _, err := e.edgesRepo.GetBySourceTargetAndType(ctx, "global", sourceFragment.ID, targetID, edgeType); err == nil {
			// Ребро уже существует, пропускаем создание
			fmt.Printf("Skipping duplicate edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
			continue
		}

		edge := &models.DependencyEdge{
			TenantID:         "global",
			SourceFragmentID: sourceFragment.ID,
			TargetFragmentID: targetID,
			Type:             edgeType,
			Properties: models.EdgeProperties{
				Criticality:   rel.Confidence,
				Amplification: 1.0,
				LatencyHours:  24,
			},
			Provenance: models.Provenance{
				Method:    "scout_discovery",
				SourceIDs: []string{job.ID},
			},
			Metadata: models.EdgeMetadata{
				CreatedAt:     time.Now(),
				LastUpdatedAt: time.Now(),
			},
		}

		// Используем Create, предполагая что репо обработает дубликаты или мы просто создаем новые
		// В реальной системе нужен UpsertEdge
		if _, err := e.edgesRepo.Create(ctx, edge); err != nil {
			// Игнорируем ошибки дубликатов
			// fmt.Printf("Edge creation note: %v\n", err)
		} else {
			fmt.Printf("Created edge: %s -> %s (%s)\n", sourceFragment.CanonicalPath, targetPath, edgeType)
		}
	}
	return nil
}

// NOTE: legacy sanitizePathName removed.
