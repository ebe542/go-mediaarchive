// Package projecttool provides cross-platform repository automation.
package projecttool

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Command describes one external development tool invocation.
type Command struct {
	Name string
	Args []string
	Dir  string
}

type commandRunner interface {
	Run(Command) error
	Output(Command) (string, error)
	LookPath(string) error
}

type execRunner struct {
	stdout io.Writer
	stderr io.Writer
}

func (runner execRunner) Run(argCommand Command) error {
	command := exec.Command(argCommand.Name, argCommand.Args...)
	command.Dir = argCommand.Dir
	command.Stdout = runner.stdout
	command.Stderr = runner.stderr

	return command.Run()
}

func (runner execRunner) Output(argCommand Command) (string, error) {
	command := exec.Command(argCommand.Name, argCommand.Args...)
	command.Dir = argCommand.Dir
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = runner.stderr

	return stdout.String(), command.Run()
}

func (execRunner) LookPath(argName string) error {
	_, err := exec.LookPath(argName)

	return err
}

func findProjectRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("determine working directory: %w", err)
	}

	for {
		modulePath := directory + string(os.PathSeparator) + "go.mod"
		if info, statErr := os.Stat(modulePath); statErr == nil && !info.IsDir() {
			return directory, nil
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return "", fmt.Errorf("inspect module file: %w", statErr)
		}

		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errorsNewProjectRoot()
		}
		directory = parent
	}
}

func errorsNewProjectRoot() error {
	return errors.New("project root containing go.mod was not found")
}
