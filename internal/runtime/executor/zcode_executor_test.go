package executor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"github.com/tidwall/gjson"
)

type zcodeRoundTripper func(*http.Request) (*http.Response, error)

func (f zcodeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestZCodeRefreshDoesNotMutateLiveCredential(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = zcodeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "bigmodel.cn" || !strings.HasSuffix(r.URL.Path, "/api_keys/key/access_tokens") || r.Header.Get("Authorization") != "Bearer account-token" {
			t.Errorf("wrong token renewal request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"code":0,"data":{"accessToken":"fresh-token","tokenType":"Bearer","expiresIn":600,"expiresAt":%d}}`, time.Now().Add(10*time.Minute).Unix())))}, nil
	})
	live := &coreauth.Auth{Provider: "zcode", Metadata: map[string]any{"business_access_token": "account-token", "organization_id": "org", "project_id": "project", "api_key_id": "key", "access_token": "old-token", "expired": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)}}
	executor := NewZCodeExecutor(&config.Config{})
	if !executor.ShouldPrepareRequestAuth(live) {
		t.Fatal("expired token was not selected for renewal")
	}
	refreshed, errRefresh := executor.PrepareRequestAuth(context.Background(), live)
	if errRefresh != nil {
		t.Fatal(errRefresh)
	}
	if refreshed.Metadata["access_token"] != "fresh-token" || live.Metadata["access_token"] != "old-token" || refreshed == live {
		t.Fatal("refresh mutated live credential")
	}
	if executor.ShouldPrepareRequestAuth(refreshed) {
		t.Fatal("fresh token still needs renewal")
	}
}

func TestZCodeMetadataOnlyExecutionAndPayloadBarrier(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer project-token" {
					t.Error("inference did not use project token")
				}
				body, errRead := io.ReadAll(r.Body)
				if errRead != nil {
					t.Error(errRead)
				}
				if gjson.GetBytes(body, "max_tokens").Int() != 17 || gjson.GetBytes(body, "messages.#").Int() != 1 || gjson.GetBytes(body, "metadata").Exists() {
					t.Errorf("payload rules were overwritten or replayed: %s", body)
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"id\":\"result\",\"model\":\"glm-5.3\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"id":"result","model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
				}
			}))
			defer server.Close()
			cfg := &config.Config{Payload: config.PayloadConfig{
				Override: []config.PayloadRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: map[string]any{"max_tokens": 17}}},
				Filter:   []config.PayloadFilterRule{{Models: []config.PayloadModelRule{{Name: "*"}}, Params: []string{"metadata", "messages.0"}}},
			}}
			executor := NewZCodeExecutor(cfg)
			auth := &coreauth.Auth{Provider: "zcode", Metadata: map[string]any{"base_url": server.URL, "access_token": "project-token", "business_access_token": "account-token", "auth_kind": "oauth"}}
			req := coreexecutor.Request{Model: "glm-5.3", Payload: []byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"first"},{"role":"user","content":"second"}],"metadata":{"remove":true},"max_tokens":500}`)}
			opts := coreexecutor.Options{SourceFormat: sdktranslator.FromString("openai"), Stream: stream}
			if stream {
				result, errStream := executor.ExecuteStream(context.Background(), auth, req, opts)
				if errStream != nil {
					t.Fatal(errStream)
				}
				count := 0
				for chunk := range result.Chunks {
					if chunk.Err != nil {
						t.Fatal(chunk.Err)
					}
					count++
				}
				if count == 0 {
					t.Fatal("empty stream")
				}
			} else {
				response, errExecute := executor.Execute(context.Background(), auth, req, opts)
				if errExecute != nil {
					t.Fatal(errExecute)
				}
				if len(response.Payload) == 0 {
					t.Fatal("empty response")
				}
			}
			if auth.Attributes != nil || auth.Metadata["access_token"] != "project-token" {
				t.Fatal("execution mutated stored credentials")
			}
		})
	}
}
