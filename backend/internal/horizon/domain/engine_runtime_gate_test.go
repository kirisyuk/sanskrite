package domain

import (
	"context"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func TestGateSummaryWithRuntimeMetadata(t *testing.T) {
	gate := gateResult{
		OverallStatus: "fail",
		TotalChecks:   3,
		Passed:        1,
		Failed:        2,
		Checks: []models.DomainGateCheck{
			{CheckID: "U1", Status: "fail", Priority: "P0"},
			{CheckID: "U2", Status: "fail", Priority: "P2"},
			{CheckID: "U3", Status: "pass", Priority: "P3"},
		},
	}

	summary := gateSummaryWithRuntime(gate, true, true)
	if !summary.GraphTruncated {
		t.Fatal("expected graphTruncated=true")
	}
	if !summary.PartialWaitTimeout {
		t.Fatal("expected partialWaitTimeout=true")
	}
	if summary.CriticalOpenCount != 1 {
		t.Fatalf("expected criticalOpenCount=1, got %d", summary.CriticalOpenCount)
	}
	if len(summary.RecoverableReasons) != 2 {
		t.Fatalf("expected 2 recoverable reasons, got %d", len(summary.RecoverableReasons))
	}
}

func TestHasRecoverableCriticalOpen(t *testing.T) {
	gate := gateResult{
		OverallStatus: "fail",
		Checks: []models.DomainGateCheck{
			{CheckID: "U1", Status: "fail", Priority: "P1"},
		},
	}
	if !hasRecoverableCriticalOpen(gate, true, false) {
		t.Fatal("expected recoverable critical open for truncated graph + P1 fail")
	}
	if hasRecoverableCriticalOpen(gate, false, false) {
		t.Fatal("did not expect recoverable critical open without recoverable runtime reasons")
	}
}

func TestAwaitGraphDeltaMarksPartialWaitTimeoutOnCanceledContext(t *testing.T) {
	e := &Engine{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	job := &models.ScoutingJob{}
	snapshot := e.awaitGraphDelta(ctx, job, Contract{}, 0, 0, 1, nil)
	if !snapshot.PartialWaitTimeout {
		t.Fatal("expected partial wait timeout marker on canceled context")
	}
}
