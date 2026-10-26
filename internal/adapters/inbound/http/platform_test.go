package http_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	inbound "github.com/IQVO/payment-provider-simulator/internal/adapters/inbound/http"
)

func TestNULIsAClientError(t *testing.T) {
	r := newRig(t, nil)
	if status, out := r.post(t, "/authorizations", "k", `{"reference":"a\u0000b"}`); status != http.StatusBadRequest || out["type"] != "/problems/malformed-request" {
		t.Fatalf("NUL body = %d %v", status, out)
	}
	if status, _, _ := r.do(t, http.MethodGet, "/authorizations/a%00b", "", ""); status != http.StatusNotFound {
		t.Fatalf("NUL path = %d", status)
	}
	if status, _, _ := r.do(t, http.MethodGet, "/healthz?x=%00", "", ""); status != http.StatusBadRequest {
		t.Fatalf("NUL query = %d", status)
	}
}

func TestHealthAndReadiness(t *testing.T) {
	var down bool
	gate := inbound.NewReadiness(time.Nanosecond, time.Second, inbound.Dependency{Name: "postgres", Check: func(context.Context) error {
		if down {
			return errors.New("boom")
		}
		return nil
	}})
	r := newRig(t, gate)
	if status, _, _ := r.do(t, http.MethodGet, "/healthz", "", ""); status != http.StatusOK {
		t.Fatalf("healthz = %d", status)
	}
	if status, _, out := r.do(t, http.MethodGet, "/readyz", "", ""); status != http.StatusOK || out["status"] != "ready" {
		t.Fatalf("readyz = %d %v", status, out)
	}
	down = true
	time.Sleep(2 * time.Millisecond)
	if status, _, out := r.do(t, http.MethodGet, "/readyz", "", ""); status != http.StatusServiceUnavailable || out["type"] != "/problems/dependency-unavailable" {
		t.Fatalf("readyz with postgres down = %d %v", status, out)
	}
	gate.SetNotReady()
	if status, _, out := r.do(t, http.MethodGet, "/readyz", "", ""); status != http.StatusServiceUnavailable || out["status"] != "not_ready" {
		t.Fatalf("draining readyz = %d %v", status, out)
	}
}
