package ingest_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/blob"
	"github.com/sourcehawk/pricedin/server/internal/domain"
	"github.com/sourcehawk/pricedin/server/internal/ingest"
)

func TestVersionedKeyPutsTheDayAndTheHashBeforeTheFile(t *testing.T) {
	day := domain.NewDate(2026, time.October, 30)
	// The SHA-256 of "hello" starts with 2cf24dba5fb0a30e.
	tests := []struct {
		base, want string
	}{
		{"sec/companyfacts/CIK0001045810.json", "sec/companyfacts/2026-10-30/2cf24dba5fb0a30e-CIK0001045810.json"},
		{"prices/massive/grouped/2026-10-29/page-1.json", "prices/massive/grouped/2026-10-29/2026-10-30/2cf24dba5fb0a30e-page-1.json"},
		{"prices/massive/aggs/BRK.B/2021-11-01_2026-10-29/page-2.json", "prices/massive/aggs/BRK.B/2021-11-01_2026-10-29/2026-10-30/2cf24dba5fb0a30e-page-2.json"},
	}
	for _, tt := range tests {
		got, err := ingest.VersionedKey(tt.base, day, []byte("hello"))
		if err != nil || got != tt.want {
			t.Errorf("VersionedKey(%q) = %q, %v; want %q", tt.base, got, err, tt.want)
		}
	}
}

func TestVersionedKeyDiffersForDifferentBodiesAndDays(t *testing.T) {
	day := domain.NewDate(2026, time.October, 30)
	a, _ := ingest.VersionedKey("sec/tickers/company_tickers_exchange.json", day, []byte("a"))
	b, _ := ingest.VersionedKey("sec/tickers/company_tickers_exchange.json", day, []byte("b"))
	c, _ := ingest.VersionedKey("sec/tickers/company_tickers_exchange.json", day.AddDays(1), []byte("a"))
	if a == b || a == c {
		t.Errorf("keys %q, %q, %q: want three distinct keys", a, b, c)
	}
}

func TestVersionedKeyRejectsInvalidBaseKeys(t *testing.T) {
	day := domain.NewDate(2026, time.October, 30)
	for _, base := range []string{"", "CIK0001045810.json", "sec//x.json", "sec/../x.json"} {
		if got, err := ingest.VersionedKey(base, day, nil); err == nil {
			t.Errorf("VersionedKey(%q) = %q, want an error", base, got)
		}
	}
}

// countingStore counts Put calls and can fail them.
type countingStore struct {
	blob.Store
	puts    int
	failPut error
}

func (c *countingStore) Put(ctx context.Context, key string, r io.Reader) error {
	c.puts++
	if c.failPut != nil {
		return c.failPut
	}
	return c.Store.Put(ctx, key, r)
}

func newCountingStore(t *testing.T) *countingStore {
	t.Helper()
	fs, err := blob.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &countingStore{Store: fs}
}

func fixedNow(s string) func() time.Time {
	return func() time.Time {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			panic(err)
		}
		return t
	}
}

func TestRecordStoresTheBodyOncePerDay(t *testing.T) {
	ctx := context.Background()
	store := newCountingStore(t)
	// 23:30 in New York is already the next day in UTC.
	rec := ingest.NewRecorder(store, fixedNow("2026-10-31T03:30:00Z"))

	for range 2 {
		if err := rec.Record(ctx, "sec/tickers/company_tickers_exchange.json", []byte("hello")); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}
	if store.puts != 1 {
		t.Errorf("Put calls = %d, want 1 for one body on one day", store.puts)
	}
	keys, err := store.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	want := "sec/tickers/2026-10-31/2cf24dba5fb0a30e-company_tickers_exchange.json"
	if len(keys) != 1 || keys[0] != want {
		t.Fatalf("keys = %v, want [%s]", keys, want)
	}
	rc, err := store.Get(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	if body, _ := io.ReadAll(rc); string(body) != "hello" {
		t.Errorf("stored body = %q, want hello", body)
	}

	if err := rec.Record(ctx, "sec/tickers/company_tickers_exchange.json", []byte("changed")); err != nil {
		t.Fatal(err)
	}
	if keys, _ := store.List(ctx, ""); len(keys) != 2 {
		t.Errorf("after a changed body: %d keys, want 2 (the first one kept)", len(keys))
	}
}

func TestRecordFailsWhenTheBlobStoreFails(t *testing.T) {
	store := newCountingStore(t)
	store.failPut = errors.New("disk full")
	rec := ingest.NewRecorder(store, fixedNow("2026-10-30T12:00:00Z"))
	err := rec.Record(context.Background(), "sec/tickers/company_tickers_exchange.json", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("Record() error = %v, want the blob store's error", err)
	}
}

func TestRecordStoresAMassiveResponseOnceWhateverItsRequestID(t *testing.T) {
	ctx := context.Background()
	store := newCountingStore(t)
	rec := ingest.NewRecorder(store, fixedNow("2026-10-30T12:00:00Z"))
	key := "prices/massive/splits/NVDA/page-1.json"
	first := `{"status":"OK","request_id":"aaa","results":[]}`
	second := `{"request_id":"bbb","status":"OK","results":[]}`
	for _, body := range []string{first, second} {
		if err := rec.Record(ctx, key, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := store.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if store.puts != 1 || len(keys) != 1 {
		t.Fatalf("puts = %d, keys = %v; want one stored response", store.puts, keys)
	}
	rc, err := store.Get(ctx, keys[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	if body, _ := io.ReadAll(rc); string(body) != first {
		t.Errorf("stored body = %s, want the first response as received", body)
	}

	if err := rec.Record(ctx, key, []byte(`{"status":"OK","request_id":"ccc","results":[{"id":"x"}]}`)); err != nil {
		t.Fatal(err)
	}
	if store.puts != 2 {
		t.Errorf("puts = %d after a changed result, want 2", store.puts)
	}
}

func TestRecordHashesAnSECResponseAsReceived(t *testing.T) {
	ctx := context.Background()
	store := newCountingStore(t)
	rec := ingest.NewRecorder(store, fixedNow("2026-10-30T12:00:00Z"))
	for _, body := range []string{`{"request_id":"a","x":1}`, `{"request_id":"b","x":1}`} {
		if err := rec.Record(ctx, "sec/tickers/company_tickers_exchange.json", []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if store.puts != 2 {
		t.Errorf("puts = %d, want 2: only Massive responses leave out request_id", store.puts)
	}
}
