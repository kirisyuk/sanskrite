package edgesemantics

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// NormalizeFragmentClassToken maps canonical AH class labels plus a small set
// of human-readable shorthands onto an AH FragmentClass.
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

// fragmentClassAliases is the closed set of human-readable non-wire aliases we
// still accept in helper surfaces. Stored plans and APIs must use the
// canonical AH fragment classes directly.
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

	"MARKET": {ah.FragmentClassMarket, ""},

	"EVIDENCE": {ah.FragmentClassEvidence, ""},

	"PERSON":     {ah.FragmentClassPerson, ""},
	"HUMAN":      {ah.FragmentClassPerson, ""},
	"INDIVIDUAL": {ah.FragmentClassPerson, ""},

	"MEDIA":         {ah.FragmentClassOrg, "media_channel"},
	"CHANNEL":       {ah.FragmentClassOrg, "media_channel"},
	"MEDIA_CHANNEL": {ah.FragmentClassOrg, "media_channel"},

	"DEPOSIT":   {ah.FragmentClassSite, "deposit"},
	"MINE":      {ah.FragmentClassSite, "deposit"},
	"FIELD":     {ah.FragmentClassSite, "deposit"},
	"BASIN":     {ah.FragmentClassSite, "deposit"},
	"RESERVOIR": {ah.FragmentClassSite, "deposit"},

	"DECISION":       {ah.FragmentClassDecision, ""},
	"DECISIONPOINT":  {ah.FragmentClassDecision, ""},
	"DECISION_POINT": {ah.FragmentClassDecision, ""},

	"COMMUNITY":  {ah.FragmentClassCommunity, ""},
	"NETWORK":    {ah.FragmentClassCommunity, ""},
	"GROUP":      {ah.FragmentClassCommunity, ""},
	"COLLECTIVE": {ah.FragmentClassCommunity, ""},

	"AFFECTEDZONE": {ah.FragmentClassGeo, "affected_zone"},
	"ZONE":         {ah.FragmentClassGeo, "affected_zone"},

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

	// Legacy supply-link overlay (no underscore in some LLM/tool outputs).
	"SUPPLY_LINK": {ah.FragmentClassSite, "supply_link"},
	"SUPPLYLINK":  {ah.FragmentClassSite, "supply_link"},
}
