package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/telemetry"
)

// defaultServiceName is the OTEL_SERVICE_NAME default: the name every span,
// metric and log line of this binary carries.
const defaultServiceName = "payment-provider-simulator"

// telemetryFlushTimeout bounds the final export attempt.
const telemetryFlushTimeout = 5 * time.Second

// startTelemetry brings OpenTelemetry up and returns the flush func to defer.
// It must run before the HTTP router is built: otelchimetric binds the global
// MeterProvider at construction.
func startTelemetry(logger *slog.Logger) (func(), error) {
	flushed, err := telemetry.Setup(context.Background(),
		getenv("OTEL_SERVICE_NAME", defaultServiceName),
		getenv("SERVICE_VERSION", telemetry.DefaultServiceVersion),
		os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if err != nil {
		return nil, err
	}
	// A failed flush is logged, never returned: turning it into a non-zero
	// exit would make every clean shutdown look like a crash.
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), telemetryFlushTimeout)
		defer cancel()
		if err := flushed(ctx); err != nil {
			logger.Warn("telemetry flush failed on shutdown", "error", err)
		}
	}, nil
}
