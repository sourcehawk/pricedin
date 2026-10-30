package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params are the Argon2id cost parameters of a password hash.
type Params struct {
	Memory  uint32 // KiB
	Time    uint32 // passes over the memory
	Threads uint8
}

// DefaultParams returns the parameters of new hashes: the OWASP minimum
// for Argon2id (19 MiB, two passes, one thread), which a small server can
// afford on every login.
func DefaultParams() Params {
	return Params{Memory: 19456, Time: 2, Threads: 1}
}

const (
	saltLen = 16
	keyLen  = 32
	// The bounds that ParseHash accepts. They keep a mistyped hash from
	// making each login allocate gigabytes.
	maxMemory  = 1 << 20 // 1 GiB in KiB
	maxTime    = 16
	minKeyLen  = 16
	maxKeyLen  = 64
	minSaltLen = 8
	maxSaltLen = 64
	// MinPasswordLen and maxPasswordLen bound the passwords that
	// HashPassword accepts.
	MinPasswordLen = 12
	maxPasswordLen = 1024
)

// ErrWeakPassword is returned by HashPassword for a password shorter than
// MinPasswordLen bytes or longer than 1024 bytes.
var ErrWeakPassword = errors.New("auth: password must be 12 to 1024 bytes")

// HashPassword returns the Argon2id hash of password with a random salt, as
// a PHC string such as "$argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>".
func HashPassword(password string, p Params) (string, error) {
	if len(password) < MinPasswordLen || len(password) > maxPasswordLen {
		return "", ErrWeakPassword
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	h := Hash{Params: p, salt: salt}
	h.key = h.derive(password, keyLen)
	return h.String(), nil
}

// Hash is a parsed Argon2id password hash.
type Hash struct {
	Params
	salt, key []byte
}

// ParseHash parses an Argon2id PHC string, version 19. It rejects other
// algorithms and parameters beyond 1 GiB of memory, 16 passes, 255
// threads, keys of 16 to 64 bytes and salts of 8 to 64 bytes.
func ParseHash(phc string) (Hash, error) {
	bad := func(why string) (Hash, error) {
		return Hash{}, fmt.Errorf("auth: parse password hash: %s", why)
	}
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return bad("want $argon2id$v=19$m=<KiB>,t=<passes>,p=<threads>$<salt>$<key>")
	}
	if parts[2] != "v=19" {
		return bad("want version v=19")
	}
	var h Hash
	seen := map[string]bool{}
	for kv := range strings.SplitSeq(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		n, err := strconv.ParseUint(v, 10, 32)
		if !ok || err != nil || seen[k] {
			return bad("malformed parameters")
		}
		seen[k] = true
		switch k {
		case "m":
			h.Memory = uint32(n)
		case "t":
			h.Time = uint32(n)
		case "p":
			if n > 255 {
				return bad("too many threads")
			}
			h.Threads = uint8(n)
		default:
			return bad("unknown parameter " + k)
		}
	}
	switch {
	case len(seen) != 3:
		return bad("want the parameters m, t and p")
	case h.Time < 1 || h.Time > maxTime || h.Threads < 1:
		return bad("time and threads out of range")
	case h.Memory < 8*uint32(h.Threads) || h.Memory > maxMemory:
		return bad("memory out of range")
	}
	var err error
	if h.salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil || len(h.salt) < minSaltLen || len(h.salt) > maxSaltLen {
		return bad("malformed salt")
	}
	if h.key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil || len(h.key) < minKeyLen || len(h.key) > maxKeyLen {
		return bad("malformed key")
	}
	return h, nil
}

// Verify reports whether password matches the hash. Its run time does not
// depend on how much of the key matches.
func (h Hash) Verify(password string) bool {
	return subtle.ConstantTimeCompare(h.derive(password, uint32(len(h.key))), h.key) == 1 //nolint:gosec // G115: ParseHash bounds the key to 64 bytes
}

// String returns the PHC string of the hash.
func (h Hash) String() string {
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", h.Memory, h.Time, h.Threads,
		base64.RawStdEncoding.EncodeToString(h.salt), base64.RawStdEncoding.EncodeToString(h.key))
}

func (h Hash) derive(password string, n uint32) []byte {
	return argon2.IDKey([]byte(password), h.salt, h.Time, h.Memory, h.Threads, n)
}
