package main

import (
	"context"
	"fmt"
	"log"

	"cloud.google.com/go/firestore"
	"github.com/igorkirisyuk/sanskrite/internal/agents"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
)

// testScenario10: Automatic Telegram source discovery and attachment
// This test simulates a scenario where:
// 1. User asks to find and attach Telegram sources relevant to a query
// 2. System lists existing sources
// 3. System reads data from Telegram channels to evaluate relevance
// 4. System creates new sources and attaches them to project in paused state
// 5. System uses plan_calibrate to review results and plan next steps
func testScenario10(
	ctx context.Context,
	orchestrator *agents.AgentOrchestrator,
	jobsRepo *repo.AgentJobsRepository,
	dataSourcesRepo *repo.DataSourcesRepository,
	sourceProjectsRepo *repo.SourceProjectsRepository,
	telegramClient *telegram.Client,
	publisher *realtime.Publisher,
	fsClient *firestore.Client,
) {
	fmt.Println("\n=== Test Scenario 10: Automatic Telegram Source Discovery ===")

	tenantID := "kirisyuk@gmail.com"
	projectID := "" // Will be found dynamically

	// Step 1: Find a project to use
	fmt.Println("\n--- Step 1: Finding project ---")

	// Query Firestore directly for projects (same approach as test_scenario9)
	iter := fsClient.Collection("core_projects").
		Where("tenantId", "==", tenantID).
		Limit(1).
		Documents(ctx)

	docSnap, err := iter.Next()
	if err == nil {
		var project models.Project
		if err := docSnap.DataTo(&project); err == nil {
			projectID = docSnap.Ref.ID
			fmt.Printf("Found project: %s (displayName: %s)\n", projectID, project.DisplayName)
		}
	}

	if projectID == "" {
		log.Fatalf("ERROR: No project ID found for tenant %s. Please check Firestore.", tenantID)
	}

	// Step 2: Create agent job with auto planning (web search → plan_calibrate loop)
	fmt.Println("\n--- Step 2: Creating agent job ---")

	query := `Найди и добавь Telegram каналы о криптовалютах и блокчейне. Сначала посмотри какие источники уже есть в проекте, затем через web_search найди новые каналы, извлеки их usernames и по одному проверь через telegram_channel_read. Каждое решение принимай через plan_calibrate, и только для релевантных каналов создавай источники.`

	job, err := orchestrator.CreateJob(ctx, tenantID, projectID, "test-user", "telegram_source_discovery_agent", map[string]interface{}{
		"userQuery": query,
		"planMode":  "auto",
		"goal": map[string]interface{}{
			"description": "Find and attach 2-3 relevant Telegram channels about cryptocurrency/blockchain",
			"successCriteria": []string{
				"relevant_sources_created_in_paused_state",
			},
		},
	}, true, "")
	if err != nil {
		log.Fatalf("Failed to create job: %v", err)
	}

	fmt.Printf("Created job: %s (planMode: %s)\n", job.ID, job.PlanMode)
	fmt.Printf("Initial steps count: %d (should be 0, will be generated)\n", len(job.Steps))

	// Step 3: Execute job with auto-planning and calibration
	fmt.Println("\n--- Step 3: Executing job with dynamic planning and calibration ---")

	onUpdate := func(update agents.Update) {
		fmt.Printf("  Update: stage=%s, progress=%.2f, message=%s\n",
			update.Stage, update.Progress, update.Message)
		if update.Error != nil {
			fmt.Printf("    Error: %s\n", *update.Error)
		}
	}

	fmt.Println("\nExecuting job with auto-planning, calibration, and source discovery...")
	if err := orchestrator.ExecuteJob(ctx, job.ID, onUpdate); err != nil {
		log.Printf("Job execution failed: %v", err)
	} else {
		fmt.Printf("Job %s completed successfully\n", job.ID)
	}

	// Step 4: Check final status and verify sources were created
	fmt.Println("\n--- Step 4: Checking final status and created sources ---")
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
		// Print output summary for source-related steps
		if step.Capability == "source_list" || step.Capability == "source_create" || step.Capability == "telegram_channel_read" {
			if step.Output != nil {
				if count, ok := step.Output["count"].(float64); ok {
					fmt.Printf("    Output count: %.0f\n", count)
				}
				if sourceId, ok := step.Output["sourceId"].(string); ok {
					fmt.Printf("    Created sourceId: %s\n", sourceId)
				}
			}
		}
		fmt.Println()
	}

	// Verify sources were created and attached via SourceProjects links
	fmt.Println("\n--- Step 5: Verifying created sources ---")
	var createdSourceCount int
	if sourceProjectsRepo != nil {
		links, err := sourceProjectsRepo.ListByProject(ctx, tenantID, projectID, nil)
		if err != nil {
			log.Printf("Failed to list source-project links: %v", err)
		} else {
			fmt.Printf("Found %d source-project links:\n", len(links))
			for _, link := range links {
				fmt.Printf("  - SourceID: %s, Status: %s\n", link.SourceID, link.Status)
				// Get source details
				if dataSourcesRepo != nil {
					source, err := dataSourcesRepo.Get(ctx, link.SourceID)
					if err == nil && source != nil {
						fmt.Printf("    Source: %s (%s), DisplayName: %s\n", source.ID, source.Kind, source.DisplayName)
						createdSourceCount++
					}
				}
			}
		}
	}

	// Check if goal was achieved
	switch finalJob.Status {
	case models.AgentJobStatusCompleted:
		fmt.Println("\n✓ Job completed successfully!")
		if createdSourceCount > 0 {
			fmt.Printf("✓ Created and attached %d sources to project\n", createdSourceCount)
		}
	case models.AgentJobStatusFailed:
		log.Printf("Job failed: status=%s", finalJob.Status)
	default:
		log.Printf("Job did not complete: status=%s", finalJob.Status)
	}
}
