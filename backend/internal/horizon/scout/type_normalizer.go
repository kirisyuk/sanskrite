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
	CanonicalPath   string // Suggested canonical path (e.g., /loc/us for USA)
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
	CanonicalPath string // Canonical path: /loc/{iso2}
}

// KnownCountries maps lowercase country names to their canonical info.
// Key: lowercase full name or common name
var KnownCountries = map[string]CountryInfo{
	// Major countries (most likely to be mistyped as ORG)
	"united states of america": {ISO2: "us", EnglishName: "United States of America", CanonicalPath: "/loc/us"},
	"united states":            {ISO2: "us", EnglishName: "United States of America", CanonicalPath: "/loc/us"},
	"usa":                      {ISO2: "us", EnglishName: "United States of America", CanonicalPath: "/loc/us"},
	"america":                  {ISO2: "us", EnglishName: "United States of America", CanonicalPath: "/loc/us"},

	"people's republic of china": {ISO2: "cn", EnglishName: "People's Republic of China", CanonicalPath: "/loc/cn"},
	"china":                      {ISO2: "cn", EnglishName: "People's Republic of China", CanonicalPath: "/loc/cn"},
	"prc":                        {ISO2: "cn", EnglishName: "People's Republic of China", CanonicalPath: "/loc/cn"},

	"russian federation": {ISO2: "ru", EnglishName: "Russian Federation", CanonicalPath: "/loc/ru"},
	"russia":             {ISO2: "ru", EnglishName: "Russian Federation", CanonicalPath: "/loc/ru"},

	"united kingdom of great britain and northern ireland": {ISO2: "gb", EnglishName: "United Kingdom", CanonicalPath: "/loc/gb"},
	"united kingdom": {ISO2: "gb", EnglishName: "United Kingdom", CanonicalPath: "/loc/gb"},
	"great britain":  {ISO2: "gb", EnglishName: "United Kingdom", CanonicalPath: "/loc/gb"},
	"uk":             {ISO2: "gb", EnglishName: "United Kingdom", CanonicalPath: "/loc/gb"},
	"britain":        {ISO2: "gb", EnglishName: "United Kingdom", CanonicalPath: "/loc/gb"},

	"federal republic of germany": {ISO2: "de", EnglishName: "Germany", CanonicalPath: "/loc/de"},
	"germany":                     {ISO2: "de", EnglishName: "Germany", CanonicalPath: "/loc/de"},
	"deutschland":                 {ISO2: "de", EnglishName: "Germany", CanonicalPath: "/loc/de"},

	"french republic": {ISO2: "fr", EnglishName: "France", CanonicalPath: "/loc/fr"},
	"france":          {ISO2: "fr", EnglishName: "France", CanonicalPath: "/loc/fr"},

	"japan": {ISO2: "jp", EnglishName: "Japan", CanonicalPath: "/loc/jp"},

	"republic of korea":                     {ISO2: "kr", EnglishName: "South Korea", CanonicalPath: "/loc/kr"},
	"south korea":                           {ISO2: "kr", EnglishName: "South Korea", CanonicalPath: "/loc/kr"},
	"korea":                                 {ISO2: "kr", EnglishName: "South Korea", CanonicalPath: "/loc/kr"},
	"democratic people's republic of korea": {ISO2: "kp", EnglishName: "North Korea", CanonicalPath: "/loc/kp"},
	"north korea":                           {ISO2: "kp", EnglishName: "North Korea", CanonicalPath: "/loc/kp"},
	"dprk":                                  {ISO2: "kp", EnglishName: "North Korea", CanonicalPath: "/loc/kp"},

	"republic of india": {ISO2: "in", EnglishName: "India", CanonicalPath: "/loc/in"},
	"india":             {ISO2: "in", EnglishName: "India", CanonicalPath: "/loc/in"},

	"federative republic of brazil": {ISO2: "br", EnglishName: "Brazil", CanonicalPath: "/loc/br"},
	"brazil":                        {ISO2: "br", EnglishName: "Brazil", CanonicalPath: "/loc/br"},

	"canada": {ISO2: "ca", EnglishName: "Canada", CanonicalPath: "/loc/ca"},

	"commonwealth of australia": {ISO2: "au", EnglishName: "Australia", CanonicalPath: "/loc/au"},
	"australia":                 {ISO2: "au", EnglishName: "Australia", CanonicalPath: "/loc/au"},

	"italian republic": {ISO2: "it", EnglishName: "Italy", CanonicalPath: "/loc/it"},
	"italy":            {ISO2: "it", EnglishName: "Italy", CanonicalPath: "/loc/it"},

	"kingdom of spain": {ISO2: "es", EnglishName: "Spain", CanonicalPath: "/loc/es"},
	"spain":            {ISO2: "es", EnglishName: "Spain", CanonicalPath: "/loc/es"},

	"united mexican states": {ISO2: "mx", EnglishName: "Mexico", CanonicalPath: "/loc/mx"},
	"mexico":                {ISO2: "mx", EnglishName: "Mexico", CanonicalPath: "/loc/mx"},

	"republic of indonesia": {ISO2: "id", EnglishName: "Indonesia", CanonicalPath: "/loc/id"},
	"indonesia":             {ISO2: "id", EnglishName: "Indonesia", CanonicalPath: "/loc/id"},

	"kingdom of the netherlands": {ISO2: "nl", EnglishName: "Netherlands", CanonicalPath: "/loc/nl"},
	"netherlands":                {ISO2: "nl", EnglishName: "Netherlands", CanonicalPath: "/loc/nl"},
	"holland":                    {ISO2: "nl", EnglishName: "Netherlands", CanonicalPath: "/loc/nl"},

	"kingdom of saudi arabia": {ISO2: "sa", EnglishName: "Saudi Arabia", CanonicalPath: "/loc/sa"},
	"saudi arabia":            {ISO2: "sa", EnglishName: "Saudi Arabia", CanonicalPath: "/loc/sa"},

	"swiss confederation": {ISO2: "ch", EnglishName: "Switzerland", CanonicalPath: "/loc/ch"},
	"switzerland":         {ISO2: "ch", EnglishName: "Switzerland", CanonicalPath: "/loc/ch"},

	"republic of turkey": {ISO2: "tr", EnglishName: "Turkey", CanonicalPath: "/loc/tr"},
	"turkey":             {ISO2: "tr", EnglishName: "Turkey", CanonicalPath: "/loc/tr"},
	"türkiye":            {ISO2: "tr", EnglishName: "Turkey", CanonicalPath: "/loc/tr"},

	"kingdom of sweden": {ISO2: "se", EnglishName: "Sweden", CanonicalPath: "/loc/se"},
	"sweden":            {ISO2: "se", EnglishName: "Sweden", CanonicalPath: "/loc/se"},

	"republic of poland": {ISO2: "pl", EnglishName: "Poland", CanonicalPath: "/loc/pl"},
	"poland":             {ISO2: "pl", EnglishName: "Poland", CanonicalPath: "/loc/pl"},

	"kingdom of belgium": {ISO2: "be", EnglishName: "Belgium", CanonicalPath: "/loc/be"},
	"belgium":            {ISO2: "be", EnglishName: "Belgium", CanonicalPath: "/loc/be"},

	"republic of austria": {ISO2: "at", EnglishName: "Austria", CanonicalPath: "/loc/at"},
	"austria":             {ISO2: "at", EnglishName: "Austria", CanonicalPath: "/loc/at"},

	"kingdom of norway": {ISO2: "no", EnglishName: "Norway", CanonicalPath: "/loc/no"},
	"norway":            {ISO2: "no", EnglishName: "Norway", CanonicalPath: "/loc/no"},

	"kingdom of denmark": {ISO2: "dk", EnglishName: "Denmark", CanonicalPath: "/loc/dk"},
	"denmark":            {ISO2: "dk", EnglishName: "Denmark", CanonicalPath: "/loc/dk"},

	"republic of finland": {ISO2: "fi", EnglishName: "Finland", CanonicalPath: "/loc/fi"},
	"finland":             {ISO2: "fi", EnglishName: "Finland", CanonicalPath: "/loc/fi"},

	"hellenic republic": {ISO2: "gr", EnglishName: "Greece", CanonicalPath: "/loc/gr"},
	"greece":            {ISO2: "gr", EnglishName: "Greece", CanonicalPath: "/loc/gr"},

	"portuguese republic": {ISO2: "pt", EnglishName: "Portugal", CanonicalPath: "/loc/pt"},
	"portugal":            {ISO2: "pt", EnglishName: "Portugal", CanonicalPath: "/loc/pt"},

	"ireland": {ISO2: "ie", EnglishName: "Ireland", CanonicalPath: "/loc/ie"},

	"new zealand": {ISO2: "nz", EnglishName: "New Zealand", CanonicalPath: "/loc/nz"},

	"republic of singapore": {ISO2: "sg", EnglishName: "Singapore", CanonicalPath: "/loc/sg"},
	"singapore":             {ISO2: "sg", EnglishName: "Singapore", CanonicalPath: "/loc/sg"},

	"hong kong special administrative region": {ISO2: "hk", EnglishName: "Hong Kong", CanonicalPath: "/loc/hk"},
	"hong kong": {ISO2: "hk", EnglishName: "Hong Kong", CanonicalPath: "/loc/hk"},

	"republic of china": {ISO2: "tw", EnglishName: "Taiwan", CanonicalPath: "/loc/tw"},
	"taiwan":            {ISO2: "tw", EnglishName: "Taiwan", CanonicalPath: "/loc/tw"},

	"state of israel": {ISO2: "il", EnglishName: "Israel", CanonicalPath: "/loc/il"},
	"israel":          {ISO2: "il", EnglishName: "Israel", CanonicalPath: "/loc/il"},

	"united arab emirates": {ISO2: "ae", EnglishName: "United Arab Emirates", CanonicalPath: "/loc/ae"},
	"uae":                  {ISO2: "ae", EnglishName: "United Arab Emirates", CanonicalPath: "/loc/ae"},

	"islamic republic of iran": {ISO2: "ir", EnglishName: "Iran", CanonicalPath: "/loc/ir"},
	"iran":                     {ISO2: "ir", EnglishName: "Iran", CanonicalPath: "/loc/ir"},

	"republic of iraq": {ISO2: "iq", EnglishName: "Iraq", CanonicalPath: "/loc/iq"},
	"iraq":             {ISO2: "iq", EnglishName: "Iraq", CanonicalPath: "/loc/iq"},

	"arab republic of egypt": {ISO2: "eg", EnglishName: "Egypt", CanonicalPath: "/loc/eg"},
	"egypt":                  {ISO2: "eg", EnglishName: "Egypt", CanonicalPath: "/loc/eg"},

	"republic of south africa": {ISO2: "za", EnglishName: "South Africa", CanonicalPath: "/loc/za"},
	"south africa":             {ISO2: "za", EnglishName: "South Africa", CanonicalPath: "/loc/za"},

	"federal republic of nigeria": {ISO2: "ng", EnglishName: "Nigeria", CanonicalPath: "/loc/ng"},
	"nigeria":                     {ISO2: "ng", EnglishName: "Nigeria", CanonicalPath: "/loc/ng"},

	"argentine republic": {ISO2: "ar", EnglishName: "Argentina", CanonicalPath: "/loc/ar"},
	"argentina":          {ISO2: "ar", EnglishName: "Argentina", CanonicalPath: "/loc/ar"},

	"republic of chile": {ISO2: "cl", EnglishName: "Chile", CanonicalPath: "/loc/cl"},
	"chile":             {ISO2: "cl", EnglishName: "Chile", CanonicalPath: "/loc/cl"},

	"republic of colombia": {ISO2: "co", EnglishName: "Colombia", CanonicalPath: "/loc/co"},
	"colombia":             {ISO2: "co", EnglishName: "Colombia", CanonicalPath: "/loc/co"},

	"bolivarian republic of venezuela": {ISO2: "ve", EnglishName: "Venezuela", CanonicalPath: "/loc/ve"},
	"venezuela":                        {ISO2: "ve", EnglishName: "Venezuela", CanonicalPath: "/loc/ve"},

	"republic of peru": {ISO2: "pe", EnglishName: "Peru", CanonicalPath: "/loc/pe"},
	"peru":             {ISO2: "pe", EnglishName: "Peru", CanonicalPath: "/loc/pe"},

	"republic of the philippines": {ISO2: "ph", EnglishName: "Philippines", CanonicalPath: "/loc/ph"},
	"philippines":                 {ISO2: "ph", EnglishName: "Philippines", CanonicalPath: "/loc/ph"},

	"socialist republic of vietnam": {ISO2: "vn", EnglishName: "Vietnam", CanonicalPath: "/loc/vn"},
	"vietnam":                       {ISO2: "vn", EnglishName: "Vietnam", CanonicalPath: "/loc/vn"},

	"kingdom of thailand": {ISO2: "th", EnglishName: "Thailand", CanonicalPath: "/loc/th"},
	"thailand":            {ISO2: "th", EnglishName: "Thailand", CanonicalPath: "/loc/th"},

	"malaysia": {ISO2: "my", EnglishName: "Malaysia", CanonicalPath: "/loc/my"},

	"islamic republic of pakistan": {ISO2: "pk", EnglishName: "Pakistan", CanonicalPath: "/loc/pk"},
	"pakistan":                     {ISO2: "pk", EnglishName: "Pakistan", CanonicalPath: "/loc/pk"},

	"people's republic of bangladesh": {ISO2: "bd", EnglishName: "Bangladesh", CanonicalPath: "/loc/bd"},
	"bangladesh":                      {ISO2: "bd", EnglishName: "Bangladesh", CanonicalPath: "/loc/bd"},

	"czech republic": {ISO2: "cz", EnglishName: "Czech Republic", CanonicalPath: "/loc/cz"},
	"czechia":        {ISO2: "cz", EnglishName: "Czech Republic", CanonicalPath: "/loc/cz"},

	"romania": {ISO2: "ro", EnglishName: "Romania", CanonicalPath: "/loc/ro"},

	"hungary": {ISO2: "hu", EnglishName: "Hungary", CanonicalPath: "/loc/hu"},

	"ukraine": {ISO2: "ua", EnglishName: "Ukraine", CanonicalPath: "/loc/ua"},

	"republic of belarus": {ISO2: "by", EnglishName: "Belarus", CanonicalPath: "/loc/by"},
	"belarus":             {ISO2: "by", EnglishName: "Belarus", CanonicalPath: "/loc/by"},

	"republic of kazakhstan": {ISO2: "kz", EnglishName: "Kazakhstan", CanonicalPath: "/loc/kz"},
	"kazakhstan":             {ISO2: "kz", EnglishName: "Kazakhstan", CanonicalPath: "/loc/kz"},

	"republic of uzbekistan": {ISO2: "uz", EnglishName: "Uzbekistan", CanonicalPath: "/loc/uz"},
	"uzbekistan":             {ISO2: "uz", EnglishName: "Uzbekistan", CanonicalPath: "/loc/uz"},

	"republic of azerbaijan": {ISO2: "az", EnglishName: "Azerbaijan", CanonicalPath: "/loc/az"},
	"azerbaijan":             {ISO2: "az", EnglishName: "Azerbaijan", CanonicalPath: "/loc/az"},

	"georgia": {ISO2: "ge", EnglishName: "Georgia", CanonicalPath: "/loc/ge"},

	"republic of armenia": {ISO2: "am", EnglishName: "Armenia", CanonicalPath: "/loc/am"},
	"armenia":             {ISO2: "am", EnglishName: "Armenia", CanonicalPath: "/loc/am"},

	"kyrgyz republic": {ISO2: "kg", EnglishName: "Kyrgyzstan", CanonicalPath: "/loc/kg"},
	"kyrgyzstan":      {ISO2: "kg", EnglishName: "Kyrgyzstan", CanonicalPath: "/loc/kg"},

	"republic of tajikistan": {ISO2: "tj", EnglishName: "Tajikistan", CanonicalPath: "/loc/tj"},
	"tajikistan":             {ISO2: "tj", EnglishName: "Tajikistan", CanonicalPath: "/loc/tj"},

	"turkmenistan": {ISO2: "tm", EnglishName: "Turkmenistan", CanonicalPath: "/loc/tm"},

	"mongolia": {ISO2: "mn", EnglishName: "Mongolia", CanonicalPath: "/loc/mn"},

	// Gulf region + countries
	"gulf cooperation council": {ISO2: "gcc", EnglishName: "Gulf Cooperation Council", CanonicalPath: "/loc/gcc"},
	"gcc":                      {ISO2: "gcc", EnglishName: "Gulf Cooperation Council", CanonicalPath: "/loc/gcc"},
	"bahrain":                  {ISO2: "bh", EnglishName: "Bahrain", CanonicalPath: "/loc/bh"},
	"kuwait":                   {ISO2: "kw", EnglishName: "Kuwait", CanonicalPath: "/loc/kw"},
	"oman":                     {ISO2: "om", EnglishName: "Oman", CanonicalPath: "/loc/om"},
	"qatar":                    {ISO2: "qa", EnglishName: "Qatar", CanonicalPath: "/loc/qa"},
	"yemen":                    {ISO2: "ye", EnglishName: "Yemen", CanonicalPath: "/loc/ye"},

	// Southeast Asia (ASEAN + members)
	"association of southeast asian nations": {ISO2: "asean", EnglishName: "ASEAN", CanonicalPath: "/loc/asean"},
	"asean":                                  {ISO2: "asean", EnglishName: "ASEAN", CanonicalPath: "/loc/asean"},
	"brunei":                                 {ISO2: "bn", EnglishName: "Brunei", CanonicalPath: "/loc/bn"},
	"brunei darussalam":                      {ISO2: "bn", EnglishName: "Brunei", CanonicalPath: "/loc/bn"},
	"cambodia":                               {ISO2: "kh", EnglishName: "Cambodia", CanonicalPath: "/loc/kh"},
	"laos":                                   {ISO2: "la", EnglishName: "Laos", CanonicalPath: "/loc/la"},
	"lao people's democratic republic":       {ISO2: "la", EnglishName: "Laos", CanonicalPath: "/loc/la"},
	"myanmar":                                {ISO2: "mm", EnglishName: "Myanmar", CanonicalPath: "/loc/mm"},
	"burma":                                  {ISO2: "mm", EnglishName: "Myanmar", CanonicalPath: "/loc/mm"},
	"timor-leste":                            {ISO2: "tl", EnglishName: "Timor-Leste", CanonicalPath: "/loc/tl"},
	"east timor":                             {ISO2: "tl", EnglishName: "Timor-Leste", CanonicalPath: "/loc/tl"},

	// Oceania
	"papua new guinea": {ISO2: "pg", EnglishName: "Papua New Guinea", CanonicalPath: "/loc/pg"},
	"fiji":             {ISO2: "fj", EnglishName: "Fiji", CanonicalPath: "/loc/fj"},
	"solomon islands":  {ISO2: "sb", EnglishName: "Solomon Islands", CanonicalPath: "/loc/sb"},
	"vanuatu":          {ISO2: "vu", EnglishName: "Vanuatu", CanonicalPath: "/loc/vu"},
	"samoa":            {ISO2: "ws", EnglishName: "Samoa", CanonicalPath: "/loc/ws"},
	"tonga":            {ISO2: "to", EnglishName: "Tonga", CanonicalPath: "/loc/to"},
	"micronesia":       {ISO2: "fm", EnglishName: "Micronesia", CanonicalPath: "/loc/fm"},
	"palau":            {ISO2: "pw", EnglishName: "Palau", CanonicalPath: "/loc/pw"},
	"marshall islands": {ISO2: "mh", EnglishName: "Marshall Islands", CanonicalPath: "/loc/mh"},
	"nauru":            {ISO2: "nr", EnglishName: "Nauru", CanonicalPath: "/loc/nr"},
	"kiribati":         {ISO2: "ki", EnglishName: "Kiribati", CanonicalPath: "/loc/ki"},
	"tuvalu":           {ISO2: "tv", EnglishName: "Tuvalu", CanonicalPath: "/loc/tv"},

	// Africa (all countries)
	"algeria":                             {ISO2: "dz", EnglishName: "Algeria", CanonicalPath: "/loc/dz"},
	"angola":                              {ISO2: "ao", EnglishName: "Angola", CanonicalPath: "/loc/ao"},
	"benin":                               {ISO2: "bj", EnglishName: "Benin", CanonicalPath: "/loc/bj"},
	"botswana":                            {ISO2: "bw", EnglishName: "Botswana", CanonicalPath: "/loc/bw"},
	"burkina faso":                        {ISO2: "bf", EnglishName: "Burkina Faso", CanonicalPath: "/loc/bf"},
	"burundi":                             {ISO2: "bi", EnglishName: "Burundi", CanonicalPath: "/loc/bi"},
	"cabo verde":                          {ISO2: "cv", EnglishName: "Cabo Verde", CanonicalPath: "/loc/cv"},
	"cape verde":                          {ISO2: "cv", EnglishName: "Cabo Verde", CanonicalPath: "/loc/cv"},
	"cameroon":                            {ISO2: "cm", EnglishName: "Cameroon", CanonicalPath: "/loc/cm"},
	"central african republic":            {ISO2: "cf", EnglishName: "Central African Republic", CanonicalPath: "/loc/cf"},
	"chad":                                {ISO2: "td", EnglishName: "Chad", CanonicalPath: "/loc/td"},
	"comoros":                             {ISO2: "km", EnglishName: "Comoros", CanonicalPath: "/loc/km"},
	"republic of the congo":               {ISO2: "cg", EnglishName: "Republic of the Congo", CanonicalPath: "/loc/cg"},
	"congo":                               {ISO2: "cg", EnglishName: "Republic of the Congo", CanonicalPath: "/loc/cg"},
	"democratic republic of the congo":    {ISO2: "cd", EnglishName: "Democratic Republic of the Congo", CanonicalPath: "/loc/cd"},
	"congo-kinshasa":                      {ISO2: "cd", EnglishName: "Democratic Republic of the Congo", CanonicalPath: "/loc/cd"},
	"cote d'ivoire":                       {ISO2: "ci", EnglishName: "Cote d'Ivoire", CanonicalPath: "/loc/ci"},
	"ivory coast":                         {ISO2: "ci", EnglishName: "Cote d'Ivoire", CanonicalPath: "/loc/ci"},
	"djibouti":                            {ISO2: "dj", EnglishName: "Djibouti", CanonicalPath: "/loc/dj"},
	"equatorial guinea":                   {ISO2: "gq", EnglishName: "Equatorial Guinea", CanonicalPath: "/loc/gq"},
	"eritrea":                             {ISO2: "er", EnglishName: "Eritrea", CanonicalPath: "/loc/er"},
	"eswatini":                            {ISO2: "sz", EnglishName: "Eswatini", CanonicalPath: "/loc/sz"},
	"swaziland":                           {ISO2: "sz", EnglishName: "Eswatini", CanonicalPath: "/loc/sz"},
	"ethiopia":                            {ISO2: "et", EnglishName: "Ethiopia", CanonicalPath: "/loc/et"},
	"gabon":                               {ISO2: "ga", EnglishName: "Gabon", CanonicalPath: "/loc/ga"},
	"gambia":                              {ISO2: "gm", EnglishName: "Gambia", CanonicalPath: "/loc/gm"},
	"ghana":                               {ISO2: "gh", EnglishName: "Ghana", CanonicalPath: "/loc/gh"},
	"guinea":                              {ISO2: "gn", EnglishName: "Guinea", CanonicalPath: "/loc/gn"},
	"guinea-bissau":                       {ISO2: "gw", EnglishName: "Guinea-Bissau", CanonicalPath: "/loc/gw"},
	"kenya":                               {ISO2: "ke", EnglishName: "Kenya", CanonicalPath: "/loc/ke"},
	"lesotho":                             {ISO2: "ls", EnglishName: "Lesotho", CanonicalPath: "/loc/ls"},
	"liberia":                             {ISO2: "lr", EnglishName: "Liberia", CanonicalPath: "/loc/lr"},
	"libya":                               {ISO2: "ly", EnglishName: "Libya", CanonicalPath: "/loc/ly"},
	"madagascar":                          {ISO2: "mg", EnglishName: "Madagascar", CanonicalPath: "/loc/mg"},
	"malawi":                              {ISO2: "mw", EnglishName: "Malawi", CanonicalPath: "/loc/mw"},
	"mali":                                {ISO2: "ml", EnglishName: "Mali", CanonicalPath: "/loc/ml"},
	"mauritania":                          {ISO2: "mr", EnglishName: "Mauritania", CanonicalPath: "/loc/mr"},
	"mauritius":                           {ISO2: "mu", EnglishName: "Mauritius", CanonicalPath: "/loc/mu"},
	"morocco":                             {ISO2: "ma", EnglishName: "Morocco", CanonicalPath: "/loc/ma"},
	"mozambique":                          {ISO2: "mz", EnglishName: "Mozambique", CanonicalPath: "/loc/mz"},
	"niger":                               {ISO2: "ne", EnglishName: "Niger", CanonicalPath: "/loc/ne"},
	"rwanda":                              {ISO2: "rw", EnglishName: "Rwanda", CanonicalPath: "/loc/rw"},
	"sao tome and principe":               {ISO2: "st", EnglishName: "Sao Tome and Principe", CanonicalPath: "/loc/st"},
	"senegal":                             {ISO2: "sn", EnglishName: "Senegal", CanonicalPath: "/loc/sn"},
	"seychelles":                          {ISO2: "sc", EnglishName: "Seychelles", CanonicalPath: "/loc/sc"},
	"sierra leone":                        {ISO2: "sl", EnglishName: "Sierra Leone", CanonicalPath: "/loc/sl"},
	"somalia":                             {ISO2: "so", EnglishName: "Somalia", CanonicalPath: "/loc/so"},
	"south sudan":                         {ISO2: "ss", EnglishName: "South Sudan", CanonicalPath: "/loc/ss"},
	"sudan":                               {ISO2: "sd", EnglishName: "Sudan", CanonicalPath: "/loc/sd"},
	"tanzania":                            {ISO2: "tz", EnglishName: "Tanzania", CanonicalPath: "/loc/tz"},
	"togo":                                {ISO2: "tg", EnglishName: "Togo", CanonicalPath: "/loc/tg"},
	"tunisia":                             {ISO2: "tn", EnglishName: "Tunisia", CanonicalPath: "/loc/tn"},
	"uganda":                              {ISO2: "ug", EnglishName: "Uganda", CanonicalPath: "/loc/ug"},
	"zambia":                              {ISO2: "zm", EnglishName: "Zambia", CanonicalPath: "/loc/zm"},
	"zimbabwe":                            {ISO2: "zw", EnglishName: "Zimbabwe", CanonicalPath: "/loc/zw"},
	"afghanistan":                         {ISO2: "af", EnglishName: "Afghanistan", CanonicalPath: "/loc/af"},
	"bhutan":                              {ISO2: "bt", EnglishName: "Bhutan", CanonicalPath: "/loc/bt"},
	"sri lanka":                           {ISO2: "lk", EnglishName: "Sri Lanka", CanonicalPath: "/loc/lk"},
	"nepal":                               {ISO2: "np", EnglishName: "Nepal", CanonicalPath: "/loc/np"},
	"maldives":                            {ISO2: "mv", EnglishName: "Maldives", CanonicalPath: "/loc/mv"},

	// European Union (special case - not a country but often typed as ORG)
	"european union": {ISO2: "eu", EnglishName: "European Union", CanonicalPath: "/loc/eu"},
	"eu":             {ISO2: "eu", EnglishName: "European Union", CanonicalPath: "/loc/eu"},
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
	"u.s.a.":             "/loc/us",
	"u.s.":               "/loc/us",
	"u.k.":               "/loc/gb",
	"p.r.c.":             "/loc/cn",
	"mainland china":     "/loc/cn",
	"west germany":       "/loc/de",
	"east germany":       "/loc/de",
	"soviet union":       "/loc/ru",
	"ussr":               "/loc/ru",
	"u.s.s.r.":           "/loc/ru",
	"roc":                "/loc/tw",
	"republic of taiwan": "/loc/tw",
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
