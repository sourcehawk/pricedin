package stocks_test

import (
	"context"
	"testing"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/stocks"
	"github.com/sourcehawk/pricedin/server/internal/stocks/stockstest"
)

func BenchmarkEvaluateNVIDIA(b *testing.B) {
	svc := stocks.New(stockstest.Fixture(b), stocks.Options{Now: func() time.Time { return stockstest.Now }})
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := svc.Evaluate(ctx, "NVDA", stocks.LeversInput{}); err != nil {
			b.Fatal(err)
		}
	}
}
