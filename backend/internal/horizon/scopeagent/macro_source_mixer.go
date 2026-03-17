package scopeagent

import (
	"fmt"
	"sort"
	"strings"
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

func buildMacroSourceBundle(
	cfg *horizonmodels.ScopeAgentConfig,
	subject *horizonmodels.WatchSubject,
	artifact *horizonmodels.MaterializedArtifact,
	triggers []*horizonmodels.WatchTrigger,
	facts []*coremodels.OntologyFact,
) (map[string]any, []string) {
	if cfg == nil {
		return nil, nil
	}
	window := inferSourceMixWindow(cfg, subject)
	queryFamilies := buildSourceMixQueryFamilies(cfg, subject, artifact, triggers, window)
	sourceRefs := dedupeMacroSourceRefs(facts)
	bundle := map[string]any{
		"discoveredBy":         "macro_agent",
		"macroAgentClass":      strings.TrimSpace(asString(cfg.Metadata["macroAgentClass"])),
		"watchSubjectId":       strings.TrimSpace(firstNonEmpty(cfg.SubjectID, asString(cfg.Metadata["watchSubjectId"]))),
		"lensId":               strings.TrimSpace(asString(cfg.Metadata["lensId"])),
		"artifactCanonicalKey": strings.TrimSpace(asString(cfg.Metadata["artifactCanonicalKey"])),
		"searchWindow": map[string]any{
			"days":        window.Days,
			"description": window.Description,
		},
		"queryFamilies": queryFamilies,
		"sourceRefs":    sourceRefs,
	}
	if artifact != nil {
		bundle["artifactSummary"] = serializeArtifactSummary(artifact)
	}
	return bundle, queryFamiliesToQueries(queryFamilies)
}

type sourceMixWindow struct {
	Days        int
	Description string
}

type sourceMixQueryFamily struct {
	Name    string   `json:"name"`
	Topic   string   `json:"topic"`
	Recency string   `json:"recency"`
	Queries []string `json:"queries"`
}

func inferSourceMixWindow(cfg *horizonmodels.ScopeAgentConfig, subject *horizonmodels.WatchSubject) sourceMixWindow {
	defaultDays := 21
	if cfg != nil && cfg.Metadata != nil {
		if policy, ok := cfg.Metadata["sourceMixPolicy"].(map[string]any); ok {
			if days, ok := toInt(policy["lookbackDays"]); ok && days > 0 {
				defaultDays = days
			}
		}
	}
	if subject != nil && subject.Metadata != nil {
		if policy, ok := subject.Metadata["timeWindowPolicy"].(map[string]any); ok {
			if days, ok := toInt(policy["lookbackDays"]); ok && days > 0 {
				defaultDays = days
			}
		}
	}
	description := fmt.Sprintf("last %d days", defaultDays)
	if defaultDays <= 7 {
		description = fmt.Sprintf("last %d days (high recency)", defaultDays)
	}
	return sourceMixWindow{Days: defaultDays, Description: description}
}

func buildSourceMixQueryFamilies(
	cfg *horizonmodels.ScopeAgentConfig,
	subject *horizonmodels.WatchSubject,
	artifact *horizonmodels.MaterializedArtifact,
	triggers []*horizonmodels.WatchTrigger,
	window sourceMixWindow,
) []sourceMixQueryFamily {
	label := strings.TrimSpace(firstNonEmpty(runContextLabel(&runContext{Config: cfg, Subject: subject}), cfg.ScopeCanonicalPath))
	modes := compactUnique(cfg.DomainModes)
	triggerTerms := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		if trigger == nil {
			continue
		}
		if reason := strings.TrimSpace(trigger.TriggerReason); reason != "" {
			triggerTerms = append(triggerTerms, reason)
		}
	}
	families := []sourceMixQueryFamily{
		{
			Name:    "subject_latest",
			Topic:   label,
			Recency: window.Description,
			Queries: compactUnique([]string{
				label + " latest developments " + window.Description,
				label + " " + strings.Join(modes, " ") + " latest " + window.Description,
			}),
		},
	}
	if len(triggerTerms) > 0 {
		families = append(families, sourceMixQueryFamily{
			Name:    "trigger_followup",
			Topic:   label,
			Recency: window.Description,
			Queries: compactUnique([]string{
				label + " " + triggerTerms[0] + " " + window.Description,
			}),
		})
	}
	if artifact != nil {
		artifactTerms := []string{strings.TrimSpace(firstNonEmpty(artifact.DisplayName, artifact.CanonicalKey))}
		if artifact.ProductAxis != "" {
			artifactTerms = append(artifactTerms, artifact.ProductAxis)
		}
		families = append(families, sourceMixQueryFamily{
			Name:    "artifact_overlay",
			Topic:   strings.Join(compactUnique(artifactTerms), " "),
			Recency: window.Description,
			Queries: compactUnique([]string{
				label + " supply chain disruption " + window.Description,
				strings.Join(compactUnique(artifactTerms), " ") + " chokepoint disruption " + window.Description,
				strings.Join(compactUnique(artifactTerms), " ") + " route bottleneck " + window.Description,
			}),
		})
	}
	for _, override := range queryOverridesFromConfig(cfg, subject) {
		families = append(families, sourceMixQueryFamily{
			Name:    "manual_override",
			Topic:   label,
			Recency: window.Description,
			Queries: []string{strings.TrimSpace(override)},
		})
	}
	return families
}

func queryFamiliesToQueries(families []sourceMixQueryFamily) []string {
	out := make([]string, 0, len(families)*2)
	for _, family := range families {
		out = append(out, family.Queries...)
	}
	return compactUnique(out)
}

func queryOverridesFromConfig(cfg *horizonmodels.ScopeAgentConfig, subject *horizonmodels.WatchSubject) []string {
	overrides := []string{}
	if cfg != nil && cfg.Metadata != nil {
		overrides = append(overrides, parseAnyStringSlice(cfg.Metadata["queryOverrides"])...)
	}
	if subject != nil && subject.Metadata != nil {
		overrides = append(overrides, parseAnyStringSlice(subject.Metadata["queryOverrides"])...)
	}
	return compactUnique(overrides)
}

func dedupeMacroSourceRefs(facts []*coremodels.OntologyFact) []map[string]any {
	type item struct {
		key string
		ref map[string]any
	}
	seen := map[string]struct{}{}
	items := make([]item, 0, 16)
	for _, fact := range facts {
		if fact == nil {
			continue
		}
		for _, ref := range fact.SourceRefs {
			sourceID := strings.TrimSpace(ref.SourceID)
			key := strings.TrimSpace(firstNonEmpty(sourceID, ref.SourceType))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			items = append(items, item{
				key: key,
				ref: map[string]any{
					"sourceId":   strings.TrimSpace(firstNonEmpty(sourceID, key)),
					"sourceType": strings.TrimSpace(ref.SourceType),
				},
			})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].key < items[j].key })
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.ref)
	}
	return out
}

func serializeArtifactSummary(artifact *horizonmodels.MaterializedArtifact) map[string]any {
	if artifact == nil {
		return nil
	}
	summary := map[string]any{
		"id":                  strings.TrimSpace(artifact.ID),
		"canonicalKey":        strings.TrimSpace(artifact.CanonicalKey),
		"kind":                string(artifact.Kind),
		"displayName":         strings.TrimSpace(artifact.DisplayName),
		"nodeRefs":            compactUnique(artifact.NodeRefs),
		"geoRefs":             compactUnique(artifact.GeoRefs),
		"flowRefs":            compactUnique(artifact.FlowRefs),
		"zoneRefs":            compactUnique(artifact.ZoneRefs),
		"affectedZoneRefs":    compactUnique(artifact.AffectedZoneRefs),
		"pathStressScore":     artifact.PathStressScore,
		"bindingScore":        artifact.BindingScore,
		"gapCount":            artifact.GapCount,
		"unresolvedJoinCount": artifact.UnresolvedJoinCount,
		"bottleneckLegIndex":  artifact.BottleneckLeg,
	}
	if legs, err := horizonmodels.GetSupplyChainPathLegs(artifact); err == nil && len(legs) > 0 {
		topLegs := make([]map[string]any, 0, 3)
		for idx, leg := range legs {
			if idx >= 3 {
				break
			}
			item := map[string]any{
				"index":             idx,
				"supplierRef":       strings.TrimSpace(leg.SupplierRef),
				"customerRef":       strings.TrimSpace(leg.CustomerRef),
				"productRef":        strings.TrimSpace(leg.ProductRef),
				"bindingConfidence": leg.BindingConfidence,
				"geoRefs":           compactUnique(leg.GeoRefs),
				"zoneRefs":          compactUnique(leg.ZoneRefs),
				"affectedZoneRefs":  compactUnique(leg.AffectedZoneRefs),
			}
			if leg.Stress != nil {
				item["stressScore"] = leg.Stress.StressScore
			}
			topLegs = append(topLegs, item)
		}
		summary["topLegs"] = topLegs
	}
	return summary
}

func serializeWatchTriggers(triggers []*horizonmodels.WatchTrigger) []map[string]any {
	out := make([]map[string]any, 0, len(triggers))
	for _, trigger := range triggers {
		if trigger == nil {
			continue
		}
		out = append(out, map[string]any{
			"type":           string(trigger.TriggerType),
			"reason":         strings.TrimSpace(trigger.TriggerReason),
			"signalStrength": trigger.SignalStrength,
			"confidence":     trigger.Confidence,
			"matchMode":      string(trigger.MatchMode),
			"createdAt":      trigger.CreatedAt.Format(time.RFC3339),
		})
	}
	return out
}
