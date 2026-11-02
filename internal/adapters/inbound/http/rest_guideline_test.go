package http_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// raw sends one request and returns the status, the replay header and the
// exact body bytes.
func (r *rig) raw(t *testing.T, method, path, key, body string) (int, string, string) {
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
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Idempotency-Replayed"), string(b)
}

func TestReplayIsByteIdenticalAndMarked(t *testing.T) {
	r := newRig(t, nil)
	body := fmt.Sprintf(authBody, "tok_approve")
	status, replayed, first := r.raw(t, http.MethodPost, "/authorizations", "k", body)
	if status != http.StatusCreated || replayed != "" {
		t.Fatalf("first = %d replayed=%q", status, replayed)
	}
	status, replayed, again := r.raw(t, http.MethodPost, "/authorizations", "k", body)
	if status != http.StatusCreated || replayed != "true" || again != first {
		t.Fatalf("replay = %d replayed=%q body=%s want %s", status, replayed, again, first)
	}
}

func TestDeclineIsCachedAndReplayed(t *testing.T) {
	r := newRig(t, nil)
	body := fmt.Sprintf(authBody, "tok_decline")
	_, _, first := r.raw(t, http.MethodPost, "/authorizations", "k", body)
	status, replayed, again := r.raw(t, http.MethodPost, "/authorizations", "k", body)
	if status != http.StatusPaymentRequired || replayed != "true" || again != first {
		t.Fatalf("decline replay = %d replayed=%q body=%s", status, replayed, again)
	}
}

// The key is global: the same key on another route is a different request.
func TestKeyReusedOnAnotherRouteIs409(t *testing.T) {
	r := newRig(t, nil)
	_, hold := authorizeWith(t, r, "k", "tok_approve")
	status, out := r.post(t, "/authorizations/"+hold["id"].(string)+"/captures", "k", `{"amountMinor":1}`)
	if status != http.StatusConflict || out["type"] != "/problems/idempotency-key-reused" {
		t.Fatalf("cross-route reuse = %d %v", status, out)
	}
}

func TestMissingKeyIs400OnEveryPost(t *testing.T) {
	r := newRig(t, nil)
	_, hold := authorizeWith(t, r, "a", "tok_approve")
	base := "/authorizations/" + hold["id"].(string)
	for _, p := range []string{"/tokens", "/authorizations", base + "/voids", base + "/reauthorizations", base + "/reductions", base + "/captures", base + "/refunds"} {
		if status, out := r.post(t, p, "", `{"amountMinor":1}`); status != http.StatusBadRequest || out["type"] != "/problems/idempotency-key-required" {
			t.Errorf("%s without key = %d %v", p, status, out)
		}
	}
}

func TestOverlongKeyIsInvalid(t *testing.T) {
	r := newRig(t, nil)
	if status, out := r.post(t, "/tokens", strings.Repeat("k", 129), `{}`); status != http.StatusBadRequest || out["type"] != "/problems/idempotency-key-invalid" {
		t.Fatalf("overlong key = %d %v", status, out)
	}
}

func TestRetiredRoutesAreGone(t *testing.T) {
	r := newRig(t, nil)
	_, hold := authorizeWith(t, r, "a", "tok_approve")
	base := "/authorizations/" + hold["id"].(string)
	for _, old := range []string{"/void", "/reauthorize", "/reduce"} {
		t.Run(old, func(t *testing.T) {
			status, _, _ := r.raw(t, http.MethodPost, base+old, "k-"+old, `{"amountMinor":1}`)
			if status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
				t.Fatalf("POST %s = %d, want 404 or 405", old, status)
			}
		})
	}
}
