package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/prompts"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/telegram"
	"golang.org/x/sync/errgroup"
)

type Orchestrator struct {
	exec         *ai.AITaskExecutor
	crawl        *crawler.WebCrawler
	telegram     TelegramFetcher
	projects     *repo.ProjectsRepository
	sources      *repo.DataSourcesRepository
	profiles     *repo.SourceProfilesRepository
	publish      *realtime.Publisher
	logger       *log.Logger
	cache        *repo.ProcessedContentRepository
	interactions *repo.InteractionsRepository
}

type TelegramFetcher interface {
	FetchUpdates(ctx context.Context, channelUsername string, lastMessageID int) ([]telegram.TelegramMessage, error)
}

func NewOrchestrator(exec *ai.AITaskExecutor, crawl *crawler.WebCrawler, tg TelegramFetcher, pr *repo.ProjectsRepository, ds *repo.DataSourcesRepository, pub *realtime.Publisher, logger *log.Logger) *Orchestrator {
	return &Orchestrator{exec: exec, crawl: crawl, telegram: tg, projects: pr, sources: ds, publish: pub, logger: logger}
}

// WithProfiles wires SourceProfiles repository (optional during gradual migration)
func (o *Orchestrator) WithProfiles(sp *repo.SourceProfilesRepository) *Orchestrator {
	o.profiles = sp
	return o
}

// Publish exposes the Publisher for wiring subscribers
func (o *Orchestrator) Publish() *realtime.Publisher { return o.publish }

// publishEvent safely publishes an event, handling nil publisher gracefully
func (o *Orchestrator) publishEvent(ctx context.Context, et realtime.EventType, tenantID string, payload interface{}) {
	if o.publish != nil {
		if err := o.publish.PublishJSON(ctx, et, tenantID, payload); err != nil {
			o.logger.Printf("orchestrator: pubsub %s error: %v", et, err)
		}
	}
}

// WithCache wires processed content cache
func (o *Orchestrator) WithCache(c *repo.ProcessedContentRepository) *Orchestrator {
	o.cache = c
	return o
}

// WithInteractions wires interactions repository for persistence
func (o *Orchestrator) WithInteractions(ir *repo.InteractionsRepository) *Orchestrator {
	o.interactions = ir
	return o
}

type ExecuteInput struct {
	TenantID    string
	ProjectID   string
	QueryText   string
	Language    string
	WebSources  []string
	TgHandles   []string
	Correlation string
	OwnerUID    string
}

// Execute runs discovery orchestration, auto-creating project if needed
func (o *Orchestrator) Execute(ctx context.Context, in ExecuteInput) error {
	// Emit started status (compat with old flow)
	o.publishEvent(ctx, realtime.EventDiscoveryStatus, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID,
		"status":        "started",
		"message":       "Discovery started",
	})

	// Auto-create project if empty
	if in.ProjectID == "" {
		title, err := o.generateProjectTitle(ctx, in.QueryText, in.Language)
		if err != nil {
			o.logger.Printf("orchestrator: generate title error: %v, using fallback", err)
			title = "Discovery " + time.Now().Format("2006-01-02 15:04")
		}

		desc := ""
		view := "discovery"
		proj := &models.Project{
			TenantID:    in.TenantID,
			DisplayName: title,
			Description: &desc,
			View:        &view,
			Status:      "active",
			OwnerUserID: in.OwnerUID,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}

		projectID, err := o.projects.Create(ctx, proj)
		if err != nil {
			o.logger.Printf("orchestrator: create project error: %v", err)
			return fmt.Errorf("failed to create project: %w", err)
		}

		in.ProjectID = projectID
		o.logger.Printf("orchestrator: auto-created project %s: %s", projectID, title)

		// Send WebSocket message about new project
		msg := map[string]interface{}{
			"type":          "discoveryProject",
			"projectId":     projectID,
			"title":         title,
			"views":         []string{"discovery"},
			"tenantId":      in.TenantID,
			"createdAt":     proj.CreatedAt.Format(time.RFC3339),
			"correlationId": in.Correlation,
		}
		o.publishEvent(ctx, "discovery.project", in.TenantID, msg)
	}

	// Process sources in parallel and await all
	eg, gctx := errgroup.WithContext(ctx)
	for _, raw := range in.WebSources {
		canonical := crawler.CanonicalizeURL(strings.TrimSpace(raw))
		if canonical == "" {
			continue
		}
		src := canonical
		eg.Go(func() error { return o.processWebSource(gctx, in, src) })
	}
	for _, h := range in.TgHandles {
		handle := strings.TrimPrefix(strings.TrimSpace(h), "@")
		if handle == "" {
			continue
		}
		h := handle
		eg.Go(func() error { return o.processTelegramSource(gctx, in, h) })
	}
	if err := eg.Wait(); err != nil {
		o.logger.Printf("orchestrator: parallel sources error: %v", err)
	}
	// Synthesize final answer from all profiles in project (simplified: load sources and aggregate)
	if err := o.synthesizeAnswer(ctx, in); err != nil {
		o.logger.Printf("orchestrator: synthesize answer error: %v", err)
	}

	// Emit completed status
	o.publishEvent(ctx, realtime.EventDiscoveryStatus, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID,
		"status":        "completed",
		"message":       "Discovery completed",
	})
	return nil
}

// generateProjectTitle uses LLM to generate a concise title from query
func (o *Orchestrator) generateProjectTitle(ctx context.Context, query, language string) (string, error) {
	if query == "" {
		return "New Chat", nil
	}

	prompt := fmt.Sprintf(`Generate a short, concise title (2-6 words) for a discovery chat based on this query:

Query: %s

Requirements:
- Title must be brief and descriptive
- Use title case
- No quotes or special formatting
- Language: %s

Return ONLY the title, nothing else.`, query, language)

	task := &ai.AITask{
		TaskType:     ai.TaskTypeContentGeneration,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			TextGenerationCapability: true,
			MinCapabilityScore:       5,
			MinContextWindow:         2000,
		},
	}

	result, err := o.exec.ExecuteTask(ctx, task)
	if err != nil {
		return "", err
	}

	title := strings.TrimSpace(result.Content)
	// Sanitize: remove quotes, limit length
	title = strings.Trim(title, `"'`)
	if len(title) > 80 {
		title = title[:77] + "..."
	}
	if title == "" {
		title = "New Chat"
	}

	return title, nil
}

func (o *Orchestrator) processWebSource(ctx context.Context, in ExecuteInput, canonicalURL string) error {
	// find or create DataSource
	var ds *models.DataSource
	needsUpdate := false
	existing, err := o.sources.FindByCanonicalSource(ctx, in.TenantID, canonicalURL)
	if err != nil {
		o.logger.Printf("orchestrator: FindByCanonicalSource for %s error: %v (will create new)", canonicalURL, err)
	}
	if existing != nil {
		o.logger.Printf("orchestrator: found existing web source %s (id=%s) for project %s", canonicalURL, existing.ID, in.ProjectID)
		ds = existing
		// ensure project link present
		if ds.ProjectID == nil || *ds.ProjectID != in.ProjectID {
			if !contains(ds.RelatedProjectIDs, in.ProjectID) {
				ds.RelatedProjectIDs = append(ds.RelatedProjectIDs, in.ProjectID)
				needsUpdate = true
			}
		}
		// Update LastSeen
		ds.LastSeen = time.Now().UTC()
		needsUpdate = true

		// If source was previously processed, skip reprocessing
		if ds.Status == "completed" && o.profiles != nil {
			if needsUpdate {
				_ = o.sources.Upsert(ctx, ds)
			}
			if sp, _ := o.profiles.GetByDataSource(ctx, in.TenantID, ds.ID); sp != nil {
				o.logger.Printf("orchestrator: reusing existing profile for %s", canonicalURL)
				o.publishEvent(ctx, realtime.EventDiscoveryProfile, in.TenantID, map[string]interface{}{
					"projectId": in.ProjectID, "sourceId": ds.ID, "profile": sp.Profile})
				return nil
			}
		}
	} else {
		now := time.Now().UTC()
		o.logger.Printf("orchestrator: creating new web source %s for project %s", canonicalURL, in.ProjectID)
		ds = &models.DataSource{
			TenantID:    in.TenantID,
			Pk:          in.TenantID,
			DisplayName: canonicalURL,
			Kind:        "web",
			Status:      "processing",
			ProjectID:   &in.ProjectID,
			OwnerUserID: in.OwnerUID,
			Details:     models.SourceDetails{URI: &canonicalURL},
			CreatedAt:   now,
			UpdatedAt:   now,
			FirstSeen:   now,
			LastSeen:    now,
		}
		if createdID, err := o.sources.Create(ctx, ds); err != nil {
			o.logger.Printf("orchestrator: failed to create web source %s: %v", canonicalURL, err)
			return err
		} else {
			o.logger.Printf("orchestrator: created web source %s with id=%s", canonicalURL, createdID)
		}
	}
	o.publishEvent(ctx, realtime.EventDiscoveryStatus, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID, "sourceId": ds.ID, "status": "fetching",
		"kind": ds.Kind, "displayName": ds.DisplayName,
	})

	// fetch content
	fr, err := o.crawl.FetchContent(ctx, canonicalURL)
	if err != nil {
		return err
	}

	// cache check
	contentHash := repo.HashContent(fr.Text)
	if o.cache != nil {
		if p, _ := o.cache.Get(ctx, in.TenantID, canonicalURL, contentHash, 24*time.Hour); p != nil {
			ds.Status = "completed"
			_ = o.sources.Upsert(ctx, ds)
			if o.profiles != nil {
				_ = o.profiles.Upsert(ctx, &models.SourceProfile{ID: in.TenantID + "_" + ds.ID, TenantID: in.TenantID, DataSourceID: ds.ID, SchemaVersion: "1.0", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Profile: *p})
			}
			o.publishEvent(ctx, realtime.EventDiscoveryProfile, in.TenantID, map[string]interface{}{
				"correlationId": in.Correlation,
				"projectId":     in.ProjectID, "sourceId": ds.ID, "profile": p})
			return nil
		}
	}

	// profile via AITaskExecutor
	prof, err := o.profile(ctx, ds.DisplayName, fr.Text, in.Language)
	if err != nil {
		return err
	}
	ds.Status = "completed"
	if err := o.sources.Upsert(ctx, ds); err != nil {
		o.logger.Printf("orchestrator: failed to upsert web source %s (profile size: ~%d bytes): %v", canonicalURL, len(mustJSON(prof)), err)
		return err
	}
	o.logger.Printf("orchestrator: saved web profile for %s, size: ~%d bytes", canonicalURL, len(mustJSON(prof)))
	if o.cache != nil {
		_ = o.cache.Put(ctx, in.TenantID, canonicalURL, contentHash, prof)
	}
	if o.profiles != nil {
		_ = o.profiles.Upsert(ctx, &models.SourceProfile{ID: in.TenantID + "_" + ds.ID, TenantID: in.TenantID, DataSourceID: ds.ID, SchemaVersion: "1.0", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Profile: *prof})
	}

	o.publishEvent(ctx, realtime.EventDiscoveryProfile, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID, "sourceId": ds.ID, "profile": prof})
	return nil
}

func (o *Orchestrator) processTelegramSource(ctx context.Context, in ExecuteInput, handle string) error {
	// find or create
	var ds *models.DataSource
	needsUpdate := false
	existing, err := o.sources.FindByCanonicalSource(ctx, in.TenantID, handle)
	if err != nil {
		o.logger.Printf("orchestrator: FindByCanonicalSource for @%s error: %v (will create new)", handle, err)
	}
	if existing != nil {
		o.logger.Printf("orchestrator: found existing telegram source @%s (id=%s) for project %s", handle, existing.ID, in.ProjectID)
		ds = existing
		if ds.ProjectID == nil || *ds.ProjectID != in.ProjectID {
			if !contains(ds.RelatedProjectIDs, in.ProjectID) {
				ds.RelatedProjectIDs = append(ds.RelatedProjectIDs, in.ProjectID)
				needsUpdate = true
			}
		}
		// Update LastSeen
		ds.LastSeen = time.Now().UTC()
		needsUpdate = true

		// If source was previously processed, skip reprocessing
		if ds.Status == "completed" && o.profiles != nil {
			if needsUpdate {
				_ = o.sources.Upsert(ctx, ds)
			}
			if sp, _ := o.profiles.GetByDataSource(ctx, in.TenantID, ds.ID); sp != nil {
				o.logger.Printf("orchestrator: reusing existing profile for @%s", handle)
				o.publishEvent(ctx, realtime.EventDiscoveryProfile, in.TenantID, map[string]interface{}{
					"projectId": in.ProjectID, "sourceId": ds.ID, "profile": sp.Profile})
				return nil
			}
		}
	} else {
		now := time.Now().UTC()
		o.logger.Printf("orchestrator: creating new telegram source @%s for project %s", handle, in.ProjectID)
		ds = &models.DataSource{
			TenantID:    in.TenantID,
			Pk:          in.TenantID,
			DisplayName: "@" + handle,
			Kind:        "telegram_channel",
			Status:      "processing",
			ProjectID:   &in.ProjectID,
			OwnerUserID: in.OwnerUID,
			Details:     models.SourceDetails{ChannelUsername: &handle},
			CreatedAt:   now, UpdatedAt: now, FirstSeen: now, LastSeen: now,
		}
		if createdID, err := o.sources.Create(ctx, ds); err != nil {
			o.logger.Printf("orchestrator: failed to create telegram source @%s: %v", handle, err)
			return err
		} else {
			o.logger.Printf("orchestrator: created telegram source @%s with id=%s", handle, createdID)
		}
	}
	o.publishEvent(ctx, realtime.EventDiscoveryStatus, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID, "sourceId": ds.ID, "status": "fetching",
		"kind": ds.Kind, "displayName": ds.DisplayName,
	})

	// fetch posts (last 30 to avoid Firestore 1MB document limit)
	msgs, err := o.telegram.FetchUpdates(ctx, handle, 0)
	if err != nil {
		o.logger.Printf("orchestrator: telegram fetch error for @%s: %v", handle, err)
		return err
	}
	// Limit to 30 most recent messages
	if len(msgs) > 30 {
		msgs = msgs[len(msgs)-30:]
	}
	// concatenate text
	var b strings.Builder
	for _, m := range msgs {
		if m.Text != "" {
			b.WriteString(m.Text)
			b.WriteString("\n\n")
		}
	}

	prof, err := o.profile(ctx, ds.DisplayName, b.String(), in.Language)
	if err != nil {
		o.logger.Printf("orchestrator: profiling error for @%s: %v", handle, err)
		return err
	}
	ds.Status = "completed"
	if err := o.sources.Upsert(ctx, ds); err != nil {
		o.logger.Printf("orchestrator: failed to upsert telegram source @%s (profile size: ~%d bytes): %v", handle, len(mustJSON(prof)), err)
		return err
	}
	o.logger.Printf("orchestrator: saved telegram profile for @%s, size: ~%d bytes", handle, len(mustJSON(prof)))
	if o.profiles != nil {
		_ = o.profiles.Upsert(ctx, &models.SourceProfile{ID: in.TenantID + "_" + ds.ID, TenantID: in.TenantID, DataSourceID: ds.ID, SchemaVersion: "1.0", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Profile: *prof})
	}
	o.publishEvent(ctx, realtime.EventDiscoveryProfile, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID, "sourceId": ds.ID, "profile": prof})
	return nil
}

func (o *Orchestrator) profile(ctx context.Context, sourceId, extractedText, lang string) (*models.SourceProfileData, error) {
	pr := NewSourceProfiler(o.exec, o.logger)
	return pr.ProfileSource(ctx, ProfileRequest{SourceID: sourceId, ExtractedText: extractedText, Language: lang})
}

func (o *Orchestrator) synthesizeAnswer(ctx context.Context, in ExecuteInput) error {
	// Load sources
	list, err := o.sources.ListByProject(ctx, in.TenantID, in.ProjectID)
	if err != nil {
		o.logger.Printf("orchestrator: failed to list sources for project %s: %v", in.ProjectID, err)
		return err
	}
	o.logger.Printf("orchestrator: loaded %d sources for project %s, preparing synthesis", len(list), in.ProjectID)
	// Build profiles array JSON via SourceProfiles repository
	profiles := make([]map[string]interface{}, 0, len(list))
	if o.profiles != nil {
		profs, _ := o.profiles.ListByProject(ctx, in.TenantID, in.ProjectID, o.sources)
		for _, sp := range profs {
			p := sp.Profile
			profiles = append(profiles, map[string]interface{}{
				"editorialStance":         p.EditorialStance,
				"agenda":                  p.Agenda,
				"motivation":              p.Motivation,
				"trustScore":              p.TrustScore,
				"trustRationale":          p.TrustRationale,
				"behaviorPatterns":        p.BehaviorPatterns,
				"contentPatterns":         p.ContentPatterns,
				"topics":                  p.Topics,
				"editorialMethods":        p.EditorialMethods,
				"goals":                   p.Goals,
				"goalAlignmentScore":      p.GoalAlignmentScore,
				"metrics":                 p.Metrics,
				"targetAudienceCountries": p.TargetAudienceCountries,
				"redFlags":                p.RedFlags,
				"coordinatedSources":      p.CoordinatedSources,
				"flags":                   p.Flags,
			})
		}
	}
	o.logger.Printf("orchestrator: synthesis will use %d profiles (out of %d sources)", len(profiles), len(list))

	profilesJSON := mustJSON(profiles)
	prompt := prompts.Format(prompts.AnswerSynthesisPrompt, map[string]string{
		"queryText":    in.QueryText,
		"language":     in.Language,
		"sourcesCount": fmt.Sprintf("%d", len(profiles)),
		"profilesJson": profilesJSON,
	})

	task := &ai.AITask{
		TaskType:       ai.TaskTypeKnowledgeSynthesis,
		InputContent:   prompt,
		AIRequirements: ai.AIRequirements{TextGenerationCapability: true},
	}
	res, err := o.exec.ExecuteTask(ctx, task)
	if err != nil {
		return err
	}
	// persist interaction (answer)
	if o.interactions != nil {
		_, _ = o.interactions.Create(ctx, &repo.Interaction{TenantID: in.TenantID, ProjectID: in.ProjectID, Kind: "discovery.answer", Content: res.Content})
	}
	o.publishEvent(ctx, realtime.EventDiscoveryAnswer, in.TenantID, map[string]interface{}{
		"correlationId": in.Correlation,
		"projectId":     in.ProjectID, "answer": res.Content})
	return nil
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func contains(arr []string, s string) bool {
	for _, x := range arr {
		if x == s {
			return true
		}
	}
	return false
}
