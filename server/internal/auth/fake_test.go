package auth_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/store"
)

// memStore keeps sessions and tokens in memory with the semantics of the
// store package: missing rows give store.ErrNotFound, a used token name
// gives store.ErrConflict, and revoked tokens are not active.
type memStore struct {
	mu       sync.Mutex
	sessions map[string]store.Session
	tokens   []store.APIToken
	// lastSeenWrites and lastUsedWrites count the updates, so tests can
	// check that they happen at most once a minute.
	lastSeenWrites int
	lastUsedWrites int
}

func newMemStore() *memStore {
	return &memStore{sessions: map[string]store.Session{}}
}

func (m *memStore) InsertSession(_ context.Context, s store.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[string(s.TokenHash)]; ok {
		return store.ErrConflict
	}
	m.sessions[string(s.TokenHash)] = s
	return nil
}

func (m *memStore) GetSession(_ context.Context, hash []byte) (store.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[string(hash)]
	if !ok {
		return store.Session{}, fmt.Errorf("get session: %w", store.ErrNotFound)
	}
	return s, nil
}

func (m *memStore) UpdateSessionLastSeen(_ context.Context, hash []byte, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[string(hash)]
	if !ok {
		return store.ErrNotFound
	}
	s.LastSeenAt = at
	m.sessions[string(hash)] = s
	m.lastSeenWrites++
	return nil
}

func (m *memStore) DeleteSession(_ context.Context, hash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, string(hash))
	return nil
}

func (m *memStore) DeleteExpiredSessions(_ context.Context, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for k, s := range m.sessions {
		if !s.ExpiresAt.After(now) {
			delete(m.sessions, k)
			n++
		}
	}
	return n, nil
}

func (m *memStore) InsertAPIToken(_ context.Context, name string, hash []byte, createdAt time.Time) (store.APIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.Name == name || string(t.TokenHash) == string(hash) {
			return store.APIToken{}, store.ErrConflict
		}
	}
	t := store.APIToken{ID: int64(len(m.tokens) + 1), Name: name, TokenHash: hash, CreatedAt: createdAt}
	m.tokens = append(m.tokens, t)
	return t, nil
}

func (m *memStore) GetActiveAPIToken(_ context.Context, hash []byte) (store.APIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if string(t.TokenHash) == string(hash) && t.RevokedAt == nil {
			return t, nil
		}
	}
	return store.APIToken{}, store.ErrNotFound
}

func (m *memStore) ListAPITokens(context.Context) ([]store.APIToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.tokens), nil
}

func (m *memStore) RevokeAPIToken(_ context.Context, name string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.tokens {
		if t.Name == name && t.RevokedAt == nil {
			m.tokens[i].RevokedAt = &at
			return nil
		}
	}
	return store.ErrNotFound
}

func (m *memStore) UpdateAPITokenLastUsed(_ context.Context, id int64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.tokens {
		if t.ID == id {
			m.tokens[i].LastUsedAt = &at
			m.lastUsedWrites++
			return nil
		}
	}
	return store.ErrNotFound
}

// clock is a settable time source.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
