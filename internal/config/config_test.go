package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigTemplateParses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, created, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !created {
		t.Fatal("expected config to be created on first init")
	}

	want := filepath.Join(dir, "sesssions", ConfigFileName)
	if path != want {
		t.Fatalf("config path = %q, want %q", path, want)
	}

	if _, err := Load(dir + "/sesssions"); err != nil {
		t.Fatalf("generated config does not parse: %v", err)
	}
}

func TestInitIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if _, created, err := Init(); err != nil || !created {
		t.Fatalf("first init: created=%v err=%v", created, err)
	}

	path, _, err := Init()
	if err != nil {
		t.Fatalf("second init: %v", err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// The user edits their config; init must not overwrite it.
	mutated := append(before, []byte("\n# my comment\n")...)
	if err := os.WriteFile(path, mutated, 0o644); err != nil {
		t.Fatal(err)
	}

	_, created, err := Init()
	if err != nil {
		t.Fatalf("third init: %v", err)
	}
	if created {
		t.Fatal("init must not recreate an existing config")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(mutated) {
		t.Fatalf("existing config was overwritten:\ngot  %q\nwant %q", got, mutated)
	}
}

func TestLoadExpandsHomeInProjects(t *testing.T) {
	home := t.TempDir()
	homeBackup := os.Getenv("HOME")
	t.Setenv("HOME", home)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(cfgPath, []byte("projects:\n  - \"~\"\n  - \"~/workspace\"\n  - \"/abs/path/\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer t.Setenv("HOME", homeBackup)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []string{home, filepath.Join(home, "workspace"), "/abs/path/"}
	for i, p := range cfg.Projects {
		if p != want[i] {
			t.Errorf("projects[%d] = %q, want %q", i, p, want[i])
		}
	}
}

// expandHome must keep the trailing-slash signal on ~-prefixed paths, since
// the session list uses it to distinguish "scan children" from "show itself".
func TestExpandHomeKeepsTrailingSlash(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cases := []struct {
		in   string
		want string
	}{
		{"~/Projects/", home + "/Projects/"},
		{"~/Projects", home + "/Projects"},
		{"~", home},
		{"/root/Projects/", "/root/Projects/"},
	}
	for _, c := range cases {
		if got := expandHome(c.in); got != c.want {
			t.Errorf("expandHome(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLoadPromotesLegacyComposeFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ConfigFileName)
	legacy := `languages:
  go:
    files:
      docker-compose.dev.yml: |
        services:
          app:
            image: alpine
`
	if err := os.WriteFile(configPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Languages["go"].Compose; got == "" || !strings.Contains(got, "image: alpine") {
		t.Fatalf("legacy compose was not promoted: %q", got)
	}
}

// A legacy global config (no template_version) is refreshed in place: the
// bundled language templates are upgraded while projects and custom languages
// are preserved.
func TestInitRefreshesBundledTemplates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	legacy := `projects:
  - "/home/user/work"
languages:
  go:
    environment:
      DOMAIN: localhost
    compose: |
      services:
        app:
          build: .
          ports:
            - "{go_dlv_port}:2345"
  python:
    environment:
      PORT: 8000
    compose: |
      services:
        py:
          image: python:3.12
`
	cfgDir := filepath.Join(dir, "sesssions")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, ConfigFileName), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	_, created, err := Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !created {
		t.Fatal("expected legacy config to be refreshed")
	}

	cfg, err := Load(cfgDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Projects) != 1 || cfg.Projects[0] != "/home/user/work" {
		t.Fatalf("projects not preserved: %v", cfg.Projects)
	}
	goCompose := cfg.Languages["go"].Compose
	if !strings.Contains(goCompose, "Dockerfile.dev") || !strings.Contains(goCompose, "${go_dlv_port}:2345") {
		t.Fatalf("go template was not refreshed:\n%s", goCompose)
	}
	if got := cfg.Languages["python"].Compose; !strings.Contains(got, "python:3.12") {
		t.Fatalf("custom language was lost: %v", cfg.Languages["python"])
	}
}

func TestLoadMissingConfig(t *testing.T) {
	cfg, err := Load(t.TempDir())

	if err != nil {
		t.Fatalf("Load on missing config: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config when file is absent")
	}
}
