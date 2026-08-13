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
	// sessionLifetime is an absolute cap rather than an idle timeout. A busy
	// browser must not be able to keep a credential valid indefinitely.
	sessionLifetime = 8 * time.Hour

	// Authentication throttling deliberately uses a small, bounded in-process
	// table. The console only listens on loopback, so this protects against a
	// local page repeatedly guessing a credential without turning the server
	// into an unbounded client-address store.
	authFailureLimit  = 5
	authFailureWindow = time.Minute
	authBlockedFor    = time.Minute
	maxAuthClients    = 256
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
	mutex          sync.Mutex
	launch         string
	session        string
	sessionExpires time.Time
	sessionActive  bool
	now            func() time.Time
	failures       map[string]authFailure
}

type authFailure struct {
	count        int
	windowStart  time.Time
	blockedUntil time.Time
	lastSeen     time.Time
}

func newAuthenticator(launch, session string, now func() time.Time) *authenticator {
	if now == nil {
		now = time.Now
	}
	return &authenticator{
		launch:         launch,
		session:        session,
		sessionExpires: now().Add(sessionLifetime),
		now:            now,
		failures:       make(map[string]authFailure),
	}
}

// grant exchanges a correct launch token for a session cookie and redirects to
// the console.
//
// Redirecting matters beyond tidiness: it removes the token from the address
// bar, so it does not sit in browser history or get copied out of a screenshot
// when an operator shares one.
func (auth *authenticator) grant(writer http.ResponseWriter, request *http.Request) {
	client := clientIdentity(request)
	supplied := request.URL.Query().Get("token")
	redeemed, err := auth.redeem(supplied)
	if err != nil {
		http.Error(writer, "could not create session", http.StatusInternalServerError)
		return
	}
	if !redeemed {
		if auth.rateLimited(client) {
			authenticationRateLimited(writer)
			return
		}
		if auth.recordFailure(client) {
			authenticationRateLimited(writer)
			return
		}
		http.Error(writer, "invalid or missing token", http.StatusUnauthorized)
		return
	}
	auth.recordSuccess(client)
	session, expires := auth.sessionCredentials()
	http.SetCookie(writer, &http.Cookie{
		Name:     sessionCookie,
		Value:    session,
		Path:     "/",
		HttpOnly: true,
		// Strict rather than Lax: no cross-site navigation should ever arrive
		// carrying this session, because every route behind it can start or
		// abandon a migration.
		SameSite: http.SameSiteStrictMode,
		// Not Secure: the server is loopback-only plaintext by design, and a
		// Secure cookie would simply never be sent.
		Expires: expires,
		MaxAge:  maxAgeUntil(auth.now(), expires),
	})
	http.Redirect(writer, request, "/", http.StatusFound)
}

// redeem consumes the launch token. It succeeds at most once: the second
// caller, whoever they are, finds nothing to redeem.
func (auth *authenticator) redeem(supplied string) (bool, error) {
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	if !constantTimeEqual(supplied, auth.launch) {
		return false, nil
	}
	now := auth.now()
	// The initial exchange starts the session lifetime. Later handoffs during
	// that lifetime must leave both the secret and its deadline alone: otherwise
	// a busy operator could slide a supposedly absolute cap forever. Once the
	// cap has elapsed, a new one-time launch gets a distinct secret so that an
	// expired cookie can never become valid again.
	if !now.Before(auth.sessionExpires) {
		session, err := newToken()
		if err != nil {
			return false, fmt.Errorf("generate renewed session token: %w", err)
		}
		auth.session = session
	}
	if !auth.sessionActive || !now.Before(auth.sessionExpires) {
		auth.sessionExpires = now.Add(sessionLifetime)
	}
	auth.sessionActive = true
	auth.launch = ""
	return true, nil
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
	if !auth.now().Before(auth.sessionExpires) {
		return false
	}
	return constantTimeEqual(supplied, auth.session)
}

func (auth *authenticator) sessionCredentials() (string, time.Time) {
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	return auth.session, auth.sessionExpires
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
		client := clientIdentity(request)
		cookie, cookieErr := request.Cookie(sessionCookie)
		header := request.Header.Get("Authorization")
		attempted := cookieErr == nil || header != ""
		if cookieErr == nil && auth.holdsSession(cookie.Value) {
			auth.recordSuccess(client)
			next.ServeHTTP(writer, request)
			return
		}
		if supplied, found := strings.CutPrefix(header, "Bearer "); found &&
			auth.holdsSession(supplied) {
			auth.recordSuccess(client)
			next.ServeHTTP(writer, request)
			return
		}
		if attempted && auth.rateLimited(client) {
			authenticationRateLimited(writer)
			return
		}
		if attempted && auth.recordFailure(client) {
			authenticationRateLimited(writer)
			return
		}
		http.Error(writer, "authentication required", http.StatusUnauthorized)
	})
}

func authenticationRateLimited(writer http.ResponseWriter) {
	writer.Header().Set("Retry-After", strconv.Itoa(int(authBlockedFor.Seconds())))
	http.Error(writer, "authentication temporarily rate limited", http.StatusTooManyRequests)
}

// clientIdentity uses only the socket peer. Forwarded address headers are
// intentionally ignored: this loopback server has no trusted proxy setting,
// and a caller controls those headers. Invalid test or transport addresses
// share one bounded bucket rather than becoming arbitrary map keys.
func clientIdentity(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return "unknown"
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "unknown"
	}
	return ip.String()
}

func (auth *authenticator) rateLimited(client string) bool {
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	now := auth.now()
	auth.pruneFailuresLocked(now)
	failure, found := auth.failures[client]
	return found && now.Before(failure.blockedUntil)
}

// recordFailure returns true when this attempt reaches the rate limit. The
// fifth failure is already rejected with 429, avoiding a free final guess.
func (auth *authenticator) recordFailure(client string) bool {
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	now := auth.now()
	auth.pruneFailuresLocked(now)
	failure, found := auth.failures[client]
	if !found || now.Sub(failure.windowStart) >= authFailureWindow {
		failure = authFailure{windowStart: now}
	}
	failure.count++
	failure.lastSeen = now
	if failure.count >= authFailureLimit {
		failure.blockedUntil = now.Add(authBlockedFor)
		failure.count = 0
		failure.windowStart = now
	}
	auth.storeFailureLocked(client, failure)
	return now.Before(failure.blockedUntil)
}

func (auth *authenticator) recordSuccess(client string) {
	auth.mutex.Lock()
	defer auth.mutex.Unlock()
	delete(auth.failures, client)
}

func (auth *authenticator) pruneFailuresLocked(now time.Time) {
	for client, failure := range auth.failures {
		if now.Sub(failure.lastSeen) >= authFailureWindow+authBlockedFor {
			delete(auth.failures, client)
		}
	}
}

func (auth *authenticator) storeFailureLocked(client string, failure authFailure) {
	if _, found := auth.failures[client]; !found && len(auth.failures) >= maxAuthClients {
		var oldestClient string
		var oldest time.Time
		for candidate, existing := range auth.failures {
			if oldestClient == "" || existing.lastSeen.Before(oldest) {
				oldestClient = candidate
				oldest = existing.lastSeen
			}
		}
		delete(auth.failures, oldestClient)
	}
	auth.failures[client] = failure
}

func maxAgeUntil(now, expiry time.Time) int {
	remaining := expiry.Sub(now)
	if remaining <= 0 {
		return -1
	}
	return int((remaining + time.Second - 1) / time.Second)
}
