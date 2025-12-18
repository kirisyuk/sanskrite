package propagation

import "strings"

func thresholdMetricKey(metricNameOrPath string) string {
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

// ThresholdManager управляет порогами значимости изменений
type ThresholdManager struct {
	// Пороги по умолчанию для разных метрик
	defaultThresholds map[string]float64
}

// NewThresholdManager создает новый менеджер порогов
func NewThresholdManager() *ThresholdManager {
	return &ThresholdManager{
		defaultThresholds: map[string]float64{
			"operational_health":     0.05, // 5% изменение
			"capacity_utilization":   0.05,
			"supply_chain_criticality": 0.1,
			"stability_index":        0.05,
			"corruption_vulnerability": 0.1,
			"sanction_exposure":      0.1,
			"geopolitical_alignment": 0.1,
			"market_sentiment":       0.1,
			"public_perception":      0.1,
			// По умолчанию для неизвестных метрик
			"default": 0.05,
		},
	}
}

// ExceedsThreshold проверяет, превышает ли изменение порог значимости
func (m *ThresholdManager) ExceedsThreshold(metricName string, delta float64) bool {
	// Используем абсолютное значение delta
	absDelta := delta
	if absDelta < 0 {
		absDelta = -absDelta
	}

	// Получаем порог для метрики
	key := thresholdMetricKey(metricName)
	threshold, exists := m.defaultThresholds[key]
	if !exists {
		threshold = m.defaultThresholds["default"]
	}

	return absDelta >= threshold
}

// SetThreshold устанавливает порог для метрики
func (m *ThresholdManager) SetThreshold(metricName string, threshold float64) {
	m.defaultThresholds[thresholdMetricKey(metricName)] = threshold
}

// GetThreshold получает порог для метрики
func (m *ThresholdManager) GetThreshold(metricName string) float64 {
	threshold, exists := m.defaultThresholds[thresholdMetricKey(metricName)]
	if !exists {
		return m.defaultThresholds["default"]
	}
	return threshold
}

