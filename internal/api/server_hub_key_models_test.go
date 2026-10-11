package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	claudemodels "github.com/router-for-me/CLIProxyAPI/v8/internal/client/claude/models"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/multiuser"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestHubKeyModelFiltersAndBareModelLists(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{AuthDir: filepath.Join(dir, "auths"), MultiUser: config.MultiUserConfig{Enabled: true, DataDir: filepath.Join(dir, "accounts")}}
	manager := auth.NewManager(nil, &auth.RoundRobinSelector{}, nil)
	manager.SetConfig(cfg)
	exec := &hubMockExecutor{}
	manager.RegisterExecutor(exec)
	s := NewServer(cfg, manager, sdkaccess.NewManager(), filepath.Join(dir, "config.yaml"))
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	user, err := s.hub.Store.Register("alice", "password-alice", "")
	if err != nil {
		t.Fatal(err)
	}
	_, session, err := s.hub.Store.Login(user.Username, "password-alice")
	if err != nil {
		t.Fatal(err)
	}
	key, secret, err := s.hub.Store.CreateKey(user, "filtered")
	if err != nil {
		t.Fatal(err)
	}
	_, otherSecret, err := s.hub.Store.CreateKey(user, "unfiltered")
	if err != nil {
		t.Fatal(err)
	}
	reg := registry.GetGlobalRegistry()
	for _, prefix := range []string{"123", "564"} {
		credential := &auth.Auth{ID: t.Name() + prefix, Provider: "codex", Prefix: prefix, Status: auth.StatusActive, Metadata: map[string]any{"owner": prefix, "channel_id": prefix, "published": true}}
		if _, err = manager.Register(context.Background(), credential); err != nil {
			t.Fatal(err)
		}
		reg.RegisterClient(credential.ID, "codex", []*registry.ModelInfo{{ID: prefix + "/deepseek", OwnedBy: "openai", Type: "codex"}})
		t.Cleanup(func() { reg.UnregisterClient(credential.ID) })
	}
	request := func(method, path, token, body string, status int) map[string]json.RawMessage {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		if strings.Contains(path, "anthropic=true") {
			r.Header.Set("Anthropic-Version", "2023-06-01")
		}
		if strings.Contains(path, "grok=true") {
			r.Header.Set("User-Agent", "grok-shell")
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		var result map[string]json.RawMessage
		if strings.HasPrefix(w.Body.String(), "{") {
			if errDecode := json.Unmarshal(w.Body.Bytes(), &result); errDecode != nil {
				t.Fatal(errDecode)
			}
		}
		return result
	}
	assertModels := func(token string, want ...string) {
		t.Helper()
		for _, endpoint := range []struct{ path, list, id string }{
			{"/v1/models", "data", "id"},
			{"/v1/models?client_version=0.153.4", "models", "slug"},
			{"/v1/models?anthropic=true", "data", "id"},
			{"/v1/models?grok=true", "data", "id"},
			{"/v1beta/models", "models", "name"},
		} {
			result := request("GET", endpoint.path, token, "", 200)
			var models []map[string]any
			if errDecode := json.Unmarshal(result[endpoint.list], &models); errDecode != nil {
				t.Fatal(errDecode)
			}
			ids := make([]string, 0, len(models))
			for _, model := range models {
				id, _ := model[endpoint.id].(string)
				id = strings.TrimPrefix(id, "models/")
				ids = append(ids, claudemodels.ResolveClaudeModelIDPrefix(id))
			}
			slices.Sort(ids)
			expected := append([]string{}, want...)
			slices.Sort(expected)
			if !slices.Equal(ids, expected) {
				t.Fatalf("%s models = %v, want %v", endpoint.path, ids, expected)
			}
		}
	}
	patch := func(body string) { request("PATCH", "/v8/hub/keys/"+key.ID, session, body, 200) }
	assertModels(secret, "123/deepseek", "564/deepseek")
	patch(`{"auto_balance":true}`)
	assertModels(secret, "123/deepseek", "564/deepseek", "deepseek")
	patch(`{"blocked_models":[" 123/deepseek ","123/deepseek"]}`)
	assertModels(secret, "564/deepseek", "deepseek")
	for _, stream := range []string{"false", "true"} {
		request("POST", "/v1/chat/completions", secret, `{"model":"deepseek(high)","stream":`+stream+`,"messages":[]}`, 200)
	}
	exec.mu.Lock()
	selected := append([]string{}, exec.selected...)
	exec.mu.Unlock()
	if !slices.Equal(selected, []string{"564", "564"}) {
		t.Fatalf("filtered bare calls used blocked source: %v", selected)
	}
	request("POST", "/v1/chat/completions", secret, `{"model":"123/deepseek(high)","messages":[]}`, 403)
	patch(`{"blocked_models":["123/deepseek","564/deepseek"]}`)
	assertModels(secret)
	blocked := request("POST", "/v1/chat/completions", secret, `{"model":"deepseek","messages":[]}`, 403)
	if !strings.Contains(string(blocked["error"]), "model_blocked") {
		t.Fatalf("missing model_blocked error: %s", blocked["error"])
	}
	assertModels(otherSecret, "123/deepseek", "564/deepseek")
	patch(`{"auto_balance":false}`)
	assertModels(secret)
	patch(`{"blocked_models":["deepseek"]}`)
	assertModels(secret)
	request("POST", "/v1/chat/completions", secret, `{"model":"564/deepseek","messages":[]}`, 403)
	reopened, err := multiuser.Open(cfg.MultiUser.DataDir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, saved, ok := reopened.AuthenticateKey(secret); !ok || !slices.Equal(saved.BlockedModels, []string{"deepseek"}) {
		t.Fatalf("key model filters did not persist: %#v", saved)
	}
	patch(`{"blocked_models":[]}`)
	assertModels(secret, "123/deepseek", "564/deepseek")
	request("PATCH", "/v8/hub/keys/"+key.ID, session, `{"blocked_models":[" "]}`, 400)
	assertModels(secret, "123/deepseek", "564/deepseek")
}
