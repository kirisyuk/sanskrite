package simulation

import (
	"fmt"
	"strings"
)

func compactStringsPreserveOrder(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, s := range items {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func mapValue(raw any) map[string]any {
	if raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	if m2, ok := raw.(map[string]interface{}); ok {
		out := make(map[string]any, len(m2))
		for k, v := range m2 {
			out[k] = v
		}
		return out
	}
	return nil
}

func stringSliceFromAny(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, s := range typed {
			if t := strings.TrimSpace(s); t != "" {
				out = append(out, t)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if t := strings.TrimSpace(fmt.Sprint(item)); t != "" {
				out = append(out, t)
			}
		}
		return out
	default:
		return nil
	}
}
