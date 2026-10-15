package projecttool

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

const execRunnerOutputHelper = "GO_MEDIAARCHIVE_EXEC_RUNNER_OUTPUT_HELPER"

func TestExecRunnerOutputCapturesCommandOutput(t *testing.T) {
	var stderr bytes.Buffer
	runner := execRunner{stderr: &stderr}

	output, err := runner.Output(Command{
		Name: os.Args[0],
		Args: []string{"-test.run=^TestExecRunnerOutputHelper$"},
		Env:  []string{execRunnerOutputHelper + "=1"},
	})
	if err != nil {
		t.Fatalf("run output helper: %v: %s", err, stderr.String())
	}
	if strings.TrimSpace(output) != "captured output" {
		t.Fatalf("expected captured output, got %q", output)
	}
}

func TestExecRunnerOutputHelper(t *testing.T) {
	if os.Getenv(execRunnerOutputHelper) != "1" {
		return
	}

	fmt.Fprintln(os.Stdout, "captured output")
	os.Exit(0)
}
