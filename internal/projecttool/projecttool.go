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

Run "projectctl COMMAND --help" for command-specific options.
`

type application struct {
	stdout io.Writer
	stderr io.Writer
	runner commandRunner
	root   string
}

// Run executes one project command and returns a process exit code.
func Run(argArguments []string, argStdout io.Writer, argStderr io.Writer) int {
	root, err := findProjectRoot()
	if err != nil {
		fmt.Fprintf(argStderr, "Error: %v\n", err)

		return 1
	}

	app := application{
		stdout: argStdout,
		stderr: argStderr,
		runner: execRunner{stdout: argStdout, stderr: argStderr},
		root:   root,
	}

	return app.run(argArguments)
}

func (app application) run(argArguments []string) int {
	if len(argArguments) == 0 {
		fmt.Fprint(app.stderr, usage)

		return 2
	}
	if argArguments[0] == "--help" || argArguments[0] == "-h" {
		fmt.Fprint(app.stdout, usage)

		return 0
	}

	var err error
	switch argArguments[0] {
	case "check":
		err = app.runCheckCommand(argArguments[1:])
	case "quality-gate":
		err = app.runQualityGateCommand(argArguments[1:])
	default:
		fmt.Fprintf(app.stderr, "Error: unknown command: %s\n\n%s", argArguments[0], usage)

		return 2
	}
	if err != nil {
		fmt.Fprintf(app.stderr, "Error: %v\n", err)

		return 1
	}

	return 0
}

func (app application) runStep(
	argDescription string,
	argName string,
	argArguments ...string,
) error {
	fmt.Fprintf(app.stdout, "\n==> %s\n", argDescription)
	if err := app.runner.Run(Command{Name: argName, Args: argArguments, Dir: app.root}); err != nil {
		return fmt.Errorf("%s: %w", strings.ToLower(argDescription), err)
	}

	return nil
}

func (app application) requireCommand(argName string, argHint string) error {
	if err := app.runner.LookPath(argName); err != nil {
		return fmt.Errorf("required command %q was not found; install it with: %s", argName, argHint)
	}

	return nil
}
