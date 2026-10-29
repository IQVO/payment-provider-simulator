package main

import (
	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/telemetry"

	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
)

// errMissingDatabaseURL is the boot error for the variable the service cannot
// run without.
var errMissingDatabaseURL = errors.New("DATABASE_URL is required: this service runs on real Postgres, there is no in-memory mode")

// MaxTimeoutDelay caps the timeout scenario's stall so consumer tests stay fast.
const MaxTimeoutDelay = 2 * time.Second

// runtimeEnv is the validated part of the environment.
type runtimeEnv struct {
	databaseURL string
}

// loadRuntime validates the environment before anything is opened or bound.
func loadRuntime(env func(string) string) (runtimeEnv, error) {
	rt := runtimeEnv{databaseURL: env("DATABASE_URL")}
	if rt.databaseURL == "" {
		return runtimeEnv{}, errMissingDatabaseURL
	}
	return rt, nil
}

// timeoutDelay reads PROVIDER_TIMEOUT_DELAY (Go duration): how long a
// timeout-scenario authorization stalls before its 504. Unset, malformed or
// negative = the default; above MaxTimeoutDelay = MaxTimeoutDelay; "0" = none.
func timeoutDelay(raw string, logger *slog.Logger) time.Duration {
	if raw == "" {
		return usecases.DefaultTimeoutDelay
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		logger.Warn("invalid PROVIDER_TIMEOUT_DELAY; using the default", "value", raw, "default", usecases.DefaultTimeoutDelay)
		return usecases.DefaultTimeoutDelay
	}
	return min(d, MaxTimeoutDelay)
}

// shutdownDrainDelay reads SHUTDOWN_DRAIN_DELAY ("0" disables; unset, negative
// or unparsable = the default).
func shutdownDrainDelay(raw string, logger *slog.Logger) time.Duration {
	if raw == "" {
		return DefaultShutdownDrainDelay
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		logger.Warn("ignoring invalid SHUTDOWN_DRAIN_DELAY", "value", raw, "default", DefaultShutdownDrainDelay.String())
		return DefaultShutdownDrainDelay
	}
	return d
}

// newLogger builds the JSON logger; LOG_LEVEL debug|info|warn|warning|error.
func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	// WithTraceContext stamps trace_id/span_id on records written with a
	// span-carrying context (ADR 0003).
	return slog.New(telemetry.WithTraceContext(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
