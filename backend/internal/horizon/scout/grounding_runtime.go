package scout

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/ai"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

const (
	logGrounding = "HZN_GROUNDING"

	groundingContractVersionV1 = "v1"

	envGroundingInternalFirst      = "HORIZON_GROUNDING_INTERNAL_FIRST"
	envGroundingRequireProvenance  = "HORIZON_GROUNDING_REQUIRE_PROVENANCE"
	stateKeyGroundingContractVer   = "groundingContractVersion"
	stateKeyOriginMode             = "originMode"
	stateKeyInternalEvidence       = "internalEvidence"
	stateKeyExternalEvidence       = "externalEvidence"
	stateKeyResolutionStats        = "resolutionStats"
	stateKeyGroundingEvidence      = "groundingEvidence"
	stateKeyGroundingDatastoreURIs = "datastoreUris"
	stateKeyLastErrorPhase         = "lastErrorPhase"
	stateKeyLastErrorReasonCode    = "lastErrorReasonCode"
	stateKeyLastErrorAtUTC         = "lastErrorAtUtc"
	stateKeyLastErrorClass         = "lastErrorClass"
	stateKeyGroundingBestEffortN   = "groundingBestEffortCount"
)

func envEnabled(name string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func internalFirstGroundingEnabled() bool {
	return envEnabled(envGroundingInternalFirst)
}

func requireGroundingProvenanceEnabled() bool {
	return envEnabled(envGroundingRequireProvenance)
}

func logGroundingInfof(format string, args ...interface{}) {
	log.Printf(logGrounding+" "+format, args...)
}

func logGroundingWarnf(format string, args ...interface{}) {
	log.Printf(logGrounding+" WARN: "+format, args...)
}

func logGroundingErrorWithContext(
	job *models.ScoutingJob,
	component,
	phase,
	reasonCode,
	errorClass string,
	bestEffort bool,
	err error,
) {
	jobID := ""
	tenantID := ""
	projectID := ""
	target := ""
	originMode := ""
	if job != nil {
		jobID = strings.TrimSpace(job.ID)
		tenantID = strings.TrimSpace(job.TenantID)
		projectID = strings.TrimSpace(job.ProjectID)
		target = strings.TrimSpace(job.TargetCanonicalPath)
		if job.State != nil {
			originMode = strings.TrimSpace(parseStateString(job.State[stateKeyOriginMode]))
		}
	}
	log.Printf(
		"%s level=error component=%s phase=%s jobId=%s tenantId=%s projectId=%s targetCanonicalPath=%s originMode=%s reasonCode=%s errorClass=%s best_effort=%t err=%v",
		logGrounding,
		strings.TrimSpace(component),
		strings.TrimSpace(phase),
		jobID,
		tenantID,
		projectID,
		target,
		originMode,
		strings.TrimSpace(reasonCode),
		strings.TrimSpace(errorClass),
		bestEffort,
		err,
	)
	recordGroundingErrorState(job, phase, reasonCode, errorClass, bestEffort)
}

func recordGroundingErrorState(job *models.ScoutingJob, phase, reasonCode, errorClass string, bestEffort bool) {
	if job == nil {
		return
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	if s := strings.TrimSpace(phase); s != "" {
		job.State[stateKeyLastErrorPhase] = s
	}
	if s := strings.TrimSpace(reasonCode); s != "" {
		job.State[stateKeyLastErrorReasonCode] = s
	}
	job.State[stateKeyLastErrorAtUTC] = time.Now().UTC().Format(time.RFC3339Nano)
	if s := strings.TrimSpace(errorClass); s != "" {
		job.State[stateKeyLastErrorClass] = s
	}
	if bestEffort {
		job.State[stateKeyGroundingBestEffortN] = parseInt(job.State[stateKeyGroundingBestEffortN]) + 1
	}
}

func mergeGroundingEvidence(base, add *ai.GroundingEvidence) *ai.GroundingEvidence {
	if base == nil && add == nil {
		return nil
	}
	if base == nil {
		cp := *add
		cp.DatastoreFragmentIDs = append([]string(nil), add.DatastoreFragmentIDs...)
		cp.DatastoreURIs = append([]string(nil), add.DatastoreURIs...)
		cp.WebSources = append([]ai.WebCitation(nil), add.WebSources...)
		return &cp
	}
	if add == nil {
		return base
	}
	base.HasDatastoreResults = base.HasDatastoreResults || add.HasDatastoreResults
	base.HasWebResults = base.HasWebResults || add.HasWebResults
	base.DatastoreFragmentIDs = appendUniqueTrim(base.DatastoreFragmentIDs, add.DatastoreFragmentIDs...)
	base.DatastoreURIs = appendUniqueTrim(base.DatastoreURIs, add.DatastoreURIs...)
	existingWeb := map[string]struct{}{}
	for _, c := range base.WebSources {
		key := strings.TrimSpace(c.URL) + "|" + strings.TrimSpace(c.Title)
		if key != "|" {
			existingWeb[key] = struct{}{}
		}
	}
	for _, c := range add.WebSources {
		key := strings.TrimSpace(c.URL) + "|" + strings.TrimSpace(c.Title)
		if key == "|" {
			continue
		}
		if _, ok := existingWeb[key]; ok {
			continue
		}
		existingWeb[key] = struct{}{}
		base.WebSources = append(base.WebSources, c)
	}
	return base
}

func appendUniqueTrim(base []string, values ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(values))
	for _, v := range base {
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

func groundingOriginModeFromEvidence(internal, external bool) string {
	switch {
	case internal && external:
		return "mixed"
	case internal:
		return "internal_only"
	case external:
		return "external_only"
	default:
		return ""
	}
}

func buildProvenanceEvidence(e *ai.GroundingEvidence, sourceIDs, documentIDs []string) (*models.GroundingInternalEvidence, *models.GroundingExternalEvidence, string) {
	if e == nil {
		return nil, nil, ""
	}
	internal := &models.GroundingInternalEvidence{
		FragmentIDs:   appendUniqueTrim(nil, e.DatastoreFragmentIDs...),
		DatastoreURIs: appendUniqueTrim(nil, e.DatastoreURIs...),
		SourceIDs:     appendUniqueTrim(nil, sourceIDs...),
		DocumentIDs:   appendUniqueTrim(nil, documentIDs...),
	}
	if len(internal.FragmentIDs) == 0 && len(internal.DatastoreURIs) == 0 && len(internal.SourceIDs) == 0 && len(internal.DocumentIDs) == 0 {
		internal = nil
	}
	external := &models.GroundingExternalEvidence{
		WebURLs:   []string{},
		Citations: []string{},
	}
	for _, web := range e.WebSources {
		external.WebURLs = appendUniqueTrim(external.WebURLs, strings.TrimSpace(web.URL))
		if t := strings.TrimSpace(web.Title); t != "" {
			external.Citations = appendUniqueTrim(external.Citations, t)
		}
	}
	if len(external.WebURLs) == 0 && len(external.Citations) == 0 {
		external = nil
	}
	mode := groundingOriginModeFromEvidence(internal != nil, external != nil)
	return internal, external, mode
}

func applyGroundingEvidenceToJobState(job *models.ScoutingJob, evidence *ai.GroundingEvidence, sourceIDs, documentIDs []string) {
	if job == nil || evidence == nil {
		return
	}
	if job.State == nil {
		job.State = map[string]interface{}{}
	}
	internal, external, mode := buildProvenanceEvidence(evidence, sourceIDs, documentIDs)
	job.State[stateKeyGroundingContractVer] = groundingContractVersionV1
	if mode != "" {
		job.State[stateKeyOriginMode] = mode
	}
	if internal != nil {
		job.State[stateKeyInternalEvidence] = map[string]interface{}{
			"fragmentIds":   append([]string(nil), internal.FragmentIDs...),
			"datastoreUris": append([]string(nil), internal.DatastoreURIs...),
			"sourceIds":     append([]string(nil), internal.SourceIDs...),
			"documentIds":   append([]string(nil), internal.DocumentIDs...),
		}
		job.State[stateKeyGroundingDatastoreURIs] = append([]string(nil), internal.DatastoreURIs...)
	}
	if external != nil {
		job.State[stateKeyExternalEvidence] = map[string]interface{}{
			"webUrls":   append([]string(nil), external.WebURLs...),
			"citations": append([]string(nil), external.Citations...),
		}
	}
	webSources := make([]map[string]interface{}, 0, len(evidence.WebSources))
	for _, c := range evidence.WebSources {
		webSources = append(webSources, map[string]interface{}{
			"url":   strings.TrimSpace(c.URL),
			"title": strings.TrimSpace(c.Title),
		})
	}
	job.State[stateKeyGroundingEvidence] = map[string]interface{}{
		"datastoreFragmentIds": append([]string(nil), evidence.DatastoreFragmentIDs...),
		"datastoreUris":        append([]string(nil), evidence.DatastoreURIs...),
		"hasDatastoreResults":  evidence.HasDatastoreResults,
		"hasWebResults":        evidence.HasWebResults,
		"webSources":           webSources,
	}
}

func applyGroundingProvenance(
	p *models.Provenance,
	evidence *ai.GroundingEvidence,
	sourceIDs,
	documentIDs []string,
	resolutionStats *models.GroundingResolutionStats,
) {
	if p == nil || evidence == nil {
		return
	}
	internal, external, mode := buildProvenanceEvidence(evidence, sourceIDs, documentIDs)
	p.GroundingContractVersion = groundingContractVersionV1
	if mode != "" {
		p.OriginMode = mode
	}
	p.InternalEvidence = internal
	p.ExternalEvidence = external
	if resolutionStats != nil {
		p.ResolutionStats = resolutionStats
	}
}

func groundingEvidenceFromJobState(job *models.ScoutingJob) *ai.GroundingEvidence {
	if job == nil || job.State == nil {
		return nil
	}
	raw, ok := job.State[stateKeyGroundingEvidence]
	if !ok || raw == nil {
		return nil
	}
	m, ok := raw.(map[string]interface{})
	if !ok || m == nil {
		return nil
	}
	out := &ai.GroundingEvidence{
		DatastoreFragmentIDs: parseStringSlice(m["datastoreFragmentIds"]),
		DatastoreURIs:        parseStringSlice(m["datastoreUris"]),
		HasDatastoreResults:  parseBool(m["hasDatastoreResults"]),
		HasWebResults:        parseBool(m["hasWebResults"]),
	}
	if ws, ok := m["webSources"].([]interface{}); ok {
		for _, item := range ws {
			row, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			out.WebSources = append(out.WebSources, ai.WebCitation{
				URL:   strings.TrimSpace(parseString(row["url"])),
				Title: strings.TrimSpace(parseString(row["title"])),
			})
		}
	}
	if len(out.DatastoreFragmentIDs) == 0 && len(out.DatastoreURIs) == 0 && len(out.WebSources) == 0 && !out.HasDatastoreResults && !out.HasWebResults {
		return nil
	}
	return out
}

func parseStringSlice(v interface{}) []string {
	out := []string{}
	switch t := v.(type) {
	case []string:
		out = append(out, t...)
	case []interface{}:
		for _, it := range t {
			if s, ok := it.(string); ok {
				out = append(out, s)
			}
		}
	}
	return appendUniqueTrim(nil, out...)
}

func parseBool(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		tv := strings.TrimSpace(strings.ToLower(t))
		return tv == "1" || tv == "true" || tv == "yes" || tv == "on"
	default:
		return false
	}
}

func parseString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func parseInt(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float32:
		return int(t)
	case float64:
		return int(t)
	default:
		return 0
	}
}
