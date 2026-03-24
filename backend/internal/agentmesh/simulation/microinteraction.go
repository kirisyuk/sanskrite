package simulation

import (
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

type counterpartyCandidate struct {
	agent  *horizonmodels.SimulationAgentSpec
	score  float64
	reason string
}

type microStepSummary struct {
	messageCount       int
	protocolCount      int
	transactionChanges int
	stateDelta         metricState
}

func processProtocolQueues(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, step int) microStepSummary {
	if world == nil {
		return microStepSummary{}
	}
	mailboxes := ensureWorldMailboxes(world, agents)
	summary := microStepSummary{}
	summary = mergeMicroStepSummary(summary, processInboxResponses(run, world, agents, mailboxes, step))
	summary = mergeMicroStepSummary(summary, processReviewQueue(run, world, agents, step))
	return summary
}

func applyMicrointeractionLayer(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, actor *horizonmodels.SimulationAgentSpec, action horizonmodels.SimulationAction, step int, interactionEffects []scenarioInteractionEffect) microStepSummary {
	if world == nil || actor == nil {
		return microStepSummary{}
	}
	mailboxes := ensureWorldMailboxes(world, agents)
	counterparties := selectCounterparties(run, world, agents, actor, action, interactionEffects)
	if len(counterparties) == 0 {
		return microStepSummary{}
	}
	summary := microStepSummary{}
	for _, target := range counterparties {
		if target == nil || strings.TrimSpace(target.ID) == "" || strings.TrimSpace(target.ID) == strings.TrimSpace(actor.ID) {
			continue
		}
		message := buildProtocolMessage(run, world, actor, target, action, step)
		var transactionID string
		if transaction, ok := upsertTransaction(run, world, actor, target, action, step); ok {
			transactionID = stringValue(transaction, "id")
			message["transactionId"] = transactionID
		}
		appendMailboxMessage(mailboxes, actor.ID, "outbox", message)
		appendMailboxMessage(mailboxes, target.ID, "inbox", message)
		world.ProtocolTraces = append(world.ProtocolTraces, message)
		summary.messageCount++
		summary.protocolCount++

		updateRelationshipState(world, actor.ID, target.ID, 0.01, 0.0, message["messageType"], "sent")
		if transactionID != "" {
			transaction := findTransaction(world, transactionID)
			if transaction == nil {
				continue
			}
			world.ProtocolTraces = append(world.ProtocolTraces, map[string]any{
				"step":            step,
				"kind":            "transaction_event",
				"protocolType":    transaction["protocolType"],
				"transactionId":   transaction["id"],
				"transactionType": transaction["transactionType"],
				"fromAgentId":     transaction["fromAgentId"],
				"toAgentId":       transaction["toAgentId"],
				"status":          transaction["status"],
				"actionType":      action.Type,
				"queueStatus":     transaction["queueStatus"],
			})
			summary.protocolCount++
			summary.transactionChanges++
		}
	}
	return summary
}

func advanceWorldTransactions(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, step int) microStepSummary {
	if world == nil || len(world.Transactions) == 0 {
		return microStepSummary{}
	}
	byID := map[string]*horizonmodels.SimulationAgentSpec{}
	for _, agent := range agents {
		if agent != nil && strings.TrimSpace(agent.ID) != "" {
			byID[strings.TrimSpace(agent.ID)] = agent
		}
	}
	summary := microStepSummary{}
	for _, tx := range world.Transactions {
		status := strings.TrimSpace(stringValue(tx, "status"))
		if isTerminalTransactionStatus(status) {
			continue
		}
		fromID := strings.TrimSpace(stringValue(tx, "fromAgentId"))
		toID := strings.TrimSpace(stringValue(tx, "toAgentId"))
		profile := relationshipProfile(world, fromID, toID)
		counterparty := byID[toID]
		counterpartyStress := 0.45
		counterpartyBudget := 0.5
		if counterparty != nil {
			counterpartyStress = mapFloat(counterparty.BeliefState, "stress", 0.45)
			counterpartyBudget = mapFloat(counterparty.ResourceState, "budget", 0.5)
		}
		nextStatus, riskDelta, priceDelta, supplyDelta := nextTransactionStatus(tx, profile, counterpartyStress, counterpartyBudget, step)
		if nextStatus == status {
			continue
		}
		tx["previousStatus"] = status
		tx["status"] = nextStatus
		tx["updatedStep"] = step
		tx["statusHistory"] = appendStatusHistory(tx["statusHistory"], step, nextStatus)
		world.ProtocolTraces = append(world.ProtocolTraces, map[string]any{
			"step":            step,
			"kind":            "transaction_status",
			"protocolType":    stringValue(tx, "protocolType"),
			"transactionId":   stringValue(tx, "id"),
			"transactionType": stringValue(tx, "transactionType"),
			"fromAgentId":     fromID,
			"toAgentId":       toID,
			"status":          nextStatus,
		})
		summary.protocolCount++
		summary.transactionChanges++
		summary.stateDelta.risk += riskDelta
		summary.stateDelta.price += priceDelta
		summary.stateDelta.supply += supplyDelta

		switch nextStatus {
		case "approved", "in_execution", "fulfilled", "reserved", "rerouted", "cleared":
			updateRelationshipState(world, fromID, toID, 0.02, 0.03, nextStatus, "advanced")
		case "delayed":
			updateRelationshipState(world, fromID, toID, -0.02, -0.01, nextStatus, "delayed")
		case "rejected", "blocked":
			updateRelationshipState(world, fromID, toID, -0.06, -0.04, nextStatus, "rejected")
		}
	}
	summary = mergeMicroStepSummary(summary, transactionOperationalLoad(world, step))
	return summary
}

func ensureWorldMailboxes(world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec) map[string]any {
	if world.AgentState == nil {
		world.AgentState = map[string]any{}
	}
	raw, _ := world.AgentState["mailboxes"].(map[string]any)
	if raw == nil {
		raw = map[string]any{}
		world.AgentState["mailboxes"] = raw
	}
	for _, agent := range agents {
		if agent == nil || strings.TrimSpace(agent.ID) == "" {
			continue
		}
		if _, ok := raw[agent.ID].(map[string]any); !ok {
			raw[agent.ID] = map[string]any{
				"inbox":  []map[string]any{},
				"outbox": []map[string]any{},
			}
		}
	}
	return raw
}

func ensureWorldQueues(world *horizonmodels.SimulationWorld) map[string]any {
	if world.AgentState == nil {
		world.AgentState = map[string]any{}
	}
	raw, _ := world.AgentState["queues"].(map[string]any)
	if raw == nil {
		raw = map[string]any{
			"review": []map[string]any{},
		}
		world.AgentState["queues"] = raw
	}
	if _, ok := raw["review"].([]map[string]any); !ok {
		raw["review"] = []map[string]any{}
	}
	return raw
}

func appendMailboxMessage(mailboxes map[string]any, agentID, box string, message map[string]any) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" || len(mailboxes) == 0 {
		return
	}
	boxes, _ := mailboxes[agentID].(map[string]any)
	if boxes == nil {
		boxes = map[string]any{}
		mailboxes[agentID] = boxes
	}
	items, _ := boxes[box].([]map[string]any)
	items = append(items, cloneMap(message))
	boxes[box] = items
}

func processInboxResponses(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, mailboxes map[string]any, step int) microStepSummary {
	if len(mailboxes) == 0 {
		return microStepSummary{}
	}
	byID := map[string]*horizonmodels.SimulationAgentSpec{}
	for _, agent := range agents {
		if agent != nil && strings.TrimSpace(agent.ID) != "" {
			byID[strings.TrimSpace(agent.ID)] = agent
		}
	}
	summary := microStepSummary{}
	for agentID, raw := range mailboxes {
		boxes, _ := raw.(map[string]any)
		if boxes == nil {
			continue
		}
		inbox, _ := boxes["inbox"].([]map[string]any)
		if len(inbox) == 0 {
			continue
		}
		agent := byID[strings.TrimSpace(agentID)]
		for _, message := range inbox {
			if int(floatValue(message, "deliveredStep", -1)) > step {
				continue
			}
			if boolValue(message, "responded", false) {
				continue
			}
			responseType, stateDelta, queueReview := protocolResponseDecision(world, agent, message, step)
			message["responded"] = true
			message["responseType"] = responseType
			message["responseStep"] = step
			world.ProtocolTraces = append(world.ProtocolTraces, map[string]any{
				"step":          step,
				"kind":          "protocol_response",
				"protocolType":  stringValue(message, "protocolType"),
				"messageType":   stringValue(message, "messageType"),
				"responseType":  responseType,
				"fromAgentId":   stringValue(message, "fromAgentId"),
				"toAgentId":     stringValue(message, "toAgentId"),
				"transactionId": message["transactionId"],
			})
			summary.protocolCount++
			summary.messageCount++
			summary.stateDelta.risk += stateDelta.risk
			summary.stateDelta.price += stateDelta.price
			summary.stateDelta.narrative += stateDelta.narrative
			summary.stateDelta.policy += stateDelta.policy
			summary.stateDelta.supply += stateDelta.supply

			fromID := stringValue(message, "fromAgentId")
			toID := stringValue(message, "toAgentId")
			switch responseType {
			case "accept", "align", "proceed":
				updateRelationshipState(world, toID, fromID, 0.02, 0.03, message["protocolType"], responseType)
			case "review", "counter":
				updateRelationshipState(world, toID, fromID, -0.005, 0, message["protocolType"], responseType)
			case "delay":
				updateRelationshipState(world, toID, fromID, -0.02, -0.01, message["protocolType"], responseType)
			case "reject":
				updateRelationshipState(world, toID, fromID, -0.05, -0.03, message["protocolType"], responseType)
			}
			if queueReview {
				queueReviewItem(world, message, agent, step)
			}
			if txID := stringValue(message, "transactionId"); txID != "" {
				if tx := findTransaction(world, txID); tx != nil {
					applyMessageResponseToTransaction(tx, responseType, step)
					summary.transactionChanges++
				}
			}
		}
	}
	return summary
}

func processReviewQueue(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, step int) microStepSummary {
	queues := ensureWorldQueues(world)
	items, _ := queues["review"].([]map[string]any)
	if len(items) == 0 {
		return microStepSummary{}
	}
	byID := map[string]*horizonmodels.SimulationAgentSpec{}
	for _, agent := range agents {
		if agent != nil && strings.TrimSpace(agent.ID) != "" {
			byID[strings.TrimSpace(agent.ID)] = agent
		}
	}
	summary := microStepSummary{}
	for _, item := range items {
		if boolValue(item, "resolved", false) {
			continue
		}
		if int(floatValue(item, "eligibleStep", 0)) > step {
			continue
		}
		reviewer := byID[strings.TrimSpace(stringValue(item, "reviewerAgentId"))]
		decision, stateDelta := reviewDecision(world, reviewer, item, step)
		item["resolved"] = true
		item["decision"] = decision
		item["decisionStep"] = step
		world.ProtocolTraces = append(world.ProtocolTraces, map[string]any{
			"step":          step,
			"kind":          "review_decision",
			"protocolType":  stringValue(item, "protocolType"),
			"transactionId": stringValue(item, "transactionId"),
			"reviewerId":    stringValue(item, "reviewerAgentId"),
			"decision":      decision,
		})
		summary.protocolCount++
		summary.stateDelta.risk += stateDelta.risk
		summary.stateDelta.price += stateDelta.price
		summary.stateDelta.narrative += stateDelta.narrative
		summary.stateDelta.policy += stateDelta.policy
		summary.stateDelta.supply += stateDelta.supply
		if tx := findTransaction(world, stringValue(item, "transactionId")); tx != nil {
			applyReviewDecisionToTransaction(tx, decision, step)
			summary.transactionChanges++
		}
	}
	return summary
}

func selectCounterparties(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, agents []*horizonmodels.SimulationAgentSpec, actor *horizonmodels.SimulationAgentSpec, action horizonmodels.SimulationAction, interactionEffects []scenarioInteractionEffect) []*horizonmodels.SimulationAgentSpec {
	if actor == nil {
		return nil
	}
	directIDs := map[string]struct{}{}
	for _, effect := range interactionEffects {
		for _, targetID := range effect.TargetAgentIDs {
			if targetID = strings.TrimSpace(targetID); targetID != "" {
				directIDs[targetID] = struct{}{}
			}
		}
	}
	preferredClasses := preferredCounterpartyClasses(action.Type)
	candidates := make([]counterpartyCandidate, 0, len(agents))
	for _, target := range agents {
		if target == nil || strings.TrimSpace(target.ID) == "" || strings.TrimSpace(target.ID) == strings.TrimSpace(actor.ID) {
			continue
		}
		score := 0.0
		reason := []string{}
		if _, ok := directIDs[target.ID]; ok {
			score += 4.0
			reason = append(reason, "direct_rule")
		}
		if overlapsActionTargetRefs(action.TargetRefs, target.OriginRefs) {
			score += 2.2
			reason = append(reason, "target_ref_match")
		}
		if matchesPreferredCounterparty(target.AgentClass, preferredClasses) {
			score += 1.4
			reason = append(reason, "class_match")
		}
		if overlapsActionTargetRefs(actor.OriginRefs, target.OriginRefs) {
			score += 0.6
			reason = append(reason, "origin_overlap")
		}
		profile := relationshipProfile(world, actor.ID, target.ID)
		score += 0.9 * profile["trust"]
		score += 0.4 * profile["reliability"]
		if score <= 0.1 {
			continue
		}
		candidates = append(candidates, counterpartyCandidate{
			agent:  target,
			score:  score,
			reason: strings.Join(reason, ","),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return strings.TrimSpace(candidates[i].agent.ID) < strings.TrimSpace(candidates[j].agent.ID)
		}
		return candidates[i].score > candidates[j].score
	})
	out := make([]*horizonmodels.SimulationAgentSpec, 0, min(len(candidates), 2))
	for _, candidate := range candidates[:min(len(candidates), 2)] {
		out = append(out, candidate.agent)
	}
	return out
}

func buildProtocolMessage(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, actor, target *horizonmodels.SimulationAgentSpec, action horizonmodels.SimulationAction, step int) map[string]any {
	protocolType, messageType := protocolForAction(action.Type)
	return map[string]any{
		"id":             fmt.Sprintf("%s:%s:%s:%d", messageType, actor.ID, target.ID, step),
		"step":           step,
		"kind":           "direct_message",
		"protocolType":   protocolType,
		"messageType":    messageType,
		"fromAgentId":    actor.ID,
		"fromClass":      actor.AgentClass,
		"toAgentId":      target.ID,
		"toClass":        target.AgentClass,
		"actionType":     action.Type,
		"targetRefs":     compactStrings(action.TargetRefs),
		"status":         "sent",
		"deliveredStep":  step + protocolLatency(protocolType, actor, target),
		"requiresReview": requiresReview(action.Type),
		"payload":        cloneMap(action.Payload),
		"justification":  strings.TrimSpace(action.Justification),
		"axis":           runAxis(run),
		"worldIndex":     world.WorldIndex,
	}
}

func upsertTransaction(run *horizonmodels.SimulationRun, world *horizonmodels.SimulationWorld, actor, target *horizonmodels.SimulationAgentSpec, action horizonmodels.SimulationAction, step int) (map[string]any, bool) {
	transactionType, protocolType, initialStatus := transactionForAction(action.Type)
	if transactionType == "" {
		return nil, false
	}
	var existing map[string]any
	for _, tx := range world.Transactions {
		if stringValue(tx, "transactionType") != transactionType {
			continue
		}
		if stringValue(tx, "fromAgentId") != actor.ID || stringValue(tx, "toAgentId") != target.ID {
			continue
		}
		if isTerminalTransactionStatus(stringValue(tx, "status")) {
			continue
		}
		existing = tx
		break
	}
	if existing == nil {
		existing = map[string]any{
			"id":              fmt.Sprintf("%s:%s:%s", transactionType, actor.ID, target.ID),
			"transactionType": transactionType,
			"protocolType":    protocolType,
			"fromAgentId":     actor.ID,
			"fromClass":       actor.AgentClass,
			"toAgentId":       target.ID,
			"toClass":         target.AgentClass,
			"status":          initialStatus,
			"openedStep":      step,
			"updatedStep":     step,
			"targetRefs":      compactStrings(action.TargetRefs),
			"subjectRefs":     transactionSubjectRefs(actor, target, action),
			"amountScore":     round3(clamp01(numericPayload(action.Payload, "delta", 0.25)*2.4 + actor.PopulationWeight*0.15)),
			"frictionScore":   round3(clamp01(0.30 + mapFloat(actor.BeliefState, "stress", 0.4)*0.4 + (1-mapFloat(actor.ResourceState, "budget", 0.5))*0.3)),
			"requiresReview":  requiresReview(action.Type),
			"queueStatus":     ternary(requiresReview(action.Type), "review_pending", "direct"),
			"statusHistory":   []map[string]any{{"step": step, "status": initialStatus}},
		}
		world.Transactions = append(world.Transactions, existing)
		if requiresReview(action.Type) {
			existing["reviewQueuedStep"] = step
		}
		return existing, true
	}
	existing["updatedStep"] = step
	existing["lastActionType"] = action.Type
	existing["frictionScore"] = round3(clamp01(floatValue(existing, "frictionScore", 0.35) + 0.03))
	existing["amountScore"] = round3(clamp01(floatValue(existing, "amountScore", 0.3) + numericPayload(action.Payload, "delta", 0.02)))
	return existing, true
}

func protocolLatency(protocolType string, actor, target *horizonmodels.SimulationAgentSpec) int {
	latency := 0
	switch strings.TrimSpace(protocolType) {
	case "regulatory_case":
		latency = 2
	case "routing_protocol", "allocation_protocol":
		latency = 1
	case "settlement_protocol":
		latency = 1
	default:
		latency = 0
	}
	if agentDomain(target) == "logistics" && agentDomain(actor) != "logistics" {
		latency++
	}
	variant := int(mapAnyFloat(actor.Traits, "variant", 0))
	if variant%3 == 2 && (strings.TrimSpace(protocolType) == "regulatory_case" || strings.TrimSpace(protocolType) == "routing_protocol") {
		latency++
	}
	return latency
}

func protocolResponseDecision(world *horizonmodels.SimulationWorld, target *horizonmodels.SimulationAgentSpec, message map[string]any, step int) (string, metricState, bool) {
	protocolType := strings.TrimSpace(stringValue(message, "protocolType"))
	targetStress := 0.4
	targetBudget := 0.5
	targetTrust := 0.5
	if target != nil {
		targetStress = mapFloat(target.BeliefState, "stress", 0.4)
		targetBudget = mapFloat(target.ResourceState, "budget", 0.5)
		targetTrust = mapFloat(target.BeliefState, "trust", 0.5)
	}
	variant := int(mapAnyFloat(target.Traits, "variant", 0))
	response := "accept"
	stateDelta := metricState{}
	queueReview := boolValue(message, "requiresReview", false)
	switch protocolType {
	case "regulatory_case":
		switch {
		case targetStress > 0.76+0.01*float64(variant%2) && targetTrust < 0.46:
			response = "reject"
			stateDelta = metricState{risk: 0.012, policy: 0.020, supply: 0.008}
		case targetStress > 0.60+0.02*float64(variant%3) || queueReview:
			response = "review"
			stateDelta = metricState{risk: 0.005, policy: 0.010}
		default:
			response = "accept"
			stateDelta = metricState{risk: -0.004, policy: 0.006}
		}
	case "routing_protocol", "allocation_protocol":
		switch {
		case targetBudget < 0.22+0.01*float64(variant%2):
			response = "delay"
			stateDelta = metricState{risk: 0.008, price: 0.008, supply: 0.010}
		case targetStress > 0.68+0.02*float64(variant%3):
			response = "counter"
			stateDelta = metricState{risk: 0.004, supply: 0.006}
		default:
			response = "proceed"
			stateDelta = metricState{risk: -0.004, supply: -0.010}
		}
	case "settlement_protocol":
		switch {
		case targetBudget < 0.18+0.01*float64(variant%2):
			response = "reject"
			stateDelta = metricState{risk: 0.012, price: 0.012}
		case targetStress > 0.58+0.02*float64(variant%3):
			response = "review"
			stateDelta = metricState{risk: 0.006, price: 0.006}
		default:
			response = "accept"
			stateDelta = metricState{risk: -0.003, price: -0.006}
		}
	case "narrative_protocol":
		switch {
		case targetTrust < 0.34:
			response = "counter"
			stateDelta = metricState{narrative: 0.012}
		case targetStress > 0.72:
			response = "delay"
			stateDelta = metricState{narrative: 0.010, risk: 0.004}
		default:
			response = "align"
			stateDelta = metricState{narrative: -0.006}
		}
	default:
		if targetStress > 0.75 {
			response = "delay"
			stateDelta = metricState{risk: 0.005}
		}
	}
	return response, stateDelta, queueReview || response == "review"
}

func queueReviewItem(world *horizonmodels.SimulationWorld, message map[string]any, reviewer *horizonmodels.SimulationAgentSpec, step int) {
	queues := ensureWorldQueues(world)
	items, _ := queues["review"].([]map[string]any)
	txID := stringValue(message, "transactionId")
	for _, item := range items {
		if stringValue(item, "transactionId") == txID && !boolValue(item, "resolved", false) {
			return
		}
	}
	reviewerID := stringValue(message, "toAgentId")
	if reviewer != nil && strings.TrimSpace(reviewer.ID) != "" {
		reviewerID = reviewer.ID
	}
	items = append(items, map[string]any{
		"id":              fmt.Sprintf("review:%s:%d", txID, step),
		"transactionId":   txID,
		"protocolType":    stringValue(message, "protocolType"),
		"reviewerAgentId": reviewerID,
		"fromAgentId":     stringValue(message, "fromAgentId"),
		"toAgentId":       stringValue(message, "toAgentId"),
		"eligibleStep":    step + 1,
		"resolved":        false,
	})
	queues["review"] = items
}

func reviewDecision(world *horizonmodels.SimulationWorld, reviewer *horizonmodels.SimulationAgentSpec, item map[string]any, step int) (string, metricState) {
	fromID := stringValue(item, "fromAgentId")
	toID := stringValue(item, "toAgentId")
	profile := relationshipProfile(world, fromID, toID)
	stress := 0.45
	budget := 0.5
	if reviewer != nil {
		stress = mapFloat(reviewer.BeliefState, "stress", 0.45)
		budget = mapFloat(reviewer.ResourceState, "budget", 0.5)
	}
	variant := int(mapAnyFloat(reviewer.Traits, "variant", 0))
	switch {
	case stress > 0.80+0.01*float64(variant%2) && profile["trust"] < 0.4:
		return "rejected", metricState{risk: 0.014, policy: 0.016, supply: 0.010}
	case budget < 0.20+0.01*float64(variant%2):
		return "deferred", metricState{risk: 0.008, price: 0.006}
	default:
		return "approved", metricState{risk: -0.006, supply: -0.008}
	}
}

func transactionOperationalLoad(world *horizonmodels.SimulationWorld, step int) microStepSummary {
	if world == nil || len(world.Transactions) == 0 {
		return microStepSummary{}
	}
	active := 0.0
	queue := 0.0
	friction := 0.0
	for _, tx := range world.Transactions {
		status := strings.TrimSpace(stringValue(tx, "status"))
		switch status {
		case "approved", "in_execution", "reserved", "rerouted", "pending_review", "delayed":
			active++
			friction += floatValue(tx, "frictionScore", 0.3)
		}
		if qs := strings.TrimSpace(stringValue(tx, "queueStatus")); qs == "review_pending" || qs == "under_review" || qs == "awaiting_response" {
			queue++
		}
	}
	if active == 0 && queue == 0 {
		return microStepSummary{}
	}
	load := clamp01(active / float64(max(len(world.Transactions), 1)))
	queueLoad := clamp01(queue / float64(max(len(world.Transactions), 1)))
	frictionLoad := clamp01(friction / float64(max(len(world.Transactions), 1)))
	return microStepSummary{
		stateDelta: metricState{
			risk:   0.004*load + 0.005*queueLoad,
			price:  0.006*load + 0.004*frictionLoad,
			supply: 0.010*load + 0.008*queueLoad + 0.004*frictionLoad,
		},
	}
}

func applyMessageResponseToTransaction(tx map[string]any, responseType string, step int) {
	tx["lastResponseType"] = responseType
	tx["updatedStep"] = step
	switch strings.TrimSpace(responseType) {
	case "accept", "align", "proceed":
		tx["status"] = "approved"
		tx["queueStatus"] = "responded"
	case "review", "counter":
		tx["status"] = "pending_review"
		tx["queueStatus"] = "under_review"
	case "delay":
		tx["status"] = "delayed"
		tx["queueStatus"] = "awaiting_response"
	case "reject":
		tx["status"] = "rejected"
		tx["queueStatus"] = "closed"
	}
	tx["statusHistory"] = appendStatusHistory(tx["statusHistory"], step, stringValue(tx, "status"))
}

func applyReviewDecisionToTransaction(tx map[string]any, decision string, step int) {
	tx["reviewDecision"] = decision
	tx["updatedStep"] = step
	switch strings.TrimSpace(decision) {
	case "approved":
		tx["status"] = "approved"
		tx["queueStatus"] = "reviewed"
	case "deferred":
		tx["status"] = "delayed"
		tx["queueStatus"] = "review_pending"
	case "rejected":
		tx["status"] = "rejected"
		tx["queueStatus"] = "closed"
	}
	tx["statusHistory"] = appendStatusHistory(tx["statusHistory"], step, stringValue(tx, "status"))
}

func findTransaction(world *horizonmodels.SimulationWorld, transactionID string) map[string]any {
	if world == nil || strings.TrimSpace(transactionID) == "" {
		return nil
	}
	for _, tx := range world.Transactions {
		if stringValue(tx, "id") == strings.TrimSpace(transactionID) {
			return tx
		}
	}
	return nil
}

func nextTransactionStatus(tx map[string]any, profile map[string]float64, counterpartyStress, counterpartyBudget float64, step int) (string, float64, float64, float64) {
	status := strings.TrimSpace(stringValue(tx, "status"))
	opened := int(floatValue(tx, "openedStep", 0))
	age := step - opened
	trust := profile["trust"]
	reliability := profile["reliability"]
	switch status {
	case "proposed":
		if boolValue(tx, "requiresReview", false) {
			return "pending_review", 0.006, 0.004, 0.008
		}
		return "approved", -0.004, 0.002, -0.006
	case "pending_review":
		if counterpartyStress > 0.82 && trust < 0.45 {
			if age >= 2 {
				return "rejected", 0.018, 0.012, 0.016
			}
			return "delayed", 0.010, 0.008, 0.012
		}
		if trust >= 0.42 || reliability >= 0.48 || counterpartyBudget > 0.28 {
			return "approved", -0.006, 0.004, -0.010
		}
	case "approved":
		switch strings.TrimSpace(stringValue(tx, "transactionType")) {
		case "shipment_reroute":
			return "rerouted", -0.008, 0.010, -0.030
		case "inventory_reservation":
			return "reserved", -0.006, 0.012, -0.024
		case "license_case":
			return "cleared", -0.010, 0.004, -0.012
		default:
			return "in_execution", -0.004, 0.006, -0.010
		}
	case "in_execution":
		if age >= 1 && counterpartyBudget > 0.16 {
			return "fulfilled", -0.012, 0.004, -0.022
		}
		if counterpartyStress > 0.84 {
			return "delayed", 0.008, 0.006, 0.012
		}
	case "delayed":
		if age >= 3 && trust < 0.38 {
			return "blocked", 0.016, 0.010, 0.018
		}
		if counterpartyStress < 0.70 || counterpartyBudget > 0.24 {
			return "approved", -0.004, 0.004, -0.008
		}
	}
	return status, 0, 0, 0
}

func preferredCounterpartyClasses(actionType string) []string {
	switch strings.TrimSpace(actionType) {
	case string(horizonmodels.SimulationActionSwitchSupplier), string(horizonmodels.SimulationActionBuy), string(horizonmodels.SimulationActionDelayPurchase):
		return []string{"supplier", "importer", "wholesaler", "china_equipment_supplier", "kazakh_chemical_supplier", "parallel_import_broker", "russian_oilfield_customer"}
	case string(horizonmodels.SimulationActionReserveInventory), string(horizonmodels.SimulationActionAllocateCapacity), string(horizonmodels.SimulationActionReprioritizeFlow):
		return []string{"jebel_ali_hub_operator", "warehouse_operator", "production_planner", "facility_operator_nevada", "facility_operator_texas", "tyumen_pump_assembly_operator", "tyumen_chemicals_operator"}
	case string(horizonmodels.SimulationActionRerouteShipment), string(horizonmodels.SimulationActionExpediteShipment):
		return []string{"jebel_ali_hub_operator", "cross_border_logistics_operator", "logistics_operator", "ground_logistics_operator", "ocean_logistics_operator", "port_operator", "rail_carrier", "parallel_import_broker"}
	case string(horizonmodels.SimulationActionRequestExemption), string(horizonmodels.SimulationActionEnforceRule), string(horizonmodels.SimulationActionLobby):
		return []string{"export_control_officer", "sanctions_compliance_officer", "transshipment_compliance_officer", "procurement_compliance_lead", "trade_official", "customs_official", "regulator"}
	case string(horizonmodels.SimulationActionAdjustBudget), string(horizonmodels.SimulationActionHedgeCost):
		return []string{"enterprise_treasury", "ofs_settlement_bank", "trade_credit_insurer", "bank", "payment_rail"}
	case string(horizonmodels.SimulationActionAdjustGuidance), string(horizonmodels.SimulationActionBroadcastMessage), string(horizonmodels.SimulationActionIssueStatement), string(horizonmodels.SimulationActionEscalate):
		return []string{"pr_agent", "investor_relations", "market_intelligence_analyst", "equity_analyst", "media"}
	default:
		return nil
	}
}

func matchesPreferredCounterparty(agentClass string, preferred []string) bool {
	if len(preferred) == 0 {
		return false
	}
	for _, item := range preferred {
		if wildcardFoldMatch(strings.TrimSpace(item), strings.TrimSpace(agentClass)) {
			return true
		}
	}
	return false
}

func wildcardFoldMatch(pattern, value string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	value = strings.ToLower(strings.TrimSpace(value))
	if pattern == "" || value == "" {
		return false
	}
	if !strings.Contains(pattern, "*") {
		return pattern == value
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 2 {
		switch {
		case parts[0] == "" && parts[1] == "":
			return true
		case parts[0] == "":
			return strings.HasSuffix(value, parts[1])
		case parts[1] == "":
			return strings.HasPrefix(value, parts[0])
		default:
			return strings.HasPrefix(value, parts[0]) && strings.HasSuffix(value, parts[1])
		}
	}
	cursor := 0
	for _, part := range parts {
		if part == "" {
			continue
		}
		index := strings.Index(value[cursor:], part)
		if index < 0 {
			return false
		}
		cursor += index + len(part)
	}
	return true
}

func overlapsActionTargetRefs(targetRefs, originRefs []string) bool {
	if len(targetRefs) == 0 || len(originRefs) == 0 {
		return false
	}
	originSet := map[string]struct{}{}
	for _, origin := range compactStrings(originRefs) {
		originSet[origin] = struct{}{}
	}
	for _, ref := range compactStrings(targetRefs) {
		if _, ok := originSet[ref]; ok {
			return true
		}
	}
	return false
}

func ensureRelationshipState(world *horizonmodels.SimulationWorld) map[string]any {
	if world.RelationshipState == nil {
		world.RelationshipState = map[string]any{}
	}
	return world.RelationshipState
}

func relationshipProfile(world *horizonmodels.SimulationWorld, fromID, toID string) map[string]float64 {
	state := ensureRelationshipState(world)
	key := strings.TrimSpace(fromID) + "->" + strings.TrimSpace(toID)
	raw, _ := state[key].(map[string]any)
	if raw == nil {
		raw = map[string]any{
			"trust":            0.5,
			"reliability":      0.5,
			"interactionCount": 0.0,
		}
		state[key] = raw
	}
	return map[string]float64{
		"trust":            mapAnyFloat(raw, "trust", 0.5),
		"reliability":      mapAnyFloat(raw, "reliability", 0.5),
		"interactionCount": mapAnyFloat(raw, "interactionCount", 0),
	}
}

func updateRelationshipState(world *horizonmodels.SimulationWorld, fromID, toID string, trustDelta, reliabilityDelta float64, lastProtocol any, lastOutcome string) {
	state := ensureRelationshipState(world)
	key := strings.TrimSpace(fromID) + "->" + strings.TrimSpace(toID)
	raw, _ := state[key].(map[string]any)
	if raw == nil {
		raw = map[string]any{}
	}
	raw["trust"] = round3(clamp01(mapAnyFloat(raw, "trust", 0.5) + trustDelta))
	raw["reliability"] = round3(clamp01(mapAnyFloat(raw, "reliability", 0.5) + reliabilityDelta))
	raw["interactionCount"] = round3(mapAnyFloat(raw, "interactionCount", 0) + 1)
	if text := strings.TrimSpace(fmt.Sprintf("%v", lastProtocol)); text != "" {
		raw["lastProtocol"] = text
	}
	if outcome := strings.TrimSpace(lastOutcome); outcome != "" {
		raw["lastOutcome"] = outcome
		switch outcome {
		case "advanced", "accept", "align", "proceed", "approved":
			raw["successCount"] = round3(mapAnyFloat(raw, "successCount", 0) + 1)
		case "delay", "deferred":
			raw["delayCount"] = round3(mapAnyFloat(raw, "delayCount", 0) + 1)
		case "reject", "rejected":
			raw["blockedCount"] = round3(mapAnyFloat(raw, "blockedCount", 0) + 1)
		}
	}
	state[key] = raw
}

func mergeMicroStepSummary(left, right microStepSummary) microStepSummary {
	left.messageCount += right.messageCount
	left.protocolCount += right.protocolCount
	left.transactionChanges += right.transactionChanges
	left.stateDelta.risk += right.stateDelta.risk
	left.stateDelta.price += right.stateDelta.price
	left.stateDelta.narrative += right.stateDelta.narrative
	left.stateDelta.policy += right.stateDelta.policy
	left.stateDelta.supply += right.stateDelta.supply
	return left
}

func protocolForAction(actionType string) (string, string) {
	switch strings.TrimSpace(actionType) {
	case string(horizonmodels.SimulationActionRequestExemption), string(horizonmodels.SimulationActionEnforceRule), string(horizonmodels.SimulationActionLobby):
		return "regulatory_case", "regulatory_request"
	case string(horizonmodels.SimulationActionRerouteShipment), string(horizonmodels.SimulationActionExpediteShipment):
		return "routing_protocol", "routing_request"
	case string(horizonmodels.SimulationActionReserveInventory), string(horizonmodels.SimulationActionAllocateCapacity), string(horizonmodels.SimulationActionReprioritizeFlow):
		return "allocation_protocol", "allocation_request"
	case string(horizonmodels.SimulationActionAdjustBudget), string(horizonmodels.SimulationActionHedgeCost):
		return "settlement_protocol", "finance_request"
	case string(horizonmodels.SimulationActionAdjustGuidance), string(horizonmodels.SimulationActionBroadcastMessage), string(horizonmodels.SimulationActionIssueStatement), string(horizonmodels.SimulationActionEscalate), string(horizonmodels.SimulationActionChangeBelief):
		return "narrative_protocol", "alignment_notice"
	case string(horizonmodels.SimulationActionSwitchSupplier), string(horizonmodels.SimulationActionBuy), string(horizonmodels.SimulationActionDelayPurchase), string(horizonmodels.SimulationActionSell):
		return "procurement_protocol", "commercial_request"
	default:
		return "coordination_protocol", "coordination_request"
	}
}

func transactionForAction(actionType string) (transactionType string, protocolType string, initialStatus string) {
	switch strings.TrimSpace(actionType) {
	case string(horizonmodels.SimulationActionRequestExemption), string(horizonmodels.SimulationActionEnforceRule):
		return "license_case", "regulatory_case", "proposed"
	case string(horizonmodels.SimulationActionRerouteShipment), string(horizonmodels.SimulationActionExpediteShipment):
		return "shipment_reroute", "routing_protocol", "approved"
	case string(horizonmodels.SimulationActionReserveInventory):
		return "inventory_reservation", "allocation_protocol", "approved"
	case string(horizonmodels.SimulationActionAllocateCapacity), string(horizonmodels.SimulationActionReprioritizeFlow):
		return "capacity_allocation", "allocation_protocol", "proposed"
	case string(horizonmodels.SimulationActionAdjustBudget), string(horizonmodels.SimulationActionHedgeCost):
		return "settlement_cover", "settlement_protocol", "proposed"
	case string(horizonmodels.SimulationActionSwitchSupplier):
		return "supplier_switch", "procurement_protocol", "proposed"
	case string(horizonmodels.SimulationActionBuy), string(horizonmodels.SimulationActionDelayPurchase), string(horizonmodels.SimulationActionSell):
		return "procurement_order", "procurement_protocol", "proposed"
	default:
		return "", "", ""
	}
}

func requiresReview(actionType string) bool {
	switch strings.TrimSpace(actionType) {
	case string(horizonmodels.SimulationActionRequestExemption), string(horizonmodels.SimulationActionEnforceRule), string(horizonmodels.SimulationActionSwitchSupplier), string(horizonmodels.SimulationActionAllocateCapacity), string(horizonmodels.SimulationActionAdjustBudget), string(horizonmodels.SimulationActionHedgeCost):
		return true
	default:
		return false
	}
}

func transactionSubjectRefs(actor, target *horizonmodels.SimulationAgentSpec, action horizonmodels.SimulationAction) []string {
	refs := make([]string, 0, len(action.TargetRefs)+len(actor.OriginRefs)+len(target.OriginRefs))
	refs = append(refs, action.TargetRefs...)
	refs = append(refs, actor.OriginRefs...)
	refs = append(refs, target.OriginRefs...)
	return compactStrings(refs)
}

func isTerminalTransactionStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "fulfilled", "reserved", "rerouted", "cleared", "rejected", "blocked":
		return true
	default:
		return false
	}
}

func appendStatusHistory(raw any, step int, status string) []map[string]any {
	history, _ := raw.([]map[string]any)
	history = append(history, map[string]any{
		"step":   step,
		"status": status,
	})
	return history
}

func countOpenTransactions(items []map[string]any) int {
	total := 0
	for _, item := range items {
		if !isTerminalTransactionStatus(stringValue(item, "status")) {
			total++
		}
	}
	return total
}

func countOpenReviewQueue(world *horizonmodels.SimulationWorld) int {
	if world == nil || world.AgentState == nil {
		return 0
	}
	queues, _ := world.AgentState["queues"].(map[string]any)
	if queues == nil {
		return 0
	}
	items, _ := queues["review"].([]map[string]any)
	total := 0
	for _, item := range items {
		if !boolValue(item, "resolved", false) {
			total++
		}
	}
	return total
}

func executionPressureFloor(world *horizonmodels.SimulationWorld, axis string) float64 {
	if world == nil {
		return 0
	}
	if axis != horizonmodels.AxisPhysicalFlowSupply &&
		axis != horizonmodels.AxisPhysicalFlowDemand &&
		axis != horizonmodels.AxisLogisticsCapacity &&
		axis != horizonmodels.AxisLogisticsLatency {
		return 0
	}
	openTx := float64(countOpenTransactions(world.Transactions))
	reviewOpen := float64(countOpenReviewQueue(world))
	if openTx == 0 && reviewOpen == 0 {
		return 0
	}
	base := clamp01(0.04 + openTx/200.0 + reviewOpen/100.0)
	return round3(base)
}

func protocolItemsByStep(items []map[string]any, step int, kind string) []map[string]any {
	out := make([]map[string]any, 0, 8)
	for _, item := range items {
		if int(floatValue(item, "step", -1)) != step {
			continue
		}
		if strings.TrimSpace(kind) != "" && strings.TrimSpace(stringValue(item, "kind")) != strings.TrimSpace(kind) {
			continue
		}
		out = append(out, cloneMap(item))
	}
	return out
}

func transactionItemsByStep(items []map[string]any, step int) []map[string]any {
	out := make([]map[string]any, 0, 8)
	for _, item := range items {
		if int(floatValue(item, "updatedStep", -1)) != step {
			continue
		}
		out = append(out, cloneMap(item))
	}
	return out
}
