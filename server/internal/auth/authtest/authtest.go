// Package authtest gives tests an in-memory auth.Store and a settable
// clock.
package authtest

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/store"
)

// Store keeps sessions and tokens in memory with the semantics of the
// store package: missing rows give store.ErrNotFound, a used token name
// gives store.ErrConflict, and revoked tokens are not active. Read its
// fields only while no request runs.
type Store struct {
	mu sync.Mutex
	// Sessions is keyed by the token hash.
	Sessions map[string]store.Session
	Tokens   []store.APIToken
	// LastSeenWrites and LastUsedWrites count the updates, so tests can
	// check that they happen at most once a minute.
	LastSeenWrites int
	LastUsedWrites int
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{Sessions: map[string]store.Session{}}
}

// InsertSession stores s, or returns store.ErrConflict for a used hash.
func (m *Store) InsertSession(_ context.Context, s store.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Sessions[string(s.TokenHash)]; ok {
		return store.ErrConflict
	}
	m.Sessions[string(s.TokenHash)] = s
	return nil
}

// GetSession returns the session with the hash, or store.ErrNotFound.
func (m *Store) GetSession(_ context.Context, hash []byte) (store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.Sessions[string(hash)]
	if !ok {
		return store.Session{}, fmt.Errorf("get session: %w", store.ErrNotFound)
	}
	return s, nil
}

// UpdateSessionLastSeen sets the last-seen time, or returns store.ErrNotFound.
func (m *Store) UpdateSessionLastSeen(_ context.Context, hash []byte, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.Sessions[string(hash)]
	if !ok {
		return store.ErrNotFound
	}
	s.LastSeenAt = at
	m.Sessions[string(hash)] = s
	m.LastSeenWrites++
	return nil
}

// DeleteSession removes the session, if any.
func (m *Store) DeleteSession(_ context.Context, hash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Sessions, string(hash))
	return nil
}

// DeleteAllSessions removes every session.
func (m *Store) DeleteAllSessions(context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := int64(len(m.Sessions))
	m.Sessions = map[string]store.Session{}
	return n, nil
}

// DeleteExpiredSessions removes the sessions that expired at or before now.
func (m *Store) DeleteExpiredSessions(_ context.Context, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for k := range m.Sessions {
		if !m.Sessions[k].ExpiresAt.After(now) {
			delete(m.Sessions, k)
			n++
		}
	}
	return n, nil
}

// InsertAPIToken stores an active token, or returns store.ErrConflict for a used name or hash.
func (m *Store) InsertAPIToken(_ context.Context, name string, hash []byte, createdAt time.Time) (store.APIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.Tokens {
		if t.Name == name || bytes.Equal(t.TokenHash, hash) {
			return store.APIToken{}, store.ErrConflict
		}
	}
	t := store.APIToken{ID: int64(len(m.Tokens) + 1), Name: name, TokenHash: hash, CreatedAt: createdAt}
	m.Tokens = append(m.Tokens, t)
	return t, nil
}

// GetActiveAPIToken returns the active token with the hash, or store.ErrNotFound.
func (m *Store) GetActiveAPIToken(_ context.Context, hash []byte) (store.APIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.Tokens {
		if bytes.Equal(t.TokenHash, hash) && t.RevokedAt == nil {
			return t, nil
		}
	}
	return store.APIToken{}, store.ErrNotFound
}

// ListAPITokens returns every token, oldest first.
func (m *Store) ListAPITokens(context.Context) ([]store.APIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.Tokens), nil
}

// RevokeAPIToken revokes the active token with the name, or returns store.ErrNotFound.
func (m *Store) RevokeAPIToken(_ context.Context, name string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.Tokens {
		if t.Name == name && t.RevokedAt == nil {
			m.Tokens[i].RevokedAt = &at
			return nil
		}
	}
	return store.ErrNotFound
}

// UpdateAPITokenLastUsed sets the last-used time, or returns store.ErrNotFound.
func (m *Store) UpdateAPITokenLastUsed(_ context.Context, id int64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.Tokens {
		if t.ID == id {
			m.Tokens[i].LastUsedAt = &at
			m.LastUsedWrites++
			return nil
		}
	}
	return store.ErrNotFound
}

// Clock is a settable time source, safe for concurrent use.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock returns a Clock at t.
func NewClock(t time.Time) *Clock {
	return &Clock{now: t}
}

// Now returns the clock's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
