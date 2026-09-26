// Package cli implements the hiok command-line client.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is what persists between invocations: where the API is and who we are.
type Config struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Email    string `json:"email"`
	Region   string `json:"region,omitempty"`
}

// ConfigPath honours HIOK_CONFIG so a CI job can point at its own credentials
// without touching the developer's.
func ConfigPath() string {
	if custom := os.Getenv("HIOK_CONFIG"); custom != "" {
		return custom
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".hiok.json"
	}
	return filepath.Join(home, ".hiok", "config.json")
}

func LoadConfig() (*Config, error) {
	cfg := &Config{Endpoint: os.Getenv("HIOK_ENDPOINT")}

	raw, err := os.ReadFile(ConfigPath())
	if err == nil {
		if err := json.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("%s is not valid JSON: %w", ConfigPath(), err)
		}
	}

	// The environment wins over the stored file: that is what makes a one-off run
	// against a different deployment possible without editing anything.
	if v := os.Getenv("HIOK_ENDPOINT"); v != "" {
		cfg.Endpoint = v
	}
	if v := os.Getenv("HIOK_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("HIOK_REGION"); v != "" {
		cfg.Region = v
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://hiokcloud.com"
	}
	return cfg, nil
}

func SaveConfig(cfg *Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 0600: the file holds a bearer token, which is as good as the password.
	return os.WriteFile(path, raw, 0o600)
}
