package edgesemantics

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// NormalizeFragmentTypeToken maps type labels/aliases (and accidental canonical paths)
// to Horizon FragmentType. It never returns empty.
func NormalizeFragmentTypeToken(raw string) models.FragmentType {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return models.FragmentTypeOrganization
	}
	if inferred, ok := ExpectedTypeFromCanonicalPath(strings.ToLower(raw)); ok && inferred != "" {
		return inferred
	}
	token := normalizeTypeToken(raw)
	if typ, ok := fragmentTypeAliases[token]; ok {
		return typ
	}
	return models.FragmentTypeOrganization
}

func normalizeTypeToken(raw string) string {
	token := strings.TrimSpace(strings.ToUpper(raw))
	token = strings.ReplaceAll(token, "-", "_")
	token = strings.ReplaceAll(token, " ", "_")
	token = strings.ReplaceAll(token, "/", "_")
	for strings.Contains(token, "__") {
		token = strings.ReplaceAll(token, "__", "_")
	}
	return strings.Trim(token, "_")
}

var fragmentTypeAliases = map[string]models.FragmentType{
	// ORGANIZATION (+ GOV aliases)
	string(models.FragmentTypeOrganization): models.FragmentTypeOrganization,
	"ORG":                                   models.FragmentTypeOrganization,
	"GOV":                                   models.FragmentTypeOrganization,
	"GOVERNMENT":                            models.FragmentTypeOrganization,
	"GOVERNMENT_BODY":                       models.FragmentTypeOrganization,
	"PUBLIC_BODY":                           models.FragmentTypeOrganization,

	// Core entity types
	string(models.FragmentTypeFacility):     models.FragmentTypeFacility,
	"FAC":                                   models.FragmentTypeFacility,
	"PLANT":                                 models.FragmentTypeFacility,
	"SITE":                                  models.FragmentTypeFacility,
	string(models.FragmentTypeGeoRegion):    models.FragmentTypeGeoRegion,
	"REGION":                                models.FragmentTypeGeoRegion,
	"GEO":                                   models.FragmentTypeGeoRegion,
	"GEOREGION":                             models.FragmentTypeGeoRegion,
	"LOCATION":                              models.FragmentTypeGeoRegion,
	"LOC":                                   models.FragmentTypeGeoRegion,
	string(models.FragmentTypeProduct):      models.FragmentTypeProduct,
	string(models.FragmentTypeProgram):      models.FragmentTypeProgram,
	string(models.FragmentTypeMarket):       models.FragmentTypeMarket,
	string(models.FragmentTypeEvent):        models.FragmentTypeEvent,
	string(models.FragmentTypePerson):       models.FragmentTypePerson,
	"HUMAN":                                 models.FragmentTypePerson,
	"INDIVIDUAL":                            models.FragmentTypePerson,
	string(models.FragmentTypeMediaChannel): models.FragmentTypeMediaChannel,
	"MEDIA":                                 models.FragmentTypeMediaChannel,
	"CHANNEL":                               models.FragmentTypeMediaChannel,
	"MEDIACHANNEL":                          models.FragmentTypeMediaChannel,
	string(models.FragmentTypeDeposit):      models.FragmentTypeDeposit,
	"MINE":                                  models.FragmentTypeDeposit,
	"FIELD":                                 models.FragmentTypeDeposit,
	"BASIN":                                 models.FragmentTypeDeposit,
	"RESERVOIR":                             models.FragmentTypeDeposit,

	// Strategic / contract overlay types
	string(models.FragmentTypeDomain):        models.FragmentTypeDomain,
	string(models.FragmentTypeSphere):        models.FragmentTypeSphere,
	string(models.FragmentTypeDecisionPoint): models.FragmentTypeDecisionPoint,
	"DECISION":                               models.FragmentTypeDecisionPoint,
	"DECISIONPOINT":                          models.FragmentTypeDecisionPoint,
	string(models.FragmentTypeMechanism):     models.FragmentTypeMechanism,

	// Runtime-specific types
	string(models.FragmentTypeSupplyLink): models.FragmentTypeSupplyLink,
	"SUPPLYLINK":                          models.FragmentTypeSupplyLink,
	string(models.FragmentTypeCommunity):  models.FragmentTypeCommunity,
	"NETWORK":                             models.FragmentTypeCommunity,
	"GROUP":                               models.FragmentTypeCommunity,
	"COLLECTIVE":                          models.FragmentTypeCommunity,
}
