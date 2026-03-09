package models

import "strings"

const (
	SourceScopeGlobalSystem = "global_system"
	SourceScopePublic       = "public"
	SourceScopeProject      = "project"

	SourceProcessingModeHeuristic = "heuristic"
	SourceProcessingModeCanvasLLM = "canvas_llm"

	SourceOutputModeDocumentFeed = "document_feed"
	SourceOutputModeOntologyFact = "ontology_fact"

	SourcePauseReasonAutoErrorThreshold = "auto_error_threshold"
)

func cloneStringPointer(in *string) *string {
	if in == nil {
		return nil
	}
	v := strings.TrimSpace(*in)
	if v == "" {
		return nil
	}
	out := v
	return &out
}

func cloneBoolPointer(in *bool) *bool {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func normalizeFacetListCompat(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func normalizeSourceScopeValue(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SourceScopeGlobalSystem:
		return SourceScopeGlobalSystem
	case SourceScopePublic:
		return SourceScopePublic
	case SourceScopeProject:
		return SourceScopeProject
	default:
		return ""
	}
}

func normalizeSourceProcessingModeValue(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SourceProcessingModeHeuristic:
		return SourceProcessingModeHeuristic
	case SourceProcessingModeCanvasLLM:
		return SourceProcessingModeCanvasLLM
	default:
		return ""
	}
}

func normalizeSourceOutputModeValue(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SourceOutputModeDocumentFeed:
		return SourceOutputModeDocumentFeed
	case SourceOutputModeOntologyFact:
		return SourceOutputModeOntologyFact
	case "query_only":
		return "query_only"
	default:
		return ""
	}
}

func isSystemCapableSourceKind(kind string) bool {
	switch NormalizeSourceKind(strings.TrimSpace(kind)) {
	case SourceKindRSS, SourceKindHorizonFeed, SourceKindWorldMonitorLegacy:
		return true
	default:
		return false
	}
}

func normalizeSourceCategoryCompat(paths []string, primary *string) ([]string, *string) {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	var normalizedPrimary *string
	if primary != nil {
		v := strings.TrimSpace(*primary)
		if v != "" {
			normalizedPrimary = &v
			if _, ok := seen[v]; !ok {
				out = append([]string{v}, out...)
			} else {
				reordered := make([]string, 0, len(out))
				reordered = append(reordered, v)
				for _, p := range out {
					if p != v {
						reordered = append(reordered, p)
					}
				}
				out = reordered
			}
		}
	}
	if normalizedPrimary == nil && len(out) > 0 {
		v := out[0]
		normalizedPrimary = &v
	}
	return out, normalizedPrimary
}

// NormalizeHorizonMonitorAliases reads legacy worldmonitor* aliases into canonical horizonMonitor* fields.
func (d *SourceDetails) NormalizeHorizonMonitorAliases() {
	if d == nil {
		return
	}
	if d.HorizonMonitorCategory == nil {
		d.HorizonMonitorCategory = cloneStringPointer(d.WorldMonitorCategoryLegacy)
	}
	if d.HorizonMonitorFeedType == nil {
		d.HorizonMonitorFeedType = cloneStringPointer(d.WorldMonitorFeedTypeLegacy)
	}
	if d.HorizonMonitorIngestMode == nil {
		d.HorizonMonitorIngestMode = cloneStringPointer(d.WorldMonitorIngestModeLegacy)
	}
	if d.HorizonMonitorSignalType == nil {
		d.HorizonMonitorSignalType = cloneStringPointer(d.WorldMonitorSignalTypeLegacy)
	}
	if d.HorizonMonitorRequiresAuth == nil {
		d.HorizonMonitorRequiresAuth = cloneBoolPointer(d.WorldMonitorRequiresAuthLegacy)
	}
	if d.SourceScope != nil {
		if v := normalizeSourceScopeValue(*d.SourceScope); v != "" {
			d.SourceScope = &v
		} else {
			d.SourceScope = nil
		}
	}
	if d.ProcessingMode != nil {
		if v := normalizeSourceProcessingModeValue(*d.ProcessingMode); v != "" {
			d.ProcessingMode = &v
		} else {
			d.ProcessingMode = nil
		}
	}
	if d.OutputMode != nil {
		if v := normalizeSourceOutputModeValue(*d.OutputMode); v != "" {
			d.OutputMode = &v
		} else {
			d.OutputMode = nil
		}
	}
}

// SyncHorizonMonitorAliases mirrors canonical horizonMonitor* fields into legacy worldmonitor* aliases.
func (d *SourceDetails) SyncHorizonMonitorAliases() {
	if d == nil {
		return
	}
	d.NormalizeHorizonMonitorAliases()

	d.WorldMonitorCategoryLegacy = cloneStringPointer(d.HorizonMonitorCategory)
	d.WorldMonitorFeedTypeLegacy = cloneStringPointer(d.HorizonMonitorFeedType)
	d.WorldMonitorIngestModeLegacy = cloneStringPointer(d.HorizonMonitorIngestMode)
	d.WorldMonitorSignalTypeLegacy = cloneStringPointer(d.HorizonMonitorSignalType)
	d.WorldMonitorRequiresAuthLegacy = cloneBoolPointer(d.HorizonMonitorRequiresAuth)
}

// NormalizeCompat normalizes source kind aliases and legacy detail fields.
func (ds *DataSource) NormalizeCompat() {
	if ds == nil {
		return
	}
	ds.Kind = NormalizeSourceKind(ds.Kind)
	ds.Details.NormalizeHorizonMonitorAliases()
	ds.SourceCategoryPaths, ds.PrimarySourceCategoryPath = normalizeSourceCategoryCompat(ds.SourceCategoryPaths, ds.PrimarySourceCategoryPath)
	ds.SourceGeoFacets = normalizeFacetListCompat(ds.SourceGeoFacets)
	ds.SourceFormatFacets = normalizeFacetListCompat(ds.SourceFormatFacets)
}

// SyncCompatForWrite performs compatibility sync before writes/serialization.
func (ds *DataSource) SyncCompatForWrite() {
	if ds == nil {
		return
	}
	ds.Kind = NormalizeSourceKind(ds.Kind)
	ds.applyCanonicalSourcePolicyDefaults()
	ds.Details.SyncHorizonMonitorAliases()
	ds.SourceCategoryPaths, ds.PrimarySourceCategoryPath = normalizeSourceCategoryCompat(ds.SourceCategoryPaths, ds.PrimarySourceCategoryPath)
	ds.SourceGeoFacets = normalizeFacetListCompat(ds.SourceGeoFacets)
	ds.SourceFormatFacets = normalizeFacetListCompat(ds.SourceFormatFacets)
}

func (ds *DataSource) applyCanonicalSourcePolicyDefaults() {
	if ds == nil {
		return
	}
	if ds.Details.SourceScope == nil {
		scope := SourceScopePublic
		if strings.TrimSpace(ds.TenantID) != "" {
			scope = SourceScopeProject
		} else if isSystemCapableSourceKind(ds.Kind) && ds.Details.DisableLLMProcessing != nil && *ds.Details.DisableLLMProcessing {
			scope = SourceScopeGlobalSystem
		}
		ds.Details.SourceScope = &scope
	}
	if ds.Details.ProcessingMode == nil {
		var mode string
		switch {
		case ds.Details.DisableLLMProcessing != nil && *ds.Details.DisableLLMProcessing:
			mode = SourceProcessingModeHeuristic
		case SourceScopeValue(ds) == SourceScopeGlobalSystem:
			mode = SourceProcessingModeCanvasLLM
		}
		if mode != "" {
			ds.Details.ProcessingMode = &mode
		}
	}
	if ds.Details.OutputMode == nil {
		var output string
		if ds.Details.HorizonMonitorIngestMode != nil {
			output = normalizeSourceOutputModeValue(*ds.Details.HorizonMonitorIngestMode)
		}
		if output == "" && isSystemCapableSourceKind(ds.Kind) {
			output = SourceOutputModeDocumentFeed
		}
		if output != "" {
			ds.Details.OutputMode = &output
		}
	}
	if ds.Details.CadencePolicy == nil {
		base := 30
		if ds.Details.PollIntervalMinutes != nil && *ds.Details.PollIntervalMinutes > 0 {
			base = *ds.Details.PollIntervalMinutes
		} else if IsGlobalSystemSource(ds) && UsesHeuristicProcessing(ds) {
			base = 60
		}
		max := 360
		empty := 2.0
		errMult := 2.0
		boost := 120
		lease := 90
		autoPauseAfterErrors := 10
		autoPauseDuration := 360
		ds.Details.CadencePolicy = &SourceCadencePolicy{
			BaseIntervalMinutes:         intPtrCompat(base),
			MinIntervalMinutes:          intPtrCompat(base),
			MaxIntervalMinutes:          intPtrCompat(max),
			EmptyBackoffMultiplier:      floatPtrCompat(empty),
			ErrorBackoffMultiplier:      floatPtrCompat(errMult),
			FreshnessBoostWindowMinutes: intPtrCompat(boost),
			LeaseDurationMinutes:        intPtrCompat(lease),
			AutoPauseAfterErrors:        intPtrCompat(autoPauseAfterErrors),
			AutoPauseDurationMinutes:    intPtrCompat(autoPauseDuration),
		}
	} else {
		if ds.Details.CadencePolicy.AutoPauseAfterErrors == nil {
			autoPauseAfterErrors := 10
			ds.Details.CadencePolicy.AutoPauseAfterErrors = intPtrCompat(autoPauseAfterErrors)
		}
		if ds.Details.CadencePolicy.AutoPauseDurationMinutes == nil {
			autoPauseDuration := 360
			ds.Details.CadencePolicy.AutoPauseDurationMinutes = intPtrCompat(autoPauseDuration)
		}
	}
	if mode := SourceProcessingModeValue(ds); mode != "" {
		disable := mode == SourceProcessingModeHeuristic
		ds.Details.DisableLLMProcessing = &disable
	}
	if output := SourceOutputModeValue(ds); output != "" {
		ds.Details.HorizonMonitorIngestMode = &output
	}
}

// SourceScopeValue returns the effective routing scope.
func SourceScopeValue(ds *DataSource) string {
	if ds == nil {
		return ""
	}
	if ds.Details.SourceScope != nil {
		if v := normalizeSourceScopeValue(*ds.Details.SourceScope); v != "" {
			return v
		}
	}
	if strings.TrimSpace(ds.TenantID) != "" {
		return SourceScopeProject
	}
	return SourceScopePublic
}

// SourceProcessingModeValue returns the effective processing lane.
func SourceProcessingModeValue(ds *DataSource) string {
	if ds == nil {
		return ""
	}
	if ds.Details.ProcessingMode != nil {
		if v := normalizeSourceProcessingModeValue(*ds.Details.ProcessingMode); v != "" {
			return v
		}
	}
	return ""
}

func SourceOutputModeValue(ds *DataSource) string {
	if ds == nil {
		return ""
	}
	if ds.Details.OutputMode != nil {
		if v := normalizeSourceOutputModeValue(*ds.Details.OutputMode); v != "" {
			return v
		}
	}
	if isSystemCapableSourceKind(ds.Kind) {
		return SourceOutputModeDocumentFeed
	}
	return ""
}

func IsGlobalSystemSource(ds *DataSource) bool {
	return SourceScopeValue(ds) == SourceScopeGlobalSystem
}

func UsesHeuristicProcessing(ds *DataSource) bool {
	return SourceProcessingModeValue(ds) == SourceProcessingModeHeuristic
}

func UsesCanvasLLMProcessing(ds *DataSource) bool {
	return SourceProcessingModeValue(ds) == SourceProcessingModeCanvasLLM
}

func ShouldDisableLLMProcessing(ds *DataSource) bool {
	return UsesHeuristicProcessing(ds)
}

func PauseReasonValue(ds *DataSource) string {
	if ds == nil || ds.PauseReason == nil {
		return ""
	}
	return strings.TrimSpace(*ds.PauseReason)
}

func IsAutoPaused(ds *DataSource) bool {
	return ds != nil && strings.TrimSpace(ds.Status) == "paused" && PauseReasonValue(ds) == SourcePauseReasonAutoErrorThreshold
}

func intPtrCompat(v int) *int {
	return &v
}

func floatPtrCompat(v float64) *float64 {
	return &v
}
