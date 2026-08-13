package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type authTestClock struct{ value time.Time }

func (clock *authTestClock) now() time.Time { return clock.value }

func (clock *authTestClock) advance(duration time.Duration) {
	clock.value = clock.value.Add(duration)
}

func TestAuthenticationFailuresAreRateLimitedPerClient(t *testing.T) {
	server := newTestServer(t)
	clock := &authTestClock{value: time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)}
	server.auth.now = clock.now
	server.auth.sessionExpires = clock.now().Add(sessionLifetime)
	handler := server.routes()

	requestFrom := func(client string) *http.Request {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/commands", nil)
		request.RemoteAddr = client + ":4321"
		request.Header.Set("Authorization", "Bearer not-the-session")
		// Forwarded headers must not create a new rate-limit identity.
		request.Header.Set("X-Forwarded-For", "198.51.100.99")
		return request
	}
	for attempt := 1; attempt < authFailureLimit; attempt++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, requestFrom("127.0.0.2"))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d returned %d, want 401", attempt, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, requestFrom("127.0.0.2"))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("limit attempt returned %d, want 429", recorder.Code)
	}
	if got := recorder.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}

	otherClient := httptest.NewRecorder()
	handler.ServeHTTP(otherClient, requestFrom("127.0.0.3"))
	if otherClient.Code != http.StatusUnauthorized {
		t.Fatalf("other client returned %d, want independent 401", otherClient.Code)
	}

	clock.advance(authBlockedFor)
	afterBlock := httptest.NewRecorder()
	handler.ServeHTTP(afterBlock, requestFrom("127.0.0.2"))
	if afterBlock.Code != http.StatusUnauthorized {
		t.Fatalf("request after block returned %d, want 401", afterBlock.Code)
	}
}

func TestAuthenticationFailureTrackingIsBounded(t *testing.T) {
	server := newTestServer(t)
	clock := &authTestClock{value: time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)}
	server.auth.now = clock.now
	handler := server.routes()

	for client := 0; client < maxAuthClients+40; client++ {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/commands", nil)
		request.RemoteAddr = fmt.Sprintf("10.0.%d.%d:1234", client/250, client%250+1)
		request.Header.Set("Authorization", "Bearer not-the-session")
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
	if got := len(server.auth.failures); got != maxAuthClients {
		t.Fatalf("tracked client failures = %d, want bounded %d", got, maxAuthClients)
	}
}

func TestSessionHasAnAbsoluteLifetimeCap(t *testing.T) {
	clock := &authTestClock{value: time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)}
	auth := newAuthenticator("launch", "session", clock.now)
	redeemed, err := auth.redeem("launch")
	if err != nil || !redeemed {
		t.Fatalf("redeem initial launch = %v, %v", redeemed, err)
	}
	handler := auth.require(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))

	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"})
		return r
	}
	beforeExpiry := httptest.NewRecorder()
	handler.ServeHTTP(beforeExpiry, request())
	if beforeExpiry.Code != http.StatusNoContent {
		t.Fatalf("session before cap returned %d, want 204", beforeExpiry.Code)
	}

	clock.advance(sessionLifetime)
	afterExpiry := httptest.NewRecorder()
	handler.ServeHTTP(afterExpiry, request())
	if afterExpiry.Code != http.StatusUnauthorized {
		t.Fatalf("session at absolute cap returned %d, want 401", afterExpiry.Code)
	}
}

func TestExpiredSessionCannotBeResurrectedByANewLaunch(t *testing.T) {
	clock := &authTestClock{value: time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)}
	auth := newAuthenticator("initial-launch", "old-session", clock.now)
	initial, err := auth.redeem("initial-launch")
	if err != nil || !initial {
		t.Fatalf("redeem initial launch = %v, %v", initial, err)
	}
	oldSession := auth.session

	clock.advance(sessionLifetime)
	launch, err := auth.remint()
	if err != nil {
		t.Fatalf("remint: %v", err)
	}
	redeemed, err := auth.redeem(launch)
	if err != nil || !redeemed {
		t.Fatalf("redeem renewed launch = %v, %v", redeemed, err)
	}
	newSession, _ := auth.sessionCredentials()
	if newSession == oldSession {
		t.Fatal("renewed launch reused an expired session secret")
	}
	if auth.holdsSession(oldSession) {
		t.Fatal("expired session secret became valid after a renewed launch")
	}
	if !auth.holdsSession(newSession) {
		t.Fatal("new session secret is not valid after renewed launch")
	}
}

func TestHandoffBeforeExpiryCannotSlideSessionDeadline(t *testing.T) {
	clock := &authTestClock{value: time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)}
	auth := newAuthenticator("initial-launch", "session", clock.now)
	initial, err := auth.redeem("initial-launch")
	if err != nil || !initial {
		t.Fatalf("redeem initial launch = %v, %v", initial, err)
	}
	oldSession, oldExpiry := auth.sessionCredentials()

	clock.advance(sessionLifetime - time.Minute)
	launch, err := auth.remint()
	if err != nil {
		t.Fatalf("remint: %v", err)
	}
	redeemed, err := auth.redeem(launch)
	if err != nil || !redeemed {
		t.Fatalf("redeem handoff launch = %v, %v", redeemed, err)
	}
	newSession, newExpiry := auth.sessionCredentials()
	if newSession != oldSession {
		t.Fatal("handoff before expiry changed the session secret")
	}
	if !newExpiry.Equal(oldExpiry) {
		t.Fatalf("handoff expiry = %s, want unchanged %s", newExpiry, oldExpiry)
	}

	clock.advance(time.Minute)
	if auth.holdsSession(oldSession) {
		t.Fatal("near-expiry handoff extended the original session lifetime")
	}
}

func TestLoginCookieCarriesTheAbsoluteSessionExpiry(t *testing.T) {
	server := newTestServer(t)
	clock := &authTestClock{value: time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)}
	server.auth.now = clock.now

	request := httptest.NewRequest(http.MethodGet, "/login?token="+server.auth.launch, nil)
	request.RemoteAddr = "127.0.0.1:1234"
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusFound {
		t.Fatalf("login returned %d, want 302", recorder.Code)
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name != sessionCookie {
			continue
		}
		want := clock.now().Add(sessionLifetime)
		if !cookie.Expires.Equal(want) {
			t.Fatalf("cookie expiry = %s, want absolute cap %s", cookie.Expires, want)
		}
		if cookie.MaxAge != int(sessionLifetime/time.Second) {
			t.Fatalf("cookie MaxAge = %d, want %d", cookie.MaxAge, int(sessionLifetime/time.Second))
		}
		return
	}
	t.Fatal("login did not set a session cookie")
}

func TestLoopbackHostGuardRejectsDNSRebindingHosts(t *testing.T) {
	server := newTestServer(t)
	guarded := server.loopbackHostGuard(server.routes())
	port := server.listener.Addr().(*net.TCPAddr).Port

	request := func(host string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/commands", nil)
		r.Host = host
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: server.auth.session})
		return r
	}
	for _, host := range []string{
		fmt.Sprintf("127.0.0.1:%d", port),
		fmt.Sprintf("localhost:%d", port),
		fmt.Sprintf("[::1]:%d", port),
		"localhost:4242",
		"127.0.0.1:4242",
		"[::1]:4242",
	} {
		recorder := httptest.NewRecorder()
		guarded.ServeHTTP(recorder, request(host))
		if recorder.Code != http.StatusOK {
			t.Errorf("allowed loopback Host %q returned %d, want 200", host, recorder.Code)
		}
	}
	for _, host := range []string{
		fmt.Sprintf("evil.example:%d", port),
		"127.0.0.1",
		"localhost",
		"localhost:not-a-port",
		"localhost:0",
		"localhost:65536",
		"localhost:+1",
		"192.0.2.1:4242",
	} {
		recorder := httptest.NewRecorder()
		guarded.ServeHTTP(recorder, request(host))
		if recorder.Code != http.StatusMisdirectedRequest {
			t.Errorf("unsafe Host %q returned %d, want 421", host, recorder.Code)
		}
	}
}

func TestLoopbackHostGuardAllowsSSHForwardedLocalPort(t *testing.T) {
	server := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})

	request, err := http.NewRequest(http.MethodGet, "http://"+server.Addr()+"/api/v1/commands", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	// ssh -L 4242:localhost:<remote-port> sends this forwarded local Host
	// header, which deliberately differs from the server's bound port.
	request.Host = "localhost:4242"
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("forwarded request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forwarded local Host returned %d, want 401 after passing host guard", response.StatusCode)
	}

	request, err = http.NewRequest(http.MethodGet, "http://"+server.Addr()+"/api/v1/commands", nil)
	if err != nil {
		t.Fatalf("new rebinding request: %v", err)
	}
	request.Host = "evil.example:4242"
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("rebinding request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("DNS host returned %d, want 421", response.StatusCode)
	}
}
