package watchdog

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Scheduler планирует время следующего сканирования
type Scheduler struct{}

// NewScheduler создает новый планировщик
func NewScheduler() *Scheduler {
	return &Scheduler{}
}

// CalculateNextScanTime вычисляет время следующего сканирования на основе ISO 8601 duration
func (s *Scheduler) CalculateNextScanTime(interval string, fromTime time.Time) (time.Time, error) {
	duration, err := s.parseISO8601Duration(interval)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse interval: %w", err)
	}
	return fromTime.Add(duration), nil
}

// parseISO8601Duration парсит ISO 8601 duration (например, "PT1H", "PT30M", "P1D")
func (s *Scheduler) parseISO8601Duration(duration string) (time.Duration, error) {
	// Упрощенная реализация для формата PT1H, PT30M, P1D
	// Полная реализация должна поддерживать все варианты ISO 8601

	// Паттерн для PT1H, PT30M и т.д.
	ptPattern := regexp.MustCompile(`PT(\d+)([HMS])`)
	matches := ptPattern.FindStringSubmatch(duration)
	if len(matches) == 3 {
		value, err := strconv.Atoi(matches[1])
		if err != nil {
			return 0, fmt.Errorf("invalid duration value: %s", matches[1])
		}

		unit := matches[2]
		switch unit {
		case "H":
			return time.Duration(value) * time.Hour, nil
		case "M":
			return time.Duration(value) * time.Minute, nil
		case "S":
			return time.Duration(value) * time.Second, nil
		}
	}

	// Паттерн для P1D, P7D и т.д.
	pPattern := regexp.MustCompile(`P(\d+)D`)
	matches = pPattern.FindStringSubmatch(duration)
	if len(matches) == 2 {
		value, err := strconv.Atoi(matches[1])
		if err != nil {
			return 0, fmt.Errorf("invalid duration value: %s", matches[1])
		}
		return time.Duration(value) * 24 * time.Hour, nil
	}

	return 0, fmt.Errorf("unsupported duration format: %s", duration)
}

