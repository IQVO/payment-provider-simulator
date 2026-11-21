package http

import (
	"net/http"

	"github.com/IQVO/payment-provider-simulator/internal/application/idempotency"
)

// ReplayCatalogueEntry is one problem type as the replay-class reference test
// (package http_test) needs it: the entry's typed error (nil for problems the
// adapter writes without one), its wire status and slug, the detail the
// production writer answers for it, and the replay class the entry declares
// ("" while unclassified — that is the red the reference test reports).
type ReplayCatalogueEntry struct {
	Err    error
	Status int
	Slug   string
	Detail string
	Class  idempotency.ReplayClass
}

// ReplayCatalogue returns every problem type the keyed routes can answer, read
// from the production catalogue (problemCatalogue plus the adapter's own
// problems), one entry per slug in match order. It is NOT a copy: it derives
// from the same literals production matches on.
func ReplayCatalogue() []ReplayCatalogueEntry {
	seen := map[string]bool{}
	out := make([]ReplayCatalogueEntry, 0, len(problemCatalogue)+5)
	add := func(err error, p problem) {
		if seen[p.slug] {
			return
		}
		seen[p.slug] = true
		detail := "the replay-class reference test refuses with " + p.slug
		if err != nil {
			detail = err.Error()
		}
		out = append(out, ReplayCatalogueEntry{Err: err, Status: p.status, Slug: p.slug, Detail: detail, Class: p.class})
	}
	for _, e := range problemCatalogue {
		add(e.err, e.p)
	}
	add(nil, keyRequired)
	add(nil, keyInvalid)
	add(nil, notFoundResource)
	add(nil, paymentDeclined)
	add(nil, dependencyUnavailable)
	add(nil, internalProblem)
	return out
}

// The production middleware and writers, exported for the replay-class
// reference test in package http_test (it must refuse through the production
// path, never a hand-written response).
var (
	// WriteErrorForTest answers err exactly as a handler does.
	WriteErrorForTest = writeError
	// WriteAnswerForTest renders a use case outcome exactly as a handler does.
	WriteAnswerForTest = writeAnswer
	// RequireIdempotencyKeyForTest is the production Idempotency-Key check.
	RequireIdempotencyKeyForTest = requireIdempotencyKey
)

// RefuseSlugForTest answers the problem the slug names through the production
// problem writer, for catalogue entries that have no typed error.
func RefuseSlugForTest(w http.ResponseWriter, r *http.Request, slug, detail string) {
	p, ok := problemBySlug(slug)
	if !ok {
		p = internalProblem
	}
	writeProblem(w, r, p, detail)
}
