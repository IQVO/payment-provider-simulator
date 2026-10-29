//go:build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/testing/pgtest"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().String()
}

func call(t *testing.T, method, url, key, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
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
	return resp.StatusCode, out
}

func waitReady(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(base + "/readyz"); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("service never became ready")
}

// TestRealBinaryServesTheProviderContract boots the real entry point against a
// real Postgres and drives the A12 contract through it.
func TestRealBinaryServesTheProviderContract(t *testing.T) {
	addr := freeAddr(t)
	t.Setenv("DATABASE_URL", pgtest.URL(t))
	t.Setenv("HTTP_ADDR", addr)
	t.Setenv("SHUTDOWN_DRAIN_DELAY", "0")
	t.Setenv("PROVIDER_TIMEOUT_DELAY", "50ms")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	base := "http://" + addr
	waitReady(t, base)

	_, tok := call(t, http.MethodPost, base+"/tokens", "t1", `{"scenario":"approve"}`)
	token := tok["token"].(string)
	body := fmt.Sprintf(`{"amountMinor":1000,"currency":"EUR","paymentMethodToken":%q,"reference":"cs-1"}`, token)
	status, hold := call(t, http.MethodPost, base+"/authorizations", "a1", body)
	if status != http.StatusCreated {
		t.Fatalf("authorize = %d %v", status, hold)
	}
	if _, again := call(t, http.MethodPost, base+"/authorizations", "a1", body); again["id"] != hold["id"] {
		t.Fatalf("same key must answer the same hold: %v vs %v", again, hold)
	}
	path := base + "/authorizations/" + hold["id"].(string)
	if status, _ = call(t, http.MethodPost, path+"/captures", "c1", `{"amountMinor":1000}`); status != http.StatusCreated {
		t.Fatalf("capture = %d", status)
	}
	if status, _ = call(t, http.MethodPost, path+"/refunds", "r1", `{"amountMinor":1001}`); status != http.StatusConflict {
		t.Fatalf("over refund = %d", status)
	}
	if status, _ = call(t, http.MethodPost, base+"/authorizations", "a2", strings.Replace(body, token, "tok_decline", 1)); status != http.StatusPaymentRequired {
		t.Fatalf("decline = %d", status)
	}
	start := time.Now()
	if status, _ = call(t, http.MethodPost, base+"/authorizations", "a3", strings.Replace(body, token, "tok_timeout", 1)); status != http.StatusGatewayTimeout || time.Since(start) < 50*time.Millisecond {
		t.Fatalf("timeout = %d after %v", status, time.Since(start))
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not stop")
	}
}
