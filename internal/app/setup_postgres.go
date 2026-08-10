package app

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/engine"
	"gopkg.in/yaml.v3"
)

// SetupFlow is the application-owned conversation driven by the setup API.
// Its deliberately small surface keeps the browser from choosing validation,
// connection, secret, or persistence behavior.
type SetupFlow interface {
	Prompt() SetupPrompt
	Input(string) SetupPrompt
}

// NewSetupForEngine selects a supported setup workflow in the application.
func NewSetupForEngine(configPath, engineName string) (SetupFlow, error) {
	switch strings.ToLower(strings.TrimSpace(engineName)) {
	case "", "sqlite":
		return NewSetup(configPath), nil
	case "postgres", "postgresql":
		return NewPostgresSetup(configPath), nil
	default:
		return nil, errors.New("unsupported setup engine")
	}
}

type postgresSetupStep int

const (
	postgresSourceHost postgresSetupStep = iota
	postgresSourcePort
	postgresSourceDatabase
	postgresSourceUser
	postgresSourcePassword
	postgresTargetHost
	postgresTargetPort
	postgresTargetDatabase
	postgresTargetUser
	postgresTargetPassword
	postgresTargetMode
	postgresConfigPath
	postgresConfirm
	postgresDone
)

type postgresSetupVerifier func(context.Context, config.Endpoint) (*sql.DB, error)

// PostgresSetup is the credentials-aware guided workflow. Passwords exist only
// in process memory until both endpoints have been tested and the operator
// confirms the write. The generated configuration contains file origins, never
// passwords, and the companion files are owner-only.
type PostgresSetup struct {
	mu        sync.Mutex
	step      postgresSetupStep
	source    config.Endpoint
	target    config.Endpoint
	mode      string
	path      string
	error     string
	cancelled bool
	verify    postgresSetupVerifier
}

// NewPostgresSetup begins a PostgreSQL-to-PostgreSQL workflow.
func NewPostgresSetup(configPath string) *PostgresSetup {
	return newPostgresSetup(configPath, engine.OpenPostgres)
}

func newPostgresSetup(configPath string, verify postgresSetupVerifier) *PostgresSetup {
	if strings.TrimSpace(configPath) == "" {
		configPath = defaultConfigFilename
	}
	return &PostgresSetup{path: configPath, verify: verify}
}

func (setup *PostgresSetup) Prompt() SetupPrompt {
	setup.mu.Lock()
	defer setup.mu.Unlock()
	return setup.prompt()
}

func (setup *PostgresSetup) Input(input string) SetupPrompt {
	setup.mu.Lock()
	defer setup.mu.Unlock()
	if setup.step == postgresDone {
		return setup.prompt()
	}
	setup.error = ""
	value := strings.TrimSpace(input)
	switch setup.step {
	case postgresSourceHost:
		setup.source.Host, setup.step = setupRequired(value, "localhost", postgresSourceHost), postgresSourcePort
	case postgresSourcePort:
		port, ok := setupPort(value)
		if !ok {
			setup.error = "source PostgreSQL port must be a number from 1 to 65535"
			return setup.prompt()
		}
		setup.source.Port, setup.step = port, postgresSourceDatabase
	case postgresSourceDatabase:
		if value == "" {
			setup.error = "source PostgreSQL database is required"
			return setup.prompt()
		}
		setup.source.Database, setup.step = value, postgresSourceUser
	case postgresSourceUser:
		if value == "" {
			setup.error = "source PostgreSQL username is required"
			return setup.prompt()
		}
		setup.source.User, setup.step = value, postgresSourcePassword
	case postgresSourcePassword:
		if value == "" {
			setup.error = "source PostgreSQL password is required"
			return setup.prompt()
		}
		setup.source.Password = input
		if !setup.verifyEndpoint(setup.source, "source") {
			return setup.prompt()
		}
		setup.step = postgresTargetHost
	case postgresTargetHost:
		setup.target.Host, setup.step = setupRequired(value, "localhost", postgresTargetHost), postgresTargetPort
	case postgresTargetPort:
		port, ok := setupPort(value)
		if !ok {
			setup.error = "target PostgreSQL port must be a number from 1 to 65535"
			return setup.prompt()
		}
		setup.target.Port, setup.step = port, postgresTargetDatabase
	case postgresTargetDatabase:
		if value == "" {
			setup.error = "target PostgreSQL database is required"
			return setup.prompt()
		}
		setup.target.Database, setup.step = value, postgresTargetUser
	case postgresTargetUser:
		if value == "" {
			setup.error = "target PostgreSQL username is required"
			return setup.prompt()
		}
		setup.target.User, setup.step = value, postgresTargetPassword
	case postgresTargetPassword:
		if value == "" {
			setup.error = "target PostgreSQL password is required"
			return setup.prompt()
		}
		setup.target.Password = input
		if !setup.verifyEndpoint(setup.target, "target") {
			return setup.prompt()
		}
		setup.step = postgresTargetMode
	case postgresTargetMode:
		if value == "" {
			value = "drop_recreate"
		}
		if value != "drop_recreate" && value != "upsert" {
			setup.error = "target mode must be drop_recreate or upsert"
			return setup.prompt()
		}
		setup.mode, setup.step = value, postgresConfigPath
	case postgresConfigPath:
		if value == "" {
			value = setup.path
		}
		if value == "." || filepath.Base(value) == "." {
			setup.error = "configuration path must name a file"
			return setup.prompt()
		}
		setup.path, setup.step = value, postgresConfirm
	case postgresConfirm:
		switch strings.ToLower(value) {
		case "", "yes", "y":
			if err := setup.persist(); err != nil {
				setup.error = err.Error()
				return setup.prompt()
			}
			setup.step = postgresDone
		case "no", "n":
			setup.cancelled, setup.step = true, postgresDone
		default:
			setup.error = "answer yes or no"
		}
	}
	return setup.prompt()
}

func setupRequired(value, fallback string, current postgresSetupStep) string {
	if value == "" {
		return fallback
	}
	return value
}

func setupPort(value string) (int, bool) {
	if value == "" {
		return 5432, true
	}
	port, err := strconv.Atoi(value)
	return port, err == nil && port > 0 && port <= 65535
}

func (setup *PostgresSetup) verifyEndpoint(endpoint config.Endpoint, side string) bool {
	endpoint.Type = "postgres"
	endpoint.SSLMode = "require"
	database, err := setup.verify(context.Background(), endpoint)
	if err != nil {
		setup.error = side + " PostgreSQL connection could not be verified"
		return false
	}
	if database != nil {
		_ = database.Close()
	}
	return true
}

func (setup *PostgresSetup) prompt() SetupPrompt {
	prompt := SetupPrompt{Error: setup.error, ConfigPath: setup.path}
	switch setup.step {
	case postgresSourceHost:
		prompt.Step, prompt.Text, prompt.Default = "source_host", "Source PostgreSQL host", "localhost"
	case postgresSourcePort:
		prompt.Step, prompt.Text, prompt.Default = "source_port", "Source PostgreSQL port", "5432"
	case postgresSourceDatabase:
		prompt.Step, prompt.Text = "source_database", "Source PostgreSQL database"
	case postgresSourceUser:
		prompt.Step, prompt.Text = "source_user", "Source PostgreSQL username"
	case postgresSourcePassword:
		prompt.Step, prompt.Text, prompt.Masked = "source_password", "Source PostgreSQL password", true
	case postgresTargetHost:
		prompt.Step, prompt.Text, prompt.Default = "target_host", "Target PostgreSQL host", "localhost"
	case postgresTargetPort:
		prompt.Step, prompt.Text, prompt.Default = "target_port", "Target PostgreSQL port", "5432"
	case postgresTargetDatabase:
		prompt.Step, prompt.Text = "target_database", "Target PostgreSQL database"
	case postgresTargetUser:
		prompt.Step, prompt.Text = "target_user", "Target PostgreSQL username"
	case postgresTargetPassword:
		prompt.Step, prompt.Text, prompt.Masked = "target_password", "Target PostgreSQL password", true
	case postgresTargetMode:
		prompt.Step, prompt.Text, prompt.Default, prompt.Choices = "target_mode", "Target mode", "drop_recreate", []string{"drop_recreate", "upsert"}
	case postgresConfigPath:
		prompt.Step, prompt.Text, prompt.Default = "config_path", "Configuration file path", setup.path
	case postgresConfirm:
		prompt.Step, prompt.Text, prompt.Default, prompt.Choices = "confirm", "Write the tested PostgreSQL migration configuration?", "yes", []string{"yes", "no"}
	case postgresDone:
		prompt.Done = true
		if setup.cancelled {
			prompt.Text = "setup cancelled"
		} else if setup.error == "" {
			prompt.Text = "setup completed; run preflight, analyze, then a dry run before migration"
		}
	}
	return prompt
}

func (setup *PostgresSetup) persist() error {
	absolute, err := filepath.Abs(setup.path)
	if err != nil {
		return errors.New("prepare configuration path")
	}
	if _, err := os.Stat(absolute); err == nil {
		return errors.New("configuration already exists; choose another path")
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("check configuration path")
	}
	secretsDirectory := absolute + ".secrets"
	if _, err := os.Stat(secretsDirectory); err == nil {
		return errors.New("setup secret storage already exists; choose another configuration path")
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("check setup secret storage")
	}
	if err := os.MkdirAll(secretsDirectory, 0o700); err != nil {
		return errors.New("create protected setup secret storage")
	}
	if err := os.Chmod(secretsDirectory, 0o700); err != nil {
		_ = os.RemoveAll(secretsDirectory)
		return errors.New("protect setup secret storage")
	}
	sourceSecret := filepath.Join(secretsDirectory, "source.password")
	targetSecret := filepath.Join(secretsDirectory, "target.password")
	if err := writeSetupSecret(sourceSecret, setup.source.Password); err != nil {
		_ = os.RemoveAll(secretsDirectory)
		return errors.New("write protected source password")
	}
	if err := writeSetupSecret(targetSecret, setup.target.Password); err != nil {
		_ = os.RemoveAll(secretsDirectory)
		return errors.New("write protected target password")
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(secretsDirectory)
		}
	}()
	document := struct {
		Source    config.Endpoint `yaml:"source"`
		Target    config.Endpoint `yaml:"target"`
		Migration struct {
			TargetMode string `yaml:"target_mode"`
		} `yaml:"migration"`
	}{}
	document.Source = setup.source
	document.Target = setup.target
	document.Source.Type, document.Target.Type = "postgres", "postgres"
	document.Source.SSLMode, document.Target.SSLMode = "require", "require"
	document.Source.Password = "${file:" + sourceSecret + "}"
	document.Target.Password = "${file:" + targetSecret + "}"
	document.Migration.TargetMode = setup.mode
	data, err := yaml.Marshal(document)
	if err != nil {
		return errors.New("prepare configuration")
	}
	parsed, err := config.Parse(data)
	if err != nil || config.SameEndpoint(parsed.Source, parsed.Target) {
		return errors.New("generated configuration is invalid")
	}
	if directory := filepath.Dir(absolute); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return errors.New("create configuration directory")
		}
	}
	if err := writeRestricted(absolute, string(data)); err != nil {
		return errors.New("write configuration")
	}
	cleanup = false
	return nil
}

func writeSetupSecret(path, value string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.WriteString(value + "\n"); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

var _ SetupFlow = (*PostgresSetup)(nil)
