package projecttool

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type recordingRunner struct {
	commands    []Command
	outputs     map[string]string
	runErrors   map[string]error
	lookupError map[string]error
}

func (runner *recordingRunner) Run(argCommand Command) error {
	runner.commands = append(runner.commands, argCommand)

	return runner.runErrors[commandKey(argCommand)]
}

func (runner *recordingRunner) Output(argCommand Command) (string, error) {
	runner.commands = append(runner.commands, argCommand)

	return runner.outputs[commandKey(argCommand)], runner.runErrors[commandKey(argCommand)]
}

func (runner *recordingRunner) LookPath(argName string) error {
	return runner.lookupError[argName]
}

func TestCheckRunsStandardChecksAndOptionalRaceDetector(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		args     []string
		wantRace bool
	}{
		{"standard", []string{"check"}, false},
		{"race", []string{"check", "--race"}, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			runner := newRecordingRunner()
			stdout := &bytes.Buffer{}
			app := newTestApplication(runner, stdout, &bytes.Buffer{})

			if exitCode := app.run(testCase.args); exitCode != 0 {
				t.Fatalf("expected exit code 0, got %d", exitCode)
			}
			commands := recordedCommands(runner.commands)
			for _, expected := range []string{
				"go version",
				"gofmt -l .",
				"go mod tidy -diff",
				"go mod verify",
				"go vet ./...",
				"go test -count=1 -cover ./...",
				"go build ./...",
				"git diff --check",
				"git diff --cached --check",
			} {
				if !strings.Contains(commands, expected) {
					t.Errorf("expected command %q in:\n%s", expected, commands)
				}
			}
			hasRace := strings.Contains(commands, "go test -count=1 -race ./...")
			if hasRace != testCase.wantRace {
				t.Errorf("expected race command presence %t, got:\n%s", testCase.wantRace, commands)
			}
			if !strings.Contains(stdout.String(), "All project checks passed.") {
				t.Fatalf("expected success output, got %q", stdout.String())
			}
		})
	}
}

func TestCheckRejectsUnformattedFiles(t *testing.T) {
	runner := newRecordingRunner()
	runner.outputs["gofmt -l ."] = "internal/example.go\n"
	stderr := &bytes.Buffer{}
	app := newTestApplication(runner, &bytes.Buffer{}, stderr)

	if exitCode := app.run([]string{"check"}); exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "internal/example.go") {
		t.Fatalf("expected unformatted filename, got %q", stderr.String())
	}
	if strings.Contains(recordedCommands(runner.commands), "go mod tidy -diff") {
		t.Fatal("expected checks to stop after formatting failure")
	}
}

func TestQualityGateRunsRaceChecksByDefault(t *testing.T) {
	runner := newRecordingRunner()
	stdout := &bytes.Buffer{}
	app := newTestApplication(runner, stdout, &bytes.Buffer{})

	if exitCode := app.run([]string{"quality-gate"}); exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	commands := recordedCommands(runner.commands)
	if !strings.Contains(commands, "actionlint") ||
		!strings.Contains(commands, "go test -count=1 -race ./...") {
		t.Fatalf("expected workflow and race checks, got:\n%s", commands)
	}
	if strings.Contains(commands, "docker info") || strings.Contains(commands, "act pull_request") {
		t.Fatalf("expected act checks to remain optional, got:\n%s", commands)
	}
}

func TestQualityGateActChecksToolsAndRunsVerifyJob(t *testing.T) {
	runner := newRecordingRunner()
	app := newTestApplication(runner, &bytes.Buffer{}, &bytes.Buffer{})

	if exitCode := app.run([]string{"quality-gate", "--act", "--skip-race"}); exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	commands := recordedCommands(runner.commands)
	if !strings.Contains(commands, "docker info") ||
		!strings.Contains(commands, "act pull_request --job verify") {
		t.Fatalf("expected local workflow commands, got:\n%s", commands)
	}
	if strings.Contains(commands, "go test -count=1 -race ./...") {
		t.Fatalf("expected race detector to be skipped, got:\n%s", commands)
	}
}

func TestQualityGateReportsMissingActionlint(t *testing.T) {
	runner := newRecordingRunner()
	runner.lookupError["actionlint"] = errors.New("not found")
	stderr := &bytes.Buffer{}
	app := newTestApplication(runner, &bytes.Buffer{}, stderr)

	if exitCode := app.run([]string{"quality-gate"}); exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "go install github.com/rhysd/actionlint") {
		t.Fatalf("expected installation hint, got %q", stderr.String())
	}
}

func TestUnknownCommandIsAUsageError(t *testing.T) {
	stderr := &bytes.Buffer{}
	app := newTestApplication(newRecordingRunner(), &bytes.Buffer{}, stderr)

	if exitCode := app.run([]string{"unknown"}); exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("expected usage error, got %q", stderr.String())
	}
}

func newRecordingRunner() *recordingRunner {
	return &recordingRunner{
		outputs:     make(map[string]string),
		runErrors:   make(map[string]error),
		lookupError: make(map[string]error),
	}
}

func newTestApplication(
	argRunner commandRunner,
	argStdout *bytes.Buffer,
	argStderr *bytes.Buffer,
) application {
	return application{
		stdout: argStdout,
		stderr: argStderr,
		runner: argRunner,
		root:   "project-root",
	}
}

func commandKey(argCommand Command) string {
	return strings.TrimSpace(argCommand.Name + " " + strings.Join(argCommand.Args, " "))
}

func recordedCommands(argCommands []Command) string {
	values := make([]string, 0, len(argCommands))
	for _, command := range argCommands {
		values = append(values, commandKey(command))
	}

	return strings.Join(values, "\n")
}
