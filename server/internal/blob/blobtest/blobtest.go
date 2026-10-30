// Package blobtest holds the contract tests that every blob.Store passes.
package blobtest

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/sourcehawk/pricedin/server/internal/blob"
)

// Run runs the contract tests. newStore returns an empty store for each
// test.
func Run(t *testing.T, newStore func(t *testing.T) blob.Store) {
	t.Helper()
	tests := []struct {
		name string
		fn   func(t *testing.T, s blob.Store)
	}{
		{"PutThenGetReturnsTheBytes", putThenGet},
		{"PutReplacesTheObject", putReplaces},
		{"GetOfAnEmptyKeyIsNotFound", getMissing},
		{"ExistsReportsStoredKeysOnly", exists},
		{"ListReturnsTheKeysWithThePrefixInOrder", list},
		{"FailedPutKeepsTheEarlierObject", failedPut},
		{"FailedPutOfANewKeyStoresNothing", failedPutNewKey},
		{"LargeObjectRoundTrips", largeObject},
		{"InvalidKeysAreRejected", invalidKeys},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fn(t, newStore(t))
		})
	}
}

func put(t *testing.T, s blob.Store, key, body string) {
	t.Helper()
	if err := s.Put(context.Background(), key, bytes.NewBufferString(body)); err != nil {
		t.Fatalf("Put(%q) error = %v", key, err)
	}
}

func get(t *testing.T, s blob.Store, key string) []byte {
	t.Helper()
	r, err := s.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get(%q) error = %v", key, err)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read %q: %v", key, err)
	}
	return b
}

func putThenGet(t *testing.T, s blob.Store) {
	key := blob.PrefixSEC + "companyfacts/2026-10-30/ab12-CIK0000320193.json"
	put(t, s, key, `{"cik":320193}`)
	if got := string(get(t, s, key)); got != `{"cik":320193}` {
		t.Errorf("Get() = %q, want the stored body", got)
	}
}

func putReplaces(t *testing.T, s blob.Store) {
	put(t, s, "prices/a.json", "first")
	put(t, s, "prices/a.json", "second")
	if got := string(get(t, s, "prices/a.json")); got != "second" {
		t.Errorf("Get() = %q, want %q", got, "second")
	}
}

func getMissing(t *testing.T, s blob.Store) {
	put(t, s, "sec/dir/file.json", "x")
	for _, key := range []string{"sec/none.json", "sec/dir", "sec/dir/file.json/child.json"} {
		_, err := s.Get(context.Background(), key)
		if !errors.Is(err, blob.ErrNotFound) {
			t.Errorf("Get(%q) error = %v, want ErrNotFound", key, err)
		}
	}
}

func exists(t *testing.T, s blob.Store) {
	ctx := context.Background()
	put(t, s, "sec/dir/file.json", "x")
	for key, want := range map[string]bool{
		"sec/dir/file.json": true, "sec/dir": false, "sec/dir/other.json": false, "sec/dir/file.json/child.json": false,
	} {
		got, err := s.Exists(ctx, key)
		if err != nil || got != want {
			t.Errorf("Exists(%q) = %v, %v; want %v, nil", key, got, err, want)
		}
	}
}

func list(t *testing.T, s blob.Store) {
	ctx := context.Background()
	keys := []string{
		"sec/submissions/2026-10-30/b.json",
		"sec/submissions/2026-10-29/a.json",
		"sec/companyfacts/2026-10-30/c.json",
		"prices/massive/grouped/2026-10-30/p1.json",
	}
	for _, k := range keys {
		put(t, s, k, k)
	}
	tests := []struct {
		prefix string
		want   []string
	}{
		{"sec/submissions/", []string{"sec/submissions/2026-10-29/a.json", "sec/submissions/2026-10-30/b.json"}},
		{"sec/submissions/2026-10-3", []string{"sec/submissions/2026-10-30/b.json"}},
		{"sec", []string{"sec/companyfacts/2026-10-30/c.json", "sec/submissions/2026-10-29/a.json", "sec/submissions/2026-10-30/b.json"}},
		{"", []string{
			"prices/massive/grouped/2026-10-30/p1.json",
			"sec/companyfacts/2026-10-30/c.json",
			"sec/submissions/2026-10-29/a.json",
			"sec/submissions/2026-10-30/b.json",
		}},
		{"none/", nil},
		// The prefix runs through a key, which the filesystem stores as a file.
		{"sec/submissions/2026-10-30/b.json/x/", nil},
	}
	for _, tt := range tests {
		got, err := s.List(ctx, tt.prefix)
		if err != nil {
			t.Fatalf("List(%q) error = %v", tt.prefix, err)
		}
		if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("List(%q) (-want +got):\n%s", tt.prefix, diff)
		}
	}
}

// failingReader yields some bytes and then an error.
type failingReader struct{ sent bool }

var errReader = errors.New("reader broke")

func (r *failingReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, errReader
	}
	r.sent = true
	return copy(p, "partial"), nil
}

func failedPut(t *testing.T, s blob.Store) {
	put(t, s, "sec/a.json", "complete")
	if err := s.Put(context.Background(), "sec/a.json", &failingReader{}); err == nil {
		t.Fatal("Put() with a failing reader error = nil, want an error")
	}
	if got := string(get(t, s, "sec/a.json")); got != "complete" {
		t.Errorf("Get() after a failed Put = %q, want the earlier object", got)
	}
	keys, err := s.List(context.Background(), "")
	if err != nil || len(keys) != 1 {
		t.Errorf("List() after a failed Put = %v, %v; want only sec/a.json", keys, err)
	}
}

func failedPutNewKey(t *testing.T, s blob.Store) {
	ctx := context.Background()
	if err := s.Put(ctx, "sec/new.json", &failingReader{}); err == nil {
		t.Fatal("Put() with a failing reader error = nil, want an error")
	}
	if ok, err := s.Exists(ctx, "sec/new.json"); err != nil || ok {
		t.Errorf("Exists() after a failed Put = %v, %v; want false, nil", ok, err)
	}
}

func largeObject(t *testing.T, s blob.Store) {
	// Larger than one multipart part, so the S3 store uploads in parts.
	body := make([]byte, 20<<20+123)
	_, _ = rand.Read(body)
	// MultiReader hides Seek and ReadAt, like a body streamed from the network.
	if err := s.Put(context.Background(), "sec/bulk/companyfacts.zip", io.MultiReader(bytes.NewReader(body))); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if got := get(t, s, "sec/bulk/companyfacts.zip"); !bytes.Equal(got, body) {
		t.Errorf("Get() returned %d bytes that differ from the %d stored", len(got), len(body))
	}
}

func invalidKeys(t *testing.T, s blob.Store) {
	ctx := context.Background()
	for _, key := range []string{"", "/abs", "../up", "sec/../x", "sec//x", "sec/./x", "sec/.hidden", "sec/", `sec\x`, "sec/a\nb"} {
		if err := s.Put(ctx, key, bytes.NewBufferString("x")); !errors.Is(err, blob.ErrInvalidKey) {
			t.Errorf("Put(%q) error = %v, want ErrInvalidKey", key, err)
		}
		if _, err := s.Get(ctx, key); !errors.Is(err, blob.ErrInvalidKey) {
			t.Errorf("Get(%q) error = %v, want ErrInvalidKey", key, err)
		}
		if _, err := s.Exists(ctx, key); !errors.Is(err, blob.ErrInvalidKey) {
			t.Errorf("Exists(%q) error = %v, want ErrInvalidKey", key, err)
		}
	}
	for _, prefix := range []string{"../", "/abs", "sec//"} {
		if _, err := s.List(ctx, prefix); !errors.Is(err, blob.ErrInvalidKey) {
			t.Errorf("List(%q) error = %v, want ErrInvalidKey", prefix, err)
		}
	}
}
