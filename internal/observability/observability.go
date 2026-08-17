// Package observability contains bounded, best-effort operator sinks. Nothing
// in this package returns an error to the migration engine: telemetry must
// never change a migration decision or durable state transition.
package observability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Summary struct {
	RunID, Invocation, SourceEngine, TargetEngine string
	Outcome, ErrorClass, Phase                    string
	Resumable                                     bool
	Rows, Tables                                  int64
	StartedAt, EndedAt                            time.Time
}

type Options struct {
	LogWriter                    io.Writer
	JSONLogs                     bool
	Metrics                      *Collector
	OTLPEndpoint                 string
	OTLPTimeout                  time.Duration
	SlackWebhook                 string
	NotifySuccess, NotifyFailure bool
}

type Sink struct {
	mu                           sync.Mutex
	summary                      Summary
	phase                        string
	phases                       map[string]time.Time
	observed                     []phaseTiming
	logWriter                    io.Writer
	jsonLogs                     bool
	metrics                      *Collector
	otlpEndpoint                 string
	otlpTimeout                  time.Duration
	slackWebhook                 string
	notifySuccess, notifyFailure bool
	server                       *http.Server
}

// ServePrometheus starts the opt-in /metrics endpoint. Bind/listen failures
// are intentionally returned to the application so configuration admission is
// visible, but request/write failures never touch the migration.
func ServePrometheus(bind string, collector *Collector) (*http.Server, error) {
	if strings.TrimSpace(bind) == "" {
		return nil, nil
	}
	listener, err := net.Listen("tcp", bind)
	if err != nil {
		return nil, err
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = collector.WritePrometheus(w)
	})}
	go func() { _ = server.Serve(listener) }()
	return server, nil
}

func (sink *Sink) AttachPrometheusServer(server *http.Server) {
	if sink != nil {
		sink.server = server
	}
}

type phaseTiming struct {
	Phase              string
	StartedAt, EndedAt time.Time
}

func New(summary Summary, options Options) *Sink {
	if options.LogWriter == nil {
		options.LogWriter = io.Discard
	}
	if options.Metrics == nil {
		options.Metrics = NewCollector(0)
	}
	sink := &Sink{summary: sanitizeSummary(summary), phases: make(map[string]time.Time), logWriter: options.LogWriter, jsonLogs: options.JSONLogs, metrics: options.Metrics, otlpEndpoint: options.OTLPEndpoint, otlpTimeout: options.OTLPTimeout, slackWebhook: options.SlackWebhook, notifySuccess: options.NotifySuccess, notifyFailure: options.NotifyFailure}
	if sink.summary.StartedAt.IsZero() {
		sink.summary.StartedAt = time.Now().UTC()
	}
	sink.phase = "preflight"
	sink.phases[sink.phase] = sink.summary.StartedAt
	labels := summaryLabels(sink.summary)
	sink.metrics.Set("dmtx_run_identity_info", 1, labels)
	sink.log("info", "migration started", "")
	if sink.slackWebhook != "" && (sink.notifySuccess || sink.notifyFailure) {
		sink.postSlack("migration started", sink.summary)
	}
	return sink
}

// Phase records only a phase actually entered by the engine/app. It does not
// manufacture missing spans or timings.
func (sink *Sink) Phase(phase string) {
	if sink == nil || !stablePhase(phase) {
		return
	}
	now := time.Now().UTC()
	sink.mu.Lock()
	sink.closePhaseLocked(sink.phase, now)
	sink.phase = phase
	sink.phases[phase] = now
	sink.summary.Phase = phase
	sink.mu.Unlock()
	sink.log("info", "migration phase entered", "")
}

func (sink *Sink) Progress(table string, rows, estimatedBytes int64) {
	if sink == nil {
		return
	}
	labels := summaryLabels(sink.snapshot())
	if table != "" {
		labels["table_id"] = opaqueID(table)
	}
	if rows > 0 {
		sink.metrics.Add("dmtx_migration_rows_total", float64(rows), labels)
	}
	if estimatedBytes > 0 {
		sink.metrics.Add("dmtx_migration_estimated_bytes_total", float64(estimatedBytes), labels)
	}
}
func (sink *Sink) Error(class string) {
	if sink != nil && class != "" {
		labels := summaryLabels(sink.snapshot())
		labels["error_class"] = safeToken(class)
		sink.metrics.Add("dmtx_migration_errors_total", 1, labels)
	}
}
func (sink *Sink) Retry(operation, class string) {
	if sink != nil {
		labels := summaryLabels(sink.snapshot())
		labels["operation"] = safeToken(operation)
		if class != "" {
			labels["error_class"] = safeToken(class)
		}
		sink.metrics.Add("dmtx_migration_retries_total", 1, labels)
	}
}
func (sink *Sink) Fallback(kind string) {
	if sink != nil {
		labels := summaryLabels(sink.snapshot())
		labels["fallback"] = safeToken(kind)
		sink.metrics.Add("dmtx_fallback_events_total", 1, labels)
	}
}
func (sink *Sink) RuntimeAdjustments(n int) {
	if sink != nil && n > 0 {
		sink.metrics.Add("dmtx_runtime_adjustments_total", float64(n), summaryLabels(sink.snapshot()))
	}
}
func (sink *Sink) TargetChunkWrite(table string, duration time.Duration, activeWriters, queueDepth int, retryClass string) {
	if sink == nil {
		return
	}
	labels := summaryLabels(sink.snapshot())
	if table != "" {
		labels["table_id"] = opaqueID(table)
	}
	sink.metrics.Observe("dmtx_target_chunk_write_duration_seconds", duration.Seconds(), labels)
	if activeWriters >= 0 {
		sink.metrics.Set("dmtx_active_writers", float64(activeWriters), summaryLabels(sink.snapshot()))
	}
	if queueDepth >= 0 {
		sink.metrics.Set("dmtx_writer_queue_depth", float64(queueDepth), summaryLabels(sink.snapshot()))
	}
	if retryClass != "" {
		sink.Retry("write", retryClass)
	}
}

func (sink *Sink) WriterQueueDepth(depth int) {
	if sink != nil && depth >= 0 {
		sink.metrics.Set("dmtx_writer_queue_depth", float64(depth), summaryLabels(sink.snapshot()))
	}
}

// ObservedPayloadBytes records exact bounded payload bytes where a transfer
// route exposes them. An exact observation is stronger than an estimate.
func (sink *Sink) ObservedPayloadBytes(table string, bytes int64) {
	if sink == nil || bytes <= 0 {
		return
	}
	labels := summaryLabels(sink.snapshot())
	if table != "" {
		labels["table_id"] = opaqueID(table)
	}
	sink.metrics.Add("dmtx_migration_estimated_bytes_total", float64(bytes), labels)
}

func (sink *Sink) Finish(summary Summary) {
	if sink == nil {
		return
	}
	if summary.EndedAt.IsZero() {
		summary.EndedAt = time.Now().UTC()
	}
	sink.mu.Lock()
	summary = mergeSummary(sink.summary, summary)
	sink.closePhaseLocked(sink.phase, summary.EndedAt)
	observed := append([]phaseTiming(nil), sink.observed...)
	sink.summary = summary
	sink.mu.Unlock()
	labels := summaryLabels(summary)
	if summary.Outcome != "success" && summary.ErrorClass != "" {
		sink.Error(summary.ErrorClass)
	}
	sink.metrics.Set("dmtx_active_writers", 0, labels)
	sink.metrics.Set("dmtx_writer_queue_depth", 0, labels)
	if summary.Outcome == "success" {
		sink.log("info", "migration completed", "")
	} else {
		sink.log("error", "migration completed without success", summary.ErrorClass)
	}
	if sink.otlpEndpoint != "" {
		sink.postTraces(summary, observed)
	}
	if sink.slackWebhook != "" && ((summary.Outcome == "success" && sink.notifySuccess) || (summary.Outcome != "success" && sink.notifyFailure)) {
		sink.postSlack("migration completed", summary)
	}
	sink.metrics.RemoveRun(summary.RunID)
	if sink.server != nil {
		_ = sink.server.Close()
	}
}

func (sink *Sink) closePhaseLocked(phase string, end time.Time) {
	start, ok := sink.phases[phase]
	if !ok {
		return
	}
	delete(sink.phases, phase)
	if end.Before(start) {
		end = start
	}
	sink.observed = append(sink.observed, phaseTiming{phase, start, end})
	sink.metrics.Set("dmtx_migration_phase_duration_seconds", end.Sub(start).Seconds(), phaseLabels(sink.summary, phase))
}
func (sink *Sink) snapshot() Summary { sink.mu.Lock(); defer sink.mu.Unlock(); return sink.summary }
func (sink *Sink) log(level, message, class string) {
	s := sink.snapshot()
	record := map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "level": level, "message": message, "run_id": s.RunID, "phase": s.Phase, "source_engine": safeToken(s.SourceEngine), "target_engine": safeToken(s.TargetEngine), "invocation": safeToken(s.Invocation)}
	if class != "" {
		record["error_class"] = safeToken(class)
	}
	var text string
	if sink.jsonLogs {
		b, _ := json.Marshal(record)
		text = string(b)
	} else {
		text = fmt.Sprintf("%s level=%s run_id=%s phase=%s source_engine=%s target_engine=%s invocation=%s message=%q", record["timestamp"], level, s.RunID, s.Phase, s.SourceEngine, s.TargetEngine, s.Invocation, message)
		if class != "" {
			text += " error_class=" + safeToken(class)
		}
	}
	_, _ = io.WriteString(sink.logWriter, text+"\n")
}

func (sink *Sink) postSlack(event string, summary Summary) {
	duration := time.Duration(0)
	if !summary.StartedAt.IsZero() && !summary.EndedAt.IsZero() && !summary.EndedAt.Before(summary.StartedAt) {
		duration = summary.EndedAt.Sub(summary.StartedAt).Round(time.Millisecond)
	}
	payload := map[string]string{"text": fmt.Sprintf("DMTX %s: outcome=%s run=%s rows=%d tables=%d resumable=%t duration=%s", event, safeToken(summary.Outcome), summary.RunID, summary.Rows, summary.Tables, summary.Resumable, duration)}
	sink.postJSON(sink.slackWebhook, payload)
}
func (sink *Sink) postTraces(summary Summary, observed []phaseTiming) {
	attempt := summary.RunID + ":" + summary.Invocation + ":" + strconv.FormatInt(summary.StartedAt.UnixNano(), 10)
	traceID := otlpID("trace", attempt, 16)
	rootSpanID := otlpID("root", attempt, 8)
	spans := make([]any, 0, len(observed)+1)
	spans = append(spans, otlpSpan(traceID, rootSpanID, "", "dmtx.migration", summary.StartedAt, summary.EndedAt, otlpAttributes(summaryLabels(summary)), summary))
	for index, phase := range observed {
		spanID := otlpID("phase", attempt+":"+phase.Phase+":"+strconv.Itoa(index), 8)
		spans = append(spans, otlpSpan(traceID, spanID, rootSpanID, "dmtx."+phase.Phase, phase.StartedAt, phase.EndedAt, otlpAttributes(phaseLabels(summary, phase.Phase)), summary))
	}
	payload := map[string]any{"resourceSpans": []any{map[string]any{
		"resource":   map[string]any{"attributes": otlpAttributes(map[string]string{"service.name": "dmtx"})},
		"scopeSpans": []any{map[string]any{"scope": map[string]any{"name": "dmtx.observability"}, "spans": spans}},
	}}}
	sink.postJSON(sink.otlpEndpoint, payload)
}

func otlpSpan(traceID, spanID, parentSpanID, name string, started, ended time.Time, attributes []any, summary Summary) map[string]any {
	if ended.Before(started) {
		ended = started
	}
	status := map[string]any{"code": 1}
	if summary.Outcome != "" && summary.Outcome != "success" {
		status = map[string]any{"code": 2, "message": summary.ErrorClass}
	}
	span := map[string]any{"traceId": traceID, "spanId": spanID, "name": name, "startTimeUnixNano": strconv.FormatInt(started.UnixNano(), 10), "endTimeUnixNano": strconv.FormatInt(ended.UnixNano(), 10), "attributes": attributes, "status": status}
	if parentSpanID != "" {
		span["parentSpanId"] = parentSpanID
	}
	return span
}

func otlpAttributes(values map[string]string) []any {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	attributes := make([]any, 0, len(keys))
	for _, key := range keys {
		attributes = append(attributes, map[string]any{"key": key, "value": map[string]string{"stringValue": values[key]}})
	}
	return attributes
}

func otlpID(namespace, value string, bytes int) string {
	sum := sha256.Sum256([]byte(namespace + ":" + value))
	return hex.EncodeToString(sum[:bytes])
}
func (sink *Sink) postJSON(endpoint string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	timeout := sink.otlpTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil || response == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	_ = response.Body.Close()
}

type Collector struct {
	mu     sync.Mutex
	max    int
	scalar map[string]map[string]sample
	hist   map[string]map[string]*histogram
}
type sample struct {
	labels map[string]string
	value  float64
}
type histogram struct {
	labels  map[string]string
	count   uint64
	sum     float64
	buckets [5]uint64
}

const defaultMaxMetricSeries = 2048

func NewCollector(max int) *Collector {
	if max <= 0 {
		max = defaultMaxMetricSeries
	}
	return &Collector{max: max, scalar: make(map[string]map[string]sample), hist: make(map[string]map[string]*histogram)}
}
func (c *Collector) Set(name string, value float64, labels map[string]string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := labelKey(labels)
	if _, ok := c.scalar[name][key]; !ok {
		if !c.admitLocked() {
			return
		}
		if c.scalar[name] == nil {
			c.scalar[name] = map[string]sample{}
		}
	}
	c.scalar[name][key] = sample{copyLabels(labels), value}
}
func (c *Collector) Add(name string, value float64, labels map[string]string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := labelKey(labels)
	existing, ok := c.scalar[name][key]
	if !ok {
		if !c.admitLocked() {
			return
		}
		if c.scalar[name] == nil {
			c.scalar[name] = map[string]sample{}
		}
		existing = sample{copyLabels(labels), 0}
	}
	existing.value += value
	c.scalar[name][key] = existing
}
func (c *Collector) Observe(name string, value float64, labels map[string]string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := labelKey(labels)
	if c.hist[name] == nil {
		c.hist[name] = map[string]*histogram{}
	}
	h, ok := c.hist[name][key]
	if !ok {
		if !c.admitLocked() {
			return
		}
		h = &histogram{labels: copyLabels(labels)}
		c.hist[name][key] = h
	}
	h.count++
	h.sum += value
	for i, b := range [...]float64{.01, .1, 1, 10, 30} {
		if value <= b {
			h.buckets[i]++
		}
	}
}
func (c *Collector) admitLocked() bool { return c.max <= 0 || c.seriesLocked() < c.max }
func (c *Collector) seriesLocked() int {
	n := 0
	for _, m := range c.scalar {
		n += len(m)
	}
	for _, m := range c.hist {
		n += len(m)
	}
	return n
}
func (c *Collector) RemoveRun(run string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := map[string]struct{}{run: {}, opaqueID(run): {}}
	for _, name := range []string{"dmtx_migration_phase_duration_seconds", "dmtx_writer_queue_depth", "dmtx_active_writers", "dmtx_run_identity_info"} {
		for key, s := range c.scalar[name] {
			if _, ok := ids[s.labels["run_id"]]; ok {
				delete(c.scalar[name], key)
			}
		}
	}
}
func (c *Collector) WritePrometheus(w io.Writer) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, 0, len(c.scalar)+len(c.hist))
	seen := map[string]bool{}
	for n := range c.scalar {
		seen[n] = true
		names = append(names, n)
	}
	for n := range c.hist {
		if !seen[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		kind := "gauge"
		if strings.HasSuffix(n, "_total") {
			kind = "counter"
		}
		if _, ok := c.hist[n]; ok {
			kind = "histogram"
		}
		if _, err := fmt.Fprintf(w, "# TYPE %s %s\n", n, kind); err != nil {
			return err
		}
		for _, s := range c.scalar[n] {
			if _, err := fmt.Fprintf(w, "%s%s %s\n", n, formatLabels(s.labels), strconv.FormatFloat(s.value, 'g', -1, 64)); err != nil {
				return err
			}
		}
		for _, h := range c.hist[n] {
			for i, b := range [...]float64{.01, .1, 1, 10, 30} {
				l := copyLabels(h.labels)
				l["le"] = strconv.FormatFloat(b, 'g', -1, 64)
				if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", n, formatLabels(l), h.buckets[i]); err != nil {
					return err
				}
			}
			l := copyLabels(h.labels)
			l["le"] = "+Inf"
			if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n%s_sum%s %g\n%s_count%s %d\n", n, formatLabels(l), h.count, n, formatLabels(h.labels), h.sum, n, formatLabels(h.labels), h.count); err != nil {
				return err
			}
		}
	}
	return nil
}

func stablePhase(v string) bool {
	switch v {
	case "preflight", "schema_extraction", "target_preparation", "transfer", "finalization", "validation":
		return true
	}
	return false
}
func sanitizeSummary(s Summary) Summary {
	s.RunID = opaqueID(s.RunID)
	s.Invocation = safeToken(s.Invocation)
	s.SourceEngine = safeToken(s.SourceEngine)
	s.TargetEngine = safeToken(s.TargetEngine)
	s.Outcome = safeToken(s.Outcome)
	s.ErrorClass = safeToken(s.ErrorClass)
	return s
}
func mergeSummary(base, update Summary) Summary {
	update = sanitizeSummary(update)
	if update.RunID == "" {
		update.RunID = base.RunID
	}
	if update.Invocation == "" {
		update.Invocation = base.Invocation
	}
	if update.SourceEngine == "" {
		update.SourceEngine = base.SourceEngine
	}
	if update.TargetEngine == "" {
		update.TargetEngine = base.TargetEngine
	}
	if update.StartedAt.IsZero() {
		update.StartedAt = base.StartedAt
	}
	return update
}
func summaryLabels(s Summary) map[string]string {
	return map[string]string{"run_id": s.RunID, "source_engine": s.SourceEngine, "target_engine": s.TargetEngine, "invocation": s.Invocation}
}
func phaseLabels(s Summary, p string) map[string]string {
	l := summaryLabels(s)
	l["phase"] = p
	return l
}
func opaqueID(v string) string {
	if v == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:8])
}
func safeToken(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if len(v) > 48 {
		v = v[:48]
	}
	var b strings.Builder
	for _, r := range v {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func copyLabels(v map[string]string) map[string]string {
	out := make(map[string]string, len(v))
	for k, x := range v {
		out[k] = x
	}
	return out
}
func labelKey(v map[string]string) string { return formatLabels(v) }
func formatLabels(v map[string]string) string {
	if len(v) == 0 {
		return ""
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"=\""+strings.ReplaceAll(strings.ReplaceAll(v[k], "\\", "\\\\"), "\"", "\\\"")+"\"")
	}
	return "{" + strings.Join(parts, ",") + "}"
}
