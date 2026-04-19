package propagation

import (
	"testing"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestClassifySignalTransfer_CrossBorderEconomic(t *testing.T) {
	from := &horizonmodels.HorizonFragment{
		Class:         FragmentTypeOrganization,
		CanonicalPath: "/org/kz/ministry-of-finance",
	}
	to := &horizonmodels.HorizonFragment{
		Class:         FragmentTypeOrganization,
		CanonicalPath: "/org/ae/mubadala",
	}

	channel, scope, spillover, fromGeo, toGeo := classifySignalTransfer(
		horizonmodels.AxisFinancialSettlement,
		DependencyTypeDependsOn,
		from,
		to,
	)

	if channel != "economic" {
		t.Fatalf("channel = %q, want economic", channel)
	}
	if scope != "cross_border" {
		t.Fatalf("scope = %q, want cross_border", scope)
	}
	if spillover != "cross_border_economic" {
		t.Fatalf("spillover = %q, want cross_border_economic", spillover)
	}
	if fromGeo != "kz" || toGeo != "ae" {
		t.Fatalf("unexpected geo extraction: from=%q to=%q", fromGeo, toGeo)
	}
}

func TestClassifySignalTransfer_DomesticRegulatory(t *testing.T) {
	from := &horizonmodels.HorizonFragment{
		Class:         FragmentTypeOrganization,
		CanonicalPath: "/gov/kz/ministry-of-finance",
	}
	to := &horizonmodels.HorizonFragment{
		Class:         ah.FragmentClassOrg,
		Subtype:       "program",
		CanonicalPath: "/program/kz/vat-reform-2026",
	}

	channel, scope, spillover, _, _ := classifySignalTransfer(
		horizonmodels.AxisLegalCompliance,
		DependencyTypeRegulates,
		from,
		to,
	)
	if channel != "regulatory" {
		t.Fatalf("channel = %q, want regulatory", channel)
	}
	if scope != "domestic" {
		t.Fatalf("scope = %q, want domestic", scope)
	}
	if spillover != "direct_domestic" {
		t.Fatalf("spillover = %q, want direct_domestic", spillover)
	}
}
