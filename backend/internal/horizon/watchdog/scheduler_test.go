package watchdog

import (
	"testing"
	"time"
)

func TestSchedulerParseDuration(t *testing.T) {
	s := NewScheduler()
	cases := []struct {
		name      string
		in        string
		want      time.Duration
		shouldErr bool
	}{
		{name: "iso_hour", in: "PT1H", want: time.Hour},
		{name: "iso_minutes", in: "PT30M", want: 30 * time.Minute},
		{name: "iso_day", in: "P1D", want: 24 * time.Hour},
		{name: "iso_compound_time", in: "PT1H30M", want: 90 * time.Minute},
		{name: "iso_compound_day_time", in: "P1DT2H15M10S", want: 26*time.Hour + 15*time.Minute + 10*time.Second},
		{name: "go_duration", in: "1h30m", want: 90 * time.Minute},
		{name: "invalid_empty", in: "", shouldErr: true},
		{name: "invalid_iso", in: "PT", shouldErr: true},
		{name: "invalid_token", in: "P1Y", shouldErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.parseDuration(tc.in)
			if tc.shouldErr {
				if err == nil {
					t.Fatalf("expected error, got duration=%s", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("duration mismatch: got=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestSchedulerCalculateNextScanTime(t *testing.T) {
	s := NewScheduler()
	from := time.Date(2026, 2, 10, 10, 0, 0, 0, time.UTC)

	next, err := s.CalculateNextScanTime("PT1H30M", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := from.Add(90 * time.Minute)
	if !next.Equal(want) {
		t.Fatalf("next scan mismatch: got=%s want=%s", next.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}
