package criticalchainview

import (
	"encoding/json"
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
	case "deposit", "mine":
		return taxonomy.FacilityKindMine
	case "refinery":
		return taxonomy.FacilityKindRefinery
	case "processing_plant", "processor":
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

func facilityFromSiteFragment(f *horizonmodels.HorizonFragment, materialHint string, sanctioned bool) *Facility {
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
		if rv, ok := f.State["risk_score"]; ok {
			risk = rv.Value
		}
	}
	var operators []string
	if op := strings.TrimSpace(attrString(f.Attributes, string(ah.AttrKeyOperatorOrgPath))); op != "" {
		operators = []string{op}
	}
	capacity, capUnit := facilityCapacityFromState(f)
	prodMats := productionMaterialPathsFromSite(f)
	out := &Facility{
		ID:                  f.ID,
		CanonicalPath:       f.CanonicalPath,
		Name:                name,
		FacilityKind:        facilityKindFromSubtype(f.Subtype),
		Country:             strings.ToUpper(country),
		Coordinates:         latLngFromFragment(f),
		Operators:           operators,
		Capacity:            capacity,
		CapacityUnit:        capUnit,
		ProductionMaterials: prodMats,
		MaterialID:          matID,
		Stage:               stage,
		RiskScore:           risk,
		Sanctioned:          sanctioned,
		UpdatedAt:           f.UpdatedAt,
	}
	return out
}

func facilityCapacityFromState(f *horizonmodels.HorizonFragment) (float64, string) {
	if f == nil || f.State == nil {
		return 0, ""
	}
	if m, ok := f.State["capacity"]; ok {
		unit := ""
		if m.Unit != nil {
			unit = strings.TrimSpace(*m.Unit)
		}
		return m.Value, unit
	}
	return 0, ""
}

func productionMaterialPathsFromSite(f *horizonmodels.HorizonFragment) []string {
	if f == nil {
		return nil
	}
	var out []string
	for _, st := range f.SiteTransforms() {
		out = append(out, st.OutputProducts...)
	}
	return dedupeStringsKeepOrder(out)
}

func dedupeStringsKeepOrder(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func stringSliceFromAttr(bag map[string]interface{}, key string) []string {
	if bag == nil {
		return nil
	}
	raw, ok := bag[key]
	if !ok || raw == nil {
		return nil
	}
	switch t := raw.(type) {
	case []string:
		return append([]string(nil), t...)
	case []interface{}:
		var out []string
		for _, x := range t {
			switch v := x.(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					out = append(out, strings.TrimSpace(v))
				}
			}
		}
		return out
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		parts := strings.Split(t, ",")
		var out []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	default:
		return nil
	}
}

func riskHistoryFromFragment(p *horizonmodels.HorizonFragment) []RiskPoint {
	if p == nil || p.Attributes == nil {
		return nil
	}
	raw, ok := p.Attributes["risk_history"]
	if !ok || raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var pts []RiskPoint
	if err := json.Unmarshal(data, &pts); err != nil {
		return nil
	}
	return pts
}

// ApplyAHProductOverlay merges AH product fragment fields into the CriticalChain Material wire row.
func ApplyAHProductOverlay(m *Material, p *horizonmodels.HorizonFragment) {
	if m == nil || p == nil {
		return
	}
	if p.State != nil {
		if rv, ok := p.State["risk_score"]; ok {
			m.RiskScore = rv.Value
		}
		if cv, ok := p.State["criticality_score"]; ok {
			m.CriticalityScore = cv.Value
		}
	}
	if v := attrString(p.Attributes, "criticality_score"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			m.CriticalityScore = f
		}
	}
	if hs := stringSliceFromAttr(p.Attributes, "hs_codes"); len(hs) > 0 {
		m.HSCodes = hs
	}
	if hs := stringSliceFromAttr(p.Attributes, "hscodes"); len(hs) > 0 && len(m.HSCodes) == 0 {
		m.HSCodes = hs
	}
	if v := attrString(p.Attributes, "cas_number"); v != "" {
		m.CASNumber = v
	}
	if m.CASNumber == "" && p.Identifiers != nil {
		if v := strings.TrimSpace(p.Identifiers["cas"]); v != "" {
			m.CASNumber = v
		}
	}
	if len(p.AlternativeNames) > 0 {
		m.Aliases = append([]string(nil), p.AlternativeNames...)
	}
	if rh := riskHistoryFromFragment(p); len(rh) > 0 {
		m.RiskHistory = rh
	}
	if !p.UpdatedAt.IsZero() {
		m.UpdatedAt = p.UpdatedAt
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
	cz := &ConflictZone{
		ID:        f.ID,
		Name:      name,
		Country:   strings.ToUpper(country),
		Severity:  sev,
		Type:      czType,
		Source:    "horizon_geo",
		UpdatedAt: f.UpdatedAt,
	}
	if am := stringSliceFromAttr(f.Attributes, "affected_materials"); len(am) > 0 {
		cz.AffectedMaterials = am
	}
	if len(cz.AffectedMaterials) == 0 {
		if am := stringSliceFromAttr(f.Attributes, "affectedMaterials"); len(am) > 0 {
			cz.AffectedMaterials = am
		}
	}
	return cz
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
	wp := waypointsFromGeometryAttr(f.Attributes[string(ah.AttrKeyGeometry)])
	return &Route{
		ID:           f.ID,
		Type:         routeTypeFromAttrs(f),
		Waypoints:    wp,
		FromFacility: fromID,
		ToFacility:   toID,
		RiskScore:    1,
		UpdatedAt:    f.UpdatedAt,
	}
}

// waypointsFromGeometryAttr extracts vertices from a GeoJSON-like geometry value (LineString / MultiPoint).
func waypointsFromGeometryAttr(raw interface{}) []*latlng.LatLng {
	if raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var envelope struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(envelope.Type)) {
	case "linestring":
		var coords [][]float64
		if err := json.Unmarshal(envelope.Coordinates, &coords); err != nil {
			return nil
		}
		return coordsToWaypoints(coords)
	case "multipoint":
		var coords [][]float64
		if err := json.Unmarshal(envelope.Coordinates, &coords); err != nil {
			return nil
		}
		return coordsToWaypoints(coords)
	default:
		return nil
	}
}

func coordsToWaypoints(coords [][]float64) []*latlng.LatLng {
	var out []*latlng.LatLng
	for _, c := range coords {
		if len(c) < 2 {
			continue
		}
		lon, lat := c[0], c[1]
		if !horizonmodels.IsValidCoordinates(lat, lon) {
			continue
		}
		out = append(out, &latlng.LatLng{Latitude: lat, Longitude: lon})
	}
	return out
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
