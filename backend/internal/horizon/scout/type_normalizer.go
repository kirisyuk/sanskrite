package scout

import (
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// =============================================================================
// TYPE NORMALIZATION POLICY
// =============================================================================
// Entities are sometimes incorrectly typed (e.g., country named as ORGANIZATION).
// This policy normalizes types based on well-known entity lists.
//
// Priority:
// 1. Check if legalName/labels match known country names → GEO_REGION
// 2. Check canonicalPath prefix patterns → infer correct type
// 3. Keep original type if no rule matches

// TypeNormalizer handles type normalization for fragments.
type TypeNormalizer struct{}

// NewTypeNormalizer creates a new type normalizer.
func NewTypeNormalizer() *TypeNormalizer {
	return &TypeNormalizer{}
}

// NormalizationResult contains the result of type normalization.
type NormalizationResult struct {
	ShouldNormalize bool
	OriginalType    models.FragmentType
	NormalizedType  models.FragmentType
	CanonicalPath   string // Suggested canonical path (e.g., /loc/us/na/na for USA)
	Reason          string
	MergeWithPath   string // If should merge with existing fragment at this path
}

// NormalizeFragmentType checks if a fragment should have its type corrected.
// Returns NormalizationResult with suggested changes.
func (n *TypeNormalizer) NormalizeFragmentType(fragment *models.HorizonFragment) NormalizationResult {
	if fragment == nil {
		return NormalizationResult{ShouldNormalize: false}
	}

	// Rule 1: Check if this is a country incorrectly typed as ORGANIZATION
	if fragment.Type == models.FragmentTypeOrganization {
		if result := n.checkIfCountry(fragment); result.ShouldNormalize {
			return result
		}
	}

	// Rule 2: Check canonicalPath prefix vs type mismatch
	if result := n.checkPathTypeMismatch(fragment); result.ShouldNormalize {
		return result
	}

	return NormalizationResult{ShouldNormalize: false}
}

// checkIfCountry checks if fragment is actually a country.
func (n *TypeNormalizer) checkIfCountry(fragment *models.HorizonFragment) NormalizationResult {
	// Get all possible names to check
	namesToCheck := []string{
		strings.ToLower(strings.TrimSpace(fragment.Identifiers["legalName"])),
		strings.ToLower(strings.TrimSpace(fragment.Labels["en"])),
		strings.ToLower(strings.TrimSpace(fragment.Identifiers["shortName"])),
	}

	for _, name := range namesToCheck {
		if name == "" {
			continue
		}
		if country, ok := KnownCountries[name]; ok {
			return NormalizationResult{
				ShouldNormalize: true,
				OriginalType:    fragment.Type,
				NormalizedType:  models.FragmentTypeGeoRegion,
				CanonicalPath:   country.CanonicalPath,
				Reason:          "legalName/label matches known country: " + country.EnglishName,
				MergeWithPath:   country.CanonicalPath,
			}
		}
		// Also check against aliases
		if cp, ok := CountryAliases[name]; ok {
			country := KnownCountriesByPath[cp]
			return NormalizationResult{
				ShouldNormalize: true,
				OriginalType:    fragment.Type,
				NormalizedType:  models.FragmentTypeGeoRegion,
				CanonicalPath:   cp,
				Reason:          "legalName/label matches country alias: " + country.EnglishName,
				MergeWithPath:   cp,
			}
		}
	}

	return NormalizationResult{ShouldNormalize: false}
}

// checkPathTypeMismatch checks if canonicalPath prefix doesn't match type.
func (n *TypeNormalizer) checkPathTypeMismatch(fragment *models.HorizonFragment) NormalizationResult {
	path := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	if path == "" {
		return NormalizationResult{ShouldNormalize: false}
	}

	// Map of path prefixes to expected types
	pathTypeMap := map[string]models.FragmentType{
		"/loc/":         models.FragmentTypeGeoRegion,
		"/geo/":         models.FragmentTypeGeoRegion,
		"/org/":         models.FragmentTypeOrganization,
		"/fac/":         models.FragmentTypeFacility,
		"/deposit/":     models.FragmentTypeDeposit, // Natural resource deposits
		"/product/":     models.FragmentTypeProduct,
		"/program/":     models.FragmentTypeProgram,
		"/market/":      models.FragmentTypeMarket,
		"/event/":       models.FragmentTypeEvent,
		"/person/":      models.FragmentTypePerson,
		"/media/":       models.FragmentTypeMediaChannel,
		"/supply-link/": models.FragmentTypeSupplyLink,
	}

	for prefix, expectedType := range pathTypeMap {
		if strings.HasPrefix(path, prefix) && fragment.Type != expectedType {
			return NormalizationResult{
				ShouldNormalize: true,
				OriginalType:    fragment.Type,
				NormalizedType:  expectedType,
				CanonicalPath:   fragment.CanonicalPath,
				Reason:          "canonicalPath prefix " + prefix + " requires type " + string(expectedType),
			}
		}
	}

	return NormalizationResult{ShouldNormalize: false}
}

// =============================================================================
// KNOWN COUNTRIES DATABASE
// =============================================================================

// CountryInfo contains information about a known country.
type CountryInfo struct {
	ISO2          string // ISO 3166-1 alpha-2 code (lowercase)
	EnglishName   string // Official English name
	CanonicalPath string // Canonical path: /loc/{iso2}/na/na
}

// KnownCountries maps lowercase country names to their canonical info.
// Key: lowercase full name or common name
var KnownCountries = map[string]CountryInfo{
	// Major countries (most likely to be mistyped as ORG)
	"united states of america": {ISO2: "us", EnglishName: "United States of America", CanonicalPath: "/loc/us/na/na"},
	"united states":            {ISO2: "us", EnglishName: "United States of America", CanonicalPath: "/loc/us/na/na"},
	"usa":                      {ISO2: "us", EnglishName: "United States of America", CanonicalPath: "/loc/us/na/na"},
	"america":                  {ISO2: "us", EnglishName: "United States of America", CanonicalPath: "/loc/us/na/na"},

	"people's republic of china": {ISO2: "cn", EnglishName: "People's Republic of China", CanonicalPath: "/loc/cn/na/na"},
	"china":                      {ISO2: "cn", EnglishName: "People's Republic of China", CanonicalPath: "/loc/cn/na/na"},
	"prc":                        {ISO2: "cn", EnglishName: "People's Republic of China", CanonicalPath: "/loc/cn/na/na"},

	"russian federation": {ISO2: "ru", EnglishName: "Russian Federation", CanonicalPath: "/loc/ru/na/na"},
	"russia":             {ISO2: "ru", EnglishName: "Russian Federation", CanonicalPath: "/loc/ru/na/na"},

	"united kingdom of great britain and northern ireland": {ISO2: "gb", EnglishName: "United Kingdom", CanonicalPath: "/loc/gb/na/na"},
	"united kingdom": {ISO2: "gb", EnglishName: "United Kingdom", CanonicalPath: "/loc/gb/na/na"},
	"great britain":  {ISO2: "gb", EnglishName: "United Kingdom", CanonicalPath: "/loc/gb/na/na"},
	"uk":             {ISO2: "gb", EnglishName: "United Kingdom", CanonicalPath: "/loc/gb/na/na"},
	"britain":        {ISO2: "gb", EnglishName: "United Kingdom", CanonicalPath: "/loc/gb/na/na"},

	"federal republic of germany": {ISO2: "de", EnglishName: "Germany", CanonicalPath: "/loc/de/na/na"},
	"germany":                     {ISO2: "de", EnglishName: "Germany", CanonicalPath: "/loc/de/na/na"},
	"deutschland":                 {ISO2: "de", EnglishName: "Germany", CanonicalPath: "/loc/de/na/na"},

	"french republic": {ISO2: "fr", EnglishName: "France", CanonicalPath: "/loc/fr/na/na"},
	"france":          {ISO2: "fr", EnglishName: "France", CanonicalPath: "/loc/fr/na/na"},

	"japan": {ISO2: "jp", EnglishName: "Japan", CanonicalPath: "/loc/jp/na/na"},

	"republic of korea":                     {ISO2: "kr", EnglishName: "South Korea", CanonicalPath: "/loc/kr/na/na"},
	"south korea":                           {ISO2: "kr", EnglishName: "South Korea", CanonicalPath: "/loc/kr/na/na"},
	"korea":                                 {ISO2: "kr", EnglishName: "South Korea", CanonicalPath: "/loc/kr/na/na"},
	"democratic people's republic of korea": {ISO2: "kp", EnglishName: "North Korea", CanonicalPath: "/loc/kp/na/na"},
	"north korea":                           {ISO2: "kp", EnglishName: "North Korea", CanonicalPath: "/loc/kp/na/na"},
	"dprk":                                  {ISO2: "kp", EnglishName: "North Korea", CanonicalPath: "/loc/kp/na/na"},

	"republic of india": {ISO2: "in", EnglishName: "India", CanonicalPath: "/loc/in/na/na"},
	"india":             {ISO2: "in", EnglishName: "India", CanonicalPath: "/loc/in/na/na"},

	"federative republic of brazil": {ISO2: "br", EnglishName: "Brazil", CanonicalPath: "/loc/br/na/na"},
	"brazil":                        {ISO2: "br", EnglishName: "Brazil", CanonicalPath: "/loc/br/na/na"},

	"canada": {ISO2: "ca", EnglishName: "Canada", CanonicalPath: "/loc/ca/na/na"},

	"commonwealth of australia": {ISO2: "au", EnglishName: "Australia", CanonicalPath: "/loc/au/na/na"},
	"australia":                 {ISO2: "au", EnglishName: "Australia", CanonicalPath: "/loc/au/na/na"},

	"italian republic": {ISO2: "it", EnglishName: "Italy", CanonicalPath: "/loc/it/na/na"},
	"italy":            {ISO2: "it", EnglishName: "Italy", CanonicalPath: "/loc/it/na/na"},

	"kingdom of spain": {ISO2: "es", EnglishName: "Spain", CanonicalPath: "/loc/es/na/na"},
	"spain":            {ISO2: "es", EnglishName: "Spain", CanonicalPath: "/loc/es/na/na"},

	"united mexican states": {ISO2: "mx", EnglishName: "Mexico", CanonicalPath: "/loc/mx/na/na"},
	"mexico":                {ISO2: "mx", EnglishName: "Mexico", CanonicalPath: "/loc/mx/na/na"},

	"republic of indonesia": {ISO2: "id", EnglishName: "Indonesia", CanonicalPath: "/loc/id/na/na"},
	"indonesia":             {ISO2: "id", EnglishName: "Indonesia", CanonicalPath: "/loc/id/na/na"},

	"kingdom of the netherlands": {ISO2: "nl", EnglishName: "Netherlands", CanonicalPath: "/loc/nl/na/na"},
	"netherlands":                {ISO2: "nl", EnglishName: "Netherlands", CanonicalPath: "/loc/nl/na/na"},
	"holland":                    {ISO2: "nl", EnglishName: "Netherlands", CanonicalPath: "/loc/nl/na/na"},

	"kingdom of saudi arabia": {ISO2: "sa", EnglishName: "Saudi Arabia", CanonicalPath: "/loc/sa/na/na"},
	"saudi arabia":            {ISO2: "sa", EnglishName: "Saudi Arabia", CanonicalPath: "/loc/sa/na/na"},

	"swiss confederation": {ISO2: "ch", EnglishName: "Switzerland", CanonicalPath: "/loc/ch/na/na"},
	"switzerland":         {ISO2: "ch", EnglishName: "Switzerland", CanonicalPath: "/loc/ch/na/na"},

	"republic of turkey": {ISO2: "tr", EnglishName: "Turkey", CanonicalPath: "/loc/tr/na/na"},
	"turkey":             {ISO2: "tr", EnglishName: "Turkey", CanonicalPath: "/loc/tr/na/na"},
	"türkiye":            {ISO2: "tr", EnglishName: "Turkey", CanonicalPath: "/loc/tr/na/na"},

	"kingdom of sweden": {ISO2: "se", EnglishName: "Sweden", CanonicalPath: "/loc/se/na/na"},
	"sweden":            {ISO2: "se", EnglishName: "Sweden", CanonicalPath: "/loc/se/na/na"},

	"republic of poland": {ISO2: "pl", EnglishName: "Poland", CanonicalPath: "/loc/pl/na/na"},
	"poland":             {ISO2: "pl", EnglishName: "Poland", CanonicalPath: "/loc/pl/na/na"},

	"kingdom of belgium": {ISO2: "be", EnglishName: "Belgium", CanonicalPath: "/loc/be/na/na"},
	"belgium":            {ISO2: "be", EnglishName: "Belgium", CanonicalPath: "/loc/be/na/na"},

	"republic of austria": {ISO2: "at", EnglishName: "Austria", CanonicalPath: "/loc/at/na/na"},
	"austria":             {ISO2: "at", EnglishName: "Austria", CanonicalPath: "/loc/at/na/na"},

	"kingdom of norway": {ISO2: "no", EnglishName: "Norway", CanonicalPath: "/loc/no/na/na"},
	"norway":            {ISO2: "no", EnglishName: "Norway", CanonicalPath: "/loc/no/na/na"},

	"kingdom of denmark": {ISO2: "dk", EnglishName: "Denmark", CanonicalPath: "/loc/dk/na/na"},
	"denmark":            {ISO2: "dk", EnglishName: "Denmark", CanonicalPath: "/loc/dk/na/na"},

	"republic of finland": {ISO2: "fi", EnglishName: "Finland", CanonicalPath: "/loc/fi/na/na"},
	"finland":             {ISO2: "fi", EnglishName: "Finland", CanonicalPath: "/loc/fi/na/na"},

	"hellenic republic": {ISO2: "gr", EnglishName: "Greece", CanonicalPath: "/loc/gr/na/na"},
	"greece":            {ISO2: "gr", EnglishName: "Greece", CanonicalPath: "/loc/gr/na/na"},

	"portuguese republic": {ISO2: "pt", EnglishName: "Portugal", CanonicalPath: "/loc/pt/na/na"},
	"portugal":            {ISO2: "pt", EnglishName: "Portugal", CanonicalPath: "/loc/pt/na/na"},

	"ireland": {ISO2: "ie", EnglishName: "Ireland", CanonicalPath: "/loc/ie/na/na"},

	"new zealand": {ISO2: "nz", EnglishName: "New Zealand", CanonicalPath: "/loc/nz/na/na"},

	"republic of singapore": {ISO2: "sg", EnglishName: "Singapore", CanonicalPath: "/loc/sg/na/na"},
	"singapore":             {ISO2: "sg", EnglishName: "Singapore", CanonicalPath: "/loc/sg/na/na"},

	"hong kong special administrative region": {ISO2: "hk", EnglishName: "Hong Kong", CanonicalPath: "/loc/hk/na/na"},
	"hong kong": {ISO2: "hk", EnglishName: "Hong Kong", CanonicalPath: "/loc/hk/na/na"},

	"republic of china": {ISO2: "tw", EnglishName: "Taiwan", CanonicalPath: "/loc/tw/na/na"},
	"taiwan":            {ISO2: "tw", EnglishName: "Taiwan", CanonicalPath: "/loc/tw/na/na"},

	"state of israel": {ISO2: "il", EnglishName: "Israel", CanonicalPath: "/loc/il/na/na"},
	"israel":          {ISO2: "il", EnglishName: "Israel", CanonicalPath: "/loc/il/na/na"},

	"united arab emirates": {ISO2: "ae", EnglishName: "United Arab Emirates", CanonicalPath: "/loc/ae/na/na"},
	"uae":                  {ISO2: "ae", EnglishName: "United Arab Emirates", CanonicalPath: "/loc/ae/na/na"},

	"islamic republic of iran": {ISO2: "ir", EnglishName: "Iran", CanonicalPath: "/loc/ir/na/na"},
	"iran":                     {ISO2: "ir", EnglishName: "Iran", CanonicalPath: "/loc/ir/na/na"},

	"republic of iraq": {ISO2: "iq", EnglishName: "Iraq", CanonicalPath: "/loc/iq/na/na"},
	"iraq":             {ISO2: "iq", EnglishName: "Iraq", CanonicalPath: "/loc/iq/na/na"},

	"arab republic of egypt": {ISO2: "eg", EnglishName: "Egypt", CanonicalPath: "/loc/eg/na/na"},
	"egypt":                  {ISO2: "eg", EnglishName: "Egypt", CanonicalPath: "/loc/eg/na/na"},

	"republic of south africa": {ISO2: "za", EnglishName: "South Africa", CanonicalPath: "/loc/za/na/na"},
	"south africa":             {ISO2: "za", EnglishName: "South Africa", CanonicalPath: "/loc/za/na/na"},

	"federal republic of nigeria": {ISO2: "ng", EnglishName: "Nigeria", CanonicalPath: "/loc/ng/na/na"},
	"nigeria":                     {ISO2: "ng", EnglishName: "Nigeria", CanonicalPath: "/loc/ng/na/na"},

	"argentine republic": {ISO2: "ar", EnglishName: "Argentina", CanonicalPath: "/loc/ar/na/na"},
	"argentina":          {ISO2: "ar", EnglishName: "Argentina", CanonicalPath: "/loc/ar/na/na"},

	"republic of chile": {ISO2: "cl", EnglishName: "Chile", CanonicalPath: "/loc/cl/na/na"},
	"chile":             {ISO2: "cl", EnglishName: "Chile", CanonicalPath: "/loc/cl/na/na"},

	"republic of colombia": {ISO2: "co", EnglishName: "Colombia", CanonicalPath: "/loc/co/na/na"},
	"colombia":             {ISO2: "co", EnglishName: "Colombia", CanonicalPath: "/loc/co/na/na"},

	"bolivarian republic of venezuela": {ISO2: "ve", EnglishName: "Venezuela", CanonicalPath: "/loc/ve/na/na"},
	"venezuela":                        {ISO2: "ve", EnglishName: "Venezuela", CanonicalPath: "/loc/ve/na/na"},

	"republic of peru": {ISO2: "pe", EnglishName: "Peru", CanonicalPath: "/loc/pe/na/na"},
	"peru":             {ISO2: "pe", EnglishName: "Peru", CanonicalPath: "/loc/pe/na/na"},

	"republic of the philippines": {ISO2: "ph", EnglishName: "Philippines", CanonicalPath: "/loc/ph/na/na"},
	"philippines":                 {ISO2: "ph", EnglishName: "Philippines", CanonicalPath: "/loc/ph/na/na"},

	"socialist republic of vietnam": {ISO2: "vn", EnglishName: "Vietnam", CanonicalPath: "/loc/vn/na/na"},
	"vietnam":                       {ISO2: "vn", EnglishName: "Vietnam", CanonicalPath: "/loc/vn/na/na"},

	"kingdom of thailand": {ISO2: "th", EnglishName: "Thailand", CanonicalPath: "/loc/th/na/na"},
	"thailand":            {ISO2: "th", EnglishName: "Thailand", CanonicalPath: "/loc/th/na/na"},

	"malaysia": {ISO2: "my", EnglishName: "Malaysia", CanonicalPath: "/loc/my/na/na"},

	"islamic republic of pakistan": {ISO2: "pk", EnglishName: "Pakistan", CanonicalPath: "/loc/pk/na/na"},
	"pakistan":                     {ISO2: "pk", EnglishName: "Pakistan", CanonicalPath: "/loc/pk/na/na"},

	"people's republic of bangladesh": {ISO2: "bd", EnglishName: "Bangladesh", CanonicalPath: "/loc/bd/na/na"},
	"bangladesh":                      {ISO2: "bd", EnglishName: "Bangladesh", CanonicalPath: "/loc/bd/na/na"},

	"czech republic": {ISO2: "cz", EnglishName: "Czech Republic", CanonicalPath: "/loc/cz/na/na"},
	"czechia":        {ISO2: "cz", EnglishName: "Czech Republic", CanonicalPath: "/loc/cz/na/na"},

	"romania": {ISO2: "ro", EnglishName: "Romania", CanonicalPath: "/loc/ro/na/na"},

	"hungary": {ISO2: "hu", EnglishName: "Hungary", CanonicalPath: "/loc/hu/na/na"},

	"ukraine": {ISO2: "ua", EnglishName: "Ukraine", CanonicalPath: "/loc/ua/na/na"},

	"republic of belarus": {ISO2: "by", EnglishName: "Belarus", CanonicalPath: "/loc/by/na/na"},
	"belarus":             {ISO2: "by", EnglishName: "Belarus", CanonicalPath: "/loc/by/na/na"},

	"republic of kazakhstan": {ISO2: "kz", EnglishName: "Kazakhstan", CanonicalPath: "/loc/kz/na/na"},
	"kazakhstan":             {ISO2: "kz", EnglishName: "Kazakhstan", CanonicalPath: "/loc/kz/na/na"},

	"republic of uzbekistan": {ISO2: "uz", EnglishName: "Uzbekistan", CanonicalPath: "/loc/uz/na/na"},
	"uzbekistan":             {ISO2: "uz", EnglishName: "Uzbekistan", CanonicalPath: "/loc/uz/na/na"},

	"republic of azerbaijan": {ISO2: "az", EnglishName: "Azerbaijan", CanonicalPath: "/loc/az/na/na"},
	"azerbaijan":             {ISO2: "az", EnglishName: "Azerbaijan", CanonicalPath: "/loc/az/na/na"},

	"georgia": {ISO2: "ge", EnglishName: "Georgia", CanonicalPath: "/loc/ge/na/na"},

	"republic of armenia": {ISO2: "am", EnglishName: "Armenia", CanonicalPath: "/loc/am/na/na"},
	"armenia":             {ISO2: "am", EnglishName: "Armenia", CanonicalPath: "/loc/am/na/na"},

	"kyrgyz republic": {ISO2: "kg", EnglishName: "Kyrgyzstan", CanonicalPath: "/loc/kg/na/na"},
	"kyrgyzstan":      {ISO2: "kg", EnglishName: "Kyrgyzstan", CanonicalPath: "/loc/kg/na/na"},

	"republic of tajikistan": {ISO2: "tj", EnglishName: "Tajikistan", CanonicalPath: "/loc/tj/na/na"},
	"tajikistan":             {ISO2: "tj", EnglishName: "Tajikistan", CanonicalPath: "/loc/tj/na/na"},

	"turkmenistan": {ISO2: "tm", EnglishName: "Turkmenistan", CanonicalPath: "/loc/tm/na/na"},

	"mongolia": {ISO2: "mn", EnglishName: "Mongolia", CanonicalPath: "/loc/mn/na/na"},

	// Gulf region + countries
	"gulf cooperation council": {ISO2: "gcc", EnglishName: "Gulf Cooperation Council", CanonicalPath: "/loc/gcc/na/na"},
	"gcc":                      {ISO2: "gcc", EnglishName: "Gulf Cooperation Council", CanonicalPath: "/loc/gcc/na/na"},
	"bahrain":                  {ISO2: "bh", EnglishName: "Bahrain", CanonicalPath: "/loc/bh/na/na"},
	"kuwait":                   {ISO2: "kw", EnglishName: "Kuwait", CanonicalPath: "/loc/kw/na/na"},
	"oman":                     {ISO2: "om", EnglishName: "Oman", CanonicalPath: "/loc/om/na/na"},
	"qatar":                    {ISO2: "qa", EnglishName: "Qatar", CanonicalPath: "/loc/qa/na/na"},
	"yemen":                    {ISO2: "ye", EnglishName: "Yemen", CanonicalPath: "/loc/ye/na/na"},

	// Southeast Asia (ASEAN + members)
	"association of southeast asian nations": {ISO2: "asean", EnglishName: "ASEAN", CanonicalPath: "/loc/asean/na/na"},
	"asean":                                  {ISO2: "asean", EnglishName: "ASEAN", CanonicalPath: "/loc/asean/na/na"},
	"brunei":                                 {ISO2: "bn", EnglishName: "Brunei", CanonicalPath: "/loc/bn/na/na"},
	"brunei darussalam":                      {ISO2: "bn", EnglishName: "Brunei", CanonicalPath: "/loc/bn/na/na"},
	"cambodia":                               {ISO2: "kh", EnglishName: "Cambodia", CanonicalPath: "/loc/kh/na/na"},
	"laos":                                   {ISO2: "la", EnglishName: "Laos", CanonicalPath: "/loc/la/na/na"},
	"lao people's democratic republic":       {ISO2: "la", EnglishName: "Laos", CanonicalPath: "/loc/la/na/na"},
	"myanmar":                                {ISO2: "mm", EnglishName: "Myanmar", CanonicalPath: "/loc/mm/na/na"},
	"burma":                                  {ISO2: "mm", EnglishName: "Myanmar", CanonicalPath: "/loc/mm/na/na"},
	"timor-leste":                            {ISO2: "tl", EnglishName: "Timor-Leste", CanonicalPath: "/loc/tl/na/na"},
	"east timor":                             {ISO2: "tl", EnglishName: "Timor-Leste", CanonicalPath: "/loc/tl/na/na"},

	// Oceania
	"papua new guinea": {ISO2: "pg", EnglishName: "Papua New Guinea", CanonicalPath: "/loc/pg/na/na"},
	"fiji":             {ISO2: "fj", EnglishName: "Fiji", CanonicalPath: "/loc/fj/na/na"},
	"solomon islands":  {ISO2: "sb", EnglishName: "Solomon Islands", CanonicalPath: "/loc/sb/na/na"},
	"vanuatu":          {ISO2: "vu", EnglishName: "Vanuatu", CanonicalPath: "/loc/vu/na/na"},
	"samoa":            {ISO2: "ws", EnglishName: "Samoa", CanonicalPath: "/loc/ws/na/na"},
	"tonga":            {ISO2: "to", EnglishName: "Tonga", CanonicalPath: "/loc/to/na/na"},
	"micronesia":       {ISO2: "fm", EnglishName: "Micronesia", CanonicalPath: "/loc/fm/na/na"},
	"palau":            {ISO2: "pw", EnglishName: "Palau", CanonicalPath: "/loc/pw/na/na"},
	"marshall islands": {ISO2: "mh", EnglishName: "Marshall Islands", CanonicalPath: "/loc/mh/na/na"},
	"nauru":            {ISO2: "nr", EnglishName: "Nauru", CanonicalPath: "/loc/nr/na/na"},
	"kiribati":         {ISO2: "ki", EnglishName: "Kiribati", CanonicalPath: "/loc/ki/na/na"},
	"tuvalu":           {ISO2: "tv", EnglishName: "Tuvalu", CanonicalPath: "/loc/tv/na/na"},

	// Africa (all countries)
	"algeria":                             {ISO2: "dz", EnglishName: "Algeria", CanonicalPath: "/loc/dz/na/na"},
	"angola":                              {ISO2: "ao", EnglishName: "Angola", CanonicalPath: "/loc/ao/na/na"},
	"benin":                               {ISO2: "bj", EnglishName: "Benin", CanonicalPath: "/loc/bj/na/na"},
	"botswana":                            {ISO2: "bw", EnglishName: "Botswana", CanonicalPath: "/loc/bw/na/na"},
	"burkina faso":                        {ISO2: "bf", EnglishName: "Burkina Faso", CanonicalPath: "/loc/bf/na/na"},
	"burundi":                             {ISO2: "bi", EnglishName: "Burundi", CanonicalPath: "/loc/bi/na/na"},
	"cabo verde":                          {ISO2: "cv", EnglishName: "Cabo Verde", CanonicalPath: "/loc/cv/na/na"},
	"cape verde":                          {ISO2: "cv", EnglishName: "Cabo Verde", CanonicalPath: "/loc/cv/na/na"},
	"cameroon":                            {ISO2: "cm", EnglishName: "Cameroon", CanonicalPath: "/loc/cm/na/na"},
	"central african republic":            {ISO2: "cf", EnglishName: "Central African Republic", CanonicalPath: "/loc/cf/na/na"},
	"chad":                                {ISO2: "td", EnglishName: "Chad", CanonicalPath: "/loc/td/na/na"},
	"comoros":                             {ISO2: "km", EnglishName: "Comoros", CanonicalPath: "/loc/km/na/na"},
	"republic of the congo":               {ISO2: "cg", EnglishName: "Republic of the Congo", CanonicalPath: "/loc/cg/na/na"},
	"congo":                               {ISO2: "cg", EnglishName: "Republic of the Congo", CanonicalPath: "/loc/cg/na/na"},
	"democratic republic of the congo":    {ISO2: "cd", EnglishName: "Democratic Republic of the Congo", CanonicalPath: "/loc/cd/na/na"},
	"congo-kinshasa":                      {ISO2: "cd", EnglishName: "Democratic Republic of the Congo", CanonicalPath: "/loc/cd/na/na"},
	"cote d'ivoire":                       {ISO2: "ci", EnglishName: "Cote d'Ivoire", CanonicalPath: "/loc/ci/na/na"},
	"ivory coast":                         {ISO2: "ci", EnglishName: "Cote d'Ivoire", CanonicalPath: "/loc/ci/na/na"},
	"djibouti":                            {ISO2: "dj", EnglishName: "Djibouti", CanonicalPath: "/loc/dj/na/na"},
	"equatorial guinea":                   {ISO2: "gq", EnglishName: "Equatorial Guinea", CanonicalPath: "/loc/gq/na/na"},
	"eritrea":                             {ISO2: "er", EnglishName: "Eritrea", CanonicalPath: "/loc/er/na/na"},
	"eswatini":                            {ISO2: "sz", EnglishName: "Eswatini", CanonicalPath: "/loc/sz/na/na"},
	"swaziland":                           {ISO2: "sz", EnglishName: "Eswatini", CanonicalPath: "/loc/sz/na/na"},
	"ethiopia":                            {ISO2: "et", EnglishName: "Ethiopia", CanonicalPath: "/loc/et/na/na"},
	"gabon":                               {ISO2: "ga", EnglishName: "Gabon", CanonicalPath: "/loc/ga/na/na"},
	"gambia":                              {ISO2: "gm", EnglishName: "Gambia", CanonicalPath: "/loc/gm/na/na"},
	"ghana":                               {ISO2: "gh", EnglishName: "Ghana", CanonicalPath: "/loc/gh/na/na"},
	"guinea":                              {ISO2: "gn", EnglishName: "Guinea", CanonicalPath: "/loc/gn/na/na"},
	"guinea-bissau":                       {ISO2: "gw", EnglishName: "Guinea-Bissau", CanonicalPath: "/loc/gw/na/na"},
	"kenya":                               {ISO2: "ke", EnglishName: "Kenya", CanonicalPath: "/loc/ke/na/na"},
	"lesotho":                             {ISO2: "ls", EnglishName: "Lesotho", CanonicalPath: "/loc/ls/na/na"},
	"liberia":                             {ISO2: "lr", EnglishName: "Liberia", CanonicalPath: "/loc/lr/na/na"},
	"libya":                               {ISO2: "ly", EnglishName: "Libya", CanonicalPath: "/loc/ly/na/na"},
	"madagascar":                          {ISO2: "mg", EnglishName: "Madagascar", CanonicalPath: "/loc/mg/na/na"},
	"malawi":                              {ISO2: "mw", EnglishName: "Malawi", CanonicalPath: "/loc/mw/na/na"},
	"mali":                                {ISO2: "ml", EnglishName: "Mali", CanonicalPath: "/loc/ml/na/na"},
	"mauritania":                          {ISO2: "mr", EnglishName: "Mauritania", CanonicalPath: "/loc/mr/na/na"},
	"mauritius":                           {ISO2: "mu", EnglishName: "Mauritius", CanonicalPath: "/loc/mu/na/na"},
	"morocco":                             {ISO2: "ma", EnglishName: "Morocco", CanonicalPath: "/loc/ma/na/na"},
	"mozambique":                          {ISO2: "mz", EnglishName: "Mozambique", CanonicalPath: "/loc/mz/na/na"},
	"niger":                               {ISO2: "ne", EnglishName: "Niger", CanonicalPath: "/loc/ne/na/na"},
	"rwanda":                              {ISO2: "rw", EnglishName: "Rwanda", CanonicalPath: "/loc/rw/na/na"},
	"sao tome and principe":               {ISO2: "st", EnglishName: "Sao Tome and Principe", CanonicalPath: "/loc/st/na/na"},
	"senegal":                             {ISO2: "sn", EnglishName: "Senegal", CanonicalPath: "/loc/sn/na/na"},
	"seychelles":                          {ISO2: "sc", EnglishName: "Seychelles", CanonicalPath: "/loc/sc/na/na"},
	"sierra leone":                        {ISO2: "sl", EnglishName: "Sierra Leone", CanonicalPath: "/loc/sl/na/na"},
	"somalia":                             {ISO2: "so", EnglishName: "Somalia", CanonicalPath: "/loc/so/na/na"},
	"south sudan":                         {ISO2: "ss", EnglishName: "South Sudan", CanonicalPath: "/loc/ss/na/na"},
	"sudan":                               {ISO2: "sd", EnglishName: "Sudan", CanonicalPath: "/loc/sd/na/na"},
	"tanzania":                            {ISO2: "tz", EnglishName: "Tanzania", CanonicalPath: "/loc/tz/na/na"},
	"togo":                                {ISO2: "tg", EnglishName: "Togo", CanonicalPath: "/loc/tg/na/na"},
	"tunisia":                             {ISO2: "tn", EnglishName: "Tunisia", CanonicalPath: "/loc/tn/na/na"},
	"uganda":                              {ISO2: "ug", EnglishName: "Uganda", CanonicalPath: "/loc/ug/na/na"},
	"zambia":                              {ISO2: "zm", EnglishName: "Zambia", CanonicalPath: "/loc/zm/na/na"},
	"zimbabwe":                            {ISO2: "zw", EnglishName: "Zimbabwe", CanonicalPath: "/loc/zw/na/na"},
	"afghanistan":                         {ISO2: "af", EnglishName: "Afghanistan", CanonicalPath: "/loc/af/na/na"},
	"bhutan":                              {ISO2: "bt", EnglishName: "Bhutan", CanonicalPath: "/loc/bt/na/na"},
	"sri lanka":                           {ISO2: "lk", EnglishName: "Sri Lanka", CanonicalPath: "/loc/lk/na/na"},
	"nepal":                               {ISO2: "np", EnglishName: "Nepal", CanonicalPath: "/loc/np/na/na"},
	"maldives":                            {ISO2: "mv", EnglishName: "Maldives", CanonicalPath: "/loc/mv/na/na"},

	// European Union (special case - not a country but often typed as ORG)
	"european union": {ISO2: "eu", EnglishName: "European Union", CanonicalPath: "/loc/eu/na/na"},
	"eu":             {ISO2: "eu", EnglishName: "European Union", CanonicalPath: "/loc/eu/na/na"},
}

var knownCountryByISO2 map[string]string

func countryNameByISO2(iso2 string) string {
	iso2 = strings.TrimSpace(strings.ToLower(iso2))
	if iso2 == "" {
		return ""
	}
	if knownCountryByISO2 == nil {
		m := map[string]string{}
		for _, info := range KnownCountries {
			if info.ISO2 == "" || info.EnglishName == "" {
				continue
			}
			if _, ok := m[info.ISO2]; ok {
				continue
			}
			m[info.ISO2] = info.EnglishName
		}
		knownCountryByISO2 = m
	}
	return knownCountryByISO2[iso2]
}

// CountryAliases maps additional aliases to canonical paths.
var CountryAliases = map[string]string{
	"u.s.a.":             "/loc/us/na/na",
	"u.s.":               "/loc/us/na/na",
	"u.k.":               "/loc/gb/na/na",
	"p.r.c.":             "/loc/cn/na/na",
	"mainland china":     "/loc/cn/na/na",
	"west germany":       "/loc/de/na/na",
	"east germany":       "/loc/de/na/na",
	"soviet union":       "/loc/ru/na/na",
	"ussr":               "/loc/ru/na/na",
	"u.s.s.r.":           "/loc/ru/na/na",
	"roc":                "/loc/tw/na/na",
	"republic of taiwan": "/loc/tw/na/na",
}

// KnownCountriesByPath maps canonical paths to country info (reverse lookup).
var KnownCountriesByPath = make(map[string]CountryInfo)

func init() {
	// Build reverse lookup map
	for _, country := range KnownCountries {
		KnownCountriesByPath[country.CanonicalPath] = country
	}
}

// IsKnownCountryName checks if a name matches a known country.
func IsKnownCountryName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if _, ok := KnownCountries[normalized]; ok {
		return true
	}
	if _, ok := CountryAliases[normalized]; ok {
		return true
	}
	return false
}

// GetCountryCanonicalPath returns the canonical path for a country name.
// Returns empty string if not found.
func GetCountryCanonicalPath(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if country, ok := KnownCountries[normalized]; ok {
		return country.CanonicalPath
	}
	if cp, ok := CountryAliases[normalized]; ok {
		return cp
	}
	return ""
}

