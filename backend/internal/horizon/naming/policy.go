package naming

import (
	"regexp"
	"strings"
	"unicode"
)

// LegalSuffixesToRemove defines common legal suffixes to strip during identity normalization.
var LegalSuffixesToRemove = []string{
	" jsc", " pjsc", " cjsc", " ojsc",
	" llc", " pllc", " inc", " ltd", " co", " corp", " corporation",
	" group", " holdings", " limited", " company", " pty ltd",
	" sa", " ag", " se", " gmbh", " ab", " oy", " as", " spa",
	" nv", " bv", " sarl", " srl", " plc", " lp", " llp",
	" s.a.", " a.g.", " s.p.a.", " n.v.", " b.v.", " s.à r.l.", " p.l.c.",
	" p.j.s.c.", " c.j.s.c.",
}

// LegalPrefixesToRemove defines legal form prefixes (e.g. "JSC Company Name").
// Same geo + base name + legal form (prefix or suffix) = same entity with very high probability.
// Exclude short prefixes (sa, ag, co, nv, bv) that could match common words (Saudi, agency, etc.).
// Longer phrases first so "joint stock company" strips before "jsc" is checked.
var LegalPrefixesToRemove = []string{
	"joint stock company ", "public joint stock company ", "closed joint stock company ",
	"jsc ", "pjsc ", "cjsc ", "ojsc ",
	"ao ", "oao ", "zao ", // Russian АО, ОАО, ЗАО
	"llc ", "inc ", "ltd ", "corp ", "plc ",
	"gmbh ", "sarl ", "srl ",
}

// parentheticalAliasRE matches parenthetical alias/rebranding annotations that LLMs often inject.
var parentheticalAliasRE = regexp.MustCompile(`\s*\((?:formerly|previously|ex-?|now|fka|dba|aka|trading as|prev\.?|f/k/a|d/b/a|a/k/a)\s[^)]*\)`)
var trailingAcronymParenRE = regexp.MustCompile(`\s*\(([A-Za-z0-9&.\-]{2,20})\)\s*$`)

// StripParentheticalAliasAnnotations removes alias/rebranding annotations from names.
func StripParentheticalAliasAnnotations(name string) string {
	cleaned := parentheticalAliasRE.ReplaceAllString(name, "")
	return strings.TrimSpace(cleaned)
}

func stripTrailingAcronymParenthetical(name string) string {
	m := trailingAcronymParenRE.FindStringSubmatch(name)
	if len(m) < 2 {
		return strings.TrimSpace(name)
	}
	acronym := strings.TrimSpace(m[1])
	if acronym == "" {
		return strings.TrimSpace(name)
	}
	// Keep longer explanatory parentheses; remove only compact ticker/acronym markers.
	if strings.Contains(acronym, " ") {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(trailingAcronymParenRE.ReplaceAllString(name, ""))
}

// NormalizeLegalName normalizes legal names for matching/deduplication.
func NormalizeLegalName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return ""
	}

	name = StripParentheticalAliasAnnotations(name)
	name = stripTrailingAcronymParenthetical(name)
	name = strings.ReplaceAll(name, ".", "")
	name = strings.ReplaceAll(name, ",", "")
	name = strings.ReplaceAll(name, "&", "and")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.Join(strings.Fields(name), " ")

	changed := true
	for changed {
		changed = false
		for _, suffix := range LegalSuffixesToRemove {
			sfx := strings.ToLower(strings.TrimSpace(suffix))
			sfx = strings.ReplaceAll(sfx, ".", "")
			sfx = strings.ReplaceAll(sfx, ",", "")
			sfx = strings.Join(strings.Fields(sfx), " ")
			if sfx == "" {
				continue
			}
			if strings.HasSuffix(name, " "+sfx) {
				name = strings.TrimSpace(strings.TrimSuffix(name, " "+sfx))
				changed = true
				break
			}
			if name == sfx {
				name = ""
				changed = true
				break
			}
		}
	}

	// Strip legal form prefixes (e.g. "JSC Kazakhtelecom" -> "kazakhtelecom").
	// Same geo + base name regardless of prefix/suffix form = same entity.
	for _, prefix := range LegalPrefixesToRemove {
		pfx := strings.ToLower(strings.TrimSpace(prefix))
		if pfx == "" {
			continue
		}
		if strings.HasPrefix(name, pfx) {
			name = strings.TrimSpace(strings.TrimPrefix(name, pfx))
			break
		}
	}

	name = strings.Join(strings.Fields(name), " ")
	return name
}

// NormalizeLookupText lowercases and strips punctuation for tolerant lookup/indexing.
func NormalizeLookupText(in string) string {
	s := strings.ToLower(strings.TrimSpace(in))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
			continue
		}
		if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(strings.Join(strings.Fields(b.String()), " "))
}

// IsLatinScript reports whether all letters in text are from the Latin script.
func IsLatinScript(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	hasLetter := false
	for _, r := range t {
		if unicode.IsLetter(r) {
			hasLetter = true
			if !unicode.In(r, unicode.Latin) {
				return false
			}
		}
	}
	return hasLetter
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func titleCaseWords(s string) string {
	parts := strings.Fields(strings.TrimSpace(s))
	for i, p := range parts {
		r := []rune(strings.ToLower(p))
		if len(r) == 0 {
			continue
		}
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}

// BestEffortLabelFromCanonicalPath derives a readable label from canonical path slug.
func BestEffortLabelFromCanonicalPath(canonicalPath string) string {
	cp := strings.Trim(strings.TrimSpace(canonicalPath), "/")
	if cp == "" {
		return ""
	}
	parts := strings.Split(cp, "/")
	leaf := strings.TrimSpace(parts[len(parts)-1])
	if leaf == "" {
		return ""
	}
	leaf = strings.ReplaceAll(leaf, "-", " ")
	leaf = strings.ReplaceAll(leaf, "_", " ")
	leaf = strings.Join(strings.Fields(leaf), " ")
	if leaf == "" {
		return ""
	}
	if leaf == strings.ToLower(leaf) {
		return titleCaseWords(leaf)
	}
	return leaf
}

// NormalizeFragmentNames applies shared naming policy to fragment label/identifier fields.
// It mutates and returns the provided maps (initializing them when nil).
func NormalizeFragmentNames(canonicalPath string, labels map[string]string, identifiers map[string]string) (map[string]string, map[string]string) {
	if labels == nil {
		labels = map[string]string{}
	}
	if identifiers == nil {
		identifiers = map[string]string{}
	}

	for k, v := range labels {
		labels[k] = strings.TrimSpace(v)
	}
	for k, v := range identifiers {
		identifiers[k] = strings.TrimSpace(v)
	}

	legal := StripParentheticalAliasAnnotations(strings.TrimSpace(identifiers["legalName"]))
	short := StripParentheticalAliasAnnotations(strings.TrimSpace(identifiers["shortName"]))
	labelEN := StripParentheticalAliasAnnotations(strings.TrimSpace(labels["en"]))

	if legal == "" && labelEN != "" {
		legal = labelEN
	}
	if short == "" {
		short = firstNonEmpty(labelEN, legal)
	}
	if labelEN == "" {
		labelEN = firstNonEmpty(
			pickLatin(short),
			pickLatin(legal),
			BestEffortLabelFromCanonicalPath(canonicalPath),
		)
	}
	if labelEN != "" && !IsLatinScript(labelEN) {
		labelEN = firstNonEmpty(
			pickLatin(short),
			pickLatin(legal),
			BestEffortLabelFromCanonicalPath(canonicalPath),
		)
	}
	if legal == "" {
		legal = firstNonEmpty(labelEN, short)
	}
	if short == "" {
		short = firstNonEmpty(labelEN, legal)
	}

	if labelEN != "" {
		labels["en"] = labelEN
	}
	if legal != "" {
		identifiers["legalName"] = legal
	}
	if short != "" {
		identifiers["shortName"] = short
	}

	return labels, identifiers
}

func pickLatin(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if IsLatinScript(s) {
		return s
	}
	return ""
}

// NormalizeRelationNames applies shared naming policy to relation target naming fields.
func NormalizeRelationNames(targetType, targetName, targetLegalName, targetShortName string) (string, string, string) {
	_ = targetType // reserved for stricter type-specific handling if needed.
	targetName = StripParentheticalAliasAnnotations(strings.TrimSpace(targetName))
	targetLegalName = StripParentheticalAliasAnnotations(strings.TrimSpace(targetLegalName))
	targetShortName = StripParentheticalAliasAnnotations(strings.TrimSpace(targetShortName))

	if targetLegalName == "" {
		targetLegalName = firstNonEmpty(targetName, targetShortName)
	}
	if targetShortName == "" {
		targetShortName = firstNonEmpty(targetName, targetLegalName)
	}
	if targetName == "" {
		targetName = firstNonEmpty(targetLegalName, targetShortName)
	}
	if targetName != "" && !IsLatinScript(targetName) {
		targetName = firstNonEmpty(pickLatin(targetShortName), pickLatin(targetLegalName), targetName)
	}
	return targetName, targetLegalName, targetShortName
}

// PromptEntityNameNormalizationRules returns shared prompt rules for fragment/relation name handling.
func PromptEntityNameNormalizationRules() string {
	return `Name Normalization Rules (CRITICAL):
- For organizations and government bodies, ALWAYS provide:
  - fragment.identifiers.legalName: legal/official name including ownership/legal form when relevant (Inc., Ltd., LLC, GmbH, AG, S.A., PLC...)
  - fragment.identifiers.shortName: base name without ownership/legal form
- For relations, ALWAYS provide targetLegalName and targetShortName in addition to targetName.
- IMPORTANT (legal form placement):
  - Do NOT move legal form tokens to the beginning (BAD: "JSC Tochka Bank").
  - Keep official ordering from sources/registries.
  - legal_form belongs as a suffix in canonical org paths and in canonicalParts.Org.LegalForm.
- IMPORTANT (official vs transliteration):
  - Prefer the OFFICIAL legal name as published by company/registry/primary source.
  - If official legal name is non-Latin and no established official English name exists:
    - Keep fragment.identifiers.legalName in original script when known.
    - Use fragment.labels.en for transliteration/English display only (do NOT invent a new legalName).
  - fragment.labels.en MUST be Latin-script only (broad Latin is allowed, including diacritics).
- GOV-specific naming (when canonicalPath starts with /gov/):
  - Always provide BOTH fragment.labels.en and fragment.identifiers.legalName.
  - fragment.labels.en: readable English name (official translation if exists, otherwise careful transliteration).
  - fragment.identifiers.legalName: official native-script name from primary source/registry.
  - If native language/script is known, also set fragment.labels.<iso639> (e.g., labels.th, labels.ms, labels.ar, labels.ru).
  - Do NOT replace native official legalName with translation; keep translation/transliteration in labels.en.`
}

// PromptAnchorNameRules returns shared naming rules for intent anchors.
func PromptAnchorNameRules() string {
	return `Anchor naming rules:
- anchor.name is a display label, NOT a slug.
- Prefer established English name; if unavailable, use careful Latin transliteration.
- Do NOT include rebranding notes in parentheses (formerly/fka/aka) in anchor.name.
- Keep legal/official naming consistency with fragment/relation naming rules.`
}
