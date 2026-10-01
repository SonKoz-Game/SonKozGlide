package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Config struct {
	AutoUpdate      bool   `json:"autoUpdate"`
	AutoStartBypass bool   `json:"autoStartBypass"`
	ISPProfile      string `json:"ispProfile"`
	SafeDNS         bool   `json:"safeDns"`
	NetworkTuning   bool   `json:"networkTuning"`
}

func defaults() Config {
	return Config{
		AutoUpdate:      true,
		AutoStartBypass: true,
		ISPProfile:      "auto",
		SafeDNS:         true,
		NetworkTuning:   true,
	}
}

func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".sonkoz")
}

func getPath() string {
	return filepath.Join(Dir(), "settings.json")
}

func Get() Config {
	cfg := defaults()

	data, err := os.ReadFile(getPath())
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	if cfg.ISPProfile == "" {
		cfg.ISPProfile = "auto"
	}
	return cfg
}

func Save(cfg Config) error {
	if err := os.MkdirAll(Dir(), 0755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(getPath(), data, 0644)
}

func Remove() (bool, error) {
	dir := Dir()
	if !filepath.IsAbs(dir) {
		return false, errors.New("settings directory is not absolute")
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return true, os.RemoveAll(dir)
}
