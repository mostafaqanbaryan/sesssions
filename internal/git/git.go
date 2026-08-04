package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Git struct{}

type Branch struct {
	Name   string
	Remote bool
}

func NewGit() *Git {
	if _, err := exec.LookPath("git"); err != nil {
		panic("git is required")
	}

	return &Git{}
}

func (g *Git) Branches(cwd string) ([]Branch, error) {
	cmd := exec.Command("git", "branch", "-a")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	cmd.Dir = cwd

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("something wrong: %w", err)
	}

	branches := strings.SplitSeq(out.String(), "\n")

	var list []Branch
	added := make(map[string]struct{}, 100)
	for b := range branches {
		b = strings.TrimSpace(b)
		name := strings.TrimPrefix(b, "remotes/origin/")
		if _, duplcicated := added[name]; !duplcicated && name != "" {
			added[name] = struct{}{}
			list = append(list, Branch{
				Name:   name,
				Remote: name != b,
			})
		}
	}

	return list, nil
}

func (g *Git) CreateLocalBranch(cwd, branch string) error {
	cmd := exec.Command("git", "branch", "--track", branch, "origin/"+branch)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	cmd.Dir = cwd

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("something wrong: %w", err)
	}

	return nil
}

func (g *Git) CreateWorktree(cwd, worktreeDir, branch string) error {
	cmd := exec.Command("git", "worktree", "add", "-q", worktreeDir, branch)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	cmd.Dir = cwd

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("something wrong: %w", err)
	}

	return nil
}

func (g *Git) DeleteWorktree(cwd, worktreeDir string) error {
	cmd := exec.Command("git", "worktree", "remove", worktreeDir)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	cmd.Dir = cwd

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("something wrong: %w", err)
	}

	return nil
}

func (g *Git) GetWorktreeBaseDir(worktreeDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir", "--path-format=absolute")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	cmd.Dir = worktreeDir

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("something wrong: %w", err)
	}

	cwd := strings.TrimSuffix(strings.TrimSpace(out.String()), "/.git")
	return cwd, nil
}
