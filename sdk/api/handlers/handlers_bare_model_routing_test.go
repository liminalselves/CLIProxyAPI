package handlers

import (
	"context"
	"strings"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func newChannelModeHandler(t *testing.T) (*BaseAPIHandler, func()) {
	t.Helper()
	manager := coreauth.NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{MultiUser: internalconfig.MultiUserConfig{Enabled: true}})
	auth := &coreauth.Auth{
		ID:       "bare-routing-auth",
		Provider: "kimi",
		Prefix:   "ch1-kimi#1",
		Status:   coreauth.StatusActive,
		Metadata: map[string]any{"owner": "alice", "channel_id": "ch1", "published": true},
	}
	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, "kimi", []*registry.ModelInfo{{ID: "ch1-kimi#1/bare-routing-model"}})
	handler := &BaseAPIHandler{
		Cfg:         &sdkconfig.SDKConfig{RequireChannelModel: true},
		AuthManager: manager,
	}
	return handler, func() { reg.UnregisterClient(auth.ID) }
}

func TestGetRequestDetailsChannelModeBareModel(t *testing.T) {
	handler, cleanup := newChannelModeHandler(t)
	defer cleanup()

	// Without the bare-model grant a bare name keeps the channel_required error.
	_, _, errMsg := handler.getRequestDetailsWithOptions("bare-routing-model", false, false)
	if errMsg == nil || errMsg.StatusCode != 400 || !strings.Contains(errMsg.Error.Error(), "channel-prefixed") {
		t.Fatalf("bare model without grant: %#v", errMsg)
	}

	// With the grant the providers resolve from the credentials publishing the model.
	providers, normalizedModel, errMsg := handler.getRequestDetailsWithOptions("bare-routing-model", false, true)
	if errMsg != nil {
		t.Fatalf("bare model with grant: %v", errMsg.Error)
	}
	if len(providers) != 1 || providers[0] != "kimi" {
		t.Fatalf("providers = %v, want [kimi]", providers)
	}
	if normalizedModel != "bare-routing-model" {
		t.Fatalf("normalizedModel = %q, want the bare model name", normalizedModel)
	}

	// Unknown bare models still fail with model_not_found.
	_, _, errMsg = handler.getRequestDetailsWithOptions("missing-bare-model", false, true)
	if errMsg == nil || !strings.Contains(errMsg.Error.Error(), "model_not_found") {
		t.Fatalf("missing bare model: %#v", errMsg)
	}

	// Pinned models keep resolving through the registry path.
	providers, _, errMsg = handler.getRequestDetailsWithOptions("ch1-kimi#1/bare-routing-model", false, false)
	if errMsg != nil {
		t.Fatalf("pinned model: %v", errMsg.Error)
	}
	if len(providers) != 1 || providers[0] != "kimi" {
		t.Fatalf("pinned providers = %v, want [kimi]", providers)
	}
}

func TestBareModelsAllowedContextFlag(t *testing.T) {
	if coreauth.BareModelsAllowed(nil) {
		t.Fatal("nil context must not allow bare models")
	}
	if coreauth.BareModelsAllowed(context.Background()) {
		t.Fatal("plain context must not allow bare models")
	}
	if !coreauth.BareModelsAllowed(coreauth.WithBareModelsAllowed(context.Background())) {
		t.Fatal("flagged context must allow bare models")
	}
}
