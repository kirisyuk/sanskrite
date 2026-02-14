package models

import "time"

// TrackKind identifies broad mobility domain for a track fragment.
type TrackKind string

const (
	TrackKindAir    TrackKind = "air"
	TrackKindSea    TrackKind = "sea"
	TrackKindGround TrackKind = "ground"
)

// TrackFragmentData stores frequently refreshed movement state.
// It is intentionally compact: history should be persisted as facts/signals.
type TrackFragmentData struct {
	Kind            TrackKind `json:"kind" firestore:"kind"`
	ObjectType      string    `json:"objectType,omitempty" firestore:"objectType,omitempty"`           // military_aircraft, vessel, convoy, etc.
	ExternalTrackID string    `json:"externalTrackId,omitempty" firestore:"externalTrackId,omitempty"` // ICAO24/MMSI/etc.
	SourceID        string    `json:"sourceId,omitempty" firestore:"sourceId,omitempty"`
	Lat             float64   `json:"lat" firestore:"lat"`
	Lon             float64   `json:"lon" firestore:"lon"`
	HeadingDeg      *float64  `json:"headingDeg,omitempty" firestore:"headingDeg,omitempty"`
	SpeedKts        *float64  `json:"speedKts,omitempty" firestore:"speedKts,omitempty"`
	AltitudeM       *float64  `json:"altitudeM,omitempty" firestore:"altitudeM,omitempty"`
	DepthM          *float64  `json:"depthM,omitempty" firestore:"depthM,omitempty"`
	LastSeenAt      time.Time `json:"lastSeenAt" firestore:"lastSeenAt"`
	Confidence      float64   `json:"confidence,omitempty" firestore:"confidence,omitempty"`
	GeoHash         string    `json:"geoHash,omitempty" firestore:"geoHash,omitempty"`
}
