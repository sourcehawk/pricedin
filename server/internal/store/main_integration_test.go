//go:build integration

package store_test

import (
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/store/pgtest"
)

func TestMain(m *testing.M) {
	pgtest.Main(m)
}
