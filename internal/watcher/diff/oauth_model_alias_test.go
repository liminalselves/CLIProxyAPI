package diff

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestDiffOAuthModelAliasChanges_IncludesAliasChange(t *testing.T) {
	oldMap := map[string][]config.OAuthModelAlias{
		"antigravity": {
			{Name: "claude-opus-4-6-thinking", Alias: "claude-antigravity-opus-4-6-thinking"},
		},
	}
	newMap := map[string][]config.OAuthModelAlias{
		"antigravity": {
			{Name: "claude-opus-4-6-thinking", Alias: "claude-antigravity-opus-4-6"},
		},
	}

	changes, affected := DiffOAuthModelAliasChanges(oldMap, newMap)
	expectContains(t, changes, "oauth-model-alias[antigravity]: updated (1 -> 1 entries)")
	if len(affected) != 1 || affected[0] != "antigravity" {
		t.Fatalf("expected antigravity to be affected, got %#v", affected)
	}
}
