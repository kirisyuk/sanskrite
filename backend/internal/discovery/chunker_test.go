package discovery

import (
	"testing"
)

func TestChunkText(t *testing.T) {
	text := "Sentence one. Sentence two! And three? Last sentence."
	chunks := ChunkText(text, 20)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	// Ensure chunks are non-empty and boundaries respected
	for i, c := range chunks {
		if len(c) == 0 {
			t.Fatalf("chunk %d is empty", i)
		}
		if len(c) > 20 && i != len(chunks)-1 {
			t.Fatalf("chunk %d exceeds maxChars: %d", i, len(c))
		}
	}
}
