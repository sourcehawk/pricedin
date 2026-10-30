package auth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/sourcehawk/pricedin/server/internal/auth"
)

// fastParams keep the tests quick. Production hashes use DefaultParams.
var fastParams = auth.Params{Memory: 64, Time: 1, Threads: 1}

func TestHashPasswordGivesAnArgon2idPHCStringThatVerifies(t *testing.T) {
	phc, err := auth.HashPassword("correct horse battery", fastParams)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !strings.HasPrefix(phc, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Errorf("HashPassword() = %q, want an argon2id PHC string with the params", phc)
	}
	h, err := auth.ParseHash(phc)
	if err != nil {
		t.Fatalf("ParseHash() error = %v", err)
	}
	if !h.Verify("correct horse battery") {
		t.Error("Verify(right password) = false, want true")
	}
	if h.Verify("correct horse batterz") {
		t.Error("Verify(wrong password) = true, want false")
	}
}

func TestHashPasswordUsesANewSaltEachTime(t *testing.T) {
	a, _ := auth.HashPassword("correct horse battery", fastParams)
	b, _ := auth.HashPassword("correct horse battery", fastParams)
	if a == b {
		t.Error("two hashes of one password are equal, want a random salt")
	}
}

func TestHashPasswordRejectsAShortPassword(t *testing.T) {
	_, err := auth.HashPassword("short", fastParams)
	if !errors.Is(err, auth.ErrWeakPassword) {
		t.Errorf("HashPassword(short) error = %v, want ErrWeakPassword", err)
	}
}

func TestDefaultParamsMeetTheOWASPMinimum(t *testing.T) {
	p := auth.DefaultParams()
	if p.Memory < 19456 || p.Time < 2 || p.Threads < 1 {
		t.Errorf("DefaultParams() = %+v, want at least m=19456, t=2, p=1", p)
	}
}

func TestParseHashRejectsMalformedStrings(t *testing.T) {
	good, _ := auth.HashPassword("correct horse battery", fastParams)
	parts := strings.Split(good, "$")
	tests := map[string]string{
		"empty":            "",
		"bcrypt":           "$2y$10$abcdefghijklmnopqrstuv",
		"argon2i":          strings.Replace(good, "argon2id", "argon2i", 1),
		"wrong version":    strings.Replace(good, "v=19", "v=16", 1),
		"missing params":   "$argon2id$v=19$" + parts[4] + "$" + parts[5],
		"bad salt":         strings.Join([]string{"", parts[1], parts[2], parts[3], "!!", parts[5]}, "$"),
		"huge memory":      strings.Replace(good, "m=64", "m=99999999", 1),
		"zero time":        strings.Replace(good, "t=1", "t=0", 1),
		"short key":        strings.Join([]string{"", parts[1], parts[2], parts[3], parts[4], "AAAA"}, "$"),
		"trailing garbage": good + "$x",
	}
	for name, phc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := auth.ParseHash(phc); err == nil {
				t.Errorf("ParseHash(%q) error = nil, want an error", phc)
			}
		})
	}
}
