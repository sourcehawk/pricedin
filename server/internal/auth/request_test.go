package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sourcehawk/pricedin/server/internal/auth"
	"github.com/sourcehawk/pricedin/server/internal/auth/authtest"
)

func TestLimiterAllowsTheBurstThenAsksToWait(t *testing.T) {
	c := authtest.NewClock(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
	l := auth.NewLimiter(5, time.Minute, c.Now)
	for i := range 5 {
		if ok, _ := l.Allow("192.0.2.1"); !ok {
			t.Fatalf("attempt %d refused, want the first 5 allowed", i+1)
		}
	}
	ok, wait := l.Allow("192.0.2.1")
	if ok {
		t.Fatal("attempt 6 allowed, want it refused")
	}
	if wait <= 0 || wait > 12*time.Second {
		t.Errorf("wait = %v, want up to 12s (one attempt back every 12s)", wait)
	}
	if ok, _ := l.Allow("192.0.2.2"); !ok {
		t.Error("another address was refused, want each address limited on its own")
	}
	c.Advance(wait)
	if ok, _ := l.Allow("192.0.2.1"); !ok {
		t.Error("attempt after the wait refused, want it allowed")
	}
}

func TestLimiterRefusedAttemptsDoNotExtendTheWait(t *testing.T) {
	c := authtest.NewClock(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
	l := auth.NewLimiter(5, time.Minute, c.Now)
	for range 5 {
		l.Allow("a")
	}
	_, first := l.Allow("a")
	_, second := l.Allow("a")
	if second != first {
		t.Errorf("wait after a second refusal = %v, want %v as before", second, first)
	}
}

func TestLimiterForgetsIdleAddresses(t *testing.T) {
	c := authtest.NewClock(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
	l := auth.NewLimiter(5, time.Minute, c.Now)
	l.Allow("a")
	c.Advance(2 * time.Minute)
	l.Allow("b")
	if n := auth.LimiterSize(l); n != 1 {
		t.Errorf("tracked addresses = %d, want 1 after the idle one was dropped", n)
	}
}

func TestOriginsAllowOnlyTheListedOrigins(t *testing.T) {
	o := auth.NewOrigins([]string{"https://pricedin.example.com", "http://localhost:3000"})
	tests := map[string]bool{
		"https://pricedin.example.com":       true,
		"HTTPS://PricedIn.Example.com":       true,
		"http://localhost:3000":              true,
		"":                                   false,
		"null":                               false,
		"http://pricedin.example.com":        false,
		"https://pricedin.example.com:8443":  false,
		"https://evil.example.com":           false,
		"https://pricedin.example.com.evil":  false,
		"https://pricedin.example.com/":      false,
		"https://pricedin.example.com/login": false,
	}
	for origin, want := range tests {
		if got := o.Allowed(origin); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", origin, got, want)
		}
	}
	if auth.NewOrigins(nil).Allowed("http://localhost:3000") {
		t.Error("an empty list allowed an origin")
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name, remote, header, value, want string
	}{
		{"remote address", "192.0.2.1:5555", "", "", "192.0.2.1"},
		{"remote IPv6", "[2001:db8::1]:5555", "", "", "2001:db8::1"},
		{"header ignored when not trusted", "192.0.2.1:5555", "", "203.0.113.9", "192.0.2.1"},
		{"trusted header", "10.0.0.1:5555", "Fly-Client-IP", "203.0.113.9", "203.0.113.9"},
		{"trusted list takes the last entry", "10.0.0.1:5555", "X-Forwarded-For", "198.51.100.7, 203.0.113.9", "203.0.113.9"},
		{"invalid header falls back", "10.0.0.1:5555", "Fly-Client-IP", "not-an-ip", "10.0.0.1"},
		{"missing header falls back", "10.0.0.1:5555", "Fly-Client-IP", "", "10.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			r.RemoteAddr = tt.remote
			if tt.value != "" {
				h := tt.header
				if h == "" {
					h = "Fly-Client-IP"
				}
				r.Header.Set(h, tt.value)
			}
			if got := auth.ClientIP(r, tt.header); got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}
