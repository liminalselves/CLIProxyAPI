package cliproxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestUpstreamModelsEndpointResolution(t *testing.T) {
	zcodeAuth := &coreauth.Auth{Provider: "zcode", Metadata: map[string]any{"access_token": "token"}}
	baseURL, apiKey := upstreamModelsEndpoint(zcodeAuth, "zcode")
	if baseURL == "" || apiKey != "token" {
		t.Fatalf("zcode endpoint = %q, %q", baseURL, apiKey)
	}
	custom := &coreauth.Auth{Provider: "zcode", Metadata: map[string]any{"base_url": " https://example.com/v1 ", "access_token": "t2"}}
	baseURL, _ = upstreamModelsEndpoint(custom, "zcode")
	if baseURL != "https://example.com/v1" {
		t.Fatalf("custom base_url = %q", baseURL)
	}
	compat := &coreauth.Auth{Provider: "openai-compatible", Metadata: map[string]any{"base_url": "https://api.example.com", "api_key": "sk-1"}}
	baseURL, apiKey = upstreamModelsEndpoint(compat, "openai-compatible")
	if baseURL != "https://api.example.com" || apiKey != "sk-1" {
		t.Fatalf("compat endpoint = %q, %q", baseURL, apiKey)
	}
	attrFallback := &coreauth.Auth{Provider: "openai-compatible", Attributes: map[string]string{"api_key": "sk-attr"}, Metadata: map[string]any{"base_url": "https://api.example.com"}}
	if _, apiKey = upstreamModelsEndpoint(attrFallback, "openai-compatible"); apiKey != "sk-attr" {
		t.Fatalf("attribute fallback apiKey = %q", apiKey)
	}
}

func TestUpstreamCredentialModelCatalogFetchAndCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Fatalf("missing bearer token: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"object":"list","data":[{"id":"deepseek-v4-pro","object":"model","owned_by":"deepseek"},{"id":"deepseek-flash"},{"id":"deepseek-flash"},{"id":"  "}]}`)
	}))
	defer server.Close()

	a := &coreauth.Auth{ID: "compat-cache-test", Provider: "openai-compatible", Metadata: map[string]any{"base_url": server.URL + "/v1", "api_key": "sk-test"}}
	service := &Service{cfg: &config.Config{}}

	catalog, err := service.credentialModelCatalog(context.Background(), a, true)
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.Source.SupportsRefresh || catalog.Source.Kind != "account_upstream" || !catalog.Source.UpstreamDiscovery {
		t.Fatalf("source = %#v", catalog.Source)
	}
	if len(catalog.Models) != 2 || catalog.Models[0].ID != "deepseek-v4-pro" || catalog.Models[0].OwnedBy != "deepseek" {
		t.Fatalf("models = %#v", catalog.Models)
	}

	// Cached listing is served without the upstream (server behavior no longer matters).
	cached, err := service.credentialModelCatalog(context.Background(), a, false)
	if err != nil || len(cached.Models) != 2 {
		t.Fatalf("cached catalog = %#v, %v", cached, err)
	}
}

func TestUpstreamCredentialModelCatalogRefreshFailures(t *testing.T) {
	service := &Service{cfg: &config.Config{}}
	missing := &coreauth.Auth{ID: "compat-no-key", Provider: "openai-compatible", Metadata: map[string]any{}}
	if _, err := service.credentialModelCatalog(context.Background(), missing, true); !errors.Is(err, coreauth.ErrCredentialModelsRefreshFailed) {
		t.Fatalf("missing key err = %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	unauthorized := &coreauth.Auth{ID: "compat-401", Provider: "openai-compatible", Metadata: map[string]any{"base_url": server.URL, "api_key": "sk-bad"}}
	if _, err := service.credentialModelCatalog(context.Background(), unauthorized, true); !errors.Is(err, coreauth.ErrCredentialModelsRefreshFailed) {
		t.Fatalf("401 err = %v", err)
	}
}

func TestUpstreamCredentialModelCatalogZCodeFallback(t *testing.T) {
	service := &Service{cfg: &config.Config{}}
	a := &coreauth.Auth{ID: "zcode-fallback-test", Provider: "zcode", Metadata: map[string]any{"access_token": "token"}}
	catalog, err := service.credentialModelCatalog(context.Background(), a, false)
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.Source.SupportsRefresh || catalog.Source.Kind != "account_upstream" {
		t.Fatalf("source = %#v", catalog.Source)
	}
	if len(catalog.Models) != len(registry.GetZCodeModels()) {
		t.Fatalf("fallback models = %#v", catalog.Models)
	}
}

func TestSupportsCredentialModelsOpenAICompatible(t *testing.T) {
	compat := &coreauth.Auth{Provider: "openai-compatible", Metadata: map[string]any{"api_key": "sk-1"}}
	if !coreauth.SupportsCredentialModels(compat) {
		t.Fatal("openai-compatible should support model management")
	}
	configKey := &coreauth.Auth{Provider: "openai-compatible", Attributes: map[string]string{"api_key": "sk-1", "source": "config"}}
	if coreauth.SupportsCredentialModels(configKey) {
		t.Fatal("config-backed keys must stay unsupported")
	}
}
