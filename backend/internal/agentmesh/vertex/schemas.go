package vertex

import "strings"

func responseSchemaForCapability(capability string, review bool) map[string]any {
	if review {
		return reviewResponseSchema()
	}
	switch strings.TrimSpace(strings.ToLower(capability)) {
	case "scout.plan":
		return planResponseSchema()
	case "scout.orchestrate", "scout.decide":
		return decisionResponseSchema()
	case "scout.research":
		return researchResponseSchema()
	case "scout.bridge":
		return bridgeResponseSchema()
	case "scout.critic":
		return criticResponseSchema()
	case "scout.materialize":
		return materializeResponseSchema()
	case "scout.aggregate":
		return aggregateResponseSchema()
	case "scout.review":
		return reviewResponseSchema()
	default:
		return nil
	}
}

func objectSchema(required []string, properties map[string]any) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	order := make([]string, 0, len(properties))
	for key := range properties {
		order = append(order, key)
	}
	if len(order) > 0 {
		schema["propertyOrdering"] = order
	}
	return schema
}

func stringSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func boolSchema() map[string]any {
	return map[string]any{"type": "boolean"}
}

func numberSchema() map[string]any {
	return map[string]any{"type": "number"}
}

func stringArraySchema() map[string]any {
	return map[string]any{"type": "array", "items": stringSchema()}
}

func objectArraySchema(properties map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": objectSchema(nil, properties)}
}

func anyObjectSchema() map[string]any {
	return map[string]any{"type": "object"}
}

func planResponseSchema() map[string]any {
	branch := map[string]any{
		"id":               stringSchema(),
		"question":         stringSchema(),
		"role":             stringSchema(),
		"productHints":     stringArraySchema(),
		"geoHints":         stringArraySchema(),
		"actorHints":       stringArraySchema(),
		"requiresPath":     boolSchema(),
		"dependsOn":        stringArraySchema(),
		"broadScan":        boolSchema(),
		"testCriteria":     stringArraySchema(),
		"expectedInfoGain": stringSchema(),
	}
	role := map[string]any{
		"domain":                 stringSchema(),
		"primaryRole":            stringSchema(),
		"supportingRoles":        stringArraySchema(),
		"professionalQualityBar": stringSchema(),
		"investigationPosture":   stringSchema(),
		"roleRationale":          stringSchema(),
	}
	return objectSchema([]string{"goal", "branches"}, map[string]any{
		"goal":                    stringSchema(),
		"missionRole":             objectSchema(nil, role),
		"focusProducts":           stringArraySchema(),
		"focusGeos":               stringArraySchema(),
		"focusActors":             stringArraySchema(),
		"wantsPathMaterialize":    boolSchema(),
		"maxResearchRounds":       numberSchema(),
		"planningRationale":       stringSchema(),
		"existingGraphPriorities": stringArraySchema(),
		"planVersion":             numberSchema(),
		"materializationStrategy": stringSchema(),
		"branchDependencies":      anyObjectSchema(),
		"branches":                objectArraySchema(branch),
	})
}

func decisionResponseSchema() map[string]any {
	stopCriteria := map[string]any{
		"criterion": stringSchema(),
		"satisfied": boolSchema(),
		"why":       stringSchema(),
	}
	return objectSchema([]string{"chosenAction", "confidence"}, map[string]any{
		"stateSummary":            stringSchema(),
		"chosenAction":            map[string]any{"type": "string", "enum": []string{"research", "bridge", "path", "critic", "materialize", "replan", "stop"}},
		"branchId":                stringSchema(),
		"whyChosen":               stringSchema(),
		"whyNotStop":              stringSchema(),
		"whyNotPath":              stringSchema(),
		"whyNotBridge":            stringSchema(),
		"whyNotResearch":          stringSchema(),
		"whyNotCritic":            stringSchema(),
		"whyNotMaterialize":       stringSchema(),
		"whyNotReplan":            stringSchema(),
		"evidenceUsed":            stringArraySchema(),
		"stopCriteriaEvaluation":  objectArraySchema(stopCriteria),
		"expectedInformationGain": stringSchema(),
		"confidence":              numberSchema(),
	})
}

func researchResponseSchema() map[string]any {
	return objectSchema([]string{"summary", "confidence"}, map[string]any{
		"summary":                 stringSchema(),
		"evidencePoints":          objectArraySchema(map[string]any{"claim": stringSchema(), "source": stringSchema(), "confidence": numberSchema()}),
		"canonicalRefs":           stringArraySchema(),
		"productRefs":             stringArraySchema(),
		"geoRefs":                 stringArraySchema(),
		"actorRefs":               stringArraySchema(),
		"recommendedPathProducts": stringArraySchema(),
		"openQuestions":           stringArraySchema(),
		"confidence":              numberSchema(),
		"proposedGraphOps":        objectArraySchema(map[string]any{"op": stringSchema(), "reason": stringSchema(), "confidence": numberSchema()}),
		"gapDiagnosis":            anyObjectSchema(),
		"recommendedAction":       anyObjectSchema(),
	})
}

func bridgeResponseSchema() map[string]any {
	return objectSchema([]string{"summary", "bridgeExists", "confidence"}, map[string]any{
		"branchId":                  stringSchema(),
		"endpointA":                 stringSchema(),
		"endpointB":                 stringSchema(),
		"bridgeQuestion":            stringSchema(),
		"summary":                   stringSchema(),
		"bridgeExists":              boolSchema(),
		"bridgeKind":                stringSchema(),
		"missingMiddleTypes":        stringArraySchema(),
		"intermediateNodes":         stringArraySchema(),
		"intermediateRelationTypes": stringArraySchema(),
		"bridgeSteps":               objectArraySchema(map[string]any{"index": numberSchema(), "stepClass": stringSchema(), "whyNeeded": stringSchema()}),
		"routeSketches":             stringArraySchema(),
		"openQuestions":             stringArraySchema(),
		"confidence":                numberSchema(),
		"proposedGraphOps":          objectArraySchema(map[string]any{"op": stringSchema(), "reason": stringSchema(), "confidence": numberSchema()}),
		"suggestedSubscans":         stringArraySchema(),
		"gapDiagnosis":              anyObjectSchema(),
		"recommendedAction":         anyObjectSchema(),
	})
}

func criticResponseSchema() map[string]any {
	return objectSchema([]string{"confidence"}, map[string]any{
		"acceptedFindings":        stringArraySchema(),
		"riskyClaims":             stringArraySchema(),
		"openQuestions":           stringArraySchema(),
		"materializePath":         boolSchema(),
		"pathProducts":            stringArraySchema(),
		"focusActors":             stringArraySchema(),
		"focusGeos":               stringArraySchema(),
		"confidence":              numberSchema(),
		"withholdPath":            boolSchema(),
		"withholdMaterialization": boolSchema(),
		"forceReplan":             boolSchema(),
		"killBranchIds":           stringArraySchema(),
		"newBranches":             objectArraySchema(map[string]any{"question": stringSchema(), "reason": stringSchema()}),
		"reprioritizeBranchIds":   stringArraySchema(),
		"riskTargetBranchIds":     stringArraySchema(),
		"graphOpVerdicts":         objectArraySchema(map[string]any{"opIndex": numberSchema(), "verdict": stringSchema(), "reason": stringSchema()}),
		"branchAssessments":       objectArraySchema(map[string]any{"branchId": stringSchema(), "pursuitDecision": stringSchema(), "pursuitReason": stringSchema()}),
		"completenessAssessment":  anyObjectSchema(),
		"goodEnoughAssessment":    anyObjectSchema(),
		"gapDiagnosis":            anyObjectSchema(),
		"recommendedAction":       anyObjectSchema(),
	})
}

func materializeResponseSchema() map[string]any {
	return objectSchema([]string{"summary"}, map[string]any{
		"summary":           stringSchema(),
		"skipped":           boolSchema(),
		"downgraded":        boolSchema(),
		"rejected":          boolSchema(),
		"landedBranchIds":   stringArraySchema(),
		"rejectedBranchIds": stringArraySchema(),
		"corrections":       objectArraySchema(map[string]any{"action": stringSchema(), "artifactId": stringSchema(), "rationale": stringSchema()}),
	})
}

func aggregateResponseSchema() map[string]any {
	return objectSchema([]string{"summary"}, map[string]any{
		"summary":             stringSchema(),
		"findings":            stringArraySchema(),
		"openQuestions":       stringArraySchema(),
		"artifactIds":         stringArraySchema(),
		"candidateProducts":   stringArraySchema(),
		"recommendedNextStep": stringSchema(),
	})
}

func reviewResponseSchema() map[string]any {
	return objectSchema([]string{"accepted", "rationale"}, map[string]any{
		"accepted":  boolSchema(),
		"rationale": stringSchema(),
		"quality":   stringSchema(),
		"issues":    stringArraySchema(),
	})
}
