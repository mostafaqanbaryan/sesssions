package session

import (
	"os"
	"strings"
	"testing"

	"github.com/mostafaqanbaryan/sesssions/internal/config"
)

// TestListProjectModes verifies the trailing-slash semantics: a project root
// ending in "/" expands to its subdirectories; one without "/" shows itself.
func TestListProjectModes(t *testing.T) {
	D := t.TempDir()
	t.Setenv("HOME", D)
	mkdir(t, D+"/Projects/projA")
	mkdir(t, D+"/Projects/projB")
	mkdir(t, D+"/dotfiles")

	cfg := &config.Config{Projects: []string{D + "/Projects/", D + "/dotfiles"}}
	s := NewSession(cfg)
	rows, err := s.List(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Children of ~/Projects/ are listed.
	for _, name := range []string{"projA", "projB"} {
		if !hasName(rows, name) {
			t.Errorf("missing child %q in:\n%s", name, strings.Join(rows, "\n"))
		}
	}
	// dotfiles (no trailing slash) appears as itself.
	if !hasName(rows, "dotfiles") {
		t.Errorf("missing dotfiles row in:\n%s", strings.Join(rows, "\n"))
	}
	// Projects itself must never appear, only its children.
	if hasName(rows, "Projects") {
		t.Errorf("Projects dir itself appeared in:\n%s", strings.Join(rows, "\n"))
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

// hasName reports whether any row has a token equal to name (a column value).
func hasName(rows []string, name string) bool {
	for _, r := range rows {
		for _, f := range strings.Fields(r) {
			if f == name {
				return true
			}
		}
	}
	return false
}