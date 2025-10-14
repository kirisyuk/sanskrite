package discovery

import (
	"strings"
	"unicode"
)

// ChunkText splits text into semantically-friendly chunks with soft boundaries.
func ChunkText(text string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = 1200
	}
	s := strings.TrimSpace(text)
	if s == "" {
		return []string{}
	}
	var chunks []string
	start := 0
	for start < len(s) {
		end := start + maxChars
		if end >= len(s) {
			chunks = append(chunks, s[start:])
			break
		}
		// try to break at the last sentence boundary within window
		cut := lastBoundary(s[start:end])
		if cut <= 0 {
			cut = end - start
		}
		chunks = append(chunks, strings.TrimSpace(s[start:start+cut]))
		start = start + cut
	}
	return chunks
}

func lastBoundary(seg string) int {
	last := -1
	for i, r := range seg {
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			// ensure following rune is space/newline if exists
			last = i + 1
		}
	}
	// avoid cutting inside words
	if last == -1 {
		for i := len(seg) - 1; i >= 0; i-- {
			if unicode.IsSpace(rune(seg[i])) {
				last = i
				break
			}
		}
	}
	return last
}
