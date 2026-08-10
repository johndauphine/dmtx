package config

import (
	"fmt"
	"strings"
)

// AIConfig contains per-migration advisory controls. Credentials are never
// accepted as literals: api_key may only be an origin reference resolved by
// the application at request time.
type AIConfig struct {
	Provider       string               `yaml:"provider,omitempty"`
	Protocol       string               `yaml:"protocol,omitempty"`
	BaseURL        string               `yaml:"base_url,omitempty"`
	Model          string               `yaml:"model,omitempty"`
	APIKey         string               `yaml:"api_key,omitempty"`
	TimeoutSeconds int                  `yaml:"timeout_seconds,omitempty"`
	MaxTokens      int                  `yaml:"max_tokens,omitempty"`
	MaxRequests    int                  `yaml:"max_requests,omitempty"`
	Enabled        *bool                `yaml:"enabled,omitempty"`
	TypeMapping    *AITypeMappingConfig `yaml:"type_mapping,omitempty"`
}

// AITypeMappingConfig is retained only to provide a clear compatibility error.
// DMTX advisories never sample rows or perform AI type mapping.
type AITypeMappingConfig struct {
	Enabled *bool `yaml:"enabled,omitempty"`
}

func (c *AIConfig) Validate() error {
	if c == nil {
		return nil
	}
	if c.APIKey != "" && !isCredentialOrigin(c.APIKey) {
		return fmt.Errorf("ai.api_key must be an environment, file, or protected-secret origin")
	}
	if c.TimeoutSeconds < 0 || c.TimeoutSeconds > 600 {
		return fmt.Errorf("ai.timeout_seconds must be between 0 and 600")
	}
	if c.MaxTokens < 0 || c.MaxTokens > 100000 {
		return fmt.Errorf("ai.max_tokens must be between 0 and 100000")
	}
	if c.MaxRequests < 0 || c.MaxRequests > 1000 {
		return fmt.Errorf("ai.max_requests must be between 0 and 1000")
	}
	if c.TypeMapping != nil && c.TypeMapping.Enabled != nil && *c.TypeMapping.Enabled {
		return fmt.Errorf("ai.type_mapping is not supported by DMTX advisories")
	}
	return nil
}

func isCredentialOrigin(value string) bool {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"${env:", "${file:", "${secret:"} {
		if strings.HasPrefix(value, prefix) && strings.HasSuffix(value, "}") && len(value) > len(prefix)+1 {
			return true
		}
	}
	return false
}
