package auth

import (
	"context"
	"net/http"
	"sync"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

// bareModelRecordingExecutor records the auth IDs it served and fails for the
// configured auth IDs with a retry-round-eligible status.
type bareModelRecordingExecutor struct {
	mu        sync.Mutex
	failing   map[string]struct{}
	attempted []string
}

func (e *bareModelRecordingExecutor) Identifier() string { return "kimi" }

func (e *bareModelRecordingExecutor) Execute(ctx context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	cliproxyexecutor.MarkUpstreamAttempt(ctx)
	if err := e.record(auth); err != nil {
		return cliproxyexecutor.Response{}, err
	}
	return cliproxyexecutor.Response{Payload: []byte(`{}`)}, nil
}

func (e *bareModelRecordingExecutor) ExecuteStream(ctx context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	cliproxyexecutor.MarkUpstreamAttempt(ctx)
	if err := e.record(auth); err != nil {
		return nil, err
	}
	return &cliproxyexecutor.StreamResult{}, nil
}

func (e *bareModelRecordingExecutor) record(auth *Auth) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.attempted = append(e.attempted, auth.ID)
	if _, fail := e.failing[auth.ID]; fail {
		return &Error{HTTPStatus: http.StatusBadGateway, Message: "upstream failed"}
	}
	return nil
}

func (e *bareModelRecordingExecutor) attempts() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.attempted...)
}

func (*bareModelRecordingExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (*bareModelRecordingExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (*bareModelRecordingExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func newBareModelManager(t *testing.T) (*Manager, *bareModelRecordingExecutor) {
	t.Helper()
	manager := NewManager(nil, nil, nil)
	manager.SetConfig(&internalconfig.Config{MultiUser: internalconfig.MultiUserConfig{Enabled: true}})
	executor := &bareModelRecordingExecutor{failing: map[string]struct{}{}}
	manager.RegisterExecutor(executor)

	auths := []struct {
		id       string
		prefix   string
		enabled  any
		publish  bool
		register []string
	}{
		{id: "hub-kimi-a", prefix: "ch1-kimi#1", publish: true, register: []string{"ch1-kimi#1/kimi-k3"}},
		{id: "hub-kimi-b", prefix: "ch2-kimi#1", publish: true, register: []string{"ch2-kimi#1/kimi-k3"}},
		{id: "hub-kimi-off", prefix: "ch3-kimi#1", publish: false, register: []string{"ch3-kimi#1/kimi-k3"}},
		{id: "hub-kimi-limited", prefix: "ch4-kimi#1", publish: true, enabled: []any{"other-model"}, register: []string{"ch4-kimi#1/other-model"}},
		{id: "hub-kimi-noreg", prefix: "ch5-kimi#1", publish: true, register: []string{"ch5-kimi#1/other-model"}},
	}
	reg := registry.GetGlobalRegistry()
	for _, item := range auths {
		metadata := map[string]any{"owner": "alice", "channel_id": item.id + "-channel"}
		if item.publish {
			metadata["published"] = true
		} else {
			metadata["published"] = false
		}
		if item.enabled != nil {
			metadata["enabled_models"] = item.enabled
		}
		if _, err := manager.Register(context.Background(), &Auth{ID: item.id, Provider: "kimi", Prefix: item.prefix, Status: StatusActive, Metadata: metadata}); err != nil {
			t.Fatalf("register auth %s: %v", item.id, err)
		}
		models := make([]*registry.ModelInfo, 0, len(item.register))
		for _, id := range item.register {
			models = append(models, &registry.ModelInfo{ID: id})
		}
		reg.RegisterClient(item.id, "kimi", models)
	}
	t.Cleanup(func() {
		for _, item := range auths {
			reg.UnregisterClient(item.id)
		}
	})
	return manager, executor
}

func TestAuthSupportsRouteModelBareBaseModel(t *testing.T) {
	manager, _ := newBareModelManager(t)
	registryRef := registry.GetGlobalRegistry()

	hubA := mustGetAuth(t, manager, "hub-kimi-a")
	hubB := mustGetAuth(t, manager, "hub-kimi-b")
	unpublished := mustGetAuth(t, manager, "hub-kimi-off")
	limited := mustGetAuth(t, manager, "hub-kimi-limited")
	noreg := mustGetAuth(t, manager, "hub-kimi-noreg")

	if !manager.authSupportsRouteModel(registryRef, hubA, "kimi-k3") {
		t.Error("bare model should match a published credential exposing it")
	}
	if !manager.authSupportsRouteModel(registryRef, hubB, "kimi-k3(high)") {
		t.Error("bare model with thinking suffix should strip the suffix before matching")
	}
	if manager.authSupportsRouteModel(registryRef, unpublished, "kimi-k3") {
		t.Error("bare model must not match an unpublished channel credential")
	}
	if manager.authSupportsRouteModel(registryRef, limited, "kimi-k3") {
		t.Error("bare model must not match a credential whose enabled_models allowlist lacks it")
	}
	if manager.authSupportsRouteModel(registryRef, noreg, "kimi-k3") {
		t.Error("bare model must not match a credential that does not register the model")
	}
	if !manager.authSupportsRouteModel(registryRef, hubA, "ch1-kimi#1/kimi-k3") {
		t.Error("pinned model should keep matching its own channel credential")
	}
	if manager.authSupportsRouteModel(registryRef, hubB, "ch1-kimi#1/kimi-k3") {
		t.Error("pinned model must stay bound to its own channel credential")
	}
}

func mustGetAuth(t *testing.T, manager *Manager, id string) *Auth {
	t.Helper()
	auth, ok := manager.GetByID(id)
	if !ok || auth == nil {
		t.Fatalf("auth %s not registered", id)
	}
	return auth
}

func TestProvidersForBaseModel(t *testing.T) {
	manager, _ := newBareModelManager(t)

	providers := manager.ProvidersForBaseModel("kimi-k3")
	if len(providers) != 1 || providers[0] != "kimi" {
		t.Fatalf("ProvidersForBaseModel(kimi-k3) = %v, want [kimi]", providers)
	}
	if got := manager.ProvidersForBaseModel("missing-model"); got != nil {
		t.Fatalf("ProvidersForBaseModel(missing-model) = %v, want nil", got)
	}
}

func TestBareModelExecutesAcrossChannelsWithFailover(t *testing.T) {
	manager, executor := newBareModelManager(t)
	executor.failing["hub-kimi-a"] = struct{}{}

	resp, err := manager.Execute(context.Background(), []string{"kimi"}, cliproxyexecutor.Request{Model: "kimi-k3"}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("bare model execution should fail over to the second channel: %v", err)
	}
	attempts := executor.attempts()
	if len(attempts) != 2 {
		t.Fatalf("executor attempts = %v, want both channels tried", attempts)
	}
	if attempts[0] != "hub-kimi-a" || attempts[1] != "hub-kimi-b" {
		t.Fatalf("executor attempts = %v, want [hub-kimi-a hub-kimi-b]", attempts)
	}
	if len(resp.Payload) == 0 {
		t.Fatal("expected a response payload")
	}
}

func TestBareModelRotationSpreadsRequests(t *testing.T) {
	manager, executor := newBareModelManager(t)

	if _, err := manager.Execute(context.Background(), []string{"kimi"}, cliproxyexecutor.Request{Model: "kimi-k3"}, cliproxyexecutor.Options{}); err != nil {
		t.Fatalf("first execution failed: %v", err)
	}
	first := executor.attempts()[0]
	if _, err := manager.Execute(context.Background(), []string{"kimi"}, cliproxyexecutor.Request{Model: "kimi-k3"}, cliproxyexecutor.Options{}); err != nil {
		t.Fatalf("second execution failed: %v", err)
	}
	second := executor.attempts()[1]
	if first == second {
		t.Fatalf("two sequential bare model calls both used auth %s; round-robin should rotate", first)
	}
}

func TestPinnedModelStaysWithinChannel(t *testing.T) {
	manager, executor := newBareModelManager(t)
	executor.failing["hub-kimi-a"] = struct{}{}

	_, err := manager.Execute(context.Background(), []string{"kimi"}, cliproxyexecutor.Request{Model: "ch1-kimi#1/kimi-k3"}, cliproxyexecutor.Options{})
	if err == nil {
		t.Fatal("pinned model failure should not spill into another channel")
	}
	attempts := executor.attempts()
	for _, id := range attempts {
		if id != "hub-kimi-a" {
			t.Fatalf("pinned model attempted auth %s outside its channel", id)
		}
	}
}
