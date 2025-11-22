package ontology

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/igorkirisyuk/sanskrite/internal/repo"
)

// TrendEvolutionService автоматически вычисляет velocity, acceleration, stage для Trend
type TrendEvolutionService struct {
	trendsRepo      *repo.TrendRepository
	metricsRepo     *repo.MetricStateRepository
	eventsRepo      *repo.EventRepository
	logger          *log.Logger
}

// NewTrendEvolutionService создает новый сервис эволюции трендов
func NewTrendEvolutionService(
	trendsRepo *repo.TrendRepository,
	metricsRepo *repo.MetricStateRepository,
	eventsRepo *repo.EventRepository,
	logger *log.Logger,
) *TrendEvolutionService {
	return &TrendEvolutionService{
		trendsRepo:  trendsRepo,
		metricsRepo: metricsRepo,
		eventsRepo:  eventsRepo,
		logger:      logger,
	}
}

// CalculateTrendState вычисляет новый TrendState на основе MetricStates и Events
func (s *TrendEvolutionService) CalculateTrendState(
	ctx context.Context,
	tenantID string,
	trend *models.Trend,
	atTime time.Time,
) (*models.TrendState, error) {
	// Получаем все MetricStates, связанные с трендом через EvidenceRefs
	var metricValues []float64
	var timestamps []time.Time

	// Если у тренда есть EvidenceRefs, используем их
	if len(trend.Evidence.MetricStates) > 0 {
		for _, metricID := range trend.Evidence.MetricStates {
			metric, err := s.metricsRepo.Get(ctx, metricID)
			if err == nil && metric != nil && metric.Value != nil {
				// Преобразуем Value в float64
				var floatValue float64
				switch v := metric.Value.(type) {
				case float64:
					floatValue = v
				case int:
					floatValue = float64(v)
				case int64:
					floatValue = float64(v)
				default:
					continue // Пропускаем нечисловые значения
				}
				metricValues = append(metricValues, floatValue)
				if metric.Timestamp != nil {
					timestamps = append(timestamps, *metric.Timestamp)
				} else {
					timestamps = append(timestamps, metric.CreatedAt)
				}
			}
		}
	}

	// Если недостаточно данных, возвращаем nil
	if len(metricValues) < 2 {
		return nil, fmt.Errorf("insufficient data for trend calculation")
	}

	// Вычисляем velocity (dM/dt) - средняя скорость изменения
	velocity := s.calculateVelocity(metricValues, timestamps)

	// Вычисляем acceleration (d²M/dt²) - ускорение
	acceleration := s.calculateAcceleration(metricValues, timestamps)

	// Вычисляем volatility - стандартное отклонение изменений
	volatility := s.calculateVolatility(metricValues)

	// Определяем stage на основе velocity и acceleration
	stage := s.detectStage(velocity, acceleration, metricValues)

	// Вычисляем текущий уровень (последнее значение метрики)
	var level *float64
	if len(metricValues) > 0 {
		lastValue := metricValues[len(metricValues)-1]
		level = &lastValue
	}

	// Вычисляем confidence на основе количества данных
	confidence := math.Min(1.0, float64(len(metricValues))/10.0) // Максимум при 10+ точках

	trendState := &models.TrendState{
		AtTime:      atTime,
		Stage:       stage,
		Level:       level,
		Velocity:    &velocity,
		Acceleration: &acceleration,
		Volatility:  &volatility,
		Confidence:  confidence,
		EvidenceRefs: struct {
			MetricStates []string `json:"metricStates,omitempty" firestore:"metricStates,omitempty"`
			Events       []string `json:"events,omitempty" firestore:"events,omitempty"`
		}{
			MetricStates: trend.Evidence.MetricStates,
		},
	}

	return trendState, nil
}

// calculateVelocity вычисляет скорость изменения метрики
func (s *TrendEvolutionService) calculateVelocity(values []float64, timestamps []time.Time) float64 {
	if len(values) < 2 {
		return 0.0
	}

	// Используем линейную регрессию для вычисления скорости
	var sumX, sumY, sumXY, sumX2 float64
	n := float64(len(values))

	for i, val := range values {
		// Нормализуем время относительно первого значения
		x := timestamps[i].Sub(timestamps[0]).Hours()
		sumX += x
		sumY += val
		sumXY += x * val
		sumX2 += x * x
	}

	// Формула линейной регрессии: velocity = (n*sumXY - sumX*sumY) / (n*sumX2 - sumX^2)
	denominator := n*sumX2 - sumX*sumX
	if math.Abs(denominator) < 1e-10 {
		// Fallback: простая разница между первым и последним
		return (values[len(values)-1] - values[0]) / timestamps[len(timestamps)-1].Sub(timestamps[0]).Hours()
	}

	velocity := (n*sumXY - sumX*sumY) / denominator
	return velocity
}

// calculateAcceleration вычисляет ускорение (вторая производная)
func (s *TrendEvolutionService) calculateAcceleration(values []float64, timestamps []time.Time) float64 {
	if len(values) < 3 {
		return 0.0
	}

	// Вычисляем velocity для первой и второй половины данных
	mid := len(values) / 2
	firstHalfVelocity := s.calculateVelocity(values[:mid+1], timestamps[:mid+1])
	secondHalfVelocity := s.calculateVelocity(values[mid:], timestamps[mid:])

	// Ускорение = изменение velocity / время
	timeDiff := timestamps[len(timestamps)-1].Sub(timestamps[0]).Hours()
	if timeDiff < 1e-10 {
		return 0.0
	}

	acceleration := (secondHalfVelocity - firstHalfVelocity) / timeDiff
	return acceleration
}

// calculateVolatility вычисляет волатильность (стандартное отклонение изменений)
func (s *TrendEvolutionService) calculateVolatility(values []float64) float64 {
	if len(values) < 2 {
		return 0.0
	}

	// Вычисляем изменения
	var changes []float64
	for i := 1; i < len(values); i++ {
		change := values[i] - values[i-1]
		changes = append(changes, change)
	}

	// Вычисляем среднее изменение
	var sum float64
	for _, change := range changes {
		sum += change
	}
	mean := sum / float64(len(changes))

	// Вычисляем стандартное отклонение
	var variance float64
	for _, change := range changes {
		variance += (change - mean) * (change - mean)
	}
	variance /= float64(len(changes))

	return math.Sqrt(variance)
}

// detectStage определяет стадию жизненного цикла тренда
func (s *TrendEvolutionService) detectStage(velocity, acceleration float64, values []float64) models.TrendStage {
	// Логика определения стадии:
	// - Introduction: низкие значения, положительная velocity, положительная acceleration
	// - Rise: растущие значения, положительная velocity, acceleration может быть любой
	// - Peak: высокие значения, velocity близка к 0 или отрицательная, отрицательная acceleration
	// - Decline: падающие значения, отрицательная velocity
	// - Obsolescence: очень низкие значения, отрицательная velocity

	if len(values) == 0 {
		return models.TrendStageIntroduction
	}

	lastValue := values[len(values)-1]
	firstValue := values[0]

	// Нормализуем значения для определения относительного уровня
	valueRange := math.Max(math.Abs(firstValue), math.Abs(lastValue))
	if valueRange < 1e-10 {
		valueRange = 1.0
	}
	normalizedValue := lastValue / valueRange

	// Определяем стадию на основе velocity, acceleration и нормализованного значения
	if math.Abs(velocity) < 0.01 && acceleration < -0.01 {
		return models.TrendStagePeak
	}

	if velocity > 0.01 {
		if acceleration > 0.01 {
			return models.TrendStageRise
		}
		if normalizedValue < 0.2 {
			return models.TrendStageIntroduction
		}
		return models.TrendStageRise
	}

	if velocity < -0.01 {
		if normalizedValue < 0.1 {
			return models.TrendStageObsolescence
		}
		return models.TrendStageDecline
	}

	// По умолчанию
	if normalizedValue > 0.7 {
		return models.TrendStagePeak
	}

	return models.TrendStageRise
}

// UpdateTrendState добавляет новый TrendState к Trend
func (s *TrendEvolutionService) UpdateTrendState(
	ctx context.Context,
	tenantID string,
	trendID string,
) error {
	trend, err := s.trendsRepo.Get(ctx, trendID)
	if err != nil {
		return fmt.Errorf("failed to get trend: %w", err)
	}

	// Вычисляем новый TrendState
	newState, err := s.CalculateTrendState(ctx, tenantID, trend, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to calculate trend state: %w", err)
	}

	// Добавляем к существующим States
	trend.States = append(trend.States, *newState)

	// Обновляем Trend
	trend.UpdatedAt = time.Now().UTC()
	if err := s.trendsRepo.Upsert(ctx, trend); err != nil {
		return fmt.Errorf("failed to update trend: %w", err)
	}

	if s.logger != nil {
		s.logger.Printf("[TrendEvolutionService] Updated trend %s: stage=%s, velocity=%.2f, acceleration=%.2f",
			trendID, newState.Stage, *newState.Velocity, *newState.Acceleration)
	}

	return nil
}

