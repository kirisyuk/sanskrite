package propagation

import "strings"

func metricBaseName(metricNameOrPath string) string {
	s := strings.TrimSpace(metricNameOrPath)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "/") {
		parts := strings.Split(strings.Trim(s, "/"), "/")
		if len(parts) == 0 {
			return ""
		}
		return parts[len(parts)-1]
	}
	return s
}

// getMetricType определяет тип метрики (magnitude или directional) на основе её имени
// Использует список стандартизированных метрик из таксономии
func getMetricType(metricName string) string {
	metricName = metricBaseName(metricName)
	// Стандартизированные directional метрики (из taxonomy.md)
	directionalMetrics := map[string]bool{
		"geopolitical_alignment": true,
		"market_sentiment":       true,
		"public_perception":      true,
		"sentiment_score":        true,
		"political_alignment":    true,
		"correlation_coefficient": true,
	}

	if directionalMetrics[metricName] {
		return "directional"
	}

	// По умолчанию считаем magnitude
	// Стандартизированные magnitude метрики:
	// operational_health, capacity_utilization, supply_chain_criticality,
	// water_intensity, energy_intensity, labor_intensity,
	// stability_index, corruption_vulnerability, sanction_exposure
	return "magnitude"
}

// clampMetricValue ограничивает значение метрики в зависимости от её типа
func clampMetricValue(value float64, metricType string) float64 {
	if metricType == "directional" {
		// Directional: [-1.0, 1.0]
		if value < -1.0 {
			return -1.0
		}
		if value > 1.0 {
			return 1.0
		}
		return value
	}

	// Magnitude: [0.0, 1.0]
	if value < 0.0 {
		return 0.0
	}
	if value > 1.0 {
		return 1.0
	}
	return value
}

