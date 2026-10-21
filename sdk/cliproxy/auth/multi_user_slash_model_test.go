package auth

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

const slashModel = "vanchin/deepseek-v4.1-flash"

func registerSlashModel(manager *Manager) {
	reg := registry.GetGlobalRegistry()
	for _, id := range []string{"hub-kimi-a", "hub-kimi-b"} {
		auth, _ := manager.GetByID(id)
		reg.RegisterClient(id, "kimi", []*registry.ModelInfo{{ID: auth.Prefix + "/" + slashModel}})
	}
}

func TestSlashModelFailoverAndExplicitRouting(t *testing.T) {
	for _, path := range []string{"execute", "count", "stream"} {
		for _, mode := range []string{"bare", "prefixed", "pinned", "all-failed", "prelude"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				firstErr := &Error{HTTPStatus: 400, Message: "first failure"}
				manager, executor := newBareModelFailoverManager(t, firstErr)
				registerSlashModel(manager)
				model := slashModel
				opts := cliproxyexecutor.Options{}
				wantAttempts := []string{"hub-kimi-a", "hub-kimi-b"}
				var wantErr error
				switch mode {
				case "prefixed":
					model = "ch1-kimi#1/" + slashModel
					wantAttempts, wantErr = wantAttempts[:1], firstErr
				case "pinned":
					opts.Metadata = map[string]any{cliproxyexecutor.PinnedAuthMetadataKey: "hub-kimi-a"}
					wantAttempts, wantErr = wantAttempts[:1], firstErr
				case "all-failed":
					wantErr = &Error{HTTPStatus: 422, Message: "last failure"}
					executor.errors["hub-kimi-b"] = wantErr
				case "prelude":
					executor.prefix = [][]byte{[]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n")}
				}
				err := runBareModelFailover(t, manager, WithBareModelsAllowed(context.Background()), path, model, opts)
				if !errors.Is(err, wantErr) {
					t.Fatalf("error = %v, want %v", err, wantErr)
				}
				if got := executor.attempts(); !reflect.DeepEqual(got, wantAttempts) {
					t.Fatalf("attempts = %v, want %v", got, wantAttempts)
				}
			})
		}
	}
}

func TestSlashModelRotation(t *testing.T) {
	for _, path := range []string{"execute", "count", "stream"} {
		t.Run(path, func(t *testing.T) {
			manager, executor := newBareModelFailoverManager(t, nil)
			registerSlashModel(manager)
			if got := manager.ProvidersForBaseModel(slashModel); !reflect.DeepEqual(got, []string{"kimi"}) {
				t.Fatalf("providers = %v", got)
			}
			for range 4 {
				if err := runBareModelFailover(t, manager, WithBareModelsAllowed(context.Background()), path, slashModel, cliproxyexecutor.Options{}); err != nil {
					t.Fatal(err)
				}
			}
			if got := executor.attempts(); !reflect.DeepEqual(got, []string{"hub-kimi-a", "hub-kimi-b", "hub-kimi-a", "hub-kimi-b"}) {
				t.Fatalf("attempts = %v, want alternating channels", got)
			}
		})
	}
}

func TestSlashModelPrefixBoundaries(t *testing.T) {
	manager, _ := newBareModelManager(t)
	registerSlashModel(manager)
	auth := mustGetAuth(t, manager, "hub-kimi-a")
	pinned := auth.Prefix + "/" + slashModel
	if got := manager.ModelRoutingPrefix(slashModel); got != "" {
		t.Fatalf("upstream namespace treated as prefix: %q", got)
	}
	if got := manager.ModelRoutingPrefix(pinned + "(high)"); got != auth.Prefix {
		t.Fatalf("pinned prefix = %q, want %q", got, auth.Prefix)
	}
	if got := rewriteModelForAuth(pinned, auth); got != slashModel {
		t.Fatalf("upstream model = %q, want %q", got, slashModel)
	}
	auth.Disabled, auth.Status = true, StatusDisabled
	if _, err := manager.Update(context.Background(), auth); err != nil {
		t.Fatal(err)
	}
	if manager.ModelRoutingPrefix(pinned) != auth.Prefix {
		t.Fatal("disabled credentials must retain the pinned routing boundary")
	}
	other := mustGetAuth(t, manager, "hub-kimi-b")
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(other.ID, "kimi", []*registry.ModelInfo{{ID: other.Prefix + "/" + pinned}})
	if manager.authSupportsRouteModel(reg, other, pinned) {
		t.Fatal("an explicit route must not become a namespaced model on another channel")
	}
	if _, err := manager.Execute(WithBareModelsAllowed(context.Background()), []string{"kimi"}, cliproxyexecutor.Request{Model: pinned}, cliproxyexecutor.Options{}); err == nil {
		t.Fatal("a disabled explicit route must not execute as another channel's base model")
	}
}

func TestSlashModelSelectionExclusions(t *testing.T) {
	for _, path := range []string{"execute", "count", "stream"} {
		for _, scope := range []string{slashModel, "ch1-kimi#1/" + slashModel, "ch1-kimi#1", "deepseek-v4.1-flash"} {
			t.Run(path+"/"+scope, func(t *testing.T) {
				manager, executor := newBareModelFailoverManager(t, nil)
				registerSlashModel(manager)
				ctx := WithBlockedModels(WithBareModelsAllowed(context.Background()), []string{scope})
				err := runBareModelFailover(t, manager, ctx, path, slashModel, cliproxyexecutor.Options{})
				if scope == slashModel {
					var blocked *Error
					if !errors.As(err, &blocked) || blocked.Code != "model_blocked" || blocked.HTTPStatus != 403 || len(executor.attempts()) != 0 {
						t.Fatalf("excluded model executed: attempts = %v, error = %v", executor.attempts(), err)
					}
					return
				}
				want := []string{"hub-kimi-b"}
				if scope == "deepseek-v4.1-flash" {
					want = []string{"hub-kimi-a"}
				}
				if err != nil || !reflect.DeepEqual(executor.attempts(), want) {
					t.Fatalf("attempts = %v, want %v; error = %v", executor.attempts(), want, err)
				}
			})
		}
	}
}

func TestSlashModelExclusions(t *testing.T) {
	auth := channelCredential("123-provider", "123", "")
	other := channelCredential("456-provider", "456", "")
	for _, scope := range []string{slashModel, auth.Prefix + "/" + slashModel, auth.Prefix, "123"} {
		ctx := WithBlockedModels(context.Background(), []string{scope})
		for _, requested := range []string{slashModel, auth.Prefix + "/" + slashModel, slashModel + "(high)"} {
			if !AuthModelBlocked(ctx, auth, requested) {
				t.Fatalf("scope %q failed to block %q", scope, requested)
			}
		}
		if scope != slashModel && AuthModelBlocked(ctx, other, slashModel) {
			t.Fatalf("scope %q blocked another channel", scope)
		}
	}
	ctx := WithBlockedModels(context.Background(), []string{"deepseek-v4.1-flash"})
	if ModelBlocked(ctx, slashModel) || AuthModelBlocked(ctx, auth, slashModel) || AuthModelBlocked(ctx, auth, auth.Prefix+"/"+slashModel) {
		t.Fatal("a namespaced model must not inherit exclusions for its last segment")
	}
}
