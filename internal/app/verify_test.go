package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mostafaqanbaryan/sesssions/internal/config"
	"github.com/mostafaqanbaryan/sesssions/internal/git"
	"github.com/mostafaqanbaryan/sesssions/internal/tmux"
)

func writeGlobalConfig(t *testing.T) *config.Config {
	t.Helper()
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	if err := os.MkdirAll(filepath.Join(cfgDir, "sesssions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "sesssions", "sesssions.yaml"), []byte(config.DefaultConfigTemplate), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load("")
	if err != nil || cfg == nil {
		t.Fatalf("load global config: %v", err)
	}
	return cfg
}

// A project that declares its language inherits the compose definition from
// the global config and renders it to a cache file.
func TestRenderProjectFromGlobalLanguage(t *testing.T) {
	cfg := writeGlobalConfig(t)

	proj := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(proj, config.ProjectConfigFileName),
		[]byte("language: go\ncompose: \"\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	a := NewApp(cfg, nil, &tmux.Tmux{}, &git.Git{})
	r, err := a.renderProject(proj)
	if err != nil {
		t.Fatalf("renderProject: %v", err)
	}

	if r.Cwd != proj {
		t.Fatalf("cwd = %q, want %q", r.Cwd, proj)
	}
	data, err := os.ReadFile(r.File)
	if err != nil {
		t.Fatalf("read rendered compose: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "services:") {
		t.Fatalf("rendered compose missing services:\n%s", content)
	}
	if !strings.Contains(content, "container_name: ${CONTAINER_NAME}") {
		t.Fatalf("rendered compose missing container_name:\n%s", content)
	}
	// Volumes, build context and env files are absolute, resolved against the
	// project dir.
	for _, want := range []string{
		filepath.Join(proj, ".go-mod-cache") + ":/go/pkg/mod",
		filepath.Join(proj, "src") + ":/app",
		filepath.Join(proj, "src", ".env"),
		filepath.Join(proj, "."),
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing project-resolved path %q:\n%s", want, content)
		}
	}
	// ${go_dlv_port} is left for docker compose to resolve from the env file.
	if !strings.Contains(content, `"${go_dlv_port}:2345"`) {
		t.Fatalf("expected ${go_dlv_port} port mapping:\n%s", content)
	}
}

// A project with its own inline compose overrides the global language template.
func TestRenderProjectUsesInlineCompose(t *testing.T) {
	cfg := writeGlobalConfig(t)

	proj := t.TempDir()
	inline := "services:\n  app:\n    image: alpine\n"
	if err := os.WriteFile(
		filepath.Join(proj, config.ProjectConfigFileName),
		[]byte("language: go\ncompose: |\n"+indentBlock(inline)+"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	a := NewApp(cfg, nil, &tmux.Tmux{}, &git.Git{})
	r, err := a.renderProject(proj)
	if err != nil {
		t.Fatalf("renderProject: %v", err)
	}
	data, _ := os.ReadFile(r.File)
	if !strings.Contains(string(data), "image: alpine") {
		t.Fatalf("expected inline compose to win:\n%s", data)
	}
	// The global language template must not leak into a project that defines
	// its own compose.
	if strings.Contains(string(data), "container_name: ${CONTAINER_NAME}") {
		t.Fatalf("global template leaked into project compose:\n%s", data)
	}
}

func TestRenderProjectMissingConfig(t *testing.T) {
	cfg := writeGlobalConfig(t)
	a := NewApp(cfg, nil, &tmux.Tmux{}, &git.Git{})

	proj := filepath.Join(t.TempDir(), "no-config")
	os.MkdirAll(proj, 0o755)

	if _, err := a.renderProject(proj); err == nil {
		t.Fatal("expected error for project without .sesssions.yaml")
	}
}

// fakeDocker puts a no-op `docker` on PATH that records its arguments to a log
// file, so UpCommand can be exercised without a real docker daemon.
func fakeDocker(t *testing.T) (logFile string) {
	t.Helper()
	bin := t.TempDir()
	logFile = filepath.Join(t.TempDir(), "args.log")
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\"; done > \"" + logFile + "\"\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logFile
}

// Whole logic: project config -> env merge -> go_dlv_port generation -> Render
// -> docker compose up, all verified end to end against a fake docker.
func TestUpCommandWholeFlow(t *testing.T) {
	cfg := writeGlobalConfig(t)
	logFile := fakeDocker(t)

	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, config.ProjectConfigFileName), []byte("language: go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewApp(cfg, nil, &tmux.Tmux{}, &git.Git{})
	if err := a.UpCommand(proj); err != nil {
		t.Fatalf("UpCommand: %v", err)
	}

	args, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	if len(lines) < 8 {
		t.Fatalf("unexpected docker args:\n%s", args)
	}
	if lines[0] != "compose" || lines[1] != "--project-directory" || lines[2] != proj {
		t.Fatalf("expected --project-directory %s:\n%s", proj, args)
	}
	if lines[3] != "--env-file" || lines[4] == "" {
		t.Fatalf("expected --env-file with a value:\n%s", args)
	}
	if lines[5] != "-f" || lines[6] == "" {
		t.Fatalf("expected -f with a compose file:\n%s", args)
	}

	// The fake docker saw the same env and compose files that Render produced.
	envData, err := os.ReadFile(lines[4])
	if err != nil {
		t.Fatalf("env file %s: %v", lines[4], err)
	}
	if !strings.Contains(string(envData), "go_dlv_port=") {
		t.Fatalf("expected go_dlv_port in env file:\n%s", envData)
	}

	// direnv file recorded the generated port.
	envrc, err := os.ReadFile(filepath.Join(proj, ".envrc"))
	if err != nil {
		t.Fatalf(".envrc missing: %v", err)
	}
	re := regexp.MustCompile(`^export go_dlv_port=(\d+)\n$`)
	m := re.FindStringSubmatch(string(envrc))
	if len(m) != 2 || !strings.Contains(string(envData), "go_dlv_port="+m[1]+"\n") {
		t.Fatalf(".envrc port must match env file:\n.envrc:\n%s", envrc)
	}
}

// Relative project paths are normalized to absolute before rendering, and
// template volumes stay ./-relative so docker compose resolves them against
// the project directory (via --project-directory), never the compose cache.
func TestRenderProjectNormalizesPaths(t *testing.T) {
	cfg := writeGlobalConfig(t)

	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, config.ProjectConfigFileName), []byte("language: go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, proj)
	if err != nil {
		t.Fatal(err)
	}

	a := NewApp(cfg, nil, &tmux.Tmux{}, &git.Git{})
	r, err := a.renderProject(rel)
	if err != nil {
		t.Fatalf("renderProject(%q): %v", rel, err)
	}
	if !filepath.IsAbs(r.Cwd) || filepath.Clean(r.Cwd) != proj {
		t.Fatalf("cwd = %q, want absolute %q", r.Cwd, proj)
	}
	data, err := os.ReadFile(r.File)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		filepath.Join(proj, ".go-mod-cache") + ":/go/pkg/mod",
		filepath.Join(proj, ".go-build-cache") + ":/.cache/go-build",
		filepath.Join(proj, "src") + ":/app",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing project-resolved volume %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "/../") || strings.Contains(content, "/./") {
		t.Fatalf("volume path has not been cleaned:\n%s", content)
	}
}

// The project's docker-compose.dev.yml is authoritative: init seeds it from a
// template, and renderProject uses only its content (not the global template).
func TestRenderProjectUsesDockerComposeDev(t *testing.T) {
	cfg := writeGlobalConfig(t)

	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, config.ProjectConfigFileName), []byte("language: go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	own := "services:\n  app:\n    image: my-custom-image\n"
	if err := os.WriteFile(filepath.Join(proj, config.DockerComposeDevFile), []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewApp(cfg, nil, &tmux.Tmux{}, &git.Git{})
	r, err := a.renderProject(proj)
	if err != nil {
		t.Fatalf("renderProject: %v", err)
	}
	data, err := os.ReadFile(r.File)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "image: my-custom-image") {
		t.Fatalf("expected project compose to win:\n%s", content)
	}
	// The global go template must not leak in (env aside).
	if strings.Contains(content, "container_name: ${CONTAINER_NAME}") {
		t.Fatalf("global template leaked into project compose:\n%s", content)
	}
}

// initCompose replaces {uid} with the current user id.
func TestInitComposeSubstitutesUID(t *testing.T) {
	out := initCompose("services:\n  app:\n    user: \"{uid}\"\n")
	uid := strconv.Itoa(os.Getuid())
	if !strings.Contains(out, `user: "`+uid+`"`) {
		t.Fatalf("expected {uid} resolved to %s:\n%s", uid, out)
	}
	if strings.Contains(out, "{uid}") {
		t.Fatalf("placeholder left unresolved:\n%s", out)
	}
}

func TestConfirmReplace(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty enter", "\n", false},
		{"n", "n\n", false},
		{"N", "N\n", false},
		{"no", "no\n", false},
		{"y", "y\n", true},
		{"Y", "Y\n", true},
		{"yes", "yes\n", true},
		{"yes with space", " yes \n", true},
		{"eof", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := confirmReplace(strings.NewReader(tc.in), "/tmp/x"); got != tc.want {
				t.Fatalf("confirmReplace(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func indentBlock(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		b.WriteString("      " + line + "\n")
	}
	return b.String()
}

// renderProject generates go_dlv_port when the project does not define one,
// prints it, and records it in .envrc.
func TestRenderProjectGeneratesGoDlvPort(t *testing.T) {
	cfg := writeGlobalConfig(t)

	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, config.ProjectConfigFileName), []byte("language: go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewApp(cfg, nil, &tmux.Tmux{}, &git.Git{})
	r, err := a.renderProject(proj)
	if err != nil {
		t.Fatalf("renderProject: %v", err)
	}

	envData, err := os.ReadFile(r.Env)
	if err != nil {
		t.Fatal(err)
	}
	envContent := string(envData)
	m := regexp.MustCompile(`(?m)^go_dlv_port=(\d+)\n`).FindStringSubmatch(envContent)
	if len(m) != 2 {
		t.Fatalf("expected go_dlv_port in env file:\n%s", envContent)
	}
	if m[1] == "" {
		t.Fatal("go_dlv_port is empty")
	}

	envrc, err := os.ReadFile(filepath.Join(proj, ".envrc"))
	if err != nil {
		t.Fatalf(".envrc missing: %v", err)
	}
	if string(envrc) != "export go_dlv_port="+m[1]+"\n" {
		t.Fatalf("unexpected .envrc content:\n%s", envrc)
	}

	// The rendered compose references ${go_dlv_port} so docker compose resolves
	// it from the env file.
	composeData, _ := os.ReadFile(r.File)
	if !strings.Contains(string(composeData), "\"${go_dlv_port}:2345\"") {
		t.Fatalf("expected ${go_dlv_port} port mapping:\n%s", composeData)
	}
}

// An explicitly defined go_dlv_port in .sesssions.yaml wins and no .envrc is
// written.
func TestRenderProjectUsesDefinedGoDlvPort(t *testing.T) {
	cfg := writeGlobalConfig(t)

	proj := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(proj, config.ProjectConfigFileName),
		[]byte("language: go\nenvironment:\n  go_dlv_port: \"34567\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	a := NewApp(cfg, nil, &tmux.Tmux{}, &git.Git{})
	r, err := a.renderProject(proj)
	if err != nil {
		t.Fatalf("renderProject: %v", err)
	}

	envData, _ := os.ReadFile(r.Env)
	if !strings.Contains(string(envData), "go_dlv_port=34567\n") {
		t.Fatalf("expected defined port in env file:\n%s", envData)
	}
	if _, err := os.Stat(filepath.Join(proj, ".envrc")); !os.IsNotExist(err) {
		t.Fatal("expected no .envrc when go_dlv_port is defined")
	}
}

// An existing .envrc value is reused instead of generating a new port.
func TestEnsureGoDlvPortReusesEnvrc(t *testing.T) {
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, ".envrc"), []byte("export OTHER=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	ensureGoDlvPort(env, proj)
	if _, ok := env[goDlvPortEnv]; !ok {
		t.Fatal("expected go_dlv_port to be set")
	}
	freePort := env[goDlvPortEnv]
	if _, err := strconv.Atoi(freePort); err != nil {
		t.Fatalf("generated port looks wrong: %s", freePort)
	}

	env2 := map[string]string{}
	ensureGoDlvPort(env2, proj)
	if env2[goDlvPortEnv] != freePort {
		t.Fatalf("port changed between runs: %s != %s", freePort, env2[goDlvPortEnv])
	}
}

// setWorktreeEnvrc drops a copied go_dlv_port (so the worktree gets its own)
// and pins CONTAINER_NAME to the worktree's project__branch name.
func TestSetWorktreeEnvrcResetsPortAndSetsContainerName(t *testing.T) {
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, ".envrc"), []byte("export OTHER=1\nexport go_dlv_port=1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewApp(nil, nil, &tmux.Tmux{}, &git.Git{})
	if err := a.setWorktreeEnvrc(proj, "myapp__feature"); err != nil {
		t.Fatalf("setWorktreeEnvrc: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(proj, ".envrc"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if strings.Contains(content, goDlvPortEnv) {
		t.Fatalf("go_dlv_port should be removed:\n%s", content)
	}
	if !strings.Contains(content, "export OTHER=1") {
		t.Fatalf("unrelated entries should be preserved:\n%s", content)
	}
	if !strings.Contains(content, "export "+containerNameEnv+"=myapp__feature") {
		t.Fatalf("CONTAINER_NAME should be set:\n%s", content)
	}
}

func TestSetWorktreeEnvrcMissing(t *testing.T) {
	proj := t.TempDir()
	a := NewApp(nil, nil, &tmux.Tmux{}, &git.Git{})
	if err := a.setWorktreeEnvrc(proj, "myapp__feature"); err != nil {
		t.Fatalf("setWorktreeEnvrc without .envrc: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(proj, ".envrc"))
	if err != nil {
		t.Fatalf("expected .envrc to be created: %v", err)
	}
	if !strings.Contains(string(data), "export "+containerNameEnv+"=myapp__feature") {
		t.Fatalf("CONTAINER_NAME should be set:\n%s", data)
	}
}

// initWorktree seeds a fresh worktree with the base project's language so
// `sesssions up` has .sesssions.yaml and docker-compose.dev.yml to work with.
// A worktree renders its own compose but reuses the base project's images
// instead of building the Dockerfile.
func TestRenderProjectForWorktreeReusesBaseImage(t *testing.T) {
	cfg := writeGlobalConfig(t)

	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, config.ProjectConfigFileName), []byte("language: go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	worktree := filepath.Join(t.TempDir(), "myapp__feature")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree, config.ProjectConfigFileName), []byte("language: go\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewApp(cfg, nil, &tmux.Tmux{}, &git.Git{})
	r, err := a.renderProjectForWorktree(base, worktree)
	if err != nil {
		t.Fatalf("renderProjectForWorktree: %v", err)
	}
	data, err := os.ReadFile(r.File)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	wantImage := composeProjectName(base) + "_app"
	if !strings.Contains(content, "image: "+wantImage) {
		t.Fatalf("expected reused image %q:\n%s", wantImage, content)
	}
	if strings.Contains(content, "build:") {
		t.Fatalf("build blocks should be removed:\n%s", content)
	}
}

func TestComposeProjectName(t *testing.T) {
	cases := map[string]string{
		"MyApp":       "myapp",
		"my-app_dev":  "my-app_dev",
		"a.b_c-d":     "a.b_c-d",
		"My App (x)!": "myappx",
	}
	for in, want := range cases {
		if got := composeProjectName(in); got != want {
			t.Errorf("composeProjectName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInitWorktreeSeedsFromBaseLanguage(t *testing.T) {
	cfg := writeGlobalConfig(t)

	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, config.ProjectConfigFileName), []byte("language: go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(t.TempDir(), "proj__feature")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}

	a := NewApp(cfg, nil, &tmux.Tmux{}, &git.Git{})
	if err := a.initWorktree(base, worktree); err != nil {
		t.Fatalf("initWorktree: %v", err)
	}

	projCfg, err := config.LoadProjectConfig(worktree)
	if err != nil || projCfg == nil || projCfg.Language != "go" {
		t.Fatalf("worktree config = %+v, err = %v", projCfg, err)
	}
	data, err := os.ReadFile(filepath.Join(worktree, config.DockerComposeDevFile))
	if err != nil {
		t.Fatalf("docker-compose.dev.yml missing: %v", err)
	}
	if !strings.Contains(string(data), "container_name: ${CONTAINER_NAME}") {
		t.Fatalf("compose not seeded from template:\n%s", data)
	}
}

// CONTAINER_NAME recorded in a worktree's .envrc overrides the session-name
// fallback when rendering compose.
func TestRenderProjectUsesEnvrcContainerName(t *testing.T) {
	cfg := writeGlobalConfig(t)

	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, config.ProjectConfigFileName), []byte("language: go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".envrc"), []byte("export "+containerNameEnv+"=proj__feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := NewApp(cfg, nil, &tmux.Tmux{}, &git.Git{})
	r, err := a.renderProject(proj)
	if err != nil {
		t.Fatalf("renderProject: %v", err)
	}
	envData, err := os.ReadFile(r.Env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(envData), containerNameEnv+"=proj__feature\n") {
		t.Fatalf("expected .envrc container name in env file:\n%s", envData)
	}
}
