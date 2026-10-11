package management

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/auth/zcode"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

type zcodeOAuthService interface {
	StartLogin(context.Context) (*zcode.LoginFlow, error)
	WaitForLogin(context.Context, *zcode.LoginFlow) (*zcode.LoginResult, error)
	CreateAuthRecord(context.Context, *zcode.LoginResult) (*coreauth.Auth, error)
}

var newZCodeOAuthService = func(cfg *config.Config) zcodeOAuthService { return zcode.NewClient(cfg, "") }

func (h *Handler) RequestZCodeToken(c *gin.Context) {
	service := newZCodeOAuthService(h.cfg)
	flow, errStart := service.StartLogin(c.Request.Context())
	if errStart != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": errStart.Error()})
		return
	}
	if errState := ValidateOAuthState(flow.State); errState != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "invalid ZCode authorization state"})
		return
	}
	RegisterOAuthSession(flow.State, "zcode")
	ctx := PopulateAuthContext(context.Background(), c)
	go h.completeZCodeOAuth(ctx, flow, service)
	expiresIn := int(time.Until(time.Unix(flow.ExpiresAt, 0)).Seconds())
	if maximum := int(zcode.MaxLoginDuration.Seconds()); expiresIn > maximum {
		expiresIn = maximum
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "url": flow.AuthorizeURL, "state": flow.State, "flow": "polling", "expires_in": expiresIn})
}

func (h *Handler) completeZCodeOAuth(ctx context.Context, flow *zcode.LoginFlow, service zcodeOAuthService) {
	ctx, cancel := context.WithTimeout(ctx, zcode.MaxLoginDuration)
	defer cancel()
	go watchOAuthSessionCancel(ctx, cancel, flow.State, "zcode")
	login, errLogin := service.WaitForLogin(ctx, flow)
	if errLogin != nil {
		if IsOAuthSessionPending(flow.State, "zcode") {
			message := errLogin.Error()
			if errors.Is(errLogin, context.DeadlineExceeded) {
				message = "ZCode authorization timed out; start login again"
			}
			SetOAuthSessionError(flow.State, message)
		}
		return
	}
	if !IsOAuthSessionPending(flow.State, "zcode") {
		return
	}
	record, errRecord := service.CreateAuthRecord(ctx, login)
	if errRecord != nil {
		if IsOAuthSessionPending(flow.State, "zcode") {
			SetOAuthSessionError(flow.State, errRecord.Error())
		}
		return
	}
	if errGuard := guardOAuthSessionPendingForSave(flow.State, "zcode"); errGuard != nil {
		return
	}
	if _, errSave := h.saveTokenRecord(ctx, record); errSave != nil {
		SetOAuthSessionError(flow.State, "Failed to save ZCode credentials")
		return
	}
	CompleteOAuthSession(flow.State)
	log.Info("ZCode Coding Plan authentication successful")
}
