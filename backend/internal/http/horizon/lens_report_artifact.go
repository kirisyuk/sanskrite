package horizon

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	artemplates "github.com/igorkirisyuk/sanskrite/internal/artifacts/templates"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathquery"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/simscope"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/supplychainreport"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/workscope"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

const lensSupplyChainReportShareTTL = 30 * 24 * time.Hour

type lensSupplyChainReportResponse struct {
	ArtifactID       string `json:"artifactId"`
	ShareToken       string `json:"shareToken"`
	ShareURL         string `json:"shareUrl"`
	ProjectShareURL  string `json:"projectShareUrl"`
	ExpiresAt        string `json:"expiresAt"`
	Filename         string `json:"filename"`
	ProjectID        string `json:"projectId"`
	LensID           string `json:"lensId"`
	MaterializedKind string `json:"materializedKind"`
}

type lensSupplyChainReportRequest struct {
	LLMEnabled      *bool  `json:"llmEnabled,omitempty"`
	Model           string `json:"model,omitempty"`
	MaxOutputTokens *int   `json:"maxOutputTokens,omitempty"`
	ThinkingBudget  *int   `json:"thinkingBudget,omitempty"`
}

type lensSupplyChainReportData struct {
	lens               *horizonmodels.Lens
	scopePaths         []string
	workScope          workscope.WorkScope
	resolved           workscope.ResolvedWorkSet
	allMaterialized    []*horizonmodels.MaterializedArtifact
	chains             []*horizonmodels.MaterializedArtifact
	scopedChainCount   int
	discoveredChainCnt int
	fragmentsByPath    map[string]*horizonmodels.HorizonFragment
	riskOverlay        *horizonmodels.SupplyChainLensRiskOverlay
	createdAt          time.Time
}

type lensSupplyChainSectionGenerator struct {
	h *HorizonHandlers
}

func (g lensSupplyChainSectionGenerator) Generate(ctx context.Context, req supplychainreport.GenerateRequest) (*supplychainreport.GenerateResponse, error) {
	if g.h == nil || g.h.genAIHandler == nil {
		return nil, fmt.Errorf("genai handler is not configured")
	}
	resp, err := g.h.genAIHandler.GenerateDirect(ctx, genAIGenerateRequestFromSupplyChain(req))
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("empty genai response")
	}
	return &supplychainreport.GenerateResponse{
		Content:    resp.Content,
		Model:      strings.TrimSpace(resp.Model),
		TokensUsed: resp.Usage.TotalTokens,
	}, nil
}

func (h *HorizonHandlers) CreateLensSupplyChainReportArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authInfo, err := requireAuth(r)
	if err != nil || authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	if h.coreDocsRepo == nil || h.shareTokensRepo == nil {
		http.Error(w, "Artifact storage is not configured", http.StatusServiceUnavailable)
		return
	}
	if h.materializedArtifactsRepo == nil || h.service == nil || h.service.Fragments() == nil || h.service.Edges() == nil {
		http.Error(w, "Supply-chain report substrate is not configured", http.StatusServiceUnavailable)
		return
	}
	reqBody := defaultLensSupplyChainReportRequest()
	if r.Body != nil && (r.ContentLength > 0 || strings.Contains(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "application/json")) {
		defer r.Body.Close()
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&reqBody); err != nil && err.Error() != "EOF" {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}
	}

	lens, err := h.resolveGraphIntentLens(r.Context(), r, authInfo.TenantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	reportData, err := h.buildLensSupplyChainReportData(r.Context(), r, lens, authInfo.TenantID)
	if err != nil {
		http.Error(w, "Failed to build report substrate", http.StatusInternalServerError)
		return
	}

	artifact, err := h.createLensSupplyChainReportArtifact(r.Context(), lens, authInfo.UID, reportData, reqBody)
	if err != nil {
		http.Error(w, "Failed to create report artifact", http.StatusInternalServerError)
		return
	}

	projectShareURL, shareToken, expiresAt, err := h.createProjectShareLink(r.Context(), r, lens.ProjectID, authInfo.UID)
	if err != nil {
		http.Error(w, "Failed to create share token", http.StatusInternalServerError)
		return
	}

	documentShareURL := projectShareURL + "?doc=" + artifact.ID
	response := lensSupplyChainReportResponse{
		ArtifactID:       artifact.ID,
		ShareToken:       shareToken,
		ShareURL:         documentShareURL,
		ProjectShareURL:  projectShareURL,
		ExpiresAt:        expiresAt.Format(time.RFC3339),
		Filename:         artifact.Filename,
		ProjectID:        lens.ProjectID,
		LensID:           lens.ID,
		MaterializedKind: "dynamic_artifact",
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (h *HorizonHandlers) buildLensSupplyChainReportData(ctx context.Context, _ *http.Request, lens *horizonmodels.Lens, edgeTenantID string) (*lensSupplyChainReportData, error) {
	scopePaths := workscope.LensWorkbenchScopePaths(lens)
	workScope, err := workscope.ResolveLensMapWorkScope(ctx, lens, edgeTenantID, scopePaths, h.service.Fragments(), h.service.Edges())
	if err != nil {
		return nil, err
	}
	resolved, err := workscope.NewResolver(h.materializedArtifactsRepo).Resolve(ctx, workScope)
	if err != nil {
		return nil, err
	}
	allMaterialized := workscope.CollectMaterializedArtifacts(ctx, h.materializedArtifactsRepo, resolved, simscope.Profile{})

	const chainLimit = 160
	scopedChains, discoveredChains, err := pathquery.CollectSupplyChainsForLens(ctx, h.materializedArtifactsRepo, lens, chainLimit)
	if err != nil {
		return nil, err
	}
	chains := dedupeSupplyChainArtifacts(append(append([]*horizonmodels.MaterializedArtifact{}, scopedChains...), discoveredChains...))
	for _, a := range chains {
		annotateSupplyChainTraceArtifact(a, "")
	}
	for _, artifact := range allMaterialized {
		if artifact == nil || artifact.Kind != horizonmodels.MaterializedKindSupplyChainPath {
			continue
		}
		chains = dedupeSupplyChainArtifacts(append(chains, artifact))
	}
	annotateSupplyChainArtifactRiskOverlays(chains)
	annotateSupplyChainFamilyArtifacts(chains)

	refs := make([]string, 0, len(resolved.ScopePaths)+64)
	refs = append(refs, resolved.ScopePaths...)
	for _, chain := range chains {
		if chain == nil {
			continue
		}
		horizonmodels.PopulateSupplyChainPathIndexes(chain)
		refs = append(refs, chain.NodeRefs...)
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(chain.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, leg := range parsed.Legs {
			refs = append(refs, leg.TransportViaRefs...)
			refs = append(refs, leg.ZoneRefs...)
			refs = append(refs, leg.AffectedZoneRefs...)
		}
	}
	refs = uniqueTrimmedReportStrings(refs)

	fragmentsByPath := map[string]*horizonmodels.HorizonFragment{}
	fragments, err := h.service.Fragments().ListByCanonicalPaths(ctx, refs)
	if err == nil {
		for _, fragment := range fragments {
			if fragment == nil {
				continue
			}
			cp := strings.TrimSpace(fragment.CanonicalPath)
			if cp == "" {
				continue
			}
			fragmentsByPath[cp] = fragment
		}
	}
	for _, chain := range chains {
		hydrateSupplyChainTraceEndpoints(chain, "", fragmentsByPath)
	}

	return &lensSupplyChainReportData{
		lens:               lens,
		scopePaths:         scopePaths,
		workScope:          workScope,
		resolved:           resolved,
		allMaterialized:    allMaterialized,
		chains:             chains,
		scopedChainCount:   len(scopedChains),
		discoveredChainCnt: len(discoveredChains),
		fragmentsByPath:    fragmentsByPath,
		riskOverlay:        buildSupplyChainLensRiskOverlay(chains),
		createdAt:          time.Now().UTC(),
	}, nil
}

func (h *HorizonHandlers) createLensSupplyChainReportArtifact(ctx context.Context, lens *horizonmodels.Lens, userID string, reportData *lensSupplyChainReportData, req lensSupplyChainReportRequest) (*coremodels.CoreDoc, error) {
	report := supplychainreport.Build(&supplychainreport.Input{
		Lens:               reportData.lens,
		ScopePaths:         reportData.scopePaths,
		WorkScope:          reportData.workScope,
		Resolved:           reportData.resolved,
		AllMaterialized:    reportData.allMaterialized,
		Chains:             reportData.chains,
		ScopedChainCount:   reportData.scopedChainCount,
		DiscoveredChainCnt: reportData.discoveredChainCnt,
		FragmentsByPath:    reportData.fragmentsByPath,
		CreatedAt:          reportData.createdAt,
	})
	polishOptions := supplychainreport.PolishOptions{
		Enabled:        req.LLMEnabled == nil || *req.LLMEnabled,
		Model:          strings.TrimSpace(req.Model),
		ThinkingBudget: req.ThinkingBudget,
	}
	if req.MaxOutputTokens != nil {
		polishOptions.MaxOutputTokens = *req.MaxOutputTokens
	}
	polished := supplychainreport.PolishPackages(ctx, report.Packages, lensSupplyChainSectionGenerator{h: h}, polishOptions)
	generatedSections := supplychainreport.BuildSectionsFromPackages(report.Packages, polished)
	templateID := "supply_chain_consulting_memo_v1"
	template, templateErr := artemplates.GetSystemTemplate(templateID)
	sections := generatedSections
	if templateErr == nil && template != nil {
		sections = buildTemplateBackedSupplyChainSections(template, generatedSections, reportData.createdAt)
	}
	fullDocument := supplychainreport.AssembleMarkdown(sections)
	filename := buildLensSupplyChainReportFilename(lens, reportData.createdAt)
	description := fmt.Sprintf("Supply-chain report for lens %q built on %s", strings.TrimSpace(lens.Name), reportData.createdAt.Format("2006-01-02"))
	docType := "dynamic_artifact"
	parseStatus := "parsed"
	reviewPrompt := "Deterministic substrate assembled from materialized supply-chain paths, then optionally polished section-by-section by LLM without inventing facts."
	userRequest := "Build a supply-chain report artifact from the current lens work scope using deterministic graph substrate and section-by-section LLM polish."
	versionID := "supply-chain-" + reportData.createdAt.Format("20060102T150405Z")
	orchestratorPrompt := "Preserve deterministic facts and section structure. Do not invent additional facts."
	tone := "executive"
	audience := "executive"
	detailLevel := "moderate"
	createdBy := strings.TrimSpace(userID)
	defaultModel := firstNonEmptyReport(strings.TrimSpace(req.Model), supplychainreport.DefaultPolishModel)
	systemPrompt := "Deterministic supply-chain substrate assembly with optional section-by-section LLM polish."
	if !(req.LLMEnabled == nil || *req.LLMEnabled) || len(polished) == 0 {
		defaultModel = "deterministic"
	}
	if template != nil && template.SystemPrompts != nil {
		if prompt := strings.TrimSpace(template.SystemPrompts["en"]); prompt != "" {
			systemPrompt = prompt
		}
	}

	sectionSnapshots := make(map[string]coremodels.ArtifactSectionSnapshot, len(sections))
	sectionModels := make(map[string]string, len(sections))
	totalTokensUsed := 0
	for _, section := range sections {
		content := ""
		if section.Content != nil {
			content = *section.Content
		}
		modelName := "deterministic"
		tokensUsed := 0
		if item, ok := polished[section.ID]; ok {
			modelName = firstNonEmptyReport(item.Model, strings.TrimSpace(req.Model), supplychainreport.DefaultPolishModel)
			tokensUsed = item.TokensUsed
		}
		sectionSnapshots[section.ID] = coremodels.ArtifactSectionSnapshot{
			Content:     content,
			Prompt:      section.Prompt,
			Model:       modelName,
			TokensUsed:  tokensUsed,
			GeneratedAt: reportData.createdAt,
		}
		sectionModels[section.ID] = modelName
		totalTokensUsed += tokensUsed
	}

	artifactConfig := &coremodels.ArtifactConfig{
		Format:            "report",
		Focus:             "executive",
		OrchestratorModel: defaultModel,
		DefaultModel:      defaultModel,
		ReviewPrompt:      reviewPrompt,
		Structure:         sections,
		ContextSources: coremodels.ArtifactContextSources{
			Documents: nil,
			GraphSources: []coremodels.ArtifactGraphContextSource{
				{
					Kind:       "lens_supply_chain_report",
					LensID:     stringPointerIfNonEmpty(lens.ID),
					ReportType: stringPointerIfNonEmpty("consulting_memo"),
					TemplateID: stringPointerIfNonEmpty(templateID),
				},
			},
		},
		AutoUpdateEnabled: false,
		CurrentVersionID:  versionID,
		VersionsRetained:  10,
		Versions: []coremodels.ArtifactVersion{
			{
				ID:           versionID,
				Timestamp:    reportData.createdAt,
				Trigger:      "manual",
				FullDocument: fullDocument,
				Sections:     sectionSnapshots,
				ContextSnapshot: coremodels.ArtifactContextSnapshot{
					Documents:      nil,
					PinnedMessages: nil,
					MessageCount:   0,
					DocumentTitles: nil,
				},
				GenerationMeta: coremodels.ArtifactGenerationMeta{
					TotalTokensUsed:   totalTokensUsed,
					DurationMs:        0,
					SectionsGenerated: len(sections),
				},
				GenerationMode: "interactive",
				GenerationParams: coremodels.GenerationParams{
					PreferredLanguage: "en",
					SystemPrompt:      systemPrompt,
					OrchestratorModel: defaultModel,
					SectionModels:     sectionModels,
					Tone:              &tone,
					Audience:          &audience,
					DetailLevel:       &detailLevel,
				},
				StructureSnapshot: append([]coremodels.ArtifactSection(nil), sections...),
			},
		},
		GenerationSettings: coremodels.GenerationSettings{
			PreferredLanguage:        "en",
			SystemPrompt:             systemPrompt,
			OrchestratorSystemPrompt: &orchestratorPrompt,
			Tone:                     &tone,
			Audience:                 &audience,
			DetailLevel:              &detailLevel,
			MaxOutputTokens:          req.MaxOutputTokens,
			ThinkingBudget:           req.ThinkingBudget,
		},
		UserRequest: &userRequest,
		StructureGeneration: &coremodels.StructureGenerationConfig{
			Mode:           "template",
			SourceTemplate: stringPointerIfNonEmpty(templateID),
			GeneratedAt:    &reportData.createdAt,
			UserModified:   false,
		},
	}

	doc := &coremodels.CoreDoc{
		TenantID:      lens.TenantID,
		ProjectID:     lens.ProjectID,
		Filename:      filename,
		ContentType:   "text/markdown",
		Description:   &description,
		CreatedBy:     stringPointerIfNonEmpty(createdBy),
		ExtractedText: &fullDocument,
		ParseStatus:   &parseStatus,
		ParseMeta: map[string]any{
			"artifactType":            "lens_supply_chain_report",
			"reportMode":              reportModeForPolish(polished),
			"lensId":                  lens.ID,
			"workscopeDigest":         workscope.Digest(reportData.resolved),
			"scopePathsN":             len(reportData.resolved.ScopePaths),
			"fragmentIdsN":            len(reportData.resolved.FragmentIDs),
			"materializedArtifactsN":  len(reportData.allMaterialized),
			"supplyChainPathsN":       len(reportData.chains),
			"scopedSupplyChainPathsN": reportData.scopedChainCount,
			"discoveredSupplyPathsN":  reportData.discoveredChainCnt,
			"llmSectionsN":            len(polished),
			"llmModel":                strings.TrimSpace(req.Model),
			"sourceTemplate":          templateID,
		},
		DocType:        &docType,
		ArtifactConfig: artifactConfig,
	}

	id, err := h.coreDocsRepo.Create(ctx, doc)
	if err != nil {
		return nil, err
	}
	doc.ID = id
	return doc, nil
}

func dedupeSupplyChainArtifacts(artifacts []*horizonmodels.MaterializedArtifact) []*horizonmodels.MaterializedArtifact {
	if len(artifacts) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(artifacts))
	out := make([]*horizonmodels.MaterializedArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		key := strings.TrimSpace(artifact.ID)
		if key == "" {
			key = strings.TrimSpace(artifact.CanonicalKey)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		horizonmodels.PopulateSupplyChainPathIndexes(artifact)
		out = append(out, artifact)
	}
	sortSupplyChainArtifacts(out)
	return out
}

func buildLensSupplyChainReportFilename(lens *horizonmodels.Lens, createdAt time.Time) string {
	slug := slugifyFilenamePart(safeLensName(lens))
	if slug == "" {
		slug = "lens"
	}
	return fmt.Sprintf("%s-supply-chain-report-%s.md", slug, createdAt.Format("2006-01-02"))
}

func buildTemplateBackedSupplyChainSections(template *coremodels.ArtifactTemplate, generated []coremodels.ArtifactSection, generatedAt time.Time) []coremodels.ArtifactSection {
	if template == nil || len(template.DefaultStructure) == 0 {
		return generated
	}
	statusCompleted := "completed"
	byID := make(map[string]coremodels.ArtifactSection, len(generated))
	for _, section := range generated {
		byID[section.ID] = section
	}
	out := make([]coremodels.ArtifactSection, 0, len(template.DefaultStructure))
	for _, item := range template.DefaultStructure {
		generatedSection, ok := byID[item.ID]
		content := ""
		if ok && generatedSection.Content != nil {
			content = *generatedSection.Content
		}
		prompt := strings.TrimSpace(item.PromptTemplate)
		if prompt == "" && ok {
			prompt = generatedSection.Prompt
		}
		description := strings.TrimSpace(item.Description)
		if description == "" && ok && generatedSection.Description != nil {
			description = strings.TrimSpace(*generatedSection.Description)
		}
		contentCopy := content
		descriptionCopy := description
		section := coremodels.ArtifactSection{
			ID:                item.ID,
			Title:             item.Title,
			Order:             item.Order,
			Prompt:            prompt,
			Content:           &contentCopy,
			GeneratedAt:       &generatedAt,
			Description:       &descriptionCopy,
			Required:          item.Required,
			Editable:          item.Editable,
			AutoUpdateEnabled: false,
			DependsOn:         append([]string(nil), item.DependsOn...),
			GenerationStatus:  &statusCompleted,
		}
		if ok {
			section.Model = generatedSection.Model
			section.TokensUsed = generatedSection.TokensUsed
		}
		out = append(out, section)
	}
	return out
}

func (h *HorizonHandlers) createProjectShareLink(ctx context.Context, r *http.Request, projectID string, userID string) (string, string, time.Time, error) {
	token, err := generateSecureShareToken(36)
	if err != nil {
		return "", "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(lensSupplyChainReportShareTTL)
	shareRecord := &corerepo.ShareToken{
		ProjectID: projectID,
		Token:     token,
		ExpiresAt: expiresAt,
		CreatedBy: strings.TrimSpace(userID),
	}
	if _, err := h.shareTokensRepo.Create(ctx, shareRecord); err != nil {
		return "", "", time.Time{}, err
	}
	baseURL := externalBaseURLForRequest(r)
	return strings.TrimRight(baseURL, "/") + "/shared/" + token, token, expiresAt, nil
}

func generateSecureShareToken(size int) (string, error) {
	if size <= 0 {
		size = 36
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func externalBaseURLForRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if base := strings.TrimSpace(r.Header.Get("X-External-Base-Url")); base != "" {
		return strings.TrimRight(base, "/")
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return strings.TrimRight(origin, "/")
	}
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	return strings.TrimRight(proto+"://"+host, "/")
}

func defaultLensSupplyChainReportRequest() lensSupplyChainReportRequest {
	maxOutputTokens := supplychainreport.DefaultPolishMaxOutputTokens
	thinkingBudget := supplychainreport.DefaultPolishThinkingBudget
	enabled := true
	return lensSupplyChainReportRequest{
		LLMEnabled:      &enabled,
		Model:           supplychainreport.DefaultPolishModel,
		MaxOutputTokens: &maxOutputTokens,
		ThinkingBudget:  &thinkingBudget,
	}
}

func reportModeForPolish(polished map[string]supplychainreport.PolishResult) string {
	if len(polished) == 0 {
		return "deterministic"
	}
	return "deterministic_plus_llm"
}

func genAIGenerateRequestFromSupplyChain(req supplychainreport.GenerateRequest) gen_combined.GenAIRequest {
	return gen_combined.GenAIRequest{
		Model:             strings.TrimSpace(req.Model),
		Prompt:            req.Prompt,
		SystemInstruction: req.SystemInstruction,
		Temperature:       0.2,
		MaxOutputTokens:   req.MaxOutputTokens,
		ThinkingBudget:    req.ThinkingBudget,
	}
}

func uniqueTrimmedReportStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func safeLensName(lens *horizonmodels.Lens) string {
	if lens == nil {
		return "Lens"
	}
	return firstNonEmptyReport(strings.TrimSpace(lens.Name), strings.TrimSpace(lens.ID), "Lens")
}

func firstNonEmptyReport(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func slugifyFilenamePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ':
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func stringPointerIfNonEmpty(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
