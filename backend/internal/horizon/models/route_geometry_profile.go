package models

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"time"
)

type RouteGeometryProfile struct {
	ID              string        `json:"id,omitempty" firestore:"id,omitempty"`
	TenantID        string        `json:"tenantId,omitempty" firestore:"tenantId,omitempty"`
	ProfileKey      string        `json:"profileKey" firestore:"profileKey"`
	RouteVariantKey string        `json:"routeVariantKey,omitempty" firestore:"routeVariantKey,omitempty"`
	OrderedRefs     []string      `json:"orderedRefs,omitempty" firestore:"orderedRefs,omitempty"`
	FlowRefs        []string      `json:"flowRefs,omitempty" firestore:"flowRefs,omitempty"`
	Coordinates     []Coordinates `json:"coordinates,omitempty" firestore:"coordinates,omitempty"`
	EncodedPolyline string        `json:"encodedPolyline,omitempty" firestore:"encodedPolyline,omitempty"`
	Source          string        `json:"source,omitempty" firestore:"source,omitempty"`
	Accuracy        string        `json:"accuracy,omitempty" firestore:"accuracy,omitempty"`
	Notes           string        `json:"notes,omitempty" firestore:"notes,omitempty"`
	CreatedAt       time.Time     `json:"createdAt" firestore:"createdAt"`
	UpdatedAt       time.Time     `json:"updatedAt" firestore:"updatedAt"`
}

type RouteGeometryCandidate struct {
	ID                      string    `json:"id,omitempty" firestore:"id,omitempty"`
	TenantID                string    `json:"tenantId,omitempty" firestore:"tenantId,omitempty"`
	ProfileKey              string    `json:"profileKey" firestore:"profileKey"`
	RouteVariantKey         string    `json:"routeVariantKey,omitempty" firestore:"routeVariantKey,omitempty"`
	OrderedRefs             []string  `json:"orderedRefs,omitempty" firestore:"orderedRefs,omitempty"`
	SupplyLinkCanonicalPath string    `json:"supplyLinkCanonicalPath,omitempty" firestore:"supplyLinkCanonicalPath,omitempty"`
	PathArtifactKey         string    `json:"pathArtifactKey,omitempty" firestore:"pathArtifactKey,omitempty"`
	Reason                  string    `json:"reason,omitempty" firestore:"reason,omitempty"`
	LastSeenAt              time.Time `json:"lastSeenAt,omitempty" firestore:"lastSeenAt,omitempty"`
	SeenCount               int       `json:"seenCount,omitempty" firestore:"seenCount,omitempty"`
	CreatedAt               time.Time `json:"createdAt" firestore:"createdAt"`
	UpdatedAt               time.Time `json:"updatedAt" firestore:"updatedAt"`
}

func NormalizeOrderedRouteRefs(refs []string) []string {
	out := make([]string, 0, len(refs))
	prev := ""
	for _, ref := range refs {
		ref = canonicalizeTransportViaRef(ref)
		if ref == "" || ref == prev {
			continue
		}
		out = append(out, ref)
		prev = ref
	}
	return out
}

func BuildRouteGeometryProfileKey(refs []string) string {
	normalized := NormalizeOrderedRouteRefs(refs)
	if len(normalized) == 0 {
		return ""
	}
	sum := sha1.Sum([]byte(strings.Join(normalized, "\n")))
	return hex.EncodeToString(sum[:])
}

func (p *RouteGeometryProfile) Normalize() {
	if p == nil {
		return
	}
	p.TenantID = strings.TrimSpace(p.TenantID)
	p.ProfileKey = strings.TrimSpace(p.ProfileKey)
	p.RouteVariantKey = strings.TrimSpace(p.RouteVariantKey)
	p.OrderedRefs = NormalizeOrderedRouteRefs(p.OrderedRefs)
	p.FlowRefs = uniqueTrimmedStrings(p.FlowRefs)
	p.Source = strings.TrimSpace(p.Source)
	p.Accuracy = strings.TrimSpace(p.Accuracy)
	p.Notes = strings.TrimSpace(p.Notes)
	if p.ProfileKey == "" {
		p.ProfileKey = BuildRouteGeometryProfileKey(p.OrderedRefs)
	}
	p.Coordinates = normalizeRouteProfileCoordinates(p.Coordinates)
	if p.EncodedPolyline == "" && len(p.Coordinates) >= 2 {
		p.EncodedPolyline = EncodeRoutePolyline(p.Coordinates)
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
}

func (c *RouteGeometryCandidate) Normalize() {
	if c == nil {
		return
	}
	c.TenantID = strings.TrimSpace(c.TenantID)
	c.ProfileKey = strings.TrimSpace(c.ProfileKey)
	c.RouteVariantKey = strings.TrimSpace(c.RouteVariantKey)
	c.OrderedRefs = NormalizeOrderedRouteRefs(c.OrderedRefs)
	c.SupplyLinkCanonicalPath = strings.TrimSpace(c.SupplyLinkCanonicalPath)
	c.PathArtifactKey = strings.TrimSpace(c.PathArtifactKey)
	c.Reason = strings.TrimSpace(c.Reason)
	if c.ProfileKey == "" {
		c.ProfileKey = BuildRouteGeometryProfileKey(c.OrderedRefs)
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.LastSeenAt.IsZero() {
		c.LastSeenAt = now
	}
	c.UpdatedAt = now
}

func normalizeRouteProfileCoordinates(coords []Coordinates) []Coordinates {
	if len(coords) == 0 {
		return nil
	}
	out := make([]Coordinates, 0, len(coords))
	for _, coord := range coords {
		if !IsRealGeoCoordinate(coord.Lat, coord.Lon) {
			continue
		}
		if len(out) > 0 {
			prev := out[len(out)-1]
			if absFloat(prev.Lat-coord.Lat) < 1e-7 && absFloat(prev.Lon-coord.Lon) < 1e-7 {
				continue
			}
		}
		out = append(out, coord)
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func EncodeRoutePolyline(coords []Coordinates) string {
	coords = normalizeRouteProfileCoordinates(coords)
	if len(coords) == 0 {
		return ""
	}
	var out strings.Builder
	lastLat := 0
	lastLon := 0
	for _, coord := range coords {
		lat := int(coord.Lat*1e5 + 0.5)
		if coord.Lat < 0 {
			lat = int(coord.Lat*1e5 - 0.5)
		}
		lon := int(coord.Lon*1e5 + 0.5)
		if coord.Lon < 0 {
			lon = int(coord.Lon*1e5 - 0.5)
		}
		encodePolylineValue(&out, lat-lastLat)
		encodePolylineValue(&out, lon-lastLon)
		lastLat = lat
		lastLon = lon
	}
	return out.String()
}

func DecodeRoutePolyline(encoded string) []Coordinates {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil
	}
	out := make([]Coordinates, 0, 32)
	lat := 0
	lon := 0
	index := 0
	for index < len(encoded) {
		deltaLat, next, ok := decodePolylineValue(encoded, index)
		if !ok {
			return nil
		}
		index = next
		deltaLon, next, ok := decodePolylineValue(encoded, index)
		if !ok {
			return nil
		}
		index = next
		lat += deltaLat
		lon += deltaLon
		out = append(out, Coordinates{
			Lat: float64(lat) / 1e5,
			Lon: float64(lon) / 1e5,
		})
	}
	return normalizeRouteProfileCoordinates(out)
}

func encodePolylineValue(out *strings.Builder, value int) {
	value <<= 1
	if value < 0 {
		value = ^value
	}
	for value >= 0x20 {
		out.WriteByte(byte((0x20 | (value & 0x1f)) + 63))
		value >>= 5
	}
	out.WriteByte(byte(value + 63))
}

func decodePolylineValue(encoded string, start int) (int, int, bool) {
	result := 0
	shift := 0
	index := start
	for index < len(encoded) {
		b := int(encoded[index]) - 63
		index++
		result |= (b & 0x1f) << shift
		shift += 5
		if b < 0x20 {
			delta := result >> 1
			if result&1 != 0 {
				delta = ^delta
			}
			return delta, index, true
		}
	}
	return 0, start, false
}
