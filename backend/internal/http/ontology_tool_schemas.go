package http

import gen_gemini "github.com/igorkirisyuk/sanskrite/internal/genai/gemini"

// GetOntologyToolSchemas returns all ontology tool schemas
func GetOntologyToolSchemas() []gen_gemini.Tool {
	return []gen_gemini.Tool{
		getEntitySearchToolSchema(),
		getEntityProfileToolSchema(),
		getEventTimelineToolSchema(),
		getEventDetailToolSchema(),
		getClaimQueryToolSchema(),
		getUncertaintyListToolSchema(),
		getRelationGraphToolSchema(),
		getCausalLinksToolSchema(),
		getTrendGetToolSchema(),
		getMetricLatestToolSchema(),
		getScenarioRunToolSchema(),
		getRiskTopToolSchema(),
		getSourceProfileToolSchema(),
		getTrustRecalculateToolSchema(),
		getDocumentSearchToolSchema(),
		getEntityEnrichToolSchema(),
	}
}

func getEntitySearchToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "entity_search",
		Description: "Search for entities in the ontological model by name, type, or domain. Returns a list of matching entities with their basic information. Use this when you need to find specific entities (people, organizations, products, locations, etc.) or explore entities of a certain type.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Entity canonical name to search for (e.g., 'Nvidia', 'Apple Inc.', 'Moscow')",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"description": "Filter by entity type: Person, Organization, Product, Location, EventEntity, Document, Facility, SupplyChainNode, GovernmentProject, IndexMetric, Other",
					"enum": []string{"Person", "Organization", "Product", "Location", "EventEntity", "Document", "Facility", "SupplyChainNode", "GovernmentProject", "IndexMetric", "Other"},
				},
				"domain": map[string]interface{}{
					"type":        "string",
					"description": "Filter by business domain (e.g., 'Technology', 'Finance', 'Healthcare')",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results (1-100, default: 20)",
					"minimum":     1,
					"maximum":     100,
				},
			},
		},
	}
}

func getEntityProfileToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "entity_profile",
		Description: "Get comprehensive profile of an entity including its attributes, related claims, events, relations, and trends. Use this when you need deep information about a specific entity, its relationships, and its role in the knowledge graph.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityId": map[string]interface{}{
					"type":        "string",
					"description": "Unique identifier of the entity",
				},
			},
			"required": []string{"entityId"},
		},
	}
}

func getEventTimelineToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "event_timeline",
		Description: "Get timeline of events for an entity, domain, or filtered by type and time range. Returns events ordered by occurrence time. Use this to understand what happened to an entity over time, explore events in a domain, or analyze temporal patterns.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityId": map[string]interface{}{
					"type":        "string",
					"description": "Filter events where this entity participated",
				},
				"domain": map[string]interface{}{
					"type":        "string",
					"description": "Filter events by business domain",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"description": "Filter by event type (e.g., 'ProductLaunch', 'Merger', 'RegulationChange')",
				},
				"from": map[string]interface{}{
					"type":        "string",
					"description": "Start date in ISO 8601 format (e.g., '2024-01-01T00:00:00Z')",
				},
				"to": map[string]interface{}{
					"type":        "string",
					"description": "End date in ISO 8601 format (e.g., '2024-12-31T23:59:59Z')",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results (1-200, default: 50)",
					"minimum":     1,
					"maximum":     200,
				},
			},
		},
	}
}

func getEventDetailToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "event_detail",
		Description: "Get full details of a specific event including participants, related entities, claims, and causal links. Use this to understand the complete context and impact of a particular event.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"eventId": map[string]interface{}{
					"type":        "string",
					"description": "Unique identifier of the event",
				},
			},
			"required": []string{"eventId"},
		},
	}
}

func getClaimQueryToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "claim_query",
		Description: "Query claims (factual statements) by subject, predicate, or domain. Claims represent verified facts about entities and their relationships. Use this to find specific facts, verify information, or explore what is known about a subject.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"subjectId": map[string]interface{}{
					"type":        "string",
					"description": "Filter claims about this entity or event",
				},
				"predicate": map[string]interface{}{
					"type":        "string",
					"description": "Filter by predicate type (e.g., 'locatedIn', 'owns', 'employs', 'revenue')",
				},
				"domain": map[string]interface{}{
					"type":        "string",
					"description": "Filter by business domain",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results (1-200, default: 50)",
					"minimum":     1,
					"maximum":     200,
				},
			},
		},
	}
}

func getUncertaintyListToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "uncertainty_list",
		Description: "List uncertainty models (conflicts, risks, ambiguities) for an entity or domain. These represent conflicting information, potential risks, or ambiguous situations. Use this to identify areas of uncertainty, conflicting facts, or potential risks.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityId": map[string]interface{}{
					"type":        "string",
					"description": "Filter uncertainties related to this entity",
				},
				"domain": map[string]interface{}{
					"type":        "string",
					"description": "Filter by business domain",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results (1-100, default: 20)",
					"minimum":     1,
					"maximum":     100,
				},
			},
		},
	}
}

func getRelationGraphToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "relation_graph",
		Description: "Get relation graph for an entity or domain. Returns all relations (connections) between entities, concepts, and other ontological objects. Use this to understand how entities are connected, explore the knowledge graph structure, or find relationships.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityId": map[string]interface{}{
					"type":        "string",
					"description": "Get relations for this entity (both as subject and object)",
				},
				"domain": map[string]interface{}{
					"type":        "string",
					"description": "Filter relations by business domain",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results (1-500, default: 100)",
					"minimum":     1,
					"maximum":     500,
				},
			},
		},
	}
}

func getCausalLinksToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "causal_links",
		Description: "Get causal links (cause-effect relationships) between events. Returns causal relationships with probabilities and evidence. Use this to understand what events caused other events, analyze chains of causation, or predict consequences.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"eventId": map[string]interface{}{
					"type":        "string",
					"description": "Filter causal links involving this event (as cause or effect)",
				},
				"domain": map[string]interface{}{
					"type":        "string",
					"description": "Filter by business domain",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results (1-200, default: 50)",
					"minimum":     1,
					"maximum":     200,
				},
			},
		},
	}
}

func getTrendGetToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "trend_get",
		Description: "Get trend information for a metric or focus area. Returns trend states with velocity, acceleration, and stage. Use this to understand how metrics are evolving over time, identify trends, or analyze patterns.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"focusId": map[string]interface{}{
					"type":        "string",
					"description": "Filter trends by focus ID",
				},
				"metric": map[string]interface{}{
					"type":        "string",
					"description": "Filter trends by metric name (e.g., 'revenue', 'marketShare', 'userCount')",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results (1-100, default: 20)",
					"minimum":     1,
					"maximum":     100,
				},
			},
		},
	}
}

func getMetricLatestToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "metric_latest",
		Description: "Get latest metric states for an entity, metric name, or all metrics. Returns quantitative measurements with timestamps, confidence, and provenance. Use this to get current values of metrics, track changes, or analyze quantitative data.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"metricId": map[string]interface{}{
					"type":        "string",
					"description": "Get specific metric by ID",
				},
				"entityId": map[string]interface{}{
					"type":        "string",
					"description": "Filter metrics for this entity",
				},
				"metricName": map[string]interface{}{
					"type":        "string",
					"description": "Filter by metric name (e.g., 'revenue', 'marketShare', 'employeeCount')",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results (1-200, default: 50)",
					"minimum":     1,
					"maximum":     200,
				},
			},
		},
	}
}

func getScenarioRunToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "scenario_run",
		Description: "Run scenario analysis for decision-making and impact assessment. Takes input parameters and returns scenario outcomes with impact on metrics and recommendations. Use this to simulate outcomes, evaluate decisions, or predict consequences of actions.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"input": map[string]interface{}{
					"type":        "object",
					"description": "Scenario input parameters (decision variables, assumptions, etc.)",
				},
			},
			"required": []string{"input"},
		},
	}
}

func getRiskTopToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "risk_top",
		Description: "Get top risks for a domain or supply chain. Aggregates risks from uncertainty models, scenarios, and claims. Use this to identify potential threats, assess risk levels, or prioritize risk mitigation efforts.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"domain": map[string]interface{}{
					"type":        "string",
					"description": "Filter risks by business domain",
				},
				"supplyChainId": map[string]interface{}{
					"type":        "string",
					"description": "Filter risks for a specific supply chain",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results (1-50, default: 10)",
					"minimum":     1,
					"maximum":     50,
				},
			},
		},
	}
}

func getSourceProfileToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "source_profile",
		Description: "Get source profile including source metadata and trust information. Returns source details and current trust score. Use this to assess source reliability, understand data provenance, or evaluate information quality.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"sourceId": map[string]interface{}{
					"type":        "string",
					"description": "Unique identifier of the source",
				},
			},
			"required": []string{"sourceId"},
		},
	}
}

func getTrustRecalculateToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "trust_recalculate",
		Description: "Trigger recalculation of trust score for a source. Recalculates confidence scores for claims and events from this source based on updated trust metrics. Use this when source reliability has changed or needs to be updated.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"sourceId": map[string]interface{}{
					"type":        "string",
					"description": "Source ID to recalculate trust for",
				},
			},
			"required": []string{"sourceId"},
		},
	}
}

func getDocumentSearchToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "document_search",
		Description: "Search documents in the system. Returns matching documents with metadata. Use this to find source documents, access original content, or explore document collections.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filter": map[string]interface{}{
					"type":        "object",
					"description": "Search filters (e.g., {'sourceId': '...', 'dateRange': {...}})",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results (1-100, default: 20)",
					"minimum":     1,
					"maximum":     100,
				},
			},
		},
	}
}

func getEntityEnrichToolSchema() gen_gemini.Tool {
	return gen_gemini.Tool{
		Name:        "entity_enrich",
		Description: "Trigger enrichment of an entity with external knowledge bases (e.g., Wikidata). Updates entity with additional attributes, aliases, and identifiers from external sources. Use this to enhance entity information or fill missing data.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"entityId": map[string]interface{}{
					"type":        "string",
					"description": "Entity ID to enrich",
				},
			},
			"required": []string{"entityId"},
		},
	}
}

