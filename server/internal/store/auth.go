package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Session is a login session of the owner. The store keeps only the hash
// of the session token.
type Session struct {
	TokenHash  []byte
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	UserAgent  string
	IP         string
	// OwnerFingerprint identifies the owner credentials that started the
	// session; empty for a session from before the column existed.
	OwnerFingerprint []byte
}

// InsertSession stores sess. It returns ErrConflict when a session with the
// same hash exists.
func (s *Store) InsertSession(ctx context.Context, sess Session) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO sessions (token_hash, created_at, expires_at, last_seen_at, user_agent, ip, owner_fingerprint)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		sess.TokenHash, sess.CreatedAt, sess.ExpiresAt, sess.LastSeenAt, sess.UserAgent, sess.IP, nonNil(sess.OwnerFingerprint))
	if isUniqueViolation(err) {
		return fmt.Errorf("insert session: %w", ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// GetSession returns the session with the token hash, or ErrNotFound. It
// returns an expired session too: the caller checks ExpiresAt.
func (s *Store) GetSession(ctx context.Context, tokenHash []byte) (Session, error) {
	var sess Session
	err := s.db.QueryRow(ctx, `
		SELECT token_hash, created_at, expires_at, last_seen_at, user_agent, ip, owner_fingerprint
		FROM sessions WHERE token_hash = $1`, tokenHash).
		Scan(&sess.TokenHash, &sess.CreatedAt, &sess.ExpiresAt, &sess.LastSeenAt, &sess.UserAgent, &sess.IP, &sess.OwnerFingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, notFound("get session")
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	return sess, nil
}

// UpdateSessionLastSeen sets the last-seen time of the session, or returns
// ErrNotFound.
func (s *Store) UpdateSessionLastSeen(ctx context.Context, tokenHash []byte, at time.Time) error {
	tag, err := s.db.Exec(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE token_hash = $1`, tokenHash, at)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("update session")
	}
	return nil
}

// DeleteSession removes the session. Removing a session that does not exist
// is not an error, so a repeated logout succeeds.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteAllSessions removes every session and returns how many it removed.
func (s *Store) DeleteAllSessions(ctx context.Context) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM sessions`)
	if err != nil {
		return 0, fmt.Errorf("delete all sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteExpiredSessions removes the sessions that expired at or before now
// and returns how many it removed.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// APIToken is a bearer token for scripts and agents. The store keeps only
// the hash of the token.
type APIToken struct {
	ID         int64
	Name       string
	TokenHash  []byte
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time // nil while the token is active
}

const apiTokenColumns = `id, name, token_hash, created_at, last_used_at, revoked_at` //nolint:gosec // G101: column names, not a credential

func scanAPIToken(row pgx.Row) (APIToken, error) {
	var t APIToken
	var used, revoked pgtype.Timestamptz
	err := row.Scan(&t.ID, &t.Name, &t.TokenHash, &t.CreatedAt, &used, &revoked)
	t.LastUsedAt, t.RevokedAt = fromPgTimePtr(used), fromPgTimePtr(revoked)
	return t, err
}

// InsertAPIToken stores a new active token and returns it. It returns
// ErrConflict when the name or the hash is already used, also by a revoked
// token.
func (s *Store) InsertAPIToken(ctx context.Context, name string, tokenHash []byte, createdAt time.Time) (APIToken, error) {
	t, err := scanAPIToken(s.db.QueryRow(ctx, `
		INSERT INTO api_tokens (name, token_hash, created_at) VALUES ($1, $2, $3)
		RETURNING `+apiTokenColumns, name, tokenHash, createdAt))
	if isUniqueViolation(err) {
		return APIToken{}, fmt.Errorf("insert api token %q: %w", name, ErrConflict)
	}
	if err != nil {
		return APIToken{}, fmt.Errorf("insert api token %q: %w", name, err)
	}
	return t, nil
}

// GetActiveAPIToken returns the token with the hash, or ErrNotFound when no
// token has it or the token is revoked.
func (s *Store) GetActiveAPIToken(ctx context.Context, tokenHash []byte) (APIToken, error) {
	t, err := scanAPIToken(s.db.QueryRow(ctx, `
		SELECT `+apiTokenColumns+` FROM api_tokens WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return APIToken{}, notFound("get api token")
	}
	if err != nil {
		return APIToken{}, fmt.Errorf("get api token: %w", err)
	}
	return t, nil
}

// ListAPITokens returns every token, revoked ones too, oldest first.
func (s *Store) ListAPITokens(ctx context.Context) ([]APIToken, error) {
	rows, err := s.db.Query(ctx, `SELECT `+apiTokenColumns+` FROM api_tokens ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (APIToken, error) { return scanAPIToken(row) })
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	return out, nil
}

// RevokeAPIToken revokes the active token with the name, or returns
// ErrNotFound when there is none.
func (s *Store) RevokeAPIToken(ctx context.Context, name string, at time.Time) error {
	tag, err := s.db.Exec(ctx, `UPDATE api_tokens SET revoked_at = $2 WHERE name = $1 AND revoked_at IS NULL`, name, at)
	if err != nil {
		return fmt.Errorf("revoke api token %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("revoke api token %q", name)
	}
	return nil
}

// UpdateAPITokenLastUsed sets the last-used time of the token with the ID,
// or returns ErrNotFound.
func (s *Store) UpdateAPITokenLastUsed(ctx context.Context, id int64, at time.Time) error {
	tag, err := s.db.Exec(ctx, `UPDATE api_tokens SET last_used_at = $2 WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("update api token %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("update api token %d", id)
	}
	return nil
}

// nonNil returns b, or an empty slice for nil, because the column is NOT
// NULL.
func nonNil(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}
