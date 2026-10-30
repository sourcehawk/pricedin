package fundamentals_test

import (
	"math/rand/v2"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/fundamentals"
)

// The same facts give the same series, whatever their order. Garmin's
// filings report one year with two start dates a day apart, which snap to
// one period.
func TestDeriveGivesTheSameSeriesForTheSameFacts(t *testing.T) {
	for _, cik := range []domain.CIK{cikGRMN, cikKO, cikNVDA} {
		facts := fixtureFacts(t, cik)
		want, err := fundamentals.Derive(cik, facts, fundamentals.Options{})
		if err != nil {
			t.Fatal(err)
		}
		rng := rand.New(rand.NewPCG(1, uint64(cik))) //nolint:gosec // a test shuffle
		for range 10 {
			rng.Shuffle(len(facts), func(i, j int) { facts[i], facts[j] = facts[j], facts[i] })
			got, err := fundamentals.Derive(cik, facts, fundamentals.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Fatalf("%s: Derive() differs for the same facts in another order (-first +later):\n%s", cik, diff)
			}
		}
	}
}
