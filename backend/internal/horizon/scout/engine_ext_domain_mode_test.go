package scout

import (
	"context"
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
)

func TestEnsureJobDomainMode_AddsGovernanceForOrgByDefault(t *testing.T) {
	engine := &ScoutEngine{}
	job := &horizonmodels.ScoutingJob{
		TargetCanonicalPath: "/org/us/acme-inc",
	}

	engine.ensureJobDomainMode(context.Background(), job, job.TargetCanonicalPath)
	modes := taxonomy.NormalizeDomainModes(getJobDomainModes(job))

	if !taxonomy.HasDomainMode(modes, taxonomy.DomainModeGeneral) {
		t.Fatalf("expected %q mode for /org/, got %v", taxonomy.DomainModeGeneral, modes)
	}
	if !taxonomy.HasDomainMode(modes, taxonomy.DomainModeGovernance) {
		t.Fatalf("expected %q mode for /org/, got %v", taxonomy.DomainModeGovernance, modes)
	}
}

func TestEnsureJobDomainMode_AddsGovernanceForGovWhenGeneralPreset(t *testing.T) {
	engine := &ScoutEngine{}
	job := &horizonmodels.ScoutingJob{
		TargetCanonicalPath: "/gov/us/department-of-state",
	}
	job.State = map[string]interface{}{
		"domainMode": taxonomy.DomainModeGeneral,
	}

	engine.ensureJobDomainMode(context.Background(), job, job.TargetCanonicalPath)
	modes := taxonomy.NormalizeDomainModes(getJobDomainModes(job))

	if !taxonomy.HasDomainMode(modes, taxonomy.DomainModeGeneral) {
		t.Fatalf("expected %q mode for /gov/, got %v", taxonomy.DomainModeGeneral, modes)
	}
	if !taxonomy.HasDomainMode(modes, taxonomy.DomainModeGovernance) {
		t.Fatalf("expected %q mode for /gov/, got %v", taxonomy.DomainModeGovernance, modes)
	}
}

func TestEnsureJobDomainMode_KeepsExistingModesAndAddsGovernanceForOrg(t *testing.T) {
	engine := &ScoutEngine{}
	job := &horizonmodels.ScoutingJob{
		TargetCanonicalPath: "/org/de/siemens-ag",
	}
	job.State = map[string]interface{}{
		"domainModes": []string{taxonomy.DomainModeSupplyChain},
	}

	engine.ensureJobDomainMode(context.Background(), job, job.TargetCanonicalPath)
	modes := taxonomy.NormalizeDomainModes(getJobDomainModes(job))

	if !taxonomy.HasDomainMode(modes, taxonomy.DomainModeSupplyChain) {
		t.Fatalf("expected %q to be preserved, got %v", taxonomy.DomainModeSupplyChain, modes)
	}
	if !taxonomy.HasDomainMode(modes, taxonomy.DomainModeGovernance) {
		t.Fatalf("expected %q mode for /org/, got %v", taxonomy.DomainModeGovernance, modes)
	}
}

func TestEnsureJobDomainMode_DoesNotInjectGovernanceForNonOrgGov(t *testing.T) {
	engine := &ScoutEngine{}
	job := &horizonmodels.ScoutingJob{
		TargetCanonicalPath: "/market/us/technology/semiconductors",
	}

	engine.ensureJobDomainMode(context.Background(), job, job.TargetCanonicalPath)
	modes := taxonomy.NormalizeDomainModes(getJobDomainModes(job))

	if taxonomy.HasDomainMode(modes, taxonomy.DomainModeGovernance) {
		t.Fatalf("did not expect %q for non org/gov path, got %v", taxonomy.DomainModeGovernance, modes)
	}
}
