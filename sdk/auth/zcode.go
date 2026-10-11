package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/zcode"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

type ZCodeAuthenticator struct{}

func NewZCodeAuthenticator() Authenticator { return &ZCodeAuthenticator{} }

func (ZCodeAuthenticator) Provider() string { return "zcode" }

func (ZCodeAuthenticator) RefreshLead() *time.Duration {
	lead := 2 * time.Minute
	return &lead
}

func (ZCodeAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cliproxy auth: configuration is required")
	}
	if opts == nil {
		opts = &LoginOptions{}
	}
	ctx, cancel := context.WithTimeout(ctx, zcode.MaxLoginDuration)
	defer cancel()
	client := zcode.NewClient(cfg, "")
	flow, errStart := client.StartLogin(ctx)
	if errStart != nil {
		return nil, errStart
	}
	fmt.Printf("To authorize your personal GLM Coding Plan, visit:\n%s\n", flow.AuthorizeURL)
	if !opts.NoBrowser && browser.IsAvailable() {
		if errOpen := browser.OpenURL(flow.AuthorizeURL); errOpen != nil {
			log.Warnf("Failed to open ZCode authorization page: %v", errOpen)
		}
	}
	fmt.Println("Waiting for authorization (up to five minutes)...")
	login, errLogin := client.WaitForLogin(ctx, flow)
	if errLogin != nil {
		return nil, errLogin
	}
	return client.CreateAuthRecord(ctx, login)
}
