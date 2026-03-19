package adapters

import (
	"strconv"
	"strings"
)


func stringVal(m map[string]interface{}, k string) string {
	if v, ok := m[k]; ok && v != nil {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
		if i, ok := v.(int); ok {
			return strconv.Itoa(i)
		}
	}
	return ""
}

func floatVal(m map[string]interface{}, k string) float64 {
	if v, ok := m[k]; ok && v != nil {
		switch x := v.(type) {
		case float64:
			return x
		case float32:
			return float64(x)
		case int:
			return float64(x)
		case string:
			f, _ := strconv.ParseFloat(x, 64)
			return f
		}
	}
	return 0
}
