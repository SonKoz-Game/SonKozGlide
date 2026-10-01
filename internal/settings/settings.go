package settings

import (
	"encoding/json"
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

func getPath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".sonkoz")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "settings.json")
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
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(getPath(), data, 0644)
}
