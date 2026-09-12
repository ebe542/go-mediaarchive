package projecttool

import "fmt"

const qualityGateUsage = `Usage: projectctl quality-gate [--act] [--skip-race] [--help]

Options:
  --act        Run the GitHub Actions verify job locally with act and Docker.
  --skip-race  Skip the race detector in the direct local checks.
  --help       Show this help text.
`

func (app application) runQualityGateCommand(argArguments []string) error {
	runAct := false
	runRace := true
	for _, argument := range argArguments {
		switch argument {
		case "--act":
			runAct = true
		case "--skip-race":
			runRace = false
		case "--help", "-h":
			fmt.Fprint(app.stdout, qualityGateUsage)

			return nil
		default:
			fmt.Fprint(app.stderr, qualityGateUsage)

			return fmt.Errorf("unknown quality-gate argument %q", argument)
		}
	}

	if err := app.requireCommand(
		"actionlint",
		"go install github.com/rhysd/actionlint/cmd/actionlint@latest",
	); err != nil {
		return err
	}
	if err := app.runStep("GitHub Actions workflow syntax", "actionlint"); err != nil {
		return err
	}

	fmt.Fprintln(app.stdout, "\n==> Quality gate project checks")
	if err := app.runChecks(runRace); err != nil {
		return err
	}

	if runAct {
		if err := app.requireCommand("docker", "Docker Desktop with its Linux engine"); err != nil {
			return err
		}
		if err := app.requireCommand("act", "winget install nektos.act"); err != nil {
			return err
		}
		if err := app.runStep("Docker availability", "docker", "info"); err != nil {
			return err
		}
		if err := app.runStep(
			"GitHub Actions verify job",
			"act",
			"pull_request",
			"--job",
			"verify",
		); err != nil {
			return err
		}
	}

	fmt.Fprintln(app.stdout, "\nAll local quality gate checks passed.")

	return nil
}
