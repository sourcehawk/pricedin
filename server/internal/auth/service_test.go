package auth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/auth"
)

const (
	ownerEmail    = "owner@example.com"
	ownerPassword = "correct horse battery"
	ttl           = 30 * 24 * time.Hour
)

type fixture struct {
	svc   *auth.Service
	store *memStore
	clock *clock
	// verifications counts the Argon2id verifications that Login ran.
	verifications *int
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	phc, err := auth.HashPassword(ownerPassword, fastParams)
	if err != nil {
		t.Fatal(err)
	}
	return newFixtureWith(t, auth.Options{OwnerEmail: ownerEmail, OwnerPasswordHash: phc})
}

func newFixtureWith(t *testing.T, opts auth.Options) fixture {
	t.Helper()
	st := newMemStore()
	c := &clock{now: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
	opts.SessionTTL = ttl
	opts.Now = c.Now
	svc, err := auth.New(st, opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	n := 0
	auth.SetVerifyHook(svc, func() { n++ })
	return fixture{svc: svc, store: st, clock: c, verifications: &n}
}

func login(t *testing.T, f fixture) string {
	t.Helper()
	token, _, err := f.svc.Login(context.Background(), ownerEmail, ownerPassword, auth.Client{IP: "192.0.2.1", UserAgent: "test"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	return token
}

func TestLoginWithTheOwnerCredentialsStartsASession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token, sess, err := f.svc.Login(ctx, "  Owner@Example.COM ", ownerPassword, auth.Client{IP: "192.0.2.1", UserAgent: "test"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if want := f.clock.Now().Add(ttl); !sess.ExpiresAt.Equal(want) || sess.Email != ownerEmail {
		t.Errorf("Login() session = %+v, want email %s expiring at %v", sess, ownerEmail, want)
	}
	got, err := f.svc.Session(ctx, token)
	if err != nil {
		t.Fatalf("Session() error = %v", err)
	}
	if got != sess {
		t.Errorf("Session() = %+v, want %+v", got, sess)
	}
}

func TestLoginStoresOnlyTheHashOfTheToken(t *testing.T) {
	f := newFixture(t)
	token := login(t, f)
	sum := sha256.Sum256([]byte(token))
	if _, ok := f.store.sessions[string(sum[:])]; !ok || len(f.store.sessions) != 1 {
		t.Errorf("stored sessions = %d, want one keyed by the SHA-256 of the token", len(f.store.sessions))
	}
	for k, s := range f.store.sessions {
		if bytes.Contains([]byte(k), []byte(token)) || bytes.Contains(s.TokenHash, []byte(token)) {
			t.Error("the store holds the token itself")
		}
	}
}

func TestLoginGivesTheSameErrorForAWrongPasswordAndAnUnknownEmail(t *testing.T) {
	tests := map[string]struct{ email, password string }{
		"wrong password": {ownerEmail, "wrong horse battery"},
		"unknown email":  {"stranger@example.com", ownerPassword},
		"empty":          {"", ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			_, _, err := f.svc.Login(context.Background(), tt.email, tt.password, auth.Client{})
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Errorf("Login() error = %v, want ErrInvalidCredentials", err)
			}
			if *f.verifications != 1 {
				t.Errorf("Login() ran %d Argon2id verifications, want 1 so that the timing does not tell", *f.verifications)
			}
			if len(f.store.sessions) != 0 {
				t.Error("a failed login stored a session")
			}
		})
	}
}

func TestLoginWithoutAnOwnerIsDisabled(t *testing.T) {
	f := newFixtureWith(t, auth.Options{})
	_, _, err := f.svc.Login(context.Background(), ownerEmail, ownerPassword, auth.Client{})
	if !errors.Is(err, auth.ErrLoginDisabled) {
		t.Errorf("Login() error = %v, want ErrLoginDisabled", err)
	}
}

func TestNewRejectsAMalformedOwnerHash(t *testing.T) {
	_, err := auth.New(newMemStore(), auth.Options{OwnerEmail: ownerEmail, OwnerPasswordHash: "$argon2id$nonsense", SessionTTL: ttl})
	if err == nil {
		t.Error("New() error = nil, want an error for a malformed hash")
	}
}

func TestSessionRejectsUnknownAndEmptyTokens(t *testing.T) {
	f := newFixture(t)
	login(t, f)
	for _, token := range []string{"", "not-a-token", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"} {
		if _, err := f.svc.Session(context.Background(), token); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Errorf("Session(%q) error = %v, want ErrUnauthenticated", token, err)
		}
	}
}

func TestSessionExpiresAfterTheTTLAndIsDeleted(t *testing.T) {
	f := newFixture(t)
	token := login(t, f)
	f.clock.Advance(ttl - time.Second)
	if _, err := f.svc.Session(context.Background(), token); err != nil {
		t.Fatalf("Session() just before expiry error = %v", err)
	}
	f.clock.Advance(time.Second)
	if _, err := f.svc.Session(context.Background(), token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("Session() at expiry error = %v, want ErrUnauthenticated", err)
	}
	if len(f.store.sessions) != 0 {
		t.Error("the expired session is still stored")
	}
}

func TestSessionUpdatesLastSeenAtMostOnceAMinute(t *testing.T) {
	f := newFixture(t)
	token := login(t, f)
	ctx := context.Background()
	for range 5 {
		f.clock.Advance(10 * time.Second)
		if _, err := f.svc.Session(ctx, token); err != nil {
			t.Fatal(err)
		}
	}
	if f.store.lastSeenWrites != 0 {
		t.Fatalf("last-seen writes within the first minute = %d, want 0", f.store.lastSeenWrites)
	}
	f.clock.Advance(10 * time.Second)
	if _, err := f.svc.Session(ctx, token); err != nil {
		t.Fatal(err)
	}
	if f.store.lastSeenWrites != 1 {
		t.Errorf("last-seen writes after a minute = %d, want 1", f.store.lastSeenWrites)
	}
	for _, s := range f.store.sessions {
		if !s.LastSeenAt.Equal(f.clock.Now()) {
			t.Errorf("LastSeenAt = %v, want %v", s.LastSeenAt, f.clock.Now())
		}
	}
}

func TestLogoutEndsTheSessionAndCanRepeat(t *testing.T) {
	f := newFixture(t)
	token := login(t, f)
	ctx := context.Background()
	if err := f.svc.Logout(ctx, token); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := f.svc.Session(ctx, token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("Session() after logout error = %v, want ErrUnauthenticated", err)
	}
	if err := f.svc.Logout(ctx, token); err != nil {
		t.Errorf("second Logout() error = %v, want nil", err)
	}
}

func TestCreatedAPITokenAuthenticatesUntilRevoked(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token, created, err := f.svc.CreateAPIToken(ctx, "agent-1")
	if err != nil {
		t.Fatalf("CreateAPIToken() error = %v", err)
	}
	if created.Name != "agent-1" || !created.CreatedAt.Equal(f.clock.Now()) {
		t.Errorf("CreateAPIToken() = %+v, want name agent-1 created now", created)
	}
	if sum := sha256.Sum256([]byte(token)); !bytes.Equal(f.store.tokens[0].TokenHash, sum[:]) {
		t.Error("the store does not hold the SHA-256 of the token")
	}
	got, err := f.svc.APIToken(ctx, token)
	if err != nil || got.Name != "agent-1" {
		t.Fatalf("APIToken() = %+v, %v; want agent-1", got, err)
	}
	if err := f.svc.RevokeAPIToken(ctx, "agent-1"); err != nil {
		t.Fatalf("RevokeAPIToken() error = %v", err)
	}
	if _, err := f.svc.APIToken(ctx, token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("APIToken() after revoke error = %v, want ErrUnauthenticated", err)
	}
}

func TestAPITokenUpdatesLastUsedAtMostOnceAMinute(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token, _, err := f.svc.CreateAPIToken(ctx, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := f.svc.APIToken(ctx, token); err != nil {
			t.Fatal(err)
		}
		f.clock.Advance(10 * time.Second)
	}
	if f.store.lastUsedWrites != 1 {
		t.Fatalf("last-used writes in 30 seconds = %d, want 1 (the first use)", f.store.lastUsedWrites)
	}
	f.clock.Advance(time.Minute)
	if _, err := f.svc.APIToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	if f.store.lastUsedWrites != 2 {
		t.Errorf("last-used writes after a minute = %d, want 2", f.store.lastUsedWrites)
	}
}

func TestCreateAPITokenRejectsBadAndTakenNames(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, _, err := f.svc.CreateAPIToken(ctx, "agent-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.CreateAPIToken(ctx, "agent-1"); !errors.Is(err, auth.ErrTokenNameTaken) {
		t.Errorf("CreateAPIToken(taken) error = %v, want ErrTokenNameTaken", err)
	}
	for _, name := range []string{"", "has space", "slash/name", string(make([]byte, 65))} {
		if _, _, err := f.svc.CreateAPIToken(ctx, name); !errors.Is(err, auth.ErrInvalidTokenName) {
			t.Errorf("CreateAPIToken(%q) error = %v, want ErrInvalidTokenName", name, err)
		}
	}
}

func TestRevokeAPITokenReportsAnUnknownName(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.RevokeAPIToken(context.Background(), "nobody"); !errors.Is(err, auth.ErrTokenNotFound) {
		t.Errorf("RevokeAPIToken(unknown) error = %v, want ErrTokenNotFound", err)
	}
}

func TestTokensAreRandomAndURLSafe(t *testing.T) {
	f := newFixture(t)
	a := login(t, f)
	b := login(t, f)
	if a == b {
		t.Error("two logins gave the same token")
	}
	if len(a) != 43 {
		t.Errorf("token length = %d, want 43 (32 bytes in unpadded base64url)", len(a))
	}
	for _, r := range a {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			t.Fatalf("token %q has the character %q, want base64url only", a, r)
		}
	}
}
