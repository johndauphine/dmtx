package app

// Choosing what a run acts on: options from a Request or from argv, and the
// rules both must obey.
//
// This mirrors resume_selection.go deliberately. Resume has had a shared
// validResumeOptions since the seam was introduced; run did not, and the
// difference was a live §21.1 violation rather than a stylistic one. The
// derivation below lived only in runArguments - an argv parser - so
// `dmtx run --config m.yaml` recorded into m.yaml.state.db while the same
// command through the API recorded into nothing at all.

// runUsage and resumeUsage are each one string, so the flags an operator is
// offered cannot drift from the flags the parser accepts without both changing
// in the same edit. Both were duplicated across two files before this - the
// parser's copy and the executor's - which is two places to update and one to
// forget.
//
// The --state example is a .yaml deliberately, though the derived default is
// .state.db. state.NewBackend selects the YAML store for .yaml and .yml and
// SQLite for everything else, so the example shows the thing --state is for:
// naming a state file the default would not have produced. An example matching
// the default would demonstrate only what happens without the flag.
const runUsage = "usage: dmtx run [CONFIG | @CONFIG | --config CONFIG | --profile NAME] " +
	"[--state PATH] [--source-schema NAME] [--target-schema NAME] [--workers N] " +
	"[--skip-preflight LIST|all] [--dry-run] [--acknowledge-destructive | --confirm-backup]"

const resumeUsage = "usage: dmtx resume [CONFIG | @CONFIG | --config CONFIG | --profile NAME] " +
	"[--state PATH] [--acknowledge-destructive] " +
	"[--force-resume] [--skip-preflight LIST|all] [--abandon --abandon-reason TEXT]"

// runOptions is what a run acts on, however it was asked for.
type runOptions struct {
	configPath              string
	profileName             string
	statePath               string
	dryRun                  bool
	destructiveAcknowledged bool
	sourceSchema            string
	targetSchema            string
	workers                 int
	skipPreflight           string
}

// runOptionsFrom builds the options from a Request rather than argv, so a
// surface with no command line gets the same defaults the command line gets.
func runOptionsFrom(request Request) (runOptions, bool) {
	configPath := request.ConfigPath
	if configPath == "" && request.ProfileName == "" {
		configPath = "config.yaml"
	}
	return validRunOptions(runOptions{
		configPath:              configPath,
		profileName:             request.ProfileName,
		statePath:               request.StatePath,
		dryRun:                  request.DryRun,
		destructiveAcknowledged: request.AcknowledgeDestructive,
		sourceSchema:            request.SourceSchema,
		targetSchema:            request.TargetSchema,
		workers:                 request.Workers,
		skipPreflight:           request.SkipPreflight,
	})
}

// validRunOptions holds the rules both entry points must obey.
//
// The state path is derived here rather than in either caller. That is the
// whole point: a rule applied in a parser is a rule the surfaces without a
// parser do not get, and "both remember to derive it" is not a design - it is
// two chances to forget.
func validRunOptions(options runOptions) (runOptions, bool) {
	if (options.configPath == "") == (options.profileName == "") {
		return runOptions{}, false
	}
	if options.statePath == "" {
		if options.configPath == "" {
			// A profile has no safe filesystem-derived state name.
			return runOptions{}, false
		}
		options.statePath = options.configPath + ".state.db"
	}
	return options, true
}
