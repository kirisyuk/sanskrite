package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/agents"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
)

// testScenario6 тестирует все новые capabilities
func testScenario6(
	ctx context.Context,
	orchestrator *agents.AgentOrchestrator,
	jobsRepo *repo.AgentJobsRepository,
	entitiesRepo *repo.EntityRepository,
	relationsRepo *repo.RelationRepository,
	claimsRepo *repo.ClaimRepository,
	eventsRepo *repo.EventRepository,
	entityEnricher *ontology.EntityEnricher,
	conflictDetector *ontology.ConflictDetector,
	causalDetector *ontology.CausalDetector,
	entityResolver *ontology.EntityResolver,
	genaiClient *gen_combined.Client,
) {
	tenantID := "kirisyuk@gmail.com"
	projectID := "KtwHOHp0LTNAG63HrkC6"

	// Создаем тестовые сущности
	now := time.Now().UTC()
	entity1 := &models.Entity{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "test",
			UpdatedBy: "test",
			Labels:    map[string]string{"en": "Test Entity 1"},
			Status:    "active",
		},
		CanonicalName: "Test Entity 1",
		Type:          models.EntityTypeOrganization,
		Aliases:       []string{"TE1"},
	}
	entity1ID, _ := entitiesRepo.Create(ctx, entity1)

	entity2 := &models.Entity{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "test",
			UpdatedBy: "test",
			Labels:    map[string]string{"en": "Test Entity 2"},
			Status:    "active",
		},
		CanonicalName: "Test Entity 2",
		Type:          models.EntityTypeOrganization,
		Aliases:       []string{"TE2"},
		Attributes:    map[string]interface{}{"key1": "value1"},
	}
	entity2ID, _ := entitiesRepo.Create(ctx, entity2)

	// Создаем связь
	relation := &models.Relation{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "test",
			UpdatedBy: "test",
			Status:    "active",
		},
		SubjectID:  entity1ID,
		Predicate:  "contains",
		ObjectID:   entity2ID,
		Direction:  models.RelationDirectionSubjectToObject,
		Confidence: 0.9,
	}
	relationID, _ := relationsRepo.Create(ctx, relation)

	// Создаем claim для тестирования конфликтов
	claim := &models.Claim{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "test",
			UpdatedBy: "test",
			Status:    "active",
		},
		SubjectRef: models.SubjectRef{
			ID:   entity1ID,
			Kind: models.ReferenceKindEntity,
		},
		Predicate: "hasValue",
		ObjectRef: models.ObjectRef{
			Kind:  models.ReferenceKindLiteral,
			Value: "100",
		},
		Confidence: 0.8,
		Stance:     models.StanceSupports,
		Provenance: models.ClaimProvenance{
			SourceIDs:   []string{"test-source"},
			Method:      models.ExtractionMethodNLP,
			ExtractedAt: now,
		},
	}
	claimID, _ := claimsRepo.Create(ctx, claim)

	// Создаем event для тестирования причинно-следственных связей
	eventTime := now.Add(-24 * time.Hour)
	event := &models.Event{
		BaseOntologyFields: models.BaseOntologyFields{
			TenantID:  tenantID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: "test",
			UpdatedBy: "test",
			Status:    "active",
		},
		Type:       "test_event",
		Title:      "Test Event",
		OccurredAt: &eventTime,
		Confidence: 0.7,
		Provenance: models.Provenance{
			SourceIDs: []string{"test-source"},
		},
	}
	eventID, _ := eventsRepo.Create(ctx, event)

	fmt.Printf("Created test entities: %s, %s\n", entity1ID, entity2ID)
	fmt.Printf("Created relation: %s\n", relationID)
	fmt.Printf("Created claim: %s\n", claimID)
	fmt.Printf("Created event: %s\n", eventID)

	// Создаем job для тестирования всех новых capabilities
	task := map[string]interface{}{
		"testAllCapabilities": true,
	}

	job, err := orchestrator.CreateJob(ctx, tenantID, projectID, "test-user", "test_all_capabilities", task, true)
	if err != nil {
		log.Printf("Failed to create job: %v", err)
		return
	}

	fmt.Printf("Created job: %s\n", job.ID)

	steps := []models.AgentStep{
		// 1. Entity Extract
		{
			ID:         "step_extract",
			Capability: "entity_extract",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"text": "Apple Inc. is a technology company based in Cupertino, California. Microsoft Corporation is another tech giant.",
			},
			DependsOn:  []string{},
			IsCritical: false,
		},
		// 2. Entity Enrich (если доступен)
		{
			ID:         "step_enrich",
			Capability: "entity_enrich",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"entityId": entity1ID,
			},
			DependsOn:  []string{},
			IsCritical: false,
		},
		// 3. Entity Hierarchy Build
		{
			ID:         "step_hierarchy",
			Capability: "entity_hierarchy_build",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"entityId": entity1ID,
				"maxDepth": 2,
			},
			DependsOn:  []string{},
			IsCritical: false,
		},
		// 4. Relation Find
		{
			ID:         "step_relation_find",
			Capability: "relation_find",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"entityId": entity1ID,
			},
			DependsOn:  []string{},
			IsCritical: false,
		},
		// 5. Entity Validate
		{
			ID:         "step_validate",
			Capability: "entity_validate",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"entityId": entity1ID,
			},
			DependsOn:  []string{},
			IsCritical: false,
		},
		// 6. Entity Merge Attributes
		{
			ID:         "step_merge",
			Capability: "entity_merge_attributes",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"targetEntityId":  entity1ID,
				"sourceEntityIds": []string{entity2ID},
			},
			DependsOn:  []string{},
			IsCritical: false,
		},
		// 7. Conflict Detect
		{
			ID:         "step_conflict",
			Capability: "conflict_detect",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"claimId": claimID,
			},
			DependsOn:  []string{},
			IsCritical: false,
		},
		// 8. Causal Link Detect
		{
			ID:         "step_causal",
			Capability: "causal_link_detect",
			Status:     models.AgentStepStatusPending,
			Input: map[string]interface{}{
				"eventId": eventID,
			},
			DependsOn:  []string{},
			IsCritical: false,
		},
	}

	job.Steps = steps
	if err := jobsRepo.Update(ctx, job); err != nil {
		log.Printf("Failed to update job with steps: %v", err)
		return
	}

	onUpdate := func(update agents.Update) {
		fmt.Printf("  Update: stage=%s, progress=%.2f, message=%s\n",
			update.Stage, update.Progress, update.Message)
	}

	if err := orchestrator.ExecuteJob(ctx, job.ID, onUpdate); err != nil {
		log.Printf("Job execution failed: %v", err)
	} else {
		fmt.Printf("Job %s completed successfully\n", job.ID)
	}

	finalJob, _ := jobsRepo.Get(ctx, job.ID)
	if finalJob != nil {
		fmt.Printf("\nFinal status: %s, progress: %.2f\n", finalJob.Status, finalJob.Progress)
		for _, step := range finalJob.Steps {
			fmt.Printf("  Step %s (%s): %s\n", step.ID, step.Capability, step.Status)
			if step.Error != nil {
				fmt.Printf("    Error: %s\n", *step.Error)
			}
			if step.Output != nil {
				if count, ok := step.Output["count"].(float64); ok {
					fmt.Printf("    Output count: %.0f\n", count)
				}
			}
		}
	}
}

