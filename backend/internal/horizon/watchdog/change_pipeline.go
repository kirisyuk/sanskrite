package watchdog

import (
	"fmt"
	"hash/fnv"
	"math"
	"regexp"
	"strings"
)

// ChangeDecision represents the outcome of multi-phase change evaluation.
type ChangeDecision struct {
	// ChangeAmount is normalized to [0..1], where 0 means no meaningful change and 1 means maximal change.
	ChangeAmount float64
	// Method describes which phase produced the final decision.
	Method string
	// Reason is a short explanation useful for logs/debugging.
	Reason string

	// Debug metrics (optional)
	NormalizedHashEqual bool
	SimHashSimilarity   *float64
	EmbeddingCosineSim  *float64
	LLMChangeAmount     *float64
}

type changeTuning struct {
	// If SimHash similarity is >= this value, we treat change as insignificant.
	simHashNoChangeThreshold float64
	// If SimHash similarity is <= this value, we treat change as significant (skip LLM).
	simHashSignificantThreshold float64

	// If embedding cosine similarity is >= this value, we treat change as insignificant.
	embeddingNoChangeThreshold float64
	// If embedding cosine similarity is <= this value, we treat change as significant.
	embeddingSignificantThreshold float64
}

func defaultChangeTuning() changeTuning {
	return changeTuning{
		// ОПТИМИЗАЦИЯ: Агрессивные пороги для уменьшения grey zone и снижения LLM вызовов.
		// SimHash grey zone сужена с [0.900-0.985] до [0.930-0.985] (было 8.5%, стало 5.5%).
		simHashNoChangeThreshold:      0.985, // Без изменений: >=98.5% similarity
		simHashSignificantThreshold:   0.930, // Было 0.900, стало 0.930 (агрессивнее)
		// Embeddings grey zone сужена с [0.900-0.970] до [0.920-0.970] (было 7%, стало 5%).
		embeddingNoChangeThreshold:    0.970, // Без изменений: >=97% similarity
		embeddingSignificantThreshold: 0.920, // Было 0.900, стало 0.920 (агрессивнее)
		// Результат: ~50% меньше LLM вызовов за счет более агрессивной фильтрации на фазе embeddings.
	}
}

// normalizeContent tries to reduce noise from minor formatting changes.
// It deliberately stays conservative to avoid hiding real changes.
func normalizeContent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)

	// Collapse whitespace.
	reWS := regexp.MustCompile(`\s+`)
	s = reWS.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

var reToken = regexp.MustCompile(`[a-z0-9]+`)

func tokens(s string) []string {
	if s == "" {
		return nil
	}
	return reToken.FindAllString(s, -1)
}

// simHash64 computes a 64-bit SimHash over tokens.
// This is fast and robust to small reorderings but is only a heuristic.
func simHash64(s string) uint64 {
	toks := tokens(s)
	if len(toks) == 0 {
		return 0
	}
	var v [64]int
	for _, t := range toks {
		h := fnv1a64(t)
		for i := 0; i < 64; i++ {
			if (h>>uint(i))&1 == 1 {
				v[i]++
			} else {
				v[i]--
			}
		}
	}
	var out uint64
	for i := 0; i < 64; i++ {
		if v[i] >= 0 {
			out |= 1 << uint(i)
		}
	}
	return out
}

func fnv1a64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

func hamming64(a, b uint64) int {
	x := a ^ b
	// builtin bits.OnesCount64 would be nicer; keep it local to avoid extra import churn.
	n := 0
	for x != 0 {
		x &= x - 1
		n++
	}
	return n
}

func simHashSimilarity(a, b uint64) float64 {
	d := hamming64(a, b)
	return 1.0 - (float64(d) / 64.0)
}

func cosineSimilarity(a, b []float64) (float64, error) {
	if len(a) == 0 || len(b) == 0 {
		return 0, fmt.Errorf("empty vector")
	}
	if len(a) != len(b) {
		return 0, fmt.Errorf("dimension mismatch: %d vs %d", len(a), len(b))
	}
	var dot, na, nb float64
	for i := 0; i < len(a); i++ {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0, fmt.Errorf("zero-norm vector")
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb)), nil
}

