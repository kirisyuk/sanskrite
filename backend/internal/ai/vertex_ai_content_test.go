package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestVertexAISearchContentQuality checks whether Gemini actually uses fragment data
// (canonical paths, labels, identifiers) from the datastore grounding in its output.
func TestVertexAISearchContentQuality(t *testing.T) {
	if os.Getenv("VERTEX_AI_LIVE_TEST") != "1" {
		t.Skip("set VERTEX_AI_LIVE_TEST=1 to run live Vertex AI tests")
	}

	projectID := "hatlone-test"
	location := "us-central1"
	dataStoreID := "horizon-fragments-store"

	mm := NewAIModelManager()
	svc, err := NewVertexAIService(projectID, location, mm)
	if err != nil {
		t.Fatalf("NewVertexAIService: %v", err)
	}
	svc.WithDataStore(dataStoreID)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Test: Ask model to resolve entities using datastore + return structured data
	t.Run("EntityResolutionWithCanonicalPaths", func(t *testing.T) {
		prompt := `You have access to a knowledge base of entities (organizations, products, markets, etc.) via grounding.

For EACH of the following entities, search the knowledge base and return:
1. The exact canonicalPath from the knowledge base (e.g., /org/sa/saudi-aramco)
2. The English label
3. Any identifiers you found (wikidataId, legalName, shortName)
4. Whether you found it in the knowledge base or only via web search

Entities to resolve:
- Saudi Aramco
- SABIC
- ACWA Power
- NEOM
- Ma'aden

Return as JSON array:
[{"name":"...","canonicalPath":"...","label":"...","identifiers":{"wikidataId":"...","legalName":"..."},"source":"datastore|web|both"}]`

		task := &AITask{
			TaskType:     TaskTypeEntityExtraction,
			InputContent: prompt,
			AIRequirements: AIRequirements{
				TextGenerationCapability:  true,
				SearchGroundingCapability: true,
				MaxOutputSize:             4000,
			},
		}

		result, err := svc.ExecuteTask(ctx, task, "gemini-2.5-flash")
		if err != nil {
			t.Fatalf("ExecuteTask: %v", err)
		}

		t.Logf("=== RAW RESPONSE ===")
		t.Logf("%s", result.Content)

		// Count datastore vs web citations
		dsCitations := 0
		webCitations := 0
		for _, c := range result.Citations {
			if strings.HasPrefix(c.Title, "[DATASTORE]") {
				dsCitations++
			} else {
				webCitations++
			}
		}
		t.Logf("\n=== CITATIONS: %d datastore, %d web, %d total ===", dsCitations, webCitations, len(result.Citations))

		// Check if response contains canonical path patterns
		hasCanonicalPaths := strings.Contains(result.Content, "/org/")
		hasIdentifiers := strings.Contains(result.Content, "wikidata") || strings.Contains(result.Content, "Q")
		t.Logf("Has canonical paths (/org/...): %v", hasCanonicalPaths)
		t.Logf("Has identifiers (wikidata): %v", hasIdentifiers)

		// Try to parse JSON from response
		content := result.Content
		// Strip markdown code fences if present
		if idx := strings.Index(content, "```json"); idx != -1 {
			content = content[idx+7:]
		} else if idx := strings.Index(content, "```"); idx != -1 {
			content = content[idx+3:]
		}
		if idx := strings.LastIndex(content, "```"); idx != -1 {
			content = content[:idx]
		}
		content = strings.TrimSpace(content)

		var entities []map[string]interface{}
		if err := json.Unmarshal([]byte(content), &entities); err != nil {
			t.Logf("Could not parse as JSON array: %v", err)
			// Try to find JSON within the content
			if start := strings.Index(result.Content, "["); start != -1 {
				if end := strings.LastIndex(result.Content, "]"); end > start {
					jsonStr := result.Content[start : end+1]
					if err := json.Unmarshal([]byte(jsonStr), &entities); err != nil {
						t.Logf("Could not parse extracted JSON either: %v", err)
					}
				}
			}
		}

		if len(entities) > 0 {
			t.Logf("\n=== PARSED ENTITIES ===")
			for _, e := range entities {
				name, _ := e["name"].(string)
				cp, _ := e["canonicalPath"].(string)
				label, _ := e["label"].(string)
				source, _ := e["source"].(string)
				ids := ""
				if idMap, ok := e["identifiers"].(map[string]interface{}); ok {
					idBytes, _ := json.Marshal(idMap)
					ids = string(idBytes)
				}
				t.Logf("  %s: canonicalPath=%s label=%q source=%s identifiers=%s",
					name, cp, label, source, ids)

				// Verify canonical path looks valid
				if cp != "" && strings.HasPrefix(cp, "/") {
					t.Logf("    ✓ Valid canonical path")
				} else if cp != "" {
					t.Logf("    ✗ Unexpected canonical path format: %s", cp)
				} else {
					t.Logf("    ✗ No canonical path returned")
				}
			}
		}
	})

	// Test: Check if model can distinguish between entities in DB vs not in DB
	t.Run("ExistingVsNewEntity", func(t *testing.T) {
		prompt := `Search the knowledge base. For each entity below, tell me:
1. Does it EXIST in the knowledge base? (yes/no)
2. If yes, what is its canonicalPath?
3. If yes, what identifiers does it have?

Entities:
- Saudi Aramco (oil company in Saudi Arabia)
- Sirius Cybernetics Corporation (fictional company from Hitchhiker's Guide)
- NEOM (Saudi megacity project)
- Wakanda Industries (fictional)

Return as JSON: [{"name":"...","exists":true/false,"canonicalPath":"...","identifiers":{}}]`

		task := &AITask{
			TaskType:     TaskTypeEntityExtraction,
			InputContent: prompt,
			AIRequirements: AIRequirements{
				TextGenerationCapability:  true,
				SearchGroundingCapability: true,
				MaxOutputSize:             2000,
			},
		}

		result, err := svc.ExecuteTask(ctx, task, "gemini-2.5-flash")
		if err != nil {
			t.Fatalf("ExecuteTask: %v", err)
		}

		t.Logf("=== RAW RESPONSE ===")
		t.Logf("%s", result.Content)

		dsCitations := 0
		for _, c := range result.Citations {
			if strings.HasPrefix(c.Title, "[DATASTORE]") {
				dsCitations++
			}
		}
		t.Logf("\n=== CITATIONS: %d datastore / %d total ===", dsCitations, len(result.Citations))

		// Quick checks
		if !strings.Contains(result.Content, "true") {
			t.Error("Expected 'true' for existing entities")
		}
		if !strings.Contains(result.Content, "false") {
			t.Error("Expected 'false' for fictional entities")
		}
		_ = fmt.Sprint() // avoid unused import
	})
}
