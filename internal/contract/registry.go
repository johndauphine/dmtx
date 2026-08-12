package contract

// FrontendDisposition makes unsupported and planned interactive capabilities explicit.
type FrontendDisposition string

const (
	Supported FrontendDisposition = "supported"
	Planned   FrontendDisposition = "planned"
	Omitted   FrontendDisposition = "omitted"
)

type Command struct {
	Name        string
	Aliases     []string
	Description string
	Category    string
	TUI         FrontendDisposition
	WebUI       FrontendDisposition

	// Note explains an Omitted disposition to whoever asks for the command.
	//
	// Omitted covers two different situations that must not be answered with
	// the same sentence: a command that exists but is not something a front end
	// should run, and a command that does not apply to dmtx at all. An operator
	// told the wrong one goes looking for a flag that will never exist.
	Note string
}

var Commands = []Command{
	// Stage 5 deliberately replaces the TUI with the authenticated WebUI.
	{Name: "run", Description: "Run a migration", Category: "migration", TUI: Omitted, WebUI: Supported},
	{Name: "resume", Description: "Resume an interrupted migration", Category: "migration", TUI: Omitted, WebUI: Supported},
	{Name: "status", Description: "Show migration status", Category: "inspect", TUI: Omitted, WebUI: Supported},
	{Name: "history", Description: "Show migration history", Category: "inspect", TUI: Omitted, WebUI: Supported},
	{Name: "validate", Description: "Validate configuration", Category: "inspect", TUI: Omitted, WebUI: Supported},
	{Name: "diagnose", Description: "Diagnose configuration or state", Category: "inspect", TUI: Omitted, WebUI: Supported},
	{Name: "preflight", Aliases: []string{"health-check"}, Description: "Run preflight checks", Category: "inspect", TUI: Omitted, WebUI: Supported},
	{Name: "analyze", Description: "Explain the transfer plan", Category: "inspect", TUI: Omitted, WebUI: Supported},
	{Name: "profile", Description: "Manage saved profiles", Category: "configuration", TUI: Omitted, WebUI: Supported},
	{Name: "ai", Description: "Request an AI configuration review", Category: "advisory", TUI: Omitted, WebUI: Supported},
	{Name: "init", Description: "Initialize a configuration", Category: "configuration", TUI: Omitted, WebUI: Supported},
	{Name: "init-secrets", Description: "Initialize protected secrets", Category: "configuration", TUI: Omitted, WebUI: Supported},
	{Name: "setup", Description: "Run guided setup", Category: "configuration", TUI: Omitted, WebUI: Supported},
	{
		Name: "wizard", Description: "Edit a migration configuration", Category: "configuration", TUI: Omitted, WebUI: Omitted,
		Note: "dmtx has guided setup but no separate in-place configuration editor; setup never overwrites an existing configuration",
	},
	// These are browser-session commands. Their state and rendering belong to
	// the WebUI, but placing them in the shared registry makes the command
	// vocabulary match DMT rather than maintaining a hidden client-side list.
	{Name: "session", Description: "Manage session defaults", Category: "system", TUI: Omitted, WebUI: Supported},
	{Name: "logs", Description: "Save the session transcript", Category: "system", TUI: Omitted, WebUI: Supported},
	{
		Name: "verbosity", Description: "Show or set log verbosity", Category: "system", TUI: Omitted, WebUI: Omitted,
		Note: "dmtx has no configurable logging subsystem; a stored level would not affect command behavior",
	},
	{
		Name: "explore", Description: "Manage exploration policy", Category: "system", TUI: Omitted, WebUI: Omitted,
		Note: "dmtx runtime tuning is deterministic safety control, not DMT's exploratory policy",
	},
	{Name: "about", Description: "Show version and features", Category: "system", TUI: Omitted, WebUI: Supported},
	{Name: "help", Description: "Show command help", Category: "system", TUI: Omitted, WebUI: Supported},
	{Name: "clear", Description: "Clear the console", Category: "system", TUI: Omitted, WebUI: Supported},
	{Name: "quit", Aliases: []string{"exit"}, Description: "Leave the console", Category: "system", TUI: Omitted, WebUI: Supported},
	// cache is Omitted rather than Planned because there is nothing to clear.
	// DMT's cache command clears a type-mapping cache; dmtx has none, and the
	// only thing it keeps under the user cache directory is lease coordination
	// state, which is durable and would be actively harmful to clear while a
	// migration is running. A command that clears nothing is worse than an
	// absent one, because it tells an operator something happened. Reinstate
	// this the day there is a cache worth clearing.
	{
		Name: "cache", Description: "Manage the migration cache", Category: "system", TUI: Omitted, WebUI: Omitted,
		Note: "dmtx keeps no cache to clear",
	},
	// config was in DMT's slash-command surface but missing here; without it a
	// front end built from this registry would silently lack it.
	{Name: "config", Description: "Show resolved configuration", Category: "inspect", TUI: Omitted, WebUI: Supported},
	// serve starts the browser front end. It is Omitted for both interactive
	// surfaces because it is the thing that launches them: a command to start
	// the WebUI, offered inside the WebUI, is nonsense rather than a gap.
	{
		Name: "serve", Aliases: []string{"webui", "gui"}, Description: "Start the WebUI", Category: "system",
		TUI: Omitted, WebUI: Omitted,
		Note: "serve starts a front end, so it is not something a front end runs",
	},
}

// Resolve finds a command by its name or any of its aliases.
//
// Aliases are part of the registry, so anything reading the registry to decide
// what dmtx can do has to read them too. Matching on Name alone made
// "health-check" - a registered alias of preflight - work on the command line
// and come back as an unknown command through the seam, which is the surface
// disagreement the parity criterion forbids.
func Resolve(name string) (Command, bool) {
	for _, command := range Commands {
		if command.Name == name {
			return command, true
		}
		for _, alias := range command.Aliases {
			if alias == name {
				return command, true
			}
		}
	}
	return Command{}, false
}

// Valid reports whether the shipped registry holds its invariants.
func Valid() bool { return valid(Commands) }

// valid takes the registry as an argument so the invariants can be tested
// against deliberately broken ones.
//
// Valid() alone could only ever be checked against the registry that ships,
// which asserts that the data is good and nothing at all about whether the
// check works - removing a rule from here and the field it guards leaves such
// a test passing.
func valid(commands []Command) bool {
	seen := map[string]bool{}
	for _, command := range commands {
		if command.Name == "" || command.Description == "" || command.Category == "" ||
			command.TUI == "" || command.WebUI == "" {
			return false
		}
		// A command omitted from both surfaces has to say why, or its refusal
		// explains nothing to the operator who asked for it.
		if command.TUI == Omitted && command.WebUI == Omitted && command.Note == "" {
			return false
		}
		// Names and aliases share one space, because that is how they are
		// looked up. A collision would make which command answers depend on
		// registry order.
		for _, spelling := range append([]string{command.Name}, command.Aliases...) {
			if seen[spelling] {
				return false
			}
			seen[spelling] = true
		}
	}
	return true
}
