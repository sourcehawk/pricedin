package auth

// SetVerifyHook makes Login call f after each Argon2id verification.
func SetVerifyHook(s *Service, f func()) {
	s.onVerify = f
}

// LimiterSize returns how many client addresses l tracks.
func LimiterSize(l *Limiter) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.clients)
}

// MaxClients is the most addresses a Limiter tracks.
const MaxClients = maxClients
