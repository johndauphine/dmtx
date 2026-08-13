package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/johndauphine/dmtx/internal/config"
	"gopkg.in/yaml.v3"
)

// SetupPrompt is one safe, serialisable question from the guided setup flow.
// The browser only renders and submits this data; setup choices, validation,
// and the eventual write remain in this package with the other application
// decisions.
type SetupPrompt struct {
	Done       bool     `json:"done"`
	Step       string   `json:"step"`
	Text       string   `json:"text"`
	Default    string   `json:"default,omitempty"`
	Choices    []string `json:"choices,omitempty"`
	Error      string   `json:"error,omitempty"`
	Masked     bool     `json:"masked,omitempty"`
	ConfigPath string   `json:"config_path,omitempty"`
}

// Setup is the first guided setup vertical slice. It deliberately starts with
// SQLite because those endpoints need no credentials: accepting a database
// password before the secure secret-origin contract is available would put a
// secret in a browser workflow with nowhere safe to persist it.
//
// It is stateful by design. A wizard has a prompt, an input, and later an
// outcome; forcing it into Request/Outcome would make an HTTP handler decide
// state transitions that belong to the application.
type Setup struct {
	mu         sync.Mutex
	step       int
	sourcePath string
	targetPath string
	targetMode string
	configPath string
	errorText  string
	cancelled  bool
}

const (
	setupSource = iota
	setupTarget
	setupTargetMode
	setupConfigPath
	setupConfirm
	setupDone
)

// NewSetup begins a guided SQLite-to-SQLite setup. configPath supplies the
// initial suggestion only; the operator confirms the final destination before
// a file is written.
func NewSetup(configPath string) *Setup {
	if strings.TrimSpace(configPath) == "" {
		configPath = defaultConfigFilename
	}
	return &Setup{configPath: configPath}
}

// newSetupFromConfig starts the SQLite wizard from an encrypted profile's
// already-parsed configuration. The profile is input only: confirmation writes
// a new owner-only YAML file and never silently mutates the encrypted profile.
func newSetupFromConfig(configPath string, cfg config.Config) (*Setup, error) {
	if cfg.Source.Type != "sqlite" || cfg.Target.Type != "sqlite" {
		return nil, newSetupStartError("saved profile is not a SQLite-to-SQLite configuration")
	}
	setup := NewSetup(configPath)
	setup.sourcePath = cfg.Source.Database
	setup.targetPath = cfg.Target.Database
	setup.targetMode = cfg.Migration.TargetMode
	return setup, nil
}

// Prompt reports the current setup question without advancing it.
func (setup *Setup) Prompt() SetupPrompt {
	setup.mu.Lock()
	defer setup.mu.Unlock()
	return setup.prompt()
}

// Input applies one answer and reports the next question. Invalid input leaves
// the workflow at the same question, so a client never has to reconstruct a
// partially completed setup conversation.
func (setup *Setup) Input(input string) SetupPrompt {
	setup.mu.Lock()
	defer setup.mu.Unlock()

	setup.errorText = ""
	if setup.step == setupDone {
		return setup.prompt()
	}
	value := strings.TrimSpace(input)
	switch setup.step {
	case setupSource:
		if value == "" {
			value = setup.sourcePath
			if value == "" {
				value = "source.db"
			}
		}
		if err := validateSQLiteSource(value); err != nil {
			setup.errorText = err.Error()
			return setup.prompt()
		}
		setup.sourcePath = value
		setup.step = setupTarget
	case setupTarget:
		if value == "" {
			value = setup.targetPath
			if value == "" {
				value = "target.db"
			}
		}
		setup.targetPath = value
		setup.step = setupTargetMode
	case setupTargetMode:
		if value == "" {
			value = setup.targetMode
			if value == "" {
				value = "drop_recreate"
			}
		}
		if value != "drop_recreate" && value != "upsert" {
			setup.errorText = "target mode must be drop_recreate or upsert"
			return setup.prompt()
		}
		setup.targetMode = value
		setup.step = setupConfigPath
	case setupConfigPath:
		if value == "" {
			value = setup.configPath
		}
		if filepath.Base(value) == "." || value == "." {
			setup.errorText = "configuration path must name a file"
			return setup.prompt()
		}
		setup.configPath = value
		setup.step = setupConfirm
	case setupConfirm:
		switch strings.ToLower(value) {
		case "", "yes", "y":
			if err := setup.persist(); err != nil {
				setup.errorText = err.Error()
				return setup.prompt()
			}
			setup.step = setupDone
		case "no", "n":
			setup.cancelled = true
			setup.step = setupDone
		default:
			setup.errorText = "answer yes or no"
		}
	}
	return setup.prompt()
}

func validateSQLiteSource(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return errors.New("source SQLite database is not readable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return errors.New("source SQLite database is not readable")
	}
	if !info.Mode().IsRegular() {
		return errors.New("source SQLite database must be a regular file")
	}
	return nil
}
func (setup *Setup) prompt() SetupPrompt {
	prompt := SetupPrompt{Error: setup.errorText, ConfigPath: setup.configPath}
	switch setup.step {
	case setupSource:
		prompt.Step, prompt.Text, prompt.Default = "source_database", "Source SQLite database path", setup.sourcePath
		if prompt.Default == "" {
			prompt.Default = "source.db"
		}
	case setupTarget:
		prompt.Step, prompt.Text, prompt.Default = "target_database", "Target SQLite database path", setup.targetPath
		if prompt.Default == "" {
			prompt.Default = "target.db"
		}
	case setupTargetMode:
		prompt.Step, prompt.Text, prompt.Default = "target_mode", "Target mode", setup.targetMode
		if prompt.Default == "" {
			prompt.Default = "drop_recreate"
		}
		prompt.Choices = []string{"drop_recreate", "upsert"}
	case setupConfigPath:
		prompt.Step, prompt.Text, prompt.Default = "config_path", "Configuration file path", setup.configPath
	case setupConfirm:
		prompt.Step, prompt.Text, prompt.Default = "confirm", "Write the validated SQLite migration configuration?", "yes"
		prompt.Choices = []string{"yes", "no"}
	case setupDone:
		prompt.Done = true
		if setup.cancelled {
			prompt.Text = "setup cancelled"
			return prompt
		}
		if setup.errorText == "" {
			prompt.Text = "setup completed; run validate, analyze, then a dry run before migration"
		}
	}
	return prompt
}

func (setup *Setup) persist() error {
	type setupMigration struct {
		TargetMode string `yaml:"target_mode"`
	}
	type setupDocument struct {
		Source    config.Endpoint `yaml:"source"`
		Target    config.Endpoint `yaml:"target"`
		Migration setupMigration  `yaml:"migration"`
	}
	data, err := yaml.Marshal(setupDocument{
		Source:    config.Endpoint{Type: "sqlite", Database: setup.sourcePath},
		Target:    config.Endpoint{Type: "sqlite", Database: setup.targetPath},
		Migration: setupMigration{TargetMode: setup.targetMode},
	})
	if err != nil {
		return errors.New("prepare configuration")
	}
	parsed, err := config.Parse(data)
	if err != nil {
		return errors.New("generated configuration is invalid")
	}
	if config.SameEndpoint(parsed.Source, parsed.Target) {
		return errors.New("source and target must be different SQLite databases")
	}
	if _, err := os.Stat(setup.configPath); err == nil {
		return errors.New("configuration already exists; choose another path")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check configuration path")
	}
	if directory := filepath.Dir(setup.configPath); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return errors.New("create configuration directory")
		}
	}
	if err := writeSetupConfigurationNew(setup.configPath, string(data)); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("configuration already exists; choose another path")
		}
		return errors.New("write configuration")
	}
	return nil
}

// writeSetupConfigurationNew creates the final guided-setup configuration
// exactly once. Unlike init, a setup flow has promised not to overwrite an
// existing configuration, so O_EXCL is required to uphold that promise across
// the check-and-write race (including a dangling symlink). The mode is set on
// both creation and the descriptor to remain owner-only under a permissive
// umask. A failed partial write is removed only when the path still names the
// object this invocation created.
func writeSetupConfigurationNew(path, contents string) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	created, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return statErr
	}
	success := false
	defer func() {
		if success {
			return
		}
		_ = file.Close()
		current, currentErr := os.Lstat(path)
		if currentErr == nil && os.SameFile(created, current) {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if _, err := file.WriteString(contents); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	success = true
	return nil
}
