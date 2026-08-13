package app

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/profiles"
)

func profileTestPaths(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	secretsPath := filepath.Join(directory, "secrets.yaml")
	if err := os.WriteFile(secretsPath, []byte("encryption:\n  master_key: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(directory, "profiles.db"), secretsPath
}

func profileTestConfig() []byte {
	return []byte("source:\n  type: sqlite\n  database: source.db\ntarget:\n  type: sqlite\n  database: target.db\nmigration:\n  target_mode: drop_recreate\n")
}

func TestResumeProfilePreservesDMTOriginSyntax(t *testing.T) {
	request, _, dispatched := ParseRequest([]string{"resume", "--profile=prod"})
	if !dispatched {
		t.Fatal("DMT profile spelling was refused before the WebUI could apply its state default")
	}
	if request.ProfileName != "prod" || request.StatePath != "" || request.ConfigPath != "" {
		t.Fatalf("unexpected unresolved profile request: %+v", request)
	}
	request, _, dispatched = ParseRequest([]string{"resume", "--profile", "prod", "--state", "run.db"})
	if !dispatched {
		t.Fatal("profile resume with explicit state was refused")
	}
	if request.ProfileName != "prod" || request.StatePath != "run.db" || request.ConfigPath != "" {
		t.Fatalf("unexpected resume profile request: %+v", request)
	}
}

func TestProfileOriginLoadsIntoNormalConfigParser(t *testing.T) {
	profilesPath, secretsPath := profileTestPaths(t)
	store, err := profiles.OpenWithSecrets(profilesPath, secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("prod", profileTestConfig()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	data, origin, err := configurationDataAt(
		Request{Command: "resume", ProfileName: "prod"},
		profilesPath,
		secretsPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if origin != "profile prod" {
		t.Fatalf("origin = %q", origin)
	}
	if _, err := config.Parse(data); err != nil {
		t.Fatalf("profile bytes bypassed normal parser: %v", err)
	}
}

func TestProfileOriginRejectsAmbiguousSelection(t *testing.T) {
	_, _, err := configurationDataAt(
		Request{ConfigPath: "migration.yaml", ProfileName: "prod"},
		"ignored.db",
		"ignored.yaml",
	)
	if err == nil || !strings.Contains(err.Error(), "either configuration path or profile") {
		t.Fatalf("ambiguous origin error = %v", err)
	}
}

func TestProfileCommandSaveListDelete(t *testing.T) {
	profilesPath, secretsPath := profileTestPaths(t)
	open := func() (*profiles.Store, error) {
		return profiles.OpenWithSecrets(profilesPath, secretsPath)
	}
	configPath := filepath.Join(t.TempDir(), "migration.yaml")
	if err := os.WriteFile(configPath, profileTestConfig(), 0o600); err != nil {
		t.Fatal(err)
	}
	save := executeProfileWithStore(
		newOutcome("profile"),
		Request{Command: "profile", ProfileAction: "save", ProfileName: "prod", ConfigPath: configPath},
		open,
	)
	if save.ExitCode != Success {
		t.Fatalf("save outcome: %+v", save)
	}
	list := executeProfileWithStore(newOutcome("profile"), Request{Command: "profile", ProfileAction: "list"}, open)
	if list.ExitCode != Success || len(list.Messages) != 1 || list.Messages[0].Text != "prod" {
		t.Fatalf("list outcome: %+v", list)
	}
	deleteOutcome := executeProfileWithStore(
		newOutcome("profile"),
		Request{Command: "profile", ProfileAction: "delete", ProfileName: "prod"},
		open,
	)
	if deleteOutcome.ExitCode != Success {
		t.Fatalf("delete outcome: %+v", deleteOutcome)
	}
	if bytes.Contains([]byte(strings.Join(messageTexts(deleteOutcome), "\n")), []byte("password")) {
		t.Fatal("profile output exposed plaintext")
	}
}

func TestProfilePortableExportImportWritesOwnerOnlyCiphertext(t *testing.T) {
	profilesPath, secretsPath := profileTestPaths(t)
	open := func() (*profiles.Store, error) {
		return profiles.OpenWithSecrets(profilesPath, secretsPath)
	}
	store, err := open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("prod", profileTestConfig()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(t.TempDir(), "portable.yaml")
	passphraseFile := filepath.Join(t.TempDir(), "passphrase")
	passphrase := []byte("correct horse battery staple\n")
	if err := os.WriteFile(passphraseFile, passphrase, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := executeProfileWithStore(newOutcome("profile"), Request{
		Command: "profile", ProfileAction: "export", ProfileName: "prod", OutputPath: output, PassphraseFile: passphraseFile,
	}, open)
	if result.ExitCode != Success {
		t.Fatalf("export outcome: %+v", result)
	}
	wantMessage := "exported portable encrypted profile prod to " + output
	if len(result.Messages) != 1 || result.Messages[0].Text != wantMessage {
		t.Fatalf("export messages = %+v, want %q", result.Messages, wantMessage)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, profileTestConfig()) || bytes.Contains(data, []byte("correct horse")) {
		t.Fatalf("portable export contains plaintext: %q", data)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("export permissions = %04o, want 0600", info.Mode().Perm())
	}

	imported := executeProfileWithStore(newOutcome("profile"), Request{
		Command: "profile", ProfileAction: "import", ProfileName: "imported", OutputPath: output, PassphraseFile: passphraseFile,
	}, open)
	if imported.ExitCode != Success {
		t.Fatalf("import outcome: %+v", imported)
	}
	store, err = open()
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("imported")
	_ = store.Close()
	if err != nil || !bytes.Equal(got, profileTestConfig()) {
		t.Fatalf("round trip = %q, %v", got, err)
	}
}

func TestProfileImportAuthenticationFailureDoesNotOverwrite(t *testing.T) {
	profilesPath, secretsPath := profileTestPaths(t)
	open := func() (*profiles.Store, error) { return profiles.OpenWithSecrets(profilesPath, secretsPath) }
	store, err := open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("prod", []byte("original")); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	portable := filepath.Join(t.TempDir(), "portable.json")
	if err := os.WriteFile(portable, []byte(`{"format":"dmtx-profile-export","version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	passphrase := filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(passphrase, []byte("good"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome := executeProfileWithStore(newOutcome("profile"), Request{Command: "profile", ProfileAction: "import", ProfileName: "prod", OutputPath: portable, PassphraseFile: passphrase}, open)
	if outcome.ExitCode == Success || strings.Contains(strings.Join(messageTexts(outcome), "\n"), "good") {
		t.Fatalf("unsafe import outcome: %+v", outcome)
	}
	store, err = open()
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("prod")
	_ = store.Close()
	if err != nil || string(got) != "original" {
		t.Fatalf("profile mutated after failed import: %q, %v", got, err)
	}
}

func TestProfileImportInvalidConfigurationAndInsecurePassphraseDoNotMutate(t *testing.T) {
	profilesPath, secretsPath := profileTestPaths(t)
	open := func() (*profiles.Store, error) { return profiles.OpenWithSecrets(profilesPath, secretsPath) }
	store, err := open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("prod", []byte("original")); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	secret := []byte("never-print-this-passphrase")
	portable, err := profiles.SealPortable([]byte("not: [valid"), secret)
	if err != nil {
		t.Fatal(err)
	}
	portablePath := filepath.Join(t.TempDir(), "portable.json")
	if err := os.WriteFile(portablePath, portable, 0o600); err != nil {
		t.Fatal(err)
	}
	passphrasePath := filepath.Join(t.TempDir(), "passphrase")
	if err := os.WriteFile(passphrasePath, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := executeProfileWithStore(newOutcome("profile"), Request{Command: "profile", ProfileAction: "import", ProfileName: "prod", OutputPath: portablePath, PassphraseFile: passphrasePath}, open)
	if invalid.ExitCode == Success || strings.Contains(strings.Join(messageTexts(invalid), "\n"), "not: [valid") {
		t.Fatalf("invalid config outcome: %+v", invalid)
	}
	if err := os.Chmod(passphrasePath, 0o644); err != nil {
		t.Fatal(err)
	}
	insecure := executeProfileWithStore(newOutcome("profile"), Request{Command: "profile", ProfileAction: "import", ProfileName: "prod", OutputPath: portablePath, PassphraseFile: passphrasePath}, open)
	if insecure.ExitCode == Success || strings.Contains(strings.Join(messageTexts(insecure), "\n"), string(secret)) {
		t.Fatalf("insecure passphrase outcome: %+v", insecure)
	}
	store, err = open()
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("prod")
	_ = store.Close()
	if err != nil || string(got) != "original" {
		t.Fatalf("profile mutated after refused import: %q, %v", got, err)
	}
}

func TestProfileExportRefusesPassphraseFileAliases(t *testing.T) {
	profilesPath, secretsPath := profileTestPaths(t)
	open := func() (*profiles.Store, error) { return profiles.OpenWithSecrets(profilesPath, secretsPath) }
	store, err := open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("prod", profileTestConfig()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	passphrasePath := filepath.Join(directory, "passphrase")
	passphraseBytes := []byte("correct horse battery staple\n")
	if err := os.WriteFile(passphrasePath, passphraseBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		output string
	}{
		{name: "same path", output: passphrasePath},
	}
	hardLink := filepath.Join(directory, "passphrase-hardlink")
	if err := os.Link(passphrasePath, hardLink); err == nil {
		cases = append(cases, struct {
			name   string
			output string
		}{name: "hard link", output: hardLink})
	} else {
		t.Logf("hard links unavailable; skipping hard-link alias case: %v", err)
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			beforePassphrase, err := os.ReadFile(passphrasePath)
			if err != nil {
				t.Fatal(err)
			}
			beforeOutput, err := os.ReadFile(testCase.output)
			if err != nil {
				t.Fatal(err)
			}
			outcome := executeProfileWithStore(newOutcome("profile"), Request{
				Command: "profile", ProfileAction: "export", ProfileName: "prod", OutputPath: testCase.output, PassphraseFile: passphrasePath,
			}, open)
			if outcome.ExitCode == Success {
				t.Fatalf("aliased export succeeded: %+v", outcome)
			}
			afterPassphrase, err := os.ReadFile(passphrasePath)
			if err != nil {
				t.Fatal(err)
			}
			afterOutput, err := os.ReadFile(testCase.output)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(afterPassphrase, beforePassphrase) || !bytes.Equal(afterOutput, beforeOutput) {
				t.Fatal("aliased export changed the passphrase/output bytes")
			}
		})
	}
}

func TestProfileImportRefusesPassphraseFileAliasesWithoutMutation(t *testing.T) {
	profilesPath, secretsPath := profileTestPaths(t)
	open := func() (*profiles.Store, error) { return profiles.OpenWithSecrets(profilesPath, secretsPath) }
	store, err := open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("prod", []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	passphrasePath := filepath.Join(directory, "passphrase")
	if err := os.WriteFile(passphrasePath, []byte("correct horse battery staple\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		input string
	}{
		{name: "same path", input: passphrasePath},
	}
	hardLink := filepath.Join(directory, "passphrase-hardlink")
	if err := os.Link(passphrasePath, hardLink); err == nil {
		cases = append(cases, struct {
			name  string
			input string
		}{name: "hard link", input: hardLink})
	} else {
		t.Logf("hard links unavailable; skipping hard-link alias case: %v", err)
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			outcome := executeProfileWithStore(newOutcome("profile"), Request{
				Command: "profile", ProfileAction: "import", ProfileName: "prod", OutputPath: testCase.input, PassphraseFile: passphrasePath,
			}, open)
			if outcome.ExitCode == Success {
				t.Fatalf("aliased import succeeded: %+v", outcome)
			}
			store, err := open()
			if err != nil {
				t.Fatal(err)
			}
			got, loadErr := store.Load("prod")
			_ = store.Close()
			if loadErr != nil || string(got) != "original" {
				t.Fatalf("profile mutated after aliased import: %q, %v", got, loadErr)
			}
		})
	}
}

func TestProfilePortableFileReadersBoundAndRejectLinks(t *testing.T) {
	directory := t.TempDir()
	oversizePassphrase := filepath.Join(directory, "oversize-passphrase")
	if err := os.WriteFile(oversizePassphrase, bytes.Repeat([]byte{'p'}, maxProfilePassphraseFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProfilePassphrase(oversizePassphrase); err == nil {
		t.Fatal("oversize passphrase file was accepted")
	}
	oversizePortable := filepath.Join(directory, "oversize-portable")
	if err := os.WriteFile(oversizePortable, bytes.Repeat([]byte{'x'}, maxPortableProfileFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRegularProfileFile(oversizePortable); err == nil {
		t.Fatal("oversize portable file was accepted")
	}
	if runtime.GOOS == "windows" {
		return
	}
	target := filepath.Join(directory, "target")
	if err := os.WriteFile(target, []byte("passphrase"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readProfilePassphrase(link); err == nil {
		t.Fatal("passphrase symlink was accepted")
	}
	if _, err := readRegularProfileFile(link); err == nil {
		t.Fatal("portable symlink was accepted")
	}
}

func TestReadBoundedRegularFileRejectsSwapBetweenLstatAndOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks is not reliably available on Windows")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "passphrase")
	original := filepath.Join(directory, "original")
	target := filepath.Join(directory, "replacement")
	if err := os.WriteFile(path, []byte("original passphrase"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("replacement passphrase"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := readBoundedRegularFileWithOpen(path, maxProfilePassphraseFile, true, func(name string) (*os.File, error) {
		if err := os.Rename(name, original); err != nil {
			return nil, err
		}
		if err := os.Symlink(target, name); err != nil {
			return nil, err
		}
		return os.Open(name)
	})
	if err == nil {
		t.Fatal("reader accepted a path swapped to a symlink between Lstat and Open")
	}
}

func messageTexts(outcome Outcome) []string {
	texts := make([]string, len(outcome.Messages))
	for index, message := range outcome.Messages {
		texts[index] = message.Text
	}
	return texts
}
