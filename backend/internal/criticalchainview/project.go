package criticalchainview

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/criticalchainview/config"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
	"google.golang.org/genproto/googleapis/type/latlng"
)

const globalTenant = "global"

func attrString(bag map[string]interface{}, key string) string {
	if bag == nil {
		return ""
	}
	v, ok := bag[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func productCategoryFromFragment(f *horizonmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if s := attrString(f.Attributes, "category"); s != "" {
		return s
	}
	if s := attrString(f.Attributes, "product_category"); s != "" {
		return s
	}
	if f.Labels != nil {
		if s := strings.TrimSpace(f.Labels["category"]); s != "" {
			return s
		}
	}
	return ""
}

func materialSlugFromFragment(f *horizonmodels.HorizonFragment) string {
	if f == nil {
		return ""
	}
	if s := attrString(f.Attributes, "critical_chain_material_id"); s != "" {
		return s
	}
	path := strings.Trim(f.CanonicalPath, "/")
	parts := strings.Split(path, "/")
	if len(parts) >= 2 && parts[0] == "product" {
		return parts[len(parts)-1]
	}
	return ""
}

func facilityKindFromSubtype(sub string) taxonomy.FacilityKind {
	switch strings.TrimSpace(strings.ToLower(sub)) {
	case "deposit":
		return taxonomy.FacilityKindMine
	case "refinery":
		return taxonomy.FacilityKindRefinery
	case "processing_plant":
		return taxonomy.FacilityKindOther
	case "factory":
		return taxonomy.FacilityKindFactory
	default:
		return taxonomy.FacilityKindOther
	}
}

func latLngFromFragment(f *horizonmodels.HorizonFragment) *latlng.LatLng {
	if f == nil {
		return nil
	}
	if f.Coordinates != nil && horizonmodels.IsValidCoordinates(f.Coordinates.Lat, f.Coordinates.Lon) {
		return &latlng.LatLng{Latitude: f.Coordinates.Lat, Longitude: f.Coordinates.Lon}
	}
	return nil
}

func facilityFromSiteFragment(f *horizonmodels.HorizonFragment, materialHint string) *Facility {
	if f == nil || f.Class != ah.FragmentClassSite {
		return nil
	}
	sub := strings.TrimSpace(strings.ToLower(f.Subtype))
	if sub == "route" || sub == "supply_link" {
		return nil
	}
	name := strings.TrimSpace(f.Description)
	if name == "" {
		name = f.CanonicalPath
	}
	country := strings.TrimSpace(f.GeoSlug)
	if country == "" {
		country = attrString(f.Attributes, string(ah.AttrKeyGeoSlug))
	}
	matID := materialHint
	if matID == "" {
		matID = attrString(f.Attributes, "material_id")
	}
	stage := 0
	if s := attrString(f.Attributes, "production_stage_id"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			stage = n
		}
	}
	risk := 1.0
	if f.State != nil {
		if rv, ok := f.State["risk_score"]; ok && rv.Value != 0 {
			risk = rv.Value
		}
	}
	return &Facility{
		ID:            f.ID,
		CanonicalPath: f.CanonicalPath,
		Name:          name,
		FacilityKind:  facilityKindFromSubtype(f.Subtype),
		Country:       strings.ToUpper(country),
		Coordinates:   latLngFromFragment(f),
		MaterialID:    matID,
		Stage:         stage,
		RiskScore:     risk,
		UpdatedAt:     f.UpdatedAt,
	}
}

func conflictZoneFromGeoFragment(f *horizonmodels.HorizonFragment) *ConflictZone {
	if f == nil {
		return nil
	}
	sev := ConflictZoneSeverity(strings.ToUpper(attrString(f.Attributes, string(ah.AttrKeySeverity))))
	if sev == "" {
		sev = SeverityMedium
	}
	czType := ConflictTypeArmed
	if dk := strings.ToUpper(attrString(f.Attributes, string(ah.AttrKeyDisruptionKind))); dk != "" {
		switch dk {
		case "SANCTIONS", "SANCTION":
			czType = ConflictTypeSanctions
		case "NATURAL", "WEATHER", "GEO":
			czType = ConflictTypeNatural
		case "POLITICAL":
			czType = ConflictTypePolitical
		}
	}
	name := strings.TrimSpace(f.Description)
	if name == "" {
		name = f.CanonicalPath
	}
	country := strings.TrimSpace(f.GeoSlug)
	if country == "" {
		country = attrString(f.Attributes, string(ah.AttrKeyGeoSlug))
	}
	return &ConflictZone{
		ID:        f.ID,
		Name:      name,
		Country:   strings.ToUpper(country),
		Severity:  sev,
		Type:      czType,
		Source:    "horizon_geo",
		UpdatedAt: f.UpdatedAt,
	}
}

func routeTypeFromAttrs(f *horizonmodels.HorizonFragment) RouteType {
	mode := strings.ToUpper(attrString(f.Attributes, string(ah.AttrKeyMode)))
	switch mode {
	case "SEA", "MARINE":
		return RouteTypeSea
	case "AIR":
		return RouteTypeAir
	case "PIPELINE":
		return RouteTypePipeline
	case "RAIL":
		return RouteTypeRail
	case "LAND", "ROAD":
		return RouteTypeLand
	default:
		return RouteTypeSea
	}
}

func routeFromAHFragment(f *horizonmodels.HorizonFragment, fromID, toID string) *Route {
	if f == nil {
		return nil
	}
	return &Route{
		ID:           f.ID,
		Type:         routeTypeFromAttrs(f),
		FromFacility: fromID,
		ToFacility:   toID,
		RiskScore:    1,
		UpdatedAt:    f.UpdatedAt,
	}
}

func stepTypeForAHRelation(rel ah.RelationName) ProcessingStepType {
	switch rel {
	case ah.RelationNameProduces:
		return StepProcessing
	case ah.RelationNameSupplies:
		return StepTransport
	default:
		return StepProcessing
	}
}

func materialFromDefinition(def *config.MaterialDefinition) *Material {
	if def == nil {
		return nil
	}
	name := def.DisplayName["en"]
	if name == "" {
		name = def.MaterialID
	}
	return &Material{
		ID:               def.MaterialID,
		Name:             name,
		Category:         def.Category,
		CriticalityScore: 3.0,
		RiskScore:        1.0,
	}
}
