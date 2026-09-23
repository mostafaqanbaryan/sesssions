package fzf

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mostafaqanbaryan/sesssions/internal/domain"
)

// Confirm shows an fzf dialog with the given choices and an optional shell
// preview command. It returns the selected choice, or domain.ErrEmptySelection
// when the dialog is cancelled (escape / ctrl-c). cwd is the working directory
// for both fzf and its preview command.
func Confirm(header, preview string, choices []string, cwd string) (string, error) {
	args := []string{
		"--header", header,
		"--reverse",
		"--style", "full",
		"--pointer", "→",
		"--prompt", "> ",
	}
	if preview != "" {
		args = append(args, "--preview", preview, "--preview-window", "down,60%")
	}

	cmd := exec.Command("fzf", args...)
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader(strings.Join(choices, "\n"))

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if cmd.ProcessState.ExitCode() == 130 {
			return "", domain.ErrEmptySelection
		}
		return "", fmt.Errorf("something wrong: %w", err)
	}

	return strings.TrimSpace(out.String()), nil
}
