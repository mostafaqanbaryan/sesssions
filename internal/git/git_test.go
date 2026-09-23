package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHasChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := &Git{}
	changed, err := g.HasChanges(dir)
	if err != nil {
		t.Fatalf("HasChanges: %v", err)
	}
	if !changed {
		t.Fatal("untracked file should count as a change")
	}

	run("add", "a.txt")
	run("commit", "-m", "init")
	changed, err = g.HasChanges(dir)
	if err != nil {
		t.Fatalf("HasChanges: %v", err)
	}
	if changed {
		t.Fatal("clean worktree should have no changes")
	}

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err = g.HasChanges(dir)
	if err != nil {
		t.Fatalf("HasChanges: %v", err)
	}
	if !changed {
		t.Fatal("modified file should count as a change")
	}
}

// A dirty worktree cannot be removed without force, and --force drops it
// together with its uncommitted changes.
func TestDeleteWorktreeForce(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	base := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(base, "init")
	if err := os.WriteFile(filepath.Join(base, "a.txt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(base, "add", "a.txt")
	run(base, "commit", "-m", "init")

	wt := filepath.Join(t.TempDir(), "wt")
	run(base, "worktree", "add", "-b", "feature", wt)
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := &Git{}
	if err := g.DeleteWorktree(base, wt, false); err == nil {
		t.Fatal("expected non-force removal of a dirty worktree to fail")
	}
	if err := g.DeleteWorktree(base, wt, true); err != nil {
		t.Fatalf("force remove: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir should be gone, stat err = %v", err)
	}
}

func TestCopyIgnoredFiles(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	files := map[string]string{
		".gitignore":             ".env\nsrc/.env\nnode_modules/\n.envrc\n*.log\n",
		".env":                   "x=1\n",
		"src/.env":               "y=2\n",
		"src/app.go":             "package main\n",
		"node_modules/pkg/ix.js": "1\n",
		".envrc":                 "export go_dlv_port=1111\nexport FOO=1\n",
		"debug.log":              "1\n",
		"keep.txt":               "tracked file, not in .gitignore\n",
	}
	for path, content := range files {
		fp := filepath.Join(base, path)
		if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	g := &Git{}
	n, err := g.CopyIgnoredFiles(base, dest)
	if err != nil {
		t.Fatalf("CopyIgnoredFiles: %v", err)
	}

	want := []string{".env", "src/.env", ".envrc", "debug.log", "node_modules/pkg/ix.js"}
	if n != len(want) {
		t.Fatalf("copied %d files, want %d", n, len(want))
	}
	for _, p := range want {
		if _, err := os.Stat(filepath.Join(dest, p)); err != nil {
			t.Fatalf("expected %s to be copied: %v", p, err)
		}
	}
	for _, p := range []string{"src/app.go", "keep.txt"} {
		if _, err := os.Stat(filepath.Join(dest, p)); !os.IsNotExist(err) {
			t.Fatalf("%s should not have been copied", p)
		}
	}
}

func TestCopyIgnoredFilesNoGitignore(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "app.go"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := &Git{}
	n, err := g.CopyIgnoredFiles(base, dest)
	if err != nil {
		t.Fatalf("CopyIgnoredFiles: %v", err)
	}
	if n != 0 {
		t.Fatalf("copied %d files, want 0", n)
	}
}

func TestCopyIgnoredFilesNegation(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(base, ".gitignore"), []byte("*.log\n!keep.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "debug.log"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "keep.log"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := &Git{}
	n, err := g.CopyIgnoredFiles(base, dest)
	if err != nil {
		t.Fatalf("CopyIgnoredFiles: %v", err)
	}
	if n != 1 {
		t.Fatalf("copied %d files, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(dest, "debug.log")); err != nil {
		t.Fatal("debug.log should have been copied")
	}
	if _, err := os.Stat(filepath.Join(dest, "keep.log")); !os.IsNotExist(err) {
		t.Fatal("keep.log should not have been copied")
	}
}
