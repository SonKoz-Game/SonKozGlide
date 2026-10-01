package settings

import (
	"encoding/json"
	"testing"
)

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
