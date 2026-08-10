package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// EnsureMasterKey returns the existing profile-sealing key or creates one in
// the protected secrets file without reserialising unrelated configuration.
func EnsureMasterKey(path string) ([]byte, error) {
	if err := ValidatePermissions(path); err != nil {
		return nil, fmt.Errorf("profile secrets unavailable: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile secrets: %w", err)
	}
	var parsed Config
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, errors.New("parse profile secrets")
	}
	if encoded := parsed.Encryption.MasterKey; encoded != "" {
		key, err := base64.RawStdEncoding.DecodeString(encoded)
		if err != nil || len(key) != 32 {
			return nil, errors.New("invalid profile master key")
		}
		return key, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, errors.New("generate profile master key")
	}
	updated, err := withMasterKey(data, base64.RawStdEncoding.EncodeToString(key))
	if err != nil {
		return nil, fmt.Errorf("update profile secrets: %w", err)
	}
	if err := atomicWrite0600(path, updated); err != nil {
		return nil, fmt.Errorf("persist profile secrets: %w", err)
	}
	return key, nil
}
