// Command api is payment-provider-simulator's composition root: it wires env
// config into the Postgres adapters, the adapters into use cases and the use
// cases into the HTTP router. It runs on real Postgres only (there is no
// in-memory mode) and neither publishes nor consumes Kafka events (ADR 0002).
//
// Environment:
//
//	HTTP_ADDR                listen address (default :8080)
//	DATABASE_URL             Postgres DSN (REQUIRED)
//	MIGRATIONS_DATABASE_URL  direct DSN for the boot migrations (default DATABASE_URL)
//	PROVIDER_TIMEOUT_DELAY   stall of a timeout-scenario authorization (default 1s, max 2s)
//	SHUTDOWN_DRAIN_DELAY     wait after flipping /readyz before closing (default 5s)
//	LOG_LEVEL
//
// GET /readyz is 200 only while Postgres answers a ping (verdict cached 3s,
// bounded 2s) and the process is not draining; /healthz is liveness only.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	inboundhttp "github.com/IQVO/payment-provider-simulator/internal/adapters/inbound/http"
	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/clock"
	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/ids"
	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/postgres"
	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
	"github.com/IQVO/payment-provider-simulator/internal/bootretry"
)

// shutdownTimeout bounds the HTTP server's graceful drain.
const shutdownTimeout = 10 * time.Second

// DefaultShutdownDrainDelay is how long shutdown waits, after flipping /readyz
// to not-ready, before closing the listener.
const DefaultShutdownDrainDelay = 5 * time.Second

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("service exited with error", "error", err)
		os.Exit(1)
	}
}

// run boots the service and serves until SIGINT/SIGTERM or until ctx is
// cancelled (the integration test stops the real entry point that way).
func run(ctx context.Context) error {
	logger := newLogger(getenv("LOG_LEVEL", "info"))
	slog.SetDefault(logger)

	rt, err := loadRuntime(os.Getenv)
	if err != nil {
		return err
	}
	pool, err := openPool(ctx, rt, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	readiness := inboundhttp.NewReadiness(inboundhttp.DefaultReadinessTTL, inboundhttp.DefaultReadinessTimeout,
		inboundhttp.Dependency{Name: "postgres", Check: pool.Ping})
	httpServer := &http.Server{
		Addr:              getenv("HTTP_ADDR", ":8080"),
		Handler:           inboundhttp.NewRouter(buildServer(pool, timeoutDelay(os.Getenv("PROVIDER_TIMEOUT_DELAY"), logger), readiness)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return serveUntilSignal(ctx, logger, httpServer, readiness)
}

// openPool runs the embedded migrations and opens the pool. The first outbound
// dial of an injected pod can be reset, so both retry with backoff.
func openPool(ctx context.Context, rt runtimeEnv, logger *slog.Logger) (*pgxpool.Pool, error) {
	migrationsURL := getenv("MIGRATIONS_DATABASE_URL", rt.databaseURL)
	if err := bootretry.Retry(ctx, logger, "run migrations", func() error { return postgres.RunMigrations(migrationsURL) }); err != nil {
		return nil, err
	}
	pool, err := postgres.NewPool(ctx, rt.databaseURL)
	if err != nil {
		return nil, err
	}
	if err := bootretry.Retry(ctx, logger, "ping postgres", func() error { return pool.Ping(ctx) }); err != nil {
		pool.Close()
		return nil, err
	}
	logger.Info("postgres adapters configured")
	return pool, nil
}

func buildServer(pool *pgxpool.Pool, delay time.Duration, readiness *inboundhttp.Readiness) *inboundhttp.Server {
	d := usecases.Deps{
		UoW: postgres.NewUnitOfWork(pool), Authorizations: postgres.NewAuthorizationRepository(pool),
		Idempotency: postgres.NewIdempotencyStore(pool), Clock: clock.System{}, IDs: ids.Generator{}, TimeoutDelay: delay,
	}
	return &inboundhttp.Server{
		IssueToken: usecases.IssueToken{Deps: d}, Authorize: usecases.Authorize{Deps: d},
		GetAuthorization: usecases.GetAuthorization{Deps: d}, Void: usecases.Void{Deps: d},
		Reauthorize: usecases.Reauthorize{Deps: d}, Reduce: usecases.Reduce{Deps: d},
		Capture: usecases.Capture{Deps: d}, Refund: usecases.Refund{Deps: d}, Readiness: readiness,
	}
}

// serveUntilSignal runs httpServer until SIGINT/SIGTERM (or a listen error),
// then shuts down in the fleet order: /readyz flips to 503, the drain delay
// passes, HTTP drains; the caller's deferred pool.Close runs last.
func serveUntilSignal(ctx context.Context, logger *slog.Logger, httpServer *http.Server, readiness *inboundhttp.Readiness) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-errCh:
		return err
	case <-sigCtx.Done():
	}
	readiness.SetNotReady()
	if delay := shutdownDrainDelay(os.Getenv("SHUTDOWN_DRAIN_DELAY"), logger); delay > 0 {
		logger.Info("shutdown: readiness flipped to not-ready; waiting for traffic to drain", "drain_delay", delay.String())
		time.Sleep(delay)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
