package observability

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSinkRedactsAndRemovesRunGauges(t *testing.T) {
	secret := "postgres://alice:secret@example.invalid/private"
	metrics := NewCollector(32)
	var logs bytes.Buffer
	started := time.Now().UTC()
	sink := New(Summary{RunID: secret, Invocation: "run", SourceEngine: "sqlite", TargetEngine: "postgres", StartedAt: started}, Options{LogWriter: &logs, JSONLogs: true, Metrics: metrics})
	sink.Phase("transfer")
	sink.TargetChunkWrite(secret, time.Millisecond, 2, 1, "")
	var live bytes.Buffer
	if err := metrics.WritePrometheus(&live); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(live.String(), secret) || strings.Contains(logs.String(), secret) {
		t.Fatalf("operator sink leaked secret: logs=%s metrics=%s", logs.String(), live.String())
	}
	if !strings.Contains(live.String(), "dmtx_target_chunk_write_duration_seconds_bucket") {
		t.Fatalf("histogram exposition missing buckets: %s", live.String())
	}
	sink.Finish(Summary{Outcome: "success", EndedAt: started.Add(time.Second)})
	var terminal bytes.Buffer
	if err := metrics.WritePrometheus(&terminal); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(terminal.String(), "dmtx_active_writers{") || strings.Contains(terminal.String(), "dmtx_run_identity_info{") {
		t.Fatalf("run gauges remained after finish: %s", terminal.String())
	}
}

func TestCollectorGlobalSeriesCapIncludesHistogram(t *testing.T) {
	c := NewCollector(2)
	c.Set("one", 1, map[string]string{"x": "1"})
	c.Observe("two", 1, map[string]string{"x": "2"})
	c.Add("three_total", 1, map[string]string{"x": "3"})
	if got := c.seriesLocked(); got != 2 {
		t.Fatalf("series=%d, want global cap 2", got)
	}
}

func TestCollectorHasABoundedDefaultSeriesCap(t *testing.T) {
	if got := NewCollector(0).max; got != defaultMaxMetricSeries || got <= 0 {
		t.Fatalf("default series cap=%d, want %d", got, defaultMaxMetricSeries)
	}
}

func TestOTLPHTTPExportUsesTraceServiceJSONAndRedacts(t *testing.T) {
	secret := "postgres://alice:secret@example.invalid/private"
	var got map[string]any
	var contentType string
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode OTLP request: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	started := time.Now().UTC()
	sink := New(Summary{RunID: secret, Invocation: "resume", SourceEngine: "sqlite", TargetEngine: "postgres", StartedAt: started}, Options{LogWriter: &bytes.Buffer{}, OTLPEndpoint: server.URL, OTLPTimeout: time.Second})
	sink.Phase("transfer")
	sink.Finish(Summary{Outcome: "success", EndedAt: started.Add(time.Second)})
	if contentType != "application/json" {
		t.Fatalf("content type=%q", contentType)
	}
	encoded, _ := json.Marshal(got)
	if bytes.Contains(encoded, []byte(secret)) {
		t.Fatalf("OTLP payload leaked secret: %s", encoded)
	}
	resources := got["resourceSpans"].([]any)
	if len(resources) != 1 {
		t.Fatalf("resourceSpans=%#v", resources)
	}
	scopes := resources[0].(map[string]any)["scopeSpans"].([]any)
	spans := scopes[0].(map[string]any)["spans"].([]any)
	if len(spans) < 2 {
		t.Fatalf("spans=%#v", spans)
	}
	root := spans[0].(map[string]any)
	traceID, rootID := root["traceId"].(string), root["spanId"].(string)
	if len(traceID) != 32 || len(rootID) != 16 {
		t.Fatalf("invalid IDs trace=%q span=%q", traceID, rootID)
	}
	if _, err := strconv.ParseInt(root["startTimeUnixNano"].(string), 10, 64); err != nil {
		t.Fatalf("root start nanos: %v", err)
	}
	for _, raw := range spans[1:] {
		span := raw.(map[string]any)
		if span["traceId"] != traceID || span["parentSpanId"] != rootID {
			t.Fatalf("child parentage=%#v", span)
		}
		if _, err := strconv.ParseInt(span["endTimeUnixNano"].(string), 10, 64); err != nil {
			t.Fatalf("child end nanos: %v", err)
		}
		if span["status"].(map[string]any)["code"] != float64(1) {
			t.Fatalf("success status=%#v", span["status"])
		}
	}
}

func TestTextLogFields(t *testing.T) {
	secret := "secret-run-id"
	var logs bytes.Buffer
	sink := New(Summary{RunID: secret, Invocation: "run", SourceEngine: "sqlite", TargetEngine: "postgres", StartedAt: time.Now().UTC()}, Options{LogWriter: &logs})
	sink.log("error", "test failure", "state")
	line := logs.String()
	for _, field := range []string{"source_engine=sqlite", "target_engine=postgres", "invocation=run", "run_id=" + opaqueID(secret), "error_class=state"} {
		if !strings.Contains(line, field) {
			t.Fatalf("text log missing %q: %s", field, line)
		}
	}
}

func TestSlackUsesSingleHashedRunID(t *testing.T) {
	secret := "secret-run-id"
	var mu sync.Mutex
	var text string
	server := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode Slack payload: %v", err)
		}
		mu.Lock()
		text = payload["text"]
		mu.Unlock()
	}))
	defer server.Close()
	sink := New(Summary{RunID: secret, Invocation: "run", SourceEngine: "sqlite", TargetEngine: "postgres", StartedAt: time.Now().UTC()}, Options{LogWriter: &bytes.Buffer{}, SlackWebhook: server.URL})
	sink.postSlack("test", sink.snapshot())
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(text, "run="+opaqueID(secret)) || strings.Contains(text, opaqueID(opaqueID(secret))) || strings.Contains(text, "duration=-") {
		t.Fatalf("slack correlation=%q", text)
	}
}

func newIPv4Server(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listener unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}
