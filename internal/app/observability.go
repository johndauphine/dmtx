package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/migrate"
	"github.com/johndauphine/dmtx/internal/observability"
)

func startOperatorSink(cfg config.Config, summary observability.Summary) *observability.Sink {
	collector := observability.NewCollector(cfg.Observability.MaxMetricSeries)
	sink := observability.New(summary, observability.Options{
		LogWriter: os.Stderr, JSONLogs: cfg.Observability.LogFormat == "json",
		Metrics: collector, OTLPEndpoint: cfg.Observability.OTLPEndpoint,
		OTLPTimeout: cfg.Observability.OTLPTimeout, SlackWebhook: cfg.Slack.WebhookURL,
		NotifySuccess: cfg.Slack.NotifySuccess, NotifyFailure: cfg.Slack.NotifyFailure,
	})
	if server, err := observability.ServePrometheus(cfg.Observability.PrometheusBind, collector); err == nil {
		sink.AttachPrometheusServer(server)
	} else {
		// Sink setup is non-authoritative. Keep a bounded diagnostic local and
		// continue the migration rather than making a scrape listener a policy.
		fmt.Fprintf(os.Stderr, "dmtx observability: prometheus disabled: %v\n", err)
	}
	return sink
}

func operatorSummary(runID, invocation string, cfg config.Config, started time.Time) observability.Summary {
	return observability.Summary{RunID: runID, Invocation: invocation, SourceEngine: cfg.Source.Type, TargetEngine: cfg.Target.Type, StartedAt: started}
}

func reportRuntimeTuningMetrics(sink *observability.Sink, result migrate.Result) {
	if sink == nil || result.RuntimeTuning == nil {
		return
	}
	count := 0
	for _, table := range result.RuntimeTuning.Tables {
		count += len(table.Adjustments)
	}
	sink.RuntimeAdjustments(count)
}

func (observer tableCheckpointObserver) ObserveNetworkTelemetry(fact migrate.NetworkTelemetry) {
	if observer.operator == nil {
		return
	}
	table := strings.TrimPrefix(fact.TableSchema+"."+fact.TableName, ".")
	if fact.Duration > 0 {
		observer.operator.TargetChunkWrite(table, fact.Duration, fact.ActiveWriters, fact.QueueDepth, "")
		observer.operator.ObservedPayloadBytes(table, fact.PayloadBytes)
	}
	if fact.RetryClass != "" {
		observer.operator.Retry("write", fact.RetryClass)
	}
}
func (observer tableCheckpointObserver) ObserveTargetWriteTelemetry(fact migrate.TargetWriteTelemetry) {
	if observer.operator == nil {
		return
	}
	observer.operator.TargetChunkWrite(fact.Table, fact.Duration, fact.ActiveWriters, fact.QueueDepth, "")
}
func (observer tableCheckpointObserver) ObserveWriterQueueDepth(depth int) {
	if observer.operator != nil {
		observer.operator.WriterQueueDepth(depth)
	}
}
func (observer tableCheckpointObserver) ObservePayloadBytes(table string, bytes int64) {
	if observer.operator != nil {
		observer.operator.ObservedPayloadBytes(table, bytes)
	}
}
func (observer tableCheckpointObserver) ObserveMigrationFallback(kind string) {
	if observer.operator != nil {
		observer.operator.Fallback(kind)
	}
}
func (observer tableCheckpointObserver) ObserveMigrationRetry(operation string) {
	if observer.operator != nil {
		observer.operator.Retry(operation, "")
	}
}
func (observer tableCheckpointObserver) ObserveMigrationPhase(phase string) {
	if observer.operator != nil {
		observer.operator.Phase(phase)
	}
}
