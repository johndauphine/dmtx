package app

import (
	"context"
	"crypto/x509"
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

type mssqlSetupStep int

const (
	mssqlSourceHost mssqlSetupStep = iota
	mssqlSourcePort
	mssqlSourceDatabase
	mssqlSourceUser
	mssqlSourcePassword
	mssqlSourceTLSCAFile
	mssqlTargetHost
	mssqlTargetPort
	mssqlTargetDatabase
	mssqlTargetUser
	mssqlTargetPassword
	mssqlTargetTLSCAFile
	mssqlTargetMode
	mssqlConfigPath
	mssqlConfirm
	mssqlDone
)

type mssqlSetupVerifier func(context.Context, config.Endpoint) (*sql.DB, error)

const mssqlSetupVerificationTimeout = 5 * time.Second

// A CA bundle normally contains a handful of certificates. One MiB leaves
// ample room for a deliberately large trust chain while preventing guided
// setup from consuming unbounded local input.
const maxMSSQLSetupTLSCAFile int64 = 1 << 20

// MSSQLSetup creates a tested SQL Server-to-SQL Server configuration. TLS 1.2
// encryption is imposed by engine.OpenSQLServer; each optional CA path is
// retained as a connection setting but no secret from a saved profile is ever
// exposed as a default or persisted without a new explicit answer.
type MSSQLSetup struct {
	mu        sync.Mutex
	step      mssqlSetupStep
	source    config.Endpoint
	target    config.Endpoint
	mode      string
	path      string
	error     string
	cancelled bool
	verify    mssqlSetupVerifier
	timeout   time.Duration
}

// NewMSSQLSetup begins a SQL Server-to-SQL Server workflow.
func NewMSSQLSetup(configPath string) *MSSQLSetup {
	return newMSSQLSetup(configPath, engine.OpenSQLServer)
}

func newMSSQLSetup(configPath string, verify mssqlSetupVerifier) *MSSQLSetup {
	if strings.TrimSpace(configPath) == "" {
		configPath = defaultConfigFilename
	}
	return &MSSQLSetup{path: configPath, verify: verify, timeout: mssqlSetupVerificationTimeout}
}

// newMSSQLSetupFromConfig seeds only non-secret endpoint settings. Passwords
// from a decrypted profile are deliberately discarded before prompt creation.
func newMSSQLSetupFromConfig(configPath string, cfg config.Config) (*MSSQLSetup, error) {
	if cfg.Source.Type != "mssql" || cfg.Target.Type != "mssql" {
		return nil, newSetupStartError("saved profile is not a SQL Server-to-SQL Server configuration")
	}
	setup := NewMSSQLSetup(configPath)
	setup.source, setup.target = cfg.Source, cfg.Target
	setup.source.Password, setup.target.Password = "", ""
	setup.mode = cfg.Migration.TargetMode
	return setup, nil
}

func (setup *MSSQLSetup) Prompt() SetupPrompt {
	setup.mu.Lock()
	defer setup.mu.Unlock()
	return setup.prompt()
}

func (setup *MSSQLSetup) Input(input string) SetupPrompt {
	setup.mu.Lock()
	defer setup.mu.Unlock()
	if setup.step == mssqlDone {
		return setup.prompt()
	}
	setup.error = ""
	value := strings.TrimSpace(input)
	switch setup.step {
	case mssqlSourceHost:
		setup.source.Host, setup.step = mssqlRequired(value, setup.source.Host), mssqlSourcePort
	case mssqlSourcePort:
		port, ok := mssqlPort(value, setup.source.Port)
		if !ok {
			setup.error = "source SQL Server port must be a number from 1 to 65535"
			return setup.prompt()
		}
		setup.source.Port, setup.step = port, mssqlSourceDatabase
	case mssqlSourceDatabase:
		setup.source.Database = valueOr(value, setup.source.Database)
		if setup.source.Database == "" {
			setup.error = "source SQL Server database is required"
			return setup.prompt()
		}
		setup.step = mssqlSourceUser
	case mssqlSourceUser:
		setup.source.User = valueOr(value, setup.source.User)
		if setup.source.User == "" {
			setup.error = "source SQL Server username is required"
			return setup.prompt()
		}
		setup.step = mssqlSourcePassword
	case mssqlSourcePassword:
		if value == "" {
			setup.error = "source SQL Server password is required"
			return setup.prompt()
		}
		setup.source.Password, setup.step = input, mssqlSourceTLSCAFile
	case mssqlSourceTLSCAFile:
		setup.source.TLSCAFile = valueOr(value, setup.source.TLSCAFile)
		if err := validateMSSQLTLSCAFile(setup.source.TLSCAFile); err != nil {
			setup.error = "source SQL Server TLS CA certificate could not be verified"
			return setup.prompt()
		}
		if !setup.verifyEndpoint(setup.source, "source") {
			return setup.prompt()
		}
		setup.step = mssqlTargetHost
	case mssqlTargetHost:
		setup.target.Host, setup.step = mssqlRequired(value, setup.target.Host), mssqlTargetPort
	case mssqlTargetPort:
		port, ok := mssqlPort(value, setup.target.Port)
		if !ok {
			setup.error = "target SQL Server port must be a number from 1 to 65535"
			return setup.prompt()
		}
		setup.target.Port, setup.step = port, mssqlTargetDatabase
	case mssqlTargetDatabase:
		setup.target.Database = valueOr(value, setup.target.Database)
		if setup.target.Database == "" {
			setup.error = "target SQL Server database is required"
			return setup.prompt()
		}
		setup.step = mssqlTargetUser
	case mssqlTargetUser:
		setup.target.User = valueOr(value, setup.target.User)
		if setup.target.User == "" {
			setup.error = "target SQL Server username is required"
			return setup.prompt()
		}
		setup.step = mssqlTargetPassword
	case mssqlTargetPassword:
		if value == "" {
			setup.error = "target SQL Server password is required"
			return setup.prompt()
		}
		setup.target.Password, setup.step = input, mssqlTargetTLSCAFile
	case mssqlTargetTLSCAFile:
		setup.target.TLSCAFile = valueOr(value, setup.target.TLSCAFile)
		if err := validateMSSQLTLSCAFile(setup.target.TLSCAFile); err != nil {
			setup.error = "target SQL Server TLS CA certificate could not be verified"
			return setup.prompt()
		}
		if !setup.verifyEndpoint(setup.target, "target") {
			return setup.prompt()
		}
		setup.step = mssqlTargetMode
	case mssqlTargetMode:
		if value == "" {
			value = valueOr(setup.mode, "drop_recreate")
		}
		if value != "drop_recreate" && value != "upsert" {
			setup.error = "target mode must be drop_recreate or upsert"
			return setup.prompt()
		}
		setup.mode, setup.step = value, mssqlConfigPath
	case mssqlConfigPath:
		if value == "" {
			value = setup.path
		}
		if value == "." || filepath.Base(value) == "." {
			setup.error = "configuration path must name a file"
			return setup.prompt()
		}
		setup.path, setup.step = value, mssqlConfirm
	case mssqlConfirm:
		switch strings.ToLower(value) {
		case "", "yes", "y":
			if err := setup.persist(); err != nil {
				setup.error = err.Error()
				return setup.prompt()
			}
			setup.step = mssqlDone
		case "no", "n":
			setup.cancelled, setup.step = true, mssqlDone
		default:
			setup.error = "answer yes or no"
		}
	}
	return setup.prompt()
}

func mssqlRequired(value, fallback string) string {
	if value != "" {
		return value
	}
	if fallback != "" {
		return fallback
	}
	return "localhost"
}

func valueOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func mssqlPort(value string, fallback int) (int, bool) {
	if value == "" {
		if fallback > 0 {
			return fallback, true
		}
		return 1433, true
	}
	port, err := strconv.Atoi(value)
	return port, err == nil && port > 0 && port <= 65535
}

// validateMSSQLTLSCAFile mirrors the certificate file formats accepted by the
// SQL Server driver. It deliberately returns only an opaque error so neither
// path nor parser details can escape through the guided-setup API.
func validateMSSQLTLSCAFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	contents, _, err := readBoundedRegularFile(path, maxMSSQLSetupTLSCAFile, false)
	if err != nil {
		return errors.New("TLS CA file is unsafe")
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pem":
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(contents) {
			return errors.New("TLS CA file contains no certificates")
		}
	case ".der":
		if _, err := x509.ParseCertificate(contents); err != nil {
			return errors.New("TLS CA file is invalid")
		}
	default:
		return errors.New("TLS CA file format is unsupported")
	}
	return nil
}

func (setup *MSSQLSetup) verifyEndpoint(endpoint config.Endpoint, side string) bool {
	endpoint.Type = "mssql"
	verificationContext, cancel := context.WithTimeout(context.Background(), setup.timeout)
	defer cancel()
	database, err := setup.verify(verificationContext, endpoint)
	if err != nil {
		setup.error = side + " SQL Server connection could not be verified"
		if side == "source" {
			setup.step = mssqlSourcePassword
		} else {
			setup.step = mssqlTargetPassword
		}
		return false
	}
	if database != nil {
		_ = database.Close()
	}
	return true
}

func (setup *MSSQLSetup) prompt() SetupPrompt {
	prompt := SetupPrompt{Error: setup.error, ConfigPath: setup.path}
	switch setup.step {
	case mssqlSourceHost:
		prompt.Step, prompt.Text, prompt.Default = "source_host", "Source SQL Server host", mssqlRequired("", setup.source.Host)
	case mssqlSourcePort:
		prompt.Step, prompt.Text, prompt.Default = "source_port", "Source SQL Server port", mssqlPortDefault(setup.source.Port)
	case mssqlSourceDatabase:
		prompt.Step, prompt.Text, prompt.Default = "source_database", "Source SQL Server database", setup.source.Database
	case mssqlSourceUser:
		prompt.Step, prompt.Text, prompt.Default = "source_user", "Source SQL Server username", setup.source.User
	case mssqlSourcePassword:
		prompt.Step, prompt.Text, prompt.Masked = "source_password", "Source SQL Server password", true
	case mssqlSourceTLSCAFile:
		prompt.Step, prompt.Text, prompt.Default = "source_tls_ca_file", "Source SQL Server TLS CA certificate path (optional)", setup.source.TLSCAFile
	case mssqlTargetHost:
		prompt.Step, prompt.Text, prompt.Default = "target_host", "Target SQL Server host", mssqlRequired("", setup.target.Host)
	case mssqlTargetPort:
		prompt.Step, prompt.Text, prompt.Default = "target_port", "Target SQL Server port", mssqlPortDefault(setup.target.Port)
	case mssqlTargetDatabase:
		prompt.Step, prompt.Text, prompt.Default = "target_database", "Target SQL Server database", setup.target.Database
	case mssqlTargetUser:
		prompt.Step, prompt.Text, prompt.Default = "target_user", "Target SQL Server username", setup.target.User
	case mssqlTargetPassword:
		prompt.Step, prompt.Text, prompt.Masked = "target_password", "Target SQL Server password", true
	case mssqlTargetTLSCAFile:
		prompt.Step, prompt.Text, prompt.Default = "target_tls_ca_file", "Target SQL Server TLS CA certificate path (optional)", setup.target.TLSCAFile
	case mssqlTargetMode:
		prompt.Step, prompt.Text, prompt.Default, prompt.Choices = "target_mode", "Target mode", valueOr(setup.mode, "drop_recreate"), []string{"drop_recreate", "upsert"}
	case mssqlConfigPath:
		prompt.Step, prompt.Text, prompt.Default = "config_path", "Configuration file path", setup.path
	case mssqlConfirm:
		prompt.Step, prompt.Text, prompt.Default, prompt.Choices = "confirm", "Write the tested SQL Server migration configuration?", "yes", []string{"yes", "no"}
	case mssqlDone:
		prompt.Done = true
		if setup.cancelled {
			prompt.Text = "setup cancelled"
		} else if setup.error == "" {
			prompt.Text = "setup completed; run preflight, analyze, then a dry run before migration"
		}
	}
	return prompt
}

func mssqlPortDefault(port int) string {
	if port == 0 {
		port = 1433
	}
	return strconv.Itoa(port)
}

func (setup *MSSQLSetup) persist() error {
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
	document.Source, document.Target = setup.source, setup.target
	document.Source.Type, document.Target.Type = "mssql", "mssql"
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
	if err := writeSetupConfigurationNew(absolute, string(data)); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("configuration already exists; choose another path")
		}
		return errors.New("write configuration")
	}
	cleanup = false
	return nil
}

var _ SetupFlow = (*MSSQLSetup)(nil)
