package pgtest_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

func TestImageMatchesTheComposeDatabase(t *testing.T) {
	compose, err := os.ReadFile("../../../../docker-compose.yml")
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	m := regexp.MustCompile(`(?m)^\s+image:\s+(postgres:\S+)\s*$`).FindSubmatch(compose)
	if m == nil {
		t.Fatal("docker-compose.yml has no postgres image line")
	}
	if got := string(m[1]); got != pgtest.Image {
		t.Errorf("docker-compose.yml image = %q, pgtest.Image = %q; keep them the same", got, pgtest.Image)
	}
}
