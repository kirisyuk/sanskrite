package watchdog

import "log"

// logf writes a prefixed log line to make Watchdog decisions easy to filter in Cloud Logging.
func logf(format string, args ...any) {
	log.Printf("Watchdog: "+format, args...)
}



