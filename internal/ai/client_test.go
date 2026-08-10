package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/secrets"
)

func TestLMStudioUsesOpenAICompatibleProtocolWithoutCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "" {
			t.Fatal("local LM Studio request sent an authorization header")
		}
		var body map[string]json.RawMessage
		data, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["max_tokens"]; !ok {
			t.Fatal("LM Studio request omitted max_tokens")
		}
		if _, ok := body["max_completion_tokens"]; ok {
			t.Fatal("LM Studio request sent max_completion_tokens")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"ok\"}"}}]}`))
	}))
	defer server.Close()
	client, err := NewClient(&config.AIConfig{Provider: "lmstudio", BaseURL: server.URL, Model: "qwen-local"}, secrets.Config{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Generate(context.Background(), "facts")
	if err != nil || got != `{"summary":"ok"}` {
		t.Fatalf("Generate() = %q, %v", got, err)
	}
	if client.ProviderName() != "lmstudio" || client.Protocol() != ProtocolOpenAICompat {
		t.Fatalf("provider = %s/%s", client.ProviderName(), client.Protocol())
	}
}

func TestOpenAIUsesCompletionTokensAndBearerAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer mock-openai-key" {
			t.Fatalf("authorization header = %q", request.Header.Get("Authorization"))
		}
		var body map[string]json.RawMessage
		data, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		var model string
		if err := json.Unmarshal(body["model"], &model); err != nil {
			t.Fatal(err)
		}
		var maxCompletionTokens int
		if err := json.Unmarshal(body["max_completion_tokens"], &maxCompletionTokens); err != nil {
			t.Fatal(err)
		}
		if model != "gpt-5.6-luna" || maxCompletionTokens != 256 {
			t.Fatalf("OpenAI request model/tokens = %q/%d", model, maxCompletionTokens)
		}
		if _, ok := body["max_tokens"]; ok {
			t.Fatal("OpenAI request sent max_tokens")
		}
		_, _ = writer.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"{\\\"summary\\\":\\\"ok\\\"}\"}}]}"))
	}))
	defer server.Close()
	t.Setenv("AI_TEST_OPENAI_KEY", "mock-openai-key")
	client, err := NewClient(&config.AIConfig{
		Provider: "openai", Protocol: ProtocolOpenAI, BaseURL: server.URL, Model: "gpt-5.6-luna",
		MaxTokens: 256, APIKey: "${env:AI_TEST_OPENAI_KEY}",
	}, secrets.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Generate(context.Background(), "facts"); err != nil {
		t.Fatal(err)
	}
}

func TestGoogleUsesGoogleProtocolAndRedactsCredentialFromURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1beta/models/gemini-test:generateContent" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		if request.URL.RawQuery != "" || request.Header.Get("x-goog-api-key") != "secret-key" {
			t.Fatalf("Google credential placement is unsafe: query=%q header=%q", request.URL.RawQuery, request.Header.Get("x-goog-api-key"))
		}
		_, _ = writer.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"summary\":\"ok\"}"}]}}]}`))
	}))
	defer server.Close()
	t.Setenv("AI_TEST_KEY", "secret-key")
	client, err := NewClient(&config.AIConfig{Provider: "google", Protocol: "google", BaseURL: server.URL, Model: "gemini-test", APIKey: "${env:AI_TEST_KEY}"}, secrets.Config{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Generate(context.Background(), "facts")
	if err != nil || got != `{"summary":"ok"}` {
		t.Fatalf("Generate() = %q, %v", got, err)
	}
}

func TestRemoteHTTPAndPlaintextCredentialAreRefused(t *testing.T) {
	if _, err := NewClient(&config.AIConfig{Provider: "custom", BaseURL: "http://example.com", APIKey: "${env:KEY}"}, secrets.Config{}); err == nil {
		t.Fatal("expected remote HTTP endpoint refusal")
	}
	if _, err := NewClient(&config.AIConfig{Provider: "openai", APIKey: "plain-secret"}, secrets.Config{}); err == nil || strings.Contains(err.Error(), "plain-secret") {
		t.Fatalf("plaintext credential was not safely refused: %v", err)
	}
}

func TestGenerateHonorsCancellationAndRequestLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer func() {
		server.CloseClientConnections()
		server.Close()
	}()
	client, err := NewClient(&config.AIConfig{Provider: "lmstudio", BaseURL: server.URL, MaxRequests: 1, TimeoutSeconds: 1}, secrets.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.Generate(ctx, "facts"); err == nil {
		t.Fatal("expected cancellation/timeout")
	}
	if _, err := client.Generate(context.Background(), "facts"); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected bounded request error, got %v", err)
	}
}

func TestDecodeAdvisoryAcceptsSingleJSONCodeFence(t *testing.T) {
	advisory, err := DecodeAdvisory(strings.Repeat(string(rune(96)), 3) + "json\n{\"summary\":\"ok\"}\n" + strings.Repeat(string(rune(96)), 3))
	if err != nil || advisory.Summary != "ok" {
		t.Fatalf("fenced advisory = %+v, %v", advisory, err)
	}
}

func TestDecodeAdvisoryClassifiesBoundedFailures(t *testing.T) {
	cases := []struct {
		name string
		data string
		want ParseFailureClass
	}{
		{name: "empty", data: "", want: ParseFailureEmpty},
		{name: "fence shape", data: strings.Repeat(string(rune(96)), 3) + "json\n{\"summary\":\"ok\"}", want: ParseFailureFenceShape},
		{name: "json syntax", data: "{\"summary\":", want: ParseFailureJSONSyntax},
		{name: "schema validation", data: "{\"summary\":\"ok\",\"patch_recommendations\":[]}", want: ParseFailureSchemaValidation},
		{name: "trailing prose", data: "{\"summary\":\"ok\"}\nAdditional explanation", want: ParseFailureJSONSyntax},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeAdvisory(tc.data)
			if got := ParseFailureClassOf(err); got != tc.want {
				t.Fatalf("parse class = %q, want %q (err=%v)", got, tc.want, err)
			}
			if strings.Contains(err.Error(), tc.data) && tc.data != "" {
				t.Fatal("parse error retained response content")
			}
		})
	}
}

func TestDecodeAdvisoryRejectsUnknownAndOversizedFields(t *testing.T) {
	if _, err := DecodeAdvisory(`{"summary":"ok","secret":"must reject"}`); err == nil {
		t.Fatal("unknown advisory field accepted")
	}
	if _, err := DecodeAdvisory(`{"summary":""}`); err == nil {
		t.Fatal("empty advisory summary accepted")
	}
}
