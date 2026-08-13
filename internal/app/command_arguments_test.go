package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johndauphine/dmtx/internal/state"
)

func TestDMTArgumentSyntaxBuildsCanonicalRequests(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want Request
	}{
		{
			name: "run at config and equals state",
			args: []string{"run", "@migration.yaml", "--state=custom.db", "--dry-run", "--confirm-backup"},
			want: Request{
				Command: "run", ConfigPath: "migration.yaml", StatePath: "custom.db",
				DryRun: true, AcknowledgeDestructive: true,
			},
		},
		{
			name: "resume positional config",
			args: []string{"resume", "migration.yaml", "--force-resume"},
			want: Request{
				Command: "resume", ConfigPath: "migration.yaml",
				StatePath: "migration.yaml.state.db", ForceResume: true,
			},
		},
		{
			name: "validate positional config",
			args: []string{"validate", "@migration.yaml"},
			want: Request{Command: "validate", ConfigPath: "migration.yaml"},
		},
		{
			name: "config equals profile",
			args: []string{"config", "--profile=prod"},
			want: Request{Command: "config", ProfileName: "prod"},
		},
		{
			name: "preflight alias positional config",
			args: []string{"health-check", "migration.yaml"},
			want: Request{Command: "preflight", ConfigPath: "migration.yaml"},
		},
		{
			name: "status positional config",
			args: []string{"status", "@migration.yaml"},
			want: Request{
				Command: "status", ConfigPath: "migration.yaml",
				StatePath: "migration.yaml.state.db", Latest: true,
			},
		},
		{
			name: "history selected run",
			args: []string{"history", "migration.yaml", "--run=run-7"},
			want: Request{
				Command: "history", ConfigPath: "migration.yaml",
				StatePath: "migration.yaml.state.db", RunID: "run-7",
			},
		},
		{
			name: "diagnose selected run",
			args: []string{"diagnose", "@migration.yaml", "--run", "run-7"},
			want: Request{
				Command: "diagnose", ConfigPath: "migration.yaml",
				StatePath: "migration.yaml.state.db", RunID: "run-7",
			},
		},
		{
			name: "profile save positional config",
			args: []string{"profile", "save", "prod", "@migration.yaml"},
			want: Request{
				Command: "profile", ProfileAction: "save",
				ProfileName: "prod", ConfigPath: "migration.yaml",
			},
		},
		{
			name: "init equals config",
			args: []string{"init", "--config=migration.yaml", "--force"},
			want: Request{Command: "init", ConfigPath: "migration.yaml", Force: true},
		},
		{
			name: "run DMT invocation overrides",
			args: []string{"run", "migration.yaml", "--source-schema=src", "--target-schema", "dst", "--workers", "4", "--skip-preflight", "source.connectivity,target.connectivity"},
			want: Request{Command: "run", ConfigPath: "migration.yaml", StatePath: "migration.yaml.state.db", SourceSchema: "src", TargetSchema: "dst", Workers: 4, SkipPreflight: "source.connectivity,target.connectivity"},
		},
		{
			name: "status detailed",
			args: []string{"status", "migration.yaml", "--detailed"},
			want: Request{Command: "status", ConfigPath: "migration.yaml", StatePath: "migration.yaml.state.db", Latest: true, Detailed: true},
		},
		{
			name: "profile export output",
			args: []string{"profile", "export", "prod", "@portable.json", "--passphrase-file=secret"},
			want: Request{Command: "profile", ProfileAction: "export", ProfileName: "prod", OutputPath: "portable.json", PassphraseFile: "secret"},
		},
		{
			name: "profile export default is portable envelope",
			args: []string{"profile", "export", "prod", "--passphrase-file=secret"},
			want: Request{Command: "profile", ProfileAction: "export", ProfileName: "prod", OutputPath: "prod.dmtx-profile.json", PassphraseFile: "secret"},
		},
		{
			name: "profile import",
			args: []string{"profile", "import", "prod", "@portable.json", "--passphrase-file", "secret"},
			want: Request{Command: "profile", ProfileAction: "import", ProfileName: "prod", OutputPath: "portable.json", PassphraseFile: "secret"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, outcome, dispatched := ParseRequest(test.args)
			if !dispatched {
				t.Fatalf("arguments were refused: %+v", outcome.Messages)
			}
			gotJSON, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			wantJSON, err := json.Marshal(test.want)
			if err != nil {
				t.Fatal(err)
			}
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("request = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestMalformedAndUnimplementedDMTArgumentsAreExplicitlyRefused(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"history", "--run"}, "requires a value"},
		{[]string{"history", "--unknown"}, "unknown flag --unknown"},
		{[]string{"status", "--state", "one.db", "--state=two.db"}, "provided more than once"},
		{[]string{"validate", "migration.yaml", "--ai-triage"}, "exists in DMT but its capability is not supported"},
	}
	for _, test := range tests {
		_, outcome, dispatched := ParseRequest(test.args)
		if dispatched {
			t.Errorf("%v dispatched", test.args)
			continue
		}
		if len(outcome.Messages) != 1 || !strings.Contains(outcome.Messages[0].Text, test.want) {
			t.Errorf("%v refusal = %+v, want %q", test.args, outcome.Messages, test.want)
		}
	}
}

func TestHelpAndUsageAdvertiseAcceptedDMTForms(t *testing.T) {
	help := strings.Join(helpLines(), "\n")
	for _, expected := range []string{
		"/run [CONFIG", "--skip-preflight LIST|all", "/resume [CONFIG",
		"/preflight|/health-check", "[-d|--detailed]", "[--force|-f] [--with-ai]",
		"--passphrase-file PATH (default: NAME.dmtx-profile.json)",
	} {
		if !strings.Contains(help, expected) {
			t.Errorf("help missing %q", expected)
		}
	}

	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"status", "--unknown"}, "[-d|--detailed]"},
		{[]string{"resume", "--unknown"}, "[--skip-preflight LIST|all]"},
		{[]string{"preflight", "--unknown"}, "[--skip-preflight LIST|all]"},
		{[]string{"init-secrets", "--unknown"}, "[--force|-f] [--with-ai]"},
		{[]string{"profile", "export", "prod"}, "default: NAME.dmtx-profile.json"},
	} {
		_, outcome, dispatched := ParseRequest(test.args)
		if dispatched || len(outcome.Messages) != 1 || !strings.Contains(outcome.Messages[0].Text, test.want) {
			t.Errorf("%v outcome = %+v, want usage containing %q", test.args, outcome, test.want)
		}
	}
}

func TestHistoryRunSelectsTheLatestTransitionForThatID(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "history.yaml")
	store := state.YAMLStore{Path: statePath}
	started := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	if err := store.InitializeRun(state.Run{
		ID: "selected", Outcome: state.Running, Resumable: true, StartedAt: started,
	}, "hash"); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(state.Run{
		ID: "selected", Outcome: state.Success, StartedAt: started, EndedAt: started.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeRun(state.Run{
		ID: "newer-other", Outcome: state.Failed, StartedAt: started.Add(2 * time.Minute),
	}, "other-hash"); err != nil {
		t.Fatal(err)
	}

	outcome := executeShowState(Request{
		Command: "history", StatePath: statePath, RunID: "selected",
	})
	if outcome.ExitCode != Success || outcome.Payload == nil || outcome.Payload.Kind != PayloadRun {
		t.Fatalf("selected history outcome = %+v", outcome)
	}
	var selected state.Run
	if err := json.Unmarshal(outcome.Payload.Data, &selected); err != nil {
		t.Fatal(err)
	}
	if selected.ID != "selected" || selected.Outcome != state.Success {
		t.Fatalf("selected transition = %+v", selected)
	}
}
