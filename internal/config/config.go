package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Root   string   `yaml:"root"`
	Build  string   `yaml:"build"`
	Exec   string   `yaml:"exec"`
	Ignore []string `yaml:"ignore"`
}

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

func DefaultPaths() []string {
	home, _ := os.UserHomeDir()
	return []string{
		"hotreload.yaml",
		".hotreload.yaml",
		filepath.Join(home, ".config", "hotreload.yaml"),
	}
}
