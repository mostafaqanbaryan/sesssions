package compose

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Rendered is a rendered compose definition ready for docker compose.
type Rendered struct {
	Cwd  string // working directory for docker compose
	File string // path to the temporary compose file
	Env  string // path to the temporary env file (for --env-file)
}

// Render substitutes placeholders in a compose definition, writes it (plus a
// companion env file) to the cache dir outside the repo, and returns a
// Rendered ready for Up/Down.
//
// Placeholders supported in the compose content:
//
//	{uid}               the current user id
//	{base}              the base name of the project directory
//	__SESSION_NAME__    replaced with sessionName
//
// `${CONTAINER_NAME}` / `${DOMAIN}` / `${go_dlv_port}` and friends are resolved
// by docker compose from the environment file, which is built from env plus the
// session name. env are the merged global + project environment variables.
func Render(projectDir, sessionName string, composeContent string, env map[string]string) (*Rendered, error) {
	if strings.TrimSpace(composeContent) == "" {
		return nil, fmt.Errorf("no compose definition in .sesssions.yaml")
	}

	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("resolve project dir: %w", err)
	}
	projectDir = absProject

	uid := strconv.Itoa(os.Getuid())
	base := filepath.Base(projectDir)

	content := composeContent
	content = strings.ReplaceAll(content, "{uid}", uid)
	content = strings.ReplaceAll(content, "{base}", base)
	content = strings.ReplaceAll(content, "__SESSION_NAME__", sessionName)
	// Backwards compatibility: configs generated before go_dlv_port moved to
	// the environment use the bare {go_dlv_port} placeholder. Shield the
	// ${go_dlv_port} env form so it stays intact for docker compose.
	port := env["go_dlv_port"]
	if port != "" {
		const shield = "\x00GODLV\x00"
		content = strings.ReplaceAll(content, "${go_dlv_port}", shield)
		content = strings.ReplaceAll(content, "{go_dlv_port}", port)
		content = strings.ReplaceAll(content, shield, "${go_dlv_port}")
	}

	// Validate as YAML and make every project-relative path absolute. The
	// compose file lives in a cache dir, so leaving paths relative would make
	// docker compose resolve them against that cache dir instead of the
	// project. Absolute paths keep the rendered file self-contained.
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, fmt.Errorf("compose definition is not valid YAML: %w", err)
	}
	absolutizePaths(&doc, projectDir)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("encode compose definition: %w", err)
	}
	content = buf.String()

	// Always provide CONTAINER_NAME so ${CONTAINER_NAME} never stays blank.
	if env == nil {
		env = map[string]string{}
	}
	if env["CONTAINER_NAME"] == "" {
		env["CONTAINER_NAME"] = sessionName
	}

	dir := filepath.Join(cacheDir(), "sesssions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	hash := sha256.Sum256([]byte(projectDir + "\x00" + sessionName))
	id := hex.EncodeToString(hash[:8])

	composePath := filepath.Join(dir, id+".yaml")
	if err := os.WriteFile(composePath, []byte(content), 0o600); err != nil {
		return nil, fmt.Errorf("write compose file: %w", err)
	}

	envPath := filepath.Join(dir, id+".env")
	if err := writeEnvFile(envPath, env); err != nil {
		return nil, err
	}

	return &Rendered{Cwd: projectDir, File: composePath, Env: envPath}, nil
}

// ReuseBuildImages rewrites services so they reuse an already-built docker
// image instead of rebuilding: a service that has a build block and no explicit
// image gets `image: projectName_serviceName` and its build block is removed.
// Services that already declare an image are left untouched. This lets a
// worktree run the base project's image rather than rebuilding the Dockerfile.
func ReuseBuildImages(content, projectName string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return "", fmt.Errorf("parse compose: %w", err)
	}
	root := &doc
	for root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	services := mappingGet(root, "services")
	if services == nil {
		return content, nil
	}
	for i := 0; i+1 < len(services.Content); i += 2 {
		name := services.Content[i].Value
		svc := services.Content[i+1]
		if svc.Kind != yaml.MappingNode {
			continue
		}
		if mappingGet(svc, "image") != nil || mappingGet(svc, "build") == nil {
			continue
		}
		removeKey(svc, "build")
		addScalar(svc, "image", projectName+"_"+name)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", fmt.Errorf("encode compose: %w", err)
	}
	return buf.String(), nil
}

// removeKey removes a key/value pair from a yaml mapping node.
func removeKey(m *yaml.Node, key string) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

// Up starts the stack defined in the rendered compose file.
func Up(r *Rendered) error {
	return run(r, "up", "-d", "--build")
}

// Down stops and removes the stack defined in the rendered compose file.
func Down(r *Rendered) error {
	return run(r, "down", "-v", "--remove-orphans")
}

func run(r *Rendered, args ...string) error {
	cmdArgs := append([]string{"compose", "--project-directory", r.Cwd, "--env-file", r.Env, "-f", r.File}, args...)
	cmd := exec.Command("docker", cmdArgs...)
	cmd.Dir = r.Cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose: %w", err)
	}
	return nil
}

// absolutizePaths rewrites project-relative build contexts, env files and bind
// mount sources in a parsed compose tree so they resolve against projectDir no
// matter where docker compose reads the file from.
func absolutizePaths(root *yaml.Node, projectDir string) {
	for root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		root = root.Content[0]
	}
	services := mappingGet(root, "services")
	if services == nil {
		return
	}
	for i := 0; i+1 < len(services.Content); i += 2 {
		svc := services.Content[i+1]
		if svc.Kind != yaml.MappingNode {
			continue
		}
		absolutizeService(svc, projectDir)
	}
}

func absolutizeService(svc *yaml.Node, projectDir string) {
	if build := mappingGet(svc, "build"); build != nil {
		switch build.Kind {
		case yaml.ScalarNode:
			build.Value = absHostPath(projectDir, build.Value)
		case yaml.MappingNode:
			ctx := mappingGet(build, "context")
			if ctx != nil && ctx.Kind == yaml.ScalarNode {
				ctx.Value = absHostPath(projectDir, ctx.Value)
			} else if ctx == nil {
				addScalar(build, "context", projectDir)
			}
		}
	}

	if envFile := mappingGet(svc, "env_file"); envFile != nil {
		switch envFile.Kind {
		case yaml.ScalarNode:
			envFile.Value = absHostPath(projectDir, envFile.Value)
		case yaml.SequenceNode:
			for _, item := range envFile.Content {
				switch item.Kind {
				case yaml.ScalarNode:
					item.Value = absHostPath(projectDir, item.Value)
				case yaml.MappingNode:
					if p := mappingGet(item, "path"); p != nil {
						p.Value = absHostPath(projectDir, p.Value)
					}
				}
			}
		}
	}

	if vols := mappingGet(svc, "volumes"); vols != nil && vols.Kind == yaml.SequenceNode {
		for _, item := range vols.Content {
			switch item.Kind {
			case yaml.ScalarNode:
				absolutizeVolumeString(item, projectDir)
			case yaml.MappingNode:
				if t := mappingGet(item, "type"); t != nil && t.Value == "bind" {
					if src := mappingGet(item, "source"); src != nil {
						src.Value = absHostPath(projectDir, src.Value)
					}
				}
			}
		}
	}
}

// absolutizeVolumeString rewrites the host part of a "host:target[:mode]"
// volume when it is a project-relative bind mount. Named volumes (no leading
// ./ ../ / or ~) are left untouched.
func absolutizeVolumeString(node *yaml.Node, projectDir string) {
	parts := strings.SplitN(node.Value, ":", 2)
	if len(parts) != 2 {
		return
	}
	abs := absHostPath(projectDir, parts[0])
	if abs != parts[0] {
		node.Value = abs + ":" + parts[1]
	}
}

// absHostPath joins a project-relative path to projectDir. Absolute paths, ~/
// prefixes and named volumes are returned unchanged.
func absHostPath(projectDir, p string) string {
	if p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "~") {
		return p
	}
	if !strings.HasPrefix(p, ".") {
		return p
	}
	joined := filepath.Join(projectDir, p)
	if strings.HasSuffix(p, "/") && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined
}

func mappingGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func addScalar(m *yaml.Node, key, value string) {
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value},
	)
}

// writeEnvFile writes KEY=VALUE lines sorted for determinism.
func writeEnvFile(path string, env map[string]string) error {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, env[k])
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// cacheDir returns the user cache directory ($XDG_CACHE_HOME or ~/.cache).
func cacheDir() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cache"
	}
	return filepath.Join(home, ".cache")
}
