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

// maxClients bounds the addresses that a Limiter tracks. A new address
// beyond it makes the Limiter forget the address it saw longest ago, so
// that many addresses cannot lock the owner out.
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
// count. The addresses of one IPv6 /64 network share one bucket, since
// one client usually holds the whole network.
func (l *Limiter) Allow(addr string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastSweep) >= l.window {
		l.sweep(now)
	}
	key := limitKey(addr)
	c, ok := l.clients[key]
	if !ok {
		if len(l.clients) >= maxClients {
			l.forgetOldest()
		}
		c = &client{bucket: rate.NewLimiter(l.limit, l.burst)}
		l.clients[key] = c
	}
	c.lastSeen = now
	r := c.bucket.ReserveN(now, 1)
	if d := r.DelayFrom(now); d > 0 {
		r.CancelAt(now)
		return false, d
	}
	return true, 0
}

// limitKey returns the /64 network of an IPv6 address, and any other
// address as it is.
func limitKey(addr string) string {
	ip, err := netip.ParseAddr(addr)
	if err != nil || !ip.Is6() || ip.Is4In6() {
		return addr
	}
	prefix, _ := ip.Prefix(64) // cannot fail for an IPv6 address and 64 bits
	return prefix.String()
}

func (l *Limiter) forgetOldest() {
	var oldest string
	var at time.Time
	for addr, c := range l.clients {
		if oldest == "" || c.lastSeen.Before(at) {
			oldest, at = addr, c.lastSeen
		}
	}
	delete(l.clients, oldest)
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
// header name, it reads that header, every line of it as one
// comma-separated list, and takes the entry hops places from the right:
// each of hops trusted proxies appends one entry, so 1 takes the entry
// that the nearest proxy wrote. It falls back to the remote address of the
// connection when the list is shorter than hops or the entry is not a
// valid address. Only name a header that the proxies always set, and the
// number of them, or a client can choose its own address.
func ClientIP(r *http.Request, trustedHeader string, hops int) string {
	if trustedHeader != "" && hops >= 1 {
		var entries []string
		for _, line := range r.Header.Values(trustedHeader) {
			for e := range strings.SplitSeq(line, ",") {
				if e = strings.TrimSpace(e); e != "" {
					entries = append(entries, e)
				}
			}
		}
		if len(entries) >= hops {
			if addr, err := netip.ParseAddr(entries[len(entries)-hops]); err == nil {
				return addr.Unmap().String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
