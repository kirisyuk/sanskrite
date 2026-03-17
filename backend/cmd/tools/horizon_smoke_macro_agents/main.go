package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

func main() {
	opts := parseOptions()
	ctx := context.Background()

	runtime, err := newToolRuntime(ctx, opts)
	if err != nil {
		log.Fatalf("runtime init failed: %v", err)
	}
	defer runtime.close()

	var report *E2EReport
	switch opts.Mode {
	case modeLegacyTrigger:
		report, err = runtime.runLegacyTrigger(ctx, opts)
	case modeLegacyCoverageGap:
		report, err = runtime.runLegacyCoverageGap(ctx, opts)
	case modeGroundedRun:
		report, err = runtime.runGrounded(ctx, opts)
	case modeAnalyzeChain:
		report, err = runtime.analyzeChain(ctx, opts)
	case modeReplayReport:
		report, err = runtime.replayReport(ctx, opts)
	case modeLensE2E:
		report, err = runtime.runLensE2E(ctx, opts)
	case modeFullE2E:
		report, err = runtime.runFullE2E(ctx, opts)
	case modeScanCandidates:
		report, err = runtime.scanCandidates(ctx, opts)
	default:
		log.Fatalf("unsupported mode %q", opts.Mode)
	}
	if err != nil {
		log.Fatal(err)
	}

	renderTextReport(report)
	if strings.TrimSpace(opts.OutPath) != "" {
		if err := writeJSONReport(opts.OutPath, report); err != nil {
			log.Fatalf("write report failed: %v", err)
		}
	}
	if !opts.TextOnly {
		printJSON(report)
	}
}

func parseOptions() toolOptions {
	var opts toolOptions
	flag.StringVar(&opts.ProjectID, "project", firstNonEmpty(os.Getenv("GCP_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), "etigram"), "GCP project id")
	flag.StringVar(&opts.TenantID, "tenant", "global", "tenant id")
	flag.StringVar(&opts.CanonicalPath, "canonical-path", "/org/us/tesla", "anchor canonical path")
	flag.StringVar(&opts.CanonicalContains, "canonical-contains", "", "substring filter for canonical paths when scanning candidates")
	flag.StringVar(&opts.SubjectID, "subject-id", "", "watch subject id")
	flag.StringVar(&opts.LensID, "lens-id", "", "lens id")
	flag.StringVar(&opts.RunID, "run-id", "", "scope agent run id for replay/analyze")
	flag.StringVar(&opts.ProposalID, "proposal-id", "", "agent proposal id for replay/analyze")
	flag.StringVar(&opts.FactID, "fact-id", "", "accepted signal fact id for replay/analyze")
	flag.StringVar(&opts.AxisPrefix, "axis-prefix", "", "signal axis prefix filter for scan/analyze")
	flag.StringVar(&opts.Mode, "mode", modeFullE2E, "mode: full-e2e|grounded-run|analyze-chain|lens-e2e|replay-report|scan-candidates|trigger|coverage-gap")
	flag.StringVar(&opts.TriggerReason, "reason", "macro agent grounded smoke", "trigger/proposal reason")
	flag.StringVar(&opts.QueryOverride, "query-override", "", "extra search-grounding query override")
	flag.StringVar(&opts.OutPath, "out", "", "optional JSON report output path")
	flag.IntVar(&opts.LookbackDays, "lookback-days", 0, "override search grounding lookback window in days")
	flag.IntVar(&opts.Limit, "limit", 8, "batch/list limit")
	flag.IntVar(&opts.TopEndpoints, "top-endpoints", 5, "number of propagated endpoints to analyze")
	flag.IntVar(&opts.TopInterpretations, "top-interpretations", 3, "number of endpoint interpretations to build")
	flag.BoolVar(&opts.RequireMaterialChange, "require-material-change", true, "fail if grounding produced no material accepted output")
	flag.BoolVar(&opts.PersistPropagation, "persist-propagation", false, "persist propagation instead of simulation")
	flag.BoolVar(&opts.EnablePropagationTrace, "enable-propagation-trace", false, "enable raw propagation trace collection during analysis")
	flag.IntVar(&opts.PropagationTraceSteps, "propagation-trace-steps", 128, "max raw propagation trace steps to capture when trace is enabled")
	flag.BoolVar(&opts.TextOnly, "text-only", false, "skip JSON stdout and print only text summary")
	flag.IntVar(&opts.ScanFacts, "scan-facts", 80, "number of recent signal facts to scan in scan-candidates mode")
	flag.Parse()

	opts.ProjectID = strings.TrimSpace(opts.ProjectID)
	opts.TenantID = strings.TrimSpace(firstNonEmpty(opts.TenantID, "global"))
	opts.CanonicalPath = strings.TrimSpace(opts.CanonicalPath)
	opts.CanonicalContains = strings.TrimSpace(opts.CanonicalContains)
	opts.SubjectID = strings.TrimSpace(opts.SubjectID)
	opts.LensID = strings.TrimSpace(opts.LensID)
	opts.RunID = strings.TrimSpace(opts.RunID)
	opts.ProposalID = strings.TrimSpace(opts.ProposalID)
	opts.FactID = strings.TrimSpace(opts.FactID)
	opts.AxisPrefix = strings.TrimSpace(opts.AxisPrefix)
	opts.Mode = strings.TrimSpace(firstNonEmpty(opts.Mode, modeFullE2E))
	opts.TriggerReason = strings.TrimSpace(firstNonEmpty(opts.TriggerReason, "macro agent grounded smoke"))
	opts.QueryOverride = strings.TrimSpace(opts.QueryOverride)
	opts.OutPath = strings.TrimSpace(opts.OutPath)
	if opts.Limit <= 0 {
		opts.Limit = 8
	}
	if opts.TopEndpoints <= 0 {
		opts.TopEndpoints = 5
	}
	if opts.TopInterpretations < 0 {
		opts.TopInterpretations = 0
	}
	if opts.TopInterpretations == 0 && opts.Mode != modeScanCandidates {
		// allow explicit skip only for candidate scanning; other modes default to interpreted reports
		opts.TopInterpretations = 3
	}
	if opts.TopInterpretations <= 0 && opts.Mode != modeScanCandidates {
		opts.TopInterpretations = 3
	}
	if opts.ScanFacts <= 0 {
		opts.ScanFacts = 80
	}
	if opts.PropagationTraceSteps <= 0 {
		opts.PropagationTraceSteps = 128
	}
	return opts
}

func writeJSONReport(path string, report *E2EReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		log.Fatal(err)
	}
}

func renderTextReport(report *E2EReport) {
	if report == nil {
		return
	}
	fmt.Println()
	fmt.Println("=== Macro Agent Grounded E2E ===")
	verdictStatus := "n/a"
	if report.Verdict != nil && strings.TrimSpace(report.Verdict.Status) != "" {
		verdictStatus = report.Verdict.Status
	}
	fmt.Printf("Mode: %s | Verdict: %s\n", report.Mode, verdictStatus)
	if report.Subject != nil {
		fmt.Printf("Subject: %s (%s) %s\n", report.Subject.DisplayLabel, report.Subject.SubjectKind, report.Subject.AnchorCanonicalPath)
	}
	if report.Runtime != nil {
		fmt.Printf("Grounded runs: %d | Proposals: %d | Accepted signals: %d\n", len(report.Runs), len(report.Proposals), len(report.AcceptedSignals))
		if report.Runtime.NoopReason != "" {
			fmt.Printf("Runtime note: %s\n", report.Runtime.NoopReason)
		}
	}
	if report.Grounding != nil {
		fmt.Printf("Grounding: model=%s citations=%d webUrls=%d datastoreHits=%d\n",
			report.Grounding.Model,
			report.Grounding.CitationsCount,
			len(report.Grounding.WebURLs),
			len(report.Grounding.DatastoreFragmentIDs),
		)
	}
	fmt.Println("Checks:")
	if len(report.Criteria) == 0 {
		fmt.Println("- not evaluated in this mode")
	} else {
		for _, stage := range report.Criteria {
			if stage == nil {
				continue
			}
			fmt.Printf("- %s: %s", stage.Name, stage.Status)
			if stage.Reason != "" {
				fmt.Printf(" (%s)", stage.Reason)
			}
			fmt.Println()
		}
	}
	if report.Verdict != nil && report.Verdict.Summary != "" {
		fmt.Println("Summary:")
		fmt.Println(report.Verdict.Summary)
	}
	fmt.Println()
	fmt.Println("1. What appeared:")
	if len(report.Proposals) > 0 {
		for _, item := range report.Proposals[:minInt(len(report.Proposals), 3)] {
			if item == nil {
				continue
			}
			fmt.Printf("- %s | %s | confidence=%.2f\n", item.ProposalType, firstNonEmpty(stringValue(item.Payload["title"]), item.Explanation, item.SignalKey), item.Confidence)
		}
	} else if len(report.AcceptedSignals) > 0 {
		for _, item := range report.AcceptedSignals[:minInt(len(report.AcceptedSignals), 3)] {
			if item == nil || item.Fact == nil {
				continue
			}
			fmt.Printf("- signal fact | %s | confidence=%.2f\n", firstNonEmpty(item.Fact.Title, item.Fact.Type), item.Fact.Confidence)
		}
	} else {
		fmt.Println("- nothing materialized")
	}

	fmt.Println("2. Signal layer:")
	if len(report.AcceptedSignals) > 0 {
		for _, item := range report.AcceptedSignals[:minInt(len(report.AcceptedSignals), 3)] {
			if item == nil || item.Signal == nil {
				continue
			}
			fmt.Printf("- %s:%s | amplitude=%.2f | seeds=%d\n", item.Signal.Polarity, item.Signal.Axis, item.Signal.Amplitude, len(item.SeedFragmentIDs))
		}
	} else {
		fmt.Println("- no accepted signal facts")
	}

	fmt.Println("3. Where it reached:")
	if report.PropagationAnalysis != nil && len(report.PropagationAnalysis.Signals) > 0 {
		primary := report.PropagationAnalysis.Signals[0]
		if primary != nil && len(primary.TopEndpoints) > 0 {
			for _, node := range primary.TopEndpoints[:minInt(len(primary.TopEndpoints), 3)] {
				fmt.Printf("- %s | hops=%d | amplitude=%.2f\n", firstNonEmpty(node.CanonicalPath, node.FragmentID), node.Hops, node.TotalAmplitude)
			}
		} else {
			fmt.Println("- signal stayed on seed node")
		}
	} else {
		fmt.Println("- no propagation analysis")
	}

	fmt.Println("4. Why start/end make sense:")
	if report.InterpretationAnalysis != nil && report.InterpretationAnalysis.SeedInterpretation != nil && report.InterpretationAnalysis.SeedInterpretation.Interpretation != nil {
		fmt.Printf("- seed: %s\n", report.InterpretationAnalysis.SeedInterpretation.Interpretation.Summary)
	} else {
		fmt.Println("- seed interpretation missing")
	}
	if report.InterpretationAnalysis != nil && len(report.InterpretationAnalysis.EndpointInterpretations) > 0 {
		for _, item := range report.InterpretationAnalysis.EndpointInterpretations[:minInt(len(report.InterpretationAnalysis.EndpointInterpretations), 2)] {
			if item == nil || item.Interpretation == nil {
				continue
			}
			fmt.Printf("- endpoint: %s\n", item.Interpretation.Summary)
		}
	} else {
		fmt.Println("- endpoint interpretation missing")
	}

	fmt.Println("5. Is the flow meaningful:")
	for _, stage := range report.Criteria {
		if stage != nil && stage.Name == stageMeaningful {
			if stage.Reason != "" {
				fmt.Printf("- %s (%s)\n", stage.Status, stage.Reason)
			} else {
				fmt.Printf("- %s\n", stage.Status)
			}
			break
		}
	}
	fmt.Println()
}
