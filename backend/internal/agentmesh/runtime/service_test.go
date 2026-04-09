package runtime

import (
	"encoding/json"
	"testing"
)

func TestCloneObjectMapBreaksAliasBeforeEmbeddingReview(t *testing.T) {
	source := map[string]any{
		"accepted":  true,
		"rationale": "ok",
		"nested": map[string]any{
			"quality": "high",
		},
	}

	output := cloneObjectMap(source)
	review := cloneObjectMap(source)
	output["review"] = review

	if _, ok := review["review"]; ok {
		t.Fatalf("review map unexpectedly contains self-reference")
	}
	if _, err := json.Marshal(output); err != nil {
		t.Fatalf("json.Marshal(output) error = %v", err)
	}
}
