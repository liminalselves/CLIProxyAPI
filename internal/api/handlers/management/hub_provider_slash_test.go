package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestHubProviderSlashModelsReachUpstream(t *testing.T) {
	const upstream = "vanchin/deepseek-v4.1-flash"
	for _, alias := range []string{"", "flash", "public/flash"} {
		t.Run("alias="+alias, func(t *testing.T) {
			h, store, manager, admin, _ := setupHubProviderTest(t)
			channel, err := store.CreateChannel(admin, "573", false)
			if err != nil {
				t.Fatal(err)
			}
			requests := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				if errDecode := json.NewDecoder(r.Body).Decode(&body); errDecode != nil {
					t.Errorf("decode upstream request: %v", errDecode)
				}
				requests <- body.Model
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"chatcmpl-test","model":"vanchin/deepseek-v4.1-flash","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
			}))
			defer server.Close()
			create, err := json.Marshal(map[string]any{
				"name": "provider", "base_url": server.URL + "/v1", "api_key": "test-key",
				"models": []hubProviderModelInput{{Name: upstream, Alias: alias}},
			})
			if err != nil {
				t.Fatal(err)
			}
			w, out := providerRequest(t, h, admin, "POST", "/v8/hub/providers?channel_id="+channel.ID, string(create))
			if w.Code != http.StatusCreated {
				t.Fatalf("create slash model: %d %s", w.Code, w.Body.String())
			}
			var id string
			if err = json.Unmarshal(out["provider_id"], &id); err != nil {
				t.Fatal(err)
			}
			patch, _ := json.Marshal(map[string]any{"models": []hubProviderModelInput{{Name: upstream, Alias: alias}}})
			w, _ = providerRequest(t, h, admin, "PATCH", "/v8/hub/providers/"+providerRouteID(id), string(patch))
			if w.Code != http.StatusOK {
				t.Fatalf("patch slash model: %d %s", w.Code, w.Body.String())
			}
			auth, ok := manager.GetByID(id)
			if !ok {
				t.Fatal("created provider missing")
			}
			models, configured := auth.CredentialModels()
			if !configured || len(models) != 1 || models[0].Name != upstream {
				t.Fatalf("persisted model = %#v", models)
			}
			public := alias
			if public == "" {
				public = upstream
			}
			compatName := auth.Attributes["compat_name"]
			cfg := &config.Config{
				MultiUser:           config.MultiUserConfig{Enabled: true},
				OpenAICompatibility: []config.OpenAICompatibility{{Name: compatName, Models: []config.OpenAICompatibilityModel{{Name: upstream, Alias: public}}}},
			}
			manager.SetConfig(cfg)
			provider := util.OpenAICompatibleProviderKey(compatName)
			manager.RegisterExecutor(executor.NewOpenAICompatExecutor(provider, cfg))
			reg := registry.GetGlobalRegistry()
			reg.RegisterClient(id, provider, []*registry.ModelInfo{{ID: auth.Prefix + "/" + public}})
			t.Cleanup(func() { reg.UnregisterClient(id) })
			for _, model := range []string{public, auth.Prefix + "/" + public} {
				payload, _ := json.Marshal(map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": "hello"}}})
				_, err = manager.Execute(coreauth.WithBareModelsAllowed(context.Background()), []string{provider}, cliproxyexecutor.Request{Model: model, Payload: payload}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
				if err != nil {
					t.Fatalf("execute %q: %v", model, err)
				}
				if got := <-requests; got != upstream {
					t.Fatalf("upstream received model %q, want %q", got, upstream)
				}
			}
		})
	}
}
