package horizon

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	artemplates "github.com/igorkirisyuk/sanskrite/internal/artifacts/templates"
	gen_combined "github.com/igorkirisyuk/sanskrite/internal/genai/combined"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/naming"
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

type reportRefCount struct {
	Ref   string
	Label string
	Role  string
	Count int
}

type reportGapCount struct {
	Label string
	Count int
}

type reportFamilySummary struct {
	Key            string
	ProductAxis    string
	SupplierRef    string
	SupplierLabel  string
	CustomerRef    string
	CustomerLabel  string
	PathCount      int
	ValidatedCount int
	HeuristicCount int
	GapCount       int
	OpenGapSamples []string
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

func (h *HorizonHandlers) buildLensSupplyChainReportData(ctx context.Context, r *http.Request, lens *horizonmodels.Lens, edgeTenantID string) (*lensSupplyChainReportData, error) {
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
	scopedChains, discoveredChains, err := h.collectSupplyChainsForLens(r, lens, chainLimit)
	if err != nil {
		return nil, err
	}
	chains := dedupeSupplyChainArtifacts(append(append([]*horizonmodels.MaterializedArtifact{}, scopedChains...), discoveredChains...))
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

func buildLensSupplyChainReportSections(reportData *lensSupplyChainReportData) []coremodels.ArtifactSection {
	now := reportData.createdAt
	statusCompleted := "completed"

	makeSection := func(id string, order int, title string, description string, prompt string, content string) coremodels.ArtifactSection {
		contentCopy := content
		descriptionCopy := description
		return coremodels.ArtifactSection{
			ID:                id,
			Title:             title,
			Order:             order,
			Prompt:            prompt,
			Content:           &contentCopy,
			GeneratedAt:       &now,
			Description:       &descriptionCopy,
			Required:          true,
			Editable:          true,
			AutoUpdateEnabled: false,
			GenerationStatus:  &statusCompleted,
		}
	}

	return []coremodels.ArtifactSection{
		makeSection(
			"executive_snapshot",
			1,
			"Executive Snapshot",
			"Answer-first summary of the current supply-chain footprint and dominant vulnerabilities.",
			"Summarize the current supply-chain exposure with answer-first findings.",
			buildLensSupplyChainExecutiveSection(reportData),
		),
		makeSection(
			"workscope_coverage",
			2,
			"Workscope And Coverage",
			"Describe how much of the lens work scope is covered by map-expanded objects and materialized artifacts.",
			"Summarize lens workscope coverage and materialized substrate.",
			buildLensSupplyChainCoverageSection(reportData),
		),
		makeSection(
			"network_architecture",
			3,
			"Network Architecture",
			"Surface the dominant hubs, corridors, and family clusters inside the current chain set.",
			"Describe network architecture, concentration, and family clusters.",
			buildLensSupplyChainArchitectureSection(reportData),
		),
		makeSection(
			"critical_paths",
			4,
			"Critical Paths",
			"Rank the structurally most critical chains and bottleneck legs.",
			"Rank the most stressed paths and explain their bottlenecks.",
			buildLensSupplyChainCriticalPathsSection(reportData),
		),
		makeSection(
			"bottlenecks_gaps",
			5,
			"Bottlenecks And Gaps",
			"List the main bottlenecks, coverage gaps, and sufficiency warnings.",
			"Summarize bottlenecks and gaps from sufficiency and stress overlays.",
			buildLensSupplyChainBottlenecksSection(reportData),
		),
		makeSection(
			"appendix",
			6,
			"Appendix",
			"Expose the deterministic scope and artifact basis behind the report.",
			"List scope anchors, referenced artifacts, and deterministic metadata.",
			buildLensSupplyChainAppendixSection(reportData),
		),
	}
}

func buildLensSupplyChainExecutiveSection(reportData *lensSupplyChainReportData) string {
	chains := reportData.chains
	riskOverlay := reportData.riskOverlay
	validated, heuristic, withGaps := summarizePathStatuses(chains)
	topHubs := collectTopHubRefs(chains, reportData.fragmentsByPath, 4)
	topFamilies := collectFamilySummaries(chains, reportData.fragmentsByPath, 3)
	topPath := firstPathRef(riskOverlay)

	lines := []string{
		fmt.Sprintf("Lens `%s` currently resolves to **%d supply-chain paths** across **%d family clusters**.", safeLensName(reportData.lens), len(chains), countSupplyChainFamilies(chains)),
		"",
		"| Metric | Value |",
		"| --- | --- |",
		fmt.Sprintf("| Scoped paths | %d |", reportData.scopedChainCount),
		fmt.Sprintf("| Discovered paths | %d |", reportData.discoveredChainCnt),
		fmt.Sprintf("| Validated paths | %d |", validated),
		fmt.Sprintf("| Heuristic paths | %d |", heuristic),
		fmt.Sprintf("| Paths with open gaps | %d |", withGaps),
		fmt.Sprintf("| Map-expanded scope objects | %d |", len(reportData.resolved.ScopePaths)),
	}
	if riskOverlay != nil {
		lines = append(lines, fmt.Sprintf("| Lens criticality | %.2f |", riskOverlay.Criticality))
		lines = append(lines, fmt.Sprintf("| Stressed paths | %d |", riskOverlay.StressedPaths))
	}
	lines = append(lines, "")
	lines = append(lines, "### Key takeaways", "")

	if len(topHubs) > 0 {
		lines = append(lines, fmt.Sprintf("- Hub concentration is led by %s.", joinTopRefCounts(topHubs, 3)))
	} else {
		lines = append(lines, "- Hub concentration is not yet explicit in the current route bindings; route overlays remain sparse.")
	}
	if topPath != nil {
		lines = append(lines, fmt.Sprintf("- The most critical path at read time is `%s`, with criticality %.2f and stress %.2f.", strings.TrimSpace(topPath.DisplayName), topPath.Criticality, topPath.StressScore))
	}
	if len(topFamilies) > 0 {
		lines = append(lines, fmt.Sprintf("- Family concentration is anchored on `%s`.", topFamilies[0].Key))
	}
	if riskOverlay != nil && len(riskOverlay.TopMechanisms) > 0 {
		lines = append(lines, fmt.Sprintf("- Dominant risk mechanism in the current lens is `%s`.", strings.TrimSpace(riskOverlay.TopMechanisms[0].Label)))
	}
	if withGaps > 0 {
		lines = append(lines, fmt.Sprintf("- Coverage is not fully closed: **%d paths** still carry unresolved joins or open gaps.", withGaps))
	} else {
		lines = append(lines, "- The current path set is structurally coherent at read time: no material open-gap flags are present.")
	}

	return strings.Join(lines, "\n")
}

func buildLensSupplyChainCoverageSection(reportData *lensSupplyChainReportData) string {
	artifactCounts := map[string]int{}
	for _, artifact := range reportData.allMaterialized {
		if artifact == nil {
			continue
		}
		artifactCounts[string(artifact.Kind)]++
	}

	lines := []string{
		"| Coverage element | Count |",
		"| --- | --- |",
		fmt.Sprintf("| Original workbench scope paths | %d |", len(reportData.scopePaths)),
		fmt.Sprintf("| Map-expanded scope paths | %d |", len(reportData.resolved.ScopePaths)),
		fmt.Sprintf("| Neighbor fragment IDs | %d |", len(reportData.resolved.FragmentIDs)),
		fmt.Sprintf("| Materialized artifacts in resolved work scope | %d |", len(reportData.allMaterialized)),
		fmt.Sprintf("| Supply-chain paths selected for report | %d |", len(reportData.chains)),
		fmt.Sprintf("| Resolved fragment labels | %d |", len(reportData.fragmentsByPath)),
		"",
		"### Materialized substrate",
		"",
	}

	if len(artifactCounts) == 0 {
		lines = append(lines, "- No materialized artifacts were returned for the current work scope.")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "| Kind | Count |", "| --- | --- |")
	for _, kind := range sortedArtifactKinds(artifactCounts) {
		lines = append(lines, fmt.Sprintf("| `%s` | %d |", kind, artifactCounts[kind]))
	}
	lines = append(lines, "")
	lines = append(lines, "### Scope notes", "")
	lines = append(lines, fmt.Sprintf("- Work scope digest: `%s`.", workscope.Digest(reportData.resolved)))
	lines = append(lines, "- Scope expansion uses the same map-oriented wider-object resolver as the lens workbench, including one-hop graph neighbors.")
	if len(reportData.resolved.ScopePaths) > len(reportData.scopePaths) {
		lines = append(lines, fmt.Sprintf("- Map expansion added **%d** extra canonical paths beyond the original lens scope.", len(reportData.resolved.ScopePaths)-len(reportData.scopePaths)))
	}

	return strings.Join(lines, "\n")
}

func buildLensSupplyChainArchitectureSection(reportData *lensSupplyChainReportData) string {
	topFamilies := collectFamilySummaries(reportData.chains, reportData.fragmentsByPath, 8)
	topHubs := collectTopHubRefs(reportData.chains, reportData.fragmentsByPath, 8)
	topFlows := collectTopFlowRefs(reportData.chains, 8)
	topNodes := collectTopNodeRefs(reportData.riskOverlay, 8)

	lines := []string{"### Path families", ""}
	if len(topFamilies) == 0 {
		lines = append(lines, "- No explicit path family metadata is present in the current chain set.")
	} else {
		lines = append(lines, "| Family | Product axis | Supplier anchor | Customer anchor | Paths | Validated | Gap-flagged |", "| --- | --- | --- | --- | --- | --- | --- |")
		for _, family := range topFamilies {
			lines = append(lines, fmt.Sprintf("| `%s` | %s | %s | %s | %d | %d | %d |",
				family.Key,
				markdownEscape(firstNonEmptyReport(family.ProductAxis, "n/a")),
				markdownEscape(firstNonEmptyReport(family.SupplierLabel, family.SupplierRef, "n/a")),
				markdownEscape(firstNonEmptyReport(family.CustomerLabel, family.CustomerRef, "n/a")),
				family.PathCount,
				family.ValidatedCount,
				family.GapCount,
			))
		}
	}

	lines = append(lines, "", "### Hub concentration", "")
	if len(topHubs) == 0 {
		lines = append(lines, "- No transport-via hubs are currently encoded in a reusable way.")
	} else {
		lines = append(lines, "| Hub | Paths touching hub |", "| --- | --- |")
		for _, hub := range topHubs {
			lines = append(lines, fmt.Sprintf("| %s | %d |", markdownEscape(firstNonEmptyReport(hub.Label, hub.Ref)), hub.Count))
		}
	}

	lines = append(lines, "", "### Corridor and node concentration", "")
	if len(topFlows) > 0 {
		lines = append(lines, "| Flow / corridor | Paths |", "| --- | --- |")
		for _, flow := range topFlows {
			lines = append(lines, fmt.Sprintf("| %s | %d |", markdownEscape(firstNonEmptyReport(flow.Label, flow.Ref)), flow.Count))
		}
		lines = append(lines, "")
	}
	if len(topNodes) > 0 {
		lines = append(lines, "| Node | Role | Paths |", "| --- | --- | --- |")
		for _, node := range topNodes {
			lines = append(lines, fmt.Sprintf("| %s | %s | %d |", markdownEscape(firstNonEmptyReport(node.Label, node.Ref)), markdownEscape(firstNonEmptyReport(node.Role, "node")), node.Count))
		}
	}

	return strings.Join(lines, "\n")
}

func buildLensSupplyChainCriticalPathsSection(reportData *lensSupplyChainReportData) string {
	lines := []string{}
	riskOverlay := reportData.riskOverlay
	if riskOverlay == nil || len(riskOverlay.TopPaths) == 0 {
		lines = append(lines, "- No ranked critical paths are available for the current lens.")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "| Path | Supplier | Customer | Product | Criticality | Stress | Mechanism | Route summary |", "| --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, path := range riskOverlay.TopPaths[:minReportInt(len(riskOverlay.TopPaths), 8)] {
		lines = append(lines, fmt.Sprintf("| %s | %s | %s | %s | %.2f | %.2f | %s | %s |",
			markdownEscape(firstNonEmptyReport(path.DisplayName, path.CanonicalKey, path.ArtifactID)),
			markdownEscape(firstNonEmptyReport(path.SupplierLabel, path.SupplierRef, "n/a")),
			markdownEscape(firstNonEmptyReport(path.CustomerLabel, path.CustomerRef, "n/a")),
			markdownEscape(firstNonEmptyReport(path.ProductLabel, path.ProductRef, "n/a")),
			path.Criticality,
			path.StressScore,
			markdownEscape(firstNonEmptyReport(path.PrimaryMechanism, "n/a")),
			markdownEscape(firstNonEmptyReport(path.RouteSummary, "n/a")),
		))
	}

	if len(riskOverlay.DriverMetrics) > 0 {
		lines = append(lines, "", "### Driver metrics", "", "| Metric | Value | Scope |", "| --- | --- | --- |")
		for _, metric := range riskOverlay.DriverMetrics[:minReportInt(len(riskOverlay.DriverMetrics), 6)] {
			lines = append(lines, fmt.Sprintf("| %s | %s | %s |",
				markdownEscape(firstNonEmptyReport(metric.Label, metric.Key)),
				markdownEscape(formatOverlayMetric(metric)),
				markdownEscape(firstNonEmptyReport(metric.Scope, "lens")),
			))
		}
	}

	return strings.Join(lines, "\n")
}

func buildLensSupplyChainBottlenecksSection(reportData *lensSupplyChainReportData) string {
	topBottlenecks := collectTopBottlenecks(reportData.riskOverlay, 8)
	topGaps := collectTopGapCounts(reportData.chains, 8)
	validated, heuristic, withGaps := summarizePathStatuses(reportData.chains)

	lines := []string{
		fmt.Sprintf("- Path-status split at read time: **%d validated**, **%d heuristic**, **%d with explicit gaps**.", validated, heuristic, withGaps),
		"",
		"### Bottlenecks",
		"",
	}
	if len(topBottlenecks) == 0 {
		lines = append(lines, "- No bottleneck nodes were derived from the current path risk overlays.")
	} else {
		lines = append(lines, "| Bottleneck node | Role | Paths |", "| --- | --- | --- |")
		for _, bottleneck := range topBottlenecks {
			lines = append(lines, fmt.Sprintf("| %s | %s | %d |",
				markdownEscape(firstNonEmptyReport(bottleneck.Label, bottleneck.Ref)),
				markdownEscape(firstNonEmptyReport(bottleneck.Role, "node")),
				bottleneck.Count,
			))
		}
	}

	lines = append(lines, "", "### Open gaps and warnings", "")
	if len(topGaps) == 0 {
		lines = append(lines, "- No explicit open-gap strings were materialized in the selected path set.")
	} else {
		lines = append(lines, "| Gap / warning | Paths |", "| --- | --- |")
		for _, gap := range topGaps {
			lines = append(lines, fmt.Sprintf("| %s | %d |", markdownEscape(gap.Label), gap.Count))
		}
	}

	if reportData.riskOverlay != nil && len(reportData.riskOverlay.TopMechanisms) > 0 {
		lines = append(lines, "", "### Dominant mechanisms", "")
		for _, mechanism := range reportData.riskOverlay.TopMechanisms[:minReportInt(len(reportData.riskOverlay.TopMechanisms), 5)] {
			label := firstNonEmptyReport(mechanism.Label, mechanism.Key)
			lines = append(lines, fmt.Sprintf("- `%s` appears across %d evidence points with aggregate score %.2f.", label, mechanism.EvidenceCount, mechanism.Score))
		}
	}

	return strings.Join(lines, "\n")
}

func buildLensSupplyChainAppendixSection(reportData *lensSupplyChainReportData) string {
	lines := []string{
		"| Deterministic basis | Value |",
		"| --- | --- |",
		fmt.Sprintf("| Lens ID | `%s` |", reportData.lens.ID),
		fmt.Sprintf("| Project ID | `%s` |", reportData.lens.ProjectID),
		fmt.Sprintf("| Generated at (UTC) | `%s` |", reportData.createdAt.Format(time.RFC3339)),
		fmt.Sprintf("| Work scope digest | `%s` |", workscope.Digest(reportData.resolved)),
		fmt.Sprintf("| Scope paths | %d |", len(reportData.resolved.ScopePaths)),
		fmt.Sprintf("| Materialized artifact IDs | %d |", len(reportData.resolved.MaterializedArtifactIDs)),
		"",
		"### Scope paths",
		"",
	}

	if len(reportData.resolved.ScopePaths) == 0 {
		lines = append(lines, "- No canonical scope paths were resolved.")
	} else {
		for _, path := range reportData.resolved.ScopePaths[:minReportInt(len(reportData.resolved.ScopePaths), 40)] {
			lines = append(lines, fmt.Sprintf("- `%s`", path))
		}
		if len(reportData.resolved.ScopePaths) > 40 {
			lines = append(lines, fmt.Sprintf("- ... and %d more", len(reportData.resolved.ScopePaths)-40))
		}
	}

	lines = append(lines, "", "### Materialized artifact IDs", "")
	if len(reportData.resolved.MaterializedArtifactIDs) == 0 {
		lines = append(lines, "- No materialized artifact IDs were resolved.")
	} else {
		for _, id := range reportData.resolved.MaterializedArtifactIDs[:minReportInt(len(reportData.resolved.MaterializedArtifactIDs), 30)] {
			lines = append(lines, fmt.Sprintf("- `%s`", id))
		}
		if len(reportData.resolved.MaterializedArtifactIDs) > 30 {
			lines = append(lines, fmt.Sprintf("- ... and %d more", len(reportData.resolved.MaterializedArtifactIDs)-30))
		}
	}

	return strings.Join(lines, "\n")
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

func assembleLensSupplyChainArtifactMarkdown(sections []coremodels.ArtifactSection) string {
	if len(sections) == 0 {
		return ""
	}
	copied := append([]coremodels.ArtifactSection(nil), sections...)
	sort.SliceStable(copied, func(i, j int) bool {
		return copied[i].Order < copied[j].Order
	})
	var builder strings.Builder
	for _, section := range copied {
		builder.WriteString("## ")
		builder.WriteString(section.Title)
		builder.WriteString("\n\n")
		if section.Content != nil && strings.TrimSpace(*section.Content) != "" {
			builder.WriteString(strings.TrimSpace(*section.Content))
			builder.WriteString("\n\n")
		} else {
			builder.WriteString("*[No content]*\n\n")
		}
	}
	return strings.TrimSpace(builder.String())
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

func collectFamilySummaries(chains []*horizonmodels.MaterializedArtifact, fragments map[string]*horizonmodels.HorizonFragment, limit int) []reportFamilySummary {
	byKey := map[string]*reportFamilySummary{}
	for _, artifact := range chains {
		if artifact == nil {
			continue
		}
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		key := strings.TrimSpace(artifact.FamilyID)
		if key == "" && parsed.Family != nil {
			key = strings.TrimSpace(parsed.Family.ID)
		}
		if key == "" {
			key = strings.TrimSpace(firstNonEmptyReport(artifact.ProductAxis, "unclassified"))
		}
		entry := byKey[key]
		if entry == nil {
			entry = &reportFamilySummary{Key: key}
			byKey[key] = entry
		}
		entry.PathCount++
		if pathIsValidated(artifact) {
			entry.ValidatedCount++
		} else {
			entry.HeuristicCount++
		}
		if pathHasGap(artifact) {
			entry.GapCount++
		}
		entry.ProductAxis = firstNonEmptyReport(entry.ProductAxis, strings.TrimSpace(artifact.ProductAxis))
		if parsed.Family != nil {
			entry.ProductAxis = firstNonEmptyReport(entry.ProductAxis, strings.TrimSpace(parsed.Family.ProductAxis))
			entry.OpenGapSamples = mergeUniqueStrings(entry.OpenGapSamples, parsed.Family.OpenGaps)
			if parsed.Family.PathCount > entry.PathCount {
				entry.PathCount = parsed.Family.PathCount
			}
			if parsed.Family.ValidatedPathCount > entry.ValidatedCount {
				entry.ValidatedCount = parsed.Family.ValidatedPathCount
			}
			if parsed.Family.GapPathCount > entry.GapCount {
				entry.GapCount = parsed.Family.GapPathCount
			}
		}
		entry.SupplierRef = firstNonEmptyReport(entry.SupplierRef, strings.TrimSpace(artifact.AnchorSupplier))
		entry.CustomerRef = firstNonEmptyReport(entry.CustomerRef, strings.TrimSpace(artifact.AnchorCustomer))
		if entry.SupplierRef == "" && len(parsed.Legs) > 0 {
			entry.SupplierRef = strings.TrimSpace(parsed.Legs[0].SupplierRef)
		}
		if entry.CustomerRef == "" && len(parsed.Legs) > 0 {
			entry.CustomerRef = strings.TrimSpace(parsed.Legs[len(parsed.Legs)-1].CustomerRef)
		}
		entry.SupplierLabel = labelForCanonicalPath(entry.SupplierRef, fragments)
		entry.CustomerLabel = labelForCanonicalPath(entry.CustomerRef, fragments)
	}

	out := make([]reportFamilySummary, 0, len(byKey))
	for _, entry := range byKey {
		out = append(out, *entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PathCount == out[j].PathCount {
			return out[i].ValidatedCount > out[j].ValidatedCount
		}
		return out[i].PathCount > out[j].PathCount
	})
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func collectTopHubRefs(chains []*horizonmodels.MaterializedArtifact, fragments map[string]*horizonmodels.HorizonFragment, limit int) []reportRefCount {
	counts := map[string]int{}
	for _, artifact := range chains {
		if artifact == nil {
			continue
		}
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		perPath := map[string]struct{}{}
		for _, leg := range parsed.Legs {
			for _, ref := range leg.TransportViaRefs {
				ref = strings.TrimSpace(ref)
				if ref == "" {
					continue
				}
				perPath[ref] = struct{}{}
			}
		}
		for ref := range perPath {
			counts[ref]++
		}
	}
	return sortReportRefCounts(counts, fragments, limit)
}

func collectTopFlowRefs(chains []*horizonmodels.MaterializedArtifact, limit int) []reportRefCount {
	counts := map[string]int{}
	for _, artifact := range chains {
		if artifact == nil {
			continue
		}
		perPath := map[string]struct{}{}
		for _, ref := range artifact.FlowRefs {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			perPath[ref] = struct{}{}
		}
		for ref := range perPath {
			counts[ref]++
		}
	}
	return sortReportRefCounts(counts, nil, limit)
}

func collectTopNodeRefs(riskOverlay *horizonmodels.SupplyChainLensRiskOverlay, limit int) []reportRefCount {
	if riskOverlay == nil || len(riskOverlay.TopNodes) == 0 {
		return nil
	}
	out := make([]reportRefCount, 0, minReportInt(len(riskOverlay.TopNodes), limit))
	for _, node := range riskOverlay.TopNodes[:minReportInt(len(riskOverlay.TopNodes), limit)] {
		out = append(out, reportRefCount{
			Ref:   strings.TrimSpace(node.Ref),
			Label: firstNonEmptyReport(strings.TrimSpace(node.Label), strings.TrimSpace(node.Ref)),
			Role:  strings.TrimSpace(node.Role),
			Count: node.PathCount,
		})
	}
	return out
}

func collectTopBottlenecks(riskOverlay *horizonmodels.SupplyChainLensRiskOverlay, limit int) []reportRefCount {
	if riskOverlay == nil || len(riskOverlay.TopBottlenecks) == 0 {
		return nil
	}
	out := make([]reportRefCount, 0, minReportInt(len(riskOverlay.TopBottlenecks), limit))
	for _, node := range riskOverlay.TopBottlenecks[:minReportInt(len(riskOverlay.TopBottlenecks), limit)] {
		out = append(out, reportRefCount{
			Ref:   strings.TrimSpace(node.Ref),
			Label: firstNonEmptyReport(strings.TrimSpace(node.Label), strings.TrimSpace(node.Role), strings.TrimSpace(node.Ref)),
			Role:  strings.TrimSpace(node.Role),
			Count: node.PathCount,
		})
	}
	return out
}

func collectTopGapCounts(chains []*horizonmodels.MaterializedArtifact, limit int) []reportGapCount {
	counts := map[string]int{}
	for _, artifact := range chains {
		if artifact == nil {
			continue
		}
		parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil || parsed.Sufficiency == nil {
			continue
		}
		gaps := append([]string{}, parsed.Sufficiency.HardFailures...)
		gaps = append(gaps, parsed.Sufficiency.OpenGaps...)
		gaps = append(gaps, parsed.Sufficiency.Warnings...)
		perPath := map[string]struct{}{}
		for _, gap := range gaps {
			gap = strings.TrimSpace(gap)
			if gap == "" {
				continue
			}
			perPath[gap] = struct{}{}
		}
		for gap := range perPath {
			counts[gap]++
		}
	}
	out := make([]reportGapCount, 0, len(counts))
	for label, count := range counts {
		out = append(out, reportGapCount{Label: label, Count: count})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Label < out[j].Label
		}
		return out[i].Count > out[j].Count
	})
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func sortReportRefCounts(counts map[string]int, fragments map[string]*horizonmodels.HorizonFragment, limit int) []reportRefCount {
	out := make([]reportRefCount, 0, len(counts))
	for ref, count := range counts {
		out = append(out, reportRefCount{
			Ref:   ref,
			Label: labelForCanonicalPath(ref, fragments),
			Count: count,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return firstNonEmptyReport(out[i].Label, out[i].Ref) < firstNonEmptyReport(out[j].Label, out[j].Ref)
		}
		return out[i].Count > out[j].Count
	})
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

func summarizePathStatuses(chains []*horizonmodels.MaterializedArtifact) (validated int, heuristic int, withGaps int) {
	for _, artifact := range chains {
		if artifact == nil {
			continue
		}
		if pathHasGap(artifact) {
			withGaps++
		}
		if pathIsValidated(artifact) {
			validated++
		} else {
			heuristic++
		}
	}
	return validated, heuristic, withGaps
}

func pathIsValidated(artifact *horizonmodels.MaterializedArtifact) bool {
	if artifact == nil {
		return false
	}
	return strings.TrimSpace(artifact.PathStatus) == "validated" ||
		strings.TrimSpace(artifact.SufficiencyStatus) == horizonmodels.SupplyChainSufficiencyValidated ||
		artifact.IsConfirmedPath
}

func pathHasGap(artifact *horizonmodels.MaterializedArtifact) bool {
	if artifact == nil {
		return false
	}
	if artifact.GapCount > 0 || strings.TrimSpace(artifact.SufficiencyStatus) == horizonmodels.SupplyChainSufficiencyForensicGap {
		return true
	}
	parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload)
	if err != nil || parsed == nil || parsed.Sufficiency == nil {
		return false
	}
	return len(parsed.Sufficiency.OpenGaps) > 0 || len(parsed.Sufficiency.HardFailures) > 0
}

func firstPathRef(riskOverlay *horizonmodels.SupplyChainLensRiskOverlay) *horizonmodels.SupplyChainOverlayPathRef {
	if riskOverlay == nil || len(riskOverlay.TopPaths) == 0 {
		return nil
	}
	return &riskOverlay.TopPaths[0]
}

func joinTopRefCounts(items []reportRefCount, limit int) string {
	if len(items) == 0 || limit <= 0 {
		return ""
	}
	parts := make([]string, 0, minReportInt(len(items), limit))
	for _, item := range items[:minReportInt(len(items), limit)] {
		parts = append(parts, fmt.Sprintf("`%s` (%d paths)", firstNonEmptyReport(item.Label, item.Ref), item.Count))
	}
	return strings.Join(parts, ", ")
}

func sortedArtifactKinds(counts map[string]int) []string {
	out := make([]string, 0, len(counts))
	for kind := range counts {
		out = append(out, kind)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if counts[out[i]] == counts[out[j]] {
			return out[i] < out[j]
		}
		return counts[out[i]] > counts[out[j]]
	})
	return out
}

func formatOverlayMetric(metric horizonmodels.SupplyChainOverlayMetric) string {
	if strings.TrimSpace(metric.Unit) != "" {
		return fmt.Sprintf("%.2f %s", metric.Value, strings.TrimSpace(metric.Unit))
	}
	return fmt.Sprintf("%.2f", metric.Value)
}

func labelForCanonicalPath(ref string, fragments map[string]*horizonmodels.HorizonFragment) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if fragment := fragments[ref]; fragment != nil {
		return fragmentTraceFragmentLabel(fragment)
	}
	return naming.BestEffortLabelFromCanonicalPath(ref)
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

func markdownEscape(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("|", "\\|", "\n", " ")
	return replacer.Replace(value)
}

func minReportInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
