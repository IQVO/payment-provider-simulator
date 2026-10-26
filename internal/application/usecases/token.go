package usecases

import (
	"context"

	"github.com/IQVO/payment-provider-simulator/internal/application/views"
	"github.com/IQVO/payment-provider-simulator/internal/domain/providertoken"
)

// IssueToken is the hosted-fields stand-in: it issues a payment-method token
// that names its test scenario. Tokens are self-describing, so nothing about
// them is stored besides the idempotent answer.
type IssueToken struct{ Deps }

// IssueTokenInput is the request body of POST /tokens.
type IssueTokenInput struct {
	Key      string
	Scenario string
}

// Execute issues the token; an empty scenario means approve.
func (u IssueToken) Execute(ctx context.Context, in IssueTokenInput) (views.Answer, error) {
	sc, err := providertoken.ParseScenario(in.Scenario)
	if err != nil {
		return views.Answer{}, err
	}
	return u.once(ctx, "tokens", in.Key, fingerprint(string(sc)), func(context.Context) (views.Answer, error) {
		return views.Answer{
			Kind: views.KindToken, Token: providertoken.Issue(sc, u.IDs.NewTokenSuffix()), Scenario: string(sc),
		}, nil
	})
}
