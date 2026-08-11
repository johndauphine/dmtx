package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func consoleRequest(t *testing.T, server *Server, path string, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if authenticated {
		request.AddCookie(&http.Cookie{Name: sessionCookie, Value: server.auth.session})
	}
	recorder := httptest.NewRecorder()
	server.routes().ServeHTTP(recorder, request)
	return recorder
}

func TestConsoleServesAuthenticatedExternalAssets(t *testing.T) {
	server := newTestServer(t)
	for _, testCase := range []struct {
		path        string
		contentType string
		body        string
	}{
		{"/", "text/html", "/static/console.js"},
		{"/static/console.js", "text/javascript", "const apiRoutes"},
		{"/static/console.css", "text/css", ".console-shell"},
	} {
		t.Run(testCase.path, func(t *testing.T) {
			recorder := consoleRequest(t, server, testCase.path, true)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
			}
			if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, testCase.contentType) {
				t.Errorf("Content-Type = %q, want prefix %q", contentType, testCase.contentType)
			}
			if recorder.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Error("asset does not carry nosniff")
			}
			if recorder.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Error("asset does not carry no-referrer policy")
			}
			if recorder.Header().Get("Content-Security-Policy") != consoleCSP {
				t.Errorf("CSP = %q, want %q", recorder.Header().Get("Content-Security-Policy"), consoleCSP)
			}
			if !strings.Contains(recorder.Body.String(), testCase.body) {
				t.Errorf("asset body does not contain %q", testCase.body)
			}
		})
	}

	for _, path := range []string{"/", "/static/console.js", "/static/console.css"} {
		if recorder := consoleRequest(t, server, path, false); recorder.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated GET %s = %d, want 401", path, recorder.Code)
		}
	}
}

func TestConsoleAssetsKeepDataOutOfHTMLConstructionPaths(t *testing.T) {
	// This is a byte-level guard only. It does not prove browser interaction or
	// accessibility; real-browser acceptance covers those halves of the shell.
	for name, asset := range map[string][]byte{
		"HTML": consoleHTML,
		"JS":   consoleJS,
		"CSS":  consoleCSS,
	} {
		for _, forbidden := range []string{
			"inner" + "HTML", "outer" + "HTML", "insertAdjacent" + "HTML",
			"document.write", "eval(", "new Function",
		} {
			if strings.Contains(string(asset), forbidden) {
				t.Errorf("%s contains prohibited %q", name, forbidden)
			}
		}
		if strings.Contains(string(asset), "http://") || strings.Contains(string(asset), "https://") {
			t.Errorf("%s references an off-origin URL", name)
		}
	}
	if !strings.Contains(consoleCSP, "require-trusted-types-for 'script'") {
		t.Error("console CSP does not enforce Trusted Types for script sinks")
	}
}

func TestConsoleHasSemanticPaletteAndBrowserOnlyCommands(t *testing.T) {
	html := string(consoleHTML)
	js := string(consoleJS)
	for _, expected := range []string{
		`role="combobox"`, `aria-controls="suggestions"`, `aria-expanded="false"`,
		`id="suggestions" role="listbox"`, `id="console-status" role="status"`,
		`id="transcript"`, "/static/console.css", "/static/console.js",
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("HTML is missing %q", expected)
		}
	}
	for _, expected := range []string{
		"description", "category", "webui", "aria-activedescendant", "ArrowUp", "ArrowDown", "Escape",
		"case \"help\"", "case \"clear\"", "case \"quit\"", "window.close()", "close it manually", "localStorage", "historyKey",
		"apiRoutes", "/api/v1/commands", "/api/v1/parse", "/api/v1/complete", "/api/v1/jobs",
		"/api/v1/setup/start", "/api/v1/setup/input", "jobEventsURL", "jobCancelURL",
		"commandCompletionContext", "context.leading", "context.suffix", "activeSuggestionExactlyMatchesTypedCommand",
		"event.key === \"Enter\"", "event.key === \"Tab\"", "item.exact", "Number(right.exact) - Number(left.exact)",
		"Submitting command.", "Command answered.", "Command finished.", "Cancellation requested.",
	} {
		if !strings.Contains(js, expected) {
			t.Errorf("console JavaScript is missing %q", expected)
		}
	}
	if local := strings.Index(js, "if (localCommand(typed))"); local < 0 || local > strings.Index(js, "request(apiRoutes.parse") {
		t.Error("browser-only commands do not run before the parser seam")
	}
}

func TestConsoleRouteMarkersReachTheRegisteredMux(t *testing.T) {
	// This verifies that every route the shipped shell names resolves through
	// the real mux. It says nothing about browser fetch/event behavior, which
	// remains real-browser acceptance work.
	server := newTestServer(t)
	source := string(consoleHTML) + string(consoleJS)
	for _, testCase := range []struct {
		marker  string
		method  string
		path    string
		pattern string
	}{
		{"/static/console.js", http.MethodGet, "/static/console.js", "GET /static/"},
		{"/static/console.css", http.MethodGet, "/static/console.css", "GET /static/"},
		{"/api/v1/commands", http.MethodGet, "/api/v1/commands", "GET /api/v1/commands"},
		{"/api/v1/parse", http.MethodPost, "/api/v1/parse", "POST /api/v1/parse"},
		{"/api/v1/complete", http.MethodGet, "/api/v1/complete", "GET /api/v1/complete"},
		{"/api/v1/jobs", http.MethodGet, "/api/v1/jobs", "GET /api/v1/jobs"},
		{"/api/v1/jobs", http.MethodPost, "/api/v1/jobs", "POST /api/v1/jobs"},
		{"jobURL", http.MethodGet, "/api/v1/jobs/job-id", "GET /api/v1/jobs/{id}"},
		{"jobEventsURL", http.MethodGet, "/api/v1/jobs/job-id/events", "GET /api/v1/jobs/{id}/events"},
		{"jobCancelURL", http.MethodPost, "/api/v1/jobs/job-id/cancel", "POST /api/v1/jobs/{id}/cancel"},
		{"/api/v1/setup/start", http.MethodPost, "/api/v1/setup/start", "POST /api/v1/setup/start"},
		{"/api/v1/setup/input", http.MethodPost, "/api/v1/setup/input", "POST /api/v1/setup/input"},
	} {
		t.Run(testCase.method+" "+testCase.path, func(t *testing.T) {
			if !strings.Contains(source, testCase.marker) {
				t.Fatalf("shipped console does not name route marker %q", testCase.marker)
			}
			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			_, pattern := server.routeMux().Handler(request)
			if pattern != testCase.pattern {
				t.Errorf("mux pattern = %q, want %q", pattern, testCase.pattern)
			}
		})
	}
}

func TestConsoleCommandCompletionKeepsArgumentsAndFirstEnter(t *testing.T) {
	// Shipped-byte evidence for the browser logic. It guards the contract of
	// this branch without pretending Go has executed the keyboard interaction.
	js := string(consoleJS)
	for _, expected := range []string{
		"function commandCompletionContext()", "leading: match[1]", "suffix: line.value.slice(match[0].length)",
		"line.value = context.leading + \"/\" + item.spelling + (context.suffix || \" \");",
		"function activeSuggestionExactlyMatchesTypedCommand()", "item.spelling.toLowerCase() === commandCompletionContext().token",
		"const exact = items.findIndex(item => item.kind === \"command\" && item.exact);",
		"return candidates.sort((left, right) => Number(right.exact) - Number(left.exact));",
	} {
		if !strings.Contains(js, expected) {
			t.Errorf("console command-completion hook is missing %q", expected)
		}
	}
	for _, expected := range []string{
		"if (event.isComposing) return;", "if (event.key === \"Enter\")", "if (setupActive) {\n      form.requestSubmit();",
		"if (paletteOpen && activeSuggestion >= 0 && !activeSuggestionExactlyMatchesTypedCommand())",
		"if (chooseSuggestion()) return;", "if (paletteOpen) closePalette();", "if (event.key === \"Tab\" && paletteOpen",
	} {
		if !strings.Contains(js, expected) {
			t.Errorf("console Enter/Tab hook is missing %q", expected)
		}
	}
	enter := strings.Index(js, "if (event.key === \"Enter\")")
	if enter < 0 {
		t.Error("console has no Enter handling path")
	} else {
		enterPath := js[enter:]
		prevent := strings.Index(enterPath, "event.preventDefault();")
		closed := strings.Index(enterPath, "if (paletteOpen) closePalette();")
		if prevent < 0 || closed < 0 || prevent > closed {
			t.Error("Enter does not explicitly prevent native submission before the closed-palette requestSubmit path")
		} else if submit := strings.Index(enterPath[closed:], "form.requestSubmit();"); submit < 0 {
			t.Error("closed-palette Enter has no explicit requestSubmit path")
		}
	}
	if strings.Contains(js, "aliases.length ? \"aliases: \" + aliases.join(\", \") : \"\",\n        item.command.webui") {
		t.Error("palette metadata repeats the WebUI disposition already exposed by its dedicated badge")
	}
}

func TestConsoleHistoryAndMaskedSetupProtectionsAreShipped(t *testing.T) {
	js := string(consoleJS)
	for _, expected := range []string{
		"slice(0, 50)", "new Set", "historyDraft", "recallHistory", "isSetupInvocation",
		"legacyHistoryKey", "dmtx-console-history-v2", "localStorage.removeItem(legacyHistoryKey)",
		"if (!isSetupInvocation(typed)) remember(typed);", "line.value = \"\";",
		"if (setupActive) {", "line.type = setupMasked ? \"password\" : \"text\"",
		"apiRoutes.complete", "pathCompletionContext", "replaceStart", "replaceEnd", "[REDACTED]",
		"completionGeneration++", "scrollIntoView",
	} {
		if !strings.Contains(js, expected) {
			t.Errorf("console JavaScript is missing protection %q", expected)
		}
	}
	maskedGuard := strings.Index(js, "if (setupActive) {")
	completion := strings.Index(js, "apiRoutes.complete")
	if maskedGuard < 0 || completion < 0 || maskedGuard > completion {
		t.Fatal("masked setup input can reach completion")
	}
}
