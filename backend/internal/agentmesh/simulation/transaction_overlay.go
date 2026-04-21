package simulation

import (
	"fmt"
	"sort"
	"strings"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

func buildProtocolSummary(worlds []*horizonmodels.SimulationWorld) []map[string]any {
	type agg struct {
		protocolType string
		kindCounts   map[string]int
		classCounts  map[string]int
		messageCount int
	}
	byProtocol := map[string]*agg{}
	for _, world := range worlds {
		if world == nil {
			continue
		}
		for _, item := range world.ProtocolTraces {
			protocolType := strings.TrimSpace(stringValue(item, "protocolType"))
			if protocolType == "" {
				protocolType = "untyped_protocol"
			}
			entry := byProtocol[protocolType]
			if entry == nil {
				entry = &agg{
					protocolType: protocolType,
					kindCounts:   map[string]int{},
					classCounts:  map[string]int{},
				}
				byProtocol[protocolType] = entry
			}
			entry.kindCounts[stringValue(item, "messageType")]++
			entry.messageCount++
			if fromClass := strings.TrimSpace(stringValue(item, "fromClass")); fromClass != "" {
				entry.classCounts[fromClass]++
			}
			if toClass := strings.TrimSpace(stringValue(item, "toClass")); toClass != "" {
				entry.classCounts[toClass]++
			}
		}
	}
	type item struct {
		key string
		agg *agg
	}
	items := make([]item, 0, len(byProtocol))
	for key, value := range byProtocol {
		items = append(items, item{key: key, agg: value})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].agg.messageCount == items[j].agg.messageCount {
			return items[i].key < items[j].key
		}
		return items[i].agg.messageCount > items[j].agg.messageCount
	})
	out := make([]map[string]any, 0, min(len(items), 8))
	for _, item := range items[:min(len(items), 8)] {
		if item.key == "" || item.key == "untyped_protocol" {
			continue
		}
		out = append(out, map[string]any{
			"protocolType":       item.key,
			"messageCount":       item.agg.messageCount,
			"dominantMessages":   topMechanisms(item.agg.kindCounts),
			"participantClasses": topMechanisms(item.agg.classCounts),
			"summary":            protocolSummaryLine(item.key, item.agg.kindCounts, item.agg.classCounts),
		})
	}
	return out
}

func buildTransactionSummary(worlds []*horizonmodels.SimulationWorld) []map[string]any {
	type agg struct {
		transactionType string
		statusCounts    map[string]int
		queueCounts     map[string]int
		partyCounts     map[string]int
		total           int
	}
	byType := map[string]*agg{}
	for _, world := range worlds {
		if world == nil {
			continue
		}
		for _, tx := range world.Transactions {
			txType := strings.TrimSpace(stringValue(tx, "transactionType"))
			if txType == "" {
				txType = "untyped_transaction"
			}
			entry := byType[txType]
			if entry == nil {
				entry = &agg{
					transactionType: txType,
					statusCounts:    map[string]int{},
					queueCounts:     map[string]int{},
					partyCounts:     map[string]int{},
				}
				byType[txType] = entry
			}
			entry.total++
			entry.statusCounts[stringValue(tx, "status")]++
			entry.queueCounts[stringValue(tx, "queueStatus")]++
			if fromClass := strings.TrimSpace(stringValue(tx, "fromClass")); fromClass != "" {
				entry.partyCounts[fromClass]++
			}
			if toClass := strings.TrimSpace(stringValue(tx, "toClass")); toClass != "" {
				entry.partyCounts[toClass]++
			}
		}
	}
	type item struct {
		key string
		agg *agg
	}
	items := make([]item, 0, len(byType))
	for key, value := range byType {
		items = append(items, item{key: key, agg: value})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].agg.total == items[j].agg.total {
			return items[i].key < items[j].key
		}
		return items[i].agg.total > items[j].agg.total
	})
	out := make([]map[string]any, 0, min(len(items), 10))
	for _, item := range items[:min(len(items), 10)] {
		bottleneck := ""
		if ranked := topMechanisms(item.agg.statusCounts); len(ranked) > 0 {
			bottleneck = ranked[0]
		}
		dominantQueue := ""
		if ranked := topMechanisms(item.agg.queueCounts); len(ranked) > 0 {
			dominantQueue = ranked[0]
		}
		out = append(out, map[string]any{
			"transactionType":    item.key,
			"count":              item.agg.total,
			"statusProfile":      intMapAny(item.agg.statusCounts),
			"queueProfile":       intMapAny(item.agg.queueCounts),
			"dominantStatus":     bottleneck,
			"dominantQueue":      dominantQueue,
			"participantClasses": topMechanisms(item.agg.partyCounts),
			"summary":            transactionSummaryLine(item.key, item.agg.statusCounts, item.agg.queueCounts, item.agg.partyCounts),
		})
	}
	return out
}

func buildProtocolSummaryFromEvents(worldEvents map[string][]*horizonmodels.SimulationEvent) []map[string]any {
	type agg struct {
		messageCount int
		worldCount   int
	}
	counts := map[string]*agg{}
	worldsSeen := map[string]map[string]struct{}{}
	for worldID, events := range worldEvents {
		for _, event := range events {
			if event == nil {
				continue
			}
			items := append(anySlice(event.Payload["protocolEvents"]), anySlice(event.Payload["messages"])...)
			for _, raw := range items {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				protocol := strings.TrimSpace(firstString(item, "protocol", "kind", "type"))
				if protocol == "" {
					continue
				}
				entry := counts[protocol]
				if entry == nil {
					entry = &agg{}
					counts[protocol] = entry
				}
				entry.messageCount++
				if worldsSeen[protocol] == nil {
					worldsSeen[protocol] = map[string]struct{}{}
				}
				worldsSeen[protocol][worldID] = struct{}{}
			}
		}
	}
	type item struct {
		key string
		agg *agg
	}
	items := make([]item, 0, len(counts))
	for key, value := range counts {
		value.worldCount = len(worldsSeen[key])
		items = append(items, item{key: key, agg: value})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].agg.messageCount == items[j].agg.messageCount {
			return items[i].key < items[j].key
		}
		return items[i].agg.messageCount > items[j].agg.messageCount
	})
	out := make([]map[string]any, 0, min(len(items), 8))
	for _, item := range items[:min(len(items), 8)] {
		out = append(out, map[string]any{
			"protocolType": item.key,
			"messageCount": item.agg.messageCount,
			"worldCount":   item.agg.worldCount,
			"summary":      fmt.Sprintf("%s appears in %d worlds with %d protocol events.", item.key, item.agg.worldCount, item.agg.messageCount),
		})
	}
	return out
}

func buildTransactionSummaryFromEvents(worldEvents map[string][]*horizonmodels.SimulationEvent) []map[string]any {
	type agg struct {
		count      int
		worldCount int
	}
	statusCounts := map[string]*agg{}
	worldsSeen := map[string]map[string]struct{}{}
	for worldID, events := range worldEvents {
		for _, event := range events {
			if event == nil {
				continue
			}
			for _, raw := range anySlice(event.Payload["transactions"]) {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				status := strings.TrimSpace(firstString(item, "status"))
				if status == "" {
					status = "open"
				}
				entry := statusCounts[status]
				if entry == nil {
					entry = &agg{}
					statusCounts[status] = entry
				}
				entry.count++
				if worldsSeen[status] == nil {
					worldsSeen[status] = map[string]struct{}{}
				}
				worldsSeen[status][worldID] = struct{}{}
			}
		}
	}
	type item struct {
		key string
		agg *agg
	}
	items := make([]item, 0, len(statusCounts))
	for key, value := range statusCounts {
		value.worldCount = len(worldsSeen[key])
		items = append(items, item{key: key, agg: value})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].agg.count == items[j].agg.count {
			return items[i].key < items[j].key
		}
		return items[i].agg.count > items[j].agg.count
	})
	out := make([]map[string]any, 0, min(len(items), 8))
	for _, item := range items[:min(len(items), 8)] {
		out = append(out, map[string]any{
			"transactionType": item.key,
			"count":           item.agg.count,
			"dominantStatus":  item.key,
			"worldCount":      item.agg.worldCount,
			"summary":         fmt.Sprintf("%s transactions appear in %d worlds with %d state changes.", item.key, item.agg.worldCount, item.agg.count),
			"statusProfile":   map[string]any{item.key: item.agg.count},
		})
	}
	return out
}

func protocolSummaryLine(protocolType string, kindCounts, classCounts map[string]int) string {
	return fmt.Sprintf("%s is mainly expressed through %s and concentrated in %s.", protocolType, strings.Join(topMechanisms(kindCounts), ", "), strings.Join(topMechanisms(classCounts), ", "))
}

func transactionSummaryLine(transactionType string, statusCounts, queueCounts, partyCounts map[string]int) string {
	dominantStatuses := topMechanisms(statusCounts)
	dominantQueues := topMechanisms(queueCounts)
	dominantParties := topMechanisms(partyCounts)
	queuePart := ""
	if len(dominantQueues) > 0 {
		queuePart = fmt.Sprintf(" queue is mainly %s;", strings.Join(dominantQueues[:min(len(dominantQueues), 2)], ", "))
	}
	return fmt.Sprintf("%s is mostly ending in %s;%s concentrated around %s.", transactionType, strings.Join(dominantStatuses[:min(len(dominantStatuses), 2)], ", "), queuePart, strings.Join(dominantParties[:min(len(dominantParties), 3)], ", "))
}

func buildTransactionAwareNarrative(protocolSummary, transactionSummary []map[string]any) []string {
	lines := []string{}
	if len(protocolSummary) > 0 {
		lines = append(lines, fmt.Sprintf("The most active protocol is %s.", stringValue(protocolSummary[0], "protocolType")))
	}
	if len(transactionSummary) > 0 {
		top := transactionSummary[0]
		lines = append(lines, fmt.Sprintf("The heaviest transaction loop is %s, dominated by %s.", stringValue(top, "transactionType"), stringValue(top, "dominantStatus")))
		if queueProfile := mapValue(top["queueProfile"]); len(queueProfile) > 0 {
			if dominantQueue := firstString(top, "dominantQueue"); dominantQueue != "" {
				lines = append(lines, fmt.Sprintf("Its approval queue is mainly in %s.", dominantQueue))
			}
		}
	}
	return compactNarrativeLines(lines)
}

func anySlice(raw any) []any {
	switch typed := raw.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}
