// Package app owns the public command-line contract.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/contract"
	"github.com/johndauphine/dmtx/internal/migrate"
	"github.com/johndauphine/dmtx/internal/state"
)

const Version = "0.3.0-dev"

const (
	Success = iota
	ConfigurationError
	ConnectionError
	TransferError
	ValidationError
	Cancelled
	StateError
	FileError
)

// Run is the command-line surface: parse argv, execute, render. It is
// deliberately thin. Everything it does that is not parsing or rendering
// belongs in Execute, so a second surface can reach the same behaviour without
// synthesising an argv or scraping bytes back out of a stream.
func Run(args []string, stdout, stderr io.Writer) int {
	request, outcome, dispatched := parseRequest(args)
	if !dispatched {
		_ = RenderText(stdout, stderr, outcome)
		return outcome.ExitCode
	}
	outcome = ExecuteWithProgress(context.Background(), request, func(progress Progress) {
		_ = RenderProgress(stderr, progress)
	})
	if err := RenderText(stdout, stderr, outcome); err != nil {
		fmt.Fprintf(stderr, "write output: %v\n", err)
		return FileError
	}
	return outcome.ExitCode
}

// ParseRequest turns argv into a Request, for a surface that has words to parse
// but no command line to have parsed them.
//
// Exported so the browser console does not have to reimplement it. The flag
// rules are not decoration: run refuses a repeated --config, diagnose refuses an
// unknown flag rather than skipping it, resume refuses --abandon without a
// reason. Every one of those is a wrong answer prevented, and a second
// implementation in JavaScript would be a second place for them to be subtly
// different - unreachable by any Go test, which is where every other rule in
// this package is held.
//
// It is also what gives the §21.1 parity criterion something to stand on.
// Before this, "both surfaces offer the same commands" was a claim about two
// separate parsers agreeing. Now there is one parser, and agreement is not a
// thing that can lapse.
//
// dispatched=false means argv answered itself - version, help, an unknown
// command - and the Outcome holds what to say. There is nothing to Execute in
// that case, and a caller that executes anyway will be told the command is
// unknown.
func ParseRequest(args []string) (Request, Outcome, bool) {
	return parseRequest(args)
}

// parseRequest turns argv into a Request. It returns dispatched=false for the
// cases that are answered by argv alone - version, help, an unknown command -
// because those have no orchestration to perform and should not pretend to.
func parseRequest(args []string) (Request, Outcome, bool) {
	out := newOutcome("")
	if !contract.Valid() {
		return Request{}, out.failWith(
			StateError,
			"internal command registry is invalid",
		), false
	}
	if len(args) == 0 {
		out.out("DMTX has no terminal UI; use dmtx serve for the WebUI or --help for CLI commands.")
		return Request{}, out.done(Success), false
	}
	switch args[0] {
	case "--version", "version":
		out.out(Version)
		return Request{}, out.done(Success), false
	case "--help", "help":
		for _, line := range helpLines() {
			out.out(line)
		}
		return Request{}, out.done(Success), false
	case "run":
		options, err := parseRunArguments(args[1:])
		if err != nil {
			return refuseArguments(out, err, runUsage)
		}
		return Request{
			Command:                "run",
			ConfigPath:             options.configPath,
			ProfileName:            options.profileName,
			StatePath:              options.statePath,
			DryRun:                 options.dryRun,
			AcknowledgeDestructive: options.destructiveAcknowledged,
			SourceSchema:           options.sourceSchema,
			TargetSchema:           options.targetSchema,
			Workers:                options.workers,
			SkipPreflight:          options.skipPreflight,
		}, Outcome{}, true
	case "resume":
		options, err := parseResumeArguments(args[1:])
		if err != nil {
			return refuseArguments(out, err, resumeUsage)
		}
		return Request{
			Command:                "resume",
			ConfigPath:             options.configPath,
			ProfileName:            options.profileName,
			StatePath:              options.statePath,
			AcknowledgeDestructive: options.destructiveAcknowledged,
			ForceResume:            options.forceResume,
			Abandon:                options.abandon,
			AbandonReason:          options.abandonReason,
			SkipPreflight:          options.skipPreflight,
		}, Outcome{}, true
	case "status", "history":
		request, err := parseStateArguments(args[0], args[1:])
		if err != nil {
			usage := "usage: dmtx status [CONFIG | @CONFIG | --profile NAME] [--state PATH] [-d|--detailed]"
			if args[0] == "history" {
				usage = "usage: dmtx history [CONFIG | @CONFIG | --profile NAME] [--state PATH] [--run ID]"
			}
			return refuseArguments(out, err, usage)
		}
		return request, Outcome{}, true
	case "validate":
		request, err := parseConfigOriginArguments("validate", args[1:])
		if err != nil {
			return refuseArguments(out, err, "usage: dmtx validate [CONFIG | @CONFIG | --config CONFIG | --profile NAME]")
		}
		return request, Outcome{}, true
	case "config":
		request, err := parseConfigOriginArguments("config", args[1:])
		if err != nil {
			return refuseArguments(out, err, "usage: dmtx config [CONFIG | @CONFIG | --config CONFIG | --profile NAME]")
		}
		return request, Outcome{}, true
	case "analyze":
		request, err := parseConfigOriginArguments("analyze", args[1:])
		if err != nil {
			return refuseArguments(out, err, "usage: dmtx analyze [CONFIG | @CONFIG | --config CONFIG | --profile NAME]")
		}
		return request, Outcome{}, true
	case "init":
		request, err := parseInitArguments(args[1:])
		if err != nil {
			return refuseArguments(out, err, "usage: dmtx init [--config CONFIG] [--force]")
		}
		return request, Outcome{}, true
	case "init-secrets":
		request, err := parseInitSecretsArguments(args[1:])
		if err != nil {
			return refuseArguments(out, err, "usage: dmtx init-secrets [--force|-f] [--with-ai]")
		}
		return request, Outcome{}, true
	case "diagnose":
		request, err := parseDiagnoseArguments(args[1:])
		if err != nil {
			return refuseArguments(out, err, "usage: dmtx diagnose [CONFIG | @CONFIG | --profile NAME] [--state PATH] [--run ID]")
		}
		return request, Outcome{}, true
	case "preflight", "health-check":
		// The alias is resolved here so nothing downstream has to know it
		// exists.
		request, err := parseConfigOriginArguments("preflight", args[1:])
		if err != nil {
			return refuseArguments(out, err, "usage: dmtx preflight [CONFIG | @CONFIG | --config CONFIG | --profile NAME] [--skip-preflight LIST|all]")
		}
		return request, Outcome{}, true
	case "profile":
		request, err := parseProfileArguments(args[1:])
		if err != nil {
			return refuseArguments(out, err, "usage: dmtx profile save NAME [CONFIG] | list | delete NAME | export NAME [OUTPUT] --passphrase-file PATH (default: NAME.dmtx-profile.json) | import NAME INPUT --passphrase-file PATH")
		}
		return request, Outcome{}, true
	case "ai":
		request, err := parseAIArguments(args[1:])
		if err != nil {
			return refuseArguments(out, err, "usage: dmtx ai (config-review | runbook) [CONFIG | @CONFIG | --profile NAME] [--timeout DURATION] [--request TEXT]")
		}
		return request, Outcome{}, true
	default:
		return Request{}, classifyUnhandled(out, args[0]), false
	}
}

func refuseArguments(
	out *outcomeBuilder,
	err error,
	usage string,
) (Request, Outcome, bool) {
	message := err.Error()
	if usage != "" {
		message += "\n" + usage
	}
	return Request{}, out.failWith(ConfigurationError, message), false
}

// initArguments reads init's flags, refusing anything it does not know.
func initArguments(args []string) (Request, bool) {
	request, err := parseInitArguments(args)
	return request, err == nil
}

// diagnoseArguments reads diagnose's flags, refusing anything it does not know.
//
// Refusing matters more here than the flags do. Skipping an unrecognised flag
// would make "dmtx diagnose --state X --ruun abc" diagnose a different run than
// the operator asked about and say nothing about it - a wrong answer delivered
// confidently, which is worse than the error they would have fixed in seconds.
func diagnoseArguments(args []string) (Request, bool) {
	request, err := parseDiagnoseArguments(args)
	return request, err == nil
}

// classifyUnhandled answers for a command no surface implements yet.
//
// Both surfaces call this, which is the point. When the command line said a
// command was "planned in this stage" and Execute called the same command
// unknown, dmtx was telling an operator two different things about what it can
// do depending on where they asked - the one thing the parity criterion
// forbids. Restating the rule in two places is how that happened; there is one
// place now.
func classifyUnhandled(out *outcomeBuilder, command string) Outcome {
	// Resolve, not a name comparison: aliases are registry entries too, and
	// looking up only names made "webui" unknown while "serve", which it is an
	// alias of, was refused with a reason.
	if registered, ok := contract.Resolve(command); ok {
		if registered.WebUI == contract.Omitted && registered.TUI == contract.Omitted {
			// Registered but deliberately not something to run. The reason
			// comes from the registry rather than from here, because Omitted
			// covers two different situations - a command that exists but is
			// not a front end's to run, and one that does not apply to dmtx at
			// all - and answering both with the same sentence sends an operator
			// looking for a flag that will never exist.
			refusal := command + " is not available"
			if registered.Note != "" {
				refusal += ": " + registered.Note
			}
			return out.failWith(ConfigurationError, refusal)
		}
		out.out(command + " is planned in this stage.")
		return out.done(Success)
	}
	return out.failWith(
		ConfigurationError,
		fmt.Sprintf("unknown command %q; use --help", command),
	)
}

// Execute performs a request and reports what happened. This is the seam every
// surface shares: the CLI renders the Outcome as text, an API renders it as
// JSON, and a parity test compares two Outcomes rather than two transcripts.
// Every command is behind the seam. An unrecognised one is refused rather than
// silently producing an empty Outcome.
func Execute(ctx context.Context, request Request) Outcome {
	return ExecuteWithProgress(ctx, request, nil)
}

// ExecuteWithProgress runs a command and reports how far it has got.
//
// Separate from Execute rather than an extra parameter on it, because the
// overwhelming majority of callers - every CLI invocation - have nobody to
// report to, and because Request is a transport type pinned by golden wire
// tests. A function is not something that survives being serialised, so it does
// not belong in the request; it belongs beside it.
//
// progress may be nil, and a nil sink costs one nil check per checkpoint.
func ExecuteWithProgress(
	ctx context.Context,
	request Request,
	progress ProgressFunc,
) Outcome {
	reporter := newProgressReporter(progress)
	// An alias is canonicalised before dispatch, the way parseRequest already
	// does it for argv. Without this the seam refused "health-check" as unknown
	// while the command line accepted it, so the two surfaces disagreed about
	// what dmtx can do - and the Outcome would name a command nothing else
	// downstream recognises.
	if resolved, ok := contract.Resolve(request.Command); ok {
		request.Command = resolved.Name
	}
	request = ApplyCommandDefaults(request)
	switch request.Command {
	case "validate":
		return executeValidate(ctx, request)
	case "preflight":
		return executePreflight(ctx, request)
	case "status", "history":
		return executeShowState(request)
	case "config":
		return executeConfig(request)
	case "diagnose":
		return executeDiagnose(request)
	case "analyze":
		return executeAnalyze(ctx, request)
	case "init":
		return executeInit(request)
	case "profile":
		return executeProfile(request)
	case "init-secrets":
		return executeInitSecrets(request)
	case "ai":
		return executeAI(ctx, request)
	case "run":
		return executeRun(ctx, request, reporter)
	case "resume":
		return executeResume(ctx, request, reporter)
	default:
		return classifyUnhandled(newOutcome(request.Command), request.Command)
	}
}

// ApplyCommandDefaults applies DMT's historical config.yaml fallback only
// after a surface has had its chance to apply its own session defaults.
// Status/history intentionally do not participate: their useful WebUI default
// is the project-scoped state database, not a config-derived one.
func ApplyCommandDefaults(request Request) Request {
	if request.ConfigPath != "" || request.ProfileName != "" {
		return request
	}
	switch request.Command {
	case "run", "resume", "validate", "preflight", "config", "diagnose", "analyze", "ai":
		request.ConfigPath = "config.yaml"
	}
	return request
}

func executeRun(ctx context.Context, request Request, progress *progressReporter) Outcome {
	out := newOutcome(request.Command)
	options, ok := runOptionsFrom(request)
	if !ok {
		return out.failWith(ConfigurationError, runUsage)
	}
	configPath := options.configPath
	statePath := options.statePath
	dryRun := options.dryRun
	destructiveAcknowledged := options.destructiveAcknowledged
	data, _, err := configurationData(request)
	if err != nil {
		return out.failWith(FileError, "read configuration: "+err.Error())
	}
	cfg, err := config.Parse(data)
	if err != nil {
		return out.failWith(ConfigurationError, "configuration: "+err.Error())
	}
	if err := applyInvocationOverrides(&cfg, request); err != nil {
		return out.failWith(ConfigurationError, "configuration: "+err.Error())
	}
	auditPath := configPath
	if auditPath == "" {
		auditPath = statePath
	}
	if err := migrate.ValidateMigration(cfg); err != nil {
		return out.failWith(ConfigurationError, "configuration: "+err.Error())
	}
	cfg.Migration.DestructiveAcknowledged = destructiveAcknowledged
	if dryRun {
		plan, err := migrate.DryRun(ctx, cfg)
		if err != nil {
			return out.failWith(ConfigurationError, "dry run: "+err.Error())
		}
		if plan.Admission != nil && plan.Admission.Supported {
			applyDryRunSchemaDriftState(cfg, statePath, &plan)
		}
		if plan.Deletes != nil &&
			cfg.Migration.Deletes.Mode == config.DeleteModeReconcile {
			if stateErr := applyDryRunDeleteDueState(
				cfg,
				statePath,
				&plan,
				time.Now().UTC(),
			); stateErr != nil {
				plan.Proceed = false
				plan.Deletes.StateError =
					"durable delete due-state could not be inspected read-only"
			}
			migrate.ApplyDryRunDeleteCandidateImpact(
				ctx,
				cfg,
				&plan,
			)
		}
		if err := out.setPayload(PayloadPlan, plan); err != nil {
			return out.failWith(FileError, "write dry run: "+err.Error())
		}
		if !plan.Proceed {
			return out.done(ConfigurationError)
		}
		return out.done(Success)
	}
	if err := config.ValidateBoundedStage4Settings(cfg.Migration); err != nil {
		return out.failWith(ConfigurationError, "configuration: "+err.Error())
	}
	configHash, err := config.Hash(cfg)
	if err != nil {
		return out.failWith(StateError, "configuration hash: "+err.Error())
	}
	resumeCompatibilityHash, err := config.ResumeCompatibilityHash(cfg)
	if err != nil {
		return out.failWith(StateError, "resume compatibility hash: "+err.Error())
	}
	migrationContext, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	store, err := state.NewBackend(statePath)
	if err != nil {
		return out.failWith(StateError, "state backend: "+err.Error())
	}
	started := time.Now().UTC()
	runID := started.Format("20060102T150405.000000000Z")
	leaseStore, lease, err := acquireTargetLease(cfg.Target, runID)
	if err != nil {
		return out.failWith(StateError, "acquire target lease: "+err.Error())
	}
	guard := state.NewLeaseGuard(leaseStore, lease)
	leaseReleased := false
	defer func() {
		if !leaseReleased {
			_ = guard.Release()
		}
	}()
	store, err = newStage4FencedStateBackend(store, guard)
	if err != nil {
		return out.failWith(StateError, "fence Stage 4 state backend: "+err.Error())
	}
	sourceIdentity, err := endpointWorkloadIdentity(cfg.Source)
	if err != nil {
		return out.failWith(StateError, "source workload identity: "+err.Error())
	}
	targetIdentity, err := endpointWorkloadIdentity(cfg.Target)
	if err != nil {
		return out.failWith(StateError, "target workload identity: "+err.Error())
	}
	if err := store.InitializeRun(state.Run{
		ID:             runID,
		Source:         cfg.Source.Database,
		Target:         cfg.Target.Database,
		SourceEngine:   cfg.Source.Type,
		SourceIdentity: sourceIdentity,
		TargetIdentity: targetIdentity,
		Outcome:        state.Running,
		Resumable:      true,
		Reason:         "migration in progress",
		StartedAt:      started,
	}, configHash); err != nil {
		return out.failWith(StateError, "record migration state: "+err.Error())
	}
	if err := store.SaveResumeCompatibilityHash(runID, resumeCompatibilityHash); err != nil {
		return out.failWith(StateError, "record resume compatibility: "+err.Error())
	}
	spoolDirectory, err := stage4SpoolDirectory(statePath, runID)
	if err != nil {
		if stateErr := persistStage4SpoolPreparationFailure(
			store,
			runID,
			err,
		); stateErr != nil {
			return out.failWith(StateError, "record Stage 4 spool preparation failure: "+stateErr.Error())
		}
		return out.failWith(StateError, "Stage 4 spool directory: "+err.Error())
	}
	if err := appendAudit(auditPath, runID, "run_started"); err != nil {
		return out.failWith(StateError, err.Error())
	}
	if err := appLifecycleBoundary("run_initialized"); err != nil {
		return out.failWith(StateError, "run lifecycle: "+err.Error())
	}
	migrationContext, heartbeat := startLeaseHeartbeat(migrationContext, guard, 30*time.Second)
	observer := tableCheckpointObserver{
		store:          store,
		runID:          runID,
		guard:          guard,
		resume:         false,
		spoolDirectory: spoolDirectory,
		configPath:     auditPath,
		progress:       progress,
	}
	result, err := migrate.Execute(migrationContext, cfg, observer)
	if heartbeatErr := heartbeat.Stop(); heartbeatErr != nil {
		err = fmt.Errorf("%w: renew target lease: %v", state.ErrState, heartbeatErr)
	}
	if err == nil {
		if ownershipErr := guard.Renew(); ownershipErr != nil {
			err = fmt.Errorf("%w: verify final target lease: %v", state.ErrState, ownershipErr)
		}
	}
	if err != nil {
		disposition := migrationAttemptDisposition(result, err, cfg.Migration)
		endedAt := time.Now().UTC()
		if stateErr := persistAttemptDisposition(
			store, runID, disposition, err.Error(), endedAt,
		); stateErr != nil {
			return out.failWith(StateError, "record migration outcome: "+stateErr.Error())
		}
		if auditErr := appendAttemptTerminalAudit(
			auditPath,
			runID,
			"run",
			result,
			disposition,
			err,
		); auditErr != nil {
			return out.failWith(StateError, auditErr.Error())
		}
		if releaseErr := guard.Release(); releaseErr != nil {
			return out.failWith(StateError, "release target lease: "+releaseErr.Error())
		}
		leaseReleased = true
		if disposition.acceptedPartial {
			result.Validated = false
			if encodeErr := out.setPayload(PayloadPartialResult, acceptedPartialResult{
				Result: result, Outcome: state.Partial, Resumable: false,
			}); encodeErr != nil {
				return out.failWith(FileError, "write partial result: "+encodeErr.Error())
			}
			return out.done(Success)
		}
		out.fail("migration: " + err.Error())
		return out.done(disposition.exitCode)
	}
	if err := appendAudit(auditPath, runID, "validation_completed"); err != nil {
		return out.failWith(StateError, err.Error())
	}
	published, err := publishStage4RunSuccess(observer, runSuccessReason)
	if err != nil {
		return out.failWith(StateError, "publish completed migration state: "+err.Error())
	}
	if !published {
		if err := store.Append(state.Run{
			ID:             runID,
			Source:         cfg.Source.Database,
			Target:         cfg.Target.Database,
			SourceEngine:   cfg.Source.Type,
			SourceIdentity: sourceIdentity,
			TargetIdentity: targetIdentity,
			Outcome:        state.Success,
			Resumable:      false,
			Reason:         runSuccessReason,
			StartedAt:      started,
			EndedAt:        time.Now().UTC(),
		}); err != nil {
			return out.failWith(StateError, "record completed migration state: "+err.Error())
		}
	}
	if err := appLifecycleBoundary("run_success_persisted"); err != nil {
		return out.failWith(StateError, "run lifecycle: "+err.Error())
	}
	if err := guard.Release(); err != nil {
		return out.failWith(StateError, "release target lease: "+err.Error())
	}
	leaseReleased = true
	if err := appendAudit(auditPath, runID, "run_succeeded"); err != nil {
		return out.failWith(StateError, err.Error())
	}
	if err := out.setPayload(PayloadResult, result); err != nil {
		return out.failWith(FileError, "write result: "+err.Error())
	}
	return out.done(Success)
}

// applyDryRunSchemaDriftState reads only the latest successful aggregate
// source-schema sentinel for this exact workload. Dry-run must not create a
// run merely to use stateful selection, and a state-read uncertainty must be a
// structured non-proceed plan rather than a silently absent baseline.
func applyDryRunSchemaDriftState(
	cfg config.Config,
	statePath string,
	plan *migrate.Plan,
) {
	baseline := migrate.DryRunSchemaBaseline{}
	sourceIdentity, sourceErr := endpointWorkloadIdentity(cfg.Source)
	targetIdentity, targetErr := endpointWorkloadIdentity(cfg.Target)
	if sourceErr != nil || targetErr != nil {
		baseline.Error = "durable schema baseline scope could not be resolved"
		migrate.ApplyDryRunSchemaDrift(plan, cfg, baseline)
		return
	}
	record, found, err := state.ReadOnlyLatestSuccessfulSchemaSnapshot(
		statePath,
		state.SchemaSnapshotReadScope{
			SourceIdentity: sourceIdentity,
			TargetIdentity: targetIdentity,
			Task: state.TaskKey{
				Type:  "schema-contract",
				Table: "aggregate-source-schema",
			},
		},
	)
	if err != nil {
		baseline.Error =
			"durable schema baseline could not be inspected read-only"
		migrate.ApplyDryRunSchemaDrift(plan, cfg, baseline)
		return
	}
	baseline = migrate.DryRunSchemaBaseline{
		Found:         found,
		CanonicalJSON: record.CanonicalJSON,
		Digest:        record.Digest,
	}
	migrate.ApplyDryRunSchemaDrift(plan, cfg, baseline)
}

// applyDryRunDeleteDueState exposes due state only after matching every record
// to the exact canonical source, target, and Stage 4 table task. A state file
// often contains unrelated migration histories; its newest completed record
// must never influence another workload's delete schedule.
func applyDryRunDeleteDueState(
	cfg config.Config,
	statePath string,
	plan *migrate.Plan,
	now time.Time,
) error {
	if plan == nil || plan.Deletes == nil {
		return errors.New("dry-run delete plan is unavailable")
	}
	sourceIdentity, err := endpointWorkloadIdentity(cfg.Source)
	if err != nil {
		return fmt.Errorf("source workload identity: %w", err)
	}
	targetIdentity, err := endpointWorkloadIdentity(cfg.Target)
	if err != nil {
		return fmt.Errorf("target workload identity: %w", err)
	}
	tasks, err := dryRunDeleteTasks(cfg, plan.Tables)
	if err != nil {
		return err
	}
	plan.Deletes.Tables = make([]migrate.PlannedDeleteTable, len(tasks))
	for index, task := range tasks {
		plan.Deletes.Tables[index] = migrate.PlannedDeleteTable{
			Schema: task.Schema,
			Table:  task.Table,
		}
	}
	if len(tasks) == 0 {
		plan.Deletes.DueStateKnown = true
		plan.Deletes.Due = false
		plan.Deletes.DueReason = "no selected tables require reconciliation"
		plan.Deletes.DueStateScope =
			"per selected source/target/network-table-copy workload"
		return nil
	}
	evidence, err := state.ReadOnlyLatestSuccessfulDeleteReconciliations(
		statePath,
		state.DeleteReconciliationReadScope{
			SourceIdentity: sourceIdentity,
			TargetIdentity: targetIdentity,
			Tasks:          tasks,
		},
	)
	if err != nil {
		return err
	}
	if len(evidence) != len(tasks) {
		return errors.New("read-only delete due-state returned an incomplete task scope")
	}
	deleteTables := make([]migrate.PlannedDeleteTable, len(evidence))
	anyDue := false
	for index, item := range evidence {
		facts, dueErr := migrate.EvaluateDeleteReconciliationDue(
			now,
			cfg.Migration.Deletes.Reconcile.Interval,
			item.Record,
			item.Found,
		)
		if dueErr != nil {
			return fmt.Errorf(
				"durable delete due-state for %s.%s: %w",
				item.Task.Schema,
				item.Task.Table,
				dueErr,
			)
		}
		deleteTables[index] = migrate.PlannedDeleteTable{
			Schema:           item.Task.Schema,
			Table:            item.Task.Table,
			DueStateKnown:    true,
			Due:              facts.Due,
			LastSuccessfulAt: facts.LastSuccessfulAt,
			NextDueAt:        facts.NextDueAt,
			DueReason:        facts.Reason,
		}
		anyDue = anyDue || facts.Due
	}
	plan.Deletes.Tables = deleteTables
	plan.Deletes.DueStateKnown = true
	plan.Deletes.Due = anyDue
	plan.Deletes.DueStateScope =
		"per selected source/target/network-table-copy workload"
	if len(deleteTables) == 1 {
		plan.Deletes.LastSuccessfulAt = deleteTables[0].LastSuccessfulAt
		plan.Deletes.NextDueAt = deleteTables[0].NextDueAt
		plan.Deletes.DueReason = deleteTables[0].DueReason
	} else if anyDue {
		plan.Deletes.DueReason =
			"one or more selected table reconciliations are due"
	} else {
		plan.Deletes.DueReason =
			"all selected table reconciliations are not due"
	}
	return nil
}

func dryRunDeleteTasks(
	cfg config.Config,
	tables []migrate.PlannedTable,
) ([]state.TaskKey, error) {
	engine, err := config.CanonicalEngine(cfg.Source.Type)
	if err != nil {
		return nil, err
	}
	namespace := cfg.Source.Schema
	switch engine {
	case "postgres":
		if namespace == "" {
			namespace = "public"
		}
	case "mssql":
		if namespace == "" {
			namespace = "dbo"
		}
	case "mysql", "mariadb":
		if namespace == "" {
			namespace = cfg.Source.Database
		}
	case "sqlite":
		namespace = ""
	default:
		return nil, fmt.Errorf(
			"source engine %q has no documented delete-reconciliation task scope",
			engine,
		)
	}
	tasks := make([]state.TaskKey, len(tables))
	for index, table := range tables {
		tasks[index] = state.TaskKey{
			Type:   "network-table-copy",
			Schema: namespace,
			Table:  table.Name,
		}
		if err := tasks[index].Validate(); err != nil {
			return nil, fmt.Errorf(
				"build delete-reconciliation task for %s: %w",
				table.Name,
				err,
			)
		}
	}
	return tasks, nil
}

func migrationExitCode(err error) int {
	if isStateOrLeaseFailure(err) {
		return StateError
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Cancelled
	}
	if errors.Is(err, migrate.ErrDestructiveAcknowledgement) {
		return ConfigurationError
	}
	return TransferError
}

// executeShowState serves both status and history; Latest distinguishes them.
//
// The original wrote its errors to stdout rather than stderr. That is preserved
// exactly - it is the observable contract, and correcting it here would make
// this refactor a behaviour change wearing a refactor's clothes.
func executeShowState(request Request) Outcome {
	out := newOutcome(request.Command)
	if request.StatePath == "" {
		out.out("usage: dmtx status --state migration.yaml.state.db")
		return out.done(ConfigurationError)
	}
	store, err := state.NewBackend(request.StatePath)
	if err != nil {
		out.out(err.Error())
		return out.done(StateError)
	}
	if request.Latest {
		run, found, err := store.Latest()
		if err != nil {
			out.out(err.Error())
			return out.done(StateError)
		}
		if !found {
			out.out("no runs recorded")
			return out.done(Success)
		}
		if request.Detailed {
			tasks, err := store.ListTasks(run.ID)
			if err != nil {
				out.out(err.Error())
				return out.done(StateError)
			}
			if err := out.setPayload(PayloadStatusDetail, struct {
				Run   state.Run    `json:"run"`
				Tasks []state.Task `json:"tasks"`
			}{Run: publicRun(run), Tasks: tasks}); err != nil {
				out.out(err.Error())
				return out.done(FileError)
			}
			return out.done(Success)
		}
		if err := out.setPayload(PayloadRun, publicRun(run)); err != nil {
			out.out(err.Error())
			return out.done(FileError)
		}
		return out.done(Success)
	}
	runs, err := store.List()
	if err != nil {
		out.out(err.Error())
		return out.done(StateError)
	}
	if request.RunID != "" {
		for index := len(runs) - 1; index >= 0; index-- {
			if runs[index].ID != request.RunID {
				continue
			}
			if err := out.setPayload(PayloadRun, publicRun(runs[index])); err != nil {
				out.out(err.Error())
				return out.done(FileError)
			}
			return out.done(Success)
		}
		out.out(fmt.Sprintf("no run recorded with id %q", request.RunID))
		return out.done(StateError)
	}
	publicRuns := make([]state.Run, len(runs))
	for index, run := range runs {
		publicRuns[index] = publicRun(run)
	}
	if err := out.setPayload(PayloadRuns, publicRuns); err != nil {
		out.out(err.Error())
		return out.done(FileError)
	}
	return out.done(Success)
}

func publicRun(run state.Run) state.Run {
	run.LeaseOwnerToken = ""
	return run
}

// helpLines is the help text as data. A surface that is not a terminal needs
// the same content without a writer to push it into.
func helpLines() []string {
	return []string{
		"dmtx - deterministic database migration tool",
		"WebUI command syntax (CONFIG may also be written @CONFIG):",
		"  /run [CONFIG | --config CONFIG | --profile NAME] [--state PATH] [--source-schema NAME] [--target-schema NAME] [--workers N] [--skip-preflight LIST|all] [--dry-run] [--confirm-backup]",
		"  /resume [CONFIG | --config CONFIG | --profile NAME] [--state PATH] [--force-resume] [--skip-preflight LIST|all]",
		"  /preflight|/health-check [CONFIG | --config CONFIG | --profile NAME] [--skip-preflight LIST|all]",
		"  /validate [CONFIG | --config CONFIG | --profile NAME]",
		"  /diagnose [CONFIG | --config CONFIG | --profile NAME] [--state PATH] [--run ID]",
		"  /config [CONFIG | --config CONFIG | --profile NAME]",
		"  /analyze [CONFIG | --config CONFIG | --profile NAME]",
		"  /ai config-review|runbook [CONFIG | --profile NAME] [--timeout DURATION] [--request TEXT]",
		"  /status [CONFIG | --profile NAME] [--state PATH] [-d|--detailed]",
		"  /history [CONFIG | --profile NAME] [--state PATH] [--run ID]",
		"  /profile save NAME [CONFIG] | list | delete NAME | export NAME [OUTPUT] --passphrase-file PATH (default: NAME.dmtx-profile.json) | import NAME INPUT --passphrase-file PATH",
		"  /init [--config CONFIG] [--force] | /init-secrets [--force|-f] [--with-ai]",
		"  /setup [postgres|sqlserver] [CONFIG | @CONFIG | --config CONFIG | --profile NAME]",
		"  /session [KEY VALUE] | /session clear [KEY]    (config, profile, state-file)",
		"  /logs | /about | /clear | /quit|/exit",
		"",
		"Unavailable because DMTX has no equivalent engine capability:",
		"  /wizard; /cache; /verbosity; /explore; !shell; DMT-only AI operation flags",
	}
}
