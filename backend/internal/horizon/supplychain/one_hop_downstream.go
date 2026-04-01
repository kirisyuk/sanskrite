package supplychain

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/pathbuilder"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/productdict"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

const (
	JobUseCaseOneHopDownstream = "supply_chain_one_hop_downstream"

	defaultMaxBranchesPerProduct = 12
	maxPromptArtifacts           = 6
	maxPromptFacilities          = 12
	maxPromptProducts            = 24
	maxPromptActors              = 24
)

type OneHopDownstreamOptions struct {
	AnchorRef             string `json:"anchorRef,omitempty"`
	AnchorType            string `json:"anchorType,omitempty"`
	MaxBranchesPerProduct int    `json:"maxBranchesPerProduct,omitempty"`
	IncludeInputsContext  bool   `json:"includeInputsContext,omitempty"`
	AllowOrgFallback      bool   `json:"allowOrgFallback,omitempty"`
	AllowLLMFallback      bool   `json:"allowLLMFallback,omitempty"`
}

type OneHopDownstreamServiceConfig struct {
	Exec             *ai.AITaskExecutor
	FragmentsRepo    *horizonrepo.HorizonFragmentsRepository
	EdgesRepo        *horizonrepo.DependencyEdgesRepository
	MaterializedRepo *horizonrepo.MaterializedArtifactsRepository
	LensesRepo       *horizonrepo.LensesRepository
	ScoutingJobsRepo *horizonrepo.ScoutingJobsRepository
	ProductDict      *productdict.Runtime
	PathResolver     *horizonscout.CanonicalPathResolver
	Logger           *log.Logger
	Publisher        func(ctx context.Context, eventType string, tenantID string, payload interface{}) error
}

type OneHopDownstreamService struct {
	exec             *ai.AITaskExecutor
	fragmentsRepo    *horizonrepo.HorizonFragmentsRepository
	edgesRepo        *horizonrepo.DependencyEdgesRepository
	materializedRepo *horizonrepo.MaterializedArtifactsRepository
	lensesRepo       *horizonrepo.LensesRepository
	scoutingJobsRepo *horizonrepo.ScoutingJobsRepository
	productDict      *productdict.Runtime
	pathResolver     *horizonscout.CanonicalPathResolver
	publisher        func(ctx context.Context, eventType string, tenantID string, payload interface{}) error
	logger           *log.Logger
	pathBuilder      *pathbuilder.SupplyChainPathBuilder
}

type oneHopLLMResponse struct {
	Steps []oneHopLLMStep `json:"steps"`
}

type oneHopLLMStep struct {
	SourceActorRef    string   `json:"sourceActorRef"`
	SourceActorType   string   `json:"sourceActorType"`
	SourceProductRef  string   `json:"sourceProductRef"`
	StepKind          string   `json:"stepKind"`
	TargetProductRef  string   `json:"targetProductRef"`
	TargetActorRef    string   `json:"targetActorRef"`
	TargetActorType   string   `json:"targetActorType"`
	Confidence        float64  `json:"confidence"`
	GroundingMode     string   `json:"groundingMode"`
	Evidence          []string `json:"evidence"`
	WhyNearestStep    string   `json:"whyNearestStep"`
	WhyNotBroaderStep string   `json:"whyNotBroaderStep"`
}

type oneHopStep struct {
	SourceActorRef    string
	SourceActorType   string
	SourceProductRef  string
	StepKind          string
	TargetProductRef  string
	TargetActorRef    string
	TargetActorType   string
	Confidence        float64
	GroundingMode     string
	Evidence          []string
	WhyNearestStep    string
	WhyNotBroaderStep string
}

type oneHopContext struct {
	Anchor            *horizonmodels.HorizonFragment
	RelatedFacilities []*horizonmodels.HorizonFragment
	OwnerOrgs         []*horizonmodels.HorizonFragment
	InputProducts     []*horizonmodels.HorizonFragment
	OutputProducts    []*horizonmodels.HorizonFragment
	DownstreamActors  []*horizonmodels.HorizonFragment
	ProductEdges      []*horizonmodels.DependencyEdge
	PathArtifacts     []*horizonmodels.MaterializedArtifact
}

type oneHopPersistStats struct {
	FragmentsCreated     int
	EdgesCreated         int
	ArtifactsCreated     int
	SynonymCandidates    int
	TransformCandidates  int
	AffectedPaths        map[string]struct{}
	CreatedFragmentPaths map[string]struct{}
}

func NewOneHopDownstreamService(cfg OneHopDownstreamServiceConfig) *OneHopDownstreamService {
	var pathBuilderInst *pathbuilder.SupplyChainPathBuilder
	if cfg.MaterializedRepo != nil {
		pathBuilderInst = pathbuilder.NewSupplyChainPathBuilder(cfg.FragmentsRepo, cfg.EdgesRepo, cfg.MaterializedRepo)
	}
	return &OneHopDownstreamService{
		exec:             cfg.Exec,
		fragmentsRepo:    cfg.FragmentsRepo,
		edgesRepo:        cfg.EdgesRepo,
		materializedRepo: cfg.MaterializedRepo,
		lensesRepo:       cfg.LensesRepo,
		scoutingJobsRepo: cfg.ScoutingJobsRepo,
		productDict:      cfg.ProductDict,
		pathResolver:     cfg.PathResolver,
		publisher:        cfg.Publisher,
		logger:           cfg.Logger,
		pathBuilder:      pathBuilderInst,
	}
}

func IsOneHopJob(job *horizonmodels.ScoutingJob) bool {
	if job == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(job.TargetCanonicalPath), JobUseCaseOneHopDownstream) {
		return true
	}
	if job.State == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(stringState(job.State, "useCase")), JobUseCaseOneHopDownstream)
}

func NormalizeOptions(opts OneHopDownstreamOptions) OneHopDownstreamOptions {
	opts.AnchorRef = strings.TrimSpace(opts.AnchorRef)
	opts.AnchorType = strings.ToUpper(strings.TrimSpace(opts.AnchorType))
	if opts.MaxBranchesPerProduct <= 0 {
		opts.MaxBranchesPerProduct = defaultMaxBranchesPerProduct
	}
	if opts.MaxBranchesPerProduct > 24 {
		opts.MaxBranchesPerProduct = 24
	}
	return opts
}

func DefaultJobState(opts OneHopDownstreamOptions, lensID, projectID string) map[string]interface{} {
	opts = NormalizeOptions(opts)
	state := map[string]interface{}{
		"useCase":               JobUseCaseOneHopDownstream,
		"anchorRef":             opts.AnchorRef,
		"anchorType":            opts.AnchorType,
		"maxBranchesPerProduct": opts.MaxBranchesPerProduct,
		"includeInputsContext":  opts.IncludeInputsContext,
		"allowOrgFallback":      opts.AllowOrgFallback,
		"allowLLMFallback":      opts.AllowLLMFallback,
		"currentState":          string(horizonmodels.ScoutingJobStatePending),
	}
	if strings.TrimSpace(lensID) != "" {
		state["lensId"] = strings.TrimSpace(lensID)
	}
	if strings.TrimSpace(projectID) != "" {
		state["projectId"] = strings.TrimSpace(projectID)
	}
	return state
}

func (s *OneHopDownstreamService) ExecuteJob(ctx context.Context, job *horizonmodels.ScoutingJob) error {
	if s == nil {
		return fmt.Errorf("one-hop downstream service is nil")
	}
	if job == nil {
		return fmt.Errorf("job is nil")
	}
	if !IsOneHopJob(job) {
		return fmt.Errorf("unsupported job useCase")
	}
	startedAt := time.Now().UTC()
	opts := NormalizeOptions(loadOptionsFromJob(job))
	tenantID := firstNonEmpty(strings.TrimSpace(job.TenantID), "global")
	projectID := firstNonEmpty(strings.TrimSpace(job.ProjectID), stringState(job.State, "projectId"))
	lensID := firstNonEmpty(strings.TrimSpace(job.LensID), stringState(job.State, "lensId"))
	if strings.TrimSpace(opts.AnchorRef) == "" {
		return s.failJob(ctx, job, fmt.Errorf("anchorRef is required"), startedAt)
	}

	if err := s.markRunning(ctx, job); err != nil {
		return err
	}
	_ = s.publishLifecycleEvent(ctx, "horizon.supply_chain.one_hop.started", tenantID, map[string]interface{}{
		"tenantId":   tenantID,
		"projectId":  projectID,
		"lensId":     lensID,
		"jobId":      strings.TrimSpace(job.ID),
		"anchorRef":  strings.TrimSpace(opts.AnchorRef),
		"anchorType": strings.TrimSpace(opts.AnchorType),
		"scope":      "one_hop_downstream",
		"status":     "running",
		"startedAt":  startedAt.Format(time.RFC3339Nano),
	})

	anchor, err := s.resolveAnchor(ctx, opts.AnchorRef)
	if err != nil {
		return s.failJob(ctx, job, err, startedAt)
	}
	if !isSupportedAnchorType(anchor.Type) {
		return s.failJob(ctx, job, fmt.Errorf("anchor must be ORG or FAC, got %s", anchor.Type), startedAt)
	}
	opts.AnchorRef = strings.TrimSpace(anchor.CanonicalPath)
	opts.AnchorType = string(anchor.Type)
	contextData, err := s.collectContext(ctx, tenantID, anchor, opts)
	if err != nil {
		return s.failJob(ctx, job, err, startedAt)
	}
	_ = s.publishLifecycleEvent(ctx, "horizon.supply_chain.one_hop.progress", tenantID, map[string]interface{}{
		"tenantId":      tenantID,
		"projectId":     projectID,
		"lensId":        lensID,
		"jobId":         strings.TrimSpace(job.ID),
		"anchorRef":     anchor.CanonicalPath,
		"anchorType":    string(anchor.Type),
		"scope":         "one_hop_downstream",
		"status":        "context_ready",
		"knownInputs":   len(contextData.InputProducts),
		"knownOutputs":  len(contextData.OutputProducts),
		"knownActors":   len(contextData.DownstreamActors),
		"knownProducts": len(contextData.ProductEdges),
	})

	rawSteps, citations, fallbackUsed, err := s.discoverSteps(ctx, tenantID, projectID, lensID, anchor, contextData, opts)
	if err != nil {
		return s.failJob(ctx, job, err, startedAt)
	}
	steps := s.normalizeSteps(ctx, anchor, contextData, rawSteps, opts, fallbackUsed || len(citations) == 0)
	stats, err := s.persistSteps(ctx, tenantID, lensID, projectID, anchor, contextData, steps, opts)
	if err != nil {
		return s.failJob(ctx, job, err, startedAt)
	}

	duration := time.Since(startedAt)
	if err := s.completeJob(ctx, job, anchor, steps, stats, duration); err != nil {
		return err
	}

	affected := sortedKeys(stats.AffectedPaths)
	_ = s.publishLifecycleEvent(ctx, "horizon.supply_chain.one_hop.completed", tenantID, map[string]interface{}{
		"tenantId":               tenantID,
		"projectId":              projectID,
		"lensId":                 lensID,
		"jobId":                  strings.TrimSpace(job.ID),
		"anchorRef":              anchor.CanonicalPath,
		"anchorType":             string(anchor.Type),
		"scope":                  "one_hop_downstream",
		"status":                 "completed",
		"fragmentsCreated":       stats.FragmentsCreated,
		"edgesCreated":           stats.EdgesCreated,
		"artifactsCreated":       stats.ArtifactsCreated,
		"candidateCounts":        map[string]int{"synonym": stats.SynonymCandidates, "transform": stats.TransformCandidates},
		"affectedCanonicalPaths": affected,
		"durationMs":             duration.Milliseconds(),
		"stepsAccepted":          len(steps),
	})
	return nil
}

func (s *OneHopDownstreamService) resolveAnchor(ctx context.Context, anchorRef string) (*horizonmodels.HorizonFragment, error) {
	if s == nil || s.fragmentsRepo == nil {
		return nil, fmt.Errorf("fragments repo not configured")
	}
	ref := strings.TrimSpace(anchorRef)
	if ref == "" {
		return nil, fmt.Errorf("anchorRef is required")
	}
	if strings.HasPrefix(ref, "/") {
		if s.pathResolver != nil {
			if resolved, err := s.pathResolver.Resolve(ctx, ref, horizonscout.InferFragmentTypeFromCanonicalPath(ref)); err == nil && resolved != nil {
				if resolved.ExistingFragment != nil {
					return resolved.ExistingFragment, nil
				}
				ref = strings.TrimSpace(resolved.CanonicalPath)
			}
		}
		frag, err := s.fragmentsRepo.GetByCanonicalPath(ctx, ref)
		if err != nil || frag == nil {
			return nil, fmt.Errorf("anchor fragment not found for %s", ref)
		}
		return frag, nil
	}
	frag, err := s.fragmentsRepo.Get(ctx, ref)
	if err != nil || frag == nil {
		return nil, fmt.Errorf("anchor fragment not found for id %s", ref)
	}
	return frag, nil
}

func (s *OneHopDownstreamService) collectContext(ctx context.Context, tenantID string, anchor *horizonmodels.HorizonFragment, opts OneHopDownstreamOptions) (*oneHopContext, error) {
	out := &oneHopContext{Anchor: anchor}
	if anchor == nil || s.fragmentsRepo == nil || s.edgesRepo == nil {
		return out, nil
	}

	facilitySet := map[string]*horizonmodels.HorizonFragment{}
	if anchor.Type == horizonmodels.FragmentTypeOrganization {
		for _, edgeType := range []horizonmodels.DependencyType{
			horizonmodels.DependencyTypeOwns,
			horizonmodels.DependencyTypeControls,
			horizonmodels.DependencyTypeOperatesIn,
			horizonmodels.DependencyTypeManages,
		} {
			edges, err := s.edgesRepo.ListBySource(ctx, tenantID, strings.TrimSpace(anchor.ID), &edgeType, 64)
			if err != nil {
				continue
			}
			for _, edge := range edges {
				target, err := s.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.TargetFragmentID))
				if err != nil || target == nil || target.Type != horizonmodels.FragmentTypeFacility {
					continue
				}
				facilitySet[target.ID] = target
			}
		}
	}
	if anchor.Type == horizonmodels.FragmentTypeFacility {
		out.RelatedFacilities = []*horizonmodels.HorizonFragment{anchor}
	} else {
		out.RelatedFacilities = sortFragmentsByPath(mapValuesFragments(facilitySet))
	}

	ownerSet := map[string]*horizonmodels.HorizonFragment{}
	if anchor.Type == horizonmodels.FragmentTypeFacility {
		for _, edgeType := range []horizonmodels.DependencyType{
			horizonmodels.DependencyTypeOwns,
			horizonmodels.DependencyTypeControls,
			horizonmodels.DependencyTypeOperatesIn,
			horizonmodels.DependencyTypeManages,
		} {
			edges, err := s.edgesRepo.ListByTarget(ctx, tenantID, strings.TrimSpace(anchor.ID), &edgeType, 32)
			if err != nil {
				continue
			}
			for _, edge := range edges {
				source, err := s.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.SourceFragmentID))
				if err != nil || source == nil || source.Type != horizonmodels.FragmentTypeOrganization {
					continue
				}
				ownerSet[source.ID] = source
			}
		}
	}
	out.OwnerOrgs = sortFragmentsByPath(mapValuesFragments(ownerSet))

	actorIDs := []string{strings.TrimSpace(anchor.ID)}
	for _, fac := range out.RelatedFacilities {
		if fac != nil && strings.TrimSpace(fac.ID) != "" && strings.TrimSpace(fac.ID) != strings.TrimSpace(anchor.ID) {
			actorIDs = append(actorIDs, strings.TrimSpace(fac.ID))
		}
	}
	actorIDs = compactUniqueStrings(actorIDs)

	produceEdges, _ := s.edgesRepo.ListBySourceIDsAndType(ctx, tenantID, actorIDs, horizonmodels.DependencyTypeProduces)
	productIDs := make([]string, 0, len(produceEdges))
	for _, edge := range produceEdges {
		if edge == nil {
			continue
		}
		productIDs = append(productIDs, strings.TrimSpace(edge.TargetFragmentID))
	}
	if len(productIDs) > 0 {
		if products, err := s.fragmentsRepo.GetMany(ctx, productIDs); err == nil {
			out.OutputProducts = collectFragmentsByIDs(products, productIDs, horizonmodels.FragmentTypeProduct, maxPromptProducts)
		}
	}

	if opts.IncludeInputsContext {
		incomingSupply, _ := s.edgesRepo.ListByTargetIDsAndType(ctx, tenantID, actorIDs, horizonmodels.DependencyTypeSuppliesTo)
		inputIDs := make([]string, 0, len(incomingSupply))
		for _, edge := range incomingSupply {
			if edge == nil {
				continue
			}
			inputIDs = append(inputIDs, strings.TrimSpace(edge.SourceFragmentID))
		}
		if len(inputIDs) > 0 {
			if inputs, err := s.fragmentsRepo.GetMany(ctx, inputIDs); err == nil {
				out.InputProducts = collectFragmentsByIDs(inputs, inputIDs, horizonmodels.FragmentTypeProduct, maxPromptProducts)
			}
		}
	}

	downstreamSupply, _ := s.edgesRepo.ListBySourceIDsAndType(ctx, tenantID, actorIDs, horizonmodels.DependencyTypeSuppliesTo)
	downstreamIDs := make([]string, 0, len(downstreamSupply))
	for _, edge := range downstreamSupply {
		if edge == nil {
			continue
		}
		downstreamIDs = append(downstreamIDs, strings.TrimSpace(edge.TargetFragmentID))
	}
	if len(downstreamIDs) > 0 {
		if actors, err := s.fragmentsRepo.GetMany(ctx, downstreamIDs); err == nil {
			out.DownstreamActors = collectActorFragmentsByIDs(actors, downstreamIDs, maxPromptActors)
		}
	}

	productPathIDs := make([]string, 0, len(out.OutputProducts))
	for _, product := range out.OutputProducts {
		if product != nil && strings.TrimSpace(product.ID) != "" {
			productPathIDs = append(productPathIDs, strings.TrimSpace(product.ID))
		}
	}
	productEdges := make([]*horizonmodels.DependencyEdge, 0, 32)
	if len(productPathIDs) > 0 {
		for _, edgeType := range horizonmodels.ProductTransitionEdgeTypes() {
			typedEdges, _ := s.edgesRepo.ListBySourceIDsAndType(ctx, tenantID, productPathIDs, edgeType)
			productEdges = append(productEdges, typedEdges...)
		}
	}
	out.ProductEdges = productEdges

	if s.materializedRepo != nil {
		artifacts, _ := s.materializedRepo.FindByNodeRef(ctx, tenantID, strings.TrimSpace(anchor.CanonicalPath), horizonmodels.MaterializedKindSupplyChainPath)
		if len(artifacts) > maxPromptArtifacts {
			artifacts = artifacts[:maxPromptArtifacts]
		}
		out.PathArtifacts = artifacts
	}
	return out, nil
}

func (s *OneHopDownstreamService) discoverSteps(
	ctx context.Context,
	tenantID string,
	projectID string,
	lensID string,
	anchor *horizonmodels.HorizonFragment,
	contextData *oneHopContext,
	opts OneHopDownstreamOptions,
) ([]oneHopLLMStep, []ai.WebCitation, bool, error) {
	if s == nil || s.exec == nil {
		return nil, nil, false, fmt.Errorf("AI executor not configured")
	}
	prompt := s.buildPrompt(anchor, contextData, opts)
	verifier := ai.NewGenericVerifier(func(resp oneHopLLMResponse) error {
		return validateLLMResponse(resp)
	})
	task := &ai.AITask{
		TaskType:     ai.TaskTypeKnowledgeSynthesis,
		InputContent: prompt,
		AIRequirements: ai.AIRequirements{
			PreferredModel:            "gemini-3.1-flash-lite-preview",
			MinContextWindow:          24000,
			MaxOutputSize:             16384,
			MinCapabilityScore:        6,
			TextGenerationCapability:  true,
			SearchGroundingCapability: true,
		},
		TenantID:  tenantID,
		Project:   projectID,
		UseCase:   "horizon.supply_chain.one_hop_downstream",
		CascadeID: firstNonEmpty(lensID, strings.TrimSpace(anchor.CanonicalPath)),
		Verifier:  verifier,
	}
	parsed, fullResult, err := s.exec.ExecuteStructuredTaskFull(ctx, task)
	if err == nil {
		if response, ok := parsed.(oneHopLLMResponse); ok {
			return response.Steps, fullResult.Citations, false, nil
		}
	}
	if !opts.AllowLLMFallback {
		if err != nil {
			return nil, nil, false, err
		}
		return nil, nil, false, fmt.Errorf("unexpected one-hop structured response type")
	}

	task.AIRequirements.SearchGroundingCapability = false
	task.AIRequirements.PreferredModel = "gemini-3.1-flash-lite-preview"
	parsed, fullResult, err = s.exec.ExecuteStructuredTaskFull(ctx, task)
	if err != nil {
		return nil, nil, true, err
	}
	response, ok := parsed.(oneHopLLMResponse)
	if !ok {
		return nil, nil, true, fmt.Errorf("unexpected one-hop fallback response type")
	}
	return response.Steps, fullResult.Citations, true, nil
}

func (s *OneHopDownstreamService) normalizeSteps(
	ctx context.Context,
	anchor *horizonmodels.HorizonFragment,
	contextData *oneHopContext,
	raw []oneHopLLMStep,
	opts OneHopDownstreamOptions,
	forceFallback bool,
) []oneHopStep {
	if anchor == nil {
		return nil
	}
	allowedProducts := map[string]struct{}{}
	for _, frag := range append(append([]*horizonmodels.HorizonFragment{}, contextData.OutputProducts...), contextData.InputProducts...) {
		if frag != nil && strings.HasPrefix(strings.TrimSpace(frag.CanonicalPath), "/product/") {
			allowedProducts[strings.TrimSpace(frag.CanonicalPath)] = struct{}{}
		}
	}
	allowedActors := map[string]struct{}{strings.TrimSpace(anchor.CanonicalPath): {}}
	for _, frag := range append(append([]*horizonmodels.HorizonFragment{}, contextData.RelatedFacilities...), append(contextData.OwnerOrgs, contextData.DownstreamActors...)...) {
		if frag != nil && strings.TrimSpace(frag.CanonicalPath) != "" {
			allowedActors[strings.TrimSpace(frag.CanonicalPath)] = struct{}{}
		}
	}

	grouped := map[string][]oneHopStep{}
	for _, step := range raw {
		normalized := oneHopStep{
			SourceActorRef:    strings.TrimSpace(step.SourceActorRef),
			SourceActorType:   normalizeActorType(step.SourceActorType),
			SourceProductRef:  strings.TrimSpace(step.SourceProductRef),
			StepKind:          normalizeStepKind(step.StepKind),
			TargetProductRef:  strings.TrimSpace(step.TargetProductRef),
			TargetActorRef:    strings.TrimSpace(step.TargetActorRef),
			TargetActorType:   normalizeActorType(step.TargetActorType),
			Confidence:        clamp01(step.Confidence),
			GroundingMode:     normalizeGroundingMode(step.GroundingMode),
			Evidence:          compactUniqueStrings(step.Evidence),
			WhyNearestStep:    strings.TrimSpace(step.WhyNearestStep),
			WhyNotBroaderStep: strings.TrimSpace(step.WhyNotBroaderStep),
		}
		if normalized.StepKind == "" {
			continue
		}
		if normalized.SourceActorRef == "" {
			normalized.SourceActorRef = strings.TrimSpace(anchor.CanonicalPath)
		}
		if normalized.SourceActorType == "" {
			normalized.SourceActorType = normalizeActorType(string(anchor.Type))
		}
		if normalized.SourceProductRef == "" || !strings.HasPrefix(normalized.SourceProductRef, "/product/") {
			continue
		}
		if len(allowedProducts) > 0 {
			if _, ok := allowedProducts[normalized.SourceProductRef]; !ok {
				continue
			}
		}
		if normalized.StepKind == "pass_through" && normalized.TargetProductRef == "" {
			normalized.TargetProductRef = normalized.SourceProductRef
		}
		if normalized.StepKind != "pass_through" && normalized.TargetProductRef == "" {
			continue
		}
		if normalized.TargetProductRef != "" && !strings.HasPrefix(normalized.TargetProductRef, "/product/") {
			continue
		}
		if normalized.TargetActorRef != "" && normalized.TargetActorType == "" {
			normalized.TargetActorType = normalizeActorType(string(horizonscout.InferFragmentTypeFromCanonicalPath(normalized.TargetActorRef)))
		}
		if normalized.TargetActorRef == "" && opts.AllowOrgFallback {
			normalized.TargetActorType = "ORG"
		}
		if normalized.TargetActorRef == "" && normalized.TargetActorType == "" {
			continue
		}
		if err := validateNormalizedStep(normalized); err != nil {
			continue
		}
		if forceFallback && normalized.GroundingMode == "search" {
			normalized.GroundingMode = "llm_fallback"
		}
		if normalized.GroundingMode == "llm_fallback" {
			normalized.Confidence = minFloat(0.72, normalized.Confidence)
		}
		key := normalized.SourceProductRef
		grouped[key] = append(grouped[key], normalized)
	}

	final := make([]oneHopStep, 0, len(raw))
	for _, sourceProductRef := range sortedKeysInterface(grouped) {
		steps := grouped[sourceProductRef]
		sort.SliceStable(steps, func(i, j int) bool {
			if steps[i].Confidence == steps[j].Confidence {
				return buildStepDedupKey(steps[i]) < buildStepDedupKey(steps[j])
			}
			return steps[i].Confidence > steps[j].Confidence
		})
		seen := map[string]struct{}{}
		for _, step := range steps {
			key := buildStepDedupKey(step)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			final = append(final, step)
			if opts.MaxBranchesPerProduct > 0 && len(seen) >= opts.MaxBranchesPerProduct {
				break
			}
		}
	}
	return final
}

func (s *OneHopDownstreamService) persistSteps(
	ctx context.Context,
	tenantID string,
	lensID string,
	projectID string,
	anchor *horizonmodels.HorizonFragment,
	contextData *oneHopContext,
	steps []oneHopStep,
	opts OneHopDownstreamOptions,
) (*oneHopPersistStats, error) {
	stats := &oneHopPersistStats{
		AffectedPaths:        map[string]struct{}{},
		CreatedFragmentPaths: map[string]struct{}{},
	}
	if len(steps) == 0 {
		return stats, nil
	}

	pathToFragment := map[string]*horizonmodels.HorizonFragment{}
	cacheFragment := func(f *horizonmodels.HorizonFragment) {
		if f != nil && strings.TrimSpace(f.CanonicalPath) != "" {
			pathToFragment[strings.TrimSpace(f.CanonicalPath)] = f
			stats.AffectedPaths[strings.TrimSpace(f.CanonicalPath)] = struct{}{}
		}
	}
	cacheFragment(anchor)
	for _, frag := range append(append([]*horizonmodels.HorizonFragment{}, contextData.RelatedFacilities...), append(append(contextData.OwnerOrgs, contextData.InputProducts...), append(contextData.OutputProducts, contextData.DownstreamActors...)...)...) {
		cacheFragment(frag)
	}

	for _, step := range steps {
		sourceActor, sourceCreated, err := s.ensureFragmentByPath(ctx, step.SourceActorRef, pathToFragment, "one_hop_downstream")
		if err != nil {
			return stats, err
		}
		cacheFragment(sourceActor)
		if sourceCreated {
			stats.FragmentsCreated++
			stats.CreatedFragmentPaths[strings.TrimSpace(sourceActor.CanonicalPath)] = struct{}{}
		}
		sourceProduct, sourceProductCreated, err := s.ensureFragmentByPath(ctx, step.SourceProductRef, pathToFragment, "one_hop_downstream")
		if err != nil {
			return stats, err
		}
		cacheFragment(sourceProduct)
		if sourceProductCreated {
			stats.FragmentsCreated++
			stats.SynonymCandidates++
			stats.CreatedFragmentPaths[strings.TrimSpace(sourceProduct.CanonicalPath)] = struct{}{}
		}

		var targetActor *horizonmodels.HorizonFragment
		if strings.TrimSpace(step.TargetActorRef) != "" {
			targetActorCreated := false
			targetActor, targetActorCreated, err = s.ensureFragmentByPath(ctx, step.TargetActorRef, pathToFragment, "one_hop_downstream")
			if err != nil {
				return stats, err
			}
			cacheFragment(targetActor)
			if targetActorCreated {
				stats.FragmentsCreated++
				stats.CreatedFragmentPaths[strings.TrimSpace(targetActor.CanonicalPath)] = struct{}{}
			}
		}

		var targetProduct *horizonmodels.HorizonFragment
		if strings.TrimSpace(step.TargetProductRef) != "" {
			targetProductCreated := false
			targetProduct, targetProductCreated, err = s.ensureFragmentByPath(ctx, step.TargetProductRef, pathToFragment, "one_hop_downstream")
			if err != nil {
				return stats, err
			}
			cacheFragment(targetProduct)
			if targetProductCreated {
				stats.FragmentsCreated++
				stats.SynonymCandidates++
				stats.CreatedFragmentPaths[strings.TrimSpace(targetProduct.CanonicalPath)] = struct{}{}
			}
		}

		if created, edge, err := s.ensureEdge(ctx, tenantID, sourceActor, sourceProduct, horizonmodels.DependencyTypeProduces, buildStepQualifiers(step, anchor, "actor_output")); err != nil {
			return stats, err
		} else if created {
			stats.EdgesCreated++
			_ = s.publishEdgeUpsert(ctx, tenantID, edge)
		}

		if targetActor != nil {
			if created, edge, err := s.ensureEdge(ctx, tenantID, sourceActor, targetActor, horizonmodels.DependencyTypeSuppliesTo, buildStepQualifiers(step, anchor, "actor_continuation")); err != nil {
				return stats, err
			} else if created {
				stats.EdgesCreated++
				_ = s.publishEdgeUpsert(ctx, tenantID, edge)
			}
			if created, edge, err := s.ensureEdge(ctx, tenantID, sourceProduct, targetActor, horizonmodels.DependencyTypeSuppliesTo, buildStepQualifiers(step, anchor, "input_context")); err != nil {
				return stats, err
			} else if created {
				stats.EdgesCreated++
				_ = s.publishEdgeUpsert(ctx, tenantID, edge)
			}
		}

		if step.StepKind == "transformation" || step.StepKind == "assembly" || step.StepKind == "usage" {
			if targetProduct == nil {
				continue
			}
			edgeType := edgeTypeForStepKind(step.StepKind)
			if s.productDict != nil {
				if _, known, _, _, err := s.productDict.NormalizeProductEdgeType(ctx, edgeType, horizonmodels.FragmentTypeProduct, horizonmodels.FragmentTypeProduct, sourceProduct.CanonicalPath, targetProduct.CanonicalPath); err == nil && !known {
					stats.TransformCandidates++
				}
			}
			if created, edge, err := s.ensureEdge(ctx, tenantID, sourceProduct, targetProduct, edgeType, buildStepQualifiers(step, anchor, "product_transition")); err != nil {
				return stats, err
			} else if created {
				stats.EdgesCreated++
				_ = s.publishEdgeUpsert(ctx, tenantID, edge)
			}
			if targetActor != nil {
				if created, edge, err := s.ensureEdge(ctx, tenantID, targetActor, targetProduct, horizonmodels.DependencyTypeProduces, buildStepQualifiers(step, anchor, "target_output")); err != nil {
					return stats, err
				} else if created {
					stats.EdgesCreated++
					_ = s.publishEdgeUpsert(ctx, tenantID, edge)
				}
			}
		}

		if shouldMaterializeStep(step, sourceActor, targetActor) {
			created, err := s.ensureMaterializedPath(ctx, tenantID, lensID, sourceActor, sourceProduct, targetActor, step)
			if err != nil {
				return stats, err
			}
			if created {
				stats.ArtifactsCreated++
			}
		}
	}
	return stats, nil
}

func (s *OneHopDownstreamService) ensureFragmentByPath(
	ctx context.Context,
	rawPath string,
	cache map[string]*horizonmodels.HorizonFragment,
	provenanceMethod string,
) (*horizonmodels.HorizonFragment, bool, error) {
	if s == nil || s.fragmentsRepo == nil {
		return nil, false, fmt.Errorf("fragments repo not configured")
	}
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return nil, false, nil
	}
	if cached := cache[path]; cached != nil && strings.TrimSpace(cached.ID) != "" {
		return cached, false, nil
	}
	if strings.HasPrefix(path, "/product/") && s.productDict != nil {
		if canonicalPath, matched, err := s.productDict.CanonicalizeProductPath(ctx, path); err == nil && matched && strings.TrimSpace(canonicalPath) != "" {
			path = strings.TrimSpace(canonicalPath)
			if cached := cache[path]; cached != nil && strings.TrimSpace(cached.ID) != "" {
				return cached, false, nil
			}
		}
	}
	if existing, err := s.fragmentsRepo.GetByCanonicalPath(ctx, path); err == nil && existing != nil && strings.TrimSpace(existing.ID) != "" {
		cache[path] = existing
		return existing, false, nil
	}
	fragmentType := horizonscout.InferFragmentTypeFromCanonicalPath(path)
	switch fragmentType {
	case horizonmodels.FragmentTypeOrganization,
		horizonmodels.FragmentTypeFacility,
		horizonmodels.FragmentTypeProduct,
		horizonmodels.FragmentTypeDeposit,
		horizonmodels.FragmentTypeProgram,
		horizonmodels.FragmentTypeGeoRegion,
		horizonmodels.FragmentTypeMarket,
		horizonmodels.FragmentTypeCommunity:
	default:
		return nil, false, fmt.Errorf("unsupported fragment type for path %s", path)
	}
	label := pathLabel(path)
	fragment := &horizonmodels.HorizonFragment{
		TenantID:      "global",
		CanonicalPath: path,
		Type:          fragmentType,
		Labels:        map[string]string{"en": label},
		Identifiers:   map[string]string{},
		State:         map[string]horizonmodels.MetricValue{},
		Governance:    horizonmodels.Governance{Owner: "SYSTEM", Status: string(horizonmodels.FragmentStatusActive)},
		Metadata: horizonmodels.FragmentMetadata{
			CreatedAt:       time.Now().UTC(),
			LastUpdatedAt:   time.Now().UTC(),
			EpistemicStatus: horizonmodels.EpistemicStatusLowConfidence,
		},
		Provenance: horizonmodels.Provenance{Method: provenanceMethod},
		Status:     "active",
	}
	id, err := s.fragmentsRepo.CreateAssumeAbsent(ctx, fragment)
	if err != nil {
		return nil, false, err
	}
	fragment.ID = id
	cache[path] = fragment
	_ = s.publishNodeUpsert(ctx, "global", fragment, "upsert")
	return fragment, true, nil
}

func (s *OneHopDownstreamService) ensureEdge(
	ctx context.Context,
	tenantID string,
	source *horizonmodels.HorizonFragment,
	target *horizonmodels.HorizonFragment,
	edgeType horizonmodels.DependencyType,
	qualifiers map[string]interface{},
) (bool, *horizonmodels.DependencyEdge, error) {
	if s == nil || s.edgesRepo == nil || source == nil || target == nil {
		return false, nil, nil
	}
	existing, _ := s.edgesRepo.ListBySourceTargetAndType(ctx, tenantID, strings.TrimSpace(source.ID), strings.TrimSpace(target.ID), edgeType, 1)
	if len(existing) > 0 && existing[0] != nil && strings.TrimSpace(existing[0].ID) != "" {
		return false, existing[0], nil
	}
	edge := &horizonmodels.DependencyEdge{
		TenantID:         firstNonEmpty(strings.TrimSpace(tenantID), "global"),
		SourceFragmentID: strings.TrimSpace(source.ID),
		TargetFragmentID: strings.TrimSpace(target.ID),
		Type:             edgeType,
		Properties: horizonmodels.EdgeProperties{
			Criticality:   clamp01(floatOrDefault(qualifiers["confidence"], 0.75)),
			Amplification: 1,
		},
		Qualifiers: qualifiers,
		Provenance: horizonmodels.Provenance{
			Method:    "one_hop_downstream",
			SourceIDs: compactUniqueStrings(interfaceStringSlice(qualifiers["evidence"])),
		},
		Metadata: horizonmodels.EdgeMetadata{
			CreatedAt:     time.Now().UTC(),
			LastUpdatedAt: time.Now().UTC(),
		},
	}
	id, err := s.edgesRepo.Create(ctx, edge)
	if err != nil {
		return false, nil, err
	}
	edge.ID = id
	return true, edge, nil
}

func (s *OneHopDownstreamService) ensureMaterializedPath(
	ctx context.Context,
	tenantID string,
	lensID string,
	sourceActor *horizonmodels.HorizonFragment,
	product *horizonmodels.HorizonFragment,
	targetActor *horizonmodels.HorizonFragment,
	step oneHopStep,
) (bool, error) {
	if s == nil || s.pathBuilder == nil || sourceActor == nil || product == nil || targetActor == nil {
		return false, nil
	}
	segment := pathbuilder.PathSegment{
		SupplierRef: strings.TrimSpace(sourceActor.CanonicalPath),
		ProductRef:  strings.TrimSpace(product.CanonicalPath),
		CustomerRef: strings.TrimSpace(targetActor.CanonicalPath),
		Confidence:  step.Confidence,
		TenantID:    firstNonEmpty(strings.TrimSpace(tenantID), "global"),
	}
	path := &pathbuilder.SupplyChainPath{
		Segments:   []pathbuilder.PathSegment{segment},
		Confidence: step.Confidence,
		BuildMode:  "one_hop_downstream",
		MaxDepth:   1,
	}
	canonicalKey := pathbuilder.BuildStructuralCanonicalKey(segment.TenantID, path.Segments)
	if canonicalKey == "" {
		return false, nil
	}
	if s.materializedRepo != nil {
		if existing, err := s.materializedRepo.GetByCanonicalKey(ctx, segment.TenantID, horizonmodels.MaterializedKindSupplyChainPath, canonicalKey); err == nil && existing != nil {
			_, err = s.pathBuilder.MaterializePath(ctx, path, segment.TenantID, lensID, canonicalKey)
			if err == nil && strings.TrimSpace(lensID) != "" && s.lensesRepo != nil {
				if artifact, artErr := s.materializedRepo.GetByCanonicalKey(ctx, segment.TenantID, horizonmodels.MaterializedKindSupplyChainPath, canonicalKey); artErr == nil && artifact != nil {
					_ = s.lensesRepo.UpsertScopeArtifact(ctx, strings.TrimSpace(lensID), strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/"), oneHopScopeArtifactRef(artifact))
				}
			}
			return false, err
		}
	}
	_, err := s.pathBuilder.MaterializePath(ctx, path, segment.TenantID, lensID, canonicalKey)
	if err == nil && strings.TrimSpace(lensID) != "" && s.lensesRepo != nil && s.materializedRepo != nil {
		if artifact, artErr := s.materializedRepo.GetByCanonicalKey(ctx, segment.TenantID, horizonmodels.MaterializedKindSupplyChainPath, canonicalKey); artErr == nil && artifact != nil {
			_ = s.lensesRepo.UpsertScopeArtifact(ctx, strings.TrimSpace(lensID), strings.TrimPrefix(strings.TrimSpace(artifact.CanonicalKey), "/"), oneHopScopeArtifactRef(artifact))
		}
	}
	return err == nil, err
}

func oneHopScopeArtifactRef(artifact *horizonmodels.MaterializedArtifact) horizonmodels.ScopeArtifactRef {
	if artifact == nil {
		return horizonmodels.ScopeArtifactRef{}
	}
	horizonmodels.PopulateMaterializedArtifactIndexes(artifact)
	ref := horizonmodels.ScopeArtifactRef{
		Kind:               string(artifact.Kind),
		DisplayName:        artifact.DisplayName,
		AnchorSupplier:     artifact.AnchorSupplier,
		AnchorCustomer:     artifact.AnchorCustomer,
		ProductAxis:        artifact.ProductAxis,
		NodeRefs:           append([]string(nil), artifact.NodeRefs...),
		Status:             artifact.PathStatus,
		GeoRefs:            append([]string(nil), artifact.GeoRefs...),
		FlowRefs:           append([]string(nil), artifact.FlowRefs...),
		ZoneRefs:           append([]string(nil), artifact.ZoneRefs...),
		AffectedZoneRefs:   append([]string(nil), artifact.AffectedZoneRefs...),
		LegCount:           artifact.LegCount,
		BindingConfidence:  artifact.BindingScore,
		PathStressScore:    artifact.PathStressScore,
		BottleneckLegIndex: artifact.BottleneckLeg,
		RecoveryLatencyHrs: artifact.RecoveryLatencyHrs,
	}
	if parsed, err := horizonmodels.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
		summary := horizonmodels.SummarizeSupplyChainPathBindings(parsed)
		ref.GeoRefs = summary.GeoRefs
		ref.FlowRefs = summary.FlowRefs
		ref.ZoneRefs = summary.ZoneRefs
		ref.AffectedZoneRefs = summary.AffectedZoneRefs
		ref.SpatialSemantics = summary.SpatialSemantics
		ref.LegCount = summary.LegCount
		ref.BindingConfidence = summary.BindingConfidence
		ref.PathStressScore = summary.PathStressScore
		ref.BottleneckLegIndex = summary.BottleneckLegIndex
		ref.RecoveryLatencyHrs = summary.RecoveryLatencyHrs
	}
	return ref
}

func (s *OneHopDownstreamService) buildPrompt(anchor *horizonmodels.HorizonFragment, contextData *oneHopContext, opts OneHopDownstreamOptions) string {
	payload := map[string]interface{}{
		"anchor": map[string]interface{}{
			"canonicalPath": strings.TrimSpace(anchor.CanonicalPath),
			"type":          string(anchor.Type),
			"label":         fragmentLabel(anchor),
		},
		"relatedFacilities":       summarizeFragments(contextData.RelatedFacilities, maxPromptFacilities),
		"ownerOrgs":               summarizeFragments(contextData.OwnerOrgs, maxPromptActors),
		"inputProducts":           summarizeFragments(contextData.InputProducts, maxPromptProducts),
		"outputProducts":          summarizeFragments(contextData.OutputProducts, maxPromptProducts),
		"knownDownstreamActors":   summarizeFragments(contextData.DownstreamActors, maxPromptActors),
		"knownProductTransitions": summarizeProductEdges(contextData.ProductEdges),
		"knownMaterializedPaths":  summarizeArtifacts(contextData.PathArtifacts),
		"options": map[string]interface{}{
			"maxBranchesPerProduct": opts.MaxBranchesPerProduct,
			"includeInputsContext":  opts.IncludeInputsContext,
			"allowOrgFallback":      opts.AllowOrgFallback,
			"allowLLMFallback":      opts.AllowLLMFallback,
		},
	}
	blob, _ := json.MarshalIndent(payload, "", "  ")

	var b strings.Builder
	b.WriteString("You build Horizon one-hop downstream supply-chain expansions.\n")
	b.WriteString("Return the fullest possible set of nearest downstream steps for the anchor below.\n")
	b.WriteString("Depth is STRICTLY one hop. Prefer recall across parallel immediate branches, not depth.\n")
	b.WriteString("You MUST use canonical refs, not prose names, for actors and products.\n")
	b.WriteString("Prefer downstream FACILITY refs. Use ORG refs only when the facility cannot be supported.\n")
	b.WriteString("Allowed step kinds ONLY: pass_through, transformation, assembly, usage.\n")
	b.WriteString("Definitions:\n")
	b.WriteString("- pass_through: the same canonical product class is handed to the next actor without product change.\n")
	b.WriteString("- transformation: the upstream product is processed/refined into the next canonical product class.\n")
	b.WriteString("- assembly: a component/module/subassembly is assembled into the next canonical product class.\n")
	b.WriteString("- usage: the upstream product is directly used by the downstream product or equipment, but does not transform into it and does not become an assembled component of it.\n")
	b.WriteString("Strict bans:\n")
	b.WriteString("- no long jumps across obvious intermediate stages\n")
	b.WriteString("- no broad end-use industries or weak generic usage categories\n")
	b.WriteString("- no loops without strong reason\n")
	b.WriteString("- no service/logistics/document nodes as products\n")
	b.WriteString("- no raw-material-to-final-product jumps like ore -> vehicle\n")
	b.WriteString("- class/subclass is not synonymy and not nearest downstream unless it is the immediate assembled product class\n")
	b.WriteString("Direct operational usage is allowed when it is a specific nearest product/product dependency grounded in a concrete industrial or institutional consuming facility. Examples allowed: helium -> MRI scanner, helium -> semiconductor lithography tool, industrial gas -> wafer-fab process input. Examples not allowed: helium -> healthcare industry, industrial gas -> electronics sector.\n")
	b.WriteString("Direct nearest assembly into a final product is allowed when there is no meaningful intermediate product stage. Examples allowed: tire -> vehicle, battery-pack -> electric-vehicle, display-module -> smartphone. Examples not allowed: steel-sheet -> vehicle, ore -> vehicle.\n")
	b.WriteString("Products must stay at a stable abstraction level. Use product classes, not serial instances or specific traded units.\n")
	b.WriteString("For each step, explain why it is the nearest step and why you did not choose a broader or farther target.\n")
	b.WriteString("Grounding mode rules:\n")
	b.WriteString("- graph: directly grounded in supplied graph context\n")
	b.WriteString("- search: supported primarily by search grounding\n")
	b.WriteString("- mixed: both graph and search grounding materially support it\n")
	b.WriteString("- llm_fallback: plausible nearest industrial step when direct grounding is weak but still coherent\n")
	b.WriteString("Return EXACTLY one strict JSON object with this shape:\n")
	b.WriteString("{\"steps\":[{\"sourceActorRef\":\"/fac/... or /org/...\",\"sourceActorType\":\"FAC|ORG\",\"sourceProductRef\":\"/product/...\",\"stepKind\":\"pass_through|transformation|assembly|usage\",\"targetProductRef\":\"/product/...\",\"targetActorRef\":\"/fac/... or /org/... or empty\",\"targetActorType\":\"FAC|ORG or empty\",\"confidence\":0.0,\"groundingMode\":\"graph|search|mixed|llm_fallback\",\"evidence\":[\"short evidence string\"],\"whyNearestStep\":\"...\",\"whyNotBroaderStep\":\"...\"}]}\n")
	b.WriteString("If a field is unknown, use empty string or empty array. No markdown. No prose outside JSON.\n\n")
	b.WriteString("Graph context JSON:\n")
	b.Write(blob)
	return b.String()
}

func validateLLMResponse(resp oneHopLLMResponse) error {
	if len(resp.Steps) == 0 {
		return nil
	}
	for i, step := range resp.Steps {
		if normalizeStepKind(step.StepKind) == "" {
			return fmt.Errorf("steps[%d].stepKind is invalid", i)
		}
		if strings.TrimSpace(step.SourceProductRef) == "" {
			return fmt.Errorf("steps[%d].sourceProductRef is required", i)
		}
		if normalizeStepKind(step.StepKind) != "pass_through" && strings.TrimSpace(step.TargetProductRef) == "" {
			return fmt.Errorf("steps[%d].targetProductRef is required", i)
		}
	}
	return nil
}

func validateNormalizedStep(step oneHopStep) error {
	if step.SourceActorRef == "" || step.SourceProductRef == "" {
		return fmt.Errorf("missing source fields")
	}
	if step.TargetActorRef != "" && step.TargetActorRef == step.SourceActorRef {
		return fmt.Errorf("source and target actors must differ")
	}
	switch step.StepKind {
	case "pass_through":
		if step.TargetProductRef != "" && step.TargetProductRef != step.SourceProductRef {
			return fmt.Errorf("pass_through must keep the same product")
		}
	case "transformation", "assembly", "usage":
		if step.TargetProductRef == "" || step.TargetProductRef == step.SourceProductRef {
			return fmt.Errorf("transition must change product")
		}
	default:
		return fmt.Errorf("invalid step kind")
	}
	if isBroadLongJump(step) {
		return fmt.Errorf("broad long jump rejected")
	}
	return nil
}

func isBroadLongJump(step oneHopStep) bool {
	source := strings.ToLower(strings.TrimSpace(step.SourceProductRef))
	target := strings.ToLower(strings.TrimSpace(step.TargetProductRef))
	if source == "" || target == "" {
		return false
	}
	if step.StepKind == "pass_through" {
		return false
	}
	if step.StepKind == "usage" {
		return false
	}
	rawLike := containsAny(source, "ore", "concentrate", "raw-", "crude", "/raw/", "/material/", "feedstock", "yellowcake")
	componentLike := containsAny(source, "module", "pack", "cell", "tire", "sensor", "display", "ecu", "board", "blade", "glass", "motor", "engine", "battery-pack", "bms")
	finalLike := containsAny(target, "vehicle", "smartphone", "iphone", "laptop", "aircraft", "reactor", "machine", "machinery", "phone", "computer")
	if rawLike && finalLike {
		return true
	}
	if step.StepKind == "assembly" && !componentLike && finalLike && containsAny(source, "/material/", "steel", "copper", "aluminum", "aluminium", "rubber", "chemical") {
		return true
	}
	if step.StepKind == "transformation" && finalLike && rawLike {
		return true
	}
	return false
}

func shouldMaterializeStep(step oneHopStep, sourceActor, targetActor *horizonmodels.HorizonFragment) bool {
	if sourceActor == nil || targetActor == nil {
		return false
	}
	return sourceActor.Type == horizonmodels.FragmentTypeFacility && targetActor.Type == horizonmodels.FragmentTypeFacility
}

func buildStepQualifiers(step oneHopStep, anchor *horizonmodels.HorizonFragment, purpose string) map[string]interface{} {
	qualifiers := map[string]interface{}{
		"oneHopPurpose":     purpose,
		"oneHopStepKind":    step.StepKind,
		"oneHopConfidence":  step.Confidence,
		"groundingMode":     step.GroundingMode,
		"whyNearestStep":    step.WhyNearestStep,
		"whyNotBroaderStep": step.WhyNotBroaderStep,
		"oneHopGenerated":   true,
		"oneHopAnchorRef":   strings.TrimSpace(anchor.CanonicalPath),
		"oneHopAnchorType":  string(anchor.Type),
		"source_role":       "supplier",
		"target_role":       "customer",
		"evidence":          append([]string(nil), step.Evidence...),
		"confidence":        step.Confidence,
	}
	if purpose == "actor_output" || purpose == "target_output" {
		delete(qualifiers, "source_role")
		delete(qualifiers, "target_role")
	}
	if purpose == "product_transition" {
		delete(qualifiers, "source_role")
		delete(qualifiers, "target_role")
		if relationKind := relationKindForStepKind(step.StepKind); relationKind != "" {
			qualifiers["productRelationKind"] = relationKind
		}
	}
	return qualifiers
}

func (s *OneHopDownstreamService) markRunning(ctx context.Context, job *horizonmodels.ScoutingJob) error {
	job.Status = coremodels.AgentJobStatusRunning
	job.Progress = 0.05
	job.CurrentState = horizonmodels.ScoutingJobStateExecution
	job.UpdatedAt = time.Now().UTC()
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	job.State["currentState"] = string(job.CurrentState)
	job.State["useCase"] = JobUseCaseOneHopDownstream
	return s.scoutingJobsRepo.Update(ctx, job)
}

func (s *OneHopDownstreamService) completeJob(
	ctx context.Context,
	job *horizonmodels.ScoutingJob,
	anchor *horizonmodels.HorizonFragment,
	steps []oneHopStep,
	stats *oneHopPersistStats,
	duration time.Duration,
) error {
	now := time.Now().UTC()
	job.Status = coremodels.AgentJobStatusCompleted
	job.Progress = 1
	job.CurrentState = horizonmodels.ScoutingJobStateVerified
	job.UpdatedAt = now
	job.CompletedAt = &now
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	job.State["currentState"] = string(job.CurrentState)
	job.State["fragmentsCreated"] = stats.FragmentsCreated
	job.State["edgesCreated"] = stats.EdgesCreated
	job.State["artifactsCreated"] = stats.ArtifactsCreated
	job.State["affectedCanonicalPaths"] = sortedKeys(stats.AffectedPaths)
	job.Result = map[string]interface{}{
		"scope":                  "one_hop_downstream",
		"anchorRef":              strings.TrimSpace(anchor.CanonicalPath),
		"anchorType":             string(anchor.Type),
		"stepsAccepted":          len(steps),
		"fragmentsCreated":       stats.FragmentsCreated,
		"edgesCreated":           stats.EdgesCreated,
		"artifactsCreated":       stats.ArtifactsCreated,
		"candidateCounts":        map[string]int{"synonym": stats.SynonymCandidates, "transform": stats.TransformCandidates},
		"affectedCanonicalPaths": sortedKeys(stats.AffectedPaths),
		"durationMs":             duration.Milliseconds(),
	}
	return s.scoutingJobsRepo.Update(ctx, job)
}

func (s *OneHopDownstreamService) failJob(ctx context.Context, job *horizonmodels.ScoutingJob, runErr error, startedAt time.Time) error {
	if job == nil {
		return runErr
	}
	now := time.Now().UTC()
	job.Status = coremodels.AgentJobStatusFailed
	job.Progress = 1
	job.CurrentState = horizonmodels.ScoutingJobStateFailed
	job.UpdatedAt = now
	job.CompletedAt = &now
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	job.State["currentState"] = string(job.CurrentState)
	job.Result = map[string]interface{}{
		"scope":      "one_hop_downstream",
		"status":     "failed",
		"error":      runErr.Error(),
		"durationMs": time.Since(startedAt).Milliseconds(),
	}
	_ = s.scoutingJobsRepo.Update(ctx, job)
	_ = s.publishLifecycleEvent(ctx, "horizon.supply_chain.one_hop.failed", firstNonEmpty(strings.TrimSpace(job.TenantID), "global"), map[string]interface{}{
		"tenantId":   firstNonEmpty(strings.TrimSpace(job.TenantID), "global"),
		"projectId":  strings.TrimSpace(job.ProjectID),
		"lensId":     strings.TrimSpace(job.LensID),
		"jobId":      strings.TrimSpace(job.ID),
		"anchorRef":  stringState(job.State, "anchorRef"),
		"anchorType": stringState(job.State, "anchorType"),
		"scope":      "one_hop_downstream",
		"status":     "failed",
		"error":      runErr.Error(),
		"durationMs": time.Since(startedAt).Milliseconds(),
	})
	return runErr
}

func (s *OneHopDownstreamService) publishLifecycleEvent(ctx context.Context, eventType string, tenantID string, payload interface{}) error {
	if s == nil || s.publisher == nil {
		return nil
	}
	return s.publisher(ctx, eventType, firstNonEmpty(strings.TrimSpace(tenantID), "global"), payload)
}

func (s *OneHopDownstreamService) publishNodeUpsert(ctx context.Context, tenantID string, fragment *horizonmodels.HorizonFragment, operation string) error {
	if s == nil || s.publisher == nil || fragment == nil {
		return nil
	}
	payload := map[string]interface{}{
		"tenantId":      firstNonEmpty(strings.TrimSpace(tenantID), "global"),
		"fragmentId":    strings.TrimSpace(fragment.ID),
		"canonicalPath": strings.TrimSpace(fragment.CanonicalPath),
		"type":          string(fragment.Type),
		"label":         fragmentLabel(fragment),
		"operation":     strings.TrimSpace(operation),
		"updatedAt":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	return s.publisher(ctx, "horizon.graph.node.upserted", firstNonEmpty(strings.TrimSpace(tenantID), "global"), payload)
}

func (s *OneHopDownstreamService) publishEdgeUpsert(ctx context.Context, tenantID string, edge *horizonmodels.DependencyEdge) error {
	if s == nil || s.publisher == nil || edge == nil {
		return nil
	}
	src := ""
	dst := ""
	if s.fragmentsRepo != nil {
		if frag, err := s.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.SourceFragmentID)); err == nil && frag != nil {
			src = strings.TrimSpace(frag.CanonicalPath)
		}
		if frag, err := s.fragmentsRepo.Get(ctx, strings.TrimSpace(edge.TargetFragmentID)); err == nil && frag != nil {
			dst = strings.TrimSpace(frag.CanonicalPath)
		}
	}
	payload := map[string]interface{}{
		"tenantId":            firstNonEmpty(strings.TrimSpace(tenantID), "global"),
		"edgeId":              strings.TrimSpace(edge.ID),
		"sourceFragmentId":    strings.TrimSpace(edge.SourceFragmentID),
		"targetFragmentId":    strings.TrimSpace(edge.TargetFragmentID),
		"sourceCanonicalPath": src,
		"targetCanonicalPath": dst,
		"type":                string(edge.Type),
		"qualifiers":          cloneMap(edge.Qualifiers),
		"properties":          edge.Properties,
		"ts":                  time.Now().UTC().Format(time.RFC3339Nano),
	}
	return s.publisher(ctx, "horizon.graph.edge.upserted", firstNonEmpty(strings.TrimSpace(tenantID), "global"), payload)
}

func loadOptionsFromJob(job *horizonmodels.ScoutingJob) OneHopDownstreamOptions {
	opts := OneHopDownstreamOptions{
		AnchorRef:             strings.TrimSpace(job.TargetCanonicalPath),
		AnchorType:            stringState(job.State, "anchorType"),
		MaxBranchesPerProduct: intState(job.State, "maxBranchesPerProduct", defaultMaxBranchesPerProduct),
		IncludeInputsContext:  boolState(job.State, "includeInputsContext", true),
		AllowOrgFallback:      boolState(job.State, "allowOrgFallback", true),
		AllowLLMFallback:      boolState(job.State, "allowLLMFallback", true),
	}
	if v := stringState(job.State, "anchorRef"); v != "" {
		opts.AnchorRef = v
	}
	return opts
}

func summarizeFragments(fragments []*horizonmodels.HorizonFragment, limit int) []map[string]string {
	if limit <= 0 {
		limit = len(fragments)
	}
	out := make([]map[string]string, 0, minInt(len(fragments), limit))
	for _, fragment := range fragments {
		if fragment == nil || strings.TrimSpace(fragment.CanonicalPath) == "" {
			continue
		}
		out = append(out, map[string]string{
			"canonicalPath": strings.TrimSpace(fragment.CanonicalPath),
			"type":          string(fragment.Type),
			"label":         fragmentLabel(fragment),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func summarizeProductEdges(edges []*horizonmodels.DependencyEdge) []map[string]string {
	out := make([]map[string]string, 0, len(edges))
	for _, edge := range edges {
		if edge == nil {
			continue
		}
		out = append(out, map[string]string{
			"sourceFragmentId": strings.TrimSpace(edge.SourceFragmentID),
			"targetFragmentId": strings.TrimSpace(edge.TargetFragmentID),
			"type":             string(edge.Type),
		})
	}
	if len(out) > maxPromptProducts {
		out = out[:maxPromptProducts]
	}
	return out
}

func summarizeArtifacts(artifacts []*horizonmodels.MaterializedArtifact) []map[string]string {
	out := make([]map[string]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		out = append(out, map[string]string{
			"kind":         string(artifact.Kind),
			"canonicalKey": strings.TrimSpace(artifact.CanonicalKey),
			"displayName":  strings.TrimSpace(artifact.DisplayName),
			"productAxis":  strings.TrimSpace(artifact.ProductAxis),
			"pathStatus":   strings.TrimSpace(artifact.PathStatus),
		})
	}
	return out
}

func collectFragmentsByIDs(items map[string]*horizonmodels.HorizonFragment, orderedIDs []string, fragmentType horizonmodels.FragmentType, limit int) []*horizonmodels.HorizonFragment {
	seen := map[string]struct{}{}
	out := make([]*horizonmodels.HorizonFragment, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		fragment := items[strings.TrimSpace(id)]
		if fragment == nil || fragment.Type != fragmentType {
			continue
		}
		cp := strings.TrimSpace(fragment.CanonicalPath)
		if cp == "" {
			continue
		}
		if _, ok := seen[cp]; ok {
			continue
		}
		seen[cp] = struct{}{}
		out = append(out, fragment)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return sortFragmentsByPath(out)
}

func collectActorFragmentsByIDs(items map[string]*horizonmodels.HorizonFragment, orderedIDs []string, limit int) []*horizonmodels.HorizonFragment {
	seen := map[string]struct{}{}
	out := make([]*horizonmodels.HorizonFragment, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		fragment := items[strings.TrimSpace(id)]
		if fragment == nil {
			continue
		}
		if fragment.Type != horizonmodels.FragmentTypeOrganization && fragment.Type != horizonmodels.FragmentTypeFacility {
			continue
		}
		cp := strings.TrimSpace(fragment.CanonicalPath)
		if cp == "" {
			continue
		}
		if _, ok := seen[cp]; ok {
			continue
		}
		seen[cp] = struct{}{}
		out = append(out, fragment)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return sortFragmentsByPath(out)
}

func sortFragmentsByPath(in []*horizonmodels.HorizonFragment) []*horizonmodels.HorizonFragment {
	sort.SliceStable(in, func(i, j int) bool {
		return strings.TrimSpace(in[i].CanonicalPath) < strings.TrimSpace(in[j].CanonicalPath)
	})
	return in
}

func mapValuesFragments(in map[string]*horizonmodels.HorizonFragment) []*horizonmodels.HorizonFragment {
	out := make([]*horizonmodels.HorizonFragment, 0, len(in))
	for _, fragment := range in {
		if fragment != nil {
			out = append(out, fragment)
		}
	}
	return out
}

func isSupportedAnchorType(ft horizonmodels.FragmentType) bool {
	return ft == horizonmodels.FragmentTypeOrganization || ft == horizonmodels.FragmentTypeFacility
}

func normalizeActorType(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "ORG", string(horizonmodels.FragmentTypeOrganization):
		return "ORG"
	case "FAC", string(horizonmodels.FragmentTypeFacility):
		return "FAC"
	default:
		return ""
	}
}

func normalizeGroundingMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "graph":
		return "graph"
	case "search":
		return "search"
	case "mixed":
		return "mixed"
	case "llm_fallback":
		return "llm_fallback"
	default:
		return "llm_fallback"
	}
}

func normalizeStepKind(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "pass_through", "pass-through", "passthrough":
		return "pass_through"
	case "transformation", "transform":
		return "transformation"
	case "assembly", "assemble", "assembled_into":
		return "assembly"
	case "usage", "use", "used in", "used_in", "used-in":
		return "usage"
	default:
		return ""
	}
}

func edgeTypeForStepKind(stepKind string) horizonmodels.DependencyType {
	switch strings.TrimSpace(stepKind) {
	case "assembly":
		return horizonmodels.DependencyTypeAssemblesInto
	case "usage":
		return horizonmodels.DependencyTypeUsedIn
	default:
		return horizonmodels.DependencyTypeTransformsInto
	}
}

func relationKindForStepKind(stepKind string) string {
	switch strings.TrimSpace(stepKind) {
	case "assembly":
		return "assembly"
	case "usage":
		return "usage"
	case "transformation":
		return "transformation"
	default:
		return ""
	}
}

func buildStepDedupKey(step oneHopStep) string {
	return strings.Join([]string{
		strings.TrimSpace(step.SourceActorRef),
		strings.TrimSpace(step.SourceProductRef),
		strings.TrimSpace(step.StepKind),
		strings.TrimSpace(step.TargetProductRef),
		strings.TrimSpace(step.TargetActorRef),
	}, "|")
}

func pathLabel(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 {
		return path
	}
	slug := strings.ReplaceAll(parts[len(parts)-1], "-", " ")
	slug = strings.ReplaceAll(slug, "_", " ")
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return path
	}
	words := strings.Fields(slug)
	for i := range words {
		if len(words[i]) > 0 {
			words[i] = strings.ToUpper(words[i][:1]) + words[i][1:]
		}
	}
	return strings.Join(words, " ")
}

func fragmentLabel(fragment *horizonmodels.HorizonFragment) string {
	if fragment == nil {
		return ""
	}
	if v := strings.TrimSpace(fragment.Labels["en"]); v != "" {
		return v
	}
	return pathLabel(fragment.CanonicalPath)
}

func compactUniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func interfaceStringSlice(v interface{}) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out
	default:
		return nil
	}
}

func stringState(state map[string]interface{}, key string) string {
	if state == nil {
		return ""
	}
	if value, ok := state[key].(string); ok {
		return strings.TrimSpace(value)
	}
	if value, ok := state[key]; ok && value != nil {
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
	return ""
}

func intState(state map[string]interface{}, key string, fallback int) int {
	if state == nil {
		return fallback
	}
	switch value := state[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return fallback
	}
}

func boolState(state map[string]interface{}, key string, fallback bool) bool {
	if state == nil {
		return fallback
	}
	switch value := state[key].(type) {
	case bool:
		return value
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return fallback
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	if v == 0 {
		return 0.7
	}
	return v
}

func floatOrDefault(v interface{}, fallback float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return fallback
	}
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func containsAny(text string, needles ...string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	for _, needle := range needles {
		needle = strings.ToLower(strings.TrimSpace(needle))
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func sortedKeysInterface[K ~string, V any](m map[K]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, string(key))
	}
	sort.Strings(out)
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
