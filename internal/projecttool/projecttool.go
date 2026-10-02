package projecttool

import (
	"fmt"
	"io"
	"strings"
)

const usage = `Usage: projectctl COMMAND [OPTIONS]

Commands:
  check         Run the standard project checks.
  quality-gate  Validate workflows and reproduce the quality gate locally.
  release       Validate or build a release.

Run "projectctl COMMAND --help" for command-specific options.
`

type application struct {
	stdout io.Writer
	stderr io.Writer
	runner commandRunner
	root   string
}

// Run executes one project command and returns a process exit code.
func Run(arguments []string, stdout io.Writer, stderr io.Writer) int {
	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)

		return 1
	}

	app := application{
		stdout: stdout,
		stderr: stderr,
		runner: execRunner{stdout: stdout, stderr: stderr},
		root:   root,
	}

	return app.run(arguments)
}

func (app application) run(arguments []string) int {
	if len(arguments) == 0 {
		fmt.Fprint(app.stderr, usage)

		return 2
	}
	if arguments[0] == "--help" || arguments[0] == "-h" {
		fmt.Fprint(app.stdout, usage)

		return 0
	}

	var err error
	switch arguments[0] {
	case "check":
		err = app.runCheckCommand(arguments[1:])
	case "quality-gate":
		err = app.runQualityGateCommand(arguments[1:])
	case "release":
		err = app.runReleaseCommand(arguments[1:])
	default:
		fmt.Fprintf(app.stderr, "Error: unknown command: %s\n\n%s", arguments[0], usage)

		return 2
	}
	if err != nil {
		fmt.Fprintf(app.stderr, "Error: %v\n", err)

		return 1
	}

	return 0
}

func (app application) runStep(
	description string,
	name string,
	arguments ...string,
) error {
	fmt.Fprintf(app.stdout, "\n==> %s\n", description)
	if err := app.runner.Run(Command{Name: name, Args: arguments, Dir: app.root}); err != nil {
		return fmt.Errorf("%s: %w", strings.ToLower(description), err)
	}

	return nil
}

func (app application) requireCommand(name string, hint string) error {
	if err := app.runner.LookPath(name); err != nil {
		return fmt.Errorf("required command %q was not found; install it with: %s", name, hint)
	}

	return nil
}
