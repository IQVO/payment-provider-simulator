// internal/adapters/inbound/http/idempotency_replay_classes_wiring_test.go —
// the repo-specific half of the idempotency replay-class reference test
// (idempotency_replay_classes_test.go is the verbatim harness template).
//
// This service has NO storing HTTP middleware: every POST runs the keyed-run
// engine usecases.Deps.Once — Claim, effect and stored answer in ONE unit of
// work; a released key is that transaction rolled back. So Middleware wraps
// the fake handler with the production key check around the production Once
// engine over the memory doubles of the store ports (the store sits behind
// ports; docker-free), Refuse goes through the production writers, Write
// stands for the aggregate write a real effect makes (it joins the unit's
// context and is rolled back with it), and Committed/KeyRows count from
// outside any unit.
package http_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	inbound "github.com/IQVO/payment-provider-simulator/internal/adapters/inbound/http"
	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/memory"
	"github.com/IQVO/payment-provider-simulator/internal/application/usecases"
	"github.com/IQVO/payment-provider-simulator/internal/application/views"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providerauth"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providertoken"
)

// newReplayOps returns the six operations over ONE clean store (a fresh memory
// store per call). The classes come from the production catalogue; the probe
// errors come from the production domain and use cases.
func newReplayOps(t *testing.T) replayOps {
	t.Helper()
	store := memory.NewStore()
	deps := usecases.Deps{
		UoW: memory.NewUnitOfWork(store), Authorizations: memory.NewAuthorizations(store),
		Idempotency: memory.NewIdempotency(store), Clock: memory.NewClock(t0), IDs: &memory.IDs{},
		Refusals: inbound.NewRefusals(),
	}
	auths := memory.NewAuthorizations(store)

	// probe5xx stands for an unmapped failure (the fake handler answered a
	// 5xx): no catalogue entry matches, so Once releases it like RETRYABLE —
	// the fleet's safe fallback — and the production writer renders it as the
	// internal-error problem.
	probe5xx := errors.New("probe: the handler answered a 5xx")

	// refusal carries what Refuse answered back into the effect translation.
	// The reference test drives one request at a time, so one slot is enough.
	var refusal struct {
		err     error
		answers bool
		answer  views.Answer
	}

	entries := map[string]inbound.ReplayCatalogueEntry{}
	for _, e := range inbound.ReplayCatalogue() {
		entries[e.Slug] = e
	}

	return replayOps{
		Catalogue: func() []problemEntry {
			out := make([]problemEntry, 0, len(entries))
			for _, e := range inbound.ReplayCatalogue() {
				out = append(out, problemEntry{Slug: e.Slug, Status: e.Status, Class: string(e.Class)})
			}
			return out
		},
		Middleware: func(_ *testing.T, next http.Handler) http.Handler {
			return inbound.RequireIdempotencyKeyForTest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				defer func() { refusal.err, refusal.answers, refusal.answer = nil, false, views.Answer{} }()
				answer, err := deps.Once(r.Context(), "replay", r.Header.Get(idempotencyKeyHeader), string(body),
					func(ctx context.Context) (views.Answer, error) {
						rec := httptest.NewRecorder()
						next.ServeHTTP(rec, r.WithContext(ctx))
						switch {
						case refusal.err != nil:
							return views.Answer{}, refusal.err
						case refusal.answers:
							return refusal.answer, nil
						case rec.Code >= http.StatusInternalServerError:
							return views.Answer{}, probe5xx
						default:
							// A success: KindHold is the answer kind the
							// router answers 201 for.
							return views.Answer{
								Kind: views.KindHold, ID: "hold_probe", ExpiresAt: t0.Add(7 * 24 * time.Hour),
								AmountMinor: 1, Currency: "EUR",
							}, nil
						}
					})
				inbound.WriteAnswerForTest(w, r, answer, err)
			}))
		},
		Refuse: func(w http.ResponseWriter, r *http.Request, e problemEntry) {
			ent := entries[e.Slug]
			if ent.Err != nil {
				refusal.err = ent.Err
				inbound.WriteErrorForTest(w, r, ent.Err)
				return
			}
			switch e.Slug {
			case "payment-declined":
				// Production stores the decline as an Answer (KindDeclined),
				// not an error: ADR 0004, the behaviour customer-payments
				// depends on.
				refusal.answers, refusal.answer = true, views.Answer{Kind: views.KindDeclined, Code: providertoken.DeclineCode}
			default:
				// A FINAL/OUTCOME problem the adapter writes without a typed
				// error: the effect hands back a refusal answer, which Once
				// stores like any answer.
				refusal.answers = true
				refusal.answer = views.Answer{Kind: views.KindProblem, Slug: e.Slug, Detail: ent.Detail}
			}
			inbound.RefuseSlugForTest(w, r, e.Slug, ent.Detail)
		},
		Write: func(ctx context.Context, tag string) error {
			a, err := providerauth.Authorize(providerauth.ID("hold_probe_"+tag), providertoken.Approve, tag, "EUR", 1, t0)
			if err != nil {
				return err
			}
			return auths.Save(ctx, a)
		},
		Committed: func(t *testing.T, tag string) int {
			t.Helper()
			if _, err := auths.Get(context.Background(), providerauth.ID("hold_probe_"+tag)); err == nil {
				return 1
			}
			return 0
		},
		KeyRows: func(t *testing.T, key string) int {
			t.Helper()
			return store.Held(key)
		},
	}
}
