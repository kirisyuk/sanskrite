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

// testScenario7AutoPlanning тестирует автоматическое планирование
func testScenario7AutoPlanning(
	ctx context.Context,
	orchestrator *agents.AgentOrchestrator,
	jobsRepo *repo.AgentJobsRepository,
	coreDocsRepo *repo.CoreDocsRepo,
) {
	fmt.Println("\n--- Scenario 7: Auto Planning ---")

	// Создать тестовый документ, если его нет
	testDocID := "test-doc-auto-planning"
	testDoc, err := coreDocsRepo.Get(ctx, testDocID)
	if err != nil {
		now := time.Now().UTC()
		sizeBytes := int64(200)
		extractedText := "Документ содержит информацию о поставках картофеля в 2024 году. Объемы поставок: январь - 100 тонн, февраль - 120 тонн, март - 150 тонн. Основной поставщик - ООО 'Агро-Плюс'."
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
	// НЕ указываем шаги явно - они должны быть сгенерированы автоматически
	task := map[string]interface{}{
		"userQuery": "Найди документ о поставках картофеля, прочитай его, проанализируй содержимое и сохрани краткое резюме",
		"planMode":  "auto", // Включаем автоматическое планирование
	}

	job, err := orchestrator.CreateJob(ctx, "test-tenant", "test-project", "test-user", "auto_planning_agent", task, true, "")
	if err != nil {
		log.Fatalf("Failed to create job: %v", err)
	}

	fmt.Printf("Created job: %s (planMode: %s)\n", job.ID, job.PlanMode)
	fmt.Printf("Initial steps count: %d (should be 0, will be generated)\n", len(job.Steps))

	// Выполнить job - orchestrator должен автоматически сгенерировать план
	onUpdate := func(update agents.Update) {
		fmt.Printf("  Update: stage=%s, progress=%.2f, message=%s\n",
			update.Stage, update.Progress, update.Message)
		if update.Error != nil {
			fmt.Printf("    Error: %s\n", *update.Error)
		}
	}

	fmt.Println("\nExecuting job with auto-planning...")
	if err := orchestrator.ExecuteJob(ctx, job.ID, onUpdate); err != nil {
		log.Printf("Job execution failed: %v", err)
	} else {
		fmt.Printf("Job %s completed successfully\n", job.ID)
	}

	// Получить финальный статус и проверить план
	finalJob, err := jobsRepo.Get(ctx, job.ID)
	if err != nil {
		log.Printf("Failed to get final job: %v", err)
		return
	}

	fmt.Printf("\n=== Final Job Status ===\n")
	fmt.Printf("Status: %s\n", finalJob.Status)
	fmt.Printf("Progress: %.2f\n", finalJob.Progress)
	fmt.Printf("PlanMode: %s\n", finalJob.PlanMode)
	fmt.Printf("UserQuery: %s\n", finalJob.UserQuery)

	if finalJob.Intent != nil {
		fmt.Printf("Intent: %+v\n", finalJob.Intent)
	}
	if finalJob.Goal != nil {
		fmt.Printf("Goal: %+v\n", finalJob.Goal)
	}

	fmt.Printf("\nGenerated Steps (%d):\n", len(finalJob.Steps))
	for i, step := range finalJob.Steps {
		fmt.Printf("  Step %d: %s (%s)\n", i+1, step.ID, step.Capability)
		fmt.Printf("    Status: %s\n", step.Status)
		if step.ExecutionStatus != "" {
			fmt.Printf("    ExecutionStatus: %s\n", step.ExecutionStatus)
		}
		if step.Confidence != nil {
			fmt.Printf("    Confidence: %.2f\n", *step.Confidence)
		}
		if step.Message != "" {
			fmt.Printf("    Message: %s\n", step.Message)
		}
		if step.Error != nil {
			fmt.Printf("    Error: %s\n", *step.Error)
		}
		if len(step.DependsOn) > 0 {
			fmt.Printf("    DependsOn: %v\n", step.DependsOn)
		}
		fmt.Println()
	}

	// Проверить, что план был сгенерирован
	if len(finalJob.Steps) == 0 {
		log.Printf("ERROR: No steps were generated! Auto-planning failed.")
		return
	}

	fmt.Printf("✓ Auto-planning test completed: %d steps were generated and executed\n", len(finalJob.Steps))
}
