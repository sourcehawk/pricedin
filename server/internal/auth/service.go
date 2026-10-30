// Package auth authenticates the owner of a PricedIn deployment: the
// password login with server-side sessions, the API tokens for scripts and
// agents, the login rate limit, the Origin check and the client address.
// Secrets never reach the store: it keeps SHA-256 hashes of the tokens.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/store"
)

// Store is the part of *store.Store that the service uses.
type Store interface {
	InsertSession(ctx context.Context, sess store.Session) error
	GetSession(ctx context.Context, tokenHash []byte) (store.Session, error)
	UpdateSessionLastSeen(ctx context.Context, tokenHash []byte, at time.Time) error
	DeleteSession(ctx context.Context, tokenHash []byte) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)
	InsertAPIToken(ctx context.Context, name string, tokenHash []byte, createdAt time.Time) (store.APIToken, error)
	GetActiveAPIToken(ctx context.Context, tokenHash []byte) (store.APIToken, error)
	ListAPITokens(ctx context.Context) ([]store.APIToken, error)
	RevokeAPIToken(ctx context.Context, name string, at time.Time) error
	UpdateAPITokenLastUsed(ctx context.Context, id int64, at time.Time) error
}

// The errors of the service. Every other error is a failure of the store.
var (
	// ErrInvalidCredentials is the one answer to a wrong email or a wrong
	// password, so a caller cannot tell which one was wrong.
	ErrInvalidCredentials = errors.New("auth: invalid email or password")
	// ErrLoginDisabled means that no owner is configured.
	ErrLoginDisabled = errors.New("auth: no owner is configured")
	// ErrUnauthenticated means that a session or API token is unknown,
	// expired or revoked.
	ErrUnauthenticated = errors.New("auth: not authenticated")
	// ErrInvalidTokenName means that a token name is not 1 to 64 letters,
	// digits, dots, dashes or underscores.
	ErrInvalidTokenName = errors.New("auth: token name must be 1 to 64 letters, digits, '.', '-' or '_'")
	// ErrTokenNameTaken means that a token, also a revoked one, has the
	// name.
	ErrTokenNameTaken = errors.New("auth: token name already used")
	// ErrTokenNotFound means that no active token has the name.
	ErrTokenNotFound = errors.New("auth: no active token with that name")
)

// Options configure a Service.
type Options struct {
	// OwnerEmail and OwnerPasswordHash (an Argon2id PHC string) identify
	// the owner. Both empty disables the login.
	OwnerEmail        string
	OwnerPasswordHash string
	// SessionTTL is the lifetime of a session from its login.
	SessionTTL time.Duration
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
}

// Session is an owner's login session, without its token.
type Session struct {
	Email     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// TokenInfo describes an API token, without the token.
type TokenInfo struct {
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time // nil while active
}

// Client describes who sends a login, for the session record.
type Client struct {
	IP        string
	UserAgent string
}

// touchInterval is the shortest time between two writes of a session's
// last-seen time or a token's last-used time.
const touchInterval = time.Minute

// verifySlots bounds the Argon2id verifications that run at once, so that
// many logins at the same moment cannot exhaust the server's memory.
const verifySlots = 2

// tokenBytes is the size of a session token or API token before encoding.
const tokenBytes = 32

var tokenName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Service is safe for concurrent use.
type Service struct {
	store      Store
	ownerEmail string
	owner      *Hash
	// decoy has the owner hash's parameters. A login with an unknown email
	// verifies against it, so it takes as long as one with a wrong
	// password.
	decoy    Hash
	ttl      time.Duration
	now      func() time.Time
	slots    chan struct{}
	onVerify func()
}

// New returns a Service. It fails when only one of OwnerEmail and
// OwnerPasswordHash is set, or when the hash does not parse.
func New(st Store, opts Options) (*Service, error) {
	s := &Service{store: st, ttl: opts.SessionTTL, now: opts.Now, slots: make(chan struct{}, verifySlots)}
	if s.now == nil {
		s.now = time.Now
	}
	email := normalizeEmail(opts.OwnerEmail)
	switch {
	case email == "" && opts.OwnerPasswordHash == "":
		return s, nil
	case email == "" || opts.OwnerPasswordHash == "":
		return nil, errors.New("auth: set both the owner email and the password hash, or neither")
	}
	h, err := ParseHash(opts.OwnerPasswordHash)
	if err != nil {
		return nil, err
	}
	s.ownerEmail, s.owner = email, &h
	s.decoy = Hash{Params: h.Params, salt: make([]byte, len(h.salt)), key: make([]byte, len(h.key))}
	if _, err := io.ReadFull(rand.Reader, s.decoy.key); err != nil {
		return nil, fmt.Errorf("auth: decoy hash: %w", err)
	}
	return s, nil
}

// LoginEnabled reports whether an owner is configured.
func (s *Service) LoginEnabled() bool {
	return s.owner != nil
}

// Login checks the owner's credentials and starts a session. It returns
// the session token, which the caller hands to the client and never
// stores, and the session. The email is compared without case. A wrong
// email or password gives ErrInvalidCredentials after the same work;
// without an owner it gives ErrLoginDisabled. It waits while other
// verifications run, until ctx ends.
func (s *Service) Login(ctx context.Context, email, password string, c Client) (string, Session, error) {
	if s.owner == nil {
		return "", Session{}, ErrLoginDisabled
	}
	emailOK := subtle.ConstantTimeCompare([]byte(normalizeEmail(email)), []byte(s.ownerEmail)) == 1
	hash := s.decoy
	if emailOK {
		hash = *s.owner
	}
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return "", Session{}, fmt.Errorf("auth: login: %w", ctx.Err())
	}
	passwordOK := hash.Verify(password)
	<-s.slots
	if s.onVerify != nil {
		s.onVerify()
	}
	if !emailOK || !passwordOK {
		return "", Session{}, ErrInvalidCredentials
	}

	now := s.now()
	if _, err := s.store.DeleteExpiredSessions(ctx, now); err != nil {
		return "", Session{}, fmt.Errorf("auth: login: %w", err)
	}
	token, hashed, err := newToken()
	if err != nil {
		return "", Session{}, err
	}
	sess := store.Session{
		TokenHash: hashed, CreatedAt: now, ExpiresAt: now.Add(s.ttl), LastSeenAt: now,
		UserAgent: truncate(c.UserAgent, 512), IP: truncate(c.IP, 64),
	}
	if err := s.store.InsertSession(ctx, sess); err != nil {
		return "", Session{}, fmt.Errorf("auth: login: %w", err)
	}
	return token, s.view(sess), nil
}

// Session returns the session of token. It gives ErrUnauthenticated for a
// token that is unknown or expired, and for any token while no owner is
// configured. It deletes an expired session.
func (s *Service) Session(ctx context.Context, token string) (Session, error) {
	if s.owner == nil || token == "" {
		return Session{}, ErrUnauthenticated
	}
	hashed := HashToken(token)
	sess, err := s.store.GetSession(ctx, hashed)
	if errors.Is(err, store.ErrNotFound) {
		return Session{}, ErrUnauthenticated
	}
	if err != nil {
		return Session{}, fmt.Errorf("auth: session: %w", err)
	}
	now := s.now()
	if !now.Before(sess.ExpiresAt) {
		if err := s.store.DeleteSession(ctx, hashed); err != nil {
			return Session{}, fmt.Errorf("auth: session: %w", err)
		}
		return Session{}, ErrUnauthenticated
	}
	if now.Sub(sess.LastSeenAt) >= touchInterval {
		err := s.store.UpdateSessionLastSeen(ctx, hashed, now)
		if errors.Is(err, store.ErrNotFound) {
			return Session{}, ErrUnauthenticated
		}
		if err != nil {
			return Session{}, fmt.Errorf("auth: session: %w", err)
		}
	}
	return s.view(sess), nil
}

// Logout ends the session of token. Ending a session that does not exist
// is not an error.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if err := s.store.DeleteSession(ctx, HashToken(token)); err != nil {
		return fmt.Errorf("auth: logout: %w", err)
	}
	return nil
}

// APIToken returns the active API token with this value, or
// ErrUnauthenticated.
func (s *Service) APIToken(ctx context.Context, token string) (TokenInfo, error) {
	if token == "" {
		return TokenInfo{}, ErrUnauthenticated
	}
	t, err := s.store.GetActiveAPIToken(ctx, HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return TokenInfo{}, ErrUnauthenticated
	}
	if err != nil {
		return TokenInfo{}, fmt.Errorf("auth: api token: %w", err)
	}
	now := s.now()
	if t.LastUsedAt == nil || now.Sub(*t.LastUsedAt) >= touchInterval {
		if err := s.store.UpdateAPITokenLastUsed(ctx, t.ID, now); err != nil {
			return TokenInfo{}, fmt.Errorf("auth: api token: %w", err)
		}
		t.LastUsedAt = &now
	}
	return tokenInfo(t), nil
}

// CreateAPIToken makes a new API token with the name and returns the
// token, which is shown once and never stored. It gives
// ErrInvalidTokenName or ErrTokenNameTaken for an unusable name.
func (s *Service) CreateAPIToken(ctx context.Context, name string) (string, TokenInfo, error) {
	if !tokenName.MatchString(name) {
		return "", TokenInfo{}, ErrInvalidTokenName
	}
	token, hashed, err := newToken()
	if err != nil {
		return "", TokenInfo{}, err
	}
	t, err := s.store.InsertAPIToken(ctx, name, hashed, s.now())
	if errors.Is(err, store.ErrConflict) {
		return "", TokenInfo{}, ErrTokenNameTaken
	}
	if err != nil {
		return "", TokenInfo{}, fmt.Errorf("auth: create api token: %w", err)
	}
	return token, tokenInfo(t), nil
}

// ListAPITokens returns every API token, revoked ones too, oldest first.
func (s *Service) ListAPITokens(ctx context.Context) ([]TokenInfo, error) {
	ts, err := s.store.ListAPITokens(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: list api tokens: %w", err)
	}
	out := make([]TokenInfo, len(ts))
	for i, t := range ts {
		out[i] = tokenInfo(t)
	}
	return out, nil
}

// RevokeAPIToken revokes the active token with the name, or gives
// ErrTokenNotFound.
func (s *Service) RevokeAPIToken(ctx context.Context, name string) error {
	err := s.store.RevokeAPIToken(ctx, name, s.now())
	if errors.Is(err, store.ErrNotFound) {
		return ErrTokenNotFound
	}
	if err != nil {
		return fmt.Errorf("auth: revoke api token: %w", err)
	}
	return nil
}

// HashToken returns the SHA-256 of a session token or API token, the form
// in which the store keeps it.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func newToken() (token string, hashed []byte, err error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("auth: new token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

func (s *Service) view(sess store.Session) Session {
	return Session{Email: s.ownerEmail, CreatedAt: sess.CreatedAt, ExpiresAt: sess.ExpiresAt}
}

func tokenInfo(t store.APIToken) TokenInfo {
	return TokenInfo{Name: t.Name, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
