// Package shell runs the external commands ghprofile depends on.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ErrNoCommand is returned when no supported helper is installed.
var ErrNoCommand = errors.New("no supported command found")

// Result is the outcome of running an external command.
type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// Run executes a command and captures its output. A non-zero exit is reported
// in Result.Code rather than as an error, because some tools use exit codes to
// mean something other than failure.
func Run(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return res, nil
	case errors.As(err, &exitErr):
		res.Code = exitErr.ExitCode()
		return res, nil
	default:
		return res, fmt.Errorf("run %s: %w", name, err)
	}
}

// Copy puts text on the system clipboard.
func Copy(ctx context.Context, text string) error {
	name, args, err := clipboardCommand()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("copy to clipboard: %w", err)
	}
	return nil
}

// Open asks the desktop to open a URL or file.
func Open(ctx context.Context, target string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s: %w", name, ErrNoCommand)
	}
	if _, err := Run(ctx, name, target); err != nil {
		return err
	}
	return nil
}

func clipboardCommand() (string, []string, error) {
	candidates := [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err == nil {
			return c[0], c[1:], nil
		}
	}
	return "", nil, ErrNoCommand
}

// RunInteractive runs a command with the terminal attached, for tools that
// prompt. Nothing is captured, because the point is that the user sees and
// answers the prompt themselves.
func RunInteractive(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w", name, err)
	}
	return nil
}
