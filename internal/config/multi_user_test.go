package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMultiUserConfigV8RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "共享配置.yaml")
	if err := os.WriteFile(path, []byte("config-version: 8\nserver:\n  port: 8317\nmulti-user:\n  enabled: true\n  data-dir: ./账户\n  session-ttl-hours: 48\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MultiUser.Enabled || cfg.MultiUser.DataDir != "./账户" || cfg.MultiUser.SessionTTLHours != 48 {
		t.Fatalf("configuration lost: %#v", cfg.MultiUser)
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var restored Config
	if err := yaml.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	cleaned, _, err := NormalizeConfigLayout(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateV8Config(cleaned); err != nil {
		t.Fatal(err)
	}
	var normalized Config
	if err := yaml.Unmarshal(cleaned, &normalized); err != nil {
		t.Fatal(err)
	}
	if normalized.MultiUser != cfg.MultiUser {
		t.Fatal("configuration migration dropped multi-user settings")
	}
	if restored.MultiUser != cfg.MultiUser {
		t.Fatalf("round trip changed account config: %#v", restored.MultiUser)
	}
	if err := os.WriteFile(path, []byte("multi-user:\n  session-ttl-hours: -1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("invalid session duration accepted")
	}
}
