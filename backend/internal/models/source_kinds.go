package models

import "strings"

const (
	SourceKindRSS             = "rss"
	SourceKindTelegramChannel = "telegram_channel"
	SourceKindWeb             = "web"
	SourceKindHorizonFeed     = "horizon_monitor_feed"
	// SourceKindWorldMonitorLegacy is kept only for backward compatibility with old records.
	SourceKindWorldMonitorLegacy = "worldmonitor_feed"
)

// NormalizeSourceKind converts legacy aliases to the canonical kind.
func NormalizeSourceKind(kind string) string {
	k := strings.TrimSpace(strings.ToLower(kind))
	switch k {
	case SourceKindWorldMonitorLegacy:
		return SourceKindHorizonFeed
	default:
		return k
	}
}

// IsHorizonMonitorFeedKind returns true for canonical and legacy Horizon Monitor feed kinds.
func IsHorizonMonitorFeedKind(kind string) bool {
	k := NormalizeSourceKind(kind)
	return k == SourceKindHorizonFeed
}
