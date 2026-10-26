package main

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestLoadRuntimeRequiresDatabaseURL(t *testing.T) {
	if _, err := loadRuntime(func(string) string { return "" }); !errors.Is(err, errMissingDatabaseURL) {
		t.Fatalf("err = %v", err)
	}
	rt, err := loadRuntime(func(k string) string { return map[string]string{"DATABASE_URL": "postgres://x"}[k] })
	if err != nil || rt.databaseURL != "postgres://x" {
		t.Fatalf("rt = %+v, %v", rt, err)
	}
}

func TestTimeoutDelay(t *testing.T) {
	cases := map[string]time.Duration{
		"": usecases.DefaultTimeoutDelay, "250ms": 250 * time.Millisecond, "0": 0,
		"10s": MaxTimeoutDelay, "2s": 2 * time.Second, "-1s": usecases.DefaultTimeoutDelay, "junk": usecases.DefaultTimeoutDelay,
	}
	for raw, want := range cases {
		if got := timeoutDelay(raw, quiet); got != want {
			t.Errorf("timeoutDelay(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestShutdownDrainDelay(t *testing.T) {
	cases := map[string]time.Duration{"": DefaultShutdownDrainDelay, "0": 0, "2s": 2 * time.Second, "-1s": DefaultShutdownDrainDelay, "x": DefaultShutdownDrainDelay}
	for raw, want := range cases {
		if got := shutdownDrainDelay(raw, quiet); got != want {
			t.Errorf("shutdownDrainDelay(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestLoggerLevels(t *testing.T) {
	for _, lvl := range []string{"debug", "WARN", "warning", "error", "", "bogus"} {
		if newLogger(lvl) == nil {
			t.Fatalf("no logger for %q", lvl)
		}
	}
	if getenv("PPS_SURELY_UNSET", "fb") != "fb" {
		t.Fatal("getenv fallback")
	}
	t.Setenv("PPS_SET", "v")
	if getenv("PPS_SET", "fb") != "v" {
		t.Fatal("getenv value")
	}
}

func TestRunRefusesMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := run(t.Context()); !errors.Is(err, errMissingDatabaseURL) {
		t.Fatalf("err = %v", err)
	}
}
