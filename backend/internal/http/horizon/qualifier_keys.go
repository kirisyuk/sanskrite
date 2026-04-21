package horizon

// Qualifier keys on dependency edges and path materialization (wire-stable strings).
const (
	QualifierTransportVia     = "transport_via"
	QualifierAtFacility       = "at_facility"
	QualifierFlowRefs         = "flow_refs"
	QualifierZoneRefs         = "zone_refs"
	QualifierAffectedZoneRefs = "affected_zone_refs"
	QualifierFacet            = "facet"

	QualifierRouteMode            = "route_mode"
	QualifierGeoRefs              = "geo_refs"
	QualifierAlternateFlowRefs    = "alternate_flow_refs"
	QualifierSpatialSemantics     = "spatial_semantics"
	QualifierBindingConfidence    = "binding_confidence"
	QualifierPathStressScore      = "path_stress_score"
	QualifierPathFragility        = "path_fragility"
	QualifierPathRerouteability   = "path_rerouteability"
	QualifierPathZonePressure     = "path_zone_pressure"
	QualifierPathAffectedPressure = "path_affected_pressure"
	QualifierRecoveryLatencyHours = "recovery_latency_hours"
)
