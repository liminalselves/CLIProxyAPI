package management

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codex"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/multiuser"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestHubOAuthSaveOwnsAndPreservesChannelOnRelogin(t *testing.T) {
	dir := t.TempDir()
	store, err := multiuser.Open(filepath.Join(dir, "accounts"), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	alice, err := store.Register("alice", "password-alice", "")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := store.CreateChannel(alice, "gpt", false)
	if err != nil {
		t.Fatal(err)
	}
	files := sdkAuth.NewFileTokenStore()
	files.SetBaseDir(filepath.Join(dir, "auths"))
	manager := coreauth.NewManager(files, &coreauth.RoundRobinSelector{}, nil)
	h := NewHandler(&config.Config{AuthDir: filepath.Join(dir, "auths")}, "", manager)
	h.tokenStore = files
	h.SetHubStore(store)
	var received *coreauth.Auth
	h.SetPostAuthPersistHook(func(_ context.Context, a *coreauth.Auth) error { received = a.Clone(); return nil })
	ctx := multiuser.WithCredentialContext(context.Background(), multiuser.CredentialContext{Actor: alice, Channel: ch})
	record := &coreauth.Auth{ID: "Codex-Test.json", FileName: "Codex-Test.json", Provider: "codex", Storage: &codex.CodexTokenStorage{AccessToken: "first-token", RefreshToken: "refresh-token", Email: "test@example.com", Type: "codex"}, Metadata: map[string]any{"email": "test@example.com"}}
	path, err := h.saveTokenRecord(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if received == nil || received.Owner() != "alice" || received.Prefix != "gpt-codex#1" || received.ChannelID() != ch.ID {
		t.Fatalf("OAuth callback lost ownership: %#v", received)
	}
	if record.ID != h.authIDForPath(path) {
		t.Fatalf("noncanonical OAuth ID %s for %s", record.ID, path)
	}
	if _, err := manager.Register(coreauth.WithSkipPersist(context.Background()), received); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["owner"] != "alice" || metadata["prefix"] != "gpt-codex#1" || metadata["access_token"] != "first-token" {
		t.Fatalf("serialized metadata wrong: %#v", metadata)
	}
	metadata["enabled_models"] = []string{"gpt-5"}
	metadata["note"] = "keep this"
	metadata["disabled"] = true
	raw, err = json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	updated := &coreauth.Auth{ID: "Codex-Test.json", FileName: "Codex-Test.json", Provider: "codex", Storage: &codex.CodexTokenStorage{AccessToken: "new-token", RefreshToken: "new-refresh", Email: "test@example.com", Type: "codex"}, Metadata: map[string]any{"email": "test@example.com"}}
	secondPath, err := h.saveTokenRecord(ctx, updated)
	if err != nil {
		t.Fatal(err)
	}
	if secondPath != path || !received.Disabled || received.Owner() != "alice" || received.Metadata["note"] != "keep this" || !received.ChannelModelEnabled("gpt-5") || received.ChannelModelEnabled("gpt-4") {
		t.Fatalf("relogin lost channel settings: %#v", received)
	}
	if token, _ := received.Metadata["access_token"].(string); token != "new-token" {
		t.Fatal("relogin restored old OAuth token")
	}
	audits, count, err := store.Audits(multiuser.Filter{Actor: "alice", Action: "credential.saved"})
	if err != nil || count != 2 || len(audits) != 2 {
		t.Fatal("OAuth credential saves were not audited")
	}
}
