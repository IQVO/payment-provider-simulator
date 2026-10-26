package http_test

import (
	"fmt"
	"net/http"
	"testing"
)

func authorizeWith(t *testing.T, r *rig, key, token string) (int, map[string]any) {
	t.Helper()
	return r.post(t, "/authorizations", key, fmt.Sprintf(authBody, token))
}

func TestTokenIssuance(t *testing.T) {
	r := newRig(t, nil)
	status, out := r.post(t, "/tokens", "k1", `{"scenario":"expire-early"}`)
	if status != http.StatusCreated || out["token"] != "tok_expire-early_t1" || out["scenario"] != "expire-early" {
		t.Fatalf("token = %d %v", status, out)
	}
	if status, out = r.post(t, "/tokens", "k2", ``); status != http.StatusCreated || out["scenario"] != "approve" {
		t.Fatalf("default scenario = %d %v", status, out)
	}
	if status, _ = r.post(t, "/tokens", "k3", `{"scenario":"nope"}`); status != http.StatusUnprocessableEntity {
		t.Fatalf("bad scenario status = %d", status)
	}
	if status, _ = r.post(t, "/tokens", "k4", `{"scenario":"approve","pan":"4111"}`); status != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", status)
	}
	if status, _ = r.post(t, "/tokens", "", `{}`); status != http.StatusBadRequest {
		t.Fatalf("missing key status = %d", status)
	}
}

func TestAuthorizeIsIdempotentOverHTTP(t *testing.T) {
	r := newRig(t, nil)
	status, first := authorizeWith(t, r, "k", "tok_approve")
	if status != http.StatusCreated || first["id"] != "hold_1" || first["expiresAt"] != "2026-11-02T09:00:00Z" {
		t.Fatalf("authorize = %d %v", status, first)
	}
	if _, again := authorizeWith(t, r, "k", "tok_approve"); again["id"] != first["id"] {
		t.Fatalf("replay = %v", again)
	}
	if status, out := r.post(t, "/authorizations", "k", `{"amountMinor":5,"currency":"EUR","paymentMethodToken":"tok_approve","reference":"cs-1"}`); status != http.StatusUnprocessableEntity || out["type"] != "/problems/idempotency-key-reused" {
		t.Fatalf("reused key = %d %v", status, out)
	}
}

func TestDeclineIs402ProblemWithCode(t *testing.T) {
	r := newRig(t, nil)
	status, ctype, out := r.do(t, http.MethodPost, "/authorizations", "k", fmt.Sprintf(authBody, "tok_decline"))
	if status != http.StatusPaymentRequired || ctype != "application/problem+json" || out["code"] != "CARD_DECLINED" || out["type"] != "/problems/payment-declined" {
		t.Fatalf("decline = %d %s %v", status, ctype, out)
	}
	if again, _ := authorizeWith(t, r, "k", "tok_decline"); again != http.StatusPaymentRequired {
		t.Fatalf("decline replay = %d", again)
	}
}

func TestTimeoutIs504(t *testing.T) {
	r := newRig(t, nil)
	status, out := authorizeWith(t, r, "k", "tok_timeout")
	if status != http.StatusGatewayTimeout || out["type"] != "/problems/provider-timeout" {
		t.Fatalf("timeout = %d %v", status, out)
	}
}

func TestExpireEarlyHoldExpiresInMinutes(t *testing.T) {
	r := newRig(t, nil)
	if _, out := authorizeWith(t, r, "k", "tok_expire-early"); out["expiresAt"] != "2026-10-26T09:05:00Z" {
		t.Fatalf("hold = %v", out)
	}
}

func TestCardNumbersAndGarbageAreRejected(t *testing.T) {
	r := newRig(t, nil)
	for _, token := range []string{"4111111111111111", "x"} {
		if status, _ := authorizeWith(t, r, "k", token); status != http.StatusUnprocessableEntity {
			t.Errorf("token %q status = %d", token, status)
		}
	}
	if status, _ := r.post(t, "/authorizations", "k", `{"amountMinor":"x"}`); status != http.StatusBadRequest {
		t.Fatalf("bad body status = %d", status)
	}
}
