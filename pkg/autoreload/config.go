package autoreload

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk (autoreload.yaml) configuration format.
type Config struct {
	Root   string   `yaml:"root"`
	Build  string   `yaml:"build"`
	Exec   string   `yaml:"exec"`
	Ignore []string `yaml:"ignore"`
}

// Load reads and parses an autoreload.yaml file from path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// DefaultPaths returns the locations searched for a config file, in order.
func DefaultPaths() []string {
	home, _ := os.UserHomeDir()
	return []string{
		"autoreload.yaml",
		".autoreload.yaml",
		filepath.Join(home, ".config", "autoreload.yaml"),
	}
}
