package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/zcode"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type fakeZCodeOAuth struct {
	flow   *zcode.LoginFlow
	wait   func(context.Context) (*zcode.LoginResult, error)
	create func(context.Context) (*coreauth.Auth, error)
}

func (f *fakeZCodeOAuth) StartLogin(context.Context) (*zcode.LoginFlow, error) { return f.flow, nil }
func (f *fakeZCodeOAuth) WaitForLogin(ctx context.Context, _ *zcode.LoginFlow) (*zcode.LoginResult, error) {
	return f.wait(ctx)
}
func (f *fakeZCodeOAuth) CreateAuthRecord(ctx context.Context, _ *zcode.LoginResult) (*coreauth.Auth, error) {
	return f.create(ctx)
}

func TestZCodeOAuthV8PollingOutlivesRequest(t *testing.T) {
	dir := t.TempDir()
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: dir}, nil)
	release := make(chan struct{})
	entered := make(chan struct{})
	done := make(chan struct{})
	service := &fakeZCodeOAuth{
		flow: &zcode.LoginFlow{State: "zcode-request-test", AuthorizeURL: "https://bigmodel.cn/login?state=zcode-request-test", ExpiresAt: time.Now().Add(time.Minute).Unix()},
		wait: func(ctx context.Context) (*zcode.LoginResult, error) {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if ctx.Err() != nil {
				t.Error("background login inherited HTTP request cancellation")
			}
			return &zcode.LoginResult{}, nil
		},
	}
	service.create = func(context.Context) (*coreauth.Auth, error) {
		return &coreauth.Auth{ID: "zcode-test.json", FileName: "zcode-test.json", Provider: "zcode", Metadata: map[string]any{"type": "zcode", "auth_kind": "oauth", "access_token": "project-token", "business_access_token": "account-token"}}, nil
	}
	original := newZCodeOAuthService
	newZCodeOAuthService = func(*config.Config) zcodeOAuthService { return service }
	t.Cleanup(func() { newZCodeOAuthService = original; CancelOAuthSession(service.flow.State) })
	router := gin.New()
	router.GET("/oauth/auth-url", h.StartOAuthV8)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/oauth/auth-url?provider=zcode", nil).WithContext(requestCtx))
	cancelRequest()
	if w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	var response struct{ State, Flow string }
	if errDecode := json.Unmarshal(w.Body.Bytes(), &response); errDecode != nil {
		t.Fatal(errDecode)
	}
	if response.State != service.flow.State || response.Flow != "polling" {
		t.Fatalf("wrong response: %s", w.Body.String())
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("polling did not begin")
	}
	// Observe persistence through the existing hook, rather than timing a file write.
	h.SetPostAuthPersistHook(func(context.Context, *coreauth.Auth) error { close(done); return nil })
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("credential was not saved")
	}
	data, errRead := os.ReadFile(filepath.Join(dir, "zcode-test.json"))
	if errRead != nil {
		t.Fatal(errRead)
	}
	var stored map[string]any
	if errDecode := json.Unmarshal(data, &stored); errDecode != nil {
		t.Fatal(errDecode)
	}
	if stored["access_token"] != "project-token" || stored["business_access_token"] != "account-token" {
		t.Fatal("credential tokens were mixed")
	}
}

func TestZCodeCancelledLoginDoesNotSave(t *testing.T) {
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: t.TempDir()}, nil)
	flow := &zcode.LoginFlow{State: "zcode-cancel-test"}
	RegisterOAuthSession(flow.State, "zcode")
	defer CancelOAuthSession(flow.State)
	service := &fakeZCodeOAuth{
		wait: func(context.Context) (*zcode.LoginResult, error) {
			CancelOAuthSession(flow.State)
			return &zcode.LoginResult{}, nil
		},
		create: func(context.Context) (*coreauth.Auth, error) {
			t.Error("cancelled login attempted to create credentials")
			return nil, nil
		},
	}
	h.completeZCodeOAuth(context.Background(), flow, service)
	if IsOAuthSessionPending(flow.State, "zcode") {
		t.Fatal("cancelled session remained pending")
	}
	if provider, errProvider := NormalizeOAuthProvider("zcode"); errProvider != nil || provider != "zcode" {
		t.Fatalf("normalization: %s %v", provider, errProvider)
	}
}
