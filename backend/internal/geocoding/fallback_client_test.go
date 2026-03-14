package geocoding

import (
	"context"
	"errors"
	"testing"

	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

type mockGeoClient struct {
	result *ontology.GeocodingResult
	err    error
	calls  int
}

func (m *mockGeoClient) Geocode(_ context.Context, _ string) (*ontology.GeocodingResult, error) {
	m.calls++
	return m.result, m.err
}

func TestFallbackClient_PrimaryHit(t *testing.T) {
	primary := &mockGeoClient{result: &ontology.GeocodingResult{Lat: 55.7, Lon: 37.6, FormattedAddress: "Moscow"}}
	fallback := &mockGeoClient{result: &ontology.GeocodingResult{Lat: 55.8, Lon: 37.7, FormattedAddress: "Moscow Google"}}

	c := NewFallbackClient(primary, fallback, nil)
	r, err := c.Geocode(context.Background(), "Moscow, Russia")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.FormattedAddress != "Moscow" {
		t.Errorf("expected primary result, got %q", r.FormattedAddress)
	}
	if r.Provider != ontology.GeocodingProviderNominatim {
		t.Errorf("expected primary provider=%q, got %q", ontology.GeocodingProviderNominatim, r.Provider)
	}
	if primary.calls != 1 {
		t.Errorf("expected 1 primary call, got %d", primary.calls)
	}
	if fallback.calls != 0 {
		t.Errorf("expected 0 fallback calls, got %d", fallback.calls)
	}
}

func TestFallbackClient_PrimaryMiss(t *testing.T) {
	primary := &mockGeoClient{result: nil}
	fallback := &mockGeoClient{result: &ontology.GeocodingResult{Lat: 55.8, Lon: 37.7, FormattedAddress: "Moscow Google"}}

	c := NewFallbackClient(primary, fallback, nil)
	r, err := c.Geocode(context.Background(), "Moscow, Russia")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.FormattedAddress != "Moscow Google" {
		t.Errorf("expected fallback result, got %q", r.FormattedAddress)
	}
	if r.Provider != ontology.GeocodingProviderGoogle {
		t.Errorf("expected fallback provider=%q, got %q", ontology.GeocodingProviderGoogle, r.Provider)
	}
	if primary.calls != 1 || fallback.calls != 1 {
		t.Errorf("expected 1+1 calls, got %d+%d", primary.calls, fallback.calls)
	}
}

func TestFallbackClient_PrimaryError(t *testing.T) {
	primary := &mockGeoClient{err: errors.New("nominatim timeout")}
	fallback := &mockGeoClient{result: &ontology.GeocodingResult{Lat: 55.8, Lon: 37.7, FormattedAddress: "Moscow Google"}}

	c := NewFallbackClient(primary, fallback, nil)
	r, err := c.Geocode(context.Background(), "Moscow, Russia")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.FormattedAddress != "Moscow Google" {
		t.Errorf("expected fallback result, got %q", r.FormattedAddress)
	}
}

func TestFallbackClient_NilPrimary(t *testing.T) {
	fallback := &mockGeoClient{result: &ontology.GeocodingResult{Lat: 55.8, Lon: 37.7}}
	c := NewFallbackClient(nil, fallback, nil)
	r, err := c.Geocode(context.Background(), "Moscow")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r == nil {
		t.Fatal("expected result")
	}
}

func TestFallbackClient_BothNil(t *testing.T) {
	c := NewFallbackClient(nil, nil, nil)
	if c != nil {
		t.Error("expected nil client when both are nil")
	}
}
