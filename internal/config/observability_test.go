package config

import (
	"strings"
	"testing"
)

func TestObservabilityYAMLAdmissionAndSanitize(t *testing.T) {
	cfg, err := Parse([]byte("source: {type: sqlite, database: source.db}\ntarget: {type: sqlite, database: target.db}\nobservability:\n  log_format: json\n  prometheus_bind: 127.0.0.1:9091\n  otlp_endpoint: https://collector.example/v1/traces\n  otlp_timeout: 3s\n  max_metric_series: 12\nslack:\n  webhook_url: https://hooks.example/secret\n  notify_success: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := Sanitize(cfg).Slack.WebhookURL; got != "[REDACTED]" {
		t.Fatalf("webhook sanitize=%q", got)
	}
	if _, err := Parse([]byte("source: {}\ntarget: {}\nobservability: {log_format: xml}\n")); err == nil || !strings.Contains(err.Error(), "log_format") {
		t.Fatalf("invalid format error=%v", err)
	}
	if _, err := Parse([]byte("source: {}\ntarget: {}\nobservability: {otlp_endpoint: 'https://u:p@example/v1'}\n")); err == nil || !strings.Contains(err.Error(), "without credentials") {
		t.Fatalf("unsafe endpoint error=%v", err)
	}
	if _, err := Parse([]byte("source: {}\ntarget: {}\nslack: {webhook_url: 'https://u:p@example/v1'}\n")); err == nil || !strings.Contains(err.Error(), "webhook_url") {
		t.Fatalf("unsafe webhook error=%v", err)
	}
	if _, err := Parse([]byte("source: {}\ntarget: {}\nslack: {unknown: true}\n")); err == nil || !strings.Contains(err.Error(), "slack.unknown") {
		t.Fatalf("unknown Slack field error=%v", err)
	}
}
