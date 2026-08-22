package app

import (
	"os"
	"strings"
	"testing"
)

func TestTaggedReleaseCommandsDoNotDependOnAWorkingTree(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	// Git may check this file out with CRLF endings on Windows. Normalize the
	// content so this source-level workflow assertion is platform-independent.
	workflow := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, command := range []string{
		"gh release view \"$GITHUB_REF_NAME\" \\\n" +
			"              --repo \"$GITHUB_REPOSITORY\"",
		"gh release upload \"$GITHUB_REF_NAME\" dist/* --clobber \\\n" +
			"              --repo \"$GITHUB_REPOSITORY\"",
		"gh release create \"$GITHUB_REF_NAME\" dist/* \\\n" +
			"              --repo \"$GITHUB_REPOSITORY\"",
	} {
		if !strings.Contains(workflow, command) {
			t.Fatalf("release command does not name its repository:\n%s", command)
		}
	}
}

func TestReleaseWorkflowEmbedsV100ReleaseCandidateTags(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, snippet := range []string{
		"1.0.0-rc.1",
		"version=\"${GITHUB_REF_NAME#v}\"",
		"^[0-9]+\\.[0-9]+\\.[0-9]+([+-][0-9A-Za-z.-]+)?$",
		"-X github.com/johndauphine/dmtx/internal/app.Version=$VERSION",
		"test \"$(verify/dmtx --version)\" = \"$VERSION\"",
	} {
		if !strings.Contains(workflow, snippet) {
			t.Fatalf("release workflow is missing version handling %q", snippet)
		}
	}
}

func TestReleaseWorkflowMarksHyphenatedVersionsAsPrereleases(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, snippet := range []string{
		"shell: bash",
		"if [[ \"$version\" == *-* ]]; then",
		"prerelease_flag=(--prerelease)",
		"prerelease_flag=()",
		"\"${prerelease_flag[@]}\"",
	} {
		if !strings.Contains(workflow, snippet) {
			t.Fatalf("release workflow is missing prerelease handling %q", snippet)
		}
	}
}
