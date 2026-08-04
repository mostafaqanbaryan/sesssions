package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"

	"github.com/mostafaqanbaryan/sesssions/internal/config"
	"github.com/mostafaqanbaryan/sesssions/internal/domain"
	"github.com/mostafaqanbaryan/sesssions/internal/fzf"
	"github.com/mostafaqanbaryan/sesssions/internal/git"
	"github.com/mostafaqanbaryan/sesssions/internal/session"
	"github.com/mostafaqanbaryan/sesssions/internal/tmux"
)

type App struct {
	path   string
	config *config.Config
	// searcher    Searcher
	session     *session.Session
	multiplexer *tmux.Tmux
	git         *git.Git
}

type Searcher[T any] interface {
	NewWindow() domain.Window[T]
}

func NewApp(cfg *config.Config, session *session.Session, multiplexer *tmux.Tmux, git *git.Git) *App {
	exec, err := os.Executable()
	if err != nil {
		panic("executable not found")
	}

	return &App{
		path:    exec,
		git:     git,
		config:  cfg,
		session: session,
		// searcher:    searcher,
		multiplexer: multiplexer,
	}
}

func (a *App) ListCommand() error {
	sessions := a.multiplexer.ListSessions()
	searcher := fzf.NewFzf[domain.SearchDirectoryItem]()
	window := searcher.NewWindow()

	rows, err := a.session.List(sessions)
	if err != nil {
		return err
	}

	window.BindKey("Ctrl-A", "Add Worktree")
	window.BindKey("Ctrl-X", "Delete Worktree")

	window.Preview("ls -1 {5}")
	window.ShowColumns("1,2,3,4")
	selected, command, err := window.Display(rows)
	if err != nil && !errors.Is(err, domain.ErrEmptySelection) {
		return err
	}

	switch command {
	case "ctrl-a":
		return a.BranchesCommand(selected.GetFullPath())
	case "ctrl-x":
		return a.DeleteWorktreeCommand(selected)
	default:
		if err := a.multiplexer.CreateSession(selected); err != nil && !errors.Is(err, domain.ErrSessionExists) {
			return err
		}

		if err := a.multiplexer.AttachSession(selected); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) BranchesCommand(cwd string) error {
	searcher := fzf.NewFzf[domain.SearchBranchItem]()
	window := searcher.NewWindow()
	window.Preview("git log -n 10 {2}")
	window.ShowColumns("1,2")
	window.Cwd(cwd)

	branches, err := a.git.Branches(cwd)
	if err != nil {
		return err
	}

	rows := make([]string, 0, len(branches))
	for _, b := range branches {
		kind := "Local"
		if b.Remote {
			kind = "Remote"
		}
		rows = append(rows, fmt.Sprintf("[%s] \t%s\t%s", kind, b.Name, cwd))
	}

	selected, _, err := window.Display(rows)
	if err != nil {
		return err
	}

	if err := a.AddWorktreeCommand(cwd, selected); err != nil {
		return err
	}

	return nil
}

func (a *App) AddWorktreeCommand(cwd string, branch domain.SearchBranchItem) error {
	if branch.IsRemote() {
		if err := a.git.CreateLocalBranch(cwd, branch.GetBranchName()); err != nil {
			return err
		}
	}

	// envrc1 := path.Join(cwd, ".envrc")
	// f1, err := os.Open(envrc1)
	// if err != nil {
	// 	return err
	// }
	// defer f1.Close()
	// content1, err := io.ReadAll(f1)
	// if err != nil {
	// 	return err
	// }
	// newContent1 := regexp.MustCompile(`export CONTAINER_NAME=.+`).ReplaceAllString(string(content1), "export CONTAINER_NAME="+branch.GetSessionName())
	// fmt.Println(newContent1)
	//
	// return nil

	if err := a.git.CreateWorktree(cwd, branch.GetFullPath(), branch.GetBranchName()); err != nil {
		return err
	}

	if err := a.multiplexer.CreateSession(branch); err != nil && !errors.Is(err, domain.ErrSessionExists) {
		return err
	}

	// TODO: this should be moved to config
	if err := a.copyFileToWorktree(cwd, branch, "docker-compose.dev.yml"); err != nil {
		return err
	}
	if err := a.copyFileToWorktree(cwd, branch, "src/.env"); err != nil {
		return err
	}
	if err := a.copyFileToWorktree(cwd, branch, ".envrc"); err != nil {
		return err
	}
	envrc := path.Join(branch.GetFullPath(), ".envrc")
	f, err := os.Open(envrc)
	if err != nil {
		return err
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	newContent := regexp.MustCompile(`export CONTAINER_NAME=.+`).ReplaceAllString(string(content), "export CONTAINER_NAME="+branch.GetSessionName())
	fmt.Println(newContent)
	if err := os.WriteFile(envrc, []byte(newContent), 0o644); err != nil {
		return err
	}

	/////////

	// if err := a.multiplexer.RunCommandInSession(branch, a.config.PostWorktreeHook); err != nil {
	// 	return err
	// }

	if err := a.multiplexer.AttachSession(branch); err != nil {
		return err
	}

	return nil
}

func (a *App) DeleteWorktreeCommand(selected domain.SearchDirectoryItem) error {
	if err := a.multiplexer.CloseSession(selected.GetSessionName()); err != nil {
		return err
	}

	cwd, err := a.git.GetWorktreeBaseDir(selected.GetFullPath())
	if err != nil {
		return err
	}

	if err := a.git.DeleteWorktree(cwd, selected.GetFullPath()); err != nil {
		return err
	}

	return nil
}

func (a *App) HelpCommand() {
	fmt.Println("sesssions - sesssions is a cli tool to manage tmux sessions")
	fmt.Println("Usage:")
	fmt.Println("  sesssions [command]")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  add-worktree    [name]   Create a new worktree")
	fmt.Println("  delete-worktree [name]   Delete a worktree")
	fmt.Println("  branches        [name]   List git branches")
	fmt.Println("  list                     List all sessions")
	fmt.Println("  help                     Show this help")
}

func (a *App) copyFileToWorktree(cwd string, branch domain.SearchBranchItem, filename string) error {
	f, err := os.Open(path.Join(cwd, filename))
	if err != nil {
		return err
	}
	defer f.Close()

	dest, err := os.Create(path.Join(branch.GetFullPath(), filename))
	if err != nil {
		return err
	}
	defer dest.Close()

	_, err = io.Copy(dest, f)
	if err != nil {
		return err
	}

	dest.Sync()
	return nil
}
