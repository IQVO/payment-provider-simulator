package http_test

import (
	"net/http"
	"testing"
)

type step struct {
	path, key, body string
	status          int
	problem         string
}

func TestHoldOperationsEndToEnd(t *testing.T) {
	r := newRig(t, nil)
	_, hold := authorizeWith(t, r, "a", "tok_approve")
	id := hold["id"].(string)
	base := "/authorizations/" + id
	steps := []step{
		{"/captures", "c1", `{"amountMinor":600}`, http.StatusCreated, ""},
		{"/captures", "c2", `{"amountMinor":401}`, http.StatusConflict, "over-capture"},
		{"/refunds", "r1", `{"amountMinor":100}`, http.StatusCreated, ""},
		{"/refunds", "r2", `{"amountMinor":501}`, http.StatusConflict, "over-refund"},
		{"/reduce", "d1", `{"amountMinor":200}`, http.StatusOK, ""},
		{"/reduce", "d2", `{"amountMinor":900}`, http.StatusUnprocessableEntity, "not-a-reduction"},
		{"/void", "v1", `{}`, http.StatusOK, ""},
		{"/void", "v2", ``, http.StatusConflict, "illegal-transition"},
	}
	for _, st := range steps {
		status, out := r.post(t, base+st.path, st.key, st.body)
		if status != st.status {
			t.Fatalf("%s %s = %d %v, want %d", st.path, st.key, status, out, st.status)
		}
		if st.problem != "" && out["type"] != "/problems/"+st.problem {
			t.Fatalf("%s %s type = %v, want %s", st.path, st.key, out["type"], st.problem)
		}
	}
	_, _, got := r.do(t, http.MethodGet, base, "", "")
	if got["status"] != "VOIDED" || got["capturedMinor"] != float64(600) || got["refundedMinor"] != float64(100) || len(got["movements"].([]any)) != 2 {
		t.Fatalf("hold = %v", got)
	}
}

func TestReauthorizeAndExpiry(t *testing.T) {
	r := newRig(t, nil)
	_, hold := authorizeWith(t, r, "a", "tok_expire-early")
	base := "/authorizations/" + hold["id"].(string)
	r.clock.Advance(4 * 60 * 1e9)
	status, next := r.post(t, base+"/reauthorize", "re", `{"amountMinor":700}`)
	if status != http.StatusCreated || next["id"] == hold["id"] || next["amountMinor"] != float64(700) {
		t.Fatalf("reauthorize = %d %v", status, next)
	}
	r.clock.Advance(10 * 60 * 1e9)
	if status, out := r.post(t, "/authorizations/"+next["id"].(string)+"/captures", "c", `{"amountMinor":1}`); status != http.StatusConflict || out["type"] != "/problems/hold-expired" {
		t.Fatalf("capture after expiry = %d %v", status, out)
	}
}

func TestUnknownHoldAndMalformedOperations(t *testing.T) {
	r := newRig(t, nil)
	if status, out := r.post(t, "/authorizations/nope/captures", "k", `{"amountMinor":1}`); status != http.StatusNotFound || out["type"] != "/problems/not-found" {
		t.Fatalf("unknown hold = %d %v", status, out)
	}
	if status, _, _ := r.do(t, http.MethodGet, "/authorizations/nope", "", ""); status != http.StatusNotFound {
		t.Fatalf("get unknown = %d", status)
	}
	_, hold := authorizeWith(t, r, "a", "tok_approve")
	if status, _ := r.post(t, "/authorizations/"+hold["id"].(string)+"/captures", "k", `{"amountMinor":1} {}`); status != http.StatusBadRequest {
		t.Fatalf("trailing data = %d", status)
	}
	if status, _ := r.post(t, "/authorizations/"+hold["id"].(string)+"/void", "k", `{"x":1}`); status != http.StatusBadRequest {
		t.Fatalf("void with fields = %d", status)
	}
}
