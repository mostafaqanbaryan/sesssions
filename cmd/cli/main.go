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

func main() {
	// searcher := fzf.NewFzf()
	multiplexer := tmux.NewTmux()
	c := config.NewConfig()
	s := session.NewSession(c)
	g := git.NewGit()
	// a := app.NewApp(c, s, searcher, multiplexer, g)
	a := app.NewApp(c, s, multiplexer, g)

	if len(os.Args) < 2 {
		a.HelpCommand()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "help":
		a.HelpCommand()
		os.Exit(2)
	case "list":
		if err := a.ListCommand(); err != nil {
			exitWithError(err)
		}
		exitGracefully()
	case "branches":
		if len(os.Args) < 3 {
			a.HelpCommand()
			os.Exit(2)
		}
		if err := a.BranchesCommand(os.Args[2]); err != nil {
			exitWithError(err)
		}
		exitGracefully()
		// case "add-worktree":
		// 	if len(os.Args) < 4 {
		// 		a.HelpCommand()
		// 		os.Exit(2)
		// 	}
		// 	if err := a.AddWorktreeCommand(os.Args[2], os.Args[3]); err != nil {
		// 		exitWithError(err)
		// 	}
		// 	exitGracefully()
		// case "delete-worktree":
		// 	if len(os.Args) < 3 {
		// 		a.HelpCommand()
		// 		os.Exit(2)
		// 	}
		// 	if err := a.DeleteWorktreeCommand(os.Args[2]); err != nil {
		// 		exitWithError(err)
		// 	}
		// 	exitGracefully()
	}
}

func exitWithError(err error) {
	fmt.Println(err)
	os.Exit(1)
}

func exitGracefully() {
	os.Exit(0)
}
