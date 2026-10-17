package cliproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestCredentialModelsRegisterCapabilitiesAndSurviveReregistration(t *testing.T) {
	models, err := coreauth.DecodeCredentialModels([]byte(`[{"name":"gpt-5","alias":"my-model","max-context-length":123456,"thinking":{"levels":["high"]},"is-compat":true,"support-configuration-update":false,"input-modalities":["text","image"],"output-modalities":["text"]},{"name":"manual-new-model"}]`))
	if err != nil {
		t.Fatal(err)
	}
	a := &coreauth.Auth{ID: "credential-custom-models", Provider: "codex", Prefix: "573-codex#1", Attributes: map[string]string{"plan_type": "pro"}, Metadata: map[string]any{"models": models, "owner": "alice", "channel_id": "channel", "published": true}}
	service := &Service{cfg: &config.Config{OAuthModelAlias: map[string][]config.OAuthModelAlias{"codex": {{Name: "gpt-5", Alias: "global-alias"}}}}}
	service.cfg.MultiUser.Enabled = true
	reg := registry.GetGlobalRegistry()
	t.Cleanup(func() { reg.UnregisterClient(a.ID) })
	for range 2 {
		service.registerModelsForAuth(context.Background(), a)
		registered := reg.GetModelsForClient(a.ID)
		if len(registered) != 2 {
			t.Fatalf("registered models = %#v", registered)
		}
		byID := map[string]*ModelInfo{}
		for _, model := range registered {
			byID[model.ID] = model
		}
		custom := byID["573-codex#1/my-model"]
		if custom == nil || custom.MetadataModelID != "gpt-5" || custom.ContextLength != 123456 || custom.MaxContextLength != 123456 || !custom.IsCompat || custom.SupportConfigurationUpdate || !custom.ExplicitThinking || !custom.ExplicitInputModalities || len(custom.SupportedInputModalities) != 2 || len(custom.Thinking.Levels) != 1 {
			t.Fatalf("configured capabilities: %#v", custom)
		}
		if byID["573-codex#1/manual-new-model"] == nil || byID["573-codex#1/global-alias"] != nil {
			t.Fatal("manual model or explicit alias precedence was lost")
		}
	}
	// A persisted JSON round trip must behave like the in-memory configuration.
	data, err := json.Marshal(a.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &a.Metadata); err != nil {
		t.Fatal(err)
	}
	service.registerModelsForAuth(context.Background(), a)
	if len(reg.GetModelsForClient(a.ID)) != 2 {
		t.Fatal("models did not survive a file reload")
	}
	a.Metadata["models"] = []any{}
	service.registerModelsForAuth(context.Background(), a)
	if len(reg.GetModelsForClient(a.ID)) != 0 {
		t.Fatal("empty models restored the catalog")
	}
	delete(a.Metadata, "models")
	service.registerModelsForAuth(context.Background(), a)
	if len(reg.GetModelsForClient(a.ID)) == 0 {
		t.Fatal("reset did not restore the catalog")
	}
}

func TestCredentialModelsPreserveCatalogAndRespectExclusions(t *testing.T) {
	thinking := &registry.ThinkingSupport{Levels: []string{"low", "high"}}
	source := &ModelInfo{ID: "native", ContextLength: 100, SupportConfigurationUpdate: true, SupportedInputModalities: []string{"text", "image"}, Thinking: thinking}
	a := &coreauth.Auth{Provider: "codex", Metadata: map[string]any{"models": []coreauth.CredentialModel{{Name: "native", Alias: "alias"}, {Name: "new"}}}}
	got, configured := buildCredentialModels(a, []*ModelInfo{source}, nil, false)
	if !configured || len(got) != 2 || got[0].ContextLength != 100 || !got[0].SupportConfigurationUpdate || len(got[0].SupportedInputModalities) != 2 {
		t.Fatalf("catalog capabilities not inherited: %#v", got)
	}
	got[0].Thinking.Levels[0] = "changed"
	if source.Thinking.Levels[0] != "low" || source.ID != "native" {
		t.Fatal("configuration mutated a shared catalog")
	}
	got, _ = buildCredentialModels(a, []*ModelInfo{source}, []string{"native"}, false)
	if len(got) != 1 || got[0].ID != "new" {
		t.Fatal("custom models bypassed upstream exclusions")
	}
	got, _ = buildCredentialModels(a, []*ModelInfo{source}, nil, true)
	if len(got) != 1 || got[0].ID != "alias" {
		t.Fatal("account-derived catalogs accepted an unentitled model")
	}
	a.Metadata["models"] = map[string]any{"not": "a list"}
	got, configured = buildCredentialModels(a, []*ModelInfo{source}, nil, false)
	if !configured || len(got) != 0 {
		t.Fatal("malformed file configuration published defaults")
	}
}

func TestCredentialModelCatalogSourcesAndPlans(t *testing.T) {
	service := &Service{cfg: &config.Config{}}
	for _, tc := range []struct {
		provider, plan string
		want           []*registry.ModelInfo
	}{
		{"codex", "free", registry.GetCodexFreeModels()}, {"codex", "plus", registry.GetCodexPlusModels()}, {"codex", "team", registry.GetCodexTeamModels()}, {"zcode", "", registry.GetZCodeModels()},
	} {
		a := &coreauth.Auth{Provider: tc.provider, Attributes: map[string]string{"plan_type": tc.plan}}
		catalog, err := service.credentialModelCatalog(context.Background(), a, false)
		if err != nil || len(catalog.Models) != len(tc.want) {
			t.Fatalf("%s/%s catalog: %#v %v", tc.provider, tc.plan, catalog, err)
		}
		if catalog.Source.UpstreamDiscovery {
			t.Fatal("static catalogs advertised account discovery")
		}
	}
	_, err := service.credentialModelCatalog(context.Background(), &coreauth.Auth{Provider: "zcode"}, true)
	if !errors.Is(err, coreauth.ErrCredentialModelsUnsupported) {
		t.Fatalf("built-in refresh: %v", err)
	}
	_, err = service.credentialModelCatalog(context.Background(), &coreauth.Auth{Provider: "openai-compatible"}, false)
	if !errors.Is(err, coreauth.ErrCredentialModelsUnsupported) {
		t.Fatalf("provider catalog: %v", err)
	}
}

func TestCredentialModelAntigravityDiscoveryRefreshesAccountAndRegistration(t *testing.T) {
	modelID := registry.GetAntigravityModels()[0].ID
	var calls, response atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != antigravityModelsPath || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("discovery did not use the selected account endpoint and credential")
		}
		switch response.Load() {
		case 1:
			_, _ = w.Write([]byte(`{"models":{}}`))
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = fmt.Fprintf(w, `{"models":{%q:{}},"webSearchModelIds":[%q]}`, modelID, modelID)
		}
	}))
	defer server.Close()
	manager := coreauth.NewManager(nil, nil, nil)
	a, err := manager.Register(context.Background(), &coreauth.Auth{
		ID: "credential-model-discovery-antigravity", Provider: "antigravity", Prefix: "account-antigravity#1",
		Attributes: map[string]string{"base_url": server.URL},
		Metadata:   map[string]any{"access_token": "test-token", "models": []coreauth.CredentialModel{{Name: modelID, Alias: "my-model"}, {Name: "unverified-model"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{cfg: &config.Config{}, coreManager: manager}
	service.cfg.ForceModelPrefix = true
	key := service.antigravityCapabilityKey(a)
	t.Cleanup(func() {
		service.WaitAntigravityProbes()
		registry.GetGlobalRegistry().UnregisterClient(a.ID)
		antigravityCapabilityMu.Lock()
		delete(antigravityCapabilityCache, key)
		for failureKey := range antigravityAuthFailureCache {
			if strings.HasPrefix(failureKey, key+"/") {
				delete(antigravityAuthFailureCache, failureKey)
			}
		}
		antigravityCapabilityMu.Unlock()
	})
	if _, err := service.credentialModelCatalog(t.Context(), a, false); !errors.Is(err, coreauth.ErrCredentialModelsUnavailable) || calls.Load() != 0 {
		t.Fatalf("uncached read initiated account discovery: %v", err)
	}
	catalog, err := service.credentialModelCatalog(t.Context(), a, true)
	service.WaitAntigravityProbes()
	if err != nil || len(catalog.Models) != 1 || calls.Load() != 1 || catalog.Source.Kind != "account_upstream" || !catalog.Source.UpstreamDiscovery {
		t.Fatalf("account discovery: %#v %v, calls=%d", catalog, err, calls.Load())
	}
	registered := registry.GetGlobalRegistry().GetModelsForClient(a.ID)
	if len(registered) != 1 || registered[0].ID != a.Prefix+"/my-model" || registered[0].MetadataModelID != modelID || !registered[0].SupportsWebSearch {
		t.Fatalf("account refresh did not preserve aliases and capabilities: %#v", registered)
	}
	if _, err := service.credentialModelCatalog(t.Context(), a, false); err != nil || calls.Load() != 1 {
		t.Fatalf("cached catalog read: %v, calls=%d", err, calls.Load())
	}
	response.Store(1)
	catalog, err = service.credentialModelCatalog(t.Context(), a, true)
	service.WaitAntigravityProbes()
	if err != nil || catalog.Models == nil || len(catalog.Models) != 0 || calls.Load() != 2 || len(registry.GetGlobalRegistry().GetModelsForClient(a.ID)) != 0 {
		t.Fatalf("authoritative empty account catalog was not applied: %#v %v", catalog, err)
	}
	response.Store(2)
	if _, err := service.credentialModelCatalog(t.Context(), a, true); !errors.Is(err, coreauth.ErrCredentialModelsRefreshFailed) {
		t.Fatalf("failed account refresh was reported as success: %v", err)
	}
	catalog, err = service.credentialModelCatalog(t.Context(), a, false)
	if err != nil || catalog.Models == nil || len(catalog.Models) != 0 {
		t.Fatalf("failed refresh discarded the last successful catalog: %#v %v", catalog, err)
	}
	current, _ := manager.GetByID(a.ID)
	models, configured := current.CredentialModels()
	if !configured || len(models) != 2 || models[0].Alias != "my-model" {
		t.Fatal("discovery changed the saved model configuration")
	}
	changed := current.Clone()
	changed.Metadata["models"] = []coreauth.CredentialModel{}
	if antigravityAuthModelSettingsEqual(current, changed) {
		t.Fatal("model configuration changes did not invalidate stale account probes")
	}
}
