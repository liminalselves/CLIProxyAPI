package management

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/multiuser"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestHubCredentialModelAliases(t *testing.T) {
	h, store, manager, admin, _ := setupHubProviderTest(t)
	channel, err := store.CreateChannel(admin, "models", false)
	if err != nil {
		t.Fatal(err)
	}
	name := "models-codex.json"
	path := filepath.Join(h.cfg.AuthDir, name)
	if err = os.MkdirAll(h.cfg.AuthDir, 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := map[string]any{
		"type": "codex", "owner": admin.Username, "channel_id": channel.ID,
		"prefix": "models-codex#1", "access_token": "private-token",
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	a := &coreauth.Auth{ID: name, FileName: name, Provider: "codex", Prefix: "models-codex#1", Metadata: metadata, Attributes: map[string]string{"path": path}}
	if _, err = manager.Register(coreauth.WithSkipPersist(context.Background()), a); err != nil {
		t.Fatal(err)
	}
	var hooked *coreauth.Auth
	h.SetPostAuthPersistHook(func(_ context.Context, updated *coreauth.Auth) error {
		hooked = updated.Clone()
		return nil
	})
	patch := func(actor multiuser.User, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PATCH", "/v8/hub/credentials/"+providerRouteID(name)+"/fields", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: providerRouteID(name)}}
		c.Set("hubUser", actor)
		h.HubPatchCredentialFields(c)
		return w
	}
	body := `{"model_aliases":[{"name":"gpt-5","alias":"my-gpt"}]}`
	if w := patch(multiuser.User{Username: "bob", Role: "member"}, body); w.Code != 403 {
		t.Fatalf("non-manager alias edit: %d %s", w.Code, w.Body.String())
	}
	if w := patch(admin, body); w.Code != 200 {
		t.Fatalf("alias edit: %d %s", w.Code, w.Body.String())
	}
	if hooked == nil {
		t.Fatal("alias edit did not refresh runtime registration")
	}
	aliases := coreauth.OAuthModelAliasesFromAttributes(hooked.Attributes)
	if len(aliases) != 1 || aliases[0].Name != "gpt-5" || aliases[0].Alias != "my-gpt" {
		t.Fatalf("runtime alias settings: %#v", aliases)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(persisted), "my-gpt") || !strings.Contains(string(persisted), "private-token") {
		t.Fatal("alias persistence lost settings or credential material")
	}
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(name, "codex", []*registry.ModelInfo{
		{ID: "models-codex#1/my-gpt", MetadataModelID: "gpt-5"},
		{ID: "models-codex#1/org/model"},
	})
	t.Cleanup(func() { reg.UnregisterClient(name) })
	for _, actor := range []multiuser.User{admin, {Username: "bob", Role: "member"}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/v8/hub/credentials", nil)
		c.Set("hubUser", actor)
		h.HubCredentials(c)
		var response struct {
			Credentials []struct {
				Models      []string `json:"models"`
				Definitions []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"model_definitions"`
				Aliases json.RawMessage `json:"model_aliases"`
			} `json:"credentials"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil || len(response.Credentials) != 1 {
			t.Fatalf("credential summary: %s (%v)", w.Body.String(), err)
		}
		item := response.Credentials[0]
		if len(item.Models) != 2 || strings.Contains(w.Body.String(), "private-token") {
			t.Fatal("credential summary lost models or exposed a token")
		}
		if actor.Username == admin.Username {
			if len(item.Definitions) != 2 || !strings.Contains(string(item.Aliases), "my-gpt") {
				t.Fatalf("missing editable model settings: %s", w.Body.String())
			}
			upstream := map[string]string{}
			for _, model := range item.Definitions {
				upstream[model.ID] = model.Name
			}
			if upstream["models-codex#1/my-gpt"] != "gpt-5" || upstream["models-codex#1/org/model"] != "org/model" {
				t.Fatalf("model names were not resolved: %#v", upstream)
			}
		} else if item.Definitions != nil || item.Aliases != nil {
			t.Fatal("non-manager received editable settings")
		}
	}
	if w := patch(admin, `{"model_aliases":[]}`); w.Code != 200 {
		t.Fatalf("clear aliases: %d %s", w.Code, w.Body.String())
	}
	if len(coreauth.OAuthModelAliasesFromAttributes(hooked.Attributes)) != 0 {
		t.Fatal("cleared aliases remained in runtime attributes")
	}
}
