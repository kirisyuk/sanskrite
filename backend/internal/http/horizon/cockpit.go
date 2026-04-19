package horizon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	horizondomain "github.com/igorkirisyuk/sanskrite/internal/horizon/domain"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	horizonpropagation "github.com/igorkirisyuk/sanskrite/internal/horizon/propagation"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/scout"
	"github.com/igorkirisyuk/sanskrite/internal/horizon/trace"
	httputil "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/taxonomy/ah"
)

// GetCockpitData получает данные для Cockpit UI (граф, метрики, узлы)
func (h *HorizonHandlers) GetCockpitData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Извлекаем lensID из пути: /api/horizon/lenses/{lensId}/cockpit
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/")
	pathParts := strings.Split(path, "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}
	lensID := pathParts[0]

	// Получаем Lens
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}

	// Проверяем доступ
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Resolve scope to canonical paths (best-effort).
	// Cockpit loads root nodes strictly from lens.Scope, so any canonical path rewrite/retarget must be reflected here,
	// otherwise the node "disappears" even though it exists under a new canonicalPath.
	rawScope := append([]string(nil), lens.Scope...)
	scopePaths := make([]string, 0, len(rawScope)+8)
	seen := map[string]struct{}{}
	scopeArtifacts := make(map[string]models.ScopeArtifactRef)
	artifactPayloads := make([]*models.MaterializedArtifact, 0, 8)
	for _, s := range rawScope {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "/artifact/") && h.materializedArtifactsRepo != nil {
			artKey := strings.TrimPrefix(s, "/artifact/")
			artifact, aerr := h.materializedArtifactsRepo.GetByCanonicalKeyAnyKind(r.Context(), lens.TenantID, artKey)
			if aerr == nil && artifact != nil {
				models.PopulateSupplyChainPathIndexes(artifact)
				scopeArtifacts[artKey] = models.ScopeArtifactRef{
					Kind:             string(artifact.Kind),
					DisplayName:      artifact.DisplayName,
					AnchorSupplier:   artifact.AnchorSupplier,
					AnchorCustomer:   artifact.AnchorCustomer,
					ProductAxis:      artifact.ProductAxis,
					NodeRefs:         append([]string(nil), artifact.NodeRefs...),
					Status:           artifact.PathStatus,
					GeoRefs:          append([]string(nil), artifact.GeoRefs...),
					FlowRefs:         append([]string(nil), artifact.FlowRefs...),
					ZoneRefs:         append([]string(nil), artifact.ZoneRefs...),
					AffectedZoneRefs: append([]string(nil), artifact.AffectedZoneRefs...),
				}
				if parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload); err == nil && parsed != nil {
					summary := models.SummarizeSupplyChainPathBindings(parsed)
					ref := scopeArtifacts[artKey]
					ref.LegCount = summary.LegCount
					ref.BindingConfidence = summary.BindingConfidence
					ref.PathStressScore = summary.PathStressScore
					ref.BottleneckLegIndex = summary.BottleneckLegIndex
					ref.RecoveryLatencyHrs = summary.RecoveryLatencyHrs
					ref.SpatialSemantics = append([]string(nil), summary.SpatialSemantics...)
					scopeArtifacts[artKey] = ref
				}
				artifactPayloads = append(artifactPayloads, artifact)
				for _, ref := range artifact.NodeRefs {
					ref = strings.TrimSpace(ref)
					if ref == "" {
						continue
					}
					if _, ok := seen[ref]; ok {
						continue
					}
					seen[ref] = struct{}{}
					scopePaths = append(scopePaths, ref)
				}
				continue
			}
		}
		// Expand QueryAddress scope entries if cached.
		if strings.HasPrefix(s, "/query/") && lens.ResolvedQueries != nil {
			if resolved := lens.ResolvedQueries[s]; len(resolved) > 0 {
				for _, rp := range resolved {
					rp = strings.TrimSpace(rp)
					if rp == "" {
						continue
					}
					if _, ok := seen[rp]; ok {
						continue
					}
					seen[rp] = struct{}{}
					scopePaths = append(scopePaths, rp)
				}
				continue
			}
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		scopePaths = append(scopePaths, s)
	}

	resolvedScope := make([]string, 0, len(scopePaths))
	scopeRewrites := map[string]string{}
	if se := h.service.Scout(); se != nil {
		if resolver := se.CanonicalPathResolver(); resolver != nil {
			for _, p := range scopePaths {
				ft := scout.InferFragmentTypeFromCanonicalPath(p)
				if ft == "" {
					ft = ah.FragmentClassOrg
				}
				if rr, rerr := resolver.Resolve(r.Context(), p, ft); rerr == nil && rr != nil && strings.TrimSpace(rr.CanonicalPath) != "" {
					cp := strings.TrimSpace(rr.CanonicalPath)
					if cp != "" && cp != p {
						scopeRewrites[p] = cp
					}
					resolvedScope = append(resolvedScope, cp)
				} else {
					resolvedScope = append(resolvedScope, p)
				}
			}
		}
	}
	if len(resolvedScope) == 0 {
		resolvedScope = scopePaths
	}

	// De-dupe resolved scope while preserving order.
	seen2 := map[string]struct{}{}
	outScope := make([]string, 0, len(resolvedScope))
	for _, p := range resolvedScope {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen2[p]; ok {
			continue
		}
		seen2[p] = struct{}{}
		outScope = append(outScope, p)
	}
	scopeDomainPaths := map[string]struct{}{}
	for _, p := range outScope {
		if strings.HasPrefix(strings.TrimSpace(p), "/domain/") {
			scopeDomainPaths[strings.TrimSpace(p)] = struct{}{}
		}
	}

	// Получаем все HorizonFragment из scope (resolved)
	fragments, err := h.service.Fragments().ListByCanonicalPaths(r.Context(), outScope)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Root nodes = scope fragments (filtered). No hop+1 expansion: only scope entities.
	rootFragments := make([]*models.HorizonFragment, 0, len(fragments))
	rootIDs := make(map[string]struct{}, len(fragments))
	rootByID := make(map[string]*models.HorizonFragment, len(fragments))
	rootByCanonicalPath := make(map[string]*models.HorizonFragment, len(fragments))
	for _, fragment := range fragments {
		if fragment == nil || isSphereFragment(fragment) {
			continue
		}
		rootFragments = append(rootFragments, fragment)
		if fragment.ID != "" {
			rootIDs[fragment.ID] = struct{}{}
			rootByID[fragment.ID] = fragment
		}
		if cp := strings.TrimSpace(fragment.CanonicalPath); cp != "" {
			rootByCanonicalPath[cp] = fragment
		}
	}

	// Collect edges ONLY among scope nodes.
	edgesRepo := h.service.Edges()
	var allEdges []interface{}
	seenEdges := make(map[string]struct{}, 2048)
	skippedIncomingDomainEdges := 0

	addEdge := func(edge *models.DependencyEdge) {
		if edge == nil {
			return
		}
		if _, ok := rootIDs[edge.SourceFragmentID]; !ok {
			return
		}
		if _, ok := rootIDs[edge.TargetFragmentID]; !ok {
			return
		}
		if shouldSkipDomainIncomingEdgeForCockpit(rootByID[edge.SourceFragmentID], rootByID[edge.TargetFragmentID]) {
			skippedIncomingDomainEdges++
			return
		}
		k := edge.SourceFragmentID + "|" + edge.TargetFragmentID + "|" + string(edge.Relation)
		if _, ok := seenEdges[k]; ok {
			return
		}
		seenEdges[k] = struct{}{}
		allEdges = append(allEdges, edge)
	}

	// Pull edges for each root, but only keep those that stay within scope.
	for _, fragment := range rootFragments {
		if fragment == nil || fragment.ID == "" {
			continue
		}
		outgoingEdges, err := edgesRepo.ListBySource(r.Context(), "global", fragment.ID, nil, 200)
		if err == nil {
			for _, edge := range outgoingEdges {
				addEdge(edge)
			}
		}
		incomingEdges, err := edgesRepo.ListByTarget(r.Context(), "global", fragment.ID, nil, 200)
		if err == nil {
			for _, edge := range incomingEdges {
				addEdge(edge)
			}
		}
	}
	if skippedIncomingDomainEdges > 0 {
		trace.Log(r.Context(), "cockpit", "cockpit.edges.skip_incoming_domain", map[string]any{
			"tenantId": strings.TrimSpace(lens.TenantID),
			"lensId":   strings.TrimSpace(lens.ID),
			"count":    skippedIncomingDomainEdges,
		})
	}
	allEdges = addSyntheticArtifactEdges(allEdges, seenEdges, artifactPayloads, rootByCanonicalPath)

	// Вычисляем метрики Lens через MetricsCalculator
	// Получаем все фрагменты и вычисляем метрики
	metrics := make(map[string]interface{})
	if len(rootFragments) > 0 {
		// Простое вычисление метрик на основе фрагментов
		metricSums := make(map[string]float64)
		metricCounts := make(map[string]int)
		for _, fragment := range rootFragments {
			for metricName, metricValue := range fragment.State {
				metricSums[metricName] += metricValue.Value
				metricCounts[metricName]++
			}
		}
		for metricName, count := range metricCounts {
			if count > 0 {
				metrics[metricName] = metricSums[metricName] / float64(count)
			}
		}
		metrics["totalFragments"] = len(rootFragments)
	}

	// If scope rewrites happened, log once (best-effort) for debugging.
	if len(scopeRewrites) > 0 {
		sample := make([]string, 0, 12)
		i := 0
		for k, v := range scopeRewrites {
			if i >= 12 {
				break
			}
			sample = append(sample, strings.TrimSpace(k)+" -> "+strings.TrimSpace(v))
			i++
		}
		trace.Log(r.Context(), "cockpit", "cockpit.scope.rewritten", map[string]any{
			"tenantId":    strings.TrimSpace(lens.TenantID),
			"lensId":      strings.TrimSpace(lens.ID),
			"rawScopeN":   len(rawScope),
			"resolvedN":   len(outScope),
			"rewritesN":   len(scopeRewrites),
			"rewritesTop": sample,
		})
	}

	// Формируем ответ для Cockpit UI
	lensPayload := map[string]interface{}{
		"id":   lens.ID,
		"name": lens.Name,
		// For Cockpit, scope is the resolved (canonical) scope.
		"scope": outScope,
		// Keep raw scope for debugging/traceability (optional; UI may ignore it).
		"scopeRaw":       rawScope,
		"scopeExpanded":  outScope,
		"scopeArtifacts": scopeArtifacts,
		"metrics":        metrics,
	}
	if len(lens.ViewConfig) > 0 {
		lensPayload["viewConfig"] = lens.ViewConfig
	}
	response := map[string]interface{}{
		"lens": lensPayload,
		"graph": map[string]interface{}{
			"nodes": rootFragments,
			"edges": allEdges,
		},
		"summary": map[string]interface{}{
			"totalFragments": len(rootFragments),
			"totalEdges":     len(allEdges),
			"metrics":        metrics,
		},
		"domainAnswers": []interface{}{},
	}
	if len(scopeDomainPaths) > 0 {
		if domainAnswersRepo := h.service.DomainAnswerSnapshots(); domainAnswersRepo != nil {
			if snapshots, err := domainAnswersRepo.ListByLens(r.Context(), lensID, 50); err == nil && len(snapshots) > 0 {
				out := make([]interface{}, 0, len(snapshots))
				for _, s := range snapshots {
					if s == nil {
						continue
					}
					pathCandidates := []string{
						strings.TrimSpace(s.DomainCanonicalPath),
						strings.TrimSpace(s.DomainID),
						strings.TrimSpace(s.DomainContract.CanonicalPath),
					}
					matched := false
					for _, cp := range pathCandidates {
						if cp == "" {
							continue
						}
						if _, ok := scopeDomainPaths[cp]; ok {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
					out = append(out, s)
				}
				response["domainAnswers"] = out
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func addSyntheticArtifactEdges(
	allEdges []interface{},
	seenEdges map[string]struct{},
	artifacts []*models.MaterializedArtifact,
	rootByCanonicalPath map[string]*models.HorizonFragment,
) []interface{} {
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Kind != models.MaterializedKindSupplyChainPath {
			continue
		}
		parsed, err := models.ParseSupplyChainPathPayload(artifact.Payload)
		if err != nil || parsed == nil {
			continue
		}
		for _, edge := range parsed.Edges {
			if edge.From < 0 || edge.From >= len(parsed.Nodes) || edge.To < 0 || edge.To >= len(parsed.Nodes) {
				continue
			}
			sourceFrag := rootByCanonicalPath[strings.TrimSpace(parsed.Nodes[edge.From].Ref)]
			targetFrag := rootByCanonicalPath[strings.TrimSpace(parsed.Nodes[edge.To].Ref)]
			if sourceFrag == nil || targetFrag == nil || sourceFrag.ID == "" || targetFrag.ID == "" {
				continue
			}
			key := sourceFrag.ID + "|" + targetFrag.ID + "|" + strings.TrimSpace(edge.Type) + "|artifact:" + strings.TrimSpace(artifact.CanonicalKey)
			if _, ok := seenEdges[key]; ok {
				continue
			}
			seenEdges[key] = struct{}{}
			allEdges = append(allEdges, map[string]interface{}{
				"id":                      fmt.Sprintf("artifact:%s:%d:%s", artifact.ID, derefInt(edge.SegmentIndex), strings.TrimSpace(edge.Type)),
				"sourceFragmentId":        sourceFrag.ID,
				"targetFragmentId":        targetFrag.ID,
				"type":                    edge.Type,
				"materialized":            true,
				"artifactCanonicalKey":    strings.TrimSpace(artifact.CanonicalKey),
				"artifactId":              strings.TrimSpace(artifact.ID),
				"pathRole":                strings.TrimSpace(edge.Role),
				"segmentIndex":            edge.SegmentIndex,
				"supplyLinkCanonicalPath": strings.TrimSpace(edge.SupplyLinkCanonicalPath),
				"supplyLinkId":            strings.TrimSpace(edge.SupplyLinkID),
				"qualifiers":              edge.Qualifiers,
				"properties": map[string]interface{}{
					"criticality":   firstMetricValue(edge.Metrics, "confidence"),
					"amplification": 1,
				},
			})
		}
	}
	return allEdges
}

func derefInt(v *int) int {
	if v == nil {
		return -1
	}
	return *v
}

func firstMetricValue(metrics map[string]models.MetricValue, key string) float64 {
	if len(metrics) == 0 {
		return 0
	}
	if metric, ok := metrics[key]; ok {
		return metric.Value
	}
	for _, metric := range metrics {
		return metric.Value
	}
	return 0
}

func isSphereFragment(fragment *models.HorizonFragment) bool {
	if fragment == nil {
		return false
	}
	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	return strings.HasPrefix(cp, "/sphere/")
}

func isDomainFragment(fragment *models.HorizonFragment) bool {
	if fragment == nil {
		return false
	}
	cp := strings.ToLower(strings.TrimSpace(fragment.CanonicalPath))
	return strings.HasPrefix(cp, "/domain/")
}

func shouldSkipDomainIncomingEdgeForCockpit(source, target *models.HorizonFragment) bool {
	return isDomainFragment(target) && !isDomainFragment(source)
}

// SimulateWhatIf выполняет What-If симуляцию изменения метрик
func (h *HorizonHandlers) SimulateWhatIf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authInfo := httputil.GetAuthInfo(r.Context())
	if authInfo == nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Извлекаем lensID из пути: /api/horizon/lenses/{lensId}/simulate
	path := strings.TrimPrefix(r.URL.Path, "/api/horizon/lenses/")
	pathParts := strings.Split(path, "/")
	if len(pathParts) == 0 || pathParts[0] == "" {
		http.Error(w, "lensId required in path", http.StatusBadRequest)
		return
	}
	lensID := pathParts[0]

	var req struct {
		FragmentID   string                 `json:"fragmentId"`
		MetricName   string                 `json:"metricName"`
		NewValue     map[string]interface{} `json:"newValue"` // MetricValue структура
		ContractType string                 `json:"contractType,omitempty"`
		DomainModes  []string               `json:"domainModes,omitempty"`
		EngineID     string                 `json:"engineId,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Получаем Lens
	lens, err := h.service.Lens().GetLens(r.Context(), lensID)
	if err != nil {
		http.Error(w, "Lens not found", http.StatusNotFound)
		return
	}

	// Проверяем доступ
	if lens.TenantID != authInfo.TenantID {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Получаем Propagation Engine
	propagationEngine := h.service.Propagation()
	if propagationEngine == nil {
		http.Error(w, "Propagation engine not available", http.StatusInternalServerError)
		return
	}

	// Получаем текущий фрагмент
	fragment, err := h.service.Fragments().Get(r.Context(), req.FragmentID)
	if err != nil {
		http.Error(w, "Fragment not found", http.StatusNotFound)
		return
	}

	// Получаем старое значение метрики
	oldMetricValue, exists := fragment.State[req.MetricName]
	if !exists {
		http.Error(w, "Metric not found", http.StatusBadRequest)
		return
	}

	// Создаем новое значение метрики из запроса
	// TODO: Преобразовать req.NewValue в models.MetricValue
	// Временная реализация: используем старое значение с новым value
	newMetricValue := oldMetricValue
	if value, ok := req.NewValue["value"].(float64); ok {
		newMetricValue.Value = value
	}

	// Выполняем симуляцию распространения в read-only режиме
	binding := horizonpropagation.DomainAdapterBinding{
		ContractType: strings.TrimSpace(req.ContractType),
		DomainModes:  append([]string(nil), req.DomainModes...),
		EngineID:     strings.TrimSpace(req.EngineID),
	}
	if binding.ContractType == "" {
		contractTypes := contractTypesFromScope(lens.Scope)
		if len(contractTypes) > 0 {
			binding.ContractType = strings.TrimSpace(contractTypes[0])
			if len(binding.DomainModes) == 0 {
				binding.DomainModes = horizondomain.DomainModesForContractTypes(contractTypes)
			}
		}
	}
	if binding.ContractType != "" && len(binding.DomainModes) == 0 {
		binding.DomainModes = horizondomain.DomainModesForContractTypes([]string{binding.ContractType})
	}
	ctxWithBinding := horizonpropagation.WithDomainAdapterBinding(r.Context(), binding)
	propagationResult, err := propagationEngine.SimulatePropagation(ctxWithBinding, req.FragmentID, req.MetricName, oldMetricValue, newMetricValue)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Формируем ответ
	response := map[string]interface{}{
		"simulationId":      "sim-" + req.FragmentID + "-" + req.MetricName,
		"affectedFragments": propagationResult.AffectedFragments,
		"affectedLenses":    propagationResult.AffectedLenses,
		"propagationTime":   propagationResult.PropagationTime.String(),
		"metrics":           propagationResult.Metrics,
		"contractType":      binding.ContractType,
		"domainModes":       binding.DomainModes,
		"engineId":          binding.EngineID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func contractTypesFromScope(scope []string) []string {
	seen := make(map[string]struct{}, len(scope))
	out := make([]string, 0, len(scope))
	for _, entry := range scope {
		ct := contractTypeFromDomainPath(entry)
		if ct == "" {
			continue
		}
		if _, ok := seen[ct]; ok {
			continue
		}
		seen[ct] = struct{}{}
		out = append(out, ct)
	}
	return out
}

func contractTypeFromDomainPath(path string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	if parts[0] != "domain" || parts[1] != "v1" {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(parts[2]))
}
