package management

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// Tool proxy overrides apply to token acquisition, never to the saved credential.
type zcodeManagementPreparer struct {
	executor *executor.ZCodeExecutor
	proxyURL string
}

func (p zcodeManagementPreparer) ShouldPrepareRequestAuth(auth *coreauth.Auth) bool {
	return p.executor.ShouldPrepareRequestAuth(auth)
}

func (p zcodeManagementPreparer) PrepareRequestAuth(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	proxyURL := auth.ProxyURL
	if strings.TrimSpace(p.proxyURL) != "" {
		auth.ProxyURL = p.proxyURL
	}
	updated, errPrepare := p.executor.PrepareRequestAuth(ctx, auth)
	if updated != nil {
		updated.ProxyURL = proxyURL
	}
	return updated, errPrepare
}

func (h *Handler) resolveZCodeToken(ctx context.Context, auth *coreauth.Auth, requestProxyURL string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if auth == nil {
		return "", nil
	}
	var cfg *config.Config
	if h != nil {
		cfg = h.cfg
	}
	preparer := zcodeManagementPreparer{executor: executor.NewZCodeExecutor(cfg), proxyURL: requestProxyURL}
	if !preparer.ShouldPrepareRequestAuth(auth) {
		return stringValue(auth.Metadata, "access_token"), nil
	}
	if h == nil || h.authManager == nil || auth.ID == "" {
		return "", fmt.Errorf("ZCode token refresh requires a registered credential")
	}
	updated, errPrepare := h.authManager.PrepareRequestAuth(ctx, preparer, auth)
	if errPrepare != nil {
		return "", errPrepare
	}
	if updated == nil {
		return "", fmt.Errorf("ZCode token refresh returned no credential")
	}
	return stringValue(updated.Metadata, "access_token"), nil
}

// Reset cards require the ZCode JWT and the business token, not the project inference token.
func applyZCodeResetAuthorization(auth *coreauth.Auth, req *http.Request) error {
	if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.Provider), "zcode") || req.URL.Scheme != "https" || req.URL.Host != "zcode.z.ai" {
		return nil
	}
	if req.URL.Path != "/api/v1/coding-plan/reset/status" && req.URL.Path != "/api/v1/coding-plan/reset/use" {
		return nil
	}
	jwt := stringValue(auth.Metadata, "zcode_jwt")
	businessToken := stringValue(auth.Metadata, "business_access_token")
	if jwt == "" || businessToken == "" {
		return fmt.Errorf("ZCode authorization is incomplete; sign in again")
	}
	if !strings.HasPrefix(strings.ToLower(jwt), "bearer ") {
		jwt = "Bearer " + jwt
	}
	req.Header.Set("Authorization", jwt)
	req.Header.Set("X-Bigmodel-Authorization", businessToken)
	req.Header.Set("Bigmodel-Target-Type", "PERSONAL")
	return nil
}
