package executor

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/zcode"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

var (
	_ coreauth.ProviderExecutor    = (*ZCodeExecutor)(nil)
	_ coreauth.RequestAuthPreparer = (*ZCodeExecutor)(nil)
)

type ZCodeExecutor struct {
	*OpenAICompatExecutor
	cfg *config.Config
}

func NewZCodeExecutor(cfg *config.Config) *ZCodeExecutor {
	return &ZCodeExecutor{OpenAICompatExecutor: NewOpenAICompatExecutor("zcode", cfg), cfg: cfg}
}

func (e *ZCodeExecutor) enrichAuth(auth *coreauth.Auth) (*coreauth.Auth, error) {
	if auth == nil {
		return nil, errors.New("missing ZCode credentials")
	}
	cloned := auth.Clone()
	if cloned.Attributes == nil {
		cloned.Attributes = make(map[string]string)
	}
	baseURL, _ := cloned.Metadata["base_url"].(string)
	if strings.TrimSpace(baseURL) == "" {
		baseURL = zcode.DefaultAPIBaseURL
	}
	token, _ := cloned.Metadata["access_token"].(string)
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("missing Coding Plan project token; sign in with ZCode again")
	}
	cloned.Attributes["base_url"] = strings.TrimSpace(baseURL)
	cloned.Attributes["api_key"] = strings.TrimSpace(token)
	return cloned, nil
}

func (e *ZCodeExecutor) Execute(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	enriched, errAuth := e.enrichAuth(auth)
	if errAuth != nil {
		return coreexecutor.Response{}, errAuth
	}
	return e.OpenAICompatExecutor.Execute(ctx, enriched, req, opts)
}

func (e *ZCodeExecutor) ExecuteStream(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	enriched, errAuth := e.enrichAuth(auth)
	if errAuth != nil {
		return nil, errAuth
	}
	return e.OpenAICompatExecutor.ExecuteStream(ctx, enriched, req, opts)
}

func (e *ZCodeExecutor) CountTokens(ctx context.Context, auth *coreauth.Auth, req coreexecutor.Request, opts coreexecutor.Options) (coreexecutor.Response, error) {
	enriched, errAuth := e.enrichAuth(auth)
	if errAuth != nil {
		return coreexecutor.Response{}, errAuth
	}
	return e.OpenAICompatExecutor.CountTokens(ctx, enriched, req, opts)
}

func (e *ZCodeExecutor) PrepareRequest(req *http.Request, auth *coreauth.Auth) error {
	enriched, errAuth := e.enrichAuth(auth)
	if errAuth != nil {
		return errAuth
	}
	return e.OpenAICompatExecutor.PrepareRequest(req, enriched)
}

func (e *ZCodeExecutor) HttpRequest(ctx context.Context, auth *coreauth.Auth, req *http.Request) (*http.Response, error) {
	if e.ShouldPrepareRequestAuth(auth) {
		refreshed, errRefresh := e.Refresh(ctx, auth)
		if errRefresh != nil {
			return nil, errRefresh
		}
		auth = refreshed
	}
	enriched, errAuth := e.enrichAuth(auth)
	if errAuth != nil {
		return nil, errAuth
	}
	return e.OpenAICompatExecutor.HttpRequest(ctx, enriched, req)
}

func (e *ZCodeExecutor) ShouldPrepareRequestAuth(auth *coreauth.Auth) bool {
	if auth == nil {
		return false
	}
	token, _ := auth.Metadata["access_token"].(string)
	expiration, hasExpiration := auth.ExpirationTime()
	return strings.TrimSpace(token) == "" || hasExpiration && !expiration.After(time.Now().Add(2*time.Minute))
}

func (e *ZCodeExecutor) PrepareRequestAuth(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	if !e.ShouldPrepareRequestAuth(auth) {
		return auth, nil
	}
	return e.Refresh(ctx, auth)
}

func (e *ZCodeExecutor) Refresh(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	if refreshed, handled, errRefresh := helps.RefreshAuthViaHome(ctx, e.cfg, auth); handled {
		return refreshed, errRefresh
	}
	if auth == nil {
		return nil, errors.New("missing ZCode credentials")
	}
	loginToken, _ := auth.Metadata["business_access_token"].(string)
	organizationID, _ := auth.Metadata["organization_id"].(string)
	projectID, _ := auth.Metadata["project_id"].(string)
	keyID, _ := auth.Metadata["api_key_id"].(string)
	client := zcode.NewClient(e.cfg, auth.ProxyURL)
	token, errToken := client.IssueProjectToken(ctx, loginToken, zcode.ProjectLocation{OrganizationID: organizationID, ProjectID: projectID, APIKeyID: keyID})
	if errToken != nil {
		return nil, errToken
	}
	// Refresh candidates must not change the manager's live credential before acceptance.
	cloned := auth.Clone()
	zcode.ApplyProjectToken(cloned, token, time.Now())
	return cloned, nil
}
