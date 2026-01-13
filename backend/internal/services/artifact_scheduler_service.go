package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// ArtifactSchedulerService handles scheduled auto-updates for artifacts
type ArtifactSchedulerService struct {
	coreDocsRepo  *repo.CoreDocsRepo
	orchestrator  *ArtifactOrchestratorService
	checkInterval time.Duration // How often to check for scheduled updates
}

// NewArtifactSchedulerService creates a new scheduler service
func NewArtifactSchedulerService(
	coreDocsRepo *repo.CoreDocsRepo,
	orchestrator *ArtifactOrchestratorService,
) *ArtifactSchedulerService {
	return &ArtifactSchedulerService{
		coreDocsRepo:  coreDocsRepo,
		orchestrator:  orchestrator,
		checkInterval: 1 * time.Hour, // Check every hour by default
	}
}

// WithCheckInterval sets the check interval for scheduled updates
func (s *ArtifactSchedulerService) WithCheckInterval(interval time.Duration) *ArtifactSchedulerService {
	s.checkInterval = interval
	return s
}

// CheckInterval returns the current check interval
func (s *ArtifactSchedulerService) CheckInterval() time.Duration {
	return s.checkInterval
}

// Start starts the scheduler loop
func (s *ArtifactSchedulerService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	// Run immediately on startup
	s.processScheduledUpdates(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processScheduledUpdates(ctx)
		}
	}
}

// processScheduledUpdates checks all artifacts and triggers updates for scheduled ones
func (s *ArtifactSchedulerService) processScheduledUpdates(ctx context.Context) {
	_ = ctx // reserved for future: batched Firestore queries / cancellation, keep signature stable
	// Get all artifacts with auto-update enabled
	// Note: This is a simplified approach - in production, you might want to:
	// 1. Use Firestore queries to filter by autoUpdateEnabled and trigger type
	// 2. Add indexes for better performance
	// 3. Process in batches

	// For now, we'll need to list all artifacts and filter in memory
	// This is acceptable if artifact count is reasonable (< 1000 per tenant)
	// For larger scale, implement Firestore query with proper indexes

	// Since we don't have a direct way to query all artifacts across all tenants,
	// we'll need to implement this differently. For now, let's create a method
	// that can be called per-project or per-tenant.

	log.Printf("[ArtifactScheduler] Checking for scheduled artifact updates...")
}

// ProcessProjectArtifacts processes scheduled updates for artifacts in a specific project
func (s *ArtifactSchedulerService) ProcessProjectArtifacts(ctx context.Context, tenantID, projectID string) error {
	// List all documents in the project
	docs, err := s.coreDocsRepo.ListByProject(ctx, tenantID, projectID, 0, nil)
	if err != nil {
		return fmt.Errorf("failed to list documents: %w", err)
	}

	now := time.Now().UTC()
	triggeredCount := 0

	for _, doc := range docs {
		// Skip if not an artifact
		if doc.DocType == nil || *doc.DocType != "dynamic_artifact" {
			continue
		}

		// Skip if auto-update not enabled
		if doc.ArtifactConfig == nil || !doc.ArtifactConfig.AutoUpdateEnabled {
			continue
		}

		// Skip if no auto-update config
		if doc.ArtifactConfig.AutoUpdateConfig == nil {
			continue
		}

		trigger := doc.ArtifactConfig.AutoUpdateConfig.Trigger

		// Check if this artifact needs scheduled update
		shouldUpdate := false
		switch trigger {
		case "daily":
			shouldUpdate = s.shouldTriggerDaily(ctx, doc, now)
		case "weekly":
			shouldUpdate = s.shouldTriggerWeekly(ctx, doc, now)
		default:
			// Not a scheduled trigger, skip
			continue
		}

		if shouldUpdate {
			// Check conditions if specified
			if doc.ArtifactConfig.AutoUpdateConfig.Conditions != nil {
				// For scheduled updates, conditions are less relevant,
				// but we can still check them if needed
				// For now, we'll skip condition checks for scheduled updates
			}

			// Start generation in auto mode
			session, err := s.orchestrator.StartGeneration(ctx, StartGenerationRequest{
				ArtifactID: doc.ID,
				Mode:       "auto",
				TenantID:   tenantID,
				ProjectID:  projectID,
			})

			if err != nil {
				log.Printf("[ArtifactScheduler] Failed to start generation for artifact %s: %v", doc.ID, err)
				continue
			}

			log.Printf("[ArtifactScheduler] Triggered scheduled update for artifact %s (session: %s, trigger: %s)", doc.ID, session.ID, trigger)
			triggeredCount++
		}
	}

	if triggeredCount > 0 {
		log.Printf("[ArtifactScheduler] Triggered %d scheduled updates for project %s", triggeredCount, projectID)
	}

	return nil
}

// shouldTriggerDaily checks if a daily update should be triggered
func (s *ArtifactSchedulerService) shouldTriggerDaily(ctx context.Context, artifact *models.CoreDoc, now time.Time) bool {
	_ = ctx // reserved for future: tenant/timezone lookup, keep signature stable
	// Check last update time
	lastUpdate := s.getLastUpdateTime(artifact)
	if lastUpdate.IsZero() {
		// Never updated, check if schedule time has passed today
		return s.isScheduleTimePassed(artifact, now)
	}

	// Check if 24 hours have passed since last update
	hoursSinceUpdate := now.Sub(lastUpdate).Hours()
	if hoursSinceUpdate < 24 {
		return false
	}

	// If schedule time is set, check if we're past the scheduled time today
	if artifact.ArtifactConfig.AutoUpdateConfig.ScheduleTime != nil {
		return s.isScheduleTimePassed(artifact, now)
	}

	// No schedule time, trigger if 24 hours passed
	return true
}

// shouldTriggerWeekly checks if a weekly update should be triggered
func (s *ArtifactSchedulerService) shouldTriggerWeekly(ctx context.Context, artifact *models.CoreDoc, now time.Time) bool {
	_ = ctx // reserved for future: tenant/timezone lookup, keep signature stable
	// Check last update time
	lastUpdate := s.getLastUpdateTime(artifact)
	if lastUpdate.IsZero() {
		// Never updated, check if schedule time has passed this week
		return s.isScheduleTimePassed(artifact, now)
	}

	// Check if 7 days have passed since last update
	daysSinceUpdate := now.Sub(lastUpdate).Hours() / 24
	if daysSinceUpdate < 7 {
		return false
	}

	// If schedule time is set, check if we're past the scheduled time this week
	if artifact.ArtifactConfig.AutoUpdateConfig.ScheduleTime != nil {
		return s.isScheduleTimePassed(artifact, now)
	}

	// No schedule time, trigger if 7 days passed
	return true
}

// isScheduleTimePassed checks if the scheduled time has passed
// For daily: checks if schedule time has passed today
// For weekly: checks if schedule time has passed this week (same day of week)
func (s *ArtifactSchedulerService) isScheduleTimePassed(artifact *models.CoreDoc, now time.Time) bool {
	if artifact.ArtifactConfig == nil || artifact.ArtifactConfig.AutoUpdateConfig == nil {
		return false
	}

	scheduleTime := artifact.ArtifactConfig.AutoUpdateConfig.ScheduleTime
	if scheduleTime == nil || *scheduleTime == "" {
		// No schedule time set, allow trigger
		return true
	}

	// Parse schedule time (HH:MM format)
	parsedTime, err := time.Parse("15:04", *scheduleTime)
	if err != nil {
		log.Printf("[ArtifactScheduler] Invalid schedule time format: %s (expected HH:MM): %v", *scheduleTime, err)
		return true // Default to allowing trigger if format is invalid
	}

	hour := parsedTime.Hour()
	minute := parsedTime.Minute()

	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		log.Printf("[ArtifactScheduler] Invalid schedule time values: hour=%d, minute=%d", hour, minute)
		return true
	}

	// Get scheduled time today (or this week for weekly)
	trigger := artifact.ArtifactConfig.AutoUpdateConfig.Trigger
	scheduledTime := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)

	if trigger == "weekly" {
		// For weekly, find the same day of week
		lastUpdate := s.getLastUpdateTime(artifact)
		if !lastUpdate.IsZero() {
			// Find next occurrence of the same weekday
			daysSinceLastUpdate := int(now.Sub(lastUpdate).Hours() / 24)
			if daysSinceLastUpdate < 7 {
				// Not a full week yet
				return false
			}
		}
		// For weekly, we check if we're past the scheduled time on the same weekday
		// This is a simplified check - in production, you might want to store the day of week
	}

	// Check if scheduled time has passed today
	return now.After(scheduledTime) || now.Equal(scheduledTime)
}

// getLastUpdateTime gets the last update time from artifact versions
func (s *ArtifactSchedulerService) getLastUpdateTime(artifact *models.CoreDoc) time.Time {
	if artifact.ArtifactConfig == nil || len(artifact.ArtifactConfig.Versions) == 0 {
		// Check if any section has been generated
		for _, section := range artifact.ArtifactConfig.Structure {
			if section.GeneratedAt != nil {
				return *section.GeneratedAt
			}
		}
		// Fallback to artifact creation time
		return artifact.CreatedAt
	}

	// Get the most recent version timestamp
	// Versions are sorted by timestamp desc (newest first)
	mostRecent := artifact.ArtifactConfig.Versions[0]
	return mostRecent.Timestamp
}

// ProcessAllProjects processes scheduled updates for all projects
// This is a helper method that can be called periodically
// Note: In production, you might want to implement this differently,
// e.g., using Cloud Scheduler to call per-project endpoints
func (s *ArtifactSchedulerService) ProcessAllProjects(ctx context.Context) error {
	// This would require listing all projects, which might not be available
	// in the current architecture. For now, this is a placeholder.
	// In production, you might:
	// 1. Use Cloud Scheduler to call per-project endpoints
	// 2. Use a job queue to process projects in batches
	// 3. Add a Firestore query to find all artifacts with scheduled updates

	log.Printf("[ArtifactScheduler] ProcessAllProjects called - implementation needed")
	return nil
}
