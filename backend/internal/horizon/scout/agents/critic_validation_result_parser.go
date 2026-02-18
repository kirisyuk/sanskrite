package agents

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func parseValidationResultJSON(jsonData []byte) (*ValidationResult, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(jsonData, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal validation result payload: %w", err)
	}
	return parseValidationResultMap(payload)
}

func parseValidationResultMap(payload map[string]interface{}) (*ValidationResult, error) {
	if payload == nil {
		return nil, fmt.Errorf("failed to unmarshal validation result: empty payload")
	}
	normalized := make(map[string]interface{}, len(payload))
	for k, v := range payload {
		normalized[k] = v
	}

	if v, ok := normalized["missingInfo"]; ok {
		normalized["missingInfo"] = normalizeMissingInfo(v)
	}
	if v, ok := normalized["confidence"]; ok {
		if num, parsed := parseInterfaceFloat(v); parsed {
			normalized["confidence"] = num
		}
	}

	normalizedJSON, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal normalized validation result: %w", err)
	}

	var out ValidationResult
	if err := json.Unmarshal(normalizedJSON, &out); err != nil {
		return nil, fmt.Errorf("failed to unmarshal validation result: %w", err)
	}
	return &out, nil
}

func normalizeMissingInfo(raw interface{}) []string {
	var out []string
	appendItem := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		out = append(out, s)
	}

	switch v := raw.(type) {
	case nil:
		return out
	case string:
		appendItem(v)
	case []interface{}:
		for _, item := range v {
			switch it := item.(type) {
			case nil:
				continue
			case string:
				appendItem(it)
			case map[string]interface{}:
				appendItem(extractQuestionFromObject(it))
			default:
				appendItem(fmt.Sprint(it))
			}
		}
	case map[string]interface{}:
		appendItem(extractQuestionFromObject(v))
	default:
		appendItem(fmt.Sprint(v))
	}

	return out
}

func extractQuestionFromObject(obj map[string]interface{}) string {
	for _, key := range []string{"question", "query", "text", "reason", "message", "missingInfo"} {
		if raw, ok := obj[key]; ok {
			switch val := raw.(type) {
			case string:
				if strings.TrimSpace(val) != "" {
					return val
				}
			default:
				asText := strings.TrimSpace(fmt.Sprint(val))
				if asText != "" && asText != "<nil>" {
					return asText
				}
			}
		}
	}
	return strings.TrimSpace(fmt.Sprint(obj))
}

func parseInterfaceFloat(raw interface{}) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint32:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return n, err == nil
	default:
		return 0, false
	}
}
