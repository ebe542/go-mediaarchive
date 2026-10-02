package projecttool

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseValidateChecksTagAncestryAndChangelog(t *testing.T) {
	root := createReleaseProject(t)
	runner := newRecordingRunner()
	runner.outputs["git cat-file -t v1.2.3"] = "tag\n"
	runner.outputs["git rev-list -n 1 v1.2.3"] = "release-commit\n"
	stdout := &bytes.Buffer{}
	app := application{stdout: stdout, stderr: &bytes.Buffer{}, runner: runner, root: root}

	if exitCode := app.run([]string{"release", "validate", "--version", "v1.2.3"}); exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	commands := recordedCommands(runner.commands)
	for _, expected := range []string{
		"git cat-file -t v1.2.3",
		"git rev-list -n 1 v1.2.3",
		"git merge-base --is-ancestor release-commit origin/main",
	} {
		if !strings.Contains(commands, expected) {
			t.Errorf("expected command %q in:\n%s", expected, commands)
		}
	}
	if !strings.Contains(stdout.String(), "Release v1.2.3 is valid.") {
		t.Fatalf("expected validation result, got %q", stdout.String())
	}
}

func TestReleaseValidateRejectsInvalidState(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		version   string
		tagType   string
		changelog string
		expected  string
	}{
		{"invalid version", "1.2.3", "tag\n", "## [1.2.3] - 2026-09-12\n", "vMAJOR.MINOR.PATCH"},
		{"lightweight tag", "v1.2.3", "commit\n", "## [1.2.3] - 2026-09-12\n", "must be annotated"},
		{"missing changelog", "v1.2.3", "tag\n", "# Changelog\n", "no dated section"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, "CHANGELOG.md"), testCase.changelog, 0o644)
			runner := newRecordingRunner()
			runner.outputs["git cat-file -t "+testCase.version] = testCase.tagType
			runner.outputs["git rev-list -n 1 "+testCase.version] = "release-commit\n"
			stderr := &bytes.Buffer{}
			app := application{stdout: &bytes.Buffer{}, stderr: stderr, runner: runner, root: root}

			if exitCode := app.run([]string{"release", "validate", "--version", testCase.version}); exitCode == 0 {
				t.Fatal("expected validation failure")
			}
			if !strings.Contains(stderr.String(), testCase.expected) {
				t.Fatalf("expected %q, got %q", testCase.expected, stderr.String())
			}
		})
	}
}

func TestReleaseBuildCreatesArchivesAndChecksums(t *testing.T) {
	root := createReleaseProject(t)
	runner := newRecordingRunner()
	runner.onRun = func(command Command) error {
		if command.Name != "go" || len(command.Args) < 5 || command.Args[0] != "build" {
			return nil
		}
		outputPath := command.Args[3]
		return os.WriteFile(outputPath, []byte("test binary"), 0o755)
	}
	app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, runner: runner, root: root}

	if exitCode := app.run([]string{
		"release", "build", "--version", "v1.2.3", "--output-directory", "dist",
	}); exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	dist := filepath.Join(root, "dist")
	entries, err := os.ReadDir(dist)
	if err != nil {
		t.Fatalf("read release output: %v", err)
	}
	if len(entries) != 6 {
		t.Fatalf("expected five archives and checksums, got %d", len(entries))
	}
	checksumData, err := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	if err != nil {
		t.Fatalf("read checksums: %v", err)
	}
	if lines := strings.Count(strings.TrimSpace(string(checksumData)), "\n") + 1; lines != 5 {
		t.Fatalf("expected five checksums, got %d", lines)
	}

	archivePath := filepath.Join(dist, "go-mediaarchive_1.2.3_windows_amd64.zip")
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open Windows archive: %v", err)
	}
	defer archive.Close()
	archiveNames := make([]string, 0, len(archive.File))
	for _, file := range archive.File {
		archiveNames = append(archiveNames, file.Name)
	}
	for _, expected := range []string{
		"go-mediaarchive_1.2.3_windows_amd64/go-mediaarchive-server.exe",
		"go-mediaarchive_1.2.3_windows_amd64/go-mediaarchive-admin.exe",
		"go-mediaarchive_1.2.3_windows_amd64/go-mediaarchive-client.exe",
		"go-mediaarchive_1.2.3_windows_amd64/CHANGELOG.md",
		"go-mediaarchive_1.2.3_windows_amd64/LICENSE",
		"go-mediaarchive_1.2.3_windows_amd64/README.md",
	} {
		if !containsString(archiveNames, expected) {
			t.Errorf("expected archive member %q in %v", expected, archiveNames)
		}
	}
	if commands := recordedCommands(runner.commands); strings.Count(commands, "go build -trimpath") != 15 {
		t.Fatalf("expected 15 cross-platform builds, got:\n%s", commands)
	}
}

func TestReleaseBuildDoesNotPublishAfterBuildFailure(t *testing.T) {
	root := createReleaseProject(t)
	runner := newRecordingRunner()
	runner.onRun = func(command Command) error {
		if command.Name == "go" && len(command.Args) > 0 && command.Args[0] == "build" {
			return errors.New("build failed")
		}

		return nil
	}
	stderr := &bytes.Buffer{}
	app := application{stdout: &bytes.Buffer{}, stderr: stderr, runner: runner, root: root}

	if exitCode := app.run([]string{"release", "build", "--version", "v1.2.3"}); exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	entries, err := os.ReadDir(filepath.Join(root, "dist"))
	if err != nil {
		t.Fatalf("read release output: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no published files, got %v", entries)
	}
	if !strings.Contains(stderr.String(), "build server for linux/amd64") {
		t.Fatalf("expected contextual build error, got %q", stderr.String())
	}
}

func createReleaseProject(test *testing.T) string {
	test.Helper()

	root := test.TempDir()
	writeTestFile(test, filepath.Join(root, "CHANGELOG.md"), "## [1.2.3] - 2026-09-12\n", 0o644)
	writeTestFile(test, filepath.Join(root, "LICENSE"), "MIT License\n", 0o644)
	writeTestFile(test, filepath.Join(root, "README.md"), "# Test project\n", 0o644)

	return root
}

func writeTestFile(
	test *testing.T,
	path string,
	content string,
	mode os.FileMode,
) {
	test.Helper()

	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		test.Fatalf("write test file: %v", err)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}

	return false
}
