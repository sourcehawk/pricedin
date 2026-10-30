package auth

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// maxClients bounds the addresses that a Limiter tracks. When that many
// addresses are active at once, a new address is refused until some go
// idle: the login is for one owner, so refusing is safer than forgetting.
const maxClients = 10000

// Limiter is a token bucket per client address. It is safe for concurrent
// use.
type Limiter struct {
	mu        sync.Mutex
	limit     rate.Limit
	burst     int
	window    time.Duration
	now       func() time.Time
	clients   map[string]*client
	lastSweep time.Time
}

type client struct {
	bucket   *rate.Limiter
	lastSeen time.Time
}

// NewLimiter allows each address attempts within window, as a burst, and
// gives back one attempt every window/attempts. now nil means time.Now.
func NewLimiter(attempts int, window time.Duration, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{
		limit: rate.Every(window / time.Duration(attempts)), burst: attempts, window: window,
		now: now, clients: map[string]*client{},
	}
}

// Allow takes one attempt for the address. When none is left, it returns
// false and how long to wait for the next one; a refused attempt does not
// count.
func (l *Limiter) Allow(addr string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastSweep) >= l.window {
		l.sweep(now)
	}
	c, ok := l.clients[addr]
	if !ok {
		if len(l.clients) >= maxClients {
			return false, l.window
		}
		c = &client{bucket: rate.NewLimiter(l.limit, l.burst)}
		l.clients[addr] = c
	}
	c.lastSeen = now
	r := c.bucket.ReserveN(now, 1)
	if d := r.DelayFrom(now); d > 0 {
		r.CancelAt(now)
		return false, d
	}
	return true, 0
}

// sweep drops the addresses that were idle for a whole window: their
// buckets are full again, so a new bucket is the same.
func (l *Limiter) sweep(now time.Time) {
	for addr, c := range l.clients {
		if now.Sub(c.lastSeen) >= l.window {
			delete(l.clients, addr)
		}
	}
	l.lastSweep = now
}

// Origins is a set of web origins, such as "https://pricedin.example.com".
type Origins struct {
	allowed map[string]bool
}

// NewOrigins returns the set of the origins in list, which must be in the
// form that config.Auth.PublicOrigins gives.
func NewOrigins(list []string) Origins {
	o := Origins{allowed: map[string]bool{}}
	for _, s := range list {
		o.allowed[strings.ToLower(s)] = true
	}
	return o
}

// Allowed reports whether origin, the value of an Origin header, is in the
// set. Scheme and host compare without case; anything else must match
// exactly, so "null" and an empty value never match.
func (o Origins) Allowed(origin string) bool {
	return origin != "" && o.allowed[strings.ToLower(origin)]
}

// ClientIP returns the address of the client that sent r. With a trusted
// header name, it takes the address from that header, the last entry of a
// comma-separated list, which the nearest proxy wrote. It falls back to the
// remote address of the connection when the header is missing or holds no
// valid address. Only name a header that the proxy in front of the server
// always sets, or a client can choose its own address.
func ClientIP(r *http.Request, trustedHeader string) string {
	if trustedHeader != "" {
		v := r.Header.Get(trustedHeader)
		if i := strings.LastIndexByte(v, ','); i >= 0 {
			v = v[i+1:]
		}
		if addr, err := netip.ParseAddr(strings.TrimSpace(v)); err == nil {
			return addr.Unmap().String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
