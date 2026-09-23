package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const ProjectConfigFileName = ".sesssions.yaml"

// DockerComposeDevFile is the per-project compose file seeded from a language
// template by `sesssions init`. It is the authoritative compose source for a
// project; the file may be edited freely.
const DockerComposeDevFile = "docker-compose.dev.yml"

// ProjectConfig is the per-project configuration, stored in .sesssions.yaml
// in the project root. It declares the project's language and holds the
// docker-compose YAML (and environment) inline, so no compose files are
// committed to the repository.
type ProjectConfig struct {
	Language    string            `yaml:"language,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Compose     string            `yaml:"compose,omitempty"`
}

func LoadProjectConfig(dir string) (*ProjectConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, ProjectConfigFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveProjectConfig(dir string, cfg *ProjectConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ProjectConfigFileName), data, 0o644)
}