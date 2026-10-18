package auth

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestCredentialModelsResolveUpstreamAndResponseAlias(t *testing.T) {
	models, err := DecodeCredentialModels([]byte(`[{"name":"upstream-model","alias":"my-model"},{"name":"native"}]`))
	if err != nil {
		t.Fatal(err)
	}
	a := &Auth{Provider: "codex", Metadata: map[string]any{"models": models}}
	m := NewManager(nil, nil, nil)
	m.SetOAuthModelAlias(map[string][]config.OAuthModelAlias{"codex": {{Name: "wrong-upstream", Alias: "my-model"}}})
	result := m.resolveOAuthModelAliasWithResult(a, "my-model(high)")
	if result.UpstreamModel != "upstream-model(high)" || result.OriginalAlias != "my-model" {
		t.Fatalf("custom alias: %#v", result)
	}
	result = m.resolveOAuthModelAliasWithResult(a, "native")
	if result.UpstreamModel != "native" {
		t.Fatalf("unaliased model: %#v", result)
	}
	a.Metadata["models"] = []any{}
	if result := m.resolveOAuthModelAliasWithResult(a, "my-model"); result.UpstreamModel != "" {
		t.Fatal("empty custom list inherited a global alias")
	}
	delete(a.Metadata, "models")
	if result := m.resolveOAuthModelAliasWithResult(a, "my-model"); result.UpstreamModel != "wrong-upstream" {
		t.Fatal("reset did not restore global alias resolution")
	}
}

func TestCredentialModelScopeAndNormalization(t *testing.T) {
	models, err := DecodeCredentialModels([]byte(`[{"name":" org/model ","alias":" friendly ","thinking":{"levels":[" HIGH ","high","none"]},"input-modalities":["TEXT","text","image"]}]`))
	if err != nil || len(models) != 1 {
		t.Fatalf("decode: %#v %v", models, err)
	}
	model := models[0]
	if model.Name != "org/model" || model.Alias != "friendly" || len(model.Thinking.Levels) != 2 || !model.Thinking.ZeroAllowed || len(*model.InputModalities) != 2 {
		t.Fatalf("normalization: %#v", model)
	}
	for _, a := range []*Auth{nil, {Provider: "codex", Attributes: map[string]string{AttributeAPIKey: "key"}, Metadata: map[string]any{"models": models}}} {
		if _, configured := a.CredentialModels(); configured {
			t.Fatalf("non-OAuth model list intercepted: %#v", a)
		}
	}
	compat := &Auth{Provider: "openai-compatible", Metadata: map[string]any{"models": models}}
	if entries, configured := compat.CredentialModels(); !configured || len(entries) != 1 || entries[0].Name != "org/model" {
		t.Fatalf("openai-compatible model list not honored: %#v", entries)
	}
}
