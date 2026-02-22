package models

import "strings"

type UnitTemplateKey string

const (
	UnitTemplateGeneral         UnitTemplateKey = "general"
	UnitTemplateRouteFlow       UnitTemplateKey = "route_flow"
	UnitTemplateSupplyFlow      UnitTemplateKey = "supply_flow"
	UnitTemplateProcurementFlow UnitTemplateKey = "procurement_flow"
)

type UnitTemplatePair struct {
	Source UnitCode
	Target UnitCode
}

type UnitTemplateContract struct {
	Key          UnitTemplateKey
	AllowedPairs []UnitTemplatePair
	DefaultPair  UnitTemplatePair
}

var unitTemplateContracts = map[UnitTemplateKey]UnitTemplateContract{
	UnitTemplateRouteFlow: {
		Key: UnitTemplateRouteFlow,
		AllowedPairs: []UnitTemplatePair{
			{Source: UnitCodeLogistics, Target: UnitCodeLogistics},
			{Source: UnitCodeEnterprise, Target: UnitCodeEnterprise},
			{Source: UnitCodeCoreOps, Target: UnitCodeCoreOps},
		},
		DefaultPair: UnitTemplatePair{Source: UnitCodeLogistics, Target: UnitCodeLogistics},
	},
	UnitTemplateSupplyFlow: {
		Key: UnitTemplateSupplyFlow,
		AllowedPairs: []UnitTemplatePair{
			{Source: UnitCodeCoreOps, Target: UnitCodeCoreOps},
			{Source: UnitCodeEnterprise, Target: UnitCodeEnterprise},
			{Source: UnitCodeLogistics, Target: UnitCodeLogistics},
			{Source: UnitCodeConsumer, Target: UnitCodeConsumer},
			{Source: UnitCodeEnterprise, Target: UnitCodeConsumer},
			{Source: UnitCodeConsumer, Target: UnitCodeEnterprise},
		},
		DefaultPair: UnitTemplatePair{Source: UnitCodeEnterprise, Target: UnitCodeEnterprise},
	},
	UnitTemplateProcurementFlow: {
		Key: UnitTemplateProcurementFlow,
		AllowedPairs: []UnitTemplatePair{
			{Source: UnitCodeEnterprise, Target: UnitCodeEnterprise},
			{Source: UnitCodeCorporate, Target: UnitCodeEnterprise},
			{Source: UnitCodeLogistics, Target: UnitCodeLogistics},
			{Source: UnitCodeEnterprise, Target: UnitCodeCoreOps},
		},
		DefaultPair: UnitTemplatePair{Source: UnitCodeEnterprise, Target: UnitCodeEnterprise},
	},
}

func resolveUnitTemplateByQualifier(q ObjectQualifier) UnitTemplateKey {
	switch q {
	case ObjectQualifierRoute:
		return UnitTemplateRouteFlow
	case ObjectQualifierSupply:
		return UnitTemplateSupplyFlow
	case ObjectQualifierProcurement, ObjectQualifierProcurer:
		return UnitTemplateProcurementFlow
	default:
		return UnitTemplateGeneral
	}
}

// UnitTemplateKeyForObjectQualifier maps object qualifier to closed unit template key.
func UnitTemplateKeyForObjectQualifier(q ObjectQualifier) UnitTemplateKey {
	return resolveUnitTemplateByQualifier(q)
}

// UnitTemplateContractForKey returns a copy of the closed unit template contract.
func UnitTemplateContractForKey(key UnitTemplateKey) (UnitTemplateContract, bool) {
	contract, ok := unitTemplateContracts[key]
	if !ok {
		return UnitTemplateContract{}, false
	}
	out := contract
	out.AllowedPairs = append([]UnitTemplatePair(nil), contract.AllowedPairs...)
	return out, true
}

// UnitTemplateContractForQualifier returns a non-general unit template for qualifier when applicable.
func UnitTemplateContractForQualifier(q ObjectQualifier) (UnitTemplateContract, bool) {
	key := resolveUnitTemplateByQualifier(q)
	if key == UnitTemplateGeneral {
		return UnitTemplateContract{}, false
	}
	return UnitTemplateContractForKey(key)
}

func normalizeUnitTemplatePair(source, target UnitCode) UnitTemplatePair {
	return UnitTemplatePair{
		Source: NormalizeUnitCode(string(source)),
		Target: NormalizeUnitCode(string(target)),
	}
}

func isAllowedUnitTemplatePair(contract UnitTemplateContract, pair UnitTemplatePair) bool {
	for _, p := range contract.AllowedPairs {
		if p.Source == pair.Source && p.Target == pair.Target {
			return true
		}
	}
	return false
}

func shouldApplyUnitTemplateToRelation(relType DependencyType) bool {
	switch strings.TrimSpace(string(relType)) {
	case string(DependencyTypeSuppliesTo), string(DependencyTypeDependsOn):
		return true
	default:
		return false
	}
}

// ApplyUnitTemplatePairByQualifier enforces closed unit-pair templates on flow relations.
// Returns normalized source/target unit codes, template key, and whether pair was adjusted.
func ApplyUnitTemplatePairByQualifier(q ObjectQualifier, relType DependencyType, source, target UnitCode) (UnitCode, UnitCode, UnitTemplateKey, bool) {
	source = NormalizeUnitCode(string(source))
	target = NormalizeUnitCode(string(target))
	if !shouldApplyUnitTemplateToRelation(relType) {
		return source, target, UnitTemplateGeneral, false
	}
	key := resolveUnitTemplateByQualifier(q)
	if key == UnitTemplateGeneral {
		return source, target, key, false
	}
	contract, ok := unitTemplateContracts[key]
	if !ok {
		return source, target, UnitTemplateGeneral, false
	}
	current := normalizeUnitTemplatePair(source, target)
	if current.Source == UnitCodeUnclassified || current.Target == UnitCodeUnclassified {
		return contract.DefaultPair.Source, contract.DefaultPair.Target, key, true
	}
	if isAllowedUnitTemplatePair(contract, current) {
		return current.Source, current.Target, key, false
	}
	return contract.DefaultPair.Source, contract.DefaultPair.Target, key, true
}
