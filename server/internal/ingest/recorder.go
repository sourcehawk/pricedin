package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/blob"
	"github.com/sourcehawk/pricedin/server/internal/domain"
)

// Recorder keeps every raw response in a blob store. It implements
// sec.Recorder and massive.Recorder.
type Recorder struct {
	store blob.Store
	now   func() time.Time
}

// NewRecorder returns a Recorder over store. now gives the retrieval time;
// nil means time.Now.
func NewRecorder(store blob.Store, now func() time.Time) *Recorder {
	if now == nil {
		now = time.Now
	}
	return &Recorder{store: store, now: now}
}

// Record stores body under VersionedKey(key, today in UTC, body). For a key
// with prefix "prices/massive/", the hash leaves out the top-level
// request_id; the stored body keeps it. A body that is already stored under
// its key is not written again. An error of the blob store is returned,
// and fails the client's request.
func (r *Recorder) Record(ctx context.Context, key string, body []byte) error {
	hashed := body
	if strings.HasPrefix(key, blob.PrefixPrices+"massive/") {
		hashed = withoutRequestID(body)
	}
	versioned, err := VersionedKey(key, domain.DateOf(r.now().UTC()), hashed)
	if err != nil {
		return err
	}
	return putOnce(ctx, r.store, versioned, func() error {
		return r.store.Put(ctx, versioned, bytes.NewReader(body))
	})
}

// withoutRequestID returns the JSON object body without its top-level
// request_id, in a canonical form, or body itself when it is not a JSON
// object.
func withoutRequestID(body []byte) []byte {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return body
	}
	delete(fields, "request_id")
	// Marshal sorts the keys and keeps each value's bytes.
	out, err := json.Marshal(fields)
	if err != nil {
		return body
	}
	return out
}

// VersionedKey returns the key under which a response with the base key
// and the body, retrieved on day, is kept: the day and the first 16 hex
// digits of the body's SHA-256 go before the last segment of base, so
// "sec/companyfacts/CIK0001045810.json" becomes
// "sec/companyfacts/2026-10-30/<sha16>-CIK0001045810.json". Distinct bodies
// never share a key, and a refreshed response never replaces an earlier
// one. base must be a valid blob key of two or more segments.
func VersionedKey(base string, day domain.Date, body []byte) (string, error) {
	sum := sha256.Sum256(body)
	return versionedKey(base, day, hex.EncodeToString(sum[:])[:16])
}

func versionedKey(base string, day domain.Date, sha16 string) (string, error) {
	if err := blob.ValidateKey(base); err != nil {
		return "", fmt.Errorf("ingest: record: %w", err)
	}
	dir, file, ok := cutLast(base, "/")
	if !ok {
		return "", fmt.Errorf("ingest: record: key %q has no folder", base)
	}
	return dir + "/" + day.String() + "/" + sha16 + "-" + file, nil
}

func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return "", s, false
	}
	return s[:i], s[i+len(sep):], true
}

// putOnce calls put unless key already holds an object.
func putOnce(ctx context.Context, store blob.Store, key string, put func() error) error {
	exists, err := store.Exists(ctx, key)
	if err != nil {
		return fmt.Errorf("ingest: record %s: %w", key, err)
	}
	if exists {
		return nil
	}
	if err := put(); err != nil {
		return fmt.Errorf("ingest: record %s: %w", key, err)
	}
	return nil
}
