package http_test

import (
	"os"
	"strings"
	"testing"

	inbound "github.com/IQVO/payment-provider-simulator/internal/adapters/inbound/http"
)

// adrPath is the ADR whose table this test holds equal to the catalogue.
const adrPath = "../../../docs/adr/0005-every-problem-type-has-a-replay-class.md"

// adrClassTable parses the `| `slug` | CLASS | status |` rows of ADR 0005.
func adrClassTable(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read ADR: %v", err)
	}
	table := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		slug, class, ok := adrRow(line)
		if !ok {
			continue
		}
		if prev, seen := table[slug]; seen && prev != class {
			t.Errorf("ADR row for %s appears twice with classes %s and %s", slug, prev, class)
		}
		table[slug] = class
	}
	return table
}

// adrRow reads one table row of the ADR: the slug in backticks and the class.
func adrRow(line string) (slug, class string, ok bool) {
	fields := strings.Split(strings.TrimSpace(line), "|")
	if len(fields) < 4 || !strings.HasPrefix(strings.TrimSpace(fields[1]), "`") {
		return "", "", false
	}
	slug = strings.Trim(strings.TrimSpace(fields[1]), "`")
	class = strings.TrimSpace(fields[2])
	if class == "Class" { // header row
		return "", "", false
	}
	return slug, class, true
}

// TestTheADRDeclaresTheClassOfEveryProblemType holds ADR 0005's class table
// equal to the production catalogue: one row per problem type, the same class,
// and the ADR names no problem type the catalogue does not answer.
func TestTheADRDeclaresTheClassOfEveryProblemType(t *testing.T) {
	table := adrClassTable(t)
	catalogue := inbound.ReplayCatalogue()
	if len(table) != len(catalogue) {
		t.Errorf("ADR table has %d rows, catalogue has %d entries", len(table), len(catalogue))
	}
	adrClassesMatchTheCatalogue(t, table, catalogue)
	noADRRowForAnUnansweredProblem(t, table, catalogue)
}

func adrClassesMatchTheCatalogue(t *testing.T, table map[string]string, catalogue []inbound.ReplayCatalogueEntry) {
	t.Helper()
	for _, e := range catalogue {
		class, ok := table[e.Slug]
		if !ok {
			t.Errorf("problem type %q has no ADR 0005 row", e.Slug)
			continue
		}
		if class != string(e.Class) {
			t.Errorf("ADR 0005 class of %q is %q, catalogue declares %q", e.Slug, class, e.Class)
		}
	}
}

func noADRRowForAnUnansweredProblem(t *testing.T, table map[string]string, catalogue []inbound.ReplayCatalogueEntry) {
	t.Helper()
	known := map[string]bool{}
	for _, e := range catalogue {
		known[e.Slug] = true
	}
	for slug := range table {
		if !known[slug] {
			t.Errorf("ADR 0005 classifies %q, which the catalogue never answers", slug)
		}
	}
}

// TestThe5xxAnswersAreRetryable pins the rule the reference test also asserts:
// nothing that answers 5xx may be stored.
func TestThe5xxAnswersAreRetryable(t *testing.T) {
	for _, e := range inbound.ReplayCatalogue() {
		if e.Status >= 500 && string(e.Class) != "RETRYABLE" {
			t.Errorf("problem type %q answers %d: its class is %q, want RETRYABLE (a 5xx is never stored)", e.Slug, e.Status, e.Class)
		}
	}
}
