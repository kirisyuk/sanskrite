package trace

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/rtctx"
)

// Log emits a single-line JSON trace event for Horizon lens/scout pipelines.
// Always-on by design: this is meant for diagnosing logic issues in production.
//
// Output example:
// {
//   "ts":"2025-12-28T12:00:00Z",
//   "tracePrefix":"lens:abcd",
//   "phase":"intent",
//   "event":"intent.resolve.start",
//   "tenantId":"...",
//   "projectId":"...",
//   "lensId":"...",
//   "jobId":"...",
//   "correlationId":"abcd",
//   "sessionId":"...",
//   "data":{...}
// }
func Log(ctx context.Context, phase string, event string, data map[string]any) {
	phase = strings.TrimSpace(phase)
	event = strings.TrimSpace(event)
	if phase == "" {
		phase = "unknown"
	}
	if event == "" {
		event = "unknown"
	}

	rc, _ := rtctx.Get(ctx)
	correlationID := strings.TrimSpace(rc.CorrelationID)
	sessionID := strings.TrimSpace(rc.SessionID)

	tracePrefix := ""
	if correlationID != "" {
		tracePrefix = "lens:" + correlationID
	}

	payload := map[string]any{
		"ts":          time.Now().UTC().Format(time.RFC3339Nano),
		"tracePrefix": tracePrefix,
		"phase":       phase,
		"event":       event,
		"tenantId":    strings.TrimSpace(rc.TenantID),
		"projectId":   strings.TrimSpace(rc.ProjectID),
		"lensId":      strings.TrimSpace(rc.LensID),
		"correlationId": func() string {
			if correlationID == "" {
				return ""
			}
			return correlationID
		}(),
		"sessionId": func() string {
			if sessionID == "" {
				return ""
			}
			return sessionID
		}(),
	}

	// Optional known IDs in data.
	if data != nil {
		if v, ok := data["jobId"].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				payload["jobId"] = s
			}
		}
		if v, ok := data["lensId"].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				payload["lensId"] = s
			}
		}
		if v, ok := data["projectId"].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				payload["projectId"] = s
			}
		}
		if v, ok := data["tenantId"].(string); ok {
			if s := strings.TrimSpace(v); s != "" {
				payload["tenantId"] = s
			}
		}
	}

	payload["data"] = data

	b, err := json.Marshal(payload)
	if err != nil {
		// Never fail the business logic because of tracing.
		fmt.Printf(`{"ts":"%s","phase":"%s","event":"trace.marshal_failed","error":%q}`+"\n",
			time.Now().UTC().Format(time.RFC3339Nano), phase, err.Error())
		return
	}
	fmt.Println(string(b))
}

func Trunc(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	if max < 12 {
		return s[:max]
	}
	return s[:max-12] + "…(trunc)"
}

func StrSlice(in []string, max int) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

func HashText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}






