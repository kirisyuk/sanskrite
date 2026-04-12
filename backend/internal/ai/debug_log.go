package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
)

// aiDebugLogEnabled enables verbose AI request/response logging (Vertex, Vertex AI Search,
// request sizes, raw search previews, grounding query lists, etc.).
// It MUST be opt-in because logs may contain user content / model outputs.
func aiDebugLogEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("AI_DEBUG_LOG")))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func shortHash(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	// 8 hex chars is enough for log correlation.
	return hex.EncodeToString(sum[:4])
}

func truncateForLog(s string, maxChars int) string {
	s = strings.TrimSpace(s)
	if maxChars <= 0 || s == "" {
		return ""
	}
	if len(s) <= maxChars {
		return s
	}
	// Keep head + tail (useful for JSON where closing braces matter)
	head := maxChars * 2 / 3
	tail := maxChars - head
	if head < 0 {
		head = 0
	}
	if tail < 0 {
		tail = 0
	}
	return s[:head] + "\n...<truncated>...\n" + s[len(s)-tail:]
}

func singleLineForLog(s string, maxChars int) string {
	// Make sure the final output is a single log line (Cloud Run splits multiline logs).
	v := truncateForLog(s, maxChars)
	v = strings.ReplaceAll(v, "\r\n", "\n")
	v = strings.ReplaceAll(v, "\r", "\n")
	// Preserve line breaks but encode them so Cloud Run won't split.
	v = strings.ReplaceAll(v, "\n", "\\n")
	v = strings.ReplaceAll(v, "\t", "\\t")
	return strings.TrimSpace(v)
}
