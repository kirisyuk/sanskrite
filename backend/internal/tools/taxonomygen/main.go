// Command taxonomygen reads the AgentHorde taxonomy JSON snapshot
// (shared/taxonomy/taxonomy.json) and emits a Go file containing typed
// constants and lookup tables for fragment classes, relations, subtype enums,
// attribute keys, sanctions regimes, and incoterms.
//
// It is the Go half of the codegen pipeline driven by go generate inside the
// generated package. The single source of truth lives in
// agents/shared/agenthorde_common/agenthorde_common/canonicalizer/taxonomy.py.
//
// Usage (typically invoked via go generate, see backend/internal/taxonomy/ah/doc.go):
//
//	taxonomygen -in shared/taxonomy/taxonomy.json -out internal/taxonomy/ah/taxonomy_generated.go -package ah
//
// CI guard mode:
//
//	taxonomygen -in shared/taxonomy/taxonomy.json -out internal/taxonomy/ah/taxonomy_generated.go -package ah -check
//
// exits non-zero if the file on disk does not match what would be emitted.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// taxonomyPayload mirrors the JSON shape produced by
// agents/shared/agenthorde_common/scripts/export_taxonomy.py. We keep it
// schema-version-agnostic at the top level: unknown keys are ignored.
type taxonomyPayload struct {
	SchemaVersion   string                                  `json:"schema_version"`
	FragmentClasses fragmentClassesBlock                    `json:"fragment_classes"`
	SubtypeEnums    map[string]subtypeEnumBlock             `json:"subtype_enums"`
	Relations       relationsBlock                          `json:"relations"`
	Incoterms       []string                                `json:"incoterms"`
	SanctionsRegime map[string]sanctionsRegimeSpec          `json:"sanctions_regimes"`
	AttributeSpec   []attributeSpec                         `json:"attribute_spec"`
}

type fragmentClassesBlock struct {
	All            []string `json:"all"`
	Structural     []string `json:"structural"`
	DecisionLayer  []string `json:"decision_layer"`
}

type subtypeEnumBlock struct {
	Description string   `json:"description"`
	Values      []string `json:"values"`
}

type relationsBlock struct {
	Names              []string                       `json:"names"`
	AllowedLayers      map[string][]string            `json:"allowed_layers"`
	LayerSections      map[string]string              `json:"layer_sections"`
	EndpointKinds      map[string][]endpointKindRow   `json:"endpoint_kinds"`
	EndpointBlacklist  map[string][]endpointPair      `json:"endpoint_blacklist"`
}

type endpointKindRow struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type endpointPair struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type sanctionsRegimeSpec struct {
	CanonicalPath    string   `json:"canonical_path"`
	Label            string   `json:"label"`
	JurisdictionISO2 string   `json:"jurisdiction_iso2"`
	Aliases          []string `json:"aliases"`
}

type attributeSpec struct {
	Name        string   `json:"name"`
	Carrier     string   `json:"carrier"`
	AppliesTo   []string `json:"applies_to"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Description string   `json:"description"`
}

func main() {
	in := flag.String("in", "shared/taxonomy/taxonomy.json", "Path to taxonomy JSON snapshot")
	out := flag.String("out", "internal/taxonomy/ah/taxonomy_generated.go", "Path to write generated Go file")
	pkg := flag.String("package", "ah", "Generated Go package name")
	check := flag.Bool("check", false, "Do not write; exit non-zero if -out is stale relative to freshly built source")
	flag.Parse()

	payload, err := loadPayload(*in)
	if err != nil {
		fail("load %s: %v", *in, err)
	}

	src, err := render(payload, *pkg)
	if err != nil {
		fail("render: %v", err)
	}

	if *check {
		existing, readErr := os.ReadFile(*out)
		if readErr != nil {
			fail("check: cannot read %s: %v", *out, readErr)
		}
		if !bytes.Equal(existing, src) {
			fmt.Fprintf(os.Stderr,
				"taxonomygen: --check failed; %s is stale.\n"+
					"Run `go generate ./internal/taxonomy/ah` (after refreshing %s) and commit the result.\n",
				*out, *in,
			)
			os.Exit(2)
		}
		return
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fail("mkdir %s: %v", filepath.Dir(*out), err)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		fail("write %s: %v", *out, err)
	}
	fmt.Fprintf(os.Stdout, "taxonomygen: wrote %s (%d bytes)\n", *out, len(src))
}

func loadPayload(path string) (*taxonomyPayload, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p taxonomyPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "taxonomygen: "+format+"\n", args...)
	os.Exit(1)
}

// ---------------------------------------------------------------------------
// Naming helpers.
// ---------------------------------------------------------------------------

// pascal converts snake_case / kebab-case / dot.case strings to PascalCase.
// Already-mixed-case inputs (e.g. "MechanismCategory") are passed through
// unchanged so enum names stay stable.
func pascal(s string) string {
	if s == "" {
		return ""
	}
	if !strings.ContainsAny(s, "_-.") && isLikelyPascal(s) {
		return s
	}
	parts := splitOnSeparators(s)
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, "")
}

func isLikelyPascal(s string) bool {
	if s == "" {
		return false
	}
	first := s[0]
	if first < 'A' || first > 'Z' {
		return false
	}
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func splitOnSeparators(s string) []string {
	out := []string{}
	cur := strings.Builder{}
	for _, r := range s {
		if r == '_' || r == '-' || r == '.' || r == ' ' || r == '/' {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// ---------------------------------------------------------------------------
// Rendering.
// ---------------------------------------------------------------------------

func render(p *taxonomyPayload, pkg string) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by taxonomygen from shared/taxonomy/taxonomy.json; DO NOT EDIT.\n")
	b.WriteString("// Single source of truth: agents/shared/agenthorde_common/agenthorde_common/canonicalizer/taxonomy.py\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("// SchemaVersion is the AgentHorde taxonomy SoT version this file was generated from.\n")
	fmt.Fprintf(&b, "const SchemaVersion = %q\n\n", p.SchemaVersion)

	renderFragmentClasses(&b, p.FragmentClasses)
	renderLayers(&b, p.Relations)
	renderRelationNames(&b, p.Relations.Names)
	renderRelationLayerTables(&b, p.Relations)
	renderRelationEndpointKinds(&b, p.Relations.EndpointKinds)
	renderRelationEndpointBlacklist(&b, p.Relations.EndpointBlacklist)
	renderSubtypeEnums(&b, p.SubtypeEnums)
	renderAttributeSpec(&b, p.AttributeSpec)
	renderIncoterms(&b, p.Incoterms)
	renderSanctionsRegimes(&b, p.SanctionsRegime)

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		// Surface the unformatted source on error so the user can see what we
		// produced — this only happens when our codegen has a real bug.
		fmt.Fprintln(os.Stderr, b.String())
		return nil, fmt.Errorf("gofmt: %w", err)
	}
	return formatted, nil
}

func renderFragmentClasses(b *strings.Builder, blk fragmentClassesBlock) {
	all := append([]string{}, blk.All...)
	sort.Strings(all)
	b.WriteString("// FragmentClass enumerates the AgentHorde structural + semantic fragment classes.\n")
	b.WriteString("// Wire / canonical-path values are snake_case and stable; the Go identifiers are PascalCase.\n")
	b.WriteString("type FragmentClass string\n\n")
	b.WriteString("const (\n")
	for _, c := range all {
		fmt.Fprintf(b, "\tFragmentClass%s FragmentClass = %q\n", pascal(c), c)
	}
	b.WriteString(")\n\n")

	b.WriteString("// FragmentClasses lists every fragment class in deterministic order.\n")
	b.WriteString("var FragmentClasses = []FragmentClass{\n")
	for _, c := range all {
		fmt.Fprintf(b, "\tFragmentClass%s,\n", pascal(c))
	}
	b.WriteString("}\n\n")

	structural := append([]string{}, blk.Structural...)
	sort.Strings(structural)
	b.WriteString("// StructuralFragmentClasses are fragments that participate in structural relations\n")
	b.WriteString("// (ownership, operations, flows). Decision-layer fragments are excluded.\n")
	b.WriteString("var StructuralFragmentClasses = map[FragmentClass]struct{}{\n")
	for _, c := range structural {
		fmt.Fprintf(b, "\tFragmentClass%s: {},\n", pascal(c))
	}
	b.WriteString("}\n\n")

	dec := append([]string{}, blk.DecisionLayer...)
	sort.Strings(dec)
	b.WriteString("// DecisionLayerFragmentClasses are fragments that live in the decision layer\n")
	b.WriteString("// (decision points, mechanisms). They participate only in decision-layer relations.\n")
	b.WriteString("var DecisionLayerFragmentClasses = map[FragmentClass]struct{}{\n")
	for _, c := range dec {
		fmt.Fprintf(b, "\tFragmentClass%s: {},\n", pascal(c))
	}
	b.WriteString("}\n\n")
}

func collectLayers(rel relationsBlock) []string {
	seen := map[string]struct{}{}
	for _, layers := range rel.AllowedLayers {
		for _, l := range layers {
			seen[l] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for l := range seen {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

func collectLayerSections(rel relationsBlock) []string {
	seen := map[string]struct{}{}
	for _, sec := range rel.LayerSections {
		seen[sec] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for l := range seen {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

func renderLayers(b *strings.Builder, rel relationsBlock) {
	layers := collectLayers(rel)
	b.WriteString("// Layer enumerates the supergraph layers a relation may live in.\n")
	b.WriteString("type Layer string\n\n")
	b.WriteString("const (\n")
	for _, l := range layers {
		fmt.Fprintf(b, "\tLayer%s Layer = %q\n", pascal(l), l)
	}
	b.WriteString(")\n\n")
	b.WriteString("// Layers lists every supergraph layer in deterministic order.\n")
	b.WriteString("var Layers = []Layer{\n")
	for _, l := range layers {
		fmt.Fprintf(b, "\tLayer%s,\n", pascal(l))
	}
	b.WriteString("}\n\n")

	sections := collectLayerSections(rel)
	b.WriteString("// LayerSection enumerates the topical buckets a relation rolls up into.\n")
	b.WriteString("type LayerSection string\n\n")
	b.WriteString("const (\n")
	for _, s := range sections {
		fmt.Fprintf(b, "\tLayerSection%s LayerSection = %q\n", pascal(s), s)
	}
	b.WriteString(")\n\n")
	b.WriteString("// LayerSections lists every layer section in deterministic order.\n")
	b.WriteString("var LayerSections = []LayerSection{\n")
	for _, s := range sections {
		fmt.Fprintf(b, "\tLayerSection%s,\n", pascal(s))
	}
	b.WriteString("}\n\n")
}

func renderRelationNames(b *strings.Builder, names []string) {
	sorted := append([]string{}, names...)
	sort.Strings(sorted)
	b.WriteString("// RelationName enumerates AgentHorde relation names (wire-stable snake_case).\n")
	b.WriteString("type RelationName string\n\n")
	b.WriteString("const (\n")
	for _, n := range sorted {
		fmt.Fprintf(b, "\tRelationName%s RelationName = %q\n", pascal(n), n)
	}
	b.WriteString(")\n\n")
	b.WriteString("// RelationNames lists every relation name in deterministic order.\n")
	b.WriteString("var RelationNames = []RelationName{\n")
	for _, n := range sorted {
		fmt.Fprintf(b, "\tRelationName%s,\n", pascal(n))
	}
	b.WriteString("}\n\n")
}

func renderRelationLayerTables(b *strings.Builder, rel relationsBlock) {
	relNames := append([]string{}, rel.Names...)
	sort.Strings(relNames)

	b.WriteString("// RelationAllowedLayers maps each relation to the supergraph layers it may live in.\n")
	b.WriteString("var RelationAllowedLayers = map[RelationName][]Layer{\n")
	for _, n := range relNames {
		layers := append([]string{}, rel.AllowedLayers[n]...)
		sort.Strings(layers)
		fmt.Fprintf(b, "\tRelationName%s: {", pascal(n))
		for i, l := range layers {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "Layer%s", pascal(l))
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n\n")

	b.WriteString("// RelationLayerSection maps each relation to its topical bucket (e.g. \"ownership\", \"flow_topology\").\n")
	b.WriteString("var RelationLayerSection = map[RelationName]LayerSection{\n")
	for _, n := range relNames {
		sec, ok := rel.LayerSections[n]
		if !ok {
			continue
		}
		fmt.Fprintf(b, "\tRelationName%s: LayerSection%s,\n", pascal(n), pascal(sec))
	}
	b.WriteString("}\n\n")
}

func renderRelationEndpointKinds(b *strings.Builder, kinds map[string][]endpointKindRow) {
	b.WriteString("// EndpointKindRow is one allowed (from, to) class pair for a relation, with its semantic kind.\n")
	b.WriteString("type EndpointKindRow struct {\n")
	b.WriteString("\tFrom FragmentClass\n")
	b.WriteString("\tTo   FragmentClass\n")
	b.WriteString("\tKind string\n")
	b.WriteString("}\n\n")

	relNames := make([]string, 0, len(kinds))
	for n := range kinds {
		relNames = append(relNames, n)
	}
	sort.Strings(relNames)

	b.WriteString("// RelationEndpointKinds enumerates every allowed (from -> to) endpoint pair for a relation,\n")
	b.WriteString("// keyed by the wire kind tag the canonicalizer attaches when materializing the edge.\n")
	b.WriteString("var RelationEndpointKinds = map[RelationName][]EndpointKindRow{\n")
	for _, n := range relNames {
		rows := append([]endpointKindRow{}, kinds[n]...)
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].From != rows[j].From {
				return rows[i].From < rows[j].From
			}
			if rows[i].To != rows[j].To {
				return rows[i].To < rows[j].To
			}
			return rows[i].Kind < rows[j].Kind
		})
		fmt.Fprintf(b, "\tRelationName%s: {\n", pascal(n))
		for _, r := range rows {
			fmt.Fprintf(b,
				"\t\t{From: FragmentClass%s, To: FragmentClass%s, Kind: %q},\n",
				pascal(r.From), pascal(r.To), r.Kind,
			)
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n\n")
}

func renderRelationEndpointBlacklist(b *strings.Builder, bl map[string][]endpointPair) {
	b.WriteString("// EndpointPair is an (from, to) class pair, used by the relation endpoint blacklist.\n")
	b.WriteString("type EndpointPair struct {\n")
	b.WriteString("\tFrom FragmentClass\n")
	b.WriteString("\tTo   FragmentClass\n")
	b.WriteString("}\n\n")

	relNames := make([]string, 0, len(bl))
	for n := range bl {
		relNames = append(relNames, n)
	}
	sort.Strings(relNames)

	b.WriteString("// RelationEndpointBlacklist captures (from, to) pairs that must NEVER be used for a relation,\n")
	b.WriteString("// even when both endpoints individually appear in RelationEndpointKinds.\n")
	b.WriteString("var RelationEndpointBlacklist = map[RelationName][]EndpointPair{\n")
	for _, n := range relNames {
		pairs := append([]endpointPair{}, bl[n]...)
		sort.Slice(pairs, func(i, j int) bool {
			if pairs[i].From != pairs[j].From {
				return pairs[i].From < pairs[j].From
			}
			return pairs[i].To < pairs[j].To
		})
		fmt.Fprintf(b, "\tRelationName%s: {\n", pascal(n))
		for _, p := range pairs {
			fmt.Fprintf(b,
				"\t\t{From: FragmentClass%s, To: FragmentClass%s},\n",
				pascal(p.From), pascal(p.To),
			)
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n\n")
}

func renderSubtypeEnums(b *strings.Builder, enums map[string]subtypeEnumBlock) {
	names := make([]string, 0, len(enums))
	for n := range enums {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		blk := enums[name]
		typeName := pascal(name)
		if blk.Description != "" {
			fmt.Fprintf(b, "// %s — %s\n", typeName, blk.Description)
		} else {
			fmt.Fprintf(b, "// %s closed enumeration.\n", typeName)
		}
		fmt.Fprintf(b, "type %s string\n\n", typeName)
		b.WriteString("const (\n")
		values := append([]string{}, blk.Values...)
		sort.Strings(values)
		for _, v := range values {
			fmt.Fprintf(b, "\t%s%s %s = %q\n", typeName, pascal(v), typeName, v)
		}
		b.WriteString(")\n\n")
		fmt.Fprintf(b, "// %sValues lists every %s value in deterministic order.\n", typeName, typeName)
		fmt.Fprintf(b, "var %sValues = []%s{\n", typeName, typeName)
		for _, v := range values {
			fmt.Fprintf(b, "\t%s%s,\n", typeName, pascal(v))
		}
		b.WriteString("}\n\n")
	}
}

func renderAttributeSpec(b *strings.Builder, specs []attributeSpec) {
	b.WriteString("// AttributeSpec describes one attribute slot in the AgentHorde data model: who carries it\n")
	b.WriteString("// (fragment / edge / both), which classes / relations it applies to, its primitive type,\n")
	b.WriteString("// whether it is required, and a human-readable description.\n")
	b.WriteString("type AttributeSpec struct {\n")
	b.WriteString("\tName        string\n")
	b.WriteString("\tCarrier     string\n")
	b.WriteString("\tAppliesTo   []string\n")
	b.WriteString("\tType        string\n")
	b.WriteString("\tRequired    bool\n")
	b.WriteString("\tDescription string\n")
	b.WriteString("}\n\n")

	// Stable, declaration-order list (matches the python author's intent).
	b.WriteString("// AttributeSpecs is the declaration-order list of every attribute slot.\n")
	b.WriteString("var AttributeSpecs = []AttributeSpec{\n")
	for _, s := range specs {
		applies := append([]string{}, s.AppliesTo...)
		fmt.Fprintf(b, "\t{Name: %q, Carrier: %q, AppliesTo: %s, Type: %q, Required: %t, Description: %s},\n",
			s.Name, s.Carrier, goStringSlice(applies), s.Type, s.Required, goRawString(s.Description),
		)
	}
	b.WriteString("}\n\n")

	// Stable PascalCase constants for in-code references to attribute names. Names are
	// de-duplicated by snake_case; multiple specs with the same name (different
	// carriers) collapse to one constant.
	b.WriteString("// Attribute key constants. Use these instead of bare string literals when reading or\n")
	b.WriteString("// writing attribute maps so renames in the SoT propagate at compile time.\n")
	seen := map[string]struct{}{}
	keys := make([]string, 0, len(specs))
	for _, s := range specs {
		if _, ok := seen[s.Name]; ok {
			continue
		}
		seen[s.Name] = struct{}{}
		keys = append(keys, s.Name)
	}
	sort.Strings(keys)
	b.WriteString("const (\n")
	for _, k := range keys {
		fmt.Fprintf(b, "\tAttrKey%s = %q\n", pascal(k), k)
	}
	b.WriteString(")\n\n")
}

func renderIncoterms(b *strings.Builder, items []string) {
	sorted := append([]string{}, items...)
	sort.Strings(sorted)
	b.WriteString("// Incoterms2020 is the closed list of recognized Incoterm codes (ICC 2020 revision).\n")
	b.WriteString("var Incoterms2020 = []string{\n")
	for _, c := range sorted {
		fmt.Fprintf(b, "\t%q,\n", c)
	}
	b.WriteString("}\n\n")
}

func renderSanctionsRegimes(b *strings.Builder, regimes map[string]sanctionsRegimeSpec) {
	b.WriteString("// SanctionsRegimeSpec describes one canonical sanctions / export-control regime.\n")
	b.WriteString("type SanctionsRegimeSpec struct {\n")
	b.WriteString("\tCanonicalPath    string\n")
	b.WriteString("\tLabel            string\n")
	b.WriteString("\tJurisdictionISO2 string\n")
	b.WriteString("\tAliases          []string\n")
	b.WriteString("}\n\n")

	codes := make([]string, 0, len(regimes))
	for c := range regimes {
		codes = append(codes, c)
	}
	sort.Strings(codes)

	b.WriteString("// SanctionsRegimes maps canonical regime codes to their spec.\n")
	b.WriteString("var SanctionsRegimes = map[string]SanctionsRegimeSpec{\n")
	for _, c := range codes {
		spec := regimes[c]
		aliases := append([]string{}, spec.Aliases...)
		fmt.Fprintf(b, "\t%q: {CanonicalPath: %q, Label: %q, JurisdictionISO2: %q, Aliases: %s},\n",
			c, spec.CanonicalPath, spec.Label, spec.JurisdictionISO2, goStringSlice(aliases),
		)
	}
	b.WriteString("}\n\n")
}

// goStringSlice renders a []string{...} literal preserving order.
func goStringSlice(in []string) string {
	if len(in) == 0 {
		return "nil"
	}
	parts := make([]string, len(in))
	for i, s := range in {
		parts[i] = fmt.Sprintf("%q", s)
	}
	return "[]string{" + strings.Join(parts, ", ") + "}"
}

// goRawString picks between %q and a raw-string literal when the description
// contains backticks, escapes, or multi-line content. Always returns a valid
// Go string literal.
func goRawString(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, "`\n") {
		return fmt.Sprintf("%q", s)
	}
	if !strings.Contains(s, "`") {
		return "`" + s + "`"
	}
	return fmt.Sprintf("%q", s)
}
