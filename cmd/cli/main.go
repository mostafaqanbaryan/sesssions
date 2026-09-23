package main

import (
	"fmt"
	"os"

	"github.com/mostafaqanbaryan/sesssions/internal/app"
	"github.com/mostafaqanbaryan/sesssions/internal/config"
	"github.com/mostafaqanbaryan/sesssions/internal/git"
	"github.com/mostafaqanbaryan/sesssions/internal/session"
	"github.com/mostafaqanbaryan/sesssions/internal/tmux"
)

// Version is the sesssions release version. Override at build time with
// -ldflags "-X main.Version=x.y.z".
var Version = "0.1.0"

func main() {
	multiplexer := tmux.NewTmux()
	g := git.NewGit()
	a := app.NewApp(nil, nil, multiplexer, g)

	if len(os.Args) < 2 {
		a.HelpCommand()
		os.Exit(2)
	}

	// `version` must work before the config is required.
	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Printf("sesssions %s\n", Version)
		os.Exit(0)
	}

	// Load the global config. `init` may bootstrap it first if absent.
	if os.Args[1] == "init" {
		if _, _, err := config.Init(); err != nil {
			exitWithError(err)
		}
	}

	c, err := config.Load("")
	if err != nil {
		exitWithError(err)
	}
	if c == nil {
		exitWithError(fmt.Errorf("config not found, run `sesssions init` first"))
	}

	s := session.NewSession(c)
	a = app.NewApp(c, s, multiplexer, g)

	switch os.Args[1] {
	case "help":
		a.HelpCommand()
		os.Exit(2)
	case "init":
		if err := a.InitCommand(); err != nil {
			exitWithError(err)
		}
	case "list":
		if err := a.ListCommand(); err != nil {
			exitWithError(err)
		}
	case "up":
		dir := "."
		if len(os.Args) >= 3 {
			dir = os.Args[2]
		}
		if err := a.UpCommand(dir); err != nil {
			exitWithError(err)
		}
	case "down":
		dir := "."
		if len(os.Args) >= 3 {
			dir = os.Args[2]
		}
		if err := a.DownCommand(dir); err != nil {
			exitWithError(err)
		}
	case "branches":
		if len(os.Args) < 3 {
			a.HelpCommand()
			os.Exit(2)
		}
		if err := a.BranchesCommand(os.Args[2]); err != nil {
			exitWithError(err)
		}
	case "setup-worktree":
		if len(os.Args) < 4 {
			a.HelpCommand()
			os.Exit(2)
		}
		if err := a.SetupWorktreeCommand(os.Args[2], os.Args[3]); err != nil {
			exitWithError(err)
		}
	case "add-worktree":
		if len(os.Args) < 4 {
			a.HelpCommand()
			os.Exit(2)
		}
		if err := a.AddWorktreeByParamsCommand(os.Args[2], os.Args[3]); err != nil {
			exitWithError(err)
		}
	case "delete-worktree":
		if len(os.Args) < 3 {
			a.HelpCommand()
			os.Exit(2)
		}
		if err := a.DeleteWorktreeByPathCommand(os.Args[2]); err != nil {
			exitWithError(err)
		}
	}
	exitGracefully()
}

func exitWithError(err error) {
	fmt.Println(err)
	os.Exit(1)
}

func exitGracefully() {
	os.Exit(0)
}
