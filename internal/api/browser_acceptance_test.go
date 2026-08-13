//go:build browser

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"github.com/johndauphine/dmtx/internal/app"
	"github.com/johndauphine/dmtx/internal/contract"
	"github.com/johndauphine/dmtx/internal/profiles"
	"github.com/johndauphine/dmtx/internal/secrets"
)

type browserParseReply struct {
	Line       string      `json:"line"`
	Dispatched bool        `json:"dispatched"`
	Request    app.Request `json:"request"`
	Outcome    app.Outcome `json:"outcome"`
	Status     int         `json:"status"`
}

// TestBrowserConsoleControls is deliberately opt-in: it drives a real local
// Chromium-family browser, never an external migration database or Docker.
func TestBrowserConsoleControls(t *testing.T) {
	binary := browserBinary(t)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	seedBrowserProfile(t, "browser-seeded")
	if err := os.WriteFile(filepath.Join(root, "complete-me.yaml"), []byte("source: sqlite\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.listener.Close() })
	command := newBlockingCommand()
	server.jobs.execute = func(ctx context.Context, request app.Request, report app.ProgressFunc) app.Outcome {
		if request.Command == "profile" {
			return app.ExecuteWithProgress(ctx, request, report)
		}
		report(app.Progress{Kind: app.ProgressTablesPlanned, Tables: []string{"safe_fixture"}, Total: 1})
		return command.run(ctx, request, report)
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	t.Cleanup(func() {
		stop()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})

	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(binary), chromedp.Flag("headless", true), chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-first-run", true), chromedp.Flag("no-default-browser-check", true),
	)...)
	defer cancelAllocator()
	browser, cancelBrowser := chromedp.NewContext(allocator)
	defer cancelBrowser()
	browser, timeout := context.WithTimeout(browser, 30*time.Second)
	defer timeout()

	// Navigating to the one-time URL must exchange its token, redirect to the
	// clean root URL, and leave an authenticated session for all subsequent UI
	// calls.
	if err := chromedp.Run(browser,
		chromedp.Navigate(server.URL()),
		chromedp.WaitVisible("#line", chromedp.ByID),
		chromedp.WaitVisible("#command-summary", chromedp.ByID),
	); err != nil {
		t.Fatalf("open authenticated console with %s: %v", binary, err)
	}
	var location, summary string
	if err := chromedp.Run(browser, chromedp.Location(&location), chromedp.Text("#command-summary", &summary, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(location, "token=") {
		t.Fatalf("launch token remains in browser address: %s", location)
	}
	if !strings.Contains(summary, "registered") {
		t.Fatalf("registry discovery summary = %q", summary)
	}
	if err := chromedp.Run(browser, chromedp.Click("#line", chromedp.ByID), chromedp.WaitVisible("#suggestions", chromedp.ByID)); err != nil {
		t.Fatalf("open complete command palette: %v", err)
	}
	var missingSupported []string
	if err := chromedp.Run(browser, chromedp.Evaluate(`fetch("/api/v1/commands").then(r=>r.json()).then(cs => { const palette=document.querySelector("#suggestions").innerText.toLowerCase(); return cs.filter(c=>c.webui==="supported").flatMap(c=>[c.name,...(c.aliases||[])]).filter(name=>!palette.includes("/"+name.toLowerCase())); })`, &missingSupported, func(params *runtime.EvaluateParams) *runtime.EvaluateParams { return params.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("compare canonical registry to palette: %v", err)
	}
	if len(missingSupported) != 0 {
		t.Fatalf("supported registry commands absent from browser palette: %v", missingSupported)
	}
	if err := chromedp.Run(browser, chromedp.KeyEvent(kb.Escape)); err != nil {
		t.Fatal(err)
	}
	assertBrowserParseParity(t, browser)
	// /help is a browser-local command, so exercise that rendering path rather
	// than sending it through /parse as though it were a server command.
	if err := chromedp.Run(browser,
		chromedp.Evaluate(`(() => { const line=document.querySelector("#line"); line.value="/help"; line.dispatchEvent(new Event("input", {bubbles:true})); })()`, nil),
		chromedp.Click("button[type=submit]", chromedp.ByQuery),
		chromedp.Poll(`document.querySelector("#console-status").innerText.includes("discovery is open")`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.KeyEvent(kb.Escape),
	); err != nil {
		t.Fatalf("browser-local help: %v", err)
	}

	// Slash discovery/help and keyboard selection exercise the same registry
	// that command execution uses.
	if err := chromedp.Run(browser,
		chromedp.SendKeys("#line", "/stat", chromedp.ByID),
		chromedp.WaitVisible("#suggestions", chromedp.ByID),
		chromedp.KeyEvent(kb.ArrowDown), chromedp.KeyEvent(kb.Tab),
	); err != nil {
		t.Fatalf("palette keyboard completion: %v", err)
	}
	var line string
	if err := chromedp.Run(browser, chromedp.Value("#line", &line, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "/") || strings.TrimSpace(line) == "/stat" {
		t.Fatalf("keyboard completion = %q, want a registry command", line)
	}
	// @ completion stays root-confined and is selected through the real palette.
	if err := chromedp.Run(browser,
		chromedp.Evaluate(`(() => { const line=document.querySelector("#line"); line.value="@comp"; line.setSelectionRange(line.value.length, line.value.length); line.dispatchEvent(new Event("input", {bubbles:true})); })()`, nil),
		chromedp.WaitVisible("#suggestions", chromedp.ByID), chromedp.KeyEvent(kb.Tab),
	); err != nil {
		t.Fatalf("root-confined @ completion: %v", err)
	}
	if err := chromedp.Run(browser, chromedp.Value("#line", &line, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "complete-me.yaml") || !strings.Contains(line, root) {
		t.Fatalf("@ completion = %q", line)
	}
	if err := chromedp.Run(browser, chromedp.Evaluate(`(() => { const line=document.querySelector("#line"); line.value="/status"; line.setSelectionRange(line.value.length,line.value.length); line.dispatchEvent(new Event("input", {bubbles:true})); })()`, nil), chromedp.KeyEvent(kb.Escape)); err != nil {
		t.Fatal(err)
	}

	// The controlled executor publishes public progress, then waits. Reloading
	// while it is live proves recovery is server/job based rather than tab based;
	// explicit cancel supplies the terminal outcome without touching an adapter.
	if err := chromedp.Run(browser,
		chromedp.Click("button[type=submit]", chromedp.ByQuery),
		chromedp.WaitVisible("#cancel:not([disabled])", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("parse/submit controlled job: %v", err)
	}
	select {
	case <-command.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("controlled job did not start")
	}
	if err := chromedp.Run(browser, chromedp.Reload(), chromedp.WaitVisible("#cancel:not([disabled])", chromedp.ByQuery)); err != nil {
		t.Fatalf("reload/recovery: %v", err)
	}
	if err := chromedp.Run(browser, chromedp.Click("#cancel", chromedp.ByID), chromedp.WaitVisible("#console-status", chromedp.ByID)); err != nil {
		t.Fatalf("explicit cancel: %v", err)
	}
	if err := chromedp.Run(browser, chromedp.Poll(`document.querySelector("#transcript").innerText.includes("Cancelled.")`, nil, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("cancel outcome: %v", err)
	}
	// The profile job is the sole real application execution in this fixture.
	// It lists a pre-seeded encrypted SQLite profile under the temporary HOME;
	// no profile save/delete/export/import and no user profile data is touched.
	if err := chromedp.Run(browser,
		chromedp.Evaluate(`(() => { const line=document.querySelector("#line"); line.value="/profile list"; line.setSelectionRange(line.value.length,line.value.length); line.dispatchEvent(new Event("input", {bubbles:true})); })()`, nil),
		chromedp.Click("button[type=submit]", chromedp.ByQuery),
		chromedp.Poll(`document.querySelector("#transcript").innerText.includes("browser-seeded")`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("browser profile list: %v", err)
	}

	// Browser-local recall is bounded and setup invocations are never retained.
	if err := chromedp.Run(browser,
		chromedp.Evaluate(`localStorage.setItem("dmtx-console-history-v2", JSON.stringify(["/status", "/setup secret"]))`, nil),
		chromedp.Reload(), chromedp.WaitVisible("#command-summary", chromedp.ByID),
		chromedp.Click("#line", chromedp.ByID), chromedp.KeyEvent(kb.Escape), chromedp.KeyEvent(kb.ArrowUp)); err != nil {
		t.Fatalf("history recall: %v", err)
	}
	if err := chromedp.Run(browser, chromedp.Value("#line", &line, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if line != "/status" {
		t.Fatalf("history recalled %q; setup input must be excluded", line)
	}
	if err := chromedp.Run(browser, chromedp.Evaluate(`localStorage.setItem("dmtx-console-history-v2", JSON.stringify(["/status"]))`, nil)); err != nil {
		t.Fatal(err)
	}
	// SQL Server setup reaches the masked credential prompt without trying a
	// connection. Stop at the optional CA prompt: verification cannot start
	// until that answer is submitted.
	if err := chromedp.Run(browser,
		chromedp.Evaluate(`(() => { const line=document.querySelector("#line"); line.value="/setup sqlserver"; line.setSelectionRange(line.value.length,line.value.length); line.dispatchEvent(new Event("input", {bubbles:true})); })()`, nil),
		chromedp.Click("button[type=submit]", chromedp.ByQuery),
		chromedp.Poll(`document.querySelector("#transcript").innerText.includes("Source SQL Server host") && document.querySelector("#transcript").innerText.includes("default: localhost")`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.SendKeys("#line", "sqlserver-source", chromedp.ByID),
		chromedp.Click("button[type=submit]", chromedp.ByQuery),
		chromedp.Poll(`document.querySelector("#transcript").innerText.includes("Source SQL Server port")`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.SendKeys("#line", "1433", chromedp.ByID),
		chromedp.Click("button[type=submit]", chromedp.ByQuery),
		chromedp.Poll(`document.querySelector("#transcript").innerText.includes("Source SQL Server database")`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.SendKeys("#line", "sqlserver-source-db", chromedp.ByID),
		chromedp.Click("button[type=submit]", chromedp.ByQuery),
		chromedp.Poll(`document.querySelector("#transcript").innerText.includes("Source SQL Server username")`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.SendKeys("#line", "sqlserver-source-user", chromedp.ByID),
		chromedp.Click("button[type=submit]", chromedp.ByQuery),
		chromedp.Poll(`document.querySelector("#line").type === "password" && document.querySelector("#transcript").innerText.includes("Source SQL Server password")`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.SendKeys("#line", "browser-sqlserver-password", chromedp.ByID),
		chromedp.Click("button[type=submit]", chromedp.ByQuery),
		chromedp.Poll(`document.querySelector("#line").type === "text" && document.querySelector("#transcript").innerText.includes("Source SQL Server TLS CA certificate path (optional)") && document.querySelector("#transcript").innerText.includes("[REDACTED]")`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	); err != nil {
		t.Fatalf("SQL Server setup credential UI: %v", err)
	}
	var storedHistory string
	if err := chromedp.Run(browser, chromedp.Evaluate(`localStorage.getItem("dmtx-console-history-v2") || ""`, &storedHistory)); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"setup", "sqlserver-source", "1433", "sqlserver-source-db", "sqlserver-source-user", "browser-sqlserver-password"} {
		if strings.Contains(storedHistory, forbidden) {
			t.Fatalf("setup command, answer, or password leaked into local history: %q", storedHistory)
		}
	}

	assertBrowserPWA(t, browser, location)
}

// assertBrowserPWA checks Chromium's parsed manifest representation and waits
// for the browser's actual service-worker registration. It intentionally does
// not drive an install prompt: those prompts are browser policy/UI dependent.
func assertBrowserPWA(t *testing.T, browser context.Context, pageURL string) {
	t.Helper()
	parsedURL, err := url.Parse(pageURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		t.Fatalf("unexpected browser URL %q", pageURL)
	}
	origin := parsedURL.Scheme + "://" + parsedURL.Host

	var worker struct {
		Scope    string `json:"scope"`
		Active   bool   `json:"active"`
		State    string `json:"state"`
		TimedOut bool   `json:"timedOut"`
	}
	if err := chromedp.Run(browser, chromedp.Evaluate(`Promise.race([navigator.serviceWorker.ready.then(registration => ({scope: registration.scope, active: !!registration.active, state: registration.active ? registration.active.state : ""})), new Promise(resolve => setTimeout(() => resolve({timedOut:true}), 5000))])`, &worker, func(params *runtime.EvaluateParams) *runtime.EvaluateParams { return params.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("wait for registered service worker: %v", err)
	}
	if worker.TimedOut || worker.Scope != origin+"/" || !worker.Active || worker.State != "activated" {
		t.Fatalf("service-worker registration = %#v, want active root scope %q", worker, origin+"/")
	}
	var controller bool
	if err := chromedp.Run(browser, chromedp.Poll(`!!navigator.serviceWorker.controller && navigator.serviceWorker.controller.scriptURL === location.origin + "/static/service-worker.js"`, &controller, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("wait for active service-worker control: %v", err)
	}
	if !controller {
		t.Fatal("active service worker did not control the console document")
	}

	// Page.getAppManifest gives Chrome's own parsed representation. The CDP
	// call must run inside chromedp.Run so it inherits the target executor.
	var chromeManifestURL, chromeManifestData string
	var chromeManifestErrors []*page.AppManifestError
	var chromeManifest *page.WebAppManifest
	if err := chromedp.Run(browser, chromedp.ActionFunc(func(ctx context.Context) error {
		var actionErr error
		chromeManifestURL, chromeManifestErrors, chromeManifestData, chromeManifest, actionErr = page.GetAppManifest().Do(ctx)
		return actionErr
	})); err != nil {
		t.Fatalf("Chrome parsed manifest: %v", err)
	}
	if chromeManifestURL != origin+"/static/manifest.webmanifest" || len(chromeManifestErrors) != 0 || chromeManifest == nil || chromeManifestData == "" {
		t.Fatalf("Chrome manifest result: url=%q errors=%#v data=%t manifest=%#v", chromeManifestURL, chromeManifestErrors, chromeManifestData != "", chromeManifest)
	}
	if chromeManifest.Display != "kStandalone" || chromeManifest.StartURL != origin+"/" {
		t.Fatalf("Chrome manifest display/start URL = %q/%q", chromeManifest.Display, chromeManifest.StartURL)
	}
	chromeIcons := map[string]bool{}
	for _, icon := range chromeManifest.Icons {
		if icon != nil && icon.Type == "image/png" {
			chromeIcons[icon.URL+"|"+icon.Sizes] = true
		}
	}
	if !chromeIcons[origin+"/static/icon-192.png|192x192"] || !chromeIcons[origin+"/static/icon-512.png|512x512"] {
		t.Fatalf("Chrome parsed manifest icons = %#v", chromeManifest.Icons)
	}

	// Fetching through the authenticated page supplements Chrome's parsed
	// representation by exercising the same session-bound response path.
	var manifest struct {
		Display  string `json:"display"`
		StartURL string `json:"start_url"`
		Icons    []struct {
			Source string `json:"src"`
			Sizes  string `json:"sizes"`
			Type   string `json:"type"`
		} `json:"icons"`
	}
	if err := chromedp.Run(browser, chromedp.Evaluate(`fetch("/static/manifest.webmanifest").then(response => { if (!response.ok) throw new Error("manifest status " + response.status); return response.json(); })`, &manifest, func(params *runtime.EvaluateParams) *runtime.EvaluateParams { return params.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("Chrome manifest fetch: %v", err)
	}
	if manifest.Display != "standalone" || manifest.StartURL != "/" {
		t.Fatalf("manifest display/start URL = %q/%q", manifest.Display, manifest.StartURL)
	}
	icons := map[string]bool{}
	for _, icon := range manifest.Icons {
		if icon.Type == "image/png" {
			icons[icon.Source+"|"+icon.Sizes] = true
		}
	}
	if !icons["/static/icon-192.png|192x192"] || !icons["/static/icon-512.png|512x512"] {
		t.Fatalf("manifest icons = %#v", manifest.Icons)
	}
	beforeCache := browserShellCachePaths(t, browser)
	wantCache := []string{
		"/static/console.css",
		"/static/console.js",
		"/static/icon-192.png",
		"/static/icon-512.png",
		"/static/icon.svg",
		"/static/manifest.webmanifest",
	}
	if !reflect.DeepEqual(beforeCache, wantCache) {
		t.Fatalf("shell cache entries before authenticated fetches = %v, want %v", beforeCache, wantCache)
	}
	var authenticatedFetches struct {
		Root     int `json:"root"`
		Commands int `json:"commands"`
	}
	if err := chromedp.Run(browser, chromedp.Evaluate(`Promise.all([fetch("/", {credentials:"same-origin"}), fetch("/api/v1/commands", {credentials:"same-origin"})]).then(([root, commands]) => ({root:root.status, commands:commands.status}))`, &authenticatedFetches, func(params *runtime.EvaluateParams) *runtime.EvaluateParams { return params.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("authenticated document/API fetches: %v", err)
	}
	if authenticatedFetches.Root != 200 || authenticatedFetches.Commands != 200 {
		t.Fatalf("authenticated document/API statuses = %#v", authenticatedFetches)
	}
	if afterCache := browserShellCachePaths(t, browser); !reflect.DeepEqual(afterCache, beforeCache) {
		t.Fatalf("shell cache changed after authenticated document/API fetches: before=%v after=%v", beforeCache, afterCache)
	}

	// The registration/manifest proof above is runtime evidence. This final
	// fetch checks the deliberately narrow cache policy without assuming an
	// install prompt is available in a headless browser.
	var apiUncached bool
	if err := chromedp.Run(browser, chromedp.Evaluate(`fetch("/static/service-worker.js").then(r=>r.text()).then(worker => { const cached=worker.slice(worker.indexOf("const shellAssets"), worker.indexOf("self.addEventListener(\"install\"")); return worker.includes('pathname.startsWith("/api/v1/")') && !cached.includes("/api/v1/"); })`, &apiUncached, func(params *runtime.EvaluateParams) *runtime.EvaluateParams { return params.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("PWA cache policy inspection: %v", err)
	}
	if !apiUncached {
		t.Fatal("service worker cache list contains API response paths or lacks API bypass")
	}
}

func browserShellCachePaths(t *testing.T, browser context.Context) []string {
	t.Helper()
	var paths []string
	if err := chromedp.Run(browser, chromedp.Evaluate(`caches.open("dmtx-console-shell-v1").then(cache => cache.keys()).then(requests => requests.map(request => new URL(request.url).pathname).sort())`, &paths, func(params *runtime.EvaluateParams) *runtime.EvaluateParams { return params.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("inspect shell Cache Storage entries: %v", err)
	}
	return paths
}

func seedBrowserProfile(t *testing.T, name string) {
	t.Helper()
	secretsPath, err := secrets.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(secretsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretsPath, []byte("encryption:\n  master_key: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := profiles.OpenWithSecrets(filepath.Join(filepath.Dir(secretsPath), "profiles.db"), secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Save(name, []byte("source:\n  type: sqlite\n  database: source.db\ntarget:\n  type: sqlite\n  database: target.db\nmigration:\n  target_mode: drop_recreate\n")); err != nil {
		t.Fatal(err)
	}
}

// assertBrowserParseParity posts every non-local Supported spelling through
// the authenticated browser fetch path. Parsing is deliberately all it does:
// the table supplies valid, non-destructive syntax for commands whose real
// execution could migrate, initialize, persist profiles, or contact an AI.
func assertBrowserParseParity(t *testing.T, browser context.Context) {
	t.Helper()
	cases := map[string][]string{
		"run":          {"/run --dry-run --config safe.yaml --state safe.state.db"},
		"resume":       {"/resume --config safe.yaml --state safe.state.db"},
		"status":       {"/status --state safe.state.db"},
		"history":      {"/history --state safe.state.db"},
		"validate":     {"/validate --config safe.yaml"},
		"diagnose":     {"/diagnose --config safe.yaml --state safe.state.db"},
		"preflight":    {"/preflight --config safe.yaml"},
		"analyze":      {"/analyze --config safe.yaml"},
		"profile":      {"/profile list"},
		"ai":           {"/ai config-review --config safe.yaml"},
		"init":         {"/init --config safe.yaml"},
		"init-secrets": {"/init-secrets"},
		"config":       {"/config --config safe.yaml"},
	}
	// These Supported registry entries are browser-owned UI actions or the
	// guided setup state machine; they have no /parse request counterpart.
	local := map[string]bool{"setup": true, "session": true, "logs": true, "about": true, "help": true, "clear": true, "quit": true, "exit": true}
	var lines []string
	for _, command := range contract.Commands {
		if command.WebUI != contract.Supported || local[command.Name] {
			continue
		}
		base, ok := cases[command.Name]
		if !ok {
			t.Fatalf("no safe browser parse case for Supported command %q", command.Name)
		}
		for _, spelling := range append([]string{command.Name}, command.Aliases...) {
			for _, line := range base {
				lines = append(lines, "/"+spelling+strings.TrimPrefix(line, "/"+command.Name))
			}
		}
	}
	var replies []browserParseReply
	encodedLines, err := json.Marshal(lines)
	if err != nil {
		t.Fatal(err)
	}
	expression := fmt.Sprintf(`Promise.all(%s.map(line => fetch("/api/v1/parse", {method:"POST", headers:{"Content-Type":"application/json"}, body:JSON.stringify({line:line})}).then(response => response.json().then(body => Object.assign({line:line,status:response.status}, body)))))`, encodedLines)
	if err := chromedp.Run(browser, chromedp.Evaluate(expression, &replies, func(params *runtime.EvaluateParams) *runtime.EvaluateParams { return params.WithAwaitPromise(true) })); err != nil {
		t.Fatalf("browser parse parity fetches: %v", err)
	}
	if len(replies) != len(lines) {
		t.Fatalf("browser parse replies = %d, want %d", len(replies), len(lines))
	}
	for _, reply := range replies {
		if reply.Status != 200 {
			t.Errorf("parse %q status = %d", reply.Line, reply.Status)
			continue
		}
		words, err := splitLine(reply.Line)
		if err != nil {
			t.Fatalf("safe line %q: %v", reply.Line, err)
		}
		words[0] = strings.TrimPrefix(words[0], "/")
		request, outcome, dispatched := app.ParseRequest(words)
		request = app.ApplyCommandDefaults(request)
		if reply.Dispatched != dispatched {
			t.Errorf("parse %q dispatched = %t, want %t", reply.Line, reply.Dispatched, dispatched)
		}
		if dispatched && !reflect.DeepEqual(reply.Request, request) {
			t.Errorf("parse %q request = %#v, want %#v", reply.Line, reply.Request, request)
		}
		if !dispatched && !reflect.DeepEqual(reply.Outcome, outcome) {
			t.Errorf("parse %q outcome = %#v, want %#v", reply.Line, reply.Outcome, outcome)
		}
	}
}

func browserBinary(t *testing.T) string {
	t.Helper()
	if supplied := os.Getenv("DMTX_BROWSER_BINARY"); supplied != "" {
		if _, err := os.Stat(supplied); err != nil {
			t.Fatalf("DMTX_BROWSER_BINARY %q: %v", supplied, err)
		}
		return supplied
	}
	for _, candidate := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		"google-chrome", "chromium", "chromium-browser", "microsoft-edge",
	} {
		if filepath.IsAbs(candidate) {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	if os.Getenv("CI") != "" {
		t.Fatal("no Chrome, Chromium, or Edge binary found; CI must set DMTX_BROWSER_BINARY or install a Chromium-family browser")
	}
	t.Skip("no Chrome, Chromium, or Edge binary found; set DMTX_BROWSER_BINARY to run browser acceptance")
	return ""
}
