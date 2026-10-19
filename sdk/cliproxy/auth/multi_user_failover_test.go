package auth

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type bareModelFailoverExecutor struct {
	*bareModelRecordingExecutor
	errors            map[string]error
	prefix            [][]byte
	closeAfterPrelude bool
	onAttempt         func()
}

func (e *bareModelFailoverExecutor) execute(ctx context.Context, auth *Auth) error {
	cliproxyexecutor.MarkUpstreamAttempt(ctx)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.attempted = append(e.attempted, auth.ID)
	if e.onAttempt != nil {
		e.onAttempt()
	}
	return e.errors[auth.ID]
}

func (e *bareModelFailoverExecutor) Execute(ctx context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	if err := e.execute(ctx, auth); err != nil {
		return cliproxyexecutor.Response{}, err
	}
	return cliproxyexecutor.Response{Payload: []byte(`{"ok":true}`)}, nil
}

func (e *bareModelFailoverExecutor) CountTokens(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.Execute(ctx, auth, req, opts)
}

func (e *bareModelFailoverExecutor) ExecuteStream(ctx context.Context, auth *Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	err := e.execute(ctx, auth)
	if err != nil && len(e.prefix) == 0 {
		return nil, err
	}
	chunks := make(chan cliproxyexecutor.StreamChunk, len(e.prefix)+1)
	if err != nil {
		for _, payload := range e.prefix {
			chunks <- cliproxyexecutor.StreamChunk{Payload: payload}
		}
		if !e.closeAfterPrelude {
			chunks <- cliproxyexecutor.StreamChunk{Err: err}
		}
	} else {
		chunks <- cliproxyexecutor.StreamChunk{Payload: []byte("data: {\"ok\":true}\n\n")}
	}
	close(chunks)
	return &cliproxyexecutor.StreamResult{Headers: http.Header{"X-Channel": []string{auth.ID}}, Chunks: chunks}, nil
}

type bareModelCompatibleFailoverExecutor struct {
	*bareModelFailoverExecutor
}

func (e *bareModelCompatibleFailoverExecutor) Identifier() string {
	return "openai-compatible-test"
}

func runBareModelFailover(t *testing.T, manager *Manager, ctx context.Context, path, model string, opts cliproxyexecutor.Options, providers ...string) error {
	t.Helper()
	if len(providers) == 0 {
		providers = []string{"kimi"}
	}
	req := cliproxyexecutor.Request{Model: model}
	switch path {
	case "count":
		_, err := manager.ExecuteCount(ctx, providers, req, opts)
		return err
	case "stream":
		stream, err := manager.ExecuteStream(ctx, providers, req, opts)
		if stream != nil {
			for chunk := range stream.Chunks {
				if chunk.Err != nil {
					err = chunk.Err
				}
			}
		}
		return err
	default:
		_, err := manager.Execute(ctx, providers, req, opts)
		return err
	}
}

func newBareModelFailoverManager(t *testing.T, firstError error, prefix ...string) (*Manager, *bareModelFailoverExecutor) {
	t.Helper()
	manager, recorder := newBareModelManager(t)
	manager.SetRetryConfig(0, 0, 0)
	executor := &bareModelFailoverExecutor{bareModelRecordingExecutor: recorder, errors: map[string]error{"hub-kimi-a": firstError}}
	for _, payload := range prefix {
		executor.prefix = append(executor.prefix, []byte(payload))
	}
	manager.RegisterExecutor(executor)
	return manager, executor
}

func TestBareModelRotationIsolatesModels(t *testing.T) {
	for _, path := range []string{"execute", "count", "stream"} {
		for _, provider := range []string{"kimi", "openai-compatible-test"} {
			t.Run(path+"/"+provider, func(t *testing.T) {
				manager, executor := newBareModelFailoverManager(t, nil)
				if provider != "kimi" {
					manager.RegisterExecutor(&bareModelCompatibleFailoverExecutor{executor})
				}
				reg := registry.GetGlobalRegistry()
				reg.RegisterClient("hub-kimi-a", "kimi", []*registry.ModelInfo{
					{ID: "ch1-kimi#1/kimi-k3"},
					{ID: "ch1-kimi#1/rotation-peer"},
				})
				_, err := manager.Register(context.Background(), &Auth{
					ID: "hub-kimi-c", Provider: provider, Prefix: "ch6-kimi#1", Status: StatusActive,
					Metadata: map[string]any{"owner": "alice", "channel_id": "rotation-channel", "published": true},
				})
				if err != nil {
					t.Fatal(err)
				}
				reg.RegisterClient("hub-kimi-c", provider, []*registry.ModelInfo{{ID: "ch6-kimi#1/rotation-peer"}})
				t.Cleanup(func() { reg.UnregisterClient("hub-kimi-c") })

				ctx := WithBareModelsAllowed(context.Background())
				for i := range 6 {
					model := "kimi-k3"
					if i%2 == 1 {
						model += "(high)"
					}
					for _, requested := range []string{model, "rotation-peer"} {
						if err := runBareModelFailover(t, manager, ctx, path, requested, cliproxyexecutor.Options{}, "kimi", provider); err != nil {
							t.Fatal(err)
						}
					}
				}

				attempts := executor.attempts()
				if len(attempts) != 12 {
					t.Fatalf("attempts = %v, want 12 successful requests", attempts)
				}
				counts := [2]map[string]int{{}, {}}
				for i, id := range attempts {
					counts[i%2][id]++
				}
				want := [2]map[string]int{
					{"hub-kimi-a": 3, "hub-kimi-b": 3},
					{"hub-kimi-a": 3, "hub-kimi-c": 3},
				}
				if !reflect.DeepEqual(counts, want) {
					t.Fatalf("per-model distribution = %v, want %v; attempts = %v", counts, want, attempts)
				}
			})
		}
	}
}

func TestBareModelFailoverRequestErrors(t *testing.T) {
	for _, path := range []string{"execute", "count", "stream"} {
		for _, status := range []int{400, 401, 403, 404, 409, 413, 422, 429, 500, 502, 503} {
			t.Run(path+"/"+http.StatusText(status), func(t *testing.T) {
				firstError := &Error{HTTPStatus: status, Message: `{"error":{"type":"invalid_request_error","message":"channel rejected request"}}`}
				manager, executor := newBareModelFailoverManager(t, firstError)
				ctx := WithBareModelsAllowed(context.Background())
				if err := runBareModelFailover(t, manager, ctx, path, "kimi-k3", cliproxyexecutor.Options{}); err != nil {
					t.Fatalf("healthy second channel was not used: %v", err)
				}
				if got := executor.attempts(); !reflect.DeepEqual(got, []string{"hub-kimi-a", "hub-kimi-b"}) {
					t.Fatalf("attempts = %v, want both channels", got)
				}
			})
		}
	}
}

func TestBareModelFailoverExhaustsChannels(t *testing.T) {
	for _, path := range []string{"execute", "count", "stream"} {
		for _, action := range []string{"", RequestScopedActionStop, RequestScopedActionStopAndCooldown} {
			t.Run(path+"/"+action, func(t *testing.T) {
				manager, executor := newBareModelFailoverManager(t, &Error{HTTPStatus: 400, Message: "first failure"})
				lastError := &Error{HTTPStatus: 422, Message: "last failure"}
				executor.errors["hub-kimi-b"] = lastError
				manager.SetRetryConfig(3, 0, 1)
				if action != "" {
					auth, _ := manager.GetByID("hub-kimi-a")
					auth.Metadata["request_scoped_errors"] = []internalconfig.RequestScopedErrorRule{
						{Status: 400, Match: []string{"first failure"}, Action: action},
					}
					if _, err := manager.Update(context.Background(), auth); err != nil {
						t.Fatal(err)
					}
					if got, ok := matchRequestScopedErrorAction(auth, executor.errors[auth.ID], manager.runtimeConfigSnapshot()); !ok || got != action {
						t.Fatalf("request rule = %q, %v, want %q", got, ok, action)
					}
				}
				err := runBareModelFailover(t, manager, WithBareModelsAllowed(context.Background()), path, "kimi-k3", cliproxyexecutor.Options{})
				if !errors.Is(err, lastError) {
					t.Fatalf("error = %v, want last channel failure %v", err, lastError)
				}
				if got := executor.attempts(); !reflect.DeepEqual(got, []string{"hub-kimi-a", "hub-kimi-b"}) {
					t.Fatalf("attempts = %v, want both channels despite credential retry cap", got)
				}
			})
		}
	}
}

func TestBareModelFailoverAcrossProviders(t *testing.T) {
	for _, path := range []string{"execute", "count", "stream"} {
		t.Run(path, func(t *testing.T) {
			manager, executor := newBareModelFailoverManager(t, &Error{HTTPStatus: 400, Message: "first provider failed"})
			compatible := &bareModelCompatibleFailoverExecutor{executor}
			manager.RegisterExecutor(compatible)
			auth, _ := manager.GetByID("hub-kimi-b")
			auth.Provider = compatible.Identifier()
			if _, err := manager.Update(context.Background(), auth); err != nil {
				t.Fatal(err)
			}
			err := runBareModelFailover(t, manager, WithBareModelsAllowed(context.Background()), path, "kimi-k3", cliproxyexecutor.Options{}, "kimi", compatible.Identifier())
			if err != nil {
				t.Fatalf("healthy compatible provider was not used: %v", err)
			}
			if got := executor.attempts(); !reflect.DeepEqual(got, []string{"hub-kimi-a", "hub-kimi-b"}) {
				t.Fatalf("attempts = %v, want failover across provider types", got)
			}
		})
	}
}

func TestBareModelFailoverPreservesExplicitRouting(t *testing.T) {
	for _, path := range []string{"execute", "count", "stream"} {
		for _, mode := range []string{"disabled", "prefixed", "pinned"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				firstError := &Error{HTTPStatus: 400, Message: "first failure"}
				manager, executor := newBareModelFailoverManager(t, firstError)
				ctx := context.Background()
				model := "kimi-k3"
				opts := cliproxyexecutor.Options{}
				if mode != "disabled" {
					ctx = WithBareModelsAllowed(ctx)
				}
				if mode == "prefixed" {
					model = "ch1-kimi#1/kimi-k3"
				}
				if mode == "pinned" {
					opts.Metadata = map[string]any{cliproxyexecutor.PinnedAuthMetadataKey: "hub-kimi-a"}
				}
				if err := runBareModelFailover(t, manager, ctx, path, model, opts); !errors.Is(err, firstError) {
					t.Fatalf("error = %v, want original request failure", err)
				}
				if got := executor.attempts(); !reflect.DeepEqual(got, []string{"hub-kimi-a"}) {
					t.Fatalf("attempts = %v, explicit routing must not switch channels", got)
				}
			})
		}
	}
}

func TestBareModelFailoverStreamPrelude(t *testing.T) {
	cases := []struct {
		name   string
		prefix []string
	}{
		{name: "responses", prefix: []string{"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"output\":[]}}\n\n", "data: {\"type\":\"response.in_progress\"}"}},
		{name: "split responses", prefix: []string{"data: {\"type\":\"response.cre", "ated\"}\n\n"}},
		{name: "chat", prefix: []string{"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":null}]}\n\n"}},
		{name: "messages", prefix: []string{"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"content\":[]}}\n\n", "data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"}},
		{name: "heartbeat", prefix: []string{": ping\n\n", "data: {\"type\":\"ping\"}\n\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager, executor := newBareModelFailoverManager(t, &Error{HTTPStatus: 502, Message: "failed before output"}, tc.prefix...)
			stream, err := manager.ExecuteStream(WithBareModelsAllowed(context.Background()), []string{"kimi"}, cliproxyexecutor.Request{Model: "kimi-k3"}, cliproxyexecutor.Options{})
			if err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			for chunk := range stream.Chunks {
				if chunk.Err != nil {
					t.Fatalf("first channel failure escaped failover: %v", chunk.Err)
				}
				output.Write(chunk.Payload)
			}
			if got := executor.attempts(); !reflect.DeepEqual(got, []string{"hub-kimi-a", "hub-kimi-b"}) {
				t.Fatalf("attempts = %v, want both channels", got)
			}
			if got := stream.Headers.Get("X-Channel"); got != "hub-kimi-b" {
				t.Fatalf("response headers belong to %q, want successful channel", got)
			}
			if output.String() != "data: {\"ok\":true}\n\n" {
				t.Fatalf("failed channel prelude leaked: %q", output.String())
			}
		})
	}
}

func TestBareModelFailoverCancellation(t *testing.T) {
	for _, path := range []string{"execute", "count", "stream"} {
		t.Run(path, func(t *testing.T) {
			manager, executor := newBareModelFailoverManager(t, &Error{HTTPStatus: 400, Message: "failed"})
			ctx, cancel := context.WithCancel(WithBareModelsAllowed(context.Background()))
			defer cancel()
			executor.onAttempt = cancel
			if err := runBareModelFailover(t, manager, ctx, path, "kimi-k3", cliproxyexecutor.Options{}); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want cancellation", err)
			}
			if got := executor.attempts(); !reflect.DeepEqual(got, []string{"hub-kimi-a"}) {
				t.Fatalf("attempts = %v, cancellation must stop failover", got)
			}
		})
	}
}

func TestBareModelFailoverStreamPreludeEOF(t *testing.T) {
	manager, executor := newBareModelFailoverManager(t, &Error{HTTPStatus: 502}, "data: {\"type\":\"response.created\"}\n\n")
	executor.closeAfterPrelude = true
	if err := runBareModelFailover(t, manager, WithBareModelsAllowed(context.Background()), "stream", "kimi-k3", cliproxyexecutor.Options{}); err != nil {
		t.Fatal(err)
	}
	if got := executor.attempts(); !reflect.DeepEqual(got, []string{"hub-kimi-a", "hub-kimi-b"}) {
		t.Fatalf("attempts = %v, prelude-only stream must fail over", got)
	}
}

func TestBareModelFailoverStreamLastError(t *testing.T) {
	for _, action := range []string{"", RequestScopedActionStop, RequestScopedActionStopAndCooldown} {
		t.Run(action, func(t *testing.T) {
			manager, executor := newBareModelFailoverManager(t, &Error{HTTPStatus: 400, Message: "first failure"}, "data: {\"type\":\"response.created\"}\n\n")
			lastError := &Error{HTTPStatus: 422, Message: "last failure"}
			executor.errors["hub-kimi-b"] = lastError
			if action != "" {
				auth, _ := manager.GetByID("hub-kimi-a")
				auth.Metadata["request_scoped_errors"] = []internalconfig.RequestScopedErrorRule{{Status: 400, Match: []string{"first failure"}, Action: action}}
				if _, err := manager.Update(context.Background(), auth); err != nil {
					t.Fatal(err)
				}
			}
			stream, err := manager.ExecuteStream(WithBareModelsAllowed(context.Background()), []string{"kimi"}, cliproxyexecutor.Request{Model: "kimi-k3"}, cliproxyexecutor.Options{})
			if stream != nil {
				if got := stream.Headers.Get("X-Channel"); got != "hub-kimi-b" {
					t.Fatalf("last error headers = %q, want last channel", got)
				}
				for chunk := range stream.Chunks {
					if len(chunk.Payload) > 0 {
						t.Fatalf("failed channel prelude leaked: %q", chunk.Payload)
					}
					if chunk.Err != nil {
						err = chunk.Err
					}
				}
			}
			if !errors.Is(err, lastError) {
				t.Fatalf("error = %v, want last stream failure", err)
			}
			if got := executor.attempts(); !reflect.DeepEqual(got, []string{"hub-kimi-a", "hub-kimi-b"}) {
				t.Fatalf("attempts = %v, want both channels", got)
			}
		})
	}
}

func TestBareModelFailoverDoesNotReplayOutput(t *testing.T) {
	for _, payload := range []string{
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n",
		"data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"web_search_call\"}}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n",
		"data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"tool_use\",\"name\":\"run\"}}\n\n",
		"data: {\"type\":\"unknown_event\"}\n\n",
	} {
		t.Run(payload, func(t *testing.T) {
			firstError := &Error{HTTPStatus: 502, Message: "failed after output"}
			manager, executor := newBareModelFailoverManager(t, firstError, payload)
			err := runBareModelFailover(t, manager, WithBareModelsAllowed(context.Background()), "stream", "kimi-k3", cliproxyexecutor.Options{})
			if !errors.Is(err, firstError) {
				t.Fatalf("error = %v, want in-stream failure", err)
			}
			if got := executor.attempts(); !reflect.DeepEqual(got, []string{"hub-kimi-a"}) {
				t.Fatalf("attempts = %v, must not replay output or tool operations", got)
			}
		})
	}
}
