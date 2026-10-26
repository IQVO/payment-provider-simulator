package memory_test

import (
	"testing"

	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/memory"
	"github.com/IQVO/payment-provider-simulator/internal/adapters/outbound/repocontract"
)

func TestRepositoryContract(t *testing.T) {
	repocontract.Run(t, func(*testing.T) repocontract.Adapters {
		s := memory.NewStore()
		return repocontract.Adapters{UoW: memory.NewUnitOfWork(s), Authorizations: memory.NewAuthorizations(s), Idempotency: memory.NewIdempotency(s)}
	})
}
