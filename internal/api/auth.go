package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sessionCookie names the cookie exchanged for the launch token.
const sessionCookie = "dmtx_session"

const (
	sessionLifetime  = 8 * time.Hour
	failedAuthLimit  = 8
	failedAuthWindow = time.Minute
)

// newToken returns a hex-encoded cryptographically random secret.
//
// The token exists so that binding to loopback is not, by itself, the
// authorization boundary. Any web page the operator visits can issue requests
// to 127.0.0.1, so a server that trusts everything reaching the port trusts
// every site the operator browses. Since the token is generated at startup and
// carried in the URL the browser is opened at, the operator never sees or types
// it: the access is one click and still authenticated.
func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// authenticator holds the launch token and the session secret it is exchanged
// for.
//
// They are separate values, and the launch token really is single-use: it is
// cleared the first time it is redeemed. Reusing one secret for both would make
// the URL a long-lived bearer credential, so anywhere that URL came to rest -
// a shell history, a pasted message, a screenshot - would stay usable for as
// long as the server ran. Describing it as one-time while accepting it forever
// is the kind of claim this codebase keeps finding in its own tests.
type authenticator struct {
	mutex         sync.Mutex
	launch        string
	session       string
	sessionIssued time.Time
	host          string
	failures      map[string]authFailure
}

type authFailure struct {
	count int
	until time.Time
}

// grant exchanges a correct launch token for a session cookie and redirects to
// the console.
//
// Redirecting matters beyond tidiness: it removes the token from the address
// bar, so it does not sit in browser history or get copied out of a screenshot
// when an operator shares one.
func (auth *authenticator) grant(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	supplied := request.URL.Query().Get("token")
	session, ok := auth.redeemSession(supplied)
	if !ok {
		http.Error(writer, "invalid or missing token", http.StatusUnauthorized)
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name:     sessionCookie,
		Value:    session,
		Path:     "/",
		HttpOnly: true,
		// Strict rather than Lax: no cross-site navigation should ever arrive
		// carrying this session, because every route behind it can start or
		// abandon a migration.
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionLifetime.Seconds()),
		// Not Secure: the server is loopback-only plaintext by design, and a
		// Secure cookie would simply never be sent.
	})
	http.Redirect(writer, request, "/", http.StatusFound)
}

func (auth *authenticator) redeemSession(supplied string) (string, bool) {
	replacement, err := newToken()
	if err != nil {
		return "", false
	}
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	if !constantTimeEqual(supplied, auth.launch) {
		return "", false
	}
	auth.launch = ""
	// A successful launch redemption establishes a new single-operator
	// session. Rotating rather than merely refreshing a timestamp prevents an
	// old cookie from inheriting a later login's absolute lifetime.
	auth.session = replacement
	auth.sessionIssued = time.Now()
	return replacement, true
}

// remint issues a replacement launch token, so a second invocation can be sent
// to this server with a URL that is single-use like the original.
//
// It replaces rather than adds: at most one launch token is outstanding, so two
// handoffs racing leave only the later one usable. The operator whose browser
// arrives with the older token is told the token is invalid and runs the
// command again, which is a worse morning than a queue of valid tokens would
// give them but a much better one than a URL that stays live.
func (auth *authenticator) remint() (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	auth.launch = token
	return token, nil
}

// holdsSession reports whether a value is the session secret.
func (auth *authenticator) holdsSession(supplied string) bool {
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	return !auth.sessionIssued.IsZero() && time.Since(auth.sessionIssued) <= sessionLifetime && constantTimeEqual(supplied, auth.session)
}

// constantTimeEqual compares without returning early on the first differing
// byte, which would leak its position to anything that can time the call.
func constantTimeEqual(supplied, expected string) bool {
	if supplied == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) == 1
}

// require wraps a handler so it only runs for an authenticated request.
//
// A bearer header is accepted alongside the cookie so scripts and the CLI's own
// parity tests can call the API without pretending to be a browser.
func (auth *authenticator) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		peer := peerIdentity(request)
		cookieAuthenticated := false
		if cookie, err := request.Cookie(sessionCookie); err == nil && auth.holdsSession(cookie.Value) {
			cookieAuthenticated = true
		}
		header := request.Header.Get("Authorization")
		bearerAuthenticated := false
		if supplied, found := strings.CutPrefix(header, "Bearer "); found && auth.holdsSession(supplied) {
			bearerAuthenticated = true
		}
		if cookieAuthenticated || bearerAuthenticated {
			// The listener itself only accepts loopback peers. Restricting Host
			// and Origin validation to that served transport keeps in-process
			// httptest route checks from pretending to be a network boundary.
			if loopbackPeer(request) && (!auth.validHost(request.Host) || (auth.host != "" && cookieAuthenticated && unsafeMethod(request.Method) && !sameOrigin(request, auth.host))) {
				writer.Header().Set("Cache-Control", "no-store")
				writeJSON(writer, http.StatusForbidden, map[string]string{"error": "request origin is not permitted"})
				return
			}
			auth.clearFailure(peer)
			writer.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(writer, request)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		if retry := auth.recordFailure(peer); retry > 0 {
			writer.Header().Set("Retry-After", strconv.Itoa(retry))
			writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": "too many failed authentication attempts"})
			return
		}
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
	})
}

func peerIdentity(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if request.RemoteAddr != "" {
		return request.RemoteAddr
	}
	return "unknown"
}

func loopbackPeer(request *http.Request) bool {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func (auth *authenticator) recordFailure(peer string) int {
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	now := time.Now()
	failure := auth.failures[peer]
	if now.Before(failure.until) {
		return int(time.Until(failure.until).Seconds()) + 1
	}
	failure.count++
	if failure.count >= failedAuthLimit {
		failure.until = now.Add(failedAuthWindow)
		failure.count = 0
		auth.failures[peer] = failure
		return int(failedAuthWindow.Seconds())
	}
	auth.failures[peer] = failure
	return 0
}

func (auth *authenticator) clearFailure(peer string) {
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	delete(auth.failures, peer)
}

func (auth *authenticator) validHost(host string) bool { return auth.host == "" || host == auth.host }
func unsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}
func sameOrigin(request *http.Request, host string) bool {
	origin := request.Header.Get("Origin")
	return origin == "http://"+host
}
