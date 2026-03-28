package models

import "testing"

func TestRouteGeometryPolylineRoundTrip(t *testing.T) {
	coords := []Coordinates{
		{Lat: 26.65, Lon: 50.04},
		{Lat: 26.58, Lon: 56.30},
		{Lat: 24.80, Lon: 58.90},
		{Lat: 20.50, Lon: 63.00},
	}
	encoded := EncodeRoutePolyline(coords)
	if encoded == "" {
		t.Fatal("encoded polyline is empty")
	}
	decoded := DecodeRoutePolyline(encoded)
	if len(decoded) != len(coords) {
		t.Fatalf("decoded len=%d want=%d", len(decoded), len(coords))
	}
	for idx := range coords {
		if absFloat(decoded[idx].Lat-coords[idx].Lat) > 1e-5 || absFloat(decoded[idx].Lon-coords[idx].Lon) > 1e-5 {
			t.Fatalf("decoded[%d]=%+v want=%+v", idx, decoded[idx], coords[idx])
		}
	}
}
