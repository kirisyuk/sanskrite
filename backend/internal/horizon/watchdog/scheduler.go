package watchdog

import (
	"fmt"
	"strconv"
	"strings"
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
	duration, err := s.parseDuration(interval)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse interval: %w", err)
	}
	if duration <= 0 {
		return time.Time{}, fmt.Errorf("interval must be positive")
	}
	return fromTime.Add(duration), nil
}

// parseDuration accepts:
// - ISO-8601 subsets: PnD, PTnH, PTnM, PTnS, PTnHnM, PTnHnMnS, PnDTnHnMnS
// - Go durations: 1h30m, 45m, 10s (fallback for operational convenience)
func (s *Scheduler) parseDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty interval")
	}
	upper := strings.ToUpper(raw)
	if strings.HasPrefix(upper, "P") {
		return parseISO8601Duration(upper)
	}
	// Operational fallback: allow Go duration literals.
	if d, err := time.ParseDuration(strings.ToLower(raw)); err == nil {
		return d, nil
	}
	return 0, fmt.Errorf("unsupported duration format: %s", raw)
}

func parseISO8601Duration(duration string) (time.Duration, error) {
	if duration == "" || duration[0] != 'P' {
		return 0, fmt.Errorf("unsupported duration format: %s", duration)
	}
	rest := duration[1:]
	if rest == "" {
		return 0, fmt.Errorf("unsupported duration format: %s", duration)
	}

	var days, hours, minutes, seconds int
	inTime := false
	consumed := false
	num := strings.Builder{}
	flush := func(unit byte) error {
		if num.Len() == 0 {
			return fmt.Errorf("missing value for unit %c in %s", unit, duration)
		}
		v, err := strconv.Atoi(num.String())
		if err != nil {
			return fmt.Errorf("invalid duration value %q in %s", num.String(), duration)
		}
		num.Reset()
		switch unit {
		case 'D':
			if inTime {
				return fmt.Errorf("day unit not allowed in time section: %s", duration)
			}
			days += v
		case 'H':
			if !inTime {
				return fmt.Errorf("hour unit requires time section: %s", duration)
			}
			hours += v
		case 'M':
			if !inTime {
				return fmt.Errorf("minute unit requires time section: %s", duration)
			}
			minutes += v
		case 'S':
			if !inTime {
				return fmt.Errorf("second unit requires time section: %s", duration)
			}
			seconds += v
		default:
			return fmt.Errorf("unsupported ISO unit %c in %s", unit, duration)
		}
		consumed = true
		return nil
	}

	for i := 0; i < len(rest); i++ {
		ch := rest[i]
		switch {
		case ch >= '0' && ch <= '9':
			num.WriteByte(ch)
		case ch == 'T':
			if inTime {
				return 0, fmt.Errorf("duplicate time section in %s", duration)
			}
			inTime = true
		case ch == 'D' || ch == 'H' || ch == 'M' || ch == 'S':
			if err := flush(ch); err != nil {
				return 0, err
			}
		default:
			return 0, fmt.Errorf("unsupported ISO duration token %q in %s", string(ch), duration)
		}
	}
	if num.Len() > 0 {
		return 0, fmt.Errorf("dangling numeric value in %s", duration)
	}
	if !consumed {
		return 0, fmt.Errorf("unsupported duration format: %s", duration)
	}
	d := time.Duration(days)*24*time.Hour +
		time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second
	return d, nil
}
