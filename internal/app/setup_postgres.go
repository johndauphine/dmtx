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
	"time"

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

// NewSetupForProfile loads a sealed profile as setup input. Confirmation always
// creates a separate YAML configuration; the encrypted profile is never
// changed implicitly. PostgreSQL passwords are intentionally not carried into
// the setup prompts or output secret files.
func NewSetupForProfile(profileName, configPath, engineName string) (SetupFlow, error) {
	if strings.TrimSpace(profileName) == "" {
		return nil, errors.New("profile name is required")
	}
	data, _, err := configurationData(Request{ProfileName: profileName})
	if err != nil {
		return nil, err
	}
	return newSetupForProfileData(profileName, configPath, engineName, data)
}

// newSetupForProfileData keeps profile decryption at the boundary while making
// the profile-seeded setup policy independently testable without writing to a
// caller's real protected profile directory.
func newSetupForProfileData(profileName, configPath, engineName string, data []byte) (SetupFlow, error) {
	if strings.TrimSpace(profileName) == "" {
		return nil, errors.New("profile name is required")
	}
	cfg, err := config.Parse(data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = profileName + ".yaml"
	}
	engine := strings.ToLower(strings.TrimSpace(engineName))
	if engine == "" {
		engine = cfg.Source.Type
	}
	switch engine {
	case "sqlite":
		return newSetupFromConfig(configPath, cfg)
	case "postgres", "postgresql":
		return newPostgresSetupFromConfig(configPath, cfg)
	default:
		return nil, errors.New("profile uses an unsupported setup engine")
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

const postgresSetupVerificationTimeout = 5 * time.Second

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
	timeout   time.Duration
}

// NewPostgresSetup begins a PostgreSQL-to-PostgreSQL workflow.
func NewPostgresSetup(configPath string) *PostgresSetup {
	return newPostgresSetup(configPath, engine.OpenPostgres)
}

func newPostgresSetup(configPath string, verify postgresSetupVerifier) *PostgresSetup {
	if strings.TrimSpace(configPath) == "" {
		configPath = defaultConfigFilename
	}
	return &PostgresSetup{path: configPath, verify: verify, timeout: postgresSetupVerificationTimeout}
}

// newPostgresSetupFromConfig seeds only non-secret endpoint settings. The
// password fields are cleared before the flow begins, so decrypting a profile
// can never turn into a browser-visible password default.
func newPostgresSetupFromConfig(configPath string, cfg config.Config) (*PostgresSetup, error) {
	if cfg.Source.Type != "postgres" || cfg.Target.Type != "postgres" {
		return nil, errors.New("profile is not a PostgreSQL-to-PostgreSQL configuration")
	}
	setup := NewPostgresSetup(configPath)
	setup.source, setup.target = cfg.Source, cfg.Target
	setup.source.Password, setup.target.Password = "", ""
	setup.mode = cfg.Migration.TargetMode
	return setup, nil
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
		fallback := setup.source.Host
		if fallback == "" {
			fallback = "localhost"
		}
		setup.source.Host, setup.step = setupRequired(value, fallback, postgresSourceHost), postgresSourcePort
	case postgresSourcePort:
		if value == "" && setup.source.Port > 0 {
			value = strconv.Itoa(setup.source.Port)
		}
		port, ok := setupPort(value)
		if !ok {
			setup.error = "source PostgreSQL port must be a number from 1 to 65535"
			return setup.prompt()
		}
		setup.source.Port, setup.step = port, postgresSourceDatabase
	case postgresSourceDatabase:
		if value == "" {
			value = setup.source.Database
		}
		if value == "" {
			setup.error = "source PostgreSQL database is required"
			return setup.prompt()
		}
		setup.source.Database, setup.step = value, postgresSourceUser
	case postgresSourceUser:
		if value == "" {
			value = setup.source.User
		}
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
		fallback := setup.target.Host
		if fallback == "" {
			fallback = "localhost"
		}
		setup.target.Host, setup.step = setupRequired(value, fallback, postgresTargetHost), postgresTargetPort
	case postgresTargetPort:
		if value == "" && setup.target.Port > 0 {
			value = strconv.Itoa(setup.target.Port)
		}
		port, ok := setupPort(value)
		if !ok {
			setup.error = "target PostgreSQL port must be a number from 1 to 65535"
			return setup.prompt()
		}
		setup.target.Port, setup.step = port, postgresTargetDatabase
	case postgresTargetDatabase:
		if value == "" {
			value = setup.target.Database
		}
		if value == "" {
			setup.error = "target PostgreSQL database is required"
			return setup.prompt()
		}
		setup.target.Database, setup.step = value, postgresTargetUser
	case postgresTargetUser:
		if value == "" {
			value = setup.target.User
		}
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
			value = setup.mode
			if value == "" {
				value = "drop_recreate"
			}
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
	verificationContext, cancel := context.WithTimeout(context.Background(), setup.timeout)
	defer cancel()
	database, err := setup.verify(verificationContext, endpoint)
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
		prompt.Step, prompt.Text, prompt.Default = "source_host", "Source PostgreSQL host", setup.source.Host
		if prompt.Default == "" {
			prompt.Default = "localhost"
		}
	case postgresSourcePort:
		prompt.Step, prompt.Text, prompt.Default = "source_port", "Source PostgreSQL port", strconv.Itoa(setup.source.Port)
		if setup.source.Port == 0 {
			prompt.Default = "5432"
		}
	case postgresSourceDatabase:
		prompt.Step, prompt.Text, prompt.Default = "source_database", "Source PostgreSQL database", setup.source.Database
	case postgresSourceUser:
		prompt.Step, prompt.Text, prompt.Default = "source_user", "Source PostgreSQL username", setup.source.User
	case postgresSourcePassword:
		prompt.Step, prompt.Text, prompt.Masked = "source_password", "Source PostgreSQL password", true
	case postgresTargetHost:
		prompt.Step, prompt.Text, prompt.Default = "target_host", "Target PostgreSQL host", setup.target.Host
		if prompt.Default == "" {
			prompt.Default = "localhost"
		}
	case postgresTargetPort:
		prompt.Step, prompt.Text, prompt.Default = "target_port", "Target PostgreSQL port", strconv.Itoa(setup.target.Port)
		if setup.target.Port == 0 {
			prompt.Default = "5432"
		}
	case postgresTargetDatabase:
		prompt.Step, prompt.Text, prompt.Default = "target_database", "Target PostgreSQL database", setup.target.Database
	case postgresTargetUser:
		prompt.Step, prompt.Text, prompt.Default = "target_user", "Target PostgreSQL username", setup.target.User
	case postgresTargetPassword:
		prompt.Step, prompt.Text, prompt.Masked = "target_password", "Target PostgreSQL password", true
	case postgresTargetMode:
		prompt.Step, prompt.Text, prompt.Default, prompt.Choices = "target_mode", "Target mode", setup.mode, []string{"drop_recreate", "upsert"}
		if prompt.Default == "" {
			prompt.Default = "drop_recreate"
		}
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
