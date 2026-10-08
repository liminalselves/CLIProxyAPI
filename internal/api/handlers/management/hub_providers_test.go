package management

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/multiuser"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func setupHubProviderTest(t *testing.T) (*Handler, *multiuser.Store, *coreauth.Manager, multiuser.User, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	store, err := multiuser.Open(filepath.Join(dir, "账户"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := store.Register("alice", "password-alice", "")
	if err != nil {
		t.Fatal(err)
	}
	files := sdkAuth.NewFileTokenStore()
	files.SetBaseDir(filepath.Join(dir, "auths"))
	manager := coreauth.NewManager(files, &coreauth.RoundRobinSelector{}, nil)
	h := NewHandler(&config.Config{AuthDir: filepath.Join(dir, "auths")}, "", manager)
	h.tokenStore = files
	h.SetHubStore(store)
	return h, store, manager, admin, filepath.Join(dir, "账户")
}

func providerRequest(t *testing.T, h *Handler, actor multiuser.User, method, target, body string) (*httptest.ResponseRecorder, map[string]json.RawMessage) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("hubUser", actor)
	if method == "PATCH" {
		route := strings.TrimPrefix(target, "/v8/hub/providers/")
		c.Params = gin.Params{{Key: "id", Value: route}}
		h.HubPatchProvider(c)
	} else {
		h.HubCreateProvider(c)
	}
	var out map[string]json.RawMessage
	if data := w.Body.Bytes(); len(data) > 0 && data[0] == '{' {
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
	}
	return w, out
}

func providerRouteID(providerID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(providerID))
}

func TestHubCreateProviderPinsNamedPrefix(t *testing.T) {
	h, store, manager, admin, _ := setupHubProviderTest(t)
	ch, err := store.CreateChannel(admin, "573", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateChannel(admin, "573-clash", false); err != nil {
		t.Fatal(err)
	}

	w, out := providerRequest(t, h, admin, "POST", "/v8/hub/providers?channel_id="+ch.ID, `{"name":"daye","base_url":"https://api.example.com/v1/","api_key":"sk-secret","models":[{"name":"kimi-k3"},{"name":"gpt-5","alias":"gpt5"}]}`)
	if w.Code != 201 {
		t.Fatalf("create provider: %d %s", w.Code, w.Body.String())
	}
	var pinned string
	if err := json.Unmarshal(out["prefix"], &pinned); err != nil || pinned != "573-daye" {
		t.Fatalf("pinned prefix %q %v", pinned, err)
	}
	var providerID string
	if err := json.Unmarshal(out["provider_id"], &providerID); err != nil {
		t.Fatal(err)
	}
	a, ok := manager.GetByID(providerID)
	if !ok {
		t.Fatal("provider not registered in auth manager")
	}
	if a.Prefix != "573-daye" || a.Owner() != "alice" || a.ChannelID() != ch.ID {
		t.Fatalf("provider stamping wrong: %#v", a)
	}
	if a.Attributes["base_url"] != "https://api.example.com/v1" || a.Attributes["api_key"] != "sk-secret" || a.Attributes["compat_name"] == "" {
		t.Fatalf("provider attributes missing: %#v", a.Attributes)
	}
	if a.Metadata["created_by"] != "alice" || a.Metadata["provider_name"] != "daye" || a.Metadata["channel_prefix"] != "573" {
		t.Fatalf("provider metadata wrong: %#v", a.Metadata)
	}
	if _, err := os.Stat(filepath.Join(h.cfg.AuthDir, "573-daye.json")); err != nil {
		t.Fatalf("provider file missing: %v", err)
	}

	// Duplicate provider name conflicts on the pinned prefix.
	w, _ = providerRequest(t, h, admin, "POST", "/v8/hub/providers?channel_id="+ch.ID, `{"name":"daye","base_url":"https://other.example.com/v1","api_key":"sk-y","models":[{"name":"m"}]}`)
	if w.Code != 409 {
		t.Fatalf("duplicate provider name: %d", w.Code)
	}
	// A provider name that would collide with an existing channel slug is refused.
	w, _ = providerRequest(t, h, admin, "POST", "/v8/hub/providers?channel_id="+ch.ID, `{"name":"clash","base_url":"https://x.example.com","api_key":"k","models":[{"name":"m"}]}`)
	if w.Code != 409 {
		t.Fatalf("channel-slug collision accepted: %d", w.Code)
	}
	// A provider pinned prefix must not collide with another channel's provider:
	// channel "573" + provider "pool-x" and channel "573-pool" + provider "x"
	// would both produce "573-pool-x".
	other, err := store.CreateChannel(admin, "573-pool", false)
	if err != nil {
		t.Fatal(err)
	}
	w, _ = providerRequest(t, h, admin, "POST", "/v8/hub/providers?channel_id="+other.ID, `{"name":"x","base_url":"https://x.example.com","api_key":"k","models":[{"name":"m"}]}`)
	if w.Code != 201 {
		t.Fatalf("unrelated provider: %d %s", w.Code, w.Body.String())
	}
	w, _ = providerRequest(t, h, admin, "POST", "/v8/hub/providers?channel_id="+ch.ID, `{"name":"pool-x","base_url":"https://y.example.com","api_key":"k","models":[{"name":"m"}]}`)
	if w.Code != 409 {
		t.Fatalf("cross-channel pinned collision accepted: %d", w.Code)
	}
	// Validation failures.
	for _, body := range []string{
		`{"name":"Da#ye","base_url":"https://x.example.com","api_key":"k","models":[{"name":"m"}]}`,
		`{"name":"ok","base_url":"not-a-url","api_key":"k","models":[{"name":"m"}]}`,
		`{"name":"ok","base_url":"https://x.example.com","models":[{"name":"m"}]}`,
		`{"name":"ok","base_url":"https://x.example.com","api_key":"k"}`,
		`{"name":"ok","base_url":"https://x.example.com","api_key":"k","models":[{"name":"a/b"}]}`,
	} {
		if w, _ := providerRequest(t, h, admin, "POST", "/v8/hub/providers?channel_id="+ch.ID, body); w.Code != 400 {
			t.Fatalf("invalid body accepted: %s -> %d", body, w.Code)
		}
	}
}

func TestHubPatchProviderKeepsKeyAndPrefix(t *testing.T) {
	h, store, manager, admin, _ := setupHubProviderTest(t)
	ch, err := store.CreateChannel(admin, "573", false)
	if err != nil {
		t.Fatal(err)
	}
	_, out := providerRequest(t, h, admin, "POST", "/v8/hub/providers?channel_id="+ch.ID, `{"name":"daye","base_url":"https://api.example.com/v1","api_key":"sk-secret","models":[{"name":"kimi-k3"}]}`)
	var providerID string
	if err := json.Unmarshal(out["provider_id"], &providerID); err != nil {
		t.Fatal(err)
	}

	w, _ := providerRequest(t, h, admin, "PATCH", "/v8/hub/providers/"+providerRouteID(providerID), `{"base_url":"https://api2.example.com/v1","models":[{"name":"kimi-k3"},{"name":"kimi-k4"}],"disabled":true}`)
	if w.Code != 200 {
		t.Fatalf("patch provider: %d %s", w.Code, w.Body.String())
	}
	updated, _ := manager.GetByID(providerID)
	if updated.Prefix != "573-daye" {
		t.Fatalf("prefix changed: %s", updated.Prefix)
	}
	if updated.Metadata["api_key"] != "sk-secret" {
		t.Fatal("empty api_key wiped the stored key")
	}
	if updated.Attributes["base_url"] != "https://api2.example.com/v1" || !updated.Disabled {
		t.Fatalf("patch not applied: %#v", updated)
	}
	models, _ := json.Marshal(updated.Metadata["models"])
	if !strings.Contains(string(models), "kimi-k4") {
		t.Fatalf("models not updated: %s", models)
	}

	// A plain OAuth credential must not be patchable as a provider.
	cred := &coreauth.Auth{ID: "plain.json", FileName: "plain.json", Provider: "codex", Metadata: map[string]any{"type": "codex"}}
	if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), cred); err != nil {
		t.Fatal(err)
	}
	w, _ = providerRequest(t, h, admin, "PATCH", "/v8/hub/providers/"+providerRouteID(cred.ID), `{"label":"x"}`)
	if w.Code != 400 {
		t.Fatalf("plain credential patched as provider: %d", w.Code)
	}
}

func TestHubMigrateNamingPinsLegacyData(t *testing.T) {
	h, store, manager, _, storeDir := setupHubProviderTest(t)
	_ = store
	// Simulate pre-pinning data: a channel whose prefix embeds the username and
	// credential files stamped with the old pool prefix.
	legacyDB := `{"users":{"alice":{"username":"alice","display_name":"阿丽","role":"admin","disabled":false,"created_at":"2026-01-01T00:00:00Z","password_hash":"x"}},"keys":{},"invites":{},"sessions":{},"channels":{"ch-legacy":{"id":"ch-legacy","owner":"alice","name":"旧渠道","slug":"573","prefix":"alice-573","published":true,"created_at":"2026-01-01T00:00:00Z"}},"messages":{}}`
	if err := os.WriteFile(filepath.Join(storeDir, "accounts.db"), []byte(legacyDB), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := multiuser.Open(storeDir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h.SetHubStore(reopened)

	writeLegacyAuth := func(name, provider, prefix string) {
		t.Helper()
		metadata := map[string]any{"type": provider, "owner": "alice", "channel_id": "ch-legacy", "channel_name": "旧渠道", "prefix": prefix, "published": true}
		raw, _ := json.Marshal(metadata)
		if err := os.MkdirAll(h.cfg.AuthDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(h.cfg.AuthDir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		a := &coreauth.Auth{ID: name, FileName: name, Provider: provider, Prefix: prefix, Metadata: metadata, Attributes: map[string]string{"path": filepath.Join(h.cfg.AuthDir, name)}}
		if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), a); err != nil {
			t.Fatalf("register legacy auth: %v", err)
		}
	}
	writeLegacyAuth("alice-573-kimi-b.json", "kimi", "alice-573")
	writeLegacyAuth("alice-573-kimi-a.json", "kimi", "alice-573")
	writeLegacyAuth("alice-573-codex.json", "codex", "alice-573")

	h.MigrateHubNaming()

	ch, ok := reopened.Channel("ch-legacy")
	if !ok || ch.Prefix != "573" {
		t.Fatalf("channel prefix not migrated: %#v", ch)
	}
	expected := map[string]string{
		"alice-573-kimi-a.json": "573-kimi#1",
		"alice-573-kimi-b.json": "573-kimi#2",
		"alice-573-codex.json":  "573-codex#1",
	}
	for id, pinned := range expected {
		a, ok := manager.GetByID(id)
		if !ok {
			t.Fatalf("auth %s missing", id)
		}
		if a.Prefix != pinned {
			t.Fatalf("auth %s prefix %s, want %s", id, a.Prefix, pinned)
		}
		if a.Metadata["channel_prefix"] != "573" {
			t.Fatalf("auth %s channel_prefix %#v", id, a.Metadata["channel_prefix"])
		}
		if _, hasIndex := hubMetadataInt(a.Metadata, "channel_index"); !hasIndex {
			t.Fatalf("auth %s missing channel_index", id)
		}
	}
	// Counter floor: the next kimi credential in this channel gets #3.
	next, err := reopened.AllocateCredentialIndex(ch.ID, "kimi")
	if err != nil || next != 3 {
		t.Fatalf("index floor not seeded: %d %v", next, err)
	}
	// Migration is idempotent.
	h.MigrateHubNaming()
	for id, pinned := range expected {
		a, _ := manager.GetByID(id)
		if a.Prefix != pinned {
			t.Fatalf("second migration changed %s to %s", id, a.Prefix)
		}
	}
}
