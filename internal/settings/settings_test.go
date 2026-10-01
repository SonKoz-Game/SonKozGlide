package settings

import (
	"encoding/json"
	"os"
	"testing"
)

func useTempHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
}

func TestGetDoesNotCreateTheDirectory(t *testing.T) {
	useTempHome(t)

	if cfg := Get(); cfg != defaults() {
		t.Fatalf("missing file must yield defaults, got %+v", cfg)
	}
	if _, err := os.Stat(Dir()); !os.IsNotExist(err) {
		t.Fatalf("reading settings must not recreate %s after an uninstall", Dir())
	}
}

func TestRemoveDeletesSavedSettings(t *testing.T) {
	useTempHome(t)

	cfg := defaults()
	cfg.ISPProfile = "vodafone"
	if err := Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	if Get().ISPProfile != "vodafone" {
		t.Fatal("saved settings were not read back")
	}

	removed, err := Remove()
	if err != nil || !removed {
		t.Fatalf("remove = %v, %v", removed, err)
	}
	if _, err := os.Stat(Dir()); !os.IsNotExist(err) {
		t.Fatal("settings directory still exists")
	}

	removed, err = Remove()
	if err != nil || removed {
		t.Fatalf("second remove = %v, %v; want nothing to remove", removed, err)
	}
}

func TestPartialConfigKeepsDefaultsForMissingKeys(t *testing.T) {
	cfg := defaults()
	if err := json.Unmarshal([]byte(`{"autoUpdate":false,"ispProfile":"vodafone"}`), &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.AutoUpdate {
		t.Fatal("an explicit false must win over the default")
	}
	if cfg.ISPProfile != "vodafone" {
		t.Fatalf("unexpected profile %q", cfg.ISPProfile)
	}
	if !cfg.SafeDNS || !cfg.NetworkTuning {
		t.Fatal("a settings file written before these keys existed must not silently disable them")
	}
}

func TestExplicitOptOutSurvives(t *testing.T) {
	cfg := defaults()
	if err := json.Unmarshal([]byte(`{"safeDns":false,"networkTuning":false}`), &cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.SafeDNS || cfg.NetworkTuning {
		t.Fatal("the user turning these off must stick")
	}
}
