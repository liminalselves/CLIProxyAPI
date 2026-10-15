package cliproxy

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

func TestBuildCodexConfigModelsSelectsDefaultsOrConfiguredModels(t *testing.T) {
	configured := buildCodexConfigModels(&config.CodexKey{Models: []config.CodexModel{{
		Name: "upstream-codex", Alias: "configured-codex",
	}}})
	if len(configured) != 1 {
		t.Fatalf("configured model count = %d, want 1", len(configured))
	}
	if configured[0].ID != "configured-codex" {
		t.Fatalf("configured model ID = %q, want configured-codex", configured[0].ID)
	}

	defaults := buildCodexConfigModels(&config.CodexKey{})
	wantDefaults := registry.GetCodexProModels()
	if len(defaults) != len(wantDefaults) {
		t.Fatalf("default model count = %d, want %d", len(defaults), len(wantDefaults))
	}
	defaultIDs := make(map[string]struct{}, len(defaults))
	for _, model := range defaults {
		if model != nil {
			defaultIDs[model.ID] = struct{}{}
		}
	}
	for _, modelID := range []string{"gpt-image-1.5", "gpt-image-2", "gpt-image-2.5-flare", "gpt-image-2.5-sunburst", "gpt-image-2.5"} {
		if _, ok := defaultIDs[modelID]; !ok {
			t.Errorf("missing default model %q", modelID)
		}
	}
}

func TestBuildConfigModelsDisplayNameFallback(t *testing.T) {
	model := buildClaudeConfigModels(&config.ClaudeKey{Models: []config.ClaudeModel{{
		Name: "claude-upstream", Alias: "claude-catalog",
	}}})[0]
	if model.DisplayName != "claude-upstream" {
		t.Fatalf("DisplayName = %q, want upstream model name", model.DisplayName)
	}
}
