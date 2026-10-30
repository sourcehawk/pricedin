package httpapi_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func loginRequest(t *testing.T, password string) *http.Request {
	t.Helper()
	req := request(t, http.MethodPost, "/api/auth/login", map[string]string{"email": ownerEmail, "password": password})
	req.Header.Set("Origin", webOrigin)
	return req
}

func TestLoginSetsASecureSessionCookie(t *testing.T) {
	h := newHarness(t)
	res := h.do(t, loginRequest(t, ownerPassword), true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.StatusCode, res.body)
	}
	var sess map[string]any
	res.json(t, &sess)
	if sess["email"] != ownerEmail {
		t.Errorf("session = %v, want the owner's email", sess)
	}
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v, want one", cookies)
	}
	c := cookies[0]
	if c.Name != "pricedin_session" || c.Value == "" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != int(sessionTTL.Seconds()) {
		t.Errorf("cookie = %+v, want pricedin_session, HttpOnly, Secure, SameSite=Lax, Path=/, Max-Age 30 days", c)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestLoginCookieIsNotSecureWhenTurnedOff(t *testing.T) {
	h := newHarness(t, withInsecureCookie())
	if c := h.login(t); c.Secure {
		t.Error("cookie is Secure, want it without Secure for local http")
	}
}

func TestSessionAnswersWithTheCookieOnly(t *testing.T) {
	h := newHarness(t)
	c := h.login(t)
	res := h.do(t, withCookie(request(t, http.MethodGet, "/api/auth/session", nil), c), true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d, body %s", res.StatusCode, res.body)
	}
	h.do(t, request(t, http.MethodGet, "/api/auth/session", nil), true).problem(t, http.StatusUnauthorized, "unauthenticated")
	h.do(t, withBearer(request(t, http.MethodGet, "/api/auth/session", nil), h.token(t)), true).problem(t, http.StatusUnauthorized, "unauthenticated")
}

func TestExpiredSessionIsRejectedAndItsCookieCleared(t *testing.T) {
	h := newHarness(t)
	c := h.login(t)
	h.clock.Advance(sessionTTL)
	res := h.do(t, withCookie(request(t, http.MethodGet, "/api/v1/status", nil), c), true)
	res.problem(t, http.StatusUnauthorized, "unauthenticated")
	cleared := false
	for _, k := range res.Cookies() {
		cleared = cleared || (k.Name == "pricedin_session" && k.MaxAge < 0)
	}
	if !cleared {
		t.Errorf("cookies = %v, want the session cookie cleared", res.Cookies())
	}
}

func TestLoginWithWrongCredentialsFails(t *testing.T) {
	h := newHarness(t)
	for name, body := range map[string]map[string]string{
		"wrong password": {"email": ownerEmail, "password": "wrong horse battery"},
		"unknown email":  {"email": "x@example.com", "password": ownerPassword},
	} {
		t.Run(name, func(t *testing.T) {
			req := request(t, http.MethodPost, "/api/auth/login", body)
			req.Header.Set("Origin", webOrigin)
			res := h.do(t, req, true)
			p := res.problem(t, http.StatusUnauthorized, "invalid-credentials")
			if len(res.Cookies()) != 0 {
				t.Error("a failed login set a cookie")
			}
			if strings.Contains(p["detail"].(string), "email") && !strings.Contains(p["detail"].(string), "password") {
				t.Errorf("detail %q tells which credential was wrong", p["detail"])
			}
		})
	}
}

func TestLoginNeedsTheWebAppsOrigin(t *testing.T) {
	h := newHarness(t)
	for name, origin := range map[string]string{"no origin": "", "foreign origin": "https://evil.example.com", "null origin": "null"} {
		t.Run(name, func(t *testing.T) {
			req := request(t, http.MethodPost, "/api/auth/login", map[string]string{"email": ownerEmail, "password": ownerPassword})
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			h.do(t, req, false).problem(t, http.StatusForbidden, "forbidden-origin")
		})
	}
}

func TestLoginNeedsJSON(t *testing.T) {
	h := newHarness(t)
	req := request(t, http.MethodPost, "/api/auth/login", nil)
	req.Header.Set("Origin", webOrigin)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.do(t, req, false).problem(t, http.StatusUnsupportedMediaType, "unsupported-media-type")
}

func TestLoginRejectsMalformedBodies(t *testing.T) {
	h := newHarness(t)
	i := 0
	for name, body := range map[string]string{
		"not JSON":       `{"email":`,
		"unknown field":  `{"email":"owner@example.com","password":"x","admin":true}`,
		"missing field":  `{"email":"owner@example.com"}`,
		"two objects":    `{"email":"a","password":"b"}{}`,
		"wrong type":     `{"email":1,"password":"b"}`,
		"array, not obj": `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			req := request(t, http.MethodPost, "/api/auth/login", nil)
			req.Body = readCloser(body)
			// Malformed logins count toward the rate limit too.
			i++
			req.RemoteAddr = "198.51.100." + strconv.Itoa(i) + ":1"
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
			req.Header.Set("Origin", webOrigin)
			h.do(t, req, false).problem(t, http.StatusBadRequest, "invalid-request")
		})
	}
}

func TestLoginRateLimitPerAddress(t *testing.T) {
	h := newHarness(t)
	for i := range 5 {
		if res := h.do(t, loginRequest(t, "wrong horse battery"), true); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, res.StatusCode)
		}
	}
	res := h.do(t, loginRequest(t, ownerPassword), true)
	res.problem(t, http.StatusTooManyRequests, "rate-limited")
	if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err != nil || s < 1 || s > 12 {
		t.Errorf("Retry-After = %q, want 1 to 12 seconds", res.Header.Get("Retry-After"))
	}
	other := loginRequest(t, ownerPassword)
	other.RemoteAddr = "192.0.2.99:1"
	if res := h.do(t, other, true); res.StatusCode != http.StatusOK {
		t.Errorf("another address status = %d, want 200", res.StatusCode)
	}
	h.clock.Advance(12 * time.Second)
	if res := h.do(t, loginRequest(t, ownerPassword), true); res.StatusCode != http.StatusOK {
		t.Errorf("status after the wait = %d, want 200", res.StatusCode)
	}
}

func TestLoginRateLimitUsesTheTrustedProxyHeader(t *testing.T) {
	h := newHarness(t, withProxyHeader("Fly-Client-IP"))
	for i := range 6 {
		req := loginRequest(t, "wrong horse battery")
		req.Header.Set("Fly-Client-IP", "203.0.113."+strconv.Itoa(i))
		if res := h.do(t, req, true); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("client %d status = %d, want 401: each client has its own limit", i, res.StatusCode)
		}
	}
}

func TestLoginWithoutAnOwnerIsDisabled(t *testing.T) {
	h := newHarness(t, withoutOwner())
	h.do(t, loginRequest(t, ownerPassword), true).problem(t, http.StatusServiceUnavailable, "login-disabled")
}

func TestLogoutEndsTheSessionAndClearsTheCookie(t *testing.T) {
	h := newHarness(t)
	c := h.login(t)
	req := withCookie(request(t, http.MethodPost, "/api/auth/logout", map[string]any{}), c)
	req.Header.Set("Origin", webOrigin)
	res := h.do(t, req, true)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, body %s", res.StatusCode, res.body)
	}
	if cs := res.Cookies(); len(cs) != 1 || cs[0].Name != "pricedin_session" || cs[0].MaxAge >= 0 {
		t.Errorf("cookies = %v, want the session cookie cleared", cs)
	}
	h.do(t, withCookie(request(t, http.MethodGet, "/api/v1/status", nil), c), true).problem(t, http.StatusUnauthorized, "unauthenticated")
	if len(h.authStore.Sessions) != 0 {
		t.Error("the session is still stored")
	}
}

func TestLogoutNeedsTheWebAppsOrigin(t *testing.T) {
	h := newHarness(t)
	c := h.login(t)
	h.do(t, withCookie(request(t, http.MethodPost, "/api/auth/logout", map[string]any{}), c), false).problem(t, http.StatusForbidden, "forbidden-origin")
	if len(h.authStore.Sessions) != 1 {
		t.Error("a logout without Origin ended the session")
	}
}
