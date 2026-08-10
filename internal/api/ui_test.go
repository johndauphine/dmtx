package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleServesAuthenticatedCommandSurface(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: server.auth.session})
	recorder := httptest.NewRecorder()

	server.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("console returned %d", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("console content type %q", contentType)
	}
	for _, expected := range []string{
		"DMTX Console", "/api/v1/parse", "/api/v1/jobs", "/api/v1/commands", "/api/v1/complete", "/cancel", "dmtx-console-history",
		"/api/v1/setup/start", "/api/v1/setup/input", "setupActive", "renderSetupPrompt", "setupMasked",
		"setup postgres", "[REDACTED]",
	} {
		if !strings.Contains(recorder.Body.String(), expected) {
			t.Errorf("console is missing %q", expected)
		}
	}
}

func TestConsoleContainsLifecycleRecoveryControls(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: server.auth.session})
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)

	body := recorder.Body.String()
	for _, expected := range []string{
		"historyIndex", "ArrowUp", "response.text()", "/api/v1/jobs",
		"recoverJobs", "result.state !== \"cancelling\"", "Cancelled.", "/cancel\", {}",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("console lifecycle behavior is missing %q", expected)
		}
	}
}

func TestConsoleMaskedSetupInputSkipsCompletionAndHistory(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: server.auth.session})
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	body := recorder.Body.String()

	maskedGuard := strings.Index(body, "if (setupActive && setupMasked) {")
	completionRequest := strings.Index(body, "/api/v1/complete?prefix=")
	if maskedGuard < 0 || completionRequest < 0 || maskedGuard > completionRequest {
		t.Fatal("masked setup input can reach path completion")
	}
	for _, expected := range []string{
		"let completionOptions = []", "function clearCompletionOptions()",
		"if (setupMasked) clearCompletionOptions()",
		"if (setupActive && setupMasked) return;",
		"if (!setupActive || !setupMasked) historyIndex = -1;",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("console is missing masked setup protection %q", expected)
		}
	}
}
