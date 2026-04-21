// Package wirekeys holds stable qualifier key strings shared by scout subpackages
// without import cycles (scout imports agents; agents must not import scout).
package wirekeys

const (
	QualifierTransportVia                = "transport_via"
	QualifierAtFacility                  = "at_facility"
	QualifierFlowRefs                    = "flow_refs"
	QualifierZoneRefs                    = "zone_refs"
	QualifierAffectedZoneRefs            = "affected_zone_refs"
	QualifierFacet                       = "facet"
	QualifierTransmissionContractVersion = "transmission_contract_version"
	QualifierTransmissionContractStatus  = "transmission_contract_status"
	QualifierTransmissionContractMode    = "transmission_contract_mode"
	QualifierTransmissionContractMissing = "transmission_contract_missing"

	TransmissionContractVersionV1 = "v1"

	TransmissionContractStatusComplete = "complete"
	TransmissionContractStatusPartial  = "partial"
	TransmissionContractStatusMissing  = "missing"

	TransmissionContractModeInferred = "inferred"
	TransmissionContractModeExplicit = "explicit"
)
