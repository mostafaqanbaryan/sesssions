package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	proj := t.TempDir()
	r, err := Render(proj, "my-session", "services:\n  app:\n    image: alpine\n    user: \"{uid}\"\n    container_name: {base}_${CONTAINER_NAME}\n", map[string]string{"FOO": "bar"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	data, err := os.ReadFile(r.File)
	if err != nil {
		t.Fatalf("read rendered file: %v", err)
	}
	content := string(data)

	if _, err := os.Stat(r.File); err != nil {
		t.Fatalf("rendered file missing: %v", err)
	}
	// {base} -> project base name.
	if !strings.Contains(content, filepath.Base(proj)+"_${CONTAINER_NAME}") {
		t.Fatalf("expected {base} substitution:\n%s", content)
	}
	// {uid} -> numeric uid.
	if !strings.Contains(content, `user: "`+uidStr()+`"`) {
		t.Fatalf("expected {uid} substitution:\n%s", content)
	}
}

// Render always sets CONTAINER_NAME so ${CONTAINER_NAME} never stays blank.
func TestRenderSetsContainerName(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	r, err := Render(t.TempDir(), "my-session", "services:\n  a:\n    image: alpine\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	env, err := os.ReadFile(r.Env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(env), "CONTAINER_NAME=my-session\n") {
		t.Fatalf("expected CONTAINER_NAME in env file:\n%s", env)
	}
	// Explicit value wins.
	r2, err := Render(t.TempDir(), "s", "services:\n  a:\n    image: alpine\n", map[string]string{"CONTAINER_NAME": "custom"})
	if err != nil {
		t.Fatal(err)
	}
	env2, _ := os.ReadFile(r2.Env)
	if !strings.Contains(string(env2), "CONTAINER_NAME=custom\n") {
		t.Fatalf("expected custom CONTAINER_NAME:\n%s", env2)
	}
}

// Render substitutes {go_dlv_port} from the environment for configs generated
// before the placeholder moved to ${go_dlv_port}.
func TestRenderSubstitutesGoDlvPort(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	r, err := Render(t.TempDir(), "s", "services:\n  a:\n    image: alpine\n    ports:\n      - \"{go_dlv_port}:2345\"\n", map[string]string{"go_dlv_port": "40123"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(r.File)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"40123:2345"`) {
		t.Fatalf("expected {go_dlv_port} resolved from env:\n%s", data)
	}
	if strings.Contains(string(data), "{go_dlv_port}") {
		t.Fatalf("placeholder left unresolved:\n%s", data)
	}
}

// fakeDocker puts a no-op `docker` on PATH that records its arguments to a log
// file, so Up/Down can be exercised without a real docker daemon.
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

// The whole render + up path must invoke docker compose with --project-directory
// pointing at the real project, so relative paths like ./src/.env resolve
// against the project (not the cache dir where the compose file lives).
func TestUpUsesProjectDirectory(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	logFile := fakeDocker(t)

	proj := t.TempDir()
	if err := os.MkdirAll(filepath.Join(proj, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	compose := "services:\n  app:\n    image: alpine\n    env_file:\n      - ./src/.env\n"
	r, err := Render(proj, "my-session", compose, map[string]string{"go_dlv_port": "40123"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Up(r); err != nil {
		t.Fatal(err)
	}

	args, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	for _, want := range []string{
		"compose", "--project-directory", proj, "--env-file", r.Env, "-f", r.File, "up", "-d", "--build",
	} {
		if len(lines) == 0 || lines[0] != want {
			t.Fatalf("docker args mismatch, missing/misplaced %q:\n%s", want, args)
		}
		lines = lines[1:]
	}
	if len(lines) != 0 {
		t.Fatalf("unexpected extra docker args:\n%s", args)
	}
}

func TestUpPropagatesDockerFailure(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	r, err := Render(t.TempDir(), "s", "services:\n  a:\n    image: alpine\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Up(r); err == nil {
		t.Fatal("expected error when docker fails")
	}
}

// Relative build contexts, env files and bind mounts are rewritten to absolute
// paths so docker compose resolves them against the project, never the cache
// dir where the rendered file lives.
func TestRenderAbsolutizesPaths(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	proj := t.TempDir()

	compose := `services:
  app:
    build: .
    env_file:
      - ./src/.env
    volumes:
      - ./.go-mod-cache:/go/pkg/mod
      - ./src:/app:ro
      - /abs/path:/x
      - named_vol:/data
      - ~/cache:/home/cache
  db:
    build:
      context: ../compose/db
      dockerfile: Dockerfile.db
    env_file: ./.env
    volumes:
      - type: bind
        source: ./data
        target: /var/lib/data
`
	r, err := Render(proj, "s", compose, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(r.File)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	abs := func(p string) string { return filepath.Join(proj, p) }
	for _, want := range []string{
		abs("."),
		abs("src/.env"),
		abs(".go-mod-cache") + ":/go/pkg/mod",
		abs("src") + ":/app:ro",
		"/abs/path:/x",
		"named_vol:/data",
		"~/cache:/home/cache",
		abs("../compose/db"),
		abs(".env"),
		abs("data"),
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing resolved path %q:\n%s", want, content)
		}
	}
	// No path got joined twice.
	if strings.Contains(content, filepath.Join(proj, filepath.Join(proj, ""))) {
		t.Fatalf("path double-joined:\n%s", content)
	}
}

func TestRenderInvalidYAML(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	_, err := Render(t.TempDir(), "x", "services: [unclosed", nil)
	if err == nil {
		t.Fatal("expected error for invalid YAML compose")
	}
}

func TestRenderEmpty(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if _, err := Render(t.TempDir(), "x", "   ", nil); err == nil {
		t.Fatal("expected error for empty compose")
	}
}

func TestRenderWritesOutsideProject(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	proj := t.TempDir()
	r, err := Render(proj, "s", "services:\n  a:\n    image: alpine\n", nil)
	if err != nil {
		t.Fatal(err)
	}

	// The rendered file must not live inside the project directory.
	if strings.HasPrefix(r.File, proj+string(os.PathSeparator)) {
		t.Fatalf("rendered file lives inside the project: %s", r.File)
	}
}

// A service with a build block and no explicit image is switched to the
// already-built image for projectName; services with an explicit image are
// untouched.
func TestReuseBuildImages(t *testing.T) {
	content := `services:
  app:
    build:
      context: .
      dockerfile: Dockerfile.dev
    container_name: ${CONTAINER_NAME}
  php:
    image: php:8.3-fpm
  nginx:
    build: .
`
	out, err := ReuseBuildImages(content, "myapp")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "image: myapp_app") {
		t.Fatalf("expected app to reuse myapp_app:\n%s", out)
	}
	if !strings.Contains(out, "image: myapp_nginx") {
		t.Fatalf("expected nginx to reuse myapp_nginx:\n%s", out)
	}
	if strings.Contains(out, "build:") {
		t.Fatalf("build blocks should be removed:\n%s", out)
	}
	if strings.Contains(out, "image: myapp_php") {
		t.Fatalf("service with explicit image must not change:\n%s", out)
	}
}

func uidStr() string {
	return strings.TrimSpace(uidString())
}

func uidString() string {
	b, _ := os.ReadFile("/proc/self/status")
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			f := strings.Fields(line)
			if len(f) > 1 {
				return f[1]
			}
		}
	}
	return "?"
}
