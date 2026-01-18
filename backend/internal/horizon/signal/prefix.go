package signal

// This package is a tiny "namespace anchor" for the Signal Propagation System
// described in risk-architecture.md.
//
// It intentionally contains only stable identifiers/constants that should be shared
// across extract/seed/propagate/trigger/interpret layers to avoid drift.

const (
	// Prefix is the universal namespace for the signal propagation system (risk + opportunity).
	Prefix = "horizon.signal"

	// Log* are canonical log prefixes used across the system.
	LogInject     = "[horizon.signal.inject]"
	LogExtract    = "[horizon.signal.extract]"
	LogSeed       = "[horizon.signal.seed]"
	LogPropagate  = "[horizon.signal.propagate]"
	LogPipeline   = "[horizon.signal.pipeline]"
	LogTrigger    = "[horizon.signal.trigger]"
	LogQueue      = "[horizon.signal.queue]"
	LogInterpret  = "[horizon.signal.interpret]"
	LogCache      = "[horizon.signal.cache]"
	LogAPI        = "[horizon.signal.api]"
)

