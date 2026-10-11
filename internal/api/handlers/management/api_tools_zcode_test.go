package management

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type zcodeQuotaTransport func(*http.Request) (*http.Response, error)

func (f zcodeQuotaTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResolveZCodeQuotaTokenRefreshesAndPersists(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = zcodeQuotaTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "bigmodel.cn" || !strings.HasSuffix(r.URL.Path, "/api_keys/key/access_tokens") || r.Header.Get("Authorization") != "Bearer business-token" {
			t.Error("wrong ZCode project-token acquisition request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"code":0,"data":{"accessToken":"fresh-project-token","tokenType":"Bearer","expiresIn":600,"expiresAt":%d}}`, time.Now().Add(10*time.Minute).Unix())))}, nil
	})
	store := &memoryAuthStore{}
	manager := coreauth.NewManager(store, nil, nil)
	auth, errRegister := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{
		ID: "zcode-quota", Provider: "zcode", Metadata: map[string]any{
			"type": "zcode", "auth_kind": "oauth", "business_access_token": "business-token",
			"access_token": "old-project-token", "expired": time.Now().Add(-time.Minute).Format(time.RFC3339),
			"organization_id": "org", "project_id": "project", "api_key_id": "key",
		},
	})
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	h := &Handler{cfg: &config.Config{}, authManager: manager, tokenStore: store}
	for i := 0; i < 2; i++ {
		token, errToken := h.resolveTokenForAuth(context.Background(), auth.Clone(), "")
		if errToken != nil {
			t.Fatal(errToken)
		}
		if token != "fresh-project-token" {
			t.Fatalf("quota token = %q", token)
		}
	}
	if calls != 1 {
		t.Fatalf("project token minted %d times, want 1", calls)
	}
	if auth.Metadata["access_token"] != "old-project-token" {
		t.Fatal("input credential mutated")
	}
	live, _ := manager.GetByID(auth.ID)
	if live.Metadata["access_token"] != "fresh-project-token" || live.Metadata["business_access_token"] != "business-token" {
		t.Fatal("manager did not preserve separated tokens")
	}
	stored, errList := store.List(context.Background())
	if errList != nil {
		t.Fatal(errList)
	}
	if len(stored) != 1 || stored[0].Metadata["access_token"] != "fresh-project-token" {
		t.Fatal("refreshed token was not persisted")
	}
}

func TestResolveZCodeQuotaTokenConcurrentSingleflight(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	http.DefaultTransport = zcodeQuotaTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		once.Do(func() { close(started) })
		<-release
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"code":0,"data":{"accessToken":"fresh-project-token","tokenType":"Bearer","expiresIn":600,"expiresAt":%d}}`, time.Now().Add(10*time.Minute).Unix())))}, nil
	})
	manager := coreauth.NewManager(&memoryAuthStore{}, nil, nil)
	auth, errRegister := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: "zcode-concurrent", Provider: "zcode", Metadata: map[string]any{
		"business_access_token": "business-token", "access_token": "old-token", "expired": time.Now().Add(-time.Minute).Format(time.RFC3339),
		"organization_id": "org", "project_id": "project", "api_key_id": "key",
	}})
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	h := &Handler{cfg: &config.Config{}, authManager: manager}
	results := make(chan error, 8)
	for i := 0; i < cap(results); i++ {
		go func() {
			token, errToken := h.resolveTokenForAuth(context.Background(), auth.Clone(), "")
			if errToken == nil && token != "fresh-project-token" {
				errToken = fmt.Errorf("unexpected quota token")
			}
			results <- errToken
		}()
	}
	<-started
	close(release)
	for i := 0; i < cap(results); i++ {
		if errToken := <-results; errToken != nil {
			t.Fatal(errToken)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent queries minted %d tokens, want 1", calls.Load())
	}
}

func TestResolveZCodeQuotaTokenFailureSemantics(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, saveFailure := range []bool{false, true} {
		t.Run(fmt.Sprintf("save_failure=%t", saveFailure), func(t *testing.T) {
			http.DefaultTransport = zcodeQuotaTransport(func(r *http.Request) (*http.Response, error) {
				status, body := http.StatusUnauthorized, `{"message":"fixture-secret"}`
				if saveFailure {
					status = http.StatusOK
					body = fmt.Sprintf(`{"code":0,"data":{"accessToken":"new-token","tokenType":"Bearer","expiresIn":600,"expiresAt":%d}}`, time.Now().Add(10*time.Minute).Unix())
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			var store coreauth.Store = &memoryAuthStore{}
			saveErr := errors.New("fixture store unavailable")
			if saveFailure {
				store = &failingMetaTokenStore{err: saveErr}
			}
			manager := coreauth.NewManager(store, nil, nil)
			auth, errRegister := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: "zcode-failure", Provider: "zcode", Metadata: map[string]any{
				"business_access_token": "business-token", "access_token": "expired-token", "expired": time.Now().Add(-time.Minute).Format(time.RFC3339),
				"organization_id": "org", "project_id": "project", "api_key_id": "key",
			}})
			if errRegister != nil {
				t.Fatal(errRegister)
			}
			h := &Handler{cfg: &config.Config{}, authManager: manager}
			token, errToken := h.resolveTokenForAuth(context.Background(), auth, "")
			live, _ := manager.GetByID(auth.ID)
			if saveFailure {
				// Ordinary refreshes use the manager's non-fatal persistence policy.
				if errToken != nil || token != "new-token" || live.Metadata["access_token"] != "new-token" {
					t.Fatal("store failure discarded a valid runtime token")
				}
				stored, errList := store.List(context.Background())
				if errList != nil || len(stored) != 0 {
					t.Fatal("failed store unexpectedly retained the token")
				}
				return
			}
			if errToken == nil || token != "" {
				t.Fatal("failed refresh returned a token")
			}
			if strings.Contains(errToken.Error(), "fixture-secret") {
				t.Fatal("upstream body leaked")
			}
			if live.Metadata["access_token"] != "expired-token" {
				t.Fatal("failed refresh modified live token")
			}
		})
	}
}

func TestResolveZCodeQuotaFreshTokenSkipsAcquisition(t *testing.T) {
	auth := &coreauth.Auth{Provider: "zcode", Metadata: map[string]any{"access_token": "fresh-token", "expired": time.Now().Add(time.Hour).Format(time.RFC3339)}}
	token, errToken := (*Handler)(nil).resolveTokenForAuth(context.Background(), auth, "")
	if errToken != nil || token != "fresh-token" {
		t.Fatalf("fresh token lookup failed: %v", errToken)
	}
}

func TestResolveZCodeQuotaTokenNeverFallsBackToBusinessToken(t *testing.T) {
	auth := &coreauth.Auth{Provider: "zcode", Metadata: map[string]any{"business_access_token": "business-token"}}
	token, errToken := (*Handler)(nil).resolveTokenForAuth(context.Background(), auth, "")
	if errToken == nil || token != "" {
		t.Fatal("missing project token did not require refresh")
	}
}

func TestZCodeResetAuthorizationUsesSeparateTokens(t *testing.T) {
	auth := &coreauth.Auth{Provider: "zcode", Metadata: map[string]any{
		"access_token": "project-token", "business_access_token": "business-token", "zcode_jwt": "zcode-jwt",
	}}
	req, errRequest := http.NewRequest(http.MethodPost, "https://zcode.z.ai/api/v1/coding-plan/reset/use", nil)
	if errRequest != nil {
		t.Fatal(errRequest)
	}
	if errAuth := applyZCodeResetAuthorization(auth, req); errAuth != nil {
		t.Fatal(errAuth)
	}
	if req.Header.Get("Authorization") != "Bearer zcode-jwt" || req.Header.Get("X-Bigmodel-Authorization") != "business-token" || req.Header.Get("Bigmodel-Target-Type") != "PERSONAL" {
		t.Fatal("reset authorization did not preserve token roles")
	}
	req.URL, _ = url.Parse("https://bigmodel.cn/api/monitor/usage/quota/limit")
	req.Header = make(http.Header)
	if errAuth := applyZCodeResetAuthorization(auth, req); errAuth != nil || len(req.Header) != 0 {
		t.Fatal("reset tokens attached to quota request")
	}
	req.URL, _ = url.Parse("https://zcode.z.ai/api/v1/coding-plan/reset/status")
	delete(auth.Metadata, "zcode_jwt")
	if errAuth := applyZCodeResetAuthorization(auth, req); errAuth == nil || !strings.Contains(errAuth.Error(), "sign in again") {
		t.Fatal("missing JWT did not require reauthorization")
	}
}
