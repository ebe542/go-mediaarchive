package projecttool

import (
	"errors"
	"fmt"
	"strings"
)

const checkUsage = `Usage: projectctl check [--race] [--help]

Options:
  --race  Run the test suite with Go's race detector.
  --help  Show this help text.
`

func (app application) runCheckCommand(argArguments []string) error {
	runRace := false
	for _, argument := range argArguments {
		switch argument {
		case "--race":
			runRace = true
		case "--help", "-h":
			fmt.Fprint(app.stdout, checkUsage)

			return nil
		default:
			fmt.Fprint(app.stderr, checkUsage)
			return fmt.Errorf("unknown check argument %q", argument)
		}
	}

	return app.runChecks(runRace)
}

func (app application) runChecks(argRunRace bool) error {
	fmt.Fprintf(app.stdout, "Checking project in %s\n", app.root)
	if err := app.runStep("Go version", "go", "version"); err != nil {
		return err
	}

	fmt.Fprintln(app.stdout, "\n==> Go formatting")
	unformatted, err := app.runner.Output(Command{Name: "gofmt", Args: []string{"-l", "."}, Dir: app.root})
	if err != nil {
		return fmt.Errorf("go formatting: %w", err)
	}
	if strings.TrimSpace(unformatted) != "" {
		fmt.Fprintf(
			app.stderr,
			"The following Go files require formatting:\n%sRun: go fmt ./...\n",
			unformatted,
		)

		return errors.New("go formatting failed")
	}

	steps := []struct {
		description string
		name        string
		arguments   []string
	}{
		{"Module files", "go", []string{"mod", "tidy", "-diff"}},
		{"Module checksums", "go", []string{"mod", "verify"}},
		{"Static analysis", "go", []string{"vet", "./..."}},
		{"Tests", "go", []string{"test", "-count=1", "-cover", "./..."}},
	}
	for _, step := range steps {
		if err := app.runStep(step.description, step.name, step.arguments...); err != nil {
			return err
		}
	}

	if argRunRace {
		if err := app.runStep("Race detector", "go", "test", "-count=1", "-race", "./..."); err != nil {
			return err
		}
	}
	if err := app.runStep("Build", "go", "build", "./..."); err != nil {
		return err
	}

	fmt.Fprintln(app.stdout, "\n==> Git whitespace")
	for _, arguments := range [][]string{{"diff", "--check"}, {"diff", "--cached", "--check"}} {
		if err := app.runner.Run(Command{Name: "git", Args: arguments, Dir: app.root}); err != nil {
			return fmt.Errorf("git whitespace: %w", err)
		}
	}

	fmt.Fprintln(app.stdout, "\nAll project checks passed.")

	return nil
}
