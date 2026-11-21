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

// refusalState carries what Refuse answered back into the effect translation.
// The reference test drives one request at a time, so one slot is enough.
type refusalState struct {
	err     error
	answers bool
	answer  views.Answer
	// raw is a 5xx the fake answered itself (an entry with no typed error):
	// answered verbatim, never stored.
	raw bool
}

// errProbe5xx stands for an unmapped failure (the fake handler answered a 5xx):
// no catalogue entry matches, so Once releases it like RETRYABLE — the fleet's
// safe fallback — and the production writer renders it as the internal-error
// problem.
var errProbe5xx = errors.New("probe: the handler answered a 5xx")

// copyRecorder answers w with what rec recorded, verbatim.
func copyRecorder(w http.ResponseWriter, rec *httptest.ResponseRecorder) {
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

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
	entries := map[string]inbound.ReplayCatalogueEntry{}
	for _, e := range inbound.ReplayCatalogue() {
		entries[e.Slug] = e
	}
	refusal := &refusalState{}
	return replayOps{
		Catalogue: catalogueEntries,
		Middleware: func(_ *testing.T, next http.Handler) http.Handler {
			return inbound.RequireIdempotencyKeyForTest(replayHandler(deps, refusal, next))
		},
		Refuse: refuseWith(entries, refusal),
		Write:  probeWrite(auths),
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

// catalogueEntries maps the production catalogue onto the reference test's
// problem entries (slug, status, class).
func catalogueEntries() []problemEntry {
	out := make([]problemEntry, 0, 16)
	for _, e := range inbound.ReplayCatalogue() {
		out = append(out, problemEntry{Slug: e.Slug, Status: e.Status, Class: string(e.Class)})
	}
	return out
}

// replayHandler is the production composition: the key check, then the Once
// engine with the fake handler as the effect. The effect translates what the
// fake answered (a typed refusal, a refusal answer, a 5xx, or a success) into
// the (Answer, error) the engine stores or releases by class; the response is
// rendered back through the production writer.
func replayHandler(deps usecases.Deps, refusal *refusalState, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() { *refusal = refusalState{} }()
		var rec *httptest.ResponseRecorder
		answer, err := deps.Once(r.Context(), "replay", r.Header.Get(idempotencyKeyHeader), string(body),
			func(ctx context.Context) (views.Answer, error) {
				rec = httptest.NewRecorder()
				next.ServeHTTP(rec, r.WithContext(ctx))
				return effectAnswer(refusal, rec)
			})
		// A 5xx the fake answered itself: answer it verbatim (Once released
		// the key; nothing was stored).
		if refusal.raw && errors.Is(err, errProbe5xx) {
			copyRecorder(w, rec)
			return
		}
		inbound.WriteAnswerForTest(w, r, answer, err)
	})
}

// effectAnswer translates the fake handler's answer into what the engine
// stores or releases: a typed refusal error, a refusal answer, an unmapped
// 5xx, or a success answer.
func effectAnswer(refusal *refusalState, rec *httptest.ResponseRecorder) (views.Answer, error) {
	switch {
	case refusal.err != nil:
		return views.Answer{}, refusal.err
	case refusal.answers:
		return refusal.answer, nil
	case rec.Code >= http.StatusInternalServerError:
		return views.Answer{}, errProbe5xx
	default:
		// A success: KindHold is the answer kind the router answers 201 for.
		return views.Answer{
			Kind: views.KindHold, ID: "hold_probe", ExpiresAt: t0.Add(7 * 24 * time.Hour),
			AmountMinor: 1, Currency: "EUR",
		}, nil
	}
}

// refuseWith answers entry e exactly as production does — through the
// production error writer for typed errors, through the production problem
// writer otherwise — and records what the effect must hand to the engine.
func refuseWith(entries map[string]inbound.ReplayCatalogueEntry, refusal *refusalState) func(w http.ResponseWriter, r *http.Request, e problemEntry) {
	return func(w http.ResponseWriter, r *http.Request, e problemEntry) {
		ent := entries[e.Slug]
		if ent.Err != nil {
			refusal.err = ent.Err
			inbound.WriteErrorForTest(w, r, ent.Err)
			return
		}
		if e.Status >= http.StatusInternalServerError {
			// A 5xx entry without a typed error (internal-error,
			// dependency-unavailable): never stored, answered as written.
			refusal.raw = true
			inbound.RefuseSlugForTest(w, r, e.Slug, ent.Detail)
			return
		}
		refusal.answers, refusal.answer = refusalAnswer(e.Slug, ent.Detail)
		inbound.RefuseSlugForTest(w, r, e.Slug, ent.Detail)
	}
}

// refusalAnswer is the answer the effect hands back for a problem the adapter
// writes without a typed error: the decline is a stored Answer (ADR 0004, the
// behaviour customer-payments depends on); other FINAL/OUTCOME problems are
// stored as refusal answers.
func refusalAnswer(slug, detail string) (bool, views.Answer) {
	if slug == "payment-declined" {
		return true, views.Answer{Kind: views.KindDeclined, Code: providertoken.DeclineCode}
	}
	return true, views.Answer{Kind: views.KindProblem, Slug: slug, Detail: detail}
}

// probeWrite stands for the aggregate write a real effect makes: a hold saved
// through the unit's context, committed with the unit, rolled back with it.
func probeWrite(auths *memory.Authorizations) func(ctx context.Context, tag string) error {
	return func(ctx context.Context, tag string) error {
		a, err := providerauth.Authorize(providerauth.ID("hold_probe_"+tag), providertoken.Approve, tag, "EUR", 1, t0)
		if err != nil {
			return err
		}
		return auths.Save(ctx, a)
	}
}
