package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/multiuser"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func setupCredentialModelsTest(t *testing.T) (*Handler, *coreauth.Auth, multiuser.User) {
	t.Helper()
	h, store, manager, owner, _ := setupHubProviderTest(t)
	channel, err := store.CreateChannel(owner, "models", false)
	if err != nil {
		t.Fatal(err)
	}
	a := &coreauth.Auth{ID: "credential-model-management.json", FileName: "credential-model-management.json", Provider: "codex", Prefix: "models-codex#1", Metadata: map[string]any{
		"type": "codex", "owner": owner.Username, "channel_id": channel.ID,
		"prefix": "models-codex#1", "access_token": "private-token", "enabled_models": []string{"model-a"},
	}}
	if _, err := manager.Register(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	h.SetCredentialModelCatalogHook(func(_ context.Context, _ *coreauth.Auth, _ bool) (*coreauth.CredentialModelCatalog, error) {
		return &coreauth.CredentialModelCatalog{Models: []*registry.ModelInfo{{ID: "model-a", ContextLength: 200000}, {ID: "model-b"}}, Source: registry.ModelCatalogStatus{Kind: "remote_catalog", SupportsRefresh: true, AutoRefresh: true, RefreshIntervalSeconds: 10800}}, nil
	})
	return h, a, owner
}

func credentialModelsRequest(t *testing.T, h *Handler, a *coreauth.Auth, actor multiuser.User, method, body string, discover, management bool) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/v8/hub/credentials/"+providerRouteID(a.ID)+"/models", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: providerRouteID(a.ID)}}
	c.Set("hubUser", actor)
	if management {
		c.Params = nil
		c.Request.URL.RawQuery = "name=" + a.FileName
	}
	if discover {
		h.DiscoverCredentialModels(c)
	} else {
		h.CredentialModels(c)
	}
	return w
}

func TestCredentialModelManagementLifecycle(t *testing.T) {
	h, a, owner := setupCredentialModelsTest(t)
	hookCalls := 0
	h.SetPostAuthPersistHook(func(_ context.Context, updated *coreauth.Auth) error {
		hookCalls++
		if updated.ID != a.ID {
			t.Error("wrong runtime credential")
		}
		return nil
	})
	request := func(method, body string) map[string]json.RawMessage {
		t.Helper()
		w := credentialModelsRequest(t, h, a, owner, method, body, false, false)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private-token") {
			t.Fatal("model management exposed a credential token")
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	payload := request(http.MethodGet, "")
	if string(payload["configured"]) != "false" || !strings.Contains(string(payload["available_models"]), "model-b") {
		t.Fatal("catalog discovery was filtered by the publication allowlist")
	}
	payload = request(http.MethodPut, `{"models":[{"name":"model-a","alias":"my-model","max-context-length":123456,"thinking":{"levels":["HIGH","high"]},"is-compat":false,"support-configuration-update":false,"input-modalities":["TEXT","IMAGE"]},{"name":"org/manual-model"}]}`)
	if string(payload["configured"]) != "true" || !strings.Contains(string(payload["models"]), "my-model") {
		t.Fatal("explicit models were not returned")
	}
	current, _ := h.authManager.GetByID(a.ID)
	models, configured := current.CredentialModels()
	if !configured || len(models) != 2 || models[0].MaxContextLength == nil || *models[0].MaxContextLength != 123456 || len(models[0].Thinking.Levels) != 1 {
		t.Fatalf("saved models: %#v", models)
	}
	path := current.Attributes[coreauth.AttributePath]
	if path == "" {
		path = h.cfg.AuthDir + "/" + a.FileName
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "private-token") || !strings.Contains(string(data), "my-model") {
		t.Fatalf("model persistence lost settings or secrets: %v", err)
	}
	payload = request(http.MethodPatch, `{"models":[{"name":"model-b","alias":"added"}],"remove":["org/manual-model"]}`)
	if strings.Contains(string(payload["models"]), "org/manual-model") || !strings.Contains(string(payload["models"]), "my-model") || !strings.Contains(string(payload["models"]), "added") {
		t.Fatalf("merge/remove: %s", payload["models"])
	}
	payload = request(http.MethodPut, `{"models":[]}`)
	if string(payload["models"]) != "[]" || string(payload["configured"]) != "true" {
		t.Fatal("an empty explicit list did not remain distinct from defaults")
	}
	payload = request(http.MethodDelete, "")
	if string(payload["configured"]) != "false" || !strings.Contains(string(payload["models"]), "model-b") {
		t.Fatal("reset did not restore the provider catalog")
	}
	if hookCalls != 4 {
		t.Fatalf("runtime sync calls = %d, want 4", hookCalls)
	}
	current, _ = h.authManager.GetByID(a.ID)
	if _, exists := current.Metadata["models"]; exists {
		t.Fatal("reset retained model overrides")
	}
	if _, exists := current.Metadata["enabled_models"]; !exists {
		t.Fatal("reset erased unrelated publication policy")
	}
}

func TestCredentialModelManagementPermissionsAndValidation(t *testing.T) {
	h, a, owner := setupCredentialModelsTest(t)
	outsider := multiuser.User{Username: "other", Role: "member"}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodPost} {
		w := credentialModelsRequest(t, h, a, outsider, method, `{"models":[]}`, method == http.MethodPost, false)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s outsider: %d %s", method, w.Code, w.Body.String())
		}
	}
	for _, body := range []string{
		`{}`, `{"models":null}`, `{"models":[{"name":""}]}`, `{"models":[{"name":"model-a","fork":true}]}`,
		`{"models":[{"name":"model-a","max-context-length":0}]}`, `{"models":[{"name":"model-a","thinking":{"min":100,"max":10}}]}`,
		`{"models":[{"name":"model-a","input-modalities":["unknown"]}]}`, `{"models":[{"name":"model-a","alias":"same"},{"name":"model-b","alias":"SAME"}]}`,
		`{"models":[{"name":"models-codex#1/model-a"}]}`, `{"models":[],"api_key":"bad"}`, `{"models":[]} {}`, `{"models":[],"remove":["a"]}`,
	} {
		w := credentialModelsRequest(t, h, a, owner, http.MethodPut, body, false, false)
		if w.Code != http.StatusBadRequest {
			t.Errorf("accepted %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	current, _ := h.authManager.GetByID(a.ID)
	if _, exists := current.Metadata["models"]; exists {
		t.Fatal("rejected writes changed configuration")
	}
	// Management uses the same operation, but its lookup comes from the query.
	w := credentialModelsRequest(t, h, a, owner, http.MethodPut, `{"models":[{"name":"manual-model"}]}`, false, true)
	if w.Code != http.StatusOK {
		t.Fatalf("management API: %d %s", w.Code, w.Body.String())
	}
	current, _ = h.authManager.GetByID(a.ID)
	current.Provider = "openai-compatible"
	if _, err := h.authManager.Update(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	w = credentialModelsRequest(t, h, a, owner, http.MethodPut, `{"models":[]}`, false, false)
	if w.Code != http.StatusConflict {
		t.Fatalf("provider edited as an OAuth credential: %d", w.Code)
	}
}

func TestCredentialModelDiscoveryDoesNotReplaceOverrides(t *testing.T) {
	h, a, owner := setupCredentialModelsTest(t)
	w := credentialModelsRequest(t, h, a, owner, http.MethodPut, `{"models":[{"name":"manual-model","alias":"kept"}]}`, false, false)
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	refreshes := 0
	var failure error
	h.SetCredentialModelCatalogHook(func(_ context.Context, auth *coreauth.Auth, refresh bool) (*coreauth.CredentialModelCatalog, error) {
		if auth.ID != a.ID || !refresh {
			t.Fatal("discovery did not request a refresh of the selected credential")
		}
		refreshes++
		if failure != nil {
			return nil, failure
		}
		return &coreauth.CredentialModelCatalog{Models: []*registry.ModelInfo{{ID: "new-model"}}, Source: registry.ModelCatalogStatus{Kind: "remote_catalog"}}, nil
	})
	w = credentialModelsRequest(t, h, a, owner, http.MethodPost, "", true, false)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "new-model") || !strings.Contains(w.Body.String(), "kept") {
		t.Fatalf("discovery: %d %s", w.Code, w.Body.String())
	}
	failure = coreauth.ErrCredentialModelsRefreshFailed
	w = credentialModelsRequest(t, h, a, owner, http.MethodPost, "", true, false)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("failed refresh: %d %s", w.Code, w.Body.String())
	}
	failure = coreauth.ErrCredentialModelsUnsupported
	w = credentialModelsRequest(t, h, a, owner, http.MethodPost, "", true, false)
	if w.Code != http.StatusConflict {
		t.Fatalf("disabled refresh: %d %s", w.Code, w.Body.String())
	}
	current, _ := h.authManager.GetByID(a.ID)
	models, _ := current.CredentialModels()
	if refreshes != 3 || len(models) != 1 || models[0].Alias != "kept" {
		t.Fatal("discovery changed saved overrides")
	}
}

func TestCredentialModelManagementWithoutAccountCatalog(t *testing.T) {
	h, a, owner := setupCredentialModelsTest(t)
	current, _ := h.authManager.GetByID(a.ID)
	current.Provider = "antigravity"
	if _, err := h.authManager.Update(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	h.SetCredentialModelCatalogHook(nil)
	w := credentialModelsRequest(t, h, a, owner, http.MethodGet, "", false, false)
	var payload struct {
		Configured       bool                        `json:"configured"`
		CatalogAvailable bool                        `json:"catalog_available"`
		Source           registry.ModelCatalogStatus `json:"source"`
		Capabilities     map[string]bool             `json:"capabilities"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || payload.Configured || payload.CatalogAvailable || payload.Source.Kind != "account_upstream" || payload.Capabilities["manual_models"] {
		t.Fatalf("unknown account catalog was advertised as available: %s", w.Body.String())
	}
	w = credentialModelsRequest(t, h, a, owner, http.MethodPatch, `{"models":[{"name":"model-a"}]}`, false, false)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("merged unknown defaults: %d %s", w.Code, w.Body.String())
	}
	for _, tc := range []struct{ method, body string }{
		{http.MethodPut, `{"models":[{"name":"model-a"}]}`},
		{http.MethodPatch, `{"models":[{"name":"model-b"}]}`},
		{http.MethodDelete, ""},
	} {
		w = credentialModelsRequest(t, h, a, owner, tc.method, tc.body, false, false)
		if w.Code != http.StatusOK {
			t.Fatalf("%s without an account catalog: %d %s", tc.method, w.Code, w.Body.String())
		}
	}
}
