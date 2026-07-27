package main

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/novr/utsusemi/internal/config"
)

func writeConfig(path string, cfg *config.Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
