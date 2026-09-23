package git

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

func (g *Git) DeleteWorktree(cwd, worktreeDir string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreeDir)

	cmd := exec.Command("git", args...)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	cmd.Dir = cwd

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("something wrong: %w", err)
	}

	return nil
}

// HasChanges reports whether the worktree has uncommitted changes (staged,
// unstaged or untracked files), so callers can refuse to remove it.
func (g *Git) HasChanges(worktreeDir string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	cmd.Dir = worktreeDir

	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("something wrong: %w", err)
	}

	return strings.TrimSpace(out.String()) != "", nil
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

	base := strings.TrimSuffix(strings.TrimSpace(out.String()), "/.git")
	return base, nil
}

// CopyIgnoredFiles copies, from cwd into dest (a fresh worktree), the files and
// directories listed in cwd/.gitignore so non-tracked files (.env, .envrc, ...)
// are present in the new worktree. Returns the number of files copied.
func (g *Git) CopyIgnoredFiles(cwd, dest string) (int, error) {
	raw, err := os.ReadFile(filepath.Join(cwd, ".gitignore"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("read .gitignore: %w", err)
	}

	positives, negatives := parseIgnorePatterns(string(raw))
	if len(positives) == 0 {
		return 0, nil
	}

	count := 0
	err = filepath.WalkDir(cwd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == cwd {
			return nil
		}
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(os.PathSeparator)) {
			return fs.SkipDir
		}
		if !ignoreMatches(rel, positives) || ignoreMatches(rel, negatives) {
			return nil
		}
		dst := filepath.Join(dest, rel)
		if d.IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if err := copySymlink(path, dst); err != nil {
				return err
			}
			count++
			return nil
		}
		if err := copyFile(path, dst); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		return count, err
	}
	return count, nil
}

type ignorePattern struct {
	re       *regexp.Regexp
	matchRel bool
}

func parseIgnorePatterns(lines string) (positives, negatives []ignorePattern) {
	for _, line := range strings.Split(lines, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		neg := strings.HasPrefix(line, "!")
		if neg {
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
		}
		line = strings.TrimPrefix(line, "/")
		line = strings.TrimSuffix(line, "/")
		if line == "" || line == "*" || line == "**" {
			continue
		}
		p := compileIgnorePattern(line)
		if neg {
			negatives = append(negatives, p)
		} else {
			positives = append(positives, p)
		}
	}
	return positives, negatives
}

func compileIgnorePattern(pattern string) ignorePattern {
	p := regexp.QuoteMeta(pattern)
	p = strings.ReplaceAll(p, `\*\*`, `.*`)
	p = strings.ReplaceAll(p, `\*`, `[^/]*`)
	p = strings.ReplaceAll(p, `\?`, `[^/]`)
	return ignorePattern{
		re:       regexp.MustCompile(`^` + p + `$`),
		matchRel: strings.Contains(pattern, "/"),
	}
}

// ignoreMatches reports whether rel matches any of the given patterns, checking
// the pattern against the relative path itself and against every ancestor
// directory (so ignoring a directory ignores its contents).
func ignoreMatches(rel string, patterns []ignorePattern) bool {
	candidates := []string{rel}
	for dir := filepath.Dir(rel); dir != "." && dir != rel; dir = filepath.Dir(dir) {
		candidates = append(candidates, dir)
	}
	base := filepath.Base(rel)
	for _, p := range patterns {
		for _, c := range candidates {
			name := base
			if p.matchRel {
				name = c
			} else {
				name = filepath.Base(c)
			}
			if p.re.MatchString(name) {
				return true
			}
		}
	}
	return false
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func copySymlink(src, dst string) error {
	target, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Symlink(target, dst)
}
