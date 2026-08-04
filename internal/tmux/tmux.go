package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/mostafaqanbaryan/sesssions/internal/domain"
)

type Tmux struct{}

func NewTmux() *Tmux {
	if _, err := exec.LookPath("tmux"); err != nil {
		panic("tmux is required")
	}

	return &Tmux{}
}

func (t Tmux) CreateSession(selected domain.SearchItem) error {
	if err := t.hasSession(selected.GetSessionName()); err != nil {
		return fmt.Errorf("has session error: %w", err)
	}

	cmd := exec.Command("tmux", "new-session", "-ds", selected.GetSessionName(), "-c", selected.GetFullPath())
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("something wrong: %w", err)
	}

	options := exec.Command("tmux", "set-option", "-t", selected.GetSessionName(), "@root", selected.GetFullPath())
	options.Stderr = os.Stderr
	if err := options.Run(); err != nil {
		return fmt.Errorf("something wrong: %w", err)
	}

	return nil
}

func (t Tmux) CloseSession(sessionName string) error {
	// if err := t.hasSession(sessionName); err != nil {
	// 	return fmt.Errorf("has session error: %w", err)
	// }

	cmd := exec.Command("tmux", "kill-session", "-t", sessionName)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("something wrong: %w", err)
	}

	return nil
}

func (t Tmux) AttachSession(selected domain.SearchItem) error {
	cmd := exec.Command("tmux", "switch-client", "-t", selected.GetSessionName())
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("something wrong: %w", err)
	}

	return nil
}

func (t Tmux) ListSessions() []domain.SearchDirectoryItem {
	var list []domain.SearchDirectoryItem
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_attached}\t#S\t#{@root}")
	cmd.Stdout = nil
	cmd.Stderr = nil

	out, err := cmd.Output()
	if err != nil {
		panic(err)
	}

	for line := range strings.SplitSeq(string(out), "\n") {
		params := strings.Split(line, "\t")
		if len(params) != 3 {
			continue
		}

		list = append(list, domain.NewSearchDirectoryItem(params[1], path.Dir(params[2]), domain.SearchItemKindSession))
	}

	return list
}

func (t Tmux) hasSession(name string) error {
	cmd := exec.Command("tmux")
	cmd.Args = append(cmd.Args, "has-session", "-t", name)
	cmd.Stdout = nil
	cmd.Stderr = nil

	cmd.Run()

	if cmd.ProcessState.ExitCode() == 0 {
		return domain.ErrSessionExists
	}

	return nil
}
