package secrets

// AIConfig contains protected global provider settings. The file is owner-only
// (0600); migration YAML must use origins rather than embedding these values.
type AIConfig struct {
	DefaultProvider string               `yaml:"default_provider,omitempty"`
	Providers       map[string]*Provider `yaml:"providers,omitempty"`
}

// Provider describes one native or OpenAI-compatible model endpoint.
type Provider struct {
	Protocol       string `yaml:"protocol,omitempty"`
	BaseURL        string `yaml:"base_url,omitempty"`
	APIKey         string `yaml:"api_key,omitempty"`
	Model          string `yaml:"model,omitempty"`
	ContextWindow  int    `yaml:"context_window,omitempty"`
	MaxTokens      int    `yaml:"max_tokens,omitempty"`
	MaxRequests    int    `yaml:"max_requests,omitempty"`
	TimeoutSeconds int    `yaml:"timeout_seconds,omitempty"`
}
