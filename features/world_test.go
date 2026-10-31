package features_test

import (
	"log/slog"

	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	inbound "github.com/IQVO/payment-provider-simulator/internal/adapters/inbound/http"
	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/memory"
	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
)

var t0 = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

// world is the state of one scenario.
type world struct {
	clock *memory.Clock
	srv   *httptest.Server
	keys  int

	token   string
	holdID  string
	newHold string
	status  int
	body    map[string]any
	answers []map[string]any
}

func newWorld() *world {
	s := memory.NewStore()
	clock := memory.NewClock(t0)
	d := usecases.Deps{
		UoW: memory.NewUnitOfWork(s), Authorizations: memory.NewAuthorizations(s), Idempotency: memory.NewIdempotency(s),
		Clock: clock, IDs: &memory.IDs{}, TimeoutDelay: 5 * time.Millisecond,
	}
	api := &inbound.Server{
		IssueToken: usecases.IssueToken{Deps: d}, Authorize: usecases.Authorize{Deps: d},
		GetAuthorization: usecases.GetAuthorization{Deps: d}, Void: usecases.Void{Deps: d},
		Reauthorize: usecases.Reauthorize{Deps: d}, Reduce: usecases.Reduce{Deps: d},
		Capture: usecases.Capture{Deps: d}, Refund: usecases.Refund{Deps: d}, Readiness: inbound.NewReadiness(0, 0),
	}
	return &world{clock: clock, srv: httptest.NewServer(inbound.NewRouter(api, slog.Default(), ""))}
}

// do sends one request, recording status and body; key "" on a POST sends a
// fresh Idempotency-Key.
func (w *world) do(method, path, key, body string) error {
	req, err := http.NewRequest(method, w.srv.URL+path, strings.NewReader(body))
	if err != nil {
		return err
	}
	if method == http.MethodPost {
		if key == "" {
			w.keys++
			key = "key-" + strconv.Itoa(w.keys)
		}
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	w.status, w.body = resp.StatusCode, map[string]any{}
	_ = json.Unmarshal(raw, &w.body)
	w.answers = append(w.answers, w.body)
	return nil
}
