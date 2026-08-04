package fzf

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mostafaqanbaryan/sesssions/internal/domain"
)

// type bindKey struct {
// 	key         string
// 	command     string
// 	description string
// }

type FzfWindow[T domain.SearchItem] struct {
	cmd   *exec.Cmd
	binds []string
	title []string
	// previewCommand string
}

func NewFzfWindow[T domain.SearchItem]() *FzfWindow[T] {
	cmd := exec.Command("fzf")
	return &FzfWindow[T]{
		cmd: cmd,
	}
}

func (f *FzfWindow[T]) BindKey(key, description string) {
	f.binds = append(f.binds, strings.ToLower(key))
	f.title = append(f.title, description+": "+key)
}

func (f *FzfWindow[T]) Preview(command string) {
	f.cmd.Args = append(f.cmd.Args, "--preview", command)
}

func (f *FzfWindow[T]) ShowColumns(columns string) {
	f.cmd.Args = append(f.cmd.Args, "--with-nth", columns)
}

func (f *FzfWindow[T]) Cwd(cwd string) {
	f.cmd.Dir = cwd
}

func (f *FzfWindow[T]) Display(rows []string) (T, string, error) {
	f.cmd.Args = append(f.cmd.Args, "--header", strings.Join(f.title, " | "))
	f.cmd.Args = append(f.cmd.Args, "--delimiter", "\t")
	f.cmd.Args = append(f.cmd.Args, "--preview-window", "right,30%")
	f.cmd.Args = append(f.cmd.Args, "--reverse")
	f.cmd.Args = append(f.cmd.Args, "--style", "full")
	f.cmd.Args = append(f.cmd.Args, "--prompt", "Session > ")
	f.cmd.Args = append(f.cmd.Args, "--pointer", "→")
	if f.binds != nil {
		f.cmd.Args = append(f.cmd.Args, "--expect", strings.Join(f.binds, ","))
	}

	f.cmd.Stdin = strings.NewReader(strings.Join(rows, "\n"))

	var out bytes.Buffer
	f.cmd.Stdout = &out
	f.cmd.Stderr = os.Stderr

	var zero T
	if err := f.cmd.Run(); err != nil {
		if f.cmd.ProcessState.ExitCode() == 130 {
			return zero, "", domain.ErrEmptySelection
		}

		return zero, "", fmt.Errorf("something wrong: %w", err)
	}

	var args []string
	if f.binds != nil {
		args = strings.Split(out.String(), "\n")
	} else {
		args = []string{"NO_EXPECT", out.String()}
	}

	t, err := zero.Parse(args[1])
	if err != nil {
		return zero, "", err
	}

	return t.(T), args[0], nil
}
