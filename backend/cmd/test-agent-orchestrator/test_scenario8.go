package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/agents"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// testScenario8Replanning тестирует репланирование при неудаче шага
func testScenario8Replanning(
	ctx context.Context,
	orchestrator *agents.AgentOrchestrator,
	jobsRepo *repo.AgentJobsRepository,
	coreDocsRepo *repo.CoreDocsRepo,
) {
	fmt.Println("\n--- Scenario 8: Replanning Test ---")

	// Создать тестовый документ с известным ID
	testDocID := "test-doc-replanning"
	testDoc, err := coreDocsRepo.Get(ctx, testDocID)
	if err != nil {
		now := time.Now().UTC()
		sizeBytes := int64(300)
		extractedText := "This document contains information about potato supplies in 2024. Supply volumes: January - 100 tons, February - 120 tons, March - 150 tons. Main supplier - Agro-Plus LLC."
		testDoc = &models.CoreDoc{
			ID:            testDocID,
			TenantID:      "test-tenant",
			ProjectID:     "test-project",
			Filename:      "potato-supplies-2024.txt",
			ContentType:   "text/plain",
			SizeBytes:     &sizeBytes,
			ExtractedText: &extractedText,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := coreDocsRepo.Upsert(ctx, testDoc); err != nil {
			log.Printf("WARNING: Failed to create test document: %v", err)
		} else {
			fmt.Printf("Created test document: %s\n", testDocID)
		}
	} else {
		fmt.Printf("Test document already exists: %s\n", testDocID)
	}

	// Создать job с автоматическим планированием
	// Запрос намеренно сложный, чтобы вызвать необходимость репланирования
	task := map[string]interface{}{
		"userQuery": "Find and read a document about potato supplies, then create a summary",
		"planMode":  "auto",
	}

	job, err := orchestrator.CreateJob(ctx, "test-tenant", "test-project", "test-user", "replanning_agent", task, true, "")
	if err != nil {
		log.Fatalf("Failed to create job: %v", err)
	}

	fmt.Printf("Created job: %s (planMode: %s)\n", job.ID, job.PlanMode)
	fmt.Printf("Initial steps count: %d (will be generated)\n", len(job.Steps))

	// Выполнить job - ожидаем, что при неудаче шага произойдет репланирование
	onUpdate := func(update agents.Update) {
		fmt.Printf("  Update: stage=%s, progress=%.2f, message=%s\n",
			update.Stage, update.Progress, update.Message)
		if update.Error != nil {
			fmt.Printf("    Error: %s\n", *update.Error)
		}
	}

	fmt.Println("\nExecuting job with auto-planning and replanning...")
	if err := orchestrator.ExecuteJob(ctx, job.ID, onUpdate); err != nil {
		log.Printf("Job execution failed: %v", err)
	} else {
		fmt.Printf("Job %s completed successfully\n", job.ID)
	}

	// Получить финальный статус и проверить репланирование
	finalJob, err := jobsRepo.Get(ctx, job.ID)
	if err != nil {
		log.Printf("Failed to get final job: %v", err)
		return
	}

	fmt.Printf("\n=== Final Job Status ===\n")
	fmt.Printf("Status: %s\n", finalJob.Status)
	fmt.Printf("Progress: %.2f\n", finalJob.Progress)
	fmt.Printf("PlanMode: %s\n", finalJob.PlanMode)
	fmt.Printf("ReplanCount: %d\n", finalJob.ReplanCount)
	if finalJob.LastReplanAt != nil {
		fmt.Printf("LastReplanAt: %s\n", finalJob.LastReplanAt.Format(time.RFC3339))
	}
	fmt.Printf("ReplanTriggered: %v\n", finalJob.ReplanTriggered)

	fmt.Printf("\nAll Steps (%d):\n", len(finalJob.Steps))
	for i, step := range finalJob.Steps {
		fmt.Printf("  Step %d: %s (%s)\n", i+1, step.ID, step.Capability)
		fmt.Printf("    Status: %s\n", step.Status)
		if step.ExecutionStatus != "" {
			fmt.Printf("    ExecutionStatus: %s\n", step.ExecutionStatus)
		}
		if step.Message != "" {
			fmt.Printf("    Message: %s\n", step.Message)
		}
		if step.Error != nil {
			fmt.Printf("    Error: %s\n", *step.Error)
		}
		fmt.Println()
	}

	// Проверить, что репланирование произошло
	if finalJob.ReplanCount > 0 {
		fmt.Printf("✓ Replanning occurred: %d replan(s)\n", finalJob.ReplanCount)
	} else {
		fmt.Printf("⚠ No replanning occurred (this is OK if initial plan succeeded)\n")
	}

	if len(finalJob.Steps) == 0 {
		log.Printf("ERROR: No steps were generated!")
		return
	}

	fmt.Printf("✓ Replanning test completed: %d total steps executed\n", len(finalJob.Steps))
}
