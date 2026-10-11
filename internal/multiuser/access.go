package multiuser

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
)

const AccessProviderID = "multi-user"

type AccessProvider struct{ Store *Store }

func (*AccessProvider) Identifier() string { return AccessProviderID }

func (p *AccessProvider) Authenticate(_ context.Context, r *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	if p.Store == nil {
		return nil, sdkaccess.NewInvalidCredentialError()
	}
	values := []string{bearer(r.Header.Get("Authorization")), r.Header.Get("X-Api-Key"), r.Header.Get("X-Goog-Api-Key"), r.URL.Query().Get("key"), r.URL.Query().Get("auth_token")}
	provided := false
	for _, value := range values {
		if value == "" {
			continue
		}
		provided = true
		user, key, ok := p.Store.AuthenticateKey(value)
		if ok {
			autoBalance := "false"
			if key.AutoBalance {
				autoBalance = "true"
			}
			blockedModels, _ := json.Marshal(key.BlockedModels)
			return &sdkaccess.Result{Provider: AccessProviderID, Principal: key.ID, Metadata: map[string]string{"site_id": p.Store.SiteID(), "username": user.Username, "key_id": key.ID, "key_name": key.Name, "key_auto_balance": autoBalance, "key_blocked_models": string(blockedModels)}}, nil
		}
	}
	if !provided {
		return nil, sdkaccess.NewNoCredentialsError()
	}
	return nil, sdkaccess.NewInvalidCredentialError()
}

func bearer(header string) string {
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		return parts[1]
	}
	return ""
}
