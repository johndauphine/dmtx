package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// commandArgumentSpec describes the syntax a command accepts. It is the one
// normalizer used by the CLI and the browser parser, so positional config
// files, @config files, --flag VALUE, and --flag=VALUE cannot drift between
// commands or surfaces.
type commandArgumentSpec struct {
	command     string
	bools       map[string]string
	values      map[string]string
	unsupported map[string]struct{}
}

type parsedCommandArguments struct {
	positionals []string
	bools       map[string]bool
	values      map[string]string
	present     map[string]bool
}

// parseCommandArguments follows DMT's slash-command grammar. A leading @ is
// syntax only for positional paths; flag values are already unambiguous and
// are retained byte-for-byte.
func parseCommandArguments(
	spec commandArgumentSpec,
	args []string,
) (parsedCommandArguments, error) {
	parsed := parsedCommandArguments{
		bools:   map[string]bool{},
		values:  map[string]string{},
		present: map[string]bool{},
	}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if !strings.HasPrefix(argument, "-") {
			parsed.positionals = append(
				parsed.positionals,
				strings.TrimPrefix(argument, "@"),
			)
			continue
		}

		name, inlineValue, hasInlineValue := strings.Cut(argument, "=")
		if _, knownButUnsupported := spec.unsupported[name]; knownButUnsupported {
			return parsed, fmt.Errorf(
				"/%s: flag %s exists in DMT but its capability is not supported by DMTX",
				spec.command,
				name,
			)
		}
		if canonical, ok := spec.bools[name]; ok {
			if hasInlineValue {
				return parsed, fmt.Errorf(
					"/%s: flag %s does not take a value (see /help)",
					spec.command,
					name,
				)
			}
			if parsed.present[canonical] {
				return parsed, fmt.Errorf(
					"/%s: flag %s was provided more than once (see /help)",
					spec.command,
					name,
				)
			}
			parsed.present[canonical] = true
			parsed.bools[canonical] = true
			continue
		}
		if canonical, ok := spec.values[name]; ok {
			if parsed.present[canonical] {
				return parsed, fmt.Errorf(
					"/%s: flag %s was provided more than once (see /help)",
					spec.command,
					name,
				)
			}
			value := inlineValue
			if !hasInlineValue {
				if index+1 >= len(args) {
					return parsed, fmt.Errorf(
						"/%s: flag %s requires a value (see /help)",
						spec.command,
						name,
					)
				}
				index++
				value = args[index]
			}
			if value == "" {
				return parsed, fmt.Errorf(
					"/%s: flag %s requires a value (see /help)",
					spec.command,
					name,
				)
			}
			parsed.present[canonical] = true
			parsed.values[canonical] = value
			continue
		}
		return parsed, fmt.Errorf(
			"/%s: unknown flag %s (see /help)",
			spec.command,
			name,
		)
	}
	return parsed, nil
}

func unsupportedArguments(names ...string) map[string]struct{} {
	unsupported := make(map[string]struct{}, len(names))
	for _, name := range names {
		unsupported[name] = struct{}{}
	}
	return unsupported
}

func originArgumentValues() map[string]string {
	return map[string]string{
		"--config":  "config",
		"--profile": "profile",
	}
}

func resolveConfigOrigin(
	command string,
	parsed parsedCommandArguments,
) (string, string, error) {
	if len(parsed.positionals) > 1 {
		return "", "", fmt.Errorf(
			"/%s: expected at most one config file (see /help)",
			command,
		)
	}
	configPath := parsed.values["config"]
	if len(parsed.positionals) == 1 {
		if parsed.positionals[0] == "" {
			return "", "", fmt.Errorf(
				"/%s: config file cannot be empty (see /help)",
				command,
			)
		}
		if configPath != "" {
			return "", "", fmt.Errorf(
				"/%s: choose a positional config file or --config, not both",
				command,
			)
		}
		configPath = parsed.positionals[0]
	}
	profileName := parsed.values["profile"]
	if configPath != "" && profileName != "" {
		return "", "", fmt.Errorf(
			"/%s: choose a config file or --profile, not both",
			command,
		)
	}
	return configPath, profileName, nil
}

func parseRunArguments(args []string) (runOptions, error) {
	values := originArgumentValues()
	values["--state"] = "state"
	values["--source-schema"] = "source-schema"
	values["--target-schema"] = "target-schema"
	values["--workers"] = "workers"
	values["--skip-preflight"] = "skip-preflight"
	parsed, err := parseCommandArguments(commandArgumentSpec{
		command: "run",
		values:  values,
		bools: map[string]string{
			"--dry-run":                 "dry-run",
			"--acknowledge-destructive": "acknowledge-destructive",
			// DMT calls this acknowledgement --confirm-backup. It is an
			// alias for DMTX's stronger destructive-action acknowledgement.
			"--confirm-backup": "acknowledge-destructive",
		},
		unsupported: unsupportedArguments("--ai-schema-advisor"),
	}, args)
	if err != nil {
		return runOptions{}, err
	}
	configPath, profileName, err := resolveConfigOrigin("run", parsed)
	if err != nil {
		return runOptions{}, err
	}
	options := runOptions{
		configPath:              configPath,
		profileName:             profileName,
		statePath:               parsed.values["state"],
		dryRun:                  parsed.bools["dry-run"],
		destructiveAcknowledged: parsed.bools["acknowledge-destructive"],
	}
	if value := parsed.values["workers"]; value != "" {
		workers, err := positiveWorkers("run", value)
		if err != nil {
			return runOptions{}, err
		}
		options.workers = workers
	}
	options.sourceSchema = parsed.values["source-schema"]
	options.targetSchema = parsed.values["target-schema"]
	options.skipPreflight = parsed.values["skip-preflight"]
	if options.statePath == "" && options.configPath != "" {
		options.statePath = options.configPath + ".state.db"
	}
	// A WebUI supplies its project-scoped state default after parsing. Keep a
	// profile-only request intact so the same DMT syntax works there; the
	// executor still refuses an unresolved state path at its seam.
	if options.profileName != "" && options.statePath == "" {
		return options, nil
	}
	// A surface with session defaults fills the origin after parsing. Keep an
	// entirely unresolved request intact so it can do that; Execute supplies
	// DMT's historical config.yaml fallback only if it remains unresolved.
	if options.configPath == "" && options.profileName == "" {
		return options, nil
	}
	if normalized, ok := validRunOptions(options); ok {
		return normalized, nil
	}
	return runOptions{}, fmt.Errorf("/run: invalid config, profile, or state combination")
}

func parseResumeArguments(args []string) (resumeOptions, error) {
	values := originArgumentValues()
	values["--state"] = "state"
	values["--abandon-reason"] = "abandon-reason"
	values["--skip-preflight"] = "skip-preflight"
	parsed, err := parseCommandArguments(commandArgumentSpec{
		command: "resume",
		values:  values,
		bools: map[string]string{
			"--acknowledge-destructive": "acknowledge-destructive",
			"--force-resume":            "force-resume",
			"--abandon":                 "abandon",
		},
	}, args)
	if err != nil {
		return resumeOptions{}, err
	}
	configPath, profileName, err := resolveConfigOrigin("resume", parsed)
	if err != nil {
		return resumeOptions{}, err
	}
	options := resumeOptions{
		configPath:              configPath,
		profileName:             profileName,
		statePath:               parsed.values["state"],
		destructiveAcknowledged: parsed.bools["acknowledge-destructive"],
		forceResume:             parsed.bools["force-resume"],
		abandon:                 parsed.bools["abandon"],
		abandonReason:           parsed.values["abandon-reason"],
		skipPreflight:           parsed.values["skip-preflight"],
	}
	if options.abandon != (options.abandonReason != "") ||
		options.abandon && (options.forceResume || options.destructiveAcknowledged) {
		return resumeOptions{}, fmt.Errorf("/resume: --abandon requires only --abandon-reason TEXT")
	}
	if options.statePath == "" && options.configPath != "" {
		options.statePath = options.configPath + ".state.db"
	}
	if options.profileName != "" && options.statePath == "" {
		return options, nil
	}
	if options.configPath == "" && options.profileName == "" {
		return options, nil
	}
	if normalized, ok := validResumeOptions(options); ok {
		return normalized, nil
	}
	return resumeOptions{}, fmt.Errorf("/resume: invalid config, profile, or state combination")
}

func parseConfigOriginArguments(command string, args []string) (Request, error) {
	var unsupported map[string]struct{}
	values := originArgumentValues()
	switch command {
	case "validate":
		unsupported = unsupportedArguments("--ai-triage", "--timeout")
	case "preflight":
		values["--skip-preflight"] = "skip-preflight"
		unsupported = unsupportedArguments("--ai-review")
	case "analyze":
		unsupported = unsupportedArguments("-a", "--apply", "--ai-explain")
	}
	parsed, err := parseCommandArguments(commandArgumentSpec{
		command:     command,
		values:      values,
		unsupported: unsupported,
	}, args)
	if err != nil {
		return Request{}, err
	}
	configPath, profileName, err := resolveConfigOrigin(command, parsed)
	if err != nil {
		return Request{}, err
	}
	return Request{
		Command:       command,
		ConfigPath:    configPath,
		ProfileName:   profileName,
		SkipPreflight: parsed.values["skip-preflight"],
	}, nil
}

func parseStateArguments(command string, args []string) (Request, error) {
	values := originArgumentValues()
	values["--state"] = "state"
	bools := map[string]string{}
	if command == "history" {
		values["--run"] = "run"
	} else {
		bools["-d"] = "detailed"
		bools["--detailed"] = "detailed"
	}
	parsed, err := parseCommandArguments(commandArgumentSpec{
		command: command,
		values:  values,
		bools:   bools,
	}, args)
	if err != nil {
		return Request{}, err
	}
	configPath, profileName, err := resolveConfigOrigin(command, parsed)
	if err != nil {
		return Request{}, err
	}
	statePath := parsed.values["state"]
	if statePath == "" && configPath != "" {
		statePath = configPath + ".state.db"
	}
	return Request{
		Command:     command,
		ConfigPath:  configPath,
		ProfileName: profileName,
		StatePath:   statePath,
		RunID:       parsed.values["run"],
		Latest:      command == "status",
		Detailed:    parsed.bools["detailed"],
	}, nil
}

func parseDiagnoseArguments(args []string) (Request, error) {
	values := originArgumentValues()
	values["--state"] = "state"
	values["--run"] = "run"
	parsed, err := parseCommandArguments(commandArgumentSpec{
		command: "diagnose",
		values:  values,
		unsupported: unsupportedArguments(
			"--ai-triage",
			"--timeout",
		),
	}, args)
	if err != nil {
		return Request{}, err
	}
	configPath, profileName, err := resolveConfigOrigin("diagnose", parsed)
	if err != nil {
		return Request{}, err
	}
	statePath := parsed.values["state"]
	if statePath == "" && configPath != "" {
		statePath = configPath + ".state.db"
	}
	return Request{
		Command:     "diagnose",
		ConfigPath:  configPath,
		ProfileName: profileName,
		StatePath:   statePath,
		RunID:       parsed.values["run"],
	}, nil
}

func parseProfileArguments(args []string) (Request, error) {
	if len(args) == 0 {
		return Request{}, fmt.Errorf("/profile: an action is required (see /help)")
	}
	action := args[0]
	parsed, err := parseCommandArguments(commandArgumentSpec{
		command: "profile " + action,
		values: map[string]string{
			"--config":          "config",
			"--passphrase-file": "passphrase-file",
		},
	}, args[1:])
	if err != nil {
		return Request{}, err
	}
	switch action {
	case "list":
		if len(parsed.positionals) != 0 || len(parsed.values) != 0 {
			return Request{}, fmt.Errorf("/profile list: no arguments are accepted")
		}
		return Request{Command: "profile", ProfileAction: "list"}, nil
	case "delete":
		if len(parsed.positionals) != 1 || parsed.positionals[0] == "" || len(parsed.values) != 0 {
			return Request{}, fmt.Errorf("/profile delete: profile NAME is required")
		}
		return Request{
			Command: "profile", ProfileAction: "delete", ProfileName: parsed.positionals[0],
		}, nil
	case "save":
		if len(parsed.positionals) < 1 || len(parsed.positionals) > 2 || parsed.positionals[0] == "" || parsed.values["passphrase-file"] != "" {
			return Request{}, fmt.Errorf("/profile save: expected NAME [CONFIG]")
		}
		configPath := parsed.values["config"]
		if len(parsed.positionals) == 2 {
			if configPath != "" {
				return Request{}, fmt.Errorf("/profile save: choose positional CONFIG or --config, not both")
			}
			configPath = parsed.positionals[1]
		}
		if configPath == "" {
			configPath = "config.yaml"
		}
		return Request{
			Command: "profile", ProfileAction: "save",
			ProfileName: parsed.positionals[0], ConfigPath: configPath,
		}, nil
	case "export":
		if len(parsed.positionals) < 1 || len(parsed.positionals) > 2 || parsed.positionals[0] == "" || parsed.values["config"] != "" || parsed.values["passphrase-file"] == "" {
			return Request{}, fmt.Errorf("/profile export: expected NAME [OUTPUT] --passphrase-file PATH")
		}
		outputPath := defaultProfileExportPath(parsed.positionals[0])
		if len(parsed.positionals) == 2 {
			outputPath = parsed.positionals[1]
		}
		return Request{
			Command: "profile", ProfileAction: "export", ProfileName: parsed.positionals[0], OutputPath: outputPath, PassphraseFile: parsed.values["passphrase-file"],
		}, nil
	case "import":
		if len(parsed.positionals) != 2 || parsed.positionals[0] == "" || parsed.positionals[1] == "" || parsed.values["config"] != "" || parsed.values["passphrase-file"] == "" {
			return Request{}, fmt.Errorf("/profile import: expected NAME INPUT --passphrase-file PATH")
		}
		return Request{
			Command: "profile", ProfileAction: "import", ProfileName: parsed.positionals[0], OutputPath: parsed.positionals[1], PassphraseFile: parsed.values["passphrase-file"],
		}, nil
	default:
		return Request{}, fmt.Errorf("/profile: unknown action %q (see /help)", action)
	}
}

// defaultProfileExportPath deliberately never reuses config.yaml: a portable
// profile is an encrypted JSON envelope, not a migration configuration. Names
// that are already safe file stems remain recognisable; every other name gets
// a bounded, path-safe digest rather than being interpreted as a path.
func defaultProfileExportPath(name string) string {
	if safeProfileExportStem(name) {
		return name + ".dmtx-profile.json"
	}
	sum := sha256.Sum256([]byte(name))
	return "profile-" + hex.EncodeToString(sum[:]) + ".dmtx-profile.json"
}

func safeProfileExportStem(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 200 {
		return false
	}
	for _, character := range name {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func parseInitArguments(args []string) (Request, error) {
	parsed, err := parseCommandArguments(commandArgumentSpec{
		command: "init",
		values:  map[string]string{"--config": "config"},
		bools:   map[string]string{"--force": "force"},
	}, args)
	if err != nil {
		return Request{}, err
	}
	if len(parsed.positionals) != 0 {
		return Request{}, fmt.Errorf("/init: config must be supplied with --config")
	}
	return Request{
		Command: "init", ConfigPath: parsed.values["config"], Force: parsed.bools["force"],
	}, nil
}

func parseInitSecretsArguments(args []string) (Request, error) {
	parsed, err := parseCommandArguments(commandArgumentSpec{
		command: "init-secrets",
		bools: map[string]string{
			"--force": "force", "-f": "force",
			// DMTX's protected starter template always includes its live AI
			// provider section, so --with-ai is an idempotent compatibility
			// spelling rather than a second, weaker template choice.
			"--with-ai": "with-ai",
		},
	}, args)
	if err != nil {
		return Request{}, err
	}
	if len(parsed.positionals) != 0 {
		return Request{}, fmt.Errorf("/init-secrets: unexpected positional argument")
	}
	return Request{Command: "init-secrets", Force: parsed.bools["force"]}, nil
}

func positiveWorkers(command, value string) (int, error) {
	workers, err := strconv.Atoi(value)
	if err != nil || workers < 1 {
		return 0, fmt.Errorf("/%s: --workers requires a positive integer, got %q", command, value)
	}
	return workers, nil
}
