// Package blob keeps raw downloads, such as SEC files and price provider
// responses, so that every stored value can be reprocessed from its source.
// It has a filesystem store for local use and tests, and an S3-compatible
// store for production.
package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Key prefixes of the raw downloads.
const (
	PrefixSEC    = "sec/"
	PrefixPrices = "prices/"
)

// ErrNotFound is returned, wrapped, by Get for a key that holds no object.
var ErrNotFound = errors.New("blob: not found")

// ErrInvalidKey is returned, wrapped, for a key or prefix that ValidateKey
// or ValidatePrefix rejects.
var ErrInvalidKey = errors.New("blob: invalid key")

// Store keeps objects under keys. A key is a path of segments separated by
// "/", such as "sec/companyfacts/2026-10-30/ab12-CIK0000320193.json"; see
// ValidateKey. A key must not also be the start of another key followed by
// "/", because the filesystem store cannot hold a file and a folder under
// one name. Implementations are safe for concurrent use.
type Store interface {
	// Put stores everything r yields under key and replaces any object
	// there. When Put fails, the key keeps its earlier object, or stays
	// empty.
	Put(ctx context.Context, key string, r io.Reader) error
	// Get opens the object under key, or returns ErrNotFound. The caller
	// closes the reader.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Exists reports whether an object is stored under key.
	Exists(ctx context.Context, key string) (bool, error)
	// List returns every key that starts with prefix, in byte order. The
	// prefix does not have to end at a "/"; the empty prefix lists all
	// keys.
	List(ctx context.Context, prefix string) ([]string, error)
}

// ValidateKey accepts a key of one or more segments separated by "/". A
// segment is not empty and does not start with ".", which also rules out
// "." and "..". A key has no backslash and no control character.
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty", ErrInvalidKey)
	}
	if strings.ContainsFunc(key, func(r rune) bool { return r == '\\' || r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("%w: %q has a backslash or a control character", ErrInvalidKey, key)
	}
	for seg := range strings.SplitSeq(key, "/") {
		if seg == "" || seg[0] == '.' {
			return fmt.Errorf("%w: %q has an empty segment or one that starts with a dot", ErrInvalidKey, key)
		}
	}
	return nil
}

// ValidatePrefix accepts the empty prefix, and any prefix that is a valid key
// or a valid key followed by "/".
func ValidatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	return ValidateKey(strings.TrimSuffix(prefix, "/"))
}
