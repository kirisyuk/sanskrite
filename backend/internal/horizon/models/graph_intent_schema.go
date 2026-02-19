package models

import (
	"encoding/json"
	"fmt"
)

const graphIntentDraft07SchemaURI = "http://json-schema.org/draft-07/schema#"

func GraphIntentDraft07Schemas() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		"plan_request":   GraphIntentPlanRequestSchemaDraft07(),
		"plan_response":  GraphIntentPlanResponseSchemaDraft07(),
		"apply_request":  GraphIntentApplyRequestSchemaDraft07(),
		"apply_response": GraphIntentApplyResponseSchemaDraft07(),
	}
}

func GraphIntentDraft07SchemaJSON(name string) ([]byte, error) {
	s, ok := GraphIntentDraft07Schemas()[name]
	if !ok {
		return nil, fmt.Errorf("unknown graph-intent schema: %s", name)
	}
	return json.MarshalIndent(s, "", "  ")
}

func GraphIntentPlanRequestSchemaDraft07() map[string]interface{} {
	return map[string]interface{}{
		"$schema":              graphIntentDraft07SchemaURI,
		"$id":                  "https://sanskrite.dev/schemas/horizon/graph-intent/plan-request.v1.schema.json",
		"title":                "GraphIntentPlanRequest",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"version", "rawQuery", "snapshot"},
		"properties": map[string]interface{}{
			"version": map[string]interface{}{
				"type":  "string",
				"const": GraphIntentVersionV1,
			},
			"rawQuery": map[string]interface{}{
				"type":      "string",
				"minLength": 1,
			},
			"queryLanguage": map[string]interface{}{
				"type":    "string",
				"default": "auto",
			},
			"executionLanguage": map[string]interface{}{
				"type":    "string",
				"enum":    []string{GraphIntentExecutionLanguageEN},
				"default": GraphIntentExecutionLanguageEN,
			},
			"snapshot": map[string]interface{}{
				"$ref": "#/definitions/snapshot",
			},
			"options": map[string]interface{}{
				"$ref": "#/definitions/planOptions",
			},
		},
		"definitions": graphIntentDefinitionsDraft07(),
	}
}

func GraphIntentPlanResponseSchemaDraft07() map[string]interface{} {
	return map[string]interface{}{
		"$schema":              graphIntentDraft07SchemaURI,
		"$id":                  "https://sanskrite.dev/schemas/horizon/graph-intent/plan-response.v1.schema.json",
		"title":                "GraphIntentPlanResponse",
		"type":                 "object",
		"additionalProperties": false,
		"required": []string{
			"version",
			"normalizedEnglishQuery",
			"intentClass",
			"confidence",
			"create",
			"update",
			"transform",
			"requiresConfirmation",
		},
		"properties": map[string]interface{}{
			"version": map[string]interface{}{
				"type":  "string",
				"const": GraphIntentVersionV1,
			},
			"normalizedEnglishQuery": map[string]interface{}{
				"type":      "string",
				"minLength": 1,
			},
			"intentClass": map[string]interface{}{
				"type": "string",
				"enum": []string{
					GraphIntentClassCreate,
					GraphIntentClassUpdate,
					GraphIntentClassDelete,
					GraphIntentClassMerge,
					GraphIntentClassTransform,
					GraphIntentClassMixed,
					GraphIntentClassUnknown,
				},
			},
			"confidence": map[string]interface{}{
				"type":    "number",
				"minimum": 0.0,
				"maximum": 1.0,
			},
			"create": map[string]interface{}{
				"$ref": "#/definitions/createBlock",
			},
			"update": map[string]interface{}{
				"$ref": "#/definitions/updateBlock",
			},
			"delete": map[string]interface{}{
				"$ref": "#/definitions/deleteBlock",
			},
			"merge": map[string]interface{}{
				"$ref": "#/definitions/mergeBlock",
			},
			"transform": map[string]interface{}{
				"$ref": "#/definitions/transformBlock",
			},
			"explanations": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"warnings": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"requiresConfirmation": map[string]interface{}{
				"type": "boolean",
			},
		},
		"definitions": graphIntentDefinitionsDraft07(),
	}
}

func GraphIntentApplyRequestSchemaDraft07() map[string]interface{} {
	return map[string]interface{}{
		"$schema":              graphIntentDraft07SchemaURI,
		"$id":                  "https://sanskrite.dev/schemas/horizon/graph-intent/apply-request.v1.schema.json",
		"title":                "GraphIntentApplyRequest",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"version", "plan", "idempotencyKey"},
		"properties": map[string]interface{}{
			"version": map[string]interface{}{
				"type":  "string",
				"const": GraphIntentVersionV1,
			},
			"plan": map[string]interface{}{
				"$ref": "#/definitions/planResponse",
			},
			"applyMode": map[string]interface{}{
				"type": "string",
				"enum": []string{
					GraphIntentApplyModePatch,
					GraphIntentApplyModeDry,
				},
				"default": GraphIntentApplyModePatch,
			},
			"idempotencyKey": map[string]interface{}{
				"type":      "string",
				"minLength": 1,
			},
		},
		"definitions": graphIntentDefinitionsWithPlanDraft07(),
	}
}

func GraphIntentApplyResponseSchemaDraft07() map[string]interface{} {
	return map[string]interface{}{
		"$schema":              graphIntentDraft07SchemaURI,
		"$id":                  "https://sanskrite.dev/schemas/horizon/graph-intent/apply-response.v1.schema.json",
		"title":                "GraphIntentApplyResponse",
		"type":                 "object",
		"additionalProperties": false,
		"required": []string{
			"applied",
			"createdNodeCount",
			"createdEdgeCount",
			"updatedNodeCount",
			"updatedEdgeCount",
		},
		"properties": map[string]interface{}{
			"applied": map[string]interface{}{"type": "boolean"},
			"createdNodeCount": map[string]interface{}{
				"type":    "integer",
				"minimum": 0,
			},
			"createdEdgeCount": map[string]interface{}{
				"type":    "integer",
				"minimum": 0,
			},
			"updatedNodeCount": map[string]interface{}{
				"type":    "integer",
				"minimum": 0,
			},
			"updatedEdgeCount": map[string]interface{}{
				"type":    "integer",
				"minimum": 0,
			},
			"unresolvedItems": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"$ref": "#/definitions/unresolvedItem",
				},
			},
			"warnings": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"actionPreview": map[string]interface{}{
				"$ref": "#/definitions/actionPlan",
			},
		},
		"definitions": graphIntentDefinitionsDraft07(),
	}
}

func graphIntentDefinitionsWithPlanDraft07() map[string]interface{} {
	defs := graphIntentDefinitionsDraft07()
		defs["planResponse"] = map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"required": []string{
			"version",
			"normalizedEnglishQuery",
			"intentClass",
			"confidence",
			"create",
			"update",
			"transform",
			"requiresConfirmation",
		},
		"properties": map[string]interface{}{
			"version": map[string]interface{}{
				"type":  "string",
				"const": GraphIntentVersionV1,
			},
			"normalizedEnglishQuery": map[string]interface{}{
				"type":      "string",
				"minLength": 1,
			},
			"intentClass": map[string]interface{}{
				"type": "string",
				"enum": []string{
					GraphIntentClassCreate,
					GraphIntentClassUpdate,
					GraphIntentClassDelete,
					GraphIntentClassMerge,
					GraphIntentClassTransform,
					GraphIntentClassMixed,
					GraphIntentClassUnknown,
				},
			},
			"confidence": map[string]interface{}{
				"type":    "number",
				"minimum": 0.0,
				"maximum": 1.0,
			},
			"create": map[string]interface{}{
				"$ref": "#/definitions/createBlock",
			},
			"update": map[string]interface{}{
				"$ref": "#/definitions/updateBlock",
			},
			"delete": map[string]interface{}{
				"$ref": "#/definitions/deleteBlock",
			},
			"merge": map[string]interface{}{
				"$ref": "#/definitions/mergeBlock",
			},
			"transform": map[string]interface{}{
				"$ref": "#/definitions/transformBlock",
			},
			"explanations": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"warnings": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"requiresConfirmation": map[string]interface{}{
				"type": "boolean",
			},
		},
	}
	return defs
}

func graphIntentDefinitionsDraft07() map[string]interface{} {
	relationTypes := GraphIntentAllowedRelationTypesSorted()
	groupingKinds := GraphIntentAllowedGroupingKindsSorted()
	nodeTypes := GraphIntentAllowedNodeTypesSorted()

	return map[string]interface{}{
		"snapshot": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"nodes": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/snapshotNode",
					},
				},
				"edges": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/snapshotEdge",
					},
				},
				"selectedNodeIds": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
				"visibleNodeIds": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
				"viewState": map[string]interface{}{
					"$ref": "#/definitions/viewState",
				},
			},
		},
		"viewState": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"zoom": map[string]interface{}{
					"type": "number",
				},
				"filters": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": true,
				},
			},
		},
		"xyPoint": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"x", "y"},
			"properties": map[string]interface{}{
				"x": map[string]interface{}{"type": "number"},
				"y": map[string]interface{}{"type": "number"},
			},
		},
		"snapshotNode": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"nodeId":        map[string]interface{}{"type": "string"},
				"fragmentId":    map[string]interface{}{"type": "string"},
				"canonicalPath": map[string]interface{}{"type": "string"},
				"label":         map[string]interface{}{"type": "string"},
				"type":          map[string]interface{}{"type": "string", "enum": nodeTypes},
				"position":      map[string]interface{}{"$ref": "#/definitions/xyPoint"},
			},
		},
		"snapshotEdge": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"edgeId":           map[string]interface{}{"type": "string"},
				"sourceNodeId":     map[string]interface{}{"type": "string"},
				"targetNodeId":     map[string]interface{}{"type": "string"},
				"sourceFragmentId": map[string]interface{}{"type": "string"},
				"targetFragmentId": map[string]interface{}{"type": "string"},
				"relationType":     map[string]interface{}{"type": "string", "enum": relationTypes},
				"direction":        map[string]interface{}{"type": "string", "enum": []string{"forward", "reverse", "bidirectional"}},
			},
		},
		"planOptions": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"allowSearchGrounding":   map[string]interface{}{"type": "boolean"},
				"allowInternalGrounding": map[string]interface{}{"type": "boolean"},
				"maxNewNodes": map[string]interface{}{
					"type":    "integer",
					"minimum": 1,
					"maximum": 500,
				},
				"maxNewEdges": map[string]interface{}{
					"type":    "integer",
					"minimum": 1,
					"maximum": 1000,
				},
			},
		},
		"createBlock": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"groupings": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/grouping",
					},
				},
				"nodes": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/nodeSpec",
					},
				},
				"edges": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/edgeSpec",
					},
				},
			},
		},
		"grouping": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"groupingId", "groupingKind", "title"},
			"properties": map[string]interface{}{
				"groupingId": map[string]interface{}{
					"type":      "string",
					"minLength": 1,
				},
				"groupingKind": map[string]interface{}{
					"type": "string",
					"enum": groupingKinds,
				},
				"title": map[string]interface{}{
					"type":      "string",
					"minLength": 1,
				},
				"anchor": map[string]interface{}{
					"$ref": "#/definitions/anchor",
				},
				"entities": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/nodeSpec",
					},
				},
				"relations": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/edgeSpec",
					},
				},
				"evidence": map[string]interface{}{
					"$ref": "#/definitions/evidenceRef",
				},
			},
		},
		"anchor": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"canonicalPathHint": map[string]interface{}{
					"type": "string",
				},
			},
		},
		"evidenceRef": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"internalRefs": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
				"searchRefs": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
			},
		},
		"nodeSpec": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"label", "type", "canonicalPathHint"},
			"properties": map[string]interface{}{
				"tempId": map[string]interface{}{
					"type": "string",
				},
				"label": map[string]interface{}{
					"type":      "string",
					"minLength": 1,
				},
				"type": map[string]interface{}{
					"type": "string",
					"enum": nodeTypes,
				},
				"canonicalPathHint": map[string]interface{}{
					"type": "string",
				},
				"state": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": true,
				},
				"metadata": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": true,
				},
			},
		},
		"edgeSpec": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"relationType"},
			"anyOf": []interface{}{
				map[string]interface{}{"required": []string{"fromTempId", "toTempId"}},
				map[string]interface{}{"required": []string{"fromNodeId", "toNodeId"}},
			},
			"properties": map[string]interface{}{
				"tempId": map[string]interface{}{"type": "string"},
				"fromTempId": map[string]interface{}{
					"type": "string",
				},
				"toTempId": map[string]interface{}{
					"type": "string",
				},
				"fromNodeId": map[string]interface{}{
					"type": "string",
				},
				"toNodeId": map[string]interface{}{
					"type": "string",
				},
				"relationType": map[string]interface{}{
					"type": "string",
					"enum": relationTypes,
				},
				"direction": map[string]interface{}{
					"type": "string",
					"enum": []string{"forward", "reverse", "bidirectional"},
				},
				"confidence": map[string]interface{}{
					"type":    "number",
					"minimum": 0.0,
					"maximum": 1.0,
				},
				"qualifiers": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": true,
				},
				"metrics": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": true,
				},
			},
		},
		"updateBlock": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"nodePatches": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/nodePatch",
					},
				},
				"edgePatches": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/edgePatch",
					},
				},
				"relationRewrite": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/relationRewrite",
					},
				},
			},
		},
		"deleteBlock": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"nodeIds": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
				"edgeIds": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
			},
		},
		"mergeBlock": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"primaryNodeId": map[string]interface{}{
					"type": "string",
				},
				"duplicateNodeIds": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
				"mergeFragmentData": map[string]interface{}{
					"type": "boolean",
				},
			},
		},
		"nodePatch": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"patchId":       map[string]interface{}{"type": "string"},
				"nodeId":        map[string]interface{}{"type": "string"},
				"tempId":        map[string]interface{}{"type": "string"},
				"fragmentId":    map[string]interface{}{"type": "string"},
				"canonicalPath": map[string]interface{}{"type": "string"},
				"label":         map[string]interface{}{"type": "string"},
				"type":          map[string]interface{}{"type": "string", "enum": nodeTypes},
				"statePatch":    map[string]interface{}{"type": "object", "additionalProperties": true},
				"metadataPatch": map[string]interface{}{"type": "object", "additionalProperties": true},
			},
		},
		"edgePatch": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"patchId":          map[string]interface{}{"type": "string"},
				"edgeId":           map[string]interface{}{"type": "string"},
				"sourceNodeId":     map[string]interface{}{"type": "string"},
				"targetNodeId":     map[string]interface{}{"type": "string"},
				"sourceFragmentId": map[string]interface{}{"type": "string"},
				"targetFragmentId": map[string]interface{}{"type": "string"},
				"relationType":     map[string]interface{}{"type": "string", "enum": relationTypes},
				"direction":        map[string]interface{}{"type": "string", "enum": []string{"forward", "reverse", "bidirectional"}},
				"attributes":       map[string]interface{}{"type": "object", "additionalProperties": true},
			},
		},
		"relationRewrite": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"match", "replaceWith"},
			"properties": map[string]interface{}{
				"rewriteId": map[string]interface{}{
					"type": "string",
				},
				"match": map[string]interface{}{
					"$ref": "#/definitions/relationMatch",
				},
				"replaceWith": map[string]interface{}{
					"$ref": "#/definitions/relationReplacement",
				},
				"reason": map[string]interface{}{
					"type": "string",
				},
			},
		},
		"relationMatch": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"relationType": map[string]interface{}{"type": "string", "enum": relationTypes},
				"sourceNodeIds": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
				"targetNodeIds": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string"},
				},
			},
		},
		"relationReplacement": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"relationType"},
			"properties": map[string]interface{}{
				"relationType": map[string]interface{}{"type": "string", "enum": relationTypes},
				"direction":    map[string]interface{}{"type": "string", "enum": []string{"forward", "reverse", "bidirectional"}},
			},
		},
		"transformBlock": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"deriveChains": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/deriveChain",
					},
				},
				"restructure": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/restructure",
					},
				},
			},
		},
		"deriveChain": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"fromNodeId", "toNodeId"},
			"properties": map[string]interface{}{
				"chainId":       map[string]interface{}{"type": "string"},
				"fromNodeId":    map[string]interface{}{"type": "string"},
				"toNodeId":      map[string]interface{}{"type": "string"},
				"constraints":   map[string]interface{}{"$ref": "#/definitions/chainConstraints"},
				"proposedPath":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"proposedEdges": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"confidence":    map[string]interface{}{"type": "number", "minimum": 0.0, "maximum": 1.0},
			},
		},
		"chainConstraints": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"allowedRelationTypes": map[string]interface{}{
					"type":  "array",
					"items": map[string]interface{}{"type": "string", "enum": relationTypes},
				},
				"maxHops": map[string]interface{}{
					"type":    "integer",
					"minimum": 1,
				},
			},
		},
		"restructure": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"restructureId": map[string]interface{}{"type": "string"},
				"scopeNodeIds":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"mode":          map[string]interface{}{"type": "string"},
				"params":        map[string]interface{}{"type": "object", "additionalProperties": true},
			},
		},
		"unresolvedItem": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"reason"},
			"properties": map[string]interface{}{
				"tempId":          map[string]interface{}{"type": "string"},
				"reason":          map[string]interface{}{"type": "string"},
				"suggestedAction": map[string]interface{}{"type": "string"},
			},
		},
		"actionPlan": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"actions"},
			"properties": map[string]interface{}{
				"actions": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"$ref": "#/definitions/action",
					},
				},
			},
		},
		"action": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"actionId", "kind", "scope"},
			"properties": map[string]interface{}{
				"actionId": map[string]interface{}{"type": "string"},
				"kind": map[string]interface{}{
					"type": "string",
					"enum": []string{
						"create_grouping",
						"create_node",
						"create_edge",
						"update_node",
						"update_edge",
						"rewrite_relation",
						"derive_chain",
						"restructure_graph",
					},
				},
				"scope": map[string]interface{}{
					"type": "string",
					"enum": []string{"create", "update", "transform"},
				},
				"refId": map[string]interface{}{"type": "string"},
				"payload": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": true,
				},
			},
		},
	}
}
