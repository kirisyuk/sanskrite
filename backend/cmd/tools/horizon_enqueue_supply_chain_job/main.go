package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/firestore"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	horizonagents "github.com/igorkirisyuk/sanskrite/internal/horizon/scout/agents"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout/pipeline"
	horizontasks "github.com/igorkirisyuk/sanskrite/internal/horizon/tasks"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	ctx := context.Background()

	projectID := flag.String("project", "etigram", "GCP project id")
	tenantID := flag.String("tenant", "global", "tenant id")
	jobProjectID := flag.String("job-project", "codex-supply-chain-assembly", "job project id")
	targetDomain := flag.String("domain", "/domain/v1/supply-chain/geo/cn/product/smartphone", "target domain canonical path")
	anchor := flag.String("anchor", "/product/device/electronics/smartphone", "primary anchor canonical path")
	anchorType := flag.String("anchor-type", "product", "anchor type: org|product|deposit")
	direction := flag.String("direction", "both", "query direction")
	depth := flag.Int("depth", 4, "query depth")
	graphBuildMode := flag.String("graph-build-mode", horizonagents.SupplyChainGraphBuildModeHotPathPlusGraph, "supply-chain graph build mode: hot_path_only|hot_path_plus_graph")
	productFilter := flag.String("product-filter", "smartphone,battery-pack,application-processor,display-module", "comma-separated product filters")
	geoFilter := flag.String("geo-filter", "cn,tw,kr,jp", "comma-separated geo filters")
	queueLocation := flag.String("queue-location", "us-central1", "Cloud Tasks location")
	queueName := flag.String("queue", "etigram-scout", "Cloud Tasks queue name")
	targetURL := flag.String("target-url", "https://etigram-api-onq73n4gpa-uc.a.run.app/api/v1/internal/tasks/scout/run", "Cloud Tasks target URL")
	oidcServiceAccount := flag.String("oidc-service-account", "backend@etigram.iam.gserviceaccount.com", "OIDC service account email")
	oidcAudience := flag.String("oidc-audience", "https://etigram-api-onq73n4gpa-uc.a.run.app", "OIDC audience")
	flag.Parse()

	fsClient, err := firestore.NewClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore init failed: %v", err)
	}
	defer fsClient.Close()

	agentJobsRepo := corerepo.NewAgentJobsRepository(fsClient)
	scoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
	dispatcher, err := horizontasks.NewScoutCloudTasksDispatcher(
		ctx,
		strings.TrimSpace(*projectID),
		strings.TrimSpace(*queueLocation),
		strings.TrimSpace(*queueName),
		strings.TrimSpace(*targetURL),
		strings.TrimSpace(*oidcServiceAccount),
		strings.TrimSpace(*oidcAudience),
	)
	if err != nil {
		log.Fatalf("cloud tasks dispatcher init failed: %v", err)
	}
	defer dispatcher.Close()

	now := time.Now().UTC()
	target := strings.TrimSpace(*targetDomain)
	anchorValue := strings.TrimSpace(*anchor)
	mode := strings.TrimSpace(*graphBuildMode)
	if mode == "" {
		mode = horizonagents.SupplyChainGraphBuildModeHotPathPlusGraph
	}
	products := splitCSV(*productFilter)
	geos := splitCSV(*geoFilter)
	executionPlan := horizonscout.BuildExecutionPlanContract(nil, []string{"supply_chain"}, nil)

	job := &horizonmodels.ScoutingJob{
		AgentJob: coremodels.AgentJob{
			TenantID:  strings.TrimSpace(*tenantID),
			ProjectID: strings.TrimSpace(*jobProjectID),
			AgentID:   "scout",
			Status:    coremodels.AgentJobStatusPending,
			CreatedAt: now,
			UpdatedAt: now,
			State: map[string]interface{}{
				"targetCanonicalPath":         target,
				"currentState":                string(horizonmodels.ScoutingJobStatePending),
				"scoutingStrategy":            string(horizonmodels.ScoutingStrategySnapshot),
				"domainModes":                 []string{"supply_chain"},
				"supplyChainPlanMode":         true,
				"supplyChainGraphBuildMode":   mode,
				pipeline.StateKeyPipelinePass: string(pipeline.PassBaseEnrichment),
				"domains": []horizonagents.DomainResolution{
					{
						CanonicalPath: target,
						ContractType:  "supply-chain",
						Title:         "Codex supply chain assembly verification",
						Description:   "Run a fresh supply-chain domain job and verify final assembly graph materialization.",
						Facets: map[string][]string{
							"product": products,
							"geo":     geos,
						},
						Confidence: 1,
						Reason:     "manual verification of supply chain final assembly pass",
						SupplyChainExecutionPlan: &horizonagents.SupplyChainExecutionPlan{
							GraphBuildMode: mode,
							Queries: []horizonagents.SupplyChainQuery{
								{
									Anchor:        anchorValue,
									AnchorType:    strings.TrimSpace(*anchorType),
									Direction:     strings.TrimSpace(*direction),
									Depth:         *depth,
									ProductFilter: products,
									GeoFilter:     geos,
									Reason:        "materialize multi-branch supply chain around final product anchor",
								},
							},
						},
					},
				},
			},
		},
		TargetCanonicalPath: target,
		ScoutingStrategy:    string(horizonmodels.ScoutingStrategySnapshot),
		BillingContext: horizonmodels.BillingContext{
			ChargeModel: string(horizonmodels.ChargeModelBounty),
			ProjectID:   strings.TrimSpace(*jobProjectID),
		},
	}
	horizonscout.ApplyExecutionPlanToState(job.State, executionPlan)

	jobID, err := scoutingJobsRepo.Create(ctx, job)
	if err != nil {
		log.Fatalf("create scouting job failed: %v", err)
	}
	if err := dispatcher.EnqueueScoutJob(ctx, jobID); err != nil {
		log.Fatalf("enqueue scout job failed: %v", err)
	}

	fmt.Printf("job_id=%s\n", jobID)
	fmt.Printf("target=%s\n", target)
	fmt.Printf("anchor=%s\n", anchorValue)
	fmt.Printf("graph_build_mode=%s\n", mode)
	fmt.Printf("products=%v\n", products)
	fmt.Printf("geos=%v\n", geos)
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}
