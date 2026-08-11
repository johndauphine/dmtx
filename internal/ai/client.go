package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/secrets"
)

const (
	ProtocolOpenAI       = "openai"
	ProtocolAnthropic    = "anthropic"
	ProtocolGoogle       = "google"
	ProtocolOpenAICompat = "openai_compatible"
	defaultTimeout       = 30 * time.Second
	defaultMaxTokens     = 2048
	maxPromptBytes       = 64 << 10
	maxResponseBytes     = 1 << 20
)

// Provider is the resolved, in-memory endpoint. APIKey is never serialised or
// included in errors; it exists only for the duration of a request.
type Provider struct {
	Name           string
	Protocol       string
	BaseURL        string
	Model          string
	APIKey         string
	Timeout        time.Duration
	MaxTokens      int
	MaxRequests    int
	RequestsUsed   int
	localInference bool
}

// Client is a bounded advisory client. It has no persistence and does not
// expose provider credentials through its public API.
type Client struct {
	provider Provider
	http     *http.Client
	mu       sync.Mutex
}

func (client *Client) ProviderName() string { return client.provider.Name }
func (client *Client) Model() string        { return client.provider.Model }
func (client *Client) Protocol() string     { return client.provider.Protocol }

func NewClient(cfg *config.AIConfig, global secrets.Config) (*Client, error) {
	if cfg != nil {
		if err := cfg.Validate(); err != nil {
			return nil, err
		}
	}
	name := ""
	if cfg != nil {
		name = strings.ToLower(strings.TrimSpace(cfg.Provider))
	}
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(global.AI.DefaultProvider))
	}
	if name == "" {
		return nil, errors.New("no AI provider configured")
	}
	resolved := Provider{Name: name}
	if base, ok := global.AI.Providers[name]; ok && base != nil {
		resolved.Protocol = base.Protocol
		resolved.BaseURL = base.BaseURL
		resolved.Model = base.Model
		resolved.APIKey = base.APIKey
		resolved.MaxTokens = base.MaxTokens
		resolved.MaxRequests = base.MaxRequests
		resolved.Timeout = time.Duration(base.TimeoutSeconds) * time.Second
	}
	if cfg != nil {
		if cfg.Protocol != "" {
			resolved.Protocol = cfg.Protocol
		}
		if cfg.BaseURL != "" {
			resolved.BaseURL = cfg.BaseURL
		}
		if cfg.Model != "" {
			resolved.Model = cfg.Model
		}
		if cfg.MaxTokens > 0 {
			resolved.MaxTokens = cfg.MaxTokens
		}
		if cfg.MaxRequests > 0 {
			resolved.MaxRequests = cfg.MaxRequests
		}
		if cfg.TimeoutSeconds > 0 {
			resolved.Timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
		}
		if cfg.APIKey != "" {
			key, err := resolveOrigin(cfg.APIKey, global, name)
			if err != nil {
				return nil, err
			}
			resolved.APIKey = key
		}
	}
	resolved.Protocol = normalizeProtocol(name, resolved.Protocol, resolved.BaseURL)
	if resolved.BaseURL == "" {
		resolved.BaseURL = defaultBaseURL(name, resolved.Protocol)
	}
	if resolved.Model == "" {
		resolved.Model = "default"
	}
	if resolved.MaxTokens == 0 {
		resolved.MaxTokens = defaultMaxTokens
	}
	if resolved.Timeout == 0 {
		resolved.Timeout = defaultTimeout
	}
	if resolved.Timeout < time.Second || resolved.Timeout > 10*time.Minute {
		return nil, errors.New("AI timeout must be between 1 second and 10 minutes")
	}
	resolved.localInference = isLoopbackURL(resolved.BaseURL)
	if err := validateEndpoint(resolved.BaseURL, resolved.localInference); err != nil {
		return nil, err
	}
	if resolved.Protocol != ProtocolOpenAICompat && resolved.APIKey == "" {
		return nil, fmt.Errorf("AI provider %s requires a protected credential origin", resolved.Name)
	}
	return &Client{provider: resolved, http: &http.Client{Timeout: resolved.Timeout}}, nil
}

func normalizeProtocol(name, protocol, baseURL string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol != "" {
		if protocol == "openai_chat" || protocol == "openai-compatible" {
			return ProtocolOpenAICompat
		}
		return protocol
	}
	switch name {
	case "anthropic":
		return ProtocolAnthropic
	case "google", "gemini":
		return ProtocolGoogle
	case "openai":
		return ProtocolOpenAI
	default:
		_ = baseURL
		return ProtocolOpenAICompat
	}
}

func defaultBaseURL(name, protocol string) string {
	switch protocol {
	case ProtocolAnthropic:
		return "https://api.anthropic.com"
	case ProtocolGoogle:
		return "https://generativelanguage.googleapis.com"
	case ProtocolOpenAI:
		return "https://api.openai.com"
	case ProtocolOpenAICompat:
		if name == "lmstudio" {
			return "http://127.0.0.1:1234"
		}
		if name == "ollama" {
			return "http://127.0.0.1:11434"
		}
	}
	return ""
}

func validateEndpoint(raw string, local bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("AI endpoint must be an absolute URL")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errors.New("AI endpoint must not include userinfo, query, or fragment")
	}
	if u.Scheme != "https" && !(local && u.Scheme == "http") {
		return errors.New("non-loopback AI endpoints require HTTPS")
	}
	return nil
}

func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func resolveOrigin(origin string, global secrets.Config, providerName string) (string, error) {
	value := strings.TrimSpace(origin)
	if strings.HasPrefix(value, "${env:") && strings.HasSuffix(value, "}") {
		name := strings.TrimSuffix(strings.TrimPrefix(value, "${env:"), "}")
		if name == "" {
			return "", errors.New("AI credential environment origin is empty")
		}
		secret := os.Getenv(name)
		if secret == "" {
			return "", errors.New("AI credential environment origin is unavailable")
		}
		return secret, nil
	}
	if strings.HasPrefix(value, "${file:") && strings.HasSuffix(value, "}") {
		path := strings.TrimSuffix(strings.TrimPrefix(value, "${file:"), "}")
		if path == "" {
			return "", errors.New("AI credential file origin is empty")
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return "", errors.New("AI credential file origin is unavailable")
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			return "", errors.New("AI credential file origin has insecure permissions")
		}
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil || strings.TrimSpace(string(data)) == "" {
			return "", errors.New("AI credential file origin is unavailable")
		}
		return strings.TrimSpace(string(data)), nil
	}
	if strings.HasPrefix(value, "${secret:") && strings.HasSuffix(value, "}") {
		name := strings.TrimSuffix(strings.TrimPrefix(value, "${secret:"), "}")
		if name == "" {
			name = providerName
		}
		provider := global.AI.Providers[name]
		if provider == nil || provider.APIKey == "" {
			return "", errors.New("AI protected-secret origin is unavailable")
		}
		return provider.APIKey, nil
	}
	return "", errors.New("AI credential must use a protected origin")
}

// Generate sends a bounded text request and returns only the response text.
// Response bodies and request credentials are never included in errors.
func (client *Client) Generate(ctx context.Context, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" || len(prompt) > maxPromptBytes {
		return "", errors.New("AI advisory prompt is empty or too large")
	}
	client.mu.Lock()
	if client.provider.MaxRequests > 0 && client.provider.RequestsUsed >= client.provider.MaxRequests {
		client.mu.Unlock()
		return "", errors.New("AI advisory request limit reached")
	}
	client.provider.RequestsUsed++
	client.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, client.provider.Timeout)
	defer cancel()
	body, endpoint, headers, err := client.request(prompt)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", errors.New("AI advisory request could not be created")
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.http.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", context.Canceled
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", context.DeadlineExceeded
		}
		return "", errors.New("AI advisory request failed")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return "", errors.New("AI advisory response is unavailable")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("AI advisory provider returned status %d", resp.StatusCode)
	}
	text, err := decodeText(client.provider.Protocol, data)
	if err != nil {
		return "", errors.New("AI advisory response was invalid")
	}
	return text, nil
}

func (client *Client) request(prompt string) ([]byte, string, map[string]string, error) {
	model := client.provider.Model
	switch client.provider.Protocol {
	case ProtocolAnthropic:
		body, err := json.Marshal(map[string]any{"model": model, "max_tokens": client.provider.MaxTokens, "messages": []map[string]string{{"role": "user", "content": prompt}}})
		return body, strings.TrimRight(client.provider.BaseURL, "/") + "/v1/messages", map[string]string{"x-api-key": client.provider.APIKey, "anthropic-version": "2023-06-01"}, err
	case ProtocolGoogle:
		body, err := json.Marshal(map[string]any{"contents": []map[string]any{{"parts": []map[string]string{{"text": prompt}}}}, "generationConfig": map[string]any{"maxOutputTokens": client.provider.MaxTokens, "temperature": 0}})
		return body, strings.TrimRight(client.provider.BaseURL, "/") + "/v1beta/models/" + url.PathEscape(model) + ":generateContent", map[string]string{"x-goog-api-key": client.provider.APIKey}, err
	default:
		requestFields := map[string]any{
			"model":    model,
			"messages": []map[string]string{{"role": "user", "content": prompt}},
		}
		if client.provider.Protocol == ProtocolOpenAI {
			// GPT-5-family OpenAI Chat Completions use the newer completion
			// token field. Keep max_tokens for generic OpenAI-compatible
			// servers such as LM Studio.
			requestFields["max_completion_tokens"] = client.provider.MaxTokens
		} else {
			requestFields["max_tokens"] = client.provider.MaxTokens
		}
		body, err := json.Marshal(requestFields)
		endpoint := strings.TrimRight(client.provider.BaseURL, "/")
		switch {
		case strings.HasSuffix(endpoint, "/chat/completions"):
		case strings.HasSuffix(endpoint, "/v1"):
			endpoint += "/chat/completions"
		default:
			endpoint += "/v1/chat/completions"
		}
		headers := map[string]string{}
		if client.provider.APIKey != "" {
			headers["Authorization"] = "Bearer " + client.provider.APIKey
		}
		return body, endpoint, headers, err
	}
}

func decodeText(protocol string, data []byte) (string, error) {
	var value struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return "", err
	}
	if protocol == ProtocolAnthropic && len(value.Content) > 0 {
		return value.Content[0].Text, nil
	}
	if protocol == ProtocolGoogle && len(value.Candidates) > 0 && len(value.Candidates[0].Content.Parts) > 0 {
		return value.Candidates[0].Content.Parts[0].Text, nil
	}
	if len(value.Choices) > 0 {
		return value.Choices[0].Message.Content, nil
	}
	return "", errors.New("response has no text")
}
