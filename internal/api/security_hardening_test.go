package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/johndauphine/dmtx/internal/app"
)

func TestAuthenticatedRequestBypassesAndClearsFailedPeerLimit(t *testing.T) {
	auth := &authenticator{session: "good", sessionIssued: time.Now(), host: "127.0.0.1:8484", failures: map[string]authFailure{}}
	called := false
	handler := auth.require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	for attempt := 0; attempt < failedAuthLimit; attempt++ {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8484/", nil)
		request.RemoteAddr = "127.0.0.1:9000"
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8484/", nil)
	request.RemoteAddr = "127.0.0.1:9000"
	request.Header.Set("Authorization", "Bearer good")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !called {
		t.Fatalf("valid credential was rate-limited: status=%d", response.Code)
	}
	if _, limited := auth.failures["127.0.0.1"]; limited {
		t.Fatal("valid credential did not clear peer failures")
	}
}

func TestExpiredSessionIsRefused(t *testing.T) {
	auth := &authenticator{session: "old", sessionIssued: time.Now().Add(-sessionLifetime - time.Second), failures: map[string]authFailure{}}
	handler := auth.require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("expired session invoked handler")
	}))
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8484/api/v1/commands", nil)
	request.Header.Set("Authorization", "Bearer old")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status=%d, want 401", response.Code)
	}
}

func TestLoopbackRequestRequiresExactHost(t *testing.T) {
	auth := &authenticator{session: "good", sessionIssued: time.Now(), host: "127.0.0.1:8484", failures: map[string]authFailure{}}
	called := false
	handler := auth.require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	request := httptest.NewRequest(http.MethodGet, "http://attacker.invalid/api/v1/commands", nil)
	request.RemoteAddr = "127.0.0.1:9000"
	request.Header.Set("Authorization", "Bearer good")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || called {
		t.Fatalf("rebound Host status=%d called=%t", response.Code, called)
	}
}

func TestStartedEventUsesOnlySafeRequestProjection(t *testing.T) {
	server := newTestServer(t)
	running, err := server.jobs.start(app.Request{Command: "run", ConfigPath: "/private/browser-secret-sentinel.yaml", AIAction: "config-review", AIRequest: "token=browser-secret-sentinel", AbandonReason: "browser-secret-sentinel"})
	if err != nil {
		t.Fatal(err)
	}
	events, _, _ := running.next(0)
	if len(events) == 0 {
		t.Fatal("no started event")
	}
	encoded := string(events[0].Data)
	if strings.Contains(encoded, "browser-secret-sentinel") || strings.Contains(encoded, "/private/") {
		t.Fatalf("started event leaked input: %s", encoded)
	}
	var frame struct {
		Request publicRequest `json:"request"`
	}
	if err := json.Unmarshal(events[0].Data, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Request.ConfigOrigin != "file" || !frame.Request.AIConfigReview {
		t.Fatalf("safe projection = %#v", frame.Request)
	}
}

func TestCookieUnsafeCrossPortOriginNeverInvokesHandler(t *testing.T) {
	auth := &authenticator{session: "good", sessionIssued: time.Now(), host: "127.0.0.1:8484", failures: map[string]authFailure{}}
	called := false
	handler := auth.require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8484/api/v1/jobs", nil)
	request.RemoteAddr = "127.0.0.1:9001"
	request.Header.Set("Origin", "http://127.0.0.1:9999")
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "good"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || called {
		t.Fatalf("cross-port cookie request status=%d called=%t", response.Code, called)
	}
}

func TestCookieUnsafeRequestRequiresAnExactOrigin(t *testing.T) {
	auth := &authenticator{session: "good", sessionIssued: time.Now(), host: "127.0.0.1:8484", failures: map[string]authFailure{}}
	handler := auth.require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("origin-less cookie request invoked handler")
	}))
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8484/api/v1/jobs", nil)
	request.RemoteAddr = "127.0.0.1:9001"
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "good"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("origin-less cookie request status=%d, want 403", response.Code)
	}
}

func TestDirectJobAdmissionDoesNotReflectUnknownOrFreeText(t *testing.T) {
	server := newTestServer(t)
	for _, body := range []string{
		`{"command":"unknown-browser-secret-sentinel"}`,
		`{"command":"ai","ai_action":"config-review","ai_request":"token=browser-secret-sentinel"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+server.auth.session)
		response := httptest.NewRecorder()
		server.routes().ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "browser-secret-sentinel") {
			t.Fatalf("direct admission leaked input: status=%d body=%q", response.Code, response.Body.String())
		}
	}
}
