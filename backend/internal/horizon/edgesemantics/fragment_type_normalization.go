package edgesemantics

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// NormalizeFragmentClassToken maps assorted type labels (modern AH
// classes, legacy UPPER_SNAKE FragmentType values, and human shorthands
// like "FAC" / "PLANT") onto an AH FragmentClass, plus an optional
// subtype hint when the legacy alias collapses into (class, subtype).
//
// The function NEVER returns an empty class: when nothing matches it
// falls back to ah.FragmentClassOrg with ok=false so callers can warn.
func NormalizeFragmentClassToken(raw string) (ah.FragmentClass, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ah.FragmentClassOrg, "", false
	}
	if class, sub, ok := ExpectedClassFromCanonicalPath(strings.ToLower(raw)); ok {
		return class, sub, true
	}
	token := normalizeTypeToken(raw)
	if entry, ok := fragmentClassAliases[token]; ok {
		return entry.class, entry.subtype, true
	}
	if class, sub, ok := models.NormalizeFragmentClassName(token, ""); ok {
		return class, sub, true
	}
	return ah.FragmentClassOrg, "", false
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

type fragmentClassAlias struct {
	class   ah.FragmentClass
	subtype string
}

// fragmentClassAliases is the closed set of human-shorthand aliases.
// Every legacy UPPER_SNAKE FragmentType is normalised via the
// migration-map-derived models.NormalizeFragmentClassName fallback
// (see relation_alias.go); this map only adds the colloquial aliases
// (FAC / PLANT / NETWORK / etc.) that aren't part of the legacy enum
// but appear in LLM output and tool input.
var fragmentClassAliases = map[string]fragmentClassAlias{
	"ORG":             {ah.FragmentClassOrg, ""},
	"ORGANISATION":    {ah.FragmentClassOrg, ""},
	"GOV":             {ah.FragmentClassOrg, "gov"},
	"GOVERNMENT":      {ah.FragmentClassOrg, "gov"},
	"GOVERNMENT_BODY": {ah.FragmentClassOrg, "gov"},
	"PUBLIC_BODY":     {ah.FragmentClassOrg, "gov"},

	"FAC":   {ah.FragmentClassSite, ""},
	"PLANT": {ah.FragmentClassSite, ""},
	"SITE":  {ah.FragmentClassSite, ""},

	"REGION":     {ah.FragmentClassGeo, ""},
	"GEO":        {ah.FragmentClassGeo, ""},
	"GEOREGION":  {ah.FragmentClassGeo, ""},
	"GEO_REGION": {ah.FragmentClassGeo, ""},
	"LOCATION":   {ah.FragmentClassGeo, ""},
	"LOC":        {ah.FragmentClassGeo, ""},

	"PRODUCT": {ah.FragmentClassProduct, ""},

	"PROGRAM": {ah.FragmentClassOrg, "program"},
	"MARKET":  {ah.FragmentClassMarket, ""},

	"EVENT":    {ah.FragmentClassEvidence, "event"},
	"EVIDENCE": {ah.FragmentClassEvidence, ""},

	"PERSON":     {ah.FragmentClassPerson, ""},
	"HUMAN":      {ah.FragmentClassPerson, ""},
	"INDIVIDUAL": {ah.FragmentClassPerson, ""},

	"MEDIA_CHANNEL": {ah.FragmentClassOrg, "media_channel"},
	"MEDIA":         {ah.FragmentClassOrg, "media_channel"},
	"CHANNEL":       {ah.FragmentClassOrg, "media_channel"},
	"MEDIACHANNEL":  {ah.FragmentClassOrg, "media_channel"},

	"DEPOSIT":   {ah.FragmentClassSite, "deposit"},
	"MINE":      {ah.FragmentClassSite, "deposit"},
	"FIELD":     {ah.FragmentClassSite, "deposit"},
	"BASIN":     {ah.FragmentClassSite, "deposit"},
	"RESERVOIR": {ah.FragmentClassSite, "deposit"},

	"DECISION":       {ah.FragmentClassDecision, ""},
	"DECISIONPOINT":  {ah.FragmentClassDecision, ""},
	"DECISION_POINT": {ah.FragmentClassDecision, ""},
	"MECHANISM":      {ah.FragmentClassMechanism, ""},

	"COMMUNITY":  {ah.FragmentClassCommunity, ""},
	"NETWORK":    {ah.FragmentClassCommunity, ""},
	"GROUP":      {ah.FragmentClassCommunity, ""},
	"COLLECTIVE": {ah.FragmentClassCommunity, ""},

	"AFFECTED_ZONE": {ah.FragmentClassGeo, "affected_zone"},
	"AFFECTEDZONE":  {ah.FragmentClassGeo, "affected_zone"},
	"ZONE":          {ah.FragmentClassGeo, "affected_zone"},

	"REGIME":  {ah.FragmentClassRegime, ""},
	"ROUTE":   {ah.FragmentClassRoute, ""},
	"PROJECT": {ah.FragmentClassProject, ""},

	"VESSEL_CLASS": {ah.FragmentClassVesselClass, ""},
	"VESSELCLASS":  {ah.FragmentClassVesselClass, ""},

	"MOBILE_ASSET": {ah.FragmentClassMobileAsset, ""},
	"MOBILEASSET":  {ah.FragmentClassMobileAsset, ""},
	"VESSEL":       {ah.FragmentClassMobileAsset, ""},
	"AIRCRAFT":     {ah.FragmentClassMobileAsset, ""},
	"TRACK":        {ah.FragmentClassMobileAsset, ""},

	// Legacy strategic overlays — folded but recognised so legacy LLM
	// output still resolves to a valid class for normalisation.
	"DOMAIN":      {ah.FragmentClassEvidence, "domain_legacy"},
	"SPHERE":      {ah.FragmentClassEvidence, "sphere_legacy"},
	"SUPPLY_LINK": {ah.FragmentClassSite, "supply_link"},
	"SUPPLYLINK":  {ah.FragmentClassSite, "supply_link"},
}
