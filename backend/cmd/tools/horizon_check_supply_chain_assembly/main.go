package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/gcp"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonrepo "github.com/igorkirisyuk/sanskrite/internal/horizon/repo"
	horizonscout "github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	corerepo "github.com/igorkirisyuk/sanskrite/internal/repo"
)

func main() {
	ctx := context.Background()
	projectID := flag.String("project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	tenantID := flag.String("tenant", "", "tenant id; defaults to job tenant or global")
	jobID := flag.String("job", "", "scouting job id to inspect")
	artifactKey := flag.String("artifact-key", "", "assembly artifact canonical key")
	jsonOut := flag.Bool("json", false, "print JSON report")
	showPaths := flag.Bool("show-paths", false, "include derived path keys in text output")
	requireDerivedPaths := flag.Bool("require-derived-paths", true, "fail when derivedPathKeys is empty")
	failOnMissingDerivedPaths := flag.Bool("fail-on-missing-derived-paths", true, "fail when declared derived path artifacts are missing")
	requireAssemblyEdges := flag.Bool("require-assembly-edges", false, "fail when assemblyEdges is empty")
	minAnchorFanin := flag.Int("min-anchor-fanin", 1, "minimum unique incoming upstream products per anchor")
	flag.Parse()

	if strings.TrimSpace(*jobID) == "" && strings.TrimSpace(*artifactKey) == "" {
		log.Fatal("either -job or -artifact-key is required")
	}

	fs, err := gcp.NewFirestoreClient(ctx, strings.TrimSpace(*projectID))
	if err != nil {
		log.Fatalf("firestore client init failed: %v", err)
	}
	defer fs.Close()

	agentJobsRepo := corerepo.NewAgentJobsRepository(fs.Client())
	scoutingJobsRepo := horizonrepo.NewScoutingJobsRepository(agentJobsRepo)
	artifactsRepo := horizonrepo.NewMaterializedArtifactsRepository(fs.Client())

	var (
		job                 *horizonmodels.ScoutingJob
		jobSummary          map[string]interface{}
		resolvedTenantID    string
		resolvedArtifactKey string
	)
	if strings.TrimSpace(*jobID) != "" {
		job, err = scoutingJobsRepo.Get(ctx, strings.TrimSpace(*jobID))
		if err != nil {
			log.Fatalf("load scouting job failed: %v", err)
		}
		resolvedTenantID = strings.TrimSpace(job.TenantID)
		if summary, ok := job.State["supplyChainAssembly"].(map[string]interface{}); ok && summary != nil {
			jobSummary = summary
			if key := strings.TrimSpace(fmt.Sprintf("%v", summary["artifactKey"])); key != "" && key != "<nil>" {
				resolvedArtifactKey = key
			}
		}
	}
	if strings.TrimSpace(*tenantID) != "" {
		resolvedTenantID = strings.TrimSpace(*tenantID)
	}
	if resolvedTenantID == "" {
		resolvedTenantID = "global"
	}
	if strings.TrimSpace(*artifactKey) != "" {
		resolvedArtifactKey = strings.TrimSpace(*artifactKey)
	}
	if resolvedArtifactKey == "" {
		if job != nil && job.State != nil && hasRichSupplyChainState(job.State) {
			log.Fatal("assembly artifact key is empty while chainContext/supplyChainMaterialization already contains rich state; this indicates a supply-chain assembly bug")
		}
		log.Fatal("assembly artifact key is empty; pass -artifact-key or provide a job with state.supplyChainAssembly.artifactKey")
	}

	artifact, err := loadAssemblyArtifact(ctx, artifactsRepo, resolvedTenantID, resolvedArtifactKey)
	if err != nil {
		log.Fatalf("load assembly artifact failed: %v", err)
	}

	parsed, err := horizonmodels.ParseSupplyChainAssemblyGraphPayload(artifact.Payload)
	if err != nil {
		log.Fatalf("parse assembly artifact payload failed: %v", err)
	}
	pathArtifacts, missingLookups := loadDerivedPathArtifacts(ctx, artifactsRepo, resolvedTenantID, parsed.DerivedPathKeys)

	report, err := horizonscout.DiagnoseSupplyChainAssembly(
		artifact,
		pathArtifacts,
		jobSummary,
		horizonscout.SupplyChainAssemblyDiagnosticOptions{
			RequireDerivedPaths:       *requireDerivedPaths,
			FailOnMissingDerivedPaths: *failOnMissingDerivedPaths,
			RequireAssemblyEdges:      *requireAssemblyEdges,
			MinAnchorFanin:            *minAnchorFanin,
		},
	)
	if err != nil {
		log.Fatalf("diagnostic failed: %v", err)
	}
	if len(missingLookups) > 0 {
		report.Warnings = append(report.Warnings, fmt.Sprintf("lookup_missing=%v", missingLookups))
	}

	if *jsonOut {
		out := map[string]interface{}{
			"jobId":  strings.TrimSpace(*jobID),
			"report": report,
		}
		if jobSummary != nil {
			out["jobSummary"] = jobSummary
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			log.Fatalf("marshal json failed: %v", err)
		}
		fmt.Println(string(data))
	} else {
		printTextReport(strings.TrimSpace(*jobID), report, parsed.DerivedPathKeys, *showPaths)
	}

	if !report.Pass {
		os.Exit(1)
	}
}

func loadAssemblyArtifact(
	ctx context.Context,
	artifactsRepo *horizonrepo.MaterializedArtifactsRepository,
	tenantID string,
	canonicalKey string,
) (*horizonmodels.MaterializedArtifact, error) {
	tenants := []string{strings.TrimSpace(tenantID)}
	if tenantID != "global" {
		tenants = append(tenants, "global")
	}
	for _, candidateTenant := range tenants {
		if candidateTenant == "" {
			continue
		}
		artifact, err := artifactsRepo.GetByCanonicalKey(ctx, candidateTenant, horizonmodels.MaterializedKindSupplyChainAssemblyGraph, canonicalKey)
		if err == nil && artifact != nil {
			return artifact, nil
		}
		if err != nil && !errors.Is(err, horizonrepo.ErrMaterializedArtifactNotFound) {
			return nil, err
		}
	}
	if artifact, err := artifactsRepo.GetByCanonicalKeyAnyKind(ctx, strings.TrimSpace(tenantID), canonicalKey); err == nil && artifact != nil {
		return nil, fmt.Errorf("artifact %s exists with kind=%q, not %q", canonicalKey, artifact.Kind, horizonmodels.MaterializedKindSupplyChainAssemblyGraph)
	}
	return nil, fmt.Errorf("assembly artifact not found: tenant=%s key=%s", tenantID, canonicalKey)
}

func loadDerivedPathArtifacts(
	ctx context.Context,
	artifactsRepo *horizonrepo.MaterializedArtifactsRepository,
	tenantID string,
	keys []string,
) ([]*horizonmodels.MaterializedArtifact, []string) {
	artifacts := make([]*horizonmodels.MaterializedArtifact, 0, len(keys))
	missing := make([]string, 0)
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		var found *horizonmodels.MaterializedArtifact
		for _, candidateTenant := range []string{strings.TrimSpace(tenantID), "global"} {
			if candidateTenant == "" {
				continue
			}
			artifact, err := artifactsRepo.GetByCanonicalKey(ctx, candidateTenant, horizonmodels.MaterializedKindSupplyChainPath, key)
			if err == nil && artifact != nil {
				found = artifact
				break
			}
			if err != nil && !errors.Is(err, horizonrepo.ErrMaterializedArtifactNotFound) {
				missing = append(missing, fmt.Sprintf("%s(err=%v)", key, err))
				break
			}
		}
		if found != nil {
			artifacts = append(artifacts, found)
			continue
		}
		missing = append(missing, key)
	}
	sort.Strings(missing)
	return artifacts, missing
}

func printTextReport(jobID string, report *horizonscout.SupplyChainAssemblyDiagnosticReport, derivedPathKeys []string, showPaths bool) {
	fmt.Printf("pass=%t job=%s tenant=%s\n", report.Pass, firstNonEmpty(jobID, "-"), firstNonEmpty(report.TenantID, "-"))
	fmt.Printf("artifact=%s id=%s status=%s summary_status=%s stage=%s provisional=%t source_mode=%s\n", firstNonEmpty(report.ArtifactKey, "-"), firstNonEmpty(report.ArtifactID, "-"), firstNonEmpty(report.PathStatus, "-"), firstNonEmpty(report.SummaryStatus, "-"), firstNonEmpty(report.Stage, "-"), report.Provisional, firstNonEmpty(report.SourceMode, "-"))
	fmt.Printf("anchors=%v resolved=%v\n", report.AnchorSet, report.ResolvedAnchors)
	fmt.Printf("counts products=%d actors=%d supply_edges=%d transforms=%d assembly_edges=%d routes=%d derived_declared=%d derived_found=%d\n",
		report.ProductNodesN, report.ActorNodesN, report.SupplyEdgesN, report.TransformEdgesN, report.AssemblyEdgesN, report.RouteBindingsN, report.DerivedPathKeysN, report.DerivedPathArtifactsN)
	if len(report.AnchorFanin) > 0 {
		keys := make([]string, 0, len(report.AnchorFanin))
		for key := range report.AnchorFanin {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Printf("anchor_fanin[%s]=%d\n", key, report.AnchorFanin[key])
		}
	}
	if len(report.OpenGaps) > 0 {
		fmt.Printf("open_gaps=%v\n", report.OpenGaps)
	}
	if showPaths && len(derivedPathKeys) > 0 {
		fmt.Printf("derived_paths=%v\n", derivedPathKeys)
	}
	if len(report.MissingDerivedPathKeys) > 0 {
		fmt.Printf("missing_derived_paths=%v\n", report.MissingDerivedPathKeys)
	}
	if len(report.SummaryMismatches) > 0 {
		fmt.Printf("summary_mismatches=%v\n", report.SummaryMismatches)
	}
	if len(report.Warnings) > 0 {
		fmt.Printf("warnings=%v\n", report.Warnings)
	}
	if len(report.Errors) > 0 {
		fmt.Printf("errors=%v\n", report.Errors)
	}
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

func hasRichSupplyChainState(state map[string]interface{}) bool {
	if state == nil {
		return false
	}
	if summary, ok := state["supplyChainSummary"].(map[string]interface{}); ok && summary != nil {
		if parseInt(summary["discoveredNodes"]) > 0 || parseInt(summary["materializedFragments"]) > 0 || parseInt(summary["createdEdges"]) > 0 {
			return true
		}
	}
	if materialization, ok := state["supplyChainMaterialization"].(map[string]interface{}); ok && materialization != nil {
		if parseInt(materialization["materializedFragments"]) > 0 || parseInt(materialization["materializedEdges"]) > 0 || parseInt(materialization["materializedPaths"]) > 0 {
			return true
		}
	}
	if contextRaw := strings.TrimSpace(fmt.Sprint(state["chainContext"])); contextRaw != "" && contextRaw != "<nil>" && contextRaw != "{}" {
		return true
	}
	return false
}

func parseInt(raw interface{}) int {
	switch value := raw.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float32:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}
