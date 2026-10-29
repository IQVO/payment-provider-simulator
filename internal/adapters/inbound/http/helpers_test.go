package http_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	inbound "github.com/IQVO/payment-provider-simulator/internal/adapters/inbound/http"
	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/memory"
	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
)

var t0 = time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

type rig struct {
	srv   *httptest.Server
	clock *memory.Clock
}

func newRig(t *testing.T, readiness *inbound.Readiness) *rig {
	t.Helper()
	s := memory.NewStore()
	c := memory.NewClock(t0)
	d := usecases.Deps{
		UoW: memory.NewUnitOfWork(s), Authorizations: memory.NewAuthorizations(s),
		Idempotency: memory.NewIdempotency(s), Clock: c, IDs: &memory.IDs{}, TimeoutDelay: 10 * time.Millisecond,
	}
	h := inbound.NewRouter(&inbound.Server{
		IssueToken: usecases.IssueToken{Deps: d}, Authorize: usecases.Authorize{Deps: d},
		GetAuthorization: usecases.GetAuthorization{Deps: d}, Void: usecases.Void{Deps: d},
		Reauthorize: usecases.Reauthorize{Deps: d}, Reduce: usecases.Reduce{Deps: d},
		Capture: usecases.Capture{Deps: d}, Refund: usecases.Refund{Deps: d}, Readiness: readiness,
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &rig{srv: srv, clock: c}
}

// do sends one request and returns the status, the content type and the
// decoded JSON object.
func (r *rig) do(t *testing.T, method, path, key, body string) (int, string, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, r.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, resp.Header.Get("Content-Type"), out
}

func (r *rig) post(t *testing.T, path, key, body string) (int, map[string]any) {
	t.Helper()
	status, _, out := r.do(t, http.MethodPost, path, key, body)
	return status, out
}

const authBody = `{"amountMinor":1000,"currency":"EUR","paymentMethodToken":"%s","reference":"cs-1"}`
