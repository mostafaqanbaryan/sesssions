package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed templates/sesssions.yaml
var DefaultConfigTemplate string

const (
	ConfigFileName = "sesssions.yaml"

	// CurrentTemplateVersion is bumped whenever the bundled language compose
	// templates change. Init refreshes the bundled templates when an existing
	// config predates a given version (preserving projects and custom
	// languages).
	CurrentTemplateVersion = 2
)

// LanguageConfig defines the defaults for one language: environment variables
// and the docker-compose YAML template that `init` embeds into a project's
// .sesssions.yaml.
type LanguageConfig struct {
	Environment map[string]string `yaml:"environment,omitempty"`
	Compose     string            `yaml:"compose,omitempty"`

	// Files is kept for backwards compatibility with configs generated before
	// compose definitions moved inline. Load promotes its compose file into
	// Compose, so old global configs continue to work without migration.
	Files map[string]string `yaml:"files,omitempty"`
}

// Config is the global configuration stored at ~/.config/sesssions/sesssions.yaml.
type Config struct {
	TemplateVersion int                       `yaml:"template_version"`
	Projects        []string                  `yaml:"projects"`
	Languages       map[string]LanguageConfig `yaml:"languages"`
}

// DefaultConfigDir returns the platform config directory (e.g. ~/.config on Linux).
func DefaultConfigDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "sesssions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config/sesssions"
	}
	return filepath.Join(home, ".config", "sesssions")
}

// DefaultConfigPath returns the path to the global configuration file.
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), ConfigFileName)
}

// Load reads the global configuration, returning (nil, nil) when the file
// does not exist. ConfigDir allows overriding the config directory in tests.
func Load(configDir string) (*Config, error) {
	if configDir == "" {
		configDir = DefaultConfigDir()
	}

	data, err := os.ReadFile(filepath.Join(configDir, ConfigFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", ConfigFileName, err)
	}

	// Promote the old files.docker-compose.dev.yml shape to the current
	// inline compose field. This keeps existing user-edited global configs
	// working after the schema change.
	for name, lang := range cfg.Languages {
		if strings.TrimSpace(lang.Compose) == "" {
			for fileName, compose := range lang.Files {
				if fileName == "docker-compose.yml" || fileName == "docker-compose.dev.yml" {
					lang.Compose = compose
					cfg.Languages[name] = lang
					break
				}
			}
		}
	}

	for i, p := range cfg.Projects {
		cfg.Projects[i] = expandHome(p)
	}
	return &cfg, nil
}

// expandHome resolves a leading ~ or ~/ to the current user's home directory.
// A trailing slash is preserved so that `~/Projects/` remains distinct from
// `~/Projects` — the session list uses it to mean "scan the children" vs
// "show the directory itself".
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		if p == "~" {
			return home
		}
		joined := filepath.Join(home, p[2:])
		if strings.HasSuffix(p, "/") && !strings.HasSuffix(joined, "/") {
			joined += "/"
		}
		return joined
	}
	return p
}

// Init writes the bundled default configuration to ~/.config/sesssions/sesssions.yaml.
// On first run it writes the full default file. On later runs it only refreshes
// the bundled language templates when the file predates CurrentTemplateVersion —
// the projects list and any custom languages are preserved. When the config is
// already current it is left untouched.
func Init() (string, bool, error) {
	dir := DefaultConfigDir()
	path := filepath.Join(dir, ConfigFileName)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, fmt.Errorf("create config dir: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, fmt.Errorf("read config: %w", err)
	}

	if len(data) == 0 {
		if err := os.WriteFile(path, []byte(DefaultConfigTemplate), 0o644); err != nil {
			return "", false, fmt.Errorf("write config: %w", err)
		}
		return path, true, nil
	}

	var current Config
	parses := yaml.Unmarshal(data, &current) == nil
	if parses && current.TemplateVersion >= CurrentTemplateVersion {
		return path, false, nil
	}

	// Legacy or stale config: upgrade the bundled language templates in place,
	// keeping the user's projects and custom languages.
	var bundled Config
	if err := yaml.Unmarshal([]byte(DefaultConfigTemplate), &bundled); err != nil {
		return "", false, fmt.Errorf("parse bundled config: %w", err)
	}
	if current.Languages == nil {
		current.Languages = map[string]LanguageConfig{}
	}
	for name, lang := range bundled.Languages {
		current.Languages[name] = lang
	}
	current.TemplateVersion = CurrentTemplateVersion

	out, err := yaml.Marshal(current)
	if err != nil {
		return "", false, fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return "", false, fmt.Errorf("write config: %w", err)
	}
	return path, true, nil
}
