package auth_test

import (
	"fmt"
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
		name, remote, header string
		hops                 int
		values               []string
		want                 string
	}{
		{"remote address", "192.0.2.1:5555", "", 1, nil, "192.0.2.1"},
		{"remote IPv6", "[2001:db8::1]:5555", "", 1, nil, "2001:db8::1"},
		{"header ignored when not trusted", "192.0.2.1:5555", "", 1, []string{"203.0.113.9"}, "192.0.2.1"},
		{"trusted header", "10.0.0.1:5555", "Fly-Client-IP", 1, []string{"203.0.113.9"}, "203.0.113.9"},
		{"one hop takes the last entry", "10.0.0.1:5555", "X-Forwarded-For", 1, []string{"198.51.100.7, 203.0.113.9"}, "203.0.113.9"},
		{"two hops skip the nearest proxy", "10.0.0.1:5555", "X-Forwarded-For", 2, []string{"198.51.100.7, 203.0.113.9"}, "198.51.100.7"},
		{"a repeated header is one list", "10.0.0.1:5555", "X-Forwarded-For", 2, []string{"6.6.6.6, 198.51.100.7", "203.0.113.9"}, "198.51.100.7"},
		{"too few entries falls back", "10.0.0.1:5555", "X-Forwarded-For", 3, []string{"198.51.100.7, 203.0.113.9"}, "10.0.0.1"},
		{"invalid entry falls back", "10.0.0.1:5555", "Fly-Client-IP", 1, []string{"not-an-ip"}, "10.0.0.1"},
		{"missing header falls back", "10.0.0.1:5555", "Fly-Client-IP", 1, nil, "10.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			r.RemoteAddr = tt.remote
			h := tt.header
			if h == "" {
				h = "Fly-Client-IP"
			}
			for _, v := range tt.values {
				r.Header.Add(h, v)
			}
			if got := auth.ClientIP(r, tt.header, tt.hops); got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLimiterKeysIPv6ByItsSlash64(t *testing.T) {
	c := authtest.NewClock(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
	l := auth.NewLimiter(1, time.Minute, c.Now)
	if ok, _ := l.Allow("2001:db8:1:2::1"); !ok {
		t.Fatal("first attempt refused")
	}
	if ok, _ := l.Allow("2001:db8:1:2:ffff::9"); ok {
		t.Error("another address of the same /64 was allowed, want it to share the bucket")
	}
	if ok, _ := l.Allow("2001:db8:1:3::1"); !ok {
		t.Error("an address of another /64 was refused")
	}
	if ok, _ := l.Allow("192.0.2.1"); !ok {
		t.Error("an IPv4 address was refused")
	}
}

func TestAFullLimiterForgetsTheOldestAddress(t *testing.T) {
	c := authtest.NewClock(time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC))
	l := auth.NewLimiter(1, time.Minute, c.Now)
	l.Allow("10.255.255.255") // the oldest
	c.Advance(time.Second)
	for i := range auth.MaxClients - 1 {
		l.Allow(fmt.Sprintf("10.%d.%d.%d", i>>16&255, i>>8&255, i&255))
	}
	if ok, _ := l.Allow("192.0.2.1"); !ok {
		t.Fatal("a new address was refused while the limiter was full, want the oldest forgotten")
	}
	if n := auth.LimiterSize(l); n != auth.MaxClients {
		t.Errorf("tracked addresses = %d, want %d", n, auth.MaxClients)
	}
	if ok, _ := l.Allow("10.255.255.255"); !ok {
		t.Error("the forgotten oldest address was refused, want a new bucket")
	}
}
