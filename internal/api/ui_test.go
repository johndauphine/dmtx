package api

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
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
		{"/static/icon.svg", "image/svg+xml", "<svg"},
		{"/manifest.webmanifest", "application/manifest+json", "DMTX Console"},
		{"/sw.js", "text/javascript", "network outage"},
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

	for _, path := range []string{"/", "/static/console.js", "/static/console.css", "/static/icon.svg", "/manifest.webmanifest", "/sw.js"} {
		if recorder := consoleRequest(t, server, path, false); recorder.Code != http.StatusUnauthorized {
			t.Errorf("unauthenticated GET %s = %d, want 401", path, recorder.Code)
		}
	}
}

func TestConsolePWAAndRecallStayBoundedAndSafe(t *testing.T) {
	script := string(consoleJS)
	for _, required := range []string{
		"trustedTypes.createPolicy(\"dmtx-service-worker\"", "createScriptURL", "serviceWorker.register(workerURL)",
		"maxTranscriptEntries", "textContent",
		"isRecallSafe", "--request", "--abandon-reason", "init-secrets",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("console script missing %q", required)
		}
	}
	for _, forbidden := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "eval("} {
		if strings.Contains(script, forbidden) {
			t.Errorf("console script contains unsafe renderer %q", forbidden)
		}
	}
	worker := string(consoleWorker)
	for _, required := range []string{"fetch(event.request)", "response.ok", "cache.put", "caches.match", "url.pathname.startsWith(\"/api/v1/\")"} {
		if !strings.Contains(worker, required) {
			t.Errorf("service worker missing %q", required)
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
		"case \"help\"", "case \"clear\"", "case \"quit\"", "case \"exit\"", "case \"logs\"", "case \"session\"", "case \"about\"", "function localHelp()", "line: \"help\"", "window.close()", "close it manually", "localStorage", "historyKey",
		"apiRoutes", "/api/v1/commands", "/api/v1/parse", "/api/v1/complete", "/api/v1/jobs",
		"/api/v1/session", "/api/v1/setup/start", "/api/v1/setup/input", "jobEventsURL", "jobCancelURL",
		"commandCompletionContext", "context.leading", "context.suffix", "activeSuggestionExactlyMatchesTypedCommand",
		"event.key === \"Enter\"", "event.key === \"Tab\"", "item.exact", "Number(right.exact) - Number(left.exact)",
		"Submitting command.", "Command answered.", "Command finished.", "Cancellation requested.",
	} {
		if !strings.Contains(js, expected) {
			t.Errorf("console JavaScript is missing %q", expected)
		}
	}
	submit := strings.Index(js, "form.addEventListener(\"submit\"")
	if submit < 0 {
		t.Fatal("console has no submit handler")
	}
	submitPath := js[submit:]
	if local := strings.Index(submitPath, "if (await localCommand(typed))"); local < 0 || local > strings.Index(submitPath, "request(apiRoutes.parse") {
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
		{"/api/v1/session", http.MethodGet, "/api/v1/session", "GET /api/v1/session"},
		{"/api/v1/session", http.MethodPost, "/api/v1/session", "POST /api/v1/session"},
		{"/api/v1/session", http.MethodDelete, "/api/v1/session/config", "DELETE /api/v1/session/{key}"},
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
		"completionGeneration++", "scrollIntoView", "function consoleWords(value)", "function setupInvocation(typed)", "profile_name", "--config=", "--profile=",
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

// TestConsoleRendersStructuredPayloads pins the renderer boundary for every
// structured payload a WebUI-supported command can return. Go does not execute
// browser JavaScript; this instead guards the shipped code's explicit
// public-field mapping and bounded output.
func TestConsoleRendersStructuredPayloads(t *testing.T) {
	js := string(consoleJS)
	for _, expected := range []string{
		"const maxRenderedRuns = 50", "const maxRenderedFieldLength = 512",
		"function boundedPayloadText(value)", "function payloadRecord(value)",
		"function payloadList(record, name, limit = 12)", "function renderedRun(run)",
		"const maxProgressTableNames = 12", "function renderProgress(data)",
		"case \"tables_planned\"", "case \"table_started\"", "case \"table_finished\"",
		"events.addEventListener(\"progress\", event => renderProgress(event.data))",
		"function renderPayload(payload)", "payload.kind === \"run\"", "payload.kind === \"runs\"",
		"function latestRunRecords(records)", "records.slice(-maxRenderedRuns)",
		"status_detail: renderedStatusDetail", "function renderedStatusDetail(data)", "record.tasks.slice(0, 12)",
		"plan: renderedPlan", "result: data => renderedResult(data, false)",
		"partial_result: data => renderedResult(data, true)", "validation_result: renderedValidation", "preflight_report: renderedPreflight",
		"resume_response: renderedResumeResponse", "config: renderedConfig",
		"diagnosis: renderedDiagnosis", "analysis: renderedAnalysis", "ai_advisory: renderedAIAdvisory",
		"No runs recorded.",
		"payloadNumber(record, \"rows\")", "record.tables.slice(0, 12)",
		"record.findings.slice(0, 12)", "payloadList(record, \"skip_selectors\")",
		"payloadText(item, \"message\")", "payloadText(item, \"remedy\")", "payloadText(item, \"evidence\")",
		"payloadText(endpoint, \"user\")", "payloadText(endpoint, \"ssl_mode\")", "payloadText(endpoint, \"tls_ca_file\")",
		"const notes = Array.isArray(record.notes) ? record.notes.slice(0, 12) : []",
		"function renderedAIAdvisory(data)", "advisory.findings.slice(0, 12)", "advisory.warnings.slice(0, 12)",
		"appendTranscript(text)", "const renderedPayload = renderPayload(outcome.payload)",
	} {
		if !strings.Contains(js, expected) {
			t.Errorf("console JavaScript is missing structured run rendering hook %q", expected)
		}
	}
	for _, forbidden := range []string{
		"JSON.stringify(payload.data)", "Object.entries(run)",
		"events.addEventListener(\"progress\", event => appendTranscript(event.data))",
	} {
		if strings.Contains(js, forbidden) {
			t.Errorf("console payload renderer must not render unbounded or sensitive payload data via %q", forbidden)
		}
	}
}

// TestConsolePayloadRendererBehavior runs the browser-side renderer in a tiny
// DOM harness. The source-string test above guards its security boundary; this
// one proves the renderer actually accepts every supported payload shape and
// applies its row, item, and field caps.
func TestConsolePayloadRendererBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the browser renderer harness")
	}
	const harness = `
const fs = require("fs");
const vm = require("vm");
let source = fs.readFileSync(process.argv[1], "utf8");
source = source.slice(0, source.lastIndexOf("Promise.all([loadCommands(), recoverJobs()])"));
const entries = [];
const element = () => ({
  hidden: false, value: "", type: "text", disabled: false,
  className: "", textContent: "", children: [],
  addEventListener() {}, setAttribute() {}, removeAttribute() {},
  replaceChildren() { this.children = []; }, append(...items) { this.children.push(...items); entries.push(...items); },
  scrollIntoView() {}
});
const transcript = element();
const context = {
  console, URLSearchParams, JSON, String, Number, Array, Object, Map, Set,
  document: {
    querySelector(selector) { return selector === "#transcript" ? transcript : element(); },
    createElement() { return element(); }
  },
  localStorage: { removeItem() {}, getItem() { return null; }, setItem() {} },
  window: { close() {}, setTimeout() {} }
};
vm.createContext(context);
vm.runInContext(source, context);
function assert(condition, message) { if (!condition) throw new Error(message); }
const sessionWords = context.consoleWords('/session config "my config.yaml"');
assert(sessionWords.words.length === 3 && sessionWords.words[2] === "my config.yaml", "session quote grammar missing");
assert(context.consoleWords('/session config "unterminated').error === "unterminated quote", "session quote refusal missing");
const setupConfig = context.setupInvocation('/setup --config=migration.yaml');
assert(setupConfig && setupConfig.engine === "sqlite" && setupConfig.config_path === "migration.yaml", "setup config equals form missing");
const setupProfile = context.setupInvocation('/setup --profile saved');
assert(setupProfile && setupProfile.profile_name === "saved" && setupProfile.config_path === "", "profile-backed setup missing");
assert(context.setupInvocation('/wizard @migration.yaml') === null, "wizard must reach the server refusal rather than impersonate setup");
function render(payload) {
  entries.length = 0;
  assert(context.renderPayload(payload), "payload was not rendered: " + payload.kind);
  return entries.map(entry => entry.textContent).join("\n");
}
function progress(event) {
  entries.length = 0;
  context.renderProgress(event);
  return entries.map(entry => entry.textContent).join("\n");
}
const run = { id: "run-1", outcome: "success", resumable: false, source: "source.db", target: "target.db" };
assert(render({kind:"run", data:run}).includes("run-1"), "run summary missing");
const detailedTasks = Array.from({length:13}, (_, i) => ({table:"detail-" + i, status:"completed", rows_done:i}));
const detail = render({kind:"status_detail", data:{run, tasks:detailedTasks}});
assert(detail.includes("Detailed status") && detail.includes("detail-0") && !detail.includes("detail-12") && detail.includes("Tasks: additional entries omitted"), "detailed status or cap missing");
assert(render({kind:"runs", data:[]}).includes("No runs recorded."), "empty history missing");
const cappedHistory = render({kind:"runs", data:Array.from({length:51}, (_, i) => ({id:"run-" + i}))});
assert(cappedHistory.split("\n").filter(value => value.startsWith("Run run-")).length === 50 && !cappedHistory.includes("Run run-0\n") && cappedHistory.includes("Run run-50"), "recent run cap missing");
const transitionHistory = render({kind:"runs", data:[{id:"run-a",outcome:"running",ended_at:"0001-01-01T00:00:00Z"},{id:"run-a",outcome:"success",ended_at:"2026-08-11T12:00:00Z"},{id:"run-b",outcome:"running"},{id:"run-b",outcome:"failed"}]});
assert(transitionHistory.includes("History: 2 run(s).") && transitionHistory.includes("Outcome: success") && transitionHistory.includes("Outcome: failed") && !transitionHistory.includes("Outcome: running") && !transitionHistory.includes("0001-01-01"), "history did not select terminal transitions");
assert(render({kind:"result", data:{tables:2, rows:30, validated:true}}).includes("Rows: 30"), "aggregate result rows missing");
assert(render({kind:"partial_result", data:{tables:2, rows:30, validated:false, outcome:"partial", resumable:true}}).includes("Partial migration result"), "partial result missing");
const validationTables = Array.from({length:13}, (_, i) => ({table:"validation-" + i, source_rows:i, target_rows:i, match:true}));
const validation = render({kind:"validation_result", data:{passed:false, tables:validationTables}});
assert(validation.includes("Passed: no") && validation.includes("validation-0") && !validation.includes("validation-12") && validation.includes("additional tables omitted"), "validation detail or cap missing");
const planTables = Array.from({length:13}, (_, i) => ({name:"table-" + i, rows:i, rows_provenance:"exact"}));
const plan = render({kind:"plan", data:{proceed:false, source_type:"sqlite", target_type:"postgres", target_mode:"upsert", tables:planTables, admission:{supported:false,error:"refused"}, target:{presence:"unknown",preflight:"skipped",error:"unavailable",limitations:["no target"]}, schema_drift:{status:"unavailable",blocks_proceed:true,error:"no baseline"}}});
assert(plan.includes("table-0") && !plan.includes("table-12") && plan.includes("Schema status: unavailable"), "plan detail or cap missing");
const findings = Array.from({length:13}, (_, i) => ({severity:"error", side:"target", check:"check-" + i, class:"failed", message:"message-" + i, remedy:"remedy-" + i, evidence:"evidence-" + i}));
const preflight = render({kind:"preflight_report", data:{proceed:false, findings, skip_selectors:["skip-a"]}});
assert(preflight.includes("message-0") && preflight.includes("remedy-0") && preflight.includes("evidence-0") && !preflight.includes("message-12") && preflight.includes("Skip selector: skip-a"), "preflight detail or cap missing");
assert(render({kind:"resume_response", data:{run_id:"run-1", outcome:"abandoned", resumable:false}}).includes("Resume response"), "resume response missing");
const config = render({kind:"config", data:{path:"migration.yaml", source:{type:"postgres",host:"db",port:5432,database:"shop",schema:"public",user:"reader",ssl_mode:"require",tls_ca_file:"ca.pem"}, target:{type:"sqlite",database:"out.db"}, migration:{target_mode:"upsert",workers:4,connection_limit:8}, notes:[{severity:"warning",message:"review me"}]}});
assert(config.includes("User: reader") && config.includes("SSL mode: require") && config.includes("TLS CA file: ca.pem") && config.includes("Diagnostic: warning: review me"), "config public details missing");
assert(render({kind:"diagnosis", data:{run, tables:{total:2,completed:1,in_progress:1,not_started:0}, incomplete:["orders"], findings:["interrupted"], next_step:"resume"}}).includes("Diagnosis"), "diagnosis missing");
assert(render({kind:"analysis", data:{path:"migration.yaml", tuning:{workers:{value:4,provenance:"requested"}, memory_budget_bytes:{value:100,provenance:"derived"}}}}).includes("Workers: 4"), "analysis missing");
const advisoryFindings = Array.from({length:13}, (_, i) => ({category:"safety", title:"finding-" + i, summary:"summary-" + i, action:"action-" + i}));
const advisoryWarnings = Array.from({length:13}, (_, i) => "warning-" + i);
const advisory = render({kind:"ai_advisory", data:{status:"ok", provider:"openai", model:"safe-model", advisory:{summary:"review summary", findings:advisoryFindings, warnings:advisoryWarnings}}});
assert(advisory.includes("review summary") && advisory.includes("finding-0") && !advisory.includes("finding-12") && advisory.includes("Findings: additional entries omitted") && advisory.includes("Warnings: additional entries omitted"), "AI advisory detail or cap missing");
assert(render({kind:"ai_advisory", data:{status:"unavailable", error:"not configured"}}).includes("not configured"), "AI advisory unavailable state missing");
const progressTables = Array.from({length:13}, (_, i) => "progress-" + i);
const planned = progress(JSON.stringify({kind:"tables_planned", tables:progressTables, done:0, total:13}));
assert(planned.includes("progress-0") && !planned.includes("progress-12") && planned.includes("Additional table names omitted."), "planned progress detail or cap missing");
assert(progress(JSON.stringify({kind:"table_started", table:"orders", done:1, total:3})).includes("Starting table orders (1 of 3 completed)."), "started progress missing");
assert(progress(JSON.stringify({kind:"table_finished", table:"orders", rows:7, done:2, total:3})).includes("Finished table orders (2 of 3; 7 rows)."), "finished progress missing");
assert(progress(JSON.stringify({kind:"unknown", secret:"do-not-dump", done:0, total:0})) === "Progress update unavailable (unknown event).", "unknown progress was dumped");
assert(progress("not-json-do-not-dump") === "Progress update unavailable.", "malformed progress was dumped");
const long = render({kind:"run", data:{id:"x".repeat(600)}});
assert(!long.includes("x".repeat(513)), "field cap missing");
`
	command := exec.Command(node, "-e", harness, "static/console.js")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("console renderer harness failed: %v\n%s", err, output)
	}
}
