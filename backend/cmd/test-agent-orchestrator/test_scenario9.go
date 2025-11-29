package main

import (
	"context"
	"fmt"
	"log"

	"cloud.google.com/go/firestore"

	"github.com/igorkirisyuk/sanskrite/internal/agents"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// testScenario9: Dynamic planning for supply planning with data improvement
// This test simulates a real-world scenario where:
// 1. User asks for supply planning based on a CSV file
// 2. System needs to find the file, read it, understand data quality issues
// 3. Improve the data, save it, make it available for BigQuery
// 4. Execute BigQuery query, handle errors, and retry if needed
func testScenario9(
	ctx context.Context,
	orchestrator *agents.AgentOrchestrator,
	jobsRepo *repo.AgentJobsRepository,
	_ *repo.CoreDocsRepo,
	fsClient *firestore.Client,
) {
	fmt.Println("\n=== Test Scenario 9: Dynamic Supply Planning with Data Improvement ===")

	tenantID := "kirisyuk@gmail.com"
	projectID := "" // Will be found dynamically

	// Step 1: Find data123.csv to get projectID (required for CreateJob validation)
	fmt.Println("\n--- Step 1: Finding data123.csv ---")

	// Query Firestore directly for data123.csv
	iter := fsClient.Collection("core_docs").
		Where("tenantId", "==", tenantID).
		Where("filename", "==", "data123.csv").
		Limit(1).
		Documents(ctx)

	docSnap, err := iter.Next()
	if err != nil {
		log.Fatalf("Failed to find data123.csv: %v", err)
	}

	var coreDoc models.CoreDoc
	if err := docSnap.DataTo(&coreDoc); err != nil {
		log.Fatalf("Failed to parse CoreDoc: %v", err)
	}

	projectID = coreDoc.ProjectID
	fmt.Printf("Found data123.csv: coreDocID=%s, projectID=%s\n", coreDoc.ID, projectID)

	// Проверим, существует ли Document для этого CoreDoc
	docsIter := fsClient.Collection("rag_documents").
		Where("tenantId", "==", tenantID).
		Where("attrs.coreDocId", "==", coreDoc.ID).
		Limit(1).
		Documents(ctx)
	ragDocSnap, ragDocErr := docsIter.Next()
	var ragDocID string
	if ragDocErr != nil {
		fmt.Printf("WARNING: Document for CoreDoc %s not found: %v\n", coreDoc.ID, ragDocErr)
		fmt.Printf("This means the CoreDoc was not indexed in RAG system!\n")
	} else {
		var ragDoc map[string]interface{}
		ragDocSnap.DataTo(&ragDoc)
		ragDocID = ragDocSnap.Ref.ID
		fmt.Printf("Found Document for CoreDoc: Document.ID=%s, StoragePath=%v\n", ragDocID, ragDoc["storagePath"])
		fmt.Printf("NOTE: Vertex AI Search may return outdated files from GCS that no longer exist in Firestore!\n")
		fmt.Printf("The correct Document ID to use is: %s\n", ragDocID)
	}
	fmt.Printf("The agent should: 1) search for data123.csv, 2) read it, 3) analyze data quality,\n")
	fmt.Printf("4) improve data if needed, 5) save it, 6) execute BigQuery query, 7) handle errors and retry\n")

	// Step 2: Create agent job with auto-planning
	fmt.Println("\n--- Step 2: Creating agent job ---")

	query := "Выполни BigQuery анализ файла data123.csv из core_docs_metadata. Используй SPLIT для парсинга CSV (разделитель - табуляция). Посчитай количество строк в файле и покажи первые 5 строк данных. Сохрани результат в отчет."

	job, err := orchestrator.CreateJob(ctx, tenantID, projectID, "test-user", "supply_planning_agent", map[string]interface{}{
		"userQuery": query,
		"planMode":  "auto",
	}, true, "")
	if err != nil {
		log.Fatalf("Failed to create job: %v", err)
	}

	fmt.Printf("Created job: %s (planMode: %s)\n", job.ID, job.PlanMode)
	fmt.Printf("Initial steps count: %d (should be 0, will be generated)\n", len(job.Steps))

	// Step 3: Execute job with auto-planning and replanning
	fmt.Println("\n--- Step 3: Executing job with dynamic planning ---")

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

	// Step 4: Check final status
	fmt.Println("\n--- Step 4: Checking final status ---")
	finalJob, err := jobsRepo.Get(ctx, job.ID)
	if err != nil {
		log.Printf("Failed to get final job: %v", err)
		return
	}

	fmt.Printf("\n=== Final Job Status ===\n")
	fmt.Printf("Status: %s\n", finalJob.Status)
	fmt.Printf("Progress: %.2f\n", finalJob.Progress)
	fmt.Printf("PlanMode: %s\n", finalJob.PlanMode)

	// Print step summary
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
		if len(step.DependsOn) > 0 {
			fmt.Printf("    DependsOn: %v\n", step.DependsOn)
		}
		fmt.Println()
	}

	// Check if goal was achieved
	switch finalJob.Status {
	case models.AgentJobStatusCompleted:
		fmt.Println("\n✓ Job completed successfully!")
	case models.AgentJobStatusFailed:
		log.Printf("Job failed: status=%s", finalJob.Status)
	default:
		log.Printf("Job did not complete: status=%s", finalJob.Status)
	}
}
