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
	// metricThresholds override defaults for specific metric names (highest priority)
	metricThresholds map[string]float64
	// unitThresholds are defaults by unit (e.g., ratio, usd, units)
	unitThresholds map[string]float64
	// defaultThreshold is fallback when neither metric nor unit thresholds are known
	defaultThreshold float64
}

// NewThresholdManager создает новый менеджер порогов
func NewThresholdManager() *ThresholdManager {
	return &ThresholdManager{
		metricThresholds: map[string]float64{
			"operational_health":     0.05, // 5% изменение
			"capacity_utilization":   0.05,
			"supply_chain_criticality": 0.1,
			"stability_index":        0.05,
			"corruption_vulnerability": 0.1,
			"sanction_exposure":      0.1,
			"geopolitical_alignment": 0.1,
			"market_sentiment":       0.1,
			"public_perception":      0.1,
			// Overrides for metrics where unit-level threshold is too blunt.
			"price":                   1,
			"freight_cost":            1_000,
		},
		unitThresholds: map[string]float64{
			// Units: thresholds are ABSOLUTE deltas on MetricValue.Value.
			"ratio":         0.01,
			"usd":           1_000_000,
			"eur":           1_000_000,
			"rub":           50_000_000,
			"units":         100,
			"units_per_day": 100,
			"days":          1,
			"hours":         1,
			"index_point":   5,
			"code":          1,
		},
		defaultThreshold: 0.05,
	}
}

// ExceedsThreshold проверяет, превышает ли изменение порог значимости
func (m *ThresholdManager) ExceedsThreshold(metricName string, delta float64, unit *string) bool {
	// Используем абсолютное значение delta
	absDelta := delta
	if absDelta < 0 {
		absDelta = -absDelta
	}

	// 1) Metric override (highest priority)
	key := thresholdMetricKey(metricName)
	if key != "" {
		if thr, ok := m.metricThresholds[key]; ok {
			return absDelta >= thr
		}
	}

	// 2) Unit-level threshold
	if unit != nil {
		u := strings.ToLower(strings.TrimSpace(*unit))
		if u != "" {
			if thr, ok := m.unitThresholds[u]; ok {
				return absDelta >= thr
			}
		}
	}

	// 3) Default
	return absDelta >= m.defaultThreshold
}

// SetThreshold устанавливает порог для метрики (override).
func (m *ThresholdManager) SetThreshold(metricName string, threshold float64) {
	if m.metricThresholds == nil {
		m.metricThresholds = map[string]float64{}
	}
	m.metricThresholds[thresholdMetricKey(metricName)] = threshold
}

// SetUnitThreshold sets default threshold for a unit (e.g., ratio/usd/units).
func (m *ThresholdManager) SetUnitThreshold(unit string, threshold float64) {
	if m.unitThresholds == nil {
		m.unitThresholds = map[string]float64{}
	}
	u := strings.ToLower(strings.TrimSpace(unit))
	if u == "" {
		return
	}
	m.unitThresholds[u] = threshold
}

