package features_test

import (
	"fmt"
	"time"

	"github.com/cucumber/godog"
)

func registerHoldSteps(sc *godog.ScenarioContext, get func() *world) {
	op := func(sub string, amount int, hold string) error {
		w := get()
		if hold == "" {
			hold = w.holdID
		}
		body := fmt.Sprintf(`{"amountMinor":%d}`, amount)
		if sub == "void" {
			body = `{}`
		}
		if err := w.do("POST", "/authorizations/"+hold+"/"+sub, "", body); err != nil {
			return err
		}
		if sub == "reauthorize" {
			if id, ok := w.body["id"].(string); ok {
				w.newHold = id
			}
		}
		return nil
	}
	sc.Step(`^I capture (\d+)$`, func(n int) error { return op("captures", n, "") })
	sc.Step(`^I capture (\d+) on the hold "([^"]*)"$`, func(n int, h string) error { return op("captures", n, h) })
	sc.Step(`^I capture (\d+) on the new hold$`, func(n int) error { return op("captures", n, get().newHold) })
	sc.Step(`^I refund (\d+)$`, func(n int) error { return op("refunds", n, "") })
	sc.Step(`^I reduce the hold to (\d+)$`, func(n int) error { return op("reduce", n, "") })
	sc.Step(`^I reauthorize the hold for (\d+)$`, func(n int) error { return op("reauthorize", n, "") })
	sc.Step(`^I void the hold$`, func() error { return op("void", 0, "") })
	sc.Step(`^(\d+) minutes? pass(?:es)?$`, func(n int) error { return get().advance(n) })
	sc.Step(`^the hold shows remaining (\d+)$`, func(n int) error { return get().holdField("remainingMinor", float64(n)) })
	sc.Step(`^the old hold is "([^"]*)"$`, func(s string) error { return get().holdField("status", s) })
}

func (w *world) holdField(field string, want any) error {
	if err := w.do("GET", "/authorizations/"+w.holdID, "", ""); err != nil {
		return err
	}
	if w.body[field] != want {
		return fmt.Errorf("hold %s = %v, want %v", field, w.body[field], want)
	}
	return nil
}

func registerAssertions(sc *godog.ScenarioContext, get func() *world) {
	sc.Step(`^the response status is (\d+)$`, func(n int) error {
		if w := get(); w.status != n {
			return fmt.Errorf("status = %d, want %d (%v)", w.status, n, w.body)
		}
		return nil
	})
	sc.Step(`^the problem type is "([^"]*)"$`, func(slug string) error {
		if w := get(); w.body["type"] != "/problems/"+slug {
			return fmt.Errorf("problem type = %v, want %s", w.body["type"], slug)
		}
		return nil
	})
	sc.Step(`^the decline code is "([^"]*)"$`, func(code string) error {
		if w := get(); w.body["code"] != code {
			return fmt.Errorf("code = %v, want %s", w.body["code"], code)
		}
		return nil
	})
	sc.Step(`^the hold expires in (\d+) minutes$`, func(n int) error {
		w := get()
		raw, _ := w.body["expiresAt"].(string)
		at, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return fmt.Errorf("expiresAt %q: %w", raw, err)
		}
		if got := at.Sub(t0); got != time.Duration(n)*time.Minute {
			return fmt.Errorf("hold expires in %v, want %d minutes", got, n)
		}
		return nil
	})
	sc.Step(`^both answers carry the same hold id$`, func() error {
		a := get().answers
		if len(a) < 3 || a[len(a)-1]["id"] == nil || a[len(a)-1]["id"] != a[len(a)-2]["id"] {
			return fmt.Errorf("answers differ: %v", a)
		}
		return nil
	})
}
