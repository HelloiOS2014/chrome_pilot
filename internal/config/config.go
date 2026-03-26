package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	WSPort      int    `yaml:"ws_port"`
	IdleTimeout string `yaml:"idle_timeout"`
	SocketPath  string `yaml:"socket_path"`
	LogLevel    string `yaml:"log_level"`
	TmpMaxAge   string `yaml:"tmp_max_age"`
	TmpMaxSize  string `yaml:"tmp_max_size"`
}

func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: home dir: %w", err)
	}
	return filepath.Join(home, ".chrome-pilot"), nil
}

func DefaultConfig() *Config {
	dataDir, _ := DataDir()
	return &Config{
		WSPort:      9333,
		IdleTimeout: "30m",
		SocketPath:  filepath.Join(dataDir, "chrome-pilot.sock"),
		LogLevel:    "info",
		TmpMaxAge:   "24h",
		TmpMaxSize:  "500MB",
	}
}

func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return cfg, nil
}

func ConfigPath() (string, error) {
	dataDir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "config.yaml"), nil
}
