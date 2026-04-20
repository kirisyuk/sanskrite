package criticalchainview

import (
	"testing"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

func TestWaypointsFromGeometryLineString(t *testing.T) {
	raw := map[string]interface{}{
		"type": "LineString",
		"coordinates": []interface{}{
			[]interface{}{12.5, 55.3},
			[]interface{}{12.6, 55.4},
		},
	}
	wp := waypointsFromGeometryAttr(raw)
	if len(wp) != 2 {
		t.Fatalf("got %d waypoints", len(wp))
	}
	if wp[0].Latitude != 55.3 || wp[0].Longitude != 12.5 {
		t.Fatalf("first point: %+v", wp[0])
	}
}

func TestApplyAHProductOverlayRiskHistory(t *testing.T) {
	ts := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	p := &horizonmodels.HorizonFragment{
		Attributes: map[string]interface{}{
			"risk_history": []interface{}{
				map[string]interface{}{
					"timestamp": ts.Format(time.RFC3339),
					"value":     2.5,
				},
			},
			"hs_codes": []interface{}{"1234", "5678"},
			"cas_number": "7440-00-0",
		},
		State: map[string]horizonmodels.MetricValue{
			"criticality_score": {Value: 4.2},
		},
	}
	m := &Material{ID: "x", RiskScore: 1, CriticalityScore: 3}
	ApplyAHProductOverlay(m, p)
	if len(m.RiskHistory) != 1 || m.RiskHistory[0].Value != 2.5 {
		t.Fatalf("risk history: %+v", m.RiskHistory)
	}
	if len(m.HSCodes) != 2 || m.CASNumber == "" {
		t.Fatalf("codes: hs=%v cas=%q", m.HSCodes, m.CASNumber)
	}
	if m.CriticalityScore != 4.2 {
		t.Fatalf("criticality %v", m.CriticalityScore)
	}
}

func TestFacilityFromSiteSanctionedAndCapacity(t *testing.T) {
	u := "t/y"
	f := &horizonmodels.HorizonFragment{
		ID:            "s1",
		Class:         ah.FragmentClassSite,
		Subtype:       "refinery",
		Description:   "Test",
		CanonicalPath: "/site/xx/refinery/a",
		State: map[string]horizonmodels.MetricValue{
			"risk_score": {Value: 0},
			"capacity":   {Value: 100, Unit: &u},
		},
	}
	out := facilityFromSiteFragment(f, "", true)
	if out == nil || !out.Sanctioned || out.RiskScore != 0 {
		t.Fatalf("facility: %+v", out)
	}
	if out.Capacity != 100 || out.CapacityUnit != "t/y" {
		t.Fatalf("capacity: %v %q", out.Capacity, out.CapacityUnit)
	}
}
