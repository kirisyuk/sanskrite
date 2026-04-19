package models

import (
	"math"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// HorizonMonitorObjectKind is a normalized object kind imported from Horizon Monitor.
type HorizonMonitorObjectKind string

const (
	HorizonMonitorKindHotspot               HorizonMonitorObjectKind = "hotspot"
	HorizonMonitorKindAPTGroup              HorizonMonitorObjectKind = "apt_group"
	HorizonMonitorKindConflictZone          HorizonMonitorObjectKind = "conflict_zone"
	HorizonMonitorKindWaterway              HorizonMonitorObjectKind = "waterway"
	HorizonMonitorKindMilitaryBase          HorizonMonitorObjectKind = "military_base"
	HorizonMonitorKindUnderseaCable         HorizonMonitorObjectKind = "undersea_cable"
	HorizonMonitorKindNuclearFacility       HorizonMonitorObjectKind = "nuclear_facility"
	HorizonMonitorKindEconomicCenter        HorizonMonitorObjectKind = "economic_center"
	HorizonMonitorKindSpaceport             HorizonMonitorObjectKind = "spaceport"
	HorizonMonitorKindCriticalMineral       HorizonMonitorObjectKind = "critical_mineral_project"
	HorizonMonitorKindPipeline              HorizonMonitorObjectKind = "pipeline"
	HorizonMonitorKindPort                  HorizonMonitorObjectKind = "port"
	HorizonMonitorKindGeoAnchor             HorizonMonitorObjectKind = "geo_anchor"
	HorizonMonitorKindTransportLandingPoint HorizonMonitorObjectKind = "transport_landing_point"
)

// HorizonMonitorCoordinates aliases generic fragment coordinates for Horizon Monitor payloads.
type HorizonMonitorCoordinates = Coordinates

// HorizonMonitorLandingPoint represents a cable landing point.
type HorizonMonitorLandingPoint struct {
	Country     string                     `json:"country,omitempty" firestore:"country,omitempty"`
	City        string                     `json:"city,omitempty" firestore:"city,omitempty"`
	Coordinates *HorizonMonitorCoordinates `json:"coordinates,omitempty" firestore:"coordinates,omitempty"`
}

// HorizonMonitorFragmentData stores typed metadata from Horizon Monitor entities.
type HorizonMonitorFragmentData struct {
	ObjectKind      HorizonMonitorObjectKind     `json:"objectKind" firestore:"objectKind"`
	SourceObjectID  string                       `json:"sourceObjectId,omitempty" firestore:"sourceObjectId,omitempty"`
	Coordinates     *HorizonMonitorCoordinates   `json:"coordinates,omitempty" firestore:"coordinates,omitempty"`
	Route           []HorizonMonitorCoordinates  `json:"route,omitempty" firestore:"route,omitempty"`
	Polygon         []HorizonMonitorCoordinates  `json:"polygon,omitempty" firestore:"polygon,omitempty"`
	LandingPoints   []HorizonMonitorLandingPoint `json:"landingPoints,omitempty" firestore:"landingPoints,omitempty"`
	Countries       []string                     `json:"countries,omitempty" firestore:"countries,omitempty"`
	Parties         []string                     `json:"parties,omitempty" firestore:"parties,omitempty"`
	Keywords        []string                     `json:"keywords,omitempty" firestore:"keywords,omitempty"`
	AKA             string                       `json:"aka,omitempty" firestore:"aka,omitempty"`
	Sponsor         string                       `json:"sponsor,omitempty" firestore:"sponsor,omitempty"`
	FacilityType    string                       `json:"facilityType,omitempty" firestore:"facilityType,omitempty"`
	MarketType      string                       `json:"marketType,omitempty" firestore:"marketType,omitempty"`
	PipelineType    string                       `json:"pipelineType,omitempty" firestore:"pipelineType,omitempty"`
	PortType        string                       `json:"portType,omitempty" firestore:"portType,omitempty"`
	Mineral         string                       `json:"mineral,omitempty" firestore:"mineral,omitempty"`
	Capacity        string                       `json:"capacity,omitempty" firestore:"capacity,omitempty"`
	CapacityTbps    *float64                     `json:"capacityTbps,omitempty" firestore:"capacityTbps,omitempty"`
	Operator        string                       `json:"operator,omitempty" firestore:"operator,omitempty"`
	Status          string                       `json:"status,omitempty" firestore:"status,omitempty"`
	Significance    string                       `json:"significance,omitempty" firestore:"significance,omitempty"`
	ThreatCategory  string                       `json:"threatCategory,omitempty" firestore:"threatCategory,omitempty"`
	ThreatLevel     string                       `json:"threatLevel,omitempty" firestore:"threatLevel,omitempty"`
	ClassifierScore *float64                     `json:"classifierScore,omitempty" firestore:"classifierScore,omitempty"`
}

func cloneHorizonMonitorFragmentData(in *HorizonMonitorFragmentData) *HorizonMonitorFragmentData {
	if in == nil {
		return nil
	}
	out := *in
	if in.Coordinates != nil {
		c := *in.Coordinates
		out.Coordinates = &c
	}
	if in.CapacityTbps != nil {
		v := *in.CapacityTbps
		out.CapacityTbps = &v
	}
	if in.ClassifierScore != nil {
		v := *in.ClassifierScore
		out.ClassifierScore = &v
	}
	out.Route = append([]HorizonMonitorCoordinates(nil), in.Route...)
	out.Polygon = append([]HorizonMonitorCoordinates(nil), in.Polygon...)
	out.Countries = append([]string(nil), in.Countries...)
	out.Parties = append([]string(nil), in.Parties...)
	out.Keywords = append([]string(nil), in.Keywords...)
	if len(in.LandingPoints) > 0 {
		out.LandingPoints = make([]HorizonMonitorLandingPoint, 0, len(in.LandingPoints))
		for _, lp := range in.LandingPoints {
			cp := lp
			if lp.Coordinates != nil {
				c := *lp.Coordinates
				cp.Coordinates = &c
			}
			out.LandingPoints = append(out.LandingPoints, cp)
		}
	}
	return &out
}

// EffectiveHorizonMonitor returns canonical Horizon Monitor data with legacy fallback.
func (f *HorizonFragment) EffectiveHorizonMonitor() *HorizonMonitorFragmentData {
	if f == nil {
		return nil
	}
	if f.HorizonMonitor != nil {
		return f.HorizonMonitor
	}
	return f.WorldMonitorLegacy
}

// NormalizeHorizonMonitorAliases reads legacy worldMonitor alias into canonical horizonMonitor field.
func (f *HorizonFragment) NormalizeHorizonMonitorAliases() {
	if f == nil {
		return
	}
	if f.HorizonMonitor == nil && f.WorldMonitorLegacy != nil {
		f.HorizonMonitor = cloneHorizonMonitorFragmentData(f.WorldMonitorLegacy)
	}
}

// SyncHorizonMonitorAliases mirrors canonical horizonMonitor field into legacy worldMonitor alias.
func (f *HorizonFragment) SyncHorizonMonitorAliases() {
	if f == nil {
		return
	}
	f.NormalizeHorizonMonitorAliases()
	if f.HorizonMonitor != nil {
		f.WorldMonitorLegacy = cloneHorizonMonitorFragmentData(f.HorizonMonitor)
	}
}

// horizonMonitorGeoUpdatableClasses lists the AH fragment classes the
// Horizon Monitor pipeline is allowed to refresh from geo-driven feeds.
// Subtype gating (e.g. only site/deposit, only evidence/event) lives in
// IsHorizonMonitorGeoUpdatableFragment so the predicate can reason about
// the (Class, Subtype) tuple rather than collapsing them on a single
// legacy enum.
var horizonMonitorGeoUpdatableClasses = map[ah.FragmentClass]struct{}{
	ah.FragmentClassOrg:         {},
	ah.FragmentClassSite:        {},
	ah.FragmentClassGeo:         {},
	ah.FragmentClassEvidence:    {},
	ah.FragmentClassMarket:      {},
	ah.FragmentClassMobileAsset: {},
}

// IsHorizonMonitorGeoUpdatableClass reports whether the AH fragment class
// can be selected for geo-driven updates in the Horizon Monitor pipeline.
func IsHorizonMonitorGeoUpdatableClass(c ah.FragmentClass) bool {
	_, ok := horizonMonitorGeoUpdatableClasses[c]
	return ok
}

// IsHorizonMonitorGeoUpdatableFragment returns true when the fragment's
// (Class, Subtype) tuple is eligible for the Horizon Monitor geo refresh
// pipeline. Replaces the legacy IsHorizonMonitorGeoUpdatableType helper
// which keyed on the now-removed FragmentType enum (notably FACILITY,
// DEPOSIT, EVENT, TRACK — all collapsed into AH classes here, with the
// subtype carrying the specialised gate).
func IsHorizonMonitorGeoUpdatableFragment(f *HorizonFragment) bool {
	if f == nil {
		return false
	}
	if !IsHorizonMonitorGeoUpdatableClass(f.Class) {
		return false
	}
	switch f.Class {
	case ah.FragmentClassEvidence:
		return f.Subtype == "" || f.Subtype == "event"
	case ah.FragmentClassMobileAsset:
		return true
	}
	return true
}

// IsRealGeoCoordinate validates coordinate range and excludes Null Island fallback.
func IsRealGeoCoordinate(lat, lon float64) bool {
	if !IsValidCoordinates(lat, lon) {
		return false
	}
	return !(math.Abs(lat) < 1e-9 && math.Abs(lon) < 1e-9)
}

// HasRealGeoCoordinates checks whether a coordinate struct carries a valid non-fallback position.
func HasRealGeoCoordinates(c *Coordinates) bool {
	if c == nil {
		return false
	}
	return IsRealGeoCoordinate(c.Lat, c.Lon)
}

// IsHorizonMonitorGeoEligibleFragment implements selection policy:
// real geo + allowed fragment class/subtype.
func IsHorizonMonitorGeoEligibleFragment(f *HorizonFragment) bool {
	if f == nil || !IsHorizonMonitorGeoUpdatableFragment(f) {
		return false
	}
	if HasRealGeoCoordinates(f.Coordinates) {
		return true
	}
	if wm := f.EffectiveHorizonMonitor(); wm != nil && wm.Coordinates != nil {
		return IsRealGeoCoordinate(wm.Coordinates.Lat, wm.Coordinates.Lon)
	}
	if f.Track != nil {
		return IsRealGeoCoordinate(f.Track.Lat, f.Track.Lon)
	}
	return false
}
