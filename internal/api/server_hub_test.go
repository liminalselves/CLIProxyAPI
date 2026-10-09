package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/multiuser"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/usage"
)

type hubMockExecutor struct {
	mu       sync.Mutex
	selected []string
}

func (*hubMockExecutor) Identifier() string { return "codex" }
func (e *hubMockExecutor) Execute(ctx context.Context, a *auth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	e.mu.Lock()
	e.selected = append(e.selected, a.Owner())
	e.mu.Unlock()
	r := helps.NewUsageReporter(ctx, "codex", req.Model, a)
	r.Publish(ctx, usage.Detail{InputTokens: 7, OutputTokens: 3, TotalTokens: 10})
	return coreexecutor.Response{Payload: []byte(`{"id":"test","object":"chat.completion","model":"gpt-5","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`)}, nil
}
func (e *hubMockExecutor) ExecuteStream(ctx context.Context, a *auth.Auth, req coreexecutor.Request, _ coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	e.mu.Lock()
	e.selected = append(e.selected, a.Owner())
	e.mu.Unlock()
	payload := []byte("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\ndata: [DONE]\n\n")
	chunks := make(chan coreexecutor.StreamChunk, 1)
	chunks <- coreexecutor.StreamChunk{Payload: payload}
	close(chunks)
	r := helps.NewUsageReporter(ctx, "codex", req.Model, a)
	r.SetStream(true)
	r.Publish(ctx, usage.Detail{InputTokens: 7, OutputTokens: 3, TotalTokens: 10})
	return &coreexecutor.StreamResult{Chunks: chunks}, nil
}
func (*hubMockExecutor) CountTokens(context.Context, *auth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, nil
}
func (*hubMockExecutor) Refresh(_ context.Context, a *auth.Auth) (*auth.Auth, error) { return a, nil }
func (*hubMockExecutor) PrepareRequest(*http.Request, *auth.Auth) error              { return nil }

func (*hubMockExecutor) HttpRequest(context.Context, *auth.Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

type hubUsageBarrier struct {
	site      string
	delivered chan struct{}
}

func (p *hubUsageBarrier) HandleUsage(_ context.Context, r usage.Record) {
	if r.Attribution.SiteID == p.site {
		p.delivered <- struct{}{}
	}
}

func TestHubAccountsCredentialsRoutingAndTransparentUsage(t *testing.T) {
	dir := t.TempDir()
	authDir := filepath.Join(dir, "凭证")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store := sdkAuth.NewFileTokenStore()
	store.SetBaseDir(authDir)
	previous := sdkAuth.GetTokenStore()
	sdkAuth.RegisterTokenStore(store)
	t.Cleanup(func() { sdkAuth.RegisterTokenStore(previous) })
	cfg := &config.Config{AuthDir: authDir, MultiUser: config.MultiUserConfig{Enabled: true, DataDir: filepath.Join(dir, "账户")}, SDKConfig: config.SDKConfig{APIKeys: []string{"legacy-shared-key"}}, WebsocketAuth: true}
	manager := auth.NewManager(store, &auth.RoundRobinSelector{}, nil)
	manager.SetConfig(cfg)
	exec := &hubMockExecutor{}
	manager.RegisterExecutor(exec)
	syncModels := func(_ context.Context, a *auth.Auth) error {
		models := []*registry.ModelInfo{}
		if !a.Disabled && a.ChannelPublished() {
			for _, base := range []string{"gpt-5", "gpt-4"} {
				if a.ChannelModelEnabled(base) {
					models = append(models, &registry.ModelInfo{ID: a.Prefix + "/" + base, MetadataModelID: base, OwnedBy: "openai", Type: "codex"})
				}
			}
		}
		registry.GetGlobalRegistry().RegisterClient(a.ID, "codex", models)
		return nil
	}
	s := NewServer(cfg, manager, sdkaccess.NewManager(), filepath.Join(dir, "config.yaml"), WithPostAuthPersistHook(syncModels))
	t.Cleanup(func() {
		for _, a := range manager.List() {
			registry.GetGlobalRegistry().UnregisterClient(a.ID)
		}
		_ = s.Stop(context.Background())
	})
	if s.hub == nil {
		t.Fatalf("hub unavailable: %v", s.hubInitErr)
	}
	request := func(method, path, token, body string, expected int) map[string]json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != expected {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, expected, w.Body.String())
		}
		var out map[string]json.RawMessage
		if len(w.Body.Bytes()) > 0 && w.Body.Bytes()[0] == '{' {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	str := func(raw json.RawMessage) string {
		var out string
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	request("POST", "/v8/hub/register", "", `{"username":"alice","display_name":"阿丽","password":"password-alice"}`, 400)
	registered := request("POST", "/v8/hub/register", "", `{"username":"alice","password":"password-alice"}`, 201)
	if strings.Contains(string(registered["user"]), "display_name") {
		t.Fatal("registration returned a separate display name")
	}
	aliceSession := str(request("POST", "/v8/hub/login", "", `{"username":"alice","password":"password-alice"}`, 200)["token"])
	request("PATCH", "/v8/hub/me", aliceSession, `{"username":"renamed"}`, 404)
	inviteBody := request("POST", "/v8/hub/invites", aliceSession, `{"max_uses":1,"days":7}`, 201)
	var invite multiuser.Invite
	if err := json.Unmarshal(inviteBody["invite"], &invite); err != nil {
		t.Fatal(err)
	}
	request("POST", "/v8/hub/register", "", `{"username":"bob","password":"password-bob","invite":"`+invite.Code+`"}`, 201)
	bobSession := str(request("POST", "/v8/hub/login", "", `{"username":"bob","password":"password-bob"}`, 200)["token"])
	request("POST", "/v8/hub/invites", bobSession, `{}`, 403)
	for _, body := range []string{
		`{"username":"renamed","disabled":true}`,
		`{"display_name":"Renamed","disabled":true}`,
	} {
		request("PATCH", "/v8/hub/users/bob", aliceSession, body, 400)
	}
	if bob, ok := s.hub.Store.User("bob"); !ok || bob.Disabled {
		t.Fatal("rejected identity update changed the member")
	}
	request("PATCH", "/v8/hub/users/bob", aliceSession, `{"disabled":true}`, 200)
	request("PATCH", "/v8/hub/users/bob", aliceSession, `{"disabled":false}`, 200)
	bobSession = str(request("POST", "/v8/hub/login", "", `{"username":"bob","password":"password-bob"}`, 200)["token"])
	request("POST", "/v8/hub/channels", aliceSession, `{"slug":"gpt","name":"阿丽的 GPT"}`, 400)
	aliceChBody := request("POST", "/v8/hub/channels", aliceSession, `{"slug":"gpt"}`, 201)
	bobChBody := request("POST", "/v8/hub/channels", bobSession, `{"slug":"bob-sub"}`, 201)
	if strings.Contains(string(aliceChBody["channel"]), `"name"`) {
		t.Fatal("channel creation returned a separate channel name")
	}
	var aliceCh, bobCh multiuser.Channel
	_ = json.Unmarshal(aliceChBody["channel"], &aliceCh)
	_ = json.Unmarshal(bobChBody["channel"], &bobCh)
	for _, body := range []string{
		`{"name":"renamed","published":false}`,
		`{"slug":"renamed","public":true}`,
		`{"prefix":"renamed","announcement":"not saved"}`,
	} {
		request("PATCH", "/v8/hub/channels/"+aliceCh.ID, aliceSession, body, 400)
	}
	unchanged, _ := s.hub.Store.Channel(aliceCh.ID)
	if unchanged.Slug != "gpt" || unchanged.Prefix != "gpt" || !unchanged.Published || unchanged.Public || unchanged.Announcement != "" {
		t.Fatalf("rejected channel update changed persisted state: %#v", unchanged)
	}
	request("PATCH", "/v8/hub/channels/"+aliceCh.ID, aliceSession, `{"announcement":"maintenance"}`, 200)
	request("PATCH", "/v8/hub/channels/"+bobCh.ID, bobSession, `{"public":true}`, 200)
	request("PATCH", "/v8/hub/channels/"+bobCh.ID, aliceSession, `{"announcement":"shared maintenance"}`, 200)
	request("POST", "/v8/hub/credentials?channel_id="+aliceCh.ID+"&name=gpt.json", bobSession, `{"type":"codex","access_token":"test-token"}`, 403)
	aliceUpload := request("POST", "/v8/hub/credentials?channel_id="+aliceCh.ID+"&name=gpt.json", aliceSession, `{"type":"codex","access_token":"test-token","owner":"forged","prefix":"forged","plan_type":"plus"}`, 201)
	bobUpload := request("POST", "/v8/hub/credentials?channel_id="+bobCh.ID+"&name=gpt.json", bobSession, `{"type":"codex","access_token":"test-token"}`, 201)
	aliceID, bobID := str(aliceUpload["credential_id"]), str(bobUpload["credential_id"])
	aliceAuth, _ := manager.GetByID(aliceID)
	if aliceAuth.Owner() != "alice" || aliceAuth.Prefix != "gpt-codex#1" {
		t.Fatalf("upload ownership spoofed: %#v", aliceAuth)
	}
	for _, path := range []string{"/v8/hub/credentials", "/v8/hub/marketplace"} {
		summaries := request("GET", path, bobSession, "", 200)
		field := "credentials"
		if path == "/v8/hub/marketplace" {
			field = "models"
		}
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(summaries[field], &items); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range items {
			if _, hasName := item["channel_name"]; hasName {
				t.Fatal("resource summary returned a separate channel name")
			}
			if str(item["channel_id"]) == aliceCh.ID {
				found = true
				if str(item["channel_slug"]) != "gpt" {
					t.Fatalf("resource summary lost its channel identifier: %s", item["channel_slug"])
				}
			}
		}
		if !found {
			t.Fatalf("%s did not return the mounted credential", path)
		}
	}
	aliceRoute := base64.RawURLEncoding.EncodeToString([]byte(aliceID))
	request("PATCH", "/v8/hub/credentials/"+aliceRoute, bobSession, `{"disabled":true}`, 403)
	request("PATCH", "/v8/hub/credentials/"+aliceRoute+"/fields", bobSession, `{"note":"not allowed"}`, 403)
	request("PATCH", "/v8/hub/credentials/"+aliceRoute+"/fields", aliceSession, `{"owner":"bob"}`, 400)
	request("PATCH", "/v8/hub/credentials/"+aliceRoute+"/fields", aliceSession, `{"prefix":"bob-gpt"}`, 400)
	request("PATCH", "/v8/hub/credentials/"+aliceRoute+"/fields", aliceSession, `{"note":"owner settings","priority":3}`, 200)
	updatedAuth, _ := manager.GetByID(aliceID)
	if updatedAuth.Owner() != "alice" || updatedAuth.Prefix != "gpt-codex#1" || updatedAuth.Metadata["note"] != "owner settings" {
		t.Fatal("credential details altered ownership or discarded settings")
	}
	request("POST", "/v8/hub/credentials/"+aliceRoute+"/refresh", bobSession, "", 403)
	request("DELETE", "/v8/hub/credentials/"+aliceRoute, bobSession, ``, 403)
	request("PATCH", "/v8/hub/credentials/"+base64.RawURLEncoding.EncodeToString([]byte(bobID)), aliceSession, `{"label":"管理员可修改"}`, 200)
	keyBody := request("POST", "/v8/hub/keys", bobSession, `{"name":"手机"}`, 201)
	bobKey := str(keyBody["secret"])
	request("POST", "/v8/hub/keys", bobSession, `{"name":"电脑"}`, 201)
	request("GET", "/v1/models", "legacy-shared-key", "", 401)
	listed := request("GET", "/v1/models", bobKey, "", 200)
	if strings.Contains(string(listed["data"]), `"id":"gpt-5"`) || !strings.Contains(string(listed["data"]), "gpt-codex#1/gpt-5") {
		t.Fatalf("channel model listing: %s", listed["data"])
	}
	if _, err := manager.SelectAuth(context.Background(), "codex", "", coreexecutor.Options{}); err == nil {
		t.Fatal("model-less selection escaped the channel requirement")
	}
	request("POST", "/v1/chat/completions", bobKey, `{"model":"gpt-5","messages":[]}`, 400)
	request("POST", "/v1/live", bobKey, `{}`, 501)
	barrier := &hubUsageBarrier{site: s.hub.Store.SiteID(), delivered: make(chan struct{}, 10)}
	usage.RegisterNamedPlugin(t.Name()+dir, barrier)
	request("POST", "/v1/chat/completions", bobKey, `{"model":"gpt-codex#1/gpt-5","messages":[{"role":"user","content":"hello"}]}`, 200)
	select {
	case <-barrier.delivered:
	case <-time.After(10 * time.Second):
		t.Fatal("usage sink did not finish")
	}
	total, _, err := s.hub.Store.Summary(multiuser.Filter{Consumer: "bob", Owner: "alice"})
	if err != nil || total.TotalTokens != 10 || total.Calls != 1 {
		t.Fatalf("usage attribution: %#v %v", total, err)
	}
	request("GET", "/v8/hub/usage", aliceSession, "", 200)
	request("GET", "/v8/hub/usage", bobSession, "", 200)
	request("GET", "/v8/hub/audit", bobSession, "", 200)
	request("POST", "/v1/chat/completions", bobKey, `{"model":"gpt-codex#1/gpt-5","stream":true,"messages":[{"role":"user","content":"hello"}]}`, 200)
	select {
	case <-barrier.delivered:
	case <-time.After(10 * time.Second):
		t.Fatal("stream usage sink did not finish")
	}
	exec.mu.Lock()
	if len(exec.selected) != 2 || exec.selected[0] != "alice" || exec.selected[1] != "alice" {
		t.Fatalf("channel routed to a different owner: %#v", exec.selected)
	}
	exec.mu.Unlock()
	var createdKey struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(keyBody["key"], &createdKey); err != nil {
		t.Fatal(err)
	}
	request("POST", "/v1/chat/completions", bobKey, `{"model":"gpt-5","messages":[]}`, 400)
	request("PATCH", "/v8/hub/keys/"+createdKey.ID, bobSession, `{"auto_balance":true}`, 200)
	request("POST", "/v1/chat/completions", bobKey, `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`, 200)
	request("POST", "/v1/chat/completions", bobKey, `{"model":"gpt-5(stream)","messages":[{"role":"user","content":"hi"}]}`, 200)
	exec.mu.Lock()
	bareAttempts := len(exec.selected)
	exec.mu.Unlock()
	if bareAttempts != 4 {
		t.Fatalf("bare model calls did not route through the channel credential: %d attempts", bareAttempts)
	}
	request("PATCH", "/v8/hub/keys/"+createdKey.ID, bobSession, `{"auto_balance":false}`, 200)
	request("POST", "/v1/chat/completions", bobKey, `{"model":"gpt-5","messages":[]}`, 400)
	request("PATCH", "/v8/hub/credentials/"+aliceRoute, aliceSession, `{"enabled_models":["gpt-5"]}`, 200)
	listed = request("GET", "/v1/models", bobKey, "", 200)
	if strings.Contains(string(listed["data"]), "gpt-codex#1/gpt-4") {
		t.Fatal("unlisted model still available")
	}
	request("PATCH", "/v8/hub/channels/"+aliceCh.ID, aliceSession, `{"published":false}`, 200)
	request("POST", "/v1/chat/completions", bobKey, `{"model":"gpt-codex#1/gpt-5","messages":[]}`, 400)
	calls, count, err := s.hub.Store.Calls(multiuser.Filter{Consumer: "bob"})
	if err != nil || count < 3 || len(calls) < 3 {
		t.Fatalf("client failure calls not logged: %d %v", count, err)
	}
	creds := request("GET", "/v8/hub/credentials", bobSession, "", 200)
	if !strings.Contains(string(creds["credentials"]), "管理员可修改") {
		t.Fatal("credential label lost after synchronization")
	}
	if strings.Contains(string(creds["credentials"]), "test-token") {
		t.Fatal("public credential list leaked token")
	}
	audits, _, err := s.hub.Store.Audits(multiuser.Filter{Actor: "bob"})
	if err != nil || len(audits) == 0 {
		t.Fatal("missing actor audit")
	}
	reopened, err := multiuser.Open(cfg.MultiUser.DataDir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reloadedUser, userOK := reopened.User("alice")
	reloadedChannel, channelOK := reopened.Channel(aliceCh.ID)
	if !userOK || reloadedUser.Username != "alice" || !channelOK || reloadedChannel.Slug != "gpt" || reloadedChannel.Prefix != "gpt" {
		t.Fatal("restart changed the account or channel identity")
	}
	total, _, err = reopened.Summary(multiuser.Filter{Owner: "alice"})
	if err != nil || total.TotalTokens != 40 {
		t.Fatalf("restart usage: %#v %v", total, err)
	}
	request("GET", "/hub.html", "", "", 302)
	request("GET", "/management.html", "", "", 200)
	request("GET", "/v8/management/credentials", bobSession, "", 403)
	request("GET", "/v8/management/credentials", aliceSession, "", 200)
	request("GET", "/v8/hub/credentials/"+aliceRoute+"/download", bobSession, "", 403)
	request("GET", "/v8/hub/credentials/"+aliceRoute+"/download", aliceSession, "", 200)
}
