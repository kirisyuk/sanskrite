package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/crawler"
	"github.com/igorkirisyuk/sanskrite/internal/discovery"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
	"github.com/igorkirisyuk/sanskrite/internal/realtime"
)

type VerifiedHandler struct {
	projectsRepo  *repo.ProjectsRepository
	sharesRepo    *repo.ShareTokensRepository
	interRepo     *repo.InteractionsRepository
	sourcesRepo   *repo.DataSourcesRepository
	docsRepo      *repo.DocumentsRepository
	orchestrator  *discovery.Orchestrator
	genaiHandler  *GenAIHandler
	publisher     *realtime.Publisher
	logger        *log.Logger
}

func NewVerifiedHandler(
	projectsRepo *repo.ProjectsRepository,
	sharesRepo *repo.ShareTokensRepository,
	interRepo *repo.InteractionsRepository,
	sourcesRepo *repo.DataSourcesRepository,
	docsRepo *repo.DocumentsRepository,
	orchestrator *discovery.Orchestrator,
	genaiHandler *GenAIHandler,
	publisher *realtime.Publisher,
	logger *log.Logger,
) *VerifiedHandler {
	return &VerifiedHandler{
		projectsRepo: projectsRepo,
		sharesRepo:   sharesRepo,
		interRepo:    interRepo,
		sourcesRepo:  sourcesRepo,
		docsRepo:     docsRepo,
		orchestrator: orchestrator,
		genaiHandler: genaiHandler,
		publisher:    publisher,
		logger:       logger,
	}
}

type createVerifiedRequest struct {
	URL   string `json:"url"`
	Model string `json:"model,omitempty"` // Optional: gemini_fast, gemini_quality, grok_fast, grok_quality, gpt_fast, gpt_quality, claude_fast, claude_balanced
}

type createVerifiedResponse struct {
	Code   string `json:"code"`
	URL    string `json:"url"`
	Status string `json:"status"`
}

func randomCode(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// generateUniqueCode generates a unique code for verified project
func (h *VerifiedHandler) generateUniqueCode(ctx context.Context) (string, error) {
	for i := 0; i < 10; i++ {
		code := randomCode(10)
		_, err := h.projectsRepo.GetByVerifiedCode(ctx, code)
		if err != nil {
			// Code doesn't exist, we can use it
			return code, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique code after 10 attempts")
}

// POST /api/v1/verified
func (h *VerifiedHandler) CreateVerified(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST is allowed", nil)
		return
	}

	// Verify system authentication
	auth := GetAuthInfo(r.Context())
	if auth == nil || !auth.IsSystem {
		WriteUnauthorized(w, "System authentication required")
		return
	}

	var req createVerifiedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteBadRequest(w, "INVALID_REQUEST", "Invalid request body")
		return
	}

	if req.URL == "" {
		WriteBadRequest(w, "INVALID_URL", "URL is required")
		return
	}

	// Validate model if provided
	if req.Model != "" {
		validModels := map[string]bool{
			"gemini_fast":      true,
			"gemini_quality":   true,
			"grok_fast":        true,
			"grok_quality":     true,
			"gpt_fast":         true,
			"gpt_quality":      true,
			"claude_fast":      true,
			"claude_balanced":  true,
		}
		if !validModels[req.Model] {
			WriteBadRequest(w, "INVALID_MODEL", "Invalid model. Allowed: gemini_fast, gemini_quality, grok_fast, grok_quality, gpt_fast, gpt_quality, claude_fast, claude_balanced")
			return
		}
	}

	ctx := r.Context()

	// Normalize URL
	canonicalURL := crawler.CanonicalizeURL(req.URL)

	// Check if verified project already exists for this URL
	existingProjects, err := h.projectsRepo.List(ctx, "system", nil)
	if err == nil {
		for _, p := range existingProjects {
			if p.VerifiedCode != nil {
				// Check interactions for this project to find URL
				interactions, err := h.interRepo.ListByProject(ctx, p.ID)
				if err == nil && len(interactions) > 0 {
					// First interaction should contain URL metadata
					for _, it := range interactions {
						if it.Kind == "system" && it.Subkind != nil && *it.Subkind == "verified.metadata" {
							// Parse metadata from content (stored as JSON)
							var metadata map[string]interface{}
							if err := json.Unmarshal([]byte(it.Content), &metadata); err == nil {
								if cu, ok := metadata["canonicalUrl"].(string); ok && cu == canonicalURL {
									// Found existing verified project
									status := "completed"
									if s, ok := metadata["status"].(string); ok {
										status = s
									}
									resp := createVerifiedResponse{
										Code:   *p.VerifiedCode,
										URL:    req.URL,
										Status: status,
									}
									WriteJSON(w, http.StatusOK, resp)
									return
								}
							}
							break
						}
					}
				}
			}
		}
	}

	// Generate unique code
	code, err := h.generateUniqueCode(ctx)
	if err != nil {
		h.logger.Printf("verified: failed to generate code: %v", err)
		WriteInternalError(w, "Failed to generate code")
		return
	}

	// Create verified project
	now := time.Now().UTC()
	selectedModel := req.Model
	if selectedModel == "" {
		selectedModel = "gemini_fast" // default model
	}

	project := &models.Project{
		TenantID:    "system",
		DisplayName: fmt.Sprintf("Verified: %s", req.URL),
		Status:      "active",
		OwnerUserID: "system",
		VerifiedCode: &code,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	projectID, err := h.projectsRepo.Create(ctx, project)
	if err != nil {
		h.logger.Printf("verified: failed to create project: %v", err)
		WriteInternalError(w, "Failed to create project")
		return
	}
	project.ID = projectID

	// Create ShareToken for public access
	exp := time.Now().UTC().Add(365 * 24 * time.Hour) // 1 year expiration for verified
	shareToken := &repo.ShareToken{
		ProjectID: projectID,
		Token:     code, // Use same code as token for simplicity
		ExpiresAt: exp,
		CreatedBy: "system",
	}
	_, err = h.sharesRepo.Create(ctx, shareToken)
	if err != nil {
		h.logger.Printf("verified: failed to create share token: %v", err)
		// Continue anyway, token creation is not critical
	}

	// Create metadata interaction
	metadataContent := fmt.Sprintf(`{"url":"%s","canonicalUrl":"%s","model":"%s","status":"pending"}`, req.URL, canonicalURL, selectedModel)
	metadataInteraction := &repo.Interaction{
		TenantID:  "system",
		ProjectID: projectID,
		Kind:      "system",
		Subkind:   stringPtr("verified.metadata"),
		Content:   metadataContent,
		Author: &repo.ParticipantRef{
			ParticipantID: "system",
			Kind:          "system",
		},
	}
	_, err = h.interRepo.Create(ctx, metadataInteraction)
	if err != nil {
		h.logger.Printf("verified: failed to create metadata interaction: %v", err)
		// Continue anyway
	}

	// Publish fetch_start event
	if h.publisher != nil {
		_ = h.publisher.PublishJSON(ctx, "verified.stream.fetch_start", "system", map[string]interface{}{
			"code": code,
			"url":  req.URL,
		})
	}

	// Process asynchronously
	go h.processVerifiedAnalysis(context.Background(), projectID, code, req.URL, canonicalURL, selectedModel)

	// Return immediate response
	resp := createVerifiedResponse{
		Code:   code,
		URL:    req.URL,
		Status: "pending",
	}
	WriteJSON(w, http.StatusCreated, resp)
}

// processVerifiedAnalysis processes the URL analysis asynchronously
func (h *VerifiedHandler) processVerifiedAnalysis(ctx context.Context, projectID, code, url, canonicalURL, model string) {
	// Update metadata interaction status to processing
	interactions, _ := h.interRepo.ListByProject(ctx, projectID)
	var metadataInteractionID string
	for _, it := range interactions {
		if it.Kind == "system" && it.Subkind != nil && *it.Subkind == "verified.metadata" {
			metadataInteractionID = it.ID
			break
		}
	}
	if metadataInteractionID != "" {
		_ = h.interRepo.Update(ctx, metadataInteractionID, map[string]interface{}{
			"content": fmt.Sprintf(`{"url":"%s","canonicalUrl":"%s","model":"%s","status":"processing"}`, url, canonicalURL, model),
		})
	}

	// Step 1: Find or create data source
	existingSource, err := h.sourcesRepo.FindByCanonicalSource(ctx, "", canonicalURL)
	var sourceID string
	if err == nil && existingSource != nil {
		sourceID = existingSource.ID
		h.logger.Printf("verified: using existing source %s for %s", sourceID, canonicalURL)
	} else {
		// Create new public source
		now := time.Now().UTC()
		ds := &models.DataSource{
			TenantID:    "", // Empty for public sources
			Pk:          "global",
			Kind:        "web",
			DisplayName: canonicalURL,
			Status:      "active",
			OwnerUserID: "system",
			Details: models.SourceDetails{
				URI: &canonicalURL,
			},
			CreatedAt: now,
			UpdatedAt: now,
			FirstSeen:  now,
			LastSeen:   now,
		}
		sourceID, err = h.sourcesRepo.Create(ctx, ds)
		if err != nil {
			h.logger.Printf("verified: failed to create source: %v", err)
			// Update status to failed
			if metadataInteractionID != "" {
				errMsg := fmt.Sprintf("Failed to create source: %v", err)
				_ = h.interRepo.Update(ctx, metadataInteractionID, map[string]interface{}{
					"content": fmt.Sprintf(`{"url":"%s","canonicalUrl":"%s","model":"%s","status":"failed","error":"%s"}`, url, canonicalURL, model, errMsg),
				})
			}
			// Publish fetch_failed event
			if h.publisher != nil {
				_ = h.publisher.PublishJSON(ctx, "verified.stream.fetch_failed", "system", map[string]interface{}{
					"code":  code,
					"url":   url,
					"error": fmt.Sprintf("Failed to create source: %v", err),
				})
			}
			return
		}
		h.logger.Printf("verified: created new source %s for %s", sourceID, canonicalURL)
	}

	// Step 2: Ingest web content (creates/updates document and chunks)
	err = h.orchestrator.IngestWeb(ctx, "system", sourceID, canonicalURL)
	if err != nil {
		h.logger.Printf("verified: failed to ingest web content: %v", err)
		// Update status to failed
		if metadataInteractionID != "" {
			errMsg := fmt.Sprintf("Failed to ingest content: %v", err)
			_ = h.interRepo.Update(ctx, metadataInteractionID, map[string]interface{}{
				"content": fmt.Sprintf(`{"url":"%s","canonicalUrl":"%s","model":"%s","status":"failed","error":"%s"}`, url, canonicalURL, model, errMsg),
			})
		}
		// Publish fetch_failed event
		if h.publisher != nil {
			_ = h.publisher.PublishJSON(ctx, "verified.stream.fetch_failed", "system", map[string]interface{}{
				"code":  code,
				"url":   url,
				"error": fmt.Sprintf("Failed to ingest content: %v", err),
			})
		}
		return
	}

	// Publish fetch_success event
	if h.publisher != nil {
		_ = h.publisher.PublishJSON(ctx, "verified.stream.fetch_success", "system", map[string]interface{}{
			"code":     code,
			"url":      url,
			"sourceId": sourceID,
		})
	}

	// Step 3: Get document ID
	doc, err := h.docsRepo.GetByExternalID(ctx, "system", sourceID, canonicalURL)
	if err != nil || doc == nil {
		h.logger.Printf("verified: failed to get document: %v", err)
		// Update status to failed
		if metadataInteractionID != "" {
			errMsg := fmt.Sprintf("Failed to get document: %v", err)
			_ = h.interRepo.Update(ctx, metadataInteractionID, map[string]interface{}{
				"content": fmt.Sprintf(`{"url":"%s","canonicalUrl":"%s","model":"%s","status":"failed","error":"%s"}`, url, canonicalURL, model, errMsg),
			})
		}
		// Publish analysis_failed event
		if h.publisher != nil {
			_ = h.publisher.PublishJSON(ctx, "verified.stream.analysis_failed", "system", map[string]interface{}{
				"code":  code,
				"url":   url,
				"error": fmt.Sprintf("Failed to get document: %v", err),
			})
		}
		return
	}

	// Update metadata with source and document IDs
	if metadataInteractionID != "" {
		_ = h.interRepo.Update(ctx, metadataInteractionID, map[string]interface{}{
			"content": fmt.Sprintf(`{"url":"%s","canonicalUrl":"%s","model":"%s","status":"processing","sourceId":"%s","documentId":"%s"}`, url, canonicalURL, model, sourceID, doc.ID),
		})
	}

	// Publish analysis_start event
	if h.publisher != nil {
		_ = h.publisher.PublishJSON(ctx, "verified.stream.analysis_start", "system", map[string]interface{}{
			"code":       code,
			"url":        url,
			"documentId": doc.ID,
		})
	}

	// Step 4: Perform AI analysis with search grounding
	analysisPrompt := fmt.Sprintf(`Analyze the following web page content and provide an alternative perspective or critical evaluation.

The content is from: %s

YOUR TASK:
Evaluate the content and provide a well-reasoned contribution that adds value through critical analysis or an alternative angle.
If the content has limitations, biases, or missing viewpoints, articulate them constructively.

CRITICAL REQUIREMENTS FOR TONE AND STYLE:
1) Write for educated non-specialists, not for policy analysts or academics. Avoid bureaucratic jargon and acronyms without explanation.
2) Ground your analysis in real human context: who is affected? What are the practical consequences for people, communities, or organizations?
3) Use narrative and concrete examples alongside data. Tell a story, not a spreadsheet. Weave ideas into coherent prose rather than bullet-only lists.
4) Express uncertainty both qualitatively ("likely", "possibly", "unclear") AND quantitatively when logically justified. If you provide metrics (probabilities, confidence ranges, pro/con weights), wrap them in human-readable explanations:
   - Good: "This scenario is more likely to fail than succeed (roughly 60%% risk vs 40%% benefit), primarily because..."
   - Good: "The evidence base is fairly strong (about 78%% of key claims have direct sources), though some projections rely on analogies..."
   - Bad: "Balance: pro 40%%, con 60%%" (naked numbers without context)
   Always explain the reasoning behind your numbers: what base rates, analogies, or models inform them.
5) Acknowledge trade-offs and moral/values dimensions where relevant. Recognize that decisions affect real people's lives.
6) Structure your response for readability: use section headings, short paragraphs, and strategic use of lists—but avoid purely mechanical formatting.
7) When citing sources or data from web search, integrate them naturally into your narrative with brief inline explanations. Avoid dense citation blocks that interrupt flow.
8) FORMALIZATION OPTION: If the topic requires rigorous policy analysis, quantitative risk assessment, or regulatory evaluation, you may use structured analytical frameworks (pro/con weights, evidence quality metrics, probability ranges, scenario trees). However, you MUST:
   - Introduce each framework in plain language first (e.g., "To weigh the arguments systematically, I'll score the strength of evidence...")
   - Explain your methodology and assumptions explicitly
   - Connect metrics back to real-world consequences for people/organizations
   - Close with a human-readable synthesis, not just raw numbers

LANGUAGE REQUIREMENT:
Respond in the same language as the source content. Use English or other languages inline only if you use well-known and accepted terms in the target language.

Do not ask clarifying questions or request additional input; rely only on the provided content.
Do not restate the prompt or context; respond directly with your analysis.

Content:
%s

Your analysis:`, url, doc.FullText)

	// Use selected model with mandatory web search grounding
	genaiReq := gen_combined.GenAIRequest{
		Model:              model,
		Prompt:             analysisPrompt,
		WebSearchEnabled:   true,
		MaxOutputTokens:    2048,
		Temperature:        0.7,
		SystemInstruction:  "You are a critical analyst. Provide thorough, evidence-based analysis with web search verification.",
	}

	result, err := h.genaiHandler.GenerateDirect(ctx, genaiReq)
	if err != nil {
		h.logger.Printf("verified: failed to execute AI analysis: %v", err)
		// Update status to failed
		if metadataInteractionID != "" {
			errMsg := fmt.Sprintf("Failed to analyze: %v", err)
			_ = h.interRepo.Update(ctx, metadataInteractionID, map[string]interface{}{
				"content": fmt.Sprintf(`{"url":"%s","canonicalUrl":"%s","model":"%s","status":"failed","error":"%s"}`, url, canonicalURL, model, errMsg),
			})
		}
		// Publish analysis_failed event
		if h.publisher != nil {
			_ = h.publisher.PublishJSON(ctx, "verified.stream.analysis_failed", "system", map[string]interface{}{
				"code":  code,
				"url":   url,
				"error": fmt.Sprintf("Failed to analyze: %v", err),
			})
		}
		return
	}

	// Step 5: Create analysis interaction
	analysisInteraction := &repo.Interaction{
		TenantID:  "system",
		ProjectID: projectID,
		Kind:      "assistant",
		Subkind:   stringPtr("verified.analysis"),
		Content:   result.Content,
		Model:     &result.Model,
		Author: &repo.ParticipantRef{
			ParticipantID: "system",
			Kind:          "ai",
		},
		Usage: &repo.UsageMeta{
			PromptTokens:    result.Usage.PromptTokens,
			OutputTokens:    result.Usage.OutputTokens,
			TotalTokens:     result.Usage.TotalTokens,
			ReasoningTokens: result.Usage.ReasoningTokens,
		},
		Sources: []string{sourceID},
		CoreDocIDs: []string{doc.ID},
	}
	_, err = h.interRepo.Create(ctx, analysisInteraction)
	if err != nil {
		h.logger.Printf("verified: failed to create analysis interaction: %v", err)
		// Continue anyway
	}

	// Update metadata interaction status to completed
	if metadataInteractionID != "" {
		_ = h.interRepo.Update(ctx, metadataInteractionID, map[string]interface{}{
			"content": fmt.Sprintf(`{"url":"%s","canonicalUrl":"%s","model":"%s","status":"completed","sourceId":"%s","documentId":"%s"}`, url, canonicalURL, result.Model, sourceID, doc.ID),
		})
	}

	// Publish analysis_complete event
	if h.publisher != nil {
		_ = h.publisher.PublishJSON(ctx, "verified.stream.analysis_complete", "system", map[string]interface{}{
			"code":  code,
			"url":   url,
			"model": result.Model,
		})
	}

	h.logger.Printf("verified: successfully completed analysis for %s (code: %s)", canonicalURL, code)
}

// Helper function to create string pointer
func stringPtr(s string) *string {
	return &s
}

// GET /api/v1/public/verified/{code}
func (h *VerifiedHandler) GetPublicVerified(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Missing code")
		return
	}
	code := parts[4]

	// Get project by verified code
	project, err := h.projectsRepo.GetByVerifiedCode(r.Context(), code)
	if err != nil {
		WriteNotFound(w, "Analysis not found")
		return
	}

	// Get share token to check expiration
	shareToken, _, err := h.sharesRepo.GetByToken(r.Context(), code)
	if err != nil || shareToken == nil {
		WriteNotFound(w, "Share token not found")
		return
	}

	// Check if expired
	if !shareToken.IsActive || time.Now().UTC().After(shareToken.ExpiresAt) {
		WriteError(w, http.StatusGone, "EXPIRED", "Analysis has expired", nil)
		return
	}

	// Touch access
	_ = h.sharesRepo.TouchAccess(r.Context(), shareToken.ID)

	// Load interactions
	interactions, err := h.interRepo.ListByProject(r.Context(), project.ID)
	if err != nil {
		h.logger.Printf("verified: failed to load interactions: %v", err)
		interactions = []*repo.Interaction{}
	}

	// Extract metadata and analysis from interactions
	var metadataInteraction *repo.Interaction
	var analysisInteraction *repo.Interaction
	for _, it := range interactions {
		if it.Kind == "system" && it.Subkind != nil && *it.Subkind == "verified.metadata" {
			metadataInteraction = it
		} else if it.Kind == "assistant" && it.Subkind != nil && *it.Subkind == "verified.analysis" {
			analysisInteraction = it
		}
	}

	// Parse metadata
	var url, canonicalURL, status, model string
	var errorMsg *string
	if metadataInteraction != nil {
		// Parse JSON from content
		var metadata map[string]interface{}
		if err := json.Unmarshal([]byte(metadataInteraction.Content), &metadata); err == nil {
			if u, ok := metadata["url"].(string); ok {
				url = u
			}
			if cu, ok := metadata["canonicalUrl"].(string); ok {
				canonicalURL = cu
			}
			if s, ok := metadata["status"].(string); ok {
				status = s
			}
			if m, ok := metadata["model"].(string); ok {
				model = m
			}
			if e, ok := metadata["error"].(string); ok {
				errorMsg = &e
			}
		}
	}

	// Return public format
	type PublicVerified struct {
		ID           string    `json:"id"`
		Code         string    `json:"code"`
		URL          string    `json:"url"`
		CanonicalURL string    `json:"canonicalUrl"`
		Analysis     string    `json:"analysis"`
		Model        string    `json:"model"`
		Status       string    `json:"status"`
		Error        *string   `json:"error,omitempty"`
		CreatedAt    time.Time `json:"createdAt"`
		UpdatedAt    time.Time `json:"updatedAt"`
	}

	pub := PublicVerified{
		ID:           project.ID,
		Code:         code,
		URL:          url,
		CanonicalURL: canonicalURL,
		Model:        model,
		Status:       status,
		Error:        errorMsg,
		CreatedAt:    project.CreatedAt,
		UpdatedAt:    project.UpdatedAt,
	}

	if analysisInteraction != nil {
		pub.Analysis = analysisInteraction.Content
		if analysisInteraction.Model != nil {
			pub.Model = *analysisInteraction.Model
		}
	}

	WriteJSON(w, http.StatusOK, pub)
}

// GET /api/v1/public/documents/{id}
// Public endpoint to retrieve system documents (tenantId = "system" only)
func (h *VerifiedHandler) GetPublicDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET is allowed", nil)
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 5 {
		WriteBadRequest(w, "INVALID_PATH", "Missing document ID")
		return
	}
	documentID := parts[4]

	if documentID == "" {
		WriteBadRequest(w, "INVALID_ID", "Document ID is required")
		return
	}

	ctx := r.Context()

	// Get document by ID
	doc, err := h.docsRepo.GetByID(ctx, documentID)
	if err != nil {
		h.logger.Printf("public document: failed to get document %s: %v", documentID, err)
		WriteNotFound(w, "Document not found")
		return
	}

	// SECURITY: Only allow access to system tenant documents
	if doc.TenantID != "system" {
		h.logger.Printf("public document: access denied to non-system document %s (tenant: %s)", documentID, doc.TenantID)
		WriteNotFound(w, "Document not found")
		return
	}

	// Return public document format
	type PublicDocument struct {
		ID              string    `json:"id"`
		Title           string    `json:"title,omitempty"`
		URL             string    `json:"url,omitempty"`
		FullText        string    `json:"fullText"`
		ContentType     string    `json:"contentType,omitempty"`
		DocumentType    string    `json:"documentType,omitempty"`
		Language        string    `json:"language,omitempty"`
		IngestedAt      time.Time `json:"ingestedAt"`
	}

	pubDoc := PublicDocument{
		ID:              doc.ID,
		Title:           doc.Title,
		URL:             doc.URL,
		FullText:        doc.FullText,
		ContentType:     doc.ContentType,
		DocumentType:    doc.DocumentType,
		Language:        doc.Language,
		IngestedAt:      doc.IngestedAt,
	}

	WriteJSON(w, http.StatusOK, pubDoc)
}
