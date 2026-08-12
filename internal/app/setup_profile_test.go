package app

import (
	"strings"
	"testing"
)

func TestWizardRefusalExplainsMissingEditorMode(t *testing.T) {
	_, outcome, dispatched := ParseRequest([]string{"wizard", "--profile", "saved"})
	if dispatched || outcome.ExitCode == Success || len(outcome.Messages) != 1 {
		t.Fatalf("wizard outcome = %+v, dispatched=%v", outcome, dispatched)
	}
	if !strings.Contains(outcome.Messages[0].Text, "no separate in-place configuration editor") {
		t.Fatalf("wizard refusal = %q", outcome.Messages[0].Text)
	}
}
