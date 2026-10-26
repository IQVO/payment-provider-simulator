package features_test

import (
	"testing"

	"github.com/cucumber/godog"
)

// TestFeatures runs the Gherkin scenarios of this directory against the real
// chi router over the use cases wired to the in-memory doubles.
func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeScenario,
		Options:             &godog.Options{Format: "pretty", Paths: []string{"."}, TestingT: t, Strict: true},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}
