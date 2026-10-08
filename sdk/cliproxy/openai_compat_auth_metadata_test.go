package cliproxy

import (
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestOpenAICompatFromAuthMetadata(t *testing.T) {
	t.Run("models from metadata", func(t *testing.T) {
		a := &coreauth.Auth{
			Provider:   "openai-compatible",
			Attributes: map[string]string{"compat_name": "hub-ch-1-daye"},
			Metadata: map[string]any{
				"models": []any{
					map[string]any{"name": "kimi-k3", "alias": "kimi-k3"},
					map[string]any{"name": "gpt-5", "alias": "gpt5"},
				},
			},
		}
		compat := openAICompatFromAuthMetadata(a)
		if compat == nil {
			t.Fatal("metadata models ignored")
		}
		if compat.Name != "hub-ch-1-daye" {
			t.Fatalf("compat name = %q", compat.Name)
		}
		if len(compat.Models) != 2 || compat.Models[1].Alias != "gpt5" {
			t.Fatalf("models = %#v", compat.Models)
		}
		models := buildOpenAICompatibilityConfigModels(compat)
		if len(models) != 2 {
			t.Fatalf("built models = %d", len(models))
		}
	})
	t.Run("missing models", func(t *testing.T) {
		a := &coreauth.Auth{Provider: "openai-compatible", Metadata: map[string]any{}}
		if compat := openAICompatFromAuthMetadata(a); compat != nil {
			t.Fatalf("empty metadata produced entry: %#v", compat)
		}
	})
	t.Run("invalid shape", func(t *testing.T) {
		a := &coreauth.Auth{Provider: "openai-compatible", Metadata: map[string]any{"models": "nope"}}
		if compat := openAICompatFromAuthMetadata(a); compat != nil {
			t.Fatalf("invalid metadata produced entry: %#v", compat)
		}
	})
	t.Run("nil auth", func(t *testing.T) {
		if compat := openAICompatFromAuthMetadata(nil); compat != nil {
			t.Fatal("nil auth produced entry")
		}
	})
}
