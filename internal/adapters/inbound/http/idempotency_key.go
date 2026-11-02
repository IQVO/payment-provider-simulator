package http

import "net/http"

const maxIdempotencyKeyLen = 128

var (
	keyRequired = problem{http.StatusBadRequest, "idempotency-key-required", "Idempotency-Key header is required"}
	keyInvalid  = problem{http.StatusBadRequest, "idempotency-key-invalid", "Idempotency-Key must be 1-128 printable ASCII characters"}
)

// requireIdempotencyKey wraps every POST route (fleet REST guideline 2.2): a
// missing key is 400 idempotency-key-required and a malformed one 400
// idempotency-key-invalid, before the body is read. The use cases then claim
// the key in the same unit of work as the aggregate change.
func requireIdempotencyKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vals := r.Header.Values(idempotencyKeyHeader)
		switch {
		case len(vals) == 0 || vals[0] == "":
			writeBody(w, keyRequired.status, problemBody(r, keyRequired, "the Idempotency-Key header is required on this operation"))
		case len(vals) > 1 || !validIdempotencyKey(vals[0]):
			writeBody(w, keyInvalid.status, problemBody(r, keyInvalid, "the Idempotency-Key header must be 1-128 printable ASCII characters"))
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func validIdempotencyKey(key string) bool {
	if len(key) > maxIdempotencyKeyLen {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] > 0x7e {
			return false
		}
	}
	return true
}
