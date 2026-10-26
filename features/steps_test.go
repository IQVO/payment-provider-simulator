package features_test

import (
	"context"
	"fmt"
	"time"

	"github.com/cucumber/godog"
)

func initializeScenario(sc *godog.ScenarioContext) {
	var w *world
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w = newWorld()
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		w.srv.Close()
		return ctx, nil
	})
	sc.Step(`^a token for scenario "([^"]*)"$`, func(s string) error { return w.issue(s) })
	sc.Step(`^a hold of (\d+) EUR with that token$`, func(n int) error { return w.authorize(n, w.token, "") })
	sc.Step(`^I authorize (\d+) EUR with that token$`, func(n int) error { return w.authorize(n, w.token, "") })
	sc.Step(`^I authorize (\d+) EUR with that token under key "([^"]*)"$`, func(n int, k string) error { return w.authorize(n, w.token, k) })
	sc.Step(`^I authorize (\d+) EUR with the token "([^"]*)"$`, func(n int, tok string) error { return w.authorize(n, tok, "") })
	registerAssertions(sc, func() *world { return w })
	registerHoldSteps(sc, func() *world { return w })
}

func (w *world) issue(scenario string) error {
	if err := w.do("POST", "/tokens", "", fmt.Sprintf(`{"scenario":%q}`, scenario)); err != nil {
		return err
	}
	tok, _ := w.body["token"].(string)
	if w.status != 201 || tok == "" {
		return fmt.Errorf("token issuance answered %d %v", w.status, w.body)
	}
	w.token = tok
	return nil
}

func (w *world) authorize(amount int, token, key string) error {
	body := fmt.Sprintf(`{"amountMinor":%d,"currency":"EUR","paymentMethodToken":%q,"reference":"cs-1"}`, amount, token)
	if err := w.do("POST", "/authorizations", key, body); err != nil {
		return err
	}
	if id, ok := w.body["id"].(string); ok {
		w.holdID = id
	}
	return nil
}

func (w *world) advance(minutes int) error {
	w.clock.Advance(time.Duration(minutes) * time.Minute)
	return nil
}
