package app

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mostafaqanbaryan/sesssions/internal/compose"
	"github.com/mostafaqanbaryan/sesssions/internal/config"
	"github.com/mostafaqanbaryan/sesssions/internal/domain"
	"github.com/mostafaqanbaryan/sesssions/internal/fzf"
	"github.com/mostafaqanbaryan/sesssions/internal/git"
	"github.com/mostafaqanbaryan/sesssions/internal/session"
	"github.com/mostafaqanbaryan/sesssions/internal/tmux"
)

type App struct {
	path        string
	config      *config.Config
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
		path:        exec,
		git:         git,
		config:      cfg,
		session:     session,
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
	window.Prompt("Session ")
	window.ShowColumns("1,2,3,4")
	selected, command, err := window.Display(rows)
	if errors.Is(err, domain.ErrEmptySelection) {
		return nil
	}

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
	// Resolve a possibly-relative argument (e.g. `sesssions branches .`) so the
	// branch item's parent is absolute: session names and worktree paths are
	// derived from it, and a relative "." would otherwise produce a broken
	// `.__<branch>` session / a worktree in the wrong place.
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return err
	}
	cwd = absCwd

	searcher := fzf.NewFzf[domain.SearchBranchItem]()
	window := searcher.NewWindow()
	window.Preview("git log -n 10 {2}")
	window.ShowColumns("1,2")
	window.Prompt("Select a branch")
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

// InitCommand writes the selected project's language into .sesssions.yaml and
// seeds (or, with confirmation, replaces) the project's docker-compose.dev.yml
// from the language template in the global config. The language is picked from
// the languages defined in the global config.
func (a *App) InitCommand() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	searcher := fzf.NewFzf[domain.SearchLanguageItem]()
	window := searcher.NewWindow()
	window.Prompt("Select a language")
	window.Cwd(cwd)

	rows := make([]string, 0, len(a.config.Languages))
	for name := range a.config.Languages {
		rows = append(rows, name)
	}
	if len(rows) == 0 {
		return fmt.Errorf("no languages defined in the global config")
	}

	selected, _, err := window.Display(rows)
	if err != nil {
		return err
	}

	langName := selected.GetLanguage()

	composePath := filepath.Join(cwd, config.DockerComposeDevFile)
	existed := false
	replace := false
	if _, err := os.Stat(composePath); err == nil {
		existed = true
		if !confirmReplace(os.Stdin, composePath) {
			fmt.Printf("kept existing %s\n", composePath)
		} else {
			replace = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := a.initProject(cwd, langName, replace); err != nil {
		return err
	}

	switch {
	case replace:
		fmt.Printf("replaced %s with the %s template\n", composePath, langName)
	case !existed:
		fmt.Printf("created %s from the %s template\n", composePath, langName)
	}
	return nil
}

// initProject records langName in the project's .sesssions.yaml (preserving any
// existing environment / inline compose) and seeds docker-compose.dev.yml from
// the language's compose template. An existing compose file is kept unless
// replace is true. It is the non-interactive core shared by `sesssions init`
// and worktree setup.
func (a *App) initProject(cwd, langName string, replace bool) error {
	lang := a.languageConfig(langName)
	if lang == nil {
		return fmt.Errorf("language %q not found in the global config", langName)
	}
	if strings.TrimSpace(lang.Compose) == "" {
		return fmt.Errorf("language %q has no compose template in the global config", langName)
	}

	cfg, err := config.LoadProjectConfig(cwd)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &config.ProjectConfig{}
	}
	cfg.Language = langName
	if err := config.SaveProjectConfig(cwd, cfg); err != nil {
		return err
	}

	composePath := filepath.Join(cwd, config.DockerComposeDevFile)
	if _, err := os.Stat(composePath); err == nil && !replace {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Bake the current user id into the template so the committed compose file
	// is self-describing; Render still substitutes any unrendered {uid}.
	return os.WriteFile(composePath, []byte(initCompose(lang.Compose)), 0o644)
}

// initWorktree prepares a fresh worktree using the base repository's language,
// so `sesssions up` has a .sesssions.yaml and a docker-compose.dev.yml to work
// with. Worktree-local files (e.g. copied from .gitignore) are never replaced.
func (a *App) initWorktree(base, worktree string) error {
	langName := a.resolveLanguageName(base)
	if a.languageConfig(langName) == nil {
		return fmt.Errorf("could not determine the language of %s; run `sesssions init` there first", base)
	}

	// Inherit the base project's per-project overrides when the worktree has
	// no .sesssions.yaml of its own yet.
	if cfg, err := config.LoadProjectConfig(worktree); err == nil && cfg == nil {
		if baseCfg, err := config.LoadProjectConfig(base); err == nil && baseCfg != nil {
			baseCfg.Language = langName
			if err := config.SaveProjectConfig(worktree, baseCfg); err != nil {
				return err
			}
		}
	}

	return a.initProject(worktree, langName, false)
}

// initCompose prepares a language template for the project: the current user
// id replaces the {uid} placeholder.
func initCompose(compose string) string {
	return strings.ReplaceAll(compose, "{uid}", strconv.Itoa(os.Getuid()))
}

// confirmReplace asks the user whether an existing file may be overwritten.
// It returns true only on an explicit y/yes answer; anything else (including
// an empty enter) is treated as "no".
func confirmReplace(r io.Reader, path string) bool {
	fmt.Printf("%s already exists. Replace it with the new template? [y/N]: ", path)
	var answer string
	_, err := fmt.Fscanln(r, &answer)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}

// languageConfig returns the global config definition for a language label.
func (a *App) languageConfig(label string) *config.LanguageConfig {
	if a.config == nil || label == "" {
		return nil
	}
	lang, ok := a.config.Languages[strings.ToLower(label)]
	if !ok {
		return nil
	}
	return &lang
}

// resolveLanguageName returns the project's language declared in
// .sesssions.yaml, falling back to its base directory name.
func (a *App) resolveLanguageName(cwd string) string {
	if projectCfg, err := config.LoadProjectConfig(cwd); err == nil && projectCfg != nil && projectCfg.Language != "" {
		return projectCfg.Language
	}
	return filepath.Base(cwd)
}

// resolveLanguage returns the config.LanguageConfig matching the project's
// language declared in .sesssions.yaml (or its base directory name), or nil.
func (a *App) resolveLanguage(cwd string) *config.LanguageConfig {
	return a.languageConfig(a.resolveLanguageName(cwd))
}

// renderProject renders the docker-compose definition for a project into a
// cache file outside the repo.
//
// The project's docker-compose.dev.yml is the authoritative compose source:
// it is seeded by `sesssions init` from a language template and may be edited
// freely. The global language template only supplies default environment and
// a fallback compose for projects without the file.
func (a *App) renderProject(cwd string) (*compose.Rendered, error) {
	return a.renderProjectInternal(cwd, "")
}

// renderProjectForWorktree is like renderProject but reuses the base project's
// already-built docker images instead of rebuilding the Dockerfile.
func (a *App) renderProjectForWorktree(base, cwd string) (*compose.Rendered, error) {
	return a.renderProjectInternal(cwd, base)
}

func (a *App) renderProjectInternal(cwd, base string) (*compose.Rendered, error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}
	cwd = abs

	projectCfg, err := config.LoadProjectConfig(cwd)
	if err != nil {
		return nil, fmt.Errorf("load project config: %w", err)
	}
	if projectCfg == nil {
		return nil, fmt.Errorf("no .sesssions.yaml in %s (run `sesssions init` first)", cwd)
	}

	env := map[string]string{}
	if lang := a.resolveLanguage(cwd); lang != nil {
		env = mergeEnv(env, lang.Environment)
	}
	env = mergeEnv(env, projectCfg.Environment)
	ensureContainerName(env, cwd)

	var composeContent string
	if data, err := os.ReadFile(filepath.Join(cwd, config.DockerComposeDevFile)); err == nil {
		composeContent = string(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", config.DockerComposeDevFile, err)
	}

	// Backwards compatibility: projects without docker-compose.dev.yml fall
	// back to an inline compose in .sesssions.yaml, then the global template.
	if composeContent == "" {
		if projectCfg.Compose != "" {
			composeContent = projectCfg.Compose
		} else if lang := a.resolveLanguage(cwd); lang != nil {
			composeContent = lang.Compose
		}
	}

	// A worktree reuses the base project's images instead of rebuilding, so the
	// build blocks are replaced with the base's already-built image tags.
	if base != "" {
		projectName := composeProjectName(base)
		composeContent, err = compose.ReuseBuildImages(composeContent, projectName)
		if err != nil {
			return nil, fmt.Errorf("reuse base image: %w", err)
		}
	}

	// Make sure the Delve debug port is available in the environment.
	ensureGoDlvPort(env, cwd)

	if composeContent == "" {
		return nil, fmt.Errorf("no compose definition for %s: run `sesssions init` to create docker-compose.dev.yml", cwd)
	}

	sessionName := deriveSessionName(cwd)
	r, err := compose.Render(cwd, sessionName, composeContent, env)
	if err != nil {
		return nil, fmt.Errorf("render compose: %w", err)
	}
	return r, nil
}

// composeProjectName computes the docker compose project name that docker
// derives from a project dir (basename, lowercased, invalid characters
// dropped). It matches the image tag `<project>_<service>` built for the
// project's services.
func composeProjectName(dir string) string {
	base := strings.ToLower(filepath.Base(dir))
	re := regexp.MustCompile(`[^a-z0-9._-]`)
	return re.ReplaceAllString(base, "")
}

// mergeEnv returns b overlaid on a (project overrides global).
func mergeEnv(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// shellQuote wraps s in single quotes for safe use in a shell command sent to a
// tmux pane.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// goDlvPortEnv is the environment variable holding the host port mapped to the
// container's Delve debugger (port 2345).
const goDlvPortEnv = "go_dlv_port"

// containerNameEnv is the environment variable docker compose renders
// ${CONTAINER_NAME} from.
const containerNameEnv = "CONTAINER_NAME"

// ensureGoDlvPort makes go_dlv_port available in env. It is left untouched
// when already set by the config or the process environment. Otherwise a
// random free port is picked, printed, and recorded in the project's .envrc
// so direnv (and later runs) see a stable value.
func ensureGoDlvPort(env map[string]string, cwd string) {
	if _, ok := env[goDlvPortEnv]; ok {
		return
	}
	if v := os.Getenv(goDlvPortEnv); v != "" {
		env[goDlvPortEnv] = v
		return
	}
	if v := readEnvrcValue(cwd, goDlvPortEnv); v != "" {
		env[goDlvPortEnv] = v
		return
	}
	port := randomPort()
	env[goDlvPortEnv] = port
	fmt.Printf("go_dlv_port not defined; using %s and recorded it in %s/.envrc\n", port, cwd)
	if err := setEnvrcValue(cwd, goDlvPortEnv, port); err != nil {
		fmt.Printf("Warning: failed to write %s/.envrc: %v\n", cwd, err)
	}
}

// ensureContainerName makes CONTAINER_NAME available in env from the project's
// .envrc when neither the config nor the process environment already set it.
// This lets each worktree pin its own container name; when unset, compose.Render
// falls back to the session name.
func ensureContainerName(env map[string]string, cwd string) {
	if env[containerNameEnv] != "" {
		return
	}
	if v := os.Getenv(containerNameEnv); v != "" {
		env[containerNameEnv] = v
		return
	}
	if v := readEnvrcValue(cwd, containerNameEnv); v != "" {
		env[containerNameEnv] = v
	}
}

// randomPort returns a free TCP port for the Delve remote debugger, drawn
// from the 23450-49999 range.
func randomPort() string {
	const min, max = 23450, 49999
	span := big.NewInt(max - min + 1)
	for i := 0; i < 64; i++ {
		n, err := rand.Int(rand.Reader, span)
		if err != nil {
			continue
		}
		port := min + int(n.Int64())
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		ln.Close()
		return strconv.Itoa(port)
	}
	return strconv.Itoa(min)
}

// readEnvrcValue returns the value of an exported var in the project's .envrc,
// or "" when absent.
func readEnvrcValue(cwd, key string) string {
	data, err := os.ReadFile(filepath.Join(cwd, ".envrc"))
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`(?m)^\s*export\s+` + regexp.QuoteMeta(key) + `=([^\s]+)`)
	m := re.FindStringSubmatch(string(data))
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

// setEnvrcValue sets an exported var in the project's .envrc (direnv file),
// replacing an existing line or appending a new one.
func setEnvrcValue(cwd, key, value string) error {
	path := filepath.Join(cwd, ".envrc")
	line := "export " + key + "=" + value
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	re := regexp.MustCompile(`(?m)^\s*export\s+` + regexp.QuoteMeta(key) + `=.*$`)
	if re.Match(data) {
		return os.WriteFile(path, []byte(re.ReplaceAllString(string(data), line)), 0o644)
	}
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}
	data = append(data, []byte(line+"\n")...)
	return os.WriteFile(path, data, 0o644)
}

// UpCommand starts the project's stack. cwd is the project root.
func (a *App) UpCommand(cwd string) error {
	r, err := a.renderProject(cwd)
	if err != nil {
		return err
	}
	fmt.Printf("compose up for %s (%s)\n", r.Cwd, r.File)
	return compose.Up(r)
}

// DownCommand stops and removes the project's stack.
func (a *App) DownCommand(cwd string) error {
	r, err := a.renderProject(cwd)
	if err != nil {
		return err
	}
	fmt.Printf("compose down for %s (%s)\n", r.Cwd, r.File)
	return compose.Down(r)
}

// UpWorktreeCommand starts a worktree's stack reusing the base project's
// already-built docker images instead of rebuilding the Dockerfile.
func (a *App) UpWorktreeCommand(base, cwd string) error {
	r, err := a.renderProjectForWorktree(base, cwd)
	if err != nil {
		return err
	}
	fmt.Printf("compose up for %s (%s) reusing %s images\n", r.Cwd, r.File, composeProjectName(base))
	return compose.Up(r)
}

// deriveSessionName names a tmux/docker session for a project dir:
// the parent__base convention used for worktrees, otherwise the base name.
func deriveSessionName(cwd string) string {
	base := filepath.Base(cwd)
	re := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	return re.ReplaceAllString(strings.ToLower(base), "-")
}

func (a *App) AddWorktreeCommand(cwd string, branch domain.SearchBranchItem) error {
	if branch.IsRemote() {
		if err := a.git.CreateLocalBranch(cwd, branch.GetBranchName()); err != nil {
			return err
		}
	}

	worktreePath := branch.GetFullPath()

	if err := a.git.CreateWorktree(cwd, worktreePath, branch.GetBranchName()); err != nil {
		return err
	}

	if err := a.multiplexer.CreateSession(branch); err != nil && !errors.Is(err, domain.ErrSessionExists) {
		return err
	}

	// Run the setup (copy, init, envrc, up) inside the new pane so large
	// gitignored files and compose builds don't pause the CLI; the user is
	// switched into the session while it runs.
	setup := shellQuote(a.path) + " setup-worktree " + shellQuote(cwd) + " " + shellQuote(worktreePath)
	if err := a.multiplexer.SendKeys(branch.GetSessionName(), setup); err != nil {
		return err
	}

	if err := a.multiplexer.AttachSession(branch); err != nil {
		return err
	}

	return nil
}

// SetupWorktreeCommand brings a fresh worktree to life, running inside the
// worktree's own tmux pane: copy gitignored files over, initialize the project
// (.sesssions.yaml + docker-compose.dev.yml), give it its own .envrc values,
// then start the compose stack. Each step is printed as a checklist.
func (a *App) SetupWorktreeCommand(base, worktree string) error {
	containerName := deriveSessionName(worktree)

	steps := []struct {
		title string
		run   func() error
	}{
		{
			title: "copying gitignored files",
			run: func() error {
				n, err := a.git.CopyIgnoredFiles(base, worktree)
				if err != nil {
					return err
				}
				fmt.Printf("    %d file(s) copied from .gitignore\n", n)
				return nil
			},
		},
		{
			title: "initializing project (sesssions init)",
			run: func() error {
				if err := a.initWorktree(base, worktree); err != nil {
					return err
				}
				fmt.Printf("    project files ready in %s\n", worktree)
				return nil
			},
		},
		{
			title: "setting .envrc for the new worktree",
			run: func() error {
				return a.setWorktreeEnvrc(worktree, containerName)
			},
		},
		{
			title: "starting compose (sesssions up)",
			run: func() error {
				return a.UpWorktreeCommand(base, worktree)
			},
		},
	}
	for i, step := range steps {
		fmt.Printf("[%d/%d] %s\n", i+1, len(steps), step.title)
		if err := step.run(); err != nil {
			fmt.Printf("    failed: %v\n", err)
		}
	}
	return nil
}

// setWorktreeEnvrc gives the worktree its own .envrc values: CONTAINER_NAME is
// pinned to the worktree's project__branch name (so every worktree gets a
// unique container / traefik host) and a copied go_dlv_port is dropped so the
// next `sesssions up` picks a fresh, worktree-specific port instead of reusing
// the source project's (which would collide). Other entries are preserved.
func (a *App) setWorktreeEnvrc(cwd, containerName string) error {
	path := filepath.Join(cwd, ".envrc")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	re := regexp.MustCompile(`(?m)^\s*export\s+` + goDlvPortEnv + `=.*$\n?`)
	cleaned := re.ReplaceAllString(string(data), "")
	if cleaned != string(data) {
		if err := os.WriteFile(path, []byte(cleaned), 0o644); err != nil {
			return err
		}
	}

	if containerName == "" {
		return nil
	}
	return setEnvrcValue(cwd, containerNameEnv, containerName)
}

func (a *App) DeleteWorktreeCommand(selected domain.SearchDirectoryItem) error {
	worktreePath := selected.GetFullPath()

	cwd, err := a.git.GetWorktreeBaseDir(worktreePath)
	if err != nil {
		cwd = worktreePath
	}

	// Tear down the compose stack before removing the worktree.
	if err := a.DownCommand(worktreePath); err != nil {
		fmt.Printf("Warning: compose down failed: %v\n", err)
	}

	// Never drop uncommitted work silently: show the pending changes and let
	// the user decide whether to discard them and remove the worktree.
	changed, err := a.git.HasChanges(worktreePath)
	if err != nil {
		return err
	}
	force := false
	if changed {
		ok, err := a.confirmWorktreeRemoval(worktreePath)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Printf("kept worktree %s (uncommitted changes)\n", worktreePath)
			return nil
		}
		force = true
	}

	if err := a.multiplexer.CloseSession(selected.GetSessionName()); err != nil {
		fmt.Printf("Warning: failed to close tmux session: %v\n", err)
	}

	if err := a.git.DeleteWorktree(cwd, worktreePath, force); err != nil {
		return err
	}

	return nil
}

// confirmWorktreeRemoval shows the worktree's pending changes in an fzf dialog
// and asks whether to discard them and remove the worktree anyway. Cancelling
// (escape) keeps the worktree.
func (a *App) confirmWorktreeRemoval(worktreePath string) (bool, error) {
	const (
		remove = "Remove worktree and discard changes"
		keep   = "Keep worktree"
	)

	preview := "git status --short; echo; git diff --color=always; git diff --cached --color=always"
	selected, err := fzf.Confirm(
		"Uncommitted changes in "+filepath.Base(worktreePath)+" — keep or remove?",
		preview,
		[]string{remove, keep},
		worktreePath,
	)
	if errors.Is(err, domain.ErrEmptySelection) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return selected == remove, nil
}

func (a *App) DeleteWorktreeByPathCommand(targetPath string) error {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}

	name := filepath.Base(absPath)
	parent := filepath.Dir(absPath)
	item := domain.NewSearchDirectoryItem(name, parent, domain.SearchItemKindDirectory)
	return a.DeleteWorktreeCommand(item)
}

func (a *App) AddWorktreeByParamsCommand(cwd, branchName string) error {
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return err
	}

	branches, err := a.git.Branches(absCwd)
	if err != nil {
		return err
	}

	var matched *domain.SearchBranchItem
	for _, b := range branches {
		if b.Name == branchName {
			item := domain.NewSearchBranchItem(b.Name, absCwd, b.Remote)
			matched = &item
			break
		}
	}

	if matched == nil {
		item := domain.NewSearchBranchItem(branchName, absCwd, false)
		matched = &item
	}

	return a.AddWorktreeCommand(absCwd, *matched)
}

func (a *App) HelpCommand() {
	fmt.Println("sesssions - A cli tool to manage tmux sessions, git worktrees and docker compose")
	fmt.Println("Usage:")
	fmt.Println("  sesssions [command]")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  init                                  Set this project's language and create docker-compose.dev.yml")
	fmt.Println("  up [path]                           Start the project's compose stack")
	fmt.Println("  down [path]                         Stop the project's compose stack")
	fmt.Println("  add-worktree    <cwd> <branch>      Create a new worktree (brings compose up)")
	fmt.Println("  delete-worktree <path-or-worktree>  Delete a worktree (takes compose down)")
	fmt.Println("  branches        <cwd>               List git branches")
	fmt.Println("  list                                List all sessions")
	fmt.Println("  version                             Show version")
	fmt.Println("  help                                Show this help")
	fmt.Println("")
	fmt.Println("Global config (auto-created on first run): ~/.config/sesssions/sesssions.yaml")
	fmt.Println("Project config: .sesssions.yaml (language) + docker-compose.dev.yml (compose, seeded by `sesssions init`)")
}
